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
