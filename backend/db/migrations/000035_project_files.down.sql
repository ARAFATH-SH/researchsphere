-- Undoes 000035. Figures cannot be expressed in the old shape (a storage key without a
-- stored_files row, and a user as every stored file's owner), so their rows go first; their
-- objects are queued for deletion by the stored_files trigger of 000007.

DROP TRIGGER project_files_delete_file ON project_files;
DROP FUNCTION project_file_deleted();
DELETE FROM stored_files WHERE project_id IS NOT NULL;
DELETE FROM project_files WHERE kind = 'binary';

DROP TRIGGER project_files_max ON project_files;
DROP FUNCTION ensure_project_files_max();
DROP TRIGGER project_files_parent ON project_files;
DROP FUNCTION ensure_project_file_tree();
DROP INDEX project_files_path_uq;
DROP INDEX project_files_updated_by_idx;

ALTER TABLE project_files
    DROP CONSTRAINT project_files_folder,
    DROP CONSTRAINT project_files_binary,
    DROP CONSTRAINT project_files_text,
    DROP CONSTRAINT project_files_path_check,
    DROP CONSTRAINT project_files_stored_file_key,
    DROP CONSTRAINT project_files_stored_file_fk,
    DROP COLUMN updated_by,
    DROP COLUMN version,
    DROP COLUMN stored_file_id,
    ALTER COLUMN id SET DEFAULT gen_random_uuid(),
    ADD COLUMN storage_key text,
    ADD CONSTRAINT project_files_path_check CHECK (
        length(path) BETWEEN 1 AND 255
        AND path ~ '^[A-Za-z0-9._ -]+(/[A-Za-z0-9._ -]+)*$'
        AND path !~ '(^|/)\.{1,2}(/|$)'),
    ADD CONSTRAINT project_files_project_id_path_key UNIQUE (project_id, path),
    ADD CONSTRAINT project_files_check CHECK (kind <> 'text' OR storage_key IS NULL),
    ADD CONSTRAINT project_files_check1 CHECK (kind <> 'binary' OR (storage_key IS NOT NULL AND content IS NULL AND ydoc_state IS NULL)),
    ADD CONSTRAINT project_files_check2 CHECK (kind <> 'folder' OR (storage_key IS NULL AND content IS NULL AND ydoc_state IS NULL AND size_bytes = 0)),
    ADD CONSTRAINT project_files_check3 CHECK (kind <> 'binary' OR (size_bytes <= 5242880 AND path ~* '\.(png|jpe?g|pdf)$'));

DROP INDEX stored_files_uploaded_by_idx;
DROP INDEX stored_files_project_idx;
ALTER TABLE stored_files
    DROP CONSTRAINT stored_files_scanned,
    ADD CONSTRAINT stored_files_scanned CHECK ((scan_status = 'pending') = (scanned_at IS NULL)
                                               OR purpose <> 'attachment'),
    DROP CONSTRAINT stored_files_id_project_key,
    DROP CONSTRAINT stored_files_project_purpose,
    DROP CONSTRAINT stored_files_one_owner,
    DROP COLUMN uploaded_by,
    DROP COLUMN project_id,
    ALTER COLUMN owner_user_id SET NOT NULL;
