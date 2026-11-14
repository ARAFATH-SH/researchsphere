-- Sample data (slice 16): the ground truth for the synthetic sample profiles that
-- `make sample-data` (cmd/seedsample) creates in the development database, and the marker
-- that finds them again. docs/decisions.md, 2026-11-15 (sample data).
--
-- The application never reads this table. Only cmd/seedsample writes it, and the NFR-6
-- evaluation (slice 17) reads it: for a query profile in the test split, a candidate is
-- relevant when it has the same main_cluster. A shared second_cluster does not count.
--
-- A sample account has a row here and an address at @sample.researchsphere.local;
-- `make sample-purge` deletes only accounts that have both. Deleting the user deletes the row.
-- seq is the profile's number in the generator (sample-00042@...), so a run tops up the
-- missing numbers below n; seed is the generator seed that made it.
CREATE TABLE sample_profiles (
    user_id        uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    seq            integer     NOT NULL UNIQUE,
    seed           bigint      NOT NULL,
    main_cluster   text        NOT NULL,
    second_cluster text,
    split          text        NOT NULL,
    sparse         boolean     NOT NULL,
    bangla         text        NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sample_profiles_seq            CHECK (seq >= 0),
    CONSTRAINT sample_profiles_main_cluster   CHECK (main_cluster ~ '^[a-z][a-z0-9_]{1,40}$'),
    CONSTRAINT sample_profiles_second_cluster CHECK (second_cluster ~ '^[a-z][a-z0-9_]{1,40}$'
                                                     AND second_cluster <> main_cluster),
    -- tuning: free for building and tuning the matcher; test: held out for NFR-6.
    CONSTRAINT sample_profiles_split          CHECK (split IN ('tuning', 'test')),
    -- none: English only; mixed: Bangla headline or bio with English tags; bangla: Bangla text.
    CONSTRAINT sample_profiles_bangla         CHECK (bangla IN ('none', 'mixed', 'bangla'))
);
CREATE INDEX sample_profiles_cluster_idx ON sample_profiles (main_cluster, split);
