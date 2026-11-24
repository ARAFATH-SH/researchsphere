-- Automatic venue import (FR-14.5; slice 25A, docs/venues-sync.md, docs/decisions.md
-- 2026-11-24). Conferences come daily from open deadline repositories (ccf-deadlines,
-- hci-deadlines), journals from OpenAlex. A venue is upserted by source + source_key; one
-- an administrator added by hand, or edited after import, is never touched again.

-- ---------------------------------------------------------------------------
-- Where a venue comes from: added by hand, a file import, or one of the synced sources.
-- ---------------------------------------------------------------------------

DROP INDEX venues_import_key_idx;
ALTER TABLE venues
    DROP CONSTRAINT venues_source,
    DROP CONSTRAINT venues_source_key,
    ADD CONSTRAINT venues_source CHECK (source IN ('manual', 'import', 'ccfddl', 'hci_deadlines', 'openalex')),
    ADD CONSTRAINT venues_source_key CHECK (
        (source <> 'manual') = (source_key IS NOT NULL) AND char_length(source_key) BETWEEN 1 AND 200);
-- Re-importing updates, never duplicates (FR-14.5): one venue per source and key.
CREATE UNIQUE INDEX venues_source_key_idx ON venues (source, source_key) WHERE source <> 'manual';

-- ---------------------------------------------------------------------------
-- Rolling journals: they take submissions at any time, so they have no deadline, no zone
-- and no dates around one. Only a journal can be rolling. Every other venue keeps its
-- deadline and zone, as before.
-- ---------------------------------------------------------------------------

ALTER TABLE venues
    ADD COLUMN rolling boolean NOT NULL DEFAULT false,
    ALTER COLUMN submission_deadline DROP NOT NULL,
    ALTER COLUMN deadline_tz DROP NOT NULL,
    ADD CONSTRAINT venues_rolling CHECK (
        CASE WHEN rolling
             THEN type = 'journal' AND submission_deadline IS NULL AND deadline_tz IS NULL
                  AND abstract_deadline IS NULL AND notification_at IS NULL
             ELSE submission_deadline IS NOT NULL AND deadline_tz IS NOT NULL END);
-- The rolling list (GET /venues?rolling=true): live rolling journals by name.
CREATE INDEX venues_rolling_idx ON venues (name, id) WHERE rolling AND removed_at IS NULL;

-- ---------------------------------------------------------------------------
-- official: the venue's own site. issn_portal: the sync found no working https homepage for
-- a journal and links its ISSN portal record instead, until an administrator sets one.
-- ---------------------------------------------------------------------------

ALTER TABLE venues
    ADD COLUMN link_kind text NOT NULL DEFAULT 'official',
    ADD CONSTRAINT venues_link_kind CHECK (link_kind IN ('official', 'issn_portal'));

-- ---------------------------------------------------------------------------
-- Locked: an administrator edited a synced venue, so the sync never overwrites it again.
-- ---------------------------------------------------------------------------

ALTER TABLE venues
    ADD COLUMN locked_at timestamptz,
    ADD CONSTRAINT venues_locked CHECK (locked_at IS NULL OR source IN ('ccfddl', 'hci_deadlines', 'openalex'));

-- ---------------------------------------------------------------------------
-- The audit trail names the automatic import as its actor: no user, the name "Automatic
-- import", and only the import's own actions, each with the run's ID.
-- ---------------------------------------------------------------------------

ALTER TABLE venue_actions
    ADD COLUMN actor text NOT NULL DEFAULT 'admin',
    ADD CONSTRAINT venue_actions_actor CHECK (
        actor = 'admin'
        OR (actor = 'import' AND admin_id IS NULL AND admin_name = 'Automatic import'
            AND action IN ('import_create', 'import_update')));

-- ---------------------------------------------------------------------------
-- The sync's own state, one row per source: what it last read (a commit, an ETag), the
-- lease of the run in progress, and when the last scheduled run started, so that several
-- API instances run each source once a day between them.
-- ---------------------------------------------------------------------------

CREATE TABLE venue_sync_sources (
    source            text        PRIMARY KEY,
    commit_sha        text,
    etag              text,
    lease_until       timestamptz,
    last_scheduled_at timestamptz,
    last_run_at       timestamptz,
    CONSTRAINT venue_sync_sources_source CHECK (source IN ('ccfddl', 'hci_deadlines', 'openalex')),
    CONSTRAINT venue_sync_sources_commit CHECK (commit_sha ~ '^[0-9a-f]{40}$'),
    CONSTRAINT venue_sync_sources_etag   CHECK (char_length(etag) BETWEEN 1 AND 200)
);
INSERT INTO venue_sync_sources (source) VALUES ('ccfddl'), ('hci_deadlines'), ('openalex');

-- The data files of a repository at the commit last read, so a day with no new commit maps
-- the same files again (a deadline round passes with time) without fetching them.
CREATE TABLE venue_sync_files (
    source  text NOT NULL REFERENCES venue_sync_sources (source) ON DELETE CASCADE,
    path    text NOT NULL,
    content text NOT NULL,
    PRIMARY KEY (source, path),
    CONSTRAINT venue_sync_files_path    CHECK (char_length(path) BETWEEN 1 AND 300),
    CONSTRAINT venue_sync_files_content CHECK (octet_length(content) <= 1048576)
);

-- One row per run: its counts, its errors and its rejected entries with their reasons
-- (at most 200). The last 30 runs of each source are kept.
CREATE TABLE venue_sync_runs (
    id           uuid        PRIMARY KEY DEFAULT uuid_v7(),
    source       text        NOT NULL REFERENCES venue_sync_sources (source) ON DELETE CASCADE,
    dry_run      boolean     NOT NULL,
    trigger      text        NOT NULL,
    requested_by uuid        REFERENCES users (id) ON DELETE SET NULL,
    started_at   timestamptz NOT NULL,
    finished_at  timestamptz,
    status       text        NOT NULL DEFAULT 'running',
    commit_sha   text,
    created      integer     NOT NULL DEFAULT 0,
    updated      integer     NOT NULL DEFAULT 0,
    unchanged    integer     NOT NULL DEFAULT 0,
    skipped      integer     NOT NULL DEFAULT 0,
    rejected     integer     NOT NULL DEFAULT 0,
    errors       jsonb       NOT NULL DEFAULT '[]',
    rejects      jsonb       NOT NULL DEFAULT '[]',
    CONSTRAINT venue_sync_runs_trigger CHECK (trigger IN ('schedule', 'on_demand', 'command')),
    CONSTRAINT venue_sync_runs_status  CHECK (status IN ('running', 'succeeded', 'failed')),
    CONSTRAINT venue_sync_runs_finished CHECK ((status = 'running') = (finished_at IS NULL)),
    CONSTRAINT venue_sync_runs_counts CHECK (
        created >= 0 AND updated >= 0 AND unchanged >= 0 AND skipped >= 0 AND rejected >= 0),
    CONSTRAINT venue_sync_runs_errors  CHECK (jsonb_typeof(errors) = 'array'),
    CONSTRAINT venue_sync_runs_rejects CHECK (jsonb_typeof(rejects) = 'array' AND jsonb_array_length(rejects) <= 200)
);
CREATE INDEX venue_sync_runs_source_idx ON venue_sync_runs (source, dry_run, started_at DESC);

-- Whether a journal homepage answers over https, checked at most every 30 days.
CREATE TABLE venue_sync_link_checks (
    url          text        PRIMARY KEY,
    ok           boolean     NOT NULL,
    final_status integer     NOT NULL,
    checked_at   timestamptz NOT NULL,
    CONSTRAINT venue_sync_link_checks_url CHECK (char_length(url) <= 2048 AND url ~ '^https://')
);

-- ---------------------------------------------------------------------------
-- The sync's embeddings queue at low priority (1, like make embed-backfill), so a first run
-- of about a thousand venues does not hold up users' own changes. The sync sets
-- researchsphere.embedding_priority for its transaction only; everyone else queues at 0.
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION embedding_enqueue_venue() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM embedding_enqueue('venue', NEW.id,
        CASE WHEN current_setting('researchsphere.embedding_priority', true) = '1' THEN 1 ELSE 0 END::smallint);
    RETURN NULL;
END;
$$;

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
        PERFORM embedding_enqueue('venue', vid,
            CASE WHEN current_setting('researchsphere.embedding_priority', true) = '1' THEN 1 ELSE 0 END::smallint);
    END IF;
    RETURN NULL;
END;
$$;
