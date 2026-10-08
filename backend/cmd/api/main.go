// Command api runs the ResearchSphere API server. Settings come from environment
// variables; see .env.example in the repository root.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/config"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/health"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/server"
)

// shutdownTimeout is how long requests in flight get to finish after Ctrl+C or SIGTERM.
const shutdownTimeout = 10 * time.Second

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

	logger := slog.New(httpx.NewLogHandler(slog.NewJSONHandler(logOut, &slog.HandlerOptions{Level: cfg.LogLevel})))
	logger.Info("starting api", slog.Any("config", cfg))

	// Neither client connects until first used, so the server starts while the database
	// or Redis is down; the health check reports it. Parse errors are not wrapped
	// because they may quote the URL.
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return errors.New("DATABASE_URL is not accepted by the PostgreSQL driver")
	}
	defer pool.Close()

	redis.SetLogger(redisLogger{logger})
	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return errors.New("REDIS_URL is not accepted by the Redis client")
	}
	rdb := redis.NewClient(redisOpts)
	defer func() { _ = rdb.Close() }()

	redisPing := health.PingerFunc(func(ctx context.Context) error { return rdb.Ping(ctx).Err() })
	handler := server.New(logger, server.Handlers{
		Handler: health.NewHandler(pool, redisPing, health.DefaultTimeout, logger),
	})

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", ":"+strconv.Itoa(cfg.Port))
	if err != nil {
		return fmt.Errorf("listen on port %d: %w", cfg.Port, err)
	}
	return serve(ctx, ln, handler, logger)
}

// redisLogger sends go-redis's own messages through slog, so every log line is JSON.
type redisLogger struct{ logger *slog.Logger }

func (l redisLogger) Printf(ctx context.Context, format string, v ...any) {
	l.logger.WarnContext(ctx, "redis client", slog.String("detail", fmt.Sprintf(format, v...)))
}

// serve answers requests on ln until ctx is cancelled, then waits up to shutdownTimeout
// for requests in flight.
func serve(ctx context.Context, ln net.Listener, handler http.Handler, logger *slog.Logger) error {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", slog.String("addr", ln.Addr().String()))
		errCh <- srv.Serve(ln)
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("server stopped: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server stopped: %w", err)
	}
	logger.Info("shutdown complete")
	return nil
}
