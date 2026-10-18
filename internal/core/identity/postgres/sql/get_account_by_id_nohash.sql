-- Store.ByIDNoHash: the admin reads (GetAccount, and the target load of disable, enable and
-- temporary password). Args: $1 id uuid. At most one row (pgx.ErrNoRows -> NotFound via
-- db.Wrap). Columns: as list_accounts.sql, in order: id uuid, person_id uuid, email text,
-- superuser boolean, password_change_required boolean, session_generation integer,
-- disabled_at timestamptz (NULL = enabled), last_login_at timestamptz (NULL = never),
-- password_changed_at timestamptz, created_at timestamptz, updated_at timestamptz. That is
-- account.Record without password_hash: the hash is read only where a password is verified.
SELECT id, person_id, email, superuser, password_change_required, session_generation,
       disabled_at, last_login_at, password_changed_at, created_at, updated_at
FROM core.account
WHERE id = $1
