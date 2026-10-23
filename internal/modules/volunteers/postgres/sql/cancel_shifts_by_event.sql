-- Store.CancelShiftsByEvent: one batch of the volunteers.cancel-shifts job (events.event-
-- closed; system actor). As cancel_shifts_by_department.sql, for an event: up to $4
-- non-canceled shifts of the event that have not ended, reason event_closed, followed by
-- cancel_signups_by_shifts.sql in the same Write transaction.
-- Args: $1 event_id uuid, $2 now timestamptz, $3 reason text ('event_closed'),
--       $4 limit integer (vol.MaxList).
-- Columns: id uuid. Served by shift_event_starts_idx (partial: event_id IS NOT NULL, which
-- "event_id = $1" implies) for the batch and shift_pkey for the update; see
-- cancel_shifts_by_department.sql for why the update is by id list and why starts is bounded.
UPDATE volunteers.shift
SET canceled_at = $2::timestamptz, cancel_reason = $3::text, updated_at = $2::timestamptz
WHERE id = ANY(ARRAY(
        SELECT id
        FROM volunteers.shift
        WHERE event_id = $1::uuid
          AND canceled_at IS NULL
          AND ends > $2::timestamptz
          AND starts > $2::timestamptz - interval '48 hours'
        ORDER BY starts, id
        LIMIT $4::integer))
  AND canceled_at IS NULL
RETURNING id
