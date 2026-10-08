// Package config reads the API server's settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
)

// Defaults for optional settings.
const (
	DefaultPort     = 8080
	DefaultLogLevel = "info"
)

// Config holds the API server's settings. DatabaseURL and RedisURL may contain
// passwords: never log them. LogValue logs only the safe fields.
type Config struct {
	DatabaseURL string
	RedisURL    string
	Port        int
	LogLevel    slog.Level
}

// Load reads the settings through getenv (os.Getenv in main). Errors name the
// variable but never include its value.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		DatabaseURL: strings.TrimSpace(getenv("DATABASE_URL")),
		RedisURL:    strings.TrimSpace(getenv("REDIS_URL")),
	}

	var missing []string
	if cfg.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if cfg.RedisURL == "" {
		missing = append(missing, "REDIS_URL")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variable(s): %s", strings.Join(missing, ", "))
	}

	var errs []error
	if err := checkURL("DATABASE_URL", cfg.DatabaseURL, "postgres", "postgresql"); err != nil {
		errs = append(errs, err)
	}
	if err := checkURL("REDIS_URL", cfg.RedisURL, "redis", "rediss"); err != nil {
		errs = append(errs, err)
	}

	port, err := parsePort(getenv("API_PORT"))
	if err != nil {
		errs = append(errs, err)
	}
	cfg.Port = port

	level, err := parseLevel(getenv("LOG_LEVEL"))
	if err != nil {
		errs = append(errs, err)
	}
	cfg.LogLevel = level

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

// LogValue keeps the URLs, and so any password in them, out of the logs.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("port", c.Port),
		slog.String("log_level", c.LogLevel.String()),
	)
}

// checkURL reports a malformed URL without echoing it: url.Parse errors quote
// the input, so they are not wrapped.
func checkURL(name, raw string, schemes ...string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL", name)
	}
	for _, s := range schemes {
		if u.Scheme == s {
			return nil
		}
	}
	return fmt.Errorf("%s must start with %s://", name, strings.Join(schemes, ":// or "))
}

func parsePort(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultPort, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("API_PORT must be a number from 1 to 65535, got %q", raw)
	}
	return port, nil
}

func parseLevel(raw string) (slog.Level, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		raw = DefaultLogLevel
	}
	switch raw {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error, got %q", raw)
}
