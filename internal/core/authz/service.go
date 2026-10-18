package authz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
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

// store is the storage the service declares (D8); postgres.Store implements it. Every
// error is already classified by db.Wrap. A unique violation is Conflict and a foreign key
// violation Invalid, both with *db.Error.Constraint set; DeleteRole re-kinds the
// referenced-role violation to Conflict.
type store interface {
	LockInvariants(ctx context.Context) error

	EffectivePermissions(ctx context.Context, person model.PersonID, now time.Time) ([]rows.Held, error)
	EffectiveFor(ctx context.Context, person model.PersonID, now time.Time, permission string) ([]rows.Held, error)
	RulesHeldBy(ctx context.Context, person model.PersonID, now time.Time) ([]rows.HeldRule, error)
	TitlesOf(ctx context.Context, person model.PersonID, now time.Time, limit int) ([]string, error)

	ListRoles(ctx context.Context, scope string) ([]rows.Role, error)
	GetRole(ctx context.Context, id rows.RoleID) (rows.Role, error)
	ListRolePermissions(ctx context.Context) (map[rows.RoleID][]string, error)
	RolePermissions(ctx context.Context, id rows.RoleID) ([]string, error)
	InsertRole(ctx context.Context, r rows.Role) error
	UpdateRole(ctx context.Context, id rows.RoleID, name, description string, at time.Time) error
	DeleteRolePermissions(ctx context.Context, id rows.RoleID) error
	InsertRolePermissions(ctx context.Context, id rows.RoleID, perms []string) error
	DeleteRole(ctx context.Context, id rows.RoleID) error
	CountHolders(ctx context.Context, q rows.Holders) (removed, remaining int, err error)

	GetGrant(ctx context.Context, id rows.GrantID) (rows.Grant, error)
	InsertGrant(ctx context.Context, g rows.Grant) error
	DeleteGrant(ctx context.Context, id rows.GrantID) (rows.Grant, error)
	ListGrants(ctx context.Context, f rows.GrantFilter) ([]rows.Grant, error)
	DeleteGrantsByPerson(ctx context.Context, person model.PersonID) (int, error)
	ClearGrantedBy(ctx context.Context, person model.PersonID) error
	ExportByPerson(ctx context.Context, person model.PersonID) ([]rows.ExportGrant, error)

	ListRules(ctx context.Context, now time.Time, declared []rows.Declared, id rows.RuleID) ([]rows.Rule, error)
	InsertRule(ctx context.Context, r rows.Rule) error
	DeleteRule(ctx context.Context, id rows.RuleID) error

	SeedStarterPermissions(ctx context.Context, keys, perms []string, at time.Time) (int, error)
}

var _ store = (*postgres.Store)(nil)

// Options builds the service. Audit and Signals are scoped to "access" by app.
type Options struct {
	DB          db.DB
	Clock       clock.Clock
	Logger      *slog.Logger
	Tracer      telemetry.Tracer
	Meter       telemetry.Meter // optional: nil records no metrics
	Audit       audit.Recorder  // scoped to "access"; required
	Signals     signals.Bus     // scoped to "access"; required
	Actor       ActorFunc       // required
	Declared    []PermissionDecl
	Departments DepartmentSource // optional: nil refuses department grants (Unsupported)
}

// Validate fails errs.Invalid joining a validate.FieldError per problem: "authz.db",
// "authz.clock", "authz.logger", "authz.tracer", "authz.audit", "authz.signals",
// "authz.actor" (nil), "authz.declared[i].name|scope|reaches|starter_roles[j]" (a
// declaration that fails PermissionDecl.Validate) and "authz.declared" (two declarations
// with one name, code duplicate).
func (o Options) Validate() error {
	v := validate.Errors{Op: "authz.Options.Validate"}
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
	if o.Audit == nil {
		v.Addf("authz.audit", codeRequired, "must be set")
	}
	if o.Signals == nil {
		v.Addf("authz.signals", codeRequired, "must be set")
	}
	if o.Actor == nil {
		v.Addf("authz.actor", codeRequired, "must be set")
	}
	seen := make(map[Permission]struct{}, len(o.Declared))
	dup := false
	for i, d := range o.Declared {
		prefix := "authz.declared[" + strconv.Itoa(i) + "]."
		for _, fe := range validate.Fields(d.Validate()) {
			v.Addf(prefix+fe.Field, fe.Code, "%s", fe.Reason)
		}
		if _, ok := seen[d.Name]; ok {
			dup = true
		}
		seen[d.Name] = struct{}{}
	}
	if dup {
		v.Addf("authz.declared", codeDuplicate, "a permission is declared twice")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// catalogue is the declared permissions, frozen at New.
type catalogue struct {
	list   []PermissionDecl // sorted by name
	byName map[Permission]PermissionDecl
}

func newCatalogue(declared []PermissionDecl) catalogue {
	c := catalogue{byName: make(map[Permission]PermissionDecl, len(declared))}
	for _, d := range declared {
		d.StarterRoles = slices.Clone(d.StarterRoles)
		c.byName[d.Name] = d
		c.list = append(c.list, d)
	}
	slices.SortFunc(c.list, func(a, b PermissionDecl) int { return strings.Compare(string(a.Name), string(b.Name)) })
	return c
}

// Service is authz. Safe for concurrent use; nothing to close (no goroutines).
type Service struct {
	store   store
	db      db.DB
	clock   clock.Clock
	logger  *slog.Logger
	tracer  telemetry.Tracer
	audit   audit.Recorder
	signals signals.Bus
	actorFn ActorFunc
	depts   DepartmentSource
	cat     catalogue
	metrics metrics
}

var (
	_ Authorizer        = (*Service)(nil)
	_ audit.TitleSource = (*Service)(nil)
)

// New validates o and returns the service over postgres.Store. No I/O: it works against
// db.None() (app decision 7) and on a read-only copy. Fails errs.Invalid (F1, F2) and the
// meter's kind when an instrument cannot be created.
func New(o Options) (*Service, error) {
	const op = "authz.New"
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
	const op = "authz.New"
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
	return &Service{
		store: st, db: o.DB, clock: o.Clock, logger: o.Logger, tracer: o.Tracer,
		audit: o.Audit, signals: o.Signals, actorFn: o.Actor, depts: o.Departments,
		cat: newCatalogue(o.Declared), metrics: m,
	}, nil
}

// now is the clock's time in UTC.
func (s *Service) now() time.Time { return s.clock.Now().UTC() }

// start opens the span of one use case, "authz.<name>".
func (s *Service) start(ctx context.Context, name string) (context.Context, telemetry.Span) {
	return s.tracer.Start(ctx, "authz."+name)
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
	return wrap("authz.Service.record", s.audit.Record(ctx, entries...))
}

// wrap adds op to err and keeps its kind and its chain (errors.Is still finds
// db.ErrReadOnly). nil stays nil.
func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}

// fieldInvalid builds an errs.Invalid error carrying one validate.FieldError.
func fieldInvalid(op, field, code, format string, args ...any) error {
	v := validate.Errors{Op: op}
	v.Addf(field, code, format, args...)
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// fieldConflict builds an errs.Conflict error carrying one validate.FieldError (the api
// shows the fields of a Conflict as it does those of an Invalid).
func fieldConflict(op, field, code string, params map[string]string, reason string) error {
	return errs.E(op, errs.Conflict, &validate.FieldError{Field: field, Code: code, Params: params, Reason: reason})
}

// constraintOf returns the constraint name a database error carries, or "".
func constraintOf(err error) string {
	var pe *db.Error
	if errors.As(err, &pe) {
		return pe.Constraint
	}
	return ""
}

// actor returns the person making the request: errs.Unauthenticated inside a signal
// delivery (decision 7), without a session or with a restricted one.
func (s *Service) actor(ctx context.Context, op string) (Actor, error) {
	if signals.InDelivery(ctx) {
		return Actor{}, errs.E(op, errs.Unauthenticated, errors.New("a signal handler acts as the system, not as a person"))
	}
	a, ok := s.actorFn(ctx)
	if !ok || (a.Person.IsZero() && !a.Superuser) {
		return Actor{}, errs.E(op, errs.Unauthenticated, errors.New("no signed-in person"))
	}
	return a, nil
}

// metrics holds the instruments; each method is a no-op without a Meter.
type metrics struct {
	decisions telemetry.Counter
}

func newMetrics(m telemetry.Meter) (metrics, error) {
	if m == nil {
		return metrics{}, nil
	}
	c, err := m.Counter("bms.authz.decisions", telemetry.WithUnit("{decision}"),
		telemetry.WithDescription("Authorization decisions by outcome."))
	if err != nil {
		return metrics{}, fmt.Errorf("authz.New: %w", err)
	}
	return metrics{decisions: c}, nil
}

// The outcomes bms.authz.decisions counts.
const (
	outcomeAllowed         = "allowed"
	outcomeDenied          = "denied"
	outcomeSuperuser       = "superuser"
	outcomeUnauthenticated = "unauthenticated"
	outcomeError           = "error"
)

func (m metrics) decided(ctx context.Context, outcome string) {
	if m.decisions != nil {
		m.decisions.Add(ctx, 1, telemetry.Token("outcome", outcome))
	}
}

// inTx runs fn in a transaction of the given mode. fn's errors carry their own context, so
// the transaction's error is returned as it is.
func (s *Service) inTx(ctx context.Context, mode db.Mode, fn func(ctx context.Context) error) error {
	return s.db.Transact(ctx, mode, fn) //nolint:wrapcheck // fn's errors are wrapped where they arise
}
