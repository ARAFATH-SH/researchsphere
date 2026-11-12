package main

import (
	"strings"
	"testing"
)

func TestCheckTarget(t *testing.T) {
	// The password in these URLs is checked for: an error must never repeat it.
	const pass = "secretpw"
	url := func(host, name string) string { return "postgres://u:" + pass + "@" + host + "/" + name }
	local := url("localhost:5433", "researchsphere")
	tests := []struct {
		name, url, env, want string
	}{
		{name: "local development", url: local},
		{name: "explicit development", url: local, env: "development"},
		{name: "loopback address", url: url("127.0.0.1:5433", "researchsphere")},
		{name: "no URL", env: "development", want: "not set"},
		{name: "test database", url: url("localhost:5433", "researchsphere_test"), want: "test database"},
		{name: "production", url: local, env: "production", want: "production"},
		{name: "production in capitals", url: local, env: " PRODUCTION ", want: "production"},
		{name: "remote host", url: url("db.example.org:5432", "researchsphere"), want: "local database"},
		{name: "no database name", url: url("localhost:5433", ""), want: "no database"},
		{name: "not a URL", url: url("localhost:port", "x"), want: "not a valid URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkTarget(tt.url, tt.env)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("checkTarget: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want one with %q", err, tt.want)
			}
			if strings.Contains(err.Error(), pass) {
				t.Errorf("the error repeats the password: %v", err)
			}
		})
	}
}

func TestCheckPassword(t *testing.T) {
	for _, p := range []string{"", "short", strings.Repeat("x", 129)} {
		if err := checkPassword(p); err == nil || !strings.Contains(err.Error(), "DEV_SEED_PASSWORD") {
			t.Errorf("checkPassword(%d characters) = %v, want an error naming DEV_SEED_PASSWORD", len(p), err)
		}
	}
	if err := checkPassword("a local password"); err != nil {
		t.Errorf("checkPassword(valid) = %v", err)
	}
}

// The accounts are at the test domain and include one administrator (FR-17.9).
func TestAccounts(t *testing.T) {
	admins := 0
	for _, a := range accounts {
		if !strings.HasSuffix(a.email, "@example.com") {
			t.Errorf("%s is not at example.com", a.email)
		}
		if a.role == "admin" {
			admins++
		}
	}
	if len(accounts) != 5 || admins != 1 {
		t.Errorf("%d accounts with %d administrators, want 5 with 1", len(accounts), admins)
	}
}
