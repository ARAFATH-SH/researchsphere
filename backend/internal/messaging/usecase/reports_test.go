package usecase

import (
	"errors"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

func ptr(s string) *string { return &s }

// chat returns a conversation between ada and bob with one message from each.
func chat(t *testing.T, h *harness) (id, fromAda, fromBob string) {
	t.Helper()
	h.connect("ada", "bob")
	a := h.direct(t, "ada", "bob", "hello")
	b := h.direct(t, "bob", "ada", "unwanted")
	return a.Message.ConversationID, a.Message.ID, b.Message.ID
}

func TestReport(t *testing.T) {
	h := newHarness()
	id, _, fromBob := chat(t, h)
	r, err := h.svc.Report(t.Context(), "ada", id, domain.ReportInput{
		Reason: domain.ReasonHarassment, Details: ptr("  repeated  "), MessageID: &fromBob,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.ReportedID != "bob" || r.BlockedByMe || r.ID == "" {
		t.Errorf("receipt = %+v", r)
	}
	got := h.store.reports[0]
	if got.Reported.UserID != "bob" || got.Reporter.UserID != "ada" || *got.Details != "repeated" || *got.MessageID != fromBob {
		t.Errorf("stored = %+v", got)
	}
	if h.blocks.set["ada|bob"] {
		t.Error("a report blocked the user")
	}
}

// The receipt says whether the caller has blocked the other, so the client knows what to offer.
func TestReportSaysWhetherBlocked(t *testing.T) {
	h := newHarness()
	id, _, _ := chat(t, h)
	h.blocks.set["ada|bob"] = true
	r, err := h.svc.Report(t.Context(), "ada", id, domain.ReportInput{Reason: domain.ReasonSpam})
	if err != nil || !r.BlockedByMe {
		t.Errorf("%+v, %v", r, err)
	}
}

func TestReportRefusals(t *testing.T) {
	h := newHarness()
	id, fromAda, fromBob := chat(t, h)
	other := h.direct(t, "ada", "cy", "elsewhere").Message.ID
	long := strings.Repeat("é", MaxReportText+1)
	field := func(name string) error { return &domain.InvalidFieldError{Field: name} }
	tests := []struct {
		name       string
		reporter   string
		in         domain.ReportInput
		want       error
		wantFields string
	}{
		{name: "non-member", reporter: "cy", in: domain.ReportInput{Reason: domain.ReasonSpam}, want: domain.ErrNotFound},
		{name: "bad reason", reporter: "ada", in: domain.ReportInput{Reason: "rude"}, wantFields: "reason"},
		{name: "long details", reporter: "ada", in: domain.ReportInput{Reason: domain.ReasonSpam, Details: &long}, wantFields: "details"},
		{name: "NUL", reporter: "ada", in: domain.ReportInput{Reason: domain.ReasonSpam, Details: ptr("a\x00")}, wantFields: "details"},
		{name: "message elsewhere", reporter: "ada", in: domain.ReportInput{Reason: domain.ReasonSpam, MessageID: &other}, want: domain.ErrNotFound},
		{name: "own message", reporter: "ada", in: domain.ReportInput{Reason: domain.ReasonSpam, MessageID: &fromAda}, want: domain.ErrOwnMessage},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.svc.Report(t.Context(), tc.reporter, id, tc.in)
			if tc.wantFields != "" {
				var f *domain.InvalidFieldError
				if !errors.As(err, &f) || f.Field != tc.wantFields {
					t.Errorf("err = %v, want %v", err, field(tc.wantFields))
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
	if len(h.store.reports) != 0 || h.limiter.used["reports:day|ada"] != 0 {
		t.Errorf("a refused report was stored or counted: %v %v", h.store.reports, h.limiter.used)
	}
	if _, err := h.svc.Report(t.Context(), "bob", id, domain.ReportInput{Reason: domain.ReasonSpam, MessageID: &fromAda}); err != nil {
		t.Errorf("the other member's message: %v", err)
	}
	_ = fromBob
}

func TestReportDetailsLimitIsInclusive(t *testing.T) {
	h := newHarness()
	id, _, _ := chat(t, h)
	text := strings.Repeat("é", MaxReportText)
	if _, err := h.svc.Report(t.Context(), "ada", id, domain.ReportInput{Reason: domain.ReasonOther, Details: &text}); err != nil {
		t.Error(err)
	}
	if _, err := h.svc.Report(t.Context(), "ada", id, domain.ReportInput{Reason: domain.ReasonOther, Details: ptr("   ")}); err != nil {
		t.Error(err)
	}
	if h.store.reports[1].Details != nil {
		t.Errorf("blank details stored as %q", *h.store.reports[1].Details)
	}
}

func TestReportLimit(t *testing.T) {
	h := newHarness()
	id, _, _ := chat(t, h)
	for range ReportsPerDay {
		if _, err := h.svc.Report(t.Context(), "ada", id, domain.ReportInput{Reason: domain.ReasonSpam}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := h.svc.Report(t.Context(), "ada", id, domain.ReportInput{Reason: domain.ReasonSpam})
	var limited *domain.RateLimitedError
	if !errors.As(err, &limited) {
		t.Errorf("11th report: %v", err)
	}
	if _, err := h.svc.Report(t.Context(), "bob", id, domain.ReportInput{Reason: domain.ReasonSpam}); err != nil {
		t.Errorf("another user: %v", err)
	}
}

func TestListReports(t *testing.T) {
	h := newHarness()
	id, _, _ := chat(t, h)
	r, err := h.svc.Report(t.Context(), "ada", id, domain.ReportInput{Reason: domain.ReasonSpam})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Report(t.Context(), "bob", id, domain.ReportInput{Reason: domain.ReasonSpam}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.ResolveReport(t.Context(), "admin", r.ID, domain.OutcomeDismissed, nil); err != nil {
		t.Fatal(err)
	}
	for status, want := range map[string]int{"": 1, "open": 1, "resolved": 1} {
		p, err := h.svc.ListReports(t.Context(), status, 1)
		if err != nil || p.Total != want {
			t.Errorf("status %q: %+v, %v", status, p, err)
		}
	}
	if _, err := h.svc.ListReports(t.Context(), "closed", 1); !errors.Is(err, domain.ErrInvalidStatus) {
		t.Errorf("bad status: %v", err)
	}
	for _, page := range []int{0, 1 << 40} {
		if _, err := h.svc.ListReports(t.Context(), "", page); !errors.Is(err, domain.ErrInvalidPage) {
			t.Errorf("page %d: %v", page, err)
		}
	}
	if len(h.store.views) != 0 {
		t.Errorf("the list wrote audit rows: %v", h.store.views)
	}
}

// NFR-14: every read leaves an audit row, and without one nothing is read.
func TestReportDetailAudit(t *testing.T) {
	h := newHarness()
	id, _, _ := chat(t, h)
	r, err := h.svc.Report(t.Context(), "ada", id, domain.ReportInput{Reason: domain.ReasonSpam})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		d, err := h.svc.ReportDetail(t.Context(), "admin", r.ID, "")
		if err != nil || len(d.Messages) != 2 || d.Report.ID != r.ID {
			t.Fatalf("%+v, %v", d, err)
		}
	}
	if len(h.store.views) != 2 || h.store.views[1] != [2]string{r.ID, "admin"} {
		t.Errorf("views = %v", h.store.views)
	}
	h.store.failView = errors.New("audit down")
	if d, err := h.svc.ReportDetail(t.Context(), "admin", r.ID, ""); !errors.Is(err, h.store.failView) || d.Messages != nil {
		t.Errorf("no audit row, but read: %+v, %v", d, err)
	}
}

func TestReportDetailRefusals(t *testing.T) {
	h := newHarness()
	if _, err := h.svc.ReportDetail(t.Context(), "admin", "none", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("no report: %v", err)
	}
	if _, err := h.svc.ReportDetail(t.Context(), "admin", "none", "x"); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Errorf("bad cursor: %v", err)
	}
	if len(h.store.views) != 0 {
		t.Errorf("views = %v", h.store.views)
	}
}

func TestReportDetailPaging(t *testing.T) {
	h := newHarness()
	h.noLimits()
	id, _, _ := chat(t, h)
	r, err := h.svc.Report(t.Context(), "ada", id, domain.ReportInput{Reason: domain.ReasonSpam})
	if err != nil {
		t.Fatal(err)
	}
	for range ReportMessagePageSize {
		h.direct(t, "ada", "bob", "more")
	}
	d, err := h.svc.ReportDetail(t.Context(), "admin", r.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Messages) != ReportMessagePageSize || d.NextBefore == nil {
		t.Fatalf("%d messages, next %v", len(d.Messages), d.NextBefore)
	}
	d, err = h.svc.ReportDetail(t.Context(), "admin", r.ID, *d.NextBefore)
	if err != nil || len(d.Messages) != 2 || d.NextBefore != nil {
		t.Errorf("%d messages, next %v, %v", len(d.Messages), d.NextBefore, err)
	}
}

func TestResolveReport(t *testing.T) {
	h := newHarness()
	id, _, _ := chat(t, h)
	r, err := h.svc.Report(t.Context(), "ada", id, domain.ReportInput{Reason: domain.ReasonSpam})
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("x", MaxReportText+1)
	var f *domain.InvalidFieldError
	if _, err := h.svc.ResolveReport(t.Context(), "admin", r.ID, "maybe", nil); !errors.As(err, &f) || f.Field != "outcome" {
		t.Errorf("bad outcome: %v", err)
	}
	if _, err := h.svc.ResolveReport(t.Context(), "admin", r.ID, domain.OutcomeDismissed, &long); !errors.As(err, &f) || f.Field != "note" {
		t.Errorf("long note: %v", err)
	}
	got, err := h.svc.ResolveReport(t.Context(), "admin", r.ID, domain.OutcomeActionTaken, ptr(" warned "))
	if err != nil || *got.Outcome != domain.OutcomeActionTaken || *got.Note != "warned" || got.ResolvedAt == nil {
		t.Fatalf("%+v, %v", got, err)
	}
	if _, err := h.svc.ResolveReport(t.Context(), "admin", r.ID, domain.OutcomeDismissed, nil); !errors.Is(err, domain.ErrAlreadyResolved) {
		t.Errorf("twice: %v", err)
	}
	if _, err := h.svc.ResolveReport(t.Context(), "admin", "none", domain.OutcomeDismissed, nil); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("no report: %v", err)
	}
}

func TestResolveForAction(t *testing.T) {
	h := newHarness()
	h.connect("ada", "bob")
	sent := h.direct(t, "ada", "bob", "hello")
	r, err := h.svc.Report(t.Context(), "bob", sent.Message.ConversationID, domain.ReportInput{Reason: domain.ReasonSpam})
	if err != nil {
		t.Fatal(err)
	}
	note := "internal"
	if err := h.svc.ResolveForAction(t.Context(), "admin", "unknown", "ada", nil); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown: error = %v", err)
	}
	if err := h.svc.ResolveForAction(t.Context(), "admin", r.ID, "bob", nil); !errors.Is(err, domain.ErrReportAboutOther) {
		t.Errorf("about someone else: error = %v", err)
	}
	if err := h.svc.ResolveForAction(t.Context(), "admin", r.ID, "ada", &note); err != nil {
		t.Fatal(err)
	}
	got, err := h.store.FindReport(t.Context(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome == nil || *got.Outcome != domain.OutcomeActionTaken || got.Note == nil || *got.Note != note {
		t.Errorf("report = %+v", got)
	}
	if err := h.svc.ResolveForAction(t.Context(), "admin", r.ID, "ada", nil); !errors.Is(err, domain.ErrAlreadyResolved) {
		t.Errorf("twice: error = %v", err)
	}
}
