package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/logging"
)

const panicText = "boom: secret internal detail"

// fakeAPI answers every operation in the contract with a fixed success, so these tests
// exercise the router and middleware without any module.
type fakeAPI struct{}

func (fakeAPI) GetHealth(context.Context, gen.GetHealthRequestObject) (gen.GetHealthResponseObject, error) {
	return gen.GetHealth200JSONResponse{Status: gen.HealthStatusOk, Database: gen.HealthDatabaseOk, Redis: gen.HealthRedisOk}, nil
}

func (fakeAPI) RegisterUser(context.Context, gen.RegisterUserRequestObject) (gen.RegisterUserResponseObject, error) {
	return gen.RegisterUser201JSONResponse{Email: "ada@example.org"}, nil
}

func (fakeAPI) VerifyEmail(context.Context, gen.VerifyEmailRequestObject) (gen.VerifyEmailResponseObject, error) {
	return gen.VerifyEmail204Response{}, nil
}

func (fakeAPI) ResendVerification(context.Context, gen.ResendVerificationRequestObject) (gen.ResendVerificationResponseObject, error) {
	return gen.ResendVerification202Response{}, nil
}

func (fakeAPI) LoginUser(context.Context, gen.LoginUserRequestObject) (gen.LoginUserResponseObject, error) {
	return gen.LoginUser200JSONResponse{Body: gen.CurrentUser{Email: "ada@example.org"}}, nil
}

func (fakeAPI) RefreshSession(context.Context, gen.RefreshSessionRequestObject) (gen.RefreshSessionResponseObject, error) {
	return gen.RefreshSession204Response{}, nil
}

func (fakeAPI) LogoutUser(context.Context, gen.LogoutUserRequestObject) (gen.LogoutUserResponseObject, error) {
	return gen.LogoutUser204Response{}, nil
}

func (fakeAPI) RequestPasswordReset(context.Context, gen.RequestPasswordResetRequestObject) (gen.RequestPasswordResetResponseObject, error) {
	return gen.RequestPasswordReset202Response{}, nil
}

func (fakeAPI) ConfirmPasswordReset(context.Context, gen.ConfirmPasswordResetRequestObject) (gen.ConfirmPasswordResetResponseObject, error) {
	return gen.ConfirmPasswordReset204Response{}, nil
}

func (fakeAPI) ChangePassword(context.Context, gen.ChangePasswordRequestObject) (gen.ChangePasswordResponseObject, error) {
	return gen.ChangePassword204Response{}, nil
}

func (fakeAPI) DeleteAccount(context.Context, gen.DeleteAccountRequestObject) (gen.DeleteAccountResponseObject, error) {
	return gen.DeleteAccount204Response{}, nil
}

func (fakeAPI) ListUsers(context.Context, gen.ListUsersRequestObject) (gen.ListUsersResponseObject, error) {
	return gen.ListUsers200JSONResponse{Items: []gen.AdminUser{}}, nil
}

func (fakeAPI) GetMyProfile(context.Context, gen.GetMyProfileRequestObject) (gen.GetMyProfileResponseObject, error) {
	return gen.GetMyProfile200JSONResponse{}, nil
}

func (fakeAPI) UpdateMyProfile(context.Context, gen.UpdateMyProfileRequestObject) (gen.UpdateMyProfileResponseObject, error) {
	return gen.UpdateMyProfile200JSONResponse{}, nil
}

func (fakeAPI) GetProfile(context.Context, gen.GetProfileRequestObject) (gen.GetProfileResponseObject, error) {
	return gen.GetProfile200JSONResponse{}, nil
}

func (fakeAPI) AddMyInterest(context.Context, gen.AddMyInterestRequestObject) (gen.AddMyInterestResponseObject, error) {
	return gen.AddMyInterest201JSONResponse{}, nil
}

func (fakeAPI) RemoveMyInterest(context.Context, gen.RemoveMyInterestRequestObject) (gen.RemoveMyInterestResponseObject, error) {
	return gen.RemoveMyInterest204Response{}, nil
}

func (fakeAPI) AddMySkill(context.Context, gen.AddMySkillRequestObject) (gen.AddMySkillResponseObject, error) {
	return gen.AddMySkill201JSONResponse{}, nil
}

func (fakeAPI) RemoveMySkill(context.Context, gen.RemoveMySkillRequestObject) (gen.RemoveMySkillResponseObject, error) {
	return gen.RemoveMySkill204Response{}, nil
}

func (fakeAPI) AddMyEducation(context.Context, gen.AddMyEducationRequestObject) (gen.AddMyEducationResponseObject, error) {
	return gen.AddMyEducation201JSONResponse{}, nil
}

func (fakeAPI) UpdateMyEducation(context.Context, gen.UpdateMyEducationRequestObject) (gen.UpdateMyEducationResponseObject, error) {
	return gen.UpdateMyEducation200JSONResponse{}, nil
}

func (fakeAPI) RemoveMyEducation(context.Context, gen.RemoveMyEducationRequestObject) (gen.RemoveMyEducationResponseObject, error) {
	return gen.RemoveMyEducation204Response{}, nil
}

func (fakeAPI) AddMyExperience(context.Context, gen.AddMyExperienceRequestObject) (gen.AddMyExperienceResponseObject, error) {
	return gen.AddMyExperience201JSONResponse{}, nil
}

func (fakeAPI) UpdateMyExperience(context.Context, gen.UpdateMyExperienceRequestObject) (gen.UpdateMyExperienceResponseObject, error) {
	return gen.UpdateMyExperience200JSONResponse{}, nil
}

func (fakeAPI) RemoveMyExperience(context.Context, gen.RemoveMyExperienceRequestObject) (gen.RemoveMyExperienceResponseObject, error) {
	return gen.RemoveMyExperience204Response{}, nil
}

func (fakeAPI) ListMyPublications(context.Context, gen.ListMyPublicationsRequestObject) (gen.ListMyPublicationsResponseObject, error) {
	return gen.ListMyPublications200JSONResponse{}, nil
}

func (fakeAPI) ListPublications(context.Context, gen.ListPublicationsRequestObject) (gen.ListPublicationsResponseObject, error) {
	return gen.ListPublications200JSONResponse{}, nil
}

func (fakeAPI) AddMyPublication(context.Context, gen.AddMyPublicationRequestObject) (gen.AddMyPublicationResponseObject, error) {
	return gen.AddMyPublication201JSONResponse{}, nil
}

func (fakeAPI) UpdateMyPublication(context.Context, gen.UpdateMyPublicationRequestObject) (gen.UpdateMyPublicationResponseObject, error) {
	return gen.UpdateMyPublication200JSONResponse{}, nil
}

func (fakeAPI) RemoveMyPublication(context.Context, gen.RemoveMyPublicationRequestObject) (gen.RemoveMyPublicationResponseObject, error) {
	return gen.RemoveMyPublication204Response{}, nil
}

func (fakeAPI) LookupPublicationByDOI(context.Context, gen.LookupPublicationByDOIRequestObject) (gen.LookupPublicationByDOIResponseObject, error) {
	return gen.LookupPublicationByDOI200JSONResponse{}, nil
}

func (fakeAPI) AddMyPublicationFromDOI(context.Context, gen.AddMyPublicationFromDOIRequestObject) (gen.AddMyPublicationFromDOIResponseObject, error) {
	return gen.AddMyPublicationFromDOI201JSONResponse{}, nil
}

func (fakeAPI) SetMyOrcidId(context.Context, gen.SetMyOrcidIdRequestObject) (gen.SetMyOrcidIdResponseObject, error) {
	return gen.SetMyOrcidId200JSONResponse{}, nil
}

func (fakeAPI) ListMyOrcidWorks(context.Context, gen.ListMyOrcidWorksRequestObject) (gen.ListMyOrcidWorksResponseObject, error) {
	return gen.ListMyOrcidWorks200JSONResponse{}, nil
}

func (fakeAPI) ImportMyOrcidWorks(context.Context, gen.ImportMyOrcidWorksRequestObject) (gen.ImportMyOrcidWorksResponseObject, error) {
	return gen.ImportMyOrcidWorks200JSONResponse{}, nil
}

func (fakeAPI) AddMyLanguage(context.Context, gen.AddMyLanguageRequestObject) (gen.AddMyLanguageResponseObject, error) {
	return gen.AddMyLanguage201JSONResponse{}, nil
}

func (fakeAPI) UpdateMyLanguage(context.Context, gen.UpdateMyLanguageRequestObject) (gen.UpdateMyLanguageResponseObject, error) {
	return gen.UpdateMyLanguage200JSONResponse{}, nil
}

func (fakeAPI) RemoveMyLanguage(context.Context, gen.RemoveMyLanguageRequestObject) (gen.RemoveMyLanguageResponseObject, error) {
	return gen.RemoveMyLanguage204Response{}, nil
}

func (fakeAPI) GetMyContact(context.Context, gen.GetMyContactRequestObject) (gen.GetMyContactResponseObject, error) {
	return gen.GetMyContact200JSONResponse{}, nil
}

func (fakeAPI) UpdateMyContact(context.Context, gen.UpdateMyContactRequestObject) (gen.UpdateMyContactResponseObject, error) {
	return gen.UpdateMyContact200JSONResponse{}, nil
}

func (fakeAPI) AddMyLink(context.Context, gen.AddMyLinkRequestObject) (gen.AddMyLinkResponseObject, error) {
	return gen.AddMyLink201JSONResponse{}, nil
}

func (fakeAPI) RemoveMyLink(context.Context, gen.RemoveMyLinkRequestObject) (gen.RemoveMyLinkResponseObject, error) {
	return gen.RemoveMyLink204Response{}, nil
}

func (fakeAPI) UploadMyPhoto(context.Context, gen.UploadMyPhotoRequestObject) (gen.UploadMyPhotoResponseObject, error) {
	return gen.UploadMyPhoto200JSONResponse{ProfileImageStoredJSONResponse: gen.ProfileImageStoredJSONResponse{Url: "/api/v1/media/x"}}, nil
}

func (fakeAPI) RemoveMyPhoto(context.Context, gen.RemoveMyPhotoRequestObject) (gen.RemoveMyPhotoResponseObject, error) {
	return gen.RemoveMyPhoto204Response{}, nil
}

func (fakeAPI) UploadMyCover(context.Context, gen.UploadMyCoverRequestObject) (gen.UploadMyCoverResponseObject, error) {
	return gen.UploadMyCover200JSONResponse{ProfileImageStoredJSONResponse: gen.ProfileImageStoredJSONResponse{Url: "/api/v1/media/x"}}, nil
}

func (fakeAPI) RemoveMyCover(context.Context, gen.RemoveMyCoverRequestObject) (gen.RemoveMyCoverResponseObject, error) {
	return gen.RemoveMyCover204Response{}, nil
}

func (fakeAPI) GetMedia(context.Context, gen.GetMediaRequestObject) (gen.GetMediaResponseObject, error) {
	return gen.GetMedia302Response{Headers: gen.GetMedia302ResponseHeaders{Location: "http://localhost/x", CacheControl: "private, max-age=300"}}, nil
}

func (fakeAPI) SuggestInterestTags(context.Context, gen.SuggestInterestTagsRequestObject) (gen.SuggestInterestTagsResponseObject, error) {
	return gen.SuggestInterestTags200JSONResponse{Items: []gen.Tag{}}, nil
}

func (fakeAPI) SuggestSkillTags(context.Context, gen.SuggestSkillTagsRequestObject) (gen.SuggestSkillTagsResponseObject, error) {
	return gen.SuggestSkillTags200JSONResponse{Items: []gen.Tag{}}, nil
}

// GetCurrentUser echoes the user ID that the auth middleware put in the context.
func (fakeAPI) GetCurrentUser(ctx context.Context, _ gen.GetCurrentUserRequestObject) (gen.GetCurrentUserResponseObject, error) {
	id, _ := httpx.UserID(ctx)
	return gen.GetCurrentUser200JSONResponse{FullName: id}, nil
}

var _ gen.StrictServerInterface = fakeAPI{}

var testSigner = accesstoken.New([]byte("test-secret-that-is-at-least-32-bytes-long"), 15*time.Minute)

// newTestRouter returns the full router with a fake API, a /test/panic route, and a
// buffer that receives the JSON logs.
func newTestRouter(t *testing.T) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(logging.NewHandler(slog.NewJSONHandler(&logs, nil)))
	router := newRouter(logger, fakeAPI{}, httpx.RequireAuth(testSigner, time.Now), func(mux *http.ServeMux) {
		mux.HandleFunc("GET /test/panic", func(http.ResponseWriter, *http.Request) { panic(panicText) })
	})
	return router, &logs
}

func serve(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, path, nil))
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body, err)
	}
	if len(body) != 2 {
		t.Errorf("body has keys %v, want exactly code and message", body)
	}
	code, _ = body["code"].(string)
	message, _ = body["message"].(string)
	if code == "" || message == "" {
		t.Errorf("body %v lacks a code or message", body)
	}
	return code, message
}

func TestErrorShape(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   string
		wantAllow  string
	}{
		{"unknown route", http.MethodGet, "/nope", http.StatusNotFound, "not_found", ""},
		{"unknown route under api", http.MethodGet, "/api/v1/nope", http.StatusNotFound, "not_found", ""},
		{"root", http.MethodGet, "/", http.StatusNotFound, "not_found", ""},
		{"wrong method", http.MethodPost, "/api/v1/health", http.StatusMethodNotAllowed, "method_not_allowed", "GET, HEAD"},
		{"panic", http.MethodGet, "/test/panic", http.StatusInternalServerError, "internal_error", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, _ := newTestRouter(t)
			rec := serve(t, router, tt.method, tt.path)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			code, _ := decodeError(t, rec)
			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if got := rec.Header().Get("Allow"); got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}
			if strings.Contains(rec.Body.String(), "boom") {
				t.Errorf("body leaks the panic value: %s", rec.Body)
			}
		})
	}
}

func TestRequestIDIsSetAndUnique(t *testing.T) {
	router, _ := newTestRouter(t)
	seen := map[string]bool{}
	for range 3 {
		id := serve(t, router, http.MethodGet, "/api/v1/health").Header().Get(httpx.RequestIDHeader)
		if len(id) != 32 {
			t.Errorf("request ID %q, want 32 hex characters", id)
		}
		if seen[id] {
			t.Errorf("request ID %q repeated", id)
		}
		seen[id] = true
	}
}

func TestClientRequestIDIsIgnored(t *testing.T) {
	router, _ := newTestRouter(t)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/health", nil)
	req.Header.Set(httpx.RequestIDHeader, "injected\nvalue")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if got := rec.Header().Get(httpx.RequestIDHeader); got == "injected\nvalue" || len(got) != 32 {
		t.Errorf("request ID = %q, want a new server-made ID", got)
	}
}

func TestOneAccessLogLinePerRequest(t *testing.T) {
	tests := []struct {
		method, path string
		wantStatus   float64
	}{
		{http.MethodGet, "/api/v1/health?token=abc", 200},
		{http.MethodGet, "/nope", 404},
		{http.MethodGet, "/test/panic", 500},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			router, logs := newTestRouter(t)
			rec := serve(t, router, tt.method, tt.path)

			var access []map[string]any
			for line := range strings.SplitSeq(strings.TrimSpace(logs.String()), "\n") {
				var entry map[string]any
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					t.Fatalf("log line %q is not JSON: %v", line, err)
				}
				if entry["msg"] == "request" {
					access = append(access, entry)
				}
			}
			if len(access) != 1 {
				t.Fatalf("got %d access log lines, want 1:\n%s", len(access), logs)
			}
			e := access[0]
			wantPath, _, _ := strings.Cut(tt.path, "?")
			if e["method"] != tt.method || e["path"] != wantPath || e["status"] != tt.wantStatus {
				t.Errorf("access line = %v, want method %s path %s status %v", e, tt.method, wantPath, tt.wantStatus)
			}
			if _, ok := e["duration_ms"].(float64); !ok {
				t.Errorf("access line lacks duration_ms: %v", e)
			}
			if e["request_id"] != rec.Header().Get(httpx.RequestIDHeader) {
				t.Errorf("request_id = %v, want %q", e["request_id"], rec.Header().Get(httpx.RequestIDHeader))
			}
			if strings.Contains(logs.String(), "token=abc") {
				t.Errorf("logs contain the query string:\n%s", logs)
			}
		})
	}
}

func TestPanicIsLoggedWithRequestID(t *testing.T) {
	router, logs := newTestRouter(t)
	rec := serve(t, router, http.MethodGet, "/test/panic")
	out := logs.String()
	if !strings.Contains(out, "panic recovered") || !strings.Contains(out, panicText) {
		t.Errorf("panic not logged:\n%s", out)
	}
	if !strings.Contains(out, `"request_id":"`+rec.Header().Get(httpx.RequestIDHeader)+`"`) {
		t.Errorf("panic log lacks the request ID:\n%s", out)
	}
}

// The contract secures every operation without `security: []`; the generated wrapper
// tells the auth middleware so, and public operations need no cookie.
func TestContractDecidesWhichOperationsNeedASession(t *testing.T) {
	router, _ := newTestRouter(t)

	rec := serve(t, router, http.MethodGet, "/api/v1/auth/me")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /auth/me without a cookie: status = %d, want 401", rec.Code)
	}
	if code, _ := decodeError(t, rec); code != httpx.CodeUnauthenticated {
		t.Errorf("code = %q, want %q", code, httpx.CodeUnauthenticated)
	}

	token, _, err := testSigner.Sign("user-1", "researcher", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Cookie", httpx.AccessCookie+"="+token)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"user-1"`) {
		t.Errorf("GET /auth/me with a valid cookie: %d %s, want 200 with the user ID", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/password",
		strings.NewReader(`{"currentPassword":"password1","newPassword":"password2"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /auth/password without a cookie: status = %d, want 401", rec.Code)
	}

	if rec := serve(t, router, http.MethodGet, "/api/v1/health"); rec.Code != http.StatusOK {
		t.Errorf("GET /health without a cookie: status = %d, want 200", rec.Code)
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"email":"ada@example.org","password":"pw"}`)))
	if rec.Code != http.StatusOK {
		t.Errorf("POST /auth/login without a cookie: status = %d, want 200", rec.Code)
	}
	for _, path := range []string{"/api/v1/auth/refresh", "/api/v1/auth/logout"} {
		if rec := serve(t, router, http.MethodPost, path); rec.Code != http.StatusNoContent {
			t.Errorf("POST %s without an access cookie: status = %d, want 204", path, rec.Code)
		}
	}
}

func TestRoutes(t *testing.T) {
	got := Routes()
	if len(got) != 50 {
		t.Errorf("Routes() returned %d routes, want the 50 operations in the contract: %v", len(got), got)
	}
	seen := map[string]bool{}
	for _, p := range got {
		if seen[p] {
			t.Errorf("route %q registered twice", p)
		}
		seen[p] = true
	}
	for _, want := range []string{"GET /api/v1/health", "GET /api/v1/auth/me", "DELETE /api/v1/account", "GET /api/v1/media/{fileId}"} {
		if !seen[want] {
			t.Errorf("Routes() lacks %q", want)
		}
	}
}
