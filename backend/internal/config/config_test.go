package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

const (
	testPassword = "s3cret-pw"
	testDBURL    = "postgres://user:" + testPassword + "@localhost:5433/db?sslmode=disable"
	testRedisURL = "redis://:" + testPassword + "@localhost:6379/0"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
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

func TestLogValueHidesSecrets(t *testing.T) {
	cfg, err := Load(env(map[string]string{"DATABASE_URL": testDBURL, "REDIS_URL": testRedisURL}))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config loaded", "config", cfg)

	out := buf.String()
	for _, secret := range []string{testPassword, testDBURL, testRedisURL, "postgres://", "redis://"} {
		if strings.Contains(out, secret) {
			t.Errorf("log line contains %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, `"port":8080`) {
		t.Errorf("log line lacks the port: %s", out)
	}
}
