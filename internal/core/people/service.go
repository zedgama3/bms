package people

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zedgama3/bms/internal/contracts/personpage"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people/postgres"
	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// store is the storage the service declares (D8); postgres.Store implements it. Every
// error is already classified by db.Wrap. A unique violation is Conflict and a foreign key
// violation Invalid, both with *db.Error.Constraint set. A single-row read of a missing row
// is NotFound.
type store interface {
	// Person.
	GetPerson(ctx context.Context, id model.PersonID) (rows.Person, error)
	GetForUpdate(ctx context.Context, id model.PersonID) (rows.Person, error)
	// InsertPerson inserts p; inserted is false (and nothing written) when the ID exists.
	InsertPerson(ctx context.Context, p rows.Person, now time.Time) (row rows.Person, inserted bool, err error)
	// Upsert applies the patch to the row or creates it; inserted says which.
	Upsert(ctx context.Context, p rows.PersonPatch, now time.Time) (row rows.Person, inserted bool, err error)
	UpdatePerson(ctx context.Context, p rows.PersonPatch, now time.Time) (rows.Person, error)
	PlayaNameOf(ctx context.Context, id model.PersonID) (string, error)
	NamesOf(ctx context.Context, ids []model.PersonID) (map[model.PersonID]string, error)
	Search(ctx context.Context, q rows.SearchQuery) ([]rows.Match, error)
	LockDeletion(ctx context.Context, id model.PersonID) error
	// DeleteOrphan deletes the person only when the row is empty and unreferenced; false
	// means it was kept.
	DeleteOrphan(ctx context.Context, id model.PersonID) (bool, error)
	DeletePerson(ctx context.Context, id model.PersonID) (int, error)

	// Emergency contacts.
	LockPerson(ctx context.Context, id model.PersonID) error
	Contacts(ctx context.Context, person model.PersonID) ([]rows.Contact, error)
	GetContact(ctx context.Context, person model.PersonID, id ContactID) (rows.Contact, error)
	// AddContact appends c; added is false when the person already has limit contacts.
	AddContact(ctx context.Context, c rows.Contact, limit int) (row rows.Contact, added bool, err error)
	UpdateContact(ctx context.Context, c rows.Contact) (rows.Contact, error)
	DeleteContact(ctx context.Context, person model.PersonID, id ContactID) error
	Renumber(ctx context.Context, person model.PersonID) error
	DeleteContactsOf(ctx context.Context, person model.PersonID) (int, error)

	// Consents.
	Consent(ctx context.Context, person model.PersonID, purpose string) (bool, error)
	Consents(ctx context.Context, person model.PersonID) ([]rows.Consent, error)
	SetConsent(ctx context.Context, c rows.Consent) (rows.Consent, error)
	DeleteConsentsOf(ctx context.Context, person model.PersonID) (int, error)

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

	// The export document's parts.
	ExportPerson(ctx context.Context, id model.PersonID) (rows.Person, error)
	ExportContacts(ctx context.Context, id model.PersonID) ([]rows.Contact, error)
	ExportConsents(ctx context.Context, id model.PersonID) ([]rows.Consent, error)
	ExportRequests(ctx context.Context, id model.PersonID) ([]rows.Request, error)
}

var _ store = (*postgres.Store)(nil)

// PolicySource is settings' seam (M2). nil means DefaultPolicy.
type PolicySource interface {
	Policy(ctx context.Context) (Policy, error)
}

// Accounts is identity's seam. EmailOf fails NotFound when the person has no account.
// LinkAccount re-points the account with that address to person and returns the account ID
// and the person ID it pointed at before (the orphan sign-up minted); it fails NotFound
// (field "email", code "unknown") when no account has the address, Conflict "superuser" for
// a superuser account, Conflict "has_account" when person already has one. Neither checks a
// permission: people has authorized and audits the use case.
type Accounts interface {
	EmailOf(ctx context.Context, person model.PersonID) (model.Email, error)
	LinkAccount(ctx context.Context, email model.Email, person model.PersonID) (model.AccountID, model.PersonID, error)
	ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
	DeletePerson(ctx context.Context, person model.PersonID) (int, error)
}

// Access is authz's seam (its ExportPerson and DeletePerson).
type Access interface {
	ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
	DeletePerson(ctx context.Context, person model.PersonID) (int, error)
}

// AuditCounter is audit.Log's CountAbout, for the deletion report.
type AuditCounter interface {
	CountAbout(ctx context.Context, person model.PersonID) (int, error)
}

// PersonData is what app adapts each module.ModulePersonData to.
type PersonData interface {
	ExportPerson(ctx context.Context, person model.PersonID) (Export, error)
	DeletePerson(ctx context.Context, person model.PersonID) (ModuleDeletion, error)
}

// Source is one module's PersonData with its name, in build order.
type Source struct {
	Name     string
	Exporter PersonData
}

// SectionProvider is one module's contribution to people.person-page.
type SectionProvider struct {
	Module   string
	Provider personpage.Provider
}

// Directory is what every module receives as Deps.People.
type Directory interface {
	// NamesOf returns the playa name of each known person (missing or deleted IDs are
	// omitted; an unfilled name is ""). Internal level: any actor, including a signal
	// handler or a job running as the system. Fails Invalid when len(ids) > MaxNames.
	NamesOf(ctx context.Context, ids []model.PersonID) (map[model.PersonID]string, error)
	// ConsentOf reports whether person currently consents to purpose (false when unknown or
	// deleted). Internal level, no permission: the module is deciding whether it may
	// process; the module audits the read it then performs. Fails Invalid for a bad
	// purpose or a zero person.
	ConsentOf(ctx context.Context, person model.PersonID, purpose Purpose) (bool, error)
}

// Options builds the service. Audit and Signals are scoped to "people" by app.
type Options struct {
	DB         db.DB
	Clock      clock.Clock
	Logger     *slog.Logger
	Tracer     telemetry.Tracer
	Meter      telemetry.Meter // optional: nil records no metrics
	Audit      audit.Recorder  // scoped "people"
	Signals    signals.Bus     // scoped "people": publishes the three, subscribes identity.account-created
	Authz      authz.Authorizer
	Actor      authz.ActorFunc // app: identity.PersonFrom + SessionFrom.Superuser
	Accounts   Accounts
	Access     Access
	AuditCount AuditCounter
	Policy     PolicySource // optional; nil = DefaultPolicy
}

// Validate fails errs.Invalid joining a validate.FieldError per nil dependency (fields
// people.db, people.clock, people.logger, people.tracer, people.audit, people.signals,
// people.authz, people.actor, people.accounts, people.access, people.audit_count).
func (o Options) Validate() error {
	v := validate.Errors{Op: "people.Options.Validate"}
	for _, d := range []struct {
		field string
		nil   bool
	}{
		{"people.db", o.DB == nil},
		{"people.clock", o.Clock == nil},
		{"people.logger", o.Logger == nil},
		{"people.tracer", o.Tracer == nil},
		{"people.audit", o.Audit == nil},
		{"people.signals", o.Signals == nil},
		{"people.authz", o.Authz == nil},
		{"people.actor", o.Actor == nil},
		{"people.accounts", o.Accounts == nil},
		{"people.access", o.Access == nil},
		{"people.audit_count", o.AuditCount == nil},
	} {
		if d.nil {
			v.Addf(d.field, codeRequired, "must be set")
		}
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// linkState is what Link hands the service, frozen.
type linkState struct {
	sources  []Source
	sections []SectionProvider
}

// Service is people. Safe for concurrent use; nothing to close (no goroutines of its own).
type Service struct {
	store      store
	db         db.DB
	clock      clock.Clock
	logger     *slog.Logger
	tracer     telemetry.Tracer
	audit      audit.Recorder
	signals    signals.Bus
	authz      authz.Authorizer
	actorFn    authz.ActorFunc
	accounts   Accounts
	access     Access
	auditCount AuditCounter
	policySrc  PolicySource
	metrics    metrics
	linked     atomic.Pointer[linkState]
	cooldown   *cooldown
}

var (
	_ Directory           = (*Service)(nil)
	_ identity.NameSource = (*Service)(nil)
)

// New validates o, builds the service over postgres.Store and subscribes the
// identity.account-created handler on Options.Signals. No I/O: it works against db.None()
// (app decision 7) and on a read-only copy. Fails errs.Invalid (F1), the meter's kind when
// an instrument cannot be created and the bus's kind when the subscription is refused.
func New(o Options) (*Service, error) {
	const op = "people.New"
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
	const op = "people.New"
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
		store: st, db: o.DB, clock: o.Clock, logger: o.Logger, tracer: o.Tracer, audit: o.Audit,
		signals: o.Signals, authz: o.Authz, actorFn: o.Actor, accounts: o.Accounts, access: o.Access,
		auditCount: o.AuditCount, policySrc: o.Policy, metrics: m, cooldown: newCooldown(),
	}
	if err := o.Signals.Subscribe(identity.SignalAccountCreated, s.handleAccountCreated); err != nil {
		return nil, fmt.Errorf("%s: subscribe: %w", op, err)
	}
	return s, nil
}

// now is the clock's time in UTC.
func (s *Service) now() time.Time { return s.clock.Now().UTC() }

// start opens the span of one use case, "people.<name>".
func (s *Service) start(ctx context.Context, name string) (context.Context, telemetry.Span) {
	return s.tracer.Start(ctx, "people."+name)
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
	return wrap("people.Service.record", s.audit.Record(ctx, entries...))
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

// publish publishes one signal carrying person (and request when set) in the caller's
// transaction.
func (s *Service) publish(ctx context.Context, name signals.Name, entries ...signals.Entry) error {
	return wrap("people.Service.publish", s.signals.Publish(ctx, name, signals.NewPayload(entries...)))
}

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

// anywhere reports whether the actor holds p in any extent (decision 9).
func (s *Service) anywhere(ctx context.Context, p authz.Permission) (bool, error) {
	ext, err := s.authz.ExtentOf(ctx, p)
	if err != nil {
		return false, wrap("people.Service.anywhere", err)
	}
	return !ext.IsZero(), nil
}

// holds reports whether the actor holds the site-wide permission p. Only errs.Permission
// is "no"; any other failure is returned.
func (s *Service) holds(ctx context.Context, p authz.Permission) (bool, error) {
	err := s.authz.Allow(ctx, p)
	switch {
	case err == nil:
		return true, nil
	case errs.Is(err, errs.Permission):
		return false, nil
	default:
		return false, wrap("people.Service.holds", err)
	}
}

// require fails errs.Permission (with the authorizer's error) unless the actor holds the
// site-wide permission p.
func (s *Service) require(ctx context.Context, op string, p authz.Permission) error {
	if err := s.authz.Allow(ctx, p); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// requireAnywhere fails errs.Permission unless the actor holds p in some extent.
func (s *Service) requireAnywhere(ctx context.Context, op string, p authz.Permission) error {
	ok, err := s.anywhere(ctx, p)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if !ok {
		return errs.E(op, errs.Permission, errors.New("the permission "+string(p)+" is not granted"))
	}
	return nil
}

// selfOrManage lets the actor act on person when it is the actor, or when the actor holds
// people.profiles.manage. Anything else is errs.Permission.
func (s *Service) selfOrManage(ctx context.Context, op string, a authz.Actor, person model.PersonID) (self bool, err error) {
	if !a.Person.IsZero() && a.Person == person {
		return true, nil
	}
	return false, s.require(ctx, op, PermissionProfilesManage)
}

// policy reads the regional's field policy. A nil source is DefaultPolicy; a failing or
// invalid one fails the use case.
func (s *Service) policy(ctx context.Context) (Policy, error) {
	const op = "people.Service.policy"
	if s.policySrc == nil {
		return DefaultPolicy(), nil
	}
	p, err := s.policySrc.Policy(ctx)
	if err != nil {
		return Policy{}, fmt.Errorf("%s: %w", op, err)
	}
	if err := p.Validate(); err != nil {
		return Policy{}, fmt.Errorf("%s: %w", op, err)
	}
	return p, nil
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

// metrics holds the instruments; each method is a no-op without a Meter.
type metrics struct {
	exports   telemetry.Counter
	deletions telemetry.Counter
	sections  telemetry.Counter
}

func newMetrics(m telemetry.Meter) (metrics, error) {
	if m == nil {
		return metrics{}, nil
	}
	var (
		out metrics
		err error
	)
	if out.exports, err = m.Counter("bms.people.exports", telemetry.WithUnit("{export}"),
		telemetry.WithDescription("Per-person exports by outcome.")); err != nil {
		return metrics{}, fmt.Errorf("people.New: %w", err)
	}
	if out.deletions, err = m.Counter("bms.people.deletions", telemetry.WithUnit("{deletion}"),
		telemetry.WithDescription("Per-person deletions by outcome.")); err != nil {
		return metrics{}, fmt.Errorf("people.New: %w", err)
	}
	if out.sections, err = m.Counter("bms.people.sections", telemetry.WithUnit("{section}"),
		telemetry.WithDescription("Person-page sections by module and outcome.")); err != nil {
		return metrics{}, fmt.Errorf("people.New: %w", err)
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

func (m metrics) section(ctx context.Context, module, outcome string) {
	if m.sections != nil {
		m.sections.Add(ctx, 1, telemetry.Token("module", module), telemetry.Token("outcome", outcome))
	}
}

// ---- the export cooldown ----------------------------------------------------------------

// cooldown remembers when each person last exported their own data, bounded by
// MaxTrackedExports (decision 10 and "Cooldown"). Per process, in memory.
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

// Link hands the service the modules' PersonData and page providers, once, from app's link
// step (after module Build, before api.New). Fails errs.Internal on a second call, a nil
// provider or exporter, or a source without a name. Before Link, Export and Delete fail
// errs.Internal and pages carry no sections.
func (s *Service) Link(sources []Source, sections []SectionProvider) error {
	const op = "people.Service.Link"
	for _, src := range sources {
		if src.Name == "" || src.Exporter == nil {
			return errs.E(op, errs.Internal, errors.New("a source needs a name and a PersonData"))
		}
	}
	for _, sec := range sections {
		if sec.Module == "" || sec.Provider == nil {
			return errs.E(op, errs.Internal, errors.New("a section provider needs a module name and a provider"))
		}
	}
	st := &linkState{sources: append([]Source(nil), sources...), sections: append([]SectionProvider(nil), sections...)}
	if !s.linked.CompareAndSwap(nil, st) {
		return errs.E(op, errs.Internal, errors.New("Link was already called"))
	}
	return nil
}

// links returns what Link handed over, or an empty state before it. ok is false before Link.
func (s *Service) links() (st *linkState, ok bool) {
	if st := s.linked.Load(); st != nil {
		return st, true
	}
	return &linkState{}, false
}

// requireLinked fails errs.Internal before Link (F31).
func (s *Service) requireLinked(op string) (*linkState, error) {
	st, ok := s.links()
	if !ok {
		return nil, errs.E(op, errs.Internal, errors.New("Link has not been called"))
	}
	return st, nil
}
