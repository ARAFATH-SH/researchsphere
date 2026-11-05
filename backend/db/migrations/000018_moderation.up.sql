-- Moderation actions: warnings, messaging restrictions, timed suspensions and their audit
-- trail (FR-3.4, FR-15.3, FR-15.5 to FR-15.10; slice 14A, chunk 14A.2b).

-- A system notification has no actor: it comes from ResearchSphere. Each later system type
-- joins this list.
ALTER TABLE notifications
    ALTER COLUMN actor_id DROP NOT NULL,
    ADD CONSTRAINT notifications_actor_by_type CHECK ((actor_id IS NULL) = (type = 'account_warning'));

-- suspended_at stays what every query reads. suspended_until empty means permanent. An account
-- suspended by hand before this migration has no reason.
ALTER TABLE users
    ADD COLUMN suspended_until   timestamptz,
    ADD COLUMN suspension_reason text,
    ADD CONSTRAINT users_suspended_until CHECK (
        suspended_until IS NULL OR (suspended_at IS NOT NULL AND suspended_until > suspended_at)),
    ADD CONSTRAINT users_suspension_reason CHECK (
        suspension_reason IS NULL
        OR (suspended_at IS NOT NULL AND char_length(suspension_reason) BETWEEN 10 AND 1000));

CREATE TABLE account_warnings (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    reason          text        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    acknowledged_at timestamptz,
    CONSTRAINT account_warnings_reason_length CHECK (char_length(reason) BETWEEN 10 AND 1000)
);
-- The banner's list, newest first.
CREATE INDEX account_warnings_pending_idx ON account_warnings (user_id, created_at DESC, id DESC)
    WHERE acknowledged_at IS NULL;
CREATE INDEX account_warnings_user_idx ON account_warnings (user_id);

-- A restriction is active while lifted_at is empty and ends_at has not passed. That depends on
-- the clock, so no index can make it unique; the action locks the user's row instead.
CREATE TABLE account_restrictions (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       text        NOT NULL,
    starts_at  timestamptz NOT NULL,
    ends_at    timestamptz NOT NULL,
    lifted_at  timestamptz,
    lifted_by  uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT account_restrictions_kind CHECK (kind IN ('messaging')),
    CONSTRAINT account_restrictions_ends CHECK (ends_at > starts_at),
    CONSTRAINT account_restrictions_lifted CHECK (lifted_at IS NOT NULL OR lifted_by IS NULL)
);
CREATE INDEX account_restrictions_active_idx ON account_restrictions (user_id, kind, ends_at DESC);
CREATE INDEX account_restrictions_lifted_by_idx ON account_restrictions (lifted_by);

-- The audit trail: one row per action, never updated. The note is internal: it never leaves
-- the administrators' history. report_id has no foreign key on purpose, like
-- message_report_views: this record outlives a report that cascades away with its conversation.
CREATE TABLE moderation_actions (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    admin_id       uuid        REFERENCES users (id) ON DELETE SET NULL,
    action         text        NOT NULL,
    reason         text        NOT NULL,
    note           text,
    report_id      uuid,
    warning_id     uuid        REFERENCES account_warnings (id) ON DELETE CASCADE,
    restriction_id uuid        REFERENCES account_restrictions (id) ON DELETE CASCADE,
    ends_at        timestamptz,
    created_at     timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT moderation_actions_action CHECK (
        action IN ('warning', 'restriction', 'restriction_lifted', 'suspension', 'restoration')),
    CONSTRAINT moderation_actions_reason_length CHECK (char_length(reason) BETWEEN 10 AND 1000),
    CONSTRAINT moderation_actions_note_length CHECK (char_length(note) <= 1000),
    CONSTRAINT moderation_actions_warning CHECK ((action = 'warning') = (warning_id IS NOT NULL)),
    CONSTRAINT moderation_actions_restriction CHECK (
        (action IN ('restriction', 'restriction_lifted')) = (restriction_id IS NOT NULL)),
    CONSTRAINT moderation_actions_ends CHECK (
        CASE action
            WHEN 'restriction' THEN ends_at IS NOT NULL
            WHEN 'suspension' THEN true -- empty means permanent
            ELSE ends_at IS NULL
        END),
    -- Lifting and restoring reverse an action; they resolve no report.
    CONSTRAINT moderation_actions_report CHECK (
        report_id IS NULL OR action IN ('warning', 'restriction', 'suspension'))
);
-- The history, newest first.
CREATE INDEX moderation_actions_user_idx ON moderation_actions (user_id, created_at DESC, id DESC);
-- For the cascades and the foreign keys.
CREATE INDEX moderation_actions_admin_idx ON moderation_actions (admin_id);
CREATE INDEX moderation_actions_warning_idx ON moderation_actions (warning_id);
CREATE INDEX moderation_actions_restriction_idx ON moderation_actions (restriction_id);
