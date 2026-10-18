-- Store.Rehash: SignIn upgrades a hash made with weaker parameters (decision 5). The same
-- password, so: no generation bump, no password_changed_at, no updated_at.
-- Args: $1 id uuid, $2 new_hash text, $3 old_hash text (the hash that was verified). The
-- compare-and-swap on $3 keeps a rehash from overwriting a password changed since the
-- verification. Exec; RowsAffected 0 is not an error (someone changed the password, or the
-- account is gone): ignore it. db.ErrReadOnly is ignored by the caller (read-only copy).
UPDATE core.account
SET password_hash = $2
WHERE id = $1
  AND password_hash = $3
