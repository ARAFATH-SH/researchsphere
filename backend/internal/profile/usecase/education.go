package usecase

import (
	"context"
	"strings"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// Education limits (FR-4.4). Lengths count characters after trimming; migrations 000005
// and 000011 hold them as database constraints. The year range depends on the clock, so only the
// usecase checks it.
const (
	MaxEducationEntries           = 20 // entries per user
	MaxEducationInstitutionLength = 200
	MaxDegreeLength               = 100
	MaxFieldOfStudyLength         = 100
	MaxGradeLength                = 50
	MaxEducationDescriptionLength = 2000
	MinEducationYear              = 1950
	MaxYearsAhead                 = 10 // the latest year allowed is this many after the current one
)

// EducationInput is a new education entry. A nil optional field means "not set"; a nil
// EndYear means the entry is current.
type EducationInput struct {
	Institution  string
	Degree       *string
	FieldOfStudy *string
	StartYear    int
	EndYear      *int
	Grade        *string
	Description  *string
}

// Field is one field of a partial update. Set is false when the field was left out, and
// the stored value stays. Null is true when the field was set to null.
type Field[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// EducationPatch is a partial update of an education entry. Null clears Degree,
// FieldOfStudy, EndYear, Grade or Description; for Institution or StartYear it is refused
// like an empty value.
type EducationPatch struct {
	Institution  Field[string]
	Degree       Field[string]
	FieldOfStudy Field[string]
	StartYear    Field[int]
	EndYear      Field[int]
	Grade        Field[string]
	Description  Field[string]
}

// AddEducation validates in and adds it to the user's education (FR-4.4). It returns the
// first invalid field's error, in the contract's order, and domain.ErrEducationLimitReached
// when the user already has MaxEducationEntries; nothing is stored then. It returns
// domain.ErrNotFound when the account no longer exists.
func (s *Service) AddEducation(ctx context.Context, userID string, in EducationInput) (domain.Education, error) {
	e := domain.Education{
		UserID:       userID,
		Institution:  in.Institution,
		Degree:       in.Degree,
		FieldOfStudy: in.FieldOfStudy,
		StartYear:    in.StartYear,
		EndYear:      in.EndYear,
		Grade:        in.Grade,
		Description:  in.Description,
	}
	e, err := s.normalizeEducation(e)
	if err != nil {
		return domain.Education{}, err
	}
	var saved domain.Education
	err = s.store.RunInTx(ctx, func(tx domain.Tx) error {
		// Lock first, so the count below cannot change before the entry is stored.
		if err := tx.LockUser(ctx, userID); err != nil {
			return err
		}
		n, err := tx.CountEducation(ctx, userID)
		if err != nil {
			return err
		}
		if n >= MaxEducationEntries {
			return domain.ErrEducationLimitReached
		}
		saved, err = tx.InsertEducation(ctx, e)
		return err
	})
	if err != nil {
		return domain.Education{}, err
	}
	return saved, nil
}

// UpdateEducation changes the fields set in patch on the user's entry id and validates
// the entry as a whole, so the end year cannot end up before the start year whichever of
// the two changes. It returns domain.ErrNotFound when the user has no such entry,
// including one that belongs to someone else; that check comes before validation, so an
// invalid patch to someone else's entry reveals nothing either.
func (s *Service) UpdateEducation(ctx context.Context, userID, id string, patch EducationPatch) (domain.Education, error) {
	var saved domain.Education
	err := s.store.RunInTx(ctx, func(tx domain.Tx) error {
		e, err := tx.FindEducationForUpdate(ctx, userID, id)
		if err != nil {
			return err
		}
		if e, err = s.normalizeEducation(apply(e, patch)); err != nil {
			return err
		}
		saved, err = tx.UpdateEducation(ctx, e)
		return err
	})
	if err != nil {
		return domain.Education{}, err
	}
	return saved, nil
}

// RemoveEducation deletes the user's entry id. It returns domain.ErrNotFound when the
// user has no such entry, including one that belongs to someone else.
func (s *Service) RemoveEducation(ctx context.Context, userID, id string) error {
	return s.store.DeleteEducation(ctx, userID, id)
}

// apply copies the fields set in p onto e. A null Institution or StartYear becomes the
// empty value, which validation then refuses in the usual field order.
func apply(e domain.Education, p EducationPatch) domain.Education {
	if p.Institution.Set {
		e.Institution = p.Institution.Value // "" when null
	}
	if p.Degree.Set {
		e.Degree = nullable(p.Degree)
	}
	if p.FieldOfStudy.Set {
		e.FieldOfStudy = nullable(p.FieldOfStudy)
	}
	if p.StartYear.Set {
		e.StartYear = p.StartYear.Value // 0 when null
	}
	if p.EndYear.Set {
		e.EndYear = nullable(p.EndYear)
	}
	if p.Grade.Set {
		e.Grade = nullable(p.Grade)
	}
	if p.Description.Set {
		e.Description = nullable(p.Description)
	}
	return e
}

func nullable[T any](f Field[T]) *T {
	if f.Null {
		return nil
	}
	v := f.Value
	return &v
}

// normalizeEducation trims the text, turns blank optional text into nil and checks each
// field in the contract's order: institution, degree, field of study, start year, end
// year, grade, description.
func (s *Service) normalizeEducation(e domain.Education) (domain.Education, error) {
	e.Institution = strings.TrimSpace(e.Institution)
	if e.Institution == "" || !fits(e.Institution, MaxEducationInstitutionLength, true) {
		return e, domain.ErrInvalidEducationInstitution
	}
	var err error
	if e.Degree, err = optional(e.Degree, MaxDegreeLength, true, domain.ErrInvalidDegree); err != nil {
		return e, err
	}
	if e.FieldOfStudy, err = optional(e.FieldOfStudy, MaxFieldOfStudyLength, true, domain.ErrInvalidFieldOfStudy); err != nil {
		return e, err
	}
	latest := s.now().Year() + MaxYearsAhead
	if e.StartYear < MinEducationYear || e.StartYear > latest {
		return e, domain.ErrInvalidStartYear
	}
	if e.EndYear != nil && (*e.EndYear < e.StartYear || *e.EndYear > latest) {
		return e, domain.ErrInvalidEndYear
	}
	if e.Grade, err = optional(e.Grade, MaxGradeLength, true, domain.ErrInvalidGrade); err != nil {
		return e, err
	}
	if e.Description, err = optional(e.Description, MaxEducationDescriptionLength, false, domain.ErrInvalidDescription); err != nil {
		return e, err
	}
	return e, nil
}
