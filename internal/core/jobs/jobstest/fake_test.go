package jobstest_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/jobs/jobstest"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

var _ jobs.Registrar = (*jobstest.Fake)(nil)

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func wantKind(t testing.TB, err error, kind errs.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("got nil error, want kind %v", kind)
	}
	if !errs.Is(err, kind) {
		t.Fatalf("kind = %v (%v), want %v", errs.KindOf(err), err, kind)
	}
}

func wantField(t testing.TB, err error, field, code string) {
	t.Helper()
	wantKind(t, err, errs.Invalid)
	for _, fe := range validate.Fields(err) {
		if (field == "" || fe.Field == field) && (code == "" || fe.Code == code) {
			return
		}
	}
	t.Fatalf("no field error %q/%q in %v", field, code, err)
}

func nop(context.Context, jobs.Job) error { return nil }

func TestFake_Register_ValidatesLikeTheRealOne(t *testing.T) {
	f := jobstest.NewFake("volunteers")
	wantField(t, f.Register(jobs.Decl{Name: "volunteers.a", Every: time.Second, Handler: nop}), "every", "range")
	wantField(t, f.Register(jobs.Decl{Name: "volunteers.a", Timeout: 2 * time.Hour, Handler: nop}), "timeout", "range")
	wantField(t, f.Register(jobs.Decl{Name: "volunteers.a"}), "handler", "")
	wantField(t, f.Register(jobs.Decl{Name: "bad", Handler: nop}), "name", "")
	wantField(t, f.Register(jobs.Decl{Name: "people.a", Handler: nop}), "name", "scope")
	if got := f.Registered(); len(got) != 0 {
		t.Fatalf("failed registrations were recorded: %v", got)
	}
	d := jobs.Decl{Name: "volunteers.remind", Handler: nop}
	if err := f.Register(d); err != nil {
		t.Fatalf("a valid Register failed after invalid ones: %v", err)
	}
}

func TestFake_Register_RecordsInOrderAndRefusesTwice(t *testing.T) {
	f := jobstest.NewFake("volunteers")
	a := jobs.Decl{Name: "volunteers.a", Handler: nop}
	b := jobs.Decl{Name: "volunteers.b", Every: time.Hour, Timeout: time.Minute, Handler: nop}
	if err := f.Register(a); err != nil {
		t.Fatal(err)
	}
	if err := f.Register(b); err != nil {
		t.Fatal(err)
	}
	wantKind(t, f.Register(a), errs.Conflict)
	got := f.Registered()
	if len(got) != 2 || got[0].Name != "volunteers.a" || got[1].Name != "volunteers.b" ||
		got[1].Every != time.Hour || got[1].Timeout != time.Minute {
		t.Fatalf("Registered() = %+v", got)
	}
	got[0].Name = "mutated.x" // a copy: mutating it changes nothing
	if f.Registered()[0].Name != "volunteers.a" {
		t.Error("Registered returned the Fake's own slice")
	}
}

func TestFake_Enqueue_ValidatesLikeTheRealOne(t *testing.T) {
	f := jobstest.NewFake("volunteers")
	if err := f.Register(jobs.Decl{Name: "volunteers.remind", Handler: nop}); err != nil {
		t.Fatal(err)
	}
	ctx := ctxT(t)
	_, err := f.Enqueue(ctx, jobs.Request{Name: "volunteers.never"})
	wantField(t, err, "name", "unknown")
	_, err = f.Enqueue(ctx, jobs.Request{Name: "people.remind"})
	wantField(t, err, "name", "scope")
	_, err = f.Enqueue(ctx, jobs.Request{Name: "volunteers.remind", Key: "bad key"})
	wantField(t, err, "key", "malformed")
	_, err = f.Enqueue(ctx, jobs.Request{Name: "volunteers.remind", RunAt: time.Now().Add(jobs.MaxDelay + 48*time.Hour)})
	wantField(t, err, "run_at", "range")
	_, err = f.Enqueue(ctx, jobs.Request{Name: "bad"})
	wantField(t, err, "name", "")
	if got := f.Enqueued(); len(got) != 0 {
		t.Fatalf("refused requests were recorded: %v", got)
	}
}

func TestFake_Enqueue_RecordsAndReturnsAnID(t *testing.T) {
	f := jobstest.NewFake("volunteers")
	if err := f.Register(jobs.Decl{Name: "volunteers.remind", Handler: nop}); err != nil {
		t.Fatal(err)
	}
	args := jobs.Args{"shift": signals.RecordID(model.NewID[model.PersonID]())}
	at := time.Now().Add(time.Hour)
	id, err := f.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind", Key: "k1", Args: args, RunAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if id.IsZero() {
		t.Error("Enqueue returned the zero JobID")
	}
	got := f.Enqueued()
	if len(got) != 1 || got[0].Name != "volunteers.remind" || got[0].Key != "k1" ||
		!got[0].RunAt.Equal(at) || got[0].Args["shift"] != args["shift"] {
		t.Fatalf("Enqueued() = %+v", got)
	}
}

func TestFake_Enqueue_InsideTransaction_AppearsOnlyAfterCommit(t *testing.T) {
	f := jobstest.NewFake("volunteers")
	if err := f.Register(jobs.Decl{Name: "volunteers.remind", Handler: nop}); err != nil {
		t.Fatal(err)
	}
	d := dbtest.NewFake()

	var during int
	err := d.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if _, err := f.Enqueue(ctx, jobs.Request{Name: "volunteers.remind", Key: "committed"}); err != nil {
			return fmt.Errorf("enqueue: %w", err)
		}
		during = len(f.Enqueued())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if during != 0 {
		t.Errorf("request visible before commit (%d recorded)", during)
	}
	if got := f.Enqueued(); len(got) != 1 || got[0].Key != "committed" {
		t.Fatalf("after commit Enqueued() = %+v", got)
	}

	boom := errors.New("rolled back")
	err = d.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if _, e := f.Enqueue(ctx, jobs.Request{Name: "volunteers.remind", Key: "rolled-back"}); e != nil {
			return fmt.Errorf("enqueue: %w", e)
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Transact err = %v", err)
	}
	if got := f.Enqueued(); len(got) != 1 {
		t.Fatalf("a rolled-back request was recorded: %+v", got)
	}
}

func TestFake_Fail_MakesTheNextEnqueueFail(t *testing.T) {
	f := jobstest.NewFake("volunteers")
	if err := f.Register(jobs.Decl{Name: "volunteers.remind", Handler: nop}); err != nil {
		t.Fatal(err)
	}
	f.Fail(errs.E("test", errs.Unavailable, errors.New("down")))
	_, err := f.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind"})
	wantKind(t, err, errs.Unavailable)
	if got := f.Enqueued(); len(got) != 0 {
		t.Errorf("a failed Enqueue was recorded: %+v", got)
	}
	f.Fail(nil)
	if _, err := f.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind"}); err != nil {
		t.Fatalf("after Fail(nil): %v", err)
	}
}

func TestFakeRun_Unregistered_ReturnsNotFound(t *testing.T) {
	f := jobstest.NewFake("volunteers")
	wantKind(t, f.Run(ctxT(t), "volunteers.never", jobs.Job{}), errs.NotFound)
}

func TestFakeRun_RunsHandlerAsTheSystem(t *testing.T) {
	f := jobstest.NewFake("volunteers")
	var gotJob jobs.Job
	var inJob, hasTx bool
	boom := errs.E("test", errs.Conflict, errors.New("boom"))
	err := f.Register(jobs.Decl{Name: "volunteers.remind", Handler: func(ctx context.Context, j jobs.Job) error {
		gotJob, inJob = j, jobs.InJob(ctx)
		_, hasTx = db.TxMode(ctx)
		return boom
	}})
	if err != nil {
		t.Fatal(err)
	}
	if jobs.InJob(ctxT(t)) {
		t.Fatal("InJob is true outside a handler")
	}
	want := jobs.Job{ID: model.NewID[jobs.JobID](), Name: "volunteers.remind", Key: "k", Attempt: 3}
	got := f.Run(ctxT(t), "volunteers.remind", want)
	if got != boom { //nolint:errorlint // the handler's error comes back unchanged
		t.Errorf("Run returned %v, want the handler's error unchanged", got)
	}
	if !inJob {
		t.Error("InJob(ctx) is false inside the handler")
	}
	if hasTx {
		t.Error("the handler's ctx carries a transaction")
	}
	if gotJob.ID != want.ID || gotJob.Key != "k" || gotJob.Attempt != 3 {
		t.Errorf("handler got %+v, want %+v", gotJob, want)
	}
}

func TestFake_ConcurrentUse_IsRaceFree(t *testing.T) {
	f := jobstest.NewFake("volunteers")
	if err := f.Register(jobs.Decl{Name: "volunteers.remind", Handler: nop}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.Enqueue(context.Background(), jobs.Request{Name: "volunteers.remind"}); err != nil {
				t.Error(err)
			}
			_ = f.Enqueued()
			_ = f.Registered()
			if err := f.Run(context.Background(), "volunteers.remind", jobs.Job{}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := len(f.Enqueued()); n != 20 {
		t.Errorf("Enqueued() has %d requests, want 20", n)
	}
}
