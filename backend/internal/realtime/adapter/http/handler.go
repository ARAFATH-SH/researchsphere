// Package http serves the realtime module's operations: the WebSocket upgrade and the
// presence list (docs/realtime.md). It is the only code that knows the WebSocket library.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/usecase"
)

// MaxFrame is the largest frame a client may send. A bigger one closes the socket with 1009.
const MaxFrame = 4 << 10

// Error codes of these operations.
const (
	CodeOriginNotAllowed    = "origin_not_allowed"
	CodeUpgradeRequired     = "upgrade_required"
	CodeRealtimeUnavailable = "realtime_unavailable"
	CodeAccountSuspended    = "account_suspended"
)

// handshakeTimeout bounds the checks before the upgrade.
const handshakeTimeout = 3 * time.Second

// Service is what the handlers need from the hub.
type Service interface {
	Reserve(ctx context.Context, userID string) (*usecase.Reservation, error)
	Suspension(ctx context.Context, userID string) (*domain.Suspension, error)
	Online(ctx context.Context, viewerID string, userIDs []string) ([]domain.Status, error)
}

// Handler implements the realtime operations.
type Handler struct {
	svc    Service
	origin string // the web client's origin, in lower case
	host   string
	logger *slog.Logger
}

// NewHandler returns a Handler that accepts sockets from the origin of appBaseURL.
func NewHandler(svc Service, appBaseURL string, logger *slog.Logger) *Handler {
	h := &Handler{svc: svc, logger: logger}
	if u, err := url.Parse(appBaseURL); err == nil && u.Host != "" {
		h.origin = strings.ToLower(u.Scheme + "://" + u.Host)
		h.host = strings.ToLower(u.Host)
	}
	return h
}

// upgrade is the response of OpenWebSocket: the strict wrapper calls Visit with the writer,
// and it stays there for as long as the socket lives.
type upgrade struct {
	h *Handler
	r *http.Request
}

// OpenWebSocket upgrades the request. The session and the role were checked by the router,
// because the contract secures this operation.
func (h *Handler) OpenWebSocket(ctx context.Context, _ gen.OpenWebSocketRequestObject) (gen.OpenWebSocketResponseObject, error) {
	r, ok := httpx.Request(ctx)
	if !ok {
		return nil, errors.New("open websocket: no request in the context")
	}
	return upgrade{h: h, r: r}, nil
}

// VisitOpenWebSocketResponse runs the handshake, then reads frames until the socket ends.
func (u upgrade) VisitOpenWebSocketResponse(w http.ResponseWriter) error {
	h, r := u.h, u.r
	ctx := r.Context()
	userID, ok := httpx.UserID(ctx)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthenticated, "Log in to continue.")
		return nil
	}
	if h.origin == "" || !strings.EqualFold(r.Header.Get("Origin"), h.origin) {
		httpx.WriteError(w, http.StatusForbidden, CodeOriginNotAllowed, "This origin may not open a connection.")
		return nil
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		httpx.WriteError(w, http.StatusUpgradeRequired, CodeUpgradeRequired, "This address accepts only WebSocket connections.")
		return nil
	}

	hctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	// Subscribe first, then check: a suspension that commits after the check is heard.
	res, err := h.svc.Reserve(hctx, userID)
	if err != nil {
		h.logger.WarnContext(ctx, "websocket refused: no subscription", slog.Any("error", err))
		httpx.WriteError(w, http.StatusServiceUnavailable, CodeRealtimeUnavailable, "Live updates are not available right now.")
		return nil
	}
	suspended, err := h.svc.Suspension(hctx, userID)
	if err != nil {
		res.Cancel(ctx)
		return err
	}
	if suspended != nil {
		res.Cancel(ctx)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		return json.NewEncoder(w).Encode(gen.Error{
			Code: CodeAccountSuspended, Message: "This account is suspended. Contact support.",
			Reason: suspended.Reason, Until: suspended.Until,
		})
	}

	// A hijacked connection keeps the server's read and write deadlines.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns:  []string{h.host}, // checked above; Accept compares only the host
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		res.Cancel(ctx)
		h.logger.WarnContext(ctx, "websocket upgrade failed", slog.Any("error", err))
		return nil // Accept has answered
	}
	conn.SetReadLimit(MaxFrame)

	expires, _ := httpx.TokenExpiry(ctx)
	client := res.Attach(socket{conn}, expires)
	defer client.Done()
	for {
		kind, frame, err := conn.Read(ctx)
		if err != nil {
			return nil
		}
		if kind == websocket.MessageText {
			client.Receive(ctx, frame)
		}
	}
}

// socket adapts a connection to domain.Socket.
type socket struct{ conn *websocket.Conn }

func (s socket) Write(ctx context.Context, frame []byte) error {
	return s.conn.Write(ctx, websocket.MessageText, frame)
}

func (s socket) Ping(ctx context.Context) error { return s.conn.Ping(ctx) }

func (s socket) Close(code domain.CloseCode) error {
	return s.conn.Close(websocket.StatusCode(code), code.Text())
}

// GetPresence answers who is online (FR-16.4).
func (h *Handler) GetPresence(ctx context.Context, req gen.GetPresenceRequestObject) (gen.GetPresenceResponseObject, error) {
	userID, ok := httpx.UserID(ctx)
	if !ok {
		return nil, errors.New("get presence: no user in the context")
	}
	var ids []string
	if req.Params.UserIds != nil {
		for id := range strings.SplitSeq(*req.Params.UserIds, ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
	}
	for _, id := range ids {
		if !isUUID(id) {
			return invalidIDs("userIds must be a comma-separated list of user IDs."), nil
		}
	}
	items, err := h.svc.Online(ctx, userID, ids)
	switch {
	case errors.Is(err, domain.ErrNoIDs):
		return invalidIDs("userIds is required."), nil
	case errors.Is(err, domain.ErrTooManyIDs):
		return invalidIDs("userIds may hold at most 50 IDs."), nil
	case err != nil:
		return nil, err
	}
	out := gen.PresenceList{Items: make([]gen.PresenceStatus, len(items))}
	for i, it := range items {
		var id openapi_types.UUID
		if err := id.UnmarshalText([]byte(it.UserID)); err != nil {
			return nil, err
		}
		out.Items[i] = gen.PresenceStatus{UserId: id, Online: it.Online}
	}
	return gen.GetPresence200JSONResponse(out), nil
}

func invalidIDs(message string) gen.GetPresenceResponseObject {
	field := "userIds"
	return gen.GetPresence400JSONResponse{Code: httpx.CodeBadRequest, Message: message, Field: &field}
}

// isUUID checks the shape only: the store treats anything else as a missing row, but the
// contract answers 400.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := range len(s) {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if s[i] != '-' {
				return false
			}
		case !strings.ContainsRune("0123456789abcdefABCDEF", rune(s[i])):
			return false
		}
	}
	return true
}
