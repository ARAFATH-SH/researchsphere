package usecase

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

var testProjectScores = ScoreMap{Model: testModel, Slope: 25, Intercept: -21}

type fakeProjectStore struct {
	projects   []domain.ProjectCandidate
	cards      map[string]domain.ProjectCard
	eligible   map[string]bool // nil: every kept project still eligible
	self       domain.ProjectSelf
	selfErr    error
	people     []domain.Candidate
	stillPeers map[string]bool // nil: every kept researcher still eligible
	owned      map[string][]string
	rankCalls  int
	peerCalls  int
}

func (s *fakeProjectStore) RankProjects(_ context.Context, _ string, limit int) ([]domain.ProjectCandidate, error) {
	s.rankCalls++
	return s.projects[:min(limit, len(s.projects))], nil
}

func (s *fakeProjectStore) ProjectCards(_ context.Context, ids []string) (map[string]domain.ProjectCard, error) {
	out := map[string]domain.ProjectCard{}
	for _, id := range ids {
		if c, ok := s.cards[id]; ok {
			out[id] = c
		}
	}
	return out, nil
}

func (s *fakeProjectStore) ProjectsStillEligible(_ context.Context, _ string, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range ids {
		if s.eligible == nil || s.eligible[id] {
			out[id] = true
		}
	}
	return out, nil
}

func (s *fakeProjectStore) ProjectSelf(context.Context, string) (domain.ProjectSelf, error) {
	return s.self, s.selfErr
}

func (s *fakeProjectStore) RankResearchers(_ context.Context, _ string, limit int) ([]domain.Candidate, error) {
	s.peerCalls++
	return s.people[:min(limit, len(s.people))], nil
}

func (s *fakeProjectStore) ResearchersStillEligible(_ context.Context, _ string, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range ids {
		if s.stillPeers == nil || s.stillPeers[id] {
			out[id] = true
		}
	}
	return out, nil
}

func (s *fakeProjectStore) OwnedProjects(_ context.Context, ids ...string) ([]string, error) {
	var out []string
	for _, id := range ids {
		out = append(out, s.owned[id]...)
	}
	return out, nil
}

type fakeProjectCache struct {
	projects    map[string]domain.KeptProjects
	researchers map[string]domain.Kept
	gen         int64
	genErr      error
	deletedP    []string
	deletedR    []string
}

func newFakeProjectCache() *fakeProjectCache {
	return &fakeProjectCache{projects: map[string]domain.KeptProjects{}, researchers: map[string]domain.Kept{}}
}

func (c *fakeProjectCache) GetProjects(_ context.Context, id string) (domain.KeptProjects, bool, error) {
	k, ok := c.projects[id]
	return k, ok, nil
}

func (c *fakeProjectCache) SetProjects(_ context.Context, id string, k domain.KeptProjects, _ time.Duration) error {
	c.projects[id] = k
	return nil
}

func (c *fakeProjectCache) DeleteProjects(_ context.Context, ids ...string) error {
	for _, id := range ids {
		delete(c.projects, id)
	}
	c.deletedP = append(c.deletedP, ids...)
	return nil
}

func (c *fakeProjectCache) GetResearchers(_ context.Context, id string) (domain.Kept, bool, error) {
	k, ok := c.researchers[id]
	return k, ok, nil
}

func (c *fakeProjectCache) SetResearchers(_ context.Context, id string, k domain.Kept, _ time.Duration) error {
	c.researchers[id] = k
	return nil
}

func (c *fakeProjectCache) DeleteResearchers(_ context.Context, ids ...string) error {
	for _, id := range ids {
		delete(c.researchers, id)
	}
	c.deletedR = append(c.deletedR, ids...)
	return nil
}

func (c *fakeProjectCache) Generation(context.Context) (int64, error) { return c.gen, c.genErr }

func (c *fakeProjectCache) NextGeneration(context.Context) error {
	c.gen++
	return nil
}

// fakeAccess lets only owner see the researcher lists of the projects in visible.
type fakeAccess struct {
	owner   string
	visible map[string]bool
}

func (a fakeAccess) CanSeeRecommendedResearchers(_ context.Context, user, project string) error {
	switch {
	case !a.visible[project]:
		return domain.ErrProjectNotFound
	case user != a.owner:
		return domain.ErrNotProjectOwner
	}
	return nil
}

func pcand(id string, cos float64, skills ...string) domain.ProjectCandidate {
	return domain.ProjectCandidate{ProjectID: id, Cosine: cos, Skills: skills}
}

func card(id string, members int) domain.ProjectCard {
	return domain.ProjectCard{ID: id, Title: "Project " + id, Owner: domain.ProjectOwner{UserID: "o-" + id, FullName: "Owner"}, MemberCount: members}
}

type projectEnv struct {
	svc    *Service
	people *fakeStore
	store  *fakeProjectStore
	cache  *fakeProjectCache
}

func newProjectEnv(self domain.Self, store *fakeProjectStore) *projectEnv {
	people := &fakeStore{self: self}
	cache := newFakeProjectCache()
	svc, _ := newTestService(people, &fakeCache{})
	svc.WithProjects(ProjectDeps{Store: store, Cache: cache, Scores: testProjectScores,
		Access: fakeAccess{owner: "owner", visible: map[string]bool{"p": true, "hidden-to-others": true}}})
	return &projectEnv{svc: svc, people: people, store: store, cache: cache}
}

func readyProjectStore() *fakeProjectStore {
	return &fakeProjectStore{
		projects: []domain.ProjectCandidate{pcand("near", 0.88, "Go", "Python"), pcand("mid", 0.85), pcand("far", 0.80)},
		cards:    map[string]domain.ProjectCard{"near": card("near", 1), "mid": card("mid", 40), "far": card("far", 3)},
	}
}

// AI-3.1 states, in the order of People's (AI-4.2, AI-1.5).
func TestProjectsStates(t *testing.T) {
	for _, tt := range []struct {
		name      string
		self      domain.Self
		wantState domain.State
	}{
		{"empty profile", domain.Self{Embedding: stamp("h")}, domain.StateNeedsProfile},
		{"not embedded yet", domain.Self{Skills: []string{"Go"}}, domain.StateProcessing},
		{"ready", domain.Self{Skills: []string{"Go"}, Embedding: stamp("h")}, domain.StateReady},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newProjectEnv(tt.self, readyProjectStore())
			res, err := e.svc.Projects(t.Context(), "me", 10)
			if err != nil {
				t.Fatal(err)
			}
			if res.State != tt.wantState || res.Items == nil || res.Missing == nil {
				t.Fatalf("state %q items %v missing %v", res.State, res.Items, res.Missing)
			}
			if (len(res.Items) > 0) != (tt.wantState == domain.StateReady) {
				t.Errorf("items %v in state %q", res.Items, res.State)
			}
			if tt.wantState == domain.StateNeedsProfile && len(res.Missing) != 3 {
				t.Errorf("missing %v", res.Missing)
			}
		})
	}
}

// Limit: below 1 refused, above 10 is 10; a Service without WithProjects refuses.
func TestProjectsLimit(t *testing.T) {
	e := newProjectEnv(domain.Self{Skills: []string{"Go"}, Embedding: stamp("h")}, readyProjectStore())
	if _, err := e.svc.Projects(t.Context(), "me", 0); !errors.Is(err, domain.ErrInvalidLimit) {
		t.Errorf("limit 0: %v", err)
	}
	res, err := e.svc.Projects(t.Context(), "me", 2)
	if err != nil || len(res.Items) != 2 {
		t.Errorf("limit 2: %d items, %v", len(res.Items), err)
	}
	plain, _ := newTestService(&fakeStore{}, &fakeCache{})
	if _, err := plain.Projects(t.Context(), "me", 10); err == nil {
		t.Error("a Service without project matching answered")
	}
}

// AI-2.4 for projects: the order is the store's cosine order, the score a function of the
// cosine alone with the project map, and a project's member count changes neither.
// domain.ProjectCandidate holds only an ID, a cosine and skills.
func TestProjectsRankOnlyBySimilarity(t *testing.T) {
	e := newProjectEnv(domain.Self{Skills: []string{"Go"}, Embedding: stamp("h")}, readyProjectStore())
	res, err := e.svc.Projects(t.Context(), "me", 10)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i, it := range res.Items {
		ids = append(ids, it.Project.ID)
		if it.Score != testProjectScores.Score(e.store.projects[i].Cosine) {
			t.Errorf("%s: score %d, want the project map's Score(cos)", it.Project.ID, it.Score)
		}
	}
	if !reflect.DeepEqual(ids, []string{"near", "mid", "far"}) {
		t.Errorf("order %v, want the cosine order (mid has 40 members)", ids)
	}
	if res.Items[1].Project.MemberCount != 40 {
		t.Error("the card's member count is not shown")
	}
	allowed := map[string]bool{"ProjectID": true, "Cosine": true, "Skills": true}
	typ := reflect.TypeFor[domain.ProjectCandidate]()
	for i := range typ.NumField() {
		if f := typ.Field(i); !allowed[f.Name] {
			t.Errorf("domain.ProjectCandidate has field %s: ranking may use only the embedding similarity (AI-2.4)", f.Name)
		}
	}
}

// AI-3.1 shared: up to 3 of the project's required skills the caller has, ignoring case, in
// the project's order and spelling; interests do not count. The card keeps 5 skills.
func TestProjectsSharedSkills(t *testing.T) {
	store := &fakeProjectStore{
		projects: []domain.ProjectCandidate{pcand("p1", 0.9, "Docker", "go", "Machine learning", "PyTorch", "R", "SQL")},
		cards:    map[string]domain.ProjectCard{"p1": card("p1", 1)},
	}
	self := domain.Self{Interests: []string{"Docker"}, Skills: []string{"SQL", "GO", " pytorch ", "R"}, Embedding: stamp("h")}
	e := newProjectEnv(self, store)
	res, err := e.svc.Projects(t.Context(), "me", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Items[0].Shared; !reflect.DeepEqual(got, []string{"go", "PyTorch", "R"}) {
		t.Errorf("shared = %v, want the first 3 matches in the project's order", got)
	}
	if got := res.Items[0].Project.Skills; len(got) != domain.CardSkills || got[0] != "Docker" {
		t.Errorf("card skills = %v", got)
	}
}

func TestSharedSkills(t *testing.T) {
	for _, tt := range []struct {
		project, person, want []string
	}{
		{nil, []string{"Go"}, []string{}},
		{[]string{"Go"}, nil, []string{}},
		{[]string{"Go", "go"}, []string{"GO"}, []string{"Go"}},
		{[]string{"  ", "Go"}, []string{"", "go"}, []string{"Go"}},
		{[]string{"A", "B", "C", "D"}, []string{"d", "c", "b", "a"}, []string{"A", "B", "C"}},
	} {
		if got := SharedSkills(tt.project, tt.person); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SharedSkills(%v, %v) = %v, want %v", tt.project, tt.person, got, tt.want)
		}
	}
}

// The kept list is used while both the caller's embedding stamp and the projects generation
// match; a change to either ranks again. A kept list is rechecked on reading.
func TestProjectsCache(t *testing.T) {
	e := newProjectEnv(domain.Self{Skills: []string{"Go"}, Embedding: stamp("h1")}, readyProjectStore())
	call := func() []domain.ProjectRecommendation {
		t.Helper()
		res, err := e.svc.Projects(t.Context(), "me", 10)
		if err != nil {
			t.Fatal(err)
		}
		return res.Items
	}
	call()
	call()
	if e.store.rankCalls != 1 {
		t.Fatalf("ranked %d times, want 1 (the second call reads the kept list)", e.store.rankCalls)
	}
	e.people.self.Embedding = stamp("h2")
	call()
	if e.store.rankCalls != 2 {
		t.Errorf("a new profile embedding did not rank again")
	}
	e.svc.ProjectChanged(t.Context(), "near")
	call()
	if e.store.rankCalls != 3 {
		t.Errorf("a project change (new generation) did not rank again")
	}
	e.store.eligible = map[string]bool{"near": true, "far": true}
	if items := call(); len(items) != 2 || items[1].Project.ID != "far" || e.store.rankCalls != 3 {
		t.Errorf("recheck kept %v after %d ranks", items, e.store.rankCalls)
	}

	// No generation: ranked every time, never kept.
	e.cache.genErr = errors.New("redis down")
	delete(e.cache.projects, "me")
	call()
	call()
	if e.store.rankCalls != 5 || len(e.cache.projects) != 0 {
		t.Errorf("without a generation: %d ranks, kept %v", e.store.rankCalls, e.cache.projects)
	}
}

// The cache hooks: a membership change drops the members' project lists and the project's
// researcher list; a link change drops both users' lists and the researcher lists of what
// they own; a project change raises the generation and drops its researcher list.
func TestProjectCacheHooks(t *testing.T) {
	e := newProjectEnv(domain.Self{}, &fakeProjectStore{owned: map[string][]string{"a": {"pa1", "pa2"}, "b": {"pb"}}})
	ctx := t.Context()
	e.svc.MembersChanged(ctx, "p", "u1", "u2")
	if !reflect.DeepEqual(e.cache.deletedP, []string{"u1", "u2"}) || !reflect.DeepEqual(e.cache.deletedR, []string{"p"}) {
		t.Errorf("members changed: dropped %v and %v", e.cache.deletedP, e.cache.deletedR)
	}
	e.cache.deletedP, e.cache.deletedR = nil, nil
	e.svc.LinkChanged(ctx, "a", "b")
	slices.Sort(e.cache.deletedR)
	if !reflect.DeepEqual(e.cache.deletedP, []string{"a", "b"}) || !reflect.DeepEqual(e.cache.deletedR, []string{"pa1", "pa2", "pb"}) {
		t.Errorf("link changed: dropped %v and %v", e.cache.deletedP, e.cache.deletedR)
	}
	e.cache.deletedR = nil
	e.svc.ProjectChanged(ctx, "p")
	if e.cache.gen != 1 || !reflect.DeepEqual(e.cache.deletedR, []string{"p"}) {
		t.Errorf("project changed: generation %d, dropped %v", e.cache.gen, e.cache.deletedR)
	}
	// A Service without project matching ignores the hooks.
	plain, _ := newTestService(&fakeStore{}, &fakeCache{})
	plain.ProjectChanged(ctx, "p")
	plain.MembersChanged(ctx, "p", "u")
	plain.LinkChanged(ctx, "a", "b")
}

// AI-2.3: the owner only (403 for others, 404 when hidden); processing without an embedding;
// shared = the project's required skills the researcher has; the connection status is
// filled; the list is kept per project embedding and rechecked.
func TestProjectResearchers(t *testing.T) {
	store := &fakeProjectStore{
		self: domain.ProjectSelf{OwnerID: "owner", Skills: []string{"Go", "Remote sensing", "SQL"}, Embedding: stamp("p1")},
		people: []domain.Candidate{
			{Person: domain.Person{UserID: "r1"}, Cosine: 0.9, Interests: []string{"SQL"}, Skills: []string{"sql", "Go"}},
			{Person: domain.Person{UserID: "r2"}, Cosine: 0.8},
		},
	}
	e := newProjectEnv(domain.Self{}, store)
	ctx := t.Context()
	if _, err := e.svc.ProjectResearchers(ctx, "someone", "p"); !errors.Is(err, domain.ErrNotProjectOwner) {
		t.Errorf("non-owner: %v", err)
	}
	if _, err := e.svc.ProjectResearchers(ctx, "owner", "missing"); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("hidden project: %v", err)
	}
	res, err := e.svc.ProjectResearchers(ctx, "owner", "p")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != domain.StateReady || len(res.Items) != 2 {
		t.Fatalf("result %+v", res)
	}
	if got := res.Items[0].Shared; !reflect.DeepEqual(got, []string{"Go", "SQL"}) {
		t.Errorf("shared = %v, want the project's skills the researcher has, in the project's order", got)
	}
	if res.Items[0].Score != testProjectScores.Score(0.9) || res.Items[0].Connection != domain.StatusNone {
		t.Errorf("item %+v", res.Items[0])
	}
	if _, err := e.svc.ProjectResearchers(ctx, "owner", "p"); err != nil || store.peerCalls != 1 {
		t.Errorf("second call ranked again (%d) or failed: %v", store.peerCalls, err)
	}
	store.stillPeers = map[string]bool{"r2": true}
	res, _ = e.svc.ProjectResearchers(ctx, "owner", "p")
	if len(res.Items) != 1 || res.Items[0].UserID != "r2" {
		t.Errorf("recheck kept %v", res.Items)
	}
	store.self.Embedding = stamp("p2")
	if _, err := e.svc.ProjectResearchers(ctx, "owner", "p"); err != nil || store.peerCalls != 2 {
		t.Errorf("a new project embedding did not rank again (%d): %v", store.peerCalls, err)
	}
	store.self.Embedding = nil
	res, err = e.svc.ProjectResearchers(ctx, "owner", "p")
	if err != nil || res.State != domain.StateProcessing || res.Items == nil || len(res.Items) != 0 {
		t.Errorf("no embedding: %+v, %v", res, err)
	}
}

// The project score map is the one passed in, not the people map: a profile-project cosine
// of 0.86 scores near 50 with the fitted project constants, near 15 with the people map's.
func TestProjectsUseProjectScoreMap(t *testing.T) {
	people := ScoreMap{Model: testModel, Slope: 78.7208, Intercept: -70.2827}
	projects := ScoreMap{Model: testModel, Slope: 87.6849, Intercept: -74.5228}
	store := &fakeProjectStore{projects: []domain.ProjectCandidate{pcand("p1", 0.86)}, cards: map[string]domain.ProjectCard{"p1": card("p1", 1)}}
	cache := newFakeProjectCache()
	svc := New(&fakeStore{self: domain.Self{Skills: []string{"Go"}, Embedding: stamp("h")}}, &fakeCache{}, fakeRelations{}, people, slog.New(slog.DiscardHandler))
	svc.WithProjects(ProjectDeps{Store: store, Cache: cache, Access: fakeAccess{}, Scores: projects})
	res, err := svc.Projects(t.Context(), "me", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := res.Items[0].Score, projects.Score(0.86); got != want || got == people.Score(0.86) {
		t.Errorf("score %d, want the project map's %d (people map: %d)", got, want, people.Score(0.86))
	}
}
