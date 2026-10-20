-- Store.JobByKey: the existing row for a (name, key), after enqueue_job.sql or
-- defer_job.sql returned ZERO rows. Read-only; use QueryRow. Run it in the same transaction
-- as the insert (it is a new statement, so it sees a row a concurrent transaction committed
-- while the insert waited on the unique index).
-- Args: $1 name text, $2 key text.
-- Columns, in order: id uuid, status text. pgx.ErrNoRows (NotFound) only if the row was
-- purged in between (a final row older than DoneTTL; the caller may retry the insert once).
-- Served by job_name_key_uidx.
SELECT id, status
FROM core.job
WHERE name = $1::text
  AND key = $2::text
