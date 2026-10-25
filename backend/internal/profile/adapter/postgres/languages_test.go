//go:build integration

package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// insertLanguage stores l in its own transaction.
func insertLanguage(t *testing.T, s *Store, l domain.Language) domain.Language {
	t.Helper()
	var saved domain.Language
	err := s.RunInTx(t.Context(), func(tx domain.Tx) error {
		var err error
		saved, err = tx.InsertLanguage(t.Context(), l)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

// updateLanguage runs the steps the usecase runs to change a language: the owner-scoped
// lookup, then the owner-scoped update.
func updateLanguage(ctx context.Context, s *Store, userID, id string, change func(*domain.Language)) (domain.Language, error) {
	var saved domain.Language
	err := s.RunInTx(ctx, func(tx domain.Tx) error {
		l, err := tx.FindLanguageForUpdate(ctx, userID, id)
		if err != nil {
			return err
		}
		change(&l)
		saved, err = tx.UpdateLanguage(ctx, l)
		return err
	})
	return saved, err
}

// nameTaken runs LanguageNameTaken in its own transaction.
func nameTaken(t *testing.T, s *Store, userID, name, exceptID string) bool {
	t.Helper()
	var taken bool
	err := s.RunInTx(t.Context(), func(tx domain.Tx) error {
		var err error
		taken, err = tx.LanguageNameTaken(t.Context(), userID, name, exceptID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return taken
}

// A language is created, changed and deleted, and reads back as stored each time (FR-4.9).
func TestCreatePatchDeleteLanguage(t *testing.T) {
	s, pool := newTestStore(t)
	ada := newUser(t, pool)

	saved := insertLanguage(t, s, domain.Language{UserID: ada, Name: "bangla", Proficiency: domain.ProficiencyFullProfessional})
	want := domain.Language{ID: saved.ID, UserID: ada, Name: "bangla", Proficiency: domain.ProficiencyFullProfessional}
	if saved.ID == "" || !reflect.DeepEqual(saved, want) {
		t.Fatalf("inserted %+v, want %+v", saved, want)
	}

	got, err := updateLanguage(t.Context(), s, ada, saved.ID, func(l *domain.Language) {
		l.Name, l.Proficiency = "Bangla", domain.ProficiencyNativeOrBilingual
	})
	if err != nil {
		t.Fatal(err)
	}
	want = domain.Language{ID: saved.ID, UserID: ada, Name: "Bangla", Proficiency: domain.ProficiencyNativeOrBilingual}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("updated %+v, want %+v", got, want)
	}
	list, err := s.ListLanguages(t.Context(), ada)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(list, []domain.Language{want}) {
		t.Errorf("listed %+v, want %+v", list, want)
	}

	if err := s.DeleteLanguage(t.Context(), ada, saved.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.ListLanguages(t.Context(), ada); len(list) != 0 {
		t.Errorf("%d languages after delete, want none", len(list))
	}
	if err := s.DeleteLanguage(t.Context(), ada, saved.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleted twice: err = %v, want ErrNotFound", err)
	}
}

// Languages are listed oldest first; the usecase orders them by proficiency.
func TestListLanguagesOldestFirst(t *testing.T) {
	s, pool := newTestStore(t)
	ada := newUser(t, pool)
	first := insertLanguage(t, s, domain.Language{UserID: ada, Name: "Japanese", Proficiency: domain.ProficiencyElementary})
	second := insertLanguage(t, s, domain.Language{UserID: ada, Name: "Bangla", Proficiency: domain.ProficiencyNativeOrBilingual})
	third := insertLanguage(t, s, domain.Language{UserID: ada, Name: "English", Proficiency: domain.ProficiencyFullProfessional})
	list, err := s.ListLanguages(t.Context(), ada)
	if err != nil {
		t.Fatal(err)
	}
	if want := []domain.Language{first, second, third}; !reflect.DeepEqual(list, want) {
		t.Errorf("listed %+v, want %+v", list, want)
	}
}

// Names are compared ignoring letter case, per user, and a language may keep its own name.
func TestLanguageNameTaken(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool), newUser(t, pool)
	bangla := insertLanguage(t, s, domain.Language{UserID: ada, Name: "Bangla", Proficiency: domain.ProficiencyNativeOrBilingual})

	if !nameTaken(t, s, ada, "BANGLA", "") {
		t.Error(`"BANGLA" is not taken for Ada, want taken`)
	}
	if nameTaken(t, s, ada, "bangla", bangla.ID) {
		t.Error(`"bangla" is taken when excepting Bangla itself, want free`)
	}
	if nameTaken(t, s, ada, "English", "") {
		t.Error(`"English" is taken for Ada, want free`)
	}
	if nameTaken(t, s, bob, "Bangla", "") {
		t.Error(`"Bangla" is taken for Bob, want free`)
	}
}

// The unique index catches a name the usecase did not check, on insert and on update.
func TestLanguageUniqueNameBackstop(t *testing.T) {
	s, pool := newTestStore(t)
	ada := newUser(t, pool)
	insertLanguage(t, s, domain.Language{UserID: ada, Name: "Bangla", Proficiency: domain.ProficiencyNativeOrBilingual})
	english := insertLanguage(t, s, domain.Language{UserID: ada, Name: "English", Proficiency: domain.ProficiencyFullProfessional})

	err := s.RunInTx(t.Context(), func(tx domain.Tx) error {
		_, err := tx.InsertLanguage(t.Context(), domain.Language{UserID: ada, Name: "bangla", Proficiency: domain.ProficiencyElementary})
		return err
	})
	if !errors.Is(err, domain.ErrLanguageExists) {
		t.Errorf("insert: err = %v, want ErrLanguageExists", err)
	}
	_, err = updateLanguage(t.Context(), s, ada, english.ID, func(l *domain.Language) { l.Name = "BANGLA" })
	if !errors.Is(err, domain.ErrLanguageExists) {
		t.Errorf("update: err = %v, want ErrLanguageExists", err)
	}
}

func TestInsertLanguageUnknownUser(t *testing.T) {
	s, _ := newTestStore(t)
	err := s.RunInTx(t.Context(), func(tx domain.Tx) error {
		_, err := tx.InsertLanguage(t.Context(), domain.Language{
			UserID: "00000000-0000-0000-0000-0000000000ff", Name: "Bangla", Proficiency: domain.ProficiencyElementary,
		})
		return err
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// The constraints of migration 000010 catch a language the usecase would have refused.
func TestInsertLanguageConstraintBackstop(t *testing.T) {
	s, pool := newTestStore(t)
	ada := newUser(t, pool)
	err := s.RunInTx(t.Context(), func(tx domain.Tx) error {
		_, err := tx.InsertLanguage(t.Context(), domain.Language{UserID: ada, Name: "Bangla", Proficiency: "fluent"})
		return err
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "23514" {
		t.Errorf("err = %v, want a check violation", err)
	}
}

// The queries name the owner, so another user's language is never found, changed or
// deleted, and the answer is the same as for a language that does not exist (FR-4.9).
func TestLanguageIsOwnerScoped(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool), newUser(t, pool)
	entry := insertLanguage(t, s, domain.Language{UserID: ada, Name: "Bangla", Proficiency: domain.ProficiencyNativeOrBilingual})

	err := s.RunInTx(t.Context(), func(tx domain.Tx) error {
		_, err := tx.FindLanguageForUpdate(t.Context(), bob, entry.ID)
		return err
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Bob finds Ada's language: err = %v, want ErrNotFound", err)
	}

	// The update query checks the owner itself, not only the lookup before it.
	err = s.RunInTx(t.Context(), func(tx domain.Tx) error {
		hijack := entry
		hijack.UserID, hijack.Name = bob, "Hijacked"
		_, err := tx.UpdateLanguage(t.Context(), hijack)
		return err
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Bob updates Ada's language: err = %v, want ErrNotFound", err)
	}

	if err := s.DeleteLanguage(t.Context(), bob, entry.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Bob deletes Ada's language: err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteLanguage(t.Context(), ada, "not-a-uuid"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an ID that is not a UUID: err = %v, want ErrNotFound", err)
	}
	list, err := s.ListLanguages(t.Context(), ada)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(list, []domain.Language{entry}) {
		t.Fatalf("Ada's languages = %+v, want %+v unchanged", list, entry)
	}
}
