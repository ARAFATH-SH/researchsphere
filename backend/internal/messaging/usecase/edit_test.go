package usecase

import (
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// text is a message's body, or "" when it has none.
func text(m domain.Message) string {
	if m.Body == nil {
		return ""
	}
	return *m.Body
}

// FR-16.5: the sender edits their message; members see the latest text and editedAt, the
// earlier text is kept for moderation, and the edit counts as a message (FR-16.10).
func TestEditMessage(t *testing.T) {
	h := newHarness()
	id, fromAda, _ := chat(t, h)

	m, err := h.svc.Edit(t.Context(), "ada", id, fromAda, "  hello again \n")
	if err != nil {
		t.Fatal(err)
	}
	if text(m) != "hello again" || m.EditedAt == nil || !m.EditedAt.Equal(h.store.clock()) || m.Deleted() {
		t.Errorf("edited = %+v", m)
	}
	page := h.mustMessages(t, "bob", id)
	if got := page.Items[1]; got.ID != fromAda || text(got) != "hello again" || got.EditedAt == nil {
		t.Errorf("bob sees %+v", got)
	}
	want := []domain.MessageVersion{{MessageID: fromAda, Body: "hello", ShownFrom: page.Items[1].CreatedAt, ShownUntil: h.store.clock(), EndedBy: domain.EndedByEdit}}
	if !slices.Equal(h.store.versions, want) {
		t.Errorf("versions = %+v, want %+v", h.store.versions, want)
	}
	if !slices.Contains(h.pub.events, "edited "+fromAda+" to [bob]") {
		t.Errorf("events = %v", h.pub.events)
	}
	// One send and one edit each.
	if h.limiter.used["messages:minute|ada"] != 2 || h.limiter.used["messages:day|ada"] != 2 {
		t.Errorf("limits = %v", h.limiter.used)
	}

	// A second edit keeps the first edit's text, shown from that edit.
	first := *m.EditedAt
	h.store.clock = func() time.Time { return first.Add(time.Minute) }
	if _, err := h.svc.Edit(t.Context(), "ada", id, fromAda, "third"); err != nil {
		t.Fatal(err)
	}
	if v := h.store.versions[1]; v.Body != "hello again" || !v.ShownFrom.Equal(first) || v.EndedBy != domain.EndedByEdit {
		t.Errorf("second version = %+v", v)
	}
}

// Every refusal of an edit stores nothing, publishes nothing and uses no quota.
func TestEditRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(h *harness, id, fromAda, fromBob string)
		editor string
		msg    func(fromAda, fromBob string) string
		body   string
		want   func(error) bool
	}{
		{name: "a stranger", editor: "carl", want: is(domain.ErrNotFound)},
		{name: "the other member", editor: "bob", want: is(domain.ErrNotFound)},
		{name: "the other member's message", msg: func(_, fromBob string) string { return fromBob }, want: is(domain.ErrNotFound)},
		{name: "an unknown message", msg: func(string, string) string { return "00000000-0000-7000-8000-999999999999" }, want: is(domain.ErrNotFound)},
		{name: "an empty body", body: " \n ", want: is(domain.ErrInvalidBody)},
		{name: "a body of 5001 characters", body: longText(5001), want: is(domain.ErrInvalidBody)},
		{name: "a deleted message", setup: func(h *harness, id, fromAda, _ string) {
			if err := h.svc.Delete(t.Context(), "ada", id, fromAda); err != nil {
				t.Fatal(err)
			}
		}, want: is(domain.ErrNotFound)},
		{name: "after the window", setup: func(h *harness, _, _, _ string) {
			h.now = time.Date(2026, 11, 4, 10, 15, 1, 1, time.UTC) // sent at 10:00:01
		}, want: is(domain.ErrEditWindowClosed)},
		{name: "a suspended sender", setup: func(h *harness, _, _, _ string) {
			h.actors["ada"] = domain.Standing{Suspended: true}
		}, want: as[*domain.SuspendedError]},
		{name: "a restricted sender", setup: func(h *harness, _, _, _ string) {
			until := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
			h.actors["ada"] = domain.Standing{RestrictedUntil: &until}
		}, want: as[*domain.RestrictedError]},
		{name: "the minute limit", setup: func(h *harness, _, _, _ string) {
			h.limiter.used["messages:minute|ada"] = testLimits.MessagesPerMinute
		}, want: as[*domain.RateLimitedError]},
		{name: "the day limit", setup: func(h *harness, _, _, _ string) {
			h.limiter.used["messages:day|ada"] = testLimits.MessagesPerDay
		}, want: as[*domain.RateLimitedError]},
		{name: "the other member suspended", setup: func(h *harness, _, _, _ string) {
			h.store.hidden["bob"] = true
		}, want: is(domain.ErrNotFound)},
		{name: "blocked by the editor", setup: func(h *harness, _, _, _ string) {
			h.blocks.set["ada|bob"] = true
		}, want: is(domain.ErrYouBlocked)},
		{name: "blocked by the other member", setup: func(h *harness, _, _, _ string) {
			h.blocks.set["bob|ada"] = true
		}, want: is(domain.ErrCannotMessage)},
		{name: "a block that commits while the edit waits", setup: func(h *harness, _, _, _ string) {
			h.blocks.onLock = func() { h.blocks.set["bob|ada"] = true }
		}, want: is(domain.ErrCannotMessage)},
		{name: "a declined conversation", setup: func(h *harness, id, _, _ string) {
			h.store.conversations[id].state = domain.StateDeclined
		}, want: is(domain.ErrClosed)},
		{name: "a pending request", setup: func(h *harness, id, _, _ string) {
			ada := "ada"
			h.store.conversations[id].state, h.store.conversations[id].requestedBy = domain.StateRequest, &ada
		}, want: is(domain.ErrRequestPending)},
		{name: "a deletion that commits while the edit waits", setup: func(h *harness, _, fromAda, _ string) {
			h.store.onLockMessage = func() {
				if _, err := h.store.DeleteMessage(t.Context(), fromAda); err != nil {
					t.Fatal(err)
				}
			}
		}, want: is(domain.ErrNotFound)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()
			id, fromAda, fromBob := chat(t, h)
			if tc.setup != nil {
				tc.setup(h, id, fromAda, fromBob)
			}
			editor, msg, body := "ada", fromAda, "changed"
			if tc.editor != "" {
				editor = tc.editor
			}
			if tc.msg != nil {
				msg = tc.msg(fromAda, fromBob)
			}
			if tc.body != "" {
				body = tc.body
			}
			versions, events, quota := len(h.store.versions), len(h.pub.events), maps.Clone(h.limiter.used)
			_, err := h.svc.Edit(t.Context(), editor, id, msg, body)
			if !tc.want(err) {
				t.Fatalf("err = %v", err)
			}
			if slices.Contains(h.store.calls, "edit") || len(h.store.versions) != versions || len(h.pub.events) != events {
				t.Errorf("calls %v, versions %v, events %v", h.store.calls, h.store.versions, h.pub.events)
			}
			if !maps.Equal(h.limiter.used, quota) {
				t.Errorf("a refused edit used quota: %v -> %v", quota, h.limiter.used)
			}
			if got := text(h.store.messages[0]); got != "hello" && !h.store.messages[0].Deleted() {
				t.Errorf("text = %q", got)
			}
		})
	}
}

// The window is inclusive: an edit at exactly 15 minutes is accepted (MESSAGE_EDIT_WINDOW).
func TestEditAtTheEndOfTheWindow(t *testing.T) {
	h := newHarness()
	id, fromAda, _ := chat(t, h)
	h.now = time.Date(2026, 11, 4, 10, 15, 1, 0, time.UTC) // sent at 10:00:01
	if _, err := h.svc.Edit(t.Context(), "ada", id, fromAda, "in time"); err != nil {
		t.Error(err)
	}
}

// An unchanged body, after trimming, is a no-op: nothing stored or published, no quota used,
// and the message comes back as it is.
func TestEditUnchangedBody(t *testing.T) {
	h := newHarness()
	id, fromAda, _ := chat(t, h)
	events := len(h.pub.events)
	m, err := h.svc.Edit(t.Context(), "ada", id, fromAda, " hello\n")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != fromAda || text(m) != "hello" || m.EditedAt != nil {
		t.Errorf("message = %+v", m)
	}
	if slices.Contains(h.store.calls, "edit") || len(h.store.versions) != 0 || len(h.pub.events) != events || h.limiter.used["messages:minute|ada"] != 1 {
		t.Errorf("calls %v, versions %v, events %v, limits %v", h.store.calls, h.store.versions, h.pub.events, h.limiter.used)
	}
}

// FR-16.5: the sender deletes their message at any time; it stays as a tombstone, its text is
// kept for moderation, and it no longer counts as unread.
func TestDeleteMessage(t *testing.T) {
	h := newHarness()
	id, _, _ := chat(t, h)
	h.direct(t, "ada", "bob", "last")
	before, err := h.svc.UnreadCounts(t.Context(), "bob")
	if err != nil {
		t.Fatal(err)
	}
	last := h.mustMessages(t, "ada", id).Items[0]
	if err := h.svc.Delete(t.Context(), "ada", id, last.ID); err != nil {
		t.Fatal(err)
	}
	page := h.mustMessages(t, "bob", id)
	if got := page.Items[0]; got.ID != last.ID || got.Body != nil || !got.Deleted() {
		t.Errorf("tombstone = %+v", got)
	}
	if v := h.store.versions; len(v) != 1 || v[0].Body != "last" || v[0].EndedBy != domain.EndedByDeletion {
		t.Errorf("versions = %+v", v)
	}
	if !slices.Contains(h.pub.events, "deleted "+last.ID+" to [bob]") {
		t.Errorf("events = %v", h.pub.events)
	}
	after, err := h.svc.UnreadCounts(t.Context(), "bob")
	if err != nil || after.Total != before.Total-1 {
		t.Errorf("unread %d -> %d (%v)", before.Total, after.Total, err)
	}
	it, err := h.svc.Get(t.Context(), "bob", id)
	if err != nil || it.LastMessage == nil || !it.LastMessage.Deleted || it.LastMessage.Body != nil {
		t.Errorf("preview = %+v (%v)", it.LastMessage, err)
	}
	if h.limiter.used["messages:minute|ada"] != 2 {
		t.Errorf("a deletion used quota: %v", h.limiter.used)
	}

	// Deleting again changes nothing and tells nobody.
	events := len(h.pub.events)
	if err := h.svc.Delete(t.Context(), "ada", id, last.ID); err != nil {
		t.Error(err)
	}
	if len(h.store.versions) != 1 || len(h.pub.events) != events {
		t.Errorf("second delete: versions %v, events %v", h.store.versions, h.pub.events)
	}
}

// Only the sender may delete; anyone else learns nothing (NFR-14).
func TestDeleteRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, who string
		msg       func(fromAda, fromBob string) string
	}{
		{name: "a stranger", who: "carl"},
		{name: "the other member", who: "bob"},
		{name: "the other member's message", who: "ada", msg: func(_, fromBob string) string { return fromBob }},
		{name: "an unknown message", who: "ada", msg: func(string, string) string { return "00000000-0000-7000-8000-999999999999" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()
			id, fromAda, fromBob := chat(t, h)
			msg := fromAda
			if tc.msg != nil {
				msg = tc.msg(fromAda, fromBob)
			}
			events := len(h.pub.events)
			if err := h.svc.Delete(t.Context(), tc.who, id, msg); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("err = %v", err)
			}
			if len(h.store.versions) != 0 || len(h.pub.events) != events || h.store.messages[0].Deleted() || h.store.messages[1].Deleted() {
				t.Errorf("versions %v, events %v", h.store.versions, h.pub.events)
			}
		})
	}
}

// Deleting is allowed when sending is not: after a block, a decline, a suspension or a
// restriction, at the rate limit, after the edit window, and with the other member suspended.
func TestDeleteAllowedWhenSendingIsNot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(h *harness, id string)
	}{
		{"blocked by the sender", func(h *harness, _ string) { h.blocks.set["ada|bob"] = true }},
		{"blocked by the other member", func(h *harness, _ string) { h.blocks.set["bob|ada"] = true }},
		{"a declined conversation", func(h *harness, id string) { h.store.conversations[id].state = domain.StateDeclined }},
		{"a suspended sender", func(h *harness, _ string) { h.actors["ada"] = domain.Standing{Suspended: true} }},
		{"a restricted sender", func(h *harness, _ string) {
			until := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
			h.actors["ada"] = domain.Standing{RestrictedUntil: &until}
		}},
		{"at the rate limit", func(h *harness, _ string) { h.limiter.used["messages:minute|ada"] = testLimits.MessagesPerMinute }},
		{"after the edit window", func(h *harness, _ string) { h.now = h.now.Add(24 * time.Hour) }},
		{"the other member suspended", func(h *harness, _ string) { h.store.hidden["bob"] = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()
			id, fromAda, _ := chat(t, h)
			tc.setup(h, id)
			if err := h.svc.Delete(t.Context(), "ada", id, fromAda); err != nil {
				t.Fatal(err)
			}
			if !h.store.messages[0].Deleted() {
				t.Error("not deleted")
			}
		})
	}
}

// A retried clientId of a deleted message returns the tombstone, not its text.
func TestReplayOfDeletedMessage(t *testing.T) {
	h := newHarness()
	h.connect("ada", "bob")
	sent, err := h.svc.SendDirect(t.Context(), "ada", "bob", "oops", "c1")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Delete(t.Context(), "ada", sent.Message.ConversationID, sent.Message.ID); err != nil {
		t.Fatal(err)
	}
	again, err := h.svc.SendDirect(t.Context(), "ada", "bob", "oops", "c1")
	if err != nil || !again.Replayed || !again.Message.Deleted() || again.Message.Body != nil {
		t.Errorf("replay = %+v (%v)", again, err)
	}
}

// The report detail carries each message's kept versions (NFR-14).
func TestReportDetailCarriesVersions(t *testing.T) {
	h := newHarness()
	id, _, fromBob := chat(t, h)
	r, err := h.svc.Report(t.Context(), "ada", id, domain.ReportInput{Reason: domain.ReasonHarassment, MessageID: &fromBob})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Edit(t.Context(), "bob", id, fromBob, "nicer"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Delete(t.Context(), "bob", id, fromBob); err != nil {
		t.Fatal(err)
	}
	d, err := h.svc.ReportDetail(t.Context(), "admin", r.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var got domain.ReportedMessage
	for _, m := range d.Messages {
		if m.ID == fromBob {
			got = m
		}
	}
	if len(got.Versions) != 2 || got.Versions[0].Body != "unwanted" || got.Versions[0].EndedBy != domain.EndedByEdit ||
		got.Versions[1].Body != "nicer" || got.Versions[1].EndedBy != domain.EndedByDeletion {
		t.Errorf("versions = %+v", got.Versions)
	}
	for _, m := range d.Messages {
		if m.ID != fromBob && len(m.Versions) != 0 {
			t.Errorf("message %s got %+v", m.ID, m.Versions)
		}
	}
}

func is(target error) func(error) bool {
	return func(err error) bool { return errors.Is(err, target) }
}

func as[T error](err error) bool {
	var target T
	return errors.As(err, &target)
}

func longText(n int) string {
	r := make([]rune, n)
	for i := range r {
		r[i] = 'é'
	}
	return string(r)
}
