// Package domain holds the realtime module's entities and the interfaces its usecase needs:
// the events pushed over WebSocket, typing, and who is online (FR-16.1, FR-16.3, FR-16.4,
// FR-9.4). docs/realtime.md is the full contract. It imports only the standard library.
package domain

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Event types a client receives.
const (
	EventMessageCreated      = "message.created"
	EventConversationRead    = "conversation.read"
	EventConversationUpdated = "conversation.updated"
	EventUnreadChanged       = "unread.changed"
	EventTyping              = "typing"
	EventPresence            = "presence"
	EventNotificationCreated = "notification.created"
	EventNotificationsUnread = "notifications.unread"
)

// Event types a client sends.
const (
	ClientTypingStart = "typing.start"
	ClientTypingStop  = "typing.stop"
)

// Event is one frame to a client. Data must marshal to JSON.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// ClientEvent is one frame from a client.
type ClientEvent struct {
	Type string `json:"type"`
	Data struct {
		ConversationID string `json:"conversationId"`
	} `json:"data"`
}

// Typing is the data of EventTyping.
type Typing struct {
	ConversationID string `json:"conversationId"`
	UserID         string `json:"userId"`
	IsTyping       bool   `json:"isTyping"`
}

// Status is the data of EventPresence, and one item of the presence list.
type Status struct {
	UserID string `json:"userId"`
	Online bool   `json:"online"`
}

// CloseCode is a WebSocket close code (docs/realtime.md).
type CloseCode int

// Close codes. The 4xxx ones are this API's own.
const (
	CloseNormal       CloseCode = 1000
	CloseGoingAway    CloseCode = 1001 // the server is shutting down
	CloseTryLater     CloseCode = 1013 // the send buffer filled up, or the connection died
	CloseTokenExpired CloseCode = 4401
	CloseSessionEnded CloseCode = 4403 // logout, suspension, password change or reset, account deleted
	CloseReplaced     CloseCode = 4409 // the user opened more sockets than allowed
)

// Text is the reason sent with the code. It says nothing about the user.
func (c CloseCode) Text() string {
	switch c {
	case CloseGoingAway:
		return "server shutting down"
	case CloseTryLater:
		return "too slow"
	case CloseTokenExpired:
		return "access token expired"
	case CloseSessionEnded:
		return "session ended"
	case CloseReplaced:
		return "too many connections"
	}
	return ""
}

// CloseCommand tells the instances to close a user's sockets: one when Socket is set,
// otherwise all of them.
type CloseCommand struct {
	Socket string    `json:"socket,omitempty"`
	Code   CloseCode `json:"code"`
}

// Envelope is what travels on a user's channel: one event, or one command.
type Envelope struct {
	Event json.RawMessage `json:"event,omitempty"`
	Close *CloseCommand   `json:"close,omitempty"`
}

// Delivery is one envelope received for a user.
type Delivery struct {
	UserID  string
	Payload []byte
}

// Bus carries envelopes between API instances, on one channel per user. It holds nothing:
// an envelope nobody is subscribed to is lost, so clients refetch after a reconnect.
type Bus interface {
	// Publish sends payload to whoever is subscribed to the user.
	Publish(ctx context.Context, userID string, payload []byte) error
	// Subscribe starts receiving the user's envelopes. It returns once they are flowing.
	Subscribe(ctx context.Context, userID string) error
	// Unsubscribe stops it. It does not wait for an answer, and a Subscribe called after it
	// returns takes effect after it.
	Unsubscribe(ctx context.Context, userID string) error
	// Deliveries is where received envelopes arrive.
	Deliveries() <-chan Delivery
}

// Presence records which sockets are open. An entry that is not refreshed in time expires, so
// a crashed instance does not leave people online.
type Presence interface {
	// Add records or refreshes a socket, and returns the user's live sockets, oldest first.
	Add(ctx context.Context, userID, socketID string, now time.Time) ([]string, error)
	// Remove forgets a socket and returns how many live ones the user has left.
	Remove(ctx context.Context, userID, socketID string, now time.Time) (int, error)
	// Count returns how many live sockets the user has.
	Count(ctx context.Context, userID string, now time.Time) (int, error)
	// Online says which of userIDs have a live socket.
	Online(ctx context.Context, userIDs []string, now time.Time) (map[string]bool, error)
}

// Suspension says why a user may not connect.
type Suspension struct {
	Reason *string
	Until  *time.Time // nil when permanent
}

// Access answers who may see what. The messaging and moderation modules own the answers;
// cmd/api implements it with their usecases.
type Access interface {
	// TypingPeer returns the other member of conversationID when userID may tell them about
	// typing now: both are members, it is active, neither blocked the other.
	TypingPeer(ctx context.Context, userID, conversationID string) (otherID string, ok bool, err error)
	// Peers returns the people who may see userID's presence: those who share an active
	// conversation with the user, with no block either way.
	Peers(ctx context.Context, userID string) ([]string, error)
	// Visible returns which of userIDs viewerID may see, by the same rule.
	Visible(ctx context.Context, viewerID string, userIDs []string) ([]string, error)
	// Suspension returns the user's suspension, nil when there is none.
	Suspension(ctx context.Context, userID string) (*Suspension, error)
}

// Socket is one open connection to a client. All methods may run at the same time.
type Socket interface {
	// Write sends one text frame.
	Write(ctx context.Context, frame []byte) error
	// Ping sends a ping and waits for the pong.
	Ping(ctx context.Context) error
	// Close ends the connection with the code. It may wait for the client.
	Close(code CloseCode) error
}

// Errors of the realtime module.
var (
	// ErrClosed refuses a new socket once the server is shutting down.
	ErrClosed = errors.New("realtime: shutting down")
	// ErrTooManyIDs refuses a presence list over MaxPresenceIDs.
	ErrTooManyIDs = errors.New("realtime: too many user IDs")
	// ErrNoIDs refuses an empty presence list.
	ErrNoIDs = errors.New("realtime: no user IDs")
)
