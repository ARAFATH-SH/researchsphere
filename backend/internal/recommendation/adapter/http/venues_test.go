package http

import (
	"testing"
	"time"
	_ "time/tzdata" // as cmd/api: IANA zones resolve without /usr/share/zoneinfo
)

// The wall time of a deadline in its zone follows the event module's rule
// (internal/event/usecase.LoadZone): AoE is UTC-12 all year, UTC is UTC, anything else an
// IANA name.
func TestDeadlineZone(t *testing.T) {
	at := time.Date(2027, 3, 2, 11, 59, 59, 0, time.UTC)
	for _, tt := range []struct{ zone, want string }{
		{"AoE", "2027-03-01T23:59:59"},
		{"UTC", "2027-03-02T11:59:59"},
		{"Asia/Dhaka", "2027-03-02T17:59:59"},
		{"America/New_York", "2027-03-02T06:59:59"},
	} {
		loc, err := deadlineZone(tt.zone)
		if err != nil {
			t.Fatalf("%s: %v", tt.zone, err)
		}
		if got := at.In(loc).Format(localLayout); got != tt.want {
			t.Errorf("%s: %s, want %s", tt.zone, got, tt.want)
		}
	}
	if _, err := deadlineZone("Not/AZone"); err == nil {
		t.Error("an unknown zone was accepted")
	}
}
