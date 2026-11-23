package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
)

// Each internal service's operations accept only that service's token, and RequireAuth
// refuses any internal operation the token check did not pass (docs/compile.md).
func TestRequireServiceTokens(t *testing.T) {
	syncTok, compileTok := []byte("sync-token-0123456789abcdef0123456789"), []byte("compile-token-0123456789abcdef012345")
	signer := accesstoken.New([]byte("test-secret-that-is-at-least-32-bytes-long"), 15*time.Minute)
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	chain := RequireServiceTokens(
		ServiceScheme{Scopes: gen.SyncTokenScopes, Token: syncTok},
		ServiceScheme{Scopes: gen.CompileTokenScopes, Token: compileTok},
	)(RequireAuth(signer, time.Now)(ok))
	tests := []struct {
		name   string
		scopes any
		token  string
		want   int
	}{
		{"compile operation, compile token", gen.CompileTokenScopes, string(compileTok), http.StatusNoContent},
		{"compile operation, sync token", gen.CompileTokenScopes, string(syncTok), http.StatusUnauthorized},
		{"compile operation, no token", gen.CompileTokenScopes, "", http.StatusUnauthorized},
		{"sync operation, sync token", gen.SyncTokenScopes, string(syncTok), http.StatusNoContent},
		{"sync operation, compile token", gen.SyncTokenScopes, string(compileTok), http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/internal/x", nil)
			r = r.WithContext(context.WithValue(r.Context(), tt.scopes, []string{}))
			if tt.token != "" {
				r.Header.Set("Authorization", "Bearer "+tt.token)
			}
			w := httptest.NewRecorder()
			chain.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Errorf("status = %d, want %d", w.Code, tt.want)
			}
		})
	}
	// Without the token middleware a compile operation is refused, not let through.
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/internal/x", nil)
	r = r.WithContext(context.WithValue(r.Context(), gen.CompileTokenScopes, []string{}))
	w := httptest.NewRecorder()
	RequireAuth(signer, time.Now)(ok).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unchecked compile operation: status = %d, want 401", w.Code)
	}
	// An empty token accepts nothing.
	r = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/internal/x", nil)
	r = r.WithContext(context.WithValue(r.Context(), gen.CompileTokenScopes, []string{}))
	r.Header.Set("Authorization", "Bearer ")
	w = httptest.NewRecorder()
	RequireServiceTokens(ServiceScheme{Scopes: gen.CompileTokenScopes})(ok).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("empty token: status = %d, want 401", w.Code)
	}
}

// Stream aborts the connection, never answers an error into a body already started.
func TestStreamAbortsAfterHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	func() {
		defer func() {
			v := recover()
			if err, ok := v.(error); !ok || !strings.Contains(err.Error(), "boom") {
				t.Errorf("recovered %v, want an abort carrying the error", v)
			}
		}()
		_ = Stream(w, time.Minute, http.Header{"Content-Type": {"application/zip"}}, func(out io.Writer) error {
			_, _ = io.WriteString(out, "PK")
			return errors.New("boom")
		})
	}()
	_ = w
	w = httptest.NewRecorder()
	if err := Stream(w, time.Minute, http.Header{"X-A": {"1"}}, func(out io.Writer) error {
		_, err := io.WriteString(out, "ok")
		return err
	}); err != nil || w.Body.String() != "ok" || w.Header().Get("X-A") != "1" {
		t.Errorf("stream: err %v, body %q", err, w.Body.String())
	}
}
