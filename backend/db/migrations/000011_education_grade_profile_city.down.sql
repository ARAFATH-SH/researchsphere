ALTER TABLE profiles DROP COLUMN city;

ALTER TABLE profile_education
    DROP COLUMN description,
    DROP COLUMN grade;

ALTER TABLE profile_education RENAME CONSTRAINT profile_education_field_of_study_check TO education_field_of_study_check;
ALTER TABLE profile_education RENAME CONSTRAINT profile_education_degree_check TO education_degree_check;
ALTER TABLE profile_education RENAME CONSTRAINT profile_education_institution_check TO education_institution_check;
ALTER TABLE profile_education RENAME CONSTRAINT profile_education_end_year_check TO education_end_year_check;
ALTER TABLE profile_education RENAME CONSTRAINT profile_education_start_year_check TO education_start_year_check;
ALTER TABLE profile_education RENAME CONSTRAINT profile_education_check TO education_check;
ALTER TABLE profile_education RENAME CONSTRAINT profile_education_user_id_fkey TO education_user_id_fkey;
ALTER TABLE profile_education RENAME CONSTRAINT profile_education_pkey TO education_pkey;
ALTER INDEX profile_education_user_idx RENAME TO education_user_idx;
ALTER TABLE profile_education RENAME TO education;
