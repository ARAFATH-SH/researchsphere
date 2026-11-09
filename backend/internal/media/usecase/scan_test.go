package usecase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

// fakeScanQueue keeps pending files in memory with the lease rules of the real queue: a write
// with a token that is not the current one changes nothing.
type fakeScanQueue struct {
	jobs    []domain.ScanJob // pending and claimable, in order
	status  map[string]domain.ScanStatus
	tries   map[string]int
	delays  map[string][]time.Duration
	codes   map[string]string
	queued  []string // object keys queued for deletion
	lost    map[string]bool
	claims  int
	seq     int
	failMk  error
	pending map[string]domain.ScanJob // claimed, by file
}

func newFakeScanQueue(files ...string) *fakeScanQueue {
	q := &fakeScanQueue{status: map[string]domain.ScanStatus{}, tries: map[string]int{}, delays: map[string][]time.Duration{},
		codes: map[string]string{}, lost: map[string]bool{}, pending: map[string]domain.ScanJob{}}
	for _, f := range files {
		q.jobs = append(q.jobs, domain.ScanJob{FileID: f, OwnerID: "owner-" + f, ObjectKey: "key/" + f})
		q.status[f] = domain.ScanPending
	}
	return q
}

func (q *fakeScanQueue) ClaimScans(_ context.Context, limit int, lease time.Duration) ([]domain.ScanJob, error) {
	q.claims++
	if lease != ScanLease {
		return nil, fmt.Errorf("lease %v", lease)
	}
	var out []domain.ScanJob
	for len(q.jobs) > 0 && len(out) < limit {
		j := q.jobs[0]
		q.jobs = q.jobs[1:]
		q.seq++
		j.Token, j.Attempts = fmt.Sprintf("token-%d", q.seq), q.tries[j.FileID]
		q.pending[j.FileID] = j
		out = append(out, j)
	}
	return out, nil
}

func (q *fakeScanQueue) held(job domain.ScanJob) bool {
	cur, ok := q.pending[job.FileID]
	return ok && cur.Token == job.Token && !q.lost[job.FileID]
}

func (q *fakeScanQueue) end(job domain.ScanJob, st domain.ScanStatus) (bool, error) {
	if q.failMk != nil {
		return false, q.failMk
	}
	if !q.held(job) {
		return false, nil
	}
	delete(q.pending, job.FileID)
	q.status[job.FileID] = st
	return true, nil
}

func (q *fakeScanQueue) MarkClean(_ context.Context, job domain.ScanJob) (bool, error) {
	return q.end(job, domain.ScanClean)
}

func (q *fakeScanQueue) MarkInfected(_ context.Context, job domain.ScanJob) (bool, error) {
	ok, err := q.end(job, domain.ScanInfected)
	if ok {
		q.queued = append(q.queued, job.ObjectKey)
	}
	return ok, err
}

func (q *fakeScanQueue) MarkFailed(_ context.Context, job domain.ScanJob, code string) (bool, error) {
	ok, err := q.end(job, domain.ScanFailed)
	if ok {
		q.tries[job.FileID]++
		q.codes[job.FileID] = code
	}
	return ok, err
}

func (q *fakeScanQueue) RetryLater(_ context.Context, job domain.ScanJob, delay time.Duration, counted bool, code string) (bool, error) {
	if !q.held(job) {
		return false, nil
	}
	delete(q.pending, job.FileID)
	if counted {
		q.tries[job.FileID]++
		q.codes[job.FileID] = code
	}
	q.delays[job.FileID] = append(q.delays[job.FileID], delay)
	q.jobs = append(q.jobs, domain.ScanJob{FileID: job.FileID, OwnerID: job.OwnerID, ObjectKey: job.ObjectKey})
	return true, nil
}

// fakeScanner answers by the content of the file it is given.
type fakeScanner struct {
	down    bool // Ping and Scan fail as unreachable
	scanned []string
}

func (s *fakeScanner) Ping(context.Context) error {
	if s.down {
		return fmt.Errorf("%w: connection refused", domain.ErrScannerUnavailable)
	}
	return nil
}

func (s *fakeScanner) Scan(_ context.Context, r io.Reader) (domain.ScanVerdict, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return domain.ScanVerdict{}, fmt.Errorf("read: %w", err)
	}
	s.scanned = append(s.scanned, string(b))
	if s.down {
		return domain.ScanVerdict{}, fmt.Errorf("%w: reset", domain.ErrScannerUnavailable)
	}
	switch {
	case strings.Contains(string(b), "EICAR"):
		return domain.ScanVerdict{Infected: true, Signature: "Eicar-Test-Signature"}, nil
	case strings.Contains(string(b), "TOO-BIG"):
		return domain.ScanVerdict{}, &domain.ScanFileError{Reason: "INSTREAM size limit exceeded."}
	}
	return domain.ScanVerdict{}, nil
}

// fakeScanObjects serves each key's content, and records deletions.
type fakeScanObjects struct {
	content map[string]string
	failGet error
	deleted []string
}

func (o *fakeScanObjects) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if o.failGet != nil {
		return nil, o.failGet
	}
	c, ok := o.content[key]
	if !ok {
		return nil, domain.ErrObjectMissing
	}
	return io.NopCloser(strings.NewReader(c)), nil
}

func (o *fakeScanObjects) Delete(_ context.Context, key string) error {
	o.deleted = append(o.deleted, key)
	return nil
}

type fakeListener struct {
	infected   []string
	scanned    []string
	failInfect error
}

func (l *fakeListener) Infected(_ context.Context, fileID, ownerID string) error {
	if l.failInfect != nil {
		return l.failInfect
	}
	l.infected = append(l.infected, fileID+" "+ownerID)
	return nil
}

func (l *fakeListener) Scanned(_ context.Context, fileID string, st domain.ScanStatus) {
	l.scanned = append(l.scanned, fileID+" "+string(st))
}

type passTx struct{}

func (passTx) InTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

type scanFixture struct {
	queue    *fakeScanQueue
	scanner  *fakeScanner
	objects  *fakeScanObjects
	listener *fakeListener
	logs     *bytes.Buffer
	worker   *ScanWorker
}

func newScanFixture(content map[string]string) *scanFixture {
	files := make([]string, 0, len(content))
	for k := range content {
		files = append(files, k)
	}
	slices.Sort(files)
	objects := map[string]string{}
	for f, c := range content {
		objects["key/"+f] = c
	}
	f := &scanFixture{queue: newFakeScanQueue(files...), scanner: &fakeScanner{}, objects: &fakeScanObjects{content: objects},
		listener: &fakeListener{}, logs: &bytes.Buffer{}}
	f.worker = NewScanWorker(f.queue, passTx{}, f.objects, f.objects, f.scanner, f.listener, slog.New(slog.NewTextHandler(f.logs, nil)))
	return f
}

// FR-16.7: a clean file becomes clean; malware is marked infected, its object deleted at once
// and queued as a backup, and the owner told in the same transaction.
func TestScanCleanAndInfected(t *testing.T) {
	f := newScanFixture(map[string]string{"a": "hello", "b": "X5O!P%@AP EICAR-STANDARD-ANTIVIRUS-TEST-FILE"})
	if n, err := f.worker.RunOnce(t.Context()); err != nil || n != 2 {
		t.Fatalf("RunOnce = %d, %v", n, err)
	}
	if f.queue.status["a"] != domain.ScanClean || f.queue.status["b"] != domain.ScanInfected {
		t.Errorf("status = %v", f.queue.status)
	}
	if !slices.Equal(f.queue.queued, []string{"key/b"}) || !slices.Equal(f.objects.deleted, []string{"key/b"}) {
		t.Errorf("queued %v, deleted %v", f.queue.queued, f.objects.deleted)
	}
	if !slices.Equal(f.listener.infected, []string{"b owner-b"}) || !slices.Equal(f.listener.scanned, []string{"a clean", "b infected"}) {
		t.Errorf("infected %v, scanned %v", f.listener.infected, f.listener.scanned)
	}
	if !strings.Contains(f.logs.String(), "Eicar-Test-Signature") {
		t.Errorf("the signature should be logged: %s", f.logs)
	}
	if n, _ := f.worker.RunOnce(t.Context()); n != 0 || len(f.scanner.scanned) != 2 {
		t.Errorf("a finished file was scanned again: %d, %v", n, f.scanner.scanned)
	}
}

// If the owner cannot be told, the file stays pending and its object is kept.
func TestScanInfectedNotifyFails(t *testing.T) {
	f := newScanFixture(map[string]string{"b": "EICAR"})
	f.listener.failInfect = errors.New("db down")
	if _, err := f.worker.RunOnce(t.Context()); !errors.Is(err, f.listener.failInfect) {
		t.Fatalf("err = %v", err)
	}
	if len(f.objects.deleted) != 0 || len(f.listener.scanned) != 0 {
		t.Errorf("deleted %v, scanned %v", f.objects.deleted, f.listener.scanned)
	}
}

// An error about the file counts: 30 s, 1, 2 and 4 minutes later it is tried again, and the
// fifth error ends the scan as failed.
func TestScanFileErrorRetriesThenFails(t *testing.T) {
	f := newScanFixture(map[string]string{"big": "TOO-BIG"})
	for i := range MaxScanTries {
		if n, err := f.worker.RunOnce(t.Context()); err != nil || n != 1 {
			t.Fatalf("try %d: %d, %v", i+1, n, err)
		}
	}
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute}
	if !slices.Equal(f.queue.delays["big"], want) {
		t.Errorf("delays = %v, want %v", f.queue.delays["big"], want)
	}
	if f.queue.status["big"] != domain.ScanFailed || f.queue.tries["big"] != MaxScanTries || f.queue.codes["big"] != domain.ScanErrorScanner {
		t.Errorf("status %s, tries %d, code %s", f.queue.status["big"], f.queue.tries["big"], f.queue.codes["big"])
	}
	if !slices.Equal(f.listener.scanned, []string{"big failed"}) {
		t.Errorf("scanned = %v", f.listener.scanned)
	}
}

// A file whose object is gone fails at once.
func TestScanObjectMissing(t *testing.T) {
	f := newScanFixture(map[string]string{"gone": "x"})
	delete(f.objects.content, "key/gone")
	if _, err := f.worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.queue.status["gone"] != domain.ScanFailed || f.queue.codes["gone"] != domain.ScanErrorMissing {
		t.Errorf("status %s, code %s", f.queue.status["gone"], f.queue.codes["gone"])
	}
}

// The scanner being away is not the file's fault: no try is used, nothing is claimed while it
// does not answer, the wait doubles up to a minute, and one warning covers the whole outage.
func TestScanScannerUnreachable(t *testing.T) {
	f := newScanFixture(map[string]string{"a": "hello", "b": "world"})
	f.scanner.down = true
	var waits []time.Duration
	for range 8 {
		waits = append(waits, f.worker.Step(t.Context()))
	}
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, time.Minute, time.Minute, time.Minute}
	if !slices.Equal(waits, want) {
		t.Errorf("waits = %v, want %v", waits, want)
	}
	if f.queue.claims != 0 || f.queue.tries["a"] != 0 {
		t.Errorf("claimed %d, tries %v", f.queue.claims, f.queue.tries)
	}
	if n := strings.Count(f.logs.String(), "malware scanner unreachable"); n != 1 {
		t.Errorf("%d warnings, want 1:\n%s", n, f.logs)
	}
	f.scanner.down = false
	if wait := f.worker.Step(t.Context()); wait != 0 {
		t.Errorf("wait after a batch = %v", wait)
	}
	if !strings.Contains(f.logs.String(), "malware scanner reachable again") || f.queue.status["a"] != domain.ScanClean {
		t.Errorf("logs %s, status %v", f.logs, f.queue.status)
	}
	if wait := f.worker.Step(t.Context()); wait != ScanPoll {
		t.Errorf("idle wait = %v", wait)
	}
	// A second outage warns again, once.
	f.scanner.down = true
	f.worker.Step(t.Context())
	f.worker.Step(t.Context())
	if n := strings.Count(f.logs.String(), "malware scanner unreachable"); n != 2 {
		t.Errorf("%d warnings, want 2", n)
	}
}

// When the scanner goes away in the middle of a batch, the file and the rest of the batch are
// given back without using a try.
func TestScanScannerDropsMidBatch(t *testing.T) {
	f := newScanFixture(map[string]string{"a": "hello", "b": "world"})
	f.worker.scanner = &dropAfterPing{f.scanner}
	if wait := f.worker.Step(t.Context()); wait != UnreachableBackoffMin {
		t.Errorf("wait = %v", wait)
	}
	if f.queue.tries["a"] != 0 || f.queue.tries["b"] != 0 || len(f.queue.jobs) != 2 || len(f.queue.pending) != 0 {
		t.Errorf("tries %v, back in queue %d, held %d", f.queue.tries, len(f.queue.jobs), len(f.queue.pending))
	}
	if f.queue.status["a"] != domain.ScanPending {
		t.Errorf("status = %v", f.queue.status)
	}
}

// dropAfterPing answers the ping, then breaks off every scan.
type dropAfterPing struct{ *fakeScanner }

func (d *dropAfterPing) Scan(context.Context, io.Reader) (domain.ScanVerdict, error) {
	return domain.ScanVerdict{}, fmt.Errorf("%w: connection reset", domain.ErrScannerUnavailable)
}

// The bucket failing is not the file's fault either.
func TestScanStorageUnavailable(t *testing.T) {
	f := newScanFixture(map[string]string{"a": "hello"})
	f.objects.failGet = errors.New("minio down")
	if _, err := f.worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.queue.tries["a"] != 0 || f.queue.status["a"] != domain.ScanPending || len(f.queue.jobs) != 1 {
		t.Errorf("tries %v, status %v", f.queue.tries, f.queue.status)
	}
}

// A worker whose lease ran out (another took the file) writes nothing and tells nobody.
func TestScanLostLease(t *testing.T) {
	f := newScanFixture(map[string]string{"a": "hello", "b": "EICAR"})
	f.queue.lost["a"], f.queue.lost["b"] = true, true
	if _, err := f.worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.queue.status["a"] != domain.ScanPending || f.queue.status["b"] != domain.ScanPending {
		t.Errorf("status = %v", f.queue.status)
	}
	if len(f.listener.scanned) != 0 || len(f.listener.infected) != 0 || len(f.objects.deleted) != 0 {
		t.Errorf("scanned %v, infected %v, deleted %v", f.listener.scanned, f.listener.infected, f.objects.deleted)
	}
}
