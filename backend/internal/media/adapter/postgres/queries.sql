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

-- The malware scan of stored files (FR-16.7, NFR-13; slice 14B, chunk 14B.3). A worker leases
-- due files in one statement: SKIP LOCKED keeps two workers off the same rows while the claim
-- runs, and the lease keeps them off afterwards, without a transaction open during the scan.
-- name: ClaimScans :many
UPDATE stored_files f
SET scan_lease_until = now() + make_interval(secs => @lease_seconds::double precision),
    scan_lease_token = gen_random_uuid()
WHERE f.id IN (
    SELECT d.id FROM stored_files d
    WHERE d.scan_status = 'pending' AND d.scan_next_at <= now()
      AND (d.scan_lease_until IS NULL OR d.scan_lease_until < now())
    ORDER BY d.scan_next_at
    LIMIT @max_rows
    FOR UPDATE SKIP LOCKED
)
RETURNING f.id, f.owner_user_id, f.object_key, f.scan_attempts, f.scan_lease_token::uuid AS lease_token;

-- name: MarkScanClean :execrows
UPDATE stored_files
SET scan_status = 'clean', scanned_at = now(), scan_error = NULL, scan_lease_until = NULL, scan_lease_token = NULL
WHERE id = @id AND scan_lease_token = @lease_token AND scan_status = 'pending';

-- The row stays as the record; its object is queued for deletion now (the trigger of 000007
-- only fires when a row goes).
-- name: MarkScanInfected :execrows
WITH marked AS (
    UPDATE stored_files f
    SET scan_status = 'infected', scanned_at = now(), scan_error = NULL, scan_lease_until = NULL, scan_lease_token = NULL
    WHERE f.id = @id AND f.scan_lease_token = @lease_token AND f.scan_status = 'pending'
    RETURNING f.object_key
)
INSERT INTO storage_deletions (object_key) SELECT object_key FROM marked;

-- name: MarkScanFailed :execrows
UPDATE stored_files
SET scan_status = 'failed', scanned_at = now(), scan_error = @scan_error,
    scan_attempts = LEAST(scan_attempts + 1, 5), scan_lease_until = NULL, scan_lease_token = NULL
WHERE id = @id AND scan_lease_token = @lease_token AND scan_status = 'pending';

-- name: RetryScanLater :execrows
UPDATE stored_files
SET scan_next_at = now() + make_interval(secs => @delay_seconds::double precision),
    scan_attempts = scan_attempts + @add_attempts::integer,
    scan_error = COALESCE(sqlc.narg(scan_error), scan_error),
    scan_lease_until = NULL, scan_lease_token = NULL
WHERE id = @id AND scan_lease_token = @lease_token AND scan_status = 'pending';
