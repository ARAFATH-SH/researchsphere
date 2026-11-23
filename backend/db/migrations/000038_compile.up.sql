-- LaTeX compile (FR-13.1, FR-13.2, FR-13.4 to FR-13.6, NFR-3, NFR-4; slice 23, chunk 23.1).
-- docs/decisions.md 2026-11-23 (compile service) and docs/compile.md.
--
-- compile_jobs exists since 000001 and no code wrote it before this slice. It is the queue:
-- the compile worker claims the oldest queued row through the API's internal listener
-- (FOR UPDATE SKIP LOCKED), so no job lives only in Redis. Its status becomes text with a
-- CHECK, as every other status column, and gains `cancelled`. The PDF becomes a stored file
-- owned by the project, so it counts in the project's storage (FR-12.2) and its object is
-- queued for deletion by the trigger of 000007 when its row goes. The log stays in the row.

-- ---------------------------------------------------------------------------
-- compile_jobs
-- ---------------------------------------------------------------------------
DROP INDEX compile_jobs_one_active_uq;

ALTER TABLE compile_jobs
    ALTER COLUMN status DROP DEFAULT,
    ALTER COLUMN status TYPE text USING status::text,
    ALTER COLUMN status SET DEFAULT 'queued',
    ADD CONSTRAINT compile_jobs_status CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'timed_out', 'cancelled')),
    ALTER COLUMN id SET DEFAULT uuid_v7(),
    DROP COLUMN pdf_storage_key,
    -- The engine is a setting so that XeLaTeX can be added later (open item in decisions.md).
    ADD COLUMN engine text NOT NULL DEFAULT 'pdflatex' CONSTRAINT compile_jobs_engine CHECK (engine IN ('pdflatex')),
    -- The main file when the compile was asked for (projects.main_file_path may move later).
    ADD COLUMN main_file_path text NOT NULL DEFAULT 'main.tex' CONSTRAINT compile_jobs_main_file CHECK (length(main_file_path) BETWEEN 1 AND 255),
    ADD COLUMN worker_id text CONSTRAINT compile_jobs_worker CHECK (length(worker_id) BETWEEN 1 AND 100),
    ADD COLUMN lease_until timestamptz,
    ADD COLUMN duration_ms integer CONSTRAINT compile_jobs_duration CHECK (duration_ms >= 0),
    ADD COLUMN exit_code integer,
    ADD COLUMN failure text CONSTRAINT compile_jobs_failure CHECK (failure IN (
        'input_too_large', 'output_too_large', 'out_of_memory', 'no_pdf', 'worker_lost', 'sandbox_error', 'queue_expired')),
    -- FR-13.2: [{file, line, message, severity}], at most 100.
    ADD COLUMN errors jsonb NOT NULL DEFAULT '[]' CONSTRAINT compile_jobs_errors
        CHECK (jsonb_typeof(errors) = 'array' AND jsonb_array_length(errors) <= 100),
    -- Figures left out because their scan has not found them clean: [{path, reason}].
    ADD COLUMN skipped jsonb NOT NULL DEFAULT '[]' CONSTRAINT compile_jobs_skipped
        CHECK (jsonb_typeof(skipped) = 'array' AND jsonb_array_length(skipped) <= 200),
    -- False when the sync server did not confirm it stored the open documents in time.
    ADD COLUMN live_text_confirmed boolean NOT NULL DEFAULT true,
    ADD COLUMN pages integer CONSTRAINT compile_jobs_pages CHECK (pages >= 0),
    ADD COLUMN pdf_file_id uuid,
    -- The PDF is always the project's own stored file; it goes when that row goes.
    ADD CONSTRAINT compile_jobs_pdf_fk FOREIGN KEY (pdf_file_id, project_id)
        REFERENCES stored_files (id, project_id) ON DELETE SET NULL (pdf_file_id),
    ADD CONSTRAINT compile_jobs_pdf_uq UNIQUE (pdf_file_id),
    ADD CONSTRAINT compile_jobs_log_size CHECK (octet_length(log) <= 2097152),
    -- The status and the columns that go with it.
    ADD CONSTRAINT compile_jobs_queued CHECK (status <> 'queued' OR (started_at IS NULL AND finished_at IS NULL AND worker_id IS NULL)),
    ADD CONSTRAINT compile_jobs_running CHECK (status <> 'running' OR (started_at IS NOT NULL AND lease_until IS NOT NULL AND worker_id IS NOT NULL AND finished_at IS NULL)),
    ADD CONSTRAINT compile_jobs_finished CHECK ((status IN ('queued', 'running')) = (finished_at IS NULL)),
    ADD CONSTRAINT compile_jobs_failure_status CHECK (failure IS NULL OR status IN ('failed', 'cancelled')),
    ADD CONSTRAINT compile_jobs_cancelled CHECK (status <> 'cancelled' OR failure = 'queue_expired'),
    -- A PDF is stored while the job runs and kept only when it succeeded.
    ADD CONSTRAINT compile_jobs_pdf_status CHECK (pdf_file_id IS NULL OR status IN ('running', 'succeeded')),
    ADD CONSTRAINT compile_jobs_log_status CHECK (log IS NULL OR finished_at IS NOT NULL);

ALTER TABLE compile_jobs ALTER COLUMN engine DROP DEFAULT, ALTER COLUMN main_file_path DROP DEFAULT;

DROP TYPE compile_status;

-- One compile per project at a time (FR-13.4), as in 000001.
CREATE UNIQUE INDEX compile_jobs_one_active_uq ON compile_jobs (project_id) WHERE status IN ('queued', 'running');
-- The worker claims the oldest queued job; the sweep finds expired leases and old queued jobs.
CREATE INDEX compile_jobs_queued_idx ON compile_jobs (queued_at) WHERE status = 'queued';
CREATE INDEX compile_jobs_lease_idx ON compile_jobs (lease_until) WHERE status = 'running';
CREATE INDEX compile_jobs_requested_by_idx ON compile_jobs (requested_by) WHERE requested_by IS NOT NULL;

-- A job's PDF goes with the job, or when another PDF replaces it; the stored file's own
-- trigger (000007) queues the object for the cleanup worker.
CREATE FUNCTION compile_job_pdf_released() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.pdf_file_id IS NOT NULL AND (TG_OP = 'DELETE' OR NEW.pdf_file_id IS DISTINCT FROM OLD.pdf_file_id) THEN
        DELETE FROM stored_files WHERE id = OLD.pdf_file_id;
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER compile_jobs_release_pdf
    AFTER DELETE OR UPDATE OF pdf_file_id ON compile_jobs
    FOR EACH ROW EXECUTE FUNCTION compile_job_pdf_released();

-- ---------------------------------------------------------------------------
-- stored_files: the compiled PDF is owned by its project, like a figure. It is made by the
-- sandbox from clean files and never scanned (docs/decisions.md): it is served only by the
-- API with a sandboxing CSP and nosniff, so its status is recorded as clean.
-- ---------------------------------------------------------------------------
ALTER TABLE stored_files
    DROP CONSTRAINT stored_files_project_purpose,
    ADD CONSTRAINT stored_files_project_purpose CHECK ((purpose IN ('figure', 'pdf')) = (project_id IS NOT NULL)),
    ADD CONSTRAINT stored_files_pdf_size CHECK (purpose <> 'pdf' OR size_bytes <= 209715200),
    ADD CONSTRAINT stored_files_pdf_clean CHECK (purpose <> 'pdf' OR scan_status = 'clean');

-- ---------------------------------------------------------------------------
-- projects: the engine, a setting so that XeLaTeX can be added later.
-- ---------------------------------------------------------------------------
ALTER TABLE projects
    ADD COLUMN compile_engine text NOT NULL DEFAULT 'pdflatex' CONSTRAINT projects_compile_engine CHECK (compile_engine IN ('pdflatex'));
