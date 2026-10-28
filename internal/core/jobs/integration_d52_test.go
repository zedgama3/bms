//go:build integration

package jobs_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/retry"
)

// D52(a): a run that re-enqueues its own key and then fails keeps its attempts, so the job
// cannot retry forever; it ends failed at max attempts.
func TestSelfReenqueue_FailingRun_KeepsAttemptsAndGivesUp(t *testing.T) {
	const maxAtt = 3
	r := newIRig(t, func(o *jobs.Options) {
		o.Retry = retry.Policy{Initial: time.Second, Max: time.Second, Multiplier: 1, MaxAttempts: maxAtt}
	})
	var s jobs.Registrar
	var seen []int
	s = r.register(jobs.Decl{Name: remind, Handler: func(ctx context.Context, j jobs.Job) error {
		seen = append(seen, j.Attempt)
		if _, err := s.Enqueue(ctx, jobs.Request{Name: remind, Key: "k", RunAt: epoch}); err != nil {
			return err
		}
		return errs.E("test", errs.Unavailable, errors.New("down"))
	}})
	id := enqueue(t, s, jobs.Request{Name: remind, Key: "k", RunAt: epoch})
	var attempts []int
	for i := 0; i < maxAtt+3 && r.byID(id).Status != "failed"; i++ {
		r.runOnce()
		r.advance(2 * time.Second)
		attempts = append(attempts, r.byID(id).Attempts)
	}
	x := r.byID(id)
	if x.Status != "failed" {
		t.Fatalf("row = %s after attempts %v (handler saw %v), want it to end failed", x.Status, attempts, seen)
	}
	if len(seen) != maxAtt {
		t.Errorf("handler ran %d times (%v), want %d", len(seen), seen, maxAtt)
	}
	for i, a := range seen {
		if a != i+1 {
			t.Errorf("attempts seen = %v, want 1..%d (kept, not reset)", seen, maxAtt)
			break
		}
	}
	if x.LastError == nil || *x.LastError != "gave_up" {
		t.Errorf("last_error = %v, want gave_up", x.LastError)
	}
	if x.Attempts != maxAtt {
		t.Errorf("final attempts = %d, want %d", x.Attempts, maxAtt)
	}
}

// D50 beside D52(a): a request from outside the run, made while it runs, still resets attempts.
func TestOutsideRequest_DuringFailingRun_ResetsAttempts(t *testing.T) {
	r := newIRig(t)
	b := newBlocker()
	calls := 0
	s := r.register(jobs.Decl{Name: remind, Handler: func(ctx context.Context, j jobs.Job) error {
		calls++
		if calls == 2 {
			b.started <- j
			<-b.release
		}
		return errs.E("test", errs.Unavailable, errors.New("down"))
	}})
	id := enqueue(t, s, jobs.Request{Name: remind, Key: "k", RunAt: epoch})
	r.runOnce() // attempt 1 fails and is retried
	if got := r.byID(id).Attempts; got != 1 {
		t.Fatalf("attempts after the first failure = %d, want 1", got)
	}
	r.advance(2 * time.Minute)
	res := r.asyncOnce(ctxT(t))
	b.waitStarted(t)
	enqueue(t, s, jobs.Request{Name: remind, Key: "k", RunAt: epoch.Add(3 * time.Hour)}) // from outside
	b.open()
	if out := r.await(res); out.err != nil {
		t.Fatalf("RunOnce: %v", out.err)
	}
	x := r.byID(id)
	if x.Status != "queued" || x.Attempts != 0 {
		t.Fatalf("row = %s attempts %d, want queued with attempts reset to 0", x.Status, x.Attempts)
	}
}

// selfReenqueueHandler re-enqueues its own key at `at` and then ends with the outcome:
// "invalid" returns an Invalid error, "panic" panics.
func selfReenqueueHandler(s *jobs.Registrar, at time.Time, kind string) jobs.Handler {
	return func(ctx context.Context, _ jobs.Job) error {
		if _, err := (*s).Enqueue(ctx, jobs.Request{Name: remind, Key: "k", RunAt: at}); err != nil {
			return err
		}
		if kind == "panic" {
			panic("handler bug")
		}
		return errs.E("test", errs.Invalid, errors.New("bad"))
	}
}

// F55 / D52(a): below the limit, a non-retryable failure or a panic after a self-made request
// queues the row at the held time with its attempts kept.
func TestSelfReenqueue_NonRetryableFailure_BelowLimit_QueuesWithAttemptsKept(t *testing.T) {
	for _, kind := range []string{"invalid", "panic"} {
		t.Run(kind, func(t *testing.T) {
			r := newIRig(t)
			var s jobs.Registrar
			at := epoch.Add(4 * time.Hour)
			s = r.register(jobs.Decl{Name: remind, Handler: selfReenqueueHandler(&s, at, kind)})
			id := enqueue(t, s, jobs.Request{Name: remind, Key: "k", RunAt: epoch})
			r.runOnce()
			x := r.byID(id)
			if x.Status != "queued" || x.Attempts != 1 || !x.RunAt.Equal(at) {
				t.Fatalf("row = %+v, want queued at the held time with attempts 1 kept", x)
			}
			if x.LastError == nil || *x.LastError == "" || *x.LastError == "gave_up" {
				t.Errorf("last_error = %v, want the run's own token", x.LastError)
			}
			r.wantNoHeld(id)
		})
	}
}

// F55a: at the limit, a non-retryable failure or panic drops the self-made request and the
// row ends failed gave_up.
func TestSelfReenqueue_NonRetryableFailure_AtLimit_GivesUp(t *testing.T) {
	for _, kind := range []string{"invalid", "panic"} {
		t.Run(kind, func(t *testing.T) {
			const maxAtt = 3
			r := newIRig(t, func(o *jobs.Options) {
				o.Retry = retry.Policy{Initial: time.Second, Max: time.Second, Multiplier: 1, MaxAttempts: maxAtt}
			})
			var s jobs.Registrar
			runs := 0
			inner := selfReenqueueHandler(&s, epoch, kind)
			s = r.register(jobs.Decl{Name: remind, Handler: func(ctx context.Context, j jobs.Job) error {
				runs++
				return inner(ctx, j)
			}})
			id := enqueue(t, s, jobs.Request{Name: remind, Key: "k", RunAt: epoch})
			for i := 0; i < maxAtt+3 && r.byID(id).Status != "failed"; i++ {
				r.runOnce()
				r.advance(2 * time.Second)
			}
			x := r.byID(id)
			if x.Status != "failed" {
				t.Fatalf("row = %s after %d runs, want failed", x.Status, runs)
			}
			if runs != maxAtt || x.Attempts != maxAtt {
				t.Errorf("runs = %d attempts = %d, want %d each", runs, x.Attempts, maxAtt)
			}
			if x.LastError == nil || *x.LastError != "gave_up" {
				t.Errorf("last_error = %v, want gave_up", x.LastError)
			}
			r.wantNoHeld(id)
		})
	}
}

// F56 / D52(a): a release after a self-made request uses attempts - 1 (not 0) and applies it.
func TestSelfReenqueue_Release_UsesAttemptsMinusOne(t *testing.T) {
	r := newIRig(t)
	var s jobs.Registrar
	var calls atomic.Int32
	at := epoch.Add(6 * time.Hour)
	s = r.register(jobs.Decl{Name: remind, Handler: func(ctx context.Context, j jobs.Job) error {
		if calls.Add(1) == 1 {
			return errs.E("test", errs.Unavailable, errors.New("down"))
		}
		if _, err := s.Enqueue(ctx, jobs.Request{Name: remind, Key: "k", RunAt: at}); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}})
	id := enqueue(t, s, jobs.Request{Name: remind, Key: "k", RunAt: epoch})
	r.runOnce() // attempt 1 fails, retried
	r.advance(2 * time.Minute)
	res := r.asyncOnce(ctxT(t))
	r.pump(10*time.Millisecond, "second run to hold its request", func() bool {
		held, _ := r.nextOf(id)
		return r.byID(id).Status == "running" && calls.Load() == 2 && held != nil
	})
	if got := r.byID(id).Attempts; got != 2 {
		t.Fatalf("attempts while running = %d, want 2", got)
	}
	wantNoErr(t, r.run.Close(ctxT(t)))
	r.await(res)
	x := r.byID(id)
	if x.Status != "queued" || x.Attempts != 1 || !x.RunAt.Equal(at) {
		t.Fatalf("row = %+v, want queued at the held time with attempts 1 (2 - 1)", x)
	}
	r.wantNoHeld(id)
}

// job_next_by_run_chk: next_by_run = true needs a held request.
func TestEnqueue_NextByRunWithoutRequest_ConstraintRefuses(t *testing.T) {
	r, s, _ := remindRig(t)
	queued := enqueue(t, s, jobs.Request{Name: remind, Key: "q"})
	running := r.forceRunning(remind, "r", epoch.Add(time.Hour))
	for name, id := range map[string]jobs.JobID{"queued": queued, "running": running} {
		_, err := r.pool.Exec(ctxT(t), `UPDATE core.job SET next_by_run = true WHERE id = $1`, [16]byte(id))
		var dbErr *db.Error
		if err == nil || !errors.As(err, &dbErr) || dbErr.Code != "23514" || dbErr.Constraint != "job_next_by_run_chk" {
			t.Errorf("%s row: err = %v, want 23514 job_next_by_run_chk", name, err)
		} else if !errs.Is(err, errs.Invalid) {
			t.Errorf("%s row: kind = %v, want Invalid", name, errs.KindOf(err))
		}
	}
	_, err := r.pool.Exec(ctxT(t), `UPDATE core.job SET next_run_at = $2, next_args = '{}'::jsonb, next_by_run = true WHERE id = $1`,
		[16]byte(running), epoch)
	if err != nil {
		t.Errorf("next_by_run with a held request was refused: %v", err)
	}
}

// F56 / F55a: the reclaim of a row holding a by-run request keeps attempts below the limit
// and applies the request; at the limit it drops it and the row ends failed gave_up.
func TestReclaim_HeldByRunRequest_KeepsAttemptsOrGivesUp(t *testing.T) {
	for _, tc := range []struct {
		name     string
		attempts int
		want     string
	}{
		{"below_limit", 2, "queued"},
		{"at_limit", jobs.DefaultRetry.MaxAttempts, "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newIRig(t)
			var c counter
			r.register(jobs.Decl{Name: remind, Handler: c.handler(nil)})
			id := r.forceRunningAttempts(remind, tc.attempts, epoch.Add(-time.Minute))
			r.exec(`UPDATE core.job SET key = 'k' WHERE id = $1`, [16]byte(id))
			at := epoch.Add(8 * time.Hour)
			r.exec(`UPDATE core.job SET next_run_at = $2, next_args = '{}'::jsonb, next_by_run = true WHERE id = $1`, [16]byte(id), at)
			r.runOnce()
			x := r.byID(id)
			if x.Status != tc.want || x.Attempts != tc.attempts {
				t.Fatalf("row = %+v, want %s with attempts %d kept", x, tc.want, tc.attempts)
			}
			if tc.want == "queued" {
				if !x.RunAt.Equal(at) {
					t.Errorf("run_at = %v, want the held time %v", x.RunAt, at)
				}
			} else {
				if x.LastError == nil || *x.LastError != "gave_up" || x.Finished == nil {
					t.Errorf("row = %+v, want gave_up with finished_at", x)
				}
			}
			r.wantNoHeld(id)
			if c.n() != 0 {
				t.Errorf("handler ran %d times", c.n())
			}
		})
	}
}
