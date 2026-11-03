-- Notifications (FR-9.1, FR-9.4 to FR-9.6; slice 14, chunk 14.1). The table exists since
-- 000001. The two connection types get the names the API uses, every notification has an
-- actor and an item, the list pages by (created_at, id), cancelling or rejecting a request
-- deletes its notification by item, and a pair of users has at most one notification of
-- each connection type (re-sending after a cancel or rejection replaces it).
ALTER TYPE notification_type RENAME VALUE 'connection_request' TO 'connection_request_received';
ALTER TYPE notification_type RENAME VALUE 'connection_accepted' TO 'connection_request_accepted';

ALTER TABLE notifications
    ALTER COLUMN actor_id SET NOT NULL,
    ALTER COLUMN entity_id SET NOT NULL;

DROP INDEX notifications_user_idx;
CREATE INDEX notifications_user_idx ON notifications (user_id, created_at DESC, id DESC);
CREATE INDEX notifications_entity_idx ON notifications (entity_id);
CREATE UNIQUE INDEX notifications_connection_pair_uq ON notifications (user_id, actor_id, type)
    WHERE type IN ('connection_request_received', 'connection_request_accepted');
