-- Store.LoadSettings: every stored value, for the snapshot and for a write's area Checks.
-- Read-only; use Query. Not paginated: one row per setting set away from its default (tens of
-- rows, bounded by the declared keys), and the whole table is the snapshot. Includes keys no
-- process declares; the caller ignores them and never deletes them.
-- Args: none.
-- Columns, in order: key text, value text (NULL for a Secret), sealed bytea (NULL unless a
-- Secret; ciphertext, never log or return it). Sorted by key (stable).
SELECT key, value, sealed
FROM core.setting
ORDER BY key
