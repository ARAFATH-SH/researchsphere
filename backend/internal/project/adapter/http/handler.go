// Package http implements the project operations of docs/openapi.yaml (FR-10.1 to FR-10.3,
// FR-10.6, FR-15.11). cmd/api checks the platform action before a request reaches these
// handlers (platformGuards); the usecase checks the project role.
package http

import (
	"context"
	"errors"
	"fmt"
	"math"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/usecase"
)

// Error codes of the project operations.
const (
	CodeRateLimited          = "rate_limited"
	CodeInvalidQuery         = "invalid_query"
	CodeConfirmationMismatch = "confirmation_mismatch"
	CodeCannotReportOwn      = "cannot_report_own_project"
	CodeAlreadyReported      = "already_reported"
	CodeAccountSuspended     = "account_suspended"
	CodeAlreadyResolved      = "report_already_resolved"
)

// Service is the part of the usecase the handlers call.
type Service interface {
	Create(ctx context.Context, userID string, in usecase.CreateInput) (domain.Project, error)
	Get(ctx context.Context, viewerID, projectID string) (domain.Project, error)
	Update(ctx context.Context, viewerID, projectID string, in usecase.UpdateInput) (domain.Project, error)
	Delete(ctx context.Context, viewerID, projectID string, confirm *string) error
	ListMine(ctx context.Context, userID string, page, pageSize int) (usecase.Page, error)
	Search(ctx context.Context, viewerID string, q *string, page int) (domain.SearchResult, error)
	Report(ctx context.Context, userID, projectID string, in usecase.ReportInput) (domain.ReportReceipt, error)
	ListReports(ctx context.Context, status *string, page int) (usecase.ReportPage, error)
	GetReport(ctx context.Context, reportID string) (domain.Report, domain.ReportedProject, error)
	ResolveReport(ctx context.Context, adminID, reportID, outcome string, note *string) (domain.Report, error)
}

// Handler serves the project operations.
type Handler struct {
	svc Service
}

// NewHandler returns a Handler.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

var (
	unauthenticated = gen.UnauthorizedJSONResponse{Code: httpx.CodeUnauthenticated, Message: "Log in to continue."}
	forbidden       = gen.ForbiddenJSONResponse{Code: httpx.CodeForbidden, Message: "You do not have permission to do this."}
	notFound        = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This project does not exist."}
	reportNotFound  = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This report does not exist."}
	badRequest      = gen.Error{Code: httpx.CodeBadRequest, Message: "The request is not valid."}
)

// fieldError is the bad_request answer about one field; it never repeats the value.
func fieldError(field string) gen.Error {
	f := field
	return gen.Error{Code: httpx.CodeBadRequest, Message: "This field is not valid.", Field: &f}
}

func retryAfter(e *domain.RateLimitedError) int {
	return max(1, int(math.Ceil(e.RetryAfter.Seconds())))
}

// CreateProject creates a project owned by the caller (FR-10.1).
func (h *Handler) CreateProject(ctx context.Context, req gen.CreateProjectRequestObject) (gen.CreateProjectResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.CreateProject401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.CreateProject400JSONResponse{InvalidProjectJSONResponse: gen.InvalidProjectJSONResponse(badRequest)}, nil
	}
	in := usecase.CreateInput{Title: req.Body.Title, Description: req.Body.Description, Abstract: req.Body.Abstract}
	if req.Body.RequiredSkills != nil {
		in.Skills = *req.Body.RequiredSkills
	}
	p, err := h.svc.Create(ctx, id, in)
	var (
		field   *domain.FieldError
		limited *domain.RateLimitedError
	)
	switch {
	case errors.As(err, &field):
		return gen.CreateProject400JSONResponse{InvalidProjectJSONResponse: gen.InvalidProjectJSONResponse(fieldError(field.Field))}, nil
	case errors.As(err, &limited):
		return gen.CreateProject429JSONResponse{
			Body:    gen.Error{Code: CodeRateLimited, Message: "You have created 10 projects today. Try again later."},
			Headers: gen.CreateProject429ResponseHeaders{RetryAfter: retryAfter(limited)},
		}, nil
	case err != nil:
		return nil, err
	}
	body, err := projectBody(p)
	if err != nil {
		return nil, err
	}
	return gen.CreateProject201JSONResponse(body), nil
}

// GetProject reads a project (SRS 2.3).
func (h *Handler) GetProject(ctx context.Context, req gen.GetProjectRequestObject) (gen.GetProjectResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.GetProject401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	p, err := h.svc.Get(ctx, id, req.ProjectId)
	if errors.Is(err, domain.ErrNotFound) {
		return gen.GetProject404JSONResponse{NotFoundJSONResponse: notFound}, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := projectBody(p)
	if err != nil {
		return nil, err
	}
	return gen.GetProject200JSONResponse(body), nil
}

// UpdateProject changes a project's details or status (FR-10.2).
func (h *Handler) UpdateProject(ctx context.Context, req gen.UpdateProjectRequestObject) (gen.UpdateProjectResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.UpdateProject401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.UpdateProject400JSONResponse{InvalidProjectJSONResponse: gen.InvalidProjectJSONResponse(badRequest)}, nil
	}
	in, field := updateInput(*req.Body)
	if field != "" {
		return gen.UpdateProject400JSONResponse{InvalidProjectJSONResponse: gen.InvalidProjectJSONResponse(fieldError(field))}, nil
	}
	p, err := h.svc.Update(ctx, id, req.ProjectId, in)
	var fe *domain.FieldError
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.UpdateProject404JSONResponse{NotFoundJSONResponse: notFound}, nil
	case errors.Is(err, domain.ErrForbidden):
		return gen.UpdateProject403JSONResponse{ForbiddenJSONResponse: forbidden}, nil
	case errors.As(err, &fe):
		return gen.UpdateProject400JSONResponse{InvalidProjectJSONResponse: gen.InvalidProjectJSONResponse(fieldError(fe.Field))}, nil
	case err != nil:
		return nil, err
	}
	body, err := projectBody(p)
	if err != nil {
		return nil, err
	}
	return gen.UpdateProject200JSONResponse(body), nil
}

// updateInput reads the patch; a null for a field that cannot be cleared names that field.
func updateInput(b gen.ProjectUpdate) (usecase.UpdateInput, string) {
	var in usecase.UpdateInput
	if b.Title.IsSpecified() {
		if b.Title.IsNull() {
			return in, "title"
		}
		v := b.Title.MustGet()
		in.Title = &v
	}
	if b.Description.IsSpecified() {
		if b.Description.IsNull() {
			return in, "description"
		}
		v := b.Description.MustGet()
		in.Description = &v
	}
	if b.RequiredSkills.IsSpecified() {
		if b.RequiredSkills.IsNull() {
			return in, "requiredSkills"
		}
		v := b.RequiredSkills.MustGet()
		if v == nil {
			v = []string{}
		}
		in.Skills = &v
	}
	if b.Status.IsSpecified() {
		if b.Status.IsNull() {
			return in, "status"
		}
		v := b.Status.MustGet()
		in.Status = &v
	}
	if b.Abstract.IsSpecified() {
		in.SetAbstract = true
		if !b.Abstract.IsNull() {
			v := b.Abstract.MustGet()
			in.Abstract = &v
		}
	}
	return in, ""
}

// DeleteProject deletes a project after the owner confirms with its title (FR-10.6).
func (h *Handler) DeleteProject(ctx context.Context, req gen.DeleteProjectRequestObject) (gen.DeleteProjectResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.DeleteProject401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.svc.Delete(ctx, id, req.ProjectId, req.Params.Confirm)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.DeleteProject404JSONResponse{NotFoundJSONResponse: notFound}, nil
	case errors.Is(err, domain.ErrForbidden):
		return gen.DeleteProject403JSONResponse{ForbiddenJSONResponse: forbidden}, nil
	case errors.Is(err, domain.ErrConfirmMismatch):
		f := "confirm"
		return gen.DeleteProject400JSONResponse{
			Code: CodeConfirmationMismatch, Message: "Type the project's title to confirm.", Field: &f,
		}, nil
	case err != nil:
		return nil, err
	}
	return gen.DeleteProject204Response{}, nil
}

// ListMyProjects lists the caller's projects (FR-11.6).
func (h *Handler) ListMyProjects(ctx context.Context, req gen.ListMyProjectsRequestObject) (gen.ListMyProjectsResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListMyProjects401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	page, size := 1, usecase.DefaultPageSize
	if req.Params.Page != nil {
		page = *req.Params.Page
	}
	if req.Params.PageSize != nil {
		size = *req.Params.PageSize
	}
	res, err := h.svc.ListMine(ctx, id, page, size)
	if errors.Is(err, domain.ErrInvalidPage) {
		return gen.ListMyProjects400JSONResponse{Code: httpx.CodeBadRequest, Message: "The page is not valid."}, nil
	}
	if err != nil {
		return nil, err
	}
	body := gen.ProjectPage{Page: res.Page, PageSize: res.PageSize, Total: res.Total, Items: make([]gen.ProjectSummary, len(res.Items))}
	for i, it := range res.Items {
		s, err := summaryBody(it)
		if err != nil {
			return nil, err
		}
		body.Items[i] = s
	}
	return gen.ListMyProjects200JSONResponse(body), nil
}

// SearchProjects searches open projects (FR-10.3).
func (h *Handler) SearchProjects(ctx context.Context, req gen.SearchProjectsRequestObject) (gen.SearchProjectsResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.SearchProjects401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	page := 1
	if req.Params.Page != nil {
		page = *req.Params.Page
	}
	res, err := h.svc.Search(ctx, id, req.Params.Q, page)
	var limited *domain.RateLimitedError
	switch {
	case errors.Is(err, domain.ErrInvalidQuery):
		return gen.SearchProjects400JSONResponse{Code: CodeInvalidQuery, Message: "Type 2 to 100 characters to search."}, nil
	case errors.Is(err, domain.ErrInvalidPage):
		f := "page"
		return gen.SearchProjects400JSONResponse{Code: httpx.CodeBadRequest, Message: "The page is not valid.", Field: &f}, nil
	case errors.As(err, &limited):
		return gen.SearchProjects429JSONResponse{SearchRateLimitedJSONResponse: gen.SearchRateLimitedJSONResponse{
			Body:    gen.Error{Code: CodeRateLimited, Message: "You have searched many times in the last minute. Try again in a moment."},
			Headers: gen.SearchRateLimitedResponseHeaders{RetryAfter: retryAfter(limited)},
		}}, nil
	case err != nil:
		return nil, err
	}
	body := gen.ProjectSearchPage{
		Total: res.Total, Page: page, PageSize: usecase.SearchPageSize, Fuzzy: res.Fuzzy, CorrectedQuery: res.CorrectedQuery,
		Items: make([]gen.ProjectSearchResult, len(res.Items)),
	}
	for i, it := range res.Items {
		s, err := summaryBody(it)
		if err != nil {
			return nil, err
		}
		body.Items[i] = gen.ProjectSearchResult{
			Id: s.Id, Title: s.Title, Description: s.Description, Status: s.Status, RequiredSkills: s.RequiredSkills,
			Owner: s.Owner, MyRole: s.MyRole, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
			MatchedOn: gen.ProjectSearchResultMatchedOn(it.MatchedOn),
		}
	}
	return gen.SearchProjects200JSONResponse(body), nil
}

// ReportProject reports a project (FR-15.11).
func (h *Handler) ReportProject(ctx context.Context, req gen.ReportProjectRequestObject) (gen.ReportProjectResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ReportProject401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.ReportProject400JSONResponse(badRequest), nil
	}
	r, err := h.svc.Report(ctx, id, req.ProjectId, usecase.ReportInput{Reason: string(req.Body.Reason), Details: req.Body.Details})
	var (
		field     *domain.FieldError
		suspended *domain.SuspendedError
	)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.ReportProject404JSONResponse{NotFoundJSONResponse: notFound}, nil
	case errors.As(err, &suspended):
		return gen.ReportProject403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: gen.AccountSuspendedOrForbiddenJSONResponse{
			Code: CodeAccountSuspended, Message: "This account is suspended. Contact support.", Reason: suspended.Reason, Until: suspended.Until,
		}}, nil
	case errors.As(err, &field):
		return gen.ReportProject400JSONResponse(fieldError(field.Field)), nil
	case errors.Is(err, domain.ErrCannotReportOwn):
		return gen.ReportProject400JSONResponse{Code: CodeCannotReportOwn, Message: "You cannot report your own project."}, nil
	case errors.Is(err, domain.ErrAlreadyReported):
		return gen.ReportProject409JSONResponse{Code: CodeAlreadyReported, Message: "You have already reported this project."}, nil
	case err != nil:
		return nil, err
	}
	rid, err := uuidOf(r.ID)
	if err != nil {
		return nil, err
	}
	return gen.ReportProject201JSONResponse{Id: rid, CreatedAt: r.CreatedAt}, nil
}

// ListProjectReports lists project reports for an administrator (FR-15.11).
func (h *Handler) ListProjectReports(ctx context.Context, req gen.ListProjectReportsRequestObject) (gen.ListProjectReportsResponseObject, error) {
	if _, ok := httpx.UserID(ctx); !ok {
		return gen.ListProjectReports401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	page := 1
	if req.Params.Page != nil {
		page = *req.Params.Page
	}
	res, err := h.svc.ListReports(ctx, req.Params.Status, page)
	var field *domain.FieldError
	switch {
	case errors.As(err, &field):
		return gen.ListProjectReports400JSONResponse(fieldError(field.Field)), nil
	case errors.Is(err, domain.ErrInvalidPage):
		return gen.ListProjectReports400JSONResponse(fieldError("page")), nil
	case err != nil:
		return nil, err
	}
	body := gen.ProjectReportPage{Page: res.Page, PageSize: res.PageSize, Total: res.Total, Items: make([]gen.ProjectReport, len(res.Items))}
	for i, r := range res.Items {
		b, err := reportBody(r)
		if err != nil {
			return nil, err
		}
		body.Items[i] = b
	}
	return gen.ListProjectReports200JSONResponse(body), nil
}

// GetProjectReport reads a report and the reported project (FR-15.11).
func (h *Handler) GetProjectReport(ctx context.Context, req gen.GetProjectReportRequestObject) (gen.GetProjectReportResponseObject, error) {
	if _, ok := httpx.UserID(ctx); !ok {
		return gen.GetProjectReport401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	r, p, err := h.svc.GetReport(ctx, req.ReportId)
	if errors.Is(err, domain.ErrReportNotFound) {
		return gen.GetProjectReport404JSONResponse{NotFoundJSONResponse: reportNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	rb, err := reportBody(r)
	if err != nil {
		return nil, err
	}
	pid, err := uuidOf(p.ID)
	if err != nil {
		return nil, err
	}
	skills, err := tagsBody(p.Skills)
	if err != nil {
		return nil, err
	}
	return gen.GetProjectReport200JSONResponse{Report: rb, Project: gen.ReportedProject{
		Id: pid, Title: p.Title, Description: p.Description, Abstract: p.Abstract, Status: gen.ProjectStatus(p.Status),
		RequiredSkills: skills, MemberCount: p.MemberCount, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}}, nil
}

// ResolveProjectReport closes a project report (FR-15.11).
func (h *Handler) ResolveProjectReport(ctx context.Context, req gen.ResolveProjectReportRequestObject) (gen.ResolveProjectReportResponseObject, error) {
	adminID, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ResolveProjectReport401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.ResolveProjectReport400JSONResponse(badRequest), nil
	}
	r, err := h.svc.ResolveReport(ctx, adminID, req.ReportId, string(req.Body.Outcome), req.Body.Note)
	var field *domain.FieldError
	switch {
	case errors.As(err, &field):
		return gen.ResolveProjectReport400JSONResponse(fieldError(field.Field)), nil
	case errors.Is(err, domain.ErrReportNotFound):
		return gen.ResolveProjectReport404JSONResponse{NotFoundJSONResponse: reportNotFound}, nil
	case errors.Is(err, domain.ErrAlreadyResolved):
		return gen.ResolveProjectReport409JSONResponse{Code: CodeAlreadyResolved, Message: "This report is already resolved."}, nil
	case err != nil:
		return nil, err
	}
	b, err := reportBody(r)
	if err != nil {
		return nil, err
	}
	return gen.ResolveProjectReport200JSONResponse(b), nil
}

func projectBody(p domain.Project) (gen.Project, error) {
	id, err := uuidOf(p.ID)
	if err != nil {
		return gen.Project{}, err
	}
	owner, err := personBody(p.Owner)
	if err != nil {
		return gen.Project{}, err
	}
	skills, err := tagsBody(p.Skills)
	if err != nil {
		return gen.Project{}, err
	}
	return gen.Project{
		Id: id, Title: p.Title, Description: p.Description, Abstract: p.Abstract, Status: gen.ProjectStatus(p.Status),
		RequiredSkills: skills, Owner: owner, MyRole: roleBody(p.MyRole), MemberCount: p.MemberCount,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}, nil
}

func summaryBody(s domain.Summary) (gen.ProjectSummary, error) {
	id, err := uuidOf(s.ID)
	if err != nil {
		return gen.ProjectSummary{}, err
	}
	owner, err := personBody(s.Owner)
	if err != nil {
		return gen.ProjectSummary{}, err
	}
	skills := s.Skills
	if skills == nil {
		skills = []string{}
	}
	return gen.ProjectSummary{
		Id: id, Title: s.Title, Description: s.Description, Status: gen.ProjectStatus(s.Status), RequiredSkills: skills,
		Owner: owner, MyRole: roleBody(s.MyRole), CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}, nil
}

func reportBody(r domain.Report) (gen.ProjectReport, error) {
	id, err := uuidOf(r.ID)
	if err != nil {
		return gen.ProjectReport{}, err
	}
	pid, err := uuidOf(r.ProjectID)
	if err != nil {
		return gen.ProjectReport{}, err
	}
	reporter, err := reportPersonBody(r.Reporter)
	if err != nil {
		return gen.ProjectReport{}, err
	}
	owner, err := reportPersonBody(r.Owner)
	if err != nil {
		return gen.ProjectReport{}, err
	}
	status := gen.ProjectReportStatus("open")
	if r.ResolvedAt != nil {
		status = "resolved"
	}
	var outcome *gen.ProjectReportOutcome
	if r.Outcome != nil {
		o := gen.ProjectReportOutcome(*r.Outcome)
		outcome = &o
	}
	return gen.ProjectReport{
		Id: id, ProjectId: pid, ProjectTitle: r.ProjectTitle, Status: status, Reason: gen.ReportReason(r.Reason),
		Details: r.Details, Reporter: reporter, Owner: owner, CreatedAt: r.CreatedAt,
		Outcome: outcome, Note: r.Note, ResolvedAt: r.ResolvedAt,
	}, nil
}

func personBody(p domain.Person) (gen.ProjectPerson, error) {
	uid, err := uuidOf(p.UserID)
	if err != nil {
		return gen.ProjectPerson{}, err
	}
	return gen.ProjectPerson{UserId: uid, FullName: p.FullName, Headline: p.Headline, PhotoUrl: mediaURL(p.PhotoFileID)}, nil
}

func reportPersonBody(p domain.ReportPerson) (gen.MessageReportPerson, error) {
	uid, err := uuidOf(p.UserID)
	if err != nil {
		return gen.MessageReportPerson{}, err
	}
	return gen.MessageReportPerson{
		UserId: uid, FullName: p.FullName, Headline: p.Headline, PhotoUrl: mediaURL(p.PhotoFileID), Role: gen.Role(p.Role),
	}, nil
}

func tagsBody(skills []domain.Skill) ([]gen.Tag, error) {
	out := make([]gen.Tag, len(skills))
	for i, s := range skills {
		id, err := uuidOf(s.ID)
		if err != nil {
			return nil, err
		}
		out[i] = gen.Tag{Id: id, Name: s.Name}
	}
	return out, nil
}

func roleBody(r domain.Role) *gen.ProjectRole {
	if r == domain.RoleNone {
		return nil
	}
	v := gen.ProjectRole(r)
	return &v
}

func uuidOf(s string) (openapi_types.UUID, error) {
	var u openapi_types.UUID
	if err := u.UnmarshalText([]byte(s)); err != nil {
		return u, fmt.Errorf("project id %q: %w", s, err)
	}
	return u, nil
}

// mediaURL is where the client reads a profile photo (FR-4.6), nil when none is set.
func mediaURL(fileID *string) *string {
	if fileID == nil {
		return nil
	}
	u := httpx.MediaURL(*fileID)
	return &u
}
