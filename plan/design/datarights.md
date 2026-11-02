# Data rights

- **Status:** Approved (via boundaries.md, 2026-10-31)
- **Package:** `internal/core/datarights` (service, `http.go`, `commands.go`, `Templates`)   **UI:** none of its own; the "Your data" section of My profile, the person page's export and delete buttons and the Deletion requests screen stay in `web/src/core/people/` (`YourData.tsx`, `screens/Requests.tsx`), calling `/api/datarights/...`
- **Depends on specs:** `people` (`PeopleSource`, answered by `people.Service`), `identity` (`AccountSource`: `ExportPerson`, `DeletePerson`, `EmailOf`), `authz` (`Authorizer` from the `Lookup`; `RawSource` on `authz.Service`; `HoldersOf` on the `Lookup`), `auditlog` (`CountAbout`), `notify` (`Notifier`), `files` (`FilesSource`), `audit` (`Recorder`), `signals` (`Bus`), `db`, `telemetry`, `lib-errs`, `lib-core`, `model`, `api` (route mounting), `module-contract` (the aliases)
- **Used by:** `module-contract` (`PersonData`, `Export`, `Deletion`, `Kept` are aliases of this package's types, so every module implements them), `moduletest.Conformance`, `app` (builds it, calls `Link`, runs the `export` and `delete` commands), the web shell (`/api/datarights/...`)
- **Split from:** `people.md` (2026-11-01, boundaries step 12, decision 5). The F-numbers and test names below are `people.md`'s, unchanged; the G rows and their tests are from `boundaries.md`. Decisions 1 to 8 here were people decisions 10, 11, 12, 19, 24, 25, 26 and 27.

## Summary

**Purpose.** Carry out a person's right to their data across the core and every module: the export, the deletion and the request to delete.

**Imports.** `internal/core/datarights` imports `model`, `lib`, `core/db`, `core/audit`, `core/authz`, `core/identity`, `core/auditlog`, `core/notify`, `core/files`, `core/signals`, `core/telemetry`, `core/people` (only for the `PeopleSource` doc; the interface is declared here and satisfied by `*people.Service`). Never `core/module`, `api`, `app`, `config` or a module: the modules' sources reach it through `app`'s `Link`.

**Names.** `datarights` is the package, the core part, the route set (`/api/datarights/`), the audit area (`datarights.export|delete|request.*`), the signal prefix and the template owner. The migration set stays `people`'s (`core.data_request`, `0002_requests.sql`: forward-only). The permission names stay `people.data.export` and `people.data.delete` (boundaries decision 12); a rename with a `core.role_permission` data migration may come later and would be recorded here.

**Exported.**
- `Export{Data json.RawMessage}`, `Kept{Kind string; Count int}`, `Deletion{Deleted, Anonymized int; Kept []Kept}`, `PersonData{ExportPerson(ctx, PersonID) (Export, error); DeletePerson(ctx, PersonID) (Deletion, error)}`, `Source{Name string; PersonData}`: the shapes `module` aliases (`module.PersonData = datarights.PersonData`, and the same for the other three).
- The core sources, in the shapes the services already have: `RawSource` (authz, notify: `ExportPerson` raw JSON, `DeletePerson` a count), `AccountSource` (`RawSource` plus `EmailOf`, identity: the account's existence, which export and deletion of a person with no row need, F58), `FilesSource` (`DeletePerson` returns `files.Deletion`), `PeopleSource` (people's own rows: `ExportPerson` raw JSON, `DeletePerson` count, `Exists`), `AuditLog{CountAbout}`, `HolderSource{HoldersOf}`.
- `Request{ID, Person, Kind, Status, Note, CreatedAt, DecidedAt, DecidedBy, DecisionNote}`, `RequestID`, `RequestStatus` (`RequestOpen`, `RequestWithdrawn`, `RequestDeclined`), `RequestQuery{Status, Person}`, `DeletionReport{Person, People, Identity, Access int, Modules map[string]Deletion, Kept []Kept}`; `ExportFormat = 1`.
- `Options{DB, Clock, Logger, Tracer, Meter, Audit (scoped "datarights"), Signals (scoped "datarights"), Authz, Actor, Holders (required), Notify (required, scoped "datarights"), AuditLog, Accounts AccountSource, Access, Outbox RawSource, Files FilesSource, People PeopleSource}` (+`Validate`); `New(Options) (*Service, error)`; `(*Service).Link(modules []Source) error` (once).
- Use cases (keep people.md's signatures, permissions, audit reasons and failure modes): `Export`, `Delete`, `RequestDeletion`, `MyRequests`, `WithdrawRequest`, `ListRequests`, `DeclineRequest`, `FulfilRequest`, `ExportCommand(ctx, io.Writer, string) error`, `DeleteCommand(ctx, io.Writer, string) error`, `Routes() []httpx.Route`, `OpenAPI() []byte`.
- `Permissions []authz.PermissionDecl` (`people.data.export`, `people.data.delete`, both site, `sensitive`, starter role `account-admin`); `PublishedSignals = []signals.Name{SignalDeletionRequested}` (`datarights.deletion-requested {request, person}`); `Templates fs.FS` (the deletion-request notice, `en`, `es`).
- Constants: `ExportCooldown = 10 * time.Minute`, `MaxTrackedExports = 10000`, `MaxNoteBytes = 500` (moved from people).
- Accepted extra exports (as people.md accepts its own): `Area` (the core area name), `AccountSource`, `PermissionDataExport` and `PermissionDataDelete` (the two permission names; `people` keeps its own constants for `Capabilities`), `PayloadPerson` and `PayloadRequest` (the signal payload keys), `TemplateDeletionRequested`, `ParseRequestStatus`.
- Routes under `/api/datarights/`, all signed-in: `GET /me/export`, `GET /me/requests`, `POST /me/requests`, `POST /me/requests/{request}/withdraw`, `GET /requests`, `POST /requests/{request}/decline`, `POST /requests/{request}/fulfil`, `GET /people/{person}/export`, `DELETE /people/{person}`. Commands `bms export <person-id>` and `bms delete <person-id>`.
- Audit actions `datarights.export`, `datarights.delete`, `datarights.request.created|withdrawn|declined|fulfilled|read` (old entries keep `people.person.exported`, `people.person.deleted` and `people.request.*`).

**Key decisions.**
- **1.** (people 10) a self export is immediate (audited, `ExportCooldown`): it changes nothing and the data is the person's; a self deletion is a request a holder of `people.data.delete` fulfils or declines, because it is irreversible and touches safety records (owner, 2026-10-18).
- **2.** (people 11) deletion is one `Write` transaction: modules in build order, then files, then notify, then access (authz), then identity, then the person's requests, then people's rows (`DeletionReport.People` keeps the old count); any failure (`last_access_admin`, a superuser account, a module error) rolls everything back; audit rows stay and are reported as `Kept{Kind: "audit.entry"}` (audit decision 10).
- **3.** (people 12) export runs in no transaction and datarights records the one `datarights.export` entry; modules and sources do not audit inside `ExportPerson`.
- **4.** (people 19) the download, the deletion requests, the admin's export and delete and the two commands ship in M2 (owner, 2026-10-18).
- **5.** (people 24, D37) reads of deletion-request notes (`MyRequests`, `ListRequests`) by someone other than the requester are audited.
- **6.** (people 25, D37) data rights for an account holder with no `core.person` row still run the identity and access steps.
- **7.** (people 26, D37) `Delete` publishes, through authz's `DeletePerson`, `access.grant-revoked` for each removed grant.
- **8.** (people 27) the deletion-request notice is sent by datarights through its own `Notifier`, to the holders of `people.data.delete`, in `RequestDeletion`'s `Write` transaction; notify subscribes to nothing (notify decision 14).
- **9.** (boundaries 5) the package owns the types the module contract aliases; `people` answers for its own rows as one more source (`PeopleSource`), last in the deletion order; `people.person-created` and `people.person-deleted` stay people's, and people publishes the latter inside datarights' transaction, after commit, as before.
- **10.** (boundaries 11, 13) the signal is `datarights.deletion-requested`, the routes are under `/api/datarights/`, the audit actions are `datarights.*`; permission names, tables, columns and migrations do not change.

**errs kinds a caller must handle.** `Unauthenticated` (no actor, restricted session, inside a delivery), `Permission`, `NotFound` (unknown person or request), `Invalid` (zero ID, note too long, self deletion, duplicate source name), `Conflict` (`last_access_admin`, `in_use`, `duplicate`, `status`, `protected`, `Link` twice), `Unavailable` (read-only copy, rate limit `httpx.ErrRateLimited`), `Internal` (before `Link`), the sources' kinds wrapped with the source name.

**Expects of other specs.** `people`: `PeopleSource` (`Exists` is `NotFound` for no row; `DeletePerson` runs inside the caller's transaction and publishes `people.person-deleted` after commit). `identity`, `authz`, `notify`: `ExportPerson`/`DeletePerson` as today. `files`: its `DeletePerson` returns `files.Deletion`. `auditlog`: `CountAbout`. `authz.Lookup`: `HoldersOf`. `module-contract`: the four aliases. `app`: builds it after notify, identity and people, calls `Link` with the modules' `Set.PersonData()` converted, and mounts the route set `datarights`. `moduletest.Conformance` keeps compiling and passing because the aliases are the same types.

## Purpose

Carry out a person's right to their data across the core and every module.

## Responsibilities

- Export one person's data across identity, access, notify, files, the people rows and every module, as one JSON document, audited, with the self-export cooldown.
- Delete one person across the same sources in one audited transaction, in the order of decision 2, and return the report.
- Take, list, withdraw, decline and fulfil deletion requests (`core.data_request`), auditing reads of their notes.
- Tell the holders of `people.data.delete` that a request is waiting, without naming the person or the note.
- Publish `datarights.deletion-requested`.
- Own the `export` and `delete` commands and their exit statuses.
- Declare `people.data.export` and `people.data.delete`.

## Not responsible for

| Not this | Whose it is |
|---|---|
| The person row, contacts, consents, profile screens and the person page | `people` (it answers for its own rows through `PeopleSource`) |
| A module's own records and their export or deletion | each module's `PersonData` (module-contract); datarights orchestrates and reports |
| Identity's account rows, access grants, notify's outbox, files' attachments | `identity`, `authz`, `notify`, `files` (their `ExportPerson`/`DeletePerson`) |
| Storing and searching audit entries; counting those that name a person | `audit`, `auditlog` (`CountAbout`) |
| Deciding who may export or delete | `authz` (the permission names are datarights' declarations) |
| Delivering the notice | `notify` (datarights calls `Send`) |
| The screens | `web/src/core/people/` (they call datarights' routes) |
| Retention jobs, an export request kind, "who viewed my data" | Later (`jobs`, `audit` `ViewsOf`) |
| Backups, which still hold deleted data | the deployment (privacy-data "Backups contain everything") |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| Export | Assemble one document from every source. | `Export`, `ExportCommand`, the cooldown |
| Deletion | Delete across every source in one transaction. | `Delete`, `FulfilRequest`, `DeleteCommand`; the advisory lock; the order |
| Requests | Take and decide deletion requests, and announce them. | `core.data_request`; `RequestDeletion`, `MyRequests`, `WithdrawRequest`, `ListRequests`, `DeclineRequest`; the notice |
| Sources | Hold the modules' and the core's sources in build order. | `Link`, `Source`, `RawSource`, `FilesSource`, `PeopleSource` |

## Interface

```go
package datarights // imports people, identity, authz, auditlog, notify, files, audit, signals, db, telemetry, lib, model; never module

// The types module-contract aliases (module.PersonData = datarights.PersonData, ...).
type Export struct{ Data json.RawMessage }
type Kept struct{ Kind string; Count int }
type Deletion struct{ Deleted, Anonymized int; Kept []Kept }
type PersonData interface {
    ExportPerson(ctx context.Context, person model.PersonID) (Export, error)
    DeletePerson(ctx context.Context, person model.PersonID) (Deletion, error)
}
type Source struct{ Name string; PersonData }

// The core sources, in the shapes the services already have (no adapter anywhere).
type RawSource interface { // authz.Service, notify
    ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
    DeletePerson(ctx context.Context, person model.PersonID) (int, error)
}
type FilesSource interface {
    ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
    DeletePerson(ctx context.Context, person model.PersonID) (files.Deletion, error)
}
type PeopleSource interface { // people.Service: its own rows, last
    ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
    // DeletePerson deletes the person row, contacts and consents inside the caller's Write
    // transaction and publishes people.person-deleted after commit; Conflict when a module
    // still references the person (FK RESTRICT: a programming error in a module).
    DeletePerson(ctx context.Context, person model.PersonID) (int, error)
    // Exists reports whether the person row exists (NotFound otherwise).
    Exists(ctx context.Context, person model.PersonID) error
}
// AuditLog is auditlog's CountAbout, for the deletion report.
type AuditLog interface {
    CountAbout(ctx context.Context, person model.PersonID) (int, error)
}
// HolderSource is the authz.Lookup's HoldersOf: the explicit site-wide holders of a permission.
type HolderSource interface {
    HoldersOf(ctx context.Context, p authz.Permission) ([]model.PersonID, error)
}

type RequestID = model.ID[request]
type RequestStatus uint8 // RequestOpen = iota + 1, RequestWithdrawn, RequestDeclined

// Request is a person's deletion request. Kind is "delete" (the column exists so an export
// request can be added without a migration). Note and DecisionNote are personal.
type Request struct {
    ID           RequestID
    Person       model.PersonID
    Kind         string
    Status       RequestStatus
    Note         string
    CreatedAt    time.Time
    DecidedAt    time.Time // zero while open
    DecidedBy    model.PersonID
    DecisionNote string
}
type RequestQuery struct {
    Status RequestStatus  // zero = open
    Person model.PersonID // zero = any
}

// DeletionReport is what Delete, FulfilRequest and the delete command return.
type DeletionReport struct {
    Person   model.PersonID
    People   int                 // requests + consents + contacts + the person row (as before the split)
    Identity int                 // account rows
    Access   int                 // grant rows
    Modules  map[string]Deletion // by source name (modules, files, notify)
    Kept     []Kept              // always includes {"audit.entry", n} when n > 0
}

const ExportFormat = 1

var Permissions []authz.PermissionDecl // people.data.export, people.data.delete (names unchanged, boundaries decision 12)
var PublishedSignals = []signals.Name{SignalDeletionRequested} // "datarights.deletion-requested" {request, person}
var Templates fs.FS // the deletion-request notice (en, es)

type Options struct {
    DB       db.DB
    Clock    clock.Clock
    Logger   *slog.Logger
    Tracer   telemetry.Tracer
    Meter    telemetry.Meter // optional
    Audit    audit.Recorder   // scoped "datarights"
    Signals  signals.Bus      // scoped "datarights"
    Authz    authz.Authorizer // the Lookup
    Actor    authz.ActorFunc
    Holders  HolderSource     // required; the Lookup: who is told of a new request (people.data.delete)
    Notify   notify.Notifier  // required; scoped "datarights": the admin notice
    AuditLog AuditLog         // auditlog: CountAbout
    Accounts AccountSource    // identity: RawSource + EmailOf
    Access   RawSource        // authz.Service
    Outbox   RawSource        // notify
    Files    FilesSource
    People   PeopleSource
}

// Validate fails errs.Invalid joining a validate.FieldError per nil dependency (fields
// datarights.db, .clock, .logger, .tracer, .audit, .signals, .authz, .actor,
// .holders, .notify, .audit_log, .accounts, .access, .outbox, .files, .people).
func (o Options) Validate() error
func New(o Options) (*Service, error) // no I/O; works against db.None()

// Link gives the service the modules' sources, in build order. Fails Conflict when called
// twice, Invalid for a duplicate name or a nil source. The deletion order is: modules in
// build order, files, notify, access, identity, people (foreign keys point at core.person
// with RESTRICT; people.md decision 11).
func (s *Service) Link(modules []Source) error

func (s *Service) Export(ctx context.Context, person model.PersonID) (json.RawMessage, error)
func (s *Service) Delete(ctx context.Context, person model.PersonID) (DeletionReport, error)
func (s *Service) RequestDeletion(ctx context.Context, note string) (Request, error)
func (s *Service) MyRequests(ctx context.Context) ([]Request, error)
func (s *Service) WithdrawRequest(ctx context.Context, id RequestID) error
func (s *Service) ListRequests(ctx context.Context, q RequestQuery) ([]Request, error)
func (s *Service) DeclineRequest(ctx context.Context, id RequestID, note string) (Request, error)
func (s *Service) FulfilRequest(ctx context.Context, id RequestID) (DeletionReport, error)

// Commands (app's export and delete; actor audit.ActorOperator; no permission check: the
// operator has the database). ExportCommand writes the JSON to out; DeleteCommand writes
// the report, one line per part. id is parsed with model.ParseID (Invalid, exit 2).
func (s *Service) ExportCommand(ctx context.Context, out io.Writer, id string) error
func (s *Service) DeleteCommand(ctx context.Context, out io.Writer, id string) error

func (s *Service) Routes() []httpx.Route
func (s *Service) OpenAPI() []byte
```

**Use cases** (moved from people.md; the audit actions are `datarights.*` since boundaries decision 11):

| Method | Consumer | Pre-conditions | Post-conditions |
|---|---|---|---|
| `Export` | profile "Download my data"; person page export; admin | self (cooldown) or `people.data.export`; `Link` done | one JSON document (shape under Behavior); `datarights.export` (level sensitive) recorded before the body is written |
| `Delete` | person page delete; admin | `people.data.delete`; not self; `Link` done | everything gone in one transaction; `datarights.delete`; `people.person-deleted` published by the people source |
| `RequestDeletion` | profile screen | an actor; no open request; note ≤ `MaxNoteBytes` | open request; `datarights.request.created`; `datarights.deletion-requested` published |
| `MyRequests` | profile screen | an actor | own requests, newest first (the requester's own read is not audited) |
| `WithdrawRequest` | profile screen | own and open | status withdrawn; `datarights.request.withdrawn` |
| `ListRequests` | requests screen | `people.data.delete` | requests by status (default open), oldest first, with the person's playa name resolved by the shell through `/api/people/names`; records one `datarights.request.read` (`Level: personal`, `Fields: [note]`, `Subject` the requester) per returned request that has a note and is not the actor's own (D37, D38 (u)) |
| `DeclineRequest` | requests screen | `people.data.delete`; open | status declined, note; `datarights.request.declined` |
| `FulfilRequest` | requests screen | `people.data.delete`; open; not self | `Delete` of the request's person; `Reason: "request:<id>"` |
| `ExportCommand`/`DeleteCommand` | `app` | writing copy for delete; `Link` done | as `Export`/`Delete` with `ActorOperator`, `Reason: "command"` |
| `Link` | `app` | once | sources frozen |
| `Routes`/`OpenAPI` | `app` → `api` | — | the table below; a fragment that matches |

**Routes** (relative to `/api/datarights`, all signed-in; they were under `/api/people` until boundaries step 12, decision 14):

| Method and pattern | Use case | Notes |
|---|---|---|
| `GET /me/export` | `Export` (self) | `Content-Disposition: attachment; filename="bms-export-<person>.json"` |
| `GET /me/requests`, `POST /me/requests`, `POST /me/requests/{request}/withdraw` | requests (self) | body `{kind: "delete", note}` |
| `GET /people/{person}/export` | `Export` | attachment as above |
| `DELETE /people/{person}` | `Delete` | returns the report |
| `GET /requests?status=&person=` | `ListRequests` | |
| `POST /requests/{request}/decline` | `DeclineRequest` | body `{note}` |
| `POST /requests/{request}/fulfil` | `FulfilRequest` | returns the report |

Handlers are thin (`httpx.ReadJSON`, the service, `httpx.WriteJSON`, `httpx.Fail`); path IDs through `model.ParseID`.

## Types

Defined above; uses `model.PersonID`, `model.Sensitivity`, `authz.Permission`, `authz.PermissionDecl`, `authz.ActorFunc`, `audit.Entry`, `signals.Name`, `signals.Payload`, `files.Deletion`.

## Data

Owns `core.data_request`; the migration stays in people's set (`0002_requests.sql`, forward-only). `people` keeps no request store code; its one remaining touch is the `NOT EXISTS` read of the table in `delete_orphan_person.sql`. A future change to the table goes in a `datarights` set added to `coreSetOrder` after `people` when first needed. Retention: a request lives until its person is deleted.

**`core.data_request`**

| Column | Type | Constraints | Level |
|---|---|---|---|
| `id` | `uuid` | PK | — |
| `person_id` | `uuid` | NOT NULL, FK `core.person(id)` ON DELETE CASCADE | — |
| `kind` | `text` | NOT NULL, CHECK `kind = 'delete'` (widened later) | — |
| `status` | `text` | NOT NULL, CHECK in `('open','withdrawn','declined')` | — |
| `note` | `text` | NOT NULL DEFAULT '', `<= 500` | personal (the person's words) |
| `created_at` | `timestamptz` | NOT NULL | — |
| `decided_at` | `timestamptz` | NULL | — |
| `decided_by` | `uuid` | NULL, FK `core.person(id)` ON DELETE SET NULL | — |
| `decision_note` | `text` | NOT NULL DEFAULT '', `<= 500` | personal |

Partial unique index `data_request_open_idx ON (person_id) WHERE status = 'open'` (one open request per person; a race answers `Conflict duplicate` through `*db.Error.Constraint`). A fulfilled request disappears with its person; the trail is `datarights.request.fulfilled` and `datarights.delete`.

**Personal data never leaves this table except:** to the person; to holders of `people.data.delete` (audited); in `Export`. Nothing here goes to a log, span, metric, signal, error message or `Reason`.

## Permissions

| Permission | Allows | Scoped by | Reaches | Starter roles (`StarterRoles`) |
|---|---|---|---|---|
| `people.data.export` | `Export` of any person | site | sensitive (the export spans every module) | `account-admin` (09 §11) |
| `people.data.delete` | `Delete`, `ListRequests`, `DeclineRequest`, `FulfilRequest`; the requests screen | site | sensitive | `account-admin` (09 §11) |

Declared by datarights; the names do not change (boundaries decision 12). Own records need no permission: own `Export`, own requests, all through `Options.Actor`. `people` still asks `Allow` for these two names to fill its `Capabilities.Export` and `Capabilities.Delete`.

## Signals

- Published: `datarights.deletion-requested` `{request: RequestID, person: PersonID}` by `RequestDeletion`, inside its transaction (delivered after commit). It replaces `people.deletion-requested` (signals' owner rule: the publisher segment must equal the scope's owner). No subscriber exists in the core or `volunteers`; the notice is sent directly (decision 8).
- Published on its behalf: `people.person-deleted`, by the people source inside `Delete`'s transaction (people owns the name).
- Subscribed: none.

## Settings

None. The notice and the cooldown are not configurable.

## UI

None of its own. The screens stay in `web/src/core/people/`: "Your data" on My profile (download with the cooldown shown as a toast on 429; "Request deletion"; an open request with "Withdraw"; a declined one with the decision note), the person page's "Export data" (`Can.Export`) and "Delete person" (`Can.Delete`; a confirm dialog that repeats the playa name and lists what will happen; the report afterwards), and Deletion requests (`/people/admin/requests`, `people.data.delete`: tabs open / declined / withdrawn; cards with playa name, date, note; "Fulfil" and "Decline"; empty "No open requests"; read-only copy: decisions disabled). `YourData.tsx` and `screens/Requests.tsx` call `/api/datarights/...` (step 12); their states are `people.md`'s UI section.

## Behavior

- **Actor and permissions.** Each use case takes the actor from `Options.Actor` (`Unauthenticated` when absent, restricted, or inside `signals.InDelivery`), decides self versus other, then asks authz `Allow`. The superuser passes authz (D23) and is audited like anyone.
- **Audit.** Changes record inside their `Write` transaction (`Record` ref kinds stay `people.person` and `people.request`, only the actions were renamed; `Subject` the person); a `Record` error fails the use case (audit decision 3).
  - `Export` records `datarights.export` (`Level: sensitive`, `Fields: ["all"]`) before writing the body; a self export too.
  - `ListRequests` records one `datarights.request.read` per returned request that carries a note and is not the actor's own (`Subject` the requester), after loading and before returning, outside any `Read` transaction (D37, D38 (u)).
  - `Reason` is a token (`request:<id>`, `command`, `admin`), never free text.
- **Export without a row (D37).** If `People.Exists` answers `NotFound` but `Accounts.EmailOf` finds an account for the ID, `Export` still calls identity, access and each source and returns a document with `person` absent; `NotFound` only when no account exists either.
- **Transactions.** Requests: one `Write` transaction each, publishing inside it. `Export`: no transaction; the sources are called in turn (people, requests, identity, access, then the modules in build order, files, notify: the old order, which the code keeps); a failing source fails the export (a partial export would be mistaken for complete).
- **Deletion, step by step** (`Delete`, `FulfilRequest`, `DeleteCommand`): the shared person-deletion advisory lock (the key `people.delete:` + person, which `people`'s `UpdateMe` and `LinkAccount` also take; both packages use the same string and a test pins it) serialises `RequestDeletion` (the lock, then `People.Exists`); `Delete` takes its serialisation from the person row, locked inside `People.DeletePerson` after the modules have run; check the person (`People.Exists`; `NotFound`, unless the person has no row but `Accounts.EmailOf` finds an account for the ID: then the source, access and identity steps still run, people's own delete is a no-op and no `people.person-deleted` is published, D37);
  - for each module `Source` in build order call `DeletePerson` with the transaction's ctx (nested `Transact` joins, db decision 6), wrapping an error with the source name; then files, then notify;
  - `Access.DeletePerson` (`Conflict last_access_admin` ends it); `Accounts.DeletePerson` (`Permission` for a superuser account ends it; `Conflict protected` for a shared demo account);
  - delete the person's request rows, then `People.DeletePerson` (the person row, contacts and consents; a foreign-key violation from a module that forgot a table becomes `Conflict in_use` naming the constraint);
  - `AuditLog.CountAbout` for `Kept`; record `datarights.delete` (`Reason` token, `Record` ref the person) and `datarights.request.fulfilled` when applicable; commit.
  - The report is returned to the caller and never logged (counts only on the span). Idempotency: a second call answers `NotFound`.
- **Deletion-request notice (decision 8).** `RequestDeletion`, after the request row and `datarights.request.created` are written, inside the same `Write` transaction: `Holders.HoldersOf(ctx, "people.data.delete")` (site-wide explicit grant holders, superusers excluded; the base role is not enumerated, so a permission only the base role carries yields nobody: authz amendment, D40), then `Notify.Send(Message{Template: "datarights.deletion-requested", To: holders, Key: "deletion-requested:" + requestID, Link: "/people/admin/requests"})` with no `Vars`.
  - The body says only "A deletion request is waiting"; it names no person and carries no note.
  - No holders: nothing is sent and one info log line records the count (never a person). `Send` answering `Unsupported` (e-mail not configured) means "no e-mail": one info log line, the request still succeeds.
  - Delivery (accepted D40): the notice sends in batches of `notify.MaxRecipients` under one `Key`. `Notify.Configured` is probed once before the loop, and the whole notice is skipped (one info log line) if it is off. An `Unsupported` partway through the loop rolls the transaction back as `Unavailable`. Any other `Send` or `HoldersOf` error fails the request with its kind and rolls the transaction back (nothing is half-recorded).
- **Linked sources (D39, D44).** `app`'s `link` registers the modules' `PersonData`; notify, files, access, identity and people are `Options` fields, so their `ExportPerson` and `DeletePerson` run inside datarights' transaction like a module's. The export document gains `"files": {...}` (files.md Behavior: `about` with content, `uploaded` metadata only); the deletion report's `Modules["files"]` carries `Anonymized` (the `uploaded_by` references cleared). A module's `DeletePerson` must delete its files whose subject is the person; otherwise files' `DeletePerson` answers `Conflict in_use` naming the module and the whole deletion rolls back (F70). The bytes leave the store within the hour (`files.reap`).
- **Read-only copy.** `Export`, `MyRequests`, `ListRequests` work (audit entries spool). Every write fails `Unavailable` wrapping `db.ErrReadOnly` (`503 read_only`).
- **Cooldown.** Self exports: an in-memory map person → last export time, bounded by `MaxTrackedExports` (oldest evicted), per process; within `ExportCooldown` the call fails `Unavailable` wrapping `httpx.ErrRateLimited` (429, `Retry-After`). Admin exports and the command are not limited. The cooldown is kept on `Canceled` and once the audit entry is written.
- **Concurrency.** The service holds no mutable state beyond the cooldown map (mutex) and the `Link` slice (set once).
- **Lifecycle.** `New` builds; `Link` once; nothing to start or stop; no goroutines.
- **Telemetry.** A span per use case (`datarights.<name>`) and the counters `bms.datarights.exports{outcome}` and `bms.datarights.deletions{outcome}`, with `telemetry.ID("person_id")` and `telemetry.Token` outcomes; never a name, address, phone or note on any of them (`telemetrytest` asserts).
- **Commands.** The `DeleteCommand` report has one line per part. `export` and `delete` exit 2 only for a malformed person ID, and 1 otherwise.

**Export document** (`ExportFormat` 1), one JSON object:

```json
{
  "format": 1,
  "exported_at": "...",
  "person": { "id": "...", "playa_name": "...", "pronouns": "...", "language": "...", "legal_name": "...", "phone": "...", "contact_email": "...", "name_prompted_at": "...", "created_at": "...", "updated_at": "..." },
  "emergency_contacts": [ { "name": "...", "phone": "...", "relationship": "...", "position": 1 } ],
  "consents": [ { "purpose": "...", "granted": true, "changed_at": "..." } ],
  "requests": [ { "kind": "delete", "status": "declined", "note": "...", "created_at": "...", "decision_note": "..." } ],
  "identity": { "account": { } },
  "access": { },
  "modules": { "<name>": { } }
}
```

`person`, `emergency_contacts` and `consents` come from the people source; `requests` from datarights' own table; `identity` and `access` are the raw messages those services return; `modules` holds each module's `Export.Data` by name (an empty object for a module holding nothing). Audit rows are not included (audit "Export of a person").

**Deletion report** (JSON and the command's lines): `{"person": "...", "people": n, "identity": n, "access": n, "modules": {"<name>": {"deleted": n, "anonymized": n, "kept": [...]}}, "kept": [{"kind": "audit.entry", "count": n}]}`.

## Failure modes

The rows below were people.md's; the F-numbers and the test names are kept. G rows are new with the split.

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F11 | `Export`, `Delete` | unknown person (no row, no account) | NotFound | not-found state | `TestDelete_UnknownPerson_ReturnsNotFound`, `TestExport_UnknownPerson_ReturnsNotFound` (people's F11 covers the profile methods) |
| F28 | `Export` (self) | within `ExportCooldown` | Unavailable wrapping `httpx.ErrRateLimited` | 429 toast | `TestExport_SelfWithinCooldown_ReturnsRateLimited` |
| F29 | `Export` (other) | no `people.data.export` | Permission | hidden button | `TestExport_NoPermission_ReturnsPermission` |
| F30 | `Export` | a source fails | the source's kind, wrapped with its name | error state; nothing written | `TestExport_ModuleFails_FailsWhole` |
| F31 | `Export`, `Delete` | before `Link` | Internal | 500 | `TestDelete_BeforeLink_ReturnsInternal`, `TestCommands_BeforeLink_*` |
| F32 | `Delete`, `FulfilRequest` | no `people.data.delete` | Permission | hidden | `TestDelete_NoPermission_ReturnsPermission` |
| F33 | `Delete`, `FulfilRequest` | the person is the actor | Invalid (`person` `self`) | message | `TestDelete_Self_ReturnsInvalid` |
| F34 | `Delete` | authz refuses (last access admin) | Conflict (`last_access_admin`) | message; nothing deleted | `TestDelete_LastAccessAdmin_RollsBack` |
| F35 | `Delete` | identity refuses (superuser account) | Permission | message; nothing deleted | `TestDelete_SuperuserPerson_RollsBack` |
| F36 | `Delete` | a module's `DeletePerson` fails | the module's kind, wrapped with its name | message; nothing deleted | `TestDelete_ModuleFails_RollsBack` |
| F37 | `Delete` | a foreign key still references the person | Conflict (`in_use` + constraint) | message naming the constraint | `TestDelete_ForeignKeyLeft_ReturnsConflict` (integration) |
| F38 | `Delete` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | 503 read_only | `TestWrites_ReadOnly_ReturnUnavailable` |
| F39 | `Delete` | `Record` fails | the recorder's kind; transaction rolled back | error | `TestDelete_AuditFails_RollsBack` |
| F40 | `RequestDeletion` | an open request exists | Conflict (`duplicate`) | show the open one | `TestRequestDeletion_Open_ReturnsConflict` |
| F41 | `RequestDeletion`, `DeclineRequest` | note too long | Invalid (`note` `too_long {max}`) | field error | `TestRequestDeletion_LongNote_ReturnsInvalid` |
| F42 | `WithdrawRequest` | not own, or not open | NotFound / Conflict (`status`) | refresh | `TestWithdrawRequest_Table` |
| F43 | `DeclineRequest`, `FulfilRequest` | not open | Conflict (`status`) | refresh | `TestDeclineRequest_NotOpen_ReturnsConflict` |
| F44 | `ListRequests`, `DeclineRequest` | no `people.data.delete` | Permission | hidden | `TestListRequests_NoPermission_ReturnsPermission` |
| F50 | `ExportCommand`, `DeleteCommand` | malformed ID | Invalid | app exits 2 | `TestCommands_BadID_ReturnsInvalid` |
| F57 | `ListRequests` | a returned request has a note | nil; one `datarights.request.read` per returned request with a note, `Subject` the requester; `Record` error fails the read (D37, D38 (u)) | none | `TestListRequests_WithNote_RecordsRead` |
| F58 | `Export`, `Delete` | no person row, an account exists | nil; identity, access and module steps run (D37) | none | `TestDelete_AccountWithoutPersonRow_RunsIdentityAndAuthz` |
| F59 | `Delete` | the person held grants | nil; authz publishes `access.grant-revoked` per grant (D37) | none | authz's F66 test `TestDeletePerson_PublishesGrantRevokedPerGrant` (`internal/core/authz/integration_person_test.go`); datarights' own tests use a fake `Access` |
| F60 | `RequestDeletion` | `Holders` or `Notify` answers `Unsupported` (e-mail not configured) | nil; the request is created; one info log | nothing | `TestRequestDeletion_NoEmail_RequestStillCreated` |
| F61 | `RequestDeletion` | `HoldersOf` returns nobody | nil; request created; nothing sent; one info log | nothing | `TestRequestDeletion_NoHolders_NothingSent` |
| F62 | `RequestDeletion` | `HoldersOf` or `Send` fails other than `Unsupported` | its kind; the transaction rolls back; no request row | retry | `TestRequestDeletion_NotifyFails_RollsBack` |
| F63 | `RequestDeletion` | success with holders | the holders are `To`, `Key` is the request ID, `Link` is `/people/admin/requests`, no `Vars` (asserted from `notifytest.Fake.Sent()`) | nothing | `TestRequestDeletion_SendsNoticeToHolders` |
| F64 | `RequestDeletion` | the request is a duplicate | `Conflict`; nothing sent | show the open one | `TestRequestDeletion_Duplicate_NothingSent` |
| F70 | `Delete` | a module left a file about the person | Conflict (`in_use`, naming the module) | fix the module's `DeletePerson` | `TestDelete_ModuleLeftFile_ReturnsConflictInUse` |
| F89 | `Delete`, `FulfilRequest`, `DeleteCommand` | identity refuses with `Permission` `protected` (a shared demo account) | Conflict `protected`; nothing deleted | message | `TestDelete_ProtectedAccount_ReturnsConflict_RollsBack` |
| G1 | `New` | a nil dependency | Invalid (`datarights.<dep>`) | app exits 2 | `TestNew_NilDependency_ReturnsInvalid` |
| G2 | `Link` | called twice; a duplicate source name or a nil source | Conflict; Invalid | app exits 1 | `TestLink_Twice_ReturnsConflict`, `TestLink_DuplicateName_ReturnsInvalid` |
| G3 | `Delete` | the sources run in the wrong order | a programming error | fix the order | `TestDelete_OrderIsModulesFilesNotifyAccessIdentityPeople` (fakes record the order) |
| G4 | `PeopleSource.DeletePerson` | a module still references the person | Conflict (`in_use`) | fix the module | `TestDeletePerson_ModuleStillReferences_ReturnsConflict` (in `people`) |
| G5 | `RequestDeletion` | success | publishes `datarights.deletion-requested {request, person}` | none | `TestPublishes_DatarightsDeletionRequested` |

## Lock-in check

None: PostgreSQL through `db`, the sources through Go interfaces. `pg_advisory_xact_lock` is in every OSI-licensed PostgreSQL ≥ 16 (db decision 20).

## Test plan

- **Unit** (`internal/core/datarights`, no infrastructure): a hand-written fake store; `clock.NewFake`; `audittest.NewFake("datarights")`; `signalstest.NewFake("datarights", {deletion-requested}, {})`; `authztest.NewFake()`; fakes for `Accounts`, `Access`, `Outbox`, `Files`, `People`, `AuditLog`, `Holders` and `PersonData` sources; `notifytest.NewFake("datarights")`; `telemetrytest.New(t)` asserting no name, address, phone, requester ID or note text appears in logs, spans, the message or the recorder's `Reason`/`Fields`. Every F and G row.
- **Permission tests:** each use case has `_NoPermission_ReturnsPermission`.
- **Integration** (`//go:build integration`, `dbtest.TempDB`): the deletion transaction across a fake module table with a foreign key (F37; F34 rollback leaves every row); the open-request partial index under two concurrent requests; the advisory lock under two concurrent deletions (one `NotFound`); read-only paths with `SetReadOnly`; `person_commands_integration_test`.
- **Conformance:** every module's `moduletest.Conformance` keeps passing because `module.PersonData` and the other three are aliases of this package's types.
- **app end-to-end:** `bms export <id>` prints the document with `hello` inside; the admin deletes a person who saved a hello greeting and the report counts it; `bms delete` on the last access admin's person exits 1 with `last_access_admin`; the Mailpit run files a request and reads the notice.
- **Web** (`web/src/core/people/`): `YourData.test.tsx` and `Requests.test.tsx` call the `/api/datarights/...` paths; every state of the two screens.

## Open questions

None. The questions the split raised were settled by the step-12 code (boundaries.md, Amendments, S12): `AuditLog` is `CountAbout` only; the request types and `DeletionReport` live here; telemetry, template and OpenAPI names follow the area (operationIds `datarights*`, schemas `Datarights*`, tag `datarights`).

## Proposed changes elsewhere

None beyond `boundaries.md`'s list (items 2, 9, 14, 15, 23 and 24 apply this spec's text to `07-abstractions.md`, `go-conventions`, `people.md`, `module-contract.md` and `api.md`).

## Amendments

None yet.
