-- The notification types for membership changes the owner makes (FR-11.4, FR-10.5; slice 20,
-- chunk 20.1): a member's role changed, a member removed, ownership given to a member. The
-- join request and invitation types (FR-9.2) exist since 000001. The actor is the owner who
-- made the change and the subject is the project. It has a migration of its own, like 000017,
-- 000022 and 000023: PostgreSQL cannot use a new enum value in the transaction that adds it,
-- and the index in 000034 uses them.
ALTER TYPE notification_type ADD VALUE 'project_member_role_changed';
ALTER TYPE notification_type ADD VALUE 'project_member_removed';
ALTER TYPE notification_type ADD VALUE 'project_ownership_transferred';
