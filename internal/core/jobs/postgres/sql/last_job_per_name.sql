-- Store.LastPerName: Registered.Last for every name registered in this process. Read-only;
-- use Query. Run in the same Read transaction as the other Status statements.
-- Args: $1 names text[] (the registered names; at most a few dozen; a nil or empty array
-- returns no rows).
-- Columns, in order, sorted by name: id uuid, name text, key text (NULL: none), status text,
-- attempts integer, run_at timestamptz, started_at timestamptz (NULL until first leased),
-- finished_at timestamptz (NULL until final), last_error text (a token, NULL: none),
-- instance text (NULL: none), trace_id text (NULL: none). A name with no row is absent.
-- The newest row is the one with the greatest created_at (ties by id).
-- Served by job_name_created_idx, one index probe per name.
SELECT j.id, j.name, j.key, j.status, j.attempts, j.run_at, j.started_at, j.finished_at,
       j.last_error, j.instance, j.trace_id
FROM unnest($1::text[]) AS n(name)
CROSS JOIN LATERAL (
    SELECT id, name, key, status, attempts, run_at, started_at, finished_at, last_error,
           instance, trace_id
    FROM core.job
    WHERE name = n.name
    ORDER BY created_at DESC, id DESC
    LIMIT 1
) AS j
ORDER BY j.name
