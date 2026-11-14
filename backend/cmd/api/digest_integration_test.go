//go:build integration

// End-to-end tests of the unread message digest (FR-16.11, NFR-13): the real store, the
// renderer and the router, on the test database. The mail is recorded instead of sent.
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/smtp"
)

const testAppBaseURL = "https://rs.example"

// secretWord is in every message the tests send; no digest may contain it.
const secretWord = "Xylophone7Secret"

// recordedMail records every message instead of sending it. delay slows each send, so two
// workers overlap.
type recordedMail struct {
	mu    sync.Mutex
	sent  []smtp.Message
	delay time.Duration
}

func (m *recordedMail) SendMultipart(ctx context.Context, msg smtp.Message) error {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

func (m *recordedMail) to(address string) []smtp.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []smtp.Message
	for _, s := range m.sent {
		if s.To == address {
			out = append(out, s)
		}
	}
	return out
}

// whole is everything a recipient sees of the email.
func whole(m smtp.Message) string {
	return strings.Join([]string{m.ToName, m.Subject, m.Text, m.HTML, m.ListUnsubscribeURL}, "\n")
}

func (e mediaEnv) rename(t *testing.T, id, name string) {
	t.Helper()
	if _, err := e.pool.Exec(t.Context(), "UPDATE profiles SET full_name = $2 WHERE user_id = $1", id, name); err != nil {
		t.Fatal(err)
	}
}

func (e mediaEnv) emailOf(t *testing.T, id string) string {
	t.Helper()
	var email string
	if err := e.pool.QueryRow(t.Context(), "SELECT email::text FROM users WHERE id = $1", id).Scan(&email); err != nil {
		t.Fatal(err)
	}
	return email
}

// ageEverything moves every message 25 hours back and every member's joining 100 hours back,
// as if the messages had waited a day unread.
func (e mediaEnv) ageEverything(t *testing.T) {
	t.Helper()
	for _, q := range []string{
		"UPDATE messages SET created_at = created_at - interval '25 hours'",
		"UPDATE conversation_members SET joined_at = joined_at - interval '100 hours'",
	} {
		if _, err := e.pool.Exec(t.Context(), q); err != nil {
			t.Fatal(err)
		}
	}
}

func (e mediaEnv) sendDigests(t *testing.T) {
	t.Helper()
	if _, err := e.digests.SendDue(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func (e mediaEnv) digestCount(t *testing.T, id string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM message_digests WHERE user_id = $1", id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// anonymous sends a request with no session.
func (e mediaEnv) anonymous(t *testing.T, method, path, body, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

// The digest names counts and people, never what the messages say (NFR-13).
func TestDigestEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada := e.newProfileUser(t)
	e.rename(t, ada, "Ada Lovelace")

	// Seven people write to Ada: person i sends 8-i messages, so the order is known.
	for i := range 7 {
		p := e.newProfileUser(t)
		e.rename(t, p, fmt.Sprintf("Person %d", i))
		e.link(t, p, ada)
		id, _ := e.startChat(t, p, ada, secretWord+" first")
		for j := 1; j < 8-i; j++ {
			if code, body := e.inConversation(t, p, id, fmt.Sprintf("%s %d", secretWord, j), newClientID()); code != http.StatusCreated {
				t.Fatalf("send: %d %v", code, body)
			}
		}
	}
	// Two message requests from strangers: counted, never quoted.
	for range 2 {
		e.startChat(t, e.newProfileUser(t), ada, secretWord+" request")
	}
	e.ageEverything(t)
	e.sendDigests(t)

	got := e.digestMail.to(e.emailOf(t, ada))
	if len(got) != 1 {
		t.Fatalf("Ada got %d digests, want 1", len(got))
	}
	mail := whole(got[0])
	if strings.Contains(strings.ToLower(mail), strings.ToLower(secretWord)) {
		t.Fatalf("the digest contains message text:\n%s", mail)
	}
	total := 0
	for i := range 7 {
		total += 8 - i
	}
	if want := fmt.Sprintf("You have %d unread messages on ResearchSphere", total); got[0].Subject != want {
		t.Errorf("Subject = %q, want %q", got[0].Subject, want)
	}
	for _, want := range []string{"Person 0 (8)", "Person 4 (4)", "and 2 more conversations", "You have 2 message requests.", testAppBaseURL + "/messaging"} {
		if !strings.Contains(got[0].Text, want) || !strings.Contains(got[0].HTML, want) {
			t.Errorf("the digest lacks %q:\n%s", want, got[0].Text)
		}
	}
	if strings.Contains(got[0].Text, "Person 5") {
		t.Error("the digest names more than 5 conversations")
	}
	if !strings.HasPrefix(got[0].ListUnsubscribeURL, testAppBaseURL+"/api/v1/unsubscribe?token=") {
		t.Errorf("ListUnsubscribeURL = %q", got[0].ListUnsubscribeURL)
	}
	if !strings.Contains(got[0].Text, testAppBaseURL+"/unsubscribe?token=") {
		t.Error("the text has no unsubscribe link")
	}
	var unread, convs, requests int
	if err := e.pool.QueryRow(t.Context(), "SELECT unread_count, conversation_count, request_count FROM message_digests WHERE user_id = $1", ada).
		Scan(&unread, &convs, &requests); err != nil || unread != total || convs != 7 || requests != 2 {
		t.Errorf("record = %d, %d, %d, %v", unread, convs, requests, err)
	}

	// Within 24 hours nothing more is sent, and after them only new messages would count.
	e.sendDigests(t)
	if n := len(e.digestMail.to(e.emailOf(t, ada))); n != 1 {
		t.Errorf("Ada got %d digests after a second run, want 1", n)
	}
	// 25 hours pass: everything, the digest's state included, moves back.
	e.ageEverything(t)
	if _, err := e.pool.Exec(t.Context(), "UPDATE message_digest_state SET last_digest_at = last_digest_at - interval '25 hours', covered_until = covered_until - interval '25 hours'"); err != nil {
		t.Fatal(err)
	}
	e.sendDigests(t)
	if n := len(e.digestMail.to(e.emailOf(t, ada))); n != 1 {
		t.Errorf("a message was in two digests: Ada got %d", n)
	}
}

// Group messages are counted under the group's name, never quoted.
func TestDigestGroups(t *testing.T) {
	e := newMediaEnv(t)
	owner, ada := e.newProfileUser(t), e.newProfileUser(t)
	id := e.newGroup(t, owner, ada)
	if code, body := e.call(t, owner, http.MethodPatch, "/api/v1/conversations/"+id, `{"name":"CVS Paper Team"}`); code != http.StatusOK {
		t.Fatalf("rename: %d %v", code, body)
	}
	for range 3 {
		e.inConversation(t, owner, id, secretWord, newClientID())
	}
	e.ageEverything(t)
	e.sendDigests(t)
	got := e.digestMail.to(e.emailOf(t, ada))
	if len(got) != 1 || !strings.Contains(got[0].Text, "CVS Paper Team (3)") || strings.Contains(whole(got[0]), secretWord) {
		t.Fatalf("digest = %+v", got)
	}
	if len(e.digestMail.to(e.emailOf(t, owner))) != 0 {
		t.Error("the sender got a digest of their own messages")
	}
}

func TestDigestNotForBlockedOrOptedOut(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, bob, ada)
	e.link(t, cy, bob)
	e.startChat(t, bob, ada, secretWord)
	e.startChat(t, cy, bob, secretWord)
	e.block(t, ada, bob) // Ada blocked Bob: his message does not count for her.
	if code, body := e.call(t, bob, http.MethodPatch, "/api/v1/me/settings", `{"unreadMessageDigest":false}`); code != http.StatusOK || body["unreadMessageDigest"] != false {
		t.Fatalf("turn off: %d %v", code, body)
	}
	e.ageEverything(t)
	e.sendDigests(t)
	if n := len(e.digestMail.sent); n != 0 {
		t.Errorf("sent %d digests, want none", n)
	}
}

func TestSettingsAPI(t *testing.T) {
	e := newMediaEnv(t)
	ada := e.newProfileUser(t)
	if code, body := e.call(t, ada, http.MethodGet, "/api/v1/me/settings", ""); code != http.StatusOK || body["unreadMessageDigest"] != true {
		t.Fatalf("GET default: %d %v", code, body)
	}
	if code, body := e.call(t, ada, http.MethodPatch, "/api/v1/me/settings", `{}`); code != http.StatusBadRequest || body["code"] != "bad_request" {
		t.Errorf("PATCH {}: %d %v", code, body)
	}
	if code, body := e.call(t, ada, http.MethodPatch, "/api/v1/me/settings", `{"unreadMessageDigest":false}`); code != http.StatusOK || body["unreadMessageDigest"] != false {
		t.Fatalf("PATCH off: %d %v", code, body)
	}
	if code, body := e.call(t, ada, http.MethodGet, "/api/v1/me/settings", ""); code != http.StatusOK || body["unreadMessageDigest"] != false {
		t.Errorf("GET after off: %d %v", code, body)
	}
	if rec := e.anonymous(t, http.MethodGet, "/api/v1/me/settings", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("GET without a session: %d", rec.Code)
	}
}

// One-click unsubscribe works without a session and only ever turns the digest off.
func TestUnsubscribe(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	token, err := e.digests.UnsubscribeToken(ada)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/unsubscribe?token=" + url.QueryEscape(token)

	// What a mail client sends (RFC 8058).
	rec := e.anonymous(t, http.MethodPost, path, "List-Unsubscribe=One-Click", "application/x-www-form-urlencoded")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("one-click: %d %s", rec.Code, rec.Body)
	}
	if _, body := e.call(t, ada, http.MethodGet, "/api/v1/me/settings", ""); body["unreadMessageDigest"] != false {
		t.Errorf("Ada's digest is still on: %v", body)
	}
	if _, body := e.call(t, bob, http.MethodGet, "/api/v1/me/settings", ""); body["unreadMessageDigest"] != true {
		t.Errorf("Bob's digest changed: %v", body)
	}
	// From the page: no body. Again, after turning it back on in Settings: off once more.
	e.call(t, ada, http.MethodPatch, "/api/v1/me/settings", `{"unreadMessageDigest":true}`)
	if rec := e.anonymous(t, http.MethodPost, path, "", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("from the page: %d %s", rec.Code, rec.Body)
	}
	if _, body := e.call(t, ada, http.MethodGet, "/api/v1/me/settings", ""); body["unreadMessageDigest"] != false {
		t.Errorf("the token did not turn the digest off: %v", body)
	}
	// The same token again keeps it off (it never toggles).
	e.anonymous(t, http.MethodPost, path, "", "")
	if _, body := e.call(t, ada, http.MethodGet, "/api/v1/me/settings", ""); body["unreadMessageDigest"] != false {
		t.Errorf("a second unsubscribe turned it on: %v", body)
	}

	idPart, sig, _ := strings.Cut(token, ".")
	bobToken, _ := e.digests.UnsubscribeToken(bob)
	bobID, _, _ := strings.Cut(bobToken, ".")
	for name, bad := range map[string]string{
		"tampered":          idPart + "." + strings.ToUpper(sig[:4]) + sig[4:] + "x",
		"another user's ID": bobID + "." + sig,
		"garbage":           "garbage",
		"too long":          strings.Repeat("a", 201),
	} {
		t.Run(name, func(t *testing.T) {
			rec := e.anonymous(t, http.MethodPost, "/api/v1/unsubscribe?token="+url.QueryEscape(bad), "", "")
			if body := decodeBody(t, rec.Body.Bytes()); rec.Code != http.StatusBadRequest || body["code"] != "invalid_token" || body["field"] != "token" {
				t.Errorf("%d %v; want 400 invalid_token", rec.Code, body)
			}
		})
	}
	if _, body := e.call(t, bob, http.MethodGet, "/api/v1/me/settings", ""); body["unreadMessageDigest"] != true {
		t.Errorf("a bad token turned Bob's digest off: %v", body)
	}
	if rec := e.anonymous(t, http.MethodPost, "/api/v1/unsubscribe", "", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("no token: %d", rec.Code)
	}
	if rec := e.anonymous(t, http.MethodGet, path, "", ""); rec.Code == http.StatusNoContent {
		t.Error("a GET unsubscribed")
	}
}

// Two API instances run the job at once: every user gets exactly one digest.
func TestDigestTwoWorkers(t *testing.T) {
	e := newMediaEnv(t)
	sender := e.newProfileUser(t)
	var readers []string
	for range 6 {
		r := e.newProfileUser(t)
		e.link(t, sender, r)
		e.startChat(t, sender, r, "hello")
		readers = append(readers, r)
	}
	e.ageEverything(t)
	e.digestMail.delay = 50 * time.Millisecond

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { e.sendDigests(t) })
	}
	wg.Wait()
	for _, r := range readers {
		if n := len(e.digestMail.to(e.emailOf(t, r))); n != 1 {
			t.Errorf("a reader got %d digests, want 1", n)
		}
		if n := e.digestCount(t, r); n != 1 {
			t.Errorf("a reader has %d digest records, want 1", n)
		}
	}
	if n := len(e.digestMail.sent); n != len(readers) {
		t.Errorf("sent %d digests, want %d", n, len(readers))
	}
}
