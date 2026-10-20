-- Store.UpsertTermForms: SetTerms, the forms of one term in one language that are set (not
-- reset). Use Query (RETURNING) inside the Write transaction, after lock_revision.sql. One
-- statement per (lang, term); $3 and $4 are parallel arrays of equal length with no NULLs.
-- Args: $1 lang text, $2 term text, $3 forms text[] (singular | plural | singular_title |
-- plural_title; the caller sends each form at most once), $4 values text[] (trimmed),
-- $5 updated_at timestamptz (the service clock).
-- Columns, in order: form text. Only the forms inserted or changed come back (order unspecified; the caller sorts);
-- a form already holding that value is skipped (no change, so no audit field, no bump).
-- Constraint errors (*db.Error.Constraint, Invalid; field = terms.<term>.<form>):
-- terminology_value_length_chk (required / too_long), terminology_value_chars_chk
-- (control_chars); terminology_lang_chk, terminology_term_chk, terminology_form_chk are
-- bugs in the caller (Go validates them first).
INSERT INTO core.terminology (lang, term, form, value, updated_at)
SELECT $1, $2, t.form, t.value, $5
FROM unnest($3::text[], $4::text[]) AS t(form, value)
ON CONFLICT (lang, term, form) DO UPDATE
SET value = EXCLUDED.value,
    updated_at = EXCLUDED.updated_at
WHERE core.terminology.value IS DISTINCT FROM EXCLUDED.value
RETURNING form
