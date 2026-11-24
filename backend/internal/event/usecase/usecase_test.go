package usecase

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
)

var testCatalog = newCatalog([]domain.Field{
	{Key: "explainable_ai", Label: "AI"}, {Key: "bangla_nlp", Label: "NLP"}, {Key: "robotics", Label: "Robotics"},
	{Key: "water_resources", Label: "Water"}, {Key: "cybersecurity", Label: "Security"}, {Key: "mental_health", Label: "Mind"},
	{Key: "other", Label: "Other"},
})

// FR-14.1: deadlines are local times in AoE, UTC or an IANA zone; a date alone is 23:59:59.
func TestParseLocal(t *testing.T) {
	dhaka, _ := LoadZone("Asia/Dhaka")
	aoe, _ := LoadZone("AoE")
	tests := []struct {
		in   string
		loc  *time.Location
		want time.Time
	}{
		{"2027-03-01", aoe, time.Date(2027, 3, 2, 11, 59, 59, 0, time.UTC)},
		{"2027-03-01T23:59", aoe, time.Date(2027, 3, 2, 11, 59, 0, 0, time.UTC)},
		{"2027-03-01T08:00:30", dhaka, time.Date(2027, 3, 1, 2, 0, 30, 0, time.UTC)},
		{"2027-03-01", time.UTC, time.Date(2027, 3, 1, 23, 59, 59, 0, time.UTC)},
	}
	for _, tt := range tests {
		got, err := ParseLocal(tt.in, tt.loc)
		if err != nil || !got.Equal(tt.want) {
			t.Errorf("ParseLocal(%q, %s) = %v, %v; want %v", tt.in, tt.loc, got, err, tt.want)
		}
	}
	for _, bad := range []string{"", "1 March 2027", "2027-02-30", "2027-03-01T24:00", "2027-03-01T23:59Z", "2027-03-01 23:59", "02027-03-01"} {
		if _, err := ParseLocal(bad, time.UTC); err == nil {
			t.Errorf("ParseLocal(%q) accepted", bad)
		}
	}
	if got := FormatLocal(time.Date(2027, 3, 2, 11, 59, 59, 0, time.UTC), aoe); got != "2027-03-01T23:59:59" {
		t.Errorf("FormatLocal = %s", got)
	}
}

func TestLoadZone(t *testing.T) {
	for _, ok := range []string{"AoE", "UTC", "Asia/Dhaka", "Europe/Berlin", "America/Argentina/Salta", "Etc/GMT+12"} {
		if _, err := LoadZone(ok); err != nil {
			t.Errorf("LoadZone(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"", "aoe", "Local", "Dhaka", "Mars/Olympus", "../etc/passwd", "Asia/Dhaka/../x", "UTC+6"} {
		if _, err := LoadZone(bad); err == nil {
			t.Errorf("LoadZone(%q) accepted", bad)
		}
	}
	if aoe, _ := LoadZone("AoE"); time.Date(2027, 7, 1, 0, 0, 0, 0, aoe).UTC().Hour() != 12 {
		t.Error("AoE is UTC-12 all year")
	}
}

// The list's dates are whole days in the view zone, Asia/Dhaka unless named.
func TestDayWindow(t *testing.T) {
	dhaka, _ := loadViewZone("")
	from, to, _, err := dayWindow("2027-03-02", "2027-03-02", dhaka)
	if err != nil || !from.Equal(time.Date(2027, 3, 1, 18, 0, 0, 0, time.UTC)) || !to.Equal(time.Date(2027, 3, 2, 18, 0, 0, 0, time.UTC)) {
		t.Errorf("Dhaka day = %v to %v, %v", from, to, err)
	}
	if _, _, field, err := dayWindow("2027-03-03", "2027-03-02", dhaka); err == nil || field != "deadlineTo" {
		t.Errorf("to before from: %s %v", field, err)
	}
	if _, err := loadViewZone("AoE"); err == nil {
		t.Error("AoE is a deadline label, not a view zone")
	}
}

func validInput() Input {
	return Input{
		Name: " Conference on Graphs ", ShortName: "CoG", Type: "conference", Fields: []string{"robotics", "explainable_ai"},
		Attendance: "in_person", City: "Dhaka", Country: "bd", StartsOn: "2027-06-01", EndsOn: "2027-06-03",
		TimeZone: "AoE", SubmissionDeadline: "2027-03-01", AbstractDeadline: "2027-02-20", NotificationAt: "2027-04-15",
		OfficialURL: "https://cog.example.org/cfp?year=2027#dates", Scope: "Graphs.\r\n\r\n- **GNNs**\n- Tabs\tok",
	}
}

func TestValidateAccepts(t *testing.T) {
	v, problems := validate(validInput(), testCatalog)
	if len(problems) != 0 {
		t.Fatalf("problems = %+v", problems)
	}
	if v.Name != "Conference on Graphs" || v.Country != "BD" || v.Scope != "Graphs.\n\n- **GNNs**\n- Tabs\tok" ||
		!slices.Equal(v.FieldKeys(), []string{"explainable_ai", "robotics"}) {
		t.Errorf("venue = %+v", v)
	}
	j := Input{Name: "Journal", Type: "journal", Fields: []string{"other"}, TimeZone: "UTC", SubmissionDeadline: "2027-01-01",
		OfficialURL: "https://j.example.org", Scope: "x"}
	if _, problems := validate(j, testCatalog); len(problems) != 0 {
		t.Errorf("journal: %+v", problems)
	}
	online := Input{Name: "W", Type: "workshop", Fields: []string{"other"}, Attendance: "online", TimeZone: "UTC",
		SubmissionDeadline: "2027-01-01", OfficialURL: "https://w.example.org", Scope: "x"}
	if _, problems := validate(online, testCatalog); len(problems) != 0 {
		t.Errorf("online workshop: %+v", problems)
	}
}

// Every rule of VenueInput, each with the field and code it reports.
func TestValidateRefuses(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Input)
		want   domain.Problem
	}{
		{"no name", func(in *Input) { in.Name = "  " }, domain.Problem{Field: "name", Code: domain.CodeRequired}},
		{"long name", func(in *Input) { in.Name = strings.Repeat("a", 301) }, domain.Problem{Field: "name", Code: domain.CodeTooLong}},
		{"name on two lines", func(in *Input) { in.Name = "A\nB" }, domain.Problem{Field: "name", Code: domain.CodeInvalid}},
		{"long short name", func(in *Input) { in.ShortName = strings.Repeat("A", 41) }, domain.Problem{Field: "shortName", Code: domain.CodeTooLong}},
		{"unknown type", func(in *Input) { in.Type = "seminar" }, domain.Problem{Field: "type", Code: domain.CodeInvalid}},
		{"no field", func(in *Input) { in.Fields = nil }, domain.Problem{Field: "fields", Code: domain.CodeRequired}},
		{"unknown field", func(in *Input) { in.Fields = []string{"astrology"} }, domain.Problem{Field: "fields", Code: domain.CodeUnknownField}},
		{"field twice", func(in *Input) { in.Fields = []string{"robotics", "robotics"} }, domain.Problem{Field: "fields", Code: domain.CodeDuplicateField}},
		{"six fields", func(in *Input) {
			in.Fields = []string{"explainable_ai", "bangla_nlp", "robotics", "water_resources", "cybersecurity", "mental_health"}
		}, domain.Problem{Field: "fields", Code: domain.CodeTooManyFields}},
		{"event without attendance", func(in *Input) { in.Attendance = "" }, domain.Problem{Field: "attendance", Code: domain.CodeRequired}},
		{"bad attendance", func(in *Input) { in.Attendance = "remote" }, domain.Problem{Field: "attendance", Code: domain.CodeInvalid}},
		{"journal with attendance", func(in *Input) { in.Type, in.City, in.Country, in.StartsOn, in.EndsOn = "journal", "", "", "", "" },
			domain.Problem{Field: "attendance", Code: domain.CodeNotAllowed}},
		{"journal with dates", func(in *Input) { in.Type, in.Attendance, in.City, in.Country, in.EndsOn = "journal", "", "", "", "" },
			domain.Problem{Field: "startsOn", Code: domain.CodeNotAllowed}},
		{"in person without a city", func(in *Input) { in.City = "" }, domain.Problem{Field: "city", Code: domain.CodeRequired}},
		{"hybrid without a country", func(in *Input) { in.Attendance, in.Country = "hybrid", "" }, domain.Problem{Field: "country", Code: domain.CodeRequired}},
		{"online with a city", func(in *Input) { in.Attendance, in.Country = "online", "" }, domain.Problem{Field: "city", Code: domain.CodeNotAllowed}},
		{"bad country", func(in *Input) { in.Country = "BGD" }, domain.Problem{Field: "country", Code: domain.CodeInvalid}},
		{"bad start", func(in *Input) { in.StartsOn = "June" }, domain.Problem{Field: "startsOn", Code: domain.CodeInvalidDate}},
		{"ends first", func(in *Input) { in.EndsOn = "2027-05-31" }, domain.Problem{Field: "endsOn", Code: domain.CodeEndsBeforeStarts}},
		{"no zone", func(in *Input) { in.TimeZone = "" }, domain.Problem{Field: "timeZone", Code: domain.CodeRequired}},
		{"bad zone", func(in *Input) { in.TimeZone = "GMT+6" }, domain.Problem{Field: "timeZone", Code: domain.CodeInvalidTimeZone}},
		{"no deadline", func(in *Input) { in.SubmissionDeadline = "" }, domain.Problem{Field: "submissionDeadline", Code: domain.CodeRequired}},
		{"bad deadline", func(in *Input) { in.SubmissionDeadline = "2027-13-01" }, domain.Problem{Field: "submissionDeadline", Code: domain.CodeInvalidDate}},
		{"abstract after", func(in *Input) { in.AbstractDeadline = "2027-03-02" }, domain.Problem{Field: "abstractDeadline", Code: domain.CodeAfterDeadline}},
		{"notification before", func(in *Input) { in.NotificationAt = "2027-02-01" }, domain.Problem{Field: "notificationAt", Code: domain.CodeBeforeDeadline}},
		{"no link", func(in *Input) { in.OfficialURL = "" }, domain.Problem{Field: "officialUrl", Code: domain.CodeRequired}},
		{"http link", func(in *Input) { in.OfficialURL = "http://cog.example.org" }, domain.Problem{Field: "officialUrl", Code: domain.CodeNotHTTPS}},
		{"upper-case scheme", func(in *Input) { in.OfficialURL = "HTTPS://cog.example.org" }, domain.Problem{Field: "officialUrl", Code: domain.CodeNotHTTPS}},
		{"javascript link", func(in *Input) { in.OfficialURL = "javascript:alert(1)" }, domain.Problem{Field: "officialUrl", Code: domain.CodeNotHTTPS}},
		{"no host", func(in *Input) { in.OfficialURL = "https:///path" }, domain.Problem{Field: "officialUrl", Code: domain.CodeInvalid}},
		{"user name", func(in *Input) { in.OfficialURL = "https://evil@cog.example.org" }, domain.Problem{Field: "officialUrl", Code: domain.CodeInvalid}},
		{"space in link", func(in *Input) { in.OfficialURL = "https://cog.example.org/a b" }, domain.Problem{Field: "officialUrl", Code: domain.CodeInvalid}},
		{"long link", func(in *Input) { in.OfficialURL = "https://x.org/" + strings.Repeat("a", 2040) }, domain.Problem{Field: "officialUrl", Code: domain.CodeTooLong}},
		{"no scope", func(in *Input) { in.Scope = "\n" }, domain.Problem{Field: "scope", Code: domain.CodeRequired}},
		{"long scope", func(in *Input) { in.Scope = strings.Repeat("জ", 10001) }, domain.Problem{Field: "scope", Code: domain.CodeTooLong}},
		{"control in scope", func(in *Input) { in.Scope = "a\x00b" }, domain.Problem{Field: "scope", Code: domain.CodeInvalid}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := validInput()
			tt.change(&in)
			_, problems := validate(in, testCatalog)
			if !slices.Contains(problems, tt.want) {
				t.Errorf("problems = %+v, want %+v", problems, tt.want)
			}
		})
	}
	// A scope of exactly 10,000 Bangla characters fits.
	in := validInput()
	in.Scope = strings.Repeat("জ", 10000)
	if _, problems := validate(in, testCatalog); len(problems) != 0 {
		t.Errorf("10,000 characters: %+v", problems)
	}
}

func TestImportKey(t *testing.T) {
	tests := []struct {
		row  fileRow
		want string
	}{
		{fileRow{SourceKey: " cfp-1 ", Input: Input{Name: "X"}}, "cfp-1"},
		{fileRow{Input: Input{Name: "  Journal   of FLOODS ", SubmissionDeadline: "2027-04-01T17:00"}}, "auto:journal of floods:2027"},
		{fileRow{Input: Input{Name: "Conf", StartsOn: "2028-01-05", SubmissionDeadline: "2027-10-01"}}, "auto:conf:2028"},
		{fileRow{Input: Input{Name: "", SubmissionDeadline: "2027-10-01"}}, ""},
		{fileRow{Input: Input{Name: "No date"}}, ""},
	}
	for _, tt := range tests {
		if got, p := importKey(tt.row); got != tt.want || p != nil {
			t.Errorf("importKey(%+v) = %q, %v; want %q", tt.row, got, p, tt.want)
		}
	}
	if _, p := importKey(fileRow{SourceKey: strings.Repeat("k", 201)}); p == nil || p.Code != domain.CodeTooLong {
		t.Errorf("a long key: %v", p)
	}
	long, _ := importKey(fileRow{Input: Input{Name: strings.Repeat("n", 300), SubmissionDeadline: "2027-01-01"}})
	if len([]rune(long)) > MaxSourceKeyChars {
		t.Errorf("derived key of %d characters", len([]rune(long)))
	}
}

// The file as a whole: CSV or JSON by name, else by its first character; an unknown column
// or key, a bad shape, no rows or too many are refused before any row is looked at.
func TestParseFile(t *testing.T) {
	rows, err := parseFile("v.CSV", []byte("\xef\xbb\xbfName, FIELDS ,scope\nA,robotics; other ,\"Line one\nline two\"\n"))
	if err != nil || len(rows) != 1 || rows[0].Name != "A" || !slices.Equal(rows[0].Fields, []string{"robotics", " other "}) ||
		rows[0].Scope != "Line one\nline two" {
		t.Fatalf("csv = %+v, %v", rows, err)
	}
	rows, err = parseFile("upload", []byte(` [{"name":"A","fields":["robotics"]}]`))
	if err != nil || len(rows) != 1 || rows[0].Fields[0] != "robotics" {
		t.Fatalf("json by content = %+v, %v", rows, err)
	}
	var fileErr *domain.ImportFileError
	for _, bad := range []struct{ name, data string }{
		{"v.csv", "name,colour\nA,red\n"},
		{"v.csv", "name,name\nA,B\n"},
		{"v.csv", "name,scope\nA\n"},
		{"v.csv", "name\n"},
		{"v.csv", ""},
		{"v.json", `{"name":"A"}`},
		{"v.json", `[{"name":"A","colour":"red"}]`},
		{"v.json", `[{"name":5}]`},
		{"v.json", `[{"name":"A"}] [1]`},
		{"v.json", `[]`},
		{"v.csv", "name\n\xff\n"},
	} {
		if _, err := parseFile(bad.name, []byte(bad.data)); !errors.As(err, &fileErr) {
			t.Errorf("%s %q: %v, want an ImportFileError", bad.name, bad.data, err)
		}
	}
	if _, err := parseFile("v.csv", []byte("name\n"+strings.Repeat("A\n", MaxImportRows+1))); !errors.As(err, new(*domain.TooManyRowsError)) {
		t.Errorf("too many CSV rows: %v", err)
	}
	if rows, err := parseFile("v.csv", []byte("name\n"+strings.Repeat("A\n", MaxImportRows))); err != nil || len(rows) != MaxImportRows {
		t.Errorf("exactly %d rows: %d, %v", MaxImportRows, len(rows), err)
	}
	if _, err := parseFile("v.json", []byte("["+strings.Repeat(`{"name":"A"},`, MaxImportRows)+`{"name":"A"}]`)); !errors.As(err, new(*domain.TooManyRowsError)) {
		t.Errorf("too many JSON rows: %v", err)
	}
}

func TestImportRefusesALargeFile(t *testing.T) {
	s := New(nil, nil, nil)
	_, err := s.Import(t.Context(), "admin", "v.csv", strings.NewReader(strings.Repeat("x", MaxImportBytes+1)), true)
	if !errors.Is(err, domain.ErrFileTooLarge) {
		t.Errorf("err = %v, want ErrFileTooLarge", err)
	}
}

func TestChangedFieldsAndPatch(t *testing.T) {
	a, _ := validate(validInput(), testCatalog)
	in, err := applyPatch(inputOf(a), Patch{ShortName: Opt{Set: true, Null: true}, TimeZone: Opt{Set: true, Value: "UTC"}})
	if err != nil {
		t.Fatal(err)
	}
	if in.SubmissionDeadline != "2027-03-01T23:59:59" || in.ShortName != "" {
		t.Errorf("patched input = %+v", in)
	}
	b, problems := validate(in, testCatalog)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if got := changedFields(a, b); !slices.Equal(got, []string{"shortName", "timeZone", "submissionDeadline", "abstractDeadline", "notificationAt"}) {
		t.Errorf("changed = %v", got)
	}
	if got := changedFields(a, a); len(got) != 0 {
		t.Errorf("no change = %v", got)
	}
	for _, p := range []Patch{{Scope: Opt{Set: true, Null: true}}, {FieldsSet: true, FieldsNull: true}} {
		if _, err := applyPatch(inputOf(a), p); !errors.As(err, new(*domain.InvalidError)) {
			t.Errorf("null for a required field: %v", err)
		}
	}
}
