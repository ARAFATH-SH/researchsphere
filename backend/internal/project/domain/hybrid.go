package domain

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"
)

// Hybrid project search (AI-5.1 for projects; slice 19, chunk 19.2), as hybrid researcher
// search does it (profile module, decisions.md 2026-11-16).

// SearchMode is the search a caller asks for.
type SearchMode string

// Search modes. Auto is keyword and meaning together (AI-5.1); keyword is FR-10.3 alone.
const (
	SearchAuto    SearchMode = "auto"
	SearchKeyword SearchMode = "keyword"
)

// ResultMode says how a search's results were found. The values are the contract's.
type ResultMode string

// Result modes.
const (
	ResultHybrid          ResultMode = "hybrid"
	ResultKeyword         ResultMode = "keyword"
	ResultKeywordFallback ResultMode = "keyword_fallback" // the AI service did not answer (AI-5.4)
)

// MatchMeaning is the MatchedOn of a result only the meaning side found.
const MatchMeaning = "meaning"

// ErrInvalidSearchMode refuses a mode other than auto or keyword.
var ErrInvalidSearchMode = errors.New("project: search mode is not auto or keyword")

// QueryVector is a search text's embedding and the model that made it.
type QueryVector struct {
	Model  string
	Values []float32
}

// QueryEmbedder embeds a search text as a query (e5's "query: " prefix). It returns an error
// when the AI service is not configured, does not answer or answers with an error.
type QueryEmbedder interface {
	EmbedQuery(ctx context.Context, text string) (QueryVector, error)
}

// QueryVectorCache keeps query vectors by model and normalized search text.
type QueryVectorCache interface {
	// Get returns the vector kept for text under model; false when there is none.
	Get(ctx context.Context, model, text string) ([]float32, bool, error)
	// Set keeps v for text under v.Model for ttl.
	Set(ctx context.Context, text string, v QueryVector, ttl time.Duration) error
}

// Hybrid search sizes, as in researcher search.
const (
	// RRFK is the k of reciprocal rank fusion: a candidate at rank r (from 1) of a list
	// scores 1 / (RRFK + r) for it.
	RRFK = 60
	// HybridDepth is how many candidates each side contributes.
	HybridDepth = 50
	// HybridMax is the most results a hybrid search returns over all pages.
	HybridMax = 100
)

// KeywordCandidate is one keyword result in keyword order, with the field it matched.
type KeywordCandidate struct {
	ID        string
	MatchedOn string
}

// Fused is one result of a merged hybrid list.
type Fused struct {
	ID        string
	MatchedOn string
}

// FuseProjects merges the three candidate lists of a hybrid search, best first, and cuts the
// result at HybridMax.
//
// Strong title matches come first, in their own order, as matched on the title. Every other
// candidate is ordered by its reciprocal rank fusion score over the keyword and meaning
// lists, then those the keyword side found first, then by keyword rank, then by ID. It keeps
// the keyword side's field; a candidate only the meaning side found matched on MatchMeaning.
//
// AI-2.4: only ranks in the lists count; no popularity signal reaches this function.
func FuseProjects(strong []string, keyword []KeywordCandidate, meaning []string) []Fused {
	out := make([]Fused, 0, min(HybridMax, len(strong)+len(keyword)+len(meaning)))
	seen := make(map[string]bool, cap(out))
	for _, id := range strong {
		if !seen[id] {
			seen[id] = true
			out = append(out, Fused{ID: id, MatchedOn: "title"})
		}
	}
	type scored struct {
		Fused
		score  float64
		kwRank int // from 1; 0 when the keyword side did not find it
	}
	byID := map[string]*scored{}
	var order []*scored
	get := func(id string) *scored {
		if c := byID[id]; c != nil {
			return c
		}
		c := &scored{Fused: Fused{ID: id, MatchedOn: MatchMeaning}}
		byID[id] = c
		order = append(order, c)
		return c
	}
	for i, k := range keyword {
		if seen[k.ID] || byID[k.ID] != nil {
			continue
		}
		c := get(k.ID)
		c.MatchedOn, c.kwRank = k.MatchedOn, i+1
		c.score += 1.0 / float64(RRFK+i+1)
	}
	meant := make(map[string]bool, len(meaning))
	for i, id := range meaning {
		if seen[id] || meant[id] {
			continue
		}
		meant[id] = true
		get(id).score += 1.0 / float64(RRFK+i+1)
	}
	slices.SortStableFunc(order, func(a, b *scored) int {
		if c := cmp.Compare(b.score, a.score); c != 0 {
			return c
		}
		if (a.kwRank == 0) != (b.kwRank == 0) {
			if a.kwRank != 0 {
				return -1
			}
			return 1
		}
		if c := cmp.Compare(a.kwRank, b.kwRank); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	for _, c := range order {
		out = append(out, c.Fused)
	}
	return out[:min(len(out), HybridMax)]
}

// Watcher is told about project changes that other modules keep derived data for (project
// recommendations, AI-3.1 and AI-2.3). It is called inside the change's transaction; the
// implementation acts after the commit.
type Watcher interface {
	// ProjectChanged: the project's status or owner changed, or it was deleted.
	ProjectChanged(ctx context.Context, projectID string)
	// MembersChanged: userIDs' membership of the project, their role in it, or a join
	// request or invitation between them and it changed (FR-11, FR-10.5).
	MembersChanged(ctx context.Context, projectID string, userIDs ...string)
}
