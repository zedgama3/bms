-- Store.CancelShiftsByDepartment: one batch of the volunteers.cancel-shifts job (departments.
-- department-archived; system actor, no person). Cancels up to $4 non-canceled shifts of the
-- department that have not ended, FUTURE ones only: a shift that has ended is history and is
-- left alone. Use Query. Run inside one Write transaction per batch, followed by
-- cancel_signups_by_shifts.sql for the returned IDs (reason department_archived); the job
-- repeats until a batch returns no row. Idempotent on state: a second run, or a retry after
-- a failed batch, finds only what is left. Not SKIP LOCKED on purpose: a shift locked by a
-- booking in flight must be waited for, or the job could end with it uncanceled.
-- Args: $1 department_id uuid, $2 now timestamptz (canceled_at, updated_at; shifts whose end
--       is after it), $3 reason text ('department_archived'), $4 limit integer (vol.MaxList).
-- Columns: id uuid (the shifts just canceled; at most $4, in no particular order).
-- Served by shift_department_starts_idx for the batch and shift_pkey for the update (the id
-- list is an array, so the update is a key lookup, not a join over the whole table).
-- "starts > now - 48 hours" is implied by shift_times_chk (a shift is at most 48 hours long,
-- so one that ends after now started after now - 48h) and makes each batch a range scan from
-- now, not a walk over the department's ended shifts again.
UPDATE volunteers.shift
SET canceled_at = $2::timestamptz, cancel_reason = $3::text, updated_at = $2::timestamptz
WHERE id = ANY(ARRAY(
        SELECT id
        FROM volunteers.shift
        WHERE department_id = $1::uuid
          AND canceled_at IS NULL
          AND ends > $2::timestamptz
          AND starts > $2::timestamptz - interval '48 hours'
        ORDER BY starts, id
        LIMIT $4::integer))
  AND canceled_at IS NULL
RETURNING id
