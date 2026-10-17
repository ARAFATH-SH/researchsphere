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

// linkRow is the shape every link query returns; sqlc makes one type per query, and each
// converts to this one.
type linkRow = sqlcgen.ListUserLinksRow

// FindContact returns domain.ErrNotFound when no user has the ID, or the ID is not a UUID.
func (s *Store) FindContact(ctx context.Context, userID string) (domain.Contact, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return domain.Contact{}, domain.ErrNotFound
	}
	row, err := s.q.FindContact(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Contact{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Contact{}, fmt.Errorf("find contact: %w", err)
	}
	c := domain.Contact{
		UserID:             uid.String(),
		Email:              row.Email,
		EmailVisibility:    domain.Visibility(row.EmailVisibility),
		Phone:              textPtr(row.Phone),
		PhoneVisibility:    domain.Visibility(row.PhoneVisibility),
		BirthdayVisibility: domain.Visibility(row.BirthdayVisibility),
	}
	if row.BirthMonth.Valid && row.BirthDay.Valid {
		c.Birthday = &domain.Birthday{Month: int(row.BirthMonth.Int16), Day: int(row.BirthDay.Int16)}
	}
	return c, nil
}

// UpsertContact returns domain.ErrNotFound when the user does not exist.
func (s *Store) UpsertContact(ctx context.Context, c domain.Contact) error {
	uid, err := parseUUID(c.UserID)
	if err != nil {
		return domain.ErrNotFound
	}
	var month, day pgtype.Int2
	if b := c.Birthday; b != nil {
		// validBirthday in the usecase keeps both far inside int16.
		month = pgtype.Int2{Int16: int16(b.Month), Valid: true} //nolint:gosec // 1 to 12
		day = pgtype.Int2{Int16: int16(b.Day), Valid: true}     //nolint:gosec // 1 to 31
	}
	err = s.q.UpsertContact(ctx, sqlcgen.UpsertContactParams{
		UserID:             uid,
		Phone:              text(c.Phone),
		BirthMonth:         month,
		BirthDay:           day,
		EmailVisibility:    string(c.EmailVisibility),
		PhoneVisibility:    string(c.PhoneVisibility),
		BirthdayVisibility: string(c.BirthdayVisibility),
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == foreignKeyViolation {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("upsert contact: %w", err)
	}
	return nil
}

// ListLinks returns the user's links, oldest first (FR-4.7).
func (s *Store) ListLinks(ctx context.Context, userID string) ([]domain.Link, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return []domain.Link{}, nil
	}
	rows, err := s.q.ListUserLinks(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}
	out := make([]domain.Link, 0, len(rows))
	for _, r := range rows {
		out = append(out, link(r))
	}
	return out, nil
}

// DeleteLink returns domain.ErrNotFound when the user has no link with the ID, or an ID
// is not a UUID.
func (s *Store) DeleteLink(ctx context.Context, userID, id string) error {
	uid, lid, err := parseIDs(userID, id)
	if err != nil {
		return domain.ErrNotFound
	}
	n, err := s.q.DeleteUserLink(ctx, sqlcgen.DeleteUserLinkParams{ID: lid, UserID: uid})
	if err != nil {
		return fmt.Errorf("delete link: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// CountLinks returns how many links the user has.
func (t tx) CountLinks(ctx context.Context, userID string) (int, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return 0, domain.ErrNotFound
	}
	n, err := t.q.CountUserLinks(ctx, uid)
	if err != nil {
		return 0, fmt.Errorf("count links: %w", err)
	}
	return int(n), nil
}

// InsertLink returns domain.ErrNotFound when the user does not exist.
func (t tx) InsertLink(ctx context.Context, l domain.Link) (domain.Link, error) {
	uid, err := parseUUID(l.UserID)
	if err != nil {
		return domain.Link{}, domain.ErrNotFound
	}
	row, err := t.q.InsertLink(ctx, sqlcgen.InsertLinkParams{UserID: uid, Kind: string(l.Kind), Url: l.URL})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == foreignKeyViolation {
		return domain.Link{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Link{}, fmt.Errorf("insert link: %w", err)
	}
	return link(linkRow(row)), nil
}

func link(r linkRow) domain.Link {
	return domain.Link{ID: r.ID.String(), UserID: r.UserID.String(), Kind: domain.LinkKind(r.Kind), URL: r.Url}
}
