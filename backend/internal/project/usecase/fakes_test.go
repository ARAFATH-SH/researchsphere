package usecase

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// memStore is the Store in memory. A project's Access fields are kept on it; the viewer's
// role comes from members.
type memStore struct {
	mu       sync.Mutex
	next     int
	projects map[string]*domain.Project
	members  map[string]map[string]domain.Role // project -> user -> role
	blocks   map[[2]string]bool                // {blocker, blocked}
	away     map[string]bool                   // owners whose account is suspended or unverified
	reports  map[[2]string]bool                // open {project, reporter}
	updates  []domain.Changes
	deleted  []string
	searches []domain.SearchQuery
	hybrid   []domain.SearchQuery
	vectors  []domain.QueryVector
	failTx   error
}

func newMemStore() *memStore {
	return &memStore{
		projects: map[string]*domain.Project{}, members: map[string]map[string]domain.Role{},
		blocks: map[[2]string]bool{}, away: map[string]bool{}, reports: map[[2]string]bool{},
	}
}

// add stores a project owned by owner with status, and returns its ID.
func (s *memStore) add(owner string, status domain.Status) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addLocked(domain.NewProject{OwnerID: owner, Title: "Graph learning", Description: "A study", Abstract: ptr("Secret abstract")}, status)
}

func (s *memStore) addLocked(p domain.NewProject, status domain.Status) string {
	s.next++
	id := "p" + strconv.Itoa(s.next)
	s.projects[id] = &domain.Project{
		ID: id, Title: p.Title, Description: p.Description, Abstract: p.Abstract, Status: status,
		Owner: domain.Person{UserID: p.OwnerID}, MemberCount: 1,
	}
	s.members[id] = map[string]domain.Role{p.OwnerID: domain.RoleOwner}
	return id
}

func (s *memStore) access(p *domain.Project, viewer string) domain.Access {
	return domain.Access{
		Title: p.Title, Status: p.Status, OwnerID: p.Owner.UserID,
		OwnerAvailable: !s.away[p.Owner.UserID], BlockedByOwner: s.blocks[[2]string{p.Owner.UserID, viewer}],
		Role: s.members[p.ID][viewer],
	}
}

func (s *memStore) Create(_ context.Context, p domain.NewProject) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addLocked(p, domain.StatusOpen), nil
}

func (s *memStore) SetSkills(_ context.Context, id string, names []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[id]
	if !ok {
		return domain.ErrNotFound
	}
	p.Skills = make([]domain.Skill, len(names))
	for i, n := range names {
		p.Skills[i] = domain.Skill{ID: "s-" + strings.ToLower(n), Name: n}
	}
	return nil
}

func (s *memStore) Get(_ context.Context, id, viewer string) (domain.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[id]
	if !ok {
		return domain.Project{}, domain.ErrNotFound
	}
	out := *p
	out.Access = s.access(p, viewer)
	out.MyRole = out.Access.Role
	return out, nil
}

func (s *memStore) Lock(_ context.Context, id, viewer string) (domain.Access, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[id]
	if !ok {
		return domain.Access{}, domain.ErrNotFound
	}
	return s.access(p, viewer), nil
}

func (s *memStore) Update(_ context.Context, id string, c domain.Changes) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.projects[id]
	s.updates = append(s.updates, c)
	if c.Title != nil {
		p.Title = *c.Title
	}
	if c.Description != nil {
		p.Description = *c.Description
	}
	if c.SetAbstract {
		p.Abstract = c.Abstract
	}
	if c.Status != nil {
		p.Status = *c.Status
	}
	return nil
}

func (s *memStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.projects, id)
	s.deleted = append(s.deleted, id)
	return nil
}

func (s *memStore) Role(_ context.Context, id, user string) (domain.Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.members[id][user], nil
}

func (s *memStore) ListMine(_ context.Context, user string, limit, offset int) ([]domain.Summary, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Summary
	for id, m := range s.members {
		if r := m[user]; r != domain.RoleNone {
			out = append(out, domain.Summary{ID: id, MyRole: r, Status: s.projects[id].Status})
		}
	}
	total := len(out)
	out = out[min(offset, total):min(offset+limit, total)]
	return out, total, nil
}

func (s *memStore) Search(_ context.Context, q domain.SearchQuery) (domain.SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.searches = append(s.searches, q)
	return domain.SearchResult{Items: []domain.Summary{}}, nil
}

func (s *memStore) SearchHybrid(_ context.Context, q domain.SearchQuery, v domain.QueryVector) (domain.SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hybrid = append(s.hybrid, q)
	s.vectors = append(s.vectors, v)
	return domain.SearchResult{Items: []domain.Summary{}}, nil
}

func (s *memStore) CreateReport(_ context.Context, r domain.NewReport) (domain.ReportReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := [2]string{r.ProjectID, r.ReporterID}
	if s.reports[k] {
		return domain.ReportReceipt{}, domain.ErrAlreadyReported
	}
	s.reports[k] = true
	return domain.ReportReceipt{ID: "r1", CreatedAt: time.Now()}, nil
}

func (s *memStore) ListReports(context.Context, bool, int, int) ([]domain.Report, int, error) {
	return []domain.Report{}, 0, nil
}

func (s *memStore) GetReport(context.Context, string) (domain.Report, domain.ReportedProject, error) {
	return domain.Report{}, domain.ReportedProject{}, domain.ErrReportNotFound
}

func (s *memStore) ResolveReport(context.Context, string, string, string, *string) (domain.Report, error) {
	return domain.Report{}, nil
}

// memTx runs fn at once; failTx makes it fail without running fn.
type memTx struct{ s *memStore }

func (t memTx) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if t.s.failTx != nil {
		return t.s.failTx
	}
	return fn(ctx)
}

// tablePerms is SRS 2.3's project table for the four actions (cmd/api maps the real one).
type tablePerms struct{}

func (tablePerms) Can(role domain.Role, a domain.Action) bool {
	switch a {
	case domain.ActView:
		return true
	case domain.ActEditAbstract, domain.ActCompile:
		return role == domain.RoleOwner || role == domain.RoleEditor
	case domain.ActEditDetails, domain.ActDelete, domain.ActManageMembers, domain.ActTransfer:
		return role == domain.RoleOwner
	case domain.ActRequestToJoin:
		return role == domain.RoleNone
	case domain.ActReadFiles:
		return role != domain.RoleNone
	case domain.ActEditFiles:
		return role == domain.RoleOwner || role == domain.RoleEditor
	}
	return false
}

// memLimiter counts per user; err makes both calls fail.
type memLimiter struct {
	mu   sync.Mutex
	used map[string]int
	err  error
}

func (l *memLimiter) Used(_ context.Context, user string) (int, time.Duration, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return 0, 0, l.err
	}
	return l.used[user], time.Hour, nil
}

func (l *memLimiter) Record(_ context.Context, user string, _ time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return l.err
	}
	if l.used == nil {
		l.used = map[string]int{}
	}
	l.used[user]++
	return nil
}

// memActors suspends the users in suspended.
type memActors struct{ suspended map[string]bool }

func (a memActors) Standing(_ context.Context, user string) (domain.Standing, error) {
	if a.suspended[user] {
		reason := "Spam"
		return domain.Standing{Suspended: true, SuspensionReason: &reason}, nil
	}
	return domain.Standing{}, nil
}

type env struct {
	store    *memStore
	create   *memLimiter
	search   *memLimiter
	join     *memLimiter
	invite   *memLimiter
	actors   memActors
	members  *memMembers
	notifier *memNotifier
	watcher  *memWatcher
	service  *Service
}

func newEnv() *env {
	s := newMemStore()
	e := &env{
		store: s, create: &memLimiter{}, search: &memLimiter{}, join: &memLimiter{}, invite: &memLimiter{},
		actors: memActors{suspended: map[string]bool{}}, members: newMemMembers(s),
		notifier: &memNotifier{}, watcher: &memWatcher{},
	}
	e.service = New(Deps{
		Store: e.store, Tx: memTx{s: e.store}, Permissions: tablePerms{}, Actors: e.actors,
		CreateLimiter: e.create, SearchLimiter: e.search, Watcher: e.watcher,
		Members: e.members, Notifier: e.notifier, JoinLimiter: e.join, InviteLimiter: e.invite,
	})
	return e
}

func ptr[T any](v T) *T { return &v }

var errBoom = errors.New("boom")
