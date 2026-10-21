package storage

import (
	"strings"
	"testing"
	"time"
)

var validConfig = Config{
	Endpoint: "localhost:9000", Region: "us-east-1", Bucket: "researchsphere",
	AccessKey: "researchsphere", SecretKey: "secret-for-tests",
}

func TestNewS3RefusesMissingSettings(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{"no endpoint", func(c *Config) { c.Endpoint = "" }, "endpoint"},
		{"no region", func(c *Config) { c.Region = "" }, "region"},
		{"no bucket", func(c *Config) { c.Bucket = "" }, "bucket"},
		{"no access key", func(c *Config) { c.AccessKey = "" }, "access key"},
		{"no secret key", func(c *Config) { c.SecretKey = "" }, "secret key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig
			tt.edit(&cfg)
			_, err := NewS3(cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("NewS3() error = %v, want one naming %q", err, tt.want)
			}
		})
	}
}

// A malformed endpoint is refused without echoing it.
func TestNewS3RefusesABadEndpoint(t *testing.T) {
	cfg := validConfig
	cfg.Endpoint = "http://localhost:9000/path"
	_, err := NewS3(cfg)
	if err == nil {
		t.Fatal("NewS3() error = nil")
	}
	if strings.Contains(err.Error(), cfg.Endpoint) || strings.Contains(err.Error(), cfg.SecretKey) {
		t.Errorf("error %q repeats a setting", err)
	}
}

// Presigning with the region set needs no server, so these run without MinIO.
func TestPresignGetExpiry(t *testing.T) {
	s, err := NewS3(validConfig)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		expiry  time.Duration
		wantErr bool
	}{
		{0, true},
		{-time.Minute, true},
		{time.Second, false},
		{5 * time.Minute, false},
		{MaxPresignExpiry, false},
		{MaxPresignExpiry + time.Second, true},
	}
	for _, tt := range tests {
		u, err := s.PresignGet(t.Context(), "photo/a", tt.expiry)
		if (err != nil) != tt.wantErr {
			t.Errorf("PresignGet(%v) error = %v, wantErr %v", tt.expiry, err, tt.wantErr)
			continue
		}
		if err == nil && (u.Path != "/researchsphere/photo/a" || u.Query().Get("X-Amz-Signature") == "") {
			t.Errorf("PresignGet(%v) = %s, want a signed path-style URL", tt.expiry, u)
		}
	}
	if _, err := s.PresignGet(t.Context(), "", time.Minute); err == nil {
		t.Error("PresignGet with no key: error = nil")
	}
}

func TestPutAndDeleteRefuseBadArguments(t *testing.T) {
	s, err := NewS3(validConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(t.Context(), "", strings.NewReader("x"), 1, "text/plain"); err == nil {
		t.Error("Put with no key: error = nil")
	}
	if err := s.Put(t.Context(), "k", strings.NewReader("x"), -1, "text/plain"); err == nil {
		t.Error("Put with an unknown size: error = nil")
	}
	if err := s.Put(t.Context(), "k", strings.NewReader("x"), 1, ""); err == nil {
		t.Error("Put with no content type: error = nil")
	}
	if err := s.Delete(t.Context(), ""); err == nil {
		t.Error("Delete with no key: error = nil")
	}
}
