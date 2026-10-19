//go:build integration

package authz_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

var noDept model.DepartmentID

func (r *irig) grantAs(ctx context.Context, p model.PersonID, ro authz.Role, d model.DepartmentID) (authz.Grant, error) {
	return r.svc.Grant(ctx, authz.GrantInput{Person: p, Role: ro.ID, Department: d}) //nolint:wrapcheck // test helper passes the error through for kind checks
}

// ---- Grant: success --------------------------------------------------------------------

func TestGrant_Success_RecordsRowAuditAndSignal(t *testing.T) {
	r := newRig(t)
	admin := r.admin()
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	target := r.person()
	ends := r.clk.Now().Add(48 * time.Hour)
	auditBefore, sigBefore := len(r.entries("access.grant.created")), len(r.sigsOf(sigCreated))
	g, err := r.svc.Grant(r.pctx(admin), authz.GrantInput{Person: target, Role: ro.ID, EndsAt: ends})
	wantNoErr(t, err)
	if g.ID.IsZero() || g.Person != target || g.Role != ro.ID || !g.Department.IsZero() ||
		!g.EndsAt.Equal(ends) || g.GrantedBy != admin || !g.CreatedAt.Equal(r.clk.Now()) {
		t.Fatalf("grant = %+v", g)
	}
	wantNoErr(t, r.svc.Allow(r.pctx(target), pMedRead))

	es := r.entries("access.grant.created")
	if len(es) != auditBefore+1 || es[len(es)-1].Subject != target {
		t.Fatalf("audit entries = %+v (before the call: %d)", es, auditBefore)
	}
	sigs := r.sigsOf(sigCreated)
	if len(sigs) != sigBefore+1 {
		t.Fatalf("%d access.grant-created signals, want %d", len(sigs), sigBefore+1)
	}
	checkPayload(t, sigs[len(sigs)-1], g, false)
}

func checkPayload(t *testing.T, s signals.Signal, g authz.Grant, scoped bool) {
	t.Helper()
	gid, err := signals.Get[authz.GrantID](s.Payload, "grant")
	wantNoErr(t, err)
	pid, err := signals.Get[model.PersonID](s.Payload, "person")
	wantNoErr(t, err)
	rid, err := signals.Get[authz.RoleID](s.Payload, "role")
	wantNoErr(t, err)
	if gid != g.ID || pid != g.Person || rid != g.Role {
		t.Errorf("payload %v does not match grant %+v", s.Payload, g)
	}
	_, has := s.Payload["department"]
	if has != scoped {
		t.Errorf("department key present = %v, want %v", has, scoped)
	}
	if scoped {
		d, err := signals.Get[model.DepartmentID](s.Payload, "department")
		wantNoErr(t, err)
		if d != g.Department {
			t.Errorf("payload department = %v", d)
		}
	}
	if len(s.Payload) > 4 {
		t.Errorf("payload carries extra keys: %v", s.Payload)
	}
}

func TestGrant_DepartmentRole_PublishesDepartmentKey(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeDepartment, pShiftManage)
	g := r.give(r.person(), ro, r.d1, time.Time{})
	if g.Department != r.d1 {
		t.Fatalf("grant department = %v", g.Department)
	}
	sigs := r.sigsOf(sigCreated)
	if len(sigs) == 0 {
		t.Fatal("no signal")
	}
	checkPayload(t, sigs[len(sigs)-1], g, true)
}

func TestGrant_NoActor_ReturnsUnauthenticated(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	_, err := r.grantAs(ctxT(t), r.person(), ro, noDept)
	wantKind(t, err, errs.Unauthenticated)
}

func TestGrant_SiteHolderOfGrantsManage_MayGrantAnySiteRole(t *testing.T) {
	r := newRig(t)
	admin := r.admin()
	ro := r.mkRole(authz.ScopeSite, pMedRead, pMedWrite) // admin holds neither
	_, err := r.grantAs(r.pctx(admin), r.person(), ro, noDept)
	wantNoErr(t, err)
}

// decision 5: site-wide holders are exempt from the no-escalation rule, also for
// department roles anywhere.
func TestGrant_SiteHolder_GrantsDepartmentRoleWithoutHoldingItsPermissions(t *testing.T) {
	r := newRig(t)
	admin := r.admin()
	ro := r.mkRole(authz.ScopeDepartment, pShiftManage)
	_, err := r.grantAs(r.pctx(admin), r.person(), ro, r.d2)
	wantNoErr(t, err)
}

func TestGrant_Steward_GrantsDepartmentRolesAnywhere_ButNoSiteRoles(t *testing.T) {
	r := newRig(t)
	steward := r.person()
	r.give(steward, r.roleKey(authz.RoleDepartmentSteward), noDept, time.Time{})
	deptRole := r.mkRole(authz.ScopeDepartment, pShiftManage, pShiftRead)
	siteRole := r.mkRole(authz.ScopeSite, pMedRead)
	for _, d := range []model.DepartmentID{r.d1, r.d2} {
		_, err := r.grantAs(r.pctx(steward), r.person(), deptRole, d)
		wantNoErr(t, err)
	}
	_, err := r.grantAs(r.pctx(steward), r.person(), siteRole, noDept)
	wantKind(t, err, errs.Permission) // F35
}

func TestGrant_DepartmentAdmin_GrantsWhatTheyHoldAtTheirDepartment(t *testing.T) {
	r := newRig(t)
	da := r.person()
	r.give(da, r.roleKey(authz.RoleDepartmentAdmin), r.d1, time.Time{}) // grants.read/manage, shifts.manage
	ok := r.mkRole(authz.ScopeDepartment, pShiftManage, pGrantsRead)
	_, err := r.grantAs(r.pctx(da), r.person(), ok, r.d1)
	wantNoErr(t, err)
}

func TestGrant_DepartmentAdmin_PermissionHeldSiteWideCountsAsHeldAtD(t *testing.T) {
	r := newRig(t)
	da := r.person()
	r.give(da, r.roleKey(authz.RoleDepartmentAdmin), r.d1, time.Time{})
	r.give(da, r.mkRole(authz.ScopeSite, pShiftRead), noDept, time.Time{}) // site-wide shifts.read
	ro := r.mkRole(authz.ScopeDepartment, pShiftManage, pShiftRead)
	_, err := r.grantAs(r.pctx(da), r.person(), ro, r.d1)
	wantNoErr(t, err)
}

// F35
func TestGrant_SiteRoleByDepartmentAdmin_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	da := r.person()
	r.give(da, r.roleKey(authz.RoleDepartmentAdmin), r.d1, time.Time{})
	siteRole := r.mkRole(authz.ScopeSite, pShiftManage)
	_, err := r.grantAs(r.pctx(da), r.person(), siteRole, noDept)
	wantKind(t, err, errs.Permission)
}

func TestGrant_OtherDepartmentByDepartmentAdmin_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	da := r.person()
	r.give(da, r.roleKey(authz.RoleDepartmentAdmin), r.d1, time.Time{})
	ro := r.mkRole(authz.ScopeDepartment, pShiftManage)
	_, err := r.grantAs(r.pctx(da), r.person(), ro, r.d2)
	wantKind(t, err, errs.Permission)
}

func TestGrant_NoGrantPermission_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	callers := map[string]model.PersonID{
		"nobody":       r.person(),
		"grants.read":  r.holder(pGrantsRead),
		"roles.manage": r.holder(pRolesManage),
	}
	auditBefore, sigBefore := len(r.entries("access.grant.created")), len(r.sigsOf(sigCreated))
	for name, p := range callers {
		_, err := r.grantAs(r.pctx(p), r.person(), ro, noDept)
		if !errs.Is(err, errs.Permission) {
			t.Errorf("%s: kind = %v (%v), want Permission", name, errs.KindOf(err), err)
		}
	}
	if len(r.entries("access.grant.created")) != auditBefore || len(r.sigsOf(sigCreated)) != sigBefore {
		t.Error("a refused grant left an audit entry or signal")
	}
}

// F36
func TestGrant_Escalation_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	da := r.person()
	r.give(da, r.roleKey(authz.RoleDepartmentAdmin), r.d1, time.Time{})
	// shifts.read is not held by the department admin; shifts.manage is.
	ro := r.mkRole(authz.ScopeDepartment, pShiftManage, pShiftRead)
	_, err := r.grantAs(r.pctx(da), r.person(), ro, r.d1)
	wantKind(t, err, errs.Permission)
}

func TestGrant_Escalation_PermissionHeldAtOtherDepartmentDoesNotCount(t *testing.T) {
	r := newRig(t)
	da := r.person()
	r.give(da, r.roleKey(authz.RoleDepartmentAdmin), r.d1, time.Time{})
	r.give(da, r.mkRole(authz.ScopeDepartment, pShiftRead), r.d2, time.Time{})
	ro := r.mkRole(authz.ScopeDepartment, pShiftRead)
	_, err := r.grantAs(r.pctx(da), r.person(), ro, r.d1)
	wantKind(t, err, errs.Permission)
}

func TestGrant_Escalation_SuperuserAndSiteHoldersExempt(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeDepartment, pShiftManage, pShiftRead)
	_, err := r.grantAs(r.sctx(), r.person(), ro, r.d1)
	wantNoErr(t, err)
}

// F37
func TestGrant_Self_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	admin := r.admin()
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	_, err := r.grantAs(r.pctx(admin), admin, ro, noDept)
	wantField(t, err, "person", "self")
	if len(r.grantsOf(admin)) != 1 { // only access-admin
		t.Error("a self-grant was written")
	}
}

func TestGrant_Self_SuperuserExcepted(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	_, err := r.grantAs(r.sctx(), r.su, ro, noDept)
	wantNoErr(t, err)
}

// F38
func TestGrant_BaseRole_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	_, err := r.grantAs(r.sctx(), r.person(), r.roleKey(authz.RoleParticipant), noDept)
	wantField(t, err, "role", "base")
}

// F39
func TestGrant_UnknownRole_ReturnsNotFound(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.Grant(r.sctx(), authz.GrantInput{Person: r.person(), Role: model.NewID[authz.RoleID]()})
	wantKind(t, err, errs.NotFound)
}

func TestGrant_RequiredFields_ReturnInvalid(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite)
	_, err := r.svc.Grant(r.sctx(), authz.GrantInput{Role: ro.ID})
	wantField(t, err, "person", "required")
	_, err = r.svc.Grant(r.sctx(), authz.GrantInput{Person: r.person()})
	wantField(t, err, "role", "required")
}

// F40
func TestGrant_DepartmentMismatch_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	siteRole := r.mkRole(authz.ScopeSite, pMedRead)
	deptRole := r.mkRole(authz.ScopeDepartment, pShiftRead)
	_, err := r.grantAs(r.sctx(), r.person(), siteRole, r.d1)
	wantField(t, err, "department", "mismatch")
	_, err = r.grantAs(r.sctx(), r.person(), deptRole, noDept)
	wantField(t, err, "department", "required")
}

// F41
func TestGrant_NoDepartmentSource_ReturnsUnsupported(t *testing.T) {
	r := newRig(t, func(o *authz.Options) { o.Departments = nil })
	deptRole := r.mkRole(authz.ScopeDepartment, pShiftRead)
	_, err := r.grantAs(r.sctx(), r.person(), deptRole, r.d1)
	wantKind(t, err, errs.Unsupported)
	// Site-wide grants still work.
	_, err = r.grantAs(r.sctx(), r.person(), r.mkRole(authz.ScopeSite, pMedRead), noDept)
	wantNoErr(t, err)
}

// F42
func TestGrant_InactiveDepartment_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	deptRole := r.mkRole(authz.ScopeDepartment, pShiftRead)
	_, err := r.grantAs(r.sctx(), r.person(), deptRole, r.dOff)
	wantField(t, err, "department", "unknown")
	_, err = r.grantAs(r.sctx(), r.person(), deptRole, newDept())
	wantField(t, err, "department", "unknown")
}

func TestGrant_DepartmentSourceFails_ReturnsStoreKind(t *testing.T) {
	r := newRig(t)
	r.depts.err = errs.E("test.depts", errs.Unavailable, errors.New("departments down"))
	deptRole := r.mkRole(authz.ScopeDepartment, pShiftRead)
	_, err := r.grantAs(r.sctx(), r.person(), deptRole, r.d1)
	wantKind(t, err, errs.Unavailable)
}

// F43
func TestGrant_EndsInPast_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	for name, ends := range map[string]time.Time{
		"past":    r.clk.Now().Add(-time.Hour),
		"exactly": r.clk.Now(),
	} {
		_, err := r.svc.Grant(r.sctx(), authz.GrantInput{Person: r.person(), Role: ro.ID, EndsAt: ends})
		if !errs.Is(err, errs.Invalid) {
			t.Errorf("%s: kind = %v, want Invalid", name, errs.KindOf(err))
			continue
		}
		wantField(t, err, "ends_at", "range")
	}
	_, err := r.svc.Grant(r.sctx(), authz.GrantInput{Person: r.person(), Role: ro.ID, EndsAt: r.clk.Now().Add(time.Nanosecond)})
	wantNoErr(t, err)
}

// F44
func TestGrant_Duplicate_ReturnsConflict(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	p := r.person()
	r.give(p, ro, noDept, time.Time{})
	_, err := r.grantAs(r.sctx(), p, ro, noDept)
	wantCode(t, err, errs.Conflict, "duplicate")
}

func TestGrant_Duplicate_EndedGrantStillCounts(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	p := r.person()
	r.give(p, ro, noDept, r.clk.Now().Add(time.Minute))
	wantNoErr(t, r.clk.Advance(time.Hour))
	_, err := r.grantAs(r.sctx(), p, ro, noDept)
	wantCode(t, err, errs.Conflict, "duplicate")
}

func TestGrant_SameRoleOtherDepartment_IsNotADuplicate(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeDepartment, pShiftRead)
	p := r.person()
	r.give(p, ro, r.d1, time.Time{})
	r.give(p, ro, r.d2, time.Time{})
	if n := len(r.grantsOf(p)); n != 2 {
		t.Fatalf("%d grants, want 2", n)
	}
}

// F45 and order independence (Behavior, Rules).
func TestGrant_SeparationOfDuties_ReturnsConflict(t *testing.T) {
	r := newRig(t)
	roleA := r.mkRole(authz.ScopeSite, pPayApprove)
	roleB := r.mkRole(authz.ScopeSite, pPayAudit)
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: roleA.ID, Permission: pPayAudit, Note: "approvers do not audit"})
	wantNoErr(t, err)

	p := r.person()
	r.give(p, roleA, noDept, time.Time{})
	_, err = r.grantAs(r.sctx(), p, roleB, noDept)
	wantCode(t, err, errs.Conflict, "separation_of_duties")
	if len(r.grantsOf(p)) != 1 {
		t.Error("the violating grant was written")
	}

	// the other order
	q := r.person()
	r.give(q, roleB, noDept, time.Time{})
	_, err = r.grantAs(r.sctx(), q, roleA, noDept)
	wantCode(t, err, errs.Conflict, "separation_of_duties")
}

func TestGrant_SeparationOfDuties_ParamNamesTheRule(t *testing.T) {
	r := newRig(t)
	roleA := r.mkRole(authz.ScopeSite, pPayApprove)
	roleB := r.mkRole(authz.ScopeSite, pPayAudit)
	rule, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: roleA.ID, Permission: pPayAudit})
	wantNoErr(t, err)
	p := r.person()
	r.give(p, roleA, noDept, time.Time{})
	_, err = r.grantAs(r.sctx(), p, roleB, noDept)
	wantKind(t, err, errs.Conflict)
	found := false
	for _, fe := range validate.Fields(err) {
		if fe.Code == "separation_of_duties" && fe.Params["rule"] == rule.ID.String() {
			found = true
		}
	}
	if !found {
		t.Errorf("no separation_of_duties error with params rule=%s", rule.ID)
	}
}

func TestGrant_SeparationOfDuties_ByLevelAndAcrossExtents(t *testing.T) {
	r := newRig(t)
	gate := r.mkRole(authz.ScopeDepartment, pShiftRead)
	rich := r.mkRole(authz.ScopeSite, pMedRead)
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: gate.ID, Reaches: model.SensitivitySensitive})
	wantNoErr(t, err)
	p := r.person()
	r.give(p, gate, r.d1, time.Time{}) // held in one department only
	_, err = r.grantAs(r.sctx(), p, rich, noDept)
	wantCode(t, err, errs.Conflict, "separation_of_duties")
	// Another person is unaffected.
	_, err = r.grantAs(r.sctx(), r.person(), rich, noDept)
	wantNoErr(t, err)
}

// Site-wide holders are bound by separation-of-duties rules (decision 5).
func TestGrant_SiteGranter_BoundBySeparationOfDuties(t *testing.T) {
	r := newRig(t)
	admin := r.admin()
	roleA := r.mkRole(authz.ScopeSite, pPayApprove)
	roleB := r.mkRole(authz.ScopeSite, pPayAudit)
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: roleA.ID, Permission: pPayAudit})
	wantNoErr(t, err)
	p := r.person()
	r.give(p, roleA, noDept, time.Time{})
	_, err = r.grantAs(r.pctx(admin), p, roleB, noDept)
	wantCode(t, err, errs.Conflict, "separation_of_duties")
	// The steward granting department roles is bound too.
	steward := r.person()
	r.give(steward, r.roleKey(authz.RoleDepartmentSteward), noDept, time.Time{})
	deptA := r.mkRole(authz.ScopeDepartment, pShiftRead)
	deptB := r.mkRole(authz.ScopeDepartment, pShiftManage)
	_, err = r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: deptA.ID, Permission: pShiftManage})
	wantNoErr(t, err)
	q := r.person()
	r.give(q, deptA, r.d1, time.Time{})
	_, err = r.grantAs(r.pctx(steward), q, deptB, r.d1)
	wantCode(t, err, errs.Conflict, "separation_of_duties")
}

// F46
func TestGrant_AuditFails_RollsBack(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	p := r.person()
	cause := errs.E("test.audit", errs.Unavailable, errors.New("audit down"))
	r.rec.Fail(cause)
	_, err := r.grantAs(r.sctx(), p, ro, noDept)
	wantKind(t, err, errs.Unavailable)
	r.rec.Fail(nil)
	if n := len(r.grantsOf(p)); n != 0 {
		t.Errorf("%d grants written despite the audit failure", n)
	}
	if len(r.sigsOf(sigCreated)) != 0 {
		t.Error("a signal was published")
	}
}

// F47
func TestGrant_PublishFails_RollsBack(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	p := r.person()
	r.bus.fail(errs.E("test.bus", errs.Unavailable, errors.New("bus down")))
	_, err := r.grantAs(r.sctx(), p, ro, noDept)
	wantKind(t, err, errs.Unavailable)
	r.bus.fail(nil)
	if n := len(r.grantsOf(p)); n != 0 {
		t.Errorf("%d grants written despite the publish failure", n)
	}
	// audittest.Fake does not roll back with the transaction, so the rollback is asserted on
	// the rows and the signals only.
	if len(r.sigsOf(sigCreated)) != 0 {
		t.Error("a signal was published")
	}
}

// F48
func TestGrant_ReadOnly_ReturnsReadOnly(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	r.fdb.set(func(f *faultDB) { f.ro = true })
	_, err := r.grantAs(r.sctx(), r.person(), ro, noDept)
	wantReadOnly(t, err)
}

func TestGrant_StoreFails_ReturnsStoreKind(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	for name, injected := range map[string]error{"down": errDown, "timeout": errTimeout} {
		t.Run(name, func(t *testing.T) {
			r.fdb.set(func(f *faultDB) { f.readErr, f.txErr = injected, injected })
			t.Cleanup(func() { r.fdb.set(func(f *faultDB) { f.readErr, f.txErr = nil, nil }) })
			_, err := r.grantAs(r.sctx(), r.person(), ro, noDept)
			wantKind(t, err, errs.KindOf(injected))
		})
	}
}

func TestGrant_CancelledContext_ReturnsCanceled(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	p := r.person()
	ctx, cancel := context.WithCancel(r.sctx())
	cancel()
	_, err := r.grantAs(ctx, p, ro, noDept)
	wantKind(t, err, errs.Canceled)
}

// Foreign key (decision 14): authz does not look up the person, the database does.
func TestGrant_UnknownPerson_IsRefusedByTheDatabase(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	_, err := r.grantAs(r.sctx(), newPerson(), ro, noDept) // never inserted
	if err == nil {
		t.Fatal("granting to a person who does not exist succeeded")
	}
	if errs.Is(err, errs.Internal) {
		t.Errorf("kind = Internal (%v); a foreign-key violation must be classified", err)
	}
}

// ---- Revoke ----------------------------------------------------------------------------

func TestRevoke_Success_DeletesAuditsAndPublishes(t *testing.T) {
	r := newRig(t)
	admin := r.admin()
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	p := r.person()
	g := r.give(p, ro, noDept, time.Time{})
	wantNoErr(t, r.svc.Allow(r.pctx(p), pMedRead))
	wantNoErr(t, r.svc.Revoke(r.pctx(admin), g.ID))
	wantKind(t, r.svc.Allow(r.pctx(p), pMedRead), errs.Permission)
	if n := len(r.grantsOf(p)); n != 0 {
		t.Errorf("%d grant rows left, revocation deletes the row", n)
	}
	es := r.entries("access.grant.revoked")
	if len(es) != 1 || es[0].Subject != p {
		t.Fatalf("audit = %+v", es)
	}
	sigs := r.sigsOf(sigRevoked)
	if len(sigs) != 1 {
		t.Fatalf("%d revoked signals", len(sigs))
	}
	checkPayload(t, sigs[0], g, false)
	wantKind(t, r.svc.Revoke(r.sctx(), g.ID), errs.NotFound) // not idempotent
}

func TestRevoke_DepartmentGrant_PublishesDepartment(t *testing.T) {
	r := newRig(t)
	g := r.give(r.person(), r.mkRole(authz.ScopeDepartment, pShiftRead), r.d1, time.Time{})
	wantNoErr(t, r.svc.Revoke(r.sctx(), g.ID))
	sigs := r.sigsOf(sigRevoked)
	if len(sigs) != 1 {
		t.Fatalf("%d signals", len(sigs))
	}
	checkPayload(t, sigs[0], g, true)
}

func TestRevoke_EndedGrant_CanBeRevoked(t *testing.T) {
	r := newRig(t)
	p := r.person()
	g := r.give(p, r.mkRole(authz.ScopeSite, pMedRead), noDept, r.clk.Now().Add(time.Minute))
	wantNoErr(t, r.clk.Advance(time.Hour))
	wantNoErr(t, r.svc.Revoke(r.sctx(), g.ID))
}

// F49
func TestRevoke_OutsideExtent_ReturnsNotFound(t *testing.T) {
	r := newRig(t)
	da := r.person()
	r.give(da, r.roleKey(authz.RoleDepartmentAdmin), r.d1, time.Time{})
	deptRole := r.mkRole(authz.ScopeDepartment, pShiftManage)
	siteRole := r.mkRole(authz.ScopeSite, pMedRead)
	gSite := r.give(r.person(), siteRole, noDept, time.Time{})
	gOther := r.give(r.person(), deptRole, r.d2, time.Time{})
	gMine := r.give(r.person(), deptRole, r.d1, time.Time{})
	wantKind(t, r.svc.Revoke(r.pctx(da), gSite.ID), errs.NotFound)
	wantKind(t, r.svc.Revoke(r.pctx(da), gOther.ID), errs.NotFound)
	wantKind(t, r.svc.Revoke(r.pctx(da), model.NewID[authz.GrantID]()), errs.NotFound)
	wantNoErr(t, r.svc.Revoke(r.pctx(da), gMine.ID))
	if len(r.grantsOf(gSite.Person)) != 1 || len(r.grantsOf(gOther.Person)) != 1 {
		t.Error("a grant outside the extent was deleted")
	}
}

// Revocation requires the same right as granting, including no-escalation.
func TestRevoke_RoleBeyondCallersPermissions_ReturnsNotFound(t *testing.T) {
	r := newRig(t)
	da := r.person()
	r.give(da, r.roleKey(authz.RoleDepartmentAdmin), r.d1, time.Time{})
	big := r.mkRole(authz.ScopeDepartment, pShiftManage, pShiftRead) // shifts.read not held by da
	g := r.give(r.person(), big, r.d1, time.Time{})
	wantKind(t, r.svc.Revoke(r.pctx(da), g.ID), errs.NotFound)
}

func TestRevoke_SiteHolderRevokesDepartmentGrantAnywhere(t *testing.T) {
	r := newRig(t)
	admin := r.admin()
	g := r.give(r.person(), r.mkRole(authz.ScopeDepartment, pShiftManage), r.d2, time.Time{})
	wantNoErr(t, r.svc.Revoke(r.pctx(admin), g.ID))
}

func TestRevoke_StewardRevokesDepartmentGrantNotSiteGrant(t *testing.T) {
	r := newRig(t)
	steward := r.person()
	r.give(steward, r.roleKey(authz.RoleDepartmentSteward), noDept, time.Time{})
	gd := r.give(r.person(), r.mkRole(authz.ScopeDepartment, pShiftManage), r.d1, time.Time{})
	gs := r.give(r.person(), r.mkRole(authz.ScopeSite, pMedRead), noDept, time.Time{})
	wantNoErr(t, r.svc.Revoke(r.pctx(steward), gd.ID))
	wantKind(t, r.svc.Revoke(r.pctx(steward), gs.ID), errs.NotFound)
}

func TestRevoke_NoActor_ReturnsUnauthenticated(t *testing.T) {
	r := newRig(t)
	g := r.give(r.person(), r.mkRole(authz.ScopeSite, pMedRead), noDept, time.Time{})
	wantKind(t, r.svc.Revoke(ctxT(t), g.ID), errs.Unauthenticated)
}

// F50
func TestRevoke_Self_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	admin := r.admin()
	r.admin() // a second admin so last_access_admin is not the reason
	own := r.grantsOf(admin)[0]
	wantField(t, r.svc.Revoke(r.pctx(admin), own.ID), "person", "self")
	if len(r.grantsOf(admin)) != 1 {
		t.Error("own grant was revoked")
	}
}

func TestRevoke_Self_SuperuserExcepted(t *testing.T) {
	r := newRig(t)
	g := r.give(r.su, r.mkRole(authz.ScopeSite, pMedRead), noDept, time.Time{})
	wantNoErr(t, r.svc.Revoke(r.sctx(), g.ID))
}

// F51
func TestRevoke_LastAccessAdmin_ReturnsConflict(t *testing.T) {
	r := newRig(t)
	admin := r.admin()
	g := r.grantsOf(admin)[0]
	wantCode(t, r.svc.Revoke(r.sctx(), g.ID), errs.Conflict, "last_access_admin")
	if len(r.grantsOf(admin)) != 1 {
		t.Error("the last admin's grant was deleted")
	}
	// With a second active holder the first can go.
	r.admin()
	wantNoErr(t, r.svc.Revoke(r.sctx(), g.ID))
}

func TestRevoke_LastAccessAdmin_EndedOtherHolderDoesNotCount(t *testing.T) {
	r := newRig(t)
	admin := r.admin()
	ender := r.person()
	r.give(ender, r.roleKey(authz.RoleAccessAdmin), noDept, r.clk.Now().Add(time.Hour))
	wantNoErr(t, r.clk.Advance(2*time.Hour))
	wantCode(t, r.svc.Revoke(r.sctx(), r.grantsOf(admin)[0].ID), errs.Conflict, "last_access_admin")
}

func TestRevoke_LastAccessAdmin_DepartmentHoldersDoNotCount(t *testing.T) {
	r := newRig(t)
	admin := r.admin()
	r.give(r.person(), r.roleKey(authz.RoleDepartmentAdmin), r.d1, time.Time{}) // grants.manage@D only
	wantCode(t, r.svc.Revoke(r.sctx(), r.grantsOf(admin)[0].ID), errs.Conflict, "last_access_admin")
}

func TestRevoke_ReadOnly_ReturnsReadOnly(t *testing.T) {
	r := newRig(t)
	g := r.give(r.person(), r.mkRole(authz.ScopeSite, pMedRead), noDept, time.Time{})
	r.fdb.set(func(f *faultDB) { f.ro = true })
	wantReadOnly(t, r.svc.Revoke(r.sctx(), g.ID))
}

func TestRevoke_AuditFails_RollsBack(t *testing.T) {
	r := newRig(t)
	p := r.person()
	g := r.give(p, r.mkRole(authz.ScopeSite, pMedRead), noDept, time.Time{})
	r.rec.Fail(errs.E("test.audit", errs.Unavailable, errors.New("down")))
	wantKind(t, r.svc.Revoke(r.sctx(), g.ID), errs.Unavailable)
	r.rec.Fail(nil)
	if len(r.grantsOf(p)) != 1 {
		t.Error("grant deleted despite audit failure")
	}
	if len(r.sigsOf(sigRevoked)) != 0 {
		t.Error("revoked signal published")
	}
}

// ---- ListGrants ------------------------------------------------------------------------

// F31
func TestListGrants_NoPermission_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	p := r.person()
	_, err := r.svc.ListGrants(r.pctx(p), authz.GrantQuery{})
	wantKind(t, err, errs.Permission)
	_, err = r.svc.ListGrants(r.pctx(p), authz.GrantQuery{Person: r.person()})
	wantKind(t, err, errs.Permission)
	_, err = r.svc.ListGrants(ctxT(t), authz.GrantQuery{})
	wantKind(t, err, errs.Unauthenticated)
}

// F32
func TestListGrants_OwnPerson_Allowed(t *testing.T) {
	r := newRig(t)
	p := r.holder(pMedRead)
	page, err := r.svc.ListGrants(r.pctx(p), authz.GrantQuery{Person: p})
	wantNoErr(t, err)
	if len(page.Grants) != 1 || page.Grants[0].Person != p {
		t.Fatalf("own grants = %+v", page.Grants)
	}
}

// A person may list their own grants without the role catalogue; each grant names its role by
// key (a starter role) or name, so the shell need not show an ID.
func TestListGrants_CarriesTheRoleKeyAndName(t *testing.T) {
	r := newRig(t)
	p := r.person()
	starter := r.roleKey(authz.RoleAccountAdmin)
	custom := r.mkRole(authz.ScopeSite, pMedRead)
	r.give(p, starter, noDept, time.Time{})
	r.give(p, custom, noDept, time.Time{})
	page, err := r.svc.ListGrants(r.pctx(p), authz.GrantQuery{Person: p})
	wantNoErr(t, err)
	if len(page.Grants) != 2 {
		t.Fatalf("own grants = %+v", page.Grants)
	}
	for _, g := range page.Grants {
		switch g.Role {
		case starter.ID:
			if g.RoleKey != authz.RoleAccountAdmin {
				t.Errorf("starter grant role key = %q, want %q", g.RoleKey, authz.RoleAccountAdmin)
			}
		case custom.ID:
			if g.RoleKey != "" || g.RoleName != custom.Name || g.RoleName == "" {
				t.Errorf("custom grant role = %q %q, want no key and the role's name %q", g.RoleKey, g.RoleName, custom.Name)
			}
		default:
			t.Errorf("unexpected role %v", g.Role)
		}
	}
}

// F33
func TestListGrants_DepartmentCaller_FiltersToExtent(t *testing.T) {
	r := newRig(t)
	da := r.person()
	r.give(da, r.roleKey(authz.RoleDepartmentAdmin), r.d1, time.Time{})
	deptRole := r.mkRole(authz.ScopeDepartment, pShiftRead)
	siteRole := r.mkRole(authz.ScopeSite, pMedRead)
	in := r.give(r.person(), deptRole, r.d1, time.Time{})
	r.give(r.person(), deptRole, r.d2, time.Time{})
	r.give(r.person(), siteRole, noDept, time.Time{})
	page, err := r.svc.ListGrants(r.pctx(da), authz.GrantQuery{})
	wantNoErr(t, err)
	if len(page.Grants) == 0 {
		t.Fatal("nothing listed")
	}
	for _, g := range page.Grants {
		if g.Department != r.d1 {
			t.Errorf("department caller sees a grant for %v / role %v", g.Department, g.Role)
		}
	}
	seen := false
	for _, g := range page.Grants {
		seen = seen || g.ID == in.ID
	}
	if !seen {
		t.Error("grant inside the extent missing")
	}
	// Asking about someone outside the extent is a filtered (empty) list, never an error.
	outsider := r.give(r.person(), deptRole, r.d2, time.Time{})
	page, err = r.svc.ListGrants(r.pctx(da), authz.GrantQuery{Person: outsider.Person})
	wantNoErr(t, err)
	if len(page.Grants) != 0 {
		t.Errorf("outside grants visible: %+v", page.Grants)
	}
}

func TestListGrants_SiteHolder_SeesEverything_NewestFirst(t *testing.T) {
	r := newRig(t)
	admin := r.admin()
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	var ids []authz.GrantID
	for range 3 {
		wantNoErr(t, r.clk.Advance(time.Minute))
		ids = append(ids, r.give(r.person(), ro, noDept, time.Time{}).ID)
	}
	page, err := r.svc.ListGrants(r.pctx(admin), authz.GrantQuery{Role: ro.ID})
	wantNoErr(t, err)
	if len(page.Grants) != 3 {
		t.Fatalf("%d grants", len(page.Grants))
	}
	for i, g := range page.Grants {
		if g.ID != ids[len(ids)-1-i] {
			t.Errorf("position %d is %v, want newest first", i, g.ID)
		}
	}
	if page.Next != "" {
		t.Error("Next set on the last page")
	}
}

func TestListGrants_Filters(t *testing.T) {
	r := newRig(t)
	roA := r.mkRole(authz.ScopeSite, pMedRead)
	roB := r.mkRole(authz.ScopeDepartment, pShiftRead)
	p1, p2 := r.person(), r.person()
	gA := r.give(p1, roA, noDept, time.Time{})
	gB1 := r.give(p2, roB, r.d1, time.Time{})
	gB2 := r.give(p2, roB, r.d2, time.Time{})
	ids := func(q authz.GrantQuery) []authz.GrantID {
		page, err := r.svc.ListGrants(r.sctx(), q)
		wantNoErr(t, err)
		var out []authz.GrantID
		for _, g := range page.Grants {
			out = append(out, g.ID)
		}
		slices.SortFunc(out, func(a, b authz.GrantID) int { return slices.Compare(a[:], b[:]) })
		return out
	}
	sorted := func(gs ...authz.Grant) []authz.GrantID {
		var out []authz.GrantID
		for _, g := range gs {
			out = append(out, g.ID)
		}
		slices.SortFunc(out, func(a, b authz.GrantID) int { return slices.Compare(a[:], b[:]) })
		return out
	}
	tests := []struct {
		name string
		q    authz.GrantQuery
		want []authz.GrantID
	}{
		{"person", authz.GrantQuery{Person: p2}, sorted(gB1, gB2)},
		{"role", authz.GrantQuery{Role: roA.ID}, sorted(gA)},
		{"department", authz.GrantQuery{Department: r.d2}, sorted(gB2)},
		{"permission", authz.GrantQuery{Permission: pShiftRead}, sorted(gB1, gB2)},
		{"permission nobody holds", authz.GrantQuery{Permission: pPayApprove}, nil},
		{"person and role", authz.GrantQuery{Person: p1, Role: roB.ID}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ids(tc.q); !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// D38 (ag): a role key filters like the role's ID; an unknown key gives what an unknown ID
// gives, an empty page.
func TestListGrants_RoleKey_FiltersLikeTheID(t *testing.T) {
	r := newRig(t)
	da := r.roleKey(authz.RoleDepartmentAdmin)
	other := r.mkRole(authz.ScopeSite, pMedRead)
	p1, p2 := r.person(), r.person()
	g := r.give(p1, da, r.d1, time.Time{})
	r.give(p2, other, noDept, time.Time{})
	ids := func(q authz.GrantQuery) []authz.GrantID {
		page, err := r.svc.ListGrants(r.sctx(), q)
		wantNoErr(t, err)
		var out []authz.GrantID
		for _, g := range page.Grants {
			out = append(out, g.ID)
		}
		return out
	}
	byKey := ids(authz.GrantQuery{RoleKey: authz.RoleDepartmentAdmin})
	if want := ids(authz.GrantQuery{Role: da.ID}); !slices.Equal(byKey, want) || !slices.Equal(byKey, []authz.GrantID{g.ID}) {
		t.Errorf("by key %v, by ID %v, want [%v]", byKey, want, g.ID)
	}
	if got := ids(authz.GrantQuery{RoleKey: "no-such-role"}); len(got) != 0 {
		t.Errorf("unknown key listed %v", got)
	}
	if got := ids(authz.GrantQuery{Role: authz.RoleID(r.person())}); len(got) != 0 {
		t.Errorf("unknown ID listed %v", got)
	}
	// The key does not widen the caller's extent: a department admin still sees only theirs.
	da2 := r.person()
	r.give(da2, da, r.d2, time.Time{})
	page, err := r.svc.ListGrants(r.pctx(da2), authz.GrantQuery{RoleKey: authz.RoleDepartmentAdmin})
	wantNoErr(t, err)
	if len(page.Grants) != 1 || page.Grants[0].Department != r.d2 {
		t.Errorf("department admin saw %+v, want only the grant for their department", page.Grants)
	}
	// And a caller without access.grants.read is refused whatever the key.
	_, err = r.svc.ListGrants(r.pctx(r.person()), authz.GrantQuery{RoleKey: "no-such-role"})
	wantKind(t, err, errs.Permission)
}

func TestListGrants_EndedHiddenUnlessIncluded(t *testing.T) {
	r := newRig(t)
	p := r.person()
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	r.give(p, ro, noDept, r.clk.Now().Add(time.Hour))
	page, err := r.svc.ListGrants(r.sctx(), authz.GrantQuery{Person: p})
	wantNoErr(t, err)
	if len(page.Grants) != 1 {
		t.Fatalf("%d active grants", len(page.Grants))
	}
	wantNoErr(t, r.clk.Advance(2*time.Hour))
	page, err = r.svc.ListGrants(r.sctx(), authz.GrantQuery{Person: p})
	wantNoErr(t, err)
	if len(page.Grants) != 0 {
		t.Fatalf("ended grant listed: %+v", page.Grants)
	}
	page, err = r.svc.ListGrants(r.sctx(), authz.GrantQuery{Person: p, IncludeEnded: true})
	wantNoErr(t, err)
	if len(page.Grants) != 1 {
		t.Fatalf("IncludeEnded gave %d", len(page.Grants))
	}
}

// F34
func TestListGrants_BadCursor_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.ListGrants(r.sctx(), authz.GrantQuery{After: "%%%not-a-cursor"})
	wantField(t, err, "after", "malformed")
}

func TestListGrants_BadLimit_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	for _, n := range []int{-1, authz.MaxPageSize + 1, 100000} {
		_, err := r.svc.ListGrants(r.sctx(), authz.GrantQuery{Limit: n})
		wantField(t, err, "limit", "range")
	}
	for _, n := range []int{0, 1, authz.MaxPageSize} {
		_, err := r.svc.ListGrants(r.sctx(), authz.GrantQuery{Limit: n})
		wantNoErr(t, err)
	}
}

func TestListGrants_DefaultPageSize(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	for range authz.DefaultPageSize + 5 {
		r.give(r.person(), ro, noDept, time.Time{})
	}
	page, err := r.svc.ListGrants(r.sctx(), authz.GrantQuery{Role: ro.ID})
	wantNoErr(t, err)
	if len(page.Grants) != authz.DefaultPageSize || page.Next == "" {
		t.Fatalf("%d grants, next=%q; want %d and a cursor", len(page.Grants), page.Next, authz.DefaultPageSize)
	}
}

func TestListGrants_Keyset_OverTwoHundredFiftyGrants(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	want := map[authz.GrantID]bool{}
	for i := range 250 {
		if i%7 != 0 { // some share a created_at, so the id tiebreak is exercised
			wantNoErr(t, r.clk.Advance(time.Second))
		}
		want[r.give(r.person(), ro, noDept, time.Time{}).ID] = true
	}
	seen := map[authz.GrantID]bool{}
	var prev authz.Grant
	q := authz.GrantQuery{Role: ro.ID, Limit: 100}
	pages := 0
	for {
		page, err := r.svc.ListGrants(r.sctx(), q)
		wantNoErr(t, err)
		pages++
		for _, g := range page.Grants {
			if seen[g.ID] {
				t.Fatalf("grant %v seen twice", g.ID)
			}
			seen[g.ID] = true
			if !prev.ID.IsZero() && g.CreatedAt.After(prev.CreatedAt) {
				t.Fatalf("not newest first: %v after %v", g.CreatedAt, prev.CreatedAt)
			}
			prev = g
		}
		if page.Next == "" {
			break
		}
		if pages > 5 {
			t.Fatal("pagination does not end")
		}
		q.After = page.Next
	}
	if len(seen) != 250 || pages != 3 {
		t.Fatalf("saw %d grants over %d pages, want 250 over 3", len(seen), pages)
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("grant %v never listed", id)
		}
	}
}

func TestListGrants_StoreFails_ReturnsStoreKind(t *testing.T) {
	r := newRig(t)
	r.fdb.set(func(f *faultDB) { f.readErr = errDown })
	_, err := r.svc.ListGrants(r.sctx(), authz.GrantQuery{})
	wantKind(t, err, errs.Unavailable)
}

// F51 (D37): an admin a separation-of-duties rule denies the permission is not an effective
// access admin, so revoking the last effective one is refused.
func TestRevoke_LastAdminDeniedByRule_ReturnsConflict(t *testing.T) {
	r := newRig(t)
	first, second := r.admin(), r.admin()
	ro := r.mkRole(authz.ScopeSite, pMedRead)
	r.give(second, ro, noDept, time.Time{})
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: ro.ID, Permission: authz.PermissionGrantsManage})
	wantNoErr(t, err) // the first admin remains effective
	wantCode(t, r.svc.Revoke(r.sctx(), r.grantsOf(first)[0].ID), errs.Conflict, "last_access_admin")
	if len(r.grantsOf(first)) != 1 {
		t.Error("the last effective admin's grant was deleted")
	}
}
