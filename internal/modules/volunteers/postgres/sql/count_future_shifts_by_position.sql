-- Store.CountFutureShifts: ArchivePosition's in_use check (F50): non-canceled shifts of the
-- position that have not ended yet. Use QueryRow. Run in the same Write transaction as
-- archive_position.sql, after get_position_for_update.sql.
-- Args: $1 position_id uuid, $2 now timestamptz.
-- Columns: n bigint (scan into int64). Served by shift_position_idx.
SELECT count(*) AS n
FROM volunteers.shift
WHERE position_id = $1::uuid
  AND canceled_at IS NULL
  AND ends > $2::timestamptz
