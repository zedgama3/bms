// Package rows holds the row types demo's service and its PostgreSQL store share. It is a
// leaf: it imports model only, because demo imports postgres to build its store and so
// postgres cannot import demo.
package rows

import (
	"time"

	"github.com/zedgama3/bms/internal/model"
)

// The codes of the Conflict errors the store and the service report on the field "demo".
const (
	// Field is the validate.FieldError field of those conflicts.
	Field = "demo"
	// CodeSeeded: the marker row exists (Seed on a seeded database).
	CodeSeeded = "seeded"
	// CodeNotEmpty: a non-superuser account exists (Seed on a database with data).
	CodeNotEmpty = "not_empty"
	// CodeNotDemo: no marker row (Reset on a database the seed never marked).
	CodeNotDemo = "not_demo"
)

// State is the row of core.demo_state, the marker that this database is a demo. A zero
// SeededAt is NULL: the seed is running or failed.
type State struct {
	SeedVersion int
	Instance    string
	StartedAt   time.Time
	SeededAt    time.Time
}

// Account is a row of core.demo_account: one shared demo account. Email is the generated
// "<key>@demo.invalid" address.
type Account struct {
	Account  model.AccountID
	Person   model.PersonID
	Key      string
	Email    string
	Position int
}

// Mail is a row of core.demo_mail: one captured notification. ID is the row's own UUID
// (demo.MailID). To, Subject and Body are personal.
type Mail struct {
	ID         [16]byte
	To         string
	Subject    string
	Body       string
	MessageID  string
	CapturedAt time.Time
}
