package usecase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

// env is a Service over fakes, with users ada, bob and cy (visible) and sam (suspended or
// not verified, which the store does not tell apart).
type env struct {
	svc      *Service
	store    *fakeStore
	limiter  *fakeLimiter
	notifier *fakeNotifier
	tx       *fakeTx
	actors   fakeActors
	logs     *bytes.Buffer
}

func newEnv() env {
	e := env{store: newFakeStore(), limiter: newFakeLimiter(), actors: fakeActors{}, logs: &bytes.Buffer{}}
	e.tx = &fakeTx{store: e.store}
	e.notifier = &fakeNotifier{tx: e.tx}
	for _, u := range []string{"ada", "bob", "cy"} {
		e.store.addUser(u, false)
	}
	e.store.addUser("sam", true)
	e.svc = New(e.store, e.tx, e.limiter, e.notifier, e.actors, slog.New(slog.NewTextHandler(e.logs, nil)))
	return e
}

var ctx = context.Background()

// fakeActors gives each user a standing; nobody listed is in good standing.
type fakeActors map[string]domain.Standing

func (f fakeActors) Standing(_ context.Context, userID string) (domain.Standing, error) {
	return f[userID], nil
}

func (e env) send(t *testing.T, from, to string) domain.PendingRequest {
	t.Helper()
	r, err := e.svc.Send(ctx, from, to)
	if err != nil {
		t.Fatalf("Send %s -> %s: %v", from, to, err)
	}
	return r
}

func (e env) relation(t *testing.T, viewer, owner string) domain.Relation {
	t.Helper()
	r, err := e.svc.Relation(ctx, viewer, owner)
	if err != nil {
		t.Fatalf("Relation %s, %s: %v", viewer, owner, err)
	}
	return r
}

func TestSendCreatesRequest(t *testing.T) {
	e := newEnv()
	r := e.send(t, "ada", "bob")
	if r.ID == "" || r.Other.UserID != "bob" || r.Other.FullName != "Name of bob" || r.CreatedAt.IsZero() {
		t.Errorf("Send returned %+v, want the request naming bob", r)
	}
	if e.limiter.used["ada"] != 1 || len(e.limiter.recorded) != 1 || e.limiter.recorded[0] != RequestWindow {
		t.Errorf("limiter used %v, recorded %v; want one request counted for a day", e.limiter.used, e.limiter.recorded)
	}
	if len(e.notifier.sent) != 1 || e.notifier.sent[0].ReceiverID != "bob" || e.notifier.sent[0].RequesterID != "ada" {
		t.Errorf("notifier heard %+v, want one RequestSent ada -> bob", e.notifier.sent)
	}
}

func TestSendRefusals(t *testing.T) {
	tests := []struct {
		name  string
		setup func(e env)
		to    string
		want  error
	}{
		{"oneself (FR-7.2)", nil, "ada", domain.ErrSelf},
		{"unknown user", nil, "nobody", domain.ErrNotFound},
		{"suspended or unverified user", nil, "sam", domain.ErrNotFound},
		{"own request pending (FR-7.2)", func(e env) { mustSend(e, "ada", "bob") }, "bob", domain.ErrAlreadyPending},
		{"already connected (FR-7.2)", func(e env) {
			r := mustSend(e, "bob", "ada")
			if _, err := e.svc.Accept(ctx, "ada", r.ID); err != nil {
				panic(err)
			}
		}, "bob", domain.ErrAlreadyConnected},
		{"already connected, other direction (FR-7.2)", func(e env) {
			r := mustSend(e, "ada", "bob")
			if _, err := e.svc.Accept(ctx, "bob", r.ID); err != nil {
				panic(err)
			}
		}, "bob", domain.ErrAlreadyConnected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			if tt.setup != nil {
				tt.setup(e)
			}
			before := len(e.store.rows)
			usedBefore, notifiedBefore := e.limiter.used["ada"], len(e.notifier.sent)
			if _, err := e.svc.Send(ctx, "ada", tt.to); !errors.Is(err, tt.want) {
				t.Fatalf("Send: %v, want %v", err, tt.want)
			}
			if len(e.store.rows) != before || e.limiter.used["ada"] != usedBefore || len(e.notifier.sent) != notifiedBefore {
				t.Error("a refused request was stored, counted or notified")
			}
		})
	}
}

// When the other user's request is pending, no second request is made: the error names
// theirs so the caller can accept it.
func TestSendWhenTheyAlreadyAsked(t *testing.T) {
	e := newEnv()
	theirs := e.send(t, "bob", "ada")
	_, err := e.svc.Send(ctx, "ada", "bob")
	var pending *domain.PendingFromThemError
	if !errors.As(err, &pending) || pending.RequestID != theirs.ID {
		t.Fatalf("Send: %v, want PendingFromThemError naming %s", err, theirs.ID)
	}
	if len(e.store.rows) != 1 {
		t.Errorf("%d rows, want only bob's request", len(e.store.rows))
	}
}

// FR-7.6: after a rejection the sender may ask again, and nothing tells them they were
// rejected: their sent list and the relation look as after a cancel.
func TestSendAgainAfterRejection(t *testing.T) {
	e := newEnv()
	first := e.send(t, "ada", "bob")
	if err := e.svc.Reject(ctx, "bob", first.ID); err != nil {
		t.Fatal(err)
	}
	sent, err := e.svc.ListSent(ctx, "ada", 1)
	if err != nil || sent.Total != 0 || len(sent.Items) != 0 {
		t.Errorf("ada's sent list after the rejection: %+v, %v; want empty", sent, err)
	}
	if r := e.relation(t, "ada", "bob"); r.Status != domain.StatusNone || r.RequestID != nil {
		t.Errorf("ada sees %+v, want none", r)
	}
	if r := e.relation(t, "bob", "ada"); r.Status != domain.StatusNone {
		t.Errorf("bob sees %+v, want none", r)
	}
	second := e.send(t, "ada", "bob")
	if second.ID == first.ID {
		t.Error("the new request reused the rejected one")
	}
	// The rejected request does not block the other direction either.
	if err := e.svc.Cancel(ctx, "ada", second.ID); err != nil {
		t.Fatal(err)
	}
	e.send(t, "bob", "ada")
}

func TestSendRateLimit(t *testing.T) {
	e := newEnv()
	e.limiter.used["ada"] = RequestsPerWindow - 1
	e.send(t, "ada", "bob")

	e.limiter.left = 90 * time.Minute
	_, err := e.svc.Send(ctx, "ada", "cy")
	var limited *domain.RateLimitedError
	if !errors.As(err, &limited) || limited.RetryAfter != 90*time.Minute {
		t.Fatalf("51st request: %v, want RateLimitedError with 90m left", err)
	}
	if len(e.store.rows) != 1 || e.store.creates != 1 {
		t.Errorf("the limited request reached the store")
	}

	// The limit comes before the receiver is looked at, but after the self check.
	if _, err := e.svc.Send(ctx, "ada", "nobody"); !errors.As(err, &limited) {
		t.Errorf("limited request to an unknown user: %v, want RateLimitedError", err)
	}
	if _, err := e.svc.Send(ctx, "ada", "ada"); !errors.Is(err, domain.ErrSelf) {
		t.Errorf("limited request to oneself: %v, want ErrSelf", err)
	}
}

// When the limiter fails the request goes ahead, with a warning (fail open).
func TestSendLimiterDown(t *testing.T) {
	e := newEnv()
	e.limiter.err = errors.New("redis: connection refused")
	e.send(t, "ada", "bob")
	logs := e.logs.String()
	if strings.Count(logs, "level=WARN") != 2 || !strings.Contains(logs, "redis: connection refused") {
		t.Errorf("logs %q, want a warning for the check and one for the count", logs)
	}
}

// A refusing link that is gone when it is read (cancelled or rejected in between) makes
// Send try again, a bounded number of times.
func TestSendRetriesWhenLinkVanishes(t *testing.T) {
	e := newEnv()
	e.send(t, "bob", "ada")
	e.store.vanish = 1
	var pending *domain.PendingFromThemError
	if _, err := e.svc.Send(ctx, "ada", "bob"); !errors.As(err, &pending) {
		t.Fatalf("Send: %v, want PendingFromThemError after one retry", err)
	}
	if e.store.creates != 3 { // bob's, then ada's two attempts
		t.Errorf("%d CreateRequest calls, want 3", e.store.creates)
	}

	e.store.vanish = sendAttempts
	if _, err := e.svc.Send(ctx, "ada", "bob"); err == nil || errors.As(err, &pending) {
		t.Errorf("Send with the link always gone: %v, want a plain error", err)
	}
}

func TestSendStoreError(t *testing.T) {
	e := newEnv()
	boom := errors.New("boom")
	e.store.err = boom
	if _, err := e.svc.Send(ctx, "ada", "bob"); !errors.Is(err, boom) {
		t.Errorf("Send: %v, want the store error", err)
	}
}

// A failing notifier rolls back the change it reports, so a change is never stored
// without its notification (FR-9.1).
func TestNotifierErrorRollsBack(t *testing.T) {
	queueDown := errors.New("queue down")

	t.Run("send", func(t *testing.T) {
		e := newEnv()
		e.notifier.err = queueDown
		if _, err := e.svc.Send(ctx, "ada", "bob"); !errors.Is(err, queueDown) {
			t.Fatalf("Send: %v, want the notifier error", err)
		}
		if len(e.store.rows) != 0 || e.limiter.used["ada"] != 0 {
			t.Errorf("a request whose notification failed was stored (%d rows) or counted (%d)", len(e.store.rows), e.limiter.used["ada"])
		}
	})

	t.Run("accept, reject and cancel", func(t *testing.T) {
		e := newEnv()
		r := e.send(t, "ada", "bob")
		e.notifier.err = queueDown
		if _, err := e.svc.Accept(ctx, "bob", r.ID); !errors.Is(err, queueDown) {
			t.Errorf("Accept: %v, want the notifier error", err)
		}
		if err := e.svc.Reject(ctx, "bob", r.ID); !errors.Is(err, queueDown) {
			t.Errorf("Reject: %v, want the notifier error", err)
		}
		if err := e.svc.Cancel(ctx, "ada", r.ID); !errors.Is(err, queueDown) {
			t.Errorf("Cancel: %v, want the notifier error", err)
		}
		if rel := e.relation(t, "bob", "ada"); rel.Status != domain.StatusPendingReceived {
			t.Errorf("bob sees %+v, want the request still pending after every change rolled back", rel)
		}
		if e.tx.rollbacks != 3 {
			t.Errorf("rolled back %d times, want 3", e.tx.rollbacks)
		}
	})
}

// Every notifier call runs inside the transaction of the change it reports.
func TestNotifierRunsInsideTransaction(t *testing.T) {
	e := newEnv()
	r := e.send(t, "ada", "bob")
	if _, err := e.svc.Accept(ctx, "bob", r.ID); err != nil {
		t.Fatal(err)
	}
	r2 := e.send(t, "cy", "bob")
	if err := e.svc.Reject(ctx, "bob", r2.ID); err != nil {
		t.Fatal(err)
	}
	r3 := e.send(t, "cy", "ada")
	if err := e.svc.Cancel(ctx, "cy", r3.ID); err != nil {
		t.Fatal(err)
	}
	if e.notifier.outsideTx != 0 {
		t.Errorf("%d notifier calls ran outside a transaction", e.notifier.outsideTx)
	}
}

// Rejecting or cancelling a request withdraws its notification; nothing else is said
// (FR-7.6). A refused reject or cancel withdraws nothing.
func TestRejectAndCancelWithdraw(t *testing.T) {
	e := newEnv()
	r := e.send(t, "ada", "bob")
	if err := e.svc.Reject(ctx, "bob", r.ID); err != nil {
		t.Fatal(err)
	}
	r2 := e.send(t, "ada", "bob")
	if err := e.svc.Cancel(ctx, "ada", r2.ID); err != nil {
		t.Fatal(err)
	}
	if want := []string{r.ID, r2.ID}; !slices.Equal(e.notifier.withdrawn, want) {
		t.Errorf("withdrawn %v, want %v", e.notifier.withdrawn, want)
	}
	if len(e.notifier.accepted) != 0 {
		t.Errorf("a rejection was reported as %+v", e.notifier.accepted)
	}
	if err := e.svc.Reject(ctx, "bob", r.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second Reject: %v, want ErrNotFound", err)
	}
	if err := e.svc.Cancel(ctx, "bob", r2.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Cancel by the receiver: %v, want ErrNotFound", err)
	}
	if len(e.notifier.withdrawn) != 2 {
		t.Errorf("a refused change withdrew a notification: %v", e.notifier.withdrawn)
	}
}

func TestAccept(t *testing.T) {
	e := newEnv()
	r := e.send(t, "ada", "bob")
	c, err := e.svc.Accept(ctx, "bob", r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Other.UserID != "ada" || c.ConnectedAt.IsZero() {
		t.Errorf("Accept returned %+v, want the connection naming ada", c)
	}
	if len(e.notifier.accepted) != 1 || e.notifier.accepted[0].ID != r.ID {
		t.Errorf("notifier heard %+v, want RequestAccepted for %s", e.notifier.accepted, r.ID)
	}
	for _, pair := range [][2]string{{"ada", "bob"}, {"bob", "ada"}} {
		if rel := e.relation(t, pair[0], pair[1]); rel.Status != domain.StatusConnected || rel.RequestID != nil {
			t.Errorf("%s sees %+v, want connected", pair[0], rel)
		}
	}
	for _, u := range []string{"ada", "bob"} {
		if n, err := e.svc.CountConnections(ctx, u, u); err != nil || n != 1 {
			t.Errorf("CountConnections(%s) = %d, %v; want 1", u, n, err)
		}
	}
}

// Accept, reject and cancel answer ErrNotFound to the wrong user and for a request that
// is not pending, so the answer does not reveal the request (FR-7.3).
func TestAnswerRefusals(t *testing.T) {
	type op func(e env, user, id string) error
	accept := func(e env, user, id string) error { _, err := e.svc.Accept(ctx, user, id); return err }
	reject := func(e env, user, id string) error { return e.svc.Reject(ctx, user, id) }
	cancel := func(e env, user, id string) error { return e.svc.Cancel(ctx, user, id) }

	tests := []struct {
		name string
		op   op
		user string
		// prepare returns the request ID to use after changing the state.
		prepare func(e env, id string) string
	}{
		{"accept by the sender", accept, "ada", nil},
		{"accept by a third user", accept, "cy", nil},
		{"accept an unknown request", accept, "bob", func(env, string) string { return "req-999" }},
		{"accept an accepted request", accept, "bob", func(e env, id string) string { mustAccept(e, id); return id }},
		{"accept a rejected request", accept, "bob", func(e env, id string) string { mustReject(e, id); return id }},
		{"accept a cancelled request", accept, "bob", func(e env, id string) string { mustCancel(e, id); return id }},
		{"accept from a suspended sender", accept, "bob", func(e env, id string) string { e.store.visible["ada"] = false; return id }},
		{"reject by the sender", reject, "ada", nil},
		{"reject by a third user", reject, "cy", nil},
		{"reject an accepted request", reject, "bob", func(e env, id string) string { mustAccept(e, id); return id }},
		{"reject a rejected request", reject, "bob", func(e env, id string) string { mustReject(e, id); return id }},
		{"cancel by the receiver", cancel, "bob", nil},
		{"cancel by a third user", cancel, "cy", nil},
		{"cancel an accepted request", cancel, "ada", func(e env, id string) string { mustAccept(e, id); return id }},
		{"cancel a rejected request", cancel, "ada", func(e env, id string) string { mustReject(e, id); return id }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			id := e.send(t, "ada", "bob").ID
			if tt.prepare != nil {
				id = tt.prepare(e, id)
			}
			if err := tt.op(e, tt.user, id); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("got %v, want ErrNotFound", err)
			}
		})
	}
}

func mustSend(e env, from, to string) domain.PendingRequest {
	r, err := e.svc.Send(ctx, from, to)
	if err != nil {
		panic(err)
	}
	return r
}

func mustAccept(e env, id string) {
	if _, err := e.svc.Accept(ctx, "bob", id); err != nil {
		panic(err)
	}
}

func mustReject(e env, id string) {
	if err := e.svc.Reject(ctx, "bob", id); err != nil {
		panic(err)
	}
}

func mustCancel(e env, id string) {
	if err := e.svc.Cancel(ctx, "ada", id); err != nil {
		panic(err)
	}
}

func TestCancel(t *testing.T) {
	e := newEnv()
	r := e.send(t, "ada", "bob")
	if err := e.svc.Cancel(ctx, "ada", r.ID); err != nil {
		t.Fatal(err)
	}
	if rel := e.relation(t, "bob", "ada"); rel.Status != domain.StatusNone {
		t.Errorf("bob sees %+v after the cancel, want none", rel)
	}
	if got, _ := e.svc.ListReceived(ctx, "bob", 1); got.Total != 0 {
		t.Errorf("bob's received list after the cancel: %+v, want empty", got)
	}
	e.send(t, "ada", "bob") // a new request is allowed
}

func TestRemove(t *testing.T) {
	for _, remover := range []string{"ada", "bob"} {
		t.Run("by "+remover, func(t *testing.T) {
			e := newEnv()
			r := e.send(t, "ada", "bob")
			mustAccept(e, r.ID)
			other := map[string]string{"ada": "bob", "bob": "ada"}[remover]
			if err := e.svc.Remove(ctx, remover, other); err != nil {
				t.Fatal(err)
			}
			if rel := e.relation(t, "ada", "bob"); rel.Status != domain.StatusNone {
				t.Errorf("after removal ada sees %+v, want none", rel)
			}
			if err := e.svc.Remove(ctx, remover, other); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("second Remove: %v, want ErrNotFound", err)
			}
			e.send(t, other, remover) // either side may ask again
		})
	}
}

func TestRemoveRefusals(t *testing.T) {
	e := newEnv()
	e.send(t, "ada", "bob")
	for _, tt := range []struct{ name, user, other string }{
		{"pending request is not a connection", "ada", "bob"},
		{"pending request, receiver side", "bob", "ada"},
		{"never linked", "ada", "cy"},
		{"unknown user", "ada", "nobody"},
	} {
		if err := e.svc.Remove(ctx, tt.user, tt.other); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s: %v, want ErrNotFound", tt.name, err)
		}
	}
	if len(e.store.rows) != 1 {
		t.Error("a refused Remove deleted the pending request")
	}
}

func TestRelation(t *testing.T) {
	e := newEnv()
	r := e.send(t, "ada", "bob")
	tests := []struct {
		viewer, owner string
		want          domain.Status
		wantID        bool
	}{
		{"ada", "bob", domain.StatusPendingSent, true},
		{"bob", "ada", domain.StatusPendingReceived, true},
		{"ada", "cy", domain.StatusNone, false},
		{"ada", "ada", domain.StatusSelf, false},
	}
	for _, tt := range tests {
		got := e.relation(t, tt.viewer, tt.owner)
		if got.Status != tt.want || (got.RequestID != nil) != tt.wantID || (tt.wantID && *got.RequestID != r.ID) {
			t.Errorf("Relation(%s, %s) = %+v, want %s (request ID %v)", tt.viewer, tt.owner, got, tt.want, tt.wantID)
		}
	}
	e.store.err = errors.New("boom")
	if _, err := e.svc.Relation(ctx, "ada", "bob"); err == nil {
		t.Error("Relation hid the store error")
	}
}

func TestRelations(t *testing.T) {
	e := newEnv()
	e.store.addUser("dee", false)
	e.store.addUser("eve", false)
	sent := e.send(t, "ada", "bob")                    // pending_sent for ada
	received := e.send(t, "cy", "ada")                 // pending_received for ada
	mustAcceptAs(e, "ada", e.send(t, "dee", "ada").ID) // connected
	rejected := e.send(t, "ada", "eve")
	if err := e.svc.Reject(ctx, "eve", rejected.ID); err != nil {
		t.Fatal(err)
	}
	e.store.addUser("fay", false)
	e.send(t, "bob", "fay") // someone else's request: none for ada

	e.store.linkLists = 0
	got, err := e.svc.Relations(ctx, "ada", []string{"bob", "cy", "dee", "eve", "fay", "ada", "nobody"})
	if err != nil {
		t.Fatal(err)
	}
	if e.store.linkLists != 1 {
		t.Errorf("Relations made %d store calls, want 1", e.store.linkLists)
	}
	want := map[string]domain.Relation{
		"bob":    {Status: domain.StatusPendingSent, RequestID: &sent.ID},
		"cy":     {Status: domain.StatusPendingReceived, RequestID: &received.ID},
		"dee":    {Status: domain.StatusConnected},
		"eve":    {Status: domain.StatusNone}, // rejected (FR-7.6)
		"fay":    {Status: domain.StatusNone},
		"ada":    {Status: domain.StatusSelf},
		"nobody": {Status: domain.StatusNone},
	}
	if len(got) != len(want) {
		t.Errorf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for id, w := range want {
		g := got[id]
		if g.Status != w.Status || (g.RequestID == nil) != (w.RequestID == nil) || (w.RequestID != nil && *g.RequestID != *w.RequestID) {
			t.Errorf("%s: %+v, want %+v", id, g, w)
		}
	}

	// Nobody but the viewer, or nobody at all: no store call.
	e.store.linkLists = 0
	for _, ids := range [][]string{nil, {"ada"}} {
		if _, err := e.svc.Relations(ctx, "ada", ids); err != nil {
			t.Fatal(err)
		}
	}
	if e.store.linkLists != 0 {
		t.Errorf("Relations of nobody else made %d store calls, want 0", e.store.linkLists)
	}
	e.store.err = errors.New("boom")
	if _, err := e.svc.Relations(ctx, "ada", []string{"bob"}); err == nil {
		t.Error("Relations hid the store error")
	}
}

func TestLists(t *testing.T) {
	e := newEnv()
	// ada receives 25 requests from fresh users (two pages) and one from a hidden user.
	var ids []string
	for i := range 25 {
		u := fmt.Sprintf("u%02d", i)
		e.store.addUser(u, false)
		ids = append(ids, e.send(t, u, "ada").ID)
	}
	e.store.addUser("hidden", false)
	e.send(t, "hidden", "ada")
	e.store.visible["hidden"] = false

	p1, err := e.svc.ListReceived(ctx, "ada", 1)
	if err != nil {
		t.Fatal(err)
	}
	if p1.Total != 25 || p1.Page != 1 || p1.PageSize != PageSize || len(p1.Items) != 20 {
		t.Fatalf("page 1: total %d page %d size %d items %d; want 25, 1, 20, 20", p1.Total, p1.Page, p1.PageSize, len(p1.Items))
	}
	if p1.Items[0].ID != ids[24] || p1.Items[0].Other.UserID != "u24" {
		t.Errorf("first item %+v, want the newest request, from u24", p1.Items[0])
	}
	p2, _ := e.svc.ListReceived(ctx, "ada", 2)
	if len(p2.Items) != 5 || p2.Items[4].ID != ids[0] {
		t.Errorf("page 2 has %d items, last %+v; want 5 ending with the oldest", len(p2.Items), p2.Items)
	}
	p3, _ := e.svc.ListReceived(ctx, "ada", 3)
	if len(p3.Items) != 0 || p3.Total != 25 {
		t.Errorf("page 3: %+v, want no items and the total", p3)
	}

	sent, _ := e.svc.ListSent(ctx, "u03", 1)
	if sent.Total != 1 || sent.Items[0].Other.UserID != "ada" {
		t.Errorf("u03's sent list: %+v, want one request naming ada", sent)
	}

	// Two accepted; the hidden user's connection is left out of the list and the count.
	mustAcceptAs(e, "ada", ids[0])
	mustAcceptAs(e, "ada", ids[1])
	e.store.visible["u01"] = false
	conns, _ := e.svc.ListConnections(ctx, "ada", 1)
	if conns.Total != 1 || len(conns.Items) != 1 || conns.Items[0].Other.UserID != "u00" {
		t.Errorf("ada's connections: %+v, want only u00", conns)
	}
	if v := conns.Items[0].Viewer; v.Status != domain.StatusConnected || v.RequestID != nil {
		t.Errorf("own list item's relation %+v, want connected", v)
	}
	if n, _ := e.svc.CountConnections(ctx, "ada", "ada"); n != 1 {
		t.Errorf("CountConnections = %d, want 1", n)
	}
}

func mustAcceptAs(e env, user, id string) {
	if _, err := e.svc.Accept(ctx, user, id); err != nil {
		panic(err)
	}
}

func TestListUserConnections(t *testing.T) {
	e := newEnv()
	e.store.addUser("dee", false)
	// bob is connected with ada, cy and dee; cy has asked ada, ada is connected with dee.
	for _, u := range []string{"ada", "cy", "dee"} {
		mustAcceptAs(e, "bob", e.send(t, u, "bob").ID)
	}
	pending := e.send(t, "cy", "ada")
	mustAcceptAs(e, "ada", e.send(t, "dee", "ada").ID)

	e.store.linkLists = 0
	got, err := e.svc.ListUserConnections(ctx, "ada", "bob", 1)
	if err != nil || got.Total != 3 {
		t.Fatalf("bob's connections: %+v, %v; want 3", got, err)
	}
	if e.store.linkLists != 1 {
		t.Errorf("the page made %d link lookups, want 1", e.store.linkLists)
	}
	want := map[string]domain.Status{"ada": domain.StatusSelf, "cy": domain.StatusPendingReceived, "dee": domain.StatusConnected}
	for _, c := range got.Items {
		if c.Viewer.Status != want[c.Other.UserID] {
			t.Errorf("%s: %s, want %s", c.Other.UserID, c.Viewer.Status, want[c.Other.UserID])
		}
		if c.Other.UserID == "cy" && (c.Viewer.RequestID == nil || *c.Viewer.RequestID != pending.ID) {
			t.Errorf("cy's request ID %v, want %s", c.Viewer.RequestID, pending.ID)
		}
	}
	for _, u := range []string{"sam", "nobody"} {
		if _, err := e.svc.ListUserConnections(ctx, "ada", u, 1); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("connections of %s: %v, want ErrNotFound", u, err)
		}
	}
}

func TestAcceptReturnsConnected(t *testing.T) {
	e := newEnv()
	c, err := e.svc.Accept(ctx, "bob", e.send(t, "ada", "bob").ID)
	if err != nil || c.Viewer.Status != domain.StatusConnected || c.Viewer.RequestID != nil {
		t.Errorf("Accept = %+v, %v; want the relation connected", c, err)
	}
}

func TestListsRefuseBadPage(t *testing.T) {
	e := newEnv()
	for _, p := range []int{0, -1} {
		if _, err := e.svc.ListConnections(ctx, "ada", p); !errors.Is(err, domain.ErrInvalidPage) {
			t.Errorf("ListConnections page %d: %v", p, err)
		}
		if _, err := e.svc.ListUserConnections(ctx, "bob", "ada", p); !errors.Is(err, domain.ErrInvalidPage) {
			t.Errorf("ListUserConnections page %d: %v", p, err)
		}
		if _, err := e.svc.ListReceived(ctx, "ada", p); !errors.Is(err, domain.ErrInvalidPage) {
			t.Errorf("ListReceived page %d: %v", p, err)
		}
		if _, err := e.svc.ListSent(ctx, "ada", p); !errors.Is(err, domain.ErrInvalidPage) {
			t.Errorf("ListSent page %d: %v", p, err)
		}
	}
}

// A suspended user is refused a connection request even with a valid access token, with no quota
// used (decisions.md, 2026-11-05). A restriction is about messages and does not apply.
func TestSendConnectionRequestSuspended(t *testing.T) {
	e := newEnv()
	reason := "Repeated spam in messages"
	e.actors["ada"] = domain.Standing{Suspended: true, SuspensionReason: &reason}

	_, err := e.svc.Send(ctx, "ada", "bob")
	var refused *domain.SuspendedError
	if !errors.As(err, &refused) || *refused.Reason != reason || refused.Until != nil {
		t.Fatalf("error = %v, want *SuspendedError", err)
	}
	if used, _, _ := e.limiter.Used(ctx, "ada"); used != 0 {
		t.Errorf("requests counted = %d, want 0", used)
	}

	delete(e.actors, "ada")
	if _, err := e.svc.Send(ctx, "ada", "bob"); err != nil {
		t.Errorf("after the suspension: %v", err)
	}
}

// FR-16.6: the member picker's search is 2 to 100 characters after trimming, counted as
// characters (a 100-letter Bangla text passes), and reaches the store trimmed; each item is a
// connection.
func TestSearchConnections(t *testing.T) {
	bn := strings.Repeat("ক", 100)
	for _, tc := range []struct {
		q    string
		want error
	}{
		{"", domain.ErrInvalidQuery},
		{" a ", domain.ErrInvalidQuery},
		{bn + "ক", domain.ErrInvalidQuery},
		{"a\x00b", domain.ErrInvalidQuery},
		{"\xffab", domain.ErrInvalidQuery},
		{bn, nil},
		{" bo ", nil},
	} {
		e := newEnv()
		_, err := e.svc.SearchConnections(ctx, "ada", tc.q, 1)
		if !errors.Is(err, tc.want) {
			t.Errorf("%q: %v, want %v", tc.q, err, tc.want)
		}
		if tc.want == nil && (len(e.store.searched) != 1 || e.store.searched[0] != strings.TrimSpace(tc.q)) {
			t.Errorf("%q: store got %v", tc.q, e.store.searched)
		}
		if tc.want != nil && len(e.store.searched) != 0 {
			t.Errorf("%q: searched anyway", tc.q)
		}
	}
	e := newEnv()
	mustAcceptAs(e, "bob", e.send(t, "ada", "bob").ID)
	mustAcceptAs(e, "cy", e.send(t, "ada", "cy").ID)
	p, err := e.svc.SearchConnections(ctx, "ada", "bo", 1)
	if err != nil || p.Total != 1 || len(p.Items) != 1 || p.Items[0].Other.UserID != "bob" || p.Items[0].Viewer.Status != domain.StatusConnected {
		t.Errorf("search = %+v, %v", p, err)
	}
	if _, err := e.svc.SearchConnections(ctx, "ada", "bo", 0); !errors.Is(err, domain.ErrInvalidPage) {
		t.Errorf("page 0: %v", err)
	}
}
