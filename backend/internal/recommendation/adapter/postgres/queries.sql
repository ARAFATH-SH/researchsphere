-- Collaborator recommendations (AI-2.1, AI-2.2, AI-2.4, AI-4). Read only.
--
-- AI-2.4: the ranking orders by the cosine distance of the profile embeddings and by
-- nothing else. These queries never read publication, citation or connection counts or
-- any other popularity signal; connections and blocks are read only to leave people out
-- (TestRankQueryReadsNoPopularity checks this file).

-- name: SetIterativeScan :exec
-- HNSW iterative scan (pgvector 0.8): when the filters below drop candidates the index
-- returned, the scan goes on until LIMIT rows pass, instead of returning fewer. relaxed_order
-- may return rows slightly out of order, so RankPeople sorts again. Local to the transaction.
SELECT set_config('hnsw.iterative_scan', 'relaxed_order', true);

-- name: SetEfSearch :exec
-- The HNSW candidate list size; 100 keeps the approximate top 10 close to the exact one.
SELECT set_config('hnsw.ef_search', @ef_search::text, true);

-- name: SelfFields :one
-- The caller's AI-4.2 fields, tags in the profile's order, and the stored embedding's stamp
-- (NULLs when there is none). One row even without a profile.
SELECT
    COALESCE((SELECT p.bio IS NOT NULL AND btrim(p.bio) <> '' FROM profiles p WHERE p.user_id = @user_id::uuid), false)::boolean AS has_bio,
    COALESCE((SELECT array_agg(ri.name::text ORDER BY lower(ri.name::text), ri.name::text)
              FROM user_interests ui JOIN research_interests ri ON ri.id = ui.interest_id
              WHERE ui.user_id = @user_id::uuid), '{}')::text[] AS interests,
    COALESCE((SELECT array_agg(s.name::text ORDER BY lower(s.name::text), s.name::text)
              FROM user_skills us JOIN skills s ON s.id = us.skill_id
              WHERE us.user_id = @user_id::uuid), '{}')::text[] AS skills,
    e.model, e.content_hash, e.updated_at
FROM (SELECT 1) AS one
LEFT JOIN profile_embeddings e ON e.user_id = @user_id::uuid;

-- name: RankPeople :many
-- The lim users whose profile embedding is closest in cosine distance to the caller's
-- (AI-2.1), through profile_embeddings_hnsw_idx. Left out: the caller; embeddings of
-- another model (not comparable); unverified and suspended users; a pending or accepted
-- connection either way (connections_live_pair_uq); a block either way (FR-16.9).
WITH me AS (
    SELECT embedding, model FROM profile_embeddings WHERE user_id = @user_id::uuid
),
cand AS MATERIALIZED (
    SELECT e.user_id, (e.embedding <=> (SELECT embedding FROM me))::float8 AS distance
    FROM profile_embeddings e
    JOIN users u ON u.id = e.user_id
    WHERE e.user_id <> @user_id::uuid
      AND e.model = (SELECT model FROM me)
      AND u.email_verified_at IS NOT NULL
      AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= now()))
      AND NOT EXISTS (
          SELECT 1 FROM connections c
          WHERE LEAST(c.requester_id, c.receiver_id) = LEAST(@user_id::uuid, e.user_id)
            AND GREATEST(c.requester_id, c.receiver_id) = GREATEST(@user_id::uuid, e.user_id)
            AND c.status IN ('pending', 'accepted'))
      AND NOT EXISTS (
          SELECT 1 FROM blocks b
          WHERE (b.blocker_id = @user_id::uuid AND b.blocked_id = e.user_id)
             OR (b.blocker_id = e.user_id AND b.blocked_id = @user_id::uuid))
    ORDER BY e.embedding <=> (SELECT embedding FROM me)
    LIMIT @lim
)
SELECT cand.user_id, (1 - cand.distance)::float8 AS cosine,
       p.full_name, p.headline, p.institution, p.photo_file_id
FROM cand
JOIN profiles p ON p.user_id = cand.user_id
ORDER BY cand.distance, cand.user_id;

-- name: StillEligible :many
-- Those of ids that RankPeople could still return for the caller now: the same filters.
SELECT e.user_id
FROM profile_embeddings e
JOIN users u ON u.id = e.user_id
JOIN profiles p ON p.user_id = e.user_id
WHERE e.user_id = ANY(@ids::uuid[])
  AND e.user_id <> @user_id::uuid
  AND e.model = (SELECT m.model FROM profile_embeddings m WHERE m.user_id = @user_id::uuid)
  AND u.email_verified_at IS NOT NULL
  AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= now()))
  AND NOT EXISTS (
      SELECT 1 FROM connections c
      WHERE LEAST(c.requester_id, c.receiver_id) = LEAST(@user_id::uuid, e.user_id)
        AND GREATEST(c.requester_id, c.receiver_id) = GREATEST(@user_id::uuid, e.user_id)
        AND c.status IN ('pending', 'accepted'))
  AND NOT EXISTS (
      SELECT 1 FROM blocks b
      WHERE (b.blocker_id = @user_id::uuid AND b.blocked_id = e.user_id)
         OR (b.blocker_id = e.user_id AND b.blocked_id = @user_id::uuid));

-- name: InterestsOf :many
-- The research interests of the ranked users, for the shared list (AI-2.2).
SELECT ui.user_id, ri.name::text AS name
FROM user_interests ui
JOIN research_interests ri ON ri.id = ui.interest_id
WHERE ui.user_id = ANY(@user_ids::uuid[])
ORDER BY ui.user_id, lower(ri.name::text), ri.name::text;

-- name: SkillsOf :many
-- The skills of the ranked users, for the shared list (AI-2.2).
SELECT us.user_id, s.name::text AS name
FROM user_skills us
JOIN skills s ON s.id = us.skill_id
WHERE us.user_id = ANY(@user_ids::uuid[])
ORDER BY us.user_id, lower(s.name::text), s.name::text;

-- Project matching (AI-3.1, AI-2.3; slice 19, chunk 19.2). Read only, with the AI-2.4 rule
-- above: the ranking queries order by cosine distance only. A project's member count is read
-- only by ProjectCards, for display, after the order is fixed
-- (TestRankQueryReadsNoPopularity checks the ranking queries).
--
-- A project has one stored embedding: the AI service averages the vectors of its chunks
-- (decisions.md, 2026-11-14), so there is no per-chunk vector to rank on.

-- name: RankProjects :many
-- The lim open projects whose embedding is closest in cosine distance to the caller's
-- profile embedding (AI-3.1), through project_embeddings_hnsw_idx. Left out: embeddings of
-- another model; projects the caller is a member of; projects the caller has a pending
-- request to join; projects whose owner is unverified or suspended now; a block either way
-- between the caller and the owner (FR-16.9).
-- slice 20: join requests are written there (project_join_requests exists since 000001);
-- the filter below already leaves a pending request of kind 'request' out.
WITH me AS (
    SELECT embedding, model FROM profile_embeddings WHERE user_id = @user_id::uuid
),
cand AS MATERIALIZED (
    SELECT e.project_id, (e.embedding <=> (SELECT embedding FROM me))::float8 AS distance
    FROM project_embeddings e
    JOIN projects p ON p.id = e.project_id
    JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
    JOIN users u ON u.id = o.user_id
    WHERE e.model = (SELECT model FROM me)
      AND p.status = 'open'
      AND u.email_verified_at IS NOT NULL
      AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= now()))
      AND NOT EXISTS (
          SELECT 1 FROM project_members m
          WHERE m.project_id = p.id AND m.user_id = @user_id::uuid)
      AND NOT EXISTS (
          SELECT 1 FROM project_join_requests j
          WHERE j.project_id = p.id AND j.user_id = @user_id::uuid
            AND j.kind = 'request' AND j.status = 'pending')
      AND NOT EXISTS (
          SELECT 1 FROM blocks b
          WHERE (b.blocker_id = @user_id::uuid AND b.blocked_id = o.user_id)
             OR (b.blocker_id = o.user_id AND b.blocked_id = @user_id::uuid))
    ORDER BY e.embedding <=> (SELECT embedding FROM me)
    LIMIT @lim
)
SELECT cand.project_id, (1 - cand.distance)::float8 AS cosine
FROM cand
ORDER BY cand.distance, cand.project_id;

-- name: ProjectsStillEligible :many
-- Those of ids that RankProjects could still return for the caller now: the same filters.
SELECT p.id
FROM projects p
JOIN project_embeddings e ON e.project_id = p.id
JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
JOIN users u ON u.id = o.user_id
WHERE p.id = ANY(@ids::uuid[])
  AND e.model = (SELECT m.model FROM profile_embeddings m WHERE m.user_id = @user_id::uuid)
  AND p.status = 'open'
  AND u.email_verified_at IS NOT NULL
  AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= now()))
  AND NOT EXISTS (
      SELECT 1 FROM project_members m
      WHERE m.project_id = p.id AND m.user_id = @user_id::uuid)
  AND NOT EXISTS (
      SELECT 1 FROM project_join_requests j
      WHERE j.project_id = p.id AND j.user_id = @user_id::uuid
        AND j.kind = 'request' AND j.status = 'pending')
  AND NOT EXISTS (
      SELECT 1 FROM blocks b
      WHERE (b.blocker_id = @user_id::uuid AND b.blocked_id = o.user_id)
         OR (b.blocker_id = o.user_id AND b.blocked_id = @user_id::uuid));

-- name: ProjectCards :many
-- What a recommended project shows, read after the ranking: title, the first 300 characters
-- of the description, the owner and the member count (display only, AI-2.4).
SELECT p.id, p.title, left(p.description, 300)::text AS description,
       o.user_id AS owner_id, coalesce(op.full_name, '')::text AS owner_name,
       op.headline AS owner_headline, op.photo_file_id AS owner_photo,
       (SELECT count(*) FROM project_members m WHERE m.project_id = p.id)::int AS member_count
FROM projects p
JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
LEFT JOIN profiles op ON op.user_id = o.user_id
WHERE p.id = ANY(@ids::uuid[]);

-- name: ProjectSkillsOf :many
-- The required skills of projects, by name ignoring letter case: the project's order for
-- the shared list and the summary.
SELECT ps.project_id, s.name::text AS name
FROM project_skills ps
JOIN skills s ON s.id = ps.skill_id
WHERE ps.project_id = ANY(@project_ids::uuid[])
ORDER BY ps.project_id, lower(s.name::text), s.name::text;

-- name: ProjectSelfFields :many
-- The project's owner and the stored embedding's stamp (NULLs when there is none); no row
-- when the project does not exist.
SELECT o.user_id AS owner_id, e.model, e.content_hash, e.updated_at
FROM projects p
JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
LEFT JOIN project_embeddings e ON e.project_id = p.id
WHERE p.id = @project_id::uuid;

-- name: RankResearchers :many
-- The lim users whose profile embedding is closest in cosine distance to the project's
-- embedding (AI-2.3), through profile_embeddings_hnsw_idx. Left out: embeddings of another
-- model; the project's members; unverified and suspended users; a block either way between
-- the user and the project's owner (FR-16.9).
WITH pe AS (
    SELECT e.embedding, e.model, o.user_id AS owner_id
    FROM project_embeddings e
    JOIN project_members o ON o.project_id = e.project_id AND o.role = 'owner'
    WHERE e.project_id = @project_id::uuid
),
cand AS MATERIALIZED (
    SELECT e.user_id, (e.embedding <=> (SELECT embedding FROM pe))::float8 AS distance
    FROM profile_embeddings e
    JOIN users u ON u.id = e.user_id
    WHERE e.model = (SELECT model FROM pe)
      AND u.email_verified_at IS NOT NULL
      AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= now()))
      AND NOT EXISTS (
          SELECT 1 FROM project_members m
          WHERE m.project_id = @project_id::uuid AND m.user_id = e.user_id)
      AND NOT EXISTS (
          SELECT 1 FROM blocks b
          WHERE (b.blocker_id = (SELECT owner_id FROM pe) AND b.blocked_id = e.user_id)
             OR (b.blocker_id = e.user_id AND b.blocked_id = (SELECT owner_id FROM pe)))
    ORDER BY e.embedding <=> (SELECT embedding FROM pe)
    LIMIT @lim
)
SELECT cand.user_id, (1 - cand.distance)::float8 AS cosine,
       p.full_name, p.headline, p.institution, p.photo_file_id
FROM cand
JOIN profiles p ON p.user_id = cand.user_id
ORDER BY cand.distance, cand.user_id;

-- name: ResearchersStillEligible :many
-- Those of ids that RankResearchers could still return for the project now.
SELECT e.user_id
FROM profile_embeddings e
JOIN users u ON u.id = e.user_id
JOIN profiles p ON p.user_id = e.user_id
JOIN project_embeddings pe ON pe.project_id = @project_id::uuid
JOIN project_members o ON o.project_id = @project_id::uuid AND o.role = 'owner'
WHERE e.user_id = ANY(@ids::uuid[])
  AND e.model = pe.model
  AND u.email_verified_at IS NOT NULL
  AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= now()))
  AND NOT EXISTS (
      SELECT 1 FROM project_members m
      WHERE m.project_id = @project_id::uuid AND m.user_id = e.user_id)
  AND NOT EXISTS (
      SELECT 1 FROM blocks b
      WHERE (b.blocker_id = o.user_id AND b.blocked_id = e.user_id)
         OR (b.blocker_id = e.user_id AND b.blocked_id = o.user_id));

-- name: OwnedProjects :many
-- The projects the users own, whose researcher lists a block change clears.
SELECT project_id FROM project_members
WHERE user_id = ANY(@user_ids::uuid[]) AND role = 'owner';
