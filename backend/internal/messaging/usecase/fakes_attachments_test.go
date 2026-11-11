package usecase

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"sort"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// fakeAttachment is a message_attachments row with its stored file.
type fakeAttachment struct {
	domain.NewAttachment
	status    domain.AttachmentStatus
	messageID string
	position  int
}

func (f *fakeStore) InsertAttachment(_ context.Context, a domain.NewAttachment) error {
	f.calls = append(f.calls, "insert attachment")
	if f.failAttach != nil {
		return f.failAttach
	}
	f.attachments[a.ID] = fakeAttachment{NewAttachment: a, status: domain.AttachmentPending}
	return nil
}

func (f *fakeStore) ClaimAttachments(_ context.Context, ids []string, uploaderID, conversationID, messageID string) (int, error) {
	n := 0
	for i, id := range ids {
		a, ok := f.attachments[id]
		if !ok || a.UploaderID != uploaderID || a.ConversationID != conversationID || a.messageID != "" ||
			!a.ExpiresAt.After(f.clock()) || (a.status != domain.AttachmentPending && a.status != domain.AttachmentClean) {
			continue
		}
		a.messageID, a.position = messageID, i
		f.attachments[id] = a
		n++
	}
	return n, nil
}

func (a fakeAttachment) view() domain.Attachment {
	return domain.Attachment{ID: a.ID, Name: a.Name, Size: a.Size, ContentType: a.ContentType, Status: a.status}
}

func (f *fakeStore) sortedAttachments(keep func(fakeAttachment) bool) map[string][]domain.Attachment {
	var rows []fakeAttachment
	for _, a := range f.attachments {
		if a.messageID != "" && keep(a) {
			rows = append(rows, a)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].position < rows[j].position })
	out := map[string][]domain.Attachment{}
	for _, a := range rows {
		out[a.messageID] = append(out[a.messageID], a.view())
	}
	return out
}

func (f *fakeStore) ListAttachments(_ context.Context, conversationID string, messageIDs []string) (map[string][]domain.Attachment, error) {
	return f.sortedAttachments(func(a fakeAttachment) bool {
		return a.ConversationID == conversationID && slices.Contains(messageIDs, a.messageID)
	}), nil
}

// sentMessage is the message an attachment was sent with; the zero message when none.
func (f *fakeStore) sentMessage(id string) domain.Message {
	if m := f.message(id); m != nil {
		return *m
	}
	return domain.Message{}
}

func (f *fakeStore) FindAttachment(_ context.Context, id, userID string) (domain.StoredAttachment, error) {
	a, ok := f.attachments[id]
	if !ok {
		return domain.StoredAttachment{}, domain.ErrNotFound
	}
	sa := domain.StoredAttachment{Attachment: a.view(), UploaderID: a.UploaderID, ConversationID: a.ConversationID, ObjectKey: a.ObjectKey}
	if a.messageID != "" {
		m := f.sentMessage(a.messageID)
		sa.MessageID, sa.MessageAt, sa.MessageDeleted = &a.messageID, &m.CreatedAt, m.Deleted()
	}
	if c := f.conversations[a.ConversationID]; c != nil && c.has(userID) {
		j := c.since(userID)
		sa.JoinedAt = &j
	}
	return sa, nil
}

func (f *fakeStore) report(id string) (domain.Report, bool) {
	for _, r := range f.reports {
		if r.ID == id {
			return r, true
		}
	}
	return domain.Report{}, false
}

func (f *fakeStore) FindReportedAttachment(_ context.Context, reportID, id string) (domain.StoredAttachment, error) {
	r, ok1 := f.report(reportID)
	a, ok2 := f.attachments[id]
	if !ok1 || !ok2 || a.messageID == "" || a.ConversationID != r.ConversationID {
		return domain.StoredAttachment{}, domain.ErrNotFound
	}
	if m := f.sentMessage(a.messageID); m.CreatedAt.After(r.CreatedAt) {
		return domain.StoredAttachment{}, domain.ErrNotFound
	}
	return domain.StoredAttachment{Attachment: a.view(), ObjectKey: a.ObjectKey}, nil
}

func (f *fakeStore) ListReportedAttachments(_ context.Context, reportID string, messageIDs []string) (map[string][]domain.Attachment, error) {
	r, ok := f.report(reportID)
	if !ok {
		return map[string][]domain.Attachment{}, nil
	}
	return f.sortedAttachments(func(a fakeAttachment) bool {
		m := f.sentMessage(a.messageID)
		return a.ConversationID == r.ConversationID && slices.Contains(messageIDs, a.messageID) && !m.CreatedAt.After(r.CreatedAt)
	}), nil
}

func (f *fakeStore) AddAttachmentDownload(_ context.Context, reportID, fileID, adminID string) error {
	f.downloads = append(f.downloads, reportID+"|"+fileID+"|"+adminID)
	return nil
}

func (f *fakeStore) AttachmentAudience(_ context.Context, fileID string) (domain.AttachmentAudience, bool, error) {
	a, ok := f.attachments[fileID]
	if !ok {
		return domain.AttachmentAudience{}, false, nil
	}
	au := domain.AttachmentAudience{ConversationID: a.ConversationID, UploaderID: a.UploaderID}
	if a.messageID != "" {
		m := f.sentMessage(a.messageID)
		id := a.messageID
		au.MessageID, au.MessageAt, au.MessageDeleted = &id, &m.CreatedAt, m.Deleted()
	}
	return au, true, nil
}

func (f *fakeStore) MembersSince(_ context.Context, conversationID string, at time.Time) ([]string, error) {
	c := f.conversations[conversationID]
	if c == nil {
		return nil, nil
	}
	if !c.group() {
		return []string{c.a, c.b}, nil
	}
	var out []string
	for _, m := range c.members {
		if !m.joined.After(at) {
			out = append(out, m.user)
		}
	}
	return out, nil
}

func (f *fakeStore) DeleteExpiredAttachments(_ context.Context, limit int) (int, error) {
	n := 0
	for id, a := range f.attachments {
		if n == limit {
			break
		}
		if a.messageID == "" && !a.ExpiresAt.After(f.clock()) {
			delete(f.attachments, id)
			n++
		}
	}
	return n, nil
}

func (f *fakePublisher) AttachmentUpdated(_ context.Context, u domain.AttachmentUpdate, to []string) {
	msg := "-"
	if u.MessageID != nil {
		msg = *u.MessageID
	}
	f.events = append(f.events, fmt.Sprintf("attachment %s %s %s to %v", u.AttachmentID, msg, u.Status, to))
}

// fakeIntake accepts anything that starts with "%PDF-" as a PDF and refuses the rest, or
// returns err when set.
type fakeIntake struct{ err error }

func (f fakeIntake) Prepare(_ context.Context, data []byte) (domain.PreparedFile, error) {
	if f.err != nil {
		return domain.PreparedFile{}, f.err
	}
	if len(data) < 5 || string(data[:5]) != "%PDF-" {
		return domain.PreparedFile{}, domain.ErrUnsupportedType
	}
	return domain.PreparedFile{Data: data, ContentType: "application/pdf", Extensions: []string{".pdf"}}, nil
}

// fakeObjects keeps object keys and the last presign's arguments.
type fakeObjects struct {
	keys     map[string]int64
	deleted  []string
	presign  []string // key|filename|contentType|expiry
	failPut  error
	failSign error
}

func newFakeObjects() *fakeObjects { return &fakeObjects{keys: map[string]int64{}} }

func (o *fakeObjects) Put(_ context.Context, key string, r io.Reader, size int64, _ string) error {
	if o.failPut != nil {
		return o.failPut
	}
	n, err := io.Copy(io.Discard, r)
	if err != nil || n != size {
		return errors.New("short put")
	}
	o.keys[key] = size
	return nil
}

func (o *fakeObjects) Delete(_ context.Context, key string) error {
	o.deleted = append(o.deleted, key)
	delete(o.keys, key)
	return nil
}

func (o *fakeObjects) PresignDownload(_ context.Context, key string, expiry time.Duration, filename, contentType string) (*url.URL, error) {
	if o.failSign != nil {
		return nil, o.failSign
	}
	o.presign = append(o.presign, fmt.Sprintf("%s|%s|%s|%s", key, filename, contentType, expiry))
	return &url.URL{Scheme: "https", Host: "bucket.example", Path: "/" + key}, nil
}
