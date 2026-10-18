-- Store.SetPassword (ChangePassword): the person chose a new password. One statement, so
-- the generation bump cannot be lost to a concurrent change.
-- Args: $1 id uuid, $2 password_hash text, $3 at timestamptz (the clock's now).
-- Clears password_change_required, sets password_changed_at, ends every other session.
-- Returns the new session_generation integer (one row; use QueryRow: ErrNoRows -> NotFound),
-- which the new token carries.
UPDATE core.account
SET password_hash            = $2,
    password_change_required = false,
    password_changed_at      = $3,
    session_generation       = session_generation + 1,
    updated_at               = $3
WHERE id = $1
RETURNING session_generation
