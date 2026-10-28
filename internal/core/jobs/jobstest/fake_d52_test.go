package jobstest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/jobs/jobstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// D52(c): RunDue builds its Job from the row's current Args, not from the first request.
func TestFakeRunDue_UsesCurrentArgsAfterLaterEnqueue(t *testing.T) {
	var got []jobs.Job
	f, _ := newFake(t, func(_ context.Context, j jobs.Job) error { got = append(got, j); return nil })
	first, second := person(), person()
	id := enq(t, f, jobs.Request{Name: remind, Key: "k", RunAt: epoch, Args: jobs.Args{"person": first}})
	if again := enq(t, f, jobs.Request{Name: remind, Key: "k", RunAt: epoch, Args: jobs.Args{"person": second}}); again != id {
		t.Fatalf("second Enqueue returned %v, want the row %v", again, id)
	}
	n, err := f.RunDue(ctxT(t), epoch)
	if err != nil || n != 1 || len(got) != 1 {
		t.Fatalf("RunDue = %d, %v; handler calls %d", n, err, len(got))
	}
	if got[0].Args["person"] != second {
		t.Errorf("handler got args %v, want the later request's %v", got[0].Args, second)
	}
}

// D52(c): a row created by Run (a key with no prior Enqueue) is tracked like an enqueued one.
func TestFakeRun_CreatedRow_IsTrackedLikeAnEnqueuedOne(t *testing.T) {
	f, _ := newFake(t, nil)
	jid := model.NewID[jobs.JobID]()
	if err := f.Run(ctxT(t), remind, jobs.Job{ID: jid, Name: remind, Key: "k", Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	if q := f.Queued(); len(q) != 0 {
		t.Fatalf("a finished row is queued: %+v", q)
	}
	at := epoch.Add(time.Hour)
	got := enq(t, f, jobs.Request{Name: remind, Key: "k", RunAt: at})
	if got != jid {
		t.Errorf("Enqueue of the key returned %v, want the row Run created (%v)", got, jid)
	}
	if q := f.Queued(); len(q) != 1 || !q[0].RunAt.Equal(at) || q[0].Key != "k" {
		t.Fatalf("Queued = %+v, want the reopened row", q)
	}
}

func TestFakeRun_CreatedRow_ReenqueuedByItsOwnHandler_IsQueued(t *testing.T) {
	var f *jobstest.Fake
	at := epoch.Add(2 * time.Hour)
	f, _ = newFake(t, func(ctx context.Context, _ jobs.Job) error {
		_, err := f.Enqueue(ctx, jobs.Request{Name: remind, Key: "k", RunAt: at})
		return wrap(err)
	})
	if err := f.Run(ctxT(t), remind, jobs.Job{Name: remind, Key: "k", Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	if q := f.Queued(); len(q) != 1 || !q[0].RunAt.Equal(at) {
		t.Fatalf("Queued = %+v, want the row at %v", q, at)
	}
	n, err := f.RunDue(ctxT(t), at)
	if err != nil || n != 1 {
		t.Fatalf("RunDue = %d, %v; want the row run once", n, err)
	}
}

// D52(c): a retryable error requeues the row (it is not failed); other kinds end it failed.
func TestFakeRun_ErrorKinds_RequeueOrFail(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		requeue bool
	}{
		{"unavailable requeues", errs.E("t", errs.Unavailable, errors.New("down")), true},
		{"timeout requeues", errs.E("t", errs.Timeout, errors.New("slow")), true},
		{"invalid fails", errs.E("t", errs.Invalid, errors.New("bad")), false},
		{"internal fails", errors.New("bug"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := newFake(t, func(context.Context, jobs.Job) error { return tc.err })
			enq(t, f, jobs.Request{Name: remind, Key: "k", RunAt: epoch})
			n, err := f.RunDue(ctxT(t), epoch)
			if n != 1 || err == nil || errs.KindOf(err) != errs.KindOf(tc.err) {
				t.Fatalf("RunDue = %d, %v", n, err)
			}
			if q := f.Queued(); (len(q) == 1) != tc.requeue {
				t.Fatalf("Queued = %+v, want requeued=%v", q, tc.requeue)
			}
		})
	}
}

// D52(c): the attempt count rises across retries and the row ends failed at max attempts.
func TestFakeRunDue_Retryable_AttemptsRiseThenFailAtMax(t *testing.T) {
	var seen []int
	f, _ := newFake(t, func(_ context.Context, j jobs.Job) error {
		seen = append(seen, j.Attempt)
		return errs.E("t", errs.Unavailable, errors.New("down"))
	})
	enq(t, f, jobs.Request{Name: remind, Key: "k", RunAt: epoch})
	maxAtt := jobs.DefaultRetry.MaxAttempts
	for i := 0; i < maxAtt+3; i++ {
		drain(t, f, epoch) // the Fake skips retry delays
	}
	if len(seen) != maxAtt {
		t.Fatalf("handler ran %d times, want exactly MaxAttempts %d: %v", len(seen), maxAtt, seen)
	}
	for i, a := range seen {
		if a != i+1 {
			t.Fatalf("attempts seen = %v, want 1..%d", seen, maxAtt)
		}
	}
	if q := f.Queued(); len(q) != 0 {
		t.Errorf("Queued = %+v, want the row failed after max attempts", q)
	}
}

// D52(a) on the Fake: a run that re-enqueues its own key and then fails keeps its attempts.
func TestFake_SelfReenqueue_FailingRun_KeepsAttemptsAndGivesUp(t *testing.T) {
	var seen []int
	var f *jobstest.Fake
	f, _ = newFake(t, func(ctx context.Context, j jobs.Job) error {
		seen = append(seen, j.Attempt)
		if _, err := f.Enqueue(ctx, jobs.Request{Name: remind, Key: "k", RunAt: epoch}); err != nil {
			return wrap(err)
		}
		return errs.E("t", errs.Unavailable, errors.New("down"))
	})
	enq(t, f, jobs.Request{Name: remind, Key: "k", RunAt: epoch})
	maxAtt := jobs.DefaultRetry.MaxAttempts
	for i := 0; i < maxAtt+3; i++ {
		drain(t, f, epoch)
	}
	if len(seen) != maxAtt {
		t.Fatalf("handler ran %d times (%v), want %d: attempts must keep rising so the job ends", len(seen), seen, maxAtt)
	}
	for i, a := range seen {
		if a != i+1 {
			t.Fatalf("attempts seen = %v, want 1..%d", seen, maxAtt)
		}
	}
	if q := f.Queued(); len(q) != 0 {
		t.Errorf("Queued = %+v, want the job ended failed", q)
	}
}

// D50 on the Fake: a request from outside the run (a ctx that is not the job's) resets attempts.
func TestFake_OutsideRequest_DuringFailingRun_ResetsAttempts(t *testing.T) {
	var f *jobstest.Fake
	started, release := make(chan struct{}), make(chan struct{})
	var seen []int
	calls := 0
	f, _ = newFake(t, func(_ context.Context, j jobs.Job) error {
		seen = append(seen, j.Attempt)
		calls++
		if calls == 2 {
			close(started)
			<-release
		}
		return errs.E("t", errs.Unavailable, errors.New("down"))
	})
	enq(t, f, jobs.Request{Name: remind, Key: "k", RunAt: epoch})
	drain(t, f, epoch) // attempt 1 fails, requeued
	done := make(chan struct{})
	go func() { defer close(done); drain(t, f, epoch) }() // attempt 2, blocked
	<-started
	enq(t, f, jobs.Request{Name: remind, Key: "k", RunAt: epoch}) // from outside the run
	close(release)
	<-done
	drain(t, f, epoch)
	if len(seen) != 3 || seen[0] != 1 || seen[1] != 2 || seen[2] != 1 {
		t.Fatalf("attempts seen = %v, want [1 2 1] (outside request resets to a fresh row)", seen)
	}
}

// drain runs what is due; the handlers here fail on purpose, so only an unexpected kind fails the test.
func drain(t testing.TB, f *jobstest.Fake, now time.Time) {
	t.Helper()
	if _, err := f.RunDue(ctxT(t), now); err != nil && !errs.Is(err, errs.Unavailable) {
		t.Fatalf("RunDue: %v", err)
	}
}
