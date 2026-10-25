-- Education grade and description (FR-4.4) and profile city (FR-4.1). The education table
-- is renamed to profile_education, like profile_experience and profile_languages, and its
-- index and constraints follow. The API checks the same rules and names the invalid field;
-- these constraints are the safety net. Lengths count characters; text is never only
-- whitespace, and optional text is NULL when not set. The grade and city are one line; the
-- description may have line breaks. Existing rows get NULL in the new columns.
ALTER TABLE education RENAME TO profile_education;
ALTER INDEX education_user_idx RENAME TO profile_education_user_idx;
ALTER TABLE profile_education RENAME CONSTRAINT education_pkey TO profile_education_pkey;
ALTER TABLE profile_education RENAME CONSTRAINT education_user_id_fkey TO profile_education_user_id_fkey;
ALTER TABLE profile_education RENAME CONSTRAINT education_check TO profile_education_check;
ALTER TABLE profile_education RENAME CONSTRAINT education_start_year_check TO profile_education_start_year_check;
ALTER TABLE profile_education RENAME CONSTRAINT education_end_year_check TO profile_education_end_year_check;
ALTER TABLE profile_education RENAME CONSTRAINT education_institution_check TO profile_education_institution_check;
ALTER TABLE profile_education RENAME CONSTRAINT education_degree_check TO profile_education_degree_check;
ALTER TABLE profile_education RENAME CONSTRAINT education_field_of_study_check TO profile_education_field_of_study_check;

ALTER TABLE profile_education
    ADD COLUMN grade text CONSTRAINT profile_education_grade_check
        CHECK (char_length(grade) <= 50 AND grade ~ '\S' AND grade !~ '[\r\n]'),
    ADD COLUMN description text CONSTRAINT profile_education_description_check
        CHECK (char_length(description) <= 2000 AND description ~ '\S');

ALTER TABLE profiles
    ADD COLUMN city text CONSTRAINT profiles_city_check
        CHECK (char_length(city) <= 100 AND city ~ '\S' AND city !~ '[\r\n]');
