package people_test

// Step 8 of plan/design/boundaries.md: people.NewLookup builds the system-level reads of
// core.person. Constructor failures are unit tests; the reads are in
// integration_lookup_test.go.

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestNewLookup_NilDependency_ReturnsInvalid(t *testing.T) {
	c := telemetrytest.New(t)
	for _, tc := range []struct {
		field string
		o     people.LookupOptions
	}{
		{"people.db", people.LookupOptions{Tracer: c.Tracer("people")}},
		{"people.tracer", people.LookupOptions{DB: db.None(), Tracer: telemetry.Tracer(nil)}},
	} {
		t.Run(tc.field, func(t *testing.T) {
			l, err := people.NewLookup(tc.o)
			wantField(t, err, tc.field, "")
			if l != nil {
				t.Error("NewLookup returned a Lookup with its error")
			}
		})
	}
}

func TestNewLookup_AllNil_ReportsEveryRequiredField(t *testing.T) {
	_, err := people.NewLookup(people.LookupOptions{})
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{"people.db", "people.tracer"} {
		if !hasField(err, f, "") {
			t.Errorf("zero LookupOptions does not report %s", f)
		}
	}
}

func TestNewLookup_NoIO(t *testing.T) {
	f := dbtest.NewFake()
	c := telemetrytest.New(t)
	l, err := people.NewLookup(people.LookupOptions{DB: f, Tracer: c.Tracer("people")})
	wantNoErr(t, err)
	if l == nil {
		t.Fatal("nil Lookup")
	}
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("NewLookup ran %d statements", n)
	}
	if _, err := people.NewLookup(people.LookupOptions{DB: db.None(), Tracer: c.Tracer("people")}); err != nil {
		t.Fatalf("NewLookup over db.None(): %v", err)
	}
}
