-- Store.ResetByHash (CompletePasswordReset): find the token's row WITHOUT locking anything.
-- The redemption itself takes its locks later, in the one order every writer of
-- core.password_reset uses: the account row first (lock_reset_account.sql), then the reset
-- row (mark_password_reset_used.sql). Locking the reset row here, before the account row,
-- would deadlock with ReplaceResetToken, which takes them the other way round.
-- Args: $1 token_hash bytea (SHA-256 of the token the person presented).
-- At most one row (token_hash is unique); QueryRow: ErrNoRows -> NotFound. Columns, in order:
-- id uuid, account_id uuid, expires_at timestamptz, used_at timestamptz (NULL when unused),
-- created_at timestamptz, account_disabled boolean.
-- Never selects token_hash back. The answer is advisory: mark_password_reset_used.sql
-- decides a race through used_at IS NULL.
SELECT r.id, r.account_id, r.expires_at, r.used_at, r.created_at,
       a.disabled_at IS NOT NULL AS account_disabled
FROM core.password_reset AS r
JOIN core.account AS a ON a.id = r.account_id
WHERE r.token_hash = $1
