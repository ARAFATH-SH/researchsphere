package usecase

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

func TestNormalizeTagName(t *testing.T) {
	tests := []struct {
		in, want string
		wantErr  error
	}{
		{"Machine Learning", "Machine Learning", nil},
		{"  Machine   Learning  ", "Machine Learning", nil},
		{"Machine\tLearning\n", "Machine Learning", nil},
		{"Machine \r\n Learning", "Machine Learning", nil},
		{"Ünïcödé Ströng", "Ünïcödé Ströng", nil},
		{"a", "a", nil},
		{strings.Repeat("é", 60), strings.Repeat("é", 60), nil},
		{"  " + strings.Repeat("a", 60) + "  ", strings.Repeat("a", 60), nil},
		{strings.Repeat("a", 30) + "     " + strings.Repeat("a", 29), strings.Repeat("a", 30) + " " + strings.Repeat("a", 29), nil},
		{"", "", domain.ErrInvalidTagName},
		{" \t\n ", "", domain.ErrInvalidTagName},
		{strings.Repeat("a", 61), "", domain.ErrInvalidTagName},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := NormalizeTagName(tt.in)
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Errorf("NormalizeTagName(%q) = %q, %v; want %q, %v", tt.in, got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestAddTag(t *testing.T) {
	for _, kind := range []domain.TagKind{domain.KindInterest, domain.KindSkill} {
		t.Run(string(kind), func(t *testing.T) {
			t.Run("new tag is created, normalized, and linked", func(t *testing.T) {
				store := newFakeStore(testAccounts()...)
				tag, created, err := New(store).AddTag(t.Context(), adaID, kind, "  Machine   Learning ")
				if err != nil {
					t.Fatal(err)
				}
				if !created || tag.Name != "Machine Learning" || tag.ID == "" {
					t.Errorf("got %+v, created %v; want a new link to Machine Learning", tag, created)
				}
				if store.lockedUser != adaID {
					t.Errorf("locked %q, want the caller locked before counting", store.lockedUser)
				}
				if got, _ := store.ListTags(t.Context(), adaID, kind); !reflect.DeepEqual(got, []domain.Tag{tag}) {
					t.Errorf("user has %+v, want only %+v", got, tag)
				}
			})

			// FR-4.2: names that differ only in letter case are the same tag; the first
			// spelling stays.
			t.Run("existing tag is reused ignoring letter case", func(t *testing.T) {
				store := newFakeStore(testAccounts()...)
				svc := New(store)
				first, _, err := svc.AddTag(t.Context(), bobID, kind, "Machine Learning")
				if err != nil {
					t.Fatal(err)
				}
				got, created, err := svc.AddTag(t.Context(), adaID, kind, "machine   LEARNING")
				if err != nil {
					t.Fatal(err)
				}
				if !created || got != first {
					t.Errorf("got %+v, created %v; want the existing %+v newly linked", got, created, first)
				}
				if n := store.tagCount(kind); n != 1 {
					t.Errorf("%d tags exist, want 1", n)
				}
			})

			t.Run("adding a tag the user already has changes nothing", func(t *testing.T) {
				store := newFakeStore(testAccounts()...)
				svc := New(store)
				first, _, err := svc.AddTag(t.Context(), adaID, kind, "Robotics")
				if err != nil {
					t.Fatal(err)
				}
				got, created, err := svc.AddTag(t.Context(), adaID, kind, "ROBOTICS")
				if err != nil {
					t.Fatal(err)
				}
				if created || got != first {
					t.Errorf("got %+v, created %v; want %+v, not created", got, created, first)
				}
				if n, _ := store.CountTags(t.Context(), adaID, kind); n != 1 {
					t.Errorf("user has %d tags, want 1", n)
				}
			})

			t.Run("invalid name", func(t *testing.T) {
				store := newFakeStore(testAccounts()...)
				if _, _, err := New(store).AddTag(t.Context(), adaID, kind, "   "); !errors.Is(err, domain.ErrInvalidTagName) {
					t.Errorf("err = %v, want ErrInvalidTagName", err)
				}
				if store.txs != 0 {
					t.Error("the store was used for an invalid name")
				}
			})

			t.Run("account no longer exists", func(t *testing.T) {
				store := newFakeStore(testAccounts()...)
				if _, _, err := New(store).AddTag(t.Context(), nobody, kind, "Robotics"); !errors.Is(err, domain.ErrNotFound) {
					t.Errorf("err = %v, want ErrNotFound", err)
				}
				if store.tagCount(kind) != 0 {
					t.Error("a tag was created for a missing user")
				}
			})

			t.Run("store failure is passed on", func(t *testing.T) {
				store := newFakeStore(testAccounts()...)
				store.err = errDatabase
				if _, _, err := New(store).AddTag(t.Context(), adaID, kind, "Robotics"); !errors.Is(err, errDatabase) {
					t.Errorf("err = %v, want the store's error", err)
				}
			})
		})
	}
}

func TestAddTagLimits(t *testing.T) {
	tests := []struct {
		kind  domain.TagKind
		limit int
	}{
		{domain.KindInterest, MaxInterests},
		{domain.KindSkill, MaxSkills},
	}
	if MaxInterests != 20 || MaxSkills != 30 {
		t.Fatalf("limits are %d and %d, want 20 interests and 30 skills", MaxInterests, MaxSkills)
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			t.Run("one below the limit is accepted", func(t *testing.T) {
				store := newFakeStore(testAccounts()...)
				store.giveTags(adaID, tt.kind, tt.limit-1)
				if _, created, err := New(store).AddTag(t.Context(), adaID, tt.kind, "Last One"); err != nil || !created {
					t.Errorf("created %v, err %v; want the last tag accepted", created, err)
				}
			})

			t.Run("at the limit a new tag is refused and nothing is stored", func(t *testing.T) {
				store := newFakeStore(testAccounts()...)
				store.giveTags(adaID, tt.kind, tt.limit)
				before := store.tagCount(tt.kind)
				_, _, err := New(store).AddTag(t.Context(), adaID, tt.kind, "One Too Many")
				if !errors.Is(err, domain.ErrTagLimitReached) {
					t.Fatalf("err = %v, want ErrTagLimitReached", err)
				}
				if n, _ := store.CountTags(t.Context(), adaID, tt.kind); n != tt.limit {
					t.Errorf("user has %d tags, want %d", n, tt.limit)
				}
				if store.tagCount(tt.kind) != before {
					t.Error("the refused tag was left in the vocabulary")
				}
			})

			t.Run("at the limit an existing link is still a no-op, not a refusal", func(t *testing.T) {
				store := newFakeStore(testAccounts()...)
				store.giveTags(adaID, tt.kind, tt.limit)
				name := "existing " + string(tt.kind) + " 0" // differs in case from the stored name
				if _, created, err := New(store).AddTag(t.Context(), adaID, tt.kind, name); err != nil || created {
					t.Errorf("created %v, err %v; want a no-op", created, err)
				}
			})

			t.Run("the other kind has its own limit", func(t *testing.T) {
				store := newFakeStore(testAccounts()...)
				store.giveTags(adaID, tt.kind, tt.limit)
				other := domain.KindSkill
				if tt.kind == domain.KindSkill {
					other = domain.KindInterest
				}
				if _, _, err := New(store).AddTag(t.Context(), adaID, other, "Fresh"); err != nil {
					t.Errorf("err = %v, want the other kind accepted", err)
				}
			})
		})
	}
}

func TestRemoveTag(t *testing.T) {
	setup := func(t *testing.T) (*fakeStore, *Service, domain.Tag) {
		t.Helper()
		store := newFakeStore(testAccounts()...)
		svc := New(store)
		tag, _, err := svc.AddTag(t.Context(), adaID, domain.KindInterest, "Robotics")
		if err != nil {
			t.Fatal(err)
		}
		return store, svc, tag
	}

	t.Run("removes the link and keeps the tag", func(t *testing.T) {
		store, svc, tag := setup(t)
		if err := svc.RemoveTag(t.Context(), adaID, domain.KindInterest, tag.ID); err != nil {
			t.Fatal(err)
		}
		if n, _ := store.CountTags(t.Context(), adaID, domain.KindInterest); n != 0 {
			t.Errorf("user still has %d tags", n)
		}
		if store.tagCount(domain.KindInterest) != 1 {
			t.Error("the unused tag was deleted; it must stay for suggestions")
		}
	})

	tests := []struct {
		name  string
		user  string
		kind  domain.TagKind
		tagID func(domain.Tag) string
	}{
		{"a tag the user does not have", bobID, domain.KindInterest, func(tg domain.Tag) string { return tg.ID }},
		{"an interest removed as a skill", adaID, domain.KindSkill, func(tg domain.Tag) string { return tg.ID }},
		{"an unknown tag", adaID, domain.KindInterest, func(domain.Tag) string { return nobody }},
		{"a malformed ID", adaID, domain.KindInterest, func(domain.Tag) string { return "not-a-uuid" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, svc, tag := setup(t)
			err := svc.RemoveTag(t.Context(), tt.user, tt.kind, tt.tagID(tag))
			if !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("err = %v, want ErrNotFound", err)
			}
			if n, _ := store.CountTags(t.Context(), adaID, domain.KindInterest); n != 1 {
				t.Errorf("Ada has %d interests, want the link kept", n)
			}
		})
	}
}

func TestSuggestTags(t *testing.T) {
	t.Run("q is normalized and the limit passed on", func(t *testing.T) {
		store := newFakeStore()
		store.suggestion = []domain.Tag{{ID: adaID, Name: "Machine Learning"}}
		got, err := New(store).SuggestTags(t.Context(), domain.KindInterest, "  machine \t lea ", 5)
		if err != nil {
			t.Fatal(err)
		}
		if store.suggestQ != "machine lea" || store.suggestMax != 5 {
			t.Errorf("store got q %q, limit %d; want %q, 5", store.suggestQ, store.suggestMax, "machine lea")
		}
		if !reflect.DeepEqual(got, store.suggestion) {
			t.Errorf("got %+v, want the store's %+v", got, store.suggestion)
		}
	})

	t.Run("limit above the cap is reduced to 20", func(t *testing.T) {
		store := newFakeStore()
		if _, err := New(store).SuggestTags(t.Context(), domain.KindSkill, "go", 500); err != nil {
			t.Fatal(err)
		}
		if store.suggestMax != MaxSuggestions || MaxSuggestions != 20 {
			t.Errorf("store got limit %d, want 20", store.suggestMax)
		}
	})

	for name, q := range map[string]string{
		"empty q":             "",
		"blank q":             " \t ",
		"q longer than a tag": strings.Repeat("a", 61),
	} {
		t.Run(name+" gives an empty list without a query", func(t *testing.T) {
			store := newFakeStore()
			got, err := New(store).SuggestTags(t.Context(), domain.KindInterest, q, 10)
			if err != nil || got == nil || len(got) != 0 {
				t.Errorf("got %#v, %v; want an empty, non-nil list", got, err)
			}
			if store.suggests != 0 {
				t.Error("the store was queried")
			}
		})
	}

	for _, limit := range []int{0, -1} {
		t.Run("limit below 1", func(t *testing.T) {
			if _, err := New(newFakeStore()).SuggestTags(t.Context(), domain.KindInterest, "go", limit); !errors.Is(err, domain.ErrInvalidLimit) {
				t.Errorf("limit %d: err = %v, want ErrInvalidLimit", limit, err)
			}
		})
	}
}

// GET /profiles/me, PUT /profiles/me and GET /profiles/{userId} all return the user's
// interests and skills sorted by name (FR-4.5).
func TestProfilesIncludeTags(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	svc := New(store)
	for _, name := range []string{"robotics", "Machine Learning", "AI"} {
		if _, _, err := svc.AddTag(t.Context(), adaID, domain.KindInterest, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := svc.AddTag(t.Context(), adaID, domain.KindSkill, "Go"); err != nil {
		t.Fatal(err)
	}
	names := func(tags []domain.Tag) []string {
		var out []string
		for _, tg := range tags {
			out = append(out, tg.Name)
		}
		return out
	}
	check := func(t *testing.T, p domain.Profile) {
		t.Helper()
		if got := names(p.Interests); !reflect.DeepEqual(got, []string{"AI", "Machine Learning", "robotics"}) {
			t.Errorf("interests = %v, want sorted by name ignoring case", got)
		}
		if got := names(p.Skills); !reflect.DeepEqual(got, []string{"Go"}) {
			t.Errorf("skills = %v", got)
		}
	}

	t.Run("GetOwn", func(t *testing.T) {
		p, err := svc.GetOwn(t.Context(), adaID)
		if err != nil {
			t.Fatal(err)
		}
		check(t, p)
	})
	t.Run("View", func(t *testing.T) {
		p, err := svc.View(t.Context(), adaID)
		if err != nil {
			t.Fatal(err)
		}
		check(t, p)
	})
	t.Run("UpdateOwn", func(t *testing.T) {
		p, err := svc.UpdateOwn(t.Context(), adaID, UpdateInput{FullName: "Ada"})
		if err != nil {
			t.Fatal(err)
		}
		check(t, p)
	})
}
