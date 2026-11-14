package http

import (
	"context"
	"errors"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// CodeInvalidToken answers an unsubscribe token this server did not sign (FR-16.11).
const CodeInvalidToken = "invalid_token"

// maxTokenLength is the longest unsubscribe token accepted; a real one is 66 characters.
const maxTokenLength = 200

// DigestService is what the handler needs for the settings and the digest (FR-16.11).
type DigestService interface {
	Settings(ctx context.Context, userID string) (domain.Settings, error)
	UpdateSettings(ctx context.Context, userID string, u domain.SettingsUpdate) (domain.Settings, error)
	Unsubscribe(ctx context.Context, token string) error
}

var errNoDigests = errors.New("messaging: the digest service is not configured")

// GetMySettings returns the caller's settings.
func (h *Handler) GetMySettings(ctx context.Context, _ gen.GetMySettingsRequestObject) (gen.GetMySettingsResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.GetMySettings401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if h.digests == nil {
		return nil, errNoDigests
	}
	s, err := h.digests.Settings(ctx, id)
	if err != nil {
		return nil, err
	}
	return gen.GetMySettings200JSONResponse(settingsBody(s)), nil
}

// UpdateMySettings changes the settings the body names.
func (h *Handler) UpdateMySettings(ctx context.Context, req gen.UpdateMySettingsRequestObject) (gen.UpdateMySettingsResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.UpdateMySettings401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if h.digests == nil {
		return nil, errNoDigests
	}
	var u domain.SettingsUpdate
	if req.Body != nil {
		u.UnreadMessageDigest = req.Body.UnreadMessageDigest
	}
	s, err := h.digests.UpdateSettings(ctx, id, u)
	if errors.Is(err, domain.ErrNoSetting) {
		return gen.UpdateMySettings400JSONResponse{Code: httpx.CodeBadRequest, Message: "Name at least one setting to change."}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.UpdateMySettings200JSONResponse(settingsBody(s)), nil
}

// UnsubscribeDigest turns the digest off with the token from its email. It needs no session
// (RFC 8058 one-click unsubscribe).
func (h *Handler) UnsubscribeDigest(ctx context.Context, req gen.UnsubscribeDigestRequestObject) (gen.UnsubscribeDigestResponseObject, error) {
	if h.digests == nil {
		return nil, errNoDigests
	}
	invalid := gen.UnsubscribeDigest400JSONResponse{Code: CodeInvalidToken, Message: "This unsubscribe link is not valid.", Field: new("token")}
	if len(req.Params.Token) > maxTokenLength {
		return invalid, nil
	}
	err := h.digests.Unsubscribe(ctx, req.Params.Token)
	if errors.Is(err, domain.ErrInvalidUnsubscribeToken) {
		return invalid, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.UnsubscribeDigest204Response{}, nil
}

func settingsBody(s domain.Settings) gen.UserSettings {
	return gen.UserSettings{UnreadMessageDigest: s.UnreadMessageDigest}
}
