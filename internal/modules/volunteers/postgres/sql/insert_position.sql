-- Store.InsertPosition. Use QueryRow. Write transaction.
-- Args: $1 id uuid, $2 department_id uuid, $3 name text, $4 description text, $5 access text
--       ('open' or 'members'), $6 hide_shift_mates boolean, $7 created_at timestamptz (also
--       updated_at).
-- Columns, in order (the "position columns"): id uuid, department_id uuid, name text,
--   description text, access text, hide_shift_mates boolean, archived_at timestamptz NULL,
--   created_at timestamptz, updated_at timestamptz. Requirements are read separately
--   (list_requirements_by_positions.sql).
-- Constraint errors: position_name_key (23505: Conflict, field name, code taken);
--   position_department_id_fkey (23503: unknown department); position_name_length_chk,
--   position_name_chars_chk, position_description_length_chk, position_access_chk (23514:
--   Invalid; access: one_of).
INSERT INTO volunteers.position (id, department_id, name, description, access, hide_shift_mates, created_at, updated_at)
VALUES ($1::uuid, $2::uuid, $3::text, $4::text, $5::text, $6::boolean, $7::timestamptz, $7::timestamptz)
RETURNING id, department_id, name, description, access, hide_shift_mates, archived_at, created_at, updated_at
