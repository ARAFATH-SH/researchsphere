-- Undo 000039. This deletes every venue (and by cascade its embedding, fields and audit
-- rows): the old shape cannot hold an instant deadline, several fields or a removal.

DROP TABLE sample_venues;
DROP TABLE venue_actions;

DROP TRIGGER venues_embedding_delete ON venues;
DROP TRIGGER venues_embedding_removed ON venues;
DROP TRIGGER venue_fields_embedding ON venue_fields;
DROP TRIGGER venues_embedding_update ON venues;
DROP TRIGGER venues_embedding_insert ON venues;
DROP FUNCTION embedding_drop_venue();
DROP FUNCTION embedding_enqueue_venue_field();
DROP FUNCTION embedding_enqueue_venue();

DELETE FROM embedding_jobs WHERE owner_type = 'venue';
ALTER TABLE embedding_jobs DROP CONSTRAINT embedding_jobs_owner_type;
ALTER TABLE embedding_jobs ADD CONSTRAINT embedding_jobs_owner_type CHECK (owner_type IN ('profile', 'project'));

-- The field-count triggers go before the rows: a delete would leave their checks pending,
-- and a table with pending trigger events cannot be dropped.
DROP TRIGGER venue_fields_count ON venue_fields;
DROP TRIGGER venues_field_count ON venues;
DROP FUNCTION ensure_venue_field_count();

DELETE FROM venues;
DROP TABLE venue_fields;

DROP INDEX venues_short_name_trgm_idx;
DROP INDEX venues_name_trgm_idx;
DROP INDEX venues_deadline_idx;
DROP INDEX venues_import_key_idx;

ALTER TABLE venues
    DROP COLUMN short_name,
    DROP COLUMN city,
    DROP COLUMN country,
    DROP COLUMN attendance,
    DROP COLUMN deadline_tz,
    DROP COLUMN abstract_deadline,
    DROP COLUMN notification_at,
    DROP COLUMN official_url,
    DROP COLUMN source_key,
    DROP COLUMN created_by,
    DROP COLUMN removed_at,
    DROP COLUMN removed_by,
    DROP COLUMN removal_reason,
    DROP CONSTRAINT venues_type,
    DROP CONSTRAINT venues_source;
ALTER TABLE venues DROP CONSTRAINT IF EXISTS venues_name;
ALTER TABLE venues DROP CONSTRAINT IF EXISTS venues_scope;
ALTER TABLE venues DROP CONSTRAINT IF EXISTS venues_event_dates;

ALTER TABLE venues
    ALTER COLUMN submission_deadline DROP NOT NULL,
    ALTER COLUMN submission_deadline TYPE date USING submission_deadline::date,
    ALTER COLUMN source SET DEFAULT 'admin',
    ALTER COLUMN id SET DEFAULT gen_random_uuid();

CREATE TYPE venue_type AS ENUM ('conference', 'journal', 'workshop');
ALTER TABLE venues ALTER COLUMN type TYPE venue_type USING type::venue_type;

ALTER TABLE venues
    ADD COLUMN field      text,
    ADD COLUMN url        text,
    ADD COLUMN location   text,
    ADD COLUMN source_ref text,
    ADD CONSTRAINT venues_name_check CHECK (length(btrim(name)) > 0),
    ADD CONSTRAINT venues_source_source_ref_key UNIQUE (source, source_ref),
    ADD CONSTRAINT venues_check CHECK ((source = 'admin') = (source_ref IS NULL));

CREATE INDEX venues_deadline_idx ON venues (submission_deadline);
CREATE INDEX venues_field_idx    ON venues (lower(field), submission_deadline);

DROP TABLE research_fields;
