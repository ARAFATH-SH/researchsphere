package usecase

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

const testModel = "intfloat/multilingual-e5-base@rev"

// fakeEmbedder answers with a fixed vector, an error, or waits for the context to end.
type fakeEmbedder struct {
	err   error
	hang  bool // wait until the context ends
	calls int
	texts []string
}

func (f *fakeEmbedder) EmbedQuery(ctx context.Context, text string) (domain.QueryVector, error) {
	f.calls++
	f.texts = append(f.texts, text)
	if f.hang {
		<-ctx.Done()
		return domain.QueryVector{}, ctx.Err()
	}
	if f.err != nil {
		return domain.QueryVector{}, f.err
	}
	return domain.QueryVector{Model: testModel, Values: []float32{0.6, 0.8}}, nil
}

// fakeVectorCache keeps vectors by model and text.
type fakeVectorCache struct {
	kept map[string][]float32
	ttl  time.Duration
	err  error
	gets int
}

func (f *fakeVectorCache) Get(_ context.Context, model, text string) ([]float32, bool, error) {
	f.gets++
	if f.err != nil {
		return nil, false, f.err
	}
	v, ok := f.kept[model+"|"+text]
	return v, ok, nil
}

func (f *fakeVectorCache) Set(_ context.Context, text string, v domain.QueryVector, ttl time.Duration) error {
	if f.err != nil {
		return f.err
	}
	if f.kept == nil {
		f.kept = map[string][]float32{}
	}
	f.kept[v.Model+"|"+text], f.ttl = v.Values, ttl
	return nil
}

type hybridEnv struct {
	s        *Service
	searcher *fakeSearcher
	embedder *fakeEmbedder
	cache    *fakeVectorCache
	log      *bytes.Buffer
}

func newHybridEnv(embedder *fakeEmbedder, timeout time.Duration) *hybridEnv {
	e := &hybridEnv{
		searcher: &fakeSearcher{res: domain.PeopleResults{Total: 1}},
		embedder: embedder,
		cache:    &fakeVectorCache{},
		log:      &bytes.Buffer{},
	}
	p := PeopleSearch{
		Searcher: e.searcher, Limiter: &fakeLimiter{}, Cache: e.cache, AITimeout: timeout,
		Logger: slog.New(slog.NewTextHandler(e.log, nil)),
	}
	if embedder != nil {
		p.Embedder = embedder
	}
	e.s = New(nil, WithPeopleSearch(p))
	return e
}

func (e *hybridEnv) search(t *testing.T, q, mode string) domain.PeopleResults {
	t.Helper()
	res, err := e.s.SearchPeople(t.Context(), adaID, SearchInput{Q: sp(q), Mode: mode, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// Auto mode embeds the normalized text and runs the hybrid search (AI-5.1).
func TestSearchPeopleHybrid(t *testing.T) {
	e := newHybridEnv(&fakeEmbedder{}, time.Second)
	res := e.search(t, "  Computers   that READ  scans ", "")
	if res.Mode != domain.ResultHybrid || e.searcher.hybrid != 1 || e.searcher.keyword != 0 {
		t.Fatalf("mode = %s, hybrid = %d, keyword = %d", res.Mode, e.searcher.hybrid, e.searcher.keyword)
	}
	if got := e.embedder.texts; len(got) != 1 || got[0] != "computers that read scans" {
		t.Errorf("embedded %q, want the normalized text", got)
	}
	if e.searcher.got.Q != "Computers   that READ  scans" {
		t.Errorf("keyword text = %q, want the trimmed text as typed", e.searcher.got.Q)
	}
	if v := e.searcher.vector; v.Model != testModel || len(v.Values) != 2 {
		t.Errorf("vector = %+v", v)
	}
	if e.cache.ttl != 24*time.Hour {
		t.Errorf("kept for %v, want 24h", e.cache.ttl)
	}
}

// mode=keyword never calls the AI service.
func TestSearchPeopleKeywordModeSkipsAI(t *testing.T) {
	e := newHybridEnv(&fakeEmbedder{}, time.Second)
	res := e.search(t, "Ada", "keyword")
	if res.Mode != domain.ResultKeyword || e.embedder.calls != 0 || e.cache.gets != 0 || e.searcher.hybrid != 0 {
		t.Errorf("mode = %s, embeds = %d, cache reads = %d, hybrid = %d", res.Mode, e.embedder.calls, e.cache.gets, e.searcher.hybrid)
	}
}

// The second search of the same text (ignoring case and spacing) uses the kept vector.
func TestSearchPeopleQueryVectorCacheHit(t *testing.T) {
	e := newHybridEnv(&fakeEmbedder{}, time.Second)
	e.search(t, "eye health", "")
	res := e.search(t, "Eye  Health", "auto")
	if e.embedder.calls != 1 {
		t.Errorf("embeds = %d, want 1 (the second from the cache)", e.embedder.calls)
	}
	if res.Mode != domain.ResultHybrid || e.searcher.hybrid != 2 || e.searcher.vector.Model != testModel {
		t.Errorf("mode = %s, hybrid = %d, vector = %+v", res.Mode, e.searcher.hybrid, e.searcher.vector)
	}
}

// A cache that does not answer is skipped (fail open).
func TestSearchPeopleQueryVectorCacheDown(t *testing.T) {
	e := newHybridEnv(&fakeEmbedder{}, time.Second)
	e.cache.err = errors.New("redis down")
	e.search(t, "eye health", "")
	res := e.search(t, "eye health", "")
	if res.Mode != domain.ResultHybrid || e.embedder.calls != 2 {
		t.Errorf("mode = %s, embeds = %d", res.Mode, e.embedder.calls)
	}
}

// AI-5.4: a timeout, an error or no AI service gives keyword results, said so.
func TestSearchPeopleFallback(t *testing.T) {
	tests := []struct {
		name     string
		embedder *fakeEmbedder
	}{
		{"timeout", &fakeEmbedder{hang: true}},
		{"error", &fakeEmbedder{err: errors.New("ai service: 503 model_loading")}},
		{"not configured", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newHybridEnv(tt.embedder, 20*time.Millisecond)
			start := time.Now()
			res := e.search(t, "computers that read scans", "")
			if res.Mode != domain.ResultKeywordFallback || e.searcher.keyword != 1 || e.searcher.hybrid != 0 {
				t.Errorf("mode = %s, keyword = %d, hybrid = %d", res.Mode, e.searcher.keyword, e.searcher.hybrid)
			}
			if res.Total != 1 {
				t.Errorf("total = %d, want the keyword results", res.Total)
			}
			if took := time.Since(start); took > time.Second {
				t.Errorf("took %v, want the timeout to cut it short", took)
			}
		})
	}
}

// The default timeout is short (AI-5.4: fall back rather than wait).
func TestSearchPeopleDefaultTimeout(t *testing.T) {
	e := newHybridEnv(&fakeEmbedder{}, 0)
	if e.s.search.AITimeout != 1500*time.Millisecond {
		t.Errorf("timeout = %v", e.s.search.AITimeout)
	}
}

// While the service is down the failure is logged once; the recovery once.
func TestSearchPeopleLogsOutageOnce(t *testing.T) {
	emb := &fakeEmbedder{err: errors.New("connection refused")}
	e := newHybridEnv(emb, time.Second)
	for range 3 {
		e.search(t, "eye health", "")
	}
	if n := strings.Count(e.log.String(), "AI service not answering"); n != 1 {
		t.Errorf("down logged %d times, want 1:\n%s", n, e.log)
	}
	emb.err = nil
	e.search(t, "eye health", "")
	e.search(t, "lung health", "")
	if n := strings.Count(e.log.String(), "AI service answering again"); n != 1 {
		t.Errorf("recovery logged %d times, want 1:\n%s", n, e.log)
	}
	emb.err = errors.New("down again")
	e.search(t, "kidney health", "")
	if n := strings.Count(e.log.String(), "AI service not answering"); n != 2 {
		t.Errorf("second outage logged %d times in all, want 2", n)
	}
	if strings.Contains(e.log.String(), "kidney") {
		t.Error("the log repeats the search text")
	}
}

// Without an AI service the fallback is logged once too.
func TestSearchPeopleNotConfiguredLoggedOnce(t *testing.T) {
	e := newHybridEnv(nil, time.Second)
	e.search(t, "eye health", "")
	e.search(t, "eye health", "")
	if n := strings.Count(e.log.String(), "not configured"); n != 1 {
		t.Errorf("logged %d times, want 1", n)
	}
}

// A search the caller gave up on is not an outage.
func TestSearchPeopleCallerGaveUp(t *testing.T) {
	e := newHybridEnv(&fakeEmbedder{hang: true}, time.Second)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	_, _ = e.s.SearchPeople(ctx, adaID, SearchInput{Q: sp("eye health"), Page: 1})
	if strings.Contains(e.log.String(), "not answering") {
		t.Errorf("logged an outage:\n%s", e.log)
	}
}

func TestNormalizeQueryText(t *testing.T) {
	for in, want := range map[string]string{
		"Eye Health":      "eye health",
		"eye\t\n  health": "eye health",
		"চোখের   স্বাস্থ্য": "চোখের স্বাস্থ্য",
		"JOSÉ": "josé",
	} {
		if got := NormalizeQueryText(in); got != want {
			t.Errorf("NormalizeQueryText(%q) = %q, want %q", in, got, want)
		}
	}
}
