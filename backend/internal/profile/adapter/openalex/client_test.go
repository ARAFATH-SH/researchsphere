package openalex

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

const engineJSON = `{
	"id": "https://openalex.org/W1",
	"doi": "https://doi.org/10.1145/3292500.3330701",
	"title": "Notes on the Analytical Engine",
	"display_name": "Notes on the Analytical Engine",
	"publication_year": 2019,
	"primary_location": {
		"landing_page_url": "https://dl.acm.org/doi/10.1145/3292500.3330701",
		"source": {"display_name": "Knowledge Discovery and Data Mining"}
	},
	"abstract_inverted_index": {"We": [0], "describe": [1], "the": [2, 4], "engine": [3], "notes.": [5]}
}`

// fakeOpenAlex serves the responses in order, one per request, repeating the last, and
// counts the requests.
type fakeOpenAlex struct {
	responses []func(w http.ResponseWriter, r *http.Request)
	calls     atomic.Int32
	last      atomic.Pointer[http.Request]
}

func (f *fakeOpenAlex) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := int(f.calls.Add(1))
	f.last.Store(r)
	f.responses[min(n, len(f.responses))-1](w, r)
}

func status(code int, body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}
}

// slow answers after d, unless the client gave up first.
func slow(d time.Duration) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(d):
			status(http.StatusOK, engineJSON)(w, r)
		case <-r.Context().Done():
		}
	}
}

func newTestClient(t *testing.T, responses ...func(http.ResponseWriter, *http.Request)) (*Client, *fakeOpenAlex) {
	t.Helper()
	fake := &fakeOpenAlex{responses: responses}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return New(Config{BaseURL: srv.URL + "/", Mailto: "dev@example.org", Timeout: 100 * time.Millisecond, RetryDelay: time.Millisecond}), fake
}

func ptr[T any](v T) *T { return &v }

func TestFetchWork(t *testing.T) {
	const doi = "10.1145/3292500.3330701"
	engine := domain.WorkMetadata{
		DOI: doi, Title: "Notes on the Analytical Engine", Year: ptr(2019),
		Venue:    ptr("Knowledge Discovery and Data Mining"),
		Abstract: ptr("We describe the engine the notes."),
		URL:      ptr("https://dl.acm.org/doi/10.1145/3292500.3330701"),
	}
	tests := []struct {
		name      string
		responses []func(http.ResponseWriter, *http.Request)
		want      domain.WorkMetadata
		wantErr   error
		wantCalls int32
	}{
		{"normal work", []func(http.ResponseWriter, *http.Request){status(200, engineJSON)}, engine, nil, 1},
		{"missing abstract", []func(http.ResponseWriter, *http.Request){status(200,
			`{"title":"T","publication_year":2020,"abstract_inverted_index":null,"primary_location":{"source":{"display_name":"V"}}}`)},
			domain.WorkMetadata{DOI: doi, Title: "T", Year: ptr(2020), Venue: ptr("V")}, nil, 1},
		{"missing venue", []func(http.ResponseWriter, *http.Request){status(200,
			`{"title":"T","primary_location":{"landing_page_url":"https://x.org/p","source":null}}`)},
			domain.WorkMetadata{DOI: doi, Title: "T", URL: ptr("https://x.org/p")}, nil, 1},
		{"no primary location, title from display_name", []func(http.ResponseWriter, *http.Request){status(200,
			`{"title":null,"display_name":"D"}`)},
			domain.WorkMetadata{DOI: doi, Title: "D"}, nil, 1},
		{"404", []func(http.ResponseWriter, *http.Request){status(404, `{"error":"not found"}`)},
			domain.WorkMetadata{}, domain.ErrWorkNotFound, 1},
		{"500 then success on retry", []func(http.ResponseWriter, *http.Request){status(500, ``), status(200, engineJSON)},
			engine, nil, 2},
		{"500 twice", []func(http.ResponseWriter, *http.Request){status(503, ``)},
			domain.WorkMetadata{}, domain.ErrMetadataUnavailable, 2},
		{"timeout then success on retry", []func(http.ResponseWriter, *http.Request){slow(time.Second), status(200, engineJSON)},
			engine, nil, 2},
		{"timeout twice", []func(http.ResponseWriter, *http.Request){slow(time.Second)},
			domain.WorkMetadata{}, domain.ErrMetadataUnavailable, 2},
		{"400 is not retried", []func(http.ResponseWriter, *http.Request){status(400, `{}`), status(200, engineJSON)},
			domain.WorkMetadata{}, domain.ErrMetadataUnavailable, 1},
		{"429 from OpenAlex is not retried", []func(http.ResponseWriter, *http.Request){status(429, `{}`), status(200, engineJSON)},
			domain.WorkMetadata{}, domain.ErrMetadataUnavailable, 1},
		{"malformed JSON", []func(http.ResponseWriter, *http.Request){status(200, `{"title": "T"`)},
			domain.WorkMetadata{}, domain.ErrMetadataUnavailable, 1},
		{"not a JSON object", []func(http.ResponseWriter, *http.Request){status(200, `<html>`)},
			domain.WorkMetadata{}, domain.ErrMetadataUnavailable, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, fake := newTestClient(t, tt.responses...)
			got, err := c.FetchWork(t.Context(), doi)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
			if n := fake.calls.Load(); n != tt.wantCalls {
				t.Errorf("requests = %d, want %d", n, tt.wantCalls)
			}
		})
	}
}

func TestFetchWorkRequest(t *testing.T) {
	c, fake := newTestClient(t, status(200, engineJSON))
	if _, err := c.FetchWork(t.Context(), "10.1000/a;b?c#d"); err != nil {
		t.Fatal(err)
	}
	r := fake.last.Load()
	if r.Method != http.MethodGet || r.URL.EscapedPath() != "/works/doi:10.1000/a%3Bb%3Fc%23d" {
		t.Errorf("request %s %s", r.Method, r.URL.EscapedPath())
	}
	q := r.URL.Query()
	if q.Get("mailto") != "dev@example.org" || !strings.Contains(q.Get("select"), "abstract_inverted_index") {
		t.Errorf("query = %v", q)
	}
	if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "ResearchSphere/") || !strings.Contains(ua, "mailto:dev@example.org") {
		t.Errorf("User-Agent = %q", ua)
	}

	// Without a mailto, none is sent.
	c2, fake2 := newTestClient(t, status(200, engineJSON))
	c2.mailto = ""
	if _, err := c2.FetchWork(t.Context(), "10.1000/x"); err != nil {
		t.Fatal(err)
	}
	if r := fake2.last.Load(); r.URL.Query().Has("mailto") || strings.Contains(r.Header.Get("User-Agent"), "mailto") {
		t.Errorf("request %s with User-Agent %q, want no mailto", r.URL, r.Header.Get("User-Agent"))
	}
}

func TestFetchWorkCancelled(t *testing.T) {
	c, fake := newTestClient(t, slow(time.Second))
	ctx, cancel := context.WithCancel(t.Context())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err := c.FetchWork(ctx, "10.1000/x")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if n := fake.calls.Load(); n != 1 {
		t.Errorf("requests = %d, want 1 (no retry once the caller gave up)", n)
	}
}

func TestRebuildAbstract(t *testing.T) {
	tests := []struct {
		name  string
		index map[string][]int
		want  string
	}{
		{"nil", nil, ""},
		{"empty", map[string][]int{}, ""},
		{"in order", map[string][]int{"A": {0}, "b": {1}, "c.": {2}}, "A b c."},
		{"out of order and repeated", map[string][]int{"the": {3, 0}, "cat": {1}, "saw": {2}, "dog": {4}}, "the cat saw the dog"},
		{"gaps are skipped", map[string][]int{"one": {0}, "three": {5}}, "one three"},
		{"negative positions are ignored", map[string][]int{"x": {-1}, "y": {0}}, "y"},
		{"two words at one position keep the first in byte order", map[string][]int{"b": {0}, "a": {0}, "c": {1}}, "a c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RebuildAbstract(tt.index); got != tt.want {
				t.Errorf("RebuildAbstract() = %q, want %q", got, tt.want)
			}
		})
	}
}
