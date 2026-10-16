-- Limits on the profile fields a user edits (FR-4.1). The API checks the same rules and
-- names the invalid field; these constraints are the safety net. Lengths count
-- characters. Optional fields are NULL when not set, never blank. The country is an
-- ISO 3166-1 alpha-2 code in capitals; whether the code exists is checked by the API.
ALTER TABLE profiles
    ADD CONSTRAINT profiles_full_name_length   CHECK (char_length(full_name) <= 200),
    ADD CONSTRAINT profiles_headline_check     CHECK (char_length(headline) <= 220 AND btrim(headline) <> ''),
    ADD CONSTRAINT profiles_bio_check          CHECK (char_length(bio) <= 2000 AND btrim(bio) <> ''),
    ADD CONSTRAINT profiles_institution_check  CHECK (char_length(institution) <= 200 AND btrim(institution) <> ''),
    ADD CONSTRAINT profiles_department_check   CHECK (char_length(department) <= 200 AND btrim(department) <> ''),
    ADD CONSTRAINT profiles_job_title_check    CHECK (char_length(job_title) <= 200 AND btrim(job_title) <> ''),
    ADD CONSTRAINT profiles_country_check      CHECK (country ~ '^[A-Z]{2}$');
