package people

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people/postgres"
	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// lookupStore is the part of the storage the Lookup reads; postgres.Store implements it.
// Every error is already classified by db.Wrap. A single-row read of a missing row is
// NotFound.
type lookupStore interface {
	GetPerson(ctx context.Context, id model.PersonID) (rows.Person, error)
	PlayaNameOf(ctx context.Context, id model.PersonID) (string, error)
	NamesOf(ctx context.Context, ids []model.PersonID) (map[model.PersonID]string, error)
	Consent(ctx context.Context, person model.PersonID, purpose string) (bool, error)
}

var _ lookupStore = (*postgres.Store)(nil)

// LookupOptions builds the Lookup.
type LookupOptions struct {
	DB     db.DB
	Tracer telemetry.Tracer
}

// Validate fails errs.Invalid joining a validate.FieldError per nil dependency (fields
// people.db, people.tracer).
func (o LookupOptions) Validate() error {
	v := validate.Errors{Op: "people.LookupOptions.Validate"}
	if o.DB == nil {
		v.Addf("people.db", codeRequired, "must be set")
	}
	if o.Tracer == nil {
		v.Addf("people.tracer", codeRequired, "must be set")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Lookup is the system-level reads of core.person: NamesOf, PlayaNameOf and ConsentOf
// (internal level), LanguageOf (internal) and ContactEmailOf (personal: used only to deliver
// mail, never shown to a viewer). No permission, no audit, never on a route. It is built
// before notify and identity, which take it. Safe for concurrent use; nothing to close (no
// goroutines).
type Lookup struct {
	store  lookupStore
	tracer telemetry.Tracer
}

var _ identity.NameSource = (*Lookup)(nil)

// NewLookup validates o and builds the Lookup over postgres.Store. No I/O: it works against
// db.None() and on a read-only copy. Fails errs.Invalid for a nil dependency.
func NewLookup(o LookupOptions) (*Lookup, error) {
	const op = "people.NewLookup"
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
	const op = "people.NewLookup"
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if st == nil {
		return nil, errs.E(op, errs.Invalid, errors.New("nil store"))
	}
	return &Lookup{store: st, tracer: o.Tracer}, nil
}

// start opens the span of one read, "people.<name>".
func (l *Lookup) start(ctx context.Context, name string) (context.Context, telemetry.Span) {
	return l.tracer.Start(ctx, "people."+name)
}

// NamesOf returns the playa name of each known person; unknown or deleted IDs are omitted
// and an unfilled name is "". Internal level: it serves any actor, a signal handler and a
// job included. Fails errs.Invalid (ids, too_many {max}) for more than MaxNames IDs, and
// with the store's kinds.
func (l *Lookup) NamesOf(ctx context.Context, ids []model.PersonID) (_ map[model.PersonID]string, err error) {
	const op = "people.Lookup.NamesOf"
	ctx, span := l.start(ctx, "names-of")
	defer endSpan(span, &err)
	if len(ids) > MaxNames {
		return nil, paramInvalid(op, "ids", codeTooMany, map[string]string{"max": fmt.Sprint(MaxNames)},
			fmt.Sprintf("at most %d IDs", MaxNames))
	}
	unique := make([]model.PersonID, 0, len(ids))
	seen := make(map[model.PersonID]struct{}, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup || id.IsZero() {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	span.SetAttrs(telemetry.Int("ids", len(unique)))
	if len(unique) == 0 {
		return map[model.PersonID]string{}, nil
	}
	names, err := l.store.NamesOf(ctx, unique)
	if err != nil {
		return nil, wrap(op, err)
	}
	return names, nil
}

// ConsentOf reports whether person currently consents to purpose; false when unknown or
// deleted. Internal level, no permission: the module is deciding whether it may process.
// Fails errs.Invalid for a bad purpose or a zero person, and with the store's kinds.
func (l *Lookup) ConsentOf(ctx context.Context, person model.PersonID, purpose Purpose) (_ bool, err error) {
	const op = "people.Lookup.ConsentOf"
	ctx, span := l.start(ctx, "consent-of")
	defer endSpan(span, &err)
	if err := purpose.Validate(); err != nil {
		return false, wrap(op, err)
	}
	if person.IsZero() {
		return false, fieldInvalid(op, "person", codeRequired, "must be set")
	}
	granted, err := l.store.Consent(ctx, person, string(purpose))
	if err != nil {
		return false, wrap(op, err)
	}
	return granted, nil
}

// PlayaNameOf is identity.NameSource: the person's playa name ("" until filled in). Fails
// errs.NotFound when there is no row yet (identity shows ""), and with the store's kinds.
func (l *Lookup) PlayaNameOf(ctx context.Context, person model.PersonID) (_ string, err error) {
	const op = "people.Lookup.PlayaNameOf"
	ctx, span := l.start(ctx, "playa-name-of")
	defer endSpan(span, &err)
	name, err := l.store.PlayaNameOf(ctx, person)
	if err != nil {
		return "", wrap(op, err)
	}
	return name, nil
}

// LanguageOf is notify's language read: the person's saved language tag, "" when they have
// not chosen one (notify then uses the regional's default). A system read: no actor, no
// permission, no audit, never on a route. Fails errs.Invalid (person, required) for the zero
// ID, errs.NotFound when the person has no profile row, and the store's kinds.
func (l *Lookup) LanguageOf(ctx context.Context, person model.PersonID) (_ string, err error) {
	const op = "people.Lookup.LanguageOf"
	ctx, span := l.start(ctx, "language-of")
	defer endSpan(span, &err)
	if person.IsZero() {
		return "", fieldInvalid(op, "person", codeRequired, "must be set")
	}
	row, err := l.store.GetPerson(ctx, person)
	if err != nil {
		return "", wrap(op, err)
	}
	return row.Language, nil
}

// ContactEmailOf is notify's address read for a person without an account: the contact
// address a profile holds (personal level). A system read: no actor, no permission, no
// audit, never on a route; the caller is the notifier, which never logs the address. Fails
// errs.Invalid (person, required) for the zero ID, errs.NotFound when there is no profile row
// (unknown or deleted person) or no address, and the store's kinds.
func (l *Lookup) ContactEmailOf(ctx context.Context, person model.PersonID) (_ model.Email, err error) {
	const op = "people.Lookup.ContactEmailOf"
	ctx, span := l.start(ctx, "contact-email-of")
	defer endSpan(span, &err)
	if person.IsZero() {
		return "", fieldInvalid(op, "person", codeRequired, "must be set")
	}
	row, err := l.store.GetPerson(ctx, person)
	if err != nil {
		return "", wrap(op, err)
	}
	if row.ContactEmail == "" {
		return "", errs.E(op, errs.NotFound, errors.New("the person has no contact address"))
	}
	return model.Email(row.ContactEmail), nil
}
