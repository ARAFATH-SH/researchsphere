package usecase

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// memMembers is the MemberStore in memory, on the memStore's members, blocks and away maps.
// Every user in users exists; the others do not.
type memMembers struct {
	s        *memStore
	users    map[string]bool
	requests []*domain.JoinRequest
	next     int
	clock    time.Time
}

func newMemMembers(s *memStore) *memMembers {
	return &memMembers{s: s, users: map[string]bool{ada: true, bob: true, cy: true, dee: true},
		clock: time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)}
}

func (m *memMembers) tick() time.Time {
	m.clock = m.clock.Add(time.Second)
	return m.clock
}

func (m *memMembers) blockedEitherWay(a, b string) bool {
	return m.s.blocks[[2]string{a, b}] || m.s.blocks[[2]string{b, a}]
}

func (m *memMembers) Relation(_ context.Context, projectID, userID string) (domain.Relation, error) {
	p := m.s.projects[projectID]
	rel := domain.Relation{Exists: m.users[userID], Available: m.users[userID] && !m.s.away[userID]}
	if p != nil {
		rel.Blocked = m.blockedEitherWay(p.Owner.UserID, userID)
		rel.Role = m.s.members[projectID][userID]
	}
	for _, r := range m.requests {
		if r.ProjectID == projectID && r.UserID == userID && r.Status == domain.RequestPending {
			c := *r
			rel.Pending = &c
		}
	}
	return rel, nil
}

func (m *memMembers) CreateRequest(_ context.Context, n domain.NewJoinRequest) (domain.JoinRequest, error) {
	for _, r := range m.requests {
		if r.ProjectID == n.ProjectID && r.UserID == n.UserID && r.Status == domain.RequestPending {
			return domain.JoinRequest{}, domain.ErrRequestPending
		}
	}
	m.next++
	r := &domain.JoinRequest{
		ID: fmt.Sprintf("r%d", m.next), ProjectID: n.ProjectID, Kind: n.Kind, Status: domain.RequestPending,
		Message: n.Message, Role: n.Role, UserID: n.UserID, CreatedAt: m.tick(),
	}
	m.requests = append(m.requests, r)
	return *r, nil
}

func (m *memMembers) find(id string) *domain.JoinRequest {
	for _, r := range m.requests {
		if r.ID == id {
			return r
		}
	}
	return nil
}

func (m *memMembers) LockRequest(_ context.Context, projectID, id string, kind domain.RequestKind) (domain.JoinRequest, error) {
	r := m.find(id)
	if r == nil || r.ProjectID != projectID || r.Kind != kind {
		return domain.JoinRequest{}, domain.ErrRequestNotFound
	}
	return *r, nil
}

func (m *memMembers) DecideRequest(_ context.Context, id string, status domain.RequestStatus) error {
	r := m.find(id)
	if r.Status != domain.RequestPending {
		return fmt.Errorf("decided twice")
	}
	at := m.tick()
	r.Status, r.DecidedAt = status, &at
	return nil
}

func (m *memMembers) DeleteRequest(_ context.Context, id string) error {
	m.requests = slices.DeleteFunc(m.requests, func(r *domain.JoinRequest) bool { return r.ID == id })
	return nil
}

func (m *memMembers) AddMember(_ context.Context, projectID, userID string, role domain.Role) (domain.Member, error) {
	if m.s.members[projectID][userID] != domain.RoleNone {
		return domain.Member{}, domain.ErrAlreadyMember
	}
	m.s.members[projectID][userID] = role
	return domain.Member{Person: domain.Person{UserID: userID}, Role: role, JoinedAt: m.tick()}, nil
}

func (m *memMembers) Person(_ context.Context, userID string) (domain.Person, error) {
	if !m.users[userID] {
		return domain.Person{}, domain.ErrUserNotFound
	}
	return domain.Person{UserID: userID, FullName: strings.TrimPrefix(userID, "u-")}, nil
}

func (m *memMembers) Member(_ context.Context, projectID, userID string) (domain.Member, error) {
	role := m.s.members[projectID][userID]
	if role == domain.RoleNone {
		return domain.Member{}, domain.ErrMemberNotFound
	}
	return domain.Member{Person: domain.Person{UserID: userID}, Role: role}, nil
}

func (m *memMembers) SetRole(_ context.Context, projectID, userID string, role domain.Role) error {
	// The one-owner index, checked at every statement.
	if role == domain.RoleOwner {
		for u, r := range m.s.members[projectID] {
			if r == domain.RoleOwner && u != userID {
				return fmt.Errorf("two owners")
			}
		}
		m.s.projects[projectID].Owner = domain.Person{UserID: userID}
	}
	m.s.members[projectID][userID] = role
	return nil
}

func (m *memMembers) RemoveMember(_ context.Context, projectID, userID string) error {
	delete(m.s.members[projectID], userID)
	return nil
}

func (m *memMembers) Members(_ context.Context, projectID string, limit, offset int) ([]domain.Member, int, error) {
	var out []domain.Member
	for u, r := range m.s.members[projectID] {
		out = append(out, domain.Member{Person: domain.Person{UserID: u}, Role: r})
	}
	slices.SortFunc(out, func(a, b domain.Member) int {
		if (a.Role == domain.RoleOwner) != (b.Role == domain.RoleOwner) {
			if a.Role == domain.RoleOwner {
				return -1
			}
			return 1
		}
		return strings.Compare(a.UserID, b.UserID)
	})
	return page(out, limit, offset), len(out), nil
}

func page[T any](items []T, limit, offset int) []T {
	if offset >= len(items) {
		return []T{}
	}
	return items[offset:min(len(items), offset+limit)]
}

func (m *memMembers) ProjectRequests(_ context.Context, projectID string, kind domain.RequestKind, limit, offset int) ([]domain.RequestItem, int, error) {
	var out []domain.RequestItem
	for _, r := range m.requests {
		if r.ProjectID == projectID && r.Kind == kind && !m.s.away[r.UserID] {
			out = append(out, domain.RequestItem{JoinRequest: *r, User: domain.Person{UserID: r.UserID}})
		}
	}
	return page(out, limit, offset), len(out), nil
}

func (m *memMembers) mine(userID string, kind domain.RequestKind, pendingOnly bool) []domain.MyRequestItem {
	var out []domain.MyRequestItem
	for _, r := range m.requests {
		if r.UserID == userID && r.Kind == kind && (!pendingOnly || r.Status == domain.RequestPending) {
			out = append(out, domain.MyRequestItem{JoinRequest: *r, Project: domain.MembershipProject{ID: r.ProjectID}})
		}
	}
	return out
}

func (m *memMembers) MyRequests(_ context.Context, userID string, limit, offset int) ([]domain.MyRequestItem, int, error) {
	out := m.mine(userID, domain.KindRequest, false)
	return page(out, limit, offset), len(out), nil
}

func (m *memMembers) MyInvitations(_ context.Context, userID string, limit, offset int) ([]domain.MyRequestItem, int, error) {
	out := m.mine(userID, domain.KindInvitation, true)
	return page(out, limit, offset), len(out), nil
}

func (m *memMembers) WithdrawBetween(_ context.Context, a, b string) ([]domain.Withdrawn, error) {
	var gone []domain.Withdrawn
	m.requests = slices.DeleteFunc(m.requests, func(r *domain.JoinRequest) bool {
		owner := m.s.projects[r.ProjectID].Owner.UserID
		hit := r.Status == domain.RequestPending && (r.UserID == a && owner == b || r.UserID == b && owner == a)
		if hit {
			gone = append(gone, domain.Withdrawn{ProjectID: r.ProjectID, UserID: r.UserID})
		}
		return hit
	})
	return gone, nil
}

func (m *memMembers) VisibleTitles(_ context.Context, viewerID string, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		if p := m.s.projects[id]; p != nil && m.s.access(p, viewerID).Visible() {
			out[id] = p.Title
		}
	}
	return out, nil
}

// memNotifier records each call as one line; err makes Notify fail.
type memNotifier struct {
	calls []string
	err   error
}

func (n *memNotifier) Notify(_ context.Context, kind domain.NoticeKind, projectID, recipientID, actorID string) error {
	if n.err != nil {
		return n.err
	}
	n.calls = append(n.calls, fmt.Sprintf("%s %s to %s from %s", kind, projectID, recipientID, actorID))
	return nil
}

func (n *memNotifier) RequestHandled(_ context.Context, projectID, senderID string) error {
	n.calls = append(n.calls, fmt.Sprintf("request handled %s from %s", projectID, senderID))
	return nil
}

func (n *memNotifier) InvitationHandled(_ context.Context, projectID, inviteeID string) error {
	n.calls = append(n.calls, fmt.Sprintf("invitation handled %s to %s", projectID, inviteeID))
	return nil
}

// memWatcher records each call as one line.
type memWatcher struct{ calls []string }

func (w *memWatcher) ProjectChanged(_ context.Context, projectID string) {
	w.calls = append(w.calls, "project "+projectID)
}

func (w *memWatcher) MembersChanged(_ context.Context, projectID string, userIDs ...string) {
	w.calls = append(w.calls, "members "+projectID+" "+strings.Join(userIDs, ","))
}
