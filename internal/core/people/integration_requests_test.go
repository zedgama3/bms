//go:build integration

package people_test

import (
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/model"
)

// ---- the account-created handler --------------------------------------------------------

func TestHandleAccountCreated_Twice_InsertsOnce(t *testing.T) {
	r := newIRig(t, false)
	acct, p := model.NewID[model.AccountID](), newPerson()
	signedIn.Store(p, true) // the account exists, as sign-up committed before the delivery
	sig := accountCreatedSignal(acct, p)
	wantNoErr(t, r.bus.Deliver(ctxT(t), sig))
	if !r.rowExists(p) {
		t.Fatal("no row after the first delivery")
	}
	prof, err := r.svc.PlayaNameOf(ctxT(t), p)
	wantNoErr(t, err)
	if prof != "" {
		t.Errorf("a new account's name = %q, want empty", prof)
	}
	e := r.only("people.person.created")
	if e.Subject != p {
		t.Errorf("entry subject = %v", e.Subject)
	}
	if e.Actor.Kind != 0 && e.Actor.Kind.String() != "system" {
		t.Errorf("actor kind = %v, want the system", e.Actor.Kind)
	}
	if !e.Actor.Person.IsZero() {
		t.Error("the handler recorded a person as its actor")
	}
	if len(r.published(sigCreated)) != 1 {
		t.Fatal("people.person-created not published once")
	}
	// Redelivery is harmless and silent.
	wantNoErr(t, r.bus.Deliver(ctxT(t), sig))
	if n := r.personRows(); n != 1 {
		t.Errorf("%d rows after redelivery", n)
	}
	if len(r.published(sigCreated)) != 1 || r.count("people.person.created") != 1 {
		t.Error("the redelivery published or recorded again")
	}
}

// A delivery that arrives after the account was deleted (a deletion that succeeded without a
// row, D37) must not bring the person row back.
func TestHandleAccountCreated_AccountGone_MakesNoRow(t *testing.T) {
	f := newIRig(t, false)
	ghost := newPerson() // an account holder whose account was deleted before the delivery
	f.accts.mu.Lock()
	f.accts.gone[ghost] = true
	f.accts.mu.Unlock()
	before := len(f.published(sigCreated))
	wantNoErr(t, f.bus.Deliver(ctxT(t), accountCreatedSignal(model.NewID[model.AccountID](), ghost)))
	if f.rowExists(ghost) {
		t.Error("a late account-created delivery left an orphan row")
	}
	if len(f.published(sigCreated)) != before || f.count("people.person.created") != 0 {
		t.Error("the late delivery announced or recorded a person")
	}
}

func TestHandleAccountCreated_ExistingRow_Untouched(t *testing.T) {
	r := newIRig(t, false)
	p := r.insertPerson(seed{Playa: "Dusty Ember", Legal: mLegal})
	wantNoErr(t, r.bus.Deliver(ctxT(t), accountCreatedSignal(model.NewID[model.AccountID](), p)))
	name, err := r.svc.PlayaNameOf(ctxT(t), p)
	wantNoErr(t, err)
	if name != "Dusty Ember" {
		t.Errorf("name = %q, the handler overwrote an existing row", name)
	}
	if len(r.published(sigCreated)) != 0 || r.count("people.person.created") != 0 {
		t.Error("the handler announced a row it did not create")
	}
}

func TestHandleAccountCreated_Concurrent_PublishesOnce(t *testing.T) {
	r := newIRig(t, false)
	p := newPerson()
	signedIn.Store(p, true) // the account exists, as sign-up committed before the delivery
	sig := accountCreatedSignal(model.NewID[model.AccountID](), p)
	var wg sync.WaitGroup
	errc := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errc <- r.bus.Deliver(ctxT(t), sig)
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		if err != nil {
			t.Errorf("delivery: %v", err)
		}
	}
	if n := r.personRows(); n != 1 {
		t.Errorf("%d rows, want 1", n)
	}
	if n := len(r.published(sigCreated)); n != 1 {
		t.Errorf("%d people.person-created signals, want 1", n)
	}
}

func TestSignUp_ThenDelivery_GivesTheSessionAName(t *testing.T) {
	r := newIRig(t, true)
	u := r.signUp("new.burner", true)
	if !r.rowExists(u.person) {
		t.Fatal("no person row for the new account")
	}
	prof, err := r.svc.Me(u.ctx(t))
	wantNoErr(t, err)
	if !prof.Person.HasAccount || prof.Person.Email != u.email {
		t.Errorf("Me = %+v, want the account's address", prof.Person)
	}
	if prof.Person.Complete {
		t.Error("a profile with no playa name is Complete")
	}
	_, err = r.svc.UpdateMe(u.ctx(t), people.ProfileInput{PlayaName: str("Dusty Ember")})
	wantNoErr(t, err)
	name, err := r.svc.PlayaNameOf(ctxT(t), u.person)
	wantNoErr(t, err)
	if name != "Dusty Ember" {
		t.Errorf("PlayaNameOf = %q", name)
	}
}

// When the signal never arrives the first save repairs the row.
func TestSignUp_MissedSignal_FirstSaveRepairs(t *testing.T) {
	r := newIRig(t, true)
	u := r.signUp("missed.signal", false)
	if r.rowExists(u.person) {
		t.Fatal("a row exists although no signal was delivered")
	}
	prof, err := r.svc.Me(u.ctx(t))
	wantNoErr(t, err)
	if prof.Person.ID != u.person || prof.Person.Complete {
		t.Errorf("Me = %+v", prof.Person)
	}
	if r.rowExists(u.person) {
		t.Error("Me wrote a row")
	}
	_, err = r.svc.UpdateMe(u.ctx(t), people.ProfileInput{PlayaName: str("Late Bloomer")})
	wantNoErr(t, err)
	if !r.rowExists(u.person) {
		t.Fatal("the first save did not create the row")
	}
	// A late delivery changes nothing.
	r.deliverCreated(u)
	name, err := r.svc.PlayaNameOf(ctxT(t), u.person)
	wantNoErr(t, err)
	if name != "Late Bloomer" {
		t.Errorf("name = %q after a late delivery", name)
	}
}
