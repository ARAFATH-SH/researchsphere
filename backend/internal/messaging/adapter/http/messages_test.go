package http

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

var (
	editedAt  = at.Add(5 * time.Minute)
	deletedAt = at.Add(10 * time.Minute)
	reported  = domain.ReportedMessage{
		ID: msgID, ConversationID: convID, SenderID: otherID, CreatedAt: at, Body: ptr("as reported"),
		EditedAt: &editedAt, DeletedAt: &deletedAt, ChangedAfterReport: true,
		Versions: []domain.MessageVersion{{MessageID: msgID, Body: "first", ShownFrom: at, ShownUntil: at.Add(time.Minute), EndedBy: domain.EndedByEdit}},
	}
)

// FR-16.5: an edit answers with the message, its new text and editedAt; each refusal has its code.
func TestEditMessageAnswers(t *testing.T) {
	path := "/api/v1/conversations/" + convID + "/messages/" + msgID
	edited := message
	edited.Body, edited.EditedAt = ptr("changed"), &editedAt

	svc := &fakeService{edited: edited}
	rec := send(t, svc, http.MethodPatch, path, `{"body":" changed "}`, caller)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; %s", rec.Code, rec.Body)
	}
	body := decode(t, rec)
	if body["id"] != msgID || body["body"] != "changed" || body["editedAt"] != editedAt.Format(time.RFC3339) || body["deleted"] != false {
		t.Errorf("body = %v", body)
	}
	if !equal(svc.args, []any{caller, convID, msgID, " changed "}) {
		t.Errorf("args = %v", svc.args)
	}

	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"window", domain.ErrEditWindowClosed, http.StatusConflict, CodeEditWindowClosed},
		{"body", domain.ErrInvalidBody, http.StatusBadRequest, "invalid_body"},
		{"not the sender's", domain.ErrNotFound, http.StatusNotFound, "not_found"},
		{"blocked", domain.ErrCannotMessage, http.StatusForbidden, "cannot_message"},
		{"blocker", domain.ErrYouBlocked, http.StatusConflict, "you_blocked_this_user"},
		{"pending", domain.ErrRequestPending, http.StatusConflict, "request_pending"},
		{"closed", domain.ErrClosed, http.StatusConflict, "conversation_closed"},
		{"suspended", &domain.SuspendedError{}, http.StatusForbidden, "account_suspended"},
		{"restricted", &domain.RestrictedError{Until: at}, http.StatusForbidden, "messaging_restricted"},
		{"limited", &domain.RateLimitedError{RetryAfter: time.Second}, http.StatusTooManyRequests, "rate_limited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := send(t, &fakeService{err: tc.err}, http.MethodPatch, path, `{"body":"x"}`, caller)
			if rec.Code != tc.status || decode(t, rec)["code"] != tc.code {
				t.Errorf("got %d %s, want %d %s", rec.Code, rec.Body, tc.status, tc.code)
			}
		})
	}
	if rec := send(t, &fakeService{err: errors.New("boom")}, http.MethodPatch, path, `{"body":"x"}`, caller); rec.Code != http.StatusInternalServerError {
		t.Errorf("server error: %d", rec.Code)
	}
	if rec := send(t, &fakeService{}, http.MethodPatch, path, `not json`, caller); rec.Code != http.StatusBadRequest {
		t.Errorf("not JSON: %d", rec.Code)
	}
}

func TestDeleteMessageAnswers(t *testing.T) {
	path := "/api/v1/conversations/" + convID + "/messages/" + msgID
	svc := &fakeService{}
	if rec := send(t, svc, http.MethodDelete, path, "", caller); rec.Code != http.StatusNoContent || !equal(svc.args, []any{caller, convID, msgID}) {
		t.Errorf("got %d, args %v", rec.Code, svc.args)
	}
	rec := send(t, &fakeService{err: domain.ErrNotFound}, http.MethodDelete, path, "", caller)
	if rec.Code != http.StatusNotFound || decode(t, rec)["code"] != "not_found" {
		t.Errorf("not found: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, &fakeService{err: errors.New("boom")}, http.MethodDelete, path, "", caller); rec.Code != http.StatusInternalServerError {
		t.Errorf("server error: %d", rec.Code)
	}
}

// A tombstone carries deleted: true and a null body; only deleted marks it (FR-16.5).
func TestTombstoneBody(t *testing.T) {
	tomb := message
	tomb.Body, tomb.DeletedAt = nil, &deletedAt
	b, err := MessageBody(tomb)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Deleted || b.Body != nil || b.EditedAt != nil {
		t.Errorf("tombstone = %+v", b)
	}
	live, err := MessageBody(message)
	if err != nil || live.Deleted || live.Body == nil || *live.Body != "hi" {
		t.Errorf("live = %+v (%v)", live, err)
	}
}

// The admin report detail shows the text at the report, the versions and the deletion (NFR-14).
func TestReportDetailShowsHistory(t *testing.T) {
	rec := send(t, &fakeService{}, http.MethodGet, "/api/v1/admin/message-reports/"+reportID, "", caller)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	m := decode(t, rec)["messages"].([]any)[0].(map[string]any)
	versions := m["versions"].([]any)
	v := versions[0].(map[string]any)
	if m["body"] != "as reported" || m["changedAfterReport"] != true || m["deletedAt"] != deletedAt.Format(time.RFC3339) ||
		m["editedAt"] != editedAt.Format(time.RFC3339) || len(versions) != 1 || v["body"] != "first" || v["endedBy"] != "edited" {
		t.Errorf("message = %v", m)
	}
}
