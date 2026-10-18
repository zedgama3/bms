// Package departmentstest gives modules a departments.Directory for their tests: Fake keeps
// departments in memory, so a module's use case that labels a record or checks that a
// department is active runs without a database.
package departmentstest

import (
	"context"
	"fmt"
	"sync"

	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

type entry struct {
	name     string
	archived bool
}

// Fake implements departments.Directory (and so authz.DepartmentSource) in memory. Safe for
// concurrent use. It answers what the real service answers for what a test can set up:
// known departments with a name, archived ones named but not active, unknown ones absent
// and not active, and the same Invalid refusals.
type Fake struct {
	mu   sync.Mutex
	byID map[model.DepartmentID]entry
}

var _ departments.Directory = (*Fake)(nil)

// NewFake returns an empty Fake: no department is known. Cannot fail.
func NewFake() *Fake {
	return &Fake{byID: map[model.DepartmentID]entry{}}
}

// Add makes id a known, active department called name, replacing an earlier one (and
// restoring it). Cannot fail.
func (f *Fake) Add(id model.DepartmentID, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[id] = entry{name: name}
}

// Archive archives the department id; an unknown id is ignored. Cannot fail.
func (f *Fake) Archive(id model.DepartmentID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.byID[id]; ok {
		e.archived = true
		f.byID[id] = e
	}
}

// NamesOf returns the name of each known department among ids, archived ones included, and
// omits the rest. Fails errs.Invalid (field ids, code too_many) when len(ids) >
// departments.MaxNames, like the real service. Serves any caller.
func (f *Fake) NamesOf(_ context.Context, ids []model.DepartmentID) (map[model.DepartmentID]string, error) {
	const op = "departmentstest.Fake.NamesOf"
	if len(ids) > departments.MaxNames {
		return nil, errs.E(op, errs.Invalid, &validate.FieldError{
			Field: "ids", Code: "too_many", Params: map[string]string{"max": fmt.Sprint(departments.MaxNames)},
			Reason: fmt.Sprintf("at most %d IDs", departments.MaxNames),
		})
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[model.DepartmentID]string, len(ids))
	for _, id := range ids {
		if e, ok := f.byID[id]; ok {
			out[id] = e.name
		}
	}
	return out, nil
}

// Active reports whether id is known and not archived; (false, nil) otherwise. Fails
// errs.Invalid (field id, code required) for the zero ID, like the real service.
func (f *Fake) Active(_ context.Context, id model.DepartmentID) (bool, error) {
	if id.IsZero() {
		return false, errs.E("departmentstest.Fake.Active", errs.Invalid,
			&validate.FieldError{Field: "id", Code: validate.CodeRequired, Reason: "must be set"})
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.byID[id]
	return ok && !e.archived, nil
}
