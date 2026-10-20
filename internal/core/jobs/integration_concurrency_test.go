//go:build integration

package jobs_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/retry"
)

const openRows = "status IN ('queued', 'running')"

// forceRunningAttempts inserts a Running row whose lease lapsed at leasedUntil, with the given
// attempts already spent: a process that died mid-run.
func (r *irig) forceRunningAttempts(name jobs.Name, attempts int, leasedUntil time.Time) jobs.JobID {
	r.t.Helper()
	id := r.forceRunning(name, "", leasedUntil)
	r.exec(`UPDATE core.job SET attempts = $2 WHERE id = $1`, [16]byte(id), attempts)
	return id
}

// ---- crash loop: decision 6 "give up after a fixed count" --------------------------------

func TestRun_CrashedAtMaxAttempts_FailsGaveUpInsteadOfLeasingAgain(t *testing.T) {
	r := newIRig(t)
	var c counter
	r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(nil)})
	id := r.forceRunningAttempts("volunteers.remind", jobs.DefaultRetry.MaxAttempts, epoch.Add(-time.Minute))
	r.runOnce()
	x := r.byID(id)
	if x.Status != "failed" || x.LastError == nil || *x.LastError != "gave_up" || x.Finished == nil || x.Leased != nil {
		t.Fatalf("row = %+v, want failed/gave_up, finished, no lease", x)
	}
	if x.Attempts != jobs.DefaultRetry.MaxAttempts {
		t.Errorf("attempts = %d, want %d (no new attempt)", x.Attempts, jobs.DefaultRetry.MaxAttempts)
	}
	r.runOnce()
	if c.n() != 0 {
		t.Errorf("handler ran %d times for a row that had used all its attempts", c.n())
	}
}

func TestRun_CrashedAtCustomMaxAttempts_FailsGaveUp(t *testing.T) {
	r := newIRig(t, func(o *jobs.Options) {
		o.Retry = retry.Policy{Initial: time.Second, Max: time.Second, Multiplier: 1, MaxAttempts: 3}
	})
	var c counter
	r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(nil)})
	id := r.forceRunningAttempts("volunteers.remind", 3, epoch.Add(-time.Minute))
	r.runOnce()
	x := r.byID(id)
	if x.Status != "failed" || x.LastError == nil || *x.LastError != "gave_up" || c.n() != 0 {
		t.Fatalf("row = %+v, handler calls %d; want failed/gave_up and no run", x, c.n())
	}
}

func TestRun_CrashedBelowMaxAttempts_IsLeasedAgain(t *testing.T) {
	r := newIRig(t)
	b := newBlocker()
	r.register(jobs.Decl{Name: "volunteers.remind", Handler: b.waitCtx})
	maxAtt := jobs.DefaultRetry.MaxAttempts
	id := r.forceRunningAttempts("volunteers.remind", maxAtt-1, epoch.Add(-time.Minute))
	res := r.asyncOnce(ctxT(t))
	j := b.waitStarted(t)
	if j.ID != id || j.Attempt != maxAtt {
		t.Errorf("handler got %+v, want the same row at attempt %d", j, maxAtt)
	}
	x := r.byID(id)
	if x.Status != "running" || x.Attempts != maxAtt {
		t.Errorf("row = %+v, want running at attempt %d", x, maxAtt)
	}
	if x.LastError == nil || *x.LastError != "lost_lease" {
		t.Errorf("last_error = %v, want lost_lease recorded for the crashed attempt", x.LastError)
	}
	b.open()
	r.await(res)
	if x := r.byID(id); x.Status != "done" {
		t.Errorf("status = %q after the rerun succeeded", x.Status)
	}
}

func TestRun_CrashedAtMaxAttempts_ReadOnlyCopy_ChangesNothing(t *testing.T) {
	r := newIRig(t)
	var c counter
	r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(nil)})
	id := r.forceRunningAttempts("volunteers.remind", jobs.DefaultRetry.MaxAttempts, epoch.Add(-time.Minute))
	before := r.byID(id)
	r.flt.set(true, nil)
	_, err := r.run.RunOnce(ctxT(t))
	wantReadOnly(t, err)
	r.flt.set(false, nil)
	after := r.byID(id)
	if after.Status != "running" || after.Attempts != before.Attempts || !after.UpdatedAt.Equal(before.UpdatedAt) ||
		(after.LastError == nil) != (before.LastError == nil) || c.n() != 0 {
		t.Fatalf("row changed on a read-only copy: before %+v after %+v (calls %d)", before, after, c.n())
	}
}

// ---- no overlap: decision 7, F25 ---------------------------------------------------------

// barrier releases n goroutines together.
type barrier struct {
	ready sync.WaitGroup
	start chan struct{}
}

func newBarrier(n int) *barrier {
	b := &barrier{start: make(chan struct{})}
	b.ready.Add(n)
	return b
}

// wait marks the caller ready and blocks until every participant is.
func (b *barrier) wait() {
	b.ready.Done()
	<-b.start
}

// release waits for every participant to be ready, then lets them go.
func (b *barrier) release() {
	b.ready.Wait()
	close(b.start)
}

func TestRunNow_TwoConcurrent_OneWinsOneConflictRunning(t *testing.T) {
	r, _, _ := adminIRig(t)
	ctx := r.holder(permManage)
	for round := range 10 {
		r.exec(`DELETE FROM core.job WHERE name = 'volunteers.sweep'`)
		bar := newBarrier(2)
		errsCh := make(chan error, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				bar.wait()
				_, err := r.run.RunNow(ctx, "volunteers.sweep")
				errsCh <- err
			}()
		}
		bar.release()
		wg.Wait()
		close(errsCh)
		ok, conflicts := 0, 0
		for err := range errsCh {
			switch {
			case err == nil:
				ok++
			case errs.Is(err, errs.Conflict) && hasField(err, "", "running"):
				conflicts++
			default:
				t.Fatalf("round %d: unexpected error %v", round, describe(err))
			}
		}
		if ok != 1 || conflicts != 1 {
			t.Fatalf("round %d: %d succeeded, %d Conflict running; want 1 and 1", round, ok, conflicts)
		}
		if n := r.count("name = 'volunteers.sweep' AND " + openRows); n != 1 {
			t.Fatalf("round %d: %d open rows, want 1", round, n)
		}
	}
}

func TestTick_RacingRunNowAtSlotBoundary_AtMostOneOpenRow(t *testing.T) {
	r := newIRig(t)
	var c counter
	// A retryable failure keeps whichever row ran open, so a second one cannot hide as "done".
	r.register(jobs.Decl{Name: "volunteers.sweep", Every: time.Hour, Handler: c.handler(errs.E("test", errs.Unavailable, errors.New("down")))})
	ctx := r.holder(permManage)
	for round := range 15 {
		r.exec(`DELETE FROM core.job WHERE name = 'volunteers.sweep'`)
		r.advance(time.Hour) // a fresh slot boundary: the scheduler wants to insert it now
		bar := newBarrier(2)
		var wg sync.WaitGroup
		var runNowErr, tickErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			bar.wait()
			_, runNowErr = r.run.RunNow(ctx, "volunteers.sweep")
		}()
		go func() {
			defer wg.Done()
			bar.wait()
			_, tickErr = r.run.RunOnce(ctxT(t))
		}()
		bar.release()
		wg.Wait()
		if tickErr != nil {
			t.Fatalf("round %d: RunOnce: %v", round, describe(tickErr))
		}
		if runNowErr != nil && !errs.Is(runNowErr, errs.Conflict) {
			t.Fatalf("round %d: RunNow: %v, want success or Conflict", round, describe(runNowErr))
		}
		if n := r.count("name = 'volunteers.sweep' AND " + openRows); n > 1 {
			t.Fatalf("round %d: %d open rows of one name, want at most 1", round, n)
		}
	}
}
