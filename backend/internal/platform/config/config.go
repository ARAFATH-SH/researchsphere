// Package config reads the API server's settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
)

// Defaults for optional settings.
// The mail defaults point at Mailpit from docker-compose.yml and the web client's dev
// server, so local development needs no settings.
const (
	DefaultPort       = 8080
	DefaultLogLevel   = "info"
	DefaultSMTPHost   = "localhost"
	DefaultSMTPPort   = 1025
	DefaultMailFrom   = "ResearchSphere <no-reply@researchsphere.local>"
	DefaultAppBaseURL = "http://localhost:3000"
)

// MinJWTSecretLen is the shortest JWT_SECRET accepted, in bytes: HS256 needs a key at
// least as long as its 256-bit output.
const MinJWTSecretLen = 32

// Config holds the API server's settings. DatabaseURL and RedisURL may contain
// passwords, and JWTSecret is a key: never log them. LogValue logs only the safe fields.
type Config struct {
	DatabaseURL string
	RedisURL    string
	Port        int
	LogLevel    slog.Level

	// Email (FR-1.3). AppBaseURL is where the web client runs; links in emails point there.
	SMTPHost   string
	SMTPPort   int
	MailFrom   mail.Address
	AppBaseURL string

	// Login (FR-2.1). JWTSecret signs access tokens. CookieSecure is true when AppBaseURL
	// uses https, so the session cookies are sent only over HTTPS outside local work.
	JWTSecret    []byte
	CookieSecure bool
}

// Load reads the settings through getenv (os.Getenv in main). Errors name the
// variable but never include its value.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		DatabaseURL: strings.TrimSpace(getenv("DATABASE_URL")),
		RedisURL:    strings.TrimSpace(getenv("REDIS_URL")),
		JWTSecret:   []byte(strings.TrimSpace(getenv("JWT_SECRET"))),
	}

	var missing []string
	if cfg.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if cfg.RedisURL == "" {
		missing = append(missing, "REDIS_URL")
	}
	if len(cfg.JWTSecret) == 0 {
		missing = append(missing, "JWT_SECRET")
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
	if len(cfg.JWTSecret) < MinJWTSecretLen {
		errs = append(errs, fmt.Errorf("JWT_SECRET must be at least %d bytes; generate one with: openssl rand -base64 48", MinJWTSecretLen))
	}

	port, err := parsePort("API_PORT", getenv("API_PORT"), DefaultPort)
	if err != nil {
		errs = append(errs, err)
	}
	cfg.Port = port

	level, err := parseLevel(getenv("LOG_LEVEL"))
	if err != nil {
		errs = append(errs, err)
	}
	cfg.LogLevel = level

	cfg.SMTPHost = orDefault(getenv("SMTP_HOST"), DefaultSMTPHost)
	if cfg.SMTPPort, err = parsePort("SMTP_PORT", getenv("SMTP_PORT"), DefaultSMTPPort); err != nil {
		errs = append(errs, err)
	}
	from, err := mail.ParseAddress(orDefault(getenv("MAIL_FROM"), DefaultMailFrom))
	if err != nil {
		errs = append(errs, errors.New("MAIL_FROM must be an email address, such as ResearchSphere <no-reply@example.org>"))
	} else {
		cfg.MailFrom = *from
	}
	cfg.AppBaseURL = strings.TrimRight(orDefault(getenv("APP_BASE_URL"), DefaultAppBaseURL), "/")
	if err := checkBaseURL("APP_BASE_URL", cfg.AppBaseURL); err != nil {
		errs = append(errs, err)
	}
	cfg.CookieSecure = strings.HasPrefix(cfg.AppBaseURL, "https://")

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

// LogValue keeps the database and Redis URLs, and so any password in them, and the JWT
// secret out of the logs. The mail and cookie settings hold no secrets.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("port", c.Port),
		slog.String("log_level", c.LogLevel.String()),
		slog.String("smtp_host", c.SMTPHost),
		slog.Int("smtp_port", c.SMTPPort),
		slog.String("mail_from", c.MailFrom.String()),
		slog.String("app_base_url", c.AppBaseURL),
		slog.Bool("cookie_secure", c.CookieSecure),
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

func parsePort(name, raw string, def int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("%s must be a number from 1 to 65535, got %q", name, raw)
	}
	return port, nil
}

// checkBaseURL accepts an absolute http or https URL with a host and no query or fragment.
func checkBaseURL(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%s must be an http:// or https:// address such as http://localhost:3000, got %q", name, raw)
	}
	return nil
}

func orDefault(raw, def string) string {
	if raw = strings.TrimSpace(raw); raw != "" {
		return raw
	}
	return def
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
