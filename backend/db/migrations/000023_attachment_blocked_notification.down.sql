-- PostgreSQL cannot drop an enum value, so the type is rebuilt without it. The partial index
-- and the check compare the column with enum values, so they are rebuilt too.
DELETE FROM notifications WHERE type = 'attachment_blocked';

DROP INDEX notifications_connection_pair_uq;
ALTER TABLE notifications DROP CONSTRAINT notifications_actor_by_type;

ALTER TYPE notification_type RENAME TO notification_type_old;
CREATE TYPE notification_type AS ENUM (
    'connection_request_received',
    'connection_request_accepted',
    'project_join_request',
    'project_invitation',
    'project_join_request_accepted',
    'project_join_request_rejected',
    'project_invitation_accepted',
    'project_invitation_rejected',
    'post_comment',
    'post_like',
    'account_warning',
    'group_added'
);
ALTER TABLE notifications
    ALTER COLUMN type TYPE notification_type USING type::text::notification_type;
DROP TYPE notification_type_old;

CREATE UNIQUE INDEX notifications_connection_pair_uq ON notifications (user_id, actor_id, type)
    WHERE type IN ('connection_request_received', 'connection_request_accepted');
ALTER TABLE notifications
    ADD CONSTRAINT notifications_actor_by_type CHECK ((actor_id IS NULL) = (type = 'account_warning'));
