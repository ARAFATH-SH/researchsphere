// Package postgres implements the moderation domain's Store with the queries that sqlc generates
// from queries.sql. Every query runs in the transaction the context carries
// (platform/postgres.Transactor), so an action's rows commit together.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// Store reads and writes moderation's tables through the shared pool.
type Store struct {
	pool *pgxpool.Pool
}

var _ domain.Store = (*Store)(nil)

// NewStore returns a Store that uses pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Conn(ctx, s.pool))
}

// AddWarning stores a warning. The user ID comes from a locked row.
func (s *Store) AddWarning(ctx context.Context, userID, reason string) (string, error) {
	user, ok := parseUUID(userID)
	if !ok {
		return "", fmt.Errorf("add warning: user %q is not a UUID", userID)
	}
	id, err := s.q(ctx).InsertWarning(ctx, sqlcgen.InsertWarningParams{UserID: user, Reason: reason})
	if err != nil {
		return "", fmt.Errorf("add warning: %w", err)
	}
	return id.String(), nil
}

// PendingWarnings finds nothing for a user that is not a UUID.
func (s *Store) PendingWarnings(ctx context.Context, userID string, limit int) ([]domain.Warning, error) {
	user, ok := parseUUID(userID)
	if !ok {
		return []domain.Warning{}, nil
	}
	lim, err := int32Of(limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.q(ctx).ListPendingWarnings(ctx, sqlcgen.ListPendingWarningsParams{UserID: user, Lim: lim})
	if err != nil {
		return nil, fmt.Errorf("list pending warnings: %w", err)
	}
	out := make([]domain.Warning, len(rows))
	for i, r := range rows {
		out[i] = domain.Warning{ID: r.ID.String(), Reason: r.Reason, CreatedAt: r.CreatedAt.Time, AcknowledgedAt: timePtr(r.AcknowledgedAt)}
	}
	return out, nil
}

// FindWarning returns domain.ErrNotFound for an unknown ID, also for another user's warning.
func (s *Store) FindWarning(ctx context.Context, id, userID string) (domain.Warning, error) {
	wid, ok1 := parseUUID(id)
	user, ok2 := parseUUID(userID)
	if !ok1 || !ok2 {
		return domain.Warning{}, domain.ErrNotFound
	}
	r, err := s.q(ctx).FindWarning(ctx, sqlcgen.FindWarningParams{ID: wid, UserID: user})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Warning{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Warning{}, fmt.Errorf("find warning: %w", err)
	}
	return domain.Warning{ID: r.ID.String(), Reason: r.Reason, CreatedAt: r.CreatedAt.Time, AcknowledgedAt: timePtr(r.AcknowledgedAt)}, nil
}

// AcknowledgeWarning keeps an earlier time.
func (s *Store) AcknowledgeWarning(ctx context.Context, id, userID string, at time.Time) (bool, error) {
	wid, ok1 := parseUUID(id)
	user, ok2 := parseUUID(userID)
	if !ok1 || !ok2 {
		return false, nil
	}
	n, err := s.q(ctx).AcknowledgeWarning(ctx, sqlcgen.AcknowledgeWarningParams{ID: wid, UserID: user, At: timestamptz(at)})
	if err != nil {
		return false, fmt.Errorf("acknowledge warning: %w", err)
	}
	return n > 0, nil
}

// AddRestriction stores a restriction. The user ID comes from a locked row.
func (s *Store) AddRestriction(ctx context.Context, userID, kind string, startsAt, endsAt time.Time) (string, error) {
	user, ok := parseUUID(userID)
	if !ok {
		return "", fmt.Errorf("add restriction: user %q is not a UUID", userID)
	}
	id, err := s.q(ctx).InsertRestriction(ctx, sqlcgen.InsertRestrictionParams{
		UserID: user, Kind: kind, StartsAt: timestamptz(startsAt), EndsAt: timestamptz(endsAt),
	})
	if err != nil {
		return "", fmt.Errorf("add restriction: %w", err)
	}
	return id.String(), nil
}

// HasActiveRestriction is false for a user that is not a UUID.
func (s *Store) HasActiveRestriction(ctx context.Context, userID, kind string, now time.Time) (bool, error) {
	user, ok := parseUUID(userID)
	if !ok {
		return false, nil
	}
	active, err := s.q(ctx).HasActiveRestriction(ctx, sqlcgen.HasActiveRestrictionParams{UserID: user, Kind: kind, Now: timestamptz(now)})
	if err != nil {
		return false, fmt.Errorf("find active restriction: %w", err)
	}
	return active, nil
}

// RestrictionUser returns domain.ErrNotFound for an unknown ID.
func (s *Store) RestrictionUser(ctx context.Context, id string) (string, error) {
	rid, ok := parseUUID(id)
	if !ok {
		return "", domain.ErrNotFound
	}
	user, err := s.q(ctx).RestrictionUser(ctx, rid)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("find restriction: %w", err)
	}
	return user.String(), nil
}

// LiftRestriction ends an active restriction early.
func (s *Store) LiftRestriction(ctx context.Context, id, adminID string, now time.Time) (bool, error) {
	rid, ok1 := parseUUID(id)
	admin, ok2 := parseUUID(adminID)
	if !ok1 || !ok2 {
		return false, nil
	}
	n, err := s.q(ctx).LiftRestriction(ctx, sqlcgen.LiftRestrictionParams{
		ID: rid, Now: timestamptz(now), AdminID: admin,
	})
	if err != nil {
		return false, fmt.Errorf("lift restriction: %w", err)
	}
	return n > 0, nil
}

// AddAction writes one audit row.
func (s *Store) AddAction(ctx context.Context, a domain.NewAction) (string, error) {
	user, ok1 := parseUUID(a.UserID)
	admin, ok2 := parseUUID(a.AdminID)
	if !ok1 || !ok2 {
		return "", fmt.Errorf("add action: user %q or admin %q is not a UUID", a.UserID, a.AdminID)
	}
	var note pgtype.Text
	if a.Note != nil {
		note = pgtype.Text{String: *a.Note, Valid: true}
	}
	var ends pgtype.Timestamptz
	if a.EndsAt != nil {
		ends = timestamptz(*a.EndsAt)
	}
	id, err := s.q(ctx).InsertAction(ctx, sqlcgen.InsertActionParams{
		UserID: user, AdminID: admin, Action: string(a.Kind), Reason: a.Reason,
		Note: note, ReportID: optionalUUID(a.ReportID), WarningID: optionalUUID(a.WarningID),
		RestrictionID: optionalUUID(a.RestrictionID), EndsAt: ends,
	})
	if err != nil {
		return "", fmt.Errorf("add action: %w", err)
	}
	return id.String(), nil
}

// FindAction returns domain.ErrNotFound for an unknown ID.
func (s *Store) FindAction(ctx context.Context, id string) (domain.Action, error) {
	aid, ok := parseUUID(id)
	if !ok {
		return domain.Action{}, domain.ErrNotFound
	}
	r, err := s.q(ctx).FindAction(ctx, aid)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Action{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Action{}, fmt.Errorf("find action: %w", err)
	}
	return action(sqlcgen.ListActionsRow(r)), nil
}

// ListActions finds nothing for a user that is not a UUID.
func (s *Store) ListActions(ctx context.Context, userID string, limit, offset int) ([]domain.Action, int, error) {
	user, ok := parseUUID(userID)
	if !ok {
		return []domain.Action{}, 0, nil
	}
	lim, err := int32Of(limit)
	if err != nil {
		return nil, 0, err
	}
	off, err := int32Of(offset)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.q(ctx).ListActions(ctx, sqlcgen.ListActionsParams{UserID: user, Lim: lim, Off: off})
	if err != nil {
		return nil, 0, fmt.Errorf("list actions: %w", err)
	}
	total, err := s.q(ctx).CountActions(ctx, user)
	if err != nil {
		return nil, 0, fmt.Errorf("count actions: %w", err)
	}
	out := make([]domain.Action, len(rows))
	for i, r := range rows {
		out[i] = action(r)
	}
	return out, int(total), nil
}

// Standing has the zero value for a user that does not exist.
func (s *Store) Standing(ctx context.Context, userID string, now time.Time) (domain.StoredStanding, error) {
	user, ok := parseUUID(userID)
	if !ok {
		return domain.StoredStanding{}, nil
	}
	r, err := s.q(ctx).FindStanding(ctx, sqlcgen.FindStandingParams{ID: user, Now: timestamptz(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StoredStanding{}, nil
	}
	if err != nil {
		return domain.StoredStanding{}, fmt.Errorf("find standing: %w", err)
	}
	out := domain.StoredStanding{
		SuspendedAt: timePtr(r.SuspendedAt), SuspendedUntil: timePtr(r.SuspendedUntil), RestrictedUntil: timePtr(r.RestrictedUntil),
	}
	if r.SuspensionReason.Valid {
		out.SuspensionReason = &r.SuspensionReason.String
	}
	return out, nil
}

func action(r sqlcgen.ListActionsRow) domain.Action {
	a := domain.Action{
		ID: r.ID.String(), UserID: r.UserID.String(), AdminID: uuidPtr(r.AdminID),
		Kind: domain.ActionKind(r.Action), Reason: r.Reason, ReportID: uuidPtr(r.ReportID),
		WarningID: uuidPtr(r.WarningID), RestrictionID: uuidPtr(r.RestrictionID), EndsAt: timePtr(r.EndsAt),
		AcknowledgedAt: timePtr(r.AcknowledgedAt), LiftedAt: timePtr(r.LiftedAt), CreatedAt: r.CreatedAt.Time,
	}
	if r.Note.Valid {
		a.Note = &r.Note.String
	}
	if r.AdminName.Valid {
		a.AdminName = &r.AdminName.String
	}
	if r.LinkedReportID.Valid {
		a.Report = &domain.LinkedReport{
			ID: r.LinkedReportID.String(), Reason: r.ReportReason.String, Outcome: r.ReportOutcome.String, CreatedAt: r.ReportCreatedAt.Time,
		}
	}
	return a
}

func parseUUID(s string) (pgtype.UUID, bool) {
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		return id, false
	}
	return id, true
}

// optionalUUID is NULL for nil. The IDs come from rows the action read or wrote, except a
// report's, which the usecase has already found.
func optionalUUID(s *string) pgtype.UUID {
	if s == nil {
		return pgtype.UUID{}
	}
	id, _ := parseUUID(*s)
	return id
}

func uuidPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := u.String()
	return &s
}

func timestamptz(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

func int32Of(n int) (int32, error) {
	if n < 0 || n > math.MaxInt32 {
		return 0, fmt.Errorf("value %d does not fit a query parameter", n)
	}
	return int32(n), nil
}
