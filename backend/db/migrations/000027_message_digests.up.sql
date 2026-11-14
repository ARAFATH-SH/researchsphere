-- The unread message digest (FR-16.11, NFR-13; slice 14B, chunk 14B.6).
-- docs/decisions.md, 2026-11-14 (unread message digest).

-- Account settings, one row per user who changed one; a missing row means the defaults. Later
-- settings are added as columns here.
CREATE TABLE user_settings (
    user_id               uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    unread_message_digest boolean     NOT NULL DEFAULT true,
    updated_at            timestamptz NOT NULL DEFAULT now()
);

-- Where each user's digests stand. The job locks this row (FOR UPDATE SKIP LOCKED), so two API
-- instances never send the same user a digest; it is apart from user_settings so a settings change
-- never waits for a send. covered_until is the watermark: a digest counts only messages created
-- after the previous covered_until and at most 24 hours before it was sent, so no message is ever
-- in two digests.
CREATE TABLE message_digest_state (
    user_id        uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    last_digest_at timestamptz,
    covered_until  timestamptz,
    CONSTRAINT message_digest_state_sent_together CHECK ((last_digest_at IS NULL) = (covered_until IS NULL)),
    CONSTRAINT message_digest_state_covered CHECK (covered_until <= last_digest_at)
);

-- One row per digest sent, for support. Counts only: never message IDs or text.
CREATE TABLE message_digests (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    sent_at            timestamptz NOT NULL,
    covered_until      timestamptz NOT NULL,
    unread_count       integer     NOT NULL,
    conversation_count integer     NOT NULL,
    request_count      integer     NOT NULL,
    CONSTRAINT message_digests_covered CHECK (covered_until <= sent_at),
    CONSTRAINT message_digests_unread CHECK (unread_count >= 1),
    CONSTRAINT message_digests_conversations CHECK (conversation_count BETWEEN 1 AND unread_count),
    CONSTRAINT message_digests_requests CHECK (request_count >= 0)
);
CREATE INDEX message_digests_user_sent_idx ON message_digests (user_id, sent_at DESC);
