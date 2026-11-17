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

-- Membership (FR-11.1 to FR-11.5, FR-10.5; slice 20, chunk 20.1). A join request and an
-- invitation are project_join_requests rows of kind 'request' and 'invitation'; one row per
-- user and project may be pending (project_join_requests_pending_uq). The writes run in the
-- usecase's transaction after LockProject. "Available" means verified and not suspended now
-- (a suspension whose end has passed no longer counts).

-- name: MembershipRelation :one
-- What decides whether the user may ask to join or be invited: the account, a block either
-- way with the owner, the role, and the pending request or invitation (if any).
SELECT EXISTS (SELECT 1 FROM users u WHERE u.id = @user_id::uuid)::boolean AS user_exists,
       EXISTS (SELECT 1 FROM users u
               WHERE u.id = @user_id::uuid AND u.email_verified_at IS NOT NULL
                 AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= now())))::boolean AS available,
       EXISTS (SELECT 1 FROM blocks b
               JOIN project_members o ON o.project_id = @project_id::uuid AND o.role = 'owner'
               WHERE (b.blocker_id = o.user_id AND b.blocked_id = @user_id::uuid)
                  OR (b.blocker_id = @user_id::uuid AND b.blocked_id = o.user_id))::boolean AS blocked,
       coalesce((SELECT m.role::text FROM project_members m
                 WHERE m.project_id = @project_id::uuid AND m.user_id = @user_id::uuid), '')::text AS role;

-- name: PendingJoinRequest :one
SELECT id, project_id, user_id, kind::text AS kind, status::text AS status, message, coalesce(role::text, '')::text AS role, created_at, decided_at
FROM project_join_requests
WHERE project_id = @project_id AND user_id = @user_id AND status = 'pending';

-- name: InsertJoinRequest :one
INSERT INTO project_join_requests (project_id, user_id, kind, message, role)
VALUES (@project_id, @user_id, (@kind::text)::join_request_kind, sqlc.narg(message), (sqlc.narg(role)::text)::project_role)
RETURNING id, project_id, user_id, kind::text AS kind, status::text AS status, message, coalesce(role::text, '')::text AS role, created_at, decided_at;

-- name: LockJoinRequest :one
SELECT id, project_id, user_id, kind::text AS kind, status::text AS status, message, coalesce(role::text, '')::text AS role, created_at, decided_at
FROM project_join_requests
WHERE id = @id AND project_id = @project_id AND kind = (@kind::text)::join_request_kind
FOR UPDATE;

-- name: DecideJoinRequest :exec
UPDATE project_join_requests
SET status = (@status::text)::join_request_status, decided_at = clock_timestamp()
WHERE id = @id AND status = 'pending';

-- name: DeleteJoinRequest :exec
DELETE FROM project_join_requests WHERE id = @id;

-- name: InsertMember :one
INSERT INTO project_members (project_id, user_id, role)
VALUES (@project_id, @user_id, (@role::text)::project_role)
RETURNING joined_at;

-- name: ProjectPerson :one
SELECT u.id, coalesce(p.full_name, '')::text AS full_name, p.headline, p.photo_file_id
FROM users u
LEFT JOIN profiles p ON p.user_id = u.id
WHERE u.id = @user_id;

-- name: ProjectMember :one
SELECT m.user_id, m.role::text AS role, m.joined_at,
       coalesce(p.full_name, '')::text AS full_name, p.headline, p.photo_file_id
FROM project_members m
LEFT JOIN profiles p ON p.user_id = m.user_id
WHERE m.project_id = @project_id AND m.user_id = @user_id;

-- name: SetMemberRole :exec
-- During a transfer the owner is demoted first: project_members_one_owner_uq is checked at
-- every statement, the owner trigger at commit.
UPDATE project_members SET role = (@role::text)::project_role
WHERE project_id = @project_id AND user_id = @user_id;

-- name: DeleteMember :exec
DELETE FROM project_members WHERE project_id = @project_id AND user_id = @user_id;

-- name: CountProjectMembers :one
SELECT count(*)::int FROM project_members WHERE project_id = @project_id;

-- name: ProjectMembers :many
-- The owner first, then in the order they joined.
SELECT m.user_id, m.role::text AS role, m.joined_at,
       coalesce(p.full_name, '')::text AS full_name, p.headline, p.photo_file_id
FROM project_members m
LEFT JOIN profiles p ON p.user_id = m.user_id
WHERE m.project_id = @project_id
ORDER BY (m.role = 'owner') DESC, m.joined_at, m.user_id
LIMIT @lim OFFSET @off;

-- name: CountProjectJoinRequests :one
SELECT count(*)::int
FROM project_join_requests j
JOIN users u ON u.id = j.user_id
WHERE j.project_id = @project_id AND j.kind = (@kind::text)::join_request_kind
  AND u.email_verified_at IS NOT NULL
  AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= now()));

-- name: ProjectJoinRequests :many
-- Pending first, oldest first (the order they arrived); then decided, most recently decided
-- first. Rows of users who are now unverified or suspended are left out.
SELECT j.id, j.project_id, j.user_id, j.kind::text AS kind, j.status::text AS status, j.message, coalesce(j.role::text, '')::text AS role,
       j.created_at, j.decided_at,
       coalesce(p.full_name, '')::text AS full_name, p.headline, p.photo_file_id
FROM project_join_requests j
JOIN users u ON u.id = j.user_id
LEFT JOIN profiles p ON p.user_id = j.user_id
WHERE j.project_id = @project_id AND j.kind = (@kind::text)::join_request_kind
  AND u.email_verified_at IS NOT NULL
  AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= now()))
ORDER BY (j.status = 'pending') DESC,
         CASE WHEN j.status = 'pending' THEN j.created_at END ASC,
         j.decided_at DESC, j.id
LIMIT @lim OFFSET @off;

-- name: CountMyJoinRequests :one
SELECT count(*)::int FROM project_join_requests WHERE user_id = @user_id AND kind = 'request';

-- name: MyJoinRequests :many
-- The user's own requests, newest first, with the project they are about.
SELECT j.id, j.project_id, j.user_id, j.kind::text AS kind, j.status::text AS status, j.message, coalesce(j.role::text, '')::text AS role,
       j.created_at, j.decided_at,
       pr.title, pr.status::text AS project_status, o.user_id AS owner_id,
       coalesce(op.full_name, '')::text AS owner_name, op.headline AS owner_headline, op.photo_file_id AS owner_photo
FROM project_join_requests j
JOIN projects pr ON pr.id = j.project_id
JOIN project_members o ON o.project_id = pr.id AND o.role = 'owner'
LEFT JOIN profiles op ON op.user_id = o.user_id
WHERE j.user_id = @user_id AND j.kind = 'request'
ORDER BY j.created_at DESC, j.id DESC
LIMIT @lim OFFSET @off;

-- name: CountMyInvitations :one
SELECT count(*)::int
FROM project_join_requests j
JOIN project_members o ON o.project_id = j.project_id AND o.role = 'owner'
JOIN users ou ON ou.id = o.user_id
WHERE j.user_id = @user_id AND j.kind = 'invitation' AND j.status = 'pending'
  AND ou.email_verified_at IS NOT NULL
  AND (ou.suspended_at IS NULL OR (ou.suspended_until IS NOT NULL AND ou.suspended_until <= now()))
  AND NOT EXISTS (SELECT 1 FROM blocks b
                  WHERE (b.blocker_id = o.user_id AND b.blocked_id = j.user_id)
                     OR (b.blocker_id = j.user_id AND b.blocked_id = o.user_id));

-- name: MyInvitations :many
-- The user's pending invitations, newest first; one whose owner is now unverified, suspended
-- or blocked either way is left out until that changes.
SELECT j.id, j.project_id, j.user_id, j.kind::text AS kind, j.status::text AS status, j.message, coalesce(j.role::text, '')::text AS role,
       j.created_at, j.decided_at,
       pr.title, pr.status::text AS project_status, o.user_id AS owner_id,
       coalesce(op.full_name, '')::text AS owner_name, op.headline AS owner_headline, op.photo_file_id AS owner_photo
FROM project_join_requests j
JOIN projects pr ON pr.id = j.project_id
JOIN project_members o ON o.project_id = pr.id AND o.role = 'owner'
JOIN users ou ON ou.id = o.user_id
LEFT JOIN profiles op ON op.user_id = o.user_id
WHERE j.user_id = @user_id AND j.kind = 'invitation' AND j.status = 'pending'
  AND ou.email_verified_at IS NOT NULL
  AND (ou.suspended_at IS NULL OR (ou.suspended_until IS NOT NULL AND ou.suspended_until <= now()))
  AND NOT EXISTS (SELECT 1 FROM blocks b
                  WHERE (b.blocker_id = o.user_id AND b.blocked_id = j.user_id)
                     OR (b.blocker_id = j.user_id AND b.blocked_id = o.user_id))
ORDER BY j.created_at DESC, j.id DESC
LIMIT @lim OFFSET @off;

-- name: WithdrawJoinRequestsBetween :many
-- After a block (FR-16.9): the pending requests and invitations of either user on a project
-- the other owns.
DELETE FROM project_join_requests j
USING project_members o
WHERE o.project_id = j.project_id AND o.role = 'owner' AND j.status = 'pending'
  AND ((j.user_id = @a AND o.user_id = @b) OR (j.user_id = @b AND o.user_id = @a))
RETURNING j.project_id, j.user_id;

-- name: VisibleProjectTitles :many
-- The titles of the projects viewer_id may see (domain.Access.Visible): a member sees any;
-- anyone else an open or closed project whose owner is verified, not suspended and has not
-- blocked them.
SELECT p.id, p.title
FROM projects p
JOIN project_members o ON o.project_id = p.id AND o.role = 'owner'
JOIN users ou ON ou.id = o.user_id
WHERE p.id = ANY (@ids::uuid[])
  AND (EXISTS (SELECT 1 FROM project_members m WHERE m.project_id = p.id AND m.user_id = @viewer_id::uuid)
       OR (p.status <> 'archived' AND ou.email_verified_at IS NOT NULL AND ou.suspended_at IS NULL
           AND NOT EXISTS (SELECT 1 FROM blocks x WHERE x.blocker_id = o.user_id AND x.blocked_id = @viewer_id::uuid)));

-- ---------------------------------------------------------------------------
-- Project files (FR-10.4, FR-12.1, FR-12.2, FR-12.6; slice 21, migration 000035)
-- ---------------------------------------------------------------------------

-- name: ProjectFiles :many
-- The whole tree, at most 200 entries (project_files_max), by path ignoring letter case.
SELECT f.id, f.path, f.kind::text AS kind, f.size_bytes, f.version, f.updated_at,
       sf.content_type, sf.scan_status,
       f.updated_by, coalesce(up.full_name, '')::text AS updater_name, up.headline AS updater_headline,
       up.photo_file_id AS updater_photo
FROM project_files f
LEFT JOIN stored_files sf ON sf.id = f.stored_file_id
LEFT JOIN profiles up ON up.user_id = f.updated_by
WHERE f.project_id = @project_id
ORDER BY lower(f.path), f.path;

-- name: MainFilePath :one
SELECT main_file_path FROM projects WHERE id = @id;

-- name: ProjectFile :one
SELECT f.id, f.path, f.kind::text AS kind, f.size_bytes, f.version, f.updated_at, f.content,
       sf.object_key, sf.content_type, sf.scan_status,
       f.updated_by, coalesce(up.full_name, '')::text AS updater_name, up.headline AS updater_headline,
       up.photo_file_id AS updater_photo
FROM project_files f
LEFT JOIN stored_files sf ON sf.id = f.stored_file_id
LEFT JOIN profiles up ON up.user_id = f.updated_by
WHERE f.project_id = @project_id AND f.id = @id;

-- name: LockProjectFile :one
-- ProjectFile without the people, with the file row locked until the transaction ends.
SELECT f.id, f.path, f.kind::text AS kind, f.size_bytes, f.version, f.updated_at, f.content,
       sf.content_type
FROM project_files f
LEFT JOIN stored_files sf ON sf.id = f.stored_file_id
WHERE f.project_id = @project_id AND f.id = @id
FOR UPDATE OF f;

-- name: ProjectFileAt :one
SELECT f.id, f.kind::text AS kind
FROM project_files f
WHERE f.project_id = @project_id AND lower(f.path) = lower(@path::text);

-- name: InsertProjectFolder :one
INSERT INTO project_files (project_id, path, kind, updated_by)
VALUES (@project_id, @path, 'folder', @updated_by)
RETURNING id;

-- name: InsertProjectText :one
INSERT INTO project_files (project_id, path, kind, content, size_bytes, updated_by)
VALUES (@project_id, @path, 'text', @content::text, octet_length(@content::text), @updated_by)
RETURNING id;

-- name: InsertFigureObject :exec
-- A figure's stored file, owned by the project; the scan worker picks it up (pending).
INSERT INTO stored_files (id, project_id, uploaded_by, purpose, object_key, content_type, size_bytes, width, height)
VALUES (@id, @project_id, @uploaded_by, 'figure', @object_key, @content_type, @size_bytes,
        sqlc.narg(width)::integer, sqlc.narg(height)::integer);

-- name: InsertProjectFigure :one
INSERT INTO project_files (project_id, path, kind, stored_file_id, size_bytes, updated_by)
VALUES (@project_id, @path, 'binary', @stored_file_id, @size_bytes, @updated_by)
RETURNING id;

-- name: SaveProjectFileContent :one
-- A REST save replaces the text outright, so the Yjs state of slice 22 no longer matches it
-- and is cleared; the sync server builds it again from the text.
UPDATE project_files
SET content = @content::text, size_bytes = octet_length(@content::text), version = version + 1,
    ydoc_state = NULL, updated_by = @updated_by, updated_at = now()
WHERE id = @id AND kind = 'text'
RETURNING version;

-- name: MoveProjectFiles :execrows
-- The entry at @old_path and everything under it go to @new_path, in one statement, so the tree check
-- (project_files_parent) sees the result. @old_path is the stored spelling of the entry's path.
UPDATE project_files
SET path = @new_path::text || substr(path, char_length(@old_path::text) + 1),
    updated_by = @updated_by, updated_at = now()
WHERE project_id = @project_id
  AND (lower(path) = lower(@old_path::text)
       OR left(lower(path), char_length(@old_path::text) + 1) = lower(@old_path::text) || '/');

-- name: MoveMainFilePath :exec
-- The project's main file follows its file, or a folder it is in.
UPDATE projects
SET main_file_path = @new_path::text || substr(main_file_path, char_length(@old_path::text) + 1)
WHERE id = @id
  AND (lower(main_file_path) = lower(@old_path::text)
       OR left(lower(main_file_path), char_length(@old_path::text) + 1) = lower(@old_path::text) || '/');

-- name: FigureKeysUnder :many
SELECT sf.object_key
FROM project_files f
JOIN stored_files sf ON sf.id = f.stored_file_id
WHERE f.project_id = @project_id
  AND (lower(f.path) = lower(@path::text)
       OR left(lower(f.path), char_length(@path::text) + 1) = lower(@path::text) || '/');

-- name: DeleteProjectFiles :exec
-- The entry at @path and everything under it, in one statement; project_files_delete_file
-- deletes each figure's stored file, whose trigger queues its object.
DELETE FROM project_files
WHERE project_id = @project_id
  AND (lower(path) = lower(@path::text)
       OR left(lower(path), char_length(@path::text) + 1) = lower(@path::text) || '/');

-- name: ProjectFigureKeys :many
SELECT object_key FROM stored_files WHERE project_id = @project_id;

-- name: CountProjectFiles :one
SELECT count(*)::int FROM project_files WHERE project_id = @project_id;

-- name: ProfileFullName :one
SELECT full_name FROM profiles WHERE user_id = @user_id;

-- name: ProjectsWithoutFiles :many
-- Projects for the main.tex backfill (FR-10.4), in ID order after @after.
SELECT p.id
FROM projects p
WHERE p.id > coalesce(sqlc.narg(after)::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
  AND NOT EXISTS (SELECT 1 FROM project_files f WHERE f.project_id = p.id)
ORDER BY p.id
LIMIT @max_rows;

-- name: ProjectStorageUsed :one
-- The bytes of the project's stored files (FR-12.2 quota), except those removed as malware,
-- whose objects are gone.
SELECT coalesce(sum(size_bytes), 0)::bigint
FROM stored_files
WHERE project_id = @project_id AND scan_status <> 'infected';

-- name: PendingUploadBytes :one
-- The bytes of the project's uploads still being sent (started after @since), held against
-- the quota so several uploads at once cannot pass it.
SELECT coalesce(sum(size_bytes), 0)::bigint
FROM project_figure_uploads
WHERE project_id = @project_id AND created_at > @since AND expires_at > now();

-- name: InsertFigureUpload :one
INSERT INTO project_figure_uploads (project_id, user_id, object_key, path, size_bytes, expires_at)
VALUES (@project_id, @user_id, @object_key, @path, @size_bytes, @expires_at)
RETURNING id;

-- name: FigureUpload :one
-- The user's own upload in the project, while it has not expired.
SELECT id, object_key, path, size_bytes, expires_at
FROM project_figure_uploads
WHERE id = @id AND project_id = @project_id AND user_id = @user_id AND expires_at > now();

-- name: ClaimFigureUpload :execrows
-- Completing takes the upload in the transaction that records the figure: a second
-- completion finds nothing. The trigger queues the upload's object for deletion.
DELETE FROM project_figure_uploads WHERE id = @id AND expires_at > now();

-- name: DeleteExpiredFigureUploads :execrows
-- Abandoned uploads, a batch at a time; the trigger queues each object for deletion.
DELETE FROM project_figure_uploads
WHERE id IN (SELECT id FROM project_figure_uploads
             WHERE expires_at <= now()
             ORDER BY expires_at
             LIMIT @max_rows
             FOR UPDATE SKIP LOCKED);

-- name: ShareLockProjectRole :one
-- The user's role, '' when not a member, with the project row locked shared: content saves
-- run side by side, and wait for a membership change (which locks the row for update).
SELECT coalesce((SELECT m.role::text FROM project_members m WHERE m.project_id = p.id AND m.user_id = @user_id::uuid), '')::text AS role
FROM projects p
WHERE p.id = @id
FOR SHARE OF p;
