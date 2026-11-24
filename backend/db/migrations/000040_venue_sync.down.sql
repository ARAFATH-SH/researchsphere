-- Undo 000040. Venues from the synced sources, and rolling journals, cannot exist in the
-- 000039 shape, so they are deleted (with their fields, embeddings and audit rows, by
-- cascade). Venues added by hand or by a file import stay.

CREATE OR REPLACE FUNCTION embedding_enqueue_venue_field() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    vid uuid;
BEGIN
    IF TG_OP = 'DELETE' THEN
        vid := OLD.venue_id;
    ELSE
        vid := NEW.venue_id;
    END IF;
    IF EXISTS (SELECT 1 FROM venues WHERE id = vid AND removed_at IS NULL) THEN
        PERFORM embedding_enqueue('venue', vid);
    END IF;
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION embedding_enqueue_venue() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM embedding_enqueue('venue', NEW.id);
    RETURN NULL;
END;
$$;

DROP TABLE venue_sync_link_checks;
DROP TABLE venue_sync_runs;
DROP TABLE venue_sync_files;
DROP TABLE venue_sync_sources;

-- The field-count trigger skips a venue deleted in the same transaction.
DELETE FROM venues WHERE source IN ('ccfddl', 'hci_deadlines', 'openalex') OR rolling;

ALTER TABLE venue_actions
    DROP CONSTRAINT venue_actions_actor,
    DROP COLUMN actor;

ALTER TABLE venues
    DROP CONSTRAINT venues_locked,
    DROP COLUMN locked_at,
    DROP CONSTRAINT venues_link_kind,
    DROP COLUMN link_kind;

DROP INDEX venues_rolling_idx;
ALTER TABLE venues
    DROP CONSTRAINT venues_rolling,
    DROP COLUMN rolling,
    ALTER COLUMN submission_deadline SET NOT NULL,
    ALTER COLUMN deadline_tz SET NOT NULL;

DROP INDEX venues_source_key_idx;
ALTER TABLE venues
    DROP CONSTRAINT venues_source,
    DROP CONSTRAINT venues_source_key,
    ADD CONSTRAINT venues_source CHECK (source IN ('manual', 'import')),
    ADD CONSTRAINT venues_source_key CHECK (
        (source = 'import') = (source_key IS NOT NULL) AND char_length(source_key) BETWEEN 1 AND 200);
CREATE UNIQUE INDEX venues_import_key_idx ON venues (source_key) WHERE source = 'import';
