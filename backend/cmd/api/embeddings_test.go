package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	embeddingdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/domain"
	embeddingusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/aiclient"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// accessCookie is a verified session's access cookie for role, as getAs sends.
func accessCookie(t *testing.T, role authz.Role) *http.Cookie {
	t.Helper()
	token, _, err := accesstoken.New([]byte(testSecret), accessTTL).Sign(okUserID, string(role), true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: httpx.AccessCookie, Value: token, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
}

// okEmbeddings answers the admin operations with fixed values.
type okEmbeddings struct{}

func (okEmbeddings) Stats(context.Context) (embeddingusecase.Stats, error) {
	return embeddingusecase.Stats{Model: "m@1", Owners: []embeddingdomain.OwnerStats{{OwnerType: embeddingdomain.OwnerProfile, Total: 3}}}, nil
}

func (okEmbeddings) Rebuild(context.Context, string) (int, error) { return 7, nil }

// The AI service's errors reach the worker in the module's terms, and stay inspectable.
func TestEmbedderError(t *testing.T) {
	tests := []struct {
		in   error
		want error
	}{
		{fmt.Errorf("%w: refused", aiclient.ErrUnavailable), embeddingdomain.ErrUnavailable},
		{&aiclient.StatusError{StatusCode: 503, Code: "model_loading"}, embeddingdomain.ErrUnavailable},
		{context.DeadlineExceeded, embeddingdomain.ErrUnavailable},
		{aiclient.ErrNotConfigured, embeddingdomain.ErrNotConfigured},
	}
	for _, tt := range tests {
		got := embedderError(tt.in)
		if !errors.Is(got, tt.want) || !errors.Is(got, tt.in) {
			t.Errorf("embedderError(%v) = %v, want it to be %v and keep the cause", tt.in, got, tt.want)
		}
	}
	text := &aiclient.StatusError{StatusCode: 400, Code: "bad_request"}
	got := embedderError(text)
	if errors.Is(got, embeddingdomain.ErrUnavailable) || errors.Is(got, embeddingdomain.ErrNotConfigured) {
		t.Errorf("a 400 is a text error, got %v", got)
	}
}

func TestAIEmbedderPassesTheResultOn(t *testing.T) {
	fake := &aiclient.Fake{}
	vec, model, chunks, err := aiEmbedder{ai: fake}.EmbedDocument(t.Context(), "graph networks", 8)
	if err != nil || model != aiclient.FakeModel || chunks != 1 || len(vec) != aiclient.Dimensions {
		t.Errorf("EmbedDocument = %d numbers, %q, %d chunks, %v", len(vec), model, chunks, err)
	}
	fake.SetErr(aiclient.ErrUnavailable)
	if _, err := (aiEmbedder{ai: fake}).Model(t.Context()); !errors.Is(err, embeddingdomain.ErrUnavailable) {
		t.Errorf("Model err = %v, want ErrUnavailable", err)
	}
}

// Both admin operations need the ManageAI action: 401 without a session, 403 for a
// researcher, and the answer for an administrator.
func TestEmbeddingAdminRoutes(t *testing.T) {
	router := testRouter()
	for _, route := range []struct{ method, path, wantBody string }{
		{http.MethodGet, "/api/v1/admin/ai/embeddings", `"ownerType":"profile"`},
		{http.MethodPost, "/api/v1/admin/ai/embeddings/rebuild", `"queued":7`},
	} {
		for _, c := range []struct {
			name   string
			cookie *http.Cookie
			want   int
		}{
			{"no session", nil, http.StatusUnauthorized},
			{"researcher", accessCookie(t, authz.RoleResearcher), http.StatusForbidden},
			{"admin", accessCookie(t, authz.RoleAdmin), map[string]int{http.MethodGet: 200, http.MethodPost: 202}[route.method]},
		} {
			t.Run(route.method+" "+c.name, func(t *testing.T) {
				req := httptest.NewRequestWithContext(t.Context(), route.method, route.path, nil)
				if c.cookie != nil {
					req.AddCookie(c.cookie)
				}
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code != c.want {
					t.Fatalf("status = %d, want %d; body %s", rec.Code, c.want, rec.Body)
				}
				if c.want < 300 && !strings.Contains(rec.Body.String(), route.wantBody) {
					t.Errorf("body = %s, want %s", rec.Body, route.wantBody)
				}
				if c.want == http.StatusForbidden && strings.Contains(rec.Body.String(), "ownerType") {
					t.Errorf("a refusal leaked data: %s", rec.Body)
				}
			})
		}
	}
}
