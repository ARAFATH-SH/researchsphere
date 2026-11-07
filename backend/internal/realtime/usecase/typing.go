package usecase

import (
	"context"
	"log/slog"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/domain"
)

type typingKey struct{ userID, conversationID string }

// typingState is one user typing in one conversation. Nothing is stored beyond this. A state
// lives until its timer fires, so a refused start is throttled too; other is empty until a
// start has been relayed.
type typingState struct {
	last  time.Time
	other string
	timer *time.Timer
}

// typingStart relays a start to the other member, at most once per throttle window. The rule
// is checked on every relayed start, so a block stops it at once. The window is per instance:
// two tabs on two instances can each relay one.
func (h *Hub) typingStart(ctx context.Context, userID, conversationID string) {
	k := typingKey{userID, conversationID}
	now := h.now()
	h.typingMu.Lock()
	s := h.typing[k]
	if s != nil && now.Sub(s.last) < h.cfg.TypingThrottle {
		h.typingMu.Unlock()
		return
	}
	if s == nil {
		s = &typingState{}
		h.typing[k] = s
	} else {
		s.timer.Stop()
	}
	s.last = now
	s.timer = time.AfterFunc(h.cfg.TypingTimeout, func() { h.typingExpired(k, s) })
	h.typingMu.Unlock()

	other, ok := h.typingPeer(ctx, userID, conversationID)
	if !ok {
		return
	}
	h.typingMu.Lock()
	s.other = other
	h.typingMu.Unlock()
	h.sendTyping(ctx, other, conversationID, userID, true)
}

// typingStop relays a stop that follows a relayed start. When it is refused the state stays,
// so the auto-stop still clears what the other member sees.
func (h *Hub) typingStop(ctx context.Context, userID, conversationID string) {
	k := typingKey{userID, conversationID}
	h.typingMu.Lock()
	s := h.typing[k]
	relayed := s != nil && s.other != ""
	h.typingMu.Unlock()
	if !relayed {
		return
	}
	other, ok := h.typingPeer(ctx, userID, conversationID)
	if !ok {
		return
	}
	h.typingMu.Lock()
	if h.typing[k] == s {
		s.timer.Stop()
		delete(h.typing, k)
	}
	h.typingMu.Unlock()
	h.sendTyping(ctx, other, conversationID, userID, false)
}

// typingExpired is the auto-stop. It only ever clears an indicator already shown, so it needs
// no new check.
func (h *Hub) typingExpired(k typingKey, s *typingState) {
	h.typingMu.Lock()
	if h.typing[k] != s {
		h.typingMu.Unlock()
		return
	}
	delete(h.typing, k)
	other := s.other
	h.typingMu.Unlock()
	if other == "" {
		return
	}
	ctx, cancel := h.callContext()
	defer cancel()
	h.sendTyping(ctx, other, k.conversationID, k.userID, false)
}

func (h *Hub) typingPeer(ctx context.Context, userID, conversationID string) (string, bool) {
	other, ok, err := h.access.TypingPeer(ctx, userID, conversationID)
	if err != nil {
		h.logger.WarnContext(ctx, "realtime: check typing", slog.Any("error", err))
		return "", false
	}
	if !ok {
		h.logger.DebugContext(ctx, "realtime: typing refused", slog.String("user_id", userID))
	}
	return other, ok
}

func (h *Hub) sendTyping(ctx context.Context, to, conversationID, userID string, isTyping bool) {
	h.Send(ctx, to, domain.Event{Type: domain.EventTyping, Data: domain.Typing{
		ConversationID: conversationID, UserID: userID, IsTyping: isTyping,
	}})
}
