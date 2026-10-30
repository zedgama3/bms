-- Store.ListMail: the signed-in account's own captured mail, newest first (demo.md "Mail
-- capture", Q-DEMO2). OWN-ONLY BY CONSTRUCTION: the recipient filter is the one predicate in
-- this query, there is no parameter, role or branch that widens it, and the address is the
-- login address of the viewer (identity's EmailOf), never one from the request. Read-only; use
-- Query. Not audited (the viewer's own mail).
-- Args: $1 address text (the viewer's login address; the query lower-cases it, so a mixed-case
-- one is fine), $2 limit integer (1..demo.MaxMailList = 50; the caller defaults and validates).
-- Columns, in order, sorted by captured_at DESC, id DESC: id uuid, to_address text, subject
-- text, body text, captured_at timestamptz. (message_id is not returned.)
-- Served by demo_mail_to_idx (lower(to_address), captured_at DESC, id DESC): an index lookup of the
-- viewer's rows only, sorting at most those. At most MaxMail = 200 rows exist in all (see
-- trim_mail.sql), so the page cannot grow and there is no cursor. On a table this small the
-- planner may prefer a sequential scan; that is fine and costs microseconds.
-- Never log the arguments or the rows.
SELECT id, to_address, subject, body, captured_at
FROM core.demo_mail
WHERE lower(to_address) = lower($1::text)
ORDER BY captured_at DESC, id DESC
LIMIT $2::integer
