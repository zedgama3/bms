//go:build integration

package jobs_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func sweepRig(t *testing.T) (*irig, *counter) {
	t.Helper()
	r := newIRig(t)
	c := &counter{}
	r.register(jobs.Decl{Name: "volunteers.sweep", Every: time.Hour, Handler: c.handler(nil)})
	return r, c
}

func TestTick_ScheduledName_InsertsCurrentSlotAndRunsIt(t *testing.T) {
	r, c := sweepRig(t)
	ran := r.runOnce()
	if ran.Scheduled < 2 { // the sweep and jobs.purge
		t.Fatalf("Scheduled = %d, want the sweep's slot and jobs.purge's", ran.Scheduled)
	}
	x := r.one("volunteers.sweep")
	if x.Key == nil || *x.Key != "2026-10-19T12:00:00Z" {
		t.Errorf("slot key = %v, want the slot time in RFC 3339 UTC", x.Key)
	}
	if !x.RunAt.Equal(epoch) || x.Status != "done" || c.n() != 1 {
		t.Errorf("row = %+v, handler runs %d; want the slot run once", x, c.n())
	}
	if c.all()[0].Key != "2026-10-19T12:00:00Z" {
		t.Errorf("Job.Key = %q", c.all()[0].Key)
	}
}

func TestTick_SlotIsTruncatedToEvery(t *testing.T) {
	r, _ := sweepRig(t)
	r.advance(2*time.Hour + 25*time.Minute) // 14:25
	r.runOnce()
	x := r.one("volunteers.sweep")
	if *x.Key != "2026-10-19T14:00:00Z" || !x.RunAt.Equal(epoch.Add(2*time.Hour)) {
		t.Errorf("slot key %v run_at %v, want the 14:00 slot", *x.Key, x.RunAt)
	}
}

func TestTick_OnDemandName_IsNeverScheduled(t *testing.T) {
	r := newIRig(t)
	var c counter
	r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(nil)})
	r.runOnce()
	r.advance(5 * time.Hour)
	r.runOnce()
	if n := len(r.byName("volunteers.remind")); n != 0 || c.n() != 0 {
		t.Fatalf("%d rows and %d runs for an on-demand name", n, c.n())
	}
}

func TestTick_SameSlotTwice_IsNoop(t *testing.T) {
	r, c := sweepRig(t)
	r.runOnce()
	ran := r.runOnce() // same instant: the slot exists and is finished
	if n := len(r.byName("volunteers.sweep")); n != 1 || c.n() != 1 {
		t.Fatalf("%d rows, %d runs after a second tick in the same slot; want 1 and 1 (Ran %+v)", n, c.n(), ran)
	}
	r.advance(30 * time.Minute) // still the 12:00 slot
	r.runOnce()
	if n := len(r.byName("volunteers.sweep")); n != 1 {
		t.Fatalf("%d rows within one slot, want 1", n)
	}
}

func TestTick_SlotExists_IsNoop(t *testing.T) { // F39: a second process on the same database
	a, ca := sweepRig(t)
	b := newIRigOn(t, a.pool, a)
	var cb counter
	b.register(jobs.Decl{Name: "volunteers.sweep", Every: time.Hour, Handler: cb.handler(nil)})
	for hour := range 3 {
		var wg sync.WaitGroup
		results := make(chan onceResult, 2)
		for _, r := range []*irig{a, b} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ran, err := r.run.RunOnce(ctxT(t))
				results <- onceResult{ran, err}
			}()
		}
		wg.Wait()
		close(results)
		for res := range results {
			wantNoErr(t, res.err)
		}
		if n := len(a.byName("volunteers.sweep")); n != hour+1 {
			t.Fatalf("hour %d: %d slot rows, want %d (one per slot)", hour, n, hour+1)
		}
		if got := ca.n() + cb.n(); got != hour+1 {
			t.Fatalf("hour %d: %d runs across both processes, want %d", hour, got, hour+1)
		}
		a.advance(time.Hour)
	}
}

func TestTick_ScheduledNameOpen_InsertsNoSlot(t *testing.T) { // F38 and no overlap
	r := newIRig(t)
	var c counter
	r.register(jobs.Decl{Name: "volunteers.sweep", Every: time.Hour,
		Handler: c.handler(errs.E("test", errs.Unavailable, errors.New("down")))})
	r.runOnce() // slot 12:00 runs, fails retryably, stays open
	if x := r.one("volunteers.sweep"); x.Status != "queued" {
		t.Fatalf("slot status = %q, want queued (open)", x.Status)
	}
	r.advance(90 * time.Minute) // the 13:00 slot is due, the 12:00 row still waits
	r.runOnce()
	if n := len(r.byName("volunteers.sweep")); n != 1 {
		t.Fatalf("%d rows, want 1: a name is never scheduled over its own open row", n)
	}
}

func TestTick_NoOverlap_RunningSlotBlocksNextSlot(t *testing.T) {
	a, _ := sweepRig(t)
	b := newBlocker()
	a.register(jobs.Decl{Name: "volunteers.long", Every: time.Minute, Handler: b.waitCtx})
	res := a.asyncOnce(ctxT(t))
	b.waitStarted(t)
	// While the 12:00 slot runs, a second process ticks after the next slot came due.
	a2 := newIRigOn(t, a.pool, a)
	var c2 counter
	a2.register(jobs.Decl{Name: "volunteers.long", Every: time.Minute, Handler: c2.handler(nil)})
	a.advance(90 * time.Second) // the 12:01 slot is due; the lease (2m) has not lapsed
	ran, err := a2.run.RunOnce(ctxT(t))
	wantNoErr(t, err)
	if n := len(a.byName("volunteers.long")); n != 1 || c2.n() != 0 {
		t.Fatalf("%d rows and %d overlapping runs; want 1 and 0 (Ran %+v)", n, c2.n(), ran)
	}
	b.open()
	a.await(res)
}

func TestTick_NoCatchUp_AfterDowntime(t *testing.T) {
	r, c := sweepRig(t)
	r.runOnce()
	r.advance(5 * 24 * time.Hour) // the process was down for 5 days: 120 slots (less than DoneTTL, so the first row is still there)
	r.runOnce()
	if n := len(r.byName("volunteers.sweep")); n != 2 || c.n() != 2 {
		t.Fatalf("%d rows and %d runs after 5 days, want 2 and 2 (the first slot and the current one)", n, c.n())
	}
}

func TestTick_ReadOnly_InsertsNothing(t *testing.T) {
	r, c := sweepRig(t)
	r.flt.set(true, nil)
	_, err := r.run.RunOnce(ctxT(t))
	wantReadOnly(t, err)
	r.flt.set(false, nil)
	if n := r.count(""); n != 0 || c.n() != 0 {
		t.Fatalf("a read-only tick left %d rows and %d runs", n, c.n())
	}
}

func TestTick_StoreUnavailable_ReturnsStoreKind(t *testing.T) {
	r, _ := sweepRig(t)
	r.flt.set(false, errs.E("test", errs.Unavailable, errors.New("down")))
	_, err := r.run.RunOnce(ctxT(t))
	wantKind(t, err, errs.Unavailable)
	r.flt.set(false, errs.E("test", errs.Timeout, errors.New("slow")))
	_, err = r.run.RunOnce(ctxT(t))
	wantKind(t, err, errs.Timeout)
}
