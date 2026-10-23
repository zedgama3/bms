-- Store.ShiftsToCopy: CopyShifts. The department's non-canceled shifts in the source event
-- that have not yet been copied into the target event. Use Query. The caller (shifts.manage@D)
-- was checked by the use case. Idempotent: a second run lists nothing new, and
-- insert_shift_copy.sql is the race-safe backstop.
-- Args: $1 department_id uuid, $2 from_event_id uuid, $3 to_event_id uuid, $4 limit integer
--       (vol.MaxList), $5 after_starts timestamptz and $6 after_id uuid (the keyset cursor: the
--       last row of the previous batch; both NULL for the first batch). The use case loops
--       until a batch is shorter than the limit.
-- Columns: the shift columns (see insert_shift.sql), then position_archived boolean (true =
--   skip and count: F87).
-- Order: starts, id. Served by shift_event_starts_idx and shift_copied_key.
SELECT s.id, s.department_id, s.position_id, s.event_id, s.label, s.starts, s.ends, s.zone, s.slots,
       s.location, s.notes, s.canceled_at, s.cancel_reason, s.copied_from, s.created_by,
       s.created_at, s.updated_at,
       (p.archived_at IS NOT NULL) AS position_archived
FROM volunteers.shift s
JOIN volunteers.position p ON p.id = s.position_id
WHERE s.department_id = $1::uuid
  AND s.event_id = $2::uuid
  AND s.canceled_at IS NULL
  AND NOT EXISTS (
        SELECT 1 FROM volunteers.shift c
        WHERE c.copied_from = s.id AND c.event_id = $3::uuid)
  AND ($5::timestamptz IS NULL OR (s.starts, s.id) > ($5::timestamptz, $6::uuid))
ORDER BY s.starts, s.id
LIMIT $4::integer
