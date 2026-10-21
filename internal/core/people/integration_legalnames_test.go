//go:build integration

package people_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// D44 amendment: Directory.LegalNamesOf against a real database (F67 to F69). Names are
// invented; the markers are checked for leaks into logs, spans and metrics.

type legalFixture struct {
	*irig
	a, b, c, noName model.PersonID
}

func newLegalFixture(t *testing.T) *legalFixture {
	t.Helper()
	r := newIRig(t, false)
	r.link()
	return &legalFixture{
		irig:   r,
		a:      r.insertPerson(seed{Playa: "Dune Moth", Legal: "Odalys Brindle"}),
		b:      r.insertPerson(seed{Playa: "Salt Fox", Legal: "Fenwick Tarn"}),
		c:      r.insertPerson(seed{Playa: "Ember Hare", Legal: mLegal}),
		noName: r.insertPerson(seed{Playa: "No Legal Name"}),
	}
}

func TestLegalNamesOf_ContactsRead_ReturnsNamesAndAuditsEachPerson(t *testing.T) {
	f := newLegalFixture(t)
	_, ctx := f.admin(permContacts) // contacts.read alone: directory.read is not required
	got, err := f.svc.LegalNamesOf(ctx, []model.PersonID{f.a, f.b, f.c})
	wantNoErr(t, err)
	want := map[model.PersonID]string{f.a: "Odalys Brindle", f.b: "Fenwick Tarn", f.c: mLegal}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for id, n := range want {
		if got[id] != n {
			t.Errorf("name of %s = %q, want %q", id, got[id], n)
		}
	}
	es := f.entries("people.person.read")
	if len(es) != 3 {
		t.Fatalf("%d people.person.read entries, want one per returned person (%v)", len(es), actionsOf(f.rec.Recorded()))
	}
	seen := map[model.PersonID]bool{}
	for _, e := range es {
		if !slices.Equal(e.Fields, []string{"legal_name"}) || e.Level != model.SensitivityPersonal || e.Outcome != audit.Succeeded {
			t.Errorf("entry = %+v", e)
		}
		if e.Subject.IsZero() || seen[e.Subject] {
			t.Errorf("entry subject %s missing or repeated", e.Subject)
		}
		seen[e.Subject] = true
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("no audit entry about %s", id)
		}
	}
}

// F69
func TestLegalNamesOf_NoLegalName_OmitsAndDoesNotAudit(t *testing.T) {
	f := newLegalFixture(t)
	_, ctx := f.admin(permContacts)
	got, err := f.svc.LegalNamesOf(ctx, []model.PersonID{f.noName, f.a})
	wantNoErr(t, err)
	if _, ok := got[f.noName]; ok || len(got) != 1 || got[f.a] != "Odalys Brindle" {
		t.Errorf("got %v", got)
	}
	es := f.entries("people.person.read")
	if len(es) != 1 || es[0].Subject != f.a {
		t.Fatalf("entries = %+v, want one, about the person with a name", es)
	}
}

func TestLegalNamesOf_OnlyPeopleWithoutNames_AuditsNothing(t *testing.T) {
	f := newLegalFixture(t)
	_, ctx := f.admin(permContacts)
	got, err := f.svc.LegalNamesOf(ctx, []model.PersonID{f.noName})
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
	if n := f.count("people.person.read"); n != 0 {
		t.Errorf("%d audit entries, want 0", n)
	}
}

// Unknown IDs are absent: no error, no hint that a person does or does not exist.
func TestLegalNamesOf_UnknownIDs_AreAbsent(t *testing.T) {
	f := newLegalFixture(t)
	_, ctx := f.admin(permContacts)
	ghost := newPerson()
	got, err := f.svc.LegalNamesOf(ctx, []model.PersonID{ghost, f.b, {}})
	wantNoErr(t, err)
	if _, ok := got[ghost]; ok || len(got) != 1 || got[f.b] != "Fenwick Tarn" {
		t.Errorf("got %v", got)
	}
	if es := f.entries("people.person.read"); len(es) != 1 || es[0].Subject != f.b {
		t.Errorf("entries = %+v", es)
	}
}

// F67
func TestLegalNamesOf_NoContactsRead_ReturnsPermissionAndAuditsNothing(t *testing.T) {
	f := newLegalFixture(t)
	_, ctx := f.admin(permDirectory, permManage)
	got, err := f.svc.LegalNamesOf(ctx, []model.PersonID{f.a})
	wantKind(t, err, errs.Permission)
	if len(got) != 0 {
		t.Errorf("got %v with the error", got)
	}
	if n := f.count("people.person.read"); n != 0 {
		t.Errorf("%d audit entries", n)
	}
}

// "In any extent": a department-scoped holder may call it. Whether the names of people
// outside the department are withheld is the membership extension point's business, and
// with no provider registered decision 9 holds.
func TestLegalNamesOf_DepartmentScopedContactsRead_Succeeds(t *testing.T) {
	f := newLegalFixture(t)
	me := f.insertPerson(seed{Playa: "Dept Lead"})
	f.az.GrantIn(me, permContacts, model.NewID[model.DepartmentID]())
	got, err := f.svc.LegalNamesOf(asPerson(ctxT(t), me), []model.PersonID{f.a})
	wantNoErr(t, err)
	if got[f.a] != "Odalys Brindle" {
		t.Errorf("got %v", got)
	}
	if n := f.count("people.person.read"); n != 1 {
		t.Errorf("%d audit entries, want 1", n)
	}
}

func TestLegalNamesOf_Superuser_Succeeds(t *testing.T) {
	f := newLegalFixture(t)
	su := f.insertPerson(seed{Playa: "Root"})
	f.az.Superuser(su)
	got, err := f.svc.LegalNamesOf(asSuper(ctxT(t), su), []model.PersonID{f.b})
	wantNoErr(t, err)
	if got[f.b] != "Fenwick Tarn" {
		t.Errorf("got %v", got)
	}
}

func TestLegalNamesOf_EmptyAndNilIDs_ReturnEmptyAndAuditNothing(t *testing.T) {
	f := newLegalFixture(t)
	_, ctx := f.admin(permContacts)
	for _, ids := range [][]model.PersonID{nil, {}} {
		got, err := f.svc.LegalNamesOf(ctx, ids)
		wantNoErr(t, err)
		if len(got) != 0 {
			t.Errorf("got %v", got)
		}
	}
	if n := f.count("people.person.read"); n != 0 {
		t.Errorf("%d audit entries", n)
	}
}

// F68 boundary: exactly MaxNames IDs is accepted, one more is not.
func TestLegalNamesOf_BatchBoundary(t *testing.T) {
	f := newLegalFixture(t)
	_, ctx := f.admin(permContacts)
	ids := make([]model.PersonID, people.MaxNames+1)
	for i := range ids {
		ids[i] = newPerson()
	}
	ids[0], ids[people.MaxNames-1] = f.a, f.b // first and last of an accepted batch
	got, err := f.svc.LegalNamesOf(ctx, ids[:people.MaxNames])
	wantNoErr(t, err)
	if len(got) != 2 || got[f.a] != "Odalys Brindle" || got[f.b] != "Fenwick Tarn" {
		t.Errorf("got %v", got)
	}
	before := f.count("people.person.read")
	_, err = f.svc.LegalNamesOf(ctx, ids)
	wantKind(t, err, errs.Invalid)
	if f.count("people.person.read") != before {
		t.Error("a rejected batch was audited")
	}
}

// A person listed twice is returned and audited once.
func TestLegalNamesOf_DuplicateIDs_AuditedOncePerPerson(t *testing.T) {
	f := newLegalFixture(t)
	_, ctx := f.admin(permContacts)
	got, err := f.svc.LegalNamesOf(ctx, []model.PersonID{f.a, f.a, f.a})
	wantNoErr(t, err)
	if len(got) != 1 {
		t.Errorf("got %v", got)
	}
	if n := f.count("people.person.read"); n != 1 {
		t.Errorf("%d audit entries for one person, want 1", n)
	}
}

// Decision 15 / privacy: the audit is written before the names are returned, and a Record
// failure fails the call without the names.
func TestLegalNamesOf_AuditFails_ReturnsErrorAndNoNames(t *testing.T) {
	f := newLegalFixture(t)
	_, ctx := f.admin(permContacts)
	boom := errs.E("test.audit", errs.Unavailable, errors.New("audit down"))
	f.rec.Fail(boom)
	got, err := f.svc.LegalNamesOf(ctx, []model.PersonID{f.a})
	wantKind(t, err, errs.Unavailable)
	if len(got) != 0 {
		t.Errorf("names returned although the audit failed: %v", got)
	}
}

func TestLegalNamesOf_CanceledContext_ReturnsCanceled(t *testing.T) {
	f := newLegalFixture(t)
	_, ctx := f.admin(permContacts)
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	_, err := f.svc.LegalNamesOf(ctx, []model.PersonID{f.a})
	if err == nil || (!errs.Is(err, errs.Canceled) && !errs.Is(err, errs.Timeout)) {
		t.Fatalf("err = %v (kind %v), want Canceled", err, errs.KindOf(err))
	}
	if n := f.count("people.person.read"); n != 0 {
		t.Errorf("%d audit entries after cancellation", n)
	}
}

// The legal name must not reach logs, spans or metrics (privacy-data).
func TestLegalNamesOf_DoesNotLeakNamesToTelemetry(t *testing.T) {
	f := newLegalFixture(t)
	_, ctx := f.admin(permContacts)
	_, err := f.svc.LegalNamesOf(ctx, []model.PersonID{f.c})
	wantNoErr(t, err)
	if f.cap.Contains(mLegal) {
		t.Error("a legal name appears in logs, spans or metrics")
	}
	for _, e := range f.rec.Recorded() {
		if e.Reason != "" && strings.Contains(e.Reason, mLegal) {
			t.Error("a legal name appears in an audit reason")
		}
	}
}

func TestLegalNamesOf_ConcurrentCalls_AreSafe(t *testing.T) {
	f := newLegalFixture(t)
	_, ctx := f.admin(permContacts)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := f.svc.LegalNamesOf(ctx, []model.PersonID{f.a, f.b})
			if err != nil || len(got) != 2 {
				t.Errorf("got %v, %v", got, err)
			}
		}()
	}
	wg.Wait()
	if n := f.count("people.person.read"); n != 16 {
		t.Errorf("%d audit entries, want 16 (two people, eight calls)", n)
	}
}

// people.md Settings: a hidden legal-name mode hides the field for everyone; the stored
// values stay in the database but LegalNamesOf returns none and audits nothing.
func TestLegalNamesOf_LegalNameHidden_ReturnsNoneAndAuditsNothing_Integration(t *testing.T) {
	f := newLegalFixture(t)
	_, ctx := f.admin(permContacts)
	f.pol.set(func(p *people.Policy) { p.LegalName = people.FieldHidden })
	got, err := f.svc.LegalNamesOf(ctx, []model.PersonID{f.a, f.b, f.c})
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Errorf("hidden mode returned %v", got)
	}
	if n := len(f.entries("people.person.read")); n != 0 {
		t.Errorf("%d people.person.read entries, want 0", n)
	}
	f.pol.set(func(p *people.Policy) { p.LegalName = people.FieldOptional })
	got, err = f.svc.LegalNamesOf(ctx, []model.PersonID{f.a})
	wantNoErr(t, err)
	if got[f.a] != "Odalys Brindle" {
		t.Errorf("stored name after leaving hidden mode = %q, want it back", got[f.a])
	}
}
