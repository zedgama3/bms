-- Store.DepartmentsWithShifts: SetUp (event set-up): the departments that have non-canceled
-- shifts in the source event; the use case intersects them with the caller's shifts.manage
-- extent and runs CopyShifts for each. Use Query. Read-only.
-- Args: $1 event_id uuid. Columns: department_id uuid. Order: department_id.
-- Served by shift_event_starts_idx.
SELECT DISTINCT department_id
FROM volunteers.shift
WHERE event_id = $1::uuid
  AND canceled_at IS NULL
ORDER BY department_id
