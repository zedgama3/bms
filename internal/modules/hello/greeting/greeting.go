// Package greeting holds the record type shared by hello's service and its postgres store.
package greeting

import (
	"time"

	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// MaxBytes is the longest greeting text, in bytes.
const MaxBytes = 64

type marker struct{}

// ID identifies one saved greeting.
type ID = model.ID[marker]

// Greeting is one saved greeting: who greeted, with which word, when.
type Greeting struct {
	ID        ID             `json:"id"`
	Person    model.PersonID `json:"-"` // never on the wire; the caller already knows who they are
	Text      string         `json:"greeting"`
	CreatedAt time.Time      `json:"created_at"`
}

// Validate fails with errs.Invalid (joined validate.FieldErrors on "id", "person",
// "greeting", "created_at") for a zero ID, a zero person, an empty or over-long Text
// (MaxBytes = 64) or a zero CreatedAt.
func (g Greeting) Validate() error {
	acc := validate.Errors{Op: "greeting.Greeting.Validate"}
	if g.ID.IsZero() {
		acc.Addf("id", validate.CodeRequired, "must be set")
	}
	if g.Person.IsZero() {
		acc.Addf("person", validate.CodeRequired, "must be set")
	}
	acc.Add(validate.Range("greeting", len(g.Text), 1, MaxBytes))
	if g.CreatedAt.IsZero() {
		acc.Addf("created_at", validate.CodeRequired, "must be set")
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}
