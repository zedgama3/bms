-- Store.UpdateScheduleItem: replaces key, label, starts_at and ends_at of one item of one
-- event; position is kept. Use Query; zero rows: NotFound (unknown, or an item of another
-- event: F74).
-- Args: $1 event_id uuid, $2 id uuid, $3 key text ('' = none), $4 label text, $5 starts_at
-- timestamptz, $6 ends_at timestamptz (NULL = a moment), $7 updated_at timestamptz.
-- Columns, in order: as list_schedule_items.sql.
-- Constraint errors: as insert_schedule_item.sql.
UPDATE core.event_schedule_item SET
    key = $3, label = $4, starts_at = $5, ends_at = $6, updated_at = $7
WHERE event_id = $1 AND id = $2
RETURNING id, event_id, key, label, starts_at, ends_at, position
