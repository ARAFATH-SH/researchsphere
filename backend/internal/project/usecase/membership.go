package usecase

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Membership limits (decisions.md 2026-11-17).
const (
	// MaxJoinMessage is the length of a join request's note (FR-11.1), after trimming.
	MaxJoinMessage = 500
	// JoinRequestsPerWindow join requests a user may send in MembershipWindow, and
	// InvitationsPerWindow invitations an owner may send; only stored ones count.
	JoinRequestsPerWindow = 20
	InvitationsPerWindow  = 20
	MembershipWindow      = 24 * time.Hour
)

// Every membership write locks the project row first (Store.Lock), so the changes to one
// project's membership happen one after the other, and an account deletion that locks the
// projects its user owns waits for them or makes them wait (FR-2.9).

// noNotifier is the Notifier of a Service made without one.
type noNotifier struct{}

func (noNotifier) Notify(context.Context, domain.NoticeKind, string, string, string) error {
	return nil
}
func (noNotifier) RequestHandled(context.Context, string, string) error    { return nil }
func (noNotifier) InvitationHandled(context.Context, string, string) error { return nil }

// noLimit is the Limiter of a Service made without one: nothing is counted.
type noLimit struct{}

func (noLimit) Used(context.Context, string) (int, time.Duration, error) { return 0, 0, nil }
func (noLimit) Record(context.Context, string, time.Duration) error      { return nil }

// refuseSuspended returns a *domain.SuspendedError when userID is suspended now.
func (s *Service) refuseSuspended(ctx context.Context, userID string) error {
	st, err := s.d.Actors.Standing(ctx, userID)
	if err != nil {
		return err
	}
	if st.Suspended {
		return &domain.SuspendedError{Reason: st.SuspensionReason, Until: st.SuspendedUntil}
	}
	return nil
}

// underLimit returns a *domain.RateLimitedError when userID used up perWindow. A limit that
// cannot be read lets the action through.
func (s *Service) underLimit(ctx context.Context, l domain.Limiter, userID string, perWindow int, what string) error {
	used, left, err := l.Used(ctx, userID)
	switch {
	case err != nil:
		s.d.Logger.WarnContext(ctx, what+" limit unavailable, allowing it", slog.Any("error", err))
	case used >= perWindow:
		return &domain.RateLimitedError{RetryAfter: left}
	}
	return nil
}

func (s *Service) record(ctx context.Context, l domain.Limiter, userID, what string) {
	if err := l.Record(ctx, userID, MembershipWindow); err != nil {
		s.d.Logger.WarnContext(ctx, what+" limit not recorded", slog.Any("error", err))
	}
}

// managed locks the project and checks that ownerID may manage its members: the project is
// visible (domain.ErrNotFound), the role allows action (domain.ErrForbidden) and the account
// is not suspended (*domain.SuspendedError).
func (s *Service) managed(ctx context.Context, ownerID, projectID string, action domain.Action) (domain.Access, error) {
	a, err := s.d.Store.Lock(ctx, projectID, ownerID)
	if err != nil {
		return a, err
	}
	if !a.Visible() {
		return a, domain.ErrNotFound
	}
	if !s.d.Permissions.Can(a.Role, action) {
		return a, domain.ErrForbidden
	}
	return a, s.refuseSuspended(ctx, ownerID)
}

// RequestToJoin stores userID's request to join the project with an optional note
// (FR-11.1). Checks, in this order and in one transaction that locks the project: the
// project is visible and there is no block either way with the owner (domain.ErrNotFound),
// the account is not suspended (*domain.SuspendedError), the message (*domain.FieldError),
// the caller is not a member (domain.ErrAlreadyMember), the project is open
// (domain.ErrProjectNotOpen), there is no pending request (domain.ErrRequestPending) or
// invitation (domain.ErrInvitationPending), and the daily limit (*domain.RateLimitedError).
// The owner is notified.
func (s *Service) RequestToJoin(ctx context.Context, userID, projectID string, message *string) (domain.MyRequestItem, error) {
	var out domain.JoinRequest
	err := s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		a, err := s.d.Store.Lock(ctx, projectID, userID)
		if err != nil {
			return err
		}
		if !a.Visible() {
			return domain.ErrNotFound
		}
		rel, err := s.d.Members.Relation(ctx, projectID, userID)
		if err != nil {
			return err
		}
		if rel.Blocked {
			return domain.ErrNotFound
		}
		if err := s.refuseSuspended(ctx, userID); err != nil {
			return err
		}
		msg, err := optionalText(message, MaxJoinMessage, "message")
		if err != nil {
			return err
		}
		if !s.d.Permissions.Can(a.Role, domain.ActRequestToJoin) {
			return domain.ErrAlreadyMember
		}
		if a.Status != domain.StatusOpen {
			return domain.ErrProjectNotOpen
		}
		if rel.Pending != nil {
			if rel.Pending.Kind == domain.KindInvitation {
				return domain.ErrInvitationPending
			}
			return domain.ErrRequestPending
		}
		if err := s.underLimit(ctx, s.d.JoinLimiter, userID, JoinRequestsPerWindow, "join request"); err != nil {
			return err
		}
		out, err = s.d.Members.CreateRequest(ctx, domain.NewJoinRequest{ProjectID: projectID, UserID: userID, Kind: domain.KindRequest, Message: msg})
		if err != nil {
			return err
		}
		if err := s.d.Notifier.Notify(ctx, domain.NoticeJoinRequest, projectID, a.OwnerID, userID); err != nil {
			return err
		}
		s.d.Watcher.MembersChanged(ctx, projectID, userID)
		return nil
	})
	if err != nil {
		return domain.MyRequestItem{}, err
	}
	s.record(ctx, s.d.JoinLimiter, userID, "join request")
	p, err := s.d.Store.Get(ctx, projectID, userID)
	if err != nil {
		return domain.MyRequestItem{}, err
	}
	return domain.MyRequestItem{JoinRequest: out, Project: domain.MembershipProject{ID: p.ID, Title: p.Title, Status: p.Status, Owner: p.Owner}}, nil
}

// CancelRequest deletes userID's pending request to join the project, also while suspended.
// Anything else (another user's request, a decided one, an unknown ID) is
// domain.ErrRequestNotFound. The owner's notification of it is taken back.
func (s *Service) CancelRequest(ctx context.Context, userID, projectID, requestID string) error {
	return s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		if _, err := s.d.Store.Lock(ctx, projectID, userID); err != nil {
			return requestNotFound(err)
		}
		r, err := s.d.Members.LockRequest(ctx, projectID, requestID, domain.KindRequest)
		if err != nil {
			return err
		}
		if r.UserID != userID || r.Status != domain.RequestPending {
			return domain.ErrRequestNotFound
		}
		if err := s.d.Members.DeleteRequest(ctx, r.ID); err != nil {
			return err
		}
		if err := s.d.Notifier.RequestHandled(ctx, projectID, userID); err != nil {
			return err
		}
		s.d.Watcher.MembersChanged(ctx, projectID, userID)
		return nil
	})
}

// requestNotFound turns a missing project into a missing request.
func requestNotFound(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrRequestNotFound
	}
	return err
}

// DecideRequest accepts (with role editor or viewer) or rejects a pending join request
// (FR-11.2). Checks, in this order: the role (*domain.FieldError), then in one transaction
// that locks the project: visible (domain.ErrNotFound), the owner's action (domain.ErrForbidden),
// not suspended (*domain.SuspendedError), the request exists in the project
// (domain.ErrRequestNotFound), is pending (domain.ErrRequestNotPending) and, to accept, its
// sender is verified, not suspended and not blocked either way with the owner
// (domain.ErrRequestNotFound). Accepting adds the member in the same transaction. The sender
// is notified and the owner's notification of the request is taken back.
func (s *Service) DecideRequest(ctx context.Context, ownerID, projectID, requestID string, accept bool, role domain.Role) (domain.Member, error) {
	if accept && !domain.MemberRoleValid(role) {
		return domain.Member{}, &domain.FieldError{Field: "role"}
	}
	var out domain.Member
	err := s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		if _, err := s.managed(ctx, ownerID, projectID, domain.ActManageMembers); err != nil {
			return err
		}
		r, err := s.d.Members.LockRequest(ctx, projectID, requestID, domain.KindRequest)
		if err != nil {
			return err
		}
		if r.Status != domain.RequestPending {
			return domain.ErrRequestNotPending
		}
		notice, status := domain.NoticeRequestRejected, domain.RequestRejected
		if accept {
			rel, err := s.d.Members.Relation(ctx, projectID, r.UserID)
			if err != nil {
				return err
			}
			if !rel.Available || rel.Blocked {
				return domain.ErrRequestNotFound
			}
			if out, err = s.d.Members.AddMember(ctx, projectID, r.UserID, role); err != nil {
				return err
			}
			notice, status = domain.NoticeRequestAccepted, domain.RequestAccepted
		}
		if err := s.d.Members.DecideRequest(ctx, r.ID, status); err != nil {
			return err
		}
		if err := s.d.Notifier.RequestHandled(ctx, projectID, r.UserID); err != nil {
			return err
		}
		if err := s.d.Notifier.Notify(ctx, notice, projectID, r.UserID, ownerID); err != nil {
			return err
		}
		s.d.Watcher.MembersChanged(ctx, projectID, r.UserID, ownerID)
		return nil
	})
	return out, err
}

// InviteInput is an invitation as the owner sends it.
type InviteInput struct {
	UserID string
	Role   domain.Role
}

// Invite invites a user to the project with a role (FR-11.3). Checks, in this order: the
// role (*domain.FieldError), then in one transaction that locks the project: visible
// (domain.ErrNotFound), the owner's action (domain.ErrForbidden), not suspended
// (*domain.SuspendedError), the project is open (domain.ErrProjectNotOpen), the user exists,
// is verified, not suspended and not blocked either way with the owner
// (domain.ErrUserNotFound), is not a member (domain.ErrAlreadyMember), is not invited
// (domain.ErrAlreadyInvited) and has no pending request (domain.ErrRequestPending), and the
// owner's daily limit (*domain.RateLimitedError). The user is notified.
func (s *Service) Invite(ctx context.Context, ownerID, projectID string, in InviteInput) (domain.RequestItem, error) {
	if !domain.MemberRoleValid(in.Role) {
		return domain.RequestItem{}, &domain.FieldError{Field: "role"}
	}
	var out domain.JoinRequest
	err := s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		a, err := s.managed(ctx, ownerID, projectID, domain.ActManageMembers)
		if err != nil {
			return err
		}
		if a.Status != domain.StatusOpen {
			return domain.ErrProjectNotOpen
		}
		rel, err := s.d.Members.Relation(ctx, projectID, in.UserID)
		if err != nil {
			return err
		}
		switch {
		case in.UserID == ownerID || rel.Role != domain.RoleNone:
			return domain.ErrAlreadyMember
		case !rel.Exists || !rel.Available || rel.Blocked:
			return domain.ErrUserNotFound
		case rel.Pending != nil && rel.Pending.Kind == domain.KindInvitation:
			return domain.ErrAlreadyInvited
		case rel.Pending != nil:
			return domain.ErrRequestPending
		}
		if err := s.underLimit(ctx, s.d.InviteLimiter, ownerID, InvitationsPerWindow, "invitation"); err != nil {
			return err
		}
		out, err = s.d.Members.CreateRequest(ctx, domain.NewJoinRequest{ProjectID: projectID, UserID: in.UserID, Kind: domain.KindInvitation, Role: in.Role})
		if err != nil {
			return err
		}
		if err := s.d.Notifier.Notify(ctx, domain.NoticeInvitation, projectID, in.UserID, ownerID); err != nil {
			return err
		}
		s.d.Watcher.MembersChanged(ctx, projectID, in.UserID, ownerID)
		return nil
	})
	if err != nil {
		return domain.RequestItem{}, err
	}
	s.record(ctx, s.d.InviteLimiter, ownerID, "invitation")
	who, err := s.d.Members.Person(ctx, in.UserID)
	if err != nil {
		return domain.RequestItem{}, err
	}
	return domain.RequestItem{JoinRequest: out, User: who}, nil
}

// CancelInvitation deletes a pending invitation (FR-11.3). Checks, in one transaction that
// locks the project: visible (domain.ErrNotFound), the owner's action (domain.ErrForbidden),
// not suspended (*domain.SuspendedError), the invitation exists in the project
// (domain.ErrRequestNotFound) and is pending (domain.ErrInvitationNotPending). The invitee's
// notification of it is taken back.
func (s *Service) CancelInvitation(ctx context.Context, ownerID, projectID, invitationID string) error {
	return s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		if _, err := s.managed(ctx, ownerID, projectID, domain.ActManageMembers); err != nil {
			return err
		}
		r, err := s.d.Members.LockRequest(ctx, projectID, invitationID, domain.KindInvitation)
		if err != nil {
			return err
		}
		if r.Status != domain.RequestPending {
			return domain.ErrInvitationNotPending
		}
		if err := s.d.Members.DeleteRequest(ctx, r.ID); err != nil {
			return err
		}
		if err := s.d.Notifier.InvitationHandled(ctx, projectID, r.UserID); err != nil {
			return err
		}
		s.d.Watcher.MembersChanged(ctx, projectID, r.UserID, ownerID)
		return nil
	})
}

// AnswerInvitation accepts or rejects userID's invitation to the project (FR-11.3). Checks,
// in this order and in one transaction that locks the project: the invitation exists in the
// project and is userID's (domain.ErrRequestNotFound), is pending
// (domain.ErrInvitationNotPending); to accept, the account is not suspended
// (*domain.SuspendedError) and the owner is verified, not suspended and not blocked either
// way (domain.ErrRequestNotFound). Rejecting is allowed while suspended. The project's status
// does not matter. The owner is notified and userID's notification of the invitation is
// taken back.
func (s *Service) AnswerInvitation(ctx context.Context, userID, projectID, invitationID string, accept bool) (domain.Member, error) {
	var out domain.Member
	err := s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		a, err := s.d.Store.Lock(ctx, projectID, userID)
		if err != nil {
			return requestNotFound(err)
		}
		r, err := s.d.Members.LockRequest(ctx, projectID, invitationID, domain.KindInvitation)
		if err != nil {
			return err
		}
		if r.UserID != userID {
			return domain.ErrRequestNotFound
		}
		if r.Status != domain.RequestPending {
			return domain.ErrInvitationNotPending
		}
		notice, status := domain.NoticeInvitationRejected, domain.RequestRejected
		if accept {
			if err := s.refuseSuspended(ctx, userID); err != nil {
				return err
			}
			rel, err := s.d.Members.Relation(ctx, projectID, userID)
			if err != nil {
				return err
			}
			if !a.OwnerAvailable || rel.Blocked {
				return domain.ErrRequestNotFound
			}
			if out, err = s.d.Members.AddMember(ctx, projectID, userID, r.Role); err != nil {
				return err
			}
			notice, status = domain.NoticeInvitationAccepted, domain.RequestAccepted
		}
		if err := s.d.Members.DecideRequest(ctx, r.ID, status); err != nil {
			return err
		}
		if err := s.d.Notifier.InvitationHandled(ctx, projectID, userID); err != nil {
			return err
		}
		if err := s.d.Notifier.Notify(ctx, notice, projectID, a.OwnerID, userID); err != nil {
			return err
		}
		s.d.Watcher.MembersChanged(ctx, projectID, userID, a.OwnerID)
		return nil
	})
	return out, err
}

// ChangeRole gives a member the role editor or viewer (FR-11.4). Checks, in this order: the
// role (*domain.FieldError), then in one transaction that locks the project: visible
// (domain.ErrNotFound), the owner's action (domain.ErrForbidden), not suspended
// (*domain.SuspendedError), the user is a member (domain.ErrMemberNotFound) and not the owner
// (domain.ErrUseTransfer). The member's own role again changes nothing; otherwise they are
// notified.
func (s *Service) ChangeRole(ctx context.Context, ownerID, projectID, memberID string, role domain.Role) (domain.Member, error) {
	if !domain.MemberRoleValid(role) {
		return domain.Member{}, &domain.FieldError{Field: "role"}
	}
	var out domain.Member
	err := s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		if _, err := s.managed(ctx, ownerID, projectID, domain.ActManageMembers); err != nil {
			return err
		}
		m, err := s.d.Members.Member(ctx, projectID, memberID)
		if err != nil {
			return err
		}
		if m.Role == domain.RoleOwner {
			return domain.ErrUseTransfer
		}
		out = m
		if m.Role == role {
			return nil
		}
		if err := s.d.Members.SetRole(ctx, projectID, memberID, role); err != nil {
			return err
		}
		out.Role = role
		if err := s.d.Notifier.Notify(ctx, domain.NoticeRoleChanged, projectID, memberID, ownerID); err != nil {
			return err
		}
		s.d.Watcher.MembersChanged(ctx, projectID, memberID, ownerID)
		return nil
	})
	return out, err
}

// RemoveMember removes a member (FR-11.4). Checks, in one transaction that locks the project:
// visible (domain.ErrNotFound), the owner's action (domain.ErrForbidden), not suspended
// (*domain.SuspendedError), the user is a member (domain.ErrMemberNotFound) and not the owner
// (domain.ErrCannotRemoveOwner). The member is notified.
func (s *Service) RemoveMember(ctx context.Context, ownerID, projectID, memberID string) error {
	return s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		if _, err := s.managed(ctx, ownerID, projectID, domain.ActManageMembers); err != nil {
			return err
		}
		m, err := s.d.Members.Member(ctx, projectID, memberID)
		if err != nil {
			return err
		}
		if m.Role == domain.RoleOwner {
			return domain.ErrCannotRemoveOwner
		}
		if err := s.d.Members.RemoveMember(ctx, projectID, memberID); err != nil {
			return err
		}
		if err := s.d.Notifier.Notify(ctx, domain.NoticeRemoved, projectID, memberID, ownerID); err != nil {
			return err
		}
		s.d.Watcher.MembersChanged(ctx, projectID, memberID, ownerID)
		return nil
	})
}

// Leave ends userID's membership (FR-11.5), also while suspended. A non-member gets
// domain.ErrNotFound and the owner domain.ErrOwnerMustTransfer. Nobody is notified.
func (s *Service) Leave(ctx context.Context, userID, projectID string) error {
	return s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		a, err := s.d.Store.Lock(ctx, projectID, userID)
		if err != nil {
			return err
		}
		switch a.Role {
		case domain.RoleNone:
			return domain.ErrNotFound
		case domain.RoleOwner:
			return domain.ErrOwnerMustTransfer
		}
		if err := s.d.Members.RemoveMember(ctx, projectID, userID); err != nil {
			return err
		}
		s.d.Watcher.MembersChanged(ctx, projectID, userID, a.OwnerID)
		return nil
	})
}

// Transfer makes the member newOwnerID the owner and ownerID an editor (FR-10.5), in one
// transaction that locks the project, demoting first so the one-owner index holds at every
// statement and the owner trigger at commit. Checks: visible (domain.ErrNotFound), the
// action "Transfer ownership, delete the project" (domain.ErrForbidden), not suspended
// (*domain.SuspendedError), newOwnerID is a member (domain.ErrNotAMember) other than the
// caller (domain.ErrAlreadyOwner), verified and not suspended (domain.ErrMemberUnavailable).
// The new owner is notified. It returns the project as the caller now sees it.
func (s *Service) Transfer(ctx context.Context, ownerID, projectID, newOwnerID string) (domain.Project, error) {
	err := s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		if _, err := s.managed(ctx, ownerID, projectID, domain.ActTransfer); err != nil {
			return err
		}
		if newOwnerID == ownerID {
			return domain.ErrAlreadyOwner
		}
		rel, err := s.d.Members.Relation(ctx, projectID, newOwnerID)
		if err != nil {
			return err
		}
		if rel.Role == domain.RoleNone {
			return domain.ErrNotAMember
		}
		if !rel.Available {
			return domain.ErrMemberUnavailable
		}
		if err := s.d.Members.SetRole(ctx, projectID, ownerID, domain.RoleEditor); err != nil {
			return err
		}
		if err := s.d.Members.SetRole(ctx, projectID, newOwnerID, domain.RoleOwner); err != nil {
			return err
		}
		if err := s.d.Notifier.Notify(ctx, domain.NoticeOwnership, projectID, newOwnerID, ownerID); err != nil {
			return err
		}
		s.d.Watcher.MembersChanged(ctx, projectID, ownerID, newOwnerID)
		s.d.Watcher.ProjectChanged(ctx, projectID)
		return nil
	})
	if err != nil {
		return domain.Project{}, err
	}
	p, err := s.d.Store.Get(ctx, projectID, ownerID)
	if err != nil {
		return domain.Project{}, err
	}
	return forViewer(p), nil
}

// visibleTo returns the project's access for viewerID, domain.ErrNotFound when they may not
// see it.
func (s *Service) visibleTo(ctx context.Context, viewerID, projectID string) (domain.Project, error) {
	p, err := s.d.Store.Get(ctx, projectID, viewerID)
	if err != nil {
		return p, err
	}
	if !p.Access.Visible() || !s.d.Permissions.Can(p.MyRole, domain.ActView) {
		return p, domain.ErrNotFound
	}
	return p, nil
}

// pageArgs checks page and size as ListMine does.
func pageArgs(page, size int) (int, int, error) {
	if page < 1 || size < 1 {
		return 0, 0, domain.ErrInvalidPage
	}
	size = min(size, MaxPageSize)
	off, ok := offsetOf(page, size)
	if !ok {
		return 0, 0, domain.ErrInvalidPage
	}
	return size, off, nil
}

// MemberPage is one page of members.
type MemberPage struct {
	Items    []domain.Member
	Page     int
	PageSize int
	Total    int
}

// Members lists the project's members for anyone who may see it (SRS 2.3), the owner first.
func (s *Service) Members(ctx context.Context, viewerID, projectID string, page, pageSize int) (MemberPage, error) {
	size, off, err := pageArgs(page, pageSize)
	if err != nil {
		return MemberPage{}, err
	}
	if _, err := s.visibleTo(ctx, viewerID, projectID); err != nil {
		return MemberPage{}, err
	}
	items, total, err := s.d.Members.Members(ctx, projectID, size, off)
	if err != nil {
		return MemberPage{}, err
	}
	return MemberPage{Items: items, Page: page, PageSize: size, Total: total}, nil
}

// RequestPage is one page of a project's join requests or invitations.
type RequestPage struct {
	Items    []domain.RequestItem
	Page     int
	PageSize int
	Total    int
}

// ProjectRequests lists one kind of the project's rows for its owner (FR-11.2, FR-11.3):
// domain.ErrNotFound when the project is not visible, domain.ErrForbidden for anyone else.
func (s *Service) ProjectRequests(ctx context.Context, ownerID, projectID string, kind domain.RequestKind, page, pageSize int) (RequestPage, error) {
	size, off, err := pageArgs(page, pageSize)
	if err != nil {
		return RequestPage{}, err
	}
	p, err := s.visibleTo(ctx, ownerID, projectID)
	if err != nil {
		return RequestPage{}, err
	}
	if !s.d.Permissions.Can(p.MyRole, domain.ActManageMembers) {
		return RequestPage{}, domain.ErrForbidden
	}
	items, total, err := s.d.Members.ProjectRequests(ctx, projectID, kind, size, off)
	if err != nil {
		return RequestPage{}, err
	}
	return RequestPage{Items: items, Page: page, PageSize: size, Total: total}, nil
}

// MyRequestPage is one page of the caller's own join requests or invitations.
type MyRequestPage struct {
	Items    []domain.MyRequestItem
	Page     int
	PageSize int
	Total    int
}

// MyRequests lists userID's join requests with their status, newest first (FR-11.1).
func (s *Service) MyRequests(ctx context.Context, userID string, page, pageSize int) (MyRequestPage, error) {
	size, off, err := pageArgs(page, pageSize)
	if err != nil {
		return MyRequestPage{}, err
	}
	items, total, err := s.d.Members.MyRequests(ctx, userID, size, off)
	if err != nil {
		return MyRequestPage{}, err
	}
	return MyRequestPage{Items: items, Page: page, PageSize: size, Total: total}, nil
}

// MyInvitations lists userID's pending invitations, newest first (FR-11.3).
func (s *Service) MyInvitations(ctx context.Context, userID string, page, pageSize int) (MyRequestPage, error) {
	size, off, err := pageArgs(page, pageSize)
	if err != nil {
		return MyRequestPage{}, err
	}
	items, total, err := s.d.Members.MyInvitations(ctx, userID, size, off)
	if err != nil {
		return MyRequestPage{}, err
	}
	return MyRequestPage{Items: items, Page: page, PageSize: size, Total: total}, nil
}

// WithdrawBetween deletes the pending join requests and invitations between a and b once one
// has blocked the other (FR-16.9): those of either user on a project the other owns.
// Memberships stay. Nobody is notified; the block itself takes back what the two were told
// about each other. It runs inside the block's transaction.
func (s *Service) WithdrawBetween(ctx context.Context, a, b string) error {
	gone, err := s.d.Members.WithdrawBetween(ctx, a, b)
	if err != nil {
		return err
	}
	for _, w := range gone {
		s.d.Watcher.MembersChanged(ctx, w.ProjectID, w.UserID)
	}
	return nil
}

// Titles returns the titles of the projects in ids that viewerID may see, for the
// notifications that name them (FR-9.5).
func (s *Service) Titles(ctx context.Context, viewerID string, ids []string) (map[string]string, error) {
	return s.d.Members.VisibleTitles(ctx, viewerID, ids)
}
