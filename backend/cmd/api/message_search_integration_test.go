//go:build integration

// End-to-end tests of message search (FR-16.8, NFR-13) through the real router, the PostgreSQL
// store and the Redis limiter, on the test database.
package main

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

func (e mediaEnv) searchRaw(t *testing.T, userID string, v url.Values) (int, map[string]any, string) {
	t.Helper()
	rec := e.send(t, userID, http.MethodGet, "/api/v1/messages/search?"+v.Encode(), nil, "")
	return rec.Code, decodeBody(t, rec.Body.Bytes()), rec.Header().Get("Retry-After")
}

// FR-16.8: a Bangla word is found in the caller's chat with its conversation, sender, snippet
// and a highlight counted in code points; someone else finds nothing and gets 404 for the
// conversation; a bad q names the field.
func TestMessageSearchEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, msg := e.startChat(t, ada, bob, "👍 আমরা গবেষণা শুরু করি")

	code, body, _ := e.searchRaw(t, bob, url.Values{"q": {" গবেষণা "}})
	if code != http.StatusOK {
		t.Fatalf("search: %d %v", code, body)
	}
	items := body["items"].([]any)
	if len(items) != 1 || body["nextCursor"] != nil {
		t.Fatalf("items = %v", body)
	}
	it := items[0].(map[string]any)
	conv := it["conversation"].(map[string]any)
	sender := it["sender"].(map[string]any)
	if it["messageId"] != msg || it["conversationId"] != id || it["matchedOn"] != "text" || conv["kind"] != "direct" ||
		conv["otherMember"].(map[string]any)["userId"] != ada || sender["userId"] != ada {
		t.Errorf("item = %v", it)
	}
	snippet := []rune(it["snippet"].(string))
	ranges := it["highlights"].([]any)
	if len(ranges) != 1 {
		t.Fatalf("highlights = %v", ranges)
	}
	r := ranges[0].(map[string]any)
	start, length := int(r["start"].(float64)), int(r["length"].(float64))
	if start != 7 || string(snippet[start:start+length]) != "গবেষণা" {
		t.Errorf("highlight %v in %q", r, string(snippet))
	}

	if code, body, _ := e.searchRaw(t, cy, url.Values{"q": {"গবেষণা"}}); code != http.StatusOK || len(body["items"].([]any)) != 0 {
		t.Errorf("cy: %d %v", code, body)
	}
	code, body, _ = e.searchRaw(t, cy, url.Values{"q": {"গবেষণা"}, "conversationId": {id}})
	wantError(t, "cy in ada's chat", code, body, http.StatusNotFound, "not_found")
	code, body, _ = e.searchRaw(t, ada, url.Values{"q": {" গ "}})
	wantError(t, "short q", code, body, http.StatusBadRequest, "bad_request")
	if body["field"] != "q" {
		t.Errorf("field = %v", body["field"])
	}
	code, body, _ = e.searchRaw(t, ada, url.Values{})
	wantError(t, "missing q", code, body, http.StatusBadRequest, "bad_request")
	if body["field"] != "q" {
		t.Errorf("missing q: field = %v", body["field"])
	}
}

// FR-16.8: 30 searches a minute per user, counted in Redis; the 31st gets 429 with Retry-After,
// while another user may still search.
func TestMessageSearchRateLimit(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	for i := range 30 {
		if code, body, _ := e.searchRaw(t, ada, url.Values{"q": {"paper"}}); code != http.StatusOK {
			t.Fatalf("search %d: %d %v", i+1, code, body)
		}
	}
	code, body, retry := e.searchRaw(t, ada, url.Values{"q": {"paper"}})
	wantError(t, "31st search", code, body, http.StatusTooManyRequests, "rate_limited")
	if n, err := strconv.Atoi(retry); err != nil || n < 1 || n > 60 {
		t.Errorf("Retry-After = %q", retry)
	}
	if code, _, _ := e.searchRaw(t, bob, url.Values{"q": {"paper"}}); code != http.StatusOK {
		t.Errorf("bob: %d", code)
	}
}
