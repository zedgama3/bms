-- Store.ByID. Args: $1 id uuid. At most one row (pgx.ErrNoRows -> NotFound via db.Wrap).
-- Columns, in order (account.Record): id uuid, person_id uuid, email text, password_hash text,
-- superuser boolean, password_change_required boolean, session_generation integer,
-- disabled_at timestamptz (NULL = enabled), last_login_at timestamptz (NULL = never),
-- password_changed_at timestamptz, created_at timestamptz, updated_at timestamptz.
-- Selects the hash: for the paths that verify a password (ChangePassword) only. The admin reads
-- (GetAccount, disable, enable, temporary password) use get_account_by_id_nohash.sql.
SELECT id, person_id, email, password_hash, superuser, password_change_required,
       session_generation, disabled_at, last_login_at, password_changed_at, created_at,
       updated_at
FROM core.account
WHERE id = $1
