-- Store.DeleteConsentsByPerson: Delete, before the person row. Use Query; the row count is
-- the count. Idempotent. Rows other people recorded about this person go with it; rows this
-- person recorded about others keep, with recorded_by set NULL when the person row goes.
-- Args: $1 person_id uuid. Columns, in order: purpose text.
DELETE FROM core.person_consent
WHERE person_id = $1
RETURNING purpose
