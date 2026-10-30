//go:build integration

package jobs_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"        //nolint:depguard // faultDB must implement db.DB, whose Query returns pgx.Rows
	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // faultDB must implement db.DB, whose Exec returns pgconn.CommandTag

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// faultDB wraps a db.DB and fails on demand: readOnly behaves like a replica (writes refused
// with db.ErrReadOnly, ReadOnly() true); failAll fails every operation with err.
type faultDB struct {
	db.DB
	mu       sync.Mutex
	readOnly bool
	failAll  error
	failSQL  func(sql string) error // fails Query and QueryRow statements it matches (nil = none)
	calls    atomic.Int64
}

func (f *faultDB) set(ro bool, fail error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readOnly, f.failAll = ro, fail
}

func (f *faultDB) state() (readOnly bool, fail error) {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readOnly, f.failAll
}

func (f *faultDB) Transact(ctx context.Context, mode db.Mode, fn func(context.Context) error) error {
	ro, fail := f.state()
	if fail != nil {
		return fail
	}
	if mode == db.Write && ro {
		return errs.E("test.faultDB", errs.Unavailable, db.ErrReadOnly)
	}
	return f.DB.Transact(ctx, mode, fn) //nolint:wrapcheck // test double
}

func (f *faultDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	ro, fail := f.state()
	if fail != nil {
		return pgconn.CommandTag{}, fail
	}
	if ro && writes(sql) {
		return pgconn.CommandTag{}, errs.E("test.faultDB", errs.Unavailable, db.ErrReadOnly)
	}
	return f.DB.Exec(ctx, sql, args...) //nolint:wrapcheck // test double
}

// writes reports whether a statement changes data. "FOR UPDATE" locks, it does not write.
func writes(sql string) bool {
	u := strings.ReplaceAll(strings.ToUpper(sql), "FOR UPDATE", "")
	return strings.Contains(u, "INSERT INTO") || strings.Contains(u, "UPDATE ") || strings.Contains(u, "DELETE FROM")
}

func (f *faultDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	ro, fail := f.state()
	if fail != nil {
		return nil, fail
	}
	f.mu.Lock()
	match := f.failSQL
	f.mu.Unlock()
	if match != nil {
		if err := match(sql); err != nil {
			return nil, err
		}
	}
	if ro && writes(sql) {
		return nil, errs.E("test.faultDB", errs.Unavailable, db.ErrReadOnly)
	}
	return f.DB.Query(ctx, sql, args...) //nolint:wrapcheck // test double
}

type errRow struct{ err error }

func (e errRow) Scan(...any) error { return e.err }

func (f *faultDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	ro, fail := f.state()
	f.mu.Lock()
	match := f.failSQL
	f.mu.Unlock()
	if match != nil {
		if err := match(sql); err != nil {
			return errRow{err}
		}
	}
	if fail != nil {
		return errRow{fail}
	}
	if ro && writes(sql) {
		return errRow{errs.E("test.faultDB", errs.Unavailable, db.ErrReadOnly)}
	}
	return f.DB.QueryRow(ctx, sql, args...)
}

func (f *faultDB) ReadOnly() bool {
	f.mu.Lock()
	ro := f.readOnly
	f.mu.Unlock()
	return ro || f.DB.ReadOnly()
}

// irig is a rig over a real, migrated, throw-away database.
type irig struct {
	*rig
	pool *db.Pool
	flt  *faultDB
}

func migrate(t testing.TB, pool *db.Pool) {
	t.Helper()
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	wantNoErr(t, err)
	_, err = mig.Migrate(ctxT(t), []db.Set{jobs.Set})
	wantNoErr(t, err)
}

// newIRig builds a runner with one worker (deterministic) on a fresh database.
func newIRig(t *testing.T, mods ...func(*jobs.Options)) *irig {
	t.Helper()
	pool := dbtest.TempDB(t)
	migrate(t, pool)
	return newIRigOn(t, pool, nil, mods...)
}

// newIRigOn builds a runner over pool. A non-nil other shares its clock and takes the next
// instance name, so two runners stand for two processes on one database.
func newIRigOn(t *testing.T, pool *db.Pool, other *irig, mods ...func(*jobs.Options)) *irig {
	t.Helper()
	flt := &faultDB{DB: pool}
	r, o := newRigOptions(t, flt)
	o.Workers = 1
	if other != nil {
		r.clk = other.clk
		o.Clock = other.clk
		o.Instance = "test-b"
	}
	for _, m := range mods {
		m(&o)
	}
	run, err := jobs.New(o)
	if err != nil {
		t.Fatalf("jobs.New: %v", err)
	}
	r.run, r.opts = run, o
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := run.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return &irig{rig: r, pool: pool, flt: flt}
}

// ---- reading rows ------------------------------------------------------------------------

type row struct {
	ID                                [16]byte
	Name, Status, Args                string
	Key, LastError, Instance, TraceID *string
	Attempts                          int
	RunAt, CreatedAt, UpdatedAt       time.Time
	Leased, Started, Finished         *time.Time
}

func (r row) jobID() jobs.JobID { return jobs.JobID(r.ID) }

const rowCols = `id, name, key, args::text, status, attempts, run_at, leased_until, started_at, finished_at,
	last_error, instance, trace_id, created_at, updated_at`

func (r *irig) rows(where string, args ...any) []row {
	r.t.Helper()
	q := "SELECT " + rowCols + " FROM core.job"
	if where != "" {
		q += " WHERE " + where
	}
	q += " ORDER BY created_at, run_at, id"
	rs, err := r.pool.Query(ctxT(r.t), q, args...)
	if err != nil {
		r.t.Fatalf("query rows: %v", err)
	}
	defer rs.Close()
	var out []row
	for rs.Next() {
		var x row
		if err := rs.Scan(&x.ID, &x.Name, &x.Key, &x.Args, &x.Status, &x.Attempts, &x.RunAt, &x.Leased, &x.Started,
			&x.Finished, &x.LastError, &x.Instance, &x.TraceID, &x.CreatedAt, &x.UpdatedAt); err != nil {
			r.t.Fatalf("scan: %v", err)
		}
		out = append(out, x)
	}
	if err := rs.Err(); err != nil {
		r.t.Fatalf("rows: %v", err)
	}
	return out
}

func (r *irig) byName(name jobs.Name) []row { return r.rows("name = $1", string(name)) }

func (r *irig) one(name jobs.Name) row {
	r.t.Helper()
	rs := r.byName(name)
	if len(rs) != 1 {
		r.t.Fatalf("%q has %d rows, want 1", name, len(rs))
	}
	return rs[0]
}

func (r *irig) byID(id jobs.JobID) row {
	r.t.Helper()
	rs := r.rows("id = $1", [16]byte(id))
	if len(rs) != 1 {
		r.t.Fatalf("no row %v", id)
	}
	return rs[0]
}

func (r *irig) count(where string, args ...any) int {
	r.t.Helper()
	q := "SELECT count(*) FROM core.job"
	if where != "" {
		q += " WHERE " + where
	}
	var n int
	if err := r.pool.QueryRow(ctxT(r.t), q, args...).Scan(&n); err != nil {
		r.t.Fatalf("count: %v", err)
	}
	return n
}

func (r *irig) exec(sql string, args ...any) {
	r.t.Helper()
	if _, err := r.pool.Exec(ctxT(r.t), sql, args...); err != nil {
		r.t.Fatalf("exec %q: %v", sql, err)
	}
}

// ---- forcing rows ------------------------------------------------------------------------

// forceQueued inserts a Queued row straight into the table (key "" = none).
func (r *irig) forceQueued(name jobs.Name, key string, runAt time.Time, args string) jobs.JobID {
	r.t.Helper()
	id := model.NewID[jobs.JobID]()
	if args == "" {
		args = "{}"
	}
	var k any
	if key != "" {
		k = key
	}
	r.exec(`INSERT INTO core.job (id, name, key, args, status, attempts, run_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4::jsonb, 'queued', 0, $5, $5, $5)`, [16]byte(id), string(name), k, args, runAt)
	return id
}

// forceRunning inserts a Running row leased until leasedUntil (attempts 1).
func (r *irig) forceRunning(name jobs.Name, key string, leasedUntil time.Time) jobs.JobID {
	r.t.Helper()
	id := model.NewID[jobs.JobID]()
	var k any
	if key != "" {
		k = key
	}
	start := leasedUntil.Add(-5 * time.Minute)
	r.exec(`INSERT INTO core.job (id, name, key, args, status, attempts, run_at, leased_until, started_at, instance, created_at, updated_at)
		VALUES ($1, $2, $3, '{}'::jsonb, 'running', 1, $4, $5, $4, 'dead-copy', $4, $4)`,
		[16]byte(id), string(name), k, start, leasedUntil)
	return id
}

// forceFinal inserts a final row (done, failed or canceled) finished at finishedAt.
func (r *irig) forceFinal(name jobs.Name, key, status string, finishedAt time.Time) jobs.JobID {
	r.t.Helper()
	id := model.NewID[jobs.JobID]()
	var k any
	if key != "" {
		k = key
	}
	var lastErr any
	attempts := 1
	switch status {
	case "failed":
		lastErr, attempts = "internal", 3
	case "canceled":
		attempts = 0
	}
	r.exec(`INSERT INTO core.job (id, name, key, args, status, attempts, run_at, started_at, finished_at, last_error, created_at, updated_at)
		VALUES ($1, $2, $3, '{}'::jsonb, $4, $5, $6, $6, $6, $7, $6, $6)`,
		[16]byte(id), string(name), k, status, attempts, finishedAt, lastErr)
	return id
}

// ---- driving the loop --------------------------------------------------------------------

// pump advances the fake clock by step and yields, again and again, until cond holds. It
// never sleeps: a tick the runner has not armed for yet is simply caught by the next step.
func (r *irig) pump(step time.Duration, what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for i := 0; ; i++ {
		// Give the runner's goroutines (and their database round trips) real time to react
		// to this step before the next one; spinning, not sleeping.
		until := time.Now().Add(25 * time.Millisecond)
		for time.Now().Before(until) {
			if cond() {
				return
			}
			runtime.Gosched()
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s after %d steps", what, i)
		}
		r.advance(step)
	}
}

// runOnce is RunOnce with the usual no-error check.
func (r *irig) runOnce() jobs.Ran {
	r.t.Helper()
	ran, err := r.run.RunOnce(ctxT(r.t))
	wantNoErr(r.t, err)
	return ran
}

// asyncOnce starts RunOnce on a goroutine.
type onceResult struct {
	ran jobs.Ran
	err error
}

func (r *irig) asyncOnce(ctx context.Context) <-chan onceResult {
	ch := make(chan onceResult, 1)
	go func() {
		ran, err := r.run.RunOnce(ctx)
		ch <- onceResult{ran, err}
	}()
	return ch
}

func (r *irig) await(ch <-chan onceResult) onceResult {
	r.t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(15 * time.Second):
		r.t.Fatal("RunOnce did not return")
		return onceResult{}
	}
}

// metric sums a counter's measurements whose attribute key equals val ("" matches any).
func (r *rig) metric(instrument, key, val string) float64 {
	var sum float64
	for _, m := range r.cap.Measurements() {
		if m.Instrument == instrument && (key == "" || m.Attrs[key] == val) {
			sum += m.Value
		}
	}
	return sum
}

func (r *rig) outcomes(outcome string) float64 {
	return r.metric("bms.jobs.runs", "outcome", outcome)
}

// argsJSON decodes a row's args.
func argsJSON(t testing.TB, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("args %q: %v", s, err)
	}
	return m
}

// blocker is a handler gate: handlers announce themselves and wait to be released.
type blocker struct {
	started chan jobs.Job
	release chan struct{}
	once    sync.Once
}

func newBlocker() *blocker {
	return &blocker{started: make(chan jobs.Job, 16), release: make(chan struct{})}
}

func (b *blocker) open() { b.once.Do(func() { close(b.release) }) }

// waitCtx announces the job and blocks until released or ctx ends (then returns ctx's error
// as errs.Canceled / errs.Timeout).
func (b *blocker) waitCtx(ctx context.Context, j jobs.Job) error {
	b.started <- j
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return errs.E("test.blocker", errs.KindOf(ctx.Err()), ctx.Err())
	}
}

// waitIgnoringCtx announces the job and blocks until released, whatever ctx does.
func (b *blocker) waitIgnoringCtx(_ context.Context, j jobs.Job) error {
	b.started <- j
	<-b.release
	return nil
}

func (b *blocker) waitStarted(t testing.TB) jobs.Job {
	t.Helper()
	select {
	case j := <-b.started:
		return j
	case <-time.After(15 * time.Second):
		t.Fatal("handler did not start")
		return jobs.Job{}
	}
}

// all returns a copy of every job the handler has seen, in arrival order.
func (c *counter) all() []jobs.Job {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]jobs.Job(nil), c.jobs...)
}

func (f *fakeRedeliverer) set(fn func(signals.Delivery) error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fn = fn
}

func (f *fakeRedeliverer) got() []signals.Delivery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]signals.Delivery(nil), f.calls...)
}
