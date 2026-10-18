-- Store.ByCanonicalEmail (SignIn, SignUp checks). Args: $1 email text, the address as
-- submitted: the comparison is lower() in SQL so Go and SQL cannot disagree about the
-- canonical form. Served by account_email_canonical_key. At most one row (ErrNoRows ->
-- NotFound). Columns: as get_account_by_id.sql (including password_hash).
SELECT id, person_id, email, password_hash, superuser, password_change_required,
       session_generation, disabled_at, last_login_at, password_changed_at, created_at,
       updated_at
FROM core.account
WHERE email_canonical = lower($1)
