-- Message attachments and their virus scan (FR-16.7, NFR-13; slice 14B, chunk 14B.3).
-- docs/decisions.md, 2026-11-09 (attachments).
--
-- An attachment is a stored_files row (purpose 'attachment') plus a message_attachments row that
-- says which conversation it was uploaded to, its display name and, once sent, its message. The
-- file is uploaded first and sent with a message later; an unused one expires after 24 hours.
-- Nobody downloads a file before its scan says clean.

-- ---------------------------------------------------------------------------
-- stored_files: the scan's outcome and its retry state. 'rejected' (000007) was never written;
-- the scan ends as clean, infected (the object is deleted, the row kept as a record) or failed
-- (the scanner could not read the file after 5 tries). The lease lets one worker own a file
-- while it streams it to the scanner without holding a transaction open: a claim sets
-- scan_lease_until and a fresh scan_lease_token, and the result is written only with that token.
-- ---------------------------------------------------------------------------
ALTER TABLE stored_files
    DROP CONSTRAINT stored_files_scan_status_check,
    ADD CONSTRAINT stored_files_scan_status_check
        CHECK (scan_status IN ('pending', 'clean', 'infected', 'failed')),
    ADD COLUMN scan_attempts    integer NOT NULL DEFAULT 0
        CONSTRAINT stored_files_scan_attempts_check CHECK (scan_attempts BETWEEN 0 AND 5),
    ADD COLUMN scan_next_at     timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN scan_lease_until timestamptz,
    ADD COLUMN scan_lease_token uuid,
    -- A short code of ours (for example 'scanner_error', 'object_missing'), never the scanner's text.
    ADD COLUMN scan_error       text
        CONSTRAINT stored_files_scan_error_check CHECK (scan_error ~ '^[a-z_]{1,40}$'),
    ADD COLUMN scanned_at       timestamptz,
    ADD CONSTRAINT stored_files_lease_pair CHECK ((scan_lease_until IS NULL) = (scan_lease_token IS NULL)),
    ADD CONSTRAINT stored_files_scanned CHECK ((scan_status = 'pending') = (scanned_at IS NULL)
                                               OR purpose <> 'attachment'),
    -- FR-16.7: 25 MiB, after an image is re-encoded too.
    ADD CONSTRAINT stored_files_attachment_size CHECK (purpose <> 'attachment' OR size_bytes <= 26214400);

-- The worker's queue: pending files by when they are due.
CREATE INDEX stored_files_scan_due_idx ON stored_files (scan_next_at) WHERE scan_status = 'pending';

-- ---------------------------------------------------------------------------
-- messages: a message may carry attachments instead of text. The flag is set in the send's
-- transaction, so the body rule below stays a plain CHECK.
-- ---------------------------------------------------------------------------
ALTER TABLE messages
    ADD COLUMN has_attachments boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT messages_attachments_text_only CHECK (NOT has_attachments OR kind = 'text'),
    DROP CONSTRAINT messages_live_has_body,
    ADD CONSTRAINT messages_live_has_body
        CHECK (kind = 'system' OR deleted_at IS NOT NULL OR body IS NOT NULL OR has_attachments);

-- ---------------------------------------------------------------------------
-- message_attachments: one row per attachment, keyed by its stored file. The file belongs to the
-- uploader (the foreign key includes the owner). message_id is null until the attachment is sent;
-- then it never changes. A message has at most 5, in position order.
-- ---------------------------------------------------------------------------
CREATE TABLE message_attachments (
    file_id         uuid        PRIMARY KEY,
    uploader_id     uuid        NOT NULL,
    conversation_id uuid        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    message_id      uuid,
    position        smallint,
    name            text        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz NOT NULL,
    CONSTRAINT message_attachments_file_fk FOREIGN KEY (file_id, uploader_id)
        REFERENCES stored_files (id, owner_user_id) ON DELETE CASCADE,
    CONSTRAINT message_attachments_message_fk FOREIGN KEY (conversation_id, message_id)
        REFERENCES messages (conversation_id, id) ON DELETE CASCADE,
    CONSTRAINT message_attachments_sent CHECK ((message_id IS NULL) = (position IS NULL)),
    CONSTRAINT message_attachments_position CHECK (position BETWEEN 0 AND 4),
    -- The usecase is stricter (sanitized, no path, no quotes).
    CONSTRAINT message_attachments_name CHECK (
        char_length(name) BETWEEN 1 AND 200 AND name !~ '[[:cntrl:]]' AND position('/' IN name) = 0),
    CONSTRAINT message_attachments_expiry CHECK (expires_at > created_at),
    CONSTRAINT message_attachments_message_position_uq UNIQUE (message_id, position)
);
-- A message's attachments, and the foreign key's cascade.
CREATE INDEX message_attachments_message_idx ON message_attachments (conversation_id, message_id);
-- The purge of unused attachments.
CREATE INDEX message_attachments_unused_idx ON message_attachments (expires_at) WHERE message_id IS NULL;
-- A user's attachments (account deletion cascades through stored_files).
CREATE INDEX message_attachments_uploader_idx ON message_attachments (uploader_id);

-- An attachment row that goes, by the purge, a conversation or message that cascades, or a
-- deleted account, takes its stored file with it; that row's trigger (000007) queues the object.
CREATE FUNCTION message_attachment_deleted() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM stored_files WHERE id = OLD.file_id;
    RETURN NULL;
END;
$$;
CREATE TRIGGER message_attachments_delete_file
    AFTER DELETE ON message_attachments
    FOR EACH ROW EXECUTE FUNCTION message_attachment_deleted();

-- ---------------------------------------------------------------------------
-- notifications: attachment_blocked is a system notification (no actor), as account_warning.
-- ---------------------------------------------------------------------------
ALTER TABLE notifications
    DROP CONSTRAINT notifications_actor_by_type,
    ADD CONSTRAINT notifications_actor_by_type
        CHECK ((actor_id IS NULL) = (type IN ('account_warning', 'attachment_blocked')));

-- ---------------------------------------------------------------------------
-- message_report_attachment_downloads: one row for every administrator's download of an
-- attachment through a report (NFR-13). Like message_report_views, report_id and file_id have
-- no foreign key: the record outlives the report and the file.
-- ---------------------------------------------------------------------------
CREATE TABLE message_report_attachment_downloads (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    report_id     uuid        NOT NULL,
    file_id       uuid        NOT NULL,
    admin_id      uuid        REFERENCES users (id) ON DELETE SET NULL,
    downloaded_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX message_report_attachment_downloads_report_idx
    ON message_report_attachment_downloads (report_id, downloaded_at DESC);
CREATE INDEX message_report_attachment_downloads_admin_idx
    ON message_report_attachment_downloads (admin_id);
