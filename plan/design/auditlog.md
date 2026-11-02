# Auditlog

- **Status:** Implemented (via boundaries.md, 2026-11-01)
- **Package:** `internal/core/auditlog` (service, `http.go`, `postgres/` store with the search, export, count and purge queries)   **UI:** `web/src/core/audit/` (unchanged; the screen keeps its folder and its `core.audit.*` catalogue)
- **Depends on specs:** `audit` (the vocabulary: `Query`, `Page`, `Cursor`, `Record`, `RecordID`, `Recorder`, `Action`; the table and its guard), `authz` (`Lookup`: `Authorizer`, `PermissionDecl`; `ActorFunc`), `settings` (`Reader`, `Decl`), `jobs` (`Registrar`), `db`, `telemetry`, `lib-errs`, `lib-core`, `api` (`httpx.Route`), `boundaries` (decisions 3, 6, 7)   **Used by:** `app` (constructor, `Start`, routes, link), `datarights` (`Export`, `CountAbout`), the web audit screen
- **Split from:** `audit.md` (2026-11-01, boundaries step 6). The F-numbers below are `audit.md`'s, unchanged; B6 to B8 are from `boundaries.md`.

## Summary

**Purpose.** Let the right people read, export and expire what the audit recorder wrote, so a regional can answer "who viewed this person's data" and keep the log only as long as it chooses.

**Exported names (package `auditlog`).**
- `Service` (`New(Options) (*Service, error)`; safe for concurrent use; no `Close`, no background work of its own).
- `Options{DB, Clock, Logger, Tracer, Authz authz.Authorizer, Actor authz.ActorFunc, Settings settings.Reader, Audit audit.Recorder, Jobs jobs.Registrar}`: `Authz` is the `authz.Lookup`; `Audit` is a recorder scoped to `"audit"` (required); `Settings` is scoped to `"audit"`; `Jobs` is scoped to `"audit"`. `New` fails `Invalid`, one `FieldError` per nil dependency (B6), and does no I/O.
- `(*Service).Start(ctx) error`: registers the `audit.purge` job with `Jobs`; never writes, so it succeeds on a read-only copy. `Conflict` if registered twice (B7).
- `(*Service).Search(ctx, audit.Query) (audit.Page, error)`: `audit.search`; records `audit.searched` first (F23 to F27).
- `(*Service).Export(ctx, model.PersonID, io.Writer) (int, error)`: `audit.export`; JSON Lines, oldest first; records `audit.exported` before the first line (F28 to F31).
- `(*Service).CountAbout(ctx, model.PersonID) (int, error)`: how many entries name the person as actor or subject; no permission of its own (the caller, `datarights`, has authorized the deletion).
- `(*Service).Routes() []httpx.Route` and `OpenAPI() []byte`: `GET /entries` and `GET /people/{person}/views` under `/api/audit/` (paths, names, keys and actions unchanged).
- `Settings []settings.Decl` (`audit.retention`, `audit.self_view`; declared here, replacing the deleted package `audit/auditsettings`); `Permissions []authz.PermissionDecl` (`audit.search`, `audit.export`); aliases `PermissionSearch`, `PermissionExport` of audit's name constants.
- `audit.Query`, `Page`, `Cursor`, `RecordID` stay in `audit` (the vocabulary the recorder's `Record` also uses).

**Key decisions.**
1 `auditlog` is a separate package because it needs `authz.Lookup.Allow` and `settings`, which sit above `audit` (boundaries decision 3); `audit` cannot import them without a cycle.
2 every `Search` and `Export` is itself audited (audit.md decision 11), with `Record` run before the read transaction (audit.md decision 12).
3 the purge rule moves unchanged from `app.auditPurge`: `audit.retention` 0 keeps forever; otherwise purge entries with `At` older than now minus the retention; on the writing copy only; through `core.audit_purge`, the table's only deletion path.
4 `audit.retention` floor is 365 days when set, in hours (`8760h` to `876000h`); `audit.self_view` is `off` until `ViewsOf` ships (audit.md decisions 13 and 15, owner 2026-10-18); no key, default or range changes.
5 `audit.search` and `audit.export` are site-wide permissions reaching `sensitive`; default role Audit reviewer; the superuser bypasses (D23) and is marked. Results are not filtered per department.
6 the job reads `audit.retention` through its own `settings.Reader`; `app` parses no setting (boundaries decision 6).
7 audit entries keep their person IDs after a person is deleted (audit.md decision 10); `CountAbout` is what `datarights` reports as `Kept{Kind: "audit.entry"}`.

**Errs kinds callers handle.** `Search`/`Export`: `Unauthenticated`, `Permission`, `Invalid` (query, cursor, zero person), store kinds, `Canceled` when the export's writer fails; a search works on a read-only copy (its own entry is spooled by the recorder). `CountAbout`: `Invalid` (zero person), store kinds. `Start`: the Registrar's kinds. The purge handler: `Unavailable` wrapping `db.ErrReadOnly` on a read-only copy (B8), store kinds. `New`: `Invalid`.

**Owns.** No table: it reads `core.audit_entry` and deletes only through `core.audit_purge` (both audit's; audit.md Data). Permissions `audit.search`, `audit.export`. Settings `audit.retention`, `audit.self_view`. Job `audit.purge` (hourly) with the catalogue key `module.audit.job.purge.title`. Route set `audit`: `GET /api/audit/entries`, `GET /api/audit/people/{person}/views` (JSON Lines). Spans `audit.search`, `audit.export`, `audit.purge`. Actions of its own: `audit.searched`, `audit.exported`, `audit.purged`, recorded through the recorder scoped to `audit`. No signals.

**Expects of dependencies.** `audit`: a `Recorder` scoped `"audit"`, passed as `Options.Audit`, for the three actions it records. `authz`: the `Lookup` answering `Allow` for the actor in ctx. `settings`: a `Reader` scoped to `audit`, a stored value that does not parse reads as the default. `jobs`: `Registrar` scoped `audit`, setting the system actor on every run. `db`: `Transact`, `TxMode`, `ReadOnly`, `ErrReadOnly`. `app`: builds `auditlog` after `audit`, `authz.Lookup`, `settings` and `jobs` (boundaries build order, row 8), registers `Permissions` and `Settings`, forwards `Routes` and `OpenAPI` through `module.CoreRoutes`, calls `Start`.

**Open questions:** None.

## Purpose

Let the right people read, export and expire the audit log.

## Responsibilities

- Searching the log and exporting the views of one person's data, for people holding `audit.search` and `audit.export`, and auditing those reads too.
- Counting the entries that name a person, for the deletion report.
- The retention rule and the `audit.purge` job that applies it.
- Declaring the permissions `audit.search` and `audit.export` and the settings `audit.retention` and `audit.self_view`.
- The routes `GET /api/audit/entries` and `GET /api/audit/people/{person}/views`.

## Not responsible for

| Concern | Owner |
|---|---|
| Writing entries, the spool and its importer, the table and its append-only guard, `WithSystem`/`WithOperator`, the actor order | `audit` |
| The `Query`, `Page`, `Cursor` and `Record` vocabulary | `audit` |
| Deciding whether a permission is held | `authz` (`Lookup`) |
| Storing the setting values | `settings`; this spec declares the keys and defaults |
| Scheduling and running the purge | `jobs`; this spec registers the job and supplies the rule |
| Showing a person that their data was viewed (D22, Later) | a later amendment adds `ViewsOf`; `people`'s person page shows it |
| Export and deletion of a person's data across modules | `datarights`; audit entries are kept (audit.md decision 10) and counted as `Kept` |
| The audit screen's rendering | `web/src/core/audit/` |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| **Reader** | Answer an audit reviewer's search and export, auditing the read. | `Search`, `Export`, `CountAbout`; routes and the screen |
| **Retention** | Delete entries past the regional's retention period. | `Start` registers `audit.purge`; the rule is one function |

## Interface

```go
package auditlog // internal/core/auditlog
//
// Imports: audit, authz, settings, jobs, db, telemetry, lib, model, lib/httpx.
// Never identity, module, api, config, app or a module.

// Settings declares audit.retention and audit.self_view (from the deleted auditsettings,
// verbatim: Area "audit", MinRetention 8760h, MaxRetention 876000h).
var Settings []settings.Decl

// Permissions declares audit.search and audit.export. The name constants stay in audit as
// vocabulary (people reads them and may not import auditlog); auditlog aliases them.
const (
	PermissionSearch = audit.PermissionSearch // "audit.search": search the log
	PermissionExport = audit.PermissionExport // "audit.export": export the views of one person's data
)

var Permissions = []authz.PermissionDecl{
	{Name: PermissionSearch, Scope: authz.ScopeSite, Reaches: model.SensitivitySensitive},
	{Name: PermissionExport, Scope: authz.ScopeSite, Reaches: model.SensitivitySensitive},
}

type Options struct {
	DB       db.DB
	Clock    clock.Clock
	Logger   *slog.Logger
	Tracer   telemetry.Tracer
	Authz    authz.Authorizer // the Lookup
	Actor    authz.ActorFunc  // identity.Actor
	Settings settings.Reader  // scoped "audit"
	Audit    audit.Recorder   // scoped "audit"; required
	Jobs     jobs.Registrar   // scoped "audit": Start registers audit.purge
}

// New validates o (errs.Invalid, one FieldError per nil dependency) and does no I/O.
func New(o Options) (*Service, error)

// Start registers audit.purge with Jobs. Fails with the Registrar's kinds (Conflict for a
// name registered twice). Never writes, so it succeeds on a read-only copy.
func (s *Service) Start(ctx context.Context) error

// Search, Export and CountAbout keep audit.md's signatures and failure modes (audit.search,
// audit.export). Routes returns GET /entries and GET /people/{person}/views as
// httpx.Route; OpenAPI the fragment.
func (s *Service) Search(ctx context.Context, q audit.Query) (audit.Page, error)
func (s *Service) Export(ctx context.Context, person model.PersonID, w io.Writer) (int, error)
func (s *Service) CountAbout(ctx context.Context, person model.PersonID) (int, error)
func (s *Service) Routes() []httpx.Route
func (s *Service) OpenAPI() []byte
```

**Behavior of each method (moved from `audit.md`).**
- `Search` returns one page matching `q`. It needs an actor (`Unauthenticated`, F23) holding `audit.search` (`Permission`, F24). It records `{audit.searched, Succeeded, Subject: q.Subject, Record: q.Record}` before returning (F27). It works on a read-only copy. It fails `Invalid` (F25) or the store's kind (F26).
- `Export` writes every entry whose `Subject` is `person`, oldest first, as JSON Lines (`application/x-ndjson`, one `audit.Record` per line) to `w`, and returns how many. It needs an actor holding `audit.export` (F29). It records `{audit.exported, Succeeded, Subject: person}` before the first line is written (F31; a failed export is still on record). It fails `Invalid` for a zero person (F28), `Canceled` wrapping the write error when `w` fails (F30), or the store's kind.
- `CountAbout` returns how many entries name `person` as actor or subject. `datarights` calls it inside an already authorized deletion to report `Kept{Kind: "audit.entry"}`; no permission of its own. It fails `Invalid` for a zero person, or the store's kind.
- `Routes` lists `GET /entries` and `GET /people/{person}/views`. Handlers are thin: parse and validate the query, call `Search` or `Export`, `httpx.Fail` on error. A bad query records nothing; invalid fields are named `query.<param>`; a repeated param is Invalid; `from` and `to` must come together. An `Export` failure after lines are written aborts the response with `http.ErrAbortHandler`. Cannot fail. `OpenAPI` cannot fail (embedded; a test proves the fragment parses and names exactly the routes).

**Recording.** The three actions (`audit.searched`, `audit.exported`, `audit.purged`) are recorded through `Options.Audit`, an `audit.Recorder` scoped to `"audit"` (required; `New` fails `Invalid` if nil). Settled in boundaries erratum E3 (2026-11-01).

**Consumers per method.**

| Method | Consumer | Pre-condition | Post-condition |
|---|---|---|---|
| `Search` | the audit screen, `GET /api/audit/entries` | an actor with `audit.search` | a page, newest first, plus an `audit.searched` entry |
| `Export` | the screen's "export views", `GET /api/audit/people/{person}/views` | an actor with `audit.export` | JSON Lines on `w`, plus an `audit.exported` entry recorded first |
| `CountAbout` | `datarights` | an authorized deletion in progress | the count |
| `Start` | `app` (lifecycle) | `New` done | `audit.purge` registered |
| `Routes`, `OpenAPI` | `app` builds the `audit` route set (`module.CoreRoutes`) | none | mounted under `/api/audit/` |
| `Permissions`, `Settings` | `app` (`corePerms`, settings declarations) | none | none |

## Types

Defined here: `Service`, `Options`. Uses `audit.Query`, `audit.Page`, `audit.Cursor`, `audit.Record`, `audit.RecordID`, `audit.Action`; `model.PersonID`, `model.Sensitivity`, `model.TimeRange`; `authz.Permission`, `authz.PermissionDecl`, `authz.Authorizer`, `authz.ActorFunc`; `settings.Decl`, `settings.Reader`; `jobs.Registrar`; `httpx.Route`.

## Data

No table, no migration. The `postgres/` subpackage reads `core.audit_entry` (audit's table, audit.md Data) with four search queries (unkeyed, by actor, by subject, by record) and an expanded keyset predicate, counts by actor or subject, and deletes only through `core.audit_purge(before)`, which sets the transaction flag the guard trigger honours. Two sub-abstractions on one table: `audit` inserts, `auditlog` reads and deletes by retention. Sensitivity of the columns read: audit.md Data (`actor_person_id`, `subject_person_id` and `reason` are **personal**; everything else is internal). Retention rule: `audit.retention` below.

## Permissions

| Operation | Permission | Scoped by | Default roles (authz starter data) |
|---|---|---|---|
| `Search`, `GET /api/audit/entries` | `audit.search` (site; reaches sensitive) | site | Audit reviewer; the superuser bypasses (D23) and is marked |
| `Export`, `GET /api/audit/people/{person}/views` | `audit.export` (site; reaches sensitive) | site | Audit reviewer |
| `CountAbout` | none: called by `datarights` inside an already authorized deletion | none | none |
| the `audit.purge` job | none: system work with no actor (`ActorSystem`, set by `jobs`) | none | none |

A person never sees entries about themself through these routes unless they hold the permission; the self view is D22's later feature with its own setting. Search results are not filtered per department: the permission is site-wide, matching "audit reviewer" as one of the separated site admin jobs (D23). Department-scoped audit review is not designed; if a regional asks, `department_id` becomes a nullable column and `audit.search` a `ScopeDepartment` permission in a later migration.

## Signals

None published, none subscribed.

## Settings

Declared as `auditlog.Settings []settings.Decl` in the core settings area `audit`, through the declaration seam `settings.md` defines (`settings.Decl`, `Check` per key); `app` passes it to `settings.New`. `auditlog` reads them through its own `settings.Reader`. A reader that cannot parse a stored value gets the default (settings decision 5).

| Key | Type | Default | Valid range | Who can change it |
|---|---|---|---|---|
| `audit.retention` | Duration | `0` (keep forever, D30) | `0`, or `8760h` (365 days) to `876000h` (100 years), written in hours because the kind is `time.ParseDuration` (floor settled by the owner, 2026-10-18; hours, D39) | Settings admin; the change is audited by `settings` |
| `audit.self_view` | Enum | `off` until the Later `ViewsOf` feature ships, then `viewed` (owner, 2026-10-18) | `off`, `viewed`, `fields`, `titles` (D22, cumulative) | Settings admin; read by the Later `ViewsOf` feature only |

Validation errors are `errs.Invalid` field errors under the key, raised by `settings` from the declaration's `Check`.

## UI

`web/src/core/audit/` (unchanged: paths and folder stay), registered like a module's folder (web-shell decision 3), menu entry "Audit log" shown only to holders of `audit.search` (the shell hides what the permissions list excludes; the server checks again).

- **Audit log** (`/audit`): filters (person as subject, actor, record kind and ID, area or action, date range, superuser only), a newest-first list of entries as cards (phone-first: action, time, actor title or "superuser", subject playa name resolved through `people`'s batch lookup when the viewer may see it, otherwise the ID), "Load more" by cursor. **Empty:** "No audit entries match these filters." **Error:** the shell's error banner with the reference. **Forbidden:** the route is hidden; a direct visit shows the shell's forbidden screen. **Read-only copy:** works; the page shows the shell's read-only banner.
- **Export views of a person** (a button on the list when a subject is chosen; the screen's subject filter accepts `?subject=<person>` from the URL, and `people`'s person page links holders of `audit.search` to `/audit?subject=<person>` instead of calling the route itself, so `web/src/core/people/` never imports this folder): downloads `GET /api/audit/people/{person}/views` as `views-<person>.jsonl`. The screen says the export is recorded.
- The person's own "who viewed my data" (D22) is **Later**, not in this spec's UI.

Strings are catalogue keys under `core.audit.*` in `web/src/core/audit/messages/en.json`, plus `module.audit.job.purge.title` ("Delete audit entries past their retention", D58): the Background work screen (jobs.md W1.4) looks the `audit.purge` job's title up in the owner's catalogue, which for audit is this file.

## Behavior

**Search (audit decision 11).** Authorize (actor, then `audit.search`), validate, record `audit.searched`, then query with keyset pagination on `(at, id)`; `Limit` 0 means `audit.DefaultPageSize`; `Next` is set when `Limit + 1` rows came back. Export streams in batches of `audit.MaxPageSize`, oldest first, `audit.exported` recorded before the first row is read; a writer failure stops the stream with `Canceled`. Both run in a `Read` transaction for the query only; the search sets `plan_cache_mode = force_custom_plan` with `set_config(..., true)` as the transaction's first statement, so only that transaction is affected (D52(e)); their own `Record` runs before the transaction starts (audit decision 12).

**Purge.** The purge is an unexported method, used only by the `audit.purge` job handler (not Interface; tested white-box). The handler reads `audit.retention`: 0 keeps forever and deletes nothing. Otherwise it purges entries older than now minus the retention, on the writing copy only. In one `Write` transaction: `SELECT core.audit_purge($1)`, then `Record` of `audit.purged` with `Actor{Kind: ActorSystem}` on the same ctx (so the count and the purge commit together; the entry's `Reason` is empty, the count is in the job's log line). A zero or future `before` is `Invalid` (F40, enforced by the SQL function too); on a read-only copy the handler returns `Unavailable` wrapping `db.ErrReadOnly` and `jobs` retries later (F41, B8). The rule moves unchanged from `app.auditPurge`.

**Concurrency and lifecycle.** `*Service` is safe for concurrent use once built and has no `Close`. `New` does no I/O. `Start` registers the job once.

**Telemetry.** Spans `audit.search`, `audit.export` (`Int("records")`), `audit.purge` (`Int("deleted")`). Never a field name, reason, title or person ID on a span or metric.

## Failure modes

The rows are `audit.md`'s, moved with their numbers unchanged and their tests moved verbatim; B6 to B8 come from `boundaries.md`.

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F23 | `Search` | no actor in ctx | Unauthenticated | 401 | `TestSearch_NoActor_ReturnsUnauthenticated` |
| F24 | `Search` | actor lacks `audit.search` (or nil checker) | Permission | 403 | `TestSearch_NoPermission_ReturnsPermission` |
| F25 | `Search` | bad `Limit`, `After`, `Within`, `Record`, `Area` or `Action` | Invalid (field) | 400 with field errors | `TestSearch_BadQuery_ReturnsInvalid` |
| F26 | `Search` | store fails | db's kind | map by kind | `TestSearch_StoreFails_ReturnsKind` |
| F27 | `Search` | its own `audit.searched` entry cannot be recorded | the `Record` error | fail the search (audit decision 3) | `TestSearch_RecordFails_ReturnsError` |
| F28 | `Export` | zero person | Invalid (`person`) | 400 | `TestExport_ZeroPerson_ReturnsInvalid` |
| F29 | `Export` | no actor / lacks `audit.export` | Unauthenticated / Permission | 401 / 403 | `TestExport_NoPermission_ReturnsPermission` |
| F30 | `Export` | `w.Write` fails mid-stream | Canceled (wrapping the write error) | nothing (client gone); the `audit.exported` entry stays | `TestExport_WriterFails_ReturnsCanceled` |
| F31 | `Export` | its own entry cannot be recorded | the `Record` error | fail before writing a line | `TestExport_RecordFails_WritesNothing` |
| F40 | `purge` (unexported) | `before` zero or in the future | Invalid (`before`) | fix the job | `TestPurge_FutureOrZeroBefore_ReturnsInvalid_NothingDeleted` (white-box: the unexported `purge`) |
| F41 | `purge` (unexported) | copy read-only | Unavailable wrapping `db.ErrReadOnly` | job skips on a read-only copy | `TestPurge_ReadOnlyCopy_ReturnsUnavailable` (B8) and `TestPurge_ReadOnlyPool_ReturnsUnavailableWrappingErrReadOnly` |
| F44 | handlers | bad query string (`limit`, `after`, dates, UUIDs) | Invalid | 400 with field errors (`query.<name>`) | `TestEntriesHandler_BadQuery_Returns400` |
| B6 | `New` | nil dependency | Invalid | fail the build | `TestNew_NilDependency_ReturnsInvalid` |
| B7 | `Start` | `audit.purge` registered twice | Conflict | fail the start | `TestStart_Twice_ReturnsConflict` |
| B8 | purge handler | read-only copy | Unavailable wrapping `db.ErrReadOnly` | jobs retries later | `TestPurge_ReadOnlyCopy_ReturnsUnavailable` |

Also moved: `TestPurge_RetentionZero_DeletesNothing` and `TestStart_RegistersAuditPurge` (new, boundaries step 6). Methods that cannot fail: `Routes`, `OpenAPI`.

## Lock-in check

No external technology beyond PostgreSQL 16, already the project's baseline. Nothing here would change on another OSI-licensed store.

## Test plan

- **Unit** (no infrastructure): `dbtest.NewFake()`, `clock.NewFake`, a fake `authz.Authorizer`, a fake `settings.Reader` and `jobstest.Fake` for `Start` and the purge handler (retention 0 deletes nothing; otherwise the cut-off is now minus retention).
- **Permission tests:** `Search` and `Export` with no actor, with an actor lacking the permission, with it; `audit.searched` and `audit.exported` recorded exactly once per call; superuser actor marked. Moved from `audit` (`TestSearch_*`, `TestExport_*`) verbatim.
- **Integration** (`//go:build integration`, `dbtest.TempDB`): keyset search order and cursor; the purge through `core.audit_purge` against a real table; `CountAbout`.
- **Handler tests:** `httptest` through the route set: 200 page shape, `Next` cursor, 401/403, F44, JSON Lines content type and attachment header for the export, `X-Read-Only` untouched (api's). The audit screen's e2e is unchanged (same paths).
- **Job list:** `catalogue_jobs_test` (the job list is unchanged).
- **No personal data:** every fixture uses invented IDs; no name or address appears in an entry, a log or a span.

## Open questions

None.

## Proposed changes elsewhere

None beyond those carried by `boundaries.md`: `audit.md` (item 11, applied), `07-abstractions.md` (item 2), `go-conventions` Layers (item 9), `app.md` (item 10), `settings.md` (item 17), `api.md` (item 24).
