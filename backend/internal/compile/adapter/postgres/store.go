// Package postgres implements the compile domain's Store with the queries sqlc generates from
// queries.sql (migration 000038). It reads the project's tables it needs (main file, text
// paths, members) with queries of its own, as the recommendation module does.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/compile/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/compile/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// Store keeps compile jobs in PostgreSQL. Every method runs on the transaction the context
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

func parseUUID(v string) (pgtype.UUID, bool) {
	var u pgtype.UUID
	if err := u.Scan(v); err != nil {
		return pgtype.UUID{}, false
	}
	return u, true
}

func optTime(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

func optInt(i pgtype.Int4) *int {
	if !i.Valid {
		return nil
	}
	v := int(i.Int32)
	return &v
}

func optUUID(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := u.String()
	return &s
}

// jobRow is the columns every job query reads; the generated row types convert to it.
type jobRow struct {
	ID                pgtype.UUID
	ProjectID         pgtype.UUID
	RequestedBy       pgtype.UUID
	Status            string
	Engine            string
	MainFilePath      string
	QueuedAt          pgtype.Timestamptz
	StartedAt         pgtype.Timestamptz
	FinishedAt        pgtype.Timestamptz
	DurationMs        pgtype.Int4
	Failure           pgtype.Text
	Errors            []byte
	Skipped           []byte
	LiveTextConfirmed bool
	Pages             pgtype.Int4
	HasLog            bool
	HasPdf            bool
}

func (r jobRow) job() (domain.Job, error) {
	j := domain.Job{
		ID: r.ID.String(), ProjectID: r.ProjectID.String(), RequestedBy: optUUID(r.RequestedBy),
		Status: domain.Status(r.Status), Engine: r.Engine, MainFilePath: r.MainFilePath,
		QueuedAt: r.QueuedAt.Time, StartedAt: optTime(r.StartedAt), FinishedAt: optTime(r.FinishedAt),
		DurationMs: optInt(r.DurationMs), LiveTextConfirmed: r.LiveTextConfirmed, Pages: optInt(r.Pages),
		HasLog: r.HasLog, HasPDF: r.HasPdf, Problems: []domain.Problem{}, Skipped: []domain.Skipped{},
	}
	if r.Failure.Valid {
		f := domain.Failure(r.Failure.String)
		j.Failure = &f
	}
	if err := json.Unmarshal(r.Errors, &j.Problems); err != nil {
		return domain.Job{}, fmt.Errorf("read compile errors: %w", err)
	}
	if err := json.Unmarshal(r.Skipped, &j.Skipped); err != nil {
		return domain.Job{}, fmt.Errorf("read compile skips: %w", err)
	}
	return j, nil
}

// Project reads the project's main file and engine.
func (s *Store) Project(ctx context.Context, projectID string) (string, string, bool, error) {
	id, ok := parseUUID(projectID)
	if !ok {
		return "", "", false, domain.ErrNotFound
	}
	r, err := s.q(ctx).CompileProject(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, domain.ErrNotFound
	}
	if err != nil {
		return "", "", false, fmt.Errorf("read project for compile: %w", err)
	}
	if r.MainPath == "" {
		return r.MainFilePath, r.CompileEngine, false, nil
	}
	return r.MainPath, r.CompileEngine, true, nil
}

// QueuedCount counts the queued jobs.
func (s *Store) QueuedCount(ctx context.Context) (int, error) {
	n, err := s.q(ctx).QueuedCompileCount(ctx)
	if err != nil {
		return 0, fmt.Errorf("count queued compiles: %w", err)
	}
	return int(n), nil
}

// Insert queues a job; *domain.RunningError when the project has an active one.
func (s *Store) Insert(ctx context.Context, j domain.NewJob) (domain.Job, error) {
	pid, ok1 := parseUUID(j.ProjectID)
	uid, ok2 := parseUUID(j.RequestedBy)
	if !ok1 || !ok2 {
		return domain.Job{}, domain.ErrNotFound
	}
	q := s.q(ctx)
	id, err := q.InsertCompileJob(ctx, sqlcgen.InsertCompileJobParams{
		ProjectID: pid, RequestedBy: uid, Engine: j.Engine, MainFilePath: j.MainFilePath,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "compile_jobs_one_active_uq" {
		active, aerr := q.ActiveCompileJob(ctx, pid)
		if aerr != nil {
			return domain.Job{}, &domain.RunningError{}
		}
		return domain.Job{}, &domain.RunningError{ID: active.String()}
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("queue compile: %w", err)
	}
	return s.Get(ctx, j.ProjectID, id.String())
}

// Latest is the project's newest job.
func (s *Store) Latest(ctx context.Context, projectID string) (domain.Job, error) {
	id, ok := parseUUID(projectID)
	if !ok {
		return domain.Job{}, domain.ErrNoCompile
	}
	r, err := s.q(ctx).LatestCompileJob(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, domain.ErrNoCompile
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("read latest compile: %w", err)
	}
	return jobRow(r).job()
}

// Get is a job of the project.
func (s *Store) Get(ctx context.Context, projectID, jobID string) (domain.Job, error) {
	pid, ok1 := parseUUID(projectID)
	jid, ok2 := parseUUID(jobID)
	if !ok1 || !ok2 {
		return domain.Job{}, domain.ErrNotFound
	}
	r, err := s.q(ctx).CompileJob(ctx, sqlcgen.CompileJobParams{ProjectID: pid, ID: jid})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("read compile: %w", err)
	}
	return jobRow(r).job()
}

// Log is the job's log.
func (s *Store) Log(ctx context.Context, projectID, jobID string) (string, error) {
	pid, ok1 := parseUUID(projectID)
	jid, ok2 := parseUUID(jobID)
	if !ok1 || !ok2 {
		return "", domain.ErrNotFound
	}
	l, err := s.q(ctx).CompileLog(ctx, sqlcgen.CompileLogParams{ProjectID: pid, ID: jid})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read compile log: %w", err)
	}
	if !l.Valid {
		return "", domain.ErrNoLog
	}
	return l.String, nil
}

// LatestPDF is the project's PDF.
func (s *Store) LatestPDF(ctx context.Context, projectID string) (domain.PDF, error) {
	id, ok := parseUUID(projectID)
	if !ok {
		return domain.PDF{}, domain.ErrNoPDF
	}
	r, err := s.q(ctx).LatestCompilePDF(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PDF{}, domain.ErrNoPDF
	}
	if err != nil {
		return domain.PDF{}, fmt.Errorf("read latest PDF: %w", err)
	}
	return domain.PDF{CompileID: r.ID.String(), ObjectKey: r.ObjectKey, Size: r.SizeBytes, FinishedAt: r.FinishedAt.Time, Title: r.Title}, nil
}

// LatestPDFJob is the job holding the project's PDF.
func (s *Store) LatestPDFJob(ctx context.Context, projectID string) (string, error) {
	id, ok := parseUUID(projectID)
	if !ok {
		return "", nil
	}
	j, err := s.q(ctx).LatestPDFJob(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read latest PDF job: %w", err)
	}
	return j.String(), nil
}

// Claim takes the oldest queued job.
func (s *Store) Claim(ctx context.Context, workerID string, lease time.Duration) (domain.Claimed, bool, error) {
	r, err := s.q(ctx).ClaimCompileJob(ctx, sqlcgen.ClaimCompileJobParams{
		WorkerID: pgtype.Text{String: workerID, Valid: true}, LeaseSeconds: lease.Seconds(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Claimed{}, false, nil
	}
	if err != nil {
		return domain.Claimed{}, false, fmt.Errorf("claim compile: %w", err)
	}
	return domain.Claimed{ID: r.ID.String(), ProjectID: r.ProjectID.String(), MainFilePath: r.MainFilePath, Engine: r.Engine}, true, nil
}

// Running reads a running job under a lease.
func (s *Store) Running(ctx context.Context, jobID string) (domain.Claimed, error) {
	id, ok := parseUUID(jobID)
	if !ok {
		return domain.Claimed{}, domain.ErrNotFound
	}
	r, err := s.q(ctx).RunningCompileJob(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Claimed{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Claimed{}, fmt.Errorf("read running compile: %w", err)
	}
	return domain.Claimed{ID: r.ID.String(), ProjectID: r.ProjectID.String(), MainFilePath: r.MainFilePath, Engine: r.Engine}, nil
}

// SetSource records what the compile left out.
func (s *Store) SetSource(ctx context.Context, jobID string, skipped []domain.Skipped, liveConfirmed bool) error {
	id, ok := parseUUID(jobID)
	if !ok {
		return domain.ErrNotFound
	}
	if skipped == nil {
		skipped = []domain.Skipped{}
	}
	b, err := json.Marshal(skipped)
	if err != nil {
		return err
	}
	if err := s.q(ctx).SetCompileSource(ctx, sqlcgen.SetCompileSourceParams{ID: id, Skipped: b, LiveTextConfirmed: liveConfirmed}); err != nil {
		return fmt.Errorf("record compile source: %w", err)
	}
	return nil
}

// AttachPDF records the PDF as the project's stored file and the job's, in one transaction
// of its own unless the context carries one.
func (s *Store) AttachPDF(ctx context.Context, jobID string, p domain.NewPDF) error {
	id, ok := parseUUID(jobID)
	if !ok {
		return domain.ErrNotFound
	}
	return postgres.NewTransactor(s.pool).InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		fid, err := q.InsertCompilePDF(ctx, sqlcgen.InsertCompilePDFParams{ObjectKey: p.ObjectKey, SizeBytes: p.Size, JobID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("record PDF: %w", err)
		}
		if err := q.SetCompilePDF(ctx, sqlcgen.SetCompilePDFParams{ID: id, PdfFileID: fid}); err != nil {
			return fmt.Errorf("attach PDF: %w", err)
		}
		return nil
	})
}

// LockRunning locks the job.
func (s *Store) LockRunning(ctx context.Context, jobID string) (domain.Job, error) {
	id, ok := parseUUID(jobID)
	if !ok {
		return domain.Job{}, domain.ErrNotFound
	}
	r, err := s.q(ctx).LockCompileJob(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("lock compile: %w", err)
	}
	return jobRow(r).job()
}

// Finish records how the job ended.
func (s *Store) Finish(ctx context.Context, jobID string, f domain.Finish) (domain.Job, error) {
	id, ok := parseUUID(jobID)
	if !ok {
		return domain.Job{}, domain.ErrNotFound
	}
	problems := f.Problems
	if problems == nil {
		problems = []domain.Problem{}
	}
	b, err := json.Marshal(problems)
	if err != nil {
		return domain.Job{}, err
	}
	p := sqlcgen.FinishCompileJobParams{
		ID: id, Status: string(f.Status), Errors: b,
		DurationMs: pgtype.Int4{Int32: int32(min(f.DurationMs, 1<<31-1)), Valid: true}, //nolint:gosec // capped
	}
	if f.Failure != nil {
		p.Failure = pgtype.Text{String: string(*f.Failure), Valid: true}
	}
	if f.ExitCode != nil {
		p.ExitCode = pgtype.Int4{Int32: int32(*f.ExitCode), Valid: true} //nolint:gosec // an exit code
	}
	if f.Log != nil {
		p.Log = pgtype.Text{String: *f.Log, Valid: true}
	}
	if f.Pages != nil {
		p.Pages = pgtype.Int4{Int32: int32(min(*f.Pages, 1<<31-1)), Valid: true} //nolint:gosec // capped
	}
	q := s.q(ctx)
	if err := q.FinishCompileJob(ctx, p); err != nil {
		return domain.Job{}, fmt.Errorf("finish compile: %w", err)
	}
	r, err := q.LockCompileJob(ctx, id)
	if err != nil {
		return domain.Job{}, fmt.Errorf("read finished compile: %w", err)
	}
	return jobRow(r).job()
}

// Retain keeps the newest jobs, the newest log and one PDF (SRS 7, Retention).
func (s *Store) Retain(ctx context.Context, projectID, newest, pdfJob string, keep int) error {
	pid, ok1 := parseUUID(projectID)
	nid, ok2 := parseUUID(newest)
	if !ok1 || !ok2 {
		return domain.ErrNotFound
	}
	var kid pgtype.UUID
	if pdfJob != "" {
		kid, _ = parseUUID(pdfJob)
	}
	q := s.q(ctx)
	if err := q.DropOlderLogs(ctx, sqlcgen.DropOlderLogsParams{ProjectID: pid, Newest: nid}); err != nil {
		return fmt.Errorf("drop older logs: %w", err)
	}
	if err := q.DropOlderPDFs(ctx, sqlcgen.DropOlderPDFsParams{ProjectID: pid, Keep: kid}); err != nil {
		return fmt.Errorf("drop older PDFs: %w", err)
	}
	if err := q.DropOldJobs(ctx, sqlcgen.DropOldJobsParams{ProjectID: pid, Keep: kid, KeepCount: int32(keep)}); err != nil { //nolint:gosec // small
		return fmt.Errorf("drop old compiles: %w", err)
	}
	return nil
}

type idPair struct{ id, project pgtype.UUID }

func (s *Store) jobs(ctx context.Context, ids []idPair) ([]domain.Job, error) {
	out := make([]domain.Job, 0, len(ids))
	for _, p := range ids {
		j, err := s.Get(ctx, p.project.String(), p.id.String())
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, nil
}

// ExpireLeases fails running jobs whose lease ended.
func (s *Store) ExpireLeases(ctx context.Context) ([]domain.Job, error) {
	rows, err := s.q(ctx).ExpireCompileLeases(ctx)
	if err != nil {
		return nil, fmt.Errorf("expire compile leases: %w", err)
	}
	ids := make([]idPair, len(rows))
	for i, r := range rows {
		ids[i] = idPair{r.ID, r.ProjectID}
	}
	return s.jobs(ctx, ids)
}

// ExpireQueued cancels jobs queued before before.
func (s *Store) ExpireQueued(ctx context.Context, before time.Time) ([]domain.Job, error) {
	rows, err := s.q(ctx).ExpireQueuedCompiles(ctx, pgtype.Timestamptz{Time: before, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("expire queued compiles: %w", err)
	}
	ids := make([]idPair, len(rows))
	for i, r := range rows {
		ids[i] = idPair{r.ID, r.ProjectID}
	}
	return s.jobs(ctx, ids)
}

// TextPaths lists the project's text file paths.
func (s *Store) TextPaths(ctx context.Context, projectID string) ([]string, error) {
	id, ok := parseUUID(projectID)
	if !ok {
		return nil, nil
	}
	paths, err := s.q(ctx).CompileTextPaths(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list text paths: %w", err)
	}
	return paths, nil
}

// MemberIDs lists the project's members.
func (s *Store) MemberIDs(ctx context.Context, projectID string) ([]string, error) {
	id, ok := parseUUID(projectID)
	if !ok {
		return nil, nil
	}
	rows, err := s.q(ctx).CompileMemberIDs(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.String()
	}
	return out, nil
}

// OutputBefore lists the compiles that started before before and still hold a PDF or a log.
func (s *Store) OutputBefore(ctx context.Context, before time.Time) ([]domain.Output, error) {
	rows, err := s.q(ctx).CompileOutputBefore(ctx, pgtype.Timestamptz{Time: before, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list compile output: %w", err)
	}
	out := make([]domain.Output, len(rows))
	for i, r := range rows {
		out[i] = domain.Output{JobID: r.ID.String(), ProjectID: r.ProjectID.String(), FinishedAt: optTime(r.FinishedAt), HasPDF: r.HasPdf, HasLog: r.HasLog}
	}
	return out, nil
}

// ClearOutputBefore drops the PDFs and logs of the compiles that started before before; each
// PDF's stored_files row goes and its object is queued for deletion, in the same statement.
func (s *Store) ClearOutputBefore(ctx context.Context, before time.Time) ([]domain.Output, error) {
	rows, err := s.q(ctx).ClearCompileOutputBefore(ctx, pgtype.Timestamptz{Time: before, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("clear compile output: %w", err)
	}
	out := make([]domain.Output, len(rows))
	for i, r := range rows {
		out[i] = domain.Output{JobID: r.ID.String(), ProjectID: r.ProjectID.String(), FinishedAt: optTime(r.FinishedAt), HasPDF: r.HasPdf, HasLog: r.HasLog}
	}
	return out, nil
}
