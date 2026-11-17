-- Undoes 000036. Pending uploads go (the trigger queues their objects first), and so do the
-- figures over the old 5 MB limit: their stored files go with them (000035's trigger) and
-- their objects are queued by 000007's.

DROP TABLE project_figure_uploads;

DELETE FROM project_files WHERE kind = 'binary' AND size_bytes > 5242880;
DELETE FROM stored_files WHERE purpose = 'figure' AND size_bytes > 5242880;

ALTER TABLE project_files
    DROP CONSTRAINT project_files_binary,
    ADD CONSTRAINT project_files_binary CHECK (kind <> 'binary' OR (
        stored_file_id IS NOT NULL AND content IS NULL AND ydoc_state IS NULL
        AND size_bytes BETWEEN 1 AND 5242880 AND path ~* '\.(png|jpe?g|pdf)$'));

ALTER TABLE stored_files
    DROP CONSTRAINT stored_files_figure_size,
    ADD CONSTRAINT stored_files_figure_size CHECK (purpose <> 'figure' OR size_bytes <= 5242880);
