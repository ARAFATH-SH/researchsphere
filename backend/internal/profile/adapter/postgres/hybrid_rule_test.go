package postgres

import (
	"regexp"
	"testing"
)

// AI-2.4 in the spirit of AI-5: the queries that rank hybrid search candidates read no
// popularity signal. The keyword side (SearchPeoplePage) is checked too: it ranks by match
// field and name similarity only. Comments may name the rule; QueryText drops them.
func TestHybridQueriesReadNoPopularity(t *testing.T) {
	forbidden := regexp.MustCompile(`(?i)publication|citation|connection|cited|follower|view_count|popular`)
	for _, name := range []string{"StrongNameMatches", "SemanticPeopleCandidates", "SearchPeoplePage"} {
		q, err := QueryText(name)
		if err != nil {
			t.Fatal(err)
		}
		if m := forbidden.FindString(q); m != "" {
			t.Errorf("%s reads %q", name, m)
		}
	}
}

func TestQueryText(t *testing.T) {
	q, err := QueryText("SetHNSWEfSearch")
	if err != nil || q != "SELECT set_config('hnsw.ef_search', @ef_search::text, true)" {
		t.Errorf("QueryText = %q, %v", q, err)
	}
	if _, err := QueryText("NoSuchQuery"); err == nil {
		t.Error("unknown query: no error")
	}
}
