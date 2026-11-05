// Package http serves the connection operations in docs/openapi.yaml: sending, accepting,
// rejecting and cancelling requests (FR-7.1 to FR-7.3, FR-7.6), removing connections
// (FR-7.5), the lists of connections and requests (FR-7.4) and blocks (FR-16.9). It turns
// the usecase's errors into the contract's codes. cmd/api guards each route with its platform action
// (SRS 2.3) before it gets here.
package http

import (
	"context"
	"errors"
	"fmt"
	"math"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

// Error codes of the connection operations (docs/openapi.yaml).
const (
	CodeCannotConnectToSelf = "cannot_connect_to_self"
	CodeRateLimited         = "rate_limited"
	CodeCannotBlockSelf     = "cannot_block_self"
)

// Service is what the handler needs from the usecase.
type Service interface {
	Send(ctx context.Context, requesterID, receiverID string) (domain.PendingRequest, error)
	Accept(ctx context.Context, receiverID, requestID string) (domain.Connection, error)
	Reject(ctx context.Context, receiverID, requestID string) error
	Cancel(ctx context.Context, requesterID, requestID string) error
	Remove(ctx context.Context, userID, otherID string) error
	ListConnections(ctx context.Context, userID string, page int) (domain.ConnectionPage, error)
	ListUserConnections(ctx context.Context, viewerID, userID string, page int) (domain.ConnectionPage, error)
	ListReceived(ctx context.Context, userID string, page int) (domain.RequestPage, error)
	ListSent(ctx context.Context, userID string, page int) (domain.RequestPage, error)
	Block(ctx context.Context, blockerID, blockedID string) error
	Unblock(ctx context.Context, blockerID, blockedID string) error
	ListBlocks(ctx context.Context, userID string, page int) (domain.BlockedPage, error)
}

// Handler implements the connection operations of gen.StrictServerInterface.
type Handler struct {
	svc Service
}

// NewHandler returns a Handler that uses svc.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

var (
	unauthenticated = gen.UnauthorizedJSONResponse{Code: httpx.CodeUnauthenticated, Message: "Log in to continue."}
	requestNotFound = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This connection request does not exist."}
	profileNotFound = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This profile does not exist."}
	badPage         = gen.InvalidConnectionPageJSONResponse{Code: httpx.CodeBadRequest, Message: "Page must be a whole number of at least 1."}
)

// SendConnectionRequest sends a connection request (FR-7.1, FR-7.2, FR-7.6).
func (h *Handler) SendConnectionRequest(ctx context.Context, req gen.SendConnectionRequestRequestObject) (gen.SendConnectionRequestResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.SendConnectionRequest401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.SendConnectionRequest400JSONResponse{Code: httpx.CodeBadRequest, Message: "The request is not valid."}, nil
	}
	r, err := h.svc.Send(ctx, id, req.Body.UserId)
	var (
		limited *domain.RateLimitedError
		theirs  *domain.PendingFromThemError
	)
	switch {
	case errors.Is(err, domain.ErrSelf):
		return gen.SendConnectionRequest400JSONResponse{Code: CodeCannotConnectToSelf, Message: "You cannot connect with yourself."}, nil
	case errors.As(err, &limited):
		return gen.SendConnectionRequest429JSONResponse{
			Body:    gen.Error{Code: CodeRateLimited, Message: "You have sent 50 connection requests today. Try again later."},
			Headers: gen.SendConnectionRequest429ResponseHeaders{RetryAfter: retryAfter(limited)},
		}, nil
	case errors.Is(err, domain.ErrNotFound):
		return gen.SendConnectionRequest404JSONResponse{NotFoundJSONResponse: profileNotFound}, nil
	case errors.Is(err, domain.ErrYouBlocked):
		return gen.SendConnectionRequest409JSONResponse{Code: gen.YouBlockedThisUser, Message: "You have blocked this person. Unblock them to connect."}, nil
	case errors.Is(err, domain.ErrAlreadyPending):
		return gen.SendConnectionRequest409JSONResponse{Code: gen.RequestAlreadyPending, Message: "You have already sent this person a request."}, nil
	case errors.Is(err, domain.ErrAlreadyConnected):
		return gen.SendConnectionRequest409JSONResponse{Code: gen.AlreadyConnected, Message: "You are already connected."}, nil
	case errors.As(err, &theirs):
		rid, err := uuid(theirs.RequestID)
		if err != nil {
			return nil, err
		}
		return gen.SendConnectionRequest409JSONResponse{Code: gen.RequestPendingFromThem,
			Message: "This person has already sent you a request. Accept it to connect.", RequestId: &rid}, nil
	case err != nil:
		return nil, err
	}
	body, err := requestBody(r)
	if err != nil {
		return nil, err
	}
	return gen.SendConnectionRequest201JSONResponse(body), nil
}

// AcceptConnectionRequest accepts a request the caller received (FR-7.3).
func (h *Handler) AcceptConnectionRequest(ctx context.Context, req gen.AcceptConnectionRequestRequestObject) (gen.AcceptConnectionRequestResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.AcceptConnectionRequest401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	c, err := h.svc.Accept(ctx, id, req.RequestId)
	if errors.Is(err, domain.ErrNotFound) {
		return gen.AcceptConnectionRequest404JSONResponse{NotFoundJSONResponse: requestNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := connectionBody(c)
	if err != nil {
		return nil, err
	}
	return gen.AcceptConnectionRequest200JSONResponse(body), nil
}

// RejectConnectionRequest rejects a request the caller received (FR-7.3, FR-7.6).
func (h *Handler) RejectConnectionRequest(ctx context.Context, req gen.RejectConnectionRequestRequestObject) (gen.RejectConnectionRequestResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RejectConnectionRequest401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.svc.Reject(ctx, id, req.RequestId)
	if errors.Is(err, domain.ErrNotFound) {
		return gen.RejectConnectionRequest404JSONResponse{NotFoundJSONResponse: requestNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.RejectConnectionRequest204Response{}, nil
}

// CancelConnectionRequest cancels a request the caller sent (FR-7.3).
func (h *Handler) CancelConnectionRequest(ctx context.Context, req gen.CancelConnectionRequestRequestObject) (gen.CancelConnectionRequestResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.CancelConnectionRequest401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.svc.Cancel(ctx, id, req.RequestId)
	if errors.Is(err, domain.ErrNotFound) {
		return gen.CancelConnectionRequest404JSONResponse{NotFoundJSONResponse: requestNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.CancelConnectionRequest204Response{}, nil
}

// RemoveConnection removes the caller's connection with another user (FR-7.5).
func (h *Handler) RemoveConnection(ctx context.Context, req gen.RemoveConnectionRequestObject) (gen.RemoveConnectionResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RemoveConnection401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	err := h.svc.Remove(ctx, id, req.UserId)
	if errors.Is(err, domain.ErrNotFound) {
		return gen.RemoveConnection404JSONResponse{Code: httpx.CodeNotFound, Message: "You are not connected with this person."}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.RemoveConnection204Response{}, nil
}

// ListMyConnections returns one page of the caller's connections (FR-7.4).
func (h *Handler) ListMyConnections(ctx context.Context, req gen.ListMyConnectionsRequestObject) (gen.ListMyConnectionsResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListMyConnections401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	res, err := h.svc.ListConnections(ctx, id, pageParam(req.Params.Page))
	if errors.Is(err, domain.ErrInvalidPage) {
		return gen.ListMyConnections400JSONResponse{InvalidConnectionPageJSONResponse: badPage}, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := connectionPageBody(res)
	if err != nil {
		return nil, err
	}
	return gen.ListMyConnections200JSONResponse(body), nil
}

// ListProfileConnections returns one page of any user's connections (FR-7.4). Unknown,
// suspended and unverified accounts get the profile's 404.
func (h *Handler) ListProfileConnections(ctx context.Context, req gen.ListProfileConnectionsRequestObject) (gen.ListProfileConnectionsResponseObject, error) {
	viewer, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListProfileConnections401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	res, err := h.svc.ListUserConnections(ctx, viewer, req.UserId, pageParam(req.Params.Page))
	switch {
	case errors.Is(err, domain.ErrInvalidPage):
		return gen.ListProfileConnections400JSONResponse{InvalidConnectionPageJSONResponse: badPage}, nil
	case errors.Is(err, domain.ErrNotFound):
		return gen.ListProfileConnections404JSONResponse{NotFoundJSONResponse: profileNotFound}, nil
	case err != nil:
		return nil, err
	}
	body, err := connectionPageBody(res)
	if err != nil {
		return nil, err
	}
	return gen.ListProfileConnections200JSONResponse(body), nil
}

// ListReceivedConnectionRequests returns one page of the pending requests the caller
// received (FR-7.4).
func (h *Handler) ListReceivedConnectionRequests(ctx context.Context, req gen.ListReceivedConnectionRequestsRequestObject) (gen.ListReceivedConnectionRequestsResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListReceivedConnectionRequests401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	res, err := h.svc.ListReceived(ctx, id, pageParam(req.Params.Page))
	if errors.Is(err, domain.ErrInvalidPage) {
		return gen.ListReceivedConnectionRequests400JSONResponse{InvalidConnectionPageJSONResponse: badPage}, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := requestPageBody(res)
	if err != nil {
		return nil, err
	}
	return gen.ListReceivedConnectionRequests200JSONResponse(body), nil
}

// ListSentConnectionRequests returns one page of the pending requests the caller sent
// (FR-7.4).
func (h *Handler) ListSentConnectionRequests(ctx context.Context, req gen.ListSentConnectionRequestsRequestObject) (gen.ListSentConnectionRequestsResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ListSentConnectionRequests401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	res, err := h.svc.ListSent(ctx, id, pageParam(req.Params.Page))
	if errors.Is(err, domain.ErrInvalidPage) {
		return gen.ListSentConnectionRequests400JSONResponse{InvalidConnectionPageJSONResponse: badPage}, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := requestPageBody(res)
	if err != nil {
		return nil, err
	}
	return gen.ListSentConnectionRequests200JSONResponse(body), nil
}

// pageParam is the page asked for, 1 when missing.
func pageParam(page *int) int {
	if page == nil {
		return 1
	}
	return *page
}

// retryAfter is the time left in whole seconds, rounded up, and at least 1.
func retryAfter(e *domain.RateLimitedError) int {
	return max(1, int(math.Ceil(e.RetryAfter.Seconds())))
}

func requestBody(r domain.PendingRequest) (gen.ConnectionRequest, error) {
	rid, err := uuid(r.ID)
	if err != nil {
		return gen.ConnectionRequest{}, err
	}
	uid, err := uuid(r.Other.UserID)
	if err != nil {
		return gen.ConnectionRequest{}, err
	}
	return gen.ConnectionRequest{RequestId: rid, CreatedAt: r.CreatedAt, UserId: uid, FullName: r.Other.FullName,
		Headline: r.Other.Headline, Institution: r.Other.Institution, PhotoUrl: mediaURL(r.Other.PhotoFileID)}, nil
}

func connectionBody(c domain.Connection) (gen.Connection, error) {
	uid, err := uuid(c.Other.UserID)
	if err != nil {
		return gen.Connection{}, err
	}
	var requestID *openapi_types.UUID
	if c.Viewer.RequestID != nil {
		id, err := uuid(*c.Viewer.RequestID)
		if err != nil {
			return gen.Connection{}, err
		}
		requestID = &id
	}
	return gen.Connection{ConnectedAt: c.ConnectedAt, UserId: uid, FullName: c.Other.FullName,
		Headline: c.Other.Headline, Institution: c.Other.Institution, PhotoUrl: mediaURL(c.Other.PhotoFileID),
		ConnectionStatus: gen.ConnectionStatus(c.Viewer.Status), ConnectionRequestId: requestID}, nil
}

// Every list is empty, never null, when there is nothing in it.
func requestPageBody(p domain.RequestPage) (gen.ConnectionRequestPage, error) {
	items := make([]gen.ConnectionRequest, 0, len(p.Items))
	for _, r := range p.Items {
		b, err := requestBody(r)
		if err != nil {
			return gen.ConnectionRequestPage{}, err
		}
		items = append(items, b)
	}
	return gen.ConnectionRequestPage{Items: items, Page: p.Page, PageSize: p.PageSize, Total: p.Total}, nil
}

func connectionPageBody(p domain.ConnectionPage) (gen.ConnectionPage, error) {
	items := make([]gen.Connection, 0, len(p.Items))
	for _, c := range p.Items {
		b, err := connectionBody(c)
		if err != nil {
			return gen.ConnectionPage{}, err
		}
		items = append(items, b)
	}
	return gen.ConnectionPage{Items: items, Page: p.Page, PageSize: p.PageSize, Total: p.Total}, nil
}

func uuid(s string) (openapi_types.UUID, error) {
	var id openapi_types.UUID
	if err := id.UnmarshalText([]byte(s)); err != nil {
		return id, fmt.Errorf("connection id %q: %w", s, err)
	}
	return id, nil
}

// mediaURL is where the client reads a profile photo (FR-4.6), nil when none is set.
func mediaURL(fileID *string) *string {
	if fileID == nil {
		return nil
	}
	u := httpx.MediaURL(*fileID)
	return &u
}
