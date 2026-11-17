-- Project membership (FR-11.1 to FR-11.5, FR-10.5; slice 20, chunk 20.1). project_members and
-- project_join_requests exist since 000001: the owner is the member row with role 'owner'
-- (one at most by a unique index, at least one by a deferred trigger), and a join request or
-- invitation is a project_join_requests row of kind 'request' or 'invitation', with one
-- pending row per user and project across both kinds. Cancelling deletes the row.

-- An invitation offers a role (FR-11.3); a request gets its role when accepted (FR-11.2).
ALTER TABLE project_join_requests ADD COLUMN role project_role;

ALTER TABLE project_join_requests
    -- Only an invitation has a role, and never 'owner': ownership moves only by transfer.
    ADD CONSTRAINT project_join_requests_role
        CHECK ((kind = 'invitation') = (role IS NOT NULL) AND (role IS NULL OR role <> 'owner')),
    -- A request's message is a short note, 1 to 500 characters (the API trims it and stores
    -- an empty one as NULL); an invitation has none.
    ADD CONSTRAINT project_join_requests_message
        CHECK (message IS NULL OR (kind = 'request' AND char_length(message) BETWEEN 1 AND 500)),
    -- A decided row has its time, a pending one has none.
    ADD CONSTRAINT project_join_requests_decided
        CHECK ((status = 'pending') = (decided_at IS NULL));

-- The owner's lists: one kind of one project, pending first.
CREATE INDEX project_join_requests_project_idx
    ON project_join_requests (project_id, kind, status, created_at);

-- One notification of each project type per recipient, actor and project: sending the same
-- thing again (a second role change, a new request after a rejection) replaces the old one
-- (new time, unread), as notifications_connection_pair_uq does for connections.
CREATE UNIQUE INDEX notifications_project_uq ON notifications (user_id, actor_id, type, entity_id)
    WHERE type IN ('project_join_request', 'project_join_request_accepted', 'project_join_request_rejected',
                   'project_invitation', 'project_invitation_accepted', 'project_invitation_rejected',
                   'project_member_role_changed', 'project_member_removed', 'project_ownership_transferred');
