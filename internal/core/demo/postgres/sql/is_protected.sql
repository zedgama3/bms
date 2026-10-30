-- Store.Protected: is this account a demo account? Identity's Guard calls it before every
-- guarded change, so it is one primary-key probe. Read-only; use QueryRow.
-- Args: $1 account_id uuid.
-- Columns, in order: protected boolean (false for an unknown account: F26).
-- Served by demo_account_pkey.
SELECT EXISTS (SELECT 1 FROM core.demo_account WHERE account_id = $1::uuid) AS protected
