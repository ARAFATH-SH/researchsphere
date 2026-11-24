package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

// Venue recommendations (AI-6.1; slice 25, chunk 25.1).

var _ domain.VenueStore = (*Store)(nil)

// VenueSelf reads whether the project has an abstract and its title-and-abstract embedding
// stamp.
func (s *Store) VenueSelf(ctx context.Context, projectID string) (domain.VenueSelf, error) {
	id, ok := parseUUID(projectID)
	if !ok {
		return domain.VenueSelf{}, domain.ErrProjectNotFound
	}
	rows, err := s.q(ctx).ProjectVenueSelf(ctx, id)
	if err != nil {
		return domain.VenueSelf{}, fmt.Errorf("read project for venue recommendations: %w", err)
	}
	if len(rows) == 0 {
		return domain.VenueSelf{}, domain.ErrProjectNotFound
	}
	r := rows[0]
	self := domain.VenueSelf{HasAbstract: r.HasAbstract}
	if r.Model.Valid {
		self.Embedding = &domain.Stamp{Model: r.Model.String, ContentHash: r.ContentHash.String, UpdatedAt: r.UpdatedAt.Time}
	}
	return self, nil
}

// RankVenues ranks the venues with a deadline still ahead (AI-6.1).
func (s *Store) RankVenues(ctx context.Context, projectID string, limit int) ([]domain.VenueCandidate, error) {
	return s.rankVenues(ctx, projectID, false, limit)
}

// RankJournals ranks the rolling journals.
func (s *Store) RankJournals(ctx context.Context, projectID string, limit int) ([]domain.VenueCandidate, error) {
	return s.rankVenues(ctx, projectID, true, limit)
}

// rankVenues runs the HNSW search over venue embeddings with the ranking settings, in a
// transaction of its own, then reads the fields of the venues found.
func (s *Store) rankVenues(ctx context.Context, projectID string, rolling bool, limit int) ([]domain.VenueCandidate, error) {
	id, ok := parseUUID(projectID)
	if !ok || limit < 1 {
		return []domain.VenueCandidate{}, nil
	}
	lim, err := int32Of(limit)
	if err != nil {
		return nil, err
	}
	var out []domain.VenueCandidate
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if err := SetupRanking(ctx, q); err != nil {
			return err
		}
		rows, err := q.RankProjectVenues(ctx, sqlcgen.RankProjectVenuesParams{ProjectID: id, Rolling: rolling, Lim: lim})
		if err != nil {
			return fmt.Errorf("rank recommended venues: %w", err)
		}
		out = make([]domain.VenueCandidate, len(rows))
		ids := make([]pgtype.UUID, len(rows))
		at := make(map[string]int, len(rows))
		for i, r := range rows {
			ids[i] = r.VenueID
			c := domain.VenueCandidate{Cosine: r.Cosine, VenueCard: domain.VenueCard{
				ID: r.VenueID.String(), Name: r.Name, ShortName: textPtr(r.ShortName), Type: r.Type,
				Fields: []domain.VenueField{}, TimeZone: textPtr(r.DeadlineTz),
				OfficialURL: r.OfficialUrl, LinkKind: r.LinkKind,
			}}
			if r.SubmissionDeadline.Valid {
				d := r.SubmissionDeadline.Time.UTC()
				c.Deadline = &d
			}
			out[i] = c
			at[c.ID] = i
		}
		if len(ids) == 0 {
			return nil
		}
		fields, err := q.VenueFieldsOf(ctx, ids)
		if err != nil {
			return fmt.Errorf("read recommended venues' fields: %w", err)
		}
		for _, f := range fields {
			i := at[f.VenueID.String()]
			out[i].Fields = append(out[i].Fields, domain.VenueField{Key: f.Key, Label: f.Label})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// VenuesStillEligible returns those of ids not removed and still open for submissions.
func (s *Store) VenuesStillEligible(ctx context.Context, ids []string) (map[string]bool, error) {
	out := make(map[string]bool, len(ids))
	uids := uuidsOf(ids)
	if len(uids) == 0 {
		return out, nil
	}
	rows, err := s.q(ctx).VenuesStillEligible(ctx, uids)
	if err != nil {
		return nil, fmt.Errorf("recheck recommended venues: %w", err)
	}
	for _, r := range rows {
		out[r.String()] = true
	}
	return out, nil
}
