// Package jobstest gives tests of jobs' callers a Registrar without a database or a runner.
// Fake validates like the real one; it runs a registered handler on demand, with the same
// context marker the runner sets.
package jobstest

import (
	"context"
	"errors"
	"fmt"
	"maps"
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
// demand. Safe for concurrent use.
type Fake struct {
	owner string
	now   func() time.Time // the clock Enqueue bounds run_at against; the wall clock by default

	mu       sync.Mutex
	decls    []jobs.Decl
	enqueued []jobs.Request
	ids      map[string]jobs.JobID // (name, key) -> the ID handed out
	failErr  error
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
// records nothing). It returns a fresh ID, the same one for a repeated (name, key). It checks
// no permission. After Fail it returns that error once.
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
	if err := db.AfterCommit(ctx, func(context.Context) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.enqueued = append(f.enqueued, rec)
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

// Run calls the handler registered for name with j (jobs.InJob true, no transaction) and
// returns its error unchanged. It fills j.Name when empty. Fails errs.NotFound when no
// handler is registered for name.
func (f *Fake) Run(ctx context.Context, name jobs.Name, j jobs.Job) error {
	const op = "jobstest.Fake.Run"
	f.mu.Lock()
	var h jobs.Handler
	for _, d := range f.decls {
		if d.Name == name {
			h = d.Handler
		}
	}
	f.mu.Unlock()
	if h == nil {
		return errs.E(op, errs.NotFound, fmt.Errorf("no handler is registered for %q", name))
	}
	if j.Name == "" {
		j.Name = name
	}
	return h(rules.WithJob(ctx), j)
}

// Fail makes the next Enqueue return err (nil clears it).
func (f *Fake) Fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failErr = err
}

func copyRequest(r jobs.Request) jobs.Request {
	if r.Args != nil {
		r.Args = maps.Clone(r.Args)
	}
	return r
}
