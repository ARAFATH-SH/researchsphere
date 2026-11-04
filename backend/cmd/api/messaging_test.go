package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	messagingdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpserver"
)

const (
	okConversationID = "4c2f7d3e-9a4d-4e8b-b7c1-2d3e4f5a6b7c"
	okMessageID      = "019a5c2e-9a4d-7e8b-b7c1-2d3e4f5a6b7c"
)

var okMessage = messagingdomain.Message{ID: okMessageID, ConversationID: okConversationID, SenderID: okUserID, Body: "hi"}

var okItem = messagingdomain.Item{
	Conversation: messagingdomain.Conversation{ID: okConversationID, State: messagingdomain.StateActive},
	Other:        messagingdomain.Person{UserID: okUserID, FullName: "Ada", Available: true},
}

// okMessaging is a messaging service for which every operation succeeds.
type okMessaging struct{}

func (okMessaging) SendDirect(context.Context, string, string, string, string) (messagingdomain.Sent, error) {
	return messagingdomain.Sent{Message: okMessage, State: messagingdomain.StateActive}, nil
}

func (okMessaging) Send(context.Context, string, string, string, string) (messagingdomain.Sent, error) {
	return messagingdomain.Sent{Message: okMessage, State: messagingdomain.StateActive}, nil
}

func (okMessaging) List(context.Context, string, string, string, *int) (messagingdomain.Page, error) {
	return messagingdomain.Page{}, nil
}

func (okMessaging) Get(context.Context, string, string) (messagingdomain.Item, error) {
	return okItem, nil
}

func (okMessaging) Messages(context.Context, string, string, string, *int) (messagingdomain.MessagePage, error) {
	return messagingdomain.MessagePage{}, nil
}

func (okMessaging) MarkRead(context.Context, string, string, string) error { return nil }

func (okMessaging) Accept(context.Context, string, string) (messagingdomain.Item, error) {
	return okItem, nil
}

func (okMessaging) Decline(context.Context, string, string) (messagingdomain.Item, error) {
	return okItem, nil
}

func (okMessaging) UnreadCounts(context.Context, string) (messagingdomain.UnreadCounts, error) {
	return messagingdomain.UnreadCounts{}, nil
}

// The messaging routes go through authz (SRS 2.3): both roles may use them, an unknown role
// gets 403 and no session gets 401.
func TestMessagingRoutesThroughRouter(t *testing.T) {
	router := testRouter()
	base := "/api/v1/conversations/" + okConversationID
	send := `{"body":"hi","clientId":"` + okConversationID + `"}`
	routes := []struct {
		method, path, body string
		status             int
	}{
		{http.MethodPost, "/api/v1/messages/direct", `{"userId":"` + okUserID + `","body":"hi","clientId":"` + okConversationID + `"}`, http.StatusCreated},
		{http.MethodGet, "/api/v1/messages/unread-count", "", http.StatusOK},
		{http.MethodGet, "/api/v1/conversations", "", http.StatusOK},
		{http.MethodGet, base, "", http.StatusOK},
		{http.MethodGet, base + "/messages", "", http.StatusOK},
		{http.MethodPost, base + "/messages", send, http.StatusCreated},
		{http.MethodPost, base + "/read", `{"messageId":"` + okMessageID + `"}`, http.StatusNoContent},
		{http.MethodPost, base + "/accept", "", http.StatusOK},
		{http.MethodPost, base + "/decline", "", http.StatusOK},
	}
	for _, r := range routes {
		call := func(t *testing.T, role string) *httptest.ResponseRecorder {
			t.Helper()
			var body io.Reader
			ct := ""
			if r.body != "" {
				body, ct = strings.NewReader(r.body), "application/json"
			}
			return sendAs(t, router, r.method, r.path, body, ct, role)
		}
		name := r.method + " " + r.path
		for _, role := range []authz.Role{authz.RoleResearcher, authz.RoleAdmin} {
			t.Run(name+"/"+string(role), func(t *testing.T) {
				if rec := call(t, string(role)); rec.Code != r.status {
					t.Errorf("status = %d, want %d; body %s", rec.Code, r.status, rec.Body)
				}
			})
		}
		t.Run(name+"/unknown role", func(t *testing.T) {
			rec := call(t, "visitor")
			if rec.Code != http.StatusForbidden || rec.Body.String() != forbiddenBody {
				t.Errorf("got %d %q, want 403 with exactly %q", rec.Code, rec.Body, forbiddenBody)
			}
		})
		t.Run(name+"/not logged in", func(t *testing.T) {
			checkUnauthenticated(t, "no token", call(t, ""))
		})
	}
}

func TestMessagingRoutesAreGuarded(t *testing.T) {
	want := map[string]authz.Action{
		"POST /api/v1/messages/direct":                         authz.ActAsMember,
		"GET /api/v1/messages/unread-count":                    authz.ViewContent,
		"GET /api/v1/conversations":                            authz.ViewContent,
		"GET /api/v1/conversations/{conversationId}":           authz.ViewContent,
		"GET /api/v1/conversations/{conversationId}/messages":  authz.ViewContent,
		"POST /api/v1/conversations/{conversationId}/messages": authz.ActAsMember,
		"POST /api/v1/conversations/{conversationId}/read":     authz.ActAsMember,
		"POST /api/v1/conversations/{conversationId}/accept":   authz.ActAsMember,
		"POST /api/v1/conversations/{conversationId}/decline":  authz.ActAsMember,
	}
	registered := map[string]bool{}
	for _, r := range httpserver.Routes() {
		registered[r] = true
	}
	for route, action := range want {
		if !registered[route] {
			t.Errorf("%s is not registered", route)
		}
		if got := platformGuards[route]; got != action {
			t.Errorf("%s: guard = %v, want %v", route, got, action)
		}
	}
}
