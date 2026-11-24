// Package postgres implements the event domain's Store with the queries sqlc generates from
// queries.sql (migration 000039).
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// Store keeps venues in PostgreSQL. Every method runs on the transaction the context
// carries, if any.
type Store struct {
	pool *pgxpool.Pool
}

var _ domain.Store = (*Store)(nil)

// NewStore returns a Store that uses pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Conn(ctx, s.pool))
}

// Fields returns the fixed list of research fields, in order.
func (s *Store) Fields(ctx context.Context) ([]domain.Field, error) {
	rows, err := s.q(ctx).ResearchFields(ctx)
	if err != nil {
		return nil, fmt.Errorf("read research fields: %w", err)
	}
	out := make([]domain.Field, len(rows))
	for i, r := range rows {
		out[i] = domain.Field{Key: r.Key, Label: r.Label}
	}
	return out, nil
}

// List returns one page of live venues with the total and the facets.
func (s *Store) List(ctx context.Context, q domain.Query) (domain.Page, error) {
	lim, off := int32Of(q.Limit), int32Of(q.Offset)
	if lim < 0 || off < 0 {
		return domain.Page{}, fmt.Errorf("list venues: limit %d, offset %d", q.Limit, q.Offset)
	}
	types := make([]string, len(q.Types))
	for i, t := range q.Types {
		types[i] = string(t)
	}
	// An empty, never nil, array: NULL would make cardinality() NULL and match nothing.
	fields := append([]string{}, q.Fields...)
	now := ts(q.Now)
	from, to := optTS(q.From), optTS(q.To)
	like := likePattern(q.Q)
	db := s.q(ctx)

	rows, err := db.ListVenues(ctx, sqlcgen.ListVenuesParams{
		Rolling: q.Rolling, IncludePast: q.IncludePast, Now: now, Types: types, FieldKeys: fields, DeadlineFrom: from, DeadlineTo: to,
		Q: q.Q, QLike: like, Lim: lim, Off: off,
	})
	if err != nil {
		return domain.Page{}, fmt.Errorf("list venues: %w", err)
	}
	total, err := db.CountVenues(ctx, sqlcgen.CountVenuesParams{
		Rolling: q.Rolling, IncludePast: q.IncludePast, Now: now, Types: types, FieldKeys: fields, DeadlineFrom: from, DeadlineTo: to,
		Q: q.Q, QLike: like,
	})
	if err != nil {
		return domain.Page{}, fmt.Errorf("count venues: %w", err)
	}
	typeRows, err := db.VenueTypeCounts(ctx, sqlcgen.VenueTypeCountsParams{
		Rolling: q.Rolling, IncludePast: q.IncludePast, Now: now, FieldKeys: fields, DeadlineFrom: from, DeadlineTo: to, Q: q.Q, QLike: like,
	})
	if err != nil {
		return domain.Page{}, fmt.Errorf("count venue types: %w", err)
	}
	fieldRows, err := db.VenueFieldCounts(ctx, sqlcgen.VenueFieldCountsParams{
		Rolling: q.Rolling, IncludePast: q.IncludePast, Now: now, Types: types, DeadlineFrom: from, DeadlineTo: to, Q: q.Q, QLike: like,
	})
	if err != nil {
		return domain.Page{}, fmt.Errorf("count venue fields: %w", err)
	}
	all, err := s.Fields(ctx)
	if err != nil {
		return domain.Page{}, err
	}

	page := domain.Page{Total: int(total)}
	if page.Items, err = s.withFields(ctx, rows); err != nil {
		return domain.Page{}, err
	}
	byType := map[string]int{}
	for _, r := range typeRows {
		byType[r.Type] = int(r.N)
	}
	for _, t := range domain.VenueTypes {
		page.TypeCounts = append(page.TypeCounts, domain.TypeCount{Type: t, Count: byType[string(t)]})
	}
	byField := map[string]int{}
	for _, r := range fieldRows {
		byField[r.FieldKey] = int(r.N)
	}
	for _, f := range all {
		page.FieldCounts = append(page.FieldCounts, domain.FieldCount{Field: f, Count: byField[f.Key]})
	}
	return page, nil
}

// Get returns a live venue.
func (s *Store) Get(ctx context.Context, id string) (domain.Venue, error) {
	uid, ok := parseUUID(id)
	if !ok {
		return domain.Venue{}, domain.ErrNotFound
	}
	return s.one(ctx, func(q *sqlcgen.Queries) (sqlcgen.Venue, error) { return q.GetVenue(ctx, uid) })
}

// GetForUpdate returns a live venue, locked until the transaction ends.
func (s *Store) GetForUpdate(ctx context.Context, id string) (domain.Venue, error) {
	uid, ok := parseUUID(id)
	if !ok {
		return domain.Venue{}, domain.ErrNotFound
	}
	return s.one(ctx, func(q *sqlcgen.Queries) (sqlcgen.Venue, error) { return q.GetVenueForUpdate(ctx, uid) })
}

func (s *Store) one(ctx context.Context, get func(*sqlcgen.Queries) (sqlcgen.Venue, error)) (domain.Venue, error) {
	row, err := get(s.q(ctx))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Venue{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Venue{}, fmt.Errorf("read venue: %w", err)
	}
	vs, err := s.withFields(ctx, []sqlcgen.Venue{row})
	if err != nil {
		return domain.Venue{}, err
	}
	return vs[0], nil
}

// Create stores a new venue with its fields.
func (s *Store) Create(ctx context.Context, v domain.Venue, createdBy string) (string, error) {
	by, _ := parseUUID(createdBy)
	id, err := s.q(ctx).InsertVenue(ctx, sqlcgen.InsertVenueParams{
		Name: v.Name, ShortName: text(v.ShortName), Type: string(v.Type), Rolling: v.Rolling, Attendance: text(string(v.Attendance)),
		City: text(v.City), Country: text(v.Country), StartsOn: date(v.StartsOn), EndsOn: date(v.EndsOn),
		DeadlineTz: text(v.TimeZone), SubmissionDeadline: optTS(v.SubmissionDeadline), AbstractDeadline: optTS(v.AbstractDeadline),
		NotificationAt: optTS(v.NotificationAt), OfficialUrl: v.OfficialURL, LinkKind: string(v.Link()), Scope: v.Scope,
		Source: string(v.Source), SourceKey: text(v.SourceKey), CreatedBy: by,
	})
	if err != nil {
		return "", fmt.Errorf("insert venue: %w", err)
	}
	if err := s.q(ctx).AddVenueFields(ctx, sqlcgen.AddVenueFieldsParams{VenueID: id, Keys: v.FieldKeys()}); err != nil {
		return "", fmt.Errorf("insert venue fields: %w", err)
	}
	return uuidString(id), nil
}

// Update stores v's values over the venue v.ID. Only fields that are no longer wanted are
// deleted and only new ones added, so an unchanged list queues no embedding.
func (s *Store) Update(ctx context.Context, v domain.Venue) error {
	id, ok := parseUUID(v.ID)
	if !ok {
		return domain.ErrNotFound
	}
	q := s.q(ctx)
	if err := q.UpdateVenue(ctx, sqlcgen.UpdateVenueParams{
		ID: id, Name: v.Name, ShortName: text(v.ShortName), Type: string(v.Type), Rolling: v.Rolling,
		Attendance: text(string(v.Attendance)), City: text(v.City), Country: text(v.Country), StartsOn: date(v.StartsOn),
		EndsOn: date(v.EndsOn), DeadlineTz: text(v.TimeZone), SubmissionDeadline: optTS(v.SubmissionDeadline),
		AbstractDeadline: optTS(v.AbstractDeadline), NotificationAt: optTS(v.NotificationAt), OfficialUrl: v.OfficialURL,
		LinkKind: string(v.Link()), Scope: v.Scope,
	}); err != nil {
		return fmt.Errorf("update venue: %w", err)
	}
	keys := v.FieldKeys()
	if err := q.DeleteOtherVenueFields(ctx, sqlcgen.DeleteOtherVenueFieldsParams{VenueID: id, Keys: keys}); err != nil {
		return fmt.Errorf("delete venue fields: %w", err)
	}
	if err := q.AddVenueFields(ctx, sqlcgen.AddVenueFieldsParams{VenueID: id, Keys: keys}); err != nil {
		return fmt.Errorf("add venue fields: %w", err)
	}
	return nil
}

// Lock marks a synced venue as edited by an administrator; the first time stays.
func (s *Store) Lock(ctx context.Context, id string, at time.Time) error {
	uid, ok := parseUUID(id)
	if !ok {
		return domain.ErrNotFound
	}
	if err := s.q(ctx).LockVenue(ctx, sqlcgen.LockVenueParams{ID: uid, At: ts(at)}); err != nil {
		return fmt.Errorf("lock venue: %w", err)
	}
	return nil
}

// Remove marks a live venue removed.
func (s *Store) Remove(ctx context.Context, id, by, reason string, at time.Time) error {
	uid, ok := parseUUID(id)
	if !ok {
		return domain.ErrNotFound
	}
	byID, _ := parseUUID(by)
	n, err := s.q(ctx).RemoveVenue(ctx, sqlcgen.RemoveVenueParams{ID: uid, RemovedAt: ts(at), RemovedBy: byID, Reason: text(reason)})
	if err != nil {
		return fmt.Errorf("remove venue: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// Record writes an audit row.
func (s *Store) Record(ctx context.Context, a domain.Action) error {
	venue, ok := parseUUID(a.VenueID)
	if !ok {
		return fmt.Errorf("record venue action: venue %q is not a UUID", a.VenueID)
	}
	admin, _ := parseUUID(a.AdminID)
	imp, _ := parseUUID(a.ImportID)
	changed := append([]string{}, a.ChangedFields...)
	actor := a.Actor
	if actor == "" {
		actor = domain.ActorAdmin
	}
	if err := s.q(ctx).InsertVenueAction(ctx, sqlcgen.InsertVenueActionParams{
		VenueID: venue, Actor: string(actor), AdminID: admin, AdminName: a.AdminName, Action: string(a.Kind), Reason: text(a.Reason),
		ChangedFields: changed, ImportID: imp,
	}); err != nil {
		return fmt.Errorf("record venue action: %w", err)
	}
	return nil
}

// AdminName returns the administrator's full name, or email address without a profile.
func (s *Store) AdminName(ctx context.Context, userID string) (string, error) {
	id, ok := parseUUID(userID)
	if !ok {
		return "", fmt.Errorf("admin name: %q is not a UUID", userID)
	}
	name, err := s.q(ctx).AdminName(ctx, id)
	if err != nil {
		return "", fmt.Errorf("admin name: %w", err)
	}
	return name, nil
}

// LockImports makes imports take turns until the transaction ends.
func (s *Store) LockImports(ctx context.Context) error {
	if err := s.q(ctx).LockVenueImports(ctx); err != nil {
		return fmt.Errorf("lock venue imports: %w", err)
	}
	return nil
}

// ImportedByKeys returns the venues of source with these keys, locked.
func (s *Store) ImportedByKeys(ctx context.Context, source domain.Source, keys []string) (map[string]domain.Imported, error) {
	out := map[string]domain.Imported{}
	if len(keys) == 0 || source == domain.SourceManual {
		return out, nil
	}
	rows, err := s.q(ctx).ImportedVenuesByKeys(ctx, sqlcgen.ImportedVenuesByKeysParams{Source: string(source), Keys: keys})
	if err != nil {
		return nil, fmt.Errorf("read imported venues: %w", err)
	}
	vs, err := s.withFields(ctx, rows)
	if err != nil {
		return nil, err
	}
	for i, v := range vs {
		out[v.SourceKey] = domain.Imported{Venue: v, Removed: rows[i].RemovedAt.Valid}
	}
	return out, nil
}

// LowEmbeddingPriority makes the embeddings the transaction queues low priority.
func (s *Store) LowEmbeddingPriority(ctx context.Context) error {
	if err := s.q(ctx).LowEmbeddingPriority(ctx); err != nil {
		return fmt.Errorf("set embedding priority: %w", err)
	}
	return nil
}

// withFields turns rows into venues with their fields.
func (s *Store) withFields(ctx context.Context, rows []sqlcgen.Venue) ([]domain.Venue, error) {
	out := make([]domain.Venue, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]pgtype.UUID, len(rows))
	at := make(map[string]int, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
		out[i] = venueOf(r)
		at[out[i].ID] = i
	}
	fields, err := s.q(ctx).VenueFieldsOf(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read venue fields: %w", err)
	}
	for _, f := range fields {
		i := at[uuidString(f.VenueID)]
		out[i].Fields = append(out[i].Fields, domain.Field{Key: f.Key, Label: f.Label})
	}
	return out, nil
}

func venueOf(r sqlcgen.Venue) domain.Venue {
	return domain.Venue{
		ID: uuidString(r.ID), Name: r.Name, ShortName: r.ShortName.String, Type: domain.VenueType(r.Type), Rolling: r.Rolling,
		Attendance: domain.Attendance(r.Attendance.String), City: r.City.String, Country: r.Country.String,
		StartsOn: dateOf(r.StartsOn), EndsOn: dateOf(r.EndsOn), TimeZone: r.DeadlineTz.String,
		SubmissionDeadline: timeOf(r.SubmissionDeadline), AbstractDeadline: timeOf(r.AbstractDeadline),
		NotificationAt: timeOf(r.NotificationAt), OfficialURL: r.OfficialUrl, LinkKind: domain.LinkKind(r.LinkKind), Scope: r.Scope,
		Source: domain.Source(r.Source), SourceKey: r.SourceKey.String, LockedAt: timeOf(r.LockedAt),
		CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time,
	}
}

// likePattern is "%q%" with the LIKE wildcards in q escaped; "" for no q.
func likePattern(q string) string {
	if q == "" {
		return ""
	}
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

func parseUUID(v string) (pgtype.UUID, bool) {
	var u pgtype.UUID
	if v == "" {
		return u, false
	}
	if err := u.Scan(v); err != nil {
		return pgtype.UUID{}, false
	}
	return u, true
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func optTS(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()} }

func timeOf(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time.UTC()
}

func date(t time.Time) pgtype.Date { return pgtype.Date{Time: t, Valid: !t.IsZero()} }

func dateOf(d pgtype.Date) time.Time {
	if !d.Valid {
		return time.Time{}
	}
	return time.Date(d.Time.Year(), d.Time.Month(), d.Time.Day(), 0, 0, 0, 0, time.UTC)
}

// int32Of converts a page bound; -1 for a value out of range.
func int32Of(n int) int32 {
	if n < 0 || n > 1<<31-1 {
		return -1
	}
	return int32(n)
}
