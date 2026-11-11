-- The notification type for an attachment the virus scan blocked (FR-16.7, NFR-13; slice 14B,
-- chunk 14B.3). A system notification: no actor, and the subject is the attachment (its
-- stored_files ID). It has a migration of its own, like 000017 and 000022: PostgreSQL cannot use
-- a new enum value in the transaction that adds it, and the check in 000024 uses it.
ALTER TYPE notification_type ADD VALUE 'attachment_blocked';
