package usecase

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

func lang(name string, p domain.Proficiency) LanguageInput {
	return LanguageInput{Name: name, Proficiency: p}
}

func TestAddLanguage(t *testing.T) {
	t.Run("stored trimmed, in the casing typed, under the user lock", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		got, err := New(store).AddLanguage(t.Context(), adaID, lang("  bAngla \t", domain.ProficiencyNativeOrBilingual))
		if err != nil {
			t.Fatal(err)
		}
		want := domain.Language{ID: got.ID, UserID: adaID, Name: "bAngla", Proficiency: domain.ProficiencyNativeOrBilingual}
		if got.ID == "" || !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
		if !reflect.DeepEqual(store.languages[got.ID], want) {
			t.Errorf("stored %+v, want %+v", store.languages[got.ID], want)
		}
		if store.lockedUser != adaID {
			t.Errorf("locked user %q, want %q", store.lockedUser, adaID)
		}
	})

	t.Run("every proficiency and a name of 50 characters are accepted", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		svc := New(store)
		for i, p := range []domain.Proficiency{
			domain.ProficiencyElementary, domain.ProficiencyLimitedWorking, domain.ProficiencyProfessionalWorking,
			domain.ProficiencyFullProfessional, domain.ProficiencyNativeOrBilingual,
		} {
			if _, err := svc.AddLanguage(t.Context(), adaID, lang(fmt.Sprintf("Language %d", i), p)); err != nil {
				t.Errorf("%s: %v", p, err)
			}
		}
		if _, err := svc.AddLanguage(t.Context(), adaID, lang(strings.Repeat("é", 50), domain.ProficiencyElementary)); err != nil {
			t.Errorf("50 characters: %v", err)
		}
	})

	refused := []struct {
		name    string
		in      LanguageInput
		wantErr error
	}{
		{"empty name", lang("", domain.ProficiencyElementary), domain.ErrInvalidLanguageName},
		{"blank name", lang(" \t ", domain.ProficiencyElementary), domain.ErrInvalidLanguageName},
		{"name of 51 characters", lang(strings.Repeat("é", 51), domain.ProficiencyElementary), domain.ErrInvalidLanguageName},
		{"name with a line break", lang("Old\nNorse", domain.ProficiencyElementary), domain.ErrInvalidLanguageName},
		{"name with a carriage return", lang("Old\rNorse", domain.ProficiencyElementary), domain.ErrInvalidLanguageName},
		{"missing proficiency", lang("English", ""), domain.ErrInvalidProficiency},
		{"unknown proficiency", lang("English", "fluent"), domain.ErrInvalidProficiency},
		// The name is checked first.
		{"both invalid", lang("", "fluent"), domain.ErrInvalidLanguageName},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore(testAccounts()...)
			if _, err := New(store).AddLanguage(t.Context(), adaID, tt.in); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(store.languages) != 0 {
				t.Errorf("%d languages stored, want none", len(store.languages))
			}
		})
	}

	t.Run("the same name in another case is refused", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		svc := New(store)
		if _, err := svc.AddLanguage(t.Context(), adaID, lang("Bangla", domain.ProficiencyNativeOrBilingual)); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"Bangla", "bangla", " BANGLA "} {
			if _, err := svc.AddLanguage(t.Context(), adaID, lang(name, domain.ProficiencyElementary)); !errors.Is(err, domain.ErrLanguageExists) {
				t.Errorf("%q: err = %v, want ErrLanguageExists", name, err)
			}
		}
		if len(store.languages) != 1 {
			t.Errorf("%d languages stored, want 1", len(store.languages))
		}
		// Names are unique per user only.
		if _, err := svc.AddLanguage(t.Context(), bobID, lang("bangla", domain.ProficiencyElementary)); err != nil {
			t.Errorf("another user: %v", err)
		}
	})

	t.Run("at most 20 languages", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		svc := New(store)
		for i := range MaxLanguages {
			if _, err := svc.AddLanguage(t.Context(), adaID, lang(fmt.Sprintf("Language %d", i), domain.ProficiencyElementary)); err != nil {
				t.Fatalf("language %d: %v", i+1, err)
			}
		}
		if _, err := svc.AddLanguage(t.Context(), adaID, lang("One more", domain.ProficiencyElementary)); !errors.Is(err, domain.ErrLanguageLimitReached) {
			t.Fatalf("21st language: err = %v, want ErrLanguageLimitReached", err)
		}
		// A duplicate at the limit names the duplicate.
		if _, err := svc.AddLanguage(t.Context(), adaID, lang("language 0", domain.ProficiencyElementary)); !errors.Is(err, domain.ErrLanguageExists) {
			t.Fatalf("duplicate at the limit: err = %v, want ErrLanguageExists", err)
		}
		if n, _ := store.CountLanguages(t.Context(), adaID); n != MaxLanguages {
			t.Errorf("%d languages stored, want %d", n, MaxLanguages)
		}
		// The limit is per user.
		if _, err := svc.AddLanguage(t.Context(), bobID, lang("One more", domain.ProficiencyElementary)); err != nil {
			t.Errorf("another user: %v", err)
		}
	})

	t.Run("a duplicate the store finds is refused", func(t *testing.T) {
		// Two requests at the same moment: the unique index is the backstop.
		store := newFakeStore(testAccounts()...)
		store.languageError = domain.ErrLanguageExists
		if _, err := New(store).AddLanguage(t.Context(), adaID, lang("Bangla", domain.ProficiencyElementary)); !errors.Is(err, domain.ErrLanguageExists) {
			t.Errorf("err = %v, want ErrLanguageExists", err)
		}
	})

	t.Run("account no longer exists", func(t *testing.T) {
		_, err := New(newFakeStore()).AddLanguage(t.Context(), nobody, lang("Bangla", domain.ProficiencyElementary))
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("store error", func(t *testing.T) {
		store := newFakeStore(testAccounts()...)
		store.err = errDatabase
		if _, err := New(store).AddLanguage(t.Context(), adaID, lang("Bangla", domain.ProficiencyElementary)); !errors.Is(err, errDatabase) {
			t.Errorf("err = %v, want the store error", err)
		}
	})
}

func TestUpdateLanguage(t *testing.T) {
	// setup gives Ada "Bangla" (native) and "English" (professional working); it returns
	// the stored "Bangla".
	setup := func(t *testing.T) (*fakeStore, *Service, domain.Language) {
		t.Helper()
		store := newFakeStore(testAccounts()...)
		svc := New(store)
		stored, err := svc.AddLanguage(t.Context(), adaID, lang("Bangla", domain.ProficiencyNativeOrBilingual))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.AddLanguage(t.Context(), adaID, lang("English", domain.ProficiencyProfessionalWorking)); err != nil {
			t.Fatal(err)
		}
		return store, svc, stored
	}

	accepted := []struct {
		name  string
		patch LanguagePatch
		want  func(domain.Language) domain.Language
	}{
		{"nothing set keeps everything", LanguagePatch{}, func(l domain.Language) domain.Language { return l }},
		{"proficiency only", LanguagePatch{Proficiency: set(domain.ProficiencyFullProfessional)},
			func(l domain.Language) domain.Language { l.Proficiency = domain.ProficiencyFullProfessional; return l }},
		{"a new name, trimmed", LanguagePatch{Name: set("  Bengali ")},
			func(l domain.Language) domain.Language { l.Name = "Bengali"; return l }},
		{"only the letter case of its own name", LanguagePatch{Name: set("BANGLA")},
			func(l domain.Language) domain.Language { l.Name = "BANGLA"; return l }},
	}
	for _, tt := range accepted {
		t.Run(tt.name, func(t *testing.T) {
			store, svc, stored := setup(t)
			got, err := svc.UpdateLanguage(t.Context(), adaID, stored.ID, tt.patch)
			if err != nil {
				t.Fatal(err)
			}
			want := tt.want(stored)
			if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(store.languages[stored.ID], want) {
				t.Errorf("got %+v, stored %+v, want %+v", got, store.languages[stored.ID], want)
			}
		})
	}

	refused := []struct {
		name    string
		patch   LanguagePatch
		wantErr error
	}{
		{"null name", LanguagePatch{Name: null[string]()}, domain.ErrInvalidLanguageName},
		{"blank name", LanguagePatch{Name: set("  ")}, domain.ErrInvalidLanguageName},
		{"name of 51 characters", LanguagePatch{Name: set(strings.Repeat("a", 51))}, domain.ErrInvalidLanguageName},
		{"name with a line break", LanguagePatch{Name: set("Ban\ngla")}, domain.ErrInvalidLanguageName},
		{"null proficiency", LanguagePatch{Proficiency: null[domain.Proficiency]()}, domain.ErrInvalidProficiency},
		{"unknown proficiency", LanguagePatch{Proficiency: set[domain.Proficiency]("fluent")}, domain.ErrInvalidProficiency},
		{"another entry's name in another case", LanguagePatch{Name: set("english")}, domain.ErrLanguageExists},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			store, svc, stored := setup(t)
			if _, err := svc.UpdateLanguage(t.Context(), adaID, stored.ID, tt.patch); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got := store.languages[stored.ID]; !reflect.DeepEqual(got, stored) {
				t.Errorf("stored %+v, want it unchanged %+v", got, stored)
			}
		})
	}

	t.Run("a duplicate the store finds is refused", func(t *testing.T) {
		store, svc, stored := setup(t)
		store.languageError = domain.ErrLanguageExists
		if _, err := svc.UpdateLanguage(t.Context(), adaID, stored.ID, LanguagePatch{Name: set("Bengali")}); !errors.Is(err, domain.ErrLanguageExists) {
			t.Errorf("err = %v, want ErrLanguageExists", err)
		}
	})

	// Someone else's entry gives the same ErrNotFound as one that does not exist, even
	// for a patch that would be refused, so the answer reveals nothing.
	for name, patch := range map[string]LanguagePatch{
		"valid patch":   {Name: set("Bengali")},
		"invalid patch": {Name: null[string]()},
	} {
		t.Run("someone else's entry, "+name, func(t *testing.T) {
			store, svc, stored := setup(t)
			if _, err := svc.UpdateLanguage(t.Context(), bobID, stored.ID, patch); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
			if got := store.languages[stored.ID]; !reflect.DeepEqual(got, stored) {
				t.Errorf("stored %+v, want it unchanged %+v", got, stored)
			}
		})
		t.Run("unknown entry, "+name, func(t *testing.T) {
			_, svc, _ := setup(t)
			if _, err := svc.UpdateLanguage(t.Context(), adaID, "not-a-uuid", patch); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestRemoveLanguage(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	svc := New(store)
	stored, err := svc.AddLanguage(t.Context(), adaID, lang("Bangla", domain.ProficiencyNativeOrBilingual))
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.RemoveLanguage(t.Context(), bobID, stored.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("someone else's entry: err = %v, want ErrNotFound", err)
	}
	if _, ok := store.languages[stored.ID]; !ok {
		t.Fatal("someone else removed Ada's language")
	}
	if err := svc.RemoveLanguage(t.Context(), adaID, "not-a-uuid"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown entry: err = %v, want ErrNotFound", err)
	}
	if err := svc.RemoveLanguage(t.Context(), adaID, stored.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.languages[stored.ID]; ok {
		t.Error("language still stored after removal")
	}
	if err := svc.RemoveLanguage(t.Context(), adaID, stored.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("removed twice: err = %v, want ErrNotFound", err)
	}
}

// GetOwn and View include the languages in profile order (FR-4.9, FR-4.5): the highest
// proficiency first, then the oldest entry first.
func TestProfileHasLanguages(t *testing.T) {
	store := newFakeStore(testAccounts()...)
	svc := New(store)
	add := func(name string, p domain.Proficiency) domain.Language {
		t.Helper()
		l, err := svc.AddLanguage(t.Context(), adaID, lang(name, p))
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	elementary := add("Japanese", domain.ProficiencyElementary)
	professionalA := add("German", domain.ProficiencyProfessionalWorking)
	native := add("Bangla", domain.ProficiencyNativeOrBilingual)
	limited := add("French", domain.ProficiencyLimitedWorking)
	full := add("English", domain.ProficiencyFullProfessional)
	professionalB := add("Arabic", domain.ProficiencyProfessionalWorking) // same level as German, added after it
	want := []domain.Language{native, full, professionalA, professionalB, limited, elementary}

	own, err := svc.GetOwn(t.Context(), adaID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(own.Languages, want) {
		t.Errorf("GetOwn languages = %+v, want %+v", own.Languages, want)
	}
	viewed, err := svc.View(t.Context(), eveID, adaID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(viewed.Languages, want) {
		t.Errorf("View languages = %+v, want %+v", viewed.Languages, want)
	}
	// Another user's profile does not show Ada's languages.
	eve, err := svc.View(t.Context(), adaID, eveID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eve.Languages) != 0 {
		t.Errorf("Eve's profile has %d languages, want none", len(eve.Languages))
	}
}
