package usecase

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// Time zones of venues (docs/decisions.md, 2026-11-24).
//
// A deadline is stored as an instant with the label it was stated in. The labels are AoE,
// UTC or an IANA zone name. AoE ("anywhere on Earth") is UTC-12 all year: the deadline has
// passed nowhere on Earth until that instant. A deadline given as a date alone means
// 23:59:59 that day in its zone, which is what a call for papers means by "1 March AoE".
// A local time that does not exist or exists twice on a daylight saving change is read the
// way Go's time.Date reads it.

// Zone labels.
const (
	ZoneAoE = "AoE"
	ZoneUTC = "UTC"
	// DefaultViewZone is the zone the list's deadline range is read in when the request names
	// none: most users are in Bangladesh.
	DefaultViewZone = "Asia/Dhaka"
)

// aoe is UTC-12 with no daylight saving.
var aoe = time.FixedZone(ZoneAoE, -12*60*60)

// ianaName is the shape of an IANA zone name such as Asia/Dhaka or America/Argentina/Salta.
// The same shape is checked by the venues_deadline_tz constraint.
var ianaName = regexp.MustCompile(`^[A-Za-z][A-Za-z_-]*(/[A-Za-z0-9_+-]+){1,2}$`)

var errZone = errors.New("unknown time zone")

// LoadZone returns the location of a label: AoE, UTC or an IANA zone name.
func LoadZone(label string) (*time.Location, error) {
	switch label {
	case ZoneAoE:
		return aoe, nil
	case ZoneUTC:
		return time.UTC, nil
	}
	if !ianaName.MatchString(label) {
		return nil, errZone
	}
	loc, err := time.LoadLocation(label)
	if err != nil {
		return nil, errZone
	}
	return loc, nil
}

// loadViewZone returns the location of a view zone: an IANA name or UTC, the default when
// empty.
func loadViewZone(label string) (*time.Location, error) {
	if label == "" {
		label = DefaultViewZone
	}
	if label == ZoneAoE {
		return nil, errZone
	}
	return LoadZone(label)
}

// Layouts of local times and dates.
const (
	dateLayout    = "2006-01-02"
	minuteLayout  = "2006-01-02T15:04"
	secondLayout  = "2006-01-02T15:04:05"
	maxLocalChars = len(secondLayout)
)

var errDate = errors.New("invalid date")

// ParseLocal reads a local time in loc: YYYY-MM-DD (meaning 23:59:59 that day),
// YYYY-MM-DDTHH:MM or YYYY-MM-DDTHH:MM:SS.
func ParseLocal(s string, loc *time.Location) (time.Time, error) {
	if len(s) > maxLocalChars {
		return time.Time{}, errDate
	}
	if d, err := time.ParseInLocation(dateLayout, s, loc); err == nil {
		return time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 0, loc), nil
	}
	for _, layout := range []string{minuteLayout, secondLayout} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errDate
}

// FormatLocal is the wall time of t in loc, as YYYY-MM-DDTHH:MM:SS.
func FormatLocal(t time.Time, loc *time.Location) string {
	return t.In(loc).Format(secondLayout)
}

// parseDate reads a calendar date, YYYY-MM-DD, as midnight UTC.
func parseDate(s string) (time.Time, error) {
	if len(s) != len(dateLayout) {
		return time.Time{}, errDate
	}
	d, err := time.Parse(dateLayout, s)
	if err != nil {
		return time.Time{}, errDate
	}
	return d, nil
}

// FormatDate is a date as YYYY-MM-DD.
func FormatDate(d time.Time) string { return d.UTC().Format(dateLayout) }

// dayWindow turns the list's inclusive calendar dates into instants in loc: from becomes
// that day's local midnight, to the next day's local midnight (exclusive). An empty value
// gives the zero time.
func dayWindow(from, to string, loc *time.Location) (start, end time.Time, field string, err error) {
	if from = strings.TrimSpace(from); from != "" {
		d, err := parseDate(from)
		if err != nil {
			return time.Time{}, time.Time{}, "deadlineFrom", err
		}
		start = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
	}
	if to = strings.TrimSpace(to); to != "" {
		d, err := parseDate(to)
		if err != nil {
			return time.Time{}, time.Time{}, "deadlineTo", err
		}
		end = time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, loc)
	}
	if !start.IsZero() && !end.IsZero() && !end.After(start) {
		return time.Time{}, time.Time{}, "deadlineTo", errDate
	}
	return start, end, "", nil
}
