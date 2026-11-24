-- Venue recommendations (AI-6.1; slice 25, chunk 25.1). docs/decisions.md, 2026-11-24.
--
-- AI-6.1 ranks venues by similarity to a project's title and abstract. The project embedding
-- of AI-1.2 (project_embeddings) is made from title, required skills, description and
-- abstract, so each project also gets a second vector made from its title and abstract only.
-- The existing project job (embedding_jobs owner_type 'project') makes both: its trigger
-- already fires on every change of title or abstract. A project without an abstract has no
-- row here. One row per project, deleted with it.
--
-- No HNSW index: this vector is only read by project, and the search runs on
-- venue_embeddings_hnsw_idx.

CREATE TABLE project_abstract_embeddings (
    project_id   uuid        PRIMARY KEY REFERENCES projects (id) ON DELETE CASCADE,
    model        text        NOT NULL,
    content_hash text        NOT NULL,
    embedding    vector(768) NOT NULL,
    chunk_count  smallint    NOT NULL DEFAULT 1,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT project_abstract_embeddings_chunk_count CHECK (chunk_count BETWEEN 1 AND 8)
);
