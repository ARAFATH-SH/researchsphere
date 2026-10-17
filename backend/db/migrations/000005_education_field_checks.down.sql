ALTER TABLE education
    DROP CONSTRAINT education_field_of_study_check,
    DROP CONSTRAINT education_degree_check,
    DROP CONSTRAINT education_institution_check,
    ALTER COLUMN start_year DROP NOT NULL;

-- The degree was required before 000005; an entry without one gets the empty string,
-- which that schema allowed.
UPDATE education SET degree = '' WHERE degree IS NULL;
ALTER TABLE education ALTER COLUMN degree SET NOT NULL;
