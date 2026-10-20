//go:build integration

package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func remindRig(t *testing.T, mods ...func(*jobs.Options)) (*irig, jobs.Registrar, *counter) {
	t.Helper()
	r := newIRig(t, mods...)
	c := &counter{}
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: c.handler(nil)})
	return r, s, c
}

func TestMigrations_Apply_AndTableExists(t *testing.T) {
	r := newIRig(t)
	if n := r.count(""); n != 0 {
		t.Fatalf("fresh table holds %d rows", n)
	}
}

func TestEnqueue_Happy_StoresQueuedRow(t *testing.T) {
	r, s, _ := remindRig(t)
	person := newPerson()
	args := jobs.Args{"person": signals.RecordID(person)}
	id, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind", Args: args})
	wantNoErr(t, err)
	if id.IsZero() {
		t.Fatal("zero JobID")
	}
	x := r.one("volunteers.remind")
	if x.jobID() != id || x.Status != "queued" || x.Attempts != 0 || x.Key != nil {
		t.Fatalf("row = %+v", x)
	}
	if !x.RunAt.Equal(epoch) || !x.CreatedAt.Equal(epoch) {
		t.Errorf("run_at %v created_at %v, want the clock's now %v", x.RunAt, x.CreatedAt, epoch)
	}
	if x.Leased != nil || x.Finished != nil || x.Started != nil || x.LastError != nil || x.Instance != nil {
		t.Errorf("fresh row carries run state: %+v", x)
	}
	got := argsJSON(t, x.Args)
	if got["person"] != person.String() || len(got) != 1 {
		t.Errorf("args = %v, want only the person reference", got)
	}
}

func TestEnqueue_RunAt_FutureStored_PastIsDue(t *testing.T) {
	r, s, c := remindRig(t)
	future := epoch.Add(3 * time.Hour)
	fid, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind", Key: "future", RunAt: future})
	wantNoErr(t, err)
	pid, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind", Key: "past", RunAt: epoch.Add(-48 * time.Hour)})
	wantNoErr(t, err)
	if got := r.byID(fid).RunAt; !got.Equal(future) {
		t.Errorf("future run_at = %v, want %v", got, future)
	}
	if got := r.byID(pid).RunAt; got.After(epoch) {
		t.Errorf("past run_at = %v, want <= now", got)
	}
	r.runOnce()
	if c.n() != 1 || c.all()[0].Key != "past" {
		t.Fatalf("only the due job should run, handler saw %+v", c.all())
	}
	r.advance(3 * time.Hour)
	r.runOnce()
	if c.n() != 2 {
		t.Fatalf("handler ran %d times after the future job came due, want 2", c.n())
	}
}

func TestEnqueue_Unkeyed_TwiceMakesTwoRows(t *testing.T) {
	r, s, _ := remindRig(t)
	a, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind"})
	wantNoErr(t, err)
	b, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind"})
	wantNoErr(t, err)
	if a == b || r.count("") != 2 {
		t.Fatalf("ids %v %v, %d rows; want two distinct rows", a, b, r.count(""))
	}
}

func TestEnqueue_InsideRolledBackTransaction_LeavesNoJob(t *testing.T) {
	r, s, c := remindRig(t)
	boom := errors.New("caller's change failed")
	err := r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if _, e := s.Enqueue(ctx, jobs.Request{Name: "volunteers.remind", Key: "rolled-back"}); e != nil {
			return fmt.Errorf("enqueue: %w", e)
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Transact err = %v", err)
	}
	if n := r.count(""); n != 0 {
		t.Fatalf("%d rows after rollback, want none", n)
	}
	r.runOnce()
	if c.n() != 0 {
		t.Errorf("handler ran for a rolled-back job")
	}
}

func TestEnqueue_InsideCommittedTransaction_JobAppearsOnCommit(t *testing.T) {
	r, s, _ := remindRig(t)
	var duringTx int
	fresh := ctxT(t) // not the transaction's context: it reaches the pool on another connection
	err := r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if _, e := s.Enqueue(ctx, jobs.Request{Name: "volunteers.remind", Key: "committed"}); e != nil {
			return fmt.Errorf("enqueue: %w", e)
		}
		// A fresh context reaches the pool on another connection: it must not see the row yet.
		return r.pool.QueryRow(fresh, "SELECT count(*) FROM core.job").Scan(&duringTx)
	})
	wantNoErr(t, err)
	if duringTx != 0 {
		t.Errorf("another connection saw %d rows before commit", duringTx)
	}
	if n := r.count(""); n != 1 {
		t.Fatalf("%d rows after commit, want 1", n)
	}
}

func TestEnqueue_InsideReadTx_ReturnsInvalid(t *testing.T) {
	r, s, _ := remindRig(t)
	err := r.pool.Transact(ctxT(t), db.Read, func(ctx context.Context) error {
		_, e := s.Enqueue(ctx, jobs.Request{Name: "volunteers.remind"})
		return fmt.Errorf("enqueue: %w", e)
	})
	wantKind(t, err, errs.Invalid)
	if n := r.count(""); n != 0 {
		t.Errorf("%d rows after a refused Enqueue", n)
	}
}

func TestEnqueue_ReadOnly_ReturnsErrReadOnly_NoRow(t *testing.T) {
	r, s, _ := remindRig(t)
	r.flt.set(true, nil)
	_, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind"})
	wantReadOnly(t, err)
	r.flt.set(false, nil)
	if n := r.count(""); n != 0 {
		t.Errorf("%d rows after a refused Enqueue", n)
	}
}

func TestEnqueue_StoreUnavailable_ReturnsUnavailable(t *testing.T) {
	r, s, _ := remindRig(t)
	r.flt.set(false, errs.E("test", errs.Unavailable, errors.New("connection refused")))
	_, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind"})
	wantKind(t, err, errs.Unavailable)
	r.flt.set(false, errs.E("test", errs.Timeout, errors.New("deadline")))
	_, err = s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind"})
	wantKind(t, err, errs.Timeout)
	r.flt.set(false, nil)
	ctx, cancel := context.WithCancel(ctxT(t))
	cancel()
	_, err = s.Enqueue(ctx, jobs.Request{Name: "volunteers.remind"})
	wantKind(t, err, errs.Canceled)
	dctx, dcancel := context.WithDeadline(ctxT(t), time.Now().Add(-time.Second))
	defer dcancel()
	_, err = s.Enqueue(dctx, jobs.Request{Name: "volunteers.remind"})
	wantKind(t, err, errs.Timeout)
	if n := r.count(""); n != 0 {
		t.Errorf("%d rows after failed Enqueues", n)
	}
}

func TestEnqueue_KeyQueued_RescheduleMovesRunAtAndArgs(t *testing.T) {
	r, s, c := remindRig(t)
	p1, p2 := newPerson(), newPerson()
	first := epoch.Add(2 * time.Hour)
	second := epoch.Add(5 * time.Hour)
	id1, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind", Key: "shift-1", RunAt: first,
		Args: jobs.Args{"person": signals.RecordID(p1)}})
	wantNoErr(t, err)
	id2, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind", Key: "shift-1", RunAt: second,
		Args: jobs.Args{"person": signals.RecordID(p2)}})
	wantNoErr(t, err)
	if id1 != id2 {
		t.Fatalf("reschedule returned %v, want the original %v", id2, id1)
	}
	if n := r.count(""); n != 1 {
		t.Fatalf("%d rows, want 1", n)
	}
	x := r.byID(id1)
	if !x.RunAt.Equal(second) || x.Status != "queued" {
		t.Errorf("row = %+v, want queued at %v", x, second)
	}
	if got := argsJSON(t, x.Args); got["person"] != p2.String() {
		t.Errorf("args = %v, want the new person %v", got, p2)
	}
	r.advance(3 * time.Hour) // past the old time, before the new one
	r.runOnce()
	if c.n() != 0 {
		t.Fatal("the job ran at its old time")
	}
	r.advance(3 * time.Hour)
	r.runOnce()
	if c.n() != 1 {
		t.Fatalf("handler ran %d times at the new time, want 1", c.n())
	}
	if got := c.all()[0].Args["person"]; got != signals.RecordID(p2) {
		t.Errorf("handler got args %v, want the rescheduled ones", c.all()[0].Args)
	}
}

func TestEnqueue_SameKeyDifferentNames_AreSeparateJobs(t *testing.T) {
	r, s, _ := remindRig(t)
	r.register(jobs.Decl{Name: "volunteers.other", Handler: nop})
	a, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind", Key: "k"})
	wantNoErr(t, err)
	b, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.other", Key: "k"})
	wantNoErr(t, err)
	if a == b || r.count("") != 2 {
		t.Fatalf("ids %v %v and %d rows; want two jobs", a, b, r.count(""))
	}
}

func TestEnqueue_KeyFinal_IsNoopReturnsExistingID(t *testing.T) {
	for _, status := range []string{"running", "done", "failed", "canceled"} {
		t.Run(status, func(t *testing.T) {
			r, s, _ := remindRig(t)
			orig := epoch.Add(-time.Hour)
			var id jobs.JobID
			if status == "running" {
				id = r.forceRunning("volunteers.remind", "k", epoch.Add(time.Hour))
			} else {
				id = r.forceFinal("volunteers.remind", "k", status, orig)
			}
			before := r.byID(id)
			got, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind", Key: "k",
				RunAt: epoch.Add(9 * time.Hour), Args: jobs.Args{"person": signals.RecordID(newPerson())}})
			wantNoErr(t, err)
			if got != id {
				t.Errorf("returned %v, want the existing %v", got, id)
			}
			if n := r.count(""); n != 1 {
				t.Errorf("%d rows, want 1", n)
			}
			after := r.byID(id)
			if after.Status != before.Status || !after.RunAt.Equal(before.RunAt) || after.Args != before.Args ||
				after.Attempts != before.Attempts {
				t.Errorf("row changed: before %+v after %+v", before, after)
			}
		})
	}
}

func TestEnqueue_KeyDoneAfterRealRun_IsNoop(t *testing.T) {
	r, s, c := remindRig(t)
	id, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind", Key: "once"})
	wantNoErr(t, err)
	r.runOnce()
	if c.n() != 1 || r.byID(id).Status != "done" {
		t.Fatalf("job did not run to done: %+v", r.byID(id))
	}
	again, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind", Key: "once"})
	wantNoErr(t, err)
	if again != id {
		t.Fatalf("returned %v, want %v", again, id)
	}
	r.runOnce()
	if c.n() != 1 {
		t.Errorf("a finished key ran again (%d runs)", c.n())
	}
}

func TestEnqueue_ConcurrentSameKey_OneRowOneID(t *testing.T) {
	r, s, _ := remindRig(t)
	const n = 8
	ids := make(chan jobs.JobID, n)
	errsCh := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := s.Enqueue(context.Background(), jobs.Request{Name: "volunteers.remind", Key: "same"})
			ids <- id
			errsCh <- err
		}()
	}
	wg.Wait()
	close(ids)
	close(errsCh)
	for err := range errsCh {
		wantNoErr(t, err)
	}
	var first jobs.JobID
	for id := range ids {
		if first.IsZero() {
			first = id
		}
		if id != first {
			t.Fatalf("callers got different IDs: %v and %v", first, id)
		}
	}
	if got := r.count(""); got != 1 {
		t.Fatalf("%d rows, want 1", got)
	}
}

func TestEnqueue_Args_NeverReachLogsSpansOrMetrics(t *testing.T) {
	r, s, c := remindRig(t)
	person := newPerson()
	_, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind", Key: "leak",
		Args: jobs.Args{"person": signals.RecordID(person)}})
	wantNoErr(t, err)
	r.runOnce()
	if c.n() != 1 {
		t.Fatal("handler did not run")
	}
	if r.cap.Contains(person.String()) {
		t.Error("an argument's record ID reached a log line, span or metric")
	}
}
