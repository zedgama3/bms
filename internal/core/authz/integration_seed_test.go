//go:build integration

package authz_test

import (
	"slices"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// expectedMembership counts the (role key, permission) pairs the declarations name.
func expectedMembership(decls []authz.PermissionDecl, haveRole func(string) bool) (pairs int, byKey map[string][]authz.Permission) {
	byKey = map[string][]authz.Permission{}
	for _, d := range decls {
		for _, k := range d.StarterRoles {
			if haveRole(k) {
				byKey[k] = append(byKey[k], d.Name)
				pairs++
			}
		}
	}
	return pairs, byKey
}

func starterKeys() map[string]bool {
	m := map[string]bool{}
	for _, k := range []string{authz.RoleParticipant, authz.RoleAccountAdmin, authz.RoleAccessAdmin, authz.RoleDepartmentSteward, authz.RoleEventAdmin, authz.RoleSettingsAdmin, authz.RoleAuditReviewer, authz.RoleDepartmentAdmin} {
		m[k] = true
	}
	return m
}

func TestSeed_FillsStarterRolesOnce(t *testing.T) {
	r := newRigNoSeed(t)
	keys := starterKeys()
	wantPairs, byKey := expectedMembership(r.opts.Declared, func(k string) bool { return keys[k] })
	res, err := r.svc.Seed(r.sctx(), r.opts.Declared)
	wantNoErr(t, err)
	if res.Added != wantPairs {
		t.Fatalf("Added = %d, want %d", res.Added, wantPairs)
	}
	for key, perms := range byKey {
		ro := r.roleKey(key)
		got := permNames(ro)
		slices.Sort(perms)
		if !slices.Equal(got, perms) {
			t.Errorf("%s permissions = %v, want %v", key, got, perms)
		}
	}
	// Spec table: access-admin holds the five access permissions (not department-grants.manage).
	aa := r.roleKey(authz.RoleAccessAdmin)
	for _, p := range []authz.Permission{pRolesRead, pRolesManage, pRulesManage, pGrantsRead, pGrantsMgmt} {
		if !hasPerm(aa, p) {
			t.Errorf("access-admin lacks %s", p)
		}
	}
	if hasPerm(aa, pDeptGrants) {
		t.Error("access-admin holds department-grants.manage")
	}
	if !hasPerm(r.roleKey(authz.RoleDepartmentSteward), pDeptGrants) {
		t.Error("department-steward lacks department-grants.manage")
	}
	da := r.roleKey(authz.RoleDepartmentAdmin)
	if !hasPerm(da, pGrantsRead) || !hasPerm(da, pGrantsMgmt) {
		t.Error("department-admin lacks access.grants.read/manage")
	}
}

// F58
func TestSeed_Twice_AddsNothing(t *testing.T) {
	r := newRigNoSeed(t)
	_, err := r.svc.Seed(r.sctx(), r.opts.Declared)
	wantNoErr(t, err)
	res, err := r.svc.Seed(r.sctx(), r.opts.Declared)
	wantNoErr(t, err)
	if res.Added != 0 {
		t.Fatalf("second Seed added %d", res.Added)
	}
}

func TestSeed_NeverUndoesLaterEdits(t *testing.T) {
	r := newRig(t)
	aa := r.roleKey(authz.RoleAccessAdmin)
	keep := slices.DeleteFunc(permNames(aa), func(p authz.Permission) bool { return p == pRolesManage })
	_, err := r.svc.UpdateRole(r.sctx(), aa.ID, authz.RoleInput{Name: "Access", Permissions: keep})
	wantNoErr(t, err)
	res, err := r.svc.Seed(r.sctx(), r.opts.Declared)
	wantNoErr(t, err)
	if res.Added != 0 {
		t.Errorf("Added = %d", res.Added)
	}
	if hasPerm(r.roleKey(authz.RoleAccessAdmin), pRolesManage) {
		t.Error("Seed re-added a permission a regional removed")
	}
}

func TestSeed_NewDeclaration_AddsOnlyTheNewPair(t *testing.T) {
	r := newRig(t)
	more := append(append([]authz.PermissionDecl(nil), r.opts.Declared...),
		authz.PermissionDecl{Name: "later.thing.read", Scope: authz.ScopeSite, Reaches: model.SensitivityInternal, StarterRoles: []string{authz.RoleEventAdmin, authz.RoleSettingsAdmin}})
	res, err := r.svc.Seed(r.sctx(), more)
	wantNoErr(t, err)
	if res.Added != 2 {
		t.Fatalf("Added = %d, want 2", res.Added)
	}
	if !hasPerm(r.roleKey(authz.RoleEventAdmin), "later.thing.read") {
		t.Error("new permission not seeded into event-admin")
	}
	res, err = r.svc.Seed(r.sctx(), more)
	wantNoErr(t, err)
	if res.Added != 0 {
		t.Errorf("repeat Added = %d", res.Added)
	}
}

func TestSeed_KeyWithoutRole_IsSkippedAndRecorded(t *testing.T) {
	r := newRigNoSeed(t)
	decls := []authz.PermissionDecl{{Name: "odd.thing.read", Scope: authz.ScopeSite, Reaches: model.SensitivityInternal, StarterRoles: []string{"no-such-role", authz.RoleEventAdmin}}}
	res, err := r.svc.Seed(r.sctx(), decls)
	wantNoErr(t, err)
	if res.Added != 1 {
		t.Fatalf("Added = %d, want 1 (the key with a role)", res.Added)
	}
	// A role with that key can never appear (keys are set by migration), so the skipped
	// pair stays skipped; seeding again adds nothing.
	res, err = r.svc.Seed(r.sctx(), decls)
	wantNoErr(t, err)
	if res.Added != 0 {
		t.Fatalf("repeat Added = %d", res.Added)
	}
}

func TestSeed_SkipsDeletedStarterRole(t *testing.T) {
	r := newRigNoSeed(t)
	ro := r.roleKey(authz.RoleSettingsAdmin)
	wantNoErr(t, r.svc.DeleteRole(r.sctx(), ro.ID))
	res, err := r.svc.Seed(r.sctx(), r.opts.Declared)
	wantNoErr(t, err)
	keys := starterKeys()
	delete(keys, authz.RoleSettingsAdmin)
	want, _ := expectedMembership(r.opts.Declared, func(k string) bool { return keys[k] })
	if res.Added != want {
		t.Fatalf("Added = %d, want %d", res.Added, want)
	}
}

func TestSeed_NothingDeclared_AddsNothing(t *testing.T) {
	r := newRigNoSeed(t)
	for _, d := range [][]authz.PermissionDecl{nil, {}} {
		res, err := r.svc.Seed(r.sctx(), d)
		wantNoErr(t, err)
		if res.Added != 0 {
			t.Errorf("Added = %d", res.Added)
		}
	}
}

func TestSeed_InvalidDeclaration_ReturnsInvalid(t *testing.T) {
	r := newRigNoSeed(t)
	_, err := r.svc.Seed(r.sctx(), []authz.PermissionDecl{{Name: "Bad Name", Scope: authz.ScopeSite, Reaches: model.SensitivityInternal}})
	wantKind(t, err, errs.Invalid)
}

func TestSeed_RunsWithoutAnActor(t *testing.T) {
	r := newRigNoSeed(t)
	_, err := r.svc.Seed(ctxT(t), r.opts.Declared) // bms migrate has no session
	wantNoErr(t, err)
}

// F57
func TestSeed_ReadOnly_ReturnsReadOnly(t *testing.T) {
	r := newRigNoSeed(t)
	r.fdb.set(func(f *faultDB) { f.ro = true })
	_, err := r.svc.Seed(r.sctx(), r.opts.Declared)
	wantReadOnly(t, err)
}

func TestSeed_StoreFails_ReturnsStoreKind(t *testing.T) {
	r := newRigNoSeed(t)
	r.fdb.set(func(f *faultDB) { f.txErr, f.readErr = errDown, errDown })
	_, err := r.svc.Seed(r.sctx(), r.opts.Declared)
	wantKind(t, err, errs.Unavailable)
}
