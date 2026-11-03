DROP INDEX IF EXISTS notifications_connection_pair_uq;
DROP INDEX IF EXISTS notifications_entity_idx;
DROP INDEX IF EXISTS notifications_user_idx;
CREATE INDEX notifications_user_idx ON notifications (user_id, created_at DESC);

ALTER TABLE notifications
    ALTER COLUMN entity_id DROP NOT NULL,
    ALTER COLUMN actor_id DROP NOT NULL;

ALTER TYPE notification_type RENAME VALUE 'connection_request_accepted' TO 'connection_accepted';
ALTER TYPE notification_type RENAME VALUE 'connection_request_received' TO 'connection_request';
