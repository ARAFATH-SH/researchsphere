// Package domain holds the social module's entities and the interfaces its usecase needs:
// connection requests and connections between researchers (FR-7). It imports only the
// standard library.
package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Person is the other user of a request or connection, with the public fields a list
// shows (FR-7.4).
type Person struct {
	UserID      string
	FullName    string
	Headline    *string
	Institution *string
	PhotoFileID *string // the profile photo (FR-4.6), nil when not set
}

// Request is a connection request as the store keeps it.
type Request struct {
	ID          string
	RequesterID string
	ReceiverID  string
	CreatedAt   time.Time
}

// PendingRequest is one item of a received or sent list: the request and the other user,
// the sender in the received list and the receiver in the sent list.
type PendingRequest struct {
	ID        string
	CreatedAt time.Time
	Other     Person
}

// Connection is one item of a connections list: the other user, when the request was
// accepted, and the caller's own Relation with that user, which on another user's list
// may be anything, StatusSelf included.
type Connection struct {
	ConnectedAt time.Time
	Other       Person
	Viewer      Relation
}

// RequestPage is one page of a received or sent list.
type RequestPage struct {
	Items    []PendingRequest
	Page     int
	PageSize int
	Total    int // requests on all pages
}

// ConnectionPage is one page of a connections list.
type ConnectionPage struct {
	Items    []Connection
	Page     int
	PageSize int
	Total    int // connections on all pages
}

// LiveLink is the pending request or the connection between two users, if any; there is
// at most one per pair, in either direction (FR-7.2, enforced by connections_live_pair_uq).
type LiveLink struct {
	ID          string
	RequesterID string
	ReceiverID  string
	Accepted    bool // false while the request is pending
}

// Status is the link between a viewer and another user, seen from the viewer.
type Status string

// Statuses (FR-7). A rejected request reads as StatusNone to both sides (FR-7.6);
// StatusSelf is the viewer's own.
const (
	StatusNone            Status = "none"
	StatusPendingSent     Status = "pending_sent"
	StatusPendingReceived Status = "pending_received"
	StatusConnected       Status = "connected"
	StatusSelf            Status = "self"
)

// Relation is the viewer's Status with another user and, while a request is pending, its ID.
type Relation struct {
	Status    Status
	RequestID *string
}

// Store reads and writes connection requests. Methods that take an ID return ErrNotFound
// (or false) for a value that is not a UUID, as for a missing row.
type Store interface {
	// UserVisible reports whether the user exists, is verified and is not suspended: the
	// rule of the profile view (FR-4.5).
	UserVisible(ctx context.Context, userID string) (bool, error)
	// FindPerson returns the user's public fields, ErrNotFound when there is no such user.
	FindPerson(ctx context.Context, userID string) (Person, error)
	// CreateRequest stores a pending request from requesterID to receiverID. It returns
	// created false, and stores nothing, when a pending request or a connection already
	// exists between the two in either direction; the database decides, so two requests
	// sent at the same moment cannot both be stored.
	CreateRequest(ctx context.Context, requesterID, receiverID string) (req Request, created bool, err error)
	// FindLiveLink returns the pending request or connection between a and b, in either
	// direction; found is false when there is none.
	FindLiveLink(ctx context.Context, a, b string) (link LiveLink, found bool, err error)
	// ListLiveLinks returns, in one query, the pending requests and connections between
	// viewerID and any of otherIDs, at most one per other user. IDs that are not UUIDs
	// match nothing.
	ListLiveLinks(ctx context.Context, viewerID string, otherIDs []string) ([]LiveLink, error)
	// AcceptRequest marks the request accepted when receiverID received it, it is pending
	// and its sender is verified and not suspended; otherwise ErrNotFound. It returns the
	// request and when it was accepted.
	AcceptRequest(ctx context.Context, requestID, receiverID string) (Request, time.Time, error)
	// RejectRequest marks the request rejected, under the same conditions as AcceptRequest.
	RejectRequest(ctx context.Context, requestID, receiverID string) error
	// CancelRequest deletes the request when requesterID sent it and it is pending;
	// otherwise ErrNotFound.
	CancelRequest(ctx context.Context, requestID, requesterID string) error
	// RemoveConnection deletes the connection between userID and otherID; ErrNotFound when
	// they are not connected.
	RemoveConnection(ctx context.Context, userID, otherID string) error
	// ListConnections, ListReceived and ListSent return one page, newest first, and the
	// total, leaving out other users who are suspended or not verified.
	ListConnections(ctx context.Context, userID string, limit, offset int) ([]Connection, int, error)
	ListReceived(ctx context.Context, userID string, limit, offset int) ([]PendingRequest, int, error)
	ListSent(ctx context.Context, userID string, limit, offset int) ([]PendingRequest, int, error)
	// CountConnections is the total of ListConnections.
	CountConnections(ctx context.Context, userID string) (int, error)
}

// RequestLimiter counts a user's connection requests in a fixed window.
type RequestLimiter interface {
	// Used returns the requests counted in the user's current window and the time left in
	// it; 0 and 0 when no window is open.
	Used(ctx context.Context, userID string) (int, time.Duration, error)
	// Record counts one request. The first request of a window opens it for window.
	Record(ctx context.Context, userID string, window time.Duration) error
}

// Notifier hears about connection events, so the other user can be notified (FR-9.1).
// It is called inside the transaction of the change it reports (Transactor): an error
// rolls the change back, so a change is never stored without its notification, nor a
// notification without its change.
type Notifier interface {
	// RequestSent is called when req has been sent to req.ReceiverID.
	RequestSent(ctx context.Context, req Request) error
	// RequestAccepted is called when req.ReceiverID has accepted req.
	RequestAccepted(ctx context.Context, req Request) error
	// RequestWithdrawn is called when request requestID has been cancelled by its sender
	// or rejected by its receiver, so what was said about it can be taken back. The
	// sender is never told about a rejection (FR-7.6).
	RequestWithdrawn(ctx context.Context, requestID string) error
}

// Transactor runs fn in one database transaction that the Store and the Notifier both
// use; it commits when fn returns nil and rolls back otherwise.
type Transactor interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Errors of the social module.
var (
	ErrNotFound         = errors.New("social: not found")
	ErrSelf             = errors.New("social: cannot connect to oneself")
	ErrAlreadyPending   = errors.New("social: a request is already pending")
	ErrAlreadyConnected = errors.New("social: already connected")
	ErrInvalidPage      = errors.New("social: page is not a whole number of at least 1")
)

// PendingFromThemError says the other user has already sent the caller a request that is
// still pending, so the caller should accept it instead of sending one.
type PendingFromThemError struct {
	RequestID string
}

func (e *PendingFromThemError) Error() string {
	return fmt.Sprintf("social: request %s from the other user is pending", e.RequestID)
}

// RateLimitedError says the user has used up the requests of the current window.
type RateLimitedError struct {
	RetryAfter time.Duration // until the window ends
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("social: connection requests rate limited, retry after %s", e.RetryAfter)
}
