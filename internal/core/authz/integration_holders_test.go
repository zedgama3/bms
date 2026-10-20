//go:build integration

package authz_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func sortedIDs(ids ...model.PersonID) []model.PersonID {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(a, b model.PersonID) int { return strings.Compare(a.String(), b.String()) })
	return out
}

func wantHolders(t *testing.T, r *irig, p authz.Permission, want ...model.PersonID) {
	t.Helper()
	got, err := r.svc.HoldersOf(ctxT(t), p) // a system read: no actor in the context
	wantNoErr(t, err)
	if got == nil {
		t.Fatalf("HoldersOf(%s) = nil, want a non-nil slice", p)
	}
	w := sortedIDs(want...)
	if len(w) == 0 {
		w = []model.PersonID{}
	}
	if !slices.Equal(got, w) {
		t.Fatalf("HoldersOf(%s) = %v, want %v", p, got, w)
	}
}

// F68
func TestHoldersOf_Table(t *testing.T) {
	r := newRig(t)
	roleP := r.mkRole(authz.ScopeSite, pPayApprove)

	live := r.person() // an active site-wide grant, no end
	r.give(live, roleP, model.DepartmentID{}, time.Time{})

	future := r.person() // a grant that ends later
	r.give(future, r.mkRole(authz.ScopeSite, pPayApprove, pPayAudit), model.DepartmentID{}, r.clk.Now().Add(time.Hour))

	twice := r.person() // two roles carrying p: listed once
	r.give(twice, roleP, model.DepartmentID{}, time.Time{})
	r.give(twice, r.mkRole(authz.ScopeSite, pPayApprove), model.DepartmentID{}, time.Time{})

	ended := r.person() // a grant that ends soon, checked after the clock moves
	r.give(ended, r.mkRole(authz.ScopeSite, pPayApprove), model.DepartmentID{}, r.clk.Now().Add(30*time.Minute))

	forbidden := r.person() // a rule forbids p for the holder of another role they hold
	other := r.mkRole(authz.ScopeSite, pPayAudit)
	r.give(forbidden, roleP, model.DepartmentID{}, time.Time{})
	r.give(forbidden, other, model.DepartmentID{}, time.Time{})

	r.deptHolder(r.d1, pShiftManage) // a department grant is not site-wide
	_ = r.person()                   // someone with no grant at all
	// The superuser holds no grant, so they never appear (authz decision 6).

	wantHolders(t, r, pPayApprove, live, future, twice, ended, forbidden)

	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: other.ID, Permission: pPayApprove})
	wantNoErr(t, err)
	wantNoErr(t, r.clk.Advance(30*time.Minute)) // now == ends_at: no longer active
	wantHolders(t, r, pPayApprove, live, future, twice)

	wantNoErr(t, r.clk.Advance(time.Hour))
	wantHolders(t, r, pPayApprove, live, twice)

	// A department-scoped permission is held site-wide only by a site grant.
	wantHolders(t, r, pShiftManage)
	site := r.holder(pShiftManage)
	wantHolders(t, r, pShiftManage, site)
	wantHolders(t, r, pMedRead)
}

// F68: a rule by level forbids it too, and deleting the rule restores the holder.
func TestHoldersOf_RuleByLevelAndDeleted(t *testing.T) {
	r := newRig(t)
	p := r.person()
	roleA := r.mkRole(authz.ScopeSite, pMedRead)
	roleB := r.mkRole(authz.ScopeSite, pPayAudit)
	r.give(p, roleA, model.DepartmentID{}, time.Time{})
	r.give(p, roleB, model.DepartmentID{}, time.Time{})
	wantHolders(t, r, pMedRead, p)
	rule, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: roleB.ID, Reaches: model.SensitivitySensitive})
	wantNoErr(t, err)
	wantHolders(t, r, pMedRead)
	wantHolders(t, r, pPayAudit, p) // internal level: not reached by a sensitive rule
	wantNoErr(t, r.svc.DeleteRule(r.sctx(), rule.ID))
	wantHolders(t, r, pMedRead, p)
}

// Holders come back in person ID order.
func TestHoldersOf_InPersonIDOrder(t *testing.T) {
	r := newRig(t)
	role := r.mkRole(authz.ScopeSite, pPayApprove)
	var ids []model.PersonID
	for range 6 {
		p := r.person()
		r.give(p, role, model.DepartmentID{}, time.Time{})
		ids = append(ids, p)
	}
	got, err := r.svc.HoldersOf(ctxT(t), pPayApprove)
	wantNoErr(t, err)
	if !slices.Equal(got, sortedIDs(ids...)) {
		t.Fatalf("HoldersOf = %v, want %v", got, sortedIDs(ids...))
	}
}

// F70: a permission only the base role carries has no appointed holders; nobody is
// enumerated, however many people exist.
func TestHoldersOf_BaseRoleOnly_ReturnsEmpty(t *testing.T) {
	r := newRig(t)
	base := r.roleKey(authz.RoleParticipant)
	_, err := r.svc.UpdateRole(r.sctx(), base.ID, authz.RoleInput{Name: "Everyone", Permissions: []authz.Permission{pGreetRead, pPayApprove}})
	wantNoErr(t, err)
	a, b := r.person(), r.person()
	wantNoErr(t, r.svc.Allow(r.pctx(a), pPayApprove)) // they do hold it, through the base role
	wantNoErr(t, r.svc.Allow(r.pctx(b), pPayApprove))
	// A grant of another role, one without p, changes nothing.
	r.give(a, r.mkRole(authz.ScopeSite, pPayAudit), model.DepartmentID{}, time.Time{})
	wantHolders(t, r, pPayApprove)
	// pGreetRead is declared with the participant starter role by Seed.
	wantHolders(t, r, pGreetRead)
}

// F71
func TestHoldersOf_BaseAndGrant_ReturnsGrantHoldersOnce(t *testing.T) {
	r := newRig(t)
	base := r.roleKey(authz.RoleParticipant)
	_, err := r.svc.UpdateRole(r.sctx(), base.ID, authz.RoleInput{Name: "Everyone", Permissions: []authz.Permission{pGreetRead, pPayApprove}})
	wantNoErr(t, err)
	a := r.person()
	_ = r.person() // an ordinary participant
	r.give(a, r.mkRole(authz.ScopeSite, pPayApprove, pMedRead), model.DepartmentID{}, time.Time{})
	r.give(a, r.mkRole(authz.ScopeSite, pPayApprove), model.DepartmentID{}, time.Time{})
	wantHolders(t, r, pPayApprove, a)
}

// F69
func TestHoldersOf_StoreFails_ReturnsKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		kind errs.Kind
	}{
		{"unavailable", errDown, errs.Unavailable},
		{"timeout", errTimeout, errs.Timeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.give(r.person(), r.mkRole(authz.ScopeSite, pPayApprove), model.DepartmentID{}, time.Time{})
			r.fdb.set(func(f *faultDB) { f.readErr = tc.err })
			got, err := r.svc.HoldersOf(ctxT(t), pPayApprove)
			wantKind(t, err, tc.kind)
			if got != nil {
				t.Errorf("HoldersOf returned %v with an error", got)
			}
		})
	}
}

// A read-only copy answers: the query is a read.
func TestHoldersOf_ReadOnlyCopy_StillAnswers(t *testing.T) {
	r := newRig(t)
	p := r.person()
	r.give(p, r.mkRole(authz.ScopeSite, pPayApprove), model.DepartmentID{}, time.Time{})
	r.fdb.set(func(f *faultDB) { f.ro = true })
	wantHolders(t, r, pPayApprove, p)
}

// A separation-of-duties rule on the base role applies to everyone, so it forbids the
// permission for every grant holder and nobody is returned; deleting it restores them.
func TestHoldersOf_RuleOnBaseRole_ReturnsEmpty(t *testing.T) {
	r := newRig(t)
	a, b := r.holder(pPayApprove), r.holder(pPayApprove)
	wantHolders(t, r, pPayApprove, a, b)
	base := r.roleKey(authz.RoleParticipant)
	rule, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: base.ID, Permission: pPayApprove})
	wantNoErr(t, err)
	wantHolders(t, r, pPayApprove)
	wantNoErr(t, r.svc.DeleteRule(r.sctx(), rule.ID))
	wantHolders(t, r, pPayApprove, a, b)
}
