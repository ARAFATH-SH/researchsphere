-- Embeddings (AI-1): the queue, the stored vectors and the source text. Changes enqueue
-- through the triggers of migration 000028; these queries are the worker's and the admin's.

-- name: ClaimJobs :many
-- Lease due jobs no other worker holds: user changes first, then the oldest. SKIP LOCKED
-- lets workers claim side by side; the lease, not a row lock, keeps the job theirs while
-- they call the AI service, so a profile save that upserts the job never waits.
UPDATE embedding_jobs j
SET leased_until = clock_timestamp() + make_interval(secs => sqlc.arg(lease_seconds)::float8)
FROM (
    SELECT owner_type, owner_id
    FROM embedding_jobs
    WHERE failed_at IS NULL
      AND next_try_at <= clock_timestamp()
      AND (leased_until IS NULL OR leased_until < clock_timestamp())
    ORDER BY priority, next_try_at
    LIMIT @lim
    FOR UPDATE SKIP LOCKED
) due
WHERE j.owner_type = due.owner_type AND j.owner_id = due.owner_id
RETURNING j.owner_type, j.owner_id, j.generation, j.attempts, j.force, j.priority, j.next_try_at;

-- name: FinishJob :execrows
DELETE FROM embedding_jobs
WHERE owner_type = @owner_type AND owner_id = @owner_id AND generation = @generation;

-- name: ReleaseJob :exec
UPDATE embedding_jobs SET leased_until = NULL
WHERE owner_type = @owner_type AND owner_id = @owner_id;

-- name: RetryJob :exec
UPDATE embedding_jobs
SET leased_until = NULL,
    attempts     = attempts + 1,
    next_try_at  = @next_try_at,
    last_error   = @last_error,
    failed_at    = CASE WHEN @failed::boolean THEN clock_timestamp() END
WHERE owner_type = @owner_type AND owner_id = @owner_id AND generation = @generation;

-- name: ProfileSourceFields :one
-- The public fields of a profile (AI-1.1) and whether its user may have an embedding:
-- verified and not suspended now. No contact field is read.
SELECT p.headline, p.bio,
       (u.email_verified_at IS NOT NULL
        AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= now())))::boolean AS eligible
FROM profiles p
JOIN users u ON u.id = p.user_id
WHERE p.user_id = @user_id;

-- name: ProfileInterestNames :many
SELECT ri.name::text AS name
FROM user_interests ui
JOIN research_interests ri ON ri.id = ui.interest_id
WHERE ui.user_id = @user_id
ORDER BY lower(ri.name::text), ri.name::text;

-- name: ProfileSkillNames :many
SELECT s.name::text AS name
FROM user_skills us
JOIN skills s ON s.id = us.skill_id
WHERE us.user_id = @user_id
ORDER BY lower(s.name::text), s.name::text;

-- name: ProfilePublicationTexts :many
-- Confirmed publications only (FR-5.5), newest first as the profile lists them: by year, no
-- year last, then by when the user added it; the ID breaks ties so the order is stable.
SELECT pub.title, pub.abstract
FROM user_publications up
JOIN publications pub ON pub.id = up.publication_id
WHERE up.user_id = @user_id AND up.confirmed_at IS NOT NULL
ORDER BY pub.publication_year DESC NULLS LAST, up.created_at DESC, pub.id;

-- name: StoredProfileEmbedding :one
SELECT model, content_hash FROM profile_embeddings WHERE user_id = @user_id;

-- name: SaveProfileEmbedding :execrows
-- Only while the user may have one. FOR SHARE holds the user row: a suspension that comes
-- first is seen here (no row stored), one that comes later waits and its trigger deletes this.
WITH owner AS (
    SELECT u.id
    FROM users u
    JOIN profiles p ON p.user_id = u.id
    WHERE u.id = @user_id
      AND u.email_verified_at IS NOT NULL
      AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= now()))
    FOR SHARE OF u
)
INSERT INTO profile_embeddings (user_id, model, content_hash, embedding, chunk_count)
SELECT owner.id, @model, @content_hash, CAST(@embedding::text AS vector), @chunk_count FROM owner
ON CONFLICT (user_id) DO UPDATE
SET model        = EXCLUDED.model,
    content_hash = EXCLUDED.content_hash,
    embedding    = EXCLUDED.embedding,
    chunk_count  = EXCLUDED.chunk_count,
    updated_at   = now();

-- name: DeleteProfileEmbedding :exec
DELETE FROM profile_embeddings WHERE user_id = @user_id;

-- name: QueueStaleProfiles :execrows
-- Owners whose embedding another model made and that wait for nothing yet (AI-1.6).
INSERT INTO embedding_jobs (owner_type, owner_id, priority)
SELECT 'profile', e.user_id, 1
FROM profile_embeddings e
WHERE e.model <> @model
  AND NOT EXISTS (SELECT 1 FROM embedding_jobs j WHERE j.owner_type = 'profile' AND j.owner_id = e.user_id)
ORDER BY e.user_id
LIMIT @lim
ON CONFLICT (owner_type, owner_id) DO NOTHING;

-- name: QueueAllProfiles :one
-- Every eligible profile, through embedding_enqueue so a waiting job is reset the same way a
-- change resets it.
SELECT count(*)::bigint AS queued
FROM (
    SELECT embedding_enqueue('profile', p.user_id, 1::smallint, @force::boolean)
    FROM profiles p
    JOIN users u ON u.id = p.user_id
    WHERE u.email_verified_at IS NOT NULL
      AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= now()))
) q;

-- Projects (AI-1.2; slice 19). Every project may have an embedding; status and the owner's
-- suspension are filtered where the embeddings are read (AI-3).

-- name: ProjectSourceFields :one
SELECT title, description, abstract FROM projects WHERE id = @project_id;

-- name: ProjectSkillNames :many
SELECT s.name::text AS name
FROM project_skills ps
JOIN skills s ON s.id = ps.skill_id
WHERE ps.project_id = @project_id
ORDER BY lower(s.name::text), s.name::text;

-- name: StoredProjectEmbedding :one
SELECT model, content_hash FROM project_embeddings WHERE project_id = @project_id;

-- name: SaveProjectEmbedding :execrows
-- Only while the project exists. FOR KEY SHARE holds it: a delete that comes first is seen
-- here (no row stored), one that comes later waits and its cascade deletes this.
WITH owner AS (
    SELECT id FROM projects WHERE id = @project_id FOR KEY SHARE
)
INSERT INTO project_embeddings (project_id, model, content_hash, embedding, chunk_count)
SELECT owner.id, @model, @content_hash, CAST(@embedding::text AS vector), @chunk_count FROM owner
ON CONFLICT (project_id) DO UPDATE
SET model        = EXCLUDED.model,
    content_hash = EXCLUDED.content_hash,
    embedding    = EXCLUDED.embedding,
    chunk_count  = EXCLUDED.chunk_count,
    updated_at   = now();

-- name: DeleteProjectEmbedding :exec
DELETE FROM project_embeddings WHERE project_id = @project_id;

-- name: QueueStaleProjects :execrows
INSERT INTO embedding_jobs (owner_type, owner_id, priority)
SELECT 'project', e.project_id, 1
FROM project_embeddings e
WHERE e.model <> @model
  AND NOT EXISTS (SELECT 1 FROM embedding_jobs j WHERE j.owner_type = 'project' AND j.owner_id = e.project_id)
ORDER BY e.project_id
LIMIT @lim
ON CONFLICT (owner_type, owner_id) DO NOTHING;

-- name: QueueAllProjects :one
SELECT count(*)::bigint AS queued
FROM (SELECT embedding_enqueue('project', p.id, 1::smallint, @force::boolean) FROM projects p) q;

-- name: ProjectEmbeddingStats :one
SELECT count(*)::bigint                                  AS total,
       count(*) FILTER (WHERE model = @model)::bigint    AS current_model
FROM project_embeddings;

-- name: InsertAIAdminAction :exec
INSERT INTO ai_admin_actions (admin_id, action, queued) VALUES (@admin_id, @action, @queued);

-- name: ProfileEmbeddingStats :one
SELECT count(*)::bigint                                  AS total,
       count(*) FILTER (WHERE model = @model)::bigint    AS current_model
FROM profile_embeddings;

-- name: JobStats :many
SELECT owner_type,
       count(*) FILTER (WHERE failed_at IS NULL)::bigint     AS queued,
       count(*) FILTER (WHERE failed_at IS NOT NULL)::bigint AS failed
FROM embedding_jobs
GROUP BY owner_type;
