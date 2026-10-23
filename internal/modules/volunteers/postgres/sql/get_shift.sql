-- Store.GetShift. Use QueryRow; no row = NotFound. Read-only: any copy.
-- Args: $1 id uuid. Columns: the shift columns (see insert_shift.sql). Served by shift_pkey.
SELECT id, department_id, position_id, event_id, label, starts, ends, zone, slots, location, notes,
       canceled_at, cancel_reason, copied_from, created_by, created_at, updated_at
FROM volunteers.shift
WHERE id = $1::uuid
