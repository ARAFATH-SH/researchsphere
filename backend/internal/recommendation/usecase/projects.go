package usecase

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

// errProjectsNotConfigured is the error of a project list on a Service made without
// WithProjects.
var errProjectsNotConfigured = errors.New("recommendation: project matching is not configured")

// ProjectDeps is what project matching (AI-3.1, AI-2.3) needs. Every field is required.
type ProjectDeps struct {
	Store  domain.ProjectStore
	Cache  domain.ProjectCache
	Access domain.ProjectAccess
	// Scores maps the cosine of a profile and a project to the 0 to 100 score, in both
	// directions (make calibrate-recs checks it on profile-project pairs).
	Scores ScoreMap
}

// WithProjects lets the Service recommend projects to users and researchers to projects.
func (s *Service) WithProjects(d ProjectDeps) *Service {
	s.projects = &d
	return s
}

// ProjectResult is what a user sees in place of their project list (AI-3.1).
type ProjectResult struct {
	State   domain.State
	Missing []domain.Field // with StateNeedsProfile only
	Items   []domain.ProjectRecommendation
}

// Projects returns the open projects most similar to userID's profile, at most limit
// (above MaxLimit is MaxLimit; below 1 is domain.ErrInvalidLimit). The states are those of
// People, in the same order (AI-4.2, AI-1.5).
func (s *Service) Projects(ctx context.Context, userID string, limit int) (ProjectResult, error) {
	if limit < 1 {
		return ProjectResult{}, domain.ErrInvalidLimit
	}
	if s.projects == nil {
		return ProjectResult{}, errProjectsNotConfigured
	}
	limit = min(limit, MaxLimit)

	self, err := s.store.Self(ctx, userID)
	if err != nil {
		return ProjectResult{}, err
	}
	if missing := missingFields(self); len(missing) == 3 {
		return ProjectResult{State: domain.StateNeedsProfile, Missing: missing, Items: []domain.ProjectRecommendation{}}, nil
	}
	if self.Embedding == nil {
		return ProjectResult{State: domain.StateProcessing, Missing: []domain.Field{}, Items: []domain.ProjectRecommendation{}}, nil
	}
	s.checkModel(ctx, self.Embedding.Model)

	items, err := s.projectList(ctx, userID, self)
	if err != nil {
		return ProjectResult{}, err
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return ProjectResult{State: domain.StateReady, Missing: []domain.Field{}, Items: items}, nil
}

// projectList returns the kept list when it was made from the stored embedding at the
// current projects generation, without the projects that can no longer be recommended, or
// makes and keeps a new one. When the generation cannot be read, the list is made and not
// kept: a list kept without it could outlive a change it should not.
func (s *Service) projectList(ctx context.Context, userID string, self domain.Self) ([]domain.ProjectRecommendation, error) {
	d := s.projects
	gen, genErr := d.Cache.Generation(ctx)
	if genErr != nil {
		s.logger.WarnContext(ctx, "project recommendation cache unavailable, ranking again", slog.Any("error", genErr))
	} else {
		kept, found, err := d.Cache.GetProjects(ctx, userID)
		if err != nil {
			s.logger.WarnContext(ctx, "project recommendation cache unavailable, ranking again", slog.Any("error", err))
		}
		if err == nil && found && kept.Stamp.Equal(*self.Embedding) && kept.Generation == gen {
			return s.projectsStillEligible(ctx, userID, kept.Items)
		}
	}
	items, err := s.rankProjects(ctx, userID, self)
	if err != nil {
		return nil, err
	}
	if genErr == nil {
		kept := domain.KeptProjects{Stamp: *self.Embedding, Generation: gen, Items: items}
		if err := d.Cache.SetProjects(ctx, userID, kept, CacheTTL); err != nil {
			s.logger.WarnContext(ctx, "project recommendation list not cached", slog.Any("error", err))
		}
	}
	out := make([]domain.ProjectRecommendation, len(items))
	copy(out, items)
	return out, nil
}

// rankProjects makes a new project list.
//
// AI-2.4: the order is the cosine similarity of the caller's profile embedding and each
// project's embedding and nothing else; the member count is read afterwards, for the card
// only. The score is a fixed increasing function of the same cosine.
func (s *Service) rankProjects(ctx context.Context, userID string, self domain.Self) ([]domain.ProjectRecommendation, error) {
	d := s.projects
	cands, err := d.Store.RankProjects(ctx, userID, MaxLimit)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return []domain.ProjectRecommendation{}, nil
	}
	ids := make([]string, len(cands))
	for i, c := range cands {
		ids[i] = c.ProjectID
	}
	cards, err := d.Store.ProjectCards(ctx, ids)
	if err != nil {
		return nil, err
	}
	items := make([]domain.ProjectRecommendation, 0, len(cands))
	for _, c := range cands {
		card, ok := cards[c.ProjectID]
		if !ok {
			continue // deleted between the two reads
		}
		card.Skills = firstN(c.Skills, domain.CardSkills)
		items = append(items, domain.ProjectRecommendation{
			Project: card,
			Score:   d.Scores.Score(c.Cosine),
			Shared:  SharedSkills(c.Skills, self.Skills),
		})
	}
	return items, nil
}

// projectsStillEligible drops the kept projects that were closed, joined, blocked or whose
// owner was suspended since the list was made.
func (s *Service) projectsStillEligible(ctx context.Context, userID string, kept []domain.ProjectRecommendation) ([]domain.ProjectRecommendation, error) {
	if len(kept) == 0 {
		return []domain.ProjectRecommendation{}, nil
	}
	ids := make([]string, len(kept))
	for i, it := range kept {
		ids[i] = it.Project.ID
	}
	ok, err := s.projects.Store.ProjectsStillEligible(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]domain.ProjectRecommendation, 0, len(kept))
	for _, it := range kept {
		if ok[it.Project.ID] {
			out = append(out, it)
		}
	}
	return out, nil
}

// ResearcherResult is what a project's owner sees (AI-2.3).
type ResearcherResult struct {
	State domain.State // StateReady or StateProcessing
	Items []domain.Recommendation
}

// ProjectResearchers returns the MaxLimit researchers most similar to the project, for its
// owner (AI-2.3). It returns domain.ErrProjectNotFound when userID cannot see the project and
// domain.ErrNotProjectOwner when they may not see the list. StateProcessing means the
// project has no embedding yet.
func (s *Service) ProjectResearchers(ctx context.Context, userID, projectID string) (ResearcherResult, error) {
	d := s.projects
	if d == nil {
		return ResearcherResult{}, errProjectsNotConfigured
	}
	if err := d.Access.CanSeeRecommendedResearchers(ctx, userID, projectID); err != nil {
		return ResearcherResult{}, err
	}
	self, err := d.Store.ProjectSelf(ctx, projectID)
	if err != nil {
		return ResearcherResult{}, err
	}
	if self.Embedding == nil {
		return ResearcherResult{State: domain.StateProcessing, Items: []domain.Recommendation{}}, nil
	}
	s.checkModel(ctx, self.Embedding.Model)

	items, err := s.researcherList(ctx, projectID, self)
	if err != nil {
		return ResearcherResult{}, err
	}
	if err := s.fillConnections(ctx, userID, items); err != nil {
		return ResearcherResult{}, err
	}
	return ResearcherResult{State: domain.StateReady, Items: items}, nil
}

// researcherList returns the kept list when it was made from the project's stored
// embedding, without the users who can no longer be recommended, or makes and keeps one.
func (s *Service) researcherList(ctx context.Context, projectID string, self domain.ProjectSelf) ([]domain.Recommendation, error) {
	d := s.projects
	kept, found, err := d.Cache.GetResearchers(ctx, projectID)
	if err != nil {
		s.logger.WarnContext(ctx, "researcher recommendation cache unavailable, ranking again", slog.Any("error", err))
	}
	if err == nil && found && kept.Stamp.Equal(*self.Embedding) {
		return s.researchersStillEligible(ctx, projectID, kept.Items)
	}
	cands, err := d.Store.RankResearchers(ctx, projectID, MaxLimit)
	if err != nil {
		return nil, err
	}
	items := make([]domain.Recommendation, len(cands))
	for i, c := range cands {
		items[i] = domain.Recommendation{
			Person: c.Person,
			Score:  d.Scores.Score(c.Cosine),
			Shared: SharedSkills(self.Skills, c.Skills),
		}
	}
	if err := d.Cache.SetResearchers(ctx, projectID, domain.Kept{Stamp: *self.Embedding, Items: items}, CacheTTL); err != nil {
		s.logger.WarnContext(ctx, "researcher recommendation list not cached", slog.Any("error", err))
	}
	return cloneItems(items), nil
}

// researchersStillEligible drops the kept users who joined the project, were suspended or
// blocked since the list was made.
func (s *Service) researchersStillEligible(ctx context.Context, projectID string, kept []domain.Recommendation) ([]domain.Recommendation, error) {
	if len(kept) == 0 {
		return []domain.Recommendation{}, nil
	}
	ids := make([]string, len(kept))
	for i, it := range kept {
		ids[i] = it.UserID
	}
	ok, err := s.projects.Store.ResearchersStillEligible(ctx, projectID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Recommendation, 0, len(kept))
	for _, it := range kept {
		if ok[it.UserID] {
			out = append(out, it)
		}
	}
	return out, nil
}

// SharedSkills returns up to MaxShared of the project's required skills that the person has,
// in the project's order and spelling. Names match when they are equal ignoring letter case
// and spaces around them. The result may be empty, never nil.
func SharedSkills(projectSkills, personSkills []string) []string {
	out := make([]string, 0, MaxShared)
	have := make(map[string]bool, len(personSkills))
	for _, t := range personSkills {
		have[tagKey(t)] = true
	}
	seen := make(map[string]bool, MaxShared)
	for _, t := range projectSkills {
		if len(out) == MaxShared {
			break
		}
		k := tagKey(t)
		if k == "" || !have[k] || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, strings.TrimSpace(t))
	}
	return out
}

func firstN(s []string, n int) []string {
	out := make([]string, min(len(s), n))
	copy(out, s)
	return out
}

// ProjectChanged is told, after the commit, that a project's embedding or status changed or
// the project was deleted: every user's project list is made again, and the project's own
// researcher list is dropped. Errors are logged: a kept list is at most CacheTTL old and is
// rechecked on reading.
func (s *Service) ProjectChanged(ctx context.Context, projectID string) {
	if s.projects == nil {
		return
	}
	if err := s.projects.Cache.NextGeneration(ctx); err != nil {
		s.logger.WarnContext(ctx, "project recommendation lists not renewed after a project change", slog.Any("error", err))
	}
	if err := s.projects.Cache.DeleteResearchers(ctx, projectID); err != nil {
		s.logger.WarnContext(ctx, "researcher recommendation list not cleared after a project change", slog.Any("error", err))
	}
}

// MembersChanged is told, after the commit, that userIDs' membership of the project or a
// join request or invitation between them and it changed (FR-11, FR-10.5): their project
// lists and the project's researcher list are dropped.
func (s *Service) MembersChanged(ctx context.Context, projectID string, userIDs ...string) {
	if s.projects == nil {
		return
	}
	if err := s.projects.Cache.DeleteProjects(ctx, userIDs...); err != nil {
		s.logger.WarnContext(ctx, "project recommendation lists not cleared after a membership change", slog.Any("error", err))
	}
	if err := s.projects.Cache.DeleteResearchers(ctx, projectID); err != nil {
		s.logger.WarnContext(ctx, "researcher recommendation list not cleared after a membership change", slog.Any("error", err))
	}
}

// LinkChanged is told, after the commit, that a block between a and b was added or ended
// (or a connection changed): both users' project lists and the researcher lists of the
// projects either owns are dropped.
func (s *Service) LinkChanged(ctx context.Context, a, b string) {
	if s.projects == nil {
		return
	}
	if err := s.projects.Cache.DeleteProjects(ctx, a, b); err != nil {
		s.logger.WarnContext(ctx, "project recommendation lists not cleared after a link change", slog.Any("error", err))
	}
	owned, err := s.projects.Store.OwnedProjects(ctx, a, b)
	if err != nil {
		s.logger.WarnContext(ctx, "researcher recommendation lists not cleared after a link change", slog.Any("error", err))
		return
	}
	if err := s.projects.Cache.DeleteResearchers(ctx, owned...); err != nil {
		s.logger.WarnContext(ctx, "researcher recommendation lists not cleared after a link change", slog.Any("error", err))
	}
}
