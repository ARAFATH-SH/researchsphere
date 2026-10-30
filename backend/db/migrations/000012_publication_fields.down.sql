ALTER TABLE publications
    DROP CONSTRAINT publications_doi_lower_check,
    DROP CONSTRAINT publications_url_check,
    DROP CONSTRAINT publications_abstract_check,
    DROP CONSTRAINT publications_venue_name_check,
    DROP CONSTRAINT publications_title_check,
    ADD CONSTRAINT publications_title_check CHECK (length(btrim(title)) > 0),
    DROP COLUMN created_by;
