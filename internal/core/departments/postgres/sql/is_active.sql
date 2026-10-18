-- Store.Active: Directory.Active and authz.DepartmentSource. Read-only; use QueryRow. Always
-- returns one row, so unknown and archived are indistinguishable (privacy: never leak which).
-- Args: $1 id uuid. Columns: active boolean (true = exists and not archived).
SELECT EXISTS (SELECT 1 FROM core.department WHERE id = $1 AND archived_at IS NULL)
