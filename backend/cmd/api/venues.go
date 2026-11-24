package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	// Venue deadlines are stated in IANA time zones (docs/decisions.md, 2026-11-24): the
	// database of zones is built in, so they resolve on an image without /usr/share/zoneinfo.
	_ "time/tzdata"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/adapter/feeds"
	eventhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/event/adapter/http"
	eventpg "github.com/ARAFATH-SH/researchsphere/backend/internal/event/adapter/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
	eventusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/event/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/config"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/logging"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/redis"
	recommendationredis "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/redis"
)

// venueSyncInterval is how often the API checks whether a source's daily run is due.
const venueSyncInterval = time.Hour

// The deadline repositories the import reads (docs/venues-sync.md; licences checked
// 2026-11-24, docs/decisions.md).
var (
	ccfRepo = feeds.RepoConfig{Owner: "ccfddl", Repo: "ccf-deadlines", Branch: "main", DataDir: "conference", Depth: 1, Format: feeds.FormatCCF}
	hciRepo = feeds.RepoConfig{Owner: "hci-deadlines", Repo: "conf-database", Branch: "main", DataDir: "conferences", Depth: 0, Format: feeds.FormatHCI}
)

// venueFeeds holds what the import reads from; tests replace the HTTP settings.
type venueFeeds struct {
	repos    map[domain.Source]domain.RepoFeed
	journals domain.JournalFeed
	links    domain.LinkChecker
	info     map[domain.Source]eventusecase.SourceInfo
	// now is the import's clock; nil means time.Now (tests set it).
	now func() time.Time
}

// newVenueFeeds builds the real feeds from the settings.
func newVenueFeeds(cfg config.Config) venueFeeds {
	ccf, hci := ccfRepo, hciRepo
	for _, r := range []*feeds.RepoConfig{&ccf, &hci} {
		r.APIBase, r.RawBase, r.Contact = cfg.GitHubAPIURL, cfg.GitHubRawURL, cfg.VenueSyncContactEmail
	}
	ccfFeed, hciFeed := feeds.NewRepo(ccf), feeds.NewRepo(hci)
	oa := feeds.NewOpenAlex(feeds.OpenAlexConfig{
		BaseURL: cfg.OpenAlexBaseURL, Mailto: cfg.VenueSyncContactEmail, APIKey: cfg.OpenAlexAPIKey,
		MinWorks: cfg.VenueSyncOpenAlexWorks, Subfields: eventusecase.JournalSubfields(),
	})
	return venueFeeds{
		repos:    map[domain.Source]domain.RepoFeed{domain.SourceCCF: ccfFeed, domain.SourceHCIDeadlines: hciFeed},
		journals: oa,
		links:    feeds.NewLinks(feeds.LinkConfig{Contact: cfg.VenueSyncContactEmail}),
		info: map[domain.Source]eventusecase.SourceInfo{
			domain.SourceCCF:          {Label: "ccf-deadlines (conferences)", Licence: "MIT", URL: ccfFeed.URL()},
			domain.SourceHCIDeadlines: {Label: "hci-deadlines (HCI conferences)", Licence: "MIT", URL: hciFeed.URL()},
			domain.SourceOpenAlex:     {Label: "OpenAlex (journals)", Licence: "CC0", URL: oa.URL()},
		},
	}
}

// newVenueSyncer wires the automatic import (FR-14.5). base is the context its background
// runs use.
// watcher is told after a run that wrote venues (venue recommendations, AI-6.1); nil is
// nobody.
func newVenueSyncer(base context.Context, pool *pgxpool.Pool, cfg config.Config, f venueFeeds, watcher domain.Watcher, logger *slog.Logger) *eventusecase.Syncer {
	return eventusecase.NewSyncer(base, eventusecase.SyncDeps{
		Store: eventpg.NewStore(pool), Sync: eventpg.NewSyncStore(pool), Tx: postgres.NewTransactor(pool),
		Repos: f.repos, Journals: f.journals, Links: f.links, Info: f.info, Log: logger, Now: f.now, Watcher: watcher,
	}, eventusecase.SyncConfig{
		Daily: cfg.VenueSyncDaily, Contact: cfg.VenueSyncContactEmail != "", MinInterval: cfg.VenueSyncMinInterval,
	})
}

// newVenues wires the event module (FR-14, FR-15.1): venues live in PostgreSQL, and the
// database's triggers queue their embeddings (AI-1.3). watcher is told about every committed
// venue change (venue recommendations, AI-6.1); nil is nobody.
func newVenues(pool *pgxpool.Pool, sync *eventusecase.Syncer, watcher domain.Watcher) *eventhttp.Handler {
	svc := eventusecase.New(eventpg.NewStore(pool), postgres.NewTransactor(pool), nil)
	if watcher != nil {
		svc.Watch(watcher)
	}
	return eventhttp.NewHandler(svc, sync, nil)
}

// runVenueSyncOnce runs the automatic import once now with the API's settings and prints
// each run's counts (make venue-sync). dry reads the sources and writes no venue.
func runVenueSyncOnce(sources string, dry bool) func(context.Context, func(string) string, io.Writer) error {
	return func(ctx context.Context, getenv func(string) string, logOut io.Writer) error {
		cfg, err := config.Load(getenv)
		if err != nil {
			return err
		}
		logger := logging.New(logOut, cfg.LogLevel)
		pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()
		if err := checkSchema(ctx, pool, logger); err != nil {
			return err
		}
		var names []string
		for _, s := range strings.Split(sources, ",") {
			if s = strings.TrimSpace(s); s != "" {
				names = append(names, s)
			}
		}
		// The running API's kept venue recommendations are made again after a run that wrote
		// venues; without Redis the run goes on and they expire within 10 minutes.
		var watcher domain.Watcher
		if rdb, err := redis.NewClient(cfg.RedisURL, logger); err != nil {
			logger.Warn("venue sync: Redis unavailable, kept venue recommendations are not renewed", slog.Any("error", err))
		} else {
			cache := recommendationredis.New(rdb)
			defer func() { _ = cache.Close(); _ = rdb.Close() }()
			watcher = venueGenerationRaiser{cache: cache, logger: logger}
		}
		syncer := newVenueSyncer(ctx, pool, cfg, newVenueFeeds(cfg), watcher, logger)
		runs, err := syncer.RunNow(ctx, names, dry)
		if err != nil {
			return err
		}
		failed := 0
		for _, r := range runs {
			_, _ = fmt.Fprintf(logOut, "%-14s dry_run=%t status=%s created=%d updated=%d unchanged=%d skipped=%d rejected=%d errors=%d commit=%s\n",
				r.Source, r.DryRun, r.Status, r.Created, r.Updated, r.Unchanged, r.Skipped, r.Rejected, len(r.Errors), r.CommitSHA)
			for _, e := range r.Errors {
				_, _ = fmt.Fprintf(logOut, "  error: %s\n", e)
			}
			if r.Status == domain.SyncFailed {
				failed++
			}
		}
		if failed > 0 {
			return fmt.Errorf("%d of %d runs failed", failed, len(runs))
		}
		return nil
	}
}
