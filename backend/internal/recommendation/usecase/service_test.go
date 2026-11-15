package usecase

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

const testModel = "intfloat/multilingual-e5-base@abc"

var testScores = ScoreMap{Model: testModel, Slope: 20, Intercept: -16}

type fakeStore struct {
	self      domain.Self
	cands     []domain.Candidate
	eligible  map[string]bool
	rankCalls int
	rankLimit int
}

func (s *fakeStore) Self(context.Context, string) (domain.Self, error) { return s.self, nil }

func (s *fakeStore) Rank(_ context.Context, _ string, limit int) ([]domain.Candidate, error) {
	s.rankCalls++
	s.rankLimit = limit
	return s.cands, nil
}

func (s *fakeStore) StillEligible(_ context.Context, _ string, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range ids {
		if s.eligible == nil || s.eligible[id] {
			out[id] = true
		}
	}
	return out, nil
}

type fakeCache struct {
	kept    map[string]domain.Kept
	ttl     time.Duration
	failGet bool
}

func (c *fakeCache) Get(_ context.Context, id string) (domain.Kept, bool, error) {
	if c.failGet {
		return domain.Kept{}, false, errors.New("redis down")
	}
	k, ok := c.kept[id]
	return k, ok, nil
}

func (c *fakeCache) Set(_ context.Context, id string, k domain.Kept, ttl time.Duration) error {
	if c.kept == nil {
		c.kept = map[string]domain.Kept{}
	}
	c.kept[id], c.ttl = k, ttl
	return nil
}

func (c *fakeCache) Delete(_ context.Context, ids ...string) error {
	for _, id := range ids {
		delete(c.kept, id)
	}
	return nil
}

type fakeRelations map[string]domain.ConnectionStatus

func (r fakeRelations) Statuses(context.Context, string, []string) (map[string]domain.ConnectionStatus, error) {
	return r, nil
}

func stamp(hash string) *domain.Stamp {
	return &domain.Stamp{Model: testModel, ContentHash: hash, UpdatedAt: time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)}
}

func cand(id string, cos float64) domain.Candidate {
	return domain.Candidate{Person: domain.Person{UserID: id, FullName: "User " + id}, Cosine: cos}
}

func newTestService(store *fakeStore, cache *fakeCache) (*Service, *bytes.Buffer) {
	var logs bytes.Buffer
	return New(store, cache, fakeRelations{}, testScores, slog.New(slog.NewTextHandler(&logs, nil))), &logs
}

// AI-4.2: a profile without interests, skills and bio is asked for them, whatever else it
// has (here an embedding made from its headline); AI-4.1: one interest is enough.
func TestPeopleStates(t *testing.T) {
	tests := []struct {
		name        string
		self        domain.Self
		wantState   domain.State
		wantMissing []domain.Field
	}{
		{"empty profile", domain.Self{}, domain.StateNeedsProfile,
			[]domain.Field{domain.FieldInterests, domain.FieldSkills, domain.FieldBio}},
		{"headline only, embedded", domain.Self{Embedding: stamp("h")}, domain.StateNeedsProfile,
			[]domain.Field{domain.FieldInterests, domain.FieldSkills, domain.FieldBio}},
		{"interest, not embedded yet", domain.Self{Interests: []string{"NLP"}}, domain.StateProcessing, []domain.Field{}},
		{"bio, not embedded yet", domain.Self{HasBio: true}, domain.StateProcessing, []domain.Field{}},
		{"AI-4.1: one interest, no publications, embedded", domain.Self{Interests: []string{"NLP"}, Embedding: stamp("h")},
			domain.StateReady, []domain.Field{}},
		{"one skill, embedded", domain.Self{Skills: []string{"Python"}, Embedding: stamp("h")}, domain.StateReady, []domain.Field{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeStore{self: tt.self, cands: []domain.Candidate{cand("b", 0.9)}}
			svc, _ := newTestService(store, &fakeCache{})
			res, err := svc.People(t.Context(), "a", 10)
			if err != nil {
				t.Fatal(err)
			}
			if res.State != tt.wantState || !reflect.DeepEqual(res.Missing, tt.wantMissing) {
				t.Errorf("state %q missing %v, want %q %v", res.State, res.Missing, tt.wantState, tt.wantMissing)
			}
			if res.Items == nil {
				t.Error("items is nil, want an empty list")
			}
			if wantItems := tt.wantState == domain.StateReady; (len(res.Items) > 0) != wantItems {
				t.Errorf("items %v, want some: %v", res.Items, wantItems)
			}
			if tt.wantState != domain.StateReady && store.rankCalls != 0 {
				t.Error("ranked a profile that is not ready")
			}
		})
	}
}

// Above MaxLimit is MaxLimit; below 1 is refused; the store is always asked for MaxLimit,
// so one kept list serves every limit.
func TestPeopleLimit(t *testing.T) {
	var cands []domain.Candidate
	for i := range 12 {
		cands = append(cands, cand(string(rune('a'+i)), 0.9-float64(i)/100))
	}
	for _, tt := range []struct {
		limit, want int
		err         error
	}{{0, 0, domain.ErrInvalidLimit}, {-3, 0, domain.ErrInvalidLimit}, {1, 1, nil}, {3, 3, nil}, {10, 10, nil}, {50, 10, nil}} {
		store := &fakeStore{self: domain.Self{Interests: []string{"x"}, Embedding: stamp("h")}, cands: cands}
		svc, _ := newTestService(store, &fakeCache{})
		res, err := svc.People(t.Context(), "me", tt.limit)
		if !errors.Is(err, tt.err) {
			t.Fatalf("limit %d: err %v, want %v", tt.limit, err, tt.err)
		}
		if err == nil && (len(res.Items) != tt.want || store.rankLimit != MaxLimit) {
			t.Errorf("limit %d: %d items (store asked for %d), want %d (%d)", tt.limit, len(res.Items), store.rankLimit, tt.want, MaxLimit)
		}
	}
}

// AI-2.4: the order is the store's order, which is the cosine order, and the score is a
// function of the cosine alone. A candidate the store returns has no count of any kind:
// domain.Candidate holds only a person, a cosine and tags (checked by reflection, so a
// popularity field cannot be added without this test failing).
func TestRankUsesOnlySimilarity(t *testing.T) {
	store := &fakeStore{self: domain.Self{Interests: []string{"x"}, Embedding: stamp("h")},
		cands: []domain.Candidate{cand("near", 0.91), cand("mid", 0.85), cand("far", 0.80)}}
	svc, _ := newTestService(store, &fakeCache{})
	res, err := svc.People(t.Context(), "me", 10)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i, it := range res.Items {
		ids = append(ids, it.UserID)
		if it.Score != testScores.Score(store.cands[i].Cosine) {
			t.Errorf("%s: score %d, want Score(cos)", it.UserID, it.Score)
		}
	}
	if !reflect.DeepEqual(ids, []string{"near", "mid", "far"}) {
		t.Errorf("order %v, want the cosine order", ids)
	}

	allowed := map[string]bool{"Person": true, "Cosine": true, "Interests": true, "Skills": true}
	typ := reflect.TypeFor[domain.Candidate]()
	for i := range typ.NumField() {
		if f := typ.Field(i); !allowed[f.Name] {
			t.Errorf("domain.Candidate has field %s: ranking may use only the embedding similarity (AI-2.4)", f.Name)
		}
	}
	for _, word := range []string{"count", "citation", "publication", "connection", "follower", "popular"} {
		for i := range typ.NumField() {
			if strings.Contains(strings.ToLower(typ.Field(i).Name), word) {
				t.Errorf("domain.Candidate field %s looks like a popularity signal", typ.Field(i).Name)
			}
		}
	}
}

// The kept list is used while the embedding stamp matches; any change to the caller's
// embedding (a new hash, model or time) makes a new one.
func TestPeopleCache(t *testing.T) {
	store := &fakeStore{self: domain.Self{Interests: []string{"x"}, Embedding: stamp("h1")}, cands: []domain.Candidate{cand("b", 0.9)}}
	cache := &fakeCache{}
	svc, _ := newTestService(store, cache)
	if _, err := svc.People(t.Context(), "me", 10); err != nil {
		t.Fatal(err)
	}
	if store.rankCalls != 1 || cache.ttl != CacheTTL || CacheTTL != 10*time.Minute {
		t.Fatalf("rank calls %d, ttl %v; want 1 and 10m", store.rankCalls, cache.ttl)
	}
	if _, err := svc.People(t.Context(), "me", 10); err != nil {
		t.Fatal(err)
	}
	if store.rankCalls != 1 {
		t.Errorf("rank calls %d after a hit, want 1", store.rankCalls)
	}

	changes := map[string]func(*domain.Stamp){
		"new text":  func(s *domain.Stamp) { s.ContentHash = "h2" },
		"new model": func(s *domain.Stamp) { s.Model = "other@2" },
		"re-embed":  func(s *domain.Stamp) { s.UpdatedAt = s.UpdatedAt.Add(time.Microsecond) },
	}
	for name, change := range changes {
		st := *store.self.Embedding
		change(&st)
		store.self.Embedding = &st
		before := store.rankCalls
		if _, err := svc.People(t.Context(), "me", 10); err != nil {
			t.Fatal(err)
		}
		if store.rankCalls != before+1 {
			t.Errorf("%s: kept list used after the embedding changed", name)
		}
	}

	// Redis down: rank anyway.
	cache.failGet = true
	before := store.rankCalls
	if _, err := svc.People(t.Context(), "me", 10); err != nil {
		t.Fatal(err)
	}
	if store.rankCalls != before+1 {
		t.Error("did not rank when the cache failed")
	}
}

// A kept list drops users who can no longer be recommended (suspended, blocked, linked).
func TestPeopleRechecksKeptList(t *testing.T) {
	store := &fakeStore{self: domain.Self{Interests: []string{"x"}, Embedding: stamp("h")},
		cands: []domain.Candidate{cand("b", 0.9), cand("c", 0.8), cand("d", 0.7)}}
	svc, _ := newTestService(store, &fakeCache{})
	if _, err := svc.People(t.Context(), "me", 10); err != nil {
		t.Fatal(err)
	}
	store.eligible = map[string]bool{"b": true, "d": true}
	res, err := svc.People(t.Context(), "me", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 || res.Items[0].UserID != "b" || res.Items[1].UserID != "d" || store.rankCalls != 1 {
		t.Errorf("items %v after recheck (rank calls %d), want b, d from the kept list", res.Items, store.rankCalls)
	}
}

// Connection statuses are read on every call and filled in, none by default.
func TestPeopleConnectionStatus(t *testing.T) {
	store := &fakeStore{self: domain.Self{Interests: []string{"x"}, Embedding: stamp("h")},
		cands: []domain.Candidate{cand("b", 0.9), cand("c", 0.8)}}
	svc := New(store, &fakeCache{}, fakeRelations{"c": domain.StatusPendingSent}, testScores, slog.New(slog.DiscardHandler))
	res, err := svc.People(t.Context(), "me", 10)
	if err != nil {
		t.Fatal(err)
	}
	if res.Items[0].Connection != domain.StatusNone || res.Items[1].Connection != domain.StatusPendingSent {
		t.Errorf("statuses %q %q, want none and pending_sent", res.Items[0].Connection, res.Items[1].Connection)
	}
}

// A score map fitted for another model is logged once per model.
func TestPeopleWarnsOnModelMismatch(t *testing.T) {
	store := &fakeStore{self: domain.Self{Interests: []string{"x"}, Embedding: &domain.Stamp{Model: "other@1", ContentHash: "h"}}}
	svc, logs := newTestService(store, &fakeCache{})
	for range 3 {
		if _, err := svc.People(t.Context(), "me", 10); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(logs.String(), "fitted for another embedding model"); n != 1 {
		t.Errorf("%d warnings, want 1; logs %s", n, logs)
	}
	store.self.Embedding.Model = testModel
	if _, err := svc.People(t.Context(), "me", 10); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(logs.String(), "fitted for another embedding model"); n != 1 {
		t.Errorf("%d warnings after the matching model, want 1", n)
	}
}

// AI-2.2: the score never decreases as the cosine grows and stays within 0 to 100, over
// the whole cosine range, for the default and other valid maps.
func TestScoreMonotonicAndBounded(t *testing.T) {
	for _, m := range []ScoreMap{testScores, {Model: "m@1", Slope: 0.001, Intercept: 0}, {Model: "m@1", Slope: 500, Intercept: -400}} {
		prev := -1
		for i := 0; i <= 2000; i++ {
			cos := -1 + float64(i)/1000
			s := m.Score(cos)
			if s < 0 || s > 100 {
				t.Fatalf("%+v: Score(%v) = %d, out of 0..100", m, cos, s)
			}
			if s < prev {
				t.Fatalf("%+v: Score(%v) = %d < %d at a lower cosine", m, cos, s, prev)
			}
			prev = s
		}
	}
	if got := testScores.Score(math.NaN()); got != 0 {
		t.Errorf("Score(NaN) = %d, want 0", got)
	}
}

func TestScoreMapValidate(t *testing.T) {
	for _, tt := range []struct {
		m  ScoreMap
		ok bool
	}{
		{testScores, true},
		{ScoreMap{Model: "m@1", Slope: 0, Intercept: 1}, false},
		{ScoreMap{Model: "m@1", Slope: -2, Intercept: 1}, false},
		{ScoreMap{Model: "m@1", Slope: math.NaN()}, false},
		{ScoreMap{Model: "m@1", Slope: 1, Intercept: math.Inf(1)}, false},
		{ScoreMap{Slope: 1}, false},
	} {
		if err := tt.m.Validate(); (err == nil) != tt.ok {
			t.Errorf("Validate(%+v) = %v, want ok %v", tt.m, err, tt.ok)
		}
	}
}

func TestSharedTags(t *testing.T) {
	tests := []struct {
		name                           string
		myI, myS, theirI, theirS, want []string
	}{
		{"none", []string{"NLP"}, nil, []string{"Robotics"}, nil, []string{}},
		{"case-insensitive, viewer's spelling", []string{"Remote Sensing"}, nil, []string{"remote sensing"}, nil, []string{"Remote Sensing"}},
		{"interests first", []string{"GIS"}, []string{"Python"}, []string{"GIS"}, []string{"Python"}, []string{"GIS", "Python"}},
		{"interests first even when skills come first by name", []string{"Zoology"}, []string{"Algebra"},
			[]string{"zoology"}, []string{"algebra"}, []string{"Zoology", "Algebra"}},
		{"at most 3", []string{"A", "B", "C", "D"}, []string{"E"}, []string{"a", "b", "c", "d"}, []string{"e"}, []string{"A", "B", "C"}},
		{"interest and skill of the same name once", []string{"Statistics"}, []string{"statistics", "R"},
			[]string{"Statistics"}, []string{"Statistics", "R"}, []string{"Statistics", "R"}},
		{"an interest of one and a skill of the other is not shared", []string{"Python"}, nil, nil, []string{"Python"}, []string{}},
		{"spaces around", []string{" Bangla NLP "}, nil, []string{"bangla nlp"}, nil, []string{"Bangla NLP"}},
		{"Bangla", []string{"জলবায়ু পরিবর্তন"}, nil, []string{"জলবায়ু পরিবর্তন"}, nil, []string{"জলবায়ু পরিবর্তন"}},
		{"exact match only", []string{"Machine learning"}, nil, []string{"Machine learning systems"}, nil, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SharedTags(tt.myI, tt.myS, tt.theirI, tt.theirS)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SharedTags = %q, want %q", got, tt.want)
			}
		})
	}
}
