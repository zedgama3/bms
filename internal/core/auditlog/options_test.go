package auditlog_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/auditlog"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestNew_Valid_DoesNoIO(t *testing.T) {
	f := f0(t)
	f.opts.DB = db.None() // app builds auditlog against db.None() for bms openapi and bms check
	s, err := auditlog.New(f.opts)
	wantNoErr(t, err)
	if s == nil {
		t.Fatal("nil Service")
	}
	if n := len(f.jobs.Registered()); n != 0 {
		t.Errorf("New registered %d jobs; only Start registers", n)
	}
	if n := len(f.fake.Calls()); n != 0 {
		t.Errorf("New ran %d statements", n)
	}
}

// B6.
func TestNew_NilDependency_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name  string
		field string
		mod   func(*auditlog.Options)
	}{
		{"db", "auditlog.db", func(o *auditlog.Options) { o.DB = nil }},
		{"clock", "auditlog.clock", func(o *auditlog.Options) { o.Clock = nil }},
		{"logger", "auditlog.logger", func(o *auditlog.Options) { o.Logger = nil }},
		{"tracer", "auditlog.tracer", func(o *auditlog.Options) { o.Tracer = nil }},
		{"authz", "auditlog.authz", func(o *auditlog.Options) { o.Authz = nil }},
		{"actor", "auditlog.actor", func(o *auditlog.Options) { o.Actor = nil }},
		{"settings", "auditlog.settings", func(o *auditlog.Options) { o.Settings = nil }},
		{"audit", "auditlog.audit", func(o *auditlog.Options) { o.Audit = nil }},
		{"jobs", "auditlog.jobs", func(o *auditlog.Options) { o.Jobs = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := f0(t)
			tc.mod(&f.opts)
			s, err := auditlog.New(f.opts)
			wantKind(t, err, errs.Invalid)
			wantField(t, err, tc.field)
			if s != nil {
				t.Error("New returned a Service with its error")
			}
		})
	}
	t.Run("all nil reports every field", func(t *testing.T) {
		_, err := auditlog.New(auditlog.Options{})
		wantKind(t, err, errs.Invalid)
		for _, tc := range tests {
			wantField(t, err, tc.field)
		}
	})
}
