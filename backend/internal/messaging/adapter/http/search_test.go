package http

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/usecase"
)

func (f *fakeService) Search(_ context.Context, userID string, in usecase.SearchInput) (domain.SearchPage, error) {
	f.args = []any{userID, in}
	return f.search, f.err
}

const groupID = "7f3a1c2e-5555-4e8b-b7c1-2d3e4f5a6b7c"

// FR-16.8: the parameters reach the usecase as sent, and a result carries the conversation, the
// sender, the snippet with its ranges and what matched.
func TestSearchMessagesAnswers(t *testing.T) {
	photo := "7f3a1c2e-6666-4e8b-b7c1-2d3e4f5a6b7c"
	svc := &fakeService{search: domain.SearchPage{
		Items: []domain.SearchResult{
			{
				SearchHit: domain.SearchHit{
					MessageID: msgID, ConversationID: convID, Kind: domain.KindDirect, SenderID: otherID, SenderName: "Nafisa Hasan",
					CreatedAt: at, MatchedOn: domain.MatchText,
					Other: &domain.Person{UserID: otherID, FullName: "Nafisa Hasan", PhotoFileID: &photo},
				},
				Avatars: []domain.Person{}, Snippet: "আমি গবেষণা করি", Highlights: []domain.Range{{Start: 4, Length: 6}},
			},
			{
				SearchHit: domain.SearchHit{
					MessageID: msgID, ConversationID: groupID, Kind: domain.KindGroup, Name: "Lab", SenderID: caller, SenderName: "Me",
					CreatedAt: at, MatchedOn: domain.MatchAttachment, AttachmentName: "thesis.pdf",
				},
				Avatars: []domain.Person{{UserID: otherID, FullName: "Nafisa Hasan"}}, Snippet: "thesis.pdf",
				Highlights: []domain.Range{},
			},
		},
		NextCursor: ptr("next"),
		People:     map[string]domain.PagePerson{otherID: {UserID: otherID, FullName: "Nafisa Hasan", PhotoFileID: &photo, Available: true}},
	}}
	after, before := "2026-11-01T00:00:00+06:00", "2026-11-02T00:00:00Z"
	v := url.Values{"q": {"গবেষণা"}, "conversationId": {convID}, "from": {otherID}, "after": {after}, "before": {before},
		"cursor": {"c1"}, "limit": {"5"}}
	rec := send(t, svc, http.MethodGet, "/api/v1/messages/search?"+v.Encode(), "", caller)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; %s", rec.Code, rec.Body)
	}
	in := svc.args[1].(usecase.SearchInput)
	wantAfter, _ := time.Parse(time.RFC3339, after)
	if svc.args[0] != caller || in.Query != "গবেষণা" || in.ConversationID != convID || in.From != otherID || in.Cursor != "c1" ||
		in.Limit == nil || *in.Limit != 5 || in.After == nil || !in.After.Equal(wantAfter) || in.Before == nil {
		t.Errorf("input = %+v", in)
	}
	body := decode(t, rec)
	if body["nextCursor"] != "next" {
		t.Errorf("nextCursor = %v", body["nextCursor"])
	}
	if p := body["people"].(map[string]any)[otherID].(map[string]any); p["fullName"] != "Nafisa Hasan" || p["photoUrl"] != "/api/v1/media/"+photo {
		t.Errorf("people = %v", body["people"])
	}
	items := body["items"].([]any)
	direct, group := items[0].(map[string]any), items[1].(map[string]any)
	dc := direct["conversation"].(map[string]any)
	other := dc["otherMember"].(map[string]any)
	if dc["kind"] != "direct" || dc["name"] != nil || other["userId"] != otherID || other["photoUrl"] != "/api/v1/media/"+photo ||
		len(dc["avatars"].([]any)) != 0 {
		t.Errorf("direct conversation = %v", dc)
	}
	ranges := direct["highlights"].([]any)
	if direct["snippet"] != "আমি গবেষণা করি" || direct["matchedOn"] != "text" || len(ranges) != 1 ||
		ranges[0].(map[string]any)["start"] != 4.0 || ranges[0].(map[string]any)["length"] != 6.0 {
		t.Errorf("direct = %v", direct)
	}
	sender := direct["sender"].(map[string]any)
	if sender["userId"] != otherID || sender["fullName"] != "Nafisa Hasan" || direct["createdAt"] != at.Format(time.RFC3339) {
		t.Errorf("sender = %v, createdAt = %v", sender, direct["createdAt"])
	}
	gc := group["conversation"].(map[string]any)
	if gc["kind"] != "group" || gc["name"] != "Lab" || gc["otherMember"] != nil || len(gc["avatars"].([]any)) != 1 ||
		group["matchedOn"] != "attachment" || group["snippet"] != "thesis.pdf" {
		t.Errorf("group = %v", group)
	}
}

// FR-16.8: each refusal has its status, code and, for a bad parameter, its field.
func TestSearchMessagesRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
		field  any
	}{
		{"q", domain.ErrInvalidQuery, http.StatusBadRequest, "bad_request", "q"},
		{"cursor", domain.ErrInvalidCursor, http.StatusBadRequest, "bad_request", "cursor"},
		{"limit", domain.ErrInvalidLimit, http.StatusBadRequest, "bad_request", "limit"},
		{"not a member", domain.ErrNotFound, http.StatusNotFound, "not_found", nil},
		{"limited", &domain.RateLimitedError{RetryAfter: 1500 * time.Millisecond}, http.StatusTooManyRequests, "rate_limited", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := send(t, &fakeService{err: tc.err}, http.MethodGet, "/api/v1/messages/search?q=ab", "", caller)
			body := decode(t, rec)
			if rec.Code != tc.status || body["code"] != tc.code || body["field"] != tc.field {
				t.Errorf("status = %d, body = %v", rec.Code, body)
			}
			if tc.status == http.StatusTooManyRequests && rec.Header().Get("Retry-After") != "2" {
				t.Errorf("Retry-After = %q", rec.Header().Get("Retry-After"))
			}
		})
	}
	if rec := send(t, &fakeService{}, http.MethodGet, "/api/v1/messages/search?q=ab", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: status = %d", rec.Code)
	}
	// A missing q reaches the usecase as empty text, which it refuses (TestSearchChecksTheText):
	// q is declared optional so that the session is checked first.
	svc := &fakeService{}
	if send(t, svc, http.MethodGet, "/api/v1/messages/search", "", caller); svc.args == nil || svc.args[1].(usecase.SearchInput).Query != "" {
		t.Errorf("missing q: args = %v", svc.args)
	}
	// The generated wrapper refuses a parameter that is not of its type before the handler.
	for _, query := range []string{"q=ab&conversationId=x", "q=ab&from=x", "q=ab&after=yesterday", "q=ab&limit=x"} {
		svc := &fakeService{}
		if rec := send(t, svc, http.MethodGet, "/api/v1/messages/search?"+query, "", caller); rec.Code != http.StatusBadRequest || svc.args != nil {
			t.Errorf("%q: status = %d, args = %v", query, rec.Code, svc.args)
		}
	}
}
