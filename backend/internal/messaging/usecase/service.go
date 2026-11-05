// Package usecase holds the messaging module's logic: direct conversations between
// connections (FR-16.1), message requests to everyone else (FR-16.2), unread counts and
// read markers (FR-16.3), the send limits (FR-16.10), the membership check on every read
// and write (NFR-14), blocks and reports (FR-16.9) and an administrator's review of reports
// (NFR-14). It imports only the messaging domain.
package usecase

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// Limits on what a list or a message may hold.
const (
	// MaxBodyLength is the longest message body, in characters.
	MaxBodyLength = 5000
	// DefaultConversationLimit and MaxConversationLimit are the page sizes of the lists of
	// conversations.
	DefaultConversationLimit = 20
	MaxConversationLimit     = 50
	// DefaultMessageLimit and MaxMessageLimit are the page sizes of a conversation's
	// messages. A chat reads more at a time than other lists (docs/decisions.md).
	DefaultMessageLimit = 30
	MaxMessageLimit     = 100
)

// Limits are the send limits per user (FR-16.10), from the configuration.
type Limits struct {
	MessagesPerMinute int
	MessagesPerDay    int
	RequestsPerMinute int // new message requests
	RequestsPerDay    int
}

// window is one fixed-window limit; name is its Limiter counter.
type window struct {
	name   string
	limit  int
	length time.Duration
}

const day = 24 * time.Hour

// errDuplicate says a racing send of the same client ID won. It rolls the transaction
// back, so a conversation created for the losing send is not left empty.
var errDuplicate = errors.New("messaging: client ID already used")

// NopPublisher hears nothing. cmd/api uses it until real-time delivery (slice 14A.3).
type NopPublisher struct{}

var _ domain.Publisher = NopPublisher{}

// MessageSent does nothing.
func (NopPublisher) MessageSent(context.Context, domain.Message, []string) {}

// ReadMoved does nothing.
func (NopPublisher) ReadMoved(context.Context, string, string, string, []string) {}

// RequestAccepted does nothing.
func (NopPublisher) RequestAccepted(context.Context, string, []string) {}

// RequestDeclined does nothing.
func (NopPublisher) RequestDeclined(context.Context, string, []string) {}

// Service handles conversations and messages.
type Service struct {
	store       domain.Store
	tx          domain.Transactor
	connections domain.Connections
	blocks      domain.Blocks
	actors      domain.Actors
	limiter     domain.Limiter
	publisher   domain.Publisher
	messages    []window
	requests    []window
	reports     []window
	logger      *slog.Logger
}

// New returns a Service.
func New(store domain.Store, tx domain.Transactor, connections domain.Connections, blocks domain.Blocks, actors domain.Actors,
	limiter domain.Limiter, publisher domain.Publisher, limits Limits, logger *slog.Logger,
) *Service {
	return &Service{
		store: store, tx: tx, connections: connections, blocks: blocks, actors: actors, limiter: limiter, publisher: publisher, logger: logger,
		messages: []window{
			{"messages:minute", limits.MessagesPerMinute, time.Minute},
			{"messages:day", limits.MessagesPerDay, day},
		},
		requests: []window{
			{"requests:minute", limits.RequestsPerMinute, time.Minute},
			{"requests:day", limits.RequestsPerDay, day},
		},
		reports: []window{{"reports:day", ReportsPerDay, day}},
	}
}

// SendDirect sends a message from senderID to recipientID, finding or creating their
// conversation (FR-16.1, FR-16.2). Checks run in this order: ErrSelf, ErrInvalidBody, the
// client ID (a value used before returns the original with Replayed set), the sender's standing
// (see refuseSender), *RateLimitedError for messages, ErrNotFound for a recipient who is unknown, suspended or
// not verified, a block (see refuseBlocked), *RateLimitedError for requests when this would
// create one, then the conversation's state: ErrRequestPending or ErrClosed.
func (s *Service) SendDirect(ctx context.Context, senderID, recipientID, body, clientID string) (domain.Sent, error) {
	if senderID == recipientID {
		return domain.Sent{}, domain.ErrSelf
	}
	text, err := NormalizeBody(body)
	if err != nil {
		return domain.Sent{}, err
	}
	if sent, found, err := s.replay(ctx, senderID, clientID); found || err != nil {
		return sent, err
	}
	if err := s.refuseSender(ctx, senderID); err != nil {
		return domain.Sent{}, err
	}
	if err := s.checkLimits(ctx, senderID, s.messages); err != nil {
		return domain.Sent{}, err
	}
	visible, err := s.store.UserVisible(ctx, recipientID)
	if err != nil {
		return domain.Sent{}, err
	}
	if !visible {
		return domain.Sent{}, domain.ErrNotFound
	}
	exists, err := s.store.DirectExists(ctx, senderID, recipientID)
	if err != nil {
		return domain.Sent{}, err
	}
	if err := s.refuseBlocked(ctx, senderID, recipientID, exists); err != nil {
		return domain.Sent{}, err
	}
	state, requester := domain.StateActive, (*string)(nil)
	if !exists {
		connected, err := s.connections.Connected(ctx, senderID, recipientID)
		if err != nil {
			return domain.Sent{}, err
		}
		if !connected {
			if err := s.checkLimits(ctx, senderID, s.requests); err != nil {
				return domain.Sent{}, err
			}
			state, requester = domain.StateRequest, &senderID
		}
	}

	var (
		sent       domain.Sent
		newRequest bool
	)
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		var (
			c       domain.Conversation
			created bool
			err     error
		)
		if err := s.refuseSender(ctx, senderID); err != nil {
			return err
		}
		if err := s.lockAndRefuseBlocked(ctx, senderID, recipientID, exists); err != nil {
			return err
		}
		if !exists {
			if c, created, err = s.store.CreateDirect(ctx, senderID, recipientID, state, requester); err != nil {
				return err
			}
		}
		if !created {
			// It existed, or a first message sent at the same moment created it.
			if c, err = s.store.LockDirect(ctx, senderID, recipientID); err != nil {
				return err
			}
			if err := s.admit(ctx, &c, senderID, recipientID); err != nil {
				return err
			}
		}
		newRequest = created && state == domain.StateRequest
		sent, err = s.add(ctx, c, senderID, text, clientID)
		return err
	})
	return s.finish(ctx, sent, err, recipientID, senderID, clientID, newRequest)
}

// Send sends a message in a conversation (FR-16.1, FR-16.2, NFR-14). Checks run in this
// order: ErrInvalidBody, ErrNotFound when senderID is not a member, the client ID as in
// SendDirect, the sender's standing (see refuseSender), *RateLimitedError, ErrNotFound when the other member is suspended or not
// verified, a block (see refuseBlocked), then ErrRequestPending or ErrClosed.
func (s *Service) Send(ctx context.Context, senderID, conversationID, body, clientID string) (domain.Sent, error) {
	text, err := NormalizeBody(body)
	if err != nil {
		return domain.Sent{}, err
	}
	c, err := s.store.FindConversation(ctx, conversationID, senderID)
	if err != nil {
		return domain.Sent{}, err
	}
	if sent, found, err := s.replay(ctx, senderID, clientID); found || err != nil {
		return sent, err
	}
	if err := s.refuseSender(ctx, senderID); err != nil {
		return domain.Sent{}, err
	}
	if err := s.checkLimits(ctx, senderID, s.messages); err != nil {
		return domain.Sent{}, err
	}
	visible, err := s.store.UserVisible(ctx, c.OtherID)
	if err != nil {
		return domain.Sent{}, err
	}
	if !visible {
		return domain.Sent{}, domain.ErrNotFound
	}
	if err := s.refuseBlocked(ctx, senderID, c.OtherID, true); err != nil {
		return domain.Sent{}, err
	}
	var sent domain.Sent
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.refuseSender(ctx, senderID); err != nil {
			return err
		}
		if err := s.lockAndRefuseBlocked(ctx, senderID, c.OtherID, true); err != nil {
			return err
		}
		locked, err := s.store.LockConversation(ctx, conversationID)
		if err != nil {
			return err
		}
		if err := s.admit(ctx, &locked, senderID, c.OtherID); err != nil {
			return err
		}
		sent, err = s.add(ctx, locked, senderID, text, clientID)
		return err
	})
	return s.finish(ctx, sent, err, c.OtherID, senderID, clientID, false)
}

// refuseSuspended returns *SuspendedError when the user's account is suspended now. Inside a
// transaction it holds the user's row (domain.Actors).
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

// refuseSender is refuseSuspended, and *RestrictedError during a messaging restriction. A send
// runs it before the limits, so a refusal uses no quota, and first in its transaction, so a
// suspension or restriction that commits meanwhile cannot be passed.
func (s *Service) refuseSender(ctx context.Context, senderID string) error {
	st, err := s.actors.Standing(ctx, senderID)
	if err != nil {
		return err
	}
	switch {
	case st.Suspended:
		return &domain.SuspendedError{Reason: st.SuspensionReason, Until: st.SuspendedUntil}
	case st.RestrictedUntil != nil:
		return &domain.RestrictedError{Until: *st.RestrictedUntil}
	}
	return nil
}

// refuseBlocked refuses a send when either user has blocked the other (FR-16.9). The caller
// who blocked gets ErrYouBlocked, also when blocked back. The blocked person gets
// ErrCannotMessage, or, when the two have no conversation, ErrNotFound: the answer they get
// for that user's profile, so a first message does not reveal the block.
func (s *Service) refuseBlocked(ctx context.Context, senderID, otherID string, exists bool) error {
	state, err := s.blocks.Between(ctx, senderID, otherID)
	switch {
	case err != nil:
		return err
	case state.ByMe:
		return domain.ErrYouBlocked
	case state.ByThem && exists:
		return domain.ErrCannotMessage
	case state.ByThem:
		return domain.ErrNotFound
	}
	return nil
}

// lockAndRefuseBlocked is refuseBlocked under the pair's lock, inside the send's transaction
// and before its other locks. Once a block has committed no send can pass this. The check
// before the transaction only keeps a refused send from reaching the request limits.
func (s *Service) lockAndRefuseBlocked(ctx context.Context, senderID, otherID string, exists bool) error {
	if err := s.blocks.Lock(ctx, senderID, otherID); err != nil {
		return err
	}
	return s.refuseBlocked(ctx, senderID, otherID, exists)
}

// replay returns the message senderID already sent with clientID, if any.
func (s *Service) replay(ctx context.Context, senderID, clientID string) (domain.Sent, bool, error) {
	sent, found, err := s.store.FindSent(ctx, senderID, clientID)
	if err != nil {
		return domain.Sent{}, false, err
	}
	sent.Replayed = found
	return sent, found, nil
}

// admit decides whether an existing conversation, locked by the caller, takes a new
// message. A pending request refuses both members, unless they have connected since: a
// connection accepted while the request was being created finds no conversation to
// activate (ConnectionMade), so the first send afterwards does it.
func (s *Service) admit(ctx context.Context, c *domain.Conversation, senderID, otherID string) error {
	switch c.State {
	case domain.StateDeclined:
		return domain.ErrClosed
	case domain.StateRequest:
		connected, err := s.connections.Connected(ctx, senderID, otherID)
		if err != nil {
			return err
		}
		if !connected {
			return domain.ErrRequestPending
		}
		if err := s.store.ActivateRequest(ctx, senderID, otherID); err != nil {
			return err
		}
		c.State = domain.StateActive
	}
	return nil
}

// add stores the message in c.
func (s *Service) add(ctx context.Context, c domain.Conversation, senderID, text, clientID string) (domain.Sent, error) {
	m, added, err := s.store.AddMessage(ctx, domain.Message{ConversationID: c.ID, SenderID: senderID, Body: text, ClientID: clientID})
	if err != nil {
		return domain.Sent{}, err
	}
	if !added {
		return domain.Sent{}, errDuplicate
	}
	return domain.Sent{Message: m, State: c.State}, nil
}

// finish runs after the transaction. A send that lost a race for its client ID, or that a
// racing send of the same client ID left refused, gets the original message. Otherwise it
// counts and publishes a stored message; a limiter failure only logs (fail open).
func (s *Service) finish(ctx context.Context, sent domain.Sent, err error, recipientID, senderID, clientID string, newRequest bool) (domain.Sent, error) {
	if errors.Is(err, errDuplicate) || errors.Is(err, domain.ErrRequestPending) || errors.Is(err, domain.ErrClosed) {
		won, found, findErr := s.replay(ctx, senderID, clientID)
		switch {
		case findErr != nil:
			return domain.Sent{}, findErr
		case found:
			return won, nil
		case errors.Is(err, errDuplicate):
			return domain.Sent{}, errors.New("send message: client ID refused by a message that is gone")
		}
		return domain.Sent{}, err
	}
	if err != nil {
		return domain.Sent{}, err
	}
	s.record(ctx, senderID, s.messages)
	if newRequest {
		s.record(ctx, senderID, s.requests)
	}
	s.publisher.MessageSent(ctx, sent.Message, []string{recipientID})
	return sent, nil
}

// checkLimits returns *RateLimitedError when any of the windows is full, with the longest
// wait. A window the limiter cannot answer for is skipped (fail open, as the login lockout).
func (s *Service) checkLimits(ctx context.Context, userID string, windows []window) error {
	var (
		full bool
		wait time.Duration
	)
	for _, w := range windows {
		used, left, err := s.limiter.Used(ctx, w.name, userID)
		if err != nil {
			s.logger.WarnContext(ctx, "message rate limit unavailable, allowing the send", slog.Any("error", err))
			continue
		}
		if used >= w.limit {
			full, wait = true, max(wait, left)
		}
	}
	if full {
		return &domain.RateLimitedError{RetryAfter: wait}
	}
	return nil
}

func (s *Service) record(ctx context.Context, userID string, windows []window) {
	for _, w := range windows {
		if err := s.limiter.Record(ctx, w.name, userID, w.length); err != nil {
			s.logger.WarnContext(ctx, "message rate limit not recorded", slog.Any("error", err))
		}
	}
}

// NormalizeBody trims a message and checks it: 1 to MaxBodyLength characters, valid text
// and no NUL, which PostgreSQL cannot store. Line breaks inside it stay.
func NormalizeBody(body string) (string, error) {
	if !utf8.ValidString(body) || strings.ContainsRune(body, 0) {
		return "", domain.ErrInvalidBody
	}
	text := strings.TrimSpace(body)
	if n := utf8.RuneCountInString(text); n < 1 || n > MaxBodyLength {
		return "", domain.ErrInvalidBody
	}
	return text, nil
}

// ConnectionMade turns a pending request between a and b into an active conversation, when
// there is one. A declined conversation stays closed. It runs inside the transaction that
// stores the connection, so it cannot publish yet (docs/decisions.md).
func (s *Service) ConnectionMade(ctx context.Context, a, b string) error {
	return s.store.ActivateRequest(ctx, a, b)
}

// DeclineRequests closes a pending request between a and b after a block (FR-16.9). It runs
// inside the block's transaction, so it cannot publish yet, like ConnectionMade.
func (s *Service) DeclineRequests(ctx context.Context, a, b string) error {
	return s.store.DeclineRequests(ctx, a, b)
}

// List returns one page of userID's conversations in box, newest activity first. An empty
// box means the inbox. A nil limit means DefaultConversationLimit; above
// MaxConversationLimit it is reduced; below 1 it is ErrInvalidLimit. cursor is the
// previous page's NextCursor, or empty; one the server did not issue is ErrInvalidCursor.
func (s *Service) List(ctx context.Context, userID, box, cursor string, limitParam *int) (domain.Page, error) {
	b := domain.Box(box)
	switch b {
	case "":
		b = domain.BoxInbox
	case domain.BoxInbox, domain.BoxRequests:
	default:
		return domain.Page{}, domain.ErrInvalidBox
	}
	limit, err := pageLimit(limitParam, DefaultConversationLimit, MaxConversationLimit)
	if err != nil {
		return domain.Page{}, err
	}
	var after *domain.Cursor
	if cursor != "" {
		c, err := DecodeCursor(cursor)
		if err != nil {
			return domain.Page{}, err
		}
		after = &c
	}
	items, err := s.store.ListItems(ctx, userID, b, after, limit+1)
	if err != nil {
		return domain.Page{}, err
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		c := EncodeCursor(domain.Cursor{LastMessageAt: last.LastMessageAt, ID: last.ID})
		next = &c
	}
	return domain.Page{Items: items, NextCursor: next}, nil
}

// Get returns one of userID's conversations, ErrNotFound when they are not a member.
func (s *Service) Get(ctx context.Context, userID, id string) (domain.Item, error) {
	return s.store.FindItem(ctx, id, userID)
}

// Messages returns one page of a conversation's messages, newest first, and the other
// member's read marker (FR-16.3). ErrNotFound when userID is not a member; before is the
// previous page's NextBefore, or empty, and anything but a UUID is ErrInvalidCursor. A nil
// limit means DefaultMessageLimit; above MaxMessageLimit it is reduced; below 1 it is
// ErrInvalidLimit.
func (s *Service) Messages(ctx context.Context, userID, id, before string, limitParam *int) (domain.MessagePage, error) {
	c, err := s.store.FindConversation(ctx, id, userID)
	if err != nil {
		return domain.MessagePage{}, err
	}
	if before != "" && !isUUID(before) {
		return domain.MessagePage{}, domain.ErrInvalidCursor
	}
	limit, err := pageLimit(limitParam, DefaultMessageLimit, MaxMessageLimit)
	if err != nil {
		return domain.MessagePage{}, err
	}
	items, err := s.store.ListMessages(ctx, id, before, limit+1)
	if err != nil {
		return domain.MessagePage{}, err
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1].ID
		next = &last
	}
	return domain.MessagePage{Items: items, NextBefore: next, OtherReadID: c.OtherReadID}, nil
}

// MarkRead moves userID's read marker forward to messageID (FR-16.3). A message at or
// before the marker succeeds and changes nothing. ErrNotFound when userID is not a member
// or the message is not in this conversation.
func (s *Service) MarkRead(ctx context.Context, userID, id, messageID string) error {
	c, err := s.store.FindConversation(ctx, id, userID)
	if err != nil {
		return err
	}
	in, err := s.store.MessageIn(ctx, id, messageID)
	if err != nil {
		return err
	}
	if !in {
		return domain.ErrNotFound
	}
	moved, err := s.store.MoveReadMarker(ctx, id, userID, messageID)
	if err != nil {
		return err
	}
	if moved {
		s.publisher.ReadMoved(ctx, id, userID, messageID, []string{c.OtherID})
	}
	return nil
}

// Accept makes a pending request active (FR-16.2). Decline closes it. Both are for the
// receiver only, and anything else is ErrNotFound, so the answer reveals nothing. A suspended
// caller gets *SuspendedError; a restricted one may still answer.
func (s *Service) Accept(ctx context.Context, userID, id string) (domain.Item, error) {
	return s.answer(ctx, userID, id, domain.StateActive, s.publisher.RequestAccepted)
}

// Decline closes a pending request for good (FR-16.2), under the same conditions as Accept.
func (s *Service) Decline(ctx context.Context, userID, id string) (domain.Item, error) {
	return s.answer(ctx, userID, id, domain.StateDeclined, s.publisher.RequestDeclined)
}

func (s *Service) answer(ctx context.Context, userID, id string, state domain.State,
	publish func(context.Context, string, []string),
) (domain.Item, error) {
	var (
		requester string
		found     bool
	)
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.refuseSuspended(ctx, userID); err != nil {
			return err
		}
		var err error
		requester, found, err = s.store.AnswerRequest(ctx, id, userID, state)
		return err
	})
	if err != nil {
		return domain.Item{}, err
	}
	if !found {
		return domain.Item{}, domain.ErrNotFound
	}
	publish(ctx, id, []string{requester})
	return s.store.FindItem(ctx, id, userID)
}

// UnreadCounts returns what the messaging badge shows (FR-16.3).
func (s *Service) UnreadCounts(ctx context.Context, userID string) (domain.UnreadCounts, error) {
	return s.store.CountUnread(ctx, userID)
}

func pageLimit(param *int, def, maximum int) (int, error) {
	limit := def
	if param != nil {
		limit = min(*param, maximum)
	}
	if limit < 1 {
		return 0, domain.ErrInvalidLimit
	}
	return limit, nil
}

// EncodeCursor turns a position into the opaque value the client sends back.
func EncodeCursor(c domain.Cursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.LastMessageAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID))
}

// DecodeCursor reads a value made by EncodeCursor; anything else is ErrInvalidCursor.
func DecodeCursor(s string) (domain.Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return domain.Cursor{}, domain.ErrInvalidCursor
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok || !isUUID(id) {
		return domain.Cursor{}, domain.ErrInvalidCursor
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return domain.Cursor{}, domain.ErrInvalidCursor
	}
	return domain.Cursor{LastMessageAt: t, ID: id}, nil
}

// isUUID reports whether s is a UUID in its canonical 36-character form.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if c != '-' {
				return false
			}
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}
