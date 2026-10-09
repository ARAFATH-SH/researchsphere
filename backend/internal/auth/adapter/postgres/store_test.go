//go:build integration

// Repository tests against the real database. Run with `make test-integration`
// (needs `make up` and `make migrate`); they read DATABASE_URL.
package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

func newTestStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Fatal("DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatal("DATABASE_URL is not accepted by the PostgreSQL driver")
	}
	t.Cleanup(pool.Close)
	return NewStore(pool), pool
}

// uniqueEmail returns an address no other test run uses, and deletes its user (and,
// through the cascade, the profile and tokens) when the test ends.
func uniqueEmail(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	email := "it-" + strings.ToLower(rand.Text()) + "@example.org"
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", email)
		if err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	return email
}

// register stores a user, profile and token in one transaction, as the usecase does.
func register(ctx context.Context, s *Store, email, fullName, tokenHash string, expires time.Time) (string, error) {
	var id string
	err := s.RunInTx(ctx, func(tx domain.Tx) error {
		var err error
		if id, err = tx.CreateUser(ctx, domain.NewUser{Email: email, PasswordHash: "hash"}); err != nil {
			return err
		}
		if err := tx.CreateProfile(ctx, id, fullName); err != nil {
			return err
		}
		return tx.CreateToken(ctx, domain.NewToken{UserID: id, Purpose: domain.PurposeEmailVerification, Hash: tokenHash, ExpiresAt: expires})
	})
	return id, err
}

func TestRegisterCommitsUserProfileAndToken(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	email := uniqueEmail(t, pool)
	hash := rand.Text()

	id, err := register(ctx, s, email, "Ada Lovelace", hash, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	u, err := s.FindUserByEmail(ctx, strings.ToUpper(email))
	if err != nil {
		t.Fatalf("FindUserByEmail in upper case: %v", err)
	}
	if u.ID != id || u.Email != email || u.FullName != "Ada Lovelace" || u.Verified() {
		t.Errorf("user = %+v", u)
	}

	var stored string
	if err := pool.QueryRow(ctx, "SELECT token_hash FROM auth_tokens WHERE user_id = $1", id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != hash {
		t.Errorf("token_hash = %q, want %q", stored, hash)
	}
}

func TestCreateUserDuplicateEmailInAnotherCase(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	email := uniqueEmail(t, pool)

	if _, err := register(ctx, s, email, "Ada", rand.Text(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, err := register(ctx, s, strings.ToUpper(email), "Other", rand.Text(), time.Now().Add(time.Hour))
	if !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("second register error = %v, want ErrEmailTaken", err)
	}
}

func TestRegisterRollsBackWhenProfileFails(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	email := uniqueEmail(t, pool)

	// An empty name breaks the CHECK on profiles.full_name after the user row is written.
	if _, err := register(ctx, s, email, " ", rand.Text(), time.Now().Add(time.Hour)); err == nil {
		t.Fatal("register with an empty name succeeded")
	}
	if _, err := s.FindUserByEmail(ctx, email); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("FindUserByEmail error = %v, want ErrNotFound: the user row must be rolled back", err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE email = $1", email).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("users with the address = %d, want 0", n)
	}
}

func TestTokenLifecycle(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	email := uniqueEmail(t, pool)
	hash := rand.Text()
	expires := time.Now().Add(time.Hour).Truncate(time.Microsecond)

	id, err := register(ctx, s, email, "Ada", hash, expires)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Truncate(time.Microsecond)
	err = s.RunInTx(ctx, func(tx domain.Tx) error {
		tok, err := tx.FindTokenForUpdate(ctx, hash, domain.PurposeEmailVerification)
		if err != nil {
			return err
		}
		if tok.UserID != id || !tok.ExpiresAt.Equal(expires) || tok.UsedAt != nil {
			t.Errorf("token = %+v", tok)
		}
		if err := tx.MarkTokenUsed(ctx, tok.ID, now); err != nil {
			return err
		}
		return tx.MarkEmailVerified(ctx, id, now)
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	u, err := s.FindUserByEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	if u.EmailVerifiedAt == nil || !u.EmailVerifiedAt.Equal(now) {
		t.Errorf("email_verified_at = %v, want %v", u.EmailVerifiedAt, now)
	}

	// A second verification keeps the first time.
	later := now.Add(time.Minute)
	if err := s.RunInTx(ctx, func(tx domain.Tx) error { return tx.MarkEmailVerified(ctx, id, later) }); err != nil {
		t.Fatal(err)
	}
	if u, _ = s.FindUserByEmail(ctx, email); !u.EmailVerifiedAt.Equal(now) {
		t.Errorf("email_verified_at = %v after a second verification, want %v", u.EmailVerifiedAt, now)
	}

	err = s.RunInTx(ctx, func(tx domain.Tx) error {
		tok, err := tx.FindTokenForUpdate(ctx, hash, domain.PurposeEmailVerification)
		if err != nil {
			return err
		}
		if tok.UsedAt == nil || !tok.UsedAt.Equal(now) {
			t.Errorf("used_at = %v, want %v", tok.UsedAt, now)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFindTokenForUpdateUnknownOrOtherPurpose(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	email := uniqueEmail(t, pool)
	hash := rand.Text()
	if _, err := register(ctx, s, email, "Ada", hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		hash    string
		purpose domain.TokenPurpose
	}{
		{"unknown hash", rand.Text(), domain.PurposeEmailVerification},
		{"other purpose", hash, domain.TokenPurpose("password_reset")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.RunInTx(ctx, func(tx domain.Tx) error {
				_, err := tx.FindTokenForUpdate(ctx, tt.hash, tt.purpose)
				return err
			})
			if !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestDeleteUnusedTokensKeepsUsedOnes(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	email := uniqueEmail(t, pool)
	used, unused := rand.Text(), rand.Text()

	id, err := register(ctx, s, email, "Ada", used, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	err = s.RunInTx(ctx, func(tx domain.Tx) error {
		tok, err := tx.FindTokenForUpdate(ctx, used, domain.PurposeEmailVerification)
		if err != nil {
			return err
		}
		if err := tx.MarkTokenUsed(ctx, tok.ID, time.Now()); err != nil {
			return err
		}
		return tx.CreateToken(ctx, domain.NewToken{UserID: id, Purpose: domain.PurposeEmailVerification, Hash: unused, ExpiresAt: time.Now().Add(time.Hour)})
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.RunInTx(ctx, func(tx domain.Tx) error {
		return tx.DeleteUnusedTokens(ctx, id, domain.PurposeEmailVerification)
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(ctx, "SELECT token_hash FROM auth_tokens WHERE user_id = $1", id)
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			t.Fatal(err)
		}
		left = append(left, h)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0] != used {
		t.Errorf("tokens left = %v, want only the used one", left)
	}
}
