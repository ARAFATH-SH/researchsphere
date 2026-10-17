-- name: FindAccount :one
-- The user and, through the left join, the profile row when there is one.
SELECT u.id, u.email_verified_at, u.suspended_at,
       p.user_id AS profile_user_id, p.full_name, p.headline, p.bio, p.institution,
       p.department, p.job_title, p.country
FROM users u
LEFT JOIN profiles p ON p.user_id = u.id
WHERE u.id = @id;

-- name: UpsertProfile :one
-- Creates the profile row or replaces its edited fields (FR-4.1); other columns stay.
INSERT INTO profiles (user_id, full_name, headline, bio, institution, department, job_title, country)
VALUES (@user_id, @full_name, @headline, @bio, @institution, @department, @job_title, @country)
ON CONFLICT (user_id) DO UPDATE SET
    full_name   = EXCLUDED.full_name,
    headline    = EXCLUDED.headline,
    bio         = EXCLUDED.bio,
    institution = EXCLUDED.institution,
    department  = EXCLUDED.department,
    job_title   = EXCLUDED.job_title,
    country     = EXCLUDED.country
RETURNING user_id, full_name, headline, bio, institution, department, job_title, country;

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
SELECT count(*) FROM education WHERE user_id = @user_id;

-- name: InsertEducation :one
INSERT INTO education (user_id, institution, degree, field_of_study, start_year, end_year)
VALUES (@user_id, @institution, @degree, @field_of_study, @start_year, @end_year)
RETURNING id, user_id, institution, degree, field_of_study, start_year, end_year;

-- name: FindUserEducationForUpdate :one
-- Locks the entry until the transaction ends, so two changes to it cannot both pass the
-- end-year check against an old start year.
SELECT id, user_id, institution, degree, field_of_study, start_year, end_year
FROM education
WHERE id = @id AND user_id = @user_id
FOR UPDATE;

-- name: UpdateUserEducation :one
UPDATE education SET
    institution    = @institution,
    degree         = @degree,
    field_of_study = @field_of_study,
    start_year     = @start_year,
    end_year       = @end_year
WHERE id = @id AND user_id = @user_id
RETURNING id, user_id, institution, degree, field_of_study, start_year, end_year;

-- name: DeleteUserEducation :execrows
DELETE FROM education WHERE id = @id AND user_id = @user_id;

-- name: ListUserEducation :many
-- Current entries (no end year) first, then the latest end year, then the latest start
-- year; creation time and id make the order stable.
SELECT id, user_id, institution, degree, field_of_study, start_year, end_year
FROM education
WHERE user_id = @user_id
ORDER BY end_year DESC NULLS FIRST, start_year DESC, created_at DESC, id;
