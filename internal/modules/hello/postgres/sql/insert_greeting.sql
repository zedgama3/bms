-- Store.Insert. Args: $1 id uuid, $2 person_id uuid, $3 greeting text, $4 created_at timestamptz.
-- updated_at is set to created_at (rows are never updated). Exec; no rows returned.
INSERT INTO hello.greeting (id, person_id, greeting, created_at, updated_at)
VALUES ($1, $2, $3, $4, $4)
