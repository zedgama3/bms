-- notify 0001: the outbox (core.notify_message) and the admin's template overrides
-- (core.notify_template) (notify.md, "Data"). Schema "core" is created by the migrator.
-- Creates two new tables and touches no existing one, so it is safe on any database. The one
-- reference into another set's table is core.person (people runs first in db's fixed set
-- order: identity, people, authz, departments, events, audit, settings, jobs, notify, files).
--
-- Personal data. core.notify_message.address (an explicit recipient, SendTo), .subject and
-- .body (rendered text; a body may hold a one-time link) are personal. The text lives until
-- the message is sent or expires, whichever comes first (notify.md, "Amendments": retry,
-- expiry and the text of failed rows):
--   * a row that reaches 'sent' has all three cleared in the same UPDATE, and
--     notify_message_sent_cleared_chk makes a sent row holding any of them impossible;
--   * every row has expires_at (created_at + the caller's TTL, at most MaxAge = 7 days). From
--     that time on expire_stale_messages.sql ends the row ('failed', token 'expired', unless
--     it already failed) and clears the three columns, whatever the status and whatever
--     notify.retention is. A failed row keeps its text until then so an admin can retry it.
-- What stays after that is a status row (ids, template name, status, attempts, an error
-- token), which the purge deletes once it is older than the notify.retention setting (0 =
-- keep forever, D30). Rows of a person are deleted with that person
-- (delete_person_messages.sql, run in people's transaction), because the foreign key to
-- core.person is RESTRICT.
--
-- Timestamps come from the service's clock, so none has a default. The spec gives
-- core.notify_message no updated_at (a row moves forward through its status columns) and
-- core.notify_template no created_at (a row is rewritten in place; audit keeps the history),
-- as settings does for its tables.

CREATE TABLE core.notify_message (
    id              uuid        NOT NULL,
    -- The scope that sent it: a module name or a core area.
    owner           text        NOT NULL,
    -- "<owner>.<ident>", the full template name.
    template        text        NOT NULL,
    -- Optional idempotency key; unique per (owner, key, person_id) while it is set. Kept on
    -- a final row so a repeated Send is still a no-op until the purge deletes the row.
    key             text,
    -- The recipient; NULL for SendTo (an explicit address).
    person_id       uuid,
    -- The explicit address of a SendTo row until it is sent or expires; NULL when the address
    -- is resolved from the person at send time.
    address         text,
    -- The tag the text was rendered in.
    language        text        NOT NULL,
    -- Rendered text; NULL once the row is sent or expired (a failed row keeps it until then).
    subject         text,
    body            text,
    status          text        NOT NULL,
    -- Delivery attempts counted so far (the lease adds one; a shutdown release takes it back).
    attempts        integer     NOT NULL DEFAULT 0,
    -- Due time for a queued row.
    next_attempt_at timestamptz NOT NULL,
    -- Set exactly while status = 'sending'.
    leased_until    timestamptz,
    -- A short token (connect, auth, timeout, smtp_4xx, rejected, no_address, no_smtp,
    -- canceled, expired). Never the server's reply text, which may echo an address.
    last_error      text,
    created_at      timestamptz NOT NULL,
    -- created_at + the message's TTL (at most 7 days): after it the text and the address are
    -- cleared and the row can no longer be retried or leased.
    expires_at      timestamptz NOT NULL,
    sent_at         timestamptz,
    failed_at       timestamptz,
    CONSTRAINT notify_message_pkey PRIMARY KEY (id),
    CONSTRAINT notify_message_person_id_fkey FOREIGN KEY (person_id)
        REFERENCES core.person (id) ON DELETE RESTRICT,
    -- The all-zero UUID means "none" in Go; it must never be stored.
    CONSTRAINT notify_message_nil_uuid_chk CHECK (
        id <> '00000000-0000-0000-0000-000000000000'
        AND (person_id IS NULL OR person_id <> '00000000-0000-0000-0000-000000000000')),
    -- Bytes, to match the Go side; the grammars are the signals.Name one (owner, ident).
    CONSTRAINT notify_message_owner_chk CHECK (
        octet_length(owner) <= 32 AND owner ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$'),
    CONSTRAINT notify_message_template_chk CHECK (
        octet_length(template) <= 96
        AND template ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*\.[a-z][a-z0-9]*(-[a-z0-9]+)*$'),
    CONSTRAINT notify_message_key_chk CHECK (
        key IS NULL OR (octet_length(key) <= 128 AND key ~ '^[A-Za-z0-9._:-]{1,128}$')),
    CONSTRAINT notify_message_address_chk CHECK (
        address IS NULL OR (octet_length(address) BETWEEN 3 AND 254 AND address !~ '[[:cntrl:]]')),
    CONSTRAINT notify_message_language_chk CHECK (
        octet_length(language) <= 35 AND language ~ '^[a-z]{2,3}(-[A-Za-z0-9]{1,8})*$'),
    -- The subject is one line (it becomes a header); the body keeps tab, newline and CR.
    CONSTRAINT notify_message_subject_chk CHECK (
        subject IS NULL OR (octet_length(subject) BETWEEN 1 AND 256 AND subject !~ '[[:cntrl:]]')),
    CONSTRAINT notify_message_body_chk CHECK (
        body IS NULL OR (octet_length(body) BETWEEN 1 AND 65536
                         AND body !~ '[\x01-\x08\x0b\x0c\x0e-\x1f\x7f]')),
    CONSTRAINT notify_message_status_chk CHECK (status IN ('queued', 'sending', 'sent', 'failed')),
    CONSTRAINT notify_message_attempts_chk CHECK (attempts >= 0),
    CONSTRAINT notify_message_last_error_chk CHECK (
        last_error IS NULL OR (octet_length(last_error) <= 32 AND last_error ~ '^[a-z0-9_]+$')),
    -- A person or an explicit address, never both.
    CONSTRAINT notify_message_recipient_chk CHECK (person_id IS NULL OR address IS NULL),
    -- A waiting row can be sent: it has its text and somebody to send it to.
    CONSTRAINT notify_message_live_chk CHECK (
        status NOT IN ('queued', 'sending')
        OR (subject IS NOT NULL AND body IS NOT NULL AND (person_id IS NOT NULL OR address IS NOT NULL))),
    -- A sent row holds no personal text. A failed row keeps it until it expires.
    CONSTRAINT notify_message_sent_cleared_chk CHECK (
        status <> 'sent'
        OR (subject IS NULL AND body IS NULL AND address IS NULL)),
    -- An expired row is cleared (the token 'expired' is only ever written with the clearing).
    CONSTRAINT notify_message_expired_cleared_chk CHECK (
        last_error IS DISTINCT FROM 'expired'
        OR (subject IS NULL AND body IS NULL AND address IS NULL)),
    -- Subject and body are set and cleared together, and an address never outlives them, so
    -- "subject IS NOT NULL" means "this row still holds personal text".
    CONSTRAINT notify_message_text_chk CHECK (
        (subject IS NULL) = (body IS NULL) AND (address IS NULL OR subject IS NOT NULL)),
    CONSTRAINT notify_message_expires_chk CHECK (expires_at > created_at),
    CONSTRAINT notify_message_lease_chk CHECK ((status = 'sending') = (leased_until IS NOT NULL)),
    CONSTRAINT notify_message_sent_at_chk CHECK ((status = 'sent') = (sent_at IS NOT NULL)),
    CONSTRAINT notify_message_failed_at_chk CHECK ((status = 'failed') = (failed_at IS NOT NULL))
);

-- Idempotent enqueue: one row per (owner, key, recipient) while a key is set. A SendTo row
-- has person_id NULL, and NULLs are distinct, so this index never applies to it (the
-- superuser alert sends one key to several alert addresses). These are partial unique
-- INDEXES, so a violation would name the index; enqueue_message.sql does nothing on a
-- conflict with either.
CREATE UNIQUE INDEX notify_message_owner_key_person_uidx
    ON core.notify_message (owner, key, person_id)
    WHERE key IS NOT NULL;

-- The same for an explicit address, while the address is still held: once it is cleared (sent
-- or expired) a repeat under the same key inserts again (accepted, notify.md Amendments 6).
CREATE UNIQUE INDEX notify_message_owner_key_address_uidx
    ON core.notify_message (owner, key, address)
    WHERE key IS NOT NULL AND address IS NOT NULL;

-- The lease (lease_messages.sql) and the stale sweep: only waiting rows, so the index stays
-- as small as the queue however many sent rows pile up.
CREATE INDEX notify_message_due_idx
    ON core.notify_message (status, next_attempt_at)
    WHERE status IN ('queued', 'sending');

-- The foreign key's RESTRICT check, export and deletion of one person's rows.
CREATE INDEX notify_message_person_id_idx ON core.notify_message (person_id);

-- The outbox list (list_outbox.sql) and the cheap counts of open rows
-- (count_open_messages_by_status.sql, an index-only scan thanks to INCLUDE): everything not
-- yet sent, newest first. The spec names (created_at) "for purge"; the purge needs the
-- completion time instead (next index) and the list needs this one, which stays small
-- because sent rows, the bulk, are not in it.
CREATE INDEX notify_message_outbox_idx
    ON core.notify_message (created_at DESC, id DESC) INCLUDE (status)
    WHERE status <> 'sent';

-- The expiry sweep (expire_stale_messages.sql): only rows that still hold text, so it stays
-- as small as the live queue plus the failed rows awaiting a retry.
CREATE INDEX notify_message_expiry_idx
    ON core.notify_message (expires_at)
    WHERE subject IS NOT NULL;

-- The purge (purge_messages.sql): final rows by completion time.
CREATE INDEX notify_message_finished_idx
    ON core.notify_message ((coalesce(sent_at, failed_at)))
    WHERE status IN ('sent', 'failed');

COMMENT ON TABLE core.notify_message IS 'sensitivity: personal (the outbox; address, subject and body are personal and exist only until the row is sent or expires; status rows are internal and live until notify.retention)';
COMMENT ON COLUMN core.notify_message.owner IS 'sensitivity: internal (the scope that sent the message)';
COMMENT ON COLUMN core.notify_message.template IS 'sensitivity: internal (a template name)';
COMMENT ON COLUMN core.notify_message.key IS 'sensitivity: internal (a caller''s idempotency key; callers keep nothing personal in it)';
COMMENT ON COLUMN core.notify_message.person_id IS 'sensitivity: internal (the recipient; NULL for an explicit address)';
COMMENT ON COLUMN core.notify_message.address IS 'sensitivity: personal (an explicit recipient address, SendTo only; set NULL when the row is sent or expires, enforced by notify_message_sent_cleared_chk and notify_message_expired_cleared_chk; deleted with the row by the purge and by DeletePerson)';
COMMENT ON COLUMN core.notify_message.language IS 'sensitivity: internal (the tag the text was rendered in)';
COMMENT ON COLUMN core.notify_message.subject IS 'sensitivity: personal (rendered subject; NULL once the row is sent or expired; never listed to admins)';
COMMENT ON COLUMN core.notify_message.body IS 'sensitivity: personal (rendered body, may hold a one-time link; NULL once the row is sent or expired; never listed to admins, never logged)';
COMMENT ON COLUMN core.notify_message.expires_at IS 'sensitivity: internal (created_at plus the message TTL; the latest time the personal text is cleared)';
COMMENT ON COLUMN core.notify_message.status IS 'sensitivity: internal (queued, sending, sent, failed)';
COMMENT ON COLUMN core.notify_message.last_error IS 'sensitivity: internal (a token, never the server''s reply text)';

-- One row per (template, language) an admin has overridden; a reset deletes the row. A row
-- for a name no process registers (a disabled module) is kept and shown as "not available".
CREATE TABLE core.notify_template (
    name       text        NOT NULL,
    language   text        NOT NULL,
    -- The subject and body TEMPLATES (text/template source), not rendered text.
    subject    text        NOT NULL,
    body       text        NOT NULL,
    -- The settings admin who saved it; NULL when that person is deleted.
    updated_by uuid,
    updated_at timestamptz NOT NULL,
    CONSTRAINT notify_template_pkey PRIMARY KEY (name, language),
    CONSTRAINT notify_template_updated_by_fkey FOREIGN KEY (updated_by)
        REFERENCES core.person (id) ON DELETE SET NULL,
    CONSTRAINT notify_template_updated_by_nil_chk CHECK (
        updated_by IS NULL OR updated_by <> '00000000-0000-0000-0000-000000000000'),
    CONSTRAINT notify_template_name_chk CHECK (
        octet_length(name) <= 96
        AND name ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*\.[a-z][a-z0-9]*(-[a-z0-9]+)*$'),
    -- A concrete tag: an override is per language, never for "" (the site default).
    CONSTRAINT notify_template_language_chk CHECK (
        octet_length(language) <= 35 AND language ~ '^[a-z]{2,3}(-[A-Za-z0-9]{1,8})*$'),
    CONSTRAINT notify_template_subject_chk CHECK (
        octet_length(subject) BETWEEN 1 AND 1024 AND subject !~ '[[:cntrl:]]'),
    CONSTRAINT notify_template_body_chk CHECK (
        octet_length(body) BETWEEN 1 AND 16384
        AND body !~ '[\x01-\x08\x0b\x0c\x0e-\x1f\x7f]')
);

COMMENT ON TABLE core.notify_template IS 'sensitivity: internal (a regional''s wording; nothing personal)';
COMMENT ON COLUMN core.notify_template.subject IS 'sensitivity: internal (a subject template, not rendered text)';
COMMENT ON COLUMN core.notify_template.body IS 'sensitivity: internal (a body template, not rendered text)';
COMMENT ON COLUMN core.notify_template.updated_by IS 'sensitivity: internal (the settings admin who saved the override)';
