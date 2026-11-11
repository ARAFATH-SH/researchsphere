//go:build integration

package postgres

import (
	"time"

	"errors"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

func newReport(t *testing.T, s *Store, c, reporter, reported string, message *string) string {
	t.Helper()
	id, at, err := s.InsertReport(t.Context(), domain.NewReport{ConversationID: c, ReporterID: reporter, ReportedID: &reported,
		MessageID: message, Reason: domain.ReasonSpam})
	if err != nil || at.IsZero() {
		t.Fatalf("InsertReport: %v, %v", at, err)
	}
	return id
}

// NFR-13: the read starts from a report and stops at its moment.
func TestReportedMessagesStopAtTheReport(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	one := add(t, s, c.ID, ada, "one")
	two := add(t, s, c.ID, bob, "two")
	id := newReport(t, s, c.ID, ada, bob, &two.ID)
	add(t, s, c.ID, bob, "three")

	got, err := s.ListReportedMessages(ctx, id, "", 10)
	if err != nil || len(got) != 2 || got[0].ID != two.ID || got[1].ID != one.ID {
		t.Fatalf("ListReportedMessages = %+v, %v", got, err)
	}
	got, err = s.ListReportedMessages(ctx, id, two.ID, 10)
	if err != nil || len(got) != 1 || got[0].ID != one.ID {
		t.Errorf("before = %+v, %v", got, err)
	}
	if got, err := s.ListReportedMessages(ctx, c.ID, "", 10); err != nil || len(got) != 0 {
		t.Errorf("a conversation's ID opened messages: %+v, %v", got, err)
	}
	if got, err := s.ListReportedMessages(ctx, "not-a-uuid", "", 10); err != nil || len(got) != 0 {
		t.Errorf("a bad ID: %+v, %v", got, err)
	}
}

func TestReportRoundTrip(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, admin := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Admin")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	m := add(t, s, c.ID, bob, "rude")
	id := newReport(t, s, c.ID, ada, bob, &m.ID)

	r, err := s.FindReport(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Reporter.FullName != "Ada" || r.Reported.FullName != "Bob" || r.Reported.Headline == nil ||
		r.Reporter.Role != "researcher" || r.Reported.Role != "researcher" ||
		r.MessageID == nil || *r.MessageID != m.ID || r.ResolvedAt != nil || r.Outcome != nil {
		t.Errorf("report = %+v", r)
	}
	if _, err := s.FindReport(ctx, "not-a-uuid"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("bad ID: %v", err)
	}
	if sender, found, err := s.FindMessageSender(ctx, c.ID, m.ID, time.Time{}); err != nil || !found || sender != bob {
		t.Errorf("FindMessageSender = %s, %v, %v", sender, found, err)
	}
	if _, found, err := s.FindMessageSender(ctx, c.ID, id, time.Time{}); err != nil || found {
		t.Errorf("an unknown message: %v, %v", found, err)
	}

	note := "warned"
	for _, want := range []bool{true, false} {
		ok, err := s.ResolveReport(ctx, id, admin, domain.OutcomeActionTaken, &note)
		if err != nil || ok != want {
			t.Fatalf("ResolveReport = %v, %v; want %v", ok, err, want)
		}
	}
	if r, _ := s.FindReport(ctx, id); r.ResolvedAt == nil || *r.Outcome != domain.OutcomeActionTaken || *r.Note != note {
		t.Errorf("resolved = %+v", r)
	}
	for open, want := range map[bool]int{true: 0, false: 1} {
		items, total, err := s.ListReports(ctx, open, 20, 0)
		if err != nil || total != want || len(items) != want {
			t.Errorf("open %v: %d, %d, %v", open, len(items), total, err)
		}
	}
	if err := s.AddReportView(ctx, id, admin); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM message_report_views WHERE report_id = $1 AND admin_id = $2", id, admin).Scan(&n); err != nil || n != 1 {
		t.Errorf("views: %d, %v", n, err)
	}
}

// DeclineRequests touches only a pending request.
func TestDeclineRequests(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy")
	pending := create(t, s, ada, bob, domain.StateRequest, &ada)
	active := create(t, s, ada, cy, domain.StateActive, nil)
	if err := s.DeclineRequests(ctx, bob, ada); err != nil {
		t.Fatal(err)
	}
	if err := s.DeclineRequests(ctx, ada, cy); err != nil {
		t.Fatal(err)
	}
	if c, err := s.FindConversation(ctx, pending.ID, ada); err != nil || c.State != domain.StateDeclined {
		t.Errorf("request: %+v, %v", c, err)
	}
	if c, err := s.FindConversation(ctx, active.ID, ada); err != nil || c.State != domain.StateActive {
		t.Errorf("active: %+v, %v", c, err)
	}
}

// A conversation's flags are as seen by the caller.
func TestItemsCarryBlocks(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	if _, err := pool.Exec(ctx, "INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)", ada, bob); err != nil {
		t.Fatal(err)
	}
	mine, err := s.FindItem(ctx, c.ID, ada)
	if err != nil || !mine.BlockedByMe || mine.BlockedMe {
		t.Errorf("blocker: %+v, %v", mine, err)
	}
	theirs, err := s.ListItems(ctx, bob, domain.BoxInbox, nil, 10)
	if err != nil || len(theirs) != 1 || theirs[0].BlockedByMe || !theirs[0].BlockedMe {
		t.Errorf("blocked: %+v, %v", theirs, err)
	}
}
