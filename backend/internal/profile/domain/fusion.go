package domain

import (
	"cmp"
	"slices"
)

// Hybrid search (AI-5.1, AI-5.2): how the keyword and meaning candidates are merged.
const (
	// RRFK is the k of reciprocal rank fusion: a candidate at rank r (from 1) of a list
	// scores 1 / (RRFK + r) for it. 60 is the value of the original paper, not tuned here.
	RRFK = 60
	// HybridDepth is how many candidates each side contributes.
	HybridDepth = 50
	// HybridMax is the most results a hybrid search returns over all pages.
	HybridMax = 100
)

// KeywordCandidate is one keyword result in keyword order, with the field it matched.
type KeywordCandidate struct {
	UserID    string
	MatchedOn MatchField
}

// Fused is one result of a merged hybrid list.
type Fused struct {
	UserID    string
	MatchedOn MatchField
}

// FusePeople merges the three candidate lists of a hybrid search, best first, and cuts the
// result at HybridMax.
//
// Strong name matches (the full name equals the text, or starts with it) come first, in
// their own order, as matched on the name. Every other candidate is ordered by its
// reciprocal rank fusion score over the keyword and meaning lists, then those the keyword
// side found first, then by keyword rank, then by user ID. It keeps the keyword side's
// field; a candidate only the meaning side found matched on MatchMeaning.
//
// AI-2.4: only ranks in the lists count; no popularity signal reaches this function.
func FusePeople(strong []string, keyword []KeywordCandidate, meaning []string) []Fused {
	out := make([]Fused, 0, min(HybridMax, len(strong)+len(keyword)+len(meaning)))
	seen := make(map[string]bool, cap(out))
	for _, id := range strong {
		if !seen[id] {
			seen[id] = true
			out = append(out, Fused{UserID: id, MatchedOn: MatchName})
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
		c := &scored{Fused: Fused{UserID: id, MatchedOn: MatchMeaning}}
		byID[id] = c
		order = append(order, c)
		return c
	}
	for i, k := range keyword {
		if seen[k.UserID] || byID[k.UserID] != nil {
			continue
		}
		c := get(k.UserID)
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
		return cmp.Compare(a.UserID, b.UserID)
	})
	for _, c := range order {
		out = append(out, c.Fused)
	}
	return out[:min(len(out), HybridMax)]
}
