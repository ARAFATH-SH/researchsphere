-- One order for messages (docs/decisions.md, 2026-11-07). uuid_v7() kept only milliseconds, so
-- two sends in one millisecond got IDs in random order, and it read the clock apart from
-- created_at. uuid_v7_at makes the ID from a given instant, down to the microsecond, in the
-- 12 bits RFC 9562 allows for that (method 3); a send uses one instant for both columns.
CREATE FUNCTION uuid_v7_at(instant timestamptz) RETURNS uuid
LANGUAGE sql VOLATILE PARALLEL SAFE AS $$
    SELECT encode(
        substring(int8send(t.us / 1000) FROM 3)
            || int2send((28672 | ((t.us % 1000) * 4096 / 1000))::smallint)
            || substring(uuid_send(gen_random_uuid()) FROM 9),
        'hex')::uuid
    FROM (SELECT floor(extract(epoch FROM instant) * 1000000)::bigint AS us) t
$$;

CREATE OR REPLACE FUNCTION uuid_v7() RETURNS uuid
LANGUAGE sql VOLATILE PARALLEL SAFE AS $$
    SELECT uuid_v7_at(clock_timestamp())
$$;

-- now() is the start of the transaction, so rows of one transaction tie and a transaction that
-- waited on a lock looks older than it is. These columns order paged lists.
ALTER TABLE users ALTER COLUMN created_at SET DEFAULT clock_timestamp();
ALTER TABLE connections ALTER COLUMN created_at SET DEFAULT clock_timestamp();
ALTER TABLE blocks ALTER COLUMN created_at SET DEFAULT clock_timestamp();
ALTER TABLE notifications ALTER COLUMN created_at SET DEFAULT clock_timestamp();
