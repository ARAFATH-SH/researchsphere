//go:build integration

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	embeddingusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/aiclient"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

const fakeServiceModel = "intfloat/multilingual-e5-base@fake"

// fakeAIService speaks the AI service's protocol (GET /ready, POST /embed-document) over
// HTTP, so the real aiclient.Client is used. down makes it answer 503 model_loading.
type fakeAIService struct {
	down  atomic.Bool
	mu    sync.Mutex
	texts []string
	url   string
}

func newFakeAIService(t *testing.T) *fakeAIService {
	t.Helper()
	f := &fakeAIService{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if f.down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":"model_loading","message":"x"}`))
			return
		}
		switch r.URL.Path {
		case "/ready":
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready", "model": fakeServiceModel})
		case "/embed-document":
			var body struct {
				Text      string `json:"text"`
				MaxChunks int    `json:"maxChunks"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.MaxChunks != 8 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.mu.Lock()
			f.texts = append(f.texts, body.Text)
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": fakeServiceModel, "dimensions": aiclient.Dimensions,
				"vector": aiclient.FakeVector(aiclient.KindPassage, body.Text), "chunks": 1, "truncated": false,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	f.url = srv.URL
	return f
}

func (f *fakeAIService) client() *aiclient.Client {
	return aiclient.New(aiclient.Config{BaseURL: f.url, Token: "test-ai-service-token-0123456789abcd", RetryDelay: time.Millisecond})
}

func (f *fakeAIService) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.texts...)
}

// storedHash is the user's embedding hash, "" when there is none.
func (e mediaEnv) storedHash(t *testing.T, userID string) string {
	t.Helper()
	var hash string
	err := e.pool.QueryRow(t.Context(), "SELECT content_hash FROM profile_embeddings WHERE user_id = $1", userID).Scan(&hash)
	if err != nil && !strings.Contains(err.Error(), "no rows") {
		t.Fatal(err)
	}
	return hash
}

func (e mediaEnv) jobs(t *testing.T, userID string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM embedding_jobs WHERE owner_id = $1", userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// waitFor polls cond every 100 ms until it holds or limit passes, and returns how long it took.
func waitFor(t *testing.T, limit time.Duration, cond func() bool) (time.Duration, bool) {
	t.Helper()
	start := time.Now()
	for time.Since(start) < limit {
		if cond() {
			return time.Since(start), true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return time.Since(start), cond()
}

// AI-1.4 and AI-1.5 end to end: the API with its real worker (5 s poll) and the real AI
// client against a fake AI service. A profile change through the API is embedded within
// 60 seconds; while the service is down the job waits and the change is embedded once it is
// back.
func TestProfileChangeIsEmbeddedWithin60Seconds(t *testing.T) {
	ai := newFakeAIService(t)
	e := newMediaEnvOnPool(t, testenv.DB(t), "", "", envOptions{ai: ai.client()})
	ada := e.newProfileUser(t)
	_, worker := newEmbeddings(e.pool, ai.client(), slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go worker.Run(ctx)

	put := func(body string) time.Time {
		t.Helper()
		start := time.Now()
		rec := e.send(t, ada, http.MethodPut, "/api/v1/profiles/me", strings.NewReader(body), "application/json")
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT profile: %d %s", rec.Code, rec.Body)
		}
		// Saving never waits for the model (AI-1.5).
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("PUT took %v", d)
		}
		return start
	}

	put(`{"fullName":"Ada","headline":"Remote sensing for agriculture","bio":"I map rice fields."}`)
	want := embeddingusecase.ContentHash("Remote sensing for agriculture\nI map rice fields.")
	took, ok := waitFor(t, 60*time.Second, func() bool { return e.storedHash(t, ada) == want })
	if !ok {
		t.Fatalf("not embedded within 60 s (hash %q)", e.storedHash(t, ada))
	}
	t.Logf("embedded %v after the save", took)
	sent := ai.sent()
	if last := sent[len(sent)-1]; last != "Remote sensing for agriculture\nI map rice fields." {
		t.Errorf("text sent = %q", last)
	}
	for _, s := range sent {
		if strings.Contains(s, "@") {
			t.Errorf("the text sent contains an email address: %q", s)
		}
	}
	var model string
	var chunks int
	if err := e.pool.QueryRow(t.Context(), "SELECT model, chunk_count FROM profile_embeddings WHERE user_id = $1", ada).Scan(&model, &chunks); err != nil {
		t.Fatal(err)
	}
	if model != fakeServiceModel || chunks != 1 {
		t.Errorf("record = %s, %d chunks", model, chunks)
	}

	// Saving the same text again is skipped (AI-1.4).
	calls := len(ai.sent())
	put(`{"fullName":"Ada  Lovelace","headline":"Remote sensing for agriculture","bio":"I map rice fields."}`)
	if _, ok := waitFor(t, 15*time.Second, func() bool { return e.jobs(t, ada) == 0 }); !ok {
		t.Fatal("the job for an unchanged text was not finished")
	}
	if len(ai.sent()) != calls {
		t.Errorf("an unchanged text was sent to the model again")
	}

	// The service goes down: the change waits, nothing is lost, and it is embedded once the
	// service is back.
	ai.down.Store(true)
	put(`{"fullName":"Ada","headline":"Flood mapping","bio":"I map rice fields."}`)
	time.Sleep(7 * time.Second)
	if e.jobs(t, ada) != 1 || e.storedHash(t, ada) != want {
		t.Fatalf("while down: %d jobs, hash changed %v; want the job kept and the old embedding", e.jobs(t, ada), e.storedHash(t, ada) != want)
	}
	ai.down.Store(false)
	want = embeddingusecase.ContentHash("Flood mapping\nI map rice fields.")
	if took, ok := waitFor(t, 60*time.Second, func() bool { return e.storedHash(t, ada) == want }); !ok {
		t.Fatal("not embedded within 60 s of the service coming back")
	} else {
		t.Logf("embedded %v after the service came back", took)
	}

	// Emptying the profile deletes the embedding (AI-1.1).
	put(`{"fullName":"Ada"}`)
	if _, ok := waitFor(t, 60*time.Second, func() bool { return e.storedHash(t, ada) == "" }); !ok {
		t.Error("the embedding of an empty profile was not deleted")
	}
}

// Every kind of change the API makes to a profile's source text queues it: the profile, an
// interest or skill added or removed, a publication added, edited or removed.
func TestEveryProfileChangeThroughTheAPIQueuesTheProfile(t *testing.T) {
	e := newMediaEnv(t)
	ada := e.newProfileUser(t)
	clear := func() {
		t.Helper()
		if _, err := e.pool.Exec(t.Context(), "DELETE FROM embedding_jobs"); err != nil {
			t.Fatal(err)
		}
	}
	do := func(name, method, path, body string, want int) map[string]any {
		t.Helper()
		clear()
		var r *strings.Reader
		if body != "" {
			r = strings.NewReader(body)
		}
		var rec *httptest.ResponseRecorder
		if r != nil {
			rec = e.send(t, ada, method, path, r, "application/json")
		} else {
			rec = e.send(t, ada, method, path, nil, "")
		}
		if rec.Code != want {
			t.Fatalf("%s: %d %s, want %d", name, rec.Code, rec.Body, want)
		}
		if e.jobs(t, ada) != 1 {
			t.Errorf("%s: no embedding job queued", name)
		}
		if rec.Body.Len() == 0 {
			return nil
		}
		return decodeBody(t, rec.Body.Bytes())
	}

	do("profile update", http.MethodPut, "/api/v1/profiles/me", `{"fullName":"Ada","headline":"Rivers"}`, http.StatusOK)
	interest := do("interest added", http.MethodPost, "/api/v1/profiles/me/interests", `{"name":"hydrology `+testenv.Word(t)+`"}`, http.StatusCreated)
	do("interest removed", http.MethodDelete, "/api/v1/profiles/me/interests/"+interest["id"].(string), "", http.StatusNoContent)
	skill := do("skill added", http.MethodPost, "/api/v1/profiles/me/skills", `{"name":"GIS `+testenv.Word(t)+`"}`, http.StatusCreated)
	do("skill removed", http.MethodDelete, "/api/v1/profiles/me/skills/"+skill["id"].(string), "", http.StatusNoContent)
	pub := do("publication added", http.MethodPost, "/api/v1/profiles/me/publications", `{"title":"Flood maps","year":2025}`, http.StatusCreated)
	path := "/api/v1/profiles/me/publications/" + pub["id"].(string)
	do("publication edited", http.MethodPatch, path, `{"abstract":"We map floods."}`, http.StatusOK)
	do("publication removed", http.MethodDelete, path, "", http.StatusNoContent)

	// A change outside the text queues nothing.
	clear()
	rec := e.send(t, ada, http.MethodPut, "/api/v1/profiles/me/contact", strings.NewReader(`{"emailVisibility":"logged_in","phoneVisibility":"only_me","birthdayVisibility":"only_me"}`), "application/json")
	if rec.Code != http.StatusOK {
		t.Fatalf("contact: %d %s", rec.Code, rec.Body)
	}
	if e.jobs(t, ada) != 0 {
		t.Error("a contact change queued an embedding")
	}
}

// The admin card's operations end to end: statistics and an audited, idempotent rebuild.
func TestAdminEmbeddingsEndToEnd(t *testing.T) {
	ai := newFakeAIService(t)
	e := newMediaEnvOnPool(t, testenv.DB(t), "", "", envOptions{ai: ai.client()})
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	admin := e.newProfileUser(t)
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET role = 'admin' WHERE id = $1", admin); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(t.Context(), "UPDATE profiles SET headline = 'Graph learning'"); err != nil {
		t.Fatal(err)
	}
	_, worker := newEmbeddings(e.pool, ai.client(), slog.New(slog.DiscardHandler))
	worker.Tick(t.Context())
	if _, err := e.pool.Exec(t.Context(), "UPDATE profile_embeddings SET model = 'old-model@1' WHERE user_id = $1", bob); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(t.Context(), "UPDATE profiles SET bio = 'waiting' WHERE user_id = $1", ada); err != nil {
		t.Fatal(err)
	}

	code, body := e.asAdmin(t, admin, http.MethodGet, "/api/v1/admin/ai/embeddings", "")
	if code != http.StatusOK {
		t.Fatalf("stats: %d %v", code, body)
	}
	owners := body["ownerTypes"].([]any)
	got := owners[0].(map[string]any)
	if body["model"] != fakeServiceModel || got["ownerType"] != "profile" || got["total"] != float64(3) ||
		got["currentModel"] != float64(2) || got["queued"] != float64(1) || got["failed"] != float64(0) {
		t.Errorf("stats = %v", body)
	}

	// A researcher is refused and sees nothing.
	rec := e.send(t, ada, http.MethodGet, "/api/v1/admin/ai/embeddings", nil, "")
	if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "ownerTypes") {
		t.Errorf("researcher: %d %s", rec.Code, rec.Body)
	}
	if rec := e.send(t, ada, http.MethodPost, "/api/v1/admin/ai/embeddings/rebuild", nil, ""); rec.Code != http.StatusForbidden {
		t.Errorf("researcher rebuild: %d", rec.Code)
	}

	for range 2 {
		code, body = e.asAdmin(t, admin, http.MethodPost, "/api/v1/admin/ai/embeddings/rebuild", "")
		if code != http.StatusAccepted || body["queued"] != float64(3) {
			t.Errorf("rebuild: %d %v, want 202 and 3", code, body)
		}
	}
	var jobs, audits int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM embedding_jobs").Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM ai_admin_actions WHERE admin_id = $1", admin).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if jobs != 3 || audits != 2 {
		t.Errorf("after two rebuilds: %d jobs, %d audit rows; want 3 and 2", jobs, audits)
	}

	// The AI service down: the model is null, the counts still come.
	ai.down.Store(true)
	code, body = e.asAdmin(t, admin, http.MethodGet, "/api/v1/admin/ai/embeddings", "")
	if code != http.StatusOK || body["model"] != nil {
		t.Errorf("stats while down: %d %v", code, body)
	}
}

// The hourly upkeep clears a timed suspension that has ended, as a login would, and the
// trigger queues the profile; a suspension still running stays.
func TestEndedSuspensionsAreClearedAndQueued(t *testing.T) {
	e := newMediaEnv(t)
	ended, running := e.newProfileUser(t), e.newProfileUser(t)
	for id, until := range map[string]string{ended: "now() - interval '1 minute'", running: "now() + interval '1 day'"} {
		if _, err := e.pool.Exec(t.Context(), `UPDATE users SET suspended_at = now() - interval '2 days', suspended_until = `+until+`,
			suspension_reason = 'a reason long enough' WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.pool.Exec(t.Context(), "DELETE FROM embedding_jobs"); err != nil {
		t.Fatal(err)
	}
	n, err := e.auth.ClearEndedSuspensions(t.Context())
	if err != nil || n != 1 {
		t.Fatalf("ClearEndedSuspensions = %d, %v; want 1", n, err)
	}
	var cleared, stillSuspended bool
	if err := e.pool.QueryRow(t.Context(), "SELECT suspended_at IS NULL FROM users WHERE id = $1", ended).Scan(&cleared); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.QueryRow(t.Context(), "SELECT suspended_at IS NOT NULL FROM users WHERE id = $1", running).Scan(&stillSuspended); err != nil {
		t.Fatal(err)
	}
	if !cleared || !stillSuspended {
		t.Errorf("cleared %v, still suspended %v; want both", cleared, stillSuspended)
	}
	if e.jobs(t, ended) != 1 || e.jobs(t, running) != 0 {
		t.Errorf("jobs: ended %d, running %d; want 1 and 0", e.jobs(t, ended), e.jobs(t, running))
	}
}
