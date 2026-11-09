//go:build integration

package postgres

import (
	"time"

	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

type keptVersion struct {
	body, endedBy string
}

// versionsOf reads a message's kept versions straight from the table, oldest first.
func versionsOf(t *testing.T, pool *pgxpool.Pool, messageID string) []keptVersion {
	t.Helper()
	rows, err := pool.Query(t.Context(), "SELECT body, ended_by FROM message_versions WHERE message_id = $1 ORDER BY shown_until", messageID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []keptVersion
	for rows.Next() {
		var v keptVersion
		if err := rows.Scan(&v.body, &v.endedBy); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// FR-16.5: an edit replaces the text and keeps the old one; a deletion leaves a tombstone and
// keeps the last text; both find only the sender's message.
func TestEditAndDeleteMessage(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	m := add(t, s, c.ID, ada, "first")

	if _, err := s.FindOwnMessage(ctx, c.ID, m.ID, bob); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("another sender found it: %v", err)
	}
	if _, err := s.LockOwnMessage(ctx, c.ID, "not-a-uuid", ada); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a bad ID: %v", err)
	}

	one, err := s.EditMessage(ctx, m.ID, "second")
	if err != nil {
		t.Fatal(err)
	}
	if one.Body == nil || *one.Body != "second" || one.EditedAt == nil || one.EditedAt.Before(m.CreatedAt) || one.Deleted() {
		t.Errorf("edited = %+v", one)
	}
	two, err := s.EditMessage(ctx, m.ID, "third")
	if err != nil {
		t.Fatal(err)
	}
	var from, until []string
	rows, err := pool.Query(ctx, `SELECT shown_from::text, shown_until::text FROM message_versions WHERE message_id = $1 ORDER BY shown_until`, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var f, u string
		if err := rows.Scan(&f, &u); err != nil {
			t.Fatal(err)
		}
		from, until = append(from, f), append(until, u)
	}
	rows.Close()
	// Each version is shown from the send or the edit before it until the edit that replaced it.
	var created, firstEdit, secondEdit string
	if err := pool.QueryRow(ctx, `SELECT $1::timestamptz::text, $2::timestamptz::text, $3::timestamptz::text`,
		m.CreatedAt, *one.EditedAt, *two.EditedAt).Scan(&created, &firstEdit, &secondEdit); err != nil {
		t.Fatal(err)
	}
	if len(from) != 2 || from[0] != created || until[0] != firstEdit || from[1] != firstEdit || until[1] != secondEdit {
		t.Errorf("versions shown %v until %v; sent %s, edited %s and %s", from, until, created, firstEdit, secondEdit)
	}

	gone, err := s.DeleteMessage(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gone.Body != nil || !gone.Deleted() || gone.EditedAt == nil {
		t.Errorf("tombstone = %+v", gone)
	}
	want := []keptVersion{{"first", "edited"}, {"second", "edited"}, {"third", "deleted"}}
	if got := versionsOf(t, pool, m.ID); len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("versions = %v", got)
	}
	if _, err := s.DeleteMessage(ctx, m.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
	if _, err := s.EditMessage(ctx, m.ID, "back"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("edit of a tombstone: %v", err)
	}

	// Members see the tombstone in its place, and the retried client ID finds it.
	list, err := s.ListMessages(ctx, c.ID, time.Time{}, "", 10)
	if err != nil || len(list) != 1 || list[0].Body != nil || !list[0].Deleted() {
		t.Errorf("ListMessages = %+v, %v", list, err)
	}
	sent, found, err := s.FindSent(ctx, ada, m.ClientID)
	if err != nil || !found || sent.Message.Body != nil || !sent.Message.Deleted() {
		t.Errorf("FindSent = %+v, %v, %v", sent, found, err)
	}
	if own, err := s.FindOwnMessage(ctx, c.ID, m.ID, ada); err != nil || !own.Deleted() {
		t.Errorf("FindOwnMessage = %+v, %v", own, err)
	}
}

// A deleted message no longer counts as unread; the preview of an edited message has the new
// text, and of a deleted one no text and the flag.
func TestDeletedMessagesInListsAndCounts(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	add(t, s, c.ID, ada, "one")
	last := add(t, s, c.ID, ada, "two")

	if _, err := s.EditMessage(ctx, last.ID, "two, edited"); err != nil {
		t.Fatal(err)
	}
	it, err := s.FindItem(ctx, c.ID, bob)
	if err != nil || it.LastMessage == nil || it.LastMessage.Body == nil || *it.LastMessage.Body != "two, edited" || it.LastMessage.Deleted {
		t.Errorf("edited preview = %+v, %v", it.LastMessage, err)
	}

	if _, err := s.DeleteMessage(ctx, last.ID); err != nil {
		t.Fatal(err)
	}
	it, err = s.FindItem(ctx, c.ID, bob)
	if err != nil || it.LastMessage == nil || it.LastMessage.Body != nil || !it.LastMessage.Deleted || it.UnreadCount != 1 {
		t.Errorf("after delete: preview %+v, unread %d, %v", it.LastMessage, it.UnreadCount, err)
	}
	items, err := s.ListItems(ctx, bob, domain.BoxInbox, nil, 10)
	if err != nil || len(items) != 1 || !items[0].LastMessage.Deleted || items[0].UnreadCount != 1 {
		t.Errorf("ListItems = %+v, %v", items, err)
	}
	n, err := s.CountUnread(ctx, bob)
	if err != nil || n.Total != 1 {
		t.Errorf("CountUnread = %+v, %v", n, err)
	}
}

// NFR-14: the administrator sees the text shown at the report's moment and the versions shown
// up to it, and never a text written after it.
func TestReportedTextAtTheReportsMoment(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	editedBefore := add(t, s, c.ID, bob, "e1")
	editedAfter := add(t, s, c.ID, bob, "a")
	deletedBefore := add(t, s, c.ID, bob, "gone")
	deletedAfter := add(t, s, c.ID, bob, "kept")
	plain := add(t, s, c.ID, bob, "plain")
	mustEdit := func(id, body string) {
		t.Helper()
		if _, err := s.EditMessage(ctx, id, body); err != nil {
			t.Fatal(err)
		}
	}
	mustDelete := func(id string) {
		t.Helper()
		if _, err := s.DeleteMessage(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	mustEdit(editedBefore.ID, "e2")
	mustDelete(deletedBefore.ID)
	report := newReport(t, s, c.ID, ada, bob, &plain.ID)
	mustEdit(editedBefore.ID, "e3")
	mustEdit(editedAfter.ID, "b")
	mustDelete(deletedAfter.ID)

	msgs, err := s.ListReportedMessages(ctx, report, "", 10)
	if err != nil || len(msgs) != 5 {
		t.Fatalf("ListReportedMessages = %+v, %v", msgs, err)
	}
	byID := map[string]domain.ReportedMessage{}
	ids := make([]string, len(msgs))
	for i, m := range msgs {
		byID[m.ID], ids[i] = m, m.ID
	}
	versions, err := s.ListReportedVersions(ctx, report, ids)
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string][]string{}
	for _, v := range versions {
		kept[v.MessageID] = append(kept[v.MessageID], v.Body+"/"+string(v.EndedBy))
	}

	for _, tc := range []struct {
		name     string
		id       string
		body     *string
		changed  bool
		deleted  bool
		versions []string
	}{
		{"edited before and after", editedBefore.ID, str("e2"), true, false, []string{"e1/edited", "e2/edited"}},
		{"edited after", editedAfter.ID, str("a"), true, false, []string{"a/edited"}},
		{"deleted before", deletedBefore.ID, nil, false, true, []string{"gone/deleted"}},
		{"deleted after", deletedAfter.ID, str("kept"), true, true, []string{"kept/deleted"}},
		{"never changed", plain.ID, str("plain"), false, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := byID[tc.id]
			if (m.Body == nil) != (tc.body == nil) || (m.Body != nil && *m.Body != *tc.body) {
				t.Errorf("body = %v, want %v", m.Body, tc.body)
			}
			if m.ChangedAfterReport != tc.changed || (m.DeletedAt != nil) != tc.deleted {
				t.Errorf("changed %v, deleted at %v", m.ChangedAfterReport, m.DeletedAt)
			}
			got := kept[tc.id]
			if len(got) != len(tc.versions) {
				t.Fatalf("versions = %v, want %v", got, tc.versions)
			}
			for i := range got {
				if got[i] != tc.versions[i] {
					t.Errorf("versions = %v, want %v", got, tc.versions)
				}
			}
		})
	}
	if vs, err := s.ListReportedVersions(ctx, c.ID, ids); err != nil || len(vs) != 0 {
		t.Errorf("a conversation's ID opened versions: %+v, %v", vs, err)
	}
}
