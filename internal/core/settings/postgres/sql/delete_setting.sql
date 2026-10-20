-- Store.DeleteSetting: Set, one key reset to its default (nil change, or a value equal to the
-- default). Use Query (RETURNING) inside the Write transaction, after lock_revision.sql.
-- Args: $1 key text.
-- Columns, in order: key text. ZERO rows means no row existed (already at the default): no
-- change, no audit entry for the key.
DELETE FROM core.setting
WHERE key = $1
RETURNING key
