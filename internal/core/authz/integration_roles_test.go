//go:build integration

package authz_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func permNames(ro authz.Role) []authz.Permission {
	var out []authz.Permission
	for _, rp := range ro.Permissions {
		out = append(out, rp.Name)
	}
	return out
}

// ---- catalogue -------------------------------------------------------------------------

func TestListPermissions_SortedByName_WithScopeAndReaches(t *testing.T) {
	r := newRig(t)
	got, err := r.svc.ListPermissions(r.sctx())
	wantNoErr(t, err)
	if len(got) != len(declared()) {
		t.Fatalf("%d permissions, want %d", len(got), len(declared()))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Name >= got[i].Name {
			t.Fatalf("not sorted by name: %s before %s", got[i-1].Name, got[i].Name)
		}
	}
	for _, in := range got {
		if in.Name == pMedRead {
			if in.Reaches != model.SensitivitySensitive || in.Scope != authz.ScopeSite || in.Module() != "med" {
				t.Errorf("catalogue row for %s = %+v (module %q)", in.Name, in.PermissionDecl, in.Module())
			}
		}
	}
}

// F17
func TestListPermissions_NoPermission_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.ListPermissions(r.pctx(r.person()))
	wantKind(t, err, errs.Permission)
	_, err = r.svc.ListPermissions(ctxT(t))
	wantKind(t, err, errs.Unauthenticated)
}

func TestListPermissions_RolesRead_SeesAll(t *testing.T) {
	r := newRig(t)
	p := r.holder(pRolesRead)
	got, err := r.svc.ListPermissions(r.pctx(p))
	wantNoErr(t, err)
	if len(got) != len(declared()) {
		t.Fatalf("%d permissions, want %d", len(got), len(declared()))
	}
}

func TestListPermissions_DepartmentCaller_SeesDepartmentScopePermissionsOnly(t *testing.T) {
	r := newRig(t)
	p := r.deptHolder(r.d1, pGrantsRead)
	got, err := r.svc.ListPermissions(r.pctx(p))
	wantNoErr(t, err)
	if len(got) == 0 {
		t.Fatal("nothing listed")
	}
	for _, in := range got {
		if in.Scope != authz.ScopeDepartment {
			t.Errorf("department caller sees %s permission %s", in.Scope, in.Name)
		}
	}
}

func TestListPermissions_GrantsPermissionHolders_AreAllowed(t *testing.T) {
	r := newRig(t)
	for name, p := range map[string]model.PersonID{
		"grants.manage@D":       r.deptHolder(r.d1, pGrantsMgmt),
		"grants.read":           r.holder(pGrantsRead),
		"department-grants.mgt": r.holder(pDeptGrants),
	} {
		_, err := r.svc.ListPermissions(r.pctx(p))
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// ---- reading roles ---------------------------------------------------------------------

func TestListRoles_AfterMigrate_NineStarterRoles(t *testing.T) {
	r := newRigNoSeed(t)
	roles, err := r.svc.ListRoles(r.sctx())
	wantNoErr(t, err)
	want := map[string]struct {
		scope authz.Scope
		base  bool
	}{
		authz.RoleParticipant: {authz.ScopeSite, true}, authz.RoleAccountAdmin: {authz.ScopeSite, false},
		authz.RoleAccessAdmin: {authz.ScopeSite, false}, authz.RoleDepartmentSteward: {authz.ScopeSite, false},
		authz.RoleEventAdmin: {authz.ScopeSite, false}, authz.RoleSettingsAdmin: {authz.ScopeSite, false},
		authz.RoleAuditReviewer: {authz.ScopeSite, false}, authz.RoleDepartmentAdmin: {authz.ScopeDepartment, false},
		authz.RoleVolunteerCoordinator: {authz.ScopeSite, false}, // D44, migration 0003
	}
	if len(roles) != len(want) {
		t.Fatalf("%d roles, want %d", len(roles), len(want))
	}
	for _, ro := range roles {
		w, ok := want[ro.Key]
		if !ok {
			t.Errorf("unexpected role key %q", ro.Key)
			continue
		}
		if ro.Scope != w.scope || ro.Base != w.base || ro.Name != "" || len(ro.Permissions) != 0 || ro.ID.IsZero() {
			t.Errorf("role %s = %+v", ro.Key, ro)
		}
	}
}

// F17
func TestListRoles_NoPermission_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.ListRoles(r.pctx(r.person()))
	wantKind(t, err, errs.Permission)
	_, err = r.svc.ListRoles(ctxT(t))
	wantKind(t, err, errs.Unauthenticated)
}

func TestListRoles_Sorted_And_CarryDeclaredFlags(t *testing.T) {
	r := newRig(t)
	for _, n := range []string{"Zebra wrangler", "alpha lead", "Midway marshal"} {
		_, err := r.svc.CreateRole(r.sctx(), authz.RoleInput{Name: n, Scope: authz.ScopeSite, Permissions: []authz.Permission{pMedRead, pGhost}})
		wantNoErr(t, err)
	}
	roles, err := r.svc.ListRoles(r.sctx())
	wantNoErr(t, err)
	var named []string
	for _, ro := range roles {
		if ro.Name != "" {
			named = append(named, strings.ToLower(ro.Name))
			if len(ro.Permissions) != 2 {
				t.Errorf("%s has %d permissions", ro.Name, len(ro.Permissions))
			}
			if !slices.IsSortedFunc(ro.Permissions, func(a, b authz.RolePermission) int { return strings.Compare(string(a.Name), string(b.Name)) }) {
				t.Errorf("%s permissions not sorted: %v", ro.Name, ro.Permissions)
			}
		}
	}
	if !slices.IsSorted(named) {
		t.Errorf("named roles not sorted by name: %v", named)
	}
}

// F18
func TestListRoles_DepartmentCaller_SeesDepartmentRolesOnly(t *testing.T) {
	r := newRig(t)
	r.mkRole(authz.ScopeDepartment, pShiftRead)
	r.mkRole(authz.ScopeSite, pMedRead)
	p := r.deptHolder(r.d1, pGrantsRead)
	roles, err := r.svc.ListRoles(r.pctx(p))
	wantNoErr(t, err)
	if len(roles) == 0 {
		t.Fatal("no roles")
	}
	for _, ro := range roles {
		if ro.Scope != authz.ScopeDepartment {
			t.Errorf("department caller sees site role %q/%q", ro.Key, ro.Name)
		}
	}
}

// F19
func TestGetRole_OutsideExtent_ReturnsNotFound(t *testing.T) {
	r := newRig(t)
	siteRole := r.mkRole(authz.ScopeSite, pMedRead)
	deptRole := r.mkRole(authz.ScopeDepartment, pShiftRead)
	p := r.deptHolder(r.d1, pGrantsRead)
	_, err := r.svc.GetRole(r.pctx(p), siteRole.ID)
	wantKind(t, err, errs.NotFound)
	got, err := r.svc.GetRole(r.pctx(p), deptRole.ID)
	wantNoErr(t, err)
	if got.ID != deptRole.ID {
		t.Error("wrong role")
	}
	_, err = r.svc.GetRole(r.sctx(), model.NewID[authz.RoleID]())
	wantKind(t, err, errs.NotFound)
}

func TestGetRole_NoPermission_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	_, err := r.svc.GetRole(r.pctx(r.person()), ro.ID)
	wantKind(t, err, errs.Permission)
}

// ---- create ----------------------------------------------------------------------------

// F20
func TestCreateRole_NoPermission_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	in := authz.RoleInput{Name: "Nope", Scope: authz.ScopeSite}
	callers := []model.PersonID{r.person(), r.holder(pRolesRead), r.deptHolder(r.d1, pGrantsMgmt)}
	before := len(r.entries("access.role.created"))
	_, err := r.svc.CreateRole(r.pctx(callers[0]), in)
	wantKind(t, err, errs.Permission)
	_, err = r.svc.CreateRole(r.pctx(callers[1]), in) // read is not manage
	wantKind(t, err, errs.Permission)
	_, err = r.svc.CreateRole(r.pctx(callers[2]), in) // not site-wide
	wantKind(t, err, errs.Permission)
	_, err = r.svc.CreateRole(ctxT(t), in)
	wantKind(t, err, errs.Unauthenticated)
	if len(r.entries("access.role.created")) != before {
		t.Error("a refused create was audited")
	}
}

func TestCreateRole_Success_ReturnsRoleAndAudits(t *testing.T) {
	r := newRig(t)
	p := r.holder(pRolesManage)
	before := len(r.entries("access.role.created"))
	ro, err := r.svc.CreateRole(r.pctx(p), authz.RoleInput{
		Name: "  Gate Coordinator  ", Description: "Runs the gate", Scope: authz.ScopeSite,
		Permissions: []authz.Permission{pMedRead, pPayAudit}, // returned sorted by name
	})
	wantNoErr(t, err)
	if ro.ID.IsZero() || ro.Key != "" || ro.Base || ro.Scope != authz.ScopeSite {
		t.Errorf("role = %+v", ro)
	}
	if ro.Name != "Gate Coordinator" {
		t.Errorf("Name = %q, want trimmed", ro.Name)
	}
	if !ro.CreatedAt.Equal(r.clk.Now()) || !ro.UpdatedAt.Equal(r.clk.Now()) {
		t.Errorf("times = %v / %v, want the clock's now", ro.CreatedAt, ro.UpdatedAt)
	}
	if !slices.Equal(permNames(ro), []authz.Permission{pPayAudit, pMedRead}) {
		t.Errorf("permissions = %v, want sorted by name", permNames(ro))
	}
	es := r.entries("access.role.created")
	if len(es) != before+1 {
		t.Fatalf("%d access.role.created entries, want %d", len(es), before+1)
	}
	if es[0].Actor.Person != p && es[0].Actor.Kind != 0 {
		t.Errorf("entry actor = %+v", es[0].Actor)
	}
	got, err := r.svc.GetRole(r.sctx(), ro.ID)
	wantNoErr(t, err)
	if got.Name != ro.Name || got.Description != "Runs the gate" {
		t.Errorf("stored role = %+v", got)
	}
}

// F21
func TestCreateRole_BadName_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	tests := []struct {
		name  string
		in    authz.RoleInput
		field string
		code  string
	}{
		{"empty", authz.RoleInput{Scope: authz.ScopeSite}, "name", "required"},
		{"blank", authz.RoleInput{Name: "   ", Scope: authz.ScopeSite}, "name", "required"},
		{"too long", authz.RoleInput{Name: strings.Repeat("n", authz.MaxRoleNameBytes+1), Scope: authz.ScopeSite}, "name", "too_long"},
		{"control character", authz.RoleInput{Name: "Bad\x07Name", Scope: authz.ScopeSite}, "name", "malformed"},
		{"newline", authz.RoleInput{Name: "Two\nLines", Scope: authz.ScopeSite}, "name", "malformed"},
		{"description too long", authz.RoleInput{Name: "Fine", Description: strings.Repeat("d", authz.MaxDescriptionBytes+1), Scope: authz.ScopeSite}, "description", "too_long"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.svc.CreateRole(r.sctx(), tc.in)
			wantField(t, err, tc.field, tc.code)
		})
	}
}

func TestCreateRole_NameAndDescriptionAtTheLimit_Accepted(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.CreateRole(r.sctx(), authz.RoleInput{
		Name: strings.Repeat("n", authz.MaxRoleNameBytes), Description: strings.Repeat("d", authz.MaxDescriptionBytes), Scope: authz.ScopeSite,
	})
	wantNoErr(t, err)
}

// F22
func TestCreateRole_BadPermissions_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	many := make([]authz.Permission, authz.MaxRolePermissions+1)
	for i := range many {
		many[i] = authz.Permission("bulk.perm-" + strings.Repeat("a", 1) + string(rune('a'+i/26%26)) + string(rune('a'+i%26)) + ".do")
	}
	tests := []struct {
		name  string
		perms []authz.Permission
		field string
		code  string
	}{
		{"malformed", []authz.Permission{pMedRead, "Not A Permission"}, "permissions[1]", "malformed"},
		{"single segment", []authz.Permission{"nodots"}, "permissions[0]", "malformed"},
		{"repeated", []authz.Permission{pMedRead, pPayAudit, pMedRead}, "permissions[2]", "duplicate"},
		{"over the limit", many, "permissions", "too_long"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.svc.CreateRole(r.sctx(), authz.RoleInput{Name: r.name("Perms"), Scope: authz.ScopeSite, Permissions: tc.perms})
			wantField(t, err, tc.field, tc.code)
		})
	}
}

func TestCreateRole_ExactlyMaxPermissions_Accepted(t *testing.T) {
	r := newRig(t)
	perms := make([]authz.Permission, authz.MaxRolePermissions)
	for i := range perms {
		perms[i] = authz.Permission("bulk." + string(rune('a'+i/26)) + string(rune('a'+i%26)))
	}
	_, err := r.svc.CreateRole(r.sctx(), authz.RoleInput{Name: "Everything and more", Scope: authz.ScopeSite, Permissions: perms})
	wantNoErr(t, err)
}

// F23
func TestCreateRole_SitePermissionInDepartmentRole_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.CreateRole(r.sctx(), authz.RoleInput{Name: r.name("Dept"), Scope: authz.ScopeDepartment, Permissions: []authz.Permission{pShiftRead, pMedRead}})
	wantField(t, err, "permissions[1]", "scope")
	// An undeclared name has unknown scope and is allowed (decision 13).
	_, err = r.svc.CreateRole(r.sctx(), authz.RoleInput{Name: r.name("Dept"), Scope: authz.ScopeDepartment, Permissions: []authz.Permission{pShiftRead, pGhost}})
	wantNoErr(t, err)
}

func TestCreateRole_BadScope_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.CreateRole(r.sctx(), authz.RoleInput{Name: r.name("Zero"), Scope: 0})
	wantKind(t, err, errs.Invalid)
	_, err = r.svc.CreateRole(r.sctx(), authz.RoleInput{Name: r.name("Nine"), Scope: 9})
	wantKind(t, err, errs.Invalid)
}

// F24
func TestCreateRole_NameTaken_ReturnsConflict(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.CreateRole(r.sctx(), authz.RoleInput{Name: "Gate Crew", Scope: authz.ScopeSite})
	wantNoErr(t, err)
	for _, n := range []string{"Gate Crew", "gate crew", "GATE CREW"} {
		_, err = r.svc.CreateRole(r.sctx(), authz.RoleInput{Name: n, Scope: authz.ScopeDepartment})
		wantCode(t, err, errs.Conflict, "taken")
	}
}

// F25
func TestCreateRole_ReadOnly_ReturnsReadOnly(t *testing.T) {
	r := newRig(t)
	r.fdb.set(func(f *faultDB) { f.ro = true })
	_, err := r.svc.CreateRole(r.sctx(), authz.RoleInput{Name: "Read only", Scope: authz.ScopeSite})
	wantReadOnly(t, err)
	// Reads still work.
	_, err = r.svc.ListRoles(r.sctx())
	wantNoErr(t, err)
}

func TestRoles_StoreFails_ReturnsStoreKind(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	p := r.holder(pRolesRead, pRolesManage)
	cases := map[string]error{"down": errDown, "timeout": errTimeout}
	for name, injected := range cases {
		t.Run(name, func(t *testing.T) {
			r.fdb.set(func(f *faultDB) { f.readErr, f.txErr = injected, injected })
			t.Cleanup(func() { r.fdb.set(func(f *faultDB) { f.readErr, f.txErr = nil, nil }) })
			want := errs.KindOf(injected)
			_, err := r.svc.ListRoles(r.pctx(p))
			wantKind(t, err, want)
			_, err = r.svc.GetRole(r.pctx(p), ro.ID)
			wantKind(t, err, want)
			_, err = r.svc.CreateRole(r.pctx(p), authz.RoleInput{Name: "x1", Scope: authz.ScopeSite})
			wantKind(t, err, want)
			wantKind(t, r.svc.DeleteRole(r.pctx(p), ro.ID), want)
		})
	}
}

func TestRoles_CancelledContext_ReturnsCanceled(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(r.sctx())
	cancel()
	_, err := r.svc.ListRoles(ctx)
	wantKind(t, err, errs.Canceled)
}

// ---- update ----------------------------------------------------------------------------

func TestUpdateRole_ReplacesNameDescriptionAndPermissions(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead, pPayAudit)
	seeded := len(r.entries("access.role.changed")) // Seed records one per starter permission (D37)
	wantNoErr(t, r.clk.Advance(time.Hour))
	got, err := r.svc.UpdateRole(r.sctx(), ro.ID, authz.RoleInput{Name: "Renamed", Description: "New", Permissions: []authz.Permission{pPayApprove}})
	wantNoErr(t, err)
	if got.Name != "Renamed" || got.Description != "New" || !slices.Equal(permNames(got), []authz.Permission{pPayApprove}) {
		t.Fatalf("updated role = %+v", got)
	}
	if got.Scope != authz.ScopeSite || got.ID != ro.ID || !got.CreatedAt.Equal(ro.CreatedAt) || !got.UpdatedAt.Equal(r.clk.Now()) {
		t.Fatalf("immutable or time fields wrong: %+v", got)
	}
	if len(r.entries("access.role.changed")) != seeded+1 {
		t.Errorf("actions = %v, want one new access.role.changed", r.actions())
	}
}

func TestUpdateRole_NoPermission_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	_, err := r.svc.UpdateRole(r.pctx(r.holder(pRolesRead)), ro.ID, authz.RoleInput{Name: "x2"})
	wantKind(t, err, errs.Permission)
}

// F26 (Types says Scope is "ignored by UpdateRole"; F26 says a change is Invalid. The
// test follows F26.)
func TestUpdateRole_ScopeChange_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	_, err := r.svc.UpdateRole(r.sctx(), ro.ID, authz.RoleInput{Name: ro.Name, Scope: authz.ScopeDepartment, Permissions: []authz.Permission{pMedRead}})
	wantField(t, err, "scope", "immutable")
	got, err := r.svc.GetRole(r.sctx(), ro.ID)
	wantNoErr(t, err)
	if got.Scope != authz.ScopeSite {
		t.Error("scope changed")
	}
}

func TestUpdateRole_SitePermissionInDepartmentRole_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeDepartment, pShiftRead)
	_, err := r.svc.UpdateRole(r.sctx(), ro.ID, authz.RoleInput{Name: ro.Name, Permissions: []authz.Permission{pShiftRead, pMedRead}})
	wantField(t, err, "permissions[1]", "scope")
}

func TestUpdateRole_NameTaken_ReturnsConflict(t *testing.T) {
	r := newRig(t)
	a := r.mkRole(authz.ScopeSite)
	b := r.mkRole(authz.ScopeSite)
	_, err := r.svc.UpdateRole(r.sctx(), b.ID, authz.RoleInput{Name: strings.ToUpper(a.Name)})
	wantCode(t, err, errs.Conflict, "taken")
	// Keeping its own name is fine.
	_, err = r.svc.UpdateRole(r.sctx(), b.ID, authz.RoleInput{Name: b.Name, Description: "same name"})
	wantNoErr(t, err)
}

func TestUpdateRole_BadInput_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite)
	_, err := r.svc.UpdateRole(r.sctx(), ro.ID, authz.RoleInput{Name: ""})
	wantField(t, err, "name", "required")
	_, err = r.svc.UpdateRole(r.sctx(), ro.ID, authz.RoleInput{Name: "ok", Permissions: []authz.Permission{pMedRead, pMedRead}})
	wantField(t, err, "permissions[1]", "duplicate")
}

// F28
func TestUpdateRole_Unknown_ReturnsNotFound(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.UpdateRole(r.sctx(), model.NewID[authz.RoleID](), authz.RoleInput{Name: "ghost"})
	wantKind(t, err, errs.NotFound)
}

// Rename of a starter role sets Name and keeps Key.
func TestUpdateRole_StarterRole_RenameKeepsKey(t *testing.T) {
	r := newRig(t)
	ro := r.roleKey(authz.RoleEventAdmin)
	got, err := r.svc.UpdateRole(r.sctx(), ro.ID, authz.RoleInput{Name: "Event Czar", Permissions: permNames(ro)})
	wantNoErr(t, err)
	if got.Key != authz.RoleEventAdmin || got.Name != "Event Czar" {
		t.Fatalf("renamed starter role = %+v", got)
	}
}

// The base role is editable.
func TestUpdateRole_BaseRole_IsEditableButStaysBase(t *testing.T) {
	r := newRig(t)
	base := r.roleKey(authz.RoleParticipant)
	got, err := r.svc.UpdateRole(r.sctx(), base.ID, authz.RoleInput{Name: "Everyone", Permissions: []authz.Permission{pGreetRead, pShiftRead}})
	wantNoErr(t, err)
	if !got.Base || got.Scope != authz.ScopeSite || got.Key != authz.RoleParticipant {
		t.Fatalf("base role after edit = %+v", got)
	}
	// A person with no grant gains the new permission at once... but a department
	// permission in the base role has no department, so only site entries apply.
	wantNoErr(t, r.svc.Allow(r.pctx(r.person()), pGreetRead))
}

// F27
func TestUpdateRole_RemovesLastAccessAdmin_ReturnsConflict(t *testing.T) {
	r := newRig(t)
	admin := r.admin()
	ro := r.roleKey(authz.RoleAccessAdmin)
	keep := slices.DeleteFunc(permNames(ro), func(p authz.Permission) bool { return p == pGrantsMgmt })
	_, err := r.svc.UpdateRole(r.pctx(admin), ro.ID, authz.RoleInput{Name: "Access", Permissions: keep})
	wantCode(t, err, errs.Conflict, "last_access_admin")
	got, err := r.svc.GetRole(r.sctx(), ro.ID)
	wantNoErr(t, err)
	if !hasPerm(got, pGrantsMgmt) {
		t.Fatal("the permission was removed despite the conflict")
	}
	// The superuser is not exempt from the invariant on role edits.
	_, err = r.svc.UpdateRole(r.sctx(), ro.ID, authz.RoleInput{Name: "Access", Permissions: keep})
	wantCode(t, err, errs.Conflict, "last_access_admin")
}

// F27 (D37): another holder a separation-of-duties rule denies the permission does not count.
func TestUpdateRole_RemovesLastAdminDeniedByRule_ReturnsConflict(t *testing.T) {
	r := newRig(t)
	r.admin()
	other := r.holder(pGrantsMgmt)
	barred := r.mkRole(authz.ScopeSite, pMedRead)
	r.give(other, barred, model.DepartmentID{}, time.Time{})
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: barred.ID, Permission: pGrantsMgmt})
	wantNoErr(t, err) // the first admin remains effective
	ro := r.roleKey(authz.RoleAccessAdmin)
	keep := slices.DeleteFunc(permNames(ro), func(p authz.Permission) bool { return p == pGrantsMgmt })
	_, err = r.svc.UpdateRole(r.sctx(), ro.ID, authz.RoleInput{Name: "Access", Permissions: keep})
	wantCode(t, err, errs.Conflict, "last_access_admin")
}

func TestUpdateRole_RemovesAccessAdminPermission_AllowedWhenAnotherHolderRemains(t *testing.T) {
	r := newRig(t)
	r.admin()
	other := r.holder(pGrantsMgmt)
	ro := r.roleKey(authz.RoleAccessAdmin)
	keep := slices.DeleteFunc(permNames(ro), func(p authz.Permission) bool { return p == pGrantsMgmt })
	_, err := r.svc.UpdateRole(r.sctx(), ro.ID, authz.RoleInput{Name: "Access", Permissions: keep})
	wantNoErr(t, err)
	wantNoErr(t, r.svc.Allow(r.pctx(other), pGrantsMgmt))
}

func TestUpdateRole_EndedOtherHolderDoesNotCount(t *testing.T) {
	r := newRig(t)
	r.admin()
	ender := r.person()
	r.give(ender, r.mkRole(authz.ScopeSite, pGrantsMgmt), model.DepartmentID{}, r.clk.Now().Add(time.Hour))
	wantNoErr(t, r.clk.Advance(2*time.Hour))
	ro := r.roleKey(authz.RoleAccessAdmin)
	keep := slices.DeleteFunc(permNames(ro), func(p authz.Permission) bool { return p == pGrantsMgmt })
	_, err := r.svc.UpdateRole(r.sctx(), ro.ID, authz.RoleInput{Name: "Access", Permissions: keep})
	wantCode(t, err, errs.Conflict, "last_access_admin")
}

// ---- delete ----------------------------------------------------------------------------

func TestDeleteRole_Success_Audits(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	wantNoErr(t, r.svc.DeleteRole(r.sctx(), ro.ID))
	_, err := r.svc.GetRole(r.sctx(), ro.ID)
	wantKind(t, err, errs.NotFound)
	if len(r.entries("access.role.deleted")) != 1 {
		t.Errorf("actions = %v", r.actions())
	}
	wantKind(t, r.svc.DeleteRole(r.sctx(), ro.ID), errs.NotFound) // not idempotent: NotFound
}

func TestDeleteRole_NoPermission_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite)
	wantKind(t, r.svc.DeleteRole(r.pctx(r.holder(pRolesRead)), ro.ID), errs.Permission)
	wantKind(t, r.svc.DeleteRole(ctxT(t), ro.ID), errs.Unauthenticated)
}

// F29
func TestDeleteRole_InUse_ReturnsConflict(t *testing.T) {
	r := newRig(t)
	t.Run("active grant", func(t *testing.T) {
		ro := r.mkRole(authz.ScopeSite, pMedRead)
		r.give(r.person(), ro, model.DepartmentID{}, time.Time{})
		wantCode(t, r.svc.DeleteRole(r.sctx(), ro.ID), errs.Conflict, "in_use")
	})
	t.Run("ended grant", func(t *testing.T) {
		ro := r.mkRole(authz.ScopeSite, pMedRead)
		r.give(r.person(), ro, model.DepartmentID{}, r.clk.Now().Add(time.Minute))
		wantNoErr(t, r.clk.Advance(time.Hour))
		wantCode(t, r.svc.DeleteRole(r.sctx(), ro.ID), errs.Conflict, "in_use")
	})
	t.Run("rule", func(t *testing.T) {
		ro := r.mkRole(authz.ScopeSite, pMedRead)
		_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: ro.ID, Permission: pPayAudit})
		wantNoErr(t, err)
		wantCode(t, r.svc.DeleteRole(r.sctx(), ro.ID), errs.Conflict, "in_use")
	})
}

// F30
func TestDeleteRole_Base_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	base := r.roleKey(authz.RoleParticipant)
	wantField(t, r.svc.DeleteRole(r.sctx(), base.ID), "role", "base")
}

// decision 2 (D36): every starter role but the base role may be deleted.
func TestDeleteRole_StarterRoles_ExceptBase_MayBeDeleted(t *testing.T) {
	r := newRig(t)
	for _, key := range []string{authz.RoleAccountAdmin, authz.RoleDepartmentSteward, authz.RoleEventAdmin, authz.RoleSettingsAdmin, authz.RoleAuditReviewer, authz.RoleDepartmentAdmin, authz.RoleAccessAdmin, authz.RoleVolunteerCoordinator} {
		ro := r.roleKey(key)
		wantNoErr(t, r.svc.DeleteRole(r.sctx(), ro.ID))
	}
	roles, err := r.svc.ListRoles(r.sctx())
	wantNoErr(t, err)
	if len(roles) != 1 || !roles[0].Base {
		t.Fatalf("roles left = %+v, want only the base role", roles)
	}
}

func TestDeleteRole_StarterRole_StaysDeletedAfterSeed(t *testing.T) {
	r := newRig(t)
	ro := r.roleKey(authz.RoleEventAdmin)
	wantNoErr(t, r.svc.DeleteRole(r.sctx(), ro.ID))
	res, err := r.svc.Seed(r.sctx(), r.opts.Declared)
	wantNoErr(t, err)
	if res.Added != 0 {
		t.Errorf("Added = %d, want 0", res.Added)
	}
	roles, err := r.svc.ListRoles(r.sctx())
	wantNoErr(t, err)
	for _, x := range roles {
		if x.Key == authz.RoleEventAdmin {
			t.Fatal("deleted starter role came back")
		}
	}
}
