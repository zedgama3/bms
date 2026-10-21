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

// D44: Fake.AddLegalName seeds Directory.LegalNamesOf. All names are invented.

func TestFake_LegalNamesOf_KnownAndUnknown(t *testing.T) {
	f := peopletest.NewFake()
	a, b, none, unknown := newPerson(), newPerson(), newPerson(), newPerson()
	f.AddLegalName(a, "Odalys Brindle")
	f.AddLegalName(b, "Fenwick Aldous Tarn")
	f.Add(none, "Playa Only") // a playa name is not a legal name
	got, err := f.LegalNamesOf(context.Background(), []model.PersonID{a, b, none, unknown})
	if err != nil {
		t.Fatalf("LegalNamesOf: %v", err)
	}
	if len(got) != 2 || got[a] != "Odalys Brindle" || got[b] != "Fenwick Aldous Tarn" {
		t.Errorf("got %v, want the two seeded names", got)
	}
	for _, p := range []model.PersonID{none, unknown} {
		if _, ok := got[p]; ok {
			t.Errorf("person %s present in the result, want absent", p)
		}
	}
}

func TestFake_LegalNamesOf_DoesNotChangeNamesOf(t *testing.T) {
	f := peopletest.NewFake()
	p := newPerson()
	f.AddLegalName(p, "Odalys Brindle")
	got, err := f.NamesOf(context.Background(), []model.PersonID{p})
	if err != nil {
		t.Fatalf("NamesOf: %v", err)
	}
	for _, n := range got {
		if n == "Odalys Brindle" {
			t.Error("NamesOf returned a legal name")
		}
	}
}

func TestFake_AddLegalName_Overwrites(t *testing.T) {
	f := peopletest.NewFake()
	p := newPerson()
	f.AddLegalName(p, "Odalys Brindle")
	f.AddLegalName(p, "Odalys Quarrel")
	got, err := f.LegalNamesOf(context.Background(), []model.PersonID{p})
	if err != nil || got[p] != "Odalys Quarrel" {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestFake_LegalNamesOf_EmptyAndNil(t *testing.T) {
	f := peopletest.NewFake()
	for _, ids := range [][]model.PersonID{nil, {}} {
		got, err := f.LegalNamesOf(context.Background(), ids)
		if err != nil || len(got) != 0 {
			t.Errorf("ids %v: got %v, %v", ids, got, err)
		}
	}
}

func TestFake_LegalNamesOf_TooMany_ReturnsInvalid(t *testing.T) {
	f := peopletest.NewFake()
	ids := make([]model.PersonID, people.MaxNames+1)
	for i := range ids {
		ids[i] = newPerson()
	}
	_, err := f.LegalNamesOf(context.Background(), ids)
	if err == nil || !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
	if _, err := f.LegalNamesOf(context.Background(), ids[:people.MaxNames]); err != nil {
		t.Errorf("exactly MaxNames ids: %v", err)
	}
}

func TestFake_LegalNamesOf_ReturnsACopy(t *testing.T) {
	f := peopletest.NewFake()
	p := newPerson()
	f.AddLegalName(p, "Odalys Brindle")
	got, err := f.LegalNamesOf(context.Background(), []model.PersonID{p})
	if err != nil {
		t.Fatal(err)
	}
	got[p] = "changed"
	delete(got, p)
	again, err := f.LegalNamesOf(context.Background(), []model.PersonID{p})
	if err != nil {
		t.Fatal(err)
	}
	if again[p] != "Odalys Brindle" {
		t.Errorf("a caller's edit reached the fake: %v", again)
	}
}

func TestFake_LegalNames_ConcurrentUse_IsSafe(t *testing.T) {
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
				f.AddLegalName(id, fmt.Sprintf("Burner Legal %d-%d", i, j))
				if _, err := f.LegalNamesOf(context.Background(), ids); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	got, err := f.LegalNamesOf(context.Background(), ids)
	if err != nil || len(got) != len(ids) {
		t.Errorf("got %d names, %v", len(got), err)
	}
}
