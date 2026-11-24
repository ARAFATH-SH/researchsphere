-- Venues (FR-14.1 to FR-14.5, FR-15.1; migration 000039). sqlc generates sqlcgen/ from this
-- file. The list's filter is written out in each of its four queries (page, count, and the
-- two facets, each of which ignores its own filter); keep them the same.

-- name: ResearchFields :many
SELECT key, label FROM research_fields ORDER BY position, key;

-- name: ListVenues :many
-- Upcoming first by deadline, then (with include_past) past ones, most recently closed
-- first; then name and ID. A deadline has passed at or before now. Rolling journals (no
-- deadline) are their own list, by name and ID.
SELECT * FROM venues v
WHERE v.removed_at IS NULL
  AND v.rolling = @rolling::boolean
  AND (@include_past::boolean OR @rolling::boolean OR v.submission_deadline > @now::timestamptz)
  AND (cardinality(@types::text[]) = 0 OR v.type = ANY (@types::text[]))
  AND (cardinality(@field_keys::text[]) = 0 OR EXISTS (
        SELECT 1 FROM venue_fields f WHERE f.venue_id = v.id AND f.field_key = ANY (@field_keys::text[])))
  AND (sqlc.narg(deadline_from)::timestamptz IS NULL OR v.submission_deadline >= sqlc.narg(deadline_from)::timestamptz)
  AND (sqlc.narg(deadline_to)::timestamptz IS NULL OR v.submission_deadline < sqlc.narg(deadline_to)::timestamptz)
  AND (@q::text = ''
       OR v.name ILIKE @q_like::text ESCAPE '\' OR v.short_name ILIKE @q_like::text ESCAPE '\'
       OR word_similarity(@q::text, v.name) >= 0.4
       OR word_similarity(@q::text, coalesce(v.short_name, '')) >= 0.4)
ORDER BY (v.submission_deadline <= @now::timestamptz),
         CASE WHEN v.submission_deadline > @now::timestamptz THEN v.submission_deadline END,
         v.submission_deadline DESC, v.name, v.id
LIMIT @lim OFFSET @off;

-- name: CountVenues :one
SELECT count(*)::bigint FROM venues v
WHERE v.removed_at IS NULL
  AND v.rolling = @rolling::boolean
  AND (@include_past::boolean OR @rolling::boolean OR v.submission_deadline > @now::timestamptz)
  AND (cardinality(@types::text[]) = 0 OR v.type = ANY (@types::text[]))
  AND (cardinality(@field_keys::text[]) = 0 OR EXISTS (
        SELECT 1 FROM venue_fields f WHERE f.venue_id = v.id AND f.field_key = ANY (@field_keys::text[])))
  AND (sqlc.narg(deadline_from)::timestamptz IS NULL OR v.submission_deadline >= sqlc.narg(deadline_from)::timestamptz)
  AND (sqlc.narg(deadline_to)::timestamptz IS NULL OR v.submission_deadline < sqlc.narg(deadline_to)::timestamptz)
  AND (@q::text = ''
       OR v.name ILIKE @q_like::text ESCAPE '\' OR v.short_name ILIKE @q_like::text ESCAPE '\'
       OR word_similarity(@q::text, v.name) >= 0.4
       OR word_similarity(@q::text, coalesce(v.short_name, '')) >= 0.4);

-- name: VenueTypeCounts :many
-- The list's filter without its type filter.
SELECT v.type, count(*)::bigint AS n FROM venues v
WHERE v.removed_at IS NULL
  AND v.rolling = @rolling::boolean
  AND (@include_past::boolean OR @rolling::boolean OR v.submission_deadline > @now::timestamptz)
  AND (cardinality(@field_keys::text[]) = 0 OR EXISTS (
        SELECT 1 FROM venue_fields f WHERE f.venue_id = v.id AND f.field_key = ANY (@field_keys::text[])))
  AND (sqlc.narg(deadline_from)::timestamptz IS NULL OR v.submission_deadline >= sqlc.narg(deadline_from)::timestamptz)
  AND (sqlc.narg(deadline_to)::timestamptz IS NULL OR v.submission_deadline < sqlc.narg(deadline_to)::timestamptz)
  AND (@q::text = ''
       OR v.name ILIKE @q_like::text ESCAPE '\' OR v.short_name ILIKE @q_like::text ESCAPE '\'
       OR word_similarity(@q::text, v.name) >= 0.4
       OR word_similarity(@q::text, coalesce(v.short_name, '')) >= 0.4)
GROUP BY v.type;

-- name: VenueFieldCounts :many
-- The list's filter without its field filter; a venue counts once under each of its fields.
SELECT vf.field_key, count(*)::bigint AS n
FROM venues v
JOIN venue_fields vf ON vf.venue_id = v.id
WHERE v.removed_at IS NULL
  AND v.rolling = @rolling::boolean
  AND (@include_past::boolean OR @rolling::boolean OR v.submission_deadline > @now::timestamptz)
  AND (cardinality(@types::text[]) = 0 OR v.type = ANY (@types::text[]))
  AND (sqlc.narg(deadline_from)::timestamptz IS NULL OR v.submission_deadline >= sqlc.narg(deadline_from)::timestamptz)
  AND (sqlc.narg(deadline_to)::timestamptz IS NULL OR v.submission_deadline < sqlc.narg(deadline_to)::timestamptz)
  AND (@q::text = ''
       OR v.name ILIKE @q_like::text ESCAPE '\' OR v.short_name ILIKE @q_like::text ESCAPE '\'
       OR word_similarity(@q::text, v.name) >= 0.4
       OR word_similarity(@q::text, coalesce(v.short_name, '')) >= 0.4)
GROUP BY vf.field_key;

-- name: VenueFieldsOf :many
-- The fields of these venues, in the order of the fixed list.
SELECT vf.venue_id, rf.key, rf.label
FROM venue_fields vf
JOIN research_fields rf ON rf.key = vf.field_key
WHERE vf.venue_id = ANY (@venue_ids::uuid[])
ORDER BY vf.venue_id, rf.position, rf.key;

-- name: GetVenue :one
SELECT * FROM venues WHERE id = @id AND removed_at IS NULL;

-- name: GetVenueForUpdate :one
SELECT * FROM venues WHERE id = @id AND removed_at IS NULL FOR UPDATE;

-- name: InsertVenue :one
INSERT INTO venues (name, short_name, type, rolling, attendance, city, country, starts_on, ends_on, deadline_tz,
                    submission_deadline, abstract_deadline, notification_at, official_url, link_kind, scope, source,
                    source_key, created_by)
VALUES (@name, sqlc.narg(short_name), @type, @rolling, sqlc.narg(attendance), sqlc.narg(city), sqlc.narg(country),
        sqlc.narg(starts_on), sqlc.narg(ends_on), sqlc.narg(deadline_tz), sqlc.narg(submission_deadline),
        sqlc.narg(abstract_deadline), sqlc.narg(notification_at), @official_url, @link_kind, @scope, @source,
        sqlc.narg(source_key), sqlc.narg(created_by))
RETURNING id;

-- name: UpdateVenue :exec
UPDATE venues
SET name                = @name,
    short_name          = sqlc.narg(short_name),
    type                = @type,
    rolling             = @rolling,
    attendance          = sqlc.narg(attendance),
    city                = sqlc.narg(city),
    country             = sqlc.narg(country),
    starts_on           = sqlc.narg(starts_on),
    ends_on             = sqlc.narg(ends_on),
    deadline_tz         = sqlc.narg(deadline_tz),
    submission_deadline = sqlc.narg(submission_deadline),
    abstract_deadline   = sqlc.narg(abstract_deadline),
    notification_at     = sqlc.narg(notification_at),
    official_url        = @official_url,
    link_kind           = @link_kind,
    scope               = @scope
WHERE id = @id;

-- name: LockVenue :exec
-- An administrator edited a synced venue: the sync leaves it alone from now on.
UPDATE venues SET locked_at = coalesce(locked_at, @at::timestamptz)
WHERE id = @id AND source IN ('ccfddl', 'hci_deadlines', 'openalex');

-- name: DeleteOtherVenueFields :exec
-- Only the fields no longer wanted, so an unchanged field fires no trigger.
DELETE FROM venue_fields WHERE venue_id = @venue_id AND NOT (field_key = ANY (@keys::text[]));

-- name: AddVenueFields :exec
INSERT INTO venue_fields (venue_id, field_key)
SELECT @venue_id, k FROM unnest(@keys::text[]) AS k
ON CONFLICT DO NOTHING;

-- name: RemoveVenue :execrows
UPDATE venues
SET removed_at = @removed_at, removed_by = @removed_by, removal_reason = @reason
WHERE id = @id AND removed_at IS NULL;

-- name: InsertVenueAction :exec
INSERT INTO venue_actions (venue_id, actor, admin_id, admin_name, action, reason, changed_fields, import_id)
VALUES (@venue_id, @actor, @admin_id, @admin_name, @action, sqlc.narg(reason), @changed_fields::text[], sqlc.narg(import_id));

-- name: AdminName :one
SELECT coalesce(p.full_name, u.email::text)::text AS name
FROM users u
LEFT JOIN profiles p ON p.user_id = u.id
WHERE u.id = @user_id;

-- name: LockVenueImports :exec
-- Imports take turns, so two files cannot both create the same key.
SELECT pg_advisory_xact_lock(hashtext('venue_import'));

-- name: ImportedVenuesByKeys :many
SELECT * FROM venues
WHERE source = @source AND source <> 'manual' AND source_key = ANY (@keys::text[])
FOR UPDATE;

-- name: LowEmbeddingPriority :exec
-- The embeddings this transaction queues are low priority (000040's trigger reads it).
SELECT set_config('researchsphere.embedding_priority', '1', true);

-- ---------------------------------------------------------------------------
-- The automatic import's own state (slice 25A, migration 000040).
-- ---------------------------------------------------------------------------

-- name: EnsureSyncSource :exec
-- 000040 seeds the three rows; this puts one back if it is ever missing.
INSERT INTO venue_sync_sources (source) VALUES (@source) ON CONFLICT (source) DO NOTHING;

-- name: LockSyncSource :one
SELECT * FROM venue_sync_sources WHERE source = @source FOR UPDATE;

-- name: SyncSources :many
SELECT * FROM venue_sync_sources ORDER BY source;

-- name: SaveSyncSource :exec
UPDATE venue_sync_sources
SET lease_until = sqlc.narg(lease_until), last_scheduled_at = sqlc.narg(last_scheduled_at),
    last_run_at = sqlc.narg(last_run_at)
WHERE source = @source;

-- name: SaveSyncRepoState :exec
UPDATE venue_sync_sources SET commit_sha = sqlc.narg(commit_sha), etag = sqlc.narg(etag) WHERE source = @source;

-- name: SyncFiles :many
SELECT path, content FROM venue_sync_files WHERE source = @source ORDER BY path;

-- name: DeleteSyncFiles :exec
DELETE FROM venue_sync_files WHERE source = @source;

-- name: DeleteSyncFilePaths :exec
DELETE FROM venue_sync_files WHERE source = @source AND path = ANY (@paths::text[]);

-- name: UpsertSyncFile :exec
INSERT INTO venue_sync_files (source, path, content) VALUES (@source, @path, @content)
ON CONFLICT (source, path) DO UPDATE SET content = EXCLUDED.content;

-- name: InsertSyncRun :one
INSERT INTO venue_sync_runs (source, dry_run, trigger, requested_by, started_at)
VALUES (@source, @dry_run, @trigger, sqlc.narg(requested_by), @started_at)
RETURNING id;

-- name: FinishSyncRun :exec
UPDATE venue_sync_runs
SET finished_at = @finished_at, status = @status, commit_sha = sqlc.narg(commit_sha), created = @created,
    updated = @updated, unchanged = @unchanged, skipped = @skipped, rejected = @rejected,
    errors = @errors::jsonb, rejects = @rejects::jsonb
WHERE id = @id;

-- name: PruneSyncRuns :exec
-- Keep the last 30 runs of a source, real and dry.
DELETE FROM venue_sync_runs r
WHERE r.source = @source::text AND r.status <> 'running' AND r.id NOT IN (
    SELECT k.id FROM venue_sync_runs k WHERE k.source = @source::text ORDER BY k.started_at DESC, k.id DESC LIMIT 30);

-- name: FailStaleSyncRuns :exec
-- A run still running when its source's lease has expired was cut off (a restart).
UPDATE venue_sync_runs
SET status = 'failed', finished_at = @at::timestamptz, errors = errors || '["The run was interrupted."]'::jsonb
WHERE source = @source AND status = 'running';

-- name: LatestSyncRun :one
SELECT * FROM venue_sync_runs WHERE source = @source AND dry_run = @dry_run
ORDER BY started_at DESC, id DESC LIMIT 1;

-- name: LinkChecks :many
SELECT * FROM venue_sync_link_checks WHERE url = ANY (@urls::text[]);

-- name: SaveLinkCheck :exec
INSERT INTO venue_sync_link_checks (url, ok, final_status, checked_at) VALUES (@url, @ok, @final_status, @checked_at)
ON CONFLICT (url) DO UPDATE SET ok = EXCLUDED.ok, final_status = EXCLUDED.final_status, checked_at = EXCLUDED.checked_at;
