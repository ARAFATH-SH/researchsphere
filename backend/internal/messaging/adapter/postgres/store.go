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

// UsersVisible leaves out IDs that are not UUIDs.
func (s *Store) UsersVisible(ctx context.Context, ids []string) ([]string, error) {
	rows, err := s.q(ctx).UsersVisible(ctx, parseUUIDs(ids))
	if err != nil {
		return nil, fmt.Errorf("check users: %w", err)
	}
	return uuidStrings(rows), nil
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
	m := message(sqlcgen.Message{
		ID: r.ID, ConversationID: r.ConversationID, SenderID: r.SenderID, Body: r.Body, ClientID: r.ClientID,
		CreatedAt: r.CreatedAt, EditedAt: r.EditedAt, DeletedAt: r.DeletedAt, Kind: r.Kind, Event: r.Event, EventData: r.EventData,
		HasAttachments: r.HasAttachments,
	})
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
	if m.Body == nil && !m.HasAttachments {
		return domain.Message{}, false, errors.New("add message: no body and no attachments")
	}
	q := s.q(ctx)
	arg := sqlcgen.InsertMessageParams{ConversationID: conv, SenderID: sender, ClientID: client, HasAttachments: m.HasAttachments}
	if m.Body != nil {
		arg.Body = pgtype.Text{String: *m.Body, Valid: true}
	}
	r, err := q.InsertMessage(ctx, arg)
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
	return message(r), true, nil
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
	c.Kind, c.Name, c.JoinedAt = domain.Kind(r.Kind), r.Name, r.MyJoinedAt.Time
	c.OtherID, c.OtherReadID = r.OtherID.String(), uuidPtr(r.OtherReadID)
	if c.IsGroup() {
		c.MyRole = domain.Role(r.MyRole)
	}
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
	it := item(sqlcgen.ListItemsRow(r))
	if err := s.fillAvatars(ctx, memberID, []*domain.Item{&it}); err != nil {
		return domain.Item{}, err
	}
	return it, nil
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
	ptrs := make([]*domain.Item, len(rows))
	for i, r := range rows {
		items[i] = item(r)
		ptrs[i] = &items[i]
	}
	if err := s.fillAvatars(ctx, memberID, ptrs); err != nil {
		return nil, err
	}
	return items, nil
}

// ListMessages finds nothing for a conversation that is not a UUID.
func (s *Store) ListMessages(ctx context.Context, conversationID string, since time.Time, before string, limit int) ([]domain.Message, error) {
	conv, ok := parseUUID(conversationID)
	if !ok {
		return nil, nil
	}
	arg := sqlcgen.ListMessagesParams{ConversationID: conv, Since: timestamptz(since), Lim: int32(min(limit, 1000))} //nolint:gosec // bounded above
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
	return messages(rows), nil
}

// FindOwnMessage returns domain.ErrNotFound for IDs that are not UUIDs.
func (s *Store) FindOwnMessage(ctx context.Context, conversationID, messageID, senderID string) (domain.Message, error) {
	return s.ownMessage(ctx, conversationID, messageID, senderID, false)
}

// LockOwnMessage holds the row until the transaction the context carries ends.
func (s *Store) LockOwnMessage(ctx context.Context, conversationID, messageID, senderID string) (domain.Message, error) {
	return s.ownMessage(ctx, conversationID, messageID, senderID, true)
}

func (s *Store) ownMessage(ctx context.Context, conversationID, messageID, senderID string, lock bool) (domain.Message, error) {
	conv, ok1 := parseUUID(conversationID)
	id, ok2 := parseUUID(messageID)
	sender, ok3 := parseUUID(senderID)
	if !ok1 || !ok2 || !ok3 {
		return domain.Message{}, domain.ErrNotFound
	}
	var (
		r   sqlcgen.Message
		err error
	)
	if lock {
		r, err = s.q(ctx).LockOwnMessage(ctx, sqlcgen.LockOwnMessageParams{ConversationID: conv, ID: id, SenderID: sender})
	} else {
		r, err = s.q(ctx).FindOwnMessage(ctx, sqlcgen.FindOwnMessageParams{ConversationID: conv, ID: id, SenderID: sender})
	}
	return stored(r, err, "find own message")
}

// EditMessage needs a transaction in the context only to run with the caller's locks; its
// two writes are one statement.
func (s *Store) EditMessage(ctx context.Context, messageID, body string) (domain.Message, error) {
	id, ok := parseUUID(messageID)
	if !ok {
		return domain.Message{}, domain.ErrNotFound
	}
	r, err := s.q(ctx).EditMessage(ctx, sqlcgen.EditMessageParams{ID: id, Body: body})
	return stored(r, err, "edit message")
}

// DeleteMessage keeps the text and leaves the tombstone in one statement.
func (s *Store) DeleteMessage(ctx context.Context, messageID string) (domain.Message, error) {
	id, ok := parseUUID(messageID)
	if !ok {
		return domain.Message{}, domain.ErrNotFound
	}
	r, err := s.q(ctx).DeleteMessage(ctx, id)
	return stored(r, err, "delete message")
}

// stored maps a row of messages, and no row to domain.ErrNotFound.
func stored(r sqlcgen.Message, err error, what string) (domain.Message, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Message{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Message{}, fmt.Errorf("%s: %w", what, err)
	}
	return message(r), nil
}

// MessageIn is false for IDs that are not UUIDs.
func (s *Store) MessageIn(ctx context.Context, conversationID, messageID string, since time.Time) (bool, error) {
	conv, ok1 := parseUUID(conversationID)
	id, ok2 := parseUUID(messageID)
	if !ok1 || !ok2 {
		return false, nil
	}
	v, err := s.q(ctx).MessageIn(ctx, sqlcgen.MessageInParams{ConversationID: conv, ID: id, Since: timestamptz(since)})
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

// message maps a row of messages. An invalid UUID (no sender, no client ID) maps to "".
func message(r sqlcgen.Message) domain.Message {
	return domain.Message{
		ID: r.ID.String(), ConversationID: r.ConversationID.String(), SenderID: r.SenderID.String(),
		Kind: domain.MessageKind(r.Kind), Event: systemEvent(r.Event, r.EventData),
		Body: textPtr(r.Body), ClientID: r.ClientID.String(), CreatedAt: r.CreatedAt.Time,
		EditedAt: timePtr(r.EditedAt), DeletedAt: timePtr(r.DeletedAt), HasAttachments: r.HasAttachments,
	}
}

func messages(rows []sqlcgen.Message) []domain.Message {
	out := make([]domain.Message, len(rows))
	for i, r := range rows {
		out[i] = message(r)
	}
	return out
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

func item(r sqlcgen.ListItemsRow) domain.Item {
	it := domain.Item{
		Conversation: conversation(r.ID, r.State, r.RequestedBy, r.LastMessageAt),
		UnreadCount:  int(r.UnreadCount), BlockedByMe: r.BlockedByMe, BlockedMe: r.BlockedMe,
	}
	it.Kind, it.Name, it.JoinedAt = domain.Kind(r.Kind), r.Name, r.MyJoinedAt.Time
	if it.IsGroup() {
		it.MyRole = domain.Role(r.MyRole)
		it.Group = &domain.GroupInfo{Name: r.Name, MemberCount: int(r.MemberCount), MyRole: it.MyRole, Avatars: []domain.Person{}}
	} else {
		it.Other = domain.Person{
			UserID: r.OtherID.String(), FullName: r.FullName, Headline: textPtr(r.Headline),
			PhotoFileID: uuidPtr(r.PhotoFileID), Available: r.OtherAvailable,
		}
		it.OtherID, it.OtherReadID = r.OtherID.String(), uuidPtr(r.OtherReadID)
	}
	if r.LastCreatedAt.Valid {
		// The query cannot say the preview is null; last_deleted and last_kind do.
		it.LastMessage = &domain.Preview{
			SenderID: r.LastSenderID.String(), Kind: domain.MessageKind(r.LastKind),
			Deleted: r.LastDeleted, CreatedAt: r.LastCreatedAt.Time, AttachmentCount: int(r.LastAttachmentCount),
		}
		switch {
		case r.LastEvent.Valid:
			ev := domain.EventType(r.LastEvent.String)
			it.LastMessage.Event = &ev
		case !r.LastDeleted && r.LastPreview != "": // a message with only attachments has no text
			body := r.LastPreview
			it.LastMessage.Body = &body
		}
	}
	return it
}

// ActivePeers finds nothing for a user who is not a UUID.
func (s *Store) ActivePeers(ctx context.Context, userID string) ([]string, error) {
	user, ok := parseUUID(userID)
	if !ok {
		return nil, nil
	}
	rows, err := s.q(ctx).ActivePeers(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("list active peers: %w", err)
	}
	return uuidStrings(rows), nil
}

// VisiblePeers leaves out IDs that are not UUIDs.
func (s *Store) VisiblePeers(ctx context.Context, viewerID string, ids []string) ([]string, error) {
	viewer, ok := parseUUID(viewerID)
	if !ok {
		return nil, nil
	}
	rows, err := s.q(ctx).VisiblePeers(ctx, sqlcgen.VisiblePeersParams{UserID: viewer, Ids: parseUUIDs(ids)})
	if err != nil {
		return nil, fmt.Errorf("list visible peers: %w", err)
	}
	return uuidStrings(rows), nil
}

// Partners finds nothing for a user who is not a UUID.
func (s *Store) Partners(ctx context.Context, userID string) ([]domain.Partner, error) {
	user, ok := parseUUID(userID)
	if !ok {
		return nil, nil
	}
	rows, err := s.q(ctx).ListPartners(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("list partners: %w", err)
	}
	out := make([]domain.Partner, len(rows))
	for i, r := range rows {
		out[i] = domain.Partner{ConversationID: r.ConversationID.String(), UserID: r.UserID.String()}
	}
	return out, nil
}

// DirectID finds nothing for IDs that are not UUIDs.
func (s *Store) DirectID(ctx context.Context, a, b string) (string, bool, error) {
	ua, ok1 := parseUUID(a)
	ub, ok2 := parseUUID(b)
	if !ok1 || !ok2 {
		return "", false, nil
	}
	id, err := s.q(ctx).DirectID(ctx, sqlcgen.DirectIDParams{A: ua, B: ub})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find direct conversation: %w", err)
	}
	return id.String(), true, nil
}

func uuidStrings(ids []pgtype.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// parseUUIDs leaves out the values that are not UUIDs.
func parseUUIDs(ids []string) []pgtype.UUID {
	out := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		if u, ok := parseUUID(id); ok {
			out = append(out, u)
		}
	}
	return out
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
