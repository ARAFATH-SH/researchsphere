package usecase

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// Limits on groups (FR-16.6, FR-16.10, docs/decisions.md).
const (
	// MaxGroupNameLength is the longest group name, in characters.
	MaxGroupNameLength = 100
	// GroupsPerDay is how many groups a user may create in a day.
	GroupsPerDay = 10
	// GroupMembersPerDay is how many members a user may add to groups in a day, at creation and
	// later together.
	GroupMembersPerDay = 100
)

// errGroupUnchanged ends a group change's transaction when there is nothing to change.
var errGroupUnchanged = errors.New("messaging: the group is unchanged")

// NormalizeGroupName trims a group name and checks it: 1 to MaxGroupNameLength characters,
// valid text on one line, with no control characters.
func NormalizeGroupName(name string) (string, error) {
	if !utf8.ValidString(name) {
		return "", domain.ErrInvalidGroupName
	}
	text := strings.TrimSpace(name)
	if n := utf8.RuneCountInString(text); n < 1 || n > MaxGroupNameLength {
		return "", domain.ErrInvalidGroupName
	}
	for _, r := range text {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return "", domain.ErrInvalidGroupName
		}
	}
	return text, nil
}

// memberIDs checks a list of users to add: 1 to MaxGroupMembers-1 UUIDs, not the caller.
// Repeats count once. The result is sorted, which is also the order the pair locks are taken
// in. field names the body field for *InvalidFieldError.
func memberIDs(callerID string, ids []string, field string) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !isUUID(id) || strings.EqualFold(id, callerID) {
			return nil, &domain.InvalidFieldError{Field: field}
		}
		out = append(out, strings.ToLower(id))
	}
	slices.Sort(out)
	out = slices.Compact(out)
	if len(out) < 1 || len(out) > domain.MaxGroupMembers-1 {
		return nil, &domain.InvalidFieldError{Field: field}
	}
	return out, nil
}

// CreateGroup creates a group owned by creatorID (FR-16.6) and returns it as the creator sees
// it. replayed is true when the client ID had been used and the group is the earlier one.
// Checks run in this order: ErrInvalidGroupName, *InvalidFieldError for the members, the client
// ID (ErrNotFound when the creator has left that group), *SuspendedError, *RateLimitedError for
// groups and then for added members, *IneligibleError. A messaging restriction does not stop
// it (FR-15.6). Inside the transaction the standing and the members are checked again under
// the pair locks a block takes, so a block that commits first is never passed.
func (s *Service) CreateGroup(ctx context.Context, creatorID string, in domain.GroupInput) (domain.Item, bool, error) {
	name, err := NormalizeGroupName(in.Name)
	if err != nil {
		return domain.Item{}, false, err
	}
	ids, err := memberIDs(creatorID, in.MemberIDs, "memberIds")
	if err != nil {
		return domain.Item{}, false, err
	}
	if it, found, err := s.replayGroup(ctx, creatorID, in.ClientID); found || err != nil {
		return it, found, err
	}
	if err := s.refuseSuspended(ctx, creatorID); err != nil {
		return domain.Item{}, false, err
	}
	if err := s.checkLimits(ctx, creatorID, s.groups, 1); err != nil {
		return domain.Item{}, false, err
	}
	if err := s.checkLimits(ctx, creatorID, s.members, len(ids)); err != nil {
		return domain.Item{}, false, err
	}
	if err := s.refuseIneligible(ctx, creatorID, ids); err != nil {
		return domain.Item{}, false, err
	}
	var created domain.Message
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.refuseSuspended(ctx, creatorID); err != nil {
			return err
		}
		if err := s.lockPairs(ctx, creatorID, ids); err != nil {
			return err
		}
		if err := s.refuseIneligible(ctx, creatorID, ids); err != nil {
			return err
		}
		var (
			ok  bool
			err error
		)
		created, ok, err = s.store.CreateGroup(ctx, domain.NewGroup{CreatorID: creatorID, Name: name, ClientID: in.ClientID, MemberIDs: ids})
		if err != nil {
			return err
		}
		if !ok {
			return errDuplicate
		}
		return s.notifier.MembersAdded(ctx, created.ConversationID, creatorID, ids)
	})
	if errors.Is(err, errDuplicate) {
		// A create of the same client ID won the race.
		it, found, err := s.replayGroup(ctx, creatorID, in.ClientID)
		if err == nil && !found {
			err = errors.New("create group: client ID refused by a group that is gone")
		}
		return it, found, err
	}
	if err != nil {
		return domain.Item{}, false, err
	}
	s.record(ctx, creatorID, s.groups, 1)
	s.record(ctx, creatorID, s.members, len(ids))
	everyone := append([]string{creatorID}, ids...)
	s.publisher.MessageSent(ctx, domain.Sent{Message: created, State: domain.StateActive}, everyone)
	s.publisher.MembersChanged(ctx, created.ConversationID,
		domain.MemberChange{Kind: domain.ChangeAdded, ActorID: creatorID, UserIDs: ids, Name: name}, everyone)
	it, err := s.Get(ctx, creatorID, created.ConversationID)
	return it, false, err
}

// replayGroup returns the group creatorID created with clientID, if any. found with
// ErrNotFound means the creator is no longer in it.
func (s *Service) replayGroup(ctx context.Context, creatorID, clientID string) (domain.Item, bool, error) {
	id, found, err := s.store.FindGroupByClient(ctx, creatorID, clientID)
	if err != nil || !found {
		return domain.Item{}, false, err
	}
	it, err := s.Get(ctx, creatorID, id)
	return it, true, err
}

// refuseIneligible returns *IneligibleError naming every user who is not a verified, not
// suspended connection of ownerID with no block either way. It never says why for any of them.
func (s *Service) refuseIneligible(ctx context.Context, ownerID string, ids []string) error {
	visible, err := s.store.UsersVisible(ctx, ids)
	if err != nil {
		return err
	}
	connected, err := s.connections.ConnectedAmong(ctx, ownerID, ids)
	if err != nil {
		return err
	}
	blocked, err := s.blocks.BlockedAmong(ctx, ownerID, ids)
	if err != nil {
		return err
	}
	ok := make(map[string]int, len(ids))
	for _, id := range visible {
		ok[strings.ToLower(id)]++
	}
	for _, id := range connected {
		ok[strings.ToLower(id)]++
	}
	for _, id := range blocked {
		ok[strings.ToLower(id)] = -10
	}
	var bad []string
	for _, id := range ids {
		if ok[id] != 2 {
			bad = append(bad, id)
		}
	}
	if len(bad) > 0 {
		return &domain.IneligibleError{UserIDs: bad}
	}
	return nil
}

// lockPairs takes the pair lock a block takes for ownerID with each user, in sorted order so
// two changes cannot wait on each other.
func (s *Service) lockPairs(ctx context.Context, ownerID string, ids []string) error {
	for _, id := range ids {
		if err := s.blocks.Lock(ctx, ownerID, id); err != nil {
			return err
		}
	}
	return nil
}

// ownedGroup returns the group when userID is its owner: ErrNotFound for a non-member or a
// direct conversation (NFR-14), ErrNotOwner for a member.
func (s *Service) ownedGroup(ctx context.Context, userID, id string) (domain.Conversation, error) {
	c, err := s.store.FindConversation(ctx, id, userID)
	if err != nil {
		return domain.Conversation{}, err
	}
	if !c.IsGroup() {
		return domain.Conversation{}, domain.ErrNotFound
	}
	if c.MyRole != domain.RoleOwner {
		return domain.Conversation{}, domain.ErrNotOwner
	}
	return c, nil
}

// lockOwned is ownedGroup under the conversation's lock, inside a transaction.
func (s *Service) lockOwned(ctx context.Context, userID, id string) (domain.Conversation, error) {
	c, err := s.lockMember(ctx, id, userID)
	if err != nil {
		return domain.Conversation{}, err
	}
	if c.MyRole != domain.RoleOwner {
		return domain.Conversation{}, domain.ErrNotOwner
	}
	return c, nil
}

// RenameGroup renames a group (FR-16.6). Checks run in this order: ErrInvalidGroupName,
// ErrNotFound, ErrNotOwner, *SuspendedError. The same name changes nothing and tells nobody.
func (s *Service) RenameGroup(ctx context.Context, ownerID, id, name string) (domain.Item, error) {
	text, err := NormalizeGroupName(name)
	if err != nil {
		return domain.Item{}, err
	}
	c, err := s.ownedGroup(ctx, ownerID, id)
	if err != nil {
		return domain.Item{}, err
	}
	if err := s.refuseSuspended(ctx, ownerID); err != nil {
		return domain.Item{}, err
	}
	if c.Name == text {
		return s.Get(ctx, ownerID, id)
	}
	var (
		event   domain.Message
		members []string
	)
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.refuseSuspended(ctx, ownerID); err != nil {
			return err
		}
		locked, err := s.lockOwned(ctx, ownerID, id)
		if err != nil {
			return err
		}
		if locked.Name == text {
			return errGroupUnchanged
		}
		if err := s.store.RenameGroup(ctx, id, text); err != nil {
			return err
		}
		old := locked.Name
		if event, err = s.store.AddSystemMessage(ctx, id, ownerID,
			domain.SystemEvent{Type: domain.EventGroupRenamed, UserIDs: []string{}, OldName: &old, NewName: &text}); err != nil {
			return err
		}
		members, err = s.store.GroupMemberIDs(ctx, id)
		return err
	})
	if err != nil && !errors.Is(err, errGroupUnchanged) {
		return domain.Item{}, err
	}
	if err == nil {
		s.publisher.MessageSent(ctx, domain.Sent{Message: event, State: domain.StateActive}, members)
		s.publisher.MembersChanged(ctx, id, domain.MemberChange{Kind: domain.ChangeRenamed, ActorID: ownerID, UserIDs: []string{}, Name: text}, members)
	}
	return s.Get(ctx, ownerID, id)
}

// AddMembers adds users to a group (FR-16.6). Checks run in this order: *InvalidFieldError,
// ErrNotFound, ErrNotOwner, *SuspendedError, then, for those not already members,
// *RateLimitedError, *IneligibleError and ErrGroupFull. When all of them are members already,
// nothing changes and no quota is used. A new member sees messages from the member_added
// message on.
func (s *Service) AddMembers(ctx context.Context, ownerID, id string, userIDs []string) (domain.Item, error) {
	ids, err := memberIDs(ownerID, userIDs, "userIds")
	if err != nil {
		return domain.Item{}, err
	}
	if _, err := s.ownedGroup(ctx, ownerID, id); err != nil {
		return domain.Item{}, err
	}
	if err := s.refuseSuspended(ctx, ownerID); err != nil {
		return domain.Item{}, err
	}
	current, err := s.store.GroupMemberIDs(ctx, id)
	if err != nil {
		return domain.Item{}, err
	}
	if fresh := without(ids, current); len(fresh) > 0 {
		if err := s.checkLimits(ctx, ownerID, s.members, len(fresh)); err != nil {
			return domain.Item{}, err
		}
		if err := s.refuseIneligible(ctx, ownerID, fresh); err != nil {
			return domain.Item{}, err
		}
		if len(current)+len(fresh) > domain.MaxGroupMembers {
			return domain.Item{}, domain.ErrGroupFull
		}
	}
	var (
		event   domain.Message
		added   []string
		members []string
		name    string
	)
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.refuseSuspended(ctx, ownerID); err != nil {
			return err
		}
		locked, err := s.lockOwned(ctx, ownerID, id)
		if err != nil {
			return err
		}
		name = locked.Name
		current, err := s.store.GroupMemberIDs(ctx, id)
		if err != nil {
			return err
		}
		if added = without(ids, current); len(added) == 0 {
			return errGroupUnchanged
		}
		if err := s.lockPairs(ctx, ownerID, added); err != nil {
			return err
		}
		if err := s.refuseIneligible(ctx, ownerID, added); err != nil {
			return err
		}
		if len(current)+len(added) > domain.MaxGroupMembers {
			return domain.ErrGroupFull
		}
		if event, err = s.store.AddSystemMessage(ctx, id, ownerID, domain.SystemEvent{Type: domain.EventMemberAdded, UserIDs: added}); err != nil {
			return err
		}
		if err := s.store.AddGroupMembers(ctx, id, added, event.CreatedAt); err != nil {
			return err
		}
		if err := s.notifier.MembersAdded(ctx, id, ownerID, added); err != nil {
			return err
		}
		members = append(current, added...)
		return nil
	})
	if err != nil && !errors.Is(err, errGroupUnchanged) {
		return domain.Item{}, err
	}
	if err == nil {
		s.record(ctx, ownerID, s.members, len(added))
		s.publisher.MessageSent(ctx, domain.Sent{Message: event, State: domain.StateActive}, members)
		s.publisher.MembersChanged(ctx, id, domain.MemberChange{Kind: domain.ChangeAdded, ActorID: ownerID, UserIDs: added, Name: name}, members)
	}
	return s.Get(ctx, ownerID, id)
}

// RemoveMember removes userID from a group (FR-16.6). Checks run in this order: ErrNotFound,
// ErrNotOwner, ErrRemoveSelf, *SuspendedError, ErrNotFound when userID is not a member. The
// removed member loses all access at once (NFR-14).
func (s *Service) RemoveMember(ctx context.Context, ownerID, id, userID string) error {
	if _, err := s.ownedGroup(ctx, ownerID, id); err != nil {
		return err
	}
	if strings.EqualFold(userID, ownerID) {
		return domain.ErrRemoveSelf
	}
	if err := s.refuseSuspended(ctx, ownerID); err != nil {
		return err
	}
	var gone groupExit
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.refuseSuspended(ctx, ownerID); err != nil {
			return err
		}
		locked, err := s.lockOwned(ctx, ownerID, id)
		if err != nil {
			return err
		}
		if _, err := s.store.FindConversation(ctx, id, userID); err != nil {
			return err
		}
		gone, err = s.exit(ctx, locked, ownerID, userID, domain.SystemEvent{Type: domain.EventMemberRemoved, UserIDs: []string{userID}})
		return err
	})
	if err != nil {
		return err
	}
	s.publishExit(ctx, gone, domain.MemberChange{Kind: domain.ChangeRemoved, ActorID: ownerID, UserIDs: []string{userID}, Name: gone.name})
	return nil
}

// Leave ends userID's membership of a group (FR-16.6), also while suspended or restricted.
// ErrNotFound for a non-member or a direct conversation. When the owner leaves, the member who
// joined earliest becomes owner; when the last member leaves, the group is closed.
func (s *Service) Leave(ctx context.Context, userID, id string) error {
	c, err := s.store.FindConversation(ctx, id, userID)
	if err != nil {
		return err
	}
	if !c.IsGroup() {
		return domain.ErrNotFound
	}
	var gone groupExit
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		locked, err := s.lockMember(ctx, id, userID)
		if err != nil {
			return err
		}
		gone, err = s.exit(ctx, locked, userID, userID, domain.SystemEvent{Type: domain.EventMemberLeft, UserIDs: []string{}})
		return err
	})
	if err != nil {
		return err
	}
	s.publishExit(ctx, gone, domain.MemberChange{Kind: domain.ChangeLeft, ActorID: userID, UserIDs: []string{userID}, Name: gone.name})
	return nil
}

// groupExit is what a leave or a removal wrote, for the events after the commit.
type groupExit struct {
	conversationID string
	name           string
	userID         string           // who is gone
	messages       []domain.Message // the exit's message, then any the database wrote after it
	members        []string         // the members left
}

// exit writes the exit's system message, then ends userID's membership. The database hands
// ownership over, with its own owner_changed message, or closes the group (migration 000021);
// this reads what it wrote. The user's notification of being added is taken back.
func (s *Service) exit(ctx context.Context, c domain.Conversation, actorID, userID string, ev domain.SystemEvent) (groupExit, error) {
	event, err := s.store.AddSystemMessage(ctx, c.ID, actorID, ev)
	if err != nil {
		return groupExit{}, err
	}
	removed, err := s.store.RemoveGroupMember(ctx, c.ID, userID)
	if err != nil {
		return groupExit{}, err
	}
	if !removed {
		return groupExit{}, domain.ErrNotFound
	}
	if err := s.notifier.AccessLost(ctx, c.ID, userID); err != nil {
		return groupExit{}, err
	}
	later, err := s.store.SystemMessagesAfter(ctx, c.ID, event.ID)
	if err != nil {
		return groupExit{}, err
	}
	members, err := s.store.GroupMemberIDs(ctx, c.ID)
	if err != nil {
		return groupExit{}, err
	}
	return groupExit{conversationID: c.ID, name: c.Name, userID: userID, messages: append([]domain.Message{event}, later...), members: members}, nil
}

// publishExit tells the members left about the exit and any hand-over, and the user who is gone
// that the group is gone for them.
func (s *Service) publishExit(ctx context.Context, g groupExit, change domain.MemberChange) {
	for _, m := range g.messages {
		s.publisher.MessageSent(ctx, domain.Sent{Message: m, State: domain.StateActive}, g.members)
	}
	s.publisher.MembersChanged(ctx, g.conversationID, change, g.members)
	for _, m := range g.messages {
		if m.Event != nil && m.Event.Type == domain.EventOwnerChanged {
			s.publisher.MembersChanged(ctx, g.conversationID,
				domain.MemberChange{Kind: domain.ChangeOwnerChanged, UserIDs: m.Event.UserIDs, Name: g.name}, g.members)
		}
	}
	s.publisher.AccessLost(ctx, g.conversationID, []string{g.userID})
}
