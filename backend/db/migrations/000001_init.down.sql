-- ResearchSphere: undo 000001_init.up.sql
-- Removes every table, function, type and extension the up file creates, so the database is
-- left as it was before. CI checks this by migrating up, down and up again.

BEGIN;

DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS assistant_requests;
DROP TABLE IF EXISTS venue_embeddings;
DROP TABLE IF EXISTS post_embeddings;
DROP TABLE IF EXISTS project_embeddings;
DROP TABLE IF EXISTS profile_embeddings;
DROP TABLE IF EXISTS venues;
DROP TABLE IF EXISTS post_comments;
DROP TABLE IF EXISTS post_likes;
DROP TABLE IF EXISTS posts;
DROP TABLE IF EXISTS compile_jobs;
DROP TABLE IF EXISTS project_files;
DROP TABLE IF EXISTS project_join_requests;
DROP TABLE IF EXISTS project_members;
DROP TABLE IF EXISTS project_skills;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS connections;
DROP TABLE IF EXISTS user_publications;
DROP TABLE IF EXISTS publications;
DROP TABLE IF EXISTS education;
DROP TABLE IF EXISTS user_skills;
DROP TABLE IF EXISTS user_interests;
DROP TABLE IF EXISTS skills;
DROP TABLE IF EXISTS research_interests;
DROP TABLE IF EXISTS profiles;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS auth_tokens;
DROP TABLE IF EXISTS users;

DROP FUNCTION IF EXISTS ensure_project_has_owner();
DROP FUNCTION IF EXISTS set_updated_at();

DROP TYPE IF EXISTS assistant_kind;
DROP TYPE IF EXISTS notification_type;
DROP TYPE IF EXISTS venue_type;
DROP TYPE IF EXISTS compile_status;
DROP TYPE IF EXISTS file_kind;
DROP TYPE IF EXISTS join_request_status;
DROP TYPE IF EXISTS join_request_kind;
DROP TYPE IF EXISTS project_role;
DROP TYPE IF EXISTS project_status;
DROP TYPE IF EXISTS connection_status;
DROP TYPE IF EXISTS publication_source;
DROP TYPE IF EXISTS auth_token_purpose;
DROP TYPE IF EXISTS user_role;

DROP EXTENSION IF EXISTS vector;
DROP EXTENSION IF EXISTS pg_trgm;
DROP EXTENSION IF EXISTS citext;

COMMIT;
