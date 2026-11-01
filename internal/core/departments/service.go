package departments

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/departments/postgres"
	"github.com/zedgama3/bms/internal/core/departments/rows"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// store is the storage the service declares (D8); postgres.Store implements it. Every
// error is already classified by db.Wrap. A unique violation is Conflict, and a single-row
// read or write of a missing row is NotFound.
type store interface {
	// List returns the departments by lower(name); archived ones only when asked.
	List(ctx context.Context, includeArchived bool) ([]rows.Department, error)
	Get(ctx context.Context, id model.DepartmentID) (rows.Department, error)
	// GetForUpdate is Get with a row lock, for a Write transaction.
	GetForUpdate(ctx context.Context, id model.DepartmentID) (rows.Department, error)
	// Insert inserts d. A name equal to another's ignoring case is Conflict.
	Insert(ctx context.Context, d rows.Department) (rows.Department, error)
	// Update applies the patch (nil fields unchanged) and sets updated_at to now.
	Update(ctx context.Context, p rows.Patch, now time.Time) (rows.Department, error)
	// Archive sets archived_at and updated_at to now; Restore clears archived_at.
	Archive(ctx context.Context, id model.DepartmentID, now time.Time) (rows.Department, error)
	Restore(ctx context.Context, id model.DepartmentID, now time.Time) (rows.Department, error)
	// NamesOf maps each known ID, archived included, to its name.
	NamesOf(ctx context.Context, ids []model.DepartmentID) (map[model.DepartmentID]string, error)
	// Active reports whether the department exists and is not archived.
	Active(ctx context.Context, id model.DepartmentID) (bool, error)
}

var _ store = (*postgres.Store)(nil)

// Directory is what modules get as Deps.Departments. Neither method needs an actor: both
// return internal data and are called from signal handlers and jobs.
type Directory interface {
	// NamesOf maps each known ID (archived included) to its name; unknown IDs are absent.
	// Fails errs.Invalid (ids, too_many) and store kinds.
	NamesOf(ctx context.Context, ids []model.DepartmentID) (map[model.DepartmentID]string, error)
	// Active reports whether the department exists and is not archived: (false, nil) for
	// unknown or archived. Also authz.DepartmentSource. Fails errs.Invalid (zero id) and
	// store kinds.
	Active(ctx context.Context, id model.DepartmentID) (bool, error)
}

// Options builds a Service. Audit and Signals are scoped "departments" by app.
type Options struct {
	DB      db.DB
	Clock   clock.Clock
	Logger  *slog.Logger
	Tracer  telemetry.Tracer
	Meter   telemetry.Meter
	Audit   audit.Recorder
	Signals signals.Bus
	Authz   authz.Authorizer
	Actor   authz.ActorFunc
	Lookup  *Lookup // NamesOf and Active; built before authz; required
}

// Validate fails errs.Invalid joining a validate.FieldError per nil dependency (fields
// departments.db, departments.clock, departments.logger, departments.tracer,
// departments.meter, departments.audit, departments.signals, departments.authz,
// departments.actor, departments.lookup).
func (o Options) Validate() error {
	v := validate.Errors{Op: "departments.Options.Validate"}
	for _, d := range []struct {
		field string
		nil   bool
	}{
		{"departments.db", o.DB == nil},
		{"departments.clock", o.Clock == nil},
		{"departments.logger", o.Logger == nil},
		{"departments.tracer", o.Tracer == nil},
		{"departments.meter", o.Meter == nil},
		{"departments.audit", o.Audit == nil},
		{"departments.signals", o.Signals == nil},
		{"departments.authz", o.Authz == nil},
		{"departments.actor", o.Actor == nil},
		{"departments.lookup", o.Lookup == nil},
	} {
		if d.nil {
			v.Addf(d.field, codeRequired, "must be set")
		}
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Service is departments. Safe for concurrent use; nothing to close (no goroutines).
type Service struct {
	store   store
	db      db.DB
	clock   clock.Clock
	logger  *slog.Logger
	tracer  telemetry.Tracer
	audit   audit.Recorder
	signals signals.Bus
	authz   authz.Authorizer
	actorFn authz.ActorFunc
	lookup  *Lookup
}

var (
	_ Directory              = (*Service)(nil)
	_ authz.DepartmentSource = (*Service)(nil)
)

// New validates o and builds the service over postgres.Store. No I/O: it works against
// db.None() and on a read-only copy. Fails errs.Invalid.
func New(o Options) (*Service, error) {
	const op = "departments.New"
	if err := o.Validate(); err != nil {
		return nil, err
	}
	st, err := postgres.NewStore(o.DB)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return newService(o, st)
}

// newService builds the service over st. Tests hand it a fake store.
func newService(o Options, st store) (*Service, error) {
	const op = "departments.New"
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if st == nil {
		return nil, errs.E(op, errs.Invalid, errors.New("nil store"))
	}
	return &Service{
		store: st, db: o.DB, clock: o.Clock, logger: o.Logger, tracer: o.Tracer, audit: o.Audit,
		signals: o.Signals, authz: o.Authz, actorFn: o.Actor, lookup: o.Lookup,
	}, nil
}

// NamesOf is Lookup.NamesOf.
func (s *Service) NamesOf(ctx context.Context, ids []model.DepartmentID) (map[model.DepartmentID]string, error) {
	return s.lookup.NamesOf(ctx, ids)
}

// Active is Lookup.Active.
func (s *Service) Active(ctx context.Context, id model.DepartmentID) (bool, error) {
	return s.lookup.Active(ctx, id)
}

// now is the clock's time in UTC.
func (s *Service) now() time.Time { return s.clock.Now().UTC() }

// start opens the span of one use case, "departments.<name>".
func (s *Service) start(ctx context.Context, name string) (context.Context, telemetry.Span) {
	return s.tracer.Start(ctx, "departments."+name)
}

// endSpan records *err on span (when set) and ends it. Use with defer and a named result.
func endSpan(span telemetry.Span, err *error) {
	if *err != nil {
		span.RecordError(*err)
	}
	span.End()
}

// wrap adds op to err and keeps its kind and its chain. nil stays nil.
func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}

// record writes entries through the audit recorder; the error keeps audit's kind.
func (s *Service) record(ctx context.Context, entries ...audit.Entry) error {
	return wrap("departments.Service.record", s.audit.Record(ctx, entries...))
}

// publish publishes one signal naming the department, in the caller's transaction.
func (s *Service) publish(ctx context.Context, name signals.Name, id model.DepartmentID) error {
	return wrap("departments.Service.publish",
		s.signals.Publish(ctx, name, signals.NewPayload(signals.Ref(PayloadDepartment, id))))
}

// inTx runs fn in a transaction of the given mode. fn's errors carry their own context.
func (s *Service) inTx(ctx context.Context, mode db.Mode, fn func(ctx context.Context) error) error {
	return s.db.Transact(ctx, mode, fn) //nolint:wrapcheck // fn's errors are wrapped where they arise
}

// actor returns the person making the request: errs.Unauthenticated inside a signal
// delivery, without a session or with a restricted one.
func (s *Service) actor(ctx context.Context, op string) (authz.Actor, error) {
	if signals.InDelivery(ctx) {
		return authz.Actor{}, errs.E(op, errs.Unauthenticated, errors.New("a signal handler acts as the system, not as a person"))
	}
	a, ok := s.actorFn(ctx)
	if !ok || (a.Person.IsZero() && !a.Superuser) {
		return authz.Actor{}, errs.E(op, errs.Unauthenticated, errors.New("no signed-in person"))
	}
	return a, nil
}

// require fails errs.Permission (with the authorizer's error) unless the actor holds the
// site-wide permission p.
func (s *Service) require(ctx context.Context, op string, p authz.Permission) error {
	if err := s.authz.Allow(ctx, p); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// requireIn fails errs.Permission unless the actor holds p site-wide or at d.
func (s *Service) requireIn(ctx context.Context, op string, p authz.Permission, d model.DepartmentID) error {
	if err := s.authz.AllowIn(ctx, p, d); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// notFound is the NotFound every use case answers for a department the caller may not
// know about.
func notFound(op string) error {
	return errs.E(op, errs.NotFound, errors.New("no such department"))
}
