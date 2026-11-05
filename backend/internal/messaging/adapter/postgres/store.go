// Package postgres implements the messaging domain's Store with the queries that sqlc
// generates from queries.sql. Every query runs in the transaction the context carries
// (platform/postgres.Transactor).
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// Store reads and writes conversations and messages through the shared pool.
type Store struct {
	pool *pgxpool.Pool
}

var _ domain.Store = (*Store)(nil)

// NewStore returns a Store that uses pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// q returns the queries on the context's transaction, or on the pool.
func (s *Store) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Conn(ctx, s.pool))
}

// UserVisible is false for a value that is not a UUID.
func (s *Store) UserVisible(ctx context.Context, userID string) (bool, error) {
	id, ok := parseUUID(userID)
	if !ok {
		return false, nil
	}
	v, err := s.q(ctx).UserVisible(ctx, id)
	if err != nil {
		return false, fmt.Errorf("check user: %w", err)
	}
	return v, nil
}

// FindSent finds nothing for IDs that are not UUIDs.
func (s *Store) FindSent(ctx context.Context, senderID, clientID string) (domain.Sent, bool, error) {
	sender, ok1 := parseUUID(senderID)
	client, ok2 := parseUUID(clientID)
	if !ok1 || !ok2 {
		return domain.Sent{}, false, nil
	}
	r, err := s.q(ctx).FindSent(ctx, sqlcgen.FindSentParams{SenderID: sender, ClientID: client})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Sent{}, false, nil
	}
	if err != nil {
		return domain.Sent{}, false, fmt.Errorf("find sent message: %w", err)
	}
	m := message(r.ID, r.ConversationID, r.SenderID, r.Body, r.ClientID, r.CreatedAt)
	return domain.Sent{Message: m, State: domain.State(r.State)}, true, nil
}

// DirectExists is false for IDs that are not UUIDs.
func (s *Store) DirectExists(ctx context.Context, a, b string) (bool, error) {
	ua, ok1 := parseUUID(a)
	ub, ok2 := parseUUID(b)
	if !ok1 || !ok2 {
		return false, nil
	}
	v, err := s.q(ctx).DirectExists(ctx, sqlcgen.DirectExistsParams{A: ua, B: ub})
	if err != nil {
		return false, fmt.Errorf("find direct conversation: %w", err)
	}
	return v, nil
}

// CreateDirect relies on conversations_pair_uq: ON CONFLICT DO NOTHING returns no row when
// the pair already has a conversation, also one committed while this insert waited. The
// IDs come from a user the usecase has checked, so anything else is a programming error.
func (s *Store) CreateDirect(ctx context.Context, a, b string, state domain.State, requestedBy *string) (domain.Conversation, bool, error) {
	ua, ok1 := parseUUID(a)
	ub, ok2 := parseUUID(b)
	if !ok1 || !ok2 {
		return domain.Conversation{}, false, fmt.Errorf("create conversation: %q or %q is not a UUID", a, b)
	}
	arg := sqlcgen.CreateDirectParams{State: string(state), A: ua, B: ub}
	if requestedBy != nil {
		id, ok := parseUUID(*requestedBy)
		if !ok {
			return domain.Conversation{}, false, fmt.Errorf("create conversation: requester %q is not a UUID", *requestedBy)
		}
		arg.RequestedBy = id
	}
	q := s.q(ctx)
	r, err := q.CreateDirect(ctx, arg)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Conversation{}, false, nil
	}
	if err != nil {
		return domain.Conversation{}, false, fmt.Errorf("create conversation: %w", err)
	}
	for _, user := range []pgtype.UUID{ua, ub} {
		if err := q.AddMember(ctx, sqlcgen.AddMemberParams{ConversationID: r.ID, UserID: user}); err != nil {
			return domain.Conversation{}, false, fmt.Errorf("add conversation member: %w", err)
		}
	}
	return conversation(r.ID, r.State, r.RequestedBy, r.LastMessageAt), true, nil
}

// LockDirect returns domain.ErrNotFound for IDs that are not UUIDs, or no conversation.
func (s *Store) LockDirect(ctx context.Context, a, b string) (domain.Conversation, error) {
	ua, ok1 := parseUUID(a)
	ub, ok2 := parseUUID(b)
	if !ok1 || !ok2 {
		return domain.Conversation{}, domain.ErrNotFound
	}
	r, err := s.q(ctx).LockDirect(ctx, sqlcgen.LockDirectParams{A: ua, B: ub})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Conversation{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("lock conversation: %w", err)
	}
	return conversation(r.ID, r.State, r.RequestedBy, r.LastMessageAt), nil
}

// LockConversation returns domain.ErrNotFound for a missing conversation or a bad ID.
func (s *Store) LockConversation(ctx context.Context, id string) (domain.Conversation, error) {
	cid, ok := parseUUID(id)
	if !ok {
		return domain.Conversation{}, domain.ErrNotFound
	}
	r, err := s.q(ctx).LockConversation(ctx, cid)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Conversation{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("lock conversation: %w", err)
	}
	return conversation(r.ID, r.State, r.RequestedBy, r.LastMessageAt), nil
}

// ActivateRequest changes nothing for IDs that are not UUIDs.
func (s *Store) ActivateRequest(ctx context.Context, a, b string) error {
	ua, ok1 := parseUUID(a)
	ub, ok2 := parseUUID(b)
	if !ok1 || !ok2 {
		return nil
	}
	if err := s.q(ctx).ActivateRequest(ctx, sqlcgen.ActivateRequestParams{A: ua, B: ub}); err != nil {
		return fmt.Errorf("activate request: %w", err)
	}
	return nil
}

// AddMessage needs a transaction in the context to keep its three writes together; the
// usecase calls it inside one, holding the conversation's row.
func (s *Store) AddMessage(ctx context.Context, m domain.Message) (domain.Message, bool, error) {
	conv, ok1 := parseUUID(m.ConversationID)
	sender, ok2 := parseUUID(m.SenderID)
	client, ok3 := parseUUID(m.ClientID)
	if !ok1 || !ok2 || !ok3 {
		return domain.Message{}, false, fmt.Errorf("add message: conversation %q, sender %q or client ID %q is not a UUID", m.ConversationID, m.SenderID, m.ClientID)
	}
	q := s.q(ctx)
	r, err := q.InsertMessage(ctx, sqlcgen.InsertMessageParams{ConversationID: conv, SenderID: sender, Body: m.Body, ClientID: client})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Message{}, false, nil
	}
	if err != nil {
		return domain.Message{}, false, fmt.Errorf("add message: %w", err)
	}
	if err := q.TouchConversation(ctx, sqlcgen.TouchConversationParams{ID: conv, LastMessageAt: r.CreatedAt}); err != nil {
		return domain.Message{}, false, fmt.Errorf("update conversation: %w", err)
	}
	if _, err := q.MoveReadMarker(ctx, sqlcgen.MoveReadMarkerParams{MessageID: r.ID, ConversationID: conv, UserID: sender}); err != nil {
		return domain.Message{}, false, fmt.Errorf("move sender's read marker: %w", err)
	}
	return message(r.ID, r.ConversationID, r.SenderID, r.Body, r.ClientID, r.CreatedAt), true, nil
}

// FindConversation returns domain.ErrNotFound for a bad ID or a non-member.
func (s *Store) FindConversation(ctx context.Context, id, memberID string) (domain.Conversation, error) {
	cid, ok1 := parseUUID(id)
	user, ok2 := parseUUID(memberID)
	if !ok1 || !ok2 {
		return domain.Conversation{}, domain.ErrNotFound
	}
	r, err := s.q(ctx).FindConversation(ctx, sqlcgen.FindConversationParams{ID: cid, UserID: user})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Conversation{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("find conversation: %w", err)
	}
	c := conversation(r.ID, r.State, r.RequestedBy, r.LastMessageAt)
	c.OtherID, c.OtherReadID = r.OtherID.String(), uuidPtr(r.OtherReadID)
	return c, nil
}

// FindItem returns domain.ErrNotFound for a bad ID or a non-member.
func (s *Store) FindItem(ctx context.Context, id, memberID string) (domain.Item, error) {
	cid, ok1 := parseUUID(id)
	user, ok2 := parseUUID(memberID)
	if !ok1 || !ok2 {
		return domain.Item{}, domain.ErrNotFound
	}
	r, err := s.q(ctx).FindItem(ctx, sqlcgen.FindItemParams{ID: cid, UserID: user})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Item{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Item{}, fmt.Errorf("find conversation: %w", err)
	}
	return item(sqlcgen.ListItemsRow(r)), nil
}

// ListItems finds nothing for a member that is not a UUID. The cursor's ID was checked
// when it was decoded.
func (s *Store) ListItems(ctx context.Context, memberID string, box domain.Box, after *domain.Cursor, limit int) ([]domain.Item, error) {
	user, ok := parseUUID(memberID)
	if !ok {
		return nil, nil
	}
	arg := sqlcgen.ListItemsParams{UserID: user, RequestsBox: box == domain.BoxRequests, Lim: int32(min(limit, 1000))} //nolint:gosec // bounded above
	if after != nil {
		id, ok := parseUUID(after.ID)
		if !ok {
			return nil, domain.ErrInvalidCursor
		}
		arg.HasCursor = true
		arg.CursorAt = pgtype.Timestamptz{Time: after.LastMessageAt, Valid: true}
		arg.CursorID = id
	}
	rows, err := s.q(ctx).ListItems(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	items := make([]domain.Item, len(rows))
	for i, r := range rows {
		items[i] = item(r)
	}
	return items, nil
}

// ListMessages finds nothing for a conversation that is not a UUID.
func (s *Store) ListMessages(ctx context.Context, conversationID, before string, limit int) ([]domain.Message, error) {
	conv, ok := parseUUID(conversationID)
	if !ok {
		return nil, nil
	}
	arg := sqlcgen.ListMessagesParams{ConversationID: conv, Lim: int32(min(limit, 1000))} //nolint:gosec // bounded above
	if before != "" {
		id, ok := parseUUID(before)
		if !ok {
			return nil, domain.ErrInvalidCursor
		}
		arg.HasBefore, arg.BeforeID = true, id
	}
	rows, err := s.q(ctx).ListMessages(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	items := make([]domain.Message, len(rows))
	for i, r := range rows {
		items[i] = message(r.ID, r.ConversationID, r.SenderID, r.Body, r.ClientID, r.CreatedAt)
	}
	return items, nil
}

// MessageIn is false for IDs that are not UUIDs.
func (s *Store) MessageIn(ctx context.Context, conversationID, messageID string) (bool, error) {
	conv, ok1 := parseUUID(conversationID)
	id, ok2 := parseUUID(messageID)
	if !ok1 || !ok2 {
		return false, nil
	}
	v, err := s.q(ctx).MessageIn(ctx, sqlcgen.MessageInParams{ConversationID: conv, ID: id})
	if err != nil {
		return false, fmt.Errorf("find message: %w", err)
	}
	return v, nil
}

// MoveReadMarker moves nothing for IDs that are not UUIDs.
func (s *Store) MoveReadMarker(ctx context.Context, conversationID, memberID, messageID string) (bool, error) {
	conv, ok1 := parseUUID(conversationID)
	user, ok2 := parseUUID(memberID)
	id, ok3 := parseUUID(messageID)
	if !ok1 || !ok2 || !ok3 {
		return false, nil
	}
	n, err := s.q(ctx).MoveReadMarker(ctx, sqlcgen.MoveReadMarkerParams{MessageID: id, ConversationID: conv, UserID: user})
	if err != nil {
		return false, fmt.Errorf("move read marker: %w", err)
	}
	return n > 0, nil
}

// AnswerRequest finds nothing for IDs that are not UUIDs.
func (s *Store) AnswerRequest(ctx context.Context, id, receiverID string, state domain.State) (string, bool, error) {
	cid, ok1 := parseUUID(id)
	user, ok2 := parseUUID(receiverID)
	if !ok1 || !ok2 {
		return "", false, nil
	}
	requester, err := s.q(ctx).AnswerRequest(ctx, sqlcgen.AnswerRequestParams{State: string(state), ID: cid, ReceiverID: user})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("answer request: %w", err)
	}
	return requester.String(), true, nil
}

// CountUnread is zero for a member that is not a UUID.
func (s *Store) CountUnread(ctx context.Context, memberID string) (domain.UnreadCounts, error) {
	user, ok := parseUUID(memberID)
	if !ok {
		return domain.UnreadCounts{}, nil
	}
	q := s.q(ctx)
	total, err := q.CountUnreadMessages(ctx, user)
	if err != nil {
		return domain.UnreadCounts{}, fmt.Errorf("count unread messages: %w", err)
	}
	requests, err := q.CountPendingRequests(ctx, user)
	if err != nil {
		return domain.UnreadCounts{}, fmt.Errorf("count message requests: %w", err)
	}
	return domain.UnreadCounts{Total: int(total), Requests: int(requests)}, nil
}

// DeclineRequests does nothing for a value that is not a UUID.
func (s *Store) DeclineRequests(ctx context.Context, a, b string) error {
	ua, ok1 := parseUUID(a)
	ub, ok2 := parseUUID(b)
	if !ok1 || !ok2 {
		return nil
	}
	if err := s.q(ctx).DeclineRequestBetween(ctx, sqlcgen.DeclineRequestBetweenParams{A: ua, B: ub}); err != nil {
		return fmt.Errorf("decline request: %w", err)
	}
	return nil
}

func conversation(id pgtype.UUID, state string, requestedBy pgtype.UUID, last pgtype.Timestamptz) domain.Conversation {
	return domain.Conversation{ID: id.String(), State: domain.State(state), RequestedBy: uuidPtr(requestedBy), LastMessageAt: last.Time}
}

func message(id, conversationID, senderID pgtype.UUID, body string, clientID pgtype.UUID, createdAt pgtype.Timestamptz) domain.Message {
	return domain.Message{
		ID: id.String(), ConversationID: conversationID.String(), SenderID: senderID.String(),
		Body: body, ClientID: clientID.String(), CreatedAt: createdAt.Time,
	}
}

func item(r sqlcgen.ListItemsRow) domain.Item {
	it := domain.Item{
		Conversation: conversation(r.ID, r.State, r.RequestedBy, r.LastMessageAt),
		Other: domain.Person{
			UserID: r.OtherID.String(), FullName: r.FullName, Headline: textPtr(r.Headline),
			PhotoFileID: uuidPtr(r.PhotoFileID), Available: r.OtherAvailable,
		},
		UnreadCount: int(r.UnreadCount), BlockedByMe: r.BlockedByMe, BlockedMe: r.BlockedMe,
	}
	it.OtherID, it.OtherReadID = r.OtherID.String(), uuidPtr(r.OtherReadID)
	if r.LastSenderID.Valid {
		it.LastMessage = &domain.Preview{SenderID: r.LastSenderID.String(), Body: r.LastPreview, CreatedAt: timeOf(r.LastCreatedAt)}
	}
	return it
}

func timeOf(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}

func parseUUID(s string) (pgtype.UUID, bool) {
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		return id, false
	}
	return id, true
}

func uuidPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := u.String()
	return &s
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}
