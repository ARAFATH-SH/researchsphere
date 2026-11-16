package usecase

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Field limits (FR-10.1). Lengths are characters after trimming.
const (
	MinTitleLength       = 3
	MaxTitleLength       = 150
	MaxDescriptionLength = 5000
	MaxAbstractLength    = 3000
	MaxSkills            = 20
	// MaxSkillNameLength is the profile tag rule (FR-4.2, decisions.md 2026-10-17).
	MaxSkillNameLength = 60
	MaxReportDetails   = 1000
	MaxResolutionNote  = 1000
)

// cleanText trims s and refuses text the database cannot store (invalid UTF-8, NUL).
func cleanText(s string) (string, bool) {
	s = strings.TrimSpace(s)
	return s, utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

func validTitle(s string) (string, error) {
	s, ok := cleanText(s)
	if n := utf8.RuneCountInString(s); !ok || n < MinTitleLength || n > MaxTitleLength {
		return "", &domain.FieldError{Field: "title"}
	}
	return s, nil
}

func validDescription(s string) (string, error) {
	s, ok := cleanText(s)
	if n := utf8.RuneCountInString(s); !ok || n < 1 || n > MaxDescriptionLength {
		return "", &domain.FieldError{Field: "description"}
	}
	return s, nil
}

// validAbstract returns nil for an absent or empty abstract.
func validAbstract(s *string) (*string, error) {
	if s == nil {
		return nil, nil
	}
	v, ok := cleanText(*s)
	if !ok || utf8.RuneCountInString(v) > MaxAbstractLength {
		return nil, &domain.FieldError{Field: "abstract"}
	}
	if v == "" {
		return nil, nil
	}
	return &v, nil
}

// NormalizeSkillName is the profile's tag rule (profile/usecase.NormalizeTagName, FR-4.2):
// trimmed, every run of whitespace one space, 1 to 60 characters. Modules do not import
// each other, so it is repeated here; TestNormalizeSkillNameMatchesProfileTags keeps the
// two the same.
func NormalizeSkillName(name string) (string, bool) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || !utf8.ValidString(name) || strings.ContainsRune(name, 0) || utf8.RuneCountInString(name) > MaxSkillNameLength {
		return "", false
	}
	return name, true
}

// validSkills normalizes each name, drops later duplicates ignoring letter case and allows
// at most MaxSkills.
func validSkills(names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	seen := make(map[string]bool, len(names))
	for i, raw := range names {
		name, ok := NormalizeSkillName(raw)
		if !ok {
			return nil, &domain.FieldError{Field: "requiredSkills[" + strconv.Itoa(i) + "]"}
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	if len(out) > MaxSkills {
		return nil, &domain.FieldError{Field: "requiredSkills"}
	}
	return out, nil
}

// optionalText trims s; empty means none. Over maxLen, or text the database cannot store,
// is a FieldError for field.
func optionalText(s *string, maxLen int, field string) (*string, error) {
	if s == nil {
		return nil, nil
	}
	v, ok := cleanText(*s)
	if !ok || utf8.RuneCountInString(v) > maxLen {
		return nil, &domain.FieldError{Field: field}
	}
	if v == "" {
		return nil, nil
	}
	return &v, nil
}
