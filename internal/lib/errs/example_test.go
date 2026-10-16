package errs_test

import (
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/lib/errs"
)

func ExampleE() {
	// A storage boundary classifies a driver error and names the operation.
	driverErr := errors.New("dial tcp 10.0.0.1:5432: connection refused")
	err := errs.E("volunteers.postgres.CreateShift", errs.Unavailable, driverErr)

	// A caller higher up adds context with %w, which keeps the inner kind.
	err = fmt.Errorf("volunteers.Service.Enroll: %w", err)

	fmt.Println(err)
	fmt.Println(errs.KindOf(err))
	// Output:
	// volunteers.Service.Enroll: volunteers.postgres.CreateShift: dial tcp 10.0.0.1:5432: connection refused
	// unavailable
}

func ExampleIs() {
	err := fmt.Errorf("shift %s: %w", "s-42",
		errs.E("identity.oidc.Exchange", errs.Permission, errors.New("login failed")))

	// Decide by kind, never by message. fmt.Errorf with %w keeps the inner kind.
	switch {
	case errs.Is(err, errs.Unavailable), errs.Is(err, errs.Timeout):
		fmt.Println("back off and retry")
	case errs.Is(err, errs.Permission):
		fmt.Println("report missing rights")
	default:
		fmt.Println("fail")
	}
	// Output:
	// report missing rights
}
