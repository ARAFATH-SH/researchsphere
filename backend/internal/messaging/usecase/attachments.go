package usecase

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// Rules of message attachments (FR-16.7, NFR-13, docs/decisions.md 2026-11-09).
const (
	// MaxAttachmentBytes is the largest file, as uploaded and as stored.
	MaxAttachmentBytes = 25 << 20
	// MaxAttachmentsPerMessage is how many files one message may carry.
	MaxAttachmentsPerMessage = 5
	// MaxFileNameLength is the longest display name, in characters.
	MaxFileNameLength = 200
	// AttachmentLifetime is how long an uploaded file waits to be sent before it is deleted.
	AttachmentLifetime = 24 * time.Hour
	// UploadsPerHour and UploadBytesPerDay limit a user's uploads; refused files do not count.
	UploadsPerHour    = 20
	UploadBytesPerDay = 200 << 20
	// DownloadExpiry is how long a presigned download URL works.
	DownloadExpiry = 5 * time.Minute
	// purgeBatch is how many expired attachments one statement deletes.
	purgeBatch = 100
)

// FileDeps are what attachments need beyond the rest of the Service: the type check (media
// module) and the bucket.
type FileDeps struct {
	Intake  domain.FileIntake
	Objects domain.ObjectStore
}

// UseFiles gives the Service what attachments need. Until it is called, uploads and downloads
// fail; everything else works.
func (s *Service) UseFiles(d FileDeps) {
	s.files = d
	s.uploads = []window{{"attachments:hour", UploadsPerHour, time.Hour}}
	s.uploadBytes = []window{{"attachment-bytes:day", UploadBytesPerDay, day}}
}

// normalizeWithAttachments checks the body of a send: as NormalizeBody, except that with
// attachments a missing or blank body means no text.
func normalizeWithAttachments(body *string, withAttachments bool) (*string, error) {
	if withAttachments && (body == nil || strings.TrimSpace(*body) == "") {
		return nil, nil
	}
	if body == nil {
		return nil, domain.ErrInvalidBody
	}
	text, err := NormalizeBody(*body)
	if err != nil {
		return nil, err
	}
	return &text, nil
}

// checkAttachmentIDs refuses more than MaxAttachmentsPerMessage, a repeated ID and one that is
// not a UUID.
func checkAttachmentIDs(ids []string) error {
	if len(ids) > MaxAttachmentsPerMessage {
		return domain.ErrInvalidAttachment
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		k := strings.ToLower(id)
		if !isUUID(id) || seen[k] {
			return domain.ErrInvalidAttachment
		}
		seen[k] = true
	}
	return nil
}

// withAttachments fills the attachments of the messages that carry some and are not deleted;
// every other message gets an empty list.
func (s *Service) withAttachments(ctx context.Context, conversationID string, msgs []*domain.Message) error {
	var ids []string
	for _, m := range msgs {
		m.Attachments = []domain.Attachment{}
		if m.HasAttachments && !m.Deleted() {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	byMessage, err := s.store.ListAttachments(ctx, conversationID, ids)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		if a := byMessage[m.ID]; len(a) > 0 {
			m.Attachments = a
		}
	}
	return nil
}

// UploadAttachment stores a file for uploaderID to send in the conversation later (FR-16.7).
// Checks run in this order: ErrNotFound when the uploader is not a member (NFR-13), the
// standing (see refuseSender), for a direct conversation ErrNotFound when the other member is
// suspended or not verified, a block (see refuseBlocked) and the state (ErrRequestPending,
// ErrClosed, as a send would get them, without activating a request), then the upload limits
// (*RateLimitedError), then the file: ErrFileTooLarge, *RateLimitedError when it would pass the
// daily bytes, and the type check (ErrUnsupportedType, ErrImageTooLarge, ErrInvalidImage).
// These checks run again when the file is sent, in the send's transaction; here they only
// keep a file that could not be sent from being stored. The name is sanitized for display; the
// object key never uses it. A refused file does not count towards the limits.
func (s *Service) UploadAttachment(ctx context.Context, uploaderID, conversationID, name string, r io.Reader) (domain.Upload, error) {
	if s.files.Intake == nil || s.files.Objects == nil {
		return domain.Upload{}, errors.New("upload attachment: no file storage configured")
	}
	c, err := s.store.FindConversation(ctx, conversationID, uploaderID)
	if err != nil {
		return domain.Upload{}, err
	}
	if err := s.refuseSender(ctx, uploaderID); err != nil {
		return domain.Upload{}, err
	}
	if !c.IsGroup() {
		if err := s.admitUpload(ctx, c, uploaderID); err != nil {
			return domain.Upload{}, err
		}
	}
	if err := s.checkLimits(ctx, uploaderID, s.uploads, 1); err != nil {
		return domain.Upload{}, err
	}
	if err := s.checkLimits(ctx, uploaderID, s.uploadBytes, 1); err != nil {
		return domain.Upload{}, err
	}

	data, err := io.ReadAll(io.LimitReader(r, MaxAttachmentBytes+1))
	if err != nil {
		return domain.Upload{}, fmt.Errorf("read upload: %w", err)
	}
	if len(data) > MaxAttachmentBytes {
		return domain.Upload{}, domain.ErrFileTooLarge
	}
	if len(data) == 0 {
		return domain.Upload{}, domain.ErrUnsupportedType
	}
	if err := s.checkLimits(ctx, uploaderID, s.uploadBytes, len(data)); err != nil {
		return domain.Upload{}, err
	}
	f, err := s.files.Intake.Prepare(ctx, data)
	if err != nil {
		return domain.Upload{}, err
	}
	if len(f.Data) > MaxAttachmentBytes {
		return domain.Upload{}, domain.ErrFileTooLarge
	}

	id, err := newUUID()
	if err != nil {
		return domain.Upload{}, err
	}
	key := fmt.Sprintf("conversations/%s/attachments/%s", c.ID, id)
	if err := s.files.Objects.Put(ctx, key, bytes.NewReader(f.Data), int64(len(f.Data)), f.ContentType); err != nil {
		return domain.Upload{}, fmt.Errorf("store attachment: %w", err)
	}
	a := domain.NewAttachment{
		ID: id, UploaderID: uploaderID, ConversationID: c.ID, Name: SanitizeFileName(name, f.Extensions),
		ObjectKey: key, ContentType: f.ContentType, Size: int64(len(f.Data)), Width: f.Width, Height: f.Height,
		ExpiresAt: s.now().Add(AttachmentLifetime),
	}
	if err := s.store.InsertAttachment(ctx, a); err != nil {
		// No row points at the object, so the cleanup queue does not know it.
		if derr := s.files.Objects.Delete(context.WithoutCancel(ctx), key); derr != nil {
			s.logger.ErrorContext(ctx, "orphaned object after a failed attachment upload",
				slog.String("object_key", key), slog.Any("error", derr))
		}
		return domain.Upload{}, err
	}
	s.record(ctx, uploaderID, s.uploads, 1)
	s.record(ctx, uploaderID, s.uploadBytes, len(f.Data))
	return domain.Upload{
		Attachment: domain.Attachment{ID: id, Name: a.Name, Size: a.Size, ContentType: a.ContentType, Status: domain.AttachmentPending},
		ExpiresAt:  a.ExpiresAt,
	}, nil
}

// admitUpload is what a send in a direct conversation checks of the other member, a block and
// the state, without activating a request.
func (s *Service) admitUpload(ctx context.Context, c domain.Conversation, uploaderID string) error {
	visible, err := s.store.UserVisible(ctx, c.OtherID)
	if err != nil {
		return err
	}
	if !visible {
		return domain.ErrNotFound
	}
	if err := s.refuseBlocked(ctx, uploaderID, c.OtherID, true); err != nil {
		return err
	}
	switch c.State {
	case domain.StateDeclined:
		return domain.ErrClosed
	case domain.StateRequest:
		connected, err := s.connections.Connected(ctx, uploaderID, c.OtherID)
		if err != nil {
			return err
		}
		if !connected {
			return domain.ErrRequestPending
		}
	}
	return nil
}

// Download is a presigned URL to an attachment.
type Download struct {
	URL *url.URL
}

// DownloadAttachment returns a URL that downloads the attachment, valid for DownloadExpiry
// (NFR-13). Who: a current member who can see the message (joined at or before it was sent,
// NFR-13), when it is not deleted; or the uploader of a file not sent yet, while a member.
// Everyone else, and an unknown ID, gets ErrNotFound. Then the scan: ErrScanPending,
// ErrScanFailed or ErrFileRemoved unless clean.
func (s *Service) DownloadAttachment(ctx context.Context, userID, id string) (Download, error) {
	a, err := s.store.FindAttachment(ctx, id, userID)
	if err != nil {
		return Download{}, err
	}
	if !mayDownload(a, userID) {
		return Download{}, domain.ErrNotFound
	}
	return s.presign(ctx, a)
}

func mayDownload(a domain.StoredAttachment, userID string) bool {
	if a.JoinedAt == nil {
		return false
	}
	if a.MessageID == nil {
		return a.UploaderID == userID
	}
	return !a.MessageDeleted && a.MessageAt != nil && !a.MessageAt.Before(*a.JoinedAt)
}

func (s *Service) presign(ctx context.Context, a domain.StoredAttachment) (Download, error) {
	if err := statusError(a.Status); err != nil {
		return Download{}, err
	}
	if s.files.Objects == nil {
		return Download{}, errors.New("download attachment: no file storage configured")
	}
	u, err := s.files.Objects.PresignDownload(ctx, a.ObjectKey, DownloadExpiry, a.Name, a.ContentType)
	if err != nil {
		return Download{}, fmt.Errorf("presign attachment: %w", err)
	}
	return Download{URL: u}, nil
}

func statusError(st domain.AttachmentStatus) error {
	switch st {
	case domain.AttachmentClean:
		return nil
	case domain.AttachmentPending:
		return domain.ErrScanPending
	case domain.AttachmentInfected:
		return domain.ErrFileRemoved
	}
	return domain.ErrScanFailed
}

// DownloadReportedAttachment is DownloadAttachment for an administrator reviewing a report
// (NFR-13): only an attachment of a message of the reported conversation sent by the report's
// moment, also of one deleted later. The download is recorded in the transaction of the check;
// a refused one is not.
func (s *Service) DownloadReportedAttachment(ctx context.Context, adminID, reportID, id string) (Download, error) {
	var d Download
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		a, err := s.store.FindReportedAttachment(ctx, reportID, id)
		if err != nil {
			return err
		}
		if err := statusError(a.Status); err != nil {
			return err
		}
		if err := s.store.AddAttachmentDownload(ctx, reportID, a.ID, adminID); err != nil {
			return err
		}
		d, err = s.presign(ctx, a)
		return err
	})
	if err != nil {
		return Download{}, err
	}
	return d, nil
}

// AttachmentScanned tells who can see the attachment that its scan finished: the uploader's
// tabs, and once it is sent, the current members who joined by the message. Nobody hears about
// a deleted message's file but its uploader. It cannot fail the scan: errors are logged.
func (s *Service) AttachmentScanned(ctx context.Context, fileID string, status domain.AttachmentStatus) {
	a, found, err := s.store.AttachmentAudience(ctx, fileID)
	if err != nil {
		s.logger.ErrorContext(ctx, "attachment scan event", slog.Any("error", err))
		return
	}
	if !found {
		return
	}
	to := []string{a.UploaderID}
	if a.MessageID != nil && !a.MessageDeleted && a.MessageAt != nil {
		members, err := s.store.MembersSince(ctx, a.ConversationID, *a.MessageAt)
		if err != nil {
			s.logger.ErrorContext(ctx, "attachment scan event", slog.Any("error", err))
			return
		}
		for _, m := range members {
			if !slices.Contains(to, m) {
				to = append(to, m)
			}
		}
	}
	s.publisher.AttachmentUpdated(ctx, domain.AttachmentUpdate{
		ConversationID: a.ConversationID, MessageID: a.MessageID, AttachmentID: fileID, Status: status,
	}, to)
}

// AttachmentConversation returns the conversation an attachment was uploaded to; found is false
// when it is gone.
func (s *Service) AttachmentConversation(ctx context.Context, fileID string) (string, bool, error) {
	a, found, err := s.store.AttachmentAudience(ctx, fileID)
	if err != nil || !found {
		return "", false, err
	}
	return a.ConversationID, true, nil
}

// PurgeExpiredAttachments deletes the attachments not sent within AttachmentLifetime, batch by
// batch; their stored files and objects follow through the database's queue. It returns how
// many it deleted.
func (s *Service) PurgeExpiredAttachments(ctx context.Context) (int, error) {
	total := 0
	for {
		n, err := s.store.DeleteExpiredAttachments(ctx, purgeBatch)
		total += n
		if err != nil {
			return total, err
		}
		if n < purgeBatch {
			return total, nil
		}
	}
}

// RunAttachmentPurge calls PurgeExpiredAttachments now and then every interval until ctx is
// cancelled. Errors are logged; the next pass tries again.
func (s *Service) RunAttachmentPurge(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		n, err := s.PurgeExpiredAttachments(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			s.logger.ErrorContext(ctx, "purge of unused attachments failed", slog.Any("error", err))
		case n > 0:
			s.logger.InfoContext(ctx, "deleted unused attachments", slog.Int("count", n))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// SanitizeFileName makes the name a client gave a file safe to show and to offer as a download
// name: only the last part of a path (either slash), without control or format characters
// (bidirectional overrides among them) or quotes, trimmed of spaces and dots, at most
// MaxFileNameLength characters with its extension kept, and ending with one of extensions (the
// type found from the content) so the saved file opens as what it is: "x.exe" holding a PDF
// becomes "x.exe.pdf". An empty result is "attachment" with the extension.
func SanitizeFileName(name string, extensions []string) string {
	if !utf8.ValidString(name) {
		name = strings.ToValidUTF8(name, "")
	}
	name = strings.ReplaceAll(name, `\`, "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.Is(unicode.Cc, r), unicode.Is(unicode.Cf, r), r == '"', r == '\'', r == '`':
			return -1
		}
		return r
	}, name)
	name = strings.TrimLeft(strings.TrimRight(name, " ."), " ")

	ext := ""
	if len(extensions) > 0 {
		ext = extensions[0]
		if cur := strings.ToLower(path.Ext(name)); slices.Contains(extensions, cur) {
			ext = cur
			name = name[:len(name)-len(cur)]
		}
	}
	name = strings.Trim(name, " .")
	if name == "" {
		name = "attachment"
	}
	if keep := MaxFileNameLength - utf8.RuneCountInString(ext); utf8.RuneCountInString(name) > keep {
		name = strings.TrimRight(string([]rune(name)[:keep]), " .")
	}
	return name + ext
}

// newUUID returns a random (version 4) UUID.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("new ID: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
