//go:build integration

package postgres

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// pendingFiles stores n pending files (as attachments are, before their scan) and returns
// their IDs in key order.
func pendingFiles(t *testing.T, pool *pgxpool.Pool, n int) []string {
	t.Helper()
	keys := uniqueKeys(n)
	owner := createUserWithFiles(t, pool)
	ids := make([]string, n)
	for i, k := range keys {
		if err := pool.QueryRow(t.Context(), `INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes)
			VALUES ($1, 'attachment', $2, 'application/pdf', 10) RETURNING id::text`, owner, k).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	return ids
}

func scanState(t *testing.T, pool *pgxpool.Pool, id string) (status string, attempts int, code *string, leased bool) {
	t.Helper()
	if err := pool.QueryRow(t.Context(), `SELECT scan_status, scan_attempts, scan_error, scan_lease_token IS NOT NULL
		FROM stored_files WHERE id = $1`, id).Scan(&status, &attempts, &code, &leased); err != nil {
		t.Fatal(err)
	}
	return status, attempts, code, leased
}

// FR-16.7: two workers claiming at the same moment never get the same file, and a leased file
// is not claimed again until its lease runs out.
func TestClaimScansIsExclusive(t *testing.T) {
	s, pool := newTestStore(t)
	ids := pendingFiles(t, pool, 24) // uniqueKeys names files a to z
	var (
		mu    sync.Mutex
		got   []string
		wg    sync.WaitGroup
		start = make(chan struct{})
	)
	for range 4 {
		wg.Go(func() {
			<-start
			for {
				jobs, err := s.ClaimScans(context.Background(), 3, time.Minute)
				if err != nil {
					t.Error(err)
					return
				}
				if len(jobs) == 0 {
					return
				}
				mu.Lock()
				for _, j := range jobs {
					got = append(got, j.FileID)
				}
				mu.Unlock()
			}
		})
	}
	close(start)
	wg.Wait()
	slices.Sort(got)
	want := slices.Clone(ids)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("claimed %d files (%d distinct wanted): duplicates or misses", len(got), len(want))
	}
	if jobs, err := s.ClaimScans(t.Context(), 10, time.Minute); err != nil || len(jobs) != 0 {
		t.Errorf("leased files claimed again: %d, %v", len(jobs), err)
	}
	// A lease that ran out is claimable again, with a new token.
	if _, err := pool.Exec(t.Context(), "UPDATE stored_files SET scan_lease_until = now() - interval '1 second' WHERE id = $1", ids[0]); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.ClaimScans(t.Context(), 10, time.Minute)
	if err != nil || len(jobs) != 1 || jobs[0].FileID != ids[0] || jobs[0].Token == "" || jobs[0].ObjectKey == "" || jobs[0].OwnerID == "" {
		t.Errorf("expired lease: %+v, %v", jobs, err)
	}
}

// A result is written only with the current lease: a worker whose lease was taken over changes
// nothing.
func TestScanResultNeedsTheLease(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ids := pendingFiles(t, pool, 1)
	first, err := s.ClaimScans(ctx, 1, time.Minute)
	if err != nil || len(first) != 1 {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE stored_files SET scan_lease_until = now() - interval '1 second' WHERE id = $1", ids[0]); err != nil {
		t.Fatal(err)
	}
	second, err := s.ClaimScans(ctx, 1, time.Minute)
	if err != nil || len(second) != 1 || second[0].Token == first[0].Token {
		t.Fatalf("second claim: %+v %v", second, err)
	}
	for name, mark := range map[string]func() (bool, error){
		"clean":    func() (bool, error) { return s.MarkClean(ctx, first[0]) },
		"infected": func() (bool, error) { return s.MarkInfected(ctx, first[0]) },
		"failed":   func() (bool, error) { return s.MarkFailed(ctx, first[0], domain.ScanErrorScanner) },
		"retry":    func() (bool, error) { return s.RetryLater(ctx, first[0], time.Minute, true, domain.ScanErrorScanner) },
	} {
		if ok, err := mark(); err != nil || ok {
			t.Errorf("%s with a lost lease: %v, %v", name, ok, err)
		}
	}
	if status, attempts, _, _ := scanState(t, pool, ids[0]); status != "pending" || attempts != 0 {
		t.Errorf("status %s, attempts %d", status, attempts)
	}
	if ok, err := s.MarkClean(ctx, second[0]); err != nil || !ok {
		t.Fatalf("current lease: %v, %v", ok, err)
	}
	if status, _, _, leased := scanState(t, pool, ids[0]); status != "clean" || leased {
		t.Errorf("status %s, leased %v", status, leased)
	}
	if ok, _ := s.MarkClean(ctx, second[0]); ok {
		t.Error("a finished scan was written again")
	}
}

// Infected: the row stays as a record and its object is queued for deletion in the same
// statement, also inside a transaction that then commits.
func TestMarkInfectedQueuesTheObject(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ids := pendingFiles(t, pool, 1)
	jobs, err := s.ClaimScans(ctx, 1, time.Minute)
	if err != nil || len(jobs) != 1 {
		t.Fatal(err)
	}
	if err := postgres.NewTransactor(pool).InTx(ctx, func(ctx context.Context) error {
		ok, err := s.MarkInfected(ctx, jobs[0])
		if err == nil && !ok {
			t.Error("not marked")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if status, _, _, _ := scanState(t, pool, ids[0]); status != "infected" {
		t.Errorf("status = %s", status)
	}
	if q := queueRows(t, pool, []string{jobs[0].ObjectKey}); len(q) != 1 {
		t.Errorf("queued = %v", q)
	}
}

// Retries: a counted try keeps the error code and makes the file due later; a try that does not
// count changes neither.
func TestRetryScanLater(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ids := pendingFiles(t, pool, 1)
	jobs, _ := s.ClaimScans(ctx, 1, time.Minute)
	if ok, err := s.RetryLater(ctx, jobs[0], time.Hour, true, domain.ScanErrorScanner); err != nil || !ok {
		t.Fatalf("retry: %v %v", ok, err)
	}
	status, attempts, code, leased := scanState(t, pool, ids[0])
	if status != "pending" || attempts != 1 || code == nil || *code != domain.ScanErrorScanner || leased {
		t.Errorf("after a counted try: %s %d %v %v", status, attempts, code, leased)
	}
	if jobs, _ := s.ClaimScans(ctx, 1, time.Minute); len(jobs) != 0 {
		t.Error("a file was claimed before it was due")
	}
	if _, err := pool.Exec(ctx, "UPDATE stored_files SET scan_next_at = now() WHERE id = $1", ids[0]); err != nil {
		t.Fatal(err)
	}
	jobs, _ = s.ClaimScans(ctx, 1, time.Minute)
	if len(jobs) != 1 || jobs[0].Attempts != 1 {
		t.Fatalf("reclaim: %+v", jobs)
	}
	if ok, err := s.RetryLater(ctx, jobs[0], 0, false, ""); err != nil || !ok {
		t.Fatal(err)
	}
	if _, attempts, code, _ := scanState(t, pool, ids[0]); attempts != 1 || code == nil {
		t.Errorf("an uncounted try changed attempts %d or code %v", attempts, code)
	}
	jobs, _ = s.ClaimScans(ctx, 1, time.Minute)
	if ok, err := s.MarkFailed(ctx, jobs[0], domain.ScanErrorMissing); err != nil || !ok {
		t.Fatal(err)
	}
	if status, attempts, code, _ := scanState(t, pool, ids[0]); status != "failed" || attempts != 2 || *code != domain.ScanErrorMissing {
		t.Errorf("failed: %s %d %v", status, attempts, *code)
	}
}

// Profile images are stored clean and are never claimed.
func TestCleanFilesAreNotClaimed(t *testing.T) {
	s, pool := newTestStore(t)
	createUserWithFiles(t, pool, uniqueKeys(2)...)
	if _, err := pool.Exec(t.Context(), "UPDATE stored_files SET scan_status = 'clean'"); err != nil {
		t.Fatal(err)
	}
	if jobs, err := s.ClaimScans(t.Context(), 10, time.Minute); err != nil || len(jobs) != 0 {
		t.Errorf("claimed %d, %v", len(jobs), err)
	}
}
