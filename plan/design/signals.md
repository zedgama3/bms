# signals: in-process domain events

- **Status:** Implemented (2026-10-17); Approved (2026-10-16)
- Amended 2026-10-17 (owner): records accepted, reviewer-approved implementation deviations: published counted after commit, Close waits again, Deferrer gets a fresh deadline, span attrs, `!repeated` key, Fake stamps `clock.Real()`, late deliveries dropped, Redeliver after Close (see Summary, "Accepted deviations").
- **Amended 2026-10-18 (owner approval of audit/identity):** Proposed change 7 applied for identity (`PersonFrom` is false inside a delivery; the session value is request-scoped; authz still pending); `Env.CoreSignals` gains `identity.account-created` (payload `account`, `person`) (identity.md Proposed change 12).
- **Amended 2026-10-18 (owner approval of authz/people):** Proposed change 7 applied for authz (inside `InDelivery` every decision answers `Unauthenticated`, so a handler runs as the system and uses its spec's system path; authz decision 7); Proposed change 8 applied for people; `Env.CoreSignals` gains `access.grant-created` and `access.grant-revoked` (`grant`, `person`, `role`, `department?`) and `people.person-created {person}`, `people.person-deleted {person}`, `people.deletion-requested {request, person}`; the core's scope `people` subscribes to `identity.account-created` (authz.md Proposed change 8, people.md Proposed change 6). `departments-events` is still pending.
- **Package:** `internal/core/signals` (Go module `github.com/zedgama3/bms`), plus the test
  helper package `internal/core/signals/signalstest`. **UI:** none of its own; the admin
  "modules" screen (`web-shell.md`) shows each module's published and subscribed names from
  `module.Graph`, titled through the message keys of module-contract decision 8.
- **Depends on specs:** `lib-errs.md` (Implemented): `errs.E`, `errs.Is`, `errs.KindOf`,
  kinds `Invalid`, `Conflict`, `NotFound`, `Unavailable`, `Timeout`, `Canceled`, `Internal`.
  `lib-core.md` (Implemented): `clock.Clock` (`Now`, `Since`), `validate.Errors`,
  `validate.FieldError`, `validate.Range`. `model.md` (Approved): `model.ID[E]`, `NewID`,
  `ID.Validate`, `ID.String`, `ID.MarshalText` (payloads are typed IDs and nothing else).
  `db.md` (Approved): `db.AfterCommit` (delivery runs from the hook, so nothing is delivered
  on rollback), `db.ErrReadOnly` (a handler's write on a read-only copy), `dbtest.Fake`
  (its `Transact` runs hooks after `fn`, which is what the unit tests need).
  `telemetry.md` (Approved): `telemetry.Tracer`, `Span`, `WithKind`, `WithAttrs`,
  `SpanProducer`, `SpanConsumer`, `Token`, `Module`, `ID`, `Int`, `Duration`, `Kind`,
  `telemetry.Meter`, `Counter`, `WithUnit`, `telemetrytest.Capture`. `module-contract.md`
  (Approved): adopts its `signals.Name` unchanged (its "Leaf types placed in `core/authz`
  and `core/signals`" and Proposed change 3); `Manifest.Publishes`, `Manifest.Subscribes`,
  `Env.CoreSignals`, `Deps.Signals`, `Module.Start` (where a module subscribes).
  **Used by:** every module (`Deps.Signals`: `Publish` in a use case, `Subscribe` in
  `Start`; `Ref`, `NewPayload`, `Get`), the `hello` reference module (publishes and
  subscribes to `hello.greeted`), `app` (`New`, `Scope` per module and per core publisher,
  `Close`), the core publishers when they exist (`people.md`: `people.person-created`,
  `people.person-deleted`; `departments-events.md`: its signals), `jobs` (`Deferrer`,
  `Delivery`, `Redeliver`; M4), `authz` (`InDelivery`, Proposed change 7), `core/module`
  (`Name`, already), `moduletest` and every module's unit tests (`signalstest.Fake`).

## Summary

Purpose: let a module react to what another module or the core did, once it is committed,
without importing it. In-process, synchronous, after commit, payloads hold record references
only. Owns no tables or files; nothing is durable before delivery (no outbox in M0).

Exported names (package `signals`):
- `Name` (`<publisher>.<ident>`, past tense; `Validate`, `Publisher`); a state change is its
  own name, never a name plus a status.
- `RecordID = model.ID[record]`; `Payload map[string]RecordID` (ident keys, non-zero IDs, at
  most `MaxRefs = 16`; `Validate`); `Entry`, `Ref[T ~[16]byte](key, id) Entry`,
  `NewPayload(...Entry) Payload`, `Get[T](p, key) (T, error)`.
- `SignalID`; `Signal{ID, Name, At, Payload}` (+`Validate`; ID and At filled at Publish);
  `Handler func(ctx, Signal) error`.
- `Bus{Publish(ctx, Name, Payload) error; Subscribe(Name, Handler) error}` (= `Deps.Signals`).
- `Options{Logger, Tracer, Meter, Clock, HandlerTimeout 100ms..1m, Deferrer}` (+`Validate`);
  `New(Options) (*Dispatcher, error)`; `Dispatcher.Scope(owner, publishes, subscribes) (Bus,
  error)`, `Redeliver(ctx, Delivery) error`, `Close(ctx) error` (a repeated call waits again).
- `MaxDepth = 8`; `InDelivery(ctx) bool`; `Delivery{Signal, Subscriber, Attempt}`;
  `Deferrer.Defer(ctx, Delivery) error` (nil in M0; `jobs` implements it from M4).
- `signalstest`: `Fake` (`NewFake(owner, publishes, subscribes)`, `Published()`, `Deliver(ctx,
  Signal)`, `Handlers()`); same scoping as `Scope`; records publishes only after commit and
  never delivers on its own (stamps `At` with `clock.Real()`).

Key decisions: 1 publish inside the transaction, deliver after commit from `db.AfterCommit`;
2 no outbox, delivery in memory, at-most-once (owner, 2026-10-16); 3 a failed delivery is
final until `jobs` exists, then retryable kinds go to the `Deferrer` (owner, 2026-10-16); 4
payloads are typed record references only, no strings/numbers/times (owner, 2026-10-16); 5
delivery is synchronous on the publisher's goroutine, sequential in subscription (module
build) order, 5 s per handler (owner, 2026-10-16); 6 handler ctx is the publisher's ctx
without cancellation plus a deadline (`Redeliver` keeps cancellation); 7 a handler runs as
the system, never as the person who caused it; 8 a handler panic is recovered, logged with
stack, counted, remaining handlers still run; 9 scopes enforced here, not trusted to the
manifest check; 10 one handler per name per scope; 11 nesting allowed up to `MaxDepth`; 12
span names `signal:<name>` and `deliver:<name>`; 13 a failed or refused `Publish` rolls back
the publisher.

Errs kinds callers handle: `Publish` fails `Invalid` (undeclared name, invalid payload, depth
at `MaxDepth`), `Canceled`/`Timeout` (ctx already done), `Unavailable` (after `Close`);
publishers must let the error fail and roll back the use case. A handler's error never reaches
the publisher. `Subscribe`: `Invalid` (undeclared, nil handler), `Conflict` (second handler for
a name), `Internal` (after `Close`). `Scope`: `Invalid`, `Conflict` (owner scoped twice),
`Internal` after `Close`. `Get`: `Invalid` naming the key (return it from the handler).
`Redeliver`: `Invalid`, `NotFound` (no handler), `Unavailable`, `Canceled`/`Timeout`, or the
handler's own error unchanged. Handler outcomes: `ok`, `failed`, `deferred`, `read_only`
(`db.ErrReadOnly`, warn, never deferred), `panicked`. No exported sentinels.

Owns: no routes, permissions, settings or signals of its own; names come from manifests. Only
config-like value is `HandlerTimeout` (app passes 5 s). Metrics `bms.signals.published{signal,
module}` and `bms.signals.delivered{signal,module,outcome}`; never the payload on a log, span
or metric. Core publishers get their own `Scope` from `app` (`people`, `departments`,
`events`); in M0 the core publishes none (`Env.CoreSignals` empty; from M1 it lists `identity.account-created`; from M2 also `access.grant-created`, `access.grant-revoked`, `people.person-created`, `people.person-deleted`, `people.deletion-requested`); `hello.greeted` carries
`{person}`.

Rules for modules: declare names in the manifest; subscribe once per name in `Module.Start`
(no I/O, works read-only); never publish in `Start`; publish inside the `Transact` that makes
the change; handlers are quick and idempotent on `Signal.ID`, long work becomes a job.

Expects of dependencies: lib-errs; lib-core (`clock`, `validate`); model (`ID[E]`, `NewID`);
db (`AfterCommit` runs hooks on commit or at once outside a transaction, passes the caller's
ctx unchanged; `ErrReadOnly`; `dbtest.Fake.Transact` runs hooks after fn and skips them on
failure); telemetry (`Tracer` Producer/Consumer spans, `Meter` counters,
`telemetrytest.Capture`); module-contract (`Manifest.Publishes/Subscribes`, `Env.CoreSignals`,
`Deps.Signals`, `Start`; the graph check of unknown or disabled publishers is `Resolve`'s).
`app` order at shutdown: HTTP drained, runnables (jobs, M4), `signals.Close`, `Set.Stop`, `db.Close`,
`telemetry.Close`.

Accepted deviations (2026-10-17, reviewer-approved): `bms.signals.published` is counted in the after-commit hook (rollback counts nothing); `Close` while deliveries run waits again and returns Timeout/Canceled, nil only once nothing is in flight (F32); the `Deferrer` gets the handler's trace and in-delivery marker but a fresh `HandlerTimeout` deadline (up to 2x per subscriber); consumer spans carry `Int` depth, producer spans `Int` subscribers; `NewPayload` records a repeated key under reserved key `!repeated`, which `Validate` reports as F4 on `payload`; `signalstest.Fake` stamps `At` with `clock.Real()`; a delivery arriving after `Close` is dropped with one warn and not counted; `Redeliver` after `Close` is `Unavailable`.

Pending proposed changes (targets not written): 6 jobs.md (implement `Deferrer`, call `Redeliver`). Items 1, 2, 3, 4, 5, 7, 9, 10, 11 are applied, and 8 for people and departments-events (2026-10-18, D37).

## Purpose

Let a module react to what another module or the core did, once it is committed, without
importing it.

## Responsibilities

- Define what a **signal** is: a `Name` (D24, `<publisher>.<what-happened>`), an ID, when
  it was published, and a **payload** that can hold record references and nothing else, so
  no personal data can travel on a signal by construction (privacy-data).
- Give publishers and subscribers one small **`Bus`**: `Publish` inside the use case that
  made the change, `Subscribe` once in `Module.Start`.
- Tie publishing to the publisher's **transaction**: a signal published inside a
  transaction is delivered only after that transaction commits (from `db.AfterCommit`), in
  publish order; a rolled-back transaction delivers nothing; outside a transaction it is
  delivered at once.
- **Deliver** synchronously to every subscriber in turn, each under its own deadline, each
  failure logged, counted and recorded on a span, and never propagated to the publisher.
- **Scope** the bus per module (and per core publisher) to the names its manifest declared,
  so an undeclared publish or subscribe fails with `errs.Invalid` (module-contract "`Deps`
  fields by spec").
- Provide the **seam for retries**: a failed delivery is handed to a `Deferrer` when one is
  wired (`jobs`, M4) and redelivered through `Redeliver`; in M0 no `Deferrer` exists and a
  failure is final.
- Give tests a scoped **fake bus** that records what a module published and lets a test
  deliver a signal to the module's handlers.

## Not responsible for

| Concern | Owner |
|---|---|
| Declaring which signals a module publishes and subscribes to; checking at start-up that every subscribed name has a publisher (unknown name → `Invalid`; publisher compiled in but disabled → warning); the admin graph | `module-contract.md` (`Manifest.Publishes`/`Subscribes`, `Env.CoreSignals`, `Resolve` step 5, decision 6). `app` hands the resulting lists to `Scope`. |
| The transaction, the after-commit hook, and whether an autocommit write runs the hook at once | `db.md` (`Transact`, `AfterCommit`). This spec only registers a hook. Its Proposed change 3 is answered here: no outbox table; delivery from the hook (decision 2). |
| Retrying a failed delivery later, with backoff, only on the writing copy; storing a deferred delivery until then | `jobs.md` (M4) implements `Deferrer` and calls `Redeliver` (Proposed change 6). Until then a failure is final (decision 3). |
| Durability across a crash between commit and delivery (an outbox) | nobody in M0 (decision 3). If a consumer ever needs at-least-once, `signals` adds a `core.signal_outbox` set written through the same `Transact`; the `Bus` interface does not change. |
| What a subscriber does with a signal, what it reads, and whose permission that needs | the subscribing module's spec. A handler runs as the system, not as the person who caused the signal (decision 7); it reads data through contracts (D19), never from the payload, which holds IDs only. |
| Payload fields beyond record references (a count, a status, a time) | nobody (decision 4). The name says what happened; everything else is fetched through a capability or an extension point (D19: signals are "for reactions, not for reading data"). |
| Cross-process messaging, `LISTEN`/`NOTIFY`, webhooks, a broker | nobody (D24: signals are in-process; db.md "Not responsible for"). Both copies (D27) run their own dispatcher; only the writer publishes, because publishing happens inside writes. |
| Spans, counters, the logger, the privacy policy on attributes | `telemetry.md`. This spec uses them: a Producer span per publish, a Consumer span and a counter per delivery (its Proposed change 6, answered under "Proposed changes elsewhere" with token-valid span names). |
| Audit of what a handler reads or changes | `audit.md`, called by the handler's module like any other code path. |
| Human-readable titles for signals | the module's message catalogue (`module.<name>.signal.<rest>.title`, module-contract decision 8). |
| Trust between regionals: signals crossing installations | Q-TRUST, later. The envelope is serializable (IDs and a name) so that stays possible. |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `Name` | Identify a signal as `<publisher>.<ident>`. | Adopted unchanged from module-contract. |
| `Payload`, `Ref`, `NewPayload`, `Get` | Carry record references by role, and nothing else. | `map[string]RecordID`; keys are idents; IDs non-zero. |
| `Signal`, `Handler`, `Bus` | The contract a publisher and a subscriber see. | `Publish` in a use case; `Subscribe` in `Start`. |
| `Dispatcher`, `Options`, `New`, `Scope`, `Close` | Route each committed signal to its subscribers, bounded and observed. | One per process, built by `app`; scopes are per module. |
| `Deferrer`, `Delivery`, `Redeliver` | Hand a retryable failure to `jobs` and let it try again. | `Deferrer` is nil in M0. |
| `InDelivery` | Tell a callee that it runs inside a handler. | For nesting depth here and for `authz` later. |
| `signalstest.Fake` | A scoped `Bus` with no dispatcher: records publishes after commit, delivers on demand. | What `moduletest` and module tests hand to a `Factory`. |

## Interface

```go
// Package signals is the in-process publish/subscribe mechanism modules react to each other
// through (D9, D19, D24). A signal is published inside the use case that made the change
// and delivered to its subscribers after that change is committed. It imports lib, model,
// core/db (the after-commit hook) and core/telemetry; core/module imports it for Name.
package signals

import (
	"context"
	"log/slog"
	"time"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/model"
)

// ---- Name ----

// Name identifies a signal (D24): "<publisher>.<ident>", for example
// "volunteers.shift-completed" or "people.person-created". The publisher is the module's
// name or a core area. Both segments follow module-contract's ident grammar
// "[a-z][a-z0-9]*(-[a-z0-9]+)*", at most 64 bytes each. The second segment says what
// happened, in the past tense; a change of state is its own name
// ("registrations.application-approved"), never a name plus a status (decision 4).
type Name string

// Validate fails with errs.Invalid unless n is "<ident>.<ident>" (F1).
func (n Name) Validate() error

// Publisher returns the first segment. Cannot fail; meaningful once Validate passes.
func (n Name) Publisher() string

// ---- Payload ----

// RecordID is a record identifier whose kind is known only from the key it sits under.
// It is a model.ID, so it marshals as a canonical UUID string and converts to and from any
// typed ID by its underlying [16]byte.
type RecordID = model.ID[record]

type record struct{}

// Payload is what a signal carries: references to the records involved, by role, and
// nothing else. A payload cannot hold a name, an address, a note or a number, so no
// personal data can travel on a signal (privacy-data: "Never in ... signals"). A
// subscriber that needs more asks a capability or an extension point with the IDs (D19).
//
// Keys are idents (the same grammar as a Name segment). By convention the core's shared
// keys are "person", "department", "event" (model.PersonID, DepartmentID, EventID) and a
// module's own record is its kind ("shift", "application"); the publisher's spec lists
// each signal's keys and their ID types under "Signals". At most MaxRefs entries.
type Payload map[string]RecordID

// MaxRefs bounds a payload. Sixteen is far more than any signal so far needs; the bound
// exists so a payload cannot become a data channel.
const MaxRefs = 16

// Entry is one payload reference, built by Ref.
type Entry struct {
	Key string
	ID  RecordID
}

// Ref builds an Entry from any typed ID:
//   signals.Ref("shift", shiftID), signals.Ref("person", personID)
// Cannot fail; NewPayload and Validate check it.
func Ref[T ~[16]byte](key string, id T) Entry

// NewPayload builds a Payload from entries. It cannot fail; a bad key, a zero ID, a
// repeated key or too many entries are caught by Validate, which Publish calls (F2–F4),
// so a publisher writes one line and gets one error.
func NewPayload(entries ...Entry) Payload

// Validate fails with errs.Invalid joining one validate.FieldError per problem, fields
// "payload.<key>" (key not an ident; zero ID) and "payload" (more than MaxRefs entries).
// A nil or empty payload is valid: some signals name nothing but the fact.
func (p Payload) Validate() error

// Get returns the reference under key as the subscriber's typed ID:
//   shift, err := signals.Get[volunteers.ShiftID](s.Payload, "shift")
// It fails with errs.Invalid naming the key when the key is absent or its ID is zero
// (F5): the publisher did not keep its documented shape, and the handler should return
// that error so the delivery is logged against the publisher's name.
func Get[T ~[16]byte](p Payload, key string) (T, error)

// ---- Signal, Handler, Bus ----

// SignalID identifies one publication, for logs, spans and later redelivery.
type SignalID = model.ID[signal]

type signal struct{}

// Signal is one thing that happened. The dispatcher fills ID and At at Publish (At is the
// publish time inside the transaction, from the clock, not the commit time). It is a
// plain, serializable value: a Deferrer may store it.
type Signal struct {
	ID      SignalID
	Name    Name
	At      time.Time
	Payload Payload
}

// Validate fails with errs.Invalid joining a validate.FieldError per problem: zero ID,
// invalid Name, zero At, invalid Payload (F6).
func (s Signal) Validate() error

// Handler reacts to one signal. It runs on the publisher's goroutine after the publisher's
// transaction committed, with a ctx that carries the trace but no transaction, no
// cancellation from the request that caused it, a deadline of Options.HandlerTimeout, and
// InDelivery(ctx) == true. It must be quick (a small write, a notification, enqueueing a
// job); long work goes to jobs. It is called at most once per Signal per subscriber from
// Publish; a Redeliver is a further call with the same Signal, so a handler that writes
// should be idempotent on Signal.ID. Its error never reaches the publisher: the dispatcher
// logs it once, counts it and marks the delivery span (F21–F26). It runs as the system,
// not as any person (decision 7).
type Handler func(ctx context.Context, s Signal) error

// Bus is what a publisher or a subscriber holds: Deps.Signals for a module, a scoped bus
// from Scope for a core publisher. Safe for concurrent use.
type Bus interface {
	// Publish records that something happened. name must be in the scope's publishes (F16)
	// and p must validate (F17). Inside a transaction (the ctx Transact handed out, or a
	// child of it) the signal is delivered after commit and Publish returns nil at once,
	// meaning "will be delivered if this transaction commits"; if the transaction rolls
	// back or its commit fails, nothing is delivered. Outside a transaction the signal is
	// delivered before Publish returns. Either way Publish returns nil whatever the
	// subscribers did. It fails with errs.Canceled or errs.Timeout if ctx is already done
	// (F18), errs.Invalid when called from a handler nested MaxDepth deep (F19), and
	// errs.Unavailable after Close (F20). Because Publish is called before the publisher
	// commits, any of these errors makes the publisher's use case fail and roll back, which
	// is the intended effect: a change whose signal cannot be published is not made.
	Publish(ctx context.Context, name Name, p Payload) error

	// Subscribe registers h for name, once per name per scope. It is called in
	// Module.Start, does no I/O and succeeds on a read-only copy. name must be in the
	// scope's subscribes (F12); h must not be nil (F13); a second Subscribe for the same
	// name fails with errs.Conflict (F14); after Close it fails with errs.Internal (F15).
	// Handlers run in subscription order, which app makes the module build order.
	Subscribe(name Name, h Handler) error
}

// ---- Dispatcher ----

// Options is what New needs. app builds it once per process (Proposed change 5).
type Options struct {
	Logger *slog.Logger    // component=signals; never nil
	Tracer telemetry.Tracer // scope "signals"; never nil
	Meter  telemetry.Meter  // scope "signals"; never nil
	Clock  clock.Clock      // Signal.At and handler durations; never nil
	// HandlerTimeout is the deadline each handler gets. 100ms..1m. app passes 5s
	// (Settings). A handler that needs longer registers a job instead.
	HandlerTimeout time.Duration
	// Deferrer receives a delivery that failed with a retryable kind so it can be tried
	// again later (jobs, M4). nil means a failure is final and is logged as such (decision
	// 3). It is optional in every milestone: the dispatcher works without it.
	Deferrer Deferrer
}

// Validate fails with errs.Invalid joining one validate.FieldError per problem, fields
// "signals.logger", "signals.tracer", "signals.meter", "signals.clock",
// "signals.handler_timeout" (F7).
func (o Options) Validate() error

// MaxDepth is how deep handlers may publish from handlers: a signal published by a handler
// running at depth MaxDepth fails (F19). Eight is more than any sane chain and small
// enough that a loop between two modules is caught within one request.
const MaxDepth = 8

// Dispatcher routes committed signals to their subscribers. One per process; built by app
// before the modules, closed after the HTTP server drained and before Set.Stop. Safe for
// concurrent use from New until Close returns.
type Dispatcher struct{ /* unexported: opts, mu, scopes, handlers, inflight sync.WaitGroup, closed, counters */ }

// New validates opts, creates the two counters (Behavior: "Telemetry") and returns a
// dispatcher with no scopes. It fails with errs.Invalid for bad options (F7) and with the
// meter's kind if a counter cannot be created (F8).
func New(opts Options) (*Dispatcher, error)

// Scope returns the Bus for one publisher-and-subscriber: a module (owner = Manifest.Name,
// publishes = Manifest.Publishes, subscribes = Manifest.Subscribes) or a core area
// (owner = "people", its own lists). owner follows the module-name grammar
// "[a-z][a-z0-9]{1,31}" (F9); every name in publishes must validate and have Publisher()
// == owner, every name in subscribes must validate, and neither list may repeat a name
// (F10); one Scope per owner (F11, errs.Conflict); errs.Internal after Close (F15). The
// scope is a view: it holds no goroutines and nothing to close.
// Consumers: app's Provision (Deps.Signals), app for each core publisher.
func (d *Dispatcher) Scope(owner string, publishes, subscribes []Name) (Bus, error)

// Redeliver delivers dl.Signal to the one handler dl.Subscriber registered for
// dl.Signal.Name, with the same ctx rules as a first delivery (a deadline of
// HandlerTimeout, InDelivery true) except that cancellation of ctx is honoured, since the
// caller is a job runner that must stop on shutdown. Unlike a first delivery, the
// handler's error is returned unchanged so the caller can decide by kind; the delivery is
// still counted and spanned, with attempt = dl.Attempt, and never handed to the Deferrer
// (the caller is the retrier). It fails with errs.Invalid for an invalid dl (F28),
// errs.NotFound when no such subscriber has a handler for that name, for example because
// the module was disabled since (F29), errs.Unavailable after Close (F31) and
// errs.Canceled or errs.Timeout when ctx ends (F31).
// Consumer: jobs (M4).
func (d *Dispatcher) Redeliver(ctx context.Context, dl Delivery) error

// Close refuses further Publish, Subscribe and Scope calls and waits for deliveries in
// flight to finish, within ctx. It fails with errs.Timeout or errs.Canceled when ctx ends
// first (F32); the deliveries then finish on their own goroutines and their results are
// still counted. A repeated Close waits again: it returns Timeout or Canceled while deliveries
// are still in flight and nil only once nothing is.
// Consumer: app, after the HTTP server drained and before Set.Stop.
func (d *Dispatcher) Close(ctx context.Context) error

// ---- The seam for jobs ----

// Delivery names one attempt to deliver one signal to one subscriber. It is a plain value
// a Deferrer can store and hand back to Redeliver.
type Delivery struct {
	Signal     Signal
	Subscriber string // the scope owner: a module name or core area
	Attempt    int    // 1 for the delivery that failed; the retrier increments
}

// Validate fails with errs.Invalid joining a validate.FieldError per problem: Signal
// invalid, Subscriber not a module name, Attempt < 1 (F28).
func (d Delivery) Validate() error

// Deferrer takes a delivery whose handler failed with errs.Unavailable or errs.Timeout
// (and not db.ErrReadOnly) so it can be retried later. jobs implements it (jobs.md,
// Proposed change 6): it stores the Delivery on the writing copy and calls Redeliver with
// backoff. Defer is called from the delivering goroutine with the handler's trace and
// in-delivery marker but a fresh HandlerTimeout deadline (decision 5; one subscriber can
// take up to 2x HandlerTimeout), no request cancellation; it must be quick and must not deliver anything itself. A
// Defer error is logged beside the handler's error and the delivery is final (F23).
type Deferrer interface {
	Defer(ctx context.Context, d Delivery) error
}

// ---- Context ----

// InDelivery reports whether ctx is the ctx of a running handler (first delivery or
// Redeliver), at any nesting depth. Cannot fail. Consumers: the dispatcher (nesting
// depth); authz, proposed, so that a session value carried along by a detached request ctx
// is never treated as the actor of what a handler does (decision 7, Proposed change 7).
func InDelivery(ctx context.Context) bool
```

```go
// Package signalstest gives tests a scoped Bus with no dispatcher behind it. Fake is what
// moduletest.Conformance and a module's unit tests hand to a Factory as Deps.Signals.
package signalstest

import (
	"context"

	"github.com/zedgama3/bms/internal/core/signals"
)

// Fake implements signals.Bus with the same scoping rules as Dispatcher.Scope. Publish
// validates as the real bus does, registers a db.AfterCommit hook and records the Signal
// when the hook runs, so a test that rolls back a dbtest.Fake transaction sees nothing
// published and a test that commits sees the signal with a non-zero ID and At. Publish
// never delivers; At is stamped with clock.Real(). Subscribe stores one handler per name. Safe for concurrent use.
type Fake struct{ /* unexported */ }

// NewFake fails as Scope does (F9, F10) for a bad owner or lists; a module's test passes
// its own manifest's fields so an undeclared name fails the test.
func NewFake(owner string, publishes, subscribes []signals.Name) (*Fake, error)

// Published returns every signal recorded so far (after commit), in order, as a copy.
// Cannot fail.
func (f *Fake) Published() []signals.Signal

// Deliver calls the handler the module registered for s.Name with a ctx derived from ctx
// (InDelivery true, no deadline added: the test's ctx bounds it) and returns the handler's
// error unchanged, so a test asserts the handler's kinds directly. It fails with
// errs.Invalid if s fails Validate, errs.NotFound when no handler is registered for the
// name (F33), and errs.Internal wrapping the panic value when the handler panics (F33).
func (f *Fake) Deliver(ctx context.Context, s signals.Signal) error

// Handlers returns the names the module subscribed to, sorted. Cannot fail. For a test
// that checks Start subscribed to everything the manifest declares.
func (f *Fake) Handlers() []signals.Name
```

### Who needs each member

| Member | Consumer(s) | Pre-condition | Post-condition |
|---|---|---|---|
| `Name`, `Validate`, `Publisher` | every module's manifest; `module.Manifest.Validate`, `Resolve` (already); `Scope`; `Bus` | — | one grammar everywhere |
| `RecordID`, `Payload`, `MaxRefs`, `Entry`, `Ref`, `NewPayload` | every publishing module and core publisher (`people`, `departments-events`); `hello` | typed IDs in hand | a payload that can hold only IDs |
| `Payload.Validate` | `Publish`, `Signal.Validate`, `Delivery.Validate`; publishers' tests | — | every problem named by key |
| `Get` | every subscribing module's handler; `hello` | a documented key | the typed ID, or `Invalid` naming the key |
| `SignalID`, `Signal`, `Validate` | handlers (`s.ID` for idempotency and logs); `Deferrer` and `jobs` (store and hand back); `signalstest` | — | serializable |
| `Handler` | every subscribing module (`signals.go`); `hello` | — | at most once per publish per subscriber |
| `Bus.Publish` | every publishing module's service, inside its `Transact`; core publishers | inside or outside a transaction | delivered iff committed |
| `Bus.Subscribe` | every subscribing module, in `Module.Start`; core subscribers if any | before `Close` | one handler per name per scope |
| `Options`, `Validate`, `New` | `app` (once, in `serve`, after `telemetry.New` and `db.Open`, before `Build`) | logger, tracer, meter, clock | counters created, no goroutines |
| `Dispatcher.Scope` | `app`'s `Provision` (`Deps.Signals = disp.Scope(m.Name, m.Publishes, m.Subscribes)`); `app` for each core publisher | manifest validated by `Resolve` | a scoped `Bus` |
| `Dispatcher.Redeliver`, `Delivery`, `Deferrer` | `jobs` (M4): implements `Deferrer`, stores `Delivery`, calls `Redeliver` on the writer; `Options.Deferrer` set by `app` once `jobs` exists | handler still registered | the handler's own error, by kind |
| `Dispatcher.Close` | `app` at shutdown, after the HTTP server drained and before `Set.Stop` | — | nothing in flight |
| `MaxDepth`, `InDelivery` | the dispatcher; `authz` (proposed); handlers that want to know | — | — |
| `signalstest.Fake` and accessors | `moduletest.Conformance` (Proposed change 1); every module's unit tests of `New`, its handlers and its publishing use cases; `hello` | the module's manifest fields | publishes recorded after commit; handlers callable |

## Types

Defines `Name`, `RecordID`, `Payload`, `Entry`, `SignalID`, `Signal`, `Handler`, `Bus`,
`Options`, `Dispatcher`, `Delivery`, `Deferrer`; in `signalstest`, `Fake`. Uses
`model.ID[E]` (for `RecordID` and `SignalID`), `clock.Clock`, `validate.Errors`,
`validate.FieldError`, `slog.Logger`, `telemetry.Tracer`, `telemetry.Meter`,
`telemetry.Counter`, `telemetry.Span`, `telemetry.Attr`, `db.AfterCommit`, `db.ErrReadOnly`.
Nothing from `core/module` (it imports this package).

### Payload keys of the core's signals

None in M0: the core publishes no signal yet, so `Env.CoreSignals` is empty. From M1 it lists `identity.account-created` (payload `{account: AccountID, person: PersonID}`; published inside the `Write` transaction of sign-up and of the superuser's creation; no personal data, identity.md "Signals"). From M2 it also lists `access.grant-created` and `access.grant-revoked` (`{grant, person, role, department?}`; authz.md), and `people.person-created {person}`, `people.person-deleted {person}` and `people.deletion-requested {request, person}` (people.md). From M3 it also lists `departments.department-created|archived|restored {department}` and `events.event-created {event, copied-from?}`, `events.event-changed {event}`, `events.event-closed {event}`, `events.event-reopened {event}` (departments-events.md, scopes `departments` and `events` from `app`; applied 2026-10-18, D37). The `hello`
module's `hello.greeted` carries `{person}`.

## Data

None. `signals` owns no tables and no files in M0 (decision 3). What it holds in memory and
what it emits, by field:

| Field | Where | Level | Why | Retention |
|---|---|---|---|---|
| `Signal.ID`, `Name`, `At` | memory until delivered; logs and spans (`signal_id`, `signal`) | not about a person | find one delivery in a trace | the collector's (D20) |
| `Payload` values (IDs, among them `person`) | memory until delivered; **never logged, never on a span** | internal (an opaque ID; the directory that resolves it is permission-checked) | the handler's input | gone when delivery ends |
| `Delivery` | handed to a `Deferrer`; `jobs` decides where it keeps it | as above | retry | `jobs.md` |

Nothing here is ever personal data beyond an ID: the payload type cannot hold a string or a
number, and the dispatcher logs IDs of the signal, not the payload. Per-person export and
deletion (module-contract `PersonData`) do not reach signals: nothing is stored.

## Permissions

None. `Publish` is called by a service that already checked the permission for the change
it made; `Subscribe`, `Scope`, `New`, `Close` run at start-up and shutdown with no actor;
`Redeliver` is called by `jobs` as the system. A handler runs as the system (decision 7):
what it may do is stated in its module's spec, which names the permission its own
operations would need and why a reaction is allowed without a person holding it. A handler
that reads `personal` or `sensitive` fields does so through its module's store and audit
path like any other code.

## Signals

None published, none subscribed by this package. It is the mechanism.

## Settings

None. Nothing here differs between regionals: which signals exist is code (manifests), how
they are delivered is one rule for every installation, and a regional's choices about
reactions (whether to send a notice, to whom) are the subscribing module's settings.
Constants and the one option, pinned by tests, with the reason each is not a key:

| Value | Where | Default / range | Why not configurable |
|---|---|---|---|
| `HandlerTimeout` | `Options`, set by `app` | 5 s; validated 100 ms..1 m | a handler is quick by contract; an operator cannot make a slow handler correct by waiting longer. An `Options` field (not a constant) so tests run with a short value. |
| `MaxRefs` | constant | 16 | a bound, not a tuning knob |
| `MaxDepth` | constant | 8 | a loop guard, not a tuning knob |
| handlers per name per scope | constant | 1 | one `(signal, subscriber)` names one handler, which `Redeliver` relies on |

## UI

None of its own. The admin "modules" screen (`web-shell.md`, module-contract "UI") shows
each module's `Publishes` and `Subscribes` from `module.Graph` with the titles
`module.<name>.signal.<rest>.title`; the `subscribes` edges and the "publisher disabled"
warnings come from `Resolve`. There is no screen of delivered signals: a maintainer finds a
delivery in the trace of the request that caused it (D20), by `signal` and `signal_id`.

## Behavior

### Decisions settled here, with reasons

1. **Publish inside the transaction, deliver after commit, from `db.AfterCommit`.** 09 §1
   settled "after commit"; db.md gives the hook. The alternatives were delivery inside the
   transaction (a subscriber's failure would roll back the publisher, which 09 §1 forbids,
   and a subscriber would see uncommitted rows through its own connection anyway) and
   publishing after the use case returns (every publisher would then have to remember to
   publish only on success, and a publish inside a nested `Transact` would fire before the
   outer commit). With the hook, a publisher writes `Publish` next to the write it
   describes and gets the right behaviour in every nesting.
2. **No outbox table in M0; delivery is in memory.** An outbox buys at-least-once across a
   crash between commit and delivery, at the cost of a table, a sweeper that must run only
   on the writer (`jobs`, M4), an acknowledgement protocol and a second code path for
   redelivery. D19 says signals are for reactions, not data; every reaction so far (a
   notice, a copy of templates into a new event) is recoverable by a person. The `Bus`
   interface does not change if an outbox is added later, and db.md's Proposed change 3
   already allows it ("writes it through the same `Transact`"). **Confirmed (owner,
   2026-10-16):** at-most-once until `jobs` exists is acceptable; `jobs` (M4) retries
   failed deliveries through `Deferrer`; an outbox is added only if a module later needs
   at-least-once.
3. **A failed delivery is final until `jobs` exists; then it is deferred, not retried
   here.** 09 §1 says failures are "logged and retried by `jobs`". Retrying in the request
   would hold the request; retrying on a goroutine here would duplicate `jobs` (backoff,
   writer-only, survive a restart). So the dispatcher classifies the failure and hands a
   retryable one (`Unavailable`, `Timeout`) to the `Deferrer`; a non-retryable one
   (`Invalid`, `Internal`, `Permission`, `NotFound`, `Conflict`) is a bug or a decision,
   logged at error and not retried, because trying again cannot change it; a write refused
   on a read-only copy (`db.ErrReadOnly`) is logged at warn and not deferred, because the
   `Deferrer` would need to write too (db.md Proposed change 13). In M0 `Deferrer` is nil
   and the log line says "not retried". **Confirmed (owner, 2026-10-16)** together with
   decision 2: a handler failure in M0–M3 is logged and counted, not retried.
4. **Payloads hold record references only: typed IDs under ident keys. No strings, no
   numbers, no times.** privacy-data says personal data is never in a signal; a payload
   type that can hold a `string` makes that a review rule, a type that cannot makes it a
   fact. D19 says a subscriber fetches what it needs through contracts, with the provider
   checking permissions; a payload rich enough to skip that would leak around the
   permission check. A state change is therefore a distinct name
   (`registrations.application-approved`), which also reads better in a manifest and the
   admin graph than a name plus a status field. Rejected: typed Go structs per signal in
   `internal/contracts/<publisher>/` (a package per publisher that no other rule needs,
   plus a registry of types for a future outbox, plus a review rule against personal
   fields) and `map[string]any` (no rule at all). If a consumer ever needs a number (hours
   credited), it is added here as a bounded kind with a design change, not by a module.
   **Confirmed (owner, 2026-10-16):** payloads are record references only.
5. **Delivery is synchronous, on the publisher's goroutine, sequential over subscribers,
   each handler under `HandlerTimeout`.** The alternative, a queue and worker goroutines,
   gives a faster request and costs a bounded queue, a drop policy, a shutdown drain, and a
   trace that no longer contains the reaction. A regional's traffic is small, handlers are
   quick by contract, and a maintainer who opens the trace of a request sees the request,
   its statements and every reaction under one ID (D20). Sequential delivery in
   subscription order (module build order) makes tests deterministic. A handler that needs
   long work registers a job. **Confirmed (owner, 2026-10-16):** delivery is synchronous
   on the publisher's request, with 5 s per handler (`HandlerTimeout`, set by `app`):
   generous for "a small write or a notification" and short enough that a stuck dependency
   does not hold a request past `http.request_timeout` more than once.
6. **The handler's ctx is the publisher's ctx without its cancellation, plus a deadline.**
   db.md runs hooks with the caller's ctx, which for a request is cancelled when the client
   leaves or `http.request_timeout` passes. A reaction to a committed change must not
   depend on whether the browser is still there, so the dispatcher derives
   `context.WithoutCancel(ctx)` (values kept: the trace span, so the Consumer span is a
   child of the request span) and `context.WithTimeout(..., HandlerTimeout)`. Shutdown is
   not a concern here: deliveries ride on request goroutines, which the HTTP server drains
   before `app` calls `Close`, and `Close` waits for the rest. `Redeliver` keeps the
   caller's cancellation, because its caller is the job runner.
7. **A handler runs as the system, never as the person who caused the signal.** The person
   who completed a shift did not ask for the reaction and may not hold the permission the
   reaction needs; the reaction is the regional's policy, stated in the subscribing
   module's spec. The `Signal` carries no actor (a module that needs "who" publishes a
   `{person}` ref and says so). Because decision 6 keeps ctx values, a session value
   attached by `identity` would survive into the handler; `InDelivery(ctx)` exists so
   `authz` can refuse to read it there (Proposed change 7).
8. **A panic in a handler is recovered, logged with its stack, counted, and the remaining
   handlers still run.** go-conventions reserves panics for impossible states, so a panic
   is a bug in one module; the publisher's change is already committed and the other
   subscribers did nothing wrong. Crashing the process would turn one module's bug into
   an outage; re-raising into the request would blame the publisher. The panic is logged at
   error with `stack` (the telemetry handler caps it) so it is as visible as a crash.
9. **Scopes are enforced here, not trusted to the manifest check.** `Resolve` proves the
   graph is consistent; `Scope` proves each call matches the graph, so a module that
   publishes a name it did not declare fails in its own test (with `signalstest.Fake`,
   which applies the same rule) rather than silently publishing something no graph shows.
10. **One handler per name per scope.** A module reacts to a signal in one place
    (`signals.go`); a second handler is a sign of two reactions that should be one. It also
    makes `(signal, subscriber)` identify a handler, which `Redeliver` and a `Deferrer` need.
11. **Nesting is allowed, bounded by `MaxDepth`.** A handler may make a change and publish
    its own signal (a reaction has consequences); a chain of eight is not a design anyone
    intends, so the ninth fails with `Invalid`, the handler returns that error and the log
    names the chain's last signal. Depth travels in the ctx, so it survives a nested
    `Transact`.
12. **Span names are tokens: `signal:<name>` and `deliver:<name>`.** telemetry.md's
    Proposed change 6 suggested `signal <name>`, but its `Tracer.Start` rejects a name
    with a space (F14 there) and would emit `invalid-span-name`. `:` is in the token
    grammar; `<name>` is itself a token. The owner (2026-10-16) widened telemetry's
    span-name grammar only for the api's `<METHOD> <route pattern>` form (telemetry
    decision 19); signals are not HTTP and keep the token names (Proposed change 3).
13. **`Publish` fails when the bus is closed or the name is undeclared, and that failure
    rolls back the publisher.** The publisher calls `Publish` before it commits, so a
    refusal makes the use case fail. That is the right outcome: a declared signal that
    cannot be published means the deployment is in a state (shutting down, misdeclared) in
    which the change should not be made silently without its reactions.

### `Publish`, step by step

1. If `ctx.Err() != nil`, fail (F18). If closed, fail (F20). `name` in the scope's
   publishes (F16); `p.Validate()` (F17). Depth from ctx `>= MaxDepth` → F19.
2. Build `Signal{ID: model.NewID[SignalID](), Name: name, At: clock.Now(), Payload: p}`
   (the payload map is copied, so a publisher that mutates its map afterwards changes
   nothing).
3. Open a `SpanProducer` span `signal:<name>` with `Token("signal", name)`,
   `Module(owner)`, `ID("signal_id", id)`, `Int("subscribers", n)`; end it after step 4 registers
   the hook (the span measures publishing, not delivery; the deliveries are its children
   because the hook's ctx carries the request span, not this one: the hook runs with
   `Transact`'s ctx). Count `bms.signals.published{signal, module}` inside the after-commit
   hook, not at publish time, so a rolled-back transaction counts nothing.
4. `db.AfterCommit(ctx, func(ctx context.Context) { d.deliver(ctx, owner, sig) })`. Inside
   a transaction the hook runs after commit; outside, `db` runs it at once (db.md
   `AfterCommit`), so `deliver` runs before `Publish` returns. `AfterCommit` fails only for
   a nil fn, which cannot happen; a non-nil error is wrapped as `Internal` anyway.
5. Return nil.

### `deliver`, step by step (one signal, all subscribers)

1. `inflight.Add(1)`; `defer Done()`. Snapshot the handlers for `sig.Name` under the read
   lock, in subscription order. If none: nothing to do (not an error; the Producer span
   already recorded `Int("subscribers", 0)`).
2. `base := context.WithoutCancel(ctx)` with the depth value incremented and the
   in-delivery marker set.
3. For each `(subscriber, handler)` in order: `hctx, cancel := context.WithTimeout(base,
   HandlerTimeout)`; open a `SpanConsumer` span `deliver:<name>` with `Token("signal",
   name)`, `Module(subscriber)`, `ID("signal_id", sig.ID)`, `Int("attempt", 1)`, `Int("depth", n)`; run the
   handler under `recover`; `cancel()`; classify the outcome (table below); `RecordError`
   on the span for any non-`ok` outcome; `SetAttrs(Token("outcome", ...),
   Duration("duration_ms", clk.Since(start)))`; end the span; count
   `bms.signals.delivered{signal, module=subscriber, outcome}`; log as the table says; go
   on to the next subscriber whatever happened.

| Handler result | Outcome token | Log | Deferred? |
|---|---|---|---|
| nil | `ok` | none (debug at most) | no |
| `errors.Is(err, db.ErrReadOnly)` | `read_only` | warn: "signal handler cannot write on a read-only copy" | no |
| kind `Unavailable` or `Timeout`, `Deferrer` set, `Defer` nil | `deferred` | info: "signal delivery deferred" | yes |
| kind `Unavailable` or `Timeout`, `Deferrer` nil | `failed` | error: "signal delivery failed (not retried: no deferrer)" | no |
| kind `Unavailable` or `Timeout`, `Defer` fails | `failed` | error, both errors | no |
| any other kind (`Invalid`, `Internal`, `Permission`, `NotFound`, `Conflict`, `Canceled`, `Unauthenticated`, `Unsupported`) | `failed` | error: "signal delivery failed" | no |
| panic | `panicked` | error with `stack` | no |

Every log line carries `signal`, `subscriber`, `signal_id`, `error.kind` (when there is an
error), `attempt`, `duration_ms`, and the error text (through the telemetry handler, which
scrubs and caps). Never the payload. If the handler returned after `HandlerTimeout`
elapsed by the clock, one more warn line "signal handler overran" with `duration_ms` is
written whatever the outcome (F25).

### `Redeliver`, step by step

1. `ctx.Err()` (F31); closed (F31); `dl.Validate()` (F28). Find the handler for
   `(dl.Subscriber, dl.Signal.Name)` (F29).
2. `hctx` = ctx (cancellation kept) with the deadline `min(ctx deadline, now +
   HandlerTimeout)`, depth 1, in-delivery marker; span, counter and log as in `deliver`
   with `Int("attempt", dl.Attempt)`, except that no outcome is `deferred`: the handler's
   error is returned unchanged (F30), a panic is recovered and returned as `errs.Internal`
   wrapping the panic value with outcome `panicked`.

### Concurrency and lifecycle

`*Dispatcher` and every scope are safe for concurrent use. Handlers and scopes live under
one `sync.RWMutex`; `deliver` takes a snapshot, so a `Subscribe` racing a `Publish` sees
either the old or the new list, never a torn one. `inflight` is a `sync.WaitGroup` that
`Close` waits on within its ctx. The dispatcher starts no goroutine: every delivery runs on
the goroutine that committed (a request's, or a job runner's for `Redeliver`). After
`Close`: `Publish` → `Unavailable`, `Subscribe` and `Scope` → `Internal`, `Redeliver` →
`Unavailable`, `Close` waits again and is nil once nothing is in flight. A delivery that arrives after `Close` is dropped with one warn (no payload) and not counted. `app`'s order at shutdown: HTTP server drained → runnables (jobs, M4) stopped → `signals.
Close` → `Set.Stop` → `db.Close` → `telemetry.Close` (Proposed change 5), so no handler
runs against a stopped module or a closed pool.

### Read-only copy

`Subscribe` is in memory, so `Module.Start` succeeds on a read-only copy (module-contract
decision 14, F46 there). `Publish` does not consult `db.ReadOnly()`: a signal describes a
write, and on a read-only copy the write failed before `Publish` was reached (`Transact
(Write)` fails first, db.md F22). The corner that remains is a handler that writes after a
copy became read-only between the probe and the hook: its write fails with
`db.ErrReadOnly`, the outcome is `read_only`, nothing is deferred, and the next probe
flips the state (db.md decision 8).

### Telemetry

Scope `signals`. Spans: `signal:<name>` (Producer) per publish; `deliver:<name>`
(Consumer) per delivery and per redelivery, a child of the span in the publisher's ctx
(the api's request span), with `signal`, `module`, `signal_id`, `attempt`, `outcome`,
`duration_ms`, `error.kind` and `error.op` through `RecordError`. Consumer spans carry
`Int` `depth`; producer spans carry `Int` `subscribers`. Counters (created in
`New`, `WithUnit("{signal}")` and `WithUnit("{delivery}")`):

| Instrument | Attributes |
|---|---|
| `bms.signals.published` | `signal` (token), `module` (publisher); counted in the after-commit hook |
| `bms.signals.delivered` | `signal`, `module` (subscriber), `outcome` ∈ `ok`, `failed`, `deferred`, `read_only`, `panicked` |

No ID and no text on a metric (telemetry F19); the payload is on nothing.

### Imports

Standard library (`context`, `errors`, `fmt`, `log/slog`, `regexp`, `runtime/debug`,
`sort`, `sync`, `time`), `internal/lib/errs`, `internal/lib/validate`,
`internal/lib/clock`, `internal/model`, `internal/core/db` (`AfterCommit`, `ErrReadOnly`
only; no driver type crosses), `internal/core/telemetry`. Never `core/module` (it imports
this package), never `contracts`, a module, `api`, `app` or `config` (depguard rule
`core`), never `go.opentelemetry.io` (`otel-seam`), never pgx (`storage-seam`). The core
graph stays acyclic: `telemetry ← db ← signals ← module`. `signalstest` adds nothing but
`internal/core/db` for `AfterCommit`.

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `Name.Validate` | not `<ident>.<ident>` (one segment, three segments, upper case, `_`, a 65-byte segment) | Invalid | fix the manifest | `TestNameValidate_Table` |
| F2 | `Payload.Validate` (via `Publish`) | a key is not an ident | Invalid, field `payload.<key>` | fix the publisher | `TestPayloadValidate_BadKey_ReturnsInvalid` |
| F3 | `Payload.Validate` | a zero ID | Invalid, field `payload.<key>` | fix the publisher (an optional ref is left out, not zero) | `TestPayloadValidate_ZeroID_ReturnsInvalid` |
| F4 | `Payload.Validate` | more than `MaxRefs` entries, or `NewPayload` given a repeated key (recorded under the reserved non-ident key `!repeated`, which `Validate` reports on field `payload`) | Invalid, field `payload` | fix the publisher | `TestPayloadValidate_TooManyOrRepeated_ReturnsInvalid` |
| F5 | `Get` | key absent, or its ID zero | Invalid naming the key | return it from the handler (logged against the publisher's name) | `TestGet_MissingKey_ReturnsInvalid` |
| F6 | `Signal.Validate` | zero ID, bad name, zero `At`, bad payload (each reported) | Invalid, one `FieldError` each | fix the caller (`jobs`, a test) | `TestSignalValidate_Table` |
| F7 | `Options.Validate`, `New` | `Logger`, `Tracer`, `Meter` or `Clock` nil; `HandlerTimeout` outside 100 ms..1 m; several at once | Invalid, one `FieldError` each | fix app | `TestNew_BadOptions_ReportsEach` |
| F8 | `New` | a counter cannot be created (telemetry F20–F21: the `signals` meter scope already holds the name as another kind) | the meter's kind (Invalid / Conflict) | fix app (one `New` per meter scope) | `TestNew_CounterCreationFails_ReturnsKind` (fake `Meter`) |
| F9 | `Scope`, `signalstest.NewFake` | `owner` fails `[a-z][a-z0-9]{1,31}` | Invalid | fix app | `TestScope_BadOwner_ReturnsInvalid` |
| F10 | `Scope`, `NewFake` | a publishes name invalid or with `Publisher() != owner`; a subscribes name invalid; a name repeated in a list | Invalid, field `publishes[i]` / `subscribes[i]` | fix the manifest | `TestScope_BadLists_ReturnsInvalid` |
| F11 | `Scope` | `owner` already scoped | Conflict | fix app | `TestScope_Twice_ReturnsConflict` |
| F12 | `Bus.Subscribe` | `name` not in the scope's subscribes | Invalid naming the name and the owner | declare it in the manifest | `TestSubscribe_Undeclared_ReturnsInvalid` |
| F13 | `Bus.Subscribe` | `h == nil` | Invalid | fix the module | `TestSubscribe_NilHandler_ReturnsInvalid` |
| F14 | `Bus.Subscribe` | a handler already registered for `name` in this scope | Conflict | one handler per name | `TestSubscribe_Twice_ReturnsConflict` |
| F15 | `Bus.Subscribe`, `Scope` | after `Close` | Internal | fix app ordering | `TestSubscribeAndScope_AfterClose_ReturnInternal` |
| F16 | `Bus.Publish` | `name` not in the scope's publishes | Invalid naming the name and the owner; the publisher's transaction rolls back | declare it in the manifest | `TestPublish_Undeclared_ReturnsInvalidAndNothingDelivered` |
| F17 | `Bus.Publish` | `p` fails `Validate` (F2–F4) | Invalid | fix the publisher | `TestPublish_InvalidPayload_ReturnsInvalid` |
| F18 | `Bus.Publish` | `ctx` already done | Canceled / Timeout | stop | `TestPublish_ContextDone_ReturnsKind` |
| F19 | `Bus.Publish` | called from a handler at depth `MaxDepth` | Invalid, message names the depth and the signal | the handler returns it; redesign the chain | `TestPublish_NestedBeyondMaxDepth_ReturnsInvalid` |
| F20 | `Bus.Publish` | after `Close` | Unavailable; the publisher's use case fails | 503 (shutting down) | `TestPublish_AfterClose_ReturnsUnavailable` |
| F21 | delivery | a handler returns a non-retryable kind | none to the publisher; outcome `failed`; error log; span failed; later subscribers still run | fix the subscriber | `TestDeliver_HandlerFails_LogsCountsAndContinues` |
| F22 | delivery | a handler returns `Unavailable` or `Timeout`, no `Deferrer` | none to the publisher; outcome `failed`; error log "not retried: no deferrer" | fix the dependency; wire `jobs` (M4) | `TestDeliver_RetryableWithoutDeferrer_IsFinal` |
| F23 | delivery | same, with a `Deferrer`: `Defer` succeeds / fails | outcome `deferred` (info) / `failed` (error, both errors logged) | `jobs` retries / fix `jobs` | `TestDeliver_RetryableWithDeferrer_Defers`, `TestDeliver_DeferFails_LogsBothAndIsFinal` |
| F24 | delivery | a handler's write fails with `db.ErrReadOnly` | outcome `read_only`; warn; not deferred | nothing: the copy is read-only | `TestDeliver_ReadOnly_WarnsNotDeferred` |
| F25 | delivery | a handler runs past `HandlerTimeout` | its ctx deadline passes (its I/O fails with `Timeout`); the outcome follows the error it returns; an extra warn "signal handler overran" with `duration_ms` | make the handler quick or move the work to a job | `TestDeliver_HandlerOverruns_WarnsWithDuration` (fake clock advanced inside the handler; asserts the ctx deadline too) |
| F26 | delivery | a handler panics | recovered; outcome `panicked`; error log with `stack`; later subscribers still run; the publisher is unaffected | fix the subscriber | `TestDeliver_HandlerPanics_RecoveredLoggedAndContinues` |
| F27 | delivery | no subscriber for the name | none: `published` counted, nothing delivered | — | `TestPublish_NoSubscribers_IsNotAnError` |
| F28 | `Redeliver`, `Delivery.Validate` | `dl` invalid (signal, subscriber grammar, `Attempt < 1`) | Invalid | fix `jobs` | `TestRedeliver_InvalidDelivery_ReturnsInvalid` |
| F29 | `Redeliver` | no handler for `(Subscriber, Name)` (module disabled or renamed since) | NotFound | `jobs` drops the delivery with a log line | `TestRedeliver_UnknownSubscriber_ReturnsNotFound` |
| F30 | `Redeliver` | the handler fails or panics | the handler's error unchanged (a panic as Internal); counted with `attempt`; never deferred | `jobs` decides by kind | `TestRedeliver_HandlerFails_ReturnsErrorUnchanged` |
| F31 | `Redeliver` | after `Close`; `ctx` done | Unavailable; Canceled / Timeout | stop | `TestRedeliver_AfterCloseOrCtxDone_ReturnsKind` |
| F32 | `Close` | deliveries in flight do not finish within `ctx` | Timeout / Canceled; they finish on their own; a repeated `Close` waits again, nil only once nothing is in flight | log and exit | `TestClose_InFlight_ReturnsTimeout`, `TestClose_Twice_IsNil` |
| F33 | `signalstest.Fake.Deliver` | `s` invalid; no handler for the name; the handler panics | Invalid; NotFound; Internal wrapping the panic value | fix the test or the module | `TestFakeDeliver_Table` |

Behaviours that are not failures but are proven: a transaction that rolls back after
`Publish` delivers nothing and counts nothing
(`TestPublish_TransactionRollsBack_DeliversNothing`); a commit delivers in publish order
(`TestPublish_SeveralInOneTransaction_DeliveredInOrderAfterCommit`); outside a transaction
delivery completes before `Publish` returns (`TestPublish_OutsideTransaction_DeliversAtOnce`).

Members that cannot fail, and why: `Publisher` (string split); `Ref`, `NewPayload` (build a
value; `Validate` judges it); `InDelivery` (a ctx lookup); `Published`, `Handlers` (copies).
Each has a success-path test.

## Lock-in check

No external technology: plain Go, in one process, over `db.AfterCommit`. Nothing depends on
PostgreSQL beyond the hook `db` already provides, and nothing on a broker. If a later
design needs cross-process delivery (two copies, Q-OFFLINE), the `Bus` interface is
implementable over PostgreSQL `LISTEN`/`NOTIFY` or an outbox table polled by `jobs`, both
on the OSI-licensed database every regional already runs.

## Test plan

**Unit** (no infrastructure, `go test -race`, packages `signals_test` and
`signalstest_test`, table-driven; `dbtest.NewFake()` for transactions, because its
`Transact` runs `AfterCommit` hooks after `fn` and skips them when `fn` fails;
`clock.NewFake` for `At` and durations; `telemetrytest.New(t)` for logger, tracer and
meter, so any policy violation fails the test; invented module names `alpha`, `beta`;
invented IDs only):

- Names and payloads: `TestNameValidate_AcceptsExamples` (`hello.greeted`,
  `volunteers.shift-completed`, `people.person-created`); F1–F6;
  `TestPayload_JSONRoundTrip` (canonical UUID strings; `Get` after decode);
  `TestRef_TypedIDRoundTrip` (`Ref` then `Get` returns the same typed ID);
  `TestNewPayload_CopiesSoCallerMutationIsHarmless`.
- Dispatcher: F7–F20, F27, F32; `TestScope_ReturnsIndependentBuses` (two owners, each
  sees only its names); `TestSubscribe_OrderIsSubscriptionOrder`;
  `TestPublish_FillsIDAndAtFromClock`; the three "not failures" above;
  `TestPublish_NestedFromHandler_DeliveredAfterInnerCommit` (a handler runs a fake
  `Transact` and publishes inside it; depth attribute 2 on the inner spans);
  `TestPublish_InsideReadTransaction_DeliveredAfterCommit`.
- Delivery: F21–F26; `TestDeliver_CtxHasNoRequestCancellation` (cancel the publisher's ctx
  inside the handler's predecessor; the next handler still runs);
  `TestDeliver_CtxCarriesNoTransaction` (the handler's `dbtest` calls record `InTx ==
  false`); `TestDeliver_InDeliveryTrueInsideFalseOutside`;
  `TestDeliver_SpansAndCountersPerOutcome` (through `telemetrytest`: one Producer span, one
  Consumer span per subscriber as a child of the test's span, `outcome` attribute, the two
  counters with the right attributes, no ID on any measurement);
  `TestDeliver_NeverLogsPayloadOrSpansIt` (a payload whose ID string is the marker;
  `!c.Contains(marker)` over records and spans except the `signal_id` attribute, which is
  a different ID).
- Redeliver: F28–F31; `TestRedeliver_HonoursCallerCancellation`;
  `TestRedeliver_AttemptOnSpanAndLog`.
- signalstest: F33; `TestFake_PublishRecordsAfterCommitOnly` (rollback → empty;
  commit → one signal with ID and At); `TestFake_SameScopingAsDispatcher` (F9, F10, F12,
  F16 against the fake); `TestFake_DeliverReturnsHandlerErrorUnchanged`;
  `TestFake_HandlersSorted`; `TestFake_SafeForConcurrentUse`.
- Lint: passes depguard (`core`, `otel-seam`, `storage-seam`), `errcheck`, `errorlint`,
  `wrapcheck`, `contextcheck` (the `WithoutCancel` derivation is deliberate and commented;
  a `//nolint:contextcheck` with the reason if the linter objects), `sloglint`.

**Integration** (`//go:build integration`, `dbtest.TempDB`, each test on its own
database): `TestPublish_AfterCommit_HandlerSeesTheRow` (the publisher inserts a row and
publishes inside `Transact(Write)`; the handler selects the row through the pool on its
own connection and finds it, which proves "after commit" against a real server);
`TestPublish_CommitFails_DeliversNothing` (a deferred constraint fails at `COMMIT`, db.md
F25; no delivery); `TestPublish_RollbackDeliversNothing_Real`;
`TestDeliver_HandlerWriteOnReadOnlyCopy_OutcomeReadOnly` (via `ALTER DATABASE ... SET
default_transaction_read_only = on` and a new pool, as db.md F17 does).

**Conformance:** `moduletest.Conformance` hands each `Factory` a
`signalstest.NewFake(m.Name, m.Publishes, m.Subscribes)` as `Deps.Signals` (Proposed
change 1), so `start_read_only` also proves that `Start` subscribes only to declared names;
a new rule `subscribes_every_declared_name` asserts `Fake.Handlers()` equals
`m.Subscribes` after `Start`. The `hello` module's integration test (app.md) publishes
`hello.greeted` through a request and asserts its own handler ran, in the same trace.

## Open questions

None. The draft's three questions (at-most-once until `jobs` exists; payloads as record
references only; synchronous delivery with 5 s per handler) were answered by the owner on
2026-10-16, each with the proposed answer, and are recorded in decisions 2 and 3, 4, and 5.

## Proposed changes elsewhere

Status as of 2026-10-18: items 1, 2, 3, 4, 5, 7, 9 and 10 are applied, and 8 for people and departments-events (D37). Items 6 and 11
wait for their target to be written and are carried by this list until then.

1. **`plan/design/module-contract.md` (row 4)** (applied 2026-10-16): `Deps.Signals` is `signals.Bus` as
   defined here; `Provision` sets it to `disp.Scope(m.Name, m.Publishes, m.Subscribes)`;
   `moduletest.Conformance` supplies `signalstest.NewFake(m.Name, m.Publishes,
   m.Subscribes)` and gains the rule `subscribes_every_declared_name` (after `Start`,
   `Fake.Handlers()` equals the manifest's `Subscribes`); note on `Module.Start` that a
   module subscribes at most once per name and never publishes in `Start`; the `hello`
   module's `hello.greeted` payload is `{person}`. Its Proposed change 3 is answered: names
   adopted unchanged, scoping as asked, subscriptions in `Start`.
2. **`plan/design/db.md` (row 5)** (applied 2026-10-16): its Proposed change 3 is answered: delivery from
   `AfterCommit`, no outbox, no change to `db`. One sentence to add beside `AfterCommit`:
   "`signals` detaches the hook's ctx from the request's cancellation itself; `db` passes
   the caller's ctx unchanged."
3. **`plan/design/telemetry.md` (row 15b)** (applied 2026-10-16): its Proposed change 6 is answered for
   signals with the names `signal:<name>` (Producer) and `deliver:<name>` (Consumer),
   because `signal <name>` contains a space and `Tracer.Start` would replace it with
   `invalid-span-name` (its F14). The same applied to the api's proposed `<METHOD> <route
   pattern>` request span name (its Proposed change 3); the owner (2026-10-16) chose to let
   the span-name grammar admit exactly one space, between an upper-case HTTP method and a
   route pattern (telemetry decision 19), so the api keeps OpenTelemetry's HTTP form and
   signals keep their token names. Counters are
   `bms.signals.published{signal,module}` and `bms.signals.delivered{signal,module,
   outcome}` with `outcome` ∈ `ok`, `failed`, `deferred`, `read_only`, `panicked`
   (replacing the suggested `retried`, which is `jobs`' to count on `bms.jobs.runs`).
4. **`plan/07-abstractions.md`, `signals` entry** (applied 2026-10-16): replace "Owns" with: the `Name` type;
   the `Payload` (record references only); the `Bus` (`Publish` inside the publisher's
   transaction, `Subscribe` in `Start`); the `Dispatcher` (delivery after commit from
   `db.AfterCommit`, synchronous, in order, each handler under a deadline, failures logged
   and counted, never rolling back the publisher); per-module scoping to the manifest's
   names; the `Deferrer` seam for `jobs`; `signalstest`. "Doesn't do": cross-process
   messaging, an outbox (until a consumer needs one), retries (`jobs`), the start-up graph
   check (`module`). Drop "delivery inside a transaction or after commit (decided in the
   spec)": it is after commit.
5. **`plan/design/app.md` (row 18)** (applied 2026-10-17, by app.md decision 10; one insertion: the runnables stop after the HTTP server drained and before `signals.Close`): `serve` builds `signals.New(Options{Logger:
   tel.Logger().With("component", "signals"), Tracer: tel.Tracer("signals"), Meter:
   tel.Meter("signals"), Clock: clock.Real(), HandlerTimeout: 5 * time.Second, Deferrer:
   <jobs, M4>})` after `db.Open` and before `Build`; `Provision` scopes it per module;
   each core publisher gets its scope (`people`, `departments`, `events`, as their specs
   define); `Env.CoreSignals` lists the core's names; shutdown order: HTTP server drained →
   runnables (jobs, M4) → `signals.Close` → `Set.Stop` → `db.Close` → `telemetry.Close`, all within
   `http.shutdown_timeout`.
6. **`plan/design/jobs.md` (row 14)** (pending: target not written): implement `signals.Deferrer` (store the `Delivery`
   on the writing copy; the job name `signals.redeliver`); run `Dispatcher.Redeliver` with
   backoff from `retry.Policy`, `Attempt` incremented per try, on the writer only; treat
   `NotFound` from `Redeliver` as "drop with a log line" and `errors.Is(err,
   db.ErrReadOnly)` as "stop until the next probe" (db.md Proposed change 13); state the
   give-up count; a handler is called again with the same `Signal.ID`, so the module's spec
   says how its handler is idempotent.
7. **`plan/design/authz.md` (row 7)** (applied 2026-10-18, by authz.md Proposed change 8: `Unauthenticated` inside `InDelivery`) **and `identity.md` (row 6)** (applied 2026-10-18, by identity.md Proposed change 12 and `PersonFrom` returning false inside `signals.InDelivery`): a handler's ctx may carry
   the request's session value (signals decision 6); `authz` must not treat it as the actor
   when `signals.InDelivery(ctx)` is true (the decision there is `Permission` or an explicit
   "system" path the module's spec names), and `identity` should document that the
   session value is a request-scoped value only.
8. **`plan/design/people.md` (row 8)** (applied 2026-10-18, by people.md Proposed change 6) **and `departments-events.md` (row 9)** (applied 2026-10-18, D37: the seven names and payload keys in `Env.CoreSignals`, scopes `departments` and `events`): declare their
   signal names and payload keys in a "Signals" table (name, when, keys and ID types):
   `people.person-created {person}`, `people.person-deleted {person}` (published after the
   deletion across modules commits), and the department and event signals; obtain a scoped
   `Bus` from `app`; put the names in `Env.CoreSignals`.
9. **`plan/09-core-features.md` §1 "Settled here"** (applied 2026-10-16): "with the subscriber's failures logged
   and retried by `jobs`" becomes "with the subscriber's failures logged and counted, and,
   from M4, retryable ones handed to `jobs`; nothing is durable before delivery (signals.md
   decisions 2 and 3)".
10. **`.claude/skills/add-module/SKILL.md` and `go-conventions`** (applied 2026-10-16): in §1, "the signals it
    publishes and subscribes to (payloads carry IDs, not personal data)" becomes "a table
    per published signal: name, when it is published, payload keys and their ID types
    (`signals.Payload` holds record references only); for each subscription, what the
    handler does, that it runs as the system, how it is idempotent on `Signal.ID`, and what
    happens when it fails"; in §2, `signals.go` holds the key constants, the
    `NewPayload(signals.Ref(...))` calls and the handlers; add the rule "publish inside the
    `Transact` that makes the change; subscribe in `Start`; handlers are quick, long work is
    a job"; `go-conventions` adds `signalstest.NewFake` to the testing patterns.
11. **`plan/design/module-hello.md` (row 4b)** (applied 2026-10-17, by module-hello.md's Proposed change 4; the handler counts on the counter goroutine, idempotent on `Signal.ID` with a 256-entry ring): `hello.greeted {person}`,
    published inside the transaction that saves a greeting; its handler for its own signal
    counts the greeting in memory (no write), so the reference shows a subscriber that
    works on a read-only copy; its test uses `signalstest.Fake.Deliver`.
