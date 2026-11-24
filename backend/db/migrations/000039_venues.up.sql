-- Venues (FR-14.1 to FR-14.5, FR-15.1, AI-1.3 for venues; slice 24, chunk 24.1).
-- docs/decisions.md, 2026-11-24 (venues).
--
-- venues exists since 000001 and nothing ever wrote it. Its shape does not fit FR-14 as
-- built here (one free-text field, a date deadline with no time zone, no https rule, no soft
-- delete), so it is reshaped in place. This refuses to run on a table with rows, so no data
-- is ever changed silently.

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM venues) THEN
        RAISE EXCEPTION '000039 reshapes venues and needs it empty; it has rows';
    END IF;
END;
$$;

-- ---------------------------------------------------------------------------
-- The fixed list of research fields (FR-14.3). Rows, not a CHECK, so that a later admin
-- screen can add one without a migration. The keys are the 20 sample research clusters
-- (cmd/seedsample/content/clusters.json) with broader labels, plus "other", so that a real
-- venue never has to be forced into a wrong field.
-- ---------------------------------------------------------------------------

CREATE TABLE research_fields (
    key        text        PRIMARY KEY,
    label      text        NOT NULL UNIQUE,
    position   smallint    NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT research_fields_key   CHECK (key ~ '^[a-z][a-z0-9_]{1,40}$'),
    CONSTRAINT research_fields_label CHECK (char_length(label) BETWEEN 1 AND 100 AND label = btrim(label))
);

INSERT INTO research_fields (key, label, position) VALUES
    ('medical_imaging',            'Medical imaging',                              1),
    ('explainable_ai',             'Artificial intelligence and machine learning', 2),
    ('bangla_nlp',                 'Natural language processing',                  3),
    ('climate_modelling',          'Climate science',                              4),
    ('public_health_epidemiology', 'Public health and epidemiology',               5),
    ('materials_science',          'Materials science',                            6),
    ('robotics',                   'Robotics and automation',                      7),
    ('agri_remote_sensing',        'Agriculture and remote sensing',               8),
    ('renewable_energy',           'Energy and power systems',                     9),
    ('water_resources',            'Water resources and hydrology',               10),
    ('cybersecurity',              'Cybersecurity and privacy',                   11),
    ('genomics_bioinformatics',    'Genomics and bioinformatics',                 12),
    ('development_economics',      'Economics and development',                   13),
    ('education_technology',       'Education and learning technology',           14),
    ('structural_engineering',     'Civil and structural engineering',            15),
    ('urban_transport',            'Transportation and urban planning',           16),
    ('marine_fisheries',           'Marine science and fisheries',                17),
    ('wireless_networks',          'Networks and communications',                 18),
    ('drug_discovery',             'Pharmaceutical sciences',                     19),
    ('mental_health',              'Psychology and mental health',                20),
    ('other',                      'Other',                                       99);

-- ---------------------------------------------------------------------------
-- The venue itself.
-- ---------------------------------------------------------------------------

DROP INDEX venues_deadline_idx;
DROP INDEX venues_field_idx;
ALTER TABLE venues
    DROP CONSTRAINT venues_source_source_ref_key,
    DROP CONSTRAINT venues_check,
    DROP CONSTRAINT venues_name_check,
    DROP COLUMN field,
    DROP COLUMN url,
    DROP COLUMN location,
    DROP COLUMN source_ref;

-- A text type with a CHECK, like moderation_actions: PostgreSQL cannot use an enum value in
-- the transaction that adds it, and special_issue is new.
ALTER TABLE venues ALTER COLUMN type TYPE text USING type::text;
DROP TYPE venue_type;

-- New venues get time-ordered IDs, like projects.
ALTER TABLE venues ALTER COLUMN id SET DEFAULT uuid_v7();
ALTER TABLE venues ALTER COLUMN source SET DEFAULT 'manual';

-- The deadline is an instant; deadline_tz is the label it was given in (AoE = UTC-12 with no
-- daylight saving, UTC, or an IANA zone name such as Asia/Dhaka). The API checks the zone
-- name against the time zone database; the CHECK keeps the shape.
ALTER TABLE venues
    ALTER COLUMN submission_deadline TYPE timestamptz USING submission_deadline::timestamptz,
    ALTER COLUMN submission_deadline SET NOT NULL,
    ADD COLUMN short_name        text,
    ADD COLUMN city              text,
    ADD COLUMN country           text,
    ADD COLUMN attendance        text,
    ADD COLUMN deadline_tz       text        NOT NULL,
    ADD COLUMN abstract_deadline timestamptz,
    ADD COLUMN notification_at   timestamptz,
    ADD COLUMN official_url      text        NOT NULL,
    ADD COLUMN source_key        text,
    ADD COLUMN created_by        uuid        REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN removed_at        timestamptz,
    ADD COLUMN removed_by        uuid        REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN removal_reason    text,
    -- One line, trimmed, no control characters.
    ADD CONSTRAINT venues_name CHECK (
        char_length(name) BETWEEN 1 AND 300 AND name = btrim(name) AND name !~ '[[:cntrl:]]'),
    ADD CONSTRAINT venues_short_name CHECK (
        char_length(short_name) BETWEEN 1 AND 40 AND short_name = btrim(short_name) AND short_name !~ '[[:cntrl:]]'),
    ADD CONSTRAINT venues_type CHECK (type IN ('conference', 'journal', 'workshop', 'special_issue')),
    -- Plain text or a small Markdown subset, shown as escaped text (NFR-8.2). Line feeds
    -- and tabs are the only control characters allowed; the API turns \r\n into \n.
    ADD CONSTRAINT venues_scope CHECK (
        char_length(scope) BETWEEN 1 AND 10000 AND scope !~ '[\x01-\x08\x0b-\x1f\x7f]'),
    ADD CONSTRAINT venues_city CHECK (
        char_length(city) BETWEEN 1 AND 100 AND city = btrim(city) AND city !~ '[[:cntrl:]]'),
    ADD CONSTRAINT venues_country CHECK (country ~ '^[A-Z]{2}$'),
    -- A conference or workshop takes place in person, online or both; a journal or special
    -- issue has no attendance, place or event dates. A place is needed unless online only.
    ADD CONSTRAINT venues_attendance CHECK (
        CASE WHEN type IN ('conference', 'workshop')
             THEN attendance IN ('in_person', 'online', 'hybrid')
             ELSE attendance IS NULL END),
    ADD CONSTRAINT venues_place CHECK (
        CASE WHEN attendance IN ('in_person', 'hybrid') THEN city IS NOT NULL AND country IS NOT NULL
             ELSE city IS NULL AND country IS NULL END),
    ADD CONSTRAINT venues_event_dates CHECK (
        type IN ('conference', 'workshop') OR (starts_on IS NULL AND ends_on IS NULL)),
    ADD CONSTRAINT venues_deadline_tz CHECK (
        deadline_tz ~ '^(AoE|UTC|[A-Za-z][A-Za-z_-]*(/[A-Za-z0-9_+-]+){1,2})$'),
    ADD CONSTRAINT venues_abstract_deadline CHECK (abstract_deadline <= submission_deadline),
    ADD CONSTRAINT venues_notification CHECK (notification_at >= submission_deadline),
    ADD CONSTRAINT venues_official_url CHECK (
        char_length(official_url) <= 2048 AND official_url ~ '^https://[^/?#@[:space:]]+([/?#][^[:space:]]*)?$'),
    ADD CONSTRAINT venues_source CHECK (source IN ('manual', 'import')),
    -- An imported venue always has its key; re-importing it updates, never duplicates (FR-14.5).
    ADD CONSTRAINT venues_source_key CHECK (
        (source = 'import') = (source_key IS NOT NULL) AND char_length(source_key) BETWEEN 1 AND 200),
    -- A removal always has its reason (FR-15.1); removed_by goes null if that account is deleted.
    ADD CONSTRAINT venues_removal CHECK ((removed_at IS NULL) = (removal_reason IS NULL)),
    ADD CONSTRAINT venues_removal_reason CHECK (char_length(removal_reason) BETWEEN 10 AND 1000);

CREATE UNIQUE INDEX venues_import_key_idx ON venues (source_key) WHERE source = 'import';
-- The list (FR-14.1, FR-14.2): live venues by deadline.
CREATE INDEX venues_deadline_idx ON venues (submission_deadline, name, id) WHERE removed_at IS NULL;
-- Similar names and acronyms (q): word_similarity through %>.
CREATE INDEX venues_name_trgm_idx       ON venues USING gin (name gin_trgm_ops);
CREATE INDEX venues_short_name_trgm_idx ON venues USING gin (short_name gin_trgm_ops);

-- A venue's fields: 1 to 5, from research_fields.
CREATE TABLE venue_fields (
    venue_id  uuid NOT NULL REFERENCES venues (id) ON DELETE CASCADE,
    field_key text NOT NULL REFERENCES research_fields (key) ON DELETE RESTRICT,
    PRIMARY KEY (venue_id, field_key)
);
CREATE INDEX venue_fields_field_idx ON venue_fields (field_key, venue_id);

-- At least one field and at most 5, checked at commit so that a replace (delete, then
-- insert) and a new venue with its fields are judged by the result. A venue deleted in the
-- same transaction (sample purge) is not checked.
CREATE FUNCTION ensure_venue_field_count() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    vid uuid;
    n   integer;
BEGIN
    IF TG_TABLE_NAME = 'venues' THEN
        vid := NEW.id;
    ELSIF TG_OP = 'DELETE' THEN
        vid := OLD.venue_id;
    ELSE
        vid := NEW.venue_id;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM venues WHERE id = vid) THEN
        RETURN NULL;
    END IF;
    SELECT count(*) INTO n FROM venue_fields WHERE venue_id = vid;
    IF n < 1 OR n > 5 THEN
        RAISE EXCEPTION 'venue % has % fields; it needs 1 to 5', vid, n
            USING ERRCODE = 'check_violation', CONSTRAINT = 'venue_fields_count';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER venues_field_count
    AFTER INSERT ON venues
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION ensure_venue_field_count();
CREATE CONSTRAINT TRIGGER venue_fields_count
    AFTER INSERT OR DELETE ON venue_fields
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION ensure_venue_field_count();

-- ---------------------------------------------------------------------------
-- Venue embeddings (AI-1.3, AI-1.5): the queue accepts venues, and every change to a live
-- venue's source text (name, short name, scope, fields) queues it in the same transaction.
-- A removal deletes the embedding and the job at once, so a removed venue leaves the AI
-- index; deleting a venue (sample purge only) drops its job, the embedding goes by cascade.
-- ---------------------------------------------------------------------------

ALTER TABLE embedding_jobs DROP CONSTRAINT embedding_jobs_owner_type;
ALTER TABLE embedding_jobs ADD CONSTRAINT embedding_jobs_owner_type CHECK (owner_type IN ('profile', 'project', 'venue'));

CREATE FUNCTION embedding_enqueue_venue() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM embedding_enqueue('venue', NEW.id);
    RETURN NULL;
END;
$$;

CREATE FUNCTION embedding_enqueue_venue_field() RETURNS trigger
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

CREATE FUNCTION embedding_drop_venue() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        DELETE FROM venue_embeddings WHERE venue_id = NEW.id;
        DELETE FROM embedding_jobs WHERE owner_type = 'venue' AND owner_id = NEW.id;
    ELSE
        DELETE FROM embedding_jobs WHERE owner_type = 'venue' AND owner_id = OLD.id;
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER venues_embedding_insert AFTER INSERT ON venues
    FOR EACH ROW EXECUTE FUNCTION embedding_enqueue_venue();
-- Only a real change of the text of a live venue queues: dates, links or a save with the
-- same text do not.
CREATE TRIGGER venues_embedding_update AFTER UPDATE OF name, short_name, scope ON venues
    FOR EACH ROW
    WHEN (NEW.removed_at IS NULL
          AND (OLD.name IS DISTINCT FROM NEW.name
               OR OLD.short_name IS DISTINCT FROM NEW.short_name
               OR OLD.scope IS DISTINCT FROM NEW.scope))
    EXECUTE FUNCTION embedding_enqueue_venue();
CREATE TRIGGER venue_fields_embedding AFTER INSERT OR DELETE ON venue_fields
    FOR EACH ROW EXECUTE FUNCTION embedding_enqueue_venue_field();
CREATE TRIGGER venues_embedding_removed AFTER UPDATE OF removed_at ON venues
    FOR EACH ROW
    WHEN (OLD.removed_at IS NULL AND NEW.removed_at IS NOT NULL)
    EXECUTE FUNCTION embedding_drop_venue();
CREATE TRIGGER venues_embedding_delete AFTER DELETE ON venues
    FOR EACH ROW EXECUTE FUNCTION embedding_drop_venue();

-- ---------------------------------------------------------------------------
-- What administrators did to venues (FR-15.1, FR-14.5): one row per change, written in the
-- transaction of the change, never updated. The administrator's ID goes null when that
-- account is deleted and admin_name keeps who it was, so the trail outlives the account.
-- changed_fields names the fields, never their values. One import's rows share import_id.
-- A venue is never deleted except by the sample purge, whose venues take their rows along.
-- ---------------------------------------------------------------------------

CREATE TABLE venue_actions (
    id             uuid        PRIMARY KEY DEFAULT uuid_v7(),
    venue_id       uuid        NOT NULL REFERENCES venues (id) ON DELETE CASCADE,
    admin_id       uuid        REFERENCES users (id) ON DELETE SET NULL,
    admin_name     text        NOT NULL,
    action         text        NOT NULL,
    reason         text,
    changed_fields text[]      NOT NULL DEFAULT '{}',
    import_id      uuid,
    created_at     timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT venue_actions_action CHECK (
        action IN ('create', 'update', 'remove', 'import_create', 'import_update')),
    CONSTRAINT venue_actions_admin_name CHECK (char_length(admin_name) BETWEEN 1 AND 320),
    CONSTRAINT venue_actions_reason CHECK (
        (action = 'remove') = (reason IS NOT NULL) AND char_length(reason) BETWEEN 10 AND 1000),
    CONSTRAINT venue_actions_import CHECK (
        (action IN ('import_create', 'import_update')) = (import_id IS NOT NULL))
);
CREATE INDEX venue_actions_venue_idx ON venue_actions (venue_id, created_at DESC, id DESC);
CREATE INDEX venue_actions_admin_idx ON venue_actions (admin_id);

-- ---------------------------------------------------------------------------
-- Sample venues (slice 16's rules): the marker of make sample-data venues=1. No code under
-- internal/ reads it; the sample purge deletes the venues it lists.
-- ---------------------------------------------------------------------------

CREATE TABLE sample_venues (
    venue_id   uuid        PRIMARY KEY REFERENCES venues (id) ON DELETE CASCADE,
    seq        integer     NOT NULL UNIQUE,
    seed       bigint      NOT NULL,
    cluster    text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sample_venues_seq     CHECK (seq >= 0),
    CONSTRAINT sample_venues_cluster CHECK (cluster ~ '^[a-z][a-z0-9_]{1,40}$')
);
