# Audit

- **Status:** Implemented (2026-10-18); Approved (2026-10-18; owner approved every recommended answer)
- **Amended 2026-10-28 (D52(e)):** the audit search runs with a custom plan: `plan_cache_mode = force_custom_plan` is set for the search statement, per transaction inside the `Read` transaction with `SELECT set_config('plan_cache_mode','force_custom_plan', true)` (transaction-local, the form `SET LOCAL` takes, so it works through `Query`), so a generic plan chosen for the optional filters does not slow the keyset query. Behavior "Search" updated.
- **Amended 2026-10-18 (owner approval of authz/people):** an import insert that fails with `Invalid` (for example a CHECK violation) sets that file aside as `.failed` with a warning and the other files continue (F36); `Unavailable`, `Timeout` and `Canceled` keep the file for retry as before; Proposed change 4 applied (authz: `TitleSource`, `PermissionChecker` is `Allow`, `StarterRoles: {authz.RoleAuditReviewer}`, records through `Scoped("access")`, session ignored inside `InDelivery`) and Proposed change 5 applied (people: the actions and `Fields` listed there, `people.person.exported` at level `sensitive`, `CountAbout` consumed in M2 not M4, the person page links to `/audit?subject=<person>` and the screen's subject filter accepts `?subject=`).
- **Amended 2026-10-18 (M1 build, accepted deviations):** implementation deviations recorded in Behavior, "Accepted implementation deviations (M1 build, 2026-10-18)"; `ActorKind.String` is exported and `audit.SpoolCounts(dataDir) (waiting, failed int, err error)` is added (for `bms check`). Records of what was built, not design changes.
- **Amended 2026-10-18 (D37; departments-events Proposed change 8, M2 follow-ups):** departments-events records through areas `departments` and `events`, all at level `internal`, `Subject` zero, actions `departments.department.created|changed|renamed|archived|restored`, `events.event.created|changed|closed|reopened`, `events.kind.created|changed`, `events.schedule-item.created|changed|deleted` and `events.setup.ran` (`Reason` `copy_from:<id>`), with `Record` kinds `departments.department`, `events.event`, `events.kind`, `events.schedule-item`. Two M2 follow-ups add entries: authz's `Seed` records `access.role.changed` when an upgrade adds a permission to a starter role, and people records `people.request.read` (`Level: personal`, `Fields: [note]`), one per returned request with a note that is not the actor's own, `Subject` the requester (D38 (u)); people's `people.consent.changed` entry carries the purpose in `Fields` and `granted:<purpose>` or `withdrawn:<purpose>` in `Reason` (D38 (s)).
- **Amended 2026-10-18 (D36 (e); approved by the owner 2026-10-18, D37):** `Record` never spools while ctx carries a transaction. It asks `db.TxMode(ctx)` first: inside a `Write` transaction the insert runs on the transaction and a refusal (`Unavailable` wrapping `db.ErrReadOnly`) is returned, so the caller's change fails with it and rolls back ("no audit, no action"; new F17b); inside a `Read` transaction it is `Internal` before any statement (F15, now a unit test); only a call with no transaction (a read, a login, a search) spools on a read-only copy (F17). Closes the three ways a spooled entry could outlive a rollback (Behavior, "Why never spool inside a transaction"). `audittest.Fake` refuses a `Record` inside a `Read` transaction the same way. Decisions 2, 4 and 12, the Recorder doc, F15, F17, F17b, the test plan and Proposed changes 15 and 16 updated. Writers (identity, people, authz) and the importer need no change.
- **Amended 2026-10-18 (M2 build, accepted deviations):** implementation deviations recorded in Behavior, "Accepted implementation deviations (M2 build, 2026-10-18)": decision 6 amended (the `ActorFunc` may return `ActorOperator`), and audit uses `perm.RoleAuditReviewer`. Records of what was built, not design changes.
- **Amended 2026-10-19 (D39, owner approval of settings):** audit exports `Settings []settings.Decl` in the leaf package `internal/core/audit/auditsettings` (`Settings`, `Area`, `MinRetention`, `MaxRetention`), not in `audit`: `settings` imports `audit` for its `Recorder`, so `audit` importing `settings` would be a cycle (the authz/perm precedent) (implementation, 2026-10-19) (`audit.retention`, `audit.self_view`); `audit.retention` is written in hours (`8760h`..`876000h`, `0` = forever) because the `Duration` kind is `time.ParseDuration`; `jobs` reads it through `Scoped("audit").Duration("retention")`. Proposed change 6 is applied. No behaviour changes.
- **Amended 2026-10-30 (D58; jobs J-W1 Proposed change 18):** the audit catalogue gains `module.audit.job.purge.title` (the Background work screen's title for the `audit.purge` job, jobs decision 24); see UI.
- **Amendment 2026-10-20 (approved, D44; jobs Q-JOBS6):** the `ActorFunc` may answer `ActorSystem` (no `Person`, no `Superuser`), which `app`'s function does inside a job (`jobs.InJob`). Without it every `Record` from a module's job handler fails `Internal` (`resolveActor` accepts only a person or the operator, pinned by `TestResolveActor_Order_GivenThenDeliveryThenActorFuncThenAnonymous`). Decision 6 is amended, not the order; the protection stays: an inconsistent actor from the function (`ActorSystem` or `ActorOperator` with a `Person` or `Superuser`, a person without `Person`, `ActorAnonymous`) is still `Internal` (new F13b); audit does not import `jobs` (`jobs` takes an audit `Recorder`: a cycle). Section "Amendment 2026-10-20" below.
- **Package:** `internal/core/audit` (service, types, spool, importer, `http.go`), `internal/core/audit/postgres` (store, `sql/`, `migrations/`), `internal/core/audit/audittest` (the fake recorder). **UI:** `web/src/core/audit/` (M2)
- **Depends on specs:** `model` (IDs, `Sensitivity`, `TimeRange`), `lib-errs`, `lib-core` (`clock`, `validate`), `db` (`DB`, `Transact`, `TxMode`, `ReadOnly`, `ErrReadOnly`, `Wrap`, `Set`), `telemetry` (`Tracer`, `Meter`, `TraceIDFrom`), `signals` (`InDelivery` only), `module-contract` (`Deps.Audit`, `ModuleRoutes`), `config` (`Instance.Name`, `Data.Dir`, through `app`), `api` (`httpx`, route mounting), `app` (part 2 → part 1, see Proposed change 1). Leaf types from `authz` (`Permission`, `PermissionDecl`) that `module-contract` already places there.
- **Used by:** `identity` (M1: logins, failed logins, sign-up, account disable, superuser creation), `people` (M2: reads of personal fields, export, deletion), `authz` (M2: grants, revocations, rules), `departments-events` (M3), `settings` (M2: changes), every module through `Deps.Audit`, `jobs` (M4: the retention purge), `app` (wiring and lifecycle), the web shell's audit screen (M2).

## Summary

**Purpose.** Keep an append-only record of who did what and who saw what, so that a regional can answer "who viewed this person's data" and "what did this account do" without trusting anyone's memory, on the writing copy and on a read-only copy alike.

**Exported names (package `audit`).**
- `Action` (`<area>.<ident>(.<ident>)*`, idents `[a-z][a-z0-9]*(-[a-z0-9]+)*`, at most 96 bytes; `Validate`, `Area`). `Outcome` (`Succeeded`=1, `Denied`, `Failed`; zero invalid; `String`, `ParseOutcome`, `Validate`). `ActorKind` (`ActorPerson`=1, `ActorAnonymous`, `ActorOperator`, `ActorSystem`; `String`). `SpoolCounts(dataDir) (waiting, failed int, err error)` (for `bms check`). `Actor{Kind, Person model.PersonID, Superuser bool, Titles []string}` (+`Validate`). `Ref{Kind string, ID [16]byte}` (a record reference, kind `<area>.<ident>`; `RefOf[T ~[16]byte](kind, id)`, `IsZero`, `Validate`).
- `Entry{Action, Outcome, Actor, Subject model.PersonID, Record Ref, Fields []string, Level model.Sensitivity, Reason string}` (+`Validate`): what a writer gives. `RecordID = model.ID[record]`; `Record{ID, At, Instance, TraceID, Spooled bool, Entry}`: what is stored and what `Search` returns (`Actor` fully resolved).
- `Recorder{Record(ctx, ...Entry) error}` = `module.Deps.Audit`. Each caller gets one scoped to its area from `(*Log).Scoped(area)`; an action outside the area fails `Invalid`.
- `ActorFunc func(ctx) (Actor, bool)` (wired by `app`: identity's session as a person, the operator while a command runs, the system inside a job (`jobs.InJob`; amendment 2026-10-20, approved D44); anything else from it is `Internal`, F13b); `TitleSource{TitlesOf(ctx, PersonID) ([]string, error)}` (authz, M2, optional); `PermissionChecker{Allow(ctx, authz.Permission) error}` (authz, M2, nil denies).
- `Options{DB, Clock, Logger, Tracer, Meter, Instance, DataDir, Actor, Titles, Permissions, ImportInterval}` (+`Validate`); `New(Options) (*Log, error)`. `*Log`: `Scoped(area) (Recorder, error)`, `Search(ctx, Query) (Page, error)`, `Export(ctx, PersonID, io.Writer) (int, error)`, `Run(ctx) error` (the spool importer loop, an `app` runnable), `ImportOnce(ctx) (Imported, error)`, `Purge(ctx, before time.Time) (int64, error)` (M4, the retention job), `CountAbout(ctx, PersonID) (int, error)` (M2, `people`'s deletion report), `Routes() []Route` / `OpenAPI() []byte` (`Route{Method, Pattern, Handler}`; `app` converts to `module.Route`), `Close(ctx) error` (idempotent).
- `Query{Actor, Subject PersonID; Record Ref; Area string; Action Action; Within model.TimeRange; SuperuserOnly bool; After Cursor; Limit int}`, `Cursor` (opaque: `at`,`id`), `Page{Records, Next Cursor}`, `Imported{Files, Entries, Duplicates, Truncated int; Failed []string}`.
- Constants: `MaxBatch=1000`, `MaxFields=64`, `MaxFieldBytes=64`, `MaxReasonBytes=256`, `MaxTitles=16`, `MaxTitleBytes=64`, `MaxPageSize=200`, `DefaultPageSize=50`, `SpoolDirName="audit/spool"`, `DefaultImportInterval=30s`, `SpoolVersion=1`.
- `PermissionSearch="audit.search"`, `PermissionExport="audit.export"` (both site, reach `sensitive`), `Permissions []authz.PermissionDecl`; `Migrations fs.FS`, `Set = db.Set{Name:"audit", Schema:"core"}`; actions of its own: `audit.searched`, `audit.exported`, `audit.purged`.
- `audittest.NewFake(area) *Fake` (a `Recorder` that validates like the real one, refuses a call inside a `db.Read` transaction with `Internal` like the real one (D36 (e)), keeps `Recorded() []Entry`, `Fail(err)`, safe for concurrent use).

**Key decisions.** 1 one row per (action, record), batched per call, atomic; 2 an entry for a change is written in the caller's `Write` transaction (same ctx) and commits or rolls back with it, never anywhere else: `Record` asks `db.TxMode(ctx)` and, inside a `Write` transaction, inserts on the transaction and returns whatever the server answers, `db.ErrReadOnly` included (D36 (e)); an entry for a read is a standalone statement after the data is loaded and before it is returned; 3 **no audit, no action**: a `Record` failure fails the caller's use case, also for reads (one explicit exception, owner 2026-10-18: identity's sign-out clears the cookie before it records `identity.logout`, so a failed write never keeps a person signed in; the response is still the error); 4 on a read-only copy (`db.ErrReadOnly`, or `ReadOnly()` true), and only when ctx carries no transaction, entries go to a local append-only JSONL spool under `<data.dir>/audit/spool/`, 0600 in 0700, fsync per entry; inside a transaction nothing is ever spooled (D36 (e)); any other database failure is returned, not spooled; 5 the importer (`Run`) inserts spool files when the copy can write, in batches of `MaxBatch`, idempotent on `id` (`ON CONFLICT DO NOTHING`), deletes a file once every complete line is in, renames an unparseable file, or one whose insert fails `Invalid`, `.failed` (owner, 2026-10-18; `Unavailable`, `Timeout` and `Canceled` keep it for retry), drops a truncated final line with a warning; 6 the actor comes from ctx through `ActorFunc` unless the caller sets one (login: the person who just authenticated; commands: `ActorOperator`); in a signal delivery (`signals.InDelivery`) the default is `ActorSystem`; inside a job `app`'s function answers `ActorSystem` and audit accepts it (amendment 2026-10-20, approved D44: audit cannot read `jobs.InJob` itself, `jobs` imports `audit`); 7 the actor's role or position titles at that moment and the fields viewed are stored from M1 (D22), titles empty until authz supplies `TitleSource`; 8 the superuser is marked on every entry (`actor_superuser`); 9 the table is append-only: `UPDATE`, `DELETE`, `TRUNCATE` are refused by a trigger and `UPDATE`/`TRUNCATE` revoked from the application role; the only deletion is `core.audit_purge(before)`, which sets the transaction flag `bms.audit_purge` the trigger honours; 10 entries keep their person IDs after a person is deleted (pseudonymous; legal and safety trail; `people` reports them as `Kept`) (owner, 2026-10-18); 11 every `Search` and `Export` is itself audited; 12 `Record` must not run inside a `db.Read` transaction: `Internal`, decided from `db.TxMode(ctx)` before any statement, on a writable and a read-only copy alike (D36 (e)); 13 regional differences are settings (`audit.retention`, floor 365 days when set, owner 2026-10-18; `audit.self_view`, default `off` until the Later `ViewsOf` feature ships, then `viewed`, owner 2026-10-18) and authz data (who is an audit reviewer); 14 audit never imports `core/module`: it exports its own `Route` type and `app` converts it (the shape every later core part with routes can reuse); 15 settled by the owner on 2026-10-18: the retention floor is 365 days when set (`0` stays forever), `audit.self_view` is `off` until `ViewsOf` ships and `viewed` after, and person IDs stay on audit rows after deletion (decision 10).

**Errs kinds callers handle.** `Record`: `Invalid` (bad entry, wrong area, empty or oversized batch), `Internal` (inside a `Read` transaction, after `Close` misuse of `Scoped`), `Unavailable` wrapping `db.ErrReadOnly` (inside a `Write` transaction on a read-only copy: the caller's change fails with it, the api answers `503 read_only`; D36 (e)), `Unavailable` (database down; or read-only copy, no transaction, and the spool cannot be written: disk full, unwritable data dir), `Timeout`/`Canceled`, the `TitleSource`'s kind; `Unavailable` after `Close`. `Search`/`Export`: `Unauthenticated`, `Permission`, `Invalid` (query, cursor, zero person), store kinds, `Canceled` when the export's writer fails; a search on a read-only copy works (its own entry is spooled). `ImportOnce`: `Unavailable` wrapping `db.ErrReadOnly` (nothing imported), `Unavailable` (dir or file unreadable, insert failed; retried next tick), `Invalid` (a `.failed` file, after the others imported). `Purge`: `Invalid`, `Unavailable` wrapping `ErrReadOnly`. `New`/`Options.Validate`: `Invalid` with fields `audit.db`, `audit.clock`, `audit.logger`, `audit.tracer`, `audit.actor`, `instance.name`, `data.dir`, `audit.import_interval`. No exported sentinels.

**Owns.** Table `core.audit_entry` (append-only, no `updated_at`), function `core.audit_purge`, trigger `audit_entry_guard`; the spool files; permissions `audit.search`, `audit.export`; settings `audit.retention` (duration, default 0 = forever, D30; otherwise `8760h` (365 days) to `876000h` (100 years)) exported as `auditsettings.Settings []settings.Decl` (D39; leaf package `internal/core/audit/auditsettings`, with `Area`, `MinRetention`, `MaxRetention` (implementation, 2026-10-19)) and `audit.self_view` (`off|viewed|fields|titles`, D22, Later; default `off` until the feature ships, then `viewed`); route set `audit`: `GET /api/audit/entries`, `GET /api/audit/people/{person}/views` (JSON Lines); metrics `bms.audit.recorded{via}`, `bms.audit.record_failures{kind}`, `bms.audit.imported{outcome}`, `bms.audit.spool_files`; spans `audit.record`, `audit.search`, `audit.export`, `audit.import`, `audit.purge`. No signals.

**Expects of dependencies.** `db`: `Transact` ctx-carried transactions, `TxMode(ctx)` reporting the carried transaction's mode for a `*Pool` and a `dbtest.Fake` alike (D36 (e)), `ReadOnly()`, `ErrReadOnly` on a refused write, `Wrap`, a `25006` in a `Read` transaction classified `Internal` (a backstop behind `TxMode`), `Set`, `dbtest.Fake.SetReadOnly` (also mid-transaction, to stage the flip). `telemetry`: `TraceIDFrom(ctx)`, typed attributes, `telemetrytest`. `signals`: `InDelivery(ctx)`. `config` through `app`: `Instance.Name`, `Data.Dir`. `identity`: an exported ctx accessor `app` adapts to `ActorFunc`; sets `Actor` explicitly on login. `authz` (M2): `TitleSource`, `PermissionChecker`; `app` registers `audit.Permissions` in `corePerms`. `app`: builds audit **before** identity (Proposed change 1), wires an `ActorFunc` that answers the system inside a job before it looks for a session (amendment 2026-10-20), scopes `Deps.Audit` per module, runs `Run` as a runnable and `Close` as a closer after `Set.Stop`, before `db.Close`. `settings` (M2): the two keys above. `jobs` (M4): the retention job calls `Purge`.

**Open questions:** None. `audittest.Fake` refuses a `Record` inside a `Read` transaction (F45; owner, 2026-10-18, D37); the three earlier ones are settled as decisions 10, 13 and 15 (owner, 2026-10-18). Identity's `identity.logout` is the one stated exception to decision 3 (decision 3).

**M2 build deviations (2026-10-18).** Decision 6 amended: the `ActorFunc` may return `ActorOperator` (`app` returns it while a command runs with no session); the order stays given, delivery, `ActorFunc`, anonymous. audit uses `perm.RoleAuditReviewer`.

**Amended 2026-10-28 (D52(e)).** `Search` sets `plan_cache_mode = force_custom_plan` inside its `Read` transaction with `SELECT set_config('plan_cache_mode','force_custom_plan', true)` (transaction-local, the form `SET LOCAL` takes, so it works through `Query`).

## Purpose

Keep an append-only record of who did what and who saw what.

## Responsibilities

- The `Entry` vocabulary every writer shares: action, outcome, actor, subject, record, fields, level, reason; its validation.
- Stamping what the writer must not be trusted to supply: the entry ID, the time, which copy (`Instance`), the trace ID, the resolved actor (person, superuser mark, titles).
- Writing entries to `core.audit_entry`, in the caller's transaction when there is one.
- On a read-only copy, writing entries to a local append-only spool, and importing the spool when the copy can write again, exactly once per entry.
- Keeping the table append-only at the schema level (trigger, revoked privileges), with one sanctioned deletion path for retention.
- Searching the log and exporting the views of one person's data, for people holding the audit permissions, and auditing those reads too.
- Declaring the permissions and settings that belong to the log.

## Not responsible for

| Concern | Owner |
|---|---|
| Deciding whether a read or change is allowed | `authz` (`Authorizer`); each service checks before it acts |
| Deciding whether a read must be audited | the reading service, with `model.Sensitivity.ReadIsAudited(self)` and its own Data section |
| Knowing who is signed in | `identity`; `app` adapts its ctx accessor to `ActorFunc` |
| The actor's role or position titles | `authz` (M2, `TitleSource`); `volunteers` later, through authz |
| Running the retention job on a schedule | `jobs` (M4); this spec supplies `Purge` and the setting |
| Storing the setting values | `settings` (M2); this spec declares the keys and defaults |
| Showing a person that their data was viewed (D22, Later) | a later amendment of this spec adds `ViewsOf`; `people`'s person page shows it |
| Telemetry: logs, traces, metrics, error references | `telemetry`; audit records the trace ID, never log lines |
| Alerting maintainers on a superuser login | `identity` with `notify` (M4); audit only marks the entries |
| Export and deletion of a person's data across modules | `people`; audit entries are kept (decision 10) and counted as `Kept` |
| Protecting backups and the data dir on disk | the operator (`docs/operating.md`, Proposed change 12) |
| Analytics about people | nobody; out of scope by principle |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| **Recorder** | Validate an entry, stamp it, and write it where this copy can write. | `Scoped(area)` per caller; the only write API |
| **Spool** | Hold entries durably on local disk while this copy cannot write to the database. | JSON Lines, append-only, fsync per entry, one active file per process |
| **Importer** | Move spooled entries into the table exactly once, when the copy can write. | `Run` loop and `ImportOnce`; an `app` runnable |
| **Reader** | Answer an audit reviewer's search and export, auditing the read. | `Search`, `Export`; routes and the M2 screen |
| **Guard** | Make the table append-only in the schema itself. | trigger, `REVOKE`, `core.audit_purge` |

## Interface

```go
package audit // internal/core/audit
//
// Imports: model; lib/errs, lib/clock, lib/validate; core/db; core/telemetry;
// core/signals (InDelivery); core/authz (Permission, PermissionDecl: leaf types).
// Never identity, module, api, config or a module (identity imports audit, so audit must
// not import identity).

// Limits. Bytes, not characters, to match the CHECK constraints.
const (
	MaxBatch              = 1000             // entries per Record call
	MaxFields             = 64               // field names per entry
	MaxFieldBytes         = 64               // one field name
	MaxReasonBytes        = 256              // Entry.Reason
	MaxTitles             = 16               // Actor.Titles
	MaxTitleBytes         = 64               // one title
	MaxActionBytes        = 96               // Action
	MaxPageSize           = 200              // Query.Limit
	DefaultPageSize       = 50               // Query.Limit when 0
	SpoolDirName          = "audit/spool"    // under Options.DataDir
	SpoolVersion          = 1                // "v" on every spool line
	DefaultImportInterval = 30 * time.Second // Options.ImportInterval when 0
)

// Action names what happened: "<area>.<ident>(.<ident>)*", where area is the module or
// core area that records it and each ident is "[a-z][a-z0-9]*(-[a-z0-9]+)*"; at most
// MaxActionBytes. Convention (not enforced): past tense for changes
// ("identity.account.disabled"), "read" for views ("people.profile.read"), and a
// distinct action per failure the reviewer cares about ("identity.login-failed").
type Action string

// Validate fails errs.Invalid (field "action") on bad grammar; never echoes the value.
func (a Action) Validate() error
// Area returns the text before the first dot ("" if none). Cannot fail.
func (a Action) Area() string

// Outcome says how the action ended. The zero value is invalid.
type Outcome uint8

const (
	Succeeded Outcome = iota + 1 // the action happened
	Denied                       // refused by authorization or policy (a failed login counts as Failed)
	Failed                       // attempted and did not happen for another reason
)

func (o Outcome) String() string          // "succeeded", "denied", "failed", "Outcome(<n>)"
func (o Outcome) Validate() error         // errs.Invalid unless one of the three
func ParseOutcome(s string) (Outcome, error) // exact lower-case names; errs.Invalid

// ActorKind says what kind of actor acted. The zero value means "resolve from ctx".
type ActorKind uint8

const (
	ActorPerson    ActorKind = iota + 1 // a signed-in person (Person set)
	ActorAnonymous                      // a request with no session (failed login, public route)
	ActorOperator                       // a bms command run on the host (create-superuser, export, delete)
	ActorSystem                         // a signal handler or a job
)

// Actor is who acted. A writer sets Kind, Person and Superuser only when it knows better
// than the request context (login; commands); it never sets Titles (the recorder does,
// through TitleSource; a caller-set Titles fails Invalid).
type Actor struct {
	Kind      ActorKind
	Person    model.PersonID // required iff Kind == ActorPerson
	Superuser bool           // only with ActorPerson: the break-glass account (D23)
	Titles    []string       // role or position titles at that moment (D22); set by the recorder
}

// Validate fails errs.Invalid: Kind unknown; Person set without ActorPerson or missing with
// it; Superuser without ActorPerson; Titles longer than MaxTitles or an item empty or over
// MaxTitleBytes. The zero Actor is valid (meaning "resolve").
func (a Actor) Validate() error

// Ref points at the record acted on: Kind is "<area>.<ident>" (the owning spec's table or
// type name, for example "identity.account", "people.person"); ID the record's UUID. The
// zero Ref means "no record" (a failed login for an unknown address, a search).
type Ref struct {
	Kind string
	ID   [16]byte
}

// RefOf builds a Ref from any model.ID. Cannot fail; validation happens in Entry.Validate.
func RefOf[T ~[16]byte](kind string, id T) Ref
func (r Ref) IsZero() bool    // Kind == "" && ID == zero
func (r Ref) Validate() error // errs.Invalid: Kind without ID, ID without Kind, bad Kind grammar

// Entry is what a writer records. Names, IDs and levels only: never a value of a personal
// field (a legal name, an e-mail address) anywhere in it, Reason included.
type Entry struct {
	Action  Action
	Outcome Outcome
	Actor   Actor             // zero: resolved from ctx (decision 6)
	Subject model.PersonID    // the person the record is about; zero when none
	Record  Ref               // the record acted on; zero when none
	Fields  []string          // field names viewed or changed, as the owning spec names them ("legal_name"); "[a-z][a-z0-9_]*", unique, at most MaxFields
	Level   model.Sensitivity // the highest level among Fields; required iff Fields is non-empty, zero otherwise
	Reason  string            // what the person typed when a workflow asks why (break-glass, emergency lookup); at most MaxReasonBytes; valid UTF-8, no control characters
}

// Validate fails errs.Invalid joining a validate.FieldError per problem (fields "action",
// "outcome", "actor.*", "subject", "record", "fields", "level", "reason"). It does not
// check the area against a scope: Recorder.Record does.
func (e Entry) Validate() error

type record struct{}
// RecordID identifies one stored entry.
type RecordID = model.ID[record]

// Record is one stored entry: the Entry as written plus what the recorder stamped.
type Record struct {
	ID       RecordID
	At       time.Time // when it was recorded (the recording copy's clock)
	Instance string    // which copy recorded it (config instance.name)
	TraceID  string    // 32 lower-case hex, or "" when there was no span
	Spooled  bool      // came through a spool (At is the original time)
	Entry              // Actor fully resolved (Kind never zero; Titles filled when known)
}

// Recorder is what writers hold: module.Deps.Audit, and the value each core part receives.
// Every implementation is scoped to one area.
type Recorder interface {
	// Record writes every entry or none. Each entry's Action must belong to the recorder's
	// area (F9). Entries for a change are recorded inside the caller's Write transaction
	// (the ctx carries it; db.TxMode reports it) and commit or roll back with it: on a
	// read-only copy the insert is refused like the caller's own write and the error,
	// errs.Unavailable wrapping db.ErrReadOnly, is returned, never spooled (F17b).
	// Entries for a read are recorded outside any transaction, after the data is loaded
	// and before it is returned (decision 2); on a read-only copy those go to the spool
	// (decision 4, F17). Inside a Read transaction Record fails errs.Internal before any
	// statement, on either copy (decision 12, F15). A failure means the caller's use case
	// must fail (decision 3). Fails errs.Invalid (F7-F14), errs.Internal (F15, F21),
	// errs.Unavailable (F16, F17b, F18, F22), errs.Timeout/Canceled (F20), or the
	// TitleSource's kind (F19). Safe for concurrent use.
	Record(ctx context.Context, entries ...Entry) error
}

// ActorFunc reads the actor from ctx. identity owns the session; app wraps its ctx
// accessor: Kind ActorPerson with Person and Superuser set, ok true; ActorOperator while a
// bms command runs with no session; ActorSystem inside a job (jobs.InJob; amendment
// 2026-10-20), checked before the session; or ok false when none applies. Any other answer
// (ActorAnonymous, a person without Person, the operator or the system with a Person or
// Superuser) is a wiring bug and makes Record fail Internal (F13b). Titles it sets are
// dropped. Must not do I/O.
type ActorFunc func(ctx context.Context) (Actor, bool)

// TitleSource returns the role or position titles person holds now, for D22. authz
// implements it in M2; nil in M1 (titles stay empty). Returns nil, nil for a person with
// no titles. Fails with the store's kind.
type TitleSource interface {
	TitlesOf(ctx context.Context, person model.PersonID) ([]string, error)
}

// PermissionChecker decides one site-wide permission for the actor in ctx: the same shape
// as api.PermissionChecker. authz implements it (M2); nil denies everything with
// errs.Permission, so Search and Export fail closed in M1.
type PermissionChecker interface {
	Allow(ctx context.Context, p authz.Permission) error
}

// Options builds the log. app copies config.Instance.Name and config.Data.Dir in.
type Options struct {
	DB             db.DB
	Clock          clock.Clock
	Logger         *slog.Logger
	Tracer         telemetry.Tracer
	Meter          telemetry.Meter   // optional: nil records no metrics
	Instance       string            // config instance.name grammar: 1..64, [a-z0-9]([a-z0-9-]*[a-z0-9])?
	DataDir        string            // the spool lives in filepath.Join(DataDir, SpoolDirName)
	Actor          ActorFunc         // required
	Titles         TitleSource       // optional (M1: nil)
	Permissions    PermissionChecker // optional (M1: nil, deny)
	ImportInterval time.Duration     // 0 = DefaultImportInterval; else 1s..10m
}

// Validate fails errs.Invalid joining a validate.FieldError per problem: "audit.db",
// "audit.clock", "audit.logger", "audit.tracer", "audit.actor" (nil), "instance.name",
// "data.dir" (empty or NUL), "audit.import_interval".
func (o Options) Validate() error

// New validates Options and returns the log. No I/O: the spool directory is created by
// Run or by the first spool write, so New succeeds against db.None() (app decision 7) and
// on a read-only copy. Fails errs.Invalid (F1-F4).
func New(o Options) (*Log, error)

// Log is the audit log of this process. Safe for concurrent use.
type Log struct{ /* unexported */ }

// Scoped returns a Recorder whose entries must carry actions in area. app calls it once per
// module in provision (area = Manifest.Name) and once per core part (area = the part's
// name, which module.IsCoreArea accepts: "identity", "people", "access", "settings", ...).
// Fails errs.Invalid for an area outside "[a-z][a-z0-9]{1,31}" (F5); errs.Internal after
// Close (F6).
func (l *Log) Scoped(area string) (Recorder, error)

// Query selects entries. Every field is optional; empty means "any". Results are ordered
// newest first (At desc, ID desc) and paged by keyset.
type Query struct {
	Actor         model.PersonID
	Subject       model.PersonID
	Record        Ref
	Area          string          // Action.Area() == Area
	Action        Action          // exact action
	Within        model.TimeRange // [Start, End); zero means any time
	SuperuserOnly bool
	After         Cursor          // Page.Next of the previous page
	Limit         int             // 0 = DefaultPageSize; at most MaxPageSize
}

// Validate fails errs.Invalid with field errors ("record", "area", "action", "within",
// "after", "limit").
func (q Query) Validate() error

// Cursor is an opaque, URL-safe continuation token (At and ID of the last record).
type Cursor string

// Page is one page of results. Next is "" on the last page.
type Page struct {
	Records []Record
	Next    Cursor
}

// Search returns one page matching q. Needs an actor (errs.Unauthenticated, F23) holding
// audit.search (errs.Permission, F24). Records the entry {audit.searched, Succeeded,
// Subject: q.Subject, Record: q.Record} before returning (F27). Works on a read-only copy.
// Fails errs.Invalid (F25) or the store's kind (F26).
func (l *Log) Search(ctx context.Context, q Query) (Page, error)

// Export writes every entry whose Subject is person, oldest first, as JSON Lines
// (application/x-ndjson, one Record per line) to w, and returns how many. Needs an actor
// holding audit.export (F29). Records {audit.exported, Succeeded, Subject: person} before
// the first line is written (F31; a failed export is still on record). Fails errs.Invalid
// for a zero person (F28), errs.Canceled wrapping the write error when w fails (F30), or
// the store's kind.
func (l *Log) Export(ctx context.Context, person model.PersonID, w io.Writer) (int, error)

// Run is the importer loop, an app runnable: it creates the spool directory (0700; failing
// errs.Invalid under "data.dir", F33), imports once, then every ImportInterval while the
// copy can write. It returns nil when ctx is done (F38); errs.Internal after Close. Only
// one Run per Log (a second is errs.Conflict, F39).
func (l *Log) Run(ctx context.Context) error

// Imported says what one import did. Failed holds file names (never contents) renamed
// ".failed" for the operator.
type Imported struct {
	Files      int
	Entries    int
	Duplicates int // lines whose ID was already in the table (a re-run after a crash)
	Truncated  int // final lines without a newline, dropped
	Failed     []string
}

// ImportOnce rotates the active spool file, then imports every complete file in the spool
// directory, in name (time) order, each in batches of MaxBatch per transaction with
// ON CONFLICT (id) DO NOTHING, deleting a file once all its lines are in. Serialized: a
// concurrent call waits. Returns errs.Unavailable wrapping db.ErrReadOnly without touching
// a file when the copy cannot write (F32); errs.Unavailable when the directory or a file
// cannot be read or an insert fails Unavailable, Timeout or Canceled (the file is kept, F36);
// errs.Invalid, after the other files are done, when a file had an unparseable line (F34)
// or an insert failed Invalid (a CHECK violation: the file is renamed .failed, F36b). A missing directory is not an
// error (nothing to import).
func (l *Log) ImportOnce(ctx context.Context) (Imported, error)

// Purge deletes entries with At < before through core.audit_purge, in one transaction that
// sets the guard flag, and records {audit.purged, Succeeded, Actor: ActorSystem} in the
// same transaction. The retention job (jobs.md, M4) is its consumer; built with it. Fails
// errs.Invalid when before is zero or in the future (F40); errs.Unavailable wrapping
// db.ErrReadOnly on a read-only copy (F41); the store's kind otherwise.
func (l *Log) Purge(ctx context.Context, before time.Time) (int64, error)

// Close stops Run, syncs and closes the active spool file, and refuses further Record
// calls (errs.Unavailable). Idempotent. app calls it after Set.Stop and before db.Close.
// Fails errs.Timeout/Canceled when ctx ends first (the file stays importable, F42).
func (l *Log) Close(ctx context.Context) error

// Permissions this package declares; app registers them in corePerms (authz, M2).
const (
	PermissionSearch authz.Permission = "audit.search" // search the log
	PermissionExport authz.Permission = "audit.export" // export the views of one person's data
)

var Permissions = []authz.PermissionDecl{
	{Name: PermissionSearch, Scope: authz.ScopeSite, Reaches: model.SensitivitySensitive},
	{Name: PermissionExport, Scope: authz.ScopeSite, Reaches: model.SensitivitySensitive},
}

// CountAbout returns how many entries name person as actor or subject. people calls it
// inside an already authorized deletion to report Kept{Kind: "audit.entry"} (M4); no
// permission of its own. Fails errs.Invalid for a zero person, or the store's kind.
func (l *Log) CountAbout(ctx context.Context, person model.PersonID) (int, error)

// Route is one HTTP route of the audit area, relative to /api/audit. audit cannot import
// core/module (module is the top of the core graph), so app converts each Route to a
// module.Route (Access signed-in) when it builds the "audit" route set (decision 14).
type Route struct {
	Method  string
	Pattern string
	Handler http.Handler
}

// Routes lists GET /entries and GET /people/{person}/views. Handlers are thin: parse the
// query, call Search or Export, httpx.Fail on error. Cannot fail.
func (l *Log) Routes() []Route
// OpenAPI returns the fragment for those routes. Cannot fail (embedded; a test proves it
// parses and names exactly the routes).
func (l *Log) OpenAPI() []byte

// Migrations is the embedded postgres/migrations; Set is what app hands db.Migrate.
var Migrations fs.FS
var Set = db.Set{Name: "audit", Schema: "core", Files: Migrations}
```

```go
package audittest // internal/core/audit/audittest

// Fake is a Recorder for tests: it validates entries like the real one (grammar, area,
// limits), refuses a call whose ctx carries a db.Read transaction with errs.Internal like
// the real one (decision 12; db.TxMode works under dbtest.Fake), keeps them in memory, and
// never touches a database or a file. It has no read-only state: a test that wants
// Record to fail inside a Write transaction uses Fail(err).
type Fake struct{ /* unexported */ }

func NewFake(area string) *Fake                                     // panics only on a bad area (test setup)
func (f *Fake) Record(ctx context.Context, entries ...Entry) error  // validates; Internal inside a Read transaction; appends; returns Fail's error if set
func (f *Fake) Recorded() []audit.Entry                             // a copy, in call order
func (f *Fake) Fail(err error)                                      // the next Record calls fail with err (nil clears)
```

**Consumers per method.**

| Method | Consumer | Pre-condition | Post-condition |
|---|---|---|---|
| `Scoped` | `app` (provision per module; each core part) | after `New`, before `Close` | a `Recorder` bound to one area |
| `Recorder.Record` | `identity` (login, login-failed, logout, sign-up, disable, enable, password change, superuser created and logged in), `people` (reads of personal fields, export, delete), `authz` (grant, revoke, rule changes), `settings` (changes), `departments-events`, every module | a valid entry in the recorder's area; for a change, inside the `Write` transaction; never inside a `Read` transaction | every entry is in the table (committed with the caller's transaction, if any) or, when there was no transaction, in the spool; or the error says why and the caller fails (inside a `Write` transaction on a read-only copy that error is `db.ErrReadOnly`, the same one the caller's own write would give) |
| `Search` | the audit screen (M2), `GET /api/audit/entries` | an actor with `audit.search` | a page, newest first, plus an `audit.searched` entry |
| `Export` | the audit screen's "export views" (M2), `GET /api/audit/people/{person}/views` | an actor with `audit.export` | JSON Lines on `w`, plus an `audit.exported` entry recorded first |
| `Run` | `app` (runnable) | `New` done; data dir exists or is creatable | spool imported whenever this copy can write |
| `ImportOnce` | `Run`; `bms check` may call it with a 5 s deadline to report the spool (optional, app's choice); tests | — | see `Imported` |
| `Purge` | the retention job (`jobs`, M4) | writable copy | entries older than `before` gone; one `audit.purged` entry |
| `Close` | `app` (closer) | — | no more writes; active file synced |
| `Routes`, `OpenAPI` | `app` builds the `audit` route set for `api` | — | mounted under `/api/audit/` |
| `Permissions`, `Set`, `Migrations` | `app` (`corePerms`, `coreSets`) | — | — |
| `audittest.Fake` | every module's and core part's unit tests; `moduletest.Conformance` (Proposed change 2) | — | — |

## Types

Defined here: `Action`, `Outcome`, `ActorKind`, `Actor`, `Ref`, `Entry`, `RecordID`, `Record`, `Query`, `Cursor`, `Page`, `Imported`, `Route`, `Options`. Uses `model.PersonID`, `model.Sensitivity`, `model.TimeRange`, `model.ID`; `authz.Permission`, `authz.PermissionDecl`; `db.Set`.

**Spool line.** One JSON object per line, UTF-8, `\n`-terminated, keys in this order: `v` (`SpoolVersion`), `id`, `at` (RFC 3339 with nanoseconds, UTC), `instance`, `trace_id` (omitted when empty), `action`, `outcome`, `actor` (`{kind, person?, superuser?, titles?}`), `subject?`, `record?` (`{kind, id}`), `fields?`, `level?`, `reason?`. The importer accepts `v` equal to `SpoolVersion` only; another version makes the file `.failed` (a downgrade after an upgrade is the operator's problem to notice). Never anything else: the line is a `Record`, which holds names and IDs only.

**File names.** `<unix-ms>-<pid>-<4 hex bytes>.jsonl` so files sort by time and two processes that wrongly share a data dir never clash. `.failed` is appended to a file the importer gave up on. A file the importer is reading has no marker: one process per data dir is an operator rule (Proposed change 12), and `ImportOnce` skips the file this process has open.

## Data

**Table `core.audit_entry`** (migration `0001_init.sql` of set `audit`, schema `core`). Append-only: exempt from `updated_at` (postgres-sql skill). No foreign keys: an actor or subject may have been deleted (decision 10) or imported from another copy before its person row arrived.

| Column | Type | Sensitivity | Notes |
|---|---|---|---|
| `id` | `uuid` PK | none | `RecordID` |
| `at` | `timestamptz NOT NULL` | internal | recording time |
| `created_at` | `timestamptz NOT NULL DEFAULT now()` | internal | insert time; differs from `at` for spooled rows |
| `instance` | `text NOT NULL` CHECK grammar | internal | which copy |
| `trace_id` | `text` CHECK 32 hex | internal | links to telemetry (30-day retention there) |
| `spooled` | `boolean NOT NULL DEFAULT false` | internal | |
| `action` | `text NOT NULL` CHECK grammar | internal | |
| `outcome` | `text NOT NULL` CHECK IN (`succeeded`,`denied`,`failed`) | internal | |
| `actor_kind` | `text NOT NULL` CHECK IN (`person`,`anonymous`,`operator`,`system`) | internal | |
| `actor_person_id` | `uuid` | **personal** | NOT NULL iff `actor_kind = 'person'` (CHECK) |
| `actor_superuser` | `boolean NOT NULL DEFAULT false` | internal | CHECK: only with `person` |
| `actor_titles` | `text[] NOT NULL DEFAULT '{}'` | internal | the role or position titles then (D22) |
| `subject_person_id` | `uuid` | **personal** | the person the record is about |
| `record_kind`, `record_id` | `text`, `uuid` | internal | both NULL or both set (CHECK) |
| `fields` | `text[] NOT NULL DEFAULT '{}'` | internal | names only |
| `level` | `text` CHECK IN (`public`,`internal`,`personal`,`sensitive`) | internal | NULL iff `fields = '{}'` (CHECK) |
| `reason` | `text` CHECK `octet_length <= 256` | **personal** | what the person typed; shown to audit reviewers only |

Indexes (keyset order `at DESC, id DESC`): `(subject_person_id, at DESC, id DESC) WHERE subject_person_id IS NOT NULL`; `(actor_person_id, at DESC, id DESC) WHERE actor_person_id IS NOT NULL`; `(record_kind, record_id, at DESC, id DESC) WHERE record_id IS NOT NULL`; `(at DESC, id DESC)`; `(at DESC) WHERE actor_superuser`.

Guard: `CREATE FUNCTION core.audit_entry_guard() RETURNS trigger` raising `audit entries are append-only` on `UPDATE` and `TRUNCATE` always, and on `DELETE` unless `current_setting('bms.audit_purge', true) = 'on'`; triggers `BEFORE UPDATE OR DELETE FOR EACH ROW` and `BEFORE TRUNCATE FOR EACH STATEMENT`. `REVOKE UPDATE, TRUNCATE ON core.audit_entry FROM CURRENT_USER` (the owner may revoke its own ordinary privileges; it is a guard rail against bugs, not against the operator, who can always re-grant: decision 9). `CREATE FUNCTION core.audit_purge(before timestamptz) RETURNS bigint` that runs `SET LOCAL bms.audit_purge = 'on'` then `DELETE ... WHERE at < before` and returns the count; the flag lives for that transaction only. Column comments carry the sensitivity levels above.

**Retention.** `audit.retention` (Settings), default 0 = keep forever (D30). The purge removes whole rows; nothing is anonymized because nothing in a row is a name.

**Deletion of a person** (`people`, M2): rows keep their person IDs (decision 10; owner, 2026-10-18); `people` reports `Kept{Kind: "audit.entry", Count}` for that person as actor or subject, by a count `Search`-side query this spec exposes to `people` as `CountAbout(ctx, person) (int, error)` once `people.md` names it (no permission of its own: `people` has already authorized the deletion). **Export of a person** (`people`, M2): the person's own audit rows are not part of their export in the MVP; D22's `ViewsOf` (Later) is the person-facing view.

**Spool files** (`<data.dir>/audit/spool/*.jsonl`): the same data as the table, so **personal**; 0600 in a 0700 directory; kept until imported, then deleted; a `.failed` file is kept until the operator removes it. The operator docs say the data dir on a read-only copy holds audit data and must be backed up and protected like the database (Proposed change 12).

## Permissions

| Operation | Permission | Scoped by | Default roles (authz starter data) |
|---|---|---|---|
| `Record` | none of its own: the caller's operation names its permission and checks it before acting (09 ground rules) | — | — |
| `Search`, `GET /api/audit/entries` | `audit.search` (site; reaches sensitive) | site | Audit reviewer; the superuser bypasses (D23) and is marked |
| `Export`, `GET /api/audit/people/{person}/views` | `audit.export` (site; reaches sensitive) | site | Audit reviewer |
| `ImportOnce`, `Run`, `Purge` | none: system work with no actor (`ActorSystem`) | — | — |
| `CountAbout` | none: called by `people` inside an already authorized deletion | — | — |

A person never sees entries about themself through these routes unless they hold the permission; the self view is D22's later feature with its own setting. Search results are not filtered per department: the permission is site-wide, which matches "audit reviewer" as one of the separated site admin jobs (D23). Department-scoped audit review is not designed; if a regional asks, `department_id` becomes a nullable column and `audit.search` a `ScopeDepartment` permission in a later migration.

## Signals

None published: an audit entry is a record of something that already has its own signal. None subscribed: a handler that must audit calls `Record` like any other code. `signals.InDelivery(ctx)` is read to default the actor to `ActorSystem`.

## Settings

Declared as `auditsettings.Settings []settings.Decl` (D39) in the leaf package `internal/core/audit/auditsettings` (`Settings`, `Area`, `MinRetention`, `MaxRetention`), not in `audit`: `settings` imports `audit` for its `Recorder`, so `audit` importing `settings` would be a cycle (the authz/perm precedent) (implementation, 2026-10-19), in the core settings area `audit`, through the declaration seam `settings.md` defines (`settings.Decl`, `Check` per key); `app` passes it to `settings.New`. `jobs` reads `audit.retention` through `Scoped("audit").Duration("retention")` and `Purge`s on the writing copy only. A reader that cannot parse a stored value gets the default (settings decision 5).

| Key | Type | Default | Valid range | Who can change it |
|---|---|---|---|---|
| `audit.retention` | Duration | `0` (keep forever, D30) | `0`, or `8760h` (365 days)..`876000h` (100 years), written in hours because the kind is `time.ParseDuration` (floor settled by the owner, 2026-10-18; hours, D39) | Settings admin; the change is audited by `settings` |
| `audit.self_view` | Enum | `off` until the Later `ViewsOf` feature ships, then `viewed` (owner, 2026-10-18) | `off`, `viewed`, `fields`, `titles` (D22, cumulative) | Settings admin; read by the Later `ViewsOf` feature only |

Validation errors are `errs.Invalid` field errors under the key, raised by `settings` from the declaration's `Check`.

## UI (M2)

`web/src/core/audit/`, registered like a module's folder (web-shell decision 3), menu entry "Audit log" shown only to holders of `audit.search` (the shell hides what the permissions list excludes; the server checks again).

- **Audit log** (`/audit`): filters (person as subject, actor, record kind and ID, area or action, date range, superuser only), a newest-first list of entries as cards (phone-first: action, time, actor title or "superuser", subject playa name resolved through `people`'s batch lookup when the viewer may see it, otherwise the ID), "Load more" by cursor. **Empty:** "No audit entries match these filters." **Error:** the shell's error banner with the reference. **Forbidden:** the route is hidden; a direct visit shows the shell's forbidden screen. **Read-only copy:** works; the page shows the shell's read-only banner.
- **Export views of a person** (a button on the list when a subject is chosen; the audit screen's subject filter accepts `?subject=<person>` from the URL, and `people`'s person page links holders of `audit.search` to `/audit?subject=<person>` instead of calling the route itself, so `web/src/core/people/` never imports this folder): downloads `GET /api/audit/people/{person}/views` as `views-<person>.jsonl`. The screen says the export is recorded.
- The person's own "who viewed my data" (D22) is **Later**, not in this spec's UI.

Strings are catalogue keys under `core.audit.*` in `web/src/core/audit/messages/en.json`, plus one derived key, `module.audit.job.purge.title` ("Delete audit entries past their retention", D58): the `audit.purge` job is registered by `app`, and the Background work screen (jobs.md W1.4) looks its title up in the owner's catalogue, which for audit is this file.

## Behavior

**Recording (decision 1-3, 6, 12).** `Record` validates every entry first (`Entry.Validate`, then the area check), so a bad batch writes nothing. For each entry it stamps `ID` (`model.NewID`), `At` (`Clock.Now()`, one instant for the batch), `Instance`, `TraceID` (`telemetry.TraceIDFrom(ctx)`, `""` when none). The actor: if `Entry.Actor.Kind` is zero, then `signals.InDelivery(ctx)` gives `ActorSystem`, else `Options.Actor(ctx)` gives the person, the operator or the system (or `ActorAnonymous` when `ok` is false; any other answer is `Internal`, F13b); if `Kind` is set, it is taken as given. For every `ActorPerson`, `Titles` is filled from `TitleSource.TitlesOf` once per distinct person per call (nil source: empty). Then `db.TxMode(ctx)` decides where the batch goes (D36 (e)):

- **A `Read` transaction:** `Internal` at once, with no statement and no spool line, on a writable and a read-only copy alike (decision 12, F15). The caller has a bug.
- **A `Write` transaction:** one multi-row `INSERT` on the transaction. `DB.ReadOnly()` is not consulted: the transaction is open on one connection and the server's answer for that connection is the truth, for this insert and for the caller's own writes. Any failure is returned as it is, `Unavailable` wrapping `db.ErrReadOnly` included (F17b), and nothing is spooled: the caller's `Transact` rolls back, and the api answers `503 read_only`, which is what the caller's own write would have produced.
- **No transaction:** `DB.ReadOnly()` true before the attempt, or a result of `Unavailable` wrapping `db.ErrReadOnly`, sends the whole batch to the spool with `Spooled` unset in the line (the importer sets the column) (decision 4, F17); otherwise one multi-row `INSERT` as its own statement, and any other failure is returned with nothing spooled (F16).

Entries are never retried by audit; the caller's retry of its use case records again only if it acts again, and a rolled-back transaction takes its entries with it.

**Why never spool inside a transaction (D36 (e)).** Decision 2 promises that an entry for a change commits or rolls back with the change. A spool line cannot roll back, so writing one while a transaction is open breaks the promise in three ways the M1 review found: (a) the cached read-only flag flips mid-transaction, the entry is spooled outside the transaction, the transaction then rolls back, and the spool keeps "X succeeded" for an X that never happened; (b) the flag is stale, `Record` runs before the caller's write, the insert gets `25006`, the entry is spooled, the caller's write fails, and the importer later inserts the phantom; (c) a `Record` inside a `Read` transaction on a read-only copy is spooled quietly instead of failing as the bug it is. `db.TxMode` answers the one question all three share, before the insert: is a transaction in flight, and which kind. Inside a `Write` transaction the right behaviour is the caller's own: fail with `db.ErrReadOnly` and let the transaction roll back ("no audit, no action", decision 3). The spool keeps its one meaning, "this copy could read but not write", and holds only entries for things that did happen: reads, logins and searches, which need no transaction.

**Effect on writers.** Nothing changes for identity, people or authz. Each records a change inside its `Write` transaction after its own write (identity's `SignUp`, `manage`, password and superuser paths; people's and authz's writes as their specs say); on a read-only copy `Transact(Write)` already fails fast with `db.ErrReadOnly` when the cached flag is current, and the first write gets `25006` when it is stale, so `Record` is never reached. In the one ordering this amendment changes, a `Record` that runs before the caller's write inside a `Write` transaction on a read-only copy, the caller now gets `db.ErrReadOnly` from `Record` instead of from its own write: the same kind, the same `503 read_only`, and no phantom line. Identity's login, `people`'s and the modules' audited reads, and the log's own `audit.searched` and `audit.exported` run outside any transaction and still spool. **Effect on the importer:** none; it inserts spool lines through the store, not through `Record`, and `TxMode` is not consulted there. It only sees fewer lines, none of them phantoms.

**Why not spool on every database failure.** A read needs the database too; if it is unreachable there is nothing to audit. A login is the one read that could succeed without the database (a signed token) and it is audited only on the copy that verified the password, which needed the database. Spooling only on read-only keeps the spool meaning one thing: "this copy could read but not write".

**Spool (decision 4).** One active file per process, opened lazily with `O_WRONLY|O_APPEND|O_CREATE|O_EXCL`, 0600, directory 0700 created on demand (`Run` creates it at start so the failure is seen early). Writes are serialized by a mutex; each `Record` writes its lines in one `Write` call followed by `Sync`; a short write or a sync error fails the call (F18) and the next call reopens a new file (the partial tail of the old one is the truncated-line case the importer handles). The active file is rotated (closed, so the next write opens a new one) when `ImportOnce` runs and at `Close`.

**Importer (decision 5).** `Run`: create the directory; `ImportOnce`; then on each tick (`Options.Clock.NewTicker(ImportInterval)`): if `DB.ReadOnly()` skip (debug log once per transition), else `ImportOnce`. `ImportOnce`: rotate; list `*.jsonl` sorted by name; for each file not open by this process: read line by line; a complete line is parsed and validated (`Entry.Validate` plus the stamped fields); every `MaxBatch` lines are inserted in one `Write` transaction with `ON CONFLICT (id) DO NOTHING` and `spooled = true`; when the file ends, a final line without `\n` is counted `Truncated` and dropped (it was never confirmed to the caller); the file is deleted with `Remove` after its last batch commits. An unparseable complete line stops that file: the lines before it stayed inserted (re-running is safe), the file is renamed `<name>.failed`, the name goes in `Imported.Failed`, and the next file proceeds. An insert failure of kind `Invalid` (for example a CHECK violation) is treated like an unparseable line: the lines before it stay inserted, the file is renamed `<name>.failed`, its name goes in `Imported.Failed`, a warning is logged and the next file proceeds (owner, 2026-10-18). An insert failure of kind `Unavailable`, `Timeout` or `Canceled` stops the whole run with that kind and keeps the file (next tick retries; duplicates are dropped by the conflict clause). `Run` logs one info per import with counts (`files`, `entries`, `duplicates`, `truncated`, `failed`), one warn per `.failed` file, and counts `bms.audit.imported{outcome=ok|duplicate|truncated|failed}`; the gauge `bms.audit.spool_files` observes the directory (0 when it does not exist).

**Search (decision 11).** Authorize (actor, then `audit.search`), validate, record `audit.searched`, then query with keyset pagination on `(at, id)`; `Limit` 0 means `DefaultPageSize`; `Next` is set when `Limit + 1` rows came back. Export streams in batches of `MaxPageSize`, oldest first, `audit.exported` recorded before the first row is read; a writer failure stops the stream with `Canceled`. Both run in a `Read` transaction for the query only; the search sets `plan_cache_mode = force_custom_plan` with `SET LOCAL` as the transaction's first statement, so only that transaction is affected (D52(e)); their own `Record` runs before the transaction starts (decision 12).

**Purge.** One `Write` transaction: `SELECT core.audit_purge($1)`, then `Record` of `audit.purged` with `Actor{Kind: ActorSystem}` on the same ctx (so the count and the purge commit together; the entry's `Reason` is empty, the count is in the job's log line).

**Concurrency and lifecycle.** `*Log` and every `Recorder` are safe for concurrent use. `New` does no I/O. `Run` is the only goroutine; it exits on ctx done or `Close` and `Close` waits for it (`Close` deadline). `Close` is idempotent; after it `Record` fails `Unavailable` (late signal handlers or `Set.Stop` work that still tries to audit fails loudly rather than silently; `app` orders `Close` after `Set.Stop`). The spool file handle is closed in `Close` after `Sync`.

**Telemetry.** Spans `audit.record` (`Int("entries")`, `Token("via", "db"|"spool")`, `Token("area")`), `audit.search`, `audit.export` (`Int("records")`), `audit.import` (`Int("files")`, `Int("entries")`), `audit.purge` (`Int("deleted")`). Metrics `bms.audit.recorded{via}`, `bms.audit.record_failures{kind}`, `bms.audit.imported{outcome}`, `bms.audit.spool_files` (gauge). Never a field name, reason, title, or person ID on a span or metric. Logs: warn when recording falls back to the spool, once per active file ("audit spooling started", `file` name only); info per import; warn per `.failed` file; error for a spool write failure (the caller also fails: the one place the error is logged is here, since the caller only returns it).

**Delivery by milestone.** M1: everything except the UI; routes are mounted and answer 403 (nil `PermissionChecker`), as the hello module's do in M0. M2: the UI, `TitleSource` and `PermissionChecker` wired from authz, the two settings. `CountAbout` for `people`'s deletion also ships in M2. M4: `Purge`'s caller (the retention job). Later: `ViewsOf` (D22) by amendment.

### Accepted implementation deviations (M1 build, 2026-10-18)

- The trace ID is read from the caller's ctx before the `audit.record` span starts.
- The `record_failures` metric label is `kind`.
- `Run` creates its ticker after the first import.
- `Imported.Entries` comes from RowsAffected; `Duplicates` = batch size - `Entries`.
- Handlers validate the query first, so a bad query records nothing. Invalid fields are named `query.<param>`; a repeated param is Invalid; `from` and `to` must come together.
- An `Export` failure after lines are written aborts the response with `http.ErrAbortHandler`.
- The `Ref` kind grammar is `<area>.<ident>(.<ident>)*`, no wider than the SQL CHECK.
- `TitleSource` titles are cleaned, not failed: control (Cc) and format (Cf) characters and U+FFFD are stripped, then the title is trimmed. Empty titles are dropped, at most 16 are kept, and each is capped at 64 bytes.
- `Close` with an expired ctx still closes the spool handle.
- `ActorKind.String` is exported.
- `audittest.Fake` re-implements the batch validation; `TestFake_MatchesRealValidation` keeps the two in step.
- The spool truncates back to the last good batch when a `Write` or `Sync` fails.
- `Close` cancels a running import.
- A spool closed under its mutex refuses later appends (Unavailable).
- The import line cap is 64 KiB; a longer line sets the file aside as `.failed`.
- The set-aside warning names the file, the line and the problem (a field name), never a value.
- An existing spool directory is tightened to 0700.
- `audit.SpoolCounts(dataDir) (waiting, failed int, err error)` was added for `bms check`.
- SQL: `core.audit_purge` uses `set_config('bms.audit_purge','on',true)` and resets it to `'off'`, instead of `SET LOCAL`. The triggers are `audit_entry_guard_row` and `audit_entry_guard_truncate`. `core.audit_purge` also refuses a future `before` (22023, Invalid). Search uses four query files (unkeyed, by actor, by subject, by record) and an expanded keyset predicate, for stable plans.

**M2 build (2026-10-18):**

- **Decision 6 amended.** The `ActorFunc` may return `ActorOperator` (`app` returns it while a command runs with no session). The order stays: the actor the caller gave, the delivery actor, `ActorFunc`, anonymous. It may also answer `ActorSystem` (no `Person`, no `Superuser`), as `app` does inside a job (`jobs.InJob`; amendment 2026-10-20, D44); `resolveActor` accepts a consistent person, operator or system and answers `Internal` for anything else (F13b).
- audit uses `perm.RoleAuditReviewer` (authz's leaf package, which breaks the audit<->authz import cycle).

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `New` | `DB`, `Clock`, `Logger`, `Tracer` or `Actor` nil | Invalid (`audit.*`) | fix wiring (app exits 2) | `TestNew_NilDependency_ReturnsInvalid` |
| F2 | `New` | `Instance` outside the grammar | Invalid (`instance.name`) | fix config | `TestNew_BadInstance_ReturnsInvalid` |
| F3 | `New` | `DataDir` empty or with NUL | Invalid (`data.dir`) | fix config | `TestNew_BadDataDir_ReturnsInvalid` |
| F4 | `New` | `ImportInterval` outside 1s..10m | Invalid (`audit.import_interval`) | fix wiring | `TestNew_BadImportInterval_ReturnsInvalid` |
| F5 | `Scoped` | area outside `[a-z][a-z0-9]{1,31}` | Invalid | fix wiring | `TestScoped_BadArea_ReturnsInvalid` |
| F6 | `Scoped` | after `Close` | Internal | fix ordering | `TestScoped_AfterClose_ReturnsInternal` |
| F7 | `Record` | no entries, or more than `MaxBatch` | Invalid (`entries`) | split the batch | `TestRecord_EmptyOrOversizedBatch_ReturnsInvalid` |
| F8 | `Record` | an action fails the grammar | Invalid (`entries[i].action`) | fix the constant | `TestRecord_BadAction_ReturnsInvalid` |
| F9 | `Record` | an action's area is not the recorder's | Invalid (`entries[i].action`) | use your own area | `TestRecord_ForeignArea_ReturnsInvalid` |
| F10 | `Record` | `Outcome` zero or unknown | Invalid (`entries[i].outcome`) | set it | `TestRecord_ZeroOutcome_ReturnsInvalid` |
| F11 | `Record` | `Fields` without `Level`, `Level` without `Fields`, a bad or duplicate field name, more than `MaxFields` | Invalid (`entries[i].fields` / `.level`) | fix the entry | `TestRecord_FieldsLevelMismatch_ReturnsInvalid` |
| F12 | `Record` | `Reason` over `MaxReasonBytes`, invalid UTF-8 or control characters | Invalid (`entries[i].reason`) | cap and clean input | `TestRecord_BadReason_ReturnsInvalid` |
| F13 | `Record` | `Actor` inconsistent (person without `Person`, `Person` with another kind, `Superuser` without person, caller-set `Titles`) | Invalid (`entries[i].actor.*`) | fix the entry | `TestRecord_InconsistentActor_ReturnsInvalid` |
| F14 | `Record` | `Record` half set or kind outside the grammar | Invalid (`entries[i].record`) | fix the entry | `TestRecord_HalfRef_ReturnsInvalid` |
| F15 | `Record` | ctx carries a `db.Read` transaction (`db.TxMode`), on either copy | Internal, before any statement; nothing spooled | record outside the read transaction | `TestRecord_InsideReadTx_ReturnsInternal` (unit: `dbtest.Fake`, `Calls()` shows no insert), `TestRecord_InsideReadTx_OnReadOnlyCopy_ReturnsInternal_NotSpooled` (unit: `SetReadOnly(true)`, spool dir stays empty), the existing integration `TestRecord_InsideReadTx_ReturnsInternal` kept as the end-to-end check against a real `Pool` (db's own tests prove the `25006` → `Internal` backstop) |
| F16 | `Record` | database unreachable or `db.None()` (not read-only) | Unavailable | fail the use case | `TestRecord_DBUnavailable_ReturnsUnavailable_NotSpooled` |
| F17 | `Record` | no transaction in ctx; copy read-only; spool written and synced | nil | continue; entry is in the spool | `TestRecord_ReadOnly_Spools`, `TestRecord_ReadOnlyBeforeAttempt_DoesNotTryTheDatabase` |
| F17b | `Record` | ctx carries a `db.Write` transaction and the insert is refused: the cached flag flipped after the transaction began (a), or was stale and the server answers `25006` (b) | Unavailable wrapping `db.ErrReadOnly`; nothing spooled; the caller's transaction rolls back | fail the use case (`503 read_only`), as its own write would | `TestRecord_InsideWriteTx_ReadOnly_ReturnsErrReadOnly_NotSpooled` (unit: `dbtest.Fake` writable at `Transact`, `SetReadOnly(true)` inside fn before `Record`; spool dir stays empty; `Calls()` shows the attempted insert with `InTx`), `TestRecord_InsideWriteTx_InsertRefused_ReturnsErrReadOnly_NotSpooled` (integration: a writable `TempDB`, `Transact(Write)`, `Exec("SET LOCAL transaction_read_only = on")` on the transaction's ctx, then `Record`; the table has no row after, the spool dir is empty) |
| F18 | `Record` | no transaction in ctx, copy read-only, and the spool directory or file cannot be created, written or synced (permission, disk full) | Unavailable | fail the use case (decision 3); operator fixes the disk | `TestRecord_ReadOnly_SpoolUnwritable_ReturnsUnavailable` |
| F19 | `Record` | `TitleSource` fails | its kind | fail the use case | `TestRecord_TitleSourceFails_ReturnsKind` |
| F20 | `Record` | ctx done | Canceled / Timeout | give up | `TestRecord_CtxDone_ReturnsCanceled` |
| F21 | `Record` | spool line cannot be encoded (impossible after `Validate`) | Internal | report a bug | `TestRecord_EncodeFailure_ReturnsInternal` (fault injection) |
| F22 | `Record` | after `Close` | Unavailable | fail the use case; fix shutdown order | `TestRecord_AfterClose_ReturnsUnavailable` |
| F23 | `Search` | no actor in ctx | Unauthenticated | 401 | `TestSearch_NoActor_ReturnsUnauthenticated` |
| F24 | `Search` | actor lacks `audit.search` (or nil checker) | Permission | 403 | `TestSearch_NoPermission_ReturnsPermission` |
| F25 | `Search` | bad `Limit`, `After`, `Within`, `Record`, `Area` or `Action` | Invalid (field) | 400 with field errors | `TestSearch_BadQuery_ReturnsInvalid` |
| F26 | `Search` | store fails | db's kind | map by kind | `TestSearch_StoreFails_ReturnsKind` |
| F27 | `Search` | its own `audit.searched` entry cannot be recorded | the `Record` error | fail the search (decision 3) | `TestSearch_RecordFails_ReturnsError` |
| F28 | `Export` | zero person | Invalid (`person`) | 400 | `TestExport_ZeroPerson_ReturnsInvalid` |
| F29 | `Export` | no actor / lacks `audit.export` | Unauthenticated / Permission | 401 / 403 | `TestExport_NoPermission_ReturnsPermission` |
| F30 | `Export` | `w.Write` fails mid-stream | Canceled (wrapping the write error) | nothing (client gone); the `audit.exported` entry stays | `TestExport_WriterFails_ReturnsCanceled` |
| F31 | `Export` | its own entry cannot be recorded | the `Record` error | fail before writing a line | `TestExport_RecordFails_WritesNothing` |
| F32 | `ImportOnce` | copy read-only | Unavailable wrapping `db.ErrReadOnly` | `Run` waits for the next tick | `TestImportOnce_ReadOnly_ReturnsErrReadOnly` |
| F33 | `Run` | spool directory cannot be created | Invalid (`data.dir`) | app stops `serve` (exit 2) | `TestRun_DirUncreatable_ReturnsInvalid` |
| F34 | `ImportOnce` | a complete line is unparseable, fails validation, or has another `v` | Invalid, after the other files | operator inspects the `.failed` file | `TestImportOnce_BadLine_RenamesFailed_ContinuesOthers` |
| F35 | `ImportOnce` | the final line has no newline | nil; `Truncated` counted, file deleted | nothing | `TestImportOnce_TruncatedTail_DroppedAndCounted` |
| F36 | `ImportOnce` | an insert fails Unavailable, Timeout or Canceled (database down mid-import) | that kind; file kept | `Run` retries next tick; duplicates dropped | `TestImportOnce_InsertFails_KeepsFile_RerunIdempotent` |
| F36b | `ImportOnce` | an insert fails Invalid (for example a CHECK violation) | Invalid, after the other files; the file is renamed `.failed`, warning logged | operator inspects the `.failed` file | `TestImportOnce_InsertInvalid_RenamesFailed_ContinuesOthers` |
| F37 | `ImportOnce` | directory or a file unreadable | Unavailable | operator fixes permissions | `TestImportOnce_Unreadable_ReturnsUnavailable` |
| F38 | `Run` | ctx done | nil | normal stop | `TestRun_CtxDone_ReturnsNil` |
| F39 | `Run` | called twice, or after `Close` | Conflict / Internal | fix wiring | `TestRun_Twice_ReturnsConflict` |
| F40 | `Purge` | `before` zero or in the future | Invalid (`before`) | fix the job | `TestPurge_FutureBefore_ReturnsInvalid` |
| F41 | `Purge` | copy read-only | Unavailable wrapping `db.ErrReadOnly` | job skips on a read-only copy | `TestPurge_ReadOnly_ReturnsErrReadOnly` |
| F42 | `Close` | ctx ends before the active file is synced | Timeout / Canceled | nothing: the file is importable | `TestClose_CtxDone_ReturnsTimeout_FileKept` |
| F43 | schema | `UPDATE`, `DELETE` or `TRUNCATE` on `core.audit_entry` outside `core.audit_purge` | the trigger raises; `db.Wrap` gives Internal | report a bug | `TestGuard_UpdateDeleteTruncate_Refused` (integration) |
| F44 | handlers | bad query string (`limit`, `after`, dates, UUIDs) | Invalid | 400 with field errors (`query.<name>`) | `TestEntriesHandler_BadQuery_Returns400` |

| F45 | `audittest.Fake.Record` | ctx carries a `db.Read` transaction (`db.TxMode`) | Internal; nothing recorded | fix the module: record outside the read transaction | `TestFake_InsideReadTx_ReturnsInternal` (a `dbtest.Fake` `Transact(Read)`; `Recorded()` stays empty; a `Write` transaction records normally) |

Methods that cannot fail: `Action.Area`, `Ref.IsZero`, `RefOf`, `Outcome.String`, `Routes`, `OpenAPI` (embedded; a test proves the fragment parses and names exactly the routes), `Fake.Recorded`, `Fake.Fail`.

## Lock-in check

No external technology beyond PostgreSQL 16 and the local filesystem, both already the project's baseline. The spool uses `os` only. Nothing here would change on another OSI-licensed store.

## Test plan

- **Unit** (no infrastructure): `dbtest.NewFake()` for the writable path (`Calls` shows one multi-row insert, `InTx` true when the caller's ctx carries a transaction) and `SetReadOnly(true)` for the spool path into `t.TempDir()`; the transaction rules through `db.TxMode` under the Fake: F15's two unit tests (a `Read` transaction, writable and read-only), F17b's unit test (`SetReadOnly(true)` after `Transact(Write)` began: `ErrReadOnly` back, no spool file, the Fake's `Exec` attempted on the transaction), and the existing `TestRecord_InsideWriteTx_JoinsTheCallersTransaction` and `TestRecord_SingleEntry_IsOwnStatementOutsideTx` for the two happy paths; `clock.NewFake` for `At` and the importer ticker (`BlockUntil`, `Advance`; no `time.Sleep`); `telemetrytest.New(t)` for logs, spans and metrics, asserting no field name, reason or seed marker leaks; a fake `ActorFunc`, `TitleSource` and `PermissionChecker` (the function's three accepted answers and F13b's refused ones, amendment 2026-10-20). Every F-row above has its named test. Spool: line format golden test (key order, `v`, omitted empties); `O_EXCL` naming; fsync called (an `os.File` seam or a counting wrapper); rotation on `ImportOnce` and `Close`; the mutex (`-race` with 32 concurrent `Record`s).
- **Importer unit:** a directory with three files (good, `.failed` case, truncated tail) against `dbtest.Fake`, asserting batches of `MaxBatch`, deletions, the rename, counts and the joined error.
- **Permission tests:** `Search` and `Export` with no actor, with an actor lacking the permission, with it; `audit.searched` and `audit.exported` recorded exactly once per call; superuser actor marked.
- **Integration** (`//go:build integration`, `dbtest.TempDB`): `db.Version(Migrations)`; migration applies; the guard (F43) for each statement and `core.audit_purge` succeeding with the flag; `Record` inside a `Write` transaction that rolls back leaves no row and no spool file; F17b's integration test (`SET LOCAL transaction_read_only = on` inside a `Write` transaction, then `Record`: `ErrReadOnly`, no row, no spool file; the pool's cached flag flips as db F17 says and the test does not depend on it); F15 end to end against a real `Pool` (`Read` transaction → `Internal`, no row, no spool file; the server-side `25006` → `Internal` backstop is db's own test); keyset search order and cursor continuity over 250 rows with equal `at`; `ON CONFLICT` idempotency by importing the same file twice; `CHECK` constraints (`actor_person_id` with kind, `level` with `fields`, `record_*` pairing) through `db.Wrap` → `Invalid`.
- **Handler tests:** `httptest` through the route set with a fake `Log` seam: 200 page shape, `Next` cursor, 401/403, F44, JSON Lines content type and attachment header for the export, `X-Read-Only` untouched (api's).
- **Conformance for writers:** `audittest.Fake` shares `Entry.Validate` and the area check with the real recorder (`TestFake_MatchesRealValidation` runs the same table through both), and the `Read`-transaction refusal (F45; `TestFake_MatchesRealValidation` gains the case: both return `Internal` under a `dbtest.Fake` `Transact(Read)`), so a module's unit tests catch a bad entry and a `Record` in the wrong transaction without a database.
- **No personal data:** every fixture uses invented IDs; no name or address appears in an entry, a spool line, a log or a span (`telemetrytest` fails the test otherwise).

## Amendment 2026-10-20 (approved, D44; jobs Q-JOBS6): `ActorSystem` from the `ActorFunc`

**Gap.** jobs.md Q-JOBS6 (approved, D43) settles the audit actor inside a job as "`app`'s `auditActor` answers `ActorSystem` when `jobs.InJob(ctx)`, no change to `audit`". But `resolveActor` accepts only `ActorPerson` or `ActorOperator` from the function and answers `Internal` for anything else (the bad-actor loop in `TestResolveActor_Order_GivenThenDeliveryThenActorFuncThenAnonymous` pins `{Kind: ActorSystem}` as `Internal`). Answering the system from `auditActor` as written would fail every `Record` in a module's job handler. `audit.purge` is unaffected (`Purge` sets `ActorSystem` on its own entry) and a retried signal delivery is covered by `signals.InDelivery`; what breaks is the first module job that audits (volunteers' reminders, files' sweep).

**Why the function was limited, and what is kept.** Decision 6 described the function as "the signed-in actor": a session, so anything else was taken as a bug in the wiring; the M2 build widened it to the operator for the same reason the operator exists (a command with no session). The limit was never a defence against a request: the function is one closure `app` fixes at `New` (F1), a caller of `Record` cannot reach it, and the only way to be "the system" is a ctx marked by `signals` (unexported key, set by the dispatcher) or by `jobs` (unexported key, set by the runner and by `jobstest.Fake.Run`); a request's ctx never carries either. So the amendment widens the accepted answers by one kind and keeps every consistency check: `Actor.Validate` still refuses `ActorSystem` or `ActorOperator` with a `Person` or a `Superuser`, a person without `Person`, and `ActorAnonymous` is still never the function's to say (it is audit's conclusion from `ok` false). The order of decision 6 is unchanged: the actor the caller gave, the delivery actor, the function, anonymous.

**Why not audit reading `jobs.InJob` itself, as it reads `signals.InDelivery`.** `jobs.Options.Audit` is an audit `Recorder`, so `audit` importing `jobs` is a cycle; a leaf package for the marker (as `auditsettings` and `perm` were) would be a third core package that exists only to carry one boolean, when the `ActorFunc` is already `app`'s place to say who acts when there is no session. One closure in `app` also means a later runner (a CLI batch, a migration hook) needs no audit change.

**The rule, in code terms.** `resolveActor`'s check becomes: `a.Kind` is `ActorPerson`, `ActorOperator` or `ActorSystem`, and `a.Validate() == nil`; otherwise `Internal` (F13b). `Titles` from the function are dropped as before. Nothing else in audit changes: the schema already stores `system` (`Purge` writes it), `Search` already filters on it, and the screen already shows it.

**`app`'s side** (app.md amendment of the same date): `auditActor` answers `{Kind: ActorSystem}, true` when `jobs.InJob(ctx)` **before** it looks for a session, matching audit's own order for a delivery (the delivery beats the function): a runner's ctx never carries a session, but `jobstest.Fake.Run` runs a handler on the test's ctx, which may, and the entry must still say `system`.

### Failure modes (added)

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F13b | `Record` | the `ActorFunc` answers `ok` true with an actor that is not a consistent person, operator or system (`ActorAnonymous`; a person without `Person`; the operator or the system with a `Person` or `Superuser`) | Internal | fix `app`'s wiring; a bug, never input | `TestResolveActor_ActorFuncReturnsBadActor_ReturnsInternal` (the bad-actor loop moved out of the order test; `{Kind: ActorSystem}` leaves the list, `{Kind: ActorSystem, Person: p}` and `{Kind: ActorSystem, Superuser: true}` join it) |

The accepted shapes each have a test: `TestResolveActor_ActorFuncReturnsOperator_IsTheOperator` (exists), `TestResolveActor_ActorFuncReturnsSystem_IsTheSystem` (new: `{Kind: ActorSystem, Titles: ["ignored"]}` from the function gives the system with no person, no superuser, no titles). The order test keeps its four rows; "delivery beats the function" is unchanged.

## Open questions

None. One was raised by the D36 (e) amendment and answered 2026-10-18 (D37, as proposed):

4. **Should `audittest.Fake` enforce the `Read`-transaction rule (F45)?** *Answered: yes (owner, 2026-10-18, D37).* The real recorder now fails `Internal` from `db.TxMode` before any statement, so the Fake can match it with the same one-line check (the Fake already imports `audit`, and `core/db` is a core contract it may import). The gain: a module that records inside its `Transact(Read)` fails its own unit tests and `moduletest.Conformance` instead of failing in production. The cost: any existing test that does so starts failing, which is the point, and `TestFake_MatchesRealValidation` gains one case. **Proposed answer: yes**, enforce it; the spec above is written that way (F45, Proposed change 16). If the owner prefers a lenient Fake, F45 and Proposed change 16 are dropped and nothing else changes.

The three earlier questions are settled as decisions (owner, 2026-10-18):

1. **Retention floor (decision 13, 15).** `audit.retention` is `0` (forever) by default (D30); when set it is at least 365 days, so a short period cannot defeat the log after an incident.
2. **Default of `audit.self_view` (decision 13, 15, D22).** `off` until the person-facing "your data was viewed" feature ships (the setting has no effect before then); once it ships a fresh installation defaults to `viewed` (that it was viewed, and when), which tells people the truth without naming anyone.
3. **Person IDs after deletion (decision 10, 15).** Audit rows keep `actor_person_id` and `subject_person_id` (pseudonymous IDs with no name behind them), reported as `Kept{Kind: "audit.entry"}` in the deletion result.

**Exception to decision 3 (owner, 2026-10-18).** Identity's sign-out clears the cookie before it records `identity.logout`, so an audit failure never keeps a person signed in; the response is still the audit error. This is the only action that may take effect before its entry is written. Every other reading of "no audit, no action" stands, including an emergency lookup on a read-only copy whose disk is full; the `spool_files` gauge and the spool warning make a filling disk visible first.

## Proposed changes elsewhere

Status as of 2026-10-19 (the 2026-10-20 ActorSystem amendment: approved D44, folded into the body): items 1, 2, 3, 4, 5, 6, 8, 9, 10, 11, 13 and 14 are applied (8 also for departments-events, D37; 6 on 2026-10-19, D39); item 7 (jobs) is applied 2026-10-20 (D43); item 12 waits for its target (`docs/operating.md`: not written) and are carried by this list until then. Items 15 and 16 (D36 (e), approved D37) are applied 2026-10-18.

1. (applied 2026-10-18) **`plan/design/app.md` (row 18), "How later core services plug in":** make `audit` part 1 and `identity` part 2, because identity's `build` takes `a.deps.Audit` (it is the first writer) while audit needs nothing from identity at build time: `Options.Actor` is a closure `app` writes over identity's exported ctx accessor, called only per request. The audit row becomes: `declare`: migration set `audit`, `corePerms` += `audit.Permissions`, settings declarations; `build`: `audit.New` over `cfg.Instance.Name`, `cfg.Data.Dir`, the pool, clock, telemetry, the actor closure, `a.titles` and `a.permissions` (nil until authz's part sets them: read lazily through small adapters), `a.deps.Audit` left unscoped and scoped in `provision` with `Scoped(m.Name)`; route set `audit` built from `Routes()`/`OpenAPI()` as `module.ModuleRoutes{Module: "audit"}`; runnable `Run` (start after `Set.Start`, stop with the runnables); closer `Close` after `Set.Stop`, before `db.Close`. `coreSets` order unchanged (audit has no foreign keys). `bms check` may print the spool file count (optional).
2. (applied 2026-10-18) **`plan/design/module-contract.md` (row 4):** `Deps.Audit audit.Recorder` is M1 (the table says M2 / M1; make it M1 only); add the rule "actions are prefixed `<name>.`; another prefix fails `Invalid`"; `moduletest.Conformance` sets `Deps.Audit = audittest.NewFake(manifest.Name)` when the test's provision leaves it nil, and the leak rule also scans `Recorded()` entries' `Reason` and `Fields` for the seed markers and `[email]`.
3. (applied 2026-10-18, by identity.md, which also adds `identity.account.read` (`Fields` `email`, `last_login_at`, `disabled_at`; level `personal`), `identity.account.deleted`, `identity.password.temporary-set`, `identity.sessions.ended`, `identity.superuser.reset` and `identity.account.linked` (people.md, M2); `Record` kinds used are `identity.account` and `identity.session`; the superuser's login records `identity.superuser.login` instead of `identity.login`; `identity.logout` is recorded after the cookie is cleared, the exception to decision 3) **`plan/design/identity.md` (row 6):** record through `Scoped("identity")`: `identity.login` (actor set explicitly to the person who authenticated, `Record` = the account), `identity.login-failed` (`ActorAnonymous`, `Record` = the account when one matched, zero otherwise; never the address), `identity.logout`, `identity.account.created`, `.disabled`, `.enabled`, `identity.password.changed`, `identity.superuser.created` (`ActorOperator`), `identity.superuser.login` (`Superuser: true`); export a ctx accessor `app` adapts to `audit.ActorFunc` (person and whether the session is the superuser's); changes recorded inside the `Write` transaction, logins outside any `Read` transaction.
4. (applied 2026-10-18, by authz.md Proposed change 5: `TitlesOf` returns the keys or renamed names of the non-base roles held now) **`plan/design/authz.md` (row 7):** implement `audit.TitleSource` (role names the person holds now, site and department) and `audit.PermissionChecker`; starter role "Audit reviewer" holds `audit.search` and `audit.export`; record grants, revocations and rule changes through `Scoped("access")`; ignore the session when `signals.InDelivery` (already proposed by signals).
5. (applied 2026-10-18, by people.md Proposed change 5: the actions and `Fields` listed there; `people.person.exported` at level `sensitive`; the person page links to `/audit?subject=<person>` for `audit.search` holders rather than offering the export button itself) **`plan/design/people.md` (row 8):** every read of a `personal` field by someone other than the person, and every `sensitive` read, records `people.<record>.read` with `Fields` and `Level` (`Sensitivity.ReadIsAudited`); export and deletion record `people.person.exported` / `.deleted`; deletion keeps audit rows and reports `Kept{Kind: "audit.entry"}` from `audit.CountAbout`; the person page offers "export views" to holders of `audit.export`.
6. (applied 2026-10-19, D39: `auditsettings.Settings` exported; retention in hours as above) **`plan/design/settings.md` (row 15):** the core settings declaration seam; hold `audit.retention` and `audit.self_view` as declared here; `settings` records its own changes through `Scoped("settings")`.
7. (applied 2026-10-20, D43, by jobs.md Proposed change 6: `app` registers `audit.purge` (hourly) through `jobs`) **`plan/design/jobs.md` (row 14):** the retention job reads `audit.retention` and calls `Purge(ctx, now - retention)` on the writing copy only; skips when 0.
8. (applied 2026-10-18) **`plan/design/telemetry.md` (row 15b):** mark Proposed change 7 applied (this spec records `TraceIDFrom`); add instruments `bms.audit.recorded{via}`, `bms.audit.record_failures{kind}`, `bms.audit.imported{outcome}`, `bms.audit.spool_files` to the instrument table.
9. (applied 2026-10-18) **`plan/design/config.md` (row 3):** mark Proposed change 8 applied: `Instance.Name` recorded on every entry; the spool under `Data.Dir/audit/spool`, created by `Run` at start, failing `Invalid` under `data.dir` (app prints it as `BMS_DATA_DIR`).
10. (applied 2026-10-18) **`plan/design/db.md` (row 5):** mark Proposed change 10 applied for `audit` with the deviation: `UPDATE` and `TRUNCATE` are revoked from the application role and a trigger refuses `UPDATE`, `DELETE` and `TRUNCATE`; `DELETE` is allowed only inside `core.audit_purge` through a transaction-local flag, because the installation has one role (db.md "Permissions") and a plain grant cannot distinguish the retention job from the app.
11. (applied 2026-10-18) **`plan/09-core-features.md` §8 and `plan/07-abstractions.md` "audit":** §8 "the database role the app uses can only insert and read" becomes "the app can only insert and read; the only deletion is the retention purge, which the schema enforces"; 07 gains an **Owns** line: "the `Entry` vocabulary, `Recorder` (scoped per area), the table and its guard, the read-only spool and its importer, `Search`/`Export` behind `audit.search`/`audit.export`, settings `audit.retention` and `audit.self_view`".
12. (pending: target not written) **`docs/operating.md`:** on a read-only copy the data dir holds audit entries until the copy writes again; it must be writable, backed up and protected like the database; one `bms serve` per data dir; what a `.failed` spool file is and what to do with it; disk full on a read-only copy makes audited reads fail.
13. (applied 2026-10-18) **`.claude/skills/go-conventions/SKILL.md` and `add-module/SKILL.md`:** add the rule: every read of `personal` fields about someone else and every `sensitive` read calls `deps.Audit.Record` after loading and before returning; a change records inside its `Write` transaction; never inside a `Read` transaction; actions are `<module>.<record>.<verb>`; a `Record` error fails the use case.
14. (applied 2026-10-18) **`plan/design/web-shell.md` (row 17):** `web/src/core/audit/` as a core area folder with the M2 screen above and its `messages/en.json`.
15. (applied 2026-10-18, D37) **`plan/design/db.md` (row 5):** `db.TxMode(ctx) (Mode, bool)`, amended in the same round (db.md decision 21 and its Proposed changes 14 and 15, which carry the `07-abstractions.md` "Owns" line and the skills' sentence: "on a read-only copy a `Record` inside a `Write` transaction fails with `Unavailable` wrapping `db.ErrReadOnly` like the change itself and is never spooled; only a `Record` outside any transaction spools"). No second copy of those edits is proposed here.
16. (applied 2026-10-18, D37) **`plan/design/module-contract.md` (row 4), `moduletest.Conformance` note:** one sentence: `audittest.Fake` fails `Internal` on a `Record` inside a `db.Read` transaction, so a module whose read path records inside its `Transact(Read)` fails its own unit tests and the conformance run; the fix is to record after the transaction, before returning (go-conventions). `identity.md`, `people.md` and `authz.md` need no change: their expectations of `audit` ("`Record` outside any `Read` transaction, the spool on a read-only copy", "`Recorder` honours the ctx transaction") still hold, and their failure rows for a read-only copy already name `Unavailable` wrapping `db.ErrReadOnly`.
