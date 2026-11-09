package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// InsertAttachment writes the stored file and the attachment row in one statement. The IDs
// come from the usecase, so a value that is not a UUID is a programming error.
func (s *Store) InsertAttachment(ctx context.Context, a domain.NewAttachment) error {
	id, ok1 := parseUUID(a.ID)
	uploader, ok2 := parseUUID(a.UploaderID)
	conv, ok3 := parseUUID(a.ConversationID)
	if !ok1 || !ok2 || !ok3 {
		return fmt.Errorf("insert attachment: %q, %q or %q is not a UUID", a.ID, a.UploaderID, a.ConversationID)
	}
	arg := sqlcgen.InsertAttachmentParams{
		ID: id, UploaderID: uploader, ConversationID: conv, ObjectKey: a.ObjectKey,
		ContentType: a.ContentType, SizeBytes: a.Size, Name: a.Name, ExpiresAt: timestamptz(a.ExpiresAt),
	}
	if a.Width > 0 && a.Height > 0 {
		arg.Width = pgtype.Int4{Int32: int32(min(a.Width, 1<<30)), Valid: true}   //nolint:gosec // bounded
		arg.Height = pgtype.Int4{Int32: int32(min(a.Height, 1<<30)), Valid: true} //nolint:gosec // bounded
	}
	if _, err := s.q(ctx).InsertAttachment(ctx, arg); err != nil {
		return fmt.Errorf("insert attachment: %w", err)
	}
	return nil
}

// ClaimAttachments takes nothing for IDs that are not UUIDs; the usecase checks them first.
func (s *Store) ClaimAttachments(ctx context.Context, ids []string, uploaderID, conversationID, messageID string) (int, error) {
	uploader, ok1 := parseUUID(uploaderID)
	conv, ok2 := parseUUID(conversationID)
	msg, ok3 := parseUUID(messageID)
	if !ok1 || !ok2 || !ok3 {
		return 0, nil
	}
	rows, err := s.q(ctx).ClaimAttachments(ctx, sqlcgen.ClaimAttachmentsParams{
		Ids: parseUUIDs(ids), UploaderID: uploader, ConversationID: conv, MessageID: msg,
	})
	if err != nil {
		return 0, fmt.Errorf("claim attachments: %w", err)
	}
	return len(rows), nil
}

// ListAttachments returns an empty map for a conversation that is not a UUID.
func (s *Store) ListAttachments(ctx context.Context, conversationID string, messageIDs []string) (map[string][]domain.Attachment, error) {
	conv, ok := parseUUID(conversationID)
	if !ok || len(messageIDs) == 0 {
		return map[string][]domain.Attachment{}, nil
	}
	rows, err := s.q(ctx).ListMessageAttachments(ctx, sqlcgen.ListMessageAttachmentsParams{ConversationID: conv, MessageIds: parseUUIDs(messageIDs)})
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	out := make(map[string][]domain.Attachment)
	for _, r := range rows {
		k := r.MessageID.String()
		out[k] = append(out[k], attachment(r.FileID, r.Name, r.SizeBytes, r.ContentType, r.ScanStatus))
	}
	return out, nil
}

// FindAttachment returns domain.ErrNotFound for IDs that are not UUIDs.
func (s *Store) FindAttachment(ctx context.Context, id, userID string) (domain.StoredAttachment, error) {
	fid, ok1 := parseUUID(id)
	user, ok2 := parseUUID(userID)
	if !ok1 || !ok2 {
		return domain.StoredAttachment{}, domain.ErrNotFound
	}
	r, err := s.q(ctx).FindAttachment(ctx, sqlcgen.FindAttachmentParams{ID: fid, UserID: user})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StoredAttachment{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.StoredAttachment{}, fmt.Errorf("find attachment: %w", err)
	}
	return domain.StoredAttachment{
		Attachment:     attachment(r.FileID, r.Name, r.SizeBytes, r.ContentType, r.ScanStatus),
		UploaderID:     r.UploaderID.String(),
		ConversationID: r.ConversationID.String(),
		ObjectKey:      r.ObjectKey,
		MessageID:      uuidPtr(r.MessageID),
		MessageAt:      timePtr(r.MessageCreatedAt),
		MessageDeleted: r.MessageDeleted,
		JoinedAt:       timePtr(r.MyJoinedAt),
	}, nil
}

// FindReportedAttachment returns domain.ErrNotFound for IDs that are not UUIDs.
func (s *Store) FindReportedAttachment(ctx context.Context, reportID, id string) (domain.StoredAttachment, error) {
	rid, ok1 := parseUUID(reportID)
	fid, ok2 := parseUUID(id)
	if !ok1 || !ok2 {
		return domain.StoredAttachment{}, domain.ErrNotFound
	}
	r, err := s.q(ctx).FindReportedAttachment(ctx, sqlcgen.FindReportedAttachmentParams{ReportID: rid, FileID: fid})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StoredAttachment{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.StoredAttachment{}, fmt.Errorf("find reported attachment: %w", err)
	}
	return domain.StoredAttachment{
		Attachment: domain.Attachment{ID: r.FileID.String(), Name: r.Name, ContentType: r.ContentType, Status: domain.AttachmentStatus(r.ScanStatus)},
		ObjectKey:  r.ObjectKey,
	}, nil
}

// ListReportedAttachments returns an empty map for a report that is not a UUID.
func (s *Store) ListReportedAttachments(ctx context.Context, reportID string, messageIDs []string) (map[string][]domain.Attachment, error) {
	rid, ok := parseUUID(reportID)
	if !ok || len(messageIDs) == 0 {
		return map[string][]domain.Attachment{}, nil
	}
	rows, err := s.q(ctx).ListReportedAttachments(ctx, sqlcgen.ListReportedAttachmentsParams{ReportID: rid, MessageIds: parseUUIDs(messageIDs)})
	if err != nil {
		return nil, fmt.Errorf("list reported attachments: %w", err)
	}
	out := make(map[string][]domain.Attachment)
	for _, r := range rows {
		k := r.MessageID.String()
		out[k] = append(out[k], attachment(r.FileID, r.Name, r.SizeBytes, r.ContentType, r.ScanStatus))
	}
	return out, nil
}

// AddAttachmentDownload needs IDs the usecase has already found.
func (s *Store) AddAttachmentDownload(ctx context.Context, reportID, fileID, adminID string) error {
	rid, ok1 := parseUUID(reportID)
	fid, ok2 := parseUUID(fileID)
	admin, ok3 := parseUUID(adminID)
	if !ok1 || !ok2 || !ok3 {
		return fmt.Errorf("record attachment download: %q, %q or %q is not a UUID", reportID, fileID, adminID)
	}
	if err := s.q(ctx).InsertAttachmentDownload(ctx, sqlcgen.InsertAttachmentDownloadParams{ReportID: rid, FileID: fid, AdminID: admin}); err != nil {
		return fmt.Errorf("record attachment download: %w", err)
	}
	return nil
}

// AttachmentAudience finds nothing for an ID that is not a UUID.
func (s *Store) AttachmentAudience(ctx context.Context, fileID string) (domain.AttachmentAudience, bool, error) {
	fid, ok := parseUUID(fileID)
	if !ok {
		return domain.AttachmentAudience{}, false, nil
	}
	r, err := s.q(ctx).FindAttachmentAudience(ctx, fid)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AttachmentAudience{}, false, nil
	}
	if err != nil {
		return domain.AttachmentAudience{}, false, fmt.Errorf("find attachment audience: %w", err)
	}
	return domain.AttachmentAudience{
		ConversationID: r.ConversationID.String(), MessageID: uuidPtr(r.MessageID), UploaderID: r.UploaderID.String(),
		MessageAt: timePtr(r.MessageCreatedAt), MessageDeleted: r.MessageDeleted,
	}, true, nil
}

// MembersSince finds nobody for a conversation that is not a UUID.
func (s *Store) MembersSince(ctx context.Context, conversationID string, at time.Time) ([]string, error) {
	conv, ok := parseUUID(conversationID)
	if !ok {
		return nil, nil
	}
	rows, err := s.q(ctx).MembersSince(ctx, sqlcgen.MembersSinceParams{ConversationID: conv, At: timestamptz(at)})
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return uuidStrings(rows), nil
}

// DeleteExpiredAttachments deletes one batch.
func (s *Store) DeleteExpiredAttachments(ctx context.Context, limit int) (int, error) {
	n, err := s.q(ctx).DeleteExpiredAttachments(ctx, int32(min(max(limit, 1), 1000))) //nolint:gosec // bounded
	if err != nil {
		return 0, fmt.Errorf("delete expired attachments: %w", err)
	}
	return int(n), nil
}

func attachment(id pgtype.UUID, name string, size int64, contentType, status string) domain.Attachment {
	return domain.Attachment{ID: id.String(), Name: name, Size: size, ContentType: contentType, Status: domain.AttachmentStatus(status)}
}
