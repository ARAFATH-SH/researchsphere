-- Messaging (FR-16). A direct conversation has exactly two members, so "the other member"
-- is the one row that is not the caller. Every read names the caller as a member; only
-- ListMessages and the locks do not, and the usecase calls them after FindConversation.

-- name: UserVisible :one
SELECT EXISTS (
    SELECT 1 FROM users u
    WHERE u.id = @id AND u.email_verified_at IS NOT NULL AND u.suspended_at IS NULL
)::boolean AS visible;

-- name: FindSent :one
SELECT m.id, m.conversation_id, m.sender_id, m.body, m.client_id, m.created_at, c.state
FROM messages m
JOIN conversations c ON c.id = m.conversation_id
WHERE m.sender_id = @sender_id AND m.client_id = @client_id;

-- name: DirectExists :one
SELECT EXISTS (
    SELECT 1 FROM conversations c
    WHERE c.user_low = LEAST(@a::uuid, @b::uuid) AND c.user_high = GREATEST(@a::uuid, @b::uuid)
)::boolean AS present;

-- name: CreateDirect :one
-- conversations_pair_uq settles two first messages sent at the same moment.
INSERT INTO conversations (kind, state, requested_by, user_low, user_high)
VALUES ('direct', @state, sqlc.narg('requested_by'), LEAST(@a::uuid, @b::uuid), GREATEST(@a::uuid, @b::uuid))
ON CONFLICT (user_low, user_high) DO NOTHING
RETURNING id, state, requested_by, last_message_at;

-- name: AddMember :exec
INSERT INTO conversation_members (conversation_id, user_id) VALUES (@conversation_id, @user_id);

-- name: LockDirect :one
SELECT id, state, requested_by, last_message_at
FROM conversations
WHERE user_low = LEAST(@a::uuid, @b::uuid) AND user_high = GREATEST(@a::uuid, @b::uuid)
FOR UPDATE;

-- name: LockConversation :one
SELECT id, state, requested_by, last_message_at FROM conversations WHERE id = @id FOR UPDATE;

-- name: ActivateRequest :exec
UPDATE conversations
SET state = 'active'
WHERE user_low = LEAST(@a::uuid, @b::uuid) AND user_high = GREATEST(@a::uuid, @b::uuid)
  AND state = 'request';

-- name: InsertMessage :one
-- ON CONFLICT also settles two sends of one client_id at the same moment. The ID comes
-- from uuid_v7() here, after the conversation's row is locked, so IDs follow commit order
-- within a conversation.
INSERT INTO messages (conversation_id, sender_id, body, client_id)
VALUES (@conversation_id, @sender_id, @body, @client_id)
ON CONFLICT (sender_id, client_id) DO NOTHING
RETURNING id, conversation_id, sender_id, body, client_id, created_at;

-- name: TouchConversation :exec
UPDATE conversations SET last_message_at = @last_message_at WHERE id = @id;

-- name: MoveReadMarker :execrows
-- Forward only: a marker never moves back to an earlier message.
UPDATE conversation_members
SET last_read_message_id = @message_id, last_read_at = now()
WHERE conversation_id = @conversation_id AND user_id = @user_id
  AND (last_read_message_id IS NULL OR last_read_message_id < @message_id);

-- name: FindConversation :one
SELECT c.id, c.state, c.requested_by, c.last_message_at,
       o.user_id AS other_id, o.last_read_message_id AS other_read_id
FROM conversation_members me
JOIN conversations c ON c.id = me.conversation_id
JOIN conversation_members o ON o.conversation_id = c.id AND o.user_id <> me.user_id
WHERE me.conversation_id = @id AND me.user_id = @user_id;

-- name: FindItem :one
SELECT c.id, c.state, c.requested_by, c.last_message_at,
       o.user_id AS other_id, o.last_read_message_id AS other_read_id,
       COALESCE(p.full_name, '')::text AS full_name, p.headline, p.photo_file_id,
       (u.email_verified_at IS NOT NULL AND u.suspended_at IS NULL)::boolean AS other_available,
       lm.sender_id AS last_sender_id, COALESCE(lm.preview, '')::text AS last_preview, lm.created_at AS last_created_at,
       unread.n AS unread_count
FROM conversation_members me
JOIN conversations c ON c.id = me.conversation_id
JOIN conversation_members o ON o.conversation_id = c.id AND o.user_id <> me.user_id
JOIN users u ON u.id = o.user_id
LEFT JOIN profiles p ON p.user_id = u.id
LEFT JOIN LATERAL (
    SELECT m.sender_id, left(m.body, 120) AS preview, m.created_at
    FROM messages m WHERE m.conversation_id = c.id
    ORDER BY m.id DESC LIMIT 1
) lm ON true
CROSS JOIN LATERAL (
    SELECT count(*) AS n
    FROM messages m
    WHERE m.conversation_id = c.id AND m.sender_id <> me.user_id
      AND (me.last_read_message_id IS NULL OR m.id > me.last_read_message_id)
) unread
WHERE me.conversation_id = @id AND me.user_id = @user_id;

-- name: ListItems :many
-- The columns and joins of FindItem. A request whose sender is no longer verified or is
-- suspended cannot be answered, so it leaves the requests box and CountPendingRequests.
SELECT c.id, c.state, c.requested_by, c.last_message_at,
       o.user_id AS other_id, o.last_read_message_id AS other_read_id,
       COALESCE(p.full_name, '')::text AS full_name, p.headline, p.photo_file_id,
       (u.email_verified_at IS NOT NULL AND u.suspended_at IS NULL)::boolean AS other_available,
       lm.sender_id AS last_sender_id, COALESCE(lm.preview, '')::text AS last_preview, lm.created_at AS last_created_at,
       unread.n AS unread_count
FROM conversation_members me
JOIN conversations c ON c.id = me.conversation_id
JOIN conversation_members o ON o.conversation_id = c.id AND o.user_id <> me.user_id
JOIN users u ON u.id = o.user_id
LEFT JOIN profiles p ON p.user_id = u.id
LEFT JOIN LATERAL (
    SELECT m.sender_id, left(m.body, 120) AS preview, m.created_at
    FROM messages m WHERE m.conversation_id = c.id
    ORDER BY m.id DESC LIMIT 1
) lm ON true
CROSS JOIN LATERAL (
    SELECT count(*) AS n
    FROM messages m
    WHERE m.conversation_id = c.id AND m.sender_id <> me.user_id
      AND (me.last_read_message_id IS NULL OR m.id > me.last_read_message_id)
) unread
WHERE me.user_id = @user_id
  AND CASE WHEN @requests_box::boolean
           THEN c.state = 'request' AND c.requested_by <> me.user_id
                AND u.email_verified_at IS NOT NULL AND u.suspended_at IS NULL
           ELSE c.state = 'active' OR (c.state = 'request' AND c.requested_by = me.user_id)
      END
  AND (NOT @has_cursor::boolean OR (c.last_message_at, c.id) < (@cursor_at::timestamptz, @cursor_id::uuid))
ORDER BY c.last_message_at DESC, c.id DESC
LIMIT @lim;

-- name: ListMessages :many
-- messages_conversation_id_uq, scanned backward, serves this.
SELECT id, conversation_id, sender_id, body, client_id, created_at
FROM messages
WHERE conversation_id = @conversation_id
  AND (NOT @has_before::boolean OR id < @before_id::uuid)
ORDER BY id DESC
LIMIT @lim;

-- name: MessageIn :one
SELECT EXISTS (
    SELECT 1 FROM messages WHERE conversation_id = @conversation_id AND id = @id
)::boolean AS present;

-- name: AnswerRequest :one
UPDATE conversations c
SET state = @state
FROM users r
WHERE c.id = @id AND c.state = 'request' AND c.requested_by <> @receiver_id
  AND EXISTS (SELECT 1 FROM conversation_members me WHERE me.conversation_id = c.id AND me.user_id = @receiver_id)
  AND r.id = c.requested_by AND r.email_verified_at IS NOT NULL AND r.suspended_at IS NULL
RETURNING c.requested_by;

-- name: CountUnreadMessages :one
SELECT count(*)
FROM conversation_members me
JOIN conversations c ON c.id = me.conversation_id AND c.state = 'active'
JOIN messages m ON m.conversation_id = c.id AND m.sender_id <> me.user_id
    AND (me.last_read_message_id IS NULL OR m.id > me.last_read_message_id)
WHERE me.user_id = @user_id;

-- name: CountPendingRequests :one
SELECT count(*)
FROM conversation_members me
JOIN conversations c ON c.id = me.conversation_id
    AND c.state = 'request' AND c.requested_by <> me.user_id
JOIN users r ON r.id = c.requested_by
WHERE me.user_id = @user_id AND r.email_verified_at IS NOT NULL AND r.suspended_at IS NULL;
