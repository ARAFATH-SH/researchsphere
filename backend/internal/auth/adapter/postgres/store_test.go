//go:build integration

// Repository tests against the test database. Run with `make test-integration` (needs
// `make up`); they read TEST_DATABASE_URL, and each test starts with every table empty.
package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

func newTestStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	pool := testenv.DB(t)
	return NewStore(pool), pool
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
	email := testenv.Email(t)
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
	s, _ := newTestStore(t)
	email := testenv.Email(t)

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
	email := testenv.Email(t)

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
	s, _ := newTestStore(t)
	email := testenv.Email(t)
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
	s, _ := newTestStore(t)
	email := testenv.Email(t)
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
	email := testenv.Email(t)
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

func TestFindCredentialsByEmail(t *testing.T) {
	ctx := t.Context()
	s, _ := newTestStore(t)
	email := testenv.Email(t)
	id, err := register(ctx, s, email, "Ada", rand.Text(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	c, err := s.FindCredentialsByEmail(ctx, strings.ToUpper(email))
	if err != nil {
		t.Fatalf("FindCredentialsByEmail in upper case: %v", err)
	}
	if c.ID != id || c.Email != email || c.FullName != "Ada" || c.PasswordHash != "hash" || c.Verified() {
		t.Errorf("credentials = %+v", c)
	}
	if _, err := s.FindCredentialsByEmail(ctx, "nobody-"+email); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown address: error = %v, want ErrNotFound", err)
	}
}

func TestFindUserByID(t *testing.T) {
	ctx := t.Context()
	s, _ := newTestStore(t)
	email := testenv.Email(t)
	id, err := register(ctx, s, email, "Ada", rand.Text(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	u, err := s.FindUserByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != id || u.Email != email || u.FullName != "Ada" {
		t.Errorf("user = %+v", u)
	}
	for _, other := range []string{"00000000-0000-0000-0000-000000000000", "not-a-uuid", ""} {
		if _, err := s.FindUserByID(ctx, other); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("FindUserByID(%q) error = %v, want ErrNotFound", other, err)
		}
	}
}

func TestCreateSession(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	email := testenv.Email(t)
	id, err := register(ctx, s, email, "Ada", rand.Text(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	hash := rand.Text()
	expires := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Microsecond)

	if err := s.CreateSession(ctx, domain.NewSession{UserID: id, TokenHash: hash, ExpiresAt: expires}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	var (
		gotUser    string
		gotExpires time.Time
		revoked    *time.Time
	)
	err = pool.QueryRow(ctx, "SELECT user_id::text, expires_at, revoked_at FROM sessions WHERE refresh_token_hash = $1", hash).
		Scan(&gotUser, &gotExpires, &revoked)
	if err != nil {
		t.Fatal(err)
	}
	if gotUser != id || !gotExpires.Equal(expires) || revoked != nil {
		t.Errorf("session = (%s, %v, %v), want (%s, %v, nil)", gotUser, gotExpires, revoked, id, expires)
	}

	// A second login is a second row; the same hash twice is refused.
	if err := s.CreateSession(ctx, domain.NewSession{UserID: id, TokenHash: rand.Text(), ExpiresAt: expires}); err != nil {
		t.Fatalf("second session: %v", err)
	}
	if err := s.CreateSession(ctx, domain.NewSession{UserID: id, TokenHash: hash, ExpiresAt: expires}); err == nil {
		t.Error("a duplicate refresh token hash was stored")
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id = $1", id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("sessions for the user = %d, want 2", n)
	}
}

// newSession registers a user with a new session and returns the user's ID and the
// session's refresh token hash.
func newSession(t *testing.T, s *Store, pool *pgxpool.Pool) (userID, hash string) {
	t.Helper()
	ctx := t.Context()
	userID, err := register(ctx, s, testenv.Email(t), "Ada", rand.Text(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	hash = rand.Text()
	expires := time.Now().Add(30 * 24 * time.Hour)
	if err := s.CreateSession(ctx, domain.NewSession{UserID: userID, TokenHash: hash, ExpiresAt: expires}); err != nil {
		t.Fatal(err)
	}
	return userID, hash
}

// findSession runs FindSessionForUpdate in its own transaction.
func findSession(t *testing.T, s *Store, hash string) (domain.Session, error) {
	t.Helper()
	var sess domain.Session
	err := s.RunInTx(t.Context(), func(tx domain.Tx) error {
		var err error
		sess, err = tx.FindSessionForUpdate(t.Context(), hash)
		return err
	})
	return sess, err
}

func TestRotateSession(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	userID, oldHash := newSession(t, s, pool)
	newHash := rand.Text()

	sess, err := findSession(t, s, oldHash)
	if err != nil {
		t.Fatalf("FindSessionForUpdate: %v", err)
	}
	if sess.UserID != userID || sess.RevokedAt != nil {
		t.Errorf("session = %+v, want the user's live session", sess)
	}
	err = s.RunInTx(ctx, func(tx domain.Tx) error {
		return tx.RotateSession(ctx, sess.ID, oldHash, newHash, time.Now())
	})
	if err != nil {
		t.Fatalf("RotateSession: %v", err)
	}

	if _, err := findSession(t, s, oldHash); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("FindSessionForUpdate(old hash) error = %v, want ErrNotFound", err)
	}
	if got, err := findSession(t, s, newHash); err != nil || got.ID != sess.ID {
		t.Errorf("FindSessionForUpdate(new hash) = %+v, %v; want the same session", got, err)
	}
	err = s.RunInTx(ctx, func(tx domain.Tx) error {
		got, err := tx.FindSessionByRetiredTokenForUpdate(ctx, oldHash)
		if err != nil || got.ID != sess.ID {
			t.Errorf("FindSessionByRetiredTokenForUpdate(old hash) = %+v, %v; want the session", got, err)
		}
		if _, err := tx.FindSessionByRetiredTokenForUpdate(ctx, newHash); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("FindSessionByRetiredTokenForUpdate(current hash) error = %v, want ErrNotFound", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Rotating from a hash the session no longer holds fails and changes nothing.
	err = s.RunInTx(ctx, func(tx domain.Tx) error {
		return tx.RotateSession(ctx, sess.ID, oldHash, rand.Text(), time.Now())
	})
	if err == nil {
		t.Error("RotateSession from a retired hash succeeded")
	}
	if got, err := findSession(t, s, newHash); err != nil || got.ID != sess.ID {
		t.Errorf("after a failed rotation the session must keep its hash; got %+v, %v", got, err)
	}
}

func TestRevokeSession(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	_, hash := newSession(t, s, pool)
	sess, err := findSession(t, s, hash)
	if err != nil {
		t.Fatal(err)
	}
	first := time.Now().Add(-time.Minute).Truncate(time.Microsecond)

	for _, at := range []time.Time{first, time.Now()} { // the second call keeps the first time
		err := s.RunInTx(ctx, func(tx domain.Tx) error { return tx.RevokeSession(ctx, sess.ID, at) })
		if err != nil {
			t.Fatalf("RevokeSession: %v", err)
		}
	}
	got, err := findSession(t, s, hash)
	if err != nil {
		t.Fatal(err)
	}
	if got.RevokedAt == nil || !got.RevokedAt.Equal(first) {
		t.Errorf("RevokedAt = %v, want the first revocation time %v", got.RevokedAt, first)
	}
}

func TestRevokeSessionByToken(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	userID, hash := newSession(t, s, pool)
	_, otherHash := newSession(t, s, pool)
	at := time.Now().Truncate(time.Microsecond)

	sess, err := s.RevokeSessionByToken(ctx, hash, at)
	if err != nil {
		t.Fatalf("RevokeSessionByToken: %v", err)
	}
	if sess.UserID != userID || sess.RevokedAt == nil || !sess.RevokedAt.Equal(at) {
		t.Errorf("revoked session = %+v, want the user's, revoked at %v", sess, at)
	}
	if _, err := s.RevokeSessionByToken(ctx, hash, time.Now()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second RevokeSessionByToken error = %v, want ErrNotFound", err)
	}
	if _, err := s.RevokeSessionByToken(ctx, rand.Text(), time.Now()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("RevokeSessionByToken(unknown hash) error = %v, want ErrNotFound", err)
	}
	if other, err := findSession(t, s, otherHash); err != nil || other.RevokedAt != nil {
		t.Errorf("another session = %+v, %v; want it untouched", other, err)
	}
}

func TestUpdatePasswordHash(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	email := testenv.Email(t)
	id, err := register(ctx, s, email, "Ada", rand.Text(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	other, err := register(ctx, s, testenv.Email(t), "Grace", rand.Text(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	err = s.RunInTx(ctx, func(tx domain.Tx) error { return tx.UpdatePasswordHash(ctx, id, "new-hash") })
	if err != nil {
		t.Fatalf("UpdatePasswordHash: %v", err)
	}
	creds, err := s.FindCredentialsByEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	if creds.PasswordHash != "new-hash" {
		t.Errorf("password hash = %q, want new-hash", creds.PasswordHash)
	}
	var otherHash string
	if err := pool.QueryRow(ctx, "SELECT password_hash FROM users WHERE id = $1", other).Scan(&otherHash); err != nil {
		t.Fatal(err)
	}
	if otherHash != "hash" {
		t.Errorf("another user's hash = %q, want it untouched", otherHash)
	}
}

func TestRevokeUserSessions(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	userID, first := newSession(t, s, pool)
	_, otherHash := newSession(t, s, pool)

	second, ended := rand.Text(), rand.Text()
	expires := time.Now().Add(30 * 24 * time.Hour)
	for _, h := range []string{second, ended} {
		if err := s.CreateSession(ctx, domain.NewSession{UserID: userID, TokenHash: h, ExpiresAt: expires}); err != nil {
			t.Fatal(err)
		}
	}
	endedAt := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	if _, err := s.RevokeSessionByToken(ctx, ended, endedAt); err != nil {
		t.Fatal(err)
	}

	at := time.Now().Truncate(time.Microsecond)
	if err := s.RunInTx(ctx, func(tx domain.Tx) error { return tx.RevokeUserSessions(ctx, userID, at) }); err != nil {
		t.Fatalf("RevokeUserSessions: %v", err)
	}

	for _, tt := range []struct {
		name, hash string
		want       time.Time
	}{
		{"first live session", first, at},
		{"second live session", second, at},
		{"session that had already ended", ended, endedAt},
	} {
		got, err := findSession(t, s, tt.hash)
		if err != nil {
			t.Fatal(err)
		}
		if got.RevokedAt == nil || !got.RevokedAt.Equal(tt.want) {
			t.Errorf("%s: RevokedAt = %v, want %v", tt.name, got.RevokedAt, tt.want)
		}
	}
	if other, err := findSession(t, s, otherHash); err != nil || other.RevokedAt != nil {
		t.Errorf("another user's session = %+v, %v; want it untouched", other, err)
	}
}

func TestFindCredentialsByID(t *testing.T) {
	ctx := t.Context()
	s, _ := newTestStore(t)
	email := testenv.Email(t)
	id, err := register(ctx, s, email, "Ada", rand.Text(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	c, err := s.FindCredentialsByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != id || c.Email != email || c.FullName != "Ada" || c.PasswordHash != "hash" || c.Verified() {
		t.Errorf("credentials = %+v", c)
	}
	for _, other := range []string{"00000000-0000-0000-0000-000000000000", "not-a-uuid", ""} {
		if _, err := s.FindCredentialsByID(ctx, other); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("FindCredentialsByID(%q) error = %v, want ErrNotFound", other, err)
		}
	}
}

func TestRevokeOtherUserSessions(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	userID, kept := newSession(t, s, pool)
	_, otherHash := newSession(t, s, pool)

	second, ended := rand.Text(), rand.Text()
	expires := time.Now().Add(30 * 24 * time.Hour)
	for _, h := range []string{second, ended} {
		if err := s.CreateSession(ctx, domain.NewSession{UserID: userID, TokenHash: h, ExpiresAt: expires}); err != nil {
			t.Fatal(err)
		}
	}
	endedAt := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	if _, err := s.RevokeSessionByToken(ctx, ended, endedAt); err != nil {
		t.Fatal(err)
	}
	keep, err := findSession(t, s, kept)
	if err != nil {
		t.Fatal(err)
	}

	at := time.Now().Truncate(time.Microsecond)
	err = s.RunInTx(ctx, func(tx domain.Tx) error { return tx.RevokeOtherUserSessions(ctx, userID, keep.ID, at) })
	if err != nil {
		t.Fatalf("RevokeOtherUserSessions: %v", err)
	}

	if got, err := findSession(t, s, kept); err != nil || got.RevokedAt != nil {
		t.Errorf("kept session = %+v, %v; want it live", got, err)
	}
	for _, tt := range []struct {
		name, hash string
		want       time.Time
	}{
		{"other live session", second, at},
		{"session that had already ended", ended, endedAt},
	} {
		got, err := findSession(t, s, tt.hash)
		if err != nil {
			t.Fatal(err)
		}
		if got.RevokedAt == nil || !got.RevokedAt.Equal(tt.want) {
			t.Errorf("%s: RevokedAt = %v, want %v", tt.name, got.RevokedAt, tt.want)
		}
	}
	if other, err := findSession(t, s, otherHash); err != nil || other.RevokedAt != nil {
		t.Errorf("another user's session = %+v, %v; want it untouched", other, err)
	}
}

// TestSuspendedAtIsRead sets suspended_at directly, as an administrator will (FR-15.3),
// and checks that every read the auth usecase checks it with returns it (FR-3.4).
func TestSuspendedAtIsRead(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	email := testenv.Email(t)
	tokenHash := rand.Text()
	id, err := register(ctx, s, email, "Ada", tokenHash, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	sessHash := rand.Text()
	if err := s.CreateSession(ctx, domain.NewSession{UserID: id, TokenHash: sessHash, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	read := func(t *testing.T) map[string]*time.Time {
		t.Helper()
		got := map[string]*time.Time{}
		u, err := s.FindUserByEmail(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		got["FindUserByEmail"] = u.SuspendedAt
		c, err := s.FindCredentialsByEmail(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		got["FindCredentialsByEmail"] = c.SuspendedAt
		if u, err = s.FindUserByID(ctx, id); err != nil {
			t.Fatal(err)
		}
		got["FindUserByID"] = u.SuspendedAt
		if c, err = s.FindCredentialsByID(ctx, id); err != nil {
			t.Fatal(err)
		}
		got["FindCredentialsByID"] = c.SuspendedAt
		sess, err := findSession(t, s, sessHash)
		if err != nil {
			t.Fatal(err)
		}
		got["FindSessionForUpdate"] = sess.UserSuspendedAt
		err = s.RunInTx(ctx, func(tx domain.Tx) error {
			tok, err := tx.FindTokenForUpdate(ctx, tokenHash, domain.PurposeEmailVerification)
			got["FindTokenForUpdate"] = tok.UserSuspendedAt
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	for name, at := range read(t) {
		if at != nil {
			t.Errorf("%s: SuspendedAt = %v before suspension, want nil", name, at)
		}
	}

	suspended := time.Now().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, "UPDATE users SET suspended_at = $1 WHERE id = $2", suspended, id); err != nil {
		t.Fatal(err)
	}
	for name, at := range read(t) {
		if at == nil || !at.Equal(suspended) {
			t.Errorf("%s: SuspendedAt = %v, want %v", name, at, suspended)
		}
	}
}

// TestRoleIsRead checks that the two reads whose role is signed into the access token,
// at login and at refresh, return users.role, including after it changes (SRS 2.3).
func TestRoleIsRead(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	email := testenv.Email(t)
	id, err := register(ctx, s, email, "Ada", rand.Text(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	sessHash := rand.Text()
	if err := s.CreateSession(ctx, domain.NewSession{UserID: id, TokenHash: sessHash, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"researcher", "admin"} {
		if _, err := pool.Exec(ctx, "UPDATE users SET role = $1 WHERE id = $2", want, id); err != nil {
			t.Fatal(err)
		}
		c, err := s.FindCredentialsByEmail(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		if c.Role != want {
			t.Errorf("FindCredentialsByEmail: Role = %q, want %q", c.Role, want)
		}
		sess, err := findSession(t, s, sessHash)
		if err != nil {
			t.Fatal(err)
		}
		if sess.UserRole != want {
			t.Errorf("FindSessionForUpdate: UserRole = %q, want %q", sess.UserRole, want)
		}
	}
}
