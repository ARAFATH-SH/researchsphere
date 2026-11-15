// Package postgres implements the recommendation domain's Store with the queries that sqlc
// generates from queries.sql. It reads only: profile embeddings, profiles, tags, and the
// connections and blocks that leave people out.
package postgres

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

// EfSearch is the HNSW candidate list size the ranking uses (hnsw.ef_search; pgvector's
// default is 40). The NFR-6 evaluation and the NFR-1 benchmark (cmd/recseval) use the same
// value, so they measure what the endpoint does.
const EfSearch = 100

// Store reads through the shared pool.
type Store struct {
	pool *pgxpool.Pool
	tx   postgres.Transactor
}

var _ domain.Store = (*Store)(nil)

// NewStore returns a Store that uses pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, tx: postgres.NewTransactor(pool)}
}

func (s *Store) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Conn(ctx, s.pool))
}

// Self reads the caller's fields and embedding stamp. A value that is not a UUID reads as
// an empty profile.
func (s *Store) Self(ctx context.Context, userID string) (domain.Self, error) {
	id, ok := parseUUID(userID)
	if !ok {
		return domain.Self{}, nil
	}
	row, err := s.q(ctx).SelfFields(ctx, id)
	if err != nil {
		return domain.Self{}, fmt.Errorf("read recommendation profile fields: %w", err)
	}
	self := domain.Self{Interests: row.Interests, Skills: row.Skills, HasBio: row.HasBio}
	if row.Model.Valid {
		self.Embedding = &domain.Stamp{Model: row.Model.String, ContentHash: row.ContentHash.String, UpdatedAt: row.UpdatedAt.Time}
	}
	return self, nil
}

// Rank runs the HNSW search with an iterative scan, in a transaction of its own so the
// settings stay local to it (SetupRanking), then reads the tags of the users found.
func (s *Store) Rank(ctx context.Context, userID string, limit int) ([]domain.Candidate, error) {
	id, ok := parseUUID(userID)
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
		rows, err := q.RankPeople(ctx, sqlcgen.RankPeopleParams{UserID: id, Lim: lim})
		if err != nil {
			return fmt.Errorf("rank recommended people: %w", err)
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

// SetupRanking sets the transaction's HNSW iterative scan and candidate list size. The NFR
// measurements (cmd/recseval) call it before their own ranking queries.
func SetupRanking(ctx context.Context, q *sqlcgen.Queries) error {
	if err := q.SetIterativeScan(ctx); err != nil {
		return fmt.Errorf("set hnsw iterative scan: %w", err)
	}
	if err := q.SetEfSearch(ctx, strconv.Itoa(EfSearch)); err != nil {
		return fmt.Errorf("set hnsw ef_search: %w", err)
	}
	return nil
}

// addTags fills each candidate's interests and skills, in name order.
func (s *Store) addTags(ctx context.Context, q *sqlcgen.Queries, ids []pgtype.UUID, cands []domain.Candidate) error {
	if len(ids) == 0 {
		return nil
	}
	at := make(map[string]int, len(cands))
	for i, c := range cands {
		at[c.UserID] = i
	}
	interests, err := q.InterestsOf(ctx, ids)
	if err != nil {
		return fmt.Errorf("read recommended people's interests: %w", err)
	}
	for _, r := range interests {
		i := at[r.UserID.String()]
		cands[i].Interests = append(cands[i].Interests, r.Name)
	}
	skills, err := q.SkillsOf(ctx, ids)
	if err != nil {
		return fmt.Errorf("read recommended people's skills: %w", err)
	}
	for _, r := range skills {
		i := at[r.UserID.String()]
		cands[i].Skills = append(cands[i].Skills, r.Name)
	}
	return nil
}

// StillEligible returns those of ids that Rank could still return; values that are not
// UUIDs are not.
func (s *Store) StillEligible(ctx context.Context, userID string, ids []string) (map[string]bool, error) {
	out := make(map[string]bool, len(ids))
	id, ok := parseUUID(userID)
	if !ok {
		return out, nil
	}
	uids := make([]pgtype.UUID, 0, len(ids))
	for _, raw := range ids {
		if u, ok := parseUUID(raw); ok {
			uids = append(uids, u)
		}
	}
	if len(uids) == 0 {
		return out, nil
	}
	rows, err := s.q(ctx).StillEligible(ctx, sqlcgen.StillEligibleParams{Ids: uids, UserID: id})
	if err != nil {
		return nil, fmt.Errorf("recheck recommended people: %w", err)
	}
	for _, r := range rows {
		out[r.String()] = true
	}
	return out, nil
}

func parseUUID(s string) (pgtype.UUID, bool) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, false
	}
	return u, true
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func uuidPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := u.String()
	return &s
}

func int32Of(n int) (int32, error) {
	if n < 0 || n > 1<<31-1 {
		return 0, fmt.Errorf("limit %d out of range", n)
	}
	return int32(n), nil
}
