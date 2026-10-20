-- Store.InsertSlot: the scheduler, one statement per registered name with Every > 0, run once
-- per tick inside one Write transaction. Inserts the CURRENT slot (the caller computes
-- slot = now.UTC().Truncate(Every) and key = slot.Format(time.RFC3339)) only while no row of
-- that name is Queued or Running (never overlaps itself), and only once per (name, key) (two
-- processes, or two ticks in one slot, insert it once). It never inserts an earlier slot:
-- there is no catch-up. A name whose last row is Done, Failed or Canceled gets its slot.
-- Use Query (RETURNING).
-- MUST be preceded, in the same Write transaction, by lock_job_name.sql for this name (see its
-- header): without the lock, two concurrent transactions both pass the NOT EXISTS.
-- Args: $1 id uuid (a new JobID), $2 name text, $3 key text (the slot, RFC 3339 UTC, e.g.
-- 2026-10-20T14:00:00Z), $4 run_at timestamptz (the slot time), $5 now timestamptz (created_at
-- and updated_at). args is '{}' and there is no trace.
-- Columns, in order: id uuid. ZERO rows: a row of the name is open, or this slot already
-- exists: nothing inserted (not an error; counts nothing in Ran.Scheduled).
-- Constraint errors (Invalid, caller bugs): job_nil_uuid_chk, job_name_chk, job_key_chk.
-- Served by job_name_open_idx (the NOT EXISTS) and job_name_key_uidx (the conflict).
-- Fails 25006 (db.ErrReadOnly) on a read-only copy. Two writers racing at a slot boundary
-- with different keys (clock skew) still serialize on the name lock, so only the first inserts.
INSERT INTO core.job
    (id, name, key, args, status, attempts, run_at, created_at, updated_at)
SELECT $1::uuid, $2::text, $3::text, '{}'::jsonb, 'queued', 0, $4::timestamptz,
       $5::timestamptz, $5::timestamptz
WHERE NOT EXISTS (
    SELECT 1
    FROM core.job AS open_job
    WHERE open_job.name = $2::text
      AND open_job.status IN ('queued', 'running')
)
ON CONFLICT (name, key) WHERE key IS NOT NULL DO NOTHING
RETURNING id
