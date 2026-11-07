-- The notification type of a warning (FR-15.5; slice 14A, chunk 14A.2b). It has a migration of
-- its own because PostgreSQL cannot use a new enum value in the transaction that adds it, and
-- the check in 000018 uses it.
ALTER TYPE notification_type ADD VALUE 'account_warning';
