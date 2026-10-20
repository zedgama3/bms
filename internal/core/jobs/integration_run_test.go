//go:build integration

package jobs_test

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/retry"
)

func enqueue(t *testing.T, s jobs.Registrar, req jobs.Request) jobs.JobID {
	t.Helper()
	id, err := s.Enqueue(ctxT(t), req)
	wantNoErr(t, err)
	return id
}

// ---- what a handler is given -------------------------------------------------------------

func TestRun_Handler_GetsJobAndSystemContext(t *testing.T) {
	r := newIRig(t)
	var (
		mu                  sync.Mutex
		got                 jobs.Job
		inJob, hasTx, hasDL bool
		deadline            time.Time
		hasActor            bool
	)
	s := r.register(jobs.Decl{Name: "volunteers.remind", Timeout: 30 * time.Second, Handler: func(ctx context.Context, j jobs.Job) error {
		mu.Lock()
		defer mu.Unlock()
		got = j
		inJob = jobs.InJob(ctx)
		_, hasTx = db.TxMode(ctx)
		deadline, hasDL = ctx.Deadline()
		_, hasActor = actorFn(ctx)
		return nil
	}})
	ref := signals.RecordID(newPerson())
	at := epoch.Add(-10 * time.Second).Truncate(time.Second)
	id := enqueue(t, s, jobs.Request{Name: "volunteers.remind", Key: "k-1", RunAt: at, Args: jobs.Args{"shift": ref}})
	_ = id
	if jobs.InJob(ctxT(t)) {
		t.Fatal("InJob true outside a handler")
	}
	start := time.Now()
	ran := r.runOnce()
	if ran.Ran < 1 {
		t.Fatalf("Ran = %+v", ran)
	}
	mu.Lock()
	defer mu.Unlock()
	if got.ID != id || got.Name != "volunteers.remind" || got.Key != "k-1" || got.Attempt != 1 || got.Args["shift"] != ref {
		t.Errorf("Job = %+v", got)
	}
	if !inJob {
		t.Error("InJob(ctx) false inside the handler")
	}
	if hasTx {
		t.Error("the handler's ctx carries a transaction")
	}
	if hasActor {
		t.Error("the handler's ctx carries an actor")
	}
	if !hasDL || deadline.Before(start.Add(20*time.Second)) || deadline.After(time.Now().Add(31*time.Second)) {
		t.Errorf("deadline = %v (has=%v), want about Decl.Timeout (30s) from now", deadline, hasDL)
	}
	x := r.byID(id)
	if x.Status != "done" || x.Attempts != 1 || x.Leased != nil || x.Finished == nil || !x.Finished.Equal(epoch) ||
		x.Started == nil || x.Instance == nil || *x.Instance != "test-a" || x.LastError != nil {
		t.Errorf("row after success = %+v", x)
	}
}

func TestRun_HandlerDefaultDeadline_IsRunTimeout(t *testing.T) {
	r := newIRig(t, func(o *jobs.Options) { o.RunTimeout = 20 * time.Second })
	var dl time.Time
	var has bool
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: func(ctx context.Context, _ jobs.Job) error {
		dl, has = ctx.Deadline()
		return nil
	}})
	enqueue(t, s, jobs.Request{Name: "volunteers.remind"})
	r.runOnce()
	if !has || dl.After(time.Now().Add(21*time.Second)) || dl.Before(time.Now().Add(5*time.Second)) {
		t.Errorf("deadline = %v (has=%v), want about 20s ahead", dl, has)
	}
}

func TestRun_DueRowsRunInRunAtThenIDOrder(t *testing.T) {
	r := newIRig(t)
	var c counter
	r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(nil)})
	r.forceQueued("volunteers.remind", "c", epoch.Add(-1*time.Minute), "")
	r.forceQueued("volunteers.remind", "a", epoch.Add(-3*time.Minute), "")
	r.forceQueued("volunteers.remind", "b", epoch.Add(-2*time.Minute), "")
	r.forceQueued("volunteers.remind", "future", epoch.Add(time.Minute), "")
	r.runOnce()
	var order []string
	for _, j := range c.all() {
		order = append(order, j.Key)
	}
	if strings.Join(order, ",") != "a,b,c" {
		t.Fatalf("ran %v, want [a b c] and not the future job", order)
	}
}

// ---- retries by errs kind ----------------------------------------------------------------

func TestRun_HandlerUnavailable_RequeuesWithBackoff(t *testing.T) {
	for _, kind := range []errs.Kind{errs.Unavailable, errs.Timeout} {
		t.Run(kind.String(), func(t *testing.T) {
			r := newIRig(t)
			var c counter
			s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(errs.E("test", kind, errors.New("down")))})
			id := enqueue(t, s, jobs.Request{Name: "volunteers.remind"})
			ran := r.runOnce()
			if ran.Retried != 1 || ran.Failed != 0 {
				t.Fatalf("Ran = %+v, want one retry", ran)
			}
			x := r.byID(id)
			if x.Status != "queued" || x.Attempts != 1 || x.Leased != nil || x.Finished != nil {
				t.Fatalf("row = %+v", x)
			}
			if x.LastError == nil || *x.LastError != kind.String() {
				t.Errorf("last_error = %v, want %q", x.LastError, kind.String())
			}
			if x.RunAt.Before(epoch) || x.RunAt.After(epoch.Add(time.Minute)) {
				t.Errorf("run_at = %v, want within the first backoff (<= 1m) of %v", x.RunAt, epoch)
			}
			r.advance(2 * time.Minute)
			r.runOnce()
			if c.n() != 2 || c.all()[1].Attempt != 2 {
				t.Fatalf("handler calls %d, second attempt %+v; want a second run with Attempt 2", c.n(), c.all())
			}
			if got := r.byID(id).RunAt; got.Before(epoch.Add(2*time.Minute)) || got.After(epoch.Add(2*time.Minute+2*time.Minute)) {
				t.Errorf("second backoff run_at = %v, want within 2m of now", got)
			}
			if r.outcomes("retried") != 2 {
				t.Errorf("retried outcomes = %v, want 2", r.outcomes("retried"))
			}
		})
	}
}

func TestRun_GivesUp_AfterMaxAttempts(t *testing.T) {
	t.Run("custom policy of three", func(t *testing.T) {
		r := newIRig(t, func(o *jobs.Options) {
			o.Retry = retry.Policy{Initial: time.Second, Max: time.Second, Multiplier: 1, MaxAttempts: 3}
		})
		var c counter
		s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(errs.E("test", errs.Unavailable, errors.New("down")))})
		id := enqueue(t, s, jobs.Request{Name: "volunteers.remind"})
		for i := 1; i <= 3; i++ {
			ran := r.runOnce()
			if i < 3 && ran.Retried != 1 {
				t.Fatalf("run %d: Ran = %+v, want a retry", i, ran)
			}
			if i == 3 && ran.Failed != 1 {
				t.Fatalf("run 3: Ran = %+v, want a final failure", ran)
			}
			r.advance(2 * time.Second)
		}
		x := r.byID(id)
		if x.Status != "failed" || x.Attempts != 3 || x.LastError == nil || *x.LastError != "gave_up" || x.Finished == nil {
			t.Fatalf("row = %+v, want failed/gave_up after 3 attempts", x)
		}
		r.runOnce()
		if c.n() != 3 {
			t.Errorf("handler ran %d times, want 3 and no more", c.n())
		}
	})
	t.Run("default policy of ten", func(t *testing.T) {
		r := newIRig(t)
		var c counter
		s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(errs.E("test", errs.Unavailable, errors.New("down")))})
		id := enqueue(t, s, jobs.Request{Name: "volunteers.remind"})
		for range 12 {
			r.runOnce()
			r.advance(2 * time.Hour)
		}
		x := r.byID(id)
		if c.n() != jobs.DefaultRetry.MaxAttempts || x.Status != "failed" || x.LastError == nil || *x.LastError != "gave_up" {
			t.Fatalf("handler ran %d times, row = %+v; want %d attempts then failed/gave_up", c.n(), x, jobs.DefaultRetry.MaxAttempts)
		}
	})
}

func TestRun_HandlerInvalid_FailsWithoutRetry(t *testing.T) {
	plain := errors.New("not classified")
	kinds := map[string]error{
		"invalid":         errs.E("test", errs.Invalid, plain),
		"not_found":       errs.E("test", errs.NotFound, plain),
		"conflict":        errs.E("test", errs.Conflict, plain),
		"permission":      errs.E("test", errs.Permission, plain),
		"unauthenticated": errs.E("test", errs.Unauthenticated, plain),
		"unsupported":     errs.E("test", errs.Unsupported, plain),
		"internal":        errs.E("test", errs.Internal, plain),
		"canceled":        errs.E("test", errs.Canceled, plain), // Canceled with a live ctx is not a shutdown
		"unclassified":    plain,
	}
	for token, herr := range kinds {
		t.Run(token, func(t *testing.T) {
			r := newIRig(t)
			var c counter
			s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(herr)})
			id := enqueue(t, s, jobs.Request{Name: "volunteers.remind"})
			ran := r.runOnce()
			if ran.Failed != 1 || ran.Retried != 0 {
				t.Fatalf("Ran = %+v, want one final failure", ran)
			}
			want := token
			if token == "unclassified" {
				want = "internal"
			}
			x := r.byID(id)
			if x.Status != "failed" || x.Attempts != 1 || x.Finished == nil || x.Leased != nil ||
				x.LastError == nil || *x.LastError != want {
				t.Fatalf("row = %+v, want failed with last_error %q", x, want)
			}
			r.advance(2 * time.Hour)
			r.runOnce()
			if c.n() != 1 {
				t.Errorf("a non-retryable failure ran %d times, want once", c.n())
			}
			if r.outcomes("failed") != 1 {
				t.Errorf("failed outcomes = %v, want 1", r.outcomes("failed"))
			}
		})
	}
}

func TestRun_HandlerTimeout_DeclTimeoutAppliesAndRetries(t *testing.T) {
	r := newIRig(t)
	s := r.register(jobs.Decl{Name: "volunteers.remind", Timeout: time.Second, Handler: func(ctx context.Context, _ jobs.Job) error {
		<-ctx.Done() // only Decl.Timeout (1s) can end this
		return errs.E("test", errs.KindOf(ctx.Err()), ctx.Err())
	}})
	id := enqueue(t, s, jobs.Request{Name: "volunteers.remind"})
	ran := r.runOnce()
	if ran.Retried != 1 {
		t.Fatalf("Ran = %+v, want the timed-out run retried", ran)
	}
	x := r.byID(id)
	if x.Status != "queued" || x.LastError == nil || *x.LastError != "timeout" {
		t.Errorf("row = %+v, want queued/timeout", x)
	}
}

// ---- panics ------------------------------------------------------------------------------

func TestRun_HandlerPanics_FailsAndLogsOnce(t *testing.T) {
	r := newIRig(t)
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: func(context.Context, jobs.Job) error {
		panic("handler bug")
	}})
	person := newPerson()
	id := enqueue(t, s, jobs.Request{Name: "volunteers.remind", Args: jobs.Args{"person": signals.RecordID(person)}})
	ran := r.runOnce()
	if ran.Failed != 1 {
		t.Fatalf("Ran = %+v, want one failure", ran)
	}
	x := r.byID(id)
	if x.Status != "failed" || x.LastError == nil || *x.LastError != "panic" || x.Attempts != 1 {
		t.Fatalf("row = %+v, want failed/panic", x)
	}
	if r.outcomes("panicked") != 1 {
		t.Errorf("panicked outcomes = %v, want 1", r.outcomes("panicked"))
	}
	errorLines, stack := 0, false
	for _, rec := range r.cap.Records() {
		if rec.Level >= slog.LevelError {
			errorLines++
		}
		for _, v := range rec.Attrs {
			if strings.Contains(v, ".go:") {
				stack = true
			}
		}
	}
	if errorLines != 1 {
		t.Errorf("%d error-level log lines for one panic, want exactly 1", errorLines)
	}
	if !stack {
		t.Error("the panic's log line carries no stack")
	}
	if r.cap.Contains(person.String()) {
		t.Error("an argument's record ID reached the telemetry")
	}
	r.advance(2 * time.Hour)
	r.runOnce()
	if got := r.byID(id).Attempts; got != 1 {
		t.Errorf("a panicked job was retried (attempts %d)", got)
	}
}

// ---- read-only and shutdown --------------------------------------------------------------

func TestRun_ReadOnlyMidRun_ReleasesAndPauses(t *testing.T) {
	r := newIRig(t)
	var first, second counter
	s := r.register(jobs.Decl{Name: "volunteers.first", Handler: first.handler(errs.E("test", errs.Unavailable, db.ErrReadOnly))})
	r.register(jobs.Decl{Name: "volunteers.second", Handler: second.handler(nil)})
	at := epoch.Add(-time.Minute)
	id := r.forceQueued("volunteers.first", "", at, "")
	r.forceQueued("volunteers.second", "", at.Add(time.Second), "")
	_ = s

	ran := r.runOnce()
	if ran.Released != 1 {
		t.Fatalf("Ran = %+v, want one released row", ran)
	}
	x := r.byID(id)
	if x.Status != "queued" || x.Attempts != 0 || x.Leased != nil || !x.RunAt.Equal(at) {
		t.Fatalf("row = %+v, want queued, attempt not counted, run_at unchanged", x)
	}
	if second.n() != 0 {
		t.Error("the runner kept leasing after meeting db.ErrReadOnly")
	}
	if r.outcomes("read_only") < 1 {
		t.Error("no read_only outcome counted")
	}
	// The copy is read-only: the next tick runs nothing.
	r.flt.set(true, nil)
	_, err := r.run.RunOnce(ctxT(t))
	wantReadOnly(t, err)
	r.flt.set(false, nil)
	if first.n() != 1 || second.n() != 0 {
		t.Fatalf("calls first=%d second=%d while read-only", first.n(), second.n())
	}
}

func TestRun_CanceledOnShutdown_ReleasesRow(t *testing.T) {
	r := newIRig(t)
	b := newBlocker()
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: b.waitCtx})
	id := enqueue(t, s, jobs.Request{Name: "volunteers.remind"})
	res := r.asyncOnce(ctxT(t))
	b.waitStarted(t)
	wantNoErr(t, r.run.Close(ctxT(t)))
	// Close waits for the runs in flight (RunOnce's included): the row is already released
	// when it returns, before RunOnce's result is read.
	if x := r.byID(id); x.Status != "queued" || x.Leased != nil {
		t.Errorf("row right after Close = %+v, want released", x)
	}
	out := r.await(res)
	if out.err != nil && !errs.Is(out.err, errs.Canceled) {
		t.Errorf("RunOnce err = %v, want nil or Canceled", out.err)
	}
	x := r.byID(id)
	if x.Status != "queued" || x.Attempts != 0 || x.Leased != nil || x.Finished != nil {
		t.Fatalf("row = %+v, want released: queued, attempt not counted", x)
	}
	if r.outcomes("canceled") != 1 {
		t.Errorf("canceled outcomes = %v, want 1", r.outcomes("canceled"))
	}

	// Another process picks it up and sees Attempt 1: the shutdown cost nothing.
	r2 := newIRigOn(t, r.pool, r)
	var c counter
	r2.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(nil)})
	r2.runOnce()
	if c.n() != 1 || c.all()[0].Attempt != 1 {
		t.Fatalf("second process saw %+v, want one run with Attempt 1", c.all())
	}
}

func TestRunOnce_CtxCanceledMidTick_ReturnsCanceledAndReleases(t *testing.T) {
	r := newIRig(t)
	b := newBlocker()
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: b.waitCtx})
	id := enqueue(t, s, jobs.Request{Name: "volunteers.remind"})
	ctx, cancel := context.WithCancel(ctxT(t))
	res := r.asyncOnce(ctx)
	b.waitStarted(t)
	cancel()
	out := r.await(res)
	wantKind(t, out.err, errs.Canceled)
	x := r.byID(id)
	if x.Status != "queued" || x.Attempts != 0 || x.Leased != nil {
		t.Fatalf("row = %+v, want released", x)
	}
}

func TestClose_HandlerIgnoresCancel_ReturnsTimeout(t *testing.T) {
	r := newIRig(t)
	b := newBlocker()
	t.Cleanup(b.open)
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: b.waitIgnoringCtx})
	id := enqueue(t, s, jobs.Request{Name: "volunteers.remind"})
	done := r.startRun(ctxT(t))
	var started bool
	r.pump(jobs.DefaultPollInterval, "the loop to start the handler", func() bool {
		select {
		case <-b.started:
			started = true
		default:
		}
		return started
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := r.run.Close(ctx)
	wantKind(t, err, errs.Timeout)
	if got := r.byID(id).Status; got != "running" {
		t.Errorf("abandoned row status = %q, want running until its lease lapses", got)
	}
	b.open()
	<-done
}

// ---- leases ------------------------------------------------------------------------------

func TestRun_LeaseLapsed_OtherRunnerReclaims(t *testing.T) {
	r := newIRig(t)
	var c counter
	r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(nil)})
	id := r.forceRunning("volunteers.remind", "", epoch.Add(-time.Minute)) // its owner crashed
	ran := r.runOnce()
	if ran.Ran < 1 || c.n() != 1 {
		t.Fatalf("Ran = %+v, handler calls %d; want the lapsed row re-run", ran, c.n())
	}
	if c.all()[0].ID != id || c.all()[0].Attempt != 2 {
		t.Errorf("handler got %+v, want the same row with Attempt 2", c.all()[0])
	}
	x := r.byID(id)
	if x.Status != "done" || x.Attempts != 2 || x.Instance == nil || *x.Instance != "test-a" {
		t.Errorf("row = %+v", x)
	}
}

func TestRun_LeaseValid_IsNotReclaimed(t *testing.T) {
	r := newIRig(t)
	var c counter
	r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(nil)})
	id := r.forceRunning("volunteers.remind", "", epoch.Add(time.Minute))
	r.runOnce()
	if c.n() != 0 || r.byID(id).Status != "running" {
		t.Fatalf("a row with a live lease was taken (calls %d)", c.n())
	}
}

func TestRun_TwoRunners_LeaseDisjointRows(t *testing.T) {
	a := newIRig(t, func(o *jobs.Options) { o.Workers = 1 })
	b := newIRigOn(t, a.pool, a)
	var mu sync.Mutex
	seen := map[jobs.JobID][]string{}
	arrived := 0
	gate := make(chan struct{})
	mk := func(who string) jobs.Handler {
		return func(ctx context.Context, j jobs.Job) error {
			mu.Lock()
			seen[j.ID] = append(seen[j.ID], who)
			arrived++
			if arrived == 2 {
				close(gate)
			}
			mu.Unlock()
			select {
			case <-gate: // both runners hold a row at the same moment
			case <-ctx.Done():
				return errs.E("test", errs.Canceled, ctx.Err())
			}
			return nil
		}
	}
	sa := a.register(jobs.Decl{Name: "volunteers.remind", Handler: mk("a")})
	b.register(jobs.Decl{Name: "volunteers.remind", Handler: mk("b")})
	const n = 6
	for i := range n {
		enqueue(t, sa, jobs.Request{Name: "volunteers.remind", Key: "k" + string(rune('a'+i)), RunAt: epoch})
	}
	ra, rb := a.asyncOnce(ctxT(t)), b.asyncOnce(ctxT(t))
	oa, ob := a.await(ra), a.await(rb)
	wantNoErr(t, oa.err)
	wantNoErr(t, ob.err)
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != n {
		t.Fatalf("%d distinct jobs ran, want %d", len(seen), n)
	}
	who := map[string]int{}
	for id, ws := range seen {
		if len(ws) != 1 {
			t.Errorf("job %v ran %d times (%v), want exactly once", id, len(ws), ws)
		}
		who[ws[0]]++
	}
	if who["a"] == 0 || who["b"] == 0 {
		t.Errorf("work split %v, want both runners to take rows", who)
	}
	if got := a.count("status = 'done' AND name = 'volunteers.remind'"); got != n {
		t.Errorf("%d rows done, want %d", got, n)
	}
}

func TestRun_Heartbeat_KeepsLongHandlerLeasePastLease(t *testing.T) {
	r := newIRig(t)
	b := newBlocker()
	s := r.register(jobs.Decl{Name: "volunteers.remind", Timeout: jobs.MaxRunTimeout, Handler: b.waitCtx})
	id := enqueue(t, s, jobs.Request{Name: "volunteers.remind"})
	res := r.asyncOnce(ctxT(t))
	b.waitStarted(t)

	lease := func() time.Time {
		x := r.byID(id)
		if x.Leased == nil {
			return time.Time{}
		}
		return *x.Leased
	}
	prev := lease()
	if want := epoch.Add(jobs.DefaultLease); !prev.Equal(want) {
		t.Fatalf("first lease = %v, want now+Lease = %v", prev, want)
	}
	for i := 0; i < 5; i++ { // five thirds of a lease: 200s, past the 120s lease
		cur := prev
		r.pump(jobs.DefaultLease/3, "the heartbeat to renew the lease", func() bool {
			cur = lease()
			return cur.After(prev)
		})
		prev = cur
	}
	if !prev.After(r.clk.Now()) {
		t.Fatalf("lease %v lapsed at %v despite the heartbeat", prev, r.clk.Now())
	}
	// A second process finds nothing to take.
	other := newIRigOn(t, r.pool, r)
	var c counter
	other.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(nil)})
	other.runOnce()
	if c.n() != 0 {
		t.Fatal("another runner took a row whose lease the heartbeat was keeping")
	}
	b.open()
	out := r.await(res)
	wantNoErr(t, out.err)
	if got := r.byID(id).Status; got != "done" {
		t.Errorf("status = %q, want done", got)
	}
}

func TestRun_LostLease_CancelsAndWritesNothing(t *testing.T) {
	r := newIRig(t)
	b := newBlocker()
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: b.waitCtx})
	id := enqueue(t, s, jobs.Request{Name: "volunteers.remind"})
	res := r.asyncOnce(ctxT(t))
	b.waitStarted(t)
	// Another process "re-leases" the row: its lease value moves under the first runner.
	r.exec("UPDATE core.job SET leased_until = leased_until + interval '1 hour', instance = 'thief' WHERE id = $1", [16]byte(id))
	before := r.byID(id)
	var out onceResult
	r.pump(jobs.DefaultLease/3, "the lost lease to be noticed", func() bool {
		select {
		case out = <-res:
			return true
		default:
			return false
		}
	})
	wantNoErr(t, out.err)
	after := r.byID(id)
	if after.Status != "running" || after.Attempts != before.Attempts || after.Leased == nil ||
		!after.Leased.Equal(*before.Leased) || after.Finished != nil || after.Instance == nil || *after.Instance != "thief" ||
		after.LastError != nil {
		t.Fatalf("row was written after the lease was lost:\nbefore %+v\nafter  %+v", before, after)
	}
	if r.outcomes("lost_lease") != 1 {
		t.Errorf("lost_lease outcomes = %v, want 1", r.outcomes("lost_lease"))
	}
}

// ---- no handler --------------------------------------------------------------------------

func TestRun_NoHandler_FailsNoHandler(t *testing.T) {
	r := newIRig(t)
	id := r.forceQueued("volunteers.gone", "", epoch.Add(-time.Minute), "") // its module was disabled since
	ran := r.runOnce()
	if ran.Failed != 1 {
		t.Fatalf("Ran = %+v, want one failure", ran)
	}
	x := r.byID(id)
	if x.Status != "failed" || x.LastError == nil || *x.LastError != "no_handler" || x.Finished == nil {
		t.Fatalf("row = %+v, want failed/no_handler", x)
	}
	warn := 0
	for _, rec := range r.cap.Records() {
		if rec.Level >= slog.LevelWarn {
			warn++
		}
	}
	if warn != 1 {
		t.Errorf("%d warning-or-worse log lines, want exactly one", warn)
	}
	if r.outcomes("no_handler") != 1 {
		t.Errorf("no_handler outcomes = %v, want 1", r.outcomes("no_handler"))
	}
}

// ---- telemetry ---------------------------------------------------------------------------

func TestRun_Telemetry_CountsLagAndSpans(t *testing.T) {
	r := newIRig(t)
	var c counter
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(nil)})
	r.forceQueued("volunteers.remind", "", epoch.Add(-30*time.Second), "")
	_ = s
	r.runOnce()
	var got float64
	for _, m := range r.cap.Measurements() {
		if m.Instrument == "bms.jobs.runs" && m.Attrs["job"] == "volunteers.remind" {
			got += m.Value
			if m.Attrs["module"] != "volunteers" || m.Attrs["outcome"] != "ok" {
				t.Errorf("attrs = %v", m.Attrs)
			}
		}
	}
	if got != 1 {
		t.Errorf("bms.jobs.runs for the job = %v, want 1", got)
	}
	lag := -1.0
	for _, m := range r.cap.Measurements() {
		if m.Instrument == "bms.jobs.lag" && m.Value >= 29 && m.Value <= 31 {
			lag = m.Value
		}
	}
	if lag < 0 {
		t.Errorf("no bms.jobs.lag sample near 30 seconds in %+v", r.cap.Measurements())
	}
	var tick, run bool
	for _, sp := range r.cap.Spans() {
		switch sp.Name {
		case "jobs.tick":
			tick = true
		case "jobs.run":
			if sp.Kind == telemetry.SpanConsumer && sp.Attrs["job"] == "volunteers.remind" {
				run = true
			}
		}
	}
	if !tick || !run {
		t.Errorf("spans: tick=%v run(consumer)=%v", tick, run)
	}
}

func TestEnqueue_Span_IsProducer(t *testing.T) {
	r := newIRig(t)
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: nop})
	enqueue(t, s, jobs.Request{Name: "volunteers.remind"})
	for _, sp := range r.cap.Spans() {
		if sp.Name == "jobs.enqueue" && sp.Kind == telemetry.SpanProducer {
			return
		}
	}
	t.Error("no jobs.enqueue producer span")
}

var tokenRE = regexp.MustCompile(`^[a-z_]{1,32}$`)

func TestRun_OutcomeAndLastErrorTokens_MatchGrammar(t *testing.T) {
	r := newIRig(t)
	boom := errors.New("x")
	for name, err := range map[jobs.Name]error{
		"volunteers.ok":     nil,
		"volunteers.retry":  errs.E("t", errs.Unavailable, boom),
		"volunteers.bad":    errs.E("t", errs.Invalid, boom),
		"volunteers.plain":  boom,
		"volunteers.denied": errs.E("t", errs.Permission, boom),
	} {
		var c counter
		r.register(jobs.Decl{Name: name, Handler: c.handler(err)})
		r.forceQueued(name, "", epoch.Add(-time.Minute), "")
	}
	r.register(jobs.Decl{Name: "volunteers.panics", Handler: func(context.Context, jobs.Job) error { panic("x") }})
	r.forceQueued("volunteers.panics", "", epoch.Add(-time.Minute), "")
	r.forceQueued("volunteers.gone", "", epoch.Add(-time.Minute), "")
	for range 3 {
		r.runOnce()
	}
	allowed := map[string]bool{"ok": true, "retried": true, "failed": true, "canceled": true, "read_only": true,
		"panicked": true, "no_handler": true, "lost_lease": true, "dropped": true}
	for _, m := range r.cap.Measurements() {
		if m.Instrument != "bms.jobs.runs" {
			continue
		}
		if o := m.Attrs["outcome"]; !allowed[o] || !tokenRE.MatchString(o) {
			t.Errorf("outcome %q is not one of the specified tokens", o)
		}
	}
	for _, x := range r.rows("last_error IS NOT NULL") {
		if !tokenRE.MatchString(*x.LastError) {
			t.Errorf("last_error %q does not match %s", *x.LastError, tokenRE)
		}
	}
	if r.count("last_error IS NOT NULL") == 0 {
		t.Error("scenario produced no last_error to check")
	}
}
