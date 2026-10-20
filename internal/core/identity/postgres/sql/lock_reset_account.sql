-- Store.ReplaceResetToken (step 1 of 3) and Store.CompleteReset (step 1 of 3): take the
-- account's row lock. Every writer of core.password_reset takes the account row first and
-- the reset rows after it, in that one order, so they cannot deadlock.
--   ReplaceResetToken: run the three steps in one Write transaction, in order. Without the
--   lock, two concurrent requests for one account could both delete nothing and both
--   insert, leaving two live tokens ("newest only" broken).
--   CompleteReset: run it before mark_password_reset_used.sql and set_password.sql, so the
--   disabled flag and the superuser flag read here are the ones the password write sees:
--   an account an admin disabled after the caller's unlocked lookup is refused.
-- FOR NO KEY UPDATE conflicts with itself and with the UPDATEs the other identity
-- statements make on the row, but not with the foreign-key check of the insert.
-- Args: $1 account_id uuid. One row, or none (ErrNoRows -> NotFound: the account is gone).
-- Columns, in order: id uuid, disabled boolean (disabled_at IS NOT NULL), superuser boolean.
SELECT id, disabled_at IS NOT NULL AS disabled, superuser
FROM core.account
WHERE id = $1
FOR NO KEY UPDATE
