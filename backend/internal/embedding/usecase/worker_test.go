package usecase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/domain"
)

const (
	modelA = "e5@aaaa"
	modelB = "e5@bbbb"
)

// fakeJob is a queue row in memory.
type fakeJob struct {
	domain.Job
	leased  bool
	failed  bool
	lastErr string
	nextTry time.Time
}

// memStore is the Store in memory.
type memStore struct {
	mu         sync.Mutex
	jobs       map[string]*fakeJob
	embeddings map[string]domain.Embedding
	sources    map[string]domain.ProfileSource
	projects   map[string]domain.ProjectSource
	venues     map[string]domain.VenueSource // a live venue; a removed one is absent
	staleCalls []string
	staleLeft  int // owners QueueStale still has to queue
	queueAll   []bool
	rebuilds   []string
	failClaim  error
}

func newMemStore() *memStore {
	return &memStore{jobs: map[string]*fakeJob{}, embeddings: map[string]domain.Embedding{}, sources: map[string]domain.ProfileSource{},
		projects: map[string]domain.ProjectSource{}, venues: map[string]domain.VenueSource{}}
}

func (s *memStore) enqueue(id string) {
	s.enqueueOwner(domain.OwnerProfile, id)
}

// enqueueOwner queues an owner of any type; IDs are unique across types in these tests.
func (s *memStore) enqueueOwner(owner domain.OwnerType, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		j.Generation++
		j.Attempts, j.failed, j.lastErr = 0, false, ""
		return
	}
	s.jobs[id] = &fakeJob{Job: domain.Job{OwnerType: owner, OwnerID: id, Generation: 1}}
}

func (s *memStore) ClaimJobs(_ context.Context, limit int, _ time.Duration) ([]domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failClaim != nil {
		return nil, s.failClaim
	}
	var out []domain.Job
	for _, j := range s.jobs {
		if len(out) == limit {
			break
		}
		if !j.leased && !j.failed {
			j.leased = true
			out = append(out, j.Job)
		}
	}
	return out, nil
}

func (s *memStore) FinishJob(_ context.Context, job domain.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[job.OwnerID]; ok {
		if j.Generation == job.Generation {
			delete(s.jobs, job.OwnerID)
		} else {
			j.leased = false
		}
	}
	return nil
}

func (s *memStore) ReleaseJob(_ context.Context, job domain.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[job.OwnerID]; ok {
		j.leased = false
	}
	return nil
}

func (s *memStore) RetryJob(_ context.Context, job domain.Job, lastError string, next time.Time, failed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[job.OwnerID]; ok && j.Generation == job.Generation {
		j.Attempts++
		j.lastErr, j.nextTry, j.failed, j.leased = lastError, next, failed, false
	}
	return nil
}

func (s *memStore) ProfileSource(_ context.Context, id string) (domain.ProfileSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, ok := s.sources[id]
	if !ok {
		return domain.ProfileSource{}, domain.ErrNotFound
	}
	return src, nil
}

func (s *memStore) ProjectSource(_ context.Context, id string) (domain.ProjectSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, ok := s.projects[id]
	if !ok {
		return domain.ProjectSource{}, domain.ErrNotFound
	}
	return src, nil
}

func (s *memStore) VenueSource(_ context.Context, id string) (domain.VenueSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, ok := s.venues[id]
	if !ok {
		return domain.VenueSource{}, domain.ErrNotFound
	}
	return src, nil
}

func (s *memStore) StoredEmbedding(_ context.Context, _ domain.OwnerType, id string) (domain.Stored, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.embeddings[id]
	if !ok {
		return domain.Stored{}, domain.ErrNotFound
	}
	return domain.Stored{Model: e.Model, ContentHash: e.ContentHash}, nil
}

func (s *memStore) SaveEmbedding(_ context.Context, e domain.Embedding) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.embeddings[e.OwnerID] = e
	return true, nil
}

func (s *memStore) DeleteEmbedding(_ context.Context, _ domain.OwnerType, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.embeddings, id)
	return nil
}

func (s *memStore) QueueStale(_ context.Context, model string, limit int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staleCalls = append(s.staleCalls, model)
	n := min(limit, s.staleLeft)
	s.staleLeft -= n
	return n, nil
}

func (s *memStore) QueueAll(_ context.Context, force bool) (int, error) {
	s.queueAll = append(s.queueAll, force)
	return len(s.sources), nil
}

func (s *memStore) Rebuild(_ context.Context, adminID string) (int, error) {
	s.rebuilds = append(s.rebuilds, adminID)
	return len(s.sources), nil
}

func (s *memStore) Stats(_ context.Context, model string) ([]domain.OwnerStats, error) {
	st := domain.OwnerStats{OwnerType: domain.OwnerProfile, Total: len(s.embeddings), Queued: len(s.jobs)}
	for _, e := range s.embeddings {
		if model != "" && e.Model == model {
			st.CurrentModel++
		}
	}
	return []domain.OwnerStats{st}, nil
}

// fakeEmbedder records each text and answers with err, else a vector for the model.
type fakeEmbedder struct {
	mu    sync.Mutex
	model string
	err   error
	texts []string
}

func (e *fakeEmbedder) EmbedDocument(_ context.Context, text string, maxChunks int) ([]float32, string, int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.texts = append(e.texts, text)
	if maxChunks != domain.MaxChunks {
		return nil, "", 0, fmt.Errorf("maxChunks %d", maxChunks)
	}
	if e.err != nil {
		return nil, "", 0, e.err
	}
	return []float32{1, 0}, e.model, 2, nil
}

func (e *fakeEmbedder) Model(context.Context) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return "", e.err
	}
	return e.model, nil
}

func (e *fakeEmbedder) set(model string, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.model, e.err = model, err
}

func (e *fakeEmbedder) calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.texts)
}

type workerEnv struct {
	store *memStore
	ai    *fakeEmbedder
	w     *Worker
	logs  *bytes.Buffer
	now   time.Time
}

func newWorkerEnv(t *testing.T) *workerEnv {
	t.Helper()
	e := &workerEnv{store: newMemStore(), ai: &fakeEmbedder{model: modelA}, logs: &bytes.Buffer{}, now: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)}
	logger := slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	e.w = NewWorker(e.store, e.ai, DefaultWorkerConfig(), func() time.Time { return e.now }, logger)
	return e
}

func (e *workerEnv) profile(id string, src domain.ProfileSource) {
	e.store.sources[id] = src
	e.store.enqueue(id)
}

var eligible = domain.ProfileSource{Eligible: true, Headline: "Graph learning", Skills: []string{"Go"}}

func TestWorkerEmbedsAQueuedProfile(t *testing.T) {
	e := newWorkerEnv(t)
	e.profile("u1", eligible)
	if wait := e.w.Tick(t.Context()); wait != 5*time.Second {
		t.Errorf("wait = %v, want the 5 s poll", wait)
	}
	got, ok := e.store.embeddings["u1"]
	if !ok {
		t.Fatal("no embedding stored")
	}
	if got.Model != modelA || got.ChunkCount != 2 || got.ContentHash != ContentHash(ProfileText(eligible)) {
		t.Errorf("embedding = %+v", got)
	}
	if len(e.store.jobs) != 0 {
		t.Errorf("job left: %+v", e.store.jobs)
	}
	if e.ai.texts[0] != "Graph learning\nGo" {
		t.Errorf("text sent = %q", e.ai.texts[0])
	}
}

// AI-1.2: a project job embeds the project's title, skills, description and abstract with
// the project hash; a project that is gone loses its embedding and its job.
func TestWorkerEmbedsAQueuedProject(t *testing.T) {
	e := newWorkerEnv(t)
	src := domain.ProjectSource{Title: "Graph  learning", Skills: []string{"Go", "PyTorch"}, Description: "Study\ngraphs", Abstract: "We learn."}
	e.store.projects["p1"] = src
	e.store.enqueueOwner(domain.OwnerProject, "p1")
	e.w.Tick(t.Context())
	got, ok := e.store.embeddings["p1"]
	if !ok {
		t.Fatal("no embedding stored")
	}
	if got.OwnerType != domain.OwnerProject || got.ContentHash != ProjectContentHash(ProjectText(src)) {
		t.Errorf("embedding = %+v", got)
	}
	if want := "Graph learning\nGo, PyTorch\nStudy graphs\nWe learn."; e.ai.texts[0] != want {
		t.Errorf("text sent = %q, want %q", e.ai.texts[0], want)
	}
	if len(e.store.jobs) != 0 {
		t.Errorf("job left: %+v", e.store.jobs)
	}

	// Unchanged: not embedded again.
	e.store.enqueueOwner(domain.OwnerProject, "p1")
	e.w.Tick(t.Context())
	if n := e.ai.calls(); n != 1 {
		t.Errorf("an unchanged project was embedded again: %d calls", n)
	}

	// Gone: the embedding is deleted and the job finished.
	delete(e.store.projects, "p1")
	e.store.enqueueOwner(domain.OwnerProject, "p1")
	e.w.Tick(t.Context())
	if _, ok := e.store.embeddings["p1"]; ok || len(e.store.jobs) != 0 {
		t.Errorf("gone project: embedding kept %v, jobs %+v", ok, e.store.jobs)
	}
}

// AI-1.3: a venue job embeds the venue's name, short name, field labels and scope with the
// venue hash; a venue removed (no longer a source) loses its embedding and its job.
func TestWorkerEmbedsAQueuedVenue(t *testing.T) {
	e := newWorkerEnv(t)
	src := domain.VenueSource{Name: "Workshop on  Graph Learning", ShortName: "GLW",
		Fields: []string{"Artificial intelligence and machine learning", "Networks and communications"}, Scope: "Graphs.\n\n**Topics**: GNNs"}
	e.store.venues["v1"] = src
	e.store.enqueueOwner(domain.OwnerVenue, "v1")
	e.w.Tick(t.Context())
	got, ok := e.store.embeddings["v1"]
	if !ok {
		t.Fatal("no embedding stored")
	}
	if got.OwnerType != domain.OwnerVenue || got.ContentHash != VenueContentHash(VenueText(src)) {
		t.Errorf("embedding = %+v", got)
	}
	want := "Workshop on Graph Learning\nGLW\nArtificial intelligence and machine learning, Networks and communications\nGraphs. **Topics**: GNNs"
	if e.ai.texts[0] != want {
		t.Errorf("text sent = %q, want %q", e.ai.texts[0], want)
	}
	if VenueContentHash("x") == ProjectContentHash("x") || VenueContentHash("x") == ContentHash("x") {
		t.Error("the venue hash must differ from the profile and project hashes")
	}

	// Removed: the source is gone, so the embedding is deleted and the job finished.
	delete(e.store.venues, "v1")
	e.store.enqueueOwner(domain.OwnerVenue, "v1")
	e.w.Tick(t.Context())
	if _, ok := e.store.embeddings["v1"]; ok || len(e.store.jobs) != 0 {
		t.Errorf("removed venue: embedding kept %v, jobs %+v", ok, e.store.jobs)
	}
}

// AI-1.4: the same text and model is not embedded again; a changed text is.
func TestWorkerSkipsUnchangedAndReembedsChanged(t *testing.T) {
	e := newWorkerEnv(t)
	e.profile("u1", eligible)
	e.w.Tick(t.Context())

	e.store.enqueue("u1") // e.g. a profile save that changed nothing
	e.w.Tick(t.Context())
	if e.ai.calls() != 1 || len(e.store.jobs) != 0 {
		t.Errorf("unchanged: %d AI calls, %d jobs; want 1 and 0", e.ai.calls(), len(e.store.jobs))
	}

	changed := eligible
	changed.Bio = "Now also rivers."
	e.profile("u1", changed)
	e.w.Tick(t.Context())
	if e.ai.calls() != 2 || e.store.embeddings["u1"].ContentHash != ContentHash(ProfileText(changed)) {
		t.Errorf("changed: %d AI calls, hash %s", e.ai.calls(), e.store.embeddings["u1"].ContentHash)
	}
}

// A forced job (rebuild) is embedded even when nothing changed.
func TestWorkerForcedJobReembeds(t *testing.T) {
	e := newWorkerEnv(t)
	e.profile("u1", eligible)
	e.w.Tick(t.Context())
	e.store.enqueue("u1")
	e.store.jobs["u1"].Force = true
	e.w.Tick(t.Context())
	if e.ai.calls() != 2 {
		t.Errorf("AI calls = %d, want 2", e.ai.calls())
	}
}

// An empty profile, an ineligible user and a gone user have no embedding (AI-1.1).
func TestWorkerDeletesWhenThereIsNothingToEmbed(t *testing.T) {
	for name, src := range map[string]*domain.ProfileSource{
		"empty":       {Eligible: true},
		"suspended":   {Eligible: false, Headline: "Graph learning"},
		"gone":        nil,
		"blank lines": {Eligible: true, Bio: " \n "},
	} {
		t.Run(name, func(t *testing.T) {
			e := newWorkerEnv(t)
			e.store.embeddings["u1"] = domain.Embedding{OwnerID: "u1", Model: modelA}
			if src != nil {
				e.store.sources["u1"] = *src
			}
			e.store.enqueue("u1")
			e.w.Tick(t.Context())
			if _, ok := e.store.embeddings["u1"]; ok {
				t.Error("embedding kept")
			}
			if len(e.store.jobs) != 0 || e.ai.calls() != 0 {
				t.Errorf("%d jobs left, %d AI calls; want 0 and 0", len(e.store.jobs), e.ai.calls())
			}
		})
	}
}

// AI service down: jobs stay, no attempt is counted, the worker backs off to 1 minute and
// logs once; when it is back, it logs once and catches up.
func TestWorkerKeepsJobsWhileTheAIServiceIsDown(t *testing.T) {
	e := newWorkerEnv(t)
	e.profile("u1", eligible)
	e.profile("u2", eligible)
	e.ai.set(modelA, fmt.Errorf("%w: connection refused", domain.ErrUnavailable))

	var waits []time.Duration
	for range 6 {
		waits = append(waits, e.w.Tick(t.Context()))
	}
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, time.Minute, time.Minute}
	if fmt.Sprint(waits) != fmt.Sprint(want) {
		t.Errorf("waits = %v, want %v", waits, want)
	}
	if len(e.store.jobs) != 2 {
		t.Fatalf("jobs = %d, want 2 kept", len(e.store.jobs))
	}
	for _, j := range e.store.jobs {
		if j.Attempts != 0 || j.leased || j.failed {
			t.Errorf("job %+v: want no attempt counted and no lease", j)
		}
	}
	if n := strings.Count(e.logs.String(), "embedding jobs wait"); n != 1 {
		t.Errorf("logged unavailable %d times, want once:\n%s", n, e.logs)
	}

	e.ai.set(modelA, nil)
	if wait := e.w.Tick(t.Context()); wait != 5*time.Second {
		t.Errorf("after recovery wait = %v, want 5s", wait)
	}
	if len(e.store.jobs) != 0 || len(e.store.embeddings) != 2 {
		t.Errorf("after recovery: %d jobs, %d embeddings; want 0 and 2", len(e.store.jobs), len(e.store.embeddings))
	}
	if !strings.Contains(e.logs.String(), "available again") {
		t.Errorf("recovery not logged:\n%s", e.logs)
	}
}

// The service failing in the middle of a batch: the job and the rest are released.
func TestWorkerReleasesTheBatchWhenTheServiceFailsMidway(t *testing.T) {
	e := newWorkerEnv(t)
	for i := range 3 {
		e.profile(fmt.Sprintf("u%d", i), eligible)
	}
	e.w.Tick(t.Context()) // learns the model
	for i := range 3 {
		e.profile(fmt.Sprintf("u%d", i), domain.ProfileSource{Eligible: true, Headline: fmt.Sprintf("changed %d", i)})
	}
	e.ai.set(modelA, &wrapped{domain.ErrUnavailable})
	e.w.Tick(t.Context())
	for id, j := range e.store.jobs {
		if j.leased || j.Attempts != 0 {
			t.Errorf("job %s: leased %v, attempts %d; want released, none counted", id, j.leased, j.Attempts)
		}
	}
	if len(e.store.jobs) != 3 {
		t.Errorf("jobs = %d, want 3", len(e.store.jobs))
	}
}

type wrapped struct{ err error }

func (w *wrapped) Error() string { return "503 model_loading" }
func (w *wrapped) Unwrap() error { return w.err }

// A text error counts toward 5 attempts, then the job is marked failed with the error.
func TestWorkerMarksATextErrorFailedAfter5Attempts(t *testing.T) {
	e := newWorkerEnv(t)
	e.profile("u1", eligible)
	e.w.Tick(t.Context()) // learn the model and embed once
	e.profile("u1", domain.ProfileSource{Eligible: true, Headline: "something new"})
	e.ai.set(modelA, errors.New("bad response: 400 bad_request"))
	e.ai.mu.Lock()
	e.ai.model = modelA
	e.ai.mu.Unlock()

	var nexts []time.Duration
	for i := 1; i <= 5; i++ {
		e.w.Tick(t.Context())
		j := e.store.jobs["u1"]
		if j == nil || j.Attempts != i {
			t.Fatalf("after try %d: job %+v", i, j)
		}
		nexts = append(nexts, j.nextTry.Sub(e.now))
		if i < 5 {
			e.store.jobs["u1"].leased = false
		}
	}
	j := e.store.jobs["u1"]
	if !j.failed || !strings.Contains(j.lastErr, "400 bad_request") {
		t.Errorf("job = %+v, want failed with the error", j)
	}
	if fmt.Sprint(nexts[:4]) != fmt.Sprint([]time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute}) {
		t.Errorf("retry waits = %v", nexts)
	}
	// A failed job is not claimed again, and the owner's next change resets it.
	calls := e.ai.calls()
	e.w.Tick(t.Context())
	if e.ai.calls() != calls {
		t.Error("a failed job was tried again")
	}
	e.ai.set(modelA, nil)
	e.store.enqueue("u1")
	e.w.Tick(t.Context())
	if len(e.store.jobs) != 0 || e.store.embeddings["u1"].ContentHash != ContentHash("something new") {
		t.Errorf("after a new change: jobs %d, embedding %+v", len(e.store.jobs), e.store.embeddings["u1"])
	}
}

// A change made while the worker embeds keeps the job, so the newer text is embedded too.
func TestWorkerKeepsAJobChangedWhileItWorked(t *testing.T) {
	e := newWorkerEnv(t)
	e.profile("u1", eligible)
	e.w.Tick(t.Context())
	e.store.enqueue("u1")
	e.store.sources["u1"] = domain.ProfileSource{Eligible: true, Headline: "v2"}
	// The change lands between the claim and the finish.
	e.w.embedder = hookEmbedder{fakeEmbedder: e.ai, before: func() { e.store.enqueue("u1") }}
	e.w.Tick(t.Context())
	if j := e.store.jobs["u1"]; j == nil || j.leased {
		t.Fatalf("job = %+v, want kept and released", j)
	}
}

type hookEmbedder struct {
	*fakeEmbedder
	before func()
}

func (h hookEmbedder) EmbedDocument(ctx context.Context, text string, n int) ([]float32, string, int, error) {
	h.before()
	return h.fakeEmbedder.EmbedDocument(ctx, text, n)
}

// AI-1.6: a model other than the stored one re-queues their owners in batches until done,
// and the stale embeddings are made again.
func TestWorkerRequeuesOwnersOfAnotherModel(t *testing.T) {
	e := newWorkerEnv(t)
	e.store.sources["u1"] = eligible
	e.store.embeddings["u1"] = domain.Embedding{OwnerID: "u1", Model: modelA, ContentHash: ContentHash(ProfileText(eligible))}
	e.ai.set(modelB, nil)
	e.store.staleLeft = 1200 // 3 batches of 500

	for range 4 {
		e.w.Tick(t.Context())
	}
	if fmt.Sprint(e.store.staleCalls) != fmt.Sprint([]string{modelB, modelB, modelB}) {
		t.Errorf("QueueStale calls = %v, want 3 with %s, then none", e.store.staleCalls, modelB)
	}

	// The queued owner is embedded again although its text did not change.
	e.store.enqueue("u1")
	e.w.Tick(t.Context())
	if e.store.embeddings["u1"].Model != modelB {
		t.Errorf("model = %s, want %s", e.store.embeddings["u1"].Model, modelB)
	}
}

// A model change seen in an answer starts the re-queue too.
func TestWorkerNoticesAModelChangeWhileRunning(t *testing.T) {
	e := newWorkerEnv(t)
	e.w.Tick(t.Context())
	e.ai.set(modelB, nil)
	e.profile("u1", eligible)
	e.w.Tick(t.Context())
	e.w.Tick(t.Context())
	if got := e.store.staleCalls; len(got) != 2 || got[1] != modelB {
		t.Errorf("QueueStale calls = %v, want a second one with %s", got, modelB)
	}
}

// Without a token the worker waits quietly and keeps the jobs.
func TestWorkerWaitsWhenNotConfigured(t *testing.T) {
	e := newWorkerEnv(t)
	e.profile("u1", eligible)
	e.ai.set("", domain.ErrNotConfigured)
	for range 3 {
		if wait := e.w.Tick(t.Context()); wait != time.Minute {
			t.Errorf("wait = %v, want 1 min", wait)
		}
	}
	if len(e.store.jobs) != 1 || strings.Count(e.logs.String(), "not configured") != 1 {
		t.Errorf("jobs %d, logs:\n%s", len(e.store.jobs), e.logs)
	}
}

// A full batch asks for the next tick at once.
func TestWorkerContinuesAfterAFullBatch(t *testing.T) {
	e := newWorkerEnv(t)
	for i := range 20 {
		e.profile(fmt.Sprintf("u%d", i), eligible)
	}
	if wait := e.w.Tick(t.Context()); wait != 0 {
		t.Errorf("wait = %v, want 0 after a full batch", wait)
	}
	if wait := e.w.Tick(t.Context()); wait != 5*time.Second || len(e.store.jobs) != 0 {
		t.Errorf("wait = %v with %d jobs left; want 5s and 0", wait, len(e.store.jobs))
	}
}

// A job is not started when its lease would run out first; it is released for later.
func TestWorkerDoesNotStartAJobItsLeaseCannotCover(t *testing.T) {
	e := newWorkerEnv(t)
	e.profile("u1", eligible)
	e.profile("u2", eligible)
	e.w.Tick(t.Context()) // embeds both
	e.profile("u1", domain.ProfileSource{Eligible: true, Headline: "a"})
	e.profile("u2", domain.ProfileSource{Eligible: true, Headline: "b"})
	calls := e.ai.calls()
	e.w.embedder = hookEmbedder{fakeEmbedder: e.ai, before: func() { e.now = e.now.Add(4 * time.Minute) }}
	e.w.Tick(t.Context())
	if got := e.ai.calls() - calls; got != 1 {
		t.Errorf("embedded %d jobs, want 1: the second would outlast the lease", got)
	}
	if len(e.store.jobs) != 1 {
		t.Fatalf("jobs = %d, want 1 left", len(e.store.jobs))
	}
	for _, j := range e.store.jobs {
		if j.leased {
			t.Error("the job left over is still leased")
		}
	}
}

func TestWorkerRunStopsWithItsContext(t *testing.T) {
	e := newWorkerEnv(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		e.w.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestServiceStatsRebuildAndBackfill(t *testing.T) {
	store := newMemStore()
	store.sources["u1"] = eligible
	store.embeddings["u1"] = domain.Embedding{OwnerID: "u1", Model: modelA}
	store.embeddings["u2"] = domain.Embedding{OwnerID: "u2", Model: modelB}
	ai := &fakeEmbedder{model: modelA}
	svc := New(store, ai, slog.New(slog.DiscardHandler))

	st, err := svc.Stats(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if st.Model != modelA || st.Owners[0].Total != 2 || st.Owners[0].CurrentModel != 1 {
		t.Errorf("stats = %+v", st)
	}
	ai.set("", domain.ErrUnavailable)
	if st, _ := svc.Stats(t.Context()); st.Model != "" || st.Owners[0].CurrentModel != 0 {
		t.Errorf("AI down: stats = %+v, want no model and none current", st)
	}

	if n, err := svc.Rebuild(t.Context(), "admin-1"); err != nil || n != 1 || store.rebuilds[0] != "admin-1" {
		t.Errorf("Rebuild = %d, %v; calls %v", n, err, store.rebuilds)
	}
	if n, err := svc.Backfill(t.Context()); err != nil || n != 1 || store.queueAll[0] {
		t.Errorf("Backfill = %d, %v; force %v, want not forced", n, err, store.queueAll)
	}
}

// recordWatcher records what the worker tells it.
type recordWatcher struct {
	told []string
}

func (r *recordWatcher) EmbeddingChanged(_ context.Context, t domain.OwnerType, id string) {
	r.told = append(r.told, string(t)+":"+id)
}

// The watcher (project recommendations, AI-3.1) hears of every embedding stored or deleted,
// and of nothing when an unchanged text is skipped.
func TestWorkerTellsTheWatcher(t *testing.T) {
	e := newWorkerEnv(t)
	w := &recordWatcher{}
	e.w.Watch(w)
	e.store.projects["p1"] = domain.ProjectSource{Title: "Graph learning", Description: "Study"}
	e.store.enqueueOwner(domain.OwnerProject, "p1")
	e.w.Tick(t.Context())
	e.store.enqueueOwner(domain.OwnerProject, "p1") // unchanged
	e.w.Tick(t.Context())
	delete(e.store.projects, "p1")
	e.store.enqueueOwner(domain.OwnerProject, "p1") // gone
	e.w.Tick(t.Context())
	e.profile("u1", eligible)
	e.w.Tick(t.Context())
	if want := "project:p1,project:p1,profile:u1"; strings.Join(w.told, ",") != want {
		t.Errorf("told %v, want %s", w.told, want)
	}
}
