package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// accountDeleter is the auth usecase's deletion for maintenance commands: the account deletion
// of FR-2.8 without a password (DeleteAccountByID). No route reaches it.
type accountDeleter interface {
	DeleteAccountByID(ctx context.Context, userID, by string) error
}

// sampleAccounts lists the sample accounts: a row in sample_profiles and an address at the
// sample domain, both. An account with only one of the two is not touched.
const sampleAccounts = `
SELECT s.user_id::text FROM sample_profiles s JOIN users u ON u.id = s.user_id
WHERE lower(split_part(u.email::text, '@', 2)) = $1
ORDER BY s.seq`

// Deleting a user deletes its interests, skills and publication links, whose triggers queue the
// user's embedding once more; the worker would drop those jobs, this drops them at once.
const dropOrphanJobs = `
DELETE FROM embedding_jobs j
WHERE j.owner_type = 'profile' AND j.owner_id = ANY($1::uuid[])
  AND NOT EXISTS (SELECT 1 FROM users u WHERE u.id = j.owner_id)`

// purge deletes every sample account through del, one account per transaction, so each gets
// the same cleanup as a user's own deletion: solo projects, publication links, publications
// left with no author, and the user's cascades. An account that owns a project with other
// members (FR-2.9) is kept and reported. It returns how many it deleted and kept.
func purge(ctx context.Context, pool *pgxpool.Pool, del accountDeleter, out io.Writer) (deleted, kept int, err error) {
	rows, err := pool.Query(ctx, sampleAccounts, sampleDomain)
	if err != nil {
		return 0, 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, 0, err
	}
	fmt.Fprintf(out, "Deleting %d sample accounts.\n", len(ids))
	for i, id := range ids {
		err := del.DeleteAccountByID(ctx, id, "sample_purge")
		var shared *authdomain.SharedProjectsError
		switch {
		case errors.As(err, &shared):
			kept++
			fmt.Fprintf(out, "Kept sample account %s: it owns a project with other members.\n", id)
		case errors.Is(err, authdomain.ErrNotFound):
			// Deleted meanwhile.
		case err != nil:
			return deleted, kept, fmt.Errorf("delete sample account %s: %w", id, err)
		default:
			deleted++
		}
		if (i+1)%1000 == 0 {
			fmt.Fprintf(out, "  %d of %d done\n", i+1, len(ids))
		}
	}
	if _, err := pool.Exec(ctx, dropOrphanJobs, ids); err != nil {
		return deleted, kept, fmt.Errorf("drop their embedding jobs: %w", err)
	}
	return deleted, kept, nil
}
