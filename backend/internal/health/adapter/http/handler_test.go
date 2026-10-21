package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/health/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/health/usecase"
)

// pingerFunc adapts a function to domain.Pinger.
type pingerFunc func(ctx context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error { return f(ctx) }

var (
	up   = pingerFunc(func(context.Context) error { return nil })
	down = pingerFunc(func(context.Context) error { return errors.New("connection refused") })
	// hung waits until the health check's timeout cancels it.
	hung = pingerFunc(func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
)

// server serves only the health operations: Handler's methods are promoted from a
// shallower depth than the embedded interface's, which stays nil.
type server struct {
	*Handler
	others
}

type others struct{ gen.StrictServerInterface }

func TestGetHealth(t *testing.T) {
	tests := []struct {
		name       string
		db, redis  domain.Pinger
		storage    domain.Pinger
		wantStatus int
		wantBody   map[string]string
	}{
		{
			name: "all up", db: up, redis: up, storage: up,
			wantStatus: http.StatusOK,
			wantBody:   map[string]string{"status": "ok", "database": "ok", "redis": "ok", "storage": "ok"},
		},
		{
			name: "database down", db: down, redis: up, storage: up,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   map[string]string{"code": "service_unavailable", "message": "database is not responding"},
		},
		{
			name: "redis down", db: up, redis: down, storage: up,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   map[string]string{"code": "service_unavailable", "message": "redis is not responding"},
		},
		{
			name: "database and redis down", db: down, redis: down, storage: up,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   map[string]string{"code": "service_unavailable", "message": "database and redis are not responding"},
		},
		{
			name: "storage down (MinIO stopped)", db: up, redis: up, storage: down,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   map[string]string{"code": "service_unavailable", "message": "storage is not responding"},
		},
		{
			name: "database and storage down", db: down, redis: up, storage: down,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   map[string]string{"code": "service_unavailable", "message": "database and storage are not responding"},
		},
		{
			name: "all down", db: down, redis: down, storage: down,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   map[string]string{"code": "service_unavailable", "message": "database, redis and storage are not responding"},
		},
		{
			name: "storage hangs past the timeout", db: up, redis: up, storage: hung,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   map[string]string{"code": "service_unavailable", "message": "storage is not responding"},
		},
		{
			name: "database hangs past the timeout", db: hung, redis: up, storage: up,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   map[string]string{"code": "service_unavailable", "message": "database is not responding"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := usecase.New(50*time.Millisecond,
				domain.Dependency{Name: "database", Pinger: tt.db},
				domain.Dependency{Name: "redis", Pinger: tt.redis},
				domain.Dependency{Name: "storage", Pinger: tt.storage},
			)
			h := NewHandler(checker, slog.New(slog.DiscardHandler))
			mux := http.NewServeMux()
			gen.HandlerWithOptions(gen.NewStrictHandler(server{Handler: h}, nil), gen.StdHTTPServerOptions{BaseURL: "/api/v1", BaseRouter: mux})

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/health", nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			var got map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("body %q is not JSON: %v", rec.Body, err)
			}
			if len(got) != len(tt.wantBody) {
				t.Errorf("body = %v, want %v", got, tt.wantBody)
			}
			for k, v := range tt.wantBody {
				if got[k] != v {
					t.Errorf("body[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
