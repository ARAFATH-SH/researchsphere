package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

var _ domain.DigestStore = (*Store)(nil)

// Settings returns the defaults for a user without a settings row.
func (s *Store) Settings(ctx context.Context, userID string) (domain.Settings, error) {
	id, ok := parseUUID(userID)
	if !ok {
		return domain.DefaultSettings, nil
	}
	on, err := s.q(ctx).GetUserSettings(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultSettings, nil
	}
	if err != nil {
		return domain.Settings{}, fmt.Errorf("get settings: %w", err)
	}
	return domain.Settings{UnreadMessageDigest: on}, nil
}

// UpdateSettings creates the row on the first change.
func (s *Store) UpdateSettings(ctx context.Context, userID string, u domain.SettingsUpdate) (domain.Settings, error) {
	id, ok := parseUUID(userID)
	if !ok {
		return domain.Settings{}, fmt.Errorf("update settings: user ID is not a UUID")
	}
	arg := sqlcgen.UpsertUserSettingsParams{UserID: id}
	if u.UnreadMessageDigest != nil {
		arg.UnreadMessageDigest = pgtype.Bool{Bool: *u.UnreadMessageDigest, Valid: true}
	}
	on, err := s.q(ctx).UpsertUserSettings(ctx, arg)
	if err != nil {
		return domain.Settings{}, fmt.Errorf("update settings: %w", err)
	}
	return domain.Settings{UnreadMessageDigest: on}, nil
}

// TurnOffDigest does nothing for an ID that is not a UUID or a user that does not exist.
func (s *Store) TurnOffDigest(ctx context.Context, userID string) error {
	id, ok := parseUUID(userID)
	if !ok {
		return nil
	}
	if err := s.q(ctx).TurnOffDigest(ctx, id); err != nil {
		return fmt.Errorf("turn off digest: %w", err)
	}
	return nil
}

// DigestCandidates reads without locks; ClaimDigest checks each user again.
func (s *Store) DigestCandidates(ctx context.Context, cutoff time.Time, limit int) ([]string, error) {
	rows, err := s.q(ctx).ListDigestCandidates(ctx, sqlcgen.ListDigestCandidatesParams{
		Cutoff: timestamptz(cutoff), Lim: int32(min(limit, 1000)), //nolint:gosec // capped
	})
	if err != nil {
		return nil, fmt.Errorf("list digest candidates: %w", err)
	}
	return uuidStrings(rows), nil
}

// PrepareDigests creates the missing state rows in one statement.
func (s *Store) PrepareDigests(ctx context.Context, userIDs []string) error {
	if err := s.q(ctx).EnsureDigestStates(ctx, parseUUIDs(userIDs)); err != nil {
		return fmt.Errorf("create digest states: %w", err)
	}
	return nil
}

// ClaimDigest locks the user's state row unless another worker holds it; it never waits.
func (s *Store) ClaimDigest(ctx context.Context, userID string, cutoff time.Time) (domain.DigestRecipient, bool, error) {
	id, ok := parseUUID(userID)
	if !ok {
		return domain.DigestRecipient{}, false, nil
	}
	r, err := s.q(ctx).ClaimDigest(ctx, sqlcgen.ClaimDigestParams{UserID: id, Cutoff: timestamptz(cutoff)})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DigestRecipient{}, false, nil
	}
	if err != nil {
		return domain.DigestRecipient{}, false, fmt.Errorf("claim digest: %w", err)
	}
	rec := domain.DigestRecipient{Email: r.Email, FullName: r.FullName}
	if r.CoveredUntil.Valid {
		rec.CoveredUntil = r.CoveredUntil.Time
	}
	return rec, true, nil
}

// DigestConversations has no lower bound when coveredUntil is zero.
func (s *Store) DigestConversations(ctx context.Context, userID string, coveredUntil, cutoff time.Time) ([]domain.DigestConversation, error) {
	id, ok := parseUUID(userID)
	if !ok {
		return nil, nil
	}
	arg := sqlcgen.DigestConversationsParams{UserID: id, Cutoff: timestamptz(cutoff)}
	if !coveredUntil.IsZero() {
		arg.CoveredUntil = timestamptz(coveredUntil)
	}
	rows, err := s.q(ctx).DigestConversations(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("digest conversations: %w", err)
	}
	out := make([]domain.DigestConversation, len(rows))
	for i, r := range rows {
		out[i] = domain.DigestConversation{Name: r.Name, Group: r.IsGroup, Unread: int(r.Unread)}
	}
	return out, nil
}

// PendingRequests is the requests count of the unread badge (FR-16.3).
func (s *Store) PendingRequests(ctx context.Context, userID string) (int, error) {
	id, ok := parseUUID(userID)
	if !ok {
		return 0, nil
	}
	n, err := s.q(ctx).CountPendingRequests(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("count pending requests: %w", err)
	}
	return int(n), nil
}

// RecordDigest needs the row ClaimDigest created.
func (s *Store) RecordDigest(ctx context.Context, r domain.DigestRecord) error {
	id, ok := parseUUID(r.UserID)
	if !ok {
		return fmt.Errorf("record digest: user ID is not a UUID")
	}
	err := s.q(ctx).RecordDigest(ctx, sqlcgen.RecordDigestParams{
		UserID: id, SentAt: timestamptz(r.SentAt), CoveredUntil: timestamptz(r.CoveredUntil),
		UnreadCount: int32(r.Unread), ConversationCount: int32(r.Conversations), RequestCount: int32(r.Requests), //nolint:gosec // small counts
	})
	if err != nil {
		return fmt.Errorf("record digest: %w", err)
	}
	return nil
}
