-- name: FindAccount :one
-- The user and, through the left join, the profile row when there is one.
SELECT u.id, u.email_verified_at, u.suspended_at,
       p.user_id AS profile_user_id, p.full_name, p.headline, p.bio, p.institution,
       p.department, p.job_title, p.country, p.city, p.orcid_id, p.photo_file_id, p.cover_file_id
FROM users u
LEFT JOIN profiles p ON p.user_id = u.id
WHERE u.id = @id;

-- name: UpsertProfile :one
-- Creates the profile row or replaces its edited fields (FR-4.1); other columns stay.
INSERT INTO profiles (user_id, full_name, headline, bio, institution, department, job_title, country, city)
VALUES (@user_id, @full_name, @headline, @bio, @institution, @department, @job_title, @country, @city)
ON CONFLICT (user_id) DO UPDATE SET
    full_name   = EXCLUDED.full_name,
    headline    = EXCLUDED.headline,
    bio         = EXCLUDED.bio,
    institution = EXCLUDED.institution,
    department  = EXCLUDED.department,
    job_title   = EXCLUDED.job_title,
    country     = EXCLUDED.country,
    city        = EXCLUDED.city
RETURNING user_id, full_name, headline, bio, institution, department, job_title, country, city,
          orcid_id, photo_file_id, cover_file_id;

-- name: SetOrcidID :execrows
-- Saves or clears the user's ORCID iD (FR-5.3). The unique constraint refuses an iD
-- another profile has.
UPDATE profiles SET orcid_id = @orcid_id WHERE user_id = @user_id;

-- name: LockUser :one
-- Taken before a tag is added, so two requests of one user cannot both pass the tag
-- limit. NO KEY UPDATE does not block the key-share lock the new link takes on the user.
SELECT id FROM users WHERE id = @id FOR NO KEY UPDATE;

-- Tags: research_interests and user_interests (FR-4.2, FR-4.3). The name column is citext, so = and the
-- unique constraint ignore letter case.

-- name: InsertInterestIfMissing :exec
-- Creates the tag unless one with this name, ignoring letter case, exists. Another
-- transaction inserting the same name makes this wait, then do nothing; the select that
-- follows (FindInterestByName, a new statement) then sees its row.
INSERT INTO research_interests (name) VALUES (@name) ON CONFLICT (name) DO NOTHING;

-- name: FindInterestByName :one
SELECT id, name FROM research_interests WHERE name = @name;

-- name: UserHasInterest :one
SELECT EXISTS (SELECT 1 FROM user_interests WHERE user_id = @user_id AND interest_id = @tag_id);

-- name: CountUserInterests :one
SELECT count(*) FROM user_interests WHERE user_id = @user_id;

-- name: LinkUserInterest :exec
INSERT INTO user_interests (user_id, interest_id) VALUES (@user_id, @tag_id) ON CONFLICT DO NOTHING;

-- name: UnlinkUserInterest :execrows
DELETE FROM user_interests WHERE user_id = @user_id AND interest_id = @tag_id;

-- name: ListUserInterests :many
-- citext sorts ignoring letter case; the id breaks ties.
SELECT t.id, t.name
FROM research_interests t
JOIN user_interests l ON l.interest_id = t.id
WHERE l.user_id = @user_id
ORDER BY t.name, t.id;

-- name: SuggestInterests :many
-- strpos and starts_with on lower-cased text, not LIKE, so % and _ in q match only
-- themselves. Names that start with q first, then the most used, then by name.
SELECT t.id, t.name
FROM research_interests t
WHERE strpos(lower(t.name::text), lower(@q::text)) > 0
ORDER BY starts_with(lower(t.name::text), lower(@q::text)) DESC,
         (SELECT count(*) FROM user_interests l WHERE l.interest_id = t.id) DESC,
         t.name, t.id
LIMIT @max_results;

-- Tags: skills and user_skills (FR-4.2, FR-4.3). The name column is citext, so = and the
-- unique constraint ignore letter case.

-- name: InsertSkillIfMissing :exec
-- Creates the tag unless one with this name, ignoring letter case, exists. Another
-- transaction inserting the same name makes this wait, then do nothing; the select that
-- follows (FindSkillByName, a new statement) then sees its row.
INSERT INTO skills (name) VALUES (@name) ON CONFLICT (name) DO NOTHING;

-- name: FindSkillByName :one
SELECT id, name FROM skills WHERE name = @name;

-- name: UserHasSkill :one
SELECT EXISTS (SELECT 1 FROM user_skills WHERE user_id = @user_id AND skill_id = @tag_id);

-- name: CountUserSkills :one
SELECT count(*) FROM user_skills WHERE user_id = @user_id;

-- name: LinkUserSkill :exec
INSERT INTO user_skills (user_id, skill_id) VALUES (@user_id, @tag_id) ON CONFLICT DO NOTHING;

-- name: UnlinkUserSkill :execrows
DELETE FROM user_skills WHERE user_id = @user_id AND skill_id = @tag_id;

-- name: ListUserSkills :many
-- citext sorts ignoring letter case; the id breaks ties.
SELECT t.id, t.name
FROM skills t
JOIN user_skills l ON l.skill_id = t.id
WHERE l.user_id = @user_id
ORDER BY t.name, t.id;

-- name: SuggestSkills :many
-- strpos and starts_with on lower-cased text, not LIKE, so % and _ in q match only
-- themselves. Names that start with q first, then the most used, then by name.
SELECT t.id, t.name
FROM skills t
WHERE strpos(lower(t.name::text), lower(@q::text)) > 0
ORDER BY starts_with(lower(t.name::text), lower(@q::text)) DESC,
         (SELECT count(*) FROM user_skills l WHERE l.skill_id = t.id) DESC,
         t.name, t.id
LIMIT @max_results;

-- Education entries (FR-4.4). Every query names the owner in its WHERE clause, so an entry
-- of another user is never read, changed or deleted: it is simply not found.

-- name: CountUserEducation :one
SELECT count(*) FROM profile_education WHERE user_id = @user_id;

-- name: InsertEducation :one
INSERT INTO profile_education (user_id, institution, degree, field_of_study, start_year, end_year, grade, description)
VALUES (@user_id, @institution, @degree, @field_of_study, @start_year, @end_year, @grade, @description)
RETURNING id, user_id, institution, degree, field_of_study, start_year, end_year, grade, description;

-- name: FindUserEducationForUpdate :one
-- Locks the entry until the transaction ends, so two changes to it cannot both pass the
-- end-year check against an old start year.
SELECT id, user_id, institution, degree, field_of_study, start_year, end_year, grade, description
FROM profile_education
WHERE id = @id AND user_id = @user_id
FOR UPDATE;

-- name: UpdateUserEducation :one
UPDATE profile_education SET
    institution    = @institution,
    degree         = @degree,
    field_of_study = @field_of_study,
    start_year     = @start_year,
    end_year       = @end_year,
    grade          = @grade,
    description    = @description
WHERE id = @id AND user_id = @user_id
RETURNING id, user_id, institution, degree, field_of_study, start_year, end_year, grade, description;

-- name: DeleteUserEducation :execrows
DELETE FROM profile_education WHERE id = @id AND user_id = @user_id;

-- name: ListUserEducation :many
-- Current entries (no end year) first, then the latest end year, then the latest start
-- year; creation time and id make the order stable.
SELECT id, user_id, institution, degree, field_of_study, start_year, end_year, grade, description
FROM profile_education
WHERE user_id = @user_id
ORDER BY end_year DESC NULLS FIRST, start_year DESC, created_at DESC, id;

-- Work experience entries (FR-4.8). Every query names the owner in its WHERE clause, so
-- an entry of another user is never read, changed or deleted: it is simply not found.

-- name: CountUserExperience :one
SELECT count(*) FROM profile_experience WHERE user_id = @user_id;

-- name: InsertExperience :one
INSERT INTO profile_experience (user_id, title, organization, employment_type, location, location_type,
                                start_year, start_month, end_year, end_month, is_current, description)
VALUES (@user_id, @title, @organization, @employment_type, @location, @location_type,
        @start_year, @start_month, @end_year, @end_month, @is_current, @description)
RETURNING id, user_id, title, organization, employment_type, location, location_type,
       start_year, start_month, end_year, end_month, is_current, description;

-- name: FindUserExperienceForUpdate :one
-- Locks the entry until the transaction ends, so two changes to it cannot both pass the
-- date checks against old values.
SELECT id, user_id, title, organization, employment_type, location, location_type,
       start_year, start_month, end_year, end_month, is_current, description
FROM profile_experience
WHERE id = @id AND user_id = @user_id
FOR UPDATE;

-- name: UpdateUserExperience :one
UPDATE profile_experience SET
    title           = @title,
    organization    = @organization,
    employment_type = @employment_type,
    location        = @location,
    location_type   = @location_type,
    start_year      = @start_year,
    start_month     = @start_month,
    end_year        = @end_year,
    end_month       = @end_month,
    is_current      = @is_current,
    description     = @description
WHERE id = @id AND user_id = @user_id
RETURNING id, user_id, title, organization, employment_type, location, location_type,
       start_year, start_month, end_year, end_month, is_current, description;

-- name: DeleteUserExperience :execrows
DELETE FROM profile_experience WHERE id = @id AND user_id = @user_id;

-- name: ListUserExperience :many
-- Current roles first, then the latest start, then the oldest entry; id makes the order
-- stable.
SELECT id, user_id, title, organization, employment_type, location, location_type,
       start_year, start_month, end_year, end_month, is_current, description
FROM profile_experience
WHERE user_id = @user_id
ORDER BY is_current DESC, start_year DESC, start_month DESC, created_at, id;

-- Languages (FR-4.9). Every query names the owner in its WHERE clause, so a language of
-- another user is never read, changed or deleted: it is simply not found.

-- name: CountUserLanguages :one
SELECT count(*) FROM profile_languages WHERE user_id = @user_id;

-- name: LanguageNameTaken :one
-- Compares names as the unique index profile_languages_user_name_key does. A NULL
-- except_id excepts no language.
SELECT EXISTS (
    SELECT 1 FROM profile_languages
    WHERE user_id = @user_id AND lower(name) = lower(@name::text)
      AND id IS DISTINCT FROM sqlc.narg(except_id)::uuid
);

-- name: InsertLanguage :one
INSERT INTO profile_languages (user_id, name, proficiency)
VALUES (@user_id, @name, @proficiency)
RETURNING id, user_id, name, proficiency;

-- name: FindUserLanguageForUpdate :one
-- Locks the language until the transaction ends.
SELECT id, user_id, name, proficiency
FROM profile_languages
WHERE id = @id AND user_id = @user_id
FOR UPDATE;

-- name: UpdateUserLanguage :one
UPDATE profile_languages SET
    name        = @name,
    proficiency = @proficiency
WHERE id = @id AND user_id = @user_id
RETURNING id, user_id, name, proficiency;

-- name: DeleteUserLanguage :execrows
DELETE FROM profile_languages WHERE id = @id AND user_id = @user_id;

-- name: ListUserLanguages :many
-- Oldest first; the usecase then puts the highest proficiency first. id makes the order
-- stable.
SELECT id, user_id, name, proficiency
FROM profile_languages
WHERE user_id = @user_id
ORDER BY created_at, id;

-- Contact info and links (FR-4.7). Every query names the owner in its WHERE clause, so a
-- link of another user is never read or deleted: it is simply not found.

-- name: FindContact :one
-- The user's email and, through the left join, the contact row when there is one. A
-- user without a row gets every visibility only_me, the column default.
SELECT u.email::text AS email, c.phone, c.birth_month, c.birth_day,
       coalesce(c.email_visibility, 'only_me')::text    AS email_visibility,
       coalesce(c.phone_visibility, 'only_me')::text    AS phone_visibility,
       coalesce(c.birthday_visibility, 'only_me')::text AS birthday_visibility
FROM users u
LEFT JOIN profile_contact c ON c.user_id = u.id
WHERE u.id = @user_id;

-- name: UpsertContact :exec
INSERT INTO profile_contact (user_id, phone, birth_month, birth_day, email_visibility, phone_visibility, birthday_visibility)
VALUES (@user_id, @phone, @birth_month, @birth_day, @email_visibility, @phone_visibility, @birthday_visibility)
ON CONFLICT (user_id) DO UPDATE SET
    phone               = EXCLUDED.phone,
    birth_month         = EXCLUDED.birth_month,
    birth_day           = EXCLUDED.birth_day,
    email_visibility    = EXCLUDED.email_visibility,
    phone_visibility    = EXCLUDED.phone_visibility,
    birthday_visibility = EXCLUDED.birthday_visibility;

-- name: ListUserLinks :many
SELECT id, user_id, kind, url FROM profile_links WHERE user_id = @user_id ORDER BY created_at, id;

-- name: CountUserLinks :one
SELECT count(*) FROM profile_links WHERE user_id = @user_id;

-- name: InsertLink :one
INSERT INTO profile_links (user_id, kind, url) VALUES (@user_id, @kind, @url)
RETURNING id, user_id, kind, url;

-- name: DeleteUserLink :execrows
DELETE FROM profile_links WHERE id = @id AND user_id = @user_id;

-- Publications (FR-5.1, FR-5.4, FR-5.5, FR-5.7). A publication is stored once and linked to
-- each author in user_publications; a link is confirmed when confirmed_at is set. Every
-- query that reads or changes a publication for a user joins the user's link, so a
-- publication the user is not linked to is simply not found.

-- name: CountUserPublicationLinks :one
SELECT count(*) FROM user_publications WHERE user_id = @user_id;

-- name: FindPublicationByDOI :one
-- KEY SHARE keeps the record from being deleted (by its last author leaving, or account
-- deletion) until the transaction ends, without blocking an edit of its fields.
SELECT id, doi, title, publication_year, venue_name, abstract, url, source, created_by, created_at, updated_at
FROM publications
WHERE doi = @doi
FOR KEY SHARE;

-- name: InsertPublication :one
-- A DOI another record has, even one stored at this moment by another transaction, inserts
-- nothing and returns no row; the caller then links to that record (FR-5.4).
INSERT INTO publications (doi, title, publication_year, venue_name, abstract, url, source, created_by)
VALUES (@doi, @title, @publication_year, @venue_name, @abstract, @url, @source, @created_by)
ON CONFLICT (doi) DO NOTHING
RETURNING id, doi, title, publication_year, venue_name, abstract, url, source, created_by, created_at, updated_at;

-- name: FindPublicationLink :one
SELECT confirmed_at IS NOT NULL AS confirmed
FROM user_publications
WHERE user_id = @user_id AND publication_id = @publication_id;

-- name: InsertConfirmedPublicationLink :exec
INSERT INTO user_publications (user_id, publication_id, confirmed_at)
VALUES (@user_id, @publication_id, now());

-- name: ConfirmPublicationLink :exec
UPDATE user_publications SET confirmed_at = now()
WHERE user_id = @user_id AND publication_id = @publication_id AND confirmed_at IS NULL;

-- name: FindUserPublicationForUpdate :one
-- Locks the record until the transaction ends, so an edit, a removal and account deletion
-- of a co-author run one after the other.
SELECT p.id, p.doi, p.title, p.publication_year, p.venue_name, p.abstract, p.url, p.source,
       p.created_by, p.created_at, p.updated_at
FROM publications p
JOIN user_publications up ON up.publication_id = p.id
WHERE p.id = @id AND up.user_id = @user_id
FOR UPDATE OF p;

-- name: CountPublicationAuthors :one
SELECT count(*) FROM user_publications WHERE publication_id = @publication_id;

-- name: UpdatePublication :one
UPDATE publications SET
    doi              = @doi,
    title            = @title,
    publication_year = @publication_year,
    venue_name       = @venue_name,
    abstract         = @abstract,
    url              = @url
WHERE id = @id
RETURNING id, doi, title, publication_year, venue_name, abstract, url, source, created_by, created_at, updated_at;

-- name: DeletePublicationLink :execrows
DELETE FROM user_publications WHERE user_id = @user_id AND publication_id = @publication_id;

-- name: DeletePublicationIfOrphaned :exec
DELETE FROM publications p
WHERE p.id = @id
  AND NOT EXISTS (SELECT 1 FROM user_publications up WHERE up.publication_id = p.id);

-- name: ListConfirmedUserPublications :many
-- At most a few hundred rows; the usecase sorts and pages them.
SELECT p.id, p.doi, p.title, p.publication_year, p.venue_name, p.abstract, p.url, p.source,
       p.created_by, p.created_at, p.updated_at, up.created_at AS added_at
FROM publications p
JOIN user_publications up ON up.publication_id = p.id
WHERE up.user_id = @user_id AND up.confirmed_at IS NOT NULL;

-- name: MatchPublicationsByDOI :many
-- The stored publications with one of the DOIs, and whether the user has confirmed each
-- (FR-5.3, FR-5.4). linked is false when the user has no link.
SELECT p.id, p.doi, (up.user_id IS NOT NULL)::boolean AS linked, coalesce(up.confirmed_at IS NOT NULL, false)::boolean AS confirmed
FROM publications p
LEFT JOIN user_publications up ON up.publication_id = p.id AND up.user_id = @user_id
WHERE p.doi = ANY(@dois::text[]);

-- name: CountConfirmedUserPublications :one
SELECT count(*) FROM user_publications WHERE user_id = @user_id AND confirmed_at IS NOT NULL;
