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
