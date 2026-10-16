ALTER TABLE profiles
    DROP CONSTRAINT profiles_country_check,
    DROP CONSTRAINT profiles_job_title_check,
    DROP CONSTRAINT profiles_department_check,
    DROP CONSTRAINT profiles_institution_check,
    DROP CONSTRAINT profiles_bio_check,
    DROP CONSTRAINT profiles_headline_check,
    DROP CONSTRAINT profiles_full_name_length;
