// Command api runs the ResearchSphere API server. Settings come from environment
// variables; see .env.example in the repository root. This is the only package that
// wires modules and platform together.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/argon2"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/emaildomain"
	authhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/http"
	authpg "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/postgres"
	authredis "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/redis"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/smtp"
	authusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/usecase"
	healthpg "github.com/ARAFATH-SH/researchsphere/backend/internal/health/adapter/postgres"
	healthredis "github.com/ARAFATH-SH/researchsphere/backend/internal/health/adapter/redis"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/clamav"
	mediahttp "github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/http"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/imaging"
	mediapg "github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/postgres"
	mediaredis "github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/redis"
	mediausecase "github.com/ARAFATH-SH/researchsphere/backend/internal/media/usecase"
	messaginghttp "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/http"
	messagingpg "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/postgres"
	messagingredis "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/redis"
	messagingusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/usecase"
	moderationhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/adapter/http"
	moderationpg "github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/adapter/postgres"
	moderationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/usecase"
	notificationhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/adapter/http"
	notificationpg "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/adapter/postgres"
	notificationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/config"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpserver"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/logging"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/redis"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/storage"
	profilehttp "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/http"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/openalex"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/orcid"
	profilepg "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/postgres"
	profileredis "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/redis"
	profileusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
	realtimehttp "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/adapter/http"
	realtimeusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/usecase"
	socialhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/social/adapter/http"
	socialpg "github.com/ARAFATH-SH/researchsphere/backend/internal/social/adapter/postgres"
	socialredis "github.com/ARAFATH-SH/researchsphere/backend/internal/social/adapter/redis"
	socialusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/social/usecase"
)

// accessTTL is how long an access token is valid (FR-2.1).
const accessTTL = 15 * time.Minute

// cleanupInterval is how often queued objects are deleted from storage (docs/decisions.md).
const cleanupInterval = time.Minute

// attachmentPurgeInterval is how often attachments not sent within a day are deleted (FR-16.7).
const attachmentPurgeInterval = 10 * time.Minute

// unverifiedPurgeInterval is how often accounts still unverified after
// UNVERIFIED_ACCOUNT_DAYS are deleted (FR-17.4).
const unverifiedPurgeInterval = time.Hour

func main() {
	purgeOnce := flag.Bool("purge-unverified", false, "delete the accounts still unverified after UNVERIFIED_ACCOUNT_DAYS once (FR-17.4), then exit; make purge-unverified runs it")
	digestsOnce := flag.Bool("send-digests", false, "send the unread message digests now due once (FR-16.11), then exit; make send-digests runs it")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runner := run
	switch {
	case *purgeOnce && *digestsOnce:
		fmt.Fprintln(os.Stderr, "api: use -purge-unverified or -send-digests, not both")
		stop()
		os.Exit(2)
	case *purgeOnce:
		runner = runPurgeOnce
	case *digestsOnce:
		runner = runDigestsOnce
	}
	if err := runner(ctx, os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		stop()
		os.Exit(1)
	}
}

// run starts the server and blocks until ctx is cancelled, then shuts down gracefully.
func run(ctx context.Context, getenv func(string) string, logOut io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}

	logger := logging.New(logOut, cfg.LogLevel)
	logger.Info("starting api", slog.Any("config", cfg))
	warnIgnoredSettings(cfg, logger)

	// No client connects until first used, so the server starts while the database,
	// Redis or storage is down; the health check reports it.
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	rdb, err := redis.NewClient(cfg.RedisURL, logger)
	if err != nil {
		return err
	}
	defer func() { _ = rdb.Close() }()

	files, err := storage.NewS3(cfg.Storage)
	if err != nil {
		return err
	}
	mediaStore := mediapg.NewStore(pool)
	// Deletes the objects of stored_files rows that are gone; stops with ctx.
	go mediausecase.NewCleaner(mediaStore, files, logger).Run(ctx, cleanupInterval)
	uploads := mediaredis.New(rdb)
	defer func() { _ = uploads.Close() }()
	images := mediausecase.NewImages(mediaStore, files, imaging.Codec{}, uploads, logger)

	attempts := authredis.New(rdb)
	defer func() { _ = attempts.Close() }()

	connectLimiter := socialredis.New(rdb)
	defer func() { _ = connectLimiter.Close() }()
	// Messaging and social ask what a user may do now; moderation needs messaging, so this part
	// stands on its own.
	moderationStore := moderationpg.NewStore(pool)
	standings := moderationusecase.NewStandings(moderationStore, time.Now)
	// Real-time delivery hears from most modules and reads from some; bind closes the loop.
	realtime := newRealtime(rdb, standings, realtimeusecase.DefaultConfig(), logger)
	defer realtime.close()
	go realtime.hub.Run(ctx)
	titles := &subjectTitles{}
	notifications := notificationusecase.New(notificationpg.NewStore(pool), notificationusecase.WithPublisher(realtime.events),
		notificationusecase.WithSubjectTitles(titles))
	messageLimiter := messagingredis.New(rdb)
	defer func() { _ = messageLimiter.Close() }()
	// Messaging asks social who is connected or blocked, and social tells messaging about
	// new connections and blocks, so the checker gets social once that exists.
	connected := &connectionChecker{}
	messaging := messagingusecase.New(messagingpg.NewStore(pool), postgres.NewTransactor(pool), connected, connected, messagingActors{standings: standings}, messageLimiter,
		realtime.events, groupNotifier{notifications: notifications}, messagingusecase.Limits{
			MessagesPerMinute: cfg.MessagesPerMinute, MessagesPerDay: cfg.MessagesPerDay,
			RequestsPerMinute: cfg.RequestsPerMinute, RequestsPerDay: cfg.RequestsPerDay,
			EditWindow: cfg.MessageEditWindow, SearchesPerMinute: cfg.SearchesPerMinute,
		}, time.Now, logger)
	// A connection change and its notification are written in one transaction (FR-9.1).
	social := socialusecase.New(socialpg.NewStore(pool), postgres.NewTransactor(pool), connectLimiter,
		connectionNotifier{notifications: notifications, messaging: messaging, events: realtime.events}, socialActors{standings: standings}, logger)
	connected.social = social
	titles.messaging = messaging
	realtime.bind(messaging, notifications)
	// Attachments (FR-16.7): media checks types and scans, messaging owns who may see a file.
	messaging.UseFiles(messagingusecase.FileDeps{Intake: fileIntake{media: mediausecase.NewAttachments(images, imaging.Codec{})}, Objects: files})
	go messaging.RunAttachmentPurge(ctx, attachmentPurgeInterval)
	go mediausecase.NewScanWorker(mediaStore, postgres.NewTransactor(pool), scanObjects{files: files}, files, clamav.New(cfg.ClamAVAddr),
		scanListener{messaging: messaging, notifications: notifications, logger: logger}, logger).Run(ctx)

	lookups := profileredis.New(rdb)
	defer func() { _ = lookups.Close() }()
	profileStore := profilepg.NewStore(pool)
	profiles := profileusecase.New(profileStore,
		profileusecase.WithDOILookup(profileusecase.DOILookup{
			Source:  openalex.New(openalex.Config{BaseURL: cfg.OpenAlexBaseURL, Mailto: cfg.OpenAlexMailto}),
			Cache:   lookups,
			Limiter: lookups,
			Logger:  logger,
		}),
		profileusecase.WithORCIDImport(profileusecase.ORCIDImport{
			Source:  orcid.New(orcid.Config{BaseURL: cfg.ORCIDBaseURL}),
			Cache:   lookups,
			Limiter: lookups.ORCIDLimiter(),
			Logger:  logger,
		}),
		profileusecase.WithPeopleSearch(profileusecase.PeopleSearch{
			Searcher: profileStore,
			Limiter:  lookups.SearchLimiter(),
			Logger:   logger,
		}),
		profileusecase.WithConnections(connectionLookup{social: social}),
		profileusecase.WithConversations(conversationLookup{messaging: messaging}),
	)

	tokens := accesstoken.New(cfg.JWTSecret, accessTTL)
	mailer := newMailer(cfg)
	// The unread message digest (FR-16.11): hourly, batched, safe with several instances.
	digests := newDigests(messagingpg.NewStore(pool), postgres.NewTransactor(pool), mailer, cfg, logger)
	go digests.RunDigests(ctx, digestInterval)
	authCfg := authConfig(cfg, logger)
	authCfg.Sessions, authCfg.Restrictions = realtime.events, standings
	auth := authusecase.New(authpg.NewStore(pool), argon2.New(argon2.DefaultParams), mailer, tokens, attempts, authCfg, time.Now, logger)
	go auth.RunUnverifiedPurge(ctx, unverifiedPurgeInterval)
	moderation := moderationusecase.New(moderationStore, postgres.NewTransactor(pool), moderationAccounts{auth: auth, events: realtime.events},
		moderationReports{messaging: messaging}, moderationNotifier{notifications: notifications},
		moderationMailer{mailer: mailer}, time.Now, logger)
	handler := newRouter(logger, healthpg.NewPinger(pool), healthredis.NewPinger(rdb), files,
		authhttp.NewHandler(auth, authhttp.CookieConfig{Secure: cfg.CookieSecure}),
		profilehttp.NewHandler(profiles),
		mediahttp.NewHandler(images),
		socialhttp.NewHandler(social),
		notificationhttp.NewHandler(notifications),
		messaginghttp.NewHandler(messaging, messaginghttp.WithDigests(digests)),
		moderationhttp.NewHandler(moderation),
		realtimehttp.NewHandler(realtime.hub, cfg.AppBaseURL, logger),
		httpx.RequireAuth(tokens, time.Now))

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", ":"+strconv.Itoa(cfg.Port))
	if err != nil {
		return fmt.Errorf("listen on port %d: %w", cfg.Port, err)
	}
	return httpserver.Serve(ctx, ln, handler, logger, realtime.hub.Shutdown)
}

// newMailer sends through the SMTP server the settings name: Mailpit locally, a provider in
// production (FR-17.1).
func newMailer(cfg config.Config) *smtp.Mailer {
	return smtp.New(smtp.Options{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort, From: cfg.MailFrom, TLS: smtp.TLSMode(cfg.SMTPTLS),
		Username: cfg.SMTPUsername, Password: cfg.SMTPPassword,
	})
}

// authConfig holds the auth settings that come from the environment: the sign-up domain
// checks (FR-17.2), the test domains (FR-17.9, none in production) and the age at which an
// unverified account is deleted (FR-17.4).
func authConfig(cfg config.Config, logger *slog.Logger) authusecase.Config {
	return authusecase.Config{
		AppBaseURL:           cfg.AppBaseURL,
		EmailDomains:         emaildomain.New(logger),
		TestEmailDomains:     cfg.TestEmailDomains,
		UnverifiedAccountAge: time.Duration(cfg.UnverifiedAccountDays) * 24 * time.Hour,
	}
}

// warnIgnoredSettings logs the settings production ignores (FR-17.9).
func warnIgnoredSettings(cfg config.Config, logger *slog.Logger) {
	if cfg.TestEmailDomainsIgnored {
		logger.Warn("TEST_EMAIL_DOMAINS is set but ignored in production: every address gets the domain checks and the purge")
	}
}

// runPurgeOnce runs one pass of the purge of unverified accounts (FR-17.4) with the API's
// settings, logs what it deleted and returns. `make purge-unverified` runs it, so the job
// can be tried without waiting for the hourly loop.
func runPurgeOnce(ctx context.Context, getenv func(string) string, logOut io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	logger := logging.New(logOut, cfg.LogLevel)
	warnIgnoredSettings(cfg, logger)
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	rdb, err := redis.NewClient(cfg.RedisURL, logger)
	if err != nil {
		return err
	}
	defer func() { _ = rdb.Close() }()
	attempts := authredis.New(rdb)
	defer func() { _ = attempts.Close() }()

	auth := authusecase.New(authpg.NewStore(pool), argon2.New(argon2.DefaultParams), newMailer(cfg),
		accesstoken.New(cfg.JWTSecret, accessTTL), attempts, authConfig(cfg, logger), time.Now, logger)
	n, err := auth.PurgeUnverified(ctx)
	if err != nil {
		return err
	}
	logger.Info("purge of unverified accounts finished", slog.Int("deleted", n),
		slog.Int("unverified_account_days", cfg.UnverifiedAccountDays), slog.Any("kept_test_domains", cfg.TestEmailDomains))
	return nil
}
