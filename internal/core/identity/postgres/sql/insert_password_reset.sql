-- Store.ReplaceResetToken, step 3 of 3: store the new token's hash. Run after
-- delete_account_password_resets.sql in the same transaction.
-- Args: $1 id uuid (the row id; also the e-mail's idempotency key), $2 account_id uuid,
-- $3 token_hash bytea (SHA-256 of the token, exactly 32 bytes), $4 expires_at timestamptz,
-- $5 created_at timestamptz (the clock's now). Use Exec.
-- Errors: password_reset_token_hash_key (23505, Conflict: a hash collision, which means the
-- random source is broken), password_reset_token_hash_length_chk and
-- password_reset_expires_at_chk (23514, Invalid), password_reset_account_id_fkey (23503,
-- the account vanished; step 1 makes that impossible inside the transaction).
INSERT INTO core.password_reset (id, account_id, token_hash, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5)
