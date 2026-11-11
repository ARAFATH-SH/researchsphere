package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

var _ domain.ScanQueue = (*Store)(nil)

// tq returns the queries on the context's transaction, or on the pool.
func (s *Store) tq(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Conn(ctx, s.pool))
}

// ClaimScans leases due files in one statement.
func (s *Store) ClaimScans(ctx context.Context, limit int, lease time.Duration) ([]domain.ScanJob, error) {
	rows, err := s.tq(ctx).ClaimScans(ctx, sqlcgen.ClaimScansParams{
		LeaseSeconds: lease.Seconds(), MaxRows: int32(min(max(limit, 1), 100)), //nolint:gosec // bounded
	})
	if err != nil {
		return nil, fmt.Errorf("claim scans: %w", err)
	}
	jobs := make([]domain.ScanJob, len(rows))
	for i, r := range rows {
		jobs[i] = domain.ScanJob{
			FileID: r.ID.String(), OwnerID: r.OwnerUserID.String(), ObjectKey: r.ObjectKey,
			Attempts: int(r.ScanAttempts), Token: r.LeaseToken.String(),
		}
	}
	return jobs, nil
}

// MarkClean is false when the lease was lost.
func (s *Store) MarkClean(ctx context.Context, job domain.ScanJob) (bool, error) {
	id, token, err := leaseIDs(job)
	if err != nil {
		return false, err
	}
	n, err := s.tq(ctx).MarkScanClean(ctx, sqlcgen.MarkScanCleanParams{ID: id, LeaseToken: token})
	if err != nil {
		return false, fmt.Errorf("mark scan clean: %w", err)
	}
	return n == 1, nil
}

// MarkInfected marks the file and queues its object in one statement.
func (s *Store) MarkInfected(ctx context.Context, job domain.ScanJob) (bool, error) {
	id, token, err := leaseIDs(job)
	if err != nil {
		return false, err
	}
	n, err := s.tq(ctx).MarkScanInfected(ctx, sqlcgen.MarkScanInfectedParams{ID: id, LeaseToken: token})
	if err != nil {
		return false, fmt.Errorf("mark scan infected: %w", err)
	}
	return n == 1, nil
}

// MarkFailed counts the last try.
func (s *Store) MarkFailed(ctx context.Context, job domain.ScanJob, code string) (bool, error) {
	id, token, err := leaseIDs(job)
	if err != nil {
		return false, err
	}
	n, err := s.tq(ctx).MarkScanFailed(ctx, sqlcgen.MarkScanFailedParams{
		ID: id, LeaseToken: token, ScanError: pgtype.Text{String: code, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("mark scan failed: %w", err)
	}
	return n == 1, nil
}

// RetryLater gives the lease back; an empty code keeps the last error.
func (s *Store) RetryLater(ctx context.Context, job domain.ScanJob, delay time.Duration, counted bool, code string) (bool, error) {
	id, token, err := leaseIDs(job)
	if err != nil {
		return false, err
	}
	arg := sqlcgen.RetryScanLaterParams{ID: id, LeaseToken: token, DelaySeconds: delay.Seconds(), ScanError: pgtype.Text{String: code, Valid: code != ""}}
	if counted {
		arg.AddAttempts = 1
	}
	n, err := s.tq(ctx).RetryScanLater(ctx, arg)
	if err != nil {
		return false, fmt.Errorf("retry scan later: %w", err)
	}
	return n == 1, nil
}

func leaseIDs(job domain.ScanJob) (pgtype.UUID, pgtype.UUID, error) {
	var id, token pgtype.UUID
	if err := id.Scan(job.FileID); err != nil {
		return id, token, fmt.Errorf("scan job: file ID %q: %w", job.FileID, err)
	}
	if err := token.Scan(job.Token); err != nil {
		return id, token, fmt.Errorf("scan job: lease token: %w", err)
	}
	return id, token, nil
}
