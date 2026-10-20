-- Store.PutOverride: save an override, or replace the existing one. Use QueryRow (RETURNING)
-- inside the Write transaction that also records notify.template.changed.
-- Args: $1 name text, $2 language text, $3 subject text (the subject template),
-- $4 body text (the body template), $5 updated_by uuid (the actor; NULL for none),
-- $6 updated_at timestamptz (the service clock).
-- Columns, in order: name text, language text, updated_at timestamptz.
-- Constraint errors (*db.Error.Constraint, all Invalid): notify_template_subject_chk
-- (field subject: required, too_long, control_chars), notify_template_body_chk (field body:
-- required, too_long, control_chars), notify_template_language_chk (field language:
-- malformed), notify_template_name_chk (a bug: GetTemplate has found the name registered),
-- notify_template_updated_by_fkey (an actor that is not a person), notify_template_updated_by_nil_chk.
INSERT INTO core.notify_template (name, language, subject, body, updated_by, updated_at)
VALUES ($1::text, $2::text, $3::text, $4::text, $5::uuid, $6::timestamptz)
ON CONFLICT (name, language) DO UPDATE
SET subject = EXCLUDED.subject,
    body = EXCLUDED.body,
    updated_by = EXCLUDED.updated_by,
    updated_at = EXCLUDED.updated_at
RETURNING name, language, updated_at
