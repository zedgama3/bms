package departments

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/departments/postgres"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// lookupStore is the part of the storage the Lookup reads; postgres.Store implements it.
// Every error is already classified by db.Wrap.
type lookupStore interface {
	// NamesOf maps each known ID, archived included, to its name.
	NamesOf(ctx context.Context, ids []model.DepartmentID) (map[model.DepartmentID]string, error)
	// Active reports whether the department exists and is not archived.
	Active(ctx context.Context, id model.DepartmentID) (bool, error)
}

var _ lookupStore = (*postgres.Store)(nil)

// LookupOptions builds the Lookup.
type LookupOptions struct {
	DB     db.DB
	Tracer telemetry.Tracer
}

// Validate fails errs.Invalid joining a validate.FieldError per nil dependency (fields
// departments.db, departments.tracer).
func (o LookupOptions) Validate() error {
	v := validate.Errors{Op: "departments.LookupOptions.Validate"}
	if o.DB == nil {
		v.Addf("departments.db", codeRequired, "must be set")
	}
	if o.Tracer == nil {
		v.Addf("departments.tracer", codeRequired, "must be set")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Lookup is the Directory (NamesOf, Active) over core.department alone: no permission, no
// audit, internal data only. It is built before authz, which asks it whether a department
// is active. Safe for concurrent use; nothing to close (no goroutines).
type Lookup struct {
	store  lookupStore
	tracer telemetry.Tracer
}

var (
	_ Directory              = (*Lookup)(nil)
	_ authz.DepartmentSource = (*Lookup)(nil)
)

// NewLookup validates o and builds the Lookup over postgres.Store. No I/O: it works against
// db.None() and on a read-only copy. Fails errs.Invalid for a nil dependency.
func NewLookup(o LookupOptions) (*Lookup, error) {
	const op = "departments.NewLookup"
	if err := o.Validate(); err != nil {
		return nil, err
	}
	st, err := postgres.NewStore(o.DB)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return newLookup(o, st)
}

// newLookup builds the Lookup over st. Tests hand it a fake store.
func newLookup(o LookupOptions, st lookupStore) (*Lookup, error) {
	const op = "departments.NewLookup"
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if st == nil {
		return nil, errs.E(op, errs.Invalid, errors.New("nil store"))
	}
	return &Lookup{store: st, tracer: o.Tracer}, nil
}

// start opens the span of one use case, "departments.<name>".
func (l *Lookup) start(ctx context.Context, name string) (context.Context, telemetry.Span) {
	return l.tracer.Start(ctx, "departments."+name)
}

// NamesOf maps each known department, archived included, to its name; unknown IDs are
// absent. It needs no actor: it returns internal data to code that already passed its own
// permission check or runs as the system. Fails errs.Invalid (ids, too_many {max}) for more
// than MaxNames IDs, and with the store's kinds.
func (l *Lookup) NamesOf(ctx context.Context, ids []model.DepartmentID) (_ map[model.DepartmentID]string, err error) {
	const op = "departments.Lookup.NamesOf"
	ctx, span := l.start(ctx, "names-of")
	defer endSpan(span, &err)
	if len(ids) > MaxNames {
		return nil, paramInvalid(op, "ids", codeTooMany, map[string]string{"max": fmt.Sprint(MaxNames)},
			fmt.Sprintf("at most %d IDs", MaxNames))
	}
	unique := make([]model.DepartmentID, 0, len(ids))
	seen := make(map[model.DepartmentID]struct{}, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup || id.IsZero() {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	span.SetAttrs(telemetry.Int("ids", len(unique)))
	if len(unique) == 0 {
		return map[model.DepartmentID]string{}, nil
	}
	names, err := l.store.NamesOf(ctx, unique)
	if err != nil {
		return nil, wrap(op, err)
	}
	return names, nil
}

// Active reports whether the department exists and is not archived. It answers (false,
// nil) for an unknown and for an archived department alike, so it never leaks which. It
// needs no actor and joins the transaction ctx carries (authz's Grant calls it inside its
// own). Fails errs.Invalid (id, required) for the zero ID, and with the store's kinds.
func (l *Lookup) Active(ctx context.Context, id model.DepartmentID) (_ bool, err error) {
	const op = "departments.Lookup.Active"
	ctx, span := l.start(ctx, "active")
	defer endSpan(span, &err)
	if id.IsZero() {
		return false, fieldInvalid(op, "id", codeRequired, "must be set")
	}
	span.SetAttrs(telemetry.ID("department_id", id))
	ok, err := l.store.Active(ctx, id)
	if err != nil {
		return false, wrap(op, err)
	}
	return ok, nil
}
