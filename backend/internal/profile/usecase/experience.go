package usecase

import (
	"context"
	"strings"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// Experience limits (FR-4.8). Lengths count characters after trimming; migration 000009
// holds them as database constraints. "Not in the future" depends on the clock, so only
// the usecase checks it.
const (
	MaxExperienceEntries           = 30 // entries per user
	MaxTitleLength                 = 100
	MaxOrganizationLength          = 150
	MaxLocationLength              = 100
	MaxExperienceDescriptionLength = 2000
	MinExperienceYear              = 1950
)

var employmentTypes = map[domain.EmploymentType]bool{
	domain.EmploymentFullTime: true, domain.EmploymentPartTime: true, domain.EmploymentInternship: true,
	domain.EmploymentContract: true, domain.EmploymentFreelance: true, domain.EmploymentSelfEmployed: true,
	domain.EmploymentResearchAssistant: true, domain.EmploymentTeachingAssistant: true,
	domain.EmploymentPostdoc: true, domain.EmploymentVolunteer: true,
}

var locationTypes = map[domain.LocationType]bool{
	domain.LocationOnSite: true, domain.LocationHybrid: true, domain.LocationRemote: true,
}

// ExperienceInput is a new experience entry. A nil optional field means "not set".
type ExperienceInput struct {
	Title          string
	Organization   string
	EmploymentType domain.EmploymentType
	Location       *string
	LocationType   *domain.LocationType
	Start          domain.YearMonth
	End            *domain.YearMonth
	IsCurrent      bool
	Description    *string
}

// ExperiencePatch is a partial update of an experience entry. Null clears Location,
// LocationType, End or Description; for the required fields it is refused.
type ExperiencePatch struct {
	Title          Field[string]
	Organization   Field[string]
	EmploymentType Field[domain.EmploymentType]
	Location       Field[string]
	LocationType   Field[domain.LocationType]
	Start          Field[domain.YearMonth]
	End            Field[domain.YearMonth]
	IsCurrent      Field[bool]
	Description    Field[string]
}

// AddExperience validates in and adds it to the user's experience (FR-4.8). It returns
// the first invalid field's error, in the contract's order, and
// domain.ErrExperienceLimitReached when the user already has MaxExperienceEntries;
// nothing is stored then. It returns domain.ErrNotFound when the account no longer exists.
func (s *Service) AddExperience(ctx context.Context, userID string, in ExperienceInput) (domain.Experience, error) {
	e, err := s.normalizeExperience(domain.Experience{
		UserID:         userID,
		Title:          in.Title,
		Organization:   in.Organization,
		EmploymentType: in.EmploymentType,
		Location:       in.Location,
		LocationType:   in.LocationType,
		Start:          in.Start,
		End:            in.End,
		IsCurrent:      in.IsCurrent,
		Description:    in.Description,
	}, false)
	if err != nil {
		return domain.Experience{}, err
	}
	var saved domain.Experience
	err = s.store.RunInTx(ctx, func(tx domain.Tx) error {
		// Lock first, so the count below cannot change before the entry is stored.
		if err := tx.LockUser(ctx, userID); err != nil {
			return err
		}
		n, err := tx.CountExperience(ctx, userID)
		if err != nil {
			return err
		}
		if n >= MaxExperienceEntries {
			return domain.ErrExperienceLimitReached
		}
		saved, err = tx.InsertExperience(ctx, e)
		return err
	})
	if err != nil {
		return domain.Experience{}, err
	}
	return saved, nil
}

// UpdateExperience changes the fields set in patch on the user's entry id and validates
// the entry as a whole, so isCurrent and the end date must agree and the end cannot end
// up before the start whichever field changes. It never clears the end date by itself.
// It returns domain.ErrNotFound when the user has no such entry, including one that
// belongs to someone else; that check comes before validation, so an invalid patch to
// someone else's entry reveals nothing either.
func (s *Service) UpdateExperience(ctx context.Context, userID, id string, patch ExperiencePatch) (domain.Experience, error) {
	var saved domain.Experience
	err := s.store.RunInTx(ctx, func(tx domain.Tx) error {
		e, err := tx.FindExperienceForUpdate(ctx, userID, id)
		if err != nil {
			return err
		}
		if e, err = s.normalizeExperience(applyExperience(e, patch), patch.IsCurrent.Null); err != nil {
			return err
		}
		saved, err = tx.UpdateExperience(ctx, e)
		return err
	})
	if err != nil {
		return domain.Experience{}, err
	}
	return saved, nil
}

// RemoveExperience deletes the user's entry id. It returns domain.ErrNotFound when the
// user has no such entry, including one that belongs to someone else.
func (s *Service) RemoveExperience(ctx context.Context, userID, id string) error {
	return s.store.DeleteExperience(ctx, userID, id)
}

// applyExperience copies the fields set in p onto e. A null required field becomes the
// empty value, which validation then refuses in the usual field order; a null IsCurrent
// is reported by the caller.
func applyExperience(e domain.Experience, p ExperiencePatch) domain.Experience {
	if p.Title.Set {
		e.Title = p.Title.Value // "" when null
	}
	if p.Organization.Set {
		e.Organization = p.Organization.Value
	}
	if p.EmploymentType.Set {
		e.EmploymentType = p.EmploymentType.Value
	}
	if p.Location.Set {
		e.Location = nullable(p.Location)
	}
	if p.LocationType.Set {
		e.LocationType = nullable(p.LocationType)
	}
	if p.Start.Set {
		e.Start = p.Start.Value // the zero month when null
	}
	if p.End.Set {
		e.End = nullable(p.End)
	}
	if p.IsCurrent.Set && !p.IsCurrent.Null {
		e.IsCurrent = p.IsCurrent.Value
	}
	if p.Description.Set {
		e.Description = nullable(p.Description)
	}
	return e
}

// normalizeExperience trims the text, turns blank optional text into nil and checks each
// field in the contract's order: title, organization, employment type, location,
// location type, start date, end date, isCurrent (null when currentNull), then whether
// isCurrent and the end date agree, the date range and the description.
func (s *Service) normalizeExperience(e domain.Experience, currentNull bool) (domain.Experience, error) {
	e.Title = strings.TrimSpace(e.Title)
	if e.Title == "" || !fits(e.Title, MaxTitleLength, true) {
		return e, domain.ErrInvalidTitle
	}
	e.Organization = strings.TrimSpace(e.Organization)
	if e.Organization == "" || !fits(e.Organization, MaxOrganizationLength, true) {
		return e, domain.ErrInvalidOrganization
	}
	if !employmentTypes[e.EmploymentType] {
		return e, domain.ErrInvalidEmploymentType
	}
	var err error
	if e.Location, err = optional(e.Location, MaxLocationLength, true, domain.ErrInvalidLocation); err != nil {
		return e, err
	}
	if e.LocationType != nil && !locationTypes[*e.LocationType] {
		return e, domain.ErrInvalidLocationType
	}
	now := s.now().UTC()
	latest := domain.YearMonth{Year: now.Year(), Month: int(now.Month())}
	if !inRange(e.Start, latest) {
		return e, domain.ErrInvalidStartDate
	}
	if e.End != nil && !inRange(*e.End, latest) {
		return e, domain.ErrInvalidEndDate
	}
	switch {
	case currentNull:
		return e, domain.ErrInvalidIsCurrent
	case !e.IsCurrent && e.End == nil:
		return e, domain.ErrEndDateRequired
	case e.IsCurrent && e.End != nil:
		return e, domain.ErrEndDateNotAllowed
	case e.End != nil && e.End.Before(e.Start):
		return e, domain.ErrInvalidDateRange
	}
	if e.Description, err = optional(e.Description, MaxExperienceDescriptionLength, false, domain.ErrInvalidDescription); err != nil {
		return e, err
	}
	return e, nil
}

// inRange reports whether m is a real month from January MinExperienceYear to latest.
func inRange(m, latest domain.YearMonth) bool {
	return m.Month >= 1 && m.Month <= 12 && m.Year >= MinExperienceYear && !latest.Before(m)
}
