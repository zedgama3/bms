// Package postgres is jobs' storage: the only package with SQL and the driver. The statements
// are in sql/ and the migration in migrations/; each statement's header names its arguments
// and columns, which the methods here follow.
package postgres

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/jobs/rows"
	"github.com/zedgama3/bms/internal/lib/errs"
)

//go:embed migrations/*.sql
var embedded embed.FS

//go:embed sql/*.sql
var sqlFiles embed.FS

// Migrations is the migration set of jobs, rooted at the files (db.md: NNNN_name.sql at the
// set root). It is fs.Sub(embedded, "migrations") resolved in init; that Sub failing on an
// embedded directory is impossible, so init panics (go-conventions).
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic("jobs/postgres: embedded migrations missing: " + err.Error())
	}
	Migrations = sub
}

// The statements, loaded once. A missing file is impossible: the embed directive fails the
// build.
var (
	sqlEnqueue     = mustSQL("enqueue_job.sql")
	sqlDefer       = mustSQL("defer_job.sql")
	sqlByKey       = mustSQL("get_job_by_key.sql")
	sqlInsertSlot  = mustSQL("insert_scheduled_slot.sql")
	sqlRunNow      = mustSQL("run_now_job.sql")
	sqlLease       = mustSQL("lease_jobs.sql")
	sqlReclaim     = mustSQL("reclaim_expired_leases.sql")
	sqlLockName    = mustSQL("lock_job_name.sql")
	sqlHeartbeat   = mustSQL("heartbeat_job.sql")
	sqlComplete    = mustSQL("complete_job.sql")
	sqlFail        = mustSQL("fail_job.sql")
	sqlRequeue     = mustSQL("requeue_job.sql")
	sqlRelease     = mustSQL("release_job.sql")
	sqlRetry       = mustSQL("retry_job.sql")
	sqlCancel      = mustSQL("cancel_job.sql")
	sqlStatusOf    = mustSQL("get_job_status.sql")
	sqlCounts      = mustSQL("count_jobs_by_status.sql")
	sqlQueueStats  = mustSQL("queue_stats.sql")
	sqlRecent      = mustSQL("list_recent_jobs.sql")
	sqlLastPerName = mustSQL("last_job_per_name.sql")
	sqlPurge       = mustSQL("purge_jobs.sql")
)

func mustSQL(name string) string {
	b, err := sqlFiles.ReadFile("sql/" + name)
	if err != nil {
		panic("jobs/postgres: embedded SQL missing: " + name)
	}
	return stripComments(string(b))
}

// stripComments drops the whole-line "--" comments of a statement file, so the documentation
// above each statement is not sent to the server with every call.
func stripComments(sql string) string {
	var out strings.Builder
	for line := range strings.Lines(sql) {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			out.WriteString(line)
		}
	}
	return strings.TrimSpace(out.String())
}

// Store keeps core.job in PostgreSQL. It holds a db.DB and no state; safe for concurrent use.
// Every method runs inside the caller's transaction when ctx carries one. Every error is
// already classified by db.Wrap. No method logs, and no error carries a value from a row.
type Store struct {
	db db.DB
}

// NewStore returns the store over d. Fails errs.Invalid when d is nil.
func NewStore(d db.DB) (*Store, error) {
	if d == nil {
		return nil, errs.E("jobs.postgres.NewStore", errs.Invalid, errors.New("nil database"))
	}
	return &Store{db: d}, nil
}

// scanner is the Scan of a pgx.Rows.
type scanner interface{ Scan(dest ...any) error }

// queryAll runs query and scans every row.
func queryAll[T any](ctx context.Context, d db.DB, op, query string, scan func(scanner) (T, error), args ...any) ([]T, error) {
	rs, err := d.Query(ctx, query, args...)
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []T{}
	for rs.Next() {
		v, err := scan(rs)
		if err != nil {
			return nil, db.Wrap(op, err)
		}
		out = append(out, v)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// returned runs a statement with RETURNING id and reports whether it returned a row.
func (s *Store) returned(ctx context.Context, op, query string, args ...any) (bool, error) {
	ids, err := queryAll(ctx, s.db, op, query, func(sc scanner) ([16]byte, error) {
		var id [16]byte
		err := sc.Scan(&id)
		return id, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}, args...)
	return len(ids) > 0, err
}

// ---- argument helpers --------------------------------------------------------------------

func textArg(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func utc(t time.Time) time.Time { return t.UTC() }

func orZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---- enqueue and schedule ----------------------------------------------------------------

// Enqueue inserts one queued row for n or, when n.Key is set and a Queued row with that
// (name, key) exists, moves that row to n.RunAt with n.Args. It returns the ID of the row that
// now holds the request. A Running, Done, Failed or Canceled row with that key leaves the
// request a no-op and its ID is returned. Runs on the caller's transaction.
// Fails errs.Invalid for a constraint violation (a caller bug), errs.Unavailable wrapping
// db.ErrReadOnly on a read-only copy.
func (s *Store) Enqueue(ctx context.Context, n rows.New, now time.Time) (rows.ID, error) {
	const op = "jobs.postgres.Enqueue"
	// A row purged between the insert and the lookup is the one race: insert once more.
	for range 2 {
		ids, err := queryAll(ctx, s.db, op, sqlEnqueue, scanID,
			[16]byte(n.ID), n.Name, textArg(n.Key), n.Args, utc(n.RunAt), textArg(n.TraceID), utc(now))
		if err != nil {
			return rows.ID{}, err
		}
		if len(ids) > 0 {
			return ids[0], nil
		}
		existing, err := s.byKey(ctx, op, n.Name, n.Key)
		if err == nil {
			return existing, nil
		}
		if !errs.Is(err, errs.NotFound) {
			return rows.ID{}, err
		}
	}
	return rows.ID{}, errs.E(op, errs.Conflict, errors.New("the job changed while it was being enqueued"))
}

func scanID(sc scanner) (rows.ID, error) {
	var id [16]byte
	err := sc.Scan(&id)
	return rows.ID(id), err //nolint:wrapcheck // queryAll wraps with db.Wrap
}

// byKey returns the ID of the row with (name, key).
func (s *Store) byKey(ctx context.Context, op, name, key string) (rows.ID, error) {
	var (
		id     [16]byte
		status string
	)
	if err := s.db.QueryRow(ctx, sqlByKey, name, key).Scan(&id, &status); err != nil {
		return rows.ID{}, db.Wrap(op, err)
	}
	return rows.ID(id), nil
}

// Defer inserts one queued row for n. A repeat of the same (name, key) is a no-op in every
// state, a Queued row included: it reports false.
func (s *Store) Defer(ctx context.Context, n rows.New, now time.Time) (bool, error) {
	return s.returned(ctx, "jobs.postgres.Defer", sqlDefer,
		[16]byte(n.ID), n.Name, textArg(n.Key), n.Args, utc(n.RunAt), textArg(n.TraceID), utc(now))
}

// lockName takes the transaction-scoped advisory lock of a job name (lock_job_name.sql), which
// serializes the "no row of the name is open" test and the insert across concurrent
// transactions. It is released at the end of the caller's transaction, so it only means
// something inside one; a caller that takes several locks takes them in name order.
func (s *Store) lockName(ctx context.Context, op, name string) error {
	if _, err := s.db.Exec(ctx, sqlLockName, name); err != nil {
		return db.Wrap(op, err)
	}
	return nil
}

// InsertSlot inserts the scheduled slot (name, key) due at runAt unless a row of the name is
// Queued or Running or the slot exists. It reports whether it inserted. It first takes the
// name's advisory lock on the caller's transaction (callers walk names in sorted order, so two
// schedulers cannot deadlock), which makes the no-overlap test hold across writers.
func (s *Store) InsertSlot(ctx context.Context, id rows.ID, name, key string, runAt, now time.Time) (bool, error) {
	const op = "jobs.postgres.InsertSlot"
	if err := s.lockName(ctx, op, name); err != nil {
		return false, err
	}
	return s.returned(ctx, op, sqlInsertSlot, [16]byte(id), name, key, utc(runAt), utc(now))
}

// RunNow inserts an admin run of name (key "admin:<id>") due now unless a row of the name is
// Queued or Running. It reports whether it inserted. Like InsertSlot it takes the name's
// advisory lock on the caller's transaction first.
func (s *Store) RunNow(ctx context.Context, id rows.ID, name string, now time.Time, traceID string) (bool, error) {
	const op = "jobs.postgres.RunNow"
	if err := s.lockName(ctx, op, name); err != nil {
		return false, err
	}
	return s.returned(ctx, op, sqlRunNow, [16]byte(id), name, utc(now), textArg(traceID))
}

// ---- leases and outcomes -----------------------------------------------------------------

// Lease takes up to limit due rows and marks them running until leasedUntil, with Attempts
// counting this lease and LeasedUntil as stored: the fence the statements below take
// unchanged. A lapsed Running row whose attempts already reached maxAttempts is left for
// ReclaimExpired. Must run in a short Write transaction.
func (s *Store) Lease(ctx context.Context, now time.Time, limit int, leasedUntil time.Time, instance string, maxAttempts int) ([]rows.Lease, error) {
	return queryAll(ctx, s.db, "jobs.postgres.Lease", sqlLease, func(sc scanner) (rows.Lease, error) {
		var (
			l       rows.Lease
			id      [16]byte
			key     *string
			trace   *string
			started *time.Time
		)
		if err := sc.Scan(&id, &l.Name, &key, &l.Args, &l.Attempts, &l.RunAt, &l.LeasedUntil, &trace, &started); err != nil {
			return rows.Lease{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
		}
		l.ID, l.Key, l.TraceID = rows.ID(id), deref(key), deref(trace)
		l.RunAt, l.LeasedUntil, l.StartedAt = l.RunAt.UTC(), l.LeasedUntil.UTC(), orZero(started)
		return l, nil
	}, utc(now), limit, utc(leasedUntil), instance, maxAttempts)
}

// ReclaimExpired settles up to limit Running rows whose lease has lapsed: back to Queued with
// last_error lost_lease while attempts < maxAttempts, to Failed with gave_up otherwise. Must
// run in a Write transaction. Fails errs.Unavailable wrapping db.ErrReadOnly on a read-only
// copy.
func (s *Store) ReclaimExpired(ctx context.Context, now time.Time, maxAttempts, limit int) ([]rows.Reclaimed, error) {
	return queryAll(ctx, s.db, "jobs.postgres.ReclaimExpired", sqlReclaim, func(sc scanner) (rows.Reclaimed, error) {
		var (
			r  rows.Reclaimed
			id [16]byte
			n  int32
		)
		if err := sc.Scan(&id, &r.Name, &r.Status, &n); err != nil {
			return rows.Reclaimed{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
		}
		r.ID, r.Attempts = rows.ID(id), int(n)
		return r, nil
	}, utc(now), maxAttempts, limit)
}

// Heartbeat renews a running row's lease from lease to next and returns the stored value. It
// reports false when the lease is lost (re-leased, or the row is no longer running).
func (s *Store) Heartbeat(ctx context.Context, id rows.ID, lease, next, now time.Time) (time.Time, bool, error) {
	const op = "jobs.postgres.Heartbeat"
	var stored time.Time
	err := s.db.QueryRow(ctx, sqlHeartbeat, [16]byte(id), utc(lease), utc(next), utc(now)).Scan(&stored)
	switch {
	case err == nil:
		return stored.UTC(), true, nil
	case errs.Is(err, errs.NotFound): // no row: the lease is lost
		return time.Time{}, false, nil
	default:
		return time.Time{}, false, db.Wrap(op, err)
	}
}

// Complete ends a leased row as done. It reports false when the lease was lost.
func (s *Store) Complete(ctx context.Context, id rows.ID, lease, now time.Time) (bool, error) {
	return s.returned(ctx, "jobs.postgres.Complete", sqlComplete, [16]byte(id), utc(lease), utc(now))
}

// Fail ends a leased row as failed with a token. It reports false when the lease was lost.
func (s *Store) Fail(ctx context.Context, id rows.ID, lease time.Time, token string, now time.Time) (bool, error) {
	return s.returned(ctx, "jobs.postgres.Fail", sqlFail, [16]byte(id), utc(lease), token, utc(now))
}

// Requeue puts a leased row back to queued, due at runAt, keeping the attempt counted.
func (s *Store) Requeue(ctx context.Context, id rows.ID, lease time.Time, token string, runAt, now time.Time) (bool, error) {
	return s.returned(ctx, "jobs.postgres.Requeue", sqlRequeue, [16]byte(id), utc(lease), token, utc(runAt), utc(now))
}

// Release puts a leased row back to queued, due as before, without counting the attempt. An
// empty token keeps the row's last_error.
func (s *Store) Release(ctx context.Context, id rows.ID, lease time.Time, token string, now time.Time) (bool, error) {
	return s.returned(ctx, "jobs.postgres.Release", sqlRelease, [16]byte(id), utc(lease), textArg(token), utc(now))
}

// ---- admin -------------------------------------------------------------------------------

// named runs a statement with RETURNING id, name.
func (s *Store) named(ctx context.Context, op, query string, args ...any) (string, bool, error) {
	names, err := queryAll(ctx, s.db, op, query, func(sc scanner) (string, error) {
		var (
			id   [16]byte
			name string
		)
		err := sc.Scan(&id, &name)
		return name, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}, args...)
	if err != nil || len(names) == 0 {
		return "", false, err
	}
	return names[0], true, nil
}

// Retry puts a Failed row back to Queued, due now, with its attempts reset, and returns the
// job's name. With exclusive (a scheduled name) it also requires that no row of the name is
// Queued or Running; the caller must have called LockName for the name on the same
// transaction. It reports false when nothing matched: ask StatusOf why (a row still Failed
// means the exclusive test refused).
func (s *Store) Retry(ctx context.Context, id rows.ID, now time.Time, exclusive bool) (string, bool, error) {
	return s.named(ctx, "jobs.postgres.Retry", sqlRetry, [16]byte(id), utc(now), exclusive)
}

// LockName takes the transaction-scoped advisory lock of name (lock_job_name.sql) that
// serializes the "no row of this name is open" test with the write that follows it. Call it
// inside the Write transaction, immediately before Retry with exclusive.
func (s *Store) LockName(ctx context.Context, name string) error {
	return s.lockName(ctx, "jobs.postgres.LockName", name)
}

// Cancel marks a Queued row Canceled and returns the job's name. It reports false when
// nothing matched: ask StatusOf why.
func (s *Store) Cancel(ctx context.Context, id rows.ID, now time.Time) (string, bool, error) {
	return s.named(ctx, "jobs.postgres.Cancel", sqlCancel, [16]byte(id), utc(now))
}

// StatusOf returns the name and status of a row. Fails errs.NotFound when there is none.
func (s *Store) StatusOf(ctx context.Context, id rows.ID) (name, status string, err error) {
	if err := s.db.QueryRow(ctx, sqlStatusOf, [16]byte(id)).Scan(&name, &status); err != nil {
		return "", "", db.Wrap("jobs.postgres.StatusOf", err)
	}
	return name, status, nil
}

// CountByStatus counts every row by status (reads the whole table once: for the admin
// screen). A status with no rows is absent.
func (s *Store) CountByStatus(ctx context.Context) (map[string]int, error) {
	type pair struct {
		status string
		n      int
	}
	ps, err := queryAll(ctx, s.db, "jobs.postgres.CountByStatus", sqlCounts, func(sc scanner) (pair, error) {
		var (
			p pair
			n int64
		)
		if err := sc.Scan(&p.status, &n); err != nil {
			return pair{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
		}
		p.n = int(n)
		return p, nil
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(ps))
	for _, p := range ps {
		out[p.status] = p.n
	}
	return out, nil
}

// QueueStats returns the depth of the queued and running rows (cheap: for the per-tick
// gauge). A status with no rows is absent.
func (s *Store) QueueStats(ctx context.Context, now time.Time) ([]rows.QueueStat, error) {
	return queryAll(ctx, s.db, "jobs.postgres.QueueStats", sqlQueueStats, func(sc scanner) (rows.QueueStat, error) {
		var (
			q          rows.QueueStat
			depth, due int64
			oldest     *time.Time
		)
		if err := sc.Scan(&q.Status, &depth, &due, &oldest); err != nil {
			return rows.QueueStat{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
		}
		q.Depth, q.Due, q.OldestDue = int(depth), int(due), orZero(oldest)
		return q, nil
	}, utc(now))
}

// scanJob reads the eleven columns of the status listings.
func scanJob(sc scanner, extra ...any) (rows.Job, error) {
	var (
		j                     rows.Job
		id                    [16]byte
		key, lastErr, inst, t *string
		started, finished     *time.Time
	)
	dest := append([]any{&id, &j.Name, &key, &j.Status, &j.Attempts, &j.RunAt, &started, &finished, &lastErr, &inst, &t}, extra...)
	if err := sc.Scan(dest...); err != nil {
		return rows.Job{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}
	j.ID, j.Key, j.LastError, j.Instance, j.TraceID = rows.ID(id), deref(key), deref(lastErr), deref(inst), deref(t)
	j.RunAt, j.StartedAt, j.FinishedAt = j.RunAt.UTC(), orZero(started), orZero(finished)
	return j, nil
}

// Recent returns the newest Failed rows and the Queued rows, newest first, at most limit.
func (s *Store) Recent(ctx context.Context, limit int) ([]rows.Job, error) {
	return queryAll(ctx, s.db, "jobs.postgres.Recent", sqlRecent, func(sc scanner) (rows.Job, error) {
		return scanJob(sc, new(time.Time)) // the sort time, not used
	}, limit)
}

// LastPerName returns the newest row of each of names that has one, by name.
func (s *Store) LastPerName(ctx context.Context, names []string) (map[string]rows.Job, error) {
	if names == nil {
		names = []string{}
	}
	js, err := queryAll(ctx, s.db, "jobs.postgres.LastPerName", sqlLastPerName, func(sc scanner) (rows.Job, error) {
		return scanJob(sc)
	}, names)
	if err != nil {
		return nil, err
	}
	out := make(map[string]rows.Job, len(js))
	for _, j := range js {
		out[j.Name] = j
	}
	return out, nil
}

// Purge deletes up to limit final rows finished before before and returns how many.
func (s *Store) Purge(ctx context.Context, before time.Time, limit int) (int64, error) {
	const op = "jobs.postgres.Purge"
	var n int64
	if err := s.db.QueryRow(ctx, sqlPurge, utc(before), limit).Scan(&n); err != nil {
		return 0, db.Wrap(op, err)
	}
	return n, nil
}
