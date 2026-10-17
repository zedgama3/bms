# api: the HTTP shell

- **Status:** Draft (2026-10-16)
- **Package:** `internal/api` (Go module `github.com/zedgama3/bms`), plus the foundation
  helper package `internal/lib/httpx` that this spec owns (decision 2): the wire form of an
  error, the kind-to-status table, and the three calls a handler makes (`ReadJSON`,
  `WriteJSON`, `Fail`). **UI:** none of its own; it serves the built web shell's files
  (`Options.UI`) and the endpoints the web shell reads.
- **Depends on specs:** `lib-errs.md` (Implemented): `errs.E`, `errs.Is`, `errs.KindOf`,
  `errs.Error` (`Op` for the log line), `Kind.String` (the body's `code`), every kind.
  `lib-core.md` (Implemented): `clock.Clock`, `validate.FieldError`, `validate.Fields`,
  `validate.Errors`, `validate.Range`, `validate.ListenAddr`, `validate.NonEmpty`.
  `model.md` (Approved): `model.PersonID` (the actor on a span and in an intake
  submission), `model.ParseID` (path IDs in the api's own handlers; the OpenAPI shape of
  an ID), `model.Sensitivity` and `model.TimeRange` (their OpenAPI shapes).
  `config.md` (Approved): `Options` mirrors `config.HTTP` minus `ShutdownTimeout`, which
  `app` uses (its decision 12 and Proposed change 5; decision 16 on TLS offloading).
  `module-contract.md` (Approved): `module.Route`, `module.Access`, `module.ModuleRoutes`
  (from `Set.Routes()`), `module.Graph`, `module.Node` (its Proposed change 6).
  `db.md` (Approved): `Pool.Ping`, `DB.ReadOnly`, `db.ErrReadOnly`, `db.Status` (its
  Proposed change 4). `telemetry.md` (Approved): `Tracer`, `Span`, `WithKind`, `WithAttrs`,
  `WithParentFrom`, `SpanServer`, `Token`, `Int`, `ID`, `Duration`, `Text`, `Meter`,
  `Histogram`, `WithUnit`, `RefFrom`, `TraceIDFrom`, `intake.Intake`, `intake.Batch`,
  `intake.Submission`, `intake.ErrRateLimited` (its Proposed change 3). `signals.md`
  (Approved): nothing imported; a request's span is the parent of the signal spans the
  request's commit causes (its decision 6). The leaf type `authz.Permission`
  (module-contract "Leaf types", `internal/core/authz`).
  **Used by:** `app` (`New`, `Listen`, `Serve`, `Shutdown`; builds `Options` from
  `config.HTTP`, the telemetry handles, `db`, `intake`, the module `Set` and the core route
  sets), `web-shell.md` (the wire forms: the problem body, `GET /api/modules`,
  `/api/openapi.json`, the `X-Read-Only` header, `POST /api/client-errors`), `identity.md`
  (implements `Authenticator`; reads `httpx.ClientAddrFrom`), `authz.md` (implements
  `PermissionChecker`; receives `PermissionModulesRead`), every core package and module
  with routes (`httpx.ReadJSON`, `WriteJSON`, `Fail`), the `hello` reference module,
  `frontend-implementer` (works from `/api/openapi.json`, 08-agent-workflow).

## Purpose

Expose the core's and the modules' routes over HTTP with one request pipeline.

## Responsibilities

- **Mount** every route set it is given (the modules' from `Set.Routes()`, the core's from
  `app`) under `/api/<name>/`, on `net/http`'s `ServeMux`, with exact-match patterns and a
  JSON 404 and 405 for everything else (decision 3).
- Run every request through one **middleware chain**: panic recovery, the peer check for a
  misrouted plaintext path (decision 9), the client address and effective scheme from
  trusted proxies (decision 8), security headers, the body cap, the request deadline,
  draining on shutdown; then, per route: the request span and duration histogram, the
  error mapper, the cookie guard, cross-origin protection for unsafe methods (decision 6),
  authentication (decision 5).
- **Map** every error a handler returns to one HTTP status and one problem body with the
  error reference, and write one log record per error response (decision 4).
- Serve the shell's own endpoints: `/healthz`, `/readyz`, `GET /api/modules`,
  `GET /api/admin/modules`, `POST /api/client-errors`, `GET /api/openapi.json`.
- **Assemble** the OpenAPI document from the api's base fragment and every route set's
  fragment, check at start-up that routes and fragments agree, and serve it (decision 11).
- Serve the built **web shell** (`Options.UI`): hashed assets cached for a year, everything
  else `index.html` (decision 12).
- Give handlers in any layer the three calls they need (`lib/httpx`): decode a JSON body,
  write a JSON response, hand an error to the shell's mapper (decision 2).

## Not responsible for

| Concern | Owner |
|---|---|
| Business logic of any kind; deciding what a request may do; checking a module's permission | the service behind each handler (`add-module`, 09 ground rules). The one permission this spec checks is `core.modules.read` on its own admin route. |
| Who the person is: cookies written at login, the signed session token and its per-account counter, logout, the superuser's short sessions, rate-limiting failed logins by address | `identity` (`identity.md`), which implements `Authenticator` (decision 5) and uses `httpx.ClientAddrFrom` for the login rate limit (config.md Proposed change 6). The cookie attributes are this spec's rule (decision 7); `identity` writes the cookie. |
| Permissions, roles, grants, separation of duties | `authz` (`authz.md`), which implements `PermissionChecker`. |
| Loading `http.*` from the environment; the bind rule F28 (a non-loopback `Addr` needs `TrustedProxies` or `AllowPlaintext`) | `config` (`config.md`, decision 16). This spec receives typed options from `app` and never imports `config` (layer rule 4, depguard rule `api`). |
| The shutdown deadline and the shutdown order | `app` (`app.md`): `Shutdown(ctx)` is called with `http.shutdown_timeout`, first in the order HTTP → `signals.Close` → `Set.Stop` → `db.Close` → `telemetry.Close` (signals.md Proposed change 5). |
| TLS | the front end (config.md decision 16). The binary serves plaintext to loopback, to a trusted proxy or, for development, to anyone (`AllowPlaintext`). |
| The logger, tracer, meter, the error reference, the client-error intake service, the privacy policy on attributes | `telemetry` (`telemetry.md`). This spec opens the request span, records the histogram, puts `RefFrom` in the body and calls `intake.Accept`; the intake route's body cap and status codes are here (telemetry decision 10). |
| Statement spans, pool gauges, read-only detection | `db` (`db.md`). `/readyz` calls `Ping` and `ReadOnly()`; the mapper recognises `ErrReadOnly`. |
| What a module's routes are, whether each is public, the fragment that documents them; rejecting a malformed route or an empty fragment | `module-contract.md` (`RouteProvider`, `APIDescriber`, `Build` F33 and F34). This spec mounts what `Set.Routes()` returns and checks only what the contract cannot: cross-set conflicts and route-versus-fragment agreement. |
| The module graph, the schema versions | `module.Plan.Graph()` and `db.Migrator.Status`, which `app` hands to this spec as `Options.Graph` and `Options.Schema`. |
| Catching errors in the browser, batching them, the `traceparent` header on calls, showing the reference, the read-only banner, translating `code` and field reasons, the TypeScript client generated from `/api/openapi.json` | `web-shell.md`. |
| The web shell's build, its hashed file names, its `index.html` | `web-shell.md`; `app` embeds the result and passes it as `Options.UI`. |
| Translating `validate.FieldError.Reason` | nobody yet (Open question 2). |
| File uploads and downloads, range requests, streaming | `files.md` (Later). The body cap and `WriteTimeout` here are sized for JSON; `files` proposes what it needs. |
| Trust between regionals: `/.well-known/` documents, signed records | Q-TRUST, later; such a route is a core route set like any other. |
| Audit of what a handler read or changed | `audit.md`, called by the handler's service. |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `httpx` (lib) | Give every handler, in any layer, one wire form for errors and one way to decode, encode and fail. | `Problem`, `StatusOf`, `Fail`, `ReadJSON`, `WriteJSON`, the ctx accessors for the client address and scheme. Imports `lib` only. |
| Router | Mount route sets under `/api/<name>/`, exactly, with JSON 404 and 405. | `net/http.ServeMux` (Go 1.22 patterns); conflicts across sets fail `New`. |
| Pipeline | Run every request through the same chain, outer layers before routing, inner layers per route. | Decisions 4–10; the order is in "The middleware chain". |
| Mapper | Turn the error a handler handed over into a status, a problem body, a span error and one log record. | The shell's `httpx.Failer`; recognises the four sentinels. |
| Shell routes | `/healthz`, `/readyz`, `GET /api/modules`, `GET /api/admin/modules`, `POST /api/client-errors`, `GET /api/openapi.json`. | The api's own route set, named `core`. |
| OpenAPI assembly | Merge the base fragment and every set's fragment into one 3.1 document; prove routes and fragments agree. | Decision 11; checked in `New`. |
| Static UI | Serve the built web shell with the right caching and headers, and `index.html` for every app path. | Decision 12. |
| `Server` | Listen, serve, drain and stop, for `app`. | `New` → `Listen` → `Serve` → `Shutdown`. |

## Interface

```go
// Package httpx is the HTTP vocabulary shared by the api shell and every handler in the
// core and the modules: the problem body, the kind-to-status table, and the three calls a
// handler makes. It imports the standard library, lib/errs and lib/validate only, so a
// module may use it without importing the api shell (layer rules 3 and 4).
package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/netip"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// ContentTypeProblem is the media type of every error response (RFC 9457).
const ContentTypeProblem = "application/problem+json"

// Problem is the body of every error response. It carries no prose: the web shell
// translates code (D28), and a field's reason is the validate.FieldError text (Open
// question 2). Nothing in it is personal data: reasons never echo input (lib-core).
type Problem struct {
	Status int     `json:"status"`           // the HTTP status, repeated for clients that lose it
	Code   string  `json:"code"`             // an errs.Kind name, or one of the shell codes (Behavior: "Codes")
	Ref    string  `json:"ref,omitempty"`    // telemetry.Ref "XXXX-XXXX"; absent when no trace
	Fields []Field `json:"fields,omitempty"` // for code "invalid": one per validate.FieldError, in order
}

// Field is one validation problem, from validate.FieldError.
type Field struct {
	Field  string `json:"field"`  // the dotted path the service used: "email", "shift.start"
	Reason string `json:"reason"` // the rule, never the value
}

// Shell codes that are not errs.Kind names. Codes are stable API strings pinned by a test.
const (
	CodeReadOnly             = "read_only"              // 503: db.ErrReadOnly
	CodeRateLimited          = "rate_limited"           // 429: intake.ErrRateLimited (and later limiters)
	CodeShuttingDown         = "shutting_down"          // 503: canceled by shutdown, client still connected
	CodePayloadTooLarge      = "payload_too_large"      // 413: *http.MaxBytesError
	CodeUnsupportedMediaType = "unsupported_media_type" // 415: ErrUnsupportedMediaType
	CodeCrossOrigin          = "cross_origin"           // 403: an unsafe request from another origin
	CodeMisdirected          = "misdirected"            // 421: a direct request from an untrusted peer
	CodeMethodNotAllowed     = "method_not_allowed"     // 405: the path exists, the method does not
	CodeNotFound             = "not_found"              // 404: also errs.NotFound
)

// ErrUnsupportedMediaType is wrapped (errs.Invalid) by ReadJSON when the request's
// Content-Type is not JSON; the mapper answers 415. Check it with errors.Is before the kind.
var ErrUnsupportedMediaType = errors.New("request body is not application/json")

// StatusOf is the one kind-to-status table (go-conventions "Errors"): Invalid 400,
// Unauthenticated 401, Permission 403, NotFound 404, Conflict 409, Unavailable 503,
// Timeout 504, Canceled 503, Unsupported 501, Internal 500; a value outside the set is
// 500. Cannot fail. Consumers: the api mapper; Fail's fallback; tests asserting a status.
func StatusOf(k errs.Kind) int

// Failer is what the api shell installs in every request's ctx (WithFailer) so that Fail
// reaches the shell's mapper, which adds the reference, recognises the sentinels, marks the
// span and writes the log record. Modules never implement it.
type Failer interface {
	Fail(w http.ResponseWriter, r *http.Request, err error)
}

// WithFailer returns ctx carrying f. Consumer: the api shell. Cannot fail.
func WithFailer(ctx context.Context, f Failer) context.Context

// Fail answers r with the error response for err and returns; the handler returns right
// after it. With a Failer in r.Context() (every request through the api shell) it
// delegates to it. Without one (a handler under test), it writes a Problem with
// StatusOf(errs.KindOf(err)), the kind's name as code, Fields from validate.Fields, no
// ref, plus 413 for a *http.MaxBytesError and 415 for ErrUnsupportedMediaType, so a
// module's unit test sees the same status the shell would send. A nil err is a caller bug
// and is written as 500 internal. Call it before writing anything to w. Cannot fail.
// Consumers: every handler in core packages and modules; the api's own handlers.
func Fail(w http.ResponseWriter, r *http.Request, err error)

// ReadJSON decodes r's body into dst. The body is already capped by the shell
// (Options.MaxBodyBytes); a handler that wants a tighter cap wraps r.Body with
// http.MaxBytesReader first. Content-Type must be application/json (parameters allowed);
// otherwise it fails with errs.Invalid wrapping ErrUnsupportedMediaType (F1). Malformed
// JSON, a wrong type, or trailing data fails with errs.Invalid wrapping a
// validate.FieldError whose Field is "body" (or the JSON path the decoder reports) and
// whose Reason never echoes the body (F2). An empty body is Invalid with field "body",
// reason "required" (F3). Unknown fields are ignored (decision 13). A *http.MaxBytesError
// from the capped body is returned unchanged so the mapper answers 413 (F4). ctx done
// fails with Canceled or Timeout (F5). dst must be a non-nil pointer (F6, Internal).
// Consumers: every handler that takes a body.
func ReadJSON(r *http.Request, dst any) error

// WriteJSON writes v as JSON with status and Content-Type application/json; the shell has
// already set Cache-Control: no-store on API responses. It fails with errs.Internal if v
// cannot be encoded (nothing is written then, F7) and with the kind of the write error
// when the client is gone (F8: Canceled; the caller logs nothing, the shell already did).
// A nil v with status 204 writes no body. Consumers: every handler that returns a body.
func WriteJSON(w http.ResponseWriter, status int, v any) error

// ClientAddrFrom returns the client address the shell derived for this request (decision
// 8) and false when ctx carries none (a handler run outside the shell). It is personal
// data: use it for a rate limit, never log it. Consumers: identity (failed-login limits),
// the api's intake handler. Cannot fail.
func ClientAddrFrom(ctx context.Context) (netip.Addr, bool)

// SchemeFrom returns the effective scheme, "http" or "https", the shell derived (decision
// 8), and false when ctx carries none. Consumers: identity (absolute links in e-mails are
// settings' public URL, not this; identity uses it only to decide whether a cookie may
// lack Secure under Open question 1). Cannot fail.
func SchemeFrom(ctx context.Context) (string, bool)

// WithClientAddr and WithScheme set the two values. Consumers: the api shell; tests of a
// handler that reads them. Cannot fail.
func WithClientAddr(ctx context.Context, a netip.Addr) context.Context
func WithScheme(ctx context.Context, scheme string) context.Context
```

```go
// Package api is the HTTP shell: it mounts the routes it is given, runs every request
// through one pipeline, maps errors, serves the shell's own endpoints, assembles the
// OpenAPI document and serves the web shell. It imports core contracts, model and lib,
// never a module, app or config (layer rule 4; depguard rule api).
package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/core/telemetry/intake"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/model"
)

// ---- Seams other core packages fill ----

// Authenticator resolves the session a request carries. identity implements it (M1);
// app passes it in Options. The shell calls it once per routed request, for public and
// signed-in routes alike, so the actor is on the span either way.
type Authenticator interface {
	// Authenticate reads r (its cookie or header, identity's choice) and returns a ctx
	// carrying the principal in the form identity defines, the person behind it, and nil.
	// No session: (ctx, zero, nil). A session that is present but invalid, expired or
	// revoked: errs.Unauthenticated. A dependency failure: its kind (Unavailable,
	// Timeout). It must not write to the database (09 ground rules: sessions work on a
	// read-only copy) and must not write to w; the shell never passes one.
	Authenticate(ctx context.Context, r *http.Request) (context.Context, model.PersonID, error)
}

// PermissionChecker answers whether the actor in ctx holds a site-wide permission. authz
// implements it (M2) with its Authorizer; app passes it in Options. Until then it is nil
// and every permissioned shell route answers 403 (decision 14).
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

// PermissionModulesRead guards GET /api/admin/modules (module-contract "Permissions").
// It is declared here, where it is checked; app hands it to authz with the core's other
// declarations (Proposed change 10). Scope site; reaches internal.
const PermissionModulesRead authz.Permission = "core.modules.read"

// ---- Options and Server ----

// Options is what New needs. app copies config.HTTP into the first five fields
// (config.md decision 12) and supplies the rest.
type Options struct {
	// Addr is the listen address, as config.md validated it ("host:port" or ":port").
	Addr string
	// RequestTimeout is the deadline of one request. 1s..10m.
	RequestTimeout time.Duration
	// MaxBodyBytes caps every request body. 1024..1073741824.
	MaxBodyBytes int64
	// TrustedProxies are the peers whose X-Forwarded-For and X-Forwarded-Proto are believed
	// (decision 8) and, when non-empty, the only non-loopback peers served (decision 9).
	TrustedProxies []netip.Prefix
	// AllowPlaintext serves any peer over plain HTTP, for development; New logs one
	// warning when it is true.
	AllowPlaintext bool

	Logger *slog.Logger     // component=api; never nil
	Tracer telemetry.Tracer // scope "api"; never nil
	Meter  telemetry.Meter  // scope "api"; never nil
	Clock  clock.Clock      // durations, the 421 log rate; never nil
	// Version is the build's version, for the OpenAPI info block and /healthz. A token
	// of at most 64 bytes (telemetry.Options.Version has the same rule).
	Version string

	// Routes are the route sets to mount: every module's (module.Set.Routes()) and the
	// core's (app builds a module.ModuleRoutes per core package with routes: Module is the
	// core area name, which is a reserved module name, so it cannot collide). May be
	// empty. Each set's OpenAPI fragment is required when it has routes (decision 11).
	Routes []module.ModuleRoutes

	Database Database        // never nil
	Intake   *intake.Intake  // never nil
	Graph    module.Graph    // from Plan.Graph(); serializable
	Schema   SchemaStatus    // never nil

	// Authenticator is nil in M0: every signed-in route answers 401 (decision 5).
	Authenticator Authenticator
	// Permissions is nil until M2: every permissioned shell route answers 403 (decision 14).
	Permissions PermissionChecker

	// UI is the built web shell (index.html at its root, hashed files under assets/).
	// nil means no UI: paths outside /api, /healthz and /readyz answer 404 (decision 12).
	UI fs.FS
	// ContentSecurityPolicy is sent with every HTML response. Empty means DefaultCSP.
	ContentSecurityPolicy string
}

// DefaultCSP is the policy sent with index.html when Options.ContentSecurityPolicy is
// empty: same origin for everything, data: images, no frames, no inline scripts.
// web-shell.md proposes changes if its build needs them.
const DefaultCSP = "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

// Validate fails with errs.Invalid joining one validate.FieldError per problem, fields
// "http.addr", "http.request_timeout", "http.max_body_bytes", "http.trusted_proxies",
// "api.logger", "api.tracer", "api.meter", "api.clock", "api.version", "api.database",
// "api.intake", "api.schema", "api.routes[i]" (F9).
func (o Options) Validate() error

// New validates opts, builds the pipeline and the router, mounts every route set (F10–F13),
// assembles and checks the OpenAPI document (F14–F17), creates the request-duration
// histogram (F18) and returns a Server that is not yet listening. It does no network I/O.
// Consumers: app (once, in `serve`); tests (through Handler).
func New(opts Options) (*Server, error)

// Server is the process's HTTP server. Safe for concurrent use from New until Shutdown
// returns.
type Server struct{ /* unexported: http.Server, mux, opts, histogram, openapi []byte, draining atomic.Bool, baseCancel */ }

// Handler returns the complete handler (pipeline, router, shell routes, UI), for
// httptest in tests and for app if it ever wraps it. Cannot fail.
func (s *Server) Handler() http.Handler

// OpenAPI returns the assembled document as JSON bytes, the same bytes
// GET /api/openapi.json serves. Cannot fail: New built it. Consumers: app's `bms check`
// (prints the operation count), the docs build, tests.
func (s *Server) OpenAPI() []byte

// Listen binds Options.Addr and returns the bound address (so tests may bind ":0").
// Fails with errs.Unavailable when the address is in use or cannot be bound (F19),
// errs.Invalid when the address is malformed (F20), errs.Conflict when already listening
// (F21). Consumer: app, before Serve.
func (s *Server) Listen(ctx context.Context) (net.Addr, error)

// Serve accepts connections on the listener until Shutdown is called, then returns nil.
// It fails with errs.Internal if Listen was not called (F22) and with errs.Unavailable
// wrapping the server's error if accepting fails for any reason other than Shutdown
// (F23). Consumer: app, in its own goroutine.
func (s *Server) Serve() error

// Shutdown drains: /readyz answers 503 at once, new connections are refused, in-flight
// requests get Connection: close and run to completion within ctx; when ctx ends first,
// the base context is canceled so stuck handlers fail with Canceled (mapped to 503
// shutting_down) and Shutdown returns errs.Timeout or errs.Canceled (F24). Idempotent: a
// second call returns nil. Consumer: app, first in the shutdown order.
func (s *Server) Shutdown(ctx context.Context) error
```

### Who needs each member

| Member | Consumer(s) | Pre-condition | Post-condition |
|---|---|---|---|
| `httpx.Problem`, `Field`, the codes, `ContentTypeProblem` | the mapper; `web-shell` (mirrors them in TypeScript); tests | — | one wire form for every error |
| `httpx.StatusOf` | the mapper; `Fail`'s fallback; every error-path test that asserts a status | — | the go-conventions table, in one place |
| `httpx.Failer`, `WithFailer` | the shell (installs per route) | — | `Fail` reaches the mapper |
| `httpx.Fail` | every handler in `identity`, `people`, `departments-events`, `authz`, `audit`, `settings`, every module (`http.go`), the `hello` module, the api's own handlers | called before any write | the response is written exactly once |
| `httpx.ReadJSON`, `WriteJSON` | the same handlers | body capped by the shell | decoded or a field error; encoded or nothing |
| `httpx.ClientAddrFrom`, `SchemeFrom` | `identity` (login rate limit; Open question 1), the intake handler | inside the shell | the derived values, never the raw header |
| `httpx.WithClientAddr`, `WithScheme` | the shell; handler tests | — | — |
| `Authenticator` | implemented by `identity`; called by the shell per routed request | — | the actor in ctx, or 401 |
| `PermissionChecker`, `PermissionModulesRead` | implemented by `authz`; `app` registers the permission; the admin modules handler | an actor in ctx | allowed or `Permission` |
| `Database` | `/readyz`; the `X-Read-Only` header | opened pool | reachable; mode |
| `SchemaStatus` | `GET /api/admin/modules` | — | versions per set |
| `Options`, `Validate`, `New` | `app` (`serve`); tests | typed options | a server with everything mounted and checked |
| `Handler` | tests (`httptest.NewServer`, `httptest.NewRecorder`); `app` | — | — |
| `OpenAPI` | `GET /api/openapi.json`; `bms check`; the web shell's client generation (build time) | — | one JSON document |
| `Listen`, `Serve`, `Shutdown` | `app` | — | bound; serving; drained |

## Types

Defines `Options`, `Server`, `Authenticator`, `PermissionChecker`, `Database`,
`SchemaStatus`, `PermissionModulesRead`, `DefaultCSP`; in `httpx`: `Problem`, `Field`,
the codes, `ErrUnsupportedMediaType`, `Failer`. Uses `module.Route`, `module.Access`,
`module.ModuleRoutes`, `module.Graph`, `db.Status`, `db.ErrReadOnly`, `intake.Intake`,
`intake.Batch`, `intake.Submission`, `intake.ErrRateLimited`, `telemetry.Tracer`,
`telemetry.Meter`, `telemetry.Histogram`, `telemetry.Ref`, `authz.Permission`,
`model.PersonID`, `clock.Clock`, `validate.FieldError`, `netip.Prefix`, `netip.Addr`,
`fs.FS`, `net.Addr`, `slog.Logger`.

### The response shapes of the shell's routes

| Route | Access | Response |
|---|---|---|
| `GET /healthz` | public | `200 {"status":"ok","version":"<Version>"}` always; `503 {"status":"draining"}` after `Shutdown` began |
| `GET /readyz` | public | `200 {"database":"ok","mode":"read-write"}` or `"read-only"` (db.md Proposed change 4); `503 {"database":"unavailable"}` when `Ping` fails (its kind in the log, never in the body); `503 {"status":"draining"}` after `Shutdown` began |
| `GET /api/modules` | public | `{"modules":[{"name":"hello","enabled":true}, ...]}`: every compiled-in module's name and whether it is enabled, nothing else (decision 15) |
| `GET /api/admin/modules` | signed in, `core.modules.read` | `{"graph": <module.Graph as JSON>, "schema": [{"set":"hello","built":1,"applied":1,"current":true}, ...]}`; a set's `current` is `Status.Err == nil`; the error text is not sent (it is in `bms check`) |
| `POST /api/client-errors` | public | `202` no body; `400` problem with fields; `415`; `429` with `Retry-After: 60` (telemetry Proposed change 3) |
| `GET /api/openapi.json` | public | the assembled document, `application/json`, `ETag` (SHA-256 of the bytes), `Cache-Control: no-cache` |
| `GET /{path...}` (not `/api/`, `/healthz`, `/readyz`) | public | a file from `Options.UI`, or `index.html` (decision 12); `404` problem when `UI` is nil |

IDs in these bodies are canonical UUID strings (`model.ID.MarshalText`). The admin graph
carries module names, permission names, setting keys and signal names: none of it is
personal data.

## Data

No tables, no files written. What a request leaves behind, by field, with its sensitivity
and where it goes (privacy-data; telemetry.md "Data" holds the retention rules):

| Field | Where it goes | Level | Why | Retention |
|---|---|---|---|---|
| client address (derived, decision 8) | the request ctx only (`httpx.ClientAddrFrom`); `intake.Submission.From` (memory bucket, never logged); the login rate limit (`identity`) | personal | rate limits | gone when the request ends |
| peer network prefix (`/24`, `/64`) of a misdirected request | one warn log line per prefix per hour (decision 9) | internal (a network, not a device) | an operator fixes `trusted_proxies` | the collector's |
| `person_id` | the request span and the per-error log record | internal (opaque ID) | whose request failed | the collector's |
| `http.request.method`, `http.route` (pattern), status, `duration_ms`, `error.kind`, `op`, `error_ref`, `trace_id` | span, histogram (method, route, status only), the per-error log record | not about a person | find and count failures | the collector's |
| `stack` | the log record of a recovered panic, capped by the telemetry handler | not about a person | fix the bug | the collector's |
| `User-Agent` | passed to `intake.Accept` only, capped there | internal | browser-specific crashes (telemetry decision 17) | the collector's |

Never emitted by this package: query strings, request or response bodies, headers other
than the method and the route pattern, cookies, session tokens, `Authorization`, the full
URL, the `Host` header, the whole client address (telemetry Proposed change 3). The
error body carries `code`, `ref` and `fields`: never `err.Error()`, which may name hosts
or SQL (lib-errs Proposed change 1).

API responses are sent with `Cache-Control: no-store` (except `/api/openapi.json` and
`/api/modules`, decision 12), so a shared cache or the browser's disk never keeps a body
that may hold personal data.

## Permissions

| Operation | Permission | Scoped by | Default roles | Notes |
|---|---|---|---|---|
| `GET /api/admin/modules` | `core.modules.read` (`PermissionModulesRead`, scope site, reaches internal) | site | every site-admin starter role (09 §4; module-contract "Permissions") | checked through `Options.Permissions.Allow`; nil checker → 403 (decision 14) |
| `GET /api/modules` | none (public) | — | anyone | enabled names only (decision 15) |
| `POST /api/client-errors` | none (public) | — | anyone | protected by intake's limits and the 64 KiB cap (telemetry decision 9) |
| `GET /api/openapi.json`, `/healthz`, `/readyz`, the UI files | none (public) | — | anyone | nothing personal in them |
| every mounted route | its `module.Route.Access` decides whether a session is required; the permission is the service's (09 ground rules) | — | — | the shell never checks a module's permission |

Separation of duties (D23) is `authz`'s; this spec adds no role and no all-access path.
The superuser is an ordinary actor to the shell (its banner is the web shell's, its audit
mark is `audit`'s).

## Signals

None published, none subscribed. The request span this spec opens is the parent of the
`signal:` and `deliver:` spans a request's commit causes (signals.md decision 6), which is
how a maintainer sees a request and its reactions under one trace ID.

## Settings

None. Everything here is process configuration by config.md's rule (needed to start
listening, or a security boundary): `http.addr`, `http.request_timeout`,
`http.max_body_bytes`, `http.trusted_proxies`, `http.allow_plaintext`, each with its
default and range in config.md's catalogue. Nothing in this spec behaves differently per
regional; the one future regional value, the public URL (settings.md, D18), becomes a
trusted origin for decision 6 when settings exist (Proposed change 15). Constants inside
the package, pinned by tests, with the reason each is not a key:

| Constant | Value | Why not configurable |
|---|---|---|
| `ReadHeaderTimeout` | 10 s | slowloris protection; no operator wants it longer |
| `IdleTimeout` | 120 s | keep-alive housekeeping |
| `WriteTimeout` | `RequestTimeout + 5 s` | a backstop behind the ctx deadline; `files.md` revisits it for downloads |
| `MaxHeaderBytes` | 64 KiB | `net/http`'s default would allow 1 MiB of headers |
| readiness ping deadline | 2 s | a probe must answer fast; `db.Ping` refreshes the read-only state in the same round trip |
| intake body cap | 64 KiB | telemetry Proposed change 3 |
| HSTS | `max-age=31536000` (Open question 3 on `includeSubDomains`) | one year is the common value; the front end may send its own |
| misdirected-request log rate | once per peer prefix per hour | enough to notice, not enough to flood |
| UI asset cache | `public, max-age=31536000, immutable` under `/assets/`; `no-cache` elsewhere | hashed names make it safe |
| `DefaultCSP` | above | web-shell.md proposes changes; the regional's branding (a logo) is served from the same origin |

## UI

None of its own. What this spec provides for `web-shell.md`:

- **Error handling:** every error is `application/problem+json` with `code`, `ref` and
  `fields`; the shell translates `code` (keys `error.<code>`), shows `ref` with the
  wording in telemetry.md "UI" for every 5xx and every 4xx but `invalid`, and maps
  `fields[].field` to form fields. `401` on a signed-in route sends the shell to the
  sign-in page; `403 cross_origin` is a shell bug (its calls are same-origin); `413` and
  `415` are shell bugs too.
- **Read-only banner:** the `X-Read-Only: true` header on every response from a read-only
  copy (decision 16), so the banner appears on the next call without polling; `/readyz`
  gives the same answer on demand.
- **Module registry:** `GET /api/modules` lists the enabled module names before and after
  sign-in (module-contract "UI"; decision 15). The admin "modules" screen reads
  `GET /api/admin/modules`; its forbidden state is a `403`.
- **The API client:** generated from `/api/openapi.json` at build time (the TypeScript
  tooling is `web-shell.md`'s); every call carries a `traceparent` (telemetry Proposed
  change 4); the response's `X-Trace-Id` is the same ID.
- **Static files:** the shell is built with hashed asset names under `assets/` and a root
  `index.html`; app paths are served `index.html`; the CSP forbids inline scripts.

## Behavior

### Decisions settled here, with reasons

1. **`net/http.ServeMux` is the router; no third-party router.** Go 1.22's patterns give
   methods, path values (`{id}`), exact matches (`{$}`) and wildcards (`{path...}`), and
   module-contract already fixed `Route.Pattern` in that syntax. A third-party router
   would be the first HTTP dependency for features nobody listed. What `ServeMux` lacks
   (JSON 404 and 405, conflict errors instead of panics) costs forty lines (decision 3).
2. **The error wire form, the status table and the handler helpers live in `lib/httpx`,
   not in `api`.** Module handlers are plain `http.Handler`s (module-contract decision 13)
   and modules may not import `api` (layer rule 3), yet every handler must produce the
   same error body with the same status. The table itself depends only on `errs` and
   `validate`, so it belongs in `lib`; what only the shell knows (the reference, the
   sentinels from `db` and `intake`, the span, the log line) stays in the shell's mapper,
   which `Fail` reaches through a `Failer` in the request ctx. Without a `Failer` (a
   handler under test) `Fail` falls back to the same table, so a module's unit test
   asserts `404` without the shell. go-conventions' "done only in `api`'s ErrorMapper"
   becomes "the table is `httpx.StatusOf`; the shell's mapper is the only writer on a
   real request" (Proposed change 2). Rejected: a `func(w, r) error` handler type (would
   change the Approved `Route.Handler`); an error body written by each module (drift).
3. **Mounting: `<METHOD> /api/<name><pattern>`, trailing slashes made exact, JSON 404 and
   405 through two fallback registrations, conflicts caught at `New`.** A pattern ending
   in `/` is a subtree in `ServeMux`, so a module route `/` would swallow `/api/<name>/…`;
   the shell appends `{$}` to every pattern ending in `/`, so no module can register a
   catch-all (a module that wants a wildcard says `{path...}`). For each distinct path the
   shell also registers a method-less pattern whose handler answers `405` with `Allow`
   (`ServeMux` prefers the method-specific pattern, so real routes are unaffected), and a
   `/api/` subtree handler answers `404 not_found`; both are JSON problems, where
   `ServeMux`'s own answers are plain text. `ServeMux.Handle` panics on a conflicting
   pattern; `New` recovers that panic and returns `errs.Conflict` naming both sets and
   patterns (F12), so a bad set stops start-up with a message, not a stack trace.
4. **One problem body (RFC 9457), no prose, one log line per error response, nothing for
   a success.** `application/problem+json` is what HTTP tooling and generated clients
   already understand; `code` is machine-readable (an `errs.Kind` name or a shell code),
   the web shell translates it (D28), and `err.Error()` never reaches a client (lib-errs
   decision 8). The log record (error for 5xx, warn for 4xx, debug for an unmatched route
   and for a `401` with no session at all) carries `error_ref`, `trace_id`, `error.kind`,
   `op`, `http.route`, `http.request.method`, `status`, `person_id`, `duration_ms`, so a
   reference is a one-field Loki query (telemetry decision 3). Successful requests leave
   a span and a histogram sample and no log line: a regional's logs stay readable.
5. **Authentication is a seam (`Authenticator`), called on every routed request, enforced
   by `Route.Access`.** `identity` owns sessions (M1) and does not exist in M0; the shell
   needs only "who is this, if anyone" to put `person_id` on the span, to fill
   `intake.Submission.Person` and to answer `401` on a signed-in route. Calling it on
   public routes too means a signed-in person's public-route requests are attributed. On a
   public route an invalid session is treated as no session (a stale cookie must not
   break the sign-in page or error reporting); on a signed-in route it is `401`. A nil
   `Authenticator` (M0) makes every signed-in route `401` and every public route
   anonymous, which is correct for a build with no login. The principal's ctx form is
   `identity`'s, so services read the actor through `identity`, never through `api`.
6. **Cross-site request forgery is stopped by `net/http.CrossOriginProtection` plus
   `SameSite=Lax` cookies; there is no CSRF token and no CSRF cookie.** The standard
   library's protection (Go 1.25) rejects an unsafe-method request whose `Sec-Fetch-Site`
   is cross-site, or whose `Origin` host differs from the request `Host`, and lets
   through requests that carry neither header (non-browser clients such as `curl` with a
   cookie). Every modern browser sends `Sec-Fetch-Site`; the SPA's calls are same-origin;
   and `SameSite=Lax` already keeps the cookie off cross-site `POST`s. A token scheme would
   add a cookie, a header, a rotation rule and a failure mode for nothing. The cost is one
   deployment rule: the front end must preserve the `Host` header (Caddy does by default;
   the nginx example sets it; Proposed change 14); a public URL on another host becomes a
   trusted origin when `settings` exists (Proposed change 15). A rejected request is
   `403 cross_origin`, logged at warn without the origin value (it may carry anything).
   config.md's proposal said "session and CSRF cookies"; with this decision there is only
   the session cookie (Proposed change 4).
7. **Cookies are `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, and the shell refuses a
   response that sets one otherwise.** config.md decision 16(d) makes the attributes a
   rule; a rule nobody checks drifts. The cookie guard inspects `Set-Cookie` before the
   header is written and, if a cookie lacks any attribute, answers `500 internal` and logs
   the cookie's name (never its value): a developer finds out in the first test. Rewriting
   the cookie silently was rejected (it hides the bug). Open question 1 asks whether
   `Secure` may be dropped in the one case where it makes login impossible.
8. **The client address and scheme come from `X-Forwarded-For` and `X-Forwarded-Proto`
   only when the peer is in `TrustedProxies`, by the rightmost-untrusted rule; otherwise
   from the connection.** Walking `X-Forwarded-For` from the right and skipping trusted
   addresses yields the first address a trusted hop saw, which a client cannot forge by
   prepending. From any other peer the headers are ignored entirely, never partially
   trusted (config.md decision 16(b)). The scheme is `X-Forwarded-Proto` from a trusted
   peer, else `http` (the binary never terminates TLS). `Forwarded` (RFC 7239) is not
   parsed: no front end in `deploy/` emits it by default, and two header grammars are two
   sets of bugs. The derived values travel in ctx (`httpx.ClientAddrFrom`, `SchemeFrom`)
   so `identity` and `intake` never read the raw headers.
9. **A direct request from a peer that is neither loopback nor in a non-empty
   `TrustedProxies` is answered `421 Misdirected Request` with a problem body and logged
   once per peer prefix per hour; `/healthz` and `/readyz` are exempt; `AllowPlaintext`
   turns the check off.** config.md decision 16 and its Proposed change 5 asked for this
   decision. Serving such a request would serve plaintext on a network path the operator
   did not declare; refusing it makes a misrouted path visible without serving on it. The
   health routes stay reachable so an orchestrator's probe from a node address works
   without being added as a "proxy". The log carries the peer's `/24` or `/64`, enough to
   fix the configuration and not a device identifier (privacy-data). With
   `TrustedProxies` empty, `config` already forces a loopback bind, so every peer is
   loopback and the rule has nothing to do.
10. **The request span is opened per route, after routing, so it can be named
    `<METHOD> <route pattern>` at `Start`.** telemetry's `Span` has no rename method
    (deliberately: a name set late escapes the grammar check), and the pattern is known
    only once `ServeMux` chose a handler. So the outer layers (recovery, peer check,
    address, headers, body cap, deadline, draining) run before the mux, and the span,
    histogram, mapper, cookie guard, cross-origin check and authentication are wrapped
    around each registered handler, where the pattern is a constant. Unmatched requests
    get the route token `/{unmatched}` (one series, not one per probed path) and the
    `405` fallback the real path pattern. `/healthz` and `/readyz` get no span and no
    histogram sample: a probe every ten seconds would be most of a quiet regional's
    traces. telemetry Proposed change 3 is answered with this refinement (Proposed
    change 6).
11. **OpenAPI 3.1 as JSON; fragments are relative; routes and fragments must agree at
    start-up; a set with routes must have a fragment.** JSON because the standard library
    reads it and the document is served as JSON anyway (a YAML parser would be a
    dependency for authoring comfort). A fragment is one JSON object with optional
    `paths` (keys relative to `/api/<name>`, beginning with `/`), `components.schemas`,
    and `tags`; the shell prefixes the paths, merges the schemas (a duplicate schema
    name across sets is `Conflict`, naming both sets; the convention is to prefix schema
    names with the module name), merges the tags, adds the base fragment (`Problem`,
    `Field`, `ID` as `string`/`format: uuid`, `Sensitivity` as the four-name enum,
    `TimeRange` as `{start, end}` of `date-time`; the shell's own routes), adds a
    `default` response referencing `Problem` to every operation that lacks one, and
    writes `openapi`, `info` (title `BMS API`, `version` = `Options.Version`). The check:
    every route in a set has an operation at its method and path and every operation has
    a route, else `Invalid` naming the first mismatch (F15); every local `$ref` resolves
    (F16). module-contract made `APIDescriber` optional; the web shell's screens are built
    from this document (08-agent-workflow), so a route without documentation is a route
    the front end cannot call correctly. This spec requires the fragment when a set has
    routes (F14) and proposes the contract say so (Proposed change 3). A full schema
    validator is not linked in: the structural check here plus a lint of
    `/api/openapi.json` in the web tooling (Proposed change 12) cover it.
12. **The shell serves the web UI: hashed assets immutable, `index.html` for every app
    path, `no-store` on the API.** D15 says the binary serves the UI; a second server or
    a second listener would be more to operate. Paths outside `/api/`, `/healthz` and
    `/readyz` are served from `Options.UI`: a file that exists is sent with
    `Cache-Control: public, max-age=31536000, immutable` under `/assets/` (Vite hashes
    those names) or `no-cache` otherwise; a path with no file and no extension is the
    SPA's and gets `index.html` with `no-cache` and the CSP; a path with an extension and
    no file is `404`. Every `/api/` response carries `Cache-Control: no-store`, except
    `/api/openapi.json` (`no-cache` with an `ETag`) and `/api/modules` (`no-cache`), which
    hold nothing personal. With `UI` nil (a headless build or a test) non-API paths are
    `404`.
13. **Unknown JSON fields are ignored.** telemetry decision 11 set it for intake ("an
    older server accepts a newer shell"); the same rule everywhere lets the web shell
    ship a field before the server reads it. A handler that must reject unknown fields
    (none known) decodes itself.
14. **A permissioned shell route with no `PermissionChecker` answers `403`.** Until `authz`
    exists (M2) nobody holds `core.modules.read`, and the safe reading of "no authorizer"
    is "nobody is allowed", never "everybody". The graph is still visible in `bms check`
    and, by name only, in `GET /api/modules`.
15. **`GET /api/modules` is public and lists names and enablement only; the graph and the
    schema versions are behind `core.modules.read` on `GET /api/admin/modules`.** The web
    shell must know which modules are enabled before anyone signs in (it mounts a
    module's screens only if the module is enabled, module-contract "UI"; in M0 there is
    no sign-in at all). Enabled names are not secret: the JavaScript bundle names every
    compiled-in module and their routes answer. The graph (permissions, settings keys,
    signals, warnings) and the schema state are an admin's view (09 §7). Two routes keep
    each one simple; the owner is asked to confirm the public half (Open question 4).
16. **Writes are not refused by the shell on a read-only copy; the mapper names the
    refusal, and every response says the mode.** Not every unsafe-method request writes
    the database: sign-in, sign-out, error reports and searches must work on the on-site
    copy (09 ground rules: sessions need no write). The write that cannot happen fails in
    `db` with `ErrReadOnly`, and the mapper answers `503 read_only` (db.md Proposed change
    4) so the web shell shows "this copy is read-only" instead of an error page. The
    `X-Read-Only: true` header on every response lets the banner appear at once.
17. **`Canceled` is answered as lib-errs proposed: nothing when the client left, `503
    shutting_down` with `Connection: close` when shutdown canceled the work.** The two
    cases are told apart by the server's draining flag: `Shutdown` lets in-flight
    requests finish and cancels the base context only when its deadline passes, so a
    request that sees `Canceled` while draining was cut off by the server. When the client
    is gone there is nobody to answer; the span is not marked failed (telemetry
    `RecordError` on `Canceled`) and one debug line is written. lib-errs Open question 1
    is answered (Proposed change 7).
18. **The request deadline is the ctx; the server's `WriteTimeout` is a backstop.**
    `http.TimeoutHandler` was rejected: it buffers every response and answers `503` with
    its own body. Instead `RequestTimeout` becomes the request ctx's deadline, so every
    I/O under it fails with `Timeout` and is mapped to `504`; `WriteTimeout` five seconds
    later ends a handler that ignored its ctx. `ReadHeaderTimeout` protects the accept
    loop; `ReadTimeout` is not set (the body cap and the deadline bound the body).
19. **Every response carries `X-Trace-Id`.** telemetry decision 2 allowed it; it makes a
    `curl` session traceable without reading a body, and the ID is not a secret.

### The middleware chain

Outer layers, before routing, in order:

1. **Recovery (outer).** A panic anywhere below becomes `500 internal` with a problem
   body if nothing was written, and an error log with `stack`; the process never dies for
   one request (F25).
2. **Peer check.** `TrustedProxies` non-empty, `!AllowPlaintext`, peer not loopback and
   not in the list, path not `/healthz` or `/readyz` → `421 misdirected` (decision 9,
   F26).
3. **Client address and scheme** derived (decision 8) and put in ctx.
4. **Headers:** `X-Content-Type-Options: nosniff`, `Referrer-Policy:
   strict-origin-when-cross-origin`, `X-Frame-Options: DENY`, `Permissions-Policy:
   camera=(), microphone=(), geolocation=()`; `Strict-Transport-Security` when the
   scheme is `https`; `Cache-Control: no-store` for `/api/` paths (decision 12);
   `X-Read-Only: true` when `Database.ReadOnly()`.
5. **Body cap:** `r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)` (F27).
6. **Deadline:** `ctx, cancel := context.WithTimeout(r.Context(), RequestTimeout)` (F28).
7. **Draining:** after `Shutdown` began, `Connection: close` on the response.

Then `ServeMux`. Per registered route (every mounted route, the shell's routes, the 404
and 405 fallbacks; not the health routes, not UI files), in order:

8. **Span and histogram:** `Tracer.Start(ctx, "<METHOD> <pattern>", WithKind(SpanServer),
   WithParentFrom(r.Header), WithAttrs(Token("http.request.method", m),
   Token("http.route", pattern)))`; `X-Trace-Id` from `TraceIDFrom`; on return,
   `SetAttrs(Int("http.response.status_code", st), Duration("duration_ms", d))`,
   `Histogram.Record(seconds, method, route, status)`, `End`.
9. **Mapper installed:** `httpx.WithFailer(ctx, mapper)`; the response writer is wrapped
   to record the status and whether the header was written.
10. **Recovery (inner):** a panic in a handler is recorded on the span (`RecordError`) and
    mapped as in step 1 (F25).
11. **Cookie guard:** `Set-Cookie` checked at `WriteHeader` (decision 7, F29).
12. **Cross-origin protection** for `POST`, `PUT`, `PATCH`, `DELETE` (decision 6, F30).
13. **Authentication:** `Authenticator.Authenticate`; the actor's `person_id` on the span;
    `AccessSignedIn` without a valid session → `401` (F31–F33).
14. The handler. Public shell routes skip step 13's enforcement but not its attribution.

### The mapper, step by step (`httpx.Fail` on a shell request)

1. If the header was already written: log at error "error after response started" with
   `op` and kind; return (F34).
2. Decide status and code, in this order: `errors.Is(err, db.ErrReadOnly)` → `503
   read_only`; `errors.Is(err, intake.ErrRateLimited)` → `429 rate_limited`,
   `Retry-After: 60`; `errors.Is(err, httpx.ErrUnsupportedMediaType)` → `415`;
   `errors.As(err, *http.MaxBytesError)` → `413`; kind `Canceled` → decision 17 (write
   nothing, or `503 shutting_down` with `Connection: close`); otherwise `StatusOf(kind)`
   with `Kind.String()` as code; `Unavailable` adds `Retry-After: 5`.
3. `Fields` = `validate.Fields(err)` when code is `invalid`, each `{Field, Reason}`.
4. `Ref` = `RefFrom(ctx)` when present.
5. Write the problem with `ContentTypeProblem`; `span.RecordError(err)`; log one record
   (decision 4) with `error_ref`, `trace_id`, `error.kind`, `op` (the outermost
   `errs.Error.Op`), `http.route`, `http.request.method`, `status`, `person_id`.

### Codes

| Status | `code` | When |
|---|---|---|
| 400 | `invalid` | `errs.Invalid`, with `fields` |
| 401 | `unauthenticated` | signed-in route without a valid session; `errs.Unauthenticated` from a handler |
| 403 | `permission` | `errs.Permission` |
| 403 | `cross_origin` | decision 6 |
| 404 | `not_found` | `errs.NotFound`; an unmatched `/api/` path; a UI file path with an extension that does not exist |
| 405 | `method_not_allowed` | the path exists for other methods; `Allow` lists them |
| 409 | `conflict` | `errs.Conflict` |
| 413 | `payload_too_large` | the body cap |
| 415 | `unsupported_media_type` | `ReadJSON` on a non-JSON body |
| 421 | `misdirected` | decision 9 |
| 429 | `rate_limited` | `intake.ErrRateLimited`; later limiters wrap the same sentinel or their own |
| 500 | `internal` | `errs.Internal`, an unkinded error, a panic, a cookie-guard refusal |
| 501 | `unsupported` | `errs.Unsupported` |
| 503 | `unavailable` | `errs.Unavailable` (`Retry-After: 5`) |
| 503 | `read_only` | `db.ErrReadOnly` |
| 503 | `shutting_down` | `errs.Canceled` while draining |
| 504 | `timeout` | `errs.Timeout` |

### `New`, step by step

1. `opts.Validate()` (F9); log the plaintext warning when `AllowPlaintext` (F35, not an
   error); create the histogram `http.server.request.duration` (unit `s`; F18).
2. Build the shell's own route set (`core`): the six routes above with their fragment.
3. For each set in `Routes` plus the shell's: the set name must be a module name
   (`[a-z][a-z0-9]{1,31}`) and unique (F10); each route must validate (F11; `Build` did
   it for modules, the core sets are checked here); build the full pattern, append `{$}`
   to a trailing `/`; register the route handler wrapped in the per-route layers
   (`ServeMux.Handle` under `recover`; a conflict → F12); collect methods per path.
4. For each path: register the method-less `405` fallback with `Allow`; register `/api/`
   as the `404` fallback (F13 if a set claimed `/api/` itself, which the prefix rule
   prevents).
5. Assemble the OpenAPI document (decision 11; F14–F17) and compute its `ETag`.
6. Mount `/healthz`, `/readyz` and, when `UI` is non-nil, the UI handler at `/`.
7. Build `http.Server{Handler, ReadHeaderTimeout, IdleTimeout, WriteTimeout,
   MaxHeaderBytes, BaseContext}` with a cancelable base context; return.

### Serving the UI, step by step

1. Method not `GET` or `HEAD` → `405`. Path cleaned (`path.Clean`); `..` cannot escape an
   `fs.FS`, and a path containing a NUL or not beginning with `/` is `404`.
2. `fs.Stat`: a regular file → serve it with `http.ServeFileFS`-equivalent content
   negotiation (`Last-Modified` from the embedded build time when known) and the cache
   header by prefix; a directory → treat as missing.
3. Missing with an extension (`path.Ext != ""`) → `404` problem. Missing without one →
   `index.html` with `Cache-Control: no-cache`, the CSP and `Content-Type: text/html;
   charset=utf-8`.

### Concurrency and lifecycle

`*Server` is safe for concurrent use. `New` builds everything; `Listen` binds once (F21);
`Serve` runs the accept loop on the caller's goroutine; `Shutdown` is idempotent. The
only goroutines are `net/http`'s per connection; `Shutdown` waits for them within ctx and
then cancels the base context (decision 17), so none outlives `Shutdown` beyond the
handler that ignores its ctx, which `WriteTimeout` ends. `app`'s order: `Shutdown` first
(the HTTP server drains), then `signals.Close`, `Set.Stop`, `db.Close`, `telemetry.Close`,
all within `http.shutdown_timeout` (signals.md Proposed change 5), so no request runs
against a stopped module or a closed pool. `httpx` holds no state.

### Imports

`api`: standard library (`context`, `crypto/sha256`, `encoding/hex`, `encoding/json`,
`errors`, `fmt`, `io`, `io/fs`, `log/slog`, `net`, `net/http`, `net/netip`, `path`,
`runtime/debug`, `sort`, `strings`, `sync`, `sync/atomic`, `time`), `internal/lib/errs`,
`internal/lib/validate`, `internal/lib/clock`, `internal/lib/httpx`, `internal/model`,
`internal/core/module`, `internal/core/db` (`ErrReadOnly`, `Status`; no driver type),
`internal/core/authz` (`Permission`), `internal/core/telemetry`,
`internal/core/telemetry/intake`. Never a module, `app` or `config` (depguard rule `api`,
with `config` already denied), never `go.opentelemetry.io`, never pgx. `httpx`: standard
library (`context`, `encoding/json`, `errors`, `fmt`, `io`, `mime`, `net/http`,
`net/netip`), `internal/lib/errs`, `internal/lib/validate` (depguard rule
`foundation-lib` already allows exactly that).

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `httpx.ReadJSON` | `Content-Type` not `application/json` | Invalid wrapping `ErrUnsupportedMediaType` | `Fail` → 415 | `TestReadJSON_NotJSONContentType_ReturnsUnsupportedMediaType` |
| F2 | `httpx.ReadJSON` | malformed JSON, wrong type, trailing data | Invalid, `FieldError{Field: "body" or the JSON path}`, reason never echoes the body | `Fail` → 400 with fields | `TestReadJSON_Malformed_ReturnsInvalidWithoutEcho` |
| F3 | `httpx.ReadJSON` | empty body | Invalid, field `body`, reason `required` | 400 | `TestReadJSON_EmptyBody_ReturnsInvalid` |
| F4 | `httpx.ReadJSON` | the capped body overflows | `*http.MaxBytesError` returned unchanged (kind Internal by `KindOf`; the mapper checks the type first) | `Fail` → 413 | `TestReadJSON_OverCap_ReturnsMaxBytesError` |
| F5 | `httpx.ReadJSON` | ctx done while reading | Canceled / Timeout | `Fail` | `TestReadJSON_ContextCanceled_ReturnsCanceled` |
| F6 | `httpx.ReadJSON` | `dst` nil or not a pointer | Internal | fix the handler | `TestReadJSON_BadDst_ReturnsInternal` |
| F7 | `httpx.WriteJSON` | `v` cannot be encoded | Internal; nothing written | `Fail` → 500 | `TestWriteJSON_Unencodable_ReturnsInternalWritesNothing` |
| F8 | `httpx.WriteJSON` | the client is gone mid-write | Canceled (or the write error's kind) | return; the shell logged | `TestWriteJSON_ClientGone_ReturnsCanceled` |
| F9 | `Options.Validate`, `New` | a field out of range or nil; several at once | Invalid, one `FieldError` each | fix app | `TestOptionsValidate_ReportsEach` |
| F10 | `New` | a set name invalid or repeated | Invalid naming the set | fix app | `TestNew_BadOrDuplicateSetName_ReturnsInvalid` |
| F11 | `New` | a core set's route fails `module.Route.Validate` | Invalid naming set and route | fix the core package | `TestNew_InvalidCoreRoute_ReturnsInvalid` |
| F12 | `New` | two patterns conflict in `ServeMux` (within or across sets) | Conflict naming both sets and patterns, no panic | fix the module | `TestNew_ConflictingPatterns_ReturnsConflict` |
| F13 | `New` | a set would mount at `/api/` itself or outside `/api/<name>/` | Invalid | fix the module | `TestNew_RouteEscapesPrefix_ReturnsInvalid` |
| F14 | `New` | a set has routes and `OpenAPI == nil` | Invalid naming the set | add the fragment | `TestNew_RoutesWithoutFragment_ReturnsInvalid` |
| F15 | `New` | a route with no operation, or an operation with no route, in a set | Invalid naming set, method and path | fix the fragment | `TestNew_RouteFragmentMismatch_ReturnsInvalid` |
| F16 | `New` | a fragment is not a JSON object, a path key does not start with `/`, a `$ref` does not resolve, or a `$ref` is not local | Invalid naming the set and the key | fix the fragment | `TestNew_MalformedFragment_ReturnsInvalid` |
| F17 | `New` | two sets define one `components.schemas` name | Conflict naming both | prefix the schema name | `TestNew_DuplicateSchemaName_ReturnsConflict` |
| F18 | `New` | the histogram cannot be created (telemetry F20–F21) | the meter's kind | fix app (one `New` per meter scope) | `TestNew_HistogramCreationFails_ReturnsKind` |
| F19 | `Listen` | the address is in use or cannot be bound | Unavailable | fix the port, stop the other process | `TestListen_AddrInUse_ReturnsUnavailable` |
| F20 | `Listen` | the address is malformed | Invalid | fix config | `TestListen_BadAddr_ReturnsInvalid` |
| F21 | `Listen` | already listening | Conflict | fix app | `TestListen_Twice_ReturnsConflict` |
| F22 | `Serve` | `Listen` not called | Internal | fix app | `TestServe_WithoutListen_ReturnsInternal` |
| F23 | `Serve` | the accept loop fails other than by `Shutdown` | Unavailable wrapping the error | app exits | `TestServe_ListenerClosedUnderneath_ReturnsUnavailable` |
| F24 | `Shutdown` | in-flight requests do not finish within ctx | Timeout / Canceled; the base ctx canceled; stuck handlers see `Canceled`; second `Shutdown` nil | log and exit | `TestShutdown_InFlightOverDeadline_CancelsAndReturnsTimeout`, `TestShutdown_Twice_IsNil` |
| F25 | pipeline | a handler or middleware panics | none to the caller beyond `500 internal` with `ref` (if no header yet); span failed; error log with `stack`; the server keeps serving | fix the bug | `TestPipeline_HandlerPanics_500WithRefAndStackLogged` |
| F26 | pipeline | an untrusted, non-loopback peer with `TrustedProxies` set and `AllowPlaintext` false, path not a health route | none: `421 misdirected`; warn once per prefix per hour with `peer_prefix` | fix `trusted_proxies` or the route to bms | `TestPipeline_UntrustedPeer_421AndLogsPrefixOnce`, `TestPipeline_UntrustedPeer_HealthRoutesServed` |
| F27 | pipeline | a body over `MaxBodyBytes` | `413 payload_too_large` | shorten the request | `TestPipeline_BodyOverCap_413` |
| F28 | pipeline | the handler outlives `RequestTimeout` | its I/O fails with Timeout → `504 timeout`; `WriteTimeout` ends a handler that ignores ctx | shorten the work | `TestPipeline_RequestTimeout_504` |
| F29 | cookie guard | a response sets a cookie without `Secure`, `HttpOnly`, `SameSite=Lax` or `Path=/` | `500 internal`; error log naming the cookie, never its value | fix the handler | `TestCookieGuard_MissingAttribute_500NamesCookieOnly` |
| F30 | cross-origin protection | an unsafe-method request with `Sec-Fetch-Site: cross-site` (or `same-site`) or an `Origin` whose host differs from `Host` | `403 cross_origin`; warn without the origin value | fix the caller (the shell is same-origin) | `TestCrossOrigin_Table` (cross-site rejected; same-origin, no headers, `GET` accepted) |
| F31 | authentication | signed-in route, no session at all | `401 unauthenticated`; debug log | sign in | `TestAuth_SignedInRouteNoSession_401` |
| F32 | authentication | signed-in route, `Authenticator` fails `Unauthenticated` (invalid, expired) | `401`; warn log | sign in again | `TestAuth_SignedInRouteBadSession_401Warn` |
| F33 | authentication | `Authenticator` fails with another kind (Unavailable, Timeout) | signed-in route: that kind's status; public route: served anonymously, warn log | retry / fix the dependency | `TestAuth_AuthenticatorUnavailable_503OnSignedInAnonymousOnPublic` |
| F34 | mapper | `Fail` after the header was written | none: error log "error after response started"; nothing more written | fix the handler (fail before writing) | `TestFail_AfterHeader_LogsOnly` |
| F35 | `New` | `AllowPlaintext` true | none: one warn log at start | development only | `TestNew_AllowPlaintext_LogsWarningOnce` |
| F36 | mapper | `db.ErrReadOnly` anywhere in the chain | `503 read_only` | "this copy is read-only" | `TestMapper_ReadOnly_503ReadOnlyCode` |
| F37 | mapper | `intake.ErrRateLimited` | `429 rate_limited`, `Retry-After: 60` | back off | `TestMapper_RateLimited_429RetryAfter` |
| F38 | mapper | kind `Canceled`, client gone | nothing written; debug log; span not failed | — | `TestMapper_CanceledClientGone_WritesNothing` |
| F39 | mapper | kind `Canceled` while draining | `503 shutting_down`, `Connection: close` | retry against the new instance | `TestMapper_CanceledWhileDraining_503ShuttingDown` |
| F40 | mapper | every other kind, including an unkinded error | `StatusOf(kind)`, code `Kind.String()`, `ref`, `fields` for `invalid`; one log record at the right level; `err.Error()` absent from the body | per the code | `TestMapper_KindTable` (ten kinds), `TestMapper_BodyNeverCarriesErrorText` |
| F41 | router | an unmatched `/api/` path | `404 not_found`; debug log; route token `/{unmatched}` | fix the URL | `TestRouter_UnknownPath_404JSON` |
| F42 | router | a known path with another method | `405 method_not_allowed` with `Allow`; debug log | fix the method | `TestRouter_WrongMethod_405WithAllow` |
| F43 | `/readyz` | `Database.Ping` fails or exceeds 2 s | `503 {"database":"unavailable"}`; warn with the kind | the orchestrator waits | `TestReadyz_PingFails_503` |
| F44 | `/readyz`, `/healthz` | after `Shutdown` began | `503 {"status":"draining"}` | stop routing to this instance | `TestHealth_Draining_503` |
| F45 | `GET /api/admin/modules` | no session; `Permissions` nil; `Allow` fails `Permission`; `Schema` fails | 401; 403; 403; 503 (its kind) | sign in / grant / fix the database | `TestAdminModules_Table` |
| F46 | `POST /api/client-errors` | non-JSON content type; body over 64 KiB; invalid batch; rate limited; ok | 415; 413; 400 with fields; 429 + `Retry-After: 60`; 202 no body | per telemetry Proposed change 3 | `TestClientErrors_Table` |
| F47 | UI | `UI` nil and a non-API path; a path with an extension that has no file; a non-`GET` method | `404 not_found`; `404`; `405` | — | `TestUI_Table` |
| F48 | span naming (obligation) | a pattern that would not fit telemetry's span-name grammar | cannot happen: `module.Route.Pattern` starts with `/` and has no space (F33 there), and the shell builds `<METHOD> /api/<name><pattern>`; a test proves the name passes `telemetrytest` with no `Violation` | — | `TestSpanName_EveryMountedRoute_NoViolation` |

Members that cannot fail, and why: `httpx.StatusOf` (a table with a default); `Fail`
(every input becomes a response; a nil error becomes a 500 and a test pins it); the ctx
accessors (lookups); `Handler`, `OpenAPI` (values built by `New`); `PermissionModulesRead`
(a constant). Each has a success-path test.

## Lock-in check

No external technology: `net/http`, `ServeMux`, `CrossOriginProtection`,
`MaxBytesReader` and `encoding/json` are the standard library. The front end behind
`TrustedProxies` can be any proxy or tunnel (Caddy and nginx are the examples in `deploy/`,
both OSI-licensed; Cloudflare Tunnel is an example, not a requirement). OpenAPI 3.1 is an
open specification; the document is plain JSON that any generator reads. Nothing depends
on PostgreSQL, OpenTelemetry or a hosted service: `db` and `telemetry` arrive as
interfaces, and the tests run the whole server against `dbtest`-style fakes and
`telemetrytest`.

## Test plan

**Unit** (no infrastructure, `go test -race`, packages `httpx_test` and `api_test`,
table-driven, `httptest.NewRecorder` and `httptest.NewServer(s.Handler())`;
`telemetrytest.New(t)` for the logger, tracer and meter, so any policy violation fails
the test; `clock.NewFake`; a fake `Database` (ping result, read-only flag), a fake
`Authenticator` (table of outcomes), a fake `PermissionChecker`, a fake `SchemaStatus`, a
real `intake.New` over the capture; invented module names `alpha`, `beta` with hand-built
`module.ModuleRoutes` and fragments; markers `ZQ9MARKER7` and `zq9@marker.example` in
bodies, query strings, cookies and `User-Agent`, asserted absent with
`!c.Contains(marker)` on every test that produces a log or a span):

- `httpx`: `TestStatusOf_Table` (ten kinds plus an out-of-range value);
  `TestFail_WithoutFailer_WritesProblemFromTable` (status, code, fields, no ref,
  `ContentTypeProblem`); `TestFail_WithFailer_Delegates`; `TestFail_NilErr_500`;
  `TestReadJSON_HappyPath_IgnoresUnknownFields`; `TestReadJSON_ContentTypeWithParams`;
  `TestWriteJSON_204NoBody`; `TestCtxAccessors_RoundTripAndAbsent`; `TestCodes_Pinned`
  (the code strings are stable API); F1–F8.
- Router and mounting: `TestNew_MountsUnderPrefix` (`GET /api/alpha/things/{id}`
  reaches the handler with `r.PathValue("id")`); `TestNew_TrailingSlashIsExact`
  (`/` becomes `/api/alpha/{$}`; `/api/alpha/x` is 404); `TestNew_WildcardAllowed`
  (`{path...}`); `TestNew_CoreSetMountsLikeAModule`; `TestNew_EmptyRoutes_ServesShellOnly`;
  F10–F13, F41, F42.
- Pipeline: `TestPipeline_Order` (a recording handler sees the address, scheme, deadline,
  failer, span and actor in ctx); `TestPipeline_SecurityHeaders_Table` (every response;
  HSTS only with `https`; `no-store` on `/api/`); `TestPipeline_XTraceIDMatchesSpan`;
  `TestPipeline_XReadOnlyHeader`; `TestPipeline_ClientAddr_Table` (no proxies: peer;
  trusted peer with a chain: rightmost untrusted; untrusted peer with forged headers:
  peer and `http`; `X-Forwarded-Proto: https` from a trusted peer: `https`);
  `TestPipeline_SpanNamedByRoute_WithParentFromTraceparent` (child of the header's trace;
  `http.request.method`, `http.route`, `http.response.status_code`, `person_id`);
  `TestPipeline_HistogramSample_TokensOnly` (no ID, no text on the measurement);
  `TestPipeline_HealthRoutes_NoSpanNoSample`; `TestPipeline_SuccessWritesNoLogLine`;
  `TestPipeline_NeverLogsQueryBodyCookieOrAgent` (markers everywhere, an error response
  forced, `!c.Contains`); F25–F33, F35.
- Mapper: `TestMapper_ErrorLogRecordFields` (level per status; the field set);
  `TestMapper_FieldsFromValidateErrors` (three problems → three fields in order);
  `TestMapper_UnavailableRetryAfter`; F34, F36–F40.
- Shell routes: `TestHealthz_OK`; `TestReadyz_ReadWriteAndReadOnly`;
  `TestModules_PublicListsNamesAndEnabled` (disabled module listed with `false`;
  no permissions or signals in the body); `TestAdminModules_WithPermission_GraphAndSchema`;
  `TestOpenAPI_Served_WithETagAndNotModified`; F43–F46.
- OpenAPI assembly: `TestOpenAPI_MergesFragmentsUnderPrefixes` (paths prefixed; schemas
  merged; tags merged; `info.version`; base schemas present; `default` response added);
  `TestOpenAPI_EveryShellRouteDocumented` (the shell's own set passes F15);
  `TestOpenAPI_IDSensitivityTimeRangeShapes` (model.md Proposed change 6); F14–F17.
- UI: `TestUI_HashedAssetImmutable`; `TestUI_IndexForAppPathWithCSPAndNoCache`;
  `TestUI_PathTraversalIs404`; `TestUI_CustomCSP`; F47.
- Lifecycle: `TestListenServeShutdown_RoundTrip` (`:0`; a request; `Shutdown` returns
  nil); `TestShutdown_DrainsInFlightRequest` (a handler blocked on a channel finishes
  and its response has `Connection: close`); F19–F24.
- F48 across every mounted route.
- Lint: passes depguard rules `api` and `foundation-lib`, `errcheck`, `errorlint`,
  `wrapcheck`, `bodyclose`, `noctx`, `contextcheck`, `nilerr`, `gosec`, `sloglint`.

**Integration** (`//go:build integration`): none needed for this package alone; nothing
here touches a database or the network beyond `httptest`. `app.md`'s start-up test and the
`hello` module's integration test (module-contract "Conformance") drive a real request
through `New` → `Listen` → `Serve` with a real `db.Pool`, `telemetry.New` and the module
`Set`, and assert the `hello.greeted` signal spans sit under the request span.

**Conformance:** none of its own. The checks `New` runs over every route set (F10–F17)
are the contract this spec enforces on modules, and the `hello` module is the reference
that passes them.

## Open questions

1. **May a cookie lack `Secure` when `AllowPlaintext` is on and the request's effective
   scheme is `http`?** config.md decision 16(d) says cookies are `Secure` "whatever the
   hop", which is right whenever TLS exists somewhere (the effective scheme is then
   `https`). With `http.allow_plaintext=true` and a direct plain-HTTP request from
   another machine (a phone on the LAN during development) there is no TLS anywhere, and
   a `Secure` cookie is never sent back, so login is impossible. Proposed: yes, in exactly
   that case; `identity` reads `httpx.SchemeFrom` and omits `Secure` only when the scheme
   is `http` and the shell was built with `AllowPlaintext` (the cookie guard allows the
   omission under the same two conditions); every other deployment is unchanged, and the
   start-up warning already says plaintext is for development.
2. **How are field-error reasons translated?** `validate.FieldError.Reason` is English
   ("must be between 1 and 1000, got 0") and D28 wants every string translation-ready.
   Proposed: add a machine-readable `Code` (`required`, `range`, `one_of`, `url`, ...)
   and its parameters to `validate.FieldError` (lib-core, Proposed change 8), carry it as
   `fields[].code` and `fields[].params`, and let the web shell translate by code,
   falling back to `reason` for a code it does not know. Until then the body carries
   `field` and `reason` as specified here and the shell shows the reason as-is.
3. **HSTS: `includeSubDomains` or not?** config.md Proposed change 5 suggested
   `max-age=31536000; includeSubDomains`. A regional may serve other things on
   subdomains of the bms host (a wiki, a ticket page) that are not on HTTPS yet, and
   `includeSubDomains` would break them for a year. Proposed: `max-age=31536000` alone,
   with a note in the operator docs that the front end may add `includeSubDomains` (and
   `preload`) when the regional controls every subdomain.
4. **Is `GET /api/modules` (enabled module names only) acceptable as a public route?**
   Proposed: yes (decision 15). The alternative, requiring a session, means the web shell
   cannot know which modules exist before sign-in and M0's "a hello module shows up"
   needs a login that does not exist until M1.

## Proposed changes elsewhere

1. **`plan/07-abstractions.md`, `api shell` entry:** replace with: "**Purpose:** expose
   the core's and the modules' routes over HTTP with one request pipeline. **Owns:** the
   router (`net/http.ServeMux`, routes mounted under `/api/<name>/`, JSON 404 and 405);
   the middleware chain (recovery, the peer check for a misrouted plaintext path, the
   client address and scheme from trusted proxies, security headers, body cap, request
   deadline, draining; per route: the request span and duration histogram, the error
   mapper, the cookie guard, cross-origin protection, authentication through
   `identity`'s `Authenticator`); the problem body and the error mapper (`errs` kind and
   the `db`/`intake` sentinels → status and code, `ref`, one log line per error response);
   `/healthz`, `/readyz`, `GET /api/modules`, `GET /api/admin/modules`,
   `POST /api/client-errors`, `GET /api/openapi.json`; OpenAPI assembly from the sets'
   fragments with a start-up check that routes and fragments agree; serving the built web
   shell. **Doesn't do:** business logic, permission checks (the service's), sessions
   (`identity`), permissions (`authz`), TLS (the front end), configuration loading
   (`config`), the shutdown deadline and order (`app`)." Add to the `lib` entry: `httpx`
   (the problem body, `StatusOf`, `Fail`, `ReadJSON`, `WriteJSON`, the client-address and
   scheme ctx accessors; owned by `api.md`). Layer rule 4 may add: "Handlers in any layer
   reach the shell's error mapper through `lib/httpx.Fail`; `api` is never imported."
2. **`.claude/skills/go-conventions/SKILL.md`, "Errors" table and "Layers":** the HTTP
   column's note "done only in `api`'s ErrorMapper" becomes "the table is
   `lib/httpx.StatusOf`; the api shell's mapper is the only writer on a real request;
   handlers call `httpx.Fail(w, r, err)` and never write an error body themselves". Add
   `httpx` to the `lib` line. **`.claude/skills/add-module/SKILL.md` §2, `http.go`:**
   "handlers: `httpx.ReadJSON`, call the service, `httpx.WriteJSON`; on error
   `httpx.Fail` and return. Path IDs with `model.ParseID`. The module ships an OpenAPI
   fragment (`openapi.json`, embedded) whose paths are relative to `/api/<name>` and whose
   schema names start with the module's name; `api` refuses to start if a route and the
   fragment disagree."
3. **`plan/design/module-contract.md` (row 4):** its Proposed change 6 is answered here
   (mounting, fragment form, `GET /api/modules` split into a public list and a
   permissioned admin route, the read-only mapping). Proposed amendments: (a) a
   `RouteProvider` must also implement `APIDescriber` (F34 becomes "a `RouteProvider`
   without a non-empty `OpenAPI()`"), since `api` requires the fragment (decision 11);
   (b) note beside `Route.Pattern` that a pattern ending in `/` is mounted exactly
   (`{$}` appended) and that `{path...}` is the way to take a subtree; (c) note that
   `ModuleRoutes` is also the shape `app` uses for the core's route sets, with `Module`
   set to the core area name; (d) `core.modules.read` is declared by `api`
   (`api.PermissionModulesRead`) and registered by `app`; (e) the `hello` module's
   fragment documents `GET /greeting` and `GET /public-greeting` and its schema names
   start with `Hello`.
4. **`plan/design/config.md` (row 3):** its Proposed change 5 is answered: (b) and (d)
   enforced as decisions 7 and 8; the plaintext warning F35; the direct-peer decision is
   `421` with the health routes exempt (decision 9). Two refinements to record: "session
   and CSRF cookies" becomes "the session cookie" (there is no CSRF cookie, decision 6);
   the HSTS value and the `Secure`-under-plaintext case wait for Open questions 1 and 3.
5. **`plan/design/db.md` (row 5):** its Proposed change 4 is answered: `read_only`
   mapping (decision 16, F36), `/readyz` shape as proposed, `GET /api/admin/modules`
   carries `Built`, `Applied` and `current` per set through `api.SchemaStatus` (which
   `app` builds over `Migrator.Status`), and stores turn `*db.Error.Constraint` into a
   `validate.FieldError` themselves (the mapper reads `validate.Fields` only).
6. **`plan/design/telemetry.md` (row 15b):** its Proposed change 3 is answered with one
   refinement: the span is opened per route after routing (its `Span` has no rename, so
   the name is known at `Start`; decision 10), unmatched requests use the route token
   `/{unmatched}`, the health routes get no span and no sample, and every response carries
   `X-Trace-Id` (its decision 2 allowed it). The intake route is mounted as specified
   (F46). Its "UI" wording and Proposed change 4 stand.
7. **`plan/design/lib-errs.md`, Open question 1:** answered as proposed (decision 17):
   client gone → no response and a debug line; shutdown → `503 shutting_down` with
   `Connection: close`. Its Proposed change 1 is met by `httpx.Problem` and the mapper.
8. **`plan/design/lib-core.md`, `validate`:** add consumer rows for `httpx` (`FieldError`,
   `Fields` in `Fail`'s fallback and in `ReadJSON`); if Open question 2 is answered as
   proposed, add `Code string` and `Params map[string]string` to `FieldError`, set by each
   check (`NonEmpty` → `required`, `Range` → `range` with `min`/`max`, `OneOf` →
   `one_of` with `options`, `URL` → `url`, `HostPort`/`ListenAddr` → `host_port`), and
   let `Addf` take a code.
9. **`plan/design/identity.md` (row 6):** implement `api.Authenticator` (no database write;
   `Unauthenticated` for a bad or expired session; `(ctx, zero, nil)` for none); define
   the principal's ctx accessor that services and `authz` use; write the session cookie
   with `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/` (decision 7; Open question 1 for
   the plaintext case); its login, logout and password routes are a core route set
   (`accounts`, mounted at `/api/accounts/`) with login and sign-up `AccessPublic`;
   rate-limit failed logins by `httpx.ClientAddrFrom`, answering `429 rate_limited` by
   wrapping its own sentinel that the mapper learns then (or `intake.ErrRateLimited`
   moved to a shared place); document that the session value is request-scoped
   (signals.md Proposed change 7).
10. **`plan/design/authz.md` (row 7):** satisfy `api.PermissionChecker` with its
    `Authorizer` (site-scoped `Allow`); receive `api.PermissionModulesRead` from `app`
    among the core's declarations (`PermissionDecl{Name: "core.modules.read", Scope:
    ScopeSite, Reaches: SensitivityInternal}`), granted to every site-admin starter role
    by default; refuse to treat a session value as the actor when
    `signals.InDelivery(ctx)` is true.
11. **`plan/design/app.md` (row 18):** build `api.Options` from `config.HTTP` (all but
    `ShutdownTimeout`), `tel.Logger().With("component","api")`, `tel.Tracer("api")`,
    `tel.Meter("api")`, `clock.Real()`, the build version, `Set.Routes()` plus one
    `module.ModuleRoutes` per core package with routes (`accounts`, `people`, ... as their
    specs define, `Module` = the area name), the `*db.Pool`, the `*intake.Intake`,
    `plan.Graph()`, a `SchemaStatus` closing over `Migrator.Status` and every set,
    `identity`'s `Authenticator` (M1) and `authz`'s checker (M2), the embedded web build as
    `UI` (`nil` for a headless build); `New` → `Listen` → `go Serve` → on signal
    `Shutdown(ctx)` under `http.shutdown_timeout`, then `signals.Close`, `Set.Stop`,
    `db.Close`, `telemetry.Close`; register `api.PermissionModulesRead` with `authz`;
    `bms check` prints the OpenAPI operation count; the `hello` integration test asserts a
    request's `deliver:hello.greeted` span is a descendant of its request span.
12. **`plan/design/web-shell.md` (row 17):** mirror `httpx.Problem` and the codes in
    TypeScript; translate `code` through keys `error.<code>` and, for `invalid`, map
    `fields[].field` to inputs (reason shown as-is until Open question 2); show `ref` per
    telemetry.md "UI"; on `401` go to sign-in; show the read-only banner when a response
    carries `X-Read-Only: true` and clear it when one does not; read `GET /api/modules`
    for the enabled set and `GET /api/admin/modules` for the admin screen; generate the
    client from `/api/openapi.json` at build time and lint the document in the web
    tooling (`make check`); build with hashed assets under `assets/` and no inline
    scripts (`DefaultCSP`), proposing a CSP change here if the build needs one; send
    `traceparent` on every call (telemetry Proposed change 4).
13. **`plan/09-core-features.md` §1 "Done when":** add "`GET /api/openapi.json` describes
    every mounted route; the server refuses to start when a route and its module's
    fragment disagree" and "an error response is `application/problem+json` with `code`
    and `ref`".
14. **`deploy/` front-end examples and `docs/operating.md` (when written):** the front end
    must preserve the `Host` header (decision 6: Caddy does by default; nginx needs
    `proxy_set_header Host $host`), set `X-Forwarded-For` and `X-Forwarded-Proto` and
    strip incoming ones (config.md Proposed change 11), and health checks come from
    loopback or through the front end (direct probes from other addresses are served for
    `/healthz` and `/readyz` only); document the `421` log line as "a request reached bms
    without passing the front end".
15. **`plan/design/settings.md` (row 15):** when the public URL exists (D18), `app` passes
    its origin to `api` as a trusted origin for cross-origin protection, so a front end
    that rewrites `Host` still works; until then the `Host`-preserving rule in 14 holds.
    This adds `Options.TrustedOrigins []string` to this spec at that time.
16. **`plan/design/module-hello.md` (row 4b):** ship `openapi.json` next to `module.go`
    (embedded), use `httpx.ReadJSON`/`WriteJSON`/`Fail`, read the actor through
    `identity`'s accessor (M1) and return `errs.Permission` from its service until then
    for `GET /greeting`; its tests assert `404 not_found` from `httpx.Fail`'s fallback and
    the full problem body through `app`'s integration test.
17. **`.golangci.yml`:** no new rule needed: `foundation-lib` already confines `httpx` to
    the standard library and `lib`, and the `api` rule already denies modules, `app` and
    `config`. If the owner wants a strict allowlist for `internal/api` (stdlib, `lib`,
    `model`, `core`), add it as `list-mode: strict` on the `api` rule.
