-- The notification type for being added to a group (FR-16.6, FR-9.1; slice 14B, chunk 14B.2).
-- The actor is the group's owner and the subject is the conversation. It has a migration of its
-- own, like 000017: PostgreSQL cannot use a new enum value in the transaction that adds it.
ALTER TYPE notification_type ADD VALUE 'group_added';
