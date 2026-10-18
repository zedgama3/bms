-- Store.ByPerson. Args: $1 person_id uuid. At most one row (ErrNoRows -> NotFound); served
-- by account_person_id_key. Columns: as get_account_by_id.sql (including password_hash).
SELECT id, person_id, email, password_hash, superuser, password_change_required,
       session_generation, disabled_at, last_login_at, password_changed_at, created_at,
       updated_at
FROM core.account
WHERE person_id = $1
