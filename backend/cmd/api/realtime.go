package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	authdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
	messaginghttp "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/http"
	messagingdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	messagingusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/usecase"
	moderationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/usecase"
	notificationhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/adapter/http"
	notificationdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/domain"
	notificationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	realtimeredis "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/adapter/redis"
	realtimedomain "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/domain"
	realtimeusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/usecase"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

// realtimeParts is the realtime module, wired. The messaging and notification usecases need
// events and events need them, so bind finishes the job once they exist.
type realtimeParts struct {
	hub    *realtimeusecase.Hub
	events *realtimeEvents
	access *realtimeAccess
	close  func()
}

func newRealtime(rdb *goredis.Client, standings *moderationusecase.Standings, cfg realtimeusecase.Config, logger *slog.Logger) *realtimeParts {
	bus := realtimeredis.NewBus(rdb)
	presence := realtimeredis.NewPresence(rdb)
	access := &realtimeAccess{standings: standings}
	hub := realtimeusecase.NewHub(bus, presence, access, cfg, time.Now, logger)
	return &realtimeParts{
		hub: hub, access: access, events: &realtimeEvents{hub: hub, logger: logger},
		close: func() {
			_ = bus.Close()
			_ = presence.Close()
		},
	}
}

func (p *realtimeParts) bind(messaging *messagingusecase.Service, notifications *notificationusecase.Service) {
	p.events.messaging, p.events.notifications = messaging, notifications
	p.access.messaging = messaging
}

// realtimeAccess answers the realtime module's questions with the messaging and moderation
// usecases. No module imports another; this is the only place that joins them.
type realtimeAccess struct {
	messaging *messagingusecase.Service
	standings *moderationusecase.Standings
}

var _ realtimedomain.Access = (*realtimeAccess)(nil)

func (a *realtimeAccess) TypingPeers(ctx context.Context, userID, conversationID string) ([]string, error) {
	return a.messaging.TypingPeers(ctx, userID, conversationID)
}

func (a *realtimeAccess) Peers(ctx context.Context, userID string) ([]string, error) {
	return a.messaging.ActivePeers(ctx, userID)
}

func (a *realtimeAccess) Visible(ctx context.Context, viewerID string, userIDs []string) ([]string, error) {
	return a.messaging.VisiblePeers(ctx, viewerID, userIDs)
}

func (a *realtimeAccess) Suspension(ctx context.Context, userID string) (*realtimedomain.Suspension, error) {
	s, err := a.standings.Standing(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !s.Suspended {
		return nil, nil //nolint:nilnil // no suspension is the ordinary answer
	}
	return &realtimedomain.Suspension{Reason: s.SuspensionReason, Until: s.SuspendedUntil}, nil
}

// realtimeEvents turns changes in other modules into events. Every method runs through
// postgres.AfterCommit, so a call inside a transaction waits for its commit and is dropped
// if it rolls back, and a call outside one runs at once. A failure is logged and never
// reaches the change it reports.
type realtimeEvents struct {
	hub           *realtimeusecase.Hub
	messaging     *messagingusecase.Service
	notifications *notificationusecase.Service
	logger        *slog.Logger
}

var (
	_ messagingdomain.Publisher    = (*realtimeEvents)(nil)
	_ notificationdomain.Publisher = (*realtimeEvents)(nil)
	_ authdomain.SessionListener   = (*realtimeEvents)(nil)
)

// MessageSent tells the other members, and the sender's own tabs, with clientId for those only
// (docs/realtime.md). Sending also moves the sender's read marker, so both counts may change. A
// system message of a group goes to the to list only and changes no count.
func (e *realtimeEvents) MessageSent(ctx context.Context, sent messagingdomain.Sent, to []string) {
	postgres.AfterCommit(ctx, func(ctx context.Context) {
		body, err := messaginghttp.MessageBody(sent.Message)
		if err != nil {
			e.logger.ErrorContext(ctx, "realtime: message event", slog.Any("error", err))
			return
		}
		data := gen.RealtimeMessageCreated{Message: body, ConversationState: gen.ConversationState(sent.State)}
		if sent.Message.System() {
			for _, userID := range to {
				e.hub.Send(ctx, userID, realtimedomain.Event{Type: realtimedomain.EventMessageCreated, Data: data})
			}
			return
		}
		for _, userID := range to {
			e.hub.Send(ctx, userID, realtimedomain.Event{Type: realtimedomain.EventMessageCreated, Data: data})
			e.sendUnread(ctx, userID)
		}
		mine := data
		if id, err := parseUUID(sent.Message.ClientID); err == nil {
			mine.ClientId = &id
		}
		e.hub.Send(ctx, sent.Message.SenderID, realtimedomain.Event{Type: realtimedomain.EventMessageCreated, Data: mine})
		e.sendUnread(ctx, sent.Message.SenderID)
	})
}

// MessageEdited tells the other member and the sender's own tabs the new text (FR-16.5).
func (e *realtimeEvents) MessageEdited(ctx context.Context, m messagingdomain.Message, to []string) {
	postgres.AfterCommit(ctx, func(ctx context.Context) {
		body, err := messaginghttp.MessageBody(m)
		if err != nil {
			e.logger.ErrorContext(ctx, "realtime: message edited event", slog.Any("error", err))
			return
		}
		ev := realtimedomain.Event{Type: realtimedomain.EventMessageUpdated, Data: gen.RealtimeMessageUpdated{Message: body}}
		for _, userID := range append(slices.Clone(to), m.SenderID) {
			e.hub.Send(ctx, userID, ev)
		}
	})
}

// MessageDeleted tells the other member and the sender's own tabs (FR-16.5). A deleted
// message no longer counts as unread, so the other members' counts may change.
func (e *realtimeEvents) MessageDeleted(ctx context.Context, m messagingdomain.Message, to []string) {
	postgres.AfterCommit(ctx, func(ctx context.Context) {
		c, err1 := parseUUID(m.ConversationID)
		id, err2 := parseUUID(m.ID)
		if err := errors.Join(err1, err2); err != nil {
			e.logger.ErrorContext(ctx, "realtime: message deleted event", slog.Any("error", err))
			return
		}
		ev := realtimedomain.Event{Type: realtimedomain.EventMessageDeleted, Data: gen.RealtimeMessageDeleted{ConversationId: c, MessageId: id}}
		for _, userID := range to {
			e.hub.Send(ctx, userID, ev)
			e.sendUnread(ctx, userID)
		}
		e.hub.Send(ctx, m.SenderID, ev)
	})
}

// ReadMoved tells the other member for "Seen", and the reader's own tabs their new counts.
func (e *realtimeEvents) ReadMoved(ctx context.Context, conversationID, readerID, messageID string, to []string) {
	postgres.AfterCommit(ctx, func(ctx context.Context) {
		c, err1 := parseUUID(conversationID)
		m, err2 := parseUUID(messageID)
		if err := errors.Join(err1, err2); err != nil {
			e.logger.ErrorContext(ctx, "realtime: read event", slog.Any("error", err))
			return
		}
		for _, userID := range to {
			e.hub.Send(ctx, userID, realtimedomain.Event{
				Type: realtimedomain.EventConversationRead,
				Data: gen.RealtimeConversationRead{ConversationId: c, MessageId: m},
			})
		}
		e.sendUnread(ctx, readerID)
	})
}

// RequestAccepted tells both members: to names the sender, and the conversation names the rest.
func (e *realtimeEvents) RequestAccepted(ctx context.Context, conversationID string, to []string) {
	e.answered(ctx, conversationID, to)
}

// RequestDeclined does the same as RequestAccepted.
func (e *realtimeEvents) RequestDeclined(ctx context.Context, conversationID string, to []string) {
	e.answered(ctx, conversationID, to)
}

func (e *realtimeEvents) answered(ctx context.Context, conversationID string, to []string) {
	postgres.AfterCommit(ctx, func(ctx context.Context) {
		for _, userID := range to {
			e.conversationChanged(ctx, conversationID, userID)
			if it, err := e.messaging.Get(ctx, userID, conversationID); err == nil {
				e.conversationChanged(ctx, conversationID, it.OtherID)
			}
		}
	})
}

// PairChanged tells both members of a and b's conversation, when they have one.
func (e *realtimeEvents) PairChanged(ctx context.Context, a, b string) {
	postgres.AfterCommit(ctx, func(ctx context.Context) {
		id, found, err := e.messaging.ConversationBetween(ctx, a, b)
		if err != nil {
			e.logger.WarnContext(ctx, "realtime: find conversation", slog.Any("error", err))
			return
		}
		if found {
			e.conversationChanged(ctx, id, a)
			e.conversationChanged(ctx, id, b)
		}
	})
}

// MembersChanged tells the group's current members what changed (FR-16.6), so they refetch it.
func (e *realtimeEvents) MembersChanged(ctx context.Context, conversationID string, change messagingdomain.MemberChange, to []string) {
	postgres.AfterCommit(ctx, func(ctx context.Context) {
		id, err := parseUUID(conversationID)
		if err != nil {
			e.logger.ErrorContext(ctx, "realtime: members event", slog.Any("error", err))
			return
		}
		users := make([]openapi_types.UUID, 0, len(change.UserIDs))
		for _, u := range change.UserIDs {
			uid, err := parseUUID(u)
			if err != nil {
				e.logger.ErrorContext(ctx, "realtime: members event", slog.Any("error", err))
				return
			}
			users = append(users, uid)
		}
		data := gen.RealtimeConversationMembers{
			ConversationId: id, Change: gen.RealtimeConversationMembersChange(change.Kind), UserIds: users, Name: change.Name,
		}
		if change.ActorID != "" {
			actor, err := parseUUID(change.ActorID)
			if err != nil {
				e.logger.ErrorContext(ctx, "realtime: members event", slog.Any("error", err))
				return
			}
			data.ActorId = &actor
		}
		for _, userID := range to {
			e.hub.Send(ctx, userID, realtimedomain.Event{Type: realtimedomain.EventConversationMembers, Data: data})
		}
	})
}

// AccessLost tells each user who left or was removed that the group is gone for them, so an
// open thread closes, and sends their counts, which no longer include it.
func (e *realtimeEvents) AccessLost(ctx context.Context, conversationID string, userIDs []string) {
	postgres.AfterCommit(ctx, func(ctx context.Context) {
		id, err := parseUUID(conversationID)
		if err != nil {
			e.logger.ErrorContext(ctx, "realtime: access lost event", slog.Any("error", err))
			return
		}
		for _, userID := range userIDs {
			e.hub.Send(ctx, userID, realtimedomain.Event{
				Type: realtimedomain.EventConversationUpdated,
				Data: gen.RealtimeConversationUpdated{ConversationId: id, State: gen.Active, CanMessage: false, AccessLost: true},
			})
			e.sendUnread(ctx, userID)
		}
	})
}

// conversationChanged sends the user their own view of the conversation, and their counts: a
// request that was answered or declined leaves the requests count.
func (e *realtimeEvents) conversationChanged(ctx context.Context, conversationID, userID string) {
	it, err := e.messaging.Get(ctx, userID, conversationID)
	if err != nil {
		e.logger.WarnContext(ctx, "realtime: read conversation", slog.Any("error", err))
		return
	}
	id, err := parseUUID(conversationID)
	if err != nil {
		e.logger.ErrorContext(ctx, "realtime: conversation event", slog.Any("error", err))
		return
	}
	e.hub.Send(ctx, userID, realtimedomain.Event{
		Type: realtimedomain.EventConversationUpdated,
		Data: gen.RealtimeConversationUpdated{ConversationId: id, State: gen.ConversationState(it.State), CanMessage: it.CanMessage()},
	})
	e.sendUnread(ctx, userID)
}

func (e *realtimeEvents) sendUnread(ctx context.Context, userID string) {
	c, err := e.messaging.UnreadCounts(ctx, userID)
	if err != nil {
		e.logger.WarnContext(ctx, "realtime: count unread", slog.Any("error", err))
		return
	}
	e.hub.Send(ctx, userID, realtimedomain.Event{
		Type: realtimedomain.EventUnreadChanged,
		Data: gen.MessageUnreadCount{Total: c.Total, Requests: c.Requests},
	})
}

// hidePresence takes the two out of each other's view, once a block has committed.
func (e *realtimeEvents) hidePresence(ctx context.Context, a, b string) {
	postgres.AfterCommit(ctx, func(ctx context.Context) { e.hub.HidePresence(ctx, a, b) })
}

// accountSuspended closes the user's sockets (decisions.md, 2026-11-05) and tells their
// conversation partners, whose canMessage changed, once the suspension has committed.
func (e *realtimeEvents) accountSuspended(ctx context.Context, userID string) {
	postgres.AfterCommit(ctx, func(ctx context.Context) {
		e.hub.CloseUser(ctx, userID, realtimedomain.CloseSessionEnded)
		e.partnersChanged(ctx, userID)
	})
}

// accountRestored tells the user's conversation partners, whose canMessage changed.
func (e *realtimeEvents) accountRestored(ctx context.Context, userID string) {
	postgres.AfterCommit(ctx, func(ctx context.Context) { e.partnersChanged(ctx, userID) })
}

func (e *realtimeEvents) partnersChanged(ctx context.Context, userID string) {
	partners, err := e.messaging.Partners(ctx, userID)
	if err != nil {
		e.logger.WarnContext(ctx, "realtime: list partners", slog.Any("error", err))
		return
	}
	for _, p := range partners {
		e.conversationChanged(ctx, p.ConversationID, p.UserID)
	}
}

// SessionsEnded closes the user's sockets. The auth module calls it after its commit.
func (e *realtimeEvents) SessionsEnded(ctx context.Context, userID string) {
	e.hub.CloseUser(ctx, userID, realtimedomain.CloseSessionEnded)
}

// Created sends the new notification as the list shows it. One hidden in the meantime is
// not sent.
func (e *realtimeEvents) Created(ctx context.Context, id, recipientID string) {
	postgres.AfterCommit(ctx, func(ctx context.Context) {
		it, err := e.notifications.Get(ctx, recipientID, id)
		if errors.Is(err, notificationdomain.ErrNotFound) {
			return
		}
		if err != nil {
			e.logger.WarnContext(ctx, "realtime: read notification", slog.Any("error", err))
			return
		}
		body, err := notificationhttp.NotificationBody(it)
		if err != nil {
			e.logger.ErrorContext(ctx, "realtime: notification event", slog.Any("error", err))
			return
		}
		e.hub.Send(ctx, recipientID, realtimedomain.Event{Type: realtimedomain.EventNotificationCreated, Data: body})
	})
}

// UnreadChanged sends the bell's count.
func (e *realtimeEvents) UnreadChanged(ctx context.Context, recipientID string) {
	postgres.AfterCommit(ctx, func(ctx context.Context) {
		n, err := e.notifications.UnreadCount(ctx, recipientID)
		if err != nil {
			e.logger.WarnContext(ctx, "realtime: count notifications", slog.Any("error", err))
			return
		}
		e.hub.Send(ctx, recipientID, realtimedomain.Event{
			Type: realtimedomain.EventNotificationsUnread,
			Data: gen.UnreadNotificationCount{Count: n},
		})
	})
}

func parseUUID(s string) (openapi_types.UUID, error) {
	var id openapi_types.UUID
	if err := id.UnmarshalText([]byte(s)); err != nil {
		return id, fmt.Errorf("parse UUID: %w", err)
	}
	return id, nil
}
