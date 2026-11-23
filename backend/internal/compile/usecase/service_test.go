package usecase

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/compile/domain"
)

const (
	project = "p1"
	owner   = "u-owner"
	editor  = "u-editor"
	viewer  = "u-viewer"
	outside = "u-outside"
	frozen  = "u-suspended"
)

type env struct {
	store   *memStore
	objects *memObjects
	workers *memWorkers
	limiter *memLimiter
	pub     *memPublisher
	sources *memSources
	svc     *Service
	now     time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{store: newMemStore(), objects: &memObjects{objects: map[string][]byte{}}, workers: &memWorkers{alive: true},
		limiter: &memLimiter{used: map[string]int{}}, pub: &memPublisher{}, now: time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)}
	e.store.now = func() time.Time { return e.now }
	e.store.projects[project] = memProject{main: "main.tex", engine: domain.EnginePDFLaTeX, exists: true}
	e.store.members[project] = []string{owner, editor, viewer}
	e.store.paths[project] = []string{"main.tex", "sections/intro.tex", "refs.bib"}
	e.sources = &memSources{src: domain.Source{MainFilePath: "main.tex", LiveConfirmed: true}}
	e.svc = New(Deps{
		Store: e.store, Tx: noTx{},
		Access:  memAccess{viewer: domain.ErrForbidden, outside: domain.ErrNotFound, frozen: &domain.SuspendedError{}},
		Sources: e.sources, Objects: e.objects, Workers: e.workers, Limiter: e.limiter, Publisher: e.pub,
		Now: func() time.Time { return e.now }, Config: Config{ClaimWait: -1, MaxInputBytes: 100, MaxPDFBytes: 64},
	})
	return e
}

func (e *env) start(t *testing.T) domain.Job {
	t.Helper()
	j, err := e.svc.Start(t.Context(), editor, project)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func (e *env) claim(t *testing.T) domain.Claimed {
	t.Helper()
	c, ok, err := e.svc.Claim(t.Context(), "worker-1")
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	return c
}

// FR-13.1 and SRS 2.3: editors and the owner compile; a viewer is refused, a non-member does
// not see the project, a suspended account is refused.
func TestStartRoles(t *testing.T) {
	tests := []struct {
		user string
		want error
	}{
		{owner, nil}, {editor, nil},
		{viewer, domain.ErrForbidden}, {outside, domain.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.user, func(t *testing.T) {
			e := newEnv(t)
			_, err := e.svc.Start(t.Context(), tt.user, project)
			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
	e := newEnv(t)
	var s *domain.SuspendedError
	if _, err := e.svc.Start(t.Context(), frozen, project); !errors.As(err, &s) {
		t.Errorf("suspended: err = %v", err)
	}
}

// FR-13.4: one compile per project at a time; the refusal names the running one.
func TestStartOneActivePerProject(t *testing.T) {
	e := newEnv(t)
	first := e.start(t)
	_, err := e.svc.Start(t.Context(), owner, project)
	var running *domain.RunningError
	if !errors.As(err, &running) || running.ID != first.ID {
		t.Fatalf("second start: err = %v, want RunningError for %s", err, first.ID)
	}
	e.claim(t)
	if _, err := e.svc.Start(t.Context(), owner, project); !errors.As(err, &running) {
		t.Errorf("while running: err = %v", err)
	}
	if err := e.svc.Finish(t.Context(), first.ID, domain.Result{Outcome: domain.OutcomeCompleted}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Start(t.Context(), owner, project); err != nil {
		t.Errorf("after finish: %v", err)
	}
}

func TestStartRefusals(t *testing.T) {
	e := newEnv(t)
	e.limiter.used[editor] = CompilesPerWindow
	var limited *domain.RateLimitedError
	if _, err := e.svc.Start(t.Context(), editor, project); !errors.As(err, &limited) {
		t.Errorf("over the limit: err = %v", err)
	}
	if _, err := e.svc.Start(t.Context(), owner, project); err != nil {
		t.Errorf("another user is not limited: %v", err)
	}
	if e.limiter.used[owner] != 1 {
		t.Errorf("an accepted compile counts once: %d", e.limiter.used[owner])
	}

	e = newEnv(t)
	e.store.projects[project] = memProject{main: "paper.tex", engine: domain.EnginePDFLaTeX}
	if _, err := e.svc.Start(t.Context(), owner, project); !errors.Is(err, domain.ErrMainFileMissing) {
		t.Errorf("main file missing: err = %v", err)
	}
	if e.limiter.used[owner] != 0 {
		t.Error("a refused compile counted")
	}

	e = newEnv(t)
	e.workers.alive = false
	if _, err := e.svc.Start(t.Context(), owner, project); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("no worker: err = %v", err)
	}

	e = newEnv(t)
	e.svc.d.Config.QueueMax = 1
	e.store.projects["p2"] = memProject{main: "main.tex", engine: domain.EnginePDFLaTeX, exists: true}
	e.start(t)
	if _, err := e.svc.Start(t.Context(), owner, "p2"); !errors.Is(err, domain.ErrBusy) {
		t.Errorf("queue full: err = %v", err)
	}
}

// Every change reaches every member (docs/realtime.md compile.updated).
func TestEventsReachMembers(t *testing.T) {
	e := newEnv(t)
	j := e.start(t)
	e.claim(t)
	if err := e.svc.StorePDF(t.Context(), j.ID, strings.NewReader("%PDF-1.5 x")); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Finish(t.Context(), j.ID, domain.Result{Outcome: domain.OutcomeCompleted}); err != nil {
		t.Fatal(err)
	}
	want := []domain.Status{domain.StatusQueued, domain.StatusRunning, domain.StatusSucceeded}
	if got := e.pub.statuses(); !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
	for _, s := range e.pub.sent {
		if !slices.Equal(s.to, []string{owner, editor, viewer}) {
			t.Errorf("sent to %v", s.to)
		}
	}
}

func TestClaimRecordsTheWorker(t *testing.T) {
	e := newEnv(t)
	if _, ok, err := e.svc.Claim(t.Context(), "worker-1"); ok || err != nil {
		t.Fatalf("empty queue: %v %v", ok, err)
	}
	if e.workers.seen != 1 {
		t.Errorf("seen = %d", e.workers.seen)
	}
	var field *domain.FieldError
	if _, _, err := e.svc.Claim(t.Context(), ""); !errors.As(err, &field) {
		t.Errorf("empty worker ID: %v", err)
	}
	if _, _, err := e.svc.Claim(t.Context(), strings.Repeat("w", 101)); !errors.As(err, &field) {
		t.Errorf("long worker ID: %v", err)
	}
}

// A figure is compiled only when its scan found it clean; the rest is recorded as skipped.
// Text and folders go into the tar at their project paths.
func TestSourceSkipsUncleanFigures(t *testing.T) {
	e := newEnv(t)
	j := e.start(t)
	e.claim(t)
	e.objects.objects["k/clean"] = []byte("PNGDATA")
	e.sources.src = domain.Source{MainFilePath: "main.tex", LiveConfirmed: false, Entries: []domain.SourceEntry{
		{Path: "figs", Kind: domain.EntryFolder},
		{Path: "figs/clean.png", Kind: domain.EntryFigure, Size: 7, ObjectKey: "k/clean", Scan: "clean"},
		{Path: "figs/pending.png", Kind: domain.EntryFigure, Size: 7, ObjectKey: "k/p", Scan: "pending"},
		{Path: "figs/bad.png", Kind: domain.EntryFigure, Size: 7, ObjectKey: "k/i", Scan: "infected"},
		{Path: "figs/failed.pdf", Kind: domain.EntryFigure, Size: 7, ObjectKey: "k/f", Scan: "failed"},
		{Path: "main.tex", Kind: domain.EntryText, Size: 5, Content: "hello"},
	}}
	p, err := e.svc.PrepareSource(t.Context(), j.ID)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := e.svc.WriteSource(t.Context(), p, &buf); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	tr := tar.NewReader(&buf)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		got[h.Name] = string(b)
	}
	want := map[string]string{"figs/": "", "figs/clean.png": "PNGDATA", "main.tex": "hello"}
	if len(got) != len(want) {
		t.Errorf("tar = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("tar[%s] = %q, want %q", k, got[k], v)
		}
	}
	job, _ := e.store.Get(t.Context(), project, j.ID)
	reasons := map[string]string{}
	for _, s := range job.Skipped {
		reasons[s.Path] = s.Reason
	}
	if reasons["figs/pending.png"] != domain.SkipScanPending || reasons["figs/bad.png"] != domain.SkipInfected ||
		reasons["figs/failed.pdf"] != domain.SkipScanFailed || len(reasons) != 3 {
		t.Errorf("skipped = %v", job.Skipped)
	}
	if job.LiveTextConfirmed {
		t.Error("live text not confirmed, recorded as confirmed")
	}
}

func TestSourceTooLargeFailsTheJob(t *testing.T) {
	e := newEnv(t)
	j := e.start(t)
	e.claim(t)
	e.sources.src.Entries = []domain.SourceEntry{{Path: "big.tex", Kind: domain.EntryText, Size: 101, Content: strings.Repeat("x", 101)}}
	if _, err := e.svc.PrepareSource(t.Context(), j.ID); !errors.Is(err, domain.ErrInputTooLarge) {
		t.Fatalf("err = %v", err)
	}
	job, _ := e.store.Get(t.Context(), project, j.ID)
	if job.Status != domain.StatusFailed || job.Failure == nil || *job.Failure != domain.FailInputTooLarge {
		t.Errorf("job = %s %v", job.Status, job.Failure)
	}
	if _, err := e.svc.Start(t.Context(), owner, project); err != nil {
		t.Errorf("the slot is free again: %v", err)
	}
}

func TestSourceNeedsARunningJob(t *testing.T) {
	e := newEnv(t)
	j := e.start(t)
	if _, err := e.svc.PrepareSource(t.Context(), j.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("queued job: err = %v", err)
	}
	e.claim(t)
	e.now = e.now.Add(Lease + time.Second)
	if _, err := e.svc.PrepareSource(t.Context(), j.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expired lease: err = %v", err)
	}
}

// The PDF is capped and must be a PDF (output caps, NFR-4).
func TestStorePDF(t *testing.T) {
	e := newEnv(t)
	j := e.start(t)
	e.claim(t)
	if err := e.svc.StorePDF(t.Context(), j.ID, strings.NewReader("%PDF-"+strings.Repeat("x", 60))); !errors.Is(err, domain.ErrPDFTooLarge) {
		t.Errorf("too large: err = %v", err)
	}
	if err := e.svc.StorePDF(t.Context(), j.ID, strings.NewReader("<html>")); !errors.Is(err, domain.ErrNotPDF) {
		t.Errorf("not a PDF: err = %v", err)
	}
	if len(e.objects.objects) != 0 {
		t.Errorf("a refused PDF was stored: %v", e.objects.objects)
	}
	if err := e.svc.StorePDF(t.Context(), j.ID, strings.NewReader("%PDF-1.5\n")); err != nil {
		t.Fatal(err)
	}
	if len(e.objects.objects) != 1 {
		t.Errorf("objects = %d", len(e.objects.objects))
	}
	for k := range e.objects.objects {
		if !strings.HasPrefix(k, "projects/p1/compiles/"+j.ID+"-") || !strings.HasSuffix(k, ".pdf") {
			t.Errorf("key %q", k)
		}
	}
}

// The status rule (docs/compile.md): succeeded when a PDF was stored, even with errors in
// the log; failed without one; timed_out after the 60 s kill (FR-13.5).
func TestFinishStatus(t *testing.T) {
	tests := []struct {
		name        string
		pdf         bool
		result      domain.Result
		wantStatus  domain.Status
		wantFailure domain.Failure
	}{
		{"PDF", true, domain.Result{Outcome: domain.OutcomeCompleted, Log: "./main.tex:3: Undefined control sequence.\nOutput written on main.pdf (10 pages, 1 bytes)."}, domain.StatusSucceeded, ""},
		{"no PDF", false, domain.Result{Outcome: domain.OutcomeCompleted, Log: "! Emergency stop."}, domain.StatusFailed, domain.FailNoPDF},
		{"timeout", true, domain.Result{Outcome: domain.OutcomeTimedOut}, domain.StatusTimedOut, ""},
		{"out of memory", false, domain.Result{Outcome: domain.OutcomeFailed, Failure: ptr(domain.FailOutOfMemory)}, domain.StatusFailed, domain.FailOutOfMemory},
		{"sandbox", false, domain.Result{Outcome: domain.OutcomeFailed}, domain.StatusFailed, domain.FailSandbox},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			j := e.start(t)
			e.claim(t)
			if tt.pdf {
				if err := e.svc.StorePDF(t.Context(), j.ID, strings.NewReader("%PDF-1.5")); err != nil {
					t.Fatal(err)
				}
			}
			if err := e.svc.Finish(t.Context(), j.ID, tt.result); err != nil {
				t.Fatal(err)
			}
			got, _ := e.store.Get(t.Context(), project, j.ID)
			if got.Status != tt.wantStatus || (tt.wantFailure == "") != (got.Failure == nil) ||
				(got.Failure != nil && *got.Failure != tt.wantFailure) {
				t.Errorf("status %s failure %v, want %s %s", got.Status, got.Failure, tt.wantStatus, tt.wantFailure)
			}
			if got.HasPDF != (tt.wantStatus == domain.StatusSucceeded) {
				t.Errorf("hasPdf = %v", got.HasPDF)
			}
			if !got.HasLog {
				t.Error("the newest log is kept")
			}
			if tt.name == "PDF" && (got.Pages == nil || *got.Pages != 10 || len(got.Problems) != 1) {
				t.Errorf("pages %v problems %v", got.Pages, got.Problems)
			}
		})
	}
}

func TestFinishRefusals(t *testing.T) {
	e := newEnv(t)
	j := e.start(t)
	e.claim(t)
	var field *domain.FieldError
	for _, r := range []domain.Result{
		{Outcome: "done"},
		{Outcome: domain.OutcomeCompleted, Failure: ptr(domain.FailOutOfMemory)},
		{Outcome: domain.OutcomeFailed, Failure: ptr(domain.FailWorkerLost)},
		{Outcome: domain.OutcomeCompleted, Duration: -1},
		{Outcome: domain.OutcomeCompleted, Log: strings.Repeat("x", MaxLogBytes+5000)},
	} {
		if err := e.svc.Finish(t.Context(), j.ID, r); !errors.As(err, &field) {
			t.Errorf("%+v: err = %v", r.Outcome, err)
		}
	}
	if err := e.svc.Finish(t.Context(), j.ID, domain.Result{Outcome: domain.OutcomeCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Finish(t.Context(), j.ID, domain.Result{Outcome: domain.OutcomeTimedOut}); err != nil {
		t.Errorf("a second report: %v", err)
	}
	if got, _ := e.store.Get(t.Context(), project, j.ID); got.Status != domain.StatusFailed {
		t.Errorf("a second report changed the job: %s", got.Status)
	}
	if err := e.svc.Finish(t.Context(), "nope", domain.Result{Outcome: domain.OutcomeCompleted}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown job: %v", err)
	}
}

// SRS 7, Retention: the log of the newest compile, the PDF of the newest that succeeded.
func TestRetentionKeepsTheLastSuccessfulPDF(t *testing.T) {
	e := newEnv(t)
	e.svc.d.Limiter = nil
	run := func(pdf bool) string {
		j := e.start(t)
		e.claim(t)
		if pdf {
			if err := e.svc.StorePDF(t.Context(), j.ID, strings.NewReader("%PDF-1.5")); err != nil {
				t.Fatal(err)
			}
		}
		if err := e.svc.Finish(t.Context(), j.ID, domain.Result{Outcome: domain.OutcomeCompleted, Log: "log of " + j.ID}); err != nil {
			t.Fatal(err)
		}
		return j.ID
	}
	good := run(true)
	bad := run(false)
	pdf, err := e.store.LatestPDF(t.Context(), project)
	if err != nil || pdf.CompileID != good {
		t.Fatalf("PDF after a failure = %v %v, want %s's", pdf.CompileID, err, good)
	}
	if _, err := e.store.Log(t.Context(), project, good); !errors.Is(err, domain.ErrNoLog) {
		t.Errorf("older log kept: %v", err)
	}
	if l, err := e.store.Log(t.Context(), project, bad); err != nil || l != "log of "+bad {
		t.Errorf("newest log = %q %v", l, err)
	}
	newer := run(true)
	if pdf, _ := e.store.LatestPDF(t.Context(), project); pdf.CompileID != newer {
		t.Errorf("PDF = %s, want the newest success", pdf.CompileID)
	}
	for range KeepJobs + 3 {
		run(false)
	}
	if _, err := e.store.Get(t.Context(), project, newer); err != nil {
		t.Errorf("the job holding the PDF was dropped: %v", err)
	}
	if _, err := e.store.Get(t.Context(), project, good); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an old job was kept: %v", err)
	}
}

// A worker that stops never holds a project's slot: its lease ends; a job no worker takes
// is cancelled.
func TestSweep(t *testing.T) {
	e := newEnv(t)
	j := e.start(t)
	e.claim(t)
	e.now = e.now.Add(Lease + time.Second)
	if n, err := e.svc.Sweep(t.Context()); err != nil || n != 1 {
		t.Fatalf("sweep = %d %v", n, err)
	}
	if got, _ := e.store.Get(t.Context(), project, j.ID); got.Failure == nil || *got.Failure != domain.FailWorkerLost {
		t.Errorf("lost job = %v", got.Failure)
	}
	if err := e.svc.Finish(t.Context(), j.ID, domain.Result{Outcome: domain.OutcomeCompleted}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("finish after worker_lost: %v", err)
	}
	q := e.start(t)
	e.now = e.now.Add(QueueExpiry + time.Second)
	if n, _ := e.svc.Sweep(t.Context()); n != 1 {
		t.Fatalf("queued sweep = %d", n)
	}
	if got, _ := e.store.Get(t.Context(), project, q.ID); got.Status != domain.StatusCancelled {
		t.Errorf("queued job = %s", got.Status)
	}
}

func TestReadsAreForMembers(t *testing.T) {
	e := newEnv(t)
	j := e.start(t)
	if _, err := e.svc.Latest(t.Context(), viewer, project); err != nil {
		t.Errorf("viewer reads: %v", err)
	}
	if _, err := e.svc.Get(t.Context(), outside, project, j.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("non-member: %v", err)
	}
	if _, _, err := e.svc.OpenPDF(t.Context(), viewer, project); !errors.Is(err, domain.ErrNoPDF) {
		t.Errorf("no PDF yet: %v", err)
	}
	if _, err := e.svc.Log(t.Context(), viewer, project, j.ID); !errors.Is(err, domain.ErrNoLog) {
		t.Errorf("a queued job has no log: %v", err)
	}
}

func TestTruncateLog(t *testing.T) {
	s := strings.Repeat("é", 100)
	got := truncateLog(s, 120)
	if len(got) > 120 || !strings.HasSuffix(got, cutNote) || !utf8Valid(got) {
		t.Errorf("truncated = %q (%d bytes)", got, len(got))
	}
	if truncateLog("short", 120) != "short" {
		t.Error("a short log changed")
	}
	if truncateLog("bad \xff byte", 120) != "bad � byte" {
		t.Error("invalid UTF-8 kept")
	}
}

func utf8Valid(s string) bool { return strings.ToValidUTF8(s, "") == s }
