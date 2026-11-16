// Package config reads the API server's settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/storage"
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
	DefaultS3Region   = "us-east-1"
	// OpenAlex (FR-5.2); tests point it at a fake server.
	DefaultOpenAlexBaseURL = "https://api.openalex.org"
	// ORCID public API (FR-5.3); tests point it at a fake server.
	DefaultORCIDBaseURL = "https://pub.orcid.org"
	// Send limits per user (FR-16.10).
	DefaultMessagesPerMinute = 30
	DefaultMessagesPerDay    = 1000
	DefaultRequestsPerMinute = 5
	DefaultRequestsPerDay    = 20
	// Message searches per user per minute (FR-16.8).
	DefaultSearchesPerMinute = 30
	// How long after sending a message its sender may edit it (FR-16.5).
	DefaultMessageEditWindow = 15 * time.Minute
	// clamd, the malware scanner of attachments (FR-16.7), as docker-compose.yml publishes it.
	DefaultClamAVAddr = "127.0.0.1:3310"
	// Days after sign-up an account that never verified its email address is deleted (FR-17.4).
	DefaultUnverifiedAccountDays = 7
	// The AI service (AI-1), as docker-compose.yml publishes it on this machine.
	DefaultAIServiceURL = "http://127.0.0.1:8090"

	// The recommendation score map (AI-2.2): 100 / (1 + exp(-(slope*cos + intercept))),
	// fitted with make calibrate-recs on 2026-11-15 for this model only: the sample tuning
	// split (seed 16), 2,000 tuning queries (seed 17), 200,000 pairs (each query's 50 nearest
	// and 50 random tuning profiles). A score of 50 is at cosine 0.893. docs/evaluation.md.
	DefaultRecsScoreModel     = "intfloat/multilingual-e5-base@d128750597153bb5987e10b1c3493a34e5a4502a"
	DefaultRecsScoreSlope     = 78.7208
	DefaultRecsScoreIntercept = -70.2827

	// The project score map (AI-3.1, AI-2.3) for profile-project pairs, in both directions,
	// for the same model: the people map above put them 21 points off on average, so make
	// calibrate-recs fitted this one on 2026-11-16 on the sample tuning split only (277,800
	// pairs: 2,000 tuning profiles with their 50 nearest and 50 random open tuning projects,
	// and the 778 tuning projects with their 50 nearest and 50 random tuning profiles). A
	// score of 50 is at cosine 0.850. docs/evaluation.md.
	DefaultRecsProjectScoreSlope     = 87.6849
	DefaultRecsProjectScoreIntercept = -74.5228

	// DefaultSearchAITimeout is how long a hybrid search waits for its query embedding
	// before it searches by keyword instead (AI-5.4). MaxSearchAITimeout keeps it short.
	DefaultSearchAITimeout = 1500 * time.Millisecond
	MaxSearchAITimeout     = 10 * time.Second
)

// MinAIServiceTokenLen is the shortest AI_SERVICE_TOKEN accepted; the AI service refuses a
// shorter one too.
const MinAIServiceTokenLen = 32

// Environments (APP_ENV). Production turns on the checks that keep development settings
// out of it (FR-17.1) and turns off the test email domains (FR-17.9).
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// SMTP TLS modes (SMTP_TLS, FR-17.1): STARTTLS required, TLS from the first byte (port
// 465), or plain text (Mailpit; refused in production).
const (
	SMTPTLSStartTLS = "starttls"
	SMTPTLSImplicit = "tls"
	SMTPTLSNone     = "none"
)

// MinJWTSecretLen is the shortest JWT_SECRET accepted, in bytes: HS256 needs a key at
// least as long as its 256-bit output.
const MinJWTSecretLen = 32

// Config holds the API server's settings. DatabaseURL and RedisURL may contain
// passwords, and JWTSecret and Storage.SecretKey are keys: never log them. LogValue logs only the safe fields.
type Config struct {
	DatabaseURL string
	RedisURL    string
	Port        int
	LogLevel    slog.Level

	// AppEnv is development or production (APP_ENV, default development).
	AppEnv string

	// Email (FR-1.3, FR-17.1). AppBaseURL is where the web client runs; links in emails
	// point there. SMTPPassword is a secret: never log it. SMTPTLS is one of the SMTPTLS
	// constants.
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPTLS      string
	MailFrom     mail.Address
	AppBaseURL   string

	// TestEmailDomains skip the sign-up domain checks and the purge of unverified accounts
	// outside production (FR-17.9); always empty in production.
	// TestEmailDomainsIgnored is true when TEST_EMAIL_DOMAINS was set in production, so main
	// can log a warning.
	TestEmailDomains        []string
	TestEmailDomainsIgnored bool
	// UnverifiedAccountDays is how many days after sign-up an unverified account is
	// deleted (FR-17.4).
	UnverifiedAccountDays int

	// Login (FR-2.1). JWTSecret signs access tokens. CookieSecure is true when AppBaseURL
	// uses https, so the session cookies are sent only over HTTPS outside local work.
	JWTSecret    []byte
	CookieSecure bool

	// UnsubscribeSecret signs the unread message digest's unsubscribe links (FR-16.11). The
	// links never expire, so changing it breaks every link already sent. A key: never log it.
	UnsubscribeSecret []byte

	// Object storage (FR-4.6, NFR-13): an S3-compatible bucket, MinIO locally.
	Storage storage.Config

	// Publication metadata by DOI (FR-5.2). OpenAlexMailto, when set, is sent with each
	// request so OpenAlex puts it in its "polite pool"; empty sends none.
	OpenAlexBaseURL string
	OpenAlexMailto  string
	// The ORCID public API, for importing works (FR-5.3).
	ORCIDBaseURL string
	// Send limits per user (FR-16.10): messages, and new message requests.
	MessagesPerMinute int
	MessagesPerDay    int
	RequestsPerMinute int
	RequestsPerDay    int
	// SearchesPerMinute limits message searches per user (FR-16.8).
	SearchesPerMinute int
	// MessageEditWindow is how long after sending a message its sender may edit it (FR-16.5).
	MessageEditWindow time.Duration
	// ClamAVAddr is clamd's TCP address (host:port) for the malware scan (FR-16.7).
	ClamAVAddr string

	// The internal AI service (AI-1). AIServiceToken is the shared secret it requires: never
	// log it. Empty (development only) leaves the AI service off.
	AIServiceURL   string
	AIServiceToken string

	// The recommendation score map (AI-2.2): the embedding model it was fitted for
	// ("name@revision") and its slope (> 0) and intercept. A model change needs a refit
	// (make calibrate-recs); the API logs a warning while they differ.
	RecsScoreModel     string
	RecsScoreSlope     float64
	RecsScoreIntercept float64
	// The project score map (AI-3.1, AI-2.3): profile-project pairs, fitted for
	// RecsScoreModel too; slope > 0.
	RecsProjectScoreSlope     float64
	RecsProjectScoreIntercept float64

	// SearchAITimeout bounds the query embedding of a hybrid researcher search; after it
	// the search falls back to keywords (AI-5.4).
	SearchAITimeout time.Duration
}

// Load reads the settings through getenv (os.Getenv in main). Errors name the
// variable but never include its value.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		DatabaseURL:       strings.TrimSpace(getenv("DATABASE_URL")),
		RedisURL:          strings.TrimSpace(getenv("REDIS_URL")),
		JWTSecret:         []byte(strings.TrimSpace(getenv("JWT_SECRET"))),
		UnsubscribeSecret: []byte(strings.TrimSpace(getenv("UNSUBSCRIBE_SECRET"))),
		Storage: storage.Config{
			Endpoint:  strings.TrimSpace(getenv("S3_ENDPOINT")),
			Region:    orDefault(getenv("S3_REGION"), DefaultS3Region),
			Bucket:    strings.TrimSpace(getenv("S3_BUCKET")),
			AccessKey: strings.TrimSpace(getenv("S3_ACCESS_KEY")),
			SecretKey: strings.TrimSpace(getenv("S3_SECRET_KEY")),
		},
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
	if len(cfg.UnsubscribeSecret) == 0 {
		missing = append(missing, "UNSUBSCRIBE_SECRET")
	}
	for _, v := range []struct{ name, value string }{
		{"S3_ENDPOINT", cfg.Storage.Endpoint},
		{"S3_BUCKET", cfg.Storage.Bucket},
		{"S3_ACCESS_KEY", cfg.Storage.AccessKey},
		{"S3_SECRET_KEY", cfg.Storage.SecretKey},
	} {
		if v.value == "" {
			missing = append(missing, v.name)
		}
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
	if len(cfg.UnsubscribeSecret) < MinJWTSecretLen {
		errs = append(errs, fmt.Errorf("UNSUBSCRIBE_SECRET must be at least %d bytes; generate one with: openssl rand -base64 48", MinJWTSecretLen))
	} else if string(cfg.UnsubscribeSecret) == string(cfg.JWTSecret) {
		errs = append(errs, errors.New("UNSUBSCRIBE_SECRET must differ from JWT_SECRET"))
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

	cfg.AppEnv = strings.ToLower(orDefault(getenv("APP_ENV"), EnvDevelopment))
	if cfg.AppEnv != EnvDevelopment && cfg.AppEnv != EnvProduction {
		errs = append(errs, fmt.Errorf("APP_ENV must be %s or %s, got %q", EnvDevelopment, EnvProduction, cfg.AppEnv))
	}
	production := cfg.AppEnv == EnvProduction

	cfg.SMTPHost = orDefault(getenv("SMTP_HOST"), DefaultSMTPHost)
	if cfg.SMTPPort, err = parsePort("SMTP_PORT", getenv("SMTP_PORT"), DefaultSMTPPort); err != nil {
		errs = append(errs, err)
	}
	cfg.SMTPUsername = strings.TrimSpace(getenv("SMTP_USERNAME"))
	cfg.SMTPPassword = getenv("SMTP_PASSWORD") // a password may hold spaces
	cfg.SMTPTLS = strings.ToLower(orDefault(getenv("SMTP_TLS"), SMTPTLSNone))
	if !slices.Contains([]string{SMTPTLSStartTLS, SMTPTLSImplicit, SMTPTLSNone}, cfg.SMTPTLS) {
		errs = append(errs, fmt.Errorf("SMTP_TLS must be starttls, tls or none, got %q", cfg.SMTPTLS))
	}
	if (cfg.SMTPUsername == "") != (cfg.SMTPPassword == "") {
		errs = append(errs, errors.New("SMTP_USERNAME and SMTP_PASSWORD must be set together"))
	}
	if production {
		// FR-17.1: production mail goes to a real provider, encrypted and logged in.
		if cfg.SMTPTLS == SMTPTLSNone {
			errs = append(errs, errors.New("SMTP_TLS must be starttls or tls when APP_ENV=production"))
		}
		if isLocalHost(cfg.SMTPHost) {
			errs = append(errs, errors.New("SMTP_HOST must be the mail provider's server, not this machine, when APP_ENV=production"))
		}
		if cfg.SMTPUsername == "" || cfg.SMTPPassword == "" {
			errs = append(errs, errors.New("SMTP_USERNAME and SMTP_PASSWORD are required when APP_ENV=production"))
		}
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

	if err := checkHostPort("S3_ENDPOINT", cfg.Storage.Endpoint); err != nil {
		errs = append(errs, err)
	}
	if cfg.Storage.UseSSL, err = parseBool("S3_USE_SSL", getenv("S3_USE_SSL")); err != nil {
		errs = append(errs, err)
	}

	cfg.OpenAlexBaseURL = strings.TrimRight(orDefault(getenv("OPENALEX_BASE_URL"), DefaultOpenAlexBaseURL), "/")
	if err := checkBaseURL("OPENALEX_BASE_URL", cfg.OpenAlexBaseURL); err != nil {
		errs = append(errs, err)
	}
	cfg.OpenAlexMailto = strings.TrimSpace(getenv("OPENALEX_MAILTO"))
	if a, err := mail.ParseAddress(cfg.OpenAlexMailto); cfg.OpenAlexMailto != "" && (err != nil || a.Name != "" || a.Address != cfg.OpenAlexMailto) {
		errs = append(errs, errors.New("OPENALEX_MAILTO must be a bare email address, such as dev@example.org"))
	}
	cfg.ORCIDBaseURL = strings.TrimRight(orDefault(getenv("ORCID_BASE_URL"), DefaultORCIDBaseURL), "/")
	if err := checkBaseURL("ORCID_BASE_URL", cfg.ORCIDBaseURL); err != nil {
		errs = append(errs, err)
	}

	for _, l := range []struct {
		name string
		def  int
		dst  *int
	}{
		{"MESSAGE_LIMIT_PER_MINUTE", DefaultMessagesPerMinute, &cfg.MessagesPerMinute},
		{"MESSAGE_LIMIT_PER_DAY", DefaultMessagesPerDay, &cfg.MessagesPerDay},
		{"MESSAGE_REQUEST_LIMIT_PER_MINUTE", DefaultRequestsPerMinute, &cfg.RequestsPerMinute},
		{"MESSAGE_REQUEST_LIMIT_PER_DAY", DefaultRequestsPerDay, &cfg.RequestsPerDay},
		{"MESSAGE_SEARCH_LIMIT_PER_MINUTE", DefaultSearchesPerMinute, &cfg.SearchesPerMinute},
	} {
		if *l.dst, err = parsePositive(l.name, getenv(l.name), l.def); err != nil {
			errs = append(errs, err)
		}
	}

	if cfg.MessageEditWindow, err = parseDuration("MESSAGE_EDIT_WINDOW", getenv("MESSAGE_EDIT_WINDOW"), DefaultMessageEditWindow); err != nil {
		errs = append(errs, err)
	}

	cfg.ClamAVAddr = orDefault(getenv("CLAMAV_ADDR"), DefaultClamAVAddr)
	if err := checkHostPort("CLAMAV_ADDR", cfg.ClamAVAddr); err != nil {
		errs = append(errs, err)
	}

	cfg.AIServiceURL = strings.TrimRight(orDefault(getenv("AI_SERVICE_URL"), DefaultAIServiceURL), "/")
	if err := checkBaseURL("AI_SERVICE_URL", cfg.AIServiceURL); err != nil {
		errs = append(errs, err)
	}
	cfg.AIServiceToken = strings.TrimSpace(getenv("AI_SERVICE_TOKEN"))
	switch {
	case cfg.AIServiceToken == "" && production:
		errs = append(errs, errors.New("AI_SERVICE_TOKEN is required when APP_ENV=production"))
	case cfg.AIServiceToken != "" && len(cfg.AIServiceToken) < MinAIServiceTokenLen:
		errs = append(errs, fmt.Errorf("AI_SERVICE_TOKEN must be at least %d characters; generate one with: openssl rand -base64 48", MinAIServiceTokenLen))
	case cfg.AIServiceToken != "" && (cfg.AIServiceToken == string(cfg.JWTSecret) || cfg.AIServiceToken == string(cfg.UnsubscribeSecret)):
		errs = append(errs, errors.New("AI_SERVICE_TOKEN must differ from JWT_SECRET and UNSUBSCRIBE_SECRET"))
	}

	cfg.RecsScoreModel = strings.TrimSpace(orDefault(getenv("RECS_SCORE_MODEL"), DefaultRecsScoreModel))
	if !strings.Contains(cfg.RecsScoreModel, "@") || strings.ContainsAny(cfg.RecsScoreModel, " \t") {
		errs = append(errs, errors.New("RECS_SCORE_MODEL must be the embedding model as name@revision"))
	}
	if cfg.RecsScoreSlope, err = parseFloat("RECS_SCORE_SLOPE", getenv("RECS_SCORE_SLOPE"), DefaultRecsScoreSlope); err != nil {
		errs = append(errs, err)
	} else if cfg.RecsScoreSlope <= 0 {
		errs = append(errs, errors.New("RECS_SCORE_SLOPE must be more than 0, so a more similar profile never scores lower"))
	}
	if cfg.RecsScoreIntercept, err = parseFloat("RECS_SCORE_INTERCEPT", getenv("RECS_SCORE_INTERCEPT"), DefaultRecsScoreIntercept); err != nil {
		errs = append(errs, err)
	}
	if cfg.RecsProjectScoreSlope, err = parseFloat("RECS_PROJECT_SCORE_SLOPE", getenv("RECS_PROJECT_SCORE_SLOPE"), DefaultRecsProjectScoreSlope); err != nil {
		errs = append(errs, err)
	} else if cfg.RecsProjectScoreSlope <= 0 {
		errs = append(errs, errors.New("RECS_PROJECT_SCORE_SLOPE must be more than 0, so a more similar project never scores lower"))
	}
	if cfg.RecsProjectScoreIntercept, err = parseFloat("RECS_PROJECT_SCORE_INTERCEPT", getenv("RECS_PROJECT_SCORE_INTERCEPT"), DefaultRecsProjectScoreIntercept); err != nil {
		errs = append(errs, err)
	}
	if cfg.SearchAITimeout, err = parseDuration("SEARCH_AI_TIMEOUT", getenv("SEARCH_AI_TIMEOUT"), DefaultSearchAITimeout); err != nil {
		errs = append(errs, err)
	} else if cfg.SearchAITimeout > MaxSearchAITimeout {
		errs = append(errs, fmt.Errorf("SEARCH_AI_TIMEOUT must be at most %s, so a search falls back to keywords quickly", MaxSearchAITimeout))
	}

	domains, err := parseDomains("TEST_EMAIL_DOMAINS", getenv("TEST_EMAIL_DOMAINS"))
	if err != nil {
		errs = append(errs, err)
	}
	if production {
		cfg.TestEmailDomainsIgnored = len(domains) > 0
	} else {
		cfg.TestEmailDomains = domains
	}
	if cfg.UnverifiedAccountDays, err = parsePositive("UNVERIFIED_ACCOUNT_DAYS", getenv("UNVERIFIED_ACCOUNT_DAYS"), DefaultUnverifiedAccountDays); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

// LogValue keeps the database and Redis URLs, and so any password in them, the JWT and
// unsubscribe secrets, the AI service token, the SMTP password and the storage keys out of the logs. The mail, cookie, storage location,
// OpenAlex and ORCID address settings hold no secrets; the OpenAlex contact address is
// left out as personal data.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("app_env", c.AppEnv),
		slog.Int("port", c.Port),
		slog.String("log_level", c.LogLevel.String()),
		slog.String("smtp_host", c.SMTPHost),
		slog.Int("smtp_port", c.SMTPPort),
		slog.String("smtp_tls", c.SMTPTLS),
		slog.Bool("smtp_auth", c.SMTPUsername != ""),
		slog.String("mail_from", c.MailFrom.String()),
		slog.String("app_base_url", c.AppBaseURL),
		slog.Bool("cookie_secure", c.CookieSecure),
		slog.String("s3_endpoint", c.Storage.Endpoint),
		slog.String("s3_region", c.Storage.Region),
		slog.String("s3_bucket", c.Storage.Bucket),
		slog.Bool("s3_use_ssl", c.Storage.UseSSL),
		slog.String("openalex_base_url", c.OpenAlexBaseURL),
		slog.String("orcid_base_url", c.ORCIDBaseURL),
		slog.String("clamav_addr", c.ClamAVAddr),
		slog.String("ai_service_url", c.AIServiceURL),
		slog.String("recs_score_model", c.RecsScoreModel),
		slog.Bool("ai_service_configured", c.AIServiceToken != ""),
		slog.Any("test_email_domains", c.TestEmailDomains),
		slog.Int("unverified_account_days", c.UnverifiedAccountDays),
	)
}

// isLocalHost reports whether host names this machine.
func isLocalHost(host string) bool {
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

// domainName is a lower-case domain of at least two labels, such as example.com.
var domainName = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// parseDomains reads a comma-separated list of domain names, lower-cased, without repeats;
// empty means none.
func parseDomains(name, raw string) ([]string, error) {
	var out []string
	for d := range strings.SplitSeq(raw, ",") {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if len(d) > 253 || !domainName.MatchString(d) {
			return nil, fmt.Errorf("%s must be domain names separated by commas, such as example.com,compare.researchsphere.local, got %q", name, d)
		}
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out, nil
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

func parsePositive(name, raw string, def int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a whole number of at least 1, got %q", name, raw)
	}
	return n, nil
}

// parseFloat reads a finite number; empty means def.
func parseFloat(name, raw string, def float64) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%s must be a finite number, got %q", name, raw)
	}
	return f, nil
}

// parseDuration reads a Go duration such as 15m that is more than zero; empty means def.
func parseDuration(name, raw string, def time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a duration of more than zero, such as 15m, got %q", name, raw)
	}
	return d, nil
}

// checkBaseURL accepts an absolute http or https URL with a host and no query or fragment.
func checkBaseURL(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%s must be an http:// or https:// address such as http://localhost:3000, got %q", name, raw)
	}
	return nil
}

// checkHostPort accepts host:port with no scheme or path, such as localhost:9000. The
// value is not repeated: an endpoint holds no secret, but the message stays the same shape
// as the URL checks.
func checkHostPort(name, raw string) error {
	host, port, err := net.SplitHostPort(raw)
	if err != nil || host == "" || strings.ContainsAny(raw, "/@?#") {
		return fmt.Errorf("%s must be host:port without a scheme, such as localhost:9000", name)
	}
	if _, err := parsePort(name, port, 0); err != nil {
		return err
	}
	return nil
}

// parseBool reads true or false; empty means false.
func parseBool(name, raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	}
	return false, fmt.Errorf("%s must be true or false, got %q", name, raw)
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
