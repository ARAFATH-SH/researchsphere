package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

const (
	callerID  = "00000000-0000-0000-0000-000000000001"
	otherID   = "00000000-0000-0000-0000-000000000002"
	requestID = "00000000-0000-0000-0000-0000000000a1"
	photoID   = "00000000-0000-0000-0000-0000000000f1"
)

var (
	at    = time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC)
	other = domain.Person{UserID: otherID, FullName: "Bob", Headline: ptr("Researcher"), PhotoFileID: ptr(photoID)}
)

func ptr(s string) *string { return &s }

// fakeService returns err from every operation and remembers its arguments.
type fakeService struct {
	err  error
	args []any
}

func (f *fakeService) Send(_ context.Context, requesterID, receiverID string) (domain.PendingRequest, error) {
	f.args = []any{requesterID, receiverID}
	return domain.PendingRequest{ID: requestID, CreatedAt: at, Other: other}, f.err
}

func (f *fakeService) Accept(_ context.Context, receiverID, id string) (domain.Connection, error) {
	f.args = []any{receiverID, id}
	return domain.Connection{ConnectedAt: at, Other: other}, f.err
}

func (f *fakeService) Reject(_ context.Context, receiverID, id string) error {
	f.args = []any{receiverID, id}
	return f.err
}

func (f *fakeService) Cancel(_ context.Context, requesterID, id string) error {
	f.args = []any{requesterID, id}
	return f.err
}

func (f *fakeService) Remove(_ context.Context, userID, otherID string) error {
	f.args = []any{userID, otherID}
	return f.err
}

func (f *fakeService) ListConnections(_ context.Context, userID string, page int) (domain.ConnectionPage, error) {
	f.args = []any{userID, page}
	return domain.ConnectionPage{Items: []domain.Connection{{ConnectedAt: at, Other: other}}, Page: page, PageSize: 20, Total: 21}, f.err
}

func (f *fakeService) ListUserConnections(_ context.Context, userID string, page int) (domain.ConnectionPage, error) {
	f.args = []any{userID, page}
	return domain.ConnectionPage{Page: page, PageSize: 20}, f.err
}

func (f *fakeService) ListReceived(_ context.Context, userID string, page int) (domain.RequestPage, error) {
	f.args = []any{userID, page}
	return domain.RequestPage{Items: []domain.PendingRequest{{ID: requestID, CreatedAt: at, Other: other}}, Page: page, PageSize: 20, Total: 1}, f.err
}

func (f *fakeService) ListSent(_ context.Context, userID string, page int) (domain.RequestPage, error) {
	f.args = []any{userID, page}
	return domain.RequestPage{Page: page, PageSize: 20}, f.err
}

type server struct {
	*Handler
	others
}

type others struct{ gen.StrictServerInterface }

func send(t *testing.T, svc *fakeService, method, path, body, caller string) *httptest.ResponseRecorder {
	t.Helper()
	ctx := t.Context()
	if caller != "" {
		ctx = httpx.WithUserID(ctx, caller)
	}
	var r io.Reader = http.NoBody
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequestWithContext(ctx, method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
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

func TestSendConnectionRequest(t *testing.T) {
	svc := &fakeService{}
	rec := send(t, svc, http.MethodPost, "/api/v1/connections/requests", `{"userId":"`+otherID+`"}`, callerID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	want := map[string]any{"requestId": requestID, "createdAt": "2026-11-01T09:30:00Z", "userId": otherID,
		"fullName": "Bob", "headline": "Researcher", "institution": nil, "photoUrl": "/api/v1/media/" + photoID}
	if got := decode(t, rec); !reflect.DeepEqual(got, want) {
		t.Errorf("body %v\nwant %v", got, want)
	}
	if !reflect.DeepEqual(svc.args, []any{callerID, otherID}) {
		t.Errorf("Send got %v", svc.args)
	}
}

func TestSendConnectionRequestErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		status   int
		code     string
		extraKey string
	}{
		{"self", domain.ErrSelf, http.StatusBadRequest, "cannot_connect_to_self", ""},
		{"unknown", domain.ErrNotFound, http.StatusNotFound, "not_found", ""},
		{"own pending", domain.ErrAlreadyPending, http.StatusConflict, "request_already_pending", ""},
		{"connected", domain.ErrAlreadyConnected, http.StatusConflict, "already_connected", ""},
		{"theirs pending", &domain.PendingFromThemError{RequestID: requestID}, http.StatusConflict, "request_pending_from_them", "requestId"},
		{"rate limited", &domain.RateLimitedError{RetryAfter: 90*time.Minute + 300*time.Millisecond}, http.StatusTooManyRequests, "rate_limited", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := send(t, &fakeService{err: tt.err}, http.MethodPost, "/api/v1/connections/requests", `{"userId":"`+otherID+`"}`, callerID)
			got := decode(t, rec)
			if rec.Code != tt.status || got["code"] != tt.code || got["message"] == "" {
				t.Fatalf("got %d %v, want %d %s", rec.Code, got, tt.status, tt.code)
			}
			if tt.extraKey != "" && got[tt.extraKey] != requestID {
				t.Errorf("%s = %v, want %s", tt.extraKey, got[tt.extraKey], requestID)
			}
			if tt.extraKey == "" {
				if _, ok := got["requestId"]; ok {
					t.Error("requestId sent without request_pending_from_them")
				}
			}
			if tt.status == http.StatusTooManyRequests && rec.Header().Get("Retry-After") != "5401" {
				t.Errorf("Retry-After %q, want 5401 (rounded up)", rec.Header().Get("Retry-After"))
			}
		})
	}
}

func TestSendConnectionRequestBadBody(t *testing.T) {
	for _, body := range []string{`{`, `{"userId":"x","extra":1}`} {
		rec := send(t, &fakeService{}, http.MethodPost, "/api/v1/connections/requests", body, callerID)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: status %d, want 400", body, rec.Code)
		}
	}
}

func TestAnswerOperations(t *testing.T) {
	base := "/api/v1/connections/requests/" + requestID
	tests := []struct {
		name, method, path string
		okStatus           int
	}{
		{"accept", http.MethodPost, base + "/accept", http.StatusOK},
		{"reject", http.MethodPost, base + "/reject", http.StatusNoContent},
		{"cancel", http.MethodDelete, base, http.StatusNoContent},
		{"remove", http.MethodDelete, "/api/v1/connections/" + otherID, http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{}
			rec := send(t, svc, tt.method, tt.path, "", callerID)
			if rec.Code != tt.okStatus {
				t.Fatalf("status %d %s, want %d", rec.Code, rec.Body, tt.okStatus)
			}
			wantArg := requestID
			if tt.name == "remove" {
				wantArg = otherID
			}
			if !reflect.DeepEqual(svc.args, []any{callerID, wantArg}) {
				t.Errorf("service got %v", svc.args)
			}
			if tt.name == "accept" {
				want := map[string]any{"connectedAt": "2026-11-01T09:30:00Z", "userId": otherID, "fullName": "Bob",
					"headline": "Researcher", "institution": nil, "photoUrl": "/api/v1/media/" + photoID}
				if got := decode(t, rec); !reflect.DeepEqual(got, want) {
					t.Errorf("body %v\nwant %v", got, want)
				}
			}

			rec = send(t, &fakeService{err: domain.ErrNotFound}, tt.method, tt.path, "", callerID)
			if got := decode(t, rec); rec.Code != http.StatusNotFound || got["code"] != "not_found" {
				t.Errorf("not found: %d %v", rec.Code, got)
			}
			rec = send(t, &fakeService{err: errors.New("boom")}, tt.method, tt.path, "", callerID)
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("store error: %d, want 500", rec.Code)
			}
			if rec := send(t, &fakeService{}, tt.method, tt.path, "", ""); rec.Code != http.StatusUnauthorized {
				t.Errorf("no caller: %d, want 401", rec.Code)
			}
		})
	}
}

func TestLists(t *testing.T) {
	tests := []struct {
		path    string
		wantArg string
	}{
		{"/api/v1/connections", callerID},
		{"/api/v1/connections/requests/received", callerID},
		{"/api/v1/connections/requests/sent", callerID},
		{"/api/v1/profiles/" + otherID + "/connections", otherID},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			svc := &fakeService{}
			rec := send(t, svc, http.MethodGet, tt.path, "", callerID)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d %s", rec.Code, rec.Body)
			}
			if !reflect.DeepEqual(svc.args, []any{tt.wantArg, 1}) {
				t.Errorf("service got %v, want %s and page 1", svc.args, tt.wantArg)
			}
			got := decode(t, rec)
			if items, ok := got["items"].([]any); !ok || got["pageSize"] != 20.0 || got["page"] != 1.0 {
				t.Errorf("body %v, want items (never null), page 1 and pageSize 20", got)
			} else if len(items) > 0 && items[0].(map[string]any)["userId"] != otherID {
				t.Errorf("first item %v", items[0])
			}

			rec = send(t, svc, http.MethodGet, tt.path+"?page=3", "", callerID)
			if rec.Code != http.StatusOK || svc.args[1] != 3 {
				t.Errorf("page=3: %d, service got %v", rec.Code, svc.args)
			}
			rec = send(t, &fakeService{err: domain.ErrInvalidPage}, http.MethodGet, tt.path+"?page=0", "", callerID)
			if got := decode(t, rec); rec.Code != http.StatusBadRequest || got["code"] != "bad_request" {
				t.Errorf("page=0: %d %v", rec.Code, got)
			}
			if rec := send(t, svc, http.MethodGet, tt.path+"?page=x", "", callerID); rec.Code != http.StatusBadRequest {
				t.Errorf("page=x: %d, want 400", rec.Code)
			}
			if rec := send(t, svc, http.MethodGet, tt.path, "", ""); rec.Code != http.StatusUnauthorized {
				t.Errorf("no caller: %d, want 401", rec.Code)
			}
		})
	}
	rec := send(t, &fakeService{err: domain.ErrNotFound}, http.MethodGet, "/api/v1/profiles/"+otherID+"/connections", "", callerID)
	if got := decode(t, rec); rec.Code != http.StatusNotFound || got["code"] != "not_found" {
		t.Errorf("hidden profile: %d %v", rec.Code, got)
	}
}
