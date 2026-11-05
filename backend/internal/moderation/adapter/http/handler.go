// Package http serves the moderation operations in docs/openapi.yaml (FR-15.3, FR-15.5 to
// FR-15.10). It turns the usecase's errors into the contract's codes. cmd/api guards each route
// with its platform action (SRS 2.3) before it gets here.
package http

import (
	"context"
	"errors"
	"fmt"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// Codes of the moderation operations.
const (
	CodeCannotModerateAdmin  = "cannot_moderate_admin"
	CodeAlreadyRestricted    = "already_restricted"
	CodeAlreadySuspended     = "already_suspended"
	CodeNotSuspended         = "not_suspended"
	CodeRestrictionNotActive = "restriction_not_active"
	CodeReportResolved       = "report_already_resolved"
)

// Service is what the handler needs from the usecase.
type Service interface {
	Warn(ctx context.Context, adminID, userID string, in domain.Input) (domain.Action, error)
	Restrict(ctx context.Context, adminID, userID string, in domain.RestrictInput) (domain.Action, error)
	Lift(ctx context.Context, adminID, restrictionID string, in domain.ReverseInput) error
	Suspend(ctx context.Context, adminID, userID string, in domain.SuspendInput) (domain.Action, error)
	Restore(ctx context.Context, adminID, userID string, in domain.ReverseInput) (domain.Action, error)
	History(ctx context.Context, userID string, page int) (domain.History, error)
	PendingWarnings(ctx context.Context, userID string) ([]domain.Warning, error)
	Warning(ctx context.Context, userID, id string) (domain.Warning, error)
	Acknowledge(ctx context.Context, userID, id string) error
}

// Handler implements the moderation operations of gen.StrictServerInterface.
type Handler struct {
	svc Service
}

// NewHandler returns a Handler that uses svc.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

var (
	unauthenticated = gen.UnauthorizedJSONResponse{Code: httpx.CodeUnauthenticated, Message: "Log in to continue."}
	notFound        = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This could not be found."}
	badRequest      = gen.Error{Code: httpx.CodeBadRequest, Message: "The request is not valid."}
)

// refusal is the contract's answer to one of the action errors.
type refusal struct {
	invalid   *gen.ModerationInvalidJSONResponse
	forbidden *gen.ModerationForbiddenJSONResponse
	notFound  bool
	conflict  *gen.ModerationConflictJSONResponse
}

var fieldMessages = map[string]string{
	"reason":   "The reason must have 10 to 1,000 characters.",
	"note":     "The note can have at most 1,000 characters.",
	"kind":     "This kind of restriction does not exist.",
	"days":     "A restriction lasts 1 to 90 days.",
	"until":    "A suspension ends 1 to 365 days from now.",
	"reportId": "This report is about another user.",
}

// refusalOf sorts the errors of the actions; nil for any other error.
func refusalOf(err error) *refusal {
	var field *domain.InvalidFieldError
	conflict := func(code, message string) *refusal {
		return &refusal{conflict: &gen.ModerationConflictJSONResponse{Code: code, Message: message}}
	}
	switch {
	case errors.As(err, &field):
		name := field.Field
		return &refusal{invalid: &gen.ModerationInvalidJSONResponse{Code: httpx.CodeBadRequest, Message: fieldMessages[name], Field: &name}}
	case errors.Is(err, domain.ErrCannotModerateAdmin):
		return &refusal{forbidden: &gen.ModerationForbiddenJSONResponse{Code: CodeCannotModerateAdmin, Message: "Administrators cannot be moderated."}}
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrReportNotFound):
		return &refusal{notFound: true}
	case errors.Is(err, domain.ErrAlreadyRestricted):
		return conflict(CodeAlreadyRestricted, "This user already has an active restriction. Lift it to change it.")
	case errors.Is(err, domain.ErrAlreadySuspended):
		return conflict(CodeAlreadySuspended, "This account is already suspended. Restore it to change the suspension.")
	case errors.Is(err, domain.ErrNotSuspended):
		return conflict(CodeNotSuspended, "This account is not suspended.")
	case errors.Is(err, domain.ErrRestrictionNotActive):
		return conflict(CodeRestrictionNotActive, "This restriction has already ended.")
	case errors.Is(err, domain.ErrReportResolved):
		return conflict(CodeReportResolved, "This report is already resolved.")
	}
	return nil
}

// WarnUser warns a user (FR-15.5).
func (h *Handler) WarnUser(ctx context.Context, req gen.WarnUserRequestObject) (gen.WarnUserResponseObject, error) {
	admin, ok := httpx.UserID(ctx)
	if !ok {
		return gen.WarnUser401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.WarnUser400JSONResponse{ModerationInvalidJSONResponse: gen.ModerationInvalidJSONResponse(badRequest)}, nil
	}
	a, err := h.svc.Warn(ctx, admin, req.UserId, domain.Input{Reason: req.Body.Reason, Note: req.Body.Note, ReportID: req.Body.ReportId})
	if r := refusalOf(err); r != nil {
		switch {
		case r.invalid != nil:
			return gen.WarnUser400JSONResponse{ModerationInvalidJSONResponse: *r.invalid}, nil
		case r.forbidden != nil:
			return gen.WarnUser403JSONResponse{ModerationForbiddenJSONResponse: *r.forbidden}, nil
		case r.notFound:
			return gen.WarnUser404JSONResponse{NotFoundJSONResponse: notFound}, nil
		default:
			return gen.WarnUser409JSONResponse{ModerationConflictJSONResponse: *r.conflict}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	b, err := actionBody(a)
	if err != nil {
		return nil, err
	}
	return gen.WarnUser201JSONResponse(b), nil
}

// RestrictUser stops a user sending messages (FR-15.6).
func (h *Handler) RestrictUser(ctx context.Context, req gen.RestrictUserRequestObject) (gen.RestrictUserResponseObject, error) {
	admin, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RestrictUser401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.RestrictUser400JSONResponse{ModerationInvalidJSONResponse: gen.ModerationInvalidJSONResponse(badRequest)}, nil
	}
	a, err := h.svc.Restrict(ctx, admin, req.UserId, domain.RestrictInput{
		Input: domain.Input{Reason: req.Body.Reason, Note: req.Body.Note, ReportID: req.Body.ReportId},
		Kind:  string(req.Body.Kind), Days: req.Body.Days,
	})
	if r := refusalOf(err); r != nil {
		switch {
		case r.invalid != nil:
			return gen.RestrictUser400JSONResponse{ModerationInvalidJSONResponse: *r.invalid}, nil
		case r.forbidden != nil:
			return gen.RestrictUser403JSONResponse{ModerationForbiddenJSONResponse: *r.forbidden}, nil
		case r.notFound:
			return gen.RestrictUser404JSONResponse{NotFoundJSONResponse: notFound}, nil
		default:
			return gen.RestrictUser409JSONResponse{ModerationConflictJSONResponse: *r.conflict}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	b, err := actionBody(a)
	if err != nil {
		return nil, err
	}
	return gen.RestrictUser201JSONResponse(b), nil
}

// LiftRestriction ends a restriction early (FR-15.6).
func (h *Handler) LiftRestriction(ctx context.Context, req gen.LiftRestrictionRequestObject) (gen.LiftRestrictionResponseObject, error) {
	admin, ok := httpx.UserID(ctx)
	if !ok {
		return gen.LiftRestriction401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.LiftRestriction400JSONResponse{ModerationInvalidJSONResponse: gen.ModerationInvalidJSONResponse(badRequest)}, nil
	}
	err := h.svc.Lift(ctx, admin, req.RestrictionId, domain.ReverseInput{Reason: req.Body.Reason, Note: req.Body.Note})
	if r := refusalOf(err); r != nil {
		switch {
		case r.invalid != nil:
			return gen.LiftRestriction400JSONResponse{ModerationInvalidJSONResponse: *r.invalid}, nil
		case r.forbidden != nil:
			return gen.LiftRestriction403JSONResponse{ModerationForbiddenJSONResponse: *r.forbidden}, nil
		case r.notFound:
			return gen.LiftRestriction404JSONResponse{NotFoundJSONResponse: notFound}, nil
		default:
			return gen.LiftRestriction409JSONResponse{ModerationConflictJSONResponse: *r.conflict}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return gen.LiftRestriction204Response{}, nil
}

// SuspendUser suspends an account (FR-15.3, FR-15.7).
func (h *Handler) SuspendUser(ctx context.Context, req gen.SuspendUserRequestObject) (gen.SuspendUserResponseObject, error) {
	admin, ok := httpx.UserID(ctx)
	if !ok {
		return gen.SuspendUser401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.SuspendUser400JSONResponse{ModerationInvalidJSONResponse: gen.ModerationInvalidJSONResponse(badRequest)}, nil
	}
	a, err := h.svc.Suspend(ctx, admin, req.UserId, domain.SuspendInput{
		Input: domain.Input{Reason: req.Body.Reason, Note: req.Body.Note, ReportID: req.Body.ReportId},
		Until: req.Body.Until,
	})
	if r := refusalOf(err); r != nil {
		switch {
		case r.invalid != nil:
			return gen.SuspendUser400JSONResponse{ModerationInvalidJSONResponse: *r.invalid}, nil
		case r.forbidden != nil:
			return gen.SuspendUser403JSONResponse{ModerationForbiddenJSONResponse: *r.forbidden}, nil
		case r.notFound:
			return gen.SuspendUser404JSONResponse{NotFoundJSONResponse: notFound}, nil
		default:
			return gen.SuspendUser409JSONResponse{ModerationConflictJSONResponse: *r.conflict}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	b, err := actionBody(a)
	if err != nil {
		return nil, err
	}
	return gen.SuspendUser200JSONResponse(b), nil
}

// RestoreUser ends a suspension (FR-15.3).
func (h *Handler) RestoreUser(ctx context.Context, req gen.RestoreUserRequestObject) (gen.RestoreUserResponseObject, error) {
	admin, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RestoreUser401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.RestoreUser400JSONResponse{ModerationInvalidJSONResponse: gen.ModerationInvalidJSONResponse(badRequest)}, nil
	}
	a, err := h.svc.Restore(ctx, admin, req.UserId, domain.ReverseInput{Reason: req.Body.Reason, Note: req.Body.Note})
	if r := refusalOf(err); r != nil {
		switch {
		case r.invalid != nil:
			return gen.RestoreUser400JSONResponse{ModerationInvalidJSONResponse: *r.invalid}, nil
		case r.forbidden != nil:
			return gen.RestoreUser403JSONResponse{ModerationForbiddenJSONResponse: *r.forbidden}, nil
		case r.notFound:
			return gen.RestoreUser404JSONResponse{NotFoundJSONResponse: notFound}, nil
		default:
			return gen.RestoreUser409JSONResponse{ModerationConflictJSONResponse: *r.conflict}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	b, err := actionBody(a)
	if err != nil {
		return nil, err
	}
	return gen.RestoreUser200JSONResponse(b), nil
}

// GetUserModeration returns a user's history, newest first (FR-15.9).
func (h *Handler) GetUserModeration(ctx context.Context, req gen.GetUserModerationRequestObject) (gen.GetUserModerationResponseObject, error) {
	if _, ok := httpx.UserID(ctx); !ok {
		return gen.GetUserModeration401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	page := 1
	if req.Params.Page != nil {
		page = *req.Params.Page
	}
	res, err := h.svc.History(ctx, req.UserId, page)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.GetUserModeration404JSONResponse{NotFoundJSONResponse: notFound}, nil
	case errors.Is(err, domain.ErrInvalidPage):
		return gen.GetUserModeration400JSONResponse{Code: httpx.CodeBadRequest, Message: "Page must be a whole number of at least 1."}, nil
	case err != nil:
		return nil, err
	}
	items := make([]gen.ModerationAction, len(res.Items))
	for i, a := range res.Items {
		if items[i], err = actionBody(a); err != nil {
			return nil, err
		}
	}
	return gen.GetUserModeration200JSONResponse{Items: items, Page: res.Page, PageSize: res.PageSize, Total: res.Total}, nil
}

// ListPendingWarnings lists the caller's warnings not yet acknowledged (FR-15.5).
func (h *Handler) ListPendingWarnings(ctx context.Context, _ gen.ListPendingWarningsRequestObject) (gen.ListPendingWarningsResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListPendingWarnings401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	warnings, err := h.svc.PendingWarnings(ctx, id)
	if err != nil {
		return nil, err
	}
	items := make([]gen.AccountWarning, len(warnings))
	for i, w := range warnings {
		if items[i], err = warningBody(w); err != nil {
			return nil, err
		}
	}
	return gen.ListPendingWarnings200JSONResponse{Items: items}, nil
}

// GetMyWarning returns one of the caller's warnings (FR-15.5).
func (h *Handler) GetMyWarning(ctx context.Context, req gen.GetMyWarningRequestObject) (gen.GetMyWarningResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.GetMyWarning401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	w, err := h.svc.Warning(ctx, id, req.WarningId)
	if errors.Is(err, domain.ErrNotFound) {
		return gen.GetMyWarning404JSONResponse{NotFoundJSONResponse: notFound}, nil
	}
	if err != nil {
		return nil, err
	}
	b, err := warningBody(w)
	if err != nil {
		return nil, err
	}
	return gen.GetMyWarning200JSONResponse(b), nil
}

// AcknowledgeWarning acknowledges one of the caller's warnings (FR-15.5).
func (h *Handler) AcknowledgeWarning(ctx context.Context, req gen.AcknowledgeWarningRequestObject) (gen.AcknowledgeWarningResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.AcknowledgeWarning401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.svc.Acknowledge(ctx, id, req.WarningId)
	if errors.Is(err, domain.ErrNotFound) {
		return gen.AcknowledgeWarning404JSONResponse{NotFoundJSONResponse: notFound}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.AcknowledgeWarning204Response{}, nil
}

func warningBody(w domain.Warning) (gen.AccountWarning, error) {
	id, err := uuid(w.ID)
	if err != nil {
		return gen.AccountWarning{}, err
	}
	return gen.AccountWarning{Id: id, Reason: w.Reason, CreatedAt: w.CreatedAt, Acknowledged: w.AcknowledgedAt != nil}, nil
}

func actionBody(a domain.Action) (gen.ModerationAction, error) {
	id, err := uuid(a.ID)
	if err != nil {
		return gen.ModerationAction{}, err
	}
	user, err := uuid(a.UserID)
	if err != nil {
		return gen.ModerationAction{}, err
	}
	b := gen.ModerationAction{
		Id: id, UserId: user, Action: gen.ModerationActionKind(a.Kind), Reason: a.Reason, Note: a.Note,
		AdminName: a.AdminName, EndsAt: a.EndsAt, AcknowledgedAt: a.AcknowledgedAt, LiftedAt: a.LiftedAt, CreatedAt: a.CreatedAt,
	}
	for _, f := range []struct {
		from *string
		to   **openapi_types.UUID
	}{{a.AdminID, &b.AdminId}, {a.ReportID, &b.ReportId}, {a.WarningID, &b.WarningId}, {a.RestrictionID, &b.RestrictionId}} {
		if f.from == nil {
			continue
		}
		v, err := uuid(*f.from)
		if err != nil {
			return gen.ModerationAction{}, err
		}
		*f.to = &v
	}
	if a.Report != nil {
		rid, err := uuid(a.Report.ID)
		if err != nil {
			return gen.ModerationAction{}, err
		}
		b.Report = &gen.ModerationLinkedReport{
			Id: rid, Reason: gen.ReportReason(a.Report.Reason), Outcome: gen.ModerationLinkedReportOutcome(a.Report.Outcome), CreatedAt: a.Report.CreatedAt,
		}
	}
	return b, nil
}

func uuid(s string) (openapi_types.UUID, error) {
	var id openapi_types.UUID
	if err := id.UnmarshalText([]byte(s)); err != nil {
		return id, fmt.Errorf("uuid %q: %w", s, err)
	}
	return id, nil
}
