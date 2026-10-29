-- Publications added by hand (FR-5.1, FR-5.4). The API checks the same rules and names the
-- invalid field; these constraints are the safety net. Lengths count characters; text is
-- never only whitespace, and optional text is NULL when not set. The year stays nullable
-- because an imported work may have none (FR-5.3); the API requires it for a manual entry,
-- and "at most next year" depends on the clock, so it stays in the API. The DOI is stored
-- lower-case so the bare form is the one spelling. created_by is who added the record; it
-- is cleared when that account is deleted, so a record shared with a co-author stays.
ALTER TABLE publications
    ADD COLUMN created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    DROP CONSTRAINT publications_title_check,
    ADD CONSTRAINT publications_title_check
        CHECK (char_length(title) <= 500 AND title ~ '\S' AND title !~ '[\r\n]'),
    ADD CONSTRAINT publications_venue_name_check
        CHECK (char_length(venue_name) <= 300 AND venue_name ~ '\S' AND venue_name !~ '[\r\n]'),
    ADD CONSTRAINT publications_abstract_check
        CHECK (char_length(abstract) <= 5000 AND abstract ~ '\S'),
    ADD CONSTRAINT publications_url_check
        CHECK (char_length(url) <= 500 AND url ~* '^https?://\S+$'),
    ADD CONSTRAINT publications_doi_lower_check
        CHECK (doi::text = lower(doi::text));
