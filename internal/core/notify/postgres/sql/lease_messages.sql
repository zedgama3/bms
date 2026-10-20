-- Store.Lease: SendOnce, step 2. In one short Write transaction, take up to $2 due rows and
-- mark them sending. Use Query inside the Write transaction. Due means queued with
-- next_attempt_at <= now, or sending with an expired lease (a crashed sender: the row is
-- re-leased and may be sent a second time, decision 5), AND not expired itself: a row with
-- expires_at <= now is never handed to the sender (a reset link that is already dead must not
-- go out). The test is in this statement, so no row is leased past its expiry. Such rows stay
-- as they are until expire_stale_messages.sql ends them, which Run does every PurgeInterval;
-- the sender does not need a mark_expired step. FOR UPDATE SKIP LOCKED lets two
-- senders on one database each take different rows (F29). Each lease counts one attempt.
-- Args: $1 now timestamptz, $2 limit integer (notify.MaxBatch), $3 leased_until timestamptz
-- (now + Lease). leased_until is set from this parameter, never from now(), and is returned, so
-- Go holds the exact stored value (timestamptz keeps microseconds, which pgx round-trips; a
-- value with nanoseconds comes back rounded, so always use the returned one). It is the fence
-- for mark_sent, mark_failed, requeue_message and release_message. Two leases of one row must
-- not get the same value: $3 comes from a clock that moves forward (a frozen fake clock in a
-- test can repeat it).
-- Columns, in order, sorted by next_attempt_at, id: id uuid, owner text, template text,
-- person_id uuid (NULL for SendTo), address text (NULL unless SendTo; personal, never log
-- it), subject text (personal), body text (personal), attempts integer (already counting
-- this attempt; used for the backoff and MaxAttempts decision, no longer the fence),
-- next_attempt_at timestamptz, leased_until timestamptz (the fence: pass it back unchanged),
-- expires_at timestamptz (when the text is cleared at the latest).
-- Served by notify_message_due_idx. Fails 25006 (db.ErrReadOnly) on a read-only copy.
-- Constraint errors: none reachable.
WITH due AS (
    SELECT id
    FROM core.notify_message
    WHERE expires_at > $1::timestamptz
      AND ((status = 'queued' AND next_attempt_at <= $1::timestamptz)
           OR (status = 'sending' AND leased_until < $1::timestamptz))
    ORDER BY next_attempt_at, id
    LIMIT $2::integer
    FOR UPDATE SKIP LOCKED
), leased AS (
    UPDATE core.notify_message AS m
    SET status = 'sending',
        leased_until = $3::timestamptz,
        attempts = m.attempts + 1
    FROM due
    WHERE m.id = due.id
    RETURNING m.id, m.owner, m.template, m.person_id, m.address, m.subject, m.body,
              m.attempts, m.next_attempt_at, m.leased_until, m.expires_at
)
SELECT id, owner, template, person_id, address, subject, body, attempts, next_attempt_at,
       leased_until, expires_at
FROM leased
ORDER BY next_attempt_at, id
