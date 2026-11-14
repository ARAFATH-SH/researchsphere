package aiclient

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// docService is a stand-in for POST /embed-document and GET /ready.
func docService(t *testing.T, answer func(call int32, w http.ResponseWriter, r *http.Request, body embedDocumentRequest)) (*atomic.Int32, *Client) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		var body embedDocumentRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		answer(n, w, r, body)
	}))
	t.Cleanup(srv.Close)
	return &calls, New(Config{BaseURL: srv.URL, Token: testToken, Timeout: 200 * time.Millisecond, RetryDelay: time.Millisecond})
}

func docOK(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, embedDocumentResponse{
		Model: "m@rev", Dimensions: Dimensions, Vector: FakeVector(KindPassage, "x"), Chunks: 3, Truncated: true,
	})
}

func TestEmbedDocumentSuccess(t *testing.T) {
	calls, c := docService(t, func(_ int32, w http.ResponseWriter, r *http.Request, body embedDocumentRequest) {
		if r.Method != http.MethodPost || r.URL.Path != "/embed-document" || r.Header.Get("Authorization") != "Bearer "+testToken {
			t.Errorf("request = %s %s, want POST /embed-document with the token", r.Method, r.URL.Path)
		}
		if body.Text != "headline\nগবেষণা" || body.MaxChunks != 8 {
			t.Errorf("body = %+v", body)
		}
		docOK(w)
	})
	res, err := c.EmbedDocument(t.Context(), "headline\nগবেষণা", 8)
	if err != nil {
		t.Fatal(err)
	}
	if res.Model != "m@rev" || len(res.Vector) != Dimensions || res.Chunks != 3 || !res.Truncated {
		t.Errorf("result = %s, %d numbers, %d chunks, truncated %v", res.Model, len(res.Vector), res.Chunks, res.Truncated)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestEmbedDocumentRetriesOnceAfterA5xx(t *testing.T) {
	calls, c := docService(t, func(n int32, w http.ResponseWriter, _ *http.Request, _ embedDocumentRequest) {
		if n == 1 {
			writeJSON(w, 503, map[string]string{"code": "model_loading"})
			return
		}
		docOK(w)
	})
	if _, err := c.EmbedDocument(t.Context(), "t", 8); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2", calls.Load())
	}
}

func TestEmbedDocumentErrors(t *testing.T) {
	tests := []struct {
		name    string
		answer  func(w http.ResponseWriter)
		want    error
		wantTry int32
	}{
		{"down", func(w http.ResponseWriter) { writeJSON(w, 502, nil) }, ErrUnavailable, 2},
		{"refused", func(w http.ResponseWriter) { writeJSON(w, 400, map[string]string{"code": "bad_request"}) }, ErrBadResponse, 1},
		{"short vector", func(w http.ResponseWriter) {
			writeJSON(w, 200, embedDocumentResponse{Model: "m", Dimensions: Dimensions, Vector: []float32{1}, Chunks: 1})
		}, ErrBadResponse, 1},
		{"too many chunks", func(w http.ResponseWriter) {
			writeJSON(w, 200, embedDocumentResponse{Model: "m", Dimensions: Dimensions, Vector: FakeVector(KindPassage, "x"), Chunks: 9})
		}, ErrBadResponse, 1},
		{"no model", func(w http.ResponseWriter) {
			writeJSON(w, 200, embedDocumentResponse{Dimensions: Dimensions, Vector: FakeVector(KindPassage, "x"), Chunks: 1})
		}, ErrBadResponse, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls, c := docService(t, func(_ int32, w http.ResponseWriter, _ *http.Request, _ embedDocumentRequest) { tt.answer(w) })
			_, err := c.EmbedDocument(t.Context(), "t", 8)
			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
			if calls.Load() != tt.wantTry {
				t.Errorf("calls = %d, want %d", calls.Load(), tt.wantTry)
			}
		})
	}
}

func TestEmbedDocumentChecksInputBeforeCalling(t *testing.T) {
	calls, c := docService(t, func(_ int32, w http.ResponseWriter, _ *http.Request, _ embedDocumentRequest) { docOK(w) })
	for name, in := range map[string]struct {
		text   string
		chunks int
	}{
		"empty":    {"", 8},
		"blank":    {" \n\t", 8},
		"too long": {strings.Repeat("ক", MaxDocumentChars+1), 8},
		"0 chunks": {"t", 0},
		"9 chunks": {"t", 9},
	} {
		if _, err := c.EmbedDocument(t.Context(), in.text, in.chunks); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: err = %v, want ErrInvalidInput", name, err)
		}
	}
	if _, err := c.EmbedDocument(t.Context(), strings.Repeat("ক", MaxDocumentChars), 8); err != nil {
		t.Errorf("100,000 Bangla characters: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1 (only the valid text is sent)", calls.Load())
	}
	if _, err := New(Config{BaseURL: "http://127.0.0.1:1"}).EmbedDocument(t.Context(), "t", 8); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("no token: err = %v, want ErrNotConfigured", err)
	}
}

func TestModel(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    any
		want    string
		wantErr error
	}{
		{"ready", 200, map[string]string{"status": "ready", "model": "e5@abc"}, "e5@abc", nil},
		{"no name", 200, map[string]string{"status": "ready"}, "", ErrBadResponse},
		{"loading", 503, map[string]string{"code": "model_loading"}, "", ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, c := docService(t, func(_ int32, w http.ResponseWriter, r *http.Request, _ embedDocumentRequest) {
				if r.Method != http.MethodGet || r.URL.Path != "/ready" {
					t.Errorf("request = %s %s, want GET /ready", r.Method, r.URL.Path)
				}
				writeJSON(w, tt.status, tt.body)
			})
			got, err := c.Model(t.Context())
			if got != tt.want || !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Errorf("Model = %q, %v; want %q, %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
	if _, err := New(Config{}).Model(t.Context()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("no token: err = %v, want ErrNotConfigured", err)
	}
}

func TestFakeDocumentAndModel(t *testing.T) {
	f := &Fake{}
	res, err := f.EmbedDocument(t.Context(), "a profile", 8)
	if err != nil {
		t.Fatal(err)
	}
	if res.Model != FakeModel || res.Chunks != 1 || res.Vector[0] != FakeVector(KindPassage, "a profile")[0] {
		t.Errorf("result = %+v", res)
	}
	if got := f.DocumentCalls(); len(got) != 1 || got[0] != "a profile" {
		t.Errorf("document calls = %q", got)
	}
	f.SetModel("other@1")
	if m, _ := f.Model(t.Context()); m != "other@1" {
		t.Errorf("Model = %q, want other@1", m)
	}
	if _, err := f.EmbedDocument(t.Context(), " ", 8); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("blank text: err = %v, want ErrInvalidInput", err)
	}
	f.SetErr(ErrUnavailable)
	if _, err := f.Model(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Model err = %v, want ErrUnavailable", err)
	}
}
