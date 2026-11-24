//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// The automatic venue import (FR-14.5, slice 25A) against the real database, with in-memory
// feeds. The recorded files of the real sources are tested in the feeds package and, end
// to end, in cmd/api.

// syncNow is the sync tests' clock: 24 November 2026, 12:00 UTC.
var syncNow = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

const (
	headA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	headB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// fakeRepo is a repository whose files hold editions as JSON. head is the commit it is at;
// a fetch from that commit changes nothing, as a 304 would.
type fakeRepo struct {
	mu      sync.Mutex
	head    string
	files   []domain.RepoFile
	fetches int
	fail    error
}

func editionsFile(t *testing.T, path string, eds ...domain.Edition) domain.RepoFile {
	t.Helper()
	b, err := json.Marshal(eds)
	if err != nil {
		t.Fatal(err)
	}
	return domain.RepoFile{Path: path, Content: string(b)}
}

func (r *fakeRepo) set(head string, files ...domain.RepoFile) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.head, r.files = head, files
}

func (r *fakeRepo) Fetch(_ context.Context, prev domain.RepoState) (domain.RepoFetch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fetches++
	if r.fail != nil {
		return domain.RepoFetch{}, r.fail
	}
	st := domain.RepoState{CommitSHA: r.head, ETag: `"` + r.head + `"`}
	if prev.CommitSHA == r.head {
		return domain.RepoFetch{State: st}, nil
	}
	return domain.RepoFetch{State: st, Changed: true, Full: true, Files: slices.Clone(r.files)}, nil
}

func (r *fakeRepo) Parse(files []domain.RepoFile) ([]domain.Edition, []domain.FileError) {
	var out []domain.Edition
	var errs []domain.FileError
	for _, f := range files {
		var eds []domain.Edition
		if err := json.Unmarshal([]byte(f.Content), &eds); err != nil {
			errs = append(errs, domain.FileError{Path: f.Path, Message: "not JSON"})
			continue
		}
		out = append(out, eds...)
	}
	return out, errs
}

type fakeJournals struct{ list []domain.Journal }

func (f *fakeJournals) Journals(context.Context) ([]domain.Journal, error) { return f.list, nil }

type fakeLinks struct {
	mu    sync.Mutex
	ok    map[string]bool
	calls int
}

func (f *fakeLinks) Check(_ context.Context, u string) domain.LinkCheck {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return domain.LinkCheck{OK: f.ok[u], FinalStatus: 200}
}

type syncEnv struct {
	env
	ccf, hci *fakeRepo
	journals *fakeJournals
	links    *fakeLinks
	syncer   *usecase.Syncer
	cfg      usecase.SyncConfig
}

func newSyncEnv(t *testing.T, cfg usecase.SyncConfig) *syncEnv {
	t.Helper()
	e := newEnv(t)
	*e.clock = syncNow
	s := &syncEnv{env: e, ccf: &fakeRepo{}, hci: &fakeRepo{}, journals: &fakeJournals{}, links: &fakeLinks{ok: map[string]bool{}}, cfg: cfg}
	s.syncer = usecase.NewSyncer(t.Context(), usecase.SyncDeps{
		Store: NewStore(e.pool), Sync: NewSyncStore(e.pool), Tx: postgres.NewTransactor(e.pool),
		Repos:    map[domain.Source]domain.RepoFeed{domain.SourceCCF: s.ccf, domain.SourceHCIDeadlines: s.hci},
		Journals: s.journals, Links: s.links, Now: func() time.Time { return *e.clock },
	}, cfg)
	t.Cleanup(s.syncer.Wait)
	return s
}

func (s *syncEnv) run(t *testing.T, source domain.Source, dry bool) domain.SyncRun {
	t.Helper()
	runs, err := s.syncer.RunNow(t.Context(), []string{string(source)}, dry)
	if err != nil {
		t.Fatalf("run %s: %v", source, err)
	}
	return runs[0]
}

func counts(r domain.SyncRun) [5]int {
	return [5]int{r.Created, r.Updated, r.Unchanged, r.Skipped, r.Rejected}
}

func edition(id, title string, year int, deadline string) domain.Edition {
	return domain.Edition{
		ID: id, Title: title, FullName: title + " Conference", Year: year, Link: "https://" + id + ".example.org/",
		Areas: []string{"AI"}, TimeZone: "AoE", Rounds: []domain.Round{{Deadline: deadline}}, Place: "Kyoto, Japan",
	}
}

func (s *syncEnv) venueByKey(t *testing.T, source domain.Source, key string) domain.Venue {
	t.Helper()
	got, err := NewStore(s.pool).ImportedByKeys(t.Context(), source, []string{key})
	if err != nil {
		t.Fatal(err)
	}
	return got[key].Venue
}

// FR-14.5: the first run creates; the next, with no new commit, re-reads the stored files and
// finds everything unchanged; a changed deadline updates in place with its audit row.
func TestSyncCreateUpdateUnchanged(t *testing.T) {
	s := newSyncEnv(t, usecase.SyncConfig{Contact: true})
	s.ccf.set(headA,
		editionsFile(t, "conference/AI/acl.yml", edition("acl27", "ACL", 2027, "2027-01-04 23:59:59")),
		editionsFile(t, "conference/AI/icml.yml", edition("icml27", "ICML", 2027, "2027-01-28 23:59:59"),
			edition("icml24", "ICML", 2024, "2024-02-01 23:59:59")), // past: left alone
	)
	first := s.run(t, domain.SourceCCF, false)
	if first.Status != domain.SyncSucceeded || counts(first) != [5]int{2, 0, 0, 0, 0} || first.CommitSHA != headA {
		t.Fatalf("first run = %+v", first)
	}
	v := s.venueByKey(t, domain.SourceCCF, "ccfddl:acl27")
	if v.Name != "ACL Conference" || v.Source != domain.SourceCCF || v.TimeZone != "AoE" ||
		!v.SubmissionDeadline.Equal(time.Date(2027, 1, 5, 11, 59, 59, 0, time.UTC)) {
		t.Fatalf("venue = %+v", v)
	}
	// Every change has an audit row by the import, with the run's ID, and its embedding is
	// queued at low priority.
	if n := s.scalar(t, `SELECT count(*) FROM venue_actions WHERE actor = 'import' AND admin_id IS NULL
		AND admin_name = 'Automatic import' AND action = 'import_create' AND import_id = $1`, first.ID); n != 2 {
		t.Fatalf("%d import_create rows", n)
	}
	if n := s.scalar(t, `SELECT count(*) FROM embedding_jobs WHERE owner_type = 'venue' AND priority = 1`); n != 2 {
		t.Fatalf("%d low-priority embedding jobs", n)
	}
	if n := s.scalar(t, `SELECT count(*) FROM venue_sync_files WHERE source = 'ccfddl'`); n != 2 {
		t.Fatalf("%d stored files", n)
	}

	second := s.run(t, domain.SourceCCF, false)
	if counts(second) != [5]int{0, 0, 2, 0, 0} || s.ccf.fetches != 2 {
		t.Fatalf("second run = %+v", second)
	}
	if n := s.scalar(t, `SELECT count(*) FROM venue_actions`); n != 2 {
		t.Fatalf("an unchanged run wrote %d audit rows", n-2)
	}

	moved := edition("acl27", "ACL", 2027, "2027-01-11 23:59:59")
	s.ccf.set(headB,
		editionsFile(t, "conference/AI/acl.yml", moved),
		editionsFile(t, "conference/AI/icml.yml", edition("icml27", "ICML", 2027, "2027-01-28 23:59:59")),
	)
	third := s.run(t, domain.SourceCCF, false)
	if counts(third) != [5]int{0, 1, 1, 0, 0} || third.CommitSHA != headB {
		t.Fatalf("third run = %+v", third)
	}
	var changed []string
	if err := s.pool.QueryRow(t.Context(), `SELECT changed_fields FROM venue_actions WHERE action = 'import_update' AND import_id = $1`,
		third.ID).Scan(&changed); err != nil || !slices.Equal(changed, []string{"submissionDeadline"}) {
		t.Fatalf("changed fields = %v, %v", changed, err)
	}
	if got := s.venueByKey(t, domain.SourceCCF, "ccfddl:acl27"); got.ID != v.ID {
		t.Fatal("an update made a new venue")
	}
}

// A venue an administrator added by hand, or imported from a file, is never touched; a
// synced venue an administrator edited is locked and skipped from then on; a removed one is
// skipped and never brought back.
func TestSyncLeavesManualLockedAndRemoved(t *testing.T) {
	s := newSyncEnv(t, usecase.SyncConfig{Contact: true})
	manual := s.create(t, usecase.Input{Name: "ACL Conference", Type: "conference", Fields: []string{"bangla_nlp"},
		Attendance: "online", TimeZone: "UTC", SubmissionDeadline: "2027-02-01", OfficialURL: "https://acl.example.org", Scope: "Hand"})
	if _, err := s.svc.Import(t.Context(), s.admin, "v.csv", strings.NewReader(
		"sourceKey,name,type,fields,attendance,timeZone,submissionDeadline,officialUrl,scope\n"+
			"ccfddl:acl27,From a file,conference,other,online,UTC,2027-02-01,https://f.example.org,File\n"), false); err != nil {
		t.Fatal(err)
	}
	s.ccf.set(headA, editionsFile(t, "a.yml",
		edition("acl27", "ACL", 2027, "2027-01-04 23:59:59"),
		edition("icml27", "ICML", 2027, "2027-01-28 23:59:59"),
		edition("kdd27", "KDD", 2027, "2027-02-08 23:59:59")))
	if r := s.run(t, domain.SourceCCF, false); counts(r) != [5]int{3, 0, 0, 0, 0} {
		t.Fatalf("first run = %+v", r)
	}
	if got, _ := s.svc.Get(t.Context(), manual.ID); got.Scope != "Hand" || got.Source != domain.SourceManual {
		t.Fatalf("manual venue changed: %+v", got)
	}
	if n := s.scalar(t, `SELECT count(*) FROM venues WHERE source = 'import' AND name = 'From a file'`); n != 1 {
		t.Fatal("the file import's venue changed")
	}

	// The administrator edits ICML: it is locked.
	icml := s.venueByKey(t, domain.SourceCCF, "ccfddl:icml27")
	if _, err := s.svc.Update(t.Context(), s.admin, icml.ID, usecase.Patch{Scope: usecase.Opt{Set: true, Value: "Edited by hand"}}); err != nil {
		t.Fatal(err)
	}
	locked, _ := s.svc.Get(t.Context(), icml.ID)
	if !locked.Locked() || !locked.LockedAt.Equal(syncNow) {
		t.Fatalf("not locked: %+v", locked)
	}
	// The administrator removes KDD.
	kdd := s.venueByKey(t, domain.SourceCCF, "ccfddl:kdd27")
	if err := s.svc.Remove(t.Context(), s.admin, kdd.ID, "Duplicate of another entry"); err != nil {
		t.Fatal(err)
	}

	// Every edition changes at the source.
	s.ccf.set(headB, editionsFile(t, "a.yml",
		edition("acl27", "ACL", 2027, "2027-01-05 23:59:59"),
		edition("icml27", "ICML", 2027, "2027-01-29 23:59:59"),
		edition("kdd27", "KDD", 2027, "2027-02-09 23:59:59")))
	r := s.run(t, domain.SourceCCF, false)
	if counts(r) != [5]int{0, 1, 0, 2, 0} {
		t.Fatalf("second run = %+v", r)
	}
	if got, _ := s.svc.Get(t.Context(), icml.ID); got.Scope != "Edited by hand" || !got.SubmissionDeadline.Equal(icml.SubmissionDeadline) {
		t.Fatalf("the locked venue changed: %+v", got)
	}
	if _, err := s.svc.Get(t.Context(), kdd.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("the removed venue came back: %v", err)
	}
	// Editing a venue added by hand never locks it (only synced venues lock).
	if _, err := s.svc.Update(t.Context(), s.admin, manual.ID, usecase.Patch{Scope: usecase.Opt{Set: true, Value: "Again"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.svc.Get(t.Context(), manual.ID); got.Locked() {
		t.Fatal("a manual venue was locked")
	}
}

// A dry run reads the source and reports, but writes no venue and stores nothing it read;
// its run is kept and shown apart from the real runs.
func TestSyncDryRun(t *testing.T) {
	s := newSyncEnv(t, usecase.SyncConfig{Contact: true})
	s.ccf.set(headA, editionsFile(t, "a.yml", edition("acl27", "ACL", 2027, "2027-01-04 23:59:59"),
		domain.Edition{ID: "tbd27", Title: "TBD", Year: 2027, Link: "https://t.example.org", Rounds: []domain.Round{{Deadline: "TBD"}}, Place: "Kyoto, Japan"}),
		domain.RepoFile{Path: "broken.yml", Content: "{"})
	r := s.run(t, domain.SourceCCF, true)
	if !r.DryRun || r.Status != domain.SyncSucceeded || counts(r) != [5]int{1, 0, 0, 0, 1} || len(r.Errors) != 1 {
		t.Fatalf("dry run = %+v", r)
	}
	if len(r.Rejects) != 1 || r.Rejects[0].SourceKey != "ccfddl:tbd27" ||
		!slices.Contains(r.Rejects[0].Problems, domain.Problem{Field: "submissionDeadline", Code: domain.CodeDeadlineTBD}) {
		t.Fatalf("rejects = %+v", r.Rejects)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM venues`:                                                 0,
		`SELECT count(*) FROM venue_actions`:                                          0,
		`SELECT count(*) FROM venue_sync_files`:                                       0,
		`SELECT count(*) FROM venue_sync_sources WHERE commit_sha IS NOT NULL`:        0,
		`SELECT count(*) FROM venue_sync_runs WHERE dry_run AND status = 'succeeded'`: 1,
		`SELECT count(*) FROM venue_sync_sources WHERE lease_until IS NOT NULL`:       0,
		`SELECT count(*) FROM embedding_jobs WHERE owner_type = 'venue'`:              0,
	} {
		if got := s.scalar(t, q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}
	st, err := s.syncer.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if st[0].Source != domain.SourceCCF || st[0].LastRun != nil || st[0].LastDryRun == nil || st[0].LastDryRun.ID != r.ID ||
		st[0].LastDryRun.Rejects[0].SourceKey != "ccfddl:tbd27" || !st[0].NextRunAt.IsZero() {
		t.Fatalf("status = %+v", st[0])
	}
	// The real run after it reads everything again (the dry run stored nothing) and creates.
	if r := s.run(t, domain.SourceCCF, false); counts(r) != [5]int{1, 0, 0, 0, 1} {
		t.Fatalf("real run = %+v", r)
	}
}

// Journals from OpenAlex are rolling: listed apart from the deadline list, with their
// homepage as https when it answers, else the ISSN portal.
func TestSyncRollingJournals(t *testing.T) {
	s := newSyncEnv(t, usecase.SyncConfig{Contact: true})
	topics := []domain.JournalTopic{{Name: "Deep learning", SubfieldID: 1702}, {Name: "Floods", SubfieldID: 2312}}
	s.journals.list = []domain.Journal{
		{ID: "S1", Name: "Journal of Learning", HomepageURL: "http://learning.example.org", ISSNL: "1111-1111", Topics: topics},
		{ID: "S2", Name: "Journal of Down Sites", HomepageURL: "https://down.example.org", ISSNL: "2222-2222", Topics: topics},
		{ID: "S3", Name: "Journal of Cardiology", HomepageURL: "https://heart.example.org", ISSNL: "3333-3333",
			Topics: []domain.JournalTopic{{Name: "Heart", SubfieldID: 2705}}},
		{ID: "S4", Name: "Journal Without Links", Topics: topics},
	}
	s.links.ok["https://learning.example.org"] = true
	r := s.run(t, domain.SourceOpenAlex, false)
	if counts(r) != [5]int{2, 0, 0, 0, 1} || r.Rejects[0].SourceKey != "openalex:S4" {
		t.Fatalf("run = %+v", r)
	}
	learning := s.venueByKey(t, domain.SourceOpenAlex, "openalex:S1")
	down := s.venueByKey(t, domain.SourceOpenAlex, "openalex:S2")
	if !learning.Rolling || learning.OfficialURL != "https://learning.example.org" || learning.Link() != domain.LinkOfficial ||
		learning.TimeZone != "" || !learning.SubmissionDeadline.IsZero() ||
		!slices.Equal(learning.FieldKeys(), []string{"explainable_ai", "water_resources"}) {
		t.Fatalf("learning = %+v", learning)
	}
	if down.OfficialURL != "https://portal.issn.org/resource/ISSN/2222-2222" || down.Link() != domain.LinkISSNPortal {
		t.Fatalf("down = %+v", down)
	}
	if n := s.scalar(t, `SELECT count(*) FROM venue_sync_link_checks`); n != 2 {
		t.Fatalf("%d link checks stored", n)
	}

	s.create(t, venue("Deadline workshop", "2027-01-15"))
	if got := listNames(t, s, usecase.ListInput{}); !slices.Equal(got, []string{"Deadline workshop"}) {
		t.Errorf("deadline list = %v", got)
	}
	if got := listNames(t, s, usecase.ListInput{Rolling: true}); !slices.Equal(got, []string{"Journal of Down Sites", "Journal of Learning"}) {
		t.Errorf("rolling list = %v", got)
	}
	for _, in := range []usecase.ListInput{{Rolling: true, IncludePast: true}, {Rolling: true, DeadlineFrom: "2027-01-01"}} {
		var bad *domain.InvalidError
		if _, _, _, err := s.svc.List(t.Context(), in); !errors.As(err, &bad) {
			t.Errorf("%+v: err = %v", in, err)
		}
	}

	// The next run reuses the checks (30 days) and changes nothing.
	calls := s.links.calls
	if r := s.run(t, domain.SourceOpenAlex, false); counts(r) != [5]int{0, 0, 2, 0, 1} || s.links.calls != calls {
		t.Fatalf("second run = %+v after %d new checks", r, s.links.calls-calls)
	}
	// An administrator sets the journal's own link: it becomes official, and locked.
	if _, err := s.svc.Update(t.Context(), s.admin, down.ID, usecase.Patch{OfficialURL: usecase.Opt{Set: true, Value: "https://up.example.org"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.svc.Get(t.Context(), down.ID); got.Link() != domain.LinkOfficial || !got.Locked() {
		t.Fatalf("after the admin's link: %+v", got)
	}
}

func listNames(t *testing.T, s *syncEnv, in usecase.ListInput) []string { return s.names(t, in) }

// hci-deadlines leaves out an edition ccf-deadlines has (same short name and year).
func TestSyncHCISkipsCCFEditions(t *testing.T) {
	s := newSyncEnv(t, usecase.SyncConfig{Contact: true})
	s.ccf.set(headA, editionsFile(t, "c.yml", edition("chi27", "CHI", 2027, "2026-12-10 23:59:59")))
	s.hci.set(headA, editionsFile(t, "chi.yml", edition("chi27", "CHI", 2027, "2026-12-10 23:59:59"),
		edition("uist27", "UIST", 2027, "2027-04-01 23:59:59")))
	s.run(t, domain.SourceCCF, false)
	r := s.run(t, domain.SourceHCIDeadlines, false)
	if counts(r) != [5]int{1, 0, 0, 1, 0} {
		t.Fatalf("hci run = %+v", r)
	}
	if n := s.scalar(t, `SELECT count(*) FROM venues WHERE short_name = 'CHI'`); n != 1 {
		t.Fatalf("%d CHI venues", n)
	}
}

// Runs take turns: a run in progress refuses another; a real run asked for within the
// minimum interval is refused (a dry run is not); without a contact address nothing runs;
// the schedule runs each source once per gap; a run cut off by a restart is marked failed.
func TestSyncClaims(t *testing.T) {
	s := newSyncEnv(t, usecase.SyncConfig{Contact: true, Daily: true})
	s.ccf.set(headA, editionsFile(t, "a.yml", edition("acl27", "ACL", 2027, "2027-01-04 23:59:59")))
	s.hci.set(headA)

	// A lease held by another instance.
	if _, err := s.pool.Exec(t.Context(), `INSERT INTO venue_sync_sources (source, lease_until) VALUES ('ccfddl', $1)
		ON CONFLICT (source) DO UPDATE SET lease_until = EXCLUDED.lease_until`, syncNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.syncer.Start(t.Context(), s.admin, []string{"ccfddl", "openalex"}, false); !errors.Is(err, domain.ErrSyncRunning) {
		t.Fatalf("err = %v, want running", err)
	}
	if n := s.scalar(t, `SELECT count(*) FROM venue_sync_runs`); n != 0 {
		t.Fatalf("a refused start stored %d runs", n)
	}
	// The lease expires: a run left "running" by the lost instance is failed.
	if _, err := s.pool.Exec(t.Context(), `INSERT INTO venue_sync_runs (source, dry_run, trigger, started_at) VALUES ('ccfddl', false, 'schedule', $1)`,
		syncNow.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	*s.clock = syncNow.Add(2 * time.Minute)
	runs, err := s.syncer.Start(t.Context(), s.admin, []string{"ccfddl"}, false)
	if err != nil || len(runs) != 1 || runs[0].Trigger != domain.TriggerOnDemand {
		t.Fatalf("start = %+v, %v", runs, err)
	}
	s.syncer.Wait()
	if n := s.scalar(t, `SELECT count(*) FROM venue_sync_runs WHERE status = 'failed' AND errors ? 'The run was interrupted.'`); n != 1 {
		t.Fatalf("%d interrupted runs marked failed", n)
	}
	if n := s.scalar(t, `SELECT count(*) FROM venue_sync_runs WHERE requested_by = $1 AND status = 'succeeded'`, s.admin); n != 1 {
		t.Fatal("the admin's run did not succeed")
	}

	// Within the minimum interval: a real run is refused, a dry run is not.
	if _, err := s.syncer.Start(t.Context(), s.admin, []string{"ccfddl"}, false); !errors.Is(err, domain.ErrSyncTooSoon) {
		t.Fatalf("err = %v, want too soon", err)
	}
	if _, err := s.syncer.Start(t.Context(), s.admin, []string{"ccfddl"}, true); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	s.syncer.Wait()

	// The schedule: due once, then not again for 20 hours.
	s.syncer.RunDue(t.Context())
	if n := s.scalar(t, `SELECT count(*) FROM venue_sync_runs WHERE trigger = 'schedule' AND status = 'succeeded'`); n != 3 {
		t.Fatalf("%d scheduled runs, want one per source", n)
	}
	*s.clock = s.clock.Add(19 * time.Hour)
	s.syncer.RunDue(t.Context())
	if n := s.scalar(t, `SELECT count(*) FROM venue_sync_runs WHERE trigger = 'schedule' AND status = 'succeeded'`); n != 3 {
		t.Fatalf("%d scheduled runs after 19 hours", n)
	}
	*s.clock = s.clock.Add(2 * time.Hour)
	s.syncer.RunDue(t.Context())
	if n := s.scalar(t, `SELECT count(*) FROM venue_sync_runs WHERE trigger = 'schedule' AND status = 'succeeded'`); n != 6 {
		t.Fatalf("%d scheduled runs after 21 hours", n)
	}

	// A source that fails is a failed run with its message; the others are not affected.
	s.ccf.fail = errors.New("connection refused")
	*s.clock = s.clock.Add(2 * time.Hour)
	runs, err = s.syncer.RunNow(t.Context(), nil, false)
	if err != nil || len(runs) != 3 || runs[0].Status != domain.SyncFailed || runs[1].Status != domain.SyncSucceeded {
		t.Fatalf("runs = %+v, %v", runs, err)
	}

	unconfigured := usecase.NewSyncer(t.Context(), usecase.SyncDeps{Store: NewStore(s.pool), Sync: NewSyncStore(s.pool),
		Tx: postgres.NewTransactor(s.pool)}, usecase.SyncConfig{})
	if _, err := unconfigured.Start(t.Context(), s.admin, nil, true); !errors.Is(err, usecase.ErrSyncUnconfigured) {
		t.Fatalf("err = %v", err)
	}
	var bad *domain.InvalidError
	if _, err := s.syncer.Start(t.Context(), s.admin, []string{"wikicfp"}, true); !errors.As(err, &bad) {
		t.Fatalf("err = %v", err)
	}
}
