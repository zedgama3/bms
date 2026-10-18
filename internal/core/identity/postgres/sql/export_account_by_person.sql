-- Store.ExportByPerson: ExportPerson's read. Args: $1 person_id uuid. Zero or one row (no
-- row means {"account": null}, not an error: use Query, not QueryRow). Columns: as
-- list_accounts.sql. Never the hash.
SELECT id, person_id, email, superuser, password_change_required, session_generation,
       disabled_at, last_login_at, password_changed_at, created_at, updated_at
FROM core.account
WHERE person_id = $1
