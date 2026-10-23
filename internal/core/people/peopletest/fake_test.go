package peopletest_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/people/peopletest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

var _ people.Directory = (*peopletest.Fake)(nil)

func newPerson() model.PersonID { return model.NewID[model.PersonID]() }

func wantKind(t testing.TB, err error, kind errs.Kind) {
	t.Helper()
	if err == nil || !errs.Is(err, kind) {
		t.Fatalf("err = %v (kind %v), want kind %v", err, errs.KindOf(err), kind)
	}
}

func TestFake_NamesOf_KnownUnknownAndUnfilled(t *testing.T) {
	f := peopletest.NewFake()
	a, b, unknown := newPerson(), newPerson(), newPerson()
	f.Add(a, "Dusty Ember")
	f.Add(b, "") // an unfilled name is ""
	got, err := f.NamesOf(context.Background(), []model.PersonID{a, b, unknown, a})
	if err != nil {
		t.Fatal(err)
	}
	if got[a] != "Dusty Ember" {
		t.Errorf("name = %q", got[a])
	}
	if name, ok := got[b]; !ok || name != "" {
		t.Errorf("unfilled = %q, present %v", name, ok)
	}
	if _, ok := got[unknown]; ok {
		t.Error("an unknown ID is in the map")
	}
	if len(got) != 2 {
		t.Errorf("map = %v", got)
	}
}

func TestFake_Add_Overwrites(t *testing.T) {
	f := peopletest.NewFake()
	a := newPerson()
	f.Add(a, "Dusty Ember")
	f.Add(a, "Dusty Cinder")
	got, err := f.NamesOf(context.Background(), []model.PersonID{a})
	if err != nil || got[a] != "Dusty Cinder" {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestFake_NamesOf_EmptyAndNil(t *testing.T) {
	f := peopletest.NewFake()
	for name, ids := range map[string][]model.PersonID{"nil": nil, "empty": {}} {
		got, err := f.NamesOf(context.Background(), ids)
		if err != nil || len(got) != 0 {
			t.Errorf("%s: %v, %v", name, got, err)
		}
	}
}

func TestFake_NamesOf_TooMany_ReturnsInvalid(t *testing.T) {
	f := peopletest.NewFake()
	ids := make([]model.PersonID, people.MaxNames+1)
	for i := range ids {
		ids[i] = newPerson()
	}
	_, err := f.NamesOf(context.Background(), ids)
	wantKind(t, err, errs.Invalid)
	if _, err := f.NamesOf(context.Background(), ids[:people.MaxNames]); err != nil {
		t.Errorf("exactly MaxNames: %v", err)
	}
}

func TestFake_NamesOf_ReturnsACopy(t *testing.T) {
	f := peopletest.NewFake()
	a := newPerson()
	f.Add(a, "Dusty Ember")
	got, err := f.NamesOf(context.Background(), []model.PersonID{a})
	if err != nil {
		t.Fatal(err)
	}
	got[a] = "Changed"
	delete(got, a)
	again, err := f.NamesOf(context.Background(), []model.PersonID{a})
	if err != nil {
		t.Fatal(err)
	}
	if again[a] != "Dusty Ember" {
		t.Errorf("the caller changed the fake's state: %v", again)
	}
}

func TestFake_ConsentOf(t *testing.T) {
	f := peopletest.NewFake()
	a := newPerson()
	const purpose people.Purpose = "volunteers.share-contact"
	ok, err := f.ConsentOf(context.Background(), a, purpose)
	if err != nil || ok {
		t.Fatalf("unknown person: %v, %v", ok, err)
	}
	f.Consent(a, purpose, true)
	ok, err = f.ConsentOf(context.Background(), a, purpose)
	if err != nil || !ok {
		t.Fatalf("granted: %v, %v", ok, err)
	}
	ok, err = f.ConsentOf(context.Background(), a, "volunteers.other")
	if err != nil || ok {
		t.Error("consent for another purpose")
	}
	f.Consent(a, purpose, false)
	ok, err = f.ConsentOf(context.Background(), a, purpose)
	if err != nil || ok {
		t.Error("revoked consent still granted")
	}
}

func TestFake_ConsentOf_BadInput_ReturnsInvalid(t *testing.T) {
	f := peopletest.NewFake()
	for _, bad := range []people.Purpose{"", "nodot", "Upper.case", "a.b c"} {
		_, err := f.ConsentOf(context.Background(), newPerson(), bad)
		wantKind(t, err, errs.Invalid)
	}
	_, err := f.ConsentOf(context.Background(), model.PersonID{}, "volunteers.share-contact")
	wantKind(t, err, errs.Invalid)
}

func TestFake_ConcurrentUse_IsSafe(t *testing.T) {
	f := peopletest.NewFake()
	ids := make([]model.PersonID, 20)
	for i := range ids {
		ids[i] = newPerson()
	}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j, id := range ids {
				f.Add(id, fmt.Sprintf("Burner %d-%d", i, j))
				f.Consent(id, "volunteers.share-contact", j%2 == 0)
				if _, err := f.NamesOf(context.Background(), ids); err != nil {
					t.Error(err)
				}
				if _, err := f.ConsentOf(context.Background(), id, "volunteers.share-contact"); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	got, err := f.NamesOf(context.Background(), ids)
	if err != nil || len(got) != len(ids) {
		t.Errorf("got %d names, %v", len(got), err)
	}
}

func TestFake_SetName_FirstNameAndRespelling(t *testing.T) {
	f := peopletest.NewFake()
	a := newPerson()
	if err := f.SetName(a, " Dusty  Ember "); err != nil {
		t.Fatal(err)
	}
	if err := f.SetName(a, "Dusty Ember"); err != nil { // same string once trimmed: a no-op
		t.Fatal(err)
	}
	if err := f.SetName(a, "DUSTY EMBER"); err != nil { // a respelling with the same key
		t.Fatal(err)
	}
	got, err := f.NamesOf(context.Background(), []model.PersonID{a})
	if err != nil || got[a] != "DUSTY EMBER" {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestFake_RenameByManager_ReplacesASetName(t *testing.T) {
	f := peopletest.NewFake()
	a := newPerson()
	f.Add(a, "Rude Name")
	if err := f.RenameByManager(a, "Ember Glow"); err != nil {
		t.Fatal(err)
	}
	got, err := f.NamesOf(context.Background(), []model.PersonID{a})
	if err != nil || got[a] != "Ember Glow" {
		t.Errorf("got %v, %v", got, err)
	}
}
