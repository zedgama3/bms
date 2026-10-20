-- Store.ReplaceResetToken, step 2 of 3: remove every reset row of the account: used and
-- expired ones (housekeeping) and the still-live earlier token (newest only, so an older
-- e-mail's link answers invalid). Run after lock_reset_account.sql.
-- Args: $1 account_id uuid. Use Exec; zero rows is not an error.
DELETE FROM core.password_reset
WHERE account_id = $1
