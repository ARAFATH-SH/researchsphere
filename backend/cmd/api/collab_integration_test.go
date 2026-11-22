//go:build integration

// End-to-end tests of the API's half of real-time editing (slice 22, docs/sync.md): the
// members' ticket, the sync server's internal operations on their own listener, the REST rule
// while a file is live and the events the sync server hears, through the real router,
// PostgreSQL and Redis.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	moderationdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	projectredis "github.com/ARAFATH-SH/researchsphere/backend/internal/project/adapter/redis"
)

// internalJSON calls an internal operation as the sync server does: on the internal listener
// with its token.
func (e mediaEnv) internalJSON(t *testing.T, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec := sendInternal(t, e.internal, method, path, body, testSyncToken)
	if rec.Body.Len() == 0 {
		return rec.Code, nil
	}
	return rec.Code, decodeBody(t, rec.Body.Bytes())
}

// collabProject makes a project owned by a new user with an editor and a viewer, and returns
// them, the project ID and its main.tex's ID.
func (e mediaEnv) collabProject(t *testing.T) (owner, editor, viewer, projectID, mainID string) {
	t.Helper()
	owner, editor, viewer = e.projectUser(t), e.projectUser(t), e.projectUser(t)
	projectID, path := e.ownProject(t, owner, testenv.Word(t)+" live")
	e.member(t, projectID, editor, "editor")
	e.member(t, projectID, viewer, "viewer")
	_, files, _ := e.tree(t, owner, path)
	return owner, editor, viewer, projectID, files["main.tex"]["id"].(string)
}

// randomUUID is a UUID that names nothing.
func (e mediaEnv) randomUUID(t *testing.T) string {
	t.Helper()
	var id string
	if err := e.pool.QueryRow(t.Context(), "SELECT gen_random_uuid()::text").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func ticketPath(projectID, fileID string) string {
	return "/api/v1/projects/" + projectID + "/files/" + fileID + "/collab-ticket"
}

// FR-12.7: a member's ticket opens the file once, within 30 s, with the role read when it is
// redeemed; non-members and administrators get nothing.
func TestCollabTickets(t *testing.T) {
	e := newMediaEnv(t)
	owner, editor, viewer, pid, mainID := e.collabProject(t)
	stranger, admin := e.projectUser(t), e.projectUser(t)
	e.rename(t, editor, "Grace Editor")

	rec := e.sendRole(t, editor, "researcher", true, http.MethodPost, ticketPath(pid, mainID), "")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("ticket: %d %s (Cache-Control %q)", rec.Code, rec.Body, rec.Header().Get("Cache-Control"))
	}
	tk := decodeBody(t, rec.Body.Bytes())
	if tk["role"] != "editor" || tk["displayName"] != "Grace Editor" || tk["epoch"] != nil {
		t.Errorf("ticket = %v", tk)
	}
	if exp, err := time.Parse(time.RFC3339Nano, tk["expiresAt"].(string)); err != nil || time.Until(exp) > 31*time.Second || time.Until(exp) < 20*time.Second {
		t.Errorf("expiresAt = %v (%v)", tk["expiresAt"], err)
	}
	ticket := tk["ticket"].(string)
	// Redis holds only the ticket's hash.
	keys, err := e.rdb.Keys(t.Context(), "sync:ticket:*").Result()
	if err != nil || len(keys) != 1 || strings.Contains(keys[0], ticket) {
		t.Errorf("ticket keys = %v (%v)", keys, err)
	}
	if ttl := e.rdb.TTL(t.Context(), keys[0]).Val(); ttl <= 0 || ttl > 30*time.Second {
		t.Errorf("ticket TTL = %v", ttl)
	}

	redeem := func(ticket, fileID string) (int, map[string]any) {
		return e.internalJSON(t, http.MethodPost, "/api/v1/internal/sync/tickets/redeem",
			`{"ticket":"`+ticket+`","fileId":"`+fileID+`"}`)
	}
	code, s := redeem(ticket, mainID)
	if code != http.StatusOK || s["userId"] != editor || s["projectId"] != pid || s["fileId"] != mainID || s["role"] != "editor" || s["displayName"] != "Grace Editor" {
		t.Fatalf("redeem: %d %v", code, s)
	}
	code, s = redeem(ticket, mainID)
	wantError(t, "second redeem", code, s, http.StatusNotFound, "not_found")

	// A viewer's ticket says viewer: the sync server makes the connection read-only.
	code, vt := e.json(t, viewer, http.MethodPost, ticketPath(pid, mainID), "")
	if code != http.StatusOK || vt["role"] != "viewer" {
		t.Errorf("viewer ticket: %d %v", code, vt)
	}
	// A ticket for another file is spent by the attempt.
	_, other := e.json(t, owner, http.MethodPost, "/api/v1/projects/"+pid+"/files", `{"path":"b.tex","kind":"text","content":"b"}`)
	code, s = redeem(vt["ticket"].(string), other["id"].(string))
	wantError(t, "wrong file", code, s, http.StatusNotFound, "not_found")
	code, s = redeem(vt["ticket"].(string), mainID)
	wantError(t, "after the wrong file", code, s, http.StatusNotFound, "not_found")

	// Removed between issue and redeem: refused.
	_, et := e.json(t, editor, http.MethodPost, ticketPath(pid, mainID), "")
	if code, body := e.json(t, owner, http.MethodDelete, "/api/v1/projects/"+pid+"/members/"+editor, ""); code != http.StatusNoContent {
		t.Fatalf("remove: %d %v", code, body)
	}
	code, s = redeem(et["ticket"].(string), mainID)
	wantError(t, "removed member", code, s, http.StatusNotFound, "not_found")

	// Expired: the key is gone after its TTL (shortened here rather than waiting 30 s).
	_, ot := e.json(t, owner, http.MethodPost, ticketPath(pid, mainID), "")
	keys, _ = e.rdb.Keys(t.Context(), "sync:ticket:*").Result()
	for _, k := range keys {
		e.rdb.PExpire(t.Context(), k, time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	code, s = redeem(ot["ticket"].(string), mainID)
	wantError(t, "expired", code, s, http.StatusNotFound, "not_found")

	code, body := e.json(t, stranger, http.MethodPost, ticketPath(pid, mainID), "")
	wantError(t, "non-member", code, body, http.StatusNotFound, "not_found")
	rec = e.sendRole(t, admin, "admin", true, http.MethodPost, ticketPath(pid, mainID), "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("administrator: %d %s", rec.Code, rec.Body)
	}
	_, folder := e.json(t, owner, http.MethodPost, "/api/v1/projects/"+pid+"/files", `{"path":"dir","kind":"folder"}`)
	code, body = e.json(t, owner, http.MethodPost, ticketPath(pid, folder["id"].(string)), "")
	wantError(t, "folder", code, body, http.StatusConflict, "not_a_text_file")
	if body["message"] != "Only text files can be edited together." {
		t.Errorf("folder message: %v", body["message"])
	}
}

// The internal operations are on the internal listener only, and only with the token.
func TestInternalListener(t *testing.T) {
	e := newMediaEnv(t)
	_, _, _, _, mainID := e.collabProject(t)
	path := "/api/v1/internal/sync/files/" + mainID

	if rec := sendInternal(t, e.router, http.MethodGet, path, "", testSyncToken); rec.Code != http.StatusNotFound {
		t.Errorf("public port: %d %s", rec.Code, rec.Body)
	}
	if rec := sendInternal(t, e.internal, http.MethodGet, path, "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d", rec.Code)
	}
	if rec := sendInternal(t, e.internal, http.MethodGet, path, "", "wrong-"+testSyncToken); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: %d", rec.Code)
	}
	if rec := sendInternal(t, e.internal, http.MethodGet, "/api/v1/health", "", testSyncToken); rec.Code != http.StatusNotFound {
		t.Errorf("public route on the internal listener: %d", rec.Code)
	}
	if code, f := e.internalJSON(t, http.MethodGet, path, ""); code != http.StatusOK || f["ydocState"] != nil || f["ydocEpoch"] != nil || f["version"] != 1.0 {
		t.Errorf("load: %d %v", code, f)
	}
}

// NFR-10.1: the sync server stores state, epoch, text, version and author in one write, only
// from the version it loaded; access checks read the role now (FR-12.7).
func TestSyncStoreAndAccess(t *testing.T) {
	e := newMediaEnv(t)
	owner, editor, viewer, pid, mainID := e.collabProject(t)
	stranger := e.projectUser(t)
	path := "/api/v1/internal/sync/files/" + mainID
	state := base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4})
	const epoch = "0193a7d4-0000-7000-8000-000000000001"
	store := func(base int, content, by string) (int, map[string]any) {
		b, _ := json.Marshal(map[string]any{"ydocState": state, "ydocEpoch": epoch, "content": content, "baseVersion": base, "updatedBy": by})
		return e.internalJSON(t, http.MethodPut, path+"/state", string(b))
	}

	code, body := store(1, "live text", editor)
	if code != http.StatusOK || body["version"] != 2.0 {
		t.Fatalf("store: %d %v", code, body)
	}
	var (
		gotState   []byte
		gotEpoch   string
		gotContent string
		gotVersion int
		gotBy      string
		gotSize    int64
	)
	if err := e.pool.QueryRow(t.Context(), `SELECT ydoc_state, ydoc_epoch::text, content, version, updated_by::text, size_bytes
		FROM project_files WHERE id = $1`, mainID).Scan(&gotState, &gotEpoch, &gotContent, &gotVersion, &gotBy, &gotSize); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(gotState, []byte{1, 2, 3, 4}) || gotEpoch != epoch || gotContent != "live text" || gotVersion != 2 || gotBy != editor || gotSize != 9 {
		t.Errorf("row = %v %q %q %d %q %d", gotState, gotEpoch, gotContent, gotVersion, gotBy, gotSize)
	}
	// REST readers see the text the sync server stored, and the ticket names the epoch.
	if code, f := e.json(t, viewer, http.MethodGet, "/api/v1/projects/"+pid+"/files/"+mainID, ""); code != http.StatusOK || f["content"] != "live text" {
		t.Errorf("REST read: %d %v", code, f)
	}
	if _, tk := e.json(t, owner, http.MethodPost, ticketPath(pid, mainID), ""); tk["epoch"] != epoch {
		t.Errorf("ticket epoch = %v", tk["epoch"])
	}
	code, body = store(1, "stale", editor)
	wantError(t, "stale base", code, body, http.StatusConflict, "version_conflict")
	code, body = store(2, strings.Repeat("x", 1<<20+1), editor)
	wantError(t, "too long", code, body, http.StatusRequestEntityTooLarge, "file_too_large")
	// A last editor whose account is gone is stored as nobody.
	if code, body := store(2, "by nobody", e.randomUUID(t)); code != http.StatusOK {
		t.Errorf("gone author: %d %v", code, body)
	}
	code, body = e.internalJSON(t, http.MethodPut, "/api/v1/internal/sync/files/"+e.randomUUID(t)+"/state",
		`{"ydocState":"`+state+`","ydocEpoch":"`+epoch+`","content":"x","baseVersion":1}`)
	wantError(t, "unknown file", code, body, http.StatusNotFound, "not_found")

	code, body = e.internalJSON(t, http.MethodPost, "/api/v1/internal/sync/access", `{"checks":[
		{"userId":"`+owner+`","fileId":"`+mainID+`"},{"userId":"`+editor+`","fileId":"`+mainID+`"},
		{"userId":"`+viewer+`","fileId":"`+mainID+`"},{"userId":"`+stranger+`","fileId":"`+mainID+`"}]}`)
	if code != http.StatusOK {
		t.Fatalf("access: %d %v", code, body)
	}
	var roles []string
	for _, r := range body["results"].([]any) {
		roles = append(roles, r.(map[string]any)["role"].(string))
	}
	if !slices.Equal(roles, []string{"owner", "editor", "viewer", "none"}) {
		t.Errorf("roles = %v", roles)
	}
}

// The REST rule while live (docs/decisions.md 2026-11-19): a save is refused while the sync
// server marks the file, and a REST save clears the Yjs state and its epoch.
func TestRESTSaveWhileLive(t *testing.T) {
	e := newMediaEnv(t)
	_, editor, _, pid, mainID := e.collabProject(t)
	content := "/api/v1/projects/" + pid + "/files/" + mainID + "/content"

	if err := e.rdb.Set(t.Context(), projectredis.LivePrefix+mainID, "1", 30*time.Second).Err(); err != nil {
		t.Fatal(err)
	}
	code, body := e.json(t, editor, http.MethodPut, content, `{"content":"rest","version":1}`)
	wantError(t, "live", code, body, http.StatusConflict, "live_session_open")
	if body["current"] != nil {
		t.Errorf("live refusal carries current: %v", body)
	}
	e.rdb.Del(t.Context(), projectredis.LivePrefix+mainID)

	// The sync server stored state; a REST save (the fallback) clears it and its epoch.
	state := base64.StdEncoding.EncodeToString([]byte{9})
	if code, body := e.internalJSON(t, http.MethodPut, "/api/v1/internal/sync/files/"+mainID+"/state",
		`{"ydocState":"`+state+`","ydocEpoch":"0193a7d4-0000-7000-8000-000000000002","content":"live","baseVersion":1}`); code != http.StatusOK {
		t.Fatalf("store: %d %v", code, body)
	}
	if code, body := e.json(t, editor, http.MethodPut, content, `{"content":"rest","version":2}`); code != http.StatusOK {
		t.Fatalf("REST save: %d %v", code, body)
	}
	var cleared bool
	if err := e.pool.QueryRow(t.Context(), `SELECT ydoc_state IS NULL AND ydoc_epoch IS NULL FROM project_files WHERE id = $1`, mainID).Scan(&cleared); err != nil || !cleared {
		t.Errorf("state cleared = %v (%v)", cleared, err)
	}
	if code, f := e.internalJSON(t, http.MethodGet, "/api/v1/internal/sync/files/"+mainID, ""); code != http.StatusOK || f["content"] != "rest" || f["ydocEpoch"] != nil {
		t.Errorf("load after REST: %d %v", code, f)
	}
}

// A REST save that locked the file row before the sync server marked it is committed before
// the sync server's load reads it: the load waits for the row.
func TestLoadWaitsForRESTSave(t *testing.T) {
	e := newMediaEnv(t)
	_, _, _, _, mainID := e.collabProject(t)
	tx, err := e.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), `UPDATE project_files SET content = 'from REST', size_bytes = 9, version = version + 1 WHERE id = $1`, mainID); err != nil {
		t.Fatal(err)
	}
	type result struct {
		code int
		body map[string]any
	}
	done := make(chan result, 1)
	go func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/internal/sync/files/"+mainID, nil)
		req.Header.Set("Authorization", "Bearer "+testSyncToken)
		e.internal.ServeHTTP(rec, req)
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		done <- result{rec.Code, body}
	}()
	select {
	case r := <-done:
		t.Fatalf("load did not wait for the save: %d %v", r.code, r.body)
	case <-time.After(200 * time.Millisecond):
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.code != http.StatusOK || r.body["content"] != "from REST" || r.body["version"] != 2.0 {
		t.Errorf("load = %d %v", r.code, r.body)
	}
}

// The events the sync server hears (docs/sync.md): after the commit, for role changes and
// removals, deleted files and projects, and ended sessions or suspensions.
func TestSyncEvents(t *testing.T) {
	e := newModEnv(t)
	owner, editor, viewer, pid, mainID := e.collabProject(t)
	sub := e.rdb.Subscribe(t.Context(), projectredis.SyncChannel(e.rdb.Options().DB))
	t.Cleanup(func() { _ = sub.Close() })
	if _, err := sub.Receive(t.Context()); err != nil {
		t.Fatal(err)
	}
	next := func(what string) projectredis.SyncEvent {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		m, err := sub.ReceiveMessage(ctx)
		if err != nil {
			t.Fatalf("%s: no event: %v", what, err)
		}
		var ev projectredis.SyncEvent
		if err := json.Unmarshal([]byte(m.Payload), &ev); err != nil {
			t.Fatal(err)
		}
		return ev
	}

	if code, body := e.json(t, owner, http.MethodPatch, "/api/v1/projects/"+pid+"/members/"+editor, `{"role":"viewer"}`); code != http.StatusOK {
		t.Fatalf("role change: %d %v", code, body)
	}
	if ev := next("role change"); ev.Kind != projectredis.SyncMembersChanged || ev.ProjectID != pid || !slices.Contains(ev.UserIDs, editor) {
		t.Errorf("role change event = %+v", ev)
	}
	if code, body := e.json(t, owner, http.MethodDelete, "/api/v1/projects/"+pid+"/members/"+viewer, ""); code != http.StatusNoContent {
		t.Fatalf("remove: %d %v", code, body)
	}
	if ev := next("removal"); ev.Kind != projectredis.SyncMembersChanged || !slices.Contains(ev.UserIDs, viewer) {
		t.Errorf("removal event = %+v", ev)
	}

	if code, body := e.json(t, owner, http.MethodDelete, "/api/v1/projects/"+pid+"/files/"+mainID, ""); code != http.StatusNoContent {
		t.Fatalf("delete file: %d %v", code, body)
	}
	if ev := next("file deletion"); ev.Kind != projectredis.SyncFilesDeleted || !slices.Equal(ev.FileIDs, []string{mainID}) {
		t.Errorf("file event = %+v", ev)
	}

	if _, err := e.moderation.Suspend(t.Context(), e.admin, editor, moderationdomain.SuspendInput{Input: moderationdomain.Input{Reason: modReason}}); err != nil {
		t.Fatal(err)
	}
	for {
		// A suspension may publish other events of the user's first; find the one for them.
		ev := next("suspension")
		if ev.Kind == projectredis.SyncUserEnded {
			if !slices.Equal(ev.UserIDs, []string{editor}) {
				t.Errorf("suspension event = %+v", ev)
			}
			break
		}
	}

	title := ""
	if err := e.pool.QueryRow(t.Context(), "SELECT title FROM projects WHERE id = $1", pid).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if code, body := e.json(t, owner, http.MethodDelete, "/api/v1/projects/"+pid+"?confirm="+url.QueryEscape(title), ""); code != http.StatusNoContent {
		t.Fatalf("delete project: %d %v", code, body)
	}
	if ev := next("project deletion"); ev.Kind != projectredis.SyncProjectDeleted || ev.ProjectID != pid {
		t.Errorf("project event = %+v", ev)
	}
}
