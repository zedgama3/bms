package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity/account"
	"github.com/zedgama3/bms/internal/core/identity/postgres"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// lookupStore is the part of the storage the Lookup reads; postgres.Store implements it.
// Every error is already classified by db.Wrap.
type lookupStore interface {
	// ExportByPerson returns the person's account without the hash; NotFound when none.
	ExportByPerson(ctx context.Context, p model.PersonID) (account.Record, error)
}

var _ lookupStore = (*postgres.Store)(nil)

// LookupOptions builds the Lookup.
type LookupOptions struct {
	DB     db.DB
	Tracer telemetry.Tracer
}

// Validate fails errs.Invalid joining a validate.FieldError per nil dependency (fields
// identity.db, identity.tracer).
func (o LookupOptions) Validate() error {
	v := validate.Errors{Op: "identity.LookupOptions.Validate"}
	if o.DB == nil {
		v.Addf("identity.db", codeRequired, "must be set")
	}
	if o.Tracer == nil {
		v.Addf("identity.tracer", codeRequired, "must be set")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Lookup is the read of a person's login address over core.account alone: no permission and
// no audit, because the address is used to deliver mail (notify) and by people's link flow,
// both system-level or already authorized by the caller. It is built before notify, which
// takes it as its Accounts. Safe for concurrent use; nothing to close (no goroutines).
type Lookup struct {
	store  lookupStore
	tracer telemetry.Tracer
}

// NewLookup validates o and builds the Lookup over postgres.Store. No I/O: it works against
// db.None() and on a read-only copy. Fails errs.Invalid for a nil dependency.
func NewLookup(o LookupOptions) (*Lookup, error) {
	const op = "identity.NewLookup"
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
	const op = "identity.NewLookup"
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if st == nil {
		return nil, errs.E(op, errs.Invalid, errors.New("nil store"))
	}
	return &Lookup{store: st, tracer: o.Tracer}, nil
}

// EmailOf returns the login address of the person's account. It checks no permission and
// records nothing. Works on a read-only copy. Fails Invalid for a zero ID, NotFound when the
// person has no account (F64) and store kinds. The caller never logs the address.
func (l *Lookup) EmailOf(ctx context.Context, person model.PersonID) (_ model.Email, err error) {
	const op = "identity.Lookup.EmailOf"
	ctx, span := l.tracer.Start(ctx, "identity.email-of")
	defer endSpan(span, &err)

	if person.IsZero() {
		return "", fieldInvalid(op, "person", codeRequired, "must be set")
	}
	rec, err := l.store.ExportByPerson(ctx, person)
	if err != nil {
		return "", wrap(op, err)
	}
	return rec.Email, nil
}
