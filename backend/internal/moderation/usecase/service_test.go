package usecase

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/domain"
)

const (
	adminID  = "admin"
	userID   = "user"
	reason   = "Repeated spam in messages"
	reportID = "report-1"
	secret   = "INTERNAL-NOTE-do-not-send"
)

var start = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

// world is one in-memory database, snapshotted by the fake transaction so that a failed action
// leaves nothing behind.
type world struct {
	accounts     map[string]domain.Account
	warnings     []fakeWarning
	restrictions []fakeRestriction
	actions      []domain.NewAction
	notified     []string
	resolved     []string
	suspended    map[string]*string // a reason by user
}

type fakeWarning struct {
	id, userID, reason string
	ackAt              *time.Time
}

type fakeRestriction struct {
	id, userID, kind string
	endsAt           time.Time
	liftedAt         *time.Time
}

func (w world) clone() world {
	c := w
	c.warnings, c.restrictions = slices.Clone(w.warnings), slices.Clone(w.restrictions)
	c.actions, c.notified, c.resolved = slices.Clone(w.actions), slices.Clone(w.notified), slices.Clone(w.resolved)
	c.accounts = map[string]domain.Account{}
	for k, v := range w.accounts {
		c.accounts[k] = v
	}
	c.suspended = map[string]*string{}
	for k, v := range w.suspended {
		c.suspended[k] = v
	}
	return c
}

// fakes is every port of the usecase.
type fakes struct {
	world
	sent       []domain.Email
	failMail   error
	failNotify error
	reportErr  error
}

func (f *fakes) InTx(ctx context.Context, fn func(context.Context) error) error {
	before := f.clone()
	if err := fn(ctx); err != nil {
		f.world = before
		return err
	}
	return nil
}

func (f *fakes) Find(_ context.Context, id string) (domain.Account, error) {
	a, ok := f.accounts[id]
	if !ok {
		return domain.Account{}, domain.ErrNotFound
	}
	return a, nil
}

func (f *fakes) Lock(ctx context.Context, id string) (domain.Account, error) { return f.Find(ctx, id) }

func (f *fakes) Suspend(_ context.Context, id string, at time.Time, until *time.Time, reason string) error {
	a := f.accounts[id]
	a.SuspendedAt, a.SuspendedUntil = &at, until
	f.accounts[id] = a
	f.suspended[id] = &reason
	return nil
}

func (f *fakes) Restore(_ context.Context, id string) error {
	a := f.accounts[id]
	a.SuspendedAt, a.SuspendedUntil = nil, nil
	f.accounts[id] = a
	delete(f.suspended, id)
	return nil
}

func (f *fakes) ResolveForAction(_ context.Context, admin, report, _ string, _ *string) error {
	if f.reportErr != nil {
		return f.reportErr
	}
	f.resolved = append(f.resolved, admin+":"+report)
	return nil
}

func (f *fakes) Warned(_ context.Context, warningID, user string) error {
	if f.failNotify != nil {
		return f.failNotify
	}
	f.notified = append(f.notified, user+":"+warningID)
	return nil
}

func (f *fakes) Send(_ context.Context, e domain.Email) error {
	f.sent = append(f.sent, e)
	return f.failMail
}

func (f *fakes) AddWarning(_ context.Context, user, reason string) (string, error) {
	id := "warning-" + strconv.Itoa(len(f.warnings)+1)
	f.warnings = append(f.warnings, fakeWarning{id: id, userID: user, reason: reason})
	return id, nil
}

func (f *fakes) PendingWarnings(_ context.Context, user string, limit int) ([]domain.Warning, error) {
	out := []domain.Warning{}
	for _, w := range f.warnings {
		if w.userID == user && w.ackAt == nil && len(out) < limit {
			out = append(out, domain.Warning{ID: w.id, Reason: w.reason})
		}
	}
	return out, nil
}

func (f *fakes) FindWarning(_ context.Context, id, user string) (domain.Warning, error) {
	for _, w := range f.warnings {
		if w.id == id && w.userID == user {
			return domain.Warning{ID: w.id, Reason: w.reason, AcknowledgedAt: w.ackAt}, nil
		}
	}
	return domain.Warning{}, domain.ErrNotFound
}

func (f *fakes) AcknowledgeWarning(_ context.Context, id, user string, at time.Time) (bool, error) {
	for i, w := range f.warnings {
		if w.id == id && w.userID == user {
			if w.ackAt == nil {
				f.warnings[i].ackAt = &at
			}
			return true, nil
		}
	}
	return false, nil
}

func (f *fakes) AddRestriction(_ context.Context, user, kind string, _, ends time.Time) (string, error) {
	id := "restriction-" + strconv.Itoa(len(f.restrictions)+1)
	f.restrictions = append(f.restrictions, fakeRestriction{id: id, userID: user, kind: kind, endsAt: ends})
	return id, nil
}

func (f *fakes) HasActiveRestriction(_ context.Context, user, kind string, now time.Time) (bool, error) {
	for _, r := range f.restrictions {
		if r.userID == user && r.kind == kind && r.liftedAt == nil && r.endsAt.After(now) {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakes) RestrictionUser(_ context.Context, id string) (string, error) {
	for _, r := range f.restrictions {
		if r.id == id {
			return r.userID, nil
		}
	}
	return "", domain.ErrNotFound
}

func (f *fakes) LiftRestriction(_ context.Context, id, _ string, now time.Time) (bool, error) {
	for i, r := range f.restrictions {
		if r.id == id && r.liftedAt == nil && r.endsAt.After(now) {
			f.restrictions[i].liftedAt = &now
			return true, nil
		}
	}
	return false, nil
}

func (f *fakes) AddAction(_ context.Context, a domain.NewAction) (string, error) {
	f.actions = append(f.actions, a)
	return "action-" + strconv.Itoa(len(f.actions)), nil
}

func (f *fakes) FindAction(_ context.Context, id string) (domain.Action, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(id, "action-"))
	if err != nil || n < 1 || n > len(f.actions) {
		return domain.Action{}, domain.ErrNotFound
	}
	a := f.actions[n-1]
	return domain.Action{ID: id, UserID: a.UserID, AdminID: &a.AdminID, Kind: a.Kind, Reason: a.Reason, Note: a.Note,
		ReportID: a.ReportID, WarningID: a.WarningID, RestrictionID: a.RestrictionID, EndsAt: a.EndsAt}, nil
}

func (f *fakes) ListActions(ctx context.Context, user string, limit, offset int) ([]domain.Action, int, error) {
	out := []domain.Action{}
	for i := range f.actions {
		if f.actions[i].UserID == user {
			a, err := f.FindAction(ctx, "action-"+strconv.Itoa(i+1))
			if err != nil {
				return nil, 0, err
			}
			out = append(out, a)
		}
	}
	total := len(out)
	return out[min(offset, total):min(offset+limit, total)], total, nil
}

func (f *fakes) Standing(_ context.Context, user string, now time.Time) (domain.StoredStanding, error) {
	a := f.accounts[user]
	st := domain.StoredStanding{SuspendedAt: a.SuspendedAt, SuspendedUntil: a.SuspendedUntil, SuspensionReason: f.suspended[user]}
	for _, r := range f.restrictions {
		if r.userID == user && r.liftedAt == nil && r.endsAt.After(now) {
			ends := r.endsAt
			st.RestrictedUntil = &ends
		}
	}
	return st, nil
}

func (f *fakes) RestrictedUntil(ctx context.Context, user string, now time.Time) (*time.Time, error) {
	st, err := f.Standing(ctx, user, now)
	return st.RestrictedUntil, err
}

type env struct {
	*fakes
	svc   *Service
	clock *time.Time
	logs  *bytes.Buffer
}

func newEnv() env {
	f := &fakes{world: world{accounts: map[string]domain.Account{
		adminID: {ID: adminID, Email: "admin@example.org", FullName: "Admin", Admin: true},
		"other": {ID: "other", Email: "other@example.org", FullName: "Other", Admin: true},
		userID:  {ID: userID, Email: "ada@example.org", FullName: "Ada Lovelace"},
	}, suspended: map[string]*string{}}}
	now := start
	e := env{fakes: f, clock: &now, logs: &bytes.Buffer{}}
	e.svc = New(f, f, f, f, f, f, func() time.Time { return *e.clock }, slog.New(slog.NewTextHandler(e.logs, nil)))
	return e
}

func (e env) nowIs(t time.Time) { *e.clock = t }

func note() *string {
	n := secret
	return &n
}

func report() *string {
	r := reportID
	return &r
}

func input() domain.Input { return domain.Input{Reason: reason, Note: note(), ReportID: report()} }

// actions runs each action on target with a valid body.
func actions(e env, target string) map[string]func() error {
	return map[string]func() error{
		"warn": func() error {
			_, err := e.svc.Warn(context.Background(), adminID, target, input())
			return err
		},
		"restrict": func() error {
			_, err := e.svc.Restrict(context.Background(), adminID, target, domain.RestrictInput{Input: input(), Kind: domain.KindMessaging, Days: 7})
			return err
		},
		"suspend": func() error {
			_, err := e.svc.Suspend(context.Background(), adminID, target, domain.SuspendInput{Input: input()})
			return err
		},
		"restore": func() error {
			_, err := e.svc.Restore(context.Background(), adminID, target, domain.ReverseInput{Reason: reason})
			return err
		},
	}
}

func TestWarn(t *testing.T) {
	e := newEnv()
	a, err := e.svc.Warn(context.Background(), adminID, userID, input())
	if err != nil {
		t.Fatal(err)
	}
	if len(e.warnings) != 1 || e.warnings[0].reason != reason {
		t.Fatalf("warnings = %+v", e.warnings)
	}
	id := e.warnings[0].id
	if !slices.Equal(e.notified, []string{userID + ":" + id}) {
		t.Errorf("notified = %v", e.notified)
	}
	if a.Kind != domain.ActionWarning || a.UserID != userID || *a.AdminID != adminID || a.Reason != reason ||
		*a.Note != secret || *a.ReportID != reportID || *a.WarningID != id {
		t.Errorf("audit row = %+v", a)
	}
	if !slices.Equal(e.resolved, []string{adminID + ":" + reportID}) {
		t.Errorf("resolved = %v", e.resolved)
	}
}

// Every action emails the reason and never the internal note (FR-15.8), and the note reaches no
// log either.
func TestEmailsHaveTheReasonNotTheNote(t *testing.T) {
	ends := start.Add(48 * time.Hour)
	tests := []struct {
		name    string
		run     func(e env) error
		contain []string
	}{
		{"warning", func(e env) error {
			_, err := e.svc.Warn(context.Background(), adminID, userID, input())
			return err
		}, nil},
		{"restriction", func(e env) error {
			_, err := e.svc.Restrict(context.Background(), adminID, userID, domain.RestrictInput{Input: input(), Kind: domain.KindMessaging, Days: 2})
			return err
		}, []string{"7 November 2026 at 12:00 UTC"}},
		{"lift", func(e env) error {
			if _, err := e.svc.Restrict(context.Background(), adminID, userID, domain.RestrictInput{Input: input(), Kind: domain.KindMessaging, Days: 2}); err != nil {
				return err
			}
			e.sent = nil
			return e.svc.Lift(context.Background(), adminID, e.restrictions[0].id, domain.ReverseInput{Reason: reason, Note: note()})
		}, nil},
		{"temporary suspension", func(e env) error {
			_, err := e.svc.Suspend(context.Background(), adminID, userID, domain.SuspendInput{Input: input(), Until: &ends})
			return err
		}, []string{"until 7 November 2026 at 12:00 UTC"}},
		{"permanent suspension", func(e env) error {
			_, err := e.svc.Suspend(context.Background(), adminID, userID, domain.SuspendInput{Input: input()})
			return err
		}, []string{"permanently"}},
		{"restore", func(e env) error {
			if _, err := e.svc.Suspend(context.Background(), adminID, userID, domain.SuspendInput{Input: input()}); err != nil {
				return err
			}
			e.sent = nil
			_, err := e.svc.Restore(context.Background(), adminID, userID, domain.ReverseInput{Reason: reason, Note: note()})
			return err
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			if err := tt.run(e); err != nil {
				t.Fatal(err)
			}
			if len(e.sent) != 1 {
				t.Fatalf("sent %d emails, want 1", len(e.sent))
			}
			m := e.sent[0]
			if m.To != "ada@example.org" || m.FullName != "Ada Lovelace" {
				t.Errorf("recipient = %q %q", m.To, m.FullName)
			}
			for _, want := range append(tt.contain, reason, "Ada Lovelace") {
				if !strings.Contains(m.Body, want) {
					t.Errorf("body has no %q:\n%s", want, m.Body)
				}
			}
			for _, text := range []string{m.Subject, m.Body, e.logs.String()} {
				if strings.Contains(text, secret) {
					t.Errorf("the internal note leaked: %s", text)
				}
			}
		})
	}
}

// A mail failure is logged and does not undo the action; the note stays out of the log.
func TestMailFailureKeepsTheAction(t *testing.T) {
	e := newEnv()
	e.failMail = errors.New("smtp down")
	if _, err := e.svc.Warn(context.Background(), adminID, userID, input()); err != nil {
		t.Fatalf("Warn() error = %v, want nil", err)
	}
	if len(e.warnings) != 1 || len(e.actions) != 1 {
		t.Errorf("warnings = %d, actions = %d, want 1 and 1", len(e.warnings), len(e.actions))
	}
	if !strings.Contains(e.logs.String(), "send moderation email") || strings.Contains(e.logs.String(), secret) {
		t.Errorf("log = %s", e.logs)
	}
}

func TestNoEmailWhenRefused(t *testing.T) {
	e := newEnv()
	e.reportErr = domain.ErrReportResolved
	if _, err := e.svc.Warn(context.Background(), adminID, userID, input()); !errors.Is(err, domain.ErrReportResolved) {
		t.Fatalf("error = %v", err)
	}
	if len(e.sent) != 0 {
		t.Errorf("sent %d emails, want 0", len(e.sent))
	}
}

// Any failing step undoes all of the action (FR-15.8).
func TestActionRollsBack(t *testing.T) {
	aboutOther := &domain.InvalidFieldError{Field: "reportId"}
	tests := []struct {
		name string
		set  func(e env)
		want error
	}{
		{"report unknown", func(e env) { e.reportErr = domain.ErrReportNotFound }, domain.ErrReportNotFound},
		{"report resolved", func(e env) { e.reportErr = domain.ErrReportResolved }, domain.ErrReportResolved},
		{"report about someone else", func(e env) { e.reportErr = aboutOther }, aboutOther},
	}
	for _, tt := range tests {
		for name, run := range map[string]func(e env) error{
			"warn": func(e env) error { return actions(e, userID)["warn"]() }, "restrict": func(e env) error { return actions(e, userID)["restrict"]() },
			"suspend": func(e env) error { return actions(e, userID)["suspend"]() },
		} {
			t.Run(tt.name+"/"+name, func(t *testing.T) {
				e := newEnv()
				tt.set(e)
				if err := run(e); !errors.Is(err, tt.want) {
					t.Fatalf("error = %v, want %v", err, tt.want)
				}
				if len(e.warnings)+len(e.restrictions)+len(e.actions)+len(e.notified) != 0 || e.accounts[userID].SuspendedAt != nil {
					t.Errorf("left behind: %+v", e.world)
				}
			})
		}
	}
	t.Run("notification fails", func(t *testing.T) {
		e := newEnv()
		e.failNotify = errors.New("down")
		if _, err := e.svc.Warn(context.Background(), adminID, userID, input()); !errors.Is(err, e.failNotify) {
			t.Fatalf("error = %v", err)
		}
		if len(e.warnings)+len(e.actions) != 0 {
			t.Errorf("left behind: %+v", e.world)
		}
	})
}

// An administrator cannot act on themselves or on another administrator (FR-15.10).
func TestAdministratorsAreNotModerated(t *testing.T) {
	for _, target := range []string{adminID, "other"} {
		e := newEnv()
		for name, run := range actions(e, target) {
			t.Run(target+"/"+name, func(t *testing.T) {
				if err := run(); !errors.Is(err, domain.ErrCannotModerateAdmin) {
					t.Errorf("error = %v, want ErrCannotModerateAdmin", err)
				}
			})
		}
		if len(e.actions) != 0 || len(e.sent) != 0 {
			t.Errorf("actions = %d, emails = %d, want 0", len(e.actions), len(e.sent))
		}
	}

	t.Run("lift on an administrator", func(t *testing.T) {
		e := newEnv()
		e.restrictions = []fakeRestriction{{id: "r", userID: "other", kind: domain.KindMessaging, endsAt: start.Add(time.Hour)}}
		err := e.svc.Lift(context.Background(), adminID, "r", domain.ReverseInput{Reason: reason})
		if !errors.Is(err, domain.ErrCannotModerateAdmin) {
			t.Errorf("error = %v, want ErrCannotModerateAdmin", err)
		}
	})
}

func TestUnknownUser(t *testing.T) {
	e := newEnv()
	for name, run := range actions(e, "nobody") {
		t.Run(name, func(t *testing.T) {
			if err := run(); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("error = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestReasonAndNoteLimits(t *testing.T) {
	long := func(n int) string { return strings.Repeat("é", n) }
	tests := []struct {
		name   string
		reason string
		note   *string
		field  string
	}{
		{"reason at 10", long(10), nil, ""},
		{"reason at 1000", long(1000), nil, ""},
		{"reason at 9", long(9), nil, "reason"},
		{"reason at 1001", long(1001), nil, "reason"},
		{"reason of spaces around too little", "   short    ", nil, "reason"},
		{"reason with NUL", "ten chars \x00 long", nil, "reason"},
		{"reason not UTF-8", "ten chars \xff long", nil, "reason"},
		{"note at 1000", reason, ptr(long(1000)), ""},
		{"note at 1001", reason, ptr(long(1001)), "note"},
		{"note with NUL", reason, ptr("a\x00b"), "note"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			_, err := e.svc.Warn(context.Background(), adminID, userID, domain.Input{Reason: tt.reason, Note: tt.note})
			var field *domain.InvalidFieldError
			switch {
			case tt.field == "" && err != nil:
				t.Errorf("error = %v, want nil", err)
			case tt.field != "" && (!errors.As(err, &field) || field.Field != tt.field):
				t.Errorf("error = %v, want field %s", err, tt.field)
			}
		})
	}
}

func ptr(s string) *string { return &s }

func TestReasonIsTrimmedAndAnEmptyNoteIsNone(t *testing.T) {
	e := newEnv()
	a, err := e.svc.Warn(context.Background(), adminID, userID, domain.Input{Reason: "  " + reason + "\n", Note: ptr("   ")})
	if err != nil {
		t.Fatal(err)
	}
	if a.Reason != reason || a.Note != nil {
		t.Errorf("reason = %q, note = %v", a.Reason, a.Note)
	}
}

func TestRestrictDaysAndKind(t *testing.T) {
	tests := []struct {
		name  string
		kind  string
		days  int
		field string
	}{
		{"1 day", domain.KindMessaging, 1, ""},
		{"90 days", domain.KindMessaging, 90, ""},
		{"0 days", domain.KindMessaging, 0, "days"},
		{"91 days", domain.KindMessaging, 91, "days"},
		{"unknown kind", "posting", 7, "kind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			a, err := e.svc.Restrict(context.Background(), adminID, userID, domain.RestrictInput{Input: domain.Input{Reason: reason}, Kind: tt.kind, Days: tt.days})
			if tt.field == "" {
				want := start.Add(time.Duration(tt.days) * 24 * time.Hour)
				if err != nil || !a.EndsAt.Equal(want) {
					t.Errorf("EndsAt = %v, error = %v, want %v", a.EndsAt, err, want)
				}
				return
			}
			var field *domain.InvalidFieldError
			if !errors.As(err, &field) || field.Field != tt.field {
				t.Errorf("error = %v, want field %s", err, tt.field)
			}
		})
	}
}

func TestSuspendUntilBounds(t *testing.T) {
	after := func(d time.Duration) *time.Time {
		v := start.Add(d)
		return &v
	}
	const day = 24 * time.Hour
	tests := []struct {
		name  string
		until *time.Time
		ok    bool
	}{
		{"none is permanent", nil, true},
		{"1 day", after(day), true},
		{"1 day, arriving a moment late", after(day - time.Minute), true},
		{"365 days", after(365 * day), true},
		{"just under the slack", after(day - SuspensionSlack - time.Second), false},
		{"just over the slack", after(365*day + SuspensionSlack + time.Second), false},
		{"in the past", after(-day), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			_, err := e.svc.Suspend(context.Background(), adminID, userID, domain.SuspendInput{Input: domain.Input{Reason: reason}, Until: tt.until})
			var field *domain.InvalidFieldError
			switch {
			case tt.ok && err != nil:
				t.Errorf("error = %v, want nil", err)
			case !tt.ok && (!errors.As(err, &field) || field.Field != "until"):
				t.Errorf("error = %v, want field until", err)
			}
		})
	}
}

func TestSecondRestrictionAndLift(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	in := domain.RestrictInput{Input: domain.Input{Reason: reason}, Kind: domain.KindMessaging, Days: 2}
	if _, err := e.svc.Restrict(ctx, adminID, userID, in); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Restrict(ctx, adminID, userID, in); !errors.Is(err, domain.ErrAlreadyRestricted) {
		t.Fatalf("second: error = %v, want ErrAlreadyRestricted", err)
	}
	id := e.restrictions[0].id
	if err := e.svc.Lift(ctx, adminID, id, domain.ReverseInput{Reason: reason}); err != nil {
		t.Fatal(err)
	}
	if e.actions[len(e.actions)-1].Kind != domain.ActionRestrictionLifted {
		t.Errorf("audit rows = %+v", e.actions)
	}
	if err := e.svc.Lift(ctx, adminID, id, domain.ReverseInput{Reason: reason}); !errors.Is(err, domain.ErrRestrictionNotActive) {
		t.Fatalf("lifted twice: error = %v, want ErrRestrictionNotActive", err)
	}
	if err := e.svc.Lift(ctx, adminID, "unknown", domain.ReverseInput{Reason: reason}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: error = %v, want ErrNotFound", err)
	}
	// A new one is allowed once the last is lifted.
	if _, err := e.svc.Restrict(ctx, adminID, userID, in); err != nil {
		t.Fatalf("after the lift: %v", err)
	}
}

func TestRestrictionEndedByTime(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	in := domain.RestrictInput{Input: domain.Input{Reason: reason}, Kind: domain.KindMessaging, Days: 2}
	if _, err := e.svc.Restrict(ctx, adminID, userID, in); err != nil {
		t.Fatal(err)
	}
	ends := e.restrictions[0].endsAt
	e.nowIs(ends)
	if err := e.svc.Lift(ctx, adminID, e.restrictions[0].id, domain.ReverseInput{Reason: reason}); !errors.Is(err, domain.ErrRestrictionNotActive) {
		t.Errorf("lift after the end: error = %v, want ErrRestrictionNotActive", err)
	}
	if _, err := e.svc.Restrict(ctx, adminID, userID, in); err != nil {
		t.Errorf("a new restriction after the end: %v", err)
	}
}

func TestSuspendAndRestoreStates(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	reverse := domain.ReverseInput{Reason: reason}
	if _, err := e.svc.Restore(ctx, adminID, userID, reverse); !errors.Is(err, domain.ErrNotSuspended) {
		t.Fatalf("restore when not suspended: error = %v", err)
	}
	ends := start.Add(48 * time.Hour)
	in := domain.SuspendInput{Input: domain.Input{Reason: reason}, Until: &ends}
	if _, err := e.svc.Suspend(ctx, adminID, userID, in); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Suspend(ctx, adminID, userID, in); !errors.Is(err, domain.ErrAlreadySuspended) {
		t.Fatalf("suspended twice: error = %v", err)
	}

	// Ended by time: nothing to restore, and a new suspension replaces the old.
	e.nowIs(ends)
	if _, err := e.svc.Restore(ctx, adminID, userID, reverse); !errors.Is(err, domain.ErrNotSuspended) {
		t.Fatalf("restore after the end: error = %v", err)
	}
	if _, err := e.svc.Suspend(ctx, adminID, userID, domain.SuspendInput{Input: domain.Input{Reason: reason}}); err != nil {
		t.Fatalf("suspend after the end: %v", err)
	}
	if _, err := e.svc.Restore(ctx, adminID, userID, reverse); err != nil {
		t.Fatal(err)
	}
	if e.accounts[userID].SuspendedAt != nil {
		t.Error("account still suspended")
	}
}

func TestAcknowledge(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	if _, err := e.svc.Warn(ctx, adminID, userID, domain.Input{Reason: reason}); err != nil {
		t.Fatal(err)
	}
	id := e.warnings[0].id
	if err := e.svc.Acknowledge(ctx, "other", id); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("another user's: error = %v, want ErrNotFound", err)
	}
	if err := e.svc.Acknowledge(ctx, userID, id); err != nil {
		t.Fatal(err)
	}
	first := *e.warnings[0].ackAt
	e.nowIs(start.Add(time.Hour))
	if err := e.svc.Acknowledge(ctx, userID, id); err != nil {
		t.Fatalf("twice: %v", err)
	}
	if !e.warnings[0].ackAt.Equal(first) {
		t.Errorf("acknowledged at = %v, want the first time %v", e.warnings[0].ackAt, first)
	}
	if got, err := e.svc.PendingWarnings(ctx, userID); err != nil || len(got) != 0 {
		t.Errorf("pending = %v, error = %v", got, err)
	}
	if w, err := e.svc.Warning(ctx, userID, id); err != nil || w.AcknowledgedAt == nil {
		t.Errorf("Warning() = %+v, error = %v: it must stay readable", w, err)
	}
}

func TestHistory(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	if _, err := e.svc.History(ctx, "nobody", 1); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown user: error = %v", err)
	}
	if _, err := e.svc.History(ctx, userID, 0); !errors.Is(err, domain.ErrInvalidPage) {
		t.Fatalf("page 0: error = %v", err)
	}
	for range HistoryPageSize + 1 {
		if _, err := e.svc.Warn(ctx, adminID, userID, domain.Input{Reason: reason}); err != nil {
			t.Fatal(err)
		}
	}
	h, err := e.svc.History(ctx, userID, 2)
	if err != nil || len(h.Items) != 1 || h.Total != HistoryPageSize+1 || h.Page != 2 {
		t.Errorf("page 2 = %+v, error = %v", h, err)
	}
	// Reading about an administrator is allowed; only acting is refused.
	if _, err := e.svc.History(ctx, "other", 1); err != nil {
		t.Errorf("history of an administrator: %v", err)
	}
}

// The history names its account and judges the suspension by time, also for a suspended
// user, whom no profile shows.
func TestHistoryUser(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	h, err := e.svc.History(ctx, userID, 1)
	if err != nil || h.User.FullName != "Ada Lovelace" || h.User.Email != "ada@example.org" || h.User.Admin || h.Suspended {
		t.Fatalf("not suspended: %+v, error = %v", h, err)
	}
	if h, err = e.svc.History(ctx, "other", 1); err != nil || !h.User.Admin {
		t.Errorf("administrator: %+v, error = %v", h.User, err)
	}
	ends := start.Add(24 * time.Hour)
	a := e.accounts[userID]
	a.SuspendedAt, a.SuspendedUntil = &start, &ends
	e.accounts[userID] = a
	if h, err = e.svc.History(ctx, userID, 1); err != nil || !h.Suspended {
		t.Errorf("suspended: %+v, error = %v", h, err)
	}
	e.nowIs(ends)
	if h, err = e.svc.History(ctx, userID, 1); err != nil || h.Suspended {
		t.Errorf("after its end: %+v, error = %v", h, err)
	}
}

// The client reads this to show a restriction before any send (FR-15.6).
func TestMessagingRestrictedUntil(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	standings := NewStandings(e.fakes, func() time.Time { return *e.clock })
	if got, err := standings.MessagingRestrictedUntil(ctx, userID); err != nil || got != nil {
		t.Fatalf("no restriction: %v, %v", got, err)
	}
	a, err := e.svc.Restrict(ctx, adminID, userID, domain.RestrictInput{Input: domain.Input{Reason: reason}, Kind: domain.KindMessaging, Days: 2})
	if err != nil {
		t.Fatal(err)
	}
	ends := start.Add(48 * time.Hour)
	if got, err := standings.MessagingRestrictedUntil(ctx, userID); err != nil || got == nil || !got.Equal(ends) {
		t.Errorf("restricted: %v, %v, want %v", got, err, ends)
	}
	if got, err := standings.MessagingRestrictedUntil(ctx, "other"); err != nil || got != nil {
		t.Errorf("someone else: %v, %v", got, err)
	}
	if err := e.svc.Lift(ctx, adminID, *a.RestrictionID, domain.ReverseInput{Reason: reason}); err != nil {
		t.Fatal(err)
	}
	if got, err := standings.MessagingRestrictedUntil(ctx, userID); err != nil || got != nil {
		t.Errorf("lifted: %v, %v", got, err)
	}
}

// Standing judges both by time, so each ends by itself (FR-15.6, FR-15.7).
func TestStandingEndsByTime(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	standings := NewStandings(e.fakes, func() time.Time { return *e.clock })
	if _, err := e.svc.Restrict(ctx, adminID, userID, domain.RestrictInput{Input: domain.Input{Reason: reason}, Kind: domain.KindMessaging, Days: 1}); err != nil {
		t.Fatal(err)
	}
	ends := start.Add(48 * time.Hour)
	if _, err := e.svc.Suspend(ctx, adminID, userID, domain.SuspendInput{Input: domain.Input{Reason: reason}, Until: &ends}); err != nil {
		t.Fatal(err)
	}

	st, err := standings.Standing(ctx, userID)
	if err != nil || !st.Suspended || *st.SuspensionReason != reason || !st.SuspendedUntil.Equal(ends) || st.RestrictedUntil == nil {
		t.Fatalf("standing = %+v, error = %v", st, err)
	}
	e.nowIs(start.Add(24 * time.Hour))
	if st, _ = standings.Standing(ctx, userID); !st.Suspended || st.RestrictedUntil != nil {
		t.Errorf("after the restriction ends: %+v", st)
	}
	e.nowIs(ends)
	if st, _ = standings.Standing(ctx, userID); st.Suspended || st.SuspensionReason != nil || st.SuspendedUntil != nil {
		t.Errorf("after the suspension ends: %+v", st)
	}
}
