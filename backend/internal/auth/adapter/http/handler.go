// Package http serves the auth operations in docs/openapi.yaml: registration, email
// verification and new verification links (FR-1), login and the current user (FR-2.1).
// It turns the usecase's errors into the error codes the web client shows next to each
// field, and puts the login tokens in cookies.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpserver"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// Error codes of the auth operations (docs/openapi.yaml).
const (
	CodeInvalidFullName  = "invalid_full_name"
	CodeInvalidEmail     = "invalid_email"
	CodePasswordTooShort = "password_too_short"
	CodePasswordTooLong  = "password_too_long"
	CodeEmailTaken       = "email_taken"
	CodeTokenInvalid     = "token_invalid"
	CodeTokenUsed        = "token_used"
	CodeTokenExpired     = "token_expired"

	CodeInvalidCredentials = "invalid_credentials" //nolint:gosec // an error code, not a credential
	CodeEmailNotVerified   = "email_not_verified"
)

// refreshCookiePath limits the refresh cookie to the auth routes, which will refresh and
// end sessions; no other request carries it.
const refreshCookiePath = httpserver.BaseURL + "/auth"

// service is the part of usecase.Service this handler needs.
type service interface {
	Register(ctx context.Context, in usecase.RegisterInput) (usecase.RegisterResult, error)
	VerifyEmail(ctx context.Context, token string) error
	ResendVerification(ctx context.Context, email string) error
	Login(ctx context.Context, in usecase.LoginInput) (usecase.LoginResult, error)
	CurrentUser(ctx context.Context, id string) (domain.User, error)
}

// CookieConfig sets the attributes of the session cookies.
type CookieConfig struct {
	// Secure sends the cookies over HTTPS only. It is off only for local work over http.
	Secure bool
}

// Handler implements the auth operations of gen.StrictServerInterface.
type Handler struct {
	svc     service
	cookies CookieConfig
}

// NewHandler returns a Handler that calls svc.
func NewHandler(svc service, cookies CookieConfig) *Handler {
	return &Handler{svc: svc, cookies: cookies}
}

// inputErrors maps the usecase's validation errors to a code and a message for the user.
var inputErrors = []struct {
	err           error
	code, message string
}{
	{domain.ErrInvalidFullName, CodeInvalidFullName, "Enter your full name (at most 200 characters)."},
	{domain.ErrInvalidEmail, CodeInvalidEmail, "Enter a valid email address, such as name@example.org."},
	{domain.ErrPasswordTooShort, CodePasswordTooShort, "The password must be at least 8 characters long."},
	{domain.ErrPasswordTooLong, CodePasswordTooLong, "The password must be at most 128 characters long."},
}

func inputError(err error) (gen.Error, bool) {
	for _, e := range inputErrors {
		if errors.Is(err, e.err) {
			return gen.Error{Code: e.code, Message: e.message}, true
		}
	}
	return gen.Error{}, false
}

// RegisterUser creates an account (FR-1.1, FR-1.2). Unexpected errors are returned to
// the strict server, which logs them and answers 500 in the error shape.
func (h *Handler) RegisterUser(ctx context.Context, req gen.RegisterUserRequestObject) (gen.RegisterUserResponseObject, error) {
	res, err := h.svc.Register(ctx, usecase.RegisterInput{
		FullName: req.Body.FullName,
		Email:    req.Body.Email,
		Password: req.Body.Password,
	})
	if e, ok := inputError(err); ok {
		return gen.RegisterUser400JSONResponse(e), nil
	}
	if errors.Is(err, domain.ErrEmailTaken) {
		return gen.RegisterUser409JSONResponse{
			Code:    CodeEmailTaken,
			Message: "This email address is already registered. Log in, or ask for a new verification link.",
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.RegisterUser201JSONResponse{Email: res.Email}, nil
}

// VerifyEmail verifies the address the token was sent to (FR-1.3).
func (h *Handler) VerifyEmail(ctx context.Context, req gen.VerifyEmailRequestObject) (gen.VerifyEmailResponseObject, error) {
	err := h.svc.VerifyEmail(ctx, req.Body.Token)
	switch {
	case err == nil:
		return gen.VerifyEmail204Response{}, nil
	case errors.Is(err, domain.ErrTokenInvalid):
		return gen.VerifyEmail400JSONResponse{Code: CodeTokenInvalid, Message: "This verification link is not valid. Ask for a new one."}, nil
	case errors.Is(err, domain.ErrTokenUsed):
		return gen.VerifyEmail400JSONResponse{Code: CodeTokenUsed, Message: "This verification link has already been used."}, nil
	case errors.Is(err, domain.ErrTokenExpired):
		return gen.VerifyEmail400JSONResponse{Code: CodeTokenExpired, Message: "This verification link has expired. Ask for a new one."}, nil
	}
	return nil, err
}

// ResendVerification sends a new link. The reply is the same for every well-formed
// address (FR-1.5).
func (h *Handler) ResendVerification(ctx context.Context, req gen.ResendVerificationRequestObject) (gen.ResendVerificationResponseObject, error) {
	err := h.svc.ResendVerification(ctx, req.Body.Email)
	if e, ok := inputError(err); ok {
		return gen.ResendVerification400JSONResponse(e), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.ResendVerification202Response{}, nil
}

// LoginUser checks the email address and password and sets the session cookies (FR-2.1).
// A wrong password and an unknown address get the same answer; an unverified address is
// refused only after the right password (FR-1.4).
func (h *Handler) LoginUser(ctx context.Context, req gen.LoginUserRequestObject) (gen.LoginUserResponseObject, error) {
	res, err := h.svc.Login(ctx, usecase.LoginInput{Email: req.Body.Email, Password: req.Body.Password})
	switch {
	case errors.Is(err, domain.ErrInvalidCredentials):
		return gen.LoginUser401JSONResponse{Code: CodeInvalidCredentials, Message: "The email address or password is wrong."}, nil
	case errors.Is(err, domain.ErrEmailNotVerified):
		return gen.LoginUser403JSONResponse{
			Code:    CodeEmailNotVerified,
			Message: "Verify your email address before you log in. Ask for a new link if you cannot find it.",
		}, nil
	case err != nil:
		return nil, err
	}
	body, err := currentUser(res.User)
	if err != nil {
		return nil, err
	}
	return loginOK{
		body: body,
		cookies: []*http.Cookie{
			h.cookie(httpx.AccessCookie, res.AccessToken, "/", res.AccessExpiresAt.Sub(res.IssuedAt)),
			h.cookie(httpx.RefreshCookie, res.RefreshToken, refreshCookiePath, res.RefreshExpiresAt.Sub(res.IssuedAt)),
		},
	}, nil
}

// GetCurrentUser returns the user whose ID the auth middleware put in the context. A
// valid token for an account that no longer exists is refused like a missing one.
func (h *Handler) GetCurrentUser(ctx context.Context, _ gen.GetCurrentUserRequestObject) (gen.GetCurrentUserResponseObject, error) {
	unauthenticated := gen.GetCurrentUser401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse{
		Code: httpx.CodeUnauthenticated, Message: "Log in to continue.",
	}}
	id, ok := httpx.UserID(ctx)
	if !ok {
		return unauthenticated, nil
	}
	u, err := h.svc.CurrentUser(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return unauthenticated, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := currentUser(u)
	if err != nil {
		return nil, err
	}
	return gen.GetCurrentUser200JSONResponse(body), nil
}

func currentUser(u domain.User) (gen.CurrentUser, error) {
	var id openapi_types.UUID
	if err := id.UnmarshalText([]byte(u.ID)); err != nil {
		return gen.CurrentUser{}, fmt.Errorf("user id: %w", err)
	}
	return gen.CurrentUser{Id: id, Email: u.Email, FullName: u.FullName}, nil
}

// cookie returns an httpOnly, SameSite=Lax session cookie that lasts ttl.
func (h *Handler) cookie(name, value, path string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{ //nolint:gosec // Secure is off only for local http (docs/decisions.md)
		Name:     name,
		Value:    value,
		Path:     path,
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   h.cookies.Secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// loginOK is the 200 answer to a login. The generated response type can set only one
// Set-Cookie header, and a login sets two.
type loginOK struct {
	body    gen.CurrentUser
	cookies []*http.Cookie
}

func (r loginOK) VisitLoginUserResponse(w http.ResponseWriter) error {
	for _, c := range r.cookies {
		http.SetCookie(w, c)
	}
	w.Header().Set("Cache-Control", "no-store") // a response that sets session cookies must not be cached
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(r.body)
}
