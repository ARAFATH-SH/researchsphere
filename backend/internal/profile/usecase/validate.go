package usecase

import (
	"strings"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// Input limits (FR-4.1). Lengths count characters after trimming. Migration 000003
// holds the same limits as database constraints.
const (
	MaxFullNameLength    = 200 // the same as at registration
	MaxHeadlineLength    = 220
	MaxBioLength         = 2000
	MaxInstitutionLength = 200
	MaxDepartmentLength  = 200
	MaxJobTitleLength    = 200
)

// normalize trims every field, turns blank optional fields into nil, upper-cases the
// country and checks each field in the contract's order.
func normalize(in UpdateInput) (domain.Profile, error) {
	var p domain.Profile
	name := strings.TrimSpace(in.FullName)
	if name == "" || !fits(name, MaxFullNameLength, true) {
		return p, domain.ErrInvalidFullName
	}
	p.FullName = name

	var err error
	if p.Headline, err = optional(in.Headline, MaxHeadlineLength, true, domain.ErrInvalidHeadline); err != nil {
		return p, err
	}
	if p.Bio, err = optional(in.Bio, MaxBioLength, false, domain.ErrInvalidBio); err != nil {
		return p, err
	}
	if p.Institution, err = optional(in.Institution, MaxInstitutionLength, true, domain.ErrInvalidInstitution); err != nil {
		return p, err
	}
	if p.Department, err = optional(in.Department, MaxDepartmentLength, true, domain.ErrInvalidDepartment); err != nil {
		return p, err
	}
	if p.JobTitle, err = optional(in.JobTitle, MaxJobTitleLength, true, domain.ErrInvalidJobTitle); err != nil {
		return p, err
	}
	if p.Country, err = country(in.Country); err != nil {
		return p, err
	}
	return p, nil
}

// optional trims v and returns nil when it is missing or blank, or invalid when it is
// longer than limit characters or, for a one-line field, has a line break.
func optional(v *string, limit int, oneLine bool, invalid error) (*string, error) {
	if v == nil {
		return nil, nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil, nil
	}
	if !fits(s, limit, oneLine) {
		return nil, invalid
	}
	return &s, nil
}

func fits(s string, limit int, oneLine bool) bool {
	return utf8.RuneCountInString(s) <= limit && (!oneLine || !strings.ContainsAny(s, "\r\n"))
}

// country trims and upper-cases v ("de" becomes "DE"), then requires an assigned
// ISO 3166-1 alpha-2 code.
func country(v *string) (*string, error) {
	if v == nil {
		return nil, nil
	}
	s := strings.ToUpper(strings.TrimSpace(*v))
	if s == "" {
		return nil, nil
	}
	if !countryCodes[s] {
		return nil, domain.ErrInvalidCountry
	}
	return &s, nil
}
