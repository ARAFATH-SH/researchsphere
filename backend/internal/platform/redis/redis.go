// Package redis opens the shared Redis client.
package redis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	goredis "github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"
)

// NewClient returns a client for redisURL and sends go-redis's own messages to logger.
// It does not connect until first used. The parse error is not wrapped because it may
// quote the URL, which can contain a password.
func NewClient(redisURL string, logger *slog.Logger) (*goredis.Client, error) {
	goredis.SetLogger(clientLogger{logger})
	opts, err := goredis.ParseURL(redisURL)
	if err != nil {
		return nil, errors.New("REDIS_URL is not accepted by the Redis client")
	}
	ApplyDefaults(opts)
	return goredis.NewClient(opts), nil
}

// ApplyDefaults sets the options every Redis client of this application shares. Call it on
// the options of each new client, including one copied from another client's Options().
//
// It turns off maintenance notifications: self-hosted Redis does not send them, and the
// attempt to register their handlers logs an error at startup for each client.
func ApplyDefaults(opts *goredis.Options) {
	opts.MaintNotificationsConfig = &maintnotifications.Config{Mode: maintnotifications.ModeDisabled}
}

// clientLogger sends go-redis's own messages through slog, so every log line is JSON.
type clientLogger struct{ logger *slog.Logger }

func (l clientLogger) Printf(ctx context.Context, format string, v ...any) {
	l.logger.WarnContext(ctx, "redis client", slog.String("detail", fmt.Sprintf(format, v...)))
}
