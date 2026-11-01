package authz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz/postgres"
	"github.com/zedgama3/bms/internal/core/authz/rows"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// lookupStore is the part of the storage the Lookup reads; postgres.Store implements it.
// Every error is already classified by db.Wrap.
type lookupStore interface {
	EffectivePermissions(ctx context.Context, person model.PersonID, now time.Time) ([]rows.Held, error)
	EffectiveFor(ctx context.Context, person model.PersonID, now time.Time, permission string) ([]rows.Held, error)
	RulesHeldBy(ctx context.Context, person model.PersonID, now time.Time) ([]rows.HeldRule, error)
	TitlesOf(ctx context.Context, person model.PersonID, now time.Time, limit int) ([]string, error)
	PermissionHolders(ctx context.Context, permission string, now time.Time) ([]model.PersonID, error)
	ListRoles(ctx context.Context, scope string) ([]rows.Role, error)
}

var _ lookupStore = (*postgres.Store)(nil)

// LookupOptions builds the read side: decisions and lookups over the roles and grants,
// nothing audited, nothing written. Meter is optional (nil records no metrics).
type LookupOptions struct {
	DB          db.DB
	Clock       clock.Clock
	Logger      *slog.Logger
	Tracer      telemetry.Tracer
	Meter       telemetry.Meter  // optional: nil records no metrics
	Actor       ActorFunc        // identity.Actor; required
	Declared    []PermissionDecl // the core's and the enabled modules'
	Departments DepartmentSource // the departments.Lookup; required
}

// Validate fails errs.Invalid joining a validate.FieldError per problem: "authz.db",
// "authz.clock", "authz.logger", "authz.tracer", "authz.actor", "authz.departments" (nil),
// "authz.declared[i].name|scope|reaches|starter_roles[j]" (a declaration that fails
// PermissionDecl.Validate) and "authz.declared" (two declarations with one name, code
// duplicate).
func (o LookupOptions) Validate() error {
	v := validate.Errors{Op: "authz.LookupOptions.Validate"}
	if o.DB == nil {
		v.Addf("authz.db", codeRequired, "must be set")
	}
	if o.Clock == nil {
		v.Addf("authz.clock", codeRequired, "must be set")
	}
	if o.Logger == nil {
		v.Addf("authz.logger", codeRequired, "must be set")
	}
	if o.Tracer == nil {
		v.Addf("authz.tracer", codeRequired, "must be set")
	}
	if o.Actor == nil {
		v.Addf("authz.actor", codeRequired, "must be set")
	}
	if o.Departments == nil {
		v.Addf("authz.departments", codeRequired, "must be set")
	}
	validateDeclared(&v, o.Declared)
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Lookup is the read side of authz: Authorizer (Allow, AllowIn, ExtentOf),
// identity.PermissionLister (PermissionsOf), audit.TitleSource (TitlesOf), HoldersOf and
// StarterRoleIDs, with authz.md's failure modes unchanged. Nothing it does is audited and
// nothing it does writes, so it is built before audit. Safe for concurrent use; nothing to
// close (no goroutines).
type Lookup struct {
	store   lookupStore
	db      db.DB
	clock   clock.Clock
	logger  *slog.Logger
	tracer  telemetry.Tracer
	actorFn ActorFunc
	depts   DepartmentSource
	cat     catalogue
	metrics metrics
}

var (
	_ Authorizer        = (*Lookup)(nil)
	_ audit.TitleSource = (*Lookup)(nil)
)

// NewLookup validates o and returns the Lookup over postgres.Store. No I/O: it works
// against db.None() and on a read-only copy. Fails errs.Invalid (a nil dependency, a bad
// or duplicate declaration) and the meter's kind when an instrument cannot be created.
func NewLookup(o LookupOptions) (*Lookup, error) {
	const op = "authz.NewLookup"
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
	const op = "authz.NewLookup"
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if st == nil {
		return nil, errs.E(op, errs.Invalid, errors.New("nil store"))
	}
	m, err := newMetrics(o.Meter)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return &Lookup{
		store: st, db: o.DB, clock: o.Clock, logger: o.Logger, tracer: o.Tracer,
		actorFn: o.Actor, depts: o.Departments, cat: newCatalogue(o.Declared), metrics: m,
	}, nil
}

// now is the clock's time in UTC.
func (l *Lookup) now() time.Time { return l.clock.Now().UTC() }

// start opens the span of one use case, "authz.<name>".
func (l *Lookup) start(ctx context.Context, name string) (context.Context, telemetry.Span) {
	return l.tracer.Start(ctx, "authz."+name)
}

// actor returns the person making the request: errs.Unauthenticated inside a signal
// delivery (decision 7), without a session or with a restricted one.
func (l *Lookup) actor(ctx context.Context, op string) (Actor, error) {
	return resolveActor(ctx, l.actorFn, op)
}

// resolveActor is the actor rule both halves share.
func resolveActor(ctx context.Context, fn ActorFunc, op string) (Actor, error) {
	if signals.InDelivery(ctx) {
		return Actor{}, errs.E(op, errs.Unauthenticated, errors.New("a signal handler acts as the system, not as a person"))
	}
	a, ok := fn(ctx)
	if !ok || (a.Person.IsZero() && !a.Superuser) {
		return Actor{}, errs.E(op, errs.Unauthenticated, errors.New("no signed-in person"))
	}
	return a, nil
}

// inTx runs fn in a transaction of the given mode. fn's errors carry their own context.
func (l *Lookup) inTx(ctx context.Context, mode db.Mode, fn func(ctx context.Context) error) error {
	return l.db.Transact(ctx, mode, fn) //nolint:wrapcheck // fn's errors are wrapped where they arise
}
