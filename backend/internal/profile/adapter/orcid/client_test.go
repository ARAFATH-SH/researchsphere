package orcid

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

const orcidID = "0000-0002-1825-0097"

// worksJSON has a work with a DOI, one without a DOI or year, and one whose group has a
// second summary that is not read.
const worksJSON = `{
	"group": [
		{
			"external-ids": {"external-id": [{"external-id-type": "doi", "external-id-value": "10.1145/3292500.3330701", "external-id-relationship": "self"}]},
			"work-summary": [{
				"put-code": 1001,
				"title": {"title": {"value": "Notes on the Analytical Engine"}, "subtitle": null},
				"external-ids": {"external-id": [
					{"external-id-type": "isbn", "external-id-value": "978-3-16-148410-0", "external-id-relationship": "part-of"},
					{"external-id-type": "doi", "external-id-value": "10.1145/3292500.3330701", "external-id-relationship": "self"}
				]},
				"url": {"value": "https://dl.acm.org/doi/10.1145/3292500.3330701"},
				"type": "journal-article",
				"publication-date": {"year": {"value": "2019"}, "month": {"value": "08"}, "day": null},
				"journal-title": {"value": "Knowledge Discovery and Data Mining"}
			}, {
				"put-code": 1002,
				"title": {"title": {"value": "A duplicate from another source"}}
			}]
		},
		{
			"external-ids": {"external-id": []},
			"work-summary": [{
				"put-code": 2001,
				"title": {"title": {"value": "Sketch of the engine"}},
				"external-ids": {"external-id": []},
				"url": null,
				"type": "other",
				"publication-date": null,
				"journal-title": null
			}]
		}
	],
	"path": "/0000-0002-1825-0097/works"
}`

// fakeORCID serves the responses in order, one per request, repeating the last, and
// counts the requests.
type fakeORCID struct {
	responses []func(w http.ResponseWriter, r *http.Request)
	calls     atomic.Int32
	last      atomic.Pointer[http.Request]
}

func (f *fakeORCID) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
			status(http.StatusOK, worksJSON)(w, r)
		case <-r.Context().Done():
		}
	}
}

func newTestClient(t *testing.T, responses ...func(http.ResponseWriter, *http.Request)) (*Client, *fakeORCID) {
	t.Helper()
	fake := &fakeORCID{responses: responses}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return New(Config{BaseURL: srv.URL + "/", Timeout: 100 * time.Millisecond, RetryDelay: time.Millisecond}), fake
}

func ptr[T any](v T) *T { return &v }

// group wraps one work summary in a works list.
func group(summary string) string {
	return `{"group": [{"work-summary": [` + summary + `]}]}`
}

func TestListWorks(t *testing.T) {
	normal := []domain.ORCIDWork{
		{
			PutCode: "1001", Title: "Notes on the Analytical Engine", Year: ptr(2019),
			Venue: ptr("Knowledge Discovery and Data Mining"), DOI: ptr("10.1145/3292500.3330701"),
			Type: ptr("journal-article"), URL: ptr("https://dl.acm.org/doi/10.1145/3292500.3330701"),
		},
		{PutCode: "2001", Title: "Sketch of the engine", Type: ptr("other")},
	}
	type responses = []func(http.ResponseWriter, *http.Request)
	tests := []struct {
		name      string
		responses responses
		want      []domain.ORCIDWork
		wantErr   error
		wantCalls int32
	}{
		{"normal list, with and without DOI", responses{status(200, worksJSON)}, normal, nil, 1},
		{"empty list", responses{status(200, `{"group": []}`)}, []domain.ORCIDWork{}, nil, 1},
		{"missing year, year not a number", responses{status(200, `{"group": [
			{"work-summary": [{"put-code": 1, "title": {"title": {"value": "A"}}, "publication-date": {"year": null}}]},
			{"work-summary": [{"put-code": 2, "title": {"title": {"value": "B"}}, "publication-date": {"year": {"value": "20x9"}}}]}
		]}`)}, []domain.ORCIDWork{{PutCode: "1", Title: "A"}, {PutCode: "2", Title: "B"}}, nil, 1},
		{"work without a title is skipped", responses{status(200, `{"group": [
			{"work-summary": [{"put-code": 1, "title": null}]},
			{"work-summary": [{"put-code": 2, "title": {"title": {"value": "  "}}}]},
			{"work-summary": []},
			{"work-summary": [{"put-code": 3, "title": {"title": {"value": "Kept"}}}]}
		]}`)}, []domain.ORCIDWork{{PutCode: "3", Title: "Kept"}}, nil, 1},
		{"self DOI preferred over part-of", responses{status(200, group(`{"put-code": 1, "title": {"title": {"value": "Chapter"}},
			"external-ids": {"external-id": [
				{"external-id-type": "doi", "external-id-value": "10.1000/book", "external-id-relationship": "part-of"},
				{"external-id-type": "doi", "external-id-value": "10.1000/chapter", "external-id-relationship": "self"}
			]}}`))}, []domain.ORCIDWork{{PutCode: "1", Title: "Chapter", DOI: ptr("10.1000/chapter")}}, nil, 1},
		{"part-of DOI when there is no self one", responses{status(200, group(`{"put-code": 1, "title": {"title": {"value": "Chapter"}},
			"external-ids": {"external-id": [{"external-id-type": "DOI", "external-id-value": "10.1000/book", "external-id-relationship": "part-of"}]}}`))},
			[]domain.ORCIDWork{{PutCode: "1", Title: "Chapter", DOI: ptr("10.1000/book")}}, nil, 1},
		{"DOI from the group when the summary has none", responses{status(200, `{"group": [{
			"external-ids": {"external-id": [{"external-id-type": "doi", "external-id-value": "10.1000/g", "external-id-relationship": "self"}]},
			"work-summary": [{"put-code": 1, "title": {"title": {"value": "T"}}}]}]}`)},
			[]domain.ORCIDWork{{PutCode: "1", Title: "T", DOI: ptr("10.1000/g")}}, nil, 1},
		{"404", responses{status(404, `{"error-code": 9016}`)}, nil, domain.ErrORCIDNotFound, 1},
		{"500 then success on retry", responses{status(500, ``), status(200, worksJSON)}, normal, nil, 2},
		{"500 twice", responses{status(503, ``)}, nil, domain.ErrMetadataUnavailable, 2},
		{"timeout then success on retry", responses{slow(time.Second), status(200, worksJSON)}, normal, nil, 2},
		{"timeout twice", responses{slow(time.Second)}, nil, domain.ErrMetadataUnavailable, 2},
		{"400 is not retried", responses{status(400, `{}`), status(200, worksJSON)}, nil, domain.ErrMetadataUnavailable, 1},
		{"429 from ORCID is not retried", responses{status(429, `{}`), status(200, worksJSON)}, nil, domain.ErrMetadataUnavailable, 1},
		{"malformed JSON", responses{status(200, `{"group": [`)}, nil, domain.ErrMetadataUnavailable, 1},
		{"not a works list", responses{status(200, `{}`)}, nil, domain.ErrMetadataUnavailable, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, fake := newTestClient(t, tt.responses...)
			got, err := c.ListWorks(t.Context(), orcidID)
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

func TestListWorksRequest(t *testing.T) {
	c, fake := newTestClient(t, status(200, worksJSON))
	if _, err := c.ListWorks(t.Context(), orcidID); err != nil {
		t.Fatal(err)
	}
	r := fake.last.Load()
	if r.Method != http.MethodGet || r.URL.Path != "/v3.0/"+orcidID+"/works" {
		t.Errorf("request %s %s", r.Method, r.URL.Path)
	}
	if a := r.Header.Get("Accept"); a != "application/json" {
		t.Errorf("Accept = %q", a)
	}
	if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "ResearchSphere/") {
		t.Errorf("User-Agent = %q", ua)
	}
}

func TestListWorksCancelled(t *testing.T) {
	c, fake := newTestClient(t, slow(time.Second))
	ctx, cancel := context.WithCancel(t.Context())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err := c.ListWorks(ctx, orcidID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if n := fake.calls.Load(); n != 1 {
		t.Errorf("requests = %d, want 1 (no retry once the caller gave up)", n)
	}
}

func TestNewDefaults(t *testing.T) {
	c := New(Config{})
	if c.base != DefaultBaseURL || c.http.Timeout != DefaultTimeout || c.retryDelay != DefaultRetryDelay {
		t.Errorf("New(Config{}) = base %q, timeout %s, retry %s", c.base, c.http.Timeout, c.retryDelay)
	}
}
