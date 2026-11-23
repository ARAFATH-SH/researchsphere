-- Undoes 000038. Compile jobs are records of past runs: they cannot all be expressed in the
-- old shape (`cancelled`, a PDF as a stored file), so they are deleted, and with them their
-- PDFs (the trigger below, then the stored_files trigger of 000007 queues the objects).

DELETE FROM compile_jobs;
DELETE FROM stored_files WHERE purpose = 'pdf';

ALTER TABLE projects DROP COLUMN compile_engine;

ALTER TABLE stored_files
    DROP CONSTRAINT stored_files_pdf_clean,
    DROP CONSTRAINT stored_files_pdf_size,
    DROP CONSTRAINT stored_files_project_purpose,
    ADD CONSTRAINT stored_files_project_purpose CHECK ((purpose = 'figure') = (project_id IS NOT NULL));

DROP TRIGGER compile_jobs_release_pdf ON compile_jobs;
DROP FUNCTION compile_job_pdf_released();

DROP INDEX compile_jobs_requested_by_idx;
DROP INDEX compile_jobs_lease_idx;
DROP INDEX compile_jobs_queued_idx;
DROP INDEX compile_jobs_one_active_uq;

CREATE TYPE compile_status AS ENUM ('queued', 'running', 'succeeded', 'failed', 'timed_out');

ALTER TABLE compile_jobs
    DROP CONSTRAINT compile_jobs_log_status,
    DROP CONSTRAINT compile_jobs_pdf_status,
    DROP CONSTRAINT compile_jobs_cancelled,
    DROP CONSTRAINT compile_jobs_failure_status,
    DROP CONSTRAINT compile_jobs_finished,
    DROP CONSTRAINT compile_jobs_running,
    DROP CONSTRAINT compile_jobs_queued,
    DROP CONSTRAINT compile_jobs_log_size,
    DROP CONSTRAINT compile_jobs_pdf_uq,
    DROP CONSTRAINT compile_jobs_pdf_fk,
    DROP COLUMN pdf_file_id,
    DROP COLUMN pages,
    DROP COLUMN live_text_confirmed,
    DROP COLUMN skipped,
    DROP COLUMN errors,
    DROP COLUMN failure,
    DROP COLUMN exit_code,
    DROP COLUMN duration_ms,
    DROP COLUMN lease_until,
    DROP COLUMN worker_id,
    DROP COLUMN main_file_path,
    DROP COLUMN engine,
    ADD COLUMN pdf_storage_key text,
    ALTER COLUMN id SET DEFAULT gen_random_uuid(),
    DROP CONSTRAINT compile_jobs_status,
    ALTER COLUMN status DROP DEFAULT,
    ALTER COLUMN status TYPE compile_status USING status::compile_status,
    ALTER COLUMN status SET DEFAULT 'queued';

CREATE UNIQUE INDEX compile_jobs_one_active_uq ON compile_jobs (project_id) WHERE status IN ('queued', 'running');
