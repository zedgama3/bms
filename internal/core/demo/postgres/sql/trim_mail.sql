-- Store.Capture, second statement: keep only the newest $1 rows (demo.MaxMail = 200), deleting
-- everything older than the $1-th newest. Run in the same Write transaction as insert_mail.sql,
-- after it. Use QueryRow.
-- Args: $1 keep integer (>= 1).
-- Columns, in order: trimmed bigint (scan into int64), the rows deleted (usually 0 or 1).
-- Order is (captured_at DESC, id DESC), the same total order as list_mail.sql, so the rows kept
-- are the ones a viewer lists. The subquery finds the $1-th newest row (NULL, and so nothing
-- deleted, while fewer than $1 rows exist); the DELETE removes the rows strictly older than it.
-- It takes no lock on a kept row, and two captures running at once compute nearly the same
-- threshold and delete the same old rows, so the second simply finds them gone (a row delete
-- waits for the first transaction and then skips). Over-keeping by a row for an instant is
-- possible under concurrency; the next capture trims it. The table never holds more than about
-- MaxMail rows, so the plan is a sort of at most ~200 rows and a scan of them (about 0.1 ms);
-- no index is needed for it.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy.
-- Constraint errors: none reachable.
WITH gone AS (
    DELETE FROM core.demo_mail
    WHERE (captured_at, id) < (
        SELECT captured_at, id
        FROM core.demo_mail
        ORDER BY captured_at DESC, id DESC
        OFFSET $1::integer - 1
        LIMIT 1
    )
    RETURNING 1
)
SELECT count(*) AS trimmed FROM gone
