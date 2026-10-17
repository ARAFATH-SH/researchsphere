package usecase

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

func validContact() ContactInput {
	return ContactInput{
		EmailVisibility:    domain.OnlyMe,
		PhoneVisibility:    domain.OnlyMe,
		BirthdayVisibility: domain.OnlyMe,
	}
}

func TestUpdateContactValidation(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(*ContactInput)
		wantErr error
	}{
		{"nothing set", func(*ContactInput) {}, nil},
		{"E.164 phone", func(in *ContactInput) { in.Phone = ptr("+8801712345678") }, nil},
		{"phone with spaces and hyphens", func(in *ContactInput) { in.Phone = ptr(" +880 1712-345678 ") }, nil},
		{"blank phone clears it", func(in *ContactInput) { in.Phone = ptr("  ") }, nil},
		{"phone without +", func(in *ContactInput) { in.Phone = ptr("8801712345678") }, domain.ErrInvalidPhone},
		{"phone with letters", func(in *ContactInput) { in.Phone = ptr("+88017ABC") }, domain.ErrInvalidPhone},
		{"phone with 16 digits", func(in *ContactInput) { in.Phone = ptr("+" + strings.Repeat("1", 16)) }, domain.ErrInvalidPhone},
		{"phone with one digit", func(in *ContactInput) { in.Phone = ptr("+1") }, domain.ErrInvalidPhone},
		{"phone country code starting with 0", func(in *ContactInput) { in.Phone = ptr("+0123456") }, domain.ErrInvalidPhone},
		{"phone with brackets", func(in *ContactInput) { in.Phone = ptr("+1 (555) 1234567") }, domain.ErrInvalidPhone},
		{"29 February", func(in *ContactInput) { in.Birthday = &domain.Birthday{Month: 2, Day: 29} }, nil},
		{"31 December", func(in *ContactInput) { in.Birthday = &domain.Birthday{Month: 12, Day: 31} }, nil},
		{"30 February", func(in *ContactInput) { in.Birthday = &domain.Birthday{Month: 2, Day: 30} }, domain.ErrInvalidBirthday},
		{"31 April", func(in *ContactInput) { in.Birthday = &domain.Birthday{Month: 4, Day: 31} }, domain.ErrInvalidBirthday},
		{"month 0", func(in *ContactInput) { in.Birthday = &domain.Birthday{Month: 0, Day: 1} }, domain.ErrInvalidBirthday},
		{"month 13", func(in *ContactInput) { in.Birthday = &domain.Birthday{Month: 13, Day: 1} }, domain.ErrInvalidBirthday},
		{"day 0", func(in *ContactInput) { in.Birthday = &domain.Birthday{Month: 1, Day: 0} }, domain.ErrInvalidBirthday},
		{"logged_in everywhere", func(in *ContactInput) {
			in.EmailVisibility, in.PhoneVisibility, in.BirthdayVisibility = domain.LoggedIn, domain.LoggedIn, domain.LoggedIn
		}, nil},
		{"unknown email visibility", func(in *ContactInput) { in.EmailVisibility = "everyone" }, domain.ErrInvalidVisibility},
		{"connections is not allowed yet", func(in *ContactInput) { in.PhoneVisibility = "connections" }, domain.ErrInvalidVisibility},
		{"empty birthday visibility", func(in *ContactInput) { in.BirthdayVisibility = "" }, domain.ErrInvalidVisibility},
		{"phone is checked before birthday and visibility", func(in *ContactInput) {
			in.Phone, in.Birthday, in.EmailVisibility = ptr("123"), &domain.Birthday{Month: 2, Day: 30}, "x"
		}, domain.ErrInvalidPhone},
		{"birthday is checked before visibility", func(in *ContactInput) {
			in.Birthday, in.EmailVisibility = &domain.Birthday{Month: 2, Day: 30}, "x"
		}, domain.ErrInvalidBirthday},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore(testAccounts()...)
			in := validContact()
			tt.edit(&in)
			_, err := New(store).UpdateContact(t.Context(), adaID, in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if _, stored := store.contacts[adaID]; stored != (tt.wantErr == nil) {
				t.Errorf("stored = %v, want %v", stored, tt.wantErr == nil)
			}
		})
	}
}

func TestUpdateContactStoresAndClears(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	svc := New(store)
	in := ContactInput{
		EmailVisibility:    domain.LoggedIn,
		Phone:              ptr("+880 1712-345678"),
		PhoneVisibility:    domain.OnlyMe,
		Birthday:           &domain.Birthday{Month: 2, Day: 29},
		BirthdayVisibility: domain.LoggedIn,
	}
	got, err := svc.UpdateContact(t.Context(), adaID, in)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Contact{
		UserID: adaID, EmailVisibility: domain.LoggedIn,
		Phone: ptr("+8801712345678"), PhoneVisibility: domain.OnlyMe,
		Birthday: &domain.Birthday{Month: 2, Day: 29}, BirthdayVisibility: domain.LoggedIn,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	// PUT replaces everything: a missing phone and birthday clear them.
	got, err = svc.UpdateContact(t.Context(), adaID, validContact())
	if err != nil {
		t.Fatal(err)
	}
	if got.Phone != nil || got.Birthday != nil || got.EmailVisibility != domain.OnlyMe {
		t.Errorf("after clearing got %+v", got)
	}

	if _, err := svc.UpdateContact(t.Context(), nobody, validContact()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown account: err = %v, want ErrNotFound", err)
	}
}

func TestGetContactDefaults(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	store.emails[bobID] = "bob@example.org"
	got, err := New(store).GetContact(t.Context(), bobID)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Contact{
		UserID: bobID, Email: "bob@example.org",
		EmailVisibility: domain.OnlyMe, PhoneVisibility: domain.OnlyMe, BirthdayVisibility: domain.OnlyMe,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestAddLink(t *testing.T) {
	tests := []struct {
		name    string
		kind    domain.LinkKind
		url     string
		wantURL string
		wantErr error
	}{
		{"website", domain.LinkWebsite, "https://ada.example.org", "https://ada.example.org", nil},
		{"trimmed", domain.LinkGitHub, "  https://github.com/ada  ", "https://github.com/ada", nil},
		{"scheme in capitals is lower-cased", domain.LinkOther, "HTTPS://example.org/x", "https://example.org/x", nil},
		{"every kind: google_scholar", domain.LinkGoogleScholar, "https://scholar.example.org/ada", "https://scholar.example.org/ada", nil},
		{"every kind: researchgate", domain.LinkResearchGate, "https://rg.example.org/ada", "https://rg.example.org/ada", nil},
		{"every kind: linkedin", domain.LinkLinkedIn, "https://in.example.org/ada", "https://in.example.org/ada", nil},
		{"300 characters", domain.LinkWebsite, "https://" + strings.Repeat("é", 292), "https://" + strings.Repeat("é", 292), nil},
		{"http is refused", domain.LinkWebsite, "http://ada.example.org", "", domain.ErrInvalidLinkURL},
		{"no scheme", domain.LinkWebsite, "ada.example.org", "", domain.ErrInvalidLinkURL},
		{"javascript scheme", domain.LinkWebsite, "javascript:alert(1)", "", domain.ErrInvalidLinkURL},
		{"no host", domain.LinkWebsite, "https://", "", domain.ErrInvalidLinkURL},
		{"no host, only a path", domain.LinkWebsite, "https:///path", "", domain.ErrInvalidLinkURL},
		{"whitespace inside", domain.LinkWebsite, "https://example.org/a b", "", domain.ErrInvalidLinkURL},
		{"301 characters", domain.LinkWebsite, "https://" + strings.Repeat("a", 293), "", domain.ErrInvalidLinkURL},
		{"unknown kind", "twitter", "https://example.org", "", domain.ErrInvalidLinkKind},
		{"kind is checked before the URL", "", "http://example.org", "", domain.ErrInvalidLinkKind},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore(testAccounts()...)
			got, err := New(store).AddLink(t.Context(), adaID, tt.kind, tt.url)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if len(store.profileLinks) != 0 {
					t.Errorf("stored %d links, want none", len(store.profileLinks))
				}
				return
			}
			if got.URL != tt.wantURL || got.Kind != tt.kind || got.UserID != adaID || got.ID == "" {
				t.Errorf("got %+v, want URL %q kind %q", got, tt.wantURL, tt.kind)
			}
		})
	}
}

func TestAddLinkLimit(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	store.giveLinks(adaID, MaxLinks-1)
	svc := New(store)
	if _, err := svc.AddLink(t.Context(), adaID, domain.LinkWebsite, "https://example.org/5"); err != nil {
		t.Fatalf("5th link: %v", err)
	}
	if store.lockedUser != adaID {
		t.Errorf("locked %q, want the user locked before counting", store.lockedUser)
	}
	_, err := svc.AddLink(t.Context(), adaID, domain.LinkWebsite, "https://example.org/6")
	if !errors.Is(err, domain.ErrLinkLimitReached) {
		t.Fatalf("6th link: err = %v, want ErrLinkLimitReached", err)
	}
	if n, _ := store.CountLinks(t.Context(), adaID); n != MaxLinks {
		t.Errorf("links = %d, want %d", n, MaxLinks)
	}
	// Another user's links do not count against Ada's limit.
	if _, err := svc.AddLink(t.Context(), eveID, domain.LinkWebsite, "https://example.org/eve"); err != nil {
		t.Errorf("Eve's first link: %v", err)
	}
}

func TestRemoveLink(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	svc := New(store)
	l, err := svc.AddLink(t.Context(), adaID, domain.LinkWebsite, "https://example.org")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveLink(t.Context(), eveID, l.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("someone else's link: err = %v, want ErrNotFound", err)
	}
	if err := svc.RemoveLink(t.Context(), adaID, l.ID); err != nil {
		t.Errorf("own link: %v", err)
	}
	if err := svc.RemoveLink(t.Context(), adaID, l.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("removed twice: err = %v, want ErrNotFound", err)
	}
}

func TestVisibleTo(t *testing.T) {
	link := domain.Link{ID: "l1", UserID: adaID, Kind: domain.LinkGitHub, URL: "https://github.com/ada"}
	contact := func(email, phone, birthday domain.Visibility) domain.Contact {
		return domain.Contact{
			UserID: adaID, Email: "ada@example.org", EmailVisibility: email,
			Phone: ptr("+441234567890"), PhoneVisibility: phone,
			Birthday: &domain.Birthday{Month: 12, Day: 10}, BirthdayVisibility: birthday,
			Links: []domain.Link{link},
		}
	}
	everything := domain.VisibleContact{
		Email: ptr("ada@example.org"), Phone: ptr("+441234567890"),
		Birthday: &domain.Birthday{Month: 12, Day: 10}, Links: []domain.Link{link},
	}
	tests := []struct {
		name   string
		viewer string
		c      domain.Contact
		want   domain.VisibleContact
	}{
		{"the owner sees everything, even only_me", adaID, contact(domain.OnlyMe, domain.OnlyMe, domain.OnlyMe), everything},
		{"another user sees only the links when all is only_me", eveID, contact(domain.OnlyMe, domain.OnlyMe, domain.OnlyMe),
			domain.VisibleContact{Links: []domain.Link{link}}},
		{"another user sees every logged_in item", eveID, contact(domain.LoggedIn, domain.LoggedIn, domain.LoggedIn), everything},
		{"another user sees the email only", eveID, contact(domain.LoggedIn, domain.OnlyMe, domain.OnlyMe),
			domain.VisibleContact{Email: ptr("ada@example.org"), Links: []domain.Link{link}}},
		{"another user sees the phone only", eveID, contact(domain.OnlyMe, domain.LoggedIn, domain.OnlyMe),
			domain.VisibleContact{Phone: ptr("+441234567890"), Links: []domain.Link{link}}},
		{"another user sees the birthday only", eveID, contact(domain.OnlyMe, domain.OnlyMe, domain.LoggedIn),
			domain.VisibleContact{Birthday: &domain.Birthday{Month: 12, Day: 10}, Links: []domain.Link{link}}},
		{"an item that is not set is left out even when logged_in", eveID,
			domain.Contact{UserID: adaID, EmailVisibility: domain.LoggedIn, PhoneVisibility: domain.LoggedIn, BirthdayVisibility: domain.LoggedIn},
			domain.VisibleContact{Links: []domain.Link{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VisibleTo(tt.viewer, tt.c); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// GetOwn shows the owner everything; View shows another user only the logged_in items.
func TestProfileContactFiltering(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	store.emails[adaID] = "ada@example.org"
	svc := New(store)
	_, err := svc.UpdateContact(t.Context(), adaID, ContactInput{
		EmailVisibility:    domain.OnlyMe,
		Phone:              ptr("+441234567890"),
		PhoneVisibility:    domain.LoggedIn,
		Birthday:           &domain.Birthday{Month: 12, Day: 10},
		BirthdayVisibility: domain.OnlyMe,
	})
	if err != nil {
		t.Fatal(err)
	}
	link, err := svc.AddLink(t.Context(), adaID, domain.LinkWebsite, "https://ada.example.org")
	if err != nil {
		t.Fatal(err)
	}

	own, err := svc.GetOwn(t.Context(), adaID)
	if err != nil {
		t.Fatal(err)
	}
	wantOwn := domain.VisibleContact{
		Email: ptr("ada@example.org"), Phone: ptr("+441234567890"),
		Birthday: &domain.Birthday{Month: 12, Day: 10}, Links: []domain.Link{link},
	}
	if !reflect.DeepEqual(own.Contact, wantOwn) {
		t.Errorf("owner sees %+v, want %+v", own.Contact, wantOwn)
	}

	viewedByOwner, err := svc.View(t.Context(), adaID, adaID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(viewedByOwner.Contact, wantOwn) {
		t.Errorf("owner through View sees %+v, want %+v", viewedByOwner.Contact, wantOwn)
	}

	other, err := svc.View(t.Context(), eveID, adaID)
	if err != nil {
		t.Fatal(err)
	}
	wantOther := domain.VisibleContact{Phone: ptr("+441234567890"), Links: []domain.Link{link}}
	if !reflect.DeepEqual(other.Contact, wantOther) {
		t.Errorf("other user sees %+v, want %+v", other.Contact, wantOther)
	}
}
