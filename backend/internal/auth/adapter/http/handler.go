// Package http serves the auth operations in docs/openapi.yaml: registration, email
// verification and new verification links (FR-1), login and the current user (FR-2.1),
// refreshing and ending sessions (FR-2.2), password reset (FR-2.3 to FR-2.5), changing
// the password (FR-2.6), the login lockout (FR-2.7), and deleting the account (FR-2.8,
// FR-2.9).
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
	CodeAccountSuspended   = "account_suspended"
	CodeSessionInvalid     = "session_invalid"
	CodeAccountLocked      = "account_locked"

	CodeWrongCurrentPassword = "wrong_current_password" //nolint:gosec // an error code, not a credential
	CodeSamePassword         = "same_password"          //nolint:gosec // an error code, not a credential

	CodeWrongPassword     = "wrong_password" //nolint:gosec // an error code, not a credential
	CodeOwnsSharedProject = "owns_shared_project"
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
	Refresh(ctx context.Context, token string) (usecase.RefreshResult, error)
	Logout(ctx context.Context, token string) error
	RequestPasswordReset(ctx context.Context, email string) error
	ResetPassword(ctx context.Context, token, newPassword string) error
	ChangePassword(ctx context.Context, in usecase.ChangePasswordInput) error
	DeleteAccount(ctx context.Context, in usecase.DeleteAccountInput) error
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

// RequestPasswordReset sends a reset link. The reply is the same for every well-formed
// address (FR-2.4).
func (h *Handler) RequestPasswordReset(ctx context.Context, req gen.RequestPasswordResetRequestObject) (gen.RequestPasswordResetResponseObject, error) {
	err := h.svc.RequestPasswordReset(ctx, req.Body.Email)
	if e, ok := inputError(err); ok {
		return gen.RequestPasswordReset400JSONResponse(e), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.RequestPasswordReset202Response{}, nil
}

// ConfirmPasswordReset sets the new password and ends every session of the account
// (FR-2.3, FR-2.5).
func (h *Handler) ConfirmPasswordReset(ctx context.Context, req gen.ConfirmPasswordResetRequestObject) (gen.ConfirmPasswordResetResponseObject, error) {
	err := h.svc.ResetPassword(ctx, req.Body.Token, req.Body.NewPassword)
	if e, ok := inputError(err); ok {
		return gen.ConfirmPasswordReset400JSONResponse(e), nil
	}
	switch {
	case err == nil:
		return gen.ConfirmPasswordReset204Response{}, nil
	case errors.Is(err, domain.ErrTokenInvalid):
		return gen.ConfirmPasswordReset400JSONResponse{Code: CodeTokenInvalid, Message: "This reset link is not valid. Ask for a new one."}, nil
	case errors.Is(err, domain.ErrTokenUsed):
		return gen.ConfirmPasswordReset400JSONResponse{Code: CodeTokenUsed, Message: "This reset link has already been used. Ask for a new one if you still need it."}, nil
	case errors.Is(err, domain.ErrTokenExpired):
		return gen.ConfirmPasswordReset400JSONResponse{Code: CodeTokenExpired, Message: "This reset link has expired. Ask for a new one."}, nil
	case errors.Is(err, domain.ErrAccountSuspended):
		return gen.ConfirmPasswordReset403JSONResponse(accountSuspended), nil
	}
	return nil, err
}

// LoginUser checks the email address and password and sets the session cookies (FR-2.1).
// A wrong password and an unknown address get the same answer; an unverified address is
// refused only after the right password (FR-1.4), and so is a suspended one (FR-3.4). A
// locked account is refused with 429 (FR-2.7).
func (h *Handler) LoginUser(ctx context.Context, req gen.LoginUserRequestObject) (gen.LoginUserResponseObject, error) {
	res, err := h.svc.Login(ctx, usecase.LoginInput{Email: req.Body.Email, Password: req.Body.Password})
	switch {
	case errors.Is(err, domain.ErrInvalidCredentials):
		return gen.LoginUser401JSONResponse{Code: CodeInvalidCredentials, Message: "The email address or password is wrong."}, nil
	case errors.Is(err, domain.ErrAccountSuspended):
		return gen.LoginUser403JSONResponse(accountSuspended), nil
	case errors.Is(err, domain.ErrEmailNotVerified):
		return gen.LoginUser403JSONResponse{
			Code:    CodeEmailNotVerified,
			Message: "Verify your email address before you log in. Ask for a new link if you cannot find it.",
		}, nil
	case errors.Is(err, domain.ErrAccountLocked):
		var locked *domain.LockedError
		if !errors.As(err, &locked) {
			return nil, err
		}
		return accountLocked(locked.RetryAfter), nil
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

// accountSuspended refuses a login or a password reset of a suspended account (FR-3.4).
var accountSuspended = gen.Error{Code: CodeAccountSuspended, Message: "This account is suspended. Contact support."}

// accountLocked answers a login while the account is locked (FR-2.7).
func accountLocked(left time.Duration) gen.LoginUser429JSONResponse {
	seconds, body := lockedError(left)
	return gen.LoginUser429JSONResponse{Headers: gen.LoginUser429ResponseHeaders{RetryAfter: &seconds}, Body: body}
}

// lockedError returns the Retry-After seconds and the error body for a locked account
// (FR-2.7). Both round the time left up, to whole seconds and whole minutes.
func lockedError(left time.Duration) (int, gen.Error) {
	seconds := int((left + time.Second - 1) / time.Second)
	minutes := (seconds + 59) / 60
	unit := "minutes"
	if minutes == 1 {
		unit = "minute"
	}
	return seconds, gen.Error{
		Code:    CodeAccountLocked,
		Message: fmt.Sprintf("This account is locked after too many failed login attempts. Try again in %d %s.", minutes, unit),
	}
}

// RefreshSession issues new tokens for the session in the refresh cookie and sets both
// cookies again (FR-2.1). Every refusal gets the same answer and clears both cookies, so
// the browser stops sending a refresh token that no longer works.
func (h *Handler) RefreshSession(ctx context.Context, req gen.RefreshSessionRequestObject) (gen.RefreshSessionResponseObject, error) {
	res, err := h.svc.Refresh(ctx, refreshToken(req.Params.RsRefresh))
	if errors.Is(err, domain.ErrSessionInvalid) {
		return cookieResponse{
			status:  http.StatusUnauthorized,
			body:    &gen.Error{Code: CodeSessionInvalid, Message: "Your session has ended. Log in again."},
			cookies: h.clearedCookies(),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return cookieResponse{
		status: http.StatusNoContent,
		cookies: []*http.Cookie{
			h.cookie(httpx.AccessCookie, res.AccessToken, "/", res.AccessExpiresAt.Sub(res.IssuedAt)),
			// The session keeps its login expiry, so little time may be left; a Max-Age of 0
			// would mean no Max-Age at all.
			h.cookie(httpx.RefreshCookie, res.RefreshToken, refreshCookiePath, max(res.RefreshExpiresAt.Sub(res.IssuedAt), time.Second)),
		},
	}, nil
}

// LogoutUser ends the session in the refresh cookie and clears both cookies (FR-2.2).
// It answers 204 when there is no cookie or no such session. A failure to end the
// session is a 500, so the user is never told they logged out when they did not.
func (h *Handler) LogoutUser(ctx context.Context, req gen.LogoutUserRequestObject) (gen.LogoutUserResponseObject, error) {
	if err := h.svc.Logout(ctx, refreshToken(req.Params.RsRefresh)); err != nil {
		return nil, err
	}
	return cookieResponse{status: http.StatusNoContent, cookies: h.clearedCookies()}, nil
}

func refreshToken(c *gen.RefreshCookie) string {
	if c == nil {
		return ""
	}
	return *c
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

// ChangePassword sets a new password for the logged-in user and ends every other session
// of the account (FR-2.6, FR-2.5). The session in the refresh cookie stays logged in, so
// the response sets no cookies. A wrong current password is a 400, not a 401: the web
// client answers a 401 by refreshing the session.
func (h *Handler) ChangePassword(ctx context.Context, req gen.ChangePasswordRequestObject) (gen.ChangePasswordResponseObject, error) {
	unauthenticated := gen.ChangePassword401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse{
		Code: httpx.CodeUnauthenticated, Message: "Log in to continue.",
	}}
	id, ok := httpx.UserID(ctx)
	if !ok {
		return unauthenticated, nil
	}
	err := h.svc.ChangePassword(ctx, usecase.ChangePasswordInput{
		UserID:          id,
		RefreshToken:    refreshToken(req.Params.RsRefresh),
		CurrentPassword: req.Body.CurrentPassword,
		NewPassword:     req.Body.NewPassword,
	})
	if e, ok := inputError(err); ok {
		return gen.ChangePassword400JSONResponse(e), nil
	}
	switch {
	case err == nil:
		return gen.ChangePassword204Response{}, nil
	case errors.Is(err, domain.ErrWrongCurrentPassword):
		return gen.ChangePassword400JSONResponse{Code: CodeWrongCurrentPassword, Message: "The current password is wrong."}, nil
	case errors.Is(err, domain.ErrSamePassword):
		return gen.ChangePassword400JSONResponse{Code: CodeSamePassword, Message: "Choose a password different from the current one."}, nil
	case errors.Is(err, domain.ErrNotFound):
		return unauthenticated, nil
	case errors.Is(err, domain.ErrAccountLocked):
		var locked *domain.LockedError
		if !errors.As(err, &locked) {
			return nil, err
		}
		seconds, body := lockedError(locked.RetryAfter)
		return gen.ChangePassword429JSONResponse{Headers: gen.ChangePassword429ResponseHeaders{RetryAfter: &seconds}, Body: body}, nil
	}
	return nil, err
}

// DeleteAccount deletes the logged-in user's account and clears both cookies (FR-2.8). It
// is refused with 409 and the blocking projects while the user owns a project that has
// other members (FR-2.9). A wrong password is a 400, not a 401, as for ChangePassword.
func (h *Handler) DeleteAccount(ctx context.Context, req gen.DeleteAccountRequestObject) (gen.DeleteAccountResponseObject, error) {
	unauthenticated := gen.DeleteAccount401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse{
		Code: httpx.CodeUnauthenticated, Message: "Log in to continue.",
	}}
	id, ok := httpx.UserID(ctx)
	if !ok {
		return unauthenticated, nil
	}
	err := h.svc.DeleteAccount(ctx, usecase.DeleteAccountInput{UserID: id, Password: req.Body.Password})
	switch {
	case err == nil:
		return cookieResponse{status: http.StatusNoContent, cookies: h.clearedCookies()}, nil
	case errors.Is(err, domain.ErrWrongPassword):
		return gen.DeleteAccount400JSONResponse{Code: CodeWrongPassword, Message: "The password is wrong."}, nil
	case errors.Is(err, domain.ErrNotFound):
		return unauthenticated, nil
	case errors.Is(err, domain.ErrOwnsSharedProject):
		var shared *domain.SharedProjectsError
		if !errors.As(err, &shared) {
			return nil, err
		}
		return ownsSharedProject(shared.Projects)
	case errors.Is(err, domain.ErrAccountLocked):
		var locked *domain.LockedError
		if !errors.As(err, &locked) {
			return nil, err
		}
		seconds, body := lockedError(locked.RetryAfter)
		return gen.DeleteAccount429JSONResponse{Headers: gen.DeleteAccount429ResponseHeaders{RetryAfter: &seconds}, Body: body}, nil
	}
	return nil, err
}

// ownsSharedProject lists the projects that block an account deletion (FR-2.9).
func ownsSharedProject(projects []domain.ProjectRef) (gen.DeleteAccountResponseObject, error) {
	refs := make([]gen.ProjectRef, 0, len(projects))
	for _, p := range projects {
		var id openapi_types.UUID
		if err := id.UnmarshalText([]byte(p.ID)); err != nil {
			return nil, fmt.Errorf("project id: %w", err)
		}
		refs = append(refs, gen.ProjectRef{Id: id, Title: p.Title})
	}
	return gen.DeleteAccount409JSONResponse{
		Code:     CodeOwnsSharedProject,
		Message:  "You own projects that have other members. Transfer or delete them before you delete your account.",
		Projects: refs,
	}, nil
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

// clearedCookies returns both session cookies, set to expire at once.
func (h *Handler) clearedCookies() []*http.Cookie {
	return []*http.Cookie{
		h.cookie(httpx.AccessCookie, "", "/", -time.Second),
		h.cookie(httpx.RefreshCookie, "", refreshCookiePath, -time.Second),
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

// cookieResponse answers a refresh, a logout or an account deletion. Like loginOK, it exists because the
// generated response types can set only one Set-Cookie header. body is nil for a 204.
type cookieResponse struct {
	status  int
	body    *gen.Error
	cookies []*http.Cookie
}

func (r cookieResponse) VisitRefreshSessionResponse(w http.ResponseWriter) error { return r.write(w) }

func (r cookieResponse) VisitLogoutUserResponse(w http.ResponseWriter) error { return r.write(w) }

func (r cookieResponse) VisitDeleteAccountResponse(w http.ResponseWriter) error { return r.write(w) }

func (r cookieResponse) write(w http.ResponseWriter) error {
	for _, c := range r.cookies {
		http.SetCookie(w, c)
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.body == nil {
		w.WriteHeader(r.status)
		return nil
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(r.status)
	return json.NewEncoder(w).Encode(r.body)
}
