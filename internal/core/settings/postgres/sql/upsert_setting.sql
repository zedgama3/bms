-- Store.UpsertSetting: Set, one key whose new value is not the default. Use Query (RETURNING)
-- inside the Write transaction, after lock_revision.sql. Exactly one of $2 and $3 is non-NULL
-- (a plain value, or a sealed Secret).
-- Args: $1 key text ("<area>.<key>"), $2 value text (NULL for a Secret), $3 sealed bytea
-- (NULL unless a Secret), $4 updated_at timestamptz (the service clock).
-- Columns, in order: key text. ZERO rows means the stored value already equals this one: no row
-- changed, so the caller records no audit entry for the key and does not count it as a change.
-- One row means the key was inserted or changed. A sealed value carries a fresh random nonce,
-- so a Secret always counts as a change.
-- Constraint errors (*db.Error.Constraint, all Invalid; field = the setting key):
-- setting_value_xor_sealed_chk (a bug in the caller), setting_key_chk (a key outside the
-- grammar), setting_value_length_chk (too_long), setting_value_chars_chk (control_chars),
-- setting_sealed_length_chk (a bug in the caller).
INSERT INTO core.setting (key, value, sealed, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    sealed = EXCLUDED.sealed,
    updated_at = EXCLUDED.updated_at
WHERE (core.setting.value, core.setting.sealed) IS DISTINCT FROM (EXCLUDED.value, EXCLUDED.sealed)
RETURNING key
