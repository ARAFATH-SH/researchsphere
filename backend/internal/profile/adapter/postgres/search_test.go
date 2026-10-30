//go:build integration

package postgres

import (
	"context"
	"crypto/rand"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// searchEnv seeds users inside one transaction that is rolled back when the test ends,
// and searches inside it (a savepoint stands for the search's own transaction).
type searchEnv struct {
	t  *testing.T
	tx pgx.Tx
	s  *Store
}

func newSearchEnv(t *testing.T) *searchEnv {
	t.Helper()
	_, pool := newTestStore(t)
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	s := &Store{q: sqlcgen.New(tx), searchTx: func(ctx context.Context, fn func(pgx.Tx) error) error {
		return pgx.BeginFunc(ctx, tx, fn)
	}}
	return &searchEnv{t: t, tx: tx, s: s}
}

// person is a seeded user. Empty strings are unset fields.
type person struct {
	name, headline, institution, bio string
	interests                        []string
	email, phone, link               string
	unverified, suspended            bool
}

// add stores p and returns the user ID.
func (e *searchEnv) add(p person) string {
	e.t.Helper()
	ctx := e.t.Context()
	email := p.email
	if email == "" {
		email = "it-" + strings.ToLower(rand.Text()) + "@example.org"
	}
	var id string
	err := e.tx.QueryRow(ctx, `INSERT INTO users (email, password_hash, email_verified_at, suspended_at)
		VALUES ($1, 'hash', CASE WHEN $2 THEN NULL ELSE now() END, CASE WHEN $3 THEN now() END) RETURNING id`,
		email, p.unverified, p.suspended).Scan(&id)
	if err != nil {
		e.t.Fatal(err)
	}
	e.exec(`INSERT INTO profiles (user_id, full_name, headline, institution, bio)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''))`, id, p.name, p.headline, p.institution, p.bio)
	for _, name := range p.interests {
		e.exec("INSERT INTO research_interests (name) VALUES ($1) ON CONFLICT (name) DO NOTHING", name)
		e.exec("INSERT INTO user_interests (user_id, interest_id) SELECT $1, id FROM research_interests WHERE name = $2", id, name)
	}
	if p.phone != "" {
		e.exec("INSERT INTO profile_contact (user_id, phone, phone_visibility) VALUES ($1, $2, 'logged_in')", id, p.phone)
	}
	if p.link != "" {
		e.exec("INSERT INTO profile_links (user_id, kind, url) VALUES ($1, 'website', $2)", id, p.link)
	}
	return id
}

func (e *searchEnv) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.tx.Exec(e.t.Context(), sql, args...); err != nil {
		e.t.Fatal(err)
	}
}

// search runs q; edit changes the query before it runs.
func (e *searchEnv) search(q string, edit ...func(*domain.PeopleQuery)) domain.PeopleResults {
	e.t.Helper()
	pq := domain.PeopleQuery{Q: q, Page: 1, PageSize: 20}
	for _, f := range edit {
		f(&pq)
	}
	res, err := e.s.SearchPeople(e.t.Context(), pq, 20)
	if err != nil {
		e.t.Fatal(err)
	}
	return res
}

func ids(res domain.PeopleResults) []string {
	out := make([]string, len(res.Hits))
	for i, h := range res.Hits {
		out[i] = h.UserID
	}
	return out
}

func matched(res domain.PeopleResults) []domain.MatchField {
	out := make([]domain.MatchField, len(res.Hits))
	for i, h := range res.Hits {
		out[i] = h.MatchedOn
	}
	return out
}

// Name exact > prefix > contains, then institution, interest and headline; letter case
// is ignored and a profile's best match decides (FR-6.1).
func TestSearchPeopleMatchesAndRanks(t *testing.T) {
	e := newSearchEnv(t)
	w := testenv.Word(t)
	exact := e.add(person{name: w})
	prefix := e.add(person{name: w + " " + testenv.Word(t)})
	contains := e.add(person{name: testenv.Word(t) + " " + w, institution: "University of " + w}) // name beats institution
	inst := e.add(person{name: testenv.Word(t), institution: "University of " + w})
	interest := e.add(person{name: testenv.Word(t), interests: []string{w + " studies"}})
	headline := e.add(person{name: testenv.Word(t), headline: "Working on " + w})
	e.add(person{name: testenv.Word(t)}) // matches nothing

	res := e.search(strings.ToLower(w))
	want := []string{exact, prefix, contains, inst, interest, headline}
	if got := ids(res); !reflect.DeepEqual(got, want) {
		t.Fatalf("results = %v, want %v", got, want)
	}
	wantOn := []domain.MatchField{domain.MatchName, domain.MatchName, domain.MatchName, domain.MatchInstitution, domain.MatchInterest, domain.MatchHeadline}
	if got := matched(res); !reflect.DeepEqual(got, wantOn) {
		t.Errorf("matchedOn = %v, want %v", got, wantOn)
	}
	if res.Total != 6 || res.Fuzzy || res.CorrectedQuery != nil {
		t.Errorf("total = %d, fuzzy = %v, corrected = %v; want 6, false, nil", res.Total, res.Fuzzy, res.CorrectedQuery)
	}
	h := res.Hits[2]
	if h.FullName == "" || h.Institution == nil || *h.Institution != "University of "+w || h.Headline != nil || h.PhotoFileID != nil {
		t.Errorf("hit = %+v", h)
	}
}

// A name with a one-letter typo is found by similarity (FR-6.2), and the response says
// which name it was taken for.
func TestSearchPeopleTypo(t *testing.T) {
	e := newSearchEnv(t)
	first, last := testenv.Word(t), testenv.Word(t)
	name := first + " " + last
	id := e.add(person{name: name})
	e.add(person{name: testenv.Word(t) + " " + testenv.Word(t)}) // not similar

	typo := first + " " + last[:4] + "x" + last[5:]
	res := e.search(typo)
	if got := ids(res); !reflect.DeepEqual(got, []string{id}) {
		t.Fatalf("results = %v, want [%s]", got, id)
	}
	if !res.Fuzzy || res.CorrectedQuery == nil || *res.CorrectedQuery != name || res.Hits[0].MatchedOn != domain.MatchName {
		t.Errorf("fuzzy = %v, corrected = %v, matchedOn = %s; want true, %q, name", res.Fuzzy, res.CorrectedQuery, res.Hits[0].MatchedOn, name)
	}
}

// Similar names are used only when no visible name contains the text; then literal
// institution, interest and headline matches still come after them.
func TestSearchPeopleFuzzyOnlyWithoutLiteralName(t *testing.T) {
	e := newSearchEnv(t)
	first, last := testenv.Word(t), testenv.Word(t)
	name := first + " " + last
	exact := e.add(person{name: name})
	e.add(person{name: first + " " + last[:4] + "x" + last[5:]}) // similar, not containing

	res := e.search(name)
	if got := ids(res); !reflect.DeepEqual(got, []string{exact}) || res.Fuzzy {
		t.Errorf("results = %v, fuzzy = %v; want only the literal match, not fuzzy", got, res.Fuzzy)
	}
}

// The only literal match is suspended: it does not count, so similar names are used, and
// literal headline matches still come after them.
func TestSearchPeopleFuzzyWhenLiteralNameIsHidden(t *testing.T) {
	e := newSearchEnv(t)
	first, last := testenv.Word(t), testenv.Word(t)
	e.add(person{name: first + " " + last, suspended: true})
	typo := e.add(person{name: first + " " + last[:4] + "x" + last[5:]})
	other := e.add(person{name: testenv.Word(t), headline: "About " + first + " " + last})
	res := e.search(first + " " + last)
	if got := ids(res); !reflect.DeepEqual(got, []string{typo, other}) || !res.Fuzzy {
		t.Errorf("results = %v, fuzzy = %v; want [%s %s], fuzzy", got, res.Fuzzy, typo, other)
	}
	if got := matched(res); !reflect.DeepEqual(got, []domain.MatchField{domain.MatchName, domain.MatchHeadline}) {
		t.Errorf("matchedOn = %v", got)
	}
}

// Unverified and suspended accounts are never found, as on the profile page.
func TestSearchPeopleOnlyVisibleAccounts(t *testing.T) {
	e := newSearchEnv(t)
	w := testenv.Word(t)
	visible := e.add(person{name: w + " Visible"})
	e.add(person{name: w + " Unverified", unverified: true})
	e.add(person{name: w + " Suspended", suspended: true})
	e.add(person{name: testenv.Word(t), institution: w, suspended: true})
	e.add(person{name: testenv.Word(t), interests: []string{w}, unverified: true})

	res := e.search(w)
	if got := ids(res); !reflect.DeepEqual(got, []string{visible}) || res.Total != 1 {
		t.Errorf("results = %v, total = %d; want only %s", got, res.Total, visible)
	}
	if len(res.Institutions) != 0 {
		t.Errorf("institution facets = %v, want none from hidden accounts", res.Institutions)
	}
}

// Email, phone, links and bio are never searched.
func TestSearchPeopleContactInfoNotSearchable(t *testing.T) {
	e := newSearchEnv(t)
	w := strings.ToLower(testenv.Word(t))
	e.add(person{
		name:  testenv.Word(t),
		email: w + "@example.org",
		phone: "+4915550192837",
		link:  "https://example.org/" + w,
		bio:   "My bio mentions " + w,
	})
	for _, q := range []string{w, w + "@example.org", "4915550192837", "+4915550192837", "example.org/" + w} {
		if res := e.search(q); res.Total != 0 {
			t.Errorf("search %q found %v", q, ids(res))
		}
	}
}

// % and _ in the text match only themselves: a text with a wildcard in place of letters
// matches no name literally, so only a similar name can be found (fuzzy).
func TestSearchPeopleEscapesWildcards(t *testing.T) {
	e := newSearchEnv(t)
	w := testenv.Word(t)
	e.add(person{name: w})
	for _, q := range []string{w[:3] + "%" + w[5:], w[:3] + "_" + w[4:], w[:3] + `\` + w[4:]} {
		if res := e.search(q); !res.Fuzzy {
			t.Errorf("search %q matched literally: %v", q, ids(res))
		}
	}
	id := e.add(person{name: w + " 100%_done"})
	if got := ids(e.search("100%_done")); !reflect.DeepEqual(got, []string{id}) {
		t.Errorf("literal %%_ search = %v, want [%s]", got, id)
	}
}

// The institution filter matches the whole institution ignoring case; the interest
// filter needs every selected interest; both together narrow further (FR-6.3).
func TestSearchPeopleFilters(t *testing.T) {
	e := newSearchEnv(t)
	w, instA, instB := testenv.Word(t), "Institute "+testenv.Word(t), "College "+testenv.Word(t)
	nlp, eye := "NLP "+testenv.Word(t), "Eye "+testenv.Word(t)
	a1 := e.add(person{name: w + " A1", institution: instA, interests: []string{nlp, eye}})
	a2 := e.add(person{name: w + " A2", institution: instA, interests: []string{nlp}})
	b1 := e.add(person{name: w + " B1", institution: instB, interests: []string{nlp, eye}})
	e.add(person{name: w + " B2", institution: instB + " Annex"}) // contains, but is not, instB

	inst := func(s string) func(*domain.PeopleQuery) {
		return func(q *domain.PeopleQuery) { q.Institution = &s }
	}
	interests := func(names ...string) func(*domain.PeopleQuery) {
		return func(q *domain.PeopleQuery) { q.Interests = names }
	}
	tests := []struct {
		name string
		edit []func(*domain.PeopleQuery)
		want []string
	}{
		{"institution, other letter case", []func(*domain.PeopleQuery){inst(strings.ToUpper(instA))}, []string{a1, a2}},
		{"institution is exact", []func(*domain.PeopleQuery){inst(instB)}, []string{b1}},
		{"one interest, other letter case", []func(*domain.PeopleQuery){interests(strings.ToLower(eye))}, []string{a1, b1}},
		{"all interests", []func(*domain.PeopleQuery){interests(nlp, eye)}, []string{a1, b1}},
		{"both filters", []func(*domain.PeopleQuery){inst(instA), interests(nlp, eye)}, []string{a1}},
		{"unknown interest", []func(*domain.PeopleQuery){interests(nlp, "No "+testenv.Word(t))}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := e.search(w, tt.edit...)
			if got := ids(res); !reflect.DeepEqual(got, tt.want) || res.Total != len(tt.want) {
				t.Errorf("results = %v, total = %d; want %v", got, res.Total, tt.want)
			}
		})
	}
}

// Each facet is counted with every filter but its own; selected interests come first
// (FR-6.3).
func TestSearchPeopleFacets(t *testing.T) {
	e := newSearchEnv(t)
	w, instA, instB := testenv.Word(t), "Institute "+testenv.Word(t), "College "+testenv.Word(t)
	nlp, eye, hci := "NLP "+testenv.Word(t), "Eye "+testenv.Word(t), "HCI "+testenv.Word(t)
	e.add(person{name: w + " A1", institution: instA, interests: []string{nlp, eye, hci}})
	e.add(person{name: w + " A2", institution: strings.ToLower(instA), interests: []string{nlp}})
	e.add(person{name: w + " A3", institution: instA, interests: []string{eye, nlp}})
	e.add(person{name: w + " B1", institution: instB, interests: []string{nlp, hci}})
	e.add(person{name: w + " C1"}) // no institution, no interests

	res := e.search(w)
	wantInst := []domain.Facet{{Name: instA, Count: 3}, {Name: instB, Count: 1}}
	if !reflect.DeepEqual(res.Institutions, wantInst) {
		t.Errorf("institutions = %v, want %v", res.Institutions, wantInst)
	}
	wantInt := []domain.Facet{{Name: nlp, Count: 4}, {Name: eye, Count: 2}, {Name: hci, Count: 2}}
	if !reflect.DeepEqual(res.Interests, wantInt) {
		t.Errorf("interests = %v, want %v", res.Interests, wantInt)
	}

	// The spellings of instA count as one, shown as the most common (2 of 3).
	// With instB and NLP selected: the institution facet ignores instB but keeps NLP;
	// the interest facet applies both and lists NLP first.
	res = e.search(w, func(q *domain.PeopleQuery) {
		q.Institution = &instB
		q.Interests = []string{strings.ToLower(nlp)}
	})
	if res.Total != 1 {
		t.Errorf("total = %d, want 1", res.Total)
	}
	wantInst = []domain.Facet{{Name: instA, Count: 3}, {Name: instB, Count: 1}}
	if !reflect.DeepEqual(res.Institutions, wantInst) {
		t.Errorf("institutions = %v, want %v", res.Institutions, wantInst)
	}
	wantInt = []domain.Facet{{Name: nlp, Count: 1}, {Name: hci, Count: 1}}
	if !reflect.DeepEqual(res.Interests, wantInt) {
		t.Errorf("interests = %v, want %v", res.Interests, wantInt)
	}

	// A selected interest no result has stays listed, with 0.
	missing := "Missing " + testenv.Word(t)
	res = e.search(w, func(q *domain.PeopleQuery) { q.Interests = []string{missing} })
	if want := []domain.Facet{{Name: missing, Count: 0}}; res.Total != 0 || !reflect.DeepEqual(res.Interests, want) {
		t.Errorf("total = %d, interests = %v; want 0, %v", res.Total, res.Interests, want)
	}
}

// Facets list at most maxFacets options, most results first.
func TestSearchPeopleFacetLimit(t *testing.T) {
	e := newSearchEnv(t)
	w := testenv.Word(t)
	for i := range 22 {
		e.add(person{name: fmt.Sprintf("%s %02d", w, i), institution: fmt.Sprintf("Inst %s %02d", w, i)})
	}
	res := e.search(w)
	if len(res.Institutions) != 20 || res.Institutions[0].Count != 1 {
		t.Errorf("institutions = %d options, want 20", len(res.Institutions))
	}
}

// 21 matches make two pages of 20 and 1; a page past the end is empty but keeps the
// total (FR-6.1).
func TestSearchPeoplePaging(t *testing.T) {
	e := newSearchEnv(t)
	w := testenv.Word(t)
	var want []string
	for i := range 21 {
		want = append(want, e.add(person{name: fmt.Sprintf("%s %02d", w, i)}))
	}
	page := func(n int) func(*domain.PeopleQuery) {
		return func(q *domain.PeopleQuery) { q.Page = n }
	}
	p1, p2, p3 := e.search(w), e.search(w, page(2)), e.search(w, page(3))
	if got := ids(p1); !reflect.DeepEqual(got, want[:20]) || p1.Total != 21 {
		t.Errorf("page 1 = %d results, total %d; want the first 20 by name, 21", len(got), p1.Total)
	}
	if got := ids(p2); !reflect.DeepEqual(got, want[20:]) || p2.Total != 21 {
		t.Errorf("page 2 = %v, total %d; want %v, 21", got, p2.Total, want[20:])
	}
	if len(p3.Hits) != 0 || p3.Total != 21 {
		t.Errorf("page 3 = %d results, total %d; want 0, 21", len(p3.Hits), p3.Total)
	}
}

// A result shows up to 3 interests: those matching the text or a selected interest
// first, then by name.
func TestSearchPeopleResultInterests(t *testing.T) {
	e := newSearchEnv(t)
	w := testenv.Word(t)
	sel := "Zeta " + testenv.Word(t)
	e.add(person{name: testenv.Word(t), interests: []string{"Alpha " + testenv.Word(t), "Beta " + testenv.Word(t), "Gamma " + testenv.Word(t), "Yak " + w, sel}})

	res := e.search(w, func(q *domain.PeopleQuery) { q.Interests = []string{strings.ToLower(sel)} })
	if len(res.Hits) != 1 {
		t.Fatalf("results = %v", ids(res))
	}
	got := res.Hits[0].Interests
	if len(got) != 3 || got[0] != "Yak "+w || got[1] != sel || !strings.HasPrefix(got[2], "Alpha ") {
		t.Errorf("interests = %v, want [Yak.., Zeta.., Alpha..]", got)
	}
}

// A typo in any one word of a two-word name matches (FR-6.2): the text is compared with
// the best-matching part of the name, not the whole name.
func TestSearchPeopleTypoInOneWord(t *testing.T) {
	e := newSearchEnv(t)
	first, last := testenv.Word(t), testenv.Word(t)
	for first[2] == first[3] { // swapping two equal letters would not be a typo
		first = testenv.Word(t)
	}
	name := first + " " + last
	id := e.add(person{name: name})
	e.add(person{name: testenv.Word(t) + " " + testenv.Word(t)}) // unrelated

	tests := []struct{ name, q string }{
		{"one letter wrong in the first name", first[:4] + "x" + first[5:]},
		{"one letter wrong in the last name", last[:4] + "x" + last[5:]},
		{"two letters swapped", first[:2] + first[3:4] + first[2:3] + first[4:]},
		{"other letter case", strings.ToLower(first[:4] + "x" + first[5:])},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := e.search(tt.q)
			if !slices.Contains(ids(res), id) {
				t.Fatalf("search %q = %v, want it to include %s", tt.q, ids(res), id)
			}
			if !res.Fuzzy || res.CorrectedQuery == nil || *res.CorrectedQuery != name {
				t.Errorf("fuzzy = %v, corrected = %v; want true, %s", res.Fuzzy, res.CorrectedQuery, name)
			}
			if h := res.Hits[0]; h.FullName != name || h.MatchedOn != domain.MatchName {
				t.Errorf("first result = %q matched on %s, want %s on name", h.FullName, h.MatchedOn, name)
			}
		})
	}
}

// Text that is like no name finds nobody and is not fuzzy.
func TestSearchPeopleTypoNoMatch(t *testing.T) {
	e := newSearchEnv(t)
	e.add(person{name: testenv.Word(t) + " " + testenv.Word(t)})
	if res := e.search("Zzzzz"); res.Total != 0 || res.Fuzzy || res.CorrectedQuery != nil {
		t.Errorf("search Zzzzz = %v, fuzzy %v, corrected %v; want nothing", ids(res), res.Fuzzy, res.CorrectedQuery)
	}
}

// The most similar name comes first and is the corrected query.
func TestSearchPeopleFuzzyOrder(t *testing.T) {
	e := newSearchEnv(t)
	first, last := testenv.Word(t), testenv.Word(t)
	closer := e.add(person{name: first + " " + last})                         // one letter off
	further := e.add(person{name: first[:3] + "yy" + first[5:] + " " + last}) // two letters off
	typo := first[:4] + "x" + first[5:]

	res := e.search(typo)
	if got := ids(res); !reflect.DeepEqual(got, []string{closer, further}) {
		t.Fatalf("results = %v, want [%s %s]", got, closer, further)
	}
	if res.CorrectedQuery == nil || *res.CorrectedQuery != first+" "+last {
		t.Errorf("corrected = %v, want %q", res.CorrectedQuery, first+" "+last)
	}
}
