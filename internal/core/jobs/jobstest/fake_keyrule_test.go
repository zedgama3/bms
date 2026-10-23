package jobstest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/jobs/jobstest"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

const remind jobs.Name = "volunteers.remind"

var epoch = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

// wrap keeps a handler's error (and its kind) while naming the test.
func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("test handler: %w", err)
}

func person() signals.RecordID { return signals.RecordID(model.NewID[model.PersonID]()) }

func newFake(t testing.TB, h jobs.Handler) (*jobstest.Fake, *clock.Fake) {
	t.Helper()
	clk := clock.NewFake(epoch)
	f := jobstest.NewFake("volunteers", jobstest.WithClock(clk))
	if h == nil {
		h = func(context.Context, jobs.Job) error { return nil }
	}
	if err := f.Register(jobs.Decl{Name: remind, Handler: h}); err != nil {
		t.Fatal(err)
	}
	return f, clk
}

func enq(t testing.TB, f *jobstest.Fake, r jobs.Request) jobs.JobID {
	t.Helper()
	id, err := f.Enqueue(ctxT(t), r)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	return id
}

func jobFor(r jobs.Request) jobs.Job {
	return jobs.Job{Name: r.Name, Key: r.Key, Args: r.Args, Attempt: 1, RunAt: r.RunAt}
}

type keyRuleCase struct {
	Name      string `json:"name"`
	Existing  string `json:"existing"`
	Request   string `json:"request"`
	Handler   string `json:"handler"`
	Status    string `json:"status"`
	RunAt     string `json:"runAt"`
	Attempts  int    `json:"attempts"`
	FakeSkips bool   `json:"fakeSkips"`
}

// TestFake_KeyRule_Table runs the table the PostgreSQL suite also runs (jobs.md J1).
func TestFake_KeyRule_Table(t *testing.T) {
	b, err := os.ReadFile("../internal/rules/testdata/keyrule.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []keyRuleCase
	if err := json.Unmarshal(b, &cases); err != nil || len(cases) == 0 {
		t.Fatalf("table: %v (%d cases)", err, len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			if tc.FakeSkips {
				t.Skip("a state the Fake does not model")
			}
			var f *jobstest.Fake
			var result error
			switch tc.Handler {
			case "fail":
				result = errs.E("test", errs.Invalid, errors.New("bad"))
			}
			request := epoch.Add(5 * time.Hour)
			p2 := person()
			req := jobs.Request{Name: remind, Key: "k", RunAt: request, Args: jobs.Args{"person": p2}}
			orig := jobs.Request{Name: remind, Key: "k", RunAt: epoch, Args: jobs.Args{"person": person()}}
			var id jobs.JobID
			f, _ = newFake(t, func(ctx context.Context, _ jobs.Job) error {
				if tc.Request == "during" {
					if got, err := f.Enqueue(ctx, req); err != nil || got != id {
						t.Errorf("Enqueue in handler = %v, %v; want the row's %v", got, err, id)
					}
					if q := f.Queued(); len(q) != 0 {
						t.Errorf("Queued during the run = %+v, want none (the row is running)", q)
					}
				}
				return result
			})
			id = enq(t, f, orig)
			switch tc.Existing {
			case "done", "failed":
				var herr error
				if tc.Existing == "failed" {
					herr = errs.E("test", errs.Invalid, errors.New("bad"))
				}
				// drive the real fake through a run whose handler outcome makes the row final
				result0 := result
				result = herr
				prev := tc.Request
				tc.Request = "none"
				if err := f.Run(ctxT(t), remind, jobFor(orig)); !reflect.DeepEqual(err, herr) {
					t.Fatalf("setup run = %v", err)
				}
				tc.Request, result = prev, result0
				if len(f.Queued()) != 0 {
					t.Fatalf("row not final: %+v", f.Queued())
				}
				if got := enq(t, f, req); got != id {
					t.Errorf("reopen returned %v, want %v", got, id)
				}
			case "queued":
				if got := enq(t, f, req); got != id {
					t.Errorf("move returned %v, want %v", got, id)
				}
			case "running":
				if tc.Request == "none" {
					req = jobs.Request{}
				}
				err := f.Run(ctxT(t), remind, jobFor(orig))
				if (tc.Handler == "fail") != (err != nil) {
					t.Fatalf("Run err = %v for handler %q", err, tc.Handler)
				}
			}
			q := f.Queued()
			if tc.Status != "queued" {
				if len(q) != 0 {
					t.Fatalf("Queued = %+v, want none (row %s)", q, tc.Status)
				}
				return
			}
			if len(q) != 1 {
				t.Fatalf("Queued = %+v, want one row", q)
			}
			if !q[0].RunAt.Equal(request) || !reflect.DeepEqual(q[0].Args, req.Args) || q[0].Key != "k" {
				t.Errorf("Queued row = %+v, want the request %+v", q[0], req)
			}
		})
	}
}

func TestFake_Enqueue_KeyRunning_QueuedAfterRun(t *testing.T) {
	var f *jobstest.Fake
	at := epoch.Add(48 * time.Hour)
	f, _ = newFake(t, func(ctx context.Context, _ jobs.Job) error {
		_, err := f.Enqueue(ctx, jobs.Request{Name: remind, Key: "k", RunAt: at})
		return wrap(err)
	})
	enq(t, f, jobs.Request{Name: remind, Key: "k", RunAt: epoch})
	if err := f.Run(ctxT(t), remind, jobs.Job{Name: remind, Key: "k"}); err != nil {
		t.Fatal(err)
	}
	q := f.Queued()
	if len(q) != 1 || !q[0].RunAt.Equal(at) {
		t.Fatalf("Queued = %+v, want the held request at %v", q, at)
	}
	if got := len(f.Enqueued()); got != 2 {
		t.Errorf("Enqueued heard %d requests, want 2 (the log keeps every request)", got)
	}
}

func TestFake_Enqueue_KeyFinal_Reopens(t *testing.T) {
	f, _ := newFake(t, nil)
	id := enq(t, f, jobs.Request{Name: remind, Key: "k", RunAt: epoch})
	if err := f.Run(ctxT(t), remind, jobs.Job{Name: remind, Key: "k"}); err != nil {
		t.Fatal(err)
	}
	if len(f.Queued()) != 0 {
		t.Fatalf("a finished row is still queued: %+v", f.Queued())
	}
	at := epoch.Add(time.Hour)
	if got := enq(t, f, jobs.Request{Name: remind, Key: "k", RunAt: at}); got != id {
		t.Errorf("reopen returned %v, want the same row %v", got, id)
	}
	if q := f.Queued(); len(q) != 1 || !q[0].RunAt.Equal(at) {
		t.Fatalf("Queued = %+v, want the reopened row", q)
	}
}

func TestFakeRun_HandlerReenqueuesOwnKey_QueuedShowsIt(t *testing.T) {
	var f *jobstest.Fake
	next := epoch.Add(30 * 24 * time.Hour)
	var during []jobs.Request
	f, _ = newFake(t, func(ctx context.Context, j jobs.Job) error {
		if _, err := f.Enqueue(ctx, jobs.Request{Name: remind, Key: j.Key, RunAt: next}); err != nil {
			return wrap(err)
		}
		during = f.Queued()
		return nil
	})
	enq(t, f, jobs.Request{Name: remind, Key: "k", RunAt: epoch})
	if err := f.Run(ctxT(t), remind, jobs.Job{Name: remind, Key: "k"}); err != nil {
		t.Fatal(err)
	}
	if len(during) != 0 {
		t.Errorf("Queued during the run = %+v, want none", during)
	}
	if q := f.Queued(); len(q) != 1 || !q[0].RunAt.Equal(next) {
		t.Fatalf("Queued after the run = %+v, want the carry-forward at %v", q, next)
	}
}

func TestFakeQueued_SortedByRunAtThenKey_AndACopy(t *testing.T) {
	f, _ := newFake(t, nil)
	enq(t, f, jobs.Request{Name: remind, Key: "b", RunAt: epoch.Add(2 * time.Hour)})
	enq(t, f, jobs.Request{Name: remind, Key: "z", RunAt: epoch.Add(time.Hour)})
	enq(t, f, jobs.Request{Name: remind, Key: "a", RunAt: epoch.Add(2 * time.Hour)})
	var keys []string
	for _, r := range f.Queued() {
		keys = append(keys, r.Key)
	}
	if got := strings.Join(keys, ","); got != "z,a,b" {
		t.Errorf("Queued order = %s, want z,a,b", got)
	}
	f.Queued()[0].Key = "mutated"
	if f.Queued()[0].Key != "z" {
		t.Error("Queued returned the Fake's own storage")
	}
	empty, _ := newFake(t, nil)
	if len(empty.Queued()) != 0 {
		t.Error("a fresh Fake has queued rows")
	}
}

func TestFakeRunDue_RunsDueOnceInOrder(t *testing.T) {
	var order []string
	f, _ := newFake(t, func(_ context.Context, j jobs.Job) error {
		order = append(order, j.Key)
		return nil
	})
	enq(t, f, jobs.Request{Name: remind, Key: "c", RunAt: epoch.Add(3 * time.Hour)})
	enq(t, f, jobs.Request{Name: remind, Key: "b", RunAt: epoch.Add(time.Hour)})
	enq(t, f, jobs.Request{Name: remind, Key: "a", RunAt: epoch.Add(time.Hour)})
	enq(t, f, jobs.Request{Name: remind, Key: "late", RunAt: epoch.Add(10 * time.Hour)})
	n, err := f.RunDue(ctxT(t), epoch.Add(4*time.Hour))
	if err != nil || n != 3 {
		t.Fatalf("RunDue = %d, %v; want 3, nil", n, err)
	}
	if got := strings.Join(order, ","); got != "a,b,c" {
		t.Errorf("order = %s, want a,b,c", got)
	}
	if q := f.Queued(); len(q) != 1 || q[0].Key != "late" {
		t.Errorf("Queued = %+v, want only the row not yet due", q)
	}
	if n, err := f.RunDue(ctxT(t), epoch.Add(4*time.Hour)); err != nil || n != 0 {
		t.Errorf("second RunDue = %d, %v; want 0, nil (each row once)", n, err)
	}
}

func TestFakeRunDue_BoundaryAndJob(t *testing.T) {
	var got jobs.Job
	f, _ := newFake(t, func(_ context.Context, j jobs.Job) error { got = j; return nil })
	if n, err := f.RunDue(ctxT(t), epoch); err != nil || n != 0 {
		t.Fatalf("RunDue on an empty Fake = %d, %v", n, err)
	}
	args := jobs.Args{"shift": person()}
	id := enq(t, f, jobs.Request{Name: remind, Key: "k", RunAt: epoch.Add(time.Hour), Args: args})
	if n, err := f.RunDue(ctxT(t), epoch.Add(time.Hour-time.Nanosecond)); err != nil || n != 0 {
		t.Error("a row ran before its time")
	}
	if n, err := f.RunDue(ctxT(t), epoch.Add(time.Hour)); err != nil || n != 1 {
		t.Error("a row due exactly now did not run")
	}
	if got.ID != id || got.Key != "k" || got.Attempt != 1 || !reflect.DeepEqual(got.Args, args) || !got.RunAt.Equal(epoch.Add(time.Hour)) {
		t.Errorf("handler got %+v, want the row's ID, Key, Args, RunAt and Attempt 1", got)
	}
}

func TestFakeRunDue_SelfRequeueNotRunTwice(t *testing.T) {
	var f *jobstest.Fake
	calls := 0
	f, _ = newFake(t, func(ctx context.Context, j jobs.Job) error {
		calls++
		_, err := f.Enqueue(ctx, jobs.Request{Name: remind, Key: j.Key, RunAt: epoch})
		return wrap(err)
	})
	enq(t, f, jobs.Request{Name: remind, Key: "k", RunAt: epoch})
	n, err := f.RunDue(ctxT(t), epoch.Add(time.Hour))
	if err != nil || n != 1 || calls != 1 {
		t.Fatalf("RunDue = %d, %v with %d calls; want one run", n, err, calls)
	}
	if len(f.Queued()) != 1 {
		t.Errorf("the self-requeued row is not waiting: %+v", f.Queued())
	}
	if n, err := f.RunDue(ctxT(t), epoch.Add(time.Hour)); err != nil || n != 1 || calls != 2 {
		t.Errorf("next RunDue ran %d, calls %d; want the requeued row once more", n, calls)
	}
}

func TestFakeRunDue_HandlerFails_ReturnsErrorAndRunsRest(t *testing.T) {
	ran := map[string]bool{}
	f, _ := newFake(t, func(_ context.Context, j jobs.Job) error {
		ran[j.Key] = true
		if j.Key == "a" {
			return errs.E("test", errs.Conflict, errors.New("boom"))
		}
		return nil
	})
	enq(t, f, jobs.Request{Name: remind, Key: "a", RunAt: epoch})
	enq(t, f, jobs.Request{Name: remind, Key: "b", RunAt: epoch.Add(time.Minute)})
	n, err := f.RunDue(ctxT(t), epoch.Add(time.Hour))
	if !errs.Is(err, errs.Conflict) {
		t.Fatalf("err = %v, want Conflict", err)
	}
	if n != 2 || !ran["b"] {
		t.Errorf("ran %d, b ran %v; want both rows run", n, ran["b"])
	}
	if len(f.Queued()) != 0 {
		t.Errorf("the failed row is still waiting: %+v", f.Queued())
	}
}

func TestFakeRunDue_CanceledContext_HandlersSeeIt(t *testing.T) {
	var seen error
	f, _ := newFake(t, func(ctx context.Context, _ jobs.Job) error { seen = ctx.Err(); return wrap(seen) })
	enq(t, f, jobs.Request{Name: remind, Key: "k", RunAt: epoch})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.RunDue(ctx, epoch.Add(time.Hour))
	if seen == nil && err == nil {
		t.Error("a canceled context reached no handler and failed nothing")
	}
}
