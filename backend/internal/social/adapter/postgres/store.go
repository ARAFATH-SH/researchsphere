// Package postgres implements the social domain's Store with the queries that sqlc
// generates from queries.sql.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/social/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

// Store reads and writes connection requests through the shared pool. Every query
// outside readTx runs in the transaction the context carries (platform/postgres.Transactor),
// so a change and its notification commit together.
type Store struct {
	pool *pgxpool.Pool
	// readTx runs a list query and its count in one read-only snapshot, so the total
	// matches the page.
	readTx func(ctx context.Context, fn func(*sqlcgen.Queries) error) error
}

var _ domain.Store = (*Store)(nil)

// NewStore returns a Store that uses pool.
func NewStore(pool *pgxpool.Pool) *Store {
	q := sqlcgen.New(pool)
	return &Store{pool: pool, readTx: func(ctx context.Context, fn func(*sqlcgen.Queries) error) error {
		return pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly},
			func(tx pgx.Tx) error { return fn(q.WithTx(tx)) })
	}}
}

// q returns the queries on the context's transaction, or on the pool.
func (s *Store) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Conn(ctx, s.pool))
}

// UserVisible is false for a value that is not a UUID.
func (s *Store) UserVisible(ctx context.Context, userID string) (bool, error) {
	id, ok := parseUUID(userID)
	if !ok {
		return false, nil
	}
	v, err := s.q(ctx).UserVisible(ctx, id)
	if err != nil {
		return false, fmt.Errorf("check user: %w", err)
	}
	return v, nil
}

// FindPerson returns domain.ErrNotFound when no user has the ID.
func (s *Store) FindPerson(ctx context.Context, userID string) (domain.Person, error) {
	id, ok := parseUUID(userID)
	if !ok {
		return domain.Person{}, domain.ErrNotFound
	}
	row, err := s.q(ctx).FindPerson(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Person{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Person{}, fmt.Errorf("find person: %w", err)
	}
	return person(row.ID, row.FullName, row.Headline, row.Institution, row.PhotoFileID), nil
}

// CreateRequest relies on connections_live_pair_uq: ON CONFLICT DO NOTHING returns no row
// when the pair already has a live link, also one committed while this insert waited.
// A receiver ID that is not a UUID or names no user is the caller's mistake: the usecase
// checks the receiver first.
func (s *Store) CreateRequest(ctx context.Context, requesterID, receiverID string) (domain.Request, bool, error) {
	from, ok1 := parseUUID(requesterID)
	to, ok2 := parseUUID(receiverID)
	if !ok1 || !ok2 {
		return domain.Request{}, false, fmt.Errorf("create connection request: %w", domain.ErrNotFound)
	}
	row, err := s.q(ctx).CreateRequest(ctx, sqlcgen.CreateRequestParams{RequesterID: from, ReceiverID: to})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Request{}, false, nil
	}
	if err != nil {
		return domain.Request{}, false, fmt.Errorf("create connection request: %w", err)
	}
	return domain.Request{ID: row.ID.String(), RequesterID: requesterID, ReceiverID: receiverID,
		CreatedAt: row.CreatedAt.Time}, true, nil
}

// FindLiveLink finds nothing for a value that is not a UUID.
func (s *Store) FindLiveLink(ctx context.Context, a, b string) (domain.LiveLink, bool, error) {
	ua, ok1 := parseUUID(a)
	ub, ok2 := parseUUID(b)
	if !ok1 || !ok2 {
		return domain.LiveLink{}, false, nil
	}
	row, err := s.q(ctx).FindLiveLink(ctx, sqlcgen.FindLiveLinkParams{A: ua, B: ub})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LiveLink{}, false, nil
	}
	if err != nil {
		return domain.LiveLink{}, false, fmt.Errorf("find connection: %w", err)
	}
	return domain.LiveLink{ID: row.ID.String(), RequesterID: row.RequesterID.String(),
		ReceiverID: row.ReceiverID.String(), Accepted: row.Status == sqlcgen.ConnectionStatusAccepted}, true, nil
}

// ListLiveLinks runs no query when no ID is a UUID other than the viewer's.
func (s *Store) ListLiveLinks(ctx context.Context, viewerID string, otherIDs []string) ([]domain.LiveLink, error) {
	viewer, ok := parseUUID(viewerID)
	if !ok {
		return nil, nil
	}
	ids := make([]pgtype.UUID, 0, len(otherIDs))
	for _, o := range otherIDs {
		if id, ok := parseUUID(o); ok && id != viewer {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.q(ctx).ListLiveLinks(ctx, sqlcgen.ListLiveLinksParams{OtherIds: ids, ViewerID: viewer})
	if err != nil {
		return nil, fmt.Errorf("list connection links: %w", err)
	}
	links := make([]domain.LiveLink, 0, len(rows))
	for _, r := range rows {
		links = append(links, domain.LiveLink{ID: r.ID.String(), RequesterID: r.RequesterID.String(),
			ReceiverID: r.ReceiverID.String(), Accepted: r.Status == sqlcgen.ConnectionStatusAccepted})
	}
	return links, nil
}

// AcceptRequest returns domain.ErrNotFound unless receiverID received the request, it is
// pending and its sender is visible.
func (s *Store) AcceptRequest(ctx context.Context, requestID, receiverID string) (domain.Request, time.Time, error) {
	row, err := s.answer(ctx, requestID, receiverID, sqlcgen.ConnectionStatusAccepted)
	if err != nil {
		return domain.Request{}, time.Time{}, err
	}
	return domain.Request{ID: row.ID.String(), RequesterID: row.RequesterID.String(),
		ReceiverID: row.ReceiverID.String(), CreatedAt: row.CreatedAt.Time}, row.RespondedAt.Time, nil
}

// RejectRequest keeps the row as history; it no longer blocks a new request (FR-7.6).
func (s *Store) RejectRequest(ctx context.Context, requestID, receiverID string) error {
	_, err := s.answer(ctx, requestID, receiverID, sqlcgen.ConnectionStatusRejected)
	return err
}

func (s *Store) answer(ctx context.Context, requestID, receiverID string, status sqlcgen.ConnectionStatus) (sqlcgen.AnswerRequestRow, error) {
	id, ok1 := parseUUID(requestID)
	to, ok2 := parseUUID(receiverID)
	if !ok1 || !ok2 {
		return sqlcgen.AnswerRequestRow{}, domain.ErrNotFound
	}
	row, err := s.q(ctx).AnswerRequest(ctx, sqlcgen.AnswerRequestParams{Status: status, ID: id, ReceiverID: to})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlcgen.AnswerRequestRow{}, domain.ErrNotFound
	}
	if err != nil {
		return sqlcgen.AnswerRequestRow{}, fmt.Errorf("answer connection request: %w", err)
	}
	return row, nil
}

// CancelRequest returns domain.ErrNotFound unless requesterID sent the request and it is
// pending.
func (s *Store) CancelRequest(ctx context.Context, requestID, requesterID string) error {
	id, ok1 := parseUUID(requestID)
	from, ok2 := parseUUID(requesterID)
	if !ok1 || !ok2 {
		return domain.ErrNotFound
	}
	n, err := s.q(ctx).CancelRequest(ctx, sqlcgen.CancelRequestParams{ID: id, RequesterID: from})
	if err != nil {
		return fmt.Errorf("cancel connection request: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// RemoveConnection returns domain.ErrNotFound when the two are not connected.
func (s *Store) RemoveConnection(ctx context.Context, userID, otherID string) error {
	a, ok1 := parseUUID(userID)
	b, ok2 := parseUUID(otherID)
	if !ok1 || !ok2 {
		return domain.ErrNotFound
	}
	n, err := s.q(ctx).RemoveConnection(ctx, sqlcgen.RemoveConnectionParams{UserID: a, OtherID: b})
	if err != nil {
		return fmt.Errorf("remove connection: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ListConnections returns no items for a value that is not a UUID.
func (s *Store) ListConnections(ctx context.Context, userID string, limit, offset int) ([]domain.Connection, int, error) {
	id, ok := parseUUID(userID)
	if !ok {
		return []domain.Connection{}, 0, nil
	}
	items := []domain.Connection{}
	var total int64
	err := s.readTx(ctx, func(q *sqlcgen.Queries) error {
		rows, err := q.ListConnections(ctx, sqlcgen.ListConnectionsParams{UserID: id, Lim: clamp(limit), Off: clamp(offset)})
		if err != nil {
			return err
		}
		for _, r := range rows {
			items = append(items, domain.Connection{ConnectedAt: r.RespondedAt.Time,
				Other: person(r.OtherID, r.FullName, r.Headline, r.Institution, r.PhotoFileID)})
		}
		total, err = q.CountConnections(ctx, id)
		return err
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list connections: %w", err)
	}
	return items, int(total), nil
}

// ListReceived returns no items for a value that is not a UUID.
func (s *Store) ListReceived(ctx context.Context, userID string, limit, offset int) ([]domain.PendingRequest, int, error) {
	id, ok := parseUUID(userID)
	if !ok {
		return []domain.PendingRequest{}, 0, nil
	}
	items := []domain.PendingRequest{}
	var total int64
	err := s.readTx(ctx, func(q *sqlcgen.Queries) error {
		rows, err := q.ListReceived(ctx, sqlcgen.ListReceivedParams{UserID: id, Lim: clamp(limit), Off: clamp(offset)})
		if err != nil {
			return err
		}
		for _, r := range rows {
			items = append(items, domain.PendingRequest{ID: r.ID.String(), CreatedAt: r.CreatedAt.Time,
				Other: person(r.OtherID, r.FullName, r.Headline, r.Institution, r.PhotoFileID)})
		}
		total, err = q.CountReceived(ctx, id)
		return err
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list received connection requests: %w", err)
	}
	return items, int(total), nil
}

// ListSent returns no items for a value that is not a UUID.
func (s *Store) ListSent(ctx context.Context, userID string, limit, offset int) ([]domain.PendingRequest, int, error) {
	id, ok := parseUUID(userID)
	if !ok {
		return []domain.PendingRequest{}, 0, nil
	}
	items := []domain.PendingRequest{}
	var total int64
	err := s.readTx(ctx, func(q *sqlcgen.Queries) error {
		rows, err := q.ListSent(ctx, sqlcgen.ListSentParams{UserID: id, Lim: clamp(limit), Off: clamp(offset)})
		if err != nil {
			return err
		}
		for _, r := range rows {
			items = append(items, domain.PendingRequest{ID: r.ID.String(), CreatedAt: r.CreatedAt.Time,
				Other: person(r.OtherID, r.FullName, r.Headline, r.Institution, r.PhotoFileID)})
		}
		total, err = q.CountSent(ctx, id)
		return err
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list sent connection requests: %w", err)
	}
	return items, int(total), nil
}

// CountConnections is 0 for a value that is not a UUID.
func (s *Store) CountConnections(ctx context.Context, userID string) (int, error) {
	id, ok := parseUUID(userID)
	if !ok {
		return 0, nil
	}
	n, err := s.q(ctx).CountConnections(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("count connections: %w", err)
	}
	return int(n), nil
}

func person(id pgtype.UUID, fullName string, headline, institution pgtype.Text, photo pgtype.UUID) domain.Person {
	return domain.Person{UserID: id.String(), FullName: fullName, Headline: textPtr(headline),
		Institution: textPtr(institution), PhotoFileID: uuidPtr(photo)}
}

// clamp keeps a page size or offset inside int32; a page that far out is empty anyway.
func clamp(offset int) int32 {
	return int32(min(offset, math.MaxInt32)) //nolint:gosec // bounded by the min
}

func parseUUID(s string) (pgtype.UUID, bool) {
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		return id, false
	}
	return id, true
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func uuidPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := u.String()
	return &s
}
