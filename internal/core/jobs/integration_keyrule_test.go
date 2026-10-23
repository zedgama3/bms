//go:build integration

package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"runtime"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
)

const remind jobs.Name = "volunteers.remind"

// nextOf reads the held request of a row.
func (r *irig) nextOf(id jobs.JobID) (at *time.Time, args *string) {
	r.t.Helper()
	if err := r.pool.QueryRow(ctxT(r.t), `SELECT next_run_at, next_args::text FROM core.job WHERE id = $1`,
		[16]byte(id)).Scan(&at, &args); err != nil {
		r.t.Fatalf("read held request: %v", err)
	}
	return at, args
}

func (r *irig) wantNoHeld(id jobs.JobID) {
	r.t.Helper()
	if at, args := r.nextOf(id); at != nil || args != nil {
		r.t.Errorf("row still holds a request: %v %v", at, args)
	}
}

func personArgs(p signals.RecordID) jobs.Args { return jobs.Args{"person": p} }

// runHolding leases a due keyed row whose handler blocks, calls during while it runs and
// returns the row ID and the channel that gets RunOnce's result once result is released.
type heldRun struct {
	r       *irig
	b       *blocker
	release func()
	res     <-chan onceResult
}

// startHeld registers volunteers.remind with a handler that blocks and then returns *result,
// enqueues key "k" due now, and waits until the handler runs.
func startHeld(t *testing.T, r *irig, result *error) (jobs.Registrar, jobs.JobID, *heldRun) {
	t.Helper()
	b := newBlocker()
	s := r.register(jobs.Decl{Name: remind, Handler: func(_ context.Context, j jobs.Job) error {
		b.started <- j
		<-b.release
		return *result
	}})
	id := enqueue(t, s, jobs.Request{Name: remind, Key: "k", RunAt: epoch, Args: personArgs(signals.RecordID(newPerson()))})
	h := &heldRun{r: r, b: b, release: b.open}
	h.res = r.asyncOnce(ctxT(t))
	b.waitStarted(t)
	if got := r.byID(id).Status; got != "running" {
		t.Fatalf("status = %s, want running", got)
	}
	return s, id, h
}

func TestKeyRule_Table(t *testing.T) {
	for _, tc := range loadKeyRuleCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			r := newIRig(t)
			orig := epoch
			request := epoch.Add(5 * time.Hour)
			p2 := signals.RecordID(newPerson())
			req := jobs.Request{Name: remind, Key: "k", RunAt: request, Args: personArgs(p2)}
			var result error
			switch tc.Handler {
			case "fail":
				result = errs.E("test", errs.Invalid, errors.New("bad"))
			case "retry":
				result = errs.E("test", errs.Unavailable, errors.New("down"))
			}
			var s jobs.Registrar
			var id jobs.JobID
			if tc.Existing == "running" {
				var h *heldRun
				s, id, h = startHeld(t, r, &result)
				if tc.Request == "during" {
					got := enqueue(t, s, req)
					if got != id {
						t.Errorf("Enqueue while running returned %v, want the row's %v", got, id)
					}
					at, args := r.nextOf(id)
					if at == nil || !at.Equal(request) || args == nil {
						t.Errorf("held request = %v %v, want %v", at, args, request)
					}
					if st := r.byID(id).Status; st != "running" {
						t.Errorf("status while held = %s, want running", st)
					}
				}
				h.release()
				if out := r.await(h.res); out.err != nil {
					t.Fatalf("RunOnce: %v", out.err)
				}
			} else {
				s = r.register(jobs.Decl{Name: remind, Handler: nop})
				switch tc.Existing {
				case "queued":
					id = enqueue(t, s, jobs.Request{Name: remind, Key: "k", RunAt: orig})
				default:
					id = r.forceFinal(remind, "k", tc.Existing, epoch.Add(-time.Hour))
				}
				got := enqueue(t, s, req)
				if got != id {
					t.Errorf("Enqueue returned %v, want the existing row %v", got, id)
				}
			}
			if n := r.count("name = $1", string(remind)); n != 1 {
				t.Fatalf("%d rows, want 1", n)
			}
			x := r.byID(id)
			if x.Status != tc.Status || x.Attempts != tc.Attempts {
				t.Fatalf("row = status %s attempts %d, want %s %d", x.Status, x.Attempts, tc.Status, tc.Attempts)
			}
			switch tc.RunAt {
			case "request":
				if !x.RunAt.Equal(request) {
					t.Errorf("run_at = %v, want the request's %v", x.RunAt, request)
				}
				if got := argsJSON(t, x.Args); got["person"] != p2.String() {
					t.Errorf("args = %v, want the request's", got)
				}
			case "original":
				if !x.RunAt.Equal(orig) {
					t.Errorf("run_at = %v, want unchanged %v", x.RunAt, orig)
				}
			case "later":
				if x.RunAt.Before(orig) || !x.RunAt.Before(request) {
					t.Errorf("run_at = %v, want a backoff after %v", x.RunAt, orig)
				}
			}
			r.wantNoHeld(id)
			if tc.Status == "queued" && (x.Leased != nil || x.Finished != nil) {
				t.Errorf("a queued row keeps lease or finish time: %+v", x)
			}
			if tc.Status != "queued" && x.Finished == nil {
				t.Errorf("a final row has no finished_at: %+v", x)
			}
		})
	}
}

func TestEnqueue_KeyFinal_ReopensRow(t *testing.T) {
	for _, status := range []string{"done", "failed"} {
		t.Run(status, func(t *testing.T) {
			r, s, c := remindRig(t)
			id := r.forceFinal(remind, "k", status, epoch.Add(-time.Hour))
			p := newPerson()
			got := enqueue(t, s, jobs.Request{Name: remind, Key: "k", RunAt: epoch.Add(2 * time.Hour),
				Args: personArgs(signals.RecordID(p))})
			if got != id {
				t.Errorf("returned %v, want the existing %v", got, id)
			}
			x := r.byID(id)
			if x.Status != "queued" || x.Attempts != 0 || x.Finished != nil || !x.RunAt.Equal(epoch.Add(2*time.Hour)) {
				t.Fatalf("row = %+v, want a fresh queued row", x)
			}
			r.advance(3 * time.Hour)
			r.runOnce()
			if c.n() != 1 || c.all()[0].Attempt != 1 {
				t.Errorf("handler runs %+v, want one run at Attempt 1", c.all())
			}
		})
	}
}

func TestEnqueue_KeyCanceled_ReopensRow(t *testing.T) {
	r, s, c := remindRig(t)
	id := r.forceFinal(remind, "k", "canceled", epoch.Add(-time.Hour))
	got := enqueue(t, s, jobs.Request{Name: remind, Key: "k"})
	if got != id {
		t.Errorf("returned %v, want %v", got, id)
	}
	if x := r.byID(id); x.Status != "queued" || x.Attempts != 0 || x.Finished != nil {
		t.Fatalf("row = %+v", x)
	}
	r.runOnce()
	if c.n() != 1 {
		t.Errorf("reopened job ran %d times, want 1", c.n())
	}
}

func TestEnqueue_KeyDoneAfterRealRun_Reopens(t *testing.T) {
	r, s, c := remindRig(t)
	id := enqueue(t, s, jobs.Request{Name: remind, Key: "once"})
	r.runOnce()
	if c.n() != 1 || r.byID(id).Status != "done" {
		t.Fatalf("job did not run to done: %+v", r.byID(id))
	}
	again := enqueue(t, s, jobs.Request{Name: remind, Key: "once"})
	if again != id {
		t.Fatalf("returned %v, want %v", again, id)
	}
	r.runOnce()
	if c.n() != 2 {
		t.Errorf("a repeated key ran %d times in all, want 2", c.n())
	}
}

func TestEnqueue_KeyRunning_HeldAndAppliedAfterOK(t *testing.T) {
	r := newIRig(t)
	var result error
	s, id, h := startHeld(t, r, &result)
	later := epoch.Add(72 * time.Hour)
	p := signals.RecordID(newPerson())
	if got := enqueue(t, s, jobs.Request{Name: remind, Key: "k", RunAt: later, Args: personArgs(p)}); got != id {
		t.Fatalf("returned %v, want %v", got, id)
	}
	h.release()
	wantNoErr(t, r.await(h.res).err)
	x := r.byID(id)
	if x.Status != "queued" || x.Attempts != 0 || !x.RunAt.Equal(later) || x.Finished != nil {
		t.Fatalf("row = %+v, want queued at the held time, attempts 0, not done", x)
	}
	if argsJSON(t, x.Args)["person"] != p.String() {
		t.Errorf("args = %s, want the held ones", x.Args)
	}
	r.wantNoHeld(id)
}

func TestRun_HandlerReenqueuesOwnKey_RowQueuedAgain(t *testing.T) {
	r := newIRig(t)
	var s jobs.Registrar
	next := epoch.Add(30 * 24 * time.Hour)
	calls := 0
	s = r.register(jobs.Decl{Name: remind, Handler: func(ctx context.Context, j jobs.Job) error {
		calls++
		if calls == 1 {
			_, err := s.Enqueue(ctx, jobs.Request{Name: remind, Key: j.Key, RunAt: next})
			if err != nil {
				return fmt.Errorf("re-enqueue: %w", err)
			}
			return nil
		}
		return nil
	}})
	id := enqueue(t, s, jobs.Request{Name: remind, Key: "k", RunAt: epoch})
	r.runOnce()
	x := r.byID(id)
	if x.Status != "queued" || !x.RunAt.Equal(next) || x.Attempts != 0 {
		t.Fatalf("row = %+v, want queued again at %v", x, next)
	}
	sawNext := false
	for _, rec := range r.cap.Records() {
		if rec.Message == "jobs: a job ran" && rec.Attrs["next"] == "true" {
			sawNext = true
		}
	}
	if !sawNext {
		t.Error("no 'a job ran' log line carries next=true")
	}
	r.advance(31 * 24 * time.Hour)
	r.runOnce()
	if calls != 2 || r.byID(id).Status != "done" {
		t.Errorf("calls = %d status = %s, want a second run ending done", calls, r.byID(id).Status)
	}
}

func TestRun_HeldRequest_FailedRunQueuesRequest(t *testing.T) {
	for name, mk := range map[string]func() error{
		"invalid": func() error { return errs.E("test", errs.Invalid, errors.New("bad")) },
		"panic":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			r := newIRig(t)
			b := newBlocker()
			s := r.register(jobs.Decl{Name: remind, Handler: func(_ context.Context, j jobs.Job) error {
				b.started <- j
				<-b.release
				if mk == nil {
					panic("handler bug")
				}
				return mk()
			}})
			id := enqueue(t, s, jobs.Request{Name: remind, Key: "k", RunAt: epoch})
			res := r.asyncOnce(ctxT(t))
			b.waitStarted(t)
			at := epoch.Add(4 * time.Hour)
			enqueue(t, s, jobs.Request{Name: remind, Key: "k", RunAt: at})
			b.open()
			wantNoErr(t, r.await(res).err)
			x := r.byID(id)
			if x.Status != "queued" || x.Attempts != 0 || !x.RunAt.Equal(at) {
				t.Fatalf("row = %+v, want queued at the held time, attempts 0", x)
			}
			if x.LastError == nil || *x.LastError == "" {
				t.Errorf("last_error not recorded: %+v", x)
			}
			if r.outcomes("failed")+r.outcomes("panicked") != 1 {
				t.Errorf("the failure was not counted")
			}
		})
	}
}

func TestRun_HeldRequest_RetryableRunQueuesRequestWithoutBackoff(t *testing.T) {
	r := newIRig(t)
	result := errs.E("test", errs.Unavailable, errors.New("down"))
	s, id, h := startHeld(t, r, &result)
	at := epoch.Add(9 * time.Hour)
	enqueue(t, s, jobs.Request{Name: remind, Key: "k", RunAt: at})
	h.release()
	wantNoErr(t, r.await(h.res).err)
	x := r.byID(id)
	if x.Status != "queued" || x.Attempts != 0 || !x.RunAt.Equal(at) {
		t.Fatalf("row = %+v, want queued at exactly the held time with attempts 0", x)
	}
	if x.LastError == nil || *x.LastError != "unavailable" {
		t.Errorf("last_error = %v, want unavailable", x.LastError)
	}
	if r.outcomes("retried") != 1 {
		t.Errorf("retried outcomes = %v, want 1", r.outcomes("retried"))
	}
}

func TestRun_HeldRequest_ShutdownQueuesRequest(t *testing.T) {
	r := newIRig(t)
	b := newBlocker()
	s := r.register(jobs.Decl{Name: remind, Handler: b.waitCtx})
	id := enqueue(t, s, jobs.Request{Name: remind, Key: "k", RunAt: epoch})
	res := r.asyncOnce(ctxT(t))
	b.waitStarted(t)
	at := epoch.Add(6 * time.Hour)
	enqueue(t, s, jobs.Request{Name: remind, Key: "k", RunAt: at})
	wantNoErr(t, r.run.Close(ctxT(t)))
	r.await(res)
	x := r.byID(id)
	if x.Status != "queued" || x.Attempts != 0 || !x.RunAt.Equal(at) || x.Leased != nil {
		t.Fatalf("row = %+v, want the held request queued", x)
	}
	r.wantNoHeld(id)
}

func TestReclaim_HeldRequest_QueuesRequestInsteadOfGaveUp(t *testing.T) {
	for name, attempts := range map[string]int{"gave_up": jobs.DefaultRetry.MaxAttempts, "lost_lease": 1} {
		t.Run(name, func(t *testing.T) {
			r := newIRig(t)
			var c counter
			r.register(jobs.Decl{Name: remind, Handler: c.handler(nil)})
			id := r.forceRunningAttempts(remind, attempts, epoch.Add(-time.Minute))
			r.exec(`UPDATE core.job SET key = 'k' WHERE id = $1`, [16]byte(id))
			at := epoch.Add(8 * time.Hour)
			r.exec(`UPDATE core.job SET next_run_at = $2, next_args = '{}'::jsonb WHERE id = $1`, [16]byte(id), at)
			r.runOnce()
			x := r.byID(id)
			if x.Status != "queued" || x.Attempts != 0 || !x.RunAt.Equal(at) || x.Finished != nil {
				t.Fatalf("row = %+v, want queued at the held time (not failed, not requeued as lost)", x)
			}
			r.wantNoHeld(id)
			if c.n() != 0 {
				t.Errorf("handler ran %d times before the held time", c.n())
			}
		})
	}
}

// wantNextCheck asserts err is the job_next_chk check violation (23514) classified Invalid
// by db.Wrap, with the driver's detail still reachable as a *db.Error.
func wantNextCheck(t *testing.T, step string, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: accepted, want job_next_chk refusal", step)
		return
	}
	if !errs.Is(err, errs.Invalid) {
		t.Errorf("%s: kind = %v, want Invalid", step, errs.KindOf(err))
	}
	var dbErr *db.Error
	if !errors.As(err, &dbErr) {
		t.Errorf("%s: no *db.Error in %v", step, err)
		return
	}
	if dbErr.Code != "23514" || dbErr.Constraint != "job_next_chk" {
		t.Errorf("%s: code %q constraint %q, want 23514 job_next_chk", step, dbErr.Code, dbErr.Constraint)
	}
}

func TestEnqueue_NextOnQueuedRow_ConstraintRefuses(t *testing.T) {
	r, s, _ := remindRig(t)
	queued := enqueue(t, s, jobs.Request{Name: remind, Key: "q"})
	_, err := r.pool.Exec(ctxT(t), `UPDATE core.job SET next_run_at = $2, next_args = '{}'::jsonb WHERE id = $1`,
		[16]byte(queued), epoch)
	wantNextCheck(t, "held request on a queued row", err)
	running := r.forceRunning(remind, "r", epoch.Add(time.Hour))
	_, err = r.pool.Exec(ctxT(t), `UPDATE core.job SET next_run_at = $2 WHERE id = $1`, [16]byte(running), epoch)
	wantNextCheck(t, "next_run_at without next_args", err)
	_, err = r.pool.Exec(ctxT(t), `UPDATE core.job SET next_run_at = $2, next_args = '{}'::jsonb WHERE id = $1`,
		[16]byte(running), epoch)
	if err != nil {
		t.Errorf("a held request on a running row was refused: %v", err)
	}
	if r.byID(queued).Status != "queued" {
		t.Error("queued row changed")
	}
}

// lockWaiters counts backends waiting on a row lock in a statement that touches core.job.
func (r *irig) lockWaiters() int {
	r.t.Helper()
	var n int
	if err := r.pool.QueryRow(ctxT(r.t), `SELECT count(*) FROM pg_stat_activity
		WHERE datname = current_database() AND wait_event_type = 'Lock' AND query ILIKE '%core.job%'`).Scan(&n); err != nil {
		r.t.Fatalf("read pg_stat_activity: %v", err)
	}
	return n
}

func TestEnqueue_KeyRunning_InOpenTx_OutcomeWaitsThenApplies(t *testing.T) {
	r := newIRig(t)
	var result error
	s, id, h := startHeld(t, r, &result)
	request := epoch.Add(5 * time.Hour)
	p2 := signals.RecordID(newPerson())
	enqueued, commit := make(chan struct{}), make(chan struct{})
	txDone := make(chan error, 1)
	go func() {
		txDone <- r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
			if _, err := s.Enqueue(ctx, jobs.Request{Name: remind, Key: "k", RunAt: request, Args: personArgs(p2)}); err != nil {
				return fmt.Errorf("enqueue: %w", err)
			}
			close(enqueued)
			<-commit
			return nil
		})
	}()
	select {
	case <-enqueued:
	case err := <-txDone:
		t.Fatalf("transaction ended before enqueueing: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("Enqueue in the open transaction did not return")
	}
	committed := false
	defer func() {
		if !committed {
			close(commit)
		}
	}()
	h.release() // the handler returns; its outcome write must now wait for the row lock
	deadline := time.Now().Add(15 * time.Second)
	for r.lockWaiters() == 0 {
		select {
		case res := <-h.res:
			t.Fatalf("RunOnce finished (%v) while the transaction held the row", res.err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the outcome write never waited on the row lock")
		}
		runtime.Gosched()
	}
	if got := r.byID(id).Status; got != "running" {
		t.Errorf("status while the outcome waits = %s, want running", got)
	}
	close(commit)
	committed = true
	wantNoErr(t, <-txDone)
	if out := r.await(h.res); out.err != nil {
		t.Fatalf("RunOnce: %v", out.err)
	}
	x := r.byID(id)
	if x.Status != "queued" || x.Attempts != 0 || !x.RunAt.Equal(request) || x.Leased != nil {
		t.Fatalf("row = %+v, want queued at the held run_at with attempts 0", x)
	}
	if got := argsJSON(t, x.Args)["person"]; got != p2.String() && fmt.Sprint(got) != fmt.Sprint(p2) {
		t.Errorf("args person = %v, want the held request's %v", got, p2)
	}
	r.wantNoHeld(id)
	logged := false
	for _, rec := range r.cap.Records() {
		if rec.Message == "jobs: a job ran" && rec.Attrs["next"] == "true" {
			logged = true
		}
	}
	if !logged {
		t.Error("no outcome line with next=true")
	}
}

func TestMigration0002_AppliesOnRowsOfEveryStatus(t *testing.T) {
	pool := dbtest.TempDB(t)
	only1 := fstest.MapFS{}
	b, err := fs.ReadFile(jobs.Set.Files, "0001_job.sql")
	wantNoErr(t, err)
	only1["0001_job.sql"] = &fstest.MapFile{Data: b}
	set := jobs.Set
	set.Files = only1
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	wantNoErr(t, err)
	_, err = mig.Migrate(ctxT(t), []db.Set{set})
	wantNoErr(t, err)
	r := &irig{rig: &rig{t: t}, pool: pool}
	r.forceQueued(remind, "q", epoch, "")
	r.forceRunning(remind, "r", epoch.Add(time.Hour))
	for _, st := range []string{"done", "failed", "canceled"} {
		r.forceFinal(remind, st, st, epoch)
	}
	migrate(t, pool)
	if n := r.count(""); n != 5 {
		t.Fatalf("%d rows after 0002, want 5", n)
	}
	for _, x := range r.rows("") {
		if at, args := r.nextOf(jobs.JobID(x.ID)); at != nil || args != nil {
			t.Errorf("row %s has a held request after migration", x.Status)
		}
	}
}
