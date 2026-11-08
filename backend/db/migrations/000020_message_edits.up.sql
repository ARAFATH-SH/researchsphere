-- Editing and deleting one's own messages (FR-16.5, NFR-14; slice 14B, chunk 14B.1).
-- A deleted message stays in place with no body. Every text an edit or a deletion replaces is
-- kept in message_versions, which only the administrator's report read reaches; members never
-- see it. Retention (docs/decisions.md, 2026-11-08): versions are purged 30 days after
-- shown_until unless the conversation was reported at or after that text was shown; the job
-- comes in slice 29A.

-- One row per replaced text. shown_from is when the text started to show (the message's
-- created_at or the edit that wrote it); shown_until is the edit or deletion that replaced
-- it, the same instant as messages.edited_at or deleted_at. Both use clock_timestamp(), the
-- clock of messages.created_at and message_reports.created_at, so "the text at the report's
-- moment" is a plain comparison.
CREATE TABLE message_versions (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid        NOT NULL,
    message_id      uuid        NOT NULL,
    body            text        NOT NULL,
    shown_from      timestamptz NOT NULL,
    shown_until     timestamptz NOT NULL DEFAULT clock_timestamp(),
    ended_by        text        NOT NULL,
    CONSTRAINT message_versions_ended_by CHECK (ended_by IN ('edited', 'deleted')),
    CONSTRAINT message_versions_body_length CHECK (char_length(body) BETWEEN 1 AND 5000),
    CONSTRAINT message_versions_order CHECK (shown_until >= shown_from),
    -- Goes with its message, so with the conversation and a deleted account (SRS 7, Retention).
    CONSTRAINT message_versions_message_fk
        FOREIGN KEY (conversation_id, message_id)
        REFERENCES messages (conversation_id, id) ON DELETE CASCADE
);
-- A message is deleted once.
CREATE UNIQUE INDEX message_versions_one_deletion_uq ON message_versions (message_id) WHERE ended_by = 'deleted';
-- A message's versions in order, and the foreign key's cascade.
CREATE INDEX message_versions_message_idx ON message_versions (conversation_id, message_id, shown_until);
-- For the purge job (slice 29A).
CREATE INDEX message_versions_shown_until_idx ON message_versions (shown_until);

-- A message marked deleted before this migration (the down of this one leaves its text in
-- place) becomes a tombstone again, its text kept as its deletion.
ALTER TABLE messages ALTER COLUMN body DROP NOT NULL;
INSERT INTO message_versions (conversation_id, message_id, body, shown_from, shown_until, ended_by)
SELECT conversation_id, id, body, COALESCE(edited_at, created_at),
       GREATEST(deleted_at, COALESCE(edited_at, created_at)), 'deleted'
FROM messages
WHERE deleted_at IS NOT NULL AND body IS NOT NULL;
UPDATE messages SET body = NULL WHERE deleted_at IS NOT NULL;

-- messages_body_length and messages_body_trimmed already pass a NULL body.
ALTER TABLE messages
    ADD CONSTRAINT messages_deleted_has_no_body CHECK (deleted_at IS NULL OR body IS NULL),
    -- Slice 14B.3 relaxes this for messages that carry only attachments.
    ADD CONSTRAINT messages_live_has_body CHECK (deleted_at IS NOT NULL OR body IS NOT NULL),
    ADD CONSTRAINT messages_edited_after_created CHECK (edited_at >= created_at),
    ADD CONSTRAINT messages_deleted_after_created CHECK (deleted_at >= created_at);
