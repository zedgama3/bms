-- Store.Cancel: an admin cancels a waiting row: Queued becomes Canceled and final (kept until
-- the purge). A Running row is not canceled (a worker has it). Use Query (RETURNING) inside
-- the Write transaction that also records jobs.job.canceled (Reason = the returned name).
-- Args: $1 id uuid, $2 now timestamptz (finished_at and updated_at).
-- Columns, in order: id uuid, name text. ZERO rows: no such row (NotFound) or the row is not
-- queued (Conflict, code status); ask get_job_status.sql which. A row a worker leases at the
-- same moment makes this wait on its lock and then match nothing (Conflict).
-- Fails 25006 (db.ErrReadOnly) on a read-only copy. Constraint errors: none reachable.
UPDATE core.job
SET status = 'canceled',
    finished_at = $2::timestamptz,
    updated_at = $2::timestamptz
WHERE id = $1::uuid
  AND status = 'queued'
RETURNING id, name
