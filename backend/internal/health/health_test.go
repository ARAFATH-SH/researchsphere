package health

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api"
)

var (
	up   = PingerFunc(func(context.Context) error { return nil })
	down = PingerFunc(func(context.Context) error { return errors.New("connection refused") })
	// hung waits until the health check's timeout cancels it.
	hung = PingerFunc(func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
)

func TestGetHealth(t *testing.T) {
	tests := []struct {
		name       string
		db, redis  Pinger
		wantStatus int
		wantBody   map[string]string
	}{
		{
			name: "both up", db: up, redis: up,
			wantStatus: http.StatusOK,
			wantBody:   map[string]string{"status": "ok", "database": "ok", "redis": "ok"},
		},
		{
			name: "database down", db: down, redis: up,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   map[string]string{"code": "service_unavailable", "message": "database is not responding"},
		},
		{
			name: "redis down", db: up, redis: down,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   map[string]string{"code": "service_unavailable", "message": "redis is not responding"},
		},
		{
			name: "both down", db: down, redis: down,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   map[string]string{"code": "service_unavailable", "message": "database and redis are not responding"},
		},
		{
			name: "database hangs past the timeout", db: hung, redis: up,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   map[string]string{"code": "service_unavailable", "message": "database is not responding"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(tt.db, tt.redis, 50*time.Millisecond, slog.New(slog.DiscardHandler))
			mux := http.NewServeMux()
			api.HandlerWithOptions(api.NewStrictHandler(h, nil), api.StdHTTPServerOptions{BaseURL: "/api/v1", BaseRouter: mux})

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
