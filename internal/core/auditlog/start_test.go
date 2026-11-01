package auditlog_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/auditlog"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestStart_RegistersAuditPurge(t *testing.T) {
	f := newFx(t)
	wantNoErr(t, f.svc.Start(ctxT(t)))
	got := f.jobs.Registered()
	if len(got) != 1 {
		t.Fatalf("registered %d jobs, want exactly audit.purge", len(got))
	}
	d := got[0]
	if d.Name != jobs.Name("audit.purge") {
		t.Errorf("job name = %q, want audit.purge", d.Name)
	}
	if d.Every != time.Hour {
		t.Errorf("job runs every %v, want 1h (the period app used)", d.Every)
	}
	if d.Handler == nil {
		t.Error("no handler")
	}
}

// B7.
func TestStart_Twice_ReturnsConflict(t *testing.T) {
	f := newFx(t)
	wantNoErr(t, f.svc.Start(ctxT(t)))
	wantKind(t, f.svc.Start(ctxT(t)), errs.Conflict)
}

func TestStart_JobAlreadyRegistered_ReturnsConflict(t *testing.T) {
	f := newFx(t)
	wantNoErr(t, f.jobs.Register(jobs.Decl{Name: "audit.purge", Handler: func(_ context.Context, _ jobs.Job) error { return nil }}))
	wantKind(t, f.svc.Start(ctxT(t)), errs.Conflict)
}

func TestStart_ReadOnlyCopy_Succeeds_WritesNothing(t *testing.T) {
	f := newFx(t)
	f.fake.SetReadOnly(true)
	wantNoErr(t, f.svc.Start(ctxT(t)))
	if n := len(f.fake.Calls()); n != 0 {
		t.Errorf("Start ran %d statements; it never writes", n)
	}
}

func TestStart_RegistrarFails_ReturnsItsKind(t *testing.T) {
	f := newFx(t, func(o *auditlog.Options) {
		o.Jobs = failingRegistrar{err: errs.E("test", errs.Unavailable, errors.New("registry down"))}
	})
	wantKind(t, f.svc.Start(ctxT(t)), errs.Unavailable)
}

// failingRegistrar refuses every Register with err.
type failingRegistrar struct {
	jobs.Registrar
	err error
}

func (r failingRegistrar) Register(jobs.Decl) error { return r.err }
