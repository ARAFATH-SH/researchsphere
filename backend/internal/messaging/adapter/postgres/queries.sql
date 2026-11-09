-- Messaging (FR-16). A direct conversation has exactly two members, so "the other member"
-- is the one row that is not the caller; a group (FR-16.6) has a name, an owner and up to 50
-- members. Every read names the caller as a member; only ListMessages, the locks and the group
-- queries do not, and the usecase calls them after FindConversation. A member sees only the
-- messages created from the moment they joined (joined_at), and system messages never count
-- as unread. Message rows list the columns of the messages table in its order, so sqlc maps
-- them all to sqlcgen.Message.

-- name: UsersVisible :many
SELECT u.id FROM users u
WHERE u.id = ANY(@ids::uuid[]) AND u.email_verified_at IS NOT NULL AND u.suspended_at IS NULL;

-- name: UserVisible :one
SELECT EXISTS (
    SELECT 1 FROM users u
    WHERE u.id = @id AND u.email_verified_at IS NOT NULL AND u.suspended_at IS NULL
)::boolean AS visible;

-- name: FindSent :one
SELECT m.id, m.conversation_id, m.sender_id, m.body, m.client_id, m.created_at, m.edited_at, m.deleted_at, m.kind, m.event, m.event_data, m.has_attachments, c.state
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

-- name: DeclineRequestBetween :exec
-- A block closes a pending request between the two, whoever sent it (FR-16.9).
UPDATE conversations
SET state = 'declined'
WHERE user_low = LEAST(@a::uuid, @b::uuid) AND user_high = GREATEST(@a::uuid, @b::uuid)
  AND state = 'request';

-- name: InsertMessage :one
-- The caller holds the conversation's row, so this reads the last send's time and no other
-- send runs. The ID and created_at come from one instant, after that lock, and always later
-- than the last message, even in the same microsecond or if the clock steps back: the ID
-- alone decides the order of a conversation's messages, and created_at agrees with it.
-- ON CONFLICT also settles two sends of one client_id at the same moment. body is null only
-- for a message that carries attachments (FR-16.7).
INSERT INTO messages (id, conversation_id, sender_id, body, client_id, created_at, has_attachments)
SELECT uuid_v7_at(t.instant), c.id, @sender_id::uuid, sqlc.narg(body)::text, @client_id::uuid, t.instant, @has_attachments::boolean
FROM conversations c
CROSS JOIN LATERAL (
    SELECT GREATEST(clock_timestamp(), c.last_message_at + interval '1 microsecond') AS instant
) t
WHERE c.id = @conversation_id::uuid
ON CONFLICT (sender_id, client_id) DO NOTHING
RETURNING id, conversation_id, sender_id, body, client_id, created_at, edited_at, deleted_at, kind, event, event_data, has_attachments;

-- name: TouchConversation :exec
UPDATE conversations SET last_message_at = @last_message_at WHERE id = @id;

-- name: MoveReadMarker :execrows
-- Forward only: a marker never moves back to an earlier message.
UPDATE conversation_members
SET last_read_message_id = @message_id, last_read_at = now()
WHERE conversation_id = @conversation_id AND user_id = @user_id
  AND (last_read_message_id IS NULL OR last_read_message_id < @message_id);

-- name: FindConversation :one
-- For a group there is no other member, so the join to it is a left join that only a direct
-- conversation fills.
SELECT c.id, c.kind, c.state, c.requested_by, c.last_message_at, COALESCE(c.name, '')::text AS name,
       me.role AS my_role, me.joined_at AS my_joined_at,
       o.user_id AS other_id, o.last_read_message_id AS other_read_id
FROM conversation_members me
JOIN conversations c ON c.id = me.conversation_id
LEFT JOIN conversation_members o ON c.kind = 'direct' AND o.conversation_id = c.id AND o.user_id <> me.user_id
WHERE me.conversation_id = @id AND me.user_id = @user_id;

-- name: FindItem :one
SELECT c.id, c.kind, c.state, c.requested_by, c.last_message_at, COALESCE(c.name, '')::text AS name,
       me.role AS my_role, me.joined_at AS my_joined_at,
       o.user_id AS other_id, o.last_read_message_id AS other_read_id,
       COALESCE(p.full_name, '')::text AS full_name, p.headline, p.photo_file_id,
       COALESCE(u.email_verified_at IS NOT NULL AND u.suspended_at IS NULL, false)::boolean AS other_available,
       lm.sender_id AS last_sender_id, COALESCE(lm.kind, '')::text AS last_kind, lm.event AS last_event,
       COALESCE(lm.preview, '')::text AS last_preview, lm.created_at AS last_created_at,
       COALESCE(lm.deleted, false)::boolean AS last_deleted,
       COALESCE(lm.attachment_count, 0)::integer AS last_attachment_count,
       unread.n AS unread_count,
       (SELECT count(*) FROM conversation_members gm WHERE c.kind = 'group' AND gm.conversation_id = c.id) AS member_count,
       EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = me.user_id AND b.blocked_id = o.user_id)::boolean AS blocked_by_me,
       EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = o.user_id AND b.blocked_id = me.user_id)::boolean AS blocked_me
FROM conversation_members me
JOIN conversations c ON c.id = me.conversation_id
LEFT JOIN conversation_members o ON c.kind = 'direct' AND o.conversation_id = c.id AND o.user_id <> me.user_id
LEFT JOIN users u ON u.id = o.user_id
LEFT JOIN profiles p ON p.user_id = u.id
LEFT JOIN LATERAL (
    SELECT m.sender_id, m.kind, m.event, left(m.body, 120) AS preview, m.created_at, m.deleted_at IS NOT NULL AS deleted,
           CASE WHEN m.has_attachments AND m.deleted_at IS NULL
                THEN (SELECT count(*) FROM message_attachments a
                      WHERE a.conversation_id = m.conversation_id AND a.message_id = m.id)
                ELSE 0 END AS attachment_count
    FROM messages m WHERE m.conversation_id = c.id AND m.created_at >= me.joined_at
    ORDER BY m.id DESC LIMIT 1
) lm ON true
CROSS JOIN LATERAL (
    SELECT count(*) AS n
    FROM messages m
    WHERE m.conversation_id = c.id AND m.kind = 'text' AND m.sender_id <> me.user_id AND m.deleted_at IS NULL
      AND m.created_at >= me.joined_at
      AND (me.last_read_message_id IS NULL OR m.id > me.last_read_message_id)
) unread
WHERE me.conversation_id = @id AND me.user_id = @user_id;

-- name: ListItems :many
-- The columns and joins of FindItem. A request whose sender is no longer verified or is
-- suspended cannot be answered, so it leaves the requests box and CountPendingRequests.
-- Groups are always active, so they are in the inbox.
SELECT c.id, c.kind, c.state, c.requested_by, c.last_message_at, COALESCE(c.name, '')::text AS name,
       me.role AS my_role, me.joined_at AS my_joined_at,
       o.user_id AS other_id, o.last_read_message_id AS other_read_id,
       COALESCE(p.full_name, '')::text AS full_name, p.headline, p.photo_file_id,
       COALESCE(u.email_verified_at IS NOT NULL AND u.suspended_at IS NULL, false)::boolean AS other_available,
       lm.sender_id AS last_sender_id, COALESCE(lm.kind, '')::text AS last_kind, lm.event AS last_event,
       COALESCE(lm.preview, '')::text AS last_preview, lm.created_at AS last_created_at,
       COALESCE(lm.deleted, false)::boolean AS last_deleted,
       COALESCE(lm.attachment_count, 0)::integer AS last_attachment_count,
       unread.n AS unread_count,
       (SELECT count(*) FROM conversation_members gm WHERE c.kind = 'group' AND gm.conversation_id = c.id) AS member_count,
       EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = me.user_id AND b.blocked_id = o.user_id)::boolean AS blocked_by_me,
       EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = o.user_id AND b.blocked_id = me.user_id)::boolean AS blocked_me
FROM conversation_members me
JOIN conversations c ON c.id = me.conversation_id
LEFT JOIN conversation_members o ON c.kind = 'direct' AND o.conversation_id = c.id AND o.user_id <> me.user_id
LEFT JOIN users u ON u.id = o.user_id
LEFT JOIN profiles p ON p.user_id = u.id
LEFT JOIN LATERAL (
    SELECT m.sender_id, m.kind, m.event, left(m.body, 120) AS preview, m.created_at, m.deleted_at IS NOT NULL AS deleted,
           CASE WHEN m.has_attachments AND m.deleted_at IS NULL
                THEN (SELECT count(*) FROM message_attachments a
                      WHERE a.conversation_id = m.conversation_id AND a.message_id = m.id)
                ELSE 0 END AS attachment_count
    FROM messages m WHERE m.conversation_id = c.id AND m.created_at >= me.joined_at
    ORDER BY m.id DESC LIMIT 1
) lm ON true
CROSS JOIN LATERAL (
    SELECT count(*) AS n
    FROM messages m
    WHERE m.conversation_id = c.id AND m.kind = 'text' AND m.sender_id <> me.user_id AND m.deleted_at IS NULL
      AND m.created_at >= me.joined_at
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
-- messages_conversation_id_uq, scanned backward, serves this. A deleted message is listed in
-- its place with no body; message_versions is never read for a member (NFR-14). since is the
-- member's joining time.
SELECT id, conversation_id, sender_id, body, client_id, created_at, edited_at, deleted_at, kind, event, event_data, has_attachments
FROM messages
WHERE conversation_id = @conversation_id AND created_at >= @since::timestamptz
  AND (NOT @has_before::boolean OR id < @before_id::uuid)
ORDER BY id DESC
LIMIT @lim;

-- name: LockOwnMessage :one
-- The sender's message in the conversation, held until the transaction ends so an edit and a
-- deletion of one message run one at a time. No row for anyone else's (NFR-14), nor for a
-- system message, which is no one's to edit or delete.
SELECT id, conversation_id, sender_id, body, client_id, created_at, edited_at, deleted_at, kind, event, event_data, has_attachments
FROM messages
WHERE conversation_id = @conversation_id AND id = @id AND sender_id = @sender_id AND kind = 'text'
FOR UPDATE;

-- name: FindOwnMessage :one
-- LockOwnMessage without the lock, for the checks before the transaction.
SELECT id, conversation_id, sender_id, body, client_id, created_at, edited_at, deleted_at, kind, event, event_data, has_attachments
FROM messages
WHERE conversation_id = @conversation_id AND id = @id AND sender_id = @sender_id AND kind = 'text';

-- name: EditMessage :one
-- Keeps the text it replaces in message_versions, shown from the previous edit (or the send)
-- until this one, at one instant. GREATEST keeps edited_at after created_at, which a send may
-- have set a microsecond ahead of the clock (InsertMessage).
WITH old AS (
    SELECT m.id, m.conversation_id, m.body, COALESCE(m.edited_at, m.created_at) AS shown_from,
           GREATEST(clock_timestamp(), m.created_at, m.edited_at) AS instant
    FROM messages m
    WHERE m.id = @id AND m.deleted_at IS NULL AND m.kind = 'text'
), kept AS (
    INSERT INTO message_versions (conversation_id, message_id, body, shown_from, shown_until, ended_by)
    SELECT old.conversation_id, old.id, old.body, old.shown_from, old.instant, 'edited' FROM old
    WHERE old.body IS NOT NULL -- a message with only attachments had no text to keep
)
UPDATE messages m
SET body = @body::text, edited_at = old.instant
FROM old
WHERE m.id = old.id
RETURNING m.id, m.conversation_id, m.sender_id, m.body, m.client_id, m.created_at, m.edited_at, m.deleted_at, m.kind, m.event, m.event_data, m.has_attachments;

-- name: DeleteMessage :one
-- Leaves a tombstone and keeps the last text in message_versions, as EditMessage does.
WITH old AS (
    SELECT m.id, m.conversation_id, m.body, COALESCE(m.edited_at, m.created_at) AS shown_from,
           GREATEST(clock_timestamp(), m.created_at, m.edited_at) AS instant
    FROM messages m
    WHERE m.id = @id AND m.deleted_at IS NULL AND m.kind = 'text'
), kept AS (
    INSERT INTO message_versions (conversation_id, message_id, body, shown_from, shown_until, ended_by)
    SELECT old.conversation_id, old.id, old.body, old.shown_from, old.instant, 'deleted' FROM old
    WHERE old.body IS NOT NULL -- a message with only attachments had no text to keep
)
UPDATE messages m
SET body = NULL, deleted_at = old.instant
FROM old
WHERE m.id = old.id
RETURNING m.id, m.conversation_id, m.sender_id, m.body, m.client_id, m.created_at, m.edited_at, m.deleted_at, m.kind, m.event, m.event_data, m.has_attachments;

-- name: MessageIn :one
-- since is the member's joining time: a message from before it is not theirs to see.
SELECT EXISTS (
    SELECT 1 FROM messages WHERE conversation_id = @conversation_id AND id = @id AND created_at >= @since::timestamptz
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
-- Groups included; system messages and messages from before the member joined never count.
SELECT count(*)
FROM conversation_members me
JOIN conversations c ON c.id = me.conversation_id AND c.state = 'active'
JOIN messages m ON m.conversation_id = c.id AND m.kind = 'text' AND m.sender_id <> me.user_id AND m.deleted_at IS NULL
    AND m.created_at >= me.joined_at
    AND (me.last_read_message_id IS NULL OR m.id > me.last_read_message_id)
WHERE me.user_id = @user_id;

-- name: CountPendingRequests :one
SELECT count(*)
FROM conversation_members me
JOIN conversations c ON c.id = me.conversation_id
    AND c.state = 'request' AND c.requested_by <> me.user_id
JOIN users r ON r.id = c.requested_by
WHERE me.user_id = @user_id AND r.email_verified_at IS NOT NULL AND r.suspended_at IS NULL;

-- name: FindMessageSender :one
-- Only a text message the reporter could see: a system message is no one's to report.
SELECT sender_id FROM messages
WHERE conversation_id = @conversation_id AND id = @id AND kind = 'text' AND created_at >= @since::timestamptz;

-- name: InsertReport :one
INSERT INTO message_reports (conversation_id, reporter_id, reported_id, message_id, reason, details)
VALUES (@conversation_id, @reporter_id, sqlc.narg('reported_id'), sqlc.narg('message_id'), @reason, sqlc.narg('details'))
RETURNING id, created_at;

-- name: ListReports :many
-- message_reports_status_idx serves this. The two people are shown whatever their state:
-- an administrator must be able to review a report about a suspended user. A report of a
-- whole group has no reported user.
SELECT r.id, r.conversation_id, c.kind AS conversation_kind, c.name AS group_name,
       r.reason, r.details, r.message_id, r.created_at,
       r.outcome, r.resolution_note, r.resolved_at,
       r.reporter_id, COALESCE(rp.full_name, '')::text AS reporter_name, rp.headline AS reporter_headline, rp.photo_file_id AS reporter_photo, ru.role::text AS reporter_role,
       r.reported_id, COALESCE(dp.full_name, '')::text AS reported_name, dp.headline AS reported_headline, dp.photo_file_id AS reported_photo, COALESCE(du.role::text, '')::text AS reported_role
FROM message_reports r
JOIN conversations c ON c.id = r.conversation_id
JOIN users ru ON ru.id = r.reporter_id
LEFT JOIN users du ON du.id = r.reported_id
LEFT JOIN profiles rp ON rp.user_id = r.reporter_id
LEFT JOIN profiles dp ON dp.user_id = r.reported_id
WHERE (r.resolved_at IS NULL) = @open::boolean
ORDER BY r.created_at DESC, r.id DESC
LIMIT @lim OFFSET @off;

-- name: CountReports :one
SELECT count(*) FROM message_reports r WHERE (r.resolved_at IS NULL) = @open::boolean;

-- name: FindReport :one
SELECT r.id, r.conversation_id, c.kind AS conversation_kind, c.name AS group_name,
       r.reason, r.details, r.message_id, r.created_at,
       r.outcome, r.resolution_note, r.resolved_at,
       r.reporter_id, COALESCE(rp.full_name, '')::text AS reporter_name, rp.headline AS reporter_headline, rp.photo_file_id AS reporter_photo, ru.role::text AS reporter_role,
       r.reported_id, COALESCE(dp.full_name, '')::text AS reported_name, dp.headline AS reported_headline, dp.photo_file_id AS reported_photo, COALESCE(du.role::text, '')::text AS reported_role
FROM message_reports r
JOIN conversations c ON c.id = r.conversation_id
JOIN users ru ON ru.id = r.reporter_id
LEFT JOIN users du ON du.id = r.reported_id
LEFT JOIN profiles rp ON rp.user_id = r.reporter_id
LEFT JOIN profiles dp ON dp.user_id = r.reported_id
WHERE r.id = @id;

-- name: ListReportedMessages :many
-- The one way an administrator reads a conversation (NFR-14): it starts from a report, so a
-- conversation without one is out of reach, and stops at the report's moment. at_report is
-- the text shown at that moment: none when deleted by then, else the kept version that was
-- showing, else the current text, which has shown since before the report. Text written after
-- the report never leaves the database. In a group it shows every message up to the report,
-- system messages included, whoever was a member when.
SELECT m.id, m.conversation_id, m.sender_id, m.kind, m.event, m.event_data, m.created_at, m.edited_at, m.deleted_at,
       COALESCE(at_report.body, '')::text AS body_at_report,
       (at_report.body IS NOT NULL)::boolean AS has_body_at_report,
       (COALESCE(m.edited_at > r.created_at, false) OR COALESCE(m.deleted_at > r.created_at, false))::boolean AS changed_after_report
FROM message_reports r
JOIN messages m ON m.conversation_id = r.conversation_id AND m.created_at <= r.created_at
CROSS JOIN LATERAL (
    SELECT CASE
        WHEN m.deleted_at IS NOT NULL AND m.deleted_at <= r.created_at THEN NULL
        ELSE COALESCE(
            (SELECT v.body FROM message_versions v
             WHERE v.conversation_id = m.conversation_id AND v.message_id = m.id
               AND v.shown_from <= r.created_at AND v.shown_until > r.created_at
             ORDER BY v.shown_until LIMIT 1),
            CASE WHEN COALESCE(m.edited_at, m.created_at) <= r.created_at THEN m.body END)
    END AS body
) at_report
WHERE r.id = @report_id
  AND (NOT @has_before::boolean OR m.id < @before_id::uuid)
ORDER BY m.id DESC
LIMIT @lim;

-- name: ListReportedVersions :many
-- The kept texts of the listed messages that were shown at or before the report's moment,
-- oldest first. Like ListReportedMessages it starts from the report (NFR-14).
SELECT v.message_id, v.body, v.shown_from, v.shown_until, v.ended_by
FROM message_reports r
JOIN message_versions v ON v.conversation_id = r.conversation_id AND v.shown_from <= r.created_at
WHERE r.id = @report_id AND v.message_id = ANY(@message_ids::uuid[])
ORDER BY v.message_id, v.shown_from, v.shown_until;

-- name: MembersAtReport :many
-- Everyone in the report's group at the report's moment, also those who left since. Like
-- ListReportedMessages it starts from the report (NFR-14).
SELECT ms.user_id, COALESCE(p.full_name, '')::text AS full_name, p.headline, p.photo_file_id, u.role::text AS role
FROM message_reports r
JOIN conversation_memberships ms ON ms.conversation_id = r.conversation_id
    AND ms.joined_at <= r.created_at AND (ms.left_at IS NULL OR ms.left_at > r.created_at)
JOIN users u ON u.id = ms.user_id
LEFT JOIN profiles p ON p.user_id = ms.user_id
WHERE r.id = @report_id
ORDER BY full_name, ms.user_id;

-- name: MemberAtReport :one
SELECT EXISTS (
    SELECT 1 FROM message_reports r
    JOIN conversation_memberships ms ON ms.conversation_id = r.conversation_id
        AND ms.joined_at <= r.created_at AND (ms.left_at IS NULL OR ms.left_at > r.created_at)
    WHERE r.id = @report_id AND ms.user_id = @user_id
)::boolean AS member;

-- name: InsertReportView :exec
INSERT INTO message_report_views (report_id, admin_id) VALUES (@report_id, @admin_id);

-- name: ResolveReport :execrows
UPDATE message_reports
SET resolved_at = now(), resolved_by = @admin_id, outcome = @outcome, resolution_note = sqlc.narg('note')
WHERE id = @id AND resolved_at IS NULL;

-- name: ActivePeers :many
-- Who may see the user's presence (FR-16.4): the other member of each active direct
-- conversation, with no block either way. Group members do not see each other's presence.
SELECT o.user_id
FROM conversation_members me
JOIN conversations c ON c.id = me.conversation_id AND c.state = 'active' AND c.kind = 'direct'
JOIN conversation_members o ON o.conversation_id = c.id AND o.user_id <> me.user_id
WHERE me.user_id = @user_id
  AND NOT EXISTS (
      SELECT 1 FROM blocks b
      WHERE (b.blocker_id = me.user_id AND b.blocked_id = o.user_id)
         OR (b.blocker_id = o.user_id AND b.blocked_id = me.user_id));

-- name: VisiblePeers :many
-- ActivePeers narrowed to the IDs asked about.
SELECT o.user_id
FROM conversation_members me
JOIN conversations c ON c.id = me.conversation_id AND c.state = 'active' AND c.kind = 'direct'
JOIN conversation_members o ON o.conversation_id = c.id AND o.user_id <> me.user_id
WHERE me.user_id = @user_id AND o.user_id = ANY(@ids::uuid[])
  AND NOT EXISTS (
      SELECT 1 FROM blocks b
      WHERE (b.blocker_id = me.user_id AND b.blocked_id = o.user_id)
         OR (b.blocker_id = o.user_id AND b.blocked_id = me.user_id));

-- name: ListPartners :many
-- Every direct conversation of the user, in any state, with its other member.
SELECT o.conversation_id, o.user_id
FROM conversation_members me
JOIN conversations c ON c.id = me.conversation_id AND c.kind = 'direct'
JOIN conversation_members o ON o.conversation_id = me.conversation_id AND o.user_id <> me.user_id
WHERE me.user_id = @user_id;

-- name: DirectID :one
SELECT id FROM conversations
WHERE user_low = LEAST(@a::uuid, @b::uuid) AND user_high = GREATEST(@a::uuid, @b::uuid);

-- Groups (FR-16.6). Each write runs while the caller holds the conversation's row.

-- name: FindGroupByClient :one
SELECT id FROM conversations WHERE kind = 'group' AND created_by = @created_by AND client_id = @client_id;

-- name: InsertGroup :one
-- conversations_creator_client_uq settles two creates with one client ID.
INSERT INTO conversations (kind, state, name, created_by, client_id)
VALUES ('group', 'active', @name, @created_by, @client_id)
ON CONFLICT (created_by, client_id) DO NOTHING
RETURNING id;

-- name: InsertSystemMessage :one
-- The instant rule of InsertMessage: after the conversation's last message, so the ID orders it.
INSERT INTO messages (id, conversation_id, sender_id, kind, event, event_data, created_at)
SELECT uuid_v7_at(t.instant), c.id, sqlc.narg('sender_id')::uuid, 'system', @event::text, @event_data::jsonb, t.instant
FROM conversations c
CROSS JOIN LATERAL (
    SELECT GREATEST(clock_timestamp(), c.last_message_at + interval '1 microsecond') AS instant
) t
WHERE c.id = @conversation_id::uuid
RETURNING id, conversation_id, sender_id, body, client_id, created_at, edited_at, deleted_at, kind, event, event_data, has_attachments;

-- name: AddGroupMember :exec
-- conversation_members_joined (migration 000021) opens the stay in conversation_memberships.
INSERT INTO conversation_members (conversation_id, user_id, role, joined_at)
VALUES (@conversation_id, @user_id, @role, @joined_at);

-- name: RemoveGroupMember :execrows
-- conversation_members_left (migration 000021) ends the stay, hands ownership over with its
-- owner_changed message, or closes the group when nobody is left.
DELETE FROM conversation_members WHERE conversation_id = @conversation_id AND user_id = @user_id;

-- name: RenameGroup :exec
UPDATE conversations SET name = @name WHERE id = @id AND kind = 'group';

-- name: GroupMemberIDs :many
SELECT user_id FROM conversation_members WHERE conversation_id = @conversation_id ORDER BY joined_at, user_id;

-- name: ListGroupMembers :many
SELECT gm.user_id, COALESCE(p.full_name, '')::text AS full_name, p.photo_file_id, gm.role, gm.joined_at,
       (u.email_verified_at IS NOT NULL AND u.suspended_at IS NULL)::boolean AS available
FROM conversation_members gm
JOIN users u ON u.id = gm.user_id
LEFT JOIN profiles p ON p.user_id = gm.user_id
WHERE gm.conversation_id = @conversation_id
ORDER BY gm.role = 'owner' DESC, gm.joined_at, gm.user_id;

-- name: GroupAvatars :many
-- Up to 3 members other than the viewer per group, those who joined earliest first.
SELECT a.conversation_id, a.user_id, a.full_name, a.headline, a.photo_file_id, a.available
FROM (
    SELECT gm.conversation_id, gm.user_id, COALESCE(p.full_name, '')::text AS full_name, p.headline, p.photo_file_id,
           (u.email_verified_at IS NOT NULL AND u.suspended_at IS NULL)::boolean AS available,
           row_number() OVER (PARTITION BY gm.conversation_id ORDER BY gm.joined_at, gm.user_id) AS n,
           gm.joined_at
    FROM conversation_members gm
    JOIN users u ON u.id = gm.user_id
    LEFT JOIN profiles p ON p.user_id = gm.user_id
    WHERE gm.conversation_id = ANY(@conversation_ids::uuid[]) AND gm.user_id <> @viewer_id
) a
WHERE a.n <= 3
ORDER BY a.conversation_id, a.n;

-- name: ListReaders :many
SELECT user_id, last_read_message_id FROM conversation_members
WHERE conversation_id = @conversation_id AND user_id <> @viewer_id
ORDER BY joined_at, user_id;

-- name: SystemMessagesAfter :many
SELECT id, conversation_id, sender_id, body, client_id, created_at, edited_at, deleted_at, kind, event, event_data, has_attachments
FROM messages
WHERE conversation_id = @conversation_id AND kind = 'system' AND id > @after_id
ORDER BY id;

-- ---------------------------------------------------------------------------
-- Attachments (FR-16.7, NFR-13; slice 14B, chunk 14B.3). An attachment is the stored_files row
-- (purpose 'attachment') and its message_attachments row; the ID of both is the file's.
-- ---------------------------------------------------------------------------

-- name: InsertAttachment :one
-- The file's row starts pending: the scan worker (media module) picks it up.
WITH f AS (
    INSERT INTO stored_files (id, owner_user_id, purpose, object_key, content_type, size_bytes, width, height, scan_status)
    VALUES (@id, @uploader_id, 'attachment', @object_key, @content_type, @size_bytes,
            sqlc.narg(width), sqlc.narg(height), 'pending')
    RETURNING id, owner_user_id, size_bytes, content_type, scan_status
)
INSERT INTO message_attachments (file_id, uploader_id, conversation_id, name, expires_at)
SELECT f.id, f.owner_user_id, @conversation_id, @name, @expires_at FROM f
RETURNING file_id, created_at, expires_at;

-- name: ClaimAttachments :many
-- Ties the sender's unused attachments of this conversation to the message, in the order given.
-- An expired one, or one the scan blocked or could not read, is not taken: the usecase compares
-- the count and rolls back.
UPDATE message_attachments a
SET message_id = @message_id, position = (ids.ord - 1)::smallint
FROM unnest(@ids::uuid[]) WITH ORDINALITY AS ids(id, ord), stored_files f
WHERE a.file_id = ids.id AND f.id = a.file_id
  AND a.uploader_id = @uploader_id AND a.conversation_id = @conversation_id
  AND a.message_id IS NULL AND a.expires_at > now()
  AND f.scan_status IN ('pending', 'clean')
RETURNING a.file_id;

-- name: ListMessageAttachments :many
-- The attachments of the listed messages, in order. The caller has already checked who may see
-- the messages.
SELECT a.message_id::uuid AS message_id, a.file_id, a.name, f.size_bytes, f.content_type, f.scan_status
FROM message_attachments a
JOIN stored_files f ON f.id = a.file_id
WHERE a.conversation_id = @conversation_id AND a.message_id = ANY(@message_ids::uuid[])
ORDER BY a.message_id, a.position;

-- name: FindAttachment :one
-- An attachment with what decides who may download it: its message (if sent), and the caller's
-- membership. my_joined_at is null when the caller is not a member now.
SELECT a.file_id, a.uploader_id, a.conversation_id, a.message_id, a.name,
       f.object_key, f.content_type, f.size_bytes, f.scan_status,
       m.created_at AS message_created_at, (m.deleted_at IS NOT NULL)::boolean AS message_deleted,
       me.joined_at AS my_joined_at
FROM message_attachments a
JOIN stored_files f ON f.id = a.file_id
LEFT JOIN messages m ON m.conversation_id = a.conversation_id AND m.id = a.message_id
LEFT JOIN conversation_members me ON me.conversation_id = a.conversation_id AND me.user_id = @user_id
WHERE a.file_id = @id;

-- name: FindReportedAttachment :one
-- An attachment as the administrator may reach it (NFR-14): starting from the report, only of a
-- message of the reported conversation sent by the report's moment.
SELECT a.file_id, a.name, f.object_key, f.content_type, f.scan_status
FROM message_reports r
JOIN message_attachments a ON a.conversation_id = r.conversation_id
JOIN messages m ON m.conversation_id = a.conversation_id AND m.id = a.message_id AND m.created_at <= r.created_at
JOIN stored_files f ON f.id = a.file_id
WHERE r.id = @report_id AND a.file_id = @file_id;

-- name: ListReportedAttachments :many
-- The attachments of the listed messages of a report, deleted messages' included. Like
-- ListReportedMessages it starts from the report (NFR-14).
SELECT a.message_id::uuid AS message_id, a.file_id, a.name, f.size_bytes, f.content_type, f.scan_status
FROM message_reports r
JOIN message_attachments a ON a.conversation_id = r.conversation_id
JOIN messages m ON m.conversation_id = a.conversation_id AND m.id = a.message_id AND m.created_at <= r.created_at
JOIN stored_files f ON f.id = a.file_id
WHERE r.id = @report_id AND a.message_id = ANY(@message_ids::uuid[])
ORDER BY a.message_id, a.position;

-- name: InsertAttachmentDownload :exec
INSERT INTO message_report_attachment_downloads (report_id, file_id, admin_id)
VALUES (@report_id, @file_id, @admin_id);

-- name: FindAttachmentAudience :one
-- Who hears that an attachment's scan finished: its conversation and message, and the time from
-- which a member must have joined to see it.
SELECT a.conversation_id, a.message_id, a.uploader_id, m.created_at AS message_created_at,
       (m.deleted_at IS NOT NULL)::boolean AS message_deleted
FROM message_attachments a
LEFT JOIN messages m ON m.conversation_id = a.conversation_id AND m.id = a.message_id
WHERE a.file_id = @file_id;

-- name: MembersSince :many
-- The current members who joined at or before the moment (they can see a message sent then).
SELECT user_id FROM conversation_members
WHERE conversation_id = @conversation_id AND joined_at <= @at::timestamptz;

-- name: DeleteExpiredAttachments :execrows
-- Unused attachments past their expiry. The trigger of 000024 deletes the stored file, whose
-- trigger queues the object.
DELETE FROM message_attachments
WHERE file_id IN (
    SELECT file_id FROM message_attachments
    WHERE message_id IS NULL AND expires_at <= now()
    ORDER BY expires_at
    LIMIT @max_rows
    FOR UPDATE SKIP LOCKED
);
