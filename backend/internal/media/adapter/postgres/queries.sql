-- Rows never tried come first, then the ones whose last attempt is oldest, so a key that
-- keeps failing cannot hold back the rest of the queue. SKIP LOCKED lets several API
-- instances drain at the same time without taking the same rows.
-- name: ClaimStorageDeletions :many
SELECT id, object_key, attempts
FROM storage_deletions
ORDER BY last_attempt_at NULLS FIRST, created_at
LIMIT @max_rows
FOR UPDATE SKIP LOCKED;

-- name: DeleteStorageDeletion :exec
DELETE FROM storage_deletions WHERE id = @id;

-- name: RecordStorageDeletionFailure :exec
UPDATE storage_deletions
SET attempts = attempts + 1, last_error = @last_error, last_attempt_at = now()
WHERE id = @id;

-- Profile photo and cover (FR-4.6). The profile row is locked first, so two uploads of one
-- user replace each other in turn and neither leaves a row nothing points at.
-- name: LockProfileImages :one
SELECT photo_file_id, cover_file_id FROM profiles WHERE user_id = @user_id FOR UPDATE;

-- A profile image was re-encoded by the API, so it needs no virus scan.
-- name: InsertProfileImageFile :one
INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes, width, height, scan_status)
VALUES (@owner_user_id, @purpose, @object_key, @content_type, @size_bytes, @width, @height, 'clean')
RETURNING id;

-- name: SetProfilePhoto :exec
UPDATE profiles SET photo_file_id = sqlc.narg(file_id) WHERE user_id = @user_id;

-- name: SetProfileCover :exec
UPDATE profiles SET cover_file_id = sqlc.narg(file_id) WHERE user_id = @user_id;

-- The trigger of 000007 queues the row's object for deletion.
-- name: DeleteOwnStoredFile :exec
DELETE FROM stored_files WHERE id = @id AND owner_user_id = @owner_user_id;

-- name: FindStoredFile :one
SELECT f.id, f.owner_user_id, f.purpose, f.object_key, f.scan_status,
       (u.email_verified_at IS NOT NULL)::boolean AS owner_verified,
       (u.suspended_at IS NOT NULL)::boolean AS owner_suspended
FROM stored_files f
JOIN users u ON u.id = f.owner_user_id
WHERE f.id = @id;
