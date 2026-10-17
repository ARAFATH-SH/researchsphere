-- Education entries (FR-4.4). The degree becomes optional and the start year required, as
-- the API treats them. The API checks the same text rules and names the invalid field;
-- these constraints are the safety net. Lengths count characters. Optional fields are
-- NULL when not set, never blank. The year range the API allows (1950 to ten years after
-- the current year) depends on the clock, so it stays in the API; the 1900 to 2100 checks
-- from 000001 remain as the outer bound.
ALTER TABLE education
    ALTER COLUMN degree DROP NOT NULL,
    ALTER COLUMN start_year SET NOT NULL,
    ADD CONSTRAINT education_institution_check    CHECK (char_length(institution) <= 200 AND btrim(institution) <> ''),
    ADD CONSTRAINT education_degree_check         CHECK (char_length(degree) <= 100 AND btrim(degree) <> ''),
    ADD CONSTRAINT education_field_of_study_check CHECK (char_length(field_of_study) <= 100 AND btrim(field_of_study) <> '');
