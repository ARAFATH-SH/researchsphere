DROP TABLE project_reports;

DROP TRIGGER projects_embedding_delete ON projects;
DROP TRIGGER project_skills_embedding ON project_skills;
DROP TRIGGER projects_embedding_update ON projects;
DROP TRIGGER projects_embedding_insert ON projects;
DROP FUNCTION embedding_drop_project_job();
DROP FUNCTION embedding_enqueue_project_skill();
DROP FUNCTION embedding_enqueue_project();

DELETE FROM embedding_jobs WHERE owner_type = 'project';
ALTER TABLE embedding_jobs DROP CONSTRAINT embedding_jobs_owner_type;
ALTER TABLE embedding_jobs ADD CONSTRAINT embedding_jobs_owner_type CHECK (owner_type IN ('profile'));

DROP TRIGGER project_skills_max ON project_skills;
DROP FUNCTION ensure_project_skills_max();

DROP INDEX projects_description_trgm_idx;
DROP INDEX projects_title_trgm_idx;

ALTER TABLE projects
    DROP CONSTRAINT projects_abstract_length,
    DROP CONSTRAINT projects_description_length,
    DROP CONSTRAINT projects_title_length;

ALTER TABLE projects ALTER COLUMN id SET DEFAULT gen_random_uuid();
