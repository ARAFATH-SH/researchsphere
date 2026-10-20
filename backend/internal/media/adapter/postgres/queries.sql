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
