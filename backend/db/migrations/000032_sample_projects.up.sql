-- Sample projects (slice 19, chunk 19.1): the labels of the synthetic projects that
-- `make sample-data projects=1` (cmd/seedsample) creates in the development database, kept
-- for the later project matching evaluation (AI-3), like sample_profiles (000029).
--
-- The application never reads this table. Only cmd/seedsample writes it, and only
-- cmd/seedsample and cmd/recseval read it. A sample project belongs to a sample account, so
-- `make sample-purge` deletes it through the account deletion; deleting the project deletes
-- the row. seq is the project's number in the generator; project seq k is owned by sample
-- profile seq 10k, and its cluster is that owner's main_cluster; split is the owner's.
CREATE TABLE sample_projects (
    project_id uuid        PRIMARY KEY REFERENCES projects (id) ON DELETE CASCADE,
    seq        integer     NOT NULL UNIQUE,
    seed       bigint      NOT NULL,
    owner_seq  integer     NOT NULL,
    cluster    text        NOT NULL,
    split      text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sample_projects_seq       CHECK (seq >= 0),
    CONSTRAINT sample_projects_owner_seq CHECK (owner_seq >= 0),
    CONSTRAINT sample_projects_cluster   CHECK (cluster ~ '^[a-z][a-z0-9_]{1,40}$'),
    CONSTRAINT sample_projects_split     CHECK (split IN ('tuning', 'test'))
);
CREATE INDEX sample_projects_cluster_idx ON sample_projects (cluster, split);
