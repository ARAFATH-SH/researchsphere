package http

import (
	"context"
	"errors"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/usecase"
)

// Error codes of the membership operations (FR-11, FR-10.5).
const (
	CodeAlreadyMember        = "already_member"
	CodeProjectNotOpen       = "project_not_open"
	CodeRequestPending       = "request_pending"
	CodeInvitationPending    = "invitation_pending"
	CodeAlreadyInvited       = "already_invited"
	CodeRequestNotPending    = "request_not_pending"
	CodeInvitationNotPending = "invitation_not_pending"
	CodeUseTransfer          = "use_transfer"
	CodeCannotRemoveOwner    = "cannot_remove_owner"
	CodeOwnerMustTransfer    = "owner_must_transfer"
	CodeNotAMember           = "not_a_member"
	CodeAlreadyOwner         = "already_owner"
	CodeMemberUnavailable    = "member_unavailable"
	CodeUserNotFound         = "user_not_found"
)

// MembershipService is the part of the usecase the membership handlers call.
type MembershipService interface {
	RequestToJoin(ctx context.Context, userID, projectID string, message *string) (domain.MyRequestItem, error)
	CancelRequest(ctx context.Context, userID, projectID, requestID string) error
	DecideRequest(ctx context.Context, ownerID, projectID, requestID string, accept bool, role domain.Role) (domain.Member, error)
	Invite(ctx context.Context, ownerID, projectID string, in usecase.InviteInput) (domain.RequestItem, error)
	CancelInvitation(ctx context.Context, ownerID, projectID, invitationID string) error
	AnswerInvitation(ctx context.Context, userID, projectID, invitationID string, accept bool) (domain.Member, error)
	ChangeRole(ctx context.Context, ownerID, projectID, memberID string, role domain.Role) (domain.Member, error)
	RemoveMember(ctx context.Context, ownerID, projectID, memberID string) error
	Leave(ctx context.Context, userID, projectID string) error
	Transfer(ctx context.Context, ownerID, projectID, newOwnerID string) (domain.Project, error)
	Members(ctx context.Context, viewerID, projectID string, page, pageSize int) (usecase.MemberPage, error)
	ProjectRequests(ctx context.Context, ownerID, projectID string, kind domain.RequestKind, page, pageSize int) (usecase.RequestPage, error)
	MyRequests(ctx context.Context, userID string, page, pageSize int) (usecase.MyRequestPage, error)
	MyInvitations(ctx context.Context, userID string, page, pageSize int) (usecase.MyRequestPage, error)
}

var (
	requestNotFound = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This request or invitation does not exist."}
	memberNotFound  = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This person is not a member of the project."}
	userNotFound    = gen.NotFoundJSONResponse{Code: CodeUserNotFound, Message: "This person cannot be invited."}
	invalidPage     = gen.Error{Code: httpx.CodeBadRequest, Message: "The page is not valid."}
)

// conflicts maps each membership refusal to its 409 answer.
var conflicts = []struct {
	err  error
	body gen.Error
}{
	{domain.ErrAlreadyMember, gen.Error{Code: CodeAlreadyMember, Message: "This person is already a member of the project."}},
	{domain.ErrProjectNotOpen, gen.Error{Code: CodeProjectNotOpen, Message: "This project is not open to new members."}},
	{domain.ErrRequestPending, gen.Error{Code: CodeRequestPending, Message: "A request to join is already waiting for an answer."}},
	{domain.ErrInvitationPending, gen.Error{Code: CodeInvitationPending, Message: "You are invited to this project. Accept the invitation instead."}},
	{domain.ErrAlreadyInvited, gen.Error{Code: CodeAlreadyInvited, Message: "This person is already invited."}},
	{domain.ErrRequestNotPending, gen.Error{Code: CodeRequestNotPending, Message: "This request has already been answered."}},
	{domain.ErrInvitationNotPending, gen.Error{Code: CodeInvitationNotPending, Message: "This invitation has already been answered."}},
	{domain.ErrUseTransfer, gen.Error{Code: CodeUseTransfer, Message: "The owner's role changes only by transferring ownership."}},
	{domain.ErrCannotRemoveOwner, gen.Error{Code: CodeCannotRemoveOwner, Message: "The owner cannot be removed."}},
	{domain.ErrOwnerMustTransfer, gen.Error{Code: CodeOwnerMustTransfer, Message: "Transfer ownership before leaving, or delete the project."}},
	{domain.ErrNotAMember, gen.Error{Code: CodeNotAMember, Message: "Ownership can go only to a member of the project."}},
	{domain.ErrAlreadyOwner, gen.Error{Code: CodeAlreadyOwner, Message: "You already own this project."}},
	{domain.ErrMemberUnavailable, gen.Error{Code: CodeMemberUnavailable, Message: "This member cannot take ownership now."}},
}

// conflict returns the 409 body for err, false when err is not a membership refusal.
func conflict(err error) (gen.MembershipConflictJSONResponse, bool) {
	for _, c := range conflicts {
		if errors.Is(err, c.err) {
			return gen.MembershipConflictJSONResponse(c.body), true
		}
	}
	return gen.MembershipConflictJSONResponse{}, false
}

func suspendedBody(e *domain.SuspendedError) gen.AccountSuspendedOrForbiddenJSONResponse {
	return gen.AccountSuspendedOrForbiddenJSONResponse{
		Code: CodeAccountSuspended, Message: "This account is suspended. Contact support.", Reason: e.Reason, Until: e.Until,
	}
}

func pageOf(page *gen.PageFromOne, size *gen.MembershipPageSize) (int, int) {
	p, s := 1, usecase.DefaultPageSize
	if page != nil {
		p = *page
	}
	if size != nil {
		s = *size
	}
	return p, s
}

// membershipError is what each operation answers for a usecase error; status 0 means err is
// not one of them.
type membershipError struct {
	status int
	body   gen.Error
	field  *domain.FieldError
	susp   *domain.SuspendedError
	limit  *domain.RateLimitedError
}

func classify(err error) membershipError {
	var (
		field     *domain.FieldError
		suspended *domain.SuspendedError
		limited   *domain.RateLimitedError
	)
	switch {
	case errors.As(err, &field):
		return membershipError{status: 400, body: fieldError(field.Field), field: field}
	case errors.As(err, &suspended):
		return membershipError{status: 403, susp: suspended}
	case errors.Is(err, domain.ErrForbidden):
		return membershipError{status: 403, body: gen.Error(forbidden)}
	case errors.Is(err, domain.ErrNotFound):
		return membershipError{status: 404, body: gen.Error(notFound)}
	case errors.Is(err, domain.ErrRequestNotFound):
		return membershipError{status: 404, body: gen.Error(requestNotFound)}
	case errors.Is(err, domain.ErrMemberNotFound):
		return membershipError{status: 404, body: gen.Error(memberNotFound)}
	case errors.Is(err, domain.ErrUserNotFound):
		return membershipError{status: 404, body: gen.Error(userNotFound)}
	case errors.Is(err, domain.ErrInvalidPage):
		return membershipError{status: 400, body: invalidPage}
	case errors.As(err, &limited):
		return membershipError{status: 429, limit: limited}
	}
	if c, ok := conflict(err); ok {
		return membershipError{status: 409, body: gen.Error(c)}
	}
	return membershipError{}
}

func (e membershipError) forbidden() gen.AccountSuspendedOrForbiddenJSONResponse {
	if e.susp != nil {
		return suspendedBody(e.susp)
	}
	return gen.AccountSuspendedOrForbiddenJSONResponse(e.body)
}

// CreateJoinRequest asks to join a project (FR-11.1).
func (h *Handler) CreateJoinRequest(ctx context.Context, req gen.CreateJoinRequestRequestObject) (gen.CreateJoinRequestResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.CreateJoinRequest401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	var msg *string
	if req.Body != nil {
		msg = req.Body.Message
	}
	r, err := h.members.RequestToJoin(ctx, id, req.ProjectId, msg)
	if err != nil {
		switch e := classify(err); e.status {
		case 400:
			return gen.CreateJoinRequest400JSONResponse{MembershipInvalidJSONResponse: gen.MembershipInvalidJSONResponse(e.body)}, nil
		case 403:
			return gen.CreateJoinRequest403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: e.forbidden()}, nil
		case 404:
			return gen.CreateJoinRequest404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
		case 409:
			return gen.CreateJoinRequest409JSONResponse{MembershipConflictJSONResponse: gen.MembershipConflictJSONResponse(e.body)}, nil
		case 429:
			return gen.CreateJoinRequest429JSONResponse{MembershipRateLimitedJSONResponse: gen.MembershipRateLimitedJSONResponse{
				Body:    gen.Error{Code: CodeRateLimited, Message: "You have sent 20 join requests today. Try again later."},
				Headers: gen.MembershipRateLimitedResponseHeaders{RetryAfter: retryAfter(e.limit)},
			}}, nil
		}
		return nil, err
	}
	body, err := myRequestBody(r)
	if err != nil {
		return nil, err
	}
	return gen.CreateJoinRequest201JSONResponse(body), nil
}

// CancelJoinRequest cancels the caller's pending request (FR-11.1).
func (h *Handler) CancelJoinRequest(ctx context.Context, req gen.CancelJoinRequestRequestObject) (gen.CancelJoinRequestResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.CancelJoinRequest401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.members.CancelRequest(ctx, id, req.ProjectId, req.RequestId)
	if err != nil {
		if e := classify(err); e.status == 404 {
			return gen.CancelJoinRequest404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
		}
		return nil, err
	}
	return gen.CancelJoinRequest204Response{}, nil
}

// ListProjectJoinRequests lists a project's join requests for its owner (FR-11.2).
func (h *Handler) ListProjectJoinRequests(ctx context.Context, req gen.ListProjectJoinRequestsRequestObject) (gen.ListProjectJoinRequestsResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListProjectJoinRequests401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	page, size := pageOf(req.Params.Page, req.Params.PageSize)
	res, err := h.members.ProjectRequests(ctx, id, req.ProjectId, domain.KindRequest, page, size)
	if err != nil {
		switch e := classify(err); e.status {
		case 400:
			return gen.ListProjectJoinRequests400JSONResponse{MembershipInvalidJSONResponse: gen.MembershipInvalidJSONResponse(e.body)}, nil
		case 403:
			return gen.ListProjectJoinRequests403JSONResponse{ForbiddenJSONResponse: forbidden}, nil
		case 404:
			return gen.ListProjectJoinRequests404JSONResponse{NotFoundJSONResponse: notFound}, nil
		}
		return nil, err
	}
	body := gen.ProjectJoinRequestPage{Page: res.Page, PageSize: res.PageSize, Total: res.Total, Items: make([]gen.ProjectJoinRequest, len(res.Items))}
	for i, it := range res.Items {
		rid, err := uuidOf(it.ID)
		if err != nil {
			return nil, err
		}
		user, err := personBody(it.User)
		if err != nil {
			return nil, err
		}
		body.Items[i] = gen.ProjectJoinRequest{
			Id: rid, Status: gen.JoinRequestStatus(it.Status), Message: it.Message, User: user,
			CreatedAt: it.CreatedAt, DecidedAt: it.DecidedAt,
		}
	}
	return gen.ListProjectJoinRequests200JSONResponse(body), nil
}

// AcceptJoinRequest accepts a join request with a role (FR-11.2).
func (h *Handler) AcceptJoinRequest(ctx context.Context, req gen.AcceptJoinRequestRequestObject) (gen.AcceptJoinRequestResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.AcceptJoinRequest401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	var role domain.Role
	if req.Body != nil {
		role = domain.Role(req.Body.Role)
	}
	m, err := h.members.DecideRequest(ctx, id, req.ProjectId, req.RequestId, true, role)
	if err != nil {
		switch e := classify(err); e.status {
		case 400:
			return gen.AcceptJoinRequest400JSONResponse{MembershipInvalidJSONResponse: gen.MembershipInvalidJSONResponse(e.body)}, nil
		case 403:
			return gen.AcceptJoinRequest403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: e.forbidden()}, nil
		case 404:
			return gen.AcceptJoinRequest404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
		case 409:
			return gen.AcceptJoinRequest409JSONResponse{MembershipConflictJSONResponse: gen.MembershipConflictJSONResponse(e.body)}, nil
		}
		return nil, err
	}
	body, err := memberBody(m)
	if err != nil {
		return nil, err
	}
	return gen.AcceptJoinRequest200JSONResponse(body), nil
}

// RejectJoinRequest rejects a join request (FR-11.2).
func (h *Handler) RejectJoinRequest(ctx context.Context, req gen.RejectJoinRequestRequestObject) (gen.RejectJoinRequestResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RejectJoinRequest401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	_, err := h.members.DecideRequest(ctx, id, req.ProjectId, req.RequestId, false, domain.RoleNone)
	if err != nil {
		switch e := classify(err); e.status {
		case 403:
			return gen.RejectJoinRequest403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: e.forbidden()}, nil
		case 404:
			return gen.RejectJoinRequest404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
		case 409:
			return gen.RejectJoinRequest409JSONResponse{MembershipConflictJSONResponse: gen.MembershipConflictJSONResponse(e.body)}, nil
		}
		return nil, err
	}
	return gen.RejectJoinRequest204Response{}, nil
}

// CreateInvitation invites a user (FR-11.3).
func (h *Handler) CreateInvitation(ctx context.Context, req gen.CreateInvitationRequestObject) (gen.CreateInvitationResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.CreateInvitation401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.CreateInvitation400JSONResponse{MembershipInvalidJSONResponse: gen.MembershipInvalidJSONResponse(badRequest)}, nil
	}
	r, err := h.members.Invite(ctx, id, req.ProjectId, usecase.InviteInput{UserID: req.Body.UserId.String(), Role: domain.Role(req.Body.Role)})
	if err != nil {
		switch e := classify(err); e.status {
		case 400:
			return gen.CreateInvitation400JSONResponse{MembershipInvalidJSONResponse: gen.MembershipInvalidJSONResponse(e.body)}, nil
		case 403:
			return gen.CreateInvitation403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: e.forbidden()}, nil
		case 404:
			return gen.CreateInvitation404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
		case 409:
			return gen.CreateInvitation409JSONResponse{MembershipConflictJSONResponse: gen.MembershipConflictJSONResponse(e.body)}, nil
		case 429:
			return gen.CreateInvitation429JSONResponse{MembershipRateLimitedJSONResponse: gen.MembershipRateLimitedJSONResponse{
				Body:    gen.Error{Code: CodeRateLimited, Message: "You have sent 20 invitations today. Try again later."},
				Headers: gen.MembershipRateLimitedResponseHeaders{RetryAfter: retryAfter(e.limit)},
			}}, nil
		}
		return nil, err
	}
	body, err := invitationBody(r)
	if err != nil {
		return nil, err
	}
	return gen.CreateInvitation201JSONResponse(body), nil
}

// ListProjectInvitations lists a project's invitations for its owner (FR-11.3).
func (h *Handler) ListProjectInvitations(ctx context.Context, req gen.ListProjectInvitationsRequestObject) (gen.ListProjectInvitationsResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListProjectInvitations401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	page, size := pageOf(req.Params.Page, req.Params.PageSize)
	res, err := h.members.ProjectRequests(ctx, id, req.ProjectId, domain.KindInvitation, page, size)
	if err != nil {
		switch e := classify(err); e.status {
		case 400:
			return gen.ListProjectInvitations400JSONResponse{MembershipInvalidJSONResponse: gen.MembershipInvalidJSONResponse(e.body)}, nil
		case 403:
			return gen.ListProjectInvitations403JSONResponse{ForbiddenJSONResponse: forbidden}, nil
		case 404:
			return gen.ListProjectInvitations404JSONResponse{NotFoundJSONResponse: notFound}, nil
		}
		return nil, err
	}
	body := gen.ProjectInvitationPage{Page: res.Page, PageSize: res.PageSize, Total: res.Total, Items: make([]gen.ProjectInvitation, len(res.Items))}
	for i, it := range res.Items {
		b, err := invitationBody(it)
		if err != nil {
			return nil, err
		}
		body.Items[i] = b
	}
	return gen.ListProjectInvitations200JSONResponse(body), nil
}

// CancelInvitation cancels a pending invitation (FR-11.3).
func (h *Handler) CancelInvitation(ctx context.Context, req gen.CancelInvitationRequestObject) (gen.CancelInvitationResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.CancelInvitation401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.members.CancelInvitation(ctx, id, req.ProjectId, req.InvitationId)
	if err != nil {
		switch e := classify(err); e.status {
		case 403:
			return gen.CancelInvitation403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: e.forbidden()}, nil
		case 404:
			return gen.CancelInvitation404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
		case 409:
			return gen.CancelInvitation409JSONResponse{MembershipConflictJSONResponse: gen.MembershipConflictJSONResponse(e.body)}, nil
		}
		return nil, err
	}
	return gen.CancelInvitation204Response{}, nil
}

// AcceptInvitation joins the project with the invitation's role (FR-11.3).
func (h *Handler) AcceptInvitation(ctx context.Context, req gen.AcceptInvitationRequestObject) (gen.AcceptInvitationResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.AcceptInvitation401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	m, err := h.members.AnswerInvitation(ctx, id, req.ProjectId, req.InvitationId, true)
	if err != nil {
		switch e := classify(err); e.status {
		case 403:
			return gen.AcceptInvitation403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: e.forbidden()}, nil
		case 404:
			return gen.AcceptInvitation404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
		case 409:
			return gen.AcceptInvitation409JSONResponse{MembershipConflictJSONResponse: gen.MembershipConflictJSONResponse(e.body)}, nil
		}
		return nil, err
	}
	body, err := memberBody(m)
	if err != nil {
		return nil, err
	}
	return gen.AcceptInvitation200JSONResponse(body), nil
}

// RejectInvitation rejects an invitation (FR-11.3).
func (h *Handler) RejectInvitation(ctx context.Context, req gen.RejectInvitationRequestObject) (gen.RejectInvitationResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RejectInvitation401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	_, err := h.members.AnswerInvitation(ctx, id, req.ProjectId, req.InvitationId, false)
	if err != nil {
		switch e := classify(err); e.status {
		case 404:
			return gen.RejectInvitation404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
		case 409:
			return gen.RejectInvitation409JSONResponse{MembershipConflictJSONResponse: gen.MembershipConflictJSONResponse(e.body)}, nil
		}
		return nil, err
	}
	return gen.RejectInvitation204Response{}, nil
}

// ListProjectMembers lists a project's members (SRS 2.3).
func (h *Handler) ListProjectMembers(ctx context.Context, req gen.ListProjectMembersRequestObject) (gen.ListProjectMembersResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListProjectMembers401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	page, size := pageOf(req.Params.Page, req.Params.PageSize)
	res, err := h.members.Members(ctx, id, req.ProjectId, page, size)
	if err != nil {
		switch e := classify(err); e.status {
		case 400:
			return gen.ListProjectMembers400JSONResponse{MembershipInvalidJSONResponse: gen.MembershipInvalidJSONResponse(e.body)}, nil
		case 404:
			return gen.ListProjectMembers404JSONResponse{NotFoundJSONResponse: notFound}, nil
		}
		return nil, err
	}
	body := gen.ProjectMemberPage{Page: res.Page, PageSize: res.PageSize, Total: res.Total, Items: make([]gen.ProjectMember, len(res.Items))}
	for i, m := range res.Items {
		b, err := memberBody(m)
		if err != nil {
			return nil, err
		}
		body.Items[i] = b
	}
	return gen.ListProjectMembers200JSONResponse(body), nil
}

// UpdateProjectMember changes a member's role (FR-11.4).
func (h *Handler) UpdateProjectMember(ctx context.Context, req gen.UpdateProjectMemberRequestObject) (gen.UpdateProjectMemberResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.UpdateProjectMember401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	var role domain.Role
	if req.Body != nil {
		role = domain.Role(req.Body.Role)
	}
	m, err := h.members.ChangeRole(ctx, id, req.ProjectId, req.UserId, role)
	if err != nil {
		switch e := classify(err); e.status {
		case 400:
			return gen.UpdateProjectMember400JSONResponse{MembershipInvalidJSONResponse: gen.MembershipInvalidJSONResponse(e.body)}, nil
		case 403:
			return gen.UpdateProjectMember403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: e.forbidden()}, nil
		case 404:
			return gen.UpdateProjectMember404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
		case 409:
			return gen.UpdateProjectMember409JSONResponse{MembershipConflictJSONResponse: gen.MembershipConflictJSONResponse(e.body)}, nil
		}
		return nil, err
	}
	body, err := memberBody(m)
	if err != nil {
		return nil, err
	}
	return gen.UpdateProjectMember200JSONResponse(body), nil
}

// RemoveProjectMember removes a member (FR-11.4).
func (h *Handler) RemoveProjectMember(ctx context.Context, req gen.RemoveProjectMemberRequestObject) (gen.RemoveProjectMemberResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RemoveProjectMember401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.members.RemoveMember(ctx, id, req.ProjectId, req.UserId)
	if err != nil {
		switch e := classify(err); e.status {
		case 403:
			return gen.RemoveProjectMember403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: e.forbidden()}, nil
		case 404:
			return gen.RemoveProjectMember404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
		case 409:
			return gen.RemoveProjectMember409JSONResponse{MembershipConflictJSONResponse: gen.MembershipConflictJSONResponse(e.body)}, nil
		}
		return nil, err
	}
	return gen.RemoveProjectMember204Response{}, nil
}

// LeaveProject ends the caller's membership (FR-11.5).
func (h *Handler) LeaveProject(ctx context.Context, req gen.LeaveProjectRequestObject) (gen.LeaveProjectResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.LeaveProject401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.members.Leave(ctx, id, req.ProjectId)
	if err != nil {
		switch e := classify(err); e.status {
		case 404:
			return gen.LeaveProject404JSONResponse{NotFoundJSONResponse: notFound}, nil
		case 409:
			return gen.LeaveProject409JSONResponse{MembershipConflictJSONResponse: gen.MembershipConflictJSONResponse(e.body)}, nil
		}
		return nil, err
	}
	return gen.LeaveProject204Response{}, nil
}

// TransferProject makes another member the owner (FR-10.5).
func (h *Handler) TransferProject(ctx context.Context, req gen.TransferProjectRequestObject) (gen.TransferProjectResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.TransferProject401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.TransferProject400JSONResponse{MembershipInvalidJSONResponse: gen.MembershipInvalidJSONResponse(badRequest)}, nil
	}
	p, err := h.members.Transfer(ctx, id, req.ProjectId, req.Body.UserId.String())
	if err != nil {
		switch e := classify(err); e.status {
		case 403:
			return gen.TransferProject403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: e.forbidden()}, nil
		case 404:
			return gen.TransferProject404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(e.body)}, nil
		case 409:
			return gen.TransferProject409JSONResponse{MembershipConflictJSONResponse: gen.MembershipConflictJSONResponse(e.body)}, nil
		}
		return nil, err
	}
	body, err := projectBody(p)
	if err != nil {
		return nil, err
	}
	return gen.TransferProject200JSONResponse(body), nil
}

// ListMyJoinRequests lists the caller's join requests (FR-11.1).
func (h *Handler) ListMyJoinRequests(ctx context.Context, req gen.ListMyJoinRequestsRequestObject) (gen.ListMyJoinRequestsResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListMyJoinRequests401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	page, size := pageOf(req.Params.Page, req.Params.PageSize)
	res, err := h.members.MyRequests(ctx, id, page, size)
	if errors.Is(err, domain.ErrInvalidPage) {
		return gen.ListMyJoinRequests400JSONResponse{MembershipInvalidJSONResponse: gen.MembershipInvalidJSONResponse(invalidPage)}, nil
	}
	if err != nil {
		return nil, err
	}
	body := gen.MyJoinRequestPage{Page: res.Page, PageSize: res.PageSize, Total: res.Total, Items: make([]gen.MyJoinRequest, len(res.Items))}
	for i, it := range res.Items {
		b, err := myRequestBody(it)
		if err != nil {
			return nil, err
		}
		body.Items[i] = b
	}
	return gen.ListMyJoinRequests200JSONResponse(body), nil
}

// ListMyInvitations lists the caller's pending invitations (FR-11.3).
func (h *Handler) ListMyInvitations(ctx context.Context, req gen.ListMyInvitationsRequestObject) (gen.ListMyInvitationsResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListMyInvitations401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	page, size := pageOf(req.Params.Page, req.Params.PageSize)
	res, err := h.members.MyInvitations(ctx, id, page, size)
	if errors.Is(err, domain.ErrInvalidPage) {
		return gen.ListMyInvitations400JSONResponse{MembershipInvalidJSONResponse: gen.MembershipInvalidJSONResponse(invalidPage)}, nil
	}
	if err != nil {
		return nil, err
	}
	body := gen.MyInvitationPage{Page: res.Page, PageSize: res.PageSize, Total: res.Total, Items: make([]gen.MyInvitation, len(res.Items))}
	for i, it := range res.Items {
		rid, err := uuidOf(it.ID)
		if err != nil {
			return nil, err
		}
		project, err := membershipProjectBody(it.Project)
		if err != nil {
			return nil, err
		}
		body.Items[i] = gen.MyInvitation{Id: rid, Role: gen.MemberRole(it.Role), Project: project, CreatedAt: it.CreatedAt}
	}
	return gen.ListMyInvitations200JSONResponse(body), nil
}

func memberBody(m domain.Member) (gen.ProjectMember, error) {
	user, err := personBody(m.Person)
	if err != nil {
		return gen.ProjectMember{}, err
	}
	return gen.ProjectMember{User: user, Role: gen.ProjectRole(m.Role), JoinedAt: m.JoinedAt}, nil
}

func invitationBody(r domain.RequestItem) (gen.ProjectInvitation, error) {
	rid, err := uuidOf(r.ID)
	if err != nil {
		return gen.ProjectInvitation{}, err
	}
	user, err := personBody(r.User)
	if err != nil {
		return gen.ProjectInvitation{}, err
	}
	return gen.ProjectInvitation{
		Id: rid, Status: gen.JoinRequestStatus(r.Status), Role: gen.MemberRole(r.Role), User: user,
		CreatedAt: r.CreatedAt, DecidedAt: r.DecidedAt,
	}, nil
}

func membershipProjectBody(p domain.MembershipProject) (gen.MembershipProject, error) {
	pid, err := uuidOf(p.ID)
	if err != nil {
		return gen.MembershipProject{}, err
	}
	owner, err := personBody(p.Owner)
	if err != nil {
		return gen.MembershipProject{}, err
	}
	return gen.MembershipProject{Id: pid, Title: p.Title, Status: gen.ProjectStatus(p.Status), Owner: owner}, nil
}

func myRequestBody(r domain.MyRequestItem) (gen.MyJoinRequest, error) {
	rid, err := uuidOf(r.ID)
	if err != nil {
		return gen.MyJoinRequest{}, err
	}
	project, err := membershipProjectBody(r.Project)
	if err != nil {
		return gen.MyJoinRequest{}, err
	}
	return gen.MyJoinRequest{
		Id: rid, Status: gen.JoinRequestStatus(r.Status), Message: r.Message, Project: project,
		CreatedAt: r.CreatedAt, DecidedAt: r.DecidedAt,
	}, nil
}
