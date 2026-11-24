package usecase

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

var testVenueScores = ScoreMap{Model: testModel, Slope: 25, Intercept: -20} // 50 at cosine 0.8

// venueNow is the fake clock of the venue tests.
var venueNow = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

type fakeVenueStore struct {
	self       domain.VenueSelf
	selfErr    error
	venues     []domain.VenueCandidate
	journals   []domain.VenueCandidate
	eligible   map[string]bool // nil: every kept venue still eligible
	rankCalls  int
	rankLimit  int
	journalLim int
}

func (s *fakeVenueStore) VenueSelf(context.Context, string) (domain.VenueSelf, error) {
	return s.self, s.selfErr
}

func (s *fakeVenueStore) RankVenues(_ context.Context, _ string, limit int) ([]domain.VenueCandidate, error) {
	s.rankCalls++
	s.rankLimit = limit
	return s.venues[:min(limit, len(s.venues))], nil
}

func (s *fakeVenueStore) RankJournals(_ context.Context, _ string, limit int) ([]domain.VenueCandidate, error) {
	s.journalLim = limit
	return s.journals[:min(limit, len(s.journals))], nil
}

func (s *fakeVenueStore) VenuesStillEligible(_ context.Context, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range ids {
		if s.eligible == nil || s.eligible[id] {
			out[id] = true
		}
	}
	return out, nil
}

type fakeVenueCache struct {
	kept       map[string]domain.KeptVenues
	ttl        time.Duration
	sets       int
	gen        int64
	genErr     error
	nextCalls  int
	deleteArgs []string
}

func (c *fakeVenueCache) GetVenues(_ context.Context, id string) (domain.KeptVenues, bool, error) {
	k, ok := c.kept[id]
	return k, ok, nil
}

func (c *fakeVenueCache) SetVenues(_ context.Context, id string, k domain.KeptVenues, ttl time.Duration) error {
	if c.kept == nil {
		c.kept = map[string]domain.KeptVenues{}
	}
	c.kept[id], c.ttl = k, ttl
	c.sets++
	return nil
}

func (c *fakeVenueCache) DeleteVenues(_ context.Context, ids ...string) error {
	c.deleteArgs = append(c.deleteArgs, ids...)
	for _, id := range ids {
		delete(c.kept, id)
	}
	return nil
}

func (c *fakeVenueCache) VenueGeneration(context.Context) (int64, error) { return c.gen, c.genErr }

func (c *fakeVenueCache) NextVenueGeneration(context.Context) error {
	c.nextCalls++
	c.gen++
	return nil
}

// memberOnly lets "member" see the list; anyone else gets not found.
type memberOnly struct{}

func (memberOnly) CanSeeRecommendedVenues(_ context.Context, userID, _ string) error {
	if userID != "member" {
		return domain.ErrProjectNotFound
	}
	return nil
}

func venueCand(id string, cos float64, deadline *time.Time) domain.VenueCandidate {
	tz := "AoE"
	c := domain.VenueCandidate{Cosine: cos, VenueCard: domain.VenueCard{ID: id, Name: "Venue " + id, Type: "conference",
		Fields: []domain.VenueField{}, OfficialURL: "https://" + id + ".example.org", LinkKind: "official"}}
	if deadline != nil {
		c.Deadline, c.TimeZone = deadline, &tz
	} else {
		c.Type = "journal"
	}
	return c
}

func at(d time.Duration) *time.Time {
	t := venueNow.Add(d)
	return &t
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func newVenueService(store *fakeVenueStore, cache *fakeVenueCache, c *clock, journalMin int) *Service {
	return New(&fakeStore{}, &fakeCache{}, fakeRelations{}, testScores, slog.New(slog.DiscardHandler)).
		WithVenues(VenueDeps{Store: store, Cache: cache, Access: memberOnly{}, Scores: testVenueScores, JournalMinScore: journalMin, Now: c.Now})
}

func venueIDsOf(items []domain.VenueRecommendation) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

// AI-6.3 first, then processing, then ready; the AI-6.4 notice comes with every state, and a
// non-member is refused before anything is read.
func TestProjectVenuesStates(t *testing.T) {
	for _, tt := range []struct {
		name  string
		self  domain.VenueSelf
		want  domain.State
		ranks int
	}{
		{"no abstract, even with an old embedding", domain.VenueSelf{HasAbstract: false, Embedding: stamp("h")}, domain.StateNeedsAbstract, 0},
		{"abstract, no embedding yet", domain.VenueSelf{HasAbstract: true}, domain.StateProcessing, 0},
		{"abstract and embedding", domain.VenueSelf{HasAbstract: true, Embedding: stamp("h")}, domain.StateReady, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeVenueStore{self: tt.self, venues: []domain.VenueCandidate{venueCand("a", 0.9, at(48*time.Hour))}}
			svc := newVenueService(store, &fakeVenueCache{}, &clock{venueNow}, 0)
			res, err := svc.ProjectVenues(t.Context(), "member", "p")
			if err != nil {
				t.Fatal(err)
			}
			if res.State != tt.want || res.Notice != domain.VenueNotice || res.Venues == nil || res.Journals == nil || store.rankCalls != tt.ranks {
				t.Errorf("result = %+v, ranks %d; want state %s, notice, non-nil lists, %d ranks", res, store.rankCalls, tt.want, tt.ranks)
			}
			if tt.want != domain.StateReady && (len(res.Venues) != 0 || len(res.Journals) != 0) {
				t.Errorf("lists in state %s: %+v", tt.want, res)
			}
		})
	}
	if domain.VenueNotice != "Matched on topic only. This says nothing about a venue's quality or ranking." {
		t.Errorf("notice changed: %q", domain.VenueNotice)
	}

	store := &fakeVenueStore{self: domain.VenueSelf{HasAbstract: true, Embedding: stamp("h")}}
	svc := newVenueService(store, &fakeVenueCache{}, &clock{venueNow}, 0)
	if _, err := svc.ProjectVenues(t.Context(), "stranger", "p"); !errors.Is(err, domain.ErrProjectNotFound) || store.rankCalls != 0 {
		t.Errorf("stranger: %v, ranks %d", err, store.rankCalls)
	}
	store.selfErr = domain.ErrProjectNotFound
	if _, err := svc.ProjectVenues(t.Context(), "member", "p"); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("missing project: %v", err)
	}
	unconfigured := New(&fakeStore{}, &fakeCache{}, fakeRelations{}, testScores, slog.New(slog.DiscardHandler))
	if _, err := unconfigured.ProjectVenues(t.Context(), "member", "p"); err == nil {
		t.Error("ProjectVenues without WithVenues succeeded")
	}
}

// AI-6.1, AI-6.2: 5 venues in cosine order with scores from 0 to 100 that follow the cosine;
// up to 3 journals, those below the floor left out (so the list may be empty); days left
// rounded down, never below 0.
func TestProjectVenuesItems(t *testing.T) {
	store := &fakeVenueStore{
		self: domain.VenueSelf{HasAbstract: true, Embedding: stamp("h")},
		venues: []domain.VenueCandidate{
			venueCand("a", 1, at(48*time.Hour-time.Second)), venueCand("b", 0.9, at(24*time.Hour)),
			venueCand("c", 0.5, at(time.Hour)), venueCand("d", 0, at(400*24*time.Hour)), venueCand("e", -1, at(time.Second)),
			venueCand("f", -1, at(time.Hour)),
		},
		journals: []domain.VenueCandidate{venueCand("j1", 0.95, nil), venueCand("j2", 0.8, nil), venueCand("j3", 0.79, nil), venueCand("j4", 0.99, nil)},
	}
	svc := newVenueService(store, &fakeVenueCache{}, &clock{venueNow}, 50)
	res, err := svc.ProjectVenues(t.Context(), "member", "p")
	if err != nil {
		t.Fatal(err)
	}
	if store.rankLimit != VenueLimit || store.journalLim != JournalLimit || VenueLimit != 5 || JournalLimit != 3 {
		t.Errorf("limits %d, %d", store.rankLimit, store.journalLim)
	}
	if got := venueIDsOf(res.Venues); !slices.Equal(got, []string{"a", "b", "c", "d", "e"}) {
		t.Fatalf("venues = %v", got)
	}
	wantDays := []int{1, 1, 0, 400, 0}
	for i, v := range res.Venues {
		if v.Score < 0 || v.Score > 100 || (i > 0 && v.Score > res.Venues[i-1].Score) {
			t.Errorf("score %d of %s out of bounds or order", v.Score, v.ID)
		}
		if v.DaysLeft == nil || *v.DaysLeft != wantDays[i] {
			t.Errorf("%s days left = %v, want %d", v.ID, v.DaysLeft, wantDays[i])
		}
	}
	if res.Venues[0].Score != testVenueScores.Score(1) || res.Venues[0].Score != 99 || res.Venues[4].Score != 0 {
		t.Errorf("extreme scores %d, %d; want 99 (the map at cosine 1) and 0", res.Venues[0].Score, res.Venues[4].Score)
	}
	// j1 scores 74, j2 exactly 50 (kept), j3 49 (left out); j4 is beyond the 3 ranked.
	if got := venueIDsOf(res.Journals); !slices.Equal(got, []string{"j1", "j2"}) {
		t.Errorf("journals = %v, want j1 and j2 (j3 below the floor)", got)
	}
	for _, j := range res.Journals {
		if j.DaysLeft != nil || j.Deadline != nil {
			t.Errorf("journal %s has a deadline", j.ID)
		}
	}

	store.journals = []domain.VenueCandidate{venueCand("weak", 0.1, nil)}
	svc = newVenueService(store, &fakeVenueCache{}, &clock{venueNow}, 50)
	if res, err := svc.ProjectVenues(t.Context(), "member", "p"); err != nil || len(res.Journals) != 0 || res.Journals == nil {
		t.Errorf("only weak journals: %+v, %v; want an empty list", res.Journals, err)
	}
	svc = newVenueService(store, &fakeVenueCache{}, &clock{venueNow}, 0)
	if res, _ := svc.ProjectVenues(t.Context(), "member", "p"); len(res.Journals) != 1 {
		t.Errorf("floor 0: %v, want the weak journal shown", res.Journals)
	}
}

// The lists are kept for at most CacheTTL, or until the first listed deadline; they are
// used while the stamp, the generation and every listed deadline hold, with days left worked
// out at reading; any change makes them again.
func TestProjectVenuesCache(t *testing.T) {
	self := domain.VenueSelf{HasAbstract: true, Embedding: stamp("h")}
	store := &fakeVenueStore{self: self, venues: []domain.VenueCandidate{
		venueCand("late", 0.9, at(30*24*time.Hour)), venueCand("soon", 0.8, at(3*time.Minute))}}
	cache := &fakeVenueCache{gen: 7}
	c := &clock{venueNow}
	svc := newVenueService(store, cache, c, 0)

	if _, err := svc.ProjectVenues(t.Context(), "member", "p"); err != nil {
		t.Fatal(err)
	}
	if cache.sets != 1 || cache.ttl != 3*time.Minute || cache.kept["p"].Generation != 7 || !cache.kept["p"].Stamp.Equal(*self.Embedding) {
		t.Fatalf("kept: sets %d, ttl %v, %+v; want the TTL cut to the first deadline", cache.sets, cache.ttl, cache.kept["p"])
	}
	if cache.kept["p"].Venues[0].DaysLeft != nil {
		t.Error("days left kept")
	}

	// Still fresh: served from the cache, days left recomputed for the later clock.
	c.now = venueNow.Add(2 * time.Minute)
	res, _ := svc.ProjectVenues(t.Context(), "member", "p")
	if store.rankCalls != 1 || *res.Venues[0].DaysLeft != 29 {
		t.Errorf("fresh read ranked again (%d) or days left %d", store.rankCalls, *res.Venues[0].DaysLeft)
	}

	// The first listed deadline passes at exactly its instant: no write, yet made again.
	c.now = *store.venues[1].Deadline
	store.venues = store.venues[:1]
	res, _ = svc.ProjectVenues(t.Context(), "member", "p")
	if store.rankCalls != 2 || !slices.Equal(venueIDsOf(res.Venues), []string{"late"}) {
		t.Errorf("at the deadline: ranks %d, venues %v", store.rankCalls, venueIDsOf(res.Venues))
	}
	if cache.ttl != CacheTTL {
		t.Errorf("ttl %v, want CacheTTL when no deadline is near", cache.ttl)
	}

	// A venue change raises the generation: made again.
	svc.VenuesChanged(t.Context())
	if cache.nextCalls != 1 {
		t.Errorf("VenuesChanged raised the generation %d times", cache.nextCalls)
	}
	_, _ = svc.ProjectVenues(t.Context(), "member", "p")
	if store.rankCalls != 3 {
		t.Errorf("after a venue change: ranks %d, want 3", store.rankCalls)
	}

	// A new title-and-abstract embedding (another stamp): made again.
	store.self.Embedding = stamp("h2")
	_, _ = svc.ProjectVenues(t.Context(), "member", "p")
	if store.rankCalls != 4 {
		t.Errorf("after a new embedding: ranks %d, want 4", store.rankCalls)
	}

	// The recheck drops a kept venue removed since (the generation was not raised).
	store.eligible = map[string]bool{}
	res, _ = svc.ProjectVenues(t.Context(), "member", "p")
	if store.rankCalls != 4 || len(res.Venues) != 0 {
		t.Errorf("recheck: ranks %d, venues %v", store.rankCalls, venueIDsOf(res.Venues))
	}

	// The project's embedding stored or deleted, or the project deleted, drops its lists.
	svc.ProjectVenuesChanged(t.Context(), "p")
	if _, ok := cache.kept["p"]; ok || !slices.Equal(cache.deleteArgs, []string{"p"}) {
		t.Errorf("ProjectVenuesChanged: kept %v, deleted %v", cache.kept, cache.deleteArgs)
	}

	// Without the generation the lists are made and not kept.
	cache.genErr = errors.New("redis down")
	sets := cache.sets
	if _, err := svc.ProjectVenues(t.Context(), "member", "p"); err != nil || cache.sets != sets {
		t.Errorf("no generation: %v, sets %d -> %d", err, sets, cache.sets)
	}
}

// A deadline already reached by the app clock (the database saw it a moment earlier) is
// never kept: the TTL would be 0.
func TestVenueTTL(t *testing.T) {
	for _, tt := range []struct {
		name   string
		venues []domain.VenueRecommendation
		want   time.Duration
	}{
		{"no deadline", nil, CacheTTL},
		{"far", []domain.VenueRecommendation{{VenueCard: domain.VenueCard{Deadline: at(time.Hour)}}}, CacheTTL},
		{"near", []domain.VenueRecommendation{{VenueCard: domain.VenueCard{Deadline: at(time.Hour)}}, {VenueCard: domain.VenueCard{Deadline: at(90 * time.Second)}}}, 90 * time.Second},
		{"reached", []domain.VenueRecommendation{{VenueCard: domain.VenueCard{Deadline: at(0)}}}, 0},
	} {
		if got := venueTTL(tt.venues, venueNow); got != tt.want {
			t.Errorf("%s: venueTTL = %v, want %v", tt.name, got, tt.want)
		}
	}
}
