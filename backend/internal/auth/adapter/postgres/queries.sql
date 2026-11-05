-- name: CreateUser :one
INSERT INTO users (email, password_hash)
VALUES (@email, @password_hash)
RETURNING id;

-- name: CreateProfile :exec
INSERT INTO profiles (user_id, full_name)
VALUES (@user_id, @full_name);

-- name: FindUserByEmail :one
SELECT u.id, u.email, u.email_verified_at, u.suspended_at, u.suspended_until, p.full_name
FROM users u
JOIN profiles p ON p.user_id = u.id
WHERE u.email = @email;

-- name: CreateToken :exec
INSERT INTO auth_tokens (user_id, purpose, token_hash, expires_at)
VALUES (@user_id, @purpose, @token_hash, @expires_at);

-- name: DeleteUnusedTokens :exec
DELETE FROM auth_tokens
WHERE user_id = @user_id AND purpose = @purpose AND used_at IS NULL;

-- name: FindTokenForUpdate :one
SELECT t.id, t.user_id, t.expires_at, t.used_at, u.suspended_at, u.suspended_until
FROM auth_tokens t
JOIN users u ON u.id = t.user_id
WHERE t.token_hash = @token_hash AND t.purpose = @purpose
FOR UPDATE OF t;

-- name: MarkTokenUsed :exec
UPDATE auth_tokens SET used_at = @used_at WHERE id = @id;

-- name: MarkEmailVerified :exec
UPDATE users SET email_verified_at = coalesce(email_verified_at, @verified_at)
WHERE id = @id;

-- name: FindCredentialsByEmail :one
SELECT u.id, u.email, u.password_hash, u.email_verified_at, u.suspended_at, u.suspended_until, u.suspension_reason, u.role, p.full_name
FROM users u
JOIN profiles p ON p.user_id = u.id
WHERE u.email = @email;

-- name: FindUserByID :one
SELECT u.id, u.email, u.email_verified_at, u.suspended_at, u.suspended_until, u.role, p.full_name
FROM users u
JOIN profiles p ON p.user_id = u.id
WHERE u.id = @id;

-- name: CreateSession :exec
INSERT INTO sessions (user_id, refresh_token_hash, expires_at)
VALUES (@user_id, @refresh_token_hash, @expires_at);

-- name: FindSessionForUpdate :one
SELECT s.id, s.user_id, s.expires_at, s.revoked_at, u.suspended_at, u.suspended_until, u.role
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.refresh_token_hash = @refresh_token_hash
FOR UPDATE OF s;

-- name: FindSessionByRetiredTokenForUpdate :one
SELECT s.id, s.user_id, s.expires_at, s.revoked_at
FROM sessions s
JOIN session_retired_tokens r ON r.session_id = s.id
WHERE r.token_hash = @token_hash
FOR UPDATE OF s;

-- name: UpdateSessionTokenHash :execrows
UPDATE sessions SET refresh_token_hash = @new_hash
WHERE id = @id AND refresh_token_hash = @old_hash;

-- name: InsertRetiredToken :exec
INSERT INTO session_retired_tokens (token_hash, session_id, retired_at)
VALUES (@token_hash, @session_id, @retired_at);

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = coalesce(revoked_at, @revoked_at)
WHERE id = @id;

-- name: UpdatePasswordHash :exec
UPDATE users SET password_hash = @password_hash
WHERE id = @id;

-- name: RevokeUserSessions :exec
UPDATE sessions SET revoked_at = @revoked_at
WHERE user_id = @user_id AND revoked_at IS NULL;

-- name: RevokeSessionByTokenHash :one
UPDATE sessions SET revoked_at = @revoked_at
WHERE refresh_token_hash = @refresh_token_hash AND revoked_at IS NULL
RETURNING id, user_id, expires_at, revoked_at;

-- name: FindCredentialsByID :one
SELECT u.id, u.email, u.password_hash, u.email_verified_at, u.suspended_at, u.suspended_until, p.full_name
FROM users u
JOIN profiles p ON p.user_id = u.id
WHERE u.id = @id;

-- name: RevokeOtherUserSessions :exec
UPDATE sessions SET revoked_at = @revoked_at
WHERE user_id = @user_id AND id <> @keep_id AND revoked_at IS NULL;

-- Account deletion (FR-2.8, FR-2.9). Each step is its own statement: in READ COMMITTED a
-- statement sees rows committed while an earlier one waited for its locks.

-- name: LockOwnedProjects :exec
-- Locks the projects the user owns. A new member's row takes a key-share lock on its
-- project, so nobody can join them until the transaction ends.
SELECT p.id
FROM projects p
JOIN project_members m ON m.project_id = p.id
WHERE m.user_id = @user_id AND m.role = 'owner'
FOR UPDATE OF p;

-- name: FindSharedOwnedProjects :many
SELECT p.id, p.title
FROM projects p
JOIN project_members m ON m.project_id = p.id
WHERE m.user_id = @user_id AND m.role = 'owner'
  AND EXISTS (SELECT 1 FROM project_members o WHERE o.project_id = p.id AND o.user_id <> @user_id)
ORDER BY p.title, p.id;

-- name: DeleteSoloProjects :execrows
DELETE FROM projects p
WHERE p.id IN (SELECT m.project_id FROM project_members m WHERE m.user_id = @user_id AND m.role = 'owner')
  AND NOT EXISTS (SELECT 1 FROM project_members o WHERE o.project_id = p.id AND o.user_id <> @user_id);

-- name: LockUserPublications :many
-- Locks the user's publications, so no co-author can link one until the transaction ends.
SELECT p.id
FROM publications p
JOIN user_publications up ON up.publication_id = p.id
WHERE up.user_id = @user_id
FOR UPDATE OF p;

-- name: DeleteUserPublicationLinks :exec
DELETE FROM user_publications WHERE user_id = @user_id;

-- name: DeleteAuthorlessPublications :execrows
DELETE FROM publications p
WHERE p.id = ANY(@ids::uuid[])
  AND NOT EXISTS (SELECT 1 FROM user_publications up WHERE up.publication_id = p.id);

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = @id;

-- Administration (FR-15.3).

-- name: ListUsers :many
-- One page of users, newest first; the id breaks ties so pages never overlap.
SELECT u.id, u.email, u.role, u.email_verified_at, u.suspended_at, u.suspended_until, u.created_at, p.full_name
FROM users u
JOIN profiles p ON p.user_id = u.id
ORDER BY u.created_at DESC, u.id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: LockUser :one
-- Moderation (FR-15.10): held until the action's transaction ends, so an action and a send that
-- checks the actor run one after the other.
SELECT u.id, u.email, u.email_verified_at, u.suspended_at, u.suspended_until, u.role, p.full_name
FROM users u
JOIN profiles p ON p.user_id = u.id
WHERE u.id = @id
FOR UPDATE OF u;

-- name: SuspendUser :exec
UPDATE users
SET suspended_at = @suspended_at, suspended_until = sqlc.narg('suspended_until'), suspension_reason = @reason
WHERE id = @id;

-- name: RestoreUser :exec
UPDATE users SET suspended_at = NULL, suspended_until = NULL, suspension_reason = NULL
WHERE id = @id;

-- name: ClearExpiredSuspension :exec
-- A temporary suspension ends by time; nothing runs at that moment, so the next login or
-- refresh clears it. A permanent one has no suspended_until and is never matched.
UPDATE users SET suspended_at = NULL, suspended_until = NULL, suspension_reason = NULL
WHERE id = @id AND suspended_until <= @now;

-- name: SetUserRoleByEmail :execrows
-- Only `make admin` and `make unadmin` (cmd/setrole) run it; no API operation does.
UPDATE users SET role = @role WHERE email = @email;
