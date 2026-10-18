// Package hello is the reference module: the smallest module that touches the whole module
// contract (manifest, permissions, setting, signals, routes, OpenAPI fragment, person data,
// Start and Stop). It is not useful to a burner; a regional turns it off with
// BMS_MODULES_DISABLED=hello. Copy it to start a real module (plan/design/module-hello.md).
package hello

import (
	"context"
	_ "embed" // the OpenAPI fragment
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/contracts/greeter"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/hello/greeting"
	"github.com/zedgama3/bms/internal/modules/hello/postgres"
)

const (
	// PermissionGreetingsRead allows GET /greeting (module-contract: "<module>.<ident>(.<ident>)*").
	// Scope site, Reaches model.SensitivityInternal.
	PermissionGreetingsRead authz.Permission = "hello.greetings.read"
	// PermissionGreetingsWrite allows POST /greetings. Scope site, Reaches
	// model.SensitivityInternal.
	PermissionGreetingsWrite authz.Permission = "hello.greetings.write"

	// SettingGreeting is the one setting: the word used in every greeting.
	SettingGreeting = "greeting"
	// DefaultGreeting is the setting's default.
	DefaultGreeting = "hello"
	// MaxGreetingBytes is the longest greeting word, in bytes, after trimming.
	MaxGreetingBytes = greeting.MaxBytes

	// SignalGreeted is published inside the transaction that saves a greeting.
	SignalGreeted signals.Name = "hello.greeted"
	// PayloadPerson is the one payload key: the person who greeted.
	PayloadPerson = "person"

	// PointBanner is the extension point hello defines and contributes to.
	PointBanner module.ExtensionPoint = "hello.banner"

	// seenRing is how many Signal.IDs the counter remembers to stay idempotent.
	seenRing = 256
)

// Manifest declares hello to the core. It carries no text (module-contract decision 8).
var Manifest = module.Manifest{
	Name:       "hello",
	Migrations: postgres.Migrations,
	Permissions: []authz.PermissionDecl{
		{Name: PermissionGreetingsRead, Scope: authz.ScopeSite, Reaches: model.SensitivityInternal},
		{Name: PermissionGreetingsWrite, Scope: authz.ScopeSite, Reaches: model.SensitivityInternal},
	},
	Settings:      []module.SettingDecl{{Key: SettingGreeting, Kind: module.SettingText, Default: DefaultGreeting, Check: validateGreeting}},
	Publishes:     []signals.Name{SignalGreeted},
	Subscribes:    []signals.Name{SignalGreeted},
	Provides:      []module.Capability{greeter.Name},
	Defines:       []module.ExtensionPointDecl{{Name: PointBanner, Accepts: acceptsBanner}},
	ContributesTo: []module.ExtensionPoint{PointBanner},
}

func init() { module.Register(Manifest, New) }

//go:embed openapi.json
var openAPIFragment []byte

// bannerKey is the message key hello contributes to its own extension point.
const bannerKey = "module.hello.banner.welcome"

var (
	errDenied    = errors.New("no authorizer is wired yet (M0): every permission is denied")
	errNoActor   = errors.New("no signed-in person in the request context")
	errStarted   = errors.New("the module is already started")
	errNilDep    = errors.New("nil dependency")
	errBannerKey = errors.New("a banner key must be module.<contributor>.banner.<ident>")
)

// validateGreeting is the setting's rule: 1..MaxGreetingBytes bytes after trimming, valid
// UTF-8, no control characters. It fails with errs.Invalid on field "hello.greeting".
func validateGreeting(value string) error {
	acc := validate.Errors{Op: "hello.validateGreeting"}
	v := strings.TrimSpace(value)
	acc.Add(validate.Range("hello.greeting", len(v), 1, MaxGreetingBytes))
	if !utf8.ValidString(v) {
		acc.Addf("hello.greeting", "malformed", "must be valid UTF-8")
	}
	if strings.ContainsFunc(v, unicode.IsControl) {
		acc.Addf("hello.greeting", "control", "must not contain control characters")
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// acceptsBanner reports whether v is a greeter.Banner, the point's contract type.
func acceptsBanner(v any) bool {
	_, ok := v.(greeter.Banner)
	return ok
}

// seams are the three seams and the store (module-hello decision 2). New wires them to the
// postgres store, identity.PersonFrom (M1), "deny" (until authz, M2) and "the declared
// default" (until settings, M2); tests inject fakes.
type seams struct {
	store    store
	actor    actorFunc
	allow    permissionChecker
	greeting greetingSource
}

// denyAll is the M0 permissionChecker: it refuses every permission.
type denyAll struct{}

// Allow always fails with errs.Permission.
func (denyAll) Allow(context.Context, authz.Permission) error {
	return errs.E("hello.allow", errs.Permission, errDenied)
}

// declaredDefault is the M0 greetingSource: the setting's declared default.
func declaredDefault(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", errs.E("hello.greeting", errs.KindOf(err), err)
	}
	return DefaultGreeting, nil
}

// trimmed wraps a greetingSource so the word has no surrounding space, as the setting's
// rule (validateGreeting) judges it trimmed. A value that passes the rule is then a word
// greeting.Validate accepts.
func trimmed(src greetingSource) greetingSource {
	return func(ctx context.Context) (string, error) {
		word, err := src(ctx)
		if err != nil {
			return "", err //nolint:wrapcheck // the source's own error, wrapped by each use case
		}
		return strings.TrimSpace(word), nil
	}
}

// Module is hello. It implements module.Module, module.RouteProvider, module.APIDescriber,
// module.CapabilityProvider and module.Contributor. Safe for concurrent use after Start.
type Module struct {
	svc *service

	mu      sync.Mutex // guards started
	started bool
}

// New builds the module from deps. It reads nothing from the database (app.md decision 7)
// and starts nothing. Fails with errs.Invalid when Logger, Tracer, Clock, DB, Signals or
// Links is nil, or when postgres.NewStore fails.
func New(_ context.Context, deps module.Deps) (module.Module, error) {
	const op = "hello.New"
	if err := checkDeps(op, deps); err != nil {
		return nil, err
	}
	st, err := postgres.NewStore(deps.DB)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return newModule(deps, seams{store: st, actor: identity.PersonFrom, allow: denyAll{}, greeting: declaredDefault})
}

// checkDeps fails with errs.Invalid naming every nil dependency.
func checkDeps(op string, d module.Deps) error {
	var nils []string
	if d.Logger == nil {
		nils = append(nils, "Logger")
	}
	if d.Tracer == nil {
		nils = append(nils, "Tracer")
	}
	if d.Clock == nil {
		nils = append(nils, "Clock")
	}
	if d.DB == nil {
		nils = append(nils, "DB")
	}
	if d.Signals == nil {
		nils = append(nils, "Signals")
	}
	if d.Links == nil {
		nils = append(nils, "Links")
	}
	if len(nils) > 0 {
		return errs.E(op, errs.Invalid, fmt.Errorf("%w: %s", errNilDep, strings.Join(nils, ", ")))
	}
	return nil
}

// newModule assembles the module over s. It is New without the production wiring.
func newModule(deps module.Deps, s seams) (*Module, error) {
	const op = "hello.newModule"
	if err := checkDeps(op, deps); err != nil {
		return nil, err
	}
	if s.store == nil || s.actor == nil || s.allow == nil || s.greeting == nil {
		return nil, errs.E(op, errs.Invalid, fmt.Errorf("%w: a seam is nil", errNilDep))
	}
	return &Module{svc: &service{
		store:    s.store,
		actor:    s.actor,
		allow:    s.allow,
		greeting: trimmed(s.greeting),
		links:    deps.Links,
		bus:      deps.Signals,
		db:       deps.DB,
		tracer:   deps.Tracer,
		logger:   deps.Logger,
		clock:    deps.Clock,
		counter:  newCounter(),
	}}, nil
}

// Start validates every banner contribution (frozen values, no I/O), then subscribes the
// counter's handler to SignalGreeted and starts the counter goroutine; validating first
// means a failed Start leaves no handler behind. It writes nothing
// (module-contract decision 14). Fails with errs.Conflict if called twice, errs.Invalid
// when a contribution's Key is not "module.<its module>.banner.<ident>" (naming the
// module), or the bus's kind from Subscribe.
func (m *Module) Start(ctx context.Context) error {
	const op = "hello.Start"
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return errs.E(op, errs.Conflict, errStarted)
	}
	banners, err := m.svc.banners()
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	for _, b := range banners {
		if !validBannerKey(b.Module, b.Key) {
			return errs.E(op, errs.Invalid, fmt.Errorf("module %s: %w", b.Module, errBannerKey))
		}
	}
	if err := m.svc.bus.Subscribe(SignalGreeted, m.svc.handleGreeted); err != nil {
		return fmt.Errorf("%s: subscribe: %w", op, err)
	}
	m.svc.counter.start()
	m.started = true
	m.svc.logger.DebugContext(ctx, "hello started", slog.Int("banners", len(banners)))
	return nil
}

// validBannerKey reports whether key is "module.<contributor>.banner.<ident>".
func validBannerKey(contributor, key string) bool {
	ident, ok := strings.CutPrefix(key, "module."+contributor+".banner.")
	if !ok || ident == "" {
		return false
	}
	for i, r := range ident {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' && i > 0 && i < len(ident)-1:
		default:
			return false
		}
	}
	return true
}

// Stop asks the counter goroutine to exit and waits for it, or for ctx. Nil when Start
// was never called or Stop already ran. Fails with errs.Canceled or errs.Timeout from ctx.
func (m *Module) Stop(ctx context.Context) error {
	const op = "hello.Stop"
	m.mu.Lock()
	started := m.started
	m.mu.Unlock()
	if !started {
		return nil
	}
	if err := m.svc.counter.stop(ctx); err != nil {
		return errs.E(op, errs.KindOf(err), err)
	}
	return nil
}

// Routes are relative to /api/hello (module-contract decision 13).
func (m *Module) Routes() []module.Route {
	return []module.Route{
		{Method: "GET", Pattern: "/public-greeting", Access: module.AccessPublic, Handler: m.publicGreeting()},
		{Method: "GET", Pattern: "/greeting", Access: module.AccessSignedIn, Handler: m.greeting()},
		{Method: "POST", Pattern: "/greetings", Access: module.AccessSignedIn, Handler: m.greet()},
	}
}

// OpenAPI returns the embedded openapi.json (api.md decision 11).
func (m *Module) OpenAPI() []byte { return append([]byte(nil), openAPIFragment...) }

// Capabilities provides greeter.Name as a greeter.Greeter.
func (m *Module) Capabilities() map[module.Capability]any {
	return map[module.Capability]any{greeter.Name: greeter.Greeter(greeterImpl{source: m.svc.greeting})}
}

// Contributions contributes greeter.Banner{Key: "module.hello.banner.welcome"} to PointBanner.
func (m *Module) Contributions() map[module.ExtensionPoint]any {
	return map[module.ExtensionPoint]any{PointBanner: greeter.Banner{Key: bannerKey}}
}

// greeterImpl is the greeter.Greeter hello provides: the greeting word and nothing else.
type greeterImpl struct {
	source greetingSource
}

// Greeting returns the current greeting word. Fails with the source's kind, and with
// errs.Canceled or errs.Timeout when ctx is done.
func (g greeterImpl) Greeting(ctx context.Context) (string, error) {
	word, err := g.source(ctx)
	if err != nil {
		return "", fmt.Errorf("hello.Greeter.Greeting: %w", err)
	}
	return word, nil
}

var (
	_ module.Module             = (*Module)(nil)
	_ module.RouteProvider      = (*Module)(nil)
	_ module.APIDescriber       = (*Module)(nil)
	_ module.CapabilityProvider = (*Module)(nil)
	_ module.Contributor        = (*Module)(nil)
)
