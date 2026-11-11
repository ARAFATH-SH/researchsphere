-- Group conversations (FR-16.6, with FR-16.3, FR-16.5, FR-16.9 and FR-16.10 for groups; slice
-- 14B, chunk 14B.2). docs/decisions.md, 2026-11-09.
--
-- A group has a name, an owner and up to 50 members. Each membership change writes a system
-- message in the thread, and a member sees only the messages from the moment they joined.
-- conversation_memberships keeps every stay (joined, left) so an administrator can tell who
-- was in a group at the moment of a report; members never read it. The owner trigger below
-- is the one rule for handing ownership over and for closing an empty group, whether a member
-- leaves, is removed, or their account is deleted.

-- ---------------------------------------------------------------------------
-- conversations: name, creator, the creator's client ID (idempotent create) and closing.
-- ---------------------------------------------------------------------------
ALTER TABLE conversations
    ADD COLUMN name       text,
    ADD COLUMN created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN client_id  uuid,
    ADD COLUMN closed_at  timestamptz,
    -- A group is always active and was never a request; a direct conversation has none of
    -- the group's fields.
    ADD CONSTRAINT conversations_group_fields CHECK (
        (kind = 'direct' AND name IS NULL AND created_by IS NULL AND client_id IS NULL AND closed_at IS NULL)
        OR (kind = 'group' AND name IS NOT NULL AND client_id IS NOT NULL AND state = 'active' AND requested_by IS NULL)),
    -- 1 to 100 characters, trimmed, one line. The usecase is stricter (Unicode spaces).
    ADD CONSTRAINT conversations_name CHECK (
        char_length(name) BETWEEN 1 AND 100 AND name !~ '^\s' AND name !~ '\s$' AND name !~ '[[:cntrl:]]'),
    ADD CONSTRAINT conversations_closed_after_created CHECK (closed_at >= created_at),
    -- One group per creator and client ID: a retried create finds the first.
    ADD CONSTRAINT conversations_creator_client_uq UNIQUE (created_by, client_id);

-- ---------------------------------------------------------------------------
-- conversation_members: the owner role. joined_at uses the clock of messages, so "a member
-- sees messages created at or after joined_at" is a plain comparison; a group change sets it
-- to the instant of its system message.
-- ---------------------------------------------------------------------------
ALTER TABLE conversation_members
    ADD COLUMN role text NOT NULL DEFAULT 'member',
    ADD CONSTRAINT conversation_members_role CHECK (role IN ('owner', 'member')),
    ALTER COLUMN joined_at SET DEFAULT clock_timestamp();
CREATE UNIQUE INDEX conversation_members_one_owner_uq ON conversation_members (conversation_id) WHERE role = 'owner';

-- ---------------------------------------------------------------------------
-- conversation_memberships: one row per stay in a group, kept after the member row is gone.
-- Written only by the triggers below. Read only by the administrator's report paths (NFR-13).
-- ---------------------------------------------------------------------------
CREATE TABLE conversation_memberships (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    joined_at       timestamptz NOT NULL,
    left_at         timestamptz,
    CONSTRAINT conversation_memberships_order CHECK (left_at >= joined_at)
);
-- One open stay per member.
CREATE UNIQUE INDEX conversation_memberships_open_uq ON conversation_memberships (conversation_id, user_id) WHERE left_at IS NULL;
-- "Who was in the group at this moment", and the cascades.
CREATE INDEX conversation_memberships_conversation_idx ON conversation_memberships (conversation_id, joined_at);
CREATE INDEX conversation_memberships_user_idx ON conversation_memberships (user_id);

-- ---------------------------------------------------------------------------
-- messages: system events. A system message has no body, no client ID and is never edited or
-- deleted; its sender is the actor, or none when the change had no actor (an ownership
-- hand-over the trigger makes). A text message always has a sender.
-- ---------------------------------------------------------------------------
ALTER TABLE messages
    ADD COLUMN kind       text NOT NULL DEFAULT 'text',
    ADD COLUMN event      text,
    ADD COLUMN event_data jsonb,
    ALTER COLUMN sender_id DROP NOT NULL,
    ALTER COLUMN client_id DROP NOT NULL,
    ADD CONSTRAINT messages_kind CHECK (kind IN ('text', 'system')),
    ADD CONSTRAINT messages_event CHECK (event IN (
        'group_created', 'group_renamed', 'member_added', 'member_removed', 'member_left', 'owner_changed')),
    ADD CONSTRAINT messages_text_fields CHECK (
        kind <> 'text' OR (sender_id IS NOT NULL AND client_id IS NOT NULL AND event IS NULL AND event_data IS NULL)),
    ADD CONSTRAINT messages_system_fields CHECK (
        kind <> 'system' OR (body IS NULL AND client_id IS NULL AND event IS NOT NULL
                             AND jsonb_typeof(event_data) = 'object' AND edited_at IS NULL AND deleted_at IS NULL)),
    DROP CONSTRAINT messages_live_has_body,
    -- Slice 14B.3 relaxes this for messages that carry only attachments.
    ADD CONSTRAINT messages_live_has_body CHECK (kind = 'system' OR deleted_at IS NOT NULL OR body IS NOT NULL);

-- ---------------------------------------------------------------------------
-- message_reports: a report of a whole group names no reported user.
-- ---------------------------------------------------------------------------
ALTER TABLE message_reports ALTER COLUMN reported_id DROP NOT NULL;

-- ---------------------------------------------------------------------------
-- Triggers.
-- ---------------------------------------------------------------------------

-- A new member of a group opens a stay.
CREATE FUNCTION conversation_member_joined() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM conversations c WHERE c.id = NEW.conversation_id AND c.kind = 'group') THEN
        INSERT INTO conversation_memberships (conversation_id, user_id, joined_at)
        VALUES (NEW.conversation_id, NEW.user_id, NEW.joined_at);
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER conversation_members_joined
    AFTER INSERT ON conversation_members
    FOR EACH ROW EXECUTE FUNCTION conversation_member_joined();

-- A member row that goes away, by leaving, removal or a deleted account, ends the stay. When
-- it was the owner, the member who joined earliest (then the lower user ID) becomes owner and
-- an owner_changed message with no actor is written at the conversation's next instant (the
-- rule of InsertMessage), so the thread never shows a new owner without it. When nobody is
-- left, the group is closed. Nothing happens when the conversation itself is being deleted.
CREATE FUNCTION conversation_member_left() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    c       conversations%ROWTYPE;
    heir    uuid;
    instant timestamptz;
BEGIN
    SELECT * INTO c FROM conversations WHERE id = OLD.conversation_id FOR UPDATE;
    IF NOT FOUND OR c.kind <> 'group' THEN
        RETURN NULL;
    END IF;

    UPDATE conversation_memberships
    SET left_at = GREATEST(clock_timestamp(), joined_at)
    WHERE conversation_id = OLD.conversation_id AND user_id = OLD.user_id AND left_at IS NULL;

    IF NOT EXISTS (SELECT 1 FROM conversation_members WHERE conversation_id = OLD.conversation_id) THEN
        UPDATE conversations SET closed_at = GREATEST(clock_timestamp(), created_at)
        WHERE id = OLD.conversation_id AND closed_at IS NULL;
        RETURN NULL;
    END IF;

    IF OLD.role <> 'owner' THEN
        RETURN NULL;
    END IF;

    SELECT user_id INTO heir FROM conversation_members
    WHERE conversation_id = OLD.conversation_id
    ORDER BY joined_at, user_id
    LIMIT 1;

    UPDATE conversation_members SET role = 'owner'
    WHERE conversation_id = OLD.conversation_id AND user_id = heir;

    instant := GREATEST(clock_timestamp(), c.last_message_at + interval '1 microsecond');
    INSERT INTO messages (id, conversation_id, sender_id, kind, event, event_data, created_at)
    VALUES (uuid_v7_at(instant), OLD.conversation_id, NULL, 'system', 'owner_changed',
            jsonb_build_object('newOwnerId', heir), instant);
    UPDATE conversations SET last_message_at = instant WHERE id = OLD.conversation_id;
    RETURN NULL;
END;
$$;

CREATE TRIGGER conversation_members_left
    AFTER DELETE ON conversation_members
    FOR EACH ROW EXECUTE FUNCTION conversation_member_left();
