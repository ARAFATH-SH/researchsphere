-- Notifications (FR-9). user_id is the recipient and entity_id the item, whose kind the
-- type gives (notification/domain.Registry). A notification is visible while its actor is
-- verified and not suspended; every read and every mark-as-read leaves the others out, so
-- the unread count matches the list.

-- name: PutNotification :exec
-- For the connection types notifications_connection_pair_uq allows one per recipient,
-- actor and type: a new one replaces the old (new item, created now, unread). Other types
-- are not covered by that index and are simply inserted.
INSERT INTO notifications (user_id, actor_id, type, entity_id)
VALUES (@user_id, @actor_id, @type, @entity_id)
ON CONFLICT (user_id, actor_id, type)
    WHERE type IN ('connection_request_received', 'connection_request_accepted')
    DO UPDATE SET entity_id = EXCLUDED.entity_id, created_at = now(), read_at = NULL;

-- name: DeleteNotificationsBySubject :exec
DELETE FROM notifications WHERE type = @type AND entity_id = @entity_id;

-- name: DeleteNotificationsBetween :exec
-- Everything either user was told about the other, of any type (FR-16.9).
DELETE FROM notifications
WHERE (user_id = @a AND actor_id = @b) OR (user_id = @b AND actor_id = @a);

-- name: ListNotifications :many
-- Newest first, then by id, after the cursor when has_cursor; notifications_user_idx
-- serves the order. request_pending is the state of the connection request a received
-- notification is about.
SELECT n.id, n.actor_id, n.type, n.entity_id, n.created_at, n.read_at,
       COALESCE(p.full_name, '')::text AS full_name, p.photo_file_id,
       COALESCE(c.status = 'pending', false)::boolean AS request_pending
FROM notifications n
JOIN users a ON a.id = n.actor_id
LEFT JOIN profiles p ON p.user_id = a.id
LEFT JOIN connections c ON n.type = 'connection_request_received' AND c.id = n.entity_id
WHERE n.user_id = @user_id
  AND a.email_verified_at IS NOT NULL AND a.suspended_at IS NULL
  AND (NOT @has_cursor::boolean OR (n.created_at, n.id) < (@cursor_at::timestamptz, @cursor_id::uuid))
ORDER BY n.created_at DESC, n.id DESC
LIMIT @lim;

-- name: CountUnreadNotifications :one
-- notifications_unread_idx finds the unread rows.
SELECT count(*)
FROM notifications n
JOIN users a ON a.id = n.actor_id
WHERE n.user_id = @user_id AND n.read_at IS NULL
  AND a.email_verified_at IS NOT NULL AND a.suspended_at IS NULL;

-- name: MarkNotificationRead :execrows
-- Keeps the first read time, so marking twice succeeds and changes nothing.
UPDATE notifications n
SET read_at = COALESCE(n.read_at, now())
FROM users a
WHERE n.id = @id AND n.user_id = @user_id
  AND a.id = n.actor_id AND a.email_verified_at IS NOT NULL AND a.suspended_at IS NULL;

-- name: MarkAllNotificationsRead :execrows
UPDATE notifications n
SET read_at = now()
FROM users a
WHERE n.user_id = @user_id AND n.read_at IS NULL
  AND a.id = n.actor_id AND a.email_verified_at IS NOT NULL AND a.suspended_at IS NULL;
