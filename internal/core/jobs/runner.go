package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/jobs/postgres"
	"github.com/zedgama3/bms/internal/core/jobs/rows"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/retry"
	"github.com/zedgama3/bms/internal/model"
)

// store is the storage the runner declares (D8); postgres.Store implements it. Every error is
// already classified by db.Wrap. The methods that end or move a leased row take the lease (the
// leased_until the lease or the last heartbeat returned) as the fence and report false when the
// row is gone or was re-leased.
type store interface {
	// Enqueue inserts a queued row or, with a key, moves the queued row of that (name, key);
	// it returns the ID of the row now holding the request (an existing running or final row
	// makes the request a no-op).
	Enqueue(ctx context.Context, n rows.New, now time.Time) (rows.ID, error)
	// Defer inserts a queued row; false means the (name, key) exists in any state.
	Defer(ctx context.Context, n rows.New, now time.Time) (bool, error)
	// InsertSlot inserts a scheduled slot unless a row of the name is open or the slot exists.
	// RunNow and InsertSlot serialize on the name (an advisory lock held to the end of the
	// caller's transaction), so callers walk several names in sorted order.
	InsertSlot(ctx context.Context, id rows.ID, name, key string, runAt, now time.Time) (bool, error)
	// RunNow inserts an admin run (key "admin:<id>") unless a row of the name is open.
	RunNow(ctx context.Context, id rows.ID, name string, now time.Time, traceID string) (bool, error)
	// Lease takes up to limit due rows, marks them running until leasedUntil and returns them
	// with Attempts counting this lease. A lapsed row at maxAttempts is left to ReclaimExpired.
	Lease(ctx context.Context, now time.Time, limit int, leasedUntil time.Time, instance string, maxAttempts int) ([]rows.Lease, error)
	// ReclaimExpired settles up to limit Running rows whose lease has lapsed: back to queued
	// (lost_lease), or failed (gave_up) once attempts reach maxAttempts.
	ReclaimExpired(ctx context.Context, now time.Time, maxAttempts, limit int) ([]rows.Reclaimed, error)
	// Heartbeat renews a lease; false means the lease is lost.
	Heartbeat(ctx context.Context, id rows.ID, lease, next, now time.Time) (time.Time, bool, error)
	Complete(ctx context.Context, id rows.ID, lease, now time.Time) (bool, error)
	Fail(ctx context.Context, id rows.ID, lease time.Time, token string, now time.Time) (bool, error)
	Requeue(ctx context.Context, id rows.ID, lease time.Time, token string, runAt, now time.Time) (bool, error)
	// Release gives a row back without counting the attempt; an empty token keeps last_error.
	Release(ctx context.Context, id rows.ID, lease time.Time, token string, now time.Time) (bool, error)
	// Retry and Cancel return the row's name and false when nothing matched; StatusOf says why.
	// Retry with exclusive (a scheduled name) also needs that no row of the name is open, and
	// the caller holds LockName for the name on the same transaction.
	Retry(ctx context.Context, id rows.ID, now time.Time, exclusive bool) (string, bool, error)
	// LockName takes the transaction-scoped advisory lock of a job name.
	LockName(ctx context.Context, name string) error
	Cancel(ctx context.Context, id rows.ID, now time.Time) (string, bool, error)
	// StatusOf fails errs.NotFound when there is no such row.
	StatusOf(ctx context.Context, id rows.ID) (name, status string, err error)
	CountByStatus(ctx context.Context) (map[string]int, error)
	QueueStats(ctx context.Context, now time.Time) ([]rows.QueueStat, error)
	Recent(ctx context.Context, limit int) ([]rows.Job, error)
	LastPerName(ctx context.Context, names []string) (map[string]rows.Job, error)
	Purge(ctx context.Context, before time.Time, limit int) (int64, error)
}

var _ store = (*postgres.Store)(nil)

const (
	// recordTimeout bounds the write of a run's outcome, which must survive a shutdown so a
	// finished job is not run twice.
	recordTimeout = 5 * time.Second
)

// The tokens a row's last_error takes besides the errs kinds.
const (
	tokenPanic     = "panic"
	tokenNoHandler = "no_handler"
	tokenGaveUp    = "gave_up"
	tokenCanceled  = "canceled"
)

// entry is one registered job name.
type entry struct {
	decl Decl
	// raw, when set, reads the row's stored arguments itself and the handler is not used:
	// signals.redeliver stores a Delivery, not a Payload.
	raw func(ctx context.Context, j Job, args []byte) error
}

// Runner is the service. Safe for concurrent use.
type Runner struct {
	store  store
	db     db.DB
	clock  clock.Clock
	logger *slog.Logger
	tracer telemetry.Tracer
	audit  audit.Recorder
	authz  authz.Authorizer
	actorF authz.ActorFunc
	redeli Redeliverer

	instance        string
	poll, lease, to time.Duration
	workers         int
	retry           retry.Policy
	jitter          func() float64
	sem             chan struct{} // one token per busy worker
	stop            chan struct{} // closed by Close
	stopped         sync.Once
	running         sync.WaitGroup // workers started by Run's ticks
	once            sync.WaitGroup // RunOnce calls in flight; Close waits for them
	paused          atomic.Bool    // a run met db.ErrReadOnly: lease nothing until the next tick
	gauge           atomic.Pointer[[]rows.QueueStat]
	metrics         metrics
	mu              sync.Mutex // guards entries, frozen, closed, started, done
	entries         map[Name]*entry
	frozen          bool
	closed          bool
	started         bool
	done            chan struct{} // closed when Run returns; nil until Run starts
	logMu           sync.Mutex
	unhealthy       bool // the loop is failing (log once per change)
	readOnlyLogged  bool // the last tick found a read-only copy
}

// New validates o and builds the runner over postgres.Store, registers jobs.purge and
// signals.redeliver and the metrics. No I/O: it works against db.None() and on a read-only
// copy. Fails errs.Invalid.
func New(o Options) (*Runner, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	st, err := postgres.NewStore(o.DB)
	if err != nil {
		return nil, fmt.Errorf("jobs.New: %w", err)
	}
	return newRunner(o, st)
}

// newRunner builds the runner over st. Tests hand it a fake store.
func newRunner(o Options, st store) (*Runner, error) {
	const op = "jobs.New"
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if st == nil {
		return nil, errs.E(op, errs.Invalid, errors.New("nil store"))
	}
	r := &Runner{
		store: st, db: o.DB, clock: o.Clock, logger: o.Logger, tracer: o.Tracer, audit: o.Audit,
		authz: o.Authz, actorF: o.Actor, redeli: o.Redeliverer, instance: o.Instance,
		poll: o.PollInterval, lease: o.Lease, to: o.RunTimeout, workers: o.Workers, retry: o.Retry,
		jitter:  rand.Float64, //nolint:gosec // backoff jitter, not security
		stop:    make(chan struct{}),
		entries: map[Name]*entry{},
	}
	if r.poll == 0 {
		r.poll = DefaultPollInterval
	}
	if r.lease == 0 {
		r.lease = DefaultLease
	}
	if r.to == 0 {
		r.to = DefaultRunTimeout
	}
	if r.workers == 0 {
		r.workers = DefaultWorkers
	}
	if r.retry == (retry.Policy{}) {
		r.retry = DefaultRetry
	}
	r.sem = make(chan struct{}, r.workers)
	m, err := newMetrics(o.Meter, r)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	r.metrics = m
	for _, e := range []*entry{
		{decl: Decl{Name: NamePurge, Every: time.Hour, Timeout: 10 * time.Minute, Handler: r.purgeHandler}},
		{decl: Decl{Name: NameRedeliver, Handler: nopHandler}, raw: r.redeliverHandler},
	} {
		if err := e.decl.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		r.entries[e.decl.Name] = e
	}
	return r, nil
}

func nopHandler(context.Context, Job) error { return nil }

func (r *Runner) now() time.Time { return r.clock.Now().UTC() }

// span opens the span of one use case, "jobs.<name>".
func (r *Runner) span(ctx context.Context, name string, opts ...telemetry.StartOption) (context.Context, telemetry.Span) {
	return r.tracer.Start(ctx, "jobs."+name, opts...)
}

// endSpan records *err on span (when set) and ends it. Use with defer and a named result.
func endSpan(span telemetry.Span, err *error) {
	if *err != nil {
		span.RecordError(*err)
	}
	span.End()
}

func readOnlyErr(op string) error { return errs.E(op, errs.Unavailable, db.ErrReadOnly) }

// wrap adds op to err and keeps its kind and chain. nil stays nil.
func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}

func newJobID() JobID { return model.NewID[JobID]() }

// opOf returns the operation of the outermost errs.Error in err's chain, "" when none.
func opOf(err error) string {
	var e *errs.Error
	if errors.As(err, &e) && e != nil {
		return e.Op
	}
	return ""
}

// entryOf returns the registration of name.
func (r *Runner) entryOf(name Name) (*entry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[name]
	return e, ok
}

// snapshot returns the registrations sorted by name.
func (r *Runner) snapshot() []*entry {
	r.mu.Lock()
	out := make([]*entry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e)
	}
	r.mu.Unlock()
	slices.SortFunc(out, func(a, b *entry) int {
		switch {
		case a.decl.Name < b.decl.Name:
			return -1
		case a.decl.Name > b.decl.Name:
			return 1
		}
		return 0
	})
	return out
}

// Declared returns what is registered in this process, sorted by name, without I/O and
// without a permission check: Last is nil and NextDue is zero. bms check prints it. Cannot
// fail.
func (r *Runner) Declared() []Registered {
	entries := r.snapshot()
	out := make([]Registered, len(entries))
	for i, e := range entries {
		out[i] = r.registeredOf(e)
	}
	return out
}

func (r *Runner) registeredOf(e *entry) Registered {
	timeout := e.decl.Timeout
	if timeout == 0 {
		timeout = r.to
	}
	return Registered{Name: e.decl.Name, Owner: e.decl.Name.Owner(), Every: e.decl.Every, Timeout: timeout}
}

// ---- metrics -----------------------------------------------------------------------------

// The outcomes bms.jobs.runs takes.
const (
	outcomeOK        = "ok"
	outcomeRetried   = "retried"
	outcomeFailed    = "failed"
	outcomeCanceled  = "canceled"
	outcomeReadOnly  = "read_only"
	outcomePanicked  = "panicked"
	outcomeNoHandler = "no_handler"
	outcomeLostLease = "lost_lease"
	outcomeDropped   = "dropped"
)

// metrics holds the instruments.
type metrics struct {
	runs telemetry.Counter
	lag  telemetry.Histogram
}

func newMetrics(m telemetry.Meter, r *Runner) (metrics, error) {
	const op = "jobs.New"
	var (
		out metrics
		err error
	)
	if out.runs, err = m.Counter("bms.jobs.runs", telemetry.WithUnit("{run}"),
		telemetry.WithDescription("Job runs, by job, owner and outcome.")); err != nil {
		return metrics{}, fmt.Errorf("%s: %w", op, err)
	}
	if out.lag, err = m.Histogram("bms.jobs.lag", telemetry.WithUnit("s"),
		telemetry.WithDescription("Seconds from a job's due time to the start of its run."),
		telemetry.WithBuckets(0.1, 0.5, 1, 5, 10, 30, 60, 300, 900, 3600)); err != nil {
		return metrics{}, fmt.Errorf("%s: %w", op, err)
	}
	err = m.Gauge("bms.jobs.queue", func(context.Context) []telemetry.Sample {
		stats := r.gauge.Load()
		if stats == nil {
			return nil
		}
		depth := map[string]int{rows.Queued: 0, rows.Running: 0}
		for _, s := range *stats {
			depth[s.Status] = s.Depth
		}
		out := make([]telemetry.Sample, 0, len(depth))
		for _, st := range []string{rows.Queued, rows.Running} {
			out = append(out, telemetry.Sample{Value: float64(depth[st]), Attrs: []telemetry.Attr{telemetry.Token("status", st)}})
		}
		return out
	}, telemetry.WithUnit("{job}"), telemetry.WithDescription("Open jobs by status."))
	if err != nil {
		return metrics{}, fmt.Errorf("%s: %w", op, err)
	}
	return out, nil
}

func (m metrics) run(ctx context.Context, name Name, outcome string) {
	m.runs.Add(ctx, 1, telemetry.Token("job", string(name)), telemetry.Module(name.Owner()), telemetry.Token("outcome", outcome))
}
