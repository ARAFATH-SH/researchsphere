// Package postgres implements the auth domain's Store and Tx with the queries that sqlc
// generates from queries.sql.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
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
		SuspendedAt:     timePtr(row.SuspendedAt),
		SuspendedUntil:  timePtr(row.SuspendedUntil),
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
			ID:               uuidString(row.ID),
			Email:            row.Email,
			FullName:         row.FullName,
			EmailVerifiedAt:  timePtr(row.EmailVerifiedAt),
			SuspendedAt:      timePtr(row.SuspendedAt),
			SuspendedUntil:   timePtr(row.SuspendedUntil),
			SuspensionReason: textPtr(row.SuspensionReason),
			Role:             string(row.Role),
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
		ID:               uuidString(row.ID),
		Email:            row.Email,
		FullName:         row.FullName,
		EmailVerifiedAt:  timePtr(row.EmailVerifiedAt),
		SuspendedAt:      timePtr(row.SuspendedAt),
		SuspendedUntil:   timePtr(row.SuspendedUntil),
		SuspensionReason: textPtr(row.SuspensionReason),
		Role:             string(row.Role),
		CreatedAt:        row.CreatedAt.Time,
	}, nil
}

// conn returns the queries on the context's transaction, or on the pool. The methods moderation
// calls use it, so they join moderation's transaction (platform/postgres.Transactor).
func (s *Store) conn(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Conn(ctx, s.pool))
}

// ClearExpiredSuspension clears the suspension when it ended at or before now.
func (s *Store) ClearExpiredSuspension(ctx context.Context, userID string, now time.Time) error {
	return clearExpiredSuspension(ctx, s.q, userID, now)
}

func clearExpiredSuspension(ctx context.Context, q *sqlcgen.Queries, userID string, now time.Time) error {
	id, err := parseUUID(userID)
	if err != nil {
		return err
	}
	if err := q.ClearExpiredSuspension(ctx, sqlcgen.ClearExpiredSuspensionParams{ID: id, Now: timestamptz(now)}); err != nil {
		return fmt.Errorf("clear expired suspension: %w", err)
	}
	return nil
}

// LockUser returns domain.ErrNotFound when no user has the ID, or the ID is not a UUID.
func (s *Store) LockUser(ctx context.Context, userID string) (domain.User, error) {
	id, err := parseUUID(userID)
	if err != nil {
		return domain.User{}, domain.ErrNotFound
	}
	row, err := s.conn(ctx).LockUser(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("lock user: %w", err)
	}
	return domain.User{
		ID:              uuidString(row.ID),
		Email:           row.Email,
		FullName:        row.FullName,
		EmailVerifiedAt: timePtr(row.EmailVerifiedAt),
		SuspendedAt:     timePtr(row.SuspendedAt),
		SuspendedUntil:  timePtr(row.SuspendedUntil),
		Role:            string(row.Role),
	}, nil
}

// Suspend stores the suspension and ends the user's sessions, both on the context's
// transaction. The caller holds LockUser's row lock.
func (s *Store) Suspend(ctx context.Context, userID string, at time.Time, until *time.Time, reason string) error {
	id, err := parseUUID(userID)
	if err != nil {
		return err
	}
	q := s.conn(ctx)
	var ends pgtype.Timestamptz
	if until != nil {
		ends = timestamptz(*until)
	}
	if err := q.SuspendUser(ctx, sqlcgen.SuspendUserParams{
		ID: id, SuspendedAt: timestamptz(at), SuspendedUntil: ends, Reason: pgtype.Text{String: reason, Valid: true},
	}); err != nil {
		return fmt.Errorf("suspend user: %w", err)
	}
	if err := q.RevokeUserSessions(ctx, sqlcgen.RevokeUserSessionsParams{UserID: id, RevokedAt: timestamptz(at)}); err != nil {
		return fmt.Errorf("revoke user sessions: %w", err)
	}
	return nil
}

// Restore clears the suspension on the context's transaction.
func (s *Store) Restore(ctx context.Context, userID string) error {
	id, err := parseUUID(userID)
	if err != nil {
		return err
	}
	if err := s.conn(ctx).RestoreUser(ctx, id); err != nil {
		return fmt.Errorf("restore user: %w", err)
	}
	return nil
}

// ListUsers returns at most limit users after skipping offset, newest first.
func (s *Store) ListUsers(ctx context.Context, limit, offset int) ([]domain.User, error) {
	l, err := int32Of(limit)
	if err != nil {
		return nil, err
	}
	o, err := int32Of(offset)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListUsers(ctx, sqlcgen.ListUsersParams{PageLimit: l, PageOffset: o})
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	users := make([]domain.User, 0, len(rows))
	for _, row := range rows {
		users = append(users, domain.User{
			ID:               uuidString(row.ID),
			Email:            row.Email,
			FullName:         row.FullName,
			EmailVerifiedAt:  timePtr(row.EmailVerifiedAt),
			SuspendedAt:      timePtr(row.SuspendedAt),
			SuspendedUntil:   timePtr(row.SuspendedUntil),
			SuspensionReason: textPtr(row.SuspensionReason),
			Role:             string(row.Role),
			CreatedAt:        row.CreatedAt.Time,
		})
	}
	return users, nil
}

// CountUsers returns how many users exist.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	n, err := s.q.CountUsers(ctx)
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return int(n), nil
}

// SetRoleByEmail gives the user with email the platform role, ignoring letter case in
// the address. It returns domain.ErrNotFound when no user has the address. It is not
// part of domain.Store on purpose: only the command line (cmd/setrole, `make admin`)
// changes a role, never the API (docs/decisions.md).
func (s *Store) SetRoleByEmail(ctx context.Context, email, role string) error {
	n, err := s.q.SetUserRoleByEmail(ctx, sqlcgen.SetUserRoleByEmailParams{Email: email, Role: sqlcgen.UserRole(role)})
	if err != nil {
		return fmt.Errorf("set user role: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// int32Of converts n for a LIMIT or OFFSET parameter, refusing values that do not fit.
func int32Of(n int) (int32, error) {
	if n < 0 || n > math.MaxInt32 {
		return 0, fmt.Errorf("value %d out of range for a query parameter", n)
	}
	return int32(n), nil
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
			SuspendedAt:     timePtr(row.SuspendedAt),
			SuspendedUntil:  timePtr(row.SuspendedUntil),
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

		UserSuspendedAt:    timePtr(row.SuspendedAt),
		UserSuspendedUntil: timePtr(row.SuspendedUntil),
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

func (t tx) ClearExpiredSuspension(ctx context.Context, userID string, now time.Time) error {
	return clearExpiredSuspension(ctx, t.q, userID, now)
}

func (t tx) FindSessionForUpdate(ctx context.Context, hash string) (domain.Session, error) {
	row, err := t.q.FindSessionForUpdate(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("find session: %w", err)
	}
	sess := session(row.ID, row.UserID, row.ExpiresAt, row.RevokedAt)
	sess.UserSuspendedAt = timePtr(row.SuspendedAt)
	sess.UserSuspendedUntil = timePtr(row.SuspendedUntil)
	sess.UserRole = string(row.Role)
	sess.UserVerified = row.UserVerified
	return sess, nil
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

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

// ListEndedSuspensions returns the IDs of at most limit users whose timed suspension ended at
// or before now and is not cleared yet, the longest ended first.
func (s *Store) ListEndedSuspensions(ctx context.Context, now time.Time, limit int) ([]string, error) {
	rows, err := s.q.ListEndedSuspensions(ctx, sqlcgen.ListEndedSuspensionsParams{
		Now: timestamptz(now), MaxUsers: int32(min(max(limit, 0), 10_000)), //nolint:gosec // capped
	})
	if err != nil {
		return nil, fmt.Errorf("list ended suspensions: %w", err)
	}
	ids := make([]string, len(rows))
	for i, id := range rows {
		ids[i] = uuidString(id)
	}
	return ids, nil
}

// ListUnverifiedBefore returns the IDs of at most limit accounts still unverified that
// signed up before cutoff, oldest first, leaving out addresses at skipDomains (FR-17.4).
func (s *Store) ListUnverifiedBefore(ctx context.Context, cutoff time.Time, skipDomains []string, limit int) ([]string, error) {
	rows, err := s.q.ListUnverifiedUsersBefore(ctx, sqlcgen.ListUnverifiedUsersBeforeParams{
		Cutoff: timestamptz(cutoff), SkipDomains: skipDomains, MaxUsers: int32(min(limit, 10_000)), //nolint:gosec // capped
	})
	if err != nil {
		return nil, fmt.Errorf("list unverified users: %w", err)
	}
	ids := make([]string, len(rows))
	for i, id := range rows {
		ids[i] = uuidString(id)
	}
	return ids, nil
}

// LockUnverifiedBefore locks the user until the transaction ends and reports whether it is
// still unverified and signed up before cutoff. A user that no longer exists reports false.
func (t tx) LockUnverifiedBefore(ctx context.Context, userID string, cutoff time.Time) (bool, error) {
	id, err := parseUUID(userID)
	if err != nil {
		return false, nil
	}
	purge, err := t.q.LockUnverifiedUserBefore(ctx, sqlcgen.LockUnverifiedUserBeforeParams{ID: id, Cutoff: timestamptz(cutoff)})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock unverified user: %w", err)
	}
	return purge, nil
}
