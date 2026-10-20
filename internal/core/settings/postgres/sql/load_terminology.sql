-- Store.LoadTerminology: every override, for the snapshot and the terms document. Read-only;
-- use Query. Not paginated: at most languages x terms x 4 forms rows (tens), the whole table
-- is the snapshot. Rows for a language or term this build does not know are returned; the
-- caller ignores them.
-- Args: none.
-- Columns, in order: lang text, term text, form text, value text. Sorted by (lang, term,
-- form), the primary key (stable).
SELECT lang, term, form, value
FROM core.terminology
ORDER BY lang, term, form
