// Package postgres implements the profile domain's Store with the queries that sqlc
// generates from queries.sql.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// foreignKeyViolation is the SQLSTATE of an insert that names a user who does not exist.
const foreignKeyViolation = "23503"

// Store reads and writes profiles through the shared pool.
type Store struct {
	q *sqlcgen.Queries
}

var _ domain.Store = (*Store)(nil)

// NewStore returns a Store that uses pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{q: sqlcgen.New(pool)}
}

// FindAccount returns domain.ErrNotFound when no user has the ID, or the ID is not a UUID.
func (s *Store) FindAccount(ctx context.Context, userID string) (domain.Account, error) {
	id, err := parseUUID(userID)
	if err != nil {
		return domain.Account{}, domain.ErrNotFound
	}
	row, err := s.q.FindAccount(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Account{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Account{}, fmt.Errorf("find account: %w", err)
	}
	a := domain.Account{
		UserID:    row.ID.String(),
		Verified:  row.EmailVerifiedAt.Valid,
		Suspended: row.SuspendedAt.Valid,
	}
	if row.ProfileUserID.Valid {
		a.Profile = &domain.Profile{
			UserID:      a.UserID,
			FullName:    row.FullName.String,
			Headline:    textPtr(row.Headline),
			Bio:         textPtr(row.Bio),
			Institution: textPtr(row.Institution),
			Department:  textPtr(row.Department),
			JobTitle:    textPtr(row.JobTitle),
			Country:     textPtr(row.Country),
		}
	}
	return a, nil
}

// UpsertProfile returns domain.ErrNotFound when the user does not exist.
func (s *Store) UpsertProfile(ctx context.Context, p domain.Profile) (domain.Profile, error) {
	id, err := parseUUID(p.UserID)
	if err != nil {
		return domain.Profile{}, domain.ErrNotFound
	}
	row, err := s.q.UpsertProfile(ctx, sqlcgen.UpsertProfileParams{
		UserID:      id,
		FullName:    p.FullName,
		Headline:    text(p.Headline),
		Bio:         text(p.Bio),
		Institution: text(p.Institution),
		Department:  text(p.Department),
		JobTitle:    text(p.JobTitle),
		Country:     text(p.Country),
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == foreignKeyViolation {
		return domain.Profile{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Profile{}, fmt.Errorf("upsert profile: %w", err)
	}
	return domain.Profile{
		UserID:      row.UserID.String(),
		FullName:    row.FullName,
		Headline:    textPtr(row.Headline),
		Bio:         textPtr(row.Bio),
		Institution: textPtr(row.Institution),
		Department:  textPtr(row.Department),
		JobTitle:    textPtr(row.JobTitle),
		Country:     textPtr(row.Country),
	}, nil
}

func parseUUID(s string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		return id, fmt.Errorf("parse id: %w", err)
	}
	return id, nil
}

func text(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}
