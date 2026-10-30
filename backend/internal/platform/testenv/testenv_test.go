package testenv

import (
	"regexp"
	"strings"
	"testing"
)

func TestCheckDatabaseURL(t *testing.T) {
	tests := []struct {
		name, url string
		ok        bool
	}{
		{"test database", "postgres://u:p@localhost:5433/researchsphere_test?sslmode=disable", true},
		{"development database", "postgres://u:p@localhost:5433/researchsphere?sslmode=disable", false},
		{"name test without suffix", "postgres://u:p@localhost:5433/test", false},
		{"suffix only", "postgres://u:p@localhost:5433/_test", false},
		{"_test in the middle", "postgres://u:p@localhost:5433/researchsphere_test_old", false},
		{"no database name", "postgres://u:p@localhost:5433", false},
		{"empty", "", false},
		{"blank", "  ", false},
		{"not a URL", "postgres://%zz", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CheckDatabaseURL(tt.url); (err == nil) != tt.ok {
				t.Errorf("CheckDatabaseURL(%q) = %v, want ok = %v", tt.url, err, tt.ok)
			}
		})
	}
}

func TestCheckRedisURL(t *testing.T) {
	tests := []struct {
		name, test, dev string
		ok              bool
	}{
		{"DB 15", "redis://localhost:6379/15", "redis://localhost:6379/0", true},
		{"no dev URL", "redis://localhost:6379/15", "", true},
		{"same DB on another server", "redis://other:6379/15", "redis://localhost:6379/15", true},
		{"DB 0", "redis://localhost:6379/0", "redis://localhost:6379/1", false},
		{"no DB means 0", "redis://localhost:6379", "", false},
		{"same DB as dev", "redis://localhost:6379/3", "redis://localhost:6379/3", false},
		{"empty", "", "", false},
		{"not a URL", "http://localhost:6379/15", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CheckRedisURL(tt.test, tt.dev); (err == nil) != tt.ok {
				t.Errorf("CheckRedisURL(%q, %q) = %v, want ok = %v", tt.test, tt.dev, err, tt.ok)
			}
		})
	}
}

func TestUniqueValues(t *testing.T) {
	if w := Word(t); !regexp.MustCompile(`^Q[a-w]{8}$`).MatchString(w) {
		t.Errorf("Word() = %q", w)
	}
	if a, b := Email(t), Email(t); a == b || !strings.HasSuffix(a, "@example.org") {
		t.Errorf("Email() = %q, %q; want two different addresses", a, b)
	}
	for range 50 {
		id := ORCIDiD(t)
		if !regexp.MustCompile(`^\d{4}-\d{4}-\d{4}-\d{3}[\dX]$`).MatchString(id) {
			t.Fatalf("ORCIDiD() = %q", id)
		}
		digits := strings.ReplaceAll(id, "-", "")
		total := 0
		for _, r := range digits[:15] {
			total = (total + int(r-'0')) * 2
		}
		want := (12 - total%11) % 11
		got := int(digits[15] - '0')
		if digits[15] == 'X' {
			got = 10
		}
		if got != want {
			t.Fatalf("ORCIDiD() = %q: check digit %d, want %d", id, got, want)
		}
	}
}
