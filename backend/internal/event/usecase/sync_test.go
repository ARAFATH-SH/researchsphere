package usecase

import (
	"context"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
)

// allFields is the fixed list of migration 000039.
var allFields = newCatalog([]domain.Field{
	{Key: "medical_imaging"}, {Key: "explainable_ai"}, {Key: "bangla_nlp"}, {Key: "climate_modelling"},
	{Key: "public_health_epidemiology"}, {Key: "materials_science"}, {Key: "robotics"}, {Key: "agri_remote_sensing"},
	{Key: "renewable_energy"}, {Key: "water_resources"}, {Key: "cybersecurity"}, {Key: "genomics_bioinformatics"},
	{Key: "development_economics"}, {Key: "education_technology"}, {Key: "structural_engineering"},
	{Key: "urban_transport"}, {Key: "marine_fisheries"}, {Key: "wireless_networks"}, {Key: "drug_discovery"},
	{Key: "mental_health"}, {Key: "other"},
})

var syncNow = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

// FR-14.5: the repositories' zones become ours; AoE and UTC-12 are the same instant.
func TestMapZone(t *testing.T) {
	for in, want := range map[string]string{
		"AoE": "AoE", "aoe": "AoE", "": "AoE", "UTC-12": "AoE", "GMT-12": "AoE", "UTC": "UTC", "UTC+0": "UTC",
		"UTC+8": "Etc/GMT-8", "UTC-7": "Etc/GMT+7", "UTC+5:00": "Etc/GMT-5", "PT": "America/Los_Angeles",
		"Asia/Seoul": "Asia/Seoul", "America/New_York": "America/New_York", "UTC+14": "Etc/GMT-14",
	} {
		got, ok := mapZone(in)
		if !ok || got != want {
			t.Errorf("mapZone(%q) = %q, %t; want %q", in, got, ok, want)
		}
		if _, err := LoadZone(got); err != nil {
			t.Errorf("mapZone(%q) = %q, which LoadZone refuses", in, got)
		}
	}
	for _, bad := range []string{"UTC+5:30", "UTC-13", "CEST", "Mars/Olympus", "UTC+15"} {
		if got, ok := mapZone(bad); ok {
			t.Errorf("mapZone(%q) = %q, accepted", bad, got)
		}
	}
	// 2027-01-04 23:59:59 UTC-12 is 2027-01-05 11:59:59 UTC, as AoE.
	loc, _ := LoadZone("Etc/GMT-8")
	got, _ := ParseLocal("2027-01-04T23:59:59", loc)
	if !got.Equal(time.Date(2027, 1, 4, 15, 59, 59, 0, time.UTC)) {
		t.Errorf("UTC+8 deadline = %v", got)
	}
}

// The deadline shown is the next round still open, else the last one; TBD rounds are
// ignored; only TBD rounds say tbd.
func TestPickRound(t *testing.T) {
	rounds := []domain.Round{
		{Deadline: "2026-07-15 23:59:59"}, {Deadline: "TBD"},
		{Deadline: "2026-12-15 23:59:59", AbstractDeadline: "2026-12-08 23:59:59"}, {Deadline: "2027-03-15 23:59:59"},
	}
	r, found, tbd := pickRound(rounds, aoe, syncNow)
	if !found || tbd || r.local != "2026-12-15T23:59:59" || r.abstract != "2026-12-08T23:59:59" {
		t.Fatalf("next round = %+v, %t, %t", r, found, tbd)
	}
	r, found, _ = pickRound(rounds, aoe, time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC))
	if !found || r.local != "2027-03-15T23:59:59" {
		t.Fatalf("after every round = %+v", r)
	}
	if _, found, tbd := pickRound([]domain.Round{{Deadline: "TBD"}, {Deadline: ""}}, aoe, syncNow); found || !tbd {
		t.Fatalf("only TBD: found %t tbd %t", found, tbd)
	}
	if _, found, tbd := pickRound(nil, aoe, syncNow); found || tbd {
		t.Fatalf("no rounds: found %t tbd %t", found, tbd)
	}
}

// Places as the repositories write them (docs/venues-sync.md).
func TestParsePlace(t *testing.T) {
	tests := []struct{ in, attendance, city, country string }{
		{"Seattle, WA, USA", "in_person", "Seattle", "US"},
		{"Los Angeles, CA, USA", "in_person", "Los Angeles", "US"},
		{"San Diego, CA", "in_person", "San Diego", "US"},
		{"Washington, DC, USA", "in_person", "Washington", "US"},
		{"Memorial Auditorium, Stanford, Palo Alto, CA", "in_person", "Palo Alto", "US"},
		{"Berlin, Germany (Hybrid)", "hybrid", "Berlin", "DE"},
		{"San Diego, California, United States (Hybrid)", "hybrid", "San Diego", "US"},
		{"Philadelphia, PA, USA / Online", "hybrid", "Philadelphia", "US"},
		{"Online / Xi'an, China (local event)", "hybrid", "Xi'an", "CN"},
		{"Brussels, Belgium and Online", "hybrid", "Brussels", "BE"},
		{"Vancouver, Canada & Virtual", "hybrid", "Vancouver", "CA"},
		{"Aberdeen, Scotland, UK", "in_person", "Aberdeen", "GB"},
		{"Edinburgh, Scotland", "in_person", "Edinburgh", "GB"},
		{"Kitchner-Waterloo, Ontario, Canada", "in_person", "Kitchner-Waterloo", "CA"},
		{"Seoul Dragon City, Seoul, Republic of Korea", "in_person", "Seoul", "KR"},
		{"Hsinchu, Taiwan， China", "in_person", "Hsinchu", "CN"},
		{"Istanbul, Türkiye", "in_person", "Istanbul", "TR"},
		{"Singapore", "in_person", "Singapore", "SG"},
		{"Singapore EXPO", "in_person", "Singapore EXPO", "SG"},
		{"Mexico City", "in_person", "Mexico City", "MX"},
		{"Hong Kong SAR", "in_person", "Hong Kong SAR", "HK"},
		{"São Paulo, Brazil", "in_person", "São Paulo", "BR"},
		{"Dhaka, Bangladesh", "in_person", "Dhaka", "BD"},
		{"Online", "online", "", ""},
		{"Virtual", "online", "", ""},
	}
	for _, tt := range tests {
		a, city, country, ok := parsePlace(tt.in)
		if !ok || a != tt.attendance || city != tt.city || country != tt.country {
			t.Errorf("parsePlace(%q) = %q %q %q %t; want %q %q %q", tt.in, a, city, country, ok, tt.attendance, tt.city, tt.country)
		}
	}
	for _, bad := range []string{"", "TBD", "Atlantis", "Somewhere, Narnia"} {
		if _, _, _, ok := parsePlace(bad); ok {
			t.Errorf("parsePlace(%q) accepted", bad)
		}
	}
}

// FR-14.3 for imported venues: areas and known series map to research fields, other when
// nothing applies.
func TestConferenceFields(t *testing.T) {
	tests := []struct {
		source domain.Source
		areas  []string
		title  string
		want   []string
	}{
		{domain.SourceCCF, []string{"AI"}, "ICML", []string{"explainable_ai"}},
		{domain.SourceCCF, []string{"AI"}, "ACL", []string{"bangla_nlp", "explainable_ai"}},
		{domain.SourceCCF, []string{"MX"}, "MICCAI", []string{"medical_imaging"}},
		{domain.SourceCCF, []string{"SC"}, "CCS", []string{"cybersecurity"}},
		{domain.SourceCCF, []string{"NW"}, "INFOCOM", []string{"wireless_networks"}},
		{domain.SourceCCF, []string{"DB"}, "SIGMOD", []string{"other"}},
		{domain.SourceCCF, nil, "X", []string{"other"}},
		{domain.SourceHCIDeadlines, []string{"HCI", "GM"}, "CHI", []string{"other"}},
		{domain.SourceHCIDeadlines, []string{"HRI"}, "HRI", []string{"robotics"}},
		{domain.SourceHCIDeadlines, []string{"AI", "HCI"}, "IUI", []string{"explainable_ai"}},
	}
	for _, tt := range tests {
		if got := conferenceFields(tt.source, tt.areas, tt.title); !slices.Equal(got, tt.want) {
			t.Errorf("%s %v %s = %v; want %v", tt.source, tt.areas, tt.title, got, tt.want)
		}
		for _, k := range conferenceFields(tt.source, tt.areas, tt.title) {
			if _, ok := allFields.index[k]; !ok {
				t.Errorf("%s is not a research field", k)
			}
		}
	}
	for _, k := range subfieldFields {
		if _, ok := allFields.index[k]; !ok {
			t.Errorf("subfield table names %s, not a research field", k)
		}
	}
	for _, m := range areaFields {
		for _, k := range m {
			if _, ok := allFields.index[k]; !ok {
				t.Errorf("area table names %s, not a research field", k)
			}
		}
	}
}

func acl27() domain.Edition {
	return domain.Edition{
		ID: "acl27", Title: "ACL", FullName: "Annual Meeting of the Association for Computational Linguistics", Year: 2027,
		Link: "https://2027.aclweb.org/", Areas: []string{"AI"}, Ranks: "CCF A, CORE A*", TimeZone: "UTC-12",
		Rounds: []domain.Round{{Deadline: "2027-01-04 23:59:59"}}, Place: "Kyoto, Japan",
	}
}

// An edition maps to a venue the 24.1 rules accept, with its zone and the instant in UTC.
func TestMapEdition(t *testing.T) {
	c, keep := mapEdition(domain.SourceCCF, acl27(), syncNow, DefaultSyncPastWindow)
	if !keep || len(c.problems) > 0 || c.key != "ccfddl:acl27" {
		t.Fatalf("candidate = %+v, keep %t", c, keep)
	}
	v, problems := validate(c.in, allFields)
	if len(problems) > 0 {
		t.Fatalf("validate: %v", problems)
	}
	if v.TimeZone != "AoE" || !v.SubmissionDeadline.Equal(time.Date(2027, 1, 5, 11, 59, 59, 0, time.UTC)) ||
		v.Attendance != "in_person" || v.City != "Kyoto" || v.Country != "JP" || v.ShortName != "ACL" ||
		!slices.Equal(v.FieldKeys(), []string{"explainable_ai", "bangla_nlp"}) || !strings.Contains(v.Scope, "**Rank:** CCF A, CORE A*") {
		t.Fatalf("venue = %+v", v)
	}

	// An edition whose deadline closed more than 90 days ago is left alone.
	old := acl27()
	old.Rounds = []domain.Round{{Deadline: "2026-08-01 23:59:59"}}
	if _, keep := mapEdition(domain.SourceCCF, old, syncNow, DefaultSyncPastWindow); keep {
		t.Error("an edition closed 4 months ago was kept")
	}
	old.Rounds = []domain.Round{{Deadline: "2026-10-01 23:59:59"}}
	if _, keep := mapEdition(domain.SourceCCF, old, syncNow, DefaultSyncPastWindow); !keep {
		t.Error("an edition closed 2 months ago was dropped (past editions are kept)")
	}

	// Rejects: TBD, an unknown zone, an unknown country, a link that is not https.
	bad := []struct {
		edit  func(*domain.Edition)
		field string
		code  string
	}{
		{func(e *domain.Edition) { e.Rounds = []domain.Round{{Deadline: "TBD"}} }, "submissionDeadline", domain.CodeDeadlineTBD},
		{func(e *domain.Edition) { e.TimeZone = "UTC+5:30" }, "timeZone", domain.CodeInvalidTimeZone},
		{func(e *domain.Edition) { e.Place = "Narnia" }, "country", domain.CodeUnknownCountry},
		{func(e *domain.Edition) { e.Link = "http://2027.aclweb.org/" }, "officialUrl", domain.CodeNotHTTPS},
		{func(e *domain.Edition) { e.ID, e.Title = "", "" }, "sourceKey", domain.CodeInvalid},
	}
	for _, b := range bad {
		e := acl27()
		b.edit(&e)
		c, keep := mapEdition(domain.SourceCCF, e, syncNow, DefaultSyncPastWindow)
		_, problems := validate(c.in, allFields)
		problems = dedupeProblems(append(c.problems, problems...))
		if !keep || !slices.Contains(problems, domain.Problem{Field: b.field, Code: b.code}) {
			t.Errorf("%s %s: keep %t, problems %v", b.field, b.code, keep, problems)
		}
	}
	// A TBD edition of an old year is left alone, not rejected every day.
	e := acl27()
	e.Year, e.Rounds = 2023, []domain.Round{{Deadline: "TBD"}}
	if _, keep := mapEdition(domain.SourceCCF, e, syncNow, DefaultSyncPastWindow); keep {
		t.Error("a TBD edition of 2023 was kept")
	}
	// hci-deadlines gives event dates.
	e = acl27()
	e.StartsOn, e.EndsOn = "2027-08-20", "2027-08-22"
	c, _ = mapEdition(domain.SourceHCIDeadlines, e, syncNow, DefaultSyncPastWindow)
	if v, problems := validate(c.in, allFields); len(problems) > 0 || v.StartsOn.IsZero() || c.key != "hci_deadlines:acl27" {
		t.Errorf("hci edition: %v %+v", problems, v)
	}
}

func journal() domain.Journal {
	return domain.Journal{
		ID: "S196734849", Name: "Scientific Reports", HomepageURL: "http://www.nature.com/srep/index.html", ISSNL: "2045-2322",
		Publisher: "Nature Portfolio",
		Topics: []domain.JournalTopic{
			{Name: "Image analysis", SubfieldID: 1707}, {Name: "Coastal water", SubfieldID: 2312},
			{Name: "Surgery", SubfieldID: 2746}, {Name: "Radiomics", SubfieldID: 2741},
		},
	}
}

// AI-5 for journals: only a journal whose main topic is in a listed subfield is imported;
// its fields come from its top 3 topics; it is rolling, with a scope from its topics.
func TestMapJournal(t *testing.T) {
	c, keep := mapJournal(journal())
	if !keep || c.key != "openalex:S196734849" || !c.in.Rolling || c.in.Type != "journal" ||
		!slices.Equal(c.in.Fields, []string{"explainable_ai", "water_resources"}) ||
		!strings.Contains(c.in.Scope, "**Publisher:** Nature Portfolio") {
		t.Fatalf("candidate = %+v", c)
	}
	off := journal()
	off.Topics[0].SubfieldID = 2705 // Cardiology
	if _, keep := mapJournal(off); keep {
		t.Error("a cardiology journal was kept")
	}
	if _, keep := mapJournal(domain.Journal{ID: "S1", Name: "No topics"}); keep {
		t.Error("a journal without topics was kept")
	}
	c.in.OfficialURL = "https://www.nature.com/srep/index.html"
	v, problems := validate(c.in, allFields)
	if len(problems) > 0 || !v.Rolling || !v.SubmissionDeadline.IsZero() || v.TimeZone != "" {
		t.Fatalf("validate: %v %+v", problems, v)
	}
}

// Rolling venues follow the 24.1 rules too: only a journal, with no zone and no deadline.
func TestValidateRolling(t *testing.T) {
	in := Input{Name: "J", Type: "journal", Rolling: true, Fields: []string{"other"}, OfficialURL: "https://j.example.org", Scope: "S"}
	if _, problems := validate(in, allFields); len(problems) > 0 {
		t.Fatalf("rolling journal: %v", problems)
	}
	conf := in
	conf.Type, conf.Attendance = "conference", "online"
	if _, problems := validate(conf, allFields); !slices.Contains(problems, domain.Problem{Field: "rolling", Code: domain.CodeNotAllowed}) {
		t.Errorf("rolling conference: %v", problems)
	}
	dated := in
	dated.TimeZone, dated.SubmissionDeadline = "UTC", "2027-01-01"
	_, problems := validate(dated, allFields)
	if !slices.Contains(problems, domain.Problem{Field: "timeZone", Code: domain.CodeNotAllowed}) ||
		!slices.Contains(problems, domain.Problem{Field: "submissionDeadline", Code: domain.CodeNotAllowed}) {
		t.Errorf("rolling with a deadline: %v", problems)
	}
	if k, _ := importKey(fileRow{Input: in}); k != "auto:j" {
		t.Errorf("rolling key = %q", k)
	}
	if (domain.Venue{Rolling: true}).Past(syncNow) {
		t.Error("a rolling journal is past")
	}
}

type fakeChecker struct {
	ok    map[string]bool
	calls atomic.Int32
}

func (f *fakeChecker) Check(_ context.Context, u string) domain.LinkCheck {
	f.calls.Add(1)
	return domain.LinkCheck{OK: f.ok[u], FinalStatus: 200, CheckedAt: syncNow}
}

// docs/decisions.md, 2026-11-24: http is upgraded and checked; when https does not answer,
// or there is no homepage, the ISSN portal; a check is reused for 30 days.
func TestResolveLinks(t *testing.T) {
	cands := make([]candidate, 5)
	homepages := []string{
		httpsLink("http://www.nature.com/srep"), httpsLink("https://down.example.org"), "", "", httpsLink("https://cached.example.org"),
	}
	issns := []string{"2045-2322", "1234-567X", "0000-0001", "", "1111-1111"}
	checker := &fakeChecker{ok: map[string]bool{"https://www.nature.com/srep": true}}
	cached := map[string]domain.LinkCheck{"https://cached.example.org": {OK: true, CheckedAt: syncNow.Add(-24 * time.Hour)}}
	fresh := resolveLinks(t.Context(), cands, homepages, issns, cached, checker, syncNow, DefaultSyncLinkRecheck, 2)

	if cands[0].in.OfficialURL != "https://www.nature.com/srep" || cands[0].linkKind != domain.LinkOfficial {
		t.Errorf("upgraded: %+v", cands[0])
	}
	if cands[1].in.OfficialURL != issnPortal+"1234-567X" || cands[1].linkKind != domain.LinkISSNPortal {
		t.Errorf("down: %+v", cands[1])
	}
	if cands[2].in.OfficialURL != issnPortal+"0000-0001" || cands[2].linkKind != domain.LinkISSNPortal {
		t.Errorf("no homepage: %+v", cands[2])
	}
	if !slices.Contains(cands[3].problems, domain.Problem{Field: "officialUrl", Code: domain.CodeRequired}) {
		t.Errorf("neither: %+v", cands[3])
	}
	if cands[4].in.OfficialURL != "https://cached.example.org" || checker.calls.Load() != 2 {
		t.Errorf("cached: %+v after %d checks", cands[4], checker.calls.Load())
	}
	if len(fresh) != 2 {
		t.Errorf("fresh checks = %v", fresh)
	}
	// After 30 days the cached check is made again.
	stale := map[string]domain.LinkCheck{"https://cached.example.org": {OK: true, CheckedAt: syncNow.Add(-31 * 24 * time.Hour)}}
	checker.calls.Store(0)
	resolveLinks(t.Context(), make([]candidate, 1), homepages[4:], issns[4:], stale, checker, syncNow, DefaultSyncLinkRecheck, 2)
	if checker.calls.Load() != 1 {
		t.Errorf("stale check reused")
	}
	// A run that ends keeps no check: its result is not an answer.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if f := resolveLinks(ctx, make([]candidate, 1), homepages[:1], issns[:1], nil, checker, syncNow, DefaultSyncLinkRecheck, 1); len(f) != 0 {
		t.Errorf("cancelled run kept %v", f)
	}
	for in, want := range map[string]string{"ftp://x": "", "HTTP://x.org": "https://x.org", "https://x.org": "https://x.org"} {
		if got := httpsLink(in); got != want {
			t.Errorf("httpsLink(%q) = %q", in, got)
		}
	}
	for in, want := range map[string]bool{"2045-2322": true, "1234-567X": true, "1234567X": false, "1234-56X7": false, "": false} {
		if validISSN(in) != want {
			t.Errorf("validISSN(%q) != %t", in, want)
		}
	}
}

func TestMergeFilesAndSources(t *testing.T) {
	stored := []domain.RepoFile{{Path: "a", Content: "1"}, {Path: "b", Content: "2"}, {Path: "c", Content: "3"}}
	got := mergeFiles(stored, domain.RepoFetch{Changed: true, Files: []domain.RepoFile{{Path: "b", Content: "2'"}, {Path: "d", Content: "4"}}, Deleted: []string{"c"}})
	if !slices.Equal(got, []domain.RepoFile{{Path: "a", Content: "1"}, {Path: "b", Content: "2'"}, {Path: "d", Content: "4"}}) {
		t.Errorf("merge = %v", got)
	}
	if got := mergeFiles(stored, domain.RepoFetch{}); len(got) != 3 {
		t.Errorf("unchanged = %v", got)
	}
	if got := mergeFiles(stored, domain.RepoFetch{Changed: true, Full: true, Files: stored[:1]}); len(got) != 1 {
		t.Errorf("full = %v", got)
	}
	srcs, err := parseSources([]string{"openalex", "ccfddl", "ccfddl"})
	if err != nil || !slices.Equal(srcs, []domain.Source{domain.SourceCCF, domain.SourceOpenAlex}) {
		t.Errorf("parseSources = %v, %v", srcs, err)
	}
	for _, bad := range [][]string{{"manual"}, {"import"}, {"wikicfp"}} {
		if _, err := parseSources(bad); err == nil {
			t.Errorf("parseSources(%v) accepted", bad)
		}
	}
	if editionKey(domain.Edition{Title: "IEEE  VR", Year: 2026}) != editionKey(domain.Edition{Title: "ieee vr", Year: 2026}) {
		t.Error("edition keys differ by case and spaces")
	}
}

// The import file takes the rolling column too (docs/venues-import.md): true, false or empty;
// anything else rejects the row.
func TestParseFileRolling(t *testing.T) {
	rows, err := parseFile("v.csv", []byte("name,type,rolling,fields,officialUrl,scope\n"+
		"Rolling J,journal,TRUE,other,https://r.example.org,S\nDated J,journal,,other,https://d.example.org,S\nBad J,journal,maybe,other,https://b.example.org,S\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !rows[0].Rolling || rows[0].badRolling || rows[1].Rolling || rows[1].badRolling || !rows[2].badRolling {
		t.Fatalf("rows = %+v", rows)
	}
	if _, problems := validate(rows[0].Input, allFields); len(problems) > 0 {
		t.Errorf("rolling row: %v", problems)
	}
	rows, err = parseFile("v.json", []byte(`[{"name":"J","type":"journal","rolling":"true","fields":["other"],"officialUrl":"https://j.example.org","scope":"S"}]`))
	if err != nil || !rows[0].Rolling {
		t.Fatalf("json rolling: %+v %v", rows, err)
	}
}
