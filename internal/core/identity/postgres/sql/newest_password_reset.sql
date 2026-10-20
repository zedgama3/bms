-- Store.ResetState, step 2 of 2: the creation time of the account's newest reset row,
-- used or not (the cooldown between reset e-mails, D40). Run after lock_reset_account.sql in
-- the same Write transaction, so two concurrent requests cannot both pass the cooldown.
-- Args: $1 account_id uuid. Always one row; created_at is NULL when the account has no
-- reset row.
SELECT max(created_at) AS created_at
FROM core.password_reset
WHERE account_id = $1
