-- Store.TouchLogin (SignIn). Args: $1 id uuid, $2 at timestamptz. Exec. Sets last_login_at
-- only: a login is not an account change, so updated_at stays. db.ErrReadOnly on a read-only
-- copy is ignored by the caller (F21). RowsAffected 0 means the account vanished: ignore.
UPDATE core.account
SET last_login_at = $2
WHERE id = $1
