-- Store.Get: one department, active or archived (the use case decides who may see an archived
-- one). Read-only; use Query (zero rows: NotFound).
-- Args: $1 id uuid.
-- Columns, in order: id uuid, name text, description text, contact_email text ('' = none),
-- archived_at timestamptz (NULL = active), created_at timestamptz, updated_at timestamptz.
SELECT id, name, description, contact_email, archived_at, created_at, updated_at
FROM core.department
WHERE id = $1
