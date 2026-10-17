// Package api is the HTTP shell: it mounts the routes it is given, runs every request
// through one pipeline, maps errors, serves the shell's own endpoints, assembles the
// OpenAPI document and serves the web shell. It imports core contracts, model and lib,
// never a module, app or config (layer rule 4; depguard rule api).
//
// The wire form of an error, the kind-to-status table and the three calls a handler makes
// live in lib/httpx, because modules may not import this package (api.md decision 2).
package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/core/telemetry/intake"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Authenticator resolves the session a request carries. identity implements it (M1);
// app passes it in Options. The shell calls it once per routed request, for public and
// signed-in routes alike, so the actor is on the span either way.
type Authenticator interface {
	// Authenticate reads r (its cookie or header, identity's choice) and returns a ctx
	// carrying the principal in the form identity defines, the person behind it, and nil.
	// No session: (ctx, zero, nil). A session that is present but invalid, expired or
	// revoked: errs.Unauthenticated. A dependency failure: its kind (Unavailable,
	// Timeout). It must not write to the database and must not write to w; the shell never
	// passes one. A nil ctx with a nil error is a bug in the implementation; the shell
	// answers 500.
	Authenticate(ctx context.Context, r *http.Request) (context.Context, model.PersonID, error)
}

// PermissionChecker answers whether the actor in ctx holds a site-wide permission. authz
// implements it (M2) with its Authorizer; app passes it in Options. Until then it is nil
// and every permissioned shell route answers 403.
type PermissionChecker interface {
	// Allow fails with errs.Permission when the actor lacks p, errs.Unauthenticated when
	// ctx carries no actor, and the store's kind when the decision cannot be made.
	Allow(ctx context.Context, p authz.Permission) error
}

// Database is what /readyz and the X-Read-Only header need from db. *db.Pool implements it.
type Database interface {
	Ping(ctx context.Context) error
	ReadOnly() bool
}

// SchemaStatus reports each migration set's built and applied versions for
// GET /api/admin/modules. app builds it over db.Migrator.Status and the sets it knows.
type SchemaStatus func(ctx context.Context) ([]db.Status, error)

// PermissionModulesRead guards GET /api/admin/modules. It is declared here, where it is
// checked; app hands it to authz with the core's other declarations. Scope site; reaches
// internal.
const PermissionModulesRead authz.Permission = "core.modules.read"

// Options is what New needs. app copies config.HTTP into the first five fields and
// supplies the rest.
type Options struct {
	// Addr is the listen address, as config validated it ("host:port" or ":port").
	Addr string
	// RequestTimeout is the deadline of one request. 1s..10m.
	RequestTimeout time.Duration
	// MaxBodyBytes caps every request body. 1024..1073741824.
	MaxBodyBytes int64
	// TrustedProxies are the peers whose X-Forwarded-For and X-Forwarded-Proto are
	// believed and, when non-empty, the only non-loopback peers served. A prefix of zero
	// bits (0.0.0.0/0, ::/0) is refused by Validate: it would trust every peer's headers
	// and switch the peer check off. A front end on the same host is listed as
	// 127.0.0.1/32 (or ::1/128); otherwise its clients all look like loopback.
	TrustedProxies []netip.Prefix
	// AllowPlaintext serves any peer over plain HTTP, for development; New logs one
	// warning when it is true.
	AllowPlaintext bool

	Logger *slog.Logger     // component=api; never nil
	Tracer telemetry.Tracer // scope "api"; never nil
	Meter  telemetry.Meter  // scope "api"; never nil
	Clock  clock.Clock      // the misdirected log rate and the readiness cache; never nil
	// Version is the build's version, for the OpenAPI info block and GET /api/admin/modules
	// (never /healthz). A token of at most 64 bytes.
	Version string

	// Routes are the route sets to mount: every module's (module.Set.Routes()) and the
	// core's. May be empty. Each set's OpenAPI fragment is required when it has routes.
	Routes []module.ModuleRoutes

	Database Database       // never nil
	Intake   *intake.Intake // never nil
	Graph    module.Graph   // from Plan.Graph(); serializable
	Schema   SchemaStatus   // never nil

	// Authenticator is nil in M0: every signed-in route answers 401.
	Authenticator Authenticator
	// Permissions is nil until M2: every permissioned shell route answers 403.
	Permissions PermissionChecker

	// UI is the built web shell (index.html at its root, hashed files under assets/).
	// nil means no UI: paths outside /api, /healthz and /readyz answer 404.
	UI fs.FS
	// ContentSecurityPolicy is sent with every text/html response. Empty means DefaultCSP.
	ContentSecurityPolicy string
}

// DefaultCSP is the policy sent with every HTML response when
// Options.ContentSecurityPolicy is empty: same origin for everything, data: images, no
// plugins, no frames, no inline scripts.
const DefaultCSP = "default-src 'self'; img-src 'self' data:; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

const (
	minRequestTimeout = time.Second
	maxRequestTimeout = 10 * time.Minute
	minMaxBody        = 1024
	maxMaxBody        = 1 << 30
	maxVersionBytes   = 64
)

// Validate fails with errs.Invalid joining one validate.FieldError per problem, fields
// "http.addr", "http.request_timeout", "http.max_body_bytes", "http.trusted_proxies" (an
// invalid prefix, or one of zero bits), "api.logger", "api.tracer", "api.meter",
// "api.clock", "api.version", "api.database", "api.intake", "api.schema" and
// "api.routes[i]" (a set whose name is not a module name, is repeated or reserved, whose
// route is invalid, or which has routes and no OpenAPI fragment). The address may use
// port 0, so tests can bind ":0".
func (o Options) Validate() error {
	acc := validate.Errors{Op: "api.Options.Validate"}
	acc.Add(validateAddr("http.addr", o.Addr))
	acc.Add(validate.Range("http.request_timeout", o.RequestTimeout, minRequestTimeout, maxRequestTimeout))
	acc.Add(validate.Range("http.max_body_bytes", o.MaxBodyBytes, minMaxBody, maxMaxBody))
	for _, p := range o.TrustedProxies {
		switch {
		case !p.IsValid():
			acc.Addf("http.trusted_proxies", validate.CodeHostPort, "contains an invalid prefix")
		case p.Bits() == 0:
			acc.Addf("http.trusted_proxies", "zero_bits",
				"trusts every peer: list the front end's addresses or networks")
		}
	}
	if o.Logger == nil {
		acc.Addf("api.logger", validate.CodeRequired, "must not be nil")
	}
	if o.Tracer == nil {
		acc.Addf("api.tracer", validate.CodeRequired, "must not be nil")
	}
	if o.Meter == nil {
		acc.Addf("api.meter", validate.CodeRequired, "must not be nil")
	}
	if o.Clock == nil {
		acc.Addf("api.clock", validate.CodeRequired, "must not be nil")
	}
	switch {
	case o.Version == "":
		acc.Addf("api.version", validate.CodeRequired, "must not be empty")
	case len(o.Version) > maxVersionBytes || !telemetry.IsToken(o.Version):
		acc.Addf("api.version", validate.CodeMalformed, "must be a token of at most 64 bytes")
	}
	if o.Database == nil {
		acc.Addf("api.database", validate.CodeRequired, "must not be nil")
	}
	if o.Intake == nil {
		acc.Addf("api.intake", validate.CodeRequired, "must not be nil")
	}
	if o.Schema == nil {
		acc.Addf("api.schema", validate.CodeRequired, "must not be nil")
	}
	o.validateRoutes(&acc)
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// validateAddr is validate.ListenAddr, except that port 0 (any free port) is allowed.
func validateAddr(field, addr string) error {
	if host, port, err := net.SplitHostPort(addr); err == nil && port == "0" {
		addr = net.JoinHostPort(host, "1")
	}
	return validate.ListenAddr(field, addr) //nolint:wrapcheck // a FieldError under errs.Invalid
}

// reservedSetNames are refused as the name of a set in Options.Routes: the shell's own
// routes live at absolute paths under these names (decision 20).
var setNameRE = regexp.MustCompile(`^[a-z][a-z0-9]{1,31}$`)

var reservedSetNames = map[string]bool{"core": true, "modules": true, "admin": true}

func (o Options) validateRoutes(acc *validate.Errors) {
	seen := map[string]bool{}
	for i, set := range o.Routes {
		field := fmt.Sprintf("api.routes[%d]", i)
		switch {
		case !setNameRE.MatchString(set.Module):
			acc.Addf(field, validate.CodeMalformed, "set name %q must be 2 to 32 characters of a-z and 0-9, starting with a letter", set.Module)
		case reservedSetNames[set.Module]:
			acc.Addf(field, "reserved", "set name %q is reserved for the shell's own routes", set.Module)
		case seen[set.Module]:
			acc.Addf(field, "duplicate", "set name %q is used by more than one route set", set.Module)
		}
		seen[set.Module] = true
		for j, rt := range set.Routes {
			if err := rt.Validate(); err != nil {
				acc.Addf(field, validate.CodeMalformed, "set %q route %d: %s", set.Module, j, strings.TrimSpace(fieldSummary(err)))
			}
		}
		if len(set.Routes) > 0 && len(set.OpenAPI) == 0 {
			acc.Addf(field, validate.CodeRequired, "set %q has routes and no OpenAPI fragment", set.Module)
		}
	}
}

// fieldSummary lists the field names of an Invalid error, not their values.
func fieldSummary(err error) string {
	var names []string
	for _, fe := range validate.Fields(err) {
		names = append(names, fe.Field+" "+fe.Reason)
	}
	if len(names) == 0 {
		return "invalid"
	}
	return strings.Join(names, "; ")
}

var errNilContext = errors.New("authenticator returned a nil context")

func newInvalid(op, field, code, format string, args ...any) error {
	acc := validate.Errors{Op: op}
	acc.Addf(field, code, format, args...)
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

func conflict(op, format string, args ...any) error {
	return errs.E(op, errs.Conflict, fmt.Errorf(format, args...))
}
