-- Real-time editing state (FR-12.4, FR-12.9, NFR-10.1; slice 22, docs/decisions.md 2026-11-19).
-- The sync server keeps a text file's compacted Y.Doc in ydoc_state (000001) beside the plain
-- text in content. ydoc_epoch names one Yjs history: the sync server sets a new one whenever
-- it builds the state from content (a new file, or one a REST save rewrote), and a REST save
-- clears both. A client merges offline Yjs updates only into the epoch they were made in, so a
-- rebuilt document never receives updates for items it does not have (duplicated text).

ALTER TABLE project_files
    ADD COLUMN ydoc_epoch uuid,
    ADD CONSTRAINT project_files_ydoc_epoch CHECK ((ydoc_state IS NULL) = (ydoc_epoch IS NULL)),
    -- The text is at most 1 MiB (project_files_text); the Yjs state adds item IDs and
    -- tombstones. 8 MiB is far above what a 1 MiB document needs.
    ADD CONSTRAINT project_files_ydoc_size CHECK (octet_length(ydoc_state) <= 8388608);
