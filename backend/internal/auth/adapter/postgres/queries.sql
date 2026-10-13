-- name: CreateUser :one
INSERT INTO users (email, password_hash)
VALUES (@email, @password_hash)
RETURNING id;

-- name: CreateProfile :exec
INSERT INTO profiles (user_id, full_name)
VALUES (@user_id, @full_name);

-- name: FindUserByEmail :one
SELECT u.id, u.email, u.email_verified_at, p.full_name
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
SELECT id, user_id, expires_at, used_at
FROM auth_tokens
WHERE token_hash = @token_hash AND purpose = @purpose
FOR UPDATE;

-- name: MarkTokenUsed :exec
UPDATE auth_tokens SET used_at = @used_at WHERE id = @id;

-- name: MarkEmailVerified :exec
UPDATE users SET email_verified_at = coalesce(email_verified_at, @verified_at)
WHERE id = @id;

-- name: FindCredentialsByEmail :one
SELECT u.id, u.email, u.password_hash, u.email_verified_at, p.full_name
FROM users u
JOIN profiles p ON p.user_id = u.id
WHERE u.email = @email;

-- name: FindUserByID :one
SELECT u.id, u.email, u.email_verified_at, p.full_name
FROM users u
JOIN profiles p ON p.user_id = u.id
WHERE u.id = @id;

-- name: CreateSession :exec
INSERT INTO sessions (user_id, refresh_token_hash, expires_at)
VALUES (@user_id, @refresh_token_hash, @expires_at);

-- name: FindSessionForUpdate :one
SELECT id, user_id, expires_at, revoked_at
FROM sessions
WHERE refresh_token_hash = @refresh_token_hash
FOR UPDATE;

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
