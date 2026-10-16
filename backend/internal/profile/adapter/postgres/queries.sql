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
