-- Store.UpdateShift: after get_shift_for_update.sql and the use case's checks (not canceled,
-- slots >= booked, no new overlap for a booked person: find_overlap_for_shift_change.sql).
-- The department never changes. Use QueryRow; zero rows = canceled or gone (race: Conflict
-- canceled).
-- Args: $1 id uuid, $2 position_id uuid, $3 event_id uuid (NULL = standing work), $4 label text,
--       $5 starts timestamptz, $6 ends timestamptz, $7 zone text, $8 slots integer,
--       $9 location text, $10 notes text, $11 updated_at timestamptz.
-- Columns: the shift columns. Constraint errors: as insert_shift.sql.
UPDATE volunteers.shift
SET position_id = $2::uuid, event_id = $3::uuid, label = $4::text, starts = $5::timestamptz,
    ends = $6::timestamptz, zone = $7::text, slots = $8::integer, location = $9::text,
    notes = $10::text, updated_at = $11::timestamptz
WHERE id = $1::uuid
  AND canceled_at IS NULL
RETURNING id, department_id, position_id, event_id, label, starts, ends, zone, slots, location, notes,
          canceled_at, cancel_reason, copied_from, created_by, created_at, updated_at
