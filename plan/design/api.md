# api: the HTTP shell

- **Status:** Approved (2026-10-17)
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

## Summary

**Purpose:** expose the core's and the modules' routes over HTTP through one request pipeline. Two packages: `internal/lib/httpx` (what any handler may import) and `internal/api` (the shell; imports core contracts, model, lib; never a module, `app`, `config`, otel or pgx).

**Exported, `httpx` (stdlib + errs + validate only):**
- `Problem{Status, Code, Ref, Fields []Field}`, `Field{Field, Code, Params, Reason}`, `ContentTypeProblem` (`application/problem+json`, RFC 9457).
- Shell codes (pinned strings): `read_only`(503), `rate_limited`(429), `shutting_down`(503), `payload_too_large`(413), `unsupported_media_type`(415), `cross_origin`(403), `misdirected`(403), `method_not_allowed`(405), `not_found`(404); otherwise the `errs.Kind` name.
- `StatusOf(errs.Kind) int` (Invalid 400, Unauthenticated 401, Permission 403, NotFound 404, Conflict 409, Unavailable 503, Timeout 504, Canceled 503, Unsupported 501, Internal 500).
- `Fail(w, r, err)` hands any error to the shell's mapper (falls back to the same table without one); `Failer`, `WithFailer`.
- `ReadJSON(r, dst) error` (JSON content type required, unknown fields ignored, errors never echo the body); `WriteJSON(w, status, v) error`; `ErrUnsupportedMediaType`.
- `ClientAddrFrom(ctx)`, `SchemeFrom(ctx)`, `WithClientAddr`, `WithScheme` (derived values; the address is personal data, never log it).

**Exported, `api`:**
- `New(Options) (*Server, error)`, `Options.Validate()`; `Server`: `Handler()`, `OpenAPI() []byte`, `Listen(ctx) (net.Addr, error)`, `Serve() error`, `Shutdown(ctx) error`.
- `Options`: `Addr, RequestTimeout, MaxBodyBytes, TrustedProxies []netip.Prefix, AllowPlaintext` (from `config.HTTP`); `Logger, Tracer, Meter, Clock, Version, Routes []module.ModuleRoutes, Database, Intake, Graph, Schema, Authenticator, Permissions, UI fs.FS, ContentSecurityPolicy`.
- Seams: `Authenticator.Authenticate(ctx, r) (ctx, model.PersonID, error)` (identity, nil in M0 so signed-in routes 401); `PermissionChecker.Allow(ctx, authz.Permission) error` (authz, nil until M2 so permissioned shell routes 403); `Database{Ping, ReadOnly}`; `SchemaStatus func(ctx) ([]db.Status, error)`; `PermissionModulesRead = "core.modules.read"`; `DefaultCSP`.

**Key decisions:**
- D1 `net/http.ServeMux` is the router, no third-party router.
- D2 wire form, status table and handler helpers live in `lib/httpx`, because modules may not import `api`.
- D3 routes mount as `<METHOD> /api/<name><pattern>`, trailing `/` becomes exact (`{$}`), one `/api/` fallback gives JSON 404/405 with `Allow`, no mux redirects under `/api/`, conflicts are `Conflict` at `New`.
- D4 one RFC 9457 body, no prose, `err.Error()` never sent; one log record per error response (error 5xx, warn signed-in 4xx, debug anonymous 4xx); successes log nothing.
- D5 authentication is a seam called on every routed request; invalid session on a public route counts as anonymous.
- D6 CSRF via `net/http.CrossOriginProtection` plus SameSite=Lax; no token cookie, no `Access-Control-*` ever.
- D7 cookies `Secure; HttpOnly; SameSite=Lax; Path=/`, no Domain, session cookie `__Host-` prefixed; a cookie guard turns violations into 500.
- D8 client address and scheme from `X-Forwarded-For`/`-Proto` only for peers in `TrustedProxies` (rightmost-untrusted rule); zero-bit prefix refused.
- D9 direct peer that is neither loopback nor trusted gets `403 misdirected` (not 421), health routes exempt, log per /24 or /64 prefix per hour (max 1,000 prefixes).
- D10 span opened per route after routing, named `<METHOD> <route token>`; method normalised to 7 known or `_OTHER` (span `HTTP <token>`); `{$}` rendered as `/`; unmatched route token `/{unmatched}`; no span for `/healthz`, `/readyz`.
- D11 OpenAPI 3.1 JSON assembled from per-set fragments; routes and fragments must agree at `New`; a set with routes must have a fragment.
- D12 serves the web UI: `/assets/` immutable year cache, `index.html` no-cache for app paths, CSP on every `text/html`, `no-store` on `/api/`.
- D13 unknown JSON fields ignored.
- D14 permissioned shell route with nil checker answers 403.
- D15/D27 `GET /api/modules` public, names and enabled only (owner 2026-10-17); graph and schema versions behind `core.modules.read`.
- D16 writes are not refused by the shell; `db.ErrReadOnly` maps to `503 read_only`; `X-Read-Only: true` on every response from a read-only copy.
- D17 `Canceled`: client gone writes nothing; while draining `503 shutting_down` + `Connection: close`.
- D18 request deadline = ctx deadline + connection read deadline; `WriteTimeout = RequestTimeout + 15s` is a backstop.
- D19 every response carries `X-Trace-Id`.
- D20 shell routes are absolute paths (`/healthz`, `/readyz`, `/api/modules`, `/api/admin/modules`, `/api/client-errors`, `/api/openapi.json`); set names `core`, `modules`, `admin` refused.
- D21 `/healthz` carries no version.
- D22 `/readyz` single-flights and caches `Ping` for 1 s (ping deadline 2 s).
- D23 `Serve` after `Shutdown` is nil; `Listen` after `Shutdown` is `Conflict`.
- D24 only cookie without `Secure`: `bms_session_dev`, when `AllowPlaintext` and scheme `http` (owner 2026-10-17).
- D25 field errors carry `code` and `params`; shell translates by code, falls back to `reason` (owner 2026-10-17).
- D26 HSTS `max-age=31536000` only when scheme https, no `includeSubDomains` (owner 2026-10-17).

**Errors callers handle:** `Options.Validate`/`New`: `Invalid` (fields `http.addr`, `http.request_timeout`, `http.max_body_bytes`, `http.trusted_proxies`, `api.*`), `Conflict` (pattern or schema-name clash), `Invalid` for route/fragment mismatch or bad route token; `Listen`: `Unavailable` (in use), `Invalid`, `Conflict`; `Serve`: `Internal` (no Listen), `Unavailable`; `Shutdown`: `Timeout`/`Canceled` (late handlers may still run). `ReadJSON`: `Invalid` (wrapping `ErrUnsupportedMediaType` for 415; field `body` or JSON path), `*http.MaxBytesError` returned unchanged (413), `Timeout`, `Canceled`, `Internal` (bad dst). `WriteJSON`: `Internal` (unencodable), `Canceled` (write failed). Mapper checks sentinels before kind: `db.ErrReadOnly`, `intake.ErrRateLimited` (429, `Retry-After: 60`), `httpx.ErrUnsupportedMediaType`, `*http.MaxBytesError`. `Unavailable` adds `Retry-After: 5`.

**Owns:** config consumed (via `Options`): `http.addr`, `http.request_timeout`, `http.max_body_bytes`, `http.trusted_proxies`, `http.allow_plaintext`. Permission `core.modules.read` (scope site, reaches internal; declared here, registered by `app`). No settings, signals, tables. Metric `http.server.request.duration` (s; method, route, status). Span attrs `http.request.method`, `http.route`, `http.response.status_code`, `person_id`, `duration_ms`. Headers: nosniff, Referrer-Policy, X-Frame-Options, Permissions-Policy, COOP/CORP, HSTS, `X-Trace-Id`, `X-Read-Only`. `POST /api/client-errors` answers 202/400/413/415/429 over `intake.Accept` (64 KiB cap). Constants (not keys): ReadHeaderTimeout 10s, IdleTimeout 120s, MaxHeaderBytes 64 KiB.

**Expects of dependencies:** telemetry (`Tracer.Start` with `WithParentFrom`, `RefFrom`, `TraceIDFrom`, token grammar admitting `_OTHER` and the `<METHOD> <route>` span form, `intake`); module-contract (`Route.Access`, `ModuleRoutes`, `Graph`, `RouteProvider` must also provide an OpenAPI fragment); db (`Ping`, `ReadOnly`, `ErrReadOnly`, `Status`, `Close` tolerant of late callers); identity (Authenticator, cookie rules); authz (PermissionChecker); lib-core (`validate.FieldError.Code/Params`, applied in spec, implementation follow-up needed); app (shutdown order and sub-deadline).

**Pending proposed changes:** items 9 (identity), 10 (authz), 15 (settings: trusted origins and Host allowlist), and the front-end examples and `docs/operating.md` half of 14 wait on targets not yet written; the rest are applied.

## Purpose

Expose the core's and the modules' routes over HTTP with one request pipeline.

## Responsibilities

- **Mount** every route set it is given (the modules' from `Set.Routes()`, the core's from
  `app`) under `/api/<name>/`, on `net/http`'s `ServeMux`, with exact-match patterns and
  one `/api/` fallback that answers a JSON 404, or a JSON 405 with `Allow` when the path
  exists for other methods (decision 3).
- Run every request through one **middleware chain**: panic recovery, the peer check for a
  misrouted plaintext path (decision 9), the client address and effective scheme from
  trusted proxies (decision 8), security headers, the body cap, the request deadline (ctx
  and connection read deadline, decision 18), draining on shutdown, the unclean-path
  check; then, per route: the request span and duration histogram (with the method
  normalised, decision 10), the error mapper, the cookie guard, cross-origin protection
  for unsafe methods (decision 6), authentication (decision 5).
- **Map** every error a handler returns to one HTTP status and one problem body with the
  error reference, and write one log record per error response (decision 4).
- Serve the shell's own endpoints at absolute paths: `/healthz`, `/readyz`,
  `GET /api/modules`, `GET /api/admin/modules`, `POST /api/client-errors`,
  `GET /api/openapi.json` (decision 20 on why they are not under `/api/core/`).
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
| Who the person is: cookies written at login, the signed session token and its per-account counter, logout, the superuser's short sessions, rate-limiting failed logins by address | `identity` (`identity.md`), which implements `Authenticator` (decision 5) and uses `httpx.ClientAddrFrom` for the login rate limit (config.md Proposed change 6). The cookie attributes are this spec's rule (decisions 7 and 24); `identity` writes the cookie. |
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
| Translating a field error | `web-shell.md`, by `fields[].code` with `reason` as the fallback (decision 25); `validate` sets the code (lib-core.md). |
| Deciding that a record the caller may not see is answered `404` rather than `403` | the service (privacy-data "Rules": a list must not reveal that a hidden record exists, and a single record the caller may not see is `NotFound`, never `Permission`; applied 2026-10-17 by Proposed change 2). The mapper maps whatever kind the service chose. |
| A `Host` allowlist against DNS rebinding | nobody until the public URL setting exists (settings.md, D18); Proposed change 15 adds it then. Until then the front end's virtual host is the allowlist. |
| Telling a later trusted-header identity provider (D15) that the peer is a trusted proxy | `identity`, when that provider is designed; this spec then adds a `httpx.TrustedPeerFrom(ctx)` accessor (Proposed change 9). The derivation already exists inside the pipeline. |
| File uploads and downloads, range requests, streaming | `files.md` (Later). The body cap and `WriteTimeout` here are sized for JSON; `files` proposes what it needs. |
| Trust between regionals: `/.well-known/` documents, signed records | Q-TRUST, later; such a route is a core route set like any other. |
| Audit of what a handler read or changed | `audit.md`, called by the handler's service. |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `httpx` (lib) | Give every handler, in any layer, one wire form for errors and one way to decode, encode and fail. | `Problem`, `StatusOf`, `Fail`, `ReadJSON`, `WriteJSON`, the ctx accessors for the client address and scheme. Imports `lib` only. |
| Router | Mount route sets under `/api/<name>/`, exactly, with a JSON 404 and 405 from one fallback. | `net/http.ServeMux` (Go 1.22 patterns); no method-less patterns besides `/api/`; conflicts across sets fail `New`. |
| Pipeline | Run every request through the same chain, outer layers before routing, inner layers per route. | Decisions 4–10; the order is in "The middleware chain"; the address derivation is in "Client address and scheme, step by step". |
| Mapper | Turn the error a handler handed over into a status, a problem body, a span error and one log record. | The shell's `httpx.Failer`; recognises the four sentinels. |
| Shell routes | `/healthz`, `/readyz`, `GET /api/modules`, `GET /api/admin/modules`, `POST /api/client-errors`, `GET /api/openapi.json`. | The api's own route set, named `core` for OpenAPI and uniqueness, mounted at absolute patterns and exempt from the `/api/<name>/` rule (decision 20). |
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

// Problem is the body of every error response. It carries no prose the client must
// parse: the web shell translates code (D28) and a field's code (decision 25); a field's
// reason is the validate.FieldError text, shown only when the shell knows no translation
// for the code. Nothing in it is personal data: reasons never echo string input and
// params carry the rule's parameters, never the input (lib-core).
type Problem struct {
	Status int     `json:"status"`           // the HTTP status, repeated for clients that lose it
	Code   string  `json:"code"`             // an errs.Kind name, or one of the shell codes (Behavior: "Codes")
	Ref    string  `json:"ref,omitempty"`    // telemetry.Ref "XXXX-XXXX"; absent when no trace
	Fields []Field `json:"fields,omitempty"` // for code "invalid": one per validate.FieldError, in order
}

// Field is one validation problem, from validate.FieldError (decision 25).
type Field struct {
	Field  string            `json:"field"`            // the dotted path the service used: "email", "shift.start"
	Code   string            `json:"code"`             // validate.FieldError.Code: "required", "range", "one_of", ...; "" when a check set none
	Params map[string]string `json:"params,omitempty"` // validate.FieldError.Params: the rule's parameters ("min", "max", "options"), never the input
	Reason string            `json:"reason"`           // the rule in English, never the value; the shell's fallback
}

// Shell codes that are not errs.Kind names. Codes are stable API strings pinned by a test.
const (
	CodeReadOnly             = "read_only"              // 503: db.ErrReadOnly
	CodeRateLimited          = "rate_limited"           // 429: intake.ErrRateLimited (and later limiters)
	CodeShuttingDown         = "shutting_down"          // 503: canceled by shutdown, client still connected
	CodePayloadTooLarge      = "payload_too_large"      // 413: *http.MaxBytesError
	CodeUnsupportedMediaType = "unsupported_media_type" // 415: ErrUnsupportedMediaType
	CodeCrossOrigin          = "cross_origin"           // 403: an unsafe request from another origin
	CodeMisdirected          = "misdirected"            // 403: a direct request from an untrusted peer (decision 9)
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
// StatusOf(errs.KindOf(err)), the kind's name as code, Fields from validate.Fields (each
// {Field, Code, Params, Reason}, decision 25), no
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
// validate.FieldError whose Field is "body" or the JSON path the decoder reports, cut at
// the first segment that is not a struct field of dst's type (a map key or a slice
// index), so a client-chosen key never reaches a span or a log; its Code is "malformed"
// (no Params) and the Reason never echoes the body (F2). An empty body is Invalid with
// field "body", code "required" (F3). Both codes are in lib-core's code list.
// Unknown fields are ignored (decision 13). A *http.MaxBytesError from the capped body is
// returned unchanged so the mapper answers 413 (F4). A read that fails because the
// connection's read deadline passed (the request deadline, decision 18: a net.Error
// with Timeout() true or os.ErrDeadlineExceeded) is errs.Timeout; a read that fails
// because r.Context() is done takes the ctx's kind; any other read error (the client
// went away mid-body: io.ErrUnexpectedEOF, a reset) is errs.Canceled (F5). dst must be
// a non-nil pointer (F6, Internal). Consumers: every handler that takes a body.
func ReadJSON(r *http.Request, dst any) error

// WriteJSON encodes v into a buffer, then writes status, Content-Type application/json
// and the bytes in one Write; the shell has already set Cache-Control: no-store on API
// responses. It fails with errs.Internal if v cannot be encoded (nothing is written then,
// F7) and with errs.Canceled wrapping the write error when the Write fails for any
// reason (the client is gone or the connection's write deadline passed; F8). The shell's
// response-writer wrapper remembers that first write error, so the span is marked and
// the caller logs nothing. A nil v with status 204 writes no body. Consumers: every
// handler that returns a body.
func WriteJSON(w http.ResponseWriter, status int, v any) error

// ClientAddrFrom returns the client address the shell derived for this request (decision
// 8) and false when ctx carries none (a handler run outside the shell). It is personal
// data: use it for a rate limit, never log it. Consumers: identity (failed-login limits),
// the api's intake handler. Cannot fail.
func ClientAddrFrom(ctx context.Context) (netip.Addr, bool)

// SchemeFrom returns the effective scheme, "http" or "https", the shell derived (decision
// 8), and false when ctx carries none. Consumers: identity (absolute links in e-mails are
// settings' public URL, not this; identity uses it only to decide whether to write the
// development cookie without Secure, decision 24). Cannot fail.
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
	// read-only copy) and must not write to w; the shell never passes one. A nil ctx with
	// a nil error is a bug in the implementation; the shell answers 500 (F51).
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
	// A prefix of zero bits (0.0.0.0/0, ::/0) is refused by Validate: it would trust every
	// peer's headers and switch decision 9 off (F9). A front end on the same host is
	// listed as 127.0.0.1/32 (or ::1/128); otherwise its clients all look like loopback.
	TrustedProxies []netip.Prefix
	// AllowPlaintext serves any peer over plain HTTP, for development; New logs one
	// warning when it is true.
	AllowPlaintext bool

	Logger *slog.Logger     // component=api; never nil
	Tracer telemetry.Tracer // scope "api"; never nil
	Meter  telemetry.Meter  // scope "api"; never nil
	Clock  clock.Clock      // durations, the misdirected log rate, the readiness cache; never nil
	// Version is the build's version, for the OpenAPI info block and GET /api/admin/modules
	// (never /healthz, decision 21). A token of at most 64 bytes (telemetry.Options.Version
	// has the same rule).
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
	// ContentSecurityPolicy is sent with every text/html response (index.html served for
	// an app path or as a file). Empty means DefaultCSP.
	ContentSecurityPolicy string
}

// DefaultCSP is the policy sent with every HTML response when
// Options.ContentSecurityPolicy is empty: same origin for everything, data: images, no
// plugins, no frames, no inline scripts. web-shell.md proposes changes if its build needs
// them.
const DefaultCSP = "default-src 'self'; img-src 'self' data:; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

// Validate fails with errs.Invalid joining one validate.FieldError per problem, fields
// "http.addr", "http.request_timeout", "http.max_body_bytes", "http.trusted_proxies"
// (an invalid prefix, or one of zero bits), "api.logger", "api.tracer", "api.meter",
// "api.clock", "api.version", "api.database", "api.intake", "api.schema",
// "api.routes[i]" (F9).
func (o Options) Validate() error

// New validates opts, builds the pipeline and the router, mounts every route set
// (F10–F13), checks every route's span name and route token against telemetry's grammar
// (F48), marshals the static half of the admin modules response (F52), assembles and
// checks the OpenAPI document (F14–F17), creates the request-duration histogram (F18)
// and returns a Server that is not yet listening. It does no network I/O. Consumers:
// app (once, in `serve`); tests (through Handler).
func New(opts Options) (*Server, error)

// Server is the process's HTTP server. Safe for concurrent use at all times: before
// Listen, while serving, and after Shutdown returns (Handler and OpenAPI keep answering;
// Listen fails; Serve and a second Shutdown return nil).
type Server struct{ /* unexported: http.Server, mux, known patterns, opts, histogram, openapi []byte, adminGraph []byte, draining atomic.Bool, closed atomic.Bool, baseCancel, readiness cache, misdirected log state */ }

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
// or when Shutdown has already been called (F21). Consumer: app, before Serve.
func (s *Server) Listen(ctx context.Context) (net.Addr, error)

// Serve accepts connections on the listener until Shutdown is called, then returns nil.
// Called after Shutdown (app's `go Serve()` losing the race with a signal) it returns
// nil at once. It fails with errs.Internal if Listen was never called (F22) and with
// errs.Unavailable wrapping the server's error if accepting fails for any reason other
// than Shutdown (F23). Consumer: app, in its own goroutine.
func (s *Server) Serve() error

// Shutdown drains: /healthz and /readyz answer 503 at once, the listener closes, in-flight
// requests get Connection: close and run to completion within ctx; when ctx ends first,
// the base context is canceled so stuck handlers fail with Canceled (mapped to 503
// shutting_down) and Shutdown returns errs.Timeout or errs.Canceled (F24). Handlers that
// ignore their ctx may still be running when Shutdown returns with that error (net/http
// does not kill goroutines); what app stops next must tolerate them (Proposed changes 5
// and 11). Idempotent: a second call returns nil. Before Listen it marks the server
// closed and returns nil. Consumer: app, first in the shutdown order.
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
| `httpx.ClientAddrFrom`, `SchemeFrom` | `identity` (login rate limit; the development cookie, decision 24), the intake handler | inside the shell | the derived values, never the raw header |
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
| `GET /healthz` | public | `200 {"status":"ok"}` always (no version: decision 21); `503 {"status":"draining"}` after `Shutdown` began |
| `GET /readyz` | public | `200 {"database":"ok","mode":"read-write"}` or `"read-only"` (db.md Proposed change 4); `503 {"database":"unavailable"}` when `Ping` fails (its kind in the log, never in the body); `503 {"status":"draining"}` after `Shutdown` began. One `Ping` per second at most, shared by concurrent probes (decision 22) |
| `GET /api/modules` | public | `{"modules":[{"name":"hello","enabled":true}, ...]}`: every compiled-in module's name and whether it is enabled, nothing else (decision 15) |
| `GET /api/admin/modules` | signed in, `core.modules.read` | the api-owned type below, lower-case keys: `{"version":"<Version>","graph":{"modules":[...],"edges":[...],"warnings":[...]},"schema":[{"set":"hello","built":1,"applied":1,"current":true}, ...]}`; a set's `current` is `Status.Err == nil`; the error text is not sent (it is in `bms check`). The `graph` half is marshalled once in `New` (F52) |
| `POST /api/client-errors` | public | `202` no body; `400` problem with fields; `415`; `429` with `Retry-After: 60` (telemetry Proposed change 3) |
| `GET /api/openapi.json` | public | the assembled document, `application/json`, `ETag` (SHA-256 of the bytes), `Cache-Control: no-cache` |
| any `/api/` path the mux would redirect (unclean: `//`, `.`, `..` segments; or a path missing a trailing slash a route has) | — | `404 not_found` as JSON; never `ServeMux`'s `301` (decision 3) |
| `GET /{path...}` (not `/api/`, `/healthz`, `/readyz`) | public | a file from `Options.UI`, or `index.html` (decision 12); `404` problem when `UI` is nil |

IDs in these bodies are canonical UUID strings (`model.ID.MarshalText`). The admin graph
carries module names, permission names, setting keys and signal names: none of it is
personal data.

The admin response is an api-owned type, so a change to `module.Graph` cannot change the
wire form unnoticed and no Go field name or func reaches a client:

```go
// adminModulesResponse is the body of GET /api/admin/modules. graph is built from
// module.Graph in New and marshalled once; schema is fresh per request.
type adminModulesResponse struct {
	Version string          `json:"version"`
	Graph   json.RawMessage `json:"graph"`  // adminGraph, below, marshalled in New
	Schema  []adminSchema   `json:"schema"`
}
type adminGraph struct {
	Modules  []adminModule  `json:"modules"`
	Edges    []adminEdge    `json:"edges"`
	Warnings []adminWarning `json:"warnings"`
}
type adminModule struct {
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	HasSchema     bool     `json:"has_schema"`
	Permissions   []string `json:"permissions"`    // PermissionDecl.Name
	Settings      []string `json:"settings"`       // SettingDecl keys; no Validate funcs, no defaults
	Publishes     []string `json:"publishes"`
	Subscribes    []string `json:"subscribes"`
	Provides      []string `json:"provides"`
	Requires      []string `json:"requires"`
	Uses          []string `json:"uses"`
	Defines       []string `json:"defines"`
	ContributesTo []string `json:"contributes_to"`
}
type adminEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"` // EdgeKind.String()
	Via  string `json:"via"`
}
type adminWarning struct {
	Module  string `json:"module"`
	Message string `json:"message"` // module.Warning.Message: English, no personal data by contract
}
type adminSchema struct {
	Set     string `json:"set"`
	Built   int    `json:"built"`
	Applied int    `json:"applied"`
	Current bool   `json:"current"`
}
```

The base fragment documents it as `AdminModulesResponse` with these keys (decision 11).

## Data

No tables, no files written. What a request leaves behind, by field, with its sensitivity
and where it goes (privacy-data; telemetry.md "Data" holds the retention rules):

| Field | Where it goes | Level | Why | Retention |
|---|---|---|---|---|
| client address (derived, decision 8) | the request ctx only (`httpx.ClientAddrFrom`); `intake.Submission.From` (memory bucket, never logged); the login rate limit (`identity`) | personal | rate limits | gone when the request ends |
| peer network prefix (`/24`, `/64`) of a misdirected request | one warn log line per prefix per hour, for at most 1,000 prefixes; beyond that one aggregate line per minute with a count (decision 9) | internal (a network, not a device) | an operator fixes `trusted_proxies` | the collector's |
| `person_id` | the request span and the per-error log record | internal (opaque ID) | whose request failed | the collector's |
| `http.request.method` (normalised, decision 10), `http.route` (pattern), status, `duration_ms`, `error.kind`, `op`, `error_ref`, `trace_id` | span, histogram (method, route, status only), the per-error log record | not about a person | find and count failures | the collector's |
| `error.message` (`telemetry.Text`: scrubbed, capped) | the per-error log record of a 5xx only | not about a person once scrubbed; never for a 4xx, whose messages may restate input | a maintainer reads what broke without opening a trace | the collector's |
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
| `WriteTimeout` | `RequestTimeout + 15 s` | a backstop behind the ctx deadline, sized so that two 5 s signal deliveries after a commit near the deadline still get their response written (decision 18); `files.md` revisits it for downloads |
| `MaxHeaderBytes` | 64 KiB | `net/http`'s default would allow 1 MiB of headers |
| readiness ping deadline; readiness cache | 2 s; 1 s | a probe must answer fast; `db.Ping` refreshes the read-only state in the same round trip; concurrent probes share one `Ping` and its result is reused for 1 s (decision 22) |
| intake body cap | 64 KiB | telemetry Proposed change 3 |
| HSTS | `Strict-Transport-Security: max-age=31536000`, exactly; no `includeSubDomains`, no `preload` (decision 26) | one year is the common value; a regional that controls every subdomain adds `includeSubDomains` at the front end |
| misdirected-request log rate; tracked prefixes | once per peer prefix per hour; at most 1,000 prefixes, then one aggregate line per minute | enough to notice, not enough to flood, and bounded memory under a scan |
| untrusted-forwarded-header warning | once per process | a same-host front end that is not in `trusted_proxies` (decision 9) |
| UI asset cache | `public, max-age=31536000, immutable` under `/assets/`; `no-cache` elsewhere | hashed names make it safe |
| `DefaultCSP` | above | web-shell.md proposes changes; the regional's branding (a logo) is served from the same origin |

## UI

None of its own. What this spec provides for `web-shell.md`:

- **Error handling:** every error is `application/problem+json` with `code`, `ref` and
  `fields`; the shell translates `code` (keys `error.<code>`), shows `ref` with the
  wording in telemetry.md "UI" for every code except `invalid`, `unauthenticated`,
  `read_only` and `network` (web-shell decision 9), maps
  `fields[].field` to form fields and translates each field's `code` with its `params`
  (keys `field.<code>`), showing `reason` as-is for a code it does not know (decision
  25). `401` on a signed-in route sends the shell to the
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
3. **Mounting: `<METHOD> /api/<name><pattern>`, trailing slashes made exact, one
   method-less `/api/` fallback that answers 404 or 405, no `ServeMux` redirects under
   `/api/`, conflicts caught at `New`.** A pattern ending in `/` is a subtree in
   `ServeMux`, so a module route `/` would swallow `/api/<name>/…`; the shell appends
   `{$}` to every pattern ending in `/`, so no module can register a catch-all (a module
   that wants a wildcard says `{path...}`). The shell registers **no other method-less
   pattern**: a method-less `/api/a/shifts/{id}` beside `POST /api/a/shifts/bulk` is a
   `ServeMux` conflict (neither is more specific than the other), so a per-path 405
   registration would make `New` fail on ordinary route sets. Instead the one `/api/`
   subtree handler decides between 404 and 405: for each of `GET`, `HEAD`, `POST`,
   `PUT`, `PATCH`, `DELETE` it clones the request (`r.Clone(ctx)` with that method) and
   asks `mux.Handler(clone)`; a method counts as allowed when the returned pattern is one
   the shell registered other than `/api/` (a redirect's "pattern" is a bare path and is
   never in that set). Some allowed → `405 method_not_allowed` with `Allow` in that
   fixed order (`HEAD` is listed whenever `GET` is: `ServeMux` serves `HEAD` through `GET`
   patterns); none → `404 not_found`. Both are JSON problems, where `ServeMux`'s own
   answers are plain text. The probe costs at most six tree lookups and runs only for
   requests that matched nothing. `ServeMux` also answers `301` for an unclean path and
   for `/x` when `/x/{$}` exists; a JSON API does not redirect, so the dispatch layer
   asks `mux.Handler(r)` first and, when the pattern is not one the shell registered
   (the mux chose a redirect), answers `404 not_found` for an `/api/` path (F41); for UI
   paths the mux's `301` to the clean path stands, since a browser follows it. The
   lookup is repeated by `mux.ServeHTTP`, which is what sets path values; `mux.Handler`
   does not, so the shell never dispatches from its probe. `ServeMux.Handle` panics on a
   conflicting pattern; `New` recovers that panic and returns `errs.Conflict` naming both
   sets and patterns (F12), so a bad set stops start-up with a message, not a stack
   trace.
4. **One problem body (RFC 9457), no prose, one log line per error response, nothing for
   a success.** `application/problem+json` is what HTTP tooling and generated clients
   already understand; `code` is machine-readable (an `errs.Kind` name or a shell code),
   the web shell translates it (D28), and `err.Error()` never reaches a client (lib-errs
   decision 8). The log record carries `error_ref`, `trace_id`, `error.kind`, `op`,
   `http.route`, `http.request.method`, `status`, `person_id`, `duration_ms`, and for a
   5xx also `error.message` as `telemetry.Text` (scrubbed and capped; a maintainer sees
   "dial tcp: connection refused" without opening a trace; never for a 4xx, whose
   messages may restate input), so a reference is a one-field Loki query (telemetry
   decision 3). **Levels:** error for every 5xx; warn for a 4xx from a signed-in actor
   (a bug in the shell, or a person hitting a permission wall worth seeing); debug for a
   4xx with no actor (an anonymous 4xx is Internet noise: scanners, stale bookmarks,
   bots probing methods; the histogram still counts them by status), for an unmatched
   route and for a `401` with no session at all. The alternative, rate-limiting warn
   lines per route and code, needs state and a drop rule for the same effect.
   Successful requests leave a span and a histogram sample and no log line: a
   regional's logs stay readable.
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
   is anything but `same-origin` or `none` (so `cross-site` and `same-site` are both
   rejected: a sibling subdomain is not trusted) and, when that header is absent, one
   whose `Origin` is `null` or whose `Origin` host differs from the request `Host`; it
   lets through requests that carry neither header (non-browser clients such as `curl`
   with a cookie). Every modern browser sends `Sec-Fetch-Site`; the SPA's calls are
   same-origin; and `SameSite=Lax` already keeps the cookie off cross-site `POST`s. A
   token scheme would add a cookie, a header, a rotation rule and a failure mode for
   nothing. The cost is one deployment rule that matters only for browsers without
   `Sec-Fetch-Site` (the `Origin`-versus-`Host` fallback): the front end must preserve
   the `Host` header (Caddy does by default; the nginx example sets it; Proposed change
   14); a public URL on another host becomes a trusted origin when `settings` exists
   (Proposed change 15). A rejected request is `403 cross_origin`, logged without the
   origin value (it may carry anything). No `Access-Control-*` header is ever sent: the
   API has one origin, its own. config.md's proposal said "session and CSRF cookies";
   with this decision there is only the session cookie (Proposed change 4).
7. **Cookies are `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, carry no `Domain`, and
   the shell refuses a response that sets one otherwise.** config.md decision 16(d)
   makes the attributes a rule; a rule nobody checks drifts. The cookie guard parses
   every `Set-Cookie` with `http.ParseSetCookie` at the moment the header is about to go
   out: at `WriteHeader`, at the first `Write` without one (the implicit `200`), and
   after the handler returns having written nothing (`net/http` would then send `200`
   with the headers as they stand). A cookie that fails to parse, lacks any attribute or
   carries a `Domain` (which would send it to every subdomain) makes the response
   `500 internal` (possible in all three cases: nothing has been sent yet) and logs the
   cookie's name (never its value): a developer finds out in the first test. Rewriting
   the cookie silently was rejected (it hides the bug). The session cookie's name is
   `__Host-<name>` (Proposed change 9): the browser then enforces `Secure`, no `Domain`
   and `Path=/` on its side too, which is exactly this rule. The one exception, a
   development cookie without `Secure` under a second, unprefixed name, is decision 24.
8. **The client address and scheme come from `X-Forwarded-For` and `X-Forwarded-Proto`
   only when the peer is in `TrustedProxies`, by the rightmost-untrusted rule; otherwise
   from the connection.** Walking `X-Forwarded-For` from the right and skipping trusted
   addresses yields the first address a trusted hop saw, which a client cannot forge by
   prepending. From any other peer the headers are ignored entirely, never partially
   trusted (config.md decision 16(b)). The scheme is `https` only when the last
   `X-Forwarded-Proto` value from a trusted peer is exactly `https` (case-insensitive),
   else `http` (the binary never terminates TLS). Every address is `Unmap()`ped before
   any prefix or loopback test, so `::ffff:10.0.0.1` is `10.0.0.1`. A prefix of zero
   bits is refused (F9): it trusts everyone and turns decision 9 off. `Forwarded` (RFC
   7239) is not parsed: no front end in `deploy/` emits it by default, and two header
   grammars are two sets of bugs. The exact algorithm, including malformed entries, is
   "Client address and scheme, step by step". The derived values travel in ctx
   (`httpx.ClientAddrFrom`, `SchemeFrom`) so `identity` and `intake` never read the raw
   headers.
9. **A direct request from a peer that is neither loopback nor in a non-empty
   `TrustedProxies` is answered `403` with code `misdirected` and logged once per peer
   prefix per hour (at most 1,000 prefixes tracked); `/healthz` and `/readyz` are
   exempt; `AllowPlaintext` turns the check off.** config.md decision 16 and its
   Proposed change 5 asked for this decision and suggested `421`. `421 Misdirected
   Request` (RFC 9110 §15.5.20) means "this connection is not authoritative for that
   `Host`", and an HTTP/2 client that receives it is expected to retry on another
   connection; the condition here is "this peer may not reach this listener", which is a
   refusal, and `403` says so without inviting a retry. The `code` keeps the name
   `misdirected` so the log and the body say what happened (Proposed change 4 records
   the change). Serving such a request would serve plaintext on a network path the
   operator did not declare; refusing it makes a misrouted path visible without serving
   on it. The health routes stay reachable so an orchestrator's probe from a node address
   works without being added as a "proxy". The log carries the peer's `/24` or `/64`,
   enough to fix the configuration and not a device identifier (privacy-data). The
   per-prefix state is capped at 1,000 entries (entries older than an hour are evicted
   first); once full, new prefixes are not tracked and one aggregate warn line per minute
   reports how many requests from how many untracked prefixes were refused, so a scan
   cannot grow memory or flood the log. With `TrustedProxies` empty, `config` already
   forces a loopback bind, so every peer is loopback and the rule has nothing to do.
   **A same-host front end that is not listed** is the one misconfiguration this rule
   cannot catch (its peer is loopback): every client then shares the address
   `127.0.0.1`, which collapses rate limits onto one bucket and never yields `https`
   (so HSTS is never sent). The shell warns once at start when `Addr` binds loopback and
   `TrustedProxies` is non-empty but contains no loopback address (nothing listed can
   ever connect), and once per process at runtime when a loopback peer that is not
   trusted sends `X-Forwarded-For` (a front end is there and its headers are being
   ignored); the deploy examples set `trusted_proxies=127.0.0.1/32` for a same-host proxy
   (Proposed change 14).
10. **The request span is opened per route, after routing, so it can be named
    `<METHOD> <route token>` at `Start`; the method is normalised first; the route
    token renders `{$}` as a trailing `/`.** telemetry's `Span` has no rename method
    (deliberately: a name set late escapes the grammar check), and the pattern is known
    only once `ServeMux` chose a handler. So the outer layers (recovery, peer check,
    address, headers, body cap, deadline, draining, the unclean-path check) run before
    the mux, and the span, histogram, mapper, cookie guard, cross-origin check and
    authentication are wrapped around each registered handler, where the pattern is a
    constant. **The method is untrusted input:** a client may send any token as a
    method, and it would otherwise become a span name, an attribute and a histogram
    label. The shell maps the method to one of `GET`, `POST`, `PUT`, `PATCH`, `DELETE`,
    `HEAD`, `OPTIONS` (exact, case-sensitive, as `ServeMux` matches them) and everything
    else to `_OTHER` (OpenTelemetry's HTTP semantic conventions) before any of the three;
    for `_OTHER` the span name is `HTTP <route token>` (the same conventions: the method
    part is `HTTP` when the method is unknown), and the original method is never
    recorded (F49). `_OTHER` begins with an underscore, which telemetry's `Token`
    grammar admits as a first byte for exactly this value (telemetry.md decision 20,
    owner 2026-10-17, applied by Proposed change 6). **The route token** is
    the registered pattern with `{$}` rendered as the trailing `/` it stands for
    (`/api/alpha/{$}` → `/api/alpha/`), because `$` is not in the token grammar and the
    OpenAPI path for that route is `/api/alpha/` anyway (decision 11); `New` checks
    every mounted route's token and full span name against the grammar
    (`[A-Za-z0-9/_][A-Za-z0-9._:/{}-]{0,127}`, the whole name at most 128 bytes) and
    refuses a route that fails (F48): `module.Route.Validate` only requires a leading
    `/` and no space, so a pattern with `%`, `+`, `,` or `;`, or a very long one, is
    possible and must be caught before it becomes `invalid-span-name` at runtime.
    Unmatched requests get the route token `/{unmatched}` (one series, not one per
    probed path), whether they end as `404` or `405`. `/healthz` and `/readyz` get no
    span and no histogram sample: a probe every ten seconds would be most of a quiet
    regional's traces. telemetry Proposed change 3 is answered with this refinement
    (Proposed change 6).
11. **OpenAPI 3.1 as JSON; fragments are relative; routes and fragments must agree at
    start-up; a set with routes must have a fragment.** JSON because the standard library
    reads it and the document is served as JSON anyway (a YAML parser would be a
    dependency for authoring comfort). A fragment is one JSON object with optional
    `paths` (keys relative to `/api/<name>`, beginning with `/`), `components.schemas`,
    and `tags`; the shell prefixes the paths, merges the schemas (a duplicate schema
    name across sets is `Conflict`, naming both sets; the convention is to prefix schema
    names with the module name), merges the tags, adds the base fragment (`Problem`,
    `Field`, `ID` as `string`/`format: uuid`, `Sensitivity` as the four-name enum,
    `TimeRange` as `{start, end}` of `date-time`, `AdminModulesResponse`; the shell's own
    routes), adds a `default` response referencing `Problem` to every operation that
    lacks one, and writes `openapi`, `info` (title `BMS API`, `version` =
    `Options.Version`). **Route-to-path translation**, both ways: a `ServeMux` pattern
    `/x/{$}` is the OpenAPI path `/x/` (and a route `/` is `/api/<name>/`); a wildcard
    `{p...}` is the path parameter `{p}`; a plain `{id}` is `{id}`; the method is the
    lower-case operation key. `HEAD` needs no operation: `ServeMux` serves it through the
    `GET` pattern, and a `head` operation in a fragment, if present, is checked against
    the `GET` route. **The check:** every route in a set has an operation at its method
    and translated path and every operation has a route, else `Invalid` naming the first
    mismatch (F15); every `$ref` is local (`#/components/schemas/<Name>`) and resolves
    against the **merged** document, where a set may reference its own schemas and the
    base fragment's (`Problem`, `ID`, ...) but not another set's: that would be a module
    depending on a module (F16). module-contract made `APIDescriber` optional; the web
    shell's screens are built from this document (08-agent-workflow), so a route without
    documentation is a route the front end cannot call correctly. This spec requires the
    fragment when a set has routes (F14) and proposes the contract say so (Proposed
    change 3). A full schema validator is not linked in: the structural check here plus
    a lint of `/api/openapi.json` in the web tooling (Proposed change 12) cover it.
12. **The shell serves the web UI: hashed assets immutable, `index.html` for every app
    path, the CSP on every HTML response, `no-store` on the API.** D15 says the binary
    serves the UI; a second server or a second listener would be more to operate. Paths
    outside `/api/`, `/healthz` and `/readyz` are served from `Options.UI`: a file that
    exists is sent with `Cache-Control: public, max-age=31536000, immutable` under
    `/assets/` (Vite hashes those names) or `no-cache` otherwise; a path with no file and
    no extension is the SPA's and gets `index.html` with `no-cache`; a path with an
    extension and no file is `404`. Every response whose `Content-Type` is `text/html`
    carries the CSP, whether `index.html` was chosen for an app path or requested by name
    (`/index.html`, `/`): a policy that depends on the URL is a bypass. Every `/api/`
    response carries `Cache-Control: no-store`, except `/api/openapi.json` (`no-cache`
    with an `ETag`) and `/api/modules` (`no-cache`), which hold nothing personal. With
    `UI` nil (a headless build or a test) non-API paths are `404`.
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
    each one simple. The public half is confirmed (owner, 2026-10-17; decision 27).
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
    is answered (Proposed change 7, applied 2026-10-17).
18. **The request deadline is the ctx and the connection's read deadline; the server's
    `WriteTimeout` is a backstop.** `http.TimeoutHandler` was rejected: it buffers every
    response and answers `503` with its own body. Instead `RequestTimeout` becomes the
    request ctx's deadline, so every I/O under it fails with `Timeout` and is mapped to
    `504`. A ctx deadline does not interrupt `Body.Read`, so a client that sends its
    body one byte a minute would hold a handler for as long as it likes (the body cap
    bounds bytes, not time); the deadline layer therefore also calls
    `http.NewResponseController(w).SetReadDeadline(deadline)` with the same instant, so
    a stalled body read fails with a timeout error that `ReadJSON` maps to `Timeout`
    (F5). Every response-writer wrapper in the chain implements `Unwrap()
    http.ResponseWriter` so the controller reaches the connection. `ReadTimeout` stays
    unset: `ReadHeaderTimeout` covers the headers, the per-request read deadline covers
    the body, and a server-wide `ReadTimeout` would have to be the sum of both.
    `WriteTimeout` does not stop a handler either (it only fails later writes), so it is
    sized as `RequestTimeout + 15 s`: a request that commits just before its deadline
    then delivers its signals synchronously on a ctx without the request's cancellation
    (signals.md decision 6, up to 5 s per handler) and still has to write its response;
    15 s covers two such handlers. When even that passes, the commit and the deliveries
    stand, the response write fails, `WriteJSON` returns `Canceled` (F8) and the client
    sees a closed connection; it must re-read, not retry the write.
19. **Every response carries `X-Trace-Id`.** telemetry decision 2 allowed it; it makes a
    `curl` session traceable without reading a body, and the ID is not a secret.
20. **The shell's own routes are mounted at absolute paths and are exempt from the
    `/api/<name>/` rule.** `/api/modules`, `/api/admin/modules`, `/api/client-errors` and
    `/api/openapi.json` are what module-contract, telemetry (its Proposed change 3 and a
    pinned span-name test) and the web shell already name; moving them under
    `/api/core/` would buy uniformity and cost three Approved specs a change. The set is
    named `core` for OpenAPI tags and set-name uniqueness only; nothing is mounted at
    `/api/core/`. Collisions are impossible by the module-name grammar
    (`[a-z][a-z0-9]{1,31}`): `client-errors` and `openapi.json` contain `-` and `.` and
    can never be a set name, and `modules` and `admin` are already reserved module names
    (module-contract "Reserved module names"); `New` additionally refuses a set in
    `Options.Routes` named `core`, `modules` or `admin` (F10), so a core route set from
    `app` cannot take them either.
21. **`/healthz` carries no version.** It is public and unauthenticated, and a release
    string tells a stranger which known bugs this instance has. An operator reads the
    version from `GET /api/admin/modules` (signed in, `core.modules.read`) or from the
    process's start-up log line; a liveness probe needs only `200`.
22. **`/readyz` single-flights and caches its `Ping` for one second.** The route is public
    and unthrottled, and each call was a database round trip, so anyone could turn a
    probe into a load test. Concurrent calls share one in-flight `Ping` (a mutex and a
    pending result; `sync` only) and its outcome is reused until one second has passed
    by `Clock`; a probe interval of one second or more sees fresh answers, and a flood
    costs one round trip per second. Reading db's own watcher state instead was rejected
    because `Ping` is also what refreshes the read-only mode (db.md).
23. **Serve after Shutdown is a quiet nil; Listen after Shutdown is a `Conflict`.** `app`
    starts `Serve` on a goroutine and may receive a signal before it runs, so `Serve`
    finding the server closed is the normal shutdown path, not an error. `Listen` after
    `Shutdown` can only be a programming error. `Shutdown` before `Listen` marks the
    server closed and returns nil, so the same order works whether or not binding
    happened.
24. **A cookie may lack `Secure` in exactly one case: `AllowPlaintext` is on and the
    request's effective scheme is `http`; it then goes under the separate, unprefixed
    development name `bms_session_dev`; the normal session cookie is `__Host-`-prefixed
    and always `Secure`** (owner, 2026-10-17). config.md decision 16(d) says cookies are
    `Secure` whatever the hop, which holds whenever TLS exists somewhere (the effective
    scheme is then `https`) and for `http://localhost`, which browsers treat as a secure
    context. The one case left is `http.allow_plaintext=true` with a direct plain-HTTP
    request to a LAN address (a phone on the Wi-Fi during development): no TLS anywhere,
    not a secure context, so a `Secure` cookie is never sent back and login is impossible.
    A browser rejects a `__Host-` cookie without `Secure`, so the exception cannot reuse
    the production name; a second name keeps the production cookie's rule intact. The
    rule, enforced by the cookie guard (F29): a `Set-Cookie` named `bms_session_dev` may
    omit `Secure` only when `Options.AllowPlaintext` is true and `httpx.SchemeFrom(ctx)`
    is `http`; every other attribute still applies to it; a `bms_session_dev` cookie in
    any other condition, or any other cookie without `Secure`, is the usual `500`.
    `identity` writes it only under the same two conditions (reading
    `httpx.SchemeFrom`), and never writes both cookies on one response. The start-up
    warning for `AllowPlaintext` (F35) already says plaintext is for development. The
    alternative, "no login over plaintext from a LAN address", was rejected because
    testing on a phone is how a volunteer-facing screen gets checked.
25. **Field errors carry a machine-readable code** (owner, 2026-10-17).
    `validate.FieldError` gains `Code` and `Params` (lib-core.md, applied by Proposed
    change 8), each check sets its code (`required`, `range`, `one_of`, `url`,
    `host_port`, `positive`; `httpx.ReadJSON` sets `malformed` and `required`), the wire
    form is `fields[].code` and `fields[].params`, and the web shell translates by code
    (keys `field.<code>`, with `params` as the message's arguments), falling back to
    `reason` for a code it does not know. `Reason` stays English because D28 wants every
    string a person sees to be translation-ready and the reason text is not; a code with
    parameters is what a translator can work from, and the fallback means an unknown code
    still shows something true. `Params` are strings because the shell only interpolates
    them; they carry the rule's parameters (bounds, options), never the input.
26. **HSTS is `max-age=31536000` without `includeSubDomains`** (owner, 2026-10-17).
    config.md Proposed change 5 suggested `includeSubDomains`. A regional may serve other
    things on subdomains of the bms host (a wiki, a ticket page) that are not on HTTPS
    yet, and `includeSubDomains` would break them for a year. The header is sent only
    when the effective scheme is `https` (chain step 4). A regional that controls every
    subdomain adds `includeSubDomains` (and `preload`) at its front end; the operator docs
    say so (Proposed change 14).
27. **`GET /api/modules` is public and lists enabled module names only; the graph and the
    schema versions stay behind `core.modules.read` on `GET /api/admin/modules`** (owner,
    2026-10-17, confirming decision 15). The alternative, requiring a session, means the
    web shell cannot know which modules exist before sign-in and M0's "a hello module
    shows up" needs a login that does not exist until M1. The public body is exactly
    `{"modules":[{"name":..., "enabled":...}]}`: no permissions, settings, signals,
    versions or warnings (`TestModules_PublicListsNamesAndEnabled` pins it).

### The middleware chain

Outer layers, before routing, in order:

1. **Recovery (outer).** A panic anywhere below becomes `500 internal` with a problem
   body if nothing was written, and an error log with `stack`; the process never dies for
   one request (F25). `http.ErrAbortHandler` is re-panicked unchanged: it is `net/http`'s
   own way to drop a connection quietly, and the server logs nothing for it. Recovery
   covers the request goroutine only: a goroutine a handler starts is outside every
   layer here and a panic in it ends the process, so handlers start none (long work is a
   job, `jobs.md`; a reaction is a signal handler, which `signals` wraps itself).
2. **Peer check.** `TrustedProxies` non-empty, `!AllowPlaintext`, peer not loopback and
   not in the list, path not `/healthz` or `/readyz` → `403 misdirected` (decision 9,
   F26). The peer address is parsed from `r.RemoteAddr` and unmapped here, once, for
   this step and the next (F50 when it does not parse).
3. **Client address and scheme** derived ("Client address and scheme, step by step") and
   put in ctx; the one-per-process warning for an untrusted loopback peer that sends
   `X-Forwarded-For` (decision 9, F56).
4. **Headers:** `X-Content-Type-Options: nosniff`, `Referrer-Policy:
   strict-origin-when-cross-origin`, `X-Frame-Options: DENY`, `Permissions-Policy:
   camera=(), microphone=(), geolocation=()`, `Cross-Origin-Opener-Policy: same-origin`,
   `Cross-Origin-Resource-Policy: same-origin`; `Strict-Transport-Security` when the
   scheme is `https`; `Cache-Control: no-store` for `/api/` paths (decision 12);
   `X-Read-Only: true` when `Database.ReadOnly()`. The CSP is added by whatever writes a
   `text/html` response (the UI handler, decision 12). No `Access-Control-*` header is
   ever set (decision 6).
5. **Body cap:** `r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)` (F27).
6. **Deadline:** `ctx, cancel := context.WithTimeout(r.Context(), RequestTimeout)`, then
   `http.NewResponseController(w).SetReadDeadline(deadline)` (decision 18; F28). An
   error from `SetReadDeadline` other than `http.ErrNotSupported` is a bug in the chain
   and answers `500 internal` (F53); `ErrNotSupported` happens only off a real
   connection (an `httptest.ResponseRecorder`) and is logged once at debug.
7. **Draining:** after `Shutdown` began, `Connection: close` on the response.
8. **Dispatch.** For an `/api/` path, `mux.Handler(r)` is consulted: a pattern the shell
   did not register means the mux chose a redirect (an unclean path, or a missing
   trailing slash), and the answer is `404 not_found` with the route token
   `/{unmatched}` (decision 3, F41). Otherwise `mux.ServeHTTP(w, r)`.

Then `ServeMux`. Per registered route (every mounted route, the shell's routes, the
`/api/` fallback; not the health routes, not UI files), in order:

9. **Span and histogram:** the method is normalised (decision 10, F49); then
   `Tracer.Start(ctx, "<METHOD> <route token>", WithKind(SpanServer),
   WithParentFrom(r.Header), WithAttrs(Token("http.request.method", m),
   Token("http.route", token)))`, where the name is `HTTP <route token>` for `_OTHER`;
   `X-Trace-Id` from `TraceIDFrom`; on return, `SetAttrs(Int("http.response.status_code",
   st), Duration("duration_ms", d))`, plus `Bool("http.response.aborted", true)` and
   `RecordError` when the wrapper recorded a write error (F8);
   `Histogram.Record(seconds, method, route, status)`, `End`.
10. **Mapper installed:** `httpx.WithFailer(ctx, mapper)`; the response writer is wrapped
    to record the status, whether the header was written, the bytes written and the
    first write error; the wrapper implements `Unwrap`.
11. **Recovery (inner):** a panic in a handler is recorded on the span (`RecordError`) and
    mapped as in step 1 (F25).
12. **Cookie guard:** `Set-Cookie` checked at `WriteHeader`, at the first `Write` without
    one, and after the handler returns having written nothing (decision 7, F29); the
    `bms_session_dev` exception of decision 24 is decided here from `Options.AllowPlaintext`
    and the scheme in ctx.
13. **Cross-origin protection** for `POST`, `PUT`, `PATCH`, `DELETE` (decision 6, F30).
14. **Authentication:** `Authenticator.Authenticate`; the actor's `person_id` on the span;
    `AccessSignedIn` without a valid session → `401` (F31–F33); a nil ctx with a nil
    error → `500` (F51).
15. The handler. Public shell routes skip step 14's enforcement but not its attribution.

### Client address and scheme, step by step

Inputs: the peer `p` (from `r.RemoteAddr`, F50), `TrustedProxies` (no zero-bit prefix,
F9), the header lines of `X-Forwarded-For` and `X-Forwarded-Proto`. `trusted(a)` is
"some prefix contains `a.Unmap()`"; `loopback(a)` is `a.Unmap().IsLoopback()`.

1. `p = p.Unmap()`. If `!trusted(p)`: client = `p`, scheme = `http`; both headers are
   ignored entirely. (A loopback peer that is not listed is untrusted too; decision 9's
   warning fires once if it sent `X-Forwarded-For`.) Stop.
2. Join every `X-Forwarded-For` line, in order, with `,`; split on `,`; trim ASCII
   spaces from each entry. The result is `L[0..n-1]`. If `n == 0` (no header): client =
   `p`. Go to step 5.
3. Walk `i` from `n-1` down to `0`. Parse `L[i]` with `netip.ParseAddr`; if that fails,
   with `netip.ParseAddrPort` and take its address (some proxies append a port to IPv4
   entries); if both fail, or the entry is empty, the entry is **malformed**: stop the
   walk and use the hop to its right: `L[i+1]` (already parsed and trusted, or the walk
   would have stopped there) when `i < n-1`, else `p`. Otherwise `a = parsed.Unmap()`:
   if `!trusted(a)`, client = `a`, stop; if `trusted(a)`, continue left.
4. If the walk ran off the left end (every entry trusted): client = `p`.
5. Scheme: take the last `X-Forwarded-Proto` line, split it on `,`, take the last entry,
   trim; scheme = `https` when `strings.EqualFold(entry, "https")`, else `http`
   (absent, empty, `HTTPS,http`, `wss`, `on`: all `http`).
6. `httpx.WithClientAddr(ctx, client)`, `httpx.WithScheme(ctx, scheme)`.

Each numbered case is a row of `TestPipeline_ClientAddr_Table` (test plan). Nothing in
this derivation is logged; the warning in step 1 names no address.

### The mapper, step by step (`httpx.Fail` on a shell request)

1. If the header was already written: log at error "error after response started" with
   `op` and kind; return (F34).
2. Decide status and code, in this order: `errors.Is(err, db.ErrReadOnly)` → `503
   read_only`; `errors.Is(err, intake.ErrRateLimited)` → `429 rate_limited`,
   `Retry-After: 60`; `errors.Is(err, httpx.ErrUnsupportedMediaType)` → `415`;
   `errors.As(err, *http.MaxBytesError)` → `413`; kind `Canceled` → decision 17 (write
   nothing, or `503 shutting_down` with `Connection: close`); otherwise `StatusOf(kind)`
   with `Kind.String()` as code; `Unavailable` adds `Retry-After: 5`.
3. `Fields` = `validate.Fields(err)` when code is `invalid`, each `{Field, Code, Params,
   Reason}` (decision 25).
4. `Ref` = `RefFrom(ctx)` when present.
5. Write the problem with `ContentTypeProblem`; `span.RecordError(err)`; log one record
   at the level decision 4 gives (error for 5xx; warn for a 4xx with an actor; debug for
   a 4xx without one) with `error_ref`, `trace_id`, `error.kind`, `op` (the outermost
   `errs.Error.Op`), `http.route`, `http.request.method`, `status`, `person_id`, and
   for a 5xx `error.message` as `telemetry.Text(err.Error())`.

### Codes

| Status | `code` | When |
|---|---|---|
| 400 | `invalid` | `errs.Invalid`, with `fields` |
| 401 | `unauthenticated` | signed-in route without a valid session; `errs.Unauthenticated` from a handler |
| 403 | `permission` | `errs.Permission` |
| 403 | `cross_origin` | decision 6 |
| 403 | `misdirected` | decision 9 |
| 404 | `not_found` | `errs.NotFound`, including a record the caller may not know exists (the service's choice, privacy-data); an unmatched or unclean `/api/` path; a UI file path with an extension that does not exist |
| 405 | `method_not_allowed` | the path exists for other methods; `Allow` lists them |
| 409 | `conflict` | `errs.Conflict` |
| 413 | `payload_too_large` | the body cap |
| 415 | `unsupported_media_type` | `ReadJSON` on a non-JSON body |
| 429 | `rate_limited` | `intake.ErrRateLimited`; later limiters wrap the same sentinel or their own |
| 500 | `internal` | `errs.Internal`, an unkinded error, a panic, a cookie-guard refusal |
| 501 | `unsupported` | `errs.Unsupported` |
| 503 | `unavailable` | `errs.Unavailable` (`Retry-After: 5`) |
| 503 | `read_only` | `db.ErrReadOnly` |
| 503 | `shutting_down` | `errs.Canceled` while draining |
| 504 | `timeout` | `errs.Timeout` |

### `New`, step by step

1. `opts.Validate()` (F9, including the zero-bit prefix); log the plaintext warning when
   `AllowPlaintext` (F35, not an error) and the loopback-bind warning of decision 9
   (F55, not an error); create the histogram `http.server.request.duration` (unit `s`;
   F18).
2. Build the shell's own route set (`core`): the four `/api/` routes above at their
   absolute patterns (decision 20) with their fragment.
3. For each set in `Routes`: the set name must be a module name (`[a-z][a-z0-9]{1,31}`),
   unique, and not `core`, `modules` or `admin` (F10); each route must validate (F11;
   `Build` did it for modules, the core sets are checked here); build the full pattern
   `/api/<name><pattern>`, append `{$}` to a trailing `/` (F13 if the result is `/api/`
   or does not begin with `/api/<name>/`); derive the route token (decision 10) and
   check the token and `<METHOD> <token>` against telemetry's grammar and the 128-byte
   cap (F48); register the route handler wrapped in the per-route layers
   (`ServeMux.Handle` under `recover`; a conflict → F12); remember the pattern string
   in the known set. The shell's own set goes through the same steps minus the prefix
   rule.
4. Register `/api/` (method-less) as the one fallback that answers `404` or `405` by
   probing (decision 3). No other method-less pattern is registered.
5. Build `adminGraph` from `Options.Graph` and marshal it once (F52).
6. Assemble the OpenAPI document (decision 11; F14–F17) and compute its `ETag`.
7. Mount `/healthz`, `/readyz` and, when `UI` is non-nil, the UI handler at `/`.
8. Build `http.Server{Handler, ReadHeaderTimeout, IdleTimeout, WriteTimeout,
   MaxHeaderBytes, BaseContext}` with a cancelable base context; return.

### Serving the UI, step by step

1. Method not `GET` or `HEAD` → `405`. Path cleaned (`path.Clean`); `..` cannot escape an
   `fs.FS`, and a path containing a NUL or not beginning with `/` is `404`.
2. `fs.Stat`: a regular file → serve it with `http.ServeFileFS`-equivalent content
   negotiation (`Last-Modified` from the embedded build time when known) and the cache
   header by prefix; a directory → treat as missing.
3. Missing with an extension (`path.Ext != ""`) → `404` problem. Missing without one →
   `index.html` with `Cache-Control: no-cache` and `Content-Type: text/html;
   charset=utf-8`.
4. Whatever the branch, a response whose `Content-Type` is `text/html` gets the CSP
   (decision 12), so `/`, `/index.html` and an app path all carry it.

### Concurrency and lifecycle

`*Server` is safe for concurrent use at all times. `New` builds everything; `Listen`
binds once (F21) and never after `Shutdown`; `Serve` runs the accept loop on the caller's
goroutine and returns nil whether `Shutdown` came before or during it (decision 23);
`Shutdown` is idempotent and may precede `Listen`. The only goroutines are `net/http`'s
per connection. `Shutdown` closes the listener, waits for in-flight requests within ctx,
and when ctx ends first cancels the base context (decision 17) and returns `Timeout` or
`Canceled`; **the handlers that ignored their ctx are still running then**, because
`net/http` cannot kill a goroutine, and they stop only when their next I/O fails or
`WriteTimeout` fails their write. So what `app` stops next must tolerate a late caller:
`Set.Stop` and `db.Close` must not block on it (Proposed change 5 asks db.md to give
`Close` a ctx deadline so a held connection cannot hang shutdown), and `app` should give
HTTP a sub-deadline inside `http.shutdown_timeout` so that `signals.Close`, `Set.Stop`,
`db.Close` and `telemetry.Close` keep their share (Proposed change 11). `app`'s order:
`Shutdown` first, then the runnables (jobs, M4), `signals.Close`, `Set.Stop`, `db.Close`,
`telemetry.Close` (signals.md Proposed change 5; app.md decision 10). **Drain timing:** `/readyz` answers `503` the instant
`Shutdown` is called, but a balancer that polls it only learns on its next probe, and
the listener is already closed, so for up to one probe interval new connections to this
instance are refused rather than served; an operator who needs zero refused connections
during a rollout needs `app` to wait a probe interval between the signal and `Shutdown`
(not in M0, app.md decision 17; designed with Phase 1 packaging if needed). `httpx` holds no state.

### Imports

`api`: standard library (`bytes`, `context`, `crypto/sha256`, `encoding/hex`,
`encoding/json`, `errors`, `fmt`, `io`, `io/fs`, `log/slog`, `net`, `net/http`,
`net/netip`, `path`, `runtime/debug`, `sort`, `strings`, `sync`, `sync/atomic`, `time`),
`internal/lib/errs`, `internal/lib/validate`, `internal/lib/clock`, `internal/lib/httpx`,
`internal/model`, `internal/core/module`, `internal/core/db` (`ErrReadOnly`, `Status`;
no driver type), `internal/core/authz` (`Permission`), `internal/core/telemetry`,
`internal/core/telemetry/intake`. Never a module, `app` or `config` (depguard rule `api`,
with `config` already denied), never `go.opentelemetry.io`, never pgx. `httpx`: standard
library (`bytes`, `context`, `encoding/json`, `errors`, `fmt`, `io`, `mime`, `net`,
`net/http`, `net/netip`, `os`, `reflect`, `strings`), `internal/lib/errs`,
`internal/lib/validate` (depguard rule `foundation-lib` already allows exactly that;
`reflect` walks `dst`'s type to cut a decode-error path at a map key, F2).

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `httpx.ReadJSON` | `Content-Type` not `application/json` | Invalid wrapping `ErrUnsupportedMediaType` | `Fail` → 415 | `TestReadJSON_NotJSONContentType_ReturnsUnsupportedMediaType` |
| F2 | `httpx.ReadJSON` | malformed JSON, wrong type, trailing data | Invalid, `FieldError{Field: "body" or the JSON path cut at the first non-struct segment}`, reason never echoes the body | `Fail` → 400 with fields | `TestReadJSON_Malformed_ReturnsInvalidWithoutEcho`, `TestReadJSON_MapKeyNotInFieldPath` (a `map[string]int` dst with key `ZQ9MARKER7` of the wrong type: the field is `body.prefs`, not `body.prefs.ZQ9MARKER7`) |
| F3 | `httpx.ReadJSON` | empty body | Invalid, field `body`, reason `required` | 400 | `TestReadJSON_EmptyBody_ReturnsInvalid` |
| F4 | `httpx.ReadJSON` | the capped body overflows | `*http.MaxBytesError` returned unchanged (kind Internal by `KindOf`; the mapper checks the type first) | `Fail` → 413 | `TestReadJSON_OverCap_ReturnsMaxBytesError` |
| F5 | `httpx.ReadJSON` | the read fails: the connection's read deadline passed (decision 18); the ctx is done; the client went away mid-body | Timeout; the ctx's kind; Canceled | `Fail` → 504 / per kind / nothing | `TestReadJSON_ReadDeadline_ReturnsTimeout` (a body whose `Read` returns a `net.Error` with `Timeout()`), `TestReadJSON_ContextCanceled_ReturnsCanceled`, `TestReadJSON_UnexpectedEOF_ReturnsCanceled` |
| F6 | `httpx.ReadJSON` | `dst` nil or not a pointer | Internal | fix the handler | `TestReadJSON_BadDst_ReturnsInternal` |
| F7 | `httpx.WriteJSON` | `v` cannot be encoded | Internal; nothing written (encoded to a buffer first) | `Fail` → 500 | `TestWriteJSON_Unencodable_ReturnsInternalWritesNothing` |
| F8 | `httpx.WriteJSON` | `Write` fails for any reason (client gone, write deadline passed) | Canceled wrapping the write error; the shell's wrapper records it, the span gets `http.response.aborted=true` and `RecordError` | return; log nothing | `TestWriteJSON_WriteFails_ReturnsCanceled`, `TestPipeline_WriteError_MarksSpanAborted` |
| F9 | `Options.Validate`, `New` | a field out of range or nil; a trusted prefix invalid or of zero bits; several at once | Invalid, one `FieldError` each (`http.trusted_proxies` for the prefix) | fix app or config | `TestOptionsValidate_ReportsEach`, `TestOptionsValidate_ZeroBitPrefix_ReturnsInvalid` (`0.0.0.0/0`, `::/0`) |
| F10 | `New` | a set name invalid, repeated, or one of `core`, `modules`, `admin` | Invalid naming the set | fix app | `TestNew_BadDuplicateOrShellSetName_ReturnsInvalid` |
| F11 | `New` | a core set's route fails `module.Route.Validate` | Invalid naming set and route | fix the core package | `TestNew_InvalidCoreRoute_ReturnsInvalid` |
| F12 | `New` | two patterns conflict in `ServeMux` (within or across sets) | Conflict naming both sets and patterns, no panic | fix the module | `TestNew_ConflictingPatterns_ReturnsConflict` (`GET /{kind}/x` with `GET /things/{id}` in one set), `TestNew_MethodSiblings_NoConflict` (`GET /shifts/{id}` with `POST /shifts/bulk` mounts: the pair that a per-path method-less fallback would have broken) |
| F13 | `New` | a set in `Options.Routes` would mount at `/api/` itself or outside `/api/<name>/` | Invalid | fix the module | `TestNew_RouteEscapesPrefix_ReturnsInvalid` (the shell's own absolute set is exempt and is tested to mount, decision 20) |
| F14 | `New` | a set has routes and `OpenAPI == nil` | Invalid naming the set | add the fragment | `TestNew_RoutesWithoutFragment_ReturnsInvalid` |
| F15 | `New` | a route with no operation at its method and translated path (`/x/{$}` ↔ `/x/`, `{p...}` ↔ `{p}`), or an operation with no route, in a set; `head` is never required | Invalid naming set, method and path | fix the fragment | `TestNew_RouteFragmentMismatch_ReturnsInvalid`, `TestNew_RouteFragmentTranslation_Table` (root slash, wildcard, `HEAD` absent, `head` present and matched to `GET`) |
| F16 | `New` | a fragment is not a JSON object, a path key does not start with `/`, a `$ref` is not local, does not resolve against the merged document, or names another set's schema | Invalid naming the set and the key | fix the fragment | `TestNew_MalformedFragment_ReturnsInvalid`, `TestNew_RefToOtherSetSchema_ReturnsInvalid`, `TestNew_RefToBaseSchema_Resolves` |
| F17 | `New` | two sets define one `components.schemas` name | Conflict naming both | prefix the schema name | `TestNew_DuplicateSchemaName_ReturnsConflict` |
| F18 | `New` | the histogram cannot be created (telemetry F20–F21) | the meter's kind | fix app (one `New` per meter scope) | `TestNew_HistogramCreationFails_ReturnsKind` |
| F19 | `Listen` | the address is in use or cannot be bound | Unavailable | fix the port, stop the other process | `TestListen_AddrInUse_ReturnsUnavailable` |
| F20 | `Listen` | the address is malformed | Invalid | fix config | `TestListen_BadAddr_ReturnsInvalid` |
| F21 | `Listen` | already listening; or `Shutdown` already called | Conflict | fix app | `TestListen_Twice_ReturnsConflict`, `TestListen_AfterShutdown_ReturnsConflict` |
| F22 | `Serve` | `Listen` never called (and `Shutdown` not called either) | Internal | fix app | `TestServe_WithoutListen_ReturnsInternal`; `TestServe_AfterShutdown_ReturnsNil` pins the quiet path (decision 23) |
| F23 | `Serve` | the accept loop fails other than by `Shutdown` | Unavailable wrapping the error | app exits | `TestServe_ListenerClosedUnderneath_ReturnsUnavailable` |
| F24 | `Shutdown` | in-flight requests do not finish within ctx | Timeout / Canceled; the base ctx canceled; stuck handlers see `Canceled` and may still be running when `Shutdown` returns; second `Shutdown` nil | log, stop the rest with the remaining budget, exit | `TestShutdown_InFlightOverDeadline_CancelsAndReturnsTimeout` (the handler is still blocked when `Shutdown` returns; releasing it afterwards must not panic), `TestShutdown_Twice_IsNil`, `TestShutdown_BeforeListen_IsNil` |
| F25 | pipeline | a handler or middleware panics | none to the caller beyond `500 internal` with `ref` (if no header yet); span failed; error log with `stack`; the server keeps serving. `http.ErrAbortHandler` is re-panicked and logged by nobody | fix the bug | `TestPipeline_HandlerPanics_500WithRefAndStackLogged`, `TestPipeline_ErrAbortHandler_Repanics` |
| F26 | pipeline | an untrusted, non-loopback peer with `TrustedProxies` set and `AllowPlaintext` false, path not a health route | none: `403 misdirected`; warn once per prefix per hour with `peer_prefix` | fix `trusted_proxies` or the route to bms | `TestPipeline_UntrustedPeer_403AndLogsPrefixOnce`, `TestPipeline_UntrustedPeer_HealthRoutesServed` |
| F27 | pipeline | a body over `MaxBodyBytes` | `413 payload_too_large` | shorten the request | `TestPipeline_BodyOverCap_413` |
| F28 | pipeline | the handler outlives `RequestTimeout`; a body read stalls past it | its I/O fails with Timeout → `504 timeout`; the connection read deadline fails the stalled read → `504` through F5; `WriteTimeout` fails a late write (F8) | shorten the work; send the body | `TestMapper_Timeout_504` (a handler returning `errs.Timeout`; no clock), `TestPipeline_Order` (the ctx deadline is `now + RequestTimeout` and `SetReadDeadline` was called with the same instant, through a recording writer), `TestPipeline_SlowBody_ReadDeadlineEnds504` (the unexported `testRequestTimeout` hook set to 50 ms, a real `httptest.NewServer`, a client that writes half the body and blocks on a channel until the response arrives; bounded by the deadline, no `time.Sleep`) |
| F29 | cookie guard | a response sets a cookie without `Secure`, `HttpOnly`, `SameSite=Lax` or `Path=/`, with a `Domain`, or one `http.ParseSetCookie` rejects; at `WriteHeader`, at an implicit header write, or after a handler that wrote nothing. The one allowed omission: no `Secure` on a cookie named `bms_session_dev` when `AllowPlaintext` is true and the scheme in ctx is `http` (decision 24) | `500 internal`; error log naming the cookie, never its value | fix the handler | `TestCookieGuard_Table` (each missing attribute, `Domain`, unparseable; each of the three moments; a `__Host-` cookie with every attribute passes), `TestCookieGuard_DevCookie_Table` (`bms_session_dev` without `Secure`: passes with `AllowPlaintext` and scheme `http`; `500` with `AllowPlaintext` and scheme `https`, with scheme `http` and no `AllowPlaintext`, and when it also lacks `HttpOnly`; a `__Host-` cookie without `Secure` is `500` under every condition) |
| F30 | cross-origin protection | an unsafe-method request with `Sec-Fetch-Site` other than `same-origin`/`none`, or no such header and `Origin: null` or an `Origin` whose host differs from `Host` | `403 cross_origin`; log without the origin value | fix the caller (the shell is same-origin) | `TestCrossOrigin_Table` (`cross-site` and `same-site` rejected; `Origin: null` rejected; `same-origin`, `none`, no headers, `GET` accepted; no `Access-Control-*` header on any response) |
| F31 | authentication | signed-in route, no session at all | `401 unauthenticated`; debug log | sign in | `TestAuth_SignedInRouteNoSession_401` |
| F32 | authentication | signed-in route, `Authenticator` fails `Unauthenticated` (invalid, expired) | `401`; debug log (no actor, decision 4) | sign in again | `TestAuth_SignedInRouteBadSession_401` |
| F33 | authentication | `Authenticator` fails with another kind (Unavailable, Timeout) | signed-in route: that kind's status; public route: served anonymously, warn log | retry / fix the dependency | `TestAuth_AuthenticatorUnavailable_503OnSignedInAnonymousOnPublic` |
| F34 | mapper | `Fail` after the header was written | none: error log "error after response started"; nothing more written | fix the handler (fail before writing) | `TestFail_AfterHeader_LogsOnly` |
| F35 | `New` | `AllowPlaintext` true | none: one warn log at start | development only | `TestNew_AllowPlaintext_LogsWarningOnce` |
| F36 | mapper | `db.ErrReadOnly` anywhere in the chain | `503 read_only` | "this copy is read-only" | `TestMapper_ReadOnly_503ReadOnlyCode` |
| F37 | mapper | `intake.ErrRateLimited` | `429 rate_limited`, `Retry-After: 60` | back off | `TestMapper_RateLimited_429RetryAfter` |
| F38 | mapper | kind `Canceled`, client gone | nothing written; debug log; span not failed | — | `TestMapper_CanceledClientGone_WritesNothing` |
| F39 | mapper | kind `Canceled` while draining | `503 shutting_down`, `Connection: close` | retry against the new instance | `TestMapper_CanceledWhileDraining_503ShuttingDown` |
| F40 | mapper | every other kind, including an unkinded error | `StatusOf(kind)`, code `Kind.String()`, `ref`, `fields` for `invalid`; one log record at the level of decision 4, with `error.message` (scrubbed, capped) for a 5xx only; `err.Error()` absent from the body | per the code | `TestMapper_KindTable` (ten kinds), `TestMapper_BodyNeverCarriesErrorText`, `TestMapper_LogLevel_Table` (5xx error; signed-in 4xx warn; anonymous 4xx debug), `TestMapper_ErrorMessage_5xxOnlyAndScrubbed` (a marker e-mail in the error text is absent from the capture) |
| F41 | router | an unmatched `/api/` path; an unclean `/api/` path or one the mux would redirect | `404 not_found` as JSON, never `301`; debug log; route token `/{unmatched}` | fix the URL | `TestRouter_UnknownPath_404JSON`, `TestRouter_UncleanOrRedirectPath_404NotRedirect` (`/api/alpha//x`, `/api/alpha/./x`, `/api/alpha` when `/api/alpha/{$}` exists) |
| F42 | router | a known path with another method | `405 method_not_allowed` with `Allow` from the probe, in the fixed order; debug log | fix the method | `TestRouter_WrongMethod_405WithAllow` over exactly `GET /shifts/{id}` and `POST /shifts/bulk` in set `alpha`: `PUT /api/alpha/shifts/7` → `Allow: GET, HEAD`; `PUT /api/alpha/shifts/bulk` → `Allow: GET, HEAD, POST`; `DELETE /api/alpha/nothing` → `404` |
| F43 | `/readyz` | `Database.Ping` fails or exceeds 2 s | `503 {"database":"unavailable"}`; warn with the kind; the failure is cached for the same second | the orchestrator waits | `TestReadyz_PingFails_503` |
| F44 | `/readyz`, `/healthz` | after `Shutdown` began | `503 {"status":"draining"}` | stop routing to this instance | `TestHealth_Draining_503` |
| F45 | `GET /api/admin/modules` | no session; `Permissions` nil; `Allow` fails `Permission`; `Schema` fails | 401; 403; 403; 503 (its kind) | sign in / grant / fix the database | `TestAdminModules_Table` |
| F46 | `POST /api/client-errors` | non-JSON content type; body over 64 KiB; invalid batch; rate limited; ok | 415; 413; 400 with fields; 429 + `Retry-After: 60`; 202 no body | per telemetry Proposed change 3 | `TestClientErrors_Table` |
| F47 | UI | `UI` nil and a non-API path; a path with an extension that has no file; a non-`GET` method | `404 not_found`; `404`; `405` | — | `TestUI_Table` |
| F48 | `New` | a mounted route's token (`{$}` rendered as `/`) or its span name `<METHOD> <token>` fails telemetry's token grammar or exceeds 128 bytes | Invalid naming set and route | fix the pattern | `TestNew_RouteTokenFailsGrammar_ReturnsInvalid` (`/caf%C3%A9`, `/a+b`, a 130-byte pattern), `TestSpanName_EveryMountedRoute_NoViolation` (every route that `New` accepts, including `/` and `{path...}`, starts a span with no `Violation`) |
| F49 | pipeline | the request method is not one of `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS` | none: normalised to `_OTHER` for the span attribute and the histogram, span named `HTTP <token>`; routing answers `405` or `404` as usual; the original method reaches no span, log or metric | fix the client | `TestPipeline_UnknownMethod_NormalisedToOther` (method `ZQ9MARKER7`; `!c.Contains`) |
| F50 | pipeline | `r.RemoteAddr` is not `host:port` | `500 internal` (Internal); cannot happen on a TCP listener | fix the harness that built the request | `TestPipeline_BadRemoteAddr_500` |
| F51 | authentication | `Authenticator` returns a nil ctx with a nil error | `500 internal`; error log | fix identity | `TestAuth_AuthenticatorNilCtx_500` |
| F52 | `New` | `adminGraph` cannot be marshalled | Internal (unreachable with the api-owned type: strings and bools only; the error is checked anyway) | fix the type | `TestNew_AdminGraphMarshalledOnce` (the bytes are built in `New` and served unchanged; lower-case keys; no `Validate` func, no default value) |
| F53 | deadline layer | `SetReadDeadline` fails with anything but `http.ErrNotSupported` | `500 internal` (Internal); error log | fix the chain (a wrapper without `Unwrap`) | `TestPipeline_SetReadDeadlineFails_500`, `TestPipeline_ReadDeadlineNotSupported_DebugOnce` (a recorder) |
| F54 | `/readyz` | many probes at once or in one second | none: one `Ping` shared and cached for 1 s | — | `TestReadyz_SingleFlightAndCache` (ten concurrent calls → one `Ping`; advance the fake clock 1 s → a second `Ping`) |
| F55 | `New` | `Addr` is loopback and `TrustedProxies` is non-empty but contains no loopback address | none: one warn log at start | list `127.0.0.1/32` or bind the interface the front end reaches | `TestNew_LoopbackBindWithoutLoopbackProxy_WarnsOnce` |
| F56 | pipeline | a loopback peer that is not trusted sends `X-Forwarded-For` | none: the headers are ignored (client = `127.0.0.1`, scheme `http`); one warn per process, no address in it | set `trusted_proxies=127.0.0.1/32` | `TestPipeline_UntrustedLoopbackForwardedHeader_WarnsOnce` |
| F57 | pipeline | more than 1,000 distinct untrusted peer prefixes within an hour | none: the first 1,000 get their own line; the rest one aggregate warn line per minute with counts | fix the network path; look for a scan | `TestPipeline_UntrustedPeer_PrefixCapAggregates` (fake clock: 1,001 prefixes → 1,000 lines + 1 aggregate; more within the minute → nothing; advance a minute → one line with the count) |

Members that cannot fail, and why: `httpx.StatusOf` (a table with a default); `Fail`
(every input becomes a response; a nil error becomes a 500 and a test pins it); the ctx
accessors (lookups); `Handler`, `OpenAPI` (values built by `New`); `PermissionModulesRead`
(a constant); the client-address derivation (every input has a defined outcome, including
a malformed entry; `TestPipeline_ClientAddr_Table`). Each has a success-path test.

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
  reaches the handler with `r.PathValue("id")`, which proves dispatch goes through
  `mux.ServeHTTP`, not the probe); `TestNew_TrailingSlashIsExact` (`/` becomes
  `/api/alpha/{$}`; `/api/alpha/x` is 404; `/api/alpha` is 404, not 301);
  `TestNew_WildcardAllowed` (`{path...}`); `TestNew_CoreSetMountsLikeAModule`;
  `TestNew_ShellSetAbsolute` (the four `/api/` routes answer at their absolute paths and
  nothing answers under `/api/core/`); `TestNew_EmptyRoutes_ServesShellOnly`;
  `TestRouter_HeadServedThroughGet`; F10–F13, F41, F42, F48.
- Pipeline: `TestPipeline_Order` (a recording handler sees the address, scheme, the ctx
  deadline at `now + RequestTimeout`, failer, span and actor in ctx; a recording writer
  saw `SetReadDeadline` with the same instant); `TestPipeline_SecurityHeaders_Table`
  (every response: `nosniff`, `Referrer-Policy`, `X-Frame-Options`,
  `Permissions-Policy`, COOP, CORP; HSTS only with `https` and exactly
  `max-age=31536000`, decision 26; `no-store` on `/api/`; CSP on
  every `text/html` response and on no JSON response; never an `Access-Control-*`
  header); `TestPipeline_XTraceIDMatchesSpan`; `TestPipeline_XReadOnlyHeader`;
  `TestPipeline_ClientAddr_Table`, one row per case of "Client address and scheme, step
  by step": no proxies → peer; untrusted peer with forged headers → peer and `http`;
  trusted peer, no header → peer; trusted peer, one untrusted entry → that entry;
  trusted peer, chain `client, proxyA, proxyB` with both proxies trusted → `client`;
  all entries trusted → peer; two header lines joined in order; spaces around entries
  trimmed; IPv4 entry with a port accepted; malformed entry rightmost → peer; malformed
  entry in the middle → the trusted hop to its right; empty entry (`a,,b`) counts as
  malformed; IPv4-mapped peer `::ffff:10.0.0.1` trusted by `10.0.0.0/8`; IPv4-mapped
  entry unmapped before the prefix test; `X-Forwarded-Proto: https`, `HTTPS`, `https,
  http` (last wins → `http`), `wss`, absent, from an untrusted peer (always `http`);
  `TestPipeline_SpanNamedByRoute_WithParentFromTraceparent` (child of the header's
  trace; `http.request.method`, `http.route`, `http.response.status_code`, `person_id`;
  the root route's name is `GET /api/alpha/`, no `{$}`);
  `TestPipeline_HistogramSample_TokensOnly` (no ID, no text on the measurement);
  `TestPipeline_HealthRoutes_NoSpanNoSample`; `TestPipeline_SuccessWritesNoLogLine`;
  `TestPipeline_NeverLogsQueryBodyCookieOrAgent` (markers everywhere including the
  method and `X-Forwarded-For`, an error response forced, `!c.Contains`); F25–F33, F35,
  F49, F50, F51, F53, F55–F57.
- Mapper: `TestMapper_ErrorLogRecordFields` (the field set per status);
  `TestMapper_FieldsFromValidateErrors` (three problems → three fields in order, each
  with `field`, `code`, `params` and `reason`; `params` absent when the check set none);
  `TestMapper_UnavailableRetryAfter`; F34, F36–F40.
- Shell routes: `TestHealthz_OK` (body is exactly `{"status":"ok"}`, no version);
  `TestReadyz_ReadWriteAndReadOnly`; `TestModules_PublicListsNamesAndEnabled` (disabled
  module listed with `false`; no permissions or signals in the body);
  `TestAdminModules_WithPermission_GraphAndSchema` (lower-case keys, `version`, the
  api-owned shape, no Go field names); `TestOpenAPI_Served_WithETagAndNotModified`;
  F43–F46, F52, F54.
- OpenAPI assembly: `TestOpenAPI_MergesFragmentsUnderPrefixes` (paths prefixed; schemas
  merged; tags merged; `info.version`; base schemas present including
  `AdminModulesResponse`; `default` response added);
  `TestOpenAPI_EveryShellRouteDocumented` (the shell's own set passes F15);
  `TestOpenAPI_IDSensitivityTimeRangeShapes` (model.md Proposed change 6); F14–F17.
- Cookie guard: `TestCookieGuard_Table`, `TestCookieGuard_DevCookie_Table` (F29);
  `TestCookieGuard_HostPrefixCookiePasses`.
- UI: `TestUI_HashedAssetImmutable`; `TestUI_IndexForAppPathWithCSPAndNoCache`;
  `TestUI_IndexByNameCarriesCSP` (`/` and `/index.html`); `TestUI_PathTraversalIs404`;
  `TestUI_CustomCSP`; `TestUI_DefaultCSPHasObjectSrcNone`; F47.
- Lifecycle: `TestListenServeShutdown_RoundTrip` (`:0`; a request; `Shutdown` returns
  nil); `TestShutdown_DrainsInFlightRequest` (a handler blocked on a channel finishes
  and its response has `Connection: close`); `TestServe_AfterShutdown_ReturnsNil`;
  F19–F24.
- `TestSpanName_EveryMountedRoute_NoViolation` across every mounted route (F48).
- The only wall-clock-bounded test is `TestPipeline_SlowBody_ReadDeadlineEnds504`
  (F28), through the unexported `testRequestTimeout` hook in an internal test file; every
  other timing uses `clock.NewFake`.
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

None. The four questions in the Draft were answered by the owner on 2026-10-17 and are
recorded as decisions:

1. A cookie without `Secure`: only when `AllowPlaintext` is on and the request is plain
   `http`, under the separate development name `bms_session_dev`; the session cookie is
   `__Host-`-prefixed. → decision 24, F29, Proposed changes 4 and 9.
2. Field-error translation: `validate.FieldError` gains `Code` and `Params`, the wire form
   gains `fields[].code` and `fields[].params`, the web shell translates by code and falls
   back to `reason`. → decision 25, `httpx.Field`, Proposed changes 8 and 12.
3. HSTS: `max-age=31536000` without `includeSubDomains`. → decision 26, "Settings",
   Proposed changes 4 and 14.
4. `GET /api/modules` public with enabled names only; the graph and the schema versions
   behind `core.modules.read` on `GET /api/admin/modules`. → decision 27 (and 15),
   Proposed change 3.

## Proposed changes elsewhere

Status as of 2026-10-17: items 1, 2, 3, 4, 5, 6, 7, 8, 13 and the `.env.example` half of
14 are applied. Items 9, 10, 11, 12, 15, 16 and the `deploy/` front-end examples and
`docs/operating.md` half of 14 wait for their target to be written and are carried by
this list until then. Item 17 needs nothing.

1. **`plan/07-abstractions.md`, `api shell` entry** (applied 2026-10-17): replace with: "**Purpose:** expose
   the core's and the modules' routes over HTTP with one request pipeline. **Owns:** the
   router (`net/http.ServeMux`, routes mounted under `/api/<name>/`, one `/api/` fallback
   for JSON 404 and 405, no redirects under `/api/`); the middleware chain (recovery, the
   peer check for a misrouted plaintext path, the client address and scheme from trusted
   proxies, security headers, body cap, request deadline on the ctx and the connection,
   draining; per route: the request span and duration histogram with the method
   normalised, the error mapper, the cookie guard, cross-origin protection,
   authentication through `identity`'s `Authenticator`); the problem body and the error
   mapper (`errs` kind and the `db`/`intake` sentinels → status and code, `ref`, one log
   line per error response); `/healthz`, `/readyz`, `GET /api/modules`,
   `GET /api/admin/modules`, `POST /api/client-errors`, `GET /api/openapi.json` at
   absolute paths; OpenAPI assembly from the sets' fragments with a start-up check that
   routes and fragments agree; serving the built web shell. **Doesn't do:** business
   logic, permission checks (the service's), sessions (`identity`), permissions
   (`authz`), TLS (the front end), configuration loading (`config`), the shutdown
   deadline and order (`app`)." Add to the `lib` entry: `httpx` (the problem body,
   `StatusOf`, `Fail`, `ReadJSON`, `WriteJSON`, the client-address and scheme ctx
   accessors; owned by `api.md`). Layer rule 4 may add: "Handlers in any layer reach the
   shell's error mapper through `lib/httpx.Fail`; `api` is never imported."
2. **`.claude/skills/go-conventions/SKILL.md`, "Errors" table and "Layers"** (applied
   2026-10-17, all three skills): the HTTP
   column's note "done only in `api`'s ErrorMapper" becomes "the table is
   `lib/httpx.StatusOf`; the api shell's mapper is the only writer on a real request;
   handlers call `httpx.Fail(w, r, err)` and never write an error body themselves". Add
   `httpx` to the `lib` line. **`.claude/skills/add-module/SKILL.md` §2, `http.go`:**
   "handlers: `httpx.ReadJSON`, call the service, `httpx.WriteJSON`; on error
   `httpx.Fail` and return. Path IDs with `model.ParseID`. `GET` and `HEAD` routes never
   change state: the cross-origin check does not cover them and browsers prefetch them.
   Handlers start no goroutines (panic recovery covers the request goroutine only). The
   module ships an OpenAPI fragment (`openapi.json`, embedded) whose paths are relative
   to `/api/<name>` (`/x/{$}` is documented as `/x/`, `{p...}` as `{p}`), whose schema
   names start with the module's name and whose `$ref`s point at its own schemas or the
   base ones; `api` refuses to start if a route and the fragment disagree."
   **`.claude/skills/privacy-data/SKILL.md`, "Rules":** add beside "a list must not
   reveal that a hidden record exists": "a single record the caller may not see is
   answered `errs.NotFound`, never `errs.Permission`, so a `403` never confirms that an
   ID exists; `Permission` is for an action the caller may not take on a record they may
   see."
3. **`plan/design/module-contract.md` (row 4)** (applied 2026-10-17; (e) is carried by item
   16, applied with `module-hello.md`): its Proposed change 6 is answered here
   (mounting, fragment form, `GET /api/modules` split into a public list and a
   permissioned admin route, the read-only mapping). Proposed amendments: (a) a
   `RouteProvider` must also implement `APIDescriber` (F34 becomes "a `RouteProvider`
   without a non-empty `OpenAPI()`"), since `api` requires the fragment (decision 11);
   (b) note beside `Route.Pattern` that a pattern ending in `/` is mounted exactly
   (`{$}` appended), that `{path...}` is the way to take a subtree, that the mounted
   pattern must fit telemetry's token grammar (`[A-Za-z0-9/_][A-Za-z0-9._:/{}-]*`, the
   whole `<METHOD> /api/<name><pattern>` at most 128 bytes; `api` F48 enforces it, and
   `Route.Validate` enforces the character set itself so a module learns in its own
   tests), and that a `GET` or `HEAD` route never changes state; (c) note that `ModuleRoutes` is
   also the shape `app` uses for the core's route sets, with `Module` set to the core
   area name and never `core`, `modules` or `admin`; (d) `core.modules.read` is declared
   by `api` (`api.PermissionModulesRead`) and registered by `app`; (e) the `hello`
   module's fragment documents `GET /greeting` and `GET /public-greeting` and its schema
   names start with `Hello`.
4. **`plan/design/config.md` (row 3)** (applied 2026-10-17): its Proposed change 5 is answered: (b) and (d)
   enforced as decisions 7 and 8; the plaintext warning F35; the direct-peer decision is
   `403` with code `misdirected` (not `421`: decision 9 gives the reason) with the health
   routes exempt. Refinements to record: "session and CSRF cookies" becomes "the session
   cookie" (there is no CSRF cookie, decision 6); HSTS is `max-age=31536000` alone
   (decision 26); the one `Secure`-less cookie is the development cookie of decision 24.
   **New validation:**
   `http.trusted_proxies` refuses a prefix of zero bits (`0.0.0.0/0`, `::/0`) under
   `BMS_HTTP_TRUSTED_PROXIES` with the reason "trusts every peer: list the front end's
   addresses or networks" (a new F row and `TestValidate_ZeroBitTrustedProxy_ReturnsInvalid`);
   `api.Options.Validate` repeats the check (F9) for options built by hand. The
   catalogue row for `http.trusted_proxies` adds: "a front end on the same host is listed
   as `127.0.0.1/32`; otherwise all its clients share the loopback address."
5. **`plan/design/db.md` (row 5)** (applied 2026-10-17): its Proposed change 4 is answered: `read_only`
   mapping (decision 16, F36), `/readyz` shape as proposed, `GET /api/admin/modules`
   carries `Built`, `Applied` and `current` per set through `api.SchemaStatus` (which
   `app` builds over `Migrator.Status`), and stores turn `*db.Error.Constraint` into a
   `validate.FieldError` themselves (the mapper reads `validate.Fields` only). **New:**
   `Pool.Close` must tolerate a handler that is still holding a connection when it is
   called (a request that ignored its ctx past `api.Shutdown`'s deadline, "Concurrency
   and lifecycle"): give `Close` a ctx (or an internal bound) so it cannot block shutdown
   forever, and state what a late caller sees (`Unavailable`).
6. **`plan/design/telemetry.md` (row 15b)** (applied 2026-10-17; the grammar amendment (a)
   is its decision 20, owner 2026-10-17): its Proposed change 3 is answered with these
   refinements: the span is opened per route after routing (its `Span` has no rename, so
   the name is known at `Start`; decision 10); the route token renders `{$}` as the
   trailing `/` (`GET /api/alpha/`), so no `$` reaches a span name; the request method
   is normalised to the seven known methods or `_OTHER` before it becomes a name, an
   attribute or a label, and the span is named `HTTP <route token>` for `_OTHER` (both
   per OpenTelemetry's HTTP semantic conventions); unmatched requests use the route
   token `/{unmatched}`; the health routes get no span and no sample; every response
   carries `X-Trace-Id` (its decision 2 allowed it). **Two grammar amendments:** (a)
   `Token`'s first byte admits `_` (`[A-Za-z0-9/_]`) so `_OTHER` is a valid token
   value; (b) the span-name form's method part
   `[A-Z]{3,10}` already admits `HTTP`, so nothing else changes. The intake route is
   mounted as specified (F46). Its "UI" wording and Proposed change 4 stand.
7. **`plan/design/lib-errs.md`, Open question 1** (applied 2026-10-17): answered as proposed (decision 17):
   client gone → no response and a debug line; shutdown → `503 shutting_down` with
   `Connection: close`. Its Proposed change 1 is met by `httpx.Problem` and the mapper.
8. **`plan/design/lib-core.md`, `validate`** (applied 2026-10-17; lib-core is Implemented,
   so the spec records the change and an implementation follow-up is needed): add
   consumer rows for `httpx` (`FieldError`,
   `Fields` in `Fail`'s fallback and in `ReadJSON`); per decision 25, add `Code string`
   and `Params map[string]string` to `FieldError`, set by each
   check (`NonEmpty` → `required`, `Range` → `range` with `min`/`max`, `OneOf` →
   `one_of` with `options`, `URL` → `url`, `HostPort`/`ListenAddr` → `host_port`,
   `Positive` → `positive`), reserve `malformed` for decoders (`httpx.ReadJSON`), and
   let `Addf` take a code.
9. **`plan/design/identity.md` (row 6)** (pending: target not written): implement `api.Authenticator` (no database write;
   `Unauthenticated` for a bad or expired session; `(ctx, zero, nil)` for none); define
   the principal's ctx accessor that services and `authz` use; never return a nil ctx
   with a nil error (F51); write the session cookie with `Secure`, `HttpOnly`,
   `SameSite=Lax`, `Path=/`, no `Domain`, under a `__Host-` name (decision 7); for the
   plaintext case write `bms_session_dev` without `Secure` only when `httpx.SchemeFrom`
   is `http` and the shell was built with `AllowPlaintext`, never both cookies on one
   response (decision 24); when the
   trusted-header identity provider (D15) is designed, ask this spec for
   `httpx.TrustedPeerFrom(ctx) bool` (the pipeline already knows whether the peer is in
   `TrustedProxies`; the accessor is added then, with its consumer named); its login,
   logout and password routes are a core route set
   (`accounts`, mounted at `/api/accounts/`) with login and sign-up `AccessPublic`;
   rate-limit failed logins by `httpx.ClientAddrFrom`, answering `429 rate_limited` by
   wrapping its own sentinel that the mapper learns then (or `intake.ErrRateLimited`
   moved to a shared place); document that the session value is request-scoped
   (signals.md Proposed change 7).
10. **`plan/design/authz.md` (row 7)** (pending: target not written): satisfy `api.PermissionChecker` with its
    `Authorizer` (site-scoped `Allow`); receive `api.PermissionModulesRead` from `app`
    among the core's declarations (`PermissionDecl{Name: "core.modules.read", Scope:
    ScopeSite, Reaches: SensitivityInternal}`), granted to every site-admin starter role
    by default; refuse to treat a session value as the actor when
    `signals.InDelivery(ctx)` is true.
11. **`plan/design/app.md` (row 18)** (applied 2026-10-17, by app.md's Proposed change 7: the HTTP sub-deadline is `max(1 s, 2 x shutdown_timeout / 3)`; the order is HTTP, runnables, `signals.Close`, `Set.Stop`, `db.Close`, `telemetry.Close`; there is no drain delay in M0, app.md decision 17, and if one is added later `api` gains `Server.BeginDrain()` so the health routes answer `503` while the listener stays open; `core.modules.read` is registered through `authz`'s part with `corePerms`; `bms check` prints the operation count; the `deliver:hello.greeted` assertion runs in app's e2e test once `module-hello.md` defines the write route): build `api.Options` from `config.HTTP` (all but
    `ShutdownTimeout`), `tel.Logger().With("component","api")`, `tel.Tracer("api")`,
    `tel.Meter("api")`, `clock.Real()`, the build version, `Set.Routes()` plus one
    `module.ModuleRoutes` per core package with routes (`accounts`, `people`, ... as their
    specs define, `Module` = the area name), the `*db.Pool`, the `*intake.Intake`,
    `plan.Graph()`, a `SchemaStatus` closing over `Migrator.Status` and every set,
    `identity`'s `Authenticator` (M1) and `authz`'s checker (M2), the embedded web build as
    `UI` (`nil` for a headless build); `New` → `Listen` → `go Serve` → on signal
    `Shutdown(ctx)`, then `signals.Close`, `Set.Stop`, `db.Close`, `telemetry.Close`,
    all within `http.shutdown_timeout`; **give HTTP a sub-deadline** inside that budget
    (proposed: two thirds of `shutdown_timeout`, at least 1 s) so that when `Shutdown`
    returns `Timeout` the remaining closers still have time, and expect handlers that
    ignored their ctx to be alive during those closers ("Concurrency and lifecycle");
    log the build version at start (it is no longer on `/healthz`, decision 21);
    consider a drain delay between the signal and `Shutdown` for probe-based balancers
    (the listener closes at once; a balancer learns on its next probe); register
    `api.PermissionModulesRead` with `authz`; `bms check` prints the OpenAPI operation
    count; the `hello` integration test asserts a request's `deliver:hello.greeted` span
    is a descendant of its request span.
12. **`plan/design/web-shell.md` (row 17)** (applied 2026-10-17; decision 25's `reason` fallback stands, shown inside the shell's translated `field.unknown` wrapper with the untranslated code reported; the reference is additionally hidden for `unauthenticated`, `read_only` and the client's `network` code, web-shell.md decision 9; no CSP change requested; note that the shell calls `GET /api/modules` and `GET /api/accounts/session` in parallel at boot, so neither may depend on the other): mirror `httpx.Problem` and the codes in
    TypeScript; translate `code` through keys `error.<code>` and, for `invalid`, map
    `fields[].field` to inputs and translate `fields[].code` with `fields[].params`
    (keys `field.<code>`), showing `reason` for an unknown code (decision 25); show `ref` per
    telemetry.md "UI"; on `401` go to sign-in; show the read-only banner when a response
    carries `X-Read-Only: true` and clear it when one does not; read `GET /api/modules`
    for the enabled set and `GET /api/admin/modules` for the admin screen; generate the
    client from `/api/openapi.json` at build time and lint the document in the web
    tooling (`make check`); build with hashed assets under `assets/`, no inline scripts
    and no plugins (`DefaultCSP` includes `object-src 'none'`; COOP and CORP
    `same-origin` are on every response, so the shell may not embed cross-origin
    resources or open cross-origin popups that need a reference), proposing a CSP change
    here if the build needs one; send `traceparent` on every call (telemetry Proposed
    change 4); treat `403 misdirected` like `403 cross_origin` (a deployment problem, not
    a user error); read `version` from `GET /api/admin/modules` for the admin screen.
13. **`plan/09-core-features.md` §1 "Done when"** (applied 2026-10-17): add "`GET /api/openapi.json` describes
    every mounted route; the server refuses to start when a route and its module's
    fragment disagree" and "an error response is `application/problem+json` with `code`
    and `ref`".
14. **`deploy/compose/.env.example` (applied 2026-10-17: the same-host note and the
    zero-bit refusal), the `deploy/` front-end examples and `docs/operating.md` (pending:
    not written):** the front end
    must preserve the `Host` header (decision 6: Caddy does by default; nginx needs
    `proxy_set_header Host $host`), set `X-Forwarded-For` and `X-Forwarded-Proto` and
    strip incoming ones (config.md Proposed change 11), and health checks come from
    loopback or through the front end (direct probes from other addresses are served for
    `/healthz` and `/readyz` only); **a front end on the same host as bms is listed as
    `BMS_HTTP_TRUSTED_PROXIES=127.0.0.1/32`** (or `::1/128`), otherwise every client
    shares the loopback address and HSTS is never sent (decision 9); document the `403
    misdirected` log line as "a request reached bms without passing the front end" and
    the "untrusted loopback peer sent X-Forwarded-For" warning as "the front end on this
    host is not in trusted_proxies"; note that bms sends HSTS as `max-age=31536000` only
    and that a regional controlling every subdomain adds `includeSubDomains` (and
    `preload`) at the front end (decision 26).
15. **`plan/design/settings.md` (row 15)** (pending: target not written): when the public URL exists (D18), `app` passes
    its origin to `api` as a trusted origin for cross-origin protection, so a front end
    that rewrites `Host` still works; until then the `Host`-preserving rule in 14 holds.
    This adds `Options.TrustedOrigins []string` to this spec at that time, and with it a
    **`Host` allowlist** (`Options.AllowedHosts`, the public URL's host plus loopback and
    the bind address): a request whose `Host` is not listed is answered `404` with no
    body, which closes DNS rebinding against the API; until the setting exists the front
    end's virtual host is the allowlist and `AllowPlaintext` deployments have none.
16. **`plan/design/module-hello.md` (row 4b)** (applied 2026-10-17, by module-hello.md's Proposed change 2; carries
    item 3(e): the fragment documents `GET /greeting` and `GET /public-greeting` and its
    schema names start with `Hello`): ship `openapi.json` next to `module.go`
    (embedded), use `httpx.ReadJSON`/`WriteJSON`/`Fail`, read the actor through
    `identity`'s accessor (M1) and return `errs.Permission` from its service until then
    for `GET /greeting`; its tests assert `404 not_found` from `httpx.Fail`'s fallback and
    the full problem body through `app`'s integration test. As built: `hello`'s unit tests
    assert `httpx.Fail`'s fallback statuses for the kinds `hello` returns (401, 403, 503);
    `hello` has no `NotFound` path, so the `404 not_found` body is asserted by `app`'s e2e
    test on `GET /api/hello/nope`, and the 401 body on `GET /api/hello/greeting`.
17. **`.golangci.yml`** (nothing to apply): no new rule needed: `foundation-lib` already confines `httpx` to
    the standard library and `lib`, and the `api` rule already denies modules, `app` and
    `config`. If the owner wants a strict allowlist for `internal/api` (stdlib, `lib`,
    `model`, `core`), add it as `list-mode: strict` on the `api` rule.
