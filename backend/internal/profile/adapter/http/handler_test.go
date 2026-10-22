package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
)

const (
	callerID = "00000000-0000-0000-0000-000000000001"
	otherID  = "00000000-0000-0000-0000-000000000002"
)

// fakeService returns profile and err from every operation and records what it was given.
type fakeService struct {
	profile domain.Profile
	err     error
	userID  string
	update  usecase.UpdateInput

	tag         domain.Tag // returned by AddTag
	created     bool       // returned by AddTag
	suggestions []domain.Tag
	kind        domain.TagKind // given to the tag operations
	name        string         // given to AddTag
	tagID       string         // given to RemoveTag
	q           string         // given to SuggestTags
	limit       int            // given to SuggestTags

	education      domain.Education       // returned by the education operations
	educationIn    usecase.EducationInput // given to AddEducation
	educationPatch usecase.EducationPatch // given to UpdateEducation
	educationID    string                 // given to UpdateEducation and RemoveEducation

	experience      domain.Experience       // returned by the experience operations
	experienceIn    usecase.ExperienceInput // given to AddExperience
	experiencePatch usecase.ExperiencePatch // given to UpdateExperience
	experienceID    string                  // given to UpdateExperience and RemoveExperience

	viewerID  string               // given to View
	contact   domain.Contact       // returned by GetContact and UpdateContact
	contactIn usecase.ContactInput // given to UpdateContact
	link      domain.Link          // returned by AddLink
	linkKind  domain.LinkKind      // given to AddLink
	linkURL   string               // given to AddLink
	linkID    string               // given to RemoveLink
}

func (f *fakeService) GetContact(_ context.Context, userID string) (domain.Contact, error) {
	f.userID = userID
	return f.contact, f.err
}

func (f *fakeService) UpdateContact(_ context.Context, userID string, in usecase.ContactInput) (domain.Contact, error) {
	f.userID, f.contactIn = userID, in
	return f.contact, f.err
}

func (f *fakeService) AddLink(_ context.Context, userID string, kind domain.LinkKind, url string) (domain.Link, error) {
	f.userID, f.linkKind, f.linkURL = userID, kind, url
	return f.link, f.err
}

func (f *fakeService) RemoveLink(_ context.Context, userID, id string) error {
	f.userID, f.linkID = userID, id
	return f.err
}

func (f *fakeService) AddEducation(_ context.Context, userID string, in usecase.EducationInput) (domain.Education, error) {
	f.userID, f.educationIn = userID, in
	return f.education, f.err
}

func (f *fakeService) UpdateEducation(_ context.Context, userID, id string, patch usecase.EducationPatch) (domain.Education, error) {
	f.userID, f.educationID, f.educationPatch = userID, id, patch
	return f.education, f.err
}

func (f *fakeService) RemoveEducation(_ context.Context, userID, id string) error {
	f.userID, f.educationID = userID, id
	return f.err
}

func (f *fakeService) AddExperience(_ context.Context, userID string, in usecase.ExperienceInput) (domain.Experience, error) {
	f.userID, f.experienceIn = userID, in
	return f.experience, f.err
}

func (f *fakeService) UpdateExperience(_ context.Context, userID, id string, patch usecase.ExperiencePatch) (domain.Experience, error) {
	f.userID, f.experienceID, f.experiencePatch = userID, id, patch
	return f.experience, f.err
}

func (f *fakeService) RemoveExperience(_ context.Context, userID, id string) error {
	f.userID, f.experienceID = userID, id
	return f.err
}

func (f *fakeService) AddTag(_ context.Context, userID string, kind domain.TagKind, name string) (domain.Tag, bool, error) {
	f.userID, f.kind, f.name = userID, kind, name
	return f.tag, f.created, f.err
}

func (f *fakeService) RemoveTag(_ context.Context, userID string, kind domain.TagKind, tagID string) error {
	f.userID, f.kind, f.tagID = userID, kind, tagID
	return f.err
}

func (f *fakeService) SuggestTags(_ context.Context, kind domain.TagKind, q string, limit int) ([]domain.Tag, error) {
	f.kind, f.q, f.limit = kind, q, limit
	return f.suggestions, f.err
}

func (f *fakeService) GetOwn(_ context.Context, userID string) (domain.Profile, error) {
	f.userID = userID
	return f.profile, f.err
}

func (f *fakeService) UpdateOwn(_ context.Context, userID string, in usecase.UpdateInput) (domain.Profile, error) {
	f.userID, f.update = userID, in
	return f.profile, f.err
}

func (f *fakeService) View(_ context.Context, viewerID, userID string) (domain.Profile, error) {
	f.viewerID, f.userID = viewerID, userID
	return f.profile, f.err
}

// server serves only the profile operations: Handler's methods are promoted from a
// shallower depth than the embedded interface's, which stays nil.
type server struct {
	*Handler
	others
}

type others struct{ gen.StrictServerInterface }

// send serves one request as the caller, or with no user in the context when caller is
// empty (the auth middleware would have refused it before).
func send(t *testing.T, svc *fakeService, method, path, body, caller string) *httptest.ResponseRecorder {
	t.Helper()
	ctx := t.Context()
	if caller != "" {
		ctx = httpx.WithUserID(ctx, caller) // what the auth middleware does
	}
	req := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	mux := http.NewServeMux()
	gen.HandlerWithOptions(gen.NewStrictHandlerWithOptions(server{Handler: NewHandler(svc)}, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.WriteHeader(http.StatusBadRequest)
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	}), gen.StdHTTPServerOptions{BaseURL: "/api/v1", BaseRouter: mux})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body, err)
	}
	return got
}

func ptr(s string) *string { return &s }

var ada = domain.Profile{
	UserID: callerID, FullName: "Ada Lovelace", Headline: ptr("Analyst"), Country: ptr("GB"),
}

// The body has every field, nulls for unset ones, the placeholders (FR-4.5) and the
// contact info the viewer may see, here only the empty list of links (FR-4.7).
func checkProfileBody(t *testing.T, rec *httptest.ResponseRecorder, wantID string) {
	t.Helper()
	got := decode(t, rec)
	want := map[string]any{
		"userId": wantID, "fullName": "Ada Lovelace", "headline": "Analyst", "bio": nil,
		"institution": nil, "department": nil, "jobTitle": nil, "country": "GB",
		"interests": []any{}, "skills": []any{}, "education": []any{}, "experience": []any{}, "publications": []any{},
		"connectionStatus": "none", "contact": map[string]any{"links": []any{}},
		"photoUrl": nil, "coverUrl": nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v\nwant   %v", got, want)
	}
}

// The photo and cover (FR-4.6) are paths to the media endpoint, never presigned URLs.
func TestProfileImageURLs(t *testing.T) {
	p := ada
	p.PhotoFileID, p.CoverFileID = ptr("11111111-1111-4111-8111-111111111111"), ptr("22222222-2222-4222-8222-222222222222")
	for _, path := range []string{"/api/v1/profiles/me", "/api/v1/profiles/" + otherID} {
		got := decode(t, send(t, &fakeService{profile: p}, http.MethodGet, path, "", callerID))
		if got["photoUrl"] != "/api/v1/media/11111111-1111-4111-8111-111111111111" ||
			got["coverUrl"] != "/api/v1/media/22222222-2222-4222-8222-222222222222" {
			t.Errorf("GET %s: photoUrl %v, coverUrl %v", path, got["photoUrl"], got["coverUrl"])
		}
	}
}

func TestGetMyProfile(t *testing.T) {
	t.Run("logged in", func(t *testing.T) {
		svc := &fakeService{profile: ada}
		rec := send(t, svc, http.MethodGet, "/api/v1/profiles/me", "", callerID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
		}
		if svc.userID != callerID {
			t.Errorf("service asked for %q, want the caller %q", svc.userID, callerID)
		}
		checkProfileBody(t, rec, callerID)
	})

	t.Run("no profile row gives an empty profile", func(t *testing.T) {
		rec := send(t, &fakeService{profile: domain.Profile{UserID: callerID}}, http.MethodGet, "/api/v1/profiles/me", "", callerID)
		got := decode(t, rec)
		if rec.Code != http.StatusOK || got["fullName"] != "" || got["headline"] != nil {
			t.Errorf("got %d %v, want 200 with an empty profile", rec.Code, got)
		}
	})

	for name, tt := range map[string]struct {
		svc    *fakeService
		caller string
	}{
		"account no longer exists": {&fakeService{err: domain.ErrNotFound}, callerID},
		"no user in the context":   {&fakeService{}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			rec := send(t, tt.svc, http.MethodGet, "/api/v1/profiles/me", "", tt.caller)
			if rec.Code != http.StatusUnauthorized || decode(t, rec)["code"] != httpx.CodeUnauthenticated {
				t.Errorf("got %d %s, want 401 unauthenticated", rec.Code, rec.Body)
			}
		})
	}

	t.Run("unexpected error", func(t *testing.T) {
		rec := send(t, &fakeService{err: errors.New("boom")}, http.MethodGet, "/api/v1/profiles/me", "", callerID)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})
}

func TestUpdateMyProfile(t *testing.T) {
	t.Run("saved", func(t *testing.T) {
		svc := &fakeService{profile: ada}
		body := `{"fullName":" Ada Lovelace ","headline":"Analyst","bio":null,"country":"gb"}`
		rec := send(t, svc, http.MethodPut, "/api/v1/profiles/me", body, callerID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
		}
		if svc.userID != callerID {
			t.Errorf("service updated %q, want the caller %q", svc.userID, callerID)
		}
		want := usecase.UpdateInput{FullName: " Ada Lovelace ", Headline: ptr("Analyst"), Country: ptr("gb")}
		if !reflect.DeepEqual(svc.update, want) {
			t.Errorf("service got %+v, want the body unchanged %+v", svc.update, want)
		}
		checkProfileBody(t, rec, callerID)
	})

	tests := []struct {
		err  error
		code string
	}{
		{domain.ErrInvalidFullName, CodeInvalidFullName},
		{domain.ErrInvalidHeadline, CodeInvalidHeadline},
		{domain.ErrInvalidBio, CodeInvalidBio},
		{domain.ErrInvalidInstitution, CodeInvalidInstitution},
		{domain.ErrInvalidDepartment, CodeInvalidDepartment},
		{domain.ErrInvalidJobTitle, CodeInvalidJobTitle},
		{domain.ErrInvalidCountry, CodeInvalidCountry},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			rec := send(t, &fakeService{err: tt.err}, http.MethodPut, "/api/v1/profiles/me", `{"fullName":"Ada"}`, callerID)
			got := decode(t, rec)
			if rec.Code != http.StatusBadRequest || got["code"] != tt.code || got["message"] == "" {
				t.Errorf("got %d %v, want 400 %s with a message", rec.Code, got, tt.code)
			}
		})
	}

	t.Run("account no longer exists", func(t *testing.T) {
		rec := send(t, &fakeService{err: domain.ErrNotFound}, http.MethodPut, "/api/v1/profiles/me", `{"fullName":"Ada"}`, callerID)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("body is not JSON", func(t *testing.T) {
		svc := &fakeService{}
		rec := send(t, svc, http.MethodPut, "/api/v1/profiles/me", `{`, callerID)
		if rec.Code != http.StatusBadRequest || svc.userID != "" {
			t.Errorf("got %d, service called for %q; want 400 and no call", rec.Code, svc.userID)
		}
	})
}

func TestGetProfile(t *testing.T) {
	t.Run("another user's profile", func(t *testing.T) {
		svc := &fakeService{profile: domain.Profile{UserID: otherID, FullName: "Ada Lovelace", Headline: ptr("Analyst"), Country: ptr("GB")}}
		rec := send(t, svc, http.MethodGet, "/api/v1/profiles/"+otherID, "", callerID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
		}
		if svc.userID != otherID || svc.viewerID != callerID {
			t.Errorf("service asked for %q as %q, want %q as the caller %q", svc.userID, svc.viewerID, otherID, callerID)
		}
		checkProfileBody(t, rec, otherID)
	})

	// The usecase gives ErrNotFound for unknown, suspended and unverified accounts alike.
	t.Run("not found", func(t *testing.T) {
		rec := send(t, &fakeService{err: domain.ErrNotFound}, http.MethodGet, "/api/v1/profiles/"+otherID, "", callerID)
		got := decode(t, rec)
		if rec.Code != http.StatusNotFound || got["code"] != httpx.CodeNotFound || len(got) != 2 {
			t.Errorf("got %d %v, want 404 with only code not_found and a message", rec.Code, got)
		}
	})

	// The ID is passed on as sent; the store treats a non-UUID as an unknown user.
	t.Run("malformed ID", func(t *testing.T) {
		svc := &fakeService{err: domain.ErrNotFound}
		rec := send(t, svc, http.MethodGet, "/api/v1/profiles/not-a-uuid", "", callerID)
		if rec.Code != http.StatusNotFound || svc.userID != "not-a-uuid" {
			t.Errorf("got %d, service asked for %q; want 404 after asking for not-a-uuid", rec.Code, svc.userID)
		}
	})

	t.Run("me is not taken as an ID", func(t *testing.T) {
		svc := &fakeService{profile: ada}
		send(t, svc, http.MethodGet, "/api/v1/profiles/me", "", callerID)
		if svc.userID != callerID {
			t.Errorf("service asked for %q, want GetOwn for the caller", svc.userID)
		}
	})
}
