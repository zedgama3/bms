package files_test

import (
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/jobs/jobstest"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func withJobs(j *jobstest.Fake) rigOption {
	return func(_ *rig, o *files.Options) { o.Jobs = j }
}

// Start registers the two housekeeping jobs files owns, with the periods and timeout app
// registered before they moved here.
func TestStart_RegistersReapAndSweep(t *testing.T) {
	jf := jobstest.NewFake("files")
	r := newRig(t, withJobs(jf))
	wantNoErr(t, r.svc.Start(ctxT(t)))

	want := map[jobs.Name]time.Duration{
		"files.reap":          time.Hour,
		"files.sweep-orphans": 24 * time.Hour,
	}
	got := jf.Registered()
	if len(got) != len(want) {
		t.Fatalf("registered %d jobs, want exactly %d: %+v", len(got), len(want), got)
	}
	for _, d := range got {
		every, ok := want[d.Name]
		if !ok {
			t.Errorf("unexpected job %q", d.Name)
			continue
		}
		if d.Every != every {
			t.Errorf("%s every = %v, want %v", d.Name, d.Every, every)
		}
		if d.Timeout != 30*time.Minute {
			t.Errorf("%s timeout = %v, want 30m", d.Name, d.Timeout)
		}
		if d.Handler == nil {
			t.Errorf("%s has no handler", d.Name)
		}
	}
}

func TestStart_Twice_ReturnsConflict(t *testing.T) {
	jf := jobstest.NewFake("files")
	r := newRig(t, withJobs(jf))
	wantNoErr(t, r.svc.Start(ctxT(t)))
	wantKind(t, r.svc.Start(ctxT(t)), errs.Conflict)
	if n := len(jf.Registered()); n != 2 {
		t.Errorf("%d jobs registered after a second Start, want 2", n)
	}
}

// Registering a job is in memory; on a read-only copy Start still succeeds and writes nothing.
func TestStart_ReadOnlyCopy_Succeeds_WritesNothing(t *testing.T) {
	jf := jobstest.NewFake("files")
	r := newRig(t, withJobs(jf))
	r.fake.SetReadOnly(true)
	wantNoErr(t, r.svc.Start(ctxT(t)))
	if n := len(jf.Registered()); n != 2 {
		t.Errorf("%d jobs registered, want 2", n)
	}
	if len(r.audit.Recorded()) != 0 {
		t.Error("Start recorded an audit entry")
	}
	if calls := r.fake.Calls(); len(calls) != 0 {
		t.Errorf("Start made %d database calls on a read-only copy, want none", len(calls))
	}
}

func TestNew_NilJobs_ReturnsInvalid(t *testing.T) {
	r := newRig(t, withJobs(jobstest.NewFake("files")))
	o := r.opts
	o.Jobs = nil
	_, err := files.New(o)
	wantField(t, err, errs.Invalid, "files.jobs", "required")
}
