package auditlog_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/auditlog"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// purgeFx is a started Service whose core.audit_purge answers 2 deleted.
func purgeFx(t *testing.T) (*fx, *flakyDB) {
	t.Helper()
	f := f0(t)
	f.db.answerCount("audit_purge", 2)
	svc, err := auditlog.New(f.opts)
	wantNoErr(t, err)
	f.svc = svc
	wantNoErr(t, svc.Start(ctxT(t)))
	return f, f.db
}

// cutoffs returns the "before" argument of every core.audit_purge call.
func cutoffs(d *flakyDB) []time.Time {
	var out []time.Time
	for _, args := range d.countedCalls("audit_purge") {
		for _, a := range args {
			if at, ok := a.(time.Time); ok {
				out = append(out, at)
			}
		}
	}
	return out
}

func runPurge(t *testing.T, f *fx) error {
	t.Helper()
	return f.jobs.Run(ctxT(t), "audit.purge", jobs.Job{}) //nolint:wrapcheck // test pass-through of the handler's error
}

// D30: 0 keeps forever.
func TestPurge_RetentionZero_DeletesNothing(t *testing.T) {
	f, p := purgeFx(t)
	// Unset is the declared default, 0; an explicit 0 is the same.
	for _, set := range []bool{false, true} {
		if set {
			f.settings.Set("retention", "0")
		}
		wantNoErr(t, runPurge(t, f))
	}
	if got := cutoffs(p); len(got) != 0 {
		t.Errorf("purge ran with retention 0: cut-offs %v", got)
	}
	if n := len(f.fake.Calls()); n != 0 {
		t.Errorf("a retention of 0 ran %d statements", n)
	}
	if got := f.rec.Recorded(); len(got) != 0 {
		t.Errorf("a retention of 0 recorded %d entries", len(got))
	}
}

func TestPurge_Retention_CutsOffAtNowMinusRetention(t *testing.T) {
	f, p := purgeFx(t)
	f.settings.Set("retention", "8760h")
	wantNoErr(t, runPurge(t, f))
	if got := f.recorded("audit.purged"); len(got) != 1 {
		t.Errorf("audit.purged entries = %d, want 1 per purge", len(got))
	}
	want := epoch.Add(-8760 * time.Hour)
	got := cutoffs(p)
	if len(got) != 1 || !got[0].Equal(want) {
		t.Fatalf("cut-offs = %v, want one at %v (clock minus retention)", got, want)
	}
	// The clock is injected: a later run cuts off later.
	advance(t, f.clk, 48*time.Hour)
	wantNoErr(t, runPurge(t, f))
	got = cutoffs(p)
	if len(got) != 2 || !got[1].Equal(want.Add(48*time.Hour)) {
		t.Errorf("second cut-off = %v, want %v", got, want.Add(48*time.Hour))
	}
}

func TestPurge_RetentionChange_TakesEffectAtNextRun(t *testing.T) {
	f, p := purgeFx(t)
	f.settings.Set("retention", "8760h")
	wantNoErr(t, runPurge(t, f))
	f.settings.Set("retention", "0")
	wantNoErr(t, runPurge(t, f))
	if got := cutoffs(p); len(got) != 1 {
		t.Errorf("cut-offs = %v: the setting must be read at run time, not cached at Start", got)
	}
}

// B8.
func TestPurge_ReadOnlyCopy_ReturnsUnavailable(t *testing.T) {
	f, _ := purgeFx(t)
	f.settings.Set("retention", "8760h")
	f.fake.SetReadOnly(true)
	wantReadOnly(t, runPurge(t, f))
}

func TestPurge_ReadOnlyCopy_RetentionZero_IsNotAnError(t *testing.T) {
	f, _ := purgeFx(t)
	f.fake.SetReadOnly(true)
	wantNoErr(t, runPurge(t, f))
}

func TestPurge_StoreFails_ReturnsKind(t *testing.T) {
	for _, kind := range []errs.Kind{errs.Unavailable, errs.Timeout, errs.Internal} {
		t.Run(kind.String(), func(t *testing.T) {
			f := f0(t)
			f.db.setFailQuery(errs.E("test.store", kind, errors.New("store failed")))
			svc, err := auditlog.New(f.opts)
			wantNoErr(t, err)
			f.svc = svc
			wantNoErr(t, svc.Start(ctxT(t)))
			f.settings.Set("retention", "8760h")
			wantKind(t, runPurge(t, f), kind)
		})
	}
}

// failReader is a settings.Reader whose every read fails.
type failReader struct {
	settings.Reader
	err error
}

func (r failReader) Duration(context.Context, string) (time.Duration, error) { return 0, r.err }

func TestPurge_SettingFails_ReturnsItsKind(t *testing.T) {
	f := f0(t)
	f.opts.Settings = failReader{err: errs.E("test.settings", errs.Unavailable, errors.New("down"))}
	svc, err := auditlog.New(f.opts)
	wantNoErr(t, err)
	wantNoErr(t, svc.Start(ctxT(t)))
	wantKind(t, runPurge(t, f), errs.Unavailable)
	if n := len(f.fake.Calls()); n != 0 {
		t.Errorf("purge ran %d statements though the retention could not be read", n)
	}
}

func TestPurge_CtxDone_ReturnsCanceled(t *testing.T) {
	f, _ := purgeFx(t)
	f.settings.Set("retention", "8760h")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	wantKind(t, f.jobs.Run(ctx, "audit.purge", jobs.Job{}), errs.Canceled)
}
