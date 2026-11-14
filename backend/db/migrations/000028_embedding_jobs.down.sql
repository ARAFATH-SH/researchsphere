DROP TABLE ai_admin_actions;

DROP TRIGGER users_embedding_suspended ON users;
DROP TRIGGER users_embedding_eligible ON users;
DROP TRIGGER publications_embedding ON publications;
DROP TRIGGER user_publications_embedding ON user_publications;
DROP TRIGGER user_skills_embedding ON user_skills;
DROP TRIGGER user_interests_embedding ON user_interests;
DROP TRIGGER profiles_embedding ON profiles;
DROP FUNCTION embedding_delete_suspended();
DROP FUNCTION embedding_enqueue_user();
DROP FUNCTION embedding_enqueue_publication_authors();
DROP FUNCTION embedding_enqueue_user_row();
DROP FUNCTION embedding_enqueue(text, uuid, smallint, boolean);

DROP TABLE embedding_jobs;

DROP INDEX profile_embeddings_model_idx;

ALTER TABLE venue_embeddings
    DROP CONSTRAINT venue_embeddings_model,
    DROP CONSTRAINT venue_embeddings_content_hash,
    DROP CONSTRAINT venue_embeddings_chunk_count,
    DROP COLUMN created_at,
    DROP COLUMN chunk_count;
ALTER TABLE post_embeddings
    DROP CONSTRAINT post_embeddings_model,
    DROP CONSTRAINT post_embeddings_content_hash,
    DROP CONSTRAINT post_embeddings_chunk_count,
    DROP COLUMN created_at,
    DROP COLUMN chunk_count;
ALTER TABLE project_embeddings
    DROP CONSTRAINT project_embeddings_model,
    DROP CONSTRAINT project_embeddings_content_hash,
    DROP CONSTRAINT project_embeddings_chunk_count,
    DROP COLUMN created_at,
    DROP COLUMN chunk_count;
ALTER TABLE profile_embeddings
    DROP CONSTRAINT profile_embeddings_model,
    DROP CONSTRAINT profile_embeddings_content_hash,
    DROP CONSTRAINT profile_embeddings_chunk_count,
    DROP COLUMN created_at,
    DROP COLUMN chunk_count;
