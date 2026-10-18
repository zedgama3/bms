//go:build integration

package authz_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// F52
func TestCreateRule_NoPermission_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite)
	in := authz.RuleInput{Role: ro.ID, Permission: pMedRead}
	_, err := r.svc.CreateRule(r.pctx(r.person()), in)
	wantKind(t, err, errs.Permission)
	_, err = r.svc.CreateRule(r.pctx(r.holder(pRolesManage, pRolesRead)), in)
	wantKind(t, err, errs.Permission)
	_, err = r.svc.CreateRule(ctxT(t), in)
	wantKind(t, err, errs.Unauthenticated)
}

func TestCreateRule_Success_ByPermissionAndByLevel_Audits(t *testing.T) {
	r := newRig(t)
	mgr := r.holder(pRulesManage)
	ro := r.mkRole(authz.ScopeSite)
	a, err := r.svc.CreateRule(r.pctx(mgr), authz.RuleInput{Role: ro.ID, Permission: pMedRead, Note: "keep them apart"})
	wantNoErr(t, err)
	if a.ID.IsZero() || a.Role != ro.ID || a.Permission != pMedRead || a.Reaches != 0 || a.Note != "keep them apart" || a.CreatedAt.IsZero() {
		t.Errorf("rule = %+v", a)
	}
	b, err := r.svc.CreateRule(r.pctx(mgr), authz.RuleInput{Role: ro.ID, Reaches: model.SensitivityPersonal})
	wantNoErr(t, err)
	if b.Permission != "" || b.Reaches != model.SensitivityPersonal {
		t.Errorf("rule = %+v", b)
	}
	if len(r.entries("access.rule.created")) != 2 {
		t.Errorf("actions = %v", r.actions())
	}
}

// F53
func TestCreateRule_BothOrNeither_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite)
	tests := []struct {
		name  string
		in    authz.RuleInput
		field string
		code  string
	}{
		{"both", authz.RuleInput{Role: ro.ID, Permission: pMedRead, Reaches: model.SensitivityPersonal}, "permission|reaches", "one_of"},
		{"neither", authz.RuleInput{Role: ro.ID}, "permission|reaches", "one_of"},
		{"malformed permission", authz.RuleInput{Role: ro.ID, Permission: "Bad Name"}, "permission", "malformed"},
		{"invalid level", authz.RuleInput{Role: ro.ID, Reaches: 9}, "reaches", "one_of"},
		{"note too long", authz.RuleInput{Role: ro.ID, Permission: pMedRead, Note: strings.Repeat("n", authz.MaxNoteBytes+1)}, "note", "too_long"},
		{"role required", authz.RuleInput{Permission: pMedRead}, "role", "required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.svc.CreateRule(r.sctx(), tc.in)
			if tc.field == "permission|reaches" {
				wantKind(t, err, errs.Invalid)
				found := false
				for _, fe := range validate.Fields(err) { // the spec names permission/reaches one_of
					found = found || ((fe.Field == "permission" || fe.Field == "reaches") && fe.Code == tc.code)
				}
				if !found {
					t.Fatalf("no one_of on permission or reaches in %v", validate.Fields(err))
				}
				return
			}
			wantField(t, err, tc.field, tc.code)
		})
	}
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: ro.ID, Permission: pMedRead, Note: strings.Repeat("n", authz.MaxNoteBytes)})
	wantNoErr(t, err)
}

// F54
func TestCreateRule_UnknownRole_ReturnsNotFound(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: model.NewID[authz.RoleID](), Permission: pMedRead})
	wantKind(t, err, errs.NotFound)
}

// F55
func TestCreateRule_Duplicate_ReturnsConflict(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite)
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: ro.ID, Permission: pMedRead})
	wantNoErr(t, err)
	_, err = r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: ro.ID, Permission: pMedRead, Note: "again"})
	wantCode(t, err, errs.Conflict, "duplicate")
	_, err = r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: ro.ID, Reaches: model.SensitivityPersonal})
	wantNoErr(t, err)
	_, err = r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: ro.ID, Reaches: model.SensitivityPersonal})
	wantCode(t, err, errs.Conflict, "duplicate")
}

func TestCreateRule_ReadOnly_ReturnsReadOnly(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite)
	r.fdb.set(func(f *faultDB) { f.ro = true })
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: ro.ID, Permission: pMedRead})
	wantReadOnly(t, err)
}

// F56
func TestDeleteRule_Unknown_ReturnsNotFound(t *testing.T) {
	r := newRig(t)
	wantKind(t, r.svc.DeleteRule(r.sctx(), model.NewID[authz.RuleID]()), errs.NotFound)
}

func TestDeleteRule_NoPermission_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite)
	rule, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: ro.ID, Permission: pMedRead})
	wantNoErr(t, err)
	wantKind(t, r.svc.DeleteRule(r.pctx(r.person()), rule.ID), errs.Permission)
	wantKind(t, r.svc.DeleteRule(ctxT(t), rule.ID), errs.Unauthenticated)
}

func TestDeleteRule_Success_AuditsAndIsNotIdempotent(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite)
	rule, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: ro.ID, Permission: pMedRead})
	wantNoErr(t, err)
	wantNoErr(t, r.svc.DeleteRule(r.sctx(), rule.ID))
	if len(r.entries("access.rule.deleted")) != 1 {
		t.Errorf("actions = %v", r.actions())
	}
	wantKind(t, r.svc.DeleteRule(r.sctx(), rule.ID), errs.NotFound)
	rules, err := r.svc.ListRules(r.sctx())
	wantNoErr(t, err)
	if len(rules) != 0 {
		t.Errorf("rules left: %+v", rules)
	}
}

func TestListRules_NoPermission_ReturnsPermission(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.ListRules(r.pctx(r.person()))
	wantKind(t, err, errs.Permission)
	_, err = r.svc.ListRules(ctxT(t))
	wantKind(t, err, errs.Unauthenticated)
	_, err = r.svc.ListRules(r.pctx(r.holder(pRolesRead)))
	wantNoErr(t, err)
}

func TestListRules_SortedByRoleName_AndAffected(t *testing.T) {
	r := newRig(t)
	zed, err := r.svc.CreateRole(r.sctx(), authz.RoleInput{Name: "Zed", Scope: authz.ScopeSite, Permissions: []authz.Permission{pPayApprove}})
	wantNoErr(t, err)
	amy, err := r.svc.CreateRole(r.sctx(), authz.RoleInput{Name: "Amy", Scope: authz.ScopeSite, Permissions: []authz.Permission{pMedRead}})
	wantNoErr(t, err)
	// Two people hold Zed and a forbidden permission through another role; one only Zed.
	extra := r.mkRole(authz.ScopeSite, pPayAudit)
	for range 2 {
		p := r.person()
		r.give(p, zed, noDept, time.Time{})
		r.give(p, extra, noDept, time.Time{})
	}
	r.give(r.person(), zed, noDept, time.Time{})
	_, err = r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: zed.ID, Permission: pPayAudit})
	wantNoErr(t, err)
	_, err = r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: amy.ID, Permission: pMedWrite})
	wantNoErr(t, err)
	rules, err := r.svc.ListRules(r.sctx())
	wantNoErr(t, err)
	if len(rules) != 2 || rules[0].Role != amy.ID || rules[1].Role != zed.ID {
		t.Fatalf("rules = %+v, want Amy's then Zed's", rules)
	}
	if rules[0].Affected != 0 || rules[1].Affected != 2 {
		t.Errorf("Affected = %d / %d, want 0 / 2", rules[0].Affected, rules[1].Affected)
	}
}

// "Creating a rule that people already violate is allowed ... Affected shows how many."
func TestCreateRule_ExistingViolators_AllowedAndCounted(t *testing.T) {
	r := newRig(t)
	a := r.mkRole(authz.ScopeSite, pPayApprove)
	b := r.mkRole(authz.ScopeSite, pMedRead)
	p := r.person()
	r.give(p, a, noDept, time.Time{})
	r.give(p, b, noDept, time.Time{})
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: a.ID, Reaches: model.SensitivitySensitive})
	wantNoErr(t, err)
	rules, err := r.svc.ListRules(r.sctx())
	wantNoErr(t, err)
	if len(rules) != 1 || rules[0].Affected != 1 {
		t.Fatalf("rules = %+v", rules)
	}
}

func TestRules_StoreFails_ReturnsStoreKind(t *testing.T) {
	r := newRig(t)
	ro := r.mkRole(authz.ScopeSite)
	r.fdb.set(func(f *faultDB) { f.readErr, f.txErr = errTimeout, errTimeout })
	_, err := r.svc.ListRules(r.sctx())
	wantKind(t, err, errs.Timeout)
	_, err = r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: ro.ID, Permission: pMedRead})
	wantKind(t, err, errs.Timeout)
}
