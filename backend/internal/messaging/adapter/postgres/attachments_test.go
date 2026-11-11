//go:build integration

package postgres

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// newAttachment stores an uploaded file of uploader in conversation c, pending, for a day.
func newAttachment(t *testing.T, s *Store, c, uploader, name string) string {
	t.Helper()
	id := clientID()
	err := s.InsertAttachment(t.Context(), domain.NewAttachment{
		ID: id, UploaderID: uploader, ConversationID: c, Name: name, ObjectKey: "conversations/" + c + "/attachments/" + id,
		ContentType: "application/pdf", Size: 1234, ExpiresAt: time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("InsertAttachment: %v", err)
	}
	return id
}

// sendWith stores a message with no text and claims the attachments for it, in one
// transaction, as the usecase does; it fails the test unless all were claimed.
func sendWith(t *testing.T, s *Store, pool *pgxpool.Pool, c, sender string, ids ...string) domain.Message {
	t.Helper()
	var m domain.Message
	inTx(t, pool, func(ctx context.Context) error {
		var (
			added bool
			err   error
		)
		if m, added, err = s.AddMessage(ctx, domain.Message{ConversationID: c, SenderID: sender, ClientID: clientID(), HasAttachments: true}); err != nil || !added {
			return errors.Join(err, errors.New("not added"))
		}
		n, err := s.ClaimAttachments(ctx, ids, sender, c, m.ID)
		if err == nil && n != len(ids) {
			err = errors.New("not every attachment was claimed")
		}
		return err
	})
	return m
}

func setScan(t *testing.T, pool *pgxpool.Pool, id, status string) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), "UPDATE stored_files SET scan_status = $2, scanned_at = CASE WHEN $2 = 'pending' THEN NULL ELSE now() END WHERE id = $1", id, status); err != nil {
		t.Fatal(err)
	}
}

func checkViolation(t *testing.T, err error, constraint string) {
	t.Helper()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.ConstraintName != constraint {
		t.Errorf("err = %v, want a violation of %s", err, constraint)
	}
}

// FR-16.7: an attachment is claimed once, only by its uploader, in its conversation, unexpired and
// not blocked or failed; a message lists its files in the order sent.
func TestClaimAndListAttachments(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	other := create(t, s, ada, cy, domain.StateActive, nil)
	a, b := newAttachment(t, s, c.ID, ada, "a.pdf"), newAttachment(t, s, c.ID, ada, "b.pdf")
	m := sendWith(t, s, pool, c.ID, ada, b, a)
	if !m.HasAttachments || m.Body != nil {
		t.Fatalf("message = %+v", m)
	}
	files, err := s.ListAttachments(ctx, c.ID, []string{m.ID})
	if err != nil || len(files[m.ID]) != 2 || files[m.ID][0].ID != b || files[m.ID][1].ID != a {
		t.Fatalf("listed = %+v, %v", files, err)
	}
	if f := files[m.ID][0]; f.Name != "b.pdf" || f.Size != 1234 || f.ContentType != "application/pdf" || f.Status != domain.AttachmentPending {
		t.Errorf("file = %+v", f)
	}

	expired, infected, failed := newAttachment(t, s, c.ID, ada, "e.pdf"), newAttachment(t, s, c.ID, ada, "i.pdf"), newAttachment(t, s, c.ID, ada, "f.pdf")
	if _, err := pool.Exec(ctx, "UPDATE message_attachments SET expires_at = now() - interval '1 second', created_at = now() - interval '1 day' WHERE file_id = $1", expired); err != nil {
		t.Fatal(err)
	}
	setScan(t, pool, infected, "infected")
	setScan(t, pool, failed, "failed")
	clean := newAttachment(t, s, c.ID, ada, "c.pdf")
	setScan(t, pool, clean, "clean")
	target := add(t, s, c.ID, ada, "target")
	for name, tc := range map[string]struct {
		id, uploader, conversation string
	}{
		"used":                   {a, ada, c.ID},
		"someone else's":         {newAttachment(t, s, c.ID, bob, "x.pdf"), ada, c.ID},
		"another conversation's": {newAttachment(t, s, other.ID, ada, "o.pdf"), ada, c.ID},
		"expired":                {expired, ada, c.ID},
		"infected":               {infected, ada, c.ID},
		"failed":                 {failed, ada, c.ID},
		"unknown":                {clientID(), ada, c.ID},
	} {
		if n, err := s.ClaimAttachments(ctx, []string{tc.id}, tc.uploader, tc.conversation, target.ID); err != nil || n != 0 {
			t.Errorf("%s: claimed %d, %v", name, n, err)
		}
	}
	if n, err := s.ClaimAttachments(ctx, []string{clean}, ada, c.ID, target.ID); err != nil || n != 1 {
		t.Errorf("clean: claimed %d, %v", n, err)
	}
}

// FR-16.7: the database refuses a sixth file, a text message with neither text nor files, and
// a file of another user's (the foreign key includes the owner).
func TestAttachmentConstraints(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	ids := make([]string, 6)
	for i := range ids {
		ids[i] = newAttachment(t, s, c.ID, ada, "f.pdf")
	}
	m := sendWith(t, s, pool, c.ID, ada, ids[:5]...)
	_, err := pool.Exec(ctx, "UPDATE message_attachments SET message_id = $1, position = 5 WHERE file_id = $2", m.ID, ids[5])
	checkViolation(t, err, "message_attachments_position")

	_, err = pool.Exec(ctx, "INSERT INTO messages (conversation_id, sender_id, client_id) VALUES ($1, $2, gen_random_uuid())", c.ID, ada)
	checkViolation(t, err, "messages_live_has_body")

	_, err = pool.Exec(ctx, "UPDATE message_attachments SET uploader_id = $1 WHERE file_id = $2", bob, ids[5])
	checkViolation(t, err, "message_attachments_file_fk")

	_, err = pool.Exec(ctx, "UPDATE message_attachments SET name = 'a/b' WHERE file_id = $1", ids[5])
	checkViolation(t, err, "message_attachments_name")

	_, err = pool.Exec(ctx, "UPDATE stored_files SET size_bytes = 26214401 WHERE id = $1", ids[5])
	checkViolation(t, err, "stored_files_attachment_size")
}

// NFR-13: FindAttachment gives the caller's membership and the message's time, so the usecase
// can apply joined_at; a former member has none.
func TestFindAttachment(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy, dan := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy"), newUser(t, pool, "Dan")
	g := newGroup(t, s, pool, ada, bob, cy).ConversationID
	up := newAttachment(t, s, g, ada, "n.pdf")
	unsent, err := s.FindAttachment(ctx, up, ada)
	if err != nil || unsent.MessageID != nil || unsent.JoinedAt == nil || unsent.UploaderID != ada || unsent.ObjectKey == "" {
		t.Fatalf("unsent = %+v, %v", unsent, err)
	}
	m := sendWith(t, s, pool, g, ada, up)
	got, err := s.FindAttachment(ctx, up, bob)
	if err != nil || got.MessageID == nil || *got.MessageID != m.ID || got.MessageAt == nil || !got.MessageAt.Equal(m.CreatedAt) ||
		got.JoinedAt == nil || got.JoinedAt.After(m.CreatedAt) || got.MessageDeleted {
		t.Errorf("member = %+v, %v", got, err)
	}
	if got, err := s.FindAttachment(ctx, up, dan); err != nil || got.JoinedAt != nil {
		t.Errorf("non-member = %+v, %v", got, err)
	}
	if _, err := s.FindAttachment(ctx, clientID(), ada); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	if _, err := s.FindAttachment(ctx, "nope", ada); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("not a UUID: %v", err)
	}
	// Who hears about the scan: members who joined by the message.
	inTx(t, pool, func(ctx context.Context) error {
		return s.AddGroupMembers(ctx, g, []string{dan}, time.Now().Add(time.Second))
	})
	au, found, err := s.AttachmentAudience(ctx, up)
	if err != nil || !found || au.MessageID == nil || au.UploaderID != ada {
		t.Fatalf("audience = %+v %v %v", au, found, err)
	}
	members, err := s.MembersSince(ctx, g, *au.MessageAt)
	slices.Sort(members)
	want := []string{ada, bob, cy}
	slices.Sort(want)
	if err != nil || !slices.Equal(members, want) {
		t.Errorf("members since = %v, want %v (%v)", members, want, err)
	}
	if _, err := s.DeleteMessage(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.FindAttachment(ctx, up, bob); !got.MessageDeleted {
		t.Error("deleted message not reported")
	}
}

// FR-16.5: a message with only files is deleted, and edited, without a version (it had no text);
// its file row stays for retention.
func TestDeleteAttachmentOnlyMessage(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	m := sendWith(t, s, pool, c.ID, ada, newAttachment(t, s, c.ID, ada, "a.pdf"))
	edited, err := s.EditMessage(ctx, m.ID, "with text")
	if err != nil || *edited.Body != "with text" || len(versionsOf(t, pool, m.ID)) != 0 {
		t.Fatalf("edit: %+v %v, versions %v", edited, err, versionsOf(t, pool, m.ID))
	}
	m2 := sendWith(t, s, pool, c.ID, ada, newAttachment(t, s, c.ID, ada, "b.pdf"))
	if _, err := s.DeleteMessage(ctx, m2.ID); err != nil || len(versionsOf(t, pool, m2.ID)) != 0 {
		t.Fatalf("delete: %v, versions %v", err, versionsOf(t, pool, m2.ID))
	}
	var files int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM stored_files WHERE purpose = 'attachment'").Scan(&files); err != nil || files != 2 {
		t.Errorf("stored files = %d, %v", files, err)
	}
}

// FR-2.8, FR-16.7: an attachment row that goes takes its stored file, and the object is queued;
// the purge removes only unused, expired files; account deletion queues every object.
func TestAttachmentDeletionQueuesObjects(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	stale, fresh := newAttachment(t, s, c.ID, ada, "s.pdf"), newAttachment(t, s, c.ID, ada, "f.pdf")
	sent := newAttachment(t, s, c.ID, bob, "sent.pdf")
	sendWith(t, s, pool, c.ID, bob, sent)
	if _, err := pool.Exec(ctx, "UPDATE message_attachments SET expires_at = now() - interval '1 second', created_at = now() - interval '2 days' WHERE file_id IN ($1, $2)", stale, sent); err != nil {
		t.Fatal(err)
	}
	n, err := s.DeleteExpiredAttachments(ctx, 100)
	if err != nil || n != 1 {
		t.Fatalf("purged %d, %v", n, err)
	}
	queued := func() []string {
		rows, err := pool.Query(ctx, "SELECT object_key FROM storage_deletions ORDER BY object_key")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				t.Fatal(err)
			}
			out = append(out, k)
		}
		return out
	}
	if q := queued(); len(q) != 1 || q[0] != "conversations/"+c.ID+"/attachments/"+stale {
		t.Errorf("queued = %v", q)
	}
	// Account deletion (FR-2.8) removes the direct conversation (000015 cascades it with
	// either member), so every file in it goes and every object is queued.
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", ada); err != nil {
		t.Fatal(err)
	}
	q := queued()
	for _, id := range []string{stale, fresh, sent} {
		if !slices.Contains(q, "conversations/"+c.ID+"/attachments/"+id) {
			t.Errorf("%s not queued: %v", id, q)
		}
	}
	var left int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM stored_files").Scan(&left); err != nil || left != 0 || len(q) != 3 {
		t.Errorf("stored files left %d, queued %v (%v)", left, q, err)
	}
}

// FR-2.8 in a group: the leaver's account goes with their messages and files; the others' stay.
func TestAccountDeletionInGroupQueuesOwnFiles(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy")
	g := newGroup(t, s, pool, ada, bob, cy).ConversationID
	mine, theirs := newAttachment(t, s, g, bob, "b.pdf"), newAttachment(t, s, g, cy, "c.pdf")
	sendWith(t, s, pool, g, bob, mine)
	sendWith(t, s, pool, g, cy, theirs)
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", bob); err != nil {
		t.Fatal(err)
	}
	var queued []string
	rows, err := pool.Query(ctx, "SELECT object_key FROM storage_deletions")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		queued = append(queued, k)
	}
	rows.Close()
	if len(queued) != 1 || queued[0] != "conversations/"+g+"/attachments/"+mine {
		t.Errorf("queued = %v", queued)
	}
	if _, err := s.FindAttachment(ctx, theirs, cy); err != nil {
		t.Errorf("the other member's file: %v", err)
	}
}

// NFR-13: the administrator reaches only files of the report's conversation sent by its moment,
// a deleted message's included, and each download is recorded.
func TestReportedAttachments(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	other := create(t, s, ada, cy, domain.StateActive, nil)
	before := newAttachment(t, s, c.ID, bob, "proof.pdf")
	m := sendWith(t, s, pool, c.ID, bob, before)
	elsewhere := newAttachment(t, s, other.ID, ada, "x.pdf")
	sendWith(t, s, pool, other.ID, ada, elsewhere)
	report := newReport(t, s, c.ID, ada, bob, nil)
	after := newAttachment(t, s, c.ID, bob, "later.pdf")
	m2 := sendWith(t, s, pool, c.ID, bob, after)
	if _, err := s.DeleteMessage(ctx, m.ID); err != nil {
		t.Fatal(err)
	}

	got, err := s.FindReportedAttachment(ctx, report, before)
	if err != nil || got.ID != before || got.ObjectKey == "" || got.Status != domain.AttachmentPending {
		t.Errorf("before the report = %+v, %v", got, err)
	}
	for name, id := range map[string]string{"after the report": after, "another conversation": elsewhere, "unknown": clientID()} {
		if _, err := s.FindReportedAttachment(ctx, report, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	files, err := s.ListReportedAttachments(ctx, report, []string{m.ID, m2.ID})
	if err != nil || len(files[m.ID]) != 1 || len(files[m2.ID]) != 0 {
		t.Errorf("listed = %+v, %v", files, err)
	}
	admin := newUser(t, pool, "Admin")
	if err := s.AddAttachmentDownload(ctx, report, before, admin); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM message_report_attachment_downloads WHERE report_id = $1 AND file_id = $2 AND admin_id = $3",
		report, before, admin).Scan(&n); err != nil || n != 1 {
		t.Errorf("audit rows = %d, %v", n, err)
	}
}

// The preview of a message with only files has no text and counts them.
func TestPreviewCountsAttachments(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	sendWith(t, s, pool, c.ID, ada, newAttachment(t, s, c.ID, ada, "a.pdf"), newAttachment(t, s, c.ID, ada, "b.pdf"))
	it, err := s.FindItem(t.Context(), c.ID, bob)
	if err != nil || it.LastMessage == nil || it.LastMessage.Body != nil || it.LastMessage.AttachmentCount != 2 || it.UnreadCount != 1 {
		t.Errorf("item = %+v %+v, %v", it, it.LastMessage, err)
	}
}
