package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/notification/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

const (
	callerID = "00000000-0000-0000-0000-000000000001"
	actorID  = "00000000-0000-0000-0000-000000000002"
	notifID  = "00000000-0000-0000-0000-0000000000b1"
	notif2ID = "00000000-0000-0000-0000-0000000000b2"
	reqID    = "00000000-0000-0000-0000-0000000000a1"
	photoID  = "00000000-0000-0000-0000-0000000000f1"
)

var at = time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

// fakeService returns err from every operation and remembers its arguments.
type fakeService struct {
	err  error
	args []any
	page domain.Page
}

func (f *fakeService) List(_ context.Context, recipientID, cursor string, limit *int) (domain.Page, error) {
	f.args = []any{recipientID, cursor, limit}
	return f.page, f.err
}

func (f *fakeService) UnreadCount(_ context.Context, recipientID string) (int, error) {
	f.args = []any{recipientID}
	return 3, f.err
}

func (f *fakeService) MarkRead(_ context.Context, recipientID, id string) error {
	f.args = []any{recipientID, id}
	return f.err
}

func (f *fakeService) MarkAllRead(_ context.Context, recipientID string) (int, error) {
	f.args = []any{recipientID}
	return 2, f.err
}

type server struct {
	*Handler
	others
}

type others struct{ gen.StrictServerInterface }

func send(t *testing.T, svc *fakeService, method, path, caller string) *httptest.ResponseRecorder {
	t.Helper()
	ctx := t.Context()
	if caller != "" {
		ctx = httpx.WithUserID(ctx, caller)
	}
	req := httptest.NewRequestWithContext(ctx, method, path, http.NoBody)
	mux := http.NewServeMux()
	gen.HandlerWithOptions(gen.NewStrictHandlerWithOptions(server{Handler: NewHandler(svc)}, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.WriteHeader(http.StatusBadRequest)
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	}), gen.StdHTTPServerOptions{BaseURL: "/api/v1", BaseRouter: mux})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	return m
}

func TestListNotifications(t *testing.T) {
	read := at.Add(time.Minute)
	svc := &fakeService{page: domain.Page{NextCursor: ptr("next"), Items: []domain.Item{
		{
			Notification:   domain.Notification{ID: notifID, ActorID: actorID, Type: domain.TypeConnectionRequestReceived, SubjectID: reqID, CreatedAt: at},
			Actor:          domain.Actor{UserID: actorID, FullName: "Bob", PhotoFileID: ptr(photoID)},
			RequestPending: ptr(true), Link: "/network",
		},
		{
			Notification: domain.Notification{ID: notif2ID, ActorID: actorID, Type: domain.TypeConnectionRequestAccepted, SubjectID: reqID, CreatedAt: at, ReadAt: &read},
			Actor:        domain.Actor{UserID: actorID, FullName: "Bob"},
			Link:         "/profile/" + actorID,
		},
	}}}
	rec := send(t, svc, http.MethodGet, "/api/v1/notifications?cursor=abc&limit=5", callerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	want := map[string]any{"nextCursor": "next", "items": []any{
		map[string]any{"id": notifID, "type": "connection_request_received", "createdAt": "2026-11-02T09:30:00Z", "read": false,
			"link": "/network", "actor": map[string]any{"userId": actorID, "fullName": "Bob", "photoUrl": "/api/v1/media/" + photoID},
			"requestId": reqID, "requestPending": true},
		map[string]any{"id": notif2ID, "type": "connection_request_accepted", "createdAt": "2026-11-02T09:30:00Z", "read": true,
			"link": "/profile/" + actorID, "actor": map[string]any{"userId": actorID, "fullName": "Bob", "photoUrl": nil}},
	}}
	if got := decode(t, rec); !reflect.DeepEqual(got, want) {
		t.Errorf("body %v\nwant %v", got, want)
	}
	if svc.args[0] != callerID || svc.args[1] != "abc" || *(svc.args[2].(*int)) != 5 {
		t.Errorf("List got %v", svc.args)
	}
}

func TestListNotificationsDefaults(t *testing.T) {
	svc := &fakeService{}
	rec := send(t, svc, http.MethodGet, "/api/v1/notifications", callerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	if got := decode(t, rec); !reflect.DeepEqual(got, map[string]any{"items": []any{}, "nextCursor": nil}) {
		t.Errorf("body %v, want an empty page", got)
	}
	if svc.args[1] != "" || svc.args[2].(*int) != nil {
		t.Errorf("List got %v, want no cursor and no limit", svc.args)
	}
}

func TestListNotificationsErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{domain.ErrInvalidCursor, http.StatusBadRequest, "bad_request"},
		{domain.ErrInvalidLimit, http.StatusBadRequest, "bad_request"},
	} {
		rec := send(t, &fakeService{err: tc.err}, http.MethodGet, "/api/v1/notifications", callerID)
		if got := decode(t, rec); rec.Code != tc.status || got["code"] != tc.code || got["message"] == "" {
			t.Errorf("%v: got %d %v", tc.err, rec.Code, got)
		}
	}
	if rec := send(t, &fakeService{err: errors.New("boom")}, http.MethodGet, "/api/v1/notifications", callerID); rec.Code != http.StatusInternalServerError {
		t.Errorf("store failure: status %d, want 500", rec.Code)
	}
}

func TestUnreadCount(t *testing.T) {
	svc := &fakeService{}
	rec := send(t, svc, http.MethodGet, "/api/v1/notifications/unread-count", callerID)
	if rec.Code != http.StatusOK || !reflect.DeepEqual(decode(t, rec), map[string]any{"count": float64(3)}) {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
	if !reflect.DeepEqual(svc.args, []any{callerID}) {
		t.Errorf("UnreadCount got %v", svc.args)
	}
}

func TestMarkRead(t *testing.T) {
	svc := &fakeService{}
	rec := send(t, svc, http.MethodPost, "/api/v1/notifications/"+notifID+"/read", callerID)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status %d %s", rec.Code, rec.Body)
	}
	if !reflect.DeepEqual(svc.args, []any{callerID, notifID}) {
		t.Errorf("MarkRead got %v", svc.args)
	}
	rec = send(t, &fakeService{err: domain.ErrNotFound}, http.MethodPost, "/api/v1/notifications/"+notifID+"/read", callerID)
	if got := decode(t, rec); rec.Code != http.StatusNotFound || got["code"] != "not_found" {
		t.Errorf("not found: got %d %v", rec.Code, got)
	}
}

func TestMarkAllRead(t *testing.T) {
	svc := &fakeService{}
	rec := send(t, svc, http.MethodPost, "/api/v1/notifications/read-all", callerID)
	if rec.Code != http.StatusOK || !reflect.DeepEqual(decode(t, rec), map[string]any{"marked": float64(2)}) {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

// Without a user in the context every operation answers 401 and calls nothing.
func TestUnauthenticated(t *testing.T) {
	for _, r := range [][2]string{
		{http.MethodGet, "/api/v1/notifications"},
		{http.MethodGet, "/api/v1/notifications/unread-count"},
		{http.MethodPost, "/api/v1/notifications/" + notifID + "/read"},
		{http.MethodPost, "/api/v1/notifications/read-all"},
	} {
		svc := &fakeService{}
		rec := send(t, svc, r[0], r[1], "")
		if got := decode(t, rec); rec.Code != http.StatusUnauthorized || got["code"] != "unauthenticated" || svc.args != nil {
			t.Errorf("%s %s: got %d %v, args %v", r[0], r[1], rec.Code, got, svc.args)
		}
	}
}
