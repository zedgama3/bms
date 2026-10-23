// Package peopletest gives modules a people.Directory for their tests: Fake keeps names
// and consents in memory, so a module's use case that shows a person or checks a consent
// runs without a database.
package peopletest

import (
	"context"
	"fmt"
	"strings"
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
	legal    map[model.PersonID]string
	consents map[model.PersonID]map[people.Purpose]bool
}

var _ people.Directory = (*Fake)(nil)

// NewFake returns an empty Fake: no person is known and nobody consents to anything.
// Cannot fail.
func NewFake() *Fake {
	return &Fake{
		names:    map[model.PersonID]string{},
		legal:    map[model.PersonID]string{},
		consents: map[model.PersonID]map[people.Purpose]bool{},
	}
}

// Add makes id a known person whose playa name is name ("" for one not filled in yet),
// replacing an earlier name. It is test set-up and enforces nothing: a test that seeds two
// people with one NameKey is seeding a state the real service makes impossible (decision
// 29), so use distinct names; SetName and RenameByManager enforce the rules. Cannot fail.
func (f *Fake) Add(id model.PersonID, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.names[id] = name
}

// SetName is what UpdateMe does to a playa name, in memory: it sets id's playa name to name
// (trimmed) and enforces the rules the real service and its unique index enforce (decisions
// 29 and 30), so a module's test sees them. A blank name fails errs.Invalid (field
// playa_name, code required). When id already has a name, the same string is a no-op, a
// respelling with the same people.NameKey is saved, and any other name fails errs.Invalid
// (code permanent). A name whose NameKey another person holds fails errs.Conflict (code
// taken), naming nobody. An unknown id becomes known, as UpdateMe's repair does. Nothing is
// changed on failure.
func (f *Fake) SetName(id model.PersonID, name string) error {
	return f.setName("peopletest.Fake.SetName", id, name, false)
}

// RenameByManager is what Update does to a set playa name for a holder of
// people.profiles.manage, in memory: like SetName, except that a name with another
// people.NameKey replaces a set one (decision 30). A blank name still fails errs.Invalid
// (code required), and a name another person holds still fails errs.Conflict (code taken).
func (f *Fake) RenameByManager(id model.PersonID, name string) error {
	return f.setName("peopletest.Fake.RenameByManager", id, name, true)
}

func (f *Fake) setName(op string, id model.PersonID, name string, manager bool) error {
	if id.IsZero() {
		return errs.E(op, errs.Invalid,
			&validate.FieldError{Field: "person", Code: validate.CodeRequired, Reason: "must be set"})
	}
	name = strings.TrimSpace(name)
	key := people.NameKey(name)
	if key == "" {
		return errs.E(op, errs.Invalid,
			&validate.FieldError{Field: "playa_name", Code: validate.CodeRequired, Reason: "must not be empty"})
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	cur := f.names[id]
	if cur == name {
		return nil
	}
	if cur != "" && !manager && people.NameKey(cur) != key {
		return errs.E(op, errs.Invalid,
			&validate.FieldError{Field: "playa_name", Code: "permanent", Reason: "a playa name cannot be changed once it is set"})
	}
	for other, n := range f.names {
		if other != id && people.NameKey(n) == key {
			return errs.E(op, errs.Conflict,
				&validate.FieldError{Field: "playa_name", Code: "taken", Reason: "another person already uses this playa name"})
		}
	}
	f.names[id] = name
	return nil
}

// AddLegalName sets the legal name LegalNamesOf returns for id, replacing an earlier one;
// "" removes it. It does not make id a known person. Cannot fail.
func (f *Fake) AddLegalName(id model.PersonID, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if name == "" {
		delete(f.legal, id)
		return
	}
	f.legal[id] = name
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

// LegalNamesOf returns the legal name set with AddLegalName for each of ids and omits the
// rest. Fails errs.Invalid (field ids, code too_many) when len(ids) > people.MaxNames, like
// the real service. The fake checks no permission and records no audit entry: a test of the
// permission or the audit entry uses the real service. Unlike the real service it serves
// any caller.
func (f *Fake) LegalNamesOf(_ context.Context, ids []model.PersonID) (map[model.PersonID]string, error) {
	const op = "peopletest.Fake.LegalNamesOf"
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
		if name, ok := f.legal[id]; ok {
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
