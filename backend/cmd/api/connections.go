package main

import (
	"context"

	messagingdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	messagingusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/usecase"
	profiledomain "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
	socialdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
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

// Count is the total of the user's connections list as the viewer sees it.
func (c connectionLookup) Count(ctx context.Context, viewerID, userID string) (int, error) {
	return c.social.CountConnections(ctx, viewerID, userID)
}

// Block converts the social module's block state (FR-16.9).
func (c connectionLookup) Block(ctx context.Context, viewerID, ownerID string) (profiledomain.BlockState, error) {
	b, err := c.social.BlockBetween(ctx, viewerID, ownerID)
	if err != nil {
		return profiledomain.BlockState{}, err
	}
	return profiledomain.BlockState{ByViewer: b.ByMe, ByOwner: b.ByThem}, nil
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

// conversationLookup gives the profile module the viewer's conversation with a profile's
// owner (FR-16.1, FR-16.2) from the messaging module, which owns conversations.
type conversationLookup struct {
	messaging *messagingusecase.Service
}

var _ profiledomain.ConversationLookup = conversationLookup{}

// Between finds the pair's conversation and reads it as the viewer sees it, nil when none.
func (c conversationLookup) Between(ctx context.Context, viewerID, ownerID string) (*profiledomain.ConversationLink, error) {
	id, found, err := c.messaging.ConversationBetween(ctx, viewerID, ownerID)
	if err != nil || !found {
		return nil, err
	}
	it, err := c.messaging.Get(ctx, viewerID, id)
	if err != nil {
		return nil, err
	}
	return &profiledomain.ConversationLink{
		ID:          it.ID,
		State:       string(it.State),
		RequestedBy: it.RequestedBy,
		CanMessage:  it.CanMessage(),
	}, nil
}

// connectionChecker tells the messaging module whether two users are connected (FR-16.1)
// and whether either has blocked the other (FR-16.9), from the social module. The two depend on each other at startup (social's notifier
// activates message requests), so main creates this first and sets social once it exists.
type connectionChecker struct {
	social *socialusecase.Service
}

var (
	_ messagingdomain.Connections = (*connectionChecker)(nil)
	_ messagingdomain.Blocks      = (*connectionChecker)(nil)
)

// Connected reports whether a and b have an accepted connection.
func (c *connectionChecker) Connected(ctx context.Context, a, b string) (bool, error) {
	r, err := c.social.Relation(ctx, a, b)
	if err != nil {
		return false, err
	}
	return r.Status == socialdomain.StatusConnected, nil
}

// ConnectedAmong returns which of ids have an accepted connection with userID, read at once.
func (c *connectionChecker) ConnectedAmong(ctx context.Context, userID string, ids []string) ([]string, error) {
	rs, err := c.social.Relations(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, id := range ids {
		if rs[id].Status == socialdomain.StatusConnected {
			out = append(out, id)
		}
	}
	return out, nil
}

// BlockedAmong returns which of ids are in a block with userID, either way.
func (c *connectionChecker) BlockedAmong(ctx context.Context, userID string, ids []string) ([]string, error) {
	return c.social.BlockedAmong(ctx, userID, ids)
}

// Lock holds the pair's lock in the send's transaction.
func (c *connectionChecker) Lock(ctx context.Context, a, b string) error {
	return c.social.LockPair(ctx, a, b)
}

// Between converts the social module's block state.
func (c *connectionChecker) Between(ctx context.Context, me, other string) (messagingdomain.BlockState, error) {
	b, err := c.social.BlockBetween(ctx, me, other)
	if err != nil {
		return messagingdomain.BlockState{}, err
	}
	return messagingdomain.BlockState{ByMe: b.ByMe, ByThem: b.ByThem}, nil
}
