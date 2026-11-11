// Package usecase holds the messaging module's logic: direct conversations between
// connections (FR-16.1), message requests to everyone else (FR-16.2), unread counts and
// read markers (FR-16.3), editing and deleting (FR-16.5), group conversations (FR-16.6), the
// send limits (FR-16.10), the membership check on every read and write (NFR-13), blocks and
// reports (FR-16.9) and an administrator's review of reports (NFR-13). It imports only the
// messaging domain.
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
	// EditWindow is how long after sending a message its sender may edit it (FR-16.5).
	EditWindow time.Duration
	// SearchesPerMinute is how many message searches a user may run a minute (FR-16.8).
	SearchesPerMinute int
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

// NopPublisher hears nothing.
type NopPublisher struct{}

var _ domain.Publisher = NopPublisher{}

// MessageSent does nothing.
func (NopPublisher) MessageSent(context.Context, domain.Sent, []string) {}

// ReadMoved does nothing.
func (NopPublisher) ReadMoved(context.Context, string, string, string, []string) {}

// MessageEdited does nothing.
func (NopPublisher) MessageEdited(context.Context, domain.Message, []string) {}

// MessageDeleted does nothing.
func (NopPublisher) MessageDeleted(context.Context, domain.Message, []string) {}

// RequestAccepted does nothing.
func (NopPublisher) RequestAccepted(context.Context, string, []string) {}

// RequestDeclined does nothing.
func (NopPublisher) RequestDeclined(context.Context, string, []string) {}

// PairChanged does nothing.
func (NopPublisher) PairChanged(context.Context, string, string) {}

// MembersChanged does nothing.
func (NopPublisher) MembersChanged(context.Context, string, domain.MemberChange, []string) {}

// AccessLost does nothing.
func (NopPublisher) AccessLost(context.Context, string, []string) {}

// AttachmentUpdated does nothing.
func (NopPublisher) AttachmentUpdated(context.Context, domain.AttachmentUpdate, []string) {}

// Service handles conversations and messages.
type Service struct {
	store       domain.Store
	tx          domain.Transactor
	connections domain.Connections
	blocks      domain.Blocks
	actors      domain.Actors
	limiter     domain.Limiter
	publisher   domain.Publisher
	notifier    domain.GroupNotifier
	messages    []window
	requests    []window
	reports     []window
	groups      []window
	members     []window // members added to groups
	editWindow  time.Duration
	now         func() time.Time
	logger      *slog.Logger
	files       FileDeps
	uploads     []window // attachment uploads per hour
	uploadBytes []window // attachment bytes per day
	searches    []window // message searches per minute
}

// New returns a Service. now is time.Now in production and a fixed clock in tests.
func New(store domain.Store, tx domain.Transactor, connections domain.Connections, blocks domain.Blocks, actors domain.Actors,
	limiter domain.Limiter, publisher domain.Publisher, notifier domain.GroupNotifier, limits Limits, now func() time.Time, logger *slog.Logger,
) *Service {
	return &Service{
		store: store, tx: tx, connections: connections, blocks: blocks, actors: actors, limiter: limiter, publisher: publisher,
		notifier: notifier, logger: logger,
		editWindow: limits.EditWindow, now: now,
		messages: []window{
			{"messages:minute", limits.MessagesPerMinute, time.Minute},
			{"messages:day", limits.MessagesPerDay, day},
		},
		requests: []window{
			{"requests:minute", limits.RequestsPerMinute, time.Minute},
			{"requests:day", limits.RequestsPerDay, day},
		},
		reports:  []window{{"reports:day", ReportsPerDay, day}},
		groups:   []window{{"groups:day", GroupsPerDay, day}},
		members:  []window{{"group-members:day", GroupMembersPerDay, day}},
		searches: []window{{"searches:minute", limits.SearchesPerMinute, time.Minute}},
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
	if err := s.checkLimits(ctx, senderID, s.messages, 1); err != nil {
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
			if err := s.checkLimits(ctx, senderID, s.requests, 1); err != nil {
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
		sent, err = s.add(ctx, c, senderID, &text, clientID, nil)
		return err
	})
	return s.finish(ctx, sent, err, []string{recipientID}, senderID, clientID, newRequest)
}

// Send sends a message in a conversation (FR-16.1, FR-16.2, FR-16.6, NFR-13). Checks run in
// this order: ErrInvalidBody, ErrNotFound when senderID is not a member, the client ID as in
// SendDirect, the sender's standing (see refuseSender), *RateLimitedError, ErrNotFound when the
// other member is suspended or not verified, a block (see refuseBlocked), then
// ErrRequestPending or ErrClosed. In a group there is no other member, no block applies and no
// request: after the limits only membership is checked again, under the conversation's lock.
func (s *Service) Send(ctx context.Context, senderID, conversationID, body, clientID string) (domain.Sent, error) {
	return s.SendWithAttachments(ctx, senderID, conversationID, &body, clientID, nil)
}

// SendWithAttachments is Send with files from UploadAttachment (FR-16.7). body may be nil, or
// blank, when attachmentIDs is not empty. The IDs are checked first (ErrInvalidAttachment: more
// than MaxAttachmentsPerMessage, a repeat, or not a UUID), with the body; then the checks of
// Send in its order; then, in the send's transaction, each attachment must be the sender's, of
// this conversation, unused, unexpired and not blocked or failed by its scan, else
// ErrInvalidAttachment and nothing is stored.
func (s *Service) SendWithAttachments(ctx context.Context, senderID, conversationID string, body *string, clientID string, attachmentIDs []string) (domain.Sent, error) {
	text, err := normalizeWithAttachments(body, len(attachmentIDs) > 0)
	if err != nil {
		return domain.Sent{}, err
	}
	if err := checkAttachmentIDs(attachmentIDs); err != nil {
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
	if err := s.checkLimits(ctx, senderID, s.messages, 1); err != nil {
		return domain.Sent{}, err
	}
	if c.IsGroup() {
		return s.sendInGroup(ctx, conversationID, senderID, text, clientID, attachmentIDs)
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
		sent, err = s.add(ctx, locked, senderID, text, clientID, attachmentIDs)
		return err
	})
	return s.finish(ctx, sent, err, []string{c.OtherID}, senderID, clientID, false)
}

// sendInGroup stores a message in a group and tells every other current member.
func (s *Service) sendInGroup(ctx context.Context, conversationID, senderID string, text *string, clientID string, attachmentIDs []string) (domain.Sent, error) {
	var (
		sent domain.Sent
		to   []string
	)
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.refuseSender(ctx, senderID); err != nil {
			return err
		}
		locked, err := s.lockMember(ctx, conversationID, senderID)
		if err != nil {
			return err
		}
		if sent, err = s.add(ctx, locked, senderID, text, clientID, attachmentIDs); err != nil {
			return err
		}
		to, err = s.others(ctx, conversationID, senderID)
		return err
	})
	return s.finish(ctx, sent, err, to, senderID, clientID, false)
}

// lockMember holds the conversation's row and reads it again as userID sees it, so a removal
// or a leave that committed meanwhile wins: a former member gets ErrNotFound (NFR-13).
func (s *Service) lockMember(ctx context.Context, conversationID, userID string) (domain.Conversation, error) {
	if _, err := s.store.LockConversation(ctx, conversationID); err != nil {
		return domain.Conversation{}, err
	}
	return s.store.FindConversation(ctx, conversationID, userID)
}

// others returns the conversation's current members other than userID.
func (s *Service) others(ctx context.Context, conversationID, userID string) ([]string, error) {
	ids, err := s.store.GroupMemberIDs(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != userID {
			out = append(out, id)
		}
	}
	return out, nil
}

// recipients is who hears about a change in c besides userID: the other member of a direct
// conversation, the other current members of a group.
func (s *Service) recipients(ctx context.Context, c domain.Conversation, userID string) ([]string, error) {
	if c.IsGroup() {
		return s.others(ctx, c.ID, userID)
	}
	return []string{c.OtherID}, nil
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
	if found {
		if err := s.withAttachments(ctx, sent.Message.ConversationID, []*domain.Message{&sent.Message}); err != nil {
			return domain.Sent{}, false, err
		}
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

// add stores the message in c and ties its attachments to it, in the caller's transaction.
func (s *Service) add(ctx context.Context, c domain.Conversation, senderID string, text *string, clientID string, attachmentIDs []string) (domain.Sent, error) {
	m, added, err := s.store.AddMessage(ctx, domain.Message{
		ConversationID: c.ID, SenderID: senderID, Body: text, ClientID: clientID, HasAttachments: len(attachmentIDs) > 0,
	})
	if err != nil {
		return domain.Sent{}, err
	}
	if !added {
		return domain.Sent{}, errDuplicate
	}
	if len(attachmentIDs) > 0 {
		n, err := s.store.ClaimAttachments(ctx, attachmentIDs, senderID, c.ID, m.ID)
		if err != nil {
			return domain.Sent{}, err
		}
		if n != len(attachmentIDs) {
			return domain.Sent{}, domain.ErrInvalidAttachment
		}
		if err := s.withAttachments(ctx, c.ID, []*domain.Message{&m}); err != nil {
			return domain.Sent{}, err
		}
	}
	return domain.Sent{Message: m, State: c.State}, nil
}

// finish runs after the transaction. A send that lost a race for its client ID, or that a
// racing send of the same client ID left refused, gets the original message. Otherwise it
// counts and publishes a stored message; a limiter failure only logs (fail open).
func (s *Service) finish(ctx context.Context, sent domain.Sent, err error, to []string, senderID, clientID string, newRequest bool) (domain.Sent, error) {
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
	s.record(ctx, senderID, s.messages, 1)
	if newRequest {
		s.record(ctx, senderID, s.requests, 1)
	}
	s.publisher.MessageSent(ctx, sent, to)
	return sent, nil
}

// checkLimits returns *RateLimitedError when n more would pass any of the windows, with the
// longest wait. A window the limiter cannot answer for is skipped (fail open, as the login
// lockout).
func (s *Service) checkLimits(ctx context.Context, userID string, windows []window, n int) error {
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
		if used+n > w.limit {
			full, wait = true, max(wait, left)
		}
	}
	if full {
		return &domain.RateLimitedError{RetryAfter: wait}
	}
	return nil
}

func (s *Service) record(ctx context.Context, userID string, windows []window, n int) {
	for _, w := range windows {
		if err := s.limiter.Record(ctx, w.name, userID, n, w.length); err != nil {
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
// stores the connection; the publisher waits for its commit.
func (s *Service) ConnectionMade(ctx context.Context, a, b string) error {
	if err := s.store.ActivateRequest(ctx, a, b); err != nil {
		return err
	}
	s.publisher.PairChanged(ctx, a, b)
	return nil
}

// DeclineRequests closes a pending request between a and b after a block (FR-16.9). It runs
// inside the block's transaction; the publisher waits for its commit. Any block changes
// canMessage, so it reports one with or without a request.
func (s *Service) DeclineRequests(ctx context.Context, a, b string) error {
	if err := s.store.DeclineRequests(ctx, a, b); err != nil {
		return err
	}
	s.publisher.PairChanged(ctx, a, b)
	return nil
}

// Unblocked reports that a block between a and b ended: canMessage may be true again.
func (s *Service) Unblocked(ctx context.Context, a, b string) {
	s.publisher.PairChanged(ctx, a, b)
}

// ConversationBetween returns a and b's conversation; found is false when there is none.
func (s *Service) ConversationBetween(ctx context.Context, a, b string) (string, bool, error) {
	return s.store.DirectID(ctx, a, b)
}

// TypingPeers returns who userID may tell about typing now (FR-16.4). In a direct
// conversation, the other member when userID is a member, it is active, the other member is
// available and neither blocked the other. In a group, every other current member except those
// in a block with userID either way (FR-16.6). Anything else, an unknown ID included, is none.
func (s *Service) TypingPeers(ctx context.Context, userID, id string) ([]string, error) {
	c, err := s.store.FindConversation(ctx, id, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !c.IsGroup() {
		it, err := s.store.FindItem(ctx, id, userID)
		if errors.Is(err, domain.ErrNotFound) || (err == nil && !it.CanMessage()) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return []string{it.OtherID}, nil
	}
	others, err := s.others(ctx, id, userID)
	if err != nil || len(others) == 0 {
		return nil, err
	}
	blocked, err := s.blocks.BlockedAmong(ctx, userID, others)
	if err != nil {
		return nil, err
	}
	return without(others, blocked), nil
}

// without returns ids without those in drop, in their order.
func without(ids, drop []string) []string {
	skip := make(map[string]bool, len(drop))
	for _, id := range drop {
		skip[id] = true
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !skip[id] {
			out = append(out, id)
		}
	}
	return out
}

// ActivePeers returns who may see the user's presence (FR-16.4).
func (s *Service) ActivePeers(ctx context.Context, userID string) ([]string, error) {
	return s.store.ActivePeers(ctx, userID)
}

// VisiblePeers returns which of ids viewerID may see, by the rule of ActivePeers.
func (s *Service) VisiblePeers(ctx context.Context, viewerID string, ids []string) ([]string, error) {
	return s.store.VisiblePeers(ctx, viewerID, ids)
}

// Partners returns every conversation of the user with its other member.
func (s *Service) Partners(ctx context.Context, userID string) ([]domain.Partner, error) {
	return s.store.Partners(ctx, userID)
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

// Get returns one of userID's conversations, ErrNotFound when they are not a member. A
// group comes with its current members.
func (s *Service) Get(ctx context.Context, userID, id string) (domain.Item, error) {
	it, err := s.store.FindItem(ctx, id, userID)
	if err != nil || it.Group == nil {
		return it, err
	}
	if it.Group.Members, err = s.store.ListGroupMembers(ctx, id); err != nil {
		return domain.Item{}, err
	}
	return it, nil
}

// Messages returns one page of a conversation's messages, newest first, and the other
// member's read marker (FR-16.3), or in a group every other member's. A member sees only the
// messages created from the moment they joined (FR-16.6). ErrNotFound when userID is not a
// member. q picks the page (domain.MessageQuery): more than one cursor, or a cursor that is not
// a UUID, is an InvalidFieldError naming it; an Around message the caller cannot see is
// ErrMessageNotFound. A nil limit means DefaultMessageLimit; above MaxMessageLimit it is
// reduced; below 1 it is ErrInvalidLimit. People names everyone the page shows.
func (s *Service) Messages(ctx context.Context, userID, id string, q domain.MessageQuery) (domain.MessagePage, error) {
	c, err := s.store.FindConversation(ctx, id, userID)
	if err != nil {
		return domain.MessagePage{}, err
	}
	if err := checkMessageQuery(q); err != nil {
		return domain.MessagePage{}, err
	}
	limit, err := pageLimit(q.Limit, DefaultMessageLimit, MaxMessageLimit)
	if err != nil {
		return domain.MessagePage{}, err
	}
	var page domain.MessagePage
	switch {
	case q.Around != "":
		page, err = s.messagesAround(ctx, userID, id, q.Around, limit)
	case q.After != "":
		page, err = s.messagesAfter(ctx, userID, id, q.After, limit)
	default:
		page, err = s.messagesBefore(ctx, userID, id, q.Before, limit)
	}
	if err != nil {
		return domain.MessagePage{}, err
	}
	ptrs := make([]*domain.Message, len(page.Items))
	for i := range page.Items {
		ptrs[i] = &page.Items[i]
	}
	if err := s.withAttachments(ctx, id, ptrs); err != nil {
		return domain.MessagePage{}, err
	}
	page.OtherReadID = c.OtherReadID
	if c.IsGroup() {
		if page.Readers, err = s.store.ListReaders(ctx, id, userID); err != nil {
			return domain.MessagePage{}, err
		}
	}
	if page.People, err = s.pagePeople(ctx, userID, messageRefs(page.Items)); err != nil {
		return domain.MessagePage{}, err
	}
	return page, nil
}

// checkMessageQuery allows at most one cursor, each a UUID.
func checkMessageQuery(q domain.MessageQuery) error {
	set := 0
	for _, f := range []struct{ name, value string }{{"before", q.Before}, {"after", q.After}, {"around", q.Around}} {
		if f.value == "" {
			continue
		}
		set++
		if set > 1 || !isUUID(f.value) {
			return &domain.InvalidFieldError{Field: f.name}
		}
	}
	return nil
}

func (s *Service) messagesBefore(ctx context.Context, userID, id, before string, limit int) (domain.MessagePage, error) {
	items, err := s.store.ListMessages(ctx, id, userID, before, limit+1)
	if err != nil {
		return domain.MessagePage{}, err
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		next = &items[limit-1].ID
	}
	return domain.MessagePage{Items: items, NextBefore: next}, nil
}

// messagesAfter leaves NextBefore nil: the client already holds the older messages.
func (s *Service) messagesAfter(ctx context.Context, userID, id, after string, limit int) (domain.MessagePage, error) {
	newer, err := s.store.ListMessagesAfter(ctx, id, userID, after, limit+1)
	if err != nil {
		return domain.MessagePage{}, err
	}
	var next *string
	if len(newer) > limit {
		newer = newer[:limit]
		next = &newer[limit-1].ID
	}
	return domain.MessagePage{Items: reversed(newer), NextAfter: next}, nil
}

// messagesAround centres the page on one message: up to (limit-1)/2 older ones, the rest
// newer, a short side's room going to the other.
func (s *Service) messagesAround(ctx context.Context, userID, id, around string, limit int) (domain.MessagePage, error) {
	anchor, err := s.store.FindVisibleMessage(ctx, id, userID, around)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.MessagePage{}, domain.ErrMessageNotFound
	}
	if err != nil {
		return domain.MessagePage{}, err
	}
	older, err := s.store.ListMessages(ctx, id, userID, around, limit) // newest first
	if err != nil {
		return domain.MessagePage{}, err
	}
	newer, err := s.store.ListMessagesAfter(ctx, id, userID, around, limit) // oldest first
	if err != nil {
		return domain.MessagePage{}, err
	}
	nOld, nNew := aroundSplit(limit, len(older), len(newer))
	page := domain.MessagePage{}
	page.Items = append(page.Items, reversed(newer[:nNew])...)
	page.Items = append(page.Items, anchor)
	page.Items = append(page.Items, older[:nOld]...)
	if len(older) > nOld {
		page.NextBefore = &page.Items[len(page.Items)-1].ID
	}
	if len(newer) > nNew {
		page.NextAfter = &page.Items[0].ID
	}
	return page, nil
}

// aroundSplit says how many of the older and newer messages a page centred on one message
// keeps, given how many there are: (limit-1)/2 older and the rest newer, a short side's room
// going to the other.
func aroundSplit(limit, older, newer int) (keepOld, keepNew int) {
	room := limit - 1
	keepOld = min(older, room/2)
	keepNew = min(newer, room-keepOld)
	keepOld = min(older, room-keepNew)
	return keepOld, keepNew
}

func reversed(ms []domain.Message) []domain.Message {
	out := make([]domain.Message, len(ms))
	for i, m := range ms {
		out[len(ms)-1-i] = m
	}
	return out
}

// messageRefs lists every sender and every user a system message names.
func messageRefs(items []domain.Message) []domain.PersonRef {
	var refs []domain.PersonRef
	for _, m := range items {
		if m.SenderID != "" {
			refs = append(refs, domain.PersonRef{ConversationID: m.ConversationID, UserID: m.SenderID})
		}
		if m.Event != nil {
			for _, u := range m.Event.UserIDs {
				refs = append(refs, domain.PersonRef{ConversationID: m.ConversationID, UserID: u})
			}
		}
	}
	return refs
}

// pagePeople looks the references up in one query, each pair once.
func (s *Service) pagePeople(ctx context.Context, userID string, refs []domain.PersonRef) (map[string]domain.PagePerson, error) {
	seen := make(map[domain.PersonRef]bool, len(refs))
	unique := refs[:0:0]
	for _, r := range refs {
		if !seen[r] {
			seen[r] = true
			unique = append(unique, r)
		}
	}
	people := make(map[string]domain.PagePerson, len(unique))
	if len(unique) == 0 {
		return people, nil
	}
	found, err := s.store.People(ctx, userID, unique)
	if err != nil {
		return nil, err
	}
	for _, p := range found {
		people[p.UserID] = p
	}
	return people, nil
}

// MarkRead moves userID's read marker forward to messageID (FR-16.3). A message at or
// before the marker succeeds and changes nothing. ErrNotFound when userID is not a member
// or the message is not in this conversation, or from before they joined.
func (s *Service) MarkRead(ctx context.Context, userID, id, messageID string) error {
	c, err := s.store.FindConversation(ctx, id, userID)
	if err != nil {
		return err
	}
	in, err := s.store.MessageIn(ctx, id, messageID, c.JoinedAt)
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
	if !moved {
		return nil
	}
	to, err := s.recipients(ctx, c, userID)
	if err != nil {
		return err
	}
	s.publisher.ReadMoved(ctx, id, userID, messageID, to)
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
