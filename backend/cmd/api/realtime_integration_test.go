//go:build integration

// End-to-end tests of real-time delivery (FR-16.1, FR-16.3, FR-16.4, FR-9.4, FR-16.9) with
// real sockets through the real router, on the test database and Redis DB 15.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	authusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	realtimeredis "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/adapter/redis"
	realtimedomain "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/domain"
	socialdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

// testOrigin is the web client's origin in these tests, as APP_BASE_URL gives it in main.
const testOrigin = "http://localhost:3000"

// wsWait is how long a test waits for what should arrive; the 1-second rule has its own check.
const wsWait = 3 * time.Second

type wsEvent struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

// wsClient is a test's browser tab.
type wsClient struct {
	conn   *websocket.Conn
	events chan wsEvent
	closed chan websocket.StatusCode
}

func serve(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws"
}

func tokenFor(t *testing.T, userID string) string {
	t.Helper()
	token, _, err := accesstoken.New([]byte(testSecret), accessTTL).Sign(userID, "researcher", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// attempt tries to open a socket and returns the HTTP status of a refusal.
func attempt(t *testing.T, srv *httptest.Server, header http.Header) (*websocket.Conn, int, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), wsWait)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, wsURL(srv), &websocket.DialOptions{HTTPHeader: header})
	if resp == nil {
		return conn, 0, err
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	return conn, resp.StatusCode, err
}

func dialHeader(t *testing.T, userID string) http.Header {
	t.Helper()
	return http.Header{
		"Origin": {testOrigin},
		"Cookie": {httpx.AccessCookie + "=" + tokenFor(t, userID)},
	}
}

func dial(t *testing.T, srv *httptest.Server, userID string) *wsClient {
	t.Helper()
	conn, status, err := attempt(t, srv, dialHeader(t, userID))
	if err != nil {
		t.Fatalf("dial: %v (status %d)", err, status)
	}
	c := &wsClient{conn: conn, events: make(chan wsEvent, 64), closed: make(chan websocket.StatusCode, 1)}
	go func() {
		for {
			_, frame, err := conn.Read(context.Background())
			if err != nil {
				c.closed <- websocket.CloseStatus(err)
				return
			}
			var e wsEvent
			if err := json.Unmarshal(frame, &e); err == nil {
				c.events <- e
			}
		}
	}()
	t.Cleanup(func() { _ = conn.CloseNow() })
	return c
}

// expect returns the next event of the type that matches, skipping others.
func (c *wsClient) expect(t *testing.T, typ string, match ...func(map[string]any) bool) wsEvent {
	t.Helper()
	deadline := time.After(wsWait)
	for {
		select {
		case e := <-c.events:
			if e.Type != typ {
				continue
			}
			if len(match) > 0 && !match[0](e.Data) {
				continue
			}
			return e
		case <-deadline:
			t.Fatalf("no %s event arrived", typ)
		}
	}
}

// silent fails if an event of the type arrives within d.
func (c *wsClient) silent(t *testing.T, typ string, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case e := <-c.events:
			if e.Type == typ {
				t.Fatalf("unexpected event %+v", e)
			}
		case <-deadline:
			return
		}
	}
}

func (c *wsClient) wantClosed(t *testing.T, code websocket.StatusCode) {
	t.Helper()
	select {
	case got := <-c.closed:
		if got != code {
			t.Fatalf("closed with %d, want %d", got, code)
		}
	case <-time.After(wsWait):
		t.Fatalf("still open, want close code %d", code)
	}
}

func (c *wsClient) send(t *testing.T, frame string) {
	t.Helper()
	if err := c.conn.Write(t.Context(), websocket.MessageText, []byte(frame)); err != nil {
		t.Fatal(err)
	}
}

func (c *wsClient) typing(t *testing.T, kind, conversationID string) {
	t.Helper()
	c.send(t, fmt.Sprintf(`{"type":%q,"data":{"conversationId":%q}}`, kind, conversationID))
}

func isOnline(userID string, online bool) func(map[string]any) bool {
	return func(d map[string]any) bool { return d["userId"] == userID && d["online"] == online }
}

func (e mediaEnv) presence(t *testing.T, viewer string, ids ...string) map[string]bool {
	t.Helper()
	code, body := e.call(t, viewer, http.MethodGet, "/api/v1/presence?userIds="+strings.Join(ids, ","), "")
	if code != http.StatusOK {
		t.Fatalf("presence: %d %v", code, body)
	}
	out := map[string]bool{}
	for _, it := range body["items"].([]any) {
		m := it.(map[string]any)
		out[m["userId"].(string)] = m["online"].(bool)
	}
	return out
}

func waitForOnline(t *testing.T, e mediaEnv, viewer, userID string, want bool) {
	t.Helper()
	deadline := time.Now().Add(wsWait)
	for time.Now().Before(deadline) {
		if e.presence(t, viewer, userID)[userID] == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s online is not %v", userID, want)
}

// FR-16.1: a message sent over REST reaches the other member within 1 second, and the
// sender's other tab with the clientId that matches it to the send.
func TestMessageReachesTheOtherMemberWithinOneSecond(t *testing.T) {
	e := newMediaEnv(t)
	srv := serve(t, e.router)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	bobTab := dial(t, srv, bob)
	adaTab := dial(t, srv, ada)

	client := newClientID()
	start := time.Now()
	code, sent := e.direct(t, ada, bob, "hello", client)
	if code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, sent)
	}
	got := bobTab.expect(t, realtimedomain.EventMessageCreated)
	if d := time.Since(start); d >= time.Second {
		t.Errorf("message took %s, want under 1 s (FR-16.1)", d)
	}
	t.Logf("delivered in %s", time.Since(start))

	m := got.Data["message"].(map[string]any)
	if m["body"] != "hello" || m["senderId"] != ada || got.Data["conversationState"] != "active" {
		t.Errorf("event = %+v", got)
	}
	if _, leaked := got.Data["clientId"]; leaked {
		t.Error("the other member got the sender's clientId")
	}
	if own := adaTab.expect(t, realtimedomain.EventMessageCreated); own.Data["clientId"] != client {
		t.Errorf("sender's tab = %+v, want clientId %s", own, client)
	}
	if u := bobTab.expect(t, realtimedomain.EventUnreadChanged); u.Data["total"] != 1.0 || u.Data["requests"] != 0.0 {
		t.Errorf("unread = %+v", u)
	}
}

func TestReplayedClientIDPublishesNothing(t *testing.T) {
	e := newMediaEnv(t)
	srv := serve(t, e.router)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	client := newClientID()
	if code, body := e.direct(t, ada, bob, "hello", client); code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, body)
	}
	bobTab := dial(t, srv, bob)
	if code, body := e.direct(t, ada, bob, "hello", client); code != http.StatusOK {
		t.Fatalf("retry: %d %v", code, body)
	}
	bobTab.silent(t, realtimedomain.EventMessageCreated, 200*time.Millisecond)
}

// FR-16.3: the read marker reaches the sender for "Seen", and the reader's tabs their counts.
func TestReadMarkerReachesTheSender(t *testing.T) {
	e := newMediaEnv(t)
	srv := serve(t, e.router)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, msg := e.startChat(t, ada, bob, "hello")
	adaTab := dial(t, srv, ada)
	bobTab := dial(t, srv, bob)

	code, body := e.call(t, bob, http.MethodPost, "/api/v1/conversations/"+id+"/read", fmt.Sprintf(`{"messageId":%q}`, msg))
	if code != http.StatusNoContent {
		t.Fatalf("read: %d %v", code, body)
	}
	got := adaTab.expect(t, realtimedomain.EventConversationRead)
	if got.Data["conversationId"] != id || got.Data["messageId"] != msg {
		t.Errorf("event = %+v", got)
	}
	if u := bobTab.expect(t, realtimedomain.EventUnreadChanged); u.Data["total"] != 0.0 {
		t.Errorf("unread = %+v", u)
	}
}

func hasState(state string, canMessage bool) func(map[string]any) bool {
	return func(d map[string]any) bool { return d["state"] == state && d["canMessage"] == canMessage }
}

func TestRequestAnswersUpdateTheConversation(t *testing.T) {
	for _, tc := range []struct {
		action, state string
		canMessage    bool
	}{
		{"accept", "active", true},
		{"decline", "declined", false},
	} {
		t.Run(tc.action, func(t *testing.T) {
			e := newMediaEnv(t)
			srv := serve(t, e.router)
			ada, bob := e.newProfileUser(t), e.newProfileUser(t)
			id, _ := e.startChat(t, ada, bob, "may I?")
			adaTab, bobTab := dial(t, srv, ada), dial(t, srv, bob)

			if code, body := e.call(t, bob, http.MethodPost, "/api/v1/conversations/"+id+"/"+tc.action, ""); code != http.StatusOK {
				t.Fatalf("%s: %d %v", tc.action, code, body)
			}
			adaTab.expect(t, realtimedomain.EventConversationUpdated, hasState(tc.state, tc.canMessage))
			bobTab.expect(t, realtimedomain.EventConversationUpdated, hasState(tc.state, tc.canMessage))
			bobTab.expect(t, realtimedomain.EventUnreadChanged, func(d map[string]any) bool { return d["requests"] == 0.0 })
		})
	}
}

// Activation runs inside social's transaction, so this needs the after-commit hook.
func TestConnectingUpdatesARequest(t *testing.T) {
	e := newMediaEnv(t)
	srv := serve(t, e.router)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.startChat(t, ada, bob, "may I?")
	adaTab := dial(t, srv, ada)
	e.link(t, ada, bob)
	adaTab.expect(t, realtimedomain.EventConversationUpdated, hasState("active", true))
}

// Nobody hears about a change that rolled back.
func TestRolledBackChangePublishesNothing(t *testing.T) {
	fail := &atomic.Bool{}
	e := newMediaEnvOnPool(t, testenv.DB(t), "", "", envOptions{wrapNotifier: func(n socialdomain.Notifier) socialdomain.Notifier {
		return failingNotifier{next: n, fail: fail}
	}})
	srv := serve(t, e.router)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	bobTab := dial(t, srv, bob)

	fail.Store(true)
	if code, body := e.sendRequest(t, ada, bob); code != http.StatusInternalServerError {
		t.Fatalf("request: %d %v", code, body)
	}
	bobTab.silent(t, realtimedomain.EventNotificationCreated, 200*time.Millisecond)

	fail.Store(false)
	if code, body := e.sendRequest(t, ada, bob); code != http.StatusCreated {
		t.Fatalf("request: %d %v", code, body)
	}
	bobTab.expect(t, realtimedomain.EventNotificationCreated)
}

func TestBlockAndUnblockUpdateBothMembers(t *testing.T) {
	e := newMediaEnv(t)
	srv := serve(t, e.router)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	e.startChat(t, ada, bob, "hello")
	adaTab, bobTab := dial(t, srv, ada), dial(t, srv, bob)
	adaTab.expect(t, realtimedomain.EventPresence, isOnline(bob, true))

	e.block(t, ada, bob)
	adaTab.expect(t, realtimedomain.EventConversationUpdated, hasState("active", false))
	bobTab.expect(t, realtimedomain.EventConversationUpdated, hasState("active", false))
	// The green dot goes, on both sides, though neither went offline.
	adaTab.expect(t, realtimedomain.EventPresence, isOnline(bob, false))
	bobTab.expect(t, realtimedomain.EventPresence, isOnline(ada, false))

	e.unblock(t, ada, bob)
	bobTab.expect(t, realtimedomain.EventConversationUpdated, hasState("active", true))
}

// FR-16.4: typing reaches the other member, and nothing is sent to anyone else.
func TestTypingIsRelayed(t *testing.T) {
	e := newMediaEnv(t)
	srv := serve(t, e.router)
	ada, bob, eve := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, _ := e.startChat(t, ada, bob, "hello")
	adaTab, bobTab, eveTab := dial(t, srv, ada), dial(t, srv, bob), dial(t, srv, eve)

	adaTab.typing(t, realtimedomain.ClientTypingStart, id)
	got := bobTab.expect(t, realtimedomain.EventTyping)
	if got.Data["conversationId"] != id || got.Data["userId"] != ada || got.Data["isTyping"] != true {
		t.Errorf("event = %+v", got)
	}
	adaTab.typing(t, realtimedomain.ClientTypingStop, id)
	bobTab.expect(t, realtimedomain.EventTyping, func(d map[string]any) bool { return d["isTyping"] == false })
	eveTab.silent(t, realtimedomain.EventTyping, 100*time.Millisecond)
	adaTab.silent(t, realtimedomain.EventTyping, 10*time.Millisecond)
}

func TestTypingIsRefused(t *testing.T) {
	t.Run("blocked", func(t *testing.T) {
		e := newMediaEnv(t)
		srv := serve(t, e.router)
		ada, bob := e.newProfileUser(t), e.newProfileUser(t)
		e.link(t, ada, bob)
		id, _ := e.startChat(t, ada, bob, "hello")
		e.block(t, bob, ada)
		adaTab, bobTab := dial(t, srv, ada), dial(t, srv, bob)
		adaTab.typing(t, realtimedomain.ClientTypingStart, id)
		bobTab.silent(t, realtimedomain.EventTyping, 200*time.Millisecond)
	})

	t.Run("not a member", func(t *testing.T) {
		e := newMediaEnv(t)
		srv := serve(t, e.router)
		ada, bob, eve := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
		e.link(t, ada, bob)
		id, _ := e.startChat(t, ada, bob, "hello")
		adaTab, bobTab, eveTab := dial(t, srv, ada), dial(t, srv, bob), dial(t, srv, eve)
		eveTab.typing(t, realtimedomain.ClientTypingStart, id)
		adaTab.silent(t, realtimedomain.EventTyping, 100*time.Millisecond)
		bobTab.silent(t, realtimedomain.EventTyping, 100*time.Millisecond)
	})

	t.Run("a request is not active", func(t *testing.T) {
		e := newMediaEnv(t)
		srv := serve(t, e.router)
		ada, bob := e.newProfileUser(t), e.newProfileUser(t)
		id, _ := e.startChat(t, ada, bob, "may I?")
		adaTab, bobTab := dial(t, srv, ada), dial(t, srv, bob)
		adaTab.typing(t, realtimedomain.ClientTypingStart, id)
		bobTab.silent(t, realtimedomain.EventTyping, 200*time.Millisecond)
	})
}

// FR-16.4: presence follows the sockets, for allowed viewers only.
func TestPresenceIsVisibleOnlyToAllowedUsers(t *testing.T) {
	e := newMediaEnv(t)
	srv := serve(t, e.router)
	ada, bob, eve, requester, blocker := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	e.startChat(t, ada, bob, "hello")
	e.startChat(t, requester, ada, "may I?") // a request, not active
	e.link(t, ada, blocker)
	e.startChat(t, ada, blocker, "hello")
	e.block(t, blocker, ada)

	bobTab, eveTab, requesterTab, blockerTab := dial(t, srv, bob), dial(t, srv, eve), dial(t, srv, requester), dial(t, srv, blocker)
	adaTab := dial(t, srv, ada)

	bobTab.expect(t, realtimedomain.EventPresence, isOnline(ada, true))
	waitForOnline(t, e, bob, ada, true)
	for name, viewer := range map[string]string{"no conversation": eve, "only a request": requester, "blocked": blocker} {
		if e.presence(t, viewer, ada)[ada] {
			t.Errorf("%s: sees ada online", name)
		}
	}
	for _, c := range []*wsClient{eveTab, requesterTab, blockerTab} {
		c.silent(t, realtimedomain.EventPresence, 50*time.Millisecond)
	}

	_ = adaTab.conn.Close(websocket.StatusNormalClosure, "")
	bobTab.expect(t, realtimedomain.EventPresence, isOnline(ada, false))
	waitForOnline(t, e, bob, ada, false)
}

func TestPresenceListIsChecked(t *testing.T) {
	e := newMediaEnv(t)
	ada := e.newProfileUser(t)
	tooMany := make([]string, 51)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
	}
	for name, query := range map[string]string{
		"missing": "", "empty": "?userIds=", "not a UUID": "?userIds=nope", "over 50": "?userIds=" + strings.Join(tooMany, ","),
	} {
		code, body := e.call(t, ada, http.MethodGet, "/api/v1/presence"+query, "")
		wantError(t, name, code, body, http.StatusBadRequest, "bad_request")
		if body["field"] != "userIds" {
			t.Errorf("%s: field = %v", name, body["field"])
		}
	}
	if got := e.presence(t, ada, tooMany[:50]...); len(got) != 50 {
		t.Errorf("50 IDs answered %d", len(got))
	}
}

func TestPresenceExpiresWithoutAHeartbeat(t *testing.T) {
	e := newMediaEnv(t)
	p := realtimeredis.NewPresence(e.rdb)
	t.Cleanup(func() { _ = p.Close() })
	now := time.Now()
	if _, err := p.Add(t.Context(), "crashed", "s1", now); err != nil {
		t.Fatal(err)
	}
	online, err := p.Online(t.Context(), []string{"crashed"}, now.Add(realtimeredis.PresenceTTL-time.Second))
	if err != nil || !online["crashed"] {
		t.Errorf("within the TTL: %v %v", online, err)
	}
	online, err = p.Online(t.Context(), []string{"crashed"}, now.Add(realtimeredis.PresenceTTL+time.Second))
	if err != nil || online["crashed"] {
		t.Errorf("past the TTL: %v %v", online, err)
	}
	if ttl := e.rdb.TTL(t.Context(), "realtime:presence:crashed").Val(); ttl <= 0 || ttl > realtimeredis.PresenceTTL {
		t.Errorf("key TTL = %s", ttl)
	}
}

func TestNotificationsReachTheBell(t *testing.T) {
	e := newMediaEnv(t)
	srv := serve(t, e.router)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	bobTab := dial(t, srv, bob)

	if code, body := e.sendRequest(t, ada, bob); code != http.StatusCreated {
		t.Fatalf("request: %d %v", code, body)
	}
	got := bobTab.expect(t, realtimedomain.EventNotificationCreated)
	if got.Data["type"] != "connection_request_received" || got.Data["link"] != "/network" {
		t.Errorf("event = %+v", got)
	}
	bobTab.expect(t, realtimedomain.EventNotificationsUnread, func(d map[string]any) bool { return d["count"] == 1.0 })

	if code, body := e.call(t, bob, http.MethodPost, "/api/v1/notifications/read-all", ""); code != http.StatusOK {
		t.Fatalf("read-all: %d %v", code, body)
	}
	bobTab.expect(t, realtimedomain.EventNotificationsUnread, func(d map[string]any) bool { return d["count"] == 0.0 })
}

func TestSocketClosesOnLogout(t *testing.T) {
	e := newModEnv(t)
	srv := serve(t, e.router)
	ada := e.newProfileUser(t)
	email := e.withPassword(t, ada)
	res, err := e.auth.Login(t.Context(), authusecase.LoginInput{Email: email, Password: modPassword})
	if err != nil {
		t.Fatal(err)
	}
	tab := dial(t, srv, ada)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Cookie", httpx.RefreshCookie+"="+res.RefreshToken)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", rec.Code, rec.Body)
	}
	tab.wantClosed(t, websocket.StatusCode(realtimedomain.CloseSessionEnded))
}

func TestSocketClosesOnSuspension(t *testing.T) {
	e := newModEnv(t)
	srv := serve(t, e.router)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	e.startChat(t, ada, bob, "hello")
	adaTab, bobTab := dial(t, srv, ada), dial(t, srv, bob)

	e.suspend(t, ada, fmt.Sprintf(`{"reason":%q}`, modReason))
	adaTab.wantClosed(t, websocket.StatusCode(realtimedomain.CloseSessionEnded))
	bobTab.expect(t, realtimedomain.EventConversationUpdated, hasState("active", false))
	bobTab.expect(t, realtimedomain.EventPresence, isOnline(ada, false))

	// The access token lives out its 15 minutes, so the handshake has to check.
	conn, status, err := attempt(t, srv, dialHeader(t, ada))
	if err == nil {
		_ = conn.CloseNow()
		t.Fatal("a suspended user opened a socket")
	}
	if status != http.StatusForbidden {
		t.Fatalf("handshake while suspended: %d", status)
	}

	if code, body := e.adminDo(t, http.MethodPost, userPath(ada, "restore"), fmt.Sprintf(`{"reason":%q}`, modReason)); code != http.StatusOK {
		t.Fatalf("restore: %d %v", code, body)
	}
	bobTab.expect(t, realtimedomain.EventConversationUpdated, hasState("active", true))
	dial(t, srv, ada)
}

func TestHandshakeRefusals(t *testing.T) {
	e := newMediaEnv(t)
	srv := serve(t, e.router)
	ada := e.newProfileUser(t)
	cookie := httpx.AccessCookie + "=" + tokenFor(t, ada)

	for _, tc := range []struct {
		name   string
		header http.Header
		status int
	}{
		{"another origin", http.Header{"Origin": {"http://evil.example"}, "Cookie": {cookie}}, http.StatusForbidden},
		{"another port", http.Header{"Origin": {"http://localhost:3001"}, "Cookie": {cookie}}, http.StatusForbidden},
		{"another scheme", http.Header{"Origin": {"https://localhost:3000"}, "Cookie": {cookie}}, http.StatusForbidden},
		{"no origin", http.Header{"Cookie": {cookie}}, http.StatusForbidden},
		{"no token", http.Header{"Origin": {testOrigin}}, http.StatusUnauthorized},
		{"bad token", http.Header{"Origin": {testOrigin}, "Cookie": {httpx.AccessCookie + "=nope"}}, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, status, err := attempt(t, srv, tc.header)
			if err == nil {
				_ = conn.CloseNow()
				t.Fatal("the socket opened")
			}
			if status != tc.status {
				t.Fatalf("status = %d, want %d", status, tc.status)
			}
		})
	}

	t.Run("not an upgrade", func(t *testing.T) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/api/v1/ws", http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", testOrigin)
		req.Header.Set("Cookie", cookie)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUpgradeRequired {
			t.Errorf("status = %d", resp.StatusCode)
		}
	})
}

// Fan-out across API instances: everything crosses Redis.
func TestTwoInstancesDeliverAcrossRedis(t *testing.T) {
	first := newModEnv(t)
	second := wireEnv(t, first.pool, first.rdb, first.files, "", "", first.opt)
	one, two := serve(t, first.router), serve(t, second.router)
	ada, bob := first.newProfileUser(t), first.newProfileUser(t)
	first.link(t, ada, bob)

	bobTab := dial(t, two, bob)
	adaTab := dial(t, one, ada)

	t.Run("a message", func(t *testing.T) {
		start := time.Now()
		first.startChat(t, ada, bob, "across")
		bobTab.expect(t, realtimedomain.EventMessageCreated)
		if d := time.Since(start); d >= time.Second {
			t.Errorf("message took %s", d)
		}
	})

	t.Run("typing", func(t *testing.T) {
		id, _ := first.startChat(t, ada, bob, "again")
		if got := second.presence(t, bob, ada); !got[ada] {
			t.Error("the second instance does not see a socket on the first")
		}
		adaTab.typing(t, realtimedomain.ClientTypingStart, id)
		bobTab.expect(t, realtimedomain.EventTyping)
	})

	t.Run("the socket limit holds across instances", func(t *testing.T) {
		oldest := dial(t, one, bob)
		for i := range 5 {
			time.Sleep(2 * time.Millisecond) // socket IDs sort by age, to the millisecond
			dial(t, []*httptest.Server{one, two}[i%2], bob)
		}
		// bobTab came first of all and goes first; the rest fill the limit.
		bobTab.wantClosed(t, websocket.StatusCode(realtimedomain.CloseReplaced))
		select {
		case <-oldest.closed:
			t.Log("the next oldest closed too, as six were open")
		case <-time.After(200 * time.Millisecond):
			t.Error("seven sockets were opened and the second oldest stayed open")
		}
	})

	t.Run("a suspension on one closes sockets on the other", func(t *testing.T) {
		eve := first.newProfileUser(t)
		eveTab := dial(t, two, eve)
		first.suspend(t, eve, fmt.Sprintf(`{"reason":%q}`, modReason))
		eveTab.wantClosed(t, websocket.StatusCode(realtimedomain.CloseSessionEnded))
	})
}

// The server's read and write timeouts must not end a socket: hijacking keeps them.
func TestSocketOutlivesTheServerTimeouts(t *testing.T) {
	e := newMediaEnv(t)
	srv := httptest.NewUnstartedServer(e.router)
	srv.Config.ReadTimeout = 100 * time.Millisecond
	srv.Config.WriteTimeout = 100 * time.Millisecond
	srv.Start()
	t.Cleanup(srv.Close)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	tab := dial(t, srv, bob)

	time.Sleep(300 * time.Millisecond)
	e.startChat(t, ada, bob, "still there?")
	tab.expect(t, realtimedomain.EventMessageCreated)
}

func TestLargeFrameClosesTheSocket(t *testing.T) {
	e := newMediaEnv(t)
	srv := serve(t, e.router)
	tab := dial(t, srv, e.newProfileUser(t))
	tab.send(t, strings.Repeat("x", 4<<10+1))
	tab.wantClosed(t, websocket.StatusMessageTooBig)
}

func TestShutdownClosesSocketsCleanly(t *testing.T) {
	e := newMediaEnv(t)
	srv := serve(t, e.router)
	tab := dial(t, srv, e.newProfileUser(t))
	ctx, cancel := context.WithTimeout(t.Context(), wsWait)
	defer cancel()
	e.hub.Shutdown(ctx)
	tab.wantClosed(t, websocket.StatusGoingAway)
	conn, _, err := attempt(t, srv, dialHeader(t, e.newProfileUser(t)))
	if err == nil {
		_ = conn.CloseNow()
		t.Error("a socket opened during shutdown")
	}
}
