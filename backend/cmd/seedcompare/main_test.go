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

// Every speaker writes, with a run of two somewhere, so the thread has groups to show.
func TestScriptHasGroups(t *testing.T) {
	if n := len(script); n < 35 || n > 45 {
		t.Errorf("the script has %d lines, want about 40", n)
	}
	runs := 0
	for i := 1; i < len(script); i++ {
		if speaker(i) == speaker(i-1) {
			runs++
		}
	}
	if runs == 0 {
		t.Error("nobody writes twice in a row")
	}
	// Sending moves the sender's marker, so nothing Alpha has not read may come before a
	// message of Alpha's.
	for i := len(script) - unreadByAlpha; i < len(script); i++ {
		if speaker(i) {
			t.Errorf("Alpha writes line %d, which Alpha has not read", i)
		}
	}
}
