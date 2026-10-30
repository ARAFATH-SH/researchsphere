-- Researcher search (FR-6.1 to FR-6.3; slice 12, chunk 12.1). pg_trgm and the trigram
-- index on profiles.full_name exist since 000001. These GIN trigram indexes serve the
-- "contains" matches (ILIKE '%q%') on the other searched fields. research_interests.name
-- is citext, which gin_trgm_ops does not take, so its index is on name::text, and the
-- query must use the same expression.
CREATE INDEX profiles_institution_trgm_idx ON profiles USING gin (institution gin_trgm_ops);
CREATE INDEX profiles_headline_trgm_idx ON profiles USING gin (headline gin_trgm_ops);
CREATE INDEX research_interests_name_trgm_idx ON research_interests USING gin ((name::text) gin_trgm_ops);
