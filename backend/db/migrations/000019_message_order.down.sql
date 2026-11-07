ALTER TABLE notifications ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE blocks ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE connections ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE users ALTER COLUMN created_at SET DEFAULT now();

CREATE OR REPLACE FUNCTION uuid_v7() RETURNS uuid
LANGUAGE sql VOLATILE PARALLEL SAFE AS $$
    SELECT encode(
        set_bit(set_bit(
            overlay(uuid_send(gen_random_uuid())
                    PLACING substring(int8send((extract(epoch FROM clock_timestamp()) * 1000)::bigint) FROM 3)
                    FROM 1 FOR 6),
            52, 1), 53, 1),
        'hex')::uuid
$$;

DROP FUNCTION uuid_v7_at(timestamptz);
