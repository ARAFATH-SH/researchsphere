DROP INDEX notifications_project_uq;
DROP INDEX project_join_requests_project_idx;
ALTER TABLE project_join_requests
    DROP CONSTRAINT project_join_requests_decided,
    DROP CONSTRAINT project_join_requests_message,
    DROP CONSTRAINT project_join_requests_role,
    DROP COLUMN role;
