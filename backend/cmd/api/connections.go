package main

import (
	"context"

	profiledomain "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
	socialusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/social/usecase"
)

// connectionLookup gives the profile module the connection status and count it shows
// (FR-7) from the social module, which owns connections. Neither module imports the
// other; this is the only place that joins them.
type connectionLookup struct {
	social *socialusecase.Service
}

var _ profiledomain.ConnectionLookup = connectionLookup{}

// Relation converts the social module's status; the values are the same strings.
func (c connectionLookup) Relation(ctx context.Context, viewerID, ownerID string) (profiledomain.ConnectionRelation, error) {
	r, err := c.social.Relation(ctx, viewerID, ownerID)
	if err != nil {
		return profiledomain.ConnectionRelation{}, err
	}
	return profiledomain.ConnectionRelation{Status: profiledomain.ConnectionStatus(r.Status), RequestID: r.RequestID}, nil
}

// Count is the total of the user's connections list.
func (c connectionLookup) Count(ctx context.Context, userID string) (int, error) {
	return c.social.CountConnections(ctx, userID)
}

// Relations converts the social module's statuses for a whole list, read at once.
func (c connectionLookup) Relations(ctx context.Context, viewerID string, userIDs []string) (map[string]profiledomain.ConnectionRelation, error) {
	rs, err := c.social.Relations(ctx, viewerID, userIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]profiledomain.ConnectionRelation, len(rs))
	for id, r := range rs {
		out[id] = profiledomain.ConnectionRelation{Status: profiledomain.ConnectionStatus(r.Status), RequestID: r.RequestID}
	}
	return out, nil
}
