package usecase

import (
	"context"
	"errors"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// errUnchanged ends an edit's transaction when the text is already the one asked for.
var errUnchanged = errors.New("messaging: the text is unchanged")

// Edit replaces the text of editorID's message (FR-16.5) and returns it. Checks run in this
// order: ErrInvalidBody, ErrNotFound unless editorID sent the message in this conversation and
// it is not deleted (NFR-14), ErrEditWindowClosed, then an unchanged text returns the message
// as it is, storing and publishing nothing and using no quota. Then the checks of Send: the
// sender's standing (see refuseSender), *RateLimitedError (an edit counts as a message),
// ErrNotFound when the other member is suspended or not verified, a block (see refuseBlocked),
// and ErrRequestPending or ErrClosed unless the conversation is active. An edit never activates
// a request. The replaced text is kept for moderation by the store.
func (s *Service) Edit(ctx context.Context, editorID, conversationID, messageID, body string) (domain.Message, error) {
	text, err := NormalizeBody(body)
	if err != nil {
		return domain.Message{}, err
	}
	c, err := s.store.FindConversation(ctx, conversationID, editorID)
	if err != nil {
		return domain.Message{}, err
	}
	m, err := s.store.FindOwnMessage(ctx, conversationID, messageID, editorID)
	if err != nil {
		return domain.Message{}, err
	}
	if err := s.editable(m); err != nil {
		return domain.Message{}, err
	}
	if m.Body != nil && *m.Body == text {
		return m, nil
	}
	if err := s.refuseSender(ctx, editorID); err != nil {
		return domain.Message{}, err
	}
	if err := s.checkLimits(ctx, editorID, s.messages); err != nil {
		return domain.Message{}, err
	}
	visible, err := s.store.UserVisible(ctx, c.OtherID)
	if err != nil {
		return domain.Message{}, err
	}
	if !visible {
		return domain.Message{}, domain.ErrNotFound
	}
	if err := s.refuseBlocked(ctx, editorID, c.OtherID, true); err != nil {
		return domain.Message{}, err
	}
	if err := refuseInactive(c.State); err != nil {
		return domain.Message{}, err
	}

	var edited domain.Message
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.refuseSender(ctx, editorID); err != nil {
			return err
		}
		if err := s.lockAndRefuseBlocked(ctx, editorID, c.OtherID, true); err != nil {
			return err
		}
		locked, err := s.store.LockConversation(ctx, conversationID)
		if err != nil {
			return err
		}
		if err := refuseInactive(locked.State); err != nil {
			return err
		}
		m, err := s.store.LockOwnMessage(ctx, conversationID, messageID, editorID)
		if err != nil {
			return err
		}
		if err := s.editable(m); err != nil {
			return err
		}
		if m.Body != nil && *m.Body == text {
			edited = m
			return errUnchanged
		}
		edited, err = s.store.EditMessage(ctx, messageID, text)
		return err
	})
	if errors.Is(err, errUnchanged) {
		return edited, nil
	}
	if err != nil {
		return domain.Message{}, err
	}
	s.record(ctx, editorID, s.messages)
	s.publisher.MessageEdited(ctx, edited, []string{c.OtherID})
	return edited, nil
}

// editable refuses a deleted message, as if it were unknown, and one older than the window.
func (s *Service) editable(m domain.Message) error {
	if m.Deleted() {
		return domain.ErrNotFound
	}
	if s.now().Sub(m.CreatedAt) > s.editWindow {
		return domain.ErrEditWindowClosed
	}
	return nil
}

// refuseInactive refuses a change in a conversation that is not active, without activating a
// request as admit does for a send.
func refuseInactive(state domain.State) error {
	switch state {
	case domain.StateRequest:
		return domain.ErrRequestPending
	case domain.StateDeclined:
		return domain.ErrClosed
	}
	return nil
}

// Delete leaves deleterID's message in its place with no body (FR-16.5). Only the sender may,
// at any time: in a closed conversation, after a block, while suspended or restricted, as it
// only takes back their own words. Anyone else gets ErrNotFound (NFR-14). Deleting again
// changes nothing and publishes nothing. The text is kept for moderation by the store.
func (s *Service) Delete(ctx context.Context, deleterID, conversationID, messageID string) error {
	c, err := s.store.FindConversation(ctx, conversationID, deleterID)
	if err != nil {
		return err
	}
	var (
		deleted domain.Message
		changed bool
	)
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		m, err := s.store.LockOwnMessage(ctx, conversationID, messageID, deleterID)
		if err != nil || m.Deleted() {
			return err
		}
		deleted, err = s.store.DeleteMessage(ctx, messageID)
		changed = err == nil
		return err
	})
	if err != nil {
		return err
	}
	if changed {
		s.publisher.MessageDeleted(ctx, deleted, []string{c.OtherID})
	}
	return nil
}
