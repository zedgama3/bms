package datarights

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/datarights/postgres"
	"github.com/zedgama3/bms/internal/core/datarights/rows"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// store is the storage the service declares (D8); postgres.Store implements it. Every
// error is already classified by db.Wrap. A unique violation is Conflict, a single-row read
// of a missing row is NotFound.
type store interface {
	// LockDeletion takes the advisory lock two deletions of one person serialise on (people
	// takes the same one in UpdateMe and LinkAccount). Write transaction only.
	LockDeletion(ctx context.Context, id model.PersonID) error

	// Deletion requests. Withdraw and Decline report decided=false when the request is
	// not open (or not the person's); the service then reads GetRequest to tell NotFound
	// from Conflict.
	InsertRequest(ctx context.Context, id RequestID, person model.PersonID, note string, at time.Time) (rows.Request, error)
	GetRequest(ctx context.Context, id RequestID) (rows.Request, error)
	RequestForUpdate(ctx context.Context, id RequestID) (rows.Request, error)
	MyRequests(ctx context.Context, person model.PersonID, limit int) ([]rows.Request, error)
	ListRequests(ctx context.Context, status string, person model.PersonID, limit int) ([]rows.Request, error)
	WithdrawRequest(ctx context.Context, id RequestID, person model.PersonID, at time.Time) (row rows.Request, decided bool, err error)
	DeclineRequest(ctx context.Context, id RequestID, by model.PersonID, note string, at time.Time) (row rows.Request, decided bool, err error)
	DeleteRequestsOf(ctx context.Context, person model.PersonID) (int, error)
	ExportRequests(ctx context.Context, id model.PersonID) ([]rows.Request, error)
}

var _ store = (*postgres.Store)(nil)

// Options builds the service. Audit and Signals are scoped to "datarights" by app, and
// Notify too (the template's owner is "datarights").
type Options struct {
	DB      db.DB
	Clock   clock.Clock
	Logger  *slog.Logger
	Tracer  telemetry.Tracer
	Meter   telemetry.Meter  // optional: nil records no metrics
	Audit   audit.Recorder   // scoped "datarights"
	Signals signals.Bus      // scoped "datarights": publishes datarights.deletion-requested
	Authz   authz.Authorizer // the Lookup
	Actor   authz.ActorFunc  // identity.Actor
	// Holders says who is told of a new request (people.data.delete); Notify sends the
	// notice (scoped "datarights"). When e-mail is not configured the notice is skipped,
	// not failed.
	Holders HolderSource
	Notify  notify.Notifier
	// AuditLog counts the audit rows deletion leaves in place.
	AuditLog AuditLog
	// The core sources, each a service in its own shape.
	Accounts AccountSource // identity
	Access   RawSource     // authz.Service
	Outbox   RawSource     // notify
	Files    FilesSource
	People   PeopleSource
}

// Validate fails errs.Invalid joining a validate.FieldError per nil dependency (Meter alone is
// optional)
// (fields datarights.db, datarights.clock, datarights.logger, datarights.tracer,
// datarights.audit, datarights.signals, datarights.authz, datarights.actor,
// datarights.holders, datarights.notify, datarights.audit_log, datarights.accounts, datarights.access, datarights.outbox,
// datarights.files, datarights.people).
func (o Options) Validate() error {
	v := validate.Errors{Op: "datarights.Options.Validate"}
	for _, d := range []struct {
		field string
		nil   bool
	}{
		{"datarights.db", o.DB == nil},
		{"datarights.clock", o.Clock == nil},
		{"datarights.logger", o.Logger == nil},
		{"datarights.tracer", o.Tracer == nil},
		{"datarights.audit", o.Audit == nil},
		{"datarights.signals", o.Signals == nil},
		{"datarights.authz", o.Authz == nil},
		{"datarights.actor", o.Actor == nil},
		{"datarights.holders", o.Holders == nil},
		{"datarights.notify", o.Notify == nil},
		{"datarights.audit_log", o.AuditLog == nil},
		{"datarights.accounts", o.Accounts == nil},
		{"datarights.access", o.Access == nil},
		{"datarights.outbox", o.Outbox == nil},
		{"datarights.files", o.Files == nil},
		{"datarights.people", o.People == nil},
	} {
		if d.nil {
			v.Addf(d.field, codeRequired, "must be set")
		}
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// part is one source of a person's data in the export and the deletion, under its name.
type part struct {
	name   string
	export func(ctx context.Context, person model.PersonID) (json.RawMessage, error)
	del    func(ctx context.Context, person model.PersonID) (Deletion, error)
}

// linkState is what Link hands the service, frozen: the modules, then files, then notify,
// in the order their DeletePerson runs.
type linkState struct {
	parts []part
}

// Service is datarights. Safe for concurrent use; nothing to close (no goroutines of its
// own).
type Service struct {
	store     store
	db        db.DB
	clock     clock.Clock
	logger    *slog.Logger
	tracer    telemetry.Tracer
	audit     audit.Recorder
	signals   signals.Bus
	authz     authz.Authorizer
	actorFn   authz.ActorFunc
	holders   HolderSource
	notify    notify.Notifier
	auditLog  AuditLog
	accounts  AccountSource
	access    RawSource
	outbox    RawSource
	files     FilesSource
	people    PeopleSource
	metrics   metrics
	linked    atomic.Pointer[linkState]
	cooldown  *cooldown
	filesPart part
	boxPart   part
}

// New validates o and builds the service over postgres.Store. No I/O: it works against
// db.None() (app decision 7) and on a read-only copy. Fails errs.Invalid (a nil
// dependency) and the meter's kind when an instrument cannot be created.
func New(o Options) (*Service, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	st, err := postgres.NewStore(o.DB)
	if err != nil {
		return nil, fmt.Errorf("datarights.New: %w", err)
	}
	return newService(o, st)
}

// newService builds the service over st. Tests hand it a fake store.
func newService(o Options, st store) (*Service, error) {
	const op = "datarights.New"
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
	s := &Service{
		store: st, db: o.DB, clock: o.Clock, logger: o.Logger, tracer: o.Tracer, audit: o.Audit, signals: o.Signals,
		authz: o.Authz, actorFn: o.Actor, holders: o.Holders, notify: o.Notify, auditLog: o.AuditLog,
		accounts: o.Accounts, access: o.Access, outbox: o.Outbox, files: o.Files, people: o.People,
		metrics: m, cooldown: newCooldown(),
	}
	s.filesPart = part{
		name:   "files",
		export: o.Files.ExportPerson,
		del: func(ctx context.Context, person model.PersonID) (Deletion, error) {
			d, err := o.Files.DeletePerson(ctx, person)
			return Deletion{Deleted: d.Deleted, Anonymized: d.Anonymized}, err //nolint:wrapcheck // deleteInTx names the source
		},
	}
	s.boxPart = part{
		name:   "notify",
		export: o.Outbox.ExportPerson,
		del: func(ctx context.Context, person model.PersonID) (Deletion, error) {
			n, err := o.Outbox.DeletePerson(ctx, person)
			return Deletion{Deleted: n}, err //nolint:wrapcheck // deleteInTx names the source
		},
	}
	return s, nil
}

// now is the clock's time in UTC.
func (s *Service) now() time.Time { return s.clock.Now().UTC() }

// start opens the span of one use case, "datarights.<name>".
func (s *Service) start(ctx context.Context, name string) (context.Context, telemetry.Span) {
	return s.tracer.Start(ctx, Area+"."+name)
}

// endSpan records *err on span (when set) and ends it. Use with defer and a named result.
func endSpan(span telemetry.Span, err *error) {
	if *err != nil {
		span.RecordError(*err)
	}
	span.End()
}

// record writes entries through the audit recorder; the error keeps audit's kind.
func (s *Service) record(ctx context.Context, entries ...audit.Entry) error {
	return wrap("datarights.Service.record", s.audit.Record(ctx, entries...))
}

// wrap adds op to err and keeps its kind and its chain (errors.Is still finds
// db.ErrReadOnly and httpx.ErrRateLimited). nil stays nil.
func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}

// inTx runs fn in a transaction of the given mode. fn's errors carry their own context, so
// the transaction's error is returned as it is.
func (s *Service) inTx(ctx context.Context, mode db.Mode, fn func(ctx context.Context) error) error {
	return s.db.Transact(ctx, mode, fn) //nolint:wrapcheck // fn's errors are wrapped where they arise
}

// publish publishes one signal carrying the entries in the caller's transaction.
func (s *Service) publish(ctx context.Context, name signals.Name, entries ...signals.Entry) error {
	return wrap("datarights.Service.publish", s.signals.Publish(ctx, name, signals.NewPayload(entries...)))
}

func personRef(id model.PersonID) signals.Entry { return signals.Ref(PayloadPerson, id) }
func requestRef(id RequestID) signals.Entry     { return signals.Ref(PayloadRequest, id) }

// actor returns the person making the request: errs.Unauthenticated inside a signal
// delivery, without a session or with a restricted one (F3).
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

// actorPerson is actor with the requirement that a person is signed in (a session always
// carries one).
func (s *Service) actorPerson(ctx context.Context, op string) (authz.Actor, error) {
	a, err := s.actor(ctx, op)
	if err != nil {
		return authz.Actor{}, err
	}
	if a.Person.IsZero() {
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

// constraintOf returns the constraint name a database error carries, or "".
func constraintOf(err error) string {
	var pe *db.Error
	if errors.As(err, &pe) {
		return pe.Constraint
	}
	return ""
}

// ---- metrics ----------------------------------------------------------------------------

// metrics holds the instruments; each method is a no-op without a Meter. They were
// bms.people.exports and bms.people.deletions before the move (boundaries S12 (e)).
type metrics struct {
	exports   telemetry.Counter
	deletions telemetry.Counter
}

func newMetrics(m telemetry.Meter) (metrics, error) {
	if m == nil {
		return metrics{}, nil
	}
	var (
		out metrics
		err error
	)
	if out.exports, err = m.Counter("bms.datarights.exports", telemetry.WithUnit("{export}"),
		telemetry.WithDescription("Per-person exports by outcome.")); err != nil {
		return metrics{}, fmt.Errorf("datarights.New: %w", err)
	}
	if out.deletions, err = m.Counter("bms.datarights.deletions", telemetry.WithUnit("{deletion}"),
		telemetry.WithDescription("Per-person deletions by outcome.")); err != nil {
		return metrics{}, fmt.Errorf("datarights.New: %w", err)
	}
	return out, nil
}

// The outcomes the counters take.
const (
	outcomeOK      = "ok"
	outcomeFailed  = "failed"
	outcomeLimited = "limited"
)

func (m metrics) export(ctx context.Context, outcome string) {
	if m.exports != nil {
		m.exports.Add(ctx, 1, telemetry.Token("outcome", outcome))
	}
}

func (m metrics) deletion(ctx context.Context, outcome string) {
	if m.deletions != nil {
		m.deletions.Add(ctx, 1, telemetry.Token("outcome", outcome))
	}
}

// ---- the export cooldown ----------------------------------------------------------------

// cooldown remembers when each person last exported their own data, bounded by
// MaxTrackedExports (people.md decision 10 and "Cooldown"). Per process, in memory.
type cooldown struct {
	mu   sync.Mutex
	last map[model.PersonID]time.Time
}

func newCooldown() *cooldown { return &cooldown{last: make(map[model.PersonID]time.Time)} }

// take reserves an export for person at now. It reports false while the last one is
// younger than ExportCooldown. At the bound it first drops expired entries, then the oldest.
func (c *cooldown) take(person model.PersonID, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if at, ok := c.last[person]; ok && now.Sub(at) < ExportCooldown {
		return false
	}
	if _, ok := c.last[person]; !ok && len(c.last) >= MaxTrackedExports {
		c.evict(now)
	}
	c.last[person] = now
	return true
}

// release forgets a reservation whose export failed, so the person may retry at once.
func (c *cooldown) release(person model.PersonID, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last[person].Equal(at) {
		delete(c.last, person)
	}
}

// evict makes room for one entry. The caller holds the lock.
func (c *cooldown) evict(now time.Time) {
	var (
		oldest   model.PersonID
		oldestAt time.Time
		first    = true
	)
	for p, at := range c.last {
		if now.Sub(at) >= ExportCooldown {
			delete(c.last, p)
			continue
		}
		if first || at.Before(oldestAt) {
			oldest, oldestAt, first = p, at, false
		}
	}
	if len(c.last) >= MaxTrackedExports && !first {
		delete(c.last, oldest)
	}
}

// ---- Link -------------------------------------------------------------------------------

// Link gives the service the modules' sources, in build order, once, from app's link step
// (after module Build, before api.New). The deletion order is: the modules in the order
// given, then files, then notify, then access, identity and people (foreign keys point at
// core.person with RESTRICT; people.md decision 11). Fails errs.Conflict on a second call
// and errs.Invalid for a source without a name or a PersonData, or a name used twice (the
// names "files" and "notify" are taken). Before Link, Export and Delete fail errs.Internal.
func (s *Service) Link(modules []Source) error {
	const op = "datarights.Service.Link"
	seen := map[string]bool{s.filesPart.name: true, s.boxPart.name: true}
	parts := make([]part, 0, len(modules)+2)
	for _, m := range modules {
		switch {
		case m.Name == "" || m.PersonData == nil:
			return errs.E(op, errs.Invalid, errors.New("a source needs a name and a PersonData"))
		case seen[m.Name]:
			return errs.E(op, errs.Invalid, fmt.Errorf("the source name %q is used twice", m.Name))
		}
		seen[m.Name] = true
		parts = append(parts, modulePart(m))
	}
	parts = append(parts, s.filesPart, s.boxPart)
	if !s.linked.CompareAndSwap(nil, &linkState{parts: parts}) {
		return errs.E(op, errs.Conflict, errors.New("Link was already called"))
	}
	return nil
}

// modulePart adapts a module's PersonData to the service's two operations.
func modulePart(m Source) part {
	return part{
		name: m.Name,
		export: func(ctx context.Context, person model.PersonID) (json.RawMessage, error) {
			e, err := m.ExportPerson(ctx, person)
			return e.Data, err
		},
		del: m.DeletePerson,
	}
}

// requireLinked fails errs.Internal before Link (F31).
func (s *Service) requireLinked(op string) (*linkState, error) {
	st := s.linked.Load()
	if st == nil {
		return nil, errs.E(op, errs.Internal, errors.New("Link has not been called"))
	}
	return st, nil
}
