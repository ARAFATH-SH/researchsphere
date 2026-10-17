package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// educationRow is the shape every education query returns; sqlc makes one type per
// query, and each converts to this one.
type educationRow = sqlcgen.ListUserEducationRow

// ListEducation returns the user's entries, current ones first (FR-4.4).
func (s *Store) ListEducation(ctx context.Context, userID string) ([]domain.Education, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return []domain.Education{}, nil
	}
	rows, err := s.q.ListUserEducation(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("list education: %w", err)
	}
	out := make([]domain.Education, 0, len(rows))
	for _, r := range rows {
		out = append(out, education(r))
	}
	return out, nil
}

// DeleteEducation returns domain.ErrNotFound when the user has no entry with the ID, or
// an ID is not a UUID.
func (s *Store) DeleteEducation(ctx context.Context, userID, id string) error {
	uid, eid, err := parseIDs(userID, id)
	if err != nil {
		return domain.ErrNotFound
	}
	n, err := s.q.DeleteUserEducation(ctx, sqlcgen.DeleteUserEducationParams{ID: eid, UserID: uid})
	if err != nil {
		return fmt.Errorf("delete education: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// CountEducation returns how many entries the user has.
func (t tx) CountEducation(ctx context.Context, userID string) (int, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return 0, domain.ErrNotFound
	}
	n, err := t.q.CountUserEducation(ctx, uid)
	if err != nil {
		return 0, fmt.Errorf("count education: %w", err)
	}
	return int(n), nil
}

// InsertEducation returns domain.ErrNotFound when the user does not exist.
func (t tx) InsertEducation(ctx context.Context, e domain.Education) (domain.Education, error) {
	uid, err := parseUUID(e.UserID)
	if err != nil {
		return domain.Education{}, domain.ErrNotFound
	}
	start, end, err := years(e)
	if err != nil {
		return domain.Education{}, err
	}
	row, err := t.q.InsertEducation(ctx, sqlcgen.InsertEducationParams{
		UserID:       uid,
		Institution:  e.Institution,
		Degree:       text(e.Degree),
		FieldOfStudy: text(e.FieldOfStudy),
		StartYear:    start,
		EndYear:      end,
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == foreignKeyViolation {
		return domain.Education{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Education{}, fmt.Errorf("insert education: %w", err)
	}
	return education(educationRow(row)), nil
}

// FindEducationForUpdate returns domain.ErrNotFound when the user has no entry with the
// ID, or an ID is not a UUID.
func (t tx) FindEducationForUpdate(ctx context.Context, userID, id string) (domain.Education, error) {
	uid, eid, err := parseIDs(userID, id)
	if err != nil {
		return domain.Education{}, domain.ErrNotFound
	}
	row, err := t.q.FindUserEducationForUpdate(ctx, sqlcgen.FindUserEducationForUpdateParams{ID: eid, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Education{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Education{}, fmt.Errorf("find education: %w", err)
	}
	return education(educationRow(row)), nil
}

// UpdateEducation returns domain.ErrNotFound when e.UserID has no entry e.ID.
func (t tx) UpdateEducation(ctx context.Context, e domain.Education) (domain.Education, error) {
	uid, eid, err := parseIDs(e.UserID, e.ID)
	if err != nil {
		return domain.Education{}, domain.ErrNotFound
	}
	start, end, err := years(e)
	if err != nil {
		return domain.Education{}, err
	}
	row, err := t.q.UpdateUserEducation(ctx, sqlcgen.UpdateUserEducationParams{
		Institution:  e.Institution,
		Degree:       text(e.Degree),
		FieldOfStudy: text(e.FieldOfStudy),
		StartYear:    start,
		EndYear:      end,
		ID:           eid,
		UserID:       uid,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Education{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Education{}, fmt.Errorf("update education: %w", err)
	}
	return education(educationRow(row)), nil
}

func years(e domain.Education) (int32, pgtype.Int4, error) {
	start, err := int32Of(e.StartYear)
	if err != nil {
		return 0, pgtype.Int4{}, fmt.Errorf("start year: %w", err)
	}
	if e.EndYear == nil {
		return start, pgtype.Int4{}, nil
	}
	end, err := int32Of(*e.EndYear)
	if err != nil {
		return 0, pgtype.Int4{}, fmt.Errorf("end year: %w", err)
	}
	return start, pgtype.Int4{Int32: end, Valid: true}, nil
}

func education(r educationRow) domain.Education {
	e := domain.Education{
		ID:           r.ID.String(),
		UserID:       r.UserID.String(),
		Institution:  r.Institution,
		Degree:       textPtr(r.Degree),
		FieldOfStudy: textPtr(r.FieldOfStudy),
		StartYear:    int(r.StartYear),
	}
	if r.EndYear.Valid {
		end := int(r.EndYear.Int32)
		e.EndYear = &end
	}
	return e
}
