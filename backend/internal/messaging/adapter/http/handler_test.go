package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

const (
	caller     = "7f3a1c2e-1111-4e8b-b7c1-2d3e4f5a6b7c"
	otherID    = "7f3a1c2e-2222-4e8b-b7c1-2d3e4f5a6b7c"
	convID     = "7f3a1c2e-3333-4e8b-b7c1-2d3e4f5a6b7c"
	msgID      = "019a5c2e-4444-7e8b-b7c1-2d3e4f5a6b7c"
	clientID   = "7f3a1c2e-5555-4e8b-b7c1-2d3e4f5a6b7c"
	fileID     = "7f3a1c2e-6666-4e8b-b7c1-2d3e4f5a6b7c"
	directPath = "/api/v1/messages/direct"
)

var (
	at      = time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	message = domain.Message{ID: msgID, ConversationID: convID, SenderID: caller, Body: "hi", ClientID: clientID, CreatedAt: at}
	item    = domain.Item{
		Conversation: domain.Conversation{ID: convID, State: domain.StateRequest, RequestedBy: ptr(otherID), LastMessageAt: at},
		Other:        domain.Person{UserID: otherID, FullName: "Bob", Headline: ptr("Chemist"), PhotoFileID: ptr(fileID), Available: true},
		LastMessage:  &domain.Preview{SenderID: otherID, Body: "may I?", CreatedAt: at},
		UnreadCount:  2,
	}
)

func ptr(s string) *string { return &s }

// fakeService records the arguments of the last call and answers with err.
type fakeService struct {
	args []any
	err  error
	sent domain.Sent
	read *string
	next *string
}

func (f *fakeService) SendDirect(_ context.Context, senderID, recipientID, body, cid string) (domain.Sent, error) {
	f.args = []any{senderID, recipientID, body, cid}
	return f.sent, f.err
}

func (f *fakeService) Send(_ context.Context, senderID, id, body, cid string) (domain.Sent, error) {
	f.args = []any{senderID, id, body, cid}
	return f.sent, f.err
}

func (f *fakeService) List(_ context.Context, userID, box, cursor string, limit *int) (domain.Page, error) {
	f.args = []any{userID, box, cursor, limit}
	return domain.Page{Items: []domain.Item{item}, NextCursor: f.next}, f.err
}

func (f *fakeService) Get(_ context.Context, userID, id string) (domain.Item, error) {
	f.args = []any{userID, id}
	return item, f.err
}

func (f *fakeService) Messages(_ context.Context, userID, id, before string, limit *int) (domain.MessagePage, error) {
	f.args = []any{userID, id, before, limit}
	return domain.MessagePage{Items: []domain.Message{message}, NextBefore: f.next, OtherReadID: f.read}, f.err
}

func (f *fakeService) MarkRead(_ context.Context, userID, id, messageID string) error {
	f.args = []any{userID, id, messageID}
	return f.err
}

func (f *fakeService) Accept(_ context.Context, userID, id string) (domain.Item, error) {
	f.args = []any{userID, id}
	return item, f.err
}

func (f *fakeService) Decline(_ context.Context, userID, id string) (domain.Item, error) {
	f.args = []any{userID, id}
	return item, f.err
}

func (f *fakeService) UnreadCounts(_ context.Context, userID string) (domain.UnreadCounts, error) {
	f.args = []any{userID}
	return domain.UnreadCounts{Total: 3, Requests: 1}, f.err
}

type server struct {
	*Handler
	others
}

type others struct{ gen.StrictServerInterface }

func send(t *testing.T, svc *fakeService, method, path, body, user string) *httptest.ResponseRecorder {
	t.Helper()
	ctx := t.Context()
	if user != "" {
		ctx = httpx.WithUserID(ctx, user)
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
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	return body
}

const directBody = `{"userId":"` + otherID + `","body":"hi","clientId":"` + clientID + `"}`
const sendBody = `{"body":"hi","clientId":"` + clientID + `"}`

func TestSendAnswers(t *testing.T) {
	paths := []struct{ name, path, body string }{
		{"direct", directPath, directBody},
		{"in a conversation", "/api/v1/conversations/" + convID + "/messages", sendBody},
	}
	for _, p := range paths {
		t.Run(p.name, func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				sent   domain.Sent
				err    error
				status int
				code   string
			}{
				{"new", domain.Sent{Message: message, State: domain.StateActive}, nil, http.StatusCreated, ""},
				{"replay", domain.Sent{Message: message, State: domain.StateRequest, Replayed: true}, nil, http.StatusOK, ""},
				{"self", domain.Sent{}, domain.ErrSelf, http.StatusBadRequest, "cannot_message_self"},
				{"body", domain.Sent{}, domain.ErrInvalidBody, http.StatusBadRequest, "invalid_body"},
				{"missing", domain.Sent{}, domain.ErrNotFound, http.StatusNotFound, "not_found"},
				{"pending", domain.Sent{}, domain.ErrRequestPending, http.StatusConflict, "request_pending"},
				{"closed", domain.Sent{}, domain.ErrClosed, http.StatusConflict, "conversation_closed"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					rec := send(t, &fakeService{sent: tc.sent, err: tc.err}, http.MethodPost, p.path, p.body, caller)
					if rec.Code != tc.status {
						t.Fatalf("status = %d, want %d; body %s", rec.Code, tc.status, rec.Body)
					}
					body := decode(t, rec)
					if tc.code != "" {
						if body["code"] != tc.code || body["message"] == "" {
							t.Errorf("body = %v", body)
						}
						return
					}
					m := body["message"].(map[string]any)
					if m["id"] != msgID || m["body"] != "hi" || m["conversationId"] != convID || body["conversationState"] != string(tc.sent.State) {
						t.Errorf("body = %v", body)
					}
					if _, leaked := m["clientId"]; leaked {
						t.Errorf("clientId in the answer: %v", m)
					}
				})
			}

			t.Run("rate limited", func(t *testing.T) {
				err := &domain.RateLimitedError{RetryAfter: 1500 * time.Millisecond}
				rec := send(t, &fakeService{err: err}, http.MethodPost, p.path, p.body, caller)
				if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "2" || decode(t, rec)["code"] != "rate_limited" {
					t.Errorf("got %d, Retry-After %q, %s", rec.Code, rec.Header().Get("Retry-After"), rec.Body)
				}
			})
			t.Run("server error", func(t *testing.T) {
				rec := send(t, &fakeService{err: errors.New("boom")}, http.MethodPost, p.path, p.body, caller)
				if rec.Code != http.StatusInternalServerError {
					t.Errorf("status = %d", rec.Code)
				}
			})
			t.Run("not logged in", func(t *testing.T) {
				if rec := send(t, &fakeService{}, http.MethodPost, p.path, p.body, ""); rec.Code != http.StatusUnauthorized {
					t.Errorf("status = %d", rec.Code)
				}
			})
		})
	}
}

func TestSendPassesArguments(t *testing.T) {
	svc := &fakeService{sent: domain.Sent{Message: message, State: domain.StateActive}}
	send(t, svc, http.MethodPost, directPath, directBody, caller)
	if want := []any{caller, otherID, "hi", clientID}; !equal(svc.args, want) {
		t.Errorf("args = %v, want %v", svc.args, want)
	}
	send(t, svc, http.MethodPost, "/api/v1/conversations/"+convID+"/messages", sendBody, caller)
	if want := []any{caller, convID, "hi", clientID}; !equal(svc.args, want) {
		t.Errorf("args = %v, want %v", svc.args, want)
	}
}

func equal(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A missing ID decodes to the zero UUID, which must not reach the usecase.
func TestSendRefusesMissingIDs(t *testing.T) {
	for _, tc := range []struct{ name, path, body string }{
		{"no userId", directPath, `{"body":"hi","clientId":"` + clientID + `"}`},
		{"no clientId, direct", directPath, `{"userId":"` + otherID + `","body":"hi"}`},
		{"no clientId", "/api/v1/conversations/" + convID + "/messages", `{"body":"hi"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{}
			rec := send(t, svc, http.MethodPost, tc.path, tc.body, caller)
			if rec.Code != http.StatusBadRequest || decode(t, rec)["code"] != "bad_request" || svc.args != nil {
				t.Errorf("got %d %s, args %v", rec.Code, rec.Body, svc.args)
			}
		})
	}
}

func TestConversationBody(t *testing.T) {
	check := func(t *testing.T, c map[string]any) {
		t.Helper()
		other := c["otherMember"].(map[string]any)
		if c["id"] != convID || c["state"] != "request" || c["requestedBy"] != otherID || c["unreadCount"] != float64(2) {
			t.Errorf("conversation = %v", c)
		}
		if other["userId"] != otherID || other["fullName"] != "Bob" || other["headline"] != "Chemist" ||
			other["photoUrl"] != "/api/v1/media/"+fileID || other["available"] != true {
			t.Errorf("otherMember = %v", other)
		}
		if last := c["lastMessage"].(map[string]any); last["senderId"] != otherID || last["body"] != "may I?" {
			t.Errorf("lastMessage = %v", last)
		}
	}
	t.Run("list", func(t *testing.T) {
		svc := &fakeService{next: ptr("next")}
		rec := send(t, svc, http.MethodGet, "/api/v1/conversations?box=requests&cursor=abc&limit=5", "", caller)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		body := decode(t, rec)
		check(t, body["items"].([]any)[0].(map[string]any))
		if body["nextCursor"] != "next" {
			t.Errorf("nextCursor = %v", body["nextCursor"])
		}
		if svc.args[1] != "requests" || svc.args[2] != "abc" || *svc.args[3].(*int) != 5 {
			t.Errorf("args = %v", svc.args)
		}
	})
	for name, path := range map[string]string{"accept": "/accept", "decline": "/decline"} {
		t.Run(name, func(t *testing.T) {
			rec := send(t, &fakeService{}, http.MethodPost, "/api/v1/conversations/"+convID+path, "", caller)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			check(t, decode(t, rec))
		})
	}
	t.Run("get", func(t *testing.T) {
		rec := send(t, &fakeService{}, http.MethodGet, "/api/v1/conversations/"+convID, "", caller)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		check(t, decode(t, rec))
	})
}

func TestListRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		err        error
	}{
		{"box", "/api/v1/conversations", domain.ErrInvalidBox},
		{"cursor", "/api/v1/conversations", domain.ErrInvalidCursor},
		{"limit", "/api/v1/conversations", domain.ErrInvalidLimit},
		{"before", "/api/v1/conversations/" + convID + "/messages", domain.ErrInvalidCursor},
		{"message limit", "/api/v1/conversations/" + convID + "/messages", domain.ErrInvalidLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := send(t, &fakeService{err: tc.err}, http.MethodGet, tc.path, "", caller)
			if rec.Code != http.StatusBadRequest || decode(t, rec)["code"] != "bad_request" {
				t.Errorf("got %d %s", rec.Code, rec.Body)
			}
		})
	}
}

// NFR-14: a non-member gets the same 404 from every operation on a conversation.
func TestNotFoundOnEveryConversationRoute(t *testing.T) {
	base := "/api/v1/conversations/" + convID
	for _, r := range []struct{ method, path, body string }{
		{http.MethodGet, base, ""},
		{http.MethodGet, base + "/messages", ""},
		{http.MethodPost, base + "/messages", sendBody},
		{http.MethodPost, base + "/read", `{"messageId":"` + msgID + `"}`},
		{http.MethodPost, base + "/accept", ""},
		{http.MethodPost, base + "/decline", ""},
	} {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			rec := send(t, &fakeService{err: domain.ErrNotFound}, r.method, r.path, r.body, caller)
			body := decode(t, rec)
			if rec.Code != http.StatusNotFound || body["code"] != "not_found" || body["message"] != "This conversation does not exist." {
				t.Errorf("got %d %v", rec.Code, body)
			}
		})
	}
}

func TestListMessagesBody(t *testing.T) {
	svc := &fakeService{read: ptr(msgID), next: ptr(msgID)}
	rec := send(t, svc, http.MethodGet, "/api/v1/conversations/"+convID+"/messages?before="+msgID+"&limit=2", "", caller)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := decode(t, rec)
	if body["otherLastReadMessageId"] != msgID || body["nextBefore"] != msgID || len(body["items"].([]any)) != 1 {
		t.Errorf("body = %v", body)
	}
	if svc.args[2] != msgID || *svc.args[3].(*int) != 2 {
		t.Errorf("args = %v", svc.args)
	}
	rec = send(t, &fakeService{}, http.MethodGet, "/api/v1/conversations/"+convID+"/messages", "", caller)
	if body := decode(t, rec); body["otherLastReadMessageId"] != nil || body["nextBefore"] != nil {
		t.Errorf("no marker and no next page: %v", body)
	}
}

func TestMarkRead(t *testing.T) {
	path := "/api/v1/conversations/" + convID + "/read"
	svc := &fakeService{}
	rec := send(t, svc, http.MethodPost, path, `{"messageId":"`+msgID+`"}`, caller)
	if rec.Code != http.StatusNoContent || !equal(svc.args, []any{caller, convID, msgID}) {
		t.Errorf("got %d, args %v", rec.Code, svc.args)
	}
	if rec := send(t, &fakeService{}, http.MethodPost, path, `{}`, caller); rec.Code != http.StatusBadRequest {
		t.Errorf("no messageId: %d", rec.Code)
	}
}

func TestUnreadCount(t *testing.T) {
	rec := send(t, &fakeService{}, http.MethodGet, "/api/v1/messages/unread-count", "", caller)
	body := decode(t, rec)
	if rec.Code != http.StatusOK || body["total"] != float64(3) || body["requests"] != float64(1) {
		t.Errorf("got %d %v", rec.Code, body)
	}
}

func TestEveryOperationNeedsALogin(t *testing.T) {
	base := "/api/v1/conversations/" + convID
	for _, r := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/conversations", ""},
		{http.MethodGet, base, ""},
		{http.MethodGet, base + "/messages", ""},
		{http.MethodPost, base + "/read", `{"messageId":"` + msgID + `"}`},
		{http.MethodPost, base + "/accept", ""},
		{http.MethodPost, base + "/decline", ""},
		{http.MethodGet, "/api/v1/messages/unread-count", ""},
	} {
		if rec := send(t, &fakeService{}, r.method, r.path, r.body, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: %d", r.method, r.path, rec.Code)
		}
	}
}
