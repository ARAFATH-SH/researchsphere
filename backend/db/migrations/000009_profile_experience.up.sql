-- Work experience entries (FR-4.8). The API checks the same rules and names the invalid
-- field; these constraints are the safety net. Lengths count characters; text is never
-- only whitespace, and optional text is NULL when not set. Dates are a year and a month.
-- "Not in the future" depends on the clock and the earliest year the API allows (1950) is
-- its policy, so both stay in the API; the 1900 to 2100 checks are the outer bound. An
-- entry is current exactly when it has no end date, and the end is not before the start.
CREATE TABLE profile_experience (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title           text NOT NULL CONSTRAINT profile_experience_title_check
                    CHECK (char_length(title) <= 100 AND title ~ '\S' AND title !~ '[\r\n]'),
    organization    text NOT NULL CONSTRAINT profile_experience_organization_check
                    CHECK (char_length(organization) <= 150 AND organization ~ '\S' AND organization !~ '[\r\n]'),
    employment_type text NOT NULL CONSTRAINT profile_experience_employment_type_check
                    CHECK (employment_type IN ('full_time', 'part_time', 'internship', 'contract', 'freelance',
                        'self_employed', 'research_assistant', 'teaching_assistant', 'postdoc', 'volunteer')),
    location        text CONSTRAINT profile_experience_location_check
                    CHECK (char_length(location) <= 100 AND location ~ '\S' AND location !~ '[\r\n]'),
    location_type   text CONSTRAINT profile_experience_location_type_check
                    CHECK (location_type IN ('on_site', 'hybrid', 'remote')),
    start_year      smallint NOT NULL CONSTRAINT profile_experience_start_year_check CHECK (start_year BETWEEN 1900 AND 2100),
    start_month     smallint NOT NULL CONSTRAINT profile_experience_start_month_check CHECK (start_month BETWEEN 1 AND 12),
    end_year        smallint CONSTRAINT profile_experience_end_year_check CHECK (end_year BETWEEN 1900 AND 2100),
    end_month       smallint CONSTRAINT profile_experience_end_month_check CHECK (end_month BETWEEN 1 AND 12),
    is_current      boolean NOT NULL,
    description     text CONSTRAINT profile_experience_description_check
                    CHECK (char_length(description) <= 2000 AND description ~ '\S'),
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT profile_experience_end_pair CHECK ((end_year IS NULL) = (end_month IS NULL)),
    CONSTRAINT profile_experience_current_check CHECK (is_current = (end_year IS NULL)),
    CONSTRAINT profile_experience_date_range CHECK (
        end_year IS NULL OR (end_year, end_month) >= (start_year, start_month))
);
CREATE INDEX profile_experience_user_idx ON profile_experience (user_id);
