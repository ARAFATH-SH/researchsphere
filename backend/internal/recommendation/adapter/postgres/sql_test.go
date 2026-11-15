package postgres

import (
	"strings"
	"testing"
)

// The ranking query reads no popularity signal: no counts, no publications or citations
// (AI-2.4). Comments are left out of the check.
func TestRankQueryReadsNoPopularity(t *testing.T) {
	for _, name := range []string{"RankPeople", "StillEligible", "SelfFields"} {
		q, err := QueryText(name)
		if err != nil {
			t.Fatal(err)
		}
		sql := strings.ToLower(q)
		for _, banned := range []string{"count(", "publication", "citation", "cited", "follower", "popular", "score"} {
			if strings.Contains(sql, banned) {
				t.Errorf("%s reads %q; ranking may use only the embedding similarity (AI-2.4)", name, banned)
			}
		}
	}
	if _, err := QueryText("NoSuchQuery"); err == nil {
		t.Error("QueryText found a query that does not exist")
	}
	q, err := RankPeopleSQL()
	if err != nil || strings.Contains(q, "@") || !strings.Contains(q, "$1") || !strings.Contains(q, "LIMIT $2") {
		t.Errorf("RankPeopleSQL = %q, %v", q, err)
	}
}
