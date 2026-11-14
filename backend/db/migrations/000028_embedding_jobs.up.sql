-- The embedding pipeline (AI-1.1, AI-1.4 to AI-1.6; slice 15, chunk 15.2): a queue of owners
-- whose embedding must be (re)made, the triggers that fill it in the same transaction as every
-- change to a profile's source text, and the administrators' record of rebuilds.

-- Each embedding records how many chunks of at most 512 tokens were averaged, and when it was
-- first made (AI-1.6). The hash is the hex SHA-256 of the source text, the model
-- "name@revision". All four tables now, so a later slice only adds its source type.
ALTER TABLE profile_embeddings
    ADD COLUMN chunk_count smallint    NOT NULL DEFAULT 1,
    ADD COLUMN created_at  timestamptz NOT NULL DEFAULT now(),
    ADD CONSTRAINT profile_embeddings_chunk_count  CHECK (chunk_count BETWEEN 1 AND 8),
    ADD CONSTRAINT profile_embeddings_content_hash CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT profile_embeddings_model        CHECK (model ~ '^\S+@\S+$');
ALTER TABLE project_embeddings
    ADD COLUMN chunk_count smallint    NOT NULL DEFAULT 1,
    ADD COLUMN created_at  timestamptz NOT NULL DEFAULT now(),
    ADD CONSTRAINT project_embeddings_chunk_count  CHECK (chunk_count BETWEEN 1 AND 8),
    ADD CONSTRAINT project_embeddings_content_hash CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT project_embeddings_model        CHECK (model ~ '^\S+@\S+$');
ALTER TABLE post_embeddings
    ADD COLUMN chunk_count smallint    NOT NULL DEFAULT 1,
    ADD COLUMN created_at  timestamptz NOT NULL DEFAULT now(),
    ADD CONSTRAINT post_embeddings_chunk_count  CHECK (chunk_count BETWEEN 1 AND 8),
    ADD CONSTRAINT post_embeddings_content_hash CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT post_embeddings_model        CHECK (model ~ '^\S+@\S+$');
ALTER TABLE venue_embeddings
    ADD COLUMN chunk_count smallint    NOT NULL DEFAULT 1,
    ADD COLUMN created_at  timestamptz NOT NULL DEFAULT now(),
    ADD CONSTRAINT venue_embeddings_chunk_count  CHECK (chunk_count BETWEEN 1 AND 8),
    ADD CONSTRAINT venue_embeddings_content_hash CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT venue_embeddings_model        CHECK (model ~ '^\S+@\S+$');

-- Finding the embeddings of an old model after a model change (AI-1.6).
CREATE INDEX profile_embeddings_model_idx ON profile_embeddings (model);

-- One row per owner whose embedding must be made, changed or removed (AI-1.5). A change
-- upserts the row (embedding_enqueue) and raises generation; the worker leases rows with
-- leased_until, so two workers never hold one owner, and deletes a row only when its
-- generation is still the one it read, so a change made while it worked is not lost.
-- No foreign key: the owner is one of several types. The worker drops a job whose owner
-- is gone. Priority 0 is a change by a user, 1 a re-queue after a model change or a rebuild.
-- force makes the worker embed again even when the text and model have not changed.
-- A text error counts in attempts; at 5, failed_at and last_error are set and the row stays
-- until the owner's next change.
CREATE TABLE embedding_jobs (
    owner_type   text        NOT NULL,
    owner_id     uuid        NOT NULL,
    priority     smallint    NOT NULL DEFAULT 0,
    force        boolean     NOT NULL DEFAULT false,
    generation   bigint      NOT NULL DEFAULT 1,
    requested_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    attempts     integer     NOT NULL DEFAULT 0,
    next_try_at  timestamptz NOT NULL DEFAULT clock_timestamp(),
    leased_until timestamptz,
    last_error   text,
    failed_at    timestamptz,
    PRIMARY KEY (owner_type, owner_id),
    -- Each later slice widens this list: project (AI-1.2), post and venue (AI-1.3).
    CONSTRAINT embedding_jobs_owner_type CHECK (owner_type IN ('profile')),
    CONSTRAINT embedding_jobs_priority   CHECK (priority IN (0, 1)),
    CONSTRAINT embedding_jobs_attempts   CHECK (attempts >= 0),
    CONSTRAINT embedding_jobs_generation CHECK (generation >= 1),
    CONSTRAINT embedding_jobs_last_error CHECK (char_length(last_error) <= 500),
    CONSTRAINT embedding_jobs_failed     CHECK (failed_at IS NULL OR last_error IS NOT NULL)
);
CREATE INDEX embedding_jobs_due_idx ON embedding_jobs (priority, next_try_at) WHERE failed_at IS NULL;

-- Queue the owner's embedding. A new request resets a failed or waiting job, and a change
-- by a user (priority 0) moves a re-queued one ahead.
CREATE FUNCTION embedding_enqueue(p_owner_type text, p_owner_id uuid, p_priority smallint DEFAULT 0,
                                  p_force boolean DEFAULT false) RETURNS void
LANGUAGE sql AS $$
    INSERT INTO embedding_jobs (owner_type, owner_id, priority, force)
    VALUES (p_owner_type, p_owner_id, p_priority, p_force)
    ON CONFLICT (owner_type, owner_id) DO UPDATE
    SET priority     = least(embedding_jobs.priority, EXCLUDED.priority),
        force        = embedding_jobs.force OR EXCLUDED.force,
        generation   = embedding_jobs.generation + 1,
        requested_at = clock_timestamp(),
        attempts     = 0,
        next_try_at  = clock_timestamp(),
        last_error   = NULL,
        failed_at    = NULL;
$$;

-- Triggers (AI-1.5): every change to a profile's source text queues the profile in the same
-- transaction, whatever code made it. Each only calls embedding_enqueue, or deletes the
-- embedding on suspension. The worker decides the rest (eligibility, empty text, skip).

-- A row with a user_id: profiles (headline, bio), user_interests, user_skills and
-- user_publications (added, confirmed, removed).
CREATE FUNCTION embedding_enqueue_user_row() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM embedding_enqueue('profile', OLD.user_id);
    ELSE
        PERFORM embedding_enqueue('profile', NEW.user_id);
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER profiles_embedding AFTER INSERT OR UPDATE OF headline, bio ON profiles
    FOR EACH ROW EXECUTE FUNCTION embedding_enqueue_user_row();
CREATE TRIGGER user_interests_embedding AFTER INSERT OR DELETE ON user_interests
    FOR EACH ROW EXECUTE FUNCTION embedding_enqueue_user_row();
CREATE TRIGGER user_skills_embedding AFTER INSERT OR DELETE ON user_skills
    FOR EACH ROW EXECUTE FUNCTION embedding_enqueue_user_row();
CREATE TRIGGER user_publications_embedding AFTER INSERT OR DELETE OR UPDATE OF confirmed_at ON user_publications
    FOR EACH ROW EXECUTE FUNCTION embedding_enqueue_user_row();

-- A publication's title or abstract changed: queue every user who has confirmed it, so a
-- co-author's edit of a shared record reaches all of them.
CREATE FUNCTION embedding_enqueue_publication_authors() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM embedding_enqueue('profile', up.user_id)
    FROM user_publications up
    WHERE up.publication_id = NEW.id AND up.confirmed_at IS NOT NULL;
    RETURN NULL;
END;
$$;

CREATE TRIGGER publications_embedding AFTER UPDATE OF title, abstract ON publications
    FOR EACH ROW EXECUTE FUNCTION embedding_enqueue_publication_authors();

-- Only verified, not suspended users have an embedding. Verification and the end of a
-- suspension queue one; a suspension deletes it at once.
CREATE FUNCTION embedding_enqueue_user() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM embedding_enqueue('profile', NEW.id);
    RETURN NULL;
END;
$$;

CREATE FUNCTION embedding_delete_suspended() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM profile_embeddings WHERE user_id = NEW.id;
    RETURN NULL;
END;
$$;

CREATE TRIGGER users_embedding_eligible AFTER UPDATE OF email_verified_at, suspended_at ON users
    FOR EACH ROW
    WHEN (NEW.email_verified_at IS NOT NULL AND NEW.suspended_at IS NULL
          AND (OLD.email_verified_at IS NULL OR OLD.suspended_at IS NOT NULL))
    EXECUTE FUNCTION embedding_enqueue_user();
CREATE TRIGGER users_embedding_suspended AFTER UPDATE OF suspended_at ON users
    FOR EACH ROW
    WHEN (NEW.suspended_at IS NOT NULL)
    EXECUTE FUNCTION embedding_delete_suspended();

-- What administrators did to the AI pipeline (the rebuild), kept like moderation_actions:
-- rows are added, never changed.
CREATE TABLE ai_admin_actions (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_id   uuid        REFERENCES users (id) ON DELETE SET NULL,
    action     text        NOT NULL,
    queued     integer     NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ai_admin_actions_action CHECK (action IN ('embeddings_rebuild')),
    CONSTRAINT ai_admin_actions_queued CHECK (queued >= 0)
);
CREATE INDEX ai_admin_actions_admin_idx ON ai_admin_actions (admin_id);
