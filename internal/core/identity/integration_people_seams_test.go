//go:build integration

package identity_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// seamsSvc is a Service whose checker denies every permission: the seams must not ask.
// wantAnyFieldCode checks that err carries a field error with code, whatever its field.
func wantAnyFieldCode(t testing.TB, err error, code string) {
	t.Helper()
	for _, fe := range validate.Fields(err) {
		if fe.Code == code {
			return
		}
	}
	t.Fatalf("no field error with code %q in %v", code, validate.Fields(err))
}

func seamsSvc(p *prig) *identity.Service { return p.other(withPerms(allowing())) }

func (p *prig) linkedEntries() []audit.Entry { return p.entries2("identity.account.linked") }

func TestEmailOf_Account_ReturnsTheLoginAddress(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	svc := seamsSvc(p)
	before := len(p.rec.Recorded())
	got, err := svc.EmailOf(ctxT(t), u.sess.Person)
	wantNoErr(t, err)
	if got.Canonical() != u.email.Canonical() {
		t.Errorf("EmailOf = %q, want the account's address", got)
	}
	if len(p.rec.Recorded()) != before {
		t.Error("EmailOf recorded an audit entry")
	}
}

func TestEmailOf_Works_OnAReadOnlyCopy(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	p.fdb.set(func(f *faultDB) { f.ro = true })
	if _, err := seamsSvc(p).EmailOf(ctxT(t), u.sess.Person); err != nil {
		t.Fatalf("EmailOf on a read-only copy: %v", err)
	}
}

// F64
func TestEmailOf_PersonWithoutAnAccount_NotFound(t *testing.T) {
	p := newPoolRig(t)
	p.signUp("ada.quill")
	_, err := seamsSvc(p).EmailOf(ctxT(t), newPersonID())
	wantKind(t, err, errs.NotFound)
}

func TestLinkAccount_MovesTheAccountToTheGivenPerson(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("link.target")
	other := p.signUp("bystander")
	cookie := p.cookie(u.email, u.pw)
	target := newPersonID()
	svc := seamsSvc(p)
	accountsBefore := p.accountCount()
	published := len(p.bus.Published())

	acct, prev, err := svc.LinkAccount(ctxT(t), u.email, target)
	wantNoErr(t, err)
	if acct != u.sess.Account || prev != u.sess.Person {
		t.Errorf("returned account %v and previous person %v, want %v and %v", acct, prev, u.sess.Account, u.sess.Person)
	}
	if p.accountCount() != accountsBefore {
		t.Error("the number of accounts changed")
	}
	got, err := svc.EmailOf(ctxT(t), target)
	wantNoErr(t, err)
	if got.Canonical() != u.email.Canonical() {
		t.Errorf("EmailOf(target) = %q", got)
	}
	_, err = svc.EmailOf(ctxT(t), prev)
	wantKind(t, err, errs.NotFound)
	if _, err := svc.EmailOf(ctxT(t), other.sess.Person); err != nil {
		t.Errorf("another account was disturbed: %v", err)
	}

	es := p.linkedEntries()
	if len(es) != 1 {
		t.Fatalf("%d identity.account.linked entries, want 1", len(es))
	}
	e := es[0]
	if e.Outcome != audit.Succeeded || e.Subject != target || e.Record.Kind != "identity.account" || e.Record.ID != [16]byte(u.sess.Account) {
		t.Errorf("entry = %+v, want Subject the new person and Record the account", e)
	}
	if len(p.bus.Published()) != published {
		t.Error("LinkAccount published a signal; it publishes none")
	}
	p.noLeak(string(u.email), "link.target")

	// The session keeps working: the next request carries the new person ID.
	p.wantAuthenticated(cookie, target)
}

func TestLinkAccount_EmailMatchedIgnoringCase(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("mixed.case")
	acct, _, err := seamsSvc(p).LinkAccount(ctxT(t), model.Email("MIXED.Case@EXAMPLE.org"), newPersonID())
	wantNoErr(t, err)
	if acct != u.sess.Account {
		t.Error("a different account was linked")
	}
}

// F65
func TestLinkAccount_UnknownEmail_NotFoundInTheDatabase(t *testing.T) {
	p := newPoolRig(t)
	p.signUp("someone.else")
	_, _, err := seamsSvc(p).LinkAccount(ctxT(t), mail(t, "nobody.here@example.org"), newPersonID())
	wantKind(t, err, errs.NotFound)
	hasFieldCode(t, err, "email", "unknown")
	if strings.Contains(err.Error(), "nobody.here") {
		t.Error("the error echoes the address")
	}
	if len(p.linkedEntries()) != 0 {
		t.Error("a refused link was recorded")
	}
}

// F66
func TestLinkAccount_Superuser_Conflict(t *testing.T) {
	p := newPoolRig(t)
	res, _ := p.superuser("root.operator@example.org")
	_, _, err := seamsSvc(p).LinkAccount(ctxT(t), mail(t, "root.operator@example.org"), newPersonID())
	wantKind(t, err, errs.Conflict)
	wantAnyFieldCode(t, err, "superuser")
	if row := p.rowOf(res.Person); !row.Superuser {
		t.Error("the superuser's account changed")
	}
	if len(p.linkedEntries()) != 0 {
		t.Error("a refused link was recorded")
	}
}

// F67
func TestLinkAccount_PersonHasAccount_Conflict(t *testing.T) {
	p := newPoolRig(t)
	owner := p.signUp("owner.account")
	spare := p.signUp("spare.account")
	svc := seamsSvc(p)
	_, _, err := svc.LinkAccount(ctxT(t), spare.email, owner.sess.Person)
	wantKind(t, err, errs.Conflict)
	wantAnyFieldCode(t, err, "has_account")
	// Linking an account to the person it already belongs to is the same refusal.
	_, _, err = svc.LinkAccount(ctxT(t), owner.email, owner.sess.Person)
	wantKind(t, err, errs.Conflict)
	if got, err := svc.EmailOf(ctxT(t), spare.sess.Person); err != nil || got.Canonical() != spare.email.Canonical() {
		t.Errorf("the spare account moved: %v, %v", got, err)
	}
	if len(p.linkedEntries()) != 0 {
		t.Error("a refused link was recorded")
	}
}

func TestLinkAccount_RunsInTheCallersTransaction(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("tx.target")
	target := newPersonID()
	svc := seamsSvc(p)
	boom := errors.New("caller rolls back")
	err := p.fdb.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if _, _, err := svc.LinkAccount(ctx, u.email, target); err != nil {
			return keep(err)
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Transact = %v, want the caller's error", err)
	}
	if _, err := svc.EmailOf(ctxT(t), u.sess.Person); err != nil {
		t.Errorf("the rolled-back link moved the account: %v", err)
	}
	if _, err := svc.EmailOf(ctxT(t), target); !errs.Is(err, errs.NotFound) {
		t.Errorf("the target has an account after a rollback: %v", err)
	}
	// And committed when the caller commits.
	err = p.fdb.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		_, _, err := svc.LinkAccount(ctx, u.email, target)
		return keep(err)
	})
	wantNoErr(t, err)
	if _, err := svc.EmailOf(ctxT(t), target); err != nil {
		t.Errorf("the committed link is missing: %v", err)
	}
}

func TestLinkAccount_ReadOnlyCopy_Unavailable(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ro.target")
	p.fdb.set(func(f *faultDB) { f.ro = true })
	_, _, err := seamsSvc(p).LinkAccount(ctxT(t), u.email, newPersonID())
	wantReadOnly(t, err)
	p.fdb.set(func(f *faultDB) { f.ro = false })
	if _, err := seamsSvc(p).EmailOf(ctxT(t), u.sess.Person); err != nil {
		t.Errorf("the account moved although the copy was read-only: %v", err)
	}
}

// F68
func TestDeletePerson_Superuser_Permission(t *testing.T) {
	p := newPoolRig(t)
	res, _ := p.superuser("root.operator@example.org")
	svc := seamsSvc(p)
	n, err := svc.DeletePerson(ctxT(t), res.Person)
	wantKind(t, err, errs.Permission)
	if n != 0 {
		t.Errorf("deleted %d rows", n)
	}
	if !p.rowOf(res.Person).Superuser {
		t.Error("the superuser's account is gone")
	}
	if p.count(actDeleted) != 0 {
		t.Error("identity.account.deleted was recorded for a refused deletion")
	}
}

// F68: inside the caller's transaction the refusal ends it, and a participant's account
// still goes.
func TestDeletePerson_Superuser_RefusalInCallersTransaction(t *testing.T) {
	p := newPoolRig(t)
	res, _ := p.superuser("root.operator@example.org")
	u := p.signUp("plain.burner")
	svc := seamsSvc(p)
	err := p.fdb.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if n, err := svc.DeletePerson(ctx, u.sess.Person); err != nil || n != 1 {
			t.Errorf("deleting a participant: %d, %v", n, err)
		}
		_, err := svc.DeletePerson(ctx, res.Person)
		return keep(err)
	})
	wantKind(t, err, errs.Permission)
	if !p.rowOf(res.Person).Superuser {
		t.Error("the superuser's account is gone")
	}
	if got, err := svc.EmailOf(ctxT(t), u.sess.Person); err != nil || got.Canonical() != u.email.Canonical() {
		t.Errorf("the participant's deletion was not rolled back with the refusal: %v, %v", got, err)
	}
}

func TestDeletePerson_Participant_StillDeletes_AndIsIdempotent(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("plain.burner")
	svc := seamsSvc(p)
	n, err := svc.DeletePerson(ctxT(t), u.sess.Person)
	wantNoErr(t, err)
	if n != 1 {
		t.Errorf("deleted %d, want 1", n)
	}
	n, err = svc.DeletePerson(ctxT(t), u.sess.Person)
	wantNoErr(t, err)
	if n != 0 {
		t.Errorf("second deletion removed %d", n)
	}
}
