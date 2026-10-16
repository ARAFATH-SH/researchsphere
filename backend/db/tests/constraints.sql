-- Database rules from SRS section 7, tested against the migrated schema.
-- Run with `make test-db` (needs `make up` and `make migrate`). Everything runs inside one
-- transaction that is rolled back at the end, so the database is left unchanged.
-- Each check prints "ok: ..." and the first failure stops the run with "FAIL ...".

\set ON_ERROR_STOP 1
\set QUIET 1
-- Results go nowhere; the checks report through NOTICE messages.
\o /dev/null
SET client_min_messages = notice;

BEGIN;

-- expect_error runs one statement and passes only when it fails with the given SQLSTATE:
-- 23505 unique_violation, 23514 check_violation, 22000 data_exception.
CREATE FUNCTION pg_temp.expect_error(label text, stmt text, expected text) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    BEGIN
        EXECUTE stmt;
    EXCEPTION WHEN OTHERS THEN
        IF SQLSTATE = expected THEN
            RAISE NOTICE 'ok: % (refused)', label;
            RETURN;
        END IF;
        RAISE EXCEPTION 'FAIL %: expected SQLSTATE %, got % (%)', label, expected, SQLSTATE, SQLERRM;
    END;
    RAISE EXCEPTION 'FAIL %: statement succeeded, expected SQLSTATE %', label, expected;
END;
$$;

-- expect_ok runs one statement that must succeed.
CREATE FUNCTION pg_temp.expect_ok(label text, stmt text) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    EXECUTE stmt;
    RAISE NOTICE 'ok: %', label;
EXCEPTION WHEN OTHERS THEN
    RAISE EXCEPTION 'FAIL %: % (%)', label, SQLERRM, SQLSTATE;
END;
$$;

-- expect_true passes when the query returns true.
CREATE FUNCTION pg_temp.expect_true(label text, query text) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE
    result boolean;
BEGIN
    EXECUTE query INTO result;
    IF result IS NOT TRUE THEN
        RAISE EXCEPTION 'FAIL %: % returned %', label, query, result;
    END IF;
    RAISE NOTICE 'ok: %', label;
END;
$$;

-- An owner check that fires at commit is forced to fire now, inside the statement under test.
CREATE FUNCTION pg_temp.expect_no_owner(label text, stmt text) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    BEGIN
        EXECUTE stmt;
        SET CONSTRAINTS projects_have_owner, project_members_keep_owner IMMEDIATE;
    EXCEPTION WHEN check_violation THEN
        SET CONSTRAINTS ALL DEFERRED;
        RAISE NOTICE 'ok: % (refused)', label;
        RETURN;
    END;
    SET CONSTRAINTS ALL DEFERRED;
    RAISE EXCEPTION 'FAIL %: project left without an owner', label;
END;
$$;

-- ---------------------------------------------------------------------------
-- Fixtures: three users with profiles, one project owned by user 1.
-- ---------------------------------------------------------------------------

INSERT INTO users (id, email, password_hash) VALUES
    ('00000000-0000-0000-0000-000000000001', 'ada@example.org', 'hash'),
    ('00000000-0000-0000-0000-000000000002', 'bob@example.org', 'hash'),
    ('00000000-0000-0000-0000-000000000003', 'cy@example.org',  'hash');
INSERT INTO profiles (user_id, full_name) VALUES
    ('00000000-0000-0000-0000-000000000001', 'Ada'),
    ('00000000-0000-0000-0000-000000000002', 'Bob'),
    ('00000000-0000-0000-0000-000000000003', 'Cy');
INSERT INTO projects (id, title, description) VALUES
    ('00000000-0000-0000-0000-0000000000a1', 'Graph learning', 'A study');
INSERT INTO project_members (project_id, user_id, role) VALUES
    ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000001', 'owner');

-- ---------------------------------------------------------------------------
-- User and profile
-- ---------------------------------------------------------------------------

SELECT pg_temp.expect_error('User: email is unique ignoring letter case',
    $s$INSERT INTO users (email, password_hash) VALUES ('ADA@Example.org', 'hash')$s$, '23505');
SELECT pg_temp.expect_ok('User: an account can be suspended (FR-3.4, FR-15.3)',
    $s$UPDATE users SET suspended_at = now() WHERE id = '00000000-0000-0000-0000-000000000002'$s$);
SELECT pg_temp.expect_ok('User: a suspended account can be restored (FR-15.3)',
    $s$UPDATE users SET suspended_at = NULL WHERE id = '00000000-0000-0000-0000-000000000002'$s$);
SELECT pg_temp.expect_error('Profile: one per user',
    $s$INSERT INTO profiles (user_id, full_name) VALUES ('00000000-0000-0000-0000-000000000001', 'Ada 2')$s$, '23505');
SELECT pg_temp.expect_error('Profile: ORCID iD must be well formed',
    $s$UPDATE profiles SET orcid_id = '0000-0002-1825-009' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_ok('Profile: a well-formed ORCID iD is accepted',
    $s$UPDATE profiles SET orcid_id = '0000-0002-1825-0097' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$);
SELECT pg_temp.expect_error('Profile: ORCID iD is unique',
    $s$UPDATE profiles SET orcid_id = '0000-0002-1825-0097' WHERE user_id = '00000000-0000-0000-0000-000000000002'$s$, '23505');

-- Profile field limits (FR-4.1, migration 000003). Lengths count characters, so the
-- accepted values use a two-byte letter.
SELECT pg_temp.expect_error('Profile: full name must not be blank',
    $s$UPDATE profiles SET full_name = '   ' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_ok('Profile: full name of 200 characters is accepted',
    $s$UPDATE profiles SET full_name = repeat('é', 200) WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$);
SELECT pg_temp.expect_error('Profile: full name is at most 200 characters',
    $s$UPDATE profiles SET full_name = repeat('a', 201) WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_ok('Profile: headline of 220 characters is accepted',
    $s$UPDATE profiles SET headline = repeat('é', 220) WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$);
SELECT pg_temp.expect_error('Profile: headline is at most 220 characters',
    $s$UPDATE profiles SET headline = repeat('a', 221) WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_ok('Profile: bio of 2000 characters is accepted',
    $s$UPDATE profiles SET bio = repeat('é', 2000) WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$);
SELECT pg_temp.expect_error('Profile: bio is at most 2000 characters',
    $s$UPDATE profiles SET bio = repeat('a', 2001) WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_ok('Profile: institution, department and job title of 200 characters are accepted',
    $s$UPDATE profiles SET institution = repeat('é', 200), department = repeat('é', 200), job_title = repeat('é', 200)
       WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$);
SELECT pg_temp.expect_error('Profile: institution is at most 200 characters',
    $s$UPDATE profiles SET institution = repeat('a', 201) WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Profile: department is at most 200 characters',
    $s$UPDATE profiles SET department = repeat('a', 201) WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Profile: job title is at most 200 characters',
    $s$UPDATE profiles SET job_title = repeat('a', 201) WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Profile: headline is NULL, not blank',
    $s$UPDATE profiles SET headline = ' ' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Profile: bio is NULL, not blank',
    $s$UPDATE profiles SET bio = '' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Profile: institution is NULL, not blank',
    $s$UPDATE profiles SET institution = '' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Profile: department is NULL, not blank',
    $s$UPDATE profiles SET department = '' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Profile: job title is NULL, not blank',
    $s$UPDATE profiles SET job_title = '' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_ok('Profile: optional fields may be NULL',
    $s$UPDATE profiles SET headline = NULL, bio = NULL, institution = NULL, department = NULL, job_title = NULL, country = NULL
       WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$);
SELECT pg_temp.expect_ok('Profile: country as two capital letters is accepted',
    $s$UPDATE profiles SET country = 'DE' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$);
SELECT pg_temp.expect_error('Profile: country must be capitals',
    $s$UPDATE profiles SET country = 'de' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Profile: country must be two letters',
    $s$UPDATE profiles SET country = 'DEU' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');

-- ---------------------------------------------------------------------------
-- Interest, skill, education
-- ---------------------------------------------------------------------------

INSERT INTO research_interests (name) VALUES ('NLP');
INSERT INTO skills (name) VALUES ('PyTorch');
SELECT pg_temp.expect_error('Interest: name is unique ignoring letter case (FR-4.2)',
    $s$INSERT INTO research_interests (name) VALUES ('nlp')$s$, '23505');
SELECT pg_temp.expect_error('Skill: name is unique ignoring letter case (FR-4.2)',
    $s$INSERT INTO skills (name) VALUES ('pytorch')$s$, '23505');
SELECT pg_temp.expect_error('Education: end year is not before start year',
    $s$INSERT INTO education (user_id, institution, degree, start_year, end_year)
       VALUES ('00000000-0000-0000-0000-000000000001', 'MIT', 'BSc', 2020, 2019)$s$, '23514');

-- ---------------------------------------------------------------------------
-- Publication
-- ---------------------------------------------------------------------------

INSERT INTO publications (id, doi, title) VALUES
    ('00000000-0000-0000-0000-0000000000c1', '10.1000/xyz123', 'A paper');
SELECT pg_temp.expect_error('Publication: DOI is unique ignoring letter case (FR-5.4)',
    $s$INSERT INTO publications (doi, title) VALUES ('10.1000/XYZ123', 'Same paper')$s$, '23505');
SELECT pg_temp.expect_error('Publication: DOI is stored in bare form, not as a link (FR-5.4)',
    $s$INSERT INTO publications (doi, title) VALUES ('https://doi.org/10.1000/xyz123', 'Same paper')$s$, '23514');
SELECT pg_temp.expect_ok('Publication: a manual entry needs no DOI (FR-5.1)',
    $s$INSERT INTO publications (title) VALUES ('Workshop talk')$s$);

-- ---------------------------------------------------------------------------
-- Connection (FR-7.2, FR-7.6)
-- ---------------------------------------------------------------------------

SELECT pg_temp.expect_error('Connection: no request to oneself',
    $s$INSERT INTO connections (requester_id, receiver_id)
       VALUES ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001')$s$, '23514');
INSERT INTO connections (id, requester_id, receiver_id) VALUES
    ('00000000-0000-0000-0000-0000000000d1', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002');
SELECT pg_temp.expect_error('Connection: no second request while one is pending',
    $s$INSERT INTO connections (requester_id, receiver_id)
       VALUES ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002')$s$, '23505');
SELECT pg_temp.expect_error('Connection: no request in the other direction while one is pending',
    $s$INSERT INTO connections (requester_id, receiver_id)
       VALUES ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001')$s$, '23505');
UPDATE connections SET status = 'accepted' WHERE id = '00000000-0000-0000-0000-0000000000d1';
SELECT pg_temp.expect_error('Connection: no request in the other direction once connected',
    $s$INSERT INTO connections (requester_id, receiver_id)
       VALUES ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001')$s$, '23505');
INSERT INTO connections (id, requester_id, receiver_id, status) VALUES
    ('00000000-0000-0000-0000-0000000000d2', '00000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000002', 'rejected');
SELECT pg_temp.expect_ok('Connection: a new request is allowed after a rejection (FR-7.6)',
    $s$INSERT INTO connections (requester_id, receiver_id)
       VALUES ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000003')$s$);

-- ---------------------------------------------------------------------------
-- Post, like, comment (FR-8, FR-15.2)
-- ---------------------------------------------------------------------------

INSERT INTO posts (id, author_id, body) VALUES
    ('00000000-0000-0000-0000-0000000000e1', '00000000-0000-0000-0000-000000000001', 'Hello');
INSERT INTO post_likes (post_id, user_id) VALUES
    ('00000000-0000-0000-0000-0000000000e1', '00000000-0000-0000-0000-000000000002');
SELECT pg_temp.expect_error('Like: one like per user per post (FR-8.3)',
    $s$INSERT INTO post_likes (post_id, user_id)
       VALUES ('00000000-0000-0000-0000-0000000000e1', '00000000-0000-0000-0000-000000000002')$s$, '23505');
SELECT pg_temp.expect_ok('Post: 5,000 characters are accepted (FR-8.1)',
    $s$INSERT INTO posts (author_id, body) VALUES ('00000000-0000-0000-0000-000000000001', repeat('a', 5000))$s$);
SELECT pg_temp.expect_error('Post: more than 5,000 characters is refused (FR-8.1)',
    $s$INSERT INTO posts (author_id, body) VALUES ('00000000-0000-0000-0000-000000000001', repeat('a', 5001))$s$, '23514');
SELECT pg_temp.expect_error('Post: an empty post is refused',
    $s$INSERT INTO posts (author_id, body) VALUES ('00000000-0000-0000-0000-000000000001', '   ')$s$, '23514');
SELECT pg_temp.expect_error('Post: linked to a publication or a project, not both (FR-8.1)',
    $s$INSERT INTO posts (author_id, body, publication_id, project_id)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Both',
               '00000000-0000-0000-0000-0000000000c1', '00000000-0000-0000-0000-0000000000a1')$s$, '23514');
SELECT pg_temp.expect_error('Post: a removal needs a reason (FR-15.2)',
    $s$UPDATE posts SET removed_at = now() WHERE id = '00000000-0000-0000-0000-0000000000e1'$s$, '23514');
SELECT pg_temp.expect_ok('Post: a removal with a reason is accepted (FR-15.2)',
    $s$UPDATE posts SET removed_at = now(), removal_reason = 'spam' WHERE id = '00000000-0000-0000-0000-0000000000e1'$s$);
SELECT pg_temp.expect_error('Comment: more than 2,000 characters is refused (FR-8.4)',
    $s$INSERT INTO post_comments (post_id, author_id, body)
       VALUES ('00000000-0000-0000-0000-0000000000e1', '00000000-0000-0000-0000-000000000002', repeat('a', 2001))$s$, '23514');
SELECT pg_temp.expect_error('Comment: a removal needs a reason (FR-15.2)',
    $s$INSERT INTO post_comments (post_id, author_id, body, removed_at)
       VALUES ('00000000-0000-0000-0000-0000000000e1', '00000000-0000-0000-0000-000000000002', 'Hi', now())$s$, '23514');

-- ---------------------------------------------------------------------------
-- Membership: one role per user, exactly one owner per project (FR-10.5, FR-11)
-- ---------------------------------------------------------------------------

SELECT pg_temp.expect_error('Membership: one role per user per project',
    $s$INSERT INTO project_members (project_id, user_id, role)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000001', 'editor')$s$, '23505');
SELECT pg_temp.expect_error('Membership: at most one owner per project',
    $s$INSERT INTO project_members (project_id, user_id, role)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000002', 'owner')$s$, '23505');
SELECT pg_temp.expect_no_owner('Membership: a project cannot be created without an owner',
    $s$INSERT INTO projects (title, description) VALUES ('Orphan', 'No owner')$s$);
SELECT pg_temp.expect_no_owner('Membership: the owner cannot be demoted without a new owner',
    $s$UPDATE project_members SET role = 'editor'
       WHERE project_id = '00000000-0000-0000-0000-0000000000a1' AND user_id = '00000000-0000-0000-0000-000000000001'$s$);
SELECT pg_temp.expect_no_owner('Membership: the owner cannot be removed',
    $s$DELETE FROM project_members
       WHERE project_id = '00000000-0000-0000-0000-0000000000a1' AND user_id = '00000000-0000-0000-0000-000000000001'$s$);
SELECT pg_temp.expect_no_owner('Membership: an owner of a project cannot be deleted with it still there (FR-2.9)',
    $s$DELETE FROM users WHERE id = '00000000-0000-0000-0000-000000000001'$s$);

INSERT INTO project_members (project_id, user_id, role) VALUES
    ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000002', 'editor');
UPDATE project_members SET role = 'editor'
    WHERE project_id = '00000000-0000-0000-0000-0000000000a1' AND user_id = '00000000-0000-0000-0000-000000000001';
UPDATE project_members SET role = 'owner'
    WHERE project_id = '00000000-0000-0000-0000-0000000000a1' AND user_id = '00000000-0000-0000-0000-000000000002';
SELECT pg_temp.expect_ok('Membership: ownership can be transferred in one transaction (FR-10.5)',
    $s$SET CONSTRAINTS projects_have_owner, project_members_keep_owner IMMEDIATE$s$);
SET CONSTRAINTS ALL DEFERRED;

-- ---------------------------------------------------------------------------
-- Join request (FR-11.1)
-- ---------------------------------------------------------------------------

INSERT INTO project_join_requests (id, project_id, user_id, kind) VALUES
    ('00000000-0000-0000-0000-0000000000f1', '00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000003', 'request');
SELECT pg_temp.expect_error('Join request: one pending request per user per project',
    $s$INSERT INTO project_join_requests (project_id, user_id, kind)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000003', 'request')$s$, '23505');
SELECT pg_temp.expect_error('Join request: no invitation while a request is pending',
    $s$INSERT INTO project_join_requests (project_id, user_id, kind)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000003', 'invitation')$s$, '23505');
UPDATE project_join_requests SET status = 'rejected', decided_at = now() WHERE id = '00000000-0000-0000-0000-0000000000f1';
SELECT pg_temp.expect_ok('Join request: a new request is allowed after a rejection',
    $s$INSERT INTO project_join_requests (project_id, user_id, kind)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000003', 'request')$s$);

-- ---------------------------------------------------------------------------
-- File (FR-12.1, FR-12.2): path unique within the project and cannot point outside it
-- ---------------------------------------------------------------------------

SELECT pg_temp.expect_ok('File: a text file at the project root',
    $s$INSERT INTO project_files (project_id, path, content) VALUES ('00000000-0000-0000-0000-0000000000a1', 'main.tex', '')$s$);
SELECT pg_temp.expect_ok('File: an empty folder (FR-12.1)',
    $s$INSERT INTO project_files (project_id, path, kind) VALUES ('00000000-0000-0000-0000-0000000000a1', 'figures', 'folder')$s$);
SELECT pg_temp.expect_ok('File: a PNG figure inside a folder (FR-12.2)',
    $s$INSERT INTO project_files (project_id, path, kind, storage_key, size_bytes)
       VALUES ('00000000-0000-0000-0000-0000000000a1', 'figures/plot.png', 'binary', 'key-1', 5242880)$s$);
SELECT pg_temp.expect_error('File: path is unique within a project',
    $s$INSERT INTO project_files (project_id, path) VALUES ('00000000-0000-0000-0000-0000000000a1', 'main.tex')$s$, '23505');
SELECT pg_temp.expect_error('File: path with a .. segment is refused',
    $s$INSERT INTO project_files (project_id, path) VALUES ('00000000-0000-0000-0000-0000000000a1', '../etc/passwd')$s$, '23514');
SELECT pg_temp.expect_error('File: path with a .. segment in the middle is refused',
    $s$INSERT INTO project_files (project_id, path) VALUES ('00000000-0000-0000-0000-0000000000a1', 'a/../../b.tex')$s$, '23514');
SELECT pg_temp.expect_error('File: absolute path is refused',
    $s$INSERT INTO project_files (project_id, path) VALUES ('00000000-0000-0000-0000-0000000000a1', '/main.tex')$s$, '23514');
SELECT pg_temp.expect_error('File: empty path segment is refused',
    $s$INSERT INTO project_files (project_id, path) VALUES ('00000000-0000-0000-0000-0000000000a1', 'a//b.tex')$s$, '23514');
SELECT pg_temp.expect_error('File: backslash is refused',
    $s$INSERT INTO project_files (project_id, path) VALUES ('00000000-0000-0000-0000-0000000000a1', 'a\b.tex')$s$, '23514');
SELECT pg_temp.expect_error('File: figure over 5 MB is refused (FR-12.2)',
    $s$INSERT INTO project_files (project_id, path, kind, storage_key, size_bytes)
       VALUES ('00000000-0000-0000-0000-0000000000a1', 'big.png', 'binary', 'key-2', 5242881)$s$, '23514');
SELECT pg_temp.expect_error('File: figure must be PNG, JPG or PDF (FR-12.2)',
    $s$INSERT INTO project_files (project_id, path, kind, storage_key, size_bytes)
       VALUES ('00000000-0000-0000-0000-0000000000a1', 'anim.gif', 'binary', 'key-3', 10)$s$, '23514');
SELECT pg_temp.expect_error('File: a binary file needs a storage key',
    $s$INSERT INTO project_files (project_id, path, kind) VALUES ('00000000-0000-0000-0000-0000000000a1', 'fig.pdf', 'binary')$s$, '23514');
SELECT pg_temp.expect_error('File: a folder has no content',
    $s$INSERT INTO project_files (project_id, path, kind, content) VALUES ('00000000-0000-0000-0000-0000000000a1', 'sections', 'folder', 'x')$s$, '23514');

-- ---------------------------------------------------------------------------
-- Compile job (FR-13.4)
-- ---------------------------------------------------------------------------

INSERT INTO compile_jobs (id, project_id, status) VALUES
    ('00000000-0000-0000-0000-000000000101', '00000000-0000-0000-0000-0000000000a1', 'running');
SELECT pg_temp.expect_error('Compile job: one compile per project at a time',
    $s$INSERT INTO compile_jobs (project_id) VALUES ('00000000-0000-0000-0000-0000000000a1')$s$, '23505');
UPDATE compile_jobs SET status = 'succeeded' WHERE id = '00000000-0000-0000-0000-000000000101';
SELECT pg_temp.expect_ok('Compile job: a new compile once the last one finished',
    $s$INSERT INTO compile_jobs (project_id) VALUES ('00000000-0000-0000-0000-0000000000a1')$s$);

-- ---------------------------------------------------------------------------
-- Venue (FR-14.5)
-- ---------------------------------------------------------------------------

INSERT INTO venues (id, name, type, scope, source, source_ref) VALUES
    ('00000000-0000-0000-0000-000000000201', 'ACL', 'conference', 'NLP', 'listing', '123');
SELECT pg_temp.expect_error('Venue: an imported venue is unique by source and source id',
    $s$INSERT INTO venues (name, type, scope, source, source_ref) VALUES ('ACL again', 'conference', 'NLP', 'listing', '123')$s$, '23505');
SELECT pg_temp.expect_error('Venue: an imported venue needs its source id',
    $s$INSERT INTO venues (name, type, scope, source) VALUES ('EMNLP', 'conference', 'NLP', 'listing')$s$, '23514');
SELECT pg_temp.expect_ok('Venue: venues added by an administrator need no source id (FR-15.1)',
    $s$INSERT INTO venues (name, type, scope) VALUES ('Local workshop', 'workshop', 'Graphs'),
                                                     ('Another workshop', 'workshop', 'Graphs')$s$);

-- ---------------------------------------------------------------------------
-- Embedding: one per owner, deleted with its owner (AI-1)
-- ---------------------------------------------------------------------------

INSERT INTO project_embeddings (project_id, model, content_hash, embedding) VALUES
    ('00000000-0000-0000-0000-0000000000a1', 'test-model', 'h1', array_fill(0.1::real, ARRAY[768])::vector);
SELECT pg_temp.expect_error('Embedding: one per owner',
    $s$INSERT INTO project_embeddings (project_id, model, content_hash, embedding)
       VALUES ('00000000-0000-0000-0000-0000000000a1', 'test-model', 'h2', array_fill(0.2::real, ARRAY[768])::vector)$s$, '23505');
SELECT pg_temp.expect_error('Embedding: vector must have 768 dimensions',
    $s$INSERT INTO venue_embeddings (venue_id, model, content_hash, embedding)
       VALUES ('00000000-0000-0000-0000-000000000201', 'test-model', 'h1', '[1,2,3]')$s$, '22000');

-- ---------------------------------------------------------------------------
-- Account deletion (FR-2.8, SRS 7 Retention): user 3 owns no project.
-- ---------------------------------------------------------------------------

INSERT INTO sessions (id, user_id, refresh_token_hash, expires_at) VALUES
    ('00000000-0000-0000-0000-0000000000f3', '00000000-0000-0000-0000-000000000003', 'rt-3', now() + interval '30 days');
INSERT INTO session_retired_tokens (token_hash, session_id) VALUES
    ('rt-3-old', '00000000-0000-0000-0000-0000000000f3');
SELECT pg_temp.expect_error('Session: a retired refresh token hash is unique (FR-2.2)',
    $s$INSERT INTO session_retired_tokens (token_hash, session_id)
       VALUES ('rt-3-old', '00000000-0000-0000-0000-0000000000f3')$s$, '23505');
INSERT INTO auth_tokens (user_id, purpose, token_hash, expires_at) VALUES
    ('00000000-0000-0000-0000-000000000003', 'password_reset', 'at-3', now() + interval '1 hour');
INSERT INTO user_interests (user_id, interest_id) SELECT '00000000-0000-0000-0000-000000000003', id FROM research_interests;
INSERT INTO user_skills (user_id, skill_id) SELECT '00000000-0000-0000-0000-000000000003', id FROM skills;
INSERT INTO education (user_id, institution, degree) VALUES ('00000000-0000-0000-0000-000000000003', 'MIT', 'BSc');
INSERT INTO user_publications (user_id, publication_id, confirmed_at) VALUES
    ('00000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-0000000000c1', now()),
    ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-0000000000c1', now());
INSERT INTO profile_embeddings (user_id, model, content_hash, embedding) VALUES
    ('00000000-0000-0000-0000-000000000003', 'test-model', 'h3', array_fill(0.3::real, ARRAY[768])::vector);
INSERT INTO posts (id, author_id, body) VALUES
    ('00000000-0000-0000-0000-0000000000e3', '00000000-0000-0000-0000-000000000003', 'Cy post');
INSERT INTO post_embeddings (post_id, model, content_hash, embedding) VALUES
    ('00000000-0000-0000-0000-0000000000e3', 'test-model', 'h4', array_fill(0.4::real, ARRAY[768])::vector);
INSERT INTO post_likes (post_id, user_id) VALUES
    ('00000000-0000-0000-0000-0000000000e1', '00000000-0000-0000-0000-000000000003');
INSERT INTO post_comments (post_id, author_id, body) VALUES
    ('00000000-0000-0000-0000-0000000000e1', '00000000-0000-0000-0000-000000000003', 'Cy comment'),
    ('00000000-0000-0000-0000-0000000000e3', '00000000-0000-0000-0000-000000000002', 'Bob on Cy post');
INSERT INTO notifications (user_id, type, actor_id) VALUES
    ('00000000-0000-0000-0000-000000000003', 'post_comment',       '00000000-0000-0000-0000-000000000002'),
    ('00000000-0000-0000-0000-000000000002', 'connection_request', '00000000-0000-0000-0000-000000000003');

DELETE FROM users WHERE id = '00000000-0000-0000-0000-000000000003';

SELECT pg_temp.expect_true('Deletion: profile, interests, skills, education removed (FR-2.8)', $q$
    SELECT NOT EXISTS (SELECT 1 FROM profiles       WHERE user_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM user_interests WHERE user_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM user_skills    WHERE user_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM education      WHERE user_id = '00000000-0000-0000-0000-000000000003')$q$);
SELECT pg_temp.expect_true('Deletion: sessions, retired refresh tokens and one-time tokens removed', $q$
    SELECT NOT EXISTS (SELECT 1 FROM sessions    WHERE user_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM auth_tokens WHERE user_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM session_retired_tokens WHERE session_id = '00000000-0000-0000-0000-0000000000f3')$q$);
SELECT pg_temp.expect_true('Deletion: posts, comments on them, likes and own comments removed (FR-2.8)', $q$
    SELECT NOT EXISTS (SELECT 1 FROM posts         WHERE author_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM post_comments WHERE post_id   = '00000000-0000-0000-0000-0000000000e3')
       AND NOT EXISTS (SELECT 1 FROM post_comments WHERE author_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM post_likes    WHERE user_id   = '00000000-0000-0000-0000-000000000003')$q$);
SELECT pg_temp.expect_true('Deletion: connections removed (FR-2.8)', $q$
    SELECT NOT EXISTS (SELECT 1 FROM connections
                       WHERE '00000000-0000-0000-0000-000000000003' IN (requester_id, receiver_id))$q$);
SELECT pg_temp.expect_true('Deletion: profile and post embeddings removed (FR-2.8)', $q$
    SELECT NOT EXISTS (SELECT 1 FROM profile_embeddings WHERE user_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM post_embeddings    WHERE post_id = '00000000-0000-0000-0000-0000000000e3')$q$);
SELECT pg_temp.expect_true('Deletion: notifications to and from the user removed', $q$
    SELECT NOT EXISTS (SELECT 1 FROM notifications
                       WHERE '00000000-0000-0000-0000-000000000003' IN (user_id, actor_id))$q$);
SELECT pg_temp.expect_true('Deletion: a publication shared with a co-author stays for the co-author', $q$
    SELECT EXISTS (SELECT 1 FROM user_publications
                   WHERE publication_id = '00000000-0000-0000-0000-0000000000c1'
                     AND user_id = '00000000-0000-0000-0000-000000000001')$q$);

-- ---------------------------------------------------------------------------
-- Project deletion (FR-10.6): files, members, requests, compile jobs and embedding go with it.
-- ---------------------------------------------------------------------------

INSERT INTO posts (id, author_id, body, project_id) VALUES
    ('00000000-0000-0000-0000-0000000000e4', '00000000-0000-0000-0000-000000000002', 'About the project',
     '00000000-0000-0000-0000-0000000000a1');
DELETE FROM projects WHERE id = '00000000-0000-0000-0000-0000000000a1';
SELECT pg_temp.expect_ok('Project deletion: passes the owner check at commit',
    $s$SET CONSTRAINTS projects_have_owner, project_members_keep_owner IMMEDIATE$s$);
SET CONSTRAINTS ALL DEFERRED;
SELECT pg_temp.expect_true('Project deletion: files, members, requests, compile jobs and embedding removed', $q$
    SELECT NOT EXISTS (SELECT 1 FROM project_files         WHERE project_id = '00000000-0000-0000-0000-0000000000a1')
       AND NOT EXISTS (SELECT 1 FROM project_members       WHERE project_id = '00000000-0000-0000-0000-0000000000a1')
       AND NOT EXISTS (SELECT 1 FROM project_join_requests WHERE project_id = '00000000-0000-0000-0000-0000000000a1')
       AND NOT EXISTS (SELECT 1 FROM compile_jobs          WHERE project_id = '00000000-0000-0000-0000-0000000000a1')
       AND NOT EXISTS (SELECT 1 FROM project_embeddings    WHERE project_id = '00000000-0000-0000-0000-0000000000a1')$q$);
SELECT pg_temp.expect_true('Project deletion: a post that linked to it stays, without the link', $q$
    SELECT project_id IS NULL FROM posts WHERE id = '00000000-0000-0000-0000-0000000000e4'$q$);

ROLLBACK;

\echo 'All database constraint tests passed.'
