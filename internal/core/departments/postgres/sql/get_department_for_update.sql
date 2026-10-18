-- Store.GetForUpdate: Update, Archive and Restore lock the row, then check its state.
-- Use inside the Write transaction; zero rows: NotFound.
-- Args: $1 id uuid. Columns: as get_department.sql.
SELECT id, name, description, contact_email, archived_at, created_at, updated_at
FROM core.department
WHERE id = $1
FOR UPDATE
