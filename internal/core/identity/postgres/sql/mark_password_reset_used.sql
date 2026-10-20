-- Store.CompleteReset (step 2 of 3): spend the token. Run it after lock_reset_account.sql
-- (the account row first, then the reset row: the order every writer uses) and before
-- set_password.sql (which sets the hash, clears password_change_required and bumps
-- session_generation), so a lost race fails before the password is touched.
-- Args: $1 id uuid (the reset row), $2 used_at timestamptz (the clock's now).
-- Returns the row id (one row; QueryRow: ErrNoRows -> NotFound, meaning already used or gone:
-- the caller answers Invalid(token: invalid)). This statement decides a race: the
-- predicate used_at IS NULL lets exactly one of two concurrent redemptions through, and
-- the lookup that preceded it (peek_password_reset_by_hash.sql) holds no lock.
UPDATE core.password_reset
SET used_at = $2
WHERE id = $1
  AND used_at IS NULL
RETURNING id
