-- Store.Get: one event (the schedule is list_schedule_items.sql). Also the existence check
-- for copy_from. Read-only; use Query (zero rows: NotFound).
-- Args: $1 id uuid.
-- Columns, in order: id uuid, name text, kind text, starts_at timestamptz, ends_at
-- timestamptz, zone text, closed_at timestamptz (NULL = open), created_at timestamptz,
-- updated_at timestamptz.
SELECT id, name, kind, starts_at, ends_at, zone, closed_at, created_at, updated_at
FROM core.event
WHERE id = $1
