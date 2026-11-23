package usecase

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/compile/domain"
)

// memStore keeps jobs in memory with the rules the database enforces: one active job per
// project, and the retention of Retain.
type memStore struct {
	mu       sync.Mutex
	next     int
	jobs     map[string]*memJob
	projects map[string]memProject
	members  map[string][]string
	paths    map[string][]string
	now      func() time.Time
}

type memJob struct {
	domain.Job
	log      *string
	pdfKey   string
	worker   string
	leaseEnd time.Time
}

type memProject struct {
	main, engine string
	exists       bool
}

func newMemStore() *memStore {
	return &memStore{jobs: map[string]*memJob{}, projects: map[string]memProject{}, members: map[string][]string{},
		paths: map[string][]string{}, now: time.Now}
}

func (m *memStore) Project(_ context.Context, projectID string) (string, string, bool, error) {
	p, ok := m.projects[projectID]
	if !ok {
		return "", "", false, domain.ErrNotFound
	}
	return p.main, p.engine, p.exists, nil
}

func (m *memStore) QueuedCount(context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, j := range m.jobs {
		if j.Status == domain.StatusQueued {
			n++
		}
	}
	return n, nil
}

func (m *memStore) Insert(_ context.Context, nj domain.NewJob) (domain.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.ProjectID == nj.ProjectID && j.Status.Active() {
			return domain.Job{}, &domain.RunningError{ID: j.ID}
		}
	}
	m.next++
	by := nj.RequestedBy
	j := &memJob{Job: domain.Job{ID: "job" + strconv.Itoa(m.next), ProjectID: nj.ProjectID, RequestedBy: &by,
		Status: domain.StatusQueued, Engine: nj.Engine, MainFilePath: nj.MainFilePath,
		QueuedAt: m.now().Add(time.Duration(m.next) * time.Millisecond), LiveTextConfirmed: true}}
	m.jobs[j.ID] = j
	return m.view(j), nil
}

func (m *memStore) view(j *memJob) domain.Job {
	out := j.Job
	out.HasLog, out.HasPDF = j.log != nil, j.pdfKey != ""
	return out
}

func (m *memStore) sorted(projectID string) []*memJob {
	var out []*memJob
	for _, j := range m.jobs {
		if j.ProjectID == projectID {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].QueuedAt.After(out[b].QueuedAt) })
	return out
}

func (m *memStore) Latest(_ context.Context, projectID string) (domain.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	js := m.sorted(projectID)
	if len(js) == 0 {
		return domain.Job{}, domain.ErrNoCompile
	}
	return m.view(js[0]), nil
}

func (m *memStore) Get(_ context.Context, projectID, jobID string) (domain.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok || j.ProjectID != projectID {
		return domain.Job{}, domain.ErrNotFound
	}
	return m.view(j), nil
}

func (m *memStore) Log(_ context.Context, projectID, jobID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok || j.ProjectID != projectID {
		return "", domain.ErrNotFound
	}
	if j.log == nil {
		return "", domain.ErrNoLog
	}
	return *j.log, nil
}

func (m *memStore) LatestPDF(_ context.Context, projectID string) (domain.PDF, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.sorted(projectID) {
		if j.Status == domain.StatusSucceeded && j.pdfKey != "" {
			return domain.PDF{CompileID: j.ID, ObjectKey: j.pdfKey, Title: "Graph learning"}, nil
		}
	}
	return domain.PDF{}, domain.ErrNoPDF
}

func (m *memStore) LatestPDFJob(ctx context.Context, projectID string) (string, error) {
	p, err := m.LatestPDF(ctx, projectID)
	if err != nil {
		return "", nil //nolint:nilerr // none is the empty ID
	}
	return p.CompileID, nil
}

func (m *memStore) Claim(_ context.Context, workerID string, lease time.Duration) (domain.Claimed, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var oldest *memJob
	for _, j := range m.jobs {
		if j.Status == domain.StatusQueued && (oldest == nil || j.QueuedAt.Before(oldest.QueuedAt)) {
			oldest = j
		}
	}
	if oldest == nil {
		return domain.Claimed{}, false, nil
	}
	now := m.now()
	oldest.Status, oldest.StartedAt, oldest.worker, oldest.leaseEnd = domain.StatusRunning, &now, workerID, now.Add(lease)
	return claimed(oldest), true, nil
}

func claimed(j *memJob) domain.Claimed {
	return domain.Claimed{ID: j.ID, ProjectID: j.ProjectID, MainFilePath: j.MainFilePath, Engine: j.Engine}
}

func (m *memStore) Running(_ context.Context, jobID string) (domain.Claimed, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok || j.Status != domain.StatusRunning || !m.now().Before(j.leaseEnd) {
		return domain.Claimed{}, domain.ErrNotFound
	}
	return claimed(j), nil
}

func (m *memStore) SetSource(_ context.Context, jobID string, skipped []domain.Skipped, live bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.jobs[jobID]
	j.Skipped, j.LiveTextConfirmed = skipped, live
	return nil
}

func (m *memStore) AttachPDF(_ context.Context, jobID string, p domain.NewPDF) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok || j.Status != domain.StatusRunning {
		return domain.ErrNotFound
	}
	j.pdfKey = p.ObjectKey
	return nil
}

func (m *memStore) LockRunning(_ context.Context, jobID string) (domain.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return domain.Job{}, domain.ErrNotFound
	}
	return m.view(j), nil
}

func (m *memStore) Finish(_ context.Context, jobID string, f domain.Finish) (domain.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.jobs[jobID]
	now := m.now()
	j.Status, j.Failure, j.FinishedAt, j.Problems, j.Pages, j.log = f.Status, f.Failure, &now, f.Problems, f.Pages, f.Log
	d := f.DurationMs
	j.DurationMs = &d
	if f.Status != domain.StatusSucceeded {
		j.pdfKey = ""
	}
	return m.view(j), nil
}

func (m *memStore) Retain(_ context.Context, projectID, newest, pdfJob string, keep int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, j := range m.sorted(projectID) {
		if j.ID != newest {
			j.log = nil
		}
		if j.ID != pdfJob && j.Status != domain.StatusRunning {
			j.pdfKey = ""
		}
		if i >= keep && !j.Status.Active() && j.ID != pdfJob {
			delete(m.jobs, j.ID)
		}
	}
	return nil
}

func (m *memStore) ExpireLeases(context.Context) ([]domain.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Job
	for _, j := range m.jobs {
		if j.Status == domain.StatusRunning && !m.now().Before(j.leaseEnd) {
			f := domain.FailWorkerLost
			j.Status, j.Failure, j.pdfKey = domain.StatusFailed, &f, ""
			out = append(out, m.view(j))
		}
	}
	return out, nil
}

func (m *memStore) ExpireQueued(_ context.Context, before time.Time) ([]domain.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Job
	for _, j := range m.jobs {
		if j.Status == domain.StatusQueued && j.QueuedAt.Before(before) {
			f := domain.FailQueueExpired
			j.Status, j.Failure = domain.StatusCancelled, &f
			out = append(out, m.view(j))
		}
	}
	return out, nil
}

func (m *memStore) TextPaths(_ context.Context, projectID string) ([]string, error) {
	return m.paths[projectID], nil
}

func (m *memStore) MemberIDs(_ context.Context, projectID string) ([]string, error) {
	return m.members[projectID], nil
}

type noTx struct{}

func (noTx) InTx(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }

// memAccess answers by a table of user -> error; a missing user may do everything.
type memAccess map[string]error

func (a memAccess) Authorize(_ context.Context, userID, _ string, need domain.Need) error {
	if need == domain.NeedRead && errors.Is(a[userID], domain.ErrForbidden) {
		return nil // a viewer reads
	}
	return a[userID]
}

type memSources struct {
	src domain.Source
	err error
}

func (s memSources) Source(context.Context, string) (domain.Source, error) { return s.src, s.err }

type memObjects struct {
	mu      sync.Mutex
	objects map[string][]byte
	deleted []string
}

func (o *memObjects) Put(_ context.Context, key string, r io.Reader, size int64, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if int64(len(b)) != size {
		return io.ErrShortWrite
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.objects[key] = b
	return nil
}

func (o *memObjects) Get(_ context.Context, key string) (io.ReadCloser, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	b, ok := o.objects[key]
	if !ok {
		return nil, domain.ErrObjectMissing
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (o *memObjects) Delete(_ context.Context, key string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.objects, key)
	o.deleted = append(o.deleted, key)
	return nil
}

type memWorkers struct {
	alive bool
	seen  int
}

func (w *memWorkers) Seen(context.Context, time.Duration) error { w.seen++; return nil }
func (w *memWorkers) Alive(context.Context) (bool, error)       { return w.alive, nil }

type memLimiter struct {
	used map[string]int
}

func (l *memLimiter) Used(_ context.Context, userID string) (int, time.Duration, error) {
	return l.used[userID], time.Minute, nil
}

func (l *memLimiter) Record(_ context.Context, userID string, _ time.Duration) error {
	l.used[userID]++
	return nil
}

type sent struct {
	job domain.Job
	to  []string
}

type memPublisher struct {
	mu   sync.Mutex
	sent []sent
}

func (p *memPublisher) CompileUpdated(_ context.Context, j domain.Job, to []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, sent{j, to})
}

func (p *memPublisher) statuses() []domain.Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]domain.Status, len(p.sent))
	for i, s := range p.sent {
		out[i] = s.job.Status
	}
	return out
}
