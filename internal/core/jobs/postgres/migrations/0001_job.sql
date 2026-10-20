-- jobs 0001: the job queue (core.job) (jobs.md, "Data"). Schema "core" is created by the
-- migrator. Creates one new table and its indexes and touches no existing object, so it is
-- safe on any database (the table is empty, so the index builds take no noticeable lock).
-- No foreign keys: core.job references no other table (a job about a deleted person runs,
-- finds nothing and ends done; jobs.md "Personal data: none").
--
-- Personal data: none. args holds record references (identifiers, names and times, as a
-- signals.Payload does), never a name, an address or a narrative. A reference may be a
-- person's ID, which is pseudonymous; it can outlive that person for at most the time a
-- final row is kept. Retention: jobs.purge (purge_jobs.sql) deletes final rows (done,
-- failed, canceled) whose finished_at is older than jobs.DoneTTL (7 days); waiting rows live
-- until they run or are canceled. jobs is therefore not a people.Source.
--
-- Timestamps come from the service's clock, so none has a default. Every state change bumps
-- updated_at (the statements in sql/ all take the clock's now).
--
-- One table serves both shapes of job: a scheduled slot is a row whose key is the slot time
-- (RFC 3339, UTC), a deferred job is a row with an optional dedupe key. (name, key) is unique
-- while key is set.

CREATE TABLE core.job (
    id           uuid        NOT NULL,
    -- "<owner>.<ident>", the signals.Name grammar, at most 96 bytes.
    name         text        NOT NULL,
    -- Optional dedupe key. A schedule slot stores the slot time in RFC 3339 UTC, an admin run
    -- "admin:<job id>", a redelivery "<signal id>:<subscriber>".
    key          text,
    -- The JSON form of jobs.Args: {"<ident>": "<uuid>", ...}; for signals.redeliver,
    -- {"delivery": {"signal": {...}, "subscriber": ..., "attempt": ...}}. References only.
    args         jsonb       NOT NULL DEFAULT '{}',
    status       text        NOT NULL,
    -- Leases counted so far; a release takes one back.
    attempts     integer     NOT NULL DEFAULT 0,
    -- Due time: the lease takes queued rows with run_at <= now.
    run_at       timestamptz NOT NULL,
    -- Set exactly while status = 'running'. The fence for every write by a worker: the exact
    -- value lease_jobs.sql returned.
    leased_until timestamptz,
    -- First lease.
    started_at   timestamptz,
    -- Set exactly when status is final (done, failed, canceled).
    finished_at  timestamptz,
    -- A token, never a message: the errs kind in lower case, or panic, no_handler,
    -- lost_lease, gave_up, canceled.
    last_error   text,
    -- The copy that leased it last.
    instance     text,
    -- telemetry.TraceIDFrom at enqueue.
    trace_id     text,
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL,
    CONSTRAINT job_pkey PRIMARY KEY (id),
    -- The all-zero UUID means "none" in Go; it must never be stored.
    CONSTRAINT job_nil_uuid_chk CHECK (id <> '00000000-0000-0000-0000-000000000000'),
    -- Bytes, to match the Go side; all ASCII. The signals.Name grammar.
    CONSTRAINT job_name_chk CHECK (
        octet_length(name) <= 96
        AND name ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*\.[a-z][a-z0-9]*(-[a-z0-9]+)*$'),
    CONSTRAINT job_key_chk CHECK (
        key IS NULL OR (octet_length(key) <= 128 AND key ~ '^[A-Za-z0-9._:-]{1,128}$')),
    -- References are small; this bounds a runaway payload, far above signals.MaxRefs.
    CONSTRAINT job_args_chk CHECK (
        jsonb_typeof(args) = 'object' AND octet_length(args::text) <= 65536),
    CONSTRAINT job_status_chk CHECK (status IN ('queued', 'running', 'done', 'failed', 'canceled')),
    CONSTRAINT job_attempts_chk CHECK (attempts >= 0),
    CONSTRAINT job_lease_chk CHECK ((status = 'running') = (leased_until IS NOT NULL)),
    CONSTRAINT job_finished_chk CHECK (
        (status IN ('done', 'failed', 'canceled')) = (finished_at IS NOT NULL)),
    -- A row that is running or done has been leased.
    CONSTRAINT job_started_chk CHECK (status NOT IN ('running', 'done') OR started_at IS NOT NULL),
    CONSTRAINT job_last_error_chk CHECK (
        last_error IS NULL OR last_error ~ '^[a-z_]{1,32}$'),
    -- config's instance grammar.
    CONSTRAINT job_instance_chk CHECK (
        instance IS NULL
        OR (instance ~ '^[a-z0-9]([a-z0-9-]*[a-z0-9])?$' AND octet_length(instance) BETWEEN 1 AND 64)),
    CONSTRAINT job_trace_id_chk CHECK (trace_id IS NULL OR trace_id ~ '^[0-9a-f]{32}$')
);

-- The dedupe: Enqueue, Defer and the scheduler use ON CONFLICT (name, key) WHERE key IS NOT
-- NULL. A row without a key never conflicts.
CREATE UNIQUE INDEX job_name_key_uidx
    ON core.job (name, key)
    WHERE key IS NOT NULL;

-- The lease (lease_jobs.sql): only waiting and running rows, so the index stays as small as
-- the queue however many final rows pile up.
CREATE INDEX job_due_idx
    ON core.job (run_at, id)
    WHERE status IN ('queued', 'running');

-- The sweep of lapsed leases (reclaim_expired_leases.sql): only running rows, so it is a
-- handful of entries however long the queue is. Not in jobs.md's index list; added because
-- without it the sweep reads every waiting row each tick (measured: 464 buffers for 20 000
-- waiting rows, against a few with this index).
CREATE INDEX job_lease_idx
    ON core.job (leased_until)
    WHERE status = 'running';

-- The no-overlap test (insert_scheduled_slot.sql) and RunNow's conflict (run_now_job.sql).
CREATE INDEX job_name_open_idx
    ON core.job (name)
    WHERE status IN ('queued', 'running');

-- The purge (purge_jobs.sql).
CREATE INDEX job_finished_idx
    ON core.job (finished_at)
    WHERE status IN ('done', 'failed', 'canceled');

-- The newest failed rows (list_recent_jobs.sql). Not in jobs.md's index list; failed rows are
-- few, so the index is tiny, and without it the list walks every finished row to find the
-- failed ones when failures are rare.
CREATE INDEX job_failed_idx
    ON core.job (finished_at DESC, id DESC)
    WHERE status = 'failed';

-- Status's newest row per name (last_job_per_name.sql). id breaks ties so the order is stable.
CREATE INDEX job_name_created_idx
    ON core.job (name, created_at DESC, id DESC);

COMMENT ON TABLE core.job IS 'sensitivity: internal (the job queue; record references only, no personal data)';
COMMENT ON COLUMN core.job.args IS 'sensitivity: internal (record references: identifiers, names and times; may name a person by ID, which is pseudonymous; purged with the row by jobs.purge after DoneTTL)';
COMMENT ON COLUMN core.job.last_error IS 'sensitivity: internal (a token, never a message)';
