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
	mediapg "github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/postgres"
	mediausecase "github.com/ARAFATH-SH/researchsphere/backend/internal/media/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/config"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpserver"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/logging"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/redis"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/storage"
	profilehttp "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/http"
	profilepg "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/postgres"
	profileusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
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
	// Deletes the objects of stored_files rows that are gone; stops with ctx.
	go mediausecase.NewCleaner(mediapg.NewStore(pool), files, logger).Run(ctx, cleanupInterval)

	attempts := authredis.New(rdb)
	defer func() { _ = attempts.Close() }()

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
		profilehttp.NewHandler(profileusecase.New(profilepg.NewStore(pool))),
		httpx.RequireAuth(tokens, time.Now))

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", ":"+strconv.Itoa(cfg.Port))
	if err != nil {
		return fmt.Errorf("listen on port %d: %w", cfg.Port, err)
	}
	return httpserver.Serve(ctx, ln, handler, logger)
}
