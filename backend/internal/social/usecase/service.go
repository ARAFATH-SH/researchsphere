// Package usecase holds the social module's logic: sending, accepting, rejecting and
// cancelling connection requests (FR-7.1 to FR-7.3, FR-7.6), removing connections
// (FR-7.5), the lists of connections and requests (FR-7.4), and the link between two
// users that a profile shows. It imports only the social domain.
package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

// Limits (docs/decisions.md).
const (
	// RequestsPerWindow is how many requests a user may send in one RequestWindow.
	RequestsPerWindow = 50
	RequestWindow     = 24 * time.Hour
	// PageSize is the size of every list page (FR-7.4).
	PageSize = 20
	// sendAttempts bounds the retries of Send when the link that refused a request is
	// gone by the time it is read (cancelled or rejected in between).
	sendAttempts = 3
)

// Service handles connection requests and connections.
type Service struct {
	store    domain.Store
	limiter  domain.RequestLimiter
	notifier domain.Notifier
	logger   *slog.Logger
}

// New returns a Service. notifier hears about sent and accepted requests; pass
// NopNotifier{} until notifications exist (slice 14).
func New(store domain.Store, limiter domain.RequestLimiter, notifier domain.Notifier, logger *slog.Logger) *Service {
	return &Service{store: store, limiter: limiter, notifier: notifier, logger: logger}
}

// NopNotifier is a domain.Notifier that does nothing.
type NopNotifier struct{}

// RequestSent does nothing.
func (NopNotifier) RequestSent(context.Context, domain.Request) error { return nil }

// RequestAccepted does nothing.
func (NopNotifier) RequestAccepted(context.Context, domain.Request) error { return nil }

// Send sends a connection request from requesterID to receiverID (FR-7.1). Checks run in
// this order: ErrSelf, *RateLimitedError, ErrNotFound for a receiver who is unknown,
// suspended or not verified, then the link between the two (FR-7.2): ErrAlreadyPending,
// *PendingFromThemError or ErrAlreadyConnected. A rejected request does not block a new
// one (FR-7.6). It returns the request and the receiver.
func (s *Service) Send(ctx context.Context, requesterID, receiverID string) (domain.PendingRequest, error) {
	if requesterID == receiverID {
		return domain.PendingRequest{}, domain.ErrSelf
	}
	if err := s.checkLimit(ctx, requesterID); err != nil {
		return domain.PendingRequest{}, err
	}
	visible, err := s.store.UserVisible(ctx, receiverID)
	if err != nil {
		return domain.PendingRequest{}, err
	}
	if !visible {
		return domain.PendingRequest{}, domain.ErrNotFound
	}
	req, err := s.create(ctx, requesterID, receiverID)
	if err != nil {
		return domain.PendingRequest{}, err
	}
	if err := s.limiter.Record(ctx, requesterID, RequestWindow); err != nil {
		s.logger.WarnContext(ctx, "connection request rate limit not recorded", slog.Any("error", err))
	}
	if err := s.notifier.RequestSent(ctx, req); err != nil {
		s.logger.ErrorContext(ctx, "connection request notification failed",
			slog.String("request_id", req.ID), slog.Any("error", err))
	}
	other, err := s.store.FindPerson(ctx, receiverID)
	if err != nil {
		return domain.PendingRequest{}, err
	}
	return domain.PendingRequest{ID: req.ID, CreatedAt: req.CreatedAt, Other: other}, nil
}

// create stores the request, or explains with the link that refused it. The database
// decides between two requests sent at the same moment; when the refusing link is gone
// before it can be read, it tries again.
func (s *Service) create(ctx context.Context, requesterID, receiverID string) (domain.Request, error) {
	for range sendAttempts {
		req, created, err := s.store.CreateRequest(ctx, requesterID, receiverID)
		if err != nil {
			return domain.Request{}, err
		}
		if created {
			return req, nil
		}
		link, found, err := s.store.FindLiveLink(ctx, requesterID, receiverID)
		if err != nil {
			return domain.Request{}, err
		}
		if !found {
			continue
		}
		switch {
		case link.Accepted:
			return domain.Request{}, domain.ErrAlreadyConnected
		case link.RequesterID == requesterID:
			return domain.Request{}, domain.ErrAlreadyPending
		default:
			return domain.Request{}, &domain.PendingFromThemError{RequestID: link.ID}
		}
	}
	return domain.Request{}, fmt.Errorf("send connection request: refused %d times by a link that then disappeared", sendAttempts)
}

// checkLimit returns *RateLimitedError when the user has no requests left in the window.
// When the limiter cannot answer, the request goes ahead (fail open, as the login lockout).
func (s *Service) checkLimit(ctx context.Context, userID string) error {
	used, left, err := s.limiter.Used(ctx, userID)
	if err != nil {
		s.logger.WarnContext(ctx, "connection request rate limit unavailable, allowing the request", slog.Any("error", err))
		return nil
	}
	if used >= RequestsPerWindow {
		return &domain.RateLimitedError{RetryAfter: left}
	}
	return nil
}

// Accept accepts a pending request that receiverID received (FR-7.3) and returns the new
// connection, naming the sender. Anything else is ErrNotFound, so the answer does not
// reveal whether the request exists.
func (s *Service) Accept(ctx context.Context, receiverID, requestID string) (domain.Connection, error) {
	req, at, err := s.store.AcceptRequest(ctx, requestID, receiverID)
	if err != nil {
		return domain.Connection{}, err
	}
	if err := s.notifier.RequestAccepted(ctx, req); err != nil {
		s.logger.ErrorContext(ctx, "connection accepted notification failed",
			slog.String("request_id", req.ID), slog.Any("error", err))
	}
	other, err := s.store.FindPerson(ctx, req.RequesterID)
	if err != nil {
		return domain.Connection{}, err
	}
	return domain.Connection{ConnectedAt: at, Other: other}, nil
}

// Reject rejects a pending request that receiverID received (FR-7.3). The sender is not
// told: the request leaves their sent list and they may send another (FR-7.6).
func (s *Service) Reject(ctx context.Context, receiverID, requestID string) error {
	return s.store.RejectRequest(ctx, requestID, receiverID)
}

// Cancel withdraws a pending request that requesterID sent (FR-7.3).
func (s *Service) Cancel(ctx context.Context, requesterID, requestID string) error {
	return s.store.CancelRequest(ctx, requestID, requesterID)
}

// Remove ends the connection between userID and otherID; either side may (FR-7.5).
func (s *Service) Remove(ctx context.Context, userID, otherID string) error {
	return s.store.RemoveConnection(ctx, userID, otherID)
}

// ListConnections returns one page of userID's connections (FR-7.4).
func (s *Service) ListConnections(ctx context.Context, userID string, page int) (domain.ConnectionPage, error) {
	if page < 1 {
		return domain.ConnectionPage{}, domain.ErrInvalidPage
	}
	items, total, err := s.store.ListConnections(ctx, userID, PageSize, (page-1)*PageSize)
	if err != nil {
		return domain.ConnectionPage{}, err
	}
	return domain.ConnectionPage{Items: items, Page: page, PageSize: PageSize, Total: total}, nil
}

// ListUserConnections returns one page of any user's connections to a logged-in user
// (FR-7.4). A user who is unknown, suspended or not verified gives ErrNotFound, as for
// the profile.
func (s *Service) ListUserConnections(ctx context.Context, userID string, page int) (domain.ConnectionPage, error) {
	if page < 1 {
		return domain.ConnectionPage{}, domain.ErrInvalidPage
	}
	visible, err := s.store.UserVisible(ctx, userID)
	if err != nil {
		return domain.ConnectionPage{}, err
	}
	if !visible {
		return domain.ConnectionPage{}, domain.ErrNotFound
	}
	return s.ListConnections(ctx, userID, page)
}

// ListReceived returns one page of the pending requests userID received (FR-7.4).
func (s *Service) ListReceived(ctx context.Context, userID string, page int) (domain.RequestPage, error) {
	return s.listRequests(ctx, userID, page, s.store.ListReceived)
}

// ListSent returns one page of the pending requests userID sent (FR-7.4).
func (s *Service) ListSent(ctx context.Context, userID string, page int) (domain.RequestPage, error) {
	return s.listRequests(ctx, userID, page, s.store.ListSent)
}

func (s *Service) listRequests(ctx context.Context, userID string, page int,
	list func(context.Context, string, int, int) ([]domain.PendingRequest, int, error),
) (domain.RequestPage, error) {
	if page < 1 {
		return domain.RequestPage{}, domain.ErrInvalidPage
	}
	items, total, err := list(ctx, userID, PageSize, (page-1)*PageSize)
	if err != nil {
		return domain.RequestPage{}, err
	}
	return domain.RequestPage{Items: items, Page: page, PageSize: PageSize, Total: total}, nil
}

// Relation returns the link between viewerID and ownerID as the viewer sees it, for the
// profile's Connect, Pending, Accept or Connected button. It is StatusNone on the
// viewer's own profile and after a rejection (FR-7.6).
func (s *Service) Relation(ctx context.Context, viewerID, ownerID string) (domain.Relation, error) {
	none := domain.Relation{Status: domain.StatusNone}
	if viewerID == ownerID {
		return none, nil
	}
	link, found, err := s.store.FindLiveLink(ctx, viewerID, ownerID)
	if err != nil || !found {
		return none, err
	}
	switch {
	case link.Accepted:
		return domain.Relation{Status: domain.StatusConnected}, nil
	case link.RequesterID == viewerID:
		return domain.Relation{Status: domain.StatusPendingSent, RequestID: &link.ID}, nil
	default:
		return domain.Relation{Status: domain.StatusPendingReceived, RequestID: &link.ID}, nil
	}
}

// CountConnections returns how many connections userID has: the total of their
// connections list.
func (s *Service) CountConnections(ctx context.Context, userID string) (int, error) {
	return s.store.CountConnections(ctx, userID)
}
