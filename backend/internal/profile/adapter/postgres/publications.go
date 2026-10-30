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

// publicationDOIKey is the unique constraint on publications.doi (migration 000001).
const publicationDOIKey = "publications_doi_key"

// publicationRow is the shape every publication query returns; sqlc makes one type per
// query, and each converts to this one.
type publicationRow = sqlcgen.FindPublicationByDOIRow

// ListConfirmedPublications returns the user's confirmed publications with AddedAt set.
func (s *Store) ListConfirmedPublications(ctx context.Context, userID string) ([]domain.Publication, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return []domain.Publication{}, nil
	}
	rows, err := s.q.ListConfirmedUserPublications(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("list publications: %w", err)
	}
	out := make([]domain.Publication, 0, len(rows))
	for _, r := range rows {
		p := publication(publicationRow{
			ID: r.ID, Doi: r.Doi, Title: r.Title, PublicationYear: r.PublicationYear, VenueName: r.VenueName,
			Abstract: r.Abstract, Url: r.Url, Source: r.Source, CreatedBy: r.CreatedBy,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		})
		p.AddedAt = r.AddedAt.Time
		out = append(out, p)
	}
	return out, nil
}

// CountConfirmedPublications returns how many publications the user has confirmed.
func (s *Store) CountConfirmedPublications(ctx context.Context, userID string) (int, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return 0, nil
	}
	n, err := s.q.CountConfirmedUserPublications(ctx, uid)
	if err != nil {
		return 0, fmt.Errorf("count publications: %w", err)
	}
	return int(n), nil
}

// CountPublicationLinks returns how many publications the user is linked to.
func (t tx) CountPublicationLinks(ctx context.Context, userID string) (int, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return 0, domain.ErrNotFound
	}
	n, err := t.q.CountUserPublicationLinks(ctx, uid)
	if err != nil {
		return 0, fmt.Errorf("count publication links: %w", err)
	}
	return int(n), nil
}

// FindPublicationByDOI returns domain.ErrNotFound when no publication has the DOI.
func (t tx) FindPublicationByDOI(ctx context.Context, doi string) (domain.Publication, error) {
	row, err := t.q.FindPublicationByDOI(ctx, pgtype.Text{String: doi, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Publication{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Publication{}, fmt.Errorf("find publication by doi: %w", err)
	}
	return publication(row), nil
}

// InsertPublication returns domain.ErrDuplicateDOI when another publication has the DOI.
func (t tx) InsertPublication(ctx context.Context, p domain.Publication) (domain.Publication, error) {
	year, err := yearOf(p.Year)
	if err != nil {
		return domain.Publication{}, err
	}
	createdBy := pgtype.UUID{}
	if p.CreatedBy != nil {
		if createdBy, err = parseUUID(*p.CreatedBy); err != nil {
			return domain.Publication{}, domain.ErrNotFound
		}
	}
	row, err := t.q.InsertPublication(ctx, sqlcgen.InsertPublicationParams{
		Doi:             text(p.DOI),
		Title:           p.Title,
		PublicationYear: year,
		VenueName:       text(p.Venue),
		Abstract:        text(p.Abstract),
		Url:             text(p.URL),
		Source:          sqlcgen.PublicationSource(p.Source),
		CreatedBy:       createdBy,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Publication{}, domain.ErrDuplicateDOI
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == foreignKeyViolation {
		return domain.Publication{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Publication{}, fmt.Errorf("insert publication: %w", err)
	}
	return publication(publicationRow(row)), nil
}

// PublicationLink returns how the user is linked to the publication.
func (t tx) PublicationLink(ctx context.Context, userID, publicationID string) (domain.LinkState, error) {
	uid, pid, err := parseIDs(userID, publicationID)
	if err != nil {
		return domain.NotLinked, nil
	}
	confirmed, err := t.q.FindPublicationLink(ctx, sqlcgen.FindPublicationLinkParams{UserID: uid, PublicationID: pid})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.NotLinked, nil
	case err != nil:
		return domain.NotLinked, fmt.Errorf("find publication link: %w", err)
	case confirmed:
		return domain.Confirmed, nil
	default:
		return domain.Unconfirmed, nil
	}
}

// LinkPublication returns domain.ErrNotFound when the user or the publication does not
// exist.
func (t tx) LinkPublication(ctx context.Context, userID, publicationID string) error {
	uid, pid, err := parseIDs(userID, publicationID)
	if err != nil {
		return domain.ErrNotFound
	}
	err = t.q.InsertConfirmedPublicationLink(ctx, sqlcgen.InsertConfirmedPublicationLinkParams{UserID: uid, PublicationID: pid})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == foreignKeyViolation {
		return domain.ErrNotFound
	}
	return wrap(err, "link publication")
}

// ConfirmPublication confirms the user's link; a confirmed link stays as it is.
func (t tx) ConfirmPublication(ctx context.Context, userID, publicationID string) error {
	uid, pid, err := parseIDs(userID, publicationID)
	if err != nil {
		return domain.ErrNotFound
	}
	return wrap(t.q.ConfirmPublicationLink(ctx, sqlcgen.ConfirmPublicationLinkParams{UserID: uid, PublicationID: pid}),
		"confirm publication")
}

// FindPublicationForUpdate returns domain.ErrNotFound when the user is not linked to the
// publication, or an ID is not a UUID.
func (t tx) FindPublicationForUpdate(ctx context.Context, userID, id string) (domain.Publication, error) {
	uid, pid, err := parseIDs(userID, id)
	if err != nil {
		return domain.Publication{}, domain.ErrNotFound
	}
	row, err := t.q.FindUserPublicationForUpdate(ctx, sqlcgen.FindUserPublicationForUpdateParams{ID: pid, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Publication{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Publication{}, fmt.Errorf("find publication: %w", err)
	}
	return publication(publicationRow(row)), nil
}

// CountPublicationAuthors returns how many users are linked to the publication.
func (t tx) CountPublicationAuthors(ctx context.Context, publicationID string) (int, error) {
	pid, err := parseUUID(publicationID)
	if err != nil {
		return 0, domain.ErrNotFound
	}
	n, err := t.q.CountPublicationAuthors(ctx, pid)
	if err != nil {
		return 0, fmt.Errorf("count publication authors: %w", err)
	}
	return int(n), nil
}

// UpdatePublication returns domain.ErrDuplicateDOI when another publication has the DOI,
// and domain.ErrNotFound when there is no publication p.ID.
func (t tx) UpdatePublication(ctx context.Context, p domain.Publication) (domain.Publication, error) {
	pid, err := parseUUID(p.ID)
	if err != nil {
		return domain.Publication{}, domain.ErrNotFound
	}
	year, err := yearOf(p.Year)
	if err != nil {
		return domain.Publication{}, err
	}
	row, err := t.q.UpdatePublication(ctx, sqlcgen.UpdatePublicationParams{
		ID:              pid,
		Doi:             text(p.DOI),
		Title:           p.Title,
		PublicationYear: year,
		VenueName:       text(p.Venue),
		Abstract:        text(p.Abstract),
		Url:             text(p.URL),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Publication{}, domain.ErrNotFound
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == uniqueViolation && pgErr.ConstraintName == publicationDOIKey {
		return domain.Publication{}, domain.ErrDuplicateDOI
	}
	if err != nil {
		return domain.Publication{}, fmt.Errorf("update publication: %w", err)
	}
	return publication(publicationRow(row)), nil
}

// UnlinkPublication returns domain.ErrNotFound when the user is not linked to the
// publication.
func (t tx) UnlinkPublication(ctx context.Context, userID, publicationID string) error {
	uid, pid, err := parseIDs(userID, publicationID)
	if err != nil {
		return domain.ErrNotFound
	}
	n, err := t.q.DeletePublicationLink(ctx, sqlcgen.DeletePublicationLinkParams{UserID: uid, PublicationID: pid})
	if err != nil {
		return fmt.Errorf("unlink publication: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DeletePublicationIfOrphaned deletes the publication when no user is linked to it.
func (t tx) DeletePublicationIfOrphaned(ctx context.Context, publicationID string) error {
	pid, err := parseUUID(publicationID)
	if err != nil {
		return domain.ErrNotFound
	}
	return wrap(t.q.DeletePublicationIfOrphaned(ctx, pid), "delete orphaned publication")
}

func publication(r publicationRow) domain.Publication {
	p := domain.Publication{
		ID:        r.ID.String(),
		DOI:       textPtr(r.Doi),
		Title:     r.Title,
		Venue:     textPtr(r.VenueName),
		Abstract:  textPtr(r.Abstract),
		URL:       textPtr(r.Url),
		Source:    domain.PublicationSource(r.Source),
		CreatedBy: uuidPtr(r.CreatedBy),
		CreatedAt: r.CreatedAt.Time,
		UpdatedAt: r.UpdatedAt.Time,
	}
	if r.PublicationYear.Valid {
		y := int(r.PublicationYear.Int32)
		p.Year = &y
	}
	return p
}

func yearOf(y *int) (pgtype.Int4, error) {
	if y == nil {
		return pgtype.Int4{}, nil
	}
	n, err := int32Of(*y)
	if err != nil {
		return pgtype.Int4{}, fmt.Errorf("publication year: %w", err)
	}
	return pgtype.Int4{Int32: n, Valid: true}, nil
}
