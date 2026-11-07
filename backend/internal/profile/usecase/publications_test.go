package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// newPublicationService returns a Service over store whose clock reads 15 June 2026 UTC,
// so 2027 is the latest year allowed.
func newPublicationService(store *fakeStore) *Service {
	s := New(store)
	s.now = func() time.Time { return time.Date(2026, time.June, 15, 12, 0, 0, 0, time.UTC) }
	return s
}

// validPublication is a complete manual entry without a DOI.
func validPublication() PublicationInput {
	return PublicationInput{
		Title:    "Notes on the Analytical Engine",
		Year:     iptr(2024),
		Venue:    ptr("Journal of Engines"),
		Abstract: ptr("We describe the engine.\nIn two lines."),
		URL:      ptr("https://example.org/paper"),
	}
}

func TestAddPublicationValidation(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*PublicationInput)
		want   error
	}{
		{"valid", func(*PublicationInput) {}, nil},
		{"empty title", func(in *PublicationInput) { in.Title = "  " }, domain.ErrInvalidTitle},
		{"title of 500 characters", func(in *PublicationInput) { in.Title = strings.Repeat("é", 500) }, nil},
		{"title of 501 characters", func(in *PublicationInput) { in.Title = strings.Repeat("é", 501) }, domain.ErrInvalidTitle},
		{"title with a line break", func(in *PublicationInput) { in.Title = "A\nB" }, domain.ErrInvalidTitle},
		{"missing year", func(in *PublicationInput) { in.Year = nil }, domain.ErrInvalidYear},
		{"year 1899", func(in *PublicationInput) { in.Year = iptr(1899) }, domain.ErrInvalidYear},
		{"year 1900", func(in *PublicationInput) { in.Year = iptr(1900) }, nil},
		{"next year", func(in *PublicationInput) { in.Year = iptr(2027) }, nil},
		{"two years ahead", func(in *PublicationInput) { in.Year = iptr(2028) }, domain.ErrInvalidYear},
		{"venue of 301 characters", func(in *PublicationInput) { in.Venue = ptr(strings.Repeat("v", 301)) }, domain.ErrInvalidVenue},
		{"venue with a line break", func(in *PublicationInput) { in.Venue = ptr("A\r\nB") }, domain.ErrInvalidVenue},
		{"abstract of 5000 characters", func(in *PublicationInput) { in.Abstract = ptr(strings.Repeat("a", 5000)) }, nil},
		{"abstract of 5001 characters", func(in *PublicationInput) { in.Abstract = ptr(strings.Repeat("a", 5001)) }, domain.ErrInvalidAbstract},
		{"http url", func(in *PublicationInput) { in.URL = ptr("http://example.org/p") }, nil},
		{"upper-case scheme", func(in *PublicationInput) { in.URL = ptr("HTTPS://example.org/p") }, nil},
		{"ftp url", func(in *PublicationInput) { in.URL = ptr("ftp://example.org/p") }, domain.ErrInvalidPublicationURL},
		{"url without a host", func(in *PublicationInput) { in.URL = ptr("https://") }, domain.ErrInvalidPublicationURL},
		{"url with a space", func(in *PublicationInput) { in.URL = ptr("https://example.org/a b") }, domain.ErrInvalidPublicationURL},
		{"url of 501 characters", func(in *PublicationInput) {
			in.URL = ptr("https://example.org/" + strings.Repeat("p", 481))
		}, domain.ErrInvalidPublicationURL},
		{"bad doi", func(in *PublicationInput) { in.DOI = ptr("11.1000/x") }, domain.ErrInvalidDOI},
		// The first invalid field wins, in the contract's order.
		{"title before year", func(in *PublicationInput) { in.Title = ""; in.Year = nil }, domain.ErrInvalidTitle},
		{"year before venue", func(in *PublicationInput) { in.Year = nil; in.Venue = ptr("A\nB") }, domain.ErrInvalidYear},
		{"venue before abstract", func(in *PublicationInput) {
			in.Venue = ptr("A\nB")
			in.Abstract = ptr(strings.Repeat("a", 5001))
		}, domain.ErrInvalidVenue},
		{"abstract before url", func(in *PublicationInput) {
			in.Abstract = ptr(strings.Repeat("a", 5001))
			in.URL = ptr("ftp://x")
		}, domain.ErrInvalidAbstract},
		{"url before doi", func(in *PublicationInput) { in.URL = ptr("ftp://x"); in.DOI = ptr("nope") }, domain.ErrInvalidPublicationURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore(testAccounts()...)
			in := validPublication()
			tt.modify(&in)
			_, err := newPublicationService(store).AddPublication(context.Background(), adaID, in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if wantStored := tt.want == nil; (len(store.publications) == 1) != wantStored {
				t.Errorf("%d publications stored, want stored = %v", len(store.publications), wantStored)
			}
		})
	}
}

func TestAddPublicationStoresTrimmedFieldsAndConfirmedLink(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	in := PublicationInput{
		Title:    "  Notes  ",
		Year:     iptr(2020),
		Venue:    ptr("   "),
		Abstract: ptr(""),
		URL:      ptr(" \t"),
		DOI:      ptr("  "),
	}
	res, err := newPublicationService(store).AddPublication(context.Background(), adaID, in)
	if err != nil {
		t.Fatal(err)
	}
	p := res.Publication
	if p.Title != "Notes" || *p.Year != 2020 || p.Venue != nil || p.Abstract != nil || p.URL != nil || p.DOI != nil {
		t.Errorf("stored %+v, want the title trimmed and blank optional fields nil", p)
	}
	if res.Linked {
		t.Error("Linked = true for a new record")
	}
	if p.Source != domain.SourceManual || p.CreatedBy == nil || *p.CreatedBy != adaID {
		t.Errorf("source, created by = %q, %v; want manual, Ada", p.Source, p.CreatedBy)
	}
	if state, _ := store.PublicationLink(context.Background(), adaID, p.ID); state != domain.Confirmed {
		t.Errorf("Ada's link is %v, want confirmed", state)
	}
	if store.lockedUser != adaID {
		t.Errorf("locked %q, want Ada locked before the limit check", store.lockedUser)
	}
}

func TestNormalizeDOI(t *testing.T) {
	tests := []struct {
		in      string
		want    string // "" means nil
		wantErr bool
	}{
		{"10.1145/3292500.3330701", "10.1145/3292500.3330701", false},
		{"  10.1145/ABC.Def  ", "10.1145/abc.def", false},
		{"https://doi.org/10.1000/XYZ", "10.1000/xyz", false},
		{"HTTPS://DOI.ORG/10.1000/xyz", "10.1000/xyz", false},
		{"http://doi.org/10.1000/xyz", "10.1000/xyz", false},
		{"https://dx.doi.org/10.1000/xyz", "10.1000/xyz", false},
		{"http://dx.doi.org/10.1000/xyz", "10.1000/xyz", false},
		{"doi:10.1000/xyz", "10.1000/xyz", false},
		{"DOI: 10.1000/xyz", "10.1000/xyz", false},
		{"10.123456789/x", "10.123456789/x", false},
		{"", "", false},
		{"   ", "", false},
		{"10.123/x", "", true},          // registrant code too short
		{"10.1234567890/x", "", true},   // registrant code too long
		{"10.1000/", "", true},          // no suffix
		{"10.1000/a b", "", true},       // whitespace in the suffix
		{"11.1000/x", "", true},         // not the 10. directory
		{"doi.org/10.1000/x", "", true}, // a link without a scheme is not a known prefix
		{"https://example.org/10.1000/x", "", true},
		{"doi:doi:10.1000/x", "", true}, // only one prefix is removed
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := NormalizeDOI(&tt.in)
			if tt.wantErr {
				if !errors.Is(err, domain.ErrInvalidDOI) {
					t.Fatalf("err = %v, want ErrInvalidDOI", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tt.want == "" && got != nil:
				t.Errorf("got %q, want nil", *got)
			case tt.want != "" && (got == nil || *got != tt.want):
				t.Errorf("got %v, want %q", got, tt.want)
			}
		})
	}
	if got, err := NormalizeDOI(nil); got != nil || err != nil {
		t.Errorf("NormalizeDOI(nil) = %v, %v; want nil, nil", got, err)
	}
}

func TestAddPublicationWithSameDOILinksToExistingRecord(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	svc := newPublicationService(store)
	first := validPublication()
	first.DOI = ptr("10.1000/Engine")
	a, err := svc.AddPublication(context.Background(), adaID, first)
	if err != nil {
		t.Fatal(err)
	}
	second := PublicationInput{Title: "Another title", Year: iptr(2001), DOI: ptr("https://doi.org/10.1000/ENGINE")}
	b, err := svc.AddPublication(context.Background(), bobID, second)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Linked || b.Publication.ID != a.Publication.ID {
		t.Fatalf("Bob got %+v, want Ada's record with Linked true", b)
	}
	if b.Publication.Title != first.Title {
		t.Errorf("title = %q, want the existing record's %q, not Bob's", b.Publication.Title, first.Title)
	}
	if len(store.publications) != 1 {
		t.Errorf("%d records, want one per DOI", len(store.publications))
	}
	if state, _ := store.PublicationLink(context.Background(), bobID, a.Publication.ID); state != domain.Confirmed {
		t.Errorf("Bob's link is %v, want confirmed", state)
	}
}

func TestAddPublicationLinksWhenAnotherUserStoredTheDOIFirst(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	id := store.givePublication(domain.Publication{DOI: ptr("10.1000/race"), Title: "Race", Year: iptr(2020)}, true, adaID)
	store.hideDOIOnce = true // the lookup misses it, and the insert then finds the DOI taken
	in := validPublication()
	in.DOI = ptr("10.1000/race")
	res, err := newPublicationService(store).AddPublication(context.Background(), bobID, in)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Linked || res.Publication.ID != id || len(store.publications) != 1 {
		t.Errorf("got %+v with %d records, want Bob linked to %s", res, len(store.publications), id)
	}
}

func TestAddPublicationAlreadyAddedDOI(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	store.givePublication(domain.Publication{DOI: ptr("10.1000/mine"), Title: "Mine", Year: iptr(2020)}, true, adaID)
	in := validPublication()
	in.DOI = ptr("doi:10.1000/MINE")
	_, err := newPublicationService(store).AddPublication(context.Background(), adaID, in)
	if !errors.Is(err, domain.ErrPublicationAlreadyAdded) {
		t.Fatalf("err = %v, want ErrPublicationAlreadyAdded", err)
	}
}

func TestAddPublicationConfirmsAnUnconfirmedLink(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	id := store.givePublication(domain.Publication{DOI: ptr("10.1000/orcid"), Title: "Imported", Year: iptr(2020),
		Source: domain.SourceORCID}, false, adaID)
	in := validPublication()
	in.DOI = ptr("10.1000/orcid")
	res, err := newPublicationService(store).AddPublication(context.Background(), adaID, in)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Linked || res.Publication.ID != id {
		t.Errorf("got %+v, want the existing record linked", res)
	}
	if state, _ := store.PublicationLink(context.Background(), adaID, id); state != domain.Confirmed {
		t.Errorf("link is %v, want confirmed", state)
	}
}

func TestAddPublicationLimit(t *testing.T) {
	tests := []struct {
		name  string
		have  int
		doi   bool // link to an existing record instead of creating one
		want  error
		wantN int
	}{
		{"one below the limit", MaxPublications - 1, false, nil, MaxPublications},
		{"at the limit", MaxPublications, false, domain.ErrPublicationLimitReached, MaxPublications},
		{"at the limit, linking by DOI", MaxPublications, true, domain.ErrPublicationLimitReached, MaxPublications},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore(testAccounts()...)
			for i := range tt.have {
				// Unconfirmed links count too.
				store.givePublication(domain.Publication{Title: fmt.Sprintf("P%d", i), Year: iptr(2020)}, i%2 == 0, adaID)
			}
			in := validPublication()
			if tt.doi {
				store.givePublication(domain.Publication{DOI: ptr("10.1000/bob"), Title: "Bob's", Year: iptr(2020)}, true, bobID)
				in.DOI = ptr("10.1000/bob")
			}
			_, err := newPublicationService(store).AddPublication(context.Background(), adaID, in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if n, _ := store.CountPublicationLinks(context.Background(), adaID); n != tt.wantN {
				t.Errorf("Ada has %d links, want %d", n, tt.wantN)
			}
		})
	}
}

func TestAddPublicationUnknownAccount(t *testing.T) {
	_, err := newPublicationService(newFakeStore()).AddPublication(context.Background(), adaID, validPublication())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUpdatePublication(t *testing.T) {
	type setup struct {
		source  domain.PublicationSource
		authors []string
	}
	tests := []struct {
		name  string
		setup setup
		user  string
		patch PublicationPatch
		want  error
		check func(t *testing.T, p domain.Publication)
	}{
		{
			name:  "changes the fields set",
			setup: setup{domain.SourceManual, []string{adaID}},
			user:  adaID,
			patch: PublicationPatch{
				Title: Field[string]{Set: true, Value: " New title "},
				Year:  Field[int]{Set: true, Value: 2019},
				DOI:   Field[string]{Set: true, Value: "https://doi.org/10.1000/NEW"},
			},
			check: func(t *testing.T, p domain.Publication) {
				if p.Title != "New title" || *p.Year != 2019 || *p.DOI != "10.1000/new" || *p.Venue != "Old venue" {
					t.Errorf("got %+v", p)
				}
			},
		},
		{
			name:  "null clears an optional field",
			setup: setup{domain.SourceManual, []string{adaID}},
			user:  adaID,
			patch: PublicationPatch{Venue: Field[string]{Set: true, Null: true}},
			check: func(t *testing.T, p domain.Publication) {
				if p.Venue != nil || p.Title != "Old title" {
					t.Errorf("got %+v, want only the venue cleared", p)
				}
			},
		},
		{"null title", setup{domain.SourceManual, []string{adaID}}, adaID,
			PublicationPatch{Title: Field[string]{Set: true, Null: true}}, domain.ErrInvalidTitle, nil},
		{"null year", setup{domain.SourceManual, []string{adaID}}, adaID,
			PublicationPatch{Year: Field[int]{Set: true, Null: true}}, domain.ErrInvalidYear, nil},
		{"invalid doi", setup{domain.SourceManual, []string{adaID}}, adaID,
			PublicationPatch{DOI: Field[string]{Set: true, Value: "nope"}}, domain.ErrInvalidDOI, nil},
		{"shared with another user", setup{domain.SourceManual, []string{adaID, bobID}}, adaID,
			PublicationPatch{Title: Field[string]{Set: true, Value: "Mine now"}}, domain.ErrPublicationSharedReadonly, nil},
		{"fetched for a DOI", setup{domain.SourceDOI, []string{adaID}}, adaID,
			PublicationPatch{Title: Field[string]{Set: true, Value: "x"}}, domain.ErrPublicationSharedReadonly, nil},
		{"imported from ORCID", setup{domain.SourceORCID, []string{adaID}}, adaID,
			PublicationPatch{Title: Field[string]{Set: true, Value: "x"}}, domain.ErrPublicationSharedReadonly, nil},
		// Not linked comes before validation, so an invalid patch reveals nothing.
		{"someone else's publication", setup{domain.SourceManual, []string{bobID}}, adaID,
			PublicationPatch{Title: Field[string]{Set: true, Null: true}}, domain.ErrNotFound, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore(testAccounts()...)
			id := store.givePublication(domain.Publication{Title: "Old title", Year: iptr(2020), Venue: ptr("Old venue"),
				Source: tt.setup.source}, true, tt.setup.authors...)
			got, err := newPublicationService(store).UpdatePublication(context.Background(), tt.user, id, tt.patch)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if tt.want != nil {
				if store.publications[id].Title != "Old title" {
					t.Errorf("stored title %q, want it unchanged", store.publications[id].Title)
				}
				return
			}
			if store.publications[id].Title != got.Title {
				t.Error("the returned record differs from the stored one")
			}
			tt.check(t, got)
		})
	}
}

func TestUpdatePublicationDuplicateDOI(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	store.givePublication(domain.Publication{DOI: ptr("10.1000/taken"), Title: "Bob's", Year: iptr(2020)}, true, bobID)
	id := store.givePublication(domain.Publication{Title: "Ada's", Year: iptr(2020)}, true, adaID)
	_, err := newPublicationService(store).UpdatePublication(context.Background(), adaID, id,
		PublicationPatch{DOI: Field[string]{Set: true, Value: "10.1000/TAKEN"}})
	if !errors.Is(err, domain.ErrDuplicateDOI) {
		t.Fatalf("err = %v, want ErrDuplicateDOI", err)
	}
}

func TestRemovePublication(t *testing.T) {
	t.Run("the last author's removal deletes the record", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		id := store.givePublication(domain.Publication{Title: "Solo", Year: iptr(2020)}, true, adaID)
		if err := newPublicationService(store).RemovePublication(context.Background(), adaID, id); err != nil {
			t.Fatal(err)
		}
		if _, ok := store.publications[id]; ok {
			t.Error("the orphaned record is still there")
		}
	})
	t.Run("a shared record stays for the co-author", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		id := store.givePublication(domain.Publication{DOI: ptr("10.1000/shared"), Title: "Shared", Year: iptr(2020)}, true, adaID, bobID)
		if err := newPublicationService(store).RemovePublication(context.Background(), adaID, id); err != nil {
			t.Fatal(err)
		}
		if _, ok := store.publications[id]; !ok {
			t.Fatal("the shared record was deleted")
		}
		if s, _ := store.PublicationLink(context.Background(), adaID, id); s != domain.NotLinked {
			t.Error("Ada is still linked")
		}
		if s, _ := store.PublicationLink(context.Background(), bobID, id); s != domain.Confirmed {
			t.Error("Bob lost his link")
		}
	})
	t.Run("an unconfirmed link can be removed", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		id := store.givePublication(domain.Publication{Title: "Imported", Year: iptr(2020), Source: domain.SourceORCID}, false, adaID)
		if err := newPublicationService(store).RemovePublication(context.Background(), adaID, id); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("not linked", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		id := store.givePublication(domain.Publication{Title: "Bob's", Year: iptr(2020)}, true, bobID)
		err := newPublicationService(store).RemovePublication(context.Background(), adaID, id)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
		if _, ok := store.publications[id]; !ok {
			t.Error("Bob's record was deleted")
		}
	})
	t.Run("unknown ID", func(t *testing.T) {
		err := newPublicationService(newFakeStore(testAccounts()...)).RemovePublication(context.Background(), adaID, "pub-999")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestListPublicationsOrderAndConfirmedOnly(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	// Linked in this order, a second apart.
	store.givePublication(domain.Publication{Title: "2020 first added", Year: iptr(2020)}, true, adaID)
	store.givePublication(domain.Publication{Title: "no year", Source: domain.SourceORCID}, true, adaID)
	store.givePublication(domain.Publication{Title: "2024", Year: iptr(2024)}, true, adaID)
	store.givePublication(domain.Publication{Title: "unconfirmed", Year: iptr(2025)}, false, adaID)
	store.givePublication(domain.Publication{Title: "2020 last added", Year: iptr(2020)}, true, adaID)
	store.givePublication(domain.Publication{Title: "Bob's", Year: iptr(2025)}, true, bobID)

	svc := newPublicationService(store)
	for name, list := range map[string]func() (domain.PublicationPage, error){
		"own": func() (domain.PublicationPage, error) {
			return svc.ListOwnPublications(context.Background(), adaID, 1, 10)
		},
		"viewed": func() (domain.PublicationPage, error) {
			return svc.ListPublications(context.Background(), eveID, adaID, 1, 10)
		},
	} {
		t.Run(name, func(t *testing.T) {
			page, err := list()
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, p := range page.Items {
				got = append(got, p.Title)
			}
			want := []string{"2024", "2020 last added", "2020 first added", "no year"}
			if strings.Join(got, "|") != strings.Join(want, "|") {
				t.Errorf("order = %q, want %q", got, want)
			}
			if page.Total != 4 {
				t.Errorf("total = %d, want 4 (the unconfirmed one is hidden)", page.Total)
			}
		})
	}
}

func TestListPublicationsPagination(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	for i := range 60 {
		store.givePublication(domain.Publication{Title: fmt.Sprintf("P%02d", i), Year: iptr(1960 + i)}, true, adaID)
	}
	tests := []struct {
		name              string
		page, size        int
		wantSize, wantLen int
		wantFirst         string
		wantErr           error
	}{
		{"first page", 1, 10, 10, 10, "P59", nil},
		{"second page", 2, 10, 10, 10, "P49", nil},
		{"last partial page", 3, 25, 25, 10, "P09", nil},
		{"past the end", 7, 10, 10, 0, "", nil},
		{"far past the end", 1 << 40, 50, 50, 0, "", nil},
		{"size capped at 50", 1, 500, MaxPublicationPageSize, 50, "P59", nil},
		{"page 0", 0, 10, 0, 0, "", domain.ErrInvalidPage},
		{"size 0", 1, 0, 0, 0, "", domain.ErrInvalidPage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newPublicationService(store).ListPublications(context.Background(), eveID, adaID, tt.page, tt.size)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got.Total != 60 || got.Page != tt.page || got.PageSize != tt.wantSize || len(got.Items) != tt.wantLen {
				t.Errorf("total, page, size, len = %d, %d, %d, %d; want 60, %d, %d, %d",
					got.Total, got.Page, got.PageSize, len(got.Items), tt.page, tt.wantSize, tt.wantLen)
			}
			if got.Items == nil {
				t.Error("Items is nil, want an empty list")
			}
			if tt.wantFirst != "" && got.Items[0].Title != tt.wantFirst {
				t.Errorf("first = %q, want %q", got.Items[0].Title, tt.wantFirst)
			}
		})
	}
}

func TestListPublicationsHiddenAccounts(t *testing.T) {
	for name, id := range map[string]string{"unknown": "nobody", "suspended": carlID, "unverified": dianaID} {
		t.Run(name, func(t *testing.T) {
			store := newFakeStore(testAccounts()...)
			store.givePublication(domain.Publication{Title: "Hidden", Year: iptr(2020)}, true, id)
			_, err := newPublicationService(store).ListPublications(context.Background(), eveID, id, 1, 10)
			if !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestProfileCountsConfirmedPublications(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	store.givePublication(domain.Publication{Title: "A", Year: iptr(2020)}, true, adaID)
	store.givePublication(domain.Publication{Title: "B", Year: iptr(2021)}, true, adaID, bobID)
	store.givePublication(domain.Publication{Title: "C", Year: iptr(2022)}, false, adaID)
	svc := newPublicationService(store)
	own, err := svc.GetOwn(context.Background(), adaID)
	if err != nil {
		t.Fatal(err)
	}
	viewed, err := svc.View(context.Background(), bobID, adaID)
	if err != nil {
		t.Fatal(err)
	}
	if own.PublicationCount != 2 || viewed.PublicationCount != 2 {
		t.Errorf("counts = %d, %d; want 2 (confirmed only)", own.PublicationCount, viewed.PublicationCount)
	}
}
