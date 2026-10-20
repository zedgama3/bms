# jobs

- **Status:** Implemented (2026-10-20); Approved (2026-10-20, D43)
- **Amendment 2026-10-20 (awaiting owner approval):** Q-JOBS6 corrected. Its recommendation said "no change to `audit`", but audit's `resolveActor` accepts only a person or the operator from the `ActorFunc` and answers `Internal` for `ActorSystem`, so the approved wiring would fail every `Record` in a module's job handler. The intent stands (the actor inside a job is the system, through `app`'s `auditActor` over `jobs.InJob`); it needs audit.md's amendment of the same date (the function may answer `ActorSystem`; F13b) and `auditActor` to check `InJob` before the session. Nothing in `jobs` changes. Proposed change 10.
- **Amendment 2026-10-20 (b) (awaiting owner approval):** what the review of the M4 implementation found and the code now does. (1) **Poison rows.** A tick's writer pass begins with a **reclaim** step (2a): lapsed `running` rows go back to `queued` with `last_error = lost_lease`, or to `failed gave_up` once `attempts >= Retry.MaxAttempts`; and the lease's lapsed arm gains `attempts < Retry.MaxAttempts`. Before this a job that killed its process (a fatal in a handler) at its last attempt was re-leased forever, taking the server down once per `Lease`. (2) **No overlap under concurrency.** The "no open row of this name" test in the scheduler's slot insert and in `RunNow` is serialized by a transaction-scoped advisory lock on the name, taken in the same `Write` transaction immediately before the insert; the scheduler takes names in sorted order, so two ticks cannot deadlock. (3) F45 to F50 with their tests (`integration_concurrency_test.go`). (4) Reviewer-accepted deviations: `Runner.Declared()`, exported `MaxNameBytes` and `MinTimeout`, `Status.MarshalText`/`UnmarshalText`, route `GET /` (the shell mounts `{$}`), `Enqueue` runs `Transact(Write)`, the `bms.jobs.queue` gauge reports `queued` and `running` only, `jobstest.Fake.Fail` is one-shot, `Close` also waits for `RunOnce`. (5) Q-JOBS8 to Q-JOBS13 for the owner; Proposed change 11. Each change is marked "(b)" where it sits; `spec-editor` removes the markers on approval.
- **Package:** internal/core/jobs (postgres in `internal/core/jobs/postgres`, fake in `internal/core/jobs/jobstest`)   **UI:** web/src/core/jobs
- **Depends on specs:** `model`, `lib-errs`, `lib-core` (`clock`, `retry`, `validate`), `db`, `signals` (`Delivery`, `Deferrer`, `Payload`, `Dispatcher.Redeliver`), `audit`, `authz` (leaf types, `Authorizer`, `ActorFunc`), `telemetry`, `api` (`httpx`, the route-set shape), `web-shell`, `app` (part 8), `module-contract` (`Deps.Jobs`), `settings` (reads `audit.retention` through `app`'s closure only)   **Used by:** every module (`Deps.Jobs`), `signals` (the `Deferrer`), `audit` (its purge is the first scheduled job), `people` (the retried `identity.account-created` handling), `files` (sibling draft: a sweep of orphaned uploads), `volunteers` (next: shift reminders, qualification-expiry reminders), `app` (part 8, the runnable, `bms check`), `web-shell` (the status screen)

## Summary

**Purpose.** Run work that belongs to no request, when it is due, on the writing copy, with retries. Two shapes: a **scheduled** job (every N minutes or hours; one slot at a time, never overlapping itself) and a **deferred** job (enqueued by a use case inside its own transaction, with record references as arguments and a time to run). Both are rows in one PostgreSQL table leased with `FOR UPDATE SKIP LOCKED`, so several processes on one database share the work and a read-only copy does none of it. Package `internal/core/jobs`; imports `model`, `lib`, `core/db`, `core/signals`, `core/audit`, `core/authz` (leaf types, `Authorizer`), `core/telemetry`. Never `core/settings`, `core/identity`, `core/people`, `core/notify`, `core/module`, `api`, `app`, `config` or a module.

**Names.** `jobs` is the package, the core part (8), the migration set (schema `core`), the route set (`/api/jobs/`), the permission prefix, the audit area, the screen set and the owner of one job name, `jobs.purge`.

**Exported (`jobs`).**
- `Name` (`<owner>.<ident>`, the `signals.Name` grammar, at most 96 bytes; `Validate`, `Owner`); `JobID = model.ID[job]`; `Args = signals.Payload` (record references only, at most `signals.MaxRefs`); `Status` (`Queued`, `Running`, `Done`, `Failed`, `Canceled`; `String`, `ParseStatus`, `MarshalText`/`UnmarshalText` (b): a JSON string and a usable map key).
- `Job{ID, Name, Key string, Args, Attempt int, RunAt time.Time}` (what a handler receives); `Handler func(ctx, Job) error`; `Decl{Name, Every, Timeout time.Duration, Handler}` (+`Validate`; `Every` 0 = on demand only, else `MinEvery`..`MaxEvery`); `Request{Name, Key string, Args, RunAt time.Time}` (+`Validate`; zero `RunAt` = now; `Key` dedupes per `(name, key)`).
- `Registrar{Register(Decl) error; Enqueue(ctx, Request) (JobID, error)}` = `module.Deps.Jobs`, scoped to one owner by `(*Runner).Scoped(owner)`.
- `Redeliverer{Redeliver(ctx, signals.Delivery) error}` (the `*signals.Dispatcher`, through `app`).
- `Record{ID, Name, Key, Status, Attempts int, RunAt, StartedAt, FinishedAt time.Time, LastError, Instance, TraceID string}` (`LogValue` = ID and name); `Registered{Name, Owner string, Every, Timeout time.Duration, Last *Record, NextDue time.Time}`; `StatusView{ReadOnly bool, Registered []Registered, Counts map[Status]int, Recent []Record}`; `Ran{Scheduled, Ran, Failed, Retried, Released int}`.
- `Options{DB, Clock, Logger, Tracer, Meter, Audit audit.Recorder (scoped "jobs"), Authz authz.Authorizer, Actor authz.ActorFunc, Redeliverer, Instance string, PollInterval, Lease, RunTimeout time.Duration, Workers int, Retry retry.Policy}` (+`Validate`); `New(Options) (*Runner, error)` (no I/O; works against `db.None()`).
- `*Runner` (safe for concurrent use): `Scoped(owner) (Registrar, error)`; `Defer(ctx, signals.Delivery) error` (implements `signals.Deferrer`); `Run(ctx) error` (the scheduler tick and the workers, an `app` runnable); `RunOnce(ctx) (Ran, error)`; `Close(ctx) error` (idempotent); `Status(ctx) (StatusView, error)`; `RunNow(ctx, Name) (JobID, error)`; `Retry(ctx, JobID) error`; `Cancel(ctx, JobID) error`; `Routes() []Route`; `OpenAPI() []byte`; `Declared() []Registered` (b: what is registered here, no I/O, no permission; `bms check`). `Route{Method, Pattern string, Handler http.Handler}` (all signed-in; `app` converts). `InJob(ctx) bool` (true inside a handler; `app`'s audit actor answers `ActorSystem`).
- Constants: `MaxNameBytes = 96` (b), `MinTimeout = 1s` (b), `MinEvery = 1m`, `MaxEvery = 168h`, `MaxRunTimeout = 1h`, `MaxDelay = 366 * 24h`, `MaxKeyBytes = 128`, `DefaultPollInterval = 10s`, `DefaultLease = 2m`, `DefaultRunTimeout = 5m`, `DefaultWorkers = 2`, `MaxWorkers = 32`, `DoneTTL = 7 days`, `MaxRecent = 50`, `MaxBatch = 1000`, `NameRedeliver Name = "signals.redeliver"`, `NamePurge Name = "jobs.purge"`; `DefaultRetry = retry.Policy{Initial: 1m, Max: 1h, Multiplier: 2, MaxAttempts: 10}`.
- Permissions `jobs.read` (site, reach `internal`, starter roles `authz.SiteAdminRoles`), `jobs.manage` (site, `internal`, starter `settings-admin`); `Permissions []authz.PermissionDecl`. No signals, no settings. Audit actions `jobs.job.run`, `jobs.job.retried`, `jobs.job.canceled`. `postgres.Migrations fs.FS`; `Set = db.Set{Name: "jobs", Schema: "core"}`.
- `jobstest.NewFake(owner) *Fake` (a `Registrar` that validates like the real one: `Registered() []Decl`, `Enqueued() []Request` recorded after commit, `Run(ctx, Name, Job) error` drives a registered handler with `InJob` true, `Fail(err)` makes the next `Enqueue` fail once (b)).

**Key decisions.** 1 one table, one mechanism: a scheduled job's due slot is itself a row (`key` = the slot time), so leasing, retries, the fence, the screen and the dedupe index serve both shapes; two schedulers on one database insert the same slot once. 2 at-least-once, never exactly-once: a lease with a heartbeat (`Lease`, renewed at a third of it) hands a crashed process's job to another within one lease; a handler is idempotent on `Job.ID` or on what its `Args` name, and re-reads current state rather than trusting the arguments. 3 `Enqueue` joins the caller's transaction (as `notify.Send` does): the row commits or rolls back with the change; a waiting row with the same `(name, key)` is moved to the new `RunAt` and `Args`, a running or finished one makes the request a no-op. 4 only the writing copy runs anything: the loop skips every tick while `db.ReadOnly()`, a run that meets `db.ErrReadOnly` releases its row and stops until the next probe, `Enqueue` and `Defer` fail `Unavailable` wrapping `db.ErrReadOnly` (db.md Proposed change 13). 5 arguments are record references only (`signals.Payload`), so no personal data sits in the queue, in a log line or on a span; a handler runs as the system (no actor, no session, `InJob` true) and calls system paths, as a signal handler does. 6 retries by kind: `Unavailable` and `Timeout` retry with `DefaultRetry` (ten attempts, 1 m doubling to 1 h, about four hours in all), `Canceled` while ctx is done releases the row without counting an attempt, every other kind and a panic fail the job at once (a bug is not retried); an admin may `Retry` a failed job within `DoneTTL`. 7 scheduled jobs never overlap themselves (a slot is inserted only while no row of that name waits or runs) and never catch up (at most the current slot; a process down for a week runs a purge once, not 168 times). 8 a row whose name no handler in this process knows (a module disabled since) fails `no_handler` with one log line, retryable once the module is back. 9 `signals.redeliver` is a job like any other: `Defer` stores the `Delivery` with `RunAt` = now + backoff for the next attempt and key `<signal id>:<subscriber>`; its handler calls `Redeliverer.Redeliver` with `Attempt` incremented, drops `NotFound` with a log line, and gives up after `DefaultRetry.MaxAttempts` counting the first delivery. 10 notify keeps its own sender loop (notify decision 2; 09 §6 "jobs may host the tick later"): nothing moves in M4. 11 the first scheduled jobs are `audit.purge` (hourly; `app` registers the closure over `audit.Log.Purge` and the `audit` settings reader, skipping when `audit.retention` is 0) and `jobs.purge` (hourly; finished rows older than `DoneTTL`). 12 no manifest field: `Deps.Jobs` is scoped by name prefix, `Register` in `Start` is the declaration, and nothing crosses modules, so the graph check has nothing to check. 13 `Every` is the only schedule form in M4 (`now.Truncate(Every)` in UTC is the slot); a daily time in `regional.timezone` is Later (Q-JOBS4). 14 regional differences: none here; what runs is the modules' retention settings and the regional's data. 15 the status screen and the three admin actions ship in M4 (Q-JOBS1). 16 (b) a lapsed lease is settled by a **reclaim** step at the start of every writer pass, not only by the next lease: back to `queued` (`lost_lease`, the attempt stays counted) while attempts remain, `failed gave_up` once `attempts >= Retry.MaxAttempts`; the lease's lapsed arm requires `attempts < Retry.MaxAttempts` too, so a row that kills its process at its last attempt is never leased again. 17 (b) the no-overlap test (decision 7, and `RunNow`'s F25) is serialized by `pg_advisory_xact_lock(hashtextextended('core.job:' || name, 0))`, taken in the same `Write` transaction immediately before the slot insert or `RunNow`'s insert; under `READ COMMITTED` a `NOT EXISTS` cannot see another transaction's uncommitted row, and the lock makes the second insert start after the first committed; the scheduler locks names in sorted order (no deadlock); nothing else takes it.

**Errs kinds callers handle.** `Register`: `Invalid` (name, `scope` (another owner), nil handler, `every`/`timeout` `range`), `Conflict` (name registered twice), `Internal` (after `Run`). `Enqueue`: `Invalid` (name, `scope`, `unknown` (no handler registered), `key` `malformed`/`too_long`, `args`, `run_at` `range`), `Unavailable` wrapping `db.ErrReadOnly`, store kinds (`Unavailable`, `Timeout`, `Canceled`). `Defer`: `Invalid` (delivery), `Unavailable` wrapping `db.ErrReadOnly`, store kinds, `Unavailable` after `Close`. `RunOnce`: `Unavailable` wrapping `db.ErrReadOnly` (nothing leased), store kinds. Admin methods: `Unauthenticated`, `Permission`, `NotFound` (job, name), `Invalid` (`RunNow` of an on-demand name), `Conflict` (`status`: retry of a row not `Failed`; cancel of a row not `Queued`; run-now while a row of that name waits or runs), `Unavailable` wrapping `db.ErrReadOnly`. `Run`: `Internal` (twice). `New`/`Options.Validate`: `Invalid` with fields `jobs.db`, `jobs.clock`, `jobs.logger`, `jobs.tracer`, `jobs.meter`, `jobs.audit`, `jobs.authz`, `jobs.actor`, `jobs.redeliverer`, `jobs.instance`, `jobs.poll_interval`, `jobs.lease`, `jobs.run_timeout`, `jobs.workers`, `jobs.retry`. No sentinels of its own.

**Owns.** Table `core.job` (no personal data; `args` holds record references only; `internal`); the two permissions; the three audit actions; names `jobs.purge` and `signals.redeliver`; route set `jobs`: `GET /` (b: `Routes()` says `/`; `app` converts and the api shell mounts it as `{$}`), `POST /registered/{name}/run`, `POST /queue/{job}/retry`, `POST /queue/{job}/cancel`; screen `/jobs`; keys `core.jobs.*`; spans `jobs.tick`, `jobs.enqueue` (Producer), `jobs.defer` (Producer), `jobs.run` (Consumer), `jobs.<use-case>`; metrics `bms.jobs.runs{job, module, outcome}`, `bms.jobs.queue{status}` (gauge), `bms.jobs.lag` (histogram, seconds from `run_at` to start).

**Expects of dependencies.** `db`: `Transact`, `TxMode`, `ReadOnly`, `ErrReadOnly`, `Wrap`, `*Error.Constraint`, `Set`, `None`, `dbtest`; `jobs` is already in the fixed set order. `signals`: `Delivery` (+`Validate`), `Deferrer`, `Payload` (+`Validate`, JSON round trip), `Dispatcher.Redeliver` (`NotFound` for no handler, the handler's error unchanged, honours cancellation), `InDelivery`. `audit`: a `Recorder` scoped `jobs`; the `ActorFunc` may answer `ActorSystem` (audit amendment 2026-10-20, awaiting approval), or every `Record` from a module's handler is `Internal`. `authz`: `Allow`, `SiteAdminRoles`. `telemetry`: typed attributes, `TraceIDFrom`, `telemetrytest`; the metric rows (Proposed change 5). `module-contract`: `Deps.Jobs jobs.Registrar` scoped by name; `jobstest.NewFake` in `Conformance`; `jobs` a reserved core area. `app`: part 8 (declare, build, link, the runnable, `lazyDeferrer`, the `audit.purge` closure, `auditActor` answering `ActorSystem` when `InJob`); `bms check` prints the registered jobs. `web-shell`: `web/src/core/jobs/` as a core area.

**Open questions:** Q-JOBS1 to Q-JOBS7 below (the screen in M4, the give-up policy, notify's tick staying put, daily schedules Later, `DoneTTL` a constant, the audit actor through `InJob`, no new config keys); Q-JOBS8 to Q-JOBS13 (b, from the review: `Retry` while a row of the name is open, a run past its fence, `bms check` and a failing `Start`, counting a reclaim, the fake's clock, no person ID in a dedupe key). **Proposed changes elsewhere:** 9, listed at the end, all applied 2026-10-20 (D43); 10 and 11 await approval.

## Purpose

Run work that belongs to no request, when it is due, on the writing copy, with retries.

## Responsibilities

- Hold a durable queue of work in PostgreSQL (`core.job`): scheduled slots and deferred jobs, each with a name, optional dedupe key, record-reference arguments, a due time, attempts and a lease.
- Let a module or core area register, in `Start`, the handlers for its own job names and the schedule of the ones that run on their own.
- Let a use case enqueue a deferred job inside its own transaction, so a sign-up and its reminder agree.
- Lease due rows with `FOR UPDATE SKIP LOCKED`, run each handler once per lease under a deadline, with a heartbeat on the lease, and record the outcome on the row.
- Retry retryable failures with backoff and give up after a fixed count; release a row on shutdown without counting the attempt.
- Run nothing on a read-only copy; stop a run that meets `db.ErrReadOnly` until the next probe.
- Implement `signals.Deferrer`: store a failed delivery and hand it back to `Dispatcher.Redeliver` with backoff, on the writer only.
- Purge its own finished rows after `DoneTTL`.
- Show an admin what is registered, what ran last, what waits and what failed, and let a holder of `jobs.manage` run a scheduled job now, retry a failed job or cancel a waiting one.
- Keep arguments, logs, spans and metrics free of personal data (record references, names, times and tokens only).

## Not responsible for

| Concern | Owner |
|---|---|
| Deciding what work exists, what it does and whether it is still wanted when it runs | the registering module or core area (a handler re-reads current state) |
| Sending e-mail; the outbox, its sender loop, its retries and its purge | `notify` (notify decision 2; 09 §6: the tick may move here Later) |
| Retention periods and the purge of each area's data | the area that declares `<name>.retention*` and registers the job (D39); `audit.Log.Purge` is the first |
| A person's export and deletion | `people` and each module; `core.job` holds no personal data (decision 5) |
| Delivering a signal the first time; what a handler does | `signals`; the subscriber |
| Cross-process messaging, fan-out, priorities, cron expressions, a daily time in the regional's zone | not in M4 (Q-JOBS4); a broker is never required (principle 6) |
| Running on a schedule tied to an event's dates (a reminder N hours before a shift) | the module enqueues a deferred job with `RunAt` from the record (volunteers) |
| Process configuration and the read-only probe | `config`, `db` |
| The HTTP pipeline and error mapping | `api` |
| Authorization rules | `authz`; this spec only names its permissions |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `Registrar` | let one owner declare its job handlers and enqueue its deferred jobs | = `Deps.Jobs`; scoped by name prefix; `Register` is no-I/O |
| the queue (`core.job`) | hold every job, scheduled slot and deferred alike, with its lease and outcome | one table; `(name, key)` unique; no personal data |
| the scheduler | insert the current slot of every scheduled name that has no row waiting or running | runs on each tick, writer only; never catches up |
| the workers | lease due rows one at a time, run the handler under a deadline with a heartbeat, record the outcome | `Workers` per process; at-least-once |
| the redeliverer | store a failed signal delivery and give it back to `Dispatcher.Redeliver` with backoff | `Defer` + the `signals.redeliver` handler |
| the status screen | show what is registered, what ran, what waits; run now, retry, cancel | `/jobs`; `jobs.read`, `jobs.manage` |

## Interface

```go
package jobs

// ---- Names, arguments, jobs ----

// Name is a job's name, "<owner>.<ident>": the owner is a module name or a core area, the
// ident [a-z][a-z0-9]*(-[a-z0-9]+)*; at most 96 bytes (the signals.Name grammar). A
// scoped Registrar accepts only names whose Owner is its owner.
type Name string

// Validate fails with errs.Invalid (field "name": required, malformed, too_long) (F1).
func (n Name) Validate() error

// Owner returns the part before the first dot, "" for an invalid name. Cannot fail.
func (n Name) Owner() string

// JobID identifies one row of core.job.
type JobID = model.ID[job]

type job struct{}

// Args are a job's arguments: record references only, as a signal's payload (signals
// decision 4), so nothing personal is ever queued, logged or shown. A handler reads the
// records the references name; it never trusts the arguments to be current.
type Args = signals.Payload

// Status is where a row is. The zero value is invalid.
type Status uint8

const (
	Queued   Status = iota + 1 // waiting for run_at
	Running                    // leased by a process
	Done                       // the handler returned nil
	Failed                     // the handler failed finally, or gave up, or has no handler
	Canceled                   // an admin canceled it while it waited
)

func (s Status) String() string            // "queued" | "running" | "done" | "failed" | "canceled"
func ParseStatus(s string) (Status, error) // Invalid otherwise

// (b) MarshalText makes a Status a JSON string and a usable map key (StatusView.Counts on the
// wire); UnmarshalText parses one. Both fail errs.Invalid (field "status", one_of) for a value
// that is not a status (F50).
func (s Status) MarshalText() ([]byte, error)
func (s *Status) UnmarshalText(text []byte) error

// Job is what a handler receives. Attempt is 1 for the first run of this row.
type Job struct {
	ID      JobID
	Name    Name
	Key     string // "" when the row has no dedupe key
	Args    Args
	Attempt int
	RunAt   time.Time
}

// Handler does one job's work. It runs on a worker goroutine of the writing copy, as the
// system (no session, no actor; InJob(ctx) is true; identity.PersonFrom is false), with a
// ctx that carries no transaction, a deadline of Decl.Timeout (default Options.RunTimeout)
// and the runner's cancellation (shutdown). It must be idempotent: the same row runs again
// after a crash, a lost lease, or a retry (decision 2). Its error decides what happens
// next (Behavior, "Outcome by kind"): Unavailable and Timeout retry with backoff; Canceled
// while ctx is done releases the row; db.ErrReadOnly releases it and pauses the runner;
// anything else fails the job. A panic is recovered, logged once with its stack, counted
// and fails the job. Nothing personal may be logged from a handler (go-conventions).
type Handler func(ctx context.Context, j Job) error

// Decl registers one job name with its handler.
type Decl struct {
	Name    Name
	Every   time.Duration // 0: on demand only (Enqueue, RunNow refused); else MinEvery..MaxEvery
	Timeout time.Duration // 0: Options.RunTimeout; else MinTimeout..MaxRunTimeout
	Handler Handler
}

// Validate fails with errs.Invalid joining a validate.FieldError per problem: name (F1),
// every (range), timeout (range), handler (required) (F2).
func (d Decl) Validate() error

// Request enqueues one deferred job.
type Request struct {
	Name  Name
	Key   string    // optional dedupe key, [A-Za-z0-9._:-]{1,MaxKeyBytes}; "" = none
	Args  Args      // may be empty
	RunAt time.Time // zero = now; else at most MaxDelay ahead (the past is "now")
}

// Validate fails with errs.Invalid joining a validate.FieldError per problem: name, key
// (malformed, too_long), args (signals.Payload.Validate), run_at (range) (F3).
func (r Request) Validate() error

// ---- Registrar (Deps.Jobs) ----

// Registrar is what one owner holds: Deps.Jobs for a module, Scoped(area) for a core
// area. Safe for concurrent use.
type Registrar interface {
	// Register declares a job name and its handler, and, when d.Every > 0, that the
	// runner inserts its slot every d.Every. Called from Module.Start (no I/O, works on a
	// read-only copy); a name may be registered once per process. Fails errs.Invalid for
	// an invalid Decl (F2) or a Name whose Owner is not this scope's owner (F4, field
	// "name", code "scope"), errs.Conflict when the name is already registered (F5), and
	// errs.Internal after Run began (F6).
	// Consumers: every module with background work (volunteers: shift reminders,
	// qualification-expiry reminders; files: the orphan sweep); app for audit.purge and
	// jobs.purge; the runner itself for signals.redeliver.
	Register(d Decl) error

	// Enqueue stores one deferred job to run at r.RunAt. It runs its statements under
	// db.Transact(ctx, db.Write) (b): inside the caller's Write transaction that joins it, so
	// the row commits or rolls back with the caller's change; outside any transaction it is
	// one short transaction of its own; inside a Read transaction db refuses the nesting with
	// errs.Invalid (F42). With a Key: a Queued row with the same (name, key) is
	// moved to r.RunAt with r.Args and its ID returned (a reschedule); a Running, Done,
	// Failed or Canceled row with that key leaves the request a no-op and its ID is
	// returned (the caller keys a distinct run distinctly, for example by the shift's
	// start time). Fails errs.Invalid for an invalid Request (F3), a name outside the
	// scope (F4) or a name no handler was registered for in this process (F7, code
	// "unknown"); errs.Unavailable wrapping db.ErrReadOnly on a read-only copy (F8); store
	// kinds (F9). It checks no permission: the caller's use case is the authority
	// (decision 5).
	// Consumers: volunteers (a reminder when a sign-up is confirmed), any module with
	// work to do later; the runner's RunNow.
	Enqueue(ctx context.Context, r Request) (JobID, error)
}

// InJob reports whether ctx is a handler's ctx. app's audit ActorFunc answers ActorSystem
// when it is true; authz has no actor and refuses permission-checked use cases, as inside a
// signal delivery. Cannot fail.
func InJob(ctx context.Context) bool

// ---- The runner ----

// Redeliverer hands a stored delivery back to the subscriber: the *signals.Dispatcher.
type Redeliverer interface {
	Redeliver(ctx context.Context, d signals.Delivery) error
}

// Options configure the runner. Zero durations and counts take the defaults.
type Options struct {
	DB     db.DB
	Clock  clock.Clock
	Logger *slog.Logger
	Tracer telemetry.Tracer
	Meter  telemetry.Meter

	Audit audit.Recorder   // scoped "jobs"
	Authz authz.Authorizer // the admin use cases' permission checks
	Actor authz.ActorFunc  // the session's person, for the admin use cases

	Redeliverer Redeliverer // the dispatcher; required

	Instance     string        // config Instance.Name; recorded on each run
	PollInterval time.Duration // DefaultPollInterval; 1s..5m
	Lease        time.Duration // DefaultLease; 30s..10m; renewed every Lease/3
	RunTimeout   time.Duration // DefaultRunTimeout; MinTimeout..MaxRunTimeout
	Workers      int           // DefaultWorkers; 1..MaxWorkers
	Retry        retry.Policy  // zero = DefaultRetry; MaxAttempts 1..100
}

// Validate fails with errs.Invalid joining one validate.FieldError per problem, fields
// jobs.db, jobs.clock, jobs.logger, jobs.tracer, jobs.meter, jobs.audit, jobs.authz,
// jobs.actor, jobs.redeliverer, jobs.instance, jobs.poll_interval, jobs.lease,
// jobs.run_timeout, jobs.workers, jobs.retry (F10).
func (o Options) Validate() error

// New builds the runner. No I/O: it works against db.None() and on a read-only copy. It
// registers its own jobs.purge and signals.redeliver. Fails errs.Invalid (F10).
// Consumer: app (part 8).
func New(o Options) (*Runner, error)

// Runner is the service. Safe for concurrent use.
type Runner struct{ /* unexported */ }

// Scoped returns the Registrar for owner (a module name or core area, the module-name
// grammar). A view: nothing to close. Fails errs.Invalid for a bad owner (F11),
// errs.Internal after Close (F12).
// Consumers: app (Provision: Deps.Jobs; the audit area for audit.purge).
func (r *Runner) Scoped(owner string) (Registrar, error)

// Defer implements signals.Deferrer: it stores d as a signals.redeliver job with key
// "<signal id>:<subscriber>", RunAt = now + Retry.Delay(d.Attempt + 1, u), and returns
// without delivering anything. It is quick (one insert) and never blocks on a worker.
// Fails errs.Invalid for an invalid d (F13); errs.Unavailable wrapping db.ErrReadOnly on
// a read-only copy (F14; signals logs it beside the handler's error, the delivery is
// final); store kinds; errs.Unavailable after Close (F15). A repeated Defer of the same
// (signal, subscriber) is a no-op (decision 3).
// Consumer: signals (Options.Deferrer, through app's lazyDeferrer).
func (r *Runner) Defer(ctx context.Context, d signals.Delivery) error

// Run is the loop: at once and then every PollInterval, when the database is writable,
// one tick (the scheduler inserts due slots; the workers lease and run due rows until none
// is due or Workers are busy). It freezes Register. It returns nil when ctx ends or after
// Close (running handlers get Canceled and their rows are released); errs.Internal when
// called twice (F16). A failing tick is logged once per change of state and retried next
// tick; a read-only copy is logged once when it flips and skipped.
// Consumer: app (the runnable, started after Set.Start).
func (r *Runner) Run(ctx context.Context) error

// RunOnce does one tick and waits for the runs it started, returning counts. Fails
// errs.Unavailable wrapping db.ErrReadOnly when the copy is read-only (nothing leased,
// F17), store kinds (F18), errs.Canceled when ctx ends mid-tick (rows released).
// Consumers: tests; bms check (reports, does not run).
func (r *Runner) RunOnce(ctx context.Context) (Ran, error)

// Close stops Run, cancels the runs in flight (their rows are released within
// releaseTimeout) and waits for them under ctx; it also waits for any RunOnce in flight
// (b), so a test or bms check that is mid-tick is never left writing to a closed runner.
// Idempotent. Fails with ctx's kind when the workers are still returning when ctx ends
// (F19).
// Consumer: app (the runnable's stop, after HTTP drained, before signals.Close).
func (r *Runner) Close(ctx context.Context) error

// (b) Declared returns what is registered in this process, sorted by name, with no I/O and
// no permission check: Owner, Every and the effective Timeout are filled; Last is nil and
// NextDue is zero (those need the database). Cannot fail: it reads the registry only.
// Consumer: bms check (prints the "jobs" section against db.None(); Status would need a
// session and a database, RunOnce would run things).
func (r *Runner) Declared() []Registered

// Status returns what is registered in this process with each name's last row and next
// due slot, the queue counts, and the MaxRecent most recent Failed and Queued rows. Works
// on a read-only copy. Needs jobs.read. Fails errs.Unauthenticated (F20), errs.Permission
// (F21), store kinds.
// Consumer: the status screen (GET /api/jobs/).
func (r *Runner) Status(ctx context.Context) (StatusView, error)

// RunNow enqueues a run of a scheduled name at once (key "admin:<job id>") and records
// jobs.job.run. Needs jobs.manage. Fails errs.Unauthenticated, errs.Permission (F22),
// errs.NotFound for a name not registered here (F23), errs.Invalid for an on-demand name
// (F24, field "name", code "on_demand"), errs.Conflict (code "running") while a row of that
// name is Queued or Running (F25), errs.Unavailable wrapping db.ErrReadOnly (F26).
// Consumer: the status screen.
func (r *Runner) RunNow(ctx context.Context, name Name) (JobID, error)

// Retry puts a Failed row back to Queued with run_at = now and attempts = 0, recording
// jobs.job.retried. Needs jobs.manage. Fails errs.Unauthenticated, errs.Permission,
// errs.NotFound (F27), errs.Conflict (code "status") for a row not Failed (F28),
// errs.Unavailable wrapping db.ErrReadOnly.
// Consumer: the status screen.
func (r *Runner) Retry(ctx context.Context, id JobID) error

// Cancel marks a Queued row Canceled, recording jobs.job.canceled. Needs jobs.manage.
// Fails errs.Unauthenticated, errs.Permission, errs.NotFound, errs.Conflict (code
// "status") for a row not Queued (F29), errs.Unavailable wrapping db.ErrReadOnly.
// Consumer: the status screen.
func (r *Runner) Cancel(ctx context.Context, id JobID) error

// Routes returns the route set (relative to /api/jobs: "GET /", the three POSTs; the api
// shell mounts a trailing "/" as "{$}" (b)); OpenAPI its fragment. Cannot fail.
// Consumer: app (converts to module.Route, all signed-in).
func (r *Runner) Routes() []Route
func (r *Runner) OpenAPI() []byte

// Route is the shape app converts (jobs cannot import core/module; audit decision 14).
type Route struct {
	Method, Pattern string
	Handler         http.Handler
}

// ---- Views ----

// Record is one row as the screen sees it. LogValue emits ID and name only.
type Record struct {
	ID         JobID
	Name       Name
	Key        string
	Status     Status
	Attempts   int
	RunAt      time.Time
	StartedAt  time.Time // zero until first leased
	FinishedAt time.Time // zero until final
	LastError  string    // a token: the errs kind in lower case, or panic, no_handler, lost_lease, gave_up
	Instance   string    // the copy that ran it last
	TraceID    string    // the enqueuer's trace, "" when none
}

// Registered is one name known to this process.
type Registered struct {
	Name    Name
	Owner   string
	Every   time.Duration // 0 for on demand
	Timeout time.Duration
	Last    *Record   // the newest row of this name, nil when none
	NextDue time.Time // the next slot, zero for on demand
}

// StatusView is what GET /api/jobs/ returns.
type StatusView struct {
	ReadOnly   bool
	Registered []Registered
	Counts     map[Status]int
	Recent     []Record // Failed and Queued rows, newest first, at most MaxRecent
}

// Ran counts one tick.
type Ran struct {
	Scheduled int // slots inserted
	Ran       int // handlers that returned nil
	Failed    int // final failures (including no_handler and gave_up)
	Retried   int // rows re-queued with backoff
	Released  int // rows given back (Canceled, read-only, lost lease)
}

// ---- Constants ----

const (
	MaxNameBytes        = 96          // (b) exported: the Name grammar's byte limit (signals.Name)
	MinTimeout          = time.Second // (b) exported: the floor of Decl.Timeout and Options.RunTimeout
	MinEvery            = time.Minute
	MaxEvery            = 168 * time.Hour
	MaxRunTimeout       = time.Hour
	MaxDelay            = 366 * 24 * time.Hour
	MaxKeyBytes         = 128
	DefaultPollInterval = 10 * time.Second
	DefaultLease        = 2 * time.Minute
	DefaultRunTimeout   = 5 * time.Minute
	DefaultWorkers      = 2
	MaxWorkers          = 32
	DoneTTL             = 7 * 24 * time.Hour
	MaxRecent           = 50
	MaxBatch            = 1000

	NameRedeliver Name = "signals.redeliver"
	NamePurge     Name = "jobs.purge"

	PermissionRead   = "jobs.read"
	PermissionManage = "jobs.manage"
)

// DefaultRetry is the policy for Unavailable and Timeout: ten attempts in all, waits of
// about 1, 2, 4, 8, 16, 32, 60, 60, 60 minutes with full jitter (about four hours).
var DefaultRetry = retry.Policy{Initial: time.Minute, Max: time.Hour, Multiplier: 2, MaxAttempts: 10}

var Permissions []authz.PermissionDecl // jobs.read, jobs.manage
```

```go
package jobstest

// Fake is a Registrar for tests: it validates like the real one (names, scope, Decl,
// Request), records registrations, records enqueues after commit (inside a dbtest.Fake
// transaction the request appears only once the transaction commits) and runs handlers on
// demand. Safe for concurrent use.
type Fake struct{ /* unexported */ }

func NewFake(owner string) *Fake
func (f *Fake) Register(d Decl) error
func (f *Fake) Enqueue(ctx context.Context, r Request) (JobID, error)
func (f *Fake) Registered() []Decl
func (f *Fake) Enqueued() []Request
// Run calls the handler registered for name with j (InJob true, no transaction) and
// returns its error unchanged; errs.NotFound when none is registered.
func (f *Fake) Run(ctx context.Context, name Name, j Job) error
// Fail makes the next Enqueue return err, once (b): the call after that succeeds again
// without a second Fail; nil clears a pending failure. One-shot because a use case under
// test enqueues once and the test then asserts the rollback; a sticky failure would hide a
// second enqueue in the same test.
func (f *Fake) Fail(err error)
```

### Method, consumer, pre- and post-conditions

| Method | Consumer | Precondition | Postcondition |
|---|---|---|---|
| `Register` | modules in `Start`; `app` for `audit.purge`; the runner for its own two | before `Run`; valid `Decl`; name in scope | the name has a handler in this process; a scheduled name gets a slot each `Every` |
| `Enqueue` | modules' use cases; `RunNow` | a handler registered for the name here; valid `Request` | a `Queued` row exists (new or moved) that commits with the caller |
| `Defer` | `signals` | valid `Delivery`; writer | a `signals.redeliver` row waits with backoff |
| `Run`/`Close` | `app` | once | due rows run on the writer; stopped rows are released |
| `RunOnce` | tests, `bms check` | writer | one tick done, counts returned |
| `Status` | the screen | `jobs.read` | a consistent view (one `Read` transaction) |
| `RunNow`, `Retry`, `Cancel` | the screen | `jobs.manage`; the row's status as required | the row changed; one audit entry in the same `Write` transaction |
| `Scoped` | `app` | valid owner | a view accepting that owner's names |
| `InJob` | `app`'s audit actor, modules' handlers | — | true inside a handler |
| `Declared` (b) | `bms check` | — | the registered names with owner and schedule, no I/O, no permission |

## Types

Defined here: `Name`, `JobID`, `Status`, `Job`, `Handler`, `Decl`, `Request`, `Registrar`, `Redeliverer`, `Options`, `Runner`, `Route`, `Record`, `Registered`, `StatusView`, `Ran`. Aliased: `Args = signals.Payload`. Uses `model.ID`, `signals.Delivery`, `signals.Signal`, `retry.Policy`, `authz.Permission`, `authz.PermissionDecl`, `audit.Entry`.

Record types shared by the service and `postgres/` live in the leaf package `internal/core/jobs/rows` (`rows.Job`, `rows.Lease`), importing only `model` and `lib`, as `people/rows` and `authz/rows` do.

## Data

Schema `core`, migration set `jobs` (`db.Set{Name: "jobs", Schema: "core"}`), file `0001_job.sql`. `jobs` sits in `coreSetOrder` after `settings` and before `notify` already. Timestamps `timestamptz`, from the service's clock (no defaults).

**`core.job`**

| Column | Type | Meaning |
|---|---|---|
| `id` | uuid PK | `JobID`; never the nil UUID (check) |
| `name` | text | `Name`; check: the grammar, <= 96 bytes |
| `key` | text NULL | the dedupe key; check `^[A-Za-z0-9._:-]{1,128}$`; a schedule slot stores the slot time in RFC 3339 UTC, an admin run `admin:<job id>`, a redelivery `<signal id>:<subscriber>` |
| `args` | jsonb NOT NULL DEFAULT `'{}'` | the JSON form of `Args` (`{"<ident>": "<uuid>", ...}`), or, for `signals.redeliver`, `{"delivery": {"signal": {"id", "name", "at", "payload"}, "subscriber", "attempt"}}`; identifiers, names and times only; check `jsonb_typeof(args) = 'object'` |
| `status` | text | `queued`, `running`, `done`, `failed`, `canceled` (check) |
| `attempts` | integer NOT NULL DEFAULT 0 | leases counted so far (a release takes one back); check `>= 0` |
| `run_at` | timestamptz | due time |
| `leased_until` | timestamptz NULL | set iff `status = 'running'` (check); the fence for every write by a worker |
| `started_at` | timestamptz NULL | first lease |
| `finished_at` | timestamptz NULL | set iff status is final (check) |
| `last_error` | text NULL | a token `^[a-z_]{1,32}$` (check): the errs kind in lower case, `panic`, `no_handler`, `lost_lease`, `gave_up`; never a message |
| `instance` | text NULL | the copy that leased it last (config's instance grammar, <= 64 bytes) |
| `trace_id` | text NULL | `telemetry.TraceIDFrom` at enqueue, 32 lower-case hex (check) |
| `created_at`, `updated_at` | timestamptz | every state change bumps `updated_at` |

Indexes: `job_name_key_uidx UNIQUE (name, key) WHERE key IS NOT NULL` (the dedupe; `Enqueue` and the scheduler use `ON CONFLICT`); `job_due_idx (run_at, id) WHERE status IN ('queued', 'running')` (the lease); `job_name_open_idx (name) WHERE status IN ('queued', 'running')` (the no-overlap test and `RunNow`'s conflict); `job_finished_idx (finished_at) WHERE status IN ('done', 'failed', 'canceled')` (the purge); `job_name_created_idx (name, created_at DESC)` (`Status`'s last row per name).

**Personal data: none.** `args` holds record references, which may include a person's ID as a signal payload does (signals decision 4); the row is `internal`, carries no name, address or narrative, and a deleted person's ID may linger in a finished row for at most `DoneTTL` (pseudonymous, as audit rows are; audit decision 10). There is no foreign key to `core.person`: a job about a deleted person runs, finds nothing, and ends `Done`. `jobs` is therefore not a `people.Source`. Retention: final rows are deleted by `jobs.purge` after `DoneTTL` (a constant, Q-JOBS5); waiting rows live until they run or are canceled.

## Permissions

| Permission | Allows | Scoped by | Reaches | Default roles |
|---|---|---|---|---|
| `jobs.read` | `GET /api/jobs/`: the registered names, last runs, counts and recent failed or waiting rows | site | `internal` | `authz.SiteAdminRoles` (every site admin starter role) |
| `jobs.manage` | `RunNow`, `Retry`, `Cancel` | site | `internal` | `settings-admin` |

`Register`, `Enqueue`, `Defer` and the runs check no permission: they are the system's, and the caller's use case was already authorized (decision 5; notify decision 12 is the precedent). Reads of `core.job` by the runner are system reads of non-personal data: not audited. The three admin actions are audited (`jobs.job.run|retried|canceled`, `Record` = `Ref{Kind: "jobs.job", ID}`, `Reason` = the job name) inside their `Write` transaction. `Status` is not audited (nothing personal).

## Signals

Published: none (a job's completion is not a domain event; a module that wants to announce what its job did publishes its own signal from the handler, inside the handler's transaction).

Subscribed: none. The runner reacts to deliveries only through `signals.Deferrer` (decision 9).

## Settings

None. Nothing a regional would set differently lives here: what runs is each area's own setting (`audit.retention`, a module's `<name>.retention*`, a reminder lead time in volunteers) read by that area's handler at run time. `PollInterval`, `Lease`, `RunTimeout`, `Workers` and `Retry` are `Options` filled by `app` from constants in M4 (Q-JOBS7); `DoneTTL` is a constant (Q-JOBS5).

## UI

Core area `jobs`, screen `/jobs` ("Background work"), in the admin navigation for holders of `jobs.read`; phone-first like every screen. Catalogue keys `core.jobs.*`.

- **Registered** (a card per name, grouped by owner): the owner's title (module or core area catalogue key), the name's ident, "every N" or "on demand", the last run (status, when, how long, which copy, the error token translated with the raw token as fallback), the next due time; a **Run now** button for scheduled names (`jobs.manage` only; disabled with "already waiting" while a row is open).
- **Recent** (a list): failed and waiting rows, newest first: name, key, status, attempts, due time, error token; **Retry** on a failed row and **Cancel** on a waiting row (`jobs.manage`).
- **Read-only banner:** when `StatusView.ReadOnly`, the screen says this copy runs no jobs and hides the three actions.
- **Empty states:** "Nothing is registered" (impossible in practice: `jobs.purge` always is; still rendered), "Nothing waiting or failed".
- **Error state:** the shell's `ErrorState` with the reference. **Forbidden:** the shell's forbidden state for a 403 on `GET`; the actions are hidden without `jobs.manage` and a 403 on an action shows the shell's error summary.
- No job's `args` are shown: the screen shows references by count only ("3 references"), never the IDs, so a URL or a screenshot carries nothing to look up.

`bms check` prints, under a "jobs" heading, each registered name with its owner and schedule (no database read), so an operator can see what a build will run.

## Behavior

**Lifecycle.** `New` does no I/O and registers `jobs.purge` (hourly, `Timeout` 10 m) and `signals.redeliver` (on demand). `app` builds the runner in part 8 after audit, settings and authz (it takes their seams), sets `a.deps.Jobs` (scoped by `provision` with `Scoped(m.Name)`), mounts the route set, and in `link` registers `audit.purge` (hourly) with a closure over `audit.Log.Purge` and the `audit` settings reader (`Duration("retention")`; 0 = skip). Modules register in `Start`. `Run` starts after `Set.Start` (so every registration is in) and freezes `Register`. Stop order: HTTP drained, **jobs stopped** (running handlers get `Canceled`, rows released), `signals.Close`, `Set.Stop`, `db.Close` (app D10, signals decision 6). `Close` is idempotent and waits for the workers, and for any `RunOnce` in flight (b), under the ctx it is given; a run that ignores cancellation is abandoned at the deadline and its row stays `running` until its lease lapses, when the next writer pass reclaims it (step 2a).

**A tick** (`jobs.tick` span; one per `PollInterval`, and at once on start):
1. If `db.ReadOnly()`: count `outcome=read_only` once per flip, log once, return. Nothing is leased, inserted or purged on a read-only copy (decision 4).
2a. **Reclaim** (b; `reclaim_expired_leases.sql`; runs before step 2, and is labelled 2a so the step numbers cited elsewhere in this spec keep their meaning). In one `Write` transaction, take up to `MaxBatch` rows with `status = 'running' AND leased_until < now` (`FOR UPDATE SKIP LOCKED`, by `(run_at, id)`) and settle them: `attempts < Retry.MaxAttempts` → `queued`, `run_at` unchanged (due now), `leased_until = NULL`, `last_error = 'lost_lease'`, the attempt stays counted; `attempts >= Retry.MaxAttempts` → `failed`, `last_error = 'gave_up'`, `finished_at = now`. A `gave_up` row is logged and counted `outcome=failed` like a run that gave up; a requeued one is logged `outcome=lost_lease` (not counted in `Ran.Released` today; Q-JOBS11). This is the only way a row whose handler kills its process every time (a fatal, an OOM) ends: step 3 alone would re-lease it forever, and before (b) it did, so one poison row took the server down once per `Lease`. A copy that is read-only (step 1) reclaims nothing (F47).
2. **Schedule.** For each registered name with `Every > 0`, in sorted name order: `slot = now.UTC().Truncate(Every)`, `key = slot.Format(time.RFC3339)`; take the name's advisory lock (`lock_job_name.sql`, decision 17 (b)), then insert `(name, key, run_at = slot)` with `ON CONFLICT (name, key) DO NOTHING` and only `WHERE NOT EXISTS (open row of that name)` (decision 7). Two statements per name, all in one `Write` transaction; the locks are held to its commit. Two processes on one database insert each slot once (the unique index), and a tick racing a `RunNow` of the same name leaves at most one open row (F49).
3. **Lease and run.** While a worker is free: in one short `Write` transaction, pick one due row (`status = 'queued' AND run_at <= now`, or `status = 'running' AND leased_until < now AND attempts < Retry.MaxAttempts` (b): a crashed owner with attempts left; a lapsed row at its last attempt is left for step 2a) by `(run_at, id)` with `FOR UPDATE SKIP LOCKED`, set `status = 'running'`, `leased_until = now + Lease`, `attempts + 1`, `started_at` (if null), `instance`, and `last_error = 'lost_lease'` when it was `running`; return the row. `leased_until` comes from the clock parameter and is returned, as notify's lease does, so Go holds the exact stored value as the fence. No due row: the tick ends.
4. Hand the row to a worker goroutine (below). The tick does not wait for runs (`RunOnce` does).

**A run** (`jobs.run` span, `SpanConsumer`, attributes `job` token, `module` token (the owner), `job_id` ID, `attempt` int, `outcome` token; a child of the enqueuer's trace when `trace_id` is set, through `WithParentFrom`-style linking from the stored trace ID; otherwise a root):
1. Find the handler. None registered here → `UPDATE ... SET status='failed', last_error='no_handler'` fenced by `leased_until`; log once; `outcome=no_handler` (decision 8).
2. Derive `ctx`: `context.WithoutCancel(runnerCtx)` for values, plus the runner's own cancellation (shutdown), plus `WithTimeout(Decl.Timeout)`, plus the `InJob` marker. No transaction, no session.
3. Start the heartbeat: every `Lease/3`, `UPDATE core.job SET leased_until = $new WHERE id = $1 AND leased_until = $old AND status = 'running' RETURNING leased_until`; a renewal that returns no row means the lease was lost (another process re-leased a row whose owner looked dead): cancel the run's ctx, `outcome=lost_lease`, write nothing more to the row (it is someone else's now). A renewal that fails with a store error is retried at the next beat; the run goes on while the lease has not expired.
4. Call the handler under `recover`. A panic is logged once with its stack (`error.kind=internal`, the job name and ID, never the args), counted `outcome=panicked`, and the row fails with `last_error='panic'`.
5. **Outcome by kind**, written in one fenced `UPDATE` (`WHERE id = $1 AND leased_until = $fence`; no row updated means the lease was lost, see 3) within `recordTimeout` (5 s) on a ctx without the run's cancellation, so a shutdown never loses an outcome:
   - nil → `done`, `finished_at = now`, `leased_until = NULL`; `outcome=ok`.
   - `errors.Is(err, db.ErrReadOnly)` → release: `queued`, `attempts - 1`, `leased_until = NULL`, `run_at` unchanged; the runner pauses until the next tick finds `ReadOnly()` false; `outcome=read_only` (db.md Proposed change 13).
   - `Canceled` while `ctx.Err() != nil` (shutdown or lost lease) → release as above, `last_error='canceled'`; `outcome=canceled`.
   - `Unavailable` or `Timeout` (`retry.Retryable`) and `attempts < Retry.MaxAttempts` → `queued`, `run_at = now + Retry.Delay(attempts + 1, u)`, `last_error = <kind>`; `outcome=retried`.
   - `Unavailable` or `Timeout` at the last attempt → `failed`, `last_error='gave_up'`; `outcome=failed`.
   - any other kind (`Invalid`, `NotFound`, `Conflict`, `Permission`, `Unauthenticated`, `Unsupported`, `Internal`, `Canceled` with a live ctx) → `failed`, `last_error = <kind>`; `outcome=failed`. A bug is not retried; the log line carries the kind and the op, the admin can `Retry` after a fix.
6. `bms.jobs.lag` records `started_at - run_at` in seconds; `bms.jobs.runs{job, module, outcome}` counts; the span records the error where it is handled (here), so the handler should not also log it.

**The redelivery job** (`signals.redeliver`): `Defer` stores the `Delivery` (decision 9); the handler decodes it, sets `Attempt = row.attempts + d.Attempt` (the first delivery counts), calls `Redeliverer.Redeliver`. Its errors follow the table above with two readings: `NotFound` (no handler: the module was disabled since) → `done` with a log line and `outcome=dropped` (signals Proposed change 6), and the handler's own `Unavailable`/`Timeout` → retried until `Retry.MaxAttempts` counting the first delivery, then `gave_up`. The subscribing module's spec says how its handler is idempotent on `Signal.ID` (people's `identity.account-created` handler is: `ON CONFLICT DO NOTHING`).

**Enqueue** (`jobs.enqueue` span, `SpanProducer`): validate; check the name is registered here and in scope; stamp `trace_id` from `telemetry.TraceIDFrom(ctx)`; `INSERT ... ON CONFLICT (name, key) WHERE key IS NOT NULL DO UPDATE SET run_at = EXCLUDED.run_at, args = EXCLUDED.args, updated_at = ... WHERE core.job.status = 'queued' RETURNING id`; no row back with a key set → `SELECT id` of the existing row (a running or final one: no-op). Inside the caller's transaction (`db.TxMode(ctx)` reports `Write`) the statement runs on it; inside a `Read` transaction it fails `Invalid` (db's rule for a write nested in a read); outside any, it is standalone. No `AfterCommit` hook is needed: the row is the commit.

**RunNow** takes the name's advisory lock (decision 17 (b)) and then inserts `(name, key = "admin:<new id>", run_at = now)` only `WHERE NOT EXISTS (open row of that name)` (else `Conflict running`), with the audit entry in the same `Write` transaction; two concurrent `RunNow`s of one name end with one row and one `Conflict running` (F48). The lock does not fail on a read-only copy (advisory locks are allowed in a read-only transaction); the insert that follows is what answers `25006`. **Retry**: `UPDATE ... SET status='queued', attempts=0, run_at=now, last_error=NULL, finished_at=NULL WHERE id=$1 AND status='failed'`; no row → `NotFound` if the id is unknown, `Conflict status` otherwise. **Cancel** likewise on `status='queued'`.

**Purge** (`jobs.purge`, hourly): delete final rows with `finished_at < now - DoneTTL` in batches of `MaxBatch` with `FOR UPDATE SKIP LOCKED` until a batch comes back short (notify's purge shape). Counted in the run's span; no audit entry (nothing personal).

**Concurrency.** `Workers` goroutines per process; a module's handler may run concurrently with its other jobs and with the same name under different keys, never with the same row (the lease) and, for a scheduled name, never with its own previous slot (decision 7). Two writer processes on one database (a restart overlap, or an operator who ran two) share rows through `SKIP LOCKED` and the unique slot key, and serialize the per-name "no open row" test through the advisory lock (decision 17 (b)): `Enqueue`, the lease, the heartbeat and the outcome writes never take it, so a lock holder blocks only another slot insert or `RunNow` of the same name, for one short transaction. The on-site/off-site case (D27) is a writer and a replica: the replica does nothing here; after a switchover the new writer picks up waiting rows and re-leases any whose lease lapsed. Clock skew between two writers moves slots and fences by the skew: a slot may be inserted up to the skew early and a lease may look expired up to the skew early; `Lease` of two minutes against NTP-synced hosts leaves that harmless, and the operator docs say the copies must keep time.

**Ordering.** Due rows run by `(run_at, id)`; no priorities. A long backlog delays everything equally; the `bms.jobs.lag` histogram shows it.

**Idempotency** is the handler's, by design (decision 2): the runner promises at-least-once per lease and never exactly-once. The spec says so out loud so no module relies on more: a reminder handler checks the sign-up still stands and that the reminder was not already sent (its own column or notify's idempotency `Key`), a purge is naturally idempotent, a redelivery reaches a handler that is idempotent on `Signal.ID`.

**Read-only copy.** `Register`, `Scoped`, `Status`, `Routes` work; the loop skips; `Enqueue`, `Defer`, `RunNow`, `Retry`, `Cancel` fail `Unavailable` wrapping `db.ErrReadOnly` (the api answers `503 read_only`); a run in flight when the copy flips releases its row at the next write that answers `25006`.

**Telemetry.** Spans and metrics as listed in the Summary; attributes are tokens, IDs, ints and kinds only; `args` never appear on a span, a log line or a metric; the log line per outcome carries `job`, `job_id`, `attempt`, `outcome`, `error.kind`. `bms.jobs.queue{status}` is a gauge refreshed at the end of each writer pass from one count query, and reports `queued` and `running` only (b): the final statuses are bounded by `jobs.purge` and visible in `StatusView.Counts`, and a gauge of `done` rows over seven days says nothing an operator acts on. Because the pass is skipped on a read-only copy, the gauge there is stale or absent; `StatusView.ReadOnly` is the signal that copy gives, and the writer's gauge is the one to watch. A handler's own spans are children of `jobs.run` through the ctx.

**Security.** The routes are signed-in and check their permission inside the use case; `GET` changes nothing; the three `POST`s are covered by the api shell's cross-origin check. Job names on the wire are validated against the grammar before any lookup.

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `Name.Validate` | empty, bad grammar, > 96 bytes | Invalid | fix the name | `TestName_Validate_BadGrammar_ReturnsInvalid` |
| F2 | `Decl.Validate` | invalid name, `Every` outside 1m..168h, `Timeout` outside 1s..1h, nil handler | Invalid (fields) | fix the declaration | `TestDecl_Validate_EveryOutOfRange_ReturnsInvalid`, `TestDecl_Validate_NilHandler_ReturnsInvalid` |
| F3 | `Request.Validate` | invalid name, key malformed or > 128 bytes, args invalid, `RunAt` > now + `MaxDelay` | Invalid (fields) | fix the request | `TestRequest_Validate_KeyTooLong_ReturnsInvalid`, `TestRequest_Validate_RunAtTooFar_ReturnsInvalid` |
| F4 | `Register`, `Enqueue` | name's owner is not the scope's owner | Invalid (`name`, `scope`) | use own names | `TestRegister_OtherOwner_ReturnsInvalidScope`, `TestEnqueue_OtherOwner_ReturnsInvalidScope` |
| F5 | `Register` | name already registered in this process | Conflict | register once | `TestRegister_Twice_ReturnsConflict` |
| F6 | `Register` | after `Run` began | Internal | register in `Start` | `TestRegister_AfterRun_ReturnsInternal` |
| F7 | `Enqueue` | no handler registered for the name here | Invalid (`name`, `unknown`) | register first | `TestEnqueue_Unregistered_ReturnsInvalidUnknown` |
| F8 | `Enqueue`, `Defer`, `RunNow`, `Retry`, `Cancel` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | api answers `503 read_only`; signals logs and ends the delivery | `TestEnqueue_ReadOnly_ReturnsErrReadOnly`, `TestDefer_ReadOnly_ReturnsErrReadOnly`, `TestRunNow_ReadOnly_ReturnsErrReadOnly` |
| F9 | `Enqueue` | database down, deadline passed, ctx canceled | Unavailable / Timeout / Canceled | fail the use case (the caller's transaction rolls back) | `TestEnqueue_StoreUnavailable_ReturnsUnavailable` |
| F10 | `New`, `Options.Validate` | nil dependency, bad instance, durations or counts out of range, invalid retry policy | Invalid (fields `jobs.*`) | fix the wiring (app exits 2 through F10-style mapping) | `TestNew_NilDB_ReturnsInvalid`, `TestNew_LeaseBelowMinimum_ReturnsInvalid`, `TestNew_WorkersAboveMax_ReturnsInvalid` |
| F11 | `Scoped` | owner fails the module-name grammar | Invalid | fix the wiring | `TestScoped_BadOwner_ReturnsInvalid` |
| F12 | `Scoped` | after `Close` | Internal | none (programming error) | `TestScoped_AfterClose_ReturnsInternal` |
| F13 | `Defer` | `Delivery.Validate` fails | Invalid | signals logs it; the delivery is final | `TestDefer_InvalidDelivery_ReturnsInvalid` |
| F14 | `Defer` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | as F8 | (F8) |
| F15 | `Defer` | after `Close` | Unavailable | signals logs it (shutdown) | `TestDefer_AfterClose_ReturnsUnavailable` |
| F16 | `Run` | called twice | Internal | none | `TestRun_Twice_ReturnsInternal` |
| F17 | `RunOnce` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | skip; nothing leased | `TestRunOnce_ReadOnly_ReturnsErrReadOnly` |
| F18 | `RunOnce`, the tick | store failure on schedule, lease or count | Unavailable / Timeout | log once per state change, retry next tick | `TestTick_StoreUnavailable_LogsOnceAndRetries` |
| F19 | `Close` | workers still returning when ctx ends | Timeout / Canceled | app reports; the rows' leases lapse | `TestClose_HandlerIgnoresCancel_ReturnsTimeout` |
| F20 | `Status`, admin methods | no actor | Unauthenticated | 401 | `TestStatus_NoActor_ReturnsUnauthenticated` |
| F21 | `Status` | no `jobs.read` | Permission | 403 | `TestStatus_NoPermission_ReturnsPermission` |
| F22 | `RunNow`, `Retry`, `Cancel` | no `jobs.manage` | Permission | 403 | `TestRunNow_NoPermission_ReturnsPermission`, `TestRetry_NoPermission_ReturnsPermission`, `TestCancel_NoPermission_ReturnsPermission` |
| F23 | `RunNow` | name not registered here | NotFound | 404 | `TestRunNow_UnknownName_ReturnsNotFound` |
| F24 | `RunNow` | an on-demand name (`Every == 0`) | Invalid (`name`, `on_demand`) | 400 | `TestRunNow_OnDemand_ReturnsInvalid` |
| F25 | `RunNow` | a row of that name is queued or running | Conflict (`running`) | 409; the screen says "already waiting" | `TestRunNow_AlreadyOpen_ReturnsConflict` |
| F26 | `RunNow` | read-only | Unavailable wrapping `db.ErrReadOnly` | as F8 | (F8) |
| F27 | `Retry`, `Cancel` | unknown job ID | NotFound | 404 | `TestRetry_UnknownID_ReturnsNotFound` |
| F28 | `Retry` | row not `Failed` | Conflict (`status`) | 409 | `TestRetry_NotFailed_ReturnsConflict` |
| F29 | `Cancel` | row not `Queued` | Conflict (`status`) | 409 | `TestCancel_NotQueued_ReturnsConflict` |
| F30 | a run | handler returns Unavailable or Timeout, attempts left | (row re-queued) | nothing; the handler runs again after backoff | `TestRun_HandlerUnavailable_RequeuesWithBackoff` |
| F31 | a run | handler returns Unavailable or Timeout at the last attempt | (row `failed`, `gave_up`) | admin may `Retry` | `TestRun_GivesUp_AfterMaxAttempts` |
| F32 | a run | handler returns another kind | (row `failed`, `<kind>`) | fix the bug; `Retry` | `TestRun_HandlerInvalid_FailsWithoutRetry` |
| F33 | a run | handler panics | (row `failed`, `panic`; one log with stack) | fix the bug | `TestRun_HandlerPanics_FailsAndLogsOnce` |
| F34 | a run | handler returns `db.ErrReadOnly` (copy flipped mid-run) | (row released; runner pauses) | nothing; the next writer runs it | `TestRun_ReadOnlyMidRun_ReleasesAndPauses` |
| F35 | a run | shutdown cancels the handler | (row released, attempt not counted) | nothing | `TestRun_CanceledOnShutdown_ReleasesRow` |
| F36 | a run | heartbeat finds the lease taken by another process | (`lost_lease`; row untouched; ctx canceled) | handler stops; the other process's run stands | `TestRun_LostLease_CancelsAndWritesNothing` |
| F37 | a run | no handler for the row's name in this process | (row `failed`, `no_handler`; one log) | enable the module, `Retry` | `TestRun_NoHandler_FailsNoHandler` |
| F38 | the scheduler | a row of the name is already open | (no slot inserted) | nothing; the next slot waits | `TestTick_ScheduledNameOpen_InsertsNoSlot` |
| F39 | the scheduler | the same slot exists (second process) | (no-op on the unique index) | nothing | `TestTick_SlotExists_IsNoop` (integration) |
| F40 | `signals.redeliver` | `Redeliver` returns NotFound | (row `done`, `outcome=dropped`, one log) | nothing | `TestRedeliver_NoSubscriber_DropsWithLog` |
| F41 | `signals.redeliver` | `Redeliver` returns the handler's Unavailable | (retried; `Attempt` counts the first delivery) | nothing | `TestRedeliver_HandlerUnavailable_RetriesWithIncrementedAttempt` |
| F42 | `Enqueue` | inside a `Read` transaction | Invalid (from db) | use a `Write` transaction | `TestEnqueue_InsideReadTx_ReturnsInvalid` |
| F43 | `Enqueue` | same `(name, key)` already running or final | (no-op; existing ID returned) | key distinct runs distinctly | `TestEnqueue_KeyFinal_IsNoopReturnsExistingID` |
| F44 | `jobstest.Fake.Run` | name not registered | NotFound | fix the test | `TestFakeRun_Unregistered_ReturnsNotFound` |
| F45 (b) | the reclaim (step 2a) | a `running` row's lease lapsed and `attempts >= Retry.MaxAttempts` (the process died at the last attempt) | (row `failed`, `gave_up`; never leased again; one log, `outcome=failed`) | admin may `Retry` after a fix | `TestRun_CrashedAtMaxAttempts_FailsGaveUpInsteadOfLeasingAgain`, `TestRun_CrashedAtCustomMaxAttempts_FailsGaveUp` (integration) |
| F46 (b) | the reclaim, the lease | a `running` row's lease lapsed and `attempts < Retry.MaxAttempts` | (row `queued`, `lost_lease`, attempt kept; leased again at once) | nothing; the handler runs again | `TestRun_CrashedBelowMaxAttempts_IsLeasedAgain` (integration) |
| F47 (b) | the reclaim | read-only copy | (nothing written; the row waits for the writer) | nothing; the next writer pass reclaims it | `TestRun_CrashedAtMaxAttempts_ReadOnlyCopy_ChangesNothing` (integration) |
| F48 (b) | `RunNow` | two concurrent `RunNow`s of one name | one row; the other Conflict (`running`) | 409 for the loser; the screen says "already waiting" | `TestRunNow_TwoConcurrent_OneWinsOneConflictRunning` (integration) |
| F49 (b) | the scheduler, `RunNow` | a tick's slot insert races a `RunNow` of the same name at the slot boundary | (at most one open row of the name; the loser is a no-op slot or a Conflict) | nothing | `TestTick_RacingRunNowAtSlotBoundary_AtMostOneOpenRow` (integration) |
| F50 (b) | `Status.MarshalText`, `UnmarshalText` | not a status | Invalid (`status`, `one_of`) | fix the value | `TestStatus_MarshalText_Invalid_ReturnsInvalid`, `TestStatus_UnmarshalText_Unknown_ReturnsInvalid` (to add: no test covers them yet) |

`Name.Owner`, `InJob`, `Status.String`, `Routes`, `OpenAPI`, `Declared` (b) cannot fail: pure functions over values already validated, constants or the in-memory registry.

## Lock-in check

No external technology beyond PostgreSQL, which the project already requires (D8): the queue is a table, the lock is `FOR UPDATE SKIP LOCKED` (PostgreSQL 9.5+, well under `db.MinServerVersion`), the schedule is the clock. No broker, no Redis, no cron daemon, no cloud scheduler. A regional runs it on the same single database as everything else (principle 5).

## Test plan

- **Unit** (`jobs`): a hand-written fake store behind the storage interface `service.go` declares; `clock.NewFake` drives ticks, backoff and lease expiry (no `time.Sleep`); `telemetrytest.New(t)` for logger, tracer and meter (fails on any policy violation); `audittest.NewFake("jobs")`; `authztest.NewFake()` (denies until granted) with `identity.WithSession` for the admin use cases; a fake `Redeliverer`. Table tests per failure mode above; a test that every outcome token and every `last_error` token matches `^[a-z_]{1,32}$`; a test that a handler's `Args` with an invented person's ID appears in no log, span or metric (`!c.Contains(id)`); a test that `Register` on a `db.None()` runner succeeds (`bms check`).
- **Permission tests:** F20 to F22 for each admin method; `jobs.read` holders cannot `RunNow`.
- **Integration** (`//go:build integration`, `dbtest.TempDB`): migrations apply; two runners on one database lease disjoint rows and insert one slot (F39); a row whose lease lapsed is re-leased by the other runner; the heartbeat keeps a long handler's lease past `Lease`; `Enqueue` inside a `Write` transaction that rolls back leaves no row; the dedupe upsert moves a queued row and leaves a final one; `jobs.purge` deletes only final rows older than `DoneTTL`; `dbtest.Fake.SetReadOnly` mid-run yields F34.
- **Conformance** (`moduletest`): a nil `Deps.Jobs` becomes `jobstest.NewFake(manifest.Name)`; a new rule `jobs_register_scoped` (a module's registrations carry its own prefix) and the leak rule also scans `Fake.Enqueued()` args for the seed markers (they are IDs, so this guards a future `Args` change).
- **End to end** (`app`): `serve` on a temporary database runs `jobs.purge` and `audit.purge` once at start (`bms.jobs.runs` observed); `bms check` lists the registered names.

## Open questions

- **Q-JOBS1. Does the `/jobs` screen, with Run now / Retry / Cancel, ship in M4, or status only, or Later?** Recommendation: ship it in M4 as specified. It is one screen over one `GET` and three `POST`s, and the first module with reminders (volunteers) will raise "did the reminder run?" on day one; without `Retry` a failed purge or reminder has no path but a restart.
- **Q-JOBS2. The give-up policy: ten attempts, 1 m doubling to 1 h, about four hours; the same for retried signal deliveries.** Recommendation: accept. Longer would hide a broken dependency; shorter would lose a reminder over a short outage. An admin can `Retry` for seven days, and the log and metric keep the record.
- **Q-JOBS3. Should notify's sender tick move onto `jobs` now?** Recommendation: no (notify decision 2 stands; 09 §6 says "later"). Notify's loop is implemented, tested and one `SKIP LOCKED` lease like this one; moving it buys nothing in M4 and would couple the two parts. Revisit when a third loop appears or when the audit importer and notify's loop and this one are visibly the same code.
- **Q-JOBS4. A daily schedule at a time of day in `regional.timezone` (for example "purge at 03:00 local") in M4 or Later?** Recommendation: Later. `Every` covers retention and daily scans; a reminder tied to a shift is a deferred job with `RunAt`. The addition is a second `Decl` field and a `ZoneSource` seam, not a redesign.
- **Q-JOBS5. `DoneTTL` of 7 days as a constant, or a `jobs.retention` setting?** Recommendation: a constant. The rows hold no personal data (D30 is about personal data), and the only effect of the number is how long an admin can `Retry` and how long a dedupe key blocks a repeat.
- **Q-JOBS6. The audit actor inside a job: `app`'s `auditActor` answers `ActorSystem` when `jobs.InJob(ctx)` (as audit itself does for `signals.InDelivery`).** Recommendation: accept. Approved (D43) with the words "a two-line change in `app` and no change to `audit`"; corrected 2026-10-20 (awaiting approval): audit's `resolveActor` accepts only a person or the operator from the `ActorFunc` (`Internal` otherwise, pinned by its order test), so it is a few lines in `app` **and** a one-kind widening in audit (audit.md amendment 2026-10-20, F13b: `ActorSystem` with no `Person` and no `Superuser` is accepted; everything else the function might answer is still `Internal`). `auditActor` checks `InJob` before the session, as audit checks the delivery before the function (`jobstest.Fake.Run` runs on the test's ctx, which may carry one). `audit.purge` and signal redelivery were never affected (`Purge` sets its own actor; redelivery is `InDelivery`); the first module job that audits is.
- **Q-JOBS7. `Workers` (2), `PollInterval` (10 s), `Lease` (2 m), `RunTimeout` (5 m) as constants `app` passes, with no new `BMS_JOBS_*` configuration keys in M4.** Recommendation: accept; a small server with one database needs no tuning yet, and a config key is one `config.md` line when a regional asks.

The six below come from the review of the M4 implementation (amendment (b), 2026-10-20, awaiting approval). Each describes what the code does today and what the recommendation would change.

- **Q-JOBS8 (b). `Retry` of a failed scheduled slot while another row of that name is open.** Today `Retry` checks only `status = 'failed'`, so an admin can requeue last hour's failed `audit.purge` while this hour's slot waits or runs, and the two run concurrently, which decision 7 says a scheduled name never does. Recommendation: **yes, refuse with `Conflict` (code `running`)** for a scheduled name while a row of that name is `queued` or `running`, under the same advisory lock `RunNow` takes (decision 17), with a new row F51 `TestRetry_ScheduledNameOpen_ReturnsConflict`; the screen disables Retry with "already waiting" as it does Run now. On-demand names are unaffected (their keys make distinct runs distinct). A small change: one lock, one `NOT EXISTS`, one test.
- **Q-JOBS9 (b). A run whose heartbeats keep failing past its lease expiry keeps running.** Step 3 of "A run" retries a failed renewal at the next beat "while the lease has not expired", but nothing stops the handler once `now >= fence`: the row is reclaimable (step 2a) or re-leasable (step 3) by another writer while this process still runs it, so the at-least-once overlap can last until `Decl.Timeout`, not one `Lease`. Recommendation: **yes, cancel the run's ctx when the clock passes the fence** without a successful renewal (`outcome=lost_lease`, write nothing more to the row, as a renewal that found the lease taken does), with F52 `TestRun_HeartbeatsFailPastFence_CancelsAndWritesNothing`. The handler sees `Canceled`, and the row's outcome belongs to whoever leases it next.
- **Q-JOBS10 (b). `bms check` calls each module's `Start` under `db.None()` to learn its job registrations, and fails the check when a `Start` fails.** `Start` is specified to do no I/O (go-conventions; module-contract), so this is sound today, and a `Start` that fails against `db.None()` would fail `serve` too. The cost: `check` now exercises more than it did, and a module whose `Start` legitimately fails without a database (none should) blocks the whole report. Recommendation: **print the jobs section with the problem and continue; do not fail the check for it.** `check`'s job is to report what a build would do, and the failing `Start` is better shown beside the other sections than hiding them; `serve` is where a failing `Start` must stop the process. This changes `app` (check.md's "jobs" section: `ExitFailure` only for the sections that already fail it), not `jobs`.
- **Q-JOBS11 (b). Count a reclaim's requeue in `Ran.Released` and `bms.jobs.runs{outcome=lost_lease}`.** Today the reclaim counts and meters a `gave_up` row as `outcome=failed` but only logs a requeue, so `Ran.Released` and the `lost_lease` series undercount what the log shows, and a test that drives a crash through `RunOnce` cannot assert on the counts. Recommendation: **yes**: a requeue by the reclaim increments `Ran.Released` and `bms.jobs.runs{outcome=lost_lease}`, with the `job` and `module` tokens from the row's name; `TestRunOnce_ReclaimedRow_CountsReleasedAndLostLease`. The telemetry rows already list `lost_lease` (Proposed change 5), so no change elsewhere.
- **Q-JOBS12 (b). `Request.Validate` and `jobstest.Fake` bound `RunAt` against the wall clock (`time.Now() + MaxDelay`), while the runner checks against its own clock.** The spec's "no `time.Sleep`, time from `lib/clock`" rule is kept by the runner but not by the value type, so a module test that enqueues a reminder a year ahead with a fake clock set in the past passes `Validate` only by luck of the real date. Recommendation: **yes, give the fake an optional clock**: `jobstest.NewFake(owner, opts ...Option)` with `jobstest.WithClock(clock.Clock)`, default the wall clock; the fake validates `run_at` against that clock and `Request.Validate` keeps the wall clock (a value type has no clock to take) with its doc saying so; `TestFake_Enqueue_RunAtBound_UsesTheFakeClock`. No change to the runner.
- **Q-JOBS13 (b). A dedupe key must never carry a person ID.** `Key` is free text and shows on the status screen, in a log line's `job_id` neighbourhood and in `Status.Recent`, so a module that keys a reminder `person:<id>` puts a person identifier where `Args` (record references, hidden) was designed to keep it out: pseudonymous, but it links a screen row to a person for as long as the row lives. Recommendation: **yes**: the rule "key by the record's own ID (the sign-up, the shift, the upload), never by a person ID" goes into `Request.Key`'s doc, the Data row for `key`, and the `add-module` skill text of Proposed change 9 (Proposed change 11); the conformance leak rule already scans `Fake.Enqueued()` and would then also check `Key` against the seed person markers (`jobs_key_not_person`). A regional cannot be asked to configure this away; it is a privacy rule.

## Proposed changes elsewhere

Status as of 2026-10-20: all nine are applied (owner, D43). A tenth and an eleventh were added the same day and await approval.

1. (applied 2026-10-20, D43) **`plan/design/module-contract.md` (row 4):** `Deps.Jobs jobs.Registrar` (uncomment the field; scoped by `provision` with `Scoped(m.Name)`; a module registers in `Start` with its own prefix and enqueues from use cases); `moduletest.Conformance` fills a nil `Deps.Jobs` with `jobstest.NewFake(manifest.Name)` and adds the rule `jobs_register_scoped`; its leak rule scans `Fake.Enqueued()`; mark Proposed change 13 (jobs) applied; add `jobs` to the `Deps` table row text ("scoped per module; `Register` in `Start`; `Enqueue` joins the caller's transaction; handlers are idempotent and run as the system").
2. (applied 2026-10-20, D43) **`plan/design/app.md` (row 18), part 8 and Proposed change 8:** `jobs` is built after audit, settings and authz (it takes `audit.Scoped("jobs")`, `a.deps.authz`, the actor closure, `a.disp` as `Redeliverer`, `cfg.Instance.Name`, and the constants of Q-JOBS7); `signals.New` keeps running before the parts and receives `Deferrer: lazyDeferrer{a}` (resolves `a.jobs` at call time, `Unavailable` before it is built), so no setter is needed (this replaces "constructs the runner before `signals.New`"); `link` registers `audit.purge` (hourly) through `a.jobs.Scoped("audit")` with a closure over `a.deps.audit.Purge` and `a.settings.Scoped("audit").Duration("retention")` (0 = skip); `a.deps.jobs` scoped per module in `provision`; route set `jobs`; the runnable `Run`/`Close` started after `Set.Start`, stopped after HTTP drained and before `signals.Close` (first of the runnables to stop); `auditActor` answers `ActorSystem` when `jobs.InJob(ctx)` (Q-JOBS6); `bms check` prints the registered jobs; `migrate` is unaffected.
3. (applied 2026-10-20, D43) **`plan/design/signals.md` (row 10), Proposed change 6:** mark applied by this spec: `Defer` stores on the writer with key `<signal id>:<subscriber>`; the job name is `signals.redeliver`; `Redeliver` is called with `Attempt` counting the first delivery; `NotFound` is dropped with a log line (`outcome=dropped`); `db.ErrReadOnly` releases and pauses; the give-up count is `DefaultRetry.MaxAttempts` (10) in all.
4. (applied 2026-10-20, D43) **`plan/design/db.md` (row 5), Proposed change 13:** mark applied for jobs (the loop reads `ReadOnly()` before each tick; a run that meets `ErrReadOnly` releases its row and pauses).
5. (applied 2026-10-20, D43) **`plan/design/telemetry.md` (row 15b), the metric table and Proposed change 6:** `bms.jobs.runs{job, module, outcome}` with outcomes `ok`, `retried`, `failed`, `canceled`, `read_only`, `panicked`, `no_handler`, `lost_lease`, `dropped`; add `bms.jobs.queue{status}` (gauge) and `bms.jobs.lag` (histogram, seconds); span names `jobs.tick`, `jobs.enqueue`, `jobs.defer`, `jobs.run`; mark Proposed change 6 applied.
6. (applied 2026-10-20, D43) **`plan/design/audit.md` (row 12), Proposed change 7:** mark applied: `app` registers `audit.purge` (hourly) through `jobs`, calling `Purge(ctx, now - retention)` on the writer only, skipping when `audit.retention` is 0.
7. (applied 2026-10-20, D43) **`plan/design/people.md` (row 8), Proposed change 17:** mark the redelivery part applied (a failed `identity.account-created` handling with a retryable kind is redelivered through `signals.redeliver`); the `people.retention.unused` proposal stays with `people` (its key would be `people.retention_unused` under the underscore grammar, D39, declared by `people` when it registers that job).
8. (applied 2026-10-20, D43) **`plan/07-abstractions.md`, `jobs` entry:** add "Owns: the `Registrar` contract (`Register`, `Enqueue`; = `Deps.Jobs`, scoped per owner), the table `core.job` (scheduled slots and deferred jobs, record references only), the runner (`SKIP LOCKED` leases with a heartbeat, backoff by `retry.Policy`, writer only), the `signals.Deferrer` implementation and `signals.redeliver`, `jobs.purge`, the status screen behind `jobs.read` / `jobs.manage`. Doesn't do: notify's sender loop (notify decision 2), retention periods (each area's setting), daily local-time schedules (Later), cross-process messaging." and the spec link.
9. (applied 2026-10-20, D43) **`plan/09-core-features.md`:** §1 "retryable ones handed to `jobs`" now holds (signals.md decision 3); §8 and §11 "Specs" lines may cite `jobs.md`; the milestone table's M4 row gains "a scheduled purge runs on the writer and shows on `/jobs`"; the admin screens list (§7) gains `/jobs` for site admins. Also `.claude/skills/go-conventions/SKILL.md` and `add-module`: one sentence each: "From M4 `deps.Jobs` (a `jobs.Registrar` scoped to the module): `Register` in `Start` with names `<name>.<ident>`, `Enqueue` inside the use case's transaction with record references only, handlers idempotent and running as the system; tests use `jobstest.NewFake(Manifest.Name)` and `Fake.Run`." (`spec-editor` applies after approval.)
10. (written 2026-10-20, awaiting owner approval) **`plan/design/audit.md` (row 12), decision 6 and `ActorFunc`:** the function may answer `ActorSystem` (no `Person`, no `Superuser`); `resolveActor` accepts a consistent person, operator or system and stays `Internal` for anything else (new F13b with `TestResolveActor_ActorFuncReturnsBadActor_ReturnsInternal` and `TestResolveActor_ActorFuncReturnsSystem_IsTheSystem`). **`plan/design/app.md` (row 18), part 8 row and "Operator attribution":** `auditActor` answers `{Kind: ActorSystem}, true` when `jobs.InJob(ctx)`, before the session and the operator (`TestAuditActor_InJob_IsTheSystem`, including a ctx that carries both). Both amendments are written in those specs; `spec-editor` folds them into the body on approval.
11. (written 2026-10-20, amendment (b), awaiting owner approval) **`.claude/skills/add-module/SKILL.md` and `.claude/skills/go-conventions/SKILL.md`, the `deps.Jobs` sentence of Proposed change 9:** add "a dedupe `Key` names the record's own ID (the sign-up, the shift, the upload), never a person ID: keys show on `/jobs` and in logs" (Q-JOBS13). **`plan/design/module-contract.md` (row 4), `moduletest` rules:** the leak rule also checks `Fake.Enqueued()` keys against the seed person markers (`jobs_key_not_person`). **`plan/design/app.md` (row 18), `bms check`:** if Q-JOBS10 is accepted as recommended, the "jobs" section prints a failing `Start` as a problem line and does not set `ExitFailure`; if Q-JOBS9 is accepted, nothing changes elsewhere (the run's own ctx); if Q-JOBS11 is accepted, nothing changes in telemetry.md (`lost_lease` is already an outcome). The Q-JOBS8 and Q-JOBS12 changes are inside `jobs` and `jobstest` only.
