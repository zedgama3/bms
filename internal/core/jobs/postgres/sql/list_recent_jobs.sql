-- Store.Recent: StatusView.Recent. The most recent Failed rows and the Queued rows. Read-only;
-- use Query. Run in the same Read transaction as the other Status statements.
-- Args: $1 limit integer (MaxRecent = 50, applied to the whole list).
-- Columns, in order, sorted by sort_at DESC, id DESC: id uuid, name text, key text (NULL:
-- none), status text ('failed' or 'queued'), attempts integer, run_at timestamptz,
-- started_at timestamptz (NULL until first leased), finished_at timestamptz (NULL unless
-- failed), last_error text (a token, NULL: none), instance text (NULL: none), trace_id text
-- (NULL: none). The sort time is not returned: for a failed row it is finished_at, for a
-- queued row updated_at (when it was enqueued, moved, retried or re-queued). args is not
-- returned: the screen shows references by count only and no IDs.
-- Each arm is limited to $1 before the merge, so the cost is bounded by $1 rows per arm plus
-- the sort of the queued arm.
(
    SELECT id, name, key, status, attempts, run_at, started_at, finished_at, last_error,
           instance, trace_id, finished_at AS sort_at
    FROM core.job
    WHERE status = 'failed'
    ORDER BY finished_at DESC, id DESC
    LIMIT $1::integer
)
UNION ALL
(
    SELECT id, name, key, status, attempts, run_at, started_at, finished_at, last_error,
           instance, trace_id, updated_at AS sort_at
    FROM core.job
    WHERE status = 'queued'
    ORDER BY updated_at DESC, id DESC
    LIMIT $1::integer
)
ORDER BY sort_at DESC, id DESC
LIMIT $1::integer
