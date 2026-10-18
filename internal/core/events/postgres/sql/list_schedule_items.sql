-- Store.ListScheduleItems: one event's schedule for Get. Read-only; use Query. At most
-- MaxScheduleItems = 100 rows.
-- Args: $1 event_id uuid.
-- Columns, in order: id uuid, event_id uuid, key text ('' = none), label text, starts_at
-- timestamptz, ends_at timestamptz (NULL = a moment), position integer.
-- Sorted by starts_at, position, id (stable).
SELECT id, event_id, key, label, starts_at, ends_at, position
FROM core.event_schedule_item
WHERE event_id = $1
ORDER BY starts_at, position, id
