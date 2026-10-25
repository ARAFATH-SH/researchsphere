-- Languages a user knows (FR-4.9). The API checks the same rules and names the invalid
-- field; these constraints are the safety net. The name counts characters, is never only
-- whitespace and is one line. Names that differ only in letter case are the same language
-- for one user; the stored name keeps the casing the user typed. The limit of 20 per user
-- needs a count, so the API checks it under the user lock, as it does for experience.
CREATE TABLE profile_languages (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        text NOT NULL CONSTRAINT profile_languages_name_check
                CHECK (char_length(name) <= 50 AND name ~ '\S' AND name !~ '[\r\n]'),
    proficiency text NOT NULL CONSTRAINT profile_languages_proficiency_check
                CHECK (proficiency IN ('elementary', 'limited_working', 'professional_working',
                    'full_professional', 'native_or_bilingual')),
    created_at  timestamptz NOT NULL DEFAULT now()
);
-- Also serves lookups by user_id.
CREATE UNIQUE INDEX profile_languages_user_name_key ON profile_languages (user_id, lower(name));
