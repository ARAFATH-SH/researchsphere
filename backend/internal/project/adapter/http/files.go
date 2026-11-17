package http

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/usecase"
)

// Error codes of the file operations (FR-12.1, FR-12.2, FR-12.6, docs/openapi.yaml).
const (
	CodeInvalidPath      = "invalid_path"
	CodeInvalidMove      = "invalid_move"
	CodePathTaken        = "path_taken"
	CodeParentNotFolder  = "parent_not_folder"
	CodeFileLimit        = "file_limit_reached"
	CodeNotTextFile      = "not_a_text_file"
	CodeVersionConflict  = "version_conflict"
	CodeFileTooLarge     = "file_too_large"
	CodeUnsupportedType  = "unsupported_type"
	CodeTypeMismatch     = "type_mismatch"
	CodeInvalidImage     = "invalid_image"
	CodeStorageLimit     = "storage_limit_reached"
	CodeUploadIncomplete = "upload_incomplete"
	CodeScanPending      = "scan_pending"
	CodeScanFailed       = "scan_failed"
	CodeFileRemoved      = "file_removed"
)

// FileService is what the file handlers need from the usecase.
type FileService interface {
	Files(ctx context.Context, userID, projectID string) (domain.FileTree, error)
	File(ctx context.Context, userID, projectID, fileID string) (usecase.FileContent, error)
	CreateFile(ctx context.Context, userID, projectID string, in usecase.CreateFileInput) (domain.File, error)
	MoveFile(ctx context.Context, userID, projectID, fileID, path string) (domain.File, error)
	DeleteFile(ctx context.Context, userID, projectID, fileID string) error
	SaveContent(ctx context.Context, userID, projectID, fileID, content string, version int) (domain.File, error)
	StartFigureUpload(ctx context.Context, userID, projectID, path string, size int64) (usecase.UploadTicket, error)
	CompleteFigureUpload(ctx context.Context, userID, projectID, uploadID string) (domain.File, error)
}

var (
	fileNotFound = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This file does not exist."}
	invalidPath  = gen.Error{
		Code: CodeInvalidPath,
		Message: "Use only letters, digits, '_', '-' and '.' in a path, with no spaces, and do not start a name with '.'; " +
			"at most 8 folders deep and 255 characters. Figures end in .png, .jpg, .jpeg or .pdf, and text files must not.",
		Field: ptr("path"),
	}
)

func ptr[T any](v T) *T { return &v }

// fileError is what each file operation answers for a usecase error; status 0 means err is
// not one of them.
type fileError struct {
	status int
	body   gen.Error
	susp   *domain.SuspendedError
	limit  *domain.RateLimitedError
	// size is the body of a 413 about a figure: the limit, and the storage in use.
	size *gen.StorageLimitError
}

func classifyFile(err error) fileError {
	var (
		field     *domain.FieldError
		suspended *domain.SuspendedError
		limited   *domain.RateLimitedError
		tooLarge  *domain.FigureTooLargeError
		full      *domain.StorageLimitError
	)
	switch {
	case errors.As(err, &tooLarge):
		return fileError{status: 413, size: &gen.StorageLimitError{
			Code: CodeFileTooLarge, Message: fmt.Sprintf("A figure may be at most %s.", formatBytes(tooLarge.LimitBytes)),
			LimitBytes: ptr(tooLarge.LimitBytes),
		}}
	case errors.As(err, &full):
		return fileError{status: 413, size: &gen.StorageLimitError{
			Code: CodeStorageLimit,
			Message: fmt.Sprintf("This project uses %s of %s, so a figure of %s does not fit. Delete figures it no longer needs to make room.",
				formatBytes(full.UsedBytes), formatBytes(full.LimitBytes), formatBytes(full.NeedBytes)),
			UsedBytes: ptr(full.UsedBytes), LimitBytes: ptr(full.LimitBytes),
		}}
	case errors.Is(err, domain.ErrUploadNotFound):
		return fileError{status: 404, body: gen.Error{Code: httpx.CodeNotFound, Message: "This upload does not exist or has expired. Upload the file again."}}
	case errors.Is(err, domain.ErrUploadIncomplete):
		return fileError{status: 409, body: gen.Error{Code: CodeUploadIncomplete, Message: "The file has not reached storage yet. Send it, then try again."}}
	case errors.Is(err, domain.ErrInvalidPath):
		return fileError{status: 400, body: invalidPath}
	case errors.As(err, &field):
		return fileError{status: 400, body: fieldError(field.Field)}
	case errors.Is(err, domain.ErrInvalidMove):
		return fileError{status: 400, body: gen.Error{Code: CodeInvalidMove, Message: "A folder cannot be moved into itself."}}
	case errors.Is(err, domain.ErrTypeMismatch):
		return fileError{status: 400, body: gen.Error{Code: CodeTypeMismatch,
			Message: "The file's content does not match its extension: use .png for a PNG, .jpg or .jpeg for a JPEG, .pdf for a PDF."}}
	case errors.Is(err, domain.ErrInvalidImage):
		return fileError{status: 400, body: gen.Error{Code: CodeInvalidImage, Message: "The image could not be read. It may be damaged."}}
	case errors.As(err, &suspended):
		return fileError{status: 403, susp: suspended}
	case errors.Is(err, domain.ErrForbidden):
		return fileError{status: 403, body: gen.Error(forbidden)}
	case errors.Is(err, domain.ErrNotFound):
		return fileError{status: 404, body: gen.Error(notFound)}
	case errors.Is(err, domain.ErrFileNotFound):
		return fileError{status: 404, body: gen.Error(fileNotFound)}
	case errors.Is(err, domain.ErrPathTaken):
		return fileError{status: 409, body: gen.Error{Code: CodePathTaken,
			Message: "A file or folder with this name already exists here. Names are compared ignoring letter case."}}
	case errors.Is(err, domain.ErrParentNotFolder):
		return fileError{status: 409, body: gen.Error{Code: CodeParentNotFolder, Message: "Part of this path is a file, not a folder."}}
	case errors.Is(err, domain.ErrFileLimit):
		return fileError{status: 409, body: gen.Error{Code: CodeFileLimit, Message: "A project can hold at most 200 files and folders."}}
	case errors.Is(err, domain.ErrNotTextFile):
		return fileError{status: 409, body: gen.Error{Code: CodeNotTextFile, Message: "Only a text file has content to save."}}
	case errors.Is(err, domain.ErrFileTooLarge):
		return fileError{status: 413, body: gen.Error{Code: CodeFileTooLarge,
			Message: "A text file may be at most 1 MB."}}
	case errors.Is(err, domain.ErrUnsupportedType):
		return fileError{status: 415, body: gen.Error{Code: CodeUnsupportedType, Message: "Upload a PNG, JPEG or PDF file."}}
	case errors.As(err, &limited):
		return fileError{status: 429, limit: limited}
	}
	return fileError{}
}

func (e fileError) forbidden() gen.AccountSuspendedOrForbiddenJSONResponse {
	if e.susp != nil {
		return suspendedBody(e.susp)
	}
	return gen.AccountSuspendedOrForbiddenJSONResponse(e.body)
}

func (e fileError) rateLimited(msg string) gen.ProjectFileRateLimitedJSONResponse {
	return gen.ProjectFileRateLimitedJSONResponse{
		Body:    gen.Error{Code: CodeRateLimited, Message: msg},
		Headers: gen.ProjectFileRateLimitedResponseHeaders{RetryAfter: retryAfter(e.limit)},
	}
}

// ListProjectFiles returns the project's whole file tree (FR-12.1).
func (h *Handler) ListProjectFiles(ctx context.Context, req gen.ListProjectFilesRequestObject) (gen.ListProjectFilesResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListProjectFiles401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	tree, err := h.files.Files(ctx, id, req.ProjectId)
	switch e := classifyFile(err); {
	case e.status == 403:
		return gen.ListProjectFiles403JSONResponse{ForbiddenJSONResponse: forbidden}, nil
	case e.status == 404:
		return gen.ListProjectFiles404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
	case err != nil:
		return nil, err
	}
	items := make([]gen.ProjectFile, 0, len(tree.Files))
	for _, f := range tree.Files {
		b, err := fileBody(f)
		if err != nil {
			return nil, err
		}
		items = append(items, b)
	}
	return gen.ListProjectFiles200JSONResponse{
		MainFilePath: tree.MainFilePath, Items: items,
		Storage: gen.ProjectStorage{
			UsedBytes: tree.Storage.UsedBytes, LimitBytes: tree.Storage.LimitBytes, FigureMaxBytes: tree.Storage.FigureMaxBytes,
		},
	}, nil
}

// GetProjectFile reads one file: a text file's content or a clean figure's download.
func (h *Handler) GetProjectFile(ctx context.Context, req gen.GetProjectFileRequestObject) (gen.GetProjectFileResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.GetProjectFile401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	c, err := h.files.File(ctx, id, req.ProjectId, req.FileId)
	switch e := classifyFile(err); {
	case errors.Is(err, domain.ErrScanFailed):
		return gen.GetProjectFile409JSONResponse{AttachmentNotReadyJSONResponse: gen.AttachmentNotReadyJSONResponse{
			Code: CodeScanFailed, Message: "This file could not be checked for malware, so it cannot be downloaded."}}, nil
	case errors.Is(err, domain.ErrScanPending):
		return gen.GetProjectFile409JSONResponse{AttachmentNotReadyJSONResponse: gen.AttachmentNotReadyJSONResponse{
			Code: CodeScanPending, Message: "This file is still being checked for malware. Try again in a moment."}}, nil
	case errors.Is(err, domain.ErrFileRemoved):
		return gen.GetProjectFile410JSONResponse{AttachmentRemovedJSONResponse: gen.AttachmentRemovedJSONResponse{
			Code: CodeFileRemoved, Message: "This file was removed because it contained malware."}}, nil
	case e.status == 403:
		return gen.GetProjectFile403JSONResponse{ForbiddenJSONResponse: forbidden}, nil
	case e.status == 404:
		return gen.GetProjectFile404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
	case err != nil:
		return nil, err
	}
	f, err := fileBody(c.File)
	if err != nil {
		return nil, err
	}
	body := gen.ProjectFileDetail{File: f, Content: c.Content}
	if c.URL != nil {
		body.Download = &gen.ProjectFileDownload{Url: c.URL.String(), ExpiresAt: c.ExpiresAt}
	}
	return gen.GetProjectFile200JSONResponse{Body: body, Headers: gen.GetProjectFile200ResponseHeaders{CacheControl: "no-store"}}, nil
}

// CreateProjectFile creates a text file or a folder (FR-12.1).
func (h *Handler) CreateProjectFile(ctx context.Context, req gen.CreateProjectFileRequestObject) (gen.CreateProjectFileResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.CreateProjectFile401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.CreateProjectFile400JSONResponse{InvalidProjectFileJSONResponse: gen.InvalidProjectFileJSONResponse(badRequest)}, nil
	}
	f, err := h.files.CreateFile(ctx, id, req.ProjectId, usecase.CreateFileInput{
		Path: req.Body.Path, Kind: domain.FileKind(req.Body.Kind), Content: req.Body.Content,
	})
	switch e := classifyFile(err); e.status {
	case 400:
		return gen.CreateProjectFile400JSONResponse{InvalidProjectFileJSONResponse: gen.InvalidProjectFileJSONResponse(e.body)}, nil
	case 403:
		return gen.CreateProjectFile403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: e.forbidden()}, nil
	case 404:
		return gen.CreateProjectFile404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
	case 409:
		return gen.CreateProjectFile409JSONResponse{ProjectFileConflictJSONResponse: gen.ProjectFileConflictJSONResponse(e.body)}, nil
	case 413:
		return gen.CreateProjectFile413JSONResponse{FileTooLargeJSONResponse: gen.FileTooLargeJSONResponse(e.body)}, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := fileBody(f)
	if err != nil {
		return nil, err
	}
	return gen.CreateProjectFile201JSONResponse(body), nil
}

// MoveProjectFile renames or moves a file or folder (FR-12.1).
func (h *Handler) MoveProjectFile(ctx context.Context, req gen.MoveProjectFileRequestObject) (gen.MoveProjectFileResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.MoveProjectFile401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.MoveProjectFile400JSONResponse{InvalidProjectFileJSONResponse: gen.InvalidProjectFileJSONResponse(badRequest)}, nil
	}
	f, err := h.files.MoveFile(ctx, id, req.ProjectId, req.FileId, req.Body.Path)
	switch e := classifyFile(err); e.status {
	case 400:
		return gen.MoveProjectFile400JSONResponse{InvalidProjectFileJSONResponse: gen.InvalidProjectFileJSONResponse(e.body)}, nil
	case 403:
		return gen.MoveProjectFile403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: e.forbidden()}, nil
	case 404:
		return gen.MoveProjectFile404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
	case 409:
		return gen.MoveProjectFile409JSONResponse{ProjectFileConflictJSONResponse: gen.ProjectFileConflictJSONResponse(e.body)}, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := fileBody(f)
	if err != nil {
		return nil, err
	}
	return gen.MoveProjectFile200JSONResponse(body), nil
}

// DeleteProjectFile deletes a file, or a folder with everything in it (FR-12.1).
func (h *Handler) DeleteProjectFile(ctx context.Context, req gen.DeleteProjectFileRequestObject) (gen.DeleteProjectFileResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.DeleteProjectFile401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.files.DeleteFile(ctx, id, req.ProjectId, req.FileId)
	switch e := classifyFile(err); e.status {
	case 403:
		return gen.DeleteProjectFile403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: e.forbidden()}, nil
	case 404:
		return gen.DeleteProjectFile404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.DeleteProjectFile204Response{}, nil
}

// SaveProjectFileContent stores a text file's content unless it changed since the version
// the client sent (FR-12.6).
func (h *Handler) SaveProjectFileContent(ctx context.Context, req gen.SaveProjectFileContentRequestObject) (gen.SaveProjectFileContentResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.SaveProjectFileContent401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.SaveProjectFileContent400JSONResponse{InvalidProjectFileJSONResponse: gen.InvalidProjectFileJSONResponse(badRequest)}, nil
	}
	f, err := h.files.SaveContent(ctx, id, req.ProjectId, req.FileId, req.Body.Content, req.Body.Version)
	if conflict, ok := errors.AsType[*domain.VersionConflictError](err); ok {
		return gen.SaveProjectFileContent409JSONResponse{
			Code:    CodeVersionConflict,
			Message: "This file was changed in another tab or by someone else since you opened it.",
			Current: &gen.ProjectFileCurrent{Version: conflict.Version, Content: conflict.Content, UpdatedAt: conflict.UpdatedAt},
		}, nil
	}
	switch e := classifyFile(err); e.status {
	case 400:
		return gen.SaveProjectFileContent400JSONResponse{InvalidProjectFileJSONResponse: gen.InvalidProjectFileJSONResponse(e.body)}, nil
	case 403:
		return gen.SaveProjectFileContent403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: e.forbidden()}, nil
	case 404:
		return gen.SaveProjectFileContent404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
	case 409:
		return gen.SaveProjectFileContent409JSONResponse{Code: e.body.Code, Message: e.body.Message}, nil
	case 413:
		return gen.SaveProjectFileContent413JSONResponse{FileTooLargeJSONResponse: gen.FileTooLargeJSONResponse(e.body)}, nil
	case 429:
		return gen.SaveProjectFileContent429JSONResponse{
			ProjectFileRateLimitedJSONResponse: e.rateLimited("You are saving too often. Try again in a moment."),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := fileBody(f)
	if err != nil {
		return nil, err
	}
	return gen.SaveProjectFileContent200JSONResponse(body), nil
}

// StartFigureUpload signs a POST that sends a figure straight to storage (FR-12.2).
func (h *Handler) StartFigureUpload(ctx context.Context, req gen.StartFigureUploadRequestObject) (gen.StartFigureUploadResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.StartFigureUpload401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.StartFigureUpload400JSONResponse{InvalidProjectFileJSONResponse: gen.InvalidProjectFileJSONResponse(fieldError("path"))}, nil
	}
	if req.Body.Size < 1 {
		return gen.StartFigureUpload400JSONResponse{InvalidProjectFileJSONResponse: gen.InvalidProjectFileJSONResponse{
			Code: httpx.CodeBadRequest, Message: "The file is empty.", Field: ptr("size")}}, nil
	}
	t, err := h.files.StartFigureUpload(ctx, id, req.ProjectId, req.Body.Path, req.Body.Size)
	switch e := classifyFile(err); e.status {
	case 400:
		return gen.StartFigureUpload400JSONResponse{InvalidProjectFileJSONResponse: gen.InvalidProjectFileJSONResponse(e.body)}, nil
	case 403:
		return gen.StartFigureUpload403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: e.forbidden()}, nil
	case 404:
		return gen.StartFigureUpload404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
	case 409:
		return gen.StartFigureUpload409JSONResponse{ProjectFileConflictJSONResponse: gen.ProjectFileConflictJSONResponse(e.body)}, nil
	case 413:
		return gen.StartFigureUpload413JSONResponse{FigureTooLargeJSONResponse: gen.FigureTooLargeJSONResponse(*e.size)}, nil
	case 429:
		return gen.StartFigureUpload429JSONResponse{
			ProjectFileRateLimitedJSONResponse: e.rateLimited("You have uploaded 30 figures this hour. Try again later."),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	uid, err := uuidOf(t.ID)
	if err != nil {
		return nil, err
	}
	return gen.StartFigureUpload201JSONResponse{Id: uid, Url: t.URL.String(), Fields: t.Fields, ExpiresAt: t.ExpiresAt}, nil
}

// CompleteFigureUpload records a figure the browser sent to storage (FR-12.2).
func (h *Handler) CompleteFigureUpload(ctx context.Context, req gen.CompleteFigureUploadRequestObject) (gen.CompleteFigureUploadResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.CompleteFigureUpload401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	f, err := h.files.CompleteFigureUpload(ctx, id, req.ProjectId, req.UploadId)
	switch e := classifyFile(err); e.status {
	case 400:
		return gen.CompleteFigureUpload400JSONResponse{InvalidProjectFileJSONResponse: gen.InvalidProjectFileJSONResponse(e.body)}, nil
	case 403:
		return gen.CompleteFigureUpload403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: e.forbidden()}, nil
	case 404:
		return gen.CompleteFigureUpload404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
	case 409:
		return gen.CompleteFigureUpload409JSONResponse{ProjectFileConflictJSONResponse: gen.ProjectFileConflictJSONResponse(e.body)}, nil
	case 413:
		return gen.CompleteFigureUpload413JSONResponse{FigureTooLargeJSONResponse: gen.FigureTooLargeJSONResponse(*e.size)}, nil
	case 415:
		return gen.CompleteFigureUpload415JSONResponse{UnsupportedFigureTypeJSONResponse: gen.UnsupportedFigureTypeJSONResponse(e.body)}, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := fileBody(f)
	if err != nil {
		return nil, err
	}
	return gen.CompleteFigureUpload201JSONResponse(body), nil
}

// formatBytes writes a size in MB (2^20 bytes), as the limits are set: "50 MB", "12.5 MB";
// under 1 MB in KB. The web client writes sizes the same way.
func formatBytes(n int64) string {
	const mb = 1 << 20
	if n < mb {
		return fmt.Sprintf("%d KB", (n+1023)/1024)
	}
	if n%mb == 0 {
		return fmt.Sprintf("%d MB", n/mb)
	}
	// Rounded down, so a project just under its limit never reads as full.
	return fmt.Sprintf("%.1f MB", math.Floor(float64(n)*10/mb)/10)
}

func fileBody(f domain.File) (gen.ProjectFile, error) {
	id, err := uuidOf(f.ID)
	if err != nil {
		return gen.ProjectFile{}, err
	}
	b := gen.ProjectFile{
		Id: id, Path: f.Path, Kind: gen.ProjectFileKind(f.Kind), Size: f.Size, Version: f.Version,
		ContentType: f.ContentType, UpdatedAt: f.UpdatedAt,
	}
	if f.ScanStatus != nil {
		st := gen.AttachmentStatus(*f.ScanStatus)
		b.ScanStatus = &st
	}
	if f.UpdatedBy != nil {
		p, err := personBody(*f.UpdatedBy)
		if err != nil {
			return gen.ProjectFile{}, err
		}
		b.UpdatedBy = &p
	}
	return b, nil
}
