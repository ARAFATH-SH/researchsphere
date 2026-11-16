package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

var _ domain.ProjectStore = (*Store)(nil)

// RankProjects runs the HNSW search over project embeddings with the ranking settings, in a
// transaction of its own, then reads the required skills of the projects found.
func (s *Store) RankProjects(ctx context.Context, userID string, limit int) ([]domain.ProjectCandidate, error) {
	id, ok := parseUUID(userID)
	if !ok || limit < 1 {
		return []domain.ProjectCandidate{}, nil
	}
	lim, err := int32Of(limit)
	if err != nil {
		return nil, err
	}
	var out []domain.ProjectCandidate
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if err := SetupRanking(ctx, q); err != nil {
			return err
		}
		rows, err := q.RankProjects(ctx, sqlcgen.RankProjectsParams{UserID: id, Lim: lim})
		if err != nil {
			return fmt.Errorf("rank recommended projects: %w", err)
		}
		out = make([]domain.ProjectCandidate, len(rows))
		ids := make([]pgtype.UUID, len(rows))
		at := make(map[string]int, len(rows))
		for i, r := range rows {
			ids[i] = r.ProjectID
			out[i] = domain.ProjectCandidate{ProjectID: r.ProjectID.String(), Cosine: r.Cosine, Skills: []string{}}
			at[out[i].ProjectID] = i
		}
		if len(ids) == 0 {
			return nil
		}
		skills, err := q.ProjectSkillsOf(ctx, ids)
		if err != nil {
			return fmt.Errorf("read recommended projects' skills: %w", err)
		}
		for _, r := range skills {
			i := at[r.ProjectID.String()]
			out[i].Skills = append(out[i].Skills, r.Name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ProjectCards reads what each project's card shows.
func (s *Store) ProjectCards(ctx context.Context, ids []string) (map[string]domain.ProjectCard, error) {
	out := make(map[string]domain.ProjectCard, len(ids))
	uids := uuidsOf(ids)
	if len(uids) == 0 {
		return out, nil
	}
	rows, err := s.q(ctx).ProjectCards(ctx, uids)
	if err != nil {
		return nil, fmt.Errorf("read recommended projects: %w", err)
	}
	for _, r := range rows {
		id := r.ID.String()
		out[id] = domain.ProjectCard{
			ID: id, Title: r.Title, Description: r.Description, Skills: []string{},
			Owner: domain.ProjectOwner{
				UserID: r.OwnerID.String(), FullName: r.OwnerName,
				Headline: textPtr(r.OwnerHeadline), PhotoFileID: uuidPtr(r.OwnerPhoto),
			},
			MemberCount: int(r.MemberCount),
		}
	}
	return out, nil
}

// ProjectsStillEligible returns those of ids that RankProjects could still return.
func (s *Store) ProjectsStillEligible(ctx context.Context, userID string, ids []string) (map[string]bool, error) {
	out := make(map[string]bool, len(ids))
	id, ok := parseUUID(userID)
	uids := uuidsOf(ids)
	if !ok || len(uids) == 0 {
		return out, nil
	}
	rows, err := s.q(ctx).ProjectsStillEligible(ctx, sqlcgen.ProjectsStillEligibleParams{Ids: uids, UserID: id})
	if err != nil {
		return nil, fmt.Errorf("recheck recommended projects: %w", err)
	}
	for _, r := range rows {
		out[r.String()] = true
	}
	return out, nil
}

// ProjectSelf reads the project's owner, required skills and embedding stamp.
func (s *Store) ProjectSelf(ctx context.Context, projectID string) (domain.ProjectSelf, error) {
	id, ok := parseUUID(projectID)
	if !ok {
		return domain.ProjectSelf{}, domain.ErrProjectNotFound
	}
	q := s.q(ctx)
	rows, err := q.ProjectSelfFields(ctx, id)
	if err != nil {
		return domain.ProjectSelf{}, fmt.Errorf("read project for recommendations: %w", err)
	}
	if len(rows) == 0 {
		return domain.ProjectSelf{}, domain.ErrProjectNotFound
	}
	r := rows[0]
	self := domain.ProjectSelf{OwnerID: r.OwnerID.String(), Skills: []string{}}
	if r.Model.Valid {
		self.Embedding = &domain.Stamp{Model: r.Model.String, ContentHash: r.ContentHash.String, UpdatedAt: r.UpdatedAt.Time}
	}
	skills, err := q.ProjectSkillsOf(ctx, []pgtype.UUID{id})
	if err != nil {
		return domain.ProjectSelf{}, fmt.Errorf("read project skills for recommendations: %w", err)
	}
	for _, sk := range skills {
		self.Skills = append(self.Skills, sk.Name)
	}
	return self, nil
}

// RankResearchers runs the HNSW search over profile embeddings for the project's embedding,
// then reads the tags of the users found.
func (s *Store) RankResearchers(ctx context.Context, projectID string, limit int) ([]domain.Candidate, error) {
	id, ok := parseUUID(projectID)
	if !ok || limit < 1 {
		return []domain.Candidate{}, nil
	}
	lim, err := int32Of(limit)
	if err != nil {
		return nil, err
	}
	var out []domain.Candidate
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if err := SetupRanking(ctx, q); err != nil {
			return err
		}
		rows, err := q.RankResearchers(ctx, sqlcgen.RankResearchersParams{ProjectID: id, Lim: lim})
		if err != nil {
			return fmt.Errorf("rank recommended researchers: %w", err)
		}
		out = make([]domain.Candidate, len(rows))
		ids := make([]pgtype.UUID, len(rows))
		for i, r := range rows {
			ids[i] = r.UserID
			out[i] = domain.Candidate{
				Person: domain.Person{
					UserID: r.UserID.String(), FullName: r.FullName,
					Headline: textPtr(r.Headline), Institution: textPtr(r.Institution), PhotoFileID: uuidPtr(r.PhotoFileID),
				},
				Cosine: r.Cosine,
			}
		}
		return s.addTags(ctx, q, ids, out)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ResearchersStillEligible returns those of ids that RankResearchers could still return.
func (s *Store) ResearchersStillEligible(ctx context.Context, projectID string, ids []string) (map[string]bool, error) {
	out := make(map[string]bool, len(ids))
	id, ok := parseUUID(projectID)
	uids := uuidsOf(ids)
	if !ok || len(uids) == 0 {
		return out, nil
	}
	rows, err := s.q(ctx).ResearchersStillEligible(ctx, sqlcgen.ResearchersStillEligibleParams{Ids: uids, ProjectID: id})
	if err != nil {
		return nil, fmt.Errorf("recheck recommended researchers: %w", err)
	}
	for _, r := range rows {
		out[r.String()] = true
	}
	return out, nil
}

// OwnedProjects returns the IDs of the projects the users own.
func (s *Store) OwnedProjects(ctx context.Context, userIDs ...string) ([]string, error) {
	uids := uuidsOf(userIDs)
	if len(uids) == 0 {
		return []string{}, nil
	}
	rows, err := s.q(ctx).OwnedProjects(ctx, uids)
	if err != nil {
		return nil, fmt.Errorf("read owned projects: %w", err)
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.String()
	}
	return out, nil
}

// uuidsOf parses ids, leaving out values that are not UUIDs.
func uuidsOf(ids []string) []pgtype.UUID {
	out := make([]pgtype.UUID, 0, len(ids))
	for _, raw := range ids {
		if u, ok := parseUUID(raw); ok {
			out = append(out, u)
		}
	}
	return out
}
