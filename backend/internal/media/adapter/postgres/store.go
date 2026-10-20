// Package postgres implements the media domain's DeletionQueue with the queries that sqlc
// generates from queries.sql.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

// maxErrorLen bounds the error text kept on a queue row.
const maxErrorLen = 1000

// Store reads and writes the media tables through the shared pool.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

var _ domain.DeletionQueue = (*Store)(nil)

// NewStore returns a Store that uses pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: sqlcgen.New(pool)}
}

// Drain claims up to limit queued rows in one transaction, calls fn for each and removes
// the rows fn succeeded for; the others get the attempt and error recorded. The claimed
// rows stay locked until the commit, so no other worker takes them meanwhile.
func (s *Store) Drain(ctx context.Context, limit int, fn func(context.Context, domain.PendingDeletion) error) (domain.DrainResult, error) {
	var res domain.DrainResult
	err := pgx.BeginFunc(ctx, s.pool, func(t pgx.Tx) error {
		res = domain.DrainResult{}
		q := s.q.WithTx(t)
		rows, err := q.ClaimStorageDeletions(ctx, int32(min(limit, 1000))) //nolint:gosec // bounded above
		if err != nil {
			return fmt.Errorf("claim storage deletions: %w", err)
		}
		res.Claimed = len(rows)
		for _, row := range rows {
			d := domain.PendingDeletion{ID: row.ID.String(), ObjectKey: row.ObjectKey, Attempts: int(row.Attempts)}
			if ferr := fn(ctx, d); ferr != nil {
				msg := ferr.Error()
				if len(msg) > maxErrorLen {
					msg = msg[:maxErrorLen]
				}
				if err := q.RecordStorageDeletionFailure(ctx, sqlcgen.RecordStorageDeletionFailureParams{
					ID: row.ID, LastError: pgtype.Text{String: msg, Valid: true},
				}); err != nil {
					return fmt.Errorf("record storage deletion failure: %w", err)
				}
				res.Failed++
				continue
			}
			if err := q.DeleteStorageDeletion(ctx, row.ID); err != nil {
				return fmt.Errorf("delete storage deletion: %w", err)
			}
			res.Deleted++
		}
		return nil
	})
	if err != nil {
		return domain.DrainResult{}, err
	}
	return res, nil
}
