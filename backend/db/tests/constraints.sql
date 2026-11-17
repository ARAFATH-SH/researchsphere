-- Database rules from SRS section 7, tested against the migrated schema.
-- Run with `make test-db` (needs `make up` and `make migrate`). Everything runs inside one
-- transaction that is rolled back at the end, so the database is left unchanged, and unique
-- values carry a per-run prefix (pg_temp.t below), so existing rows cannot get in the way.
-- Each check prints "ok: ..." and the first failure stops the run with "FAIL ...".

\set ON_ERROR_STOP 1
\set QUIET 1
-- Results go nowhere; the checks report through NOTICE messages.
\o /dev/null
SET client_min_messages = notice;

BEGIN;

-- expect_error runs one statement and passes only when it fails with the given SQLSTATE:
-- 23502 not_null_violation, 23503 foreign_key_violation, 23505 unique_violation, 23514 check_violation, 22000 data_exception.
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

-- Every value that must be unique across the table (emails, tag names, object keys, DOIs,
-- token hashes, source ids) starts with a prefix that is new on each run, so rows already in
-- the database cannot clash with the fixtures or change a check's result. The fixed uuids
-- below are safe as they are: their version digit is 0, and gen_random_uuid() makes only
-- version 4.
SELECT set_config('test.prefix', 'zz-test-' || substr(md5(random()::text), 1, 8), true);

-- t adds the run's prefix to a value.
CREATE FUNCTION pg_temp.t(value text) RETURNS text
LANGUAGE sql AS $$ SELECT current_setting('test.prefix') || '-' || value $$;

-- random_orcid returns a random ORCID iD with a correct ISO 7064 11,2 check digit.
CREATE FUNCTION pg_temp.random_orcid() RETURNS text
LANGUAGE plpgsql AS $$
DECLARE
    digits text := '';
    total  integer := 0;
    d      integer;
    check_value integer;
BEGIN
    FOR i IN 1..15 LOOP
        d := floor(random() * 10)::integer;
        digits := digits || d;
        total := (total + d) * 2;
    END LOOP;
    check_value := (12 - total % 11) % 11;
    digits := digits || CASE WHEN check_value = 10 THEN 'X' ELSE check_value::text END;
    RETURN substr(digits, 1, 4) || '-' || substr(digits, 5, 4) || '-' || substr(digits, 9, 4) || '-' || substr(digits, 13, 4);
END;
$$;
SELECT set_config('test.orcid', pg_temp.random_orcid(), true);

-- ---------------------------------------------------------------------------
-- Fixtures: three users with profiles, one project owned by user 1.
-- ---------------------------------------------------------------------------

INSERT INTO users (id, email, password_hash) VALUES
    ('00000000-0000-0000-0000-000000000001', pg_temp.t('ada@example.org'), 'hash'),
    ('00000000-0000-0000-0000-000000000002', pg_temp.t('bob@example.org'), 'hash'),
    ('00000000-0000-0000-0000-000000000003', pg_temp.t('cy@example.org'),  'hash');
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
    $s$INSERT INTO users (email, password_hash) VALUES (upper(pg_temp.t('ada@example.org')), 'hash')$s$, '23505');
SELECT pg_temp.expect_ok('User: an account can be suspended (FR-3.4, FR-15.3)',
    $s$UPDATE users SET suspended_at = now() WHERE id = '00000000-0000-0000-0000-000000000002'$s$);
SELECT pg_temp.expect_ok('User: a suspended account can be restored (FR-15.3)',
    $s$UPDATE users SET suspended_at = NULL WHERE id = '00000000-0000-0000-0000-000000000002'$s$);
SELECT pg_temp.expect_error('Profile: one per user',
    $s$INSERT INTO profiles (user_id, full_name) VALUES ('00000000-0000-0000-0000-000000000001', 'Ada 2')$s$, '23505');
SELECT pg_temp.expect_error('Profile: ORCID iD must be well formed',
    $s$UPDATE profiles SET orcid_id = left(current_setting('test.orcid'), 18) WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_ok('Profile: a well-formed ORCID iD is accepted',
    $s$UPDATE profiles SET orcid_id = current_setting('test.orcid') WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$);
SELECT pg_temp.expect_error('Profile: ORCID iD is unique',
    $s$UPDATE profiles SET orcid_id = current_setting('test.orcid') WHERE user_id = '00000000-0000-0000-0000-000000000002'$s$, '23505');

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

-- Profile city (FR-4.1, migration 000011): optional, at most 100 characters, one line,
-- never blank.
SELECT pg_temp.expect_ok('Profile: city may be NULL',
    $s$UPDATE profiles SET city = NULL WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$);
SELECT pg_temp.expect_ok('Profile: city of 100 characters is accepted',
    $s$UPDATE profiles SET city = repeat('é', 100) WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$);
SELECT pg_temp.expect_error('Profile: city is at most 100 characters',
    $s$UPDATE profiles SET city = repeat('a', 101) WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Profile: city is NULL, not blank',
    $s$UPDATE profiles SET city = ' ' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Profile: city has no line break',
    $s$UPDATE profiles SET city = E'Dhaka\nBangladesh' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');

-- ---------------------------------------------------------------------------
-- Interest, skill, education
-- ---------------------------------------------------------------------------

INSERT INTO research_interests (name) VALUES (pg_temp.t('NLP'));
INSERT INTO skills (name) VALUES (pg_temp.t('PyTorch'));
SELECT pg_temp.expect_error('Interest: name is unique ignoring letter case (FR-4.2)',
    $s$INSERT INTO research_interests (name) VALUES (lower(pg_temp.t('NLP')))$s$, '23505');
SELECT pg_temp.expect_error('Skill: name is unique ignoring letter case (FR-4.2)',
    $s$INSERT INTO skills (name) VALUES (lower(pg_temp.t('PyTorch')))$s$, '23505');

-- Tag name limits (FR-4.2, migration 000004): at most 60 characters, trimmed, single
-- spaces only. The accepted values use a two-byte letter, since lengths count characters.
SELECT pg_temp.expect_ok('Interest: name of 60 characters with single spaces is accepted',
    $s$INSERT INTO research_interests (name) VALUES (pg_temp.t(repeat('é', 21) || ' ' || repeat('é', 21)))$s$);
SELECT pg_temp.expect_error('Interest: name is at most 60 characters',
    $s$INSERT INTO research_interests (name) VALUES (repeat('a', 61))$s$, '23514');
SELECT pg_temp.expect_error('Interest: name has no leading space',
    $s$INSERT INTO research_interests (name) VALUES (' Robotics')$s$, '23514');
SELECT pg_temp.expect_error('Interest: name has no trailing space',
    $s$INSERT INTO research_interests (name) VALUES ('Robotics ')$s$, '23514');
SELECT pg_temp.expect_error('Interest: name has no double space',
    $s$INSERT INTO research_interests (name) VALUES ('Machine  Learning')$s$, '23514');
SELECT pg_temp.expect_error('Interest: name has no tab or line break',
    $s$INSERT INTO research_interests (name) VALUES (E'Machine\tLearning')$s$, '23514');
SELECT pg_temp.expect_ok('Skill: name of 60 characters with single spaces is accepted',
    $s$INSERT INTO skills (name) VALUES (pg_temp.t(repeat('é', 21) || ' ' || repeat('é', 21)))$s$);
SELECT pg_temp.expect_error('Skill: name is at most 60 characters',
    $s$INSERT INTO skills (name) VALUES (repeat('a', 61))$s$, '23514');
SELECT pg_temp.expect_error('Skill: name has no leading space',
    $s$INSERT INTO skills (name) VALUES (' Go')$s$, '23514');
SELECT pg_temp.expect_error('Skill: name has no trailing space',
    $s$INSERT INTO skills (name) VALUES ('Go ')$s$, '23514');
SELECT pg_temp.expect_error('Skill: name has no double space',
    $s$INSERT INTO skills (name) VALUES ('Data  Analysis')$s$, '23514');
SELECT pg_temp.expect_error('Skill: name has no tab or line break',
    $s$INSERT INTO skills (name) VALUES (E'Data\nAnalysis')$s$, '23514');
SELECT pg_temp.expect_error('Education: end year is not before start year',
    $s$INSERT INTO profile_education (user_id, institution, degree, start_year, end_year)
       VALUES ('00000000-0000-0000-0000-000000000001', 'MIT', 'BSc', 2020, 2019)$s$, '23514');

-- Education fields (FR-4.4, migration 000005): the start year is required, the degree and
-- field are optional, text is at most 200 (institution) or 100 characters and never blank.
SELECT pg_temp.expect_ok('Education: degree, field and end year may be NULL',
    $s$INSERT INTO profile_education (user_id, institution, start_year)
       VALUES ('00000000-0000-0000-0000-000000000001', 'MIT', 2020)$s$);
SELECT pg_temp.expect_error('Education: start year is required',
    $s$INSERT INTO profile_education (user_id, institution, degree)
       VALUES ('00000000-0000-0000-0000-000000000001', 'MIT', 'BSc')$s$, '23502');
SELECT pg_temp.expect_ok('Education: institution of 200 and degree and field of 100 characters are accepted',
    $s$INSERT INTO profile_education (user_id, institution, degree, field_of_study, start_year)
       VALUES ('00000000-0000-0000-0000-000000000001', repeat('é', 200), repeat('é', 100), repeat('é', 100), 2020)$s$);
SELECT pg_temp.expect_error('Education: institution is at most 200 characters',
    $s$INSERT INTO profile_education (user_id, institution, start_year)
       VALUES ('00000000-0000-0000-0000-000000000001', repeat('a', 201), 2020)$s$, '23514');
SELECT pg_temp.expect_error('Education: degree is at most 100 characters',
    $s$INSERT INTO profile_education (user_id, institution, degree, start_year)
       VALUES ('00000000-0000-0000-0000-000000000001', 'MIT', repeat('a', 101), 2020)$s$, '23514');
SELECT pg_temp.expect_error('Education: field of study is at most 100 characters',
    $s$INSERT INTO profile_education (user_id, institution, field_of_study, start_year)
       VALUES ('00000000-0000-0000-0000-000000000001', 'MIT', repeat('a', 101), 2020)$s$, '23514');
SELECT pg_temp.expect_error('Education: institution is not blank',
    $s$INSERT INTO profile_education (user_id, institution, start_year)
       VALUES ('00000000-0000-0000-0000-000000000001', '  ', 2020)$s$, '23514');
SELECT pg_temp.expect_error('Education: degree is NULL, not blank',
    $s$INSERT INTO profile_education (user_id, institution, degree, start_year)
       VALUES ('00000000-0000-0000-0000-000000000001', 'MIT', '', 2020)$s$, '23514');
SELECT pg_temp.expect_error('Education: field of study is NULL, not blank',
    $s$INSERT INTO profile_education (user_id, institution, field_of_study, start_year)
       VALUES ('00000000-0000-0000-0000-000000000001', 'MIT', ' ', 2020)$s$, '23514');

-- Education grade and description (FR-4.4, migration 000011, which also renamed the table
-- from education): both optional and never blank; the grade is at most 50 characters on
-- one line, the description at most 2000 and may have line breaks.
SELECT pg_temp.expect_true('Education: the table is profile_education, and education is gone',
    $q$SELECT to_regclass('profile_education') IS NOT NULL AND to_regclass('education') IS NULL$q$);
SELECT pg_temp.expect_ok('Education: grade and description may be NULL',
    $s$INSERT INTO profile_education (user_id, institution, start_year, grade, description)
       VALUES ('00000000-0000-0000-0000-000000000001', 'MIT', 2020, NULL, NULL)$s$);
SELECT pg_temp.expect_ok('Education: grade of 50 and description of 2000 characters with a line break are accepted',
    $s$INSERT INTO profile_education (user_id, institution, start_year, grade, description)
       VALUES ('00000000-0000-0000-0000-000000000001', 'MIT', 2020, repeat('é', 50), repeat('é', 1999) || E'\n')$s$);
SELECT pg_temp.expect_error('Education: grade is at most 50 characters',
    $s$INSERT INTO profile_education (user_id, institution, start_year, grade)
       VALUES ('00000000-0000-0000-0000-000000000001', 'MIT', 2020, repeat('a', 51))$s$, '23514');
SELECT pg_temp.expect_error('Education: grade is NULL, not blank',
    $s$INSERT INTO profile_education (user_id, institution, start_year, grade)
       VALUES ('00000000-0000-0000-0000-000000000001', 'MIT', 2020, ' ')$s$, '23514');
SELECT pg_temp.expect_error('Education: grade has no line break',
    $s$INSERT INTO profile_education (user_id, institution, start_year, grade)
       VALUES ('00000000-0000-0000-0000-000000000001', 'MIT', 2020, E'First\nclass')$s$, '23514');
SELECT pg_temp.expect_error('Education: description is at most 2000 characters',
    $s$INSERT INTO profile_education (user_id, institution, start_year, description)
       VALUES ('00000000-0000-0000-0000-000000000001', 'MIT', 2020, repeat('a', 2001))$s$, '23514');
SELECT pg_temp.expect_error('Education: description is NULL, not blank',
    $s$INSERT INTO profile_education (user_id, institution, start_year, description)
       VALUES ('00000000-0000-0000-0000-000000000001', 'MIT', 2020, E' \n ')$s$, '23514');

-- ---------------------------------------------------------------------------
-- Contact info and links (FR-4.7, migration 000006)
-- ---------------------------------------------------------------------------

SELECT pg_temp.expect_ok('Contact: a row with only the user takes the defaults',
    $s$INSERT INTO profile_contact (user_id) VALUES ('00000000-0000-0000-0000-000000000001')$s$);
SELECT pg_temp.expect_true('Contact: every visibility defaults to only_me and nothing is set', $q$
    SELECT email_visibility = 'only_me' AND phone_visibility = 'only_me' AND birthday_visibility = 'only_me'
       AND phone IS NULL AND birth_month IS NULL AND birth_day IS NULL
      FROM profile_contact WHERE user_id = '00000000-0000-0000-0000-000000000001'$q$);
SELECT pg_temp.expect_error('Contact: one row per user',
    $s$INSERT INTO profile_contact (user_id) VALUES ('00000000-0000-0000-0000-000000000001')$s$, '23505');
SELECT pg_temp.expect_ok('Contact: an E.164 phone is accepted',
    $s$UPDATE profile_contact SET phone = '+8801712345678' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$);
SELECT pg_temp.expect_error('Contact: phone starts with +',
    $s$UPDATE profile_contact SET phone = '8801712345678' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Contact: phone has no spaces',
    $s$UPDATE profile_contact SET phone = '+880 1712345678' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Contact: phone has at most 15 digits',
    $s$UPDATE profile_contact SET phone = '+1234567890123456' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Contact: phone country code does not start with 0',
    $s$UPDATE profile_contact SET phone = '+0123456789' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_ok('Contact: 29 February is accepted',
    $s$UPDATE profile_contact SET birth_month = 2, birth_day = 29 WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$);
SELECT pg_temp.expect_ok('Contact: 31 December is accepted',
    $s$UPDATE profile_contact SET birth_month = 12, birth_day = 31 WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$);
SELECT pg_temp.expect_error('Contact: 30 February is refused',
    $s$UPDATE profile_contact SET birth_month = 2, birth_day = 30 WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Contact: 31 April is refused',
    $s$UPDATE profile_contact SET birth_month = 4, birth_day = 31 WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Contact: month 13 is refused',
    $s$UPDATE profile_contact SET birth_month = 13, birth_day = 1 WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Contact: day 0 is refused',
    $s$UPDATE profile_contact SET birth_month = 1, birth_day = 0 WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Contact: a month without a day is refused',
    $s$UPDATE profile_contact SET birth_month = 5, birth_day = NULL WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Contact: a day without a month is refused',
    $s$UPDATE profile_contact SET birth_month = NULL, birth_day = 5 WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_ok('Contact: logged_in visibility is accepted',
    $s$UPDATE profile_contact SET email_visibility = 'logged_in', phone_visibility = 'logged_in', birthday_visibility = 'logged_in'
       WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$);
SELECT pg_temp.expect_error('Contact: email visibility is only_me or logged_in',
    $s$UPDATE profile_contact SET email_visibility = 'everyone' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Contact: phone visibility is only_me or logged_in',
    $s$UPDATE profile_contact SET phone_visibility = 'connections' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_error('Contact: birthday visibility is only_me or logged_in',
    $s$UPDATE profile_contact SET birthday_visibility = '' WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');

SELECT pg_temp.expect_ok('Link: an https URL of 300 characters is accepted',
    $s$INSERT INTO profile_links (user_id, kind, url)
       VALUES ('00000000-0000-0000-0000-000000000001', 'website', 'https://' || repeat('é', 292))$s$);
SELECT pg_temp.expect_error('Link: URL is at most 300 characters',
    $s$INSERT INTO profile_links (user_id, kind, url)
       VALUES ('00000000-0000-0000-0000-000000000001', 'website', 'https://' || repeat('a', 293))$s$, '23514');
SELECT pg_temp.expect_error('Link: http is refused',
    $s$INSERT INTO profile_links (user_id, kind, url)
       VALUES ('00000000-0000-0000-0000-000000000001', 'website', 'http://example.org')$s$, '23514');
SELECT pg_temp.expect_error('Link: URL has no whitespace',
    $s$INSERT INTO profile_links (user_id, kind, url)
       VALUES ('00000000-0000-0000-0000-000000000001', 'website', 'https://example.org/a b')$s$, '23514');
SELECT pg_temp.expect_error('Link: URL is more than the scheme',
    $s$INSERT INTO profile_links (user_id, kind, url)
       VALUES ('00000000-0000-0000-0000-000000000001', 'website', 'https://')$s$, '23514');
SELECT pg_temp.expect_error('Link: kind is one of the six',
    $s$INSERT INTO profile_links (user_id, kind, url)
       VALUES ('00000000-0000-0000-0000-000000000001', 'twitter', 'https://example.org')$s$, '23514');
SELECT pg_temp.expect_ok('Link: every kind is accepted',
    $s$INSERT INTO profile_links (user_id, kind, url)
       SELECT '00000000-0000-0000-0000-000000000002', k, 'https://example.org/' || k
         FROM unnest(ARRAY['website', 'google_scholar', 'researchgate', 'linkedin', 'github', 'other']) AS k$s$);

-- ---------------------------------------------------------------------------
-- Work experience (FR-4.8, migration 000009)
-- ---------------------------------------------------------------------------

SELECT pg_temp.expect_ok('Experience: a current entry with only the required fields',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', 'MIT', 'full_time', 2020, 3, true)$s$);
SELECT pg_temp.expect_ok('Experience: a past entry with every field, at the longest lengths',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, location, location_type, start_year, start_month, end_year, end_month, is_current, description)
       VALUES ('00000000-0000-0000-0000-000000000001', repeat('é', 100), repeat('é', 150), 'postdoc', repeat('é', 100), 'remote', 2018, 1, 2019, 12, false, repeat(E'é\n', 1000))$s$);
SELECT pg_temp.expect_ok('Experience: the end month may equal the start month',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, end_year, end_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Intern', 'CERN', 'internship', 2021, 6, 2021, 6, false)$s$);
SELECT pg_temp.expect_ok('Experience: every employment type is accepted',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, is_current)
       SELECT '00000000-0000-0000-0000-000000000001', 'R', 'O', t, 2020, 1, true
         FROM unnest(ARRAY['full_time', 'part_time', 'internship', 'contract', 'freelance', 'self_employed',
                           'research_assistant', 'teaching_assistant', 'postdoc', 'volunteer']) AS t$s$);
SELECT pg_temp.expect_ok('Experience: every location type is accepted',
    $s$INSERT INTO profile_experience (user_id, location_type, title, organization, employment_type, start_year, start_month, is_current)
       SELECT '00000000-0000-0000-0000-000000000001', t, 'R', 'O', 'contract', 2020, 1, true
         FROM unnest(ARRAY['on_site', 'hybrid', 'remote']) AS t$s$);
SELECT pg_temp.expect_error('Experience: user must exist',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, is_current)
       VALUES (gen_random_uuid(), 'Researcher', 'MIT', 'full_time', 2020, 3, true)$s$, '23503');
SELECT pg_temp.expect_error('Experience: title is required',
    $s$INSERT INTO profile_experience (user_id, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'MIT', 'full_time', 2020, 3, true)$s$, '23502');
SELECT pg_temp.expect_error('Experience: title is at most 100 characters',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', repeat('a', 101), 'MIT', 'full_time', 2020, 3, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: title is not blank',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', '  ', 'MIT', 'full_time', 2020, 3, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: title is one line',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', E'Research\nAssistant', 'MIT', 'full_time', 2020, 3, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: organization is required',
    $s$INSERT INTO profile_experience (user_id, title, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', 'full_time', 2020, 3, true)$s$, '23502');
SELECT pg_temp.expect_error('Experience: organization is at most 150 characters',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', repeat('a', 151), 'full_time', 2020, 3, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: organization is not blank',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', '', 'full_time', 2020, 3, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: organization is one line',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', E'MIT\r', 'full_time', 2020, 3, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: employment type is required',
    $s$INSERT INTO profile_experience (user_id, title, organization, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', 'MIT', 2020, 3, true)$s$, '23502');
SELECT pg_temp.expect_error('Experience: employment type is one of the ten',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', 'MIT', 'gig', 2020, 3, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: location is at most 100 characters',
    $s$INSERT INTO profile_experience (user_id, location, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', repeat('a', 101), 'Researcher', 'MIT', 'full_time', 2020, 3, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: location is NULL, not blank',
    $s$INSERT INTO profile_experience (user_id, location, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', ' ', 'Researcher', 'MIT', 'full_time', 2020, 3, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: location is one line',
    $s$INSERT INTO profile_experience (user_id, location, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', E'Dhaka\nBangladesh', 'Researcher', 'MIT', 'full_time', 2020, 3, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: location type is one of the three',
    $s$INSERT INTO profile_experience (user_id, location_type, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'office', 'Researcher', 'MIT', 'full_time', 2020, 3, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: description is at most 2000 characters',
    $s$INSERT INTO profile_experience (user_id, description, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', repeat('a', 2001), 'Researcher', 'MIT', 'full_time', 2020, 3, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: description is NULL, not blank',
    $s$INSERT INTO profile_experience (user_id, description, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', E' \n ', 'Researcher', 'MIT', 'full_time', 2020, 3, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: start year is required',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', 'MIT', 'full_time', 3, true)$s$, '23502');
SELECT pg_temp.expect_error('Experience: start month is required',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', 'MIT', 'full_time', 2020, true)$s$, '23502');
SELECT pg_temp.expect_error('Experience: start year is at least 1900',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', 'MIT', 'full_time', 1899, 3, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: start year is at most 2100',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', 'MIT', 'full_time', 2101, 3, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: start month is at least 1',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', 'MIT', 'full_time', 2020, 0, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: start month is at most 12',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', 'MIT', 'full_time', 2020, 13, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: end month is at most 12',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, end_year, end_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', 'MIT', 'full_time', 2020, 3, 2021, 13, false)$s$, '23514');
SELECT pg_temp.expect_error('Experience: end year is at most 2100',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, end_year, end_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', 'MIT', 'full_time', 2020, 3, 2101, 1, false)$s$, '23514');
SELECT pg_temp.expect_error('Experience: end year without end month is refused',
    $s$INSERT INTO profile_experience (user_id, end_year, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 2021, 'Researcher', 'MIT', 'full_time', 2020, 3, false)$s$, '23514');
SELECT pg_temp.expect_error('Experience: end month without end year is refused',
    $s$INSERT INTO profile_experience (user_id, end_month, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 5, 'Researcher', 'MIT', 'full_time', 2020, 3, false)$s$, '23514');
SELECT pg_temp.expect_error('Experience: is_current is required',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', 'MIT', 'full_time', 2020, 3)$s$, '23502');
SELECT pg_temp.expect_error('Experience: a past entry needs an end date',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', 'MIT', 'full_time', 2020, 3, false)$s$, '23514');
SELECT pg_temp.expect_error('Experience: a current entry has no end date',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, end_year, end_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', 'MIT', 'full_time', 2020, 3, 2021, 1, true)$s$, '23514');
SELECT pg_temp.expect_error('Experience: end is not before start, earlier year',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, end_year, end_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', 'MIT', 'full_time', 2020, 3, 2019, 12, false)$s$, '23514');
SELECT pg_temp.expect_error('Experience: end is not before start, same year earlier month',
    $s$INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, end_year, end_month, is_current)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Researcher', 'MIT', 'full_time', 2020, 3, 2020, 2, false)$s$, '23514');

-- ---------------------------------------------------------------------------
-- Languages (FR-4.9, migration 000010)
-- ---------------------------------------------------------------------------

SELECT pg_temp.expect_ok('Language: every proficiency is accepted',
    $s$INSERT INTO profile_languages (user_id, name, proficiency)
       SELECT '00000000-0000-0000-0000-000000000001', 'Lang ' || p, p
         FROM unnest(ARRAY['elementary', 'limited_working', 'professional_working',
                           'full_professional', 'native_or_bilingual']) AS p$s$);
SELECT pg_temp.expect_ok('Language: a name of 50 characters',
    $s$INSERT INTO profile_languages (user_id, name, proficiency)
       VALUES ('00000000-0000-0000-0000-000000000001', repeat('é', 50), 'elementary')$s$);
SELECT pg_temp.expect_ok('Language: Bangla for one user',
    $s$INSERT INTO profile_languages (user_id, name, proficiency)
       VALUES ('00000000-0000-0000-0000-000000000001', 'Bangla', 'native_or_bilingual')$s$);
SELECT pg_temp.expect_ok('Language: two users may know the same language',
    $s$INSERT INTO profile_languages (user_id, name, proficiency)
       VALUES ('00000000-0000-0000-0000-000000000002', 'bangla', 'elementary')$s$);
SELECT pg_temp.expect_error('Language: the same name in another case for one user is refused',
    $s$INSERT INTO profile_languages (user_id, name, proficiency)
       VALUES ('00000000-0000-0000-0000-000000000001', 'BANGLA', 'elementary')$s$, '23505');
SELECT pg_temp.expect_error('Language: user must exist',
    $s$INSERT INTO profile_languages (user_id, name, proficiency)
       VALUES (gen_random_uuid(), 'English', 'elementary')$s$, '23503');
SELECT pg_temp.expect_error('Language: name is required',
    $s$INSERT INTO profile_languages (user_id, proficiency)
       VALUES ('00000000-0000-0000-0000-000000000001', 'elementary')$s$, '23502');
SELECT pg_temp.expect_error('Language: name is at most 50 characters',
    $s$INSERT INTO profile_languages (user_id, name, proficiency)
       VALUES ('00000000-0000-0000-0000-000000000001', repeat('a', 51), 'elementary')$s$, '23514');
SELECT pg_temp.expect_error('Language: name is not empty',
    $s$INSERT INTO profile_languages (user_id, name, proficiency)
       VALUES ('00000000-0000-0000-0000-000000000001', '', 'elementary')$s$, '23514');
SELECT pg_temp.expect_error('Language: name is not blank',
    $s$INSERT INTO profile_languages (user_id, name, proficiency)
       VALUES ('00000000-0000-0000-0000-000000000001', '  ', 'elementary')$s$, '23514');
SELECT pg_temp.expect_error('Language: name is one line',
    $s$INSERT INTO profile_languages (user_id, name, proficiency)
       VALUES ('00000000-0000-0000-0000-000000000001', E'Old\nNorse', 'elementary')$s$, '23514');
SELECT pg_temp.expect_error('Language: proficiency is required',
    $s$INSERT INTO profile_languages (user_id, name)
       VALUES ('00000000-0000-0000-0000-000000000001', 'English')$s$, '23502');
SELECT pg_temp.expect_error('Language: proficiency is one of the five',
    $s$INSERT INTO profile_languages (user_id, name, proficiency)
       VALUES ('00000000-0000-0000-0000-000000000001', 'English', 'fluent')$s$, '23514');

-- ---------------------------------------------------------------------------
-- Stored files (FR-4.6, FR-12.2, NFR-13, migration 000007)
-- ---------------------------------------------------------------------------

SELECT pg_temp.expect_ok('Stored file: a photo of exactly 2 MB is accepted',
    $s$INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes, width, height)
       VALUES ('00000000-0000-0000-0000-000000000001', 'photo', pg_temp.t('photo/ok-1'), 'image/jpeg', 2097152, 400, 400)$s$);
SELECT pg_temp.expect_true('Stored file: scan status starts as pending', $q$
    SELECT scan_status = 'pending' FROM stored_files WHERE object_key = pg_temp.t('photo/ok-1')$q$);
SELECT pg_temp.expect_ok('Stored file: every purpose a user owns is accepted, a 5 MB pdf too',
    $s$INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes)
       SELECT '00000000-0000-0000-0000-000000000001', p, pg_temp.t('any/') || p, 'application/pdf', 5242880
         FROM unnest(ARRAY['cover', 'pdf', 'attachment']) AS p$s$);
SELECT pg_temp.expect_ok('Stored file: a figure belongs to a project, with its uploader (FR-12.2)',
    $s$INSERT INTO stored_files (project_id, uploaded_by, purpose, object_key, content_type, size_bytes)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000002', 'figure',
               pg_temp.t('any/figure'), 'application/pdf', 5242880)$s$);
SELECT pg_temp.expect_error('Stored file: a figure has no user owner',
    $s$INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes)
       VALUES ('00000000-0000-0000-0000-000000000001', 'figure', pg_temp.t('x/f1'), 'image/png', 10)$s$, '23514');
SELECT pg_temp.expect_error('Stored file: only a figure belongs to a project',
    $s$INSERT INTO stored_files (project_id, purpose, object_key, content_type, size_bytes)
       VALUES ('00000000-0000-0000-0000-0000000000a1', 'attachment', pg_temp.t('x/f2'), 'image/png', 10)$s$, '23514');
SELECT pg_temp.expect_error('Stored file: a user or a project owns it, not both',
    $s$INSERT INTO stored_files (owner_user_id, project_id, purpose, object_key, content_type, size_bytes)
       VALUES ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-0000000000a1', 'figure', pg_temp.t('x/f3'), 'image/png', 10)$s$, '23514');
SELECT pg_temp.expect_error('Stored file: somebody owns it',
    $s$INSERT INTO stored_files (purpose, object_key, content_type, size_bytes)
       VALUES ('photo', pg_temp.t('x/f4'), 'image/png', 10)$s$, '23514');
SELECT pg_temp.expect_error('Stored file: unknown purpose',
    $s$INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes)
       VALUES ('00000000-0000-0000-0000-000000000001', 'avatar', pg_temp.t('x/1'), 'image/png', 10)$s$, '23514');
-- 'rejected' was the old name; 000024 replaced it with infected and failed (FR-16.7).
SELECT pg_temp.expect_error('Stored file: unknown scan status',
    $s$INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes, scan_status)
       VALUES ('00000000-0000-0000-0000-000000000001', 'photo', pg_temp.t('x/2'), 'image/png', 10, 'rejected')$s$, '23514');
SELECT pg_temp.expect_error('Stored file: an attachment is at most 25 MB',
    $s$INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes)
       VALUES ('00000000-0000-0000-0000-000000000001', 'attachment', pg_temp.t('x/2b'), 'application/pdf', 26214401)$s$, '23514');
SELECT pg_temp.expect_error('Stored file: size must be more than 0',
    $s$INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes)
       VALUES ('00000000-0000-0000-0000-000000000001', 'pdf', pg_temp.t('x/3'), 'application/pdf', 0)$s$, '23514');
SELECT pg_temp.expect_error('Stored file: a photo is at most 2 MB (FR-4.6)',
    $s$INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes)
       VALUES ('00000000-0000-0000-0000-000000000001', 'photo', pg_temp.t('x/4'), 'image/png', 2097153)$s$, '23514');
SELECT pg_temp.expect_error('Stored file: a figure is at most 200 MiB, the ceiling of FIGURE_MAX_BYTES (FR-12.2)',
    $s$INSERT INTO stored_files (project_id, purpose, object_key, content_type, size_bytes)
       VALUES ('00000000-0000-0000-0000-0000000000a1', 'figure', pg_temp.t('x/5'), 'image/png', 209715201)$s$, '23514');
SELECT pg_temp.expect_error('Stored file: width and height are both set or both empty',
    $s$INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes, width)
       VALUES ('00000000-0000-0000-0000-000000000001', 'photo', pg_temp.t('x/6'), 'image/png', 10, 100)$s$, '23514');
SELECT pg_temp.expect_error('Stored file: width must be positive',
    $s$INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes, width, height)
       VALUES ('00000000-0000-0000-0000-000000000001', 'photo', pg_temp.t('x/7'), 'image/png', 10, -1, 100)$s$, '23514');
SELECT pg_temp.expect_error('Stored file: object key is unique',
    $s$INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes)
       VALUES ('00000000-0000-0000-0000-000000000001', 'photo', pg_temp.t('photo/ok-1'), 'image/png', 10)$s$, '23505');
SELECT pg_temp.expect_error('Stored file: object key has no leading slash',
    $s$INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes)
       VALUES ('00000000-0000-0000-0000-000000000001', 'photo', '/photo/x', 'image/png', 10)$s$, '23514');
SELECT pg_temp.expect_error('Stored file: object key has no ..',
    $s$INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes)
       VALUES ('00000000-0000-0000-0000-000000000001', 'photo', 'photo/../x', 'image/png', 10)$s$, '23514');
SELECT pg_temp.expect_error('Stored file: content type looks like type/subtype',
    $s$INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes)
       VALUES ('00000000-0000-0000-0000-000000000001', 'photo', pg_temp.t('x/8'), 'image', 10)$s$, '23514');
SELECT pg_temp.expect_ok('Stored file: deleting a row queues its object for deletion',
    $s$DELETE FROM stored_files WHERE object_key = pg_temp.t('any/pdf')$s$);
SELECT pg_temp.expect_true('Stored file: the deleted row''s key is in storage_deletions', $q$
    SELECT EXISTS (SELECT 1 FROM storage_deletions WHERE object_key = pg_temp.t('any/pdf') AND attempts = 0)$q$);

-- ---------------------------------------------------------------------------
-- Profile photo and cover (FR-4.6, migration 000008)
-- ---------------------------------------------------------------------------

INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes, width, height) VALUES
    ('00000000-0000-0000-0000-000000000001', 'cover', pg_temp.t('users/ada/cover/1.jpg'), 'image/jpeg', 100, 1584, 396),
    ('00000000-0000-0000-0000-000000000002', 'photo', pg_temp.t('users/bob/photo/1.jpg'), 'image/jpeg', 100, 400, 400);
SELECT pg_temp.expect_ok('Profile media: a profile points at its own photo and cover',
    $s$UPDATE profiles
       SET photo_file_id = (SELECT id FROM stored_files WHERE object_key = pg_temp.t('photo/ok-1')),
           cover_file_id = (SELECT id FROM stored_files WHERE object_key = pg_temp.t('users/ada/cover/1.jpg'))
       WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$);
SELECT pg_temp.expect_error('Profile media: a profile cannot show another user''s file',
    $s$UPDATE profiles SET photo_file_id = (SELECT id FROM stored_files WHERE object_key = pg_temp.t('users/bob/photo/1.jpg'))
       WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23503');
SELECT pg_temp.expect_error('Profile media: a file that does not exist',
    $s$UPDATE profiles SET cover_file_id = gen_random_uuid()
       WHERE user_id = '00000000-0000-0000-0000-000000000002'$s$, '23503');
SELECT pg_temp.expect_error('Profile media: one file is not both photo and cover',
    $s$UPDATE profiles SET cover_file_id = photo_file_id
       WHERE user_id = '00000000-0000-0000-0000-000000000001'$s$, '23514');
SELECT pg_temp.expect_ok('Profile media: deleting the photo row',
    $s$DELETE FROM stored_files WHERE object_key = pg_temp.t('photo/ok-1')$s$);
SELECT pg_temp.expect_true('Profile media: deleting the photo row clears only the photo and queues the object', $q$
    SELECT photo_file_id IS NULL AND cover_file_id IS NOT NULL
           AND EXISTS (SELECT 1 FROM storage_deletions WHERE object_key = pg_temp.t('photo/ok-1'))
    FROM profiles WHERE user_id = '00000000-0000-0000-0000-000000000001'$q$);

-- ---------------------------------------------------------------------------
-- Publication
-- ---------------------------------------------------------------------------

INSERT INTO publications (id, doi, title, created_by) VALUES
    ('00000000-0000-0000-0000-0000000000c1', '10.1000/' || pg_temp.t('xyz123'), 'A paper', '00000000-0000-0000-0000-000000000003');
SELECT pg_temp.expect_error('Publication: DOI is unique (FR-5.4)',
    $s$INSERT INTO publications (doi, title) VALUES ('10.1000/' || pg_temp.t('xyz123'), 'Same paper')$s$, '23505');
SELECT pg_temp.expect_error('Publication: DOI is stored lower-case, so one spelling per DOI (FR-5.4)',
    $s$INSERT INTO publications (doi, title) VALUES ('10.1000/' || upper(pg_temp.t('xyz123')), 'Same paper')$s$, '23514');
SELECT pg_temp.expect_error('Publication: DOI is stored in bare form, not as a link (FR-5.4)',
    $s$INSERT INTO publications (doi, title) VALUES ('https://doi.org/10.1000/' || pg_temp.t('xyz123'), 'Same paper')$s$, '23514');
SELECT pg_temp.expect_error('Publication: DOI registrant code has at least 4 digits (FR-5.4)',
    $s$INSERT INTO publications (doi, title) VALUES ('10.123/' || pg_temp.t('short'), 'Paper')$s$, '23514');
SELECT pg_temp.expect_ok('Publication: a manual entry needs no DOI (FR-5.1)',
    $s$INSERT INTO publications (title) VALUES ('Workshop talk')$s$);
SELECT pg_temp.expect_ok('Publication: an imported record may have no year (FR-5.3)',
    $s$INSERT INTO publications (title, source) VALUES ('Imported work', 'orcid')$s$);
SELECT pg_temp.expect_ok('Publication: every field at its limit (FR-5.1)',
    $s$INSERT INTO publications (title, publication_year, venue_name, abstract, url) VALUES
       (repeat('t', 500), 2024, repeat('v', 300), repeat('a', 4990) || E'\nline two', 'https://example.org/' || repeat('p', 480))$s$);
SELECT pg_temp.expect_error('Publication: title of 501 characters (FR-5.1)',
    $s$INSERT INTO publications (title) VALUES (repeat('t', 501))$s$, '23514');
SELECT pg_temp.expect_error('Publication: blank title (FR-5.1)',
    $s$INSERT INTO publications (title) VALUES ('   ')$s$, '23514');
SELECT pg_temp.expect_error('Publication: title on two lines (FR-5.1)',
    $s$INSERT INTO publications (title) VALUES (E'A\nB')$s$, '23514');
SELECT pg_temp.expect_error('Publication: year before 1900 (FR-5.1)',
    $s$INSERT INTO publications (title, publication_year) VALUES ('Old', 1899)$s$, '23514');
SELECT pg_temp.expect_error('Publication: venue of 301 characters (FR-5.1)',
    $s$INSERT INTO publications (title, venue_name) VALUES ('Paper', repeat('v', 301))$s$, '23514');
SELECT pg_temp.expect_error('Publication: blank venue is NULL, not stored (FR-5.1)',
    $s$INSERT INTO publications (title, venue_name) VALUES ('Paper', ' ')$s$, '23514');
SELECT pg_temp.expect_error('Publication: venue on two lines (FR-5.1)',
    $s$INSERT INTO publications (title, venue_name) VALUES ('Paper', E'A\rB')$s$, '23514');
SELECT pg_temp.expect_error('Publication: abstract of 5001 characters (FR-5.1)',
    $s$INSERT INTO publications (title, abstract) VALUES ('Paper', repeat('a', 5001))$s$, '23514');
SELECT pg_temp.expect_error('Publication: link that is not http or https (FR-5.1)',
    $s$INSERT INTO publications (title, url) VALUES ('Paper', 'ftp://example.org/p')$s$, '23514');
SELECT pg_temp.expect_error('Publication: link of 501 characters (FR-5.1)',
    $s$INSERT INTO publications (title, url) VALUES ('Paper', 'https://example.org/' || repeat('p', 481))$s$, '23514');
SELECT pg_temp.expect_error('Publication: link to a publication that does not exist (FR-5.4)',
    $s$INSERT INTO user_publications (user_id, publication_id)
       VALUES ('00000000-0000-0000-0000-000000000002', gen_random_uuid())$s$, '23503');
INSERT INTO publications (id, title, publication_year, created_by) VALUES
    ('00000000-0000-0000-0000-0000000000c2', 'Bob''s paper', 2024, '00000000-0000-0000-0000-000000000002');
INSERT INTO user_publications (user_id, publication_id, confirmed_at) VALUES
    ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-0000000000c2', now());
SELECT pg_temp.expect_error('Publication: one link per user and publication (FR-5.4)',
    $s$INSERT INTO user_publications (user_id, publication_id)
       VALUES ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-0000000000c2')$s$, '23505');
SELECT pg_temp.expect_error('Publication: created_by names a real user',
    $s$INSERT INTO publications (title, created_by) VALUES ('Paper', gen_random_uuid())$s$, '23503');
SELECT pg_temp.expect_ok('Publication: a record with links can be deleted (FR-5.7)',
    $s$DELETE FROM publications WHERE id = '00000000-0000-0000-0000-0000000000c2'$s$);
SELECT pg_temp.expect_true('Publication: deleting the record removes its links (FR-5.7)', $q$
    SELECT NOT EXISTS (SELECT 1 FROM user_publications WHERE publication_id = '00000000-0000-0000-0000-0000000000c2')$q$);

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
-- Notification (FR-9.1, migration 000014)
-- ---------------------------------------------------------------------------

SELECT pg_temp.expect_error('Notification: an actor is required',
    $s$INSERT INTO notifications (user_id, type, entity_id)
       VALUES ('00000000-0000-0000-0000-000000000002', 'connection_request_received', '00000000-0000-0000-0000-0000000000d1')$s$, '23514');
SELECT pg_temp.expect_error('Notification: an item is required',
    $s$INSERT INTO notifications (user_id, type, actor_id)
       VALUES ('00000000-0000-0000-0000-000000000002', 'connection_request_received', '00000000-0000-0000-0000-000000000001')$s$, '23502');
INSERT INTO notifications (user_id, type, actor_id, entity_id) VALUES
    ('00000000-0000-0000-0000-000000000002', 'connection_request_received',
     '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-0000000000d1');
SELECT pg_temp.expect_error('Notification: one connection request notification per pair',
    $s$INSERT INTO notifications (user_id, type, actor_id, entity_id)
       VALUES ('00000000-0000-0000-0000-000000000002', 'connection_request_received',
               '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-0000000000d2')$s$, '23505');
SELECT pg_temp.expect_ok('Notification: the same pair may also have an accepted notification',
    $s$INSERT INTO notifications (user_id, type, actor_id, entity_id)
       VALUES ('00000000-0000-0000-0000-000000000002', 'connection_request_accepted',
               '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-0000000000d1')$s$);
SELECT pg_temp.expect_ok('Notification: the other direction is another pair',
    $s$INSERT INTO notifications (user_id, type, actor_id, entity_id)
       VALUES ('00000000-0000-0000-0000-000000000001', 'connection_request_received',
               '00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-0000000000d1')$s$);
SELECT pg_temp.expect_ok('Notification: other types are not limited per pair',
    $s$INSERT INTO notifications (user_id, type, actor_id, entity_id)
       VALUES ('00000000-0000-0000-0000-000000000002', 'post_like', '00000000-0000-0000-0000-000000000001', gen_random_uuid()),
              ('00000000-0000-0000-0000-000000000002', 'post_like', '00000000-0000-0000-0000-000000000001', gen_random_uuid())$s$);
INSERT INTO notifications (user_id, type, actor_id, entity_id) VALUES
    ('00000000-0000-0000-0000-000000000002', 'project_member_role_changed',
     '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-0000000000a1');
SELECT pg_temp.expect_error('Notification: one of each project type per recipient, actor and project (FR-11)',
    $s$INSERT INTO notifications (user_id, type, actor_id, entity_id)
       VALUES ('00000000-0000-0000-0000-000000000002', 'project_member_role_changed',
               '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-0000000000a1')$s$, '23505');
SELECT pg_temp.expect_ok('Notification: the same project type about another project',
    $s$INSERT INTO notifications (user_id, type, actor_id, entity_id)
       VALUES ('00000000-0000-0000-0000-000000000002', 'project_member_role_changed',
               '00000000-0000-0000-0000-000000000001', gen_random_uuid())$s$);
SELECT pg_temp.expect_error('Notification: a project type has an actor',
    $s$INSERT INTO notifications (user_id, type, entity_id)
       VALUES ('00000000-0000-0000-0000-000000000002', 'project_ownership_transferred', gen_random_uuid())$s$, '23514');

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
    $s$INSERT INTO project_join_requests (project_id, user_id, kind, role)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000003', 'invitation', 'viewer')$s$, '23505');
UPDATE project_join_requests SET status = 'rejected', decided_at = now() WHERE id = '00000000-0000-0000-0000-0000000000f1';
SELECT pg_temp.expect_ok('Join request: a new request is allowed after a rejection',
    $s$INSERT INTO project_join_requests (project_id, user_id, kind)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000003', 'request')$s$);
SELECT pg_temp.expect_error('Join request: a decided request has its decision time',
    $s$UPDATE project_join_requests SET status = 'accepted'
       WHERE project_id = '00000000-0000-0000-0000-0000000000a1' AND user_id = '00000000-0000-0000-0000-000000000003'
         AND status = 'pending'$s$, '23514');
SELECT pg_temp.expect_error('Join request: a pending request has no decision time',
    $s$UPDATE project_join_requests SET decided_at = now()
       WHERE project_id = '00000000-0000-0000-0000-0000000000a1' AND user_id = '00000000-0000-0000-0000-000000000003'
         AND status = 'pending'$s$, '23514');
SELECT pg_temp.expect_error('Join request: a request has no role (FR-11.2)',
    $s$INSERT INTO project_join_requests (project_id, user_id, kind, role)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000003', 'request', 'editor')$s$, '23514');
SELECT pg_temp.expect_error('Join request: a message is at most 500 characters (FR-11.1)',
    $s$INSERT INTO project_join_requests (project_id, user_id, kind, status, decided_at, message)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000003', 'request', 'rejected', now(),
               repeat('x', 501))$s$, '23514');
SELECT pg_temp.expect_error('Join request: an empty message is stored as none',
    $s$INSERT INTO project_join_requests (project_id, user_id, kind, status, decided_at, message)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000003', 'request', 'rejected', now(), '')$s$, '23514');
SELECT pg_temp.expect_ok('Join request: a message of 500 characters',
    $s$INSERT INTO project_join_requests (project_id, user_id, kind, status, decided_at, message)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000003', 'request', 'rejected', now(),
               repeat('é', 500))$s$);
SELECT pg_temp.expect_error('Invitation: offers a role (FR-11.3)',
    $s$INSERT INTO project_join_requests (project_id, user_id, kind, status, decided_at)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000003', 'invitation', 'rejected', now())$s$, '23514');
SELECT pg_temp.expect_error('Invitation: never offers ownership (FR-10.5)',
    $s$INSERT INTO project_join_requests (project_id, user_id, kind, status, decided_at, role)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000003', 'invitation', 'rejected', now(), 'owner')$s$, '23514');
SELECT pg_temp.expect_error('Invitation: carries no message',
    $s$INSERT INTO project_join_requests (project_id, user_id, kind, status, decided_at, role, message)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000003', 'invitation', 'rejected', now(), 'viewer', 'Hi')$s$, '23514');

-- ---------------------------------------------------------------------------
-- File (FR-12.1, FR-12.2, migration 000035): path rules, unique ignoring letter case, every
-- entry under a folder, at most 200 per project, text at most 1 MiB, figures in storage
-- ---------------------------------------------------------------------------

-- The tree and count checks run at the end of each statement in the application; this file
-- defers every constraint (for the owner check), so they are made immediate here.
SET CONSTRAINTS project_files_parent, project_files_max IMMEDIATE;

SELECT pg_temp.expect_ok('File: a text file at the project root',
    $s$INSERT INTO project_files (project_id, path, content) VALUES ('00000000-0000-0000-0000-0000000000a1', 'main.tex', '')$s$);
SELECT pg_temp.expect_true('File: a new file is at version 1', $q$
    SELECT version = 1 FROM project_files WHERE project_id = '00000000-0000-0000-0000-0000000000a1' AND path = 'main.tex'$q$);
SELECT pg_temp.expect_ok('File: an empty folder (FR-12.1)',
    $s$INSERT INTO project_files (project_id, path, kind) VALUES ('00000000-0000-0000-0000-0000000000a1', 'figures', 'folder')$s$);
INSERT INTO stored_files (id, project_id, purpose, object_key, content_type, size_bytes) VALUES
    ('00000000-0000-0000-0000-0000000000f1', '00000000-0000-0000-0000-0000000000a1', 'figure', pg_temp.t('projects/a1/f1'), 'image/png', 5242880),
    ('00000000-0000-0000-0000-0000000000f2', '00000000-0000-0000-0000-0000000000a1', 'figure', pg_temp.t('projects/a1/f2'), 'image/png', 10);
SELECT pg_temp.expect_ok('File: a PNG figure of 5 MB inside a folder (FR-12.2)',
    $s$INSERT INTO project_files (project_id, path, kind, stored_file_id, size_bytes)
       VALUES ('00000000-0000-0000-0000-0000000000a1', 'figures/plot.png', 'binary', '00000000-0000-0000-0000-0000000000f1', 5242880)$s$);
SELECT pg_temp.expect_error('File: path is unique within a project',
    $s$INSERT INTO project_files (project_id, path, content) VALUES ('00000000-0000-0000-0000-0000000000a1', 'main.tex', '')$s$, '23505');
SELECT pg_temp.expect_error('File: path is unique ignoring letter case',
    $s$INSERT INTO project_files (project_id, path, content) VALUES ('00000000-0000-0000-0000-0000000000a1', 'Main.TEX', '')$s$, '23505');
SELECT pg_temp.expect_error('File: path with a .. segment is refused',
    $s$INSERT INTO project_files (project_id, path, content) VALUES ('00000000-0000-0000-0000-0000000000a1', '../etc/passwd', '')$s$, '23514');
SELECT pg_temp.expect_error('File: path with a .. segment in the middle is refused',
    $s$INSERT INTO project_files (project_id, path, content) VALUES ('00000000-0000-0000-0000-0000000000a1', 'figures/../../b.tex', '')$s$, '23514');
SELECT pg_temp.expect_error('File: a hidden name is refused',
    $s$INSERT INTO project_files (project_id, path, content) VALUES ('00000000-0000-0000-0000-0000000000a1', '.latexmkrc', '')$s$, '23514');
SELECT pg_temp.expect_error('File: absolute path is refused',
    $s$INSERT INTO project_files (project_id, path, content) VALUES ('00000000-0000-0000-0000-0000000000a1', '/main.tex', '')$s$, '23514');
SELECT pg_temp.expect_error('File: empty path segment is refused',
    $s$INSERT INTO project_files (project_id, path, content) VALUES ('00000000-0000-0000-0000-0000000000a1', 'figures//b.tex', '')$s$, '23514');
SELECT pg_temp.expect_error('File: backslash is refused',
    $s$INSERT INTO project_files (project_id, path, content) VALUES ('00000000-0000-0000-0000-0000000000a1', 'a\b.tex', '')$s$, '23514');
SELECT pg_temp.expect_error('File: a space is refused (LaTeX)',
    $s$INSERT INTO project_files (project_id, path, content) VALUES ('00000000-0000-0000-0000-0000000000a1', 'my paper.tex', '')$s$, '23514');
SELECT pg_temp.expect_error('File: at most 8 levels',
    $s$INSERT INTO project_files (project_id, path, content) VALUES ('00000000-0000-0000-0000-0000000000a1', 'a/b/c/d/e/f/g/h/i.tex', '')$s$, '23514');
SELECT pg_temp.expect_error('File: a name of at most 100 characters',
    $s$INSERT INTO project_files (project_id, path, content) VALUES ('00000000-0000-0000-0000-0000000000a1', repeat('a', 101), '')$s$, '23514');
SELECT pg_temp.expect_error('File: needs its parent folder',
    $s$INSERT INTO project_files (project_id, path, content) VALUES ('00000000-0000-0000-0000-0000000000a1', 'sections/intro.tex', '')$s$, '23514');
SELECT pg_temp.expect_error('File: its parent must be a folder',
    $s$INSERT INTO project_files (project_id, path, content) VALUES ('00000000-0000-0000-0000-0000000000a1', 'main.tex/intro.tex', '')$s$, '23514');
SELECT pg_temp.expect_ok('File: a parent matches ignoring letter case',
    $s$INSERT INTO project_files (project_id, path, content) VALUES ('00000000-0000-0000-0000-0000000000a1', 'Figures/notes.tex', '')$s$);
SELECT pg_temp.expect_error('File: a folder with files in it cannot go alone',
    $s$DELETE FROM project_files WHERE project_id = '00000000-0000-0000-0000-0000000000a1' AND path = 'figures'$s$, '23514');
SELECT pg_temp.expect_error('File: a folder cannot move without its files',
    $s$UPDATE project_files SET path = 'figs' WHERE project_id = '00000000-0000-0000-0000-0000000000a1' AND path = 'figures'$s$, '23514');
SELECT pg_temp.expect_ok('File: a folder moves with its files in one statement',
    $s$UPDATE project_files SET path = 'figs' || substr(path, 8)
       WHERE project_id = '00000000-0000-0000-0000-0000000000a1' AND (path = 'figures' OR lower(path) LIKE 'figures/%')$s$);
SELECT pg_temp.expect_true('File: the files moved with it', $q$
    SELECT array_agg(path ORDER BY path) = ARRAY['figs', 'figs/notes.tex', 'figs/plot.png']
    FROM project_files WHERE project_id = '00000000-0000-0000-0000-0000000000a1' AND path <> 'main.tex'$q$);
SELECT pg_temp.expect_error('File: text of more than 1 MiB is refused',
    $s$INSERT INTO project_files (project_id, path, content, size_bytes)
       VALUES ('00000000-0000-0000-0000-0000000000a1', 'big.tex', repeat('x', 1048577), 1048577)$s$, '23514');
SELECT pg_temp.expect_error('File: a text file''s size is its length',
    $s$INSERT INTO project_files (project_id, path, content, size_bytes)
       VALUES ('00000000-0000-0000-0000-0000000000a1', 'lie.tex', 'abc', 1)$s$, '23514');
SELECT pg_temp.expect_error('File: a text file has content',
    $s$INSERT INTO project_files (project_id, path) VALUES ('00000000-0000-0000-0000-0000000000a1', 'none.tex')$s$, '23514');
SELECT pg_temp.expect_error('File: a text file cannot take a figure''s extension',
    $s$INSERT INTO project_files (project_id, path, content) VALUES ('00000000-0000-0000-0000-0000000000a1', 'fake.png', '')$s$, '23514');
SELECT pg_temp.expect_error('File: figure over 200 MiB is refused (FR-12.2)',
    $s$INSERT INTO project_files (project_id, path, kind, stored_file_id, size_bytes)
       VALUES ('00000000-0000-0000-0000-0000000000a1', 'big.png', 'binary', '00000000-0000-0000-0000-0000000000f2', 209715201)$s$, '23514');
SELECT pg_temp.expect_error('File: figure must be PNG, JPG or PDF (FR-12.2)',
    $s$INSERT INTO project_files (project_id, path, kind, stored_file_id, size_bytes)
       VALUES ('00000000-0000-0000-0000-0000000000a1', 'anim.gif', 'binary', '00000000-0000-0000-0000-0000000000f2', 10)$s$, '23514');
SELECT pg_temp.expect_error('File: a binary file needs a stored file',
    $s$INSERT INTO project_files (project_id, path, kind, size_bytes) VALUES ('00000000-0000-0000-0000-0000000000a1', 'fig.pdf', 'binary', 10)$s$, '23514');
SELECT pg_temp.expect_error('File: a figure is a stored file of the same project',
    $s$INSERT INTO project_files (project_id, path, kind, stored_file_id, size_bytes)
       SELECT '00000000-0000-0000-0000-0000000000b1', 'x.png', 'binary', '00000000-0000-0000-0000-0000000000f2', 10$s$, '23503');
SELECT pg_temp.expect_error('File: a stored file backs one figure',
    $s$INSERT INTO project_files (project_id, path, kind, stored_file_id, size_bytes)
       VALUES ('00000000-0000-0000-0000-0000000000a1', 'again.png', 'binary', '00000000-0000-0000-0000-0000000000f1', 10)$s$, '23505');
SELECT pg_temp.expect_error('File: a folder has no content',
    $s$INSERT INTO project_files (project_id, path, kind, content) VALUES ('00000000-0000-0000-0000-0000000000a1', 'sections', 'folder', 'x')$s$, '23514');
SELECT pg_temp.expect_ok('File: deleting a figure deletes its stored file',
    $s$DELETE FROM project_files WHERE project_id = '00000000-0000-0000-0000-0000000000a1' AND path = 'figs/plot.png'$s$);
SELECT pg_temp.expect_true('File: and queues its object', $q$
    SELECT NOT EXISTS (SELECT 1 FROM stored_files WHERE id = '00000000-0000-0000-0000-0000000000f1')
       AND EXISTS (SELECT 1 FROM storage_deletions WHERE object_key = pg_temp.t('projects/a1/f1'))$q$);
SELECT pg_temp.expect_ok('File: a figure again, for the project deletion below',
    $s$INSERT INTO project_files (project_id, path, kind, stored_file_id, size_bytes)
       VALUES ('00000000-0000-0000-0000-0000000000a1', 'figs/p2.png', 'binary', '00000000-0000-0000-0000-0000000000f2', 10)$s$);

-- Figure uploads waiting for confirmation (FR-12.2, migration 000036).
SELECT pg_temp.expect_ok('Figure upload: a pending upload of 50 MB',
    $s$INSERT INTO project_figure_uploads (project_id, user_id, object_key, path, size_bytes, expires_at)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000001',
               'projects/00000000-0000-0000-0000-0000000000a1/uploads/00000000-0000-0000-0000-0000000000c1',
               'figs/big.png', 52428800, now() + interval '1 hour')$s$);
SELECT pg_temp.expect_error('Figure upload: at most 200 MiB',
    $s$INSERT INTO project_figure_uploads (project_id, user_id, object_key, path, size_bytes, expires_at)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000001',
               'projects/00000000-0000-0000-0000-0000000000a1/uploads/00000000-0000-0000-0000-0000000000c2',
               'figs/huge.png', 209715201, now() + interval '1 hour')$s$, '23514');
SELECT pg_temp.expect_error('Figure upload: never empty',
    $s$INSERT INTO project_figure_uploads (project_id, user_id, object_key, path, size_bytes, expires_at)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000001',
               'projects/00000000-0000-0000-0000-0000000000a1/uploads/00000000-0000-0000-0000-0000000000c3',
               'figs/none.png', 0, now() + interval '1 hour')$s$, '23514');
SELECT pg_temp.expect_error('Figure upload: its object waits under the project''s uploads/',
    $s$INSERT INTO project_figure_uploads (project_id, user_id, object_key, path, size_bytes, expires_at)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000001',
               'projects/00000000-0000-0000-0000-0000000000a1/files/00000000-0000-0000-0000-0000000000c4',
               'figs/x.png', 10, now() + interval '1 hour')$s$, '23514');
SELECT pg_temp.expect_error('Figure upload: expires after it starts',
    $s$INSERT INTO project_figure_uploads (project_id, user_id, object_key, path, size_bytes, expires_at)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000001',
               'projects/00000000-0000-0000-0000-0000000000a1/uploads/00000000-0000-0000-0000-0000000000c5',
               'figs/x.png', 10, now() - interval '1 second')$s$, '23514');
SELECT pg_temp.expect_ok('Figure upload: a second pending upload, for the project deletion below',
    $s$INSERT INTO project_figure_uploads (project_id, user_id, object_key, path, size_bytes, expires_at)
       VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000001',
               'projects/00000000-0000-0000-0000-0000000000a1/uploads/00000000-0000-0000-0000-0000000000c6',
               'figs/later.png', 10, now() + interval '1 hour')$s$);
SELECT pg_temp.expect_ok('Figure upload: deleting one (confirmed or expired)',
    $s$DELETE FROM project_figure_uploads
       WHERE object_key = 'projects/00000000-0000-0000-0000-0000000000a1/uploads/00000000-0000-0000-0000-0000000000c1'$s$);
SELECT pg_temp.expect_true('Figure upload: queues its object for deletion', $q$
    SELECT EXISTS (SELECT 1 FROM storage_deletions
                   WHERE object_key = 'projects/00000000-0000-0000-0000-0000000000a1/uploads/00000000-0000-0000-0000-0000000000c1')$q$);

-- 200 entries at most, folders included: a project of its own, filled to the limit.
INSERT INTO projects (id, title, description) VALUES ('00000000-0000-0000-0000-0000000000a9', 'Full project', 'Many files');
INSERT INTO project_members (project_id, user_id, role) VALUES ('00000000-0000-0000-0000-0000000000a9', '00000000-0000-0000-0000-000000000001', 'owner');
INSERT INTO project_files (project_id, path, kind) VALUES ('00000000-0000-0000-0000-0000000000a9', 'dir', 'folder');
INSERT INTO project_files (project_id, path, content)
    SELECT '00000000-0000-0000-0000-0000000000a9', 'dir/f' || i || '.tex', '' FROM generate_series(1, 199) AS i;
SELECT pg_temp.expect_error('File: at most 200 per project, folders included',
    $s$INSERT INTO project_files (project_id, path, kind) VALUES ('00000000-0000-0000-0000-0000000000a9', 'more', 'folder')$s$, '23514');
DELETE FROM projects WHERE id = '00000000-0000-0000-0000-0000000000a9';
SET CONSTRAINTS ALL DEFERRED;

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
    ('00000000-0000-0000-0000-000000000201', 'ACL', 'conference', 'NLP', 'listing', pg_temp.t('123'));
SELECT pg_temp.expect_error('Venue: an imported venue is unique by source and source id',
    $s$INSERT INTO venues (name, type, scope, source, source_ref) VALUES ('ACL again', 'conference', 'NLP', 'listing', pg_temp.t('123'))$s$, '23505');
SELECT pg_temp.expect_error('Venue: an imported venue needs its source id',
    $s$INSERT INTO venues (name, type, scope, source) VALUES ('EMNLP', 'conference', 'NLP', 'listing')$s$, '23514');
SELECT pg_temp.expect_ok('Venue: venues added by an administrator need no source id (FR-15.1)',
    $s$INSERT INTO venues (name, type, scope) VALUES ('Local workshop', 'workshop', 'Graphs'),
                                                     ('Another workshop', 'workshop', 'Graphs')$s$);

-- ---------------------------------------------------------------------------
-- Embedding: one per owner, deleted with its owner (AI-1)
-- ---------------------------------------------------------------------------

INSERT INTO project_embeddings (project_id, model, content_hash, embedding) VALUES
    ('00000000-0000-0000-0000-0000000000a1', 'test-model@1', repeat('1', 64), array_fill(0.1::real, ARRAY[768])::vector);
SELECT pg_temp.expect_error('Embedding: one per owner',
    $s$INSERT INTO project_embeddings (project_id, model, content_hash, embedding)
       VALUES ('00000000-0000-0000-0000-0000000000a1', 'test-model@1', repeat('2', 64), array_fill(0.2::real, ARRAY[768])::vector)$s$, '23505');
SELECT pg_temp.expect_error('Embedding: vector must have 768 dimensions',
    $s$INSERT INTO venue_embeddings (venue_id, model, content_hash, embedding)
       VALUES ('00000000-0000-0000-0000-000000000201', 'test-model@1', repeat('1', 64), '[1,2,3]')$s$, '22000');
SELECT pg_temp.expect_error('Embedding: the hash is 64 hex characters (AI-1.4)',
    $s$INSERT INTO venue_embeddings (venue_id, model, content_hash, embedding)
       VALUES ('00000000-0000-0000-0000-000000000201', 'test-model@1', 'h1', array_fill(0.1::real, ARRAY[768])::vector)$s$, '23514');
SELECT pg_temp.expect_error('Embedding: the model is name@revision (AI-1.6)',
    $s$INSERT INTO venue_embeddings (venue_id, model, content_hash, embedding)
       VALUES ('00000000-0000-0000-0000-000000000201', 'test-model', repeat('1', 64), array_fill(0.1::real, ARRAY[768])::vector)$s$, '23514');
SELECT pg_temp.expect_error('Embedding: at most 8 chunks (AI-1.1)',
    $s$INSERT INTO venue_embeddings (venue_id, model, content_hash, embedding, chunk_count)
       VALUES ('00000000-0000-0000-0000-000000000201', 'test-model@1', repeat('1', 64), array_fill(0.1::real, ARRAY[768])::vector, 9)$s$, '23514');
SELECT pg_temp.expect_error('Embedding: at least 1 chunk (AI-1.1)',
    $s$INSERT INTO venue_embeddings (venue_id, model, content_hash, embedding, chunk_count)
       VALUES ('00000000-0000-0000-0000-000000000201', 'test-model@1', repeat('1', 64), array_fill(0.1::real, ARRAY[768])::vector, 0)$s$, '23514');

-- The embedding queue (AI-1.5) and the administrators' AI actions (AI-1.6).
SELECT pg_temp.expect_ok('Embedding job: queued by embedding_enqueue',
    $s$SELECT embedding_enqueue('profile', '00000000-0000-0000-0000-0000000000f1')$s$);
SELECT pg_temp.expect_ok('Embedding job: queuing again keeps one row and raises the generation',
    $s$SELECT embedding_enqueue('profile', '00000000-0000-0000-0000-0000000000f1')$s$);
SELECT pg_temp.expect_true('Embedding job: one row per owner', $q$
    SELECT count(*) = 1 AND max(generation) = 2 FROM embedding_jobs WHERE owner_id = '00000000-0000-0000-0000-0000000000f1'$q$);
SELECT pg_temp.expect_error('Embedding job: one row per owner (primary key)',
    $s$INSERT INTO embedding_jobs (owner_type, owner_id) VALUES ('profile', '00000000-0000-0000-0000-0000000000f1')$s$, '23505');
SELECT pg_temp.expect_error('Embedding job: unknown owner type',
    $s$INSERT INTO embedding_jobs (owner_type, owner_id) VALUES ('comment', '00000000-0000-0000-0000-0000000000f2')$s$, '23514');
SELECT pg_temp.expect_error('Embedding job: priority 0 or 1',
    $s$INSERT INTO embedding_jobs (owner_type, owner_id, priority) VALUES ('profile', '00000000-0000-0000-0000-0000000000f2', 2)$s$, '23514');
SELECT pg_temp.expect_error('Embedding job: attempts are not negative',
    $s$INSERT INTO embedding_jobs (owner_type, owner_id, attempts) VALUES ('profile', '00000000-0000-0000-0000-0000000000f2', -1)$s$, '23514');
SELECT pg_temp.expect_error('Embedding job: a failed job keeps its error',
    $s$INSERT INTO embedding_jobs (owner_type, owner_id, failed_at) VALUES ('profile', '00000000-0000-0000-0000-0000000000f2', now())$s$, '23514');
SELECT pg_temp.expect_error('Embedding job: the error is at most 500 characters',
    $s$INSERT INTO embedding_jobs (owner_type, owner_id, last_error) VALUES ('profile', '00000000-0000-0000-0000-0000000000f2', repeat('x', 501))$s$, '23514');
DELETE FROM embedding_jobs WHERE owner_id = '00000000-0000-0000-0000-0000000000f1';
SELECT pg_temp.expect_error('AI admin action: known actions only',
    $s$INSERT INTO ai_admin_actions (action, queued) VALUES ('drop_everything', 0)$s$, '23514');
SELECT pg_temp.expect_error('AI admin action: the count is not negative',
    $s$INSERT INTO ai_admin_actions (action, queued) VALUES ('embeddings_rebuild', -1)$s$, '23514');

-- ---------------------------------------------------------------------------
-- Projects (FR-10.1, FR-10.6, FR-15.11, AI-1.2). Project a2 is owned by user 3 and deleted
-- at the end of this section.
-- ---------------------------------------------------------------------------

SELECT pg_temp.expect_true('Project: new IDs are UUIDv7', $q$
    SELECT pg_get_expr(d.adbin, d.adrelid) = 'uuid_v7()'
    FROM pg_attrdef d JOIN pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum
    WHERE d.adrelid = 'projects'::regclass AND a.attname = 'id'$q$);
INSERT INTO projects (id, title, description) VALUES
    ('00000000-0000-0000-0000-0000000000a2', 'Protein folding', 'Folding proteins');
INSERT INTO project_members (project_id, user_id, role) VALUES
    ('00000000-0000-0000-0000-0000000000a2', '00000000-0000-0000-0000-000000000003', 'owner');

SELECT pg_temp.expect_error('Project: title of at least 3 characters (FR-10.1)',
    $s$UPDATE projects SET title = 'ab' WHERE id = '00000000-0000-0000-0000-0000000000a2'$s$, '23514');
SELECT pg_temp.expect_error('Project: title length counts without surrounding spaces',
    $s$UPDATE projects SET title = '   ab   ' WHERE id = '00000000-0000-0000-0000-0000000000a2'$s$, '23514');
SELECT pg_temp.expect_error('Project: title of at most 150 characters',
    $s$UPDATE projects SET title = repeat('x', 151) WHERE id = '00000000-0000-0000-0000-0000000000a2'$s$, '23514');
SELECT pg_temp.expect_ok('Project: title of 150 characters',
    $s$UPDATE projects SET title = repeat('x', 150) WHERE id = '00000000-0000-0000-0000-0000000000a2'$s$);
SELECT pg_temp.expect_error('Project: description of at most 5,000 characters',
    $s$UPDATE projects SET description = repeat('x', 5001) WHERE id = '00000000-0000-0000-0000-0000000000a2'$s$, '23514');
SELECT pg_temp.expect_error('Project: abstract is absent, not empty',
    $s$UPDATE projects SET abstract = '' WHERE id = '00000000-0000-0000-0000-0000000000a2'$s$, '23514');
SELECT pg_temp.expect_error('Project: abstract of at most 3,000 characters',
    $s$UPDATE projects SET abstract = repeat('x', 3001) WHERE id = '00000000-0000-0000-0000-0000000000a2'$s$, '23514');

-- Embedding queue triggers (AI-1.2, AI-1.5).
SELECT pg_temp.expect_true('Project embedding: a new project is queued', $q$
    SELECT EXISTS (SELECT 1 FROM embedding_jobs WHERE owner_type = 'project' AND owner_id = '00000000-0000-0000-0000-0000000000a2')$q$);
DELETE FROM embedding_jobs WHERE owner_type = 'project';
UPDATE projects SET status = 'closed' WHERE id = '00000000-0000-0000-0000-0000000000a2';
SELECT pg_temp.expect_true('Project embedding: a status change queues nothing', $q$
    SELECT NOT EXISTS (SELECT 1 FROM embedding_jobs WHERE owner_type = 'project')$q$);
UPDATE projects SET title = title, description = description WHERE id = '00000000-0000-0000-0000-0000000000a2';
SELECT pg_temp.expect_true('Project embedding: saving the same text queues nothing', $q$
    SELECT NOT EXISTS (SELECT 1 FROM embedding_jobs WHERE owner_type = 'project')$q$);
UPDATE projects SET title = 'Protein folding again' WHERE id = '00000000-0000-0000-0000-0000000000a2';
SELECT pg_temp.expect_true('Project embedding: a title change queues the project', $q$
    SELECT EXISTS (SELECT 1 FROM embedding_jobs WHERE owner_type = 'project' AND owner_id = '00000000-0000-0000-0000-0000000000a2')$q$);
DELETE FROM embedding_jobs WHERE owner_type = 'project';
UPDATE projects SET description = 'Folding proteins faster' WHERE id = '00000000-0000-0000-0000-0000000000a2';
SELECT pg_temp.expect_true('Project embedding: a description change queues the project', $q$
    SELECT EXISTS (SELECT 1 FROM embedding_jobs WHERE owner_type = 'project' AND owner_id = '00000000-0000-0000-0000-0000000000a2')$q$);
DELETE FROM embedding_jobs WHERE owner_type = 'project';
UPDATE projects SET abstract = 'We fold proteins.' WHERE id = '00000000-0000-0000-0000-0000000000a2';
SELECT pg_temp.expect_true('Project embedding: an abstract change queues the project', $q$
    SELECT EXISTS (SELECT 1 FROM embedding_jobs WHERE owner_type = 'project' AND owner_id = '00000000-0000-0000-0000-0000000000a2')$q$);
DELETE FROM embedding_jobs WHERE owner_type = 'project';
INSERT INTO skills (name) SELECT pg_temp.t('Project skill ' || g) FROM generate_series(1, 21) g;
INSERT INTO project_skills (project_id, skill_id)
    SELECT '00000000-0000-0000-0000-0000000000a2', id FROM skills WHERE name = pg_temp.t('Project skill 1');
SELECT pg_temp.expect_true('Project embedding: adding a skill queues the project', $q$
    SELECT EXISTS (SELECT 1 FROM embedding_jobs WHERE owner_type = 'project' AND owner_id = '00000000-0000-0000-0000-0000000000a2')$q$);
DELETE FROM embedding_jobs WHERE owner_type = 'project';
DELETE FROM project_skills WHERE project_id = '00000000-0000-0000-0000-0000000000a2';
SELECT pg_temp.expect_true('Project embedding: removing a skill queues the project', $q$
    SELECT EXISTS (SELECT 1 FROM embedding_jobs WHERE owner_type = 'project' AND owner_id = '00000000-0000-0000-0000-0000000000a2')$q$);

-- At most 20 required skills (FR-10.1), checked at commit.
SELECT pg_temp.expect_error('Project: at most 20 required skills',
    $s$DO $d$ BEGIN
        INSERT INTO project_skills (project_id, skill_id)
            SELECT '00000000-0000-0000-0000-0000000000a2', id FROM skills WHERE name LIKE pg_temp.t('Project skill %');
        SET CONSTRAINTS project_skills_max IMMEDIATE;
    END $d$$s$, '23514');
SET CONSTRAINTS ALL DEFERRED;
SELECT pg_temp.expect_ok('Project: 20 required skills',
    $s$DO $d$ BEGIN
        INSERT INTO project_skills (project_id, skill_id)
            SELECT '00000000-0000-0000-0000-0000000000a2', id FROM skills
            WHERE name LIKE pg_temp.t('Project skill %') AND name <> pg_temp.t('Project skill 21');
        SET CONSTRAINTS project_skills_max IMMEDIATE;
    END $d$$s$);
SET CONSTRAINTS ALL DEFERRED;

-- Project reports (FR-15.11).
INSERT INTO project_reports (id, project_id, reporter_id, reason) VALUES
    ('00000000-0000-0000-0000-0000000000b1', '00000000-0000-0000-0000-0000000000a2', '00000000-0000-0000-0000-000000000002', 'spam');
SELECT pg_temp.expect_error('Project report: one open report per reporter per project',
    $s$INSERT INTO project_reports (project_id, reporter_id, reason)
       VALUES ('00000000-0000-0000-0000-0000000000a2', '00000000-0000-0000-0000-000000000002', 'other')$s$, '23505');
SELECT pg_temp.expect_error('Project report: known reasons only',
    $s$INSERT INTO project_reports (project_id, reporter_id, reason)
       VALUES ('00000000-0000-0000-0000-0000000000a2', '00000000-0000-0000-0000-000000000001', 'boring')$s$, '23514');
SELECT pg_temp.expect_error('Project report: details of at most 1,000 characters',
    $s$INSERT INTO project_reports (project_id, reporter_id, reason, details)
       VALUES ('00000000-0000-0000-0000-0000000000a2', '00000000-0000-0000-0000-000000000001', 'other', repeat('x', 1001))$s$, '23514');
SELECT pg_temp.expect_error('Project report: resolved time and outcome go together',
    $s$UPDATE project_reports SET resolved_at = now() WHERE id = '00000000-0000-0000-0000-0000000000b1'$s$, '23514');
SELECT pg_temp.expect_error('Project report: an open report has no resolution note',
    $s$UPDATE project_reports SET resolution_note = 'Note' WHERE id = '00000000-0000-0000-0000-0000000000b1'$s$, '23514');
SELECT pg_temp.expect_ok('Project report: resolved',
    $s$UPDATE project_reports SET resolved_at = now(), outcome = 'dismissed', resolved_by = '00000000-0000-0000-0000-000000000001'
       WHERE id = '00000000-0000-0000-0000-0000000000b1'$s$);
SELECT pg_temp.expect_ok('Project report: the reporter may report again once it is resolved',
    $s$INSERT INTO project_reports (project_id, reporter_id, reason)
       VALUES ('00000000-0000-0000-0000-0000000000a2', '00000000-0000-0000-0000-000000000002', 'other')$s$);

-- Deleting the project (FR-10.6) removes its job, embedding, skills and reports, and the
-- cascade of its skills does not queue it again.
INSERT INTO project_embeddings (project_id, model, content_hash, embedding) VALUES
    ('00000000-0000-0000-0000-0000000000a2', 'test-model@1', repeat('1', 64), array_fill(0.1::real, ARRAY[768])::vector);
DELETE FROM projects WHERE id = '00000000-0000-0000-0000-0000000000a2';
SELECT pg_temp.expect_true('Project deletion: job, embedding, skills and reports removed', $q$
    SELECT NOT EXISTS (SELECT 1 FROM embedding_jobs     WHERE owner_type = 'project' AND owner_id = '00000000-0000-0000-0000-0000000000a2')
       AND NOT EXISTS (SELECT 1 FROM project_embeddings WHERE project_id = '00000000-0000-0000-0000-0000000000a2')
       AND NOT EXISTS (SELECT 1 FROM project_skills     WHERE project_id = '00000000-0000-0000-0000-0000000000a2')
       AND NOT EXISTS (SELECT 1 FROM project_reports    WHERE project_id = '00000000-0000-0000-0000-0000000000a2')$q$);

-- ---------------------------------------------------------------------------
-- Messaging (FR-16.1, FR-16.2, NFR-13). Users 1 and 2 have one conversation.
-- ---------------------------------------------------------------------------

INSERT INTO conversations (id, state, user_low, user_high) VALUES
    ('00000000-0000-0000-0000-0000000000f1', 'active',
     '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002');
INSERT INTO conversation_members (conversation_id, user_id) VALUES
    ('00000000-0000-0000-0000-0000000000f1', '00000000-0000-0000-0000-000000000001'),
    ('00000000-0000-0000-0000-0000000000f1', '00000000-0000-0000-0000-000000000002');
INSERT INTO messages (id, conversation_id, sender_id, body, client_id) VALUES
    ('00000000-0000-7000-8000-0000000000f2', '00000000-0000-0000-0000-0000000000f1',
     '00000000-0000-0000-0000-000000000001', 'hello', '00000000-0000-0000-0000-0000000000f3');

SELECT pg_temp.expect_error('Conversation: one per pair',
    $s$INSERT INTO conversations (state, user_low, user_high) VALUES ('active',
       '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002')$s$, '23505');
SELECT pg_temp.expect_error('Conversation: a pair is stored low before high',
    $s$INSERT INTO conversations (state, user_low, user_high) VALUES ('active',
       '00000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000002')$s$, '23514');
SELECT pg_temp.expect_error('Conversation: no self-conversation',
    $s$INSERT INTO conversations (state, user_low, user_high) VALUES ('active',
       '00000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000003')$s$, '23514');
SELECT pg_temp.expect_error('Conversation: a direct one names its pair',
    $s$INSERT INTO conversations (state) VALUES ('active')$s$, '23514');
SELECT pg_temp.expect_error('Conversation: a group names no pair',
    $s$INSERT INTO conversations (kind, state, user_low, user_high) VALUES ('group', 'active',
       '00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000003')$s$, '23514');
SELECT pg_temp.expect_ok('Conversation: the schema allows a group (slice 14B)',
    $s$INSERT INTO conversations (id, kind, state, name, client_id)
       VALUES ('00000000-0000-0000-0000-0000000000f4', 'group', 'active', 'Lab', gen_random_uuid())$s$);
SELECT pg_temp.expect_error('Conversation: unknown kind',
    $s$INSERT INTO conversations (kind, state) VALUES ('channel', 'active')$s$, '23514');
SELECT pg_temp.expect_error('Conversation: unknown state',
    $s$UPDATE conversations SET state = 'archived' WHERE id = '00000000-0000-0000-0000-0000000000f1'$s$, '23514');
SELECT pg_temp.expect_error('Conversation: a request names who requested it',
    $s$UPDATE conversations SET state = 'request' WHERE id = '00000000-0000-0000-0000-0000000000f1'$s$, '23514');
SELECT pg_temp.expect_error('Member: once per conversation',
    $s$INSERT INTO conversation_members (conversation_id, user_id) VALUES
       ('00000000-0000-0000-0000-0000000000f1', '00000000-0000-0000-0000-000000000001')$s$, '23505');
SELECT pg_temp.expect_error('Message: empty body',
    $s$INSERT INTO messages (conversation_id, sender_id, body, client_id) VALUES ('00000000-0000-0000-0000-0000000000f1',
       '00000000-0000-0000-0000-000000000001', '', gen_random_uuid())$s$, '23514');
SELECT pg_temp.expect_error('Message: 5,001 characters',
    $s$INSERT INTO messages (conversation_id, sender_id, body, client_id) VALUES ('00000000-0000-0000-0000-0000000000f1',
       '00000000-0000-0000-0000-000000000001', repeat('a', 5001), gen_random_uuid())$s$, '23514');
SELECT pg_temp.expect_error('Message: not trimmed',
    $s$INSERT INTO messages (conversation_id, sender_id, body, client_id) VALUES ('00000000-0000-0000-0000-0000000000f1',
       '00000000-0000-0000-0000-000000000001', E'hi\n', gen_random_uuid())$s$, '23514');
SELECT pg_temp.expect_ok('Message: 5,000 characters, multi-line',
    $s$INSERT INTO messages (conversation_id, sender_id, body, client_id) VALUES ('00000000-0000-0000-0000-0000000000f1',
       '00000000-0000-0000-0000-000000000001', repeat(E'a\nb', 1666) || 'cc', gen_random_uuid())$s$);
SELECT pg_temp.expect_error('Message: a client ID once per sender',
    $s$INSERT INTO messages (conversation_id, sender_id, body, client_id) VALUES ('00000000-0000-0000-0000-0000000000f1',
       '00000000-0000-0000-0000-000000000001', 'again', '00000000-0000-0000-0000-0000000000f3')$s$, '23505');
SELECT pg_temp.expect_ok('Message: another sender may use the same client ID',
    $s$INSERT INTO messages (conversation_id, sender_id, body, client_id) VALUES ('00000000-0000-0000-0000-0000000000f1',
       '00000000-0000-0000-0000-000000000002', 'mine', '00000000-0000-0000-0000-0000000000f3')$s$);
SELECT pg_temp.expect_true('Message: IDs are UUIDv7 values', $q$
    SELECT substr(uuid_v7()::text, 15, 1) = '7' AND substr(uuid_v7()::text, 20, 1) IN ('8', '9', 'a', 'b')$q$);
SELECT pg_temp.expect_true('Message: a later ID sorts after an earlier one', $q$
    SELECT bool_and(previous < id) FROM (
        SELECT id, lag(id) OVER (ORDER BY n) AS previous
        FROM (SELECT n, uuid_v7() AS id, pg_sleep(0.002) FROM generate_series(1, 4) n) made) ordered$q$);
SELECT pg_temp.expect_true('Message: IDs made a microsecond apart in one millisecond sort in time order', $q$
    SELECT bool_and(previous < id) FROM (
        SELECT id, lag(id) OVER (ORDER BY n) AS previous
        FROM (SELECT n, uuid_v7_at(timestamptz '2026-11-07 10:00:00.5+00' + n * interval '1 microsecond') AS id
              FROM generate_series(0, 999) n) made) ordered$q$);
SELECT pg_temp.expect_true('Message: uuid_v7_at keeps the version and variant bits', $q$
    SELECT substr(id::text, 15, 1) = '7' AND substr(id::text, 20, 1) IN ('8', '9', 'a', 'b')
    FROM (SELECT uuid_v7_at(timestamptz '2026-11-07 10:00:00.999999+00') AS id) made$q$);
SELECT pg_temp.expect_true('Paged lists: creation times use the clock, not the start of the transaction', $q$
    SELECT bool_and(pg_get_expr(d.adbin, d.adrelid) = 'clock_timestamp()') AND count(*) = 4
    FROM pg_attrdef d
    JOIN pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum
    WHERE a.attname = 'created_at'
      AND d.adrelid IN ('users'::regclass, 'connections'::regclass, 'blocks'::regclass, 'notifications'::regclass)$q$);
INSERT INTO conversation_members (conversation_id, user_id) VALUES
    ('00000000-0000-0000-0000-0000000000f4', '00000000-0000-0000-0000-000000000002');
SELECT pg_temp.expect_error('Read marker: must be a message of its own conversation (NFR-13)',
    $s$UPDATE conversation_members SET last_read_message_id = '00000000-0000-7000-8000-0000000000f2'
       WHERE conversation_id = '00000000-0000-0000-0000-0000000000f4'$s$, '23503');
SELECT pg_temp.expect_ok('Read marker: can point at a message of its conversation',
    $s$UPDATE conversation_members SET last_read_message_id = '00000000-0000-7000-8000-0000000000f2'
       WHERE conversation_id = '00000000-0000-0000-0000-0000000000f1'$s$);
DELETE FROM messages WHERE id = '00000000-0000-7000-8000-0000000000f2';
SELECT pg_temp.expect_true('Read marker: deleting its message clears it, not the member', $q$
    SELECT count(*) = 2 AND count(last_read_message_id) = 0 FROM conversation_members
    WHERE conversation_id = '00000000-0000-0000-0000-0000000000f1'$q$);

-- Editing and deleting (FR-16.5, NFR-13): message f5 by user 1 in conversation f1.
INSERT INTO messages (id, conversation_id, sender_id, body, client_id) VALUES
    ('00000000-0000-7000-8000-0000000000f5', '00000000-0000-0000-0000-0000000000f1',
     '00000000-0000-0000-0000-000000000001', 'first text', gen_random_uuid());
SELECT pg_temp.expect_error('Message: a live message has a body',
    $s$UPDATE messages SET body = NULL WHERE id = '00000000-0000-7000-8000-0000000000f5'$s$, '23514');
SELECT pg_temp.expect_error('Message: a deleted message keeps no body',
    $s$UPDATE messages SET deleted_at = clock_timestamp() WHERE id = '00000000-0000-7000-8000-0000000000f5'$s$, '23514');
SELECT pg_temp.expect_error('Message: edited after it was sent',
    $s$UPDATE messages SET edited_at = created_at - interval '1 second' WHERE id = '00000000-0000-7000-8000-0000000000f5'$s$, '23514');
SELECT pg_temp.expect_error('Message: deleted after it was sent',
    $s$UPDATE messages SET body = NULL, deleted_at = created_at - interval '1 second' WHERE id = '00000000-0000-7000-8000-0000000000f5'$s$, '23514');
SELECT pg_temp.expect_error('Message version: belongs to a message of its conversation',
    $s$INSERT INTO message_versions (conversation_id, message_id, body, shown_from, ended_by)
       VALUES ('00000000-0000-0000-0000-0000000000f4', '00000000-0000-7000-8000-0000000000f5', 'first text', now(), 'edited')$s$, '23503');
SELECT pg_temp.expect_error('Message version: a known ending',
    $s$INSERT INTO message_versions (conversation_id, message_id, body, shown_from, ended_by)
       VALUES ('00000000-0000-0000-0000-0000000000f1', '00000000-0000-7000-8000-0000000000f5', 'first text', now(), 'hidden')$s$, '23514');
SELECT pg_temp.expect_error('Message version: shown before it was replaced',
    $s$INSERT INTO message_versions (conversation_id, message_id, body, shown_from, shown_until, ended_by)
       VALUES ('00000000-0000-0000-0000-0000000000f1', '00000000-0000-7000-8000-0000000000f5', 'first text', now(), now() - interval '1 second', 'edited')$s$, '23514');
SELECT pg_temp.expect_error('Message version: 1 to 5,000 characters',
    $s$INSERT INTO message_versions (conversation_id, message_id, body, shown_from, ended_by)
       VALUES ('00000000-0000-0000-0000-0000000000f1', '00000000-0000-7000-8000-0000000000f5', '', now(), 'edited')$s$, '23514');
SELECT pg_temp.expect_ok('Message: an edit keeps the earlier text',
    $s$WITH v AS (INSERT INTO message_versions (conversation_id, message_id, body, shown_from, ended_by)
                  SELECT conversation_id, id, body, created_at, 'edited' FROM messages WHERE id = '00000000-0000-7000-8000-0000000000f5')
       UPDATE messages SET body = 'second text', edited_at = clock_timestamp() WHERE id = '00000000-0000-7000-8000-0000000000f5'$s$);
SELECT pg_temp.expect_ok('Message: a deletion keeps the last text and leaves a tombstone',
    $s$WITH v AS (INSERT INTO message_versions (conversation_id, message_id, body, shown_from, ended_by)
                  SELECT conversation_id, id, body, edited_at, 'deleted' FROM messages WHERE id = '00000000-0000-7000-8000-0000000000f5')
       UPDATE messages SET body = NULL, deleted_at = clock_timestamp() WHERE id = '00000000-0000-7000-8000-0000000000f5'$s$);
SELECT pg_temp.expect_error('Message version: a message is deleted once',
    $s$INSERT INTO message_versions (conversation_id, message_id, body, shown_from, ended_by)
       VALUES ('00000000-0000-0000-0000-0000000000f1', '00000000-0000-7000-8000-0000000000f5', 'second text', now(), 'deleted')$s$, '23505');
DELETE FROM messages WHERE id = '00000000-0000-7000-8000-0000000000f5';
SELECT pg_temp.expect_true('Message version: goes with its message', $q$
    SELECT count(*) = 0 FROM message_versions WHERE message_id = '00000000-0000-7000-8000-0000000000f5'$q$);

-- Groups (FR-16.6; migration 000021): group f6, created by user 1, with user 2.
SELECT pg_temp.expect_error('Group: has a name',
    $s$INSERT INTO conversations (kind, state, client_id) VALUES ('group', 'active', gen_random_uuid())$s$, '23514');
SELECT pg_temp.expect_error('Group: a name of at most 100 characters',
    $s$INSERT INTO conversations (kind, state, name, client_id) VALUES ('group', 'active', repeat('a', 101), gen_random_uuid())$s$, '23514');
SELECT pg_temp.expect_error('Group: a name on one line',
    $s$INSERT INTO conversations (kind, state, name, client_id) VALUES ('group', 'active', E'Lab\nTwo', gen_random_uuid())$s$, '23514');
SELECT pg_temp.expect_error('Group: a trimmed name',
    $s$INSERT INTO conversations (kind, state, name, client_id) VALUES ('group', 'active', ' Lab', gen_random_uuid())$s$, '23514');
SELECT pg_temp.expect_error('Group: has the client ID of its create',
    $s$INSERT INTO conversations (kind, state, name) VALUES ('group', 'active', 'Lab')$s$, '23514');
SELECT pg_temp.expect_error('Group: always active, never a request',
    $s$INSERT INTO conversations (kind, state, name, client_id, requested_by)
       VALUES ('group', 'request', 'Lab', gen_random_uuid(), '00000000-0000-0000-0000-000000000001')$s$, '23514');
SELECT pg_temp.expect_error('Conversation: a direct one has no name',
    $s$UPDATE conversations SET name = 'Pair' WHERE id = '00000000-0000-0000-0000-0000000000f1'$s$, '23514');
INSERT INTO conversations (id, kind, state, name, created_by, client_id) VALUES
    ('00000000-0000-0000-0000-0000000000f6', 'group', 'active', 'Lab',
     '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-0000000000f7');
SELECT pg_temp.expect_error('Group: one per creator and client ID',
    $s$INSERT INTO conversations (kind, state, name, created_by, client_id) VALUES ('group', 'active', 'Again',
       '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-0000000000f7')$s$, '23505');
INSERT INTO conversation_members (conversation_id, user_id, role, joined_at) VALUES
    ('00000000-0000-0000-0000-0000000000f6', '00000000-0000-0000-0000-000000000001', 'owner', clock_timestamp()),
    ('00000000-0000-0000-0000-0000000000f6', '00000000-0000-0000-0000-000000000002', 'member', clock_timestamp());
SELECT pg_temp.expect_error('Member: one owner per group',
    $s$UPDATE conversation_members SET role = 'owner'
       WHERE conversation_id = '00000000-0000-0000-0000-0000000000f6' AND user_id = '00000000-0000-0000-0000-000000000002'$s$, '23505');
SELECT pg_temp.expect_error('Member: a known role',
    $s$UPDATE conversation_members SET role = 'admin'
       WHERE conversation_id = '00000000-0000-0000-0000-0000000000f6' AND user_id = '00000000-0000-0000-0000-000000000002'$s$, '23514');
SELECT pg_temp.expect_true('Membership: each new member of a group opens a stay', $q$
    SELECT count(*) = 2 FROM conversation_memberships
    WHERE conversation_id = '00000000-0000-0000-0000-0000000000f6' AND left_at IS NULL$q$);
SELECT pg_temp.expect_true('Membership: a direct conversation keeps no stays', $q$
    SELECT count(*) = 0 FROM conversation_memberships WHERE conversation_id = '00000000-0000-0000-0000-0000000000f1'$q$);
SELECT pg_temp.expect_error('System message: has no body',
    $s$INSERT INTO messages (conversation_id, sender_id, kind, event, event_data, body) VALUES ('00000000-0000-0000-0000-0000000000f6',
       '00000000-0000-0000-0000-000000000001', 'system', 'group_created', '{}', 'Lab')$s$, '23514');
SELECT pg_temp.expect_error('System message: has an event',
    $s$INSERT INTO messages (conversation_id, sender_id, kind, event_data) VALUES ('00000000-0000-0000-0000-0000000000f6',
       '00000000-0000-0000-0000-000000000001', 'system', '{}')$s$, '23514');
SELECT pg_temp.expect_error('System message: a known event',
    $s$INSERT INTO messages (conversation_id, sender_id, kind, event, event_data) VALUES ('00000000-0000-0000-0000-0000000000f6',
       '00000000-0000-0000-0000-000000000001', 'system', 'member_kicked', '{}')$s$, '23514');
SELECT pg_temp.expect_error('System message: its data is an object',
    $s$INSERT INTO messages (conversation_id, sender_id, kind, event, event_data) VALUES ('00000000-0000-0000-0000-0000000000f6',
       '00000000-0000-0000-0000-000000000001', 'system', 'group_created', '[]')$s$, '23514');
SELECT pg_temp.expect_error('System message: has no client ID',
    $s$INSERT INTO messages (conversation_id, sender_id, kind, event, event_data, client_id) VALUES ('00000000-0000-0000-0000-0000000000f6',
       '00000000-0000-0000-0000-000000000001', 'system', 'group_created', '{}', gen_random_uuid())$s$, '23514');
SELECT pg_temp.expect_error('Text message: has a sender',
    $s$INSERT INTO messages (conversation_id, body, client_id) VALUES ('00000000-0000-0000-0000-0000000000f6', 'hi', gen_random_uuid())$s$, '23514');
SELECT pg_temp.expect_error('Text message: has no event',
    $s$INSERT INTO messages (conversation_id, sender_id, body, client_id, event, event_data) VALUES ('00000000-0000-0000-0000-0000000000f6',
       '00000000-0000-0000-0000-000000000001', 'hi', gen_random_uuid(), 'group_created', '{}')$s$, '23514');
SELECT pg_temp.expect_ok('System message: may have no actor',
    $s$INSERT INTO messages (id, conversation_id, kind, event, event_data) VALUES ('00000000-0000-7000-8000-0000000000f8',
       '00000000-0000-0000-0000-0000000000f6', 'system', 'owner_changed', '{"newOwnerId":"00000000-0000-0000-0000-000000000001"}')$s$);
SELECT pg_temp.expect_error('System message: cannot be edited',
    $s$UPDATE messages SET edited_at = clock_timestamp() WHERE id = '00000000-0000-7000-8000-0000000000f8'$s$, '23514');
SELECT pg_temp.expect_error('System message: cannot be deleted',
    $s$UPDATE messages SET deleted_at = clock_timestamp() WHERE id = '00000000-0000-7000-8000-0000000000f8'$s$, '23514');
SELECT pg_temp.expect_ok('Report: a report of a whole group names no reported user',
    $s$INSERT INTO message_reports (conversation_id, reporter_id, reason)
       VALUES ('00000000-0000-0000-0000-0000000000f6', '00000000-0000-0000-0000-000000000002', 'other')$s$);
DELETE FROM conversation_members
WHERE conversation_id = '00000000-0000-0000-0000-0000000000f6' AND user_id = '00000000-0000-0000-0000-000000000001';
SELECT pg_temp.expect_true('Owner leaves: the member who joined earliest becomes owner', $q$
    SELECT role = 'owner' FROM conversation_members
    WHERE conversation_id = '00000000-0000-0000-0000-0000000000f6' AND user_id = '00000000-0000-0000-0000-000000000002'$q$);
SELECT pg_temp.expect_true('Owner leaves: an owner_changed message with no actor follows', $q$
    SELECT m.sender_id IS NULL AND m.event_data ->> 'newOwnerId' = '00000000-0000-0000-0000-000000000002'
           AND m.created_at = c.last_message_at
    FROM messages m JOIN conversations c ON c.id = m.conversation_id
    WHERE m.conversation_id = '00000000-0000-0000-0000-0000000000f6' AND m.event = 'owner_changed'
    ORDER BY m.id DESC LIMIT 1$q$);
SELECT pg_temp.expect_true('Owner leaves: the stay ends', $q$
    SELECT left_at IS NOT NULL FROM conversation_memberships
    WHERE conversation_id = '00000000-0000-0000-0000-0000000000f6' AND user_id = '00000000-0000-0000-0000-000000000001'$q$);
DELETE FROM conversation_members WHERE conversation_id = '00000000-0000-0000-0000-0000000000f6';
SELECT pg_temp.expect_true('Last member leaves: the group is closed, and no owner_changed is written', $q$
    SELECT c.closed_at IS NOT NULL AND (SELECT count(*) FROM messages m
                                        WHERE m.conversation_id = c.id AND m.event = 'owner_changed') = 2
    FROM conversations c WHERE c.id = '00000000-0000-0000-0000-0000000000f6'$q$);
SELECT pg_temp.expect_error('Group: closed after it was created',
    $s$UPDATE conversations SET closed_at = created_at - interval '1 second' WHERE id = '00000000-0000-0000-0000-0000000000f6'$s$, '23514');

-- ---------------------------------------------------------------------------
-- Account deletion (FR-2.8, SRS 7 Retention): user 3 owns no project.
-- ---------------------------------------------------------------------------

INSERT INTO sessions (id, user_id, refresh_token_hash, expires_at) VALUES
    ('00000000-0000-0000-0000-0000000000f3', '00000000-0000-0000-0000-000000000003', pg_temp.t('rt-3'), now() + interval '30 days');
INSERT INTO session_retired_tokens (token_hash, session_id) VALUES
    (pg_temp.t('rt-3-old'), '00000000-0000-0000-0000-0000000000f3');
SELECT pg_temp.expect_error('Session: a retired refresh token hash is unique (FR-2.2)',
    $s$INSERT INTO session_retired_tokens (token_hash, session_id)
       VALUES (pg_temp.t('rt-3-old'), '00000000-0000-0000-0000-0000000000f3')$s$, '23505');
INSERT INTO auth_tokens (user_id, purpose, token_hash, expires_at) VALUES
    ('00000000-0000-0000-0000-000000000003', 'password_reset', pg_temp.t('at-3'), now() + interval '1 hour');
INSERT INTO user_interests (user_id, interest_id) SELECT '00000000-0000-0000-0000-000000000003', id FROM research_interests WHERE name LIKE pg_temp.t('%');
INSERT INTO user_skills (user_id, skill_id) SELECT '00000000-0000-0000-0000-000000000003', id FROM skills WHERE name LIKE pg_temp.t('%');
INSERT INTO profile_education (user_id, institution, degree, start_year) VALUES ('00000000-0000-0000-0000-000000000003', 'MIT', 'BSc', 2015);
INSERT INTO profile_contact (user_id, phone) VALUES ('00000000-0000-0000-0000-000000000003', '+8801712345678');
INSERT INTO profile_links (user_id, kind, url) VALUES ('00000000-0000-0000-0000-000000000003', 'github', 'https://github.com/cy');
INSERT INTO profile_experience (user_id, title, organization, employment_type, start_year, start_month, is_current)
    VALUES ('00000000-0000-0000-0000-000000000003', 'Engineer', 'ACME', 'full_time', 2019, 1, true);
INSERT INTO profile_languages (user_id, name, proficiency) VALUES ('00000000-0000-0000-0000-000000000003', 'Welsh', 'elementary');
INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes) VALUES
    ('00000000-0000-0000-0000-000000000003', 'photo', pg_temp.t('photo/cy'),  'image/png', 100),
    ('00000000-0000-0000-0000-000000000003', 'cover', pg_temp.t('cover/cy'),  'image/png', 100);
UPDATE profiles
SET photo_file_id = (SELECT id FROM stored_files WHERE object_key = pg_temp.t('photo/cy')),
    cover_file_id = (SELECT id FROM stored_files WHERE object_key = pg_temp.t('cover/cy'))
WHERE user_id = '00000000-0000-0000-0000-000000000003';
INSERT INTO user_publications (user_id, publication_id, confirmed_at) VALUES
    ('00000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-0000000000c1', now()),
    ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-0000000000c1', now());
INSERT INTO profile_embeddings (user_id, model, content_hash, embedding) VALUES
    ('00000000-0000-0000-0000-000000000003', 'test-model@1', repeat('3', 64), array_fill(0.3::real, ARRAY[768])::vector);
INSERT INTO posts (id, author_id, body) VALUES
    ('00000000-0000-0000-0000-0000000000e3', '00000000-0000-0000-0000-000000000003', 'Cy post');
INSERT INTO post_embeddings (post_id, model, content_hash, embedding) VALUES
    ('00000000-0000-0000-0000-0000000000e3', 'test-model@1', repeat('4', 64), array_fill(0.4::real, ARRAY[768])::vector);
INSERT INTO post_likes (post_id, user_id) VALUES
    ('00000000-0000-0000-0000-0000000000e1', '00000000-0000-0000-0000-000000000003');
INSERT INTO post_comments (post_id, author_id, body) VALUES
    ('00000000-0000-0000-0000-0000000000e1', '00000000-0000-0000-0000-000000000003', 'Cy comment'),
    ('00000000-0000-0000-0000-0000000000e3', '00000000-0000-0000-0000-000000000002', 'Bob on Cy post');
INSERT INTO notifications (user_id, type, actor_id, entity_id) VALUES
    ('00000000-0000-0000-0000-000000000003', 'post_comment',                '00000000-0000-0000-0000-000000000002', gen_random_uuid()),
    ('00000000-0000-0000-0000-000000000002', 'connection_request_received', '00000000-0000-0000-0000-000000000003', gen_random_uuid());
INSERT INTO conversations (id, state, requested_by, user_low, user_high) VALUES
    ('00000000-0000-0000-0000-0000000000f5', 'request', '00000000-0000-0000-0000-000000000003',
     '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000003');
INSERT INTO conversation_members (conversation_id, user_id) VALUES
    ('00000000-0000-0000-0000-0000000000f5', '00000000-0000-0000-0000-000000000001'),
    ('00000000-0000-0000-0000-0000000000f5', '00000000-0000-0000-0000-000000000003');
INSERT INTO messages (conversation_id, sender_id, body, client_id) VALUES
    ('00000000-0000-0000-0000-0000000000f5', '00000000-0000-0000-0000-000000000003', 'may I?', gen_random_uuid());

DELETE FROM users WHERE id = '00000000-0000-0000-0000-000000000003';

SELECT pg_temp.expect_true('Deletion: profile, interests, skills, education, experience, languages, contact info and links removed (FR-2.8)', $q$
    SELECT NOT EXISTS (SELECT 1 FROM profiles        WHERE user_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM user_interests  WHERE user_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM user_skills     WHERE user_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM profile_education WHERE user_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM profile_contact WHERE user_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM profile_links   WHERE user_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM profile_experience WHERE user_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM profile_languages  WHERE user_id = '00000000-0000-0000-0000-000000000003')$q$);
SELECT pg_temp.expect_true('Deletion: sessions, retired refresh tokens and one-time tokens removed', $q$
    SELECT NOT EXISTS (SELECT 1 FROM sessions    WHERE user_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM auth_tokens WHERE user_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM session_retired_tokens WHERE session_id = '00000000-0000-0000-0000-0000000000f3')$q$);
SELECT pg_temp.expect_true('Deletion: posts, comments on them, likes and own comments removed (FR-2.8)', $q$
    SELECT NOT EXISTS (SELECT 1 FROM posts         WHERE author_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM post_comments WHERE post_id   = '00000000-0000-0000-0000-0000000000e3')
       AND NOT EXISTS (SELECT 1 FROM post_comments WHERE author_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM post_likes    WHERE user_id   = '00000000-0000-0000-0000-000000000003')$q$);
SELECT pg_temp.expect_true('Deletion: stored files removed and their objects queued for deletion (FR-2.8, NFR-13)', $q$
    SELECT NOT EXISTS (SELECT 1 FROM stored_files WHERE owner_user_id = '00000000-0000-0000-0000-000000000003')
       AND (SELECT count(*) FROM storage_deletions WHERE object_key IN (pg_temp.t('photo/cy'), pg_temp.t('cover/cy'))) = 2$q$);
SELECT pg_temp.expect_true('Deletion: connections removed (FR-2.8)', $q$
    SELECT NOT EXISTS (SELECT 1 FROM connections
                       WHERE '00000000-0000-0000-0000-000000000003' IN (requester_id, receiver_id))$q$);
SELECT pg_temp.expect_true('Deletion: profile and post embeddings removed (FR-2.8)', $q$
    SELECT NOT EXISTS (SELECT 1 FROM profile_embeddings WHERE user_id = '00000000-0000-0000-0000-000000000003')
       AND NOT EXISTS (SELECT 1 FROM post_embeddings    WHERE post_id = '00000000-0000-0000-0000-0000000000e3')$q$);
SELECT pg_temp.expect_true('Deletion: notifications to and from the user removed', $q$
    SELECT NOT EXISTS (SELECT 1 FROM notifications
                       WHERE '00000000-0000-0000-0000-000000000003' IN (user_id, actor_id))$q$);
SELECT pg_temp.expect_true('Deletion: direct conversations, their members and messages removed (FR-2.8)', $q$
    SELECT NOT EXISTS (SELECT 1 FROM conversations WHERE id = '00000000-0000-0000-0000-0000000000f5')
       AND NOT EXISTS (SELECT 1 FROM conversation_members WHERE conversation_id = '00000000-0000-0000-0000-0000000000f5')
       AND NOT EXISTS (SELECT 1 FROM messages WHERE conversation_id = '00000000-0000-0000-0000-0000000000f5')$q$);
SELECT pg_temp.expect_true('Deletion: the other member keeps their other conversations', $q$
    SELECT EXISTS (SELECT 1 FROM conversations WHERE id = '00000000-0000-0000-0000-0000000000f1')$q$);
SELECT pg_temp.expect_true('Deletion: a publication shared with a co-author stays for the co-author', $q$
    SELECT EXISTS (SELECT 1 FROM user_publications
                   WHERE publication_id = '00000000-0000-0000-0000-0000000000c1'
                     AND user_id = '00000000-0000-0000-0000-000000000001')$q$);
SELECT pg_temp.expect_true('Deletion: a shared publication the user added no longer names them', $q$
    SELECT created_by IS NULL FROM publications WHERE id = '00000000-0000-0000-0000-0000000000c1'$q$);

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
SELECT pg_temp.expect_true('Project deletion: its figures'' stored files go and their objects are queued', $q$
    SELECT NOT EXISTS (SELECT 1 FROM stored_files WHERE project_id = '00000000-0000-0000-0000-0000000000a1')
       AND EXISTS (SELECT 1 FROM storage_deletions WHERE object_key = pg_temp.t('projects/a1/f2'))$q$);
SELECT pg_temp.expect_true('Project deletion: its pending figure uploads go and their objects are queued', $q$
    SELECT NOT EXISTS (SELECT 1 FROM project_figure_uploads WHERE project_id = '00000000-0000-0000-0000-0000000000a1')
       AND EXISTS (SELECT 1 FROM storage_deletions
                   WHERE object_key = 'projects/00000000-0000-0000-0000-0000000000a1/uploads/00000000-0000-0000-0000-0000000000c6')$q$);
SELECT pg_temp.expect_true('Project deletion: a post that linked to it stays, without the link', $q$
    SELECT project_id IS NULL FROM posts WHERE id = '00000000-0000-0000-0000-0000000000e4'$q$);

-- Blocks and reports (FR-16.9, NFR-13). Users 1 and 2 have the conversation above.
INSERT INTO blocks (blocker_id, blocked_id) VALUES
    ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002');
SELECT pg_temp.expect_ok('Block: each side may block the other',
    $s$INSERT INTO blocks (blocker_id, blocked_id) VALUES
        ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001')$s$);
SELECT pg_temp.expect_error('Block: one per direction',
    $s$INSERT INTO blocks (blocker_id, blocked_id) VALUES
        ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002')$s$, '23505');
SELECT pg_temp.expect_error('Block: not oneself',
    $s$INSERT INTO blocks (blocker_id, blocked_id) VALUES
        ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001')$s$, '23514');

INSERT INTO message_reports (id, conversation_id, reporter_id, reported_id, reason) VALUES
    ('00000000-0000-0000-0000-0000000000f1', (SELECT id FROM conversations ORDER BY created_at LIMIT 1),
     '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002', 'spam');
SELECT pg_temp.expect_error('Report: a known reason',
    $s$UPDATE message_reports SET reason = 'rude'$s$, '23514');
SELECT pg_temp.expect_error('Report: details of at most 1,000 characters',
    $s$UPDATE message_reports SET details = repeat('x', 1001)$s$, '23514');
SELECT pg_temp.expect_error('Report: not of oneself',
    $s$UPDATE message_reports SET reported_id = reporter_id$s$, '23514');
SELECT pg_temp.expect_error('Report: the message must be in the conversation',
    $s$UPDATE message_reports SET message_id = '00000000-0000-0000-0000-00000000dead'$s$, '23503');
SELECT pg_temp.expect_error('Report: resolved with an outcome',
    $s$UPDATE message_reports SET resolved_at = now()$s$, '23514');
SELECT pg_temp.expect_error('Report: an outcome only when resolved',
    $s$UPDATE message_reports SET outcome = 'dismissed'$s$, '23514');
SELECT pg_temp.expect_error('Report: a known outcome',
    $s$UPDATE message_reports SET resolved_at = now(), outcome = 'closed'$s$, '23514');
SELECT pg_temp.expect_error('Report: a note only when resolved',
    $s$UPDATE message_reports SET resolution_note = 'early'$s$, '23514');
SELECT pg_temp.expect_ok('Report: resolved with an outcome and a note',
    $s$UPDATE message_reports SET resolved_at = now(), outcome = 'dismissed', resolution_note = 'ok'$s$);

-- Moderation (FR-15.5 to FR-15.8).
INSERT INTO users (id, email, password_hash)
VALUES ('00000000-0000-4000-8000-0000000000a1', pg_temp.t('moderated@example.org'), 'hash');
INSERT INTO account_warnings (id, user_id, reason)
VALUES ('00000000-0000-4000-8000-0000000000a2', '00000000-0000-4000-8000-0000000000a1', 'A reason of ten or more');
INSERT INTO account_restrictions (id, user_id, kind, starts_at, ends_at)
VALUES ('00000000-0000-4000-8000-0000000000a3', '00000000-0000-4000-8000-0000000000a1', 'messaging', now(), now() + interval '1 day');

SELECT pg_temp.expect_error('Notification: a warning has no actor',
    $s$INSERT INTO notifications (user_id, actor_id, type, entity_id)
       VALUES ('00000000-0000-4000-8000-0000000000a1', '00000000-0000-4000-8000-0000000000a1', 'account_warning', gen_random_uuid())$s$, '23514');
SELECT pg_temp.expect_error('Notification: a connection type needs its actor',
    $s$INSERT INTO notifications (user_id, type, entity_id)
       VALUES ('00000000-0000-4000-8000-0000000000a1', 'connection_request_received', gen_random_uuid())$s$, '23514');
SELECT pg_temp.expect_ok('Notification: a warning without an actor',
    $s$INSERT INTO notifications (user_id, type, entity_id)
       VALUES ('00000000-0000-4000-8000-0000000000a1', 'account_warning', gen_random_uuid())$s$);

SELECT pg_temp.expect_error('User: an end time needs a suspension',
    $s$UPDATE users SET suspended_until = now() WHERE id = '00000000-0000-4000-8000-0000000000a1'$s$, '23514');
SELECT pg_temp.expect_error('User: a reason needs a suspension',
    $s$UPDATE users SET suspension_reason = 'A reason of ten or more' WHERE id = '00000000-0000-4000-8000-0000000000a1'$s$, '23514');
SELECT pg_temp.expect_error('User: a suspension ends after it starts',
    $s$UPDATE users SET suspended_at = now(), suspended_until = now() - interval '1 day' WHERE id = '00000000-0000-4000-8000-0000000000a1'$s$, '23514');
SELECT pg_temp.expect_error('User: a suspension reason of at least 10 characters',
    $s$UPDATE users SET suspended_at = now(), suspension_reason = 'short' WHERE id = '00000000-0000-4000-8000-0000000000a1'$s$, '23514');

SELECT pg_temp.expect_error('Warning: a reason of at least 10 characters',
    $s$UPDATE account_warnings SET reason = 'short'$s$, '23514');
SELECT pg_temp.expect_error('Warning: a reason of at most 1,000 characters',
    $s$UPDATE account_warnings SET reason = repeat('x', 1001)$s$, '23514');

SELECT pg_temp.expect_error('Restriction: a known kind',
    $s$UPDATE account_restrictions SET kind = 'posting'$s$, '23514');
SELECT pg_temp.expect_error('Restriction: ends after it starts',
    $s$UPDATE account_restrictions SET ends_at = starts_at$s$, '23514');
SELECT pg_temp.expect_error('Restriction: lifted by someone only when lifted',
    $s$UPDATE account_restrictions SET lifted_by = user_id$s$, '23514');

SELECT pg_temp.expect_error('Moderation action: a known action',
    $s$INSERT INTO moderation_actions (user_id, action, reason)
       VALUES ('00000000-0000-4000-8000-0000000000a1', 'banned', 'A reason of ten or more')$s$, '23514');
SELECT pg_temp.expect_error('Moderation action: a warning names its warning',
    $s$INSERT INTO moderation_actions (user_id, action, reason)
       VALUES ('00000000-0000-4000-8000-0000000000a1', 'warning', 'A reason of ten or more')$s$, '23514');
SELECT pg_temp.expect_error('Moderation action: a restriction names its restriction',
    $s$INSERT INTO moderation_actions (user_id, action, reason, ends_at)
       VALUES ('00000000-0000-4000-8000-0000000000a1', 'restriction', 'A reason of ten or more', now())$s$, '23514');
SELECT pg_temp.expect_error('Moderation action: a restriction has an end',
    $s$INSERT INTO moderation_actions (user_id, action, reason, restriction_id)
       VALUES ('00000000-0000-4000-8000-0000000000a1', 'restriction', 'A reason of ten or more', '00000000-0000-4000-8000-0000000000a3')$s$, '23514');
SELECT pg_temp.expect_error('Moderation action: a restoration resolves no report',
    $s$INSERT INTO moderation_actions (user_id, action, reason, report_id)
       VALUES ('00000000-0000-4000-8000-0000000000a1', 'restoration', 'A reason of ten or more', gen_random_uuid())$s$, '23514');
SELECT pg_temp.expect_error('Moderation action: a note of at most 1,000 characters',
    $s$INSERT INTO moderation_actions (user_id, action, reason, note)
       VALUES ('00000000-0000-4000-8000-0000000000a1', 'suspension', 'A reason of ten or more', repeat('x', 1001))$s$, '23514');
SELECT pg_temp.expect_ok('Moderation action: a permanent suspension, its report outliving it',
    $s$INSERT INTO moderation_actions (user_id, action, reason, report_id)
       VALUES ('00000000-0000-4000-8000-0000000000a1', 'suspension', 'A reason of ten or more', gen_random_uuid())$s$);

SELECT pg_temp.expect_error('Digest state: sent time and watermark go together',
    $s$INSERT INTO message_digest_state (user_id, last_digest_at)
       VALUES ('00000000-0000-4000-8000-0000000000a1', now())$s$, '23514');
SELECT pg_temp.expect_error('Digest state: the watermark is not after the send',
    $s$INSERT INTO message_digest_state (user_id, last_digest_at, covered_until)
       VALUES ('00000000-0000-4000-8000-0000000000a1', now() - interval '1 day', now())$s$, '23514');
SELECT pg_temp.expect_ok('Digest state: a user never sent a digest',
    $s$INSERT INTO message_digest_state (user_id) VALUES ('00000000-0000-4000-8000-0000000000a1')$s$);
SELECT pg_temp.expect_error('Digest: at least one unread message',
    $s$INSERT INTO message_digests (user_id, sent_at, covered_until, unread_count, conversation_count, request_count)
       VALUES ('00000000-0000-4000-8000-0000000000a1', now(), now() - interval '1 day', 0, 1, 0)$s$, '23514');
SELECT pg_temp.expect_error('Digest: no more conversations than messages',
    $s$INSERT INTO message_digests (user_id, sent_at, covered_until, unread_count, conversation_count, request_count)
       VALUES ('00000000-0000-4000-8000-0000000000a1', now(), now() - interval '1 day', 2, 3, 0)$s$, '23514');
SELECT pg_temp.expect_error('Digest: covers nothing after its send',
    $s$INSERT INTO message_digests (user_id, sent_at, covered_until, unread_count, conversation_count, request_count)
       VALUES ('00000000-0000-4000-8000-0000000000a1', now(), now() + interval '1 hour', 2, 1, 0)$s$, '23514');
SELECT pg_temp.expect_error('Digest: no negative request count',
    $s$INSERT INTO message_digests (user_id, sent_at, covered_until, unread_count, conversation_count, request_count)
       VALUES ('00000000-0000-4000-8000-0000000000a1', now(), now() - interval '1 day', 2, 1, -1)$s$, '23514');
SELECT pg_temp.expect_ok('Digest: a valid record',
    $s$INSERT INTO message_digests (user_id, sent_at, covered_until, unread_count, conversation_count, request_count)
       VALUES ('00000000-0000-4000-8000-0000000000a1', now(), now() - interval '1 day', 2, 1, 0)$s$);

ROLLBACK;

\echo 'All database constraint tests passed.'
