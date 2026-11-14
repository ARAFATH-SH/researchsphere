package usecase

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/health/domain"
)

// fakePinger is a domain.Pinger whose answer the test chooses.
type fakePinger struct {
	err   error
	hang  bool // wait until the context is cancelled
	delay time.Duration
	calls atomic.Int32
}

func (f *fakePinger) Ping(ctx context.Context) error {
	f.calls.Add(1)
	if f.hang {
		<-ctx.Done()
		return ctx.Err()
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	return f.err
}

var errRefused = errors.New("connection refused")

func TestCheck(t *testing.T) {
	tests := []struct {
		name        string
		db, redis   *fakePinger
		storage     *fakePinger
		wantHealthy bool
		wantDown    []string
	}{
		{"all up", &fakePinger{}, &fakePinger{}, &fakePinger{}, true, nil},
		{"database down", &fakePinger{err: errRefused}, &fakePinger{}, &fakePinger{}, false, []string{"database"}},
		{"redis down", &fakePinger{}, &fakePinger{err: errRefused}, &fakePinger{}, false, []string{"redis"}},
		{"storage down", &fakePinger{}, &fakePinger{}, &fakePinger{err: errRefused}, false, []string{"storage"}},
		{"all down, in order", &fakePinger{err: errRefused}, &fakePinger{err: errRefused}, &fakePinger{err: errRefused}, false, []string{"database", "redis", "storage"}},
		{"database hangs past the timeout", &fakePinger{hang: true}, &fakePinger{}, &fakePinger{}, false, []string{"database"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New(50*time.Millisecond,
				domain.Dependency{Name: "database", Pinger: tt.db},
				domain.Dependency{Name: "redis", Pinger: tt.redis},
				domain.Dependency{Name: "storage", Pinger: tt.storage},
			)
			report := c.Check(t.Context())

			if got := report.Healthy(); got != tt.wantHealthy {
				t.Errorf("Healthy() = %v, want %v", got, tt.wantHealthy)
			}
			if got := report.Down(); !slices.Equal(got, tt.wantDown) {
				t.Errorf("Down() = %v, want %v", got, tt.wantDown)
			}
			if len(report.Results) != 3 || report.Results[0].Name != "database" || report.Results[1].Name != "redis" ||
				report.Results[2].Name != "storage" {
				t.Errorf("Results = %v, want database, redis, storage", report.Results)
			}
			if tt.db.calls.Load() != 1 || tt.redis.calls.Load() != 1 || tt.storage.calls.Load() != 1 {
				t.Errorf("pings = %d, %d, %d; want each pinged once", tt.db.calls.Load(), tt.redis.calls.Load(), tt.storage.calls.Load())
			}
		})
	}
}

func TestCheckKeepsTheError(t *testing.T) {
	c := New(time.Second, domain.Dependency{Name: "database", Pinger: &fakePinger{err: errRefused}})
	report := c.Check(t.Context())
	if !errors.Is(report.Results[0].Err, errRefused) {
		t.Errorf("Err = %v, want %v", report.Results[0].Err, errRefused)
	}
}

func TestCheckPingsAtTheSameTime(t *testing.T) {
	const delay = 200 * time.Millisecond
	c := New(time.Second,
		domain.Dependency{Name: "database", Pinger: &fakePinger{delay: delay}},
		domain.Dependency{Name: "redis", Pinger: &fakePinger{delay: delay}},
	)
	start := time.Now()
	c.Check(t.Context())
	if elapsed := time.Since(start); elapsed >= 2*delay {
		t.Errorf("Check took %v; two %v pings in parallel should take less than %v", elapsed, delay, 2*delay)
	}
}

func TestNewDefaultsTheTimeout(t *testing.T) {
	if c := New(0); c.timeout != DefaultTimeout {
		t.Errorf("timeout = %v, want %v", c.timeout, DefaultTimeout)
	}
}

// An optional dependency (the AI service) that does not respond leaves the report healthy
// and degraded (NFR-10.2); a required one still makes it unhealthy.
func TestCheckOptionalDependency(t *testing.T) {
	tests := []struct {
		name         string
		db, ai       *fakePinger
		wantHealthy  bool
		wantDown     []string
		wantDegraded []string
		wantAI       bool
	}{
		{"all up", &fakePinger{}, &fakePinger{}, true, nil, nil, true},
		{"ai down", &fakePinger{}, &fakePinger{err: errRefused}, true, nil, []string{"ai"}, false},
		{"ai hangs past the timeout", &fakePinger{}, &fakePinger{hang: true}, true, nil, []string{"ai"}, false},
		{"database and ai down", &fakePinger{err: errRefused}, &fakePinger{err: errRefused}, false, []string{"database"}, []string{"ai"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := New(50*time.Millisecond,
				domain.Dependency{Name: "database", Pinger: tt.db},
				domain.Dependency{Name: "ai", Pinger: tt.ai, Optional: true},
			).Check(t.Context())

			if got := report.Healthy(); got != tt.wantHealthy {
				t.Errorf("Healthy() = %v, want %v", got, tt.wantHealthy)
			}
			if got := report.Down(); !slices.Equal(got, tt.wantDown) {
				t.Errorf("Down() = %v, want %v", got, tt.wantDown)
			}
			if got := report.Degraded(); !slices.Equal(got, tt.wantDegraded) {
				t.Errorf("Degraded() = %v, want %v", got, tt.wantDegraded)
			}
			if got := report.Responded("ai"); got != tt.wantAI {
				t.Errorf("Responded(ai) = %v, want %v", got, tt.wantAI)
			}
			if report.Responded("missing") {
				t.Error("Responded(missing) = true, want false for a name the report does not hold")
			}
		})
	}
}
