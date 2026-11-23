// Package http implements the compile operations of docs/openapi.yaml (FR-13.1, FR-13.2,
// FR-13.4 to FR-13.6) and the compile worker's internal operations (docs/compile.md). cmd/api
// checks the platform action before a member's request reaches these handlers, and the
// service token before the worker's; the usecase checks the project role.
package http

import (
	"context"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/compile/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/compile/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// Error codes of the compile operations.
const (
	CodeCompileRunning     = "compile_running"
	CodeMainFileMissing    = "main_file_missing"
	CodeCompileUnavailable = "compile_unavailable"
	CodeCompileBusy        = "compile_busy"
	CodeRateLimited        = "rate_limited"
	CodeAccountSuspended   = "account_suspended"
	CodeNoCompile          = "no_compile"
	CodeNoLog              = "no_log"
	CodeNoPDF              = "no_pdf"
	CodeInputTooLarge      = "input_too_large"
	CodeFileTooLarge       = "file_too_large"
)

// PDFPolicy is the Content-Security-Policy of a served PDF: a PDF may hold JavaScript, and
// sandbox keeps a browser that shows it from running anything in the app's origin
// (docs/decisions.md).
const PDFPolicy = "sandbox; default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; frame-ancestors 'self'"

// Deadlines of the long responses.
const (
	pdfDeadline    = 10 * time.Minute
	sourceDeadline = 5 * time.Minute
)

// Service is what the handlers need from the usecase.
type Service interface {
	Start(ctx context.Context, userID, projectID string) (domain.Job, error)
	Latest(ctx context.Context, userID, projectID string) (domain.Job, error)
	Get(ctx context.Context, userID, projectID, jobID string) (domain.Job, error)
	Log(ctx context.Context, userID, projectID, jobID string) (string, error)
	OpenPDF(ctx context.Context, userID, projectID string) (domain.PDF, io.ReadCloser, error)
	Claim(ctx context.Context, workerID string) (domain.Claimed, bool, error)
	PrepareSource(ctx context.Context, jobID string) (usecase.Prepared, error)
	WriteSource(ctx context.Context, p usecase.Prepared, w io.Writer) error
	StorePDF(ctx context.Context, jobID string, body io.Reader) error
	Finish(ctx context.Context, jobID string, r domain.Result) error
}

// Handler serves the compile operations.
type Handler struct {
	svc Service
}

// NewHandler returns a Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

var (
	unauthenticated = gen.UnauthorizedJSONResponse{Code: httpx.CodeUnauthenticated, Message: "Log in to continue."}
	notFound        = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This project does not exist."}
	compileNotFound = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This compile does not exist."}
	jobNotFound     = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This compile job is not running."}
	badRequest      = gen.BadSyncRequestJSONResponse{Code: httpx.CodeBadRequest, Message: "The request is not valid."}
)

func fieldError(field string) gen.BadSyncRequestJSONResponse {
	f := field
	return gen.BadSyncRequestJSONResponse{Code: httpx.CodeBadRequest, Message: "This field is not valid.", Field: &f}
}

// StartCompile queues a compile (FR-13.1, FR-13.4).
func (h *Handler) StartCompile(ctx context.Context, req gen.StartCompileRequestObject) (gen.StartCompileResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.StartCompile401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	j, err := h.svc.Start(ctx, id, req.ProjectId)
	var (
		running   *domain.RunningError
		limited   *domain.RateLimitedError
		suspended *domain.SuspendedError
	)
	switch {
	case errors.As(err, &running):
		body := gen.StartCompile409JSONResponse{Code: CodeCompileRunning, Message: "A compile of this project is already running."}
		if u, err := uuidOf(running.ID); err == nil {
			body.CompileId = &u
		}
		return body, nil
	case errors.Is(err, domain.ErrMainFileMissing):
		return gen.StartCompile409JSONResponse{Code: CodeMainFileMissing,
			Message: "The main file does not exist. Create it, or choose another main file."}, nil
	case errors.As(err, &limited):
		return gen.StartCompile429JSONResponse{CompileRateLimitedJSONResponse: gen.CompileRateLimitedJSONResponse{
			Body:    gen.Error{Code: CodeRateLimited, Message: "You have started 10 compiles this minute. Try again in a moment."},
			Headers: gen.CompileRateLimitedResponseHeaders{RetryAfter: max(1, int(math.Ceil(limited.RetryAfter.Seconds())))},
		}}, nil
	case errors.As(err, &suspended):
		return gen.StartCompile403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: gen.AccountSuspendedOrForbiddenJSONResponse{
			Code: CodeAccountSuspended, Message: "This account is suspended. Contact support.", Reason: suspended.Reason, Until: suspended.Until,
		}}, nil
	case errors.Is(err, domain.ErrForbidden):
		return gen.StartCompile403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: gen.AccountSuspendedOrForbiddenJSONResponse{
			Code: httpx.CodeForbidden, Message: "You do not have permission to do this.",
		}}, nil
	case errors.Is(err, domain.ErrNotFound):
		return gen.StartCompile404JSONResponse{NotFoundJSONResponse: notFound}, nil
	case errors.Is(err, domain.ErrUnavailable):
		return gen.StartCompile503JSONResponse{Code: CodeCompileUnavailable,
			Message: "The compile service is not available right now. Your files are saved; try again in a minute."}, nil
	case errors.Is(err, domain.ErrBusy):
		return gen.StartCompile503JSONResponse{Code: CodeCompileBusy, Message: "Many compiles are waiting. Try again in a minute."}, nil
	case err != nil:
		return nil, err
	}
	body, err := jobBody(j)
	if err != nil {
		return nil, err
	}
	return gen.StartCompile202JSONResponse(body), nil
}

// GetLatestCompile reads the project's newest compile (FR-13.2).
func (h *Handler) GetLatestCompile(ctx context.Context, req gen.GetLatestCompileRequestObject) (gen.GetLatestCompileResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.GetLatestCompile401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	j, err := h.svc.Latest(ctx, id, req.ProjectId)
	switch {
	case errors.Is(err, domain.ErrNoCompile):
		return gen.GetLatestCompile404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{
			Code: CodeNoCompile, Message: "This project has not been compiled yet."}}, nil
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrForbidden):
		return gen.GetLatestCompile404JSONResponse{NotFoundJSONResponse: notFound}, nil
	case err != nil:
		return nil, err
	}
	body, err := jobBody(j)
	if err != nil {
		return nil, err
	}
	return gen.GetLatestCompile200JSONResponse(body), nil
}

// GetCompile reads one compile (FR-13.2).
func (h *Handler) GetCompile(ctx context.Context, req gen.GetCompileRequestObject) (gen.GetCompileResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.GetCompile401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	j, err := h.svc.Get(ctx, id, req.ProjectId, req.CompileId)
	if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrForbidden) {
		return gen.GetCompile404JSONResponse{NotFoundJSONResponse: compileNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := jobBody(j)
	if err != nil {
		return nil, err
	}
	return gen.GetCompile200JSONResponse(body), nil
}

// GetCompileLog reads a compile's log (FR-13.2).
func (h *Handler) GetCompileLog(ctx context.Context, req gen.GetCompileLogRequestObject) (gen.GetCompileLogResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.GetCompileLog401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	l, err := h.svc.Log(ctx, id, req.ProjectId, req.CompileId)
	switch {
	case errors.Is(err, domain.ErrNoLog):
		return gen.GetCompileLog404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{
			Code: CodeNoLog, Message: "The log is kept for the latest finished compile only."}}, nil
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrForbidden):
		return gen.GetCompileLog404JSONResponse{NotFoundJSONResponse: compileNotFound}, nil
	case err != nil:
		return nil, err
	}
	return gen.GetCompileLog200TextResponse{Body: l, Headers: gen.GetCompileLog200ResponseHeaders{CacheControl: "no-store"}}, nil
}

// DownloadProjectPdf streams the project's latest PDF (FR-13.6) with the headers that keep its
// scripts from running in the app's origin.
func (h *Handler) DownloadProjectPdf(ctx context.Context, req gen.DownloadProjectPdfRequestObject) (gen.DownloadProjectPdfResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.DownloadProjectPdf401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	disposition := "attachment"
	if d := req.Params.Disposition; d != nil {
		switch *d {
		case gen.DownloadProjectPdfParamsDispositionInline:
			disposition = "inline"
		case gen.DownloadProjectPdfParamsDispositionAttachment:
		default:
			f := "disposition"
			return gen.DownloadProjectPdf400JSONResponse{Code: httpx.CodeBadRequest, Message: "This field is not valid.", Field: &f}, nil
		}
	}
	p, r, err := h.svc.OpenPDF(ctx, id, req.ProjectId)
	switch {
	case errors.Is(err, domain.ErrNoPDF):
		return gen.DownloadProjectPdf404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{
			Code: CodeNoPDF, Message: "This project has no PDF yet. Compile it first."}}, nil
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrForbidden):
		return gen.DownloadProjectPdf404JSONResponse{NotFoundJSONResponse: notFound}, nil
	case err != nil:
		return nil, err
	}
	return pdfResponse{pdf: p, body: r, disposition: disposition}, nil
}

type pdfResponse struct {
	pdf         domain.PDF
	body        io.ReadCloser
	disposition string
}

func (r pdfResponse) VisitDownloadProjectPdfResponse(w http.ResponseWriter) error {
	defer func() { _ = r.body.Close() }()
	h := PDFHeaders(r.disposition, FileName(r.pdf.Title)+".pdf")
	h.Set("Content-Length", itoa(r.pdf.Size))
	h.Set("X-Compile-Id", r.pdf.CompileID)
	return httpx.Stream(w, pdfDeadline, h, func(out io.Writer) error {
		_, err := io.Copy(out, r.body)
		return err
	})
}

// PDFHeaders are the headers of a served PDF: its type, how to show it, never sniffed, never
// cached, and a sandboxing policy for any script it holds.
func PDFHeaders(disposition, filename string) http.Header {
	h := http.Header{}
	h.Set("Content-Type", "application/pdf")
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": filename}))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", PDFPolicy)
	h.Set("Cache-Control", "private, no-store")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	return h
}

// FileName makes a file name from a project's title, as the archive's: control characters
// and the characters file systems refuse become "_", at most 100 characters.
func FileName(title string) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.TrimSpace(title) {
		if n == 100 {
			break
		}
		switch {
		case unicode.IsSpace(r):
			r = ' '
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || strings.ContainsRune(`/\:*?"<>|`, r):
			r = '_'
		}
		b.WriteRune(r)
		n++
	}
	name := strings.Trim(b.String(), " .")
	if name == "" {
		return "project"
	}
	return name
}

// ClaimCompileJob gives the worker the oldest queued job (internal).
func (h *Handler) ClaimCompileJob(ctx context.Context, req gen.ClaimCompileJobRequestObject) (gen.ClaimCompileJobResponseObject, error) {
	if req.Body == nil {
		return gen.ClaimCompileJob400JSONResponse{BadSyncRequestJSONResponse: badRequest}, nil
	}
	c, ok, err := h.svc.Claim(ctx, req.Body.WorkerId)
	var field *domain.FieldError
	if errors.As(err, &field) {
		return gen.ClaimCompileJob400JSONResponse{BadSyncRequestJSONResponse: fieldError(field.Field)}, nil
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return gen.ClaimCompileJob204Response{}, nil
	}
	id, err1 := uuidOf(c.ID)
	pid, err2 := uuidOf(c.ProjectID)
	if err := errors.Join(err1, err2); err != nil {
		return nil, err
	}
	return gen.ClaimCompileJob200JSONResponse{Id: id, ProjectId: pid, MainFilePath: c.MainFilePath, Engine: gen.CompileEngine(c.Engine)}, nil
}

// GetCompileSource streams a running job's files as a tar (internal).
func (h *Handler) GetCompileSource(ctx context.Context, req gen.GetCompileSourceRequestObject) (gen.GetCompileSourceResponseObject, error) {
	p, err := h.svc.PrepareSource(ctx, req.CompileId)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.GetCompileSource404JSONResponse{NotFoundJSONResponse: jobNotFound}, nil
	case errors.Is(err, domain.ErrInputTooLarge):
		return gen.GetCompileSource409JSONResponse{Code: CodeInputTooLarge, Message: "The project's files are too large to compile."}, nil
	case err != nil:
		return nil, err
	}
	return sourceResponse{ctx: ctx, prepared: p, svc: h.svc}, nil
}

type sourceResponse struct {
	ctx      context.Context //nolint:containedctx // the request's, used while the body is written
	prepared usecase.Prepared
	svc      Service
}

func (r sourceResponse) VisitGetCompileSourceResponse(w http.ResponseWriter) error {
	h := http.Header{}
	h.Set("Content-Type", "application/x-tar")
	h.Set("Cache-Control", "no-store")
	return httpx.Stream(w, sourceDeadline, h, func(out io.Writer) error {
		return r.svc.WriteSource(r.ctx, r.prepared, out)
	})
}

// StoreCompilePdf stores the PDF a running job made (internal).
func (h *Handler) StoreCompilePdf(ctx context.Context, req gen.StoreCompilePdfRequestObject) (gen.StoreCompilePdfResponseObject, error) {
	if req.Body == nil {
		return gen.StoreCompilePdf400JSONResponse{BadSyncRequestJSONResponse: badRequest}, nil
	}
	err := h.svc.StorePDF(ctx, req.CompileId, req.Body)
	var tooLong *http.MaxBytesError
	switch {
	case errors.Is(err, domain.ErrPDFTooLarge), errors.As(err, &tooLong):
		return gen.StoreCompilePdf413JSONResponse{FileTooLargeJSONResponse: gen.FileTooLargeJSONResponse{
			Code: CodeFileTooLarge, Message: "The PDF is larger than the limit."}}, nil
	case errors.Is(err, domain.ErrNotPDF):
		return gen.StoreCompilePdf400JSONResponse{BadSyncRequestJSONResponse: gen.BadSyncRequestJSONResponse{
			Code: httpx.CodeBadRequest, Message: "The body is not a PDF."}}, nil
	case errors.Is(err, domain.ErrNotFound):
		return gen.StoreCompilePdf404JSONResponse{NotFoundJSONResponse: jobNotFound}, nil
	case err != nil:
		return nil, err
	}
	return gen.StoreCompilePdf204Response{}, nil
}

// FinishCompileJob records how a job ended (internal).
func (h *Handler) FinishCompileJob(ctx context.Context, req gen.FinishCompileJobRequestObject) (gen.FinishCompileJobResponseObject, error) {
	if req.Body == nil {
		return gen.FinishCompileJob400JSONResponse{BadSyncRequestJSONResponse: badRequest}, nil
	}
	b := req.Body
	r := domain.Result{
		Outcome: domain.Outcome(b.Outcome), ExitCode: b.ExitCode, Duration: time.Duration(b.DurationMs) * time.Millisecond,
		Log: b.Log, BibtexLog: b.BibtexLog,
	}
	if b.Failure != nil {
		f := domain.Failure(*b.Failure)
		r.Failure = &f
	}
	err := h.svc.Finish(ctx, req.CompileId, r)
	var field *domain.FieldError
	switch {
	case errors.As(err, &field):
		return gen.FinishCompileJob400JSONResponse{BadSyncRequestJSONResponse: fieldError(field.Field)}, nil
	case errors.Is(err, domain.ErrNotFound):
		return gen.FinishCompileJob404JSONResponse{NotFoundJSONResponse: jobNotFound}, nil
	case err != nil:
		return nil, err
	}
	return gen.FinishCompileJob204Response{}, nil
}

// JobBody is the API's Compile for a job; realtime events use it too.
func JobBody(j domain.Job) (gen.Compile, error) { return jobBody(j) }

func jobBody(j domain.Job) (gen.Compile, error) {
	id, err1 := uuidOf(j.ID)
	pid, err2 := uuidOf(j.ProjectID)
	if err := errors.Join(err1, err2); err != nil {
		return gen.Compile{}, err
	}
	c := gen.Compile{
		Id: id, ProjectId: pid, Status: gen.CompileStatus(j.Status), Engine: gen.CompileEngine(j.Engine),
		MainFilePath: j.MainFilePath, CreatedAt: j.QueuedAt, StartedAt: j.StartedAt, FinishedAt: j.FinishedAt,
		DurationMs: j.DurationMs, LiveTextConfirmed: j.LiveTextConfirmed, Pages: j.Pages, HasLog: j.HasLog, HasPdf: j.HasPDF,
		Errors: make([]gen.CompileError, len(j.Problems)), Skipped: make([]gen.CompileSkippedFile, len(j.Skipped)),
	}
	if j.RequestedBy != nil {
		if u, err := uuidOf(*j.RequestedBy); err == nil {
			c.RequestedBy = &u
		}
	}
	if j.Failure != nil {
		f := gen.CompileFailure(*j.Failure)
		c.Failure = &f
	}
	for i, p := range j.Problems {
		c.Errors[i] = gen.CompileError{File: p.File, Line: p.Line, Message: p.Message, Severity: gen.CompileErrorSeverity(p.Severity)}
	}
	for i, s := range j.Skipped {
		c.Skipped[i] = gen.CompileSkippedFile{Path: s.Path, Reason: gen.CompileSkippedFileReason(s.Reason)}
	}
	return c, nil
}

func uuidOf(s string) (openapi_types.UUID, error) {
	var u openapi_types.UUID
	err := u.UnmarshalText([]byte(s))
	return u, err
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
