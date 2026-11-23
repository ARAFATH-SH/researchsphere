-- Compile jobs (FR-13; slice 23, migration 000038). compile_jobs is the queue.

-- name: CompileProject :one
-- The main file, the engine, and the text file at the main file's path (ignoring case), if any.
SELECT p.main_file_path, p.compile_engine,
       coalesce((SELECT f.path FROM project_files f
                 WHERE f.project_id = p.id AND lower(f.path) = lower(p.main_file_path) AND f.kind = 'text'), '')::text AS main_path
FROM projects p
WHERE p.id = @id;

-- name: QueuedCompileCount :one
SELECT count(*) FROM compile_jobs WHERE status = 'queued';

-- name: InsertCompileJob :one
INSERT INTO compile_jobs (project_id, requested_by, engine, main_file_path)
VALUES (@project_id, @requested_by, @engine, @main_file_path)
RETURNING id;

-- name: ActiveCompileJob :one
SELECT id FROM compile_jobs WHERE project_id = @project_id AND status IN ('queued', 'running');

-- name: CompileJob :one
SELECT id, project_id, requested_by, status, engine, main_file_path, queued_at, started_at,
       finished_at, duration_ms, failure, errors, skipped, live_text_confirmed, pages,
       (log IS NOT NULL)::boolean AS has_log, (pdf_file_id IS NOT NULL)::boolean AS has_pdf
FROM compile_jobs
WHERE project_id = @project_id AND id = @id;

-- name: LatestCompileJob :one
SELECT id, project_id, requested_by, status, engine, main_file_path, queued_at, started_at,
       finished_at, duration_ms, failure, errors, skipped, live_text_confirmed, pages,
       (log IS NOT NULL)::boolean AS has_log, (pdf_file_id IS NOT NULL)::boolean AS has_pdf
FROM compile_jobs
WHERE project_id = @project_id
ORDER BY queued_at DESC, id DESC
LIMIT 1;

-- name: CompileLog :one
SELECT log FROM compile_jobs WHERE project_id = @project_id AND id = @id;

-- name: LatestCompilePDF :one
-- The PDF of a succeeded job; a running job's PDF is not the project's yet.
SELECT j.id, j.finished_at, sf.object_key, sf.size_bytes, p.title
FROM compile_jobs j
JOIN stored_files sf ON sf.id = j.pdf_file_id
JOIN projects p ON p.id = j.project_id
WHERE j.project_id = @project_id AND j.status = 'succeeded'
ORDER BY j.finished_at DESC
LIMIT 1;

-- name: LatestPDFJob :one
SELECT id FROM compile_jobs
WHERE project_id = @project_id AND status = 'succeeded' AND pdf_file_id IS NOT NULL
ORDER BY finished_at DESC
LIMIT 1;

-- name: ClaimCompileJob :one
UPDATE compile_jobs
SET status = 'running', started_at = now(), worker_id = @worker_id,
    lease_until = now() + make_interval(secs => @lease_seconds::float8)
WHERE id = (SELECT q.id FROM compile_jobs q WHERE q.status = 'queued'
            ORDER BY q.queued_at, q.id LIMIT 1 FOR UPDATE SKIP LOCKED)
RETURNING id, project_id, main_file_path, engine;

-- name: RunningCompileJob :one
SELECT id, project_id, main_file_path, engine
FROM compile_jobs
WHERE id = @id AND status = 'running' AND lease_until > now();

-- name: SetCompileSource :exec
UPDATE compile_jobs SET skipped = @skipped, live_text_confirmed = @live_text_confirmed
WHERE id = @id AND status = 'running';

-- name: InsertCompilePDF :one
INSERT INTO stored_files (project_id, purpose, object_key, content_type, size_bytes, scan_status)
SELECT j.project_id, 'pdf', @object_key, 'application/pdf', @size_bytes, 'clean'
FROM compile_jobs j
WHERE j.id = @job_id AND j.status = 'running' AND j.lease_until > now()
RETURNING id;

-- name: SetCompilePDF :exec
UPDATE compile_jobs SET pdf_file_id = @pdf_file_id WHERE id = @id;

-- name: LockCompileJob :one
SELECT id, project_id, requested_by, status, engine, main_file_path, queued_at, started_at,
       finished_at, duration_ms, failure, errors, skipped, live_text_confirmed, pages,
       (log IS NOT NULL)::boolean AS has_log, (pdf_file_id IS NOT NULL)::boolean AS has_pdf
FROM compile_jobs
WHERE id = @id
FOR UPDATE;

-- name: FinishCompileJob :exec
-- Only a succeeded job keeps its PDF; the trigger deletes another's.
UPDATE compile_jobs
SET status = @status, failure = sqlc.narg(failure), exit_code = sqlc.narg(exit_code),
    duration_ms = @duration_ms, log = sqlc.narg(log), errors = @errors, pages = sqlc.narg(pages),
    finished_at = now(), lease_until = NULL,
    pdf_file_id = CASE WHEN @status = 'succeeded' THEN pdf_file_id END
WHERE id = @id;

-- name: DropOlderLogs :exec
UPDATE compile_jobs SET log = NULL
WHERE project_id = @project_id AND id <> @newest AND log IS NOT NULL;

-- name: DropOlderPDFs :exec
UPDATE compile_jobs SET pdf_file_id = NULL
WHERE project_id = @project_id AND pdf_file_id IS NOT NULL AND status <> 'running'
  AND id IS DISTINCT FROM sqlc.narg(keep);

-- name: DropOldJobs :exec
DELETE FROM compile_jobs j
WHERE j.project_id = @project_id AND j.status NOT IN ('queued', 'running')
  AND j.id IS DISTINCT FROM sqlc.narg(keep)
  AND j.id NOT IN (SELECT k.id FROM compile_jobs k WHERE k.project_id = @project_id
                   ORDER BY k.queued_at DESC, k.id DESC LIMIT @keep_count);

-- name: ExpireCompileLeases :many
UPDATE compile_jobs
SET status = 'failed', failure = 'worker_lost', finished_at = now(), lease_until = NULL, pdf_file_id = NULL
WHERE status = 'running' AND lease_until <= now()
RETURNING id, project_id;

-- name: ExpireQueuedCompiles :many
UPDATE compile_jobs
SET status = 'cancelled', failure = 'queue_expired', finished_at = now()
WHERE status = 'queued' AND queued_at < @before
RETURNING id, project_id;

-- name: CompileTextPaths :many
SELECT path FROM project_files WHERE project_id = @project_id AND kind = 'text';

-- name: CompileMemberIDs :many
SELECT user_id FROM project_members WHERE project_id = @project_id;
