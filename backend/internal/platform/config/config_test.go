package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

const (
	testPassword = "s3cret-pw"
	testDBURL    = "postgres://user:" + testPassword + "@localhost:5433/db?sslmode=disable"
	testRedisURL = "redis://:" + testPassword + "@localhost:6379/0"
	testSecret   = "test-jwt-secret-0123456789abcdefghij"
	testS3Secret = "test-s3-secret-key" //nolint:gosec // test value, not a credential
)

// validDefaults are served for these settings unless vars sets them (even to "").
var validDefaults = map[string]string{
	"JWT_SECRET":    testSecret,
	"S3_ENDPOINT":   "localhost:9000",
	"S3_BUCKET":     "researchsphere",
	"S3_ACCESS_KEY": "researchsphere",
	"S3_SECRET_KEY": testS3Secret,
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
		})
	}
}

func TestLogValueHidesSecrets(t *testing.T) {
	cfg, err := Load(env(map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config loaded", "config", cfg)

	out := buf.String()
	for _, secret := range []string{testPassword, testDBURL, testRedisURL, "postgres://", "redis://", testSecret, testS3Secret} {
		if strings.Contains(out, secret) {
			t.Errorf("log line contains %q: %s", secret, out)
		}
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
		if s.Endpoint != "localhost:9000" || s.Bucket != "researchsphere" || s.Region != DefaultS3Region ||
			s.AccessKey != "researchsphere" || s.SecretKey != testS3Secret || s.UseSSL {
			t.Errorf("Storage = %+v", s)
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
