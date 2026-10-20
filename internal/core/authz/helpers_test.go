package authz_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// Everything here is invented: people are random IDs and the permissions belong to
// pretend modules.

var epoch = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// Permissions of pretend modules, declared next to authz's own.
const (
	pGreetRead   authz.Permission = "hello.greetings.read"
	pShiftManage authz.Permission = "dept.shifts.manage"
	pShiftRead   authz.Permission = "dept.shifts.read"
	pMedRead     authz.Permission = "med.reports.read"  // sensitive
	pMedWrite    authz.Permission = "med.reports.write" // personal
	pPayApprove  authz.Permission = "fin.payments.approve"
	pPayAudit    authz.Permission = "fin.payments.audit"
	pGhost       authz.Permission = "ghost.thing.do" // never declared

	pRolesRead   authz.Permission = "access.roles.read"
	pRolesManage authz.Permission = "access.roles.manage"
	pRulesManage authz.Permission = "access.rules.manage"
	pGrantsRead  authz.Permission = "access.grants.read"
	pGrantsMgmt  authz.Permission = "access.grants.manage"
	pDeptGrants  authz.Permission = "access.department-grants.manage"
)

const (
	sigCreated signals.Name = "access.grant-created"
	sigRevoked signals.Name = "access.grant-revoked"
)

// declared is authz's own six permissions plus the pretend modules'.
func declared() []authz.PermissionDecl {
	out := append([]authz.PermissionDecl(nil), authz.Permissions...)
	return append(out,
		authz.PermissionDecl{Name: pGreetRead, Scope: authz.ScopeSite, Reaches: model.SensitivityInternal, StarterRoles: []string{authz.RoleParticipant}},
		authz.PermissionDecl{Name: pShiftManage, Scope: authz.ScopeDepartment, Reaches: model.SensitivityInternal, StarterRoles: []string{authz.RoleDepartmentAdmin}},
		authz.PermissionDecl{Name: pShiftRead, Scope: authz.ScopeDepartment, Reaches: model.SensitivityInternal},
		authz.PermissionDecl{Name: pMedRead, Scope: authz.ScopeSite, Reaches: model.SensitivitySensitive},
		authz.PermissionDecl{Name: pMedWrite, Scope: authz.ScopeSite, Reaches: model.SensitivityPersonal},
		authz.PermissionDecl{Name: pPayApprove, Scope: authz.ScopeSite, Reaches: model.SensitivityInternal},
		authz.PermissionDecl{Name: pPayAudit, Scope: authz.ScopeSite, Reaches: model.SensitivityInternal},
	)
}

// ---- contexts and actors ---------------------------------------------------------------

type actorKey struct{}

func withActor(ctx context.Context, a authz.Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

func actorFromCtx(ctx context.Context) (authz.Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(authz.Actor)
	return a, ok
}

// as returns a context whose actor is person p (not the superuser).
func as(ctx context.Context, p model.PersonID) context.Context {
	return withActor(ctx, authz.Actor{Person: p})
}

// asSuper returns a context whose actor is superuser p.
func asSuper(ctx context.Context, p model.PersonID) context.Context {
	return withActor(ctx, authz.Actor{Person: p, Superuser: true})
}

func newPerson() model.PersonID { return model.NewID[model.PersonID]() }
func newDept() model.DepartmentID {
	return model.NewID[model.DepartmentID]()
}

// ---- assertions ------------------------------------------------------------------------

func wantKind(t testing.TB, err error, kind errs.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("got nil error, want kind %v", kind)
	}
	if !errs.Is(err, kind) {
		t.Fatalf("kind = %v (%v), want %v", errs.KindOf(err), err, kind)
	}
}

func wantNoErr(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---- the unit-level service ------------------------------------------------------------

// failBus wraps a signals bus so a test can make Publish fail.
type failBus struct {
	*signalstest.Fake
	mu  sync.Mutex
	err error
}

func (b *failBus) Publish(ctx context.Context, name signals.Name, p signals.Payload) error {
	b.mu.Lock()
	err := b.err
	b.mu.Unlock()
	if err != nil {
		return err
	}
	return b.Fake.Publish(ctx, name, p) //nolint:wrapcheck // test double
}

// deptSource says which departments are active.
type deptSource struct {
	mu     sync.Mutex
	active map[model.DepartmentID]bool
	err    error
}

func (d *deptSource) Active(_ context.Context, id model.DepartmentID) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.active[id], d.err
}

// rig is a Service with a fake clock, recorder, bus and telemetry capture.
type rig struct {
	t     testing.TB
	svc   *authz.Service
	opts  authz.Options
	clk   *clock.Fake
	rec   *audittest.Fake
	bus   *failBus
	cap   *telemetrytest.Capture
	depts *deptSource
	d1    model.DepartmentID // active
	d2    model.DepartmentID // active
	dOff  model.DepartmentID // archived
	su    model.PersonID     // the superuser's person
}

func validOptions(t testing.TB, d db.DB) *rig {
	t.Helper()
	c := telemetrytest.New(t)
	clk := clock.NewFake(epoch)
	rec := audittest.NewFake("access")
	inner, err := signalstest.NewFake("access", []signals.Name{sigCreated, sigRevoked}, nil)
	wantNoErr(t, err)
	bus := &failBus{Fake: inner}
	r := &rig{t: t, clk: clk, rec: rec, bus: bus, cap: c, su: newPerson(),
		d1: newDept(), d2: newDept(), dOff: newDept()}
	r.depts = &deptSource{active: map[model.DepartmentID]bool{r.d1: true, r.d2: true}}
	r.opts = authz.Options{
		DB:          d,
		Clock:       clk,
		Logger:      c.Logger(),
		Tracer:      c.Tracer("authz"),
		Meter:       c.Meter("authz"),
		Audit:       rec,
		Signals:     bus,
		Actor:       actorFromCtx,
		Declared:    declared(),
		Departments: r.depts,
	}
	return r
}

// newUnitRig builds a service over d (db.None or a dbtest fake).
func newUnitRig(t testing.TB, d db.DB, mods ...func(*authz.Options)) *rig {
	t.Helper()
	r := validOptions(t, d)
	for _, m := range mods {
		m(&r.opts)
	}
	svc, err := authz.New(r.opts)
	if err != nil {
		t.Fatalf("authz.New: %v", err)
	}
	r.svc = svc
	return r
}

func authzPermission(s string) authz.Permission { return authz.Permission(s) }
