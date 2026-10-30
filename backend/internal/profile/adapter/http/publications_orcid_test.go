package http

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
)

const (
	orcidPath      = "/api/v1/profiles/me/orcid"
	orcidWorksPath = publicationsPath + "/orcid-works"
	fromORCIDPath  = publicationsPath + "/from-orcid"
)

// The FR-5.6 message the web client shows when ORCID fails.
const orcidUnavailableMessage = "We couldn't reach ORCID. You can still add publications by DOI or by hand."

// The errors both ORCID publication operations share, and what they become.
var orcidErrors = []errorCase{
	{"no ORCID iD on the profile", domain.ErrORCIDNotSet, http.StatusConflict, CodeORCIDNotSet, false},
	{"ORCID does not know the iD", domain.ErrORCIDNotFound, http.StatusNotFound, CodeORCIDNotFound, false},
	{"ORCID unavailable", fmt.Errorf("wrapped: %w", domain.ErrMetadataUnavailable), http.StatusServiceUnavailable, CodeMetadataUnavailable, false},
	{"rate limited", &domain.RateLimitedError{RetryAfter: time.Minute}, http.StatusTooManyRequests, CodeRateLimited, false},
	{"account no longer exists", domain.ErrNotFound, http.StatusUnauthorized, httpx.CodeUnauthenticated, false},
	{"no user in the context", nil, http.StatusUnauthorized, httpx.CodeUnauthenticated, true},
}

func TestSetMyOrcidId(t *testing.T) {
	t.Run("200 with the saved iD", func(t *testing.T) {
		svc := &fakeService{orcidSaved: ptr("0000-0002-1825-0097")}
		rec := send(t, svc, http.MethodPut, orcidPath, `{"orcidId":"https://orcid.org/0000-0002-1825-0097"}`, callerID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
		}
		if svc.userID != callerID || ptrValue(svc.orcidIn) != "https://orcid.org/0000-0002-1825-0097" {
			t.Errorf("service got %q, %q", svc.userID, ptrValue(svc.orcidIn))
		}
		if got := decode(t, rec); got["orcidId"] != "0000-0002-1825-0097" {
			t.Errorf("body = %v", got)
		}
	})

	t.Run("null clears it", func(t *testing.T) {
		svc := &fakeService{orcidIn: ptr("unset")}
		rec := send(t, svc, http.MethodPut, orcidPath, `{"orcidId":null}`, callerID)
		if got := decode(t, rec); rec.Code != http.StatusOK || svc.orcidIn != nil || got["orcidId"] != nil {
			t.Errorf("status = %d, body %v, service got %v; want 200 with null", rec.Code, got, svc.orcidIn)
		}
	})

	t.Run("a body that is not JSON", func(t *testing.T) {
		if rec := send(t, &fakeService{}, http.MethodPut, orcidPath, `{`, callerID); rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	checkErrors(t, []errorCase{
		{"invalid iD", domain.ErrInvalidORCID, http.StatusBadRequest, CodeInvalidORCIDID, false},
		{"taken", domain.ErrORCIDTaken, http.StatusConflict, CodeORCIDTaken, false},
		{"account no longer exists", domain.ErrNotFound, http.StatusUnauthorized, httpx.CodeUnauthenticated, false},
		{"no user in the context", nil, http.StatusUnauthorized, httpx.CodeUnauthenticated, true},
	}, http.MethodPut, orcidPath, `{"orcidId":"0000-0002-1825-0097"}`)
}

func TestListMyOrcidWorks(t *testing.T) {
	svc := &fakeService{orcidList: usecase.ORCIDWorkList{OrcidID: "0000-0002-1825-0097", Items: []usecase.ORCIDWorkItem{
		{ORCIDWork: domain.ORCIDWork{PutCode: "1", Title: "Notes", Year: &year2024, Venue: ptr("KDD"), DOI: ptr("10.1000/xyz"), Type: ptr("journal-article"), URL: ptr("https://doi.org/10.1000/xyz")},
			ExistsAlready: true, PublicationID: publicationID, LinkedToMe: true},
		{ORCIDWork: domain.ORCIDWork{PutCode: "2", Title: "Sketch"}},
	}}}
	rec := send(t, svc, http.MethodGet, orcidWorksPath, "", callerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if svc.userID != callerID {
		t.Errorf("service got %q", svc.userID)
	}
	want := map[string]any{"orcidId": "0000-0002-1825-0097", "items": []any{
		map[string]any{"putCode": "1", "title": "Notes", "year": float64(2024), "venue": "KDD", "doi": "10.1000/xyz",
			"type": "journal-article", "url": "https://doi.org/10.1000/xyz", "existsAlready": true, "publicationId": publicationID, "linkedToMe": true},
		map[string]any{"putCode": "2", "title": "Sketch", "year": nil, "venue": nil, "doi": nil, "type": nil, "url": nil,
			"existsAlready": false, "linkedToMe": false},
	}}
	if got := decode(t, rec); !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}

	checkErrors(t, orcidErrors, http.MethodGet, orcidWorksPath, "")
	checkORCIDErrorDetails(t, http.MethodGet, orcidWorksPath, "")
}

func TestImportMyOrcidWorks(t *testing.T) {
	svc := &fakeService{importResult: usecase.ImportResult{
		Items: []usecase.ImportedWork{
			{PutCode: "1", Outcome: usecase.ImportCreated, PublicationID: publicationID},
			{PutCode: "2", Outcome: usecase.ImportSkipped, PublicationID: publicationID},
		},
		Created: 1, Skipped: 1,
	}}
	rec := send(t, svc, http.MethodPost, fromORCIDPath, `{"putCodes":["1","2"]}`, callerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if svc.userID != callerID || !reflect.DeepEqual(svc.putCodes, []string{"1", "2"}) {
		t.Errorf("service got %q, %v", svc.userID, svc.putCodes)
	}
	want := map[string]any{"created": float64(1), "linked": float64(0), "skipped": float64(1), "items": []any{
		map[string]any{"putCode": "1", "result": "created", "publicationId": publicationID},
		map[string]any{"putCode": "2", "result": "skipped", "publicationId": publicationID},
	}}
	if got := decode(t, rec); !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}

	t.Run("a body that is not JSON", func(t *testing.T) {
		if rec := send(t, &fakeService{}, http.MethodPost, fromORCIDPath, `{`, callerID); rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	checkErrors(t, append([]errorCase{
		{"invalid put-code", domain.ErrInvalidPutCode, http.StatusBadRequest, CodeInvalidPutCode, false},
		{"limit reached", domain.ErrPublicationLimitReached, http.StatusConflict, CodePublicationLimitReached, false},
	}, orcidErrors...), http.MethodPost, fromORCIDPath, `{"putCodes":["1"]}`)
	checkORCIDErrorDetails(t, http.MethodPost, fromORCIDPath, `{"putCodes":["1"]}`)
}

// checkORCIDErrorDetails checks the FR-5.6 message and the Retry-After header.
func checkORCIDErrorDetails(t *testing.T, method, path, body string) {
	t.Helper()
	rec := send(t, &fakeService{err: domain.ErrMetadataUnavailable}, method, path, body, callerID)
	if got := decode(t, rec); got["message"] != orcidUnavailableMessage {
		t.Errorf("503 message = %q, want %q", got["message"], orcidUnavailableMessage)
	}
	rec = send(t, &fakeService{err: &domain.RateLimitedError{RetryAfter: 90 * time.Second}}, method, path, body, callerID)
	if got := rec.Header().Get("Retry-After"); got != "90" {
		t.Errorf("Retry-After = %q, want 90", got)
	}
}

func ptrValue(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// The ORCID iD is a public identifier: another user's profile shows it.
func TestGetProfileShowsTheORCIDiD(t *testing.T) {
	svc := &fakeService{profile: domain.Profile{UserID: otherID, FullName: "Ada", OrcidID: ptr("0000-0002-1825-0097")}}
	rec := send(t, svc, http.MethodGet, "/api/v1/profiles/"+otherID, "", callerID)
	if got := decode(t, rec); rec.Code != http.StatusOK || got["orcidId"] != "0000-0002-1825-0097" {
		t.Errorf("status = %d, orcidId %v", rec.Code, got["orcidId"])
	}
}
