-- Store.Queue: StatusView.Failed, Running, Retrying and Waiting (Amendment J-W1, decision 25).
-- Read-only; use Query. Run in the same Read transaction as the other Status statements, so
-- the four lists are one snapshot.
-- Args: $1 limit integer (MaxRecent = 50, applied to each bucket separately).
-- Columns, in order: id uuid, name text, key text (NULL: none), status text, attempts integer,
-- run_at timestamptz, started_at timestamptz (NULL until first leased), finished_at timestamptz
-- (NULL unless failed), last_error text (a token, NULL: none), instance text (NULL: none),
-- trace_id text (NULL: none), bucket text ('failed', 'running', 'retrying' or 'waiting').
-- Four arms, each ordered and limited on its own so each can stop early on an index and no arm
-- sorts the whole queue (a long healthy queue must not make every status read sort it):
--   failed    newest finished_at first, id DESC    (job_failed_idx: finished_at DESC, id DESC)
--   running   earliest started_at first, id        (few rows; job_due_idx narrows to open rows)
--   retrying  soonest run_at first, id             (job_due_idx: run_at, id; last_error NOT NULL)
--   waiting   soonest run_at first, id             (job_due_idx: run_at, id; last_error NULL)
-- The result is sorted by arm, then by the arm's order (ord, a row_number over the at most $1
-- rows the arm kept), so the store sees each list in order. A done or canceled row is in no arm. args is not returned.
SELECT id, name, key, status, attempts, run_at, started_at, finished_at, last_error, instance,
       trace_id, bucket
FROM (
    (SELECT id, name, key, status, attempts, run_at, started_at, finished_at, last_error,
            instance, trace_id, 'failed' AS bucket, 1 AS arm,
            row_number() OVER (ORDER BY finished_at DESC, id DESC) AS ord
     FROM (SELECT * FROM core.job WHERE status = 'failed'
           ORDER BY finished_at DESC, id DESC LIMIT $1::integer) AS f)
    UNION ALL
    (SELECT id, name, key, status, attempts, run_at, started_at, finished_at, last_error,
            instance, trace_id, 'running' AS bucket, 2 AS arm,
            row_number() OVER (ORDER BY started_at, id) AS ord
     FROM (SELECT * FROM core.job WHERE status = 'running'
           ORDER BY started_at, id LIMIT $1::integer) AS r)
    UNION ALL
    (SELECT id, name, key, status, attempts, run_at, started_at, finished_at, last_error,
            instance, trace_id, 'retrying' AS bucket, 3 AS arm,
            row_number() OVER (ORDER BY run_at, id) AS ord
     FROM (SELECT * FROM core.job WHERE status = 'queued' AND last_error IS NOT NULL
           ORDER BY run_at, id LIMIT $1::integer) AS y)
    UNION ALL
    (SELECT id, name, key, status, attempts, run_at, started_at, finished_at, last_error,
            instance, trace_id, 'waiting' AS bucket, 4 AS arm,
            row_number() OVER (ORDER BY run_at, id) AS ord
     FROM (SELECT * FROM core.job WHERE status = 'queued' AND last_error IS NULL
           ORDER BY run_at, id LIMIT $1::integer) AS w)
) AS u
ORDER BY arm, ord
