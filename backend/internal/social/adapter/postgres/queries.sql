-- Connection requests and connections (FR-7). A user is visible when verified and not
-- suspended, the rule of the profile view (FR-4.5); lists leave out other users who are not.

-- name: UserVisible :one
SELECT EXISTS (
    SELECT 1 FROM users
    WHERE id = @id AND email_verified_at IS NOT NULL AND suspended_at IS NULL
) AS visible;

-- name: FindPerson :one
SELECT u.id, COALESCE(p.full_name, '')::text AS full_name, p.headline, p.institution, p.photo_file_id
FROM users u
LEFT JOIN profiles p ON p.user_id = u.id
WHERE u.id = @id;

-- name: CreateRequest :one
-- Stores a pending request unless the pair already has a pending request or a connection
-- in either direction: connections_live_pair_uq decides, so of two requests sent at the
-- same moment one is stored and the other returns no row (FR-7.2).
INSERT INTO connections (requester_id, receiver_id)
VALUES (@requester_id, @receiver_id)
ON CONFLICT ((LEAST(requester_id, receiver_id)), (GREATEST(requester_id, receiver_id)))
    WHERE status IN ('pending', 'accepted')
    DO NOTHING
RETURNING id, created_at;

-- name: FindLiveLink :one
-- The pending request or connection between two users, in either direction.
SELECT id, requester_id, receiver_id, status
FROM connections
WHERE LEAST(requester_id, receiver_id) = LEAST(@a::uuid, @b::uuid)
  AND GREATEST(requester_id, receiver_id) = GREATEST(@a::uuid, @b::uuid)
  AND status IN ('pending', 'accepted');

-- name: ListLiveLinks :many
-- The pending request or connection between the viewer and each of the other users, in
-- one statement for a whole page; the join matches connections_live_pair_uq.
SELECT c.id, c.requester_id, c.receiver_id, c.status
FROM unnest(@other_ids::uuid[]) AS o(id)
JOIN connections c
  ON LEAST(c.requester_id, c.receiver_id) = LEAST(@viewer_id::uuid, o.id)
 AND GREATEST(c.requester_id, c.receiver_id) = GREATEST(@viewer_id::uuid, o.id)
 AND c.status IN ('pending', 'accepted');

-- name: AnswerRequest :one
-- Accepts or rejects a pending request the receiver received from a visible sender.
UPDATE connections c
SET status = @status, responded_at = clock_timestamp()
FROM users s
WHERE c.id = @id AND c.receiver_id = @receiver_id AND c.status = 'pending'
  AND s.id = c.requester_id AND s.email_verified_at IS NOT NULL AND s.suspended_at IS NULL
RETURNING c.id, c.requester_id, c.receiver_id, c.created_at, c.responded_at;

-- name: CancelRequest :one
-- A cancelled request is deleted: the enum has no cancelled state, and nothing else
-- refers to it. It returns the receiver, whose recommendations change with it.
DELETE FROM connections
WHERE id = @id AND requester_id = @requester_id AND status = 'pending'
RETURNING receiver_id;

-- name: RemoveConnection :execrows
DELETE FROM connections
WHERE LEAST(requester_id, receiver_id) = LEAST(@user_id::uuid, @other_id::uuid)
  AND GREATEST(requester_id, receiver_id) = GREATEST(@user_id::uuid, @other_id::uuid)
  AND status = 'accepted';

-- name: ListConnections :many
-- Most recently connected first, leaving out anyone who blocked the viewer (FR-16.9). On
-- the caller's own list the viewer is the user.
SELECT c.id, c.responded_at, o.id AS other_id, COALESCE(p.full_name, '')::text AS full_name,
       p.headline, p.institution, p.photo_file_id
FROM connections c
JOIN users o ON o.id = CASE WHEN c.requester_id = @user_id::uuid THEN c.receiver_id ELSE c.requester_id END
LEFT JOIN profiles p ON p.user_id = o.id
WHERE (c.requester_id = @user_id::uuid OR c.receiver_id = @user_id::uuid) AND c.status = 'accepted'
  AND o.email_verified_at IS NOT NULL AND o.suspended_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = o.id AND b.blocked_id = @viewer_id::uuid)
ORDER BY c.responded_at DESC, c.id DESC
LIMIT @lim OFFSET @off;

-- Connection search (FR-16.6, the group member picker): the caller's own connections, as
-- ListConnections shows them, matched on name, institution and headline by the rule of the
-- researcher search (profile queries.sql). Parameters: q the trimmed text; pattern
-- '%' || q || '%' with \ % _ escaped, for ILIKE; fuzzy true when no connection's name contains
-- q, so names similar to it (full_name %> q, the threshold SetWordSimilarityThreshold sets in
-- the same transaction) match instead. Ranks: 0 exact name, 1 name prefix, 2 name contains,
-- 3 similar name (most similar first), 4 institution, 5 headline; then newest first. A person has thousands of connections at most,
-- so the connection list is read first and matched in memory.

-- name: SetWordSimilarityThreshold :exec
-- Transaction-local (is_local true), so it ends with the search's transaction.
SELECT set_config('pg_trgm.word_similarity_threshold', @threshold::text, true);

-- name: AnyConnectionNameContains :one
SELECT EXISTS (
    SELECT 1
    FROM connections c
    JOIN users o ON o.id = CASE WHEN c.requester_id = @user_id::uuid THEN c.receiver_id ELSE c.requester_id END
    JOIN profiles p ON p.user_id = o.id
    WHERE (c.requester_id = @user_id::uuid OR c.receiver_id = @user_id::uuid) AND c.status = 'accepted'
      AND o.email_verified_at IS NOT NULL AND o.suspended_at IS NULL
      AND NOT EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = o.id AND b.blocked_id = @user_id::uuid)
      AND p.full_name ILIKE @pattern::text
)::boolean;

-- name: SearchConnections :many
WITH mine AS (
    SELECT c.id, c.responded_at, o.id AS other_id, COALESCE(p.full_name, '')::text AS full_name,
           p.headline, p.institution, p.photo_file_id
    FROM connections c
    JOIN users o ON o.id = CASE WHEN c.requester_id = @user_id::uuid THEN c.receiver_id ELSE c.requester_id END
    LEFT JOIN profiles p ON p.user_id = o.id
    WHERE (c.requester_id = @user_id::uuid OR c.receiver_id = @user_id::uuid) AND c.status = 'accepted'
      AND o.email_verified_at IS NOT NULL AND o.suspended_at IS NULL
      AND NOT EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = o.id AND b.blocked_id = @user_id::uuid)
), ranked AS (
    SELECT mine.*,
           CASE WHEN NOT @fuzzy::boolean AND lower(full_name) = lower(@q::text) THEN 0
                WHEN NOT @fuzzy::boolean AND starts_with(lower(full_name), lower(@q::text)) THEN 1
                WHEN NOT @fuzzy::boolean AND full_name ILIKE @pattern::text THEN 2
                WHEN @fuzzy::boolean AND full_name %> @q::text THEN 3
                WHEN institution ILIKE @pattern::text THEN 4
                WHEN headline ILIKE @pattern::text THEN 5
           END AS rank
    FROM mine
)
SELECT id, responded_at, other_id, full_name, headline, institution, photo_file_id, count(*) OVER ()::bigint AS total
FROM ranked
WHERE rank IS NOT NULL
ORDER BY rank, CASE WHEN rank = 3 THEN word_similarity(@q::text, full_name) END DESC NULLS LAST, responded_at DESC, id DESC
LIMIT @lim OFFSET @off;

-- name: CountConnections :one
SELECT count(*)
FROM connections c
JOIN users o ON o.id = CASE WHEN c.requester_id = @user_id::uuid THEN c.receiver_id ELSE c.requester_id END
WHERE (c.requester_id = @user_id::uuid OR c.receiver_id = @user_id::uuid) AND c.status = 'accepted'
  AND o.email_verified_at IS NOT NULL AND o.suspended_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = o.id AND b.blocked_id = @viewer_id::uuid);

-- name: ListReceived :many
-- Pending requests the user received, newest first; the other user is the sender.
SELECT c.id, c.created_at, o.id AS other_id, COALESCE(p.full_name, '')::text AS full_name,
       p.headline, p.institution, p.photo_file_id
FROM connections c
JOIN users o ON o.id = c.requester_id
LEFT JOIN profiles p ON p.user_id = o.id
WHERE c.receiver_id = @user_id AND c.status = 'pending'
  AND o.email_verified_at IS NOT NULL AND o.suspended_at IS NULL
ORDER BY c.created_at DESC, c.id DESC
LIMIT @lim OFFSET @off;

-- name: CountReceived :one
SELECT count(*)
FROM connections c
JOIN users o ON o.id = c.requester_id
WHERE c.receiver_id = @user_id AND c.status = 'pending'
  AND o.email_verified_at IS NOT NULL AND o.suspended_at IS NULL;

-- name: ListSent :many
-- Pending requests the user sent, newest first; the other user is the receiver.
SELECT c.id, c.created_at, o.id AS other_id, COALESCE(p.full_name, '')::text AS full_name,
       p.headline, p.institution, p.photo_file_id
FROM connections c
JOIN users o ON o.id = c.receiver_id
LEFT JOIN profiles p ON p.user_id = o.id
WHERE c.requester_id = @user_id AND c.status = 'pending'
  AND o.email_verified_at IS NOT NULL AND o.suspended_at IS NULL
ORDER BY c.created_at DESC, c.id DESC
LIMIT @lim OFFSET @off;

-- name: CountSent :one
SELECT count(*)
FROM connections c
JOIN users o ON o.id = c.receiver_id
WHERE c.requester_id = @user_id AND c.status = 'pending'
  AND o.email_verified_at IS NOT NULL AND o.suspended_at IS NULL;

-- name: LockPair :exec
-- Runs blocks, connection requests and sends between two users one at a time (FR-16.9):
-- each takes this lock first in its transaction and reads the block in a later statement,
-- so nothing can pass the check once a block has committed. The key is the same whichever
-- way round the two are given.
SELECT pg_advisory_xact_lock(hashtextextended(LEAST(@a::uuid, @b::uuid)::text || GREATEST(@a::uuid, @b::uuid)::text, 0));

-- name: BlockBetween :one
SELECT EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = @me::uuid AND b.blocked_id = @other::uuid)::boolean AS by_me,
       EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = @other::uuid AND b.blocked_id = @me::uuid)::boolean AS by_them;

-- name: BlockedAmong :many
-- Which of the IDs are in a block with the user, either way: a blocked pair cannot be put in a
-- new group by either of them (FR-16.6). blocks_blocked_idx serves the second half.
SELECT b.blocked_id AS user_id FROM blocks b WHERE b.blocker_id = @user_id::uuid AND b.blocked_id = ANY(@ids::uuid[])
UNION
SELECT b.blocker_id FROM blocks b WHERE b.blocked_id = @user_id::uuid AND b.blocker_id = ANY(@ids::uuid[]);

-- name: AddBlock :execrows
INSERT INTO blocks (blocker_id, blocked_id) VALUES (@blocker_id, @blocked_id)
ON CONFLICT (blocker_id, blocked_id) DO NOTHING;

-- name: RemoveBlock :exec
DELETE FROM blocks WHERE blocker_id = @blocker_id AND blocked_id = @blocked_id;

-- name: RemoveLiveLink :exec
-- The pending request or connection between two users, in either direction. A rejected row
-- stays as history (FR-7.6).
DELETE FROM connections
WHERE LEAST(requester_id, receiver_id) = LEAST(@a::uuid, @b::uuid)
  AND GREATEST(requester_id, receiver_id) = GREATEST(@a::uuid, @b::uuid)
  AND status IN ('pending', 'accepted');

-- name: ListBlocks :many
-- Most recently blocked first; suspended and unverified users are left out, as in every list.
SELECT b.created_at, o.id AS other_id, COALESCE(p.full_name, '')::text AS full_name,
       p.headline, p.institution, p.photo_file_id
FROM blocks b
JOIN users o ON o.id = b.blocked_id
LEFT JOIN profiles p ON p.user_id = o.id
WHERE b.blocker_id = @user_id
  AND o.email_verified_at IS NOT NULL AND o.suspended_at IS NULL
ORDER BY b.created_at DESC, o.id DESC
LIMIT @lim OFFSET @off;

-- name: CountBlocks :one
SELECT count(*)
FROM blocks b
JOIN users o ON o.id = b.blocked_id
WHERE b.blocker_id = @user_id
  AND o.email_verified_at IS NOT NULL AND o.suspended_at IS NULL;
