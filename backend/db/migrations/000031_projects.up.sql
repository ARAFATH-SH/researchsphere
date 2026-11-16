-- Research projects (FR-10.1 to FR-10.3, FR-10.6, FR-15.11, AI-1.2; slice 19, chunk 19.1).
-- projects, project_skills and project_members exist since 000001. The owner is the
-- project_members row with role 'owner' (decision 2026-10-09); there is no owner column.
-- docs/decisions.md, 2026-11-16 (research projects).

-- New projects get time-ordered IDs, like messages (uuid_v7 is from 000015).
ALTER TABLE projects ALTER COLUMN id SET DEFAULT uuid_v7();

-- FR-10.1: title 3 to 150 characters after trimming, description at most 5,000, abstract
-- absent or 1 to 3,000. The API trims before it writes.
ALTER TABLE projects
    ADD CONSTRAINT projects_title_length       CHECK (char_length(btrim(title)) BETWEEN 3 AND 150),
    ADD CONSTRAINT projects_description_length CHECK (char_length(description) <= 5000),
    ADD CONSTRAINT projects_abstract_length    CHECK (abstract IS NULL OR char_length(abstract) BETWEEN 1 AND 3000);

-- Project search (FR-10.3): trigram indexes serve "contains" (ILIKE) on title and
-- description and the similar-title match (title %> q).
CREATE INDEX projects_title_trgm_idx       ON projects USING gin (title gin_trgm_ops);
CREATE INDEX projects_description_trgm_idx ON projects USING gin (description gin_trgm_ops);

-- At most 20 required skills per project, checked at commit so a replace (delete, then
-- insert) inside one transaction is judged by its result.
CREATE FUNCTION ensure_project_skills_max() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (SELECT count(*) FROM project_skills WHERE project_id = NEW.project_id) > 20 THEN
        RAISE EXCEPTION 'project % has more than 20 skills', NEW.project_id
            USING ERRCODE = 'check_violation', CONSTRAINT = 'project_skills_max';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER project_skills_max
    AFTER INSERT ON project_skills
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION ensure_project_skills_max();

-- ---------------------------------------------------------------------------
-- Project embeddings (AI-1.2, AI-1.5): the queue accepts projects, and every change to a
-- project's source text (title, description, abstract, required skills) queues it in the
-- same transaction. Deleting a project removes its job; the embedding goes by cascade.
-- ---------------------------------------------------------------------------

ALTER TABLE embedding_jobs DROP CONSTRAINT embedding_jobs_owner_type;
ALTER TABLE embedding_jobs ADD CONSTRAINT embedding_jobs_owner_type CHECK (owner_type IN ('profile', 'project'));

CREATE FUNCTION embedding_enqueue_project() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM embedding_enqueue('project', NEW.id);
    RETURN NULL;
END;
$$;

-- A skill row: queue its project, unless the project itself is being deleted (the cascade
-- removes its skills after the project row is gone).
CREATE FUNCTION embedding_enqueue_project_skill() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    pid uuid;
BEGIN
    IF TG_OP = 'DELETE' THEN
        pid := OLD.project_id;
    ELSE
        pid := NEW.project_id;
    END IF;
    IF EXISTS (SELECT 1 FROM projects WHERE id = pid) THEN
        PERFORM embedding_enqueue('project', pid);
    END IF;
    RETURN NULL;
END;
$$;

CREATE FUNCTION embedding_drop_project_job() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM embedding_jobs WHERE owner_type = 'project' AND owner_id = OLD.id;
    RETURN NULL;
END;
$$;

CREATE TRIGGER projects_embedding_insert AFTER INSERT ON projects
    FOR EACH ROW EXECUTE FUNCTION embedding_enqueue_project();
-- Only a real change of the text queues: a status change, or a save with the same text,
-- does not.
CREATE TRIGGER projects_embedding_update AFTER UPDATE OF title, description, abstract ON projects
    FOR EACH ROW
    WHEN (OLD.title IS DISTINCT FROM NEW.title
          OR OLD.description IS DISTINCT FROM NEW.description
          OR OLD.abstract IS DISTINCT FROM NEW.abstract)
    EXECUTE FUNCTION embedding_enqueue_project();
CREATE TRIGGER project_skills_embedding AFTER INSERT OR DELETE ON project_skills
    FOR EACH ROW EXECUTE FUNCTION embedding_enqueue_project_skill();
CREATE TRIGGER projects_embedding_delete AFTER DELETE ON projects
    FOR EACH ROW EXECUTE FUNCTION embedding_drop_project_job();

-- ---------------------------------------------------------------------------
-- Project reports (FR-15.11). A verified user reports a project they do not own; an
-- administrator lists, reads and resolves them. Like message_reports, but the target is a
-- project. Deleting the project deletes its reports.
-- ---------------------------------------------------------------------------

CREATE TABLE project_reports (
    id              uuid        PRIMARY KEY DEFAULT uuid_v7(),
    project_id      uuid        NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    reporter_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    reason          text        NOT NULL,
    details         text,
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    resolved_at     timestamptz,
    resolved_by     uuid        REFERENCES users (id) ON DELETE SET NULL,
    outcome         text,
    resolution_note text,
    CONSTRAINT project_reports_reason CHECK (reason IN ('spam', 'harassment', 'inappropriate', 'other')),
    CONSTRAINT project_reports_details_length CHECK (char_length(details) <= 1000),
    CONSTRAINT project_reports_outcome CHECK (outcome IN ('dismissed', 'action_taken')),
    CONSTRAINT project_reports_note_length CHECK (char_length(resolution_note) <= 1000),
    CONSTRAINT project_reports_resolved CHECK ((resolved_at IS NULL) = (outcome IS NULL)),
    CONSTRAINT project_reports_open_has_no_resolution CHECK (
        resolved_at IS NOT NULL OR (resolved_by IS NULL AND resolution_note IS NULL))
);
-- One open report per reporter per project.
CREATE UNIQUE INDEX project_reports_open_uq ON project_reports (project_id, reporter_id) WHERE resolved_at IS NULL;
-- The open and resolved lists, newest first.
CREATE INDEX project_reports_status_idx ON project_reports ((resolved_at IS NULL), created_at DESC, id DESC);
CREATE INDEX project_reports_reporter_idx ON project_reports (reporter_id);
CREATE INDEX project_reports_resolved_by_idx ON project_reports (resolved_by);
