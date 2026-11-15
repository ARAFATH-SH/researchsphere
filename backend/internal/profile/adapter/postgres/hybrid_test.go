//go:build integration

package postgres

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

const hybridModel = "intfloat/multilingual-e5-base@test"

// axis is a unit vector of 768 numbers between basis vectors i and j: angle 0 is i, and a
// larger angle is further from i in cosine.
func axis(i, j int, angle float64) []float32 {
	v := make([]float32, 768)
	v[i] = float32(math.Cos(angle))
	v[j] += float32(math.Sin(angle))
	return v
}

// queryVec is the query vector of these tests: basis vector 0.
var queryVec = domain.QueryVector{Model: hybridModel, Values: axis(0, 1, 0)}

// embed stores v as userID's profile embedding of model.
func (e *searchEnv) embed(userID string, v []float32, model string) {
	e.t.Helper()
	e.exec(`INSERT INTO profile_embeddings (user_id, model, content_hash, embedding)
		VALUES ($1, $2, repeat('a', 64), CAST($3::text AS vector))
		ON CONFLICT (user_id) DO UPDATE SET model = EXCLUDED.model, embedding = EXCLUDED.embedding`,
		userID, model, vectorText(v))
}

// hybrid runs the hybrid search of q with queryVec; edit changes the query first.
func (e *searchEnv) hybrid(q string, edit ...func(*domain.PeopleQuery)) domain.PeopleResults {
	e.t.Helper()
	pq := domain.PeopleQuery{Q: q, Page: 1, PageSize: 20}
	for _, f := range edit {
		f(&pq)
	}
	res, err := e.s.SearchPeopleHybrid(e.t.Context(), pq, queryVec, 20)
	if err != nil {
		e.t.Fatal(err)
	}
	return res
}

func hitByID(res domain.PeopleResults, id string) (domain.PersonHit, bool) {
	i := slices.IndexFunc(res.Hits, func(h domain.PersonHit) bool { return h.UserID == id })
	if i < 0 {
		return domain.PersonHit{}, false
	}
	return res.Hits[i], true
}

// AI-5.2: a profile that shares no word with the text is found by meaning, as "meaning";
// one found by keyword keeps its field; one with neither is not found.
func TestHybridMeaningOnlyMatch(t *testing.T) {
	e := newSearchEnv(t)
	w := testenv.Word(t)
	q := "computers that read " + w
	near := e.add(person{name: "Ada " + testenv.Word(t), headline: "Radiology AI for chest scans", interests: []string{"Medical imaging " + w + "x"}})
	e.embed(near, axis(0, 1, 0.1), hybridModel)
	literal := e.add(person{name: "Bob " + testenv.Word(t), headline: "I build " + q})
	e.embed(literal, axis(0, 1, 1.4), hybridModel)
	noVector := e.add(person{name: "Cy " + testenv.Word(t), headline: "Gardening"})

	res := e.hybrid(q)
	if res.Total != 2 {
		t.Fatalf("total = %d, hits %v; want 2", res.Total, ids(res))
	}
	if h, ok := hitByID(res, near); !ok || h.MatchedOn != domain.MatchMeaning || !strings.HasPrefix(h.FullName, "Ada ") ||
		h.Headline == nil || *h.Headline != "Radiology AI for chest scans" || len(h.Interests) != 1 {
		t.Errorf("meaning hit = %+v, %v", h, ok)
	}
	if h, ok := hitByID(res, literal); !ok || h.MatchedOn != domain.MatchHeadline {
		t.Errorf("keyword hit = %+v, %v", h, ok)
	}
	if _, ok := hitByID(res, noVector); ok {
		t.Error("a profile matching neither way was found")
	}
	// Found by both sides first (1/61 + 1/62), then the meaning-only one (1/61).
	if got := ids(res); !reflect.DeepEqual(got, []string{literal, near}) {
		t.Errorf("order = %v, want [literal near]", got)
	}
}

// A strong name match comes first even when others rank higher by fusion: the full name
// equal to the text, or starting with it at 3 characters or more, ignoring letter case and
// accents. "Na" is too short, and a match inside the name is not strong.
func TestHybridStrongNameFirst(t *testing.T) {
	e := newSearchEnv(t)
	first, last := "Nafisa"+testenv.Word(t), "Hasan"+testenv.Word(t)
	name := first + " " + last
	target := e.add(person{name: name})
	e.embed(target, axis(0, 1, 1.2), hybridModel)
	// Students mention her in their headline and are closest in meaning: by fusion alone
	// (both lists) they beat her (keyword first, meaning last).
	var students []string
	for i := range 3 {
		id := e.add(person{name: fmt.Sprintf("Student %s %d", testenv.Word(t), i), headline: "Student of " + name})
		e.embed(id, axis(0, 1, 0.1*float64(i+1)), hybridModel)
		students = append(students, id)
	}
	// Ten more people, closer in meaning than her and matching no keyword, put her at
	// meaning rank 14: without the pin every student (two lists, ranks at most 4) beats her
	// (keyword 1, meaning 14) whatever order their random names give them.
	for i := range 10 {
		e.embed(e.add(person{name: fmt.Sprintf("Filler %d", i)}), axis(0, 1, 0.4+0.05*float64(i)), hybridModel)
	}
	for _, tt := range []struct {
		q     string
		first bool
	}{
		{name, true},
		{strings.ToUpper(name), true},
		{first, true},                   // a whole first word
		{first + " " + last[:2], true},  // "Nafisa Ha"
		{first[:3], true},               // 3 characters, the start of the name
		{"Na", false},                   // too short
		{last, false},                   // inside the name, not its start
		{first[1:] + " " + last, false}, // not the start
	} {
		res := e.hybrid(tt.q)
		got := ids(res)
		if len(got) == 0 {
			t.Fatalf("%q: no results", tt.q)
		}
		if (got[0] == target) != tt.first {
			t.Errorf("%q: first = %s (target %s, students %v), want target first = %v", tt.q, got[0], target, students, tt.first)
		}
		if tt.first && res.Hits[0].MatchedOn != domain.MatchName {
			t.Errorf("%q: matched on %s", tt.q, res.Hits[0].MatchedOn)
		}
	}
}

// Accents and letter case are folded on both sides of a strong name match; the keyword
// side alone would not find "José" for "jose".
func TestHybridStrongNameIgnoresAccents(t *testing.T) {
	e := newSearchEnv(t)
	w := testenv.Word(t)
	jose := e.add(person{name: "José Müller" + w})
	e.embed(jose, axis(0, 1, 1.5), hybridModel)
	near := e.add(person{name: "Other " + testenv.Word(t)})
	e.embed(near, axis(0, 1, 0), hybridModel)
	for _, q := range []string{"jose muller" + w, "JOSE MÜLLER" + w, "José Muller" + w, "Jose"} {
		res := e.hybrid(q)
		if got := ids(res); len(got) == 0 || got[0] != jose || res.Hits[0].MatchedOn != domain.MatchName {
			t.Errorf("%q: results %v, want José first as a name match", q, got)
		}
	}
}

// fold_name leaves Bangla exactly as written: vowel signs, hasanta, chandrabindu, nukta
// and khanda ta are parts of letters, not accents. Latin letters are folded.
func TestFoldNameKeepsBangla(t *testing.T) {
	e := newSearchEnv(t)
	for in, want := range map[string]string{
		"নাফিসা হাসান":                "নাফিসা হাসান",
		"প্রিয়ঙ্কা কৃষ্ণ জ্যোতি ঋতু": "প্রিয়ঙ্কা কৃষ্ণ জ্যোতি ঋতু",
		"চাঁদ শরৎ য়ৌবন ক্ষৈত্র":      "চাঁদ শরৎ য়ৌবন ক্ষৈত্র",
		"  José   MÜLLER ": "jose muller",
		"Łukasz Øre ß":     "lukasz ore ss",
	} {
		var got string
		if err := e.tx.QueryRow(t.Context(), "SELECT fold_name($1)", in).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("fold_name(%q) = %q, want %q", in, got, want)
		}
	}
	// A Bangla name is still a strong match for itself.
	bn := e.add(person{name: "নাফিসা হাসান"})
	e.embed(bn, axis(0, 1, 1.5), hybridModel)
	other := e.add(person{name: "Other " + testenv.Word(t)})
	e.embed(other, axis(0, 1, 0), hybridModel)
	if got := ids(e.hybrid("নাফিসা")); len(got) == 0 || got[0] != bn {
		t.Errorf("Bangla prefix: %v, want the Bangla name first", got)
	}
}

// The institution and interest filters apply to the strong, keyword and meaning sides.
func TestHybridFiltersBothSides(t *testing.T) {
	e := newSearchEnv(t)
	w, instA, instB := testenv.Word(t), "Institute "+testenv.Word(t), "College "+testenv.Word(t)
	nlp := "NLP " + testenv.Word(t)
	kwA := e.add(person{name: "Kw A " + testenv.Word(t), headline: "about " + w, institution: instA, interests: []string{nlp}})
	kwB := e.add(person{name: "Kw B " + testenv.Word(t), headline: "about " + w, institution: instB})
	meanA := e.add(person{name: "Mean A " + testenv.Word(t), institution: instA, interests: []string{nlp}})
	meanB := e.add(person{name: "Mean B " + testenv.Word(t), institution: instB})
	strongB := e.add(person{name: w + " Strong", institution: instB})
	for i, id := range []string{kwA, kwB, meanA, meanB, strongB} {
		e.embed(id, axis(0, 1, 0.1*float64(i)), hybridModel)
	}
	all := e.hybrid(w)
	if all.Total != 5 {
		t.Fatalf("unfiltered total = %d, want 5", all.Total)
	}
	inst := func(q *domain.PeopleQuery) { s := strings.ToLower(instA); q.Institution = &s }
	if got := ids(e.hybrid(w, inst)); !sameSet(got, []string{kwA, meanA}) {
		t.Errorf("institution filter: %v, want kwA and meanA", got)
	}
	interest := func(q *domain.PeopleQuery) { q.Interests = []string{strings.ToUpper(nlp)} }
	if got := ids(e.hybrid(w, interest)); !sameSet(got, []string{kwA, meanA}) {
		t.Errorf("interest filter: %v, want kwA and meanA", got)
	}
}

// The meaning side has the people search visibility rule: verified, not suspended, nobody
// who blocked the searcher (FR-16.9); one the searcher blocked is still found. Embeddings of
// another model are not compared.
func TestHybridVisibility(t *testing.T) {
	e := newSearchEnv(t)
	viewer := e.add(person{name: "Viewer " + testenv.Word(t)})
	visible := e.add(person{name: "Visible " + testenv.Word(t)})
	unverified := e.add(person{name: "Unverified " + testenv.Word(t), unverified: true})
	suspended := e.add(person{name: "Suspended " + testenv.Word(t), suspended: true})
	blocksViewer := e.add(person{name: "Blocker " + testenv.Word(t)})
	blockedByViewer := e.add(person{name: "Blocked " + testenv.Word(t)})
	otherModel := e.add(person{name: "Other model " + testenv.Word(t)})
	for _, id := range []string{visible, unverified, suspended, blocksViewer, blockedByViewer} {
		e.embed(id, axis(0, 1, 0.2), hybridModel)
	}
	e.embed(otherModel, axis(0, 1, 0), "other/model@1")
	e.exec("INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2), ($2, $3)", blocksViewer, viewer, blockedByViewer)

	res := e.hybrid("unrelated "+testenv.Word(t), func(q *domain.PeopleQuery) { q.ViewerID = viewer })
	if got := ids(res); !sameSet(got, []string{visible, blockedByViewer}) || res.Total != 2 {
		t.Errorf("results = %v (total %d), want visible and the one the viewer blocked", got, res.Total)
	}
}

// Facets count the merged list: the institution facet without its own filter, the interest
// facet with every filter, selected interests first.
func TestHybridFacets(t *testing.T) {
	e := newSearchEnv(t)
	w, instA, instB := testenv.Word(t), "Institute "+testenv.Word(t), "College "+testenv.Word(t)
	nlp, eye := "NLP "+testenv.Word(t), "Eye "+testenv.Word(t)
	a1 := e.add(person{name: "A1 " + testenv.Word(t), headline: w, institution: instA, interests: []string{nlp, eye}})
	a2 := e.add(person{name: "A2 " + testenv.Word(t), institution: instA, interests: []string{nlp}})
	b1 := e.add(person{name: "B1 " + testenv.Word(t), institution: instB, interests: []string{eye}})
	e.add(person{name: "No match " + testenv.Word(t), institution: instB, interests: []string{eye}}) // neither side
	for i, id := range []string{a1, a2, b1} {
		e.embed(id, axis(0, 1, 0.1*float64(i)), hybridModel)
	}
	res := e.hybrid(w)
	if !reflect.DeepEqual(res.Institutions, []domain.Facet{{Name: instA, Count: 2}, {Name: instB, Count: 1}}) {
		t.Errorf("institutions = %v", res.Institutions)
	}
	wantInterests := []domain.Facet{{Name: eye, Count: 2}, {Name: nlp, Count: 2}}
	if eye > nlp {
		wantInterests[0], wantInterests[1] = wantInterests[1], wantInterests[0]
	}
	if !reflect.DeepEqual(res.Interests, wantInterests) {
		t.Errorf("interests = %v, want %v", res.Interests, wantInterests)
	}
	filtered := e.hybrid(w, func(q *domain.PeopleQuery) { q.Institution = &instB; q.Interests = []string{eye} })
	if got := ids(filtered); !reflect.DeepEqual(got, []string{b1}) {
		t.Errorf("filtered = %v, want b1", got)
	}
	// Without its own filter: both institutions, among those with the interest; a tie in
	// name order.
	if !reflect.DeepEqual(filtered.Institutions, []domain.Facet{{Name: instB, Count: 1}, {Name: instA, Count: 1}}) {
		t.Errorf("filtered institutions = %v", filtered.Institutions)
	}
	if !reflect.DeepEqual(filtered.Interests, []domain.Facet{{Name: eye, Count: 1}}) {
		t.Errorf("filtered interests = %v", filtered.Interests)
	}
}

// 20 per page over the merged list; a page past its end is empty with the same total.
func TestHybridPaging(t *testing.T) {
	e := newSearchEnv(t)
	w := testenv.Word(t)
	for i := range 25 {
		id := e.add(person{name: fmt.Sprintf("P%02d %s", i, testenv.Word(t))})
		e.embed(id, axis(0, 1, 0.01*float64(i)), hybridModel)
	}
	p1 := e.hybrid(w)
	p2 := e.hybrid(w, func(q *domain.PeopleQuery) { q.Page = 2 })
	p3 := e.hybrid(w, func(q *domain.PeopleQuery) { q.Page = 3 })
	if len(p1.Hits) != 20 || len(p2.Hits) != 5 || len(p3.Hits) != 0 || p1.Total != 25 || p3.Total != 25 {
		t.Fatalf("pages %d, %d, %d; totals %d, %d", len(p1.Hits), len(p2.Hits), len(p3.Hits), p1.Total, p3.Total)
	}
	// The meaning side alone: closest first.
	for i, h := range p1.Hits {
		if !strings.HasPrefix(h.FullName, fmt.Sprintf("P%02d ", i)) || h.MatchedOn != domain.MatchMeaning {
			t.Fatalf("hit %d = %s (%s)", i, h.FullName, h.MatchedOn)
		}
	}
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
