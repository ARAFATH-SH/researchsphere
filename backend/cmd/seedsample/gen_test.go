package main

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"unicode"
	"unicode/utf8"
)

func testGenerator(t *testing.T, seed int64) *generator {
	t.Helper()
	c, err := loadContent()
	if err != nil {
		t.Fatal(err)
	}
	return &generator{c: c, seed: seed}
}

// tenThousand is the NFR-1 size, generated once for the tests that measure shares.
var tenThousand = sync.OnceValue(func() []profile {
	c, err := loadContent()
	if err != nil {
		panic(err)
	}
	g := &generator{c: c, seed: defaultSeed}
	out := make([]profile, 10000)
	for i := range out {
		out[i] = g.profile(i)
	}
	return out
})

var (
	clusterIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}$`) // sample_profiles checks
	tagSpacing       = regexp.MustCompile(`^\s|\s$|\s\s|[\t\n\r\v\f]`)
	slotPattern      = regexp.MustCompile(`\{[a-z_0-9]+\}`)
	doiLike          = regexp.MustCompile(`(?i)10\.\d{4,9}/|\bdoi\b`)
)

func hasBengali(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Bengali, r) {
			return true
		}
	}
	return false
}

func TestContentIsComplete(t *testing.T) {
	g := testGenerator(t, 1)
	if n := len(g.c.clusters); n != 20 {
		t.Errorf("clusters = %d, want 20", n)
	}
	seen := map[string]bool{}
	for i := range g.c.clusters {
		c := &g.c.clusters[i]
		if !clusterIDPattern.MatchString(c.ID) || seen[c.ID] {
			t.Errorf("cluster ID %q is not a unique slug", c.ID)
		}
		seen[c.ID] = true
		for _, l := range []struct {
			what string
			list []string
			min  int
		}{
			{"interests", c.Interests, 15}, {"skills", c.Skills, 10}, {"departments", c.Departments, 1},
			{"headlines", c.Headlines, 3}, {"bio", c.Bio, 6}, {"methods", c.Methods, 4},
			{"objects", c.Objects, 4}, {"contexts", c.Contexts, 4}, {"titles", c.Titles, 4},
			{"abstracts", c.Abstracts, 3}, {"venues", c.Venues, 2}, {"bn_interests", c.BnInterests, 4},
			{"bn_headlines", c.BnHeadlines, 2}, {"bn_bio", c.BnBio, 4},
		} {
			if len(l.list) < l.min {
				t.Errorf("%s: %d %s, want at least %d", c.ID, len(l.list), l.what, l.min)
			}
		}
		for _, tag := range append(append(append([]string{}, c.Interests...), c.Skills...), c.BnInterests...) {
			if utf8.RuneCountInString(tag) > 60 || tagSpacing.MatchString(tag) || tag == "" {
				t.Errorf("%s: tag %q breaks the tag checks", c.ID, tag)
			}
		}
		for _, s := range append(append([]string{}, c.BnInterests...), append(c.BnHeadlines, c.BnBio...)...) {
			if !hasBengali(s) {
				t.Errorf("%s: %q is meant to be Bangla", c.ID, s)
			}
		}
		// Every template fills: no unknown slot (fill panics) and none left over.
		f := newFiller(g.rng(0, 0), c, "Lecturer", "প্রভাষক")
		for _, list := range [][]string{c.Headlines, c.Bio, c.Titles, c.Abstracts, c.BnHeadlines, c.BnBio} {
			for _, tmpl := range list {
				if got := f.fill(tmpl); slotPattern.MatchString(got) || strings.ContainsAny(got, "{}") {
					t.Errorf("%s: %q filled to %q", c.ID, tmpl, got)
				}
			}
		}
	}
	for _, tag := range append(append([]string{}, g.c.common.NoiseInterests...), g.c.common.NoiseSkills...) {
		if utf8.RuneCountInString(tag) > 60 || tagSpacing.MatchString(tag) {
			t.Errorf("noise tag %q breaks the tag checks", tag)
		}
	}
}

// The same seed makes the same data; another seed makes other data.
func TestSameSeedSameData(t *testing.T) {
	a, b, other := testGenerator(t, 16), testGenerator(t, 16), testGenerator(t, 17)
	differ := 0
	for seq := range 500 {
		pa, pb := a.profile(seq), b.profile(seq)
		if !reflect.DeepEqual(pa, pb) {
			t.Fatalf("profile %d differs between two runs with seed 16:\n%+v\n%+v", seq, pa, pb)
		}
		if !reflect.DeepEqual(pa, other.profile(seq)) {
			differ++
		}
	}
	if differ < 490 {
		t.Errorf("seed 17 made %d of 500 profiles differently, want nearly all", differ)
	}
}

// A top-up makes the same profiles as one run: profile seq does not depend on what was made
// before it, in which order, or by which generator.
func TestTopUpMatchesOneRun(t *testing.T) {
	oneRun := testGenerator(t, 16)
	var direct []profile
	for seq := range 1200 {
		direct = append(direct, oneRun.profile(seq))
	}
	topUp := testGenerator(t, 16)
	for seq := 1199; seq >= 1000; seq-- { // the top-up from 1,000, in another order
		if got := topUp.profile(seq); !reflect.DeepEqual(got, direct[seq]) {
			t.Fatalf("profile %d differs when made in a top-up", seq)
		}
	}
}

func share(ps []profile, keep func(profile) bool) float64 {
	n := 0
	for _, p := range ps {
		if keep(p) {
			n++
		}
	}
	return float64(n) / float64(len(ps))
}

func TestShares(t *testing.T) {
	ps := tenThousand()
	for _, tc := range []struct {
		what      string
		keep      func(profile) bool
		want, tol float64
	}{
		{"test split", func(p profile) bool { return p.label.split == splitTest }, testShare, 0.015},
		{"Bangla or mixed", func(p profile) bool { return p.label.bangla != langNone }, banglaShare, 0.02},
		{"Bangla only", func(p profile) bool { return p.label.bangla == langBangla }, fullBanglaShare, 0.01},
		{"sparse", func(p profile) bool { return p.label.sparse }, sparseShare, 0.015},
		{"second cluster", func(p profile) bool { return p.label.second >= 0 }, secondClusterShare, 0.02},
	} {
		if got := share(ps, tc.keep); math.Abs(got-tc.want) > tc.tol {
			t.Errorf("%s share = %.3f, want %.2f ± %.3f", tc.what, got, tc.want, tc.tol)
		}
	}
	// Every cluster has main members in both splits, so each can be evaluated (NFR-6).
	perCluster := map[int]map[string]int{}
	for _, p := range ps {
		if perCluster[p.label.main] == nil {
			perCluster[p.label.main] = map[string]int{}
		}
		perCluster[p.label.main][p.label.split]++
	}
	for i := range testGenerator(t, 1).c.clusters {
		if perCluster[i][splitTest] < 50 || perCluster[i][splitTuning] < 200 {
			t.Errorf("cluster %d: %v, want at least 50 test and 200 tuning members", i, perCluster[i])
		}
	}
}

// The language label matches the text: Bangla profiles have Bangla text, others none.
func TestBanglaText(t *testing.T) {
	for _, p := range tenThousand() {
		text := p.headline + " " + p.bio + " " + strings.Join(p.interests, " ")
		switch p.label.bangla {
		case langNone:
			if hasBengali(text) {
				t.Fatalf("profile %d is labelled English but has Bangla: %q", p.seq, text)
			}
		case langMixed:
			english := strings.IndexFunc(text, func(r rune) bool { return r < unicode.MaxASCII && unicode.IsLetter(r) }) >= 0
			if !hasBengali(text) || !english {
				t.Fatalf("profile %d is labelled mixed: %q", p.seq, text)
			}
		case langBangla:
			if !hasBengali(p.headline) || (!p.label.sparse && !hasBengali(p.bio)) {
				t.Fatalf("profile %d is labelled Bangla: %q", p.seq, text)
			}
			for _, s := range p.interests {
				if !hasBengali(s) {
					t.Fatalf("profile %d is labelled Bangla but has the interest %q", p.seq, s)
				}
			}
		}
	}
}

func TestSparseProfilesHaveHeadlineAndTwoInterests(t *testing.T) {
	for _, p := range tenThousand() {
		if !p.label.sparse {
			continue
		}
		if p.headline == "" || len(p.interests) != 2 || p.bio != "" || len(p.skills) > 0 ||
			len(p.publications) > 0 || len(p.connections) > 0 || p.institution != "" {
			t.Fatalf("sparse profile %d has more than a headline and 2 interests: %+v", p.seq, p)
		}
	}
}

// Publications are fictional: nothing looks like a DOI (the generator has no DOI field).
func TestNoRealLookingDOIs(t *testing.T) {
	pubs := 0
	for _, p := range tenThousand() {
		for _, pub := range p.publications {
			pubs++
			for _, s := range []string{pub.title, pub.abstract, pub.venue} {
				if doiLike.MatchString(s) {
					t.Fatalf("profile %d has a DOI-like text: %q", p.seq, s)
				}
			}
		}
	}
	if pubs < 15000 {
		t.Errorf("publications = %d, want at least 15,000 over 10,000 profiles", pubs)
	}
}

// Every value fits the database checks, and every link points to an earlier profile.
func TestProfilesFitTheDatabase(t *testing.T) {
	ps := tenThousand()
	g := testGenerator(t, defaultSeed)
	shared := 0
	for _, p := range ps {
		limits := []struct {
			what string
			s    string
			max  int
		}{
			{"name", p.fullName, 200}, {"headline", p.headline, 220}, {"bio", p.bio, 2000},
			{"institution", p.institution, 200}, {"department", p.department, 200},
			{"job title", p.jobTitle, 200}, {"city", p.city, 100},
		}
		for _, l := range limits {
			if utf8.RuneCountInString(l.s) > l.max || (l.s != "" && strings.TrimSpace(l.s) == "") {
				t.Fatalf("profile %d: %s %q breaks its check", p.seq, l.what, l.s)
			}
		}
		if p.country != "" && !regexp.MustCompile(`^[A-Z]{2}$`).MatchString(p.country) {
			t.Fatalf("profile %d: country %q", p.seq, p.country)
		}
		if want := fmt.Sprintf("sample-%05d@sample.researchsphere.local", p.seq); p.email != want {
			t.Fatalf("email = %q, want %q", p.email, want)
		}
		for _, pub := range p.publications {
			if utf8.RuneCountInString(pub.title) > 500 || strings.ContainsAny(pub.title, "\r\n") ||
				utf8.RuneCountInString(pub.abstract) > 5000 || utf8.RuneCountInString(pub.venue) > 300 ||
				pub.year < 2015 || pub.year > 2026 {
				t.Fatalf("profile %d: publication %+v breaks a check", p.seq, pub)
			}
			if pub.coAuthor >= 0 {
				shared++
				if pub.coAuthor >= p.seq || g.label(pub.coAuthor).main != p.label.main {
					t.Fatalf("profile %d: co-author %d is not an earlier author of the same cluster", p.seq, pub.coAuthor)
				}
			}
		}
		seen := map[int]bool{}
		for _, j := range p.connections {
			if j >= p.seq || seen[j] {
				t.Fatalf("profile %d: connection to %d", p.seq, j)
			}
			seen[j] = true
		}
	}
	if shared < 500 {
		t.Errorf("shared publications = %d, want several hundred", shared)
	}
	sameCluster := 0.0
	links := 0.0
	for _, p := range ps {
		for _, j := range p.connections {
			links++
			if g.label(j).main == p.label.main {
				sameCluster++
			}
		}
	}
	if r := sameCluster / links; r < 0.7 {
		t.Errorf("connections within the main cluster = %.2f, want mostly (about 0.8)", r)
	}
}
