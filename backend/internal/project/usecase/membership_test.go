package usecase

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

const (
	cy  = "u-cy"  // a third verified user
	dee = "u-dee" // a fourth
)

// isErr reports whether err is want, or of want's type for the error structs (a
// FieldError naming the same field).
func isErr(err, want error) bool {
	var wantField *domain.FieldError
	var fe *domain.FieldError
	var se *domain.SuspendedError
	var rl *domain.RateLimitedError
	switch {
	case errors.As(want, &wantField):
		return errors.As(err, &fe) && fe.Field == wantField.Field
	case errors.As(want, &se):
		return errors.As(err, &se)
	case errors.As(want, &rl):
		return errors.As(err, &rl)
	}
	return errors.Is(err, want)
}

// FR-11.1: the refusals of a join request, in order; none stores, notifies or counts.
func TestRequestToJoinRefusals(t *testing.T) {
	tests := []struct {
		name  string
		setup func(e *env, p string)
		user  string
		msg   *string
		want  error
	}{
		{"unknown project", func(e *env, p string) { delete(e.store.projects, p) }, bob, nil, domain.ErrNotFound},
		{"archived", func(e *env, p string) { e.store.projects[p].Status = domain.StatusArchived }, bob, nil, domain.ErrNotFound},
		{"the owner blocked the user", func(e *env, _ string) { e.store.blocks[[2]string{ada, bob}] = true }, bob, nil, domain.ErrNotFound},
		{"the user blocked the owner", func(e *env, _ string) { e.store.blocks[[2]string{bob, ada}] = true }, bob, nil, domain.ErrNotFound},
		{"the owner is suspended", func(e *env, _ string) { e.store.away[ada] = true }, bob, nil, domain.ErrNotFound},
		{"the user is suspended", func(e *env, _ string) { e.actors.suspended[bob] = true }, bob, nil, &domain.SuspendedError{}},
		{"message too long", nil, bob, ptr(strings.Repeat("é", 501)), &domain.FieldError{Field: "message"}},
		{"message with NUL", nil, bob, ptr("hi\x00"), &domain.FieldError{Field: "message"}},
		{"the owner", nil, ada, nil, domain.ErrAlreadyMember},
		{"a member", func(e *env, p string) { e.store.members[p][bob] = domain.RoleViewer }, bob, nil, domain.ErrAlreadyMember},
		{"closed", func(e *env, p string) { e.store.projects[p].Status = domain.StatusClosed }, bob, nil, domain.ErrProjectNotOpen},
		{"a pending request", func(e *env, p string) {
			e.members.requests = append(e.members.requests, &domain.JoinRequest{ID: "old", ProjectID: p, UserID: bob, Kind: domain.KindRequest, Status: domain.RequestPending})
		}, bob, nil, domain.ErrRequestPending},
		{"a pending invitation", func(e *env, p string) {
			e.members.requests = append(e.members.requests, &domain.JoinRequest{ID: "old", ProjectID: p, UserID: bob, Kind: domain.KindInvitation, Role: domain.RoleViewer, Status: domain.RequestPending})
		}, bob, nil, domain.ErrInvitationPending},
		{"20 requests today", func(e *env, _ string) { e.join.used = map[string]int{bob: JoinRequestsPerWindow} }, bob, nil, &domain.RateLimitedError{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			p := e.store.add(ada, domain.StatusOpen)
			if tt.setup != nil {
				tt.setup(e, p)
			}
			before := len(e.members.requests)
			_, err := e.service.RequestToJoin(t.Context(), tt.user, p, tt.msg)
			if !isErr(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if len(e.members.requests) != before || len(e.notifier.calls) != 0 || len(e.watcher.calls) != 0 {
				t.Errorf("a refused request changed something: %d rows, %v, %v", len(e.members.requests), e.notifier.calls, e.watcher.calls)
			}
			if e.join.used[tt.user] > 0 && tt.name != "20 requests today" {
				t.Error("a refused request was counted")
			}
		})
	}
}

// FR-11.1: a request is stored pending with its trimmed note; the owner is told; the
// requester's project list is renewed; it counts toward the limit. An empty note is none.
func TestRequestToJoin(t *testing.T) {
	e := newEnv()
	p := e.store.add(ada, domain.StatusOpen)
	r, err := e.service.RequestToJoin(t.Context(), bob, p, ptr("  I work on graphs.  "))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != domain.RequestPending || r.Kind != domain.KindRequest || r.UserID != bob || r.Message == nil || *r.Message != "I work on graphs." ||
		r.Project.ID != p || r.Project.Title != "Graph learning" {
		t.Errorf("request = %+v", r)
	}
	if !slices.Equal(e.notifier.calls, []string{"project_join_request " + p + " to " + ada + " from " + bob}) {
		t.Errorf("notices = %v", e.notifier.calls)
	}
	if !slices.Equal(e.watcher.calls, []string{"members " + p + " " + bob}) {
		t.Errorf("watcher = %v", e.watcher.calls)
	}
	if e.join.used[bob] != 1 {
		t.Errorf("counted %d", e.join.used[bob])
	}
	r2, err := e.service.RequestToJoin(t.Context(), cy, p, ptr("   "))
	if err != nil || r2.Message != nil {
		t.Errorf("empty note: %+v, %v", r2.Message, err)
	}
	// A limit that cannot be read lets it through.
	e.join.err = errBoom
	if _, err := e.service.RequestToJoin(t.Context(), dee, p, nil); err != nil {
		t.Errorf("limit down: %v", err)
	}
}

// pending adds a pending row and returns its ID.
func pending(e *env, project, user string, kind domain.RequestKind) string {
	n := domain.NewJoinRequest{ProjectID: project, UserID: user, Kind: kind}
	if kind == domain.KindInvitation {
		n.Role = domain.RoleViewer
	}
	r, err := e.members.CreateRequest(context.Background(), n)
	if err != nil {
		panic(err)
	}
	return r.ID
}

// FR-11.1: only the sender cancels, only while pending, also while suspended; the owner's
// notice is taken back.
func TestCancelRequest(t *testing.T) {
	e := newEnv()
	p := e.store.add(ada, domain.StatusOpen)
	r := pending(e, p, bob, domain.KindRequest)
	for _, who := range []string{ada, cy} {
		if err := e.service.CancelRequest(t.Context(), who, p, r); !errors.Is(err, domain.ErrRequestNotFound) {
			t.Errorf("%s cancelled bob's request: %v", who, err)
		}
	}
	if err := e.service.CancelRequest(t.Context(), bob, "nope", r); !errors.Is(err, domain.ErrRequestNotFound) {
		t.Errorf("unknown project: %v", err)
	}
	e.actors.suspended[bob] = true
	if err := e.service.CancelRequest(t.Context(), bob, p, r); err != nil {
		t.Fatal(err)
	}
	if len(e.members.requests) != 0 {
		t.Error("the request is still there")
	}
	if !slices.Equal(e.notifier.calls, []string{"request handled " + p + " from " + bob}) || !slices.Equal(e.watcher.calls, []string{"members " + p + " " + bob}) {
		t.Errorf("notices %v, watcher %v", e.notifier.calls, e.watcher.calls)
	}
	if err := e.service.CancelRequest(t.Context(), bob, p, r); !errors.Is(err, domain.ErrRequestNotFound) {
		t.Errorf("cancel twice: %v", err)
	}
	// A decided request cannot be cancelled.
	r2 := pending(e, p, cy, domain.KindRequest)
	if _, err := e.service.DecideRequest(t.Context(), ada, p, r2, false, domain.RoleNone); err != nil {
		t.Fatal(err)
	}
	if err := e.service.CancelRequest(t.Context(), cy, p, r2); !errors.Is(err, domain.ErrRequestNotFound) {
		t.Errorf("cancel a decided request: %v", err)
	}
}

// FR-11.2: who may decide, and the refusals.
func TestDecideRequestRefusals(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(e *env, p, r string) string // returns the request ID to decide
		caller string
		accept bool
		role   domain.Role
		want   error
	}{
		{"no role", nil, ada, true, domain.RoleNone, &domain.FieldError{Field: "role"}},
		{"owner role", nil, ada, true, domain.RoleOwner, &domain.FieldError{Field: "role"}},
		{"an editor", func(e *env, p, r string) string { e.store.members[p][dee] = domain.RoleEditor; return r }, dee, true, domain.RoleViewer, domain.ErrForbidden},
		{"a non-member", nil, dee, false, domain.RoleNone, domain.ErrForbidden},
		{"the sender", nil, bob, true, domain.RoleViewer, domain.ErrForbidden},
		{"an archived project, non-member", func(e *env, p, r string) string { e.store.projects[p].Status = domain.StatusArchived; return r }, dee, true, domain.RoleViewer, domain.ErrNotFound},
		{"the owner is suspended", func(e *env, _, r string) string { e.actors.suspended[ada] = true; return r }, ada, true, domain.RoleViewer, &domain.SuspendedError{}},
		{"unknown request", func(*env, string, string) string { return "nope" }, ada, true, domain.RoleViewer, domain.ErrRequestNotFound},
		{"an invitation", func(e *env, p, _ string) string { return pending(e, p, cy, domain.KindInvitation) }, ada, true, domain.RoleViewer, domain.ErrRequestNotFound},
		{"another project's request", func(e *env, _, _ string) string {
			return pending(e, e.store.add(cy, domain.StatusOpen), dee, domain.KindRequest)
		}, ada, true, domain.RoleViewer, domain.ErrRequestNotFound},
		{"the sender is suspended now", func(e *env, _, r string) string { e.store.away[bob] = true; return r }, ada, true, domain.RoleViewer, domain.ErrRequestNotFound},
		{"a block since", func(e *env, _, r string) string { e.store.blocks[[2]string{bob, ada}] = true; return r }, ada, true, domain.RoleViewer, domain.ErrRequestNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			p := e.store.add(ada, domain.StatusOpen)
			r := pending(e, p, bob, domain.KindRequest)
			if tt.setup != nil {
				r = tt.setup(e, p, r)
			}
			_, err := e.service.DecideRequest(t.Context(), tt.caller, p, r, tt.accept, tt.role)
			if !isErr(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if e.store.members[p][bob] != domain.RoleNone || len(e.notifier.calls) != 0 {
				t.Errorf("a refused decision changed something: %v", e.notifier.calls)
			}
		})
	}
}

// FR-11.2, FR-9.2: accepting adds the member with the role, also after the project closed;
// the sender is told and the owner's notice taken back; deciding again is 409.
func TestAcceptRequest(t *testing.T) {
	e := newEnv()
	p := e.store.add(ada, domain.StatusOpen)
	r := pending(e, p, bob, domain.KindRequest)
	e.store.projects[p].Status = domain.StatusClosed
	m, err := e.service.DecideRequest(t.Context(), ada, p, r, true, domain.RoleEditor)
	if err != nil {
		t.Fatal(err)
	}
	if m.UserID != bob || m.Role != domain.RoleEditor || e.store.members[p][bob] != domain.RoleEditor {
		t.Errorf("member %+v, role %q", m, e.store.members[p][bob])
	}
	if e.members.find(r).Status != domain.RequestAccepted || e.members.find(r).DecidedAt == nil {
		t.Errorf("request = %+v", e.members.find(r))
	}
	want := []string{"request handled " + p + " from " + bob, "project_join_request_accepted " + p + " to " + bob + " from " + ada}
	if !slices.Equal(e.notifier.calls, want) {
		t.Errorf("notices = %v", e.notifier.calls)
	}
	if !slices.Equal(e.watcher.calls, []string{"members " + p + " " + bob + "," + ada}) {
		t.Errorf("watcher = %v", e.watcher.calls)
	}
	for _, accept := range []bool{true, false} {
		if _, err := e.service.DecideRequest(t.Context(), ada, p, r, accept, domain.RoleViewer); !errors.Is(err, domain.ErrRequestNotPending) {
			t.Errorf("decide again (accept %v): %v", accept, err)
		}
	}
}

// FR-11.2, FR-9.2: rejecting tells the sender, who may ask again; a suspended or blocked
// sender's request can still be rejected.
func TestRejectRequest(t *testing.T) {
	e := newEnv()
	p := e.store.add(ada, domain.StatusOpen)
	r := pending(e, p, bob, domain.KindRequest)
	e.store.away[bob] = true
	if _, err := e.service.DecideRequest(t.Context(), ada, p, r, false, domain.RoleNone); err != nil {
		t.Fatal(err)
	}
	if e.members.find(r).Status != domain.RequestRejected || e.store.members[p][bob] != domain.RoleNone {
		t.Errorf("request %+v", e.members.find(r))
	}
	want := []string{"request handled " + p + " from " + bob, "project_join_request_rejected " + p + " to " + bob + " from " + ada}
	if !slices.Equal(e.notifier.calls, want) {
		t.Errorf("notices = %v", e.notifier.calls)
	}
	delete(e.store.away, bob)
	if _, err := e.service.RequestToJoin(t.Context(), bob, p, nil); err != nil {
		t.Errorf("ask again after a rejection: %v", err)
	}
}

// FR-11.3: the refusals of an invitation, in order; none stores, notifies or counts.
func TestInviteRefusals(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(e *env, p string)
		caller string
		user   string
		role   domain.Role
		want   error
	}{
		{"owner role", nil, ada, bob, domain.RoleOwner, &domain.FieldError{Field: "role"}},
		{"no role", nil, ada, bob, domain.RoleNone, &domain.FieldError{Field: "role"}},
		{"an editor invites", func(e *env, p string) { e.store.members[p][dee] = domain.RoleEditor }, dee, bob, domain.RoleViewer, domain.ErrForbidden},
		{"a non-member invites", nil, dee, bob, domain.RoleViewer, domain.ErrForbidden},
		{"unknown project", func(e *env, p string) { delete(e.store.projects, p) }, ada, bob, domain.RoleViewer, domain.ErrNotFound},
		{"the owner is suspended", func(e *env, _ string) { e.actors.suspended[ada] = true }, ada, bob, domain.RoleViewer, &domain.SuspendedError{}},
		{"closed", func(e *env, p string) { e.store.projects[p].Status = domain.StatusClosed }, ada, bob, domain.RoleViewer, domain.ErrProjectNotOpen},
		{"archived", func(e *env, p string) { e.store.projects[p].Status = domain.StatusArchived }, ada, bob, domain.RoleViewer, domain.ErrProjectNotOpen},
		{"unknown user", nil, ada, "u-nobody", domain.RoleViewer, domain.ErrUserNotFound},
		{"a suspended user", func(e *env, _ string) { e.store.away[bob] = true }, ada, bob, domain.RoleViewer, domain.ErrUserNotFound},
		{"the owner blocked the user", func(e *env, _ string) { e.store.blocks[[2]string{ada, bob}] = true }, ada, bob, domain.RoleViewer, domain.ErrUserNotFound},
		{"the user blocked the owner", func(e *env, _ string) { e.store.blocks[[2]string{bob, ada}] = true }, ada, bob, domain.RoleViewer, domain.ErrUserNotFound},
		{"the owner", nil, ada, ada, domain.RoleViewer, domain.ErrAlreadyMember},
		{"a member", func(e *env, p string) { e.store.members[p][bob] = domain.RoleViewer }, ada, bob, domain.RoleEditor, domain.ErrAlreadyMember},
		{"invited already", func(e *env, p string) { pending(e, p, bob, domain.KindInvitation) }, ada, bob, domain.RoleViewer, domain.ErrAlreadyInvited},
		{"a pending request", func(e *env, p string) { pending(e, p, bob, domain.KindRequest) }, ada, bob, domain.RoleViewer, domain.ErrRequestPending},
		{"20 invitations today", func(e *env, _ string) { e.invite.used = map[string]int{ada: InvitationsPerWindow} }, ada, bob, domain.RoleViewer, &domain.RateLimitedError{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			p := e.store.add(ada, domain.StatusOpen)
			if tt.setup != nil {
				tt.setup(e, p)
			}
			before := len(e.members.requests)
			_, err := e.service.Invite(t.Context(), tt.caller, p, InviteInput{UserID: tt.user, Role: tt.role})
			if !isErr(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if len(e.members.requests) != before || len(e.notifier.calls) != 0 {
				t.Errorf("a refused invitation changed something: %v", e.notifier.calls)
			}
		})
	}
}

// FR-11.3: an invitation offers its role, tells the invitee and counts; the owner may cancel
// it while pending, which takes back the invitee's notice.
func TestInviteAndCancel(t *testing.T) {
	e := newEnv()
	p := e.store.add(ada, domain.StatusOpen)
	inv, err := e.service.Invite(t.Context(), ada, p, InviteInput{UserID: bob, Role: domain.RoleEditor})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Kind != domain.KindInvitation || inv.Role != domain.RoleEditor || inv.Status != domain.RequestPending || inv.User.UserID != bob || inv.User.FullName != "bob" {
		t.Errorf("invitation = %+v", inv)
	}
	if !slices.Equal(e.notifier.calls, []string{"project_invitation " + p + " to " + bob + " from " + ada}) || e.invite.used[ada] != 1 {
		t.Errorf("notices %v, counted %d", e.notifier.calls, e.invite.used[ada])
	}
	if _, err := e.service.RequestToJoin(t.Context(), bob, p, nil); !errors.Is(err, domain.ErrInvitationPending) {
		t.Errorf("request while invited: %v", err)
	}
	e.notifier.calls = nil
	e.store.members[p][dee] = domain.RoleEditor
	if err := e.service.CancelInvitation(t.Context(), dee, p, inv.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("an editor cancelled: %v", err)
	}
	if err := e.service.CancelInvitation(t.Context(), ada, p, "nope"); !errors.Is(err, domain.ErrRequestNotFound) {
		t.Errorf("unknown invitation: %v", err)
	}
	if err := e.service.CancelInvitation(t.Context(), ada, p, inv.ID); err != nil {
		t.Fatal(err)
	}
	if len(e.members.requests) != 0 || !slices.Equal(e.notifier.calls, []string{"invitation handled " + p + " to " + bob}) {
		t.Errorf("rows %d, notices %v", len(e.members.requests), e.notifier.calls)
	}
	// An answered invitation cannot be cancelled.
	inv2 := pending(e, p, cy, domain.KindInvitation)
	if _, err := e.service.AnswerInvitation(t.Context(), cy, p, inv2, false); err != nil {
		t.Fatal(err)
	}
	if err := e.service.CancelInvitation(t.Context(), ada, p, inv2); !errors.Is(err, domain.ErrInvitationNotPending) {
		t.Errorf("cancel an answered invitation: %v", err)
	}
}

// FR-11.3, FR-9.2: the invitee accepts with the offered role, whatever the status; the owner
// is told; answering twice is 409; only the invitee may answer.
func TestAcceptInvitation(t *testing.T) {
	e := newEnv()
	p := e.store.add(ada, domain.StatusOpen)
	inv := pending(e, p, bob, domain.KindInvitation)
	e.store.projects[p].Status = domain.StatusArchived
	for _, who := range []string{ada, cy} {
		if _, err := e.service.AnswerInvitation(t.Context(), who, p, inv, true); !errors.Is(err, domain.ErrRequestNotFound) {
			t.Errorf("%s answered bob's invitation: %v", who, err)
		}
	}
	m, err := e.service.AnswerInvitation(t.Context(), bob, p, inv, true)
	if err != nil {
		t.Fatal(err)
	}
	if m.Role != domain.RoleViewer || e.store.members[p][bob] != domain.RoleViewer || e.members.find(inv).Status != domain.RequestAccepted {
		t.Errorf("member %+v", m)
	}
	want := []string{"invitation handled " + p + " to " + bob, "project_invitation_accepted " + p + " to " + ada + " from " + bob}
	if !slices.Equal(e.notifier.calls, want) {
		t.Errorf("notices = %v", e.notifier.calls)
	}
	if !slices.Equal(e.watcher.calls, []string{"members " + p + " " + bob + "," + ada}) {
		t.Errorf("watcher = %v", e.watcher.calls)
	}
	for _, accept := range []bool{true, false} {
		if _, err := e.service.AnswerInvitation(t.Context(), bob, p, inv, accept); !errors.Is(err, domain.ErrInvitationNotPending) {
			t.Errorf("answer twice: %v", err)
		}
	}
}

// Accepting needs an account that is not suspended and an owner who is reachable; rejecting
// does not, and tells the owner (FR-9.2).
func TestAnswerInvitationRefusals(t *testing.T) {
	tests := []struct {
		name  string
		setup func(e *env)
		want  error
	}{
		{"the invitee is suspended", func(e *env) { e.actors.suspended[bob] = true }, &domain.SuspendedError{}},
		{"the owner is suspended", func(e *env) { e.store.away[ada] = true }, domain.ErrRequestNotFound},
		{"a block since", func(e *env) { e.store.blocks[[2]string{ada, bob}] = true }, domain.ErrRequestNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			p := e.store.add(ada, domain.StatusOpen)
			inv := pending(e, p, bob, domain.KindInvitation)
			tt.setup(e)
			if _, err := e.service.AnswerInvitation(t.Context(), bob, p, inv, true); !isErr(err, tt.want) {
				t.Fatalf("accept: %v, want %v", err, tt.want)
			}
			if e.store.members[p][bob] != domain.RoleNone || len(e.notifier.calls) != 0 {
				t.Error("a refused accept changed something")
			}
			if _, err := e.service.AnswerInvitation(t.Context(), bob, p, inv, false); err != nil {
				t.Fatalf("reject: %v", err)
			}
			want := []string{"invitation handled " + p + " to " + bob, "project_invitation_rejected " + p + " to " + ada + " from " + bob}
			if !slices.Equal(e.notifier.calls, want) || e.members.find(inv).Status != domain.RequestRejected {
				t.Errorf("notices = %v", e.notifier.calls)
			}
		})
	}
}

// FR-11.4: the owner changes a member's role; the owner's own role moves only by transfer;
// the same role again tells nobody.
func TestChangeRole(t *testing.T) {
	e := newEnv()
	p := e.store.add(ada, domain.StatusOpen)
	e.store.members[p][bob] = domain.RoleViewer
	e.store.members[p][cy] = domain.RoleEditor
	checks := []struct {
		caller, member string
		role           domain.Role
		want           error
	}{
		{ada, bob, domain.RoleOwner, &domain.FieldError{Field: "role"}},
		{cy, bob, domain.RoleEditor, domain.ErrForbidden},
		{bob, bob, domain.RoleEditor, domain.ErrForbidden},
		{ada, dee, domain.RoleEditor, domain.ErrMemberNotFound},
		{ada, ada, domain.RoleEditor, domain.ErrUseTransfer},
	}
	for _, c := range checks {
		if _, err := e.service.ChangeRole(t.Context(), c.caller, p, c.member, c.role); !isErr(err, c.want) {
			t.Errorf("%s changes %s to %q: %v, want %v", c.caller, c.member, c.role, err, c.want)
		}
	}
	e.actors.suspended[ada] = true
	if _, err := e.service.ChangeRole(t.Context(), ada, p, bob, domain.RoleEditor); !isErr(err, &domain.SuspendedError{}) {
		t.Errorf("suspended owner: %v", err)
	}
	delete(e.actors.suspended, ada)
	if len(e.notifier.calls) != 0 {
		t.Fatalf("refusals notified: %v", e.notifier.calls)
	}
	m, err := e.service.ChangeRole(t.Context(), ada, p, bob, domain.RoleViewer)
	if err != nil || m.Role != domain.RoleViewer || len(e.notifier.calls) != 0 || len(e.watcher.calls) != 0 {
		t.Errorf("same role: %+v %v %v", m, err, e.notifier.calls)
	}
	m, err = e.service.ChangeRole(t.Context(), ada, p, bob, domain.RoleEditor)
	if err != nil || m.Role != domain.RoleEditor || e.store.members[p][bob] != domain.RoleEditor {
		t.Fatalf("change: %+v %v", m, err)
	}
	if !slices.Equal(e.notifier.calls, []string{"project_member_role_changed " + p + " to " + bob + " from " + ada}) ||
		!slices.Equal(e.watcher.calls, []string{"members " + p + " " + bob + "," + ada}) {
		t.Errorf("notices %v, watcher %v", e.notifier.calls, e.watcher.calls)
	}
}

// FR-11.4: the owner removes a member, never themselves; the member is told.
func TestRemoveMember(t *testing.T) {
	e := newEnv()
	p := e.store.add(ada, domain.StatusOpen)
	e.store.members[p][bob] = domain.RoleEditor
	e.store.members[p][cy] = domain.RoleEditor
	for _, c := range []struct {
		caller, member string
		want           error
	}{
		{cy, bob, domain.ErrForbidden},
		{ada, ada, domain.ErrCannotRemoveOwner},
		{ada, dee, domain.ErrMemberNotFound},
	} {
		if err := e.service.RemoveMember(t.Context(), c.caller, p, c.member); !errors.Is(err, c.want) {
			t.Errorf("%s removes %s: %v, want %v", c.caller, c.member, err, c.want)
		}
	}
	if err := e.service.RemoveMember(t.Context(), ada, p, bob); err != nil {
		t.Fatal(err)
	}
	if e.store.members[p][bob] != domain.RoleNone || e.store.members[p][cy] != domain.RoleEditor {
		t.Errorf("members = %v", e.store.members[p])
	}
	if !slices.Equal(e.notifier.calls, []string{"project_member_removed " + p + " to " + bob + " from " + ada}) {
		t.Errorf("notices = %v", e.notifier.calls)
	}
}

// FR-11.5: a member other than the owner leaves, also while suspended; nobody is told.
func TestLeave(t *testing.T) {
	e := newEnv()
	p := e.store.add(ada, domain.StatusOpen)
	e.store.members[p][bob] = domain.RoleViewer
	if err := e.service.Leave(t.Context(), ada, p); !errors.Is(err, domain.ErrOwnerMustTransfer) {
		t.Errorf("owner leaves: %v", err)
	}
	if err := e.service.Leave(t.Context(), cy, p); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("non-member leaves: %v", err)
	}
	e.actors.suspended[bob] = true
	if err := e.service.Leave(t.Context(), bob, p); err != nil {
		t.Fatal(err)
	}
	if e.store.members[p][bob] != domain.RoleNone || len(e.notifier.calls) != 0 {
		t.Errorf("members %v, notices %v", e.store.members[p], e.notifier.calls)
	}
	if !slices.Equal(e.watcher.calls, []string{"members " + p + " " + bob + "," + ada}) {
		t.Errorf("watcher = %v", e.watcher.calls)
	}
}

// FR-10.5: ownership moves to a member; the old owner becomes an editor; exactly one owner
// at every step (the fake refuses a second owner, as the unique index does).
func TestTransfer(t *testing.T) {
	e := newEnv()
	p := e.store.add(ada, domain.StatusOpen)
	e.store.members[p][bob] = domain.RoleViewer
	e.store.members[p][cy] = domain.RoleEditor
	for _, c := range []struct {
		caller, to string
		setup      func()
		want       error
	}{
		{cy, bob, nil, domain.ErrForbidden},
		{ada, dee, nil, domain.ErrNotAMember},
		{ada, ada, nil, domain.ErrAlreadyOwner},
		{ada, bob, func() { e.store.away[bob] = true }, domain.ErrMemberUnavailable},
		{ada, bob, func() { delete(e.store.away, bob); e.actors.suspended[ada] = true }, &domain.SuspendedError{}},
	} {
		if c.setup != nil {
			c.setup()
		}
		if _, err := e.service.Transfer(t.Context(), c.caller, p, c.to); !isErr(err, c.want) {
			t.Errorf("%s to %s: %v, want %v", c.caller, c.to, err, c.want)
		}
	}
	delete(e.actors.suspended, ada)
	got, err := e.service.Transfer(t.Context(), ada, p, bob)
	if err != nil {
		t.Fatal(err)
	}
	owners := 0
	for _, r := range e.store.members[p] {
		if r == domain.RoleOwner {
			owners++
		}
	}
	if owners != 1 || e.store.members[p][bob] != domain.RoleOwner || e.store.members[p][ada] != domain.RoleEditor || got.MyRole != domain.RoleEditor {
		t.Errorf("members %v, caller's role %q", e.store.members[p], got.MyRole)
	}
	if !slices.Equal(e.notifier.calls, []string{"project_ownership_transferred " + p + " to " + bob + " from " + ada}) {
		t.Errorf("notices = %v", e.notifier.calls)
	}
	if !slices.Equal(e.watcher.calls, []string{"members " + p + " " + ada + "," + bob, "project " + p}) {
		t.Errorf("watcher = %v", e.watcher.calls)
	}
	// The new owner manages; the old one no longer does.
	if _, err := e.service.Invite(t.Context(), ada, p, InviteInput{UserID: dee, Role: domain.RoleViewer}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("old owner invites: %v", err)
	}
	if _, err := e.service.Invite(t.Context(), bob, p, InviteInput{UserID: dee, Role: domain.RoleViewer}); err != nil {
		t.Errorf("new owner invites: %v", err)
	}
}

// The lists: the owner's lists of a project, the members for anyone who may see it, and
// the caller's own.
func TestMembershipLists(t *testing.T) {
	e := newEnv()
	p := e.store.add(ada, domain.StatusOpen)
	e.store.members[p][cy] = domain.RoleEditor
	pending(e, p, bob, domain.KindRequest)
	pending(e, p, dee, domain.KindInvitation)
	reqs, err := e.service.ProjectRequests(t.Context(), ada, p, domain.KindRequest, 1, 20)
	if err != nil || reqs.Total != 1 || reqs.Items[0].UserID != bob {
		t.Errorf("requests %+v %v", reqs, err)
	}
	invs, err := e.service.ProjectRequests(t.Context(), ada, p, domain.KindInvitation, 1, 100)
	if err != nil || invs.Total != 1 || invs.Items[0].UserID != dee || invs.PageSize != MaxPageSize {
		t.Errorf("invitations %+v %v", invs, err)
	}
	if _, err := e.service.ProjectRequests(t.Context(), cy, p, domain.KindRequest, 1, 20); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("an editor read the requests: %v", err)
	}
	if _, err := e.service.ProjectRequests(t.Context(), ada, p, domain.KindRequest, 0, 20); !errors.Is(err, domain.ErrInvalidPage) {
		t.Errorf("page 0: %v", err)
	}
	ms, err := e.service.Members(t.Context(), bob, p, 1, 20)
	if err != nil || ms.Total != 2 || ms.Items[0].Role != domain.RoleOwner {
		t.Errorf("members %+v %v", ms, err)
	}
	e.store.projects[p].Status = domain.StatusArchived
	if _, err := e.service.Members(t.Context(), bob, p, 1, 20); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a non-member read an archived project's members: %v", err)
	}
	if _, err := e.service.Members(t.Context(), cy, p, 1, 20); err != nil {
		t.Errorf("a member: %v", err)
	}
	mine, err := e.service.MyRequests(t.Context(), bob, 1, 20)
	if err != nil || mine.Total != 1 {
		t.Errorf("my requests %+v %v", mine, err)
	}
	myInv, err := e.service.MyInvitations(t.Context(), dee, 1, 20)
	if err != nil || myInv.Total != 1 || myInv.Items[0].Role != domain.RoleViewer {
		t.Errorf("my invitations %+v %v", myInv, err)
	}
	if _, err := e.service.MyInvitations(t.Context(), dee, 1, 0); !errors.Is(err, domain.ErrInvalidPage) {
		t.Errorf("size 0: %v", err)
	}
}

// FR-16.9 (decisions.md 2026-11-17): a block deletes the pending requests and invitations
// between the two, both ways, and leaves memberships and other users' rows alone.
func TestWithdrawBetween(t *testing.T) {
	e := newEnv()
	pa := e.store.add(ada, domain.StatusOpen)
	pb := e.store.add(bob, domain.StatusOpen)
	pending(e, pa, bob, domain.KindRequest)    // bob asked ada's project
	pending(e, pb, ada, domain.KindInvitation) // bob invited ada
	keep := pending(e, pa, cy, domain.KindRequest)
	e.store.members[pa][dee] = domain.RoleViewer
	pc := e.store.add(cy, domain.StatusOpen)
	e.store.members[pc][bob] = domain.RoleEditor
	if err := e.service.WithdrawBetween(t.Context(), bob, ada); err != nil {
		t.Fatal(err)
	}
	if len(e.members.requests) != 1 || e.members.requests[0].ID != keep {
		t.Errorf("rows left: %+v", e.members.requests)
	}
	if e.store.members[pc][bob] != domain.RoleEditor || e.store.members[pa][dee] != domain.RoleViewer {
		t.Error("a membership changed")
	}
	if len(e.notifier.calls) != 0 {
		t.Errorf("notices = %v", e.notifier.calls)
	}
	if !slices.Equal(e.watcher.calls, []string{"members " + pa + " " + bob, "members " + pb + " " + ada}) {
		t.Errorf("watcher = %v", e.watcher.calls)
	}
}

// FR-9.5: notifications name a project only to someone who may see it.
func TestTitles(t *testing.T) {
	e := newEnv()
	open := e.store.add(ada, domain.StatusOpen)
	archived := e.store.add(ada, domain.StatusArchived)
	got, err := e.service.Titles(t.Context(), bob, []string{open, archived, "nope"})
	if err != nil || len(got) != 1 || got[open] != "Graph learning" {
		t.Errorf("titles = %v, %v", got, err)
	}
	e.store.members[archived][bob] = domain.RoleViewer
	if got, _ := e.service.Titles(t.Context(), bob, []string{archived}); len(got) != 1 {
		t.Errorf("a member: %v", got)
	}
}

// A notification that cannot be stored fails the change (one transaction, FR-9.2).
func TestNotifyFailureFails(t *testing.T) {
	e := newEnv()
	p := e.store.add(ada, domain.StatusOpen)
	e.notifier.err = errBoom
	if _, err := e.service.RequestToJoin(t.Context(), bob, p, nil); !errors.Is(err, errBoom) {
		t.Errorf("err = %v", err)
	}
	if e.join.used[bob] != 0 {
		t.Error("a failed request was counted")
	}
}
