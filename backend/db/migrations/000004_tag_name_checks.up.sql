-- Tag names are normalized before they are stored (FR-4.2): trimmed, every run of
-- whitespace collapsed to one space, at most 60 characters. The API does this and names
-- the error; these constraints are the safety net. Case-insensitive uniqueness already
-- comes from the citext UNIQUE constraint.
ALTER TABLE research_interests
    ADD CONSTRAINT research_interests_name_length  CHECK (char_length(name) <= 60),
    ADD CONSTRAINT research_interests_name_spacing CHECK (name !~ '^\s|\s$|\s\s|[\t\n\r\v\f]');

ALTER TABLE skills
    ADD CONSTRAINT skills_name_length  CHECK (char_length(name) <= 60),
    ADD CONSTRAINT skills_name_spacing CHECK (name !~ '^\s|\s$|\s\s|[\t\n\r\v\f]');
