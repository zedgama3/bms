-- Store.List: the admin list, newest first, keyset on (created_at, id) descending.
-- Args: $1 after_created_at timestamptz and $2 after_id uuid (the Cursor; both NULL on the
-- first page), $3 limit integer (pass Query.Limit + 1; when more rows than Limit come back,
-- Next is the key of row Limit and the extra row is dropped).
-- Columns, in order: id uuid, person_id uuid, email text, superuser boolean,
-- password_change_required boolean, session_generation integer, disabled_at timestamptz,
-- last_login_at timestamptz, password_changed_at timestamptz, created_at timestamptz,
-- updated_at timestamptz. That is account.Record without password_hash (the hash is never
-- read for a list); leave Record.PasswordHash zero.
--
-- The cursor is spelled "created_at <= c AND (created_at < c OR id < c_id)", not as a row
-- comparison, for the reason given in core/audit's search_entries.sql: it keeps the index
-- range explicit when the cursor is NULL ('infinity').
SELECT id, person_id, email, superuser, password_change_required, session_generation,
       disabled_at, last_login_at, password_changed_at, created_at, updated_at
FROM core.account
WHERE created_at <= COALESCE($1::timestamptz, 'infinity'::timestamptz)
  AND (created_at < COALESCE($1::timestamptz, 'infinity'::timestamptz)
       OR id < COALESCE($2::uuid, 'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid))
ORDER BY created_at DESC, id DESC
LIMIT $3::integer
