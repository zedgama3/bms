-- Store.InsertRole: CreateRole. A regional's own role: key NULL, base false (only migration
-- 0001 sets either). Use Exec. Inside the Write transaction, after lock_invariants.sql.
-- Args: $1 id uuid, $2 name text, $3 description text, $4 scope text ('site' or
-- 'department'), $5 created_at timestamptz, $6 updated_at timestamptz.
-- Constraint errors (*db.Error.Constraint): role_name_key (Conflict, name taken);
-- role_name_length_chk, role_description_length_chk, role_scope_chk (Invalid).
INSERT INTO core.role (id, key, name, description, scope, base, created_at, updated_at)
VALUES ($1, NULL, $2, $3, $4, false, $5, $6)
