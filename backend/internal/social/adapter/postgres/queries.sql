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
SET status = @status, responded_at = now()
FROM users s
WHERE c.id = @id AND c.receiver_id = @receiver_id AND c.status = 'pending'
  AND s.id = c.requester_id AND s.email_verified_at IS NOT NULL AND s.suspended_at IS NULL
RETURNING c.id, c.requester_id, c.receiver_id, c.created_at, c.responded_at;

-- name: CancelRequest :execrows
-- A cancelled request is deleted: the enum has no cancelled state, and nothing else
-- refers to it.
DELETE FROM connections
WHERE id = @id AND requester_id = @requester_id AND status = 'pending';

-- name: RemoveConnection :execrows
DELETE FROM connections
WHERE LEAST(requester_id, receiver_id) = LEAST(@user_id::uuid, @other_id::uuid)
  AND GREATEST(requester_id, receiver_id) = GREATEST(@user_id::uuid, @other_id::uuid)
  AND status = 'accepted';

-- name: ListConnections :many
-- Most recently connected first.
SELECT c.id, c.responded_at, o.id AS other_id, COALESCE(p.full_name, '')::text AS full_name,
       p.headline, p.institution, p.photo_file_id
FROM connections c
JOIN users o ON o.id = CASE WHEN c.requester_id = @user_id::uuid THEN c.receiver_id ELSE c.requester_id END
LEFT JOIN profiles p ON p.user_id = o.id
WHERE (c.requester_id = @user_id::uuid OR c.receiver_id = @user_id::uuid) AND c.status = 'accepted'
  AND o.email_verified_at IS NOT NULL AND o.suspended_at IS NULL
ORDER BY c.responded_at DESC, c.id DESC
LIMIT @lim OFFSET @off;

-- name: CountConnections :one
SELECT count(*)
FROM connections c
JOIN users o ON o.id = CASE WHEN c.requester_id = @user_id::uuid THEN c.receiver_id ELSE c.requester_id END
WHERE (c.requester_id = @user_id::uuid OR c.receiver_id = @user_id::uuid) AND c.status = 'accepted'
  AND o.email_verified_at IS NOT NULL AND o.suspended_at IS NULL;

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
