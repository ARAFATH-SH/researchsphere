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
		{name: "local development", url: local, env: "development"},
		{name: "development in capitals", url: local, env: " Development "},
		{name: "loopback address", url: url("127.0.0.1:5433", "researchsphere"), env: "development"},
		{name: "APP_ENV unset", url: local, want: "APP_ENV is not set"},
		{name: "APP_ENV staging", url: local, env: "staging", want: "not development"},
		{name: "production", url: local, env: "production", want: "production"},
		{name: "production in capitals", url: local, env: " PRODUCTION ", want: "production"},
		{name: "no URL", env: "development", want: "not set"},
		{name: "test database", url: url("localhost:5433", "researchsphere_test"), env: "development", want: "test database"},
		{name: "remote host", url: url("db.example.org:5432", "researchsphere"), env: "development", want: "local database"},
		{name: "no database name", url: url("localhost:5433", ""), env: "development", want: "no database"},
		{name: "not a URL", url: url("localhost:port", "x"), env: "development", want: "not a valid URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkTarget(tt.url, tt.env)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("checkTarget() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("checkTarget() error = %v, want one containing %q", err, tt.want)
			}
			if strings.Contains(err.Error(), pass) {
				t.Error("the error repeats the password")
			}
		})
	}
}

// run refuses production and the test database before it connects to anything.
func TestRunRefusesBeforeConnecting(t *testing.T) {
	url := func(name string) string { return "postgres://u:" + "p@localhost:5433/" + name }
	for _, env := range []map[string]string{
		{"DATABASE_URL": url("researchsphere"), "APP_ENV": "production", "SAMPLE_PASSWORD": "long-enough-1"},
		{"DATABASE_URL": url("researchsphere_test"), "APP_ENV": "development", "SAMPLE_PASSWORD": "long-enough-1"},
	} {
		for _, args := range [][]string{nil, {"-purge"}} {
			var out strings.Builder
			if err := run(t.Context(), args, func(k string) string { return env[k] }, &out); err == nil {
				t.Errorf("run(%v) with %v: error = nil, want a refusal", args, env)
			}
			if out.Len() > 0 {
				t.Errorf("run(%v) printed %q before refusing", args, out.String())
			}
		}
	}
}

func TestParseFlags(t *testing.T) {
	o, err := parseFlags(nil)
	if err != nil || o.n != 1000 || o.seed != defaultSeed || o.purge || o.wait {
		t.Errorf("defaults = %+v, %v", o, err)
	}
	if o, err := parseFlags([]string{"-n", "10000", "-wait"}); err != nil || o.n != 10000 || !o.wait {
		t.Errorf("-n 10000 -wait = %+v, %v", o, err)
	}
	for _, args := range [][]string{{"-n", "0"}, {"-n", "100001"}, {"-seed", "-1"}, {"-purge", "-wait"}, {"extra"}} {
		if _, err := parseFlags(args); err == nil {
			t.Errorf("parseFlags(%v) error = nil", args)
		}
	}
}

func TestCheckPassword(t *testing.T) {
	for _, tt := range []struct {
		password string
		ok       bool
	}{{"", false}, {"short", false}, {"long-enough-1", true}, {strings.Repeat("x", 129), false}} {
		if err := checkPassword(tt.password); (err == nil) != tt.ok {
			t.Errorf("checkPassword(%d characters) error = %v", len(tt.password), err)
		}
	}
}

func TestSplitDomains(t *testing.T) {
	got := splitDomains(" example.com, Sample.ResearchSphere.local ,,")
	if strings.Join(got, "|") != "example.com|sample.researchsphere.local" {
		t.Errorf("splitDomains = %v", got)
	}
}
