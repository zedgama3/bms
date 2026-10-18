-- Store.GetForUpdate: Update, Close, Reopen, RunSetup and every schedule change lock the event
-- row, then check its state. This lock also serialises insert_schedule_item.sql's position and
-- the MaxScheduleItems count. Use inside the Write transaction; zero rows: NotFound.
-- Args: $1 id uuid. Columns: as get_event.sql.
SELECT id, name, kind, starts_at, ends_at, zone, closed_at, created_at, updated_at
FROM core.event
WHERE id = $1
FOR UPDATE
