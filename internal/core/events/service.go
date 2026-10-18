package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/zedgama3/bms/internal/contracts/eventsetup"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/events/postgres"
	"github.com/zedgama3/bms/internal/core/events/rows"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// store is the storage the service declares (D8); postgres.Store implements it. Every error
// is already classified by db.Wrap. A unique violation is Conflict and a check or foreign
// key violation Invalid, both with *db.Error.Constraint set. A single-row read or write of
// a missing row is NotFound.
type store interface {
	// ListEvents returns events newest first. closed nil: every event; true: closed
	// only; false: open only.
	ListEvents(ctx context.Context, closed *bool) ([]rows.Event, error)
	GetEvent(ctx context.Context, id model.EventID) (rows.Event, error)
	// GetEventForUpdate is GetEvent with a row lock, for a Write transaction. It also
	// serialises the schedule's count and positions.
	GetEventForUpdate(ctx context.Context, id model.EventID) (rows.Event, error)
	InsertEvent(ctx context.Context, e rows.Event) (rows.Event, error)
	UpdateEvent(ctx context.Context, p rows.Patch, now time.Time) (rows.Event, error)
	// CloseEvent and ReopenEvent report Conflict when the state is not the one needed.
	CloseEvent(ctx context.Context, id model.EventID, now time.Time) (rows.Event, error)
	ReopenEvent(ctx context.Context, id model.EventID, now time.Time) (rows.Event, error)
	InfoOf(ctx context.Context, ids []model.EventID) ([]rows.Info, error)

	ListKinds(ctx context.Context) ([]rows.Kind, error)
	GetKind(ctx context.Context, key string) (rows.Kind, error)
	InsertKind(ctx context.Context, key, name string, now time.Time) (rows.Kind, error)
	UpdateKind(ctx context.Context, key, name string, now time.Time) (rows.Kind, error)

	ListScheduleItems(ctx context.Context, event model.EventID) ([]rows.ScheduleItem, error)
	CountScheduleItems(ctx context.Context, event model.EventID) (int, error)
	GetScheduleItem(ctx context.Context, event model.EventID, id rows.ScheduleItemID) (rows.ScheduleItem, error)
	InsertScheduleItem(ctx context.Context, it rows.ScheduleItem, now time.Time) (rows.ScheduleItem, error)
	UpdateScheduleItem(ctx context.Context, it rows.ScheduleItem, now time.Time) (rows.ScheduleItem, error)
	DeleteScheduleItem(ctx context.Context, event model.EventID, id rows.ScheduleItemID) error
}

var _ store = (*postgres.Store)(nil)

// Directory is what modules get as Deps.Events. It needs no actor.
type Directory interface {
	// InfoOf maps each known ID to its Info; unknown IDs are absent. Fails errs.Invalid
	// (ids, too_many) and store kinds.
	InfoOf(ctx context.Context, ids []model.EventID) (map[model.EventID]Info, error)
}

// SetupProvider is a module's contribution to events.event-setup, named by app.
type SetupProvider struct {
	Module string
	eventsetup.Provider
}

// Options builds a Service. Audit and Signals are scoped "events" by app. SetupTimeout
// bounds one provider call (1s..5m; zero = DefaultSetupTimeout).
type Options struct {
	DB           db.DB
	Clock        clock.Clock
	Logger       *slog.Logger
	Tracer       telemetry.Tracer
	Meter        telemetry.Meter
	Audit        audit.Recorder
	Signals      signals.Bus
	Authz        authz.Authorizer
	Actor        authz.ActorFunc
	SetupTimeout time.Duration
}

// Validate fails errs.Invalid joining a validate.FieldError per problem: a nil dependency
// (fields events.db, events.clock, events.logger, events.tracer, events.meter,
// events.audit, events.signals, events.authz, events.actor) and a SetupTimeout outside
// 1s..5m other than zero (events.setup_timeout, range).
func (o Options) Validate() error {
	v := validate.Errors{Op: "events.Options.Validate"}
	for _, d := range []struct {
		field string
		nil   bool
	}{
		{"events.db", o.DB == nil},
		{"events.clock", o.Clock == nil},
		{"events.logger", o.Logger == nil},
		{"events.tracer", o.Tracer == nil},
		{"events.meter", o.Meter == nil},
		{"events.audit", o.Audit == nil},
		{"events.signals", o.Signals == nil},
		{"events.authz", o.Authz == nil},
		{"events.actor", o.Actor == nil},
	} {
		if d.nil {
			v.Addf(d.field, codeRequired, "must be set")
		}
	}
	if o.SetupTimeout != 0 {
		v.Add(validate.Range("events.setup_timeout", o.SetupTimeout, minSetupTimeout, maxSetupTimeout))
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Service is events. Safe for concurrent use; nothing to close (no goroutines of its own).
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
	timeout time.Duration
	setups  telemetry.Counter
	linked  atomic.Pointer[[]SetupProvider]
}

var _ Directory = (*Service)(nil)

// New validates o and builds the service over postgres.Store. No I/O: it works against
// db.None() and on a read-only copy. Fails errs.Invalid, and the meter's kind when the
// instrument cannot be created.
func New(o Options) (*Service, error) {
	const op = "events.New"
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
	const op = "events.New"
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if st == nil {
		return nil, errs.E(op, errs.Invalid, errors.New("nil store"))
	}
	setups, err := o.Meter.Counter("bms.events.setup", telemetry.WithUnit("{setup}"),
		telemetry.WithDescription("Event set-up calls by module and outcome."))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	timeout := o.SetupTimeout
	if timeout == 0 {
		timeout = DefaultSetupTimeout
	}
	return &Service{
		store: st, db: o.DB, clock: o.Clock, logger: o.Logger, tracer: o.Tracer, audit: o.Audit,
		signals: o.Signals, authz: o.Authz, actorFn: o.Actor, timeout: timeout, setups: setups,
	}, nil
}

// Link gives the service the providers, once, in build order, from app's link step. Fails
// errs.Internal on a second call, a nil provider, an empty module name or a repeated one.
// Providers are frozen after it.
func (s *Service) Link(providers []SetupProvider) error {
	const op = "events.Service.Link"
	seen := make(map[string]struct{}, len(providers))
	for _, p := range providers {
		if p.Module == "" || p.Provider == nil {
			return errs.E(op, errs.Internal, errors.New("a set-up provider needs a module name and a provider"))
		}
		if _, dup := seen[p.Module]; dup {
			return errs.E(op, errs.Internal, errors.New("two set-up providers share a module name"))
		}
		seen[p.Module] = struct{}{}
	}
	frozen := append([]SetupProvider(nil), providers...)
	if !s.linked.CompareAndSwap(nil, &frozen) {
		return errs.E(op, errs.Internal, errors.New("Link was already called"))
	}
	return nil
}

// providers returns what Link handed over; none before it.
func (s *Service) providers() []SetupProvider {
	if p := s.linked.Load(); p != nil {
		return *p
	}
	return nil
}

// now is the clock's time in UTC.
func (s *Service) now() time.Time { return s.clock.Now().UTC() }

// start opens the span of one use case, "events.<name>".
func (s *Service) start(ctx context.Context, name string) (context.Context, telemetry.Span) {
	return s.tracer.Start(ctx, "events."+name)
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
	return wrap("events.Service.record", s.audit.Record(ctx, entries...))
}

// publish publishes one signal naming the event (and the event it was copied from, when
// set), in the caller's transaction.
func (s *Service) publish(ctx context.Context, name signals.Name, event, copiedFrom model.EventID) error {
	entries := []signals.Entry{signals.Ref(PayloadEvent, event)}
	if !copiedFrom.IsZero() {
		entries = append(entries, signals.Ref(PayloadCopiedFrom, copiedFrom))
	}
	return wrap("events.Service.publish", s.signals.Publish(ctx, name, signals.NewPayload(entries...)))
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

// require fails errs.Permission (with the authorizer's error) unless the actor holds
// events.manage.
func (s *Service) require(ctx context.Context, op string) error {
	if err := s.authz.Allow(ctx, PermissionManage); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// manage is the start of every write: the actor, then the permission.
func (s *Service) manage(ctx context.Context, op string) error {
	if _, err := s.actor(ctx, op); err != nil {
		return err
	}
	return s.require(ctx, op)
}

// notFound is the NotFound for an unknown event, kind or schedule item.
func notFound(op, what string) error {
	return errs.E(op, errs.NotFound, errors.New("no such "+what))
}

// constraintOf returns the constraint name a database error carries, or "".
func constraintOf(err error) string {
	var pe *db.Error
	if errors.As(err, &pe) {
		return pe.Constraint
	}
	return ""
}

// The constraints the database reports (migration 0001).
const (
	conEventName    = "event_name_key"
	conEventKind    = "event_kind_fkey"
	conEventDates   = "event_dates_chk"
	conKindKey      = "event_kind_pkey"
	conScheduleKey  = "event_schedule_key"
	conScheduleKeyC = "event_schedule_item_key_chk"
	conScheduleEnds = "event_schedule_item_ends_chk"
)

// fromConstraint turns a database constraint error into the field error the spec names:
// name taken, kind unknown, dates.end range, key taken, key duplicate, key malformed,
// ends_at range. A Conflict from another constraint becomes conflict[0] (field), conflict[1]
// (code) when given. Any other error keeps its own kind, wrapped with op.
func fromConstraint(op string, err error, conflict ...string) error {
	switch constraintOf(err) {
	case conEventName:
		return fieldConflict(op, fieldName, codeTaken, "another event has this name")
	case conEventKind:
		return fieldInvalid(op, fieldKind, codeUnknown, "no such kind")
	case conEventDates:
		return fieldInvalid(op, fieldDates+".end", codeRange, "must be after the start")
	case conKindKey:
		return fieldConflict(op, fieldKey, codeTaken, "another kind has this key")
	case conScheduleKey:
		return fieldConflict(op, fieldKey, codeDuplicate, "another item of this event has this key")
	case conScheduleKeyC:
		return fieldInvalid(op, fieldKey, codeMalformed, "must be lower-case letters, digits and single hyphens")
	case conScheduleEnds:
		return fieldInvalid(op, fieldEnds, codeRange, "must be after starts_at")
	}
	if len(conflict) == 2 && errs.Is(err, errs.Conflict) {
		return fieldConflict(op, conflict[0], conflict[1], "already in use")
	}
	return wrap(op, err)
}
