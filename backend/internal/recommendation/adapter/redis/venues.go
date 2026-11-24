package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

// Venue recommendations (AI-6.1; slice 25, chunk 25.1): each project's lists, followed by
// its UUID, and the venues generation counter.
const (
	venuesPrefix = "recs:project-venues:"
	// VenueGenerationKey holds the venues generation (domain.VenueCache.VenueGeneration).
	VenueGenerationKey = "recs:venues-generation"
)

var _ domain.VenueCache = (*Cache)(nil)

// VenuesKey is where a project's venue lists are kept.
func VenuesKey(projectID string) string { return venuesPrefix + projectID }

type keptVenuesJSON struct {
	Model       string      `json:"model"`
	ContentHash string      `json:"contentHash"`
	UpdatedAt   time.Time   `json:"updatedAt"`
	Generation  int64       `json:"generation"`
	Venues      []venueJSON `json:"venues"`
	Journals    []venueJSON `json:"journals"`
}

// venueJSON is a kept item. Days left are never kept: they are worked out on reading.
type venueJSON struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	ShortName   *string          `json:"shortName,omitempty"`
	Type        string           `json:"type"`
	Fields      []venueFieldJSON `json:"fields"`
	TimeZone    *string          `json:"timeZone,omitempty"`
	Deadline    *time.Time       `json:"deadline,omitempty"`
	OfficialURL string           `json:"officialUrl"`
	LinkKind    string           `json:"linkKind"`
	Score       int              `json:"score"`
}

type venueFieldJSON struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// GetVenues reads a project's venue lists.
func (c *Cache) GetVenues(ctx context.Context, projectID string) (domain.KeptVenues, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	b, err := c.client.Get(ctx, VenuesKey(projectID)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return domain.KeptVenues{}, false, nil
	}
	if err != nil {
		return domain.KeptVenues{}, false, fmt.Errorf("read kept venue recommendations: %w", err)
	}
	var k keptVenuesJSON
	if err := json.Unmarshal(b, &k); err != nil {
		return domain.KeptVenues{}, false, fmt.Errorf("decode kept venue recommendations: %w", err)
	}
	return domain.KeptVenues{
		Stamp:      domain.Stamp{Model: k.Model, ContentHash: k.ContentHash, UpdatedAt: k.UpdatedAt},
		Generation: k.Generation,
		Venues:     venuesFromJSON(k.Venues),
		Journals:   venuesFromJSON(k.Journals),
	}, true, nil
}

// SetVenues keeps a project's venue lists for ttl.
func (c *Cache) SetVenues(ctx context.Context, projectID string, kept domain.KeptVenues, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	b, err := json.Marshal(keptVenuesJSON{
		Model: kept.Stamp.Model, ContentHash: kept.Stamp.ContentHash, UpdatedAt: kept.Stamp.UpdatedAt,
		Generation: kept.Generation, Venues: venuesToJSON(kept.Venues), Journals: venuesToJSON(kept.Journals),
	})
	if err != nil {
		return fmt.Errorf("encode venue recommendations: %w", err)
	}
	if err := c.client.Set(ctx, VenuesKey(projectID), b, ttl).Err(); err != nil {
		return fmt.Errorf("keep venue recommendations: %w", err)
	}
	return nil
}

// DeleteVenues drops the venue lists of projectIDs.
func (c *Cache) DeleteVenues(ctx context.Context, projectIDs ...string) error {
	return c.del(ctx, VenuesKey, projectIDs, "drop kept venue recommendations")
}

// VenueGeneration reads the venues generation; 0 when it was never raised.
func (c *Cache) VenueGeneration(ctx context.Context) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	n, err := c.client.Get(ctx, VenueGenerationKey).Int64()
	if errors.Is(err, goredis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read venues generation: %w", err)
	}
	return n, nil
}

// NextVenueGeneration raises the venues generation. Like the projects generation, the key
// has no expiry.
func (c *Cache) NextVenueGeneration(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := c.client.Incr(ctx, VenueGenerationKey).Err(); err != nil {
		return fmt.Errorf("raise venues generation: %w", err)
	}
	return nil
}

func venuesToJSON(in []domain.VenueRecommendation) []venueJSON {
	out := make([]venueJSON, len(in))
	for i, v := range in {
		fs := make([]venueFieldJSON, len(v.Fields))
		for j, f := range v.Fields {
			fs[j] = venueFieldJSON(f)
		}
		out[i] = venueJSON{ID: v.ID, Name: v.Name, ShortName: v.ShortName, Type: v.Type, Fields: fs,
			TimeZone: v.TimeZone, Deadline: v.Deadline, OfficialURL: v.OfficialURL, LinkKind: v.LinkKind, Score: v.Score}
	}
	return out
}

func venuesFromJSON(in []venueJSON) []domain.VenueRecommendation {
	out := make([]domain.VenueRecommendation, len(in))
	for i, v := range in {
		fs := make([]domain.VenueField, len(v.Fields))
		for j, f := range v.Fields {
			fs[j] = domain.VenueField(f)
		}
		out[i] = domain.VenueRecommendation{Score: v.Score, VenueCard: domain.VenueCard{
			ID: v.ID, Name: v.Name, ShortName: v.ShortName, Type: v.Type, Fields: fs,
			TimeZone: v.TimeZone, Deadline: v.Deadline, OfficialURL: v.OfficialURL, LinkKind: v.LinkKind,
		}}
	}
	return out
}
