package db_test

import (
	"context"
	"errors"
	"fmt"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// A set's version is its highest file number; the files must be numbered from 0001 with
// no gap.
func ExampleVersion() {
	files := fstest.MapFS{
		"0001_init.sql":      {Data: []byte("CREATE TABLE volunteers.shift (id uuid PRIMARY KEY);")},
		"0002_add_notes.sql": {Data: []byte("ALTER TABLE volunteers.shift ADD COLUMN note text;")},
	}
	v, err := db.Version(files)
	fmt.Println(v, err)
	// Output: 2 <nil>
}

// Wrap turns a driver error into an errs kind and keeps only names: the constraint is
// there for a store to map to a field, the row value in Detail is gone.
func ExampleWrap() {
	driver := &pgconn.PgError{
		Code:           "23505",
		Detail:         "Key (email)=(ada@example.org) already exists.",
		SchemaName:     "people",
		TableName:      "person",
		ConstraintName: "person_email_key",
	}
	err := db.Wrap("people.postgres.Create", driver)

	var pg *db.Error
	fmt.Println(errs.Is(err, errs.Conflict), errors.As(err, &pg))
	fmt.Println(pg.Constraint)
	fmt.Println(pg)
	// Output:
	// true true
	// person_email_key
	// pg 23505 unique_violation on people.person constraint person_email_key
}

// None is the DB for `bms openapi` and `bms check`: every statement fails with
// Unavailable wrapping ErrNoDatabase.
func ExampleNone() {
	_, err := db.None().Exec(context.Background(), "SELECT 1")
	fmt.Println(errors.Is(err, db.ErrNoDatabase), errs.Is(err, errs.Unavailable))
	// Output: true true
}
