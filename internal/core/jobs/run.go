package jobs

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/jobs/internal/rules"
	"github.com/zedgama3/bms/internal/core/jobs/rows"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/retry"
)

// ---- Run, Close, RunOnce -----------------------------------------------------------------

// Run is the loop: at once and then every PollInterval, when the database is writable, one
// tick (the scheduler inserts due slots; the workers lease and run due rows until none is due
// or Workers are busy). It freezes Register. It returns nil when ctx ends or after Close
// (running handlers get Canceled and their rows are released); errs.Internal when called
// twice. A failing tick is logged once per change of state and retried next tick; a
// read-only copy is logged once when it flips and skipped.
func (r *Runner) Run(ctx context.Context) error {
	const op = "jobs.Runner.Run"
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return errs.E(op, errs.Internal, errors.New("Run called twice"))
	}
	r.started, r.frozen = true, true
	done := make(chan struct{})
	r.done = done
	closed := r.closed
	r.mu.Unlock()
	defer close(done)
	if closed {
		return nil
	}

	// The workers' ctx ends first (the deferred cancel runs before this wait), so a handler
	// gets Canceled and its row is given back before Run returns.
	defer r.running.Wait()
	runCtx, cancel := r.bind(ctx)
	defer cancel()
	tk, err := r.clock.NewTicker(r.poll)
	if err != nil {
		return errs.E(op, errs.Internal, err)
	}
	defer tk.Stop()
	// A restart works at once instead of one interval later.
	r.tick(runCtx)
	for {
		select {
		case <-runCtx.Done():
			return nil
		case <-tk.C():
			r.tick(runCtx)
		}
	}
}

// Close stops Run, cancels the runs in flight, those of Run's workers and those started by
// RunOnce (their rows are released within recordTimeout), and waits for them under ctx.
// Idempotent. Fails with ctx's kind (errs.Timeout, errs.Canceled) when runs are still
// returning when ctx ends.
func (r *Runner) Close(ctx context.Context) error {
	const op = "jobs.Runner.Close"
	r.stopped.Do(func() {
		r.mu.Lock()
		r.closed = true
		r.mu.Unlock()
		close(r.stop)
	})
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	// No RunOnce can join once closed is set (it adds itself under mu), so this wait is safe.
	idle := make(chan struct{})
	go func() { // exits when every RunOnce has returned
		r.once.Wait()
		close(idle)
	}()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return errs.E(op, errs.KindOf(ctx.Err()), ctx.Err())
		}
	}
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return errs.E(op, errs.KindOf(ctx.Err()), ctx.Err())
	}
}

// bind returns a child of ctx that is also canceled by Close.
func (r *Runner) bind(ctx context.Context) (context.Context, context.CancelFunc) {
	c, cancel := context.WithCancel(ctx)
	go func() { // exits when c is done: the caller cancels it
		select {
		case <-r.stop:
			cancel()
		case <-c.Done():
		}
	}()
	return c, cancel
}

// RunOnce does one tick and waits for the runs it started, returning counts. Fails
// errs.Unavailable wrapping db.ErrReadOnly when the copy is read-only (nothing leased),
// store kinds, and errs.Canceled when ctx ends mid-tick (rows released).
func (r *Runner) RunOnce(ctx context.Context) (Ran, error) {
	const op = "jobs.Runner.RunOnce"
	// Join the runs Close waits for, atomically with the closed check.
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return Ran{}, errs.E(op, errs.Unavailable, errors.New("the job runner is closed"))
	}
	r.once.Add(1)
	r.mu.Unlock()
	defer r.once.Done()
	if r.db.ReadOnly() {
		return Ran{}, readOnlyErr(op)
	}
	r.paused.Store(false)
	wctx, cancel := r.bind(ctx)
	defer cancel()
	sctx, span := r.span(wctx, "tick")
	var ts tally
	var wg sync.WaitGroup
	err := r.pass(wctx, sctx, &ts, &wg, true)
	span.SetAttrs(ts.attrs()...)
	if err == nil && wctx.Err() != nil {
		err = errs.E(op, errs.KindOf(wctx.Err()), wctx.Err())
	}
	endSpan(span, &err)
	return ts.snapshot(), wrap(op, err)
}

// tick is one pass of the loop, with its logging and its read-only check.
func (r *Runner) tick(ctx context.Context) {
	sctx, span := r.span(ctx, "tick")
	defer span.End()
	if r.db.ReadOnly() {
		r.logMu.Lock()
		flipped := !r.readOnlyLogged
		r.readOnlyLogged = true
		r.logMu.Unlock()
		if flipped {
			r.metrics.tick(ctx, outcomeReadOnly)
			r.logger.InfoContext(ctx, "jobs: this copy is read-only; no job runs here")
		}
		span.SetAttrs(telemetry.Token("outcome", outcomeReadOnly))
		return
	}
	r.logMu.Lock()
	r.readOnlyLogged = false
	r.logMu.Unlock()
	r.paused.Store(false)

	var ts tally
	err := r.pass(ctx, sctx, &ts, &r.running, false)
	span.SetAttrs(ts.attrs()...)
	if err != nil {
		span.RecordError(err)
	}
	r.noteHealth(ctx, err)
}

// noteHealth logs the first failure of the loop after a good spell, and the recovery; a
// cancellation is neither.
func (r *Runner) noteHealth(ctx context.Context, err error) {
	if err != nil && errs.Is(err, errs.Canceled) {
		return
	}
	r.logMu.Lock()
	was := r.unhealthy
	r.unhealthy = err != nil
	r.logMu.Unlock()
	switch {
	case err != nil && !was:
		r.logger.WarnContext(ctx, "jobs: the runner failed", slog.String("error.kind", errs.KindOf(err).String()),
			slog.String("error.op", opOf(err)))
	case err == nil && was:
		r.logger.InfoContext(ctx, "jobs: the runner works again")
	}
}

// ---- the pass: schedule, lease, hand to workers ------------------------------------------

// tally counts what one pass did, safe for the workers' goroutines.
type tally struct {
	mu  sync.Mutex
	ran Ran
}

func (t *tally) scheduled(n int) {
	t.mu.Lock()
	t.ran.Scheduled += n
	t.mu.Unlock()
}

// outcome counts one run by its outcome token.
func (t *tally) outcome(o string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch o {
	case outcomeOK, outcomeDropped:
		t.ran.Ran++
	case outcomeRetried:
		t.ran.Retried++
	case outcomeFailed, outcomePanicked, outcomeNoHandler:
		t.ran.Failed++
	case outcomeCanceled, outcomeReadOnly, outcomeLostLease:
		t.ran.Released++
	}
}

func (t *tally) snapshot() Ran {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ran
}

func (t *tally) attrs() []telemetry.Attr {
	s := t.snapshot()
	return []telemetry.Attr{
		telemetry.Int("scheduled", s.Scheduled), telemetry.Int("ran", s.Ran), telemetry.Int("failed", s.Failed),
		telemetry.Int("retried", s.Retried), telemetry.Int("released", s.Released),
	}
}

// pass is one tick's work. ctx is the work context the runs derive from (it carries no tick
// span, so a run's span is a root or a child of the enqueuer's trace); sctx is the tick's own,
// for its statements. With wait it returns when the runs it started are done.
func (r *Runner) pass(ctx, sctx context.Context, ts *tally, wg *sync.WaitGroup, wait bool) error {
	now := r.now()
	if err := r.reclaim(sctx, now, ts); err != nil {
		return err
	}
	n, err := r.schedule(sctx, now)
	ts.scheduled(n)
	if err != nil {
		return err
	}
	derr := r.dispatch(ctx, wg, ts)
	if wait {
		wg.Wait()
	}
	stats, serr := r.store.QueueStats(sctx, r.now())
	if serr == nil {
		r.gauge.Store(&stats)
	}
	switch {
	case derr != nil:
		return derr
	case serr != nil:
		return wrap("jobs.tick: counting", serr)
	}
	return nil
}

// reclaim settles the rows whose lease lapsed (a process that died or hung with a job running)
// before anything is scheduled or leased, in one Write transaction: back to queued while
// attempts remain, failed with gave_up once they are used up, which stops a job that kills its
// process every time from being leased forever. A gave_up row is counted and logged like a run
// that failed; a requeued one is counted as released (outcome lost_lease) and logged.
func (r *Runner) reclaim(ctx context.Context, now time.Time, ts *tally) error {
	const op = "jobs.Runner.reclaim"
	var got []rows.Reclaimed
	err := r.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		var err error
		got, err = r.store.ReclaimExpired(ctx, now, r.retry.MaxAttempts, MaxBatch)
		return err //nolint:wrapcheck // wrapped below
	})
	if err != nil {
		return wrap(op, err)
	}
	for _, g := range got {
		attrs := []slog.Attr{
			slog.String("job", g.Name), slog.String("job_id", g.ID.String()), slog.Int("attempt", g.Attempts),
		}
		if g.Next {
			attrs = append(attrs, slog.Bool("next", true))
		}
		if g.Status == rows.Failed {
			ts.outcome(outcomeFailed)
			r.metrics.run(ctx, Name(g.Name), outcomeFailed)
			attrs = append(attrs, slog.String("outcome", outcomeFailed), slog.String("reason", tokenGaveUp))
			r.logger.LogAttrs(ctx, slog.LevelWarn, "jobs: a job lost its lease at its last attempt and is given up", attrs...)
			continue
		}
		// A requeue is a released run: counted in Ran.Released and bms.jobs.runs (Q-JOBS11).
		ts.outcome(outcomeLostLease)
		r.metrics.run(ctx, Name(g.Name), outcomeLostLease)
		attrs = append(attrs, slog.String("outcome", outcomeLostLease))
		r.logger.LogAttrs(ctx, slog.LevelWarn, "jobs: a job lost its lease and is queued again", attrs...)
	}
	return nil
}

// schedule inserts the current slot of every scheduled name that has no row waiting or
// running, in one Write transaction, and returns how many it inserted. It never catches up:
// the slot is the one now falls in.
func (r *Runner) schedule(ctx context.Context, now time.Time) (int, error) {
	const op = "jobs.Runner.schedule"
	var scheduled []*entry
	for _, e := range r.snapshot() {
		if e.decl.Every > 0 {
			scheduled = append(scheduled, e)
		}
	}
	if len(scheduled) == 0 {
		return 0, nil
	}
	inserted := 0
	err := r.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		inserted = 0
		for _, e := range scheduled {
			slot := now.UTC().Truncate(e.decl.Every)
			ok, err := r.store.InsertSlot(ctx, newJobID(), string(e.decl.Name), slot.Format(time.RFC3339), slot, now)
			if err != nil {
				return err //nolint:wrapcheck // wrapped below
			}
			if ok {
				inserted++
			}
		}
		return nil
	})
	if err != nil {
		return 0, wrap(op, err)
	}
	return inserted, nil
}

// leaseOne takes the next due row in one short Write transaction.
func (r *Runner) leaseOne(ctx context.Context) (rows.Lease, bool, error) {
	const op = "jobs.Runner.lease"
	now := r.now()
	var got []rows.Lease
	err := r.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		var err error
		got, err = r.store.Lease(ctx, now, 1, now.Add(r.lease), r.instance, r.retry.MaxAttempts)
		return err //nolint:wrapcheck // wrapped below
	})
	if err != nil {
		return rows.Lease{}, false, wrap(op, err)
	}
	if len(got) == 0 {
		return rows.Lease{}, false, nil
	}
	return got[0], true, nil
}

// dispatch starts a worker for each free slot that finds a due row. A worker keeps leasing
// until nothing is due.
func (r *Runner) dispatch(ctx context.Context, wg *sync.WaitGroup, ts *tally) error {
	for {
		if r.paused.Load() {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil // shutting down: the caller reports it
		default:
		}
		select {
		case r.sem <- struct{}{}:
		default:
			return nil // every worker is busy
		}
		lj, ok, err := r.leaseOne(ctx)
		if err != nil || !ok {
			<-r.sem
			return err
		}
		wg.Add(1)
		go r.work(ctx, wg, lj, ts)
	}
}

// work runs lj and then any further due rows, and exits when none is due, the runner is
// paused or ctx is done.
func (r *Runner) work(ctx context.Context, wg *sync.WaitGroup, lj rows.Lease, ts *tally) {
	defer wg.Done()
	defer func() { <-r.sem }()
	for {
		r.runOne(ctx, lj, ts)
		if r.paused.Load() || ctx.Err() != nil {
			return
		}
		next, ok, err := r.leaseOne(ctx)
		if err != nil {
			r.noteHealth(ctx, err)
			return
		}
		if !ok {
			return
		}
		lj = next
	}
}

// ---- one run -----------------------------------------------------------------------------

// leaseFence holds the leased_until the next write must present: the lease's, then each
// heartbeat's.
type leaseFence struct {
	mu    sync.Mutex
	until time.Time
	lost  bool
}

func (f *leaseFence) get() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.until
}

func (f *leaseFence) set(t time.Time) {
	f.mu.Lock()
	f.until = t
	f.mu.Unlock()
}

func (f *leaseFence) markLost() {
	f.mu.Lock()
	f.lost = true
	f.mu.Unlock()
}

func (f *leaseFence) isLost() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lost
}

// traceparent builds a W3C header that continues the enqueuer's trace.
func traceparent(traceID string) http.Header {
	h := http.Header{}
	h.Set("traceparent", "00-"+traceID+"-0000000000000001-01")
	return h
}

// runOne runs one leased row and records what came of it.
func (r *Runner) runOne(ctx context.Context, lj rows.Lease, ts *tally) {
	name := Name(lj.Name)
	opts := []telemetry.StartOption{
		telemetry.WithKind(telemetry.SpanConsumer),
		telemetry.WithAttrs(telemetry.Token("job", lj.Name), telemetry.Module(name.Owner()),
			telemetry.ID("job_id", lj.ID), telemetry.Int("attempt", lj.Attempts)),
	}
	if lj.TraceID != "" {
		opts = append(opts, telemetry.WithParentFrom(traceparent(lj.TraceID)))
	}
	sctx, span := r.span(ctx, "run", opts...)
	defer span.End()

	res := r.execute(ctx, sctx, lj)
	span.SetAttrs(telemetry.Token("outcome", res.outcome))
	if res.err != nil {
		span.RecordError(res.err)
	}
	r.metrics.run(sctx, name, res.outcome)
	ts.outcome(res.outcome)
}

// result is what a run came to: the outcome token and the handler's error, if any.
type result struct {
	outcome string
	err     error
}

// execute runs the handler under its deadline and heartbeat and writes the outcome. ctx is
// the runner's (cancellation, shutdown); sctx carries the run's span.
func (r *Runner) execute(ctx, sctx context.Context, lj rows.Lease) result {
	name := Name(lj.Name)
	fence := &leaseFence{until: lj.LeasedUntil}
	e, ok := r.entryOf(name)
	switch {
	case !ok:
		return r.settle(sctx, lj, fence, verdict{kind: vFail, token: tokenNoHandler, outcome: outcomeNoHandler}, nil)
	case ctx.Err() != nil: // leased, then shut down before it started
		return r.settle(sctx, lj, fence, verdict{kind: vRelease, token: tokenCanceled, outcome: outcomeCanceled}, nil)
	}
	r.metrics.lag.Record(sctx, max(r.now().Sub(lj.RunAt), 0).Seconds())

	timeout := e.decl.Timeout
	if timeout == 0 {
		timeout = r.to
	}
	runCtx, cancelRun := context.WithCancel(rules.WithJob(sctx, lj.ID))
	defer cancelRun()
	hctx, cancelTimeout := context.WithTimeout(runCtx, timeout)
	defer cancelTimeout()

	stopBeat := r.heartbeat(sctx, lj.ID, fence, cancelRun)
	res := r.invoke(hctx, e, lj)
	stopBeat()

	if fence.isLost() {
		r.logger.WarnContext(sctx, "jobs: the lease on a job was taken by another process; its result is not recorded",
			slog.String("job", lj.Name), slog.String("job_id", lj.ID.String()), slog.Int("attempt", lj.Attempts))
		return result{outcome: outcomeLostLease, err: res.err}
	}
	if res.panicked {
		r.logger.ErrorContext(sctx, "jobs: a handler panicked",
			slog.String("job", lj.Name), slog.String("job_id", lj.ID.String()), slog.Int("attempt", lj.Attempts),
			slog.String("error.kind", errs.Internal.String()), slog.String("stack", string(res.stack)))
	}
	return r.settle(sctx, lj, fence, r.decide(hctx, lj, res.err, res.panicked), res.err)
}

// invoked is what a handler call produced.
type invoked struct {
	err      error  // the handler's own error
	panicked bool   // the handler panicked; err is nil
	stack    []byte // the stack at the panic
}

// invoke calls the handler under recover. A panic is reported, not propagated.
func (r *Runner) invoke(ctx context.Context, e *entry, lj rows.Lease) (res invoked) {
	defer func() {
		if p := recover(); p != nil {
			res = invoked{panicked: true, stack: debug.Stack()}
		}
	}()
	j := Job{ID: lj.ID, Name: Name(lj.Name), Key: lj.Key, Attempt: lj.Attempts, RunAt: lj.RunAt}
	if e.raw != nil {
		return invoked{err: e.raw(ctx, j, lj.Args)}
	}
	args, err := decodeArgs(lj.Args)
	if err != nil {
		return invoked{err: err}
	}
	j.Args = args
	return invoked{err: e.decl.Handler(ctx, j)}
}

type verdictKind uint8

const (
	vDone verdictKind = iota + 1
	vRelease
	vRequeue
	vFail
)

// verdict is what to write to the row.
type verdict struct {
	kind    verdictKind
	token   string // last_error: "" keeps it (release) or clears it (done)
	outcome string
	runAt   time.Time // requeue: when to run again
}

// attempter is implemented by errors that know how many tries were made in all (see
// attemptError).
type attempter interface{ attempts() int }

// decide maps a handler's outcome to what happens to its row ("Outcome by kind").
func (r *Runner) decide(hctx context.Context, lj rows.Lease, err error, panicked bool) verdict {
	switch {
	case panicked:
		return verdict{kind: vFail, token: tokenPanic, outcome: outcomePanicked}
	case err == nil:
		return verdict{kind: vDone, outcome: outcomeOK}
	case errors.Is(err, errDropped):
		return verdict{kind: vDone, outcome: outcomeDropped}
	case errors.Is(err, errGaveUp):
		return verdict{kind: vFail, token: tokenGaveUp, outcome: outcomeFailed}
	case errors.Is(err, db.ErrReadOnly):
		r.paused.Store(true)
		return verdict{kind: vRelease, outcome: outcomeReadOnly}
	case (errs.Is(err, errs.Canceled) || errors.Is(err, context.Canceled)) && errors.Is(hctx.Err(), context.Canceled):
		return verdict{kind: vRelease, token: tokenCanceled, outcome: outcomeCanceled}
	case retry.Retryable(err):
		tries := lj.Attempts
		var a attempter
		if errors.As(err, &a) {
			tries = a.attempts()
		}
		if tries >= r.retry.MaxAttempts {
			return verdict{kind: vFail, token: tokenGaveUp, outcome: outcomeFailed}
		}
		return verdict{
			kind: vRequeue, token: errs.KindOf(err).String(), outcome: outcomeRetried,
			runAt: r.now().Add(r.retry.Delay(tries+1, r.jitter())),
		}
	default:
		return verdict{kind: vFail, token: errs.KindOf(err).String(), outcome: outcomeFailed}
	}
}

// settle writes v to the row, fenced by the lease, on a context that survives a shutdown, and
// logs the outcome once. A fenced-out write (the row is someone else's) becomes lost_lease.
func (r *Runner) settle(sctx context.Context, lj rows.Lease, fence *leaseFence, v verdict, herr error) result {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(sctx), recordTimeout)
	defer cancel()
	now := r.now()
	lease := fence.get()
	var (
		ok, next bool
		gaveUp   bool
		err      error
	)
	switch v.kind {
	case vDone:
		ok, next, err = r.store.Complete(wctx, lj.ID, lease, now)
	case vRelease:
		ok, next, err = r.store.Release(wctx, lj.ID, lease, v.token, now)
	case vRequeue:
		ok, next, err = r.store.Requeue(wctx, lj.ID, lease, v.token, v.runAt, now)
	default:
		ok, next, gaveUp, err = r.store.Fail(wctx, lj.ID, lease, v.token, now, r.retry.MaxAttempts)
	}
	if gaveUp { // a held by-run request was dropped at the limit: the row says gave_up (F55a)
		v.token = tokenGaveUp
	}
	outcome := v.outcome
	if outcome == outcomeDropped {
		herr = nil // the handler logged why; a dropped delivery is not an error
	}
	switch {
	case err != nil:
		if errors.Is(err, db.ErrReadOnly) {
			r.paused.Store(true)
		}
		r.logger.WarnContext(sctx, "jobs: could not record the outcome of a job; its lease will lapse",
			slog.String("job", lj.Name), slog.String("job_id", lj.ID.String()),
			slog.String("error.kind", errs.KindOf(err).String()))
		herr = errors.Join(herr, err)
	case !ok:
		outcome = outcomeLostLease
	}
	r.logOutcome(sctx, lj, outcome, v, herr, next && ok)
	return result{outcome: outcome, err: herr}
}

// logOutcome writes the one line per outcome: identifiers, the outcome and the error's kind
// and operation, never the arguments or the error's text.
func (r *Runner) logOutcome(ctx context.Context, lj rows.Lease, outcome string, v verdict, herr error, next bool) {
	level := slog.LevelInfo
	switch outcome {
	case outcomeOK, outcomeDropped:
		level = slog.LevelDebug
	case outcomeFailed, outcomePanicked, outcomeNoHandler, outcomeLostLease:
		level = slog.LevelWarn
	}
	attrs := []slog.Attr{
		slog.String("job", lj.Name), slog.String("job_id", lj.ID.String()), slog.Int("attempt", lj.Attempts),
		slog.String("outcome", outcome),
	}
	if herr != nil && outcome != outcomeDropped {
		attrs = append(attrs, slog.String("error.kind", errs.KindOf(herr).String()), slog.String("error.op", opOf(herr)))
	}
	if v.token != "" && (outcome == outcomeFailed || (v.token == tokenGaveUp && outcome != outcomeLostLease)) {
		attrs = append(attrs, slog.String("reason", v.token))
	}
	if next { // a request made during the run was queued in place of the run's own end state
		attrs = append(attrs, slog.Bool("next", true))
	}
	msg := "jobs: a job ran"
	if outcome == outcomeNoHandler {
		msg = "jobs: a job has no handler in this process; it fails until the module is back"
	}
	r.logger.LogAttrs(ctx, level, msg, attrs...)
}

// heartbeat renews the lease every Lease/3 until the returned stop is called, which waits for
// the goroutine. A renewal that finds the lease lost, or that fails once the clock has passed
// the lease's expiry (Q-JOBS9), cancels the run and marks the fence.
func (r *Runner) heartbeat(ctx context.Context, id rows.ID, fence *leaseFence, cancelRun context.CancelFunc) (stop func()) {
	tk, err := r.clock.NewTicker(r.lease / 3)
	if err != nil { // r.lease is validated; a bad ticker is a bug, and the run goes on unrenewed
		r.logger.ErrorContext(ctx, "jobs: no heartbeat for a job", slog.String("job_id", id.String()),
			slog.String("error.kind", errs.KindOf(err).String()))
		return func() {}
	}
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer tk.Stop()
		for {
			select {
			case <-quit:
				return
			case <-tk.C():
				if !r.beat(ctx, id, fence, cancelRun) {
					return
				}
			}
		}
	}()
	return func() {
		close(quit)
		<-done
	}
}

// beat renews once; false means the lease is lost (taken, or about to expire without a renewal:
// a failed renewal with no beat left before the fence) and the beating stops.
func (r *Runner) beat(ctx context.Context, id rows.ID, fence *leaseFence, cancelRun context.CancelFunc) bool {
	bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	now := r.now()
	next, ok, err := r.store.Heartbeat(bctx, id, fence.get(), now.Add(r.lease), now)
	switch {
	case err != nil:
		// Retried at the next beat while that beat will still land before the fence. Past the
		// fence the row is reclaimable by another writer, so this process stops the run (outcome
		// lost_lease, nothing more written) instead of overlapping it until the handler's
		// timeout. The test looks one beat ahead: a ticker's jitter puts the beat that should
		// cancel a few microseconds before the fence about half the time, and testing only
		// "now >= fence" would then let the run go on for a whole beat past the expiry (F52).
		lapsed := !now.Add(r.lease / 3).Before(fence.get())
		msg := "jobs: could not renew the lease of a job"
		if lapsed {
			msg = "jobs: could not renew the lease of a job before it expired; the run is canceled"
		}
		r.logger.WarnContext(ctx, msg, slog.String("job_id", id.String()),
			slog.String("error.kind", errs.KindOf(err).String()))
		if lapsed {
			fence.markLost()
			cancelRun()
			return false
		}
		return true
	case !ok:
		fence.markLost()
		cancelRun()
		return false
	}
	fence.set(next)
	return true
}

// tick counts the tick-level outcome (a read-only copy skipped).
func (m metrics) tick(ctx context.Context, outcome string) {
	m.runs.Add(ctx, 1, telemetry.Token("job", "tick"), telemetry.Token("module", "jobs"), telemetry.Token("outcome", outcome))
}
