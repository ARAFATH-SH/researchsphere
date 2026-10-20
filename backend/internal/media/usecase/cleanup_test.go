package usecase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

// fakeQueue is a domain.DeletionQueue in memory: it hands out the oldest rows and keeps
// the ones fn failed for, with their attempts and errors.
type fakeQueue struct {
	mu      sync.Mutex
	rows    []domain.PendingDeletion
	errs    map[string]string
	drains  int
	failErr error // returned by Drain instead of doing anything
}

func newQueue(keys ...string) *fakeQueue {
	q := &fakeQueue{errs: map[string]string{}}
	for i, k := range keys {
		q.rows = append(q.rows, domain.PendingDeletion{ID: fmt.Sprint(i), ObjectKey: k})
	}
	return q
}

func (q *fakeQueue) Drain(ctx context.Context, limit int, fn func(context.Context, domain.PendingDeletion) error) (domain.DrainResult, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.drains++
	if q.failErr != nil {
		return domain.DrainResult{}, q.failErr
	}
	n := min(limit, len(q.rows))
	claimed := q.rows[:n]
	var kept []domain.PendingDeletion
	res := domain.DrainResult{Claimed: n}
	for _, d := range claimed {
		if err := fn(ctx, d); err != nil {
			d.Attempts++
			q.errs[d.ObjectKey] = err.Error()
			kept = append(kept, d)
			res.Failed++
			continue
		}
		res.Deleted++
	}
	// Failed rows go to the back, as last_attempt_at ordering does in PostgreSQL.
	q.rows = append(slices.Clone(q.rows[n:]), kept...)
	return res, nil
}

func (q *fakeQueue) keys() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	var keys []string
	for _, r := range q.rows {
		keys = append(keys, r.ObjectKey)
	}
	return keys
}

// fakeObjects is a domain.ObjectDeleter that records deletions and fails for chosen keys.
type fakeObjects struct {
	mu      sync.Mutex
	deleted []string
	fail    map[string]bool
}

func (o *fakeObjects) Delete(_ context.Context, key string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.fail[key] {
		return errors.New("storage unavailable")
	}
	o.deleted = append(o.deleted, key)
	return nil
}

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestRunOnce(t *testing.T) {
	tests := []struct {
		name        string
		keys        []string
		fail        map[string]bool
		wantDeleted []string
		wantLeft    []string
		wantResult  domain.DrainResult
	}{
		{"empty queue", nil, nil, nil, nil, domain.DrainResult{}},
		{"every object deleted and its row removed", []string{"photo/a", "cover/b"}, nil,
			[]string{"photo/a", "cover/b"}, nil, domain.DrainResult{Claimed: 2, Deleted: 2}},
		{"a failed deletion keeps its row", []string{"photo/a", "photo/b"}, map[string]bool{"photo/b": true},
			[]string{"photo/a"}, []string{"photo/b"}, domain.DrainResult{Claimed: 2, Deleted: 1, Failed: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := newQueue(tt.keys...)
			o := &fakeObjects{fail: tt.fail}
			res, err := NewCleaner(q, o, discard()).RunOnce(t.Context())
			if err != nil {
				t.Fatalf("RunOnce() error = %v", err)
			}
			if res != tt.wantResult {
				t.Errorf("RunOnce() = %+v, want %+v", res, tt.wantResult)
			}
			if !slices.Equal(o.deleted, tt.wantDeleted) {
				t.Errorf("deleted = %v, want %v", o.deleted, tt.wantDeleted)
			}
			if got := q.keys(); !slices.Equal(got, tt.wantLeft) {
				t.Errorf("left in queue = %v, want %v", got, tt.wantLeft)
			}
		})
	}
}

func TestRunOnceRecordsTheFailure(t *testing.T) {
	q := newQueue("photo/a")
	o := &fakeObjects{fail: map[string]bool{"photo/a": true}}
	c := NewCleaner(q, o, discard())
	for range 2 {
		if _, err := c.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if q.rows[0].Attempts != 2 || q.errs["photo/a"] != "storage unavailable" {
		t.Errorf("row = %+v, error %q; want 2 attempts and the error kept", q.rows[0], q.errs["photo/a"])
	}
}

// A backlog larger than one batch clears in one pass.
func TestRunOnceDrainsSeveralBatches(t *testing.T) {
	keys := make([]string, 2*BatchSize+5)
	for i := range keys {
		keys[i] = fmt.Sprintf("photo/%d", i)
	}
	q := newQueue(keys...)
	res, err := NewCleaner(q, &fakeObjects{}, discard()).RunOnce(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != len(keys) || len(q.keys()) != 0 || q.drains != 3 {
		t.Errorf("RunOnce() = %+v after %d drains, %d left; want all %d deleted in 3 drains", res, q.drains, len(q.keys()), len(keys))
	}
}

// A full batch with a failure stops the pass, so a failing object is not retried at once.
func TestRunOnceStopsAfterABatchWithFailures(t *testing.T) {
	keys := make([]string, BatchSize+5)
	for i := range keys {
		keys[i] = fmt.Sprintf("photo/%d", i)
	}
	q := newQueue(keys...)
	res, err := NewCleaner(q, &fakeObjects{fail: map[string]bool{"photo/0": true}}, discard()).RunOnce(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if q.drains != 1 || res.Failed != 1 || res.Deleted != BatchSize-1 {
		t.Errorf("RunOnce() = %+v after %d drains, want one drain with 1 failure", res, q.drains)
	}
}

func TestRunOnceReturnsQueueErrors(t *testing.T) {
	q := newQueue()
	q.failErr = errors.New("database down")
	if _, err := NewCleaner(q, &fakeObjects{}, discard()).RunOnce(t.Context()); !errors.Is(err, q.failErr) {
		t.Errorf("RunOnce() error = %v, want %v", err, q.failErr)
	}
}

// Run works through the queue at once, again on every tick, and returns when ctx ends.
func TestRunStopsWhenContextIsCancelled(t *testing.T) {
	q := newQueue("photo/a")
	var logs syncBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		NewCleaner(q, &fakeObjects{}, logger).Run(ctx, 10*time.Millisecond)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		q.mu.Lock()
		drains := q.drains
		q.mu.Unlock()
		if drains >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d drains in 2s, want ticks every 10ms", drains)
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if len(q.keys()) != 0 {
		t.Errorf("queue = %v, want empty", q.keys())
	}
	if out := logs.String(); !strings.Contains(out, "storage cleanup deleted objects") {
		t.Errorf("logs lack the deletion: %s", out)
	}
}

// syncBuffer lets the test read the log while Run may still write to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
