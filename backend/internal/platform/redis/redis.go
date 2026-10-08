// Package redis opens the shared Redis client.
package redis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	goredis "github.com/redis/go-redis/v9"
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
	return goredis.NewClient(opts), nil
}

// clientLogger sends go-redis's own messages through slog, so every log line is JSON.
type clientLogger struct{ logger *slog.Logger }

func (l clientLogger) Printf(ctx context.Context, format string, v ...any) {
	l.logger.WarnContext(ctx, "redis client", slog.String("detail", fmt.Sprintf(format, v...)))
}
