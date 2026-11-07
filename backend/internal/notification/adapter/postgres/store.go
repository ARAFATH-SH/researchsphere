// Package postgres implements the notification domain's Store with the queries that sqlc
// generates from queries.sql. Every query runs in the transaction the context carries
// (platform/postgres.Transactor), so a notification is written together with the change
// another module reports.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/notification/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/notification/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// Store reads and writes notifications through the shared pool.
type Store struct {
	pool *pgxpool.Pool
}

var _ domain.Store = (*Store)(nil)

// NewStore returns a Store that uses pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// q returns the queries on the context's transaction, or on the pool.
func (s *Store) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Conn(ctx, s.pool))
}

// Put needs UUIDs for the recipient, subject and any actor: they come from another module's
// stored rows, so anything else is a programming error. An empty actor is NULL.
func (s *Store) Put(ctx context.Context, n domain.Notification) (string, error) {
	user, ok1 := parseUUID(n.RecipientID)
	actor, ok2 := pgtype.UUID{}, true
	if n.ActorID != "" {
		actor, ok2 = parseUUID(n.ActorID)
	}
	subject, ok3 := parseUUID(n.SubjectID)
	if !ok1 || !ok2 || !ok3 {
		return "", fmt.Errorf("store notification: recipient %q, actor %q or subject %q is not a UUID", n.RecipientID, n.ActorID, n.SubjectID)
	}
	id, err := s.q(ctx).PutNotification(ctx, sqlcgen.PutNotificationParams{
		UserID: user, ActorID: actor, Type: sqlcgen.NotificationType(n.Type), EntityID: subject,
	})
	if err != nil {
		return "", fmt.Errorf("store notification: %w", err)
	}
	return id.String(), nil
}

// DeleteBySubject deletes nothing for a subject that is not a UUID.
func (s *Store) DeleteBySubject(ctx context.Context, t domain.Type, subjectID string) ([]string, error) {
	subject, ok := parseUUID(subjectID)
	if !ok {
		return nil, nil
	}
	rows, err := s.q(ctx).DeleteNotificationsBySubject(ctx, sqlcgen.DeleteNotificationsBySubjectParams{
		Type: sqlcgen.NotificationType(t), EntityID: subject,
	})
	if err != nil {
		return nil, fmt.Errorf("delete notifications: %w", err)
	}
	recipients := make([]string, len(rows))
	for i, r := range rows {
		recipients[i] = r.String()
	}
	return recipients, nil
}

// Find answers ErrNotFound for an ID that is not a UUID.
func (s *Store) Find(ctx context.Context, recipientID, id string) (domain.Item, error) {
	user, ok1 := parseUUID(recipientID)
	nid, ok2 := parseUUID(id)
	if !ok1 || !ok2 {
		return domain.Item{}, domain.ErrNotFound
	}
	r, err := s.q(ctx).FindNotification(ctx, sqlcgen.FindNotificationParams{ID: nid, UserID: user})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Item{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Item{}, fmt.Errorf("find notification: %w", err)
	}
	return item(recipientID, sqlcgen.ListNotificationsRow(r)), nil
}

// List finds nothing for a recipient that is not a UUID. The cursor's ID was checked
// when it was decoded.
func (s *Store) List(ctx context.Context, recipientID string, after *domain.Cursor, limit int) ([]domain.Item, error) {
	user, ok := parseUUID(recipientID)
	if !ok {
		return nil, nil
	}
	arg := sqlcgen.ListNotificationsParams{UserID: user, Lim: int32(min(limit, 1000))} //nolint:gosec // bounded above
	if after != nil {
		id, ok := parseUUID(after.ID)
		if !ok {
			return nil, domain.ErrInvalidCursor
		}
		arg.HasCursor = true
		arg.CursorAt = pgtype.Timestamptz{Time: after.CreatedAt, Valid: true}
		arg.CursorID = id
	}
	rows, err := s.q(ctx).ListNotifications(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	items := make([]domain.Item, len(rows))
	for i, r := range rows {
		items[i] = item(recipientID, r)
	}
	return items, nil
}

func item(recipientID string, r sqlcgen.ListNotificationsRow) domain.Item {
	pending := r.RequestPending
	it := domain.Item{
		Notification: domain.Notification{
			ID: r.ID.String(), RecipientID: recipientID,
			Type: domain.Type(r.Type), SubjectID: r.EntityID.String(),
			CreatedAt: r.CreatedAt.Time, ReadAt: timePtr(r.ReadAt),
		},
		RequestPending: &pending,
	}
	if r.ActorID.Valid {
		it.ActorID = r.ActorID.String()
		it.Actor = &domain.Actor{UserID: it.ActorID, FullName: r.FullName, PhotoFileID: uuidPtr(r.PhotoFileID)}
	}
	return it
}

// CountUnread is 0 for a recipient that is not a UUID.
func (s *Store) CountUnread(ctx context.Context, recipientID string) (int, error) {
	user, ok := parseUUID(recipientID)
	if !ok {
		return 0, nil
	}
	n, err := s.q(ctx).CountUnreadNotifications(ctx, user)
	if err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return int(n), nil
}

// MarkRead finds nothing for an ID that is not a UUID.
func (s *Store) MarkRead(ctx context.Context, id, recipientID string) (bool, error) {
	nid, ok1 := parseUUID(id)
	user, ok2 := parseUUID(recipientID)
	if !ok1 || !ok2 {
		return false, nil
	}
	n, err := s.q(ctx).MarkNotificationRead(ctx, sqlcgen.MarkNotificationReadParams{ID: nid, UserID: user})
	if err != nil {
		return false, fmt.Errorf("mark notification read: %w", err)
	}
	return n > 0, nil
}

// DeleteBetween does nothing for a value that is not a UUID.
func (s *Store) DeleteBetween(ctx context.Context, a, b string) error {
	ua, ok1 := parseUUID(a)
	ub, ok2 := parseUUID(b)
	if !ok1 || !ok2 {
		return nil
	}
	if err := s.q(ctx).DeleteNotificationsBetween(ctx, sqlcgen.DeleteNotificationsBetweenParams{A: ua, B: ub}); err != nil {
		return fmt.Errorf("delete notifications between users: %w", err)
	}
	return nil
}

// MarkAllRead marks nothing for a recipient that is not a UUID.
func (s *Store) MarkAllRead(ctx context.Context, recipientID string) (int, error) {
	user, ok := parseUUID(recipientID)
	if !ok {
		return 0, nil
	}
	n, err := s.q(ctx).MarkAllNotificationsRead(ctx, user)
	if err != nil {
		return 0, fmt.Errorf("mark all notifications read: %w", err)
	}
	return int(n), nil
}

func parseUUID(s string) (pgtype.UUID, bool) {
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		return id, false
	}
	return id, true
}

func uuidPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := u.String()
	return &s
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
