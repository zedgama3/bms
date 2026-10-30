-- notify 0002: a message whose recipient has no e-mail address ends 'skipped', a fifth status,
-- not 'failed' (notify.md Amendment 2026-10-30, D57, confirmed D58). A skipped row is final
-- and holds no personal text; its end instant is stored in the existing failed_at column (no
-- new column), its last_error stays 'no_address', and the purge deletes it like a sent or
-- failed row.
--
-- Cost, honestly. The outbox is not small: notify.retention defaults to keep-forever (D30), so
-- sent rows pile up. Three CHECK constraints are replaced. Each new one is a widening of the
-- old one, so every existing row satisfies it by construction; they are added NOT VALID, which
-- takes the ACCESS EXCLUSIVE lock only for the catalog change and does not scan the table
-- (they are enforced for every later write; they are not VALIDATEd, and need not be). The
-- partial index notify_message_finished_idx is dropped and rebuilt, and that does read the
-- table once, under a SHARE lock that blocks writes to core.notify_message for the build
-- (reads continue); the index holds only final rows, and `bms migrate` runs with the sender
-- stopped. Existing rows are not rewritten: a row an earlier sender ended 'failed' with
-- 'no_address' stays failed (D58 changes only future sends).

ALTER TABLE core.notify_message DROP CONSTRAINT notify_message_status_chk;
ALTER TABLE core.notify_message ADD CONSTRAINT notify_message_status_chk
    CHECK (status IN ('queued', 'sending', 'sent', 'failed', 'skipped')) NOT VALID;

-- A skipped row, like a sent one, holds no personal text.
ALTER TABLE core.notify_message DROP CONSTRAINT notify_message_sent_cleared_chk;
ALTER TABLE core.notify_message ADD CONSTRAINT notify_message_sent_cleared_chk
    CHECK (status NOT IN ('sent', 'skipped')
           OR (subject IS NULL AND body IS NULL AND address IS NULL)) NOT VALID;

-- failed_at is the end instant of a failed or a skipped row.
ALTER TABLE core.notify_message DROP CONSTRAINT notify_message_failed_at_chk;
ALTER TABLE core.notify_message ADD CONSTRAINT notify_message_failed_at_chk
    CHECK ((status IN ('failed', 'skipped')) = (failed_at IS NOT NULL)) NOT VALID;

-- The purge (purge_messages.sql) covers skipped rows, so its partial index must too.
DROP INDEX core.notify_message_finished_idx;
CREATE INDEX notify_message_finished_idx
    ON core.notify_message ((coalesce(sent_at, failed_at)))
    WHERE status IN ('sent', 'failed', 'skipped');

COMMENT ON COLUMN core.notify_message.status IS 'sensitivity: internal (queued, sending, sent, failed, skipped)';
