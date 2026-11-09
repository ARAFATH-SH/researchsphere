//go:build integration

package clamav

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// eicar is the EICAR anti-malware test file: harmless, and every scanner reports it. It is
// built from two halves so this source file is not itself flagged by a scanner on the
// developer's machine.
var eicar = `X5O!P%@AP[4\PZX54(P^)7CC)7}$` + `EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`

// TestRealClamd scans the EICAR string and a clean buffer with a real clamd. It runs only when
// TEST_CLAMAV_ADDR names one (CI sets it once clamd answers; docs/decisions.md 2026-11-09).
func TestRealClamd(t *testing.T) {
	addr := os.Getenv("TEST_CLAMAV_ADDR")
	if addr == "" {
		t.Skip("TEST_CLAMAV_ADDR is not set; no clamd to scan with")
	}
	c := New(addr)
	if err := c.Ping(t.Context()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	v, err := c.Scan(t.Context(), strings.NewReader(eicar))
	if err != nil || !v.Infected || !strings.Contains(strings.ToLower(v.Signature), "eicar") {
		t.Errorf("EICAR: %+v, %v", v, err)
	}
	v, err = c.Scan(t.Context(), bytes.NewReader(bytes.Repeat([]byte("plain research notes\n"), 10000)))
	if err != nil || v.Infected {
		t.Errorf("clean file: %+v, %v", v, err)
	}
}
