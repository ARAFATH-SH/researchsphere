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
