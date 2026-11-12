-- FR-17.4: the hourly purge finds accounts still unverified some days after sign-up. Only
-- unverified rows are indexed, so the index stays small however many users verify.
CREATE INDEX users_unverified_created_idx ON users (created_at) WHERE email_verified_at IS NULL;
