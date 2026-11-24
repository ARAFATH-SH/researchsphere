//go:build integration

package postgres

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

// now is the tests' clock: 10 January 2027, 12:00 UTC.
var now = time.Date(2027, 1, 10, 12, 0, 0, 0, time.UTC)

type env struct {
	pool  *pgxpool.Pool
	svc   *usecase.Service
	admin string
	clock *time.Time
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testenv.DB(t)
	clock := now
	e := env{pool: pool, clock: &clock}
	e.svc = usecase.New(NewStore(pool), postgres.NewTransactor(pool), func() time.Time { return *e.clock })
	e.admin = e.user(t, "admin", "Ada Admin")
	return e
}

// user inserts a verified account with a profile and returns its ID.
func (e env) user(t *testing.T, role, name string) string {
	t.Helper()
	var id string
	if err := e.pool.QueryRow(t.Context(), `WITH u AS (
			INSERT INTO users (email, password_hash, role, email_verified_at) VALUES ($1, 'hash', $2, now()) RETURNING id)
		INSERT INTO profiles (user_id, full_name) SELECT id, $3 FROM u RETURNING user_id::text`, testenv.Email(t), role, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// venue is a valid online workshop in explainable_ai with its deadline in UTC.
func venue(name, deadline string) usecase.Input {
	return usecase.Input{
		Name: name, Type: "workshop", Fields: []string{"explainable_ai"}, Attendance: "online", TimeZone: "UTC",
		SubmissionDeadline: deadline, OfficialURL: "https://example.org/" + strings.ReplaceAll(name, " ", "-"), Scope: "About " + name,
	}
}

func (e env) create(t *testing.T, in usecase.Input) domain.Venue {
	t.Helper()
	v, err := e.svc.Create(t.Context(), e.admin, in)
	if err != nil {
		t.Fatalf("create %s: %v", in.Name, err)
	}
	return v
}

func (e env) names(t *testing.T, in usecase.ListInput) []string {
	t.Helper()
	p, _, _, err := e.svc.List(t.Context(), in)
	if err != nil {
		t.Fatalf("list %+v: %v", in, err)
	}
	out := make([]string, len(p.Items))
	for i, v := range p.Items {
		out[i] = v.Name
	}
	return out
}

func (e env) scalar(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(t.Context(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// FR-14.1, FR-14.2: upcoming venues by nearest deadline (ties by name); past ones hidden,
// and with includePast shown after the upcoming ones, most recently closed first.
func TestListOrderAndPast(t *testing.T) {
	e := newEnv(t)
	e.create(t, venue("Far", "2027-03-01"))
	e.create(t, venue("Soon B", "2027-01-15"))
	e.create(t, venue("Soon A", "2027-01-15"))
	e.create(t, venue("Closed recently", "2027-01-08"))
	e.create(t, venue("Closed long ago", "2026-11-20"))

	if got, want := e.names(t, usecase.ListInput{}), []string{"Soon A", "Soon B", "Far"}; !slices.Equal(got, want) {
		t.Errorf("upcoming = %v, want %v", got, want)
	}
	want := []string{"Soon A", "Soon B", "Far", "Closed recently", "Closed long ago"}
	if got := e.names(t, usecase.ListInput{IncludePast: true}); !slices.Equal(got, want) {
		t.Errorf("with past = %v, want %v", got, want)
	}
	p, _, _, err := e.svc.List(t.Context(), usecase.ListInput{IncludePast: true})
	if err != nil || p.Total != 5 || !p.Items[3].Past(now) || p.Items[0].Past(now) {
		t.Errorf("total %d, past flags wrong, %v", p.Total, err)
	}
}

// A deadline has passed at its instant exactly: an AoE deadline of 1 March is 2 March
// 11:59:59 UTC.
func TestPastAtTheAoEBoundary(t *testing.T) {
	e := newEnv(t)
	in := venue("AoE venue", "2027-03-01")
	in.TimeZone = "AoE"
	v := e.create(t, in)
	if want := time.Date(2027, 3, 2, 11, 59, 59, 0, time.UTC); !v.SubmissionDeadline.Equal(want) {
		t.Fatalf("deadline = %v, want %v", v.SubmissionDeadline, want)
	}
	*e.clock = time.Date(2027, 3, 2, 11, 59, 58, 0, time.UTC)
	if got := e.names(t, usecase.ListInput{}); len(got) != 1 {
		t.Errorf("one second before: %v", got)
	}
	*e.clock = time.Date(2027, 3, 2, 11, 59, 59, 0, time.UTC)
	if got := e.names(t, usecase.ListInput{}); len(got) != 0 {
		t.Errorf("at the deadline it has passed: %v", got)
	}
}

// FR-14.3: the deadline range is calendar days in the view zone, Asia/Dhaka by default. An AoE
// deadline of 1 March falls on 2 March in Dhaka (17:59:59) but on 1 March in UTC-12.
func TestDeadlineRangeAcrossTheAoEBoundary(t *testing.T) {
	e := newEnv(t)
	in := venue("AoE venue", "2027-03-01")
	in.TimeZone = "AoE"
	e.create(t, in)
	e.create(t, venue("Before", "2027-02-20"))
	e.create(t, venue("After", "2027-03-10"))

	tests := []struct {
		name string
		in   usecase.ListInput
		want []string
	}{
		{"Dhaka, 2 March", usecase.ListInput{DeadlineFrom: "2027-03-02", DeadlineTo: "2027-03-02"}, []string{"AoE venue"}},
		{"Dhaka, 1 March", usecase.ListInput{DeadlineFrom: "2027-03-01", DeadlineTo: "2027-03-01"}, nil},
		{"UTC-12, 1 March", usecase.ListInput{DeadlineFrom: "2027-03-01", DeadlineTo: "2027-03-01", TZ: "Etc/GMT+12"}, []string{"AoE venue"}},
		{"UTC, 2 March", usecase.ListInput{DeadlineFrom: "2027-03-02", DeadlineTo: "2027-03-02", TZ: "UTC"}, []string{"AoE venue"}},
		{"from only", usecase.ListInput{DeadlineFrom: "2027-03-02"}, []string{"AoE venue", "After"}},
		{"to only", usecase.ListInput{DeadlineTo: "2027-03-01"}, []string{"Before"}},
		{"to is inclusive in Dhaka", usecase.ListInput{DeadlineTo: "2027-03-02"}, []string{"Before", "AoE venue"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := e.names(t, tt.in); !slices.Equal(got, tt.want) && (len(got) != 0 || len(tt.want) != 0) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// FR-14.3: type and field filters (each may repeat, any match), q on name and short name with
// one typo, and their combinations.
func TestListFilters(t *testing.T) {
	e := newEnv(t)
	conf := venue("International Conference on Machine Learning", "2027-02-01")
	conf.Type, conf.ShortName, conf.Fields = "conference", "ICML", []string{"explainable_ai", "robotics"}
	conf.Attendance, conf.City, conf.Country = "in_person", "Vienna", "AT"
	e.create(t, conf)
	journal := venue("Journal of Bangla Language Processing", "2027-02-02")
	journal.Type, journal.Attendance, journal.Fields = "journal", "", []string{"bangla_nlp"}
	e.create(t, journal)
	ws := venue("Workshop on Robot Learning", "2027-02-03")
	ws.Fields = []string{"robotics"}
	e.create(t, ws)
	si := venue("Special Issue on Flood Forecasting", "2027-02-04")
	si.Type, si.Attendance, si.Fields = "special_issue", "", []string{"water_resources", "climate_modelling"}
	e.create(t, si)

	all := []string{"International Conference on Machine Learning", "Journal of Bangla Language Processing",
		"Workshop on Robot Learning", "Special Issue on Flood Forecasting"}
	tests := []struct {
		name string
		in   usecase.ListInput
		want []string
	}{
		{"no filter", usecase.ListInput{}, all},
		{"one type", usecase.ListInput{Types: []string{"journal"}}, all[1:2]},
		{"two types", usecase.ListInput{Types: []string{"workshop", "conference"}}, []string{all[0], all[2]}},
		{"one field", usecase.ListInput{Fields: []string{"robotics"}}, []string{all[0], all[2]}},
		{"two fields", usecase.ListInput{Fields: []string{"bangla_nlp", "climate_modelling"}}, []string{all[1], all[3]}},
		{"type and field", usecase.ListInput{Types: []string{"workshop"}, Fields: []string{"robotics"}}, all[2:3]},
		{"q, part of the name", usecase.ListInput{Q: "robot"}, all[2:3]},
		{"q, any case", usecase.ListInput{Q: "FLOOD"}, all[3:4]},
		{"q, a typo", usecase.ListInput{Q: "Forecastnig"}, all[3:4]},
		{"q, the short name", usecase.ListInput{Q: "icml"}, all[0:1]},
		{"q, no match", usecase.ListInput{Q: "Quantum chemistry"}, nil},
		{"q, a LIKE wildcard is literal", usecase.ListInput{Q: "%%"}, nil},
		{"q and type", usecase.ListInput{Q: "learning", Types: []string{"conference"}}, all[0:1]},
		{"field and range", usecase.ListInput{Fields: []string{"robotics"}, DeadlineFrom: "2027-02-03", TZ: "UTC"}, all[2:3]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := e.names(t, tt.in); !slices.Equal(got, tt.want) && (len(got) != 0 || len(tt.want) != 0) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}

	// Facets: each count ignores its own filter and keeps the others; every value is listed.
	p, _, _, err := e.svc.List(t.Context(), usecase.ListInput{Types: []string{"workshop"}, Fields: []string{"robotics"}})
	if err != nil {
		t.Fatal(err)
	}
	types := map[domain.VenueType]int{}
	for _, c := range p.TypeCounts {
		types[c.Type] = c.Count
	}
	if len(p.TypeCounts) != 4 || types["conference"] != 1 || types["workshop"] != 1 || types["journal"] != 0 {
		t.Errorf("type facets = %+v", p.TypeCounts)
	}
	fields := map[string]int{}
	for _, c := range p.FieldCounts {
		fields[c.Field.Key] = c.Count
	}
	// Only workshops count for fields: the one workshop has robotics.
	if len(p.FieldCounts) != 21 || fields["robotics"] != 1 || fields["explainable_ai"] != 0 || p.FieldCounts[20].Field.Key != "other" {
		t.Errorf("field facets = %+v", p.FieldCounts)
	}

	// Bad parameters name their field.
	for _, bad := range []struct {
		in    usecase.ListInput
		field string
	}{
		{usecase.ListInput{Types: []string{"seminar"}}, "type"},
		{usecase.ListInput{Fields: []string{"astrology"}}, "field"},
		{usecase.ListInput{DeadlineFrom: "1 March"}, "deadlineFrom"},
		{usecase.ListInput{DeadlineFrom: "2027-03-02", DeadlineTo: "2027-03-01"}, "deadlineTo"},
		{usecase.ListInput{TZ: "Mars/Olympus"}, "tz"},
		{usecase.ListInput{TZ: "AoE"}, "tz"},
		{usecase.ListInput{Q: "a"}, "q"},
		{usecase.ListInput{Page: -1}, "page"},
	} {
		_, _, _, err := e.svc.List(t.Context(), bad.in)
		var inv *domain.InvalidError
		if !errors.As(err, &inv) || inv.Problems[0].Field != bad.field {
			t.Errorf("%+v: %v, want a problem with %s", bad.in, err, bad.field)
		}
	}
}

// Paging: 20 by default, at most 50, with the total of every page.
func TestListPaging(t *testing.T) {
	e := newEnv(t)
	for i := range 23 {
		e.create(t, venue("Venue "+string(rune('A'+i)), time.Date(2027, 2, 1+i, 0, 0, 0, 0, time.UTC).Format("2006-01-02")))
	}
	p, page, size, err := e.svc.List(t.Context(), usecase.ListInput{})
	if err != nil || len(p.Items) != 20 || p.Total != 23 || page != 1 || size != 20 || p.Items[0].Name != "Venue A" {
		t.Fatalf("page 1: %d items, total %d, page %d size %d, %v", len(p.Items), p.Total, page, size, err)
	}
	p, _, _, err = e.svc.List(t.Context(), usecase.ListInput{Page: 2})
	if err != nil || len(p.Items) != 3 || p.Total != 23 || p.Items[0].Name != "Venue U" {
		t.Fatalf("page 2: %+v, %v", p.Items, err)
	}
	p, _, size, err = e.svc.List(t.Context(), usecase.ListInput{PageSize: 500})
	if err != nil || size != 50 || len(p.Items) != 23 {
		t.Fatalf("capped page: size %d, %d items, %v", size, len(p.Items), err)
	}
	p, _, _, err = e.svc.List(t.Context(), usecase.ListInput{Page: 9})
	if err != nil || len(p.Items) != 0 || p.Total != 23 {
		t.Fatalf("beyond the last page: %d items, total %d, %v", len(p.Items), p.Total, err)
	}
}

// FR-15.1: a removal hides the venue from the list and its page, refuses a change, keeps the
// row and its audit entry with the reason, and drops its embedding and job at once.
func TestRemoveHidesAndKeepsTheAudit(t *testing.T) {
	e := newEnv(t)
	v := e.create(t, venue("Gone", "2027-02-01"))
	e.create(t, venue("Kept", "2027-02-02"))
	if _, err := e.pool.Exec(t.Context(), `INSERT INTO venue_embeddings (venue_id, model, content_hash, embedding)
		VALUES ($1, 'm@1', repeat('a', 64), array_fill(0.1::real, ARRAY[768])::vector)`, v.ID); err != nil {
		t.Fatal(err)
	}

	err := e.svc.Remove(t.Context(), e.admin, v.ID, "short")
	var inv *domain.InvalidError
	if !errors.As(err, &inv) || inv.Problems[0].Field != "reason" {
		t.Fatalf("a short reason: %v", err)
	}
	if err := e.svc.Remove(t.Context(), e.admin, v.ID, "  Duplicate of another venue  "); err != nil {
		t.Fatal(err)
	}
	if got := e.names(t, usecase.ListInput{IncludePast: true}); !slices.Equal(got, []string{"Kept"}) {
		t.Errorf("list = %v", got)
	}
	if _, err := e.svc.Get(t.Context(), v.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get after removal: %v", err)
	}
	if _, err := e.svc.Update(t.Context(), e.admin, v.ID, usecase.Patch{Scope: usecase.Opt{Set: true, Value: "New"}}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("change after removal: %v", err)
	}
	if err := e.svc.Remove(t.Context(), e.admin, v.ID, "Duplicate of another venue"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second removal: %v", err)
	}
	if n := e.scalar(t, `SELECT count(*) FROM venues WHERE id = $1 AND removed_at IS NOT NULL
		AND removal_reason = 'Duplicate of another venue' AND removed_by = $2`, v.ID, e.admin); n != 1 {
		t.Error("the row is not kept with its reason")
	}
	if n := e.scalar(t, `SELECT count(*) FROM venue_actions WHERE venue_id = $1 AND action = 'remove'
		AND reason = 'Duplicate of another venue' AND admin_id = $2 AND admin_name = 'Ada Admin'`, v.ID, e.admin); n != 1 {
		t.Error("no remove audit row")
	}
	if n := e.scalar(t, `SELECT count(*) FROM venue_embeddings WHERE venue_id = $1`, v.ID) +
		e.scalar(t, `SELECT count(*) FROM embedding_jobs WHERE owner_id = $1`, v.ID); n != 0 {
		t.Error("the removed venue is still in the AI index or queue")
	}
}

// The audit trail outlives the administrator's account: the ID goes null, the name stays.
func TestAuditOutlivesTheAdmin(t *testing.T) {
	e := newEnv(t)
	other := e.user(t, "admin", "Bea Admin")
	v, err := e.svc.Create(t.Context(), other, venue("Venue", "2027-02-01"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, other); err != nil {
		t.Fatal(err)
	}
	if n := e.scalar(t, `SELECT count(*) FROM venue_actions WHERE venue_id = $1 AND action = 'create'
		AND admin_id IS NULL AND admin_name = 'Bea Admin'`, v.ID); n != 1 {
		t.Error("the audit row did not survive the account")
	}
	if n := e.scalar(t, `SELECT count(*) FROM venues WHERE id = $1 AND created_by IS NULL`, v.ID); n != 1 {
		t.Error("the venue did not survive its creator")
	}
}

// FR-15.1: a change names the changed fields in the audit, never their values; a change of
// nothing writes nothing. Changing the time zone keeps the local time.
func TestUpdateAudit(t *testing.T) {
	e := newEnv(t)
	v := e.create(t, venue("Venue", "2027-03-01T23:59"))
	ctx := t.Context()
	got, err := e.svc.Update(ctx, e.admin, v.ID, usecase.Patch{
		TimeZone: usecase.Opt{Set: true, Value: "AoE"}, ShortName: usecase.Opt{Set: true, Value: "VEN"},
		Fields: []string{"robotics", "explainable_ai"}, FieldsSet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2027, 3, 2, 11, 59, 0, 0, time.UTC); !got.SubmissionDeadline.Equal(want) || got.TimeZone != "AoE" {
		t.Errorf("deadline = %v %s, want %v AoE", got.SubmissionDeadline, got.TimeZone, want)
	}
	if keys := got.FieldKeys(); !slices.Equal(keys, []string{"explainable_ai", "robotics"}) {
		t.Errorf("fields = %v (the fixed list's order)", keys)
	}
	var changed []string
	if err := e.pool.QueryRow(ctx, `SELECT changed_fields FROM venue_actions WHERE venue_id = $1 AND action = 'update'`, v.ID).Scan(&changed); err != nil {
		t.Fatal(err)
	}
	if want := []string{"shortName", "fields", "timeZone", "submissionDeadline"}; !slices.Equal(changed, want) {
		t.Errorf("changed = %v, want %v", changed, want)
	}
	if _, err := e.svc.Update(ctx, e.admin, v.ID, usecase.Patch{ShortName: usecase.Opt{Set: true, Value: "VEN"}}); err != nil {
		t.Fatal(err)
	}
	if n := e.scalar(t, `SELECT count(*) FROM venue_actions WHERE venue_id = $1 AND action = 'update'`, v.ID); n != 1 {
		t.Errorf("an empty change wrote %d update rows", n)
	}
	_, err = e.svc.Update(ctx, e.admin, v.ID, usecase.Patch{Name: usecase.Opt{Set: true, Null: true}})
	var inv *domain.InvalidError
	if !errors.As(err, &inv) || inv.Problems[0].Field != "name" {
		t.Errorf("null name: %v", err)
	}
	_, err = e.svc.Update(ctx, e.admin, v.ID, usecase.Patch{OfficialURL: usecase.Opt{Set: true, Value: "http://example.org"}})
	if !errors.As(err, &inv) || inv.Problems[0] != (domain.Problem{Field: "officialUrl", Code: domain.CodeNotHTTPS}) {
		t.Errorf("http link: %v", err)
	}
}

// AI-1.3, AI-1.5: a new venue is queued; a change of name, short name, scope or fields queues
// it again; a change of anything else does not.
func TestEmbeddingJobOnChange(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	v := e.create(t, venue("Venue", "2027-02-01"))
	generation := func() int {
		return e.scalar(t, `SELECT coalesce(max(generation), 0)::int FROM embedding_jobs WHERE owner_type = 'venue' AND owner_id = $1`, v.ID)
	}
	if generation() == 0 {
		t.Fatal("a new venue is not queued")
	}
	// The worker took the job.
	if _, err := e.pool.Exec(ctx, `DELETE FROM embedding_jobs WHERE owner_id = $1`, v.ID); err != nil {
		t.Fatal(err)
	}
	change := func(p usecase.Patch) {
		t.Helper()
		if _, err := e.svc.Update(ctx, e.admin, v.ID, p); err != nil {
			t.Fatal(err)
		}
	}
	change(usecase.Patch{OfficialURL: usecase.Opt{Set: true, Value: "https://example.org/new"},
		SubmissionDeadline: usecase.Opt{Set: true, Value: "2027-02-05"}})
	if g := generation(); g != 0 {
		t.Errorf("a link and deadline change queued the venue (generation %d)", g)
	}
	for _, p := range []usecase.Patch{
		{Scope: usecase.Opt{Set: true, Value: "New scope"}},
		{Name: usecase.Opt{Set: true, Value: "Renamed"}},
		{ShortName: usecase.Opt{Set: true, Value: "RN"}},
		{Fields: []string{"robotics"}, FieldsSet: true},
	} {
		before := generation()
		change(p)
		if generation() <= before {
			t.Errorf("%+v did not queue the venue", p)
		}
	}
}

// importCSV is a file of three venues: a keyed conference, a journal keyed by name and year,
// and a row with an http link and an unknown field.
const importCSV = "sourceKey,name,shortName,type,fields,attendance,city,country,startsOn,endsOn,timeZone,submissionDeadline,officialUrl,scope\n" +
	"cfp-1,Conference on Robots,COR,conference,robotics;explainable_ai,hybrid,Dhaka,bd,2027-06-01,2027-06-03,AoE,2027-03-01,https://cor.example.org,Robots.\n" +
	",Journal of Floods,,journal,water_resources,,,,,,Asia/Dhaka,2027-04-01T17:00,https://jof.example.org,Floods.\n" +
	"cfp-3,Bad row,,workshop,astrology,online,,,,,UTC,2027-04-01,http://bad.example.org,Bad.\n"

func (e env) importFile(t *testing.T, name, data string, dryRun bool) usecase.ImportResult {
	t.Helper()
	res, err := e.svc.Import(t.Context(), e.admin, name, strings.NewReader(data), dryRun)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return res
}

func outcomes(res usecase.ImportResult) []string {
	out := make([]string, len(res.Rows))
	for i, r := range res.Rows {
		out[i] = r.Outcome
	}
	return out
}

// FR-14.5 (interim): a dry run reports and writes nothing; an import creates, then a second
// import of the same file changes nothing and a changed file updates by key in place of
// adding a duplicate. A rejected row says why; only https links are accepted.
func TestImport(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()

	dry := e.importFile(t, "venues.csv", importCSV, true)
	if !dry.DryRun || dry.Created != 2 || dry.Rejected != 1 || !slices.Equal(outcomes(dry), []string{"created", "created", "rejected"}) {
		t.Fatalf("dry run = %+v", dry)
	}
	if n := e.scalar(t, `SELECT count(*) FROM venues`) + e.scalar(t, `SELECT count(*) FROM venue_actions`); n != 0 {
		t.Fatalf("a dry run wrote %d rows", n)
	}
	bad := dry.Rows[2].Problems
	if !slices.Contains(bad, domain.Problem{Field: "officialUrl", Code: domain.CodeNotHTTPS}) ||
		!slices.Contains(bad, domain.Problem{Field: "fields", Code: domain.CodeUnknownField}) {
		t.Errorf("reasons = %+v", bad)
	}
	if dry.Rows[1].SourceKey != "auto:journal of floods:2027" || dry.Rows[0].VenueID != "" {
		t.Errorf("rows = %+v", dry.Rows)
	}

	res := e.importFile(t, "venues.csv", importCSV, false)
	if res.Created != 2 || res.Rejected != 1 || res.Rows[0].VenueID == "" {
		t.Fatalf("import = %+v", res)
	}
	conf, err := e.svc.Get(ctx, res.Rows[0].VenueID)
	if err != nil {
		t.Fatal(err)
	}
	if conf.Country != "BD" || conf.Source != domain.SourceImport || conf.SourceKey != "cfp-1" ||
		!conf.SubmissionDeadline.Equal(time.Date(2027, 3, 2, 11, 59, 59, 0, time.UTC)) || len(conf.Fields) != 2 {
		t.Errorf("imported venue = %+v", conf)
	}
	journal, err := e.svc.Get(ctx, res.Rows[1].VenueID)
	if err != nil || !journal.SubmissionDeadline.Equal(time.Date(2027, 4, 1, 11, 0, 0, 0, time.UTC)) {
		t.Errorf("journal deadline 17:00 in Dhaka = %v, %v", journal.SubmissionDeadline, err)
	}
	if n := e.scalar(t, `SELECT count(DISTINCT import_id) FROM venue_actions WHERE action = 'import_create'`); n != 1 {
		t.Errorf("one import's audit rows share an import ID: %d IDs", n)
	}

	again := e.importFile(t, "venues.csv", importCSV, false)
	if !slices.Equal(outcomes(again), []string{"unchanged", "unchanged", "rejected"}) || again.Rows[0].VenueID != conf.ID {
		t.Errorf("same file again = %+v", again)
	}

	changed := strings.Replace(importCSV, "Robots.", "Robots and drones.", 1)
	changed = strings.Replace(changed, "Journal of Floods,,journal,water_resources", "Journal of Floods,JOF,journal,water_resources", 1)
	upd := e.importFile(t, "venues.csv", changed, false)
	if !slices.Equal(outcomes(upd), []string{"updated", "updated", "rejected"}) || upd.Rows[1].VenueID != journal.ID {
		t.Fatalf("changed file = %+v", upd)
	}
	if n := e.scalar(t, `SELECT count(*) FROM venues`); n != 2 {
		t.Errorf("%d venues after three imports, want 2", n)
	}
	var fields []string
	if err := e.pool.QueryRow(ctx, `SELECT changed_fields FROM venue_actions WHERE venue_id = $1 AND action = 'import_update'`, conf.ID).Scan(&fields); err != nil ||
		!slices.Equal(fields, []string{"scope"}) {
		t.Errorf("changed fields = %v, %v", fields, err)
	}

	// A removed venue is not brought back by its key, and a hand-made venue is never matched.
	if err := e.svc.Remove(ctx, e.admin, conf.ID, "Cancelled this year"); err != nil {
		t.Fatal(err)
	}
	manual := venue("Journal of Floods", "2027-04-01")
	manual.Type, manual.Attendance = "journal", ""
	e.create(t, manual)
	last := e.importFile(t, "venues.csv", changed, false)
	if last.Rows[0].Outcome != "rejected" || !slices.Contains(last.Rows[0].Problems, domain.Problem{Field: "row", Code: domain.CodeVenueRemoved}) ||
		last.Rows[1].Outcome != "unchanged" {
		t.Errorf("after removal = %+v", last)
	}
}

// A JSON file reads the same way, and two rows with one key in a file reject the second.
func TestImportJSONAndDuplicateKeys(t *testing.T) {
	e := newEnv(t)
	data := `[
	  {"sourceKey": "k1", "name": "Special Issue on Rivers", "type": "special_issue", "fields": ["water_resources"],
	   "timeZone": "UTC", "submissionDeadline": "2027-05-01", "officialUrl": "https://rivers.example.org", "scope": "Rivers."},
	  {"sourceKey": "k1", "name": "Special Issue on Lakes", "type": "special_issue", "fields": ["water_resources"],
	   "timeZone": "UTC", "submissionDeadline": "2027-05-01", "officialUrl": "https://lakes.example.org", "scope": "Lakes."}
	]`
	res := e.importFile(t, "upload", data, false)
	if !slices.Equal(outcomes(res), []string{"created", "rejected"}) ||
		!slices.Equal(res.Rows[1].Problems, []domain.Problem{{Field: "sourceKey", Code: domain.CodeDuplicateInFile}}) {
		t.Fatalf("import = %+v", res)
	}
}
