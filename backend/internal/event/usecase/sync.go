package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
)

// The automatic venue import (FR-14.5; docs/venues-sync.md): once a day, and when an
// administrator asks, each source is read politely and its entries are upserted by source
// and key, with the 24.1 rules. Venues added by hand or by a file, removed, or edited by an
// administrator after import are never changed.

// Defaults of SyncConfig.
const (
	DefaultSyncMinInterval  = time.Hour
	DefaultSyncRunTimeout   = 20 * time.Minute
	DefaultSyncScheduleGap  = 20 * time.Hour
	DefaultSyncPastWindow   = 90 * 24 * time.Hour
	DefaultSyncLinkRecheck  = 30 * 24 * time.Hour
	DefaultSyncLinkParallel = 8
	// MaxSyncRejects is how many rejected entries a run keeps.
	MaxSyncRejects = 200
	// maxSyncErrors is how many error messages a run keeps.
	maxSyncErrors = 50
)

// ErrSyncUnconfigured: no contact address is set, so no source may be read.
var ErrSyncUnconfigured = errors.New("venue sync has no contact address")

// SyncConfig sets the import's behaviour. Zero durations take the defaults.
type SyncConfig struct {
	// Daily turns the daily schedule on.
	Daily bool
	// Contact says whether a contact address is set; without it no run starts.
	Contact bool
	// MinInterval is the least time between two real runs of a source asked for by an
	// administrator.
	MinInterval time.Duration
	// RunTimeout bounds one run; the lease of a run is RunTimeout plus 5 minutes.
	RunTimeout time.Duration
	// ScheduleGap is the least time between two scheduled runs of a source.
	ScheduleGap time.Duration
	// PastWindow: an edition whose deadline closed longer ago is left alone.
	PastWindow time.Duration
	// LinkRecheck is how long a homepage check is trusted; LinkParallel how many run at once.
	LinkRecheck  time.Duration
	LinkParallel int
}

// SourceInfo describes a source for the status.
type SourceInfo struct {
	Label   string
	Licence string
	URL     string
}

// Syncer runs the automatic import.
type Syncer struct {
	store    domain.Store
	sync     domain.SyncStore
	tx       domain.Transactor
	repos    map[domain.Source]domain.RepoFeed
	journals domain.JournalFeed
	links    domain.LinkChecker
	info     map[domain.Source]SourceInfo
	cfg      SyncConfig
	now      func() time.Time
	log      *slog.Logger
	watcher  domain.Watcher // nil: nobody is told

	// base is the context of background runs: the server's, so they stop with it.
	base context.Context
	wg   sync.WaitGroup
}

// SyncDeps are the Syncer's collaborators. repos holds the conference sources' feeds.
type SyncDeps struct {
	Store    domain.Store
	Sync     domain.SyncStore
	Tx       domain.Transactor
	Repos    map[domain.Source]domain.RepoFeed
	Journals domain.JournalFeed
	Links    domain.LinkChecker
	Info     map[domain.Source]SourceInfo
	Now      func() time.Time
	Log      *slog.Logger
	// Watcher, when set, is told once after a real run that created or updated venues.
	Watcher domain.Watcher
}

// NewSyncer returns a Syncer. base is the context background runs use: they stop when it
// ends.
func NewSyncer(base context.Context, d SyncDeps, cfg SyncConfig) *Syncer {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.MinInterval <= 0 {
		cfg.MinInterval = DefaultSyncMinInterval
	}
	if cfg.RunTimeout <= 0 {
		cfg.RunTimeout = DefaultSyncRunTimeout
	}
	if cfg.ScheduleGap <= 0 {
		cfg.ScheduleGap = DefaultSyncScheduleGap
	}
	if cfg.PastWindow <= 0 {
		cfg.PastWindow = DefaultSyncPastWindow
	}
	if cfg.LinkRecheck <= 0 {
		cfg.LinkRecheck = DefaultSyncLinkRecheck
	}
	if cfg.LinkParallel <= 0 {
		cfg.LinkParallel = DefaultSyncLinkParallel
	}
	return &Syncer{store: d.Store, sync: d.Sync, tx: d.Tx, repos: d.Repos, journals: d.Journals, links: d.Links,
		info: d.Info, cfg: cfg, now: d.Now, log: d.Log, watcher: d.Watcher, base: base}
}

func (y *Syncer) lease() time.Duration { return y.cfg.RunTimeout + 5*time.Minute }

// parseSources reads the sources asked for: every source when none.
func parseSources(names []string) ([]domain.Source, error) {
	if len(names) == 0 {
		return slices.Clone(domain.SyncSources), nil
	}
	var out []domain.Source
	for _, n := range names {
		s := domain.Source(strings.TrimSpace(n))
		if !s.IsSync() {
			return nil, domain.Invalid("sources", domain.CodeInvalid)
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	// In the order they run: ccfddl before hci_deadlines, whose duplicates it decides.
	slices.SortFunc(out, func(a, b domain.Source) int {
		return slices.Index(domain.SyncSources, a) - slices.Index(domain.SyncSources, b)
	})
	return out, nil
}

// claim starts a run of every source, or none: ErrSyncRunning when one is in progress,
// ErrSyncTooSoon (checkInterval) or ErrSyncNotDue (scheduled) when it ran too recently.
func (y *Syncer) claim(ctx context.Context, sources []domain.Source, dry bool, trigger domain.SyncTrigger, by string, checkInterval bool) ([]domain.SyncRun, error) {
	now := y.now()
	var runs []domain.SyncRun
	err := y.tx.InTx(ctx, func(ctx context.Context) error {
		runs = nil
		for _, src := range sources {
			st, err := y.sync.LockSource(ctx, src)
			if err != nil {
				return err
			}
			if !st.LeaseUntil.IsZero() && st.LeaseUntil.After(now) {
				return fmt.Errorf("%s: %w", src, domain.ErrSyncRunning)
			}
			if err := y.sync.FailStaleRuns(ctx, src, now); err != nil {
				return err
			}
			switch {
			case trigger == domain.TriggerSchedule && !st.LastScheduledAt.IsZero() && now.Sub(st.LastScheduledAt) < y.cfg.ScheduleGap:
				return fmt.Errorf("%s: %w", src, domain.ErrSyncNotDue)
			case checkInterval && !dry && !st.LastRunAt.IsZero() && now.Sub(st.LastRunAt) < y.cfg.MinInterval:
				return fmt.Errorf("%s: %w", src, domain.ErrSyncTooSoon)
			}
			st.LeaseUntil = now.Add(y.lease())
			if trigger == domain.TriggerSchedule {
				st.LastScheduledAt = now
			}
			if !dry {
				st.LastRunAt = now
			}
			if err := y.sync.SaveSource(ctx, st); err != nil {
				return err
			}
			r := domain.SyncRun{Source: src, DryRun: dry, Trigger: trigger, RequestedBy: by, StartedAt: now, Status: domain.SyncRunning}
			if r.ID, err = y.sync.StartRun(ctx, r); err != nil {
				return err
			}
			runs = append(runs, r)
		}
		return nil
	})
	return runs, err
}

// Start begins a run of each source asked for (every source when none) in the background,
// for an administrator (FR-14.5), and returns the runs. A real run needs MinInterval since
// the last real run of its source.
func (y *Syncer) Start(ctx context.Context, adminID string, sources []string, dry bool) ([]domain.SyncRun, error) {
	srcs, err := parseSources(sources)
	if err != nil {
		return nil, err
	}
	if !y.cfg.Contact {
		return nil, ErrSyncUnconfigured
	}
	runs, err := y.claim(ctx, srcs, dry, domain.TriggerOnDemand, adminID, true)
	if err != nil {
		return nil, err
	}
	// The goroutine fills in its own copy: the caller reads the runs as started.
	work := slices.Clone(runs)
	y.wg.Add(1)
	go func() {
		defer y.wg.Done()
		for i := range work {
			y.execute(y.base, &work[i])
		}
	}()
	return runs, nil
}

// Wait waits for the background runs Start began.
func (y *Syncer) Wait() { y.wg.Wait() }

// RunNow runs each source asked for at once and returns the finished runs: the command
// (make venue-sync). It ignores MinInterval, not a run in progress.
func (y *Syncer) RunNow(ctx context.Context, sources []string, dry bool) ([]domain.SyncRun, error) {
	srcs, err := parseSources(sources)
	if err != nil {
		return nil, err
	}
	if !y.cfg.Contact {
		return nil, ErrSyncUnconfigured
	}
	runs, err := y.claim(ctx, srcs, dry, domain.TriggerCommand, "", false)
	if err != nil {
		return nil, err
	}
	for i := range runs {
		y.execute(ctx, &runs[i])
	}
	return runs, nil
}

// RunDue runs each source whose scheduled run is due, one after another: at most once per
// ScheduleGap, whichever API instance gets there first.
func (y *Syncer) RunDue(ctx context.Context) {
	if !y.cfg.Daily || !y.cfg.Contact {
		return
	}
	for _, src := range domain.SyncSources {
		runs, err := y.claim(ctx, []domain.Source{src}, false, domain.TriggerSchedule, "", false)
		switch {
		case errors.Is(err, domain.ErrSyncNotDue), errors.Is(err, domain.ErrSyncRunning):
			continue
		case err != nil:
			y.log.Error("venue sync: start a scheduled run", slog.String("source", string(src)), slog.Any("err", err))
			continue
		}
		y.execute(ctx, &runs[0])
	}
}

// RunSchedule checks every interval, from now, whether a scheduled run is due, until ctx
// ends.
func (y *Syncer) RunSchedule(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		y.RunDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// execute runs one claimed run to its end and stores its outcome. It never returns an
// error: a failure is the run's status, with its message.
func (y *Syncer) execute(ctx context.Context, run *domain.SyncRun) {
	runCtx, cancel := context.WithTimeout(ctx, y.cfg.RunTimeout)
	err := y.runSource(runCtx, run)
	cancel()
	run.FinishedAt = y.now()
	run.Status = domain.SyncSucceeded
	if err != nil {
		run.Status = domain.SyncFailed
		run.Errors = appendError(run.Errors, err.Error())
	}
	// Stored even when ctx has ended, so the run never stays "running".
	finishCtx, cancelFinish := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancelFinish()
	ferr := y.tx.InTx(finishCtx, func(ctx context.Context) error {
		if err := y.sync.FinishRun(ctx, *run); err != nil {
			return err
		}
		st, err := y.sync.LockSource(ctx, run.Source)
		if err != nil {
			return err
		}
		st.LeaseUntil = time.Time{}
		return y.sync.SaveSource(ctx, st)
	})
	// Venues written by a run are committed even when it failed later, so the watcher is told
	// on any real run that created or updated one.
	if !run.DryRun && run.Created+run.Updated > 0 && y.watcher != nil {
		y.watcher.VenuesChanged(finishCtx)
	}
	attrs := []any{slog.String("source", string(run.Source)), slog.String("run", run.ID), slog.Bool("dry_run", run.DryRun),
		slog.String("status", string(run.Status)), slog.Int("created", run.Created), slog.Int("updated", run.Updated),
		slog.Int("unchanged", run.Unchanged), slog.Int("skipped", run.Skipped), slog.Int("rejected", run.Rejected),
		slog.Int("errors", len(run.Errors))}
	switch {
	case ferr != nil:
		y.log.Error("venue sync: store the run's outcome", append(attrs, slog.Any("err", ferr))...)
	case err != nil:
		y.log.Warn("venue sync run failed", append(attrs, slog.Any("err", err))...)
	default:
		y.log.Info("venue sync run finished", attrs...)
	}
}

func appendError(errs []string, msg string) []string {
	if len(errs) >= maxSyncErrors {
		return errs
	}
	if r := []rune(msg); len(r) > 500 {
		msg = string(r[:500])
	}
	return append(errs, msg)
}

// runSource reads one source, writes its venues, then (on a real run) stores what it read.
func (y *Syncer) runSource(ctx context.Context, run *domain.SyncRun) error {
	var (
		cands []candidate
		save  func(ctx context.Context) error
		err   error
	)
	switch run.Source {
	case domain.SourceCCF, domain.SourceHCIDeadlines:
		cands, save, err = y.conferenceCandidates(ctx, run)
	case domain.SourceOpenAlex:
		cands, save, err = y.journalCandidates(ctx, run)
	default:
		err = fmt.Errorf("unknown source %q", run.Source)
	}
	if err != nil {
		return err
	}
	if err := y.write(ctx, run, cands); err != nil {
		return err
	}
	if run.DryRun || save == nil {
		return nil
	}
	return y.tx.InTx(ctx, save)
}

// conferenceCandidates reads a repository and maps its editions.
func (y *Syncer) conferenceCandidates(ctx context.Context, run *domain.SyncRun) ([]candidate, func(context.Context) error, error) {
	feed := y.repos[run.Source]
	if feed == nil {
		return nil, nil, fmt.Errorf("no feed for %s", run.Source)
	}
	states, err := y.sync.Sources(ctx)
	if err != nil {
		return nil, nil, err
	}
	var prev domain.RepoState
	for _, s := range states {
		if s.Source == run.Source {
			prev = s.Repo
		}
	}
	stored, err := y.sync.Files(ctx, run.Source)
	if err != nil {
		return nil, nil, err
	}
	if len(stored) == 0 {
		// Nothing to fall back on if the commit is the same: read everything.
		prev = domain.RepoState{}
	}
	fetch, err := feed.Fetch(ctx, prev)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", sourceLabels[run.Source], err)
	}
	files := mergeFiles(stored, fetch)
	run.CommitSHA = fetch.State.CommitSHA

	editions, ferrs := feed.Parse(files)
	for _, fe := range ferrs {
		run.Errors = appendError(run.Errors, fe.Path+": "+fe.Message)
	}
	// ccfddl goes first: hci-deadlines leaves out an edition it has (title and year).
	var taken map[string]bool
	if run.Source == domain.SourceHCIDeadlines {
		if taken, err = y.ccfEditions(ctx); err != nil {
			return nil, nil, err
		}
	}
	now := y.now()
	var cands []candidate
	for _, e := range editions {
		c, keep := mapEdition(run.Source, e, now, y.cfg.PastWindow)
		if !keep {
			continue
		}
		c.skip = taken[editionKey(e)]
		cands = append(cands, c)
	}
	save := func(ctx context.Context) error {
		if err := y.sync.SaveRepoState(ctx, run.Source, fetch.State); err != nil {
			return err
		}
		if !fetch.Changed {
			return nil
		}
		return y.sync.SaveFiles(ctx, run.Source, fetch.Full, fetch.Files, fetch.Deleted)
	}
	return cands, save, nil
}

// mergeFiles is the data files after a fetch: the stored ones when nothing changed, the
// fetched ones when they are the full set, else the stored ones with the changes applied.
func mergeFiles(stored []domain.RepoFile, f domain.RepoFetch) []domain.RepoFile {
	switch {
	case !f.Changed:
		return stored
	case f.Full:
		return f.Files
	}
	byPath := map[string]string{}
	for _, s := range stored {
		byPath[s.Path] = s.Content
	}
	for _, d := range f.Deleted {
		delete(byPath, d)
	}
	for _, c := range f.Files {
		byPath[c.Path] = c.Content
	}
	out := make([]domain.RepoFile, 0, len(byPath))
	for p, c := range byPath {
		out = append(out, domain.RepoFile{Path: p, Content: c})
	}
	slices.SortFunc(out, func(a, b domain.RepoFile) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// editionKey identifies a conference edition across sources: its short name and year.
func editionKey(e domain.Edition) string {
	return strings.ToLower(strings.Join(strings.Fields(e.Title), " ")) + "|" + strconv.Itoa(e.Year)
}

// ccfEditions is the editions ccfddl's stored files list, by editionKey.
func (y *Syncer) ccfEditions(ctx context.Context) (map[string]bool, error) {
	feed := y.repos[domain.SourceCCF]
	if feed == nil {
		return nil, nil
	}
	files, err := y.sync.Files(ctx, domain.SourceCCF)
	if err != nil {
		return nil, err
	}
	editions, _ := feed.Parse(files)
	out := make(map[string]bool, len(editions))
	for _, e := range editions {
		out[editionKey(e)] = true
	}
	return out, nil
}

// journalCandidates reads OpenAlex, maps the journals and sets their links.
func (y *Syncer) journalCandidates(ctx context.Context, run *domain.SyncRun) ([]candidate, func(context.Context) error, error) {
	if y.journals == nil {
		return nil, nil, errors.New("no journal feed")
	}
	journals, err := y.journals.Journals(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("read OpenAlex: %w", err)
	}
	var (
		cands     []candidate
		homepages []string
		issns     []string
	)
	for _, j := range journals {
		c, keep := mapJournal(j)
		if !keep {
			continue
		}
		cands = append(cands, c)
		homepages = append(homepages, httpsLink(j.HomepageURL))
		issns = append(issns, strings.TrimSpace(j.ISSNL))
	}
	var urls []string
	for _, u := range homepages {
		if u != "" {
			urls = append(urls, u)
		}
	}
	cached, err := y.sync.LinkChecks(ctx, urls)
	if err != nil {
		return nil, nil, err
	}
	fresh := resolveLinks(ctx, cands, homepages, issns, cached, y.links, y.now(), y.cfg.LinkRecheck, y.cfg.LinkParallel)
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("check journal homepages: %w", err)
	}
	save := func(ctx context.Context) error { return y.sync.SaveLinkChecks(ctx, fresh) }
	return cands, save, nil
}

// write checks every candidate with the 24.1 rules and upserts it, in one transaction under
// the import lock. A dry run rolls it back.
func (y *Syncer) write(ctx context.Context, run *domain.SyncRun, cands []candidate) error {
	known, err := y.catalog(ctx)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(cands))
	for _, c := range cands {
		if c.key != "" {
			keys = append(keys, c.key)
		}
	}
	var res domain.SyncRun
	err = y.tx.InTx(ctx, func(ctx context.Context) error {
		res = domain.SyncRun{}
		if err := y.store.LockImports(ctx); err != nil {
			return err
		}
		if err := y.store.LowEmbeddingPriority(ctx); err != nil {
			return err
		}
		existing, err := y.store.ImportedByKeys(ctx, run.Source, keys)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, c := range cands {
			if c.skip {
				res.Skipped++
				continue
			}
			v, problems := validate(c.in, known)
			problems = append(slices.Clone(c.problems), problems...)
			problems = dedupeProblems(problems)
			if c.key != "" {
				if seen[c.key] {
					problems = append(problems, domain.Problem{Field: "sourceKey", Code: domain.CodeDuplicateInFile})
				}
				seen[c.key] = true
			}
			if len(problems) > 0 {
				res.Rejected++
				if len(res.Rejects) < MaxSyncRejects {
					res.Rejects = append(res.Rejects, domain.SyncReject{SourceKey: c.key, Name: c.name, Problems: problems})
				}
				continue
			}
			v.Source, v.SourceKey, v.LinkKind = run.Source, c.key, c.linkKind
			ex, found := existing[c.key]
			switch {
			case found && (ex.Removed || ex.Venue.Locked()):
				res.Skipped++
			case found:
				changed := changedFields(ex.Venue, v)
				if len(changed) == 0 {
					res.Unchanged++
					continue
				}
				res.Updated++
				v.ID = ex.Venue.ID
				if err := y.store.Update(ctx, v); err != nil {
					return err
				}
				if err := y.store.Record(ctx, domain.Action{VenueID: v.ID, Actor: domain.ActorImport, AdminName: domain.ImportActorName,
					Kind: domain.ActImportUpdate, ChangedFields: changed, ImportID: run.ID}); err != nil {
					return err
				}
			default:
				res.Created++
				id, err := y.store.Create(ctx, v, "")
				if err != nil {
					return err
				}
				if err := y.store.Record(ctx, domain.Action{VenueID: id, Actor: domain.ActorImport, AdminName: domain.ImportActorName,
					Kind: domain.ActImportCreate, ImportID: run.ID}); err != nil {
					return err
				}
			}
		}
		if run.DryRun {
			return errDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return err
	}
	run.Created, run.Updated, run.Unchanged, run.Skipped, run.Rejected = res.Created, res.Updated, res.Unchanged, res.Skipped, res.Rejected
	run.Rejects = res.Rejects
	return nil
}

// dedupeProblems keeps the first of each field and code: a candidate's own problem and the
// rule's (a missing place) may say the same.
func dedupeProblems(ps []domain.Problem) []domain.Problem {
	var out []domain.Problem
	fields := map[string]bool{}
	for _, p := range ps {
		if fields[p.Field] {
			continue
		}
		fields[p.Field] = true
		out = append(out, p)
	}
	return out
}

func (y *Syncer) catalog(ctx context.Context) (catalog, error) {
	fields, err := y.store.Fields(ctx)
	if err != nil {
		return catalog{}, fmt.Errorf("read research fields: %w", err)
	}
	return newCatalog(fields), nil
}

// SourceStatus is one source in the status (GET /admin/venues/sync).
type SourceStatus struct {
	Source     domain.Source
	Info       SourceInfo
	CommitSHA  string
	LastRun    *domain.SyncRun
	LastDryRun *domain.SyncRun
	NextRunAt  time.Time // zero while the schedule is off
}

// Daily says whether the daily schedule is on.
func (y *Syncer) Daily() bool { return y.cfg.Daily }

// Status returns every source's state and latest runs.
func (y *Syncer) Status(ctx context.Context) ([]SourceStatus, error) {
	states, err := y.sync.Sources(ctx)
	if err != nil {
		return nil, err
	}
	byName := map[domain.Source]domain.SourceState{}
	for _, s := range states {
		byName[s.Source] = s
	}
	out := make([]SourceStatus, 0, len(domain.SyncSources))
	for _, src := range domain.SyncSources {
		st := byName[src]
		s := SourceStatus{Source: src, Info: y.info[src], CommitSHA: st.Repo.CommitSHA}
		for _, dry := range []bool{false, true} {
			r, found, err := y.sync.LatestRun(ctx, src, dry)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			if dry {
				s.LastDryRun = &r
			} else {
				s.LastRun = &r
			}
		}
		if y.cfg.Daily && y.cfg.Contact {
			s.NextRunAt = y.now()
			if !st.LastScheduledAt.IsZero() {
				if next := st.LastScheduledAt.Add(y.cfg.ScheduleGap); next.After(s.NextRunAt) {
					s.NextRunAt = next
				}
			}
		}
		out = append(out, s)
	}
	return out, nil
}
