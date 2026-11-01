//go:build integration

package departments_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/model"
)

func (r *irig) lookup() *departments.Lookup {
	r.t.Helper()
	c := telemetrytest.New(r.t)
	l, err := departments.NewLookup(departments.LookupOptions{DB: r.pool, Tracer: c.Tracer("departments")})
	wantNoErr(r.t, err)
	return l
}

// boundaries.md step 4: the Lookup answers Active as the Service does (F30): archived and
// unknown are false, an active department true.
func TestLookup_Active_ArchivedIsFalse(t *testing.T) {
	r := newIRig(t)
	l := r.lookup()
	active := r.mk("Rangers")
	archived := r.mkArchived("Gate")
	for name, tc := range map[string]struct {
		id   model.DepartmentID
		want bool
	}{
		"active":   {active.ID, true},
		"archived": {archived.ID, false},
		"unknown":  {newDept(), false},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := l.Active(ctxT(t), tc.id)
			wantNoErr(t, err)
			if got != tc.want {
				t.Errorf("Lookup.Active = %v, want %v", got, tc.want)
			}
		})
	}
}

// The Lookup is internal: no actor, no permission, no audit record.
func TestLookup_NamesOfAndActive_NoActorNoAudit(t *testing.T) {
	r := newIRig(t)
	l := r.lookup()
	d := r.mk("Greeters")
	entries, sigs := r.mark()
	got, err := l.NamesOf(ctxT(t), []model.DepartmentID{d.ID, newDept()})
	wantNoErr(t, err)
	if len(got) != 1 {
		t.Fatalf("NamesOf = %v, want only the known department", got)
	}
	if ok, err := l.Active(ctxT(t), d.ID); err != nil || !ok {
		t.Fatalf("Active = %v, %v", ok, err)
	}
	e2, s2 := r.mark()
	if e2 != entries || s2 != sigs {
		t.Errorf("Lookup wrote audit entries or signals (%d->%d, %d->%d)", entries, e2, sigs, s2)
	}
}
