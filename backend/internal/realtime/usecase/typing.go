package usecase

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/domain"
)

type typingKey struct{ userID, conversationID string }

// typingState is one user typing in one conversation. Nothing is stored beyond this. A state
// lives until its timer fires, so a refused start is throttled too; told is empty until a
// start has been relayed, and then holds everyone who saw one.
type typingState struct {
	last  time.Time
	told  []string
	timer *time.Timer
}

// typingStart relays a start to the other member, or in a group to every other member it may
// reach, at most once per throttle window. The rule is checked on every relayed start, so a
// block stops it at once. The window is per instance: two tabs on two instances can each relay
// one.
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

	peers := h.typingPeers(ctx, userID, conversationID)
	if len(peers) == 0 {
		return
	}
	h.typingMu.Lock()
	for _, p := range peers {
		if !slices.Contains(s.told, p) {
			s.told = append(s.told, p)
		}
	}
	h.typingMu.Unlock()
	for _, p := range peers {
		h.sendTyping(ctx, p, conversationID, userID, true)
	}
}

// typingStop relays a stop that follows a relayed start. When it is refused the state stays,
// so the auto-stop still clears what the other member sees.
func (h *Hub) typingStop(ctx context.Context, userID, conversationID string) {
	k := typingKey{userID, conversationID}
	h.typingMu.Lock()
	s := h.typing[k]
	relayed := s != nil && len(s.told) > 0
	h.typingMu.Unlock()
	if !relayed {
		return
	}
	if len(h.typingPeers(ctx, userID, conversationID)) == 0 {
		return
	}
	h.typingMu.Lock()
	var told []string
	if h.typing[k] == s {
		s.timer.Stop()
		delete(h.typing, k)
		told = s.told
	}
	h.typingMu.Unlock()
	for _, p := range told {
		h.sendTyping(ctx, p, conversationID, userID, false)
	}
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
	told := s.told
	h.typingMu.Unlock()
	if len(told) == 0 {
		return
	}
	ctx, cancel := h.callContext()
	defer cancel()
	for _, p := range told {
		h.sendTyping(ctx, p, k.conversationID, k.userID, false)
	}
}

func (h *Hub) typingPeers(ctx context.Context, userID, conversationID string) []string {
	peers, err := h.access.TypingPeers(ctx, userID, conversationID)
	if err != nil {
		h.logger.WarnContext(ctx, "realtime: check typing", slog.Any("error", err))
		return nil
	}
	if len(peers) == 0 {
		h.logger.DebugContext(ctx, "realtime: typing refused", slog.String("user_id", userID))
	}
	return peers
}

func (h *Hub) sendTyping(ctx context.Context, to, conversationID, userID string, isTyping bool) {
	h.Send(ctx, to, domain.Event{Type: domain.EventTyping, Data: domain.Typing{
		ConversationID: conversationID, UserID: userID, IsTyping: isTyping,
	}})
}
