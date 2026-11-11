-- Gives each deleted message its text back from its kept version, so body can be NOT NULL
-- again, and removes the deleted messages that have none. Edits are not undone: the latest
-- text stays. The checks go first, as one of them forbids a deleted message with a body.
ALTER TABLE messages
    DROP CONSTRAINT messages_deleted_after_created,
    DROP CONSTRAINT messages_edited_after_created,
    DROP CONSTRAINT messages_live_has_body,
    DROP CONSTRAINT messages_deleted_has_no_body;

UPDATE messages m
SET body = v.body
FROM message_versions v
WHERE v.message_id = m.id AND v.ended_by = 'deleted' AND m.body IS NULL;

DELETE FROM messages WHERE body IS NULL;

DROP TABLE message_versions;

ALTER TABLE messages ALTER COLUMN body SET NOT NULL;
