-- Store.ExportByPerson: ExportPerson's read. Read-only; use Query (zero rows: {"grants": []}).
-- Args: $1 person_id uuid.
-- Columns, in order: id uuid, role_id uuid, role_key text (NULL for a regional's own role),
-- role_name text, department_id uuid (NULL: site-wide), ends_at timestamptz (NULL: no end),
-- granted_by uuid (NULL), created_at timestamptz. Ended grants included. Oldest first.
SELECT g.id, g.role_id, r.key, r.name, g.department_id, g.ends_at, g.granted_by, g.created_at
FROM core.role_grant g
JOIN core.role r ON r.id = g.role_id
WHERE g.person_id = $1
ORDER BY g.created_at, g.id
