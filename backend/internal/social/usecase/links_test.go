package usecase

import (
	"context"
	"reflect"
	"testing"
)

// fakeLinks records each LinkChanged pair, and whether it came inside a transaction.
type fakeLinks struct {
	tx        *fakeTx
	pairs     []string
	outsideTx int
}

func (l *fakeLinks) LinkChanged(_ context.Context, a, b string) {
	if l.tx.depth == 0 {
		l.outsideTx++
	}
	l.pairs = append(l.pairs, a+"|"+b)
}

func watched(t *testing.T) (env, *fakeLinks) {
	t.Helper()
	e := newEnv()
	links := &fakeLinks{tx: e.tx}
	e.svc.WatchLinks(links)
	return e, links
}

// Every change to the link between two users names both of them (AI-2.1: both
// recommendation lists depend on it); changes made in a transaction tell it inside.
func TestLinkChangedOnEveryChange(t *testing.T) {
	tests := []struct {
		name      string
		change    func(t *testing.T, e env)
		want      []string
		inTxCalls bool
	}{
		{"request sent", func(t *testing.T, e env) { mustSend(e, "ada", "bob") }, []string{"ada|bob"}, true},
		{"request accepted", func(t *testing.T, e env) {
			r := mustSend(e, "ada", "bob")
			if _, err := e.svc.Accept(ctx, "bob", r.ID); err != nil {
				t.Fatal(err)
			}
		}, []string{"ada|bob", "ada|bob"}, true},
		{"request rejected", func(t *testing.T, e env) {
			r := mustSend(e, "ada", "bob")
			if err := e.svc.Reject(ctx, "bob", r.ID); err != nil {
				t.Fatal(err)
			}
		}, []string{"ada|bob", "bob|ada"}, true},
		{"request cancelled", func(t *testing.T, e env) {
			r := mustSend(e, "ada", "bob")
			if err := e.svc.Cancel(ctx, "ada", r.ID); err != nil {
				t.Fatal(err)
			}
		}, []string{"ada|bob", "ada|bob"}, true},
		{"connection removed", func(t *testing.T, e env) {
			r := mustSend(e, "ada", "bob")
			if _, err := e.svc.Accept(ctx, "bob", r.ID); err != nil {
				t.Fatal(err)
			}
			if err := e.svc.Remove(ctx, "bob", "ada"); err != nil {
				t.Fatal(err)
			}
		}, []string{"ada|bob", "ada|bob", "bob|ada"}, false},
		{"blocked", func(t *testing.T, e env) {
			if err := e.svc.Block(ctx, "ada", "cy"); err != nil {
				t.Fatal(err)
			}
		}, []string{"ada|cy"}, true},
		{"unblocked", func(t *testing.T, e env) {
			if err := e.svc.Block(ctx, "ada", "cy"); err != nil {
				t.Fatal(err)
			}
			if err := e.svc.Unblock(ctx, "ada", "cy"); err != nil {
				t.Fatal(err)
			}
		}, []string{"ada|cy", "ada|cy"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, links := watched(t)
			tt.change(t, e)
			if !reflect.DeepEqual(links.pairs, tt.want) {
				t.Errorf("pairs %v, want %v", links.pairs, tt.want)
			}
			if tt.inTxCalls && links.outsideTx != 0 {
				t.Errorf("%d calls outside the transaction, want 0", links.outsideTx)
			}
		})
	}
}

// A refused change tells nobody, and a change rolled back has told only inside its
// transaction (the cmd/api watcher acts after the commit, so it then does nothing).
func TestLinkChangedNotOnRefusedChange(t *testing.T) {
	e, links := watched(t)
	r := mustSend(e, "ada", "bob")
	links.pairs = nil
	refused := []func() error{
		func() error { _, err := e.svc.Send(ctx, "ada", "bob"); return err },  // already pending
		func() error { _, err := e.svc.Send(ctx, "ada", "ada"); return err },  // self
		func() error { _, err := e.svc.Accept(ctx, "ada", r.ID); return err }, // not the receiver
		func() error { return e.svc.Reject(ctx, "cy", r.ID) },                 // not the receiver
		func() error { return e.svc.Cancel(ctx, "bob", r.ID) },                // not the sender
		func() error { return e.svc.Remove(ctx, "ada", "cy") },                // not connected
		func() error { return e.svc.Block(ctx, "ada", "ada") },                // self
		func() error { _, err := e.svc.Send(ctx, "ada", "sam"); return err },  // hidden user
	}
	for i, f := range refused {
		if err := f(); err == nil {
			t.Fatalf("change %d was not refused", i)
		}
	}
	if len(links.pairs) != 0 {
		t.Errorf("refused changes told %v, want nothing", links.pairs)
	}

	// The second block of the same pair adds nothing and tells nothing.
	if err := e.svc.Block(ctx, "cy", "bob"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Block(ctx, "cy", "bob"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(links.pairs, []string{"cy|bob"}) {
		t.Errorf("pairs %v after blocking twice, want one", links.pairs)
	}
}
