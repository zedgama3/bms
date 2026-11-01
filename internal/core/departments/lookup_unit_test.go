package departments_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// The Lookup is the Directory and what authz takes as its DepartmentSource.
var (
	_ departments.Directory  = (*departments.Lookup)(nil)
	_ authz.DepartmentSource = (*departments.Lookup)(nil)
)

func TestNewLookup_NilDependency_ReturnsInvalid(t *testing.T) {
	c := telemetrytest.New(t)
	tests := []struct {
		field string
		o     departments.LookupOptions
	}{
		{"departments.db", departments.LookupOptions{Tracer: c.Tracer("departments")}},
		{"departments.tracer", departments.LookupOptions{DB: db.None(), Tracer: telemetry.Tracer(nil)}},
	}
	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			l, err := departments.NewLookup(tc.o)
			wantField(t, err, tc.field, "")
			if l != nil {
				t.Error("NewLookup returned a Lookup with its error")
			}
		})
	}
}

func TestNewLookup_AllNil_ReportsEveryField(t *testing.T) {
	_, err := departments.NewLookup(departments.LookupOptions{})
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{"departments.db", "departments.tracer"} {
		if !hasField(err, f, "") {
			t.Errorf("zero LookupOptions does not report %s", f)
		}
	}
}

// "does no I/O" is implied by the build order (item 2 takes a db, nothing is read).
func TestNewLookup_NoIO(t *testing.T) {
	f := dbtest.NewFake()
	c := telemetrytest.New(t)
	l, err := departments.NewLookup(departments.LookupOptions{DB: f, Tracer: c.Tracer("departments")})
	wantNoErr(t, err)
	if l == nil {
		t.Fatal("nil Lookup")
	}
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("%d statements ran in NewLookup", n)
	}
}

// Active keeps the Service's input check: the zero ID is Invalid, before any statement.
func TestLookup_Active_ZeroID_ReturnsInvalid(t *testing.T) {
	f := dbtest.NewFake()
	c := telemetrytest.New(t)
	l, err := departments.NewLookup(departments.LookupOptions{DB: f, Tracer: c.Tracer("departments")})
	wantNoErr(t, err)
	_, err = l.Active(ctxT(t), model.DepartmentID{})
	wantKind(t, err, errs.Invalid)
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("%d statements ran for a zero ID", n)
	}
}

// Options.Lookup is required.
func TestNew_NilLookup_ReturnsInvalid(t *testing.T) {
	o, _ := validOptions(t, dbtest.NewFake())
	o.Lookup = nil
	svc, err := departments.New(o)
	wantField(t, err, "departments.lookup", "")
	if svc != nil {
		t.Error("New returned a service with its error")
	}
}
