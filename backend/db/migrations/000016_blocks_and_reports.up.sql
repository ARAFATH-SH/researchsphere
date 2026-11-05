-- Blocks, and reports of conversations with their audit trail (FR-16.9, NFR-14; slice 14A,
-- chunk 14A.2). A block is one row per direction, so two users can block each other.

CREATE TABLE blocks (
    blocker_id uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    blocked_id uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (blocker_id, blocked_id),
    CONSTRAINT blocks_not_self CHECK (blocker_id <> blocked_id)
);
-- For "who blocked this viewer" and the cascade when an account is deleted.
CREATE INDEX blocks_blocked_idx ON blocks (blocked_id, blocker_id);
-- The blocker's list, newest first.
CREATE INDEX blocks_blocker_created_idx ON blocks (blocker_id, created_at DESC);

-- created_at is the moment of the report: an administrator sees the conversation's messages
-- only up to it (NFR-14), so it uses the clock of messages.created_at, not the transaction's.
CREATE TABLE message_reports (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    reporter_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    reported_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    message_id      uuid,
    reason          text        NOT NULL,
    details         text,
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    resolved_at     timestamptz,
    resolved_by     uuid        REFERENCES users (id) ON DELETE SET NULL,
    outcome         text,
    resolution_note text,
    CONSTRAINT message_reports_reason CHECK (reason IN ('spam', 'harassment', 'inappropriate', 'other')),
    CONSTRAINT message_reports_details_length CHECK (char_length(details) <= 1000),
    CONSTRAINT message_reports_outcome CHECK (outcome IN ('dismissed', 'action_taken')),
    CONSTRAINT message_reports_note_length CHECK (char_length(resolution_note) <= 1000),
    CONSTRAINT message_reports_not_self CHECK (reporter_id <> reported_id),
    CONSTRAINT message_reports_resolved CHECK ((resolved_at IS NULL) = (outcome IS NULL)),
    CONSTRAINT message_reports_open_has_no_resolution CHECK (
        resolved_at IS NOT NULL OR (resolved_by IS NULL AND resolution_note IS NULL)),
    -- A reported message can only be one of the reported conversation.
    CONSTRAINT message_reports_message_fk
        FOREIGN KEY (conversation_id, message_id)
        REFERENCES messages (conversation_id, id) ON DELETE SET NULL (message_id)
);
-- The open and resolved lists, newest first.
CREATE INDEX message_reports_status_idx ON message_reports ((resolved_at IS NULL), created_at DESC, id DESC);
-- For the cascades and the foreign keys.
CREATE INDEX message_reports_conversation_idx ON message_reports (conversation_id, message_id);
CREATE INDEX message_reports_reporter_idx ON message_reports (reporter_id);
CREATE INDEX message_reports_reported_idx ON message_reports (reported_id);
CREATE INDEX message_reports_resolved_by_idx ON message_reports (resolved_by);

-- One row for every administrator's read of a reported conversation (NFR-14). report_id has
-- no foreign key on purpose: this record outlives a report that cascades away with its
-- conversation. It holds no message content.
CREATE TABLE message_report_views (
    id        uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    report_id uuid        NOT NULL,
    admin_id  uuid        REFERENCES users (id) ON DELETE SET NULL,
    viewed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX message_report_views_report_idx ON message_report_views (report_id, viewed_at DESC);
CREATE INDEX message_report_views_admin_idx ON message_report_views (admin_id);
