package main

import (
	"math"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// The fit recovers the slope and intercept of data drawn from a known logistic curve.
func TestFitLogistic(t *testing.T) {
	const wantA, wantB = 40.0, -34.0 // 50% at cos 0.85
	r := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // test data
	var pairs []pair
	for range 200_000 {
		c := 0.7 + 0.25*r.Float64()
		p := 1 / (1 + math.Exp(-(wantA*c + wantB)))
		pairs = append(pairs, pair{c, r.Float64() < p})
	}
	a, b, err := fitLogistic(pairs)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(a-wantA) > 2 || math.Abs(b-wantB) > 2 || math.Abs(-b/a-0.85) > 0.005 {
		t.Errorf("fit a=%.2f b=%.2f (midpoint %.4f), want %.0f %.0f (0.85)", a, b, -b/a, wantA, wantB)
	}

	// Perfectly separated data still gives a finite, increasing map.
	sep := []pair{{0.7, false}, {0.75, false}, {0.85, true}, {0.9, true}}
	if a, b, err := fitLogistic(sep); err != nil || !(a > 0) || math.IsInf(a, 0) || math.IsNaN(b) {
		t.Errorf("separable: a=%v b=%v err=%v", a, b, err)
	}
	if _, _, err := fitLogistic([]pair{{0.8, true}}); err == nil {
		t.Error("fit of one pair: no error")
	}
	if _, _, err := fitLogistic([]pair{{0.8, true}, {0.8, false}}); err == nil {
		t.Error("fit of equal cosines: no error")
	}
}

// docs/evaluation.md defines the three measures; the capped recall equals precision when at
// least k are relevant and rewards a full list when fewer are.
func TestQueryMetrics(t *testing.T) {
	tests := []struct {
		hits, relevant, k int
		p, capped, std    float64
		ok                bool
	}{
		{7, 500, 10, 0.7, 0.7, 7.0 / 500, true},
		{10, 500, 10, 1, 1, 10.0 / 500, true},
		{0, 500, 10, 0, 0, 0, true},
		{4, 4, 10, 0.4, 1, 1, true},
		{2, 4, 10, 0.2, 0.5, 0.5, true},
		{0, 0, 10, 0, 0, 0, false},
	}
	for _, tt := range tests {
		p, c, s, ok := queryMetrics(tt.hits, tt.relevant, tt.k)
		if ok != tt.ok || math.Abs(p-tt.p) > 1e-12 || math.Abs(c-tt.capped) > 1e-12 || math.Abs(s-tt.std) > 1e-12 {
			t.Errorf("queryMetrics(%d, %d, %d) = %v %v %v %v, want %v %v %v %v",
				tt.hits, tt.relevant, tt.k, p, c, s, ok, tt.p, tt.capped, tt.std, tt.ok)
		}
	}
	var r evalResult
	r.add(5, 500)
	r.add(10, 500)
	r.add(3, 0)
	r.finish()
	if r.queries != 2 || r.skipped != 1 || math.Abs(r.precision-0.75) > 1e-12 || math.Abs(r.capped-0.75) > 1e-12 {
		t.Errorf("evalResult = %+v", r)
	}
}

func TestPercentile(t *testing.T) {
	var ts []time.Duration
	for i := 1; i <= 200; i++ {
		ts = append(ts, time.Duration(i)*time.Millisecond)
	}
	if p := percentile(ts, 50); p != 100*time.Millisecond {
		t.Errorf("p50 = %v", p)
	}
	if p := percentile(ts, 95); p != 190*time.Millisecond {
		t.Errorf("p95 = %v", p)
	}
	if p := percentile(ts, 100); p != 200*time.Millisecond {
		t.Errorf("p100 = %v", p)
	}
	if p := percentile(nil, 95); p != 0 {
		t.Errorf("empty = %v", p)
	}
}

// The draw is fixed by its seed and stream, and returns distinct samples.
func TestDraw(t *testing.T) {
	var from []sample
	for i := range 100 {
		from = append(from, sample{id: string(rune('a' + i))})
	}
	a, b := draw(from, 10, 17, 3), draw(from, 10, 17, 3)
	seen := map[string]bool{}
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("same seed, different draw")
		}
		if seen[a[i].id] {
			t.Fatal("drawn twice")
		}
		seen[a[i].id] = true
	}
	if len(draw(from, 500, 1, 1)) != 100 {
		t.Error("drew more than there are")
	}
}

func TestCheckTarget(t *testing.T) {
	const local = "postgres://u:p@localhost:5433/researchsphere?sslmode=disable" //nolint:gosec // a made-up URL whose password the errors must not repeat
	tests := []struct {
		url, env, wantErr string
	}{
		{local, "development", ""},
		{local, "production", "production"},
		{local, "", "APP_ENV"},
		{"", "development", "DATABASE_URL"},
		{"postgres://u:p@localhost:5433/researchsphere_test", "development", "test database"},
		{"postgres://u:p@db.example.com:5432/researchsphere", "development", "only a local"},
	}
	for _, tt := range tests {
		err := checkTarget(tt.url, tt.env)
		if (tt.wantErr == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.wantErr)) {
			t.Errorf("checkTarget(%q, %q) = %v, want %q", tt.url, tt.env, err, tt.wantErr)
		}
		if err != nil && strings.Contains(err.Error(), ":p@") {
			t.Errorf("error repeats the password: %v", err)
		}
	}
}

func TestParseFlags(t *testing.T) {
	if o, err := parseFlags([]string{"calibrate"}); err != nil || o.queries != 2000 || o.seed != defaultSeed {
		t.Errorf("calibrate: %+v %v", o, err)
	}
	if o, err := parseFlags([]string{"-n", "50", "bench"}); err != nil || o.queries != 50 {
		t.Errorf("bench -n 50: %+v %v", o, err)
	}
	if o, err := parseFlags([]string{"bench-search"}); err != nil || o.queries != 200 {
		t.Errorf("bench-search: %+v %v", o, err)
	}
	if o, err := parseFlags([]string{"eval-search"}); err != nil || o.mode != "eval-search" {
		t.Errorf("eval-search: %+v %v", o, err)
	}
	for _, bad := range [][]string{{}, {"tune"}, {"-n", "5", "eval"}, {"eval", "extra"}, {"-n", "-1", "bench"}, {"-n", "5", "eval-search"}} {
		if _, err := parseFlags(bad); err == nil {
			t.Errorf("parseFlags(%q): no error", bad)
		}
	}
}

func TestScoreMapFromEnv(t *testing.T) {
	env := map[string]string{"RECS_SCORE_MODEL": "m@1", "RECS_SCORE_SLOPE": "30", "RECS_SCORE_INTERCEPT": "-25"}
	m, err := scoreMapFromEnv(func(k string) string { return env[k] })
	if err != nil || m.Model != "m@1" || m.Slope != 30 || m.Intercept != -25 {
		t.Errorf("scoreMapFromEnv = %+v, %v", m, err)
	}
	env["RECS_SCORE_SLOPE"] = "-1"
	if _, err := scoreMapFromEnv(func(k string) string { return env[k] }); err == nil {
		t.Error("negative slope accepted")
	}
}

// Two profiles are related when any of their clusters, main or second, is the same.
func TestRelated(t *testing.T) {
	tests := []struct {
		a, b sample
		want bool
	}{
		{sample{cluster: "x"}, sample{cluster: "x"}, true},
		{sample{cluster: "x"}, sample{cluster: "y"}, false},
		{sample{cluster: "x", second: "y"}, sample{cluster: "y"}, true},
		{sample{cluster: "x"}, sample{cluster: "y", second: "x"}, true},
		{sample{cluster: "x", second: "z"}, sample{cluster: "y", second: "z"}, true},
		{sample{cluster: "x", second: ""}, sample{cluster: "y", second: ""}, false},
	}
	for _, tt := range tests {
		if got, back := related(tt.a, tt.b), related(tt.b, tt.a); got != tt.want || back != tt.want {
			t.Errorf("related(%+v, %+v) = %v / %v, want %v", tt.a, tt.b, got, back, tt.want)
		}
	}
}

func TestScoreRow(t *testing.T) {
	var scores []int
	for i := 100; i >= 1; i-- {
		scores = append(scores, i)
	}
	if got, want := scoreRow("g", scores), "| g | 100 | 50.5 | 10 | 25 | 50 | 75 | 90 |"; got != want {
		t.Errorf("scoreRow = %q, want %q", got, want)
	}
	if scores[0] != 100 {
		t.Error("scoreRow sorted its input")
	}
	if got := scoreRow("empty", nil); !strings.HasPrefix(got, "| empty | 0 |") {
		t.Errorf("empty row = %q", got)
	}
}
