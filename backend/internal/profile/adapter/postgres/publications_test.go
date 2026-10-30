//go:build integration

package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

func yr(n int) *int { return &n }

// testDOI returns a DOI no other test run uses.
func testDOI() string { return "10.5555/it-" + strings.ToLower(rand.Text()) }

// addPublication runs the steps the usecase runs to add p for userID: look up the DOI,
// insert when it is new, link to the existing record when the insert finds it taken.
// It returns the record and whether it existed. wait, when not nil, runs between the
// lookup and the insert.
func addPublication(ctx context.Context, s *Store, userID string, p domain.Publication, wait func()) (domain.Publication, bool, error) {
	var saved domain.Publication
	var linked bool
	err := s.RunInTx(ctx, func(tx domain.Tx) error {
		saved, linked = domain.Publication{}, false
		if p.DOI != nil {
			existing, err := tx.FindPublicationByDOI(ctx, *p.DOI)
			if err == nil {
				saved, linked = existing, true
				return tx.LinkPublication(ctx, userID, existing.ID)
			}
			if !errors.Is(err, domain.ErrNotFound) {
				return err
			}
		}
		if wait != nil {
			wait()
		}
		var err error
		saved, err = tx.InsertPublication(ctx, p)
		if errors.Is(err, domain.ErrDuplicateDOI) {
			if saved, err = tx.FindPublicationByDOI(ctx, *p.DOI); err != nil {
				return err
			}
			linked = true
		} else if err != nil {
			return err
		}
		return tx.LinkPublication(ctx, userID, saved.ID)
	})
	return saved, linked, err
}

// A manual publication is stored with its fields, linked confirmed, listed and counted;
// an unconfirmed link is not (FR-5.1, FR-5.5).
func TestInsertLinkListPublications(t *testing.T) {
	s, pool := newTestStore(t)
	ada := newUser(t, pool)

	saved, linked, err := addPublication(t.Context(), s, ada, domain.Publication{
		Title: "Notes", Year: yr(2024), Venue: ptr("Journal"), Abstract: ptr("Line one\nline two"),
		URL: ptr("https://example.org/p"), Source: domain.SourceManual, CreatedBy: &ada,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if linked || saved.ID == "" || saved.Title != "Notes" || *saved.Year != 2024 || *saved.Venue != "Journal" ||
		*saved.Abstract != "Line one\nline two" || *saved.URL != "https://example.org/p" || saved.DOI != nil ||
		saved.Source != domain.SourceManual || *saved.CreatedBy != ada || saved.CreatedAt.IsZero() {
		t.Fatalf("stored %+v, linked %v", saved, linked)
	}

	// An imported record the user has not confirmed yet, without a year.
	var hidden string
	err = pool.QueryRow(t.Context(),
		"INSERT INTO publications (title, source, created_by) VALUES ('Imported', 'orcid', $1) RETURNING id", ada).Scan(&hidden)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "INSERT INTO user_publications (user_id, publication_id) VALUES ($1, $2)", ada, hidden); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListConfirmedPublications(t.Context(), ada)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != saved.ID || list[0].AddedAt.IsZero() {
		t.Fatalf("listed %+v, want only the confirmed record with AddedAt set", list)
	}
	if n, err := s.CountConfirmedPublications(t.Context(), ada); err != nil || n != 1 {
		t.Errorf("confirmed count = %d, %v; want 1", n, err)
	}
	err = s.RunInTx(t.Context(), func(tx domain.Tx) error {
		n, err := tx.CountPublicationLinks(t.Context(), ada)
		if err != nil {
			return err
		}
		if n != 2 {
			t.Errorf("link count = %d, want 2 (confirmed or not)", n)
		}
		state, err := tx.PublicationLink(t.Context(), ada, hidden)
		if state != domain.Unconfirmed {
			t.Errorf("imported link = %v, want unconfirmed", state)
		}
		if err != nil {
			return err
		}
		if err := tx.ConfirmPublication(t.Context(), ada, hidden); err != nil {
			return err
		}
		state, err = tx.PublicationLink(t.Context(), ada, hidden)
		if state != domain.Confirmed {
			t.Errorf("after confirming, link = %v, want confirmed", state)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Two users adding the same DOI share one record (FR-5.4).
func TestSameDOISharesOneRecord(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool), newUser(t, pool)
	doi := testDOI()

	first, linked, err := addPublication(t.Context(), s, ada, domain.Publication{
		DOI: &doi, Title: "Shared", Year: yr(2020), Source: domain.SourceManual, CreatedBy: &ada,
	}, nil)
	if err != nil || linked {
		t.Fatalf("first add: %v, linked %v", err, linked)
	}
	second, linked, err := addPublication(t.Context(), s, bob, domain.Publication{
		DOI: &doi, Title: "Bob's title", Year: yr(2021), Source: domain.SourceManual, CreatedBy: &bob,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !linked || second.ID != first.ID || second.Title != "Shared" {
		t.Errorf("second add got %+v, linked %v; want the first record", second, linked)
	}
	err = s.RunInTx(t.Context(), func(tx domain.Tx) error {
		_, err := tx.InsertPublication(t.Context(), domain.Publication{DOI: &doi, Title: "Again", Source: domain.SourceManual})
		if !errors.Is(err, domain.ErrDuplicateDOI) {
			t.Errorf("insert of a taken DOI: err = %v, want ErrDuplicateDOI", err)
		}
		n, err := tx.CountPublicationAuthors(t.Context(), first.ID)
		if n != 2 {
			t.Errorf("authors = %d, want 2", n)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Two users adding the same new DOI at the same moment end with one record and two links:
// the second insert waits for the first to commit, inserts nothing and links to it.
func TestConcurrentAddOfSameDOI(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool), newUser(t, pool)
	doi := testDOI()

	var looked sync.WaitGroup
	looked.Add(2)
	wait := func() { looked.Done(); looked.Wait() } // both have missed the DOI before either inserts
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	for _, u := range []string{ada, bob} {
		go func() {
			_, linked, err := addPublication(context.Background(), s, u, domain.Publication{
				DOI: &doi, Title: "Race", Year: yr(2020), Source: domain.SourceManual, CreatedBy: &u,
			}, wait)
			results <- linked
			errs <- err
		}()
	}
	linkedCount := 0
	for range 2 {
		if <-results {
			linkedCount++
		}
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if linkedCount != 1 {
		t.Errorf("%d adds linked to an existing record, want exactly 1", linkedCount)
	}
	var records, links int
	err := pool.QueryRow(t.Context(),
		`SELECT count(DISTINCT p.id), count(up.user_id)
		 FROM publications p JOIN user_publications up ON up.publication_id = p.id WHERE p.doi = $1`, doi).Scan(&records, &links)
	if err != nil {
		t.Fatal(err)
	}
	if records != 1 || links != 2 {
		t.Errorf("records, links = %d, %d; want 1, 2", records, links)
	}
}

// removePublication runs the usecase's steps: lock, unlink, delete when orphaned.
func removePublication(ctx context.Context, s *Store, userID, id string) error {
	return s.RunInTx(ctx, func(tx domain.Tx) error {
		p, err := tx.FindPublicationForUpdate(ctx, userID, id)
		if err != nil {
			return err
		}
		if err := tx.UnlinkPublication(ctx, userID, p.ID); err != nil {
			return err
		}
		return tx.DeletePublicationIfOrphaned(ctx, p.ID)
	})
}

// Removing a publication removes the user's link, and the record once no author is left
// (FR-5.7). A user who is not linked finds nothing.
func TestRemovePublication(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool), newUser(t, pool)
	doi := testDOI()

	shared, _, err := addPublication(t.Context(), s, ada, domain.Publication{
		DOI: &doi, Title: "Shared", Year: yr(2020), Source: domain.SourceManual, CreatedBy: &ada}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := addPublication(t.Context(), s, bob, domain.Publication{DOI: &doi, Title: "x", Source: domain.SourceManual}, nil); err != nil {
		t.Fatal(err)
	}
	solo, _, err := addPublication(t.Context(), s, ada, domain.Publication{
		Title: "Solo", Year: yr(2021), Source: domain.SourceManual, CreatedBy: &ada}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := removePublication(t.Context(), s, bob, solo.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Bob removing Ada's record: err = %v, want ErrNotFound", err)
	}
	if err := removePublication(t.Context(), s, ada, "not-a-uuid"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("bad ID: err = %v, want ErrNotFound", err)
	}
	for _, id := range []string{shared.ID, solo.ID} {
		if err := removePublication(t.Context(), s, ada, id); err != nil {
			t.Fatal(err)
		}
	}
	var sharedLeft, soloLeft bool
	err = pool.QueryRow(t.Context(),
		"SELECT EXISTS (SELECT 1 FROM publications WHERE id = $1), EXISTS (SELECT 1 FROM publications WHERE id = $2)",
		shared.ID, solo.ID).Scan(&sharedLeft, &soloLeft)
	if err != nil {
		t.Fatal(err)
	}
	if !sharedLeft || soloLeft {
		t.Errorf("shared left %v, solo left %v; want the shared one kept and the solo one deleted", sharedLeft, soloLeft)
	}
	if list, _ := s.ListConfirmedPublications(t.Context(), bob); len(list) != 1 {
		t.Errorf("Bob lists %d, want his shared record still there", len(list))
	}
}

// Changing a publication's DOI to one another record has is refused (FR-5.4).
func TestUpdatePublicationToTakenDOI(t *testing.T) {
	s, pool := newTestStore(t)
	ada := newUser(t, pool)
	taken := testDOI()

	if _, _, err := addPublication(t.Context(), s, ada, domain.Publication{
		DOI: &taken, Title: "Has it", Year: yr(2020), Source: domain.SourceManual, CreatedBy: &ada}, nil); err != nil {
		t.Fatal(err)
	}
	other, _, err := addPublication(t.Context(), s, ada, domain.Publication{
		Title: "Wants it", Year: yr(2020), Source: domain.SourceManual, CreatedBy: &ada}, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = s.RunInTx(t.Context(), func(tx domain.Tx) error {
		p, err := tx.FindPublicationForUpdate(t.Context(), ada, other.ID)
		if err != nil {
			return err
		}
		p.DOI = &taken
		_, err = tx.UpdatePublication(t.Context(), p)
		return err
	})
	if !errors.Is(err, domain.ErrDuplicateDOI) {
		t.Fatalf("err = %v, want ErrDuplicateDOI", err)
	}

	// A plain edit reads back as stored.
	var got domain.Publication
	err = s.RunInTx(t.Context(), func(tx domain.Tx) error {
		p, err := tx.FindPublicationForUpdate(t.Context(), ada, other.ID)
		if err != nil {
			return err
		}
		p.Title, p.Venue, p.Year = "Renamed", ptr("Venue"), yr(2019)
		got, err = tx.UpdatePublication(t.Context(), p)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Renamed" || *got.Venue != "Venue" || *got.Year != 2019 || got.DOI != nil {
		t.Errorf("updated %+v", got)
	}
}
