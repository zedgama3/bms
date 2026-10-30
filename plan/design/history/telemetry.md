# telemetry: amendment history

Amendment narratives, reasons, review findings and superseded wording moved verbatim out of `plan/design/telemetry.md` on 2026-10-30, when the amendments were folded into the spec body (only line breaks at sentence ends were added; no word changed).

## Amendment notes from the spec header

- **Amendment T1 (Implemented 2026-10-30; Approved 2026-10-28, D54, with the recommended answers to Q-TEL-1 to Q-TEL-3):** client telemetry for a public demo: chunk-load reports and `duration_ms` on `/api/client-errors`; a new `POST /api/client-events` usage and performance channel (`intake.Usage`, metrics only, no person or visit ID); allowlists from the web client manifest and the api route table; config `telemetry.usage`;
  dashboards and alert rules as deploy assets; `docs/telemetry.md`. Section "Amendment T1" before Open questions; Proposed changes 18 to 26; questions Q-TEL-1 to Q-TEL-3.
- **Amended 2026-10-17 (owner):** decisions 21 to 25 (intake `api.code` is any token, client reports carry `origin=client`, `IsToken` and `Collect` and the accepted implementation deviations, `client.stack` cap, debug logging keeps scrubbing on); the Data section now says `New` reads the OTLP certificate and key files.
- **Amended 2026-10-18 (owner approval of audit/identity):** `intake.ErrRateLimited` is, or wraps, the shared `httpx.ErrRateLimited` (api.md, identity.md Proposed change 1); instrument rows for `bms.audit.*` and `bms.identity.*` added (audit.md Proposed change 8, identity.md Proposed change 15); `TraceIDFrom` is recorded on every audit entry (Proposed change 7 applied).
- **Amended 2026-10-18 (D37):** instrument row `bms.events.setup{module,outcome}` added; spans `departments.<use-case>`, `events.<use-case>` and `events.setup` are token names (departments-events.md Proposed change 9).
- **Amended 2026-10-18 (owner approval of authz/people):** instrument rows for `bms.authz.decisions` and `bms.people.*` added (authz.md Proposed change 15, people.md Proposed change 13).
- **Amended 2026-10-20 (D44, owner approval of files):** instrument rows `bms.files.uploads{outcome}`, `bms.files.reads{outcome}`, `bms.files.swept{what}`, `bms.files.tombstones` (gauge), `bms.files.verify{outcome}` and `bms.objects.failures{op}`; span names `files.stage|attach|open|delete|reap|sweep|verify` and `objects.put|get|head|delete|list|ping` (Client, with `objects.kind`). Proposed change 17.
- **Amended 2026-10-23 (D50, jobs Amendment J1):** no new outcome token; the `jobs: a job ran` log line gains the boolean attribute `next` (allowed by the key grammar; no denylist change).
- **Amended 2026-10-20 (D43, owner approval of jobs):** instrument rows `bms.jobs.runs{job, module, outcome}` (nine outcomes), `bms.jobs.queue{status}` and `bms.jobs.lag`; span names `jobs.tick`, `jobs.enqueue`, `jobs.defer`, `jobs.run`; Proposed change 6 applied for jobs.
- **Amended 2026-10-19 (D39, owner approval of notify):** instrument rows for `bms.notify.enqueued`, `bms.notify.delivered`, `bms.notify.outbox` and `bms.notify.purged` added, and span names `notify.send`, `notify.tick`, `notify.deliver`, `notify.<use-case>` listed (notify.md Proposed change 9).

## Summary paragraph for Amendment T1

**Amendment T1 (Approved 2026-10-28, D54; folded into the sections above by Proposed change 20).** Adds, without changing a decision above: `intake.KindChunk` and `APICall.DurationMs` on the error route;
`POST /api/client-events` over `intake.NewUsage(UsageOptions{Logger, Meter, Clock, Enabled, Screens, Actions})` and `(*Usage).Accept(ctx, EventSubmission{From, UserAgent, Batch})` with `Event{Kind (screen_view|action|screen_ready|vital), Screen, From, Action, Name, Value, OccurredAt, Count, AppVersion}`, metrics only (`bms.client.screen_views{screen,module,from}`, `bms.client.actions`,
`bms.client.screen_ready|lcp|inp|cls` histograms with `browser`, `form_factor`; `bms.client_events.accepted|rejected|folded`), never a person, visit or record ID; `Intake.Options.Screens|Routes` and `bms.client_errors.accepted{kind,screen}`; allowlists from `web.ClientManifest()` (build-emitted `dist/client-manifest.json`) and `api.RouteTokens`, with `_other` and `/{unmatched}` folding; config `BMS_TELEMETRY_USAGE` (default `true`;
off answers 501); GPC and DNT honoured in the browser; dashboards and alert rules under `deploy/observability/`; `docs/telemetry.md`.
Errors added: `Usage.Accept`: `Unsupported` (off), `Invalid` (`events[i].<field>`), `Unavailable` wrapping `ErrRateLimited`; `web.ClientManifest`: `NotFound`, `Invalid`. F51 to F75.
Details in the "Amendment T1" section, whose decisions are 26 to 33 below; Q-TEL-1 to Q-TEL-3 are answered (owner, 2026-10-28) as recommended: config `BMS_TELEMETRY_USAGE` default `true` (decision 32), `from` kept (decision 27), `browser` and `form_factor` on the four histograms (decision 33).

## Amendment T1 (Approved 2026-10-28, D54): client telemetry for a public demo

**Context (owner, 2026-10-28).** The project is putting up a fully public demo so the
community can try it. "It is also important that we get telemetry that can help us identify
issues that clients are having, even if they don't report them", and to learn how people use
things. Today the web shell reports only uncaught errors, unhandled rejections, error-boundary
catches and a short list of API failures to `POST /api/client-errors` (decision 9, web-shell
decision 14). Telemetry ships to an OTLP collector (Grafana, Loki, Tempo, Mimir or Prometheus
on a separate host).

This amendment is written against the implemented spec above. Nothing in it changes a
decision already taken; it adds. Section numbers below (T1.1 to T1.9) are referenced by the
new failure modes (F51 to F75), the new Proposed changes (items 18 to 26) and the owner
questions (Q-TEL-1 to Q-TEL-3). Approval folds the Go signatures into "Interface", the
instruments into the metrics table and the rest into "Behavior".

### T1.1 Two channels, two sinks, two privacy postures

Browser telemetry splits into two routes because the two kinds of data have different sinks
and different privacy needs:

| | `POST /api/client-errors` (exists; extended) | `POST /api/client-events` (new) |
|---|---|---|
| What | things that broke: crashes, rejections, screen crashes, failed API calls, **chunk-load failures** (new) | how the shell is used and how fast it is: screen views, declared actions, screen-ready time, Web Vitals |
| Becomes | one **log record** per report at `warn`, `origin=client` (Loki), as today | **metrics only** (counters and histograms); no log record, no span |
| May carry | free text (scrubbed, capped), a stack, `person_id` when signed in, `user_agent`, the failed call's `trace_id` | tokens and numbers only; **never** `person_id`, a visit ID, text, a URL or a trace ID |
| Off switch | none (it is how the service is run) | `telemetry.usage` (T1.6); GPC and DNT in the browser |
| Limits | 20 reports per batch, 30 per address per minute, 600 per process per minute (unchanged) | 50 events per batch, 60 per address per minute, 2 000 per process per minute |

**Decision T1-a: a second route rather than a wider `/api/client-errors`.** Error reports are
log records that may name a person (their ID) and carry free text; usage events are metric
samples that must never carry either. Putting both in one body would make the "events hold
nothing about a person" statement depend on the handler reading the right fields, where two
routes make it depend on the type: `usage.Accept` has no `Person` field to forget, and the
metric policy (F19) refuses an ID or text structurally. It also lets a regional turn usage
counting off without touching error reporting, and gives the two different limits.

**Decision T1-b: usage events become metrics, never log records; there is no visit or tab
ID.** The owner asked whether a random per-tab ID should correlate events. No: an ID is what
turns counts into a profile, and the only sink that refuses an ID by construction is the
meter. What "how people use things" needs is answered without one: views per screen, actions
per screen, and transitions (each `screen_view` carries `from`, the previous screen template
in the same page load, or `_entry`), which give the flow between screens as counts rather
than as journeys of one person. A navigation sequence of one visit, time on screen and
return visits are explicitly **not collected**; a regional that wants them is asking for a
different product.

**Decision T1-c: prefer the server's count when the server sees the action.** Every API call
is already counted by route and status (`http.server.request.duration`); this amendment adds
the problem code to that picture on the server (`bms.api.errors`, Proposed change 19) rather
than asking the browser to report every 4xx. A module's business action (a shift sign-up, a
report filed) is counted in its service with its own instrument, as `people`, `jobs` and
`files` already do. The browser's `action` events exist only for what the server cannot see:
opening a dialog, using a filter or a tab, starting and abandoning a form, choosing a sort.
A screen author declares those names in the `ScreenSet` (T1.3); they are not free.

**Decision T1-d: no OpenTelemetry JavaScript SDK in the browser.** The web SDK is larger than
the shell's whole entry budget (web-shell "bundle budget": 200 KB gzip JS), and D20's goal,
one trace per click, is already met: the shell mints `traceparent` per call (web-shell
decision 6), the server continues it (decision 6 here), and every error report carries the
failed call's `trace_id` and `error_ref`, so Loki's `client.trace_id` opens the server trace
in Tempo. Kept unchanged. The one addition is `duration_ms` on an `api` report (T1.2), so a
maintainer can tell the 35 s client timeout from an instant connection reset.

### T1.2 `POST /api/client-errors`: what is added

Wire form additions to `intake.Report` (unknown fields stay ignored, so an old server accepts
a new shell):

```go
// Kind gains one value.
const KindChunk Kind = "chunk" // a lazy chunk failed to load (a deploy replaced the bundle, or the network dropped)

// APICall gains the call's wall time, as the shell measured it.
type APICall struct {
	Method     string `json:"method"`
	Route      string `json:"route"`
	Status     int    `json:"status"`
	Code       string `json:"code"`
	DurationMs int    `json:"duration_ms,omitempty"` // 0 unknown; else 1..MaxDurationMs (F69)
}

const MaxDurationMs = 600_000 // the shell's longest timeout (UPLOAD_TIMEOUT_MS)
```

- A `chunk` report's `message` is `chunk load failed <asset path>` where the asset path is the
  hashed bundle file (`assets/volunteers-3f9a1c.js`); `screen` is the screen that needed it;
  `api` must be absent (F69). Why a kind of its own: it is the one browser failure whose cause
  is the deployment, not the code (an open tab after a new build asks for a file that no longer
  exists), it needs its own alert (T1.8) and its own UI answer (the crashed screen offers
  "reload" with wording that says the site was updated; Proposed change 18).
- `client.api.route` is folded through the api's route table (T1.5): a value that is not a
  mounted pattern becomes `/{unmatched}` on the log record and on the metric, the same token
  api's own span uses (api decision 10).
- `bms.client_errors.accepted` gains the attribute `screen` (folded through the screen
  allowlist, T1.5; `_other` when unknown, `""` when the report had none), so "client error
  rate by screen" is a Prometheus query instead of a Loki aggregation. The log record keeps the
  report's own (ID-replaced) `screen`.
- The reportable set (web-shell decision 14) does not widen. A `permission`, `invalid`,
  `not_found` or `conflict` the server answered is already on the server with the same route,
  code and trace; reporting it again doubles traffic and tells the maintainer nothing new.

### T1.3 `POST /api/client-events`: the usage and performance channel

```go
// EventKind is what the web shell measured.
type EventKind string

const (
	EventScreenView  EventKind = "screen_view"  // the router settled on a screen
	EventAction      EventKind = "action"       // a declared action on a screen (T1-c)
	EventScreenReady EventKind = "screen_ready" // navigation to first settled render, in ms
	EventVital       EventKind = "vital"        // a Web Vital's final value for the page load
)

// Event is one measurement. JSON field names are the wire form; unknown fields are ignored.
// Nothing in it may identify a person, a visit or a record: no IDs, no text, no URL.
type Event struct {
	Kind       EventKind `json:"kind"`
	Screen     string    `json:"screen"`                // the route pattern, "/volunteers/shifts/{id}"; required
	From       string    `json:"from,omitempty"`        // screen_view only: the previous pattern in this page load, or "_entry"
	Action     string    `json:"action,omitempty"`      // action only: a declared name, "shift.filter"
	Name       string    `json:"name,omitempty"`        // vital only: "lcp", "inp" or "cls"
	Value      float64   `json:"value,omitempty"`       // screen_ready and lcp, inp: milliseconds; cls: the score
	OccurredAt time.Time `json:"occurred_at"`           // RFC 3339; within OccurredWithin of now
	Count      int       `json:"count,omitempty"`       // screen_view and action: repeats merged by the shell; 0 means 1
	AppVersion string    `json:"app_version,omitempty"` // a Token of at most 64 bytes
}

// EventBatch is the request body of POST /api/client-events.
type EventBatch struct {
	Events []Event `json:"events"`
}

// Validate fails with errs.Invalid joining a validate.FieldError per problem, fields
// "events" and "events[i].<field>" (F54–F62).
func (b EventBatch) Validate() error

// EventSubmission is what api hands Usage.Accept. There is no Person field on purpose
// (decision T1-a): the handler never reads the session for this route.
type EventSubmission struct {
	From      netip.Addr // for the limit; never logged, never on a metric
	UserAgent string     // reduced to two coarse tokens (browser, form_factor); never recorded as text
	Batch     EventBatch
}

// UsageOptions is what NewUsage needs.
type UsageOptions struct {
	Logger  *slog.Logger    // one warn line per hour when rate-limited; nothing per event
	Meter   telemetry.Meter // the bms.client.* instruments and bms.client_events.*
	Clock   clock.Clock
	Enabled bool            // config telemetry.usage (T1.6); false: Accept fails Unsupported and counts nothing
	Screens []string        // the screen allowlist from web.ClientManifest (T1.5); empty: fold cap only
	Actions []string        // the action allowlist from the same manifest; empty: fold cap only
}

// Limits and caps. Constants, not settings (the same reasoning as the error intake's).
const (
	MaxEvents                = 50    // events per batch
	EventsPerAddressPerMinute = 60   // events (not batches) per client address
	EventsGlobalPerMinute    = 2_000 // events per process
	MaxVitalMs               = 120_000 // lcp, inp and screen_ready values above this are a bug, not a measurement
	MaxCLS                   = 100
	MaxDistinctScreens       = 300 // fold cap when Screens is empty (a development server)
	MaxDistinctActions       = 500 // fold cap when Actions is empty
	OtherToken               = "_other"
	EntryToken               = "_entry"
)

// Usage is the service. Safe for concurrent use.
type Usage struct{ /* unexported: the buckets (shared implementation with Intake), allowlists, fold sets, instruments */ }

// NewUsage fails with errs.Invalid if Logger, Meter or Clock is nil, or if an allowlist entry
// is not a token (F51). It registers the instruments in T1.4 (F20, F21 apply).
func NewUsage(opts UsageOptions) (*Usage, error)

// Accept takes one batch: fails errs.Unsupported when usage counting is off (F52), checks ctx
// (F53), charges the batch's event count against the global bucket then the address's bucket
// (F63, F64), validates the batch (F54–F62), then for each event replaces ID-shaped segments
// in Screen and From with "{id}", folds Screen, From and Action through the allowlists (F65,
// F66), reduces UserAgent to browser and form_factor tokens, and records the event's
// instrument (T1.4). It writes no log record per event and returns nil once every event is
// recorded. Consumer: api's handler for POST /api/client-events.
func (u *Usage) Accept(ctx context.Context, s EventSubmission) error
```

The web shell's side (Proposed change 18) in one paragraph, so the implementer of the server
and the reviewer of the shell read the same contract: a collector installed after the
reporter, never throwing; it does not install at all when `navigator.globalPrivacyControl`
is `true` or `navigator.doNotTrack` is `"1"` (decision T1-f); it records a `screen_view`
when the router settles on a new match (the final screen of a redirect chain, not the
intermediate), with `from` the previous settled pattern in this page load or `_entry`;
`screen_ready` as the milliseconds from the navigation (or `navigationStart` for the first
screen) to the first moment the navigation is idle and no `ui/Loading` is mounted;
`vital` through the `web-vitals` library (Apache-2.0, about 2 KB gzip, no network; its
`onLCP`, `onINP`, `onCLS` final values, attributed to the screen current when the value
became final); an `action` when a screen calls `trackAction(name)` from `@/usage` with a name
the screen's `ScreenSet.actions` declared (an undeclared name is dropped with a development
warning). It merges identical `(kind, screen, from, action)` events into `count`, batches at
most 50, flushes every 10 s while something is pending and on `pagehide` via `sendBeacon`
(`keepalive` fetch as the fallback), backs off for `Retry-After` on 429 and on a network
failure like the reporter, and on a 404 or 501 stops for the rest of the page load (the
regional turned it off). Nothing is written to browser storage; failures of the route itself
are never reported; the collector never reads the URL's query string.

**Decision T1-e: the first path segment is not sent; the server derives `module` from the
manifest.** The manifest (T1.5) maps each screen pattern to the `ScreenSet` that owns it, so
the metric attribute `module` (the set's name: `volunteers`, `people`, `shell`) is the
server's, not the client's, and a client cannot file a screen under another module.

**Decision T1-f: Global Privacy Control and Do Not Track turn usage events off in the
browser; error reports are unaffected.** GPC's legal meaning (do not sell or share) does not
reach first-party anonymous counts, but a participant who set it expects not to be counted,
and honouring it costs one `if`. Error reports continue: they are needed to run the service,
hold no stable identifier of their own (the `person_id` on a signed-in report is the same
account the server already logs the request under), and are what the error reference on the
screen already implies.

### T1.4 Instruments added

| Instrument | Kind, unit | Attributes | Recorded by |
|---|---|---|---|
| `bms.client.screen_views` | counter, `{view}` | `screen`, `module`, `from` (a screen pattern, `_entry` or `_other`) | `intake.Usage` |
| `bms.client.actions` | counter, `{action}` | `action`, `screen`, `module` | `intake.Usage` |
| `bms.client.screen_ready` | histogram, `s` (buckets 0.1 … 30) | `screen`, `module`, `browser`, `form_factor` | `intake.Usage` |
| `bms.client.lcp` | histogram, `s` (buckets 0.5, 1, 1.5, 2, 2.5, 3, 4, 6, 8, 12) | `screen`, `module`, `browser`, `form_factor` | `intake.Usage` |
| `bms.client.inp` | histogram, `s` (buckets 0.05, 0.1, 0.2, 0.3, 0.5, 0.75, 1, 2) | same | `intake.Usage` |
| `bms.client.cls` | histogram, `1` (buckets 0.05, 0.1, 0.25, 0.5, 1) | same | `intake.Usage` |
| `bms.client_events.accepted` | counter, `{event}` | `kind` | `intake.Usage` |
| `bms.client_events.rejected` | counter, `{event}` | `reason` = `rate_limited`, `invalid`, `disabled` | `intake.Usage` |
| `bms.client_events.folded` | counter, `{value}` | `field` = `screen`, `from`, `action` | `intake.Usage` (and `Intake`, for `screen` and `api.route` on error reports) |
| `bms.client_errors.accepted` | counter, `{report}` | `kind`, **`screen`** (added; folded) | `intake` |
| `bms.api.errors` | counter, `{response}` | `http.request.method`, `http.route`, `code` (the problem body's code: a kind name or a shell code, a bounded set) | `api` (Proposed change 19) |

`browser` is one of `chromium`, `firefox`, `webkit`, `other`; `form_factor` is `phone` or
`desktop` (from the `Mobile` token in the User-Agent, the only classification that is cheap
and needed: phone performance is a hard requirement, web-shell decision 21). The classifier is
a package constant pinned by a table test; the User-Agent text itself is never recorded on
this channel. The buckets are explicit because the OpenTelemetry defaults for seconds stop at
10 s and are too coarse under 1 s; the Web Vitals "good" and "poor" thresholds (2.5 s and
4 s for LCP, 200 and 500 ms for INP, 0.1 and 0.25 for CLS) are bucket boundaries so p75
against them is exact.

Cardinality is bounded by construction: `screen`, `from` and `action` take values from the
manifest or `_other`; `module` from the set names; `browser` and `form_factor` from fixed
lists; `kind`, `reason`, `field` are closed sets. With 60 screens and 20 actions the worst case
is 60 × 61 series for `screen_views` and 60 × 8 per histogram, well inside what one Prometheus
or Mimir tenant handles; in practice transitions that never happen are never created.

### T1.5 Allowlists: the client manifest and the route table

Bounded cardinality needs a source of truth for the values a client may send. Two already
exist; this amendment makes them reachable:

- **Screens and actions**: the web build emits `dist/client-manifest.json`,
  `{"screens":[{"pattern":"/volunteers/shifts/{id}","set":"volunteers"}, …],
  "actions":[{"name":"shift.filter","set":"volunteers"}, …]}`, produced from the same
  `buildRoutes(sets)` the router uses (the shell's own screens are set `shell`), with a vitest
  test that the emitted list equals the router's patterns, so the two cannot drift. The Go
  package `web` gains `ClientManifest() (Manifest, error)` with
  `Manifest{Screens []ScreenEntry; Actions []ActionEntry}` (`NotFound` when the file is
  absent, `Invalid` when it is malformed or an entry is not a token; F72). `app` hands the
  patterns to `intake.New` and `intake.NewUsage` (Proposed change 21); when the manifest is
  missing (a checkout without a build, the Vite dev server), the allowlists are empty, the
  fold caps apply, `app` logs one warning and `bms check` reports it (F72).
- **API routes**: api exports `RouteTokens(routes []module.ModuleRoutes) []string`, the route
  tokens its own spans use (`{$}` rendered as `/`), and `app` passes them to `intake.New` as
  `Options.Routes` (Proposed change 19). An `api.route` outside the list becomes
  `/{unmatched}` on the log record and on the metric (F70).

Folding rule, shared by both services: after `Scrub` and ID replacement, a value in the list
passes; otherwise, when the list is non-empty, it becomes `_other` and
`bms.client_events.folded{field}` increments (F65); when the list is empty, the first
`MaxDistinct*` distinct values are admitted for the life of the process and the rest become
`_other` (F66). The log record of an error report keeps the unfolded (ID-replaced) value,
because Loki attributes are not series.

### T1.6 The off switch: configuration, default on

**Decision T1-g (decision 32; Q-TEL-1 answered by the owner, 2026-10-28): `telemetry.usage` is a
configuration key, `BMS_TELEMETRY_USAGE`, boolean, default `true`.** Configuration rather
than a setting, for three reasons. It is a property of the deployment like the collector
itself: with no `OTEL_EXPORTER_OTLP_ENDPOINT` the counts go nowhere, and the operator who
sets one is the person who decides what to send to it. `telemetry` sits below `settings` in
the core graph (settings imports telemetry, Summary of settings.md) and can read a setting
only through a seam `app` would have to build. And turning counting on or off is something a
regional should do deliberately and tell its participants about, which a start-up value
printed by `bms check` and the start-up line fits better than a toggle any settings admin
flips at runtime. The cost is a restart to change it. Default `true`, because the events hold
nothing about a person or a visit by construction (T1-b, T1.4), so they are not personal
data under the project's own levels (privacy-data: every level is about information about a
person), and a default of `false` would leave every regional that does not read the docs
blind to exactly the problems the owner wants found. A regional that wants it off sets one
variable and the shell stops sending within one flush. With `false`: the route stays mounted
(the OpenAPI document does not depend on configuration), `Usage.Accept` fails `Unsupported`
(501) and counts `rejected{reason="disabled"}`; the shell stops for the page load.

Config catalogue row (Proposed change 22, applied to config.md 2026-10-28 as F39): `BMS_TELEMETRY_USAGE`, default `true`, `true` or
`false` (config's strict boolean grammar), doc "count screen views, declared actions and Web
Vitals by screen template; never by person".

### T1.7 Privacy statement and what a regional tells its participants

What the two routes may carry, by field, with the sensitivity each would have if bms stored it
(privacy-data), extending the Data table:

| Field | Route | Level | Why it is needed | Retention |
|---|---|---|---|---|
| `screen`, `from`, `action`, `module`, `kind`, `name` | events | not about a person (closed sets from the build) | which screens are used, how, and from where | metrics: the collector (D18: 1 year) |
| `value`, `count` | events | not about a person | how fast, how often | same |
| `browser`, `form_factor` | events | not about a person (four and two values) | whether slowness is a phone problem | same |
| `duration_ms` on an `api` report | errors | not about a person | a timeout from a reset | logs: 30 days |
| the client address | both | personal | the rate limit | in memory, never logged, never exported (unchanged) |

Never on the events route, enforced by the type (no field exists for it) and by the metric
policy (F19): a person ID, a session or visit ID, a name, an e-mail address, a URL, a query
string, free text, a record ID (ID-shaped path segments become `{id}` before anything is
counted), a trace ID. The error route is unchanged in what it may carry (Data table above).

Proposed change 24 adds `docs/telemetry.md`: a page a regional can hand to its participants
or paste into its privacy notice, listing both routes field by field in plain words, the
retention of each sink, that counts are by screen and never by person, that GPC and DNT are
honoured, how the regional turns usage counting off, and that error reports from a signed-in
person carry the account's ID so a maintainer can help with the error the person saw. The
same page is what the demo's "about" text links to.

### T1.8 What the operator sees: dashboards and alert rules as deploy assets

Dashboards and rules are files under `deploy/observability/`, shared by the compose stack
(`make observe`) and the production example, never Go code (the "Not responsible for" row
above stands). Grafana provisions the dashboards from the directory; the rules are in
Prometheus rule format, which Prometheus, Mimir's ruler and Alloy all load. Metric names appear
as Prometheus renders them (`bms_client_screen_views_total`,
`http_server_request_duration_seconds_bucket`, `bms_client_lcp_seconds_bucket`).

Dashboards (`deploy/observability/grafana/dashboards/`), each with a `module` or `screen`
variable where the panel has that attribute:

| File | Panels |
|---|---|
| `bms-overview.json` | request rate, 5xx rate, p95 latency (`http.server.request.duration`); client errors per minute by kind; jobs failed in the last hour; notify outbox by status; export failures; a link row to the others |
| `bms-api.json` | requests and errors by route (`bms.api.errors` by `code`), p50/p95/p99 by route, status mix, 429 and 503 breakdown (`rate_limited`, `read_only`, `shutting_down`), rate-limited intake batches |
| `bms-client.json` | client error rate by screen and kind (`bms.client_errors.accepted`), failed API calls as seen by the browser (Loki: `origin="client"` and `client.kind="api"` by `client.api.route`, `client.api.code`), chunk-load failures by `app_version`, top crash messages (Loki `topk`), Web Vitals p75 by screen with the good/poor thresholds drawn (`bms.client.lcp|inp|cls`), screen-ready p75 by screen and form factor |
| `bms-usage.json` | screen views by screen and module, transitions (`from` → `screen`, a table), actions by screen, views by form factor, events accepted, rejected and folded (a non-zero `folded` means the manifest and the shell disagree) |
| `bms-database.json` | pool connections used and idle against max, pending requests, statement p95 by operation from Tempo's span metrics (`span_name=~"db\\..*"`), slowest statements (Tempo search by `db.*` spans over 500 ms) |
| `bms-jobs-notify.json` | `bms.jobs.runs` by job and outcome, queue depth by status, lag p95, `bms.notify.delivered` by outcome, outbox, purged |

Alert rules (`deploy/observability/alerts/bms.rules.yaml`, one group `bms`; thresholds are
the shipped example values, each a commented constant for the operator to tune):

| Rule | Expression (sketch) | For | Severity |
|---|---|---|---|
| `BmsServerErrorRate` | 5xx responses over all responses > 1 % | 10 m | page |
| `BmsSlowRequests` | p95 `http.server.request.duration` > 2 s | 10 m | warn |
| `BmsClientErrorSpike` | `bms_client_errors_accepted_total` rate > 10/min, or > 5× its own 1 h average | 10 m | warn |
| `BmsChunkLoadFailures` | any `kind="chunk"` accepted in 15 m | 0 | warn (a deploy left stale tabs or a missing asset) |
| `BmsWebVitalsPoor` | LCP p75 > 4 s or INP p75 > 0.5 s over 1 h, any screen with at least 20 samples | 1 h | info |
| `BmsAPIErrorsByRoute` | `bms_api_errors_total{code=~"internal\|unavailable\|timeout"}` rate > 1/min for one route | 15 m | warn |
| `BmsJobFailures` | `bms_jobs_runs_total{outcome=~"failed\|panicked\|lost_lease\|no_handler"}` increase > 0 | 15 m | warn |
| `BmsNotifyDeliveryFailing` | `bms_notify_delivered_total{outcome=~"failed\|rejected\|no_smtp"}` increase > 0 | 30 m | warn |
| `BmsDBPoolSaturated` | `db_client_connection_pending_requests` > 0 | 5 m | warn |
| `BmsSlowQueries` | p95 of Tempo span metrics for `db.*` spans > 0.5 s | 15 m | warn |
| `BmsTelemetryExportFailing` | `bms_telemetry_export_failures_total` increase > 0 | 15 m | info |
| `BmsIntakeRateLimited` | `bms_client_errors_rejected_total{reason="rate_limited"}` or the events equivalent increase > 100 | 15 m | info (abuse, or a page in a loop) |
| `BmsPolicyViolations` | `bms_telemetry_policy_violations_total` increase > 0 | 0 | page (a privacy bug reached production) |
| `BmsNoTraffic` | no `http.server.request.duration` samples | 30 m | page (the demo is down or unreachable) |

The compose stack mounts `deploy/observability/grafana/dashboards/` and the provisioning file,
and loads the rules into Prometheus (Proposed change 23). The production example and
`docs/operating.md` (telemetry Proposed change 9, still pending) point at the same files.
Grafana's `json` dashboards are generated once by hand in Grafana and exported with
`__inputs` resolved to the provisioned datasource UIDs (`prometheus`, `loki`, `tempo` as in
`datasources.yaml`); they are reviewed as files like any other deploy asset.

### T1.9 Failure modes added

F51 to F75 are rows of the "Failure modes" table above (folded 2026-10-28, Proposed change 20); their conditions refer to T1.2 to T1.5.

Members that cannot fail, and why: the User-Agent classifier (pure; every input maps to a
token); the fold (pure over a set); `EventKind` constants.

**Test plan additions.** Folded into "Test plan" above (Proposed change 20).

**Lock-in check (unchanged conclusion).** The browser side depends on `web-vitals`
(Apache-2.0) and standard browser APIs; the server side on nothing new. Dashboards are Grafana
JSON and the rules Prometheus format, both readable by their OSI-licensed implementations
(Grafana AGPL-3.0, Prometheus and Mimir Apache-2.0); a regional on another backend
re-creates six dashboards from the instrument table, which is the contract, not the JSON.


## Accepted implementation deviations (build, 2026-10-29)

- `UsageOptions` gains `Modules map[string]string` (screen pattern to owning set), filled by `app` from the client manifest. A screen it does not list, or any screen when it is nil, gets the first segment of its pattern as `module` (`shell` for `/`).
- `POST /api/client-events` is a sessionless route: the `Authenticator` is not called, so no person can reach a span or log.
- Separate meter scopes: `intake` for client errors, `usage` for client events.
- `bms check` prints `client manifest: N screens, M actions` (or `missing`) then the usage line; a missing manifest is `ExitFailure` only when the build has a UI.
- `RouteTokens` excludes `/healthz` and `/readyz`.
- The client error log attribute for the API call's duration is `client.api.duration_ms`.

## Open questions answered by Amendment T1

**Amendment T1 (2026-10-28), answered by the owner on 2026-10-28, all as recommended** and folded into decisions 26 to 33:

- **Q-TEL-1** (where the usage off switch lives and its default): configuration, `BMS_TELEMETRY_USAGE`, default `true` (decision 32, T1.6).
- **Q-TEL-2** (the `from` attribute on screen views): keep it (decision 27, T1-b).
- **Q-TEL-3** (`browser` and `form_factor` on performance samples): send the User-Agent as today and let the server reduce it to four browser classes and two form factors, on the `screen_ready`, `lcp`, `inp` and `cls` histograms only (decision 33, T1.4).
