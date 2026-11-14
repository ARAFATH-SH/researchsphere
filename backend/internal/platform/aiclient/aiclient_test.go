package aiclient

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "test-token-0123456789-abcdefghijklmnop"

// service is a stand-in for the AI service: answer decides each response, and it records
// what it received.
type service struct {
	calls   atomic.Int32
	answer  func(call int32, w http.ResponseWriter, r *http.Request)
	gotAuth atomic.Value
	gotBody atomic.Value
}

func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := s.calls.Add(1)
	s.gotAuth.Store(r.Header.Get("Authorization"))
	var body embedRequest
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.gotBody.Store(body)
	s.answer(n, w, r.WithContext(context.WithValue(r.Context(), bodyKey{}, body)))
}

// bodyKey carries the decoded request body to answer, which can no longer read it.
type bodyKey struct{}

func newService(t *testing.T, answer func(call int32, w http.ResponseWriter, r *http.Request)) (*service, *Client) {
	t.Helper()
	s := &service{answer: answer}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return s, New(Config{BaseURL: srv.URL + "/", Token: testToken, Timeout: 200 * time.Millisecond, RetryDelay: time.Millisecond})
}

func vectors(n int) [][]float32 {
	out := make([][]float32, n)
	for i := range out {
		out[i] = FakeVector(KindPassage, string(rune('a'+i)))
	}
	return out
}

func ok(w http.ResponseWriter, r *http.Request) {
	body, _ := r.Context().Value(bodyKey{}).(embedRequest)
	writeJSON(w, http.StatusOK, embedResponse{Model: "m@rev", Dimensions: Dimensions, Vectors: vectors(len(body.Texts))})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(status int, code string) func(int32, http.ResponseWriter, *http.Request) {
	return func(_ int32, w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, status, map[string]string{"code": code, "message": "x"})
	}
}

func TestEmbedSuccess(t *testing.T) {
	s, c := newService(t, func(_ int32, w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/embed" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request = %s %s (%s), want POST /embed as JSON", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		writeJSON(w, http.StatusOK, embedResponse{Model: "m@rev", Dimensions: Dimensions, Vectors: vectors(2)})
	})

	res, err := c.Embed(t.Context(), KindQuery, []string{"graph", "গ্রাফ"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Model != "m@rev" || len(res.Vectors) != 2 || len(res.Vectors[0]) != Dimensions {
		t.Errorf("result = %s with %d vectors", res.Model, len(res.Vectors))
	}
	if got := s.gotAuth.Load(); got != "Bearer "+testToken {
		t.Errorf("Authorization = %q, want the bearer token", got)
	}
	if got := s.gotBody.Load().(embedRequest); got.Kind != KindQuery || len(got.Texts) != 2 || got.Texts[1] != "গ্রাফ" {
		t.Errorf("body = %+v", got)
	}
	if s.calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", s.calls.Load())
	}
}

func TestEmbedRetries(t *testing.T) {
	tests := []struct {
		name      string
		answer    func(int32, http.ResponseWriter, *http.Request)
		wantErr   error // nil means success
		wantCalls int32
		wantCode  string
	}{
		{
			name: "500 then 200: one retry succeeds",
			answer: func(n int32, w http.ResponseWriter, r *http.Request) {
				if n == 1 {
					fail(500, "internal")(n, w, r)
					return
				}
				ok(w, r)
			},
			wantCalls: 2,
		},
		{
			name: "503 model_loading then 200",
			answer: func(n int32, w http.ResponseWriter, r *http.Request) {
				if n == 1 {
					fail(503, "model_loading")(n, w, r)
					return
				}
				ok(w, r)
			},
			wantCalls: 2,
		},
		{
			name: "timeout then 200",
			answer: func(n int32, w http.ResponseWriter, r *http.Request) {
				if n == 1 {
					select {
					case <-r.Context().Done():
					case <-time.After(2 * time.Second):
					}
					return
				}
				ok(w, r)
			},
			wantCalls: 2,
		},
		{name: "500 twice: gives up after two calls", answer: fail(500, "internal"), wantErr: ErrUnavailable, wantCalls: 2, wantCode: "internal"},
		{name: "504 twice", answer: fail(504, "timeout"), wantErr: ErrUnavailable, wantCalls: 2, wantCode: "timeout"},
		{name: "400: no retry", answer: fail(400, "bad_request"), wantErr: ErrBadResponse, wantCalls: 1, wantCode: "bad_request"},
		{name: "401: no retry", answer: fail(401, "unauthorized"), wantErr: ErrBadResponse, wantCalls: 1, wantCode: "unauthorized"},
		{name: "413: no retry", answer: fail(413, "payload_too_large"), wantErr: ErrBadResponse, wantCalls: 1, wantCode: "payload_too_large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, c := newService(t, tt.answer)
			_, err := c.Embed(t.Context(), KindPassage, []string{"text"})
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if got := s.calls.Load(); got != tt.wantCalls {
				t.Errorf("calls = %d, want %d", got, tt.wantCalls)
			}
			var se *StatusError
			if tt.wantCode != "" && (!errors.As(err, &se) || se.Code != tt.wantCode) {
				t.Errorf("err = %v, want a StatusError with code %s", err, tt.wantCode)
			}
		})
	}
}

func TestEmbedConnectionRefusedRetriesOnce(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens there any more
	c := New(Config{BaseURL: url, Token: testToken, RetryDelay: time.Millisecond})
	if _, err := c.Embed(t.Context(), KindPassage, []string{"t"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

func TestEmbedDoesNotRetryOnceTheCallerGivesUp(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	s, c := newService(t, func(_ int32, w http.ResponseWriter, _ *http.Request) {
		cancel()
		writeJSON(w, 503, map[string]string{"code": "model_loading"})
	})
	_, err := c.Embed(ctx, KindPassage, []string{"t"})
	if err == nil {
		t.Fatal("err = nil, want an error")
	}
	if got := s.calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (no retry after the context is done)", got)
	}
}

func TestEmbedRefusesBadResponses(t *testing.T) {
	tests := []struct {
		name string
		body any
	}{
		{"wrong dimensions field", embedResponse{Model: "m", Dimensions: 384, Vectors: vectors(1)}},
		{"short vector", embedResponse{Model: "m", Dimensions: Dimensions, Vectors: [][]float32{{1, 2, 3}}}},
		{"too few vectors", embedResponse{Model: "m", Dimensions: Dimensions, Vectors: nil}},
		{"too many vectors", embedResponse{Model: "m", Dimensions: Dimensions, Vectors: vectors(2)}},
		{"no model name", embedResponse{Dimensions: Dimensions, Vectors: vectors(1)}},
		{"not JSON", "<html>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, c := newService(t, func(_ int32, w http.ResponseWriter, _ *http.Request) {
				if str, isStr := tt.body.(string); isStr {
					_, _ = w.Write([]byte(str))
					return
				}
				writeJSON(w, http.StatusOK, tt.body)
			})
			if _, err := c.Embed(t.Context(), KindPassage, []string{"t"}); !errors.Is(err, ErrBadResponse) {
				t.Errorf("err = %v, want ErrBadResponse", err)
			}
			if s.calls.Load() != 1 {
				t.Errorf("calls = %d, want 1 (a bad 200 is not retried)", s.calls.Load())
			}
		})
	}
}

func TestEmbedChecksInputBeforeCalling(t *testing.T) {
	tests := []struct {
		name  string
		kind  Kind
		texts []string
	}{
		{"no texts", KindPassage, nil},
		{"33 texts", KindPassage, make([]string, 33)},
		{"empty text", KindPassage, []string{""}},
		{"8001 characters", KindPassage, []string{strings.Repeat("a", MaxTextChars+1)}},
		{"8001 Bangla characters", KindPassage, []string{strings.Repeat("ক", MaxTextChars+1)}},
		{"unknown kind", Kind("document"), []string{"t"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, c := newService(t, func(_ int32, w http.ResponseWriter, r *http.Request) { ok(w, r) })
			if _, err := c.Embed(t.Context(), tt.kind, tt.texts); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("err = %v, want ErrInvalidInput", err)
			}
			if s.calls.Load() != 0 {
				t.Errorf("calls = %d, want 0", s.calls.Load())
			}
		})
	}
	// 8,000 Bangla characters are 24,000 bytes but within the limit: characters count.
	_, c := newService(t, func(_ int32, w http.ResponseWriter, r *http.Request) { ok(w, r) })
	if _, err := c.Embed(t.Context(), KindPassage, []string{strings.Repeat("ক", MaxTextChars)}); err != nil {
		t.Errorf("8000 Bangla characters: err = %v, want nil", err)
	}
}

func TestNotConfigured(t *testing.T) {
	c := New(Config{BaseURL: "http://127.0.0.1:1"})
	if _, err := c.Embed(t.Context(), KindPassage, []string{"t"}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Embed err = %v, want ErrNotConfigured", err)
	}
	if err := c.Ping(t.Context()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Ping err = %v, want ErrNotConfigured", err)
	}
}

func TestPing(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr error
	}{
		{"ready", 200, nil},
		{"loading", 503, ErrUnavailable},
		{"not found", 404, ErrBadResponse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, c := newService(t, func(_ int32, w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/ready" {
					t.Errorf("request = %s %s, want GET /ready", r.Method, r.URL.Path)
				}
				writeJSON(w, tt.status, map[string]string{"code": "model_loading"})
			})
			err := c.Ping(t.Context())
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if s.calls.Load() != 1 {
				t.Errorf("calls = %d, want 1 (Ping does not retry)", s.calls.Load())
			}
		})
	}
}

func TestPingRespectsTheCallersTimeout(t *testing.T) {
	_, c := newService(t, func(_ int32, _ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := c.Ping(ctx); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

func TestStatusErrorKeepsNoMessage(t *testing.T) {
	_, c := newService(t, func(_ int32, w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 400, map[string]string{"code": "bad_request", "message": "secret text"})
	})
	_, err := c.Embed(t.Context(), KindPassage, []string{"t"})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("err = %v, want one without the service's message", err)
	}
}

func TestFake(t *testing.T) {
	f := &Fake{}
	res, err := f.Embed(t.Context(), KindPassage, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Model != FakeModel || len(res.Vectors) != 2 {
		t.Fatalf("result = %+v", res)
	}
	for _, v := range res.Vectors {
		var sum float64
		for _, x := range v {
			sum += float64(x) * float64(x)
		}
		if len(v) != Dimensions || math.Abs(math.Sqrt(sum)-1) > 1e-5 {
			t.Errorf("vector has %d numbers and length %v, want %d and 1", len(v), math.Sqrt(sum), Dimensions)
		}
	}
	again, _ := f.Embed(t.Context(), KindPassage, []string{"a"})
	query, _ := f.Embed(t.Context(), KindQuery, []string{"a"})
	if again.Vectors[0][0] != res.Vectors[0][0] || query.Vectors[0][0] == res.Vectors[0][0] {
		t.Error("want the same vector for the same kind and text, and a different one for another kind")
	}
	if got := f.Calls(); len(got) != 3 || got[2].Kind != KindQuery {
		t.Errorf("calls = %+v", got)
	}
	if _, err := f.Embed(t.Context(), KindPassage, nil); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("no texts: err = %v, want ErrInvalidInput", err)
	}

	f.Err = ErrUnavailable
	if _, err := f.Embed(t.Context(), KindPassage, []string{"a"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Embed err = %v", err)
	}
	if err := f.Ping(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Ping err = %v", err)
	}
}
