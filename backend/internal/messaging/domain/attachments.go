package domain

import (
	"context"
	"errors"
	"io"
	"net/url"
	"time"
)

// Message attachments (FR-16.7, NFR-13, NFR-14; slice 14B, chunk 14B.3). A file is uploaded to a
// conversation first and sent with a message later. The media module checks its type and scans
// it; the messaging module decides who may upload, send and download it.

// AttachmentStatus is an attachment's malware scan outcome.
type AttachmentStatus string

// Scan outcomes. Only a clean file is ever served.
const (
	AttachmentPending  AttachmentStatus = "pending"
	AttachmentClean    AttachmentStatus = "clean"
	AttachmentInfected AttachmentStatus = "infected"
	AttachmentFailed   AttachmentStatus = "failed"
)

// Attachment is a file of a message, as its members see it.
type Attachment struct {
	ID          string
	Name        string // for display, sanitized
	Size        int64
	ContentType string
	Status      AttachmentStatus
}

// Upload is a file just uploaded, not sent yet.
type Upload struct {
	Attachment
	ExpiresAt time.Time
}

// PreparedFile is an upload the type check accepted, ready to store.
type PreparedFile struct {
	Data        []byte
	ContentType string
	Extensions  []string // file name endings of the type, the usual one first
	Width       int      // set for an image only
	Height      int
}

// FileIntake checks an uploaded file by its content and prepares it for storage. The media
// module owns the checks; cmd/api implements this with its usecase. Errors: ErrFileTooLarge,
// ErrUnsupportedType, ErrImageTooLarge, ErrInvalidImage.
type FileIntake interface {
	Prepare(ctx context.Context, data []byte) (PreparedFile, error)
}

// ObjectStore is the bucket, as attachments use it.
type ObjectStore interface {
	// Put stores size bytes from r under key with the given content type.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Delete removes the object. A key with no object is not an error.
	Delete(ctx context.Context, key string) error
	// PresignDownload returns a URL that downloads the object as a file named filename, with
	// the given content type, until expiry.
	PresignDownload(ctx context.Context, key string, expiry time.Duration, filename, contentType string) (*url.URL, error)
}

// NewAttachment is an uploaded file to record.
type NewAttachment struct {
	ID             string
	UploaderID     string
	ConversationID string
	Name           string
	ObjectKey      string
	ContentType    string
	Size           int64
	Width, Height  int // 0 unless an image
	ExpiresAt      time.Time
}

// StoredAttachment is an attachment with what decides who may download it.
type StoredAttachment struct {
	Attachment
	UploaderID     string
	ConversationID string
	ObjectKey      string
	MessageID      *string    // nil until sent
	MessageAt      *time.Time // the message's created_at, nil until sent
	MessageDeleted bool
	// JoinedAt is when the caller joined the conversation; nil when not a member now.
	JoinedAt *time.Time
}

// AttachmentAudience is who hears that an attachment's scan finished.
type AttachmentAudience struct {
	ConversationID string
	MessageID      *string // nil until sent
	UploaderID     string
	MessageAt      *time.Time
	MessageDeleted bool
}

// AttachmentUpdate is the attachment.updated event.
type AttachmentUpdate struct {
	ConversationID string
	MessageID      *string
	AttachmentID   string
	Status         AttachmentStatus
}

// AttachmentStore keeps the message_attachments rows and their stored files. Methods run in
// the transaction the context carries, if any. IDs that are not UUIDs name no row.
type AttachmentStore interface {
	// InsertAttachment records an uploaded file, pending its scan.
	InsertAttachment(ctx context.Context, a NewAttachment) error
	// ClaimAttachments ties the uploader's unused, unexpired attachments of the conversation
	// whose scan is pending or clean to the message, in the order of ids, and returns how many
	// it took. The caller rolls back unless that is len(ids).
	ClaimAttachments(ctx context.Context, ids []string, uploaderID, conversationID, messageID string) (int, error)
	// ListAttachments returns the attachments of the messages, in order, by message ID.
	ListAttachments(ctx context.Context, conversationID string, messageIDs []string) (map[string][]Attachment, error)
	// FindAttachment returns the attachment with the caller's membership, or ErrNotFound.
	FindAttachment(ctx context.Context, id, userID string) (StoredAttachment, error)
	// FindReportedAttachment returns an attachment of a message of the report's conversation
	// sent by the report's moment, or ErrNotFound (NFR-14).
	FindReportedAttachment(ctx context.Context, reportID, id string) (StoredAttachment, error)
	// ListReportedAttachments is ListAttachments for the messages of a report (NFR-14).
	ListReportedAttachments(ctx context.Context, reportID string, messageIDs []string) (map[string][]Attachment, error)
	// AddAttachmentDownload records an administrator's download through a report.
	AddAttachmentDownload(ctx context.Context, reportID, fileID, adminID string) error
	// AttachmentAudience returns where the attachment is; found is false when it is gone.
	AttachmentAudience(ctx context.Context, fileID string) (a AttachmentAudience, found bool, err error)
	// MembersSince returns the current members who joined at or before at.
	MembersSince(ctx context.Context, conversationID string, at time.Time) ([]string, error)
	// DeleteExpiredAttachments deletes up to limit unused attachments past their expiry, with
	// their stored files, and returns how many.
	DeleteExpiredAttachments(ctx context.Context, limit int) (int, error)
}

// Errors of attachments.
var (
	// ErrInvalidAttachment: an attachment ID of a send cannot be used (FR-16.7).
	ErrInvalidAttachment = errors.New("messaging: invalid attachment")
	// ErrFileTooLarge: the upload is over 25 MB.
	ErrFileTooLarge = errors.New("messaging: file too large")
	// ErrUnsupportedType: the file's content is not of an accepted type.
	ErrUnsupportedType = errors.New("messaging: unsupported file type")
	// ErrImageTooLarge: an image over the pixel limit.
	ErrImageTooLarge = errors.New("messaging: image dimensions too large")
	// ErrInvalidImage: an image whose content cannot be read.
	ErrInvalidImage = errors.New("messaging: invalid image")
	// ErrScanPending: the file has not been scanned yet.
	ErrScanPending = errors.New("messaging: the file is being scanned")
	// ErrScanFailed: the scanner could not read the file.
	ErrScanFailed = errors.New("messaging: the file could not be scanned")
	// ErrFileRemoved: malware was found and the file deleted.
	ErrFileRemoved = errors.New("messaging: the file was removed")
)
