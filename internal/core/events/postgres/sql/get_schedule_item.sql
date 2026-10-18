-- Store.GetScheduleItem: one item of one event; an item of another event is NotFound (F74).
-- Read-only; use Query (zero rows: NotFound).
-- Args: $1 event_id uuid, $2 id uuid. Columns, in order: as list_schedule_items.sql.
SELECT id, event_id, key, label, starts_at, ends_at, position
FROM core.event_schedule_item
WHERE event_id = $1 AND id = $2
