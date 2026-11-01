package authz_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// queryless is a rig over a dbtest fake, so a test can prove no statement ran.
func queryless(t *testing.T) (*rig, *dbtest.Fake) {
	t.Helper()
	f := dbtest.NewFake()
	return newUnitRig(t, f), f
}

func wantNoStatements(t *testing.T, f *dbtest.Fake) {
	t.Helper()
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("%d statements ran, want none", n)
	}
}

// F3
func TestAllow_NoActor_ReturnsUnauthenticated(t *testing.T) {
	r, f := queryless(t)
	wantKind(t, r.svc.Allow(t.Context(), pGreetRead), errs.Unauthenticated)
	wantKind(t, r.svc.AllowIn(t.Context(), pShiftManage, r.d1), errs.Unauthenticated)
	_, err := r.svc.ExtentOf(t.Context(), pShiftManage)
	wantKind(t, err, errs.Unauthenticated)
	wantNoStatements(t, f)
}

// decision 7: a restricted session is not an actor (the app closure over identity).
func TestAllow_RestrictedSession_ReturnsUnauthenticated(t *testing.T) {
	person := newPerson()
	appActor := func(ctx context.Context) (authz.Actor, bool) {
		p, ok := identity.PersonFrom(ctx)
		s, _ := identity.SessionFrom(ctx)
		return authz.Actor{Person: p, Superuser: s.Superuser}, ok
	}
	r2 := newUnitRig(t, dbtest.NewFake(), func(r *rig) { r.lkopts.Actor = appActor })
	s := identity.Session{
		ID: model.NewID[identity.SessionID](), Account: model.NewID[model.AccountID](), Person: person,
		Superuser: true, PasswordChangeRequired: true, IssuedAt: epoch, ExpiresAt: epoch.Add(time.Hour),
	}
	ctx := identity.WithSession(t.Context(), s)
	wantKind(t, r2.svc.Allow(ctx, pGreetRead), errs.Unauthenticated)
	wantKind(t, r2.svc.AllowIn(ctx, pShiftManage, r2.d1), errs.Unauthenticated)
	_, err := r2.svc.ExtentOf(ctx, pShiftManage)
	wantKind(t, err, errs.Unauthenticated)
}

// F4: even a superuser value in ctx does not count inside a signal delivery.
func TestAllow_InDelivery_ReturnsUnauthenticated(t *testing.T) {
	r, f := queryless(t)
	bus, err := signalstest.NewFake("people", nil, []signals.Name{sigCreated})
	wantNoErr(t, err)
	var gotAllow, gotIn, gotExt error
	wantNoErr(t, bus.Subscribe(sigCreated, func(ctx context.Context, _ signals.Signal) error {
		gotAllow = r.svc.Allow(ctx, pGreetRead)
		gotIn = r.svc.AllowIn(ctx, pShiftManage, r.d1)
		_, gotExt = r.svc.ExtentOf(ctx, pShiftManage)
		return nil
	}))
	sig := signals.Signal{
		ID: model.NewID[signals.SignalID](), Name: sigCreated, At: epoch,
		Payload: signals.NewPayload(
			signals.Ref("grant", model.NewID[authz.GrantID]()),
			signals.Ref("person", newPerson()),
			signals.Ref("role", model.NewID[authz.RoleID]()),
		),
	}
	wantNoErr(t, bus.Deliver(asSuper(t.Context(), newPerson()), sig))
	wantKind(t, gotAllow, errs.Unauthenticated)
	wantKind(t, gotIn, errs.Unauthenticated)
	wantKind(t, gotExt, errs.Unauthenticated)
	wantNoStatements(t, f)
}

// F5
func TestAllow_Undeclared_ReturnsInvalid(t *testing.T) {
	r, _ := queryless(t)
	for name, ctx := range map[string]context.Context{
		"no actor":  t.Context(),
		"person":    as(t.Context(), newPerson()),
		"superuser": asSuper(t.Context(), newPerson()),
	} {
		t.Run(name, func(t *testing.T) {
			wantKind(t, r.svc.Allow(ctx, pGhost), errs.Invalid)
			wantKind(t, r.svc.AllowIn(ctx, pGhost, r.d1), errs.Invalid)
			_, err := r.svc.ExtentOf(ctx, pGhost)
			wantKind(t, err, errs.Invalid)
		})
	}
}

func TestAllow_MalformedPermission_ReturnsInvalid(t *testing.T) {
	r, _ := queryless(t)
	ctx := asSuper(t.Context(), newPerson())
	wantKind(t, r.svc.Allow(ctx, ""), errs.Invalid)
	wantKind(t, r.svc.Allow(ctx, "Not A Permission"), errs.Invalid)
}

// F10
func TestAllow_Superuser_AllowsWithoutQuery(t *testing.T) {
	r, f := queryless(t)
	ctx := asSuper(t.Context(), newPerson())
	for _, p := range []authz.Permission{pGreetRead, pMedRead, pGrantsMgmt, pDeptGrants, pShiftManage} {
		wantNoErr(t, r.svc.Allow(ctx, p))
	}
	wantNoErr(t, r.svc.AllowIn(ctx, pShiftManage, r.d2))
	wantNoErr(t, r.svc.AllowIn(ctx, pMedRead, r.d2))
	e, err := r.svc.ExtentOf(ctx, pShiftManage)
	wantNoErr(t, err)
	if !e.Site || len(e.Departments) != 0 {
		t.Fatalf("ExtentOf for the superuser = %+v, want Site only", e)
	}
	wantNoStatements(t, f)
}

func TestAllow_SuperuserWithCancelledContext_StillAllowed(t *testing.T) {
	r, f := queryless(t)
	ctx, cancel := context.WithCancel(asSuper(t.Context(), newPerson()))
	cancel()
	wantNoErr(t, r.svc.Allow(ctx, pGreetRead))
	wantNoStatements(t, f)
}

// F11
func TestAllowIn_ZeroDepartment_ReturnsInvalid(t *testing.T) {
	r, _ := queryless(t)
	for name, ctx := range map[string]context.Context{
		"person":    as(t.Context(), newPerson()),
		"superuser": asSuper(t.Context(), newPerson()),
	} {
		t.Run(name, func(t *testing.T) {
			wantKind(t, r.svc.AllowIn(ctx, pShiftManage, model.DepartmentID{}), errs.Invalid)
		})
	}
}

// F14
func TestPermissionsOf_OtherPerson_ReturnsPermission(t *testing.T) {
	r, _ := queryless(t)
	me, other := newPerson(), newPerson()
	_, err := r.svc.PermissionsOf(as(t.Context(), me), other)
	wantKind(t, err, errs.Permission)
	if strings.Contains(err.Error(), other.String()) || strings.Contains(err.Error(), me.String()) {
		t.Errorf("denial message names a person: %v", err)
	}
}

// F15
func TestPermissionsOf_Superuser_ListsAllDeclared(t *testing.T) {
	r, f := queryless(t)
	me := newPerson()
	got, err := r.svc.PermissionsOf(asSuper(t.Context(), me), me)
	wantNoErr(t, err)
	var want []string
	for _, d := range declared() {
		want = append(want, string(d.Name))
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("PermissionsOf(superuser) = %v, want %v", got, want)
	}
	wantNoStatements(t, f)
}

// The decision counter distinguishes outcomes (telemetry spec: bms.authz.decisions{outcome}).
func TestDecisions_CountedByOutcome_NoQueryOutcomes(t *testing.T) {
	r, _ := queryless(t)
	wantKind(t, r.svc.Allow(t.Context(), pGreetRead), errs.Unauthenticated)
	wantNoErr(t, r.svc.Allow(asSuper(t.Context(), newPerson()), pGreetRead))
	seen := map[string]bool{}
	for _, m := range r.cap.Measurements() {
		if m.Instrument == "bms.authz.decisions" {
			seen[m.Attrs["outcome"]] = true
		}
	}
	for _, o := range []string{"unauthenticated", "superuser"} {
		if !seen[o] {
			t.Errorf("no bms.authz.decisions measurement with outcome=%s (saw %v)", o, seen)
		}
	}
}

// Safe for concurrent use (spec: Concurrency and lifecycle).
func TestService_ConcurrentDecisions_RaceFree(t *testing.T) {
	r, _ := queryless(t)
	done := make(chan struct{})
	for range 16 {
		go func() {
			defer func() { done <- struct{}{} }()
			ctx := asSuper(t.Context(), newPerson())
			for range 50 {
				if err := r.svc.Allow(ctx, pGreetRead); err != nil {
					t.Error(err)
				}
				if _, err := r.svc.ExtentOf(ctx, pShiftManage); err != nil {
					t.Error(err)
				}
				if err := r.svc.AllowIn(ctx, pShiftManage, r.d1); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	for range 16 {
		<-done
	}
}

// New works against db.None and decisions for a superuser need no database at all.
func TestAllow_SuperuserAgainstNoneDB(t *testing.T) {
	r := newUnitRig(t, db.None())
	wantNoErr(t, r.svc.Allow(asSuper(t.Context(), newPerson()), pMedRead))
}
