-- Store.SetTemporaryPassword: an account admin sets a generated password.
-- Args: $1 id uuid, $2 password_hash text, $3 at timestamptz.
-- Sets password_change_required, ends every session. Returns the new session_generation
-- integer (one row; QueryRow: ErrNoRows -> NotFound). The service refuses the superuser
-- account and the actor's own before calling this.
UPDATE core.account
SET password_hash            = $2,
    password_change_required = true,
    password_changed_at      = $3,
    session_generation       = session_generation + 1,
    updated_at               = $3
WHERE id = $1
RETURNING session_generation
