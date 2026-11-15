// Package usecase holds the social module's logic: sending, accepting, rejecting and
// cancelling connection requests (FR-7.1 to FR-7.3, FR-7.6), removing connections
// (FR-7.5), the lists of connections and requests (FR-7.4), and the link between two
// users that a profile shows, and blocks (FR-16.9). It imports only the social domain.
package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

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
	tx       domain.Transactor
	limiter  domain.RequestLimiter
	notifier domain.Notifier
	actors   domain.Actors
	links    domain.LinkWatcher
	logger   *slog.Logger
}

// New returns a Service. notifier hears about sent, accepted, cancelled and rejected
// requests inside the transaction tx opens for each change, so the change and its
// notification are stored together or not at all (FR-9.1).
func New(store domain.Store, tx domain.Transactor, limiter domain.RequestLimiter, notifier domain.Notifier, actors domain.Actors, logger *slog.Logger) *Service {
	return &Service{store: store, tx: tx, limiter: limiter, notifier: notifier, actors: actors, links: noLinkWatcher{}, logger: logger}
}

// WatchLinks makes w hear about every change to the link between two users (AI-2.1: the
// recommendation lists). Call it before the Service is used; without it nobody hears.
func (s *Service) WatchLinks(w domain.LinkWatcher) {
	s.links = w
}

type noLinkWatcher struct{}

func (noLinkWatcher) LinkChanged(context.Context, string, string) {}

// Send sends a connection request from requesterID to receiverID (FR-7.1). Checks run in
// this order: ErrSelf, *SuspendedError, *RateLimitedError, ErrNotFound for a receiver who is unknown,
// suspended or not verified, a block (FR-16.9: ErrYouBlocked for the blocker, ErrNotFound
// for the blocked person), then the link between the two (FR-7.2): ErrAlreadyPending,
// *PendingFromThemError or ErrAlreadyConnected. A rejected request does not block a new
// one (FR-7.6). It returns the request and the receiver.
func (s *Service) Send(ctx context.Context, requesterID, receiverID string) (domain.PendingRequest, error) {
	if requesterID == receiverID {
		return domain.PendingRequest{}, domain.ErrSelf
	}
	if err := s.refuseSuspended(ctx, requesterID); err != nil {
		return domain.PendingRequest{}, err
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
	other, err := s.store.FindPerson(ctx, receiverID)
	if err != nil {
		return domain.PendingRequest{}, err
	}
	return domain.PendingRequest{ID: req.ID, CreatedAt: req.CreatedAt, Other: other}, nil
}

// refuseSuspended returns *SuspendedError when the user's account is suspended now. It runs
// before the limit, so a refusal uses no quota, and first in the transaction, where it holds
// the user's row (domain.Actors), so a suspension that commits meanwhile cannot be passed.
func (s *Service) refuseSuspended(ctx context.Context, userID string) error {
	st, err := s.actors.Standing(ctx, userID)
	if err != nil {
		return err
	}
	if st.Suspended {
		return &domain.SuspendedError{Reason: st.SuspensionReason, Until: st.SuspendedUntil}
	}
	return nil
}

// create stores the request and its notification in one transaction, or explains with
// the link that refused it. The database decides between two requests sent at the same
// moment; when the refusing link is gone before it can be read, it tries again.
func (s *Service) create(ctx context.Context, requesterID, receiverID string) (domain.Request, error) {
	for range sendAttempts {
		var (
			req     domain.Request
			created bool
		)
		err := s.tx.InTx(ctx, func(ctx context.Context) error {
			if err := s.refuseSuspended(ctx, requesterID); err != nil {
				return err
			}
			if err := s.refuseBlocked(ctx, requesterID, receiverID); err != nil {
				return err
			}
			var err error
			req, created, err = s.store.CreateRequest(ctx, requesterID, receiverID)
			if err != nil || !created {
				return err
			}
			if err := s.notifier.RequestSent(ctx, req); err != nil {
				return fmt.Errorf("notify connection request %s: %w", req.ID, err)
			}
			s.links.LinkChanged(ctx, requesterID, receiverID)
			return nil
		})
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
		switch relationOf(requesterID, link).Status {
		case domain.StatusConnected:
			return domain.Request{}, domain.ErrAlreadyConnected
		case domain.StatusPendingSent:
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
	var (
		req domain.Request
		at  time.Time
	)
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		req, at, err = s.store.AcceptRequest(ctx, requestID, receiverID)
		if err != nil {
			return err
		}
		if err := s.notifier.RequestAccepted(ctx, req); err != nil {
			return fmt.Errorf("notify accepted connection request %s: %w", req.ID, err)
		}
		s.links.LinkChanged(ctx, req.RequesterID, req.ReceiverID)
		return nil
	})
	if err != nil {
		return domain.Connection{}, err
	}
	other, err := s.store.FindPerson(ctx, req.RequesterID)
	if err != nil {
		return domain.Connection{}, err
	}
	return domain.Connection{ConnectedAt: at, Other: other, Viewer: domain.Relation{Status: domain.StatusConnected}}, nil
}

// Reject rejects a pending request that receiverID received (FR-7.3). The sender is not
// told: the request leaves their sent list and they may send another (FR-7.6).
// Its notification is withdrawn in the same transaction.
func (s *Service) Reject(ctx context.Context, receiverID, requestID string) error {
	return s.withdraw(ctx, requestID, func(ctx context.Context) (string, string, error) {
		requesterID, err := s.store.RejectRequest(ctx, requestID, receiverID)
		return receiverID, requesterID, err
	})
}

// Cancel withdraws a pending request that requesterID sent (FR-7.3), and its
// notification in the same transaction.
func (s *Service) Cancel(ctx context.Context, requesterID, requestID string) error {
	return s.withdraw(ctx, requestID, func(ctx context.Context) (string, string, error) {
		receiverID, err := s.store.CancelRequest(ctx, requestID, requesterID)
		return requesterID, receiverID, err
	})
}

// withdraw runs change, which returns the request's two users, and, when it succeeds,
// tells the notifier and the link watcher, in one transaction.
func (s *Service) withdraw(ctx context.Context, requestID string, change func(context.Context) (string, string, error)) error {
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		a, b, err := change(ctx)
		if err != nil {
			return err
		}
		if err := s.notifier.RequestWithdrawn(ctx, requestID); err != nil {
			return fmt.Errorf("withdraw notification of connection request %s: %w", requestID, err)
		}
		s.links.LinkChanged(ctx, a, b)
		return nil
	})
}

// Remove ends the connection between userID and otherID; either side may (FR-7.5).
func (s *Service) Remove(ctx context.Context, userID, otherID string) error {
	if err := s.store.RemoveConnection(ctx, userID, otherID); err != nil {
		return err
	}
	s.links.LinkChanged(ctx, userID, otherID)
	return nil
}

// ListConnections returns one page of userID's connections (FR-7.4). userID is the
// caller, so every item's Viewer is StatusConnected without another query.
func (s *Service) ListConnections(ctx context.Context, userID string, page int) (domain.ConnectionPage, error) {
	p, err := s.listConnections(ctx, userID, userID, page)
	if err != nil {
		return domain.ConnectionPage{}, err
	}
	for i := range p.Items {
		p.Items[i].Viewer = domain.Relation{Status: domain.StatusConnected}
	}
	return p, nil
}

// Search text limits of SearchConnections, as for the researcher search.
const (
	MinQueryLength = 2
	MaxQueryLength = 100
)

// SearchConnections returns one page of userID's connections whose name, headline or
// institution match q (FR-16.6), for the group member picker. q is trimmed and must have 2 to
// 100 characters, otherwise ErrInvalidQuery.
func (s *Service) SearchConnections(ctx context.Context, userID, q string, page int) (domain.ConnectionPage, error) {
	text := strings.TrimSpace(q)
	if n := utf8.RuneCountInString(text); n < MinQueryLength || n > MaxQueryLength || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return domain.ConnectionPage{}, domain.ErrInvalidQuery
	}
	if page < 1 {
		return domain.ConnectionPage{}, domain.ErrInvalidPage
	}
	items, total, err := s.store.SearchConnections(ctx, userID, text, PageSize, (page-1)*PageSize)
	if err != nil {
		return domain.ConnectionPage{}, err
	}
	for i := range items {
		items[i].Viewer = domain.Relation{Status: domain.StatusConnected}
	}
	return domain.ConnectionPage{Items: items, Page: page, PageSize: PageSize, Total: total}, nil
}

func (s *Service) listConnections(ctx context.Context, viewerID, userID string, page int) (domain.ConnectionPage, error) {
	if page < 1 {
		return domain.ConnectionPage{}, domain.ErrInvalidPage
	}
	items, total, err := s.store.ListConnections(ctx, viewerID, userID, PageSize, (page-1)*PageSize)
	if err != nil {
		return domain.ConnectionPage{}, err
	}
	return domain.ConnectionPage{Items: items, Page: page, PageSize: PageSize, Total: total}, nil
}

// ListUserConnections returns one page of any user's connections to a logged-in viewer
// (FR-7.4), each with the viewer's own Relation with that person, read for the whole page
// at once, leaving out anyone who has blocked the viewer (FR-16.9). A user who is unknown,
// suspended or not verified, or who has blocked the viewer, gives ErrNotFound, as for the
// profile.
func (s *Service) ListUserConnections(ctx context.Context, viewerID, userID string, page int) (domain.ConnectionPage, error) {
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
	blocks, err := s.store.BlockBetween(ctx, viewerID, userID)
	if err != nil {
		return domain.ConnectionPage{}, err
	}
	if blocks.ByThem {
		return domain.ConnectionPage{}, domain.ErrNotFound
	}
	p, err := s.listConnections(ctx, viewerID, userID, page)
	if err != nil {
		return domain.ConnectionPage{}, err
	}
	ids := make([]string, len(p.Items))
	for i, c := range p.Items {
		ids[i] = c.Other.UserID
	}
	rel, err := s.Relations(ctx, viewerID, ids)
	if err != nil {
		return domain.ConnectionPage{}, err
	}
	for i, c := range p.Items {
		p.Items[i].Viewer = rel[c.Other.UserID]
	}
	return p, nil
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
// profile's Connect, Pending, Accept or Connected button: StatusSelf on the viewer's own
// profile, StatusNone after a rejection (FR-7.6). It is Relations for one user.
func (s *Service) Relation(ctx context.Context, viewerID, ownerID string) (domain.Relation, error) {
	r, err := s.Relations(ctx, viewerID, []string{ownerID})
	if err != nil {
		return domain.Relation{}, err
	}
	return r[ownerID], nil
}

// Relations returns the viewer's Relation with each of otherIDs, for a page of search
// results or connections, with one store call however many there are (none when the only
// other user is the viewer). Every ID gets an entry.
func (s *Service) Relations(ctx context.Context, viewerID string, otherIDs []string) (map[string]domain.Relation, error) {
	out := make(map[string]domain.Relation, len(otherIDs))
	var others []string
	for _, id := range otherIDs {
		if id == viewerID {
			out[id] = domain.Relation{Status: domain.StatusSelf}
			continue
		}
		out[id] = domain.Relation{Status: domain.StatusNone}
		others = append(others, id)
	}
	if len(others) == 0 {
		return out, nil
	}
	links, err := s.store.ListLiveLinks(ctx, viewerID, others)
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		other := l.ReceiverID
		if other == viewerID {
			other = l.RequesterID
		}
		if _, asked := out[other]; asked {
			out[other] = relationOf(viewerID, l)
		}
	}
	return out, nil
}

// relationOf is the viewer's Relation given the live link between them and another user.
func relationOf(viewerID string, l domain.LiveLink) domain.Relation {
	id := l.ID
	switch {
	case l.Accepted:
		return domain.Relation{Status: domain.StatusConnected}
	case l.RequesterID == viewerID:
		return domain.Relation{Status: domain.StatusPendingSent, RequestID: &id}
	default:
		return domain.Relation{Status: domain.StatusPendingReceived, RequestID: &id}
	}
}

// CountConnections returns how many connections userID has: the total of their
// connections list as viewerID sees it.
func (s *Service) CountConnections(ctx context.Context, viewerID, userID string) (int, error) {
	return s.store.CountConnections(ctx, viewerID, userID)
}
