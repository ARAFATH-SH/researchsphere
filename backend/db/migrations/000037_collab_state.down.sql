-- Undoes 000037. The Yjs state stays (000001 owns the column); without its epoch it cannot
-- be told apart from a rebuilt one, so it is cleared and the sync server builds it again from
-- content.

UPDATE project_files SET ydoc_state = NULL WHERE ydoc_state IS NOT NULL;

ALTER TABLE project_files
    DROP CONSTRAINT project_files_ydoc_size,
    DROP CONSTRAINT project_files_ydoc_epoch,
    DROP COLUMN ydoc_epoch;
