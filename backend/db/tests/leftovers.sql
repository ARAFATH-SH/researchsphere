-- Lists every object left in the public schema after all migrations are undone.
-- Prints nothing when the down migrations removed everything; CI fails on any output.
-- golang-migrate's own schema_migrations table is ignored.

SELECT 'relation ' || c.relname
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public'
  AND c.relname NOT LIKE 'schema\_migrations%'
UNION ALL
SELECT 'type ' || t.typname
FROM pg_type t
JOIN pg_namespace n ON n.oid = t.typnamespace
WHERE n.nspname = 'public'
  AND t.typrelid = 0          -- not a table's row type
  AND t.typcategory <> 'A'    -- not an array type
UNION ALL
SELECT 'function ' || p.proname
FROM pg_proc p
JOIN pg_namespace n ON n.oid = p.pronamespace
WHERE n.nspname = 'public'
UNION ALL
SELECT 'extension ' || e.extname
FROM pg_extension e
WHERE e.extname <> 'plpgsql';
