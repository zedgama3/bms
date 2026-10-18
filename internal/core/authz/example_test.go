package authz_test

import (
	"context"
	"fmt"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/model"
)

// A list use case asks ExtentOf once and filters by it; a single-record use case asks
// AllowIn. The Fake stands in for the service in a module's tests. The people here are
// invented.
func Example() {
	const shifts authz.Permission = "volunteers.shifts.manage"
	fake := authztest.NewFake()
	lead := model.NewID[model.PersonID]()
	gate := model.NewID[model.DepartmentID]()
	kitchen := model.NewID[model.DepartmentID]()
	fake.GrantIn(lead, shifts, gate)

	ctx := authztest.WithActor(context.Background(), authz.Actor{Person: lead})
	ext, err := fake.ExtentOf(ctx, shifts)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("site-wide:", ext.Site)
	fmt.Println("gate:", ext.Covers(gate), "kitchen:", ext.Covers(kitchen))
	fmt.Println("allow in gate:", fake.AllowIn(ctx, shifts, gate) == nil)
	fmt.Println("allow in kitchen:", fake.AllowIn(ctx, shifts, kitchen) == nil)
	// Output:
	// site-wide: false
	// gate: true kitchen: false
	// allow in gate: true
	// allow in kitchen: false
}

func ExampleExtent_IsZero() {
	fmt.Println(authz.Extent{}.IsZero(), authz.Extent{Site: true}.IsZero())
	// Output: true false
}
