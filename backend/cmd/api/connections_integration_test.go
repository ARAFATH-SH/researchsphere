//go:build integration

// End-to-end tests of connections (FR-7) through the real router, the PostgreSQL adapters
// and the Redis limiter, with the profile's connection status and count. They reuse the
// environment of media_integration_test.go.
package main

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// call sends a JSON request as userID and returns the status and the decoded body, nil
// when there is none.
func (e mediaEnv) call(t *testing.T, userID, method, path, body string) (int, map[string]any) {
	t.Helper()
	var r io.Reader
	ct := ""
	if body != "" {
		r, ct = strings.NewReader(body), "application/json"
	}
	rec := e.send(t, userID, method, path, r, ct)
	if rec.Body.Len() == 0 {
		return rec.Code, nil
	}
	return rec.Code, decodeBody(t, rec.Body.Bytes())
}

func (e mediaEnv) sendRequest(t *testing.T, from, to string) (int, map[string]any) {
	t.Helper()
	return e.call(t, from, http.MethodPost, "/api/v1/connections/requests", `{"userId":"`+to+`"}`)
}

// profileConnection returns the connection fields of owner's profile as viewer sees it.
func (e mediaEnv) profileConnection(t *testing.T, viewer, owner string) (status string, requestID any, count float64) {
	t.Helper()
	path := "/api/v1/profiles/" + owner
	if viewer == owner {
		path = "/api/v1/profiles/me"
	}
	code, p := e.call(t, viewer, http.MethodGet, path, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s: %d %v", path, code, p)
	}
	return p["connectionStatus"].(string), p["connectionRequestId"], p["connectionCount"].(float64)
}

func (e mediaEnv) wantProfile(t *testing.T, viewer, owner, status string, requestID any, count float64) {
	t.Helper()
	gotStatus, gotID, gotCount := e.profileConnection(t, viewer, owner)
	if gotStatus != status || gotID != requestID || gotCount != count {
		t.Errorf("%s viewing %s: %s, %v, %v; want %s, %v, %v", viewer[:8], owner[:8], gotStatus, gotID, gotCount, status, requestID, count)
	}
}

func (e mediaEnv) listTotal(t *testing.T, userID, path string) (float64, []any) {
	t.Helper()
	code, body := e.call(t, userID, http.MethodGet, path, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s: %d %v", path, code, body)
	}
	return body["total"].(float64), body["items"].([]any)
}

// FR-7.1, FR-7.3, FR-7.4, FR-7.5, with the profile's status and count at each step.
func TestConnectionsSendAcceptListRemove(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.wantProfile(t, ada, bob, "none", nil, 0)

	code, req := e.sendRequest(t, ada, bob)
	if code != http.StatusCreated || req["userId"] != bob || req["fullName"] != "Ada" {
		t.Fatalf("send: %d %v", code, req)
	}
	id := req["requestId"].(string)
	e.wantProfile(t, ada, bob, "pending_sent", id, 0)
	e.wantProfile(t, bob, ada, "pending_received", id, 0)
	e.wantProfile(t, ada, ada, "self", nil, 0)

	if total, items := e.listTotal(t, ada, "/api/v1/connections/requests/sent"); total != 1 || items[0].(map[string]any)["userId"] != bob {
		t.Errorf("ada's sent list: %v %v", total, items)
	}
	if total, items := e.listTotal(t, bob, "/api/v1/connections/requests/received"); total != 1 || items[0].(map[string]any)["requestId"] != id {
		t.Errorf("bob's received list: %v %v", total, items)
	}

	// Only the receiver may accept; the sender gets the same 404 as for an unknown ID.
	if code, body := e.call(t, ada, http.MethodPost, "/api/v1/connections/requests/"+id+"/accept", ""); code != http.StatusNotFound || body["code"] != "not_found" {
		t.Errorf("accept by the sender: %d %v", code, body)
	}
	code, conn := e.call(t, bob, http.MethodPost, "/api/v1/connections/requests/"+id+"/accept", "")
	if code != http.StatusOK || conn["userId"] != ada || conn["connectedAt"] == nil {
		t.Fatalf("accept: %d %v", code, conn)
	}
	e.wantProfile(t, ada, bob, "connected", nil, 1)
	e.wantProfile(t, bob, ada, "connected", nil, 1)
	e.wantProfile(t, ada, ada, "self", nil, 1)

	if total, items := e.listTotal(t, ada, "/api/v1/connections"); total != 1 || items[0].(map[string]any)["userId"] != bob {
		t.Errorf("ada's connections: %v %v", total, items)
	}
	// A third user sees bob's connections on his profile.
	cy := e.newProfileUser(t)
	if total, items := e.listTotal(t, cy, "/api/v1/profiles/"+bob+"/connections"); total != 1 || items[0].(map[string]any)["userId"] != ada {
		t.Errorf("bob's connections as cy sees them: %v %v", total, items)
	}
	if total, _ := e.listTotal(t, ada, "/api/v1/connections/requests/sent"); total != 0 {
		t.Error("the accepted request is still in the sent list")
	}
	if code, body := e.sendRequest(t, bob, ada); code != http.StatusConflict || body["code"] != "already_connected" {
		t.Errorf("send when connected: %d %v", code, body)
	}

	if code, _ := e.call(t, bob, http.MethodDelete, "/api/v1/connections/"+ada, ""); code != http.StatusNoContent {
		t.Fatalf("remove: %d", code)
	}
	if code, body := e.call(t, ada, http.MethodDelete, "/api/v1/connections/"+bob, ""); code != http.StatusNotFound || body["code"] != "not_found" {
		t.Errorf("second remove: %d %v", code, body)
	}
	e.wantProfile(t, ada, bob, "none", nil, 0)
	if code, _ := e.sendRequest(t, ada, bob); code != http.StatusCreated {
		t.Errorf("send after removal: %d", code)
	}
}

// FR-7.6: after a rejection the sender sees no trace of it and may ask again.
func TestConnectionsRejectThenSendAgain(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	_, req := e.sendRequest(t, ada, bob)
	id := req["requestId"].(string)

	if code, _ := e.call(t, ada, http.MethodPost, "/api/v1/connections/requests/"+id+"/reject", ""); code != http.StatusNotFound {
		t.Errorf("reject by the sender: %d, want 404", code)
	}
	if code, _ := e.call(t, bob, http.MethodPost, "/api/v1/connections/requests/"+id+"/reject", ""); code != http.StatusNoContent {
		t.Fatalf("reject: %d", code)
	}
	e.wantProfile(t, ada, bob, "none", nil, 0)
	e.wantProfile(t, bob, ada, "none", nil, 0)
	if total, _ := e.listTotal(t, ada, "/api/v1/connections/requests/sent"); total != 0 {
		t.Error("the rejected request is still in the sender's list")
	}
	if total, _ := e.listTotal(t, bob, "/api/v1/connections/requests/received"); total != 0 {
		t.Error("the rejected request is still in the receiver's list")
	}
	if code, _ := e.call(t, bob, http.MethodPost, "/api/v1/connections/requests/"+id+"/accept", ""); code != http.StatusNotFound {
		t.Errorf("accept after reject: %d, want 404", code)
	}
	code, again := e.sendRequest(t, ada, bob)
	if code != http.StatusCreated || again["requestId"] == id {
		t.Errorf("send again: %d %v", code, again)
	}
}

func TestConnectionsCancel(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	_, req := e.sendRequest(t, ada, bob)
	path := "/api/v1/connections/requests/" + req["requestId"].(string)

	if code, _ := e.call(t, bob, http.MethodDelete, path, ""); code != http.StatusNotFound {
		t.Errorf("cancel by the receiver: %d, want 404", code)
	}
	if code, _ := e.call(t, ada, http.MethodDelete, path, ""); code != http.StatusNoContent {
		t.Fatalf("cancel: %d", code)
	}
	if code, _ := e.call(t, ada, http.MethodDelete, path, ""); code != http.StatusNotFound {
		t.Errorf("second cancel: %d, want 404", code)
	}
	e.wantProfile(t, bob, ada, "none", nil, 0)
	if total, _ := e.listTotal(t, bob, "/api/v1/connections/requests/received"); total != 0 {
		t.Error("the cancelled request is still received")
	}
	for _, p := range []string{"/api/v1/connections/requests/not-a-uuid", "/api/v1/connections/requests/00000000-0000-0000-0000-000000000000"} {
		if code, _ := e.call(t, ada, http.MethodDelete, p, ""); code != http.StatusNotFound {
			t.Errorf("DELETE %s: %d, want 404", p, code)
		}
	}
}

// The refusals of POST /connections/requests (FR-7.2) and their codes.
func TestConnectionsSendRefusals(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	sus, unv := e.newProfileUser(t), e.newProfileUser(t)
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET suspended_at = now() WHERE id = $1", sus); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET email_verified_at = NULL WHERE id = $1", unv); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		to     string
		status int
		code   string
	}{
		{ada, http.StatusBadRequest, "cannot_connect_to_self"},
		{sus, http.StatusNotFound, "not_found"},
		{unv, http.StatusNotFound, "not_found"},
		{"00000000-0000-0000-0000-000000000000", http.StatusNotFound, "not_found"},
		{"not-a-uuid", http.StatusNotFound, "not_found"},
	} {
		if code, body := e.sendRequest(t, ada, tt.to); code != tt.status || body["code"] != tt.code {
			t.Errorf("send to %s: %d %v, want %d %s", tt.to, code, body, tt.status, tt.code)
		}
	}

	_, req := e.sendRequest(t, ada, bob)
	if code, body := e.sendRequest(t, ada, bob); code != http.StatusConflict || body["code"] != "request_already_pending" || body["requestId"] != nil {
		t.Errorf("second send: %d %v", code, body)
	}
	// Bob asks back: no second request; the answer names ada's so the UI can offer Accept.
	if code, body := e.sendRequest(t, bob, ada); code != http.StatusConflict || body["code"] != "request_pending_from_them" || body["requestId"] != req["requestId"] {
		t.Errorf("send back: %d %v, want request_pending_from_them with %v", code, body, req["requestId"])
	}
	var n int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM connections").Scan(&n); err != nil || n != 1 {
		t.Errorf("%d rows, want 1", n)
	}
}

// Two users who send each other a request at the same moment end up with one request,
// and the other gets request_pending_from_them naming it.
func TestConnectionsSimultaneousRequests(t *testing.T) {
	e := newMediaEnv(t)
	for range 10 {
		ada, bob := e.newProfileUser(t), e.newProfileUser(t)
		var (
			wg     sync.WaitGroup
			start  = make(chan struct{})
			codes  [2]int
			bodies [2]map[string]any
		)
		for i, pair := range [][2]string{{ada, bob}, {bob, ada}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				codes[i], bodies[i] = e.sendRequest(t, pair[0], pair[1])
			}()
		}
		close(start)
		wg.Wait()
		win, lose := 0, 1
		if codes[1] == http.StatusCreated {
			win, lose = 1, 0
		}
		if codes[win] != http.StatusCreated || codes[lose] != http.StatusConflict ||
			bodies[lose]["code"] != "request_pending_from_them" || bodies[lose]["requestId"] != bodies[win]["requestId"] {
			t.Fatalf("codes %v, bodies %v; want one 201 and one 409 naming it", codes, bodies)
		}
	}
}

// Suspended users are left out of every list and the count; their profile's list is 404.
func TestConnectionsHideSuspendedUsers(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, sus, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	for _, u := range []string{bob, sus} {
		_, req := e.sendRequest(t, u, ada)
		if code, _ := e.call(t, ada, http.MethodPost, "/api/v1/connections/requests/"+req["requestId"].(string)+"/accept", ""); code != http.StatusOK {
			t.Fatal("accept failed")
		}
	}
	e.sendRequest(t, cy, ada)
	e.sendRequest(t, ada, cy) // 409; cy's request stays pending
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET suspended_at = now() WHERE id = ANY($1)", []string{sus, cy}); err != nil {
		t.Fatal(err)
	}
	if total, items := e.listTotal(t, ada, "/api/v1/connections"); total != 1 || len(items) != 1 || items[0].(map[string]any)["userId"] != bob {
		t.Errorf("ada's connections: %v %v, want only bob", total, items)
	}
	if total, _ := e.listTotal(t, ada, "/api/v1/connections/requests/received"); total != 0 {
		t.Errorf("received total %v, want the suspended sender left out", total)
	}
	e.wantProfile(t, bob, ada, "connected", nil, 1)
	if code, body := e.call(t, ada, http.MethodGet, "/api/v1/profiles/"+sus+"/connections", ""); code != http.StatusNotFound || body["code"] != "not_found" {
		t.Errorf("a suspended user's connections: %d %v, want 404", code, body)
	}
	if code, body := e.call(t, ada, http.MethodGet, "/api/v1/connections?page=0", ""); code != http.StatusBadRequest || body["code"] != "bad_request" {
		t.Errorf("page=0: %d %v", code, body)
	}
}

// Each list leaves out a user while they are suspended and shows them again once they are
// restored; the total and the profile's connectionCount follow. Another user in the same
// list stays throughout.
func TestConnectionsSuspendAndRestore(t *testing.T) {
	accept := func(t *testing.T, e mediaEnv, from, to string) {
		t.Helper()
		_, req := e.sendRequest(t, from, to)
		if code, body := e.call(t, to, http.MethodPost, "/api/v1/connections/requests/"+req["requestId"].(string)+"/accept", ""); code != http.StatusOK {
			t.Fatalf("accept: %d %v", code, body)
		}
	}
	// setup returns who reads the list, its path, the user to suspend, the user who
	// stays, and the owner whose connectionCount must equal the total ("" when the list
	// has no count).
	tests := []struct {
		name  string
		setup func(t *testing.T, e mediaEnv) (viewer, path, hidden, kept, countOwner string)
	}{
		{"my connections", func(t *testing.T, e mediaEnv) (string, string, string, string, string) {
			ada, bob, sus := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
			accept(t, e, bob, ada)
			accept(t, e, ada, sus)
			return ada, "/api/v1/connections", sus, bob, ada
		}},
		{"received requests", func(t *testing.T, e mediaEnv) (string, string, string, string, string) {
			ada, bob, sus := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
			e.sendRequest(t, bob, ada)
			e.sendRequest(t, sus, ada)
			return ada, "/api/v1/connections/requests/received", sus, bob, ""
		}},
		{"sent requests", func(t *testing.T, e mediaEnv) (string, string, string, string, string) {
			ada, bob, sus := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
			e.sendRequest(t, ada, bob)
			e.sendRequest(t, ada, sus)
			return ada, "/api/v1/connections/requests/sent", sus, bob, ""
		}},
		{"a profile's connections", func(t *testing.T, e mediaEnv) (string, string, string, string, string) {
			ada, bob, sus, viewer := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
			accept(t, e, bob, ada)
			accept(t, e, sus, ada)
			return viewer, "/api/v1/profiles/" + ada + "/connections", sus, bob, ada
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newMediaEnv(t)
			viewer, path, hidden, kept, countOwner := tt.setup(t, e)
			check := func(stage string, wantHidden bool) {
				t.Helper()
				want := map[string]bool{kept: true, hidden: !wantHidden}
				total, items := e.listTotal(t, viewer, path)
				got := map[string]bool{kept: false, hidden: false}
				for _, it := range items {
					got[it.(map[string]any)["userId"].(string)] = true
				}
				if int(total) != len(items) || got[kept] != want[kept] || got[hidden] != want[hidden] {
					t.Errorf("%s: total %v, items %v; want kept %v and suspended %v", stage, total, items, want[kept], want[hidden])
				}
				if countOwner != "" {
					if _, _, count := e.profileConnection(t, viewer, countOwner); count != total {
						t.Errorf("%s: connectionCount %v, want the list total %v", stage, count, total)
					}
				}
			}
			setSuspended := func(on bool) {
				t.Helper()
				if _, err := e.pool.Exec(t.Context(), "UPDATE users SET suspended_at = CASE WHEN $2 THEN now() END WHERE id = $1", hidden, on); err != nil {
					t.Fatal(err)
				}
			}
			check("before", false)
			setSuspended(true)
			check("suspended", true)
			setSuspended(false)
			check("restored", false)
		})
	}
}

// At most 50 new requests per user per day (Redis); the 51st gets 429 with Retry-After.
func TestConnectionsRateLimit(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	key := "social:connect:" + ada
	if err := e.rdb.Set(t.Context(), key, 49, 2*time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	if code, _ := e.sendRequest(t, ada, bob); code != http.StatusCreated {
		t.Fatalf("50th request: %d", code)
	}
	// A refused request does not count, and the window is not moved.
	r := e.send(t, ada, http.MethodPost, "/api/v1/connections/requests", strings.NewReader(`{"userId":"`+cy+`"}`), "application/json")
	if r.Code != http.StatusTooManyRequests || decodeBody(t, r.Body.Bytes())["code"] != "rate_limited" {
		t.Fatalf("51st request: %d %s", r.Code, r.Body)
	}
	if ra, err := strconv.Atoi(r.Header().Get("Retry-After")); err != nil || ra < 7000 || ra > 7200 {
		t.Errorf("Retry-After %q, want the about 7200 seconds left in the window", r.Header().Get("Retry-After"))
	}
	if n, _ := e.rdb.Get(t.Context(), key).Int(); n != 50 {
		t.Errorf("counter = %d, want 50", n)
	}
	// Others are not limited.
	if code, _ := e.sendRequest(t, bob, cy); code != http.StatusCreated {
		t.Errorf("bob's request: %d", code)
	}
}
