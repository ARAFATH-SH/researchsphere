package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// eventData is the JSON a system message keeps in messages.event_data. owner_changed is written
// by the database (migration 000021) with newOwnerId alone.
type eventData struct {
	UserIDs    []string `json:"userIds,omitempty"`
	OldName    *string  `json:"oldName,omitempty"`
	NewName    *string  `json:"newName,omitempty"`
	NewOwnerID string   `json:"newOwnerId,omitempty"`
}

// systemEvent reads a system message's event; nil for a text message. Data that does not
// parse keeps the type with no details, so one bad row cannot break a thread.
func systemEvent(event pgtype.Text, data []byte) *domain.SystemEvent {
	if !event.Valid {
		return nil
	}
	ev := domain.SystemEvent{Type: domain.EventType(event.String), UserIDs: []string{}}
	var d eventData
	if json.Unmarshal(data, &d) != nil {
		return &ev
	}
	if d.UserIDs != nil {
		ev.UserIDs = d.UserIDs
	}
	if d.NewOwnerID != "" {
		ev.UserIDs = []string{d.NewOwnerID}
	}
	ev.OldName, ev.NewName = d.OldName, d.NewName
	return &ev
}

// FindGroupByClient finds nothing for IDs that are not UUIDs.
func (s *Store) FindGroupByClient(ctx context.Context, creatorID, clientID string) (string, bool, error) {
	creator, ok1 := parseUUID(creatorID)
	client, ok2 := parseUUID(clientID)
	if !ok1 || !ok2 {
		return "", false, nil
	}
	id, err := s.q(ctx).FindGroupByClient(ctx, sqlcgen.FindGroupByClientParams{CreatedBy: creator, ClientID: client})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find group: %w", err)
	}
	return id.String(), true, nil
}

// CreateGroup needs a transaction in the context to keep its writes together.
func (s *Store) CreateGroup(ctx context.Context, g domain.NewGroup) (domain.Message, bool, error) {
	creator, ok1 := parseUUID(g.CreatorID)
	client, ok2 := parseUUID(g.ClientID)
	if !ok1 || !ok2 {
		return domain.Message{}, false, fmt.Errorf("create group: creator %q or client ID %q is not a UUID", g.CreatorID, g.ClientID)
	}
	q := s.q(ctx)
	id, err := q.InsertGroup(ctx, sqlcgen.InsertGroupParams{Name: pgtype.Text{String: g.Name, Valid: true}, CreatedBy: creator, ClientID: client})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Message{}, false, nil
	}
	if err != nil {
		return domain.Message{}, false, fmt.Errorf("create group: %w", err)
	}
	name := g.Name
	created, err := s.AddSystemMessage(ctx, id.String(), g.CreatorID,
		domain.SystemEvent{Type: domain.EventGroupCreated, UserIDs: g.MemberIDs, NewName: &name})
	if err != nil {
		return domain.Message{}, false, err
	}
	if err := q.AddGroupMember(ctx, sqlcgen.AddGroupMemberParams{
		ConversationID: id, UserID: creator, Role: string(domain.RoleOwner), JoinedAt: timestamptz(created.CreatedAt),
	}); err != nil {
		return domain.Message{}, false, fmt.Errorf("add group owner: %w", err)
	}
	if err := s.AddGroupMembers(ctx, id.String(), g.MemberIDs, created.CreatedAt); err != nil {
		return domain.Message{}, false, err
	}
	return created, true, nil
}

// AddSystemMessage stores the event as JSON and moves the conversation's last activity.
func (s *Store) AddSystemMessage(ctx context.Context, conversationID, actorID string, ev domain.SystemEvent) (domain.Message, error) {
	conv, ok := parseUUID(conversationID)
	if !ok {
		return domain.Message{}, fmt.Errorf("add system message: %w", domain.ErrNotFound)
	}
	data, err := json.Marshal(eventData{UserIDs: ev.UserIDs, OldName: ev.OldName, NewName: ev.NewName})
	if err != nil {
		return domain.Message{}, fmt.Errorf("add system message: %w", err)
	}
	arg := sqlcgen.InsertSystemMessageParams{ConversationID: conv, Event: string(ev.Type), EventData: data}
	if actorID != "" {
		actor, ok := parseUUID(actorID)
		if !ok {
			return domain.Message{}, fmt.Errorf("add system message: actor %q is not a UUID", actorID)
		}
		arg.SenderID = actor
	}
	q := s.q(ctx)
	r, err := q.InsertSystemMessage(ctx, arg)
	if err != nil {
		return domain.Message{}, fmt.Errorf("add system message: %w", err)
	}
	if err := q.TouchConversation(ctx, sqlcgen.TouchConversationParams{ID: conv, LastMessageAt: r.CreatedAt}); err != nil {
		return domain.Message{}, fmt.Errorf("update conversation: %w", err)
	}
	return message(r), nil
}

// AddGroupMembers is an error for IDs that are not UUIDs: the usecase checked each user.
func (s *Store) AddGroupMembers(ctx context.Context, conversationID string, userIDs []string, joinedAt time.Time) error {
	conv, ok := parseUUID(conversationID)
	if !ok {
		return fmt.Errorf("add group members: %w", domain.ErrNotFound)
	}
	q := s.q(ctx)
	for _, u := range userIDs {
		user, ok := parseUUID(u)
		if !ok {
			return fmt.Errorf("add group member: %q is not a UUID", u)
		}
		if err := q.AddGroupMember(ctx, sqlcgen.AddGroupMemberParams{
			ConversationID: conv, UserID: user, Role: string(domain.RoleMember), JoinedAt: timestamptz(joinedAt),
		}); err != nil {
			return fmt.Errorf("add group member: %w", err)
		}
	}
	return nil
}

// RemoveGroupMember removes nothing for IDs that are not UUIDs.
func (s *Store) RemoveGroupMember(ctx context.Context, conversationID, userID string) (bool, error) {
	conv, ok1 := parseUUID(conversationID)
	user, ok2 := parseUUID(userID)
	if !ok1 || !ok2 {
		return false, nil
	}
	n, err := s.q(ctx).RemoveGroupMember(ctx, sqlcgen.RemoveGroupMemberParams{ConversationID: conv, UserID: user})
	if err != nil {
		return false, fmt.Errorf("remove group member: %w", err)
	}
	return n > 0, nil
}

// RenameGroup renames nothing for an ID that is not a UUID.
func (s *Store) RenameGroup(ctx context.Context, conversationID, name string) error {
	conv, ok := parseUUID(conversationID)
	if !ok {
		return nil
	}
	if err := s.q(ctx).RenameGroup(ctx, sqlcgen.RenameGroupParams{ID: conv, Name: pgtype.Text{String: name, Valid: true}}); err != nil {
		return fmt.Errorf("rename group: %w", err)
	}
	return nil
}

// GroupMemberIDs finds nobody for an ID that is not a UUID.
func (s *Store) GroupMemberIDs(ctx context.Context, conversationID string) ([]string, error) {
	conv, ok := parseUUID(conversationID)
	if !ok {
		return nil, nil
	}
	rows, err := s.q(ctx).GroupMemberIDs(ctx, conv)
	if err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}
	return uuidStrings(rows), nil
}

// ListGroupMembers finds nobody for an ID that is not a UUID.
func (s *Store) ListGroupMembers(ctx context.Context, conversationID string) ([]domain.GroupMember, error) {
	conv, ok := parseUUID(conversationID)
	if !ok {
		return nil, nil
	}
	rows, err := s.q(ctx).ListGroupMembers(ctx, conv)
	if err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}
	out := make([]domain.GroupMember, len(rows))
	for i, r := range rows {
		out[i] = domain.GroupMember{
			UserID: r.UserID.String(), FullName: r.FullName, PhotoFileID: uuidPtr(r.PhotoFileID),
			Role: domain.Role(r.Role), JoinedAt: r.JoinedAt.Time, Available: r.Available,
		}
	}
	return out, nil
}

// GroupAvatars leaves out IDs that are not UUIDs.
func (s *Store) GroupAvatars(ctx context.Context, viewerID string, conversationIDs []string) (map[string][]domain.Person, error) {
	viewer, ok := parseUUID(viewerID)
	if !ok || len(conversationIDs) == 0 {
		return map[string][]domain.Person{}, nil
	}
	rows, err := s.q(ctx).GroupAvatars(ctx, sqlcgen.GroupAvatarsParams{ViewerID: viewer, ConversationIds: parseUUIDs(conversationIDs)})
	if err != nil {
		return nil, fmt.Errorf("list group avatars: %w", err)
	}
	out := make(map[string][]domain.Person, len(conversationIDs))
	for _, r := range rows {
		id := r.ConversationID.String()
		out[id] = append(out[id], domain.Person{
			UserID: r.UserID.String(), FullName: r.FullName, Headline: textPtr(r.Headline),
			PhotoFileID: uuidPtr(r.PhotoFileID), Available: r.Available,
		})
	}
	return out, nil
}

// fillAvatars reads the avatars of the groups among items in one query.
func (s *Store) fillAvatars(ctx context.Context, viewerID string, items []*domain.Item) error {
	var ids []string
	for _, it := range items {
		if it.Group != nil {
			ids = append(ids, it.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	avatars, err := s.GroupAvatars(ctx, viewerID, ids)
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.Group != nil && avatars[it.ID] != nil {
			it.Group.Avatars = avatars[it.ID]
		}
	}
	return nil
}

// ListReaders finds nobody for IDs that are not UUIDs.
func (s *Store) ListReaders(ctx context.Context, conversationID, viewerID string) ([]domain.Reader, error) {
	conv, ok1 := parseUUID(conversationID)
	viewer, ok2 := parseUUID(viewerID)
	if !ok1 || !ok2 {
		return nil, nil
	}
	rows, err := s.q(ctx).ListReaders(ctx, sqlcgen.ListReadersParams{ConversationID: conv, ViewerID: viewer})
	if err != nil {
		return nil, fmt.Errorf("list read markers: %w", err)
	}
	out := make([]domain.Reader, len(rows))
	for i, r := range rows {
		out[i] = domain.Reader{UserID: r.UserID.String(), LastReadMessageID: uuidPtr(r.LastReadMessageID)}
	}
	return out, nil
}

// SystemMessagesAfter finds nothing for IDs that are not UUIDs.
func (s *Store) SystemMessagesAfter(ctx context.Context, conversationID, afterID string) ([]domain.Message, error) {
	conv, ok1 := parseUUID(conversationID)
	after, ok2 := parseUUID(afterID)
	if !ok1 || !ok2 {
		return nil, nil
	}
	rows, err := s.q(ctx).SystemMessagesAfter(ctx, sqlcgen.SystemMessagesAfterParams{ConversationID: conv, AfterID: after})
	if err != nil {
		return nil, fmt.Errorf("list system messages: %w", err)
	}
	return messages(rows), nil
}

// GroupNames finds nothing for a viewer that is not a UUID.
func (s *Store) GroupNames(ctx context.Context, viewerID string, ids []string) (map[string]string, error) {
	viewer, ok := parseUUID(viewerID)
	if !ok || len(ids) == 0 {
		return map[string]string{}, nil
	}
	rows, err := s.q(ctx).GroupNames(ctx, sqlcgen.GroupNamesParams{ViewerID: viewer, Ids: parseUUIDs(ids)})
	if err != nil {
		return nil, fmt.Errorf("group names: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.ID.String()] = r.Name
	}
	return out, nil
}
