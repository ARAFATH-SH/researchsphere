-- Project files (FR-10.4, FR-12.1, FR-12.2, FR-12.6 server half; slice 21, chunk 21.1).
-- docs/decisions.md, 2026-11-17 (project files).
--
-- project_files exists since 000001 and no code wrote it before this slice, so its rules are
-- tightened here without touching data. Text files keep their text in `content`, the one
-- source every reader uses (REST, compile, ZIP, assistant). `ydoc_state` (000001) is left for
-- the sync server of slice 22, which keeps the Yjs document state there beside the text it
-- writes; a REST save clears it. Figures are objects in the bucket with a stored_files row
-- owned by the project, so a member's account deletion never takes a project's figure.

-- ---------------------------------------------------------------------------
-- stored_files: a file may belong to a project instead of a user. A figure (FR-12.2) always
-- does; slice 23 adds the compiled PDF. uploaded_by keeps who uploaded it, for audit only.
-- ---------------------------------------------------------------------------
ALTER TABLE stored_files
    ALTER COLUMN owner_user_id DROP NOT NULL,
    ADD COLUMN project_id  uuid REFERENCES projects (id) ON DELETE CASCADE,
    ADD COLUMN uploaded_by uuid REFERENCES users (id) ON DELETE SET NULL,
    ADD CONSTRAINT stored_files_one_owner CHECK ((owner_user_id IS NULL) <> (project_id IS NULL)),
    ADD CONSTRAINT stored_files_project_purpose CHECK ((purpose = 'figure') = (project_id IS NOT NULL)),
    ADD CONSTRAINT stored_files_id_project_key UNIQUE (id, project_id),
    -- A figure, like an attachment, records when its scan ended.
    DROP CONSTRAINT stored_files_scanned,
    ADD CONSTRAINT stored_files_scanned CHECK ((scan_status = 'pending') = (scanned_at IS NULL)
                                               OR purpose NOT IN ('attachment', 'figure'));
CREATE INDEX stored_files_project_idx ON stored_files (project_id) WHERE project_id IS NOT NULL;
CREATE INDEX stored_files_uploaded_by_idx ON stored_files (uploaded_by) WHERE uploaded_by IS NOT NULL;

-- ---------------------------------------------------------------------------
-- project_files
-- ---------------------------------------------------------------------------
ALTER TABLE project_files
    DROP CONSTRAINT project_files_path_check,
    DROP CONSTRAINT project_files_check,
    DROP CONSTRAINT project_files_check1,
    DROP CONSTRAINT project_files_check2,
    DROP CONSTRAINT project_files_check3,
    DROP CONSTRAINT project_files_project_id_path_key,
    DROP COLUMN storage_key,
    ALTER COLUMN id SET DEFAULT uuid_v7(),
    ADD COLUMN stored_file_id uuid,
    -- Raised by every change of the text, by REST and (slice 22) by the sync server: a save
    -- names the version it started from and is refused when it moved on.
    ADD COLUMN version    integer NOT NULL DEFAULT 1 CONSTRAINT project_files_version_check CHECK (version >= 1),
    ADD COLUMN updated_by uuid REFERENCES users (id) ON DELETE SET NULL,
    -- A figure is a stored file of the same project. Deleting the stored file (only its
    -- project's deletion does) takes the row with it.
    ADD CONSTRAINT project_files_stored_file_fk FOREIGN KEY (stored_file_id, project_id)
        REFERENCES stored_files (id, project_id) ON DELETE CASCADE,
    ADD CONSTRAINT project_files_stored_file_key UNIQUE (stored_file_id),
    -- Paths: segments of letters, digits, '_', '.' and '-' that do not start with '.', so no
    -- '.' or '..', no hidden files, no spaces (LaTeX's \input and \includegraphics handle
    -- them badly); 1 to 100 characters a segment, at most 8 segments, at most 255 in all; no
    -- leading, trailing or double '/'.
    ADD CONSTRAINT project_files_path_check CHECK (
        char_length(path) BETWEEN 1 AND 255
        AND path ~ '^[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}(/[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}){0,7}$'),
    -- A text file: its text in the database, at most 1 MiB of UTF-8, size_bytes its length.
    ADD CONSTRAINT project_files_text CHECK (kind <> 'text' OR (
        stored_file_id IS NULL AND content IS NOT NULL
        AND octet_length(content) <= 1048576 AND size_bytes = octet_length(content)
        AND path !~* '\.(png|jpe?g|pdf)$')),
    -- A figure: PNG, JPG or PDF of up to 5 MB in the bucket (FR-12.2).
    ADD CONSTRAINT project_files_binary CHECK (kind <> 'binary' OR (
        stored_file_id IS NOT NULL AND content IS NULL AND ydoc_state IS NULL
        AND size_bytes BETWEEN 1 AND 5242880 AND path ~* '\.(png|jpe?g|pdf)$')),
    ADD CONSTRAINT project_files_folder CHECK (kind <> 'folder' OR (
        stored_file_id IS NULL AND content IS NULL AND ydoc_state IS NULL AND size_bytes = 0));

-- One entry per path in a project, ignoring letter case (two files that differ only in case
-- would collide on a case-insensitive file system and confuse LaTeX users).
CREATE UNIQUE INDEX project_files_path_uq ON project_files (project_id, lower(path));

-- The tree: every entry's parent is a folder of the same project, and no entry is left
-- under a path that is gone or no longer a folder. Checked at the end of each statement, so
-- one UPDATE that moves a folder with everything in it, or one DELETE of a folder and its
-- contents, is judged by its result. Deferrable for a caller that needs several statements.
CREATE FUNCTION ensure_project_file_tree() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    parent text;
BEGIN
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        IF position('/' IN NEW.path) > 0 THEN
            parent := regexp_replace(NEW.path, '/[^/]+$', '');
            IF NOT EXISTS (SELECT 1 FROM project_files f
                           WHERE f.project_id = NEW.project_id AND lower(f.path) = lower(parent)
                             AND f.kind = 'folder') THEN
                RAISE EXCEPTION 'project file % has no parent folder', NEW.path
                    USING ERRCODE = 'check_violation', CONSTRAINT = 'project_files_parent';
            END IF;
        END IF;
    END IF;
    IF TG_OP = 'DELETE' OR (TG_OP = 'UPDATE' AND (OLD.path <> NEW.path OR OLD.kind <> NEW.kind)) THEN
        -- left() rather than LIKE: '_' is a wildcard of LIKE and a path character here.
        IF EXISTS (SELECT 1 FROM project_files f
                   WHERE f.project_id = OLD.project_id
                     AND left(lower(f.path), char_length(OLD.path) + 1) = lower(OLD.path) || '/'
                     AND NOT EXISTS (SELECT 1 FROM project_files p
                                     WHERE p.project_id = OLD.project_id AND p.kind = 'folder'
                                       AND lower(p.path) = lower(OLD.path))) THEN
            RAISE EXCEPTION 'project files are left under %', OLD.path
                USING ERRCODE = 'check_violation', CONSTRAINT = 'project_files_parent';
        END IF;
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER project_files_parent
    AFTER INSERT OR UPDATE OF path, kind OR DELETE ON project_files
    DEFERRABLE INITIALLY IMMEDIATE
    FOR EACH ROW EXECUTE FUNCTION ensure_project_file_tree();

-- At most 200 entries per project, folders included.
CREATE FUNCTION ensure_project_files_max() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (SELECT count(*) FROM project_files WHERE project_id = NEW.project_id) > 200 THEN
        RAISE EXCEPTION 'project % has more than 200 files', NEW.project_id
            USING ERRCODE = 'check_violation', CONSTRAINT = 'project_files_max';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER project_files_max
    AFTER INSERT ON project_files
    DEFERRABLE INITIALLY IMMEDIATE
    FOR EACH ROW EXECUTE FUNCTION ensure_project_files_max();

-- A figure row that goes takes its stored file with it; that row's trigger (000007) queues
-- the object for the cleanup worker. As message_attachments (000024).
CREATE FUNCTION project_file_deleted() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.stored_file_id IS NOT NULL THEN
        DELETE FROM stored_files WHERE id = OLD.stored_file_id;
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER project_files_delete_file
    AFTER DELETE ON project_files
    FOR EACH ROW EXECUTE FUNCTION project_file_deleted();

CREATE INDEX project_files_updated_by_idx ON project_files (updated_by) WHERE updated_by IS NOT NULL;
