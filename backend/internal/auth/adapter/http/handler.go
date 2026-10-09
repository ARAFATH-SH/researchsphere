// Package http serves the auth operations in docs/openapi.yaml: registration, email
// verification and new verification links (FR-1). It turns the usecase's errors into
// the error codes the web client shows next to each field.
package http

import (
	"context"
	"errors"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/usecase"
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
)

// service is the part of usecase.Service this handler needs.
type service interface {
	Register(ctx context.Context, in usecase.RegisterInput) (usecase.RegisterResult, error)
	VerifyEmail(ctx context.Context, token string) error
	ResendVerification(ctx context.Context, email string) error
}

// Handler implements the auth operations of gen.StrictServerInterface.
type Handler struct {
	svc service
}

// NewHandler returns a Handler that calls svc.
func NewHandler(svc service) *Handler {
	return &Handler{svc: svc}
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
