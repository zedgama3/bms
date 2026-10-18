-- Store.List: departments for the list screen and pickers. Read-only; use Query. Not paginated
-- (decision 14: tens of rows); the whole table is scanned and sorted.
-- Args: $1 include_archived boolean (the use case sets it true only for a holder of
-- departments.manage; false returns active departments only).
-- Columns, in order: as get_department.sql. Sorted by lower(name), then id (stable).
SELECT id, name, description, contact_email, archived_at, created_at, updated_at
FROM core.department
WHERE $1::boolean OR archived_at IS NULL
ORDER BY lower(name), id
