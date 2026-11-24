package usecase

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
)

// Conference editions from the deadline repositories, mapped to venues (FR-14.5;
// docs/venues-sync.md).

// candidate is one entry of a source, ready to be checked and written: its key, a name for
// reports, the venue as input and the problems found before validation. skip marks an entry
// another source already has.
type candidate struct {
	key      string
	name     string
	in       Input
	linkKind domain.LinkKind
	problems []domain.Problem
	skip     bool
}

// areaFields maps each source's area codes to research fields. A code not listed is other.
var areaFields = map[domain.Source]map[string]string{
	domain.SourceCCF: {
		"AI": "explainable_ai",
		"NW": "wireless_networks",
		"SC": "cybersecurity",
	},
	domain.SourceHCIDeadlines: {
		"AI":  "explainable_ai",
		"HRI": "robotics",
		"SP":  "cybersecurity",
	},
}

// areaLabels names the areas in the scope text.
var areaLabels = map[domain.Source]map[string]string{
	domain.SourceCCF: {
		"DS": "Computer architecture, parallel programming and storage",
		"NW": "Network systems",
		"SC": "Network and system security",
		"SE": "Software engineering, operating systems and programming languages",
		"DB": "Databases, data mining and information retrieval",
		"CT": "Theory of computing",
		"CG": "Graphics and multimedia",
		"AI": "Artificial intelligence",
		"HI": "Human-computer interaction and pervasive computing",
		"MX": "Interdisciplinary and emerging topics",
	},
	domain.SourceHCIDeadlines: {
		"HCI":  "Human-computer interaction",
		"AI":   "Artificial intelligence",
		"CSCW": "Computer-supported cooperative work",
		"DES":  "Design",
		"HAP":  "Haptics",
		"HRI":  "Human-robot interaction",
		"SP":   "Security and privacy",
		"VIS":  "Visualization",
		"XR":   "Augmented, virtual and mixed reality",
		"GM":   "Graphics and multimedia",
		"ART":  "Art and music",
	},
}

// seriesFields adds fields to conference series the areas place too broadly, by short name
// in lower case.
var seriesFields = map[string][]string{
	"acl":    {"bangla_nlp"},
	"emnlp":  {"bangla_nlp"},
	"naacl":  {"bangla_nlp"},
	"coling": {"bangla_nlp"},
	"eacl":   {"bangla_nlp"},
	"miccai": {"medical_imaging"},
	"isbi":   {"medical_imaging"},
	"ipmi":   {"medical_imaging"},
	"icra":   {"robotics"},
	"iros":   {"robotics"},
	"rss":    {"robotics"},
	"ismb":   {"genomics_bioinformatics"},
	"recomb": {"genomics_bioinformatics"},
}

// sourceLabels names each source in the scope text and the status.
var sourceLabels = map[domain.Source]string{
	domain.SourceCCF:          "ccf-deadlines",
	domain.SourceHCIDeadlines: "hci-deadlines",
	domain.SourceOpenAlex:     "OpenAlex",
}

// conferenceFields maps an edition's areas and series to 1 to 5 research fields: other only
// when nothing else applies.
func conferenceFields(source domain.Source, areas []string, title string) []string {
	var out []string
	add := func(k string) {
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	for _, k := range seriesFields[strings.ToLower(strings.TrimSpace(title))] {
		add(k)
	}
	for _, a := range areas {
		if k, ok := areaFields[source][strings.ToUpper(strings.TrimSpace(a))]; ok {
			add(k)
		}
	}
	if len(out) == 0 {
		return []string{"other"}
	}
	if len(out) > MaxFields {
		out = out[:MaxFields]
	}
	return out
}

// utcOffset is UTC+N or GMT-N with a whole number of hours.
var utcOffset = regexp.MustCompile(`^(?i)(?:UTC|GMT)([+-])(\d{1,2})(?::00)?$`)

// mapZone turns a source's time zone into one of ours: AoE, UTC or an IANA name. UTC-12 is
// AoE, a whole-hour offset becomes its Etc/GMT zone (whose sign is inverted: UTC+8 is
// Etc/GMT-8), PT is America/Los_Angeles. A missing zone is AoE, the repositories' own rule.
// ok is false for anything else.
func mapZone(s string) (string, bool) {
	s = strings.TrimSpace(s)
	switch strings.ToUpper(s) {
	case "", "AOE":
		return ZoneAoE, true
	case "UTC", "GMT", "Z", "UTC+0", "UTC-0", "GMT+0", "GMT-0", "UTC+00:00":
		return ZoneUTC, true
	case "PT", "PST", "PDT":
		return "America/Los_Angeles", true
	case "ET", "EST", "EDT":
		return "America/New_York", true
	}
	if m := utcOffset.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[2])
		switch {
		case n == 0:
			return ZoneUTC, true
		case m[1] == "-" && n == 12:
			return ZoneAoE, true
		case m[1] == "-" && n <= 12:
			return fmt.Sprintf("Etc/GMT+%d", n), true
		case m[1] == "+" && n <= 14:
			return fmt.Sprintf("Etc/GMT-%d", n), true
		}
		return "", false
	}
	if _, err := LoadZone(s); err == nil && s != ZoneAoE {
		return s, true
	}
	return "", false
}

// sourceTime is a source's local time, YYYY-MM-DD HH:MM:SS (or with a T, or without the
// seconds, or a date alone), as our input format.
func sourceTime(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "TBD") || strings.EqualFold(s, "TBA") {
		return "", false
	}
	return strings.Replace(s, " ", "T", 1), true
}

// round is a parsed submission round.
type round struct {
	deadline time.Time
	local    string
	abstract string
}

// pickRound chooses the round to show: the next one whose deadline has not passed, else the
// last one. tbd says that no round has a date.
func pickRound(rounds []domain.Round, loc *time.Location, now time.Time) (r round, found, tbd bool) {
	var parsed []round
	for _, rd := range rounds {
		local, ok := sourceTime(rd.Deadline)
		if !ok {
			continue
		}
		t, err := ParseLocal(local, loc)
		if err != nil {
			continue
		}
		pr := round{deadline: t, local: local}
		if a, ok := sourceTime(rd.AbstractDeadline); ok {
			pr.abstract = a
		}
		parsed = append(parsed, pr)
	}
	if len(parsed) == 0 {
		return round{}, false, len(rounds) > 0
	}
	slices.SortStableFunc(parsed, func(a, b round) int { return a.deadline.Compare(b.deadline) })
	for _, p := range parsed {
		if p.deadline.After(now) {
			return p, true, false
		}
	}
	return parsed[len(parsed)-1], true, false
}

// mapEdition turns an edition into a candidate. keep is false for an edition whose deadline
// closed more than window before now: it is left alone, never deleted (past editions stay).
func mapEdition(source domain.Source, e domain.Edition, now time.Time, window time.Duration) (c candidate, keep bool) {
	bad := func(field, code string) { c.problems = append(c.problems, domain.Problem{Field: field, Code: code}) }
	id := strings.ToLower(strings.TrimSpace(e.ID))
	if id == "" && e.Title != "" && e.Year > 0 {
		id = strings.ToLower(strings.Join(strings.Fields(e.Title), "")) + strconv.Itoa(e.Year)
	}
	c.key = string(source) + ":" + id
	if id == "" || utf8.RuneCountInString(c.key) > MaxSourceKeyChars || hasControl(c.key, false) {
		c.key = ""
		bad("sourceKey", domain.CodeInvalid)
	}

	title := strings.TrimSpace(e.Title)
	name := strings.TrimSpace(e.FullName)
	if name == "" {
		name = title
	}
	c.name = name
	c.in = Input{Name: name, Type: string(domain.TypeConference), OfficialURL: strings.TrimSpace(e.Link)}
	if title != "" && title != name && utf8.RuneCountInString(title) <= MaxShortNameChars {
		c.in.ShortName = title
	}
	c.in.Fields = conferenceFields(source, e.Areas, title)

	zone, ok := mapZone(e.TimeZone)
	var loc *time.Location
	if ok {
		loc, _ = LoadZone(zone)
		c.in.TimeZone = zone
	} else {
		bad("timeZone", domain.CodeInvalidTimeZone)
		loc = aoe
	}
	r, found, tbd := pickRound(e.Rounds, loc, now)
	switch {
	case found:
		if r.deadline.Before(now.Add(-window)) {
			return candidate{}, false
		}
		c.in.SubmissionDeadline = r.local
		c.in.AbstractDeadline = r.abstract
	case tbd && e.Year >= now.Year()-1:
		bad("submissionDeadline", domain.CodeDeadlineTBD)
	case tbd:
		return candidate{}, false
	default:
		bad("submissionDeadline", domain.CodeRequired)
	}

	attendance, city, country, ok := parsePlace(e.Place)
	if ok {
		c.in.Attendance, c.in.City, c.in.Country = attendance, city, country
	} else {
		// The rule would ask for a place: say why there is none.
		c.in.Attendance = string(domain.AttendInPerson)
		bad("country", domain.CodeUnknownCountry)
	}
	if _, err := parseDate(strings.TrimSpace(e.StartsOn)); err == nil {
		c.in.StartsOn = strings.TrimSpace(e.StartsOn)
		if _, err := parseDate(strings.TrimSpace(e.EndsOn)); err == nil {
			c.in.EndsOn = strings.TrimSpace(e.EndsOn)
		}
	}
	c.in.Scope = conferenceScope(source, e, name, title)
	return c, true
}

// conferenceScope is the scope text of an edition: what the source says about it.
func conferenceScope(source domain.Source, e domain.Edition, name, title string) string {
	var b strings.Builder
	b.WriteString(name)
	if title != "" && title != name {
		b.WriteString(" (" + title + ")")
	}
	if e.Year > 0 {
		fmt.Fprintf(&b, ", %d", e.Year)
	}
	b.WriteString(".")
	var labels []string
	for _, a := range e.Areas {
		if l, ok := areaLabels[source][strings.ToUpper(strings.TrimSpace(a))]; ok && !slices.Contains(labels, l) {
			labels = append(labels, l)
		}
	}
	if len(labels) > 0 {
		b.WriteString("\n\n**Area:** " + strings.Join(labels, "; ") + ".")
	}
	if r := strings.TrimSpace(e.Ranks); r != "" {
		b.WriteString("\n\n**Rank:** " + r + ".")
	}
	if n := len(e.Rounds); n > 1 {
		fmt.Fprintf(&b, "\n\nSubmissions in %d rounds; the deadline shown is the next one.", n)
	}
	b.WriteString("\n\nImported from " + sourceLabels[source] + ".")
	return strings.Map(func(r rune) rune {
		if r == '\r' || (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
			return -1
		}
		return r
	}, b.String())
}

// Markers of a place that is both in person and online, or online only.
var (
	placeHybridParen = regexp.MustCompile(`(?i)\s*\((hybrid|online|virtual)\)`)
	placeHybridJoin  = regexp.MustCompile(`(?i)\s*(/|&|\band\b)\s*(online|virtual|hybrid)\b\s*`)
	placeHybridLead  = regexp.MustCompile(`(?i)^\s*(online|virtual)\s*(/|&|\band\b)\s*`)
	placeNote        = regexp.MustCompile(`(?i)\s*\((local event|tbd|tba)\)`)
)

// parsePlace reads a place such as "Seattle, WA, USA", "Berlin, Germany (Hybrid)" or
// "Online": the attendance, and for an event in person or hybrid its city and ISO country.
// ok is false when the country is not known.
func parsePlace(place string) (attendance, city, country string, ok bool) {
	s := strings.TrimSpace(strings.ReplaceAll(place, "，", ","))
	switch strings.ToLower(s) {
	case "", "tbd", "tba", "n/a":
		return "", "", "", false
	case "online", "virtual", "online only", "virtual only", "virtual conference", "online conference":
		return string(domain.AttendOnline), "", "", true
	}
	attendance = string(domain.AttendInPerson)
	s = placeNote.ReplaceAllString(s, "")
	for _, re := range []*regexp.Regexp{placeHybridParen, placeHybridJoin, placeHybridLead} {
		if re.MatchString(s) {
			attendance = string(domain.AttendHybrid)
			s = re.ReplaceAllString(s, "")
		}
	}
	var parts []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return "", "", "", false
	}
	last := len(parts) - 1
	lastLower := strings.ToLower(parts[last])
	cityAt := last - 1
	switch {
	case countryCodes[lastLower] != "":
		country = countryCodes[lastLower]
	case len(parts) > 1 && usStates[lastLower]:
		country = "US"
	default:
		// "Singapore EXPO", "Mexico City", "Hong Kong Disneyland": a country name the place
		// starts with, the longest one.
		best := ""
		for name := range countryCodes {
			if len(name) > len(best) && strings.HasPrefix(lastLower, name+" ") {
				best = name
			}
		}
		if best == "" {
			return "", "", "", false
		}
		country = countryCodes[best]
		cityAt = last
	}
	if cityAt < 0 {
		// A city-state, or only a country: the place itself.
		cityAt = last
	}
	// Skip a state or region between the city and the country ("Los Angeles, CA, USA",
	// "Aberdeen, Scotland, UK"); the first part is the city whatever its name
	// ("Washington, DC, USA").
	for cityAt > 0 {
		l := strings.ToLower(parts[cityAt])
		if !usStates[l] && !regions[l] && countryCodes[l] == "" {
			break
		}
		cityAt--
	}
	city = parts[cityAt]
	if utf8.RuneCountInString(city) > MaxCityChars {
		city = string([]rune(city)[:MaxCityChars])
	}
	return attendance, strings.TrimSpace(city), country, true
}
