package audit_test

import (
	"context"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
)

// recordedActor records one login entry under ctx and returns the actor of the single
// spooled line. rec must come from an fx that was made read-only, so the entry spools.
func recordedActor(ctx context.Context, t *testing.T, f *fx, e audit.Entry) map[string]any {
	t.Helper()
	rec := f.scoped(t, "identity")
	before := len(f.spooled(t))
	wantNoErr(t, rec.Record(ctx, e))
	lines := f.spooled(t)
	if len(lines) != before+1 {
		t.Fatalf("%d spooled lines after Record, want %d", len(lines), before+1)
	}
	return obj(lines[before]["actor"])
}

// Decision 6 (boundaries step 3): the system marker comes before the actor function, so a
// job's entry is the system even when a session is also in ctx (and even if the operator
// marker is set).
func TestResolveActor_SystemMarker_WinsOverSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  func(context.Context) context.Context
	}{
		{"marker alone", audit.WithSystem},
		{"marker with a session", func(c context.Context) context.Context {
			return audit.WithSystem(withSession(c, person(alice)))
		}},
		{"marker with a superuser session", func(c context.Context) context.Context {
			return audit.WithSystem(withSession(c, superuser(alice)))
		}},
		{"marker with a session, operator marker set first", func(c context.Context) context.Context {
			return audit.WithSystem(audit.WithOperator(withSession(c, person(alice))))
		}},
		{"marker with a session, operator marker set after", func(c context.Context) context.Context {
			return audit.WithOperator(audit.WithSystem(withSession(c, person(alice))))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFx(t)
			f.readOnly()
			a := recordedActor(tc.ctx(ctxT(t)), t, f, loginEntry())
			if str(a["kind"]) != "system" {
				t.Errorf("actor.kind = %q, want system", str(a["kind"]))
			}
			if _, ok := a["person"]; ok {
				t.Error("a system actor carries no person")
			}
			if a["superuser"] == true {
				t.Error("a system actor is never a superuser")
			}
		})
	}
}

// An entry that names its actor keeps it; the marker only fills in for entries that name none.
func TestResolveActor_SystemMarker_ExplicitActorIsKept(t *testing.T) {
	f := newFx(t)
	f.readOnly()
	e := loginEntry()
	e.Actor = person(bob)
	a := recordedActor(audit.WithSystem(ctxT(t)), t, f, e)
	if str(a["kind"]) != "person" || str(a["person"]) != bob.String() {
		t.Errorf("actor = %v, want the named person", a)
	}
}

// The operator marker answers when nothing else does: no session, no delivery, no system.
func TestResolveActor_OperatorMarker_WithoutSession(t *testing.T) {
	f := newFx(t)
	f.readOnly()
	a := recordedActor(audit.WithOperator(ctxT(t)), t, f, loginEntry())
	if str(a["kind"]) != "operator" {
		t.Errorf("actor.kind = %q, want operator", str(a["kind"]))
	}
	if _, ok := a["person"]; ok {
		t.Error("an operator actor carries no person")
	}
	if a["superuser"] == true {
		t.Error("an operator actor is not marked superuser")
	}
}

// The order is delivery, system, Actor, operator: the Actor function beats the operator marker.
func TestResolveActor_OperatorMarker_SessionPersonComesFirst(t *testing.T) {
	f := newFx(t)
	f.readOnly()
	ctx := audit.WithOperator(withSession(ctxT(t), person(alice)))
	a := recordedActor(ctx, t, f, loginEntry())
	if str(a["kind"]) != "person" || str(a["person"]) != alice.String() {
		t.Errorf("actor = %v, want the session's person", a)
	}
}
