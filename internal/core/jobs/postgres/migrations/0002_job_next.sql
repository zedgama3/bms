-- jobs 0002 (Amendment J1): a request for a key made while its row is running. Adds
-- next_run_at and next_args to core.job: the RunAt and Args of an Enqueue that met a running
-- row with the same (name, key). Every statement that takes the row out of running (complete,
-- requeue, release, fail, the reclaim's two arms) applies the held request first: the row
-- becomes queued at next_run_at with next_args, attempts 0, and the two columns are cleared
-- (jobs.md decision 23, sql/enqueue_job.sql).
--
-- Safe on a live database with data: two nullable columns with no default (a catalog change,
-- no table rewrite). The two CHECKs are validated against the existing rows under a brief
-- ACCESS EXCLUSIVE lock; every existing row has both columns NULL so they pass, and the table
-- holds only waiting rows and a week of final rows (jobs.purge), so the scan is tiny.
--
-- Personal data: none, as args (record references only). Retention: the held request lives
-- on a running row for the length of one run, then becomes the row's args or is cleared; it
-- is purged with the row by jobs.purge like args.

ALTER TABLE core.job
    ADD COLUMN next_run_at timestamptz,
    ADD COLUMN next_args   jsonb,
    -- Both or neither; set only while running (the statements that leave running clear them).
    ADD CONSTRAINT job_next_chk CHECK (
        (next_run_at IS NULL) = (next_args IS NULL)
        AND (next_run_at IS NULL OR status = 'running')),
    -- The same content rule and bound as args (job_args_chk).
    ADD CONSTRAINT job_next_args_chk CHECK (
        next_args IS NULL
        OR (jsonb_typeof(next_args) = 'object' AND octet_length(next_args::text) <= 65536));

COMMENT ON COLUMN core.job.next_run_at IS 'sensitivity: internal (a time; the RunAt of a request held while the row runs)';
COMMENT ON COLUMN core.job.next_args IS 'sensitivity: internal (record references: identifiers, names and times; may name a person by ID, which is pseudonymous; purged with the row by jobs.purge after DoneTTL)';
