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

// Key prefixes of the project lists (AI-3.1, AI-2.3), each followed by a UUID, and the
// projects generation counter.
const (
	projectsPrefix    = "recs:projects:"
	researchersPrefix = "recs:project-people:"
	// GenerationKey holds the projects generation (domain.ProjectCache.Generation).
	GenerationKey = "recs:projects-generation"
)

var _ domain.ProjectCache = (*Cache)(nil)

// ProjectsKey is where userID's project list is kept.
func ProjectsKey(userID string) string { return projectsPrefix + userID }

// ResearchersKey is where a project's researcher list is kept.
func ResearchersKey(projectID string) string { return researchersPrefix + projectID }

type keptProjectsJSON struct {
	Model       string        `json:"model"`
	ContentHash string        `json:"contentHash"`
	UpdatedAt   time.Time     `json:"updatedAt"`
	Generation  int64         `json:"generation"`
	Items       []projectJSON `json:"items"`
}

type projectJSON struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	Skills        []string `json:"skills"`
	OwnerID       string   `json:"ownerId"`
	OwnerName     string   `json:"ownerName"`
	OwnerHeadline *string  `json:"ownerHeadline,omitempty"`
	OwnerPhotoID  *string  `json:"ownerPhotoId,omitempty"`
	MemberCount   int      `json:"memberCount"`
	Score         int      `json:"score"`
	Shared        []string `json:"shared"`
}

// GetProjects reads userID's project list.
func (c *Cache) GetProjects(ctx context.Context, userID string) (domain.KeptProjects, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	b, err := c.client.Get(ctx, ProjectsKey(userID)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return domain.KeptProjects{}, false, nil
	}
	if err != nil {
		return domain.KeptProjects{}, false, fmt.Errorf("read kept project recommendations: %w", err)
	}
	var k keptProjectsJSON
	if err := json.Unmarshal(b, &k); err != nil {
		return domain.KeptProjects{}, false, fmt.Errorf("decode kept project recommendations: %w", err)
	}
	out := domain.KeptProjects{
		Stamp:      domain.Stamp{Model: k.Model, ContentHash: k.ContentHash, UpdatedAt: k.UpdatedAt},
		Generation: k.Generation,
		Items:      make([]domain.ProjectRecommendation, len(k.Items)),
	}
	for i, it := range k.Items {
		out.Items[i] = domain.ProjectRecommendation{
			Project: domain.ProjectCard{
				ID: it.ID, Title: it.Title, Description: it.Description, Skills: nonNil(it.Skills),
				Owner:       domain.ProjectOwner{UserID: it.OwnerID, FullName: it.OwnerName, Headline: it.OwnerHeadline, PhotoFileID: it.OwnerPhotoID},
				MemberCount: it.MemberCount,
			},
			Score: it.Score, Shared: nonNil(it.Shared),
		}
	}
	return out, true, nil
}

// SetProjects keeps userID's project list for ttl.
func (c *Cache) SetProjects(ctx context.Context, userID string, kept domain.KeptProjects, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	k := keptProjectsJSON{Model: kept.Stamp.Model, ContentHash: kept.Stamp.ContentHash, UpdatedAt: kept.Stamp.UpdatedAt,
		Generation: kept.Generation, Items: make([]projectJSON, len(kept.Items))}
	for i, it := range kept.Items {
		p := it.Project
		k.Items[i] = projectJSON{ID: p.ID, Title: p.Title, Description: p.Description, Skills: p.Skills,
			OwnerID: p.Owner.UserID, OwnerName: p.Owner.FullName, OwnerHeadline: p.Owner.Headline, OwnerPhotoID: p.Owner.PhotoFileID,
			MemberCount: p.MemberCount, Score: it.Score, Shared: it.Shared}
	}
	b, err := json.Marshal(k)
	if err != nil {
		return fmt.Errorf("encode project recommendations: %w", err)
	}
	if err := c.client.Set(ctx, ProjectsKey(userID), b, ttl).Err(); err != nil {
		return fmt.Errorf("keep project recommendations: %w", err)
	}
	return nil
}

// DeleteProjects drops the project lists of userIDs.
func (c *Cache) DeleteProjects(ctx context.Context, userIDs ...string) error {
	return c.del(ctx, ProjectsKey, userIDs, "drop kept project recommendations")
}

// GetResearchers reads a project's researcher list.
func (c *Cache) GetResearchers(ctx context.Context, projectID string) (domain.Kept, bool, error) {
	return c.get(ctx, ResearchersKey(projectID))
}

// SetResearchers keeps a project's researcher list for ttl. The connection statuses are not
// kept.
func (c *Cache) SetResearchers(ctx context.Context, projectID string, kept domain.Kept, ttl time.Duration) error {
	return c.set(ctx, ResearchersKey(projectID), kept, ttl)
}

// DeleteResearchers drops the researcher lists of projectIDs.
func (c *Cache) DeleteResearchers(ctx context.Context, projectIDs ...string) error {
	return c.del(ctx, ResearchersKey, projectIDs, "drop kept researcher recommendations")
}

// Generation reads the projects generation; 0 when it was never raised.
func (c *Cache) Generation(ctx context.Context) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	n, err := c.client.Get(ctx, GenerationKey).Int64()
	if errors.Is(err, goredis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read projects generation: %w", err)
	}
	return n, nil
}

// NextGeneration raises the projects generation. The key has no expiry: if it were lost, a
// kept list made at generation 0 could be served again, so it is kept like a counter.
func (c *Cache) NextGeneration(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := c.client.Incr(ctx, GenerationKey).Err(); err != nil {
		return fmt.Errorf("raise projects generation: %w", err)
	}
	return nil
}

func (c *Cache) del(ctx context.Context, key func(string) string, ids []string, what string) error {
	if len(ids) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = key(id)
	}
	if err := c.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
