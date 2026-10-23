// Package jobstest gives tests of jobs' callers a Registrar without a database or a runner.
// Fake validates like the real one; it runs a registered handler on demand, with the same
// context marker the runner sets.
package jobstest

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/jobs/internal/rules"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Fake is a jobs.Registrar for tests: it validates like the real one (names, scope, Decl,
// Request), records registrations, records enqueues after commit (inside a dbtest.Fake
// transaction the request appears only once the transaction commits) and runs handlers on
// demand. It keeps one row per (name, key) (and one per keyless request) in the real
// states, and moves it as the real queue does (decision 3, decision 23; J1): Enqueue moves a
// queued row, reopens a final one, and holds the request on a running one; Run marks the row
// running for the handler's duration and then applies the held request (queued at its RunAt)
// or, with none, ends the row done (nil) or failed (any error; the Fake does not model
// backoff). Safe for concurrent use.
type Fake struct {
	owner string
	now   func() time.Time // the clock Enqueue bounds run_at against; the wall clock by default

	mu       sync.Mutex
	decls    []jobs.Decl
	enqueued []jobs.Request
	ids      map[string]jobs.JobID // (name, key) -> the ID handed out
	rows     []*row                // every row, in the order first created
	failErr  error
}

// row is one row of the fake queue. next* mirror core.job.next_run_at and next_args: set only
// while the row is running.
type row struct {
	id       jobs.JobID
	name     jobs.Name
	key      string
	status   string
	runAt    time.Time
	args     jobs.Args
	hasNext  bool
	nextRun  time.Time
	nextArgs jobs.Args
}

var _ jobs.Registrar = (*Fake)(nil)

// Option configures a Fake.
type Option func(*Fake)

// WithClock makes the Fake bound Request.RunAt (at most jobs.MaxDelay ahead) against c, the way
// the real Runner bounds it against its own clock; the default is the wall clock, which is
// all Request.Validate has. A nil c is ignored.
func WithClock(c clock.Clock) Option {
	return func(f *Fake) {
		if c != nil {
			f.now = c.Now
		}
	}
}

// NewFake returns a Registrar scoped to owner. Cannot fail.
func NewFake(owner string, opts ...Option) *Fake {
	f := &Fake{owner: owner, now: time.Now, ids: map[string]jobs.JobID{}}
	for _, o := range opts {
		if o != nil {
			o(f)
		}
	}
	return f
}

// Register validates d as the real Registrar does and records it. Fails errs.Invalid for an
// invalid Decl or (field "name", code "scope") another owner's name, and errs.Conflict when
// the name is registered already.
func (f *Fake) Register(d jobs.Decl) error {
	const op = "jobstest.Fake.Register"
	if err := d.Validate(); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if err := rules.Scope(op, f.owner, string(d.Name)); err != nil {
		return err //nolint:wrapcheck // rules.Scope names itself
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, have := range f.decls {
		if have.Name == d.Name {
			return errs.E(op, errs.Conflict, fmt.Errorf("job %q is already registered", d.Name))
		}
	}
	f.decls = append(f.decls, d)
	return nil
}

// Enqueue validates r as the real Registrar does (Request.ValidateAt with the Fake's clock, the
// owner's scope, a handler registered for the name: field "name", code "unknown") and records it once the
// transaction ctx carries commits (at once when it carries none; a rolled-back transaction
// records nothing). It returns a fresh ID, the same one for a repeated (name, key). With a Key
// the latest request wins, as in the real queue: a queued row is moved, a final one reopened
// and a running one holds the request until its Run ends. It checks no permission. After Fail
// it returns that error once.
func (f *Fake) Enqueue(ctx context.Context, r jobs.Request) (jobs.JobID, error) {
	const op = "jobstest.Fake.Enqueue"
	if err := r.ValidateAt(f.now()); err != nil {
		return jobs.JobID{}, fmt.Errorf("%s: %w", op, err)
	}
	if err := rules.Scope(op, f.owner, string(r.Name)); err != nil {
		return jobs.JobID{}, err //nolint:wrapcheck // rules.Scope names itself
	}
	f.mu.Lock()
	known := false
	for _, d := range f.decls {
		known = known || d.Name == r.Name
	}
	failErr := f.failErr
	f.failErr = nil
	var id jobs.JobID
	if known && failErr == nil {
		idKey := ""
		if r.Key != "" {
			idKey = string(r.Name) + "\x00" + r.Key
			id = f.ids[idKey]
		}
		if id.IsZero() {
			id = model.NewID[jobs.JobID]()
			if idKey != "" {
				f.ids[idKey] = id
			}
		}
	}
	f.mu.Unlock()
	switch {
	case failErr != nil:
		return jobs.JobID{}, failErr
	case !known:
		v := validate.Errors{Op: op}
		v.Addf("name", "unknown", "no handler is registered for this job")
		return jobs.JobID{}, v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	rec := copyRequest(r)
	at := rec.RunAt
	if now := f.now(); at.IsZero() || at.Before(now) {
		at = now // the real queue treats zero and the past as "now"
	}
	if err := db.AfterCommit(ctx, func(context.Context) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.enqueued = append(f.enqueued, rec)
		f.apply(id, rec, at.UTC())
	}); err != nil {
		return jobs.JobID{}, errs.E(op, errs.KindOf(err), errors.New("could not register the record hook"))
	}
	return id, nil
}

// Registered returns a copy of the declarations so far, in registration order.
func (f *Fake) Registered() []jobs.Decl {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]jobs.Decl(nil), f.decls...)
}

// Enqueued returns a copy of the requests recorded so far, in order.
func (f *Fake) Enqueued() []jobs.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]jobs.Request, len(f.enqueued))
	for i, r := range f.enqueued {
		out[i] = copyRequest(r)
	}
	return out
}

// apply makes the request the key's row, as the upsert of enqueue_job.sql does. The caller
// holds f.mu.
func (f *Fake) apply(id jobs.JobID, r jobs.Request, at time.Time) {
	if r.Key != "" {
		if w := f.byKey(r.Name, r.Key); w != nil {
			switch w.status {
			case statusQueued:
				w.runAt, w.args = at, r.Args
			case statusRunning:
				w.hasNext, w.nextRun, w.nextArgs = true, at, r.Args
			default: // done, failed or canceled: reopened
				w.status, w.runAt, w.args = statusQueued, at, r.Args
			}
			return
		}
	}
	f.rows = append(f.rows, &row{id: id, name: r.Name, key: r.Key, status: statusQueued, runAt: at, args: r.Args})
}

// byKey returns the row of (name, key), nil when there is none. The caller holds f.mu.
func (f *Fake) byKey(name jobs.Name, key string) *row {
	for _, w := range f.rows {
		if w.name == name && w.key == key {
			return w
		}
	}
	return nil
}

// Queued returns the rows waiting now, as the queue would hold them: one Request per row with
// the effective RunAt and Args, sorted by RunAt then Key. A running row is absent; a row ended
// by Run is absent until a new Enqueue reopens it. Cannot fail.
func (f *Fake) Queued() []jobs.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queuedLocked(nil)
}

// queuedLocked returns the waiting rows (due at or before limit when limit is non-nil), sorted.
func (f *Fake) queuedLocked(limit *time.Time) []jobs.Request {
	var ws []*row
	for _, w := range f.rows {
		if w.status == statusQueued && (limit == nil || !w.runAt.After(*limit)) {
			ws = append(ws, w)
		}
	}
	sortRows(ws)
	out := make([]jobs.Request, len(ws))
	for i, w := range ws {
		out[i] = jobs.Request{Name: w.name, Key: w.key, Args: copyArgs(w.args), RunAt: w.runAt}
	}
	return out
}

func sortRows(ws []*row) {
	slices.SortStableFunc(ws, func(a, b *row) int {
		if c := a.runAt.Compare(b.runAt); c != 0 {
			return c
		}
		if a.key != b.key {
			if a.key < b.key {
				return -1
			}
			return 1
		}
		return 0
	})
}

// Run calls the handler registered for name with j (jobs.InJob true, no transaction) and
// returns its error unchanged. It fills j.Name when empty. Fails errs.NotFound when no
// handler is registered for name. When j.Key is set the key's row is running during the call
// and leaves running by the key rule: a request held on it becomes the waiting row (Queued
// shows it), otherwise the row ends done (nil) or failed (any error or a panic). A Key with no
// row (a test drove the handler directly) gets one. A keyless j finds its row by j.ID when it
// has one.
func (f *Fake) Run(ctx context.Context, name jobs.Name, j jobs.Job) error {
	const op = "jobstest.Fake.Run"
	f.mu.Lock()
	var h jobs.Handler
	for _, d := range f.decls {
		if d.Name == name {
			h = d.Handler
		}
	}
	if h == nil {
		f.mu.Unlock()
		return errs.E(op, errs.NotFound, fmt.Errorf("no handler is registered for %q", name))
	}
	if j.Name == "" {
		j.Name = name
	}
	w := f.rowFor(name, j)
	if w != nil {
		w.status = statusRunning
	}
	f.mu.Unlock()
	finished := false
	defer func() {
		if !finished { // a panic: the real runner fails the row
			f.leave(w, errPanicked)
		}
	}()
	err := h(rules.WithJob(ctx), j)
	finished = true
	f.leave(w, err)
	return err
}

var errPanicked = errors.New("handler panicked")

// rowFor returns the row a Run drives, creating one for a Key that has none. The caller holds
// f.mu. A keyless job with no known ID has no row.
func (f *Fake) rowFor(name jobs.Name, j jobs.Job) *row {
	if j.Key != "" {
		if w := f.byKey(name, j.Key); w != nil {
			return w
		}
		w := &row{id: j.ID, name: name, key: j.Key, status: statusQueued, runAt: j.RunAt, args: j.Args}
		if w.id.IsZero() {
			w.id = model.NewID[jobs.JobID]()
		}
		f.rows = append(f.rows, w)
		return w
	}
	if !j.ID.IsZero() {
		for _, w := range f.rows {
			if w.id == j.ID {
				return w
			}
		}
	}
	return nil
}

// leave takes w out of running by the key rule: a held request becomes the waiting row,
// otherwise it ends done or failed.
func (f *Fake) leave(w *row, err error) {
	if w == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case w.hasNext:
		w.status, w.runAt, w.args = statusQueued, w.nextRun, w.nextArgs
		w.hasNext, w.nextRun, w.nextArgs = false, time.Time{}, nil
	case err != nil:
		w.status = statusFailed
	default:
		w.status = statusDone
	}
}

// RunDue runs, in (RunAt, Key) order, every waiting row with RunAt <= now, once each (rows
// requeued by their own run at or before now are left for the next call, so a handler that
// always re-enqueues itself cannot loop a test), each as Run does with a Job built from the row
// (Attempt 1, the row's ID, Key, Args and RunAt). It returns how many ran and the handlers'
// errors joined (a failing row ends failed and the others still run). It is how a test drives
// a schedule across a fake clock. Fails only with the handlers' errors.
func (f *Fake) RunDue(ctx context.Context, now time.Time) (int, error) {
	f.mu.Lock()
	var due []*row
	for _, w := range f.rows {
		if w.status == statusQueued && !w.runAt.After(now) {
			due = append(due, w)
		}
	}
	sortRows(due)
	batch := make([]jobs.Job, len(due))
	for i, w := range due {
		batch[i] = jobs.Job{ID: w.id, Name: w.name, Key: w.key, Args: copyArgs(w.args), Attempt: 1, RunAt: w.runAt}
	}
	f.mu.Unlock()
	ran := 0
	var errAll error
	for _, j := range batch {
		f.mu.Lock()
		stale := false
		for _, w := range f.rows { // a row an earlier handler cancelled or moved is not run
			if w.id == j.ID && (w.status != statusQueued || w.runAt.After(now)) {
				stale = true
			}
		}
		f.mu.Unlock()
		if stale {
			continue
		}
		ran++
		errAll = errors.Join(errAll, f.Run(ctx, j.Name, j))
	}
	return ran, errAll
}

// Fail makes the next Enqueue return err (nil clears it).
func (f *Fake) Fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failErr = err
}

func copyRequest(r jobs.Request) jobs.Request {
	r.Args = copyArgs(r.Args)
	return r
}

func copyArgs(a jobs.Args) jobs.Args {
	if a == nil {
		return nil
	}
	return maps.Clone(a)
}

// The words core.job.status holds, as the real rows use them.
const (
	statusQueued  = "queued"
	statusRunning = "running"
	statusDone    = "done"
	statusFailed  = "failed"
)
