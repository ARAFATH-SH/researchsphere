package postgres

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// AI-2.4 in the spirit of AI-5: the queries that rank hybrid project search candidates read
// no popularity signal, such as a member count. Comments may name the rule; they are left
// out of the check.
func TestHybridQueriesReadNoPopularity(t *testing.T) {
	b, err := os.ReadFile("queries.sql")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := regexp.MustCompile(`(?i)count\(|publication|citation|follower|popular|member_count`)
	for _, name := range []string{"StrongTitleMatches", "SemanticProjectCandidates", "SearchProjectsPage"} {
		m := regexp.MustCompile(`(?s)-- name: ` + name + ` :\w+\n(.*?)(?:\n-- name: |\z)`).FindStringSubmatch(string(b))
		if m == nil {
			t.Fatalf("query %s not found", name)
		}
		var code []string
		for _, line := range strings.Split(m[1], "\n") {
			if i := strings.Index(line, "--"); i >= 0 {
				line = line[:i]
			}
			code = append(code, line)
		}
		if f := forbidden.FindString(strings.Join(code, "\n")); f != "" {
			t.Errorf("%s reads %q", name, f)
		}
	}
}
