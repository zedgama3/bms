-- Store.PurgeCanceledShifts: volunteers.purge, shift leg: deletes shifts canceled before the
-- cutoff that no sign-up references any more (run after purge_signups.sql). Copies made from
-- them keep their row (copied_from becomes NULL). Exec; count = RowsAffected().
-- Args: $1 cutoff timestamptz, $2 limit integer (batch, 500).
DELETE FROM volunteers.shift
WHERE id IN (
    SELECT s.id
    FROM volunteers.shift s
    WHERE s.canceled_at < $1::timestamptz
      AND NOT EXISTS (SELECT 1 FROM volunteers.signup su WHERE su.shift_id = s.id)
    ORDER BY s.id
    LIMIT $2::integer)
