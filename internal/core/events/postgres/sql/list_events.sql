-- Store.List: events newest first. Read-only; use Query. Not paginated (decision 14).
-- Args: $1 closed boolean (NULL: every event; true: closed only; false: open only), mapped
-- by the use case from StatusAny, StatusClosed, StatusOpen.
-- Columns, in order: as get_event.sql. Sorted by starts_at descending, then id (stable).
SELECT id, name, kind, starts_at, ends_at, zone, closed_at, created_at, updated_at
FROM core.event
WHERE $1::boolean IS NULL OR (closed_at IS NOT NULL) = $1::boolean
ORDER BY starts_at DESC, id
