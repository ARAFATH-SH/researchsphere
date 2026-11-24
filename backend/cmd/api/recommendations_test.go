package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	recommendationdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
	recommendationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/usecase"
)

// okRecommendations answers with one fixed recommendation, and refuses a limit below 1 as
// the usecase does.
type okRecommendations struct{}

func (okRecommendations) People(_ context.Context, _ string, limit int) (recommendationusecase.Result, error) {
	if limit < 1 {
		return recommendationusecase.Result{}, recommendationdomain.ErrInvalidLimit
	}
	return recommendationusecase.Result{State: recommendationdomain.StateReady, Items: []recommendationdomain.Recommendation{{
		Person: recommendationdomain.Person{UserID: "22222222-2222-4222-8222-222222222222", FullName: "Nafisa Hasan"},
		Score:  87, Shared: []string{"Remote sensing"}, Connection: recommendationdomain.StatusNone,
	}}}, nil
}

func (okRecommendations) Projects(_ context.Context, _ string, limit int) (recommendationusecase.ProjectResult, error) {
	if limit < 1 {
		return recommendationusecase.ProjectResult{}, recommendationdomain.ErrInvalidLimit
	}
	return recommendationusecase.ProjectResult{State: recommendationdomain.StateReady, Items: []recommendationdomain.ProjectRecommendation{{
		Project: recommendationdomain.ProjectCard{
			ID: "33333333-3333-4333-8333-333333333333", Title: "Flood maps", Description: "Mapping floods", Skills: []string{"Python"},
			Owner:       recommendationdomain.ProjectOwner{UserID: "22222222-2222-4222-8222-222222222222", FullName: "Nafisa Hasan"},
			MemberCount: 2,
		},
		Score: 91, Shared: []string{"Python"},
	}}}, nil
}

// okRecommendations lets the caller see okProjectID's researchers; forbiddenProjectID's
// owner is someone else, and any other project is hidden.
const forbiddenProjectID = "44444444-4444-4444-8444-444444444444"

func (okRecommendations) ProjectResearchers(_ context.Context, _, projectID string) (recommendationusecase.ResearcherResult, error) {
	switch projectID {
	case okProjectID:
		return recommendationusecase.ResearcherResult{State: recommendationdomain.StateProcessing, Items: []recommendationdomain.Recommendation{}}, nil
	case forbiddenProjectID:
		return recommendationusecase.ResearcherResult{}, recommendationdomain.ErrNotProjectOwner
	}
	return recommendationusecase.ResearcherResult{}, recommendationdomain.ErrProjectNotFound
}

// okRecommendations shows okProjectID's venues: one conference with an AoE deadline and one
// rolling journal. Any other project is not found (a non-member gets the same).
func (okRecommendations) ProjectVenues(_ context.Context, _, projectID string) (recommendationusecase.VenueResult, error) {
	if projectID != okProjectID {
		return recommendationusecase.VenueResult{}, recommendationdomain.ErrProjectNotFound
	}
	deadline := time.Date(2027, 3, 2, 11, 59, 59, 0, time.UTC) // 2027-03-01 23:59:59 AoE
	aoe, short, days := "AoE", "ICFM", 96
	card := func(id, name, typ string) recommendationdomain.VenueCard {
		return recommendationdomain.VenueCard{ID: id, Name: name, Type: typ,
			Fields:      []recommendationdomain.VenueField{{Key: "water_resources", Label: "Water resources and hydrology"}},
			OfficialURL: "https://example.org/" + typ, LinkKind: "official"}
	}
	conf := card("66666666-6666-4666-8666-666666666666", "International Conference on Flood Mapping", "conference")
	conf.ShortName, conf.TimeZone, conf.Deadline = &short, &aoe, &deadline
	return recommendationusecase.VenueResult{
		State:    recommendationdomain.StateReady,
		Venues:   []recommendationdomain.VenueRecommendation{{VenueCard: conf, Score: 81, DaysLeft: &days}},
		Journals: []recommendationdomain.VenueRecommendation{{VenueCard: card("77777777-7777-4777-8777-777777777777", "Journal of Hydrology", "journal"), Score: 64}},
		Notice:   recommendationdomain.VenueNotice,
	}, nil
}

// AI-6 route: a verified session; a project the caller is not a member of is 404. The
// deadline shows its instant and its wall time in the stated zone (AoE = UTC-12), and a
// rolling journal has no deadline.
func TestProjectVenueRoute(t *testing.T) {
	router := testRouter()
	researcher := accessCookie(t, authz.RoleResearcher)
	for _, tt := range []struct {
		name, path string
		cookie     *http.Cookie
		want       int
		wantBody   string
	}{
		{"no session", "/api/v1/projects/" + okProjectID + "/recommended-venues", nil, http.StatusUnauthorized, `"code":"unauthenticated"`},
		{"member", "/api/v1/projects/" + okProjectID + "/recommended-venues", researcher, http.StatusOK,
			`{"journals":[{"daysLeft":null,"fields":[{"key":"water_resources","label":"Water resources and hydrology"}],"id":"77777777-7777-4777-8777-777777777777","linkKind":"official","name":"Journal of Hydrology","officialUrl":"https://example.org/journal","score":64,"shortName":null,"submissionDeadline":null,"timeZone":null,"type":"journal"}],` +
				`"notice":"Matched on topic only. This says nothing about a venue's quality or ranking.","state":"ready",` +
				`"venues":[{"daysLeft":96,"fields":[{"key":"water_resources","label":"Water resources and hydrology"}],"id":"66666666-6666-4666-8666-666666666666","linkKind":"official","name":"International Conference on Flood Mapping","officialUrl":"https://example.org/conference","score":81,"shortName":"ICFM","submissionDeadline":{"at":"2027-03-02T11:59:59Z","local":"2027-03-01T23:59:59"},"timeZone":"AoE","type":"conference"}]}`},
		{"not a member", "/api/v1/projects/" + forbiddenProjectID + "/recommended-venues", researcher, http.StatusNotFound, `"code":"not_found"`},
		{"not a UUID", "/api/v1/projects/nope/recommended-venues", researcher, http.StatusNotFound, `"code":"not_found"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.want, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("body = %s, want it to contain %s", rec.Body, tt.wantBody)
			}
		})
	}
}

// AI-3.1 and AI-2.3 routes: a verified session and "View profiles, search, feed, events and
// recommendations"; the project's owner only (403), a hidden project 404.
func TestProjectRecommendationRoutes(t *testing.T) {
	router := testRouter()
	unverified := func() *http.Cookie {
		token, _, err := accesstoken.New([]byte(testSecret), accessTTL).Sign(okUserID, string(authz.RoleResearcher), false, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: httpx.AccessCookie, Value: token, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
	}
	researcher := accessCookie(t, authz.RoleResearcher)
	for _, tt := range []struct {
		name, path string
		cookie     *http.Cookie
		want       int
		wantBody   string
	}{
		{"projects, no session", "/api/v1/recommendations/projects", nil, http.StatusUnauthorized, `"code":"unauthenticated"`},
		{"projects, unverified", "/api/v1/recommendations/projects", unverified(), http.StatusForbidden, `"code":"email_not_verified"`},
		{"projects", "/api/v1/recommendations/projects", researcher, http.StatusOK,
			`{"items":[{"project":{"description":"Mapping floods","id":"33333333-3333-4333-8333-333333333333","memberCount":2,"owner":{"fullName":"Nafisa Hasan","headline":null,"photoUrl":null,"userId":"22222222-2222-4222-8222-222222222222"},"requiredSkills":["Python"],"title":"Flood maps"},"score":91,"shared":["Python"]}],"missing":[],"state":"ready"}`},
		{"projects, limit 0", "/api/v1/recommendations/projects?limit=0", researcher, http.StatusBadRequest, `"field":"limit"`},
		{"researchers, no session", "/api/v1/projects/" + okProjectID + "/recommended-researchers", nil, http.StatusUnauthorized, `"code":"unauthenticated"`},
		{"researchers, unverified", "/api/v1/projects/" + okProjectID + "/recommended-researchers", unverified(), http.StatusForbidden, `"code":"email_not_verified"`},
		{"researchers, owner", "/api/v1/projects/" + okProjectID + "/recommended-researchers", researcher, http.StatusOK, `{"items":[],"missing":[],"state":"processing"}`},
		{"researchers, not the owner", "/api/v1/projects/" + forbiddenProjectID + "/recommended-researchers", researcher, http.StatusForbidden, `"code":"forbidden"`},
		{"researchers, hidden project", "/api/v1/projects/55555555-5555-4555-8555-555555555555/recommended-researchers", researcher, http.StatusNotFound, `"code":"not_found"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.want, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("body = %s, want it to contain %s", rec.Body, tt.wantBody)
			}
		})
	}
}

// The route needs a verified session and the "View profiles, search, feed, events and
// recommendations" action (SRS 2.3); limit below 1 is 400 with the field named.
func TestRecommendPeopleRoute(t *testing.T) {
	router := testRouter()
	unverified := func() *http.Cookie {
		token, _, err := accesstoken.New([]byte(testSecret), accessTTL).Sign(okUserID, string(authz.RoleResearcher), false, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: httpx.AccessCookie, Value: token, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
	}
	tests := []struct {
		name     string
		query    string
		cookie   *http.Cookie
		want     int
		wantBody string
	}{
		{"no session", "", nil, http.StatusUnauthorized, `"code":"unauthenticated"`},
		{"unverified email", "", unverified(), http.StatusForbidden, `"code":"email_not_verified"`},
		{"researcher", "", accessCookie(t, authz.RoleResearcher), http.StatusOK,
			`{"items":[{"connectionStatus":"none","fullName":"Nafisa Hasan","headline":null,"institution":null,"photoUrl":null,"score":87,"shared":["Remote sensing"],"userId":"22222222-2222-4222-8222-222222222222"}],"missing":[],"state":"ready"}`},
		{"administrator", "?limit=3", accessCookie(t, authz.RoleAdmin), http.StatusOK, `"state":"ready"`},
		{"limit 0", "?limit=0", accessCookie(t, authz.RoleResearcher), http.StatusBadRequest, `"field":"limit"`},
		{"limit not a number", "?limit=ten", accessCookie(t, authz.RoleResearcher), http.StatusBadRequest, `"code":"bad_request"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/recommendations/people"+tt.query, nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.want, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("body = %s, want it to contain %s", rec.Body, tt.wantBody)
			}
		})
	}
}
