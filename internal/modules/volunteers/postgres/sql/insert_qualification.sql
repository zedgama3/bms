-- Store.InsertQualification. Use QueryRow. Runs in the use case's Write transaction.
-- Args: $1 id uuid, $2 department_id uuid (NULL = org-wide), $3 name text, $4 description text,
--       $5 validity_days integer (NULL = indefinite), $6 created_at timestamptz (also updated_at).
-- Columns, in order (the "qualification columns", used by every qualification query):
--   id uuid, department_id uuid NULL, name text, description text, validity_days integer NULL,
--   archived_at timestamptz NULL, created_at timestamptz, updated_at timestamptz.
-- Constraint errors: qualification_name_key (23505: Conflict, field name, code taken);
--   qualification_department_id_fkey (23503: unknown department); qualification_name_length_chk,
--   qualification_name_chars_chk, qualification_description_length_chk,
--   qualification_validity_days_chk (23514: Invalid).
INSERT INTO volunteers.qualification (id, department_id, name, description, validity_days, created_at, updated_at)
VALUES ($1::uuid, $2::uuid, $3::text, $4::text, $5::integer, $6::timestamptz, $6::timestamptz)
RETURNING id, department_id, name, description, validity_days, archived_at, created_at, updated_at
