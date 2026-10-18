package people_test

import (
	"context"
	"fmt"

	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/people/peopletest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// A regional may hide or require the legal name and the phone; emergency contacts may be
// hidden or optional, never required.
func ExamplePolicy_Validate() {
	p := people.DefaultPolicy()
	p.LegalName = people.FieldRequired
	fmt.Println("required legal name:", p.Validate() == nil)

	p.EmergencyContacts = people.FieldRequired
	fmt.Println("required emergency contacts:", errs.Is(p.Validate(), errs.Invalid))
	// Output:
	// required legal name: true
	// required emergency contacts: true
}

// A consent purpose names its owner, the module or core area that asks.
func ExamplePurpose_Owner() {
	fmt.Println(people.Purpose("volunteers.share-contact").Owner())
	fmt.Println(people.Purpose("not a purpose").Owner() == "")
	// Output:
	// volunteers
	// true
}

// A module's tests use the fake directory: names for the people the test knows, and the
// consents it grants.
func Example_fakeDirectory() {
	dir := peopletest.NewFake()
	dusty := model.NewID[model.PersonID]()
	dir.Add(dusty, "Dusty Ember")
	dir.Consent(dusty, "volunteers.share-contact", true)

	ctx := context.Background()
	names, err := dir.NamesOf(ctx, []model.PersonID{dusty, model.NewID[model.PersonID]()})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(len(names), names[dusty])

	ok, err := dir.ConsentOf(ctx, dusty, "volunteers.share-contact")
	fmt.Println(ok, err)
	// Output:
	// 1 Dusty Ember
	// true <nil>
}
