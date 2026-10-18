//go:build integration

package authz_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

type exportDoc struct {
	Grants []map[string]any `json:"grants"`
}

func decodeExport(t *testing.T, raw json.RawMessage) exportDoc {
	t.Helper()
	var d exportDoc
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("export is not the expected JSON: %v", err)
	}
	return d
}

// F59
func TestExportPerson_ZeroID_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.ExportPerson(ctxT(t), model.PersonID{})
	wantKind(t, err, errs.Invalid)
}

func TestExportPerson_NoGrants_ReturnsEmptyArray(t *testing.T) {
	r := newRig(t)
	raw, err := r.svc.ExportPerson(ctxT(t), r.person())
	wantNoErr(t, err)
	d := decodeExport(t, raw)
	if d.Grants == nil || len(d.Grants) != 0 {
		t.Fatalf("export = %s, want {\"grants\":[]}", raw)
	}
}

func TestExportPerson_ListsGrantsIncludingEnded_WithRoleKeyAndName(t *testing.T) {
	r := newRig(t)
	p := r.person()
	r.give(p, r.roleKey(authz.RoleAuditReviewer), noDept, time.Time{})
	named, err := r.svc.CreateRole(r.sctx(), authz.RoleInput{Name: "Gate Coordinator", Scope: authz.ScopeSite})
	wantNoErr(t, err)
	r.give(p, named, noDept, r.clk.Now().Add(time.Minute))
	wantNoErr(t, r.clk.Advance(time.Hour))
	raw, err := r.svc.ExportPerson(ctxT(t), p)
	wantNoErr(t, err)
	d := decodeExport(t, raw)
	if len(d.Grants) != 2 {
		t.Fatalf("export = %s", raw)
	}
	text := string(raw)
	for _, want := range []string{authz.RoleAuditReviewer, "Gate Coordinator"} {
		if !strings.Contains(text, want) {
			t.Errorf("export lacks %q: %s", want, text)
		}
	}
}

func TestExportPerson_OtherPeoplesGrantsNotIncluded(t *testing.T) {
	r := newRig(t)
	p, q := r.person(), r.person()
	r.give(q, r.mkRole(authz.ScopeSite), noDept, time.Time{})
	raw, err := r.svc.ExportPerson(ctxT(t), p)
	wantNoErr(t, err)
	if d := decodeExport(t, raw); len(d.Grants) != 0 {
		t.Fatalf("export = %s", raw)
	}
}

func TestExportPerson_ReadOnlyCopy_Works(t *testing.T) {
	r := newRig(t)
	p := r.person()
	r.give(p, r.mkRole(authz.ScopeSite), noDept, time.Time{})
	r.fdb.set(func(f *faultDB) { f.ro = true })
	_, err := r.svc.ExportPerson(ctxT(t), p)
	wantNoErr(t, err)
}

// F59
func TestDeletePerson_ZeroID_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.DeletePerson(ctxT(t), model.PersonID{})
	wantKind(t, err, errs.Invalid)
}

func TestDeletePerson_DeletesGrantsIncludingEnded_Audits_Idempotent(t *testing.T) {
	r := newRig(t)
	p := r.person()
	r.give(p, r.mkRole(authz.ScopeSite, pMedRead), noDept, time.Time{})
	r.give(p, r.mkRole(authz.ScopeSite, pPayAudit), noDept, r.clk.Now().Add(time.Minute))
	wantNoErr(t, r.clk.Advance(time.Hour)) // the second has ended
	n, err := r.svc.DeletePerson(ctxT(t), p)
	wantNoErr(t, err)
	if n != 2 {
		t.Fatalf("deleted %d, want 2", n)
	}
	if len(r.grantsOf(p)) != 0 {
		t.Error("grants remain")
	}
	es := r.entries("access.grants.deleted")
	if len(es) != 1 || es[0].Subject != p {
		t.Fatalf("audit = %+v", es)
	}
	n, err = r.svc.DeletePerson(ctxT(t), p)
	wantNoErr(t, err)
	if n != 0 {
		t.Errorf("second call deleted %d", n)
	}
}

func TestDeletePerson_ClearsGrantedByOnGrantsTheyMade(t *testing.T) {
	r := newRig(t)
	granter := r.admin()
	target := r.person()
	_, err := r.svc.Grant(r.pctx(granter), authz.GrantInput{Person: target, Role: r.mkRole(authz.ScopeSite, pMedRead).ID})
	wantNoErr(t, err)
	r.admin() // another admin, so deleting the granter is allowed
	if got := r.grantsOf(target)[0].GrantedBy; got != granter {
		t.Fatalf("GrantedBy = %v", got)
	}
	_, err = r.svc.DeletePerson(ctxT(t), granter)
	wantNoErr(t, err)
	g := r.grantsOf(target)
	if len(g) != 1 || !g[0].GrantedBy.IsZero() {
		t.Fatalf("grants of target = %+v, want GrantedBy cleared", g)
	}
}

// F60
func TestDeletePerson_LastAccessAdmin_ReturnsConflict(t *testing.T) {
	r := newRig(t)
	admin := r.admin()
	_, err := r.svc.DeletePerson(ctxT(t), admin)
	wantCode(t, err, errs.Conflict, "last_access_admin")
	if len(r.grantsOf(admin)) != 1 {
		t.Error("grants were deleted despite the conflict")
	}
	other := r.admin()
	_, err = r.svc.DeletePerson(ctxT(t), admin)
	wantNoErr(t, err)
	_ = other
}

// F60 (D37): another holder a separation-of-duties rule denies the permission does not count.
func TestDeletePerson_LastAdminDeniedByRule_ReturnsConflict(t *testing.T) {
	r := newRig(t)
	admin := r.admin()
	other := r.holder(pGrantsMgmt)
	barred := r.mkRole(authz.ScopeSite, pMedRead)
	r.give(other, barred, noDept, time.Time{})
	_, err := r.svc.CreateRule(r.sctx(), authz.RuleInput{Role: barred.ID, Permission: pGrantsMgmt})
	wantNoErr(t, err)
	_, err = r.svc.DeletePerson(ctxT(t), admin)
	wantCode(t, err, errs.Conflict, "last_access_admin")
	if len(r.grantsOf(admin)) != 1 {
		t.Error("grants were deleted despite the conflict")
	}
}

// F61
func TestDeletePerson_ReadOnly_ReturnsReadOnly(t *testing.T) {
	r := newRig(t)
	r.fdb.set(func(f *faultDB) { f.ro = true })
	_, err := r.svc.DeletePerson(ctxT(t), r.person())
	wantReadOnly(t, err)
}

func TestPersonData_StoreFails_ReturnsStoreKind(t *testing.T) {
	r := newRig(t)
	p := r.person()
	r.fdb.set(func(f *faultDB) { f.readErr, f.txErr, f.execErr = errDown, errDown, errDown })
	_, err := r.svc.ExportPerson(ctxT(t), p)
	wantKind(t, err, errs.Unavailable)
	_, err = r.svc.DeletePerson(ctxT(t), p)
	wantKind(t, err, errs.Unavailable)
}

// F66
func TestDeletePerson_PublishesGrantRevokedPerGrant(t *testing.T) {
	r := newRig(t)
	r.admin() // another admin, so deleting the person is allowed
	p := r.person()
	g1 := r.give(p, r.mkRole(authz.ScopeSite, pMedRead), noDept, time.Time{})
	g2 := r.give(p, r.mkRole(authz.ScopeDepartment, pShiftRead), r.d1, time.Time{})
	g3 := r.give(p, r.mkRole(authz.ScopeSite, pPayAudit), noDept, r.clk.Now().Add(time.Hour))
	wantNoErr(t, r.clk.Advance(2*time.Hour)) // g3 has ended; it is deleted and announced too
	before := len(r.sigsOf(sigRevoked))
	n, err := r.svc.DeletePerson(ctxT(t), p)
	wantNoErr(t, err)
	if n != 3 {
		t.Fatalf("deleted %d grants, want 3", n)
	}
	sigs := r.sigsOf(sigRevoked)[before:]
	if len(sigs) != 3 {
		t.Fatalf("%d grant-revoked signals, want one per removed grant", len(sigs))
	}
	seen := map[authz.GrantID]bool{}
	for _, s := range sigs {
		gid, err := signals.Get[authz.GrantID](s.Payload, "grant")
		wantNoErr(t, err)
		seen[gid] = true
	}
	for _, g := range []authz.Grant{g1, g2, g3} {
		if !seen[g.ID] {
			t.Errorf("no signal for grant %v", g.ID)
		}
	}
	// A person with no grants announces nothing.
	before = len(r.sigsOf(sigRevoked))
	_, err = r.svc.DeletePerson(ctxT(t), r.person())
	wantNoErr(t, err)
	if len(r.sigsOf(sigRevoked)) != before {
		t.Error("a person without grants published a signal")
	}
}
