-- Store.Insert: Create. Use Query (RETURNING one row) or Exec. Inside the Write transaction.
-- Args: $1 id uuid, $2 name text, $3 description text, $4 contact_email text ('' = none),
-- $5 created_at timestamptz (also used as updated_at).
-- Columns, in order: as get_department.sql.
-- Constraint errors (*db.Error.Constraint): department_name_key (Conflict, name taken);
-- department_name_length_chk, department_name_chars_chk (Invalid, name);
-- department_description_length_chk (Invalid, description);
-- department_contact_email_length_chk (Invalid, contact_email).
INSERT INTO core.department (id, name, description, contact_email, archived_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, NULL, $5, $5)
RETURNING id, name, description, contact_email, archived_at, created_at, updated_at
