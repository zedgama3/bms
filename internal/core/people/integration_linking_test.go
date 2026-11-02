//go:build integration

package people_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// linkFixture: a manager, an account-less person, and a real identity service.
type linkFixture struct {
	*irig
	mgr    model.PersonID
	ctx    context.Context
	target model.PersonID
}

func newLinkFixture(t *testing.T) *linkFixture {
	t.Helper()
	r := newIRig(t, true)
	r.link()
	mgr, ctx := r.admin(permManage, permDirectory)
	target := r.create(ctx, "Walk On").Person.ID
	return &linkFixture{irig: r, mgr: mgr, ctx: ctx, target: target}
}

func TestLinkAccount_MovesTheAccountAndRemovesTheEmptyOrphan(t *testing.T) {
	f := newLinkFixture(t)
	u := f.signUp("link.target", true)
	if !f.rowExists(u.person) {
		t.Fatal("setup: the sign-up minted no orphan row")
	}
	res, err := f.svc.LinkAccount(f.ctx, f.target, u.email)
	wantNoErr(t, err)
	if res.Account != u.account || res.Orphan != u.person || !res.OrphanDeleted {
		t.Errorf("result = %+v, want account %v, orphan %v deleted", res, u.account, u.person)
	}
	if f.rowExists(u.person) {
		t.Error("the empty orphan row was kept")
	}
	if !f.rowExists(f.target) {
		t.Error("the target row vanished")
	}
	if f.accountRows(f.target) != 1 || f.accountRows(u.person) != 0 {
		t.Error("the account did not move to the target")
	}
	got, err := f.idSvc.EmailOf(f.ctx, f.target)
	wantNoErr(t, err)
	if got.Canonical() != u.email.Canonical() {
		t.Errorf("EmailOf(target) = %q", got)
	}
	prof, err := f.svc.Get(f.ctx, f.target)
	wantNoErr(t, err)
	if !prof.Person.HasAccount {
		t.Error("the linked person still shows no account")
	}
	e := f.only("people.person.linked")
	if e.Subject != f.target || len(e.Fields) != 0 || e.Record.ID != [16]byte(f.target) {
		t.Errorf("linked entry = %+v, want record = person and no fields", e)
	}
	if n := len(f.idRec.Recorded()); n == 0 {
		t.Error("identity recorded nothing")
	}
	found := false
	for _, x := range f.idRec.Recorded() {
		if string(x.Action) == "identity.account.linked" {
			found = true
		}
	}
	if !found {
		t.Error("identity did not record identity.account.linked")
	}
	var deleted []model.PersonID
	for _, s := range f.published(sigDeleted) {
		deleted = append(deleted, payloadPerson(s))
	}
	if len(deleted) != 1 || deleted[0] != u.person {
		t.Errorf("people.person-deleted for %v, want the orphan %v", deleted, u.person)
	}
	f.noLeak(string(u.email))
}

func TestLinkAccount_EmailIsMatchedIgnoringCase(t *testing.T) {
	f := newLinkFixture(t)
	f.signUp("mixed.case", true)
	res, err := f.svc.LinkAccount(f.ctx, f.target, model.Email("MIXED.Case@EXAMPLE.org"))
	wantNoErr(t, err)
	if res.Account.IsZero() {
		t.Error("no account returned")
	}
}

// F24
func TestLinkAccount_UnknownEmail_ReturnsNotFound(t *testing.T) {
	f := newLinkFixture(t)
	f.signUp("someone.else", true)
	_, err := f.svc.LinkAccount(f.ctx, f.target, model.Email("nobody.here@example.org"))
	wantKind(t, err, errs.NotFound)
	if !hasField(err, "email", "unknown") {
		t.Errorf("fields = %v, want email/unknown", err)
	}
	if strings.Contains(err.Error(), "nobody.here") {
		t.Error("the error echoes the address")
	}
	if f.count("people.person.linked") != 0 {
		t.Error("a refused link was recorded")
	}
}

// F25
func TestLinkAccount_HasAccount_ReturnsConflict(t *testing.T) {
	f := newLinkFixture(t)
	owner := f.signUp("owner.account", true)
	spare := f.signUp("spare.account", true)
	_, err := f.svc.LinkAccount(f.ctx, owner.person, spare.email)
	wantCode(t, err, errs.Conflict, "has_account")
	if f.accountRows(owner.person) != 1 || f.accountRows(spare.person) != 1 {
		t.Error("a refused link moved an account")
	}
	if f.count("people.person.linked") != 0 {
		t.Error("a refused link was recorded")
	}
}

// F26
func TestLinkAccount_Superuser_ReturnsConflict(t *testing.T) {
	f := newLinkFixture(t)
	su := f.superuser("root.operator")
	_, err := f.svc.LinkAccount(f.ctx, f.target, su.email)
	wantCode(t, err, errs.Conflict, "superuser")
	if f.accountRows(su.person) != 1 || f.accountRows(f.target) != 0 {
		t.Error("the superuser's account moved")
	}
}

// F27
func TestLinkAccount_OrphanWithData_Kept(t *testing.T) {
	tests := []struct {
		name  string
		prep  func(f *linkFixture, orphan model.PersonID)
		local string
	}{
		{"playa name", func(f *linkFixture, o model.PersonID) {
			f.exec("UPDATE core.person SET playa_name = $2, playa_name_key = $3 WHERE id = $1", [16]byte(o), "Dusty Ember", people.NameKey("Dusty Ember"))
		}, "keep.playa"},
		{"pronouns", func(f *linkFixture, o model.PersonID) {
			f.exec("UPDATE core.person SET pronouns = $2 WHERE id = $1", [16]byte(o), "they/them")
		}, "keep.pronouns"},
		{"legal name", func(f *linkFixture, o model.PersonID) {
			f.exec("UPDATE core.person SET legal_name = $2 WHERE id = $1", [16]byte(o), mLegal)
		}, "keep.legal"},
		{"phone", func(f *linkFixture, o model.PersonID) {
			f.exec("UPDATE core.person SET phone = $2 WHERE id = $1", [16]byte(o), mPhone)
		}, "keep.phone"},
		{"emergency contact", func(f *linkFixture, o model.PersonID) { f.insertContact(o, mECName, mECPhone, "", 1) }, "keep.contact"},
		{"consent", func(f *linkFixture, o model.PersonID) {
			f.exec(`INSERT INTO core.person_consent (person_id, purpose, granted, recorded_by, changed_at) VALUES ($1, 'volunteers.share-contact', true, $1, $2)`,
				[16]byte(o), f.clk.Now())
		}, "keep.consent"},
		{"data request", func(f *linkFixture, o model.PersonID) {
			f.exec(`INSERT INTO core.data_request (id, person_id, kind, status, note, created_at) VALUES ($1, $2, 'delete', 'open', '', $3)`,
				[16]byte(newPerson()), [16]byte(o), f.clk.Now())
		}, "keep.request"},
		{"foreign key from elsewhere", func(f *linkFixture, o model.PersonID) {
			f.exec(`CREATE TABLE IF NOT EXISTS core.zz_ref (person_id uuid NOT NULL REFERENCES core.person(id) ON DELETE RESTRICT)`)
			f.exec(`INSERT INTO core.zz_ref (person_id) VALUES ($1)`, [16]byte(o))
		}, "keep.fk"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newLinkFixture(t)
			u := f.signUp(tc.local, true)
			tc.prep(f, u.person)
			res, err := f.svc.LinkAccount(f.ctx, f.target, u.email)
			wantNoErr(t, err)
			if res.OrphanDeleted || res.Orphan != u.person {
				t.Errorf("result = %+v, want the orphan reported and kept", res)
			}
			if !f.rowExists(u.person) {
				t.Error("the orphan row, which holds data, was deleted")
			}
			if f.accountRows(f.target) != 1 {
				t.Error("the link itself did not happen")
			}
			for _, s := range f.published(sigDeleted) {
				if payloadPerson(s) == u.person {
					t.Error("people.person-deleted was published for a kept row")
				}
			}
		})
	}
}

func TestLinkAccount_OrphanRowMissing_StillLinks(t *testing.T) {
	f := newLinkFixture(t)
	u := f.signUp("no.orphan.row", false)
	res, err := f.svc.LinkAccount(f.ctx, f.target, u.email)
	wantNoErr(t, err)
	if res.OrphanDeleted {
		t.Error("OrphanDeleted with no orphan row")
	}
	if f.accountRows(f.target) != 1 {
		t.Error("not linked")
	}
}

// F11, F14
func TestLinkAccount_Permission_And_UnknownPerson(t *testing.T) {
	f := newLinkFixture(t)
	u := f.signUp("perm.target", true)
	_, noManage := f.admin(permDirectory, permContacts, permExport, permDelete)
	_, err := f.svc.LinkAccount(noManage, f.target, u.email)
	wantKind(t, err, errs.Permission)
	_, err = f.svc.LinkAccount(f.ctx, newPerson(), u.email)
	wantKind(t, err, errs.NotFound)
	if f.accountRows(u.person) != 1 {
		t.Error("a refused link moved the account")
	}
}

func TestLinkAccount_RecordFails_NothingMoves(t *testing.T) {
	f := newLinkFixture(t)
	u := f.signUp("rollback.target", true)
	f.rec.Fail(errs.E("test.audit", errs.Unavailable, context.DeadlineExceeded))
	_, err := f.svc.LinkAccount(f.ctx, f.target, u.email)
	wantKind(t, err, errs.Unavailable)
	if f.accountRows(u.person) != 1 || f.accountRows(f.target) != 0 {
		t.Error("the account moved although the audit entry failed")
	}
	if !f.rowExists(u.person) {
		t.Error("the orphan was deleted although the link failed")
	}
}

func TestLinkAccount_AccountsFail_NothingRecorded(t *testing.T) {
	r := newIRig(t, false)
	r.link()
	_, ctx := r.admin(permManage)
	target := r.create(ctx, "Walk On").Person.ID
	r.accts.linkErr = errs.E("test.accounts", errs.Unavailable, context.DeadlineExceeded)
	_, err := r.svc.LinkAccount(ctx, target, model.Email("any.one@example.org"))
	wantKind(t, err, errs.Unavailable)
	if r.count("people.person.linked") != 0 {
		t.Error("a failed link was recorded")
	}
	r.accts.linkErr = errs.E("test.accounts", errs.Conflict, context.DeadlineExceeded)
	_, err = r.svc.LinkAccount(ctx, target, model.Email("any.one@example.org"))
	wantKind(t, err, errs.Conflict)
}

func TestLinkAccount_AlreadyLinkedPerson_ReturnsConflictWithoutCallingIdentity(t *testing.T) {
	r := newIRig(t, false)
	r.link()
	_, ctx := r.admin(permManage)
	target := r.create(ctx, "Walk On").Person.ID
	r.accts.addAccount(target, mail(t, "already.linked@example.org"), false)
	_, err := r.svc.LinkAccount(ctx, target, model.Email("another.one@example.org"))
	wantCode(t, err, errs.Conflict, "has_account")
	for _, c := range r.log.all() {
		if c == "accounts.link" {
			t.Error("identity's LinkAccount was called for a person who already has an account")
		}
	}
}

// F56
func TestLinkAccount_ClearsContactEmail(t *testing.T) {
	r := newIRig(t, true)
	r.link()
	_, ctx := r.admin(permManage, permDirectory)
	walkOn := r.create(ctx, "Walk On").Person.ID
	_, err := r.svc.Update(ctx, walkOn, people.ProfileInput{ContactEmail: str(mContactEmail)})
	wantNoErr(t, err)
	if n := r.scalarInt("SELECT count(*) FROM core.person WHERE id = $1 AND contact_email IS NOT NULL", [16]byte(walkOn)); n != 1 {
		t.Fatal("setup: the walk-on has no contact address")
	}
	u := r.signUp("link.clears", true)
	_, err = r.svc.LinkAccount(ctx, walkOn, u.email)
	wantNoErr(t, err)
	if n := r.scalarInt("SELECT count(*) FROM core.person WHERE id = $1 AND contact_email IS NOT NULL", [16]byte(walkOn)); n != 0 {
		t.Error("the contact address survived the link")
	}
	e := r.only("people.person.linked")
	if !slices.Equal(e.Fields, []string{"contact_email"}) || e.Level != model.SensitivityPersonal {
		t.Errorf("linked entry = %+v, want Fields [contact_email] at personal level", e)
	}
	r.noLeak(mContactEmail)
}
