//go:build integration

package postgres

import (
	"slices"
	"testing"
)

// connect makes a and b connected.
func connect(t *testing.T, s *Store, a, b string) {
	t.Helper()
	r := create(t, s, a, b)
	if _, _, err := s.AcceptRequest(t.Context(), r.ID, b); err != nil {
		t.Fatal(err)
	}
}

func searchNames(t *testing.T, s *Store, userID, q string, limit, offset int) ([]string, int) {
	t.Helper()
	items, total, err := s.SearchConnections(t.Context(), userID, q, limit, offset)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(items))
	for i, c := range items {
		names[i] = c.Other.FullName
	}
	return names, total
}

// FR-16.6: the member picker finds the caller's own connections by name (exact, prefix, then
// contains, newest first within a rank), by a similar name when none contains the text (a
// typo), in Bangla, and by institution and headline; never someone else's connection, a
// person who blocked the caller, or a suspended one.
func TestSearchConnections(t *testing.T) {
	s, pool := newTestStore(t)
	me := newUser(t, pool, "Zinain Afrin")
	nafisa := newUser(t, pool, "Nafisa Hasan")
	nafi := newUser(t, pool, "Nafi")
	anafi := newUser(t, pool, "Ranafi Karim")
	bangla := newUser(t, pool, "রহিম উদ্দিন")
	chemist := newUser(t, pool, "Tariq Aziz")
	exec(t, pool, "UPDATE profiles SET headline = 'Polymer chemist', institution = 'Dhaka University' WHERE user_id = $1", chemist)
	blocker := newUser(t, pool, "Nafisa Blocker")
	suspended := newUser(t, pool, "Nafisa Suspended")
	stranger := newUser(t, pool, "Nafisa Stranger")
	friend := newUser(t, pool, "Friend")
	for _, u := range []string{nafisa, nafi, anafi, bangla, chemist, blocker, suspended} {
		connect(t, s, me, u)
	}
	connect(t, s, friend, stranger) // someone else's connection
	exec(t, pool, "INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)", blocker, me)
	exec(t, pool, "UPDATE users SET suspended_at = now() WHERE id = $1", suspended)

	for _, tc := range []struct {
		q    string
		want []string
	}{
		// The exact name, then one starting with the text, then one containing it.
		{"nafi", []string{"Nafi", "Nafisa Hasan", "Ranafi Karim"}},
		{"Hasn", []string{"Nafisa Hasan"}},  // a typo: no name contains it
		{"রহিম", []string{"রহিম উদ্দিন"}},   // Bangla, literal
		{"dhaka", []string{"Tariq Aziz"}},   // institution
		{"polymer", []string{"Tariq Aziz"}}, // headline
		{"stranger", nil},                   // not a connection
		{"suspended", nil},                  // hidden
		{"blocker", nil},                    // blocked the caller
		{"100%", nil},                       // % is a character, not a wildcard
	} {
		got, total := searchNames(t, s, me, tc.q, 20, 0)
		if !slices.Equal(got, tc.want) || total != len(tc.want) {
			t.Errorf("%q: %v (total %d), want %v", tc.q, got, total, tc.want)
		}
	}

	// Paging keeps the total; past the end it is still known.
	page1, total := searchNames(t, s, me, "nafi", 2, 0)
	page2, total2 := searchNames(t, s, me, "nafi", 2, 2)
	past, total3 := searchNames(t, s, me, "nafi", 2, 4)
	if len(page1) != 2 || len(page2) != 1 || len(past) != 0 || total != 3 || total2 != 3 || total3 != 3 {
		t.Errorf("pages %v %v %v, totals %d %d %d", page1, page2, past, total, total2, total3)
	}
	if got, total := searchNames(t, s, "not-a-uuid", "nafi", 20, 0); len(got) != 0 || total != 0 {
		t.Errorf("not a UUID: %v %d", got, total)
	}
}
