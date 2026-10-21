-- Profile photo and cover image (FR-4.6; slice 10A, chunk 10A.2). Each column points at the
-- user's own stored_files row: the foreign key includes the owner, so a profile cannot show
-- another user's file. Deleting the file row clears only the column that pointed at it
-- (SET NULL with a column list, PostgreSQL 15+); the row's trigger queues the object for
-- deletion (000007). The file's purpose ('photo' or 'cover') is set by the API, not checked
-- here: PostgreSQL refuses SET NULL on a foreign key that includes a generated column.
ALTER TABLE stored_files
    ADD CONSTRAINT stored_files_id_owner_key UNIQUE (id, owner_user_id);

ALTER TABLE profiles
    ADD COLUMN photo_file_id uuid,
    ADD COLUMN cover_file_id uuid,
    ADD CONSTRAINT profiles_photo_file_fkey FOREIGN KEY (photo_file_id, user_id)
        REFERENCES stored_files (id, owner_user_id) ON DELETE SET NULL (photo_file_id),
    ADD CONSTRAINT profiles_cover_file_fkey FOREIGN KEY (cover_file_id, user_id)
        REFERENCES stored_files (id, owner_user_id) ON DELETE SET NULL (cover_file_id),
    ADD CONSTRAINT profiles_photo_file_key UNIQUE (photo_file_id),
    ADD CONSTRAINT profiles_cover_file_key UNIQUE (cover_file_id),
    ADD CONSTRAINT profiles_photo_cover_differ CHECK (photo_file_id <> cover_file_id);
