package domain

import (
	"fmt"
	"reflect"
	"testing"
)

func kw(pairs ...string) []KeywordCandidate {
	out := make([]KeywordCandidate, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, KeywordCandidate{UserID: pairs[i], MatchedOn: MatchField(pairs[i+1])})
	}
	return out
}

func TestFusePeople(t *testing.T) {
	tests := []struct {
		name    string
		strong  []string
		keyword []KeywordCandidate
		meaning []string
		want    []Fused
	}{
		{
			name: "empty",
			want: []Fused{},
		},
		{
			name:    "found by both sides beats found by one",
			keyword: kw("a", "interest", "b", "headline"),
			meaning: []string{"c", "b"},
			// b: 1/62 + 1/62; a: 1/61; c: 1/61 (keyword first on a tie)
			want: []Fused{{"b", MatchHeadline}, {"a", MatchInterest}, {"c", MatchMeaning}},
		},
		{
			name:    "a tie between two one-sided candidates: keyword side first",
			keyword: kw("k", "headline"),
			meaning: []string{"m"},
			want:    []Fused{{"k", MatchHeadline}, {"m", MatchMeaning}},
		},
		{
			name:    "meaning-only candidates keep the meaning order",
			meaning: []string{"z", "y"},
			want:    []Fused{{"z", MatchMeaning}, {"y", MatchMeaning}},
		},
		{
			name:    "strong name matches first, even when found by both sides elsewhere",
			strong:  []string{"s1", "s2"},
			keyword: kw("x", "interest", "s2", "name", "s1", "name"),
			meaning: []string{"x", "s1"},
			want:    []Fused{{"s1", MatchName}, {"s2", MatchName}, {"x", MatchInterest}},
		},
		{
			name:    "a strong match only the strong list has (accents) keeps the name field",
			strong:  []string{"jose"},
			meaning: []string{"m"},
			want:    []Fused{{"jose", MatchName}, {"m", MatchMeaning}},
		},
		{
			name:    "duplicates are listed once",
			strong:  []string{"s", "s"},
			keyword: kw("a", "name", "a", "name"),
			meaning: []string{"b", "b", "a"},
			want:    []Fused{{"s", MatchName}, {"a", MatchName}, {"b", MatchMeaning}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FusePeople(tt.strong, tt.keyword, tt.meaning)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FusePeople() = %v, want %v", got, tt.want)
			}
		})
	}
}

// RRF with k = 60: a candidate in the middle of both lists beats the top of one list.
func TestFusePeopleRRFScores(t *testing.T) {
	var keyword []KeywordCandidate
	var meaning []string
	for i := range 10 {
		keyword = append(keyword, KeywordCandidate{UserID: fmt.Sprintf("k%d", i), MatchedOn: MatchHeadline})
		meaning = append(meaning, fmt.Sprintf("m%d", i))
	}
	keyword[5].UserID, meaning[5] = "both", "both" // 2/66 > 1/61
	got := FusePeople(nil, keyword, meaning)
	if got[0].UserID != "both" {
		t.Fatalf("first = %v, want both", got[0])
	}
	// Then k0 and m0 (1/61 each, keyword first), k1 and m1, ...
	want := []string{"both", "k0", "m0", "k1", "m1", "k2", "m2"}
	for i, id := range want {
		if got[i].UserID != id {
			t.Fatalf("got[%d] = %s, want %s (all %v)", i, got[i].UserID, id, got)
		}
	}
}

func TestFusePeopleCutsAtMax(t *testing.T) {
	var strong []string
	var keyword []KeywordCandidate
	var meaning []string
	for i := range HybridDepth {
		strong = append(strong, fmt.Sprintf("s%02d", i))
		keyword = append(keyword, KeywordCandidate{UserID: fmt.Sprintf("k%02d", i), MatchedOn: MatchHeadline})
		meaning = append(meaning, fmt.Sprintf("m%02d", i))
	}
	got := FusePeople(strong, keyword, meaning)
	if len(got) != HybridMax {
		t.Fatalf("len = %d, want %d", len(got), HybridMax)
	}
	if got[0].UserID != "s00" || got[HybridDepth].UserID != "k00" {
		t.Errorf("order = %v ... %v", got[0], got[HybridDepth])
	}
}
