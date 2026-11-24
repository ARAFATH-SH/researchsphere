package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// SyncStore keeps the automatic import's state in PostgreSQL (migration 000040). Every
// method runs on the transaction the context carries, if any.
type SyncStore struct {
	pool *pgxpool.Pool
}

var _ domain.SyncStore = (*SyncStore)(nil)

// NewSyncStore returns a SyncStore that uses pool.
func NewSyncStore(pool *pgxpool.Pool) *SyncStore { return &SyncStore{pool: pool} }

func (s *SyncStore) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Conn(ctx, s.pool))
}

func sourceState(r sqlcgen.VenueSyncSource) domain.SourceState {
	return domain.SourceState{
		Source:          domain.Source(r.Source),
		Repo:            domain.RepoState{CommitSHA: r.CommitSha.String, ETag: r.Etag.String},
		LeaseUntil:      timeOf(r.LeaseUntil),
		LastScheduledAt: timeOf(r.LastScheduledAt),
		LastRunAt:       timeOf(r.LastRunAt),
	}
}

// LockSource returns a source's state, locked until the transaction ends.
func (s *SyncStore) LockSource(ctx context.Context, source domain.Source) (domain.SourceState, error) {
	if err := s.q(ctx).EnsureSyncSource(ctx, string(source)); err != nil {
		return domain.SourceState{}, fmt.Errorf("lock sync source %s: %w", source, err)
	}
	r, err := s.q(ctx).LockSyncSource(ctx, string(source))
	if err != nil {
		return domain.SourceState{}, fmt.Errorf("lock sync source %s: %w", source, err)
	}
	return sourceState(r), nil
}

// SaveSource stores the lease and run times of a source.
func (s *SyncStore) SaveSource(ctx context.Context, st domain.SourceState) error {
	if err := s.q(ctx).SaveSyncSource(ctx, sqlcgen.SaveSyncSourceParams{
		Source: string(st.Source), LeaseUntil: optTS(st.LeaseUntil), LastScheduledAt: optTS(st.LastScheduledAt),
		LastRunAt: optTS(st.LastRunAt),
	}); err != nil {
		return fmt.Errorf("save sync source: %w", err)
	}
	return nil
}

// SaveRepoState stores what a repository feed last read.
func (s *SyncStore) SaveRepoState(ctx context.Context, source domain.Source, st domain.RepoState) error {
	if err := s.q(ctx).SaveSyncRepoState(ctx, sqlcgen.SaveSyncRepoStateParams{
		Source: string(source), CommitSha: text(st.CommitSHA), Etag: text(st.ETag),
	}); err != nil {
		return fmt.Errorf("save repository state: %w", err)
	}
	return nil
}

// Sources returns every source's state.
func (s *SyncStore) Sources(ctx context.Context) ([]domain.SourceState, error) {
	rows, err := s.q(ctx).SyncSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("read sync sources: %w", err)
	}
	out := make([]domain.SourceState, len(rows))
	for i, r := range rows {
		out[i] = sourceState(r)
	}
	return out, nil
}

// Files returns the stored data files of a source.
func (s *SyncStore) Files(ctx context.Context, source domain.Source) ([]domain.RepoFile, error) {
	rows, err := s.q(ctx).SyncFiles(ctx, string(source))
	if err != nil {
		return nil, fmt.Errorf("read sync files: %w", err)
	}
	out := make([]domain.RepoFile, len(rows))
	for i, r := range rows {
		out[i] = domain.RepoFile{Path: r.Path, Content: r.Content}
	}
	return out, nil
}

// SaveFiles stores data files (all of them with full).
func (s *SyncStore) SaveFiles(ctx context.Context, source domain.Source, full bool, files []domain.RepoFile, deleted []string) error {
	q := s.q(ctx)
	if full {
		if err := q.DeleteSyncFiles(ctx, string(source)); err != nil {
			return fmt.Errorf("delete sync files: %w", err)
		}
	} else if len(deleted) > 0 {
		if err := q.DeleteSyncFilePaths(ctx, sqlcgen.DeleteSyncFilePathsParams{Source: string(source), Paths: deleted}); err != nil {
			return fmt.Errorf("delete sync files: %w", err)
		}
	}
	for _, f := range files {
		if err := q.UpsertSyncFile(ctx, sqlcgen.UpsertSyncFileParams{Source: string(source), Path: f.Path, Content: f.Content}); err != nil {
			return fmt.Errorf("save sync file: %w", err)
		}
	}
	return nil
}

// StartRun stores a new run.
func (s *SyncStore) StartRun(ctx context.Context, r domain.SyncRun) (string, error) {
	by, _ := parseUUID(r.RequestedBy)
	id, err := s.q(ctx).InsertSyncRun(ctx, sqlcgen.InsertSyncRunParams{
		Source: string(r.Source), DryRun: r.DryRun, Trigger: string(r.Trigger), RequestedBy: by, StartedAt: ts(r.StartedAt),
	})
	if err != nil {
		return "", fmt.Errorf("start sync run: %w", err)
	}
	return uuidString(id), nil
}

// jsonReject is a rejected entry as stored in venue_sync_runs.rejects.
type jsonReject struct {
	SourceKey string        `json:"sourceKey"`
	Name      string        `json:"name"`
	Reasons   []jsonProblem `json:"reasons"`
}

type jsonProblem struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

// FinishRun stores a run's outcome and keeps the last 30 runs of its source.
func (s *SyncStore) FinishRun(ctx context.Context, r domain.SyncRun) error {
	id, ok := parseUUID(r.ID)
	if !ok {
		return fmt.Errorf("finish sync run: %q is not a UUID", r.ID)
	}
	errs := append([]string{}, r.Errors...)
	errJSON, err := json.Marshal(errs)
	if err != nil {
		return err
	}
	rejects := make([]jsonReject, 0, len(r.Rejects))
	for _, rj := range r.Rejects {
		j := jsonReject{SourceKey: rj.SourceKey, Name: rj.Name, Reasons: make([]jsonProblem, len(rj.Problems))}
		for i, p := range rj.Problems {
			j.Reasons[i] = jsonProblem{Field: p.Field, Code: p.Code}
		}
		rejects = append(rejects, j)
	}
	rejJSON, err := json.Marshal(rejects)
	if err != nil {
		return err
	}
	q := s.q(ctx)
	if err := q.FinishSyncRun(ctx, sqlcgen.FinishSyncRunParams{
		ID: id, FinishedAt: ts(r.FinishedAt), Status: string(r.Status), CommitSha: text(r.CommitSHA),
		Created: int32Of(r.Created), Updated: int32Of(r.Updated), Unchanged: int32Of(r.Unchanged),
		Skipped: int32Of(r.Skipped), Rejected: int32Of(r.Rejected), Errors: errJSON, Rejects: rejJSON,
	}); err != nil {
		return fmt.Errorf("finish sync run: %w", err)
	}
	if err := q.PruneSyncRuns(ctx, string(r.Source)); err != nil {
		return fmt.Errorf("prune sync runs: %w", err)
	}
	return nil
}

// FailStaleRuns marks a source's runs still running failed: its lease has expired.
func (s *SyncStore) FailStaleRuns(ctx context.Context, source domain.Source, at time.Time) error {
	if err := s.q(ctx).FailStaleSyncRuns(ctx, sqlcgen.FailStaleSyncRunsParams{Source: string(source), At: ts(at)}); err != nil {
		return fmt.Errorf("fail stale sync runs: %w", err)
	}
	return nil
}

// LatestRun returns the latest run of a source, real or dry.
func (s *SyncStore) LatestRun(ctx context.Context, source domain.Source, dryRun bool) (domain.SyncRun, bool, error) {
	r, err := s.q(ctx).LatestSyncRun(ctx, sqlcgen.LatestSyncRunParams{Source: string(source), DryRun: dryRun})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SyncRun{}, false, nil
	}
	if err != nil {
		return domain.SyncRun{}, false, fmt.Errorf("read sync run: %w", err)
	}
	run := domain.SyncRun{
		ID: uuidString(r.ID), Source: domain.Source(r.Source), DryRun: r.DryRun, Trigger: domain.SyncTrigger(r.Trigger),
		RequestedBy: uuidString(r.RequestedBy), StartedAt: timeOf(r.StartedAt), FinishedAt: timeOf(r.FinishedAt),
		Status: domain.SyncStatus(r.Status), CommitSHA: r.CommitSha.String, Created: int(r.Created), Updated: int(r.Updated),
		Unchanged: int(r.Unchanged), Skipped: int(r.Skipped), Rejected: int(r.Rejected),
	}
	if err := json.Unmarshal(r.Errors, &run.Errors); err != nil {
		return domain.SyncRun{}, false, fmt.Errorf("read sync run errors: %w", err)
	}
	var rejects []jsonReject
	if err := json.Unmarshal(r.Rejects, &rejects); err != nil {
		return domain.SyncRun{}, false, fmt.Errorf("read sync run rejects: %w", err)
	}
	for _, j := range rejects {
		rj := domain.SyncReject{SourceKey: j.SourceKey, Name: j.Name}
		for _, p := range j.Reasons {
			rj.Problems = append(rj.Problems, domain.Problem{Field: p.Field, Code: p.Code})
		}
		run.Rejects = append(run.Rejects, rj)
	}
	return run, true, nil
}

// LinkChecks returns the stored checks of these links.
func (s *SyncStore) LinkChecks(ctx context.Context, urls []string) (map[string]domain.LinkCheck, error) {
	out := map[string]domain.LinkCheck{}
	if len(urls) == 0 {
		return out, nil
	}
	rows, err := s.q(ctx).LinkChecks(ctx, urls)
	if err != nil {
		return nil, fmt.Errorf("read link checks: %w", err)
	}
	for _, r := range rows {
		out[r.Url] = domain.LinkCheck{OK: r.Ok, FinalStatus: int(r.FinalStatus), CheckedAt: timeOf(r.CheckedAt)}
	}
	return out, nil
}

// SaveLinkChecks stores checks. A link the database would refuse (not https, too long) is
// left out: it can only come from a broken feed, and is checked again next time.
func (s *SyncStore) SaveLinkChecks(ctx context.Context, checks map[string]domain.LinkCheck) error {
	q := s.q(ctx)
	for u, c := range checks {
		if len(u) > 2048 || len(u) < len("https://") || u[:len("https://")] != "https://" {
			continue
		}
		if err := q.SaveLinkCheck(ctx, sqlcgen.SaveLinkCheckParams{
			Url: u, Ok: c.OK, FinalStatus: int32Of(c.FinalStatus), CheckedAt: pgtype.Timestamptz{Time: c.CheckedAt, Valid: true},
		}); err != nil {
			return fmt.Errorf("save link check: %w", err)
		}
	}
	return nil
}
