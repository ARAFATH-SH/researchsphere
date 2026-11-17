-- Figure limits and direct uploads (FR-12.2; docs/decisions.md, 2026-11-18).
--
-- A figure may now be up to 50 MB by default; the limit is a setting (FIGURE_MAX_BYTES), so
-- the database holds only the hard ceiling every environment refuses to go over: 200 MiB.
-- The project's storage quota (PROJECT_STORAGE_MAX_BYTES) is a setting too and is checked
-- by the API under the project's lock.

ALTER TABLE stored_files
    DROP CONSTRAINT stored_files_figure_size,
    ADD CONSTRAINT stored_files_figure_size CHECK (purpose <> 'figure' OR size_bytes <= 209715200);

ALTER TABLE project_files
    DROP CONSTRAINT project_files_binary,
    ADD CONSTRAINT project_files_binary CHECK (kind <> 'binary' OR (
        stored_file_id IS NOT NULL AND content IS NULL AND ydoc_state IS NULL
        AND size_bytes BETWEEN 1 AND 209715200 AND path ~* '\.(png|jpe?g|pdf)$'));

-- An upload the browser sends straight to the bucket with a presigned POST, until the API
-- confirms it. The object waits at object_key; confirming moves it to the figure's own key
-- and deletes this row. A row that expires unconfirmed is deleted by the API's sweep. Every
-- deletion, also by cascade, queues the object in storage_deletions (000007), so an upload
-- that was abandoned, or whose project or user went, leaves nothing in the bucket.
CREATE TABLE project_figure_uploads (
    id         uuid PRIMARY KEY DEFAULT uuid_v7(),
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    object_key text NOT NULL CONSTRAINT project_figure_uploads_object_key_key UNIQUE
               CONSTRAINT project_figure_uploads_object_key_check
               CHECK (object_key ~ '^projects/[0-9a-f-]{36}/uploads/[0-9a-f-]{36}$'),
    -- Where the figure goes; checked again when it is confirmed.
    path       text NOT NULL CONSTRAINT project_figure_uploads_path_check CHECK (char_length(path) BETWEEN 1 AND 255),
    -- The size the presigned POST allows, exactly.
    size_bytes bigint NOT NULL CONSTRAINT project_figure_uploads_size_check CHECK (size_bytes BETWEEN 1 AND 209715200),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    CONSTRAINT project_figure_uploads_expiry CHECK (expires_at > created_at)
);
CREATE INDEX project_figure_uploads_project_idx ON project_figure_uploads (project_id);
CREATE INDEX project_figure_uploads_user_idx ON project_figure_uploads (user_id);
CREATE INDEX project_figure_uploads_expires_idx ON project_figure_uploads (expires_at);

CREATE TRIGGER project_figure_uploads_queue_deletion AFTER DELETE ON project_figure_uploads
    FOR EACH ROW EXECUTE FUNCTION queue_stored_file_deletion();
