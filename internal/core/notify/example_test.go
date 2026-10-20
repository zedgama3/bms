package notify_test

import (
	"fmt"

	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// A message names a template of its owner and its recipients; Validate reports every problem
// at once with a field and a code, and never echoes what the caller wrote.
func ExampleMessage_Validate() {
	m := notify.Message{
		Template: "volunteers.shift-reminder",
		To:       []model.PersonID{model.NewID[model.PersonID]()},
		Vars:     map[string]string{"shift": "Gate, 06:00"},
		Link:     "/volunteers/shifts/1",
	}
	fmt.Println(m.Validate() == nil)

	m.Link = "https://elsewhere.example"
	fmt.Println(errs.Is(m.Validate(), errs.Invalid))
	// Output:
	// true
	// true
}
