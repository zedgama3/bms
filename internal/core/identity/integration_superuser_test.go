//go:build integration

package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func TestEnsureSuperuser_Creates(t *testing.T) {
	p := newPoolRig(t)
	const addr = "root.operator@example.org"
	res, err := p.svc.EnsureSuperuser(ctxT(t), mail(t, addr))
	wantNoErr(t, err)
	if !res.Created || res.Account.IsZero() || res.Person.IsZero() || !res.Password.IsSet() {
		t.Fatalf("result = %+v", res)
	}
	pw := res.Password.Value()
	if len(pw) != identity.GeneratedPasswordLength {
		t.Errorf("password length %d", len(pw))
	}
	for _, r := range pw {
		if !strings.ContainsRune(tempAlphabet, r) {
			t.Errorf("character %q outside the alphabet", r)
			break
		}
	}
	e := p.only(actSuCreated)
	if e.Outcome != audit.Succeeded || e.Actor.Kind != audit.ActorOperator || e.Record.Kind != "identity.account" || e.Record.ID != [16]byte(res.Account) {
		t.Errorf("entry = %+v", e)
	}
	pub := p.bus.Published()
	if len(pub) != 1 || pub[0].Name != identity.SignalAccountCreated {
		t.Fatalf("published = %+v", pub)
	}
	person, err := signals.Get[model.PersonID](pub[0].Payload, identity.PayloadPerson)
	wantNoErr(t, err)
	if person != res.Person {
		t.Error("signal names another person")
	}
	row := p.rowOf(res.Person)
	if !row.Superuser || row.PasswordChangeRequired || row.PasswordHash == "" {
		t.Errorf("row = %+v", row)
	}
	if _, err := p.svc.SignIn(from(ctxT(t), nextIP()), identity.Credentials{Email: mail(t, addr), Password: pw}); err != nil {
		t.Errorf("the generated password does not sign in: %v", err)
	}
	p.noLeak(pw, "root.operator")
	if strings.Contains(res.Password.String(), pw) {
		t.Error("Secret prints the password")
	}
}

// F50: idempotent; a second run resets.
func TestEnsureSuperuser_Existing_ResetsAndEndsSessions(t *testing.T) {
	p := newPoolRig(t)
	const addr = "root.operator@example.org"
	first, iss := p.superuser(addr)
	oldCookie := p.cookie(mail(t, addr), first.Password.Value())
	p.wantAuthenticated(oldCookie, first.Person)
	_ = iss
	second, err := p.svc.EnsureSuperuser(ctxT(t), mail(t, "ROOT.Operator@example.org")) // another case
	wantNoErr(t, err)
	if second.Created {
		t.Error("Created true for an existing account")
	}
	if second.Account != first.Account || second.Person != first.Person {
		t.Error("a second superuser account was made")
	}
	if second.Password.Value() == first.Password.Value() {
		t.Error("the password was not renewed")
	}
	e := p.only(actSuReset)
	if e.Outcome != audit.Succeeded || e.Actor.Kind != audit.ActorOperator || e.Record.ID != [16]byte(first.Account) {
		t.Errorf("entry = %+v", e)
	}
	if p.count(actSuCreated) != 1 || len(p.bus.Published()) != 1 {
		t.Error("a reset must not create or publish again")
	}
	p.wantRejected(oldCookie)
	if _, err := p.svc.SignIn(from(ctxT(t), nextIP()), identity.Credentials{Email: mail(t, addr), Password: first.Password.Value()}); err == nil {
		t.Error("the old password still works")
	}
	if _, err := p.svc.SignIn(from(ctxT(t), nextIP()), identity.Credentials{Email: mail(t, addr), Password: second.Password.Value()}); err != nil {
		t.Errorf("the new password does not work: %v", err)
	}
	if p.accountCount() != 1 {
		t.Errorf("%d accounts", p.accountCount())
	}
}

func TestEnsureSuperuser_Reset_ClearsDisabledAndRestriction(t *testing.T) {
	p := newPoolRig(t)
	const addr = "root.operator@example.org"
	res, _ := p.superuser(addr)
	// a break-glass account disabled out of band is re-enabled by the reset (F50)
	_, err := p.pool.Exec(ctxT(t), "UPDATE core.account SET disabled_at = $1 WHERE person_id = $2", p.clk.Now(), [16]byte(res.Person))
	wantNoErr(t, err)
	_, err = p.svc.EnsureSuperuser(ctxT(t), mail(t, addr))
	wantNoErr(t, err)
	if row := p.rowOf(res.Person); row.DisabledAt != nil {
		t.Error("disabled_at not cleared by the reset")
	}
}

// F45
func TestEnsureSuperuser_ParticipantAddress_Conflict(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	_, err := p.svc.EnsureSuperuser(ctxT(t), u.email)
	wantKind(t, err, errs.Conflict)
	if row := p.rowOf(u.sess.Person); row.Superuser {
		t.Fatal("a participant account was promoted")
	}
	if p.count(actSuCreated)+p.count(actSuReset) != 0 {
		t.Error("an entry was recorded for the refused command")
	}
	if _, err := p.signIn(p.svc, u); err != nil {
		t.Errorf("the participant's password changed: %v", err)
	}
}

func TestEnsureSuperuser_SeveralAccountsAllowed(t *testing.T) {
	p := newPoolRig(t)
	a, _ := p.superuser("root.one@example.org")
	b, _ := p.superuser("root.two@example.org")
	if a.Account == b.Account || p.accountCount() != 2 {
		t.Error("expected two superuser accounts")
	}
}

// F46 on a real pool.
func TestEnsureSuperuser_ReadOnly_Integration(t *testing.T) {
	p := newPoolRig(t)
	p.fdb.set(func(f *faultDB) { f.ro = true })
	_, err := p.svc.EnsureSuperuser(ctxT(t), mail(t, "root.operator@example.org"))
	wantReadOnly(t, err)
	if p.accountCount() != 0 {
		t.Error("a read-only copy created an account")
	}
}

func TestEnsureSuperuser_AuditFails_RollsBack(t *testing.T) {
	p := newPoolRig(t)
	p.rec.Fail(errs.E("test.audit", errs.Unavailable, errDown))
	_, err := p.svc.EnsureSuperuser(ctxT(t), mail(t, "root.operator@example.org"))
	wantKind(t, err, errs.Unavailable)
	if p.accountCount() != 0 {
		t.Error("account survived a failed audit write")
	}
}

// ---- export and deletion ----

func TestExportPerson_AccountWithoutHash(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUpAs("Ada.Quill@example.org", pwGood)
	raw, err := p.svc.ExportPerson(ctxT(t), u.sess.Person)
	wantNoErr(t, err)
	var doc map[string]json.RawMessage
	wantNoErr(t, json.Unmarshal(raw, &doc))
	acct, ok := doc["account"]
	if !ok || string(acct) == "null" {
		t.Fatalf("export = %s", raw)
	}
	text := string(raw)
	for _, bad := range []string{"argon2", "password_hash", pwGood, "$"} {
		if strings.Contains(text, bad) {
			t.Errorf("export contains %q", bad)
		}
	}
	if !strings.Contains(text, "Ada.Quill@example.org") {
		t.Error("export lacks the address")
	}
	if p.count(actRead) != 0 {
		t.Error("identity audited an export (people records people.person.exported)")
	}
}

func TestExportPerson_NoAccount_AccountNull(t *testing.T) {
	p := newPoolRig(t)
	raw, err := p.svc.ExportPerson(ctxT(t), newPersonID())
	wantNoErr(t, err)
	var doc map[string]json.RawMessage
	wantNoErr(t, json.Unmarshal(raw, &doc))
	v, ok := doc["account"]
	if !ok || string(v) != "null" {
		t.Errorf("export = %s, want {\"account\":null}", raw)
	}
}

func TestExportPerson_WorksOnReadOnlyCopy(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	p.fdb.set(func(f *faultDB) { f.ro = true })
	if _, err := p.svc.ExportPerson(ctxT(t), u.sess.Person); err != nil {
		t.Fatalf("ExportPerson: %v", err)
	}
}

func TestDeletePerson_RemovesAccountAndSessions(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	other := p.signUp("keep.me")
	c := p.cookie(u.email, u.pw)
	var n int
	wantNoErr(t, p.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		var err error
		n, err = p.svc.DeletePerson(ctx, u.sess.Person)
		return keep(err)
	}))
	if n != 1 {
		t.Errorf("deleted %d, want 1", n)
	}
	e := p.only(actDeleted)
	if e.Outcome != audit.Succeeded || e.Subject != u.sess.Person || e.Record.ID != [16]byte(u.sess.Account) {
		t.Errorf("entry = %+v", e)
	}
	p.wantRejected(c) // F6: the session fails at the next request
	_, err := p.signIn(p.svc, u)
	wantKind(t, err, errs.Unauthenticated)
	if p.accountCount() != 1 {
		t.Errorf("%d accounts remain, want only the other person's", p.accountCount())
	}
	p.wantAuthenticated(p.cookie(other.email, other.pw), other.sess.Person)
	// idempotent, and the address is free again
	n, err = p.svc.DeletePerson(ctxT(t), u.sess.Person)
	wantNoErr(t, err)
	if n != 0 {
		t.Errorf("second delete removed %d", n)
	}
	p.signUp("ada.quill")
}

// DeletePerson joins the caller's Write transaction: a rollback keeps the account.
func TestDeletePerson_JoinsCallersTransaction_RollsBack(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	boom := errors.New("caller changed its mind")
	err := p.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if _, err := p.svc.DeletePerson(ctx, u.sess.Person); err != nil {
			return keep(err)
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if p.accountCount() != 1 {
		t.Error("the account was deleted although the caller's transaction rolled back")
	}
	p.wantAuthenticated(p.cookie(u.email, u.pw), u.sess.Person)
}

// F48 on a real pool.
func TestDeletePerson_ReadOnly_Integration(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	p.fdb.set(func(f *faultDB) { f.ro = true })
	_, err := p.svc.DeletePerson(ctxT(t), u.sess.Person)
	wantReadOnly(t, err)
	if p.accountCount() != 1 {
		t.Error("deleted on a read-only copy")
	}
}

// ---- privacy: no address, password or token in logs, spans, metrics or audit fields ----

func TestPrivacy_WholeLifecycle_NoAddressPasswordOrTokenAnywhere(t *testing.T) {
	p := newPoolRig(t)
	const addr = "zq9.person@example.org"
	const mark = "zq9.person"
	u := p.signUpAs(addr, pwGood)
	c := p.cookie(u.email, u.pw)
	p.wantAuthenticated(c, u.sess.Person)
	_, _ = p.svc.SignIn(from(ctxT(t), "203.0.113.200"), identity.Credentials{Email: u.email, Password: pwOther})                          //nolint:errcheck // failure path under test
	_, _ = p.svc.SignIn(from(ctxT(t), "203.0.113.201"), identity.Credentials{Email: mail(t, "ghost.zq9@example.org"), Password: pwOther}) //nolint:errcheck // failure path under test
	_, _ = p.svc.ChangePassword(u.ctx(t), identity.PasswordChange{Current: pwOther, New: pwNew})                                          //nolint:errcheck // failure path under test
	iss, err := p.svc.ChangePassword(u.ctx(t), identity.PasswordChange{Current: pwGood, New: pwNew})
	wantNoErr(t, err)
	_, err = p.svc.EndOtherSessions(identity.WithSession(from(ctxT(t), nextIP()), iss.Session))
	wantNoErr(t, err)
	boss := p.signUp("boss.zq9")
	_, err = p.admin().ListAccounts(boss.ctx(t), identity.AccountQuery{Email: u.email})
	wantNoErr(t, err)
	tmp, err := p.admin().SetTemporaryPassword(boss.ctx(t), u.sess.Account)
	wantNoErr(t, err)
	wantNoErr(t, p.svc.SignOut(identity.WithSession(from(ctxT(t), nextIP()), iss.Session)))
	_, err = p.svc.ExportPerson(ctxT(t), u.sess.Person)
	wantNoErr(t, err)
	_, err = p.svc.SignUp(from(ctxT(t), nextIP()), identity.Credentials{Email: u.email, Password: pwNew}) // taken
	wantKind(t, err, errs.Conflict)

	p.noLeak(addr, mark, "ghost.zq9", pwGood, pwOther, pwNew, tmp.Value(), c.Value, "203.0.113.200", "203.0.113.201")
	// the rendered text of the error for a taken address does not echo the address either
	if strings.Contains(err.Error(), mark) {
		t.Error("an error message carries the address")
	}
	for _, e := range p.rec.Recorded() {
		if e.Reason != "" {
			t.Errorf("%s carries a Reason: %q", e.Action, e.Reason)
		}
	}
}
