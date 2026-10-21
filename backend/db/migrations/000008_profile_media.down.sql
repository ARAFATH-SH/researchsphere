ALTER TABLE profiles
    DROP CONSTRAINT profiles_photo_cover_differ,
    DROP CONSTRAINT profiles_cover_file_key,
    DROP CONSTRAINT profiles_photo_file_key,
    DROP CONSTRAINT profiles_cover_file_fkey,
    DROP CONSTRAINT profiles_photo_file_fkey,
    DROP COLUMN cover_file_id,
    DROP COLUMN photo_file_id;

ALTER TABLE stored_files
    DROP CONSTRAINT stored_files_id_owner_key;
