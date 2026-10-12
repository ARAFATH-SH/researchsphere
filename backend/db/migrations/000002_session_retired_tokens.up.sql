-- Refresh tokens a session has replaced (FR-2.2). Each refresh retires the session's token;
-- a retired token sent again means it may have been stolen, and the session is ended.
-- Only the SHA-256 of a token is kept (NFR-5).

CREATE TABLE session_retired_tokens (
    token_hash text        PRIMARY KEY,
    session_id uuid        NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    retired_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX session_retired_tokens_session_idx ON session_retired_tokens (session_id);
