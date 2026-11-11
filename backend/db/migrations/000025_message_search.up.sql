-- Message search (FR-16.8, NFR-13; slice 14B, chunk 14B.4). docs/decisions.md, 2026-11-10
-- (message search).
--
-- visible_messages is the one statement of who may read which message: a current member of the
-- conversation, every message created from the moment they joined (FR-16.6). Leaving a group
-- deletes the member row, so nothing stays visible after it. The thread (ListMessages) and the
-- search (SearchMessages) both read through it; the search adds its own filters (no deleted
-- messages, no system messages). A deleted message keeps its row with no body, and earlier
-- texts live only in message_versions, which this view never reads. PostgreSQL inlines the
-- view, so each query still uses the indexes of the two tables.
CREATE VIEW visible_messages AS
SELECT me.user_id AS viewer_id,
       m.id, m.conversation_id, m.sender_id, m.body, m.client_id, m.created_at, m.edited_at,
       m.deleted_at, m.kind, m.event, m.event_data, m.has_attachments
FROM conversation_members me
JOIN messages m ON m.conversation_id = me.conversation_id AND m.created_at >= me.joined_at;

-- Word matches. The 'simple' configuration lowercases and splits on the parser's word
-- boundaries with no stemming and no stop words, so Bangla, English and mixed text are all
-- split into whole words (the 'english' configuration would stem Bangla words wrongly).
-- Splitting a Bangla word with its vowel signs needs a UTF-8 locale (LC_CTYPE); docs/decisions.md.
CREATE INDEX messages_body_simple_idx ON messages USING gin (to_tsvector('simple', body));
-- Partial words and small typos: trigram word similarity (body %> q).
CREATE INDEX messages_body_trgm_idx ON messages USING gin (body gin_trgm_ops);

-- Attachment names, the same two ways. The parser keeps a file name such as
-- "Thesis_draft-v2.pdf" as one token, so _ - . are read as spaces first.
CREATE INDEX message_attachments_name_simple_idx ON message_attachments
    USING gin (to_tsvector('simple', translate(name, '_-.', '   ')));
CREATE INDEX message_attachments_name_trgm_idx ON message_attachments USING gin (name gin_trgm_ops);
