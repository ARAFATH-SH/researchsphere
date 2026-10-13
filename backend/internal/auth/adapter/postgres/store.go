// Package postgres implements the auth domain's Store and Tx with the queries that sqlc
// generates from queries.sql.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// usersEmailKey is the unique constraint on users.email (a citext column, so the
// comparison ignores letter case).
const usersEmailKey = "users_email_key"

// Store reads and writes accounts through the shared pool.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

var (
	_ domain.Store = (*Store)(nil)
	_ domain.Tx    = tx{}
)

// NewStore returns a Store that uses pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: sqlcgen.New(pool)}
}

// RunInTx runs fn in one transaction and commits when it returns nil.
func (s *Store) RunInTx(ctx context.Context, fn func(domain.Tx) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(t pgx.Tx) error {
		return fn(tx{q: s.q.WithTx(t)})
	})
}

// FindUserByEmail returns domain.ErrNotFound when no user has the address.
func (s *Store) FindUserByEmail(ctx context.Context, email string) (domain.User, error) {
	row, err := s.q.FindUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("find user by email: %w", err)
	}
	return domain.User{
		ID:              uuidString(row.ID),
		Email:           row.Email,
		FullName:        row.FullName,
		EmailVerifiedAt: timePtr(row.EmailVerifiedAt),
	}, nil
}

// FindCredentialsByEmail returns domain.ErrNotFound when no user has the address.
func (s *Store) FindCredentialsByEmail(ctx context.Context, email string) (domain.Credentials, error) {
	row, err := s.q.FindCredentialsByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Credentials{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Credentials{}, fmt.Errorf("find credentials by email: %w", err)
	}
	return domain.Credentials{
		User: domain.User{
			ID:              uuidString(row.ID),
			Email:           row.Email,
			FullName:        row.FullName,
			EmailVerifiedAt: timePtr(row.EmailVerifiedAt),
		},
		PasswordHash: row.PasswordHash,
	}, nil
}

// FindUserByID returns domain.ErrNotFound when no user has the ID, or the ID is not a UUID.
func (s *Store) FindUserByID(ctx context.Context, userID string) (domain.User, error) {
	id, err := parseUUID(userID)
	if err != nil {
		return domain.User{}, domain.ErrNotFound
	}
	row, err := s.q.FindUserByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("find user by id: %w", err)
	}
	return domain.User{
		ID:              uuidString(row.ID),
		Email:           row.Email,
		FullName:        row.FullName,
		EmailVerifiedAt: timePtr(row.EmailVerifiedAt),
	}, nil
}

// FindCredentialsByID returns domain.ErrNotFound when no user has the ID, or the ID is
// not a UUID.
func (s *Store) FindCredentialsByID(ctx context.Context, userID string) (domain.Credentials, error) {
	id, err := parseUUID(userID)
	if err != nil {
		return domain.Credentials{}, domain.ErrNotFound
	}
	row, err := s.q.FindCredentialsByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Credentials{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Credentials{}, fmt.Errorf("find credentials by id: %w", err)
	}
	return domain.Credentials{
		User: domain.User{
			ID:              uuidString(row.ID),
			Email:           row.Email,
			FullName:        row.FullName,
			EmailVerifiedAt: timePtr(row.EmailVerifiedAt),
		},
		PasswordHash: row.PasswordHash,
	}, nil
}

// CreateSession stores a login with the hash of its refresh token.
func (s *Store) CreateSession(ctx context.Context, ns domain.NewSession) error {
	id, err := parseUUID(ns.UserID)
	if err != nil {
		return err
	}
	err = s.q.CreateSession(ctx, sqlcgen.CreateSessionParams{
		UserID:           id,
		RefreshTokenHash: ns.TokenHash,
		ExpiresAt:        timestamptz(ns.ExpiresAt),
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// RevokeSessionByToken returns domain.ErrNotFound when no live session has the hash.
func (s *Store) RevokeSessionByToken(ctx context.Context, hash string, at time.Time) (domain.Session, error) {
	row, err := s.q.RevokeSessionByTokenHash(ctx, sqlcgen.RevokeSessionByTokenHashParams{RefreshTokenHash: hash, RevokedAt: timestamptz(at)})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("revoke session by token: %w", err)
	}
	return session(row.ID, row.UserID, row.ExpiresAt, row.RevokedAt), nil
}

// tx runs the queries inside one transaction.
type tx struct {
	q *sqlcgen.Queries
}

func (t tx) CreateUser(ctx context.Context, u domain.NewUser) (string, error) {
	id, err := t.q.CreateUser(ctx, sqlcgen.CreateUserParams{Email: u.Email, PasswordHash: u.PasswordHash})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" && pgErr.ConstraintName == usersEmailKey {
		return "", domain.ErrEmailTaken
	}
	if err != nil {
		return "", fmt.Errorf("create user: %w", err)
	}
	return uuidString(id), nil
}

func (t tx) CreateProfile(ctx context.Context, userID, fullName string) error {
	id, err := parseUUID(userID)
	if err != nil {
		return err
	}
	if err := t.q.CreateProfile(ctx, sqlcgen.CreateProfileParams{UserID: id, FullName: fullName}); err != nil {
		return fmt.Errorf("create profile: %w", err)
	}
	return nil
}

func (t tx) CreateToken(ctx context.Context, nt domain.NewToken) error {
	id, err := parseUUID(nt.UserID)
	if err != nil {
		return err
	}
	err = t.q.CreateToken(ctx, sqlcgen.CreateTokenParams{
		UserID:    id,
		Purpose:   sqlcgen.AuthTokenPurpose(nt.Purpose),
		TokenHash: nt.Hash,
		ExpiresAt: timestamptz(nt.ExpiresAt),
	})
	if err != nil {
		return fmt.Errorf("create token: %w", err)
	}
	return nil
}

func (t tx) DeleteUnusedTokens(ctx context.Context, userID string, purpose domain.TokenPurpose) error {
	id, err := parseUUID(userID)
	if err != nil {
		return err
	}
	err = t.q.DeleteUnusedTokens(ctx, sqlcgen.DeleteUnusedTokensParams{UserID: id, Purpose: sqlcgen.AuthTokenPurpose(purpose)})
	if err != nil {
		return fmt.Errorf("delete unused tokens: %w", err)
	}
	return nil
}

func (t tx) FindTokenForUpdate(ctx context.Context, hash string, purpose domain.TokenPurpose) (domain.Token, error) {
	row, err := t.q.FindTokenForUpdate(ctx, sqlcgen.FindTokenForUpdateParams{TokenHash: hash, Purpose: sqlcgen.AuthTokenPurpose(purpose)})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Token{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Token{}, fmt.Errorf("find token: %w", err)
	}
	return domain.Token{
		ID:        uuidString(row.ID),
		UserID:    uuidString(row.UserID),
		ExpiresAt: row.ExpiresAt.Time,
		UsedAt:    timePtr(row.UsedAt),
	}, nil
}

func (t tx) MarkTokenUsed(ctx context.Context, tokenID string, at time.Time) error {
	id, err := parseUUID(tokenID)
	if err != nil {
		return err
	}
	if err := t.q.MarkTokenUsed(ctx, sqlcgen.MarkTokenUsedParams{ID: id, UsedAt: timestamptz(at)}); err != nil {
		return fmt.Errorf("mark token used: %w", err)
	}
	return nil
}

func (t tx) MarkEmailVerified(ctx context.Context, userID string, at time.Time) error {
	id, err := parseUUID(userID)
	if err != nil {
		return err
	}
	if err := t.q.MarkEmailVerified(ctx, sqlcgen.MarkEmailVerifiedParams{ID: id, VerifiedAt: timestamptz(at)}); err != nil {
		return fmt.Errorf("mark email verified: %w", err)
	}
	return nil
}

func (t tx) FindSessionForUpdate(ctx context.Context, hash string) (domain.Session, error) {
	row, err := t.q.FindSessionForUpdate(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("find session: %w", err)
	}
	return session(row.ID, row.UserID, row.ExpiresAt, row.RevokedAt), nil
}

func (t tx) FindSessionByRetiredTokenForUpdate(ctx context.Context, hash string) (domain.Session, error) {
	row, err := t.q.FindSessionByRetiredTokenForUpdate(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("find session by retired token: %w", err)
	}
	return session(row.ID, row.UserID, row.ExpiresAt, row.RevokedAt), nil
}

// RotateSession fails when the session no longer has oldHash, so a token is never
// retired for a session that did not hold it.
func (t tx) RotateSession(ctx context.Context, sessionID, oldHash, newHash string, at time.Time) error {
	id, err := parseUUID(sessionID)
	if err != nil {
		return err
	}
	n, err := t.q.UpdateSessionTokenHash(ctx, sqlcgen.UpdateSessionTokenHashParams{ID: id, OldHash: oldHash, NewHash: newHash})
	if err != nil {
		return fmt.Errorf("update session token: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("update session token: %d rows changed, want 1", n)
	}
	err = t.q.InsertRetiredToken(ctx, sqlcgen.InsertRetiredTokenParams{TokenHash: oldHash, SessionID: id, RetiredAt: timestamptz(at)})
	if err != nil {
		return fmt.Errorf("retire session token: %w", err)
	}
	return nil
}

func (t tx) RevokeSession(ctx context.Context, sessionID string, at time.Time) error {
	id, err := parseUUID(sessionID)
	if err != nil {
		return err
	}
	if err := t.q.RevokeSession(ctx, sqlcgen.RevokeSessionParams{ID: id, RevokedAt: timestamptz(at)}); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (t tx) UpdatePasswordHash(ctx context.Context, userID, hash string) error {
	id, err := parseUUID(userID)
	if err != nil {
		return err
	}
	if err := t.q.UpdatePasswordHash(ctx, sqlcgen.UpdatePasswordHashParams{ID: id, PasswordHash: hash}); err != nil {
		return fmt.Errorf("update password hash: %w", err)
	}
	return nil
}

func (t tx) RevokeUserSessions(ctx context.Context, userID string, at time.Time) error {
	id, err := parseUUID(userID)
	if err != nil {
		return err
	}
	if err := t.q.RevokeUserSessions(ctx, sqlcgen.RevokeUserSessionsParams{UserID: id, RevokedAt: timestamptz(at)}); err != nil {
		return fmt.Errorf("revoke user sessions: %w", err)
	}
	return nil
}

func (t tx) RevokeOtherUserSessions(ctx context.Context, userID, keepSessionID string, at time.Time) error {
	id, err := parseUUID(userID)
	if err != nil {
		return err
	}
	keep, err := parseUUID(keepSessionID)
	if err != nil {
		return err
	}
	err = t.q.RevokeOtherUserSessions(ctx, sqlcgen.RevokeOtherUserSessionsParams{UserID: id, KeepID: keep, RevokedAt: timestamptz(at)})
	if err != nil {
		return fmt.Errorf("revoke other user sessions: %w", err)
	}
	return nil
}

func (t tx) LockOwnedProjects(ctx context.Context, userID string) error {
	id, err := parseUUID(userID)
	if err != nil {
		return err
	}
	if err := t.q.LockOwnedProjects(ctx, id); err != nil {
		return fmt.Errorf("lock owned projects: %w", err)
	}
	return nil
}

func (t tx) FindSharedOwnedProjects(ctx context.Context, userID string) ([]domain.ProjectRef, error) {
	id, err := parseUUID(userID)
	if err != nil {
		return nil, err
	}
	rows, err := t.q.FindSharedOwnedProjects(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find shared owned projects: %w", err)
	}
	projects := make([]domain.ProjectRef, 0, len(rows))
	for _, r := range rows {
		projects = append(projects, domain.ProjectRef{ID: uuidString(r.ID), Title: r.Title})
	}
	return projects, nil
}

func (t tx) DeleteSoloProjects(ctx context.Context, userID string) (int64, error) {
	id, err := parseUUID(userID)
	if err != nil {
		return 0, err
	}
	n, err := t.q.DeleteSoloProjects(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("delete solo projects: %w", err)
	}
	return n, nil
}

// DeleteUserPublications locks the user's publications before it removes the links, so a
// co-author linking one at the same moment either commits first, and the publication
// stays, or waits until it is gone.
func (t tx) DeleteUserPublications(ctx context.Context, userID string) (int64, error) {
	id, err := parseUUID(userID)
	if err != nil {
		return 0, err
	}
	pubs, err := t.q.LockUserPublications(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("lock user publications: %w", err)
	}
	if err := t.q.DeleteUserPublicationLinks(ctx, id); err != nil {
		return 0, fmt.Errorf("delete user publication links: %w", err)
	}
	if len(pubs) == 0 {
		return 0, nil
	}
	n, err := t.q.DeleteAuthorlessPublications(ctx, pubs)
	if err != nil {
		return 0, fmt.Errorf("delete authorless publications: %w", err)
	}
	return n, nil
}

func (t tx) DeleteUser(ctx context.Context, userID string) error {
	id, err := parseUUID(userID)
	if err != nil {
		return domain.ErrNotFound
	}
	n, err := t.q.DeleteUser(ctx, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func session(id, userID pgtype.UUID, expiresAt, revokedAt pgtype.Timestamptz) domain.Session {
	return domain.Session{ID: uuidString(id), UserID: uuidString(userID), ExpiresAt: expiresAt.Time, RevokedAt: timePtr(revokedAt)}
}

func uuidString(id pgtype.UUID) string { return id.String() }

func parseUUID(s string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		return id, fmt.Errorf("parse id: %w", err)
	}
	return id, nil
}

func timestamptz(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
