//go:build integration

package authz_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func TestAllow_BaseRole_AppliesToEverySignedInPerson(t *testing.T) {
	r := newRig(t)
	p := r.person() // no grant row at all
	wantNoErr(t, r.svc.Allow(r.pctx(p), pGreetRead))
	wantKind(t, r.svc.Allow(r.pctx(p), pMedRead), errs.Permission)
}

// F6
func TestAllow_NotHeld_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	p := r.person()
	err := r.svc.Allow(r.pctx(p), pMedRead)
	wantKind(t, err, errs.Permission)
	if !strings.Contains(err.Error(), string(pMedRead)) {
		t.Errorf("denial does not name the permission: %v", err)
	}
	if strings.Contains(err.Error(), p.String()) {
		t.Errorf("denial names the person: %v", err)
	}
}

func TestAllow_SiteGrant_Allows(t *testing.T) {
	r := newRig(t)
	p := r.holder(pMedRead, pPayAudit)
	wantNoErr(t, r.svc.Allow(r.pctx(p), pMedRead))
	wantNoErr(t, r.svc.Allow(r.pctx(p), pPayAudit))
	wantKind(t, r.svc.Allow(r.pctx(p), pMedWrite), errs.Permission)
}

func TestAllow_UnionOfRoles(t *testing.T) {
	r := newRig(t)
	p := r.holder(pMedRead)
	r.give(p, r.mkRole(authz.ScopeSite, pPayAudit), model.DepartmentID{}, time.Time{})
	wantNoErr(t, r.svc.Allow(r.pctx(p), pMedRead))
	wantNoErr(t, r.svc.Allow(r.pctx(p), pPayAudit))
}

// F7
func TestAllow_DepartmentOnly_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	p := r.deptHolder(r.d1, pShiftManage)
	wantKind(t, r.svc.Allow(r.pctx(p), pShiftManage), errs.Permission)
}

// F12
func TestAllowIn_OtherDepartment_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	p := r.deptHolder(r.d1, pShiftManage)
	wantNoErr(t, r.svc.AllowIn(r.pctx(p), pShiftManage, r.d1))
	wantKind(t, r.svc.AllowIn(r.pctx(p), pShiftManage, r.d2), errs.Permission)
	wantKind(t, r.svc.AllowIn(r.pctx(p), pShiftRead, r.d1), errs.Permission)
}

func TestAllowIn_SiteWideHolder_CoversAnyDepartment(t *testing.T) {
	r := newRig(t)
	p := r.holder(pShiftManage)
	for _, d := range []model.DepartmentID{r.d1, r.d2, newDept()} {
		wantNoErr(t, r.svc.AllowIn(r.pctx(p), pShiftManage, d))
	}
}

func TestAllowIn_SiteScopedPermission_HeldSiteWide(t *testing.T) {
	r := newRig(t)
	p := r.holder(pMedRead)
	wantNoErr(t, r.svc.AllowIn(r.pctx(p), pMedRead, r.d1))
}

func TestAllowIn_NoActor_And_ZeroDepartment_AreChecked(t *testing.T) {
	r := newRig(t)
	p := r.holder(pShiftManage)
	wantKind(t, r.svc.AllowIn(r.pctx(p), pShiftManage, model.DepartmentID{}), errs.Invalid)
	wantKind(t, r.svc.AllowIn(ctxT(t), pShiftManage, r.d1), errs.Unauthenticated)
}

// F9: "active" means ends_at > now.
func TestAllow_EndedGrant_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	p := r.person()
	r.give(p, r.mkRole(authz.ScopeSite, pMedRead), model.DepartmentID{}, r.clk.Now().Add(time.Hour))
	wantNoErr(t, r.svc.Allow(r.pctx(p), pMedRead))
	wantNoErr(t, r.clk.Advance(time.Hour-time.Nanosecond))
	wantNoErr(t, r.svc.Allow(r.pctx(p), pMedRead))
	wantNoErr(t, r.clk.Advance(time.Nanosecond)) // now == ends_at: no longer active
	wantKind(t, r.svc.Allow(r.pctx(p), pMedRead), errs.Permission)
	wantNoErr(t, r.clk.Advance(24*time.Hour))
	wantKind(t, r.svc.Allow(r.pctx(p), pMedRead), errs.Permission)
}

func TestAllowIn_EndedDepartmentGrant_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	p := r.person()
	r.give(p, r.mkRole(authz.ScopeDepartment, pShiftManage), r.d1, r.clk.Now().Add(time.Hour))
	wantNoErr(t, r.svc.AllowIn(r.pctx(p), pShiftManage, r.d1))
	wantNoErr(t, r.clk.Advance(2*time.Hour))
	wantKind(t, r.svc.AllowIn(r.pctx(p), pShiftManage, r.d1), errs.Permission)
	e, err := r.svc.ExtentOf(r.pctx(p), pShiftManage)
	wantNoErr(t, err)
	if !e.IsZero() {
		t.Fatalf("ExtentOf after the end = %+v, want zero", e)
	}
}

func TestExpiry_PublishesNothingAndDeletesNothing(t *testing.T) {
	r := newRig(t)
	p := r.person()
	r.give(p, r.mkRole(authz.ScopeSite, pMedRead), model.DepartmentID{}, r.clk.Now().Add(time.Hour))
	before := len(r.bus.Published())
	wantNoErr(t, r.clk.Advance(3*time.Hour))
	wantKind(t, r.svc.Allow(r.pctx(p), pMedRead), errs.Permission)
	if len(r.bus.Published()) != before {
		t.Error("expiry published a signal")
	}
	if n := len(r.grantsOf(p)); n != 1 {
		t.Errorf("ended grant rows = %d, want it kept (history)", n)
	}
}

// Rules apply at decision time, so one created after the grants takes effect at once.
// F8
func TestAllow_ForbiddenByRule_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	roleA := r.mkRole(authz.ScopeSite, pPayApprove)
	roleB := r.mkRole(authz.ScopeSite, pPayAudit)
	p := r.person()
	r.give(p, roleA, model.DepartmentID{}, time.Time{})
	r.give(p, roleB, model.DepartmentID{}, time.Time{})
	wantNoErr(t, r.svc.Allow(r.pctx(p), pPayAudit))
	rule, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: roleA.ID, Permission: pPayAudit})
	wantNoErr(t, err)
	wantKind(t, r.svc.Allow(r.pctx(p), pPayAudit), errs.Permission)
	wantNoErr(t, r.svc.Allow(r.pctx(p), pPayApprove))
	// Deleting the rule restores it.
	wantNoErr(t, r.svc.DeleteRule(r.sctx(), rule.ID))
	wantNoErr(t, r.svc.Allow(r.pctx(p), pPayAudit))
}

func TestAllow_RuleOnlyAffectsHoldersOfItsRole(t *testing.T) {
	r := newRig(t)
	roleA := r.mkRole(authz.ScopeSite, pPayApprove)
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: roleA.ID, Permission: pPayAudit})
	wantNoErr(t, err)
	other := r.holder(pPayAudit)
	wantNoErr(t, r.svc.Allow(r.pctx(other), pPayAudit))
}

// Decision 3: a role edit cannot get around a rule.
func TestAllow_RoleEditCannotGetAroundRule(t *testing.T) {
	r := newRig(t)
	roleA := r.mkRole(authz.ScopeSite, pPayApprove)
	roleB := r.mkRole(authz.ScopeSite, pMedRead)
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: roleA.ID, Permission: pPayAudit})
	wantNoErr(t, err)
	p := r.person()
	r.give(p, roleA, model.DepartmentID{}, time.Time{})
	r.give(p, roleB, model.DepartmentID{}, time.Time{})
	_, err = r.svc.UpdateRole(r.sctx(), roleB.ID, authz.RoleInput{Name: roleB.Name, Permissions: []authz.Permission{pMedRead, pPayAudit}})
	wantNoErr(t, err)
	wantKind(t, r.svc.Allow(r.pctx(p), pPayAudit), errs.Permission)
	wantNoErr(t, r.svc.Allow(r.pctx(p), pMedRead))
}

// Rules forbid the permission "in every extent" and count roles held "in any extent".
func TestAllow_RuleCoversEveryExtent(t *testing.T) {
	r := newRig(t)
	deptRole := r.mkRole(authz.ScopeDepartment, pShiftRead)
	siteRole := r.mkRole(authz.ScopeSite, pShiftManage)
	p := r.person()
	r.give(p, deptRole, r.d1, time.Time{}) // held in one department only
	r.give(p, siteRole, model.DepartmentID{}, time.Time{})
	// The rule comes after the grants: creating it first would make the second Grant a Conflict.
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: deptRole.ID, Permission: pShiftManage})
	wantNoErr(t, err)
	wantKind(t, r.svc.Allow(r.pctx(p), pShiftManage), errs.Permission)
	wantKind(t, r.svc.AllowIn(r.pctx(p), pShiftManage, r.d2), errs.Permission)
}

// Decision 4: a rule by level forbids every declared permission reaching at least L.
func TestAllow_RuleByLevel_ForbidsAtOrAbove(t *testing.T) {
	r := newRig(t)
	gate := r.mkRole(authz.ScopeSite, pPayApprove)
	rich := r.mkRole(authz.ScopeSite, pMedRead, pMedWrite, pPayAudit)
	p := r.person()
	r.give(p, gate, model.DepartmentID{}, time.Time{})
	r.give(p, rich, model.DepartmentID{}, time.Time{})
	rule, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: gate.ID, Reaches: model.SensitivityPersonal})
	wantNoErr(t, err)
	wantKind(t, r.svc.Allow(r.pctx(p), pMedRead), errs.Permission)  // sensitive >= personal
	wantKind(t, r.svc.Allow(r.pctx(p), pMedWrite), errs.Permission) // personal >= personal
	wantNoErr(t, r.svc.Allow(r.pctx(p), pPayAudit))                 // internal < personal
	wantNoErr(t, r.svc.DeleteRule(r.sctx(), rule.ID))
	_, err = r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: gate.ID, Reaches: model.SensitivitySensitive})
	wantNoErr(t, err)
	wantKind(t, r.svc.Allow(r.pctx(p), pMedRead), errs.Permission)
	wantNoErr(t, r.svc.Allow(r.pctx(p), pMedWrite))
}

func TestExtentOf_RuleRemovesDepartmentRows(t *testing.T) {
	r := newRig(t)
	gate := r.mkRole(authz.ScopeSite, pPayApprove)
	dept := r.mkRole(authz.ScopeDepartment, pShiftManage)
	p := r.person()
	r.give(p, gate, model.DepartmentID{}, time.Time{})
	r.give(p, dept, r.d1, time.Time{})
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: gate.ID, Permission: pShiftManage})
	wantNoErr(t, err)
	e, err := r.svc.ExtentOf(r.pctx(p), pShiftManage)
	wantNoErr(t, err)
	if !e.IsZero() {
		t.Fatalf("ExtentOf = %+v, want zero after the rule", e)
	}
}

// Decision 13
func TestUndeclaredPermission_InRole_StoredAndHasNoEffect(t *testing.T) {
	r := newRig(t)
	ro, err := r.svc.CreateRole(r.sctx(), authz.RoleInput{Name: r.name("Ghosts"), Scope: authz.ScopeSite, Permissions: []authz.Permission{pGhost, pMedRead}})
	wantNoErr(t, err)
	got, err := r.svc.GetRole(r.sctx(), ro.ID)
	wantNoErr(t, err)
	var sawGhost bool
	for _, rp := range got.Permissions {
		switch rp.Name {
		case pGhost:
			sawGhost = true
			if rp.Declared {
				t.Error("undeclared permission shown as Declared")
			}
		case pMedRead:
			if !rp.Declared {
				t.Error("declared permission shown as not available")
			}
		}
	}
	if !sawGhost {
		t.Fatal("undeclared permission was not stored")
	}
	p := r.person()
	r.give(p, ro, model.DepartmentID{}, time.Time{})
	wantNoErr(t, r.svc.Allow(r.pctx(p), pMedRead))
	wantKind(t, r.svc.Allow(r.pctx(p), pGhost), errs.Invalid)
	names, err := r.svc.PermissionsOf(r.pctx(p), p)
	wantNoErr(t, err)
	if slices.Contains(names, string(pGhost)) {
		t.Errorf("PermissionsOf lists an undeclared permission: %v", names)
	}
	infos, err := r.svc.ListPermissions(r.sctx())
	wantNoErr(t, err)
	for _, in := range infos {
		if in.Name == pGhost {
			t.Error("the catalogue lists an undeclared permission")
		}
	}
}

func TestUndeclaredPermission_InRule_StoredAndHasNoEffect(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	rule, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: ro.ID, Permission: pGhost})
	wantNoErr(t, err)
	if rule.Permission != pGhost {
		t.Errorf("rule permission = %q", rule.Permission)
	}
	p := r.person()
	r.give(p, ro, model.DepartmentID{}, time.Time{})
	wantNoErr(t, r.svc.Allow(r.pctx(p), pMedRead))
}

func TestExtentOf_Table(t *testing.T) {
	r := newRig(t)
	site := r.holder(pShiftManage)
	none := r.person()
	one := r.deptHolder(r.d1, pShiftManage)
	two := r.deptHolder(r.d1, pShiftManage)
	r.give(two, r.mkRole(authz.ScopeDepartment, pShiftManage), r.d2, time.Time{})
	r.give(two, r.mkRole(authz.ScopeDepartment, pShiftManage), r.d1, time.Time{}) // same department again
	both := r.holder(pShiftManage)
	r.give(both, r.mkRole(authz.ScopeDepartment, pShiftManage), r.d1, time.Time{})

	tests := []struct {
		name     string
		who      model.PersonID
		site     bool
		depts    []model.DepartmentID
		wantZero bool
	}{
		{"site-wide", site, true, nil, false},
		{"nowhere", none, false, nil, true}, // F13
		{"one department", one, false, []model.DepartmentID{r.d1}, false},
		{"two departments, deduplicated", two, false, []model.DepartmentID{r.d1, r.d2}, false},
		{"site and department: site only", both, true, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, err := r.svc.ExtentOf(r.pctx(tc.who), pShiftManage)
			wantNoErr(t, err)
			if e.Site != tc.site || e.IsZero() != tc.wantZero {
				t.Fatalf("extent = %+v", e)
			}
			if len(e.Departments) != len(tc.depts) {
				t.Fatalf("departments = %v, want %v", e.Departments, tc.depts)
			}
			for _, d := range tc.depts {
				if !e.Covers(d) {
					t.Errorf("does not cover %v", d)
				}
			}
			if !slices.IsSortedFunc(e.Departments, func(a, b model.DepartmentID) int { return slices.Compare(a[:], b[:]) }) {
				t.Errorf("departments not sorted: %v", e.Departments)
			}
		})
	}
}

func TestPermissionsOf_FormatsSiteAndDepartmentEntries(t *testing.T) {
	r := newRig(t)
	p := r.holder(pMedRead, pPayAudit)
	r.give(p, r.mkRole(authz.ScopeDepartment, pShiftManage), r.d1, time.Time{})
	got, err := r.svc.PermissionsOf(r.pctx(p), p)
	wantNoErr(t, err)
	want := []string{string(pGreetRead), string(pMedRead), string(pPayAudit), string(pShiftManage) + "@" + r.d1.String()}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("PermissionsOf = %v, want %v", got, want)
	}
}

func TestPermissionsOf_AfterRules_OmitsForbidden(t *testing.T) {
	r := newRig(t)
	gate := r.mkRole(authz.ScopeSite, pPayApprove)
	p := r.holder(pPayAudit)
	r.give(p, gate, model.DepartmentID{}, time.Time{})
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: gate.ID, Permission: pPayAudit})
	wantNoErr(t, err)
	got, err := r.svc.PermissionsOf(r.pctx(p), p)
	wantNoErr(t, err)
	if slices.Contains(got, string(pPayAudit)) || !slices.Contains(got, string(pPayApprove)) {
		t.Fatalf("PermissionsOf = %v", got)
	}
}

func TestPermissionsOf_Nothing_ReturnsEmptyNotNil(t *testing.T) {
	r := newRig(t)
	base := r.roleKey(authz.RoleParticipant)
	_, err := r.svc.UpdateRole(r.sctx(), base.ID, authz.RoleInput{Name: "Everyone"})
	wantNoErr(t, err)
	p := r.person()
	got, err := r.svc.PermissionsOf(r.pctx(p), p)
	wantNoErr(t, err)
	if got == nil || len(got) != 0 {
		t.Fatalf("PermissionsOf = %#v, want a non-nil empty slice", got)
	}
}

func TestPermissionsOf_EndedGrant_NotListed(t *testing.T) {
	r := newRig(t)
	p := r.person()
	r.give(p, r.mkRole(authz.ScopeSite, pMedRead), model.DepartmentID{}, r.clk.Now().Add(time.Minute))
	got, err := r.svc.PermissionsOf(r.pctx(p), p)
	wantNoErr(t, err)
	if !slices.Contains(got, string(pMedRead)) {
		t.Fatalf("active grant missing: %v", got)
	}
	wantNoErr(t, r.clk.Advance(time.Hour))
	got, err = r.svc.PermissionsOf(r.pctx(p), p)
	wantNoErr(t, err)
	if slices.Contains(got, string(pMedRead)) {
		t.Fatalf("ended grant listed: %v", got)
	}
}

// F16
func TestTitlesOf_BaseOnly_ReturnsNil(t *testing.T) {
	r := newRig(t)
	p := r.person()
	got, err := r.svc.TitlesOf(ctxT(t), p)
	wantNoErr(t, err)
	if got != nil {
		t.Fatalf("TitlesOf = %#v, want nil", got)
	}
}

func TestTitlesOf_NonBaseRoles_KeysOrNames_Sorted(t *testing.T) {
	r := newRig(t)
	p := r.person()
	steward := r.roleKey(authz.RoleDepartmentSteward)
	r.give(p, steward, model.DepartmentID{}, time.Time{})
	r.give(p, r.roleKey(authz.RoleAuditReviewer), model.DepartmentID{}, time.Time{})
	named, err := r.svc.CreateRole(r.sctx(), authz.RoleInput{Name: "Gate Coordinator", Scope: authz.ScopeSite})
	wantNoErr(t, err)
	r.give(p, named, model.DepartmentID{}, time.Time{})
	got, err := r.svc.TitlesOf(ctxT(t), p) // no actor needed
	wantNoErr(t, err)
	want := []string{authz.RoleAuditReviewer, authz.RoleDepartmentSteward, "Gate Coordinator"}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("TitlesOf = %v, want %v", got, want)
	}
	// A renamed starter role shows its name.
	_, err = r.svc.UpdateRole(r.sctx(), steward.ID, authz.RoleInput{Name: "Steward of Stewards"})
	wantNoErr(t, err)
	got, err = r.svc.TitlesOf(ctxT(t), p)
	wantNoErr(t, err)
	if !slices.Contains(got, "Steward of Stewards") || slices.Contains(got, authz.RoleDepartmentSteward) {
		t.Fatalf("TitlesOf after rename = %v", got)
	}
}

func TestTitlesOf_DepartmentGrantCounts_EndedDoesNot(t *testing.T) {
	r := newRig(t)
	p := r.person()
	r.give(p, r.roleKey(authz.RoleDepartmentAdmin), r.d1, time.Time{})
	r.give(p, r.roleKey(authz.RoleEventAdmin), model.DepartmentID{}, r.clk.Now().Add(time.Hour))
	got, err := r.svc.TitlesOf(ctxT(t), p)
	wantNoErr(t, err)
	if !slices.Equal(got, []string{authz.RoleDepartmentAdmin, authz.RoleEventAdmin}) {
		t.Fatalf("TitlesOf = %v", got)
	}
	wantNoErr(t, r.clk.Advance(2*time.Hour))
	got, err = r.svc.TitlesOf(ctxT(t), p)
	wantNoErr(t, err)
	if !slices.Equal(got, []string{authz.RoleDepartmentAdmin}) {
		t.Fatalf("TitlesOf after expiry = %v", got)
	}
}

func TestTitlesOf_CapsAtMaxTitles(t *testing.T) {
	r := newRig(t)
	p := r.person()
	for i := 0; i < audit.MaxTitles+4; i++ {
		r.give(p, r.mkRole(authz.ScopeSite, pMedRead), model.DepartmentID{}, time.Time{})
	}
	got, err := r.svc.TitlesOf(ctxT(t), p)
	wantNoErr(t, err)
	if len(got) != audit.MaxTitles {
		t.Fatalf("len(TitlesOf) = %d, want %d", len(got), audit.MaxTitles)
	}
	for _, s := range got {
		if len(s) > audit.MaxTitleBytes {
			t.Errorf("title %q longer than %d bytes", s, audit.MaxTitleBytes)
		}
	}
}

// audit: "TitlesOf may be called inside a Write transaction; it never opens one".
func TestTitlesOf_InsideWriteTransaction_Works(t *testing.T) {
	r := newRig(t)
	p := r.person()
	r.give(p, r.roleKey(authz.RoleEventAdmin), model.DepartmentID{}, time.Time{})
	err := r.fdb.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		got, err := r.svc.TitlesOf(ctx, p)
		if err != nil {
			return fmt.Errorf("TitlesOf: %w", err)
		}
		if !slices.Equal(got, []string{authz.RoleEventAdmin}) {
			t.Errorf("TitlesOf in a transaction = %v", got)
		}
		return nil
	})
	wantNoErr(t, err)
}

// F13
func TestExtentOf_NotHeld_ReturnsZeroExtent(t *testing.T) {
	r := newRig(t)
	e, err := r.svc.ExtentOf(r.pctx(r.person()), pShiftManage)
	wantNoErr(t, err)
	if !e.IsZero() || e.Site || len(e.Departments) != 0 {
		t.Fatalf("extent = %+v, want zero", e)
	}
}

// A role stored while its permission was undeclared (so its scope was unknown) must not
// give a department grant of a site-only permission once the permission is declared site.
func TestDepartmentRole_PermissionLaterDeclaredSiteOnly_HasNoEffect(t *testing.T) {
	r := newRig(t)
	const late authz.Permission = "late.shifts.manage"
	ro, err := r.svc.CreateRole(r.sctx(), authz.RoleInput{Name: r.name("Late"), Scope: authz.ScopeDepartment, Permissions: []authz.Permission{late, pShiftRead}})
	wantNoErr(t, err)
	p := r.person()
	r.give(p, ro, r.d1, time.Time{})

	o := r.opts
	r.declared = append(append([]authz.PermissionDecl(nil), r.declared...),
		authz.PermissionDecl{Name: late, Scope: authz.ScopeSite, Reaches: model.SensitivityInternal})
	o.Lookup = r.lookupFor() // the decisions follow the new declarations
	svc2, err := authz.New(o)
	wantNoErr(t, err)

	ctx := r.pctx(p)
	wantKind(t, svc2.AllowIn(ctx, late, r.d1), errs.Permission)
	wantKind(t, svc2.Allow(ctx, late), errs.Permission)
	e, err := svc2.ExtentOf(ctx, late)
	wantNoErr(t, err)
	if !e.IsZero() {
		t.Fatalf("ExtentOf = %+v, want zero", e)
	}
	names, err := svc2.PermissionsOf(ctx, p)
	wantNoErr(t, err)
	for _, n := range names {
		if strings.HasPrefix(n, string(late)) {
			t.Errorf("PermissionsOf lists %q", n)
		}
	}
	// The department permission that is declared for departments still works.
	wantNoErr(t, svc2.AllowIn(ctx, pShiftRead, r.d1))
}

// A rule naming an undeclared permission has no effect, also when granting.
func TestGrant_RuleNamesUndeclaredPermission_NotRefused(t *testing.T) {
	r := newRig(t)
	roleA := r.mkRole(authz.ScopeSite, pPayApprove)
	roleB := r.mkRole(authz.ScopeSite, pGhost, pMedRead)
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: roleA.ID, Permission: pGhost})
	wantNoErr(t, err)
	p := r.person()
	r.give(p, roleA, model.DepartmentID{}, time.Time{})
	_, err = r.svc.Grant(r.sctx(), authz.GrantInput{Person: p, Role: roleB.ID})
	wantNoErr(t, err)
	wantNoErr(t, r.svc.Allow(r.pctx(p), pMedRead))
}
