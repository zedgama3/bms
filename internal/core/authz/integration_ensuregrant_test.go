//go:build integration

package authz_test

// EnsureGrant, the writer-only bootstrap grant: plan/design/authz.md, "Amendment
// 2026-10-28 (approved, owner, D53)", F72 to F75. People are invented.

import (
	"context"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func TestEnsureGrant_Creates_AuditsOperatorAndPublishes(t *testing.T) {
	r := newRig(t)
	p := r.person()
	ro := r.roleKey(authz.RoleAccessAdmin)
	auditBefore, sigBefore := len(r.entries("access.grant.created")), len(r.sigsOf(sigCreated))

	// No actor in ctx at all: a command.
	g, created, err := r.svc.EnsureGrant(ctxT(t), authz.GrantInput{Person: p, Role: ro.ID})
	wantNoErr(t, err)
	if !created {
		t.Fatal("created = false for a new grant")
	}
	if g.ID.IsZero() || g.Person != p || g.Role != ro.ID || !g.Department.IsZero() {
		t.Fatalf("grant = %+v", g)
	}
	// The person now holds the role's permissions.
	if got := r.grantsOf(p); len(got) != 1 || got[0].ID != g.ID {
		t.Errorf("grantsOf = %+v", got)
	}
	es := r.entries("access.grant.created")
	if len(es) != auditBefore+1 {
		t.Fatalf("%d audit entries, want %d", len(es), auditBefore+1)
	}
	last := es[len(es)-1]
	if last.Actor.Kind != audit.ActorOperator {
		t.Errorf("actor kind = %v, want ActorOperator", last.Actor.Kind)
	}
	if last.Subject != p {
		t.Error("audit subject is not the grantee")
	}
	sigs := r.sigsOf(sigCreated)
	if len(sigs) != sigBefore+1 {
		t.Fatalf("%d access.grant-created signals, want %d", len(sigs), sigBefore+1)
	}
	checkPayload(t, sigs[len(sigs)-1], g, false)
}

// F72
func TestEnsureGrant_DepartmentScope_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	p := r.person()
	dept := model.NewID[model.DepartmentID]()
	deptRole := r.roleKey(authz.RoleDepartmentAdmin)
	siteRole := r.roleKey(authz.RoleAccessAdmin)
	cases := []struct {
		name string
		in   authz.GrantInput
	}{
		{"department role", authz.GrantInput{Person: p, Role: deptRole.ID}},
		{"department role with department", authz.GrantInput{Person: p, Role: deptRole.ID, Department: dept}},
		{"site role with department", authz.GrantInput{Person: p, Role: siteRole.ID, Department: dept}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, created, err := r.svc.EnsureGrant(ctxT(t), tc.in)
			wantField(t, err, "scope", "")
			if created {
				t.Error("created = true on error")
			}
		})
	}
	if len(r.grantsOf(p)) != 0 {
		t.Error("a refused grant was written")
	}
}

func TestEnsureGrant_RequiredFields_ReturnInvalid(t *testing.T) {
	r := newRig(t)
	ro := r.roleKey(authz.RoleAccessAdmin)
	_, _, err := r.svc.EnsureGrant(ctxT(t), authz.GrantInput{Role: ro.ID})
	wantKind(t, err, errs.Invalid)
	_, _, err = r.svc.EnsureGrant(ctxT(t), authz.GrantInput{Person: r.person()})
	wantKind(t, err, errs.Invalid)
}

func TestEnsureGrant_UnknownRole_ReturnsNotFound(t *testing.T) {
	r := newRig(t)
	_, _, err := r.svc.EnsureGrant(ctxT(t), authz.GrantInput{Person: r.person(), Role: model.NewID[authz.RoleID]()})
	wantKind(t, err, errs.NotFound)
}

// F73
func TestEnsureGrant_RuleViolation_ReturnsConflict(t *testing.T) {
	r := newRig(t)
	roleA := r.mkRole(authz.ScopeSite, pPayApprove)
	roleB := r.mkRole(authz.ScopeSite, pPayAudit)
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: roleA.ID, Permission: pPayAudit, Note: "approvers do not audit"})
	wantNoErr(t, err)
	p := r.person()
	r.give(p, roleA, noDept, time.Time{})

	_, created, err := r.svc.EnsureGrant(ctxT(t), authz.GrantInput{Person: p, Role: roleB.ID})
	wantKind(t, err, errs.Conflict)
	if created {
		t.Error("created = true on error")
	}
	if len(r.grantsOf(p)) != 1 {
		t.Error("the violating grant was written")
	}
}

// F74
func TestEnsureGrant_Existing_ReturnsFalse(t *testing.T) {
	r := newRig(t)
	p := r.person()
	ro := r.roleKey(authz.RoleAccessAdmin)
	first, created, err := r.svc.EnsureGrant(ctxT(t), authz.GrantInput{Person: p, Role: ro.ID})
	wantNoErr(t, err)
	if !created {
		t.Fatal("first call: created = false")
	}
	auditBefore, sigBefore := len(r.entries("access.grant.created")), len(r.sigsOf(sigCreated))
	again, created, err := r.svc.EnsureGrant(ctxT(t), authz.GrantInput{Person: p, Role: ro.ID})
	wantNoErr(t, err)
	if created {
		t.Error("second call: created = true")
	}
	if again.ID != first.ID {
		t.Error("the existing grant was not returned")
	}
	if len(r.grantsOf(p)) != 1 {
		t.Error("a duplicate grant was written")
	}
	if len(r.entries("access.grant.created")) != auditBefore || len(r.sigsOf(sigCreated)) != sigBefore {
		t.Error("an existing grant recorded an audit entry or published a signal")
	}
}

// F75
func TestEnsureGrant_ReadOnly_ReturnsUnavailable(t *testing.T) {
	r := newRig(t)
	p := r.person()
	ro := r.roleKey(authz.RoleAccessAdmin)
	r.fdb.set(func(f *faultDB) { f.ro = true })
	_, created, err := r.svc.EnsureGrant(ctxT(t), authz.GrantInput{Person: p, Role: ro.ID})
	wantReadOnly(t, err)
	if created {
		t.Error("created = true on error")
	}
}

func TestEnsureGrant_CanceledContext_ReturnsCanceled(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(ctxT(t))
	cancel()
	_, _, err := r.svc.EnsureGrant(ctx, authz.GrantInput{Person: r.person(), Role: r.roleKey(authz.RoleAccessAdmin).ID})
	wantKind(t, err, errs.Canceled)
}
