-- Store.Disable (DisableAccount). Args: $1 id uuid, $2 at timestamptz.
-- Sets disabled_at and ends every session in one statement. Exec; RowsAffected is 1 when it
-- changed the account and 0 when the account was already disabled (or gone): the service
-- loads the account first (ByID: NotFound, superuser and self checks), so 0 means "already
-- done" (decision 11): return nil, record nothing.
UPDATE core.account
SET disabled_at        = $2,
    session_generation = session_generation + 1,
    updated_at         = $2
WHERE id = $1
  AND disabled_at IS NULL
