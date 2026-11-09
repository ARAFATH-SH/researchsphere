package usecase

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// pdf is a small file the fake intake accepts.
var pdf = []byte("%PDF-1.7 test")

// withFiles gives the harness's service a fake intake and bucket.
func (h *harness) withFiles() *fakeObjects {
	objects := newFakeObjects()
	h.svc.UseFiles(FileDeps{Intake: fakeIntake{}, Objects: objects})
	return objects
}

func (h *harness) upload(t *testing.T, user, conversationID, name string) domain.Upload {
	t.Helper()
	up, err := h.svc.UploadAttachment(t.Context(), user, conversationID, name, bytes.NewReader(pdf))
	if err != nil {
		t.Fatalf("UploadAttachment(%s): %v", user, err)
	}
	return up
}

func (h *harness) setStatus(id string, st domain.AttachmentStatus) {
	a := h.store.attachments[id]
	a.status = st
	h.store.attachments[id] = a
}

// FR-16.7: an upload is stored under a key of the server's, pending its scan, for 24 hours, and
// counts towards both limits.
func TestUploadAttachment(t *testing.T) {
	h := newHarness()
	objects := h.withFiles()
	id, _, _ := chat(t, h)
	up := h.upload(t, "ada", id, `C:\Users\ada\Report "final".pdf`)
	if up.Status != domain.AttachmentPending || up.Name != "Report final.pdf" || up.Size != int64(len(pdf)) ||
		up.ContentType != "application/pdf" || !up.ExpiresAt.Equal(h.now.Add(24*time.Hour)) {
		t.Errorf("upload = %+v", up)
	}
	stored := h.store.attachments[up.ID]
	wantKey := fmt.Sprintf("conversations/%s/attachments/%s", id, up.ID)
	if stored.ObjectKey != wantKey || objects.keys[wantKey] != int64(len(pdf)) || stored.UploaderID != "ada" || stored.ConversationID != id {
		t.Errorf("stored %+v, objects %v", stored, objects.keys)
	}
	if !isUUID(up.ID) || strings.Contains(stored.ObjectKey, "Report") {
		t.Errorf("the key must not use the name: %q", stored.ObjectKey)
	}
	if h.limiter.used["attachments:hour|ada"] != 1 || h.limiter.used["attachment-bytes:day|ada"] != len(pdf) {
		t.Errorf("limits = %v", h.limiter.used)
	}
}

// FR-16.7, FR-16.9, FR-15.6, NFR-14: only a member who could send now may upload; nothing is
// stored or counted for a refused upload.
func TestUploadAttachmentRefusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(h *harness, id string)
		user  string
		want  error
	}{
		{"not a member", nil, "eve", domain.ErrNotFound},
		{"suspended", func(h *harness, _ string) { h.actors["ada"] = domain.Standing{Suspended: true} }, "ada", &domain.SuspendedError{}},
		{"restricted", func(h *harness, _ string) {
			until := h.now.Add(time.Hour)
			h.actors["ada"] = domain.Standing{RestrictedUntil: &until}
		}, "ada", &domain.RestrictedError{}},
		{"blocked them", func(h *harness, _ string) { h.blocks.set["ada|bob"] = true }, "ada", domain.ErrYouBlocked},
		{"blocked by them", func(h *harness, _ string) { h.blocks.set["bob|ada"] = true }, "ada", domain.ErrCannotMessage},
		{"other member hidden", func(h *harness, _ string) { h.store.hidden["bob"] = true }, "ada", domain.ErrNotFound},
		{"declined", func(h *harness, id string) { h.store.conversations[id].state = domain.StateDeclined }, "ada", domain.ErrClosed},
		{"pending request", func(h *harness, id string) {
			h.store.conversations[id].state = domain.StateRequest
			delete(h.conns, "ada|bob")
		}, "ada", domain.ErrRequestPending},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()
			objects := h.withFiles()
			id, _, _ := chat(t, h)
			if tc.setup != nil {
				tc.setup(h, id)
			}
			_, err := h.svc.UploadAttachment(t.Context(), tc.user, id, "a.pdf", bytes.NewReader(pdf))
			if !sameError(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(objects.keys) != 0 || len(h.store.attachments) != 0 || h.limiter.used["attachments:hour|"+tc.user] != 0 {
				t.Errorf("stored %v %v, counted %v", objects.keys, h.store.attachments, h.limiter.used)
			}
		})
	}
}

// sameError matches sentinel errors with errors.Is and typed ones by type.
func sameError(err, want error) bool {
	if _, ok := errors.AsType[*domain.SuspendedError](want); ok {
		_, ok := errors.AsType[*domain.SuspendedError](err)
		return ok
	}
	if _, ok := errors.AsType[*domain.RestrictedError](want); ok {
		_, ok := errors.AsType[*domain.RestrictedError](err)
		return ok
	}
	if _, ok := errors.AsType[*domain.RateLimitedError](want); ok {
		_, ok := errors.AsType[*domain.RateLimitedError](err)
		return ok
	}
	return errors.Is(err, want)
}

// FR-16.7: 25 MB at most, and only what the type check accepts. A refused file uses no quota.
func TestUploadAttachmentFile(t *testing.T) {
	h := newHarness()
	objects := h.withFiles()
	id, _, _ := chat(t, h)
	big := append([]byte("%PDF-"), make([]byte, MaxAttachmentBytes)...)
	if _, err := h.svc.UploadAttachment(t.Context(), "ada", id, "big.pdf", bytes.NewReader(big)); !errors.Is(err, domain.ErrFileTooLarge) {
		t.Errorf("26 MB: %v", err)
	}
	exactly := append([]byte("%PDF-"), make([]byte, MaxAttachmentBytes-5)...)
	if _, err := h.svc.UploadAttachment(t.Context(), "ada", id, "max.pdf", bytes.NewReader(exactly)); err != nil {
		t.Errorf("exactly 25 MB: %v", err)
	}
	for _, data := range [][]byte{[]byte("MZ\x90\x00 program"), {}} {
		if _, err := h.svc.UploadAttachment(t.Context(), "ada", id, "x.pdf", bytes.NewReader(data)); !errors.Is(err, domain.ErrUnsupportedType) {
			t.Errorf("%q: %v", data, err)
		}
	}
	if len(objects.keys) != 1 || h.limiter.used["attachments:hour|ada"] != 1 {
		t.Errorf("objects %d, counted %v", len(objects.keys), h.limiter.used)
	}
}

// FR-16.7: 20 uploads per hour and 200 MB per day; a limiter that does not answer lets the
// upload through (fail open, as the message limits).
func TestUploadAttachmentLimits(t *testing.T) {
	h := newHarness()
	h.withFiles()
	id, _, _ := chat(t, h)
	h.limiter.used["attachments:hour|ada"] = UploadsPerHour
	h.limiter.left["attachments:hour"] = 10 * time.Minute
	_, err := h.svc.UploadAttachment(t.Context(), "ada", id, "a.pdf", bytes.NewReader(pdf))
	if rl, ok := errors.AsType[*domain.RateLimitedError](err); !ok || rl.RetryAfter != 10*time.Minute {
		t.Fatalf("21st upload: %v", err)
	}
	h.limiter.used["attachments:hour|ada"] = 0
	h.limiter.used["attachment-bytes:day|ada"] = UploadBytesPerDay - len(pdf) + 1
	if _, err := h.svc.UploadAttachment(t.Context(), "ada", id, "a.pdf", bytes.NewReader(pdf)); !sameError(err, &domain.RateLimitedError{}) {
		t.Fatalf("over 200 MB: %v", err)
	}
	h.limiter.used["attachment-bytes:day|ada"] = UploadBytesPerDay - len(pdf)
	h.upload(t, "ada", id, "fits.pdf")
	h.limiter.failed = true
	h.upload(t, "ada", id, "open.pdf")
}

// A failed insert leaves no object behind.
func TestUploadAttachmentInsertFails(t *testing.T) {
	h := newHarness()
	objects := h.withFiles()
	id, _, _ := chat(t, h)
	h.store.failAttach = errors.New("db down")
	if _, err := h.svc.UploadAttachment(t.Context(), "ada", id, "a.pdf", bytes.NewReader(pdf)); !errors.Is(err, h.store.failAttach) {
		t.Fatal(err)
	}
	if len(objects.keys) != 0 || len(objects.deleted) != 1 || h.limiter.used["attachments:hour|ada"] != 0 {
		t.Errorf("objects %v deleted %v counted %v", objects.keys, objects.deleted, h.limiter.used)
	}
}

// FR-16.7: a message may carry files and no text; they come back in the order sent.
func TestSendWithAttachments(t *testing.T) {
	h := newHarness()
	h.withFiles()
	id, _, _ := chat(t, h)
	a := h.upload(t, "ada", id, "a.pdf")
	b := h.upload(t, "ada", id, "b.pdf")
	sent, err := h.svc.SendWithAttachments(t.Context(), "ada", id, nil, h.clientID(), []string{b.ID, a.ID})
	if err != nil {
		t.Fatal(err)
	}
	m := sent.Message
	if m.Body != nil || !m.HasAttachments || len(m.Attachments) != 2 || m.Attachments[0].ID != b.ID || m.Attachments[1].ID != a.ID {
		t.Fatalf("message = %+v", m)
	}
	blank := "  "
	c := h.upload(t, "ada", id, "c.pdf")
	if s, err := h.svc.SendWithAttachments(t.Context(), "ada", id, &blank, h.clientID(), []string{c.ID}); err != nil || s.Message.Body != nil {
		t.Errorf("blank body with a file: %+v %v", s.Message, err)
	}
	text := " see attached "
	d := h.upload(t, "ada", id, "d.pdf")
	if s, err := h.svc.SendWithAttachments(t.Context(), "ada", id, &text, h.clientID(), []string{d.ID}); err != nil || *s.Message.Body != "see attached" {
		t.Errorf("body and a file: %+v %v", s.Message, err)
	}
	if _, err := h.svc.SendWithAttachments(t.Context(), "ada", id, nil, h.clientID(), nil); !errors.Is(err, domain.ErrInvalidBody) {
		t.Errorf("neither body nor files: %v", err)
	}
	// The other member reads them in the thread.
	page := h.mustMessages(t, "bob", id)
	if got := page.Items[0].Attachments; len(got) != 1 || got[0].ID != d.ID {
		t.Errorf("listed = %+v", page.Items[0])
	}
	if got := page.Items[2].Attachments; len(got) != 2 {
		t.Errorf("listed = %+v", page.Items[2])
	}
	// A replay returns the original message with its files.
	clientID := h.clientID()
	e := h.upload(t, "ada", id, "e.pdf")
	first, _ := h.svc.SendWithAttachments(t.Context(), "ada", id, nil, clientID, []string{e.ID})
	again, err := h.svc.SendWithAttachments(t.Context(), "ada", id, nil, clientID, []string{e.ID})
	if err != nil || !again.Replayed || again.Message.ID != first.Message.ID || len(again.Message.Attachments) != 1 {
		t.Errorf("replay = %+v %v", again, err)
	}
}

// FR-16.7: each attachment must be the sender's, of this conversation, unused, unexpired and
// not blocked or failed by its scan; otherwise nothing is sent.
func TestSendWithAttachmentsRefusals(t *testing.T) {
	h := newHarness()
	h.withFiles()
	h.noLimits()
	h.withFiles()
	id, _, _ := chat(t, h)
	h.connect("ada", "cy")
	other := h.direct(t, "ada", "cy", "hi").Message.ConversationID

	mine := func() string { return h.upload(t, "ada", id, "x.pdf").ID }
	used := mine()
	if _, err := h.svc.SendWithAttachments(t.Context(), "ada", id, nil, h.clientID(), []string{used}); err != nil {
		t.Fatal(err)
	}
	expired := mine()
	a := h.store.attachments[expired]
	a.ExpiresAt = h.store.clock()
	h.store.attachments[expired] = a
	infected, failed := mine(), mine()
	h.setStatus(infected, domain.AttachmentInfected)
	h.setStatus(failed, domain.AttachmentFailed)
	six := []string{mine(), mine(), mine(), mine(), mine(), mine()}
	dup := mine()

	for name, ids := range map[string][]string{
		"someone else's":         {h.upload(t, "bob", id, "b.pdf").ID},
		"another conversation's": {h.upload(t, "ada", other, "o.pdf").ID},
		"already used":           {used},
		"expired":                {expired},
		"infected":               {infected},
		"failed":                 {failed},
		"more than 5":            six,
		"repeated":               {dup, dup},
		"not a UUID":             {"file-1"},
		"unknown":                {uid(999)},
		"one good, one bad":      {mine(), infected},
	} {
		t.Run(name, func(t *testing.T) {
			before := len(h.store.messages)
			_, err := h.svc.SendWithAttachments(t.Context(), "ada", id, nil, h.clientID(), ids)
			if !errors.Is(err, domain.ErrInvalidAttachment) {
				t.Fatalf("err = %v", err)
			}
			if len(h.store.messages) != before {
				t.Errorf("a message was stored")
			}
			for _, fid := range ids {
				if a, ok := h.store.attachments[fid]; ok && fid != used && a.messageID != "" {
					t.Errorf("%s was claimed", fid)
				}
			}
		})
	}
}

// FR-16.7, NFR-13, NFR-14: who may download, and nothing before the scan says clean.
func TestDownloadAttachment(t *testing.T) {
	h := groupHarness()
	objects := h.withFiles()
	g := h.group(t, ben)
	up := h.upload(t, owner, g, "notes.pdf")

	// Before sending, only the uploader, and only once clean.
	if _, err := h.svc.DownloadAttachment(t.Context(), owner, up.ID); !errors.Is(err, domain.ErrScanPending) {
		t.Errorf("uploader before clean: %v", err)
	}
	h.setStatus(up.ID, domain.AttachmentClean)
	if _, err := h.svc.DownloadAttachment(t.Context(), ben, up.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("member, unsent: %v", err)
	}
	if d, err := h.svc.DownloadAttachment(t.Context(), owner, up.ID); err != nil || d.URL == nil {
		t.Errorf("uploader, unsent, clean: %v", err)
	}
	if _, err := h.svc.SendWithAttachments(t.Context(), owner, g, nil, h.clientID(), []string{up.ID}); err != nil {
		t.Fatal(err)
	}
	// A member who joined after the message never sees it (FR-16.6, NFR-14).
	h.store.clock = func() time.Time { return time.Date(2026, 11, 4, 11, 0, 0, 0, time.UTC) }
	h.now = h.store.clock()
	if _, err := h.svc.AddMembers(t.Context(), owner, g, []string{cat}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		user   string
		status domain.AttachmentStatus
		want   error
	}{
		{ben, domain.AttachmentClean, nil},
		{owner, domain.AttachmentClean, nil},
		{cat, domain.AttachmentClean, domain.ErrNotFound},
		{dan, domain.AttachmentClean, domain.ErrNotFound},
		{"not-a-uuid", domain.AttachmentClean, domain.ErrNotFound},
		{ben, domain.AttachmentPending, domain.ErrScanPending},
		{owner, domain.AttachmentPending, domain.ErrScanPending},
		{ben, domain.AttachmentFailed, domain.ErrScanFailed},
		{ben, domain.AttachmentInfected, domain.ErrFileRemoved},
		{cat, domain.AttachmentInfected, domain.ErrNotFound},
	} {
		h.setStatus(up.ID, tc.status)
		_, err := h.svc.DownloadAttachment(t.Context(), tc.user, up.ID)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s, %s: err = %v, want %v", tc.user, tc.status, err, tc.want)
		}
	}
	want := fmt.Sprintf("conversations/%s/attachments/%s|notes.pdf|application/pdf|5m0s", g, up.ID)
	if len(objects.presign) == 0 || objects.presign[len(objects.presign)-1] != want {
		t.Errorf("presign = %v, want %s", objects.presign, want)
	}
	if _, err := h.svc.DownloadAttachment(t.Context(), ben, uid(999)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}

	// A member who left can no longer download (NFR-14).
	h.setStatus(up.ID, domain.AttachmentClean)
	if err := h.svc.Leave(t.Context(), ben, g); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.DownloadAttachment(t.Context(), ben, up.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("former member: %v", err)
	}
}

// FR-16.5, FR-16.7: a deleted message shows no files and they cannot be downloaded; the file is
// kept for retention, not deleted.
func TestDeletedMessageHidesAttachments(t *testing.T) {
	h := newHarness()
	objects := h.withFiles()
	id, _, _ := chat(t, h)
	up := h.upload(t, "ada", id, "a.pdf")
	h.setStatus(up.ID, domain.AttachmentClean)
	sent, err := h.svc.SendWithAttachments(t.Context(), "ada", id, nil, h.clientID(), []string{up.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Delete(t.Context(), "ada", id, sent.Message.ID); err != nil {
		t.Fatal(err)
	}
	page := h.mustMessages(t, "bob", id)
	if !page.Items[0].Deleted() || len(page.Items[0].Attachments) != 0 {
		t.Errorf("deleted = %+v", page.Items[0])
	}
	for _, u := range []string{"ada", "bob"} {
		if _, err := h.svc.DownloadAttachment(t.Context(), u, up.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s: %v", u, err)
		}
	}
	if len(objects.deleted) != 0 || h.store.attachments[up.ID].messageID == "" {
		t.Errorf("the file must stay for retention: deleted %v", objects.deleted)
	}
}

// An edit can give an attachment-only message text, and its files stay with it.
func TestEditAttachmentOnlyMessage(t *testing.T) {
	h := newHarness()
	h.withFiles()
	id, _, _ := chat(t, h)
	up := h.upload(t, "ada", id, "a.pdf")
	sent, err := h.svc.SendWithAttachments(t.Context(), "ada", id, nil, h.clientID(), []string{up.ID})
	if err != nil {
		t.Fatal(err)
	}
	m, err := h.svc.Edit(t.Context(), "ada", id, sent.Message.ID, "now with text")
	if err != nil || *m.Body != "now with text" || len(m.Attachments) != 1 {
		t.Fatalf("edit = %+v %v", m, err)
	}
}

// FR-16.7: attachment.updated reaches the uploader alone before the file is sent, and the
// members who can see the message afterwards.
func TestAttachmentScanned(t *testing.T) {
	h := groupHarness()
	h.withFiles()
	g := h.group(t, ben)
	up := h.upload(t, ben, g, "a.pdf")
	h.pub.events = nil
	h.svc.AttachmentScanned(t.Context(), up.ID, domain.AttachmentClean)
	if want := fmt.Sprintf("attachment %s - clean to [%s]", up.ID, ben); !slices.Equal(h.pub.events, []string{want}) {
		t.Errorf("unsent: %v", h.pub.events)
	}
	sent, err := h.svc.SendWithAttachments(t.Context(), ben, g, nil, h.clientID(), []string{up.ID})
	if err != nil {
		t.Fatal(err)
	}
	h.store.clock = func() time.Time { return time.Date(2026, 11, 4, 11, 0, 0, 0, time.UTC) }
	h.now = h.store.clock()
	if _, err := h.svc.AddMembers(t.Context(), owner, g, []string{cat}); err != nil {
		t.Fatal(err)
	}
	h.pub.events = nil
	h.svc.AttachmentScanned(t.Context(), up.ID, domain.AttachmentInfected)
	want := fmt.Sprintf("attachment %s %s infected to [%s %s]", up.ID, sent.Message.ID, ben, owner)
	if !slices.Equal(h.pub.events, []string{want}) {
		t.Errorf("sent: %v, want %s", h.pub.events, want)
	}
	h.pub.events = nil
	h.svc.AttachmentScanned(t.Context(), uid(999), domain.AttachmentClean)
	if len(h.pub.events) != 0 {
		t.Errorf("unknown file: %v", h.pub.events)
	}
}

// FR-16.9, NFR-14: the report lists files with their status, a deleted message's too, and an
// administrator downloads a clean one with an audit row; a refused download writes none.
func TestReportedAttachments(t *testing.T) {
	h := newHarness()
	objects := h.withFiles()
	id, _, _ := chat(t, h)
	up := h.upload(t, "bob", id, "proof.pdf")
	sent, err := h.svc.SendWithAttachments(t.Context(), "bob", id, nil, h.clientID(), []string{up.ID})
	if err != nil {
		t.Fatal(err)
	}
	r, err := h.svc.Report(t.Context(), "ada", id, domain.ReportInput{Reason: domain.ReasonSpam})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Delete(t.Context(), "bob", id, sent.Message.ID); err != nil {
		t.Fatal(err)
	}
	d, err := h.svc.ReportDetail(t.Context(), "admin", r.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var files []domain.Attachment
	for _, m := range d.Messages {
		if m.ID == sent.Message.ID {
			files = m.Attachments
		} else if m.Attachments == nil {
			t.Errorf("message %s: nil attachments", m.ID)
		}
	}
	if len(files) != 1 || files[0].ID != up.ID || files[0].Status != domain.AttachmentPending {
		t.Fatalf("files = %+v", files)
	}
	if _, err := h.svc.DownloadReportedAttachment(t.Context(), "admin", r.ID, up.ID); !errors.Is(err, domain.ErrScanPending) || len(h.store.downloads) != 0 {
		t.Errorf("pending: %v, downloads %v", err, h.store.downloads)
	}
	h.setStatus(up.ID, domain.AttachmentClean)
	if dl, err := h.svc.DownloadReportedAttachment(t.Context(), "admin", r.ID, up.ID); err != nil || dl.URL == nil {
		t.Fatal(err)
	}
	if want := r.ID + "|" + up.ID + "|admin"; !slices.Equal(h.store.downloads, []string{want}) || len(objects.presign) != 1 {
		t.Errorf("downloads = %v, presign %v", h.store.downloads, objects.presign)
	}
	if _, err := h.svc.DownloadReportedAttachment(t.Context(), "admin", uid(998), up.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown report: %v", err)
	}
	// A file of another conversation is out of the report's reach.
	h.connect("ada", "cy")
	other := h.direct(t, "ada", "cy", "hi").Message.ConversationID
	o := h.upload(t, "ada", other, "o.pdf")
	if _, err := h.svc.SendWithAttachments(t.Context(), "ada", other, nil, h.clientID(), []string{o.ID}); err != nil {
		t.Fatal(err)
	}
	h.setStatus(o.ID, domain.AttachmentClean)
	if _, err := h.svc.DownloadReportedAttachment(t.Context(), "admin", r.ID, o.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("other conversation: %v", err)
	}
	if len(h.store.downloads) != 1 {
		t.Errorf("downloads = %v", h.store.downloads)
	}
}

// FR-16.7: files not sent within a day are deleted; sent ones stay.
func TestPurgeExpiredAttachments(t *testing.T) {
	h := newHarness()
	h.withFiles()
	id, _, _ := chat(t, h)
	keep := h.upload(t, "ada", id, "keep.pdf")
	if _, err := h.svc.SendWithAttachments(t.Context(), "ada", id, nil, h.clientID(), []string{keep.ID}); err != nil {
		t.Fatal(err)
	}
	stale, fresh := h.upload(t, "ada", id, "stale.pdf"), h.upload(t, "ada", id, "fresh.pdf")
	a := h.store.attachments[stale.ID]
	a.ExpiresAt = h.store.clock().Add(-time.Second)
	h.store.attachments[stale.ID] = a
	n, err := h.svc.PurgeExpiredAttachments(t.Context())
	if err != nil || n != 1 {
		t.Fatalf("purged %d, %v", n, err)
	}
	if _, ok := h.store.attachments[stale.ID]; ok {
		t.Error("stale attachment kept")
	}
	for _, id := range []string{keep.ID, fresh.ID} {
		if _, ok := h.store.attachments[id]; !ok {
			t.Errorf("%s deleted", id)
		}
	}
}

func TestSanitizeFileName(t *testing.T) {
	pdfExt := []string{".pdf"}
	jpg := []string{".jpg", ".jpeg"}
	long := strings.Repeat("ä", 250)
	for _, tc := range []struct {
		in   string
		ext  []string
		want string
	}{
		{"report.pdf", pdfExt, "report.pdf"},
		{"REPORT.PDF", pdfExt, "REPORT.pdf"},
		{"../../etc/passwd", pdfExt, "passwd.pdf"},
		{`C:\Users\x\notes.pdf`, pdfExt, "notes.pdf"},
		{"invoice.exe", pdfExt, "invoice.exe.pdf"},
		{"photo.jpeg", jpg, "photo.jpeg"},
		{"photo.webp", []string{".png"}, "photo.webp.png"},
		{"a\x00b\x1fc\u202Egnp.pdf", pdfExt, "abcgnp.pdf"},
		{`"quoted" 'name'.pdf`, pdfExt, "quoted name.pdf"},
		{"tab\there.pdf", pdfExt, "tab here.pdf"},
		{"  ..hidden..  ", pdfExt, "hidden.pdf"},
		{"", pdfExt, "attachment.pdf"},
		{".pdf", pdfExt, "attachment.pdf"},
		{"/", pdfExt, "attachment.pdf"},
		{"bad\xffutf8.pdf", pdfExt, "badutf8.pdf"},
		{long + ".pdf", pdfExt, strings.Repeat("ä", 196) + ".pdf"},
	} {
		got := SanitizeFileName(tc.in, tc.ext)
		if got != tc.want {
			t.Errorf("SanitizeFileName(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if n := len([]rune(got)); n > MaxFileNameLength {
			t.Errorf("%q: %d characters", got, n)
		}
	}
}
