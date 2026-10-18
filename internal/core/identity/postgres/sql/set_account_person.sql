-- Store.SetPerson (LinkAccount): re-point the account to another person. One statement; the
-- old row is read and locked in the same statement so the returned previous person is the one
-- the update replaced. Args: $1 id uuid (the account), $2 person_id uuid (the new person),
-- $3 at timestamptz (updated_at). Returns the previous person_id (one row; use QueryRow:
-- ErrNoRows -> NotFound). A person who already has an account violates account_person_id_key
-- (23505 -> Conflict with *db.Error.Constraint set). Sessions follow: Authenticate re-reads
-- the account row on every request.
WITH old AS (
    SELECT id, person_id FROM core.account WHERE id = $1 FOR UPDATE
)
UPDATE core.account a
SET person_id = $2,
    updated_at = $3
FROM old
WHERE a.id = old.id
RETURNING old.person_id
