-- ResearchSphere: initial schema
-- Target: PostgreSQL 16 with pgvector 0.5 or newer (HNSW indexes).
-- Format: golang-migrate (000001_init.up.sql / 000001_init.down.sql).
-- Requirement IDs in comments (FR-n, AI-n, NFR-n) refer to docs/SRS.md. Section 7 of the SRS
-- lists the records and the rules the database enforces.

BEGIN;

CREATE EXTENSION IF NOT EXISTS citext;   -- case-insensitive email, DOI, tag names
CREATE EXTENSION IF NOT EXISTS pg_trgm;  -- fuzzy name search (FR-6.2)
CREATE EXTENSION IF NOT EXISTS vector;   -- embeddings and similarity search (AI-1)

-- ---------------------------------------------------------------------------
-- Enumerated types
-- ---------------------------------------------------------------------------

CREATE TYPE user_role           AS ENUM ('researcher', 'admin');
CREATE TYPE auth_token_purpose  AS ENUM ('email_verification', 'password_reset');
CREATE TYPE publication_source  AS ENUM ('manual', 'orcid', 'doi');
CREATE TYPE connection_status   AS ENUM ('pending', 'accepted', 'rejected');
CREATE TYPE project_status      AS ENUM ('open', 'closed', 'archived');
CREATE TYPE project_role        AS ENUM ('owner', 'editor', 'viewer');
CREATE TYPE join_request_kind   AS ENUM ('request', 'invitation');
CREATE TYPE join_request_status AS ENUM ('pending', 'accepted', 'rejected');
CREATE TYPE file_kind           AS ENUM ('text', 'binary', 'folder');
CREATE TYPE compile_status      AS ENUM ('queued', 'running', 'succeeded', 'failed', 'timed_out');
CREATE TYPE venue_type          AS ENUM ('conference', 'journal', 'workshop');
CREATE TYPE notification_type   AS ENUM (
    'connection_request',             -- FR-9.1
    'connection_accepted',            -- FR-9.1
    'project_join_request',           -- FR-9.2
    'project_invitation',             -- FR-9.2
    'project_join_request_accepted',  -- FR-9.2
    'project_join_request_rejected',  -- FR-9.2
    'project_invitation_accepted',    -- FR-9.2
    'project_invitation_rejected',    -- FR-9.2
    'post_comment',                   -- FR-9.3
    'post_like'                       -- FR-9.7
);
CREATE TYPE assistant_kind      AS ENUM ('explain_error', 'draft_title', 'draft_abstract', 'draft_keywords');

-- Keeps updated_at current on every UPDATE.
CREATE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

-- ---------------------------------------------------------------------------
-- auth module (FR-1, FR-2, FR-3, NFR-5)
-- ---------------------------------------------------------------------------

CREATE TABLE users (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email             citext      NOT NULL UNIQUE,
    password_hash     text        NOT NULL,                 -- salted hash, never the password
    role              user_role   NOT NULL DEFAULT 'researcher',
    email_verified_at timestamptz,                          -- login refused while NULL (FR-1.4)
    suspended_at      timestamptz,                          -- login refused while set (FR-3.4, FR-15.3)
    ai_consent_at     timestamptz,                          -- set when the user accepts the assistant notice (AI-8.3)
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- One-time tokens for email verification and password reset. Only the hash is stored.
CREATE TABLE auth_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid               NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose    auth_token_purpose NOT NULL,
    token_hash text               NOT NULL UNIQUE,
    expires_at timestamptz        NOT NULL,
    used_at    timestamptz,
    created_at timestamptz        NOT NULL DEFAULT now()
);
CREATE INDEX auth_tokens_user_idx ON auth_tokens (user_id, purpose);

-- Refresh tokens, one row per logged-in device. Logout sets revoked_at.
CREATE TABLE sessions (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    refresh_token_hash text        NOT NULL UNIQUE,
    expires_at         timestamptz NOT NULL,
    revoked_at         timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_idx ON sessions (user_id);

-- ---------------------------------------------------------------------------
-- profile module (FR-4, FR-5, FR-6)
-- ---------------------------------------------------------------------------

CREATE TABLE profiles (
    user_id     uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    full_name   text        NOT NULL CHECK (length(btrim(full_name)) > 0),
    headline    text,
    bio         text,
    institution text,
    department  text,
    job_title   text,
    country     text,
    orcid_id    text UNIQUE CHECK (orcid_id ~ '^\d{4}-\d{4}-\d{4}-\d{3}[\dX]$'),
    -- Keyword search over name and institution (FR-6.1). Interests are searched through user_interests.
    search_tsv  tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', coalesce(full_name, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(institution, '')), 'B')
    ) STORED,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX profiles_search_idx      ON profiles USING gin (search_tsv);
CREATE INDEX profiles_name_trgm_idx   ON profiles USING gin (full_name gin_trgm_ops);
CREATE INDEX profiles_institution_idx ON profiles (lower(institution));

-- Shared tag vocabularies. citext makes "NLP" and "nlp" the same tag (FR-4.2).
CREATE TABLE research_interests (
    id   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name citext NOT NULL UNIQUE CHECK (length(btrim(name)) > 0)
);

CREATE TABLE skills (
    id   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name citext NOT NULL UNIQUE CHECK (length(btrim(name)) > 0)
);

CREATE TABLE user_interests (
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    interest_id uuid NOT NULL REFERENCES research_interests (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, interest_id)
);
CREATE INDEX user_interests_interest_idx ON user_interests (interest_id);

CREATE TABLE user_skills (
    user_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    skill_id uuid NOT NULL REFERENCES skills (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, skill_id)
);
CREATE INDEX user_skills_skill_idx ON user_skills (skill_id);

CREATE TABLE education (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    institution    text NOT NULL,
    degree         text NOT NULL,
    field_of_study text,
    start_year     integer CHECK (start_year BETWEEN 1900 AND 2100),
    end_year       integer CHECK (end_year BETWEEN 1900 AND 2100),
    created_at     timestamptz NOT NULL DEFAULT now(),
    CHECK (end_year IS NULL OR start_year IS NULL OR end_year >= start_year)
);
CREATE INDEX education_user_idx ON education (user_id);

-- A publication is stored once and linked to each author who is a user (FR-5.4).
-- The DOI is stored in its bare form (10.xxxx/...), so the same DOI cannot be stored twice
-- under different spellings such as a https://doi.org/ link.
CREATE TABLE publications (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    doi              citext UNIQUE CHECK (doi ~ '^10\.[0-9]{4,9}/\S+$'),
    title            text   NOT NULL CHECK (length(btrim(title)) > 0),
    abstract         text,
    venue_name       text,
    publication_year integer CHECK (publication_year BETWEEN 1900 AND 2100),
    url              text,
    source           publication_source NOT NULL DEFAULT 'manual',
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

-- confirmed_at stays NULL until the user confirms the publication is theirs (FR-5.5).
CREATE TABLE user_publications (
    user_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    publication_id uuid NOT NULL REFERENCES publications (id) ON DELETE CASCADE,
    confirmed_at   timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, publication_id)
);
CREATE INDEX user_publications_publication_idx ON user_publications (publication_id);

-- ---------------------------------------------------------------------------
-- social module: connections (FR-7)
-- ---------------------------------------------------------------------------

CREATE TABLE connections (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    requester_id uuid              NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    receiver_id  uuid              NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    status       connection_status NOT NULL DEFAULT 'pending',
    created_at   timestamptz       NOT NULL DEFAULT now(),
    responded_at timestamptz,
    CHECK (requester_id <> receiver_id)
);
-- At most one live (pending or accepted) connection per pair, in either direction (FR-7.2).
-- Rejected rows are kept as history and do not block a new request (FR-7.6).
CREATE UNIQUE INDEX connections_live_pair_uq
    ON connections (LEAST(requester_id, receiver_id), GREATEST(requester_id, receiver_id))
    WHERE status IN ('pending', 'accepted');
CREATE INDEX connections_receiver_idx  ON connections (receiver_id, status);
CREATE INDEX connections_requester_idx ON connections (requester_id, status);

-- ---------------------------------------------------------------------------
-- project module (FR-10, FR-11, FR-12, FR-13)
-- ---------------------------------------------------------------------------

CREATE TABLE projects (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title          text           NOT NULL CHECK (length(btrim(title)) > 0),
    description    text           NOT NULL,
    abstract       text,                               -- paper abstract, input to venue ranking (AI-6)
    status         project_status NOT NULL DEFAULT 'open',
    main_file_path text           NOT NULL DEFAULT 'main.tex',
    search_tsv     tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(description, '')), 'B')
    ) STORED,
    created_at     timestamptz    NOT NULL DEFAULT now(),
    updated_at     timestamptz    NOT NULL DEFAULT now()
);
CREATE INDEX projects_search_idx ON projects USING gin (search_tsv);
CREATE INDEX projects_status_idx ON projects (status, created_at DESC);

CREATE TABLE project_skills (
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    skill_id   uuid NOT NULL REFERENCES skills (id) ON DELETE CASCADE,
    PRIMARY KEY (project_id, skill_id)
);
CREATE INDEX project_skills_skill_idx ON project_skills (skill_id);

-- Membership and role. The owner is the member row with role = 'owner'.
CREATE TABLE project_members (
    project_id uuid         NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    user_id    uuid         NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       project_role NOT NULL DEFAULT 'viewer',
    joined_at  timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);
CREATE UNIQUE INDEX project_members_one_owner_uq ON project_members (project_id) WHERE role = 'owner';
CREATE INDEX project_members_user_idx ON project_members (user_id);

-- Exactly one owner per project: the unique index above allows at most one, and this check,
-- run at commit, requires at least one. Creating a project and its owner row, or transferring
-- ownership (demote, then promote), therefore has to happen in one transaction.
-- A project that no longer exists passes, so deleting a project still cascades to its members.
CREATE FUNCTION ensure_project_has_owner() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    pid uuid;
BEGIN
    IF TG_TABLE_NAME = 'projects' THEN
        pid := NEW.id;
    ELSE
        pid := OLD.project_id;
    END IF;

    IF EXISTS (SELECT 1 FROM projects WHERE id = pid)
       AND NOT EXISTS (SELECT 1 FROM project_members WHERE project_id = pid AND role = 'owner') THEN
        RAISE EXCEPTION 'project % has no owner', pid
            USING ERRCODE = 'check_violation', CONSTRAINT = 'project_has_owner';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER projects_have_owner
    AFTER INSERT ON projects
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION ensure_project_has_owner();

CREATE CONSTRAINT TRIGGER project_members_keep_owner
    AFTER UPDATE OR DELETE ON project_members
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION ensure_project_has_owner();

-- kind = 'request': the user asked to join. kind = 'invitation': the owner invited the user.
CREATE TABLE project_join_requests (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid                NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    user_id    uuid                NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       join_request_kind   NOT NULL,
    status     join_request_status NOT NULL DEFAULT 'pending',
    message    text,
    created_at timestamptz         NOT NULL DEFAULT now(),
    decided_at timestamptz
);
-- One pending request or invitation per user per project (FR-11.1).
CREATE UNIQUE INDEX project_join_requests_pending_uq
    ON project_join_requests (project_id, user_id) WHERE status = 'pending';
CREATE INDEX project_join_requests_user_idx ON project_join_requests (user_id, status);

-- Files and folders of a project (FR-12.1). Text files are edited through Yjs; the sync server
-- persists the Yjs state in ydoc_state and a plain-text snapshot in content for compiling and
-- the assistant. Binary files (figures, FR-12.2) are kept outside the database and referenced
-- by storage_key.
-- A path is relative to the project: segments of letters, digits, '.', '_', ' ' and '-',
-- separated by single slashes, with no '.' or '..' segment, so it cannot point outside it.
CREATE TABLE project_files (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  uuid        NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    path        text        NOT NULL CHECK (
                    length(path) BETWEEN 1 AND 255
                    AND path ~ '^[A-Za-z0-9._ -]+(/[A-Za-z0-9._ -]+)*$'
                    AND path !~ '(^|/)\.{1,2}(/|$)'
                ),
    kind        file_kind   NOT NULL DEFAULT 'text',
    content     text,
    ydoc_state  bytea,
    storage_key text,
    size_bytes  bigint      NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, path),
    CHECK (kind <> 'text'   OR storage_key IS NULL),
    CHECK (kind <> 'binary' OR (storage_key IS NOT NULL AND content IS NULL AND ydoc_state IS NULL)),
    CHECK (kind <> 'folder' OR (storage_key IS NULL AND content IS NULL AND ydoc_state IS NULL AND size_bytes = 0)),
    -- Figures are PNG, JPG or PDF of up to 5 MB (FR-12.2).
    CHECK (kind <> 'binary' OR (size_bytes <= 5242880 AND path ~* '\.(png|jpe?g|pdf)$'))
);

CREATE TABLE compile_jobs (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      uuid           NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    requested_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    status          compile_status NOT NULL DEFAULT 'queued',
    pdf_storage_key text,
    log             text,
    queued_at       timestamptz    NOT NULL DEFAULT now(),
    started_at      timestamptz,
    finished_at     timestamptz
);
CREATE INDEX compile_jobs_project_idx ON compile_jobs (project_id, queued_at DESC);
-- One compile per project at a time (FR-13.4).
CREATE UNIQUE INDEX compile_jobs_one_active_uq ON compile_jobs (project_id) WHERE status IN ('queued', 'running');

-- ---------------------------------------------------------------------------
-- social module: feed (FR-8, FR-15)
-- ---------------------------------------------------------------------------

-- removed_at and removal_reason are set together by an administrator (FR-15.2).
CREATE TABLE posts (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    author_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body           text        NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 5000),
    publication_id uuid REFERENCES publications (id) ON DELETE SET NULL,
    project_id     uuid REFERENCES projects (id) ON DELETE SET NULL,
    removed_at     timestamptz,
    removal_reason text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CHECK (publication_id IS NULL OR project_id IS NULL),        -- at most one link (FR-8.1)
    CHECK ((removed_at IS NULL) = (removal_reason IS NULL))
);
CREATE INDEX posts_author_idx ON posts (author_id, created_at DESC) WHERE removed_at IS NULL;
CREATE INDEX posts_recent_idx ON posts (created_at DESC) WHERE removed_at IS NULL;

CREATE TABLE post_likes (
    post_id    uuid        NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, user_id)
);
CREATE INDEX post_likes_user_idx ON post_likes (user_id);

CREATE TABLE post_comments (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id        uuid        NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    author_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body           text        NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 2000),
    removed_at     timestamptz,
    removal_reason text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CHECK ((removed_at IS NULL) = (removal_reason IS NULL))
);
CREATE INDEX post_comments_post_idx ON post_comments (post_id, created_at);

-- ---------------------------------------------------------------------------
-- event module (FR-14, FR-15)
-- ---------------------------------------------------------------------------

CREATE TABLE venues (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name                text       NOT NULL CHECK (length(btrim(name)) > 0),
    type                venue_type NOT NULL,
    scope               text       NOT NULL,            -- the text that is embedded for venue ranking
    field               text,
    url                 text,
    location            text,
    starts_on           date,
    ends_on             date,
    submission_deadline date,
    source              text       NOT NULL DEFAULT 'admin',   -- 'admin' or the listing it was imported from
    source_ref          text,                                   -- id of the entry in that listing
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source, source_ref),                                -- re-importing a listing updates, never duplicates (FR-14.5)
    CHECK ((source = 'admin') = (source_ref IS NULL)),          -- an imported venue always has its source id
    CHECK (ends_on IS NULL OR starts_on IS NULL OR ends_on >= starts_on)
);
CREATE INDEX venues_deadline_idx ON venues (submission_deadline);
CREATE INDEX venues_field_idx    ON venues (lower(field), submission_deadline);

-- ---------------------------------------------------------------------------
-- AI module: embeddings (AI-1 to AI-6)
--
-- One table per owner type so that a deleted owner removes its embedding (foreign key)
-- and each owner type has its own nearest-neighbour index.
-- 768 is the output size of the chosen model (see docs/decisions.md). A model with another
-- size needs a new migration.
-- content_hash lets the AI service skip re-embedding text that has not changed (AI-1.4).
-- ---------------------------------------------------------------------------

CREATE TABLE profile_embeddings (
    user_id      uuid PRIMARY KEY REFERENCES profiles (user_id) ON DELETE CASCADE,
    model        text        NOT NULL,
    content_hash text        NOT NULL,
    embedding    vector(768) NOT NULL,
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX profile_embeddings_hnsw_idx ON profile_embeddings USING hnsw (embedding vector_cosine_ops);

CREATE TABLE project_embeddings (
    project_id   uuid PRIMARY KEY REFERENCES projects (id) ON DELETE CASCADE,
    model        text        NOT NULL,
    content_hash text        NOT NULL,
    embedding    vector(768) NOT NULL,
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX project_embeddings_hnsw_idx ON project_embeddings USING hnsw (embedding vector_cosine_ops);

CREATE TABLE post_embeddings (
    post_id      uuid PRIMARY KEY REFERENCES posts (id) ON DELETE CASCADE,
    model        text        NOT NULL,
    content_hash text        NOT NULL,
    embedding    vector(768) NOT NULL,
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX post_embeddings_hnsw_idx ON post_embeddings USING hnsw (embedding vector_cosine_ops);

CREATE TABLE venue_embeddings (
    venue_id     uuid PRIMARY KEY REFERENCES venues (id) ON DELETE CASCADE,
    model        text        NOT NULL,
    content_hash text        NOT NULL,
    embedding    vector(768) NOT NULL,
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX venue_embeddings_hnsw_idx ON venue_embeddings USING hnsw (embedding vector_cosine_ops);

-- ---------------------------------------------------------------------------
-- AI module: writing assistant log (AI-7, AI-8)
-- Records what was asked and whether the author accepted the suggestion (AI-8.6).
-- The suggestion text itself is not stored.
-- ---------------------------------------------------------------------------

CREATE TABLE assistant_requests (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid           NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    project_id uuid           NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    kind       assistant_kind NOT NULL,
    accepted   boolean,                                  -- NULL until the author decides
    created_at timestamptz    NOT NULL DEFAULT now()
);
CREATE INDEX assistant_requests_user_idx    ON assistant_requests (user_id, created_at);        -- daily limit (AI-8.5)
CREATE INDEX assistant_requests_project_idx ON assistant_requests (project_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- notification module (FR-9)
-- A notification is deleted with its receiver and with the user who caused it (FR-2.8).
-- ---------------------------------------------------------------------------

CREATE TABLE notifications (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid              NOT NULL REFERENCES users (id) ON DELETE CASCADE,   -- receiver
    type       notification_type NOT NULL,
    actor_id   uuid REFERENCES users (id) ON DELETE CASCADE,                         -- who caused it
    entity_id  uuid,                                     -- the item: connection, join request, post or comment
    read_at    timestamptz,
    created_at timestamptz       NOT NULL DEFAULT now()
);
CREATE INDEX notifications_user_idx   ON notifications (user_id, created_at DESC);
CREATE INDEX notifications_unread_idx ON notifications (user_id) WHERE read_at IS NULL;
CREATE INDEX notifications_actor_idx  ON notifications (actor_id);

-- ---------------------------------------------------------------------------
-- updated_at triggers
-- ---------------------------------------------------------------------------

CREATE TRIGGER users_set_updated_at         BEFORE UPDATE ON users         FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER profiles_set_updated_at      BEFORE UPDATE ON profiles      FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER publications_set_updated_at  BEFORE UPDATE ON publications  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER projects_set_updated_at      BEFORE UPDATE ON projects      FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER project_files_set_updated_at BEFORE UPDATE ON project_files FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER posts_set_updated_at         BEFORE UPDATE ON posts         FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER venues_set_updated_at        BEFORE UPDATE ON venues        FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
