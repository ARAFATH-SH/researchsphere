package usecase

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
)

// Limits of a venue (VenueInput in docs/openapi.yaml). The database checks the same.
const (
	MaxNameChars      = 300
	MaxShortNameChars = 40
	MaxCityChars      = 100
	MaxScopeChars     = 10_000
	MaxURLChars       = 2048
	MaxFields         = 5
	MaxSourceKeyChars = 200
)

// Input is a venue as an administrator or a file gives it: text as sent, "" for absent.
// Deadlines are local times in TimeZone.
type Input struct {
	Name               string
	ShortName          string
	Type               string
	Fields             []string
	Attendance         string
	City               string
	Country            string
	StartsOn           string
	EndsOn             string
	TimeZone           string
	SubmissionDeadline string
	AbstractDeadline   string
	NotificationAt     string
	OfficialURL        string
	Scope              string
}

var countryCode = regexp.MustCompile(`^[A-Z]{2}$`)

// Validate checks a venue by the rules Create uses and returns it with its instants, for a
// command that writes venues without a request (the sample data of cmd/seedsample). fields
// is the fixed list of research fields. It returns an InvalidError listing every problem.
func Validate(in Input, fields []domain.Field) (domain.Venue, error) {
	v, problems := validate(in, newCatalog(fields))
	if len(problems) > 0 {
		return domain.Venue{}, &domain.InvalidError{Problems: problems}
	}
	return v, nil
}

// catalog is the fixed list of research fields with each key's place in it.
type catalog struct {
	list  []domain.Field
	index map[string]int
}

func newCatalog(fields []domain.Field) catalog {
	c := catalog{list: fields, index: make(map[string]int, len(fields))}
	for i, f := range fields {
		c.index[f.Key] = i
	}
	return c
}

// validate checks in against every rule and returns the venue it describes, or every
// problem in the order of the fields.
func validate(in Input, known catalog) (domain.Venue, []domain.Problem) {
	var (
		v        domain.Venue
		problems []domain.Problem
	)
	bad := func(field, code string) { problems = append(problems, domain.Problem{Field: field, Code: code}) }

	v.Name = strings.TrimSpace(in.Name)
	switch {
	case v.Name == "":
		bad("name", domain.CodeRequired)
	case utf8.RuneCountInString(v.Name) > MaxNameChars:
		bad("name", domain.CodeTooLong)
	case hasControl(v.Name, false):
		bad("name", domain.CodeInvalid)
	}

	v.ShortName = strings.TrimSpace(in.ShortName)
	switch {
	case utf8.RuneCountInString(v.ShortName) > MaxShortNameChars:
		bad("shortName", domain.CodeTooLong)
	case hasControl(v.ShortName, false):
		bad("shortName", domain.CodeInvalid)
	}

	v.Type = domain.VenueType(strings.TrimSpace(in.Type))
	typeOK := false
	for _, t := range domain.VenueTypes {
		typeOK = typeOK || v.Type == t
	}
	switch {
	case v.Type == "":
		bad("type", domain.CodeRequired)
	case !typeOK:
		bad("type", domain.CodeInvalid)
	}

	v.Fields, problems = checkFields(in.Fields, known, problems)

	problems = checkPlace(&v, in, typeOK, problems)

	v.TimeZone = strings.TrimSpace(in.TimeZone)
	var loc *time.Location
	if v.TimeZone == "" {
		bad("timeZone", domain.CodeRequired)
	} else if l, err := LoadZone(v.TimeZone); err != nil {
		bad("timeZone", domain.CodeInvalidTimeZone)
	} else {
		loc = l
	}
	problems = checkDeadlines(&v, in, loc, problems)

	v.OfficialURL = strings.TrimSpace(in.OfficialURL)
	if code := checkURL(v.OfficialURL); code != "" {
		bad("officialUrl", code)
	}

	// Line breaks are kept as \n alone: the database refuses \r.
	v.Scope = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(in.Scope, "\r\n", "\n"), "\r", "\n"))
	switch {
	case v.Scope == "":
		bad("scope", domain.CodeRequired)
	case utf8.RuneCountInString(v.Scope) > MaxScopeChars:
		bad("scope", domain.CodeTooLong)
	case hasControl(v.Scope, true):
		bad("scope", domain.CodeInvalid)
	}
	return v, problems
}

// checkFields keeps the fields in the order of the fixed list.
func checkFields(keys []string, known catalog, problems []domain.Problem) ([]domain.Field, []domain.Problem) {
	bad := func(code string) []domain.Problem {
		return append(problems, domain.Problem{Field: "fields", Code: code})
	}
	seen := map[string]bool{}
	var picked []int
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		i, ok := known.index[k]
		if !ok {
			return nil, bad(domain.CodeUnknownField)
		}
		if seen[k] {
			return nil, bad(domain.CodeDuplicateField)
		}
		seen[k] = true
		picked = append(picked, i)
	}
	switch {
	case len(picked) == 0:
		return nil, bad(domain.CodeRequired)
	case len(picked) > MaxFields:
		return nil, bad(domain.CodeTooManyFields)
	}
	slices.Sort(picked)
	out := make([]domain.Field, len(picked))
	for j, i := range picked {
		out[j] = known.list[i]
	}
	return out, problems
}

// checkPlace checks attendance, city, country and event dates: only an event has them, and a
// place is needed unless it is online only. When the type is not valid, only their formats
// are checked.
func checkPlace(v *domain.Venue, in Input, typeOK bool, problems []domain.Problem) []domain.Problem {
	bad := func(field, code string) { problems = append(problems, domain.Problem{Field: field, Code: code}) }
	event := v.Type.IsEvent()
	notEvent := typeOK && !event

	v.Attendance = domain.Attendance(strings.TrimSpace(in.Attendance))
	switch v.Attendance {
	case "":
		if event {
			bad("attendance", domain.CodeRequired)
		}
	case domain.AttendInPerson, domain.AttendOnline, domain.AttendHybrid:
		if notEvent {
			bad("attendance", domain.CodeNotAllowed)
		}
	default:
		bad("attendance", domain.CodeInvalid)
	}
	needPlace := v.Attendance == domain.AttendInPerson || v.Attendance == domain.AttendHybrid
	placeRefused := notEvent || v.Attendance == domain.AttendOnline

	v.City = strings.TrimSpace(in.City)
	switch {
	case v.City == "":
		if event && needPlace {
			bad("city", domain.CodeRequired)
		}
	case placeRefused:
		bad("city", domain.CodeNotAllowed)
	case utf8.RuneCountInString(v.City) > MaxCityChars:
		bad("city", domain.CodeTooLong)
	case hasControl(v.City, false):
		bad("city", domain.CodeInvalid)
	}

	v.Country = strings.ToUpper(strings.TrimSpace(in.Country))
	switch {
	case v.Country == "":
		if event && needPlace {
			bad("country", domain.CodeRequired)
		}
	case placeRefused:
		bad("country", domain.CodeNotAllowed)
	case !countryCode.MatchString(v.Country):
		bad("country", domain.CodeInvalid)
	}

	for _, d := range []struct {
		field, value string
		dst          *time.Time
	}{{"startsOn", in.StartsOn, &v.StartsOn}, {"endsOn", in.EndsOn, &v.EndsOn}} {
		s := strings.TrimSpace(d.value)
		if s == "" {
			continue
		}
		if notEvent {
			bad(d.field, domain.CodeNotAllowed)
			continue
		}
		t, err := parseDate(s)
		if err != nil {
			bad(d.field, domain.CodeInvalidDate)
			continue
		}
		*d.dst = t
	}
	if !v.StartsOn.IsZero() && !v.EndsOn.IsZero() && v.EndsOn.Before(v.StartsOn) {
		bad("endsOn", domain.CodeEndsBeforeStarts)
	}
	return problems
}

// checkDeadlines reads the deadlines in loc (nil when the zone is not valid: then only
// whether a deadline is present is checked).
func checkDeadlines(v *domain.Venue, in Input, loc *time.Location, problems []domain.Problem) []domain.Problem {
	bad := func(field, code string) { problems = append(problems, domain.Problem{Field: field, Code: code}) }
	read := func(field, value string, required bool) time.Time {
		s := strings.TrimSpace(value)
		if s == "" {
			if required {
				bad(field, domain.CodeRequired)
			}
			return time.Time{}
		}
		if loc == nil {
			return time.Time{}
		}
		t, err := ParseLocal(s, loc)
		if err != nil {
			bad(field, domain.CodeInvalidDate)
			return time.Time{}
		}
		return t.UTC()
	}
	v.SubmissionDeadline = read("submissionDeadline", in.SubmissionDeadline, true)
	v.AbstractDeadline = read("abstractDeadline", in.AbstractDeadline, false)
	v.NotificationAt = read("notificationAt", in.NotificationAt, false)
	if v.SubmissionDeadline.IsZero() {
		return problems
	}
	if !v.AbstractDeadline.IsZero() && v.AbstractDeadline.After(v.SubmissionDeadline) {
		bad("abstractDeadline", domain.CodeAfterDeadline)
	}
	if !v.NotificationAt.IsZero() && v.NotificationAt.Before(v.SubmissionDeadline) {
		bad("notificationAt", domain.CodeBeforeDeadline)
	}
	return problems
}

// checkURL returns the problem with an official link, "" when it is a valid https link: the
// scheme is exactly https, there is a host and no user name, and no space or control
// character (FR-15.1; NFR-8.2: never javascript: or data:).
func checkURL(s string) string {
	const scheme = "https://"
	switch {
	case s == "":
		return domain.CodeRequired
	case len(s) > MaxURLChars:
		return domain.CodeTooLong
	case !strings.HasPrefix(s, scheme):
		return domain.CodeNotHTTPS
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == utf8.RuneError {
			return domain.CodeInvalid
		}
	}
	rest := s[len(scheme):]
	authority := rest
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		authority = rest[:i]
	}
	if authority == "" || strings.Contains(authority, "@") {
		return domain.CodeInvalid
	}
	if u, err := url.Parse(s); err != nil || u.Host == "" || u.User != nil {
		return domain.CodeInvalid
	}
	return ""
}

// hasControl reports a control character; with text, line breaks and tabs are allowed.
func hasControl(s string, text bool) bool {
	for _, r := range s {
		if text && (r == '\n' || r == '\t') {
			continue
		}
		if unicode.IsControl(r) || r == utf8.RuneError {
			return true
		}
	}
	return false
}
