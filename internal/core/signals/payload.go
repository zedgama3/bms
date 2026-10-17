package signals

import (
	"fmt"
	"maps"
	"slices"

	"github.com/zedgama3/bms/internal/core/signals/internal/rules"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// RecordID is a record identifier whose kind is known only from the key it sits under.
// It is a model.ID, so it marshals as a canonical UUID string and converts to and from any
// typed ID by its underlying [16]byte.
type RecordID = model.ID[record]

type record struct{}

// Payload is what a signal carries: references to the records involved, by role, and
// nothing else. A payload cannot hold a name, an address, a note or a number, so no
// personal data can travel on a signal (privacy-data: "Never in ... signals"). A
// subscriber that needs more asks a capability or an extension point with the IDs (D19).
//
// Keys are idents (the same grammar as a Name segment). By convention the core's shared
// keys are "person", "department", "event" (model.PersonID, DepartmentID, EventID) and a
// module's own record is its kind ("shift", "application"); the publisher's spec lists
// each signal's keys and their ID types under "Signals". At most MaxRefs entries.
type Payload map[string]RecordID

// MaxRefs bounds a payload. Sixteen is far more than any signal so far needs; the bound
// exists so a payload cannot become a data channel.
const MaxRefs = 16

// repeatedKey marks, inside a payload built by NewPayload, that a key was given twice.
// It is not an ident, so no publisher can build it by accident; Validate reports it.
const repeatedKey = "!repeated"

// Entry is one payload reference, built by Ref.
type Entry struct {
	Key string
	ID  RecordID
}

// Ref builds an Entry from any typed ID:
//
//	signals.Ref("shift", shiftID), signals.Ref("person", personID)
//
// Cannot fail; NewPayload and Validate check it.
func Ref[T ~[16]byte](key string, id T) Entry {
	return Entry{Key: key, ID: RecordID(id)}
}

// NewPayload builds a Payload from entries. It cannot fail; a bad key, a zero ID, a
// repeated key or too many entries are caught by Validate, which Publish calls (F2-F4),
// so a publisher writes one line and gets one error. The first entry for a key wins.
func NewPayload(entries ...Entry) Payload {
	p := make(Payload, len(entries))
	for _, e := range entries {
		if _, dup := p[e.Key]; dup {
			p[repeatedKey] = RecordID{}
			continue
		}
		p[e.Key] = e.ID
	}
	return p
}

// Validate fails with errs.Invalid joining one validate.FieldError per problem, fields
// "payload.<key>" (key not an ident; zero ID) and "payload" (more than MaxRefs entries, or
// a key given twice to NewPayload). A nil or empty payload is valid: some signals name
// nothing but the fact.
func (p Payload) Validate() error {
	acc := validate.Errors{Op: "signals.Payload.Validate"}
	n := len(p)
	if _, rep := p[repeatedKey]; rep {
		n--
		acc.Addf("payload", "duplicate", "a key is given more than once")
	}
	if n > MaxRefs {
		acc.Add(errs.E("", errs.Invalid, &validate.FieldError{
			Field:  "payload",
			Code:   validate.CodeRange,
			Params: map[string]string{"min": "0", "max": fmt.Sprint(MaxRefs)},
			Reason: fmt.Sprintf("must hold at most %d references, got %d", MaxRefs, n),
		}))
	}
	for _, k := range slices.Sorted(maps.Keys(p)) {
		if k == repeatedKey {
			continue
		}
		field := "payload." + rules.Cap(k)
		if !rules.Ident(k) {
			acc.Addf(field, codeMalformed, "key must be an ident: lower-case letters, digits and single hyphens")
		}
		if p[k].IsZero() {
			acc.Addf(field, validate.CodeRequired, "must not be the zero ID (leave an optional reference out)")
		}
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Get returns the reference under key as the subscriber's typed ID:
//
//	shift, err := signals.Get[volunteers.ShiftID](s.Payload, "shift")
//
// It fails with errs.Invalid naming the key when the key is absent or its ID is zero
// (F5): the publisher did not keep its documented shape, and the handler should return
// that error so the delivery is logged against the publisher's name.
func Get[T ~[16]byte](p Payload, key string) (T, error) {
	var zero T
	id, ok := p[key]
	if !ok || id.IsZero() {
		return zero, errs.E("signals.Get", errs.Invalid, &validate.FieldError{
			Field:  "payload." + rules.Cap(key),
			Code:   validate.CodeRequired,
			Reason: fmt.Sprintf("payload has no reference under %q", rules.Cap(key)),
		})
	}
	return T(id), nil
}
