package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Wilson interval of cases worked by hand (81 of 100: centre 0.7985, half-width 0.0763).
func TestWilson(t *testing.T) {
	for _, tt := range []struct {
		hits, n int
		lo, hi  float64
	}{
		{81, 100, 0.7222, 0.8749},
		{0, 10, 0, 0.2775},
		{10, 10, 0.7225, 1},
		{0, 0, 0, 0},
	} {
		lo, hi := wilson(tt.hits, tt.n)
		if math.Abs(lo-tt.lo) > 5e-4 || math.Abs(hi-tt.hi) > 5e-4 {
			t.Errorf("wilson(%d, %d) = %.4f, %.4f; want %.4f, %.4f", tt.hits, tt.n, lo, hi, tt.lo, tt.hi)
		}
	}
}

// The chance of a hit among k random venues: none relevant is 0, too few others is 1, and
// one relevant of 10 with k 5 is 1/2.
func TestChanceHit(t *testing.T) {
	for _, tt := range []struct {
		n, r, k int
		want    float64
	}{{150, 0, 5, 0}, {10, 1, 5, 0.5}, {6, 2, 5, 1}, {150, 7, 5, 1 - (143.0 * 142 * 141 * 140 * 139 / (150.0 * 149 * 148 * 147 * 146))}} {
		if got := chanceHit(tt.n, tt.r, tt.k); math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("chanceHit(%d, %d, %d) = %v, want %v", tt.n, tt.r, tt.k, got, tt.want)
		}
	}
}

// The evaluation ranks with the endpoint's own query; only its me CTE is replaced, and the
// result has numbered parameters only.
func TestRankVenuesSQL(t *testing.T) {
	q, err := rankVenuesSQL()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{venueMeByArg, "v.submission_deadline > now()", "v.removed_at IS NULL", "v.rolling = $3::boolean", "LIMIT $4",
		"e.model = (SELECT model FROM me)", "ORDER BY e.embedding <=> (SELECT embedding FROM me)"} {
		if !strings.Contains(q, want) {
			t.Errorf("query lacks %q:\n%s", want, q)
		}
	}
	if strings.Contains(q, "@") || strings.Contains(q, "project_abstract_embeddings") {
		t.Errorf("query still reads the project or has a named parameter:\n%s", q)
	}
}

// writeSection replaces the marked block, adds one when there is none, and refuses a broken
// one.
func TestWriteSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evaluation.md")
	section := "<!-- eval-venues:begin x -->\nnew\n<!-- eval-venues:end -->\n"
	write := func(s string) {
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func() string {
		b, err := os.ReadFile(path) //nolint:gosec // a file in the test's own temporary folder
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	write("# Doc\n\nText.\n")
	if err := writeSection(path, section); err != nil || read() != "# Doc\n\nText.\n\n"+section {
		t.Errorf("add: %q, %v", read(), err)
	}
	write("# Doc\n<!-- eval-venues:begin old -->\nold\n<!-- eval-venues:end -->\nAfter.\n")
	if err := writeSection(path, section); err != nil || read() != "# Doc\n"+section+"After.\n" {
		t.Errorf("replace: %q, %v", read(), err)
	}
	write("<!-- eval-venues:end -->\n<!-- eval-venues:begin -->\n")
	if err := writeSection(path, section); err == nil {
		t.Error("a broken block was accepted")
	}
}

func TestVenueFlagsAndTargets(t *testing.T) {
	for _, args := range [][]string{{"eval-venues"}, {"calibrate-venues"}, {"-write", "../docs/evaluation.md", "eval-venues"}} {
		if _, err := parseFlags(args); err != nil {
			t.Errorf("parseFlags(%v) = %v", args, err)
		}
	}
	for _, args := range [][]string{{"-write", "x.md", "eval"}, {"-write", "x.md", "calibrate-venues"}, {"-n", "5", "eval-venues"}} {
		if _, err := parseFlags(args); err == nil {
			t.Errorf("parseFlags(%v) accepted", args)
		}
	}
	const pass = "secretpw"
	for url, ok := range map[string]bool{
		"postgres://u:" + pass + "@localhost:5433/researchsphere_test": true,
		"postgres://u:" + pass + "@localhost:5433/researchsphere":      false,
		"postgres://u:" + pass + "@db.example.org/researchsphere_test": false,
		"": false,
	} {
		err := checkTestTarget(url)
		if (err == nil) != ok || (err != nil && strings.Contains(err.Error(), pass)) {
			t.Errorf("checkTestTarget(%q) = %v", url, err)
		}
	}
}
