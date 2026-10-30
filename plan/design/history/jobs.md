# jobs: amendment history

Amendment narratives, reasons, review findings and superseded wording moved verbatim out of `plan/design/jobs.md` on 2026-10-30, when the amendments were folded into the spec body (only line breaks at sentence ends were added; no word changed).

## Notes moved from the spec header

- **Amendment J-W1 (raised 2026-10-30, D57; Approved D58; Implemented 2026-10-30):** the Background work screen for people.
What the demo walkthrough found on `/jobs` (raw names and owner tokens, "On copy demo", thirty healthy reminders under "Failed and waiting", sign-up UUIDs in a Key column, headers broken mid-word at 1280 px) and the owner's accepted direction (D57) made concrete: a job's title is the catalogue key `module.<owner>.job.<ident>.title` beside the owner's `module.<owner>.title` (decision 24; core owners without a web area use the shell catalogue; a Go test fails the build when a registered name has no `en` title; `Decl` gains no field); `StatusView.Recent` becomes four server-capped lists `Failed`, `Running`, `Retrying`, `Waiting` plus `Instance` (decision 25); the screen shows no ID, key or raw name, lists each state under its own heading with a counts line for the phone, says "server" for an instance and only when another server ran the job, and renders a 403 as the shell's error block with the reference; the kit's `Table` headers never break inside a word.
Section "Amendment J-W1" before Open questions; F59 to F67; Q-JOBS15 and Q-JOBS16 answered (D58: keys hidden entirely for now; "server" is the word); Proposed changes 15 to 19 applied 2026-10-30 (D58).
- **Amended 2026-10-28 (D52):** (a) when a run's own `Enqueue` of its key made the held request and the run then fails (any non-retryable kind, a panic, or a retryable error), the row is queued at the held `RunAt` with its `attempts` **kept**, not reset to 0, and the held request is applied only while `attempts < Retry.MaxAttempts`: at the limit it is dropped and the row ends `failed` (`gave_up`), a narrow exception to F55 for self-made requests only, so a self-rescheduling job that keeps failing cannot retry forever (mechanism: column `next_by_run`, migration `0003_job_next_by_run.sql`; the run's row ID travels in ctx, `rules.WithJob`/`rules.RunID`); a request from outside the run still resets attempts (D50; F53, F55); a run that ends with nil still resets (F54).
(c) `jobstest.Fake` fidelity: `RunDue` builds the `Job` from the row's current `Args`; a row created by `Run` (a `Key` with no row) is tracked like an enqueued one; a retryable error (`Unavailable`, `Timeout`) requeues the row with the attempt counted, until `jobs.DefaultRetry.MaxAttempts`, instead of ending it `failed`; the doc says the Fake skips retry delays (a requeued row is due at once).
F55, F58 and the Fake's doc comments are amended; new tests `TestSelfReenqueue_FailingRun_KeepsAttemptsAndGivesUp`, `TestFakeRunDue_UsesCurrentArgsAfterLaterEnqueue`, `TestFakeRun_CreatedRow_IsTrackedLikeAnEnqueuedOne`, `TestFakeRun_ErrorKinds_RequeueOrFail`.
- **Amendment 2026-10-20 (approved, D44):** Q-JOBS6 corrected.
Its recommendation said "no change to `audit`", but audit's `resolveActor` accepts only a person or the operator from the `ActorFunc` and answers `Internal` for `ActorSystem`, so the approved wiring would fail every `Record` in a module's job handler.
The intent stands (the actor inside a job is the system, through `app`'s `auditActor` over `jobs.InJob`); it needs audit.md's amendment of the same date (the function may answer `ActorSystem`; F13b) and `auditActor` to check `InJob` before the session.
Nothing in `jobs` changes.
Proposed change 10.
- **Amendment 2026-10-20 (approved, D44):** what the review of the M4 implementation found and the code now does.
(1) **Poison rows.** A tick's writer pass begins with a **reclaim** step (2a): lapsed `running` rows go back to `queued` with `last_error = lost_lease`, or to `failed gave_up` once `attempts >= Retry.MaxAttempts`; and the lease's lapsed arm gains `attempts < Retry.MaxAttempts`.
Before this a job that killed its process (a fatal in a handler) at its last attempt was re-leased forever, taking the server down once per `Lease`.
(2) **No overlap under concurrency.** The "no open row of this name" test in the scheduler's slot insert and in `RunNow` is serialized by a transaction-scoped advisory lock on the name, taken in the same `Write` transaction immediately before the insert; the scheduler takes names in sorted order, so two ticks cannot deadlock.
(3) F45 to F50 with their tests (`integration_concurrency_test.go`).
(4) Reviewer-accepted deviations: `Runner.Declared()`, exported `MaxNameBytes` and `MinTimeout`, `Status.MarshalText`/`UnmarshalText`, route `GET /` (the shell mounts `{$}`), `Enqueue` runs `Transact(Write)`, the `bms.jobs.queue` gauge reports `queued` and `running` only, `jobstest.Fake.Fail` is one-shot, `Close` also waits for `RunOnce`.
(5) Q-JOBS8 to Q-JOBS13 settled with the recommendations (decisions 18 to 22 below); Proposed changes 10 and 11 applied.
- **Amendment J1 (raised 2026-10-23, approved 2026-10-23, D50):** the dedupe key's rule is changed from "a waiting row is moved, a running or final one makes the request a no-op" to **"the latest request for a `(name, key)` wins"**: a `queued` row is moved (as before), a final row (`done`, `failed`, `canceled`) is reopened as a fresh `queued` row, and a request made while the row is `running` is held on the row (`next_run_at`, `next_args`, migration `0002`) and applied the moment the run leaves `running`, whatever its outcome.
Found by the volunteers V1 build: the reminder handler's carry-forward at the horizon (volunteers decision 25) enqueued its own key from inside its run, which the old rule made a silent no-op, so a reminder further ahead than `MaxDelay` never fired; the same rule also made an `UpdateShift` within `DoneTTL` of a sent reminder a no-op.
`jobstest.Fake` now keeps a row per key with the same states and transitions (`Queued()`, `RunDue`), so a test asserts the schedule the queue would hold, not the requests it heard.
Decision 3 and F43 reworded, decision 23, F53 to F58, Q-JOBS14.
The text below is the spec as amended; the record with the alternatives is "Amendment J1" before Open questions.

## Notes moved from the Summary

**Amended 2026-10-28 (D52).** A run's own re-enqueue of its key keeps `attempts` when the run then fails and is applied only while attempts are left, else the row ends `failed` (`gave_up`) (`next_by_run`; outside requests still reset, D50); `jobstest.Fake` runs the row's current `Args`, tracks rows `Run` creates, requeues retryable errors (attempt counted, up to `DefaultRetry.MaxAttempts`) and skips retry delays.

**Amendment J-W1 (2026-10-30, D57; Approved D58; Implemented 2026-10-30).** The screen for people: a job's title is `module.<owner>.job.<ident>.title` in the owner's catalogue (decision 24; `TestMessageCatalogues_EnglishCoversJobs` fails the build when one is missing; `Decl` unchanged); `StatusView` = `{ReadOnly, Instance, Registered, Counts, Failed, Running, Retrying, Waiting}`, each list capped at `MaxRecent` on the server, `Recent` gone (decision 25; `Retrying` = `queued` with `last_error` set); `/jobs` shows titles only (never an ID, a key, an argument or a raw name except as the fallback), the sections Failed / Running now / Waiting to retry / Waiting / Jobs with a counts line, "server" for an instance and only for a run by another server, a 403 as the shell's error block with the reference; `Table` headers `break-normal`.
F59 to F67; Q-JOBS15 (keys hidden entirely; a link from a waiting row to its record is a possible later addition) and Q-JOBS16 ("server" is the word for an instance) answered (owner, 2026-10-30, D58); Proposed changes 15 to 19, applied 2026-10-30 (D58) (web-shell, module-contract decision 8 and the two skills, module-volunteers, audit, app).

## Amendment J1 (raised 2026-10-23, approved 2026-10-23, D50): a request for a key whose row is running or finished

**What the volunteers V1 build found.** Volunteers decision 25 parks a reminder due beyond `MaxDelay - 24h` at the horizon and has the handler re-enqueue its own key while `Starts - lead` is still ahead.
The handler's `Enqueue` ran while its own row was `running`, and decision 3's upsert (`... DO UPDATE ... WHERE core.job.status = 'queued'`) matched nothing: the request was a silent no-op (the existing ID came back), the handler returned nil, the row ended `done`, and a booking more than a year ahead was never reminded.
`jobstest.Fake` recorded every request without a row state, so volunteers F112a passed.
The same rule had a second hole the build had not hit: after a reminder fired its row is `done` for `DoneTTL` (seven days), and an `UpdateShift` that moved the shift later in that time was a no-op too, so V1's "a shift moved after a reminder was sent is reminded once more for its new time" held only after the purge.
Both are one defect: a `Key` request has a state in which it is dropped without a word.

**Chosen: the latest request wins (decision 3 reworded, decision 23).** With a `Key`, `Enqueue` makes the key's row that request whatever state the row is in: a `queued` row is moved (as before), a final row is reopened as a fresh `queued` row, and a `running` row holds the request in two new columns (`next_run_at`, `next_args`, migration `0002`) that every statement taking a row out of `running` applies first (complete, requeue, release, fail, the reclaim's two arms): `queued` at the held time, `attempts = 0`, the run's own outcome logged, metered and kept in `last_error`.
One statement branch and one SQL fragment carry the whole rule; no new interface, sentinel or module API, and the volunteers handler already written (`Enqueue` on its own key from inside the run) becomes correct without change.
The reasons for this shape over the alternatives: (a) the silent branch is the defect, so the fix removes it rather than giving modules a second way around it; (b) no `Enqueue` consumer relies on "final is a no-op": volunteers' two keyed jobs both want a repeat to run again, the redelivery uses `Defer` (its own insert, unchanged: a delivery is immutable, so a repeat is the same work), scheduled slots and `RunNow` use their own inserts and the unique index over every status still keeps a `done` slot from being reinserted for the rest of its period; (c) the move-while-running window, however short, is closed too, which no handler-side primitive can do.

**Not taken.** (1) *A sentinel, `jobs.RunAgainAt(t) error`, returned by the handler*: one verdict and one statement, and it carries a parked reminder forward; but it leaves `Enqueue` on a `done` row a no-op for `DoneTTL` (the second hole) and `Enqueue` on a `running` row silently dropped, so volunteers would still need a second fix and the trap would stay for the next module.
(2) *Continuation keys in volunteers only* (`<signup>:<hop>` or `<signup>:<starts>`): the handler can park itself under a new key, but `UpdateShift` cannot cancel the old row (modules have no `Cancel`), so a moved shift leaves a parked row that fires, re-reads, re-parks under yet another key, and so on, one more live row per move; correctness then rests on the handler's re-read plus a dedupe column or notify's key for the send, the `done`-within-`DoneTTL` hole needs a third key component, and `/jobs` fills with hop keys.
Correct under the move-while-parked case only with all three, and still wrong for any other module that keys by its record.
(3) *A partial unique index on `queued` rows only* (so a running or final row never conflicts and a request always inserts): breaks the scheduler, whose "insert the slot once" relies on a `done` slot still conflicting until the next slot.
(4) *Dropping the held request on a final failure* (keeping the row `failed` for `/jobs`): hides a request the caller made in good faith behind a failure of an older one; F55 keeps the newer request and leaves the failure in the log, the span and the metric, and a persisting bug shows as `failed` on the very next run, which has no held request.

**What changes in the code.** Migration `0002_job_next.sql` (two nullable columns, two checks); `enqueue_job.sql` becomes the three-branch upsert that always returns a row (`Store.Enqueue` loses its retry loop and `byKey`); `complete_job.sql`, `requeue_job.sql`, `release_job.sql`, `fail_job.sql` and `reclaim_expired_leases.sql` gain the "apply the held request first" fragment and return `status`; `settle` and `reclaim` log `next=true` when the row stayed open; `jobstest.Fake` keeps a row per key with the real transitions and gains `Queued()` and `RunDue`; the key rule is one table shared by the PostgreSQL suite and `jobstest` (Test plan).
Nothing changes in `Registrar`, `Record`, the routes, the screen, the metrics' outcome set or any module's code.
The volunteers spec records the dependency as Amendment V2.

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

**Decision 24 (J-W1).
A job's title is a catalogue key derived from its name, `module.<owner>.job.<ident>.title`, held in the owner's web catalogue; `Decl` gains no field.** Module-contract decision 8 already fixes that the manifest carries no human-readable text and that titles are keys derived from names (`module.<name>.permission.<rest>.title`, `.signal.<rest>.title`, `.template.<ident>.title`); a job is the same shape.
The owner's own title is the existing `module.<owner>.title`.
`Register` stays a no-I/O call with no text in Go, `Registered` on the wire stays `{name, owner, …}`, and the browser looks the keys up as it already does for an owner's title.

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

**Fallback when a key is missing.** The screen shows the raw name in `<code translate="no">` (for a job) or the raw owner token (for an owner), as it does today, and nothing else changes: a missing translation must never hide a job.
The fallback is a safety net, not a state a shipped build reaches, because a Go test fails the build: `TestMessageCatalogues_EnglishCoversJobs` (`internal/app`, beside `TestMessageCatalogues_EnglishCoversManifests`) builds the runner and every compiled-in module against `db.None()` through the path `bms check` uses, calls each `Start`, reads `Declared()`, and asserts that the union of the shell's `en` catalogue and every `web/src/{modules,core}/*/messages/en.json` holds `module.<owner>.title` and `module.<owner>.job.<ident>.title` for every `Registered`.
A `Start` that fails under `db.None()` fails this test (unlike `bms check`, which only prints it, Q-JOBS10): the test cannot learn that module's names, so it cannot vouch for them.
The invented `zq*` modules the app tests register are skipped as in the manifest test.

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

**Query.** One statement, `list_queue.sql`, in the one `Read` transaction `Status` already uses, so the four lists are one snapshot: a `UNION ALL` of four arms (`failed`, `running`, `retrying`, `waiting`), each with its own `ORDER BY <the bucket's order>, id LIMIT MaxRecent`, so each arm walks an index and none scans the table (D58 review; this replaces a `row_number()`-per-bucket form).
The `failed` arm uses `job_failed_idx (finished_at DESC, id DESC)`, not `job_finished_idx`, which serves the purge; the queued arms (`retrying`, `waiting`, split on `last_error` null or set) and the `running` arm use `job_due_idx`.
The service sorts nothing in Go.

### W1.6 The screen

Core area `jobs`, screen `/jobs` ("Background work"), in the admin navigation for holders of `jobs.read`; phone first. Keys `core.jobs.*`. In order, top to bottom:

1. **`h1` "Background work"** and the intro sentence (W1.3).
2. **Read-only banner** when `StatusView.ReadOnly` (or the shell's read-only header): the sentence in W1.3; the three actions are hidden.
3. **The counts line** (`<nav aria-label="Sections">`, `core.jobs.summary.*`): three in-page links with counts from `Counts`: "Failed ({failed})", "Running ({running})", "Waiting ({queued})", each to its section's heading. It exists for the phone: an admin sees at a glance whether anything failed and jumps to it without scrolling past the cards. Items wrap as whole items; none breaks inside a word.
4. **Failed** (`h2`, `#jobs-failed`): a table of `Failed`, caption "Failed jobs, newest first", columns **Job** (the title, W1.4; the row header), **Failed** (`finished_at`), **Attempts**, **Problem** (the token in words, raw token in `<code>` as fallback, as today), **Actions** (`jobs.manage`: **Retry**). Empty state: "Nothing has failed." After a successful Retry the toast says "Queued again" and focus moves to this heading.
5. **Running now** (`h2`, `#jobs-running`): a table of `Running`, columns **Job**, **Started** (`started_at`), **Attempt**. No action (a running row cannot be retried or canceled, F28, F29). Empty state: "Nothing is running right now."
6. **Waiting to retry** (`h2`, `#jobs-retrying`): a table of `Retrying`, columns **Job**, **Due** (`run_at`), **Attempts**, **Last problem** (the token in words), **Actions** (`jobs.manage`: **Cancel**, with the confirm dialog). Empty state: "Nothing is waiting to retry."
7.
**Waiting** (`h2`, `#jobs-waiting`): a table of `Waiting`, columns **Job**, **Due**, **Actions** (`jobs.manage`: **Cancel**).
No Status column (the section says it), no Attempts (always 0 here), no Problem (always none here: that is what the walkthrough's "Problem: None" thirty times was).
Empty state: "Nothing is waiting." When the list holds exactly `MaxRecent` rows a line under the table says "The first {n} are shown; {total} are waiting in all." (`total` = `Counts.queued`); the same line, with its own wording, under the other three lists when they are full.
8.
**Jobs** (`h2`, `#jobs-registered`): the registered cards, grouped by owner under an `h3` that is the owner's title (W1.4), owners sorted by their title; within an owner, scheduled names first, then on-demand, each group sorted by title.
A card shows: the **job's title** as its `h4` (the raw name is **not** shown; it appears only as the fallback of W1.4); "Runs every N" or "Runs when something asks for it"; the last run ("Last run: {status}, {when}.
Took …"), its problem if any, and "Next due: {when}" for a scheduled name; the **Run now** button for scheduled names (`jobs.manage`; disabled with "Already waiting or running." while a row of the name is in `Running`, `Retrying` or `Waiting`, or `Last` is `queued` or `running`).
Empty state: "Nothing is registered."

**No IDs, no keys, no arguments.** The screen never renders `Record.ID`, `Record.Key`, `Record.TraceID` or an argument: the Key column is gone (that is where the sign-up UUIDs were), and the accessible names of Retry and Cancel use the job's title and a time, not the name or the ID.
Two waiting rows of one job differ by their due time, which is what an admin cancels by.
The row ID still travels in the Retry and Cancel requests (`/queue/{job}/…`); it is in the DOM as a React key only.
An operator who needs to match a row to a log line uses `GET /api/jobs/` or the log's `job_id` (Not responsible for, W1.2).

**Another server.** The instance line is shown only when a run's `Instance` differs from `StatusView.Instance` (the server that answered), as "Ran on another server ({instance})" under the card's last run, and as the same sentence under the Job cell of a `Running` row.
A one-server site, which is every regional today, never sees it; a site with two writers (a restart overlap, an operator who ran two) sees it only on the runs the other one did.
Not taken: showing it always with a better word (the name of the only server is noise on every card), and dropping it (two writers is the one case an operator needs to see which process ran a job that misbehaved).

**Forbidden and errors (the "error summary").**
- A direct visit without `jobs.read` in the session: the shell's forbidden block (`shell.forbidden`, "You don't have access to this screen.") rendered with the shell's `ErrorState` shape; no request is made, so there is no reference. The local `Forbidden.tsx` and `ScreenError.tsx` fold into one component that takes an optional `ApiError`.
- A `403` on the read (the session was stale): the shell's `ErrorState` with the `shell.forbidden` sentence and the **reference line**, no Retry button (a retry cannot help). Test `shows the forbidden sentence and the reference for a 403 on the read`.
- A `403` on an action (Run now, Retry, Cancel: `jobs.manage` revoked since the page loaded): the shell's error block beside the control that was pressed (`ErrorState` under the card for Run now and under the section heading for Retry; `ErrorSummary` inside the Cancel dialog), with the shell's `error.permission` sentence and the reference line; the actions then disappear on the refresh that follows every action (the session is re-read).
Never a toast, never silence.
Tests `shows the error summary with the reference when Run now is refused with 403`, `keeps a 403 on Cancel in the dialog`.
- Any other failed read: `ErrorState` with the reference and Retry, as today.

**Phone layout** (320 px up; the hard requirement, web-shell D21):
- Below 640 px every table is the UI kit's stacked `Table`: each row a bordered block, each cell labelled with its header; the counts line and the section headings are the way around the page; the cards are one column; every button is a 44 × 44 px target.
- From 640 px: real tables; cards in two columns.
- **Headers never break inside a word, at any width.** The kit's `Table` sets `break-normal` on every `<th>` (overriding the body's `overflow-wrap: anywhere`), so a header wraps between words or not at all; with the Key column gone no cell uses `break-all`, and the widest table (Failed) has five columns.
A title wraps between words.
The raw-name fallback, when it shows, may wrap anywhere (the body rule) so a 96-byte name cannot overflow a 320 px screen.
This is a one-class change in `web/src/ui/Table.tsx` (web-shell's kit): Proposed change 15.
- Nothing scrolls horizontally at 320 px (the smoke run asserts it at the phone viewport, as for every screen).

### W1.7 Permissions, data, signals, settings

Unchanged. `jobs.read` covers the `GET` whatever shape it returns; `jobs.manage` the three actions. `core.job` is not touched; no personal data is involved (the screen shows fewer fields than before, never more). No signal, no setting: nothing here differs by regional (the words are the catalogue's, and a regional that wants other words translates them, web-shell D28).

### W1.8 Failure modes added or changed

F59 to F67 are in the main Failure modes table (applied 2026-10-30, D58).
`Status`'s earlier modes (F20, F21) stand. The screen's other states (`ErrorState` with Retry on a failed read; the empty states; the read-only banner) are unchanged and keep their tests, reworded for the new headings.

### W1.9 Test plan additions

- **Go, `jobs`:** F59 to F62 against the fake store and, for `list_queue.sql`, the integration suite (`TestStatus_ListQueue_OneSnapshot`: rows inserted in every status with `last_error` set and null; the four lists, their order, the cap). The OpenAPI fragment test asserts `recent` is gone and the four arrays are present.
- **Go, `app`:** `TestMessageCatalogues_EnglishCoversJobs` over the real tree (skipped when `web/src` is absent, like the manifest test); `TestMessageCatalogues_FixtureStartFails_Fails` with an invented `zq*` module whose `Start` errors.
- **vitest, `web/src/core/jobs/jobs.test.tsx`:** the suite is rewritten around the new shape: titles from the catalogue and never the raw name or a key in the DOM (`screen.queryByText(/5b1f0000/)` is null; no `<code>` with a dot-name when every title exists); the four sections with their own headings and empty states; a healthy queued row appears under Waiting with no Problem cell; a requeued row under Waiting to retry with its problem; Retry only under Failed, Cancel only under the two waiting sections; "Job" column header; the three "That job…" sentences; the counts line's links; the 403 cases (F65, F66); the another-server rule (F67); axe on the full page.
Accessible names: `Run now: Delete finished job records`, `Retry Send shift reminders, failed …`.
- **vitest, `web/src/ui/kit.test.tsx`:** every `<th>` of `Table` has `break-normal`.
- **End to end (Playwright smoke, phone and desktop viewports):** `/jobs` as a site admin shows "Background work", no horizontal scroll at the phone viewport, and no header taller than one line at 1280 px (the `th` height equals the line height).

### W1.10 Not taken

- *A module-supplied description for a key* (a `Describer` the owner registers with the name, answering "Dusty's Gate shift, Sat 14:00" for a sign-up ID): a new contract method, a per-row lookup under the module's own permission check, and a personal-data path (a shift roster is `internal` at best, a playa name shows on a screen that is `internal` for site admins) for a column nobody acts on.
If an admin ever needs to know *which* reminder a row is, the volunteers module's own screens are where the sign-up lives; a link from a job row to its record is the later, smaller addition (Q-JOBS15).
- *Keeping one "Recent" list and splitting it in the browser:* the cap problem (W1.5).
- *Showing the raw name in small type under the title:* the duplication the walkthrough disliked, in a new coat; an operator has `bms check` and the API.
- *Hiding empty sections:* a section that comes and goes is harder to learn than one that says "Nothing has failed."
- *Changing `MaxRecent`:* fifty per list is plenty for a screen; the count line carries the totals.

### W1.11 Open questions (J-W1)

- **Q-JOBS15. Keys hidden entirely, or a later link from a waiting row to its record?** **Answered (owner, 2026-10-30, D58): hide keys entirely for now.** A link from a waiting row to its record is a possible later addition that needs a small owner-side contract; build it only when a regional asks.
- **Q-JOBS16. "Server" as the word for an instance** ("Ran on another server (main)"; "This server is read-only…"; "That job isn't registered on this server."). The shell's read-only banner says "This copy is read-only"; "copy" was the jargon the walkthrough flagged on this screen. **Answered (owner, 2026-10-30, D58): "server" is the word for an instance on this screen.** Whether the shell's banner follows is web-shell's call (Proposed change 15 notes it; no change is made to it).

### W1.12 Proposed changes elsewhere (J-W1)

15.
(applied 2026-10-30, D58, in web-shell.md) **`plan/design/web-shell.md` (row 11):** (a) the UI kit's `Table` sets `break-normal` on every `<th>` so a column header never breaks inside a word (a one-class change, `web/src/ui/Table.tsx`, with a kit test); (b) the shell catalogue `web/src/shell/messages/en.json` gains `module.files.title`, `module.files.job.reap.title`, `module.files.job.sweep-orphans.title`, `module.signals.title`, `module.signals.job.redeliver.title` (W1.4: core owners with no web area keep their titles beside `module.core.title`); (c) the screen table's `/jobs` row (if listed) says "Background work" with the sections of W1.6; (d) the English coverage check (decision 8, Proposed change 7) covers `TestMessageCatalogues_EnglishCoversJobs`; (e) a note that this screen says "server" for an instance (Q-JOBS16), for the shell's read-only banner to follow or not.
16.
(applied 2026-10-30, D58, in module-contract.md decision 8 and the add-module and go-conventions skills) **`plan/design/module-contract.md` (row 4), decision 8:** add `module.<name>.job.<ident>.title` for every job name the module registers in `Start` (checked by `TestMessageCatalogues_EnglishCoversJobs` in `internal/app`, which learns the names through `Start` under `db.None()` as `bms check` does); `Decl` carries no title.
**`.claude/skills/add-module/SKILL.md` and `.claude/skills/go-conventions/SKILL.md`, the `deps.Jobs` sentence:** "and a title `module.<name>.job.<ident>.title` in `en.json` for every name it registers"; and "keys show on `/jobs` and in logs" becomes "keys appear in `GET /api/jobs/` and in logs, never on the screen" (the rule "never a person ID" stands).
17. (applied 2026-10-30, D58, in module-volunteers.md) **`plan/design/module-volunteers.md` (row 20):** the four `module.volunteers.job.<ident>.title` keys of W1.4 join the module's catalogue (English as proposed, with the V3 terms); no code change in the module.
18. (applied 2026-10-30, D58, in audit.md) **`plan/design/audit.md` (row 12):** `module.audit.job.purge.title` joins `web/src/core/audit/messages/en.json` (one key; the job is registered by `app`, the title lives with the audit area's screen).
19. (applied 2026-10-30, D58, in app.md) **`plan/design/app.md` (row 18):** `bms check` is unchanged; the `internal/app` catalogue tests gain `TestMessageCatalogues_EnglishCoversJobs` and its fixture test (W1.4, F64).

**Applied 2026-10-30 (D58).** When approved, the body of this spec changes as follows (`spec-editor`): the UI section becomes W1.6; `StatusView` in the Interface and the Types list become W1.5 (`Recent` removed, `Instance`, `Failed`, `Running`, `Retrying`, `Waiting` added); the `Request.Key` comment "keys show on /jobs and in Status.Recent" becomes "keys appear in `GET /api/jobs/` and in logs, never on the screen"; `jobs.read`'s "Allows" cell reads "the registered names, last runs, counts, and the failed, running, retrying and waiting rows"; decisions 24 and 25 join the Summary; F59 to F67 join the table; Q-JOBS15 and Q-JOBS16 join the Open questions with their answers.

## Open questions as they stood

- **Q-JOBS15 (J-W1).** Keys hidden entirely, or a later link from a waiting row to its record? **Answered (owner, 2026-10-30, D58): hide keys entirely for now** (W1.11).
- **Q-JOBS16 (J-W1).** "Server" as the word for an instance on this screen. **Answered (owner, 2026-10-30, D58): yes** (W1.11).
- **Q-JOBS14 (J1).** Accept "the latest request for a `(name, key)` wins" (decision 3 reworded, decision 23) in place of "a running or final row makes the request a no-op", with the held request kept across a failed run (F55)?
**Answered (owner, 2026-10-23, D50): yes.** Recommendation was yes: It removes the one state in which a keyed request vanished silently, it is the behaviour the only keyed consumers (volunteers' reminder and `cancel-shifts`) already assumed, and the cost is two nullable columns and one SQL fragment.
The alternative that keeps decision 3 intact (a `RunAgainAt` sentinel) fixes the carry-forward alone and leaves a moved shift unreminded for seven days after a reminder was sent.

- **Q-JOBS1. Does the `/jobs` screen, with Run now / Retry / Cancel, ship in M4, or status only, or Later?** Recommendation: ship it in M4 as specified. It is one screen over one `GET` and three `POST`s, and the first module with reminders (volunteers) will raise "did the reminder run?" on day one; without `Retry` a failed purge or reminder has no path but a restart.
- **Q-JOBS2. The give-up policy: ten attempts, 1 m doubling to 1 h, about four hours; the same for retried signal deliveries.** Recommendation: accept. Longer would hide a broken dependency; shorter would lose a reminder over a short outage. An admin can `Retry` for seven days, and the log and metric keep the record.
- **Q-JOBS3. Should notify's sender tick move onto `jobs` now?** Recommendation: no (notify decision 2 stands; 09 §6 says "later"). Notify's loop is implemented, tested and one `SKIP LOCKED` lease like this one; moving it buys nothing in M4 and would couple the two parts. Revisit when a third loop appears or when the audit importer and notify's loop and this one are visibly the same code.
- **Q-JOBS4. A daily schedule at a time of day in `regional.timezone` (for example "purge at 03:00 local") in M4 or Later?** Recommendation: Later. `Every` covers retention and daily scans; a reminder tied to a shift is a deferred job with `RunAt`. The addition is a second `Decl` field and a `ZoneSource` seam, not a redesign.
- **Q-JOBS5. `DoneTTL` of 7 days as a constant, or a `jobs.retention` setting?** Recommendation: a constant. The rows hold no personal data (D30 is about personal data), and the only effect of the number is how long an admin can `Retry` and how long a dedupe key blocks a repeat.
- **Q-JOBS6.
The audit actor inside a job: `app`'s `auditActor` answers `ActorSystem` when `jobs.InJob(ctx)` (as audit itself does for `signals.InDelivery`).** Recommendation: accept.
Approved (D43) with the words "a two-line change in `app` and no change to `audit`"; corrected 2026-10-20 (approved, D44): audit's `resolveActor` accepts only a person or the operator from the `ActorFunc` (`Internal` otherwise, pinned by its order test), so it is a few lines in `app` **and** a one-kind widening in audit (audit.md amendment 2026-10-20, F13b: `ActorSystem` with no `Person` and no `Superuser` is accepted; everything else the function might answer is still `Internal`).
`auditActor` checks `InJob` before the session, as audit checks the delivery before the function (`jobstest.Fake.Run` runs on the test's ctx, which may carry one).
`audit.purge` and signal redelivery were never affected (`Purge` sets its own actor; redelivery is `InDelivery`); the first module job that audits is.
- **Q-JOBS7. `Workers` (2), `PollInterval` (10 s), `Lease` (2 m), `RunTimeout` (5 m) as constants `app` passes, with no new `BMS_JOBS_*` configuration keys in M4.** Recommendation: accept; a small server with one database needs no tuning yet, and a config key is one `config.md` line when a regional asks.

The six below come from the review of the M4 implementation (amendment 2026-10-20). All accepted as recommended (owner, 2026-10-20, D44); each is folded into the body (decisions 18 to 22, F51, F52) and the answer is stated here.

- **Q-JOBS8. `Retry` of a failed scheduled slot while another row of that name is open.** Settled: `Retry` of a scheduled name refuses with `Conflict` (code `running`) while another row of the name is open, under the name lock (decision 18, F51). (owner, 2026-10-20, D44)
- **Q-JOBS9. A run whose heartbeats keep failing past its lease expiry keeps running.** Settled: the run is canceled at `now >= fence` with `outcome=lost_lease` and nothing more is written (decision 19, F52). (owner, 2026-10-20, D44)
- **Q-JOBS10. `bms check` calls each module's `Start` under `db.None()` to learn its job registrations, and fails the check when a `Start` fails.** Settled: `bms check` prints a module's `Start` failure in its jobs section and continues; it does not fail the check (app.md, `bms check`); `serve` still stops on a failing `Start`. (owner, 2026-10-20, D44)
- **Q-JOBS11. Count a reclaim's requeue in `Ran.Released` and `bms.jobs.runs{outcome=lost_lease}`.** Settled: a reclaim's requeue counts in `Ran.Released` and `bms.jobs.runs{outcome=lost_lease}` (decision 20). (owner, 2026-10-20, D44)
- **Q-JOBS12. `Request.Validate` and `jobstest.Fake` bound `RunAt` against the wall clock (`time.Now() + MaxDelay`), while the runner checks against its own clock.** Settled: `jobstest.NewFake(owner, opts ...Option)` with `WithClock`; `Request.Validate` keeps the wall clock (decision 21). (owner, 2026-10-20, D44)
- **Q-JOBS13. A dedupe key must never carry a person ID.** Settled: a dedupe key never carries a person ID; rule `jobs_key_not_person` (decision 22). (owner, 2026-10-20, D44)
