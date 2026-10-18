-- Store.StateByID: the one read Authenticate makes per request. No write, no transaction.
-- Args: $1 id uuid. At most one row (ErrNoRows -> NotFound, which Authenticate turns into
-- Unauthenticated). Columns, in order (account.State): person_id uuid, superuser boolean,
-- password_change_required boolean, session_generation integer, disabled boolean.
-- Never selects the hash, the address or any timestamp.
SELECT person_id, superuser, password_change_required, session_generation,
       disabled_at IS NOT NULL AS disabled
FROM core.account
WHERE id = $1
