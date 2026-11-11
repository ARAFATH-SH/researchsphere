DROP TABLE message_report_attachment_downloads;

DELETE FROM notifications WHERE type = 'attachment_blocked';
ALTER TABLE notifications
    DROP CONSTRAINT notifications_actor_by_type,
    ADD CONSTRAINT notifications_actor_by_type CHECK ((actor_id IS NULL) = (type = 'account_warning'));

-- Every attachment goes, and with it its stored file, whose trigger queues the object. A live
-- message that had only attachments has nothing left to show, so it goes too (as 000020's down
-- drops tombstones without a text).
DELETE FROM message_attachments;
DROP TRIGGER message_attachments_delete_file ON message_attachments;
DROP FUNCTION message_attachment_deleted();
DROP TABLE message_attachments;
DELETE FROM stored_files WHERE purpose = 'attachment';

DELETE FROM messages WHERE kind = 'text' AND deleted_at IS NULL AND body IS NULL;
ALTER TABLE messages
    DROP CONSTRAINT messages_live_has_body,
    ADD CONSTRAINT messages_live_has_body CHECK (kind = 'system' OR deleted_at IS NOT NULL OR body IS NOT NULL),
    DROP CONSTRAINT messages_attachments_text_only,
    DROP COLUMN has_attachments;

DROP INDEX stored_files_scan_due_idx;
UPDATE stored_files SET scan_status = 'rejected' WHERE scan_status IN ('infected', 'failed');
ALTER TABLE stored_files
    DROP CONSTRAINT stored_files_attachment_size,
    DROP CONSTRAINT stored_files_scanned,
    DROP CONSTRAINT stored_files_lease_pair,
    DROP COLUMN scanned_at,
    DROP COLUMN scan_error,
    DROP COLUMN scan_lease_token,
    DROP COLUMN scan_lease_until,
    DROP COLUMN scan_next_at,
    DROP COLUMN scan_attempts,
    DROP CONSTRAINT stored_files_scan_status_check,
    ADD CONSTRAINT stored_files_scan_status_check CHECK (scan_status IN ('pending', 'clean', 'rejected'));
