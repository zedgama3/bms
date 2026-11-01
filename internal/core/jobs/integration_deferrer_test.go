//go:build integration

package jobs_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// A row Deferrer.Defer inserts is picked up and run by the real runner, which redelivers to the
// real dispatcher; the subscriber that failed once now succeeds and the row ends done.
func TestDefer_RowIsRunnableByTheRunner(t *testing.T) {
	pool := dbtest.TempDB(t)
	migrate(t, pool)
	r, o := newRigOptions(t, pool)
	o.Workers = 1

	def, err := jobs.NewDeferrer(jobs.DeferrerOptions{DB: pool, Clock: r.clk, Instance: o.Instance})
	wantNoErr(t, err)

	disp, err := signals.New(signals.Options{
		Logger: r.cap.Logger(), Tracer: r.cap.Tracer("signals"), Meter: r.cap.Meter("signals"),
		Clock: r.clk, HandlerTimeout: 5 * time.Second, Deferrer: def,
	})
	wantNoErr(t, err)
	o.Redeliverer = disp

	name := signals.Name("alpha.x")
	pub, err := disp.Scope("alpha", []signals.Name{name}, nil)
	wantNoErr(t, err)
	sub, err := disp.Scope("beta", nil, []signals.Name{name})
	wantNoErr(t, err)
	var calls, succeeded atomic.Int64
	wantNoErr(t, sub.Subscribe(name, func(ctx context.Context, _ signals.Signal) error {
		if calls.Add(1) == 1 {
			return errs.E("test.handler", errs.Unavailable, errors.New("down"))
		}
		succeeded.Store(1)
		return nil
	}))

	run, err := jobs.New(o)
	wantNoErr(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := run.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	r.run, r.opts = run, o
	ir := &irig{rig: r, pool: pool, flt: &faultDB{DB: pool}}

	wantNoErr(t, pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		return pub.Publish(ctx, name, signals.NewPayload(signals.Ref("person", model.NewID[model.PersonID]())))
	}))
	if got := calls.Load(); got != 1 {
		t.Fatalf("handler ran %d times after publish, want 1", got)
	}
	x := ir.one(jobs.NameRedeliver)
	if x.Status != "queued" {
		t.Fatalf("deferred row status = %q, want queued", x.Status)
	}
	if x.Key == nil || *x.Key == "" {
		t.Error("deferred row has no key")
	}

	ir.advance(2 * time.Hour)
	ran := ir.runOnce()
	if ran.Failed != 0 {
		t.Errorf("Ran = %+v, want no failure", ran)
	}
	if got := calls.Load(); got != 2 || succeeded.Load() != 1 {
		t.Fatalf("handler ran %d times, want 2 (the runner redelivered the deferred row)", got)
	}
	if got := ir.one(jobs.NameRedeliver); got.Status != "done" {
		t.Errorf("row status = %q, want done", got.Status)
	}
}

// Deferring the same (signal, subscriber) twice through the Deferrer stores one row, in the
// same format as the runner's own Defer.
func TestDeferrer_SameDeliveryTwice_StoresOneRowInRunnerFormat(t *testing.T) {
	r := newIRig(t)
	def, err := jobs.NewDeferrer(jobs.DeferrerOptions{DB: r.pool, Clock: r.clk, Instance: "test-a"})
	wantNoErr(t, err)
	d := delivery(1)
	ctx, span := r.cap.Tracer("jobs").Start(ctxT(t), "test")
	defer span.End()
	trace, ok := telemetry.TraceIDFrom(ctx)
	if !ok {
		t.Fatal("the test context carries no trace")
	}
	wantNoErr(t, def.Defer(ctx, d))
	wantNoErr(t, def.Defer(ctx, d))
	x := r.one(jobs.NameRedeliver)
	if x.RunAt.Before(epoch) || x.RunAt.After(epoch.Add(jobs.DefaultRetry.Delay(d.Attempt+1, 1))) {
		t.Errorf("run_at = %v, want within [now, now + first backoff]", x.RunAt)
	}
	if x.TraceID == nil || *x.TraceID != trace {
		t.Errorf("trace_id = %v, want %q", x.TraceID, trace)
	}
	if want := d.Signal.ID.String() + ":" + d.Subscriber; x.Key == nil || *x.Key != want {
		t.Errorf("key = %v, want %q", x.Key, want)
	}
	a := argsJSON(t, x.Args)
	del, _ := a["delivery"].(map[string]any)
	if del["subscriber"] != d.Subscriber || del["attempt"] != float64(d.Attempt) {
		t.Errorf("args = %v, want the delivery", a)
	}
}

// An invalid Delivery fails Invalid and inserts nothing.
func TestDeferrer_InvalidDelivery_ReturnsInvalid_StoresNothing(t *testing.T) {
	r := newIRig(t)
	def, err := jobs.NewDeferrer(jobs.DeferrerOptions{DB: r.pool, Clock: r.clk, Instance: "test-a"})
	wantNoErr(t, err)
	zeroAttempt := delivery(0)
	badSub := delivery(1)
	badSub.Subscriber = "BAD"
	for name, d := range map[string]signals.Delivery{"zero": {}, "attempt 0": zeroAttempt, "bad subscriber": badSub} {
		t.Run(name, func(t *testing.T) {
			wantKind(t, def.Defer(ctxT(t), d), errs.Invalid)
		})
	}
	if n := r.count(""); n != 0 {
		t.Errorf("%d rows after invalid deliveries, want 0", n)
	}
}
