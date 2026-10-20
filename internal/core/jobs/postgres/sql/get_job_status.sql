-- Store.StatusOf: tells Retry and Cancel why retry_job.sql or cancel_job.sql changed nothing.
-- Read-only; use QueryRow.
-- Args: $1 id uuid.
-- Columns, in order: name text, status text. pgx.ErrNoRows (NotFound) when there is no such
-- row. For Retry: any status but failed is Conflict code status. For Cancel: any status but
-- queued is Conflict code status.
-- Served by job_pkey.
SELECT name, status
FROM core.job
WHERE id = $1::uuid
