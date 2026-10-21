//go:build integration

package jobs_test

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// F51 (Q-JOBS8): a scheduled name never has two open rows, Retry included.
func TestRetry_ScheduledNameOpen_ReturnsConflict(t *testing.T) {
	for _, open := range []string{"queued", "running"} {
		t.Run(open, func(t *testing.T) {
			r, _, _ := adminIRig(t) // volunteers.sweep is scheduled (Every: hour)
			failed := r.forceFinal("volunteers.sweep", "slot-1", "failed", epoch.Add(-2*time.Hour))
			if open == "queued" {
				r.forceQueued("volunteers.sweep", "slot-2", epoch.Add(time.Hour), "")
			} else {
				r.forceRunning("volunteers.sweep", "slot-2", epoch.Add(time.Hour))
			}
			before := len(r.aud.Recorded())
			err := r.run.Retry(r.holder(permManage), failed)
			wantKind(t, err, errs.Conflict)
			if !hasField(err, "", "running") {
				t.Errorf("fields %v, want code running", describe(err))
			}
			if got := r.byID(failed).Status; got != "failed" {
				t.Errorf("status = %q, want failed", got)
			}
			if len(r.aud.Recorded()) != before {
				t.Error("audited a refused retry")
			}
		})
	}
	t.Run("no open row retries", func(t *testing.T) {
		r, _, _ := adminIRig(t)
		failed := r.forceFinal("volunteers.sweep", "slot-1", "failed", epoch.Add(-2*time.Hour))
		wantNoErr(t, r.run.Retry(r.holder(permManage), failed))
	})
	t.Run("on-demand names are unaffected", func(t *testing.T) {
		r, _, _ := adminIRig(t)
		failed := r.forceFinal("volunteers.remind", "a", "failed", epoch.Add(-2*time.Hour))
		r.forceQueued("volunteers.remind", "b", epoch.Add(time.Hour), "")
		wantNoErr(t, r.run.Retry(r.holder(permManage), failed))
	})
}

// F52 (Q-JOBS9): heartbeats that keep failing past the fence cancel the run and write nothing.
func TestRun_HeartbeatsFailPastFence_CancelsAndWritesNothing(t *testing.T) {
	r := newIRig(t)
	b := newBlocker()
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: b.waitCtx})
	id := enqueue(t, s, jobs.Request{Name: "volunteers.remind"})
	res := r.asyncOnce(ctxT(t))
	b.waitStarted(t)
	before := r.byID(id)
	// Every renewal now fails (the database is unreachable) until the lease has lapsed.
	r.flt.mu.Lock()
	r.flt.failSQL = func(sql string) error {
		if strings.Contains(sql, "AND leased_until =") && strings.Contains(sql, "RETURNING leased_until") {
			return errs.E("test.fault", errs.Unavailable, errors.New("database unreachable"))
		}
		return nil
	}
	r.flt.mu.Unlock()
	var out onceResult
	r.pump(jobs.DefaultLease/3, "the run to be canceled past its fence", func() bool {
		select {
		case out = <-res:
			return true
		default:
			return false
		}
	})
	r.flt.mu.Lock()
	r.flt.failSQL = nil
	r.flt.mu.Unlock()
	wantNoErr(t, out.err)
	after := r.byID(id)
	if after.Status != "running" || after.Attempts != before.Attempts || after.Finished != nil ||
		after.Leased == nil || !after.Leased.Equal(*before.Leased) || after.LastError != nil {
		t.Fatalf("row was written after the fence passed:\nbefore %+v\nafter  %+v", before, after)
	}
	if r.outcomes("lost_lease") != 1 {
		t.Errorf("lost_lease outcomes = %v, want 1", r.outcomes("lost_lease"))
	}
	if r.outcomes("done") != 0 {
		t.Errorf("done outcomes = %v, want 0", r.outcomes("done"))
	}
}

// F52 (decision 19): the fence is the lease expiry the last successful renewal set. A renewal whose
// clock reading is later than its tick (jitter) puts the fence off the tick grid; the run must
// still be canceled by the fence, not at the first tick after it (up to Lease/3 late).
func TestRun_HeartbeatJitterAfterRenewal_CancelsByTheFence(t *testing.T) {
	const jitter = time.Millisecond
	step := jobs.DefaultLease / 3
	r := newIRig(t)
	b := newBlocker()
	t.Cleanup(b.open) // runs before the runner's Close, so a failing run does not hang it
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: b.waitCtx})
	id := enqueue(t, s, jobs.Request{Name: "volunteers.remind"})
	res := r.asyncOnce(ctxT(t))
	b.waitStarted(t)
	before := r.byID(id)

	// The first renewal succeeds; every later one fails (the database is unreachable).
	var renewals atomic.Int64
	r.flt.mu.Lock()
	r.flt.failSQL = func(sql string) error {
		if strings.Contains(sql, "AND leased_until =") && strings.Contains(sql, "RETURNING leased_until") &&
			renewals.Add(1) > 1 {
			return errs.E("test.fault", errs.Unavailable, errors.New("database unreachable"))
		}
		return nil
	}
	r.flt.mu.Unlock()

	// One step plus jitter in a single advance: the first tick fires at +step, but the beat
	// reads the clock only once Advance returns, so its "now" (and the fence it stores) is
	// step+jitter after the lease was taken.
	r.advance(step + jitter)
	var fence time.Time
	eventually(t, "the first renewal to be stored", func() bool {
		if l := r.byID(id).Leased; l != nil && l.After(*before.Leased) {
			fence = *l
			return true
		}
		return false
	})
	if want := before.Leased.Add(step + jitter); !fence.Equal(want) {
		t.Fatalf("fence = %v, want %v (the renewal should read the clock %v after its tick)", fence, want, jitter)
	}
	afterRenewal := r.byID(id)

	// Walk the clock onto the tick grid (2*step, 3*step, 4*step from the lease): every beat now
	// reads exactly its tick, and each fails. The fence sits jitter past the 4th tick.
	// Each beat is awaited (it counts itself once it has read the clock) before the clock moves on,
	// so no beat can read a later time than its own tick.
	awaitBeat := func(n int64) {
		eventually(t, "the beat to reach the store", func() bool { return renewals.Load() >= n })
	}
	awaitBeat(1)
	r.advance(step - jitter)
	awaitBeat(2)
	for i := int64(3); i <= 4; i++ {
		r.advance(step)
		awaitBeat(i)
	}
	if got := r.clk.Now(); !got.Before(fence) {
		t.Fatalf("test setup: clock %v already at the fence %v", got, fence)
	}
	// The clock reaches the fence; no tick falls on it, so only a fence-aware check can act (a
	// look-ahead on the last tick may have canceled earlier, which is also within the spec).
	r.advance(fence.Sub(r.clk.Now()))
	var out onceResult
	eventually(t, "the run to be canceled by the fence", func() bool {
		select {
		case out = <-res:
			return true
		default:
			return false
		}
	})
	wantNoErr(t, out.err)
	if now := r.clk.Now(); now.After(fence) {
		t.Errorf("canceled with the clock at %v, after the fence %v", now, fence)
	}
	after := r.byID(id)
	if after.Status != "running" || after.Attempts != afterRenewal.Attempts || after.Finished != nil ||
		after.Leased == nil || !after.Leased.Equal(fence) || after.LastError != nil ||
		!after.UpdatedAt.Equal(afterRenewal.UpdatedAt) {
		t.Fatalf("row was written after the fence:\nbefore %+v\nafter  %+v", afterRenewal, after)
	}
	if r.outcomes("lost_lease") != 1 || r.outcomes("done") != 0 {
		t.Errorf("outcomes lost_lease=%v done=%v, want 1 and 0", r.outcomes("lost_lease"), r.outcomes("done"))
	}
}

// Q-JOBS11: the reclaim counts a requeue as Released and lost_lease; a give-up as failed.
func TestRunOnce_ReclaimedRow_CountsReleasedAndLostLease(t *testing.T) {
	t.Run("requeued", func(t *testing.T) {
		r := newIRig(t)
		var c counter
		r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(nil)})
		r.forceRunningAttempts("volunteers.remind", jobs.DefaultRetry.MaxAttempts-1, epoch.Add(-time.Minute))
		ran := r.runOnce()
		if ran.Released != 1 {
			t.Errorf("Ran.Released = %d, want 1 (the reclaimed row)", ran.Released)
		}
		if got := r.metric("bms.jobs.runs", "outcome", "lost_lease"); got != 1 {
			t.Errorf("lost_lease outcomes = %v, want 1", got)
		}
		if got := r.metric("bms.jobs.runs", "module", "volunteers"); got < 1 {
			t.Errorf("no bms.jobs.runs measurement carries module=volunteers")
		}
	})
	t.Run("gave up", func(t *testing.T) {
		r := newIRig(t)
		var c counter
		r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(nil)})
		r.forceRunningAttempts("volunteers.remind", jobs.DefaultRetry.MaxAttempts, epoch.Add(-time.Minute))
		ran := r.runOnce()
		if ran.Released != 0 || ran.Failed != 1 {
			t.Errorf("Ran = %+v, want Failed 1 and Released 0", ran)
		}
		if r.outcomes("failed") != 1 || r.outcomes("lost_lease") != 0 {
			t.Errorf("failed=%v lost_lease=%v, want 1 and 0", r.outcomes("failed"), r.outcomes("lost_lease"))
		}
	})
}
