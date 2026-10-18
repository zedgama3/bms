-- Store.Enable (EnableAccount). Args: $1 id uuid, $2 at timestamptz.
-- Clears disabled_at. The generation is not bumped: disabling already revoked every older
-- token. Exec; RowsAffected 1 = changed, 0 = already enabled (or gone): as disable_account.sql.
UPDATE core.account
SET disabled_at = NULL,
    updated_at  = $2
WHERE id = $1
  AND disabled_at IS NOT NULL
