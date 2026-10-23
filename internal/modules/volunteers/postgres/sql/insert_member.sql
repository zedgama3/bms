-- Store.InsertMember. Use QueryRow. Write transaction.
-- Args: $1 id uuid, $2 department_id uuid, $3 person_id uuid, $4 added_by uuid (NULL = none),
--       $5 created_at timestamptz (also updated_at).
-- Columns, in order (the "member columns"): id uuid, department_id uuid, person_id uuid,
--   added_by uuid NULL, created_at timestamptz, updated_at timestamptz.
-- Constraint errors: member_department_person_key (23505: Conflict, code duplicate);
--   member_person_id_fkey (23503: Invalid, field person, code unknown);
--   member_department_id_fkey (23503: unknown department).
INSERT INTO volunteers.member (id, department_id, person_id, added_by, created_at, updated_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::timestamptz, $5::timestamptz)
RETURNING id, department_id, person_id, added_by, created_at, updated_at
