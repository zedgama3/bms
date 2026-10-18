// Package peopletest gives modules a people.Directory for their tests: Fake keeps names
// and consents in memory, so a module's use case that shows a person or checks a consent
// runs without a database.
package peopletest

import (
	"context"
	"fmt"
	"sync"

	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Fake implements people.Directory in memory. Safe for concurrent use. It answers what the
// real service answers for what a test can set up: known people with a name, consents by
// purpose, unknown people omitted and unknown consents false, the same Invalid refusals.
type Fake struct {
	mu       sync.Mutex
	names    map[model.PersonID]string
	consents map[model.PersonID]map[people.Purpose]bool
}

var _ people.Directory = (*Fake)(nil)

// NewFake returns an empty Fake: no person is known and nobody consents to anything.
// Cannot fail.
func NewFake() *Fake {
	return &Fake{
		names:    map[model.PersonID]string{},
		consents: map[model.PersonID]map[people.Purpose]bool{},
	}
}

// Add makes id a known person whose playa name is name ("" for one not filled in yet),
// replacing an earlier name. Cannot fail.
func (f *Fake) Add(id model.PersonID, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.names[id] = name
}

// Consent sets whether id consents to purpose. It does not make id a known person (the
// real service refuses a consent for a person with no row, but ConsentOf only reads).
// Cannot fail.
func (f *Fake) Consent(id model.PersonID, purpose people.Purpose, granted bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.consents[id] == nil {
		f.consents[id] = map[people.Purpose]bool{}
	}
	f.consents[id][purpose] = granted
}

// NamesOf returns the name of each known person among ids and omits the rest. Fails
// errs.Invalid (field ids, code too_many) when len(ids) > people.MaxNames, like the real
// service. Serves any caller.
func (f *Fake) NamesOf(_ context.Context, ids []model.PersonID) (map[model.PersonID]string, error) {
	const op = "peopletest.Fake.NamesOf"
	if len(ids) > people.MaxNames {
		return nil, errs.E(op, errs.Invalid, &validate.FieldError{
			Field: "ids", Code: "too_many", Params: map[string]string{"max": fmt.Sprint(people.MaxNames)},
			Reason: fmt.Sprintf("at most %d IDs", people.MaxNames),
		})
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[model.PersonID]string, len(ids))
	for _, id := range ids {
		if name, ok := f.names[id]; ok {
			out[id] = name
		}
	}
	return out, nil
}

// ConsentOf reports the consent set with Consent; false when none was set. Fails
// errs.Invalid for a bad purpose or a zero person, like the real service.
func (f *Fake) ConsentOf(_ context.Context, person model.PersonID, purpose people.Purpose) (bool, error) {
	if err := purpose.Validate(); err != nil {
		return false, fmt.Errorf("peopletest.Fake.ConsentOf: %w", err)
	}
	if person.IsZero() {
		return false, errs.E("peopletest.Fake.ConsentOf", errs.Invalid,
			&validate.FieldError{Field: "person", Code: validate.CodeRequired, Reason: "must be set"})
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.consents[person][purpose], nil
}
