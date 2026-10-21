//go:build integration

package people_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func has(fields []string, name string) bool { return slices.Contains(fields, name) }

var personalGroups = []string{"legal_name", "phone", "contact_email", "email", "emergency_contacts"}

// F4
func TestMe_NoRow_ReturnsEmptyProfileWithoutWrite(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	r.spy.reset()
	prof, err := r.svc.Me(asPerson(ctxT(t), me))
	wantNoErr(t, err)
	if prof.Person.ID != me {
		t.Errorf("Person.ID = %v, want the actor %v", prof.Person.ID, me)
	}
	if prof.Person.PlayaName != "" || prof.Person.Complete {
		t.Errorf("empty profile = %+v, want an empty, incomplete one", prof.Person)
	}
	if n := r.personRows(); n != 0 {
		t.Errorf("%d person rows after Me, want 0", n)
	}
	if c := r.spy.counts(); c.WriteTx != 0 {
		t.Errorf("%d write transactions for Me", c.WriteTx)
	}
	if len(r.rec.Recorded()) != 0 || len(r.bus.Published()) != 0 {
		t.Error("Me on a missing row recorded or published something")
	}
}

func TestMe_Self_SeesEveryFieldGroupAndIsNotAudited(t *testing.T) {
	r := newIRig(t, false)
	u := newPerson()
	r.insertPersonAs(u, seed{Playa: mPlaya, Pronouns: "they/them", Legal: mLegal, Phone: mPhone})
	r.insertContact(u, mECName, mECPhone, mRelationship, 1)
	r.accts.addAccount(u, mail(t, "marker.login@example.org"), false)
	prof, err := r.svc.Me(asPerson(ctxT(t), u))
	wantNoErr(t, err)
	p := prof.Person
	if p.PlayaName != mPlaya || p.Pronouns != "they/them" || p.LegalName != mLegal || p.Phone != mPhone {
		t.Errorf("self profile = %+v", p)
	}
	if !p.HasAccount || p.Email != mail(t, "marker.login@example.org") {
		t.Errorf("HasAccount/Email = %v/%q", p.HasAccount, p.Email)
	}
	for _, f := range []string{"playa_name", "pronouns", "language", "legal_name", "phone", "email", "emergency_contacts", "consents"} {
		if !has(prof.Fields, f) {
			t.Errorf("Fields %v lacks %s", prof.Fields, f)
		}
	}
	if len(prof.Contacts) != 1 || prof.Contacts[0].Name != mECName || prof.Contacts[0].Position != 1 {
		t.Errorf("Contacts = %+v", prof.Contacts)
	}
	if !prof.Can.Edit || !prof.Can.ReadContacts || !prof.Can.Export {
		t.Errorf("self Can = %+v, want Edit, ReadContacts and Export", prof.Can)
	}
	if prof.Can.Delete || prof.Can.Link {
		t.Errorf("self Can = %+v, must not delete or link", prof.Can)
	}
	if prof.Policy != people.DefaultPolicy() {
		t.Errorf("Policy = %+v, want the default", prof.Policy)
	}
	if r.count("people.person.read") != 0 {
		t.Error("a self read was audited")
	}
}

// F5
func TestUpdateMe_EmptyPlayaName_ReturnsInvalid(t *testing.T) {
	for name, in := range map[string]people.ProfileInput{
		"empty":                {PlayaName: str("")},
		"only spaces":          {PlayaName: str("   \t ")},
		"absent on a new row":  {Pronouns: str("she/her")},
		"absent, other fields": {LegalName: str(mLegal)},
	} {
		t.Run(name, func(t *testing.T) {
			r := newIRig(t, false)
			me := newPerson()
			_, err := r.svc.UpdateMe(asPerson(ctxT(t), me), in)
			wantField(t, err, "playa_name", "required")
			if r.rowExists(me) || len(r.rec.Recorded()) != 0 || len(r.bus.Published()) != 0 {
				t.Error("a refused save left a row, an entry or a signal")
			}
		})
	}
}

// F5: an existing name cannot be emptied either.
func TestUpdateMe_ClearingThePlayaName_ReturnsInvalid(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	r.insertPersonAs(me, seed{Playa: "Dusty Ember"})
	_, err := r.svc.UpdateMe(asPerson(ctxT(t), me), people.ProfileInput{PlayaName: str(" ")})
	wantField(t, err, "playa_name", "required")
	prof, err := r.svc.Me(asPerson(ctxT(t), me))
	wantNoErr(t, err)
	if prof.Person.PlayaName != "Dusty Ember" {
		t.Errorf("name changed to %q by a refused save", prof.Person.PlayaName)
	}
}

func TestCreate_EmptyPlayaName_ReturnsInvalid(t *testing.T) {
	r := newIRig(t, false)
	_, ctx := r.admin(permManage)
	before := r.personRows()
	for _, in := range []people.ProfileInput{{}, {PlayaName: str("")}, {PlayaName: str("  ")}, {Pronouns: str("x/y")}} {
		_, err := r.svc.Create(ctx, in)
		wantField(t, err, "playa_name", "required")
	}
	if n := r.personRows(); n != before {
		t.Errorf("%d rows after refused creates, want %d", n, before)
	}
}

func TestUpdateMe_FirstSave_RepairsTheRowAndPublishesOnce(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	prof, err := r.svc.UpdateMe(asPerson(ctxT(t), me), people.ProfileInput{
		PlayaName: str("  Dusty Ember  "), Pronouns: str("they/them"), Language: str("en-US"),
	})
	wantNoErr(t, err)
	if prof.Person.ID != me || prof.Person.PlayaName != "Dusty Ember" || !prof.Person.Complete {
		t.Errorf("profile = %+v", prof.Person)
	}
	if prof.Person.Language != "en-US" || prof.Person.Pronouns != "they/them" {
		t.Errorf("profile = %+v", prof.Person)
	}
	if !r.rowExists(me) {
		t.Fatal("no row after the first save")
	}
	e := r.only("people.person.created")
	if e.Subject != me || e.Outcome != audit.Succeeded {
		t.Errorf("created entry = %+v", e)
	}
	if r.count("people.person.changed") != 0 {
		t.Error("the repair also recorded people.person.changed")
	}
	sigs := r.published(sigCreated)
	if len(sigs) != 1 || payloadPerson(sigs[0]) != me {
		t.Fatalf("published %v, want one people.person-created for the actor", sigs)
	}
	// A second save changes the row and announces nothing.
	_, err = r.svc.UpdateMe(asPerson(ctxT(t), me), people.ProfileInput{Pronouns: str("she/her")})
	wantNoErr(t, err)
	if len(r.published(sigCreated)) != 1 {
		t.Error("the second save published people.person-created again")
	}
	ch := r.only("people.person.changed")
	if ch.Subject != me || !slices.Equal(ch.Fields, []string{"pronouns"}) || ch.Level != model.SensitivityInternal {
		t.Errorf("changed entry = %+v", ch)
	}
}

func TestUpdateMe_ChangedEntryNamesFieldsAndTheHighestLevel(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	r.insertPersonAs(me, seed{Playa: "Dusty Ember"})
	_, err := r.svc.UpdateMe(asPerson(ctxT(t), me), people.ProfileInput{LegalName: str(mLegal), Pronouns: str("they/them")})
	wantNoErr(t, err)
	e := r.only("people.person.changed")
	if !has(e.Fields, "legal_name") || !has(e.Fields, "pronouns") || has(e.Fields, "playa_name") {
		t.Errorf("Fields = %v", e.Fields)
	}
	if e.Level != model.SensitivityPersonal {
		t.Errorf("Level = %v, want personal", e.Level)
	}
	r.noLeak(mLegal)
}

func TestUpdateMe_NilLeavesAndEmptyClears(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	r.insertPersonAs(me, seed{Playa: "Dusty Ember", Pronouns: "they/them", Legal: mLegal, Phone: mPhone})
	ctx := asPerson(ctxT(t), me)
	prof, err := r.svc.UpdateMe(ctx, people.ProfileInput{Pronouns: str("")})
	wantNoErr(t, err)
	if prof.Person.Pronouns != "" || prof.Person.PlayaName != "Dusty Ember" || prof.Person.LegalName != mLegal || prof.Person.Phone != mPhone {
		t.Errorf("profile = %+v", prof.Person)
	}
}

func TestUpdateMe_BumpsUpdatedAtFromTheClock(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	ctx := asPerson(ctxT(t), me)
	first, err := r.svc.UpdateMe(ctx, people.ProfileInput{PlayaName: str("Dusty Ember")})
	wantNoErr(t, err)
	wantNoErr(t, r.clk.Advance(time.Hour))
	second, err := r.svc.UpdateMe(ctx, people.ProfileInput{Pronouns: str("they/them")})
	wantNoErr(t, err)
	if !first.Person.CreatedAt.Equal(epoch) {
		t.Errorf("CreatedAt = %v, want %v", first.Person.CreatedAt, epoch)
	}
	if !second.Person.CreatedAt.Equal(epoch) || !second.Person.UpdatedAt.Equal(epoch.Add(time.Hour)) {
		t.Errorf("created/updated = %v/%v", second.Person.CreatedAt, second.Person.UpdatedAt)
	}
}

// F6
func TestProfileInput_Validation_Table(t *testing.T) {
	tests := []struct {
		name  string
		in    people.ProfileInput
		field string
		code  string
	}{
		{"playa name too long", people.ProfileInput{PlayaName: str(repeat("a", 101))}, "playa_name", "too_long"},
		{"playa name too long in bytes", people.ProfileInput{PlayaName: str(repeat("é", 51))}, "playa_name", "too_long"},
		{"playa name control", people.ProfileInput{PlayaName: str("Dusty\x07Ember")}, "playa_name", "control_chars"},
		{"playa name newline", people.ProfileInput{PlayaName: str("Dusty\nEmber")}, "playa_name", "control_chars"},
		{"playa name bad utf8", people.ProfileInput{PlayaName: str("Dusty\xffEmber")}, "playa_name", ""},
		{"pronouns too long", people.ProfileInput{Pronouns: str(repeat("p", 41))}, "pronouns", "too_long"},
		{"pronouns control", people.ProfileInput{Pronouns: str("they\x00them")}, "pronouns", "control_chars"},
		{"language malformed", people.ProfileInput{Language: str("english!")}, "language", "malformed"},
		{"language one letter", people.ProfileInput{Language: str("e")}, "language", "malformed"},
		{"language upper case primary", people.ProfileInput{Language: str("EN")}, "language", "malformed"},
		{"language too long", people.ProfileInput{Language: str("en-" + repeat("a", 33))}, "language", ""},
		{"legal name too long", people.ProfileInput{LegalName: str(repeat("l", 201))}, "legal_name", "too_long"},
		{"legal name control", people.ProfileInput{LegalName: str("Quill\tAda")}, "legal_name", "control_chars"},
		{"phone letters", people.ProfileInput{Phone: str("call me")}, "phone", "malformed"},
		{"phone too long", people.ProfileInput{Phone: str("+" + repeat("1", 40))}, "phone", "too_long"},
		{"phone leading letter", people.ProfileInput{Phone: str("x555")}, "phone", "malformed"},
		{"phone control", people.ProfileInput{Phone: str("555\x07")}, "phone", ""},
		{"contact email malformed", people.ProfileInput{ContactEmail: str("not-an-address")}, "contact_email", "malformed"},
		{"contact email two ats", people.ProfileInput{ContactEmail: str("a@@example.org")}, "contact_email", "malformed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newIRig(t, false)
			_, ctx := r.admin(permManage)
			in := tc.in
			if in.PlayaName == nil {
				in.PlayaName = str("Dusty Ember")
			}
			before := r.personRows()
			_, err := r.svc.Create(ctx, in)
			wantField(t, err, tc.field, tc.code)
			if n := r.personRows(); n != before {
				t.Errorf("%d rows after an invalid create, want %d", n, before)
			}
			// The same input fails for the person's own save, except contact_email,
			// which belongs to account-less people.
			if tc.field != "contact_email" {
				me := newPerson()
				_, err = r.svc.UpdateMe(asPerson(ctxT(t), me), in)
				wantField(t, err, tc.field, tc.code)
			}
		})
	}
}

func TestProfileInput_Boundaries_Accepted(t *testing.T) {
	tests := map[string]people.ProfileInput{
		"playa name 100 bytes":               {PlayaName: str(repeat("a", 100))},
		"playa name 50 two-byte":             {PlayaName: str(repeat("é", 50))},
		"pronouns 40 bytes":                  {Pronouns: str(repeat("p", 40))},
		"legal name 200 bytes":               {LegalName: str(repeat("l", 200))},
		"phone with extension":               {Phone: str("+1 (555) 010-0100 x12")},
		"phone plain digits":                 {Phone: str("5550100")},
		"phone 40 bytes":                     {Phone: str("+" + repeat("1", 39))},
		"language tag":                       {Language: str("en-US")},
		"language three letters":             {Language: str("fil")},
		"language empty is regional default": {Language: str("")},
		"language 35 bytes":                  {Language: str("en-" + repeat("a", 8) + "-" + repeat("b", 8) + "-" + repeat("c", 8) + "-" + repeat("d", 4))},
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			r := newIRig(t, false)
			if in.PlayaName == nil {
				in.PlayaName = str("Dusty Ember")
			}
			if _, err := r.svc.UpdateMe(asPerson(ctxT(t), newPerson()), in); err != nil {
				t.Fatalf("UpdateMe: %v", err)
			}
		})
	}
}

func TestProfileInput_ManyProblems_AllReportedAtOnce(t *testing.T) {
	r := newIRig(t, false)
	_, err := r.svc.UpdateMe(asPerson(ctxT(t), newPerson()), people.ProfileInput{
		PlayaName: str(repeat("a", 101)), Phone: str("call me"), Language: str("english!"),
	})
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{"playa_name", "phone", "language"} {
		if !hasField(err, f, "") {
			t.Errorf("no field error for %s in %v", f, err)
		}
	}
}

func TestProfileInput_ErrorsNeverEchoTheValue(t *testing.T) {
	r := newIRig(t, false)
	marker := "ZZ-marker-value-3321"
	for name, in := range map[string]people.ProfileInput{
		"phone":     {PlayaName: str("Dusty Ember"), Phone: str(marker)},
		"language":  {PlayaName: str("Dusty Ember"), Language: str(marker)},
		"playa":     {PlayaName: str(repeat(marker, 10))},
		"legalname": {PlayaName: str("Dusty Ember"), LegalName: str(marker + repeat("x", 300))},
	} {
		_, err := r.svc.UpdateMe(asPerson(ctxT(t), newPerson()), in)
		wantKind(t, err, errs.Invalid)
		if strings.Contains(err.Error(), marker) {
			t.Errorf("%s: the error message echoes the input", name)
		}
	}
}

// F7
func TestUpdateMe_RequiredLegalName_ReturnsInvalid(t *testing.T) {
	r := newIRig(t, false)
	r.pol.set(func(p *people.Policy) { p.LegalName = people.FieldRequired })
	me := newPerson()
	ctx := asPerson(ctxT(t), me)
	_, err := r.svc.UpdateMe(ctx, people.ProfileInput{PlayaName: str("Dusty Ember")})
	wantField(t, err, "legal_name", "required")
	_, err = r.svc.UpdateMe(ctx, people.ProfileInput{PlayaName: str("Dusty Ember"), LegalName: str("   ")})
	wantField(t, err, "legal_name", "required")
	if r.rowExists(me) {
		t.Error("a refused save left a row")
	}
	prof, err := r.svc.UpdateMe(ctx, people.ProfileInput{PlayaName: str("Dusty Ember"), LegalName: str(mLegal)})
	wantNoErr(t, err)
	if !prof.Person.Complete {
		t.Error("a profile with every required field is not Complete")
	}
	// Once set, it cannot be emptied again.
	_, err = r.svc.UpdateMe(ctx, people.ProfileInput{LegalName: str("")})
	wantField(t, err, "legal_name", "required")
}

func TestUpdateMe_RequiredPhone_ReturnsInvalid(t *testing.T) {
	r := newIRig(t, false)
	r.pol.set(func(p *people.Policy) { p.Phone = people.FieldRequired })
	_, err := r.svc.UpdateMe(asPerson(ctxT(t), newPerson()), people.ProfileInput{PlayaName: str("Dusty Ember")})
	wantField(t, err, "phone", "required")
}

// Decision 6: required binds the person's own saves only.
func TestAdminSaves_IgnoreRequiredModes_ProfileIncomplete(t *testing.T) {
	r := newIRig(t, false)
	r.pol.set(func(p *people.Policy) { p.LegalName = people.FieldRequired; p.Phone = people.FieldRequired })
	_, ctx := r.admin(permManage)
	prof, err := r.svc.Create(ctx, people.ProfileInput{PlayaName: str("Walk On")})
	wantNoErr(t, err)
	if prof.Person.Complete {
		t.Error("a profile missing required fields is Complete")
	}
	upd, err := r.svc.Update(ctx, prof.Person.ID, people.ProfileInput{Pronouns: str("she/her")})
	wantNoErr(t, err)
	if upd.Person.Complete {
		t.Error("still missing required fields, but Complete")
	}
	upd, err = r.svc.Update(ctx, prof.Person.ID, people.ProfileInput{LegalName: str(mLegal), Phone: str(mPhone)})
	wantNoErr(t, err)
	if !upd.Person.Complete {
		t.Error("every required field set, but not Complete")
	}
}

// A mode that becomes required makes existing profiles incomplete.
func TestComplete_FollowsThePolicy(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	r.insertPersonAs(me, seed{Playa: "Dusty Ember"})
	ctx := asPerson(ctxT(t), me)
	prof, err := r.svc.Me(ctx)
	wantNoErr(t, err)
	if !prof.Person.Complete {
		t.Fatal("a named profile under the default policy is not Complete")
	}
	r.pol.set(func(p *people.Policy) { p.LegalName = people.FieldRequired })
	prof, err = r.svc.Me(ctx)
	wantNoErr(t, err)
	if prof.Person.Complete {
		t.Error("legal name became required; the profile should be incomplete")
	}
	if prof.Policy.LegalName != people.FieldRequired {
		t.Errorf("Policy.LegalName = %v", prof.Policy.LegalName)
	}
}

// F8
func TestUpdateMe_HiddenPhone_ReturnsInvalid(t *testing.T) {
	r := newIRig(t, false)
	r.pol.set(func(p *people.Policy) { p.Phone = people.FieldHidden })
	me := newPerson()
	in := people.ProfileInput{PlayaName: str("Dusty Ember"), Phone: str(mPhone)}
	_, err := r.svc.UpdateMe(asPerson(ctxT(t), me), in)
	wantField(t, err, "phone", "hidden")
	_, ctx := r.admin(permManage)
	_, err = r.svc.Create(ctx, in)
	wantField(t, err, "phone", "hidden")
	target := r.create(ctx, "Walk On").Person.ID
	_, err = r.svc.Update(ctx, target, people.ProfileInput{Phone: str("")})
	wantField(t, err, "phone", "hidden")
}

func TestUpdateMe_HiddenLegalName_ReturnsInvalid(t *testing.T) {
	r := newIRig(t, false)
	r.pol.set(func(p *people.Policy) { p.LegalName = people.FieldHidden })
	_, err := r.svc.UpdateMe(asPerson(ctxT(t), newPerson()), people.ProfileInput{PlayaName: str("Dusty Ember"), LegalName: str(mLegal)})
	wantField(t, err, "legal_name", "hidden")
}

// Decision: hidden stops showing and accepting the field; stored values stay.
func TestHiddenMode_HidesButKeepsTheStoredValue(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	r.insertPersonAs(me, seed{Playa: "Dusty Ember", Phone: mPhone, Legal: mLegal})
	ctx := asPerson(ctxT(t), me)
	r.pol.set(func(p *people.Policy) { p.Phone = people.FieldHidden })
	prof, err := r.svc.Me(ctx)
	wantNoErr(t, err)
	if prof.Person.Phone != "" || has(prof.Fields, "phone") {
		t.Errorf("a hidden phone is shown: %q in %v", prof.Person.Phone, prof.Fields)
	}
	if prof.Person.LegalName != mLegal || !has(prof.Fields, "legal_name") {
		t.Errorf("another field vanished: %+v %v", prof.Person, prof.Fields)
	}
	// Hidden even for an admin who may read contacts.
	viewer := newPerson()
	r.grant(viewer, permDirectory, permContacts)
	vp, err := r.svc.Get(asPerson(ctxT(t), viewer), me)
	wantNoErr(t, err)
	if vp.Person.Phone != "" || has(vp.Fields, "phone") {
		t.Error("a hidden phone is shown to an admin")
	}
	r.pol.set(func(p *people.Policy) { p.Phone = people.FieldOptional })
	prof, err = r.svc.Me(ctx)
	wantNoErr(t, err)
	if prof.Person.Phone != mPhone {
		t.Errorf("phone after un-hiding = %q, want the stored value", prof.Person.Phone)
	}
}

// F9
func TestUpdate_ContactEmailWithAccount_ReturnsInvalid(t *testing.T) {
	r := newIRig(t, false)
	_, ctx := r.admin(permManage)
	target := r.create(ctx, "Walk On").Person.ID
	r.accts.addAccount(target, mail(t, "has.account@example.org"), false)
	_, err := r.svc.Update(ctx, target, people.ProfileInput{ContactEmail: str(mContactEmail)})
	wantField(t, err, "contact_email", "has_account")
	_, err = r.svc.UpdateMe(asPerson(ctxT(t), target), people.ProfileInput{ContactEmail: str(mContactEmail)})
	wantField(t, err, "contact_email", "has_account")
}

func TestCreate_ContactEmail_StoredAsEntered(t *testing.T) {
	r := newIRig(t, false)
	_, ctx := r.admin(permManage)
	prof, err := r.svc.Create(ctx, people.ProfileInput{PlayaName: str("Walk On"), ContactEmail: str("Walk.On@Example.org")})
	wantNoErr(t, err)
	if prof.Person.HasAccount {
		t.Error("a created person has an account")
	}
	viewer := newPerson()
	r.grant(viewer, permDirectory, permContacts)
	got, err := r.svc.Get(asPerson(ctxT(t), viewer), prof.Person.ID)
	wantNoErr(t, err)
	if got.Person.ContactEmail != "Walk.On@Example.org" {
		t.Errorf("ContactEmail = %q, want it as entered", got.Person.ContactEmail)
	}
	if got.Person.Email != "" {
		t.Errorf("Email = %q for a person without an account", got.Person.Email)
	}
	// Two account-less people may share an address (a household).
	if _, err := r.svc.Create(ctx, people.ProfileInput{PlayaName: str("Walk Two"), ContactEmail: str("walk.on@example.org")}); err != nil {
		t.Errorf("a second person with the same contact address: %v", err)
	}
}

func TestCreate_Row_AuditAndSignal(t *testing.T) {
	r := newIRig(t, false)
	_, ctx := r.admin(permManage)
	prof, err := r.svc.Create(ctx, people.ProfileInput{PlayaName: str(mPlaya), LegalName: str(mLegal)})
	wantNoErr(t, err)
	id := prof.Person.ID
	if id.IsZero() || prof.Person.HasAccount || prof.Person.PlayaName != mPlaya {
		t.Fatalf("Create returned %+v", prof.Person)
	}
	if !r.rowExists(id) {
		t.Fatal("no row")
	}
	e := r.only("people.person.created")
	if e.Subject != id || e.Record.Kind != "people.person" {
		t.Errorf("created entry = %+v", e)
	}
	sigs := r.published(sigCreated)
	if len(sigs) != 1 || payloadPerson(sigs[0]) != id {
		t.Errorf("published %v", sigs)
	}
	r.noLeak(mPlaya, mLegal)
}

// F11, F14
func TestUpdate_Table(t *testing.T) {
	r := newIRig(t, false)
	_, mctx := r.admin(permManage)
	target := r.create(mctx, "Walk On").Person.ID

	t.Run("unknown person is not found", func(t *testing.T) {
		_, err := r.svc.Update(mctx, newPerson(), people.ProfileInput{Pronouns: str("x/y")})
		wantKind(t, err, errs.NotFound)
	})
	t.Run("no manage permission", func(t *testing.T) {
		_, ctx := r.admin(permDirectory, permContacts)
		_, err := r.svc.Update(ctx, target, people.ProfileInput{Pronouns: str("x/y")})
		wantKind(t, err, errs.Permission)
	})
	t.Run("manager changes fields and the entry says which", func(t *testing.T) {
		before := len(r.entries("people.person.changed"))
		prof, err := r.svc.Update(mctx, target, people.ProfileInput{Pronouns: str("she/her"), Phone: str(mPhone)})
		wantNoErr(t, err)
		if prof.Person.Pronouns != "she/her" {
			t.Errorf("pronouns = %q", prof.Person.Pronouns)
		}
		es := r.entries("people.person.changed")
		if len(es) != before+1 {
			t.Fatalf("%d changed entries, want %d", len(es), before+1)
		}
		e := es[len(es)-1]
		if e.Subject != target || !has(e.Fields, "pronouns") || !has(e.Fields, "phone") || e.Level != model.SensitivityPersonal {
			t.Errorf("entry = %+v", e)
		}
		r.noLeak(mPhone)
	})
	t.Run("person cannot update another through UpdateMe", func(t *testing.T) {
		// UpdateMe only ever touches the actor's own row.
		other := newPerson()
		_, err := r.svc.UpdateMe(asPerson(ctxT(t), other), people.ProfileInput{PlayaName: str("Someone Else")})
		wantNoErr(t, err)
		got, err := r.svc.Me(asPerson(ctxT(t), target))
		wantNoErr(t, err)
		if got.Person.PlayaName == "Someone Else" {
			t.Error("UpdateMe changed another person's row")
		}
	})
}

// F10, F11, F12, F13: the visibility matrix of Get.
type getFixture struct {
	*irig
	target model.PersonID
}

func newGetFixture(t *testing.T) *getFixture {
	t.Helper()
	r := newIRig(t, false)
	target := newPerson()
	r.insertPersonAs(target, seed{Playa: mPlaya, Pronouns: "they/them", Legal: mLegal, Phone: mPhone})
	r.insertContact(target, mECName, mECPhone, mRelationship, 1)
	r.accts.addAccount(target, mail(t, "marker.login@example.org"), false)
	r.exec(`INSERT INTO core.person_consent (person_id, purpose, granted, recorded_by, changed_at) VALUES ($1, $2, true, $1, $3)`,
		[16]byte(target), "volunteers.share-contact", r.clk.Now())
	return &getFixture{irig: r, target: target}
}

func (f *getFixture) viewer(perms ...authz.Permission) context.Context {
	p := newPerson()
	f.grant(p, perms...)
	return asPerson(ctxT(f.t), p)
}

// F10
func TestGet_NoDirectoryRead_ReturnsPermission(t *testing.T) {
	f := newGetFixture(t)
	for name, perms := range map[string][]authz.Permission{
		"nothing": nil, "contacts only": {permContacts}, "manage only": {permManage}, "export and delete": {permExport, permDelete},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.svc.Get(f.viewer(perms...), f.target)
			wantKind(t, err, errs.Permission)
		})
	}
	// An unknown ID is refused the same way, so a 403 confirms nothing.
	_, err := f.svc.Get(f.viewer(), newPerson())
	wantKind(t, err, errs.Permission)
	if len(f.rec.Recorded()) != 0 {
		t.Errorf("entries %v for refused reads", actionsOf(f.rec.Recorded()))
	}
}

// F11
func TestGet_UnknownPerson_ReturnsNotFound(t *testing.T) {
	f := newGetFixture(t)
	_, err := f.svc.Get(f.viewer(permDirectory), newPerson())
	wantKind(t, err, errs.NotFound)
	_, err = f.svc.Get(f.viewer(permDirectory, permContacts, permManage), newPerson())
	wantKind(t, err, errs.NotFound)
}

// F12
func TestGet_NoContactsRead_OmitsPersonalFieldsAndDoesNotAudit(t *testing.T) {
	f := newGetFixture(t)
	prof, err := f.svc.Get(f.viewer(permDirectory), f.target)
	wantNoErr(t, err)
	p := prof.Person
	if p.PlayaName != mPlaya || p.Pronouns != "they/them" || p.ID != f.target {
		t.Errorf("internal fields = %+v", p)
	}
	if p.LegalName != "" || p.Phone != "" || p.ContactEmail != "" || p.Email != "" {
		t.Errorf("personal fields leaked: %+v", p)
	}
	for _, g := range personalGroups {
		if has(prof.Fields, g) {
			t.Errorf("Fields %v includes %s", prof.Fields, g)
		}
	}
	for _, g := range []string{"playa_name", "pronouns"} {
		if !has(prof.Fields, g) {
			t.Errorf("Fields %v lacks %s", prof.Fields, g)
		}
	}
	if has(prof.Fields, "consents") || len(prof.Consents) != 0 {
		t.Error("consents shown without people.profiles.manage")
	}
	if len(prof.Contacts) != 0 {
		t.Errorf("emergency contacts shown without people.contacts.read: %+v", prof.Contacts)
	}
	if prof.Can.ReadContacts || prof.Can.Edit || prof.Can.Delete || prof.Can.Export || prof.Can.Link {
		t.Errorf("Can = %+v for a directory-only viewer", prof.Can)
	}
	if !p.HasAccount {
		t.Error("HasAccount is internal and should be visible")
	}
	if r := f.count("people.person.read"); r != 0 {
		t.Errorf("%d people.person.read entries for a read with no personal field", r)
	}
}

func TestGet_ContactsRead_ReturnsPersonalFieldsAndAudits(t *testing.T) {
	f := newGetFixture(t)
	viewerID := newPerson()
	f.grant(viewerID, permDirectory, permContacts)
	prof, err := f.svc.Get(asPerson(ctxT(t), viewerID), f.target)
	wantNoErr(t, err)
	p := prof.Person
	if p.LegalName != mLegal || p.Phone != mPhone || p.Email != "marker.login@example.org" {
		t.Errorf("personal fields = %+v", p)
	}
	if len(prof.Contacts) != 1 || prof.Contacts[0].Name != mECName {
		t.Errorf("contacts = %+v", prof.Contacts)
	}
	if !prof.Can.ReadContacts {
		t.Error("Can.ReadContacts = false")
	}
	for _, g := range []string{"legal_name", "phone", "email", "emergency_contacts"} {
		if !has(prof.Fields, g) {
			t.Errorf("Fields %v lacks %s", prof.Fields, g)
		}
	}
	e := f.only("people.person.read")
	if e.Subject != f.target || e.Level != model.SensitivityPersonal || e.Outcome != audit.Succeeded {
		t.Errorf("read entry = %+v", e)
	}
	for _, g := range []string{"legal_name", "phone", "email", "emergency_contacts"} {
		if !has(e.Fields, g) {
			t.Errorf("audited Fields %v lack %s", e.Fields, g)
		}
	}
	for _, fld := range e.Fields {
		if !has(personalGroups, fld) {
			t.Errorf("audited field %q is not a personal group", fld)
		}
	}
	f.noLeak(mLegal, mPhone, mECName, mECPhone, mRelationship, "marker.login")
}

func TestGet_Consents_OnlyWithManage(t *testing.T) {
	f := newGetFixture(t)
	prof, err := f.svc.Get(f.viewer(permDirectory, permManage), f.target)
	wantNoErr(t, err)
	if len(prof.Consents) != 1 || prof.Consents[0].Purpose != "volunteers.share-contact" || !prof.Consents[0].Granted {
		t.Errorf("consents = %+v", prof.Consents)
	}
	if !has(prof.Fields, "consents") {
		t.Errorf("Fields %v lacks consents", prof.Fields)
	}
	if !prof.Can.Edit {
		t.Errorf("Can = %+v", prof.Can)
	}
	if prof.Person.LegalName != "" {
		t.Error("manage alone must not reveal personal fields")
	}
}

// Decision 9 / D45, no provider: a department-scoped directory.read reaches every person,
// a department-scoped contacts.read reaches nobody (no personal fields, no audit entry).
func TestGet_DepartmentScopedGrants_NoProvider_DirectoryReachesAllContactsNobody(t *testing.T) {
	f := newGetFixture(t)
	viewerID := newPerson()
	dept := model.NewID[model.DepartmentID]()
	f.az.GrantIn(viewerID, permDirectory, dept)
	f.az.GrantIn(viewerID, permContacts, dept)
	prof, err := f.svc.Get(asPerson(ctxT(t), viewerID), f.target)
	wantNoErr(t, err)
	if prof.Person.PlayaName == "" {
		t.Error("directory.read@department must still reach every person's internal fields")
	}
	if prof.Person.LegalName != "" || prof.Person.Phone != "" || len(prof.Contacts) != 0 {
		t.Errorf("a department-scoped contacts.read holder with no provider saw personal fields: %+v", prof.Person)
	}
	if f.count("people.person.read") != 0 {
		t.Error("nothing personal was returned, so nothing may be audited")
	}
	// A department grant of one permission opens nothing of the other.
	v2 := newPerson()
	f.az.GrantIn(v2, permDirectory, dept)
	p2, err := f.svc.Get(asPerson(ctxT(t), v2), f.target)
	wantNoErr(t, err)
	if p2.Person.LegalName != "" {
		t.Error("directory.read alone revealed the legal name")
	}
}

func TestGet_Superuser_SeesAllAndIsAudited(t *testing.T) {
	f := newGetFixture(t)
	prof, err := f.svc.Get(asSuper(ctxT(t), newPerson()), f.target)
	wantNoErr(t, err)
	if prof.Person.LegalName != mLegal || len(prof.Consents) != 1 {
		t.Errorf("superuser profile = %+v", prof)
	}
	if f.count("people.person.read") != 1 {
		t.Error("the superuser's read of personal fields was not audited")
	}
}

func TestGet_Self_BehavesAsMe_NoPermissionNeeded_NoAudit(t *testing.T) {
	f := newGetFixture(t)
	prof, err := f.svc.Get(asPerson(ctxT(t), f.target), f.target)
	wantNoErr(t, err)
	if prof.Person.LegalName != mLegal || len(prof.Contacts) != 1 || len(prof.Consents) != 1 {
		t.Errorf("self Get = %+v", prof)
	}
	if f.count("people.person.read") != 0 {
		t.Error("a self read was audited")
	}
}

// F13
func TestGet_AuditFails_FailsRead(t *testing.T) {
	f := newGetFixture(t)
	f.rec.Fail(errs.E("test.audit", errs.Unavailable, context.DeadlineExceeded))
	prof, err := f.svc.Get(f.viewer(permDirectory, permContacts), f.target)
	wantKind(t, err, errs.Unavailable)
	if prof.Person.LegalName != "" || prof.Person.Phone != "" || len(prof.Contacts) != 0 {
		t.Errorf("personal data returned although the audit entry failed: %+v", prof)
	}
	// An internal-only read does not need the recorder.
	prof, err = f.svc.Get(f.viewer(permDirectory), f.target)
	wantNoErr(t, err)
	if prof.Person.PlayaName != mPlaya {
		t.Error("internal read failed")
	}
}

func TestGet_AuditFailureStillAuditsNothingElse(t *testing.T) {
	f := newGetFixture(t)
	f.rec.Fail(errs.E("test.audit", errs.Unavailable, context.DeadlineExceeded))
	_, err := f.svc.Get(f.viewer(permDirectory, permContacts), f.target)
	wantKind(t, err, errs.Unavailable)
	f.rec.Fail(nil)
	if len(f.rec.Recorded()) != 0 {
		t.Error("entries recorded by a failing recorder")
	}
}

func TestGet_ContactEmailOfAccountlessPerson_IsPersonalAndAudited(t *testing.T) {
	r := newIRig(t, false)
	_, actx := r.admin(permManage)
	prof, err := r.svc.Create(actx, people.ProfileInput{PlayaName: str("Walk On"), ContactEmail: str(mContactEmail)})
	wantNoErr(t, err)
	// Without contacts.read the address is hidden.
	dir := newPerson()
	r.grant(dir, permDirectory)
	got, err := r.svc.Get(asPerson(ctxT(t), dir), prof.Person.ID)
	wantNoErr(t, err)
	if got.Person.ContactEmail != "" || has(got.Fields, "contact_email") {
		t.Error("contact email visible without people.contacts.read")
	}
	adm := newPerson()
	r.grant(adm, permDirectory, permContacts)
	got, err = r.svc.Get(asPerson(ctxT(t), adm), prof.Person.ID)
	wantNoErr(t, err)
	if string(got.Person.ContactEmail) != mContactEmail || !has(got.Fields, "contact_email") {
		t.Errorf("contact email = %q in %v", got.Person.ContactEmail, got.Fields)
	}
	if e := r.only("people.person.read"); !has(e.Fields, "contact_email") {
		t.Errorf("read entry Fields = %v", e.Fields)
	}
	r.noLeak(mContactEmail)
}

// F4 revisited: the repair works when the handler missed the signal.
func TestPlayaNameOf_AndNamesOf_EmptyUntilTheRowExists(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	_, err := r.svc.PlayaNameOf(ctxT(t), me)
	wantKind(t, err, errs.NotFound)
	got, err := r.svc.NamesOf(ctxT(t), []model.PersonID{me})
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Errorf("NamesOf of a person without a row = %v", got)
	}
	_, err = r.svc.UpdateMe(asPerson(ctxT(t), me), people.ProfileInput{PlayaName: str("Dusty Ember")})
	wantNoErr(t, err)
	name, err := r.svc.PlayaNameOf(ctxT(t), me)
	wantNoErr(t, err)
	if name != "Dusty Ember" {
		t.Errorf("PlayaNameOf = %q", name)
	}
}

// Concurrency: the repair races with itself and with readers.
func TestService_ConcurrentUse_IsSafe(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	reader := newPerson()
	r.grant(reader, permDirectory)
	var wg sync.WaitGroup
	errc := make(chan error, 64)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.svc.UpdateMe(asPerson(ctxT(t), me), people.ProfileInput{PlayaName: str("Dusty Ember"), Pronouns: str(strings.Repeat("p", i+1))}); err != nil {
				errc <- err
			}
			if _, err := r.svc.Search(asPerson(ctxT(t), reader), people.SearchQuery{Q: "dusty"}); err != nil {
				errc <- err
			}
			if _, err := r.svc.NamesOf(ctxT(t), []model.PersonID{me}); err != nil {
				errc <- err
			}
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Errorf("concurrent call: %v", err)
	}
	if n := r.personRows(); n != 1 {
		t.Errorf("%d rows, want 1", n)
	}
	if n := len(r.published(sigCreated)); n != 1 {
		t.Errorf("%d people.person-created signals, want 1", n)
	}
}

// Cancelled and expired contexts surface their own kinds.
func TestUseCases_CancelledAndExpiredContext(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	r.grant(me, permDirectory)
	r.insertPersonAs(me, seed{Playa: "Dusty Ember"})
	calls := map[string]func(ctx context.Context) error{
		"Me":  func(ctx context.Context) error { _, err := r.svc.Me(ctx); return keep(err) },
		"Get": func(ctx context.Context) error { _, err := r.svc.Get(ctx, newPerson()); return keep(err) },
		"Search": func(ctx context.Context) error {
			_, err := r.svc.Search(ctx, people.SearchQuery{Q: "dusty"})
			return keep(err)
		},
		"UpdateMe": func(ctx context.Context) error {
			_, err := r.svc.UpdateMe(ctx, people.ProfileInput{Pronouns: str("x")})
			return keep(err)
		},
		"NamesOf": func(ctx context.Context) error { _, err := r.svc.NamesOf(ctx, []model.PersonID{me}); return keep(err) },
	}
	for name, call := range calls {
		t.Run(name+"/cancelled", func(t *testing.T) {
			ctx, cancel := context.WithCancel(asPerson(ctxT(t), me))
			cancel()
			wantKind(t, call(ctx), errs.Canceled)
		})
		t.Run(name+"/deadline passed", func(t *testing.T) {
			ctx, cancel := context.WithDeadline(asPerson(ctxT(t), me), time.Now().Add(-time.Second))
			defer cancel()
			wantKind(t, call(ctx), errs.Timeout)
		})
	}
}

// F54
func TestUpdateMe_LanguageOnly_SkipsRequiredCheck(t *testing.T) {
	r := newIRig(t, false)
	r.pol.set(func(p *people.Policy) { p.LegalName = people.FieldRequired; p.Phone = people.FieldRequired })
	me := newPerson()
	ctx := asPerson(ctxT(t), me)
	prof, err := r.svc.UpdateMe(ctx, people.ProfileInput{Language: str("es")})
	wantNoErr(t, err)
	if prof.Person.Language != "es" || prof.Person.Complete {
		t.Errorf("profile = %+v, want language saved and the profile still incomplete", prof.Person)
	}
	if !r.rowExists(me) {
		t.Error("the language save made no row")
	}
	// Any other field in the body brings the check back.
	_, err = r.svc.UpdateMe(ctx, people.ProfileInput{Language: str("fr"), Pronouns: str("they/them")})
	wantField(t, err, "playa_name", "required")
}
