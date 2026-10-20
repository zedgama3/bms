-- Retention job (identity, M4) and housekeeping: delete reset rows that can no longer be
-- redeemed, used or expired. A live token is never touched. The table holds at most one row
-- per account, so a scan is cheap and there is no index on expires_at.
-- Args: $1 now timestamptz (the clock's now). Use Exec; the command tag is the count purged.
DELETE FROM core.password_reset
WHERE used_at IS NOT NULL
   OR expires_at <= $1
