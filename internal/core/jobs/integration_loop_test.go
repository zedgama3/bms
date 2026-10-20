//go:build integration

package jobs_test

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"testing"

	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestRun_Loop_RunsDueJobsThenStopsOnClose(t *testing.T) {
	r := newIRig(t)
	var c counter
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(nil)})
	id := enqueue(t, s, jobs.Request{Name: "volunteers.remind"})
	done := r.startRun(ctxT(t))
	r.pump(jobs.DefaultPollInterval, "the loop to run the job", func() bool { return c.n() >= 1 })
	eventually(t, "the row to be recorded done", func() bool { return r.byID(id).Status == "done" })
	wantNoErr(t, r.run.Close(ctxT(t)))
	wantNoErr(t, <-done)
}

func TestRun_Loop_ReadOnlyRunsNothingThenResumes(t *testing.T) {
	r := newIRig(t)
	var c counter
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(nil)})
	id := enqueue(t, s, jobs.Request{Name: "volunteers.remind"})
	r.flt.set(true, nil) // this copy is a replica now
	ctx, cancel := context.WithCancel(ctxT(t))
	done := r.startRun(ctx)
	for range 5 {
		r.advance(jobs.DefaultPollInterval)
		for range 200 {
			runtime.Gosched()
		}
	}
	if c.n() != 0 {
		t.Fatalf("a read-only copy ran %d jobs", c.n())
	}
	if x := r.byID(id); x.Status != "queued" || x.Attempts != 0 || x.Leased != nil {
		t.Fatalf("row touched on a read-only copy: %+v", x)
	}
	r.flt.set(false, nil) // promoted
	r.pump(jobs.DefaultPollInterval, "the promoted copy to run the job", func() bool { return c.n() >= 1 })
	cancel()
	wantNoErr(t, <-done)
}

func TestTick_StoreUnavailable_LogsOnceAndRetries(t *testing.T) {
	r := newIRig(t)
	var c counter
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(nil)})
	enqueue(t, s, jobs.Request{Name: "volunteers.remind"})
	r.flt.set(false, errs.E("test", errs.Unavailable, errors.New("connection refused")))
	ctx, cancel := context.WithCancel(ctxT(t))
	done := r.startRun(ctx)
	for range 5 { // five failing ticks, each seen to hit the database
		before := r.flt.calls.Load()
		r.pump(jobs.DefaultPollInterval, "a failing tick", func() bool { return r.flt.calls.Load() > before })
	}
	loud := 0
	for _, rec := range r.cap.Records() {
		if rec.Level >= slog.LevelWarn {
			loud++
		}
	}
	if loud != 1 {
		t.Errorf("%d warning-or-worse lines over five failing ticks, want exactly 1 (once per change of state)", loud)
	}
	if c.n() != 0 {
		t.Fatal("job ran while the store was down")
	}
	r.flt.set(false, nil)
	r.pump(jobs.DefaultPollInterval, "the next tick to retry and run the job", func() bool { return c.n() >= 1 })
	cancel()
	wantNoErr(t, <-done)
}
