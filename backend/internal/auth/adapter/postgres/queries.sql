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
