//go:build integration

// End-to-end tests of project membership (FR-11.1 to FR-11.6, FR-10.5, FR-9.2) through the
// real router, usecase, PostgreSQL store, notification module and Redis limits and caches.
// They reuse the environments of media_integration_test.go and the recommendation tests.
package main

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	recommendationredis "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/redis"
)

// ownProject creates an open project owned by owner and returns its ID and path.
func (e mediaEnv) ownProject(t *testing.T, owner, title string) (string, string) {
	t.Helper()
	code, p := e.json(t, owner, http.MethodPost, "/api/v1/projects", fmt.Sprintf(`{"title":%q,"description":"A study"}`, title))
	if code != http.StatusCreated {
		t.Fatalf("create project: %d %v", code, p)
	}
	id := p["id"].(string)
	return id, "/api/v1/projects/" + id
}

// notice is one notification as the list shows it.
type notice struct {
	typ, actor, subjectID, subjectTitle, link string
}

// notices lists userID's notifications, newest first.
func (e mediaEnv) notices(t *testing.T, userID string) []notice {
	t.Helper()
	code, body := e.json(t, userID, http.MethodGet, "/api/v1/notifications?limit=50", "")
	if code != http.StatusOK {
		t.Fatalf("notifications: %d %v", code, body)
	}
	var out []notice
	for _, raw := range body["items"].([]any) {
		it := raw.(map[string]any)
		n := notice{typ: it["type"].(string), link: it["link"].(string)}
		if a, ok := it["actor"].(map[string]any); ok {
			n.actor = a["userId"].(string)
		}
		if s, ok := it["subject"].(map[string]any); ok {
			n.subjectID, n.subjectTitle = s["id"].(string), s["title"].(string)
		}
		out = append(out, n)
	}
	return out
}

// hasNotice reports whether userID has a notification of typ from actor about project.
func (e mediaEnv) hasNotice(t *testing.T, userID, typ, actor, project string) bool {
	t.Helper()
	for _, n := range e.notices(t, userID) {
		if n.typ == typ && n.actor == actor && n.link == "/projects/"+project {
			return true
		}
	}
	return false
}

func itemsOf(body map[string]any) []map[string]any {
	var out []map[string]any
	for _, it := range body["items"].([]any) {
		out = append(out, it.(map[string]any))
	}
	return out
}

func (e mediaEnv) myRole(t *testing.T, userID, projectID string) string {
	t.Helper()
	code, body := e.json(t, userID, http.MethodGet, "/api/v1/me/projects?pageSize=50", "")
	if code != http.StatusOK {
		t.Fatalf("my projects: %d %v", code, body)
	}
	for _, it := range itemsOf(body) {
		if it["id"] == projectID {
			return it["myRole"].(string)
		}
	}
	return ""
}

// FR-11.1, FR-11.2, FR-11.6, FR-9.2: ask, list, decide, notify, and the member's role in
// their own list.
func TestJoinRequestsEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy := e.projectUser(t), e.projectUser(t), e.projectUser(t)
	w := testenv.Word(t)
	id, path := e.ownProject(t, ada, w+" graphs")

	code, r := e.json(t, bob, http.MethodPost, path+"/join-requests", `{"message":"  I study graphs.  "}`)
	if code != http.StatusCreated || r["status"] != "pending" || r["message"] != "I study graphs." || r["decidedAt"] != nil {
		t.Fatalf("ask: %d %v", code, r)
	}
	if pr := r["project"].(map[string]any); pr["id"] != id || pr["title"] != w+" graphs" || pr["owner"].(map[string]any)["userId"] != ada {
		t.Errorf("project = %v", pr)
	}
	req := r["id"].(string)
	ns := e.notices(t, ada)
	if len(ns) != 1 || ns[0].typ != "project_join_request" || ns[0].actor != bob || ns[0].subjectID != id || ns[0].subjectTitle != w+" graphs" || ns[0].link != "/projects/"+id {
		t.Errorf("ada's notices = %+v", ns)
	}
	code, body := e.json(t, bob, http.MethodPost, path+"/join-requests", `{}`)
	wantError(t, "ask twice", code, body, http.StatusConflict, "request_pending")
	code, body = e.json(t, cy, http.MethodPost, path+"/join-requests", fmt.Sprintf(`{"message":%q}`, strings.Repeat("x", 501)))
	if code != http.StatusBadRequest || body["field"] != "message" {
		t.Errorf("long message: %d %v", code, body)
	}
	code, body = e.json(t, ada, http.MethodPost, path+"/join-requests", `{}`)
	wantError(t, "the owner asks", code, body, http.StatusConflict, "already_member")

	// The sender's own list.
	code, body = e.json(t, bob, http.MethodGet, "/api/v1/me/join-requests", "")
	if code != http.StatusOK || body["total"] != float64(1) || itemsOf(body)[0]["status"] != "pending" {
		t.Errorf("bob's requests: %d %v", code, body)
	}

	// The owner's list; others are refused.
	code, body = e.json(t, cy, http.MethodGet, path+"/join-requests", "")
	wantError(t, "cy lists", code, body, http.StatusForbidden, "forbidden")
	code, body = e.json(t, ada, http.MethodGet, path+"/join-requests", "")
	if code != http.StatusOK || body["total"] != float64(1) || itemsOf(body)[0]["user"].(map[string]any)["userId"] != bob {
		t.Errorf("ada lists: %d %v", code, body)
	}

	// Deciding: owner only, editor or viewer, once.
	accept := path + "/join-requests/" + req + "/accept"
	code, body = e.json(t, cy, http.MethodPost, accept, `{"role":"editor"}`)
	wantError(t, "cy accepts", code, body, http.StatusForbidden, "forbidden")
	code, body = e.json(t, ada, http.MethodPost, accept, `{"role":"owner"}`)
	if code != http.StatusBadRequest || body["field"] != "role" {
		t.Errorf("accept as owner: %d %v", code, body)
	}
	code, body = e.json(t, ada, http.MethodPost, path+"/join-requests/nope/accept", `{"role":"editor"}`)
	wantError(t, "unknown request", code, body, http.StatusNotFound, "not_found")
	code, body = e.json(t, ada, http.MethodPost, accept, `{"role":"editor"}`)
	if code != http.StatusOK || body["role"] != "editor" || body["user"].(map[string]any)["userId"] != bob {
		t.Fatalf("accept: %d %v", code, body)
	}
	code, body = e.json(t, ada, http.MethodPost, accept, `{"role":"editor"}`)
	wantError(t, "accept twice", code, body, http.StatusConflict, "request_not_pending")
	code, body = e.json(t, ada, http.MethodPost, path+"/join-requests/"+req+"/reject", "")
	wantError(t, "reject after accept", code, body, http.StatusConflict, "request_not_pending")

	if !e.hasNotice(t, bob, "project_join_request_accepted", ada, id) {
		t.Error("bob was not told")
	}
	if len(e.notices(t, ada)) != 0 {
		t.Error("ada's notice of the request was not taken back")
	}
	// FR-11.6: the member's own list has the role.
	if role := e.myRole(t, bob, id); role != "editor" {
		t.Errorf("bob's role in his list = %q", role)
	}
	code, body = e.json(t, bob, http.MethodGet, "/api/v1/me/join-requests", "")
	if code != http.StatusOK || itemsOf(body)[0]["status"] != "accepted" || itemsOf(body)[0]["decidedAt"] == nil {
		t.Errorf("bob's requests after: %v", body)
	}
	code, body = e.json(t, bob, http.MethodPost, path+"/join-requests", `{}`)
	wantError(t, "a member asks", code, body, http.StatusConflict, "already_member")

	// Members: anyone who can see the project; the owner first.
	code, body = e.json(t, cy, http.MethodGet, path+"/members", "")
	if code != http.StatusOK || body["total"] != float64(2) {
		t.Fatalf("members: %d %v", code, body)
	}
	if ms := itemsOf(body); ms[0]["role"] != "owner" || ms[0]["user"].(map[string]any)["userId"] != ada || ms[1]["role"] != "editor" {
		t.Errorf("members = %v", ms)
	}

	// Reject: the sender is told (FR-9.2), and may ask again; cancel takes back the notice.
	_, r = e.json(t, cy, http.MethodPost, path+"/join-requests", `{}`)
	creq := r["id"].(string)
	if code, _ := e.json(t, ada, http.MethodPost, path+"/join-requests/"+creq+"/reject", ""); code != http.StatusNoContent {
		t.Fatalf("reject: %d", code)
	}
	if !e.hasNotice(t, cy, "project_join_request_rejected", ada, id) {
		t.Error("cy was not told of the rejection")
	}
	_, r = e.json(t, cy, http.MethodPost, path+"/join-requests", `{}`)
	creq = r["id"].(string)
	if !e.hasNotice(t, ada, "project_join_request", cy, id) {
		t.Fatal("no notice of cy's second request")
	}
	code, _ = e.json(t, bob, http.MethodDelete, path+"/join-requests/"+creq, "")
	if code != http.StatusNotFound {
		t.Errorf("bob cancelled cy's request: %d", code)
	}
	if code, _ := e.json(t, cy, http.MethodDelete, path+"/join-requests/"+creq, ""); code != http.StatusNoContent {
		t.Fatalf("cancel: %d", code)
	}
	if e.hasNotice(t, ada, "project_join_request", cy, id) {
		t.Error("the cancelled request's notice stays")
	}
	if code, _ := e.json(t, cy, http.MethodDelete, path+"/join-requests/"+creq, ""); code != http.StatusNotFound {
		t.Errorf("cancel twice: %d", code)
	}
}

// FR-11.3, FR-9.2: invite, list, answer and cancel.
func TestInvitationsEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy, dee := e.projectUser(t), e.projectUser(t), e.projectUser(t), e.projectUser(t)
	id, path := e.ownProject(t, ada, "Invited work")

	code, body := e.json(t, bob, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"viewer"}`, cy))
	wantError(t, "a non-member invites", code, body, http.StatusForbidden, "forbidden")
	code, body = e.json(t, ada, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"viewer"}`, ada))
	wantError(t, "invite yourself", code, body, http.StatusConflict, "already_member")
	code, body = e.json(t, ada, http.MethodPost, path+"/invitations", `{"userId":"00000000-0000-4000-8000-000000000000","role":"viewer"}`)
	wantError(t, "an unknown user", code, body, http.StatusNotFound, "user_not_found")

	code, inv := e.json(t, ada, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"editor"}`, cy))
	if code != http.StatusCreated || inv["role"] != "editor" || inv["status"] != "pending" || inv["user"].(map[string]any)["userId"] != cy {
		t.Fatalf("invite: %d %v", code, inv)
	}
	invID := inv["id"].(string)
	if !e.hasNotice(t, cy, "project_invitation", ada, id) {
		t.Error("cy was not told")
	}
	code, body = e.json(t, ada, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"viewer"}`, cy))
	wantError(t, "invite twice", code, body, http.StatusConflict, "already_invited")
	code, body = e.json(t, cy, http.MethodPost, path+"/join-requests", `{}`)
	wantError(t, "ask while invited", code, body, http.StatusConflict, "invitation_pending")
	// A user with a pending request: accept the request instead.
	e.json(t, bob, http.MethodPost, path+"/join-requests", `{}`)
	code, body = e.json(t, ada, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"viewer"}`, bob))
	wantError(t, "invite a user who asked", code, body, http.StatusConflict, "request_pending")

	code, body = e.json(t, cy, http.MethodGet, "/api/v1/me/invitations", "")
	if code != http.StatusOK || body["total"] != float64(1) || itemsOf(body)[0]["role"] != "editor" || itemsOf(body)[0]["project"].(map[string]any)["title"] != "Invited work" {
		t.Errorf("cy's invitations: %d %v", code, body)
	}
	code, body = e.json(t, ada, http.MethodGet, path+"/invitations", "")
	if code != http.StatusOK || body["total"] != float64(1) {
		t.Errorf("ada's invitations: %d %v", code, body)
	}
	code, body = e.json(t, cy, http.MethodGet, path+"/invitations", "")
	wantError(t, "cy lists the project's invitations", code, body, http.StatusForbidden, "forbidden")

	// Only the invitee answers; once.
	code, _ = e.json(t, dee, http.MethodPost, path+"/invitations/"+invID+"/accept", "")
	if code != http.StatusNotFound {
		t.Errorf("dee accepted cy's invitation: %d", code)
	}
	code, body = e.json(t, cy, http.MethodPost, path+"/invitations/"+invID+"/accept", "")
	if code != http.StatusOK || body["role"] != "editor" {
		t.Fatalf("accept: %d %v", code, body)
	}
	code, body = e.json(t, cy, http.MethodPost, path+"/invitations/"+invID+"/accept", "")
	wantError(t, "accept twice", code, body, http.StatusConflict, "invitation_not_pending")
	if !e.hasNotice(t, ada, "project_invitation_accepted", cy, id) {
		t.Error("ada was not told of the acceptance")
	}
	if e.hasNotice(t, cy, "project_invitation", ada, id) {
		t.Error("cy's notice of the invitation stays")
	}
	if role := e.myRole(t, cy, id); role != "editor" {
		t.Errorf("cy's role = %q", role)
	}

	// Rejecting tells the owner (FR-9.2); then a new invitation may be sent and cancelled.
	_, inv = e.json(t, ada, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"viewer"}`, dee))
	if code, _ := e.json(t, dee, http.MethodPost, path+"/invitations/"+inv["id"].(string)+"/reject", ""); code != http.StatusNoContent {
		t.Fatalf("reject: %d", code)
	}
	if !e.hasNotice(t, ada, "project_invitation_rejected", dee, id) {
		t.Error("ada was not told of the rejection")
	}
	_, inv = e.json(t, ada, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"viewer"}`, dee))
	invID = inv["id"].(string)
	code, body = e.json(t, cy, http.MethodDelete, path+"/invitations/"+invID, "")
	wantError(t, "an editor cancels", code, body, http.StatusForbidden, "forbidden")
	if code, _ := e.json(t, ada, http.MethodDelete, path+"/invitations/"+invID, ""); code != http.StatusNoContent {
		t.Fatalf("cancel: %d", code)
	}
	if e.hasNotice(t, dee, "project_invitation", ada, id) {
		t.Error("the cancelled invitation's notice stays")
	}
	if code, body := e.json(t, dee, http.MethodGet, "/api/v1/me/invitations", ""); body["total"] != float64(0) {
		t.Errorf("dee's invitations after the cancel: %d %v", code, body)
	}
}

// FR-11.4, FR-11.5, FR-10.5: roles, removal, leaving and transfer; exactly one owner.
func TestMembersEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy, dee := e.projectUser(t), e.projectUser(t), e.projectUser(t), e.projectUser(t)
	id, path := e.ownProject(t, ada, "Shared work")
	join := func(user, role string) {
		t.Helper()
		_, inv := e.json(t, ada, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":%q}`, user, role))
		if code, body := e.json(t, user, http.MethodPost, path+"/invitations/"+inv["id"].(string)+"/accept", ""); code != http.StatusOK {
			t.Fatalf("join: %d %v", code, body)
		}
	}
	join(bob, "editor")
	join(cy, "viewer")

	// Roles (FR-11.4).
	code, body := e.json(t, bob, http.MethodPatch, path+"/members/"+cy, `{"role":"editor"}`)
	wantError(t, "an editor changes a role", code, body, http.StatusForbidden, "forbidden")
	code, body = e.json(t, ada, http.MethodPatch, path+"/members/"+ada, `{"role":"editor"}`)
	wantError(t, "the owner's role", code, body, http.StatusConflict, "use_transfer")
	code, body = e.json(t, ada, http.MethodPatch, path+"/members/"+dee, `{"role":"editor"}`)
	wantError(t, "a non-member's role", code, body, http.StatusNotFound, "not_found")
	code, body = e.json(t, ada, http.MethodPatch, path+"/members/"+cy, `{"role":"editor"}`)
	if code != http.StatusOK || body["role"] != "editor" {
		t.Fatalf("change role: %d %v", code, body)
	}
	if !e.hasNotice(t, cy, "project_member_role_changed", ada, id) || e.myRole(t, cy, id) != "editor" {
		t.Error("cy's role change was not told or not kept")
	}

	// Removal (FR-11.4).
	code, body = e.json(t, ada, http.MethodDelete, path+"/members/"+ada, "")
	wantError(t, "remove the owner", code, body, http.StatusConflict, "cannot_remove_owner")
	code, body = e.json(t, bob, http.MethodDelete, path+"/members/"+cy, "")
	wantError(t, "an editor removes", code, body, http.StatusForbidden, "forbidden")
	if code, _ := e.json(t, ada, http.MethodDelete, path+"/members/"+cy, ""); code != http.StatusNoContent {
		t.Fatalf("remove: %d", code)
	}
	if !e.hasNotice(t, cy, "project_member_removed", ada, id) || e.myRole(t, cy, id) != "" {
		t.Error("cy's removal was not told or not done")
	}

	// Leaving (FR-11.5).
	code, body = e.json(t, ada, http.MethodPost, path+"/leave", "")
	wantError(t, "the owner leaves", code, body, http.StatusConflict, "owner_must_transfer")
	code, body = e.json(t, cy, http.MethodPost, path+"/leave", "")
	wantError(t, "a non-member leaves", code, body, http.StatusNotFound, "not_found")
	join(dee, "viewer")
	before := len(e.notices(t, ada))
	if code, _ := e.json(t, dee, http.MethodPost, path+"/leave", ""); code != http.StatusNoContent {
		t.Fatalf("leave: %d", code)
	}
	if e.myRole(t, dee, id) != "" || len(e.notices(t, ada)) != before {
		t.Error("dee is still a member, or someone was told")
	}

	// Transfer (FR-10.5).
	code, body = e.json(t, bob, http.MethodPost, path+"/transfer", fmt.Sprintf(`{"userId":%q}`, bob))
	wantError(t, "an editor transfers", code, body, http.StatusForbidden, "forbidden")
	code, body = e.json(t, ada, http.MethodPost, path+"/transfer", fmt.Sprintf(`{"userId":%q}`, dee))
	wantError(t, "to a non-member", code, body, http.StatusConflict, "not_a_member")
	code, body = e.json(t, ada, http.MethodPost, path+"/transfer", fmt.Sprintf(`{"userId":%q}`, ada))
	wantError(t, "to the owner", code, body, http.StatusConflict, "already_owner")
	code, body = e.json(t, ada, http.MethodPost, path+"/transfer", fmt.Sprintf(`{"userId":%q}`, bob))
	if code != http.StatusOK || body["myRole"] != "editor" || body["owner"].(map[string]any)["userId"] != bob {
		t.Fatalf("transfer: %d %v", code, body)
	}
	if n := e.countRows(t, "SELECT count(*) FROM project_members WHERE project_id = $1 AND role = 'owner'", id); n != 1 {
		t.Errorf("%d owners after the transfer", n)
	}
	if e.myRole(t, bob, id) != "owner" || e.myRole(t, ada, id) != "editor" {
		t.Error("the roles were not swapped")
	}
	if !e.hasNotice(t, bob, "project_ownership_transferred", ada, id) {
		t.Error("bob was not told")
	}
	code, body = e.json(t, ada, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"viewer"}`, dee))
	wantError(t, "the old owner invites", code, body, http.StatusForbidden, "forbidden")
	if code, body := e.json(t, ada, http.MethodPost, path+"/leave", ""); code != http.StatusNoContent {
		t.Errorf("the old owner leaves: %d %v", code, body)
	}
	// A request to the new owner notifies the new owner.
	e.json(t, dee, http.MethodPost, path+"/join-requests", `{}`)
	if !e.hasNotice(t, bob, "project_join_request", dee, id) {
		t.Error("the new owner was not told of a request")
	}
}

// Closed and archived projects take no new requests or invitations; pending ones stay
// until handled, and can be accepted.
func TestMembershipClosedProjectEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy, dee := e.projectUser(t), e.projectUser(t), e.projectUser(t), e.projectUser(t)
	_, path := e.ownProject(t, ada, "Closing soon")
	_, r := e.json(t, bob, http.MethodPost, path+"/join-requests", `{}`)
	_, inv := e.json(t, ada, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"viewer"}`, cy))
	if code, _ := e.json(t, ada, http.MethodPatch, path, `{"status":"closed"}`); code != http.StatusOK {
		t.Fatal("close")
	}
	code, body := e.json(t, dee, http.MethodPost, path+"/join-requests", `{}`)
	wantError(t, "ask a closed project", code, body, http.StatusConflict, "project_not_open")
	code, body = e.json(t, ada, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"viewer"}`, dee))
	wantError(t, "invite to a closed project", code, body, http.StatusConflict, "project_not_open")
	if code, body := e.json(t, ada, http.MethodPost, path+"/join-requests/"+r["id"].(string)+"/accept", `{"role":"viewer"}`); code != http.StatusOK {
		t.Errorf("accept a pending request after closing: %d %v", code, body)
	}
	if code, _ := e.json(t, ada, http.MethodPatch, path, `{"status":"archived"}`); code != http.StatusOK {
		t.Fatal("archive")
	}
	code, body = e.json(t, dee, http.MethodPost, path+"/join-requests", `{}`)
	wantError(t, "ask an archived project", code, body, http.StatusNotFound, "not_found")
	if code, body := e.json(t, cy, http.MethodPost, path+"/invitations/"+inv["id"].(string)+"/accept", ""); code != http.StatusOK {
		t.Errorf("accept a pending invitation after archiving: %d %v", code, body)
	}
}

// Blocks (FR-16.9): no request or invitation either way; a new block deletes the pending ones
// between the two and nobody is told; memberships stay (decisions.md 2026-11-17).
func TestMembershipBlocksEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy, dee := e.projectUser(t), e.projectUser(t), e.projectUser(t), e.projectUser(t)
	id, path := e.ownProject(t, ada, "Blocked work")

	e.block(t, bob, ada)
	code, body := e.json(t, bob, http.MethodPost, path+"/join-requests", `{}`)
	wantError(t, "ask after blocking the owner", code, body, http.StatusNotFound, "not_found")
	code, body = e.json(t, ada, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"viewer"}`, bob))
	wantError(t, "invite a user who blocked you", code, body, http.StatusNotFound, "user_not_found")
	e.unblock(t, bob, ada)
	e.block(t, ada, bob)
	code, body = e.json(t, bob, http.MethodPost, path+"/join-requests", `{}`)
	wantError(t, "ask a blocking owner", code, body, http.StatusNotFound, "not_found")
	e.unblock(t, ada, bob)

	// Pending rows, then a block.
	e.json(t, bob, http.MethodPost, path+"/join-requests", `{}`)
	_, inv := e.json(t, ada, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"viewer"}`, cy))
	_, other := e.json(t, ada, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"viewer"}`, dee))
	e.json(t, dee, http.MethodPost, path+"/invitations/"+other["id"].(string)+"/accept", "")
	bobsID, _ := e.ownProject(t, bob, "Bob's work")
	e.json(t, bob, http.MethodPost, "/api/v1/projects/"+bobsID+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"viewer"}`, ada))
	e.block(t, ada, bob)
	if n := e.countRows(t, "SELECT count(*) FROM project_join_requests WHERE status = 'pending' AND user_id IN ($1, $2)", bob, ada); n != 0 {
		t.Errorf("%d pending rows between them left", n)
	}
	if n := e.countRows(t, "SELECT count(*) FROM project_join_requests WHERE id = $1", inv["id"]); n != 1 {
		t.Error("a third user's invitation was withdrawn")
	}
	if e.hasNotice(t, ada, "project_join_request", bob, id) || e.hasNotice(t, ada, "project_invitation", bob, bobsID) {
		t.Error("a notice between them stays")
	}
	if code, body := e.json(t, bob, http.MethodGet, "/api/v1/me/join-requests", ""); body["total"] != float64(0) {
		t.Errorf("bob's requests after the block: %d %v", code, body)
	}
	// A member blocked by the owner stays a member.
	e.block(t, ada, dee)
	if e.myRole(t, dee, id) != "viewer" {
		t.Error("a block removed a member")
	}
}

// Suspended users (decisions.md 2026-11-17): a suspended owner's project takes no requests;
// a suspended owner cannot manage; a suspended member can still leave, a suspended sender
// cancel and a suspended invitee reject.
func TestMembershipSuspendedEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy, dee := e.projectUser(t), e.projectUser(t), e.projectUser(t), e.projectUser(t)
	_, path := e.ownProject(t, ada, "Suspended work")
	_, r := e.json(t, bob, http.MethodPost, path+"/join-requests", `{}`)
	_, inv := e.json(t, ada, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"viewer"}`, cy))
	_, inv2 := e.json(t, ada, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"viewer"}`, dee))
	e.json(t, dee, http.MethodPost, path+"/invitations/"+inv2["id"].(string)+"/accept", "")
	suspend := func(id string) {
		t.Helper()
		if _, err := e.pool.Exec(t.Context(), "UPDATE users SET suspended_at = now(), suspension_reason = 'Spam messages' WHERE id = $1", id); err != nil {
			t.Fatal(err)
		}
	}

	suspend(ada)
	other := e.projectUser(t)
	code, body := e.json(t, other, http.MethodPost, path+"/join-requests", `{}`)
	wantError(t, "ask a suspended owner's project", code, body, http.StatusNotFound, "not_found")
	code, body = e.json(t, ada, http.MethodPost, path+"/join-requests/"+r["id"].(string)+"/accept", `{"role":"viewer"}`)
	wantError(t, "a suspended owner accepts", code, body, http.StatusForbidden, "account_suspended")
	code, body = e.json(t, cy, http.MethodPost, path+"/invitations/"+inv["id"].(string)+"/accept", "")
	wantError(t, "accept a suspended owner's invitation", code, body, http.StatusNotFound, "not_found")
	if code, body := e.json(t, cy, http.MethodGet, "/api/v1/me/invitations", ""); body["total"] != float64(0) {
		t.Errorf("a suspended owner's invitation is listed: %d %v", code, body)
	}
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET suspended_at = NULL, suspension_reason = NULL WHERE id = $1", ada); err != nil {
		t.Fatal(err)
	}

	for _, u := range []string{bob, cy, dee} {
		suspend(u)
	}
	code, body = e.json(t, cy, http.MethodPost, path+"/invitations/"+inv["id"].(string)+"/accept", "")
	wantError(t, "a suspended invitee accepts", code, body, http.StatusForbidden, "account_suspended")
	if code, body := e.json(t, cy, http.MethodPost, path+"/invitations/"+inv["id"].(string)+"/reject", ""); code != http.StatusNoContent {
		t.Errorf("a suspended invitee rejects: %d %v", code, body)
	}
	if code, body := e.json(t, bob, http.MethodDelete, path+"/join-requests/"+r["id"].(string), ""); code != http.StatusNoContent {
		t.Errorf("a suspended sender cancels: %d %v", code, body)
	}
	if code, body := e.json(t, dee, http.MethodPost, path+"/leave", ""); code != http.StatusNoContent {
		t.Errorf("a suspended member leaves: %d %v", code, body)
	}
	code, body = e.json(t, bob, http.MethodPost, path+"/join-requests", `{}`)
	wantError(t, "a suspended user asks", code, body, http.StatusForbidden, "account_suspended")
}

// 20 join requests and 20 invitations a day; only stored ones count; 429 with Retry-After.
func TestMembershipRateLimitsEndToEnd(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, cy := e.projectUser(t), e.projectUser(t), e.projectUser(t)
	_, path := e.ownProject(t, ada, "Limited work")
	ctx := t.Context()

	if err := e.rdb.Set(ctx, "project:join:"+bob, 20, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	rec := e.sendRole(t, bob, "researcher", true, http.MethodPost, path+"/join-requests", `{}`)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("request over the limit: %d %s", rec.Code, rec.Body)
	}
	if err := e.rdb.Set(ctx, "project:invite:"+ada, 20, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	rec = e.sendRole(t, ada, "researcher", true, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"viewer"}`, cy))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("invitation over the limit: %d %s", rec.Code, rec.Body)
	}

	// Only stored ones count: a refused one does not.
	e.json(t, cy, http.MethodPost, path+"/join-requests", `{}`)
	e.json(t, cy, http.MethodPost, path+"/join-requests", `{}`) // 409
	if n, err := e.rdb.Get(ctx, "project:join:"+cy).Int(); err != nil || n != 1 {
		t.Errorf("cy's count = %d, %v", n, err)
	}
}

// Caches (AI-2.1, AI-3.1, AI-2.3): every request and membership change clears both users'
// lists and the project's researcher list after the commit, and the lists change at once.
func TestMembershipCachesEndToEnd(t *testing.T) {
	e := newRecEnv(t)
	ada, bob := e.projectUser(t), e.projectUser(t)
	e.addSkill(t, ada, "Python")
	p := e.newProject(t, bob, "Flood maps", "Mapping floods", "Python")
	path := "/api/v1/projects/" + p
	e.embedWorker.Tick(t.Context())
	researchers := path + "/recommended-researchers"
	keys := []string{recommendationredis.Key(ada), recommendationredis.ProjectsKey(ada), recommendationredis.ResearchersKey(p)}
	fill := func() {
		t.Helper()
		if code, body := e.json(t, ada, http.MethodGet, "/api/v1/recommendations/people", ""); code != http.StatusOK {
			t.Fatalf("people: %d %v", code, body)
		}
		e.projectRecs(t, ada)
		if code, _ := e.json(t, bob, http.MethodGet, researchers, ""); code != http.StatusOK {
			t.Fatal("researcher list")
		}
		for _, k := range keys {
			if !e.exists(t, k) {
				t.Fatalf("%s was not kept", k)
			}
		}
	}
	cleared := func(what string) {
		t.Helper()
		for _, k := range keys {
			if e.exists(t, k) {
				t.Errorf("%s: %s kept", what, k)
			}
		}
	}
	recommended := func() bool {
		t.Helper()
		return slices.Contains(recProjectIDs(e.projectRecs(t, ada)), p)
	}
	researcherIDs := func() []string {
		t.Helper()
		_, body := e.json(t, bob, http.MethodGet, researchers, "")
		return recIDs(body)
	}

	fill()
	if !recommended() || !slices.Contains(researcherIDs(), ada) {
		t.Fatal("the project or the researcher is not recommended to start with")
	}
	fill()
	_, r := e.json(t, ada, http.MethodPost, path+"/join-requests", `{}`)
	cleared("a request")
	if recommended() {
		t.Error("a project ada asked to join is still recommended")
	}
	fill()
	e.json(t, ada, http.MethodDelete, path+"/join-requests/"+r["id"].(string), "")
	cleared("a cancel")
	if !recommended() {
		t.Error("a cancelled request still hides the project")
	}
	fill()
	_, r = e.json(t, ada, http.MethodPost, path+"/join-requests", `{}`)
	fill()
	e.json(t, bob, http.MethodPost, path+"/join-requests/"+r["id"].(string)+"/accept", `{"role":"viewer"}`)
	cleared("an accept")
	if recommended() || slices.Contains(researcherIDs(), ada) {
		t.Error("a member is still recommended, or the project to them")
	}
	fill()
	gen := e.generation(t)
	e.json(t, bob, http.MethodPost, path+"/transfer", fmt.Sprintf(`{"userId":%q}`, ada))
	cleared("a transfer")
	if e.generation(t) != gen+1 {
		t.Error("a transfer did not renew the project lists")
	}
	e.json(t, ada, http.MethodPost, path+"/transfer", fmt.Sprintf(`{"userId":%q}`, bob))
	fill()
	// After the transfer back ada is an editor.
	e.json(t, bob, http.MethodPatch, path+"/members/"+ada, `{"role":"viewer"}`)
	cleared("a role change")
	fill()
	e.json(t, ada, http.MethodPost, path+"/leave", "")
	cleared("leaving")
	if !recommended() || !slices.Contains(researcherIDs(), ada) {
		t.Error("after leaving, the project or the researcher is not recommended again")
	}
	fill()
	_, inv := e.json(t, bob, http.MethodPost, path+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"viewer"}`, ada))
	cleared("an invitation")
	fill()
	e.json(t, ada, http.MethodPost, path+"/invitations/"+inv["id"].(string)+"/accept", "")
	cleared("accepting an invitation")
	fill()
	e.json(t, bob, http.MethodDelete, path+"/members/"+ada, "")
	cleared("a removal")
}

// Account deletion (FR-2.8, FR-2.9) is unchanged: an owner of a shared project is refused; a
// member's rows go with the account; a project with no other member goes with its owner,
// pending requests included.
func TestMembershipAccountDeletionEndToEnd(t *testing.T) {
	e := newModEnv(t)
	ada, bob, cy, dee := e.projectUser(t), e.projectUser(t), e.projectUser(t), e.projectUser(t)
	shared, sharedPath := e.ownProject(t, ada, "Shared")
	_, inv := e.json(t, ada, http.MethodPost, sharedPath+"/invitations", fmt.Sprintf(`{"userId":%q,"role":"editor"}`, bob))
	e.json(t, bob, http.MethodPost, sharedPath+"/invitations/"+inv["id"].(string)+"/accept", "")
	solo, soloPath := e.ownProject(t, dee, "Solo")
	e.json(t, cy, http.MethodPost, soloPath+"/join-requests", `{}`)
	e.json(t, bob, http.MethodPost, soloPath+"/join-requests", `{}`)

	e.withPassword(t, ada)
	code, body := e.call(t, ada, http.MethodPost, "/api/v1/account/delete", fmt.Sprintf(`{"password":%q}`, modPassword))
	wantError(t, "delete an owner of a shared project", code, body, http.StatusConflict, "owns_shared_project")

	e.withPassword(t, bob)
	if code, body := e.call(t, bob, http.MethodPost, "/api/v1/account/delete", fmt.Sprintf(`{"password":%q}`, modPassword)); code != http.StatusNoContent {
		t.Fatalf("delete a member: %d %v", code, body)
	}
	if n := e.countRows(t, "SELECT count(*) FROM project_members WHERE project_id = $1", shared); n != 1 {
		t.Errorf("%d members left, want the owner", n)
	}
	if n := e.countRows(t, "SELECT count(*) FROM project_join_requests WHERE user_id = $1", bob); n != 0 {
		t.Error("the deleted member's requests stay")
	}
	if e.hasNotice(t, ada, "project_invitation_accepted", bob, shared) || e.hasNotice(t, dee, "project_join_request", bob, solo) {
		t.Error("a notice from the deleted user stays")
	}

	e.withPassword(t, dee)
	if code, body := e.call(t, dee, http.MethodPost, "/api/v1/account/delete", fmt.Sprintf(`{"password":%q}`, modPassword)); code != http.StatusNoContent {
		t.Fatalf("delete a solo owner: %d %v", code, body)
	}
	if n := e.countRows(t, "SELECT count(*) FROM projects WHERE id = $1", solo); n != 0 {
		t.Error("the solo project stays")
	}
	if code, body := e.json(t, cy, http.MethodGet, "/api/v1/me/join-requests", ""); body["total"] != float64(0) {
		t.Errorf("cy's request to the deleted project: %d %v", code, body)
	}
}
