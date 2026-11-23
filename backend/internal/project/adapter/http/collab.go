package http

import (
	"context"
	"errors"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Real-time editing (slice 22, docs/sync.md): the members' ticket, and the internal
// operations only the sync server calls, on the API's internal listener with its token.

// CollabService is what the real-time operations need from the usecase.
type CollabService interface {
	CollabTicket(ctx context.Context, userID, projectID, fileID string) (domain.IssuedTicket, error)
	RedeemTicket(ctx context.Context, ticket, fileID string) (domain.SyncSession, error)
	SyncAccess(ctx context.Context, checks []domain.AccessCheck) ([]domain.Role, error)
	LoadCollabFile(ctx context.Context, fileID string) (domain.CollabFile, error)
	StoreCollabFile(ctx context.Context, fileID string, st domain.CollabState) (int, error)
}

var ticketNotFound = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This ticket is not valid."}

// CreateCollabTicket gives a member a one-time ticket for the sync server (FR-12.7).
func (h *Handler) CreateCollabTicket(ctx context.Context, req gen.CreateCollabTicketRequestObject) (gen.CreateCollabTicketResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.CreateCollabTicket401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	t, err := h.files.CollabTicket(ctx, id, req.ProjectId, req.FileId)
	switch e := classifyFile(err); e.status {
	case 403:
		return gen.CreateCollabTicket403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: e.forbidden()}, nil
	case 404:
		return gen.CreateCollabTicket404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
	case 409:
		body := e.body
		if errors.Is(err, domain.ErrNotTextFile) {
			body.Message = "Only text files can be edited together."
		}
		return gen.CreateCollabTicket409JSONResponse(body), nil
	case 429:
		return gen.CreateCollabTicket429JSONResponse{
			ProjectFileRateLimitedJSONResponse: e.rateLimited("You are opening files too often. Try again in a moment."),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	body := gen.CollabTicket{
		Ticket: t.Ticket, ExpiresAt: t.ExpiresAt, Role: gen.ProjectRole(t.Role), DisplayName: t.DisplayName,
	}
	if t.Epoch != nil {
		u, err := uuidOf(*t.Epoch)
		if err != nil {
			return nil, err
		}
		body.Epoch = &u
	}
	return gen.CreateCollabTicket200JSONResponse{Body: body, Headers: gen.CreateCollabTicket200ResponseHeaders{CacheControl: "no-store"}}, nil
}

// RedeemSyncTicket spends a ticket for the sync server (internal).
func (h *Handler) RedeemSyncTicket(ctx context.Context, req gen.RedeemSyncTicketRequestObject) (gen.RedeemSyncTicketResponseObject, error) {
	if req.Body == nil {
		return gen.RedeemSyncTicket400JSONResponse{BadSyncRequestJSONResponse: gen.BadSyncRequestJSONResponse(badRequest)}, nil
	}
	s, err := h.files.RedeemTicket(ctx, req.Body.Ticket, req.Body.FileId.String())
	if errors.Is(err, domain.ErrTicketNotFound) {
		return gen.RedeemSyncTicket404JSONResponse{NotFoundJSONResponse: ticketNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	user, err1 := uuidOf(s.UserID)
	project, err2 := uuidOf(s.ProjectID)
	if err := errors.Join(err1, err2); err != nil {
		return nil, err
	}
	return gen.RedeemSyncTicket200JSONResponse{
		UserId: user, ProjectId: project, FileId: req.Body.FileId, Role: gen.ProjectRole(s.Role), DisplayName: s.DisplayName,
	}, nil
}

// CheckSyncAccess reads the current role of users on files (internal).
func (h *Handler) CheckSyncAccess(ctx context.Context, req gen.CheckSyncAccessRequestObject) (gen.CheckSyncAccessResponseObject, error) {
	if req.Body == nil {
		return gen.CheckSyncAccess400JSONResponse{BadSyncRequestJSONResponse: gen.BadSyncRequestJSONResponse(badRequest)}, nil
	}
	checks := make([]domain.AccessCheck, len(req.Body.Checks))
	for i, c := range req.Body.Checks {
		checks[i] = domain.AccessCheck{UserID: c.UserId.String(), FileID: c.FileId.String()}
	}
	roles, err := h.files.SyncAccess(ctx, checks)
	if field, ok := errors.AsType[*domain.FieldError](err); ok {
		return gen.CheckSyncAccess400JSONResponse{BadSyncRequestJSONResponse: gen.BadSyncRequestJSONResponse(fieldError(field.Field))}, nil
	}
	if err != nil {
		return nil, err
	}
	out := gen.SyncAccessResult{Results: make([]gen.SyncAccessAnswer, len(roles))}
	for i, r := range roles {
		role := gen.SyncRoleNone
		if r != domain.RoleNone {
			role = gen.SyncRole(r)
		}
		out.Results[i] = gen.SyncAccessAnswer{UserId: req.Body.Checks[i].UserId, FileId: req.Body.Checks[i].FileId, Role: role}
	}
	return gen.CheckSyncAccess200JSONResponse(out), nil
}

// LoadSyncFile reads a text file for the sync server (internal).
func (h *Handler) LoadSyncFile(ctx context.Context, req gen.LoadSyncFileRequestObject) (gen.LoadSyncFileResponseObject, error) {
	f, err := h.files.LoadCollabFile(ctx, req.FileId)
	if errors.Is(err, domain.ErrFileNotFound) {
		return gen.LoadSyncFile404JSONResponse{NotFoundJSONResponse: fileNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	project, err := uuidOf(f.ProjectID)
	if err != nil {
		return nil, err
	}
	body := gen.SyncFile{ProjectId: project, Content: f.Content, Version: f.Version}
	if f.State != nil {
		st := f.State
		body.YdocState = &st
	}
	if f.Epoch != nil {
		u, err := uuidOf(*f.Epoch)
		if err != nil {
			return nil, err
		}
		body.YdocEpoch = &u
	}
	return gen.LoadSyncFile200JSONResponse(body), nil
}

// StoreSyncFile stores the sync server's state and text (internal, NFR-10.1).
func (h *Handler) StoreSyncFile(ctx context.Context, req gen.StoreSyncFileRequestObject) (gen.StoreSyncFileResponseObject, error) {
	if req.Body == nil {
		return gen.StoreSyncFile400JSONResponse{BadSyncRequestJSONResponse: gen.BadSyncRequestJSONResponse(badRequest)}, nil
	}
	b := req.Body
	st := domain.CollabState{State: b.YdocState, Epoch: b.YdocEpoch.String(), Content: b.Content, BaseVersion: b.BaseVersion}
	if b.UpdatedBy != nil {
		by := b.UpdatedBy.String()
		st.UpdatedBy = &by
	}
	v, err := h.files.StoreCollabFile(ctx, req.FileId, st)
	var field *domain.FieldError
	switch {
	case errors.As(err, &field):
		return gen.StoreSyncFile400JSONResponse{BadSyncRequestJSONResponse: gen.BadSyncRequestJSONResponse(fieldError(field.Field))}, nil
	case errors.Is(err, domain.ErrFileNotFound):
		return gen.StoreSyncFile404JSONResponse{NotFoundJSONResponse: fileNotFound}, nil
	case errors.Is(err, domain.ErrStaleVersion):
		return gen.StoreSyncFile409JSONResponse{Code: CodeVersionConflict, Message: "The file changed since the base version."}, nil
	case errors.Is(err, domain.ErrFileTooLarge):
		return gen.StoreSyncFile413JSONResponse{FileTooLargeJSONResponse: gen.FileTooLargeJSONResponse{
			Code: CodeFileTooLarge, Message: "A text file may be at most 1 MB and its real-time state 8 MB."}}, nil
	case err != nil:
		return nil, err
	}
	return gen.StoreSyncFile200JSONResponse{Version: v}, nil
}
