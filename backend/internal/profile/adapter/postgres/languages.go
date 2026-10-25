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

// uniqueViolation is the SQLSTATE of a write that breaks a unique index.
const uniqueViolation = "23505"

// languageNameKey is the unique index on a user's language names, ignoring letter case
// (migration 000010).
const languageNameKey = "profile_languages_user_name_key"

// languageRow is the shape every language query returns; sqlc makes one type per query,
// and each converts to this one.
type languageRow = sqlcgen.ListUserLanguagesRow

// ListLanguages returns the user's languages, oldest first (FR-4.9).
func (s *Store) ListLanguages(ctx context.Context, userID string) ([]domain.Language, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return []domain.Language{}, nil
	}
	rows, err := s.q.ListUserLanguages(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("list languages: %w", err)
	}
	out := make([]domain.Language, 0, len(rows))
	for _, r := range rows {
		out = append(out, language(r))
	}
	return out, nil
}

// DeleteLanguage returns domain.ErrNotFound when the user has no language with the ID,
// or an ID is not a UUID.
func (s *Store) DeleteLanguage(ctx context.Context, userID, id string) error {
	uid, lid, err := parseIDs(userID, id)
	if err != nil {
		return domain.ErrNotFound
	}
	n, err := s.q.DeleteUserLanguage(ctx, sqlcgen.DeleteUserLanguageParams{ID: lid, UserID: uid})
	if err != nil {
		return fmt.Errorf("delete language: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// CountLanguages returns how many languages the user has.
func (t tx) CountLanguages(ctx context.Context, userID string) (int, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return 0, domain.ErrNotFound
	}
	n, err := t.q.CountUserLanguages(ctx, uid)
	if err != nil {
		return 0, fmt.Errorf("count languages: %w", err)
	}
	return int(n), nil
}

// LanguageNameTaken compares names with lower(), as the unique index does.
func (t tx) LanguageNameTaken(ctx context.Context, userID, name, exceptID string) (bool, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return false, domain.ErrNotFound
	}
	var except pgtype.UUID // NULL excepts none
	if exceptID != "" {
		if except, err = parseUUID(exceptID); err != nil {
			return false, domain.ErrNotFound
		}
	}
	taken, err := t.q.LanguageNameTaken(ctx, sqlcgen.LanguageNameTakenParams{UserID: uid, Name: name, ExceptID: except})
	if err != nil {
		return false, fmt.Errorf("check language name: %w", err)
	}
	return taken, nil
}

// InsertLanguage returns domain.ErrNotFound when the user does not exist and
// domain.ErrLanguageExists when the user has the name already.
func (t tx) InsertLanguage(ctx context.Context, l domain.Language) (domain.Language, error) {
	uid, err := parseUUID(l.UserID)
	if err != nil {
		return domain.Language{}, domain.ErrNotFound
	}
	row, err := t.q.InsertLanguage(ctx, sqlcgen.InsertLanguageParams{
		UserID: uid, Name: l.Name, Proficiency: string(l.Proficiency),
	})
	if err != nil {
		return domain.Language{}, languageWriteError("insert language", err)
	}
	return language(languageRow(row)), nil
}

// FindLanguageForUpdate returns domain.ErrNotFound when the user has no language with
// the ID, or an ID is not a UUID.
func (t tx) FindLanguageForUpdate(ctx context.Context, userID, id string) (domain.Language, error) {
	uid, lid, err := parseIDs(userID, id)
	if err != nil {
		return domain.Language{}, domain.ErrNotFound
	}
	row, err := t.q.FindUserLanguageForUpdate(ctx, sqlcgen.FindUserLanguageForUpdateParams{ID: lid, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Language{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Language{}, fmt.Errorf("find language: %w", err)
	}
	return language(languageRow(row)), nil
}

// UpdateLanguage returns domain.ErrNotFound when l.UserID has no language l.ID and
// domain.ErrLanguageExists when another of the user's languages has the name.
func (t tx) UpdateLanguage(ctx context.Context, l domain.Language) (domain.Language, error) {
	uid, lid, err := parseIDs(l.UserID, l.ID)
	if err != nil {
		return domain.Language{}, domain.ErrNotFound
	}
	row, err := t.q.UpdateUserLanguage(ctx, sqlcgen.UpdateUserLanguageParams{
		Name: l.Name, Proficiency: string(l.Proficiency), ID: lid, UserID: uid,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Language{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Language{}, languageWriteError("update language", err)
	}
	return language(languageRow(row)), nil
}

// languageWriteError turns a missing user into domain.ErrNotFound and a name the user
// has already into domain.ErrLanguageExists. The usecase checks the name first, so the
// unique index decides only when two requests add the same name at the same moment.
func languageWriteError(op string, err error) error {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch {
		case pgErr.Code == foreignKeyViolation:
			return domain.ErrNotFound
		case pgErr.Code == uniqueViolation && pgErr.ConstraintName == languageNameKey:
			return domain.ErrLanguageExists
		}
	}
	return fmt.Errorf("%s: %w", op, err)
}

func language(r languageRow) domain.Language {
	return domain.Language{
		ID:          r.ID.String(),
		UserID:      r.UserID.String(),
		Name:        r.Name,
		Proficiency: domain.Proficiency(r.Proficiency),
	}
}
