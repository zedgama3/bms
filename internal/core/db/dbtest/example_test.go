package dbtest_test

import (
	"context"
	"fmt"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
)

// Fake records the statements a store ran, in a transaction or not, and runs AfterCommit
// hooks once the transaction function returned nil.
func ExampleFake() {
	f := dbtest.NewFake()
	ctx := context.Background()

	err := f.Transact(ctx, db.Write, func(tx context.Context) error {
		if _, err := f.Exec(tx, "INSERT INTO hello.greeting (id) VALUES ($1)", 1); err != nil {
			return err //nolint:wrapcheck // example closure
		}
		return db.AfterCommit(tx, func(context.Context) { fmt.Println("committed") })
	})
	fmt.Println(err)
	for _, c := range f.Calls() {
		fmt.Println(c.SQL, c.InTx)
	}
	// Output:
	// committed
	// <nil>
	// INSERT INTO hello.greeting (id) VALUES ($1) true
}
