-- Store.RunNow: an admin runs a scheduled name at once. Inserts a row due now with key
-- 'admin:<id>', only while no row of that name is Queued or Running. Use Query (RETURNING)
-- inside the Write transaction that also records jobs.job.run. The caller has already checked
-- the name is registered here with Every > 0.
-- MUST be preceded, in the same Write transaction, by lock_job_name.sql for this name (see its
-- header): without the lock, two concurrent transactions both pass the NOT EXISTS.
-- Args: $1 id uuid (a new JobID; the key is built from it), $2 name text, $3 now timestamptz
-- (run_at, created_at and updated_at), $4 trace_id text (NULL when none).
-- Columns, in order: id uuid. ZERO rows: a row of that name is open (Conflict, code running).
-- Constraint errors (Invalid, caller bugs): job_nil_uuid_chk, job_name_chk, job_trace_id_chk.
-- Served by job_name_open_idx. Fails 25006 (db.ErrReadOnly) on a read-only copy.
INSERT INTO core.job
    (id, name, key, args, status, attempts, run_at, trace_id, created_at, updated_at)
SELECT $1::uuid, $2::text, 'admin:' || ($1::uuid)::text, '{}'::jsonb, 'queued', 0,
       $3::timestamptz, $4::text, $3::timestamptz, $3::timestamptz
WHERE NOT EXISTS (
    SELECT 1
    FROM core.job AS open_job
    WHERE open_job.name = $2::text
      AND open_job.status IN ('queued', 'running')
)
RETURNING id
