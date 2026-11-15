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
// sqlc's parameters still in it (@q, sqlc.narg(institution)). The AI-5 evaluation and
// benchmark run the hybrid search queries with it (cmd/recseval), and the AI-2.4 test reads
// it.
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
	return strings.TrimSuffix(strings.TrimSpace(strings.Join(code, "\n")), ";"), nil
}
