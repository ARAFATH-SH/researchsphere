-- Moderation (FR-15.5 to FR-15.10). moderation_actions is the audit trail: rows are added and
-- never changed.

-- name: InsertWarning :one
INSERT INTO account_warnings (user_id, reason) VALUES (@user_id, @reason) RETURNING id;

-- name: ListPendingWarnings :many
-- account_warnings_pending_idx serves this.
SELECT id, reason, created_at, acknowledged_at
FROM account_warnings
WHERE user_id = @user_id AND acknowledged_at IS NULL
ORDER BY created_at DESC, id DESC
LIMIT @lim;

-- name: FindWarning :one
SELECT id, reason, created_at, acknowledged_at
FROM account_warnings
WHERE id = @id AND user_id = @user_id;

-- name: AcknowledgeWarning :execrows
-- Keeps the first time, so acknowledging twice succeeds and changes nothing.
UPDATE account_warnings
SET acknowledged_at = COALESCE(acknowledged_at, @at)
WHERE id = @id AND user_id = @user_id;

-- name: InsertRestriction :one
INSERT INTO account_restrictions (user_id, kind, starts_at, ends_at)
VALUES (@user_id, @kind, @starts_at, @ends_at)
RETURNING id;

-- name: HasActiveRestriction :one
SELECT EXISTS (
    SELECT 1 FROM account_restrictions
    WHERE user_id = @user_id AND kind = @kind AND lifted_at IS NULL AND ends_at > @now
);

-- name: RestrictionUser :one
SELECT user_id FROM account_restrictions WHERE id = @id;

-- name: LiftRestriction :execrows
UPDATE account_restrictions
SET lifted_at = @now, lifted_by = @admin_id
WHERE id = @id AND lifted_at IS NULL AND ends_at > @now;

-- name: InsertAction :one
INSERT INTO moderation_actions (user_id, admin_id, action, reason, note, report_id, warning_id, restriction_id, ends_at)
VALUES (@user_id, @admin_id, @action, @reason, sqlc.narg('note'), sqlc.narg('report_id'),
        sqlc.narg('warning_id'), sqlc.narg('restriction_id'), sqlc.narg('ends_at'))
RETURNING id;

-- name: FindAction :one
SELECT a.id, a.user_id, a.admin_id, a.action, a.reason, a.note, a.report_id, a.warning_id,
       a.restriction_id, a.ends_at, a.created_at, ap.full_name AS admin_name, w.acknowledged_at,
       r.lifted_at, mr.id AS linked_report_id, mr.reason AS report_reason,
       mr.outcome AS report_outcome, mr.created_at AS report_created_at
FROM moderation_actions a
LEFT JOIN profiles ap ON ap.user_id = a.admin_id
LEFT JOIN account_warnings w ON w.id = a.warning_id
LEFT JOIN account_restrictions r ON r.id = a.restriction_id
LEFT JOIN message_reports mr ON mr.id = a.report_id
WHERE a.id = @id;

-- name: ListActions :many
-- moderation_actions_user_idx serves the order; the id breaks ties so pages never overlap.
SELECT a.id, a.user_id, a.admin_id, a.action, a.reason, a.note, a.report_id, a.warning_id,
       a.restriction_id, a.ends_at, a.created_at, ap.full_name AS admin_name, w.acknowledged_at,
       r.lifted_at, mr.id AS linked_report_id, mr.reason AS report_reason,
       mr.outcome AS report_outcome, mr.created_at AS report_created_at
FROM moderation_actions a
LEFT JOIN profiles ap ON ap.user_id = a.admin_id
LEFT JOIN account_warnings w ON w.id = a.warning_id
LEFT JOIN account_restrictions r ON r.id = a.restriction_id
LEFT JOIN message_reports mr ON mr.id = a.report_id
WHERE a.user_id = @user_id
ORDER BY a.created_at DESC, a.id DESC
LIMIT @lim OFFSET @off;

-- name: CountActions :one
SELECT count(*) FROM moderation_actions WHERE user_id = @user_id;

-- name: FindStanding :one
-- FOR SHARE: an action holds this row FOR UPDATE, so a write that checks the actor in its
-- transaction runs wholly before or wholly after the action (decisions.md, 2026-11-05).
SELECT u.suspended_at, u.suspended_until, u.suspension_reason,
       (SELECT max(r.ends_at) FROM account_restrictions r
        WHERE r.user_id = u.id AND r.kind = 'messaging' AND r.lifted_at IS NULL AND r.ends_at > @now
       )::timestamptz AS restricted_until
FROM users u
WHERE u.id = @id
FOR SHARE OF u;
