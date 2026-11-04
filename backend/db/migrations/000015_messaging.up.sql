-- Messaging (FR-16.1 to FR-16.3, FR-16.10, NFR-14; slice 14A, chunk 14A.1). One conversation
-- per pair of users, its two members with their read markers, and its messages. `kind`
-- allows groups (slice 14B), which will leave user_low and user_high empty; edited_at and
-- deleted_at are for editing and deleting messages (also 14B) and stay empty until then.

-- uuid_v7 makes a time-ordered UUID (RFC 9562): 48 bits of Unix milliseconds, then random
-- bits. PostgreSQL 16 has no built-in for it. It reads clock_timestamp(), not now(), so
-- that an ID made after a row lock is later than those made before the lock was released.
CREATE FUNCTION uuid_v7() RETURNS uuid
LANGUAGE sql VOLATILE PARALLEL SAFE AS $$
    SELECT encode(
        set_bit(set_bit(
            overlay(uuid_send(gen_random_uuid())
                    PLACING substring(int8send((extract(epoch FROM clock_timestamp()) * 1000)::bigint) FROM 3)
                    FROM 1 FOR 6),
            52, 1), 53, 1),
        'hex')::uuid
$$;

CREATE TABLE conversations (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind            text        NOT NULL DEFAULT 'direct',
    state           text        NOT NULL,
    requested_by    uuid        REFERENCES users (id) ON DELETE CASCADE,
    user_low        uuid        REFERENCES users (id) ON DELETE CASCADE,
    user_high       uuid        REFERENCES users (id) ON DELETE CASCADE,
    created_at      timestamptz NOT NULL DEFAULT now(),
    last_message_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT conversations_kind CHECK (kind IN ('direct', 'group')),
    CONSTRAINT conversations_state CHECK (state IN ('active', 'request', 'declined')),
    CONSTRAINT conversations_requested CHECK (state = 'active' OR requested_by IS NOT NULL),
    CONSTRAINT conversations_pair CHECK (
        (kind = 'direct' AND user_low IS NOT NULL AND user_high IS NOT NULL AND user_low < user_high)
        OR (kind = 'group' AND user_low IS NULL AND user_high IS NULL)),
    CONSTRAINT conversations_pair_uq UNIQUE (user_low, user_high)
);
CREATE INDEX conversations_last_message_idx ON conversations (last_message_at DESC, id DESC);
-- For the cascades when an account is deleted; conversations_pair_uq covers user_low.
CREATE INDEX conversations_user_high_idx ON conversations (user_high);
CREATE INDEX conversations_requested_by_idx ON conversations (requested_by);

CREATE TABLE messages (
    id              uuid PRIMARY KEY DEFAULT uuid_v7(),
    conversation_id uuid        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    sender_id       uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body            text        NOT NULL,
    client_id       uuid        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    edited_at       timestamptz,
    deleted_at      timestamptz,
    CONSTRAINT messages_body_length CHECK (char_length(body) BETWEEN 1 AND 5000),
    CONSTRAINT messages_body_trimmed CHECK (body = btrim(body, E' \t\n\r\f\v')),
    CONSTRAINT messages_sender_client_uq UNIQUE (sender_id, client_id),
    -- Serves a conversation's messages newest first and unread counting, scanned backward,
    -- and is what the read marker's foreign key refers to.
    CONSTRAINT messages_conversation_id_uq UNIQUE (conversation_id, id)
);

CREATE TABLE conversation_members (
    conversation_id      uuid        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    user_id              uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    joined_at            timestamptz NOT NULL DEFAULT now(),
    last_read_message_id uuid,
    last_read_at         timestamptz,
    PRIMARY KEY (conversation_id, user_id),
    -- A read marker can only point at a message of its own conversation.
    CONSTRAINT conversation_members_last_read_fk
        FOREIGN KEY (conversation_id, last_read_message_id)
        REFERENCES messages (conversation_id, id) ON DELETE SET NULL (last_read_message_id)
);
-- A user's conversations; the list sorts them by conversations.last_message_at, which
-- lives in the other table, so no one index can serve both.
CREATE INDEX conversation_members_user_idx ON conversation_members (user_id, conversation_id);
-- For the foreign key when messages are deleted.
CREATE INDEX conversation_members_last_read_idx ON conversation_members (conversation_id, last_read_message_id);
