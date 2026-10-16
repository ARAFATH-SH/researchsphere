ALTER TABLE skills
    DROP CONSTRAINT skills_name_spacing,
    DROP CONSTRAINT skills_name_length;

ALTER TABLE research_interests
    DROP CONSTRAINT research_interests_name_spacing,
    DROP CONSTRAINT research_interests_name_length;
