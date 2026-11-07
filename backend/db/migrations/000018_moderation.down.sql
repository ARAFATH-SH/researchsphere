DROP TABLE IF EXISTS moderation_actions;
DROP TABLE IF EXISTS account_restrictions;
DROP TABLE IF EXISTS account_warnings;

ALTER TABLE users
    DROP CONSTRAINT users_suspension_reason,
    DROP CONSTRAINT users_suspended_until,
    DROP COLUMN suspension_reason,
    DROP COLUMN suspended_until;

-- System notifications cannot exist once the actor is required again.
ALTER TABLE notifications DROP CONSTRAINT notifications_actor_by_type;
DELETE FROM notifications WHERE actor_id IS NULL;
ALTER TABLE notifications ALTER COLUMN actor_id SET NOT NULL;
