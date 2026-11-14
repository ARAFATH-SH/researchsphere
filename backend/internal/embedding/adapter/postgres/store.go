// Package postgres implements the embedding domain's Store with the queries that sqlc
// generates from queries.sql. Every query runs in the transaction the context carries, if any
// (platform/postgres.Transactor).
package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// actionRebuild is ai_admin_actions.action for a rebuild.
const actionRebuild = "embeddings_rebuild"

// Store reads and writes the embedding tables through the shared pool.
type Store struct {
	pool *pgxpool.Pool
	tx   postgres.Transactor
}

var _ domain.Store = (*Store)(nil)

// NewStore returns a Store that uses pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, tx: postgres.NewTransactor(pool)}
}

func (s *Store) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Conn(ctx, s.pool))
}

// ClaimJobs leases up to limit due jobs.
func (s *Store) ClaimJobs(ctx context.Context, limit int, lease time.Duration) ([]domain.Job, error) {
	lim, err := int32Of(limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.q(ctx).ClaimJobs(ctx, sqlcgen.ClaimJobsParams{LeaseSeconds: lease.Seconds(), Lim: lim})
	if err != nil {
		return nil, fmt.Errorf("claim embedding jobs: %w", err)
	}
	jobs := make([]domain.Job, len(rows))
	for i, r := range rows {
		jobs[i] = domain.Job{
			OwnerType: domain.OwnerType(r.OwnerType), OwnerID: r.OwnerID.String(),
			Generation: r.Generation, Attempts: int(r.Attempts), Force: r.Force,
		}
	}
	return jobs, nil
}

// FinishJob deletes the job, or only ends its lease when a newer request replaced it.
func (s *Store) FinishJob(ctx context.Context, job domain.Job) error {
	id, err := ownerUUID(job.OwnerID)
	if err != nil {
		return err
	}
	n, err := s.q(ctx).FinishJob(ctx, sqlcgen.FinishJobParams{OwnerType: string(job.OwnerType), OwnerID: id, Generation: job.Generation})
	if err != nil {
		return fmt.Errorf("finish embedding job: %w", err)
	}
	if n == 0 {
		return s.ReleaseJob(ctx, job)
	}
	return nil
}

// ReleaseJob ends the job's lease.
func (s *Store) ReleaseJob(ctx context.Context, job domain.Job) error {
	id, err := ownerUUID(job.OwnerID)
	if err != nil {
		return err
	}
	if err := s.q(ctx).ReleaseJob(ctx, sqlcgen.ReleaseJobParams{OwnerType: string(job.OwnerType), OwnerID: id}); err != nil {
		return fmt.Errorf("release embedding job: %w", err)
	}
	return nil
}

// RetryJob counts a failed attempt, unless a newer request replaced the job; then it only
// ends the lease.
func (s *Store) RetryJob(ctx context.Context, job domain.Job, lastError string, next time.Time, failed bool) error {
	id, err := ownerUUID(job.OwnerID)
	if err != nil {
		return err
	}
	if err := s.q(ctx).RetryJob(ctx, sqlcgen.RetryJobParams{
		NextTryAt: pgtype.Timestamptz{Time: next, Valid: true}, LastError: pgtype.Text{String: lastError, Valid: true},
		Failed: failed, OwnerType: string(job.OwnerType), OwnerID: id, Generation: job.Generation,
	}); err != nil {
		return fmt.Errorf("retry embedding job: %w", err)
	}
	return s.ReleaseJob(ctx, job)
}

// ProfileSource reads the public fields the profile's text is made from.
func (s *Store) ProfileSource(ctx context.Context, userID string) (domain.ProfileSource, error) {
	id, ok := parseUUID(userID)
	if !ok {
		return domain.ProfileSource{}, domain.ErrNotFound
	}
	q := s.q(ctx)
	row, err := q.ProfileSourceFields(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProfileSource{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ProfileSource{}, fmt.Errorf("read profile source: %w", err)
	}
	src := domain.ProfileSource{Eligible: row.Eligible, Headline: row.Headline.String, Bio: row.Bio.String}
	if src.Interests, err = q.ProfileInterestNames(ctx, id); err != nil {
		return domain.ProfileSource{}, fmt.Errorf("read profile interests: %w", err)
	}
	if src.Skills, err = q.ProfileSkillNames(ctx, id); err != nil {
		return domain.ProfileSource{}, fmt.Errorf("read profile skills: %w", err)
	}
	pubs, err := q.ProfilePublicationTexts(ctx, id)
	if err != nil {
		return domain.ProfileSource{}, fmt.Errorf("read profile publications: %w", err)
	}
	src.Publications = make([]domain.PublicationText, len(pubs))
	for i, p := range pubs {
		src.Publications[i] = domain.PublicationText{Title: p.Title, Abstract: p.Abstract.String}
	}
	return src, nil
}

// StoredEmbedding returns the owner's model and hash.
func (s *Store) StoredEmbedding(ctx context.Context, owner domain.OwnerType, ownerID string) (domain.Stored, error) {
	if owner != domain.OwnerProfile {
		return domain.Stored{}, unknownOwner(owner)
	}
	id, ok := parseUUID(ownerID)
	if !ok {
		return domain.Stored{}, domain.ErrNotFound
	}
	row, err := s.q(ctx).StoredProfileEmbedding(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Stored{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Stored{}, fmt.Errorf("read stored embedding: %w", err)
	}
	return domain.Stored{Model: row.Model, ContentHash: row.ContentHash}, nil
}

// SaveEmbedding stores the embedding while the owner may have one.
func (s *Store) SaveEmbedding(ctx context.Context, e domain.Embedding) (bool, error) {
	if e.OwnerType != domain.OwnerProfile {
		return false, unknownOwner(e.OwnerType)
	}
	id, err := ownerUUID(e.OwnerID)
	if err != nil {
		return false, err
	}
	if e.ChunkCount < 1 || e.ChunkCount > domain.MaxChunks {
		return false, fmt.Errorf("save embedding: %d chunks", e.ChunkCount)
	}
	n, err := s.q(ctx).SaveProfileEmbedding(ctx, sqlcgen.SaveProfileEmbeddingParams{
		Model: e.Model, ContentHash: e.ContentHash, Embedding: vectorText(e.Vector), ChunkCount: int16(e.ChunkCount), UserID: id,
	})
	if err != nil {
		return false, fmt.Errorf("save embedding: %w", err)
	}
	return n > 0, nil
}

// DeleteEmbedding removes the owner's embedding.
func (s *Store) DeleteEmbedding(ctx context.Context, owner domain.OwnerType, ownerID string) error {
	if owner != domain.OwnerProfile {
		return unknownOwner(owner)
	}
	id, ok := parseUUID(ownerID)
	if !ok {
		return nil
	}
	if err := s.q(ctx).DeleteProfileEmbedding(ctx, id); err != nil {
		return fmt.Errorf("delete embedding: %w", err)
	}
	return nil
}

// QueueStale queues up to limit owners of another model.
func (s *Store) QueueStale(ctx context.Context, model string, limit int) (int, error) {
	lim, err := int32Of(limit)
	if err != nil {
		return 0, err
	}
	n, err := s.q(ctx).QueueStaleProfiles(ctx, sqlcgen.QueueStaleProfilesParams{Model: model, Lim: lim})
	if err != nil {
		return 0, fmt.Errorf("queue stale embeddings: %w", err)
	}
	return int(n), nil
}

// QueueAll queues every eligible owner at low priority.
func (s *Store) QueueAll(ctx context.Context, force bool) (int, error) {
	n, err := s.q(ctx).QueueAllProfiles(ctx, force)
	if err != nil {
		return 0, fmt.Errorf("queue all embeddings: %w", err)
	}
	return int(n), nil
}

// Rebuild queues every eligible owner, forced, and records the action, in one transaction.
func (s *Store) Rebuild(ctx context.Context, adminID string) (int, error) {
	admin, ok := parseUUID(adminID)
	if !ok {
		return 0, fmt.Errorf("rebuild: admin %q is not a UUID", adminID)
	}
	var n int
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		if n, err = s.QueueAll(ctx, true); err != nil {
			return err
		}
		queued, err := int32Of(n)
		if err != nil {
			return err
		}
		if err := s.q(ctx).InsertAIAdminAction(ctx, sqlcgen.InsertAIAdminActionParams{AdminID: admin, Action: actionRebuild, Queued: queued}); err != nil {
			return fmt.Errorf("record rebuild: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// Stats counts each owner type's embeddings and jobs.
func (s *Store) Stats(ctx context.Context, model string) ([]domain.OwnerStats, error) {
	q := s.q(ctx)
	prof, err := q.ProfileEmbeddingStats(ctx, model)
	if err != nil {
		return nil, fmt.Errorf("embedding stats: %w", err)
	}
	jobs, err := q.JobStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("job stats: %w", err)
	}
	byType := map[domain.OwnerType]domain.OwnerStats{
		domain.OwnerProfile: {OwnerType: domain.OwnerProfile, Total: int(prof.Total), CurrentModel: int(prof.CurrentModel)},
	}
	for _, j := range jobs {
		t := domain.OwnerType(j.OwnerType)
		st := byType[t]
		st.OwnerType, st.Queued, st.Failed = t, int(j.Queued), int(j.Failed)
		byType[t] = st
	}
	out := make([]domain.OwnerStats, 0, len(domain.OwnerTypes))
	for _, t := range domain.OwnerTypes {
		out = append(out, byType[t])
	}
	return out, nil
}

// vectorText is the pgvector text form, such as [0.1,-0.2].
func vectorText(v []float32) string {
	var b strings.Builder
	b.Grow(len(v) * 12)
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

func unknownOwner(t domain.OwnerType) error {
	return fmt.Errorf("unknown owner type %q", t)
}

func ownerUUID(s string) (pgtype.UUID, error) {
	id, ok := parseUUID(s)
	if !ok {
		return id, fmt.Errorf("owner %q is not a UUID", s)
	}
	return id, nil
}

func parseUUID(s string) (pgtype.UUID, bool) {
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		return id, false
	}
	return id, true
}

func int32Of(n int) (int32, error) {
	if n < 0 || n > math.MaxInt32 {
		return 0, fmt.Errorf("value %d does not fit a query parameter", n)
	}
	return int32(n), nil
}
