# telemetry: logs, traces, metrics and client error reports

- **Status:** Approved (2026-10-16)
- **Package:** `internal/core/telemetry` (Go module `github.com/zedgama3/bms`), plus the
  sub-package `internal/core/telemetry/intake` (client error reports) and the test helper
  package `internal/core/telemetry/telemetrytest`. **UI:** none of its own; the error
  screens that show a reference and the code that reports browser errors are
  `web-shell.md`'s, fed by what this spec defines.
- **Depends on specs:** `lib-errs.md` (Implemented): `errs.E`, `errs.Is`, `errs.KindOf`,
  `errs.Error` (`Op`, `Kind` read for span and log fields), kinds `Invalid`, `Conflict`,
  `Unavailable`, `Timeout`, `Canceled`, `Internal`. `lib-core.md` (Implemented):
  `clock.Clock` (the intake rate limiter and its sweep), `validate.Errors`,
  `validate.FieldError`, `validate.OneOf`, `validate.Range`, `validate.NonEmpty`.
  `lib-secret.md` (Implemented): `secret.Secret.LogValue` and `model.Email.LogValue` are
  the first line of defence this spec builds on; `secret.Redacted` is the marker it
  recognises. `model.md` (Approved): `model.PersonID` (the only identity a report or a log
  carries), `ID.String` (IDs are what logs hold instead of names). `config.md` (Approved):
  `Options` mirrors `config.Log` and `config.Instance.Name` (its decision 12 and Proposed
  change 7); `OTEL_*` is read here, not there. `module-contract.md` (Approved):
  `Deps.Logger` is built here (scoped by `app`); `Route.Access` (the intake route is
  public). `db.md` (Approved): Proposed change 12 (pool metrics, statement spans) is
  answered under "Proposed changes elsewhere".
  **Used by:** `app` (`New`, `Logger`, `Tracer`, `Meter`, `Close`; builds `Deps.Logger`
  and the proposed `Deps.Tracer`; hands a tracer and meter to `db`, `api`, `signals`,
  `jobs`), `api` (request spans, `WithParentFrom`, `RefFrom` for the error body, the
  request-duration histogram, the `/api/client-errors` route over `intake.Accept`), `db`
  (statement spans, pool gauges), `audit` (`TraceIDFrom`: 09 §8 records the trace ID on
  every entry), `signals` and `jobs` (a span and a counter per delivery and per run),
  every module (`Deps.Logger`; `Deps.Tracer` once module-contract adopts it),
  `moduletest` and every test that must prove no personal data leaks
  (`telemetrytest.Capture`), the `hello` reference module.

## Purpose

Let a maintainer find what happened in any one request from the reference the person saw.

## Responsibilities

- Build the process **logger** (`log/slog`): JSON on stdout by default, text for a
  developer's terminal (D20, config.md decision 15), every record carrying the trace and
  span IDs of the request it belongs to.
- Build the **tracer** and **meter** behind two small interfaces this spec owns, with the
  OpenTelemetry SDK beneath them, and export traces, metrics and logs over **OTLP** when
  the standard `OTEL_*` variables name a collector; export nothing, and still work, when
  they do not.
- Enforce the **privacy policy** on everything emitted, in one place: typed attributes with
  an allowlist of value kinds, a denylist of keys, scrubbing of e-mail shapes and query
  strings, caps on free text, and a counted violation for anything that breaks the rule so
  tests fail and maintainers see it.
- Define the **error reference**: the short code a person sees on an error screen or in an
  error body, how it is derived from the trace, and how the maintainer gets from it to
  the trace and its logs.
- Accept **client error reports** from the web shell (`intake`): validate, limit, scrub and
  log them with `source=client`, so a crash in the browser is as visible as one on the
  server.
- Keep going when the collector is down: no request, no start-up and no shutdown ever
  waits on, or fails because of, the observability backend.

## Not responsible for

| Concern | Owner |
|---|---|
| Reading `log.level`, `log.format` and `instance.name` from the environment | `config` (`config.md`); `app` copies them into `Options`. `OTEL_*` is this spec's to read, because the OpenTelemetry SDK defines those names and an operator expects the standard ones to work (config.md "Not responsible for"). |
| The HTTP middleware that opens a span per request, names it after the route, records the status and writes the error body with the reference; the `/api/client-errors` route itself (decoding under the body cap, status codes) | `api` (`api.md`, Proposed change 3). This spec gives it `Tracer.Start` with `WithParentFrom`, `RefFrom`, the histogram, and `intake.Accept`. |
| Catching errors in the browser, batching and deduplicating them, generating `traceparent` for API calls, showing the reference on error screens | `web-shell.md` (Proposed change 4). This spec fixes what the server accepts. |
| Statement spans and pool gauges for PostgreSQL (the pgx `QueryTracer` adapter and `Pool.Stats`) | `db` (`db.md`, Proposed change 2): only `db` may import pgx (depguard `storage-seam`). It calls this spec's `Tracer` and `Meter`. |
| Spans and counters per signal delivery and per job run | `signals.md`, `jobs.md` (Proposed change 6). |
| Which copy an audit entry came from, and the trace ID on it | `audit` (`audit.md`) calls `TraceIDFrom`. |
| Dashboards, alert rules, retention of logs, traces and metrics, who may open Grafana | the deployment: `deploy/compose/observability` for development, the example production setup and `docs/operating.md` (Proposed change 9). D20 proposes 30 days for logs. bms emits; it keeps nothing. |
| The audit log (who did what, who saw what) | `audit`. Telemetry is for maintainers and holds IDs, kinds and durations, never what a person saw. |
| Analytics about people, usage statistics, third-party analytics | nobody (privacy-data: "There's no third-party analytics"). Metrics here count requests, errors and durations by route and kind, never by person. |
| A separate request ID | nobody. The trace ID is the request ID (decision 2). |
| Log retention on the host (journald, Docker log rotation) | the operator (`docs/operating.md`). |
| Propagating trace context on outbound calls (OIDC, S3-compatible storage) | added as `Inject(ctx, http.Header)` when the first consumer appears (`identity` OIDC, `files` S3); no consumer in M0. |
| Runtime metrics (goroutines, heap), per-module business counters | later, through the same `Meter`, when a consumer names them. |
| Sending telemetry from the on-site copy back after an event (D20, Q-OFFLINE) | the later `switchover` spec. Each copy reports to its own collector and is identified by `service.instance.id`. |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `Options`, `New`, `Telemetry`, `Close` | Build the three providers once from validated options and `OTEL_*`, and shut them down within a deadline. | Never dials the collector at start; exporters are lazy. |
| Logger | Give every package a `*slog.Logger` whose records carry trace IDs and pass the policy before they reach stdout or OTLP. | One handler: policy, then fan-out to the stdout handler and the OTLP bridge. |
| `Tracer`, `Span` | Open and close spans with typed attributes, under this project's own interface. | `Start` never fails; a bad name is a counted violation. |
| `Meter`, `Counter`, `Histogram`, gauges | Record bounded-cardinality measurements under this project's own interface. | Only token-like attributes are allowed on metrics. |
| `Attr` and the policy | Make personal data hard to emit: typed constructors, key denylist, `Scrub`, caps, `Violation`. | Shared by logs, spans, metrics and `intake`. |
| `Ref`, `RefFrom`, `TraceIDFrom` | Turn the current trace into the code a person can quote, and give `audit` the trace ID. | `XXXX-XXXX`, the first four bytes of the trace ID. |
| `intake` | Accept, limit, validate, scrub and log error reports from the web shell. | A service; `api` owns the route. Client addresses are never logged. |
| `telemetrytest` | Capture what a package emitted and fail the test on any policy violation. | In-memory; no environment, no collector. |

## Interface

```go
// Package telemetry builds the process logger, tracer and meter (D20) behind interfaces
// this project owns, exports over OTLP when OTEL_* names a collector, and enforces the
// privacy policy on everything emitted. It sits at the bottom of the core graph: it
// imports lib and model and the OpenTelemetry SDK, and no other project package. It is the
// only package allowed to import go.opentelemetry.io (depguard rule, Proposed change 8).
package telemetry

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// ---- Options and lifecycle ----

// Format is how log lines are written to Stdout (config.md: log.format).
type Format string

const (
	FormatJSON Format = "json" // default; what a deployment emits (D20)
	FormatText Format = "text" // a developer's terminal
)

// Options is what New needs. app copies config.Log and config.Instance.Name into it
// (config.md decision 12), so telemetry is built in tests with no environment at all.
type Options struct {
	// Level is the minimum level written to Stdout and exported. One of slog.LevelDebug,
	// LevelInfo, LevelWarn, LevelError. At LevelDebug, records also carry source
	// file and line.
	Level slog.Level
	// Format is json or text.
	Format Format
	// Instance is this copy's name (config.Instance.Name): resource attribute
	// service.instance.id and the log attribute "instance". Grammar as config.md:
	// "[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?".
	Instance string
	// Version is the build's version (app: from -ldflags or debug.ReadBuildInfo; "dev"
	// when unknown): resource attribute service.version. A token (see Token), at most
	// 64 bytes.
	Version string
	// Stdout receives log lines. os.Stdout in app; a buffer in tests. Must not be nil.
	Stdout io.Writer
	// Clock drives the rate limits inside this package (violation warnings, export
	// failure warnings). Must not be nil.
	Clock clock.Clock
	// Environ looks up the OTEL_* variables this package reads (Behavior: "What is read
	// from the environment"). nil means os.LookupEnv. Tests pass a map lookup.
	Environ func(key string) (string, bool)
	// Observer receives a copy of everything emitted, after the policy was applied.
	// nil in production; telemetrytest sets it. See Observer.
	Observer Observer
}

// Validate fails with errs.Invalid joining one validate.FieldError per problem, fields
// "log.level", "log.format", "instance.name", "telemetry.version", "telemetry.stdout",
// "telemetry.clock" (F1–F5).
func (o Options) Validate() error

// New validates opts, reads the OTEL_* variables, builds the resource (service.name "bms"
// unless OTEL_SERVICE_NAME is set, service.version, service.instance.id, merged with
// OTEL_RESOURCE_ATTRIBUTES), the logger, the tracer provider and the meter provider, and
// attaches OTLP/HTTP exporters for each signal that has an endpoint. It never connects to
// the collector: exporters dial on their first export. It fails with errs.Invalid naming
// the variable for an unsupported protocol, a malformed endpoint, an unsupported sampler
// or a bad OTEL_SDK_DISABLED value (F6–F9, F11), and errs.Canceled or errs.Timeout if ctx
// ends first (F10). The caller owns the result and must Close it.
// Consumers: app (once per command, right after config.Load, before db.Open);
// telemetrytest.
func New(ctx context.Context, opts Options) (*Telemetry, error)

// Telemetry is the process's logger, tracer and meter. Safe for concurrent use from New
// until Close returns.
type Telemetry struct{ /* unexported: providers, base logger, policy counters, exporting flags */ }

// Logger returns the base logger: level and format from Options, "instance" attached,
// trace_id and span_id taken from the ctx of every *Context log call. app derives scoped
// loggers with With (module=<name>, component=<name>). Cannot fail.
// Consumers: app (Deps.Logger for every module and every core package's logger).
func (t *Telemetry) Logger() *slog.Logger

// Tracer returns a tracer whose spans carry the instrumentation scope name: "api", "db",
// "signals", "jobs", "module.<name>". Fails with errs.Invalid for a name that is not a
// token of at most 64 bytes (F12) and errs.Internal after Close (F13).
// Consumers: app (one per core package and module; Deps.Tracer, Proposed change 1).
func (t *Telemetry) Tracer(name string) (Tracer, error)

// Meter returns a meter under scope name; same rules as Tracer (F12, F13).
// Consumers: app (for api, db, intake, signals, jobs).
func (t *Telemetry) Meter(name string) (Meter, error)

// Exporting reports whether at least one signal has an OTLP endpoint. Cannot fail.
// Consumers: app's start-up log line and `bms check` ("OTLP export: on/off").
func (t *Telemetry) Exporting() bool

// Close flushes every provider and shuts them down, each within ctx, and returns their
// errors joined: errs.Timeout or errs.Canceled when ctx ends first (F26). Idempotent: a
// second Close returns nil. After Close, log records still reach Stdout (the stdout
// handler is never closed), and Tracer and Meter fail (F13).
// Consumers: app, last at shutdown, after db.Close, within http.shutdown_timeout.
func (t *Telemetry) Close(ctx context.Context) error

// ---- Tracer ----

// Tracer opens spans. Every implementation is safe for concurrent use.
type Tracer interface {
	// Start begins a span named name as a child of the span ctx carries, or a root span
	// when it carries none, and returns a ctx carrying the new span. It never fails and
	// never blocks. name is a token of at most 128 bytes ("db.SELECT",
	// "signal:hello.greeted"), or the HTTP form "<METHOD> <route pattern>": an upper-case
	// method ([A-Z]{3,10}), exactly one space, then a token beginning with "/", the whole
	// at most 128 bytes ("GET /api/people/{id}"; decision 19). The route is always the
	// pattern, never a concrete path. Anything else yields a span named
	// "invalid-span-name" and a counted Violation (F14). The caller must End the span
	// exactly once, usually with defer.
	Start(ctx context.Context, name string, opts ...StartOption) (context.Context, Span)
}

// Span is one unit of work in a trace.
type Span interface {
	// SetAttrs adds attributes, each already policy-checked by its constructor (F15–F18).
	// Cannot fail. No-op after End.
	SetAttrs(attrs ...Attr)
	// RecordError marks the span failed and records error.kind (errs.KindOf), error.op
	// (the outermost errs.Error's Op, when any) and exception.message (err.Error() through
	// Scrub, capped at MaxTextBytes). A nil err is ignored. errs.Canceled does not mark the
	// span failed (the caller left; nothing broke). Cannot fail. No-op after End.
	RecordError(err error)
	// End closes the span and hands it to the exporter. Idempotent. Cannot fail.
	End()
}

// SpanKind follows OpenTelemetry's kinds. The zero value is Internal.
type SpanKind uint8

const (
	SpanInternal SpanKind = iota // work inside the process (default)
	SpanServer                   // handling a request: api
	SpanClient                   // calling a dependency: db, later SMTP and S3
	SpanProducer                 // publishing a signal
	SpanConsumer                 // delivering a signal, running a job
)

// StartOption configures Start.
type StartOption func(*startConfig)

// WithKind sets the span kind. Consumers: api (Server), db (Client), signals, jobs.
func WithKind(k SpanKind) StartOption

// WithAttrs sets attributes at start, so a sampler could see them. Consumers: api
// (method and route), db (operation).
func WithAttrs(attrs ...Attr) StartOption

// WithParentFrom continues the trace named by a W3C traceparent header in h, as the api's
// request middleware does for the web shell's calls (D20: one trace covers the click and
// the server work). Only the trace ID and parent span ID are taken: the sampled flag and
// any tracestate are ignored, because the server decides sampling (decision 6). A missing
// or malformed header (wrong version, length or characters, all-zero IDs) is ignored and
// a root span starts (F33); nothing is counted, since any client may send anything.
// Consumer: api.
func WithParentFrom(h http.Header) StartOption

// ---- Attributes and the policy ----

// Attr is one key-value pair for a span, a metric or (through slog) a log record. Build
// it with the constructors below; each applies the policy, so a Span or Counter never
// sees a raw value. The zero Attr is ignored.
type Attr struct{ /* unexported: key string; value attrValue */ }

// Key returns the attribute's key after policy. Cannot fail. For tests.
func (a Attr) Key() string

// Keys are "[a-z][a-z0-9_.]{0,63}". A key on the denylist (Behavior: "The key denylist")
// keeps its key and gets the value "[denied]" plus a Violation (F15). A key that fails the
// grammar yields Attr{telemetry.invalid_key: <key, capped>} plus a Violation (F16).

// Int, Int64, Float and Bool record numbers and booleans. Cannot fail beyond the key rules.
func Int(key string, v int) Attr
func Int64(key string, v int64) Attr
func Float(key string, v float64) Attr
func Bool(key string, v bool) Attr

// Duration records d in milliseconds as an int64; by convention the key ends in "_ms".
func Duration(key string, d time.Duration) Attr

// Token records a short identifier-like string: a route pattern, an HTTP method, a module
// or signal name, an operation, an SQL keyword, a status word. Grammar:
// "[A-Za-z0-9/_][A-Za-z0-9._:/{}-]{0,127}" (no space, no "@", no "?"), so neither an
// e-mail address nor a URL with a query string fits; "_" is admitted as a first byte so
// that the api's normalised method "_OTHER" is a token (decision 20). A value that fails
// yields "[invalid-token]" and a Violation (F17). Tokens are the only strings allowed on
// metrics.
func Token(key, value string) Attr

// Text records free text: an error message, a client's crash message. The value goes
// through Scrub and is cut at MaxTextBytes with "…" appended (F18). Text is never allowed
// on a metric (F19). Use it last; prefer Token or an ID.
func Text(key, value string) Attr

// ID records a record identifier in canonical form (model.ID.String). A zero id yields the
// zero Attr, which is ignored. IDs are never allowed on a metric (F19).
func ID[T ~[16]byte](key string, id T) Attr

// Kind records errs.KindOf(err).String() under "error.kind". Cannot fail.
func Kind(k errs.Kind) Attr

// Module records a module name under "module". Same grammar as Token.
func Module(name string) Attr

// MaxTextBytes caps a Text attribute and exception.message. MaxLogValueBytes caps any
// string value in a log record (a stack trace keeps its top frames).
const (
	MaxTextBytes     = 512
	MaxLogValueBytes = 2048
)

// Scrub returns s with every e-mail-shaped token replaced by "[email]" and every "?query"
// part of a URL-shaped token replaced by "?[query]". It is applied to every string that
// reaches a log, a span or a client report. It does not cap length (the caller does) and
// does not detect names or phone numbers, which have no reliable shape: those are kept
// out by using IDs (go-conventions) and by the key denylist. Cannot fail; pure.
// Consumers: the log handler, Span.RecordError, Text, intake.
func Scrub(s string) string

// Violation is one breach of the policy that was corrected at the point of emission.
type Violation struct {
	Signal string // "log", "trace", "metric", "client"
	Rule   string // "denied_key", "invalid_key", "invalid_token", "invalid_span_name", "text_on_metric", "id_on_metric", "negative_count"
	Key    string // the attribute key or span name involved, capped at 64 bytes
}

// Observer receives a copy of what telemetry emits, after the policy ran. telemetrytest
// implements it; production passes nil. Methods must be safe for concurrent use and must
// not block.
type Observer interface {
	Record(r LogRecord)
	Span(s SpanData)
	Measurement(m Measurement)
	Violation(v Violation)
}

// LogRecord, SpanData and Measurement are plain copies for observers and tests.
type LogRecord struct {
	Time    time.Time
	Level   slog.Level
	Message string
	Attrs   map[string]string // flattened "group.key" -> value as text
	TraceID string            // 32 lower-case hex, or ""
}

type SpanData struct {
	Scope      string
	Name       string
	Kind       SpanKind
	TraceID    string
	ParentID   string // span id of the parent, "" for a root
	Attrs      map[string]string
	Failed     bool
	ErrorKind  string
	DurationMs int64
}

type Measurement struct {
	Scope      string
	Instrument string
	Value      float64
	Attrs      map[string]string
}

// ---- Meter ----

// Meter creates instruments. Instrument names are tokens of at most 128 bytes following
// OpenTelemetry's naming (dots between segments, no spaces). Safe for concurrent use.
type Meter interface {
	// Counter returns a monotonic counter. Fails with errs.Invalid for a bad name or unit
	// (F20) and errs.Conflict if name exists in this scope as another instrument kind or
	// with another unit (F21).
	Counter(name string, opts ...InstrumentOption) (Counter, error)
	// Histogram returns a histogram; WithBuckets sets explicit boundaries (default: the
	// OpenTelemetry defaults for the unit). Fails as Counter does (F20, F21).
	Histogram(name string, opts ...InstrumentOption) (Histogram, error)
	// Gauge registers observe, called on every collection; each Sample is one value with
	// its attributes. observe must be quick, safe for concurrent use and safe after its
	// owner closed (return nil). A panic inside it is recovered, logged once per
	// instrument and the collection skipped (F23). Fails with errs.Invalid if observe is
	// nil (F22) or the name is bad (F20), errs.Conflict as above (F21).
	Gauge(name string, observe func(ctx context.Context) []Sample, opts ...InstrumentOption) error
}

// Counter adds; Histogram records. Both are safe for concurrent use and never block.
// Attributes must be Token, Bool, Int, Int64 or Kind: a Text or ID attribute is dropped
// with a Violation (F19), because it would make the series unbounded.
type Counter interface {
	// Add adds n (n >= 0; a negative n is dropped with a Violation, F24).
	Add(ctx context.Context, n int64, attrs ...Attr)
}

type Histogram interface {
	Record(ctx context.Context, v float64, attrs ...Attr)
}

// Sample is one gauge observation.
type Sample struct {
	Value float64
	Attrs []Attr
}

// InstrumentOption configures an instrument.
type InstrumentOption func(*instrumentConfig)

// WithUnit sets the UCUM unit: "s", "ms", "By", "{request}", "{connection}". Default "".
func WithUnit(unit string) InstrumentOption

// WithDescription sets the English description shown by backends. No personal data.
func WithDescription(d string) InstrumentOption

// WithBuckets sets explicit histogram boundaries, strictly increasing. Histogram only.
func WithBuckets(bounds ...float64) InstrumentOption

// ---- Error references ----

// Ref is the code a person sees on an error screen or in an error body and can quote to a
// maintainer: "XXXX-XXXX", the first four bytes of the trace ID as upper-case hex with a
// hyphen (decision 3). It is short enough to read over a radio at an event and long
// enough to be unique among a regional's errors for far longer than logs are kept.
type Ref string

// Validate fails with errs.Invalid unless r is exactly "[0-9A-F]{4}-[0-9A-F]{4}" (F28).
func (r Ref) Validate() error

// RefFrom returns the reference for the trace ctx carries, and false when ctx carries no
// valid span (F27): then the caller omits the field. Cannot fail.
// Consumers: api (the error body and the per-error log line), intake (validation of the
// ref a client echoes back is Ref.Validate).
func RefFrom(ctx context.Context) (Ref, bool)

// TraceIDFrom returns the 32-character lower-case hex trace ID ctx carries, and false
// when it carries none (F27). Cannot fail.
// Consumers: audit (every entry records the trace ID, 09 §8), api (the per-error log
// line, so the reference and the full ID sit on one record).
func TraceIDFrom(ctx context.Context) (string, bool)
```

```go
// Package intake accepts error reports from the web shell (D20: POST /api/client-errors),
// limits them, validates and scrubs them, and logs each at warn with source=client. It is
// a service: api owns the route, the body cap and the status codes. It imports telemetry,
// model and lib.
package intake

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"time"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/model"
)

// Limits and caps. Constants, not settings: a regional has no reason to tune them, and a
// looser value only makes a misbehaving page louder (Settings).
const (
	MaxReports          = 20   // reports per batch
	MaxMessageBytes     = 512  // after Scrub; longer is truncated
	MaxStackBytes       = 4096 // after Scrub; longer keeps the top frames
	MaxUserAgentBytes   = 200
	MaxCount            = 1000 // a client's dedup counter
	PerAddressPerMinute = 30   // reports (not batches) per client address
	GlobalPerMinute     = 600  // reports per process
	MaxTrackedAddresses = 10_000
	RetryAfter          = time.Minute
	OccurredWithin      = 24 * time.Hour // |now - occurred_at| must be less
)

// ErrRateLimited is wrapped (errs.Unavailable) by every rejection for rate; api maps it to
// 429 with Retry-After: 60. Check it with errors.Is before the kind, as with db.ErrReadOnly.
var ErrRateLimited = errors.New("too many client error reports")

// Kind is what the web shell caught (D20).
type Kind string

const (
	KindCrash     Kind = "crash"     // an uncaught error
	KindRejection Kind = "rejection" // an unhandled promise rejection
	KindScreen    Kind = "screen"    // a React error boundary caught a crashed screen
	KindAPI       Kind = "api"       // an API call failed (network error or an error body)
)

// Report is one error the web shell caught. JSON field names are the wire form; unknown
// fields are ignored, so an older server accepts a newer shell.
type Report struct {
	Kind       Kind      `json:"kind"`
	Message    string    `json:"message"`               // free text; scrubbed; truncated at MaxMessageBytes
	Stack      string    `json:"stack,omitempty"`       // free text; scrubbed; truncated at MaxStackBytes
	Screen     string    `json:"screen,omitempty"`      // the route pattern, e.g. "/people/{id}"; a Token
	OccurredAt time.Time `json:"occurred_at"`           // RFC 3339; within OccurredWithin of now
	Count      int       `json:"count,omitempty"`       // how many times the shell saw it before sending; 0 means 1
	AppVersion string    `json:"app_version,omitempty"` // the web bundle's version; a Token of at most 64 bytes
	ErrorRef   string    `json:"error_ref,omitempty"`   // the Ref from a failed call's error body
	TraceID    string    `json:"trace_id,omitempty"`    // the traceparent trace ID the shell sent with that call; 32 hex
	API        *APICall  `json:"api,omitempty"`         // required when Kind is api; otherwise must be absent
}

// APICall describes the failed call for KindAPI. Never the URL, the query string, the
// request or response body.
type APICall struct {
	Method string `json:"method"` // GET, POST, PUT, PATCH, DELETE
	Route  string `json:"route"`  // the route pattern the shell called; a Token
	Status int    `json:"status"` // 0 for a network failure, else 100..599
	Code   string `json:"code"`   // the error body's code (an errs.Kind name) or ""
}

// Validate checks shape (F38–F44). It does not truncate: Accept does that after Scrub.
func (r Report) Validate() error

// Batch is the request body of POST /api/client-errors.
type Batch struct {
	Reports []Report `json:"reports"`
}

// Validate fails with errs.Invalid joining a validate.FieldError per problem, fields
// "reports" and "reports[i].<field>" (F37–F44).
func (b Batch) Validate() error

// Submission is what api hands Accept: the batch plus what only api knows.
type Submission struct {
	From      netip.Addr     // the client address api derived (config.md decision 16); used for the limit, never logged
	Person    model.PersonID // zero when the request has no session
	UserAgent string         // the User-Agent header; truncated at MaxUserAgentBytes
	Batch     Batch
}

// Options is what New needs.
type Options struct {
	Logger *slog.Logger    // records are written through it at warn with source=client
	Meter  telemetry.Meter // bms.client_errors.* counters
	Clock  clock.Clock     // the token buckets and OccurredWithin
}

// Intake is the service. Safe for concurrent use.
type Intake struct{ /* unexported: buckets map keyed by address (IPv6 by /64), global bucket, mu, counters */ }

// New fails with errs.Invalid if any option is nil (F35).
func New(opts Options) (*Intake, error)

// Accept takes one batch: checks ctx (F47); charges the batch's report count against the
// global bucket then the address's bucket, rejecting the whole batch with
// errs.Unavailable wrapping ErrRateLimited when either is short (F45, F46); validates the
// batch (F36–F44); then, for each report, scrubs and truncates the text fields, replaces
// UUID-shaped segments in Screen and API.Route with "{id}", and writes one log record at
// warn (Behavior: "What a client report becomes"). Returns nil once every record is
// written. It does no I/O other than logging.
// Consumer: api's handler for POST /api/client-errors.
func (i *Intake) Accept(ctx context.Context, s Submission) error
```

```go
// Package telemetrytest gives tests a Telemetry that keeps everything in memory and fails
// the test on any policy violation. No environment, no collector, no goroutines left.
package telemetrytest

// Capture is an Observer with accessors. New builds a telemetry.Telemetry with
// FormatText into a buffer, LevelDebug, a clock.Fake, an Environ that returns nothing (so
// nothing is exported) and this Capture as Observer; it registers t.Cleanup to Close it
// and to fail t if Violations is non-empty. Safe for concurrent use.
type Capture struct{ /* unexported */ }

// New cannot fail: a construction error stops the test with t.Fatal.
func New(t testing.TB) *Capture

// Telemetry returns the instance; Logger, Tracer(name) and Meter(name) are shortcuts.
func (c *Capture) Telemetry() *telemetry.Telemetry
func (c *Capture) Logger() *slog.Logger
func (c *Capture) Tracer(name string) telemetry.Tracer
func (c *Capture) Meter(name string) telemetry.Meter

// Records, Spans, Measurements and Violations return copies of what was captured so far.
func (c *Capture) Records() []telemetry.LogRecord
func (c *Capture) Spans() []telemetry.SpanData
func (c *Capture) Measurements() []telemetry.Measurement
func (c *Capture) Violations() []telemetry.Violation

// Contains reports whether needle occurs in any captured message, attribute value, span
// name or the stdout buffer: the one-line leak check every spec's test plan uses
// (`!c.Contains(marker)`). Cannot fail.
func (c *Capture) Contains(needle string) bool

// Stdout returns what the stdout handler wrote. For format tests.
func (c *Capture) Stdout() string
```

### Who needs each member

| Member | Consumer(s) | Pre-condition | Post-condition |
|---|---|---|---|
| `Options`, `Validate`, `New` | `app` (right after `config.Load`, before anything else logs); `telemetrytest` | valid options | providers built; nothing dialed |
| `Logger` | `app` (`Deps.Logger` with `module=<name>`; every core package's logger; the start-up line) | — | trace IDs on every `*Context` call; policy applied |
| `Tracer`, `Tracer.Start`, `Span` | `api` (one Server span per request), `db` (one Client span per statement), `signals` (Producer on publish, Consumer per delivery), `jobs` (Consumer per run), modules through the proposed `Deps.Tracer` (sub-spans in long use cases), `hello` | after `New` | a span that is ended once |
| `WithParentFrom` | `api` | the request's header | the trace continues from the browser's ID |
| `WithKind`, `WithAttrs` | `api`, `db`, `signals`, `jobs` | — | kind and start attributes set |
| `Span.RecordError` | `api` (the mapped error), `db` (a failed statement), `signals`, `jobs`, module services that open spans | — | failed span with kind and op |
| `Meter`, `Counter`, `Histogram`, `Gauge` | `api` (`http.server.request.duration`), `db` (`db.client.connection.*` gauges), `intake` (`bms.client_errors.*`), `signals` and `jobs` (counters per outcome), this package (`bms.telemetry.*`) | after `New` | bounded series |
| `Attr` constructors | every span and metric caller above; `Module` by `app` when scoping | — | policy applied before the value exists |
| `Scrub`, `MaxTextBytes`, `MaxLogValueBytes` | the log handler, `RecordError`, `Text`, `intake`; tests | — | e-mail shapes and query strings gone |
| `Violation`, `Observer`, `LogRecord`, `SpanData`, `Measurement` | `telemetrytest`; any later in-process check | — | copies after policy |
| `Ref`, `Validate`, `RefFrom` | `api` (error body field `ref`; the per-error log line); `intake` (validates `error_ref`); `web-shell` (shows it) | a span in ctx | `XXXX-XXXX` |
| `TraceIDFrom` | `audit` (09 §8), `api` (per-error log line) | a span in ctx | 32 hex |
| `Exporting` | `app` (start-up line), `bms check` | — | — |
| `Close` | `app`, last at shutdown | — | flushed within ctx |
| `intake.New`, `Accept`, `Batch`, `Report`, `Submission`, `ErrRateLimited`, constants | `api` (the route handler; maps `ErrRateLimited` to 429); `web-shell` (the wire form) | — | every accepted report is one log record |
| `telemetrytest.Capture` and accessors | `moduletest.Conformance` (`no_personal_data_in_logs`, Proposed change 1); every spec's leak tests (`db`, `config` already name a capturing handler); this package's tests | — | fails on violations |

## Types

Defines `Format`, `Options`, `Telemetry`, `Tracer`, `Span`, `SpanKind`, `StartOption`,
`Attr`, `Violation`, `Observer`, `LogRecord`, `SpanData`, `Measurement`, `Meter`,
`Counter`, `Histogram`, `Sample`, `InstrumentOption`, `Ref`; in `intake`: `Kind`, `Report`,
`APICall`, `Batch`, `Submission`, `Options`, `Intake`, `ErrRateLimited`; in
`telemetrytest`: `Capture`. Uses `clock.Clock`, `errs.Kind`, `errs.Error`,
`validate.Errors`, `validate.FieldError`, `model.PersonID`, `slog.Logger`, `slog.Level`,
`netip.Addr`, `http.Header`, and, inside the package only, the OpenTelemetry API and SDK
types (never in an exported signature, so a caller compiles against this spec alone).

## Data

No tables, no files. `telemetry` writes to `Options.Stdout` and to the collector; it
reads nothing from disk. What it emits, by field, with the sensitivity each would have if
it were stored by bms (privacy-data), and where retention is set:

| Field | Where | Level | Why it is needed | Retention |
|---|---|---|---|---|
| `trace_id`, `span_id`, `error_ref` | every log record in a request; spans; the error body | not about a person | the thread from a reference to a trace | stdout: operator; OTLP: collector (D20 proposes 30 days) |
| `person_id` (a `model.PersonID`) | log records and spans where the api or a service names the actor; client reports | internal (an opaque ID; the directory that resolves it is `people`, permission-checked) | "whose request failed" without a name | same |
| `module`, `op`, `error.kind`, `http.route`, `http.request.method`, status, durations, counts | logs, spans, metrics | not about a person | where and what | same |
| `client.message`, `client.stack` | client-report log records | internal; scrubbed (e-mail shapes, query strings) and capped | what broke in the browser | same |
| `user_agent` | client-report log records | internal; capped at 200 bytes | browser-specific crashes are the common kind | same |
| client address | intake's in-memory buckets only | personal (a network identifier) | the rate limit | held at most one idle minute; **never logged, never exported** |

Never emitted, enforced by the policy and by review: names (legal or playa), e-mail
addresses, phone numbers, addresses, emergency contacts, incident or medical text, form
values, request or response bodies, query strings, SQL text or arguments, session tokens,
passwords, `OTEL_EXPORTER_OTLP_HEADERS` (it may carry a collector credential), the
database URL. `secret.Secret` and `model.Email` print markers on their own; this spec adds
`Scrub`, the key denylist and the typed attributes for everything else.

Per-person export and deletion (privacy-data, module-contract `PersonData`) do not cover
telemetry: logs and spans hold a person's ID only, cannot be searched by person inside bms,
and expire with the collector's retention. That is the reason a bounded retention is part
of D20 and of the deploy examples (Proposed change 9); decision 18 sets the default.

## Permissions

| Operation | Permission | Scoped by | Default roles | Notes |
|---|---|---|---|---|
| `POST /api/client-errors` (`intake.Accept`) | none: `module.AccessPublic` | — | anyone, signed in or not | the sign-in page must be able to report its own crash (decision 9). Protection is the limits (`PerAddressPerMinute`, `GlobalPerMinute`, `MaxReports`, the api's 64 KiB body cap) and the fact that nothing is readable back. |
| Reading logs, traces and metrics | none in bms | — | the regional's maintainers | access control is the collector's and Grafana's (D20: "Grafana access is limited to the regional's maintainers"); the operator docs say how. |

Nothing else here is an operation on data: `New`, `Logger`, `Tracer`, `Meter` and `Close`
run with no actor. Every span and log record about a request is written by a package that
already checked the request's permission; telemetry adds no access.

## Signals

None published, none subscribed. `signals` wraps each delivery in a span through `Tracer`
(Proposed change 6); telemetry knows nothing about signals.

## Settings

None. Everything here is **process configuration** by config.md's rule ("differs per
process rather than per regional"): `log.level`, `log.format`, `instance.name` are in the
catalogue, and the collector is named by the operator's `OTEL_*` variables. No regional
behaves differently inside this spec; the regional's choices (which backend, what
retention, who sees Grafana) live in the deployment. Constants inside the package, pinned
by tests, with the reason each is not a key:

| Constant | Value | Why not configurable |
|---|---|---|
| span queue, log queue | 2048 records each (the SDK's default), non-blocking: full means drop and count | memory bound per process; `OTEL_BSP_MAX_QUEUE_SIZE` and `OTEL_BLRP_MAX_QUEUE_SIZE` are honoured by the SDK for an operator who insists |
| export timeout | 10 s per batch | shorter than the SDK's 30 s so a dead collector releases the exporter goroutine quickly; `OTEL_EXPORTER_OTLP_TIMEOUT` overrides |
| metric export interval | 60 s | `OTEL_METRIC_EXPORT_INTERVAL` overrides |
| sampler | `always_on` | a regional's traffic is small and every error must have its trace (decision 6); `OTEL_TRACES_SAMPLER` overrides within the supported set |
| export-failure warning | once per signal per minute | a down collector must not fill stdout |
| violation warning | once per `(Signal, Rule, Key)` per hour; always counted | the counter is the signal; the log line is the pointer |
| `MaxTextBytes`, `MaxLogValueBytes` | 512, 2048 | a message is a pointer to a trace, not a payload |
| intake limits and caps | the constants in the interface | see `intake` |

## UI

None of its own. What this spec provides for screens elsewhere:

- **Error screens and error bodies** (`web-shell.md`, `api.md`): the `ref` field, formatted
  `XXXX-XXXX`. Proposed wording, translation-ready: "Something went wrong. If you ask for
  help, quote the reference `1A2B-3C4D`." with a copy button. The reference is shown for
  every 5xx and for 4xx responses other than validation errors (a validation error shows
  its fields instead). Refined by `web-shell.md` decision 9 (2026-10-17): also not shown
  for `unauthenticated` (the person lands on sign-in), `read_only` (an expected state with
  its own banner) and the client's own `network` code (no server answered).
- **The maintainer's path** (`docs/operating.md`, Proposed change 9): in Grafana, Explore
  → Loki: `{service_name="bms"} | json | error_ref="1A2B-3C4D"` → the record carries
  `trace_id` → the derived field opens the trace in Tempo → Tempo's "logs for this trace"
  shows every record of the request. With text logs on a terminal: `grep 1A2B-3C4D`.
- **`bms check`**: prints "log: json/info", "OTLP export: on (traces, metrics, logs)" or
  "off", never a header value.

## Behavior

### Decisions settled here, with reasons

1. **Interfaces owned by this project, the OpenTelemetry SDK behind them; no other package
   imports `go.opentelemetry.io`.** The OpenTelemetry API is vendor-neutral and OSI-licensed,
   so lock-in is not the reason. The reason is the privacy policy: with the raw API any
   package can write `attribute.String("email", e)`; with `Attr` there is no constructor
   that takes a free string except `Text`, which scrubs and caps, and metrics refuse
   everything but tokens. A second reason is dependency hygiene: forty packages importing
   the SDK is forty places an upgrade touches. The cost is a thin wrapper (`Start`,
   `SetAttrs`, `RecordError`, `End`, three instrument kinds), small enough that
   `telemetrytest` reimplements its observable side in memory. depguard enforces the
   boundary (Proposed change 8).
2. **The trace ID is the request ID; there is no second one.** Two IDs means two things to
   copy and a mapping to maintain. Every log record of a request carries `trace_id`, every
   error response carries a reference derived from it, and `audit` records it. The api may
   add an `X-Trace-Id` response header for `curl` users if it wants; this spec does not
   need it.
3. **The error reference is the first four bytes of the trace ID, `XXXX-XXXX`.** A
   separate random code would need its own log field and its own join; deriving it means
   the reference *is* the trace, and the log line the api writes for every error response
   (with `error_ref` and the full `trace_id`) makes the lookup a one-field Loki query even
   when the trace itself was sampled away or dropped. Thirty-two bits is enough: a regional
   produces thousands of errors a month, not billions, and the maintainer also has a date.
   Upper-case hex has no `O`/`0` or `l`/`1` ambiguity beyond `0`, which is the only one of
   the pair present. Format confirmed: `XXXX-XXXX`, no prefix (owner, 2026-10-16).
4. **The span and log providers are always the SDK's, even with no collector; only the
   exporters depend on `OTEL_*`.** With a no-op provider there would be no trace IDs, so no
   references and no `trace_id` on log lines, which a regional running without Grafana still
   wants in its stdout logs. Spans with no exporter cost an allocation and nothing else.
5. **OTLP over HTTP/protobuf only; `grpc` is refused at start.** Every backend in D20's
   stack and every OpenTelemetry Collector accepts OTLP/HTTP; gRPC would add the largest
   dependency in the binary for a second way to do the same thing. `OTEL_EXPORTER_OTLP_PROTOCOL`
   set to anything but `http/protobuf` fails `New` with the variable's name (F6), rather
   than silently exporting nothing.
6. **The server decides sampling; the browser's `traceparent` contributes only IDs; the
   default is `always_on`; `parentbased_*` samplers are refused.** D20 wants one trace per
   click and every error traceable. A browser can set the sampled flag to anything, so it
   is ignored, and because it is ignored a parent-based sampler would see an unsampled
   remote parent and drop every browser-originated request. `always_on`, `always_off` and
   `traceidratio` (deterministic on the ID) are supported through `OTEL_TRACES_SAMPLER`;
   the others fail `New` (F8). A regional's traffic is hundreds of people, so 100% costs
   little. Default `always_on`, operator override with `OTEL_TRACES_SAMPLER=traceidratio`,
   confirmed (owner, 2026-10-16).
7. **One log handler applies the policy before fan-out.** Scrubbing in `ReplaceAttr` on the
   stdout handler alone would leave the OTLP copy unscrubbed. The handler rewrites the
   record (keys checked, strings scrubbed and capped, `trace_id`/`span_id` added) and then
   calls the stdout handler and, when exporting, the OTLP bridge, so both sinks see the
   same bytes. `WithAttrs` and `WithGroup` pre-apply the same rewrite.
8. **Attributes are typed; the only free-text constructor scrubs and caps; metrics take
   tokens only.** Names and phone numbers have no reliable shape, so detection cannot be the
   defence; the defence is that the natural way to write an attribute is an ID, a token or
   a number. `Scrub` catches the one personal-data shape that is unambiguous (an e-mail
   address) and the one that smuggles everything (`?query`). On metrics, a free string or
   an ID is an unbounded series, which is also a memory leak, so both are refused.
9. **The client-error route is public, rate-limited per address and globally, and the
   address is never logged.** `POST /api/client-errors` is reachable without a session,
   protected by the limits (owner, 2026-10-16). A crash on the sign-in page is exactly the
   kind a maintainer never hears about otherwise. Making the route public means the only protection is
   limits, so they are strict (30 reports per address per minute, 600 per process) and the
   whole batch is refused when short, so a runaway page cannot trickle through. The client
   address is needed for the limit and for nothing else; it is personal data under most
   privacy law and D20 lists no use for it, so it lives in memory for the bucket's life
   and nowhere else.
10. **`intake` is a service, not an `http.Handler`.** The error body format, the body cap,
    the client address derivation and the status codes are the api's; a handler here would
    duplicate the error mapper or import `api`, which the layer rules forbid. `Accept`
    returns kinds, plus the `ErrRateLimited` sentinel for the one status (`429`) that no
    kind expresses, following `db.ErrReadOnly`'s precedent.
11. **Text fields in a report are truncated; shape violations are rejected.** A client
    cannot know a string's byte length after scrubbing, so the server cuts `message` and
    `stack` rather than bounce the batch; but a wrong `kind`, a route that is not a token,
    a malformed `trace_id` or a timestamp from last year are bugs in the shell, and bouncing
    them with field errors (`reports[2].screen: must be a route pattern`) is how a
    front-end author finds out.
12. **A down collector costs nothing but a counter and one warning a minute.** Exporters
    are wrapped to count failures per signal (`bms.telemetry.export_failures{signal}`) and
    the SDK's global error handler is routed to the logger under the same rate limit. Queues
    are bounded and non-blocking (drops counted as `bms.telemetry.dropped{signal}`). `New`
    never dials; `Close` flushes within the caller's deadline and returns `Timeout` rather
    than hang. A request's latency is unaffected because every export runs on the SDK's
    own goroutines.
13. **Policy violations are corrected, counted and, in tests, fatal.** Dropping a log
    record because of a bad key would hide the very error being reported; failing a
    request over a span name would be worse. So the value becomes a marker, the record
    still goes out, `bms.telemetry.policy_violations{signal,rule}` increments and one
    warning names the key. `telemetrytest.Capture` fails the test on any violation, so a
    bad key never reaches a release that was tested.
14. **Durations on attributes are integer milliseconds; the histogram unit is seconds.**
    OpenTelemetry's semantic conventions use seconds as a double for
    `http.server.request.duration`, and backends expect it; for an attribute, an integer
    with an `_ms` suffix reads better in a log line than `0.0123`.
15. **Resource attributes are `service.name`, `service.version`, `service.instance.id`
    and whatever `OTEL_RESOURCE_ATTRIBUTES` adds; no host or process detection.** The
    host name is the operator's business and not needed to tell copies apart:
    `service.instance.id` is `instance.name` (config.md), which is also audit's "which
    copy". `OTEL_SERVICE_NAME` overrides `bms` for an operator running two installations
    into one collector.
16. **The stdout handler is never closed.** `Close` flushes exporters; a record logged
    after `Close` (a shutdown error in `app`) must still reach the terminal.
17. **Client error reports include the browser's `User-Agent`, capped at 200 bytes**
    (owner, 2026-10-16). Most browser crashes are browser-specific, so without it the
    maintainer has to ask which browser the person used. It is internal-level data (Data),
    cut at `MaxUserAgentBytes`, and bounded by the collector's retention like every other
    field of the record.
18. **The example deployment keeps logs and traces for 30 days and metrics for 1 year,
    and the operator docs say that per-person export and deletion do not reach telemetry**
    (owner, 2026-10-16). Logs and spans hold IDs only and expire with the collector, so a
    bounded retention is what makes "IDs only, bounded retention" a true privacy statement;
    metrics hold no IDs, so a year costs nothing in privacy terms. bms cannot enforce
    retention; the values are set in Loki, Tempo and Prometheus configuration and stated in
    `docs/operating.md` (Proposed change 9).
19. **A span name is a token, or `<METHOD> <route pattern>`: exactly one space, between an
    upper-case HTTP method and a route pattern, and nothing else new** (owner,
    2026-10-16). The first draft allowed tokens only, which rejected the api's request
    span name (`GET /api/people/{id}`, Proposed change 3) and the `signal <name>` form this
    spec had suggested to `signals` (signals.md Proposed change 3 reported both).
    OpenTelemetry's HTTP semantic conventions name a server span `{method} {route}`, and
    every backend in D20 groups, searches and derives rates by that form; `GET:/api/...`
    would be a private dialect. So the grammar admits `[A-Z]{3,10}`, one space, then a
    token beginning with `/`: a lower-case method, two spaces, a space anywhere else or a
    name without the leading `/` still fail (F14). The route is the pattern, never the
    concrete path, for the same reason as `http.route` and `screen`: a concrete path puts
    an ID in a name and makes a series per record. Signals are not HTTP and keep the token
    names `signal:<name>` and `deliver:<name>` (signals.md decision 12).
20. **The token grammar admits `_` as a first byte, so `_OTHER` is a token** (owner,
    2026-10-17; api.md Proposed change 6). The api normalises the request method to one
    of the seven known methods or `_OTHER` before it becomes a span attribute or a
    histogram label (api.md decision 10), and `_OTHER` is the spelling OpenTelemetry's HTTP
    semantic conventions use; a private spelling (`OTHER`) would make every dashboard
    built on the conventions miss it. The grammar becomes
    `[A-Za-z0-9/_][A-Za-z0-9._:/{}-]{0,127}`: exactly the first byte changes, nothing
    else is admitted. Span names are unaffected: the span for an unknown method is
    `HTTP <route token>` (the method part `[A-Z]{3,10}` already admits `HTTP`), never
    `_OTHER <route>`.

### What is read from the environment

Read by this package through `Options.Environ` and validated here:

| Variable | Use | Valid |
|---|---|---|
| `OTEL_SDK_DISABLED` | `true` turns every exporter off; the providers still run (decision 4) | `true`, `false`, unset (F9) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | base URL; each signal appends `/v1/traces`, `/v1/metrics`, `/v1/logs` | absolute `http` or `https` URL (F7) |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, `_METRICS_ENDPOINT`, `_LOGS_ENDPOINT` | a full URL for one signal; wins over the base for that signal | absolute `http`/`https` URL (F7) |
| `OTEL_EXPORTER_OTLP_PROTOCOL` and the three signal variants | must be `http/protobuf` when set (decision 5) | (F6) |
| `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG` | sampler | `always_on` (default), `always_off`, `traceidratio` with ARG in `[0,1]` (F8) |
| `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES` | resource | as the SDK parses them |

Read by the SDK itself, not validated here beyond what the SDK does (a bad value falls
back to the SDK default and the SDK's warning reaches the logger through the error
handler): `OTEL_EXPORTER_OTLP_HEADERS` (and signal variants; may carry a credential:
never logged, never printed by `bms check`), `OTEL_EXPORTER_OTLP_TIMEOUT`,
`OTEL_EXPORTER_OTLP_CERTIFICATE`, `_CLIENT_CERTIFICATE`, `_CLIENT_KEY`, `_COMPRESSION`,
`OTEL_BSP_*`, `OTEL_BLRP_*`, `OTEL_METRIC_EXPORT_INTERVAL`, `OTEL_METRIC_EXPORT_TIMEOUT`,
`OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT`, `OTEL_SPAN_ATTRIBUTE_COUNT_LIMIT`. `OTEL_TRACES_EXPORTER`
and its siblings (the autoexport convention) are not read: the exporter is OTLP when an
endpoint is set and nothing otherwise. A signal exports iff `OTEL_SDK_DISABLED` is not
`true` and it has an endpoint (its own or the base).

### `New`, step by step

1. If `ctx` is done, fail (F10). `opts.Validate()` (F1–F5); `Environ` defaults to
   `os.LookupEnv`; `Observer` may be nil.
2. Read the variables above (F6–F9). Decide per signal whether it exports.
3. Build the resource: `service.name=bms`, `service.version`, `service.instance.id`, merged
   with `OTEL_SERVICE_NAME` and `OTEL_RESOURCE_ATTRIBUTES` (environment wins), plus the
   SDK's `telemetry.sdk.*`.
4. Tracer provider: the sampler from step 2; if traces export, an OTLP/HTTP span exporter
   with the endpoint from step 2, wrapped by the failure counter, behind a batch processor
   (queue 2048, export timeout 10 s, non-blocking). An exporter constructor error is F11.
5. Meter provider: if metrics export, an OTLP/HTTP metric exporter behind a periodic reader
   (60 s, 10 s timeout); cumulative temporality; otherwise no reader (instruments still
   work and cost nothing). Register the package's own instruments
   (`bms.telemetry.export_failures`, `dropped`, `policy_violations`).
6. Logger provider: if logs export, an OTLP/HTTP log exporter behind a batch processor,
   and the slog bridge handler over it.
7. The stdout handler (`slog.NewJSONHandler` or `NewTextHandler`, `Level`, `AddSource`
   when `Level <= Debug`), then the policy handler over stdout and the bridge, then the base
   logger `With("instance", opts.Instance)`.
8. Route the SDK's global error handler to the base logger at warn, rate-limited (once per
   message per minute). Set the global text-map propagator to W3C Trace Context (used only
   by `WithParentFrom` and a later `Inject`). No other global is set.
9. Return. Nothing has been dialed. If any step after 4 fails, the providers built so far
   are shut down before returning.

### The log handler

For every record: check the key of each attribute (and of each group's attributes) against
the grammar and the denylist; string values through `Scrub` and the cap; `time`, `level`,
`msg` as slog writes them; `trace_id` and `span_id` added when the ctx carries a valid
span (so a call without ctx has none: `sloglint` with `context: scope` makes the
`*Context` form mandatory wherever a ctx exists). `Handle` returns the stdout handler's
error (slog ignores it; the package counts it as `bms.telemetry.log_write_failures` and
never panics, F32). A record below `Level` is dropped before any work. The handler itself
never logs.

### The key denylist

A key is normalised (lower-cased, `.` and `-` replaced by `_`) and denied when it equals
an entry or ends with `_` followed by one: `email`, `e_mail`, `legal_name`, `first_name`,
`last_name`, `full_name`, `display_name`, `playa_name`, `phone`, `tel`, `mobile`,
`address`, `street`, `emergency_contact`, `password`, `passwd`, `secret`, `token`,
`authorization`, `cookie`, `set_cookie`, `session`, `body`, `request_body`,
`response_body`, `query`, `query_string`, `url_full`, `url_query`, `sql`, `statement`,
`args`, `params`, `form`, `narrative`, `notes`, `medical`, `diagnosis`. The list is a
package constant pinned by a test; a reviewer adds to it when a new personal field
appears in a spec. It is a second line, not the first: the first is that services log
IDs (go-conventions) and that `secret.Secret` and `model.Email` redact themselves.

### `Scrub`

E-mail shape: a maximal run of `[^\s@<>,;"]+@[^\s@<>,;"]+\.[^\s@<>,;"./]+` → `[email]`.
Query string: in a token that contains `://` or starts with `/`, everything from the
first `?` to the next whitespace or end → `?[query]`. Applied once; idempotent. The
regular expressions are compiled at package init and pinned by a table test with the
false-positive cases (`a@b` without a dot is left; `user@host:5432/db` in a pgx connect
error is left, since it has no `.`-terminated domain after `@`... and db already drops
the URL anyway; a UUID is left; an RFC 3339 time is left).

### Spans

`Start` builds the span through the SDK tracer of the scope, with the kind and start
attributes; `WithParentFrom` parses `traceparent` by hand (version `00`, 32-hex trace ID
not all zero, 16-hex parent ID not all zero, 2-hex flags) and builds a remote parent span
context with flags cleared; anything else is a root. `End` computes `SpanData` for the
observer and ends the SDK span. `RecordError`: `error.kind`, `error.op` (first
`*errs.Error` by `errors.As`), `exception.message` (scrubbed, capped); status Error unless
the kind is `Canceled`. Span names are tokens, or the HTTP form `<METHOD> <route pattern>`
(decision 19); instrument names are tokens; scope names are tokens of at most 64 bytes.

### `intake.Accept`, step by step

1. If `ctx` is done, fail (F47). `s.From` must be valid (F36); IPv6 addresses are keyed by
   their `/64`.
2. `n := len(s.Batch.Reports)`; `n` must be `1..MaxReports` (F37) before any bucket is
   charged. Take `n` tokens from the global bucket (`GlobalPerMinute`, burst equal to the
   rate), then from the address's bucket (`PerAddressPerMinute`); if either is short, put
   back what was taken, count `bms.client_errors.rejected{reason="rate_limited"}` by `n`,
   log "client error reports rate-limited" once per hour process-wide with the count since
   the last line (no address), and fail (F45, F46). The address table holds at most
   `MaxTrackedAddresses` buckets; when full, the bucket idle longest is evicted; buckets
   idle for a minute are swept on the next call (the clock drives both; no goroutine).
3. `s.Batch.Validate()` (F38–F44); a failure counts `rejected{reason="invalid"}` by `n` and
   returns the joined field errors. `OccurredAt` is compared with `Clock.Now()`.
4. For each report: `Scrub` and cut `Message` and `Stack`; replace UUID-shaped and
   32-hex segments in `Screen` and `API.Route` with `{id}`; `Count` 0 → 1; truncate
   `UserAgent`; then write one record (below). Count `accepted{kind}` by `Count`.
5. Return nil.

### What a client report becomes

One record at `warn`, message `client error`, attributes: `source=client`,
`client.kind`, `client.message`, `client.stack` (omitted when empty), `client.screen`,
`client.occurred_at` (RFC 3339), `client.count`, `client.app_version`, `client.error_ref`,
`client.trace_id` (the reported one; the record's own `trace_id` is the intake request's),
`client.api.method`, `client.api.route`, `client.api.status`, `client.api.code` (for
`KindAPI`), `user_agent`, `person_id` (when `Person` is non-zero). The web shell's
`trace_id` of a failed call equals the server trace of that call (decision 6), so Loki's
`client.trace_id` leads to the server-side spans and logs of the same failure.

### Metrics defined by this spec and its consumers (M0)

| Instrument | Kind, unit | Attributes | Recorded by |
|---|---|---|---|
| `http.server.request.duration` | histogram, `s` | `http.request.method`, `http.route`, `http.response.status_code` | `api` |
| `db.client.connection.count` | gauge, `{connection}` | `state` = `idle` or `used` | `db` from `Pool.Stats` |
| `db.client.connection.max` | gauge, `{connection}` | — | `db` |
| `db.client.connection.pending_requests` | gauge, `{request}` | — | `db` |
| `bms.client_errors.accepted` | counter, `{report}` | `kind` | `intake` |
| `bms.client_errors.rejected` | counter, `{report}` | `reason` = `rate_limited` or `invalid` | `intake` |
| `bms.telemetry.export_failures` | counter, `{batch}` | `signal` | this package |
| `bms.telemetry.dropped` | counter, `{record}` | `signal` | this package |
| `bms.telemetry.policy_violations` | counter, `{violation}` | `signal`, `rule` | this package |
| `bms.telemetry.log_write_failures` | counter, `{record}` | — | this package |
| `bms.signals.published`, `bms.signals.delivered`, `bms.jobs.runs` | counter | `signal`/`job` (token), `module`, `outcome` (`delivered`: `ok`, `failed`, `deferred`, `read_only`, `panicked`; `published` has none) | `signals` (signals.md "Telemetry"), `jobs` (Proposed change 6) |

Rates, errors and durations per route also come from Tempo's metrics generator over the
spans (`deploy/compose/observability/tempo.yaml`), so a regional that exports traces only
still gets a RED dashboard.

### Concurrency and lifecycle

`*Telemetry`, every `Tracer`, `Span`, `Meter`, `Counter`, `Histogram` and `*intake.Intake`
are safe for concurrent use. `Span` methods after `End` are no-ops. Goroutines: only the
SDK's batch processors and periodic reader, started in `New`, stopped and waited for in
`Close`; `intake` runs none (the sweep happens on calls). `app` calls `New` once per
command before `db.Open` and `Close` once, last, after `db.Close`, within
`http.shutdown_timeout`; a `New` failure is printed by `cmd/bms` to stderr as
`<VARIABLE>: <reason>` like a config failure, since no logger exists yet.

### Imports

`telemetry`: standard library (`context`, `errors`, `fmt`, `io`, `log/slog`, `net/http`,
`net/url`, `os`, `regexp`, `strings`, `sync`, `sync/atomic`, `time`, `unicode/utf8`),
`internal/lib/errs`, `internal/lib/validate`, `internal/lib/clock`, and
`go.opentelemetry.io/otel` (`attribute`, `codes`, `propagation`, `trace`, `metric`,
`log`), `go.opentelemetry.io/otel/sdk` (`resource`, `trace`, `metric`, `log`),
`go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`,
`.../otlpmetric/otlpmetrichttp`, `.../otlplog/otlploghttp`,
`go.opentelemetry.io/contrib/bridges/otelslog`, `go.opentelemetry.io/otel/semconv/v1.26.0`
(or the version current at implementation). Not `model` (the `ID` constructor is generic
over `~[16]byte`), not `config`, not `core/db`, not pgx. `intake` adds `internal/model`,
`net/netip`. `telemetrytest` adds `testing`. depguard: `go.opentelemetry.io` allowed only
under `internal/core/telemetry/**` (Proposed change 8).

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `Options.Validate` | `Level` not one of the four | Invalid, field `log.level` | fix app | `TestOptionsValidate_BadLevel_ReturnsInvalid` |
| F2 | `Options.Validate` | `Format` not `json`/`text` | Invalid, `log.format` | fix app | `TestOptionsValidate_BadFormat_ReturnsInvalid` |
| F3 | `Options.Validate` | `Instance` fails the grammar | Invalid, `instance.name` | fix app | `TestOptionsValidate_BadInstance_ReturnsInvalid` |
| F4 | `Options.Validate` | `Version` empty, not a token, or over 64 bytes | Invalid, `telemetry.version` | fix the build | `TestOptionsValidate_BadVersion_ReturnsInvalid` |
| F5 | `Options.Validate` | `Stdout` or `Clock` nil; several of F1–F5 at once | Invalid, one `FieldError` each | fix app | `TestOptionsValidate_NilDepsAndMany_ReportsEach` |
| F6 | `New` | `OTEL_EXPORTER_OTLP_PROTOCOL` (or a signal variant) set to anything but `http/protobuf` | Invalid, field = the variable, reason "only http/protobuf is supported" | unset it or set `http/protobuf` | `TestNew_GRPCProtocol_ReturnsInvalidNamingVariable` |
| F7 | `New` | an endpoint variable is not an absolute `http`/`https` URL | Invalid, field = the variable; never echoes the value (it may carry userinfo) | fix the URL | `TestNew_BadEndpoint_ReturnsInvalidWithoutEcho` |
| F8 | `New` | `OTEL_TRACES_SAMPLER` not in the supported set (`parentbased_*`, `jaeger_remote`, `xray`, unknown), or `traceidratio` with ARG missing, not a number or outside `[0,1]` | Invalid naming the variable and listing the supported values | fix it | `TestNew_UnsupportedSampler_ReturnsInvalid` |
| F9 | `New` | `OTEL_SDK_DISABLED` not `true`/`false` | Invalid | fix it | `TestNew_BadSDKDisabled_ReturnsInvalid` |
| F10 | `New` | `ctx` done | Canceled / Timeout | stop | `TestNew_ContextCanceled_ReturnsCanceled` |
| F11 | `New` | an exporter constructor fails (an unreadable `OTEL_EXPORTER_OTLP_CERTIFICATE`) | Invalid wrapping the SDK error, naming the signal; providers built so far shut down | fix the file | `TestNew_ExporterConstructionFails_ReturnsInvalidAndCleansUp` |
| F12 | `Tracer`, `Meter` | name not a token or over 64 bytes | Invalid | fix the caller | `TestTracerMeter_BadScopeName_ReturnsInvalid` |
| F13 | `Tracer`, `Meter` | after `Close` | Internal | fix app ordering | `TestTracerMeter_AfterClose_ReturnsInternal` |
| F14 | `Tracer.Start` | name neither a token nor `<METHOD> <route pattern>` (`[A-Z]{3,10}`, one space, a token beginning with `/`; decision 19), or over 128 bytes: a lower-case method (`get /x`), two spaces, a space elsewhere (`signal hello.greeted`), a route without `/` (`GET x`) | none: span named `invalid-span-name`; `Violation{trace, invalid_span_name}` | fix the caller (the test fails) | `TestStart_BadName_UsesPlaceholderAndCountsViolation` (table of the rejected shapes), `TestStart_HTTPSpanName_Accepted` (`GET /api/people/{id}`, `POST /api/client-errors` pass unchanged) |
| F15 | any `Attr` constructor | key on the denylist | none: value `[denied]`; `Violation{…, denied_key}` | fix the caller | `TestAttr_DeniedKey_RedactsAndCountsViolation` |
| F16 | any `Attr` constructor | key fails the grammar | none: `telemetry.invalid_key=<key>`; `Violation{…, invalid_key}` | fix the caller | `TestAttr_InvalidKey_ReplacedAndCounted` |
| F17 | `Token`, `Module` | value fails the token grammar (a space, `@`, `?`, a first byte other than `[A-Za-z0-9/_]` such as `.` or `-`, over 128 bytes) | none: `[invalid-token]`; `Violation{…, invalid_token}` | fix the caller | `TestToken_InvalidValue_ReplacedAndCounted` (its table also pins `_OTHER` accepted and `.x`, `-x` rejected; decision 20) |
| F18 | `Text`, `RecordError` | value over `MaxTextBytes`, or containing an e-mail shape or a query string | none: scrubbed, cut with `…`; no violation (expected use) | — | `TestText_ScrubsAndTruncates` |
| F19 | `Counter.Add`, `Histogram.Record`, gauge samples | a `Text` or `ID` attribute | none: that attribute dropped; `Violation{metric, text_on_metric/id_on_metric}` | fix the caller | `TestMetric_TextOrIDAttr_DroppedAndCounted` |
| F20 | `Meter.Counter/Histogram/Gauge` | name not a token or over 128 bytes; unit not UCUM-shaped (contains a space); non-increasing buckets | Invalid | fix the caller | `TestMeter_BadNameUnitOrBuckets_ReturnsInvalid` |
| F21 | same | name already registered in this scope as another kind or unit | Conflict | one definition per name | `TestMeter_DuplicateNameDifferentKind_ReturnsConflict` |
| F22 | `Meter.Gauge` | `observe` nil | Invalid | fix the caller | `TestGauge_NilObserve_ReturnsInvalid` |
| F23 | gauge collection | `observe` panics | none: recovered, logged at error once per instrument, collection skipped | fix the observer | `TestGauge_ObservePanics_RecoveredAndLoggedOnce` |
| F24 | `Counter.Add` | `n < 0` | none: dropped; `Violation{metric, negative_count}` | fix the caller | `TestCounterAdd_Negative_DroppedAndCounted` |
| F25 | export (any signal) | collector unreachable, 4xx/5xx, timeout, queue full | none to any caller: `bms.telemetry.export_failures{signal}` / `dropped{signal}`; one warn per signal per minute; requests unaffected | fix the collector | `TestExport_CollectorDown_NeverBlocksCountsAndWarnsOnce` |
| F26 | `Close` | `ctx` ends before flush and shutdown finish | Timeout / Canceled, per-provider errors joined; second `Close` nil | log and exit | `TestClose_ContextExpired_ReturnsTimeout`, `TestClose_Twice_IsNil` |
| F27 | `RefFrom`, `TraceIDFrom` | no valid span in `ctx` | none: `ok == false` | omit the field | `TestRefFrom_NoSpan_ReturnsFalse` |
| F28 | `Ref.Validate` | not `[0-9A-F]{4}-[0-9A-F]{4}` (lower case, no hyphen, wrong length) | Invalid | 400 (intake field error) | `TestRefValidate_Table` |
| F29 | log handler | a string value over `MaxLogValueBytes` | none: cut with `…` | — | `TestHandler_LongValue_Truncated` |
| F30 | log handler | an e-mail shape or query string in `msg` or a value | none: `[email]` / `?[query]` | — | `TestHandler_ScrubsMessageAndValues` |
| F31 | log handler | a denied key (also inside a group, also via `With`) | none: `[denied]`; `Violation{log, denied_key}` | fix the caller | `TestHandler_DeniedKey_RedactsInGroupsAndWith` |
| F32 | log handler | the stdout writer fails | `Handle` returns the error (slog drops it); `bms.telemetry.log_write_failures` counted; no panic; the OTLP copy still sent | nothing: stdout is gone | `TestHandler_StdoutWriteFails_CountsNoPanic` |
| F33 | `WithParentFrom` | missing or malformed `traceparent`; `tracestate` present | none: root span; `tracestate` ignored; the sampled flag ignored even when valid | — | `TestWithParentFrom_Table` (valid, bad version, short, zero IDs, flags cleared) |
| F34 | `Span` methods | after `End` | none: no-op | — | `TestSpan_AfterEnd_NoOp` |
| F35 | `intake.New` | `Logger`, `Meter` or `Clock` nil | Invalid | fix app | `TestIntakeNew_NilDependency_ReturnsInvalid` |
| F36 | `Accept` | `From` not valid | Invalid | fix api | `TestAccept_InvalidAddress_ReturnsInvalid` |
| F37 | `Accept`, `Batch.Validate` | zero or more than `MaxReports` reports | Invalid, field `reports` | 400 | `TestAccept_EmptyOrTooManyReports_ReturnsInvalid` |
| F38 | `Batch.Validate` | `kind` not one of the four | Invalid, `reports[i].kind` | 400 | `TestBatchValidate_BadKind_ReturnsInvalid` |
| F39 | `Batch.Validate` | `api` absent for `kind=api`, or present for another kind; method not one of five; route not a token; status not 0 or 100..599; code not an `errs.Kind` name or `""` | Invalid, `reports[i].api.<field>` | 400 | `TestBatchValidate_APICall_Table` |
| F40 | `Batch.Validate` | `screen` not a token | Invalid, `reports[i].screen`, reason "must be a route pattern" | 400 | `TestBatchValidate_ScreenNotToken_ReturnsInvalid` |
| F41 | `Batch.Validate` | `error_ref` fails `Ref.Validate`; `trace_id` not 32 lower-case hex | Invalid | 400 | `TestBatchValidate_RefAndTraceID_ReturnInvalid` |
| F42 | `Batch.Validate` (with the clock) | `occurred_at` zero or more than `OccurredWithin` from now | Invalid, `reports[i].occurred_at` | 400 | `TestAccept_OccurredAtOutOfWindow_ReturnsInvalid` |
| F43 | `Batch.Validate` | `count` negative or over `MaxCount` | Invalid | 400 | `TestBatchValidate_Count_ReturnsInvalid` |
| F44 | `Batch.Validate` | `app_version` not a token or over 64 bytes; `message` empty | Invalid | 400 | `TestBatchValidate_VersionAndMessage_ReturnInvalid` |
| F45 | `Accept` | the address's bucket short by the batch size | Unavailable wrapping `ErrRateLimited`; `rejected{rate_limited}`; no per-report log; one hourly line without the address | 429, `Retry-After: 60` | `TestAccept_PerAddressLimit_ReturnsRateLimitedWithoutLoggingAddress` |
| F46 | `Accept` | the global bucket short | same as F45 | same | `TestAccept_GlobalLimit_ReturnsRateLimited` |
| F47 | `Accept` | `ctx` done at entry | Canceled / Timeout | stop | `TestAccept_ContextCanceled_ReturnsCanceled` |
| F48 | `Accept` | `message` or `stack` over its cap, or carrying an e-mail or a query string | none: scrubbed and cut; accepted | — | `TestAccept_TextFields_ScrubbedAndTruncated` |
| F49 | `Accept` | a UUID-shaped segment in `screen` or `api.route` | none: replaced by `{id}`; accepted | — | `TestAccept_IDSegments_ReplacedWithPlaceholder` |
| F50 | `Accept` | the address table is full | none: the longest-idle bucket evicted; accepted | — | `TestAccept_AddressTableFull_EvictsIdle` |

Members that cannot fail, and why: `Logger` (returns a value built by `New`); `Exporting`
(a flag); `Span.SetAttrs`, `RecordError`, `End` (every bad input is corrected, decision 13);
`Int`, `Int64`, `Float`, `Bool`, `Duration`, `ID`, `Kind` (key rules apply, but the result is
always a usable `Attr`); `Scrub` (pure); `Ref.String`; `Capture` accessors (copies). Each
has a success-path test.

## Lock-in check

External technology wrapped: the OpenTelemetry Go API and SDK and the OTLP exporters
(Apache-2.0); the OTLP protocol (an open specification). Behind them, the reference backend
is Grafana, Loki and Tempo (AGPL-3.0), Prometheus and Alloy (Apache-2.0): every piece
OSI-licensed and self-hosted (D20), with `deploy/compose/observability` as the running
proof. Portability is proven twice: (1) any OTLP/HTTP receiver works unchanged, for example
the OpenTelemetry Collector (Apache-2.0) with Jaeger (traces, Apache-2.0), Prometheus
(metrics) and OpenSearch (logs, Apache-2.0), or SigNoz (MIT) — the integration test uses a
plain `httptest` OTLP receiver, not Grafana; (2) the interfaces (`Tracer`, `Meter`, `slog`)
name nothing OpenTelemetry-specific, and `telemetrytest` is a complete in-memory
implementation of their observable behaviour, so a regional that wanted neither could
implement them over `log/slog` alone. No hosted service is required; stdout JSON logs with
trace IDs work with no collector at all.

## Test plan

**Unit** (no infrastructure, `go test -race`, packages `telemetry_test`, `intake_test`,
`telemetrytest_test`, plus internal tests for the handler and the parser; table-driven;
fake clock; every leak test uses the markers `ZQ9MARKER7` and `zq9@marker.example` and
asserts `!c.Contains(marker)`; invented people only):

- Options and New: F1–F11; `TestNew_NoEnvironment_WorksWithoutExport` (`Exporting()` false;
  a span still has a trace ID; a log line has `trace_id`); `TestNew_EndpointPerSignal_Table`
  (base only; signal-specific wins; `OTEL_SDK_DISABLED=true` turns all off);
  `TestNew_ResourceAttributes` (service name default and override, version, instance id);
  `TestNew_NeverDials` (an endpoint on a closed port; `New` returns at once).
- Logger: `TestHandler_JSONShape` (keys `time`, `level`, `msg`, `instance`, `trace_id`,
  `span_id`, attributes; one line per record); `TestHandler_TextFormat`;
  `TestHandler_LevelFilter`; `TestHandler_AddSourceAtDebugOnly`;
  `TestHandler_SecretAndEmailLogValueRedacted` (`slog.Any` on a `secret.Secret` and a
  `model.Email` prints markers through this handler); F29–F32;
  `TestHandler_RecordWithoutContext_HasNoTraceID`.
- Tracer and spans: `TestStart_ChildOfContextSpan`; `TestStart_RootWhenNoSpan`;
  `TestSpan_RecordError_KindOpMessage` (an `errs.E` chain: `error.kind`, `error.op`,
  scrubbed message; `Canceled` not marked failed); `TestSpan_KindsAndStartAttrs`; F12–F14
  (both F14 tests: the rejected shapes and the accepted HTTP form), F33, F34;
  `TestSpan_SafeForConcurrentUse`.
- Attributes and policy: `TestKeyGrammar_Table`; `TestDenylist_Pinned` (the list is
  stable) and `TestDenylist_Matching_Table` (`email`, `person.email`, `http_request_body`
  denied; `user_agent`, `db.name`, `set_name`, `module` allowed); `TestTokenGrammar_Table`
  (routes with `{id}` and `{path...}`, methods, `_OTHER`, `module.name` accepted; e-mail,
  URL with `?`, space, a leading `.` or `-` rejected); `TestScrub_Table` (e-mails in prose and in `mailto:`; query strings
  in `/path?x=1` and `https://h/p?x`; false positives left alone; idempotent);
  `TestID_ZeroIsIgnored`; F15–F18.
- Meter: `TestCounterHistogramGauge_RoundTrip` (through the Observer);
  `TestMetricAttrs_TokenBoolIntKindAllowed`; F19–F24.
- Refs: `TestRefFrom_FirstFourBytesUpperHex`; `TestTraceIDFrom_MatchesSpan`; F27, F28.
- Close: `TestClose_FlushesPendingSpansAndLogs` (with an in-process receiver, below);
  `TestClose_StdoutStillWorksAfter`; F26, F13.
- intake: `TestAccept_HappyPath_OneRecordPerReport` (every attribute present, `source=client`,
  `person_id` only when set, the address never in `Contains`); `TestAccept_AnonymousSubmission`;
  `TestAccept_CountsAcceptedByCount`; `TestBucket_RefillsWithFakeClock`;
  `TestBucket_IPv6KeyedBySlash64`; `TestAccept_HourlyRateLimitLineHasNoAddress`;
  `TestBatch_JSONIgnoresUnknownFields`; F35–F50.
- telemetrytest: `TestCapture_FailsOnViolationAtCleanup` (a sub-test with a denied key is
  reported failed by a wrapped `testing.TB`); `TestCapture_ContainsSearchesEverything`.
- Lint: passes the new depguard rule (Proposed change 8), `errcheck`, `errorlint`,
  `wrapcheck`, `noctx`, `contextcheck`, `sloglint` (the handler is the one place that
  constructs records), `gosec`, `forbidigo`.

**Integration** (`//go:build integration`; no compose service needed for these):

- `TestExport_ReachesOTLPReceiver`: an `httptest.Server` that accepts
  `POST /v1/traces`, `/v1/metrics`, `/v1/logs` with `Content-Type: application/x-protobuf`
  and counts requests; `New` with its URL as `OTEL_EXPORTER_OTLP_ENDPOINT`; one span, one
  log record, one counter increment; `Close`; each path was hit at least once and the
  bodies are non-empty. Decoding is not needed: the SDK's own tests cover the wire format,
  and the development stack (`make observe`, Grafana on `:3000`) is the manual M0 check
  ("see its traces in Grafana", 09 M0).
- `TestExport_CollectorDown_RequestLatencyUnaffected` (F25): endpoint on a closed port; a
  thousand spans and log records end within a tight budget; the failure counter is
  non-zero; exactly one warn per signal in the buffer; `Close` within its deadline.
- `TestExport_HeadersNeverLogged`: `OTEL_EXPORTER_OTLP_HEADERS=authorization=ZQ9MARKER7`
  with a receiver that returns 500; the marker appears in no log line.

**Conformance:** `moduletest.Conformance` uses `telemetrytest.New(t)` for `Deps.Logger`
(and `Deps.Tracer` once adopted), so `no_personal_data_in_logs` also fails on any policy
violation (Proposed change 1).

## Open questions

None. The draft's five questions (error reference format, public client-error route,
`User-Agent` in client reports, example-deployment retention, default sampler) were
answered by the owner on 2026-10-16 and are recorded in decisions 3, 9, 17, 18 and 6.

## Proposed changes elsewhere

Status as of 2026-10-17: items 1, 2, 3, 8, 10, 11, 13, 14 and 15 are applied; item 9 is
applied for `deploy/compose/.env.example` and Tempo's retention, and Prometheus's retention flag, and pending for
Loki retention, the alert rules and `docs/operating.md`; item 12 is applied; item 6 is
applied for `signals.md` and pending for `jobs.md`; items 4, 5 and 7 wait for their
target to be written and are carried by this list until then.

1. **`plan/design/module-contract.md` (row 4)** (applied 2026-10-16): add `Tracer telemetry.Tracer` to `Deps`
   (scope `module.<name>`, set by `Provision`, checked non-nil by `Build` beside `Logger`
   and `Clock`; F25 there gains it); add the row to "`Deps` fields by spec" (M0); in
   "Not responsible for", replace "tracing is the api shell's middleware" with "the request
   span is the api shell's; a module opens sub-spans through `Deps.Tracer`"; `moduletest.
   Conformance` supplies `Logger` and `Tracer` from `telemetrytest.New(t)`, and
   `no_personal_data_in_logs` also fails on any `Violation` and checks spans. The `hello`
   module opens one span in its handler.
2. **`plan/design/db.md` (row 5), answering its Proposed change 12** (applied 2026-10-16; db's F56 and F57 added): add
   `Tracer telemetry.Tracer` and `Meter telemetry.Meter` to `db.Options` (nil means none;
   `Validate` unchanged); in `Open`, when `Tracer` is set, install a pgx `QueryTracer`
   written inside `db` that opens a `SpanClient` span named `db.<OPERATION>` (first SQL
   keyword, upper-cased, as a `Token`; `db.system=postgresql`; on failure `RecordError`
   and `db.response.status_code` = the SQLSTATE as a `Token`) and never records statement
   text or arguments; add `func (p *Pool) Stats() Stats` with
   `Stats{Acquired, Idle, Total, Max, Pending int; WaitDuration time.Duration}` and,
   when `Meter` is set, register the three gauges in the metrics table from it. `db` imports
   `core/telemetry` (allowed: `telemetry` is below `db` in the core graph).
3. **`plan/design/api.md` (row 16)** (applied 2026-10-17, by api.md's Proposed change 6,
   with these refinements: the span is opened per route after routing, so the name is
   known at `Start`; the route token renders `{$}` as the trailing `/`; the method is
   normalised to the seven known methods or `_OTHER` before it becomes a name, an
   attribute or a label, and the span is named `HTTP <route token>` for `_OTHER` (this
   spec's decision 20 admits `_OTHER`); unmatched requests use the route token
   `/{unmatched}`; `/healthz` and `/readyz` get no span and no sample; every response
   carries `X-Trace-Id`; the error log level is error for a 5xx, warn for a 4xx from a
   signed-in actor and debug for an anonymous 4xx; the intake route is mounted as
   specified): the tracing middleware opens one `SpanServer` span per
   request with `WithParentFrom(r.Header)`, names it `<METHOD> <route pattern>` once the
   route is known (the one non-token span-name form, decision 19), sets `http.request.method`, `http.route`, `http.response.status_code`
   and `person_id` (when a session exists), calls `RecordError` with the mapped error, and
   records `http.server.request.duration` in seconds; the error body carries `ref` from
   `RefFrom` (omitted when `ok` is false) and the api writes one log record per error
   response (error for 5xx, warn for 4xx) with `error_ref`, `trace_id`, `error.kind`,
   `op`, `http.route`, `status`; the panic recovery logs the stack as one attribute
   (`stack`, cut by the handler's cap); request logs never carry query strings, bodies,
   client addresses or `User-Agent` (the one exception is the intake record, which carries
   the agent by decision); mount `POST /api/client-errors` as `AccessPublic`, decode under
   a 64 KiB `http.MaxBytesReader` into `intake.Batch` (unknown fields ignored), call
   `intake.Accept` with the client address it derived, the session's person (zero if none)
   and `User-Agent`; answer 202 with no body, 400 with field errors for `Invalid`, 415 for a
   content type other than `application/json`, and 429 with `Retry-After: 60` and code
   `rate_limited` when `errors.Is(err, intake.ErrRateLimited)`, checked before the kind.
4. **`plan/design/web-shell.md` (row 17)** (applied 2026-10-17 by web-shell.md decisions 6, 9, 13 and 14 and "The reporter, step by step", with these refinements: reported `api` failures are `network`, `malformed_response`, `internal`, `cross_origin`, `misdirected`, `method_not_allowed`, `payload_too_large`, `unsupported_media_type` and an untranslated code; at most 100 distinct pending reports, oldest dropped; the 10 s timer runs only while something is pending; `keepalive` `fetch` is the `sendBeacon` fallback; a network failure backs off 10 s doubling to 5 min; `screen` is the router's pattern with `:p` → `{p}`; the reference is hidden for `unauthenticated`, `read_only` and `network` too): the API client sends a fresh W3C `traceparent`
   per call (random trace ID and parent ID, flags `01`) and keeps it with the call's result;
   it catches uncaught errors, unhandled rejections, error boundaries and failed calls,
   deduplicates by `(kind, message, screen)` with a `count`, batches at most 20 reports
   and sends at most one batch per 10 s (and one on `pagehide` via `sendBeacon` with an
   `application/json` Blob); each report's `screen` and `api.route` are route patterns
   (never the URL), `error_ref` and `trace_id` come from the failed call, `app_version` is
   the bundle version; it never includes form values, URLs with query strings, or the
   response body; it never reports a failure of `/api/client-errors` itself and backs off
   for `Retry-After` on 429; error screens show the `ref` with the wording in "UI" and a
   copy button; nothing is written to browser storage.
5. **`plan/design/app.md` (row 18)** (pending: target not written): call `telemetry.New` right after `config.Load` with
   `Options{Level: cfg.Log.Level, Format: cfg.Log.Format, Instance: cfg.Instance.Name,
   Version: <build version>, Stdout: os.Stdout, Clock: clock.Real()}`; on failure print
   `<VARIABLE>: <reason>` to stderr and exit 2; log the effective configuration once
   (`slog.Any("config", cfg)` is safe); pass `Tracer("db")`/`Meter("db")` in `db.Options`,
   `Tracer("api")`/`Meter("api")` to the api, `Tracer("signals")`, `Tracer("jobs")` and
   meters likewise; build `intake.New` with `Logger().With("component","intake")`,
   `Meter("intake")` and the clock, and hand it to the api; `Provision` sets `Deps.Logger =
   tel.Logger().With("module", m.Name)` and `Deps.Tracer = tel.Tracer("module."+m.Name)`;
   `Close` last at shutdown within `http.shutdown_timeout`; `bms check` prints the log
   level and format and `Exporting()`; `bms serve`'s start-up line includes the version and
   whether export is on.
6. **`plan/design/signals.md` (row 10) and `jobs.md` (row 14)** (applied 2026-10-16 for
   `signals.md`, by its Proposed change 3, with the token names `signal:<name>` (Producer)
   and `deliver:<name>` (Consumer), the counters `bms.signals.published{signal,module}`
   and `bms.signals.delivered{signal,module,outcome}`, and `outcome` ∈ `ok`, `failed`,
   `deferred`, `read_only`, `panicked`, `retried` being `jobs`' to count; pending for
   `jobs.md`: target not written): wrap each publish in a
   `SpanProducer` span `signal <name>` and each delivery in a `SpanConsumer` span
   `signal <name>` with `Module(subscriber)` as a child of the publisher's ctx (after
   commit, so the trace spans the request and its consequences); each job run in a
   `SpanConsumer` span `job <name>`; `RecordError` on failure; counters
   `bms.signals.delivered{signal,module,outcome}` and `bms.jobs.runs{job,outcome}`
   (`outcome` = `ok`, `failed`, `retried`); payloads never on spans (IDs only, by their
   own rule).
7. **`plan/design/audit.md` (row 12)** (pending: target not written): record `telemetry.TraceIDFrom(ctx)` on every entry
   (09 §8), empty when none.
8. **`.golangci.yml`** (applied 2026-10-16): add a depguard rule `otel-seam`: files `$all` except
   `**/internal/core/telemetry/**`, deny `go.opentelemetry.io` ("telemetry wraps the SDK;
   use telemetry.Tracer, Meter and slog"); allow `go.opentelemetry.io` under
   `internal/core/telemetry/**`. forbidigo: deny `otel\.GetTracerProvider`,
   `otel\.Tracer`, `otel\.Meter` everywhere (globals are not used).
9. **`deploy/compose/.env.example`, `deploy/compose/observability/*`, `docs/operating.md`
   (when written)** (`.env.example`, Tempo's `block_retention: 720h` and Prometheus's
   `--storage.tsdb.retention.time=1y` applied 2026-10-16; pending: Loki retention needs a
   Loki config file the compose stack does not yet carry, the alert rules wait for the production example, and
   `docs/operating.md` is not written): add a commented `OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318`
   with a note that only `http/protobuf` is supported and that `OTEL_EXPORTER_OTLP_HEADERS`
   is a credential; set retention in Loki (30 d), Tempo (30 d) and Prometheus (1 y) per
   decision 18 and say so; add D20's alert rules (error rate, client-error spikes, slow
   requests) when the production example is written; the operator docs give the
   maintainer's path from a reference ("UI") and state that Grafana access is for
   maintainers only, that telemetry holds person IDs and expires with retention, and that
   stdout logs are the host's to rotate.
10. **`plan/07-abstractions.md`, `telemetry` entry** (applied 2026-10-16, as layer rule 9; the api shell's "request IDs and tracing" became "the request span and error reference"): replace "Owns" with: the logger
    (JSON or text on stdout, trace IDs on every record), `Tracer` and `Meter` interfaces
    over the OpenTelemetry SDK with OTLP/HTTP export when `OTEL_*` names a collector, the
    privacy policy (typed attributes, key denylist, `Scrub`, caps, counted violations), the
    error reference (`XXXX-XXXX` from the trace ID), `TraceIDFrom` for audit, the client
    error intake service (`intake`), and `telemetrytest`. "Doesn't do": the request
    middleware and the intake route (api), the browser side (web shell), statement spans and
    pool gauges (db), dashboards, alerts and retention (deployment), audit, analytics about
    people. Layer rules: "`core/telemetry` is at the bottom of the core graph: it imports
    `lib` only (and the OpenTelemetry SDK, which no other package imports); every core
    package, `api`, `app` and every module may import it." Drop "request and trace IDs" in
    favour of "trace IDs" (decision 2).
11. **`.claude/skills/go-conventions/SKILL.md`, "Logging and observability"** (applied 2026-10-16): add: use the
    `*Context` log methods wherever a ctx exists, so records carry the trace ID
    (`sloglint context: scope` enforces it); attribute keys follow the key grammar and
    never a denied key (list in telemetry.md); spans come from `telemetry.Tracer` with
    typed `Attr`s, `Token` for names and routes, `ID` for records, `Text` only for error
    messages; never import `go.opentelemetry.io` outside `core/telemetry`; tests that
    check leaks use `telemetrytest.New(t)` and `Contains`.
12. **`plan/03-decisions.md`, D20 (for `plan-keeper`)** (applied 2026-10-16): add: "bms exports OTLP over
    HTTP/protobuf only; the error reference is the first four bytes of the trace ID
    (`XXXX-XXXX`); traces are sampled 100% by default; the server ignores the browser's
    sampled flag (telemetry.md decisions 3, 5, 6; confirmed by the owner, 2026-10-16)".
13. **`plan/09-core-features.md` §1 "Done when"** (applied 2026-10-16): add "a collector that is down or absent
    never fails start-up, a request or shutdown; stdout logs carry the trace ID without
    any collector".
14. **`plan/design/lib-core.md`, consumer rows** (applied 2026-10-16): `clock.Since` and `Backoff.Attempt` name
    "`telemetry` (health)"; there is no health endpoint in this spec (`/healthz` and
    `/readyz` are `api`'s). Replace with `api` (request duration) and `jobs` respectively;
    `telemetry` uses `clock.Clock` for its own rate limits and `intake` for the buckets.
15. **`plan/design/config.md` (row 3), "Variables the process reads that are not in the
    catalogue"** (applied 2026-10-16): note that `OTEL_EXPORTER_OTLP_PROTOCOL` must be `http/protobuf` and that
    `telemetry` validates the `OTEL_*` variables it reads (this spec's table) at start.
