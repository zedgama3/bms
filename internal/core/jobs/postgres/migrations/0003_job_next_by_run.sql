-- jobs 0003 (D52 a): tells a request held on a running row that the row's own run made
-- (a handler that re-enqueues its own (name, key)) from one another caller made. Adds
-- next_by_run to core.job. When the run then fails, a held request made by the run itself
-- keeps the attempts count (a self-rescheduling failing job exhausts its attempts and ends
-- failed instead of retrying forever); a request from outside the run still resets attempts
-- to 0 (D50). The statements that take a row out of running clear it with next_run_at.
--
-- Safe on a live database with data: one boolean column with a constant default (a catalog
-- change in PostgreSQL 11 and later, no table rewrite). The CHECK is validated against the
-- existing rows under a brief ACCESS EXCLUSIVE lock; every existing row has the default
-- false, so it passes, and the table holds only waiting rows and a week of final rows
-- (jobs.purge), so the scan is tiny.
--
-- Personal data: none (a flag). Retention: lives with next_run_at for the length of one run.

ALTER TABLE core.job
    ADD COLUMN next_by_run boolean NOT NULL DEFAULT false,
    -- Only meaningful while a request is held.
    ADD CONSTRAINT job_next_by_run_chk CHECK (NOT next_by_run OR next_run_at IS NOT NULL);

COMMENT ON COLUMN core.job.next_by_run IS 'sensitivity: internal (true when the request held while the row runs was made by the run itself)';
