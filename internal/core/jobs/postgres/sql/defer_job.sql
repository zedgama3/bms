-- Store.Defer: a signals.redeliver row (Runner.Defer). Like enqueue_job.sql, but a repeat of
-- the same (name, key) is a no-op in EVERY state, a queued row included (jobs.md: "a
-- repeated Defer of the same (signal, subscriber) is a no-op"): it is not moved. Use Query
-- (RETURNING).
-- Args: $1 id uuid, $2 name text ('signals.redeliver'), $3 key text ('<signal id>:<subscriber>';
-- at most 128 bytes), $4 args jsonb ({"delivery": {...}}), $5 run_at timestamptz
-- (now + backoff), $6 trace_id text (NULL when none), $7 now timestamptz.
-- Columns, in order: id uuid. ZERO rows: the (name, key) already exists: nothing changed.
-- Constraint errors (Invalid, caller bugs): job_nil_uuid_chk, job_name_chk, job_key_chk,
-- job_args_chk, job_trace_id_chk.
-- Served by job_name_key_uidx. Fails 25006 (db.ErrReadOnly) on a read-only copy.
INSERT INTO core.job
    (id, name, key, args, status, attempts, run_at, trace_id, created_at, updated_at)
VALUES
    ($1::uuid, $2::text, $3::text, $4::jsonb, 'queued', 0, $5::timestamptz, $6::text,
     $7::timestamptz, $7::timestamptz)
ON CONFLICT (name, key) WHERE key IS NOT NULL DO NOTHING
RETURNING id
