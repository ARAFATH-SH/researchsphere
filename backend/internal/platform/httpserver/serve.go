package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// ShutdownTimeout is how long requests in flight get to finish after Ctrl+C or SIGTERM.
const ShutdownTimeout = 10 * time.Second

// Serve answers requests on ln until ctx is cancelled, then waits up to ShutdownTimeout
// for requests in flight. http.Server.Shutdown does not know about upgraded connections, so
// onShutdown, when given, closes them first, within the same time.
func Serve(ctx context.Context, ln net.Listener, handler http.Handler, logger *slog.Logger, onShutdown ...func(context.Context)) error {
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
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ShutdownTimeout)
	defer cancel()
	for _, f := range onShutdown {
		f(shutdownCtx)
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server stopped: %w", err)
	}
	logger.Info("shutdown complete")
	return nil
}
