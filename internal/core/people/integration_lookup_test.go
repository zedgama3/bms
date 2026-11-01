//go:build integration

package people_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func (r *irig) lookup() *people.Lookup {
	r.t.Helper()
	c := telemetrytest.New(r.t)
	l, err := people.NewLookup(people.LookupOptions{DB: r.pool, Tracer: c.Tracer("people")})
	wantNoErr(r.t, err)
	return l
}

// boundaries.md B16: a deleted person is NotFound to every read that has a person to find
// (notify then sends with no name, no language and, if no login address, no address).
func TestLookup_ContactEmailOf_Deleted_ReturnsNotFound(t *testing.T) {
	r := newIRig(t, false)
	r.link()
	l := r.lookup()
	p := r.insertPerson(seed{Playa: "Dusty Ember", ContactEmail: "dusty@example.org"})
	if got, err := l.ContactEmailOf(ctxT(t), p); err != nil || got != "dusty@example.org" {
		t.Fatalf("before deletion: ContactEmailOf = %q, %v", got, err)
	}
	_, ctx := r.admin(permDelete)
	_, err := r.svc.Delete(ctx, p)
	wantNoErr(t, err)
	_, err = l.ContactEmailOf(ctxT(t), p)
	wantKind(t, err, errs.NotFound)
	_, err = l.PlayaNameOf(ctxT(t), p)
	wantKind(t, err, errs.NotFound)
	_, err = l.LanguageOf(ctxT(t), p)
	wantKind(t, err, errs.NotFound)
}

func TestLookup_UnknownPerson_ReadsReturnNotFound(t *testing.T) {
	r := newIRig(t, false)
	l := r.lookup()
	unknown := newPerson()
	_, err := l.ContactEmailOf(ctxT(t), unknown)
	wantKind(t, err, errs.NotFound)
	_, err = l.PlayaNameOf(ctxT(t), unknown)
	wantKind(t, err, errs.NotFound)
	_, err = l.LanguageOf(ctxT(t), unknown)
	wantKind(t, err, errs.NotFound)
	// NamesOf omits what it does not know; ConsentOf says false.
	names, err := l.NamesOf(ctxT(t), []model.PersonID{unknown})
	wantNoErr(t, err)
	if len(names) != 0 {
		t.Errorf("NamesOf(unknown) = %v", names)
	}
	ok, err := l.ConsentOf(ctxT(t), unknown, "volunteers.share-contact")
	wantNoErr(t, err)
	if ok {
		t.Error("consent for an unknown person")
	}
}

// The Lookup answers what the Service answers, with no actor, no permission and no audit:
// the contact address is personal but used only to deliver mail (notify.md "Addresses").
func TestLookup_Reads_MatchTheService_NoActorNoAudit(t *testing.T) {
	r := newIRig(t, false)
	r.link()
	l := r.lookup()
	p := r.insertPerson(seed{Playa: "Dusty Ember", ContactEmail: "dusty@example.org"})
	before := len(r.rec.Recorded())
	got, err := l.ContactEmailOf(ctxT(t), p) // no session, no actor in the ctx
	wantNoErr(t, err)
	want, err := r.svc.ContactEmailOf(ctxT(t), p)
	wantNoErr(t, err)
	if got != want || got != "dusty@example.org" {
		t.Errorf("Lookup %q, Service %q", got, want)
	}
	name, err := l.PlayaNameOf(ctxT(t), p)
	wantNoErr(t, err)
	if name != "Dusty Ember" {
		t.Errorf("PlayaNameOf = %q", name)
	}
	names, err := l.NamesOf(ctxT(t), []model.PersonID{p})
	wantNoErr(t, err)
	if names[p] != "Dusty Ember" {
		t.Errorf("NamesOf = %v", names)
	}
	if _, err := l.LanguageOf(ctxT(t), p); err != nil {
		t.Errorf("LanguageOf: %v", err)
	}
	if len(r.rec.Recorded()) != before {
		t.Error("the Lookup recorded an audit entry")
	}
}

func TestLookup_ZeroPerson_ReturnsInvalid(t *testing.T) {
	r := newIRig(t, false)
	_, err := r.lookup().ContactEmailOf(ctxT(t), model.PersonID{})
	wantKind(t, err, errs.Invalid)
}
