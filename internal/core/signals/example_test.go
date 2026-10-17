package signals_test

import (
	"fmt"

	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func ExampleName_Validate() {
	fmt.Println(signals.Name("volunteers.shift-completed").Validate())
	err := signals.Name("Volunteers_Shift").Validate()
	fmt.Println(errs.Is(err, errs.Invalid))
	// Output:
	// <nil>
	// true
}

func ExampleGet() {
	person := model.NewID[model.PersonID]()
	p := signals.NewPayload(signals.Ref("person", person))

	got, err := signals.Get[model.PersonID](p, "person")
	fmt.Println(err, got == person)

	_, err = signals.Get[model.PersonID](p, "shift")
	fmt.Println(errs.Is(err, errs.Invalid))
	// Output:
	// <nil> true
	// true
}

func ExamplePayload_Validate() {
	person := model.NewID[model.PersonID]()
	fmt.Println(signals.NewPayload(signals.Ref("person", person)).Validate())

	bad := signals.NewPayload(signals.Ref("Person", person), signals.Ref("shift", model.PersonID{}))
	fmt.Println(errs.Is(bad.Validate(), errs.Invalid))
	// Output:
	// <nil>
	// true
}
