-- Store.ListByEmail: the admin search for one exact address (AccountQuery.Email). Zero or one
-- row; no paging. Args: $1 email text (compared as email_canonical = lower($1)).
-- Columns: as list_accounts.sql (no password_hash).
SELECT id, person_id, email, superuser, password_change_required, session_generation,
       disabled_at, last_login_at, password_changed_at, created_at, updated_at
FROM core.account
WHERE email_canonical = lower($1)
