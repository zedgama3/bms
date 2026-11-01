package identity_test

// Step 8 of plan/design/boundaries.md: identity.NewLookup builds EmailOf over core.account
// alone. Constructor failures are unit tests; the reads are in integration_lookup_test.go.

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

func TestNewLookup_NilDependency_ReturnsInvalid(t *testing.T) {
	c := telemetrytest.New(t)
	for _, tc := range []struct {
		field string
		o     identity.LookupOptions
	}{
		{"identity.db", identity.LookupOptions{Tracer: c.Tracer("identity")}},
		{"identity.tracer", identity.LookupOptions{DB: db.None(), Tracer: telemetry.Tracer(nil)}},
	} {
		t.Run(tc.field, func(t *testing.T) {
			l, err := identity.NewLookup(tc.o)
			wantField(t, err, tc.field, "")
			if l != nil {
				t.Error("NewLookup returned a Lookup with its error")
			}
		})
	}
}

func TestNewLookup_AllNil_ReportsEveryField(t *testing.T) {
	_, err := identity.NewLookup(identity.LookupOptions{})
	wantKind(t, err, errs.Invalid)
	have := map[string]bool{}
	for _, fe := range validate.Fields(err) {
		have[fe.Field] = true
	}
	for _, f := range []string{"identity.db", "identity.tracer"} {
		if !have[f] {
			t.Errorf("zero LookupOptions does not report %s", f)
		}
	}
}

// No I/O at build: the fake database sees no statement, and db.None() (which refuses every
// statement, as under bms openapi and bms check) builds.
func TestNewLookup_NoIO(t *testing.T) {
	f := dbtest.NewFake()
	c := telemetrytest.New(t)
	l, err := identity.NewLookup(identity.LookupOptions{DB: f, Tracer: c.Tracer("identity")})
	wantNoErr(t, err)
	if l == nil {
		t.Fatal("nil Lookup")
	}
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("NewLookup ran %d statements", n)
	}
	if _, err := identity.NewLookup(identity.LookupOptions{DB: db.None(), Tracer: c.Tracer("identity")}); err != nil {
		t.Fatalf("NewLookup over db.None(): %v", err)
	}
}
