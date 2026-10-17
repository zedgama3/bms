package model_test

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/zedgama3/bms/internal/model"
)

func ExampleParseID() {
	id, err := model.ParseID[model.PersonID]("0F8FAD5B-D9CB-469F-A165-70867728950E")
	fmt.Println(id, err)
	_, err = model.ParseID[model.PersonID]("not-a-uuid")
	fmt.Println(err != nil)
	// Output:
	// 0f8fad5b-d9cb-469f-a165-70867728950e <nil>
	// true
}

func ExampleSensitivity_ReadIsAudited() {
	fmt.Println(model.SensitivityPersonal.ReadIsAudited(false))
	fmt.Println(model.SensitivityPersonal.ReadIsAudited(true))
	fmt.Println(model.SensitivitySensitive.ReadIsAudited(true))
	// Output:
	// true
	// false
	// true
}

func ExampleTimeRange_Overlaps() {
	t0 := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	morning := model.TimeRange{Start: t0, End: t0.Add(4 * time.Hour)}
	afternoon := model.TimeRange{Start: t0.Add(4 * time.Hour), End: t0.Add(8 * time.Hour)}
	fmt.Println(morning.Overlaps(afternoon), morning.Duration())
	// Output: false 4h0m0s
}

func ExampleParseEmail() {
	e, err := model.ParseEmail("  Pat.Example@Example.org ")
	fmt.Println(e, e.Canonical(), err)
	out, err := json.Marshal(struct {
		ID model.EventID `json:"id,omitzero"`
	}{})
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(string(out))
	// Output:
	// Pat.Example@Example.org pat.example@example.org <nil>
	// {}
}
