// Package usecase holds the health check's logic: ping every dependency at the same
// time, each under a timeout. It imports only the health domain.
package usecase

import (
	"context"
	"sync"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/health/domain"
)

// DefaultTimeout bounds each ping, so a hung service cannot hang the health check.
const DefaultTimeout = 2 * time.Second

// Checker pings the dependencies of the API.
type Checker struct {
	deps    []domain.Dependency
	timeout time.Duration
}

// New returns a Checker for deps. A timeout of 0 means DefaultTimeout.
func New(timeout time.Duration, deps ...domain.Dependency) *Checker {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Checker{deps: deps, timeout: timeout}
}

// Check pings every dependency at the same time and returns one result per dependency,
// in the order they were given.
func (c *Checker) Check(ctx context.Context) domain.Report {
	results := make([]domain.Result, len(c.deps))
	var wg sync.WaitGroup
	for i, d := range c.deps {
		wg.Go(func() {
			pctx, cancel := context.WithTimeout(ctx, c.timeout)
			defer cancel()
			results[i] = domain.Result{Name: d.Name, Optional: d.Optional, Err: d.Pinger.Ping(pctx)}
		})
	}
	wg.Wait()
	return domain.Report{Results: results}
}
