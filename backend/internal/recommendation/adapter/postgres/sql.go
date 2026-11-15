package postgres

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"
)

//go:embed queries.sql
var queriesSQL string

// QueryText returns the SQL of the named query in queries.sql without its comments, with
// sqlc's named parameters still in it (@user_id, @lim). The NFR-1 benchmark explains the
// ranking query with it (cmd/recseval), and the AI-2.4 test reads it.
func QueryText(name string) (string, error) {
	m := regexp.MustCompile(`(?s)-- name: ` + regexp.QuoteMeta(name) + ` :\w+\n(.*?)(?:\n-- name: |\z)`).FindStringSubmatch(queriesSQL)
	if m == nil {
		return "", fmt.Errorf("query %s not found", name)
	}
	var code []string
	for _, line := range strings.Split(m[1], "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		if strings.TrimSpace(line) != "" {
			code = append(code, line)
		}
	}
	return strings.TrimSuffix(strings.Join(code, "\n"), ";"), nil
}

// RankPeopleSQL is the ranking query with $1 for the caller's ID and $2 for the limit, as
// sqlc numbers them, for EXPLAIN.
func RankPeopleSQL() (string, error) {
	q, err := QueryText("RankPeople")
	if err != nil {
		return "", err
	}
	q = strings.ReplaceAll(q, "@user_id", "$1")
	return strings.ReplaceAll(q, "@lim", "$2"), nil
}
