package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// account is one development account.
type account struct {
	email, name, role string
}

// accounts are the development accounts (FR-17.9), at the test domain example.com.
var accounts = []account{
	{"test1@example.com", "Test One", "researcher"},
	{"test2@example.com", "Test Two", "researcher"},
	{"test3@example.com", "Test Three", "researcher"},
	{"test4@example.com", "Test Four", "researcher"},
	{"admin@example.com", "Dev Admin", "admin"},
}

// upsertUser creates the account or repairs it: the password, a verified address (kept when
// already set), the role, and no suspension.
const upsertUser = `
INSERT INTO users (email, password_hash, email_verified_at, role) VALUES ($1, $2, now(), $3::user_role)
ON CONFLICT (email) DO UPDATE
SET password_hash = EXCLUDED.password_hash,
    email_verified_at = COALESCE(users.email_verified_at, now()),
    role = EXCLUDED.role,
    suspended_at = NULL, suspended_until = NULL, suspension_reason = NULL
RETURNING id`

const upsertProfile = `
INSERT INTO profiles (user_id, full_name) VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET full_name = EXCLUDED.full_name`

// seed creates or repairs every account in one transaction. hash is the argon2 hash of the
// password. It touches no other account and nothing else of these ones.
func seed(ctx context.Context, pool *pgxpool.Pool, hash string) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		for _, a := range accounts {
			var id string
			if err := tx.QueryRow(ctx, upsertUser, a.email, hash, a.role).Scan(&id); err != nil {
				return fmt.Errorf("account %s: %w", a.email, err)
			}
			if _, err := tx.Exec(ctx, upsertProfile, id, a.name); err != nil {
				return fmt.Errorf("profile of %s: %w", a.email, err)
			}
		}
		return nil
	})
}
