-- Accent-insensitive name matching for hybrid search (AI-5.1; slice 18, chunk 18.1).
-- A strong name match (the full name equals the search text, or starts with it) ignores
-- letter case and accents: "jose" finds "José". unaccent is a contrib extension shipped
-- with PostgreSQL. Its default rules cover Latin, Greek and Cyrillic letters only, so
-- Bangla vowel signs, hasanta and the like are never stripped (TestFoldNameKeepsBangla).

CREATE EXTENSION IF NOT EXISTS unaccent;

-- fold_name lowercases, removes accents and collapses runs of whitespace. unaccent(text)
-- is only STABLE (it looks the dictionary up by search_path); naming the dictionary makes
-- the result depend on the input alone, so the function can be IMMUTABLE and indexed.
CREATE FUNCTION fold_name(name text) RETURNS text
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    AS $$ SELECT regexp_replace(btrim(lower(public.unaccent('public.unaccent'::regdictionary, name))), '\s+', ' ', 'g') $$;

-- Serves the equality and prefix (LIKE 'x%') tests of a strong name match.
CREATE INDEX profiles_folded_name_idx ON profiles (fold_name(full_name) text_pattern_ops);
