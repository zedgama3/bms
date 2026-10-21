//go:build integration

package authz_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// D44: the volunteer-coordinator starter role (migration 0003) and the amended Seed (F58).
// Permissions of other modules are pretend declarations here, as in the other authz tests.

const (
	pDirRead       authz.Permission = "people.directory.read"
	pContactsRead  authz.Permission = "people.contacts.read"
	pRostersRead   authz.Permission = "volunteers.rosters.read"
	pQualsRead     authz.Permission = "volunteers.qualifications.read"
	pShiftsManage2 authz.Permission = "volunteers.shifts.manage"
)

func coordinatorPerms() []authz.Permission {
	return []authz.Permission{pDirRead, pContactsRead, pRostersRead, pQualsRead}
}

// coordinatorDecls adds the four coordinator permissions (seeded into the coordinator only)
// and a shifts permission the coordinator must not get.
func coordinatorDecls(base []authz.PermissionDecl, withVolunteers bool) []authz.PermissionDecl {
	out := slices.Clone(base)
	co := []string{authz.RoleVolunteerCoordinator}
	out = append(out,
		authz.PermissionDecl{Name: pDirRead, Scope: authz.ScopeDepartment, Reaches: model.SensitivityInternal, StarterRoles: co},
		authz.PermissionDecl{Name: pContactsRead, Scope: authz.ScopeDepartment, Reaches: model.SensitivityPersonal, StarterRoles: co},
	)
	if withVolunteers {
		out = append(out,
			authz.PermissionDecl{Name: pRostersRead, Scope: authz.ScopeDepartment, Reaches: model.SensitivityInternal, StarterRoles: co},
			authz.PermissionDecl{Name: pQualsRead, Scope: authz.ScopeDepartment, Reaches: model.SensitivityPersonal, StarterRoles: co},
			authz.PermissionDecl{Name: pShiftsManage2, Scope: authz.ScopeDepartment, Reaches: model.SensitivityInternal, StarterRoles: []string{authz.RoleDepartmentAdmin}},
		)
	}
	return out
}

// newCoordRig is a migrated, unseeded rig whose service declares the coordinator's permissions.
func newCoordRig(t *testing.T) *irig {
	t.Helper()
	return newRigNoSeed(t, func(o *authz.Options) { o.Declared = coordinatorDecls(o.Declared, true) })
}

func (r *irig) ledger(key string) int {
	r.t.Helper()
	var n int
	if err := r.pool.QueryRow(ctxT(r.t), "SELECT count(*) FROM core.role_seed WHERE role_key = $1", key).Scan(&n); err != nil {
		r.t.Fatalf("ledger query: %v", err)
	}
	return n
}

func (r *irig) insertStarterRole(key string) {
	r.t.Helper()
	now := time.Now().UTC()
	_, err := r.pool.Exec(ctxT(r.t),
		`INSERT INTO core.role (id, key, name, description, scope, base, created_at, updated_at)
		 VALUES ($1, $2, '', '', 'site', false, $3, $3)`,
		[16]byte(model.NewID[authz.RoleID]()), key, now)
	if err != nil {
		r.t.Fatalf("inserting role: %v", err)
	}
}

// Migration 0003 on an empty database: the role row exists before any Seed.
func TestMigration0003_VolunteerCoordinatorRole_ExistsSiteScopedWithNoPermissions(t *testing.T) {
	r := newRigNoSeed(t)
	ro := r.roleKey(authz.RoleVolunteerCoordinator)
	if ro.Scope != authz.ScopeSite || ro.Base {
		t.Errorf("role = scope %v base %v; want a non-base site role", ro.Scope, ro.Base)
	}
	if len(ro.Permissions) != 0 {
		t.Errorf("permissions before Seed = %v, want none (Seed fills them, decision 12)", ro.Permissions)
	}
}

func TestMigration_NineStarterRoles(t *testing.T) {
	r := newRigNoSeed(t)
	roles, err := r.svc.ListRoles(r.sctx())
	wantNoErr(t, err)
	var keys []string
	for _, ro := range roles {
		if ro.Key != "" {
			keys = append(keys, ro.Key)
		}
	}
	want := []string{authz.RoleParticipant, authz.RoleAccountAdmin, authz.RoleAccessAdmin, authz.RoleDepartmentSteward,
		authz.RoleEventAdmin, authz.RoleSettingsAdmin, authz.RoleAuditReviewer, authz.RoleDepartmentAdmin, authz.RoleVolunteerCoordinator}
	slices.Sort(keys)
	slices.Sort(want)
	if !slices.Equal(keys, want) {
		t.Fatalf("starter roles = %v, want %v", keys, want)
	}
}

// The role is editable and deletable like any starter role except the base (decision 2).
func TestVolunteerCoordinator_CanBeDeleted(t *testing.T) {
	r := newRigNoSeed(t)
	ro := r.roleKey(authz.RoleVolunteerCoordinator)
	wantNoErr(t, r.svc.DeleteRole(r.sctx(), ro.ID))
	roles, err := r.svc.ListRoles(r.sctx())
	wantNoErr(t, err)
	for _, x := range roles {
		if x.Key == authz.RoleVolunteerCoordinator {
			t.Fatal("deleted starter role is still listed")
		}
	}
}

func TestSeed_VolunteerCoordinator_GetsExactlyItsDeclaredPermissions(t *testing.T) {
	r := newCoordRig(t)
	decls := r.opts.Declared
	_, err := r.svc.Seed(r.sctx(), decls)
	wantNoErr(t, err)
	ro := r.roleKey(authz.RoleVolunteerCoordinator)
	got := permNames(ro)
	want := coordinatorPerms()
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("coordinator permissions = %v, want %v", got, want)
	}
	// Spec: "nothing that manages shifts".
	if hasPerm(ro, pShiftsManage2) {
		t.Error("the coordinator holds volunteers.shifts.manage")
	}
}

// A person granted the role is allowed the four reads and refused shift management.
func TestVolunteerCoordinator_Granted_AllowsReadsAndNotShiftManagement(t *testing.T) {
	r := newCoordRig(t)
	_, err := r.svc.Seed(r.sctx(), r.opts.Declared)
	wantNoErr(t, err)
	p := r.person()
	r.give(p, r.roleKey(authz.RoleVolunteerCoordinator), model.DepartmentID{}, time.Time{})
	for _, perm := range coordinatorPerms() {
		if err := r.svc.Allow(r.pctx(p), perm); err != nil {
			t.Errorf("Allow(%s) = %v", perm, err)
		}
	}
	wantKind(t, r.svc.Allow(r.pctx(p), pShiftsManage2), errs.Permission)
	ext, err := r.svc.ExtentOf(r.pctx(p), pShiftsManage2)
	wantNoErr(t, err)
	if ext.Site || len(ext.Departments) != 0 {
		t.Errorf("shifts.manage extent = %+v, want none", ext)
	}
	// A site-wide grant of a department-scopable permission reaches every department.
	wantNoErr(t, r.svc.AllowIn(r.pctx(p), pContactsRead, r.d1))
}

// F58 (amended): a key with no role is skipped and not recorded; once a role with that key
// exists the next Seed applies it.
func TestSeed_KeyWithNoRole_AppliesAfterRoleExists(t *testing.T) {
	r := newRigNoSeed(t)
	decls := []authz.PermissionDecl{{Name: "later.thing.read", Scope: authz.ScopeSite, Reaches: model.SensitivityInternal, StarterRoles: []string{"later-role"}}}
	res, err := r.svc.Seed(r.sctx(), decls)
	wantNoErr(t, err)
	if res.Added != 0 {
		t.Fatalf("Added = %d with no such role", res.Added)
	}
	if n := r.ledger("later-role"); n != 0 {
		t.Fatalf("%d ledger rows for a pair whose role does not exist; it must not be recorded", n)
	}
	r.insertStarterRole("later-role")
	res, err = r.svc.Seed(r.sctx(), decls)
	wantNoErr(t, err)
	if res.Added != 1 {
		t.Fatalf("Added = %d after the role appeared, want 1", res.Added)
	}
	if !hasPerm(r.roleKey("later-role"), "later.thing.read") {
		t.Error("the pair was not applied once the role existed")
	}
	if n := r.ledger("later-role"); n != 1 {
		t.Errorf("ledger rows = %d after applying, want 1", n)
	}
	// Recorded now: idempotent again.
	res, err = r.svc.Seed(r.sctx(), decls)
	wantNoErr(t, err)
	if res.Added != 0 {
		t.Errorf("third Seed added %d", res.Added)
	}
}

// The case D44 exists for: volunteers lists the key before the role does.
func TestSeed_CoordinatorPairBeforeRole_AppliedAtNextSeedAndAudited(t *testing.T) {
	r := newCoordRig(t)
	ro := r.roleKey(authz.RoleVolunteerCoordinator)
	wantNoErr(t, r.svc.DeleteRole(r.sctx(), ro.ID)) // as if migration 0003 had not run yet
	decls := r.opts.Declared
	_, err := r.svc.Seed(r.sctx(), decls)
	wantNoErr(t, err)
	before := len(r.entries("access.role.changed"))
	if n := r.ledger(authz.RoleVolunteerCoordinator); n != 0 {
		t.Fatalf("%d ledger rows recorded while the role did not exist", n)
	}
	// Other starter roles are unaffected by the skip.
	r.insertStarterRole(authz.RoleVolunteerCoordinator) // "bms migrate" ran 0003
	res, err := r.svc.Seed(r.sctx(), decls)
	wantNoErr(t, err)
	if res.Added != len(coordinatorPerms()) {
		t.Fatalf("Added = %d, want %d", res.Added, len(coordinatorPerms()))
	}
	got := permNames(r.roleKey(authz.RoleVolunteerCoordinator))
	want := coordinatorPerms()
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("permissions = %v, want %v", got, want)
	}
	es := r.entries("access.role.changed")[before:]
	if len(es) != len(coordinatorPerms()) {
		t.Fatalf("%d role.changed entries, want one per applied permission (%d)", len(es), len(coordinatorPerms()))
	}
	for _, e := range es {
		if e.Actor.Kind != audit.ActorOperator || e.Record.Kind != "access.role" || !strings.HasPrefix(e.Reason, "seed:") {
			t.Errorf("entry = %+v", e)
		}
	}
	if n := r.ledger(authz.RoleVolunteerCoordinator); n != len(coordinatorPerms()) {
		t.Errorf("ledger rows = %d, want %d", n, len(coordinatorPerms()))
	}
}

// Skipping one pair does not stop the others in the same declaration or run.
func TestSeed_KeyWithNoRole_DoesNotBlockOtherPairs(t *testing.T) {
	r := newRigNoSeed(t)
	decls := []authz.PermissionDecl{{Name: "mixed.thing.read", Scope: authz.ScopeSite, Reaches: model.SensitivityInternal,
		StarterRoles: []string{"nobody-yet", authz.RoleEventAdmin}}}
	res, err := r.svc.Seed(r.sctx(), decls)
	wantNoErr(t, err)
	if res.Added != 1 || !hasPerm(r.roleKey(authz.RoleEventAdmin), "mixed.thing.read") {
		t.Fatalf("Added = %d; event-admin pair not applied", res.Added)
	}
}

// Once applied, a regional's edit is never undone, even though a later Seed retries
// unrecorded pairs.
func TestSeed_AfterRetry_NeverUndoesLaterEditToCoordinator(t *testing.T) {
	r := newCoordRig(t)
	decls := r.opts.Declared
	_, err := r.svc.Seed(r.sctx(), decls)
	wantNoErr(t, err)
	ro := r.roleKey(authz.RoleVolunteerCoordinator)
	keep := slices.DeleteFunc(permNames(ro), func(p authz.Permission) bool { return p == pQualsRead })
	_, err = r.svc.UpdateRole(r.sctx(), ro.ID, authz.RoleInput{Name: "Coordinator", Permissions: keep})
	wantNoErr(t, err)
	res, err := r.svc.Seed(r.sctx(), decls)
	wantNoErr(t, err)
	if res.Added != 0 || hasPerm(r.roleKey(authz.RoleVolunteerCoordinator), pQualsRead) {
		t.Errorf("Added = %d; Seed undid a regional's edit", res.Added)
	}
}

// A coordinator role the regional deleted after its pairs were recorded stays deleted.
func TestSeed_DeletedCoordinatorAfterSeeding_DoesNotReturn(t *testing.T) {
	r := newCoordRig(t)
	decls := r.opts.Declared
	_, err := r.svc.Seed(r.sctx(), decls)
	wantNoErr(t, err)
	wantNoErr(t, r.svc.DeleteRole(r.sctx(), r.roleKey(authz.RoleVolunteerCoordinator).ID))
	res, err := r.svc.Seed(r.sctx(), decls)
	wantNoErr(t, err)
	if res.Added != 0 {
		t.Errorf("Added = %d for a deleted starter role", res.Added)
	}
	roles, err := r.svc.ListRoles(r.sctx())
	wantNoErr(t, err)
	for _, x := range roles {
		if x.Key == authz.RoleVolunteerCoordinator {
			t.Fatal("Seed brought the deleted role back")
		}
	}
}
