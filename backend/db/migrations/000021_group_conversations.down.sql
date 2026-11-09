-- Removes groups and everything in them (messages, members, reports cascade), then the group
-- columns, the history and the triggers. The triggers go first, so deleting the groups writes
-- nothing.
DROP TRIGGER conversation_members_left ON conversation_members;
DROP TRIGGER conversation_members_joined ON conversation_members;
DROP FUNCTION conversation_member_left();
DROP FUNCTION conversation_member_joined();

DELETE FROM conversations WHERE kind = 'group';

-- Only groups could hold system messages, and only group reports name no one.
DELETE FROM messages WHERE kind = 'system';
DELETE FROM message_reports WHERE reported_id IS NULL;
ALTER TABLE message_reports ALTER COLUMN reported_id SET NOT NULL;

ALTER TABLE messages
    DROP CONSTRAINT messages_live_has_body,
    DROP CONSTRAINT messages_system_fields,
    DROP CONSTRAINT messages_text_fields,
    DROP CONSTRAINT messages_event,
    DROP CONSTRAINT messages_kind,
    DROP COLUMN event_data,
    DROP COLUMN event,
    DROP COLUMN kind,
    ALTER COLUMN client_id SET NOT NULL,
    ALTER COLUMN sender_id SET NOT NULL,
    ADD CONSTRAINT messages_live_has_body CHECK (deleted_at IS NOT NULL OR body IS NOT NULL);

DROP TABLE conversation_memberships;

DROP INDEX conversation_members_one_owner_uq;
ALTER TABLE conversation_members
    ALTER COLUMN joined_at SET DEFAULT now(),
    DROP CONSTRAINT conversation_members_role,
    DROP COLUMN role;

ALTER TABLE conversations
    DROP CONSTRAINT conversations_creator_client_uq,
    DROP CONSTRAINT conversations_closed_after_created,
    DROP CONSTRAINT conversations_name,
    DROP CONSTRAINT conversations_group_fields,
    DROP COLUMN closed_at,
    DROP COLUMN client_id,
    DROP COLUMN created_by,
    DROP COLUMN name;
