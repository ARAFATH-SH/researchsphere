package usecase

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// fakeEmbedder embeds every text to one vector, or fails with err, or waits for delay.
type fakeEmbedder struct {
	mu    sync.Mutex
	err   error
	delay time.Duration
	texts []string
}

func (f *fakeEmbedder) EmbedQuery(ctx context.Context, text string) (domain.QueryVector, error) {
	f.mu.Lock()
	f.texts = append(f.texts, text)
	err, delay := f.err, f.delay
	f.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return domain.QueryVector{}, ctx.Err()
		}
	}
	if err != nil {
		return domain.QueryVector{}, err
	}
	return domain.QueryVector{Model: "m@1", Values: []float32{1, 0}}, nil
}

func (f *fakeEmbedder) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.texts)
}

// fakeVectors keeps vectors by model and text; err makes every call fail.
type fakeVectors struct {
	kept map[string][]float32
	err  error
}

func (f *fakeVectors) Get(_ context.Context, model, text string) ([]float32, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	v, ok := f.kept[model+"|"+text]
	return v, ok, nil
}

func (f *fakeVectors) Set(_ context.Context, text string, v domain.QueryVector, _ time.Duration) error {
	if f.err != nil {
		return f.err
	}
	f.kept[v.Model+"|"+text] = v.Values
	return nil
}

// recordWatcher records the project changes it is told about.
type recordWatcher struct {
	changed []string
}

func (w *recordWatcher) ProjectChanged(_ context.Context, id string) {
	w.changed = append(w.changed, id)
}
func (w *recordWatcher) MembersChanged(context.Context, string, ...string) {}

type hybridEnv struct {
	*env
	embedder *fakeEmbedder
	vectors  *fakeVectors
	logs     *bytes.Buffer
}

func newHybridEnv(embedder *fakeEmbedder, timeout time.Duration) *hybridEnv {
	e := &hybridEnv{env: newEnv(), embedder: embedder, vectors: &fakeVectors{kept: map[string][]float32{}}, logs: &bytes.Buffer{}}
	d := Deps{
		Store: e.store, Tx: memTx{s: e.store}, Permissions: tablePerms{}, Actors: e.actors,
		CreateLimiter: e.create, SearchLimiter: e.search, VectorCache: e.vectors, AITimeout: timeout,
		Logger: slog.New(slog.NewTextHandler(e.logs, nil)),
	}
	if embedder != nil {
		d.Embedder = embedder
	}
	e.service = New(d)
	return e
}

func (e *hybridEnv) run(t *testing.T, q, mode string) domain.SearchResult {
	t.Helper()
	res, err := e.service.Search(t.Context(), ada, &q, mode, 1)
	if err != nil {
		t.Fatalf("Search(%q, %q): %v", q, mode, err)
	}
	return res
}

// AI-5.1 for projects: auto (the default) embeds the normalized text and runs the hybrid
// search; keyword mode never calls the AI service.
func TestSearchProjectsModes(t *testing.T) {
	e := newHybridEnv(&fakeEmbedder{}, 0)
	for _, mode := range []string{"", "auto"} {
		if res := e.run(t, "  Graph   LEARNING ", mode); res.Mode != domain.ResultHybrid {
			t.Errorf("mode %q answered %q, want hybrid", mode, res.Mode)
		}
	}
	if len(e.store.hybrid) != 2 || e.store.hybrid[0].Q != "Graph   LEARNING" || e.store.vectors[0].Model != "m@1" {
		t.Errorf("hybrid searches = %+v %+v", e.store.hybrid, e.store.vectors)
	}
	if e.embedder.calls() != 1 || e.embedder.texts[0] != "graph learning" {
		t.Errorf("embedded %v, want once, normalized (the second was kept)", e.embedder.texts)
	}
	if res := e.run(t, "graph", "keyword"); res.Mode != domain.ResultKeyword {
		t.Errorf("keyword mode answered %q", res.Mode)
	}
	if e.embedder.calls() != 1 || len(e.store.searches) != 1 {
		t.Errorf("keyword mode embedded or skipped the keyword search: %d embeds, %d searches", e.embedder.calls(), len(e.store.searches))
	}
	if _, err := e.service.Search(t.Context(), ada, ptr("graph"), "semantic", 1); !errors.Is(err, domain.ErrInvalidSearchMode) {
		t.Errorf("mode semantic: %v, want ErrInvalidSearchMode", err)
	}
}

// AI-5.1: auto takes up to 300 characters, keyword up to 100.
func TestSearchProjectsLengthByMode(t *testing.T) {
	e := newHybridEnv(&fakeEmbedder{}, 0)
	long := strings.Repeat("x", 300)
	if _, err := e.service.Search(t.Context(), ada, &long, "auto", 1); err != nil {
		t.Errorf("300 characters in auto mode: %v", err)
	}
	if _, err := e.service.Search(t.Context(), ada, ptr(long+"x"), "auto", 1); !errors.Is(err, domain.ErrInvalidQuery) {
		t.Errorf("301 characters in auto mode: %v", err)
	}
	if _, err := e.service.Search(t.Context(), ada, &long, "keyword", 1); !errors.Is(err, domain.ErrInvalidQuery) {
		t.Errorf("300 characters in keyword mode: %v", err)
	}
}

// AI-5.4: when the AI service fails, is too slow or is not configured, the keyword search
// runs and the result says so; the outage is logged once, the recovery once, never the text.
func TestSearchProjectsFallback(t *testing.T) {
	emb := &fakeEmbedder{err: errors.New("503 from the AI service")}
	e := newHybridEnv(emb, 0)
	for range 3 {
		if res := e.run(t, "secret topic", ""); res.Mode != domain.ResultKeywordFallback {
			t.Fatalf("mode = %q, want keyword_fallback", res.Mode)
		}
	}
	if len(e.store.searches) != 3 || len(e.store.hybrid) != 0 {
		t.Errorf("searches: %d keyword, %d hybrid", len(e.store.searches), len(e.store.hybrid))
	}
	if n := strings.Count(e.logs.String(), "AI service not answering"); n != 1 {
		t.Errorf("outage logged %d times, want once", n)
	}
	emb.mu.Lock()
	emb.err = nil
	emb.mu.Unlock()
	if res := e.run(t, "secret topic", ""); res.Mode != domain.ResultHybrid {
		t.Errorf("after recovery mode = %q", res.Mode)
	}
	if n := strings.Count(e.logs.String(), "answering again"); n != 1 {
		t.Errorf("recovery logged %d times, want once", n)
	}
	if strings.Contains(e.logs.String(), "secret topic") {
		t.Error("the log holds the search text")
	}

	slow := newHybridEnv(&fakeEmbedder{delay: time.Second}, 20*time.Millisecond)
	if res := slow.run(t, "graph", ""); res.Mode != domain.ResultKeywordFallback {
		t.Errorf("slow AI service: mode %q", res.Mode)
	}
	off := newHybridEnv(nil, 0)
	if res := off.run(t, "graph", ""); res.Mode != domain.ResultKeywordFallback {
		t.Errorf("no AI service: mode %q", res.Mode)
	}
}

// A search the caller gave up on is not an outage.
func TestSearchProjectsCallerGaveUp(t *testing.T) {
	e := newHybridEnv(&fakeEmbedder{delay: time.Second}, 5*time.Second)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	q := "graph"
	_, _ = e.service.Search(ctx, ada, &q, "", 1)
	if strings.Contains(e.logs.String(), "not answering") {
		t.Error("a cancelled request was logged as an outage")
	}
}

// A broken vector cache is skipped; the text is embedded.
func TestSearchProjectsVectorCacheDown(t *testing.T) {
	e := newHybridEnv(&fakeEmbedder{}, 0)
	e.vectors.err = errors.New("redis down")
	e.run(t, "graph", "")
	if res := e.run(t, "graph", ""); res.Mode != domain.ResultHybrid {
		t.Errorf("mode %q", res.Mode)
	}
	if e.embedder.calls() != 2 {
		t.Errorf("embedded %d times, want 2 (nothing kept)", e.embedder.calls())
	}
}

// The 60-a-minute limit covers both modes.
func TestSearchProjectsLimitCoversBothModes(t *testing.T) {
	e := newHybridEnv(&fakeEmbedder{}, 0)
	e.run(t, "graph", "")
	e.run(t, "graph", "keyword")
	if e.search.used[ada] != 2 {
		t.Errorf("counted %d searches, want 2", e.search.used[ada])
	}
}

// Project recommendations are told about a status change and a deletion, not about other
// edits (AI-3.1 cache).
func TestWatcherToldOfStatusChangeAndDelete(t *testing.T) {
	e := newEnv()
	w := &recordWatcher{}
	e.service = New(Deps{
		Store: e.store, Tx: memTx{s: e.store}, Permissions: tablePerms{}, Actors: e.actors,
		CreateLimiter: e.create, SearchLimiter: e.search, Watcher: w,
	})
	id := e.store.add(ada, domain.StatusOpen)
	if _, err := e.service.Update(t.Context(), ada, id, UpdateInput{Title: ptr("New title")}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.service.Update(t.Context(), ada, id, UpdateInput{Status: ptr("open")}); err != nil {
		t.Fatal(err)
	}
	if len(w.changed) != 0 {
		t.Fatalf("told of %v for a title edit and the same status", w.changed)
	}
	if _, err := e.service.Update(t.Context(), ada, id, UpdateInput{Status: ptr("closed")}); err != nil {
		t.Fatal(err)
	}
	if err := e.service.Delete(t.Context(), ada, id, ptr("New title")); err != nil {
		t.Fatal(err)
	}
	if len(w.changed) != 2 || w.changed[0] != id || w.changed[1] != id {
		t.Errorf("told of %v, want the status change and the delete", w.changed)
	}
}

// AI-2.3: only the owner sees the recommended researchers; a non-member gets 403 on a
// visible project and 404 on a hidden one.
func TestCanSeeRecommendedResearchers(t *testing.T) {
	e := newEnv()
	open := e.store.add(ada, domain.StatusOpen)
	archived := e.store.add(ada, domain.StatusArchived)
	e.store.members[open]["u-ed"] = domain.RoleEditor
	for _, tt := range []struct {
		user, project string
		want          error
	}{
		{ada, open, nil},
		{ada, archived, nil},
		{"u-ed", open, domain.ErrForbidden},
		{bob, open, domain.ErrForbidden},
		{bob, archived, domain.ErrNotFound},
		{bob, "missing", domain.ErrNotFound},
	} {
		if err := e.service.CanSeeRecommendedResearchers(t.Context(), tt.user, tt.project); !errors.Is(err, tt.want) {
			t.Errorf("%s on %s: %v, want %v", tt.user, tt.project, err, tt.want)
		}
	}
}
