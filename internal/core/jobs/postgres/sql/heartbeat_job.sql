-- Store.Heartbeat: renew a running row's lease, every Lease/3. Use QueryRow (RETURNING),
-- standalone (its own statement).
-- Args: $1 id uuid, $2 leased_until timestamptz (the fence: the value held now), $3 new
-- leased_until timestamptz (now + Lease; must differ from $2, which a forward-moving clock
-- guarantees), $4 now timestamptz (updated_at).
-- Columns, in order: leased_until timestamptz (the stored new value; it is the fence from
-- now on). pgx.ErrNoRows means the lease is lost (re-leased by another process, or the row is
-- no longer running): cancel the run and write nothing more to the row. A store error is
-- retried at the next beat.
-- Fails 25006 (db.ErrReadOnly) on a read-only copy. Constraint errors: none reachable.
UPDATE core.job
SET leased_until = $3::timestamptz,
    updated_at = $4::timestamptz
WHERE id = $1::uuid
  AND status = 'running'
  AND leased_until = $2::timestamptz
RETURNING leased_until
