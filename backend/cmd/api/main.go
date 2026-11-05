// Command api runs the ResearchSphere API server. Settings come from environment
// variables; see .env.example in the repository root. This is the only package that
// wires modules and platform together.
package main

import (
	"context"
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
	authhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/http"
	authpg "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/postgres"
	authredis "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/redis"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/smtp"
	authusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/usecase"
	healthpg "github.com/ARAFATH-SH/researchsphere/backend/internal/health/adapter/postgres"
	healthredis "github.com/ARAFATH-SH/researchsphere/backend/internal/health/adapter/redis"
	mediahttp "github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/http"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/imaging"
	mediapg "github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/postgres"
	mediaredis "github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/redis"
	mediausecase "github.com/ARAFATH-SH/researchsphere/backend/internal/media/usecase"
	messaginghttp "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/http"
	messagingpg "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/postgres"
	messagingredis "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/redis"
	messagingusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/usecase"
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
	socialhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/social/adapter/http"
	socialpg "github.com/ARAFATH-SH/researchsphere/backend/internal/social/adapter/postgres"
	socialredis "github.com/ARAFATH-SH/researchsphere/backend/internal/social/adapter/redis"
	socialusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/social/usecase"
)

// accessTTL is how long an access token is valid (FR-2.1).
const accessTTL = 15 * time.Minute

// cleanupInterval is how often queued objects are deleted from storage (docs/decisions.md).
const cleanupInterval = time.Minute

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Getenv, os.Stdout); err != nil {
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
	notifications := notificationusecase.New(notificationpg.NewStore(pool))
	messageLimiter := messagingredis.New(rdb)
	defer func() { _ = messageLimiter.Close() }()
	// Messaging asks social who is connected or blocked, and social tells messaging about
	// new connections and blocks, so the checker gets social once that exists.
	connected := &connectionChecker{}
	messaging := messagingusecase.New(messagingpg.NewStore(pool), postgres.NewTransactor(pool), connected, connected, messageLimiter,
		messagingusecase.NopPublisher{}, messagingusecase.Limits{
			MessagesPerMinute: cfg.MessagesPerMinute, MessagesPerDay: cfg.MessagesPerDay,
			RequestsPerMinute: cfg.RequestsPerMinute, RequestsPerDay: cfg.RequestsPerDay,
		}, logger)
	// A connection change and its notification are written in one transaction (FR-9.1).
	social := socialusecase.New(socialpg.NewStore(pool), postgres.NewTransactor(pool), connectLimiter,
		connectionNotifier{notifications: notifications, messaging: messaging}, logger)
	connected.social = social

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
	)

	tokens := accesstoken.New(cfg.JWTSecret, accessTTL)
	auth := authusecase.New(
		authpg.NewStore(pool),
		argon2.New(argon2.DefaultParams),
		smtp.New(cfg.SMTPHost, cfg.SMTPPort, cfg.MailFrom),
		tokens,
		attempts,
		authusecase.Config{AppBaseURL: cfg.AppBaseURL},
		time.Now,
		logger,
	)
	handler := newRouter(logger, healthpg.NewPinger(pool), healthredis.NewPinger(rdb), files,
		authhttp.NewHandler(auth, authhttp.CookieConfig{Secure: cfg.CookieSecure}),
		profilehttp.NewHandler(profiles),
		mediahttp.NewHandler(images),
		socialhttp.NewHandler(social),
		notificationhttp.NewHandler(notifications),
		messaginghttp.NewHandler(messaging),
		httpx.RequireAuth(tokens, time.Now))

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", ":"+strconv.Itoa(cfg.Port))
	if err != nil {
		return fmt.Errorf("listen on port %d: %w", cfg.Port, err)
	}
	return httpserver.Serve(ctx, ln, handler, logger)
}
