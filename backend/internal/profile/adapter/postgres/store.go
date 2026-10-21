// Package postgres implements the profile domain's Store and Tx with the queries that
// sqlc generates from queries.sql. Each tag kind has its own table and queries; the
// methods pick them by kind.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// foreignKeyViolation is the SQLSTATE of an insert that names a user who does not exist.
const foreignKeyViolation = "23503"

// Store reads and writes profiles through the shared pool.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

var (
	_ domain.Store = (*Store)(nil)
	_ domain.Tx    = tx{}
)

// NewStore returns a Store that uses pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: sqlcgen.New(pool)}
}

// RunInTx runs fn in one transaction and commits when it returns nil.
func (s *Store) RunInTx(ctx context.Context, fn func(domain.Tx) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(t pgx.Tx) error {
		return fn(tx{q: s.q.WithTx(t)})
	})
}

// FindAccount returns domain.ErrNotFound when no user has the ID, or the ID is not a UUID.
func (s *Store) FindAccount(ctx context.Context, userID string) (domain.Account, error) {
	id, err := parseUUID(userID)
	if err != nil {
		return domain.Account{}, domain.ErrNotFound
	}
	row, err := s.q.FindAccount(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Account{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Account{}, fmt.Errorf("find account: %w", err)
	}
	a := domain.Account{
		UserID:    row.ID.String(),
		Verified:  row.EmailVerifiedAt.Valid,
		Suspended: row.SuspendedAt.Valid,
	}
	if row.ProfileUserID.Valid {
		a.Profile = &domain.Profile{
			UserID:      a.UserID,
			FullName:    row.FullName.String,
			Headline:    textPtr(row.Headline),
			Bio:         textPtr(row.Bio),
			Institution: textPtr(row.Institution),
			Department:  textPtr(row.Department),
			JobTitle:    textPtr(row.JobTitle),
			Country:     textPtr(row.Country),
			PhotoFileID: uuidPtr(row.PhotoFileID),
			CoverFileID: uuidPtr(row.CoverFileID),
		}
	}
	return a, nil
}

// UpsertProfile returns domain.ErrNotFound when the user does not exist.
func (s *Store) UpsertProfile(ctx context.Context, p domain.Profile) (domain.Profile, error) {
	id, err := parseUUID(p.UserID)
	if err != nil {
		return domain.Profile{}, domain.ErrNotFound
	}
	row, err := s.q.UpsertProfile(ctx, sqlcgen.UpsertProfileParams{
		UserID:      id,
		FullName:    p.FullName,
		Headline:    text(p.Headline),
		Bio:         text(p.Bio),
		Institution: text(p.Institution),
		Department:  text(p.Department),
		JobTitle:    text(p.JobTitle),
		Country:     text(p.Country),
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == foreignKeyViolation {
		return domain.Profile{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Profile{}, fmt.Errorf("upsert profile: %w", err)
	}
	return domain.Profile{
		UserID:      row.UserID.String(),
		FullName:    row.FullName,
		Headline:    textPtr(row.Headline),
		Bio:         textPtr(row.Bio),
		Institution: textPtr(row.Institution),
		Department:  textPtr(row.Department),
		JobTitle:    textPtr(row.JobTitle),
		Country:     textPtr(row.Country),
		PhotoFileID: uuidPtr(row.PhotoFileID),
		CoverFileID: uuidPtr(row.CoverFileID),
	}, nil
}

// ListTags returns the user's tags of kind, sorted by name ignoring letter case.
func (s *Store) ListTags(ctx context.Context, userID string, kind domain.TagKind) ([]domain.Tag, error) {
	id, err := parseUUID(userID)
	if err != nil {
		return nil, err
	}
	var rows []tagRow
	switch kind {
	case domain.KindInterest:
		r, err := s.q.ListUserInterests(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("list interests: %w", err)
		}
		rows = interestRows(r)
	case domain.KindSkill:
		r, err := s.q.ListUserSkills(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("list skills: %w", err)
		}
		rows = skillRows(r)
	default:
		return nil, unknownKind(kind)
	}
	return tags(rows), nil
}

// RemoveTag returns domain.ErrNotFound when the user does not have the tag, or an ID is
// not a UUID.
func (s *Store) RemoveTag(ctx context.Context, userID string, kind domain.TagKind, tagID string) error {
	uid, err := parseUUID(userID)
	if err != nil {
		return domain.ErrNotFound
	}
	tid, err := parseUUID(tagID)
	if err != nil {
		return domain.ErrNotFound
	}
	var n int64
	switch kind {
	case domain.KindInterest:
		n, err = s.q.UnlinkUserInterest(ctx, sqlcgen.UnlinkUserInterestParams{UserID: uid, TagID: tid})
	case domain.KindSkill:
		n, err = s.q.UnlinkUserSkill(ctx, sqlcgen.UnlinkUserSkillParams{UserID: uid, TagID: tid})
	default:
		return unknownKind(kind)
	}
	if err != nil {
		return fmt.Errorf("remove %s: %w", kind, err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// SuggestTags returns at most limit tags of kind whose name contains q (FR-4.3).
func (s *Store) SuggestTags(ctx context.Context, kind domain.TagKind, q string, limit int) ([]domain.Tag, error) {
	n, err := int32Of(limit)
	if err != nil {
		return nil, err
	}
	var rows []tagRow
	switch kind {
	case domain.KindInterest:
		r, err := s.q.SuggestInterests(ctx, sqlcgen.SuggestInterestsParams{Q: q, MaxResults: n})
		if err != nil {
			return nil, fmt.Errorf("suggest interests: %w", err)
		}
		rows = interestRows(r)
	case domain.KindSkill:
		r, err := s.q.SuggestSkills(ctx, sqlcgen.SuggestSkillsParams{Q: q, MaxResults: n})
		if err != nil {
			return nil, fmt.Errorf("suggest skills: %w", err)
		}
		rows = skillRows(r)
	default:
		return nil, unknownKind(kind)
	}
	return tags(rows), nil
}

// tx runs the steps of adding a tag or changing education inside one transaction.
type tx struct {
	q *sqlcgen.Queries
}

// LockUser returns domain.ErrNotFound when the user does not exist.
func (t tx) LockUser(ctx context.Context, userID string) error {
	id, err := parseUUID(userID)
	if err != nil {
		return domain.ErrNotFound
	}
	_, err = t.q.LockUser(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return wrap(err, "lock user")
}

// FindOrCreateTag inserts the name unless a tag already has it ignoring letter case,
// then reads the tag back. The two statements are separate on purpose: the select is a
// new statement, so it sees a row that a concurrent transaction committed while the
// insert waited for it.
func (t tx) FindOrCreateTag(ctx context.Context, kind domain.TagKind, name string) (domain.Tag, error) {
	switch kind {
	case domain.KindInterest:
		if err := t.q.InsertInterestIfMissing(ctx, name); err != nil {
			return domain.Tag{}, fmt.Errorf("insert interest: %w", err)
		}
		r, err := t.q.FindInterestByName(ctx, name)
		if err != nil {
			return domain.Tag{}, fmt.Errorf("find interest: %w", err)
		}
		return domain.Tag{ID: r.ID.String(), Name: r.Name}, nil
	case domain.KindSkill:
		if err := t.q.InsertSkillIfMissing(ctx, name); err != nil {
			return domain.Tag{}, fmt.Errorf("insert skill: %w", err)
		}
		r, err := t.q.FindSkillByName(ctx, name)
		if err != nil {
			return domain.Tag{}, fmt.Errorf("find skill: %w", err)
		}
		return domain.Tag{ID: r.ID.String(), Name: r.Name}, nil
	default:
		return domain.Tag{}, unknownKind(kind)
	}
}

func (t tx) HasTag(ctx context.Context, userID string, kind domain.TagKind, tagID string) (bool, error) {
	uid, tid, err := parseIDs(userID, tagID)
	if err != nil {
		return false, err
	}
	var has bool
	switch kind {
	case domain.KindInterest:
		has, err = t.q.UserHasInterest(ctx, sqlcgen.UserHasInterestParams{UserID: uid, TagID: tid})
	case domain.KindSkill:
		has, err = t.q.UserHasSkill(ctx, sqlcgen.UserHasSkillParams{UserID: uid, TagID: tid})
	default:
		return false, unknownKind(kind)
	}
	return has, wrap(err, "has tag")
}

func (t tx) CountTags(ctx context.Context, userID string, kind domain.TagKind) (int, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return 0, err
	}
	var n int64
	switch kind {
	case domain.KindInterest:
		n, err = t.q.CountUserInterests(ctx, uid)
	case domain.KindSkill:
		n, err = t.q.CountUserSkills(ctx, uid)
	default:
		return 0, unknownKind(kind)
	}
	return int(n), wrap(err, "count tags")
}

func (t tx) LinkTag(ctx context.Context, userID string, kind domain.TagKind, tagID string) error {
	uid, tid, err := parseIDs(userID, tagID)
	if err != nil {
		return err
	}
	switch kind {
	case domain.KindInterest:
		err = t.q.LinkUserInterest(ctx, sqlcgen.LinkUserInterestParams{UserID: uid, TagID: tid})
	case domain.KindSkill:
		err = t.q.LinkUserSkill(ctx, sqlcgen.LinkUserSkillParams{UserID: uid, TagID: tid})
	default:
		return unknownKind(kind)
	}
	return wrap(err, "link tag")
}

// tagRow is a row of either tag table.
type tagRow struct {
	ID   pgtype.UUID
	Name string
}

func interestRows(rs []sqlcgen.ResearchInterest) []tagRow {
	out := make([]tagRow, len(rs))
	for i, r := range rs {
		out[i] = tagRow(r)
	}
	return out
}

func skillRows(rs []sqlcgen.Skill) []tagRow {
	out := make([]tagRow, len(rs))
	for i, r := range rs {
		out[i] = tagRow(r)
	}
	return out
}

func tags(rows []tagRow) []domain.Tag {
	out := make([]domain.Tag, len(rows))
	for i, r := range rows {
		out[i] = domain.Tag{ID: r.ID.String(), Name: r.Name}
	}
	return out
}

func unknownKind(kind domain.TagKind) error {
	return fmt.Errorf("unknown tag kind %q", kind)
}

// wrap adds context to err, and returns nil for nil.
func wrap(err error, what string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", what, err)
}

func parseIDs(userID, tagID string) (pgtype.UUID, pgtype.UUID, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return uid, pgtype.UUID{}, err
	}
	tid, err := parseUUID(tagID)
	return uid, tid, err
}

func int32Of(n int) (int32, error) {
	if n < 0 || n > math.MaxInt32 {
		return 0, fmt.Errorf("%d out of range", n)
	}
	return int32(n), nil
}

func parseUUID(s string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		return id, fmt.Errorf("parse id: %w", err)
	}
	return id, nil
}

func text(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func uuidPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := u.String()
	return &s
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}
