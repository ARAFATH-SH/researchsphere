-- Research projects (FR-10.1 to FR-10.3, FR-10.6, FR-15.11). The owner is the member row
-- with role 'owner' (000001); every read joins it. A value given as @viewer_id that is not
-- a member gets an empty role.

-- name: InsertProject :one
INSERT INTO projects (title, description, abstract)
VALUES (@title, @description, sqlc.narg(abstract))
RETURNING id;

-- name: InsertOwner :exec
-- The deferred owner check (projects_have_owner) passes at commit because this row is
-- written in the project's transaction.
INSERT INTO project_members (project_id, user_id, role) VALUES (@project_id, @user_id, 'owner');

-- name: InsertSkillNames :exec
-- Skills are the shared tags of profiles (FR-4.2): citext, so a name that differs only in
-- letter case is the existing tag and keeps its first spelling.
INSERT INTO skills (name) SELECT unnest(@names::text[]) ON CONFLICT (name) DO NOTHING;

-- name: RemoveSkillsExcept :exec
-- Removes the project's skills whose names are not in names (compared ignoring case).
DELETE FROM project_skills ps
USING skills s
WHERE ps.project_id = @project_id AND s.id = ps.skill_id
  AND NOT (s.name = ANY ((@names::text[])::citext[]));

-- name: AddSkills :exec
INSERT INTO project_skills (project_id, skill_id)
SELECT @project_id, s.id FROM skills s WHERE s.name = ANY ((@names::text[])::citext[])
ON CONFLICT DO NOTHING;

-- name: ProjectSkills :many
-- citext sorts ignoring letter case; the id breaks ties.
SELECT s.id, s.name::text AS name
FROM project_skills ps
JOIN skills s ON s.id = ps.skill_id
WHERE ps.project_id = @project_id
ORDER BY s.name, s.id;

-- name: SkillsOfProjects :many
SELECT ps.project_id, s.name::text AS name
FROM project_skills ps
JOIN skills s ON s.id = ps.skill_id
WHERE ps.project_id = ANY (@ids::uuid[])
ORDER BY ps.project_id, s.name, s.id;

-- name: GetProject :one
-- The project, its owner and what the viewer needs for the visibility rule: whether the
-- owner's account is usable (verified, not suspended), whether the owner has blocked the
-- viewer (FR-16.9) and the viewer's role.
SELECT p.id, p.title, p.description, p.abstract, p.status::text AS status, p.created_at, p.updated_at,
       o.user_id AS owner_id,
       coalesce(op.full_name, '')::text AS owner_name, op.headline AS owner_headline, op.photo_file_id AS owner_photo,
       (ou.email_verified_at IS NOT NULL AND ou.suspended_at IS NULL)::boolean AS owner_available,
       EXISTS (SELECT 1 FROM blocks x WHERE x.blocker_id = o.user_id AND x.blocked_id = @viewer_id::uuid)::boolean AS blocked_by_owner,
       coalesce((SELECT m.role::text FROM project_members m WHERE m.project_id = p.id AND m.user_id = @viewer_id::uuid), '')::text AS my_role,
       (SELECT count(*) FROM project_members m WHERE m.project_id = p.id)::int AS member_count
FROM projects p
JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
JOIN users ou ON ou.id = o.user_id
LEFT JOIN profiles op ON op.user_id = o.user_id
WHERE p.id = @id;

-- name: LockProject :one
-- GetProject's access fields, with the project row locked until the transaction ends, so
-- an edit or a delete decides on what it changes.
SELECT p.title, p.status::text AS status, o.user_id AS owner_id,
       (ou.email_verified_at IS NOT NULL AND ou.suspended_at IS NULL)::boolean AS owner_available,
       EXISTS (SELECT 1 FROM blocks x WHERE x.blocker_id = o.user_id AND x.blocked_id = @viewer_id::uuid)::boolean AS blocked_by_owner,
       coalesce((SELECT m.role::text FROM project_members m WHERE m.project_id = p.id AND m.user_id = @viewer_id::uuid), '')::text AS my_role
FROM projects p
JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
JOIN users ou ON ou.id = o.user_id
WHERE p.id = @id
FOR UPDATE OF p;

-- name: UpdateProject :exec
-- A NULL title, description or status keeps the value; abstract changes only when
-- set_abstract is true, to the given value or NULL.
UPDATE projects
SET title       = coalesce(sqlc.narg(title)::text, title),
    description = coalesce(sqlc.narg(description)::text, description),
    abstract    = CASE WHEN @set_abstract::boolean THEN sqlc.narg(abstract)::text ELSE abstract END,
    status      = coalesce(sqlc.narg(status)::text, status::text)::project_status
WHERE id = @id;

-- name: DeleteProject :exec
-- The schema's cascades remove members, skills, join requests, files, compile jobs, the
-- embedding and reports; projects_embedding_delete removes the embedding job. Stored file
-- objects (slice 21) go through stored_files and storage_deletions.
DELETE FROM projects WHERE id = @id;

-- name: MemberRole :one
SELECT role::text FROM project_members WHERE project_id = @project_id AND user_id = @user_id;

-- name: CountMyProjects :one
SELECT count(*)::int FROM project_members WHERE user_id = @user_id;

-- name: MyProjects :many
-- Every project the user is a member of, any status, most recently changed first.
SELECT p.id, p.title, left(p.description, 300)::text AS description, p.status::text AS status,
       p.created_at, p.updated_at, m.role::text AS my_role,
       o.user_id AS owner_id,
       coalesce(op.full_name, '')::text AS owner_name, op.headline AS owner_headline, op.photo_file_id AS owner_photo
FROM project_members m
JOIN projects p ON p.id = m.project_id
JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
LEFT JOIN profiles op ON op.user_id = o.user_id
WHERE m.user_id = @user_id
ORDER BY p.updated_at DESC, p.id DESC
LIMIT @lim OFFSET @off;

-- Project search (FR-10.3). Parameters:
--   q        the trimmed search text
--   pattern  q as an ILIKE pattern, '%' + q with \ % _ escaped + '%'
--   fuzzy    true when no visible open project's title contains q: titles with a word
--            similar to q (title %> q, with the threshold SetWordSimilarityThreshold sets in
--            the same transaction) take the place of the literal title matches
--   viewer_id  the searcher; a project whose owner blocked them is left out (FR-16.9)
-- A result's rank is its best match: 0 title equal, 1 title prefix, 2 title contains,
-- 3 similar title, 4 required skill, 5 description. Only open projects whose owner is
-- verified and not suspended are found.

-- name: SetWordSimilarityThreshold :exec
-- Transaction-local (is_local true), so it ends with the search's transaction.
SELECT set_config('pg_trgm.word_similarity_threshold', @threshold::text, true);

-- name: AnyVisibleTitleContains :one
SELECT EXISTS (
    SELECT 1
    FROM projects p
    JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
    JOIN users u ON u.id = o.user_id
    WHERE p.title ILIKE @pattern::text AND p.status = 'open'
      AND u.email_verified_at IS NOT NULL AND u.suspended_at IS NULL
      AND NOT EXISTS (SELECT 1 FROM blocks x WHERE x.blocker_id = o.user_id AND x.blocked_id = @viewer_id::uuid)
);

-- name: SearchProjectsSummary :one
-- The number of results, and the title most similar to q among the similar-title results
-- ('' when there are none).
WITH hits AS (
    SELECT p.id,
           CASE WHEN lower(p.title) = lower(@q::text) THEN 0
                WHEN starts_with(lower(p.title), lower(@q::text)) THEN 1
                ELSE 2 END AS rank
    FROM projects p
    WHERE NOT @fuzzy::boolean AND p.title ILIKE @pattern::text
    UNION ALL
    SELECT p.id, 3 FROM projects p WHERE @fuzzy::boolean AND p.title %> @q::text
    UNION ALL
    SELECT ps.project_id, 4
    FROM skills s
    JOIN project_skills ps ON ps.skill_id = s.id
    WHERE s.name::text ILIKE @pattern::text
    UNION ALL
    SELECT p.id, 5 FROM projects p WHERE p.description ILIKE @pattern::text
), best AS (
    SELECT id, min(rank) AS rank FROM hits GROUP BY id
), matches AS (
    SELECT b.id, b.rank, p.title
    FROM best b
    JOIN projects p ON p.id = b.id
    JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
    JOIN users u ON u.id = o.user_id
    WHERE p.status = 'open' AND u.email_verified_at IS NOT NULL AND u.suspended_at IS NULL
      AND NOT EXISTS (SELECT 1 FROM blocks x WHERE x.blocker_id = o.user_id AND x.blocked_id = @viewer_id::uuid)
)
SELECT count(*)::int AS total,
       coalesce((array_agg(m.title ORDER BY word_similarity(@q::text, m.title) DESC, similarity(m.title, @q::text) DESC, m.title, m.id)
                 FILTER (WHERE m.rank = 3))[1], '')::text AS corrected_query
FROM matches m;

-- name: SearchProjectsPage :many
-- Within a rank: word similarity (it orders similar titles; every literal title match has
-- 1), then newest first, then ID.
WITH hits AS (
    SELECT p.id,
           CASE WHEN lower(p.title) = lower(@q::text) THEN 0
                WHEN starts_with(lower(p.title), lower(@q::text)) THEN 1
                ELSE 2 END AS rank
    FROM projects p
    WHERE NOT @fuzzy::boolean AND p.title ILIKE @pattern::text
    UNION ALL
    SELECT p.id, 3 FROM projects p WHERE @fuzzy::boolean AND p.title %> @q::text
    UNION ALL
    SELECT ps.project_id, 4
    FROM skills s
    JOIN project_skills ps ON ps.skill_id = s.id
    WHERE s.name::text ILIKE @pattern::text
    UNION ALL
    SELECT p.id, 5 FROM projects p WHERE p.description ILIKE @pattern::text
), best AS (
    SELECT id, min(rank) AS rank FROM hits GROUP BY id
)
SELECT p.id, b.rank::int AS rank, p.title, left(p.description, 300)::text AS description, p.status::text AS status,
       p.created_at, p.updated_at,
       coalesce((SELECT m.role::text FROM project_members m WHERE m.project_id = p.id AND m.user_id = @viewer_id::uuid), '')::text AS my_role,
       o.user_id AS owner_id,
       coalesce(op.full_name, '')::text AS owner_name, op.headline AS owner_headline, op.photo_file_id AS owner_photo
FROM best b
JOIN projects p ON p.id = b.id
JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
JOIN users u ON u.id = o.user_id
LEFT JOIN profiles op ON op.user_id = o.user_id
WHERE p.status = 'open' AND u.email_verified_at IS NOT NULL AND u.suspended_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM blocks x WHERE x.blocker_id = o.user_id AND x.blocked_id = @viewer_id::uuid)
ORDER BY b.rank, word_similarity(@q::text, p.title) DESC, p.created_at DESC, p.id
LIMIT @lim OFFSET @off;

-- Project reports (FR-15.11).

-- name: InsertProjectReport :one
INSERT INTO project_reports (project_id, reporter_id, reason, details)
VALUES (@project_id, @reporter_id, @reason, sqlc.narg(details))
RETURNING id, created_at;

-- name: CountProjectReports :one
SELECT count(*)::int FROM project_reports WHERE (resolved_at IS NULL) = @open::boolean;

-- name: ListProjectReports :many
-- Newest first; served by project_reports_status_idx.
SELECT r.id, r.project_id, p.title AS project_title, r.reason, r.details, r.created_at,
       r.resolved_at, r.outcome, r.resolution_note,
       r.reporter_id, coalesce(rp.full_name, '')::text AS reporter_name, rp.headline AS reporter_headline,
       rp.photo_file_id AS reporter_photo, ru.role::text AS reporter_role,
       o.user_id AS owner_id, coalesce(op.full_name, '')::text AS owner_name, op.headline AS owner_headline,
       op.photo_file_id AS owner_photo, ou.role::text AS owner_role
FROM project_reports r
JOIN projects p ON p.id = r.project_id
JOIN users ru ON ru.id = r.reporter_id
LEFT JOIN profiles rp ON rp.user_id = r.reporter_id
JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
JOIN users ou ON ou.id = o.user_id
LEFT JOIN profiles op ON op.user_id = o.user_id
WHERE (r.resolved_at IS NULL) = @open::boolean
ORDER BY r.created_at DESC, r.id DESC
LIMIT @lim OFFSET @off;

-- name: GetProjectReport :one
SELECT r.id, r.project_id, p.title AS project_title, r.reason, r.details, r.created_at,
       r.resolved_at, r.outcome, r.resolution_note,
       r.reporter_id, coalesce(rp.full_name, '')::text AS reporter_name, rp.headline AS reporter_headline,
       rp.photo_file_id AS reporter_photo, ru.role::text AS reporter_role,
       o.user_id AS owner_id, coalesce(op.full_name, '')::text AS owner_name, op.headline AS owner_headline,
       op.photo_file_id AS owner_photo, ou.role::text AS owner_role
FROM project_reports r
JOIN projects p ON p.id = r.project_id
JOIN users ru ON ru.id = r.reporter_id
LEFT JOIN profiles rp ON rp.user_id = r.reporter_id
JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
JOIN users ou ON ou.id = o.user_id
LEFT JOIN profiles op ON op.user_id = o.user_id
WHERE r.id = @id;

-- name: ReportedProject :one
-- The reported project for an administrator, whatever its status (FR-15.11).
SELECT p.id, p.title, p.description, p.abstract, p.status::text AS status, p.created_at, p.updated_at,
       (SELECT count(*) FROM project_members m WHERE m.project_id = p.id)::int AS member_count
FROM projects p
WHERE p.id = @id;

-- name: ResolveProjectReport :execrows
UPDATE project_reports
SET resolved_at = clock_timestamp(), resolved_by = @admin_id, outcome = @outcome, resolution_note = sqlc.narg(note)
WHERE id = @id AND resolved_at IS NULL;

-- name: ProjectReportExists :one
SELECT EXISTS (SELECT 1 FROM project_reports WHERE id = @id);

-- Hybrid project search (AI-5.1 for projects; slice 19, chunk 19.2): the strong title matches
-- and the candidates by meaning, with the keyword search's visibility rule (open, owner
-- verified and not suspended, owner has not blocked the searcher). Like SearchProjectsPage,
-- they order by match and similarity only, never by member counts (AI-2.4).

-- name: SetHNSWIterativeScan :exec
-- HNSW iterative scan (pgvector 0.8): when the filters drop rows the index returned, the
-- scan goes on until LIMIT rows pass. relaxed_order may return rows slightly out of order, so
-- SemanticProjectCandidates sorts again. Local to the transaction.
SELECT set_config('hnsw.iterative_scan', 'relaxed_order', true);

-- name: SetHNSWEfSearch :exec
SELECT set_config('hnsw.ef_search', @ef_search::text, true);

-- name: StrongTitleMatches :many
-- Visible open projects whose title equals q, or starts with q when prefix_ok, ignoring
-- letter case and accents (fold_name, migration 000030). prefix_pattern is the LIKE pattern
-- of q followed by %, with \ % _ escaped. Exact matches first, then the keyword order within
-- the title ranks: newest first, then ID.
SELECT p.id, p.title, left(p.description, 300)::text AS description, p.status::text AS status,
       p.created_at, p.updated_at,
       coalesce((SELECT m.role::text FROM project_members m WHERE m.project_id = p.id AND m.user_id = @viewer_id::uuid), '')::text AS my_role,
       o.user_id AS owner_id,
       coalesce(op.full_name, '')::text AS owner_name, op.headline AS owner_headline, op.photo_file_id AS owner_photo,
       (fold_name(p.title) = fold_name(@q::text))::boolean AS exact
FROM projects p
JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
JOIN users u ON u.id = o.user_id
LEFT JOIN profiles op ON op.user_id = o.user_id
WHERE (fold_name(p.title) = fold_name(@q::text)
       OR (@prefix_ok::boolean AND fold_name(p.title) LIKE fold_name(@prefix_pattern::text)))
  AND p.status = 'open' AND u.email_verified_at IS NOT NULL AND u.suspended_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM blocks x WHERE x.blocker_id = o.user_id AND x.blocked_id = @viewer_id::uuid)
ORDER BY exact DESC, p.created_at DESC, p.id
LIMIT @lim;

-- name: SemanticProjectCandidates :many
-- The lim visible open projects whose embedding is closest in cosine distance to the query
-- vector, through project_embeddings_hnsw_idx, among embeddings of the query's model. The
-- order is the cosine distance and nothing else, then the ID for ties.
WITH cand AS MATERIALIZED (
    SELECT e.project_id, (e.embedding <=> CAST(@embedding::text AS vector))::float8 AS distance
    FROM project_embeddings e
    JOIN projects p ON p.id = e.project_id
    JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
    JOIN users u ON u.id = o.user_id
    WHERE e.model = @model::text
      AND p.status = 'open' AND u.email_verified_at IS NOT NULL AND u.suspended_at IS NULL
      AND NOT EXISTS (SELECT 1 FROM blocks x WHERE x.blocker_id = o.user_id AND x.blocked_id = @viewer_id::uuid)
    ORDER BY e.embedding <=> CAST(@embedding::text AS vector)
    LIMIT @lim
)
SELECT p.id, p.title, left(p.description, 300)::text AS description, p.status::text AS status,
       p.created_at, p.updated_at,
       coalesce((SELECT m.role::text FROM project_members m WHERE m.project_id = p.id AND m.user_id = @viewer_id::uuid), '')::text AS my_role,
       o.user_id AS owner_id,
       coalesce(op.full_name, '')::text AS owner_name, op.headline AS owner_headline, op.photo_file_id AS owner_photo
FROM cand
JOIN projects p ON p.id = cand.project_id
JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
LEFT JOIN profiles op ON op.user_id = o.user_id
ORDER BY cand.distance, cand.project_id;
