-- Store.DeleteTermForms: SetTerms, the forms of one term in one language reset to the default.
-- Use Query (RETURNING) inside the Write transaction, after lock_revision.sql.
-- Args: $1 lang text, $2 term text, $3 forms text[].
-- Columns, in order: form text. Only the forms that had an override come back (order
-- unspecified; the caller sorts); forms already at the default are not in the result (no change, no audit field).
DELETE FROM core.terminology
WHERE lang = $1 AND term = $2 AND form = ANY($3::text[])
RETURNING form
