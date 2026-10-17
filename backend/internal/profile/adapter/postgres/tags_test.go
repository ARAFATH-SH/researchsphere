//go:build integration

package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

var kinds = []domain.TagKind{domain.KindInterest, domain.KindSkill}

// token returns lower-case text no other test run uses, and deletes every tag of kind
// whose name contains it when the test ends. Users are cleaned up by newUser.
func token(t *testing.T, pool *pgxpool.Pool, kind domain.TagKind) string {
	t.Helper()
	tok := "zq" + strings.ToLower(rand.Text()[:10])
	t.Cleanup(func() {
		q := "DELETE FROM research_interests WHERE strpos(lower(name::text), $1) > 0"
		if kind == domain.KindSkill {
			q = "DELETE FROM skills WHERE strpos(lower(name::text), $1) > 0"
		}
		if _, err := pool.Exec(context.Background(), q, tok); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	return tok
}

// addTag runs the steps the usecase runs to add a tag, in one transaction, with limit.
func addTag(ctx context.Context, s *Store, userID string, kind domain.TagKind, name string, limit int) (domain.Tag, bool, error) {
	return addTagPausing(ctx, s, userID, kind, name, limit, 0)
}

// addTagPausing is addTag with a pause between counting and linking, which gives
// concurrent transactions time to count too if nothing makes them wait.
func addTagPausing(ctx context.Context, s *Store, userID string, kind domain.TagKind, name string, limit int, pause time.Duration) (domain.Tag, bool, error) {
	var (
		tag     domain.Tag
		created bool
	)
	err := s.RunInTx(ctx, func(tx domain.Tx) error {
		if err := tx.LockUser(ctx, userID); err != nil {
			return err
		}
		t, err := tx.FindOrCreateTag(ctx, kind, name)
		if err != nil {
			return err
		}
		tag = t
		has, err := tx.HasTag(ctx, userID, kind, t.ID)
		if err != nil || has {
			return err
		}
		n, err := tx.CountTags(ctx, userID, kind)
		if err != nil {
			return err
		}
		if n >= limit {
			return domain.ErrTagLimitReached
		}
		time.Sleep(pause)
		created = true
		return tx.LinkTag(ctx, userID, kind, t.ID)
	})
	return tag, created, err
}

func findOrCreate(ctx context.Context, s *Store, kind domain.TagKind, name string) (domain.Tag, error) {
	var tag domain.Tag
	err := s.RunInTx(ctx, func(tx domain.Tx) error {
		var err error
		tag, err = tx.FindOrCreateTag(ctx, kind, name)
		return err
	})
	return tag, err
}

func countTags(ctx context.Context, s *Store, userID string, kind domain.TagKind) (int, error) {
	var n int
	err := s.RunInTx(ctx, func(tx domain.Tx) error {
		var err error
		n, err = tx.CountTags(ctx, userID, kind)
		return err
	})
	return n, err
}

func tagRows(t *testing.T, pool *pgxpool.Pool, kind domain.TagKind, tok string) int {
	t.Helper()
	q := "SELECT count(*) FROM research_interests WHERE strpos(lower(name::text), $1) > 0"
	if kind == domain.KindSkill {
		q = "SELECT count(*) FROM skills WHERE strpos(lower(name::text), $1) > 0"
	}
	var n int
	if err := pool.QueryRow(t.Context(), q, tok).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// FR-4.2: the insert-on-conflict-then-select reuses a tag whose name differs only in
// letter case, and keeps the first spelling.
func TestFindOrCreateTagIgnoresCase(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			s, pool := newTestStore(t)
			tok := token(t, pool, kind)

			first, err := findOrCreate(t.Context(), s, kind, "Machine Learning "+tok)
			if err != nil {
				t.Fatal(err)
			}
			again, err := findOrCreate(t.Context(), s, kind, "MACHINE learning "+strings.ToUpper(tok))
			if err != nil {
				t.Fatal(err)
			}
			if again != first || first.Name != "Machine Learning "+tok {
				t.Errorf("got %+v then %+v; want the same tag with the first spelling", first, again)
			}
			if n := tagRows(t, pool, kind, tok); n != 1 {
				t.Errorf("%d rows, want 1", n)
			}
		})
	}
}

// Many transactions creating the same new tag at once, in different letter cases, all
// get the one row; none fails on the unique constraint.
func TestFindOrCreateTagConcurrent(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			s, pool := newTestStore(t)
			tok := token(t, pool, kind)

			const n = 8
			got := make([]domain.Tag, n)
			errs := make([]error, n)
			var wg sync.WaitGroup
			for i := range n {
				wg.Go(func() {
					name := "Robotics " + tok
					if i%2 == 1 {
						name = strings.ToUpper(name)
					}
					got[i], errs[i] = findOrCreate(context.Background(), s, kind, name)
				})
			}
			wg.Wait()
			for i := range n {
				if errs[i] != nil {
					t.Fatalf("transaction %d: %v", i, errs[i])
				}
				if got[i].ID != got[0].ID {
					t.Errorf("transaction %d got %+v, want %+v", i, got[i], got[0])
				}
			}
			if rows := tagRows(t, pool, kind, tok); rows != 1 {
				t.Errorf("%d rows, want 1", rows)
			}
		})
	}
}

// The user-row lock makes concurrent adds by one user respect the limit.
func TestAddTagLimitUnderConcurrency(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			s, pool := newTestStore(t)
			tok := token(t, pool, kind)
			user := newUser(t, pool)
			const limit = 3
			for i := range limit - 1 {
				if _, _, err := addTag(t.Context(), s, user, kind, fmt.Sprintf("Seed %d %s", i, tok), limit); err != nil {
					t.Fatal(err)
				}
			}

			const n = 6
			errs := make([]error, n)
			var wg sync.WaitGroup
			for i := range n {
				wg.Go(func() {
					_, _, errs[i] = addTagPausing(context.Background(), s, user, kind, fmt.Sprintf("Race %d %s", i, tok), limit, 50*time.Millisecond)
				})
			}
			wg.Wait()
			var ok, refused int
			for _, err := range errs {
				switch {
				case err == nil:
					ok++
				case errors.Is(err, domain.ErrTagLimitReached):
					refused++
				default:
					t.Fatalf("unexpected error: %v", err)
				}
			}
			count, err := countTags(t.Context(), s, user, kind)
			if err != nil {
				t.Fatal(err)
			}
			if ok != 1 || refused != n-1 || count != limit {
				t.Errorf("%d added, %d refused, user has %d; want 1, %d, %d", ok, refused, count, n-1, limit)
			}
			// The refused transactions rolled back the tags they created.
			if rows := tagRows(t, pool, kind, tok); rows != limit {
				t.Errorf("%d tag rows, want %d", rows, limit)
			}
		})
	}
}

func TestLockUserUnknown(t *testing.T) {
	s, _ := newTestStore(t)
	for _, id := range []string{"00000000-0000-0000-0000-0000000000ff", "not-a-uuid"} {
		err := s.RunInTx(t.Context(), func(tx domain.Tx) error { return tx.LockUser(t.Context(), id) })
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("LockUser(%q) err = %v, want ErrNotFound", id, err)
		}
	}
}

// Linking, listing in name order, and removing; a removed tag stays for others.
func TestListAndRemoveTags(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			s, pool := newTestStore(t)
			tok := token(t, pool, kind)
			user := newUser(t, pool)
			var added []domain.Tag
			for _, name := range []string{"beta " + tok, "Alpha " + tok, "GAMMA " + tok} {
				tag, created, err := addTag(t.Context(), s, user, kind, name, 30)
				if err != nil || !created {
					t.Fatalf("add %q: created %v, err %v", name, created, err)
				}
				added = append(added, tag)
			}
			if _, created, err := addTag(t.Context(), s, user, kind, "ALPHA "+tok, 30); err != nil || created {
				t.Errorf("adding a tag the user has: created %v, err %v; want a no-op", created, err)
			}

			got, err := s.ListTags(t.Context(), user, kind)
			if err != nil {
				t.Fatal(err)
			}
			want := []domain.Tag{added[1], added[0], added[2]} // Alpha, beta, GAMMA
			if !reflect.DeepEqual(got, want) {
				t.Errorf("ListTags = %+v, want %+v", got, want)
			}

			if err := s.RemoveTag(t.Context(), user, kind, added[0].ID); err != nil {
				t.Fatal(err)
			}
			if got, _ := s.ListTags(t.Context(), user, kind); len(got) != 2 {
				t.Errorf("after remove, user has %+v", got)
			}
			if rows := tagRows(t, pool, kind, tok); rows != 3 {
				t.Errorf("%d tag rows, want 3: an unused tag stays", rows)
			}
			for _, id := range []string{added[0].ID, "00000000-0000-0000-0000-0000000000ff", "not-a-uuid"} {
				if err := s.RemoveTag(t.Context(), user, kind, id); !errors.Is(err, domain.ErrNotFound) {
					t.Errorf("RemoveTag(%q) err = %v, want ErrNotFound", id, err)
				}
			}
		})
	}
}

// FR-4.3: names that start with q first, then names that contain it; within each, the
// most used first, then by name ignoring case.
func TestSuggestTagsOrdering(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			s, pool := newTestStore(t)
			tok := token(t, pool, kind)
			users := []string{newUser(t, pool), newUser(t, pool), newUser(t, pool)}

			// name -> how many users have it
			tags := []struct {
				name  string
				users int
			}{
				{tok + " alpha", 1},                   // prefix, 1 user
				{tok + " beta", 2},                    // prefix, 2 users
				{"Applied " + tok, 3},                 // contains, 3 users
				{"Basic " + tok, 0},                   // contains, unused
				{strings.ToUpper(tok) + " gamma", 0},  // prefix in other case, unused
				{tok + " aardvark", 0},                // prefix, unused, sorts before gamma
				{"unrelated " + tok[:4] + " name", 0}, // does not contain the token
			}
			for _, tg := range tags {
				created, err := findOrCreate(t.Context(), s, kind, tg.name)
				if err != nil {
					t.Fatal(err)
				}
				for _, u := range users[:tg.users] {
					if _, _, err := addTag(t.Context(), s, u, kind, created.Name, 30); err != nil {
						t.Fatal(err)
					}
				}
			}
			t.Cleanup(func() {
				q := "DELETE FROM research_interests WHERE name = $1"
				if kind == domain.KindSkill {
					q = "DELETE FROM skills WHERE name = $1"
				}
				_, _ = pool.Exec(context.Background(), q, "unrelated "+tok[:4]+" name")
			})

			names := func(ts []domain.Tag) []string {
				out := []string{}
				for _, tg := range ts {
					out = append(out, tg.Name)
				}
				return out
			}
			wantAll := []string{
				tok + " beta", tok + " alpha", tok + " aardvark", strings.ToUpper(tok) + " gamma",
				"Applied " + tok, "Basic " + tok,
			}
			tests := []struct {
				q     string
				limit int
				want  []string
			}{
				{tok, 20, wantAll},
				{strings.ToUpper(tok), 20, wantAll},
				{tok, 2, wantAll[:2]},
				{tok + " al", 20, []string{tok + " alpha"}},
				{tok + "%", 20, []string{}},               // % is a literal character, not a wildcard
				{tok[:3] + "_" + tok[4:], 20, []string{}}, // so is _
			}
			for _, tt := range tests {
				got, err := s.SuggestTags(t.Context(), kind, tt.q, tt.limit)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(names(got), tt.want) {
					t.Errorf("SuggestTags(%q, %d) = %v\nwant %v", tt.q, tt.limit, names(got), tt.want)
				}
			}
		})
	}
}
