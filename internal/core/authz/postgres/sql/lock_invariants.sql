-- Store.LockInvariants: the one advisory lock that serialises every write touching the
-- invariants (grant, revoke, role change, role delete, create rule, delete person, seed; authz.md decision
-- 16), so the duplicate check, the separation-of-duties check and the last-access-admin check
-- are not racing. Transaction-scoped: released at commit or rollback. Take it first inside
-- Write, after the service's read-only check. No args. Use Exec. db.Wrap classifies any
-- failure (a read-only copy included).
SELECT pg_advisory_xact_lock(hashtext('access.invariants'))
