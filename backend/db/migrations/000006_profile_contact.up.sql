-- Contact info (FR-4.7). One row per user, created on the first save; a user without a
-- row has no phone or birthday and shows nothing to others. The birthday has no year.
-- Each visibility is 'only_me' or 'logged_in'; slice 13 widens the CHECK with
-- 'connections'. The API checks the same rules and names the invalid field; these
-- constraints are the safety net.
CREATE TABLE profile_contact (
    user_id             uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    phone               text CONSTRAINT profile_contact_phone_check CHECK (phone ~ '^\+[1-9][0-9]{1,14}$'),
    birth_month         smallint CONSTRAINT profile_contact_birth_month_check CHECK (birth_month BETWEEN 1 AND 12),
    birth_day           smallint,
    email_visibility    text NOT NULL DEFAULT 'only_me',
    phone_visibility    text NOT NULL DEFAULT 'only_me',
    birthday_visibility text NOT NULL DEFAULT 'only_me',
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT profile_contact_birthday_pair CHECK ((birth_month IS NULL) = (birth_day IS NULL)),
    -- 29 February is allowed: there is no year to make it invalid.
    CONSTRAINT profile_contact_birth_day_check CHECK (
        birth_day BETWEEN 1 AND CASE
            WHEN birth_month = 2 THEN 29
            WHEN birth_month IN (4, 6, 9, 11) THEN 30
            ELSE 31
        END),
    CONSTRAINT profile_contact_email_visibility_check    CHECK (email_visibility    IN ('only_me', 'logged_in')),
    CONSTRAINT profile_contact_phone_visibility_check    CHECK (phone_visibility    IN ('only_me', 'logged_in')),
    CONSTRAINT profile_contact_birthday_visibility_check CHECK (birthday_visibility IN ('only_me', 'logged_in'))
);
CREATE TRIGGER profile_contact_set_updated_at BEFORE UPDATE ON profile_contact FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Links on a profile (FR-4.7), visible to every logged-in user. At most 5 per user; the
-- API counts them after locking the user row, as it does for tags.
CREATE TABLE profile_links (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       text NOT NULL CONSTRAINT profile_links_kind_check
               CHECK (kind IN ('website', 'google_scholar', 'researchgate', 'linkedin', 'github', 'other')),
    url        text NOT NULL CONSTRAINT profile_links_url_check
               CHECK (char_length(url) <= 300 AND url ~ '^https://[^[:space:]]+$'),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX profile_links_user_idx ON profile_links (user_id, created_at);
