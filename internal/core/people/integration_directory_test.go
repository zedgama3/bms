//go:build integration

package people_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func matchNames(ms []people.Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.PlayaName
	}
	return out
}

type searchFixture struct {
	*irig
	a, b, c, d model.PersonID // see newSearchFixture
	dirOnly    context.Context
	withCon    context.Context
}

// newSearchFixture seeds four people:
//
//	a: playa "Cobalt Heron", legal name "Marguerite Quill", phone "+1 555 0142"
//	b: playa "Marguerite Fan"
//	c: playa "Plain Walker", no account, contact address ada.quill@example.org
//	d: playa "Dune Skipper", login address zed.finch@example.org
func newSearchFixture(t *testing.T) *searchFixture {
	t.Helper()
	r := newIRig(t, false)
	f := &searchFixture{irig: r}
	f.a = r.insertPerson(seed{Playa: "Cobalt Heron", Pronouns: "she/her", Legal: "Marguerite Quill", Phone: "+1 555 0142"})
	f.b = r.insertPerson(seed{Playa: "Marguerite Fan"})
	f.c = r.insertPerson(seed{Playa: "Plain Walker", ContactEmail: "ada.quill@example.org"})
	f.d = r.insertPerson(seed{Playa: "Dune Skipper"})
	r.accts.addAccount(f.d, mail(t, "zed.finch@example.org"), false)
	dir := newPerson()
	r.grant(dir, permDirectory)
	f.dirOnly = asPerson(ctxT(t), dir)
	con := newPerson()
	r.grant(con, permDirectory, permContacts)
	f.withCon = asPerson(ctxT(t), con)
	return f
}

func TestSearch_ByPlayaName_CaseInsensitiveAndOrdered(t *testing.T) {
	r := newIRig(t, false)
	for _, n := range []string{"Echo Fox", "Alpha Fox", "Charlie Moth", "Bravo Fox", "Delta Wasp"} {
		r.insertPerson(seed{Playa: n, Pronouns: "they/them"})
	}
	_, ctx := r.admin(permDirectory)
	tests := []struct {
		name string
		q    string
		want []string
	}{
		{"substring, lower case", "fox", []string{"Alpha Fox", "Bravo Fox", "Echo Fox"}},
		{"upper case", "FOX", []string{"Alpha Fox", "Bravo Fox", "Echo Fox"}},
		{"prefix", "alp", []string{"Alpha Fox"}},
		{"middle of a word", "ravo", []string{"Bravo Fox"}},
		{"surrounding spaces are trimmed", "  moth ", []string{"Charlie Moth"}},
		{"no match", "zzz", []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.svc.Search(ctx, people.SearchQuery{Q: tc.q})
			wantNoErr(t, err)
			if names := matchNames(got); !slices.Equal(names, tc.want) {
				t.Errorf("matches = %v, want %v", names, tc.want)
			}
		})
	}
	got, err := r.svc.Search(ctx, people.SearchQuery{Q: "alpha"})
	wantNoErr(t, err)
	if len(got) != 1 || got[0].ID.IsZero() || got[0].Pronouns != "they/them" {
		t.Errorf("match = %+v, want ID and pronouns filled", got)
	}
	if r.count("people.search.read") != 0 || r.count("people.person.read") != 0 {
		t.Error("a playa-name search was audited")
	}
}

func TestSearch_Limit_DefaultAndExplicit(t *testing.T) {
	r := newIRig(t, false)
	for i := 1; i <= 25; i++ {
		r.insertPerson(seed{Playa: fmt.Sprintf("Zed %02d", i)})
	}
	_, ctx := r.admin(permDirectory)
	got, err := r.svc.Search(ctx, people.SearchQuery{Q: "zed"})
	wantNoErr(t, err)
	if len(got) != people.DefaultSearchResults {
		t.Errorf("default limit returned %d, want %d", len(got), people.DefaultSearchResults)
	}
	if got[0].PlayaName != "Zed 01" || got[19].PlayaName != "Zed 20" {
		t.Errorf("first/last = %q/%q, want the first twenty by name", got[0].PlayaName, got[19].PlayaName)
	}
	got, err = r.svc.Search(ctx, people.SearchQuery{Q: "zed", Limit: 3})
	wantNoErr(t, err)
	if names := matchNames(got); !slices.Equal(names, []string{"Zed 01", "Zed 02", "Zed 03"}) {
		t.Errorf("limit 3 = %v", names)
	}
	got, err = r.svc.Search(ctx, people.SearchQuery{Q: "zed", Limit: people.MaxSearchResults})
	wantNoErr(t, err)
	if len(got) != 25 {
		t.Errorf("limit max returned %d, want all 25", len(got))
	}
}

func TestSearch_WildcardsAreLiteral(t *testing.T) {
	r := newIRig(t, false)
	r.insertPerson(seed{Playa: "Alpha Fox"})
	r.insertPerson(seed{Playa: "Bravo Fox"})
	r.insertPerson(seed{Playa: "100% Moth"})
	r.insertPerson(seed{Playa: "Snake_Eyes"})
	_, ctx := r.admin(permDirectory)
	for q, want := range map[string][]string{
		"%%":   {},
		"a_":   {},
		"o_":   {},
		"\\x":  {},
		"0% ":  {"100% Moth"},
		"e_e":  {"Snake_Eyes"},
		"_ox ": {},
	} {
		got, err := r.svc.Search(ctx, people.SearchQuery{Q: q})
		wantNoErr(t, err)
		if names := matchNames(got); !slices.Equal(names, want) {
			t.Errorf("Search(%q) = %v, want %v", q, names, want)
		}
	}
}

// F16
func TestSearch_LegalNameWithoutContactsRead_NoMatch(t *testing.T) {
	f := newSearchFixture(t)
	got, err := f.svc.Search(f.dirOnly, people.SearchQuery{Q: "marguerite"})
	wantNoErr(t, err)
	// Only person b, whose playa name matches; person a's legal name does not count.
	if names := matchNames(got); !slices.Equal(names, []string{"Marguerite Fan"}) {
		t.Errorf("matches = %v, want only the playa-name match", names)
	}
	for _, q := range []string{"quill", "0142", "ada.quill", "555 0142", "zed.finch"} {
		got, err := f.svc.Search(f.dirOnly, people.SearchQuery{Q: q})
		wantNoErr(t, err)
		if len(got) != 0 {
			t.Errorf("Search(%q) without contacts.read = %v, want nothing", q, matchNames(got))
		}
	}
	if len(f.rec.Recorded()) != 0 {
		t.Errorf("entries %v for searches that matched no personal field", actionsOf(f.rec.Recorded()))
	}
}

func TestSearch_PersonalFields_WithContactsRead_MatchAndAreAudited(t *testing.T) {
	f := newSearchFixture(t)
	tests := []struct {
		name string
		q    string
		want model.PersonID
	}{
		{"legal name", "quill", f.a},
		{"phone", "0142", f.a},
		{"contact address", "ada.quill@example.org", f.c},
		{"contact address, any case", "ADA.Quill@EXAMPLE.org", f.c},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := len(f.entries("people.search.read"))
			got, err := f.svc.Search(f.withCon, people.SearchQuery{Q: tc.q})
			wantNoErr(t, err)
			ids := []model.PersonID{}
			for _, m := range got {
				ids = append(ids, m.ID)
			}
			if !slices.Contains(ids, tc.want) {
				t.Fatalf("Search(%q) = %v, want it to include the person", tc.q, matchNames(got))
			}
			es := f.entries("people.search.read")[before:]
			if len(es) == 0 {
				t.Fatal("no people.search.read entry for a personal-field match")
			}
			var e *audit.Entry
			for i := range es {
				if es[i].Subject == tc.want {
					e = &es[i]
				}
			}
			if e == nil {
				t.Fatalf("no search.read entry about the matched person: %v", es)
			}
			if len(e.Fields) > 0 {
				if e.Level != model.SensitivityPersonal {
					t.Errorf("Level = %v, want personal", e.Level)
				}
				for _, fld := range e.Fields {
					if !has(personalGroups, fld) {
						t.Errorf("field %q is not a personal group", fld)
					}
				}
			}
		})
	}
	f.noLeak("Marguerite Quill", "+1 555 0142", "ada.quill@example.org")
}

func TestSearch_OneEntryPerResultThatMatchedAPersonalField(t *testing.T) {
	f := newSearchFixture(t)
	// "marguerite" matches a (legal name) and b (playa name): only a is a personal match.
	got, err := f.svc.Search(f.withCon, people.SearchQuery{Q: "marguerite"})
	wantNoErr(t, err)
	if names := matchNames(got); !slices.Equal(names, []string{"Cobalt Heron", "Marguerite Fan"}) {
		t.Fatalf("matches = %v", names)
	}
	es := f.entries("people.search.read")
	if len(es) != 1 || es[0].Subject != f.a {
		t.Fatalf("search.read entries = %+v, want exactly one about the legal-name match", es)
	}
	// A playa-name-only search by the same viewer records nothing.
	before := len(f.rec.Recorded())
	_, err = f.svc.Search(f.withCon, people.SearchQuery{Q: "plain walker"})
	wantNoErr(t, err)
	if len(f.rec.Recorded()) != before {
		t.Error("a playa-name match was audited")
	}
}

func TestSearch_NeverMatchesTheLoginAddress(t *testing.T) {
	f := newSearchFixture(t)
	for name, ctx := range map[string]context.Context{"directory": f.dirOnly, "contacts": f.withCon} {
		got, err := f.svc.Search(ctx, people.SearchQuery{Q: "zed.finch"})
		wantNoErr(t, err)
		if len(got) != 0 {
			t.Errorf("%s: searching a login address found %v (that is the accounts screen's job)", name, matchNames(got))
		}
	}
}

func TestSearch_AuditFails_FailsAndReturnsNothing(t *testing.T) {
	f := newSearchFixture(t)
	f.rec.Fail(errs.E("test.audit", errs.Unavailable, context.DeadlineExceeded))
	got, err := f.svc.Search(f.withCon, people.SearchQuery{Q: "quill"})
	wantKind(t, err, errs.Unavailable)
	if len(got) != 0 {
		t.Errorf("results %v returned although the audit entry failed", got)
	}
}

func TestSearch_DepartmentScopedHolders(t *testing.T) {
	f := newSearchFixture(t)
	p := newPerson()
	dept := model.NewID[model.DepartmentID]()
	f.az.GrantIn(p, permDirectory, dept)
	f.az.GrantIn(p, permContacts, dept)
	got, err := f.svc.Search(asPerson(ctxT(t), p), people.SearchQuery{Q: "quill"})
	wantNoErr(t, err)
	if len(got) == 0 {
		t.Error("a department-scoped contacts.read holder cannot match personal fields (decision 9: any extent reaches everyone)")
	}
}

// ---- NamesOf and PlayaNameOf ------------------------------------------------------------

// F18
func TestNamesOf_UnknownIDs_Omitted(t *testing.T) {
	r := newIRig(t, false)
	known := r.insertPerson(seed{Playa: "Dusty Ember"})
	unnamed := r.insertPerson(seed{})
	unknown := newPerson()
	got, err := r.svc.NamesOf(ctxT(t), []model.PersonID{known, unnamed, unknown, known})
	wantNoErr(t, err)
	if got[known] != "Dusty Ember" {
		t.Errorf("name = %q", got[known])
	}
	if name, ok := got[unnamed]; !ok || name != "" {
		t.Errorf("unfilled name = %q, present %v; want present and empty", name, ok)
	}
	if _, ok := got[unknown]; ok {
		t.Error("an unknown ID is in the map")
	}
	if len(got) != 2 {
		t.Errorf("map = %v, want two entries", got)
	}
	if len(r.rec.Recorded()) != 0 {
		t.Error("NamesOf is internal and must not be audited")
	}
}

func TestNamesOf_DeletedPersonOmitted(t *testing.T) {
	r := newIRig(t, false)
	r.link()
	p := r.insertPerson(seed{Playa: "Dusty Ember"})
	_, ctx := r.admin(permDelete)
	_, err := r.svc.Delete(ctx, p)
	wantNoErr(t, err)
	got, err := r.svc.NamesOf(ctxT(t), []model.PersonID{p})
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Errorf("deleted person in %v", got)
	}
}

func TestNamesOf_BatchesIntoAConstantNumberOfStatements(t *testing.T) {
	r := newIRig(t, false)
	ids := make([]model.PersonID, people.MaxNames)
	for i := range ids {
		ids[i] = r.insertPerson(seed{Playa: fmt.Sprintf("Burner %03d", i)})
	}
	count := func(n int) int {
		r.spy.reset()
		got, err := r.svc.NamesOf(ctxT(t), ids[:n])
		wantNoErr(t, err)
		if len(got) != n {
			t.Fatalf("NamesOf(%d IDs) returned %d names", n, len(got))
		}
		return r.spy.counts().Stmts
	}
	one, many := count(1), count(people.MaxNames)
	if many != one {
		t.Errorf("%d statements for 1 ID but %d for %d IDs: not batched", one, many, people.MaxNames)
	}
	if many > 2 {
		t.Errorf("%d statements for a batch, want at most 2", many)
	}
}

func TestNamesOf_NoStatementsForNoIDs(t *testing.T) {
	r := newIRig(t, false)
	r.spy.reset()
	got, err := r.svc.NamesOf(ctxT(t), nil)
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestPlayaNameOf_ReturnsTheNameOrEmpty(t *testing.T) {
	r := newIRig(t, false)
	named := r.insertPerson(seed{Playa: "Dusty Ember"})
	unnamed := r.insertPerson(seed{})
	got, err := r.svc.PlayaNameOf(ctxT(t), named)
	wantNoErr(t, err)
	if got != "Dusty Ember" {
		t.Errorf("name = %q", got)
	}
	got, err = r.svc.PlayaNameOf(inDelivery(t), unnamed)
	wantNoErr(t, err)
	if got != "" {
		t.Errorf("unfilled name = %q, want empty", got)
	}
}

// ---- consents ---------------------------------------------------------------------------

func TestSetConsent_Self_GrantsAndRevokes(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	ctx := asPerson(ctxT(t), me)
	const purpose people.Purpose = "volunteers.share-contact"

	ok, err := r.svc.ConsentOf(ctxT(t), me, purpose)
	wantNoErr(t, err)
	if ok {
		t.Fatal("consent before anyone gave it")
	}
	c, err := r.svc.SetConsent(ctx, me, purpose, true)
	wantNoErr(t, err)
	if c.Person != me || c.Purpose != purpose || !c.Granted || c.RecordedBy != me || !c.ChangedAt.Equal(epoch) {
		t.Errorf("consent = %+v", c)
	}
	ok, err = r.svc.ConsentOf(inDelivery(t), me, purpose)
	wantNoErr(t, err)
	if !ok {
		t.Error("ConsentOf = false after granting")
	}
	e := r.only("people.consent.changed")
	if e.Subject != me || !slices.Equal(e.Fields, []string{"purpose"}) || e.Level != model.SensitivityInternal {
		t.Errorf("entry = %+v", e)
	}
	if got := fmt.Sprintf("%+v", e); strings.Contains(got, string(purpose)) {
		t.Errorf("the purpose value is in the audit entry: %s", got)
	}

	wantNoErr(t, r.clk.Advance(5*time.Second))
	c, err = r.svc.SetConsent(ctx, me, purpose, false)
	wantNoErr(t, err)
	if c.Granted {
		t.Error("consent still granted after revoking")
	}
	ok, err = r.svc.ConsentOf(ctxT(t), me, purpose)
	wantNoErr(t, err)
	if ok {
		t.Error("ConsentOf = true after revoking")
	}
	if r.count("people.consent.changed") != 2 {
		t.Errorf("%d consent entries, want 2", r.count("people.consent.changed"))
	}
	if n := r.consentRows(me); n != 1 {
		t.Errorf("%d consent rows, want one per purpose", n)
	}
}

func TestSetConsent_ManagerRecordsAPaperForm(t *testing.T) {
	r := newIRig(t, false)
	target := r.insertPerson(seed{Playa: "Dusty Ember"})
	mgr, ctx := r.admin(permManage)
	c, err := r.svc.SetConsent(ctx, target, "volunteers.share-contact", true)
	wantNoErr(t, err)
	if c.Person != target || c.RecordedBy != mgr {
		t.Errorf("consent = %+v, want person %v recorded by %v", c, target, mgr)
	}
	ok, err := r.svc.ConsentOf(ctxT(t), target, "volunteers.share-contact")
	wantNoErr(t, err)
	if !ok {
		t.Error("the recorded consent is not visible to ConsentOf")
	}
	if e := r.only("people.consent.changed"); e.Subject != target {
		t.Errorf("entry subject = %v, want the person the consent is about", e.Subject)
	}
}

func TestSetConsent_Table(t *testing.T) {
	r := newIRig(t, false)
	target := r.insertPerson(seed{Playa: "Dusty Ember"})
	t.Run("other person without manage", func(t *testing.T) {
		_, ctx := r.admin(permDirectory, permContacts)
		_, err := r.svc.SetConsent(ctx, target, "volunteers.share-contact", true)
		wantKind(t, err, errs.Permission)
	})
	t.Run("unknown person with manage", func(t *testing.T) {
		_, ctx := r.admin(permManage)
		_, err := r.svc.SetConsent(ctx, newPerson(), "volunteers.share-contact", true)
		wantKind(t, err, errs.NotFound)
	})
	t.Run("self without a row", func(t *testing.T) {
		me := newPerson()
		_, err := r.svc.SetConsent(asPerson(ctxT(t), me), me, "volunteers.share-contact", true)
		wantKind(t, err, errs.NotFound)
	})
	if n := r.consentRows(target); n != 0 {
		t.Errorf("%d consent rows after refused calls", n)
	}
}

func TestConsentOf_UnknownAndDeleted_False(t *testing.T) {
	r := newIRig(t, false)
	r.link()
	ok, err := r.svc.ConsentOf(ctxT(t), newPerson(), "volunteers.share-contact")
	wantNoErr(t, err)
	if ok {
		t.Error("consent for an unknown person")
	}
	p := r.insertPerson(seed{Playa: "Dusty Ember"})
	_, err = r.svc.SetConsent(asPerson(ctxT(t), p), p, "volunteers.share-contact", true)
	wantNoErr(t, err)
	_, ctx := r.admin(permDelete)
	_, err = r.svc.Delete(ctx, p)
	wantNoErr(t, err)
	ok, err = r.svc.ConsentOf(ctxT(t), p, "volunteers.share-contact")
	wantNoErr(t, err)
	if ok {
		t.Error("consent survives the person's deletion")
	}
}

// ---- emergency contacts -----------------------------------------------------------------

func TestContacts_AddUpdateRemove_Self(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	ctx := asPerson(ctxT(t), me)

	c1, err := r.svc.AddContact(ctx, me, people.ContactInput{Name: mECName, Phone: mECPhone, Relationship: mRelationship})
	wantNoErr(t, err)
	if c1.ID.IsZero() || c1.Person != me || c1.Name != mECName || c1.Phone != mECPhone || c1.Relationship != mRelationship || c1.Position != 1 {
		t.Errorf("contact = %+v", c1)
	}
	c2, err := r.svc.AddContact(ctx, me, people.ContactInput{Name: "Pell Mallow", Phone: "+1 555 0199"})
	wantNoErr(t, err)
	if c2.Position != 2 {
		t.Errorf("second contact position = %d, want 2", c2.Position)
	}
	if e := r.entries("people.contact.created"); len(e) != 2 || e[0].Subject != me || e[0].Record.Kind != "people.contact" {
		t.Errorf("created entries = %+v", e)
	}

	wantNoErr(t, r.clk.Advance(time.Minute))
	upd, err := r.svc.UpdateContact(ctx, me, c1.ID, people.ContactInput{Name: "Orville T.", Phone: mECPhone, Relationship: "friend"})
	wantNoErr(t, err)
	if upd.ID != c1.ID || upd.Name != "Orville T." || upd.Relationship != "friend" || upd.Position != 1 {
		t.Errorf("updated = %+v", upd)
	}
	if r.count("people.contact.changed") != 1 {
		t.Error("no people.contact.changed entry")
	}

	wantNoErr(t, r.svc.RemoveContact(ctx, me, c1.ID))
	if r.count("people.contact.deleted") != 1 {
		t.Error("no people.contact.deleted entry")
	}
	prof, err := r.svc.Me(ctx)
	wantNoErr(t, err)
	if len(prof.Contacts) != 1 || prof.Contacts[0].ID != c2.ID {
		t.Errorf("contacts after removing the first = %+v", prof.Contacts)
	}
	r.noLeak(mECName, mECPhone, mRelationship, "Pell Mallow", "Orville T.")
}

func TestContacts_ManagerActsForAnotherPerson(t *testing.T) {
	r := newIRig(t, false)
	target := r.insertPerson(seed{Playa: "Dusty Ember"})
	_, ctx := r.admin(permManage)
	c, err := r.svc.AddContact(ctx, target, people.ContactInput{Name: mECName, Phone: mECPhone})
	wantNoErr(t, err)
	if c.Person != target {
		t.Errorf("contact belongs to %v, want %v", c.Person, target)
	}
	_, err = r.svc.UpdateContact(ctx, target, c.ID, people.ContactInput{Name: mECName, Phone: "+1 555 0100"})
	wantNoErr(t, err)
	wantNoErr(t, r.svc.RemoveContact(ctx, target, c.ID))
	if e := r.only("people.contact.created"); e.Subject != target {
		t.Errorf("entry subject = %v, want the person the contact is about", e.Subject)
	}
}

func TestContacts_OtherPersonWithoutManage_ReturnsPermission(t *testing.T) {
	r := newIRig(t, false)
	target := r.insertPerson(seed{Playa: "Dusty Ember"})
	cid := r.insertContact(target, mECName, mECPhone, "", 1)
	_, ctx := r.admin(permDirectory, permContacts)
	in := people.ContactInput{Name: "Pell Mallow", Phone: "+1 555 0199"}
	_, err := r.svc.AddContact(ctx, target, in)
	wantKind(t, err, errs.Permission)
	_, err = r.svc.UpdateContact(ctx, target, cid, in)
	wantKind(t, err, errs.Permission)
	wantKind(t, r.svc.RemoveContact(ctx, target, cid), errs.Permission)
	if n := r.contactRows(target); n != 1 {
		t.Errorf("%d contact rows, want the original one", n)
	}
}

func TestContacts_UnknownPerson_ReturnsNotFound(t *testing.T) {
	r := newIRig(t, false)
	_, ctx := r.admin(permManage)
	in := people.ContactInput{Name: mECName, Phone: mECPhone}
	_, err := r.svc.AddContact(ctx, newPerson(), in)
	wantKind(t, err, errs.NotFound)
	_, err = r.svc.UpdateContact(ctx, newPerson(), model.NewID[people.ContactID](), in)
	wantKind(t, err, errs.NotFound)
	wantKind(t, r.svc.RemoveContact(ctx, newPerson(), model.NewID[people.ContactID]()), errs.NotFound)
}

// F19
func TestAddContact_AtMax_ReturnsInvalid(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	ctx := asPerson(ctxT(t), me)
	for i := 1; i <= people.MaxEmergencyContacts; i++ {
		_, err := r.svc.AddContact(ctx, me, people.ContactInput{Name: fmt.Sprintf("Contact %d", i), Phone: "+1 555 0100"})
		wantNoErr(t, err)
	}
	_, err := r.svc.AddContact(ctx, me, people.ContactInput{Name: "One Too Many", Phone: "+1 555 0100"})
	wantField(t, err, "contacts", "too_many")
	if got := paramOf(err, "contacts", "too_many", "max"); got != "10" {
		t.Errorf("max param = %q, want 10", got)
	}
	if n := r.contactRows(me); n != people.MaxEmergencyContacts {
		t.Errorf("%d contacts, want %d", n, people.MaxEmergencyContacts)
	}
	// Removing one makes room again.
	prof, err := r.svc.Me(ctx)
	wantNoErr(t, err)
	wantNoErr(t, r.svc.RemoveContact(ctx, me, prof.Contacts[0].ID))
	if _, err := r.svc.AddContact(ctx, me, people.ContactInput{Name: "Fits Now", Phone: "+1 555 0100"}); err != nil {
		t.Errorf("after a removal: %v", err)
	}
}

// F20
func TestAddContact_EmptyName_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name  string
		in    people.ContactInput
		field string
		code  string
	}{
		{"empty name", people.ContactInput{Name: "", Phone: "+1 555 0100"}, "name", "required"},
		{"blank name", people.ContactInput{Name: "   ", Phone: "+1 555 0100"}, "name", "required"},
		{"empty phone", people.ContactInput{Name: "Pell", Phone: ""}, "phone", "required"},
		{"blank phone", people.ContactInput{Name: "Pell", Phone: "  "}, "phone", "required"},
		{"name too long", people.ContactInput{Name: repeat("n", 201), Phone: "+1 555 0100"}, "name", "too_long"},
		{"phone too long", people.ContactInput{Name: "Pell", Phone: "+" + repeat("1", 40)}, "phone", "too_long"},
		{"phone malformed", people.ContactInput{Name: "Pell", Phone: "ring the bell"}, "phone", "malformed"},
		{"relationship too long", people.ContactInput{Name: "Pell", Phone: "+1 555 0100", Relationship: repeat("r", 61)}, "relationship", "too_long"},
		{"name control chars", people.ContactInput{Name: "Pell\x07", Phone: "+1 555 0100"}, "name", "control_chars"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newIRig(t, false)
			me := r.insertPerson(seed{Playa: "Dusty Ember"})
			ctx := asPerson(ctxT(t), me)
			_, err := r.svc.AddContact(ctx, me, tc.in)
			wantField(t, err, tc.field, tc.code)
			cid := r.insertContact(me, mECName, mECPhone, "", 1)
			_, err = r.svc.UpdateContact(ctx, me, cid, tc.in)
			wantField(t, err, tc.field, tc.code)
			if n := r.contactRows(me); n != 1 {
				t.Errorf("%d contact rows, want just the seeded one", n)
			}
		})
	}
}

func TestAddContact_BoundariesAccepted(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	ctx := asPerson(ctxT(t), me)
	_, err := r.svc.AddContact(ctx, me, people.ContactInput{Name: repeat("n", 200), Phone: "+" + repeat("1", 39), Relationship: repeat("r", 60)})
	wantNoErr(t, err)
	_, err = r.svc.AddContact(ctx, me, people.ContactInput{Name: "N", Phone: "5"})
	wantNoErr(t, err)
}

// F21
func TestAddContact_Hidden_ReturnsInvalid(t *testing.T) {
	r := newIRig(t, false)
	r.pol.set(func(p *people.Policy) { p.EmergencyContacts = people.FieldHidden })
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	cid := r.insertContact(me, mECName, mECPhone, "", 1)
	ctx := asPerson(ctxT(t), me)
	in := people.ContactInput{Name: "Pell", Phone: "+1 555 0100"}
	_, err := r.svc.AddContact(ctx, me, in)
	wantField(t, err, "contacts", "hidden")
	_, err = r.svc.UpdateContact(ctx, me, cid, in)
	wantField(t, err, "contacts", "hidden")
	prof, err := r.svc.Me(ctx)
	wantNoErr(t, err)
	if len(prof.Contacts) != 0 || has(prof.Fields, "emergency_contacts") {
		t.Errorf("hidden contacts shown: %+v %v", prof.Contacts, prof.Fields)
	}
	_, mctx := r.admin(permManage)
	_, err = r.svc.AddContact(mctx, me, in)
	wantField(t, err, "contacts", "hidden")
}

// Q-CORE7: emergency contacts are never required, so a profile is complete without them.
func TestEmergencyContacts_NeverRequired_ProfileCompleteWithout(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	prof, err := r.svc.Me(asPerson(ctxT(t), me))
	wantNoErr(t, err)
	if !prof.Person.Complete {
		t.Error("a profile with no emergency contacts is incomplete")
	}
}

// F22
func TestUpdateContact_OtherPersons_ReturnsNotFound(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	other := r.insertPerson(seed{Playa: "Pell Mallow"})
	theirs := r.insertContact(other, mECName, mECPhone, "", 1)
	ctx := asPerson(ctxT(t), me)
	in := people.ContactInput{Name: "Hijack", Phone: "+1 555 0100"}
	_, err := r.svc.UpdateContact(ctx, me, theirs, in)
	wantKind(t, err, errs.NotFound)
	wantKind(t, r.svc.RemoveContact(ctx, me, theirs), errs.NotFound)
	_, err = r.svc.UpdateContact(ctx, me, model.NewID[people.ContactID](), in)
	wantKind(t, err, errs.NotFound)
	// A manager naming the wrong person is refused the same way.
	_, mctx := r.admin(permManage)
	_, err = r.svc.UpdateContact(mctx, me, theirs, in)
	wantKind(t, err, errs.NotFound)
	wantKind(t, r.svc.RemoveContact(mctx, me, theirs), errs.NotFound)
	if n := r.contactRows(other); n != 1 {
		t.Errorf("the other person's contact count = %d, want 1", n)
	}
}

func TestContacts_PositionsStayUniqueAndAscending(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	ctx := asPerson(ctxT(t), me)
	var ids []people.ContactID
	for i := range 4 {
		c, err := r.svc.AddContact(ctx, me, people.ContactInput{Name: fmt.Sprintf("Contact %d", i), Phone: "+1 555 0100"})
		wantNoErr(t, err)
		ids = append(ids, c.ID)
	}
	wantNoErr(t, r.svc.RemoveContact(ctx, me, ids[1]))
	_, err := r.svc.AddContact(ctx, me, people.ContactInput{Name: "Late", Phone: "+1 555 0100"})
	wantNoErr(t, err)
	prof, err := r.svc.Me(ctx)
	wantNoErr(t, err)
	last := 0
	for _, c := range prof.Contacts {
		if c.Position <= last {
			t.Errorf("positions not strictly ascending: %+v", prof.Contacts)
		}
		last = c.Position
	}
	if len(prof.Contacts) != 4 {
		t.Errorf("%d contacts, want 4", len(prof.Contacts))
	}
	if prof.Contacts[len(prof.Contacts)-1].Name != "Late" {
		t.Error("the newest contact is not last")
	}
}

// The contact address is matched by Canonical(): exactly, in any letter case, never as a
// substring (model.md Proposed change 5).
func TestSearch_ContactAddress_ExactOnly(t *testing.T) {
	f := newSearchFixture(t)
	for _, q := range []string{"ada.quill", "ada.quill@example", "quill@example.org", "@example.org", "da.quill@example.org"} {
		got, err := f.svc.Search(f.withCon, people.SearchQuery{Q: q})
		wantNoErr(t, err)
		for _, m := range got {
			if m.ID == f.c {
				t.Errorf("Search(%q) found the person by a substring of the contact address", q)
			}
		}
	}
	for _, q := range []string{"ada.quill@example.org", "ADA.QUILL@EXAMPLE.ORG", "Ada.Quill@Example.Org"} {
		before := len(f.entries("people.search.read"))
		got, err := f.svc.Search(f.withCon, people.SearchQuery{Q: q})
		wantNoErr(t, err)
		found := false
		for _, m := range got {
			found = found || m.ID == f.c
		}
		if !found {
			t.Errorf("Search(%q) did not find the exact address", q)
		}
		if len(f.entries("people.search.read")) <= before {
			t.Errorf("Search(%q): the match was not audited", q)
		}
	}
}
