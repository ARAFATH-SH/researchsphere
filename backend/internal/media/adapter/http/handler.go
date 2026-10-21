// Package http serves the media operations in docs/openapi.yaml: uploading and removing
// the profile photo and cover image (FR-4.6), and reading a stored file through a
// presigned URL after a permission check (NFR-13). cmd/api guards each route with its
// platform action (SRS 2.3) and caps the request body before it gets here.
package http

import (
	"context"
	"errors"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// Error codes of the media operations (docs/openapi.yaml).
const (
	CodeFileTooLarge    = "file_too_large"
	CodeUnsupportedType = "unsupported_type"
	CodeImageTooLarge   = "image_too_large"
	CodeInvalidImage    = "invalid_image"
	CodeRateLimited     = "rate_limited"
)

// CacheControl is sent with every media redirect: the browser may reuse it for 5
// minutes, half the life of the presigned URL (docs/decisions.md).
const CacheControl = "private, max-age=300"

// formField is the multipart part that holds the image.
const formField = "file"

// service is the part of usecase.Images this handler needs.
type service interface {
	Upload(ctx context.Context, userID string, purpose domain.Purpose, r io.Reader) (string, error)
	Remove(ctx context.Context, userID string, purpose domain.Purpose) error
	FileURL(ctx context.Context, callerID, fileID string) (*url.URL, error)
}

// Handler implements the media operations of gen.StrictServerInterface.
type Handler struct {
	svc service
}

// NewHandler returns a Handler that calls svc.
func NewHandler(svc service) *Handler {
	return &Handler{svc: svc}
}

var unauthenticated = gen.UnauthorizedJSONResponse{Code: httpx.CodeUnauthenticated, Message: "Log in to continue."}

// uploadResult is what an upload gives the response: exactly one field is set.
type uploadResult struct {
	stored      *gen.ProfileImage
	badRequest  *gen.Error // 400
	noProfile   *gen.Error // 404
	tooLarge    *gen.Error // 413
	unsupported *gen.Error // 415
	limited     *gen.UploadRateLimitedJSONResponse
}

// upload reads the file part and runs the usecase. An error is unexpected (500).
func (h *Handler) upload(ctx context.Context, userID string, purpose domain.Purpose, body *multipart.Reader) (uploadResult, error) {
	part, err := filePart(body)
	if err != nil {
		return readFailure(err), nil
	}
	r := &trackingReader{r: part}
	fileID, err := h.svc.Upload(ctx, userID, purpose, r)
	if r.err != nil && !errors.Is(r.err, io.EOF) {
		// The body itself failed: over the cap set in cmd/api, or not valid multipart.
		return readFailure(r.err), nil
	}
	var limited *domain.RateLimitedError
	switch {
	case err == nil:
		return uploadResult{stored: &gen.ProfileImage{Url: httpx.MediaURL(fileID)}}, nil
	case errors.As(err, &limited):
		return uploadResult{limited: &gen.UploadRateLimitedJSONResponse{
			Body:    gen.Error{Code: CodeRateLimited, Message: "You have uploaded 5 images in the last hour. Try again later."},
			Headers: gen.UploadRateLimitedResponseHeaders{RetryAfter: retryAfter(limited)},
		}}, nil
	case errors.Is(err, domain.ErrFileTooLarge):
		return uploadResult{tooLarge: tooLarge(purpose)}, nil
	case errors.Is(err, domain.ErrUnsupportedType):
		return uploadResult{unsupported: &gen.Error{Code: CodeUnsupportedType, Message: "Choose a JPEG, PNG or WebP image."}}, nil
	case errors.Is(err, domain.ErrImageTooLarge):
		return uploadResult{badRequest: &gen.Error{Code: CodeImageTooLarge, Message: "The image may be at most 8000 pixels wide and high."}}, nil
	case errors.Is(err, domain.ErrInvalidImage):
		return uploadResult{badRequest: &gen.Error{Code: CodeInvalidImage, Message: "The image could not be read. It may be damaged."}}, nil
	case errors.Is(err, domain.ErrNoProfile):
		return uploadResult{noProfile: &gen.Error{Code: httpx.CodeNotFound, Message: "Fill in your profile before adding images."}}, nil
	}
	return uploadResult{}, err
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

// readFailure turns an error reading the body into a response: 413 over the body cap,
// 400 otherwise (no file part, or not valid multipart).
func readFailure(err error) uploadResult {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return uploadResult{tooLarge: &gen.Error{Code: CodeFileTooLarge, Message: "The file is too large."}}
	}
	return uploadResult{badRequest: &gen.Error{Code: httpx.CodeBadRequest, Message: "Send the image as multipart form data in a part named file."}}
}

func tooLarge(p domain.Purpose) *gen.Error {
	msg := "The photo may be at most 5 MB."
	if p == domain.PurposeCover {
		msg = "The cover image may be at most 8 MB."
	}
	return &gen.Error{Code: CodeFileTooLarge, Message: msg}
}

// retryAfter is the time left in whole seconds, rounded up, and at least 1.
func retryAfter(e *domain.RateLimitedError) int {
	return max(1, int(math.Ceil(e.RetryAfter.Seconds())))
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

// UploadMyPhoto stores the caller's profile photo (FR-4.6).
func (h *Handler) UploadMyPhoto(ctx context.Context, req gen.UploadMyPhotoRequestObject) (gen.UploadMyPhotoResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.UploadMyPhoto401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	res, err := h.upload(ctx, id, domain.PurposePhoto, req.Body)
	switch {
	case err != nil:
		return nil, err
	case res.stored != nil:
		return gen.UploadMyPhoto200JSONResponse{ProfileImageStoredJSONResponse: gen.ProfileImageStoredJSONResponse(*res.stored)}, nil
	case res.limited != nil:
		return gen.UploadMyPhoto429JSONResponse{UploadRateLimitedJSONResponse: *res.limited}, nil
	case res.tooLarge != nil:
		return gen.UploadMyPhoto413JSONResponse{FileTooLargeJSONResponse: gen.FileTooLargeJSONResponse(*res.tooLarge)}, nil
	case res.unsupported != nil:
		return gen.UploadMyPhoto415JSONResponse{UnsupportedImageTypeJSONResponse: gen.UnsupportedImageTypeJSONResponse(*res.unsupported)}, nil
	case res.noProfile != nil:
		return gen.UploadMyPhoto404JSONResponse{NoProfileJSONResponse: gen.NoProfileJSONResponse(*res.noProfile)}, nil
	}
	return gen.UploadMyPhoto400JSONResponse{InvalidImageJSONResponse: gen.InvalidImageJSONResponse(*res.badRequest)}, nil
}

// UploadMyCover stores the caller's cover image (FR-4.6).
func (h *Handler) UploadMyCover(ctx context.Context, req gen.UploadMyCoverRequestObject) (gen.UploadMyCoverResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.UploadMyCover401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	res, err := h.upload(ctx, id, domain.PurposeCover, req.Body)
	switch {
	case err != nil:
		return nil, err
	case res.stored != nil:
		return gen.UploadMyCover200JSONResponse{ProfileImageStoredJSONResponse: gen.ProfileImageStoredJSONResponse(*res.stored)}, nil
	case res.limited != nil:
		return gen.UploadMyCover429JSONResponse{UploadRateLimitedJSONResponse: *res.limited}, nil
	case res.tooLarge != nil:
		return gen.UploadMyCover413JSONResponse{FileTooLargeJSONResponse: gen.FileTooLargeJSONResponse(*res.tooLarge)}, nil
	case res.unsupported != nil:
		return gen.UploadMyCover415JSONResponse{UnsupportedImageTypeJSONResponse: gen.UnsupportedImageTypeJSONResponse(*res.unsupported)}, nil
	case res.noProfile != nil:
		return gen.UploadMyCover404JSONResponse{NoProfileJSONResponse: gen.NoProfileJSONResponse(*res.noProfile)}, nil
	}
	return gen.UploadMyCover400JSONResponse{InvalidImageJSONResponse: gen.InvalidImageJSONResponse(*res.badRequest)}, nil
}

// RemoveMyPhoto takes the photo off the caller's profile (FR-4.6).
func (h *Handler) RemoveMyPhoto(ctx context.Context, _ gen.RemoveMyPhotoRequestObject) (gen.RemoveMyPhotoResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RemoveMyPhoto401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if err := h.svc.Remove(ctx, id, domain.PurposePhoto); err != nil {
		return nil, err
	}
	return gen.RemoveMyPhoto204Response{}, nil
}

// RemoveMyCover takes the cover image off the caller's profile (FR-4.6).
func (h *Handler) RemoveMyCover(ctx context.Context, _ gen.RemoveMyCoverRequestObject) (gen.RemoveMyCoverResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RemoveMyCover401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if err := h.svc.Remove(ctx, id, domain.PurposeCover); err != nil {
		return nil, err
	}
	return gen.RemoveMyCover204Response{}, nil
}

// GetMedia redirects to a presigned URL of the file when the caller may see it (NFR-13).
func (h *Handler) GetMedia(ctx context.Context, req gen.GetMediaRequestObject) (gen.GetMediaResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.GetMedia401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	u, err := h.svc.FileURL(ctx, id, req.FileId)
	if errors.Is(err, domain.ErrNotFound) {
		return gen.GetMedia404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "The requested resource does not exist."}}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetMedia302Response{Headers: gen.GetMedia302ResponseHeaders{
		Location: u.String(), CacheControl: CacheControl,
	}}, nil
}
