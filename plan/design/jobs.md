# jobs

- **Status:** Implemented (2026-10-20; Amendment J1 (D50) implemented 2026-10-23); Approved (2026-10-20, D43)
- **Amendment J-W1 (raised 2026-10-30, D57; Approved D58; Implemented 2026-10-30):** the Background work screen for people. What the demo walkthrough found on `/jobs` (raw names and owner tokens, "On copy demo", thirty healthy reminders under "Failed and waiting", sign-up UUIDs in a Key column, headers broken mid-word at 1280 px) and the owner's accepted direction (D57) made concrete: a job's title is the catalogue key `module.<owner>.job.<ident>.title` beside the owner's `module.<owner>.title` (decision 24; core owners without a web area use the shell catalogue; a Go test fails the build when a registered name has no `en` title; `Decl` gains no field); `StatusView.Recent` becomes four server-capped lists `Failed`, `Running`, `Retrying`, `Waiting` plus `Instance` (decision 25); the screen shows no ID, key or raw name, lists each state under its own heading with a counts line for the phone, says "server" for an instance and only when another server ran the job, and renders a 403 as the shell's error block with the reference; the kit's `Table` headers never break inside a word. Section "Amendment J-W1" before Open questions; F59 to F67; Q-JOBS15 and Q-JOBS16 answered (D58: keys hidden entirely for now; "server" is the word); Proposed changes 15 to 19 applied 2026-10-30 (D58).
- **Amended 2026-10-28 (D52):** (a) when a run's own `Enqueue` of its key made the held request and the run then fails (any non-retryable kind, a panic, or a retryable error), the row is queued at the held `RunAt` with its `attempts` **kept**, not reset to 0, and the held request is applied only while `attempts < Retry.MaxAttempts`: at the limit it is dropped and the row ends `failed` (`gave_up`), a narrow exception to F55 for self-made requests only, so a self-rescheduling job that keeps failing cannot retry forever (mechanism: column `next_by_run`, migration `0003_job_next_by_run.sql`; the run's row ID travels in ctx, `rules.WithJob`/`rules.RunID`); a request from outside the run still resets attempts (D50; F53, F55); a run that ends with nil still resets (F54). (c) `jobstest.Fake` fidelity: `RunDue` builds the `Job` from the row's current `Args`; a row created by `Run` (a `Key` with no row) is tracked like an enqueued one; a retryable error (`Unavailable`, `Timeout`) requeues the row with the attempt counted, until `jobs.DefaultRetry.MaxAttempts`, instead of ending it `failed`; the doc says the Fake skips retry delays (a requeued row is due at once). F55, F58 and the Fake's doc comments are amended; new tests `TestSelfReenqueue_FailingRun_KeepsAttemptsAndGivesUp`, `TestFakeRunDue_UsesCurrentArgsAfterLaterEnqueue`, `TestFakeRun_CreatedRow_IsTrackedLikeAnEnqueuedOne`, `TestFakeRun_ErrorKinds_RequeueOrFail`.
- **Amendment 2026-10-20 (approved, D44):** Q-JOBS6 corrected. Its recommendation said "no change to `audit`", but audit's `resolveActor` accepts only a person or the operator from the `ActorFunc` and answers `Internal` for `ActorSystem`, so the approved wiring would fail every `Record` in a module's job handler. The intent stands (the actor inside a job is the system, through `app`'s `auditActor` over `jobs.InJob`); it needs audit.md's amendment of the same date (the function may answer `ActorSystem`; F13b) and `auditActor` to check `InJob` before the session. Nothing in `jobs` changes. Proposed change 10.
- **Amendment 2026-10-20 (approved, D44):** what the review of the M4 implementation found and the code now does. (1) **Poison rows.** A tick's writer pass begins with a **reclaim** step (2a): lapsed `running` rows go back to `queued` with `last_error = lost_lease`, or to `failed gave_up` once `attempts >= Retry.MaxAttempts`; and the lease's lapsed arm gains `attempts < Retry.MaxAttempts`. Before this a job that killed its process (a fatal in a handler) at its last attempt was re-leased forever, taking the server down once per `Lease`. (2) **No overlap under concurrency.** The "no open row of this name" test in the scheduler's slot insert and in `RunNow` is serialized by a transaction-scoped advisory lock on the name, taken in the same `Write` transaction immediately before the insert; the scheduler takes names in sorted order, so two ticks cannot deadlock. (3) F45 to F50 with their tests (`integration_concurrency_test.go`). (4) Reviewer-accepted deviations: `Runner.Declared()`, exported `MaxNameBytes` and `MinTimeout`, `Status.MarshalText`/`UnmarshalText`, route `GET /` (the shell mounts `{$}`), `Enqueue` runs `Transact(Write)`, the `bms.jobs.queue` gauge reports `queued` and `running` only, `jobstest.Fake.Fail` is one-shot, `Close` also waits for `RunOnce`. (5) Q-JOBS8 to Q-JOBS13 settled with the recommendations (decisions 18 to 22 below); Proposed changes 10 and 11 applied.
- **Amendment J1 (raised 2026-10-23, approved 2026-10-23, D50):** the dedupe key's rule is changed from "a waiting row is moved, a running or final one makes the request a no-op" to **"the latest request for a `(name, key)` wins"**: a `queued` row is moved (as before), a final row (`done`, `failed`, `canceled`) is reopened as a fresh `queued` row, and a request made while the row is `running` is held on the row (`next_run_at`, `next_args`, migration `0002`) and applied the moment the run leaves `running`, whatever its outcome. Found by the volunteers V1 build: the reminder handler's carry-forward at the horizon (volunteers decision 25) enqueued its own key from inside its run, which the old rule made a silent no-op, so a reminder further ahead than `MaxDelay` never fired; the same rule also made an `UpdateShift` within `DoneTTL` of a sent reminder a no-op. `jobstest.Fake` now keeps a row per key with the same states and transitions (`Queued()`, `RunDue`), so a test asserts the schedule the queue would hold, not the requests it heard. Decision 3 and F43 reworded, decision 23, F53 to F58, Q-JOBS14. The text below is the spec as amended; the record with the alternatives is "Amendment J1" before Open questions.
- **Package:** internal/core/jobs (postgres in `internal/core/jobs/postgres`, fake in `internal/core/jobs/jobstest`)   **UI:** web/src/core/jobs
- **Depends on specs:** `model`, `lib-errs`, `lib-core` (`clock`, `retry`, `validate`), `db`, `signals` (`Delivery`, `Deferrer`, `Payload`, `Dispatcher.Redeliver`), `audit`, `authz` (leaf types, `Authorizer`, `ActorFunc`), `telemetry`, `api` (`httpx`, the route-set shape), `web-shell`, `app` (part 8), `module-contract` (`Deps.Jobs`), `settings` (reads `audit.retention` through `app`'s closure only)   **Used by:** every module (`Deps.Jobs`), `signals` (the `Deferrer`), `audit` (its purge is the first scheduled job), `people` (the retried `identity.account-created` handling), `files` (sibling draft: a sweep of orphaned uploads), `volunteers` (next: shift reminders, qualification-expiry reminders), `app` (part 8, the runnable, `bms check`), `web-shell` (the status screen)

## Summary

**Purpose.** Run work that belongs to no request, when it is due, on the writing copy, with retries. Two shapes: a **scheduled** job (every N minutes or hours; one slot at a time, never overlapping itself) and a **deferred** job (enqueued by a use case inside its own transaction, with record references as arguments and a time to run). Both are rows in one PostgreSQL table leased with `FOR UPDATE SKIP LOCKED`, so several processes on one database share the work and a read-only copy does none of it. Package `internal/core/jobs`; imports `model`, `lib`, `core/db`, `core/signals`, `core/audit`, `core/authz` (leaf types, `Authorizer`), `core/telemetry`. Never `core/settings`, `core/identity`, `core/people`, `core/notify`, `core/module`, `api`, `app`, `config` or a module.

**Names.** `jobs` is the package, the core part (8), the migration set (schema `core`), the route set (`/api/jobs/`), the permission prefix, the audit area, the screen set and the owner of one job name, `jobs.purge`.

**Exported (`jobs`).**
- `Name` (`<owner>.<ident>`, the `signals.Name` grammar, at most 96 bytes; `Validate`, `Owner`); `JobID = model.ID[job]`; `Args = signals.Payload` (record references only, at most `signals.MaxRefs`); `Status` (`Queued`, `Running`, `Done`, `Failed`, `Canceled`; `String`, `ParseStatus`, `MarshalText`/`UnmarshalText`: a JSON string and a usable map key).
- `Job{ID, Name, Key string, Args, Attempt int, RunAt time.Time}` (what a handler receives); `Handler func(ctx, Job) error`; `Decl{Name, Every, Timeout time.Duration, Handler}` (+`Validate`; `Every` 0 = on demand only, else `MinEvery`..`MaxEvery`); `Request{Name, Key string, Args, RunAt time.Time}` (+`Validate` on the wall clock and `ValidateAt(now time.Time) error`, an accepted deviation so jobstest's fake validates against its own clock; zero `RunAt` = now; `Key` dedupes per `(name, key)`).
- `Registrar{Register(Decl) error; Enqueue(ctx, Request) (JobID, error)}` = `module.Deps.Jobs`, scoped to one owner by `(*Runner).Scoped(owner)`.
- `Redeliverer{Redeliver(ctx, signals.Delivery) error}` (the `*signals.Dispatcher`, through `app`).
- `Record{ID, Name, Key, Status, Attempts int, RunAt, StartedAt, FinishedAt time.Time, LastError, Instance, TraceID string}` (`LogValue` = ID and name); `Registered{Name, Owner string, Every, Timeout time.Duration, Last *Record, NextDue time.Time}`; `StatusView{ReadOnly bool, Registered []Registered, Counts map[Status]int, Recent []Record}`; `Ran{Scheduled, Ran, Failed, Retried, Released int}`.
- `Options{DB, Clock, Logger, Tracer, Meter, Audit audit.Recorder (scoped "jobs"), Authz authz.Authorizer, Actor authz.ActorFunc, Redeliverer, Instance string, PollInterval, Lease, RunTimeout time.Duration, Workers int, Retry retry.Policy}` (+`Validate`); `New(Options) (*Runner, error)` (no I/O; works against `db.None()`).
- `*Runner` (safe for concurrent use): `Scoped(owner) (Registrar, error)`; `Defer(ctx, signals.Delivery) error` (implements `signals.Deferrer`); `Run(ctx) error` (the scheduler tick and the workers, an `app` runnable); `RunOnce(ctx) (Ran, error)`; `Close(ctx) error` (idempotent); `Status(ctx) (StatusView, error)`; `RunNow(ctx, Name) (JobID, error)`; `Retry(ctx, JobID) error`; `Cancel(ctx, JobID) error`; `Routes() []Route`; `OpenAPI() []byte`; `Declared() []Registered` (what is registered here, no I/O, no permission; `bms check`). `Route{Method, Pattern string, Handler http.Handler}` (all signed-in; `app` converts). `InJob(ctx) bool` (true inside a handler; `app`'s audit actor answers `ActorSystem`).
- Constants: `MaxNameBytes = 96`, `MinTimeout = 1s`, `MinEvery = 1m`, `MaxEvery = 168h`, `MaxRunTimeout = 1h`, `MaxDelay = 366 * 24h`, `MaxKeyBytes = 128`, `DefaultPollInterval = 10s`, `DefaultLease = 2m`, `DefaultRunTimeout = 5m`, `DefaultWorkers = 2`, `MaxWorkers = 32`, `DoneTTL = 7 days`, `MaxRecent = 50`, `MaxBatch = 1000`, `NameRedeliver Name = "signals.redeliver"`, `NamePurge Name = "jobs.purge"`; `DefaultRetry = retry.Policy{Initial: 1m, Max: 1h, Multiplier: 2, MaxAttempts: 10}`.
- Permissions `jobs.read` (site, reach `internal`, starter roles `authz.SiteAdminRoles`), `jobs.manage` (site, `internal`, starter `settings-admin`); `Permissions []authz.PermissionDecl`. No signals, no settings. Audit actions `jobs.job.run`, `jobs.job.retried`, `jobs.job.canceled`. `postgres.Migrations fs.FS`; `Set = db.Set{Name: "jobs", Schema: "core"}`.
- `jobstest.NewFake(owner, opts ...Option) *Fake` with `WithClock(clock.Clock)` (a `Registrar` that validates like the real one and keeps one row per `(name, key)` with the real states and transitions: `Registered() []Decl`, `Enqueued() []Request` (every request heard, recorded after commit), `Queued() []Request` (the rows now waiting, with the `RunAt` and `Args` the queue would hold, by `RunAt`), `Run(ctx, Name, Job) error` drives a registered handler with `InJob` true and moves the key's row through `running` and out of it by the key rule, `RunDue(ctx, now) (int, error)` runs every waiting row due at `now` with the row's current `Args` (a row created by `Run` is tracked; a retryable error requeues it; the Fake skips retry delays, D52(c)), `Fail(err)` makes the next `Enqueue` fail once).

**Key decisions.** 1 one table, one mechanism: a scheduled job's due slot is itself a row (`key` = the slot time), so leasing, retries, the fence, the screen and the dedupe index serve both shapes; two schedulers on one database insert the same slot once. 2 at-least-once, never exactly-once: a lease with a heartbeat (`Lease`, renewed at a third of it) hands a crashed process's job to another within one lease; a handler is idempotent on `Job.ID` or on what its `Args` name, and re-reads current state rather than trusting the arguments. 3 `Enqueue` joins the caller's transaction (as `notify.Send` does): the row commits or rolls back with the change; with a `Key`, **the latest request wins**: a waiting row with the same `(name, key)` is moved to the new `RunAt` and `Args`, a final one is reopened as a fresh waiting row, and a request made while the row runs is held on it and applied when the run leaves `running` (decision 23). 23 the key rule has one mechanism: `next_run_at`/`next_args` are set only while `status = 'running'`, and every statement that takes a row out of `running` (complete, requeue, release, fail, the reclaim's two arms) applies them first: `queued` at `next_run_at` with `next_args`, `attempts = 0` (D52(a): when the run itself made the latest request (`next_by_run`, set from the run's row ID carried in ctx by `rules.WithJob`/`rules.RunID`; the latest request decides it), a fail or a lease reclaim keeps `attempts` and applies the request only while `attempts < Retry.MaxAttempts`, else drops it and the row ends `failed` (`gave_up`); a requeue keeps `attempts`, a release uses `attempts - 1`, complete clears the flag and resets as before; an outside request resets to 0 in every case, D50), `started_at = NULL`; the run's own outcome is still logged, metered and kept in `last_error`; the row never ends while a newer request waits on it. A handler that re-enqueues its own key from inside its run (volunteers' reminder carrying itself past the horizon) therefore works with no API of its own, a use case that enqueues the key during the run is never lost, and a key whose row finished is usable again at once instead of after `DoneTTL`. `Defer`, the scheduler's slot and `RunNow` keep their own inserts and are unchanged. 4 only the writing copy runs anything: the loop skips every tick while `db.ReadOnly()`, a run that meets `db.ErrReadOnly` releases its row and stops until the next probe, `Enqueue` and `Defer` fail `Unavailable` wrapping `db.ErrReadOnly` (db.md Proposed change 13). 5 arguments are record references only (`signals.Payload`), so no personal data sits in the queue, in a log line or on a span; a handler runs as the system (no actor, no session, `InJob` true) and calls system paths, as a signal handler does. 6 retries by kind: `Unavailable` and `Timeout` retry with `DefaultRetry` (ten attempts, 1 m doubling to 1 h, about four hours in all), `Canceled` while ctx is done releases the row without counting an attempt, every other kind and a panic fail the job at once (a bug is not retried); an admin may `Retry` a failed job within `DoneTTL`. 7 scheduled jobs never overlap themselves (a slot is inserted only while no row of that name waits or runs) and never catch up (at most the current slot; a process down for a week runs a purge once, not 168 times). 8 a row whose name no handler in this process knows (a module disabled since) fails `no_handler` with one log line, retryable once the module is back. 9 `signals.redeliver` is a job like any other: `Defer` stores the `Delivery` with `RunAt` = now + backoff for the next attempt and key `<signal id>:<subscriber>`; its handler calls `Redeliverer.Redeliver` with `Attempt` incremented, drops `NotFound` with a log line, and gives up after `DefaultRetry.MaxAttempts` counting the first delivery. 10 notify keeps its own sender loop (notify decision 2; 09 §6 "jobs may host the tick later"): nothing moves in M4. 11 the first scheduled jobs are `audit.purge` (hourly; `app` registers the closure over `audit.Log.Purge` and the `audit` settings reader, skipping when `audit.retention` is 0), `jobs.purge` (hourly; finished rows older than `DoneTTL`), and, registered by `app` through `runner.Scoped("files")` with closures over the files service like `audit.purge`, `files.reap` (hourly) and `files.sweep-orphans` (daily) (D44). 12 no manifest field: `Deps.Jobs` is scoped by name prefix, `Register` in `Start` is the declaration, and nothing crosses modules, so the graph check has nothing to check. 13 `Every` is the only schedule form in M4 (`now.Truncate(Every)` in UTC is the slot); a daily time in `regional.timezone` is Later (Q-JOBS4). 14 regional differences: none here; what runs is the modules' retention settings and the regional's data. 15 the status screen and the three admin actions ship in M4 (Q-JOBS1). 16 a lapsed lease is settled by a **reclaim** step at the start of every writer pass, not only by the next lease: back to `queued` (`lost_lease`, the attempt stays counted) while attempts remain, `failed gave_up` once `attempts >= Retry.MaxAttempts`; the lease's lapsed arm requires `attempts < Retry.MaxAttempts` too, so a row that kills its process at its last attempt is never leased again. 17 the no-overlap test (decision 7, and `RunNow`'s F25) is serialized by `pg_advisory_xact_lock(hashtextextended('core.job:' || name, 0))`, taken in the same `Write` transaction immediately before the slot insert or `RunNow`'s insert; under `READ COMMITTED` a `NOT EXISTS` cannot see another transaction's uncommitted row, and the lock makes the second insert start after the first committed; the scheduler locks names in sorted order (no deadlock); nothing else takes it. 18 `Retry` of a failed row whose name is scheduled is refused with `Conflict` (code `running`) while another row of that name is `queued` or `running`, under the same advisory lock as `RunNow` (Q-JOBS8, D44); on-demand names are unaffected; a failed row whose name is not registered in this process (its module is disabled) is treated as on-demand: no open-row guard, and the retried row fails `no_handler`. 19 a run whose heartbeats keep failing is canceled when the clock reaches the fence without a successful renewal: `outcome=lost_lease`, nothing more written to the row (Q-JOBS9, D44). Because the check runs at the heartbeat, the run is canceled on a failed renewal when the next beat would land at or past the fence: up to one beat (`Lease/3`) before the fence, never after. 20 a reclaim's requeue counts in `Ran.Released` and `bms.jobs.runs{outcome=lost_lease}` (Q-JOBS11, D44). 21 `jobstest.NewFake(owner, opts ...Option)` with `WithClock(clock.Clock)` bounds `run_at` by that clock; `Request.Validate` keeps the wall clock (Q-JOBS12, D44). 22 a dedupe `Key` never carries a person ID: key by the record's own ID; the conformance leak rule `jobs_key_not_person` checks it (Q-JOBS13, D44). 24 (J-W1, D58) a job's title is the catalogue key `module.<owner>.job.<ident>.title` in the owner's catalogue, `Decl` gains no field, and `TestMessageCatalogues_EnglishCoversJobs` fails the build when one is missing. 25 (J-W1, D58) `StatusView.Recent` is replaced by `Failed`, `Running`, `Retrying`, `Waiting` (each capped at `MaxRecent` on the server) plus `Instance`, so a long healthy queue cannot hide a failure. `bms check` prints a module's `Start` failure in its jobs section and does not fail the check (Q-JOBS10, D44; `app`).

**Errs kinds callers handle.** `Register`: `Invalid` (name, `scope` (another owner), nil handler, `every`/`timeout` `range`), `Conflict` (name registered twice), `Internal` (after `Run`). `Enqueue`: `Invalid` (name, `scope`, `unknown` (no handler registered), `key` `malformed`/`too_long`, `args`, `run_at` `range`), `Unavailable` wrapping `db.ErrReadOnly`, store kinds (`Unavailable`, `Timeout`, `Canceled`). `Defer`: `Invalid` (delivery), `Unavailable` wrapping `db.ErrReadOnly`, store kinds, `Unavailable` after `Close`. `RunOnce`: `Unavailable` wrapping `db.ErrReadOnly` (nothing leased), store kinds. Admin methods: `Unauthenticated`, `Permission`, `NotFound` (job, name), `Invalid` (`RunNow` of an on-demand name), `Conflict` (`status`: retry of a row not `Failed`; cancel of a row not `Queued`; run-now while a row of that name waits or runs), `Unavailable` wrapping `db.ErrReadOnly`. `Run`: `Internal` (twice). `New`/`Options.Validate`: `Invalid` with fields `jobs.db`, `jobs.clock`, `jobs.logger`, `jobs.tracer`, `jobs.meter`, `jobs.audit`, `jobs.authz`, `jobs.actor`, `jobs.redeliverer`, `jobs.instance`, `jobs.poll_interval`, `jobs.lease`, `jobs.run_timeout`, `jobs.workers`, `jobs.retry`. No sentinels of its own.

**Owns.** Table `core.job` (no personal data; `args` holds record references only; `internal`); the two permissions; the three audit actions; names `jobs.purge` and `signals.redeliver`; route set `jobs`: `GET /` (`Routes()` says `/`; `app` converts and the api shell mounts it as `{$}`), `POST /registered/{name}/run`, `POST /queue/{job}/retry`, `POST /queue/{job}/cancel`; screen `/jobs`; keys `core.jobs.*`; spans `jobs.tick`, `jobs.enqueue` (Producer), `jobs.defer` (Producer), `jobs.run` (Consumer), `jobs.<use-case>`; metrics `bms.jobs.runs{job, module, outcome}`, `bms.jobs.queue{status}` (gauge), `bms.jobs.lag` (histogram, seconds from `run_at` to start).

**Expects of dependencies.** `db`: `Transact`, `TxMode`, `ReadOnly`, `ErrReadOnly`, `Wrap`, `*Error.Constraint`, `Set`, `None`, `dbtest`; `jobs` is already in the fixed set order. `signals`: `Delivery` (+`Validate`), `Deferrer`, `Payload` (+`Validate`, JSON round trip), `Dispatcher.Redeliver` (`NotFound` for no handler, the handler's error unchanged, honours cancellation), `InDelivery`. `audit`: a `Recorder` scoped `jobs`; the `ActorFunc` may answer `ActorSystem` (audit amendment 2026-10-20, approved D44), or every `Record` from a module's handler is `Internal`. `authz`: `Allow`, `SiteAdminRoles`. `telemetry`: typed attributes, `TraceIDFrom`, `telemetrytest`; the metric rows (Proposed change 5). `module-contract`: `Deps.Jobs jobs.Registrar` scoped by name; `jobstest.NewFake` in `Conformance`; `jobs` a reserved core area. `app`: part 8 (declare, build, link, the runnable, `lazyDeferrer`, the `audit.purge` closure, `auditActor` answering `ActorSystem` when `InJob`); `bms check` prints the registered jobs. `web-shell`: `web/src/core/jobs/` as a core area.

**Open questions:** Q-JOBS1 to Q-JOBS7 below (the screen in M4, the give-up policy, notify's tick staying put, daily schedules Later, `DoneTTL` a constant, the audit actor through `InJob`, no new config keys); Q-JOBS8 to Q-JOBS13 (from the review: `Retry` while a row of the name is open, a run past its fence, `bms check` and a failing `Start`, counting a reclaim, the fake's clock, no person ID in a dedupe key). Q-JOBS14 (the latest request for a key wins, in place of "a running or final row makes the request a no-op"). All fourteen are settled (D43, D44; Q-JOBS14 D50). **Proposed changes elsewhere:** 11, listed at the end, all applied 2026-10-20 (D43, D44); 13 and 14 (J1) applied 2026-10-23 (D50).

**Amended 2026-10-28 (D52).** A run's own re-enqueue of its key keeps `attempts` when the run then fails and is applied only while attempts are left, else the row ends `failed` (`gave_up`) (`next_by_run`; outside requests still reset, D50); `jobstest.Fake` runs the row's current `Args`, tracks rows `Run` creates, requeues retryable errors (attempt counted, up to `DefaultRetry.MaxAttempts`) and skips retry delays.

**Amendment J-W1 (2026-10-30, D57; Approved D58; Implemented 2026-10-30).** The screen for people: a job's title is `module.<owner>.job.<ident>.title` in the owner's catalogue (decision 24; `TestMessageCatalogues_EnglishCoversJobs` fails the build when one is missing; `Decl` unchanged); `StatusView` = `{ReadOnly, Instance, Registered, Counts, Failed, Running, Retrying, Waiting}`, each list capped at `MaxRecent` on the server, `Recent` gone (decision 25; `Retrying` = `queued` with `last_error` set); `/jobs` shows titles only (never an ID, a key, an argument or a raw name except as the fallback), the sections Failed / Running now / Waiting to retry / Waiting / Jobs with a counts line, "server" for an instance and only for a run by another server, a 403 as the shell's error block with the reference; `Table` headers `break-normal`. F59 to F67; Q-JOBS15 (keys hidden entirely; a link from a waiting row to its record is a possible later addition) and Q-JOBS16 ("server" is the word for an instance) answered (owner, 2026-10-30, D58); Proposed changes 15 to 19, applied 2026-10-30 (D58) (web-shell, module-contract decision 8 and the two skills, module-volunteers, audit, app).

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

// MarshalText makes a Status a JSON string and a usable map key (StatusView.Counts on the
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
	Key   string    // optional dedupe key, [A-Za-z0-9._:-]{1,MaxKeyBytes}; "" = none. Name the
	                    // record's own ID (the sign-up, the shift, the upload), never a person ID:
	                    // keys appear in GET /api/jobs/ and in logs, never on the screen (jobs_key_not_person)
	Args  Args      // may be empty
	RunAt time.Time // zero = now; else at most MaxDelay ahead (the past is "now")
}

// Validate fails with errs.Invalid joining a validate.FieldError per problem: name, key
// (malformed, too_long), args (signals.Payload.Validate), run_at (range) (F3); Validate uses the wall clock.
func (r Request) Validate() error

// ValidateAt is Validate against the given clock reading instead of the wall clock
// (accepted deviation, 2026-10-20: lets jobstest's fake validate against its own clock).
func (r Request) ValidateAt(now time.Time) error

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
	// db.Transact(ctx, db.Write): inside the caller's Write transaction that joins it, so
	// the row commits or rolls back with the caller's change; outside any transaction it is
	// one short transaction of its own; inside a Read transaction db refuses the nesting with
	// errs.Invalid (F42). With a Key, the latest request wins (decision 3): a Queued
	// row with the same (name, key) is moved to r.RunAt with r.Args (a reschedule); a
	// Done, Failed or Canceled row is reopened as a fresh Queued row at r.RunAt with r.Args
	// (attempts 0; F43); a Running row keeps running and holds r.RunAt and r.Args as its
	// next request, which the runner applies when the run leaves Running, whatever its
	// outcome (F53): the row is then Queued at r.RunAt and never ends with the request
	// unheard. A handler may call Enqueue on its own Key from inside its run to run again
	// later (F54). In every case the key's row ID is returned. A caller that wants two
	// runs to coexist keys them distinctly (for example by the shift's start time).
	// Fails errs.Invalid for an invalid Request (F3), a name outside the
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
// (signal, subscriber) is a no-op in every state (its own insert, ON CONFLICT DO NOTHING;
// unchanged: a delivery is immutable, so a repeat is the same work, not a newer
// request).
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
// so a test or bms check that is mid-tick is never left writing to a closed runner.
// Idempotent. Fails with ctx's kind when the workers are still returning when ctx ends
// (F19).
// Consumer: app (the runnable's stop, after HTTP drained, before signals.Close).
func (r *Runner) Close(ctx context.Context) error

// Declared returns what is registered in this process, sorted by name, with no I/O and
// no permission check: Owner, Every and the effective Timeout are filled; Last is nil and
// NextDue is zero (those need the database). Cannot fail: it reads the registry only.
// Consumer: bms check (prints the "jobs" section against db.None(); Status would need a
// session and a database, RunOnce would run things).
func (r *Runner) Declared() []Registered

// Status returns what is registered in this process with each name's last row and next
// due slot, the queue counts, and the Failed, Running, Retrying and Waiting rows (each list at most MaxRecent, J-W1 decision 25). Works
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
// errs.Conflict (code "running") for a scheduled name while another row of it is Queued
// or Running (F51),
// errs.Unavailable wrapping db.ErrReadOnly.
// Consumer: the status screen.
func (r *Runner) Retry(ctx context.Context, id JobID) error

// Cancel marks a Queued row Canceled, recording jobs.job.canceled. Needs jobs.manage.
// Fails errs.Unauthenticated, errs.Permission, errs.NotFound, errs.Conflict (code
// "status") for a row not Queued (F29), errs.Unavailable wrapping db.ErrReadOnly.
// Consumer: the status screen.
func (r *Runner) Cancel(ctx context.Context, id JobID) error

// Routes returns the route set (relative to /api/jobs: "GET /", the three POSTs; the api
// shell mounts a trailing "/" as "{$}"; OpenAPI its fragment. Cannot fail.
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
	Instance   string         // this process's Options.Instance; the screen marks a run by any other server (J-W1 W1.6)
	Registered []Registered
	Counts     map[Status]int // every row by status
	Failed     []Record       // status failed; newest finished_at first; at most MaxRecent
	Running    []Record       // status running; earliest started_at first; at most MaxRecent
	Retrying   []Record       // status queued with last_error set; soonest run_at first; at most MaxRecent
	Waiting    []Record       // status queued with last_error null; soonest run_at first; at most MaxRecent
}

// Ran counts one tick.
type Ran struct {
	Scheduled int // slots inserted
	Ran       int // handlers that returned nil
	Failed    int // final failures (including no_handler and gave_up)
	Retried   int // rows re-queued with backoff
	Released  int // rows given back (Canceled, read-only, lost lease, a reclaim's requeue)
}

// ---- Constants ----

const (
	MaxNameBytes        = 96          // exported: the Name grammar's byte limit (signals.Name)
	MinTimeout          = time.Second // exported: the floor of Decl.Timeout and Options.RunTimeout
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
// demand. It keeps one row per (name, key) (and one per keyless request) in the real
// states, and moves it as the real queue does (decision 3, decision 23): Enqueue moves
// a queued row, reopens a final one, and holds the request on a running one; Run marks the
// row running for the handler's duration and then applies the held request (queued at its
// RunAt) or, with none, ends the row done (nil) or failed (any error; the Fake does not
// model backoff). Safe for concurrent use.
type Fake struct{ /* unexported */ }

// Option configures a Fake. WithClock(c) makes the Fake bound Request.RunAt (at most
// MaxDelay ahead) against c; the default is the wall clock. Request.Validate itself keeps
// the wall clock, because a value type has no clock to take; its doc says so.
type Option func(*Fake)

func WithClock(c clock.Clock) Option

func NewFake(owner string, opts ...Option) *Fake
func (f *Fake) Register(d Decl) error
func (f *Fake) Enqueue(ctx context.Context, r Request) (JobID, error)
func (f *Fake) Registered() []Decl
// Enqueued is every request heard so far, in order: the log. A test that asserts what is
// scheduled asserts Queued instead (a request made during a run shows here at once and in
// Queued only after the run).
func (f *Fake) Enqueued() []Request
// Queued is the rows waiting now, as the queue would hold them: one Request per row with
// the effective RunAt and Args, sorted by RunAt then Key. A running row is absent; a row
// ended by Run is absent until a new Enqueue reopens it. Cannot fail.
func (f *Fake) Queued() []Request
// Run calls the handler registered for name with j (InJob true, no transaction) and
// returns its error unchanged; errs.NotFound when none is registered (F44). When j.Key is
// set, the key's row is running during the call and leaves running by the key rule: a
// request held on it becomes the waiting row (Queued shows it); otherwise the row ends,
// done or failed. A Key with no row (a test drove the handler directly) gets one, tracked like an enqueued
// row (D52(c)). A retryable error (Unavailable, Timeout) requeues the row; the Fake skips
// the retry delay.
func (f *Fake) Run(ctx context.Context, name Name, j Job) error
// RunDue runs, in (RunAt, Key) order, every waiting row with RunAt <= now, once each (rows
// requeued by their own run at or before now are left for the next call, so a handler
// that always re-enqueues itself cannot loop a test), each as Run does with a Job built
// from the row's current state (Attempt 1, the row's ID, Key, current Args and RunAt).
// The Fake skips retry delays: a row requeued after a retryable error is due at once
// (D52(c)). It returns how many ran and
// the handlers' errors joined (a failing row ends failed and the others still run). It is
// how a test drives a schedule across a fake clock (volunteers F112b). Fails only with the
// handlers' errors.
func (f *Fake) RunDue(ctx context.Context, now time.Time) (int, error)
// Fail makes the next Enqueue return err, once: the call after that succeeds again
// without a second Fail; nil clears a pending failure. One-shot because a use case under
// test enqueues once and the test then asserts the rollback; a sticky failure would hide a
// second enqueue in the same test.
func (f *Fake) Fail(err error)
```

### Method, consumer, pre- and post-conditions

| Method | Consumer | Precondition | Postcondition |
|---|---|---|---|
| `Register` | modules in `Start`; `app` for `audit.purge`; the runner for its own two | before `Run`; valid `Decl`; name in scope | the name has a handler in this process; a scheduled name gets a slot each `Every` |
| `Enqueue` | modules' use cases; `RunNow` | a handler registered for the name here; valid `Request` | the key's row holds this request and commits with the caller: `Queued` (new, moved or reopened), or `Running` with the request held for the end of the run |
| `Defer` | `signals` | valid `Delivery`; writer | a `signals.redeliver` row waits with backoff |
| `Run`/`Close` | `app` | once | due rows run on the writer; stopped rows are released |
| `RunOnce` | tests, `bms check` | writer | one tick done, counts returned |
| `Status` | the screen | `jobs.read` | a consistent view (one `Read` transaction) |
| `RunNow`, `Retry`, `Cancel` | the screen | `jobs.manage`; the row's status as required | the row changed; one audit entry in the same `Write` transaction |
| `Scoped` | `app` | valid owner | a view accepting that owner's names |
| `InJob` | `app`'s audit actor, modules' handlers | — | true inside a handler |
| `Declared` | `bms check` | — | the registered names with owner and schedule, no I/O, no permission |

## Types

Defined here: `Name`, `JobID`, `Status`, `Job`, `Handler`, `Decl`, `Request`, `Registrar`, `Redeliverer`, `Options`, `Runner`, `Route`, `Record`, `Registered`, `StatusView`, `Ran`. Aliased: `Args = signals.Payload`. Uses `model.ID`, `signals.Delivery`, `signals.Signal`, `retry.Policy`, `authz.Permission`, `authz.PermissionDecl`, `audit.Entry`.

Record types shared by the service and `postgres/` live in the leaf package `internal/core/jobs/rows` (`rows.Job`, `rows.Lease`), importing only `model` and `lib`, as `people/rows` and `authz/rows` do.

## Data

Schema `core`, migration set `jobs` (`db.Set{Name: "jobs", Schema: "core"}`), files `0001_job.sql`, `0002_job_next.sql` and `0003_job_next_by_run.sql` (adds `next_by_run boolean NOT NULL DEFAULT false` and its check; D52(a)); `0002` adds `next_run_at`, `next_args` and `job_next_chk`; `ALTER TABLE ... ADD COLUMN` with a constant `DEFAULT false` for `next_by_run` (no table rewrite on PostgreSQL 11 and later) and none for the others, safe on a live table). `jobs` sits in `coreSetOrder` after `settings` and before `notify` already. Timestamps `timestamptz`, from the service's clock (no defaults).

**`core.job`**

| Column | Type | Meaning |
|---|---|---|
| `id` | uuid PK | `JobID`; never the nil UUID (check) |
| `name` | text | `Name`; check: the grammar, <= 96 bytes |
| `key` | text NULL | the dedupe key; check `^[A-Za-z0-9._:-]{1,128}$`; never a person ID (a module keys by the record's own ID; `jobs_key_not_person`); a schedule slot stores the slot time in RFC 3339 UTC, an admin run `admin:<job id>`, a redelivery `<signal id>:<subscriber>` |
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
| `next_by_run` | boolean NOT NULL DEFAULT false | (`0003_job_next_by_run.sql`) whether the latest held request came from the run itself (its row ID in ctx); set on `Enqueue`/`Run` while the row is `running`, the latest request deciding; check: true only while a request is held (`next_run_at IS NOT NULL`); read by fail, requeue, release and the reclaim (decision 23), cleared with the request |
| `next_run_at` | timestamptz NULL | (`0002`) the `RunAt` of a request made by `Enqueue` while the row was `running`; set only while `status = 'running'` and only with `next_args` (check `job_next_chk`: `(next_run_at IS NULL) = (next_args IS NULL) AND (next_run_at IS NULL OR status = 'running')`); applied and cleared by the statement that takes the row out of `running` (decision 23) |
| `next_args` | jsonb NULL | (`0002`) the `Args` of that request; same content rule and size check as `args` (`job_next_args_chk`); references only, `internal` |
| `created_at`, `updated_at` | timestamptz | every state change bumps `updated_at` |

Indexes: `job_name_key_uidx UNIQUE (name, key) WHERE key IS NOT NULL` (the dedupe; `Enqueue` and the scheduler use `ON CONFLICT`; it stays over every status, because the scheduler's slot insert relies on a `done` slot still conflicting for the rest of its period); `job_due_idx (run_at, id) WHERE status IN ('queued', 'running')` (the lease); `job_name_open_idx (name) WHERE status IN ('queued', 'running')` (the no-overlap test and `RunNow`'s conflict); `job_finished_idx (finished_at) WHERE status IN ('done', 'failed', 'canceled')` (the purge); `job_name_created_idx (name, created_at DESC)` (`Status`'s last row per name).

**Personal data: none.** `args` holds record references, which may include a person's ID as a signal payload does (signals decision 4); the row is `internal`, carries no name, address or narrative, and a deleted person's ID may linger in a finished row for at most `DoneTTL` (pseudonymous, as audit rows are; audit decision 10). There is no foreign key to `core.person`: a job about a deleted person runs, finds nothing, and ends `Done`. `jobs` is therefore not a `people.Source`. Retention: final rows are deleted by `jobs.purge` after `DoneTTL` (a constant, Q-JOBS5); waiting rows live until they run or are canceled.

## Permissions

| Permission | Allows | Scoped by | Reaches | Default roles |
|---|---|---|---|---|
| `jobs.read` | `GET /api/jobs/`: the registered names, last runs, counts, and the failed, running, retrying and waiting rows | site | `internal` | `authz.SiteAdminRoles` (every site admin starter role) |
| `jobs.manage` | `RunNow`, `Retry`, `Cancel` | site | `internal` | `settings-admin` |

`Register`, `Enqueue`, `Defer` and the runs check no permission: they are the system's, and the caller's use case was already authorized (decision 5; notify decision 12 is the precedent). Reads of `core.job` by the runner are system reads of non-personal data: not audited. The three admin actions are audited (`jobs.job.run|retried|canceled`, `Record` = `Ref{Kind: "jobs.job", ID}`, `Reason` = the job name) inside their `Write` transaction. `Status` is not audited (nothing personal).

## Signals

Published: none (a job's completion is not a domain event; a module that wants to announce what its job did publishes its own signal from the handler, inside the handler's transaction).

Subscribed: none. The runner reacts to deliveries only through `signals.Deferrer` (decision 9).

## Settings

None. Nothing a regional would set differently lives here: what runs is each area's own setting (`audit.retention`, a module's `<name>.retention*`, a reminder lead time in volunteers) read by that area's handler at run time. `PollInterval`, `Lease`, `RunTimeout`, `Workers` and `Retry` are `Options` filled by `app` from constants in M4 (Q-JOBS7); `DoneTTL` is a constant (Q-JOBS5).

## UI

Core area `jobs`, screen `/jobs` ("Background work"), in the admin navigation for holders of `jobs.read`; phone-first like every screen. Catalogue keys `core.jobs.*`. The full screen is Amendment J-W1, W1.6 (applied here, D58); in short:

- **Titles only.** A job is shown by its catalogue title (`module.<owner>.job.<ident>.title`, W1.4), grouped under its owner's title; never an ID, a key, an argument or a raw name (the raw name appears only as the fallback when a key is missing). Arguments are never shown.
- **Sections** under their own `h2`, top to bottom: a counts line of in-page links (for the phone), **Failed** (Retry), **Running now** (no action), **Waiting to retry** (Cancel), **Waiting** (Cancel), **Jobs** (a card per name: schedule, last run, next due, **Run now** for scheduled names). Actions need `jobs.manage`. Each section has its own empty state.
- **Read-only banner:** when `StatusView.ReadOnly`, the screen says this server runs no jobs and hides the three actions. "Server" is the word for an instance, shown only for a run by another server.
- **Error state:** the shell's `ErrorState` with the reference and Retry. **Forbidden:** the shell's forbidden block for a missing `jobs.read` (no request) or a 403 on the read (with the reference, no Retry); a 403 on an action shows the shell's error block beside the control with the reference.
- The kit's `Table` headers never break inside a word.

`bms check` prints, under a "jobs" heading, each registered name with its owner and schedule (no database read), so an operator can see what a build will run.

## Behavior

**Lifecycle.** `New` does no I/O and registers `jobs.purge` (hourly, `Timeout` 10 m) and `signals.redeliver` (on demand). `app` builds the runner in part 8 after audit, settings and authz (it takes their seams), sets `a.deps.Jobs` (scoped by `provision` with `Scoped(m.Name)`), mounts the route set, and in `link` registers `audit.purge` (hourly) with a closure over `audit.Log.Purge` and the `audit` settings reader (`Duration("retention")`; 0 = skip). Modules register in `Start`. `Run` starts after `Set.Start` (so every registration is in) and freezes `Register`. Stop order: HTTP drained, **jobs stopped** (running handlers get `Canceled`, rows released), `signals.Close`, `Set.Stop`, `db.Close` (app D10, signals decision 6). `Close` is idempotent and waits for the workers, and for any `RunOnce` in flight, under the ctx it is given; a run that ignores cancellation is abandoned at the deadline and its row stays `running` until its lease lapses, when the next writer pass reclaims it (step 2a).

**A tick** (`jobs.tick` span; one per `PollInterval`, and at once on start):
1. If `db.ReadOnly()`: count `outcome=read_only` once per flip, log once, return. Nothing is leased, inserted or purged on a read-only copy (decision 4).
2a. **Reclaim** (`reclaim_expired_leases.sql`; runs before step 2, and is labelled 2a so the step numbers cited elsewhere in this spec keep their meaning). In one `Write` transaction, take up to `MaxBatch` rows with `status = 'running' AND leased_until < now` (`FOR UPDATE SKIP LOCKED`, by `(run_at, id)`) and settle them: `attempts < Retry.MaxAttempts` → `queued`, `run_at` unchanged (due now), `leased_until = NULL`, `last_error = 'lost_lease'`, the attempt stays counted; `attempts >= Retry.MaxAttempts` → `failed`, `last_error = 'gave_up'`, `finished_at = now`. A `gave_up` row is logged and counted `outcome=failed` like a run that gave up; a requeued one is logged `outcome=lost_lease`, counted in `Ran.Released` and metered `bms.jobs.runs{outcome=lost_lease}` with the `job` and `module` tokens from the row's name (Q-JOBS11; `TestRunOnce_ReclaimedRow_CountsReleasedAndLostLease`). This is the only way a row whose handler kills its process every time (a fatal, an OOM) ends: step 3 alone would re-lease it forever, and before this step it did, so one poison row took the server down once per `Lease`. A copy that is read-only (step 1) reclaims nothing (F47).  Both arms first apply a held request (decision 23): a lapsed row with `next_run_at` set becomes `queued` at it with `attempts = 0` whatever its attempts, counted and logged as `lost_lease` with `next=true` (F56).
2. **Schedule.** For each registered name with `Every > 0`, in sorted name order: `slot = now.UTC().Truncate(Every)`, `key = slot.Format(time.RFC3339)`; take the name's advisory lock (`lock_job_name.sql`, decision 17, then insert `(name, key, run_at = slot)` with `ON CONFLICT (name, key) DO NOTHING` and only `WHERE NOT EXISTS (open row of that name)` (decision 7). Two statements per name, all in one `Write` transaction; the locks are held to its commit. Two processes on one database insert each slot once (the unique index), and a tick racing a `RunNow` of the same name leaves at most one open row (F49).
3. **Lease and run.** While a worker is free: in one short `Write` transaction, pick one due row (`status = 'queued' AND run_at <= now`, or `status = 'running' AND leased_until < now AND attempts < Retry.MaxAttempts`: a crashed owner with attempts left; a lapsed row at its last attempt is left for step 2a) by `(run_at, id)` with `FOR UPDATE SKIP LOCKED`, set `status = 'running'`, `leased_until = now + Lease`, `attempts + 1`, `started_at` (if null), `instance`, and `last_error = 'lost_lease'` when it was `running`; return the row. `leased_until` comes from the clock parameter and is returned, as notify's lease does, so Go holds the exact stored value as the fence. No due row: the tick ends.
4. Hand the row to a worker goroutine (below). The tick does not wait for runs (`RunOnce` does).

**A run** (`jobs.run` span, `SpanConsumer`, attributes `job` token, `module` token (the owner), `job_id` ID, `attempt` int, `outcome` token; a child of the enqueuer's trace when `trace_id` is set, through `WithParentFrom`-style linking from the stored trace ID; otherwise a root):
1. Find the handler. None registered here → `UPDATE ... SET status='failed', last_error='no_handler'` fenced by `leased_until`; log once; `outcome=no_handler` (decision 8).
2. Derive `ctx`: `context.WithoutCancel(runnerCtx)` for values, plus the runner's own cancellation (shutdown), plus `WithTimeout(Decl.Timeout)`, plus the `InJob` marker. No transaction, no session.
3. Start the heartbeat: every `Lease/3`, `UPDATE core.job SET leased_until = $new WHERE id = $1 AND leased_until = $old AND status = 'running' RETURNING leased_until`; a renewal that returns no row means the lease was lost (another process re-leased a row whose owner looked dead): cancel the run's ctx, `outcome=lost_lease`, write nothing more to the row (it is someone else's now). A renewal that fails with a store error is retried at the next beat; the run goes on while the lease has not expired. When the clock reaches the fence (`now >= leased_until`) without a successful renewal, the run's ctx is canceled: `outcome=lost_lease`, nothing more written to the row, as for a renewal that found the lease taken (F52); a failed renewal cancels already when the next beat would land at or past the fence (up to one beat, `Lease/3`, before it; never after); the row's outcome belongs to whoever leases it next.
4. Call the handler under `recover`. A panic is logged once with its stack (`error.kind=internal`, the job name and ID, never the args), counted `outcome=panicked`, and the row fails with `last_error='panic'`.
5. **Outcome by kind**, written in one fenced `UPDATE` (`WHERE id = $1 AND leased_until = $fence`; no row updated means the lease was lost, see 3) within `recordTimeout` (5 s) on a ctx without the run's cancellation, so a shutdown never loses an outcome:
   - (decision 23) **first, in the same `UPDATE`:** when `next_run_at` is set, the row does not take the final or requeued state below but becomes the held request: `status = 'queued'`, `run_at = next_run_at`, `args = next_args`, `attempts = 0`, `started_at = NULL`, `finished_at = NULL`, `next_run_at = next_args = NULL`, `leased_until = NULL`, `last_error` = the token the outcome below would have written (NULL after nil); the statement `RETURNING status` tells the runner which happened, the log line gains `next=true`, `attempts` stays 0 unless the run itself made the held request (`next_by_run`, D52(a)): then fail and reclaim keep `attempts` and apply the request only while `attempts < Retry.MaxAttempts` (else the row ends `failed`, `gave_up`), requeue keeps `attempts`, release uses `attempts - 1`, and the outcome token, metric and `Ran` count stay those of the run itself (an `ok` run that left a waiting row is still `ok`). The reclaim's two arms (step 2a) apply the same rule, so a request held on a row whose process died is not lost either. A fenced-out write (lost lease) applies nothing: the lessee's own exit will.
   - nil → `done`, `finished_at = now`, `leased_until = NULL`; `outcome=ok`.
   - `errors.Is(err, db.ErrReadOnly)` → release: `queued`, `attempts - 1`, `leased_until = NULL`, `run_at` unchanged; the runner pauses until the next tick finds `ReadOnly()` false; `outcome=read_only` (db.md Proposed change 13).
   - `Canceled` while `ctx.Err() != nil` (shutdown or lost lease) → release as above, `last_error='canceled'`; `outcome=canceled`.
   - `Unavailable` or `Timeout` (`retry.Retryable`) and `attempts < Retry.MaxAttempts` → `queued`, `run_at = now + Retry.Delay(attempts + 1, u)`, `last_error = <kind>`; `outcome=retried`.
   - `Unavailable` or `Timeout` at the last attempt → `failed`, `last_error='gave_up'`; `outcome=failed`.
   - any other kind (`Invalid`, `NotFound`, `Conflict`, `Permission`, `Unauthenticated`, `Unsupported`, `Internal`, `Canceled` with a live ctx) → `failed`, `last_error = <kind>`; `outcome=failed`. A bug is not retried; the log line carries the kind and the op, the admin can `Retry` after a fix.
6. `bms.jobs.lag` records `started_at - run_at` in seconds; `bms.jobs.runs{job, module, outcome}` counts; the span records the error where it is handled (here), so the handler should not also log it.

**The redelivery job** (`signals.redeliver`): `Defer` stores the `Delivery` (decision 9); the handler decodes it, sets `Attempt = row.attempts + d.Attempt` (the first delivery counts), calls `Redeliverer.Redeliver`. Its errors follow the table above with two readings: `NotFound` (no handler: the module was disabled since) → `done` with a log line and `outcome=dropped` (signals Proposed change 6), and the handler's own `Unavailable`/`Timeout` → retried until `Retry.MaxAttempts` counting the first delivery, then `gave_up`. The subscribing module's spec says how its handler is idempotent on `Signal.ID` (people's `identity.account-created` handler is: `ON CONFLICT DO NOTHING`).

**Enqueue** (`jobs.enqueue` span, `SpanProducer`): validate; check the name is registered here and in scope; stamp `trace_id` from `telemetry.TraceIDFrom(ctx)`;  one `INSERT ... ON CONFLICT (name, key) WHERE key IS NOT NULL DO UPDATE ... RETURNING id` whose `SET` branches on the existing row's status: `queued` → `run_at = EXCLUDED.run_at, args = EXCLUDED.args` (moved); `done`, `failed` or `canceled` → the same plus `status = 'queued', attempts = 0, started_at = NULL, finished_at = NULL, last_error = NULL, instance = NULL` (reopened; `created_at` keeps the row's birth); `running` → `next_run_at = EXCLUDED.run_at, next_args = EXCLUDED.args, next_by_run = (core.job.id = the run's row ID from ctx, `rules.RunID`)` and nothing else (held; `status`, `run_at`, `args`, `attempts` and `leased_until` untouched, so the heartbeat's and the outcome's fences still match); in every branch `trace_id = coalesce(EXCLUDED.trace_id, core.job.trace_id)` and `updated_at`. The statement always returns the key's row ID, so the old "zero rows, then `SELECT id`" path and its purge race are gone: an `ON CONFLICT DO UPDATE` that meets a row a concurrent purge is deleting waits and then inserts. The upsert row-locks the existing row for the caller's transaction, as it did for a queued row; a heartbeat or outcome write of a `running` row blocked by that lock waits inside its `recordTimeout`, and a use case must not hold a `Write` transaction open for seconds around `Enqueue` (it never should). Inside the caller's transaction (`db.TxMode(ctx)` reports `Write`) the statement runs on it; inside a `Read` transaction it fails `Invalid` (db's rule for a write nested in a read); outside any, it is standalone. No `AfterCommit` hook is needed: the row is the commit.

**RunNow** takes the name's advisory lock (decision 17 and then inserts `(name, key = "admin:<new id>", run_at = now)` only `WHERE NOT EXISTS (open row of that name)` (else `Conflict running`), with the audit entry in the same `Write` transaction; two concurrent `RunNow`s of one name end with one row and one `Conflict running` (F48). The lock does not fail on a read-only copy (advisory locks are allowed in a read-only transaction); the insert that follows is what answers `25006`. **Retry**: for a scheduled name it takes the name's advisory lock first (decision 17), then `UPDATE ... SET status='queued', attempts=0, run_at=now, last_error=NULL, finished_at=NULL WHERE id=$1 AND status='failed' AND NOT EXISTS (open row of that name)` (the `NOT EXISTS` only for a scheduled name); no row → `NotFound` if the id is unknown, `Conflict status` if it is not failed, `Conflict running` if another row of the name is open (F51); on-demand names are unaffected; a failed row whose name is not registered in this process (its module is disabled) is treated as on-demand: no open-row guard, and the retried row fails `no_handler`. **Cancel** likewise on `status='queued'`.

**Purge** (`jobs.purge`, hourly): delete final rows with `finished_at < now - DoneTTL` in batches of `MaxBatch` with `FOR UPDATE SKIP LOCKED` until a batch comes back short (notify's purge shape). Counted in the run's span; no audit entry (nothing personal).

**Concurrency.** `Workers` goroutines per process; a module's handler may run concurrently with its other jobs and with the same name under different keys, never with the same row (the lease) and, for a scheduled name, never with its own previous slot (decision 7). Two writer processes on one database (a restart overlap, or an operator who ran two) share rows through `SKIP LOCKED` and the unique slot key, and serialize the per-name "no open row" test through the advisory lock (decision 17: `Enqueue`, the lease, the heartbeat and the outcome writes never take it, so a lock holder blocks only another slot insert or `RunNow` of the same name, for one short transaction. The on-site/off-site case (D27) is a writer and a replica: the replica does nothing here; after a switchover the new writer picks up waiting rows and re-leases any whose lease lapsed. Clock skew between two writers moves slots and fences by the skew: a slot may be inserted up to the skew early and a lease may look expired up to the skew early; `Lease` of two minutes against NTP-synced hosts leaves that harmless, and the operator docs say the copies must keep time.

**Ordering.** Due rows run by `(run_at, id)`; no priorities. A long backlog delays everything equally; the `bms.jobs.lag` histogram shows it.

**Idempotency** is the handler's, by design (decision 2): the runner promises at-least-once per lease and never exactly-once. The spec says so out loud so no module relies on more: a reminder handler checks the sign-up still stands and that the reminder was not already sent (its own column or notify's idempotency `Key`), a purge is naturally idempotent, a redelivery reaches a handler that is idempotent on `Signal.ID`.

**Read-only copy.** `Register`, `Scoped`, `Status`, `Routes` work; the loop skips; `Enqueue`, `Defer`, `RunNow`, `Retry`, `Cancel` fail `Unavailable` wrapping `db.ErrReadOnly` (the api answers `503 read_only`); a run in flight when the copy flips releases its row at the next write that answers `25006`.

**Telemetry.** Spans and metrics as listed in the Summary; attributes are tokens, IDs, ints and kinds only; `args` never appear on a span, a log line or a metric; the log line per outcome carries `job`, `job_id`, `attempt`, `outcome`, `error.kind`. `bms.jobs.queue{status}` is a gauge refreshed at the end of each writer pass from one count query, and reports `queued` and `running` only: the final statuses are bounded by `jobs.purge` and visible in `StatusView.Counts`, and a gauge of `done` rows over seven days says nothing an operator acts on. Because the pass is skipped on a read-only copy, the gauge there is stale or absent; `StatusView.ReadOnly` is the signal that copy gives, and the writer's gauge is the one to watch. A handler's own spans are children of `jobs.run` through the ctx.

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
| F43 | `Enqueue` | same `(name, key)` already final (`done`, `failed`, `canceled`) | (the row is reopened `queued` at the new `RunAt` with the new `Args`, `attempts = 0`; its ID returned) | nothing; a key is reusable at once | `TestEnqueue_KeyFinal_ReopensRow` (replaces `TestEnqueue_KeyFinal_IsNoopReturnsExistingID`), `TestEnqueue_KeyCanceled_ReopensRow` |
| F44 | `jobstest.Fake.Run` | name not registered | NotFound | fix the test | `TestFakeRun_Unregistered_ReturnsNotFound` |
| F45 | the reclaim (step 2a) | a `running` row's lease lapsed and `attempts >= Retry.MaxAttempts` (the process died at the last attempt) | (row `failed`, `gave_up`; never leased again; one log, `outcome=failed`) | admin may `Retry` after a fix | `TestRun_CrashedAtMaxAttempts_FailsGaveUpInsteadOfLeasingAgain`, `TestRun_CrashedAtCustomMaxAttempts_FailsGaveUp` (integration) |
| F46 | the reclaim, the lease | a `running` row's lease lapsed and `attempts < Retry.MaxAttempts` | (row `queued`, `lost_lease`, attempt kept; leased again at once) | nothing; the handler runs again | `TestRun_CrashedBelowMaxAttempts_IsLeasedAgain` (integration) |
| F47 | the reclaim | read-only copy | (nothing written; the row waits for the writer) | nothing; the next writer pass reclaims it | `TestRun_CrashedAtMaxAttempts_ReadOnlyCopy_ChangesNothing` (integration) |
| F48 | `RunNow` | two concurrent `RunNow`s of one name | one row; the other Conflict (`running`) | 409 for the loser; the screen says "already waiting" | `TestRunNow_TwoConcurrent_OneWinsOneConflictRunning` (integration) |
| F49 | the scheduler, `RunNow` | a tick's slot insert races a `RunNow` of the same name at the slot boundary | (at most one open row of the name; the loser is a no-op slot or a Conflict) | nothing | `TestTick_RacingRunNowAtSlotBoundary_AtMostOneOpenRow` (integration) |
| F50 | `Status.MarshalText`, `UnmarshalText` | not a status | Invalid (`status`, `one_of`) | fix the value | `TestStatus_MarshalText_Invalid_ReturnsInvalid`, `TestStatus_UnmarshalText_Unknown_ReturnsInvalid` |
| F51 | `Retry` | a scheduled name has another row `queued` or `running` | Conflict (`running`) | the screen disables Retry with "already waiting" | `TestRetry_ScheduledNameOpen_ReturnsConflict` |
| F52 | a run | heartbeats fail until `now >= fence` | (`lost_lease`; ctx canceled; nothing more written to the row) | handler sees `Canceled`; the next lessee owns the outcome | `TestRun_HeartbeatsFailPastFence_CancelsAndWritesNothing`, `TestRun_HeartbeatJitterAfterRenewal_CancelsByTheFence` |
| F53 | `Enqueue` | same `(name, key)` is `running` | (the request is held on the row; its ID returned; when the run ends with nil the row is `queued` at the held `RunAt` with the held `Args`, `attempts = 0`, not `done`) | nothing; the request is applied after the run | `TestEnqueue_KeyRunning_HeldAndAppliedAfterOK` (integration: a handler blocked on a channel, `Enqueue` from the test, release, assert the row) |
| F54 | a run | the handler enqueues its own key from inside the run and returns nil | (as F53: the row is `queued` again at the handler's `RunAt`; `outcome=ok`, log `next=true`) | nothing; the handler runs again when due | `TestRun_HandlerReenqueuesOwnKey_RowQueuedAgain` (integration), `TestFakeRun_HandlerReenqueuesOwnKey_QueuedShowsIt` (jobstest) |
| F55 | a run | a held request exists and the run fails finally (any non-retryable kind, a panic) or retryably | (the row is `queued` at the held `RunAt`, `attempts = 0` (D52(a): **kept** instead when the run itself made the held request, and the request is then applied only while `attempts < Retry.MaxAttempts`; at the limit it is dropped and the row ends `failed` (`gave_up`): a narrow exception for self-made requests only, so a self-rescheduling failing job cannot retry forever), `last_error = <token>`; it is not `failed` and gets no backoff: the newer request supersedes the failed one; the failure is still `outcome=failed`/`retried` in the log, span and metric) | fix the bug if it recurs: the next run shows it with no held request | `TestRun_HeldRequest_FailedRunQueuesRequest`, `TestRun_HeldRequest_RetryableRunQueuesRequestWithoutBackoff` (integration) |
| F56 | a run, the reclaim | a held request exists and the run is released (shutdown, read-only, `lost_lease` reclaim) or reclaimed `gave_up` | (the row is `queued` at the held `RunAt`; an outside request: `attempts = 0`, and the release's own `attempts - 1` and the reclaim's `failed` do not apply; a by-run request (D52(a)): a release uses `attempts - 1`, a reclaim keeps `attempts` and, at `attempts >= Retry.MaxAttempts`, drops the request and the row ends `failed` (`gave_up`), see F55a) | nothing | `TestRun_HeldRequest_ShutdownQueuesRequest`, `TestReclaim_HeldRequest_QueuesRequestInsteadOfGaveUp` (integration) |
| F55a | a run, the reclaim | the run re-enqueued its own key (`next_by_run`) and then fails (any kind, a panic) or its lease is reclaimed, with `attempts >= Retry.MaxAttempts` | (attempts are kept while below the limit and the request is applied; at the limit the held request is dropped and the row ends `failed`, `gave_up`; an outside request during a failing run still resets `attempts` to 0) | admin may `Retry` after a fix | `TestSelfReenqueue_FailingRun_KeepsAttemptsAndGivesUp` (integration), `TestFake_SelfReenqueue_FailingRun_KeepsAttemptsAndGivesUp` (unit), `TestOutsideRequest_DuringFailingRun_ResetsAttempts` (integration) |
| F57 | `Enqueue` (the upsert) | the held request would be written on a row that is not `running`, or `next_run_at` without `next_args` | Invalid (constraint `job_next_chk`, 23514 through `Wrap`; unreachable through the statement; a hand edit) | none (a bug) | `TestEnqueue_NextOnQueuedRow_ConstraintRefuses` (integration, direct SQL) |
| F58 | `jobstest.Fake.RunDue` | a handler fails | the handlers' errors joined (that row ends `failed`, or is requeued when the error is retryable (D52(c)); the rest still run) | the test asserts the kind | `TestFakeRunDue_HandlerFails_ReturnsErrorAndRunsRest` |
| F59 | `Status` | rows in every status | (none: `Failed`, `Running`, `Retrying`, `Waiting` hold the right rows in the right order; `done` and `canceled` in none) | render | `TestStatus_SplitsQueueByState` |
| F60 | `Status` | more than `MaxRecent` rows in one state | (none: that list holds the first `MaxRecent` by its order; the others are not shortened) | show "the first {n} are shown" | `TestStatus_EachListCappedSeparately` |
| F61 | `Status` | a `queued` row with `last_error` set | (none: it is in `Retrying`, not `Waiting`; after an admin `Retry` it is in `Waiting`) | render | `TestStatus_RequeuedRowIsRetrying`, `TestStatus_AdminRetriedRowIsWaiting` |
| F62 | `Status` | store failure on the list query | Unavailable / Timeout | `ErrorState` with the reference and Retry | `TestStatus_StoreUnavailable_ReturnsUnavailable` |
| F63 | the screen | a job or owner title key is missing from the catalogue | (none: the raw name in `<code translate="no">`, the raw owner token) | add the key; the build test fails | `shows a raw name in code when the catalogue has no title` (vitest); `TestMessageCatalogues_EnglishCoversJobs` (Go, fails the build) |
| F64 | `TestMessageCatalogues_EnglishCoversJobs` | a module's `Start` fails under `db.None()` | (the test fails naming the module) | fix `Start` (it must not read) | `TestMessageCatalogues_FixtureStartFails_Fails` |
| F65 | the screen | `403` on the read | (the shell's forbidden sentence with the reference; no Retry) | ask for the permission | `shows the forbidden sentence and the reference for a 403 on the read` |
| F66 | the screen | `403` on Run now, Retry or Cancel | (the shell's error block beside the control with the reference; the actions vanish on refresh) | — | `shows the error summary with the reference when Run now is refused with 403`, `keeps a 403 on Cancel in the dialog` |
| F67 | the screen | a run's `instance` ≠ `StatusView.Instance` | (none: the "another server" sentence; absent otherwise) | — | `marks a run by another server and nothing on the home server` |

`Name.Owner`, `InJob`, `Status.String`, `Routes`, `OpenAPI`, `Declared` cannot fail: pure functions over values already validated, constants or the in-memory registry.

## Lock-in check

No external technology beyond PostgreSQL, which the project already requires (D8): the queue is a table, the lock is `FOR UPDATE SKIP LOCKED` (PostgreSQL 9.5+, well under `db.MinServerVersion`), the schedule is the clock. No broker, no Redis, no cron daemon, no cloud scheduler. A regional runs it on the same single database as everything else (principle 5).

## Test plan

- **Unit** (`jobs`): a hand-written fake store behind the storage interface `service.go` declares; `clock.NewFake` drives ticks, backoff and lease expiry (no `time.Sleep`); `telemetrytest.New(t)` for logger, tracer and meter (fails on any policy violation); `audittest.NewFake("jobs")`; `authztest.NewFake()` (denies until granted) with `identity.WithSession` for the admin use cases; a fake `Redeliverer`. Table tests per failure mode above; a test that every outcome token and every `last_error` token matches `^[a-z_]{1,32}$`; a test that a handler's `Args` with an invented person's ID appears in no log, span or metric (`!c.Contains(id)`); a test that `Register` on a `db.None()` runner succeeds (`bms check`).  **The key rule as one table**, `internal/core/jobs/internal/rules/testdata/keyrule.json` (existing status × outcome → expected status, `run_at`, `attempts`, `next_*`; JSON because a `_test.go` table cannot be shared across packages), consumed by two tests so the fake cannot drift from the queue again: `TestKeyRule_Table` (the integration suite, against PostgreSQL) and `TestFake_KeyRule_Table` (jobstest, against the Fake; the cancel and backoff rows, which the Fake does not model, are covered by the integration suite only). (Build note 2026-10-23: the unit fake store does not model the queue, so it does not consume the table.)
- **Permission tests:** F20 to F22 for each admin method; `jobs.read` holders cannot `RunNow`.
- **Integration** (`//go:build integration`, `dbtest.TempDB`): migrations apply; two runners on one database lease disjoint rows and insert one slot (F39); a row whose lease lapsed is re-leased by the other runner; the heartbeat keeps a long handler's lease past `Lease`; `Enqueue` inside a `Write` transaction that rolls back leaves no row; the dedupe upsert moves a queued row, reopens a final one and holds on a running one (F43, F53); a handler that enqueues its own key is queued again (F54); a held request survives a failed, retried, released and reclaimed run (F55, F56); migration `0002` applies on a database with `0001` rows in every status; `jobs.purge` deletes only final rows older than `DoneTTL`; `dbtest.Fake.SetReadOnly` mid-run yields F34.
- **Conformance** (`moduletest`): a nil `Deps.Jobs` becomes `jobstest.NewFake(manifest.Name)`; a new rule `jobs_register_scoped` (a module's registrations carry its own prefix) and the leak rule also scans `Fake.Enqueued()` args and keys for the seed markers (args are IDs, so this guards a future `Args` change; a key checked against the seed person markers is rule `jobs_key_not_person`). Also `TestFake_Enqueue_RunAtBound_UsesTheFakeClock`, and  `TestFake_Enqueue_KeyRunning_QueuedAfterRun`, `TestFake_Enqueue_KeyFinal_Reopens`, `TestFakeRunDue_RunsDueOnceInOrder`, `TestFakeRunDue_SelfRequeueNotRunTwice`.
- **End to end** (`app`): `serve` on a temporary database runs `jobs.purge` and `audit.purge` once at start (`bms.jobs.runs` observed); `bms check` lists the registered names.

## Amendment J1 (raised 2026-10-23, approved 2026-10-23, D50): a request for a key whose row is running or finished

**What the volunteers V1 build found.** Volunteers decision 25 parks a reminder due beyond `MaxDelay - 24h` at the horizon and has the handler re-enqueue its own key while `Starts - lead` is still ahead. The handler's `Enqueue` ran while its own row was `running`, and decision 3's upsert (`... DO UPDATE ... WHERE core.job.status = 'queued'`) matched nothing: the request was a silent no-op (the existing ID came back), the handler returned nil, the row ended `done`, and a booking more than a year ahead was never reminded. `jobstest.Fake` recorded every request without a row state, so volunteers F112a passed. The same rule had a second hole the build had not hit: after a reminder fired its row is `done` for `DoneTTL` (seven days), and an `UpdateShift` that moved the shift later in that time was a no-op too, so V1's "a shift moved after a reminder was sent is reminded once more for its new time" held only after the purge. Both are one defect: a `Key` request has a state in which it is dropped without a word.

**Chosen: the latest request wins (decision 3 reworded, decision 23).** With a `Key`, `Enqueue` makes the key's row that request whatever state the row is in: a `queued` row is moved (as before), a final row is reopened as a fresh `queued` row, and a `running` row holds the request in two new columns (`next_run_at`, `next_args`, migration `0002`) that every statement taking a row out of `running` applies first (complete, requeue, release, fail, the reclaim's two arms): `queued` at the held time, `attempts = 0`, the run's own outcome logged, metered and kept in `last_error`. One statement branch and one SQL fragment carry the whole rule; no new interface, sentinel or module API, and the volunteers handler already written (`Enqueue` on its own key from inside the run) becomes correct without change. The reasons for this shape over the alternatives: (a) the silent branch is the defect, so the fix removes it rather than giving modules a second way around it; (b) no `Enqueue` consumer relies on "final is a no-op": volunteers' two keyed jobs both want a repeat to run again, the redelivery uses `Defer` (its own insert, unchanged: a delivery is immutable, so a repeat is the same work), scheduled slots and `RunNow` use their own inserts and the unique index over every status still keeps a `done` slot from being reinserted for the rest of its period; (c) the move-while-running window, however short, is closed too, which no handler-side primitive can do.

**Not taken.** (1) *A sentinel, `jobs.RunAgainAt(t) error`, returned by the handler*: one verdict and one statement, and it carries a parked reminder forward; but it leaves `Enqueue` on a `done` row a no-op for `DoneTTL` (the second hole) and `Enqueue` on a `running` row silently dropped, so volunteers would still need a second fix and the trap would stay for the next module. (2) *Continuation keys in volunteers only* (`<signup>:<hop>` or `<signup>:<starts>`): the handler can park itself under a new key, but `UpdateShift` cannot cancel the old row (modules have no `Cancel`), so a moved shift leaves a parked row that fires, re-reads, re-parks under yet another key, and so on, one more live row per move; correctness then rests on the handler's re-read plus a dedupe column or notify's key for the send, the `done`-within-`DoneTTL` hole needs a third key component, and `/jobs` fills with hop keys. Correct under the move-while-parked case only with all three, and still wrong for any other module that keys by its record. (3) *A partial unique index on `queued` rows only* (so a running or final row never conflicts and a request always inserts): breaks the scheduler, whose "insert the slot once" relies on a `done` slot still conflicting until the next slot. (4) *Dropping the held request on a final failure* (keeping the row `failed` for `/jobs`): hides a request the caller made in good faith behind a failure of an older one; F55 keeps the newer request and leaves the failure in the log, the span and the metric, and a persisting bug shows as `failed` on the very next run, which has no held request.

**What changes in the code.** Migration `0002_job_next.sql` (two nullable columns, two checks); `enqueue_job.sql` becomes the three-branch upsert that always returns a row (`Store.Enqueue` loses its retry loop and `byKey`); `complete_job.sql`, `requeue_job.sql`, `release_job.sql`, `fail_job.sql` and `reclaim_expired_leases.sql` gain the "apply the held request first" fragment and return `status`; `settle` and `reclaim` log `next=true` when the row stayed open; `jobstest.Fake` keeps a row per key with the real transitions and gains `Queued()` and `RunDue`; the key rule is one table shared by the PostgreSQL suite and `jobstest` (Test plan). Nothing changes in `Registrar`, `Record`, the routes, the screen, the metrics' outcome set or any module's code. The volunteers spec records the dependency as Amendment V2.

**Test expectations that change:** `TestEnqueue_KeyFinal_IsNoopReturnsExistingID` becomes `TestEnqueue_KeyFinal_ReopensRow` (F43); `TestEnqueue_KeyRunning_*` is new (F53); any test that asserted "a second `Enqueue` of a done key is a no-op" asserts a reopened row; volunteers' F112a asserts `jobstest.Fake.Queued()`.

## Amendment J-W1 (raised 2026-10-30, D57): the Background work screen for people

- **Status:** Approved 2026-10-30, D58. Nothing below is implemented yet.
- **Scope:** the sub-abstraction "the status screen" (`/jobs`, `web/src/core/jobs/`), the `StatusView` it reads, and the catalogue keys that name a job. The queue, the scheduler, the workers, the redeliverer, the `Registrar`, the table, the permissions and the three admin actions are unchanged.

### W1.1 What the walkthrough found, and what the owner accepted

A walkthrough of `/jobs` on the public demo (2026-10-30) found: (1) each registered card shows the raw job name twice (`audit.purge` as the title, then `audit.purge` again in small type); (2) the owner headings `files` and `signals` are raw lowercase tokens; (3) "On copy demo" is jargon; (4) about thirty healthy shift reminders sit under "Failed and waiting" with Problem "None", so the one section that should list trouble is mostly fine rows; (5) the Key column shows sign-up UUIDs; (6) the table's column headers broke inside a word at 1280 px.

The owner accepted (D57): "Background work" names the area and "job" names one item; the column "Work" becomes "Job" and the two "That work…" sentences follow; a 403 on the screen shows the error summary the spec names; waiting jobs are listed apart from failed ones; no IDs are shown, and owner and job titles come from the catalogues. This amendment settles the details.

Causes, for the record: (1) and (2) because no catalogue key for a job exists and `module.files.title` and `module.signals.title` were never written (files and signals have no web area, so no catalogue of their own); (4) because `StatusView.Recent` is one list of `failed` and `queued` rows, newest first, and healthy deferred rows are `queued` too; (5) because the screen rendered `Record.Key` as a column; (6) because `body { overflow-wrap: anywhere }` (`web/src/index.css`, the rule that keeps long tokens from overflowing a 320 px screen) also applies to `<th>`, and seven columns, one of them a 36-character UUID in `break-all`, left each header narrower than its own word.

### W1.2 Purpose of the screen

Show a site admin what the site does on its own, in their words.

Not responsible for: operator support (the row ID, the key, the arguments, the trace: those are in the logs, `bms check` and `GET /api/jobs/`, not on the screen); deciding what a job does (its owner); authorization (`authz`; the permissions are unchanged).

### W1.3 Words

| Thing | Word on the screen | Never |
|---|---|---|
| the area, the nav entry, the `h1` | **Background work** | jobs, queue, tasks |
| one registered name, one row | **job** | work (as a count noun), task, item |
| a `queued` row | **waiting** | queued, pending |
| a `queued` row with `last_error` set | **waiting to retry** | requeued |
| a `running` row | **running** | leased, in flight |
| a `failed` row | **failed** | errored |
| `Record.Instance`, `Options.Instance` | **server** (Q-JOBS16, D58) | copy, instance, node |

The catalogue changes that follow from the words (`web/src/core/jobs/messages/en.json`; every other language falls back to `en`, web-shell D8):

| Key | English |
|---|---|
| `core.jobs.col.name` | "Job" (was "Work") |
| `core.jobs.run_conflict` | "That job is already waiting or running." |
| `core.jobs.run_on_demand` | "That job only runs when something asks for it." |
| `core.jobs.run_unknown` | "That job isn't registered on this server." |
| `core.jobs.readonly_banner` | "This server is read-only and runs no background work, so nothing can be started, retried or canceled here." |
| `core.jobs.intro` | "The jobs this site runs on its own: what has failed, what is running or waiting, and every job that is registered." |
| `core.jobs.registered.title` | "Jobs" (was "Registered work") |
| `core.jobs.registered.on_demand` | "Runs when something asks for it" (was "Runs on demand") |
| `core.jobs.registered.instance` | "Ran on another server ({instance})" (was "On copy {instance}"; shown only by the rule in W1.6) |
| `core.jobs.recent.*` | removed; replaced by the section keys in W1.6 |
| `core.jobs.col.key` | removed (W1.6, no key column) |
| `core.jobs.run_label` | "Run now: {title}" (the argument is the job's title, W1.4, not its name) |
| `core.jobs.retry_label` | "Retry {title}, failed {when}" |
| `core.jobs.cancel_label` | "Cancel {title}, due {when}" |

`module.jobs.permission.read.title` ("See background work and its recent runs") and `module.jobs.permission.manage.title` ("Run, retry and cancel background work") already use the words and stay.

### W1.4 Titles for owners and jobs: catalogue keys

**Decision 24 (J-W1). A job's title is a catalogue key derived from its name, `module.<owner>.job.<ident>.title`, held in the owner's web catalogue; `Decl` gains no field.** Module-contract decision 8 already fixes that the manifest carries no human-readable text and that titles are keys derived from names (`module.<name>.permission.<rest>.title`, `.signal.<rest>.title`, `.template.<ident>.title`); a job is the same shape. The owner's own title is the existing `module.<owner>.title`. `Register` stays a no-I/O call with no text in Go, `Registered` on the wire stays `{name, owner, …}`, and the browser looks the keys up as it already does for an owner's title.

Where the keys live, by owner:

| Owner | Catalogue file | Keys |
|---|---|---|
| a module | `web/src/modules/<name>/messages/en.json` | `module.<name>.title` (exists), `module.<name>.job.<ident>.title` for every name it registers |
| a core area with a web area (`audit`, `jobs`, `settings`, `people`, …) | `web/src/core/<area>/messages/en.json` | the same |
| a core owner with no web area (`files`, `signals` today) | `web/src/shell/messages/en.json`, beside `module.core.title` | `module.<owner>.title`, `module.<owner>.job.<ident>.title` |

The catalogues are one flat map at run time, so the location is a convention for authors and translators and for the check below, not a lookup rule. `<ident>` may contain hyphens (`sweep-orphans`), as signal idents already do.

**The twelve names registered today and their English titles** (the implementer writes them; the words are the catalogue's, and volunteers' use its V3 terms):

| Name | Key | English |
|---|---|---|
| `audit.purge` | `module.audit.job.purge.title` (`core/audit`) | "Delete audit entries past their retention" |
| `jobs.purge` | `module.jobs.job.purge.title` (`core/jobs`) | "Delete finished job records" |
| `signals.redeliver` | `module.signals.job.redeliver.title` (shell) | "Deliver a message between modules again" |
| — | `module.signals.title` (shell) | "Messages between modules" |
| `files.reap` | `module.files.job.reap.title` (shell) | "Delete the content of deleted files" |
| `files.sweep-orphans` | `module.files.job.sweep-orphans.title` (shell) | "Remove uploads that were never attached" |
| — | `module.files.title` (shell) | "Files" |
| `volunteers.shift-reminder` | `module.volunteers.job.shift-reminder.title` | "Send {term_shift} reminders" |
| `volunteers.expiry-scan` | `module.volunteers.job.expiry-scan.title` | "Warn about {term_qualification_plural} about to expire" |
| `volunteers.purge` | `module.volunteers.job.purge.title` | "Delete volunteer records past their retention" |
| `volunteers.cancel-shifts` | `module.volunteers.job.cancel-shifts.title` | "Cancel the {term_shift_plural} of an archived {term_department} or a closed {term_event}" |

**Fallback when a key is missing.** The screen shows the raw name in `<code translate="no">` (for a job) or the raw owner token (for an owner), as it does today, and nothing else changes: a missing translation must never hide a job. The fallback is a safety net, not a state a shipped build reaches, because a Go test fails the build: `TestMessageCatalogues_EnglishCoversJobs` (`internal/app`, beside `TestMessageCatalogues_EnglishCoversManifests`) builds the runner and every compiled-in module against `db.None()` through the path `bms check` uses, calls each `Start`, reads `Declared()`, and asserts that the union of the shell's `en` catalogue and every `web/src/{modules,core}/*/messages/en.json` holds `module.<owner>.title` and `module.<owner>.job.<ident>.title` for every `Registered`. A `Start` that fails under `db.None()` fails this test (unlike `bms check`, which only prints it, Q-JOBS10): the test cannot learn that module's names, so it cannot vouch for them. The invented `zq*` modules the app tests register are skipped as in the manifest test.

Not taken: (a) *`Decl.Title string`*: human text in Go, which decision 8 forbids and D28 (translation-ready from day one) is the reason for. (b) *Titles resolved on the server and sent in `Registered`*: the server has no catalogue and no language; every other title on every screen is a browser lookup. (c) *Deriving a title from the ident* (`sweep-orphans` → "Sweep orphans"): an English-looking string that is not a translation and reads as nonsense for half the names (`reap`, `redeliver`).

### W1.5 `StatusView`: the queue in four lists

**Decision 25 (J-W1). `StatusView.Recent` is replaced by four lists, each capped at `MaxRecent` on the server, so a long healthy queue cannot push a failure off the screen.** Thirty reminders are the normal state of a site with one event coming up; fifty (`MaxRecent`) will be too, and "newest first" over `failed` and `queued` together then shows no failure at all. Splitting in the browser would not fix that cap. The split is by the row's state, which is what an admin acts on:

```go
// StatusView is what GET /api/jobs/ returns (Amendment J-W1: Recent became four lists).
type StatusView struct {
	ReadOnly   bool
	Instance   string         // this process's Options.Instance; the screen marks a run by any other server (W1.6)
	Registered []Registered
	Counts     map[Status]int // every row by status; unchanged
	Failed     []Record       // status failed; newest finished_at first; at most MaxRecent
	Running    []Record       // status running; earliest started_at first; at most MaxRecent
	Retrying   []Record       // status queued with last_error set; soonest run_at first; at most MaxRecent
	Waiting    []Record       // status queued with last_error null; soonest run_at first; at most MaxRecent
}
```

`Retrying` is "waiting to retry": a row the runner re-queued after a retryable failure (`unavailable`, `timeout`), a reclaim (`lost_lease`), or a held request applied after a failing run (decision 23 keeps the run's token in `last_error`). An admin's `Retry` clears `last_error`, so a retried row is `Waiting`, as it should be: the admin asked for it. A `done` or `canceled` row is in no list (the cards show the last run of a name; `Counts` has the totals), as before.

**Wire.** `GET /api/jobs/` answers `{read_only, instance, registered, counts, failed, running, retrying, waiting}`; `recent` is gone. `JobsRecord` is unchanged (`key` stays on the wire: it is `internal`, behind `jobs.read`, and an operator with `curl` is who needs it; the screen does not show it, W1.6). The OpenAPI fragment and the generated TypeScript types change with it. No other consumer of `Recent` exists (the screen is the one reader; `bms check` uses `Declared()`).

**Query.** One statement, `list_queue.sql`, in the one `Read` transaction `Status` already uses, so the four lists are one snapshot: a `UNION ALL` of four arms (`failed`, `running`, `retrying`, `waiting`), each with its own `ORDER BY <the bucket's order>, id LIMIT MaxRecent`, so each arm walks an index and none scans the table (D58 review; this replaces a `row_number()`-per-bucket form). The `failed` arm uses `job_failed_idx (finished_at DESC, id DESC)`, not `job_finished_idx`, which serves the purge; the queued arms (`retrying`, `waiting`, split on `last_error` null or set) and the `running` arm use `job_due_idx`. The service sorts nothing in Go.

### W1.6 The screen

Core area `jobs`, screen `/jobs` ("Background work"), in the admin navigation for holders of `jobs.read`; phone first. Keys `core.jobs.*`. In order, top to bottom:

1. **`h1` "Background work"** and the intro sentence (W1.3).
2. **Read-only banner** when `StatusView.ReadOnly` (or the shell's read-only header): the sentence in W1.3; the three actions are hidden.
3. **The counts line** (`<nav aria-label="Sections">`, `core.jobs.summary.*`): three in-page links with counts from `Counts`: "Failed ({failed})", "Running ({running})", "Waiting ({queued})", each to its section's heading. It exists for the phone: an admin sees at a glance whether anything failed and jumps to it without scrolling past the cards. Items wrap as whole items; none breaks inside a word.
4. **Failed** (`h2`, `#jobs-failed`): a table of `Failed`, caption "Failed jobs, newest first", columns **Job** (the title, W1.4; the row header), **Failed** (`finished_at`), **Attempts**, **Problem** (the token in words, raw token in `<code>` as fallback, as today), **Actions** (`jobs.manage`: **Retry**). Empty state: "Nothing has failed." After a successful Retry the toast says "Queued again" and focus moves to this heading.
5. **Running now** (`h2`, `#jobs-running`): a table of `Running`, columns **Job**, **Started** (`started_at`), **Attempt**. No action (a running row cannot be retried or canceled, F28, F29). Empty state: "Nothing is running right now."
6. **Waiting to retry** (`h2`, `#jobs-retrying`): a table of `Retrying`, columns **Job**, **Due** (`run_at`), **Attempts**, **Last problem** (the token in words), **Actions** (`jobs.manage`: **Cancel**, with the confirm dialog). Empty state: "Nothing is waiting to retry."
7. **Waiting** (`h2`, `#jobs-waiting`): a table of `Waiting`, columns **Job**, **Due**, **Actions** (`jobs.manage`: **Cancel**). No Status column (the section says it), no Attempts (always 0 here), no Problem (always none here: that is what the walkthrough's "Problem: None" thirty times was). Empty state: "Nothing is waiting." When the list holds exactly `MaxRecent` rows a line under the table says "The first {n} are shown; {total} are waiting in all." (`total` = `Counts.queued`); the same line, with its own wording, under the other three lists when they are full.
8. **Jobs** (`h2`, `#jobs-registered`): the registered cards, grouped by owner under an `h3` that is the owner's title (W1.4), owners sorted by their title; within an owner, scheduled names first, then on-demand, each group sorted by title. A card shows: the **job's title** as its `h4` (the raw name is **not** shown; it appears only as the fallback of W1.4); "Runs every N" or "Runs when something asks for it"; the last run ("Last run: {status}, {when}. Took …"), its problem if any, and "Next due: {when}" for a scheduled name; the **Run now** button for scheduled names (`jobs.manage`; disabled with "Already waiting or running." while a row of the name is in `Running`, `Retrying` or `Waiting`, or `Last` is `queued` or `running`). Empty state: "Nothing is registered."

**No IDs, no keys, no arguments.** The screen never renders `Record.ID`, `Record.Key`, `Record.TraceID` or an argument: the Key column is gone (that is where the sign-up UUIDs were), and the accessible names of Retry and Cancel use the job's title and a time, not the name or the ID. Two waiting rows of one job differ by their due time, which is what an admin cancels by. The row ID still travels in the Retry and Cancel requests (`/queue/{job}/…`); it is in the DOM as a React key only. An operator who needs to match a row to a log line uses `GET /api/jobs/` or the log's `job_id` (Not responsible for, W1.2).

**Another server.** The instance line is shown only when a run's `Instance` differs from `StatusView.Instance` (the server that answered), as "Ran on another server ({instance})" under the card's last run, and as the same sentence under the Job cell of a `Running` row. A one-server site, which is every regional today, never sees it; a site with two writers (a restart overlap, an operator who ran two) sees it only on the runs the other one did. Not taken: showing it always with a better word (the name of the only server is noise on every card), and dropping it (two writers is the one case an operator needs to see which process ran a job that misbehaved).

**Forbidden and errors (the "error summary").**
- A direct visit without `jobs.read` in the session: the shell's forbidden block (`shell.forbidden`, "You don't have access to this screen.") rendered with the shell's `ErrorState` shape; no request is made, so there is no reference. The local `Forbidden.tsx` and `ScreenError.tsx` fold into one component that takes an optional `ApiError`.
- A `403` on the read (the session was stale): the shell's `ErrorState` with the `shell.forbidden` sentence and the **reference line**, no Retry button (a retry cannot help). Test `shows the forbidden sentence and the reference for a 403 on the read`.
- A `403` on an action (Run now, Retry, Cancel: `jobs.manage` revoked since the page loaded): the shell's error block beside the control that was pressed (`ErrorState` under the card for Run now and under the section heading for Retry; `ErrorSummary` inside the Cancel dialog), with the shell's `error.permission` sentence and the reference line; the actions then disappear on the refresh that follows every action (the session is re-read). Never a toast, never silence. Tests `shows the error summary with the reference when Run now is refused with 403`, `keeps a 403 on Cancel in the dialog`.
- Any other failed read: `ErrorState` with the reference and Retry, as today.

**Phone layout** (320 px up; the hard requirement, web-shell D21):
- Below 640 px every table is the UI kit's stacked `Table`: each row a bordered block, each cell labelled with its header; the counts line and the section headings are the way around the page; the cards are one column; every button is a 44 × 44 px target.
- From 640 px: real tables; cards in two columns.
- **Headers never break inside a word, at any width.** The kit's `Table` sets `break-normal` on every `<th>` (overriding the body's `overflow-wrap: anywhere`), so a header wraps between words or not at all; with the Key column gone no cell uses `break-all`, and the widest table (Failed) has five columns. A title wraps between words. The raw-name fallback, when it shows, may wrap anywhere (the body rule) so a 96-byte name cannot overflow a 320 px screen. This is a one-class change in `web/src/ui/Table.tsx` (web-shell's kit): Proposed change 15.
- Nothing scrolls horizontally at 320 px (the smoke run asserts it at the phone viewport, as for every screen).

### W1.7 Permissions, data, signals, settings

Unchanged. `jobs.read` covers the `GET` whatever shape it returns; `jobs.manage` the three actions. `core.job` is not touched; no personal data is involved (the screen shows fewer fields than before, never more). No signal, no setting: nothing here differs by regional (the words are the catalogue's, and a regional that wants other words translates them, web-shell D28).

### W1.8 Failure modes added or changed

F59 to F67 are in the main Failure modes table (applied 2026-10-30, D58).
`Status`'s earlier modes (F20, F21) stand. The screen's other states (`ErrorState` with Retry on a failed read; the empty states; the read-only banner) are unchanged and keep their tests, reworded for the new headings.

### W1.9 Test plan additions

- **Go, `jobs`:** F59 to F62 against the fake store and, for `list_queue.sql`, the integration suite (`TestStatus_ListQueue_OneSnapshot`: rows inserted in every status with `last_error` set and null; the four lists, their order, the cap). The OpenAPI fragment test asserts `recent` is gone and the four arrays are present.
- **Go, `app`:** `TestMessageCatalogues_EnglishCoversJobs` over the real tree (skipped when `web/src` is absent, like the manifest test); `TestMessageCatalogues_FixtureStartFails_Fails` with an invented `zq*` module whose `Start` errors.
- **vitest, `web/src/core/jobs/jobs.test.tsx`:** the suite is rewritten around the new shape: titles from the catalogue and never the raw name or a key in the DOM (`screen.queryByText(/5b1f0000/)` is null; no `<code>` with a dot-name when every title exists); the four sections with their own headings and empty states; a healthy queued row appears under Waiting with no Problem cell; a requeued row under Waiting to retry with its problem; Retry only under Failed, Cancel only under the two waiting sections; "Job" column header; the three "That job…" sentences; the counts line's links; the 403 cases (F65, F66); the another-server rule (F67); axe on the full page. Accessible names: `Run now: Delete finished job records`, `Retry Send shift reminders, failed …`.
- **vitest, `web/src/ui/kit.test.tsx`:** every `<th>` of `Table` has `break-normal`.
- **End to end (Playwright smoke, phone and desktop viewports):** `/jobs` as a site admin shows "Background work", no horizontal scroll at the phone viewport, and no header taller than one line at 1280 px (the `th` height equals the line height).

### W1.10 Not taken

- *A module-supplied description for a key* (a `Describer` the owner registers with the name, answering "Dusty's Gate shift, Sat 14:00" for a sign-up ID): a new contract method, a per-row lookup under the module's own permission check, and a personal-data path (a shift roster is `internal` at best, a playa name shows on a screen that is `internal` for site admins) for a column nobody acts on. If an admin ever needs to know *which* reminder a row is, the volunteers module's own screens are where the sign-up lives; a link from a job row to its record is the later, smaller addition (Q-JOBS15).
- *Keeping one "Recent" list and splitting it in the browser:* the cap problem (W1.5).
- *Showing the raw name in small type under the title:* the duplication the walkthrough disliked, in a new coat; an operator has `bms check` and the API.
- *Hiding empty sections:* a section that comes and goes is harder to learn than one that says "Nothing has failed."
- *Changing `MaxRecent`:* fifty per list is plenty for a screen; the count line carries the totals.

### W1.11 Open questions (J-W1)

- **Q-JOBS15. Keys hidden entirely, or a later link from a waiting row to its record?** **Answered (owner, 2026-10-30, D58): hide keys entirely for now.** A link from a waiting row to its record is a possible later addition that needs a small owner-side contract; build it only when a regional asks.
- **Q-JOBS16. "Server" as the word for an instance** ("Ran on another server (main)"; "This server is read-only…"; "That job isn't registered on this server."). The shell's read-only banner says "This copy is read-only"; "copy" was the jargon the walkthrough flagged on this screen. **Answered (owner, 2026-10-30, D58): "server" is the word for an instance on this screen.** Whether the shell's banner follows is web-shell's call (Proposed change 15 notes it; no change is made to it).

### W1.12 Proposed changes elsewhere (J-W1)

15. (applied 2026-10-30, D58, in web-shell.md) **`plan/design/web-shell.md` (row 11):** (a) the UI kit's `Table` sets `break-normal` on every `<th>` so a column header never breaks inside a word (a one-class change, `web/src/ui/Table.tsx`, with a kit test); (b) the shell catalogue `web/src/shell/messages/en.json` gains `module.files.title`, `module.files.job.reap.title`, `module.files.job.sweep-orphans.title`, `module.signals.title`, `module.signals.job.redeliver.title` (W1.4: core owners with no web area keep their titles beside `module.core.title`); (c) the screen table's `/jobs` row (if listed) says "Background work" with the sections of W1.6; (d) the English coverage check (decision 8, Proposed change 7) covers `TestMessageCatalogues_EnglishCoversJobs`; (e) a note that this screen says "server" for an instance (Q-JOBS16), for the shell's read-only banner to follow or not.
16. (applied 2026-10-30, D58, in module-contract.md decision 8 and the add-module and go-conventions skills) **`plan/design/module-contract.md` (row 4), decision 8:** add `module.<name>.job.<ident>.title` for every job name the module registers in `Start` (checked by `TestMessageCatalogues_EnglishCoversJobs` in `internal/app`, which learns the names through `Start` under `db.None()` as `bms check` does); `Decl` carries no title. **`.claude/skills/add-module/SKILL.md` and `.claude/skills/go-conventions/SKILL.md`, the `deps.Jobs` sentence:** "and a title `module.<name>.job.<ident>.title` in `en.json` for every name it registers"; and "keys show on `/jobs` and in logs" becomes "keys appear in `GET /api/jobs/` and in logs, never on the screen" (the rule "never a person ID" stands).
17. (applied 2026-10-30, D58, in module-volunteers.md) **`plan/design/module-volunteers.md` (row 20):** the four `module.volunteers.job.<ident>.title` keys of W1.4 join the module's catalogue (English as proposed, with the V3 terms); no code change in the module.
18. (applied 2026-10-30, D58, in audit.md) **`plan/design/audit.md` (row 12):** `module.audit.job.purge.title` joins `web/src/core/audit/messages/en.json` (one key; the job is registered by `app`, the title lives with the audit area's screen).
19. (applied 2026-10-30, D58, in app.md) **`plan/design/app.md` (row 18):** `bms check` is unchanged; the `internal/app` catalogue tests gain `TestMessageCatalogues_EnglishCoversJobs` and its fixture test (W1.4, F64).

**Applied 2026-10-30 (D58).** When approved, the body of this spec changes as follows (`spec-editor`): the UI section becomes W1.6; `StatusView` in the Interface and the Types list become W1.5 (`Recent` removed, `Instance`, `Failed`, `Running`, `Retrying`, `Waiting` added); the `Request.Key` comment "keys show on /jobs and in Status.Recent" becomes "keys appear in `GET /api/jobs/` and in logs, never on the screen"; `jobs.read`'s "Allows" cell reads "the registered names, last runs, counts, and the failed, running, retrying and waiting rows"; decisions 24 and 25 join the Summary; F59 to F67 join the table; Q-JOBS15 and Q-JOBS16 join the Open questions with their answers.

## Open questions

- **Q-JOBS15 (J-W1).** Keys hidden entirely, or a later link from a waiting row to its record? **Answered (owner, 2026-10-30, D58): hide keys entirely for now** (W1.11).
- **Q-JOBS16 (J-W1).** "Server" as the word for an instance on this screen. **Answered (owner, 2026-10-30, D58): yes** (W1.11).
- **Q-JOBS14 (J1).** Accept "the latest request for a `(name, key)` wins" (decision 3 reworded, decision 23) in place of "a running or final row makes the request a no-op", with the held request kept across a failed run (F55)? **Answered (owner, 2026-10-23, D50): yes.** Recommendation was yes: It removes the one state in which a keyed request vanished silently, it is the behaviour the only keyed consumers (volunteers' reminder and `cancel-shifts`) already assumed, and the cost is two nullable columns and one SQL fragment. The alternative that keeps decision 3 intact (a `RunAgainAt` sentinel) fixes the carry-forward alone and leaves a moved shift unreminded for seven days after a reminder was sent.

- **Q-JOBS1. Does the `/jobs` screen, with Run now / Retry / Cancel, ship in M4, or status only, or Later?** Recommendation: ship it in M4 as specified. It is one screen over one `GET` and three `POST`s, and the first module with reminders (volunteers) will raise "did the reminder run?" on day one; without `Retry` a failed purge or reminder has no path but a restart.
- **Q-JOBS2. The give-up policy: ten attempts, 1 m doubling to 1 h, about four hours; the same for retried signal deliveries.** Recommendation: accept. Longer would hide a broken dependency; shorter would lose a reminder over a short outage. An admin can `Retry` for seven days, and the log and metric keep the record.
- **Q-JOBS3. Should notify's sender tick move onto `jobs` now?** Recommendation: no (notify decision 2 stands; 09 §6 says "later"). Notify's loop is implemented, tested and one `SKIP LOCKED` lease like this one; moving it buys nothing in M4 and would couple the two parts. Revisit when a third loop appears or when the audit importer and notify's loop and this one are visibly the same code.
- **Q-JOBS4. A daily schedule at a time of day in `regional.timezone` (for example "purge at 03:00 local") in M4 or Later?** Recommendation: Later. `Every` covers retention and daily scans; a reminder tied to a shift is a deferred job with `RunAt`. The addition is a second `Decl` field and a `ZoneSource` seam, not a redesign.
- **Q-JOBS5. `DoneTTL` of 7 days as a constant, or a `jobs.retention` setting?** Recommendation: a constant. The rows hold no personal data (D30 is about personal data), and the only effect of the number is how long an admin can `Retry` and how long a dedupe key blocks a repeat.
- **Q-JOBS6. The audit actor inside a job: `app`'s `auditActor` answers `ActorSystem` when `jobs.InJob(ctx)` (as audit itself does for `signals.InDelivery`).** Recommendation: accept. Approved (D43) with the words "a two-line change in `app` and no change to `audit`"; corrected 2026-10-20 (approved, D44): audit's `resolveActor` accepts only a person or the operator from the `ActorFunc` (`Internal` otherwise, pinned by its order test), so it is a few lines in `app` **and** a one-kind widening in audit (audit.md amendment 2026-10-20, F13b: `ActorSystem` with no `Person` and no `Superuser` is accepted; everything else the function might answer is still `Internal`). `auditActor` checks `InJob` before the session, as audit checks the delivery before the function (`jobstest.Fake.Run` runs on the test's ctx, which may carry one). `audit.purge` and signal redelivery were never affected (`Purge` sets its own actor; redelivery is `InDelivery`); the first module job that audits is.
- **Q-JOBS7. `Workers` (2), `PollInterval` (10 s), `Lease` (2 m), `RunTimeout` (5 m) as constants `app` passes, with no new `BMS_JOBS_*` configuration keys in M4.** Recommendation: accept; a small server with one database needs no tuning yet, and a config key is one `config.md` line when a regional asks.

The six below come from the review of the M4 implementation (amendment 2026-10-20). All accepted as recommended (owner, 2026-10-20, D44); each is folded into the body (decisions 18 to 22, F51, F52) and the answer is stated here.

- **Q-JOBS8. `Retry` of a failed scheduled slot while another row of that name is open.** Settled: `Retry` of a scheduled name refuses with `Conflict` (code `running`) while another row of the name is open, under the name lock (decision 18, F51). (owner, 2026-10-20, D44)
- **Q-JOBS9. A run whose heartbeats keep failing past its lease expiry keeps running.** Settled: the run is canceled at `now >= fence` with `outcome=lost_lease` and nothing more is written (decision 19, F52). (owner, 2026-10-20, D44)
- **Q-JOBS10. `bms check` calls each module's `Start` under `db.None()` to learn its job registrations, and fails the check when a `Start` fails.** Settled: `bms check` prints a module's `Start` failure in its jobs section and continues; it does not fail the check (app.md, `bms check`); `serve` still stops on a failing `Start`. (owner, 2026-10-20, D44)
- **Q-JOBS11. Count a reclaim's requeue in `Ran.Released` and `bms.jobs.runs{outcome=lost_lease}`.** Settled: a reclaim's requeue counts in `Ran.Released` and `bms.jobs.runs{outcome=lost_lease}` (decision 20). (owner, 2026-10-20, D44)
- **Q-JOBS12. `Request.Validate` and `jobstest.Fake` bound `RunAt` against the wall clock (`time.Now() + MaxDelay`), while the runner checks against its own clock.** Settled: `jobstest.NewFake(owner, opts ...Option)` with `WithClock`; `Request.Validate` keeps the wall clock (decision 21). (owner, 2026-10-20, D44)
- **Q-JOBS13. A dedupe key must never carry a person ID.** Settled: a dedupe key never carries a person ID; rule `jobs_key_not_person` (decision 22). (owner, 2026-10-20, D44)

## Proposed changes elsewhere

Status as of 2026-10-20: all eleven are applied (owner, D43; the tenth and eleventh D44). A twelfth (from files.md, D44) is applied below. Two more (13, 14, from Amendment J1) are applied below (2026-10-23, D50).

1. (applied 2026-10-20, D43) **`plan/design/module-contract.md` (row 4):** `Deps.Jobs jobs.Registrar` (uncomment the field; scoped by `provision` with `Scoped(m.Name)`; a module registers in `Start` with its own prefix and enqueues from use cases); `moduletest.Conformance` fills a nil `Deps.Jobs` with `jobstest.NewFake(manifest.Name)` and adds the rule `jobs_register_scoped`; its leak rule scans `Fake.Enqueued()`; mark Proposed change 13 (jobs) applied; add `jobs` to the `Deps` table row text ("scoped per module; `Register` in `Start`; `Enqueue` joins the caller's transaction; handlers are idempotent and run as the system").
2. (applied 2026-10-20, D43) **`plan/design/app.md` (row 18), part 8 and Proposed change 8:** `jobs` is built after audit, settings and authz (it takes `audit.Scoped("jobs")`, `a.deps.authz`, the actor closure, `a.disp` as `Redeliverer`, `cfg.Instance.Name`, and the constants of Q-JOBS7); `signals.New` keeps running before the parts and receives `Deferrer: lazyDeferrer{a}` (resolves `a.jobs` at call time, `Unavailable` before it is built), so no setter is needed (this replaces "constructs the runner before `signals.New`"); `link` registers `audit.purge` (hourly) through `a.jobs.Scoped("audit")` with a closure over `a.deps.audit.Purge` and `a.settings.Scoped("audit").Duration("retention")` (0 = skip); `a.deps.jobs` scoped per module in `provision`; route set `jobs`; the runnable `Run`/`Close` started after `Set.Start`, stopped after HTTP drained and before `signals.Close` (first of the runnables to stop); `auditActor` answers `ActorSystem` when `jobs.InJob(ctx)` (Q-JOBS6); `bms check` prints the registered jobs; `migrate` is unaffected.
3. (applied 2026-10-20, D43) **`plan/design/signals.md` (row 10), Proposed change 6:** mark applied by this spec: `Defer` stores on the writer with key `<signal id>:<subscriber>`; the job name is `signals.redeliver`; `Redeliver` is called with `Attempt` counting the first delivery; `NotFound` is dropped with a log line (`outcome=dropped`); `db.ErrReadOnly` releases and pauses; the give-up count is `DefaultRetry.MaxAttempts` (10) in all.
4. (applied 2026-10-20, D43) **`plan/design/db.md` (row 5), Proposed change 13:** mark applied for jobs (the loop reads `ReadOnly()` before each tick; a run that meets `ErrReadOnly` releases its row and pauses).
5. (applied 2026-10-20, D43) **`plan/design/telemetry.md` (row 15b), the metric table and Proposed change 6:** `bms.jobs.runs{job, module, outcome}` with outcomes `ok`, `retried`, `failed`, `canceled`, `read_only`, `panicked`, `no_handler`, `lost_lease`, `dropped`; add `bms.jobs.queue{status}` (gauge) and `bms.jobs.lag` (histogram, seconds); span names `jobs.tick`, `jobs.enqueue`, `jobs.defer`, `jobs.run`; mark Proposed change 6 applied.
6. (applied 2026-10-20, D43) **`plan/design/audit.md` (row 12), Proposed change 7:** mark applied: `app` registers `audit.purge` (hourly) through `jobs`, calling `Purge(ctx, now - retention)` on the writer only, skipping when `audit.retention` is 0.
7. (applied 2026-10-20, D43) **`plan/design/people.md` (row 8), Proposed change 17:** mark the redelivery part applied (a failed `identity.account-created` handling with a retryable kind is redelivered through `signals.redeliver`); the `people.retention.unused` proposal stays with `people` (its key would be `people.retention_unused` under the underscore grammar, D39, declared by `people` when it registers that job).
8. (applied 2026-10-20, D43) **`plan/07-abstractions.md`, `jobs` entry:** add "Owns: the `Registrar` contract (`Register`, `Enqueue`; = `Deps.Jobs`, scoped per owner), the table `core.job` (scheduled slots and deferred jobs, record references only), the runner (`SKIP LOCKED` leases with a heartbeat, backoff by `retry.Policy`, writer only), the `signals.Deferrer` implementation and `signals.redeliver`, `jobs.purge`, the status screen behind `jobs.read` / `jobs.manage`. Doesn't do: notify's sender loop (notify decision 2), retention periods (each area's setting), daily local-time schedules (Later), cross-process messaging." and the spec link.
9. (applied 2026-10-20, D43) **`plan/09-core-features.md`:** §1 "retryable ones handed to `jobs`" now holds (signals.md decision 3); §8 and §11 "Specs" lines may cite `jobs.md`; the milestone table's M4 row gains "a scheduled purge runs on the writer and shows on `/jobs`"; the admin screens list (§7) gains `/jobs` for site admins. Also `.claude/skills/go-conventions/SKILL.md` and `add-module`: one sentence each: "From M4 `deps.Jobs` (a `jobs.Registrar` scoped to the module): `Register` in `Start` with names `<name>.<ident>`, `Enqueue` inside the use case's transaction with record references only, handlers idempotent and running as the system; tests use `jobstest.NewFake(Manifest.Name)` and `Fake.Run`." (`spec-editor` applies after approval.)
10. (applied 2026-10-20, D44) **`plan/design/audit.md` (row 12), decision 6 and `ActorFunc`:** the function may answer `ActorSystem` (no `Person`, no `Superuser`); `resolveActor` accepts a consistent person, operator or system and stays `Internal` for anything else (new F13b with `TestResolveActor_ActorFuncReturnsBadActor_ReturnsInternal` and `TestResolveActor_ActorFuncReturnsSystem_IsTheSystem`). **`plan/design/app.md` (row 18), part 8 row and "Operator attribution":** `auditActor` answers `{Kind: ActorSystem}, true` when `jobs.InJob(ctx)`, before the session and the operator (`TestAuditActor_InJob_IsTheSystem`, including a ctx that carries both). Both amendments are written in those specs; `spec-editor` folds them into the body on approval.
11. (applied 2026-10-20, D44) **`.claude/skills/add-module/SKILL.md` and `.claude/skills/go-conventions/SKILL.md`, the `deps.Jobs` sentence of Proposed change 9:** add "a dedupe `Key` names the record's own ID (the sign-up, the shift, the upload), never a person ID: keys show on `/jobs` and in logs" (Q-JOBS13). **`plan/design/module-contract.md` (row 4), `moduletest` rules:** the leak rule also checks `Fake.Enqueued()` keys against the seed person markers (`jobs_key_not_person`). **`plan/design/app.md` (row 18), `bms check`:** as accepted (Q-JOBS10), the "jobs" section prints a failing `Start` as a problem line and does not set `ExitFailure`; for Q-JOBS9 nothing changes elsewhere (the run's own ctx); for Q-JOBS11 nothing changes in telemetry.md (`lost_lease` is already an outcome). The Q-JOBS8 and Q-JOBS12 changes are inside `jobs` and `jobstest` only.
12. (applied 2026-10-20, D44, by files.md Proposed change 9) **`plan/design/files.md` (row 14):** decision 11's list of the first scheduled jobs gains `files.reap` (hourly) and `files.sweep-orphans` (daily), both registered by `app` through `runner.Scoped("files")` with closures over the files service, like `audit.purge`; nothing else changes in jobs.
13. (J1; applied 2026-10-23, D50, with volunteers Amendment V2) **`plan/design/module-volunteers.md`:** Amendment V2 (drafted with J1): decision 25's carry-forward and V1's "reminded once more for its new time" rely on decision 23; F112a asserts `jobstest.Fake.Queued()`; new F112b (`RunDue` across two horizons on a fake clock), F112c (the real runner, integration) and F151a (a move after the reminder was sent); no code change in the module.
14. (J1; applied 2026-10-23, D50) **`.claude/skills/add-module/SKILL.md` and `.claude/skills/go-conventions/SKILL.md`, the `deps.Jobs` sentence:** add "with a `Key`, the latest request wins: a repeat moves, reopens or (while the row runs) is held and applied after the run, so a handler may `Enqueue` its own key to run again later; a test asserts the schedule with `jobstest.Fake.Queued()` and drives it with `Fake.RunDue`, never from `Enqueued()` alone". **`plan/design/module-contract.md` (row 4), `moduletest`:** the leak rule scans `Fake.Queued()` as well as `Fake.Enqueued()` (a held request's args are rows too). **`plan/design/telemetry.md`:** no new outcome token; the `jobs: a job ran` log line gains the boolean attribute `next` (allowed by the key grammar; no denylist change).
