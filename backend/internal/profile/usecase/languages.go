package usecase

import (
	"context"
	"slices"
	"strings"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// Language limits (FR-4.9). The name's length counts characters after trimming;
// migration 000010 holds it and the unique name as database constraints. The limit per
// user needs a count, so only the usecase checks it.
const (
	MaxLanguages          = 20 // languages per user
	MaxLanguageNameLength = 50
)

// proficiencyRank orders the levels, the highest last; a level not in it is invalid.
var proficiencyRank = map[domain.Proficiency]int{
	domain.ProficiencyElementary:          1,
	domain.ProficiencyLimitedWorking:      2,
	domain.ProficiencyProfessionalWorking: 3,
	domain.ProficiencyFullProfessional:    4,
	domain.ProficiencyNativeOrBilingual:   5,
}

// LanguageInput is a new language.
type LanguageInput struct {
	Name        string
	Proficiency domain.Proficiency
}

// LanguagePatch is a partial update of a language. Null is refused for both fields.
type LanguagePatch struct {
	Name        Field[string]
	Proficiency Field[domain.Proficiency]
}

// AddLanguage validates in and adds it to the user's languages (FR-4.9). It returns the
// first invalid field's error, then domain.ErrLanguageExists when the user has the name
// already, ignoring letter case, then domain.ErrLanguageLimitReached when the user
// already has MaxLanguages; nothing is stored then. It returns domain.ErrNotFound when
// the account no longer exists.
func (s *Service) AddLanguage(ctx context.Context, userID string, in LanguageInput) (domain.Language, error) {
	l, err := normalizeLanguage(domain.Language{UserID: userID, Name: in.Name, Proficiency: in.Proficiency})
	if err != nil {
		return domain.Language{}, err
	}
	var saved domain.Language
	err = s.store.RunInTx(ctx, func(tx domain.Tx) error {
		// Lock first, so the checks below cannot change before the language is stored.
		if err := tx.LockUser(ctx, userID); err != nil {
			return err
		}
		taken, err := tx.LanguageNameTaken(ctx, userID, l.Name, "")
		if err != nil {
			return err
		}
		if taken {
			return domain.ErrLanguageExists
		}
		n, err := tx.CountLanguages(ctx, userID)
		if err != nil {
			return err
		}
		if n >= MaxLanguages {
			return domain.ErrLanguageLimitReached
		}
		saved, err = tx.InsertLanguage(ctx, l)
		return err
	})
	if err != nil {
		return domain.Language{}, err
	}
	return saved, nil
}

// UpdateLanguage changes the fields set in patch on the user's language id. A new name
// must not be another of the user's languages, ignoring letter case; changing only the
// letter case of its own name is allowed. It returns domain.ErrNotFound when the user
// has no such language, including one that belongs to someone else; that check comes
// before validation, so an invalid patch to someone else's language reveals nothing
// either.
func (s *Service) UpdateLanguage(ctx context.Context, userID, id string, patch LanguagePatch) (domain.Language, error) {
	var saved domain.Language
	err := s.store.RunInTx(ctx, func(tx domain.Tx) error {
		l, err := tx.FindLanguageForUpdate(ctx, userID, id)
		if err != nil {
			return err
		}
		if patch.Name.Set {
			l.Name = patch.Name.Value // "" when null, which validation refuses
		}
		if patch.Proficiency.Set {
			l.Proficiency = patch.Proficiency.Value
		}
		if l, err = normalizeLanguage(l); err != nil {
			return err
		}
		if patch.Name.Set {
			taken, err := tx.LanguageNameTaken(ctx, userID, l.Name, l.ID)
			if err != nil {
				return err
			}
			if taken {
				return domain.ErrLanguageExists
			}
		}
		saved, err = tx.UpdateLanguage(ctx, l)
		return err
	})
	if err != nil {
		return domain.Language{}, err
	}
	return saved, nil
}

// RemoveLanguage deletes the user's language id. It returns domain.ErrNotFound when the
// user has no such language, including one that belongs to someone else.
func (s *Service) RemoveLanguage(ctx context.Context, userID, id string) error {
	return s.store.DeleteLanguage(ctx, userID, id)
}

// normalizeLanguage trims the name and checks the name, then the proficiency.
func normalizeLanguage(l domain.Language) (domain.Language, error) {
	l.Name = strings.TrimSpace(l.Name)
	if l.Name == "" || !fits(l.Name, MaxLanguageNameLength, true) {
		return l, domain.ErrInvalidLanguageName
	}
	if proficiencyRank[l.Proficiency] == 0 {
		return l, domain.ErrInvalidProficiency
	}
	return l, nil
}

// sortLanguages puts the highest proficiency first. The sort is stable, so languages of
// one level stay in the store's order, the oldest first.
func sortLanguages(languages []domain.Language) {
	slices.SortStableFunc(languages, func(a, b domain.Language) int {
		return proficiencyRank[b.Proficiency] - proficiencyRank[a.Proficiency]
	})
}
