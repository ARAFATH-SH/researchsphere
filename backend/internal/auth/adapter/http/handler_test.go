package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/usecase"
)

// fakeService returns err from every operation and records what it was given.
type fakeService struct {
	err      error
	register usecase.RegisterInput
	token    string
	email    string
}

func (f *fakeService) Register(_ context.Context, in usecase.RegisterInput) (usecase.RegisterResult, error) {
	f.register = in
	return usecase.RegisterResult{Email: in.Email}, f.err
}

func (f *fakeService) VerifyEmail(_ context.Context, token string) error {
	f.token = token
	return f.err
}

func (f *fakeService) ResendVerification(_ context.Context, email string) error {
	f.email = email
	return f.err
}

// server serves only the auth operations: Handler's methods are promoted from a
// shallower depth than the embedded interface's, which stays nil.
type server struct {
	*Handler
	others
}

type others struct{ gen.StrictServerInterface }

func post(t *testing.T, svc *fakeService, path, body string) *httptest.ResponseRecorder {
	t.Helper()
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
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	if rec.Body.Len() == 0 {
		return nil
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body, err)
	}
	return got
}

func TestRegisterUser(t *testing.T) {
	const body = `{"fullName":"Ada","email":"ada@example.org","password":"password1"}`
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"created", nil, http.StatusCreated, ""},
		{"invalid name", domain.ErrInvalidFullName, http.StatusBadRequest, CodeInvalidFullName},
		{"invalid email", domain.ErrInvalidEmail, http.StatusBadRequest, CodeInvalidEmail},
		{"short password", domain.ErrPasswordTooShort, http.StatusBadRequest, CodePasswordTooShort},
		{"long password", domain.ErrPasswordTooLong, http.StatusBadRequest, CodePasswordTooLong},
		{"email taken", domain.ErrEmailTaken, http.StatusConflict, CodeEmailTaken},
		{"unexpected error", errors.New("database down"), http.StatusInternalServerError, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{err: tt.err}
			rec := post(t, svc, "/auth/register", body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			got := decode(t, rec)
			switch {
			case tt.wantStatus == http.StatusCreated && got["email"] != "ada@example.org":
				t.Errorf("body = %v, want the email", got)
			case tt.wantCode != "" && (got["code"] != tt.wantCode || got["message"] == ""):
				t.Errorf("body = %v, want code %q and a message", got, tt.wantCode)
			}
			if want := (usecase.RegisterInput{FullName: "Ada", Email: "ada@example.org", Password: "password1"}); svc.register != want {
				t.Errorf("usecase got %+v, want %+v", svc.register, want)
			}
		})
	}
}

func TestVerifyEmail(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"verified", nil, http.StatusNoContent, ""},
		{"unknown token", domain.ErrTokenInvalid, http.StatusBadRequest, CodeTokenInvalid},
		{"used token", domain.ErrTokenUsed, http.StatusBadRequest, CodeTokenUsed},
		{"expired token", domain.ErrTokenExpired, http.StatusBadRequest, CodeTokenExpired},
		{"unexpected error", errors.New("database down"), http.StatusInternalServerError, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{err: tt.err}
			rec := post(t, svc, "/auth/verify-email", `{"token":"abc"}`)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			got := decode(t, rec)
			if tt.wantCode != "" && (got["code"] != tt.wantCode || got["message"] == "") {
				t.Errorf("body = %v, want code %q and a message", got, tt.wantCode)
			}
			if tt.wantStatus == http.StatusNoContent && rec.Body.Len() != 0 {
				t.Errorf("204 with body %q", rec.Body)
			}
			if svc.token != "abc" {
				t.Errorf("usecase got token %q", svc.token)
			}
		})
	}
}

func TestResendVerification(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"accepted", nil, http.StatusAccepted, ""},
		{"malformed email", domain.ErrInvalidEmail, http.StatusBadRequest, CodeInvalidEmail},
		{"unexpected error", errors.New("database down"), http.StatusInternalServerError, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{err: tt.err}
			rec := post(t, svc, "/auth/resend-verification", `{"email":"ada@example.org"}`)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if got := decode(t, rec); tt.wantCode != "" && got["code"] != tt.wantCode {
				t.Errorf("body = %v, want code %q", got, tt.wantCode)
			}
			if svc.email != "ada@example.org" {
				t.Errorf("usecase got email %q", svc.email)
			}
		})
	}
}

func TestMalformedBody(t *testing.T) {
	for _, path := range []string{"/auth/register", "/auth/verify-email", "/auth/resend-verification"} {
		t.Run(path, func(t *testing.T) {
			if rec := post(t, &fakeService{}, path, `{not json`); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}
