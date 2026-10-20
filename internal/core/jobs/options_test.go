package jobs_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/retry"
)

func validOptions(t *testing.T) jobs.Options {
	t.Helper()
	_, o := newRigOptions(t, dbtest.NewFake())
	return o
}

func TestNew_ValidOptions_Builds(t *testing.T) {
	o := validOptions(t)
	wantNoErr(t, o.Validate())
	r, err := jobs.New(o)
	wantNoErr(t, err)
	if r == nil {
		t.Fatal("nil runner")
	}
}

func TestNew_WorksAgainstDBNone(t *testing.T) { // no I/O in New, Register or Scoped (bms check)
	o := validOptions(t)
	o.DB = db.None()
	r, err := jobs.New(o)
	wantNoErr(t, err)
	s, err := r.Scoped("volunteers")
	wantNoErr(t, err)
	wantNoErr(t, s.Register(jobs.Decl{Name: "volunteers.remind", Every: time.Hour, Handler: nop}))
	wantNoErr(t, r.Close(ctxT(t)))
}

func TestNew_ZeroOptions_ListsEveryRequiredField(t *testing.T) {
	_, err := jobs.New(jobs.Options{})
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{
		"jobs.db", "jobs.clock", "jobs.logger", "jobs.tracer", "jobs.meter", "jobs.audit",
		"jobs.authz", "jobs.actor", "jobs.redeliverer",
	} {
		if !hasField(err, f, "") {
			t.Errorf("field %q missing from %v", f, describe(err))
		}
	}
}

func TestNew_NilDB_ReturnsInvalid(t *testing.T) {
	o := validOptions(t)
	o.DB = nil
	_, err := jobs.New(o)
	wantField(t, err, "jobs.db", "")
	wantField(t, o.Validate(), "jobs.db", "")
}

func TestNew_NilDependency_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		field string
		mod   func(*jobs.Options)
	}{
		{"jobs.clock", func(o *jobs.Options) { o.Clock = nil }},
		{"jobs.logger", func(o *jobs.Options) { o.Logger = nil }},
		{"jobs.tracer", func(o *jobs.Options) { o.Tracer = nil }},
		{"jobs.meter", func(o *jobs.Options) { o.Meter = nil }},
		{"jobs.audit", func(o *jobs.Options) { o.Audit = nil }},
		{"jobs.authz", func(o *jobs.Options) { o.Authz = nil }},
		{"jobs.actor", func(o *jobs.Options) { o.Actor = nil }},
		{"jobs.redeliverer", func(o *jobs.Options) { o.Redeliverer = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			o := validOptions(t)
			tc.mod(&o)
			_, err := jobs.New(o)
			wantField(t, err, tc.field, "")
			wantField(t, o.Validate(), tc.field, "")
		})
	}
}

func TestNew_BadInstance_ReturnsInvalid(t *testing.T) {
	for _, in := range []string{"UPPER", "has space", "-lead", "trail-", strings.Repeat("a", 65), "é"} {
		o := validOptions(t)
		o.Instance = in
		_, err := jobs.New(o)
		wantField(t, err, "jobs.instance", "")
	}
}

func TestNew_DurationsAndCountsOutOfRange_ReturnInvalid(t *testing.T) {
	tests := []struct {
		name  string
		field string
		mod   func(*jobs.Options)
		ok    bool
	}{
		{"poll below min", "jobs.poll_interval", func(o *jobs.Options) { o.PollInterval = 999 * time.Millisecond }, false},
		{"poll above max", "jobs.poll_interval", func(o *jobs.Options) { o.PollInterval = 5*time.Minute + time.Nanosecond }, false},
		{"poll negative", "jobs.poll_interval", func(o *jobs.Options) { o.PollInterval = -time.Second }, false},
		{"poll min", "jobs.poll_interval", func(o *jobs.Options) { o.PollInterval = time.Second }, true},
		{"poll max", "jobs.poll_interval", func(o *jobs.Options) { o.PollInterval = 5 * time.Minute }, true},
		{"lease below min", "jobs.lease", func(o *jobs.Options) { o.Lease = 30*time.Second - time.Nanosecond }, false},
		{"lease above max", "jobs.lease", func(o *jobs.Options) { o.Lease = 10*time.Minute + time.Nanosecond }, false},
		{"lease min", "jobs.lease", func(o *jobs.Options) { o.Lease = 30 * time.Second }, true},
		{"lease max", "jobs.lease", func(o *jobs.Options) { o.Lease = 10 * time.Minute }, true},
		{"run timeout below min", "jobs.run_timeout", func(o *jobs.Options) { o.RunTimeout = 999 * time.Millisecond }, false},
		{"run timeout above max", "jobs.run_timeout", func(o *jobs.Options) { o.RunTimeout = jobs.MaxRunTimeout + time.Nanosecond }, false},
		{"run timeout min", "jobs.run_timeout", func(o *jobs.Options) { o.RunTimeout = time.Second }, true},
		{"run timeout max", "jobs.run_timeout", func(o *jobs.Options) { o.RunTimeout = jobs.MaxRunTimeout }, true},
		{"workers negative", "jobs.workers", func(o *jobs.Options) { o.Workers = -1 }, false},
		{"workers above max", "jobs.workers", func(o *jobs.Options) { o.Workers = jobs.MaxWorkers + 1 }, false},
		{"workers one", "jobs.workers", func(o *jobs.Options) { o.Workers = 1 }, true},
		{"workers max", "jobs.workers", func(o *jobs.Options) { o.Workers = jobs.MaxWorkers }, true},
		{"retry negative initial", "jobs.retry", func(o *jobs.Options) {
			o.Retry = retry.Policy{Initial: -time.Second, Max: time.Minute, Multiplier: 2, MaxAttempts: 3}
		}, false},
		{"retry max below initial", "jobs.retry", func(o *jobs.Options) {
			o.Retry = retry.Policy{Initial: time.Minute, Max: time.Second, Multiplier: 2, MaxAttempts: 3}
		}, false},
		{"retry multiplier below one", "jobs.retry", func(o *jobs.Options) {
			o.Retry = retry.Policy{Initial: time.Second, Max: time.Minute, Multiplier: 0.5, MaxAttempts: 3}
		}, false},
		{"retry attempts above 100", "jobs.retry", func(o *jobs.Options) {
			o.Retry = retry.Policy{Initial: time.Second, Max: time.Minute, Multiplier: 2, MaxAttempts: 101}
		}, false},
		{"retry attempts 100", "jobs.retry", func(o *jobs.Options) {
			o.Retry = retry.Policy{Initial: time.Second, Max: time.Minute, Multiplier: 2, MaxAttempts: 100}
		}, true},
		{"retry attempts 1", "jobs.retry", func(o *jobs.Options) {
			o.Retry = retry.Policy{Initial: time.Second, Max: time.Minute, Multiplier: 2, MaxAttempts: 1}
		}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := validOptions(t)
			tc.mod(&o)
			_, err := jobs.New(o)
			if tc.ok {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, tc.field, "")
		})
	}
}

func TestNew_LeaseBelowMinimum_ReturnsInvalid(t *testing.T) {
	o := validOptions(t)
	o.Lease = 10 * time.Second
	_, err := jobs.New(o)
	wantField(t, err, "jobs.lease", "")
}

func TestNew_WorkersAboveMax_ReturnsInvalid(t *testing.T) {
	o := validOptions(t)
	o.Workers = jobs.MaxWorkers + 1
	_, err := jobs.New(o)
	wantField(t, err, "jobs.workers", "")
}

func TestNew_EveryProblem_IsListedAtOnce(t *testing.T) {
	o := validOptions(t)
	o.Lease = time.Second
	o.Workers = 99
	o.PollInterval = time.Hour
	o.RunTimeout = time.Nanosecond
	o.Instance = "BAD"
	err := o.Validate()
	for _, f := range []string{"jobs.lease", "jobs.workers", "jobs.poll_interval", "jobs.run_timeout", "jobs.instance"} {
		if !hasField(err, f, "") {
			t.Errorf("field %q missing from %v", f, describe(err))
		}
	}
}

func TestNew_ZeroDurationsAndCounts_TakeDefaults(t *testing.T) {
	o := validOptions(t)
	o.PollInterval, o.Lease, o.RunTimeout, o.Workers, o.Retry = 0, 0, 0, 0, retry.Policy{}
	wantNoErr(t, o.Validate())
}

// authz import keeps the permission constants checked against the type.
var _ authz.Permission = permRead
