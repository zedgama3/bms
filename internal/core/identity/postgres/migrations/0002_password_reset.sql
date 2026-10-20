-- identity 0002: the forgotten-password token (core.password_reset). Schema "core" exists.
-- Creates one new table and one index; touches no existing table, so it is safe on any
-- database with data and takes no lock on core.account beyond the foreign-key check.
--
-- The table holds a credential: token_hash is SHA-256 of a one-time reset token. The token
-- itself is never stored, logged or exported. Only the newest token of an account is valid:
-- ReplaceResetToken deletes the account's earlier rows (delete_account_password_resets.sql)
-- before it inserts.
-- Retention: a row lives until it is used or expires, and goes at the next reset request for
-- that account (delete_account_password_resets.sql), at the retention job
-- (purge_password_resets.sql), or with its account (ON DELETE CASCADE, so DeletePerson
-- needs no extra statement). ExportPerson does not include it: a credential, nothing about
-- the person.
--
-- Timestamps come from the service's clock, so none has a default. The spec gives the table
-- no updated_at: a row is inserted once and only used_at changes, once.

CREATE TABLE core.password_reset (
    id          uuid        NOT NULL,
    account_id  uuid        NOT NULL,
    -- SHA-256 of the token: exactly 32 bytes. Redeemed by looking this up.
    token_hash  bytea       NOT NULL,
    expires_at  timestamptz NOT NULL,
    -- Set once, by mark_password_reset_used.sql; a replay of a used token answers "invalid".
    used_at     timestamptz,
    created_at  timestamptz NOT NULL,
    CONSTRAINT password_reset_pkey PRIMARY KEY (id),
    CONSTRAINT password_reset_token_hash_key UNIQUE (token_hash),
    CONSTRAINT password_reset_account_id_fkey FOREIGN KEY (account_id)
        REFERENCES core.account (id) ON DELETE CASCADE,
    CONSTRAINT password_reset_token_hash_length_chk CHECK (octet_length(token_hash) = 32),
    CONSTRAINT password_reset_expires_at_chk CHECK (expires_at > created_at),
    -- The all-zero UUID means "none" in Go; it must never be stored.
    CONSTRAINT password_reset_nil_uuid_chk CHECK (
        id         <> '00000000-0000-0000-0000-000000000000'
        AND account_id <> '00000000-0000-0000-0000-000000000000')
);

-- Serves the per-account delete (a new request, and the cascade from core.account). The
-- redeem lookup is served by password_reset_token_hash_key, the used mark by the primary key.
CREATE INDEX password_reset_account_id_idx ON core.password_reset (account_id);

COMMENT ON TABLE core.password_reset IS
    'One-time forgotten-password tokens (identity), one live row per account at most. Holds a credential (the token''s SHA-256), never the token. Retention: removed at the account''s next request, by purge_password_resets.sql, or with the account (cascade); no retention setting. Export: none.';
COMMENT ON COLUMN core.password_reset.id IS 'sensitivity: internal (also the idempotency key of the reset e-mail)';
COMMENT ON COLUMN core.password_reset.account_id IS 'sensitivity: internal (whose reset it is; cascades on account deletion)';
COMMENT ON COLUMN core.password_reset.token_hash IS 'sensitivity: sensitive (a credential: SHA-256 of the one-time token; the token is never stored, logged or exported)';
COMMENT ON COLUMN core.password_reset.expires_at IS 'sensitivity: internal (now + accounts.password_reset_lifetime)';
COMMENT ON COLUMN core.password_reset.used_at IS 'sensitivity: internal (a replay answers invalid)';
COMMENT ON COLUMN core.password_reset.created_at IS 'sensitivity: internal';
