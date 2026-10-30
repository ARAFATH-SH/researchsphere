package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// profileOrcidKey is the unique constraint on profiles.orcid_id (migration 000001).
const profileOrcidKey = "profiles_orcid_id_key"

// SetOrcidID returns domain.ErrORCIDTaken when another profile has the iD, and
// domain.ErrNotFound when the user has no profile row or the ID is not a UUID.
func (s *Store) SetOrcidID(ctx context.Context, userID string, orcidID *string) error {
	id, err := parseUUID(userID)
	if err != nil {
		return domain.ErrNotFound
	}
	n, err := s.q.SetOrcidID(ctx, sqlcgen.SetOrcidIDParams{UserID: id, OrcidID: text(orcidID)})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == uniqueViolation && pgErr.ConstraintName == profileOrcidKey {
		return domain.ErrORCIDTaken
	}
	if err != nil {
		return fmt.Errorf("set ORCID iD: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// MatchPublicationsByDOI reads the stored publications with the DOIs in one query. A user
// ID that is not a UUID has no links.
func (t tx) MatchPublicationsByDOI(ctx context.Context, userID string, dois []string) (map[string]domain.DOIMatch, error) {
	out := make(map[string]domain.DOIMatch)
	if len(dois) == 0 {
		return out, nil
	}
	uid, _ := parseUUID(userID) // an invalid ID matches no link
	rows, err := t.q.MatchPublicationsByDOI(ctx, sqlcgen.MatchPublicationsByDOIParams{UserID: uid, Dois: dois})
	if err != nil {
		return nil, fmt.Errorf("match publications by DOI: %w", err)
	}
	for _, r := range rows {
		m := domain.DOIMatch{PublicationID: r.ID.String(), State: domain.NotLinked}
		switch {
		case r.Confirmed:
			m.State = domain.Confirmed
		case r.Linked:
			m.State = domain.Unconfirmed
		}
		out[r.Doi.String] = m
	}
	return out, nil
}
