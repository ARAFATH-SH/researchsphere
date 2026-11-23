package config

import (
	"bytes"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	testPassword = "s3cret-pw"
	testDBURL    = "postgres://user:" + testPassword + "@localhost:5433/db?sslmode=disable"
	testRedisURL = "redis://:" + testPassword + "@localhost:6379/0"
	testSecret   = "test-jwt-secret-0123456789abcdefghij"
	testUnsubKey = "test-unsubscribe-secret-0123456789ab"  //nolint:gosec // test value, not a credential
	testS3Secret = "test-s3-secret-key"                    //nolint:gosec // test value, not a credential
	testSMTPPass = "smtp-secret-do-not-log"                //nolint:gosec // test value, not a credential
	testAIToken  = "test-ai-service-token-0123456789abcd"  //nolint:gosec // test value, not a credential
	testSyncTok  = "test-sync-service-token-0123456789ab"  //nolint:gosec // test value, not a credential
	testCompTok  = "test-compile-service-token-0123456789" //nolint:gosec // test value, not a credential
)

// validDefaults are served for these settings unless vars sets them (even to "").
var validDefaults = map[string]string{
	"JWT_SECRET":         testSecret,
	"UNSUBSCRIBE_SECRET": testUnsubKey,
	"S3_ENDPOINT":        "localhost:9000",
	"S3_BUCKET":          "researchsphere",
	"S3_ACCESS_KEY":      "researchsphere",
	"S3_SECRET_KEY":      testS3Secret,
}

// env serves vars, with valid JWT and storage settings unless vars sets them.
func env(vars map[string]string) func(string) string {
	return func(k string) string {
		if v, ok := vars[k]; ok {
			return v
		}
		return validDefaults[k]
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name      string
		vars      map[string]string
		wantPort  int
		wantLevel slog.Level
		wantErr   []string // substrings the error must contain; nil means no error
	}{
		{
			name:      "all set",
			vars:      map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "API_PORT": "9090", "LOG_LEVEL": "debug"},
			wantPort:  9090,
			wantLevel: slog.LevelDebug,
		},
		{
			name:      "defaults for port and level",
			vars:      map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL},
			wantPort:  8080,
			wantLevel: slog.LevelInfo,
		},
		{
			name:      "level is case-insensitive",
			vars:      map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "LOG_LEVEL": "WARN"},
			wantPort:  8080,
			wantLevel: slog.LevelWarn,
		},
		{
			name:    "database url missing",
			vars:    map[string]string{"REDIS_URL": testRedisURL},
			wantErr: []string{"DATABASE_URL"},
		},
		{
			name:    "redis url missing",
			vars:    map[string]string{"DATABASE_URL": testDBURL},
			wantErr: []string{"REDIS_URL"},
		},
		{
			name:    "both missing",
			vars:    map[string]string{},
			wantErr: []string{"DATABASE_URL", "REDIS_URL"},
		},
		{
			name:    "blank counts as missing",
			vars:    map[string]string{"DATABASE_URL": "  ", "REDIS_URL": testRedisURL},
			wantErr: []string{"DATABASE_URL"},
		},
		{
			name:    "wrong database scheme",
			vars:    map[string]string{"DATABASE_URL": "mysql://user:" + testPassword + "@x/db", "REDIS_URL": testRedisURL},
			wantErr: []string{"DATABASE_URL must start with postgres://"},
		},
		{
			name:    "unparsable database url",
			vars:    map[string]string{"DATABASE_URL": "postgres://user:" + testPassword + "@[bad", "REDIS_URL": testRedisURL},
			wantErr: []string{"DATABASE_URL is not a valid URL"},
		},
		{
			name:    "port not a number",
			vars:    map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "API_PORT": "abc"},
			wantErr: []string{"API_PORT"},
		},
		{
			name:    "port zero",
			vars:    map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "API_PORT": "0"},
			wantErr: []string{"API_PORT"},
		},
		{
			name:    "port too large",
			vars:    map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "API_PORT": "70000"},
			wantErr: []string{"API_PORT"},
		},
		{
			name:    "unknown level",
			vars:    map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "LOG_LEVEL": "loud"},
			wantErr: []string{"LOG_LEVEL"},
		},
		{
			name:    "smtp port not a number",
			vars:    map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "SMTP_PORT": "mail"},
			wantErr: []string{"SMTP_PORT"},
		},
		{
			name:    "mail from not an address",
			vars:    map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "MAIL_FROM": "ResearchSphere"},
			wantErr: []string{"MAIL_FROM"},
		},
		{
			name:    "app base url without scheme",
			vars:    map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "APP_BASE_URL": "localhost:3000"},
			wantErr: []string{"APP_BASE_URL"},
		},
		{
			name:    "app base url with a fragment",
			vars:    map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "APP_BASE_URL": "https://rs.example/#x"},
			wantErr: []string{"APP_BASE_URL"},
		},
		{
			name:    "jwt secret missing",
			vars:    map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "JWT_SECRET": ""},
			wantErr: []string{"JWT_SECRET"},
		},
		{
			name:    "jwt secret too short",
			vars:    map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "JWT_SECRET": testPassword},
			wantErr: []string{"JWT_SECRET must be at least 32 bytes"},
		},
		{
			name:    "unsubscribe secret missing",
			vars:    map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "UNSUBSCRIBE_SECRET": ""},
			wantErr: []string{"UNSUBSCRIBE_SECRET"},
		},
		{
			name:    "unsubscribe secret too short",
			vars:    map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "UNSUBSCRIBE_SECRET": testPassword},
			wantErr: []string{"UNSUBSCRIBE_SECRET must be at least 32 bytes"},
		},
		{
			name:    "unsubscribe secret the same as the jwt secret",
			vars:    map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "UNSUBSCRIBE_SECRET": testSecret},
			wantErr: []string{"UNSUBSCRIBE_SECRET must differ from JWT_SECRET"},
		},
		{
			name:    "several errors reported together",
			vars:    map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "API_PORT": "x", "LOG_LEVEL": "y"},
			wantErr: []string{"API_PORT", "LOG_LEVEL"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(env(tt.vars))
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("Load() error = nil, want one mentioning %v", tt.wantErr)
				}
				for _, s := range tt.wantErr {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("error %q does not mention %q", err, s)
					}
				}
				if strings.Contains(err.Error(), testPassword) {
					t.Errorf("error %q leaks the password", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.Port != tt.wantPort {
				t.Errorf("Port = %d, want %d", cfg.Port, tt.wantPort)
			}
			if cfg.LogLevel != tt.wantLevel {
				t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, tt.wantLevel)
			}
			if cfg.DatabaseURL != testDBURL || cfg.RedisURL != testRedisURL {
				t.Errorf("URLs not kept: %q, %q", cfg.DatabaseURL, cfg.RedisURL)
			}
		})
	}
}

func TestLoadMailSettings(t *testing.T) {
	t.Run("defaults point at Mailpit and the dev web client", func(t *testing.T) {
		cfg, err := Load(env(map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.SMTPHost != "localhost" || cfg.SMTPPort != 1025 {
			t.Errorf("SMTP = %s:%d, want localhost:1025", cfg.SMTPHost, cfg.SMTPPort)
		}
		if cfg.MailFrom.Address != "no-reply@researchsphere.local" || cfg.MailFrom.Name != "ResearchSphere" {
			t.Errorf("MailFrom = %+v", cfg.MailFrom)
		}
		if cfg.AppBaseURL != "http://localhost:3000" {
			t.Errorf("AppBaseURL = %q", cfg.AppBaseURL)
		}
	})

	t.Run("set values are used and the base URL loses its trailing slash", func(t *testing.T) {
		cfg, err := Load(env(map[string]string{
			"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL,
			"SMTP_HOST": "smtp.example.org", "SMTP_PORT": "587",
			"MAIL_FROM": "no-reply@example.org", "APP_BASE_URL": "https://rs.example.org/",
		}))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.SMTPHost != "smtp.example.org" || cfg.SMTPPort != 587 || cfg.MailFrom.Address != "no-reply@example.org" {
			t.Errorf("cfg = %+v", cfg)
		}
		if cfg.AppBaseURL != "https://rs.example.org" {
			t.Errorf("AppBaseURL = %q", cfg.AppBaseURL)
		}
	})
}

func TestLoadLoginSettings(t *testing.T) {
	tests := []struct {
		baseURL    string
		wantSecure bool
	}{
		{"", false}, // the default, http://localhost:3000
		{"http://localhost:3000", false},
		{"https://rs.example.org", true},
	}
	for _, tt := range tests {
		t.Run(tt.baseURL, func(t *testing.T) {
			vars := map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}
			if tt.baseURL != "" {
				vars["APP_BASE_URL"] = tt.baseURL
			}
			cfg, err := Load(env(vars))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.CookieSecure != tt.wantSecure {
				t.Errorf("CookieSecure = %v, want %v", cfg.CookieSecure, tt.wantSecure)
			}
			if string(cfg.JWTSecret) != testSecret {
				t.Error("JWTSecret is not the value of JWT_SECRET")
			}
			if string(cfg.UnsubscribeSecret) != testUnsubKey {
				t.Error("UnsubscribeSecret is not the value of UNSUBSCRIBE_SECRET")
			}
		})
	}
}

func TestLogValueHidesSecrets(t *testing.T) {
	cfg, err := Load(env(map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL,
		"SMTP_USERNAME": "apikey", "SMTP_PASSWORD": testSMTPPass, "AI_SERVICE_TOKEN": testAIToken,
		"SYNC_SERVICE_TOKEN": testSyncTok, "COMPILE_SERVICE_TOKEN": testCompTok}))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config loaded", "config", cfg)

	out := buf.String()
	for _, secret := range []string{testPassword, testDBURL, testRedisURL, "postgres://", "redis://", testSecret, testUnsubKey, testS3Secret, testSMTPPass, testAIToken, testSyncTok} {
		if strings.Contains(out, secret) {
			t.Errorf("log line contains %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, `"ai_service_configured":true`) {
		t.Errorf("log line lacks whether the AI service is configured: %s", out)
	}
	if !strings.Contains(out, `"port":8080`) {
		t.Errorf("log line lacks the port: %s", out)
	}
}

func TestLoadStorageSettings(t *testing.T) {
	base := map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}
	with := func(extra map[string]string) map[string]string {
		vars := map[string]string{}
		for k, v := range base {
			vars[k] = v
		}
		for k, v := range extra {
			vars[k] = v
		}
		return vars
	}

	t.Run("defaults", func(t *testing.T) {
		cfg, err := Load(env(base))
		if err != nil {
			t.Fatal(err)
		}
		s := cfg.Storage
		if s.Endpoint != "127.0.0.1:9000" || s.Bucket != "researchsphere" || s.Region != DefaultS3Region ||
			s.AccessKey != "researchsphere" || s.SecretKey != testS3Secret || s.UseSSL {
			t.Errorf("Storage = %+v", s)
		}
	})
	// Presigned URLs go to the browser with this host: in development "localhost" and ::1 are
	// written 127.0.0.1, where docker-compose.yml publishes MinIO; production keeps its host.
	t.Run("loopback in development", func(t *testing.T) {
		for in, want := range map[string]string{
			"localhost:9000": "127.0.0.1:9000", "LOCALHOST:9100": "127.0.0.1:9100", "[::1]:9000": "127.0.0.1:9000",
			"127.0.0.1:9000": "127.0.0.1:9000", "minio.example.org:443": "minio.example.org:443",
		} {
			cfg, err := Load(env(with(map[string]string{"S3_ENDPOINT": in})))
			if err != nil || cfg.Storage.Endpoint != want {
				t.Errorf("%s: endpoint %q (%v), want %q", in, cfg.Storage.Endpoint, err, want)
			}
		}
		prod := maps.Clone(prodMail)
		prod["S3_ENDPOINT"] = "localhost:9000"
		if cfg, err := Load(env(prod)); err != nil || cfg.Storage.Endpoint != "localhost:9000" {
			t.Errorf("production: %q (%v)", cfg.Storage.Endpoint, err)
		}
	})
	t.Run("region and ssl", func(t *testing.T) {
		cfg, err := Load(env(with(map[string]string{"S3_REGION": "eu-central-1", "S3_USE_SSL": "TRUE"})))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Storage.Region != "eu-central-1" || !cfg.Storage.UseSSL {
			t.Errorf("Storage = %+v", cfg.Storage)
		}
	})

	refused := []struct {
		name    string
		vars    map[string]string
		wantErr []string
	}{
		{"all storage settings missing", map[string]string{"S3_ENDPOINT": "", "S3_BUCKET": "", "S3_ACCESS_KEY": "", "S3_SECRET_KEY": " "},
			[]string{"S3_ENDPOINT", "S3_BUCKET", "S3_ACCESS_KEY", "S3_SECRET_KEY"}},
		{"endpoint with a scheme", map[string]string{"S3_ENDPOINT": "http://localhost:9000"}, []string{"S3_ENDPOINT"}},
		{"endpoint without a port", map[string]string{"S3_ENDPOINT": "localhost"}, []string{"S3_ENDPOINT"}},
		{"endpoint with a path", map[string]string{"S3_ENDPOINT": "localhost:9000/bucket"}, []string{"S3_ENDPOINT"}},
		{"endpoint with a bad port", map[string]string{"S3_ENDPOINT": "localhost:99999"}, []string{"S3_ENDPOINT"}},
		{"ssl not a boolean", map[string]string{"S3_USE_SSL": "yes"}, []string{"S3_USE_SSL"}},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(env(with(tt.vars)))
			if err == nil {
				t.Fatalf("Load() error = nil, want one naming %v", tt.wantErr)
			}
			for _, name := range tt.wantErr {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("error %q does not name %s", err, name)
				}
			}
			if strings.Contains(err.Error(), testS3Secret) {
				t.Errorf("error %q leaks the storage secret", err)
			}
		})
	}
}

func TestLoadOpenAlexSettings(t *testing.T) {
	vars := func(extra map[string]string) map[string]string {
		v := map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}
		for k, x := range extra {
			v[k] = x
		}
		return v
	}
	tests := []struct {
		name       string
		extra      map[string]string
		wantBase   string
		wantMailto string
		wantErr    string
	}{
		{"defaults", nil, DefaultOpenAlexBaseURL, "", ""},
		{"set", map[string]string{"OPENALEX_BASE_URL": "http://127.0.0.1:4010/", "OPENALEX_MAILTO": " dev@example.org "},
			"http://127.0.0.1:4010", "dev@example.org", ""},
		{"base without a scheme", map[string]string{"OPENALEX_BASE_URL": "api.openalex.org"}, "", "", "OPENALEX_BASE_URL"},
		{"base with a query", map[string]string{"OPENALEX_BASE_URL": "https://api.openalex.org?x=1"}, "", "", "OPENALEX_BASE_URL"},
		{"mailto not an address", map[string]string{"OPENALEX_MAILTO": "nobody"}, "", "", "OPENALEX_MAILTO"},
		{"mailto with a name", map[string]string{"OPENALEX_MAILTO": "Dev <dev@example.org>"}, "", "", "OPENALEX_MAILTO"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(env(vars(tt.extra)))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load() error = %v, want one naming %s", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.OpenAlexBaseURL != tt.wantBase || cfg.OpenAlexMailto != tt.wantMailto {
				t.Errorf("OpenAlex = %q, %q; want %q, %q", cfg.OpenAlexBaseURL, cfg.OpenAlexMailto, tt.wantBase, tt.wantMailto)
			}
		})
	}
}

func TestLoadORCIDSettings(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		wantBase string
		wantErr  bool
	}{
		{"default", "", DefaultORCIDBaseURL, false},
		{"set, trailing slash removed", "http://127.0.0.1:4011/", "http://127.0.0.1:4011", false},
		{"without a scheme", "pub.orcid.org", "", true},
		{"with a query", "https://pub.orcid.org?x=1", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}
			if tt.base != "" {
				v["ORCID_BASE_URL"] = tt.base
			}
			cfg, err := Load(env(v))
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "ORCID_BASE_URL") {
					t.Fatalf("Load() error = %v, want one naming ORCID_BASE_URL", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.ORCIDBaseURL != tt.wantBase {
				t.Errorf("ORCIDBaseURL = %q, want %q", cfg.ORCIDBaseURL, tt.wantBase)
			}
		})
	}
}

func TestLoadMessageEditWindow(t *testing.T) {
	cfg, err := Load(env(map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}))
	if err != nil || cfg.MessageEditWindow != 15*time.Minute {
		t.Errorf("default = %v (%v)", cfg.MessageEditWindow, err)
	}
	cfg, err = Load(env(map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "MESSAGE_EDIT_WINDOW": " 90s "}))
	if err != nil || cfg.MessageEditWindow != 90*time.Second {
		t.Errorf("set = %v (%v)", cfg.MessageEditWindow, err)
	}
	for _, bad := range []string{"0", "-1m", "15", "soon"} {
		v := map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "MESSAGE_EDIT_WINDOW": bad}
		if _, err := Load(env(v)); err == nil || !strings.Contains(err.Error(), "MESSAGE_EDIT_WINDOW") {
			t.Errorf("MESSAGE_EDIT_WINDOW=%s: error = %v", bad, err)
		}
	}
}

func TestLoadMessageLimits(t *testing.T) {
	base := map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}
	cfg, err := Load(env(base))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MessagesPerMinute != 30 || cfg.MessagesPerDay != 1000 || cfg.RequestsPerMinute != 5 || cfg.RequestsPerDay != 20 {
		t.Errorf("defaults = %d, %d, %d, %d", cfg.MessagesPerMinute, cfg.MessagesPerDay, cfg.RequestsPerMinute, cfg.RequestsPerDay)
	}

	set := map[string]string{
		"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "MESSAGE_LIMIT_PER_MINUTE": "1", "MESSAGE_LIMIT_PER_DAY": "2",
		"MESSAGE_REQUEST_LIMIT_PER_MINUTE": "3", "MESSAGE_REQUEST_LIMIT_PER_DAY": " 4 ",
	}
	if cfg, err = Load(env(set)); err != nil {
		t.Fatal(err)
	}
	if cfg.MessagesPerMinute != 1 || cfg.MessagesPerDay != 2 || cfg.RequestsPerMinute != 3 || cfg.RequestsPerDay != 4 {
		t.Errorf("set = %d, %d, %d, %d", cfg.MessagesPerMinute, cfg.MessagesPerDay, cfg.RequestsPerMinute, cfg.RequestsPerDay)
	}

	for _, name := range []string{"MESSAGE_LIMIT_PER_MINUTE", "MESSAGE_LIMIT_PER_DAY", "MESSAGE_REQUEST_LIMIT_PER_MINUTE", "MESSAGE_REQUEST_LIMIT_PER_DAY"} {
		for _, bad := range []string{"0", "-1", "many"} {
			v := map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, name: bad}
			if _, err := Load(env(v)); err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("%s=%s: error = %v", name, bad, err)
			}
		}
	}
}

// FR-16.8: the message search limit defaults to 30 a minute and must be a positive number.
func TestLoadMessageSearchLimit(t *testing.T) {
	base := map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}
	if cfg, err := Load(env(base)); err != nil || cfg.SearchesPerMinute != 30 {
		t.Errorf("default = %d (%v)", cfg.SearchesPerMinute, err)
	}
	base["MESSAGE_SEARCH_LIMIT_PER_MINUTE"] = " 7 "
	if cfg, err := Load(env(base)); err != nil || cfg.SearchesPerMinute != 7 {
		t.Errorf("set = %d (%v)", cfg.SearchesPerMinute, err)
	}
	for _, bad := range []string{"0", "-1", "many"} {
		base["MESSAGE_SEARCH_LIMIT_PER_MINUTE"] = bad
		if _, err := Load(env(base)); err == nil || !strings.Contains(err.Error(), "MESSAGE_SEARCH_LIMIT_PER_MINUTE") {
			t.Errorf("%s: error = %v", bad, err)
		}
	}
}

func TestLoadClamAVAddr(t *testing.T) {
	cfg, err := Load(env(map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}))
	if err != nil || cfg.ClamAVAddr != DefaultClamAVAddr {
		t.Errorf("default = %q (%v)", cfg.ClamAVAddr, err)
	}
	cfg, err = Load(env(map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "CLAMAV_ADDR": " clamav:3310 "}))
	if err != nil || cfg.ClamAVAddr != "clamav:3310" {
		t.Errorf("set = %q (%v)", cfg.ClamAVAddr, err)
	}
	for _, bad := range []string{"clamav", "tcp://clamav:3310", "clamav:port"} {
		v := map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "CLAMAV_ADDR": bad}
		if _, err := Load(env(v)); err == nil || !strings.Contains(err.Error(), "CLAMAV_ADDR") {
			t.Errorf("CLAMAV_ADDR=%s: error = %v", bad, err)
		}
	}
}

// FR-12.2 (docs/decisions.md 2026-11-18): the figure limits come from settings, in bytes or
// with K, M or G; a figure is at most 200 MiB in every environment, no more than a project
// holds and no more than the scanner reads whole.
func TestLoadFigureLimits(t *testing.T) {
	base := map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}
	cfg, err := Load(env(base))
	if err != nil || cfg.FigureMaxBytes != 50<<20 || cfg.ProjectStorageMaxBytes != 500<<20 || cfg.ClamAVMaxBytes != 210<<20 {
		t.Fatalf("defaults = %d, %d, %d (%v)", cfg.FigureMaxBytes, cfg.ProjectStorageMaxBytes, cfg.ClamAVMaxBytes, err)
	}
	cfg, err = Load(env(with(base, "FIGURE_MAX_BYTES", "10485760", "PROJECT_STORAGE_MAX_BYTES", " 1G ", "CLAMAV_MAX_BYTES", "300m")))
	if err != nil || cfg.FigureMaxBytes != 10<<20 || cfg.ProjectStorageMaxBytes != 1<<30 || cfg.ClamAVMaxBytes != 300<<20 {
		t.Errorf("set = %d, %d, %d (%v)", cfg.FigureMaxBytes, cfg.ProjectStorageMaxBytes, cfg.ClamAVMaxBytes, err)
	}
	// 200 MiB is allowed, with a scanner limit above it; one byte more is not, in development
	// as in production.
	if _, err := Load(env(with(base, "FIGURE_MAX_BYTES", "200M"))); err != nil {
		t.Errorf("200M: %v", err)
	}
	for _, v := range []map[string]string{base, prodMail} {
		_, err := Load(env(with(v, "FIGURE_MAX_BYTES", "209715201", "CLAMAV_MAX_BYTES", "1G")))
		if err == nil || !strings.Contains(err.Error(), "FIGURE_MAX_BYTES must be at most") {
			t.Errorf("over 200M (%s): %v", v["APP_ENV"], err)
		}
	}
	for name, kv := range map[string][]string{
		"over the project limit": {"FIGURE_MAX_BYTES", "60M", "PROJECT_STORAGE_MAX_BYTES", "50M"},
		"over the scanner limit": {"FIGURE_MAX_BYTES", "60M", "CLAMAV_MAX_BYTES", "59M"},
	} {
		if _, err := Load(env(with(base, kv...))); err == nil || !strings.Contains(err.Error(), "must not be more than") {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, bad := range []string{"0", "-1", "50MB", "five", "1.5M", "9999999999999G"} {
		if _, err := Load(env(with(base, "FIGURE_MAX_BYTES", bad))); err == nil || !strings.Contains(err.Error(), "FIGURE_MAX_BYTES must be a size") {
			t.Errorf("FIGURE_MAX_BYTES=%s: %v", bad, err)
		}
	}
}

// prodMail is a production mail setup that passes every FR-17.1 check.
var prodMail = map[string]string{
	"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "APP_ENV": "production",
	"SMTP_HOST": "smtp-relay.brevo.com", "SMTP_PORT": "587", "SMTP_TLS": "starttls",
	"SMTP_USERNAME": "apikey", "SMTP_PASSWORD": "provider-key",
	"AI_SERVICE_TOKEN": testAIToken, "SYNC_SERVICE_TOKEN": testSyncTok, "COMPILE_SERVICE_TOKEN": testCompTok,
}

// with returns a copy of base with the key and value pairs in kv set.
func with(base map[string]string, kv ...string) map[string]string {
	out := maps.Clone(base)
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i]] = kv[i+1]
	}
	return out
}

// FR-17.1: the provider is chosen by settings alone, and production refuses a setup that
// would send unencrypted, to this machine or without logging in.
func TestLoadMailProvider(t *testing.T) {
	t.Run("development defaults: Mailpit, plain, no login", func(t *testing.T) {
		cfg, err := Load(env(map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.AppEnv != EnvDevelopment || cfg.SMTPTLS != SMTPTLSNone || cfg.SMTPUsername != "" || cfg.SMTPPassword != "" {
			t.Errorf("cfg = env %q, tls %q, user %q", cfg.AppEnv, cfg.SMTPTLS, cfg.SMTPUsername)
		}
	})
	t.Run("a valid production setup loads", func(t *testing.T) {
		cfg, err := Load(env(prodMail))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.AppEnv != EnvProduction || cfg.SMTPTLS != SMTPTLSStartTLS || cfg.SMTPUsername != "apikey" || cfg.SMTPPassword != "provider-key" {
			t.Errorf("cfg = %+v", cfg)
		}
	})
	t.Run("implicit TLS in production", func(t *testing.T) {
		if _, err := Load(env(with(prodMail, "SMTP_TLS", "TLS", "SMTP_PORT", "465"))); err != nil {
			t.Error(err)
		}
	})

	refusals := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"production without TLS", with(prodMail, "SMTP_TLS", "none"), "SMTP_TLS must be starttls or tls"},
		{"production with TLS unset (defaults to none)", with(prodMail, "SMTP_TLS", ""), "SMTP_TLS must be starttls or tls"},
		{"production to localhost", with(prodMail, "SMTP_HOST", "localhost"), "SMTP_HOST must be"},
		{"production to 127.0.0.1", with(prodMail, "SMTP_HOST", "127.0.0.1"), "SMTP_HOST must be"},
		{"production to ::1", with(prodMail, "SMTP_HOST", "::1"), "SMTP_HOST must be"},
		{"production with SMTP_HOST unset (defaults to localhost)", with(prodMail, "SMTP_HOST", ""), "SMTP_HOST must be"},
		{"production without credentials", with(prodMail, "SMTP_USERNAME", "", "SMTP_PASSWORD", ""), "required when APP_ENV=production"},
		{"username without password", with(prodMail, "SMTP_PASSWORD", ""), "must be set together"},
		{"password without username", map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "SMTP_PASSWORD": "x"}, "must be set together"},
		{"unknown TLS mode", map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "SMTP_TLS": "ssl"}, "SMTP_TLS must be starttls, tls or none"},
		{"unknown APP_ENV", map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "APP_ENV": "staging"}, "APP_ENV must be"},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(env(tt.vars))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want one containing %q", err, tt.want)
			}
			if strings.Contains(err.Error(), "provider-key") {
				t.Errorf("error repeats the SMTP password: %v", err)
			}
		})
	}
}

// FR-17.9 and FR-17.4: test domains apply only outside production; the purge age defaults
// to 7 days.
func TestLoadEmailTrustSettings(t *testing.T) {
	base := map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}
	cfg, err := Load(env(with(base, "TEST_EMAIL_DOMAINS", " Example.com, compare.researchsphere.local,example.com ,")))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.TestEmailDomains, []string{"example.com", "compare.researchsphere.local"}) || cfg.TestEmailDomainsIgnored {
		t.Errorf("development: TestEmailDomains = %v, ignored = %v", cfg.TestEmailDomains, cfg.TestEmailDomainsIgnored)
	}
	if cfg.UnverifiedAccountDays != 7 {
		t.Errorf("UnverifiedAccountDays = %d, want 7", cfg.UnverifiedAccountDays)
	}

	cfg, err = Load(env(with(prodMail, "TEST_EMAIL_DOMAINS", "example.com")))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.TestEmailDomains) != 0 || !cfg.TestEmailDomainsIgnored {
		t.Errorf("production: TestEmailDomains = %v, ignored = %v; want none, ignored", cfg.TestEmailDomains, cfg.TestEmailDomainsIgnored)
	}
	if cfg, err = Load(env(prodMail)); err != nil || cfg.TestEmailDomainsIgnored {
		t.Errorf("production without the setting: ignored = %v, %v", cfg.TestEmailDomainsIgnored, err)
	}

	if cfg, err = Load(env(with(base, "UNVERIFIED_ACCOUNT_DAYS", "30"))); err != nil || cfg.UnverifiedAccountDays != 30 {
		t.Errorf("UnverifiedAccountDays = %d, %v; want 30", cfg.UnverifiedAccountDays, err)
	}
	for _, bad := range []map[string]string{
		with(base, "UNVERIFIED_ACCOUNT_DAYS", "0"),
		with(base, "UNVERIFIED_ACCOUNT_DAYS", "seven"),
		with(base, "TEST_EMAIL_DOMAINS", "localhost"),
		with(base, "TEST_EMAIL_DOMAINS", "exa mple.com"),
		with(base, "TEST_EMAIL_DOMAINS", "@example.com"),
	} {
		if _, err := Load(env(bad)); err == nil {
			t.Errorf("Load(%v) succeeded, want an error", bad)
		}
	}
}

// The AI service (AI-1): optional in development, required in production, and its token
// is long enough and not another secret.
func TestLoadAIService(t *testing.T) {
	dev := map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}

	t.Run("development defaults: local URL, no token (AI off)", func(t *testing.T) {
		cfg, err := Load(env(dev))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.AIServiceURL != DefaultAIServiceURL || cfg.AIServiceToken != "" {
			t.Errorf("cfg = %q, token set %v", cfg.AIServiceURL, cfg.AIServiceToken != "")
		}
	})
	t.Run("set", func(t *testing.T) {
		cfg, err := Load(env(with(dev, "AI_SERVICE_URL", " http://ai:8000/ ", "AI_SERVICE_TOKEN", " "+testAIToken+" ")))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.AIServiceURL != "http://ai:8000" || cfg.AIServiceToken != testAIToken {
			t.Errorf("cfg = %q, token %v", cfg.AIServiceURL, cfg.AIServiceToken == testAIToken)
		}
	})
	t.Run("production with a token loads", func(t *testing.T) {
		if _, err := Load(env(prodMail)); err != nil {
			t.Error(err)
		}
	})

	refusals := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"production without a token", with(prodMail, "AI_SERVICE_TOKEN", ""), "AI_SERVICE_TOKEN is required"},
		{"short token", with(dev, "AI_SERVICE_TOKEN", "short"), "AI_SERVICE_TOKEN must be at least 32"},
		{"token equal to JWT_SECRET", with(dev, "AI_SERVICE_TOKEN", testSecret), "AI_SERVICE_TOKEN must differ"},
		{"token equal to UNSUBSCRIBE_SECRET", with(dev, "AI_SERVICE_TOKEN", testUnsubKey), "AI_SERVICE_TOKEN must differ"},
		{"URL without a scheme", with(dev, "AI_SERVICE_URL", "ai:8000"), "AI_SERVICE_URL must be"},
		{"URL with a query", with(dev, "AI_SERVICE_URL", "http://ai:8000/?x=1"), "AI_SERVICE_URL must be"},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(env(tt.vars))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want %q", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), testSecret) {
				t.Errorf("error repeats a secret: %v", err)
			}
		})
	}
}

// The sync server's token and the internal listener (slice 22, docs/sync.md).
func TestLoadSyncService(t *testing.T) {
	dev := map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}

	cfg, err := Load(env(dev))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SyncServiceToken != "" || cfg.InternalAPIAddr != DefaultInternalAPIAddr {
		t.Errorf("defaults: token set %v, addr %q", cfg.SyncServiceToken != "", cfg.InternalAPIAddr)
	}
	cfg, err = Load(env(with(dev, "SYNC_SERVICE_TOKEN", " "+testSyncTok+" ", "INTERNAL_API_ADDR", "10.0.0.5:8081")))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SyncServiceToken != testSyncTok || cfg.InternalAPIAddr != "10.0.0.5:8081" {
		t.Errorf("set: token %v, addr %q", cfg.SyncServiceToken == testSyncTok, cfg.InternalAPIAddr)
	}

	refusals := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"production without a token", with(prodMail, "SYNC_SERVICE_TOKEN", ""), "SYNC_SERVICE_TOKEN is required"},
		{"short token", with(dev, "SYNC_SERVICE_TOKEN", "short"), "SYNC_SERVICE_TOKEN must be at least 32"},
		{"token equal to JWT_SECRET", with(dev, "SYNC_SERVICE_TOKEN", testSecret), "SYNC_SERVICE_TOKEN must differ"},
		{"token equal to AI_SERVICE_TOKEN", with(dev, "SYNC_SERVICE_TOKEN", testAIToken, "AI_SERVICE_TOKEN", testAIToken), "SYNC_SERVICE_TOKEN must differ"},
		{"address with a scheme", with(dev, "INTERNAL_API_ADDR", "http://127.0.0.1:8081"), "INTERNAL_API_ADDR must be"},
		{"address without a port", with(dev, "INTERNAL_API_ADDR", "127.0.0.1"), "INTERNAL_API_ADDR must be"},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(env(tt.vars))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want %q", err, tt.want)
			}
			if err != nil && (strings.Contains(err.Error(), testSecret) || strings.Contains(err.Error(), testAIToken)) {
				t.Errorf("error repeats a secret: %v", err)
			}
		})
	}
}

// The recommendation score map (AI-2.2): the fitted defaults, or values that keep the order
// of similarity (slope above 0) and name a model.
func TestLoadRecsScoreMap(t *testing.T) {
	dev := map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}

	cfg, err := Load(env(dev))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RecsScoreModel != DefaultRecsScoreModel || cfg.RecsScoreSlope != DefaultRecsScoreSlope || cfg.RecsScoreIntercept != DefaultRecsScoreIntercept {
		t.Errorf("defaults = %q %v %v", cfg.RecsScoreModel, cfg.RecsScoreSlope, cfg.RecsScoreIntercept)
	}
	if cfg.RecsProjectScoreSlope != DefaultRecsProjectScoreSlope || cfg.RecsProjectScoreIntercept != DefaultRecsProjectScoreIntercept {
		t.Errorf("project defaults = %v %v", cfg.RecsProjectScoreSlope, cfg.RecsProjectScoreIntercept)
	}
	if !(DefaultRecsScoreSlope > 0) || !(DefaultRecsProjectScoreSlope > 0) {
		t.Error("the default slopes must be above 0")
	}
	cfg, err = Load(env(with(dev, "RECS_SCORE_MODEL", " m@2 ", "RECS_SCORE_SLOPE", "30.5", "RECS_SCORE_INTERCEPT", "-25",
		"RECS_PROJECT_SCORE_SLOPE", "40", "RECS_PROJECT_SCORE_INTERCEPT", "-33.5")))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RecsScoreModel != "m@2" || cfg.RecsScoreSlope != 30.5 || cfg.RecsScoreIntercept != -25 {
		t.Errorf("set = %q %v %v", cfg.RecsScoreModel, cfg.RecsScoreSlope, cfg.RecsScoreIntercept)
	}
	if cfg.RecsProjectScoreSlope != 40 || cfg.RecsProjectScoreIntercept != -33.5 {
		t.Errorf("project set = %v %v", cfg.RecsProjectScoreSlope, cfg.RecsProjectScoreIntercept)
	}

	for _, tt := range []struct{ name, key, value, want string }{
		{"zero slope", "RECS_SCORE_SLOPE", "0", "RECS_SCORE_SLOPE must be more than 0"},
		{"negative slope", "RECS_SCORE_SLOPE", "-3", "RECS_SCORE_SLOPE must be more than 0"},
		{"slope not a number", "RECS_SCORE_SLOPE", "steep", "RECS_SCORE_SLOPE must be a finite number"},
		{"infinite slope", "RECS_SCORE_SLOPE", "Inf", "RECS_SCORE_SLOPE must be a finite number"},
		{"NaN intercept", "RECS_SCORE_INTERCEPT", "NaN", "RECS_SCORE_INTERCEPT must be a finite number"},
		{"model without revision", "RECS_SCORE_MODEL", "e5", "RECS_SCORE_MODEL must be"},
		{"model with a space", "RECS_SCORE_MODEL", "e5@a b", "RECS_SCORE_MODEL must be"},
		{"zero project slope", "RECS_PROJECT_SCORE_SLOPE", "0", "RECS_PROJECT_SCORE_SLOPE must be more than 0"},
		{"project slope not a number", "RECS_PROJECT_SCORE_SLOPE", "steep", "RECS_PROJECT_SCORE_SLOPE must be a finite number"},
		{"NaN project intercept", "RECS_PROJECT_SCORE_INTERCEPT", "NaN", "RECS_PROJECT_SCORE_INTERCEPT must be a finite number"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(env(with(dev, tt.key, tt.value)))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadSearchAITimeout(t *testing.T) {
	cfg, err := Load(env(map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}))
	if err != nil || cfg.SearchAITimeout != 1500*time.Millisecond {
		t.Errorf("default = %v (%v)", cfg.SearchAITimeout, err)
	}
	cfg, err = Load(env(map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "SEARCH_AI_TIMEOUT": "800ms"}))
	if err != nil || cfg.SearchAITimeout != 800*time.Millisecond {
		t.Errorf("set = %v (%v)", cfg.SearchAITimeout, err)
	}
	for _, bad := range []string{"0s", "-1s", "soon", "11s"} {
		if _, err := Load(env(map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL, "SEARCH_AI_TIMEOUT": bad})); err == nil || !strings.Contains(err.Error(), "SEARCH_AI_TIMEOUT") {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
}

func TestLoadCompileService(t *testing.T) {
	dev := map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}
	cfg, err := Load(env(dev))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CompileServiceToken != "" || cfg.CompileMaxPDFBytes != DefaultCompileMaxPDFBytes ||
		cfg.CompileMaxInputBytes != DefaultCompileMaxInputBytes || cfg.ArchiveMaxBytes != DefaultArchiveMaxBytes ||
		cfg.CompileQueueMax != DefaultCompileQueueMax {
		t.Errorf("defaults: %+v", cfg)
	}
	cfg, err = Load(env(with(dev, "COMPILE_SERVICE_TOKEN", testCompTok, "COMPILE_MAX_PDF_BYTES", "60M", "COMPILE_QUEUE_MAX", "5")))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CompileServiceToken != testCompTok || cfg.CompileMaxPDFBytes != 60<<20 || cfg.CompileQueueMax != 5 {
		t.Errorf("set: token %v, pdf %d, queue %d", cfg.CompileServiceToken == testCompTok, cfg.CompileMaxPDFBytes, cfg.CompileQueueMax)
	}
	refusals := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"production without a token", with(prodMail, "COMPILE_SERVICE_TOKEN", ""), "COMPILE_SERVICE_TOKEN is required"},
		{"short token", with(dev, "COMPILE_SERVICE_TOKEN", "short"), "COMPILE_SERVICE_TOKEN must be at least 32"},
		{"token equal to the sync token", with(dev, "COMPILE_SERVICE_TOKEN", testSyncTok, "SYNC_SERVICE_TOKEN", testSyncTok), "COMPILE_SERVICE_TOKEN must differ"},
		{"PDF over the database's ceiling", with(dev, "COMPILE_MAX_PDF_BYTES", "300M"), "COMPILE_MAX_PDF_BYTES must be at most"},
		{"input over the sandbox", with(dev, "COMPILE_MAX_INPUT_BYTES", "1G"), "COMPILE_MAX_INPUT_BYTES must be at most"},
		{"queue of zero", with(dev, "COMPILE_QUEUE_MAX", "0"), "COMPILE_QUEUE_MAX"},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(env(tt.vars))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want %q", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), testSyncTok) {
				t.Errorf("error repeats a secret: %v", err)
			}
		})
	}
}
