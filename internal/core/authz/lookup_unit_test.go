package authz_test

import (
	"log/slog"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Compile-time: the Lookup is what the consumers take (boundaries.md, "authz: the Lookup").
var (
	_ authz.Authorizer          = (*authz.Lookup)(nil)
	_ identity.PermissionLister = (*authz.Lookup)(nil)
	_ audit.TitleSource         = (*authz.Lookup)(nil)
)

func validLookupOptions(t testing.TB, d db.DB) authz.LookupOptions {
	t.Helper()
	r := validOptions(t, d)
	return r.lookupOptions()
}

// boundaries.md step 4 / build-order item 3: the Departments source is required.
func TestNewLookup_NilDepartments_ReturnsInvalid(t *testing.T) {
	o := validLookupOptions(t, db.None())
	o.Departments = nil
	l, err := authz.NewLookup(o)
	wantKind(t, err, errs.Invalid)
	if !hasField(err, "authz.departments") {
		t.Fatalf("no field error authz.departments in %v", validate.Fields(err))
	}
	if l != nil {
		t.Fatal("NewLookup returned a Lookup with an error")
	}
}

// Every required LookupOptions field is reported, by name.
func TestNewLookup_NilDependency_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		field string
		mod   func(*authz.LookupOptions)
	}{
		{"authz.db", func(o *authz.LookupOptions) { o.DB = nil }},
		{"authz.clock", func(o *authz.LookupOptions) { o.Clock = clock.Clock(nil) }},
		{"authz.logger", func(o *authz.LookupOptions) { o.Logger = (*slog.Logger)(nil) }},
		{"authz.tracer", func(o *authz.LookupOptions) { o.Tracer = telemetry.Tracer(nil) }},
		{"authz.actor", func(o *authz.LookupOptions) { o.Actor = nil }},
		{"authz.departments", func(o *authz.LookupOptions) { o.Departments = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			o := validLookupOptions(t, db.None())
			tc.mod(&o)
			l, err := authz.NewLookup(o)
			wantKind(t, err, errs.Invalid)
			if !hasField(err, tc.field) {
				t.Fatalf("no field error %q in %v", tc.field, validate.Fields(err))
			}
			if l != nil {
				t.Fatal("NewLookup returned a Lookup with an error")
			}
		})
	}
}

func TestNewLookup_AllNil_ReportsEveryField(t *testing.T) {
	_, err := authz.NewLookup(authz.LookupOptions{})
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{"authz.db", "authz.clock", "authz.logger", "authz.tracer", "authz.actor", "authz.departments"} {
		if !hasField(err, f) {
			t.Errorf("missing field error %q", f)
		}
	}
}

func TestNewLookup_OptionalFieldsMayBeNil(t *testing.T) {
	o := validLookupOptions(t, db.None())
	o.Meter = nil
	o.Declared = nil
	l, err := authz.NewLookup(o)
	wantNoErr(t, err)
	if l == nil {
		t.Fatal("nil Lookup")
	}
}

// F2 carries over: the Lookup validates the declarations too.
func TestNewLookup_DuplicateDeclaration_ReturnsInvalid(t *testing.T) {
	o := validLookupOptions(t, db.None())
	o.Declared = append(o.Declared, o.Declared[0])
	_, err := authz.NewLookup(o)
	wantKind(t, err, errs.Invalid)
	if !hasField(err, "authz.declared") {
		t.Fatalf("no authz.declared field error in %v", validate.Fields(err))
	}
}

// "does no I/O": a database that refuses every statement still builds a Lookup.
func TestNewLookup_NoIO_WorksAgainstNoneDB(t *testing.T) {
	f := dbtest.NewFake()
	o := validLookupOptions(t, f)
	_, err := authz.NewLookup(o)
	wantNoErr(t, err)
	wantNoStatements(t, f)
}

// authz.New requires the Lookup.
func TestNew_NilLookup_ReturnsInvalid(t *testing.T) {
	r := validOptions(t, db.None())
	r.opts.Lookup = nil
	svc, err := authz.New(r.opts)
	wantKind(t, err, errs.Invalid)
	if !hasField(err, "authz.lookup") {
		t.Fatalf("no field error authz.lookup in %v", validate.Fields(err))
	}
	if svc != nil {
		t.Fatal("New returned a service with an error")
	}
}

// F3, F4 on the Lookup itself (not through the Service): no actor is Unauthenticated and
// no statement runs.
func TestLookup_NoActor_ReturnsUnauthenticated(t *testing.T) {
	f := dbtest.NewFake()
	r := validOptions(t, f)
	l := r.lookupFor()
	wantKind(t, l.Allow(t.Context(), pGreetRead), errs.Unauthenticated)
	wantKind(t, l.AllowIn(t.Context(), pShiftManage, r.d1), errs.Unauthenticated)
	_, err := l.ExtentOf(t.Context(), pShiftManage)
	wantKind(t, err, errs.Unauthenticated)
	wantNoStatements(t, f)
}

// F5-style: an undeclared permission is Invalid on the Lookup, before any statement.
func TestLookup_UndeclaredPermission_ReturnsInvalid(t *testing.T) {
	f := dbtest.NewFake()
	r := validOptions(t, f)
	l := r.lookupFor()
	ctx := as(t.Context(), model.NewID[model.PersonID]())
	wantKind(t, l.Allow(ctx, pGhost), errs.Invalid)
	wantNoStatements(t, f)
}

// The Service's decision methods forward to the Lookup (spec: "the Service forwards the
// decision methods to it"): a Service and its Lookup agree on the no-actor answer.
func TestService_Decisions_ForwardToLookup(t *testing.T) {
	r := newUnitRig(t, dbtest.NewFake())
	l := r.opts.Lookup
	a := r.svc.Allow(t.Context(), pGreetRead)
	b := l.Allow(t.Context(), pGreetRead)
	if errs.KindOf(a) != errs.KindOf(b) {
		t.Fatalf("service kind %v, lookup kind %v", errs.KindOf(a), errs.KindOf(b))
	}
}
