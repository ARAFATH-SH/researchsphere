package main

import (
	"context"
	"errors"
	"io"
	"log/slog"

	mediadomain "github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
	mediausecase "github.com/ARAFATH-SH/researchsphere/backend/internal/media/usecase"
	messagingdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	messagingusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/usecase"
	notificationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/storage"
)

// Message attachments (FR-16.7) join three modules: messaging decides who may upload, send and
// download; media checks the type and runs the malware scan; notification tells the uploader
// when a file is blocked. No module imports another; this file is where they meet.

// fileIntake gives messaging the media module's type check and image re-encoding.
type fileIntake struct {
	media *mediausecase.Attachments
}

var _ messagingdomain.FileIntake = fileIntake{}

func (f fileIntake) Prepare(ctx context.Context, data []byte) (messagingdomain.PreparedFile, error) {
	p, err := f.media.Prepare(ctx, data)
	switch {
	case errors.Is(err, mediadomain.ErrFileTooLarge):
		return messagingdomain.PreparedFile{}, messagingdomain.ErrFileTooLarge
	case errors.Is(err, mediadomain.ErrUnsupportedType):
		return messagingdomain.PreparedFile{}, messagingdomain.ErrUnsupportedType
	case errors.Is(err, mediadomain.ErrImageTooLarge):
		return messagingdomain.PreparedFile{}, messagingdomain.ErrImageTooLarge
	case errors.Is(err, mediadomain.ErrInvalidImage):
		return messagingdomain.PreparedFile{}, messagingdomain.ErrInvalidImage
	case err != nil:
		return messagingdomain.PreparedFile{}, err
	}
	return messagingdomain.PreparedFile{
		Data: p.Data, ContentType: p.ContentType, Extensions: p.Extensions, Width: p.Width, Height: p.Height,
	}, nil
}

// scanObjects gives the scan worker the bucket, with storage's "not found" in media's terms.
type scanObjects struct {
	files storage.Storage
}

var _ mediadomain.ObjectReader = scanObjects{}

func (o scanObjects) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	r, err := o.files.Get(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, mediadomain.ErrObjectMissing
	}
	return r, err
}

// scanListener tells messaging and notification what the scan found.
type scanListener struct {
	messaging     *messagingusecase.Service
	notifications *notificationusecase.Service
	logger        *slog.Logger
}

var _ mediadomain.ScanListener = scanListener{}

// Infected writes the uploader's notification in the scan's transaction. A file that is not an
// attachment, such as a project figure (FR-12.2, owned by its project), tells nobody: the
// figure shows as infected in the project's file list.
func (l scanListener) Infected(ctx context.Context, fileID, ownerID string) error {
	conversationID, found, err := l.messaging.AttachmentConversation(ctx, fileID)
	if err != nil || !found {
		return err
	}
	return l.notifications.AttachmentBlocked(ctx, conversationID, ownerID)
}

// Scanned sends attachment.updated; the publisher waits for the commit.
func (l scanListener) Scanned(ctx context.Context, fileID string, status mediadomain.ScanStatus) {
	l.messaging.AttachmentScanned(ctx, fileID, messagingdomain.AttachmentStatus(status))
}
