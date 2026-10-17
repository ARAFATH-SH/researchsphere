//go:build integration

package postgres

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// insertLink stores l in its own transaction.
func insertLink(ctx context.Context, s *Store, l domain.Link) (domain.Link, error) {
	var saved domain.Link
	err := s.RunInTx(ctx, func(tx domain.Tx) error {
		var err error
		saved, err = tx.InsertLink(ctx, l)
		return err
	})
	return saved, err
}

func TestFindContactDefaults(t *testing.T) {
	s, pool := newTestStore(t)
	user := newUser(t, pool)
	got, err := s.FindContact(t.Context(), user)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != user || got.Email == "" || got.Phone != nil || got.Birthday != nil ||
		got.EmailVisibility != domain.OnlyMe || got.PhoneVisibility != domain.OnlyMe || got.BirthdayVisibility != domain.OnlyMe {
		t.Errorf("got %+v, want the email, nothing set and every visibility only_me", got)
	}
	for _, id := range []string{"00000000-0000-0000-0000-0000000000ff", "not-a-uuid"} {
		if _, err := s.FindContact(t.Context(), id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("FindContact(%q): err = %v, want ErrNotFound", id, err)
		}
	}
}

func TestUpsertContact(t *testing.T) {
	s, pool := newTestStore(t)
	user := newUser(t, pool)
	c := domain.Contact{
		UserID: user, EmailVisibility: domain.LoggedIn,
		Phone: ptr("+8801712345678"), PhoneVisibility: domain.OnlyMe,
		Birthday: &domain.Birthday{Month: 2, Day: 29}, BirthdayVisibility: domain.LoggedIn,
	}
	if err := s.UpsertContact(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	got, err := s.FindContact(t.Context(), user)
	if err != nil {
		t.Fatal(err)
	}
	c.Email = got.Email
	if !reflect.DeepEqual(got, c) {
		t.Errorf("stored %+v, want %+v", got, c)
	}

	// A second save replaces everything, clearing what is nil.
	cleared := domain.Contact{UserID: user, EmailVisibility: domain.OnlyMe, PhoneVisibility: domain.LoggedIn, BirthdayVisibility: domain.OnlyMe}
	if err := s.UpsertContact(t.Context(), cleared); err != nil {
		t.Fatal(err)
	}
	got, err = s.FindContact(t.Context(), user)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phone != nil || got.Birthday != nil || got.PhoneVisibility != domain.LoggedIn || got.EmailVisibility != domain.OnlyMe {
		t.Errorf("after clearing got %+v", got)
	}

	if err := s.UpsertContact(t.Context(), domain.Contact{
		UserID: "00000000-0000-0000-0000-0000000000ff", EmailVisibility: domain.OnlyMe,
		PhoneVisibility: domain.OnlyMe, BirthdayVisibility: domain.OnlyMe,
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown user: err = %v, want ErrNotFound", err)
	}
}

// The database refuses what the usecase would refuse, should a later code path skip it.
func TestContactConstraints(t *testing.T) {
	s, pool := newTestStore(t)
	user := newUser(t, pool)
	base := domain.Contact{UserID: user, EmailVisibility: domain.OnlyMe, PhoneVisibility: domain.OnlyMe, BirthdayVisibility: domain.OnlyMe}
	tests := []struct {
		name string
		edit func(*domain.Contact)
	}{
		{"phone without +", func(c *domain.Contact) { c.Phone = ptr("8801712345678") }},
		{"30 February", func(c *domain.Contact) { c.Birthday = &domain.Birthday{Month: 2, Day: 30} }},
		{"unknown visibility", func(c *domain.Contact) { c.EmailVisibility = "everyone" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base
			tt.edit(&c)
			err := s.UpsertContact(t.Context(), c)
			if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "23514" {
				t.Errorf("err = %v, want a check violation", err)
			}
		})
	}
	if _, err := insertLink(t.Context(), s, domain.Link{UserID: user, Kind: domain.LinkWebsite, URL: "http://example.org"}); err == nil {
		t.Error("an http link was stored")
	}
}

func TestLinksAreOwnerScoped(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool), newUser(t, pool)
	first, err := insertLink(t.Context(), s, domain.Link{UserID: ada, Kind: domain.LinkGitHub, URL: "https://github.com/ada"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := insertLink(t.Context(), s, domain.Link{UserID: ada, Kind: domain.LinkWebsite, URL: "https://ada.example.org"})
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Link{
		{ID: first.ID, UserID: ada, Kind: domain.LinkGitHub, URL: "https://github.com/ada"},
		{ID: second.ID, UserID: ada, Kind: domain.LinkWebsite, URL: "https://ada.example.org"},
	}
	if got, err := s.ListLinks(t.Context(), ada); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ListLinks = %+v, %v; want %+v oldest first", got, err, want)
	}
	if got, _ := s.ListLinks(t.Context(), bob); len(got) != 0 {
		t.Errorf("Bob sees %+v, want none of Ada's links", got)
	}

	if err := s.DeleteLink(t.Context(), bob, first.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Bob deleting Ada's link: err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteLink(t.Context(), ada, "not-a-uuid"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("malformed ID: err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteLink(t.Context(), ada, first.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListLinks(t.Context(), ada); !reflect.DeepEqual(got, want[1:]) {
		t.Errorf("after delete ListLinks = %+v, want %+v", got, want[1:])
	}

	if _, err := insertLink(t.Context(), s, domain.Link{UserID: "00000000-0000-0000-0000-0000000000ff", Kind: domain.LinkOther, URL: "https://x.org"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown user: err = %v, want ErrNotFound", err)
	}
}

// The usecase's lock-count-insert keeps a user at 5 links even when requests race.
func TestAddLinkLimitUnderConcurrency(t *testing.T) {
	s, pool := newTestStore(t)
	user := newUser(t, pool)
	const limit = 5
	add := func(pause time.Duration) error {
		return s.RunInTx(context.Background(), func(tx domain.Tx) error {
			if err := tx.LockUser(context.Background(), user); err != nil {
				return err
			}
			n, err := tx.CountLinks(context.Background(), user)
			if err != nil {
				return err
			}
			if n >= limit {
				return domain.ErrLinkLimitReached
			}
			time.Sleep(pause)
			_, err = tx.InsertLink(context.Background(), domain.Link{UserID: user, Kind: domain.LinkOther, URL: "https://example.org"})
			return err
		})
	}
	for range limit - 1 {
		if err := add(0); err != nil {
			t.Fatal(err)
		}
	}

	const n = 6
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { errs[i] = add(50 * time.Millisecond) })
	}
	wg.Wait()
	var ok, refused int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, domain.ErrLinkLimitReached):
			refused++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 || refused != n-1 {
		t.Errorf("%d added and %d refused, want 1 and %d", ok, refused, n-1)
	}
	if list, _ := s.ListLinks(t.Context(), user); len(list) != limit {
		t.Errorf("%d links, want %d", len(list), limit)
	}
}
