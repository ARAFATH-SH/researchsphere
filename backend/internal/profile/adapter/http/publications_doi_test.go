package http

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
)

const (
	lookupPath  = "/api/v1/publications/lookup"
	fromDOIPath = publicationsPath + "/from-doi"
)

// The FR-5.6 message the web client shows when OpenAlex fails.
const unavailableMessage = "We couldn't reach the publication database. You can add this publication by hand."

// The errors both DOI operations share, and what they become.
var doiErrors = []errorCase{
	{"invalid DOI", domain.ErrInvalidDOI, http.StatusBadRequest, CodeInvalidDOI, false},
	{"not found", domain.ErrWorkNotFound, http.StatusNotFound, CodeDOINotFound, false},
	{"unavailable", fmt.Errorf("wrapped: %w", domain.ErrMetadataUnavailable), http.StatusServiceUnavailable, CodeMetadataUnavailable, false},
	{"rate limited", &domain.RateLimitedError{RetryAfter: time.Minute}, http.StatusTooManyRequests, CodeRateLimited, false},
	{"no user in the context", nil, http.StatusUnauthorized, httpx.CodeUnauthenticated, true},
}

func TestLookupPublicationByDOI(t *testing.T) {
	t.Run("200 with a fetched preview", func(t *testing.T) {
		svc := &fakeService{preview: usecase.DOIPreview{DOI: "10.1000/xyz", Title: "Notes", Year: &year2024, URL: ptr("https://doi.org/10.1000/xyz")}}
		rec := send(t, svc, http.MethodGet, lookupPath+"?doi="+url.QueryEscape("https://doi.org/10.1000/XYZ"), "", callerID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
		}
		if svc.userID != callerID || svc.doi != "https://doi.org/10.1000/XYZ" {
			t.Errorf("service got %q, %q", svc.userID, svc.doi)
		}
		want := map[string]any{
			"doi": "10.1000/xyz", "title": "Notes", "year": float64(2024), "venue": nil, "abstract": nil,
			"url": "https://doi.org/10.1000/xyz", "existsAlready": false, "linkedToMe": false,
		}
		if got := decode(t, rec); !reflect.DeepEqual(got, want) {
			t.Errorf("body = %v, want %v", got, want)
		}
	})

	t.Run("200 with a stored record", func(t *testing.T) {
		svc := &fakeService{preview: usecase.DOIPreview{DOI: "10.1000/xyz", Title: "Notes", ExistsAlready: true, PublicationID: publicationID, LinkedToMe: true}}
		got := decode(t, send(t, svc, http.MethodGet, lookupPath+"?doi=10.1000/xyz", "", callerID))
		if got["existsAlready"] != true || got["publicationId"] != publicationID || got["linkedToMe"] != true {
			t.Errorf("body = %v", got)
		}
	})

	t.Run("a missing doi parameter reaches the usecase as empty", func(t *testing.T) {
		svc := &fakeService{doi: "unset", err: domain.ErrInvalidDOI}
		rec := send(t, svc, http.MethodGet, lookupPath, "", callerID)
		if rec.Code != http.StatusBadRequest || svc.doi != "" || decode(t, rec)["code"] != CodeInvalidDOI {
			t.Errorf("status = %d, doi %q; want 400 invalid_doi with an empty DOI", rec.Code, svc.doi)
		}
	})

	checkErrors(t, doiErrors, http.MethodGet, lookupPath+"?doi=10.1000/xyz", "")
	checkDOIErrorDetails(t, http.MethodGet, lookupPath+"?doi=10.1000/xyz", "")
}

func TestAddMyPublicationFromDOI(t *testing.T) {
	for _, linked := range []bool{false, true} {
		t.Run("201 with the record and linked", func(t *testing.T) {
			doiNotes := notes
			doiNotes.Source = domain.SourceDOI
			svc := &fakeService{publication: doiNotes, linked: linked}
			rec := send(t, svc, http.MethodPost, fromDOIPath, `{"doi":"doi:10.1000/XYZ"}`, callerID)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body)
			}
			if svc.userID != callerID || svc.doi != "doi:10.1000/XYZ" {
				t.Errorf("service got %q, %q", svc.userID, svc.doi)
			}
			wantBody := map[string]any{"linked": linked}
			for k, v := range notesBody {
				wantBody[k] = v
			}
			wantBody["source"] = "doi"
			if got := decode(t, rec); !reflect.DeepEqual(got, wantBody) {
				t.Errorf("body = %v, want %v", got, wantBody)
			}
		})
	}

	t.Run("a body that is not JSON", func(t *testing.T) {
		if rec := send(t, &fakeService{}, http.MethodPost, fromDOIPath, `{`, callerID); rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	checkErrors(t, append([]errorCase{
		{"limit reached", domain.ErrPublicationLimitReached, http.StatusConflict, CodePublicationLimitReached, false},
		{"already added", domain.ErrPublicationAlreadyAdded, http.StatusConflict, CodePublicationAlreadyAdded, false},
		{"account no longer exists", domain.ErrNotFound, http.StatusUnauthorized, httpx.CodeUnauthenticated, false},
	}, doiErrors...), http.MethodPost, fromDOIPath, `{"doi":"10.1000/xyz"}`)
	checkDOIErrorDetails(t, http.MethodPost, fromDOIPath, `{"doi":"10.1000/xyz"}`)
}

// checkDOIErrorDetails checks the FR-5.6 message and the Retry-After header.
func checkDOIErrorDetails(t *testing.T, method, path, body string) {
	t.Helper()
	rec := send(t, &fakeService{err: domain.ErrMetadataUnavailable}, method, path, body, callerID)
	if got := decode(t, rec); got["message"] != unavailableMessage {
		t.Errorf("503 message = %q, want %q", got["message"], unavailableMessage)
	}
	for _, tt := range []struct {
		left time.Duration
		want string
	}{{90 * time.Second, "90"}, {1500 * time.Millisecond, "2"}, {0, "1"}} {
		rec := send(t, &fakeService{err: &domain.RateLimitedError{RetryAfter: tt.left}}, method, path, body, callerID)
		if got := rec.Header().Get("Retry-After"); got != tt.want {
			t.Errorf("left %s: Retry-After = %q, want %q", tt.left, got, tt.want)
		}
	}
}
