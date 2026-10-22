package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// experienceRow is the shape every experience query returns; sqlc makes one type per
// query, and each converts to this one.
type experienceRow = sqlcgen.ListUserExperienceRow

// ListExperience returns the user's entries, current ones first (FR-4.8).
func (s *Store) ListExperience(ctx context.Context, userID string) ([]domain.Experience, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return []domain.Experience{}, nil
	}
	rows, err := s.q.ListUserExperience(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("list experience: %w", err)
	}
	out := make([]domain.Experience, 0, len(rows))
	for _, r := range rows {
		out = append(out, experience(r))
	}
	return out, nil
}

// DeleteExperience returns domain.ErrNotFound when the user has no entry with the ID, or
// an ID is not a UUID.
func (s *Store) DeleteExperience(ctx context.Context, userID, id string) error {
	uid, eid, err := parseIDs(userID, id)
	if err != nil {
		return domain.ErrNotFound
	}
	n, err := s.q.DeleteUserExperience(ctx, sqlcgen.DeleteUserExperienceParams{ID: eid, UserID: uid})
	if err != nil {
		return fmt.Errorf("delete experience: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// CountExperience returns how many entries the user has.
func (t tx) CountExperience(ctx context.Context, userID string) (int, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return 0, domain.ErrNotFound
	}
	n, err := t.q.CountUserExperience(ctx, uid)
	if err != nil {
		return 0, fmt.Errorf("count experience: %w", err)
	}
	return int(n), nil
}

// InsertExperience returns domain.ErrNotFound when the user does not exist.
func (t tx) InsertExperience(ctx context.Context, e domain.Experience) (domain.Experience, error) {
	uid, err := parseUUID(e.UserID)
	if err != nil {
		return domain.Experience{}, domain.ErrNotFound
	}
	d, err := datesOf(e)
	if err != nil {
		return domain.Experience{}, err
	}
	row, err := t.q.InsertExperience(ctx, sqlcgen.InsertExperienceParams{
		UserID:         uid,
		Title:          e.Title,
		Organization:   e.Organization,
		EmploymentType: string(e.EmploymentType),
		Location:       text(e.Location),
		LocationType:   locationTypeText(e.LocationType),
		StartYear:      d.startYear,
		StartMonth:     d.startMonth,
		EndYear:        d.endYear,
		EndMonth:       d.endMonth,
		IsCurrent:      e.IsCurrent,
		Description:    text(e.Description),
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == foreignKeyViolation {
		return domain.Experience{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Experience{}, fmt.Errorf("insert experience: %w", err)
	}
	return experience(experienceRow(row)), nil
}

// FindExperienceForUpdate returns domain.ErrNotFound when the user has no entry with the
// ID, or an ID is not a UUID.
func (t tx) FindExperienceForUpdate(ctx context.Context, userID, id string) (domain.Experience, error) {
	uid, eid, err := parseIDs(userID, id)
	if err != nil {
		return domain.Experience{}, domain.ErrNotFound
	}
	row, err := t.q.FindUserExperienceForUpdate(ctx, sqlcgen.FindUserExperienceForUpdateParams{ID: eid, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Experience{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Experience{}, fmt.Errorf("find experience: %w", err)
	}
	return experience(experienceRow(row)), nil
}

// UpdateExperience returns domain.ErrNotFound when e.UserID has no entry e.ID.
func (t tx) UpdateExperience(ctx context.Context, e domain.Experience) (domain.Experience, error) {
	uid, eid, err := parseIDs(e.UserID, e.ID)
	if err != nil {
		return domain.Experience{}, domain.ErrNotFound
	}
	d, err := datesOf(e)
	if err != nil {
		return domain.Experience{}, err
	}
	row, err := t.q.UpdateUserExperience(ctx, sqlcgen.UpdateUserExperienceParams{
		Title:          e.Title,
		Organization:   e.Organization,
		EmploymentType: string(e.EmploymentType),
		Location:       text(e.Location),
		LocationType:   locationTypeText(e.LocationType),
		StartYear:      d.startYear,
		StartMonth:     d.startMonth,
		EndYear:        d.endYear,
		EndMonth:       d.endMonth,
		IsCurrent:      e.IsCurrent,
		Description:    text(e.Description),
		ID:             eid,
		UserID:         uid,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Experience{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Experience{}, fmt.Errorf("update experience: %w", err)
	}
	return experience(experienceRow(row)), nil
}

// dates is an entry's start and end in the columns' types.
type dates struct {
	startYear, startMonth int16
	endYear, endMonth     pgtype.Int2
}

func datesOf(e domain.Experience) (dates, error) {
	var d dates
	var err error
	if d.startYear, err = int16Of(e.Start.Year); err != nil {
		return d, fmt.Errorf("start year: %w", err)
	}
	if d.startMonth, err = int16Of(e.Start.Month); err != nil {
		return d, fmt.Errorf("start month: %w", err)
	}
	if e.End == nil {
		return d, nil
	}
	y, err := int16Of(e.End.Year)
	if err != nil {
		return d, fmt.Errorf("end year: %w", err)
	}
	m, err := int16Of(e.End.Month)
	if err != nil {
		return d, fmt.Errorf("end month: %w", err)
	}
	d.endYear, d.endMonth = pgtype.Int2{Int16: y, Valid: true}, pgtype.Int2{Int16: m, Valid: true}
	return d, nil
}

func int16Of(n int) (int16, error) {
	if n < 0 || n > math.MaxInt16 {
		return 0, fmt.Errorf("%d out of range", n)
	}
	return int16(n), nil
}

func locationTypeText(t *domain.LocationType) pgtype.Text {
	if t == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: string(*t), Valid: true}
}

func experience(r experienceRow) domain.Experience {
	e := domain.Experience{
		ID:             r.ID.String(),
		UserID:         r.UserID.String(),
		Title:          r.Title,
		Organization:   r.Organization,
		EmploymentType: domain.EmploymentType(r.EmploymentType),
		Location:       textPtr(r.Location),
		Start:          domain.YearMonth{Year: int(r.StartYear), Month: int(r.StartMonth)},
		IsCurrent:      r.IsCurrent,
		Description:    textPtr(r.Description),
	}
	if r.LocationType.Valid {
		t := domain.LocationType(r.LocationType.String)
		e.LocationType = &t
	}
	if r.EndYear.Valid && r.EndMonth.Valid {
		e.End = &domain.YearMonth{Year: int(r.EndYear.Int16), Month: int(r.EndMonth.Int16)}
	}
	return e
}
