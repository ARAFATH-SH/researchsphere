package http

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// Error codes of attachments (FR-16.7, docs/openapi.yaml).
const (
	CodeInvalidAttachment = "invalid_attachment"
	CodeFileTooLarge      = "file_too_large"
	CodeUnsupportedType   = "unsupported_type"
	CodeImageTooLarge     = "image_too_large"
	CodeInvalidImage      = "invalid_image"
	CodeScanPending       = "scan_pending"
	CodeScanFailed        = "scan_failed"
	CodeFileRemoved       = "file_removed"
)

// Headers of every attachment redirect: never cached (the URL lives 5 minutes), never sniffed.
const (
	attachmentCacheControl = "no-store"
	attachmentNoSniff      = "nosniff"
)

// formField is the multipart part that holds the file.
const formField = "file"

// AttachmentService is what the attachment handlers need from the usecase.
type AttachmentService interface {
	SendWithAttachments(ctx context.Context, senderID, conversationID string, body *string, clientID string, attachmentIDs []string) (domain.Sent, error)
	UploadAttachment(ctx context.Context, uploaderID, conversationID, name string, r io.Reader) (domain.Upload, error)
	DownloadAttachment(ctx context.Context, userID, id string) (usecase.Download, error)
	DownloadReportedAttachment(ctx context.Context, adminID, reportID, id string) (usecase.Download, error)
}

var attachmentNotFound = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This file does not exist."}

func invalidAttachment() gen.InvalidMessageJSONResponse {
	field := "attachmentIds"
	return gen.InvalidMessageJSONResponse{Code: CodeInvalidAttachment, Message: "One of the files cannot be sent. Upload it again.", Field: &field}
}

// UploadAttachment stores a file to send in the conversation (FR-16.7).
func (h *Handler) UploadAttachment(ctx context.Context, req gen.UploadAttachmentRequestObject) (gen.UploadAttachmentResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.UploadAttachment401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.UploadAttachment400JSONResponse{InvalidImageJSONResponse: badMultipart()}, nil
	}
	part, err := filePart(req.Body)
	if err != nil {
		return uploadReadFailure(err), nil
	}
	r := &trackingReader{r: part}
	up, err := h.svc.UploadAttachment(ctx, id, req.ConversationId, part.FileName(), r)
	if r.err != nil && !errors.Is(r.err, io.EOF) {
		// The body itself failed: over the cap set in cmd/api, or not valid multipart.
		return uploadReadFailure(r.err), nil
	}
	if res := uploadRefusal(err); res != nil {
		return res, nil
	}
	if err != nil {
		return nil, err
	}
	fid, err := uuid(up.ID)
	if err != nil {
		return nil, err
	}
	return gen.UploadAttachment201JSONResponse{
		Id: fid, Name: up.Name, Size: up.Size, ContentType: up.ContentType,
		Status: gen.AttachmentStatus(up.Status), ExpiresAt: up.ExpiresAt,
	}, nil
}

// uploadRefusal answers the usecase's refusals of an upload; nil for success and for an
// unexpected error.
func uploadRefusal(err error) gen.UploadAttachmentResponseObject {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrFileTooLarge):
		return gen.UploadAttachment413JSONResponse{FileTooLargeJSONResponse: gen.FileTooLargeJSONResponse{Code: CodeFileTooLarge, Message: "A file may be at most 25 MB."}}
	case errors.Is(err, domain.ErrUnsupportedType):
		return gen.UploadAttachment415JSONResponse{UnsupportedAttachmentTypeJSONResponse: gen.UnsupportedAttachmentTypeJSONResponse{
			Code: CodeUnsupportedType, Message: "Attach a PDF, an image (PNG, JPEG, WebP) or a Word, Excel or PowerPoint file without macros.",
		}}
	case errors.Is(err, domain.ErrImageTooLarge):
		return gen.UploadAttachment400JSONResponse{InvalidImageJSONResponse: gen.InvalidImageJSONResponse{Code: CodeImageTooLarge, Message: "The image may be at most 8000 pixels wide and high."}}
	case errors.Is(err, domain.ErrInvalidImage):
		return gen.UploadAttachment400JSONResponse{InvalidImageJSONResponse: gen.InvalidImageJSONResponse{Code: CodeInvalidImage, Message: "The image could not be read. It may be damaged."}}
	}
	r := refusalOf(err)
	switch {
	case r == nil:
		return nil
	case r.forbidden != nil:
		return gen.UploadAttachment403JSONResponse{MessageForbiddenJSONResponse: *r.forbidden}
	case r.notFound:
		return gen.UploadAttachment404JSONResponse{NotFoundJSONResponse: notFound}
	case r.conflict != nil:
		return gen.UploadAttachment409JSONResponse{MessageRefusedJSONResponse: *r.conflict}
	case r.limited:
		return gen.UploadAttachment429JSONResponse{AttachmentRateLimitedJSONResponse: gen.AttachmentRateLimitedJSONResponse{
			Body:    gen.Error{Code: CodeRateLimited, Message: "You have uploaded too many files. Try again later."},
			Headers: gen.AttachmentRateLimitedResponseHeaders{RetryAfter: r.retryAfter},
		}}
	}
	return nil
}

func badMultipart() gen.InvalidImageJSONResponse {
	return gen.InvalidImageJSONResponse{Code: httpx.CodeBadRequest, Message: "Send the file as multipart form data in a part named file."}
}

// uploadReadFailure turns an error reading the body into a response: 413 over the body cap,
// 400 otherwise (no file part, or not valid multipart).
func uploadReadFailure(err error) gen.UploadAttachmentResponseObject {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return gen.UploadAttachment413JSONResponse{FileTooLargeJSONResponse: gen.FileTooLargeJSONResponse{Code: CodeFileTooLarge, Message: "A file may be at most 25 MB."}}
	}
	return gen.UploadAttachment400JSONResponse{InvalidImageJSONResponse: badMultipart()}
}

// filePart returns the part named formField, skipping any other parts.
func filePart(body *multipart.Reader) (*multipart.Part, error) {
	for {
		p, err := body.NextPart()
		if err != nil {
			return nil, err
		}
		if p.FormName() == formField {
			return p, nil
		}
	}
}

// trackingReader remembers the first error its reader returned.
type trackingReader struct {
	r   io.Reader
	err error
}

func (t *trackingReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if err != nil && t.err == nil {
		t.err = err
	}
	return n, err
}

// GetAttachment redirects to a presigned download of the file when the caller may have it
// (FR-16.7, NFR-13, NFR-14).
func (h *Handler) GetAttachment(ctx context.Context, req gen.GetAttachmentRequestObject) (gen.GetAttachmentResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.GetAttachment401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	d, err := h.svc.DownloadAttachment(ctx, id, req.AttachmentId)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.GetAttachment404JSONResponse{NotFoundJSONResponse: attachmentNotFound}, nil
	case errors.Is(err, domain.ErrScanPending), errors.Is(err, domain.ErrScanFailed):
		return gen.GetAttachment409JSONResponse{AttachmentNotReadyJSONResponse: notReady(err)}, nil
	case errors.Is(err, domain.ErrFileRemoved):
		return gen.GetAttachment410JSONResponse{AttachmentRemovedJSONResponse: removed()}, nil
	case err != nil:
		return nil, err
	}
	return gen.GetAttachment302Response(redirect(d)), nil
}

// GetReportedAttachment is GetAttachment for an administrator reviewing a report (NFR-14).
func (h *Handler) GetReportedAttachment(ctx context.Context, req gen.GetReportedAttachmentRequestObject) (gen.GetReportedAttachmentResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.GetReportedAttachment401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	d, err := h.svc.DownloadReportedAttachment(ctx, id, req.ReportId, req.AttachmentId)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.GetReportedAttachment404JSONResponse{NotFoundJSONResponse: attachmentNotFound}, nil
	case errors.Is(err, domain.ErrScanPending), errors.Is(err, domain.ErrScanFailed):
		return gen.GetReportedAttachment409JSONResponse{AttachmentNotReadyJSONResponse: notReady(err)}, nil
	case errors.Is(err, domain.ErrFileRemoved):
		return gen.GetReportedAttachment410JSONResponse{AttachmentRemovedJSONResponse: removed()}, nil
	case err != nil:
		return nil, err
	}
	return gen.GetReportedAttachment302Response(redirect(d)), nil
}

func redirect(d usecase.Download) gen.AttachmentRedirectResponse {
	return gen.AttachmentRedirectResponse{Headers: gen.AttachmentRedirectResponseHeaders{
		Location: d.URL.String(), CacheControl: attachmentCacheControl, XContentTypeOptions: attachmentNoSniff,
	}}
}

func notReady(err error) gen.AttachmentNotReadyJSONResponse {
	if errors.Is(err, domain.ErrScanFailed) {
		return gen.AttachmentNotReadyJSONResponse{Code: CodeScanFailed, Message: "This file could not be checked for malware, so it cannot be downloaded."}
	}
	return gen.AttachmentNotReadyJSONResponse{Code: CodeScanPending, Message: "This file is still being checked for malware. Try again in a moment."}
}

func removed() gen.AttachmentRemovedJSONResponse {
	return gen.AttachmentRemovedJSONResponse{Code: CodeFileRemoved, Message: "This file was removed because it contained malware."}
}

// AttachmentBody is an attachment as the API shows it.
func AttachmentBody(a domain.Attachment) (gen.Attachment, error) {
	id, err := uuid(a.ID)
	if err != nil {
		return gen.Attachment{}, err
	}
	return gen.Attachment{Id: id, Name: a.Name, Size: a.Size, ContentType: a.ContentType, Status: gen.AttachmentStatus(a.Status)}, nil
}

func attachmentsBody(as []domain.Attachment) ([]gen.Attachment, error) {
	out := make([]gen.Attachment, 0, len(as))
	for _, a := range as {
		b, err := AttachmentBody(a)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

func attachmentIDs(ids *[]openapi_types.UUID) []string {
	if ids == nil {
		return nil
	}
	out := make([]string, len(*ids))
	for i, id := range *ids {
		out[i] = id.String()
	}
	return out
}
