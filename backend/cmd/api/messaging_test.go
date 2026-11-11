package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	messagingdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	messagingusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpserver"
)

const (
	okConversationID = "4c2f7d3e-9a4d-4e8b-b7c1-2d3e4f5a6b7c"
	okMessageID      = "019a5c2e-9a4d-7e8b-b7c1-2d3e4f5a6b7c"
)

var okMessage = messagingdomain.Message{ID: okMessageID, ConversationID: okConversationID, SenderID: okUserID, Body: &okBody}

var okBody = "hi"

var okItem = messagingdomain.Item{
	Conversation: messagingdomain.Conversation{ID: okConversationID, State: messagingdomain.StateActive},
	Other:        messagingdomain.Person{UserID: okUserID, FullName: "Ada", Available: true},
}

// okMessaging is a messaging service for which every operation succeeds.
type okMessaging struct{}

func (okMessaging) Edit(context.Context, string, string, string, string) (messagingdomain.Message, error) {
	return okMessage, nil
}

func (okMessaging) Delete(context.Context, string, string, string) error { return nil }

func (okMessaging) SendDirect(context.Context, string, string, string, string) (messagingdomain.Sent, error) {
	return messagingdomain.Sent{Message: okMessage, State: messagingdomain.StateActive}, nil
}

func (okMessaging) Send(context.Context, string, string, string, string) (messagingdomain.Sent, error) {
	return messagingdomain.Sent{Message: okMessage, State: messagingdomain.StateActive}, nil
}

func (okMessaging) SendWithAttachments(context.Context, string, string, *string, string, []string) (messagingdomain.Sent, error) {
	return messagingdomain.Sent{Message: okMessage, State: messagingdomain.StateActive}, nil
}

func (okMessaging) UploadAttachment(context.Context, string, string, string, io.Reader) (messagingdomain.Upload, error) {
	return messagingdomain.Upload{Attachment: messagingdomain.Attachment{ID: okMessage.ID, Name: "a.pdf", Size: 1,
		ContentType: "application/pdf", Status: messagingdomain.AttachmentPending}}, nil
}

func (okMessaging) DownloadAttachment(context.Context, string, string) (messagingusecase.Download, error) {
	return messagingusecase.Download{URL: &url.URL{Scheme: "https", Host: "bucket.example"}}, nil
}

func (okMessaging) DownloadReportedAttachment(context.Context, string, string, string) (messagingusecase.Download, error) {
	return messagingusecase.Download{URL: &url.URL{Scheme: "https", Host: "bucket.example"}}, nil
}

func (okMessaging) List(context.Context, string, string, string, *int) (messagingdomain.Page, error) {
	return messagingdomain.Page{}, nil
}

func (okMessaging) Get(context.Context, string, string) (messagingdomain.Item, error) {
	return okItem, nil
}

func (okMessaging) Messages(context.Context, string, string, messagingdomain.MessageQuery) (messagingdomain.MessagePage, error) {
	return messagingdomain.MessagePage{}, nil
}

func (okMessaging) MarkRead(context.Context, string, string, string) error { return nil }

func (okMessaging) Accept(context.Context, string, string) (messagingdomain.Item, error) {
	return okItem, nil
}

func (okMessaging) Decline(context.Context, string, string) (messagingdomain.Item, error) {
	return okItem, nil
}

func (okMessaging) Report(context.Context, string, string, messagingdomain.ReportInput) (messagingdomain.ReportReceipt, error) {
	reported := okUserID
	return messagingdomain.ReportReceipt{ID: okUserID, ReportedID: &reported}, nil
}

var okGroup = messagingdomain.Item{
	Conversation: messagingdomain.Conversation{ID: okConversationID, Kind: messagingdomain.KindGroup, State: messagingdomain.StateActive},
	Group:        &messagingdomain.GroupInfo{Name: "Lab", MemberCount: 2, MyRole: messagingdomain.RoleOwner},
}

func (okMessaging) CreateGroup(context.Context, string, messagingdomain.GroupInput) (messagingdomain.Item, bool, error) {
	return okGroup, false, nil
}

func (okMessaging) RenameGroup(context.Context, string, string, string) (messagingdomain.Item, error) {
	return okGroup, nil
}

func (okMessaging) AddMembers(context.Context, string, string, []string) (messagingdomain.Item, error) {
	return okGroup, nil
}

func (okMessaging) RemoveMember(context.Context, string, string, string) error { return nil }

func (okMessaging) Leave(context.Context, string, string) error { return nil }

func (okMessaging) ListReports(context.Context, string, int) (messagingdomain.ReportPage, error) {
	return messagingdomain.ReportPage{}, nil
}

func (okMessaging) ReportDetail(context.Context, string, string, string) (messagingdomain.ReportDetail, error) {
	return messagingdomain.ReportDetail{Report: okReport}, nil
}

func (okMessaging) ResolveReport(context.Context, string, string, messagingdomain.ReportOutcome, *string) (messagingdomain.Report, error) {
	return okReport, nil
}

var okReport = messagingdomain.Report{
	ID: okUserID, ConversationID: okUserID,
	Reporter: messagingdomain.ReportPerson{UserID: okUserID}, Reported: &messagingdomain.ReportPerson{UserID: okUserID},
}

func (okMessaging) UnreadCounts(context.Context, string) (messagingdomain.UnreadCounts, error) {
	return messagingdomain.UnreadCounts{}, nil
}

func (okMessaging) Search(context.Context, string, messagingusecase.SearchInput) (messagingdomain.SearchPage, error) {
	return messagingdomain.SearchPage{Items: []messagingdomain.SearchResult{}}, nil
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
		{http.MethodGet, "/api/v1/messages/search?q=paper", "", http.StatusOK},
		{http.MethodGet, "/api/v1/conversations", "", http.StatusOK},
		{http.MethodGet, base, "", http.StatusOK},
		{http.MethodGet, base + "/messages", "", http.StatusOK},
		{http.MethodPost, base + "/messages", send, http.StatusCreated},
		{http.MethodPost, base + "/read", `{"messageId":"` + okMessageID + `"}`, http.StatusNoContent},
		{http.MethodPost, base + "/accept", "", http.StatusOK},
		{http.MethodPost, base + "/decline", "", http.StatusOK},
		{http.MethodPatch, base + "/messages/" + okMessageID, `{"body":"edited"}`, http.StatusOK},
		{http.MethodDelete, base + "/messages/" + okMessageID, "", http.StatusNoContent},
		{http.MethodPost, "/api/v1/conversations/groups", `{"name":"Lab","memberIds":["` + okUserID + `"],"clientId":"` + okConversationID + `"}`, http.StatusCreated},
		{http.MethodPatch, base, `{"name":"Lab 2"}`, http.StatusOK},
		{http.MethodPost, base + "/members", `{"userIds":["` + okUserID + `"]}`, http.StatusOK},
		{http.MethodDelete, base + "/members/" + okUserID, "", http.StatusNoContent},
		{http.MethodPost, base + "/leave", "", http.StatusNoContent},
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
		"POST /api/v1/messages/direct":                                       authz.ActAsMember,
		"GET /api/v1/messages/unread-count":                                  authz.ViewContent,
		"GET /api/v1/messages/search":                                        authz.ViewContent,
		"GET /api/v1/conversations":                                          authz.ViewContent,
		"GET /api/v1/conversations/{conversationId}":                         authz.ViewContent,
		"GET /api/v1/conversations/{conversationId}/messages":                authz.ViewContent,
		"POST /api/v1/conversations/{conversationId}/messages":               authz.ActAsMember,
		"POST /api/v1/conversations/{conversationId}/read":                   authz.ActAsMember,
		"POST /api/v1/conversations/{conversationId}/accept":                 authz.ActAsMember,
		"POST /api/v1/conversations/{conversationId}/decline":                authz.ActAsMember,
		"PATCH /api/v1/conversations/{conversationId}/messages/{messageId}":  authz.ActAsMember,
		"DELETE /api/v1/conversations/{conversationId}/messages/{messageId}": authz.ActAsMember,
		"POST /api/v1/conversations/groups":                                  authz.ActAsMember,
		"PATCH /api/v1/conversations/{conversationId}":                       authz.ActAsMember,
		"POST /api/v1/conversations/{conversationId}/members":                authz.ActAsMember,
		"DELETE /api/v1/conversations/{conversationId}/members/{userId}":     authz.ActAsMember,
		"POST /api/v1/conversations/{conversationId}/leave":                  authz.ActAsMember,
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
