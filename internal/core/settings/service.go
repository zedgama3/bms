package settings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/settings/postgres"
	"github.com/zedgama3/bms/internal/core/settings/rows"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// store is the storage the service declares (D8); postgres.Store implements it. Every error
// is already classified by db.Wrap; a missing revision row is errs.NotFound.
type store interface {
	// Revision reads the revision without a lock.
	Revision(ctx context.Context) (int64, error)
	// LockRevision takes the revision row's lock until the transaction ends (the first
	// statement of a write) and returns the revision before the write.
	LockRevision(ctx context.Context) (int64, error)
	// BumpRevision adds one and returns the new revision (the last statement of a write
	// that changed something).
	BumpRevision(ctx context.Context) (int64, error)
	// LoadSettings and LoadTerminology return every stored row, declared or not.
	LoadSettings(ctx context.Context) ([]rows.Setting, error)
	LoadTerminology(ctx context.Context) ([]rows.Term, error)
	// UpsertSetting stores a key away from its default (value xor sealed) and reports
	// whether a row was inserted or changed; DeleteSetting resets it and reports whether
	// a row existed.
	UpsertSetting(ctx context.Context, key string, value *string, sealed []byte, now time.Time) (bool, error)
	DeleteSetting(ctx context.Context, key string) (bool, error)
	// UpsertTermForms and DeleteTermForms return the forms that really changed, sorted.
	UpsertTermForms(ctx context.Context, lang, term string, forms, values []string, now time.Time) ([]string, error)
	DeleteTermForms(ctx context.Context, lang, term string, forms []string) ([]string, error)
}

var _ store = (*postgres.Store)(nil)

// Options builds a Service. Audit is scoped "settings" by app.
type Options struct {
	DB     db.DB
	Clock  clock.Clock
	Logger *slog.Logger
	Tracer telemetry.Tracer
	// Meter is optional: without one the service counts nothing.
	Meter telemetry.Meter
	Audit audit.Recorder // scoped "settings"
	// Authz is the authz.Lookup app builds before settings (decisions only).
	Authz           authz.Authorizer
	Actor           authz.ActorFunc
	Areas           []Area        // every area: the core's (regional first), then the modules'
	SealingKey      secret.Secret // optional; without it Secret values cannot be set or read
	RefreshInterval time.Duration // 0 = DefaultRefreshInterval; 1s..10m
}

// minSealingKeyBytes is the shortest sealing key config allows.
const minSealingKeyBytes = 32

// Validate fails errs.Invalid joining a validate.FieldError per problem: a nil dependency
// (fields settings.db, settings.clock, settings.logger, settings.tracer, settings.audit,
// settings.authz, settings.actor), settings.refresh_interval outside 1s..10m,
// settings.sealing_key for a key under 32 bytes, settings.areas[i].<field> for an invalid area, and settings.areas for a duplicate area
// name.
func (o Options) Validate() error {
	v := validate.Errors{Op: "settings.Options.Validate"}
	for _, d := range []struct {
		field string
		nil   bool
	}{
		{"settings.db", o.DB == nil},
		{"settings.clock", o.Clock == nil},
		{"settings.logger", o.Logger == nil},
		{"settings.tracer", o.Tracer == nil},
		{"settings.audit", o.Audit == nil},
		{"settings.authz", o.Authz == nil},
		{"settings.actor", o.Actor == nil},
	} {
		if d.nil {
			v.Addf(d.field, codeRequired, "must be set")
		}
	}
	if o.SealingKey.IsSet() && len(o.SealingKey.Value()) < minSealingKeyBytes {
		v.Addf("settings.sealing_key", "too_short", "must be at least %d bytes", minSealingKeyBytes)
	}
	if o.RefreshInterval != 0 {
		v.Add(validate.Range("settings.refresh_interval", o.RefreshInterval, minRefreshInterval, maxRefreshInterval))
	}
	seen := make(map[string]struct{}, len(o.Areas))
	termArea := map[string]int{} // term key -> index of the first area that declared it
	for i, a := range o.Areas {
		for _, fe := range validate.Fields(a.Validate()) {
			v.Addf(fmt.Sprintf("settings.areas[%d].%s", i, fe.Field), fe.Code, "%s", fe.Reason)
		}
		for k, t := range a.Terms {
			first, dup := termArea[t.Key]
			if !dup {
				termArea[t.Key] = i
				continue
			}
			if first != i { // a repeat inside one area is Area.Validate's
				v.Addf(fmt.Sprintf("settings.areas[%d].terms[%d].key", i, k), codeDuplicate,
					"the term is also declared by another area; move the word to CoreTerms so both use one")
			}
		}
		if _, dup := seen[a.Name]; dup {
			v.Addf("settings.areas", "duplicate", "an area is declared more than once")
		}
		seen[a.Name] = struct{}{}
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// entry is one declared setting in the index.
type entry struct {
	area      string
	full      string // "<area>.<key>"
	decl      Decl
	normalize func(string) string // nil = none
}

// Service is settings. Safe for concurrent use; the one goroutine is Run's, owned by the
// caller.
type Service struct {
	store  store
	db     db.DB
	clock  clock.Clock
	logger *slog.Logger
	tracer telemetry.Tracer
	audit  audit.Recorder
	authz  authz.Authorizer
	actorF authz.ActorFunc

	areas    []Area           // core areas, then modules, each in the order given
	index    map[string]entry // full key -> entry
	areaIdx  map[string]int   // area name -> position in areas
	sealer   *sealer          // nil without a sealing key
	interval time.Duration
	defaults map[string]map[string]Forms // lang -> term -> forms, embedded (core terms)
	terms    []termEntry                 // every declared term: CoreTerms first, then areas in build order (D57)
	termIdx  map[string]int              // term key -> position in terms

	snap    atomic.Pointer[snapshot]
	loading chan struct{} // capacity 1: held while the first load runs
	running atomic.Bool

	warned   sync.Map // full key -> struct{}: stale values already warned about
	pollMu   sync.Mutex
	pollWarn time.Time // when a failed poll was last logged

	metrics metrics
}

// New validates o and builds the service over postgres.Store. No I/O: it works against
// db.None() and on a read-only copy. Fails errs.Invalid.
func New(o Options) (*Service, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	st, err := postgres.NewStore(o.DB)
	if err != nil {
		return nil, fmt.Errorf("settings.New: %w", err)
	}
	return newService(o, st)
}

// newService builds the service over st. Tests hand it a fake store.
func newService(o Options, st store) (*Service, error) {
	const op = "settings.New"
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if st == nil {
		return nil, errs.E(op, errs.Invalid, errors.New("nil store"))
	}
	sl, err := newSealer(o.SealingKey)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defaults, err := loadDefaultTerms()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	m, err := newMetrics(o.Meter)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	s := &Service{
		store: st, db: o.DB, clock: o.Clock, logger: o.Logger, tracer: o.Tracer, audit: o.Audit,
		authz: o.Authz, actorF: o.Actor, sealer: sl, interval: o.RefreshInterval, defaults: defaults,
		loading: make(chan struct{}, 1), metrics: m,
	}
	if s.interval == 0 {
		s.interval = DefaultRefreshInterval
	}
	s.areas = orderAreas(o.Areas)
	s.indexTerms()
	s.index = make(map[string]entry)
	s.areaIdx = make(map[string]int, len(s.areas))
	for i, a := range s.areas {
		s.areaIdx[a.Name] = i
		for _, d := range a.Decls {
			full := a.Name + "." + d.Key
			s.index[full] = entry{area: a.Name, full: full, decl: d, normalize: d.Normalize}
		}
	}
	return s, nil
}

// orderAreas puts the core areas first, then the modules, each group in the order given.
func orderAreas(in []Area) []Area {
	out := make([]Area, 0, len(in))
	for _, a := range in {
		if a.Core {
			out = append(out, cloneArea(a))
		}
	}
	for _, a := range in {
		if !a.Core {
			out = append(out, cloneArea(a))
		}
	}
	return out
}

func cloneArea(a Area) Area {
	ds := make([]Decl, len(a.Decls))
	for i, d := range a.Decls {
		d.Options = append([]string(nil), d.Options...)
		ds[i] = d
	}
	a.Decls = ds
	ts := make([]TermDecl, len(a.Terms))
	for i, t := range a.Terms {
		if t.Defaults == nil {
			ts[i] = t
			continue
		}
		defs := make(map[string]Forms, len(t.Defaults))
		for lang, f := range t.Defaults {
			defs[lang] = f
		}
		t.Defaults = defs
		ts[i] = t
	}
	a.Terms = ts
	return a
}

// termEntry is one declared term in the index: where it comes from and, for a module's
// term, its defaults (a core term's are in Service.defaults).
type termEntry struct {
	key      string
	area     string           // coreArea, or the declaring area's name
	defaults map[string]Forms // nil for a core term
}

// indexTerms lists every declared term: CoreTerms first, then each area's in build order.
// Options.Validate has already refused a key declared twice.
func (s *Service) indexTerms() {
	s.terms = make([]termEntry, 0, len(CoreTerms))
	for _, k := range CoreTerms {
		s.terms = append(s.terms, termEntry{key: k, area: coreArea})
	}
	for _, a := range s.areas {
		for _, t := range a.Terms {
			s.terms = append(s.terms, termEntry{key: t.Key, area: a.Name, defaults: t.Defaults})
		}
	}
	s.termIdx = make(map[string]int, len(s.terms))
	for i, t := range s.terms {
		s.termIdx[t.key] = i
	}
}

// ---- helpers shared by the use cases -----------------------------------------------------

func (s *Service) now() time.Time { return s.clock.Now().UTC() }

// start opens the span of one use case, "settings.<name>".
func (s *Service) start(ctx context.Context, name string) (context.Context, telemetry.Span) {
	return s.tracer.Start(ctx, "settings."+name)
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

// inTx runs fn in a transaction of the given mode. fn's errors carry their own context.
func (s *Service) inTx(ctx context.Context, mode db.Mode, fn func(ctx context.Context) error) error {
	return s.db.Transact(ctx, mode, fn) //nolint:wrapcheck // fn's errors are wrapped where they arise
}

// require fails errs.Unauthenticated without a signed-in person and errs.Permission unless
// the actor holds the site-wide permission p.
func (s *Service) require(ctx context.Context, op string, p authz.Permission) error {
	a, ok := s.actorF(ctx)
	if !ok || (a.Person.IsZero() && !a.Superuser) {
		return errs.E(op, errs.Unauthenticated, errors.New("no signed-in person"))
	}
	if err := s.authz.Allow(ctx, p); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// ---- metrics -----------------------------------------------------------------------------

// The outcomes bms.settings.refresh takes.
const (
	outcomeOK        = "ok"
	outcomeUnchanged = "unchanged"
	outcomeFailed    = "failed"
)

// metrics holds the instruments; each method is a no-op without a Meter.
type metrics struct {
	refresh telemetry.Counter
	stale   telemetry.Counter
}

func newMetrics(m telemetry.Meter) (metrics, error) {
	if m == nil {
		return metrics{}, nil
	}
	var (
		out metrics
		err error
	)
	if out.refresh, err = m.Counter("bms.settings.refresh", telemetry.WithUnit("{refresh}"),
		telemetry.WithDescription("Snapshot refresh polls by outcome.")); err != nil {
		return metrics{}, fmt.Errorf("settings.New: %w", err)
	}
	if out.stale, err = m.Counter("bms.settings.stale_values", telemetry.WithUnit("{read}"),
		telemetry.WithDescription("Reads that fell back to the default because the stored value no longer parses.")); err != nil {
		return metrics{}, fmt.Errorf("settings.New: %w", err)
	}
	return out, nil
}

func (m metrics) refreshed(ctx context.Context, outcome string) {
	if m.refresh != nil {
		m.refresh.Add(ctx, 1, telemetry.Token("outcome", outcome))
	}
}

func (m metrics) staleRead(ctx context.Context) {
	if m.stale != nil {
		m.stale.Add(ctx, 1)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
