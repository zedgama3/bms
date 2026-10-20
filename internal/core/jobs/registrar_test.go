package jobs_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func readOnlyRig(t *testing.T) (*rig, *dbtest.Fake) {
	t.Helper()
	d := dbtest.NewFake()
	d.SetReadOnly(true)
	return newRig(t, d), d
}

// ---- Register ----------------------------------------------------------------------------

func TestRegister_Valid_Succeeds(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	s := r.scoped("volunteers")
	wantNoErr(t, s.Register(jobs.Decl{Name: "volunteers.remind", Handler: nop}))
	wantNoErr(t, s.Register(jobs.Decl{Name: "volunteers.sweep", Every: time.Hour, Timeout: time.Minute, Handler: nop}))
}

func TestRegister_OnReadOnlyCopy_Succeeds(t *testing.T) {
	r, _ := readOnlyRig(t)
	wantNoErr(t, r.scoped("volunteers").Register(jobs.Decl{Name: "volunteers.remind", Handler: nop}))
}

func TestRegister_InvalidDecl_ReturnsInvalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	s := r.scoped("volunteers")
	wantField(t, s.Register(jobs.Decl{Name: "volunteers.a", Every: time.Second, Handler: nop}), "every", "range")
	wantField(t, s.Register(jobs.Decl{Name: "volunteers.b", Timeout: time.Hour * 2, Handler: nop}), "timeout", "range")
	wantField(t, s.Register(jobs.Decl{Name: "volunteers.c"}), "handler", "")
	wantField(t, s.Register(jobs.Decl{Name: "volunteers", Handler: nop}), "name", "")
	// A failed Register leaves nothing behind: the name is still free.
	wantNoErr(t, s.Register(jobs.Decl{Name: "volunteers.a", Handler: nop}))
}

func TestRegister_OtherOwner_ReturnsInvalidScope(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	s := r.scoped("volunteers")
	for _, n := range []jobs.Name{"people.remind", "jobs.purge", "signals.redeliver", "volunteersx.a"} {
		wantField(t, s.Register(jobs.Decl{Name: n, Handler: nop}), "name", "scope")
	}
}

func TestRegister_Twice_ReturnsConflict(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	s := r.scoped("volunteers")
	d := jobs.Decl{Name: "volunteers.remind", Handler: nop}
	wantNoErr(t, s.Register(d))
	wantKind(t, s.Register(d), errs.Conflict)
	// A second scope for the same owner shares the process-wide name table.
	wantKind(t, r.scoped("volunteers").Register(d), errs.Conflict)
}

func TestRegister_RunnersOwnNames_AreTaken(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	wantKind(t, r.scoped("jobs").Register(jobs.Decl{Name: jobs.NamePurge, Every: time.Hour, Handler: nop}), errs.Conflict)
	wantKind(t, r.scoped("signals").Register(jobs.Decl{Name: jobs.NameRedeliver, Handler: nop}), errs.Conflict)
}

func TestRegister_AfterRun_ReturnsInternal(t *testing.T) {
	r, _ := readOnlyRig(t)
	ctx, cancel := context.WithCancel(ctxT(t))
	done := r.startRun(ctx) // returns once Register is frozen
	wantKind(t, r.scoped("volunteers").Register(jobs.Decl{Name: "volunteers.late", Handler: nop}), errs.Internal)
	cancel()
	wantNoErr(t, <-done)
}

func TestRegister_Concurrent_ExactlyOneWins(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	d := jobs.Decl{Name: "volunteers.remind", Handler: nop}
	s := r.scoped("volunteers")
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- s.Register(d)
		}()
	}
	wg.Wait()
	close(results)
	ok := 0
	for err := range results {
		if err == nil {
			ok++
			continue
		}
		wantKind(t, err, errs.Conflict)
	}
	if ok != 1 {
		t.Fatalf("%d registrations succeeded, want exactly 1", ok)
	}
}

// ---- Scoped ------------------------------------------------------------------------------

func TestScoped_BadOwner_ReturnsInvalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	for _, owner := range []string{"", "Volunteers", "a.b", "has space", "-x", "é"} {
		_, err := r.run.Scoped(owner)
		wantKind(t, err, errs.Invalid)
	}
}

func TestScoped_ValidOwners_Succeed(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	for _, owner := range []string{"volunteers", "audit", "jobs", "afterreports", "a1"} {
		if _, err := r.run.Scoped(owner); err != nil {
			t.Errorf("Scoped(%q): %v", owner, err)
		}
	}
}

func TestScoped_AfterClose_ReturnsInternal(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	wantNoErr(t, r.run.Close(ctxT(t)))
	_, err := r.run.Scoped("volunteers")
	wantKind(t, err, errs.Internal)
}

// ---- Enqueue (no store needed) -----------------------------------------------------------

func TestEnqueue_OtherOwner_ReturnsInvalidScope(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	r.register(jobs.Decl{Name: "people.sweep", Handler: nop})
	s := r.scoped("volunteers")
	_, err := s.Enqueue(ctxT(t), jobs.Request{Name: "people.sweep"})
	wantField(t, err, "name", "scope")
}

func TestEnqueue_Unregistered_ReturnsInvalidUnknown(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	s := r.scoped("volunteers")
	_, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.never"})
	wantField(t, err, "name", "unknown")
}

func TestEnqueue_InvalidRequest_ReturnsInvalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: nop})
	_, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind", Key: "bad key"})
	wantField(t, err, "key", "malformed")
	_, err = s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind", RunAt: time.Now().Add(jobs.MaxDelay + 48*time.Hour)})
	wantField(t, err, "run_at", "range")
}

func TestEnqueue_ReadOnly_ReturnsErrReadOnly(t *testing.T) {
	r, _ := readOnlyRig(t)
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: nop})
	_, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind"})
	wantReadOnly(t, err)
}

func TestEnqueue_ReadOnly_InsideWriteTx_ReturnsErrReadOnly(t *testing.T) {
	d := dbtest.NewFake()
	r := newRig(t, d)
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: nop})
	d.SetReadOnly(true)
	err := d.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		_, e := s.Enqueue(ctx, jobs.Request{Name: "volunteers.remind"})
		return fmt.Errorf("enqueue: %w", e)
	})
	wantReadOnly(t, err)
}
