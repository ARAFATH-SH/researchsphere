package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
)

// jsonBodyRoutes is every operation with a JSON body. missing is the field an empty object
// is refused for: the first required one, by name; empty for a body with none required.
// wrong is a body whose named field has the wrong type.
var jsonBodyRoutes = []struct {
	method, path, missing, wrong, wrongField, message string
}{
	{http.MethodPost, "/api/v1/account/delete", "password", `{"password":5}`, "password", "password must be a string."},
	{http.MethodPatch, "/api/v1/me/settings", "", `{"unreadMessageDigest":"no"}`, "unreadMessageDigest", "unreadMessageDigest must be true or false."},
	{http.MethodPost, "/api/v1/auth/register", "email", `{"email":5}`, "email", "email must be a string."},
	{http.MethodPost, "/api/v1/auth/verify-email", "token", `{"token":5}`, "token", "token must be a string."},
	{http.MethodPost, "/api/v1/auth/resend-verification", "email", `{"email":5}`, "email", "email must be a string."},
	{http.MethodPost, "/api/v1/auth/password-reset/request", "email", `{"email":5}`, "email", "email must be a string."},
	{http.MethodPost, "/api/v1/auth/password-reset/confirm", "newPassword", `{"newPassword":5}`, "newPassword", "newPassword must be a string."},
	{http.MethodPost, "/api/v1/auth/login", "email", `{"email":5}`, "email", "email must be a string."},
	{http.MethodPost, "/api/v1/auth/password", "currentPassword", `{"currentPassword":5}`, "currentPassword", "currentPassword must be a string."},
	{http.MethodPut, "/api/v1/profiles/me", "fullName", `{"fullName":5}`, "fullName", "fullName must be a string."},
	{http.MethodPost, "/api/v1/profiles/me/interests", "name", `{"name":5}`, "name", "name must be a string."},
	{http.MethodPost, "/api/v1/profiles/me/skills", "name", `{"name":5}`, "name", "name must be a string."},
	{http.MethodPost, "/api/v1/profiles/me/education", "institution", `{"institution":5}`, "institution", "institution must be a string."},
	{http.MethodPatch, "/api/v1/profiles/me/education/" + okUserID, "", `{"startYear":"then"}`, "startYear", "startYear must be a whole number."},
	{http.MethodPost, "/api/v1/profiles/me/experience", "employmentType", `{"employmentType":5}`, "employmentType", "employmentType must be a string."},
	{http.MethodPatch, "/api/v1/profiles/me/experience/" + okUserID, "", `{"title":5}`, "title", "title must be a string."},
	{http.MethodPost, "/api/v1/profiles/me/languages", "name", `{"name":5}`, "name", "name must be a string."},
	{http.MethodPatch, "/api/v1/profiles/me/languages/" + okUserID, "", `{"name":5}`, "name", "name must be a string."},
	{http.MethodPut, "/api/v1/profiles/me/contact", "birthdayVisibility", `{"birthdayVisibility":5}`, "birthdayVisibility", "birthdayVisibility must be a string."},
	{http.MethodPost, "/api/v1/profiles/me/links", "kind", `{"kind":5}`, "kind", "kind must be a string."},
	{http.MethodPost, "/api/v1/profiles/me/publications", "title", `{"title":5}`, "title", "title must be a string."},
	{http.MethodPost, "/api/v1/profiles/me/publications/from-doi", "doi", `{"doi":5}`, "doi", "doi must be a string."},
	{http.MethodPut, "/api/v1/profiles/me/orcid", "orcidId", `{"orcidId":5}`, "orcidId", "orcidId must be a string."},
	{http.MethodPost, "/api/v1/profiles/me/publications/from-orcid", "putCodes", `{"putCodes":5}`, "putCodes", "putCodes must be a list."},
	{http.MethodPatch, "/api/v1/profiles/me/publications/" + okUserID, "", `{"title":5}`, "title", "title must be a string."},
	{http.MethodPost, "/api/v1/connections/requests", "userId", `{"userId":5}`, "userId", "userId must be a string."},
	{http.MethodPost, "/api/v1/blocks", "userId", `{"userId":5}`, "userId", "userId must be a UUID."},
	{http.MethodPost, "/api/v1/conversations/" + okConversationID + "/reports", "reason", `{"reason":5}`, "reason", "reason must be a string."},
	{http.MethodPost, "/api/v1/admin/message-reports/" + okConversationID + "/resolve", "outcome", `{"outcome":5}`, "outcome", "outcome must be a string."},
	{http.MethodPost, "/api/v1/messages/direct", "body", `{"body":5}`, "body", "body must be a string."},
	// FR-16.7: body may be left out when attachmentIds is set, so clientId is the one required field.
	{http.MethodPost, "/api/v1/conversations/" + okConversationID + "/messages", "clientId", `{"body":5}`, "body", "body must be a string."},
	{http.MethodPost, "/api/v1/conversations/" + okConversationID + "/read", "messageId", `{"messageId":5}`, "messageId", "messageId must be a UUID."},
	{http.MethodPatch, "/api/v1/conversations/" + okConversationID + "/messages/" + okMessageID, "body", `{"body":5}`, "body", "body must be a string."},
	{http.MethodPost, "/api/v1/conversations/groups", "clientId", `{"clientId":5}`, "clientId", "clientId must be a UUID."},
	{http.MethodPatch, "/api/v1/conversations/" + okConversationID, "name", `{"name":5}`, "name", "name must be a string."},
	{http.MethodPost, "/api/v1/conversations/" + okConversationID + "/members", "userIds", `{"userIds":"x"}`, "userIds", "userIds must be a list."},
	{http.MethodPost, "/api/v1/admin/users/" + okUserID + "/warnings", "reason", `{"reason":5}`, "reason", "reason must be a string."},
	{http.MethodPost, "/api/v1/admin/users/" + okUserID + "/suspension", "reason", `{"reason":5}`, "reason", "reason must be a string."},
	{http.MethodPost, "/api/v1/admin/users/" + okUserID + "/restore", "reason", `{"reason":5}`, "reason", "reason must be a string."},
	{http.MethodPost, "/api/v1/admin/restrictions/" + okUserID + "/lift", "reason", `{"reason":5}`, "reason", "reason must be a string."},
	{http.MethodPost, "/api/v1/admin/users/" + okUserID + "/restrictions", "days", `{"days":"week"}`, "days", "days must be a whole number."},
	{http.MethodPost, "/api/v1/projects", "description", `{"description":"d","title":5}`, "title", "title must be a string."},
	{http.MethodPatch, "/api/v1/projects/" + okProjectID, "", `{"requiredSkills":"Go"}`, "requiredSkills", "requiredSkills must be a list."},
	{http.MethodPost, "/api/v1/projects/" + okProjectID + "/reports", "reason", `{"reason":5}`, "reason", "reason must be a string."},
	{http.MethodPost, "/api/v1/admin/project-reports/" + okUserID + "/resolve", "outcome", `{"outcome":5}`, "outcome", "outcome must be a string."},
	{http.MethodPost, "/api/v1/projects/" + okProjectID + "/join-requests", "", `{"message":5}`, "message", "message must be a string."},
	{http.MethodPost, "/api/v1/projects/" + okProjectID + "/join-requests/" + okJoinRequestID + "/accept", "role", `{"role":5}`, "role", "role must be a string."},
	{http.MethodPost, "/api/v1/projects/" + okProjectID + "/invitations", "role", `{"role":"viewer","userId":5}`, "userId", "userId must be a UUID."},
	{http.MethodPatch, "/api/v1/projects/" + okProjectID + "/members/" + okUserID, "role", `{"role":5}`, "role", "role must be a string."},
	{http.MethodPost, "/api/v1/projects/" + okProjectID + "/transfer", "userId", `{"userId":5}`, "userId", "userId must be a UUID."},
	{http.MethodPost, "/api/v1/projects/" + okProjectID + "/files", "kind", `{"kind":"text","path":5}`, "path", "path must be a string."},
	{http.MethodPatch, "/api/v1/projects/" + okProjectID + "/files/" + okProjectFileID, "path", `{"path":5}`, "path", "path must be a string."},
	{http.MethodPost, "/api/v1/projects/" + okProjectID + "/files/uploads", "path", `{"path":"a.png","size":"big"}`, "size", "size must be a whole number."},
	{http.MethodPut, "/api/v1/projects/" + okProjectID + "/files/" + okProjectFileID + "/content", "content", `{"content":"x","version":"2"}`, "version", "version must be a whole number."},
	// The sync server's internal operations (slice 22), sent with its token.
	{http.MethodPost, "/api/v1/internal/sync/tickets/redeem", "fileId", `{"fileId":"` + okProjectFileID + `","ticket":5}`, "ticket", "ticket must be a string."},
	{http.MethodPost, "/api/v1/internal/sync/access", "checks", `{"checks":5}`, "checks", "checks must be a list."},
	{http.MethodPut, "/api/v1/internal/sync/files/" + okProjectFileID + "/state", "baseVersion", `{"baseVersion":"1"}`, "baseVersion", "baseVersion must be a whole number."},
	{http.MethodPost, "/api/v1/internal/compile/claim", "workerId", `{"workerId":5}`, "workerId", "workerId must be a string."},
	{http.MethodPost, "/api/v1/internal/compile/jobs/" + okCompileID + "/finish", "bibtexLog", `{"outcome":"completed","durationMs":"1","log":"","bibtexLog":""}`, "durationMs", "durationMs must be a whole number."},
}

// Every operation with a JSON body goes through the same decoder and the same error shape.
func TestEveryJSONBodyGetsFieldErrors(t *testing.T) {
	router := testRouter()
	post := func(t *testing.T, method, path, body string) (int, map[string]any) {
		t.Helper()
		role := authz.RoleResearcher
		if strings.HasPrefix(path, "/api/v1/admin/") {
			role = authz.RoleAdmin
		}
		var rec *httptest.ResponseRecorder
		if strings.HasPrefix(path, internalPrefix) {
			token := testSyncToken
			if strings.Contains(path, "/internal/compile/") {
				token = testCompileToken
			}
			rec = sendInternal(t, router, method, path, body, token)
		} else {
			rec = sendAs(t, router, method, path, strings.NewReader(body), "application/json", string(role))
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil && rec.Body.Len() > 0 {
			t.Fatalf("body %q: %v", rec.Body, err)
		}
		return rec.Code, got
	}
	for _, r := range jsonBodyRoutes {
		name := r.method + " " + r.path
		t.Run(name+"/wrong type", func(t *testing.T) {
			code, got := post(t, r.method, r.path, r.wrong)
			if code != http.StatusBadRequest || got["code"] != "bad_request" || got["field"] != r.wrongField || got["message"] != r.message {
				t.Errorf("got %d %v; want field %s, message %q", code, got, r.wrongField, r.message)
			}
		})
		t.Run(name+"/malformed JSON", func(t *testing.T) {
			code, got := post(t, r.method, r.path, `{"`)
			if code != http.StatusBadRequest || got["code"] != "bad_request" || got["message"] != "The request body is not valid JSON." {
				t.Errorf("got %d %v", code, got)
			}
			if _, ok := got["field"]; ok {
				t.Errorf("a field in %v", got)
			}
		})
		if r.missing != "" {
			t.Run(name+"/missing required", func(t *testing.T) {
				code, got := post(t, r.method, r.path, `{}`)
				if code != http.StatusBadRequest || got["code"] != "bad_request" || got["field"] != r.missing || got["message"] != r.missing+" is required." {
					t.Errorf("got %d %v; want %s is required", code, got, r.missing)
				}
			})
		}
	}
}

// A new operation with a JSON body must join jsonBodyRoutes: the generated code reads each
// such body in one place.
func TestJSONBodyRoutesAreComplete(t *testing.T) {
	src, err := os.ReadFile("../../internal/api/gen/api.gen.go")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Count(string(src), "decodeJSONBody(r.Body, &body)")
	if got := len(jsonBodyRoutes); got != want {
		t.Errorf("jsonBodyRoutes has %d operations, the contract %d", got, want)
	}
}
