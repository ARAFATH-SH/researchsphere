package http

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
)

const searchPath = "/api/v1/search/people"

// The parameters reach the usecase as sent; a missing page is page 1 (FR-6.1, FR-6.3).
func TestSearchPeopleParameters(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  usecase.SearchInput
	}{
		{"q only", "?q=ada", usecase.SearchInput{Q: ptr("ada"), Page: 1}},
		{"no q", "", usecase.SearchInput{Page: 1}},
		{"every filter", "?q=ada&institution=MIT&interest=NLP&interest=Eye+health&page=2",
			usecase.SearchInput{Q: ptr("ada"), Institution: ptr("MIT"), Interests: []string{"NLP", "Eye health"}, Page: 2}},
		{"page 0 is passed on, not defaulted", "?q=ada&page=0", usecase.SearchInput{Q: ptr("ada"), Page: 0}},
		{"mode keyword", "?q=ada&mode=keyword", usecase.SearchInput{Q: ptr("ada"), Mode: "keyword", Page: 1}},
		{"mode auto", "?q=ada&mode=auto", usecase.SearchInput{Q: ptr("ada"), Mode: "auto", Page: 1}},
		{"an unknown mode is passed on for the usecase to refuse", "?q=ada&mode=fast",
			usecase.SearchInput{Q: ptr("ada"), Mode: "fast", Page: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{}
			rec := send(t, svc, http.MethodGet, searchPath+tt.query, "", callerID)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
			}
			if svc.userID != callerID || !reflect.DeepEqual(svc.searchIn, tt.want) {
				t.Errorf("service got %q, %+v; want %+v", svc.userID, svc.searchIn, tt.want)
			}
		})
	}
}

const (
	searchRequestID = "00000000-0000-0000-0000-0000000000a9"
	thirdID         = "00000000-0000-0000-0000-000000000003"
)

// The body has every field, nulls for unset ones, the photo as a media path and empty
// lists, never null (FR-6.1, FR-6.2).
func TestSearchPeopleBody(t *testing.T) {
	svc := &fakeService{searchRes: domain.PeopleResults{
		Mode: domain.ResultHybrid,
		Hits: []domain.PersonHit{
			{UserID: otherID, FullName: "Mahmud Rahman", Headline: ptr("PhD student"), Institution: ptr("BUET"),
				PhotoFileID: ptr("11111111-1111-4111-8111-111111111111"), Interests: []string{"Eye health"}, MatchedOn: domain.MatchName,
				Connection: domain.ConnectionRelation{Status: domain.ConnectionPendingReceived, RequestID: ptr(searchRequestID)}},
			{UserID: callerID, FullName: "Ada Lovelace", MatchedOn: domain.MatchHeadline,
				Connection: domain.ConnectionRelation{Status: domain.ConnectionSelf}},
			{UserID: thirdID, FullName: "Nafisa Hasan", MatchedOn: domain.MatchMeaning,
				Connection: domain.ConnectionRelation{Status: domain.ConnectionNone}},
		},
		Total: 21, Fuzzy: true, CorrectedQuery: ptr("Mahmud Rahman"),
		Institutions: []domain.Facet{{Name: "BUET", Count: 12}},
		Interests:    []domain.Facet{{Name: "Eye health", Count: 3}},
	}}
	rec := send(t, svc, http.MethodGet, searchPath+"?q=Mahmod+Rahmn&page=2", "", callerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	want := map[string]any{
		"items": []any{
			map[string]any{"userId": otherID, "fullName": "Mahmud Rahman", "headline": "PhD student", "institution": "BUET",
				"photoUrl": "/api/v1/media/11111111-1111-4111-8111-111111111111", "interests": []any{"Eye health"}, "matchedOn": "name",
				"connectionStatus": "pending_received", "connectionRequestId": searchRequestID},
			map[string]any{"userId": callerID, "fullName": "Ada Lovelace", "headline": nil, "institution": nil,
				"photoUrl": nil, "interests": []any{}, "matchedOn": "headline",
				"connectionStatus": "self", "connectionRequestId": nil},
			map[string]any{"userId": thirdID, "fullName": "Nafisa Hasan", "headline": nil, "institution": nil,
				"photoUrl": nil, "interests": []any{}, "matchedOn": "meaning",
				"connectionStatus": "none", "connectionRequestId": nil},
		},
		"mode":  "hybrid",
		"total": float64(21), "page": float64(2), "pageSize": float64(20),
		"fuzzy": true, "correctedQuery": "Mahmud Rahman",
		"facets": map[string]any{
			"institutions": []any{map[string]any{"name": "BUET", "count": float64(12)}},
			"interests":    []any{map[string]any{"name": "Eye health", "count": float64(3)}},
		},
	}
	if got := decode(t, rec); !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v\nwant   %v", got, want)
	}
}

func TestSearchPeopleEmptyBody(t *testing.T) {
	svc := &fakeService{searchRes: domain.PeopleResults{Mode: domain.ResultKeywordFallback}}
	got := decode(t, send(t, svc, http.MethodGet, searchPath+"?q=zz", "", callerID))
	want := map[string]any{
		"mode":  "keyword_fallback",
		"items": []any{}, "total": float64(0), "page": float64(1), "pageSize": float64(20),
		"fuzzy": false, "correctedQuery": nil,
		"facets": map[string]any{"institutions": []any{}, "interests": []any{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v\nwant   %v", got, want)
	}
}

func TestSearchPeopleErrors(t *testing.T) {
	cases := []errorCase{
		{"invalid query", domain.ErrInvalidQuery, http.StatusBadRequest, CodeInvalidQuery, false},
		{"invalid mode", domain.ErrInvalidSearchMode, http.StatusBadRequest, httpx.CodeBadRequest, false},
		{"invalid filter", domain.ErrInvalidSearchFilter, http.StatusBadRequest, httpx.CodeBadRequest, false},
		{"invalid page", domain.ErrInvalidPage, http.StatusBadRequest, httpx.CodeBadRequest, false},
		{"rate limited", &domain.RateLimitedError{RetryAfter: 1500 * time.Millisecond}, http.StatusTooManyRequests, CodeRateLimited, false},
		{"no user in the context", nil, http.StatusUnauthorized, httpx.CodeUnauthenticated, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			caller := callerID
			if c.noCaller {
				caller = ""
			}
			rec := send(t, &fakeService{err: c.err}, http.MethodGet, searchPath+"?q=ada", "", caller)
			if rec.Code != c.status {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, c.status, rec.Body)
			}
			if got := decode(t, rec)["code"]; got != c.code {
				t.Errorf("code = %v, want %s", got, c.code)
			}
			if c.status == http.StatusTooManyRequests && rec.Header().Get("Retry-After") != "2" {
				t.Errorf("Retry-After = %q, want 2 (rounded up)", rec.Header().Get("Retry-After"))
			}
		})
	}
}

// A page that is not a whole number never reaches the usecase.
func TestSearchPeopleMalformedPage(t *testing.T) {
	svc := &fakeService{}
	rec := send(t, svc, http.MethodGet, searchPath+"?q=ada&page=two", "", callerID)
	if rec.Code != http.StatusBadRequest || svc.userID != "" {
		t.Errorf("status = %d, service called = %v; want 400 and no call", rec.Code, svc.userID != "")
	}
}

// The invalid_query message names the limit of the mode asked for; a bad mode names its field.
func TestSearchPeopleErrorMessages(t *testing.T) {
	tests := []struct {
		query  string
		svcErr error
		want   map[string]any
	}{
		{"?q=a", domain.ErrInvalidQuery,
			map[string]any{"code": CodeInvalidQuery, "message": "Enter 2 to 300 characters to search."}},
		{"?q=a&mode=keyword", domain.ErrInvalidQuery,
			map[string]any{"code": CodeInvalidQuery, "message": "Enter 2 to 100 characters to search by keyword."}},
		{"?q=ada&mode=fast", domain.ErrInvalidSearchMode,
			map[string]any{"code": httpx.CodeBadRequest, "message": "The search mode must be auto or keyword.", "field": "mode"}},
	}
	for _, tt := range tests {
		rec := send(t, &fakeService{err: tt.svcErr}, http.MethodGet, searchPath+tt.query, "", callerID)
		if got := decode(t, rec); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: body = %v, want %v", tt.query, got, tt.want)
		}
	}
}
