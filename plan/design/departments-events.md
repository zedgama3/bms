# Departments and events

- **Status:** Implemented (2026-10-18; approved D37)
- **Amended 2026-10-19 (D39, owner approval of settings):** Proposed change 11 applied: the new-event form reads `regional.timezone` from `GET /api/settings/public` and falls back to the browser's zone when it is empty; the terms arrive as `term_department`, `term_event` and the `_plural`, `_title` and `_plural_title` forms. No behaviour of the services changes.
- **Package:** `internal/core/departments`, `internal/core/events`, `internal/contracts/eventsetup`   **UI:** `web/src/core/departments/`, `web/src/core/events/`
- **Depends on specs:** model, lib-errs, lib-core, db, signals, telemetry, audit, identity (the actor), authz (the `Authorizer`, `DepartmentSource`, starter roles), api (`httpx`), web-shell, app (parts), module-contract (`Deps`, extension points)   **Used by:** authz (`DepartmentSource`, the grant screens), every module from Phase 1 (`volunteers` first: `Deps.Departments`, `Deps.Events`, the `events.event-setup` point, the signals), the admin screens (09 §7)

## Summary

**Purpose.** Keep the two reference records every module attaches its data to by ID (D19): the regional's standing departments and the events it puts on (D24). Two packages, two core parts, one spec. `departments` (`internal/core/departments`): the departments, their details, archiving, the `Directory` and authz's `DepartmentSource`. `events` (`internal/core/events`): the events, their kinds, dates and zone, the schedule inside an event, closing, the `Directory`, and the `events.event-setup` extension point (`contracts/eventsetup`) that lets modules copy what they need into a new event. Both import `model`, `lib`, `core/db`, `core/audit`, `core/authz` (Authorizer and leaf types), `core/signals`, `core/telemetry`; `events` also `contracts/eventsetup`. Never `core/module`, `core/identity` (the actor arrives as `authz.ActorFunc`), `api`, `app`, `config` or a module. Leadership is not a table here: it is a department-scoped grant of `department-admin` (authz; 09 §5).

**Exported (`departments`).**
- `Department{ID model.DepartmentID, Name, Description string, ContactEmail model.Email, Archived bool, ArchivedAt, CreatedAt, UpdatedAt time.Time}` (+`Validate`); `CreateInput{Name, Description, ContactEmail string}`; `UpdateInput{Name, Description, ContactEmail *string}` (nil = unchanged); `ListQuery{IncludeArchived bool}`.
- `Directory` (= `module.Deps.Departments`, proposed): `NamesOf(ctx, []model.DepartmentID) (map[model.DepartmentID]string, error)` (archived included), `Active(ctx, model.DepartmentID) (bool, error)` (false for unknown or archived; also `authz.DepartmentSource`).
- `Options{DB, Clock, Logger, Tracer, Meter, Audit audit.Recorder (scoped "departments"), Signals signals.Bus (scoped "departments"), Authz authz.Authorizer, Actor authz.ActorFunc}` (+`Validate`); `New(Options) (*Service, error)` (no I/O; works against `db.None()`).
- `*Service` (safe for concurrent use; nothing to close): `Directory` + `List`, `Get`, `Create`, `Update`, `Archive`, `Restore`, `Routes() []Route`, `OpenAPI() []byte`; `Route{Method, Pattern string, Handler http.Handler}` (all signed-in; `app` converts).
- Constants `MaxNameBytes = 100`, `MaxDescriptionBytes = 2000`, `MaxNames = 200`. Permissions `departments.manage` (site, internal; starter `department-steward`), `departments.details.manage` (department-scopable, internal; starter `department-admin`, `department-steward`); `Permissions []authz.PermissionDecl`. Signals `departments.department-created|archived|restored {department}`. Audit actions `departments.department.created|changed|renamed|archived|restored`. `Set = db.Set{Name: "departments", Schema: "core"}`, `postgres.Migrations`. `departmentstest.NewFake()` (`Add(id, name)`, `Archive(id)`).

**Exported (`events`).**
- `Kind{Key, Name string, Builtin bool, CreatedAt, UpdatedAt}`; `KindBurn = "burn"`, `KindDecompression = "decompression"`, `KindOther = "other"`; `KindInput{Key, Name string}`.
- `Event{ID model.EventID, Name, Kind string, Dates model.TimeRange, Zone string, Closed bool, ClosedAt, CreatedAt, UpdatedAt}` (+`Validate`); `ScheduleItemID`; `ScheduleItem{ID, Event, Key, Label string, Starts, Ends time.Time, Position int}` (`Ends` zero = a moment); `Detail{Event, Schedule []ScheduleItem}`; `CreateInput{Name, Kind string, Dates model.TimeRange, Zone string, CopyFrom model.EventID}`; `UpdateInput{Name, Kind, Zone *string, Dates *model.TimeRange}`; `ScheduleInput{Key, Label string, Starts, Ends time.Time}`; `ListQuery{Status}` (`StatusAny`, `StatusOpen`, `StatusClosed`); `Created{Event, Setup []SetupResult}`; `SetupResult{Module string, Items int, Failed bool, Code string}`; `Info{ID, Name, Kind, Dates, Zone, Closed}`.
- `Directory` (= `module.Deps.Events`, proposed): `InfoOf(ctx, []model.EventID) (map[model.EventID]Info, error)`.
- `SetupProvider{Module string; eventsetup.Provider}`; `Options{DB, Clock, Logger, Tracer, Meter, Audit (scoped "events"), Signals (scoped "events"), Authz, Actor, SetupTimeout time.Duration}` (+`Validate`); `New(Options) (*Service, error)`; `(*Service).Link(providers []SetupProvider) error` (once, from `app`'s `link`).
- `*Service`: `Directory` + `List`, `Get`, `Create`, `Update`, `Close`, `Reopen`, `RunSetup`, `ListKinds`, `CreateKind`, `UpdateKind`, `AddScheduleItem`, `UpdateScheduleItem`, `RemoveScheduleItem`, `Routes`, `OpenAPI`.
- Constants `MaxNameBytes = 100`, `MaxKindKeyBytes = 32`, `MaxKindNameBytes = 60`, `MaxLabelBytes = 100`, `MaxZoneBytes = 64`, `MaxScheduleItems = 100`, `MaxEventDuration = 366 * 24h`, `MaxInfo = 200`, `DefaultSetupTimeout = 10s`. Permission `events.manage` (site, internal; starter `event-admin`); `Permissions`. Signals `events.event-created {event, copied-from?}`, `events.event-changed`, `events.event-closed`, `events.event-reopened {event}`. Extension point `events.event-setup`. Audit actions `events.event.created|changed|closed|reopened`, `events.kind.created|changed`, `events.schedule-item.created|changed|deleted`, `events.setup.ran`. `Set = db.Set{Name: "events", Schema: "core"}`, `postgres.Migrations`. `eventstest.NewFake()` (`Add(Info)`).
- `eventsetup` (contract): `Point = "events.event-setup"`, `Request{Event, CopyFrom model.EventID}`, `Result{Items int}`, `Provider{SetUp(ctx, Request) (Result, error)}`.

**Key decisions.** 1 two packages and two `app` parts named `departments` and `events` (both reserved core areas, so `module.IsCoreArea` passes; app F28, not amended), one spec file (row 9); jobs, notify, files become parts 8-10 (owner, 2026-10-18, D37): everything else (migration sets, route sets, audit areas, signal scopes, screen sets) was already per area. 2 active departments and all events are `internal` data readable by any signed-in person without a permission, like `people.NamesOf` (owner, 2026-10-18, D37): modules label records by them everywhere, and a removable permission would break those labels. 3 archived departments are visible only to holders of `departments.manage` (any list) and, one at a time, to holders of `departments.details.manage@D`; to anyone else `Get` is `NotFound`; `NamesOf` keeps answering for archived departments so history stays labelled. 4 archiving leaves grants in place (restore keeps the leadership; authz Proposed change 10), flips `Active` to false (authz refuses new department grants; modules refuse writes), and is reversible; departments are never deleted. 5 leadership = granting `department-admin` at D through authz (`access.department-grants.manage` for the steward, `access.grants.manage@D` for the department's own admins); no leads table, no leadership route; the department page links to `/access/grants?department=D`. 6 renaming needs `departments.manage` (history and module labels change), details need `departments.details.manage@D`; one `Update` decides by the fields set. 7 a department's contact address is a shared mailbox, `internal`, never a person's address (the form says "the department's shared mailbox, never a person's address"; owner, 2026-10-18, D37). 8 an event has instants (`TimeRange`) plus an IANA zone, validated with `time.LoadLocation` (app embeds `time/tzdata`); the UI renders in the event's zone (model Proposed change 8). 9 event kinds are rows a regional can add and rename (`burn`, `decompression`, `other` seeded, `Builtin`; the seeded three keep catalogue keys for translation); never deleted (owner, 2026-10-18, D37). 10 the schedule inside an event is a list of items (a window or a moment, an optional token `Key` unique per event such as `temple-burn`) with no containment rule: the screen warns when an item falls outside the dates; schedule items ship in M3 (owner, 2026-10-18, D37). 11 closing is a flag (`closed_at`), reversible with `Reopen`, audited; a closed event accepts no changes (`Conflict closed`); modules decide what "closed" means for them (volunteers: no new sign-ups). 12 `Create` with `CopyFrom` commits the event first, then calls every `events.event-setup` provider in build order, each under `SetupTimeout`, in the caller's ctx (the provider checks the caller's permission); a provider failure never undoes the event, it is reported in `Created.Setup`, counted, and the admin can `RunSetup` again; providers are idempotent per (event, module). 13 the signal `events.event-created` and the point are the same occurrence seen two ways: the point for work with a visible result, the signal for reactions. 14 no pagination on either list: a regional has tens of departments and events. 15 names are unique ignoring case among all departments and among all events (archived and closed included), so a restore never collides. 16 nothing here is personal data: no `PersonData`, no export, nothing to delete; audit holds who changed what. 17 regional differences are data (department names, kinds, zones, leadership terms through grant end dates): no settings owned. 18 no event scope in authz (authz decision 18): a module scopes by department and filters by event itself. 19 the department page's Leadership card shows only to holders of `access.grants.read` (and department admins at that department) in M3; a participant sees the contact mailbox; a `HoldersOf(ctx, dept, roleKey)` seam in authz is Later (owner, 2026-10-18, D37); `department-steward` now holds site-wide `access.grants.read`, so stewards see the card at every department (D38, supersedes D37(q)). 20 (D38) `RunSetup` checks in the order actor, input, permission. 21 (D38) `events.setup.ran` is recorded with `context.WithoutCancel(ctx)` under a 5s deadline, so an expired request deadline does not lose it. 22 (D38) a provider returning `Timeout` counts as metric outcome `timeout`; one that returns success after its deadline passed is reported as success with its items; a panic is logged once, with its stack. 23 (D38) an empty zone fails `required`, a non-IANA zone `malformed`. 24 (D38) kind names may repeat (only the key is unique); a builtin kind shows its catalogue label until renamed, then its stored name everywhere. 25 (D38) the kind audit `Ref` is a fixed ID from sha256(`events.kind:`+key) truncated to 16 bytes, `Reason` `key:<key>`. 26 (D38) `RunSetup` with no provider linked records nothing. 27 (D38) deferred: (af) to `settings` in M4; (ai) until a module contributes set-up.

**Errs kinds callers handle.** Every use case: `Unauthenticated` (no actor; `NamesOf`, `Active`, `InfoOf` excepted), store kinds (`Unavailable`, `Timeout`, `Canceled`), `Unavailable` wrapping `db.ErrReadOnly` on writes. `Permission` (`departments.manage`, `departments.details.manage@D`, `events.manage`). `NotFound` (unknown ID; archived department outside the caller's reach). `Invalid` with field codes `required`, `too_long {max}`, `control_chars`, `malformed`, `range`, `unknown`, `self`, `too_many {max}`, `one_of`. `Conflict` with codes `taken` (name, kind key), `archived`, `not_archived`, `closed`, `open`, `duplicate` (schedule key). `Link`: `Internal`. `New`/`Options.Validate`: `Invalid` with fields `departments.*` / `events.*`. No sentinels.

**Owns.** Tables `core.department`; `core.event_kind`, `core.event`, `core.event_schedule_item` (all `internal`, kept forever). The three permissions, seven signals, one extension point, the audit actions above; route sets `departments` (`GET|POST /`, `GET|PUT /{department}`, `POST /{department}/archive|restore`) and `events` (`GET|POST /`, `GET|POST /kinds`, `PUT /kinds/{kind}`, `GET|PUT /{event}`, `POST /{event}/close|reopen|setup`, `POST /{event}/schedule`, `PUT|DELETE /{event}/schedule/{item}`); screens `/departments`, `/departments/new`, `/departments/:department`, `/events`, `/events/new`, `/events/:event`, `/events/kinds`; keys `core.departments.*`, `core.events.*`; spans `departments.<use-case>`, `events.<use-case>`, `events.setup`; metric `bms.events.setup{module,outcome}`. No settings, no config keys.

**Expects of dependencies.** `app`: parts 6 `departments` and 7 `events` (jobs, notify, files move to 8–10); `lazyDepartments{a}` as authz's `Departments`; `events`' `link` over `a.set.Contributors(eventsetup.Point)`; `import _ "time/tzdata"`. `authz`: `Allow`, `AllowIn`, `DepartmentSource` semantics (`false, nil` for unknown and archived), `StarterRoles` seeding. `audit`: a `Recorder` per area; `Subject` may be zero when `Record` is set. `signals`: scoped `Bus`; `InDelivery`. `db`: `Transact`, `AfterCommit`, `Wrap`, `*Error.Constraint`, `ErrReadOnly`. `module-contract`: `Deps.Departments`, `Deps.Events`, `Env.CoreExtensionPoints` with `events.event-setup`, fakes in `moduletest`. `web-shell`: `useFormat` zone rendering, `NavEntry.access`. `people`: nothing (the grant screens already use its names route).

**Open questions:** none; the six were answered 2026-10-18 (D37) and are folded into decisions 1, 2, 7, 9, 10 and 19; the D38 follow-ups (2026-10-19) are decisions 19 and 20 to 27. **Proposed changes elsewhere:** 13, listed at the end; 12 applied 2026-10-18 (D37), item 11 (`settings.md`) applied 2026-10-19 (D39).

## Purpose

Keep the reference records every module attaches its data to by ID.

## Responsibilities

- **Departments.** The regional's standing departments (D24, Q-CORE9): name, description,
  contact mailbox; creating, renaming, editing details, archiving and restoring; listing
  them for everyone and labelling module records by ID (`NamesOf`); telling authz whether a
  department is active (`DepartmentSource`).
- **Events.** The gatherings the regional puts on (D24, Q-CORE13): name, kind, start and
  end instants with the event's zone, the schedule inside the event (gate hours, the temple
  burn), closing and reopening; listing them for everyone; labelling module records
  (`InfoOf`); calling modules to set up a new event (`events.event-setup`).
- **Event kinds.** The named kinds (`burn`, `decompression`, `other` seeded) a regional can
  extend and rename.
- **Signals** so modules react to a department being archived or an event changing state.
- **The M3 screens** for all of the above, phone first.

## Not responsible for

| Concern | Owner |
|---|---|
| Who leads a department (the grant), appointing and removing leads, leadership terms | `authz`: a department-scoped grant of `department-admin`, with an optional end date (Q-CORE11); the steward uses `access.department-grants.manage` |
| Who belongs to a department, positions (Khaki, R12), qualifications, shifts | `volunteers` (Phase 1), by `DepartmentID` and `EventID` |
| Work around an event (land prep, build, strike, inventory, training, work weekends) | the module that runs it (Q-CORE10); an activity may name an event or none |
| What "closed" forbids inside a module | the module (decision 11) |
| Anything about a person; export and deletion of personal data | nothing personal is stored here (decision 16); `people` orchestrates the rest |
| Terminology ("department", "event" in the regional's words), the regional's default zone | `settings` (D28); until then the catalogue defaults and the browser's zone |
| Deciding permissions | `authz` (`Allow`, `AllowIn`) |
| Recording changes | `audit`, through the scoped `Recorder` |
| A page section per module on a department page | Later: a `departments.department-page` point modelled on `people.person-page`, designed when `volunteers` needs it (no consumer today) |
| Map grids, camp numbers, site plans | `registrations` or a later module |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `departments` | Hold the regional's standing departments, with their archiving. | package `internal/core/departments`, part 6, set `departments`, route set `departments`, screen set `departments` |
| `events` | Hold the events the regional puts on, with their dates, schedule and kinds. | package `internal/core/events`, part 7, set `events`, route set `events`, screen set `events` |
| `eventsetup` | Let a module set up its data for a new event, with a visible result. | contract `internal/contracts/eventsetup`, point `events.event-setup`, defined by `events` |

## Interface

### `departments`

```go
package departments

// Department is a standing part of the regional. Nothing in it is personal data:
// ContactEmail is the department's shared mailbox (decision 7).
type Department struct {
	ID           model.DepartmentID `json:"id"`
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	ContactEmail model.Email        `json:"contact_email,omitzero"`
	Archived     bool               `json:"archived"`
	ArchivedAt   time.Time          `json:"archived_at,omitzero"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

// Validate checks the limits below. Fails Invalid with FieldErrors on name,
// description, contact_email.
func (d Department) Validate() error

// CreateInput creates a department. ContactEmail may be empty.
type CreateInput struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	ContactEmail string `json:"contact_email"`
}

// UpdateInput changes a department; nil means unchanged. Setting Name needs
// departments.manage; the others departments.details.manage at the department.
type UpdateInput struct {
	Name         *string `json:"name"`
	Description  *string `json:"description"`
	ContactEmail *string `json:"contact_email"`
}

// ListQuery filters List. IncludeArchived needs departments.manage.
type ListQuery struct {
	IncludeArchived bool
}

// Directory is what modules get as Deps.Departments. Neither method needs an actor:
// both return internal data and are called from signal handlers and jobs.
type Directory interface {
	// NamesOf maps each known ID (archived included) to its name; unknown IDs are
	// absent. Fails Invalid (ids: too_many) and store kinds.
	NamesOf(ctx context.Context, ids []model.DepartmentID) (map[model.DepartmentID]string, error)
	// Active reports whether the department exists and is not archived: (false, nil)
	// for unknown or archived. Also authz.DepartmentSource. Fails Invalid (zero id)
	// and store kinds.
	Active(ctx context.Context, id model.DepartmentID) (bool, error)
}

// Options builds a Service. Audit and Signals are scoped "departments" by app.
type Options struct {
	DB      db.DB
	Clock   clock.Clock
	Logger  *slog.Logger
	Tracer  telemetry.Tracer
	Meter   telemetry.Meter
	Audit   audit.Recorder
	Signals signals.Bus
	Authz   authz.Authorizer
	Actor   authz.ActorFunc
}

// Validate fails Invalid naming the missing field (departments.db, departments.clock,
// departments.logger, departments.tracer, departments.meter, departments.audit,
// departments.signals, departments.authz, departments.actor).
func (o Options) Validate() error

// New validates and returns the service. No I/O. Fails Invalid.
func New(o Options) (*Service, error)

// Service is safe for concurrent use and has nothing to close.
type Service struct{ /* unexported */ }

// List returns active departments by name, plus archived ones when asked.
// Fails Unauthenticated, Permission (IncludeArchived without departments.manage),
// store kinds.
func (s *Service) List(ctx context.Context, q ListQuery) ([]Department, error)

// Get returns one department. An archived department is NotFound unless the caller holds
// departments.manage or departments.details.manage at it. Fails Unauthenticated,
// NotFound, store kinds.
func (s *Service) Get(ctx context.Context, id model.DepartmentID) (Department, error)

// Create needs departments.manage. Records departments.department.created and publishes
// departments.department-created. Fails Unauthenticated, Permission, Invalid,
// Conflict (taken), Unavailable (read-only), store kinds.
func (s *Service) Create(ctx context.Context, in CreateInput) (Department, error)

// Update changes the fields set. Records departments.department.renamed and/or
// .changed. Fails Unauthenticated, Permission, NotFound, Invalid, Conflict (archived,
// taken), Unavailable (read-only), store kinds.
func (s *Service) Update(ctx context.Context, id model.DepartmentID, in UpdateInput) (Department, error)

// Archive needs departments.manage. Grants stay (decision 4). Records
// departments.department.archived; publishes departments.department-archived.
// Fails Unauthenticated, Permission, NotFound, Conflict (archived), Unavailable, store kinds.
func (s *Service) Archive(ctx context.Context, id model.DepartmentID) (Department, error)

// Restore is Archive's inverse. Conflict (not_archived).
func (s *Service) Restore(ctx context.Context, id model.DepartmentID) (Department, error)

// NamesOf and Active implement Directory (and authz.DepartmentSource).
func (s *Service) NamesOf(ctx context.Context, ids []model.DepartmentID) (map[model.DepartmentID]string, error)
func (s *Service) Active(ctx context.Context, id model.DepartmentID) (bool, error)

// Route is a route relative to /api/departments; app converts to module.Route
// (all signed-in). Routes and OpenAPI cannot fail.
type Route struct {
	Method  string
	Pattern string
	Handler http.Handler
}
func (s *Service) Routes() []Route
func (s *Service) OpenAPI() []byte

const (
	MaxNameBytes        = 100
	MaxDescriptionBytes = 2000
	MaxNames            = 200

	PermissionManage        authz.Permission = "departments.manage"
	PermissionDetailsManage authz.Permission = "departments.details.manage"

	SignalCreated  signals.Name = "departments.department-created"
	SignalArchived signals.Name = "departments.department-archived"
	SignalRestored signals.Name = "departments.department-restored"
	PayloadDepartment           = "department"
)

var (
	Permissions      []authz.PermissionDecl
	PublishedSignals []signals.Name
	Set              = db.Set{Name: "departments", Schema: "core", Files: postgres.Migrations}
)
```

`departmentstest.NewFake() *Fake`: a `Directory` with `Add(id model.DepartmentID, name string)`, `Archive(id)`, safe for concurrent use; `moduletest.Conformance` supplies it when `Deps.Departments` is nil (Proposed change 3).

**Per method: consumer, pre- and post-conditions.**

| Method | Consumer | Pre | Post |
|---|---|---|---|
| `List` | the departments screen; the grant screens' department filter and picker (authz UI); module screens' department pickers (through the route) | signed in; `IncludeArchived` needs `departments.manage` | sorted by `lower(name)`; archived rows carry `Archived: true` |
| `Get` | the department page; the grant screen's heading | signed in | decision 3 for archived |
| `Create` | the steward's "New department" | `departments.manage`; input valid | row exists; audit entry and signal committed together |
| `Update` | the department page's "Edit details" (admins) and "Rename" (steward) | at least one field set; permissions by field (decision 6); department active | `UpdatedAt` advanced; audit entry per kind of change |
| `Archive`, `Restore` | the steward's actions | `departments.manage`; state as required | `Active` flips; grants untouched; signal after commit |
| `NamesOf` | modules labelling records (export documents, notifications), the event and grant screens through modules' own routes | at most `MaxNames` IDs | unknown IDs absent; archived included |
| `Active` | authz's `Grant` (inside its `Write` transaction, joined through ctx); modules before writing a record for a department | non-zero ID | `(false, nil)` never leaks whether the ID exists versus is archived |
| `Routes`, `OpenAPI` | `app` part 6 | — | the fragment names exactly the routes (a test proves it) |

### `events`

```go
package events

// Kind is a named kind of event. The three seeded kinds are Builtin: the UI shows
// them through catalogue keys core.events.kind.<key> until renamed (then Name, D38);
// custom kinds show Name. Names may repeat; only Key is unique (D38).
type Kind struct {
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	Builtin   bool      `json:"builtin"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const (
	KindBurn          = "burn"
	KindDecompression = "decompression"
	KindOther         = "other"
)

// KindInput creates or renames a kind. Key is immutable after creation
// (ident grammar, at most MaxKindKeyBytes).
type KindInput struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Event is a gathering the regional puts on (D24). Dates are instants; Zone is the IANA
// name the dates are shown in.
type Event struct {
	ID        model.EventID   `json:"id"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	Dates     model.TimeRange `json:"dates"`
	Zone      string          `json:"zone"`
	Closed    bool            `json:"closed"`
	ClosedAt  time.Time       `json:"closed_at,omitzero"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Validate checks name, kind (shape only), dates (TimeRange.Validate, MaxEventDuration)
// and zone (time.LoadLocation). Fails Invalid with FieldErrors name, kind, dates.start,
// dates.end, zone.
func (e Event) Validate() error

type scheduleItem struct{}
// ScheduleItemID identifies a schedule item.
type ScheduleItemID = model.ID[scheduleItem]

// ScheduleItem is a window or a moment inside an event (gate hours, the temple burn).
// Key is an optional token unique within the event ("temple-burn"), for modules that
// anchor to it later; Ends zero means a moment.
type ScheduleItem struct {
	ID       ScheduleItemID `json:"id"`
	Event    model.EventID  `json:"event"`
	Key      string         `json:"key,omitempty"`
	Label    string         `json:"label"`
	Starts   time.Time      `json:"starts_at"`
	Ends     time.Time      `json:"ends_at,omitzero"`
	Position int            `json:"position"`
}

// Detail is an event with its schedule, sorted by Starts then Position.
type Detail struct {
	Event
	Schedule []ScheduleItem `json:"schedule"`
}

// CreateInput creates an event. CopyFrom (optional) names the event whose module data
// the events.event-setup providers copy (decision 12).
type CreateInput struct {
	Name     string          `json:"name"`
	Kind     string          `json:"kind"`
	Dates    model.TimeRange `json:"dates"`
	Zone     string          `json:"zone"`
	CopyFrom model.EventID   `json:"copy_from,omitzero"`
}

// UpdateInput changes an event; nil means unchanged.
type UpdateInput struct {
	Name  *string          `json:"name"`
	Kind  *string          `json:"kind"`
	Zone  *string          `json:"zone"`
	Dates *model.TimeRange `json:"dates"`
}

// ScheduleInput adds or replaces a schedule item.
type ScheduleInput struct {
	Key    string    `json:"key"`
	Label  string    `json:"label"`
	Starts time.Time `json:"starts_at"`
	Ends   time.Time `json:"ends_at,omitzero"`
}

// Status filters List.
type Status uint8

const (
	StatusAny Status = iota + 1
	StatusOpen
	StatusClosed
)

func ParseStatus(s string) (Status, error) // "any" | "open" | "closed"; Invalid otherwise

type ListQuery struct {
	Status Status // zero = StatusAny
}

// SetupResult is one provider's outcome. Code is the errs kind name when Failed
// ("permission", "timeout", "unavailable", ...), else "".
type SetupResult struct {
	Module string `json:"module"`
	Items  int    `json:"items"`
	Failed bool   `json:"failed"`
	Code   string `json:"code,omitempty"`
}

// Created is Create's answer: the event and what each provider did.
type Created struct {
	Event Event         `json:"event"`
	Setup []SetupResult `json:"setup"`
}

// Info is what modules get about an event.
type Info struct {
	ID     model.EventID
	Name   string
	Kind   string
	Dates  model.TimeRange
	Zone   string
	Closed bool
}

// Directory is what modules get as Deps.Events. Needs no actor.
type Directory interface {
	// InfoOf maps each known ID to its Info; unknown IDs are absent. Fails Invalid
	// (ids: too_many) and store kinds.
	InfoOf(ctx context.Context, ids []model.EventID) (map[model.EventID]Info, error)
}

// SetupProvider is a module's contribution to events.event-setup, named by app.
type SetupProvider struct {
	Module string
	eventsetup.Provider
}

// Options builds a Service. SetupTimeout bounds one provider call (1s..5m;
// zero = DefaultSetupTimeout).
type Options struct {
	DB           db.DB
	Clock        clock.Clock
	Logger       *slog.Logger
	Tracer       telemetry.Tracer
	Meter        telemetry.Meter
	Audit        audit.Recorder
	Signals      signals.Bus
	Authz        authz.Authorizer
	Actor        authz.ActorFunc
	SetupTimeout time.Duration
}

func (o Options) Validate() error // Invalid: events.db, ..., events.setup_timeout
func New(o Options) (*Service, error)

// Link gives the service the providers, once, in build order. Fails Internal when
// called twice, on a nil provider, an empty or duplicate module name.
func (s *Service) Link(providers []SetupProvider) error

type Service struct{ /* unexported */ }

// List returns events newest first (Dates.Start desc). Fails Unauthenticated, Invalid
// (status), store kinds.
func (s *Service) List(ctx context.Context, q ListQuery) ([]Event, error)

// Get returns an event with its schedule. Fails Unauthenticated, NotFound, store kinds.
func (s *Service) Get(ctx context.Context, id model.EventID) (Detail, error)

// Create needs events.manage. Commits the event (audit events.event.created, signal
// events.event-created), then runs the providers when CopyFrom is set and records
// events.setup.ran. Fails Unauthenticated, Permission, Invalid, Conflict (taken),
// Unavailable (read-only), store kinds. A provider failure is a SetupResult, not an error.
func (s *Service) Create(ctx context.Context, in CreateInput) (Created, error)

// Update needs events.manage; the event must be open. Records events.event.changed;
// publishes events.event-changed. Fails Unauthenticated, Permission, NotFound, Invalid,
// Conflict (closed, taken), Unavailable, store kinds.
func (s *Service) Update(ctx context.Context, id model.EventID, in UpdateInput) (Event, error)

// Close and Reopen need events.manage. Conflict (closed) / Conflict (open).
func (s *Service) Close(ctx context.Context, id model.EventID) (Event, error)
func (s *Service) Reopen(ctx context.Context, id model.EventID) (Event, error)

// RunSetup needs events.manage; the event must be open; from must exist and differ.
// Runs every provider again (they are idempotent) and records events.setup.ran (not
// when no provider is linked or none ran). Checks run in the order actor, input,
// permission (D38). If ctx ends partway, each provider not yet run is reported Failed
// (Code "timeout" or "canceled", Items 0), no metric is counted for it, events.setup.ran
// is recorded (Failed, Fields the failed modules) when at least one provider ran, and
// the ctx error is returned.
// Fails Unauthenticated, Permission, NotFound, Invalid (copy_from), Conflict (closed),
// Unavailable, store kinds.
func (s *Service) RunSetup(ctx context.Context, id, from model.EventID) ([]SetupResult, error)

// Kinds. ListKinds needs a session; the others events.manage.
func (s *Service) ListKinds(ctx context.Context) ([]Kind, error)
func (s *Service) CreateKind(ctx context.Context, in KindInput) (Kind, error)   // Conflict (taken)
func (s *Service) UpdateKind(ctx context.Context, key string, in KindInput) (Kind, error) // NotFound; Key ignored

// Schedule. All need events.manage and an open event.
func (s *Service) AddScheduleItem(ctx context.Context, event model.EventID, in ScheduleInput) (ScheduleItem, error)
func (s *Service) UpdateScheduleItem(ctx context.Context, event model.EventID, item ScheduleItemID, in ScheduleInput) (ScheduleItem, error)
func (s *Service) RemoveScheduleItem(ctx context.Context, event model.EventID, item ScheduleItemID) error

func (s *Service) InfoOf(ctx context.Context, ids []model.EventID) (map[model.EventID]Info, error)

type Route struct {
	Method  string
	Pattern string
	Handler http.Handler
}
func (s *Service) Routes() []Route
func (s *Service) OpenAPI() []byte

const (
	MaxNameBytes        = 100
	MaxKindKeyBytes     = 32
	MaxKindNameBytes    = 60
	MaxLabelBytes       = 100
	MaxZoneBytes        = 64
	MaxScheduleItems    = 100
	MaxEventDuration    = 366 * 24 * time.Hour
	MaxInfo             = 200
	DefaultSetupTimeout = 10 * time.Second

	PermissionManage authz.Permission = "events.manage"

	SignalCreated  signals.Name = "events.event-created"
	SignalChanged  signals.Name = "events.event-changed"
	SignalClosed   signals.Name = "events.event-closed"
	SignalReopened signals.Name = "events.event-reopened"
	PayloadEvent                = "event"
	PayloadCopiedFrom           = "copied-from"
)

var (
	Permissions      []authz.PermissionDecl
	PublishedSignals []signals.Name
	Set              = db.Set{Name: "events", Schema: "core", Files: postgres.Migrations}
)
```

`eventstest.NewFake() *Fake`: a `Directory` with `Add(Info)`, safe for concurrent use.

### `eventsetup` (contract)

```go
package eventsetup

// Point is the extension point events defines; app accepts a Provider.
const Point = "events.event-setup"

// Request names the new event and the one to copy from (never zero, never equal).
type Request struct {
	Event    model.EventID
	CopyFrom model.EventID
}

// Result is what a provider did; Items counts records it created or copied.
type Result struct {
	Items int
}

// Provider is a module's set-up for a new event. It runs in the caller's ctx (the event
// admin), under events' SetupTimeout, after the event is committed; it checks the
// caller's permission and fails Permission when they may not copy its data; it is
// idempotent per (Event, module), because RunSetup calls it again; it never imports
// events (it only receives IDs).
type Provider interface {
	SetUp(ctx context.Context, req Request) (Result, error)
}
```

**Per method: consumer, pre- and post-conditions (`events`).**

| Method | Consumer | Pre | Post |
|---|---|---|---|
| `List` | the events screen; module screens' event pickers (through the route) | signed in | newest first; `Closed` set |
| `Get` | the event page | signed in | schedule sorted |
| `Create` | the event admin's "New event" | `events.manage`; valid input; `CopyFrom` existing and open or closed (any) | event committed before any provider runs; `Setup` has one entry per linked provider when `CopyFrom` is set, else empty |
| `Update` | the event page's "Edit" | `events.manage`; open | signal `events.event-changed` after commit (modules re-check anchored work) |
| `Close`, `Reopen` | the event page | `events.manage`; state as required | `Closed` flips; signal after commit |
| `RunSetup` | the event page's "Run set-up again" | `events.manage`; open; `from` ≠ `id` | each provider called once; one `events.setup.ran` entry |
| `ListKinds`, `CreateKind`, `UpdateKind` | the new-event form; the kinds screen | session / `events.manage` | keys immutable; builtin names editable |
| `AddScheduleItem`, `UpdateScheduleItem`, `RemoveScheduleItem` | the event page's schedule editor | `events.manage`; open; at most `MaxScheduleItems` | audit entry per change; no signal (modules read the schedule when they need it) |
| `InfoOf` | modules rendering times in an event's zone, refusing writes for a closed event, labelling export documents | at most `MaxInfo` IDs | unknown IDs absent |
| `Link` | `app` part 7 `link` | once | providers frozen |
| `Routes`, `OpenAPI` | `app` part 7 | — | fragment matches routes |

## Types

Defined above. Uses `model.DepartmentID`, `model.EventID`, `model.TimeRange`,
`model.Email`, `authz.Permission`, `authz.PermissionDecl`, `signals.Name`, `audit.Recorder`.
Record types shared by the service and `postgres/` live in `departments/rows` and
`events/rows` (the leaf-package pattern authz and people use).

## Data

Both sets use schema `core` (db.md's fixed order already lists `departments` then `events`,
between `authz` and `audit`). Nothing in either table is personal data: every column is
`internal` and kept forever (departments are archived, never deleted; events are closed,
never deleted). No column references `core.person`.

**`core.department`** (set `departments`, migration `0001_init.sql`)

| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` PK | `model.DepartmentID` |
| `name` | `text` NOT NULL, `octet_length` 1..100, no control characters | unique ignoring case: `CREATE UNIQUE INDEX department_name_key ON core.department (lower(name))` (decision 15); the store maps constraint `department_name_key` to `Conflict taken` on field `name` (by constraint name, D38) |
| `description` | `text` NOT NULL DEFAULT `''`, `octet_length` <= 2000 | |
| `contact_email` | `text` NOT NULL DEFAULT `''`, `octet_length` <= 254 | the department's shared mailbox, stored as entered (shape checked by `model.ParseEmail`); `internal` (decision 7) |
| `archived_at` | `timestamptz` NULL | NULL = active |
| `created_at`, `updated_at` | `timestamptz` NOT NULL | from `clock` |

Index `department_active_name_idx ON core.department (lower(name)) WHERE archived_at IS NULL`
is not needed at this size; `List` scans the table.

**`core.event_kind`** (set `events`, `0001_init.sql`)

| Column | Type | Notes |
|---|---|---|
| `key` | `text` PK, `CHECK (key ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$' AND octet_length(key) <= 32)` | |
| `name` | `text` NOT NULL, `octet_length` 1..60 | not unique: names may repeat (D38) |
| `builtin` | `boolean` NOT NULL DEFAULT false | true for the three rows `0001` inserts: `burn` "Burn", `decompression` "Decompression", `other` "Other" |
| `created_at`, `updated_at` | `timestamptz` NOT NULL | the seed rows use the migration's `now()` (the one time a core migration writes data besides authz's starter roles) |

**`core.event`**

| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` PK | `model.EventID` |
| `name` | `text` NOT NULL, 1..100 bytes, no control characters | unique ignoring case (`event_name_key`) → `Conflict taken` |
| `kind` | `text` NOT NULL REFERENCES `core.event_kind(key)` ON DELETE RESTRICT | the store maps the foreign-key violation to `Invalid kind unknown` |
| `starts_at`, `ends_at` | `timestamptz` NOT NULL, `CHECK (ends_at > starts_at)` | `model.TimeRange`, half-open |
| `zone` | `text` NOT NULL, 1..64 bytes | IANA name; validated in Go with `time.LoadLocation` |
| `closed_at` | `timestamptz` NULL | NULL = open |
| `created_at`, `updated_at` | `timestamptz` NOT NULL | |

Index `event_starts_at_idx ON core.event (starts_at DESC)`.

**`core.event_schedule_item`**

| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` PK | `ScheduleItemID` |
| `event_id` | `uuid` NOT NULL REFERENCES `core.event(id)` ON DELETE RESTRICT | |
| `key` | `text` NOT NULL DEFAULT `''`, same grammar as a kind key when not empty, <= 32 bytes | `CREATE UNIQUE INDEX event_schedule_key ON core.event_schedule_item (event_id, key) WHERE key <> ''` → `Conflict duplicate` on `key` |
| `label` | `text` NOT NULL, 1..100 bytes | |
| `starts_at` | `timestamptz` NOT NULL | |
| `ends_at` | `timestamptz` NULL, `CHECK (ends_at IS NULL OR ends_at > starts_at)` | NULL = a moment |
| `position` | `integer` NOT NULL | insertion order; ties in `Starts` sort by it |
| `created_at`, `updated_at` | `timestamptz` NOT NULL | |

Modules may reference `core.department(id)` and `core.event(id)` with foreign keys
(`ON DELETE RESTRICT`; neither is ever deleted) and must never read these tables directly
(D10); they use the `Directory` interfaces.

## Permissions

| Permission | Allows | Scoped by | Reaches | Starter roles |
|---|---|---|---|---|
| `departments.manage` | create, rename, archive and restore departments; list archived ones; `Get` an archived one | site | internal | `department-steward` |
| `departments.details.manage` | change one department's description and contact mailbox; `Get` it while archived | department | internal | `department-admin`, `department-steward` |
| `events.manage` | create, change, close and reopen events; kinds; schedule; `RunSetup` | site | internal | `event-admin` |

Reads of active departments, of all events and of kinds need a session and no permission
(decision 2). `NamesOf`, `Active` and `InfoOf` need neither: they return
`internal` data to code that already passed its own permission check or runs as the system.
Leadership needs nothing here (decision 5). The base role `participant` holds none of the
three. Starter role contents after `Seed` on M3: `department-steward` gains
`departments.manage` and `departments.details.manage` (and, D38, authz's `access.grants.read`); `event-admin` gains
`events.manage`; `department-admin` gains `departments.details.manage` (authz's table,
Proposed change 1).

## Signals

Published (payload keys and types; IDs only):

| Name | When | Payload |
|---|---|---|
| `departments.department-created` | inside `Create`'s transaction | `department` DepartmentID |
| `departments.department-archived` | inside `Archive`'s transaction | `department` |
| `departments.department-restored` | inside `Restore`'s transaction | `department` |
| `events.event-created` | inside `Create`'s transaction, before any provider runs | `event` EventID, `copied-from` EventID (only when set) |
| `events.event-changed` | inside `Update`'s transaction (name, kind, dates or zone) | `event` |
| `events.event-closed` | inside `Close`'s transaction | `event` |
| `events.event-reopened` | inside `Reopen`'s transaction | `event` |

Not published: detail changes of a department (nobody reacts; audited), kind changes,
schedule changes (modules that anchor to a schedule item read it when they need it; a
`events.schedule-changed` signal is added when `volunteers` designs anchored shifts).
Subscribed: none. Expected subscribers (Phase 1): `volunteers` on
`departments.department-archived` (stop sign-ups for the department's open shifts),
`events.event-closed` (stop sign-ups), `events.event-changed` (flag shifts now outside the
dates). A handler failure is logged and counted by `signals` and is final until `jobs`
(signals decision 3); the department or event change stands.

## Settings

None. Department names, kinds and zones are rows; leadership terms are grant end dates
(authz); the words "department" and "event" come from the terminology layer (`{term_*}`,
settings, D28), the new-event form's default zone from the browser until `settings` adds the
regional's zone (Proposed change 11). No retention setting: nothing here is personal and
history must stay labelled (D30).

## UI

Two core screen sets (`kind: "core"`, web-shell decision 3): `web/src/core/departments/`
(keys `core.departments.*`) and `web/src/core/events/` (keys `core.events.*`), each with
`messages/en.json`. Phone first (web-shell D21): lists are cards, forms one column, actions
in the shell's `Dialog`, 44 px targets. Every failure uses `ErrorState`/`ErrorSummary`;
field codes map to inputs by `name`. Read-only copy: every list and page works; buttons are
disabled with the read-only note. Forbidden: hidden; a direct visit shows the shell's
forbidden screen. Message arguments use `{term_department}` and `{term_event}`.

Nav: main area **Departments** (`/departments`, `signed-in`) and **Events** (`/events`,
`signed-in`); admin area **Event kinds** (`/events/kinds`, `access: ["events.manage"]`).

- **Departments** (`/departments`): cards with name, first line of the description, contact
  mailbox as a `mailto:` link. Holders of `departments.manage` see "New department" and a
  "Show archived" toggle (`?archived=1`); archived cards carry an "Archived" badge.
  *Empty:* "No departments yet. {A steward creates the first one.}" (second sentence only
  for stewards).
- **New department** (`/departments/new`, `departments.manage`): name, description,
  contact mailbox with the help text "the department's shared mailbox, such as
  rangers@your-regional.org, never a person's address". Errors `required`, `too_long`,
  `malformed`, `taken`.
- **Department page** (`/departments/:department`): name, description, contact mailbox.
  **Edit details** (description, mailbox) for `departments.details.manage` at it or
  site-wide; **Rename**, **Archive** / **Restore** (confirm dialogs; archive explains "its
  leads keep their roles; modules stop new work for it; you can restore it") for
  `departments.manage`. A **Leadership** card for holders of `access.grants.read` (site or
  at this department; stewards hold it site-wide, D38) has a "See roles here" link to `/access/grants?department=<id>&role=department-admin` (the grants list filtered to `department-admin` at that department; `role` takes a key, D38) and
  "Appoint a lead" to `/access/grants/new?department=<id>` for holders of
  `access.department-grants.manage` or `access.grants.manage@<id>`; others see the contact
  mailbox only (decision 19). *Archived:* a banner "Archived on <date>"; edit buttons
  hidden. *Not found:* the shell's not-found state (also for an archived department the
  caller may not see).
- **Events** (`/events`): three groups computed in the browser from `dates` and `closed`:
  **Now** (`Contains(now)`, open), **Upcoming** (start in the future, open), **Past** (ended
  or closed, with a "Closed" badge). Cards: name, kind (catalogue key for builtin kinds,
  else `name`), dates in the event's zone through `useFormat().dateTime(instant, zone)`
  (the zone is shown when it differs from the browser's). Holders of `events.manage` see
  "New event". *Empty:* "No events yet."
- **New event** (`/events/new`, `events.manage`): name, kind (select from `GET /kinds`),
  start and end (`datetime-local` inputs interpreted in the chosen zone, sent as instants),
  zone (select from `Intl.supportedValuesOf("timeZone")`, default the browser's, with a
  text fallback), "Copy set-up from" (select of existing events, newest first; the UI
  preselects the newest event of the same kind, the person may clear it). On success, the
  event page opens with a **Set-up** card listing each module's result ("volunteers: 12
  items copied", "incidents: nothing to copy", "registrations: failed (no permission)")
  and "Run set-up again". Errors `required`, `too_long`, `range` (end before start, or
  longer than a year), `malformed` (zone), `unknown` (kind, copy source), `taken`.
- **Event page** (`/events/:event`): name, kind, dates with zone, status; the **Schedule**
  list (label, time or window in the event's zone; items outside the dates carry a
  warning). For `events.manage`: **Edit** (same form as New without copy), **Close** /
  **Reopen** (confirm; closing before the end says "This event has not ended yet"),
  **Add schedule item** (label, optional key, start, optional end), edit and delete per
  item, **Run set-up again** (choose the source event). *Closed:* a banner; edit buttons
  hidden except Reopen.
- **Event kinds** (`/events/kinds`, `events.manage`): cards with key and name; "New kind"
  (key, name; names may repeat, D38); rename inline. No delete (decision 9). A builtin kind shows its catalogue label until renamed, then its stored name everywhere, in the kinds screen, the new-event form and event cards (D38). Errors `malformed` (key), `taken`.
- **authz's grant screens** unhide their department filter and department roles once
  `GET /api/departments` exists (authz.md "UI"); the picker lists active departments by
  name.

End-to-end (`scripts/e2e.sh`, default mode) adds: steward creates a department; access
admin grants `department-admin` at it to a second person; that person edits the description
and cannot rename; steward archives and restores it; event admin creates a kind, an event
with a schedule item, closes it; the readonly mode visits both lists.

## Behavior

**Actor and permission, in order.** Each use case: read the actor (`Options.Actor`; none,
restricted or `signals.InDelivery` → `Unauthenticated`, except `NamesOf`, `Active`,
`InfoOf`); validate the input (`Invalid` before any statement); check the permission
(`Allow` for site permissions, `AllowIn(departments.details.manage, D)` for details); then
the store. `Update` on a department decides by field: `Name != nil` →
`Allow(departments.manage)`; any other field set → `AllowIn(details.manage, D)`; both
checks run when both kinds of field are set; no field set → `Invalid body required`.

**Writes.** One `Transact(Write)` each: `SELECT ... FOR UPDATE` the row (archive, restore,
update, close, reopen, schedule changes), check state (`Conflict archived|not_archived|
closed|open`), write, `Audit.Record` inside the transaction (`Record` the row's `Ref`:
kinds `departments.department`, `events.event`, `events.kind`, `events.schedule-item`;
`Subject` zero; `Fields` the names of the fields that changed; `Level` internal; for `events.kind` the `Ref` is a fixed ID from sha256(`"events.kind:"+key`) truncated to 16 bytes and `Reason` is `key:<key>`, D38), then
`Signals.Publish` (delivered after commit). A read-only copy fails with `Unavailable`
wrapping `db.ErrReadOnly` from the first statement. The unique-name and schedule-key
constraints are the serialisation: no advisory lock is needed.

**`Create` with set-up (events).** Transaction 1: insert, audit `events.event.created`,
publish `events.event-created` (`copied-from` when set). Commit. If `CopyFrom` is zero or
no provider is linked, return `Created{Event, Setup: []}`. Otherwise, for each provider in
link order: `ctx, cancel := context.WithTimeout(ctx, SetupTimeout)`; span `events.setup`
with `module` token; call `SetUp`; a result or an error becomes a `SetupResult` (`Code` =
`errs.KindOf(err)` name; a panic is recovered and reported as `internal`); count
`bms.events.setup{module, outcome}` (`ok`, `failed`, `timeout`, `permission`; a provider error of kind `Timeout` is `timeout`; a provider that returns success after its deadline passed is `ok` with its items; a panic is logged once, with its stack; D38). Then
transaction 2, run with `context.WithoutCancel(ctx)` under a 5s deadline so an expired request deadline does not lose it (D38): `Audit.Record` `events.setup.ran` (`Outcome` Succeeded when every provider
succeeded, else Failed; `Reason` `copy_from:<id>`; `Fields` the module names that failed).
If transaction 2 fails, `Create` returns the error even though the event exists (F64): the
UI shows it and a reload shows the event. Providers run with the caller's ctx and actor, so
a module that needs a permission the event admin lacks answers `Permission` and the result
says so; the module's own leads can run its copy from its own screens later. `RunSetup`
repeats the loop and transaction 2 for an open event. If the caller's ctx ends partway through the loop (D38), each provider that has not run yet is reported as failed (`Code` `timeout` or `canceled`, `Items` 0) and no metric is counted for it; transaction 2 still records `events.setup.ran` on the detached ctx with Outcome Failed and `Fields` the failed modules, whenever at least one provider ran, and then the ctx error is returned; nothing is recorded if no provider ran.

**`Get` of an archived department.** Load the row; if archived, `Allow(departments.manage)`
and, failing that, `AllowIn(departments.details.manage, id)`; if neither, `NotFound` (never
`Permission`: the response must not confirm the department exists, privacy-data).

**Lists.** `List` (departments) runs one statement (`WHERE archived_at IS NULL` unless
`IncludeArchived` and `departments.manage`); `List` (events) filters by `closed_at` per
`Status`; neither paginates (decision 14). `NamesOf`/`InfoOf` take at most 200 IDs, run one
statement with `= ANY($1)`, and answer a map.

**Zone and dates.** `Validate` loads the zone with `time.LoadLocation` (`Invalid zone
required` for `""`; `malformed` on failure or for `"Local"`; D38); `app` imports `time/tzdata` so a container
without a zone database still works (Proposed change 2). Dates are validated by
`model.TimeRange.Validate` (fields prefixed `dates.`) and `Duration() <= MaxEventDuration`
(`Invalid dates.end range`). Schedule items are not checked against the dates (decision
10). Overlapping events are allowed (several can be open at once); the list shows them.

**Concurrency and lifecycle.** Both services are safe for concurrent use and hold no
goroutines; nothing to close. `Link` is called once by `app` before `api.New`; providers
are frozen after it. `New` does no I/O and succeeds against `db.None()`. Deadlines: every
statement inherits `db`'s `QueryTimeout`; a provider call is bounded by `SetupTimeout`.
Retries: none here; the client never retries a mutation (web-shell D11); `RunSetup` is the
human retry.

**Telemetry.** Spans `departments.<use-case>`, `events.<use-case>`, `events.setup`
(attributes: `department_id`/`event_id` as `telemetry.ID`, `module` as a token, `outcome`).
Logs carry IDs only; a department or event name is never logged (it is not personal, but
the rule is simpler kept absolute). Metric `bms.events.setup{module,outcome}`.

**Delivery by milestone.** M3: everything above. Phase 1 (`volunteers`): `Deps.Departments`
and `Deps.Events` consumed; `ScheduleOf(ctx, EventID)` added to `events.Directory` when
anchored shifts are designed; the `departments.department-page` point when the module has a
section to add. Later: a `HoldersOf` seam from authz so every signed-in person can see a
department's leads (decision 19); the regional's default zone from `settings`; bulk
import of departments (D29).

**What `volunteers` (Phase 1) will need from here.** Departments: `Deps.Departments.Active`
before every write that names a department (positions, shifts, memberships);
`NamesOf` for export documents, notifications and the person-page section; the
`departments.department-archived` signal to stop sign-ups; `GET /api/departments` in its
screens for pickers (it may not import `web/src/core/departments/`). Events:
`Deps.Events.InfoOf` for the zone its screens and notifications render shift times in and
for `Closed` (refuse new sign-ups); `GET /api/events` for pickers; an
`eventsetup.Provider` that copies shifts from `CopyFrom` (positions are standing, so event
set-up copies shifts only; module-volunteers, D44), idempotent per event; the signals above; later `ScheduleOf` for shifts anchored to a
schedule item ("perimeter starts 90 minutes before `temple-burn`"). Department-scoped
permissions (`volunteers.shifts.manage` at D) resolve through authz's `AllowIn`/`ExtentOf`;
a shift's `event_id` is a nullable foreign key to `core.event(id)` (work with no event,
Q-CORE10).

## Failure modes

Store kinds (`Unavailable`, `Timeout`, `Canceled`) apply to every method that reaches the
database and are tested once per package (`TestStoreFailure_PropagatesKind`). `Unavailable`
wrapping `db.ErrReadOnly` applies to every write and is tested once per package
(`TestWrites_ReadOnlyCopy_ReturnUnavailableReadOnly`). An audit `Record` failure or a
`Publish` failure inside a write rolls it back and returns that error
(`TestWrite_AuditFails_RollsBack`, `TestWrite_PublishFails_RollsBack`, per package).

### `departments`

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `New` | a nil dependency | Invalid (`departments.<field>`) | fix wiring | `TestNew_MissingDependency_ReturnsInvalid` |
| F2 | `List` | no actor | Unauthenticated | sign in | `TestList_NoActor_ReturnsUnauthenticated` |
| F3 | `List` | `IncludeArchived` without `departments.manage` | Permission | hide the toggle | `TestList_IncludeArchivedWithoutManage_ReturnsPermission` |
| F4 | `Get` | zero or unknown ID | NotFound | not-found state | `TestGet_Unknown_ReturnsNotFound` |
| F5 | `Get` | archived, caller holds neither permission | NotFound | not-found state | `TestGet_ArchivedWithoutPermission_ReturnsNotFound` |
| F6 | `Get` | archived, caller holds `details.manage@D` | — (succeeds) | show the banner | `TestGet_ArchivedAsDepartmentAdmin_Succeeds` |
| F7 | `Create` | no `departments.manage` | Permission | hide the button | `TestCreate_WithoutManage_ReturnsPermission` |
| F8 | `Create` | name empty, too long, or with control characters | Invalid (`name` `required`/`too_long {max}`/`control_chars`) | field error | `TestCreate_BadName_ReturnsInvalid` |
| F9 | `Create` | description too long | Invalid (`description` `too_long`) | field error | `TestCreate_LongDescription_ReturnsInvalid` |
| F10 | `Create` | contact mailbox not an e-mail shape | Invalid (`contact_email` `malformed`) | field error | `TestCreate_BadContactEmail_ReturnsInvalid` |
| F11 | `Create` | name equal to another's ignoring case | Conflict (`name` `taken`) | field error | `TestCreate_NameTaken_ReturnsConflict` |
| F12 | `Update` | no field set | Invalid (`body` `required`) | fix the form | `TestUpdate_NoFields_ReturnsInvalid` |
| F13 | `Update` | `Name` set without `departments.manage` | Permission | hide Rename | `TestUpdate_RenameWithoutManage_ReturnsPermission` |
| F14 | `Update` | details set without `details.manage` at D (held elsewhere or not at all) | Permission | hide Edit | `TestUpdate_DetailsOutsideExtent_ReturnsPermission` |
| F15 | `Update` | department archived | Conflict (`archived`) | show the banner | `TestUpdate_Archived_ReturnsConflict` |
| F16 | `Update` | unknown ID | NotFound | not-found state | `TestUpdate_Unknown_ReturnsNotFound` |
| F17 | `Update` | new name taken | Conflict (`name` `taken`) | field error | `TestUpdate_NameTaken_ReturnsConflict` |
| F18 | `Update` | invalid field values (as F8–F10) | Invalid | field error | `TestUpdate_BadValues_ReturnsInvalid` |
| F19 | `Archive` | no `departments.manage` | Permission | hide | `TestArchive_WithoutManage_ReturnsPermission` |
| F20 | `Archive` | unknown ID | NotFound | not-found | `TestArchive_Unknown_ReturnsNotFound` |
| F21 | `Archive` | already archived | Conflict (`archived`) | refresh | `TestArchive_Twice_ReturnsConflict` |
| F22 | `Archive` | two concurrent archives of one department | one succeeds, the other Conflict (`archived`) | refresh | `TestArchive_Concurrent_OneWins` (integration) |
| F23 | `Restore` | not archived | Conflict (`not_archived`) | refresh | `TestRestore_Active_ReturnsConflict` |
| F24 | `Restore` | no `departments.manage` / unknown | Permission / NotFound | hide / not-found | `TestRestore_WithoutManage_ReturnsPermission`, `TestRestore_Unknown_ReturnsNotFound` |
| F25 | `Archive` | grants at the department exist | — (they stay; `Active` false) | nothing | `TestArchive_KeepsGrants_ActiveFalse` (integration with authz) |
| F26 | `NamesOf` | more than `MaxNames` IDs | Invalid (`ids` `too_many {max}`) | batch | `TestNamesOf_TooMany_ReturnsInvalid` |
| F27 | `NamesOf` | unknown IDs among known | — (absent from the map) | label as unknown | `TestNamesOf_UnknownIDsAbsent` |
| F28 | `NamesOf` | archived department | — (named) | label with "(archived)" | `TestNamesOf_IncludesArchived` |
| F29 | `Active` | zero ID | Invalid (`id` `required`) | fix the caller | `TestActive_ZeroID_ReturnsInvalid` |
| F30 | `Active` | unknown or archived | `(false, nil)` | refuse the write | `TestActive_UnknownOrArchived_False` |
| F31 | `Active` | called inside authz's `Write` transaction | — (joins the ctx transaction) | nothing | `TestActive_InsideTransaction_UsesIt` (integration) |
| F32 | handlers | a malformed path ID | Invalid (`department` `malformed`) | 400 | `TestHandlers_BadPathID_Returns400` |
| F33 | `Routes`/`OpenAPI` | fragment and routes disagree | caught by `api.New` (Invalid) at start | fix | `TestOpenAPI_NamesExactlyTheRoutes` |

### `events`

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F40 | `New` | nil dependency; `SetupTimeout` outside 1s..5m | Invalid (`events.<field>`) | fix wiring | `TestNew_BadOptions_ReturnsInvalid` |
| F41 | `List` | no actor | Unauthenticated | sign in | `TestList_NoActor_ReturnsUnauthenticated` |
| F42 | `List` | unknown status string (handler) | Invalid (`status` `one_of`) | fix the query | `TestListHandler_BadStatus_Returns400` |
| F43 | `Get` | unknown ID | NotFound | not-found | `TestGet_Unknown_ReturnsNotFound` |
| F44 | `Create` | no `events.manage` | Permission | hide | `TestCreate_WithoutManage_ReturnsPermission` |
| F45 | `Create` | bad name (as F8) | Invalid (`name`) | field error | `TestCreate_BadName_ReturnsInvalid` |
| F46 | `Create` | kind unknown or malformed | Invalid (`kind` `unknown`/`malformed`) | field error | `TestCreate_UnknownKind_ReturnsInvalid` |
| F47 | `Create` | start or end zero; end not after start | Invalid (`dates.start`/`dates.end` `required`/`range`) | field error | `TestCreate_BadDates_ReturnsInvalid` |
| F48 | `Create` | longer than `MaxEventDuration` | Invalid (`dates.end` `range`) | field error | `TestCreate_TooLong_ReturnsInvalid` |
| F49 | `Create` | zone empty (`required`), `Local` or not an IANA name (`malformed`), or too long (`too_long`) (D38) | Invalid (`zone` `required`/`malformed`/`too_long`) | field error | `TestCreate_BadZone_ReturnsInvalid` |
| F50 | `Create` | `CopyFrom` unknown | Invalid (`copy_from` `unknown`) | field error | `TestCreate_UnknownCopyFrom_ReturnsInvalid` |
| F51 | `Create` | name taken | Conflict (`name` `taken`) | field error | `TestCreate_NameTaken_ReturnsConflict` |
| F52 | `Create` | a provider returns an error | — (`SetupResult{Failed, Code}`; event kept) | show the card | `TestCreate_ProviderFails_ReportsAndKeepsEvent` |
| F53 | `Create` | a provider exceeds `SetupTimeout` or returns `Timeout` (metric outcome `timeout`, D38) | — (`Code: "timeout"`) | show; retry later | `TestCreate_ProviderTimesOut_Reported` |
| F54 | `Create` | a provider panics | — (`Code: "internal"`, logged once with stack, D38) | show | `TestCreate_ProviderPanics_Recovered` |
| F55 | `Create` | a provider fails `Permission` (admin lacks the module's permission) | — (`Code: "permission"`) | show; the module's leads copy later | `TestCreate_ProviderPermission_Reported` |
| F56 | `Create` | `CopyFrom` zero | — (no provider called, `Setup` empty) | nothing | `TestCreate_NoCopy_RunsNoProvider` |
| F57 | `Update` | event closed | Conflict (`closed`) | show the banner | `TestUpdate_Closed_ReturnsConflict` |
| F58 | `Update` | no field set / invalid values / taken / unknown | Invalid / Invalid / Conflict / NotFound | as above | `TestUpdate_NoFields_ReturnsInvalid`, `TestUpdate_BadValues_ReturnsInvalid`, `TestUpdate_NameTaken_ReturnsConflict`, `TestUpdate_Unknown_ReturnsNotFound` |
| F59 | `Close` | already closed | Conflict (`closed`) | refresh | `TestClose_Twice_ReturnsConflict` |
| F60 | `Reopen` | not closed | Conflict (`open`) | refresh | `TestReopen_Open_ReturnsConflict` |
| F61 | `Close`/`Reopen` | no `events.manage` / unknown | Permission / NotFound | hide / not-found | `TestClose_WithoutManage_ReturnsPermission`, `TestClose_Unknown_ReturnsNotFound` |
| F62 | `RunSetup` | `from` equals `id` | Invalid (`copy_from` `self`) | fix the form | `TestRunSetup_Self_ReturnsInvalid` |
| F63 | `RunSetup` | event closed; `from` unknown; no permission | Conflict (`closed`); Invalid (`copy_from` `unknown`); Permission | as above | `TestRunSetup_Closed_ReturnsConflict`, `TestRunSetup_UnknownFrom_ReturnsInvalid`, `TestRunSetup_WithoutManage_ReturnsPermission` |
| F64 | `Create`/`RunSetup` | the `events.setup.ran` audit write fails after providers ran | the audit error (event and copies stand) | show the error; reload | `TestCreate_SetupAuditFails_ReturnsErrorEventExists` |
| F65 | `RunSetup` | no provider linked | — (empty result; nothing recorded, D38) | show "nothing to set up" | `TestRunSetup_NoProviders_Empty` (asserts no `events.setup.ran`) |
| F65a | `Create`/`RunSetup` | the request ctx is cancelled or past its deadline when `events.setup.ran` is written (D38) | the entry is still recorded (`WithoutCancel`, 5s deadline); if that write also fails, F64 | none | `TestRunSetup_ExpiredRequestCtx_StillRecordsSetupRan` |
| F65b | `RunSetup` | the order of checks (D38) | no actor → Unauthenticated; then bad input (`copy_from`) → Invalid; then no `events.manage` → Permission | as above | `TestRunSetup_BadInputWithoutManage_ReturnsInvalidNotPermission` (unit), `TestRunSetup_BadInputWithoutManage_ReturnsInvalidBeforePermission` (integration) |
| F65c | `Create`/`RunSetup` | a provider returns success after its deadline (D38) | — (reported as success with its items, outcome `ok`) | none | `TestCreate_ProviderSucceedsAfterDeadline_ReportedAsSuccess` |
| F65d | `Create`/`RunSetup` | the request ctx ends partway through the provider loop (D38) | the ctx error (`Timeout` or `Canceled`); unrun providers reported Failed (`Code` `timeout`/`canceled`, `Items` 0), no metric for them; `events.setup.ran` recorded (Failed) if at least one provider ran, else nothing | show; run set-up again | `TestRunSetup_RequestCtxExpiresMidLoop_RecordsRanProvidersAndReportsSkipped` |
| F66 | `CreateKind` | key malformed or too long; name empty or too long | Invalid (`key` `malformed`/`too_long`; `name` `required`/`too_long`) | field error | `TestCreateKind_BadInput_ReturnsInvalid` |
| F67 | `CreateKind` | key exists | Conflict (`key` `taken`) | field error | `TestCreateKind_Taken_ReturnsConflict` |
| F68 | `UpdateKind` | unknown key | NotFound | not-found | `TestUpdateKind_Unknown_ReturnsNotFound` |
| F69 | kinds | no `events.manage` on create or update | Permission | hide | `TestKinds_WithoutManage_ReturnsPermission` |
| F70 | `AddScheduleItem` | event closed | Conflict (`closed`) | banner | `TestAddScheduleItem_Closed_ReturnsConflict` |
| F71 | `AddScheduleItem` | label empty or too long; `Starts` zero; `Ends` not after `Starts`; key malformed | Invalid (`label`, `starts_at` `required`, `ends_at` `range`, `key` `malformed`) | field error | `TestAddScheduleItem_BadInput_ReturnsInvalid` |
| F72 | `AddScheduleItem` | key already used in this event | Conflict (`key` `duplicate`) | field error | `TestAddScheduleItem_DuplicateKey_ReturnsConflict` |
| F73 | `AddScheduleItem` | already `MaxScheduleItems` | Invalid (`schedule` `too_many {max}`) | remove one | `TestAddScheduleItem_TooMany_ReturnsInvalid` |
| F74 | `UpdateScheduleItem`/`RemoveScheduleItem` | item unknown or not of this event | NotFound | refresh | `TestScheduleItem_WrongEvent_ReturnsNotFound` |
| F75 | schedule | no `events.manage` | Permission | hide | `TestSchedule_WithoutManage_ReturnsPermission` |
| F76 | `InfoOf` | more than `MaxInfo` IDs | Invalid (`ids` `too_many`) | batch | `TestInfoOf_TooMany_ReturnsInvalid` |
| F77 | `InfoOf` | unknown IDs | — (absent) | label as unknown | `TestInfoOf_UnknownAbsent` |
| F78 | `Link` | called twice; nil provider; empty or duplicate module | Internal | fix `app` | `TestLink_Twice_ReturnsInternal`, `TestLink_BadProvider_ReturnsInternal` |
| F79 | handlers | malformed path ID or body | Invalid | 400 | `TestHandlers_BadInput_Returns400` |
| F80 | `Routes`/`OpenAPI` | disagreement | caught by `api.New` | fix | `TestOpenAPI_NamesExactlyTheRoutes` |

`Routes`, `OpenAPI`, `ParseStatus` on valid input, `Validate` on valid values and
`departmentstest`/`eventstest` setters cannot fail: they touch no I/O and take no
unchecked input.

## Lock-in check

No external technology beyond PostgreSQL (db.md) and Go's zone database (`time/tzdata`,
BSD-licensed, embedded). Nothing to replace.

## Test plan

- **Unit (no infrastructure):** each service over an in-memory fake of its own store
  interface (as `people` does), `authztest.NewFake()` for permissions (grant `departments.
  manage` etc. per case; `GrantIn` for `details.manage@D`), `audittest.NewFake(area)`,
  `signalstest.NewFake(area, PublishedSignals, nil)`, `telemetrytest.New(t)`, a fake
  clock. Every F-row above; the per-package store-failure, read-only, audit-rollback and
  publish-rollback tests; `TestCreate_PublishesAfterCommitOnly`; `TestCreate_Setup_
  RunsProvidersInLinkOrderWithTimeout` (a counting provider; the fake clock does not drive
  the timeout, a blocking provider with a short `SetupTimeout` does); `TestOptions_
  Validate_Table`; `TestValidate_Table` for `Department`, `Event`, `ScheduleInput`,
  `KindInput`; `TestNoPersonalDataInLogs` (`telemetrytest` capture with a marker name).
- **Handlers:** `httptest` over `Routes()` with `httpx.WithFailer`; JSON shapes pinned
  (`TestWire_Shapes`); `TestOpenAPI_NamesExactlyTheRoutes`.
- **Permission tests:** one table per permission: holder succeeds, non-holder fails, `@D`
  holder fails outside D, superuser succeeds, no actor is `Unauthenticated`.
- **Integration (`//go:build integration`, `dbtest.TempDB`):** migrations apply on an empty
  database; the three kinds exist; unique-name conflicts (`taken`) under concurrency; F22;
  F25 and F31 with a real `authz.Service` wired to `departments` as `DepartmentSource`
  (grant at an archived department fails `Invalid department unknown`); schedule key
  uniqueness; `InfoOf`/`NamesOf` with 200 IDs.
- **`app`:** `TestParts_OrderAndNames_Pinned` gains `departments`, `events`;
  `TestLazyDepartments_BeforeBuild_Unsupported`; the e2e steps in "UI".
- **Conformance:** not a module; the fakes `departmentstest.Fake` and `eventstest.Fake`
  get `TestFake_Table` tests, and `moduletest.Conformance` is extended to supply them
  (Proposed change 3).

## Open questions

None; answered 2026-10-18 (D37), each as its recommendation: (1) two parts `departments` (6) and
`events` (7), one spec file, jobs/notify/files become 8-10, app F28 not amended (decision 1);
(2) the contact address is `internal`, a shared mailbox (decision 7); (3) the Leadership card (stewards also, D38)
only for `access.grants.read` holders and department admins there, `HoldersOf` Later
(decision 19); (4) every signed-in person lists active departments and all events without a
permission (decision 2); (5) event kinds are editable rows, the seeded three keep catalogue
keys (decision 9); (6) schedule items in M3 (decision 10).

## Proposed changes elsewhere

Status as of 2026-10-19 (D39): all 13 items are applied (1 to 10, 12 and 13 on 2026-10-18, D37; 11 on 2026-10-19). The code halves of items 2, 3 and 7 (`internal/app`, `cmd/bms`, `internal/core/module`, `moduletest`, `web/src`) are left for the implementers. The `plan/README.md` part of item 13 is plan-keeper's.

1. (applied 2026-10-18 (D37)) **`plan/design/authz.md` (row 7):** mark Proposed change 10 applied: `*departments.Service`
   implements `DepartmentSource` (`Active` = exists and not archived); `app` passes
   `Departments: lazyDepartments{a}` (answers `Unsupported` until part 6 is built);
   `GET /api/departments` exists, so the grant screens' department filter, department
   picker and department roles unhide; the starter-roles table gains `departments.manage`
   and `departments.details.manage` for `department-steward`, `events.manage` for
   `event-admin`, `departments.details.manage` for `department-admin`; "set or reset
   leadership" is granting `department-admin` through `access.department-grants.manage`;
   archiving leaves grants in place.
2. (applied 2026-10-18 (D37), in app.md rows 6 to 10; the code in `internal/app` and `cmd/bms` is for the implementer) **`plan/design/app.md` (row 18) and `internal/app`:** split row 6 of "How later core
   services plug in" into 6 `departments` (`declare`: `departments.Set`, `Permissions`,
   `PublishedSignals`; `build`: `departments.New` over `a.dbc`, clock, telemetry scope
   `departments`, `audit.Scoped("departments")`, `a.disp.Scope("departments", published,
   nil)`, `a.deps.authz`, `authzActor`; sets `a.departments`, `a.deps.departments`, route
   set `departments`) and 7 `events` (`declare`: `events.Set`, `Permissions`,
   `PublishedSignals`, `module.ExtensionPointDecl{Name: eventsetup.Point, Accepts: a
   `eventsetup.Provider`}`; `build`: `events.New` likewise with `SetupTimeout:
   events.DefaultSetupTimeout`; sets `a.events`, `a.deps.events`, route set `events`;
   `link`: `a.set.Contributors(eventsetup.Point)` → `[]events.SetupProvider` → `Link`);
   renumber jobs 8, notify 9, files 10 (also D12 and `coreParts`' comment); add
   `lazyDepartments` to `core_adapters.go` and pass it in `buildAuthz`; `cmd/bms` (or
   `app`) adds `import _ "time/tzdata"`; `TestParts_OrderAndNames_Pinned` and the e2e
   steps; mark Proposed change 10 applied for departments-events. The `assembly` gains
   `departments *departments.Service`, `events *events.Service`.
3. (applied 2026-10-18 (D37), in module-contract.md; the code in `internal/core/module` and `moduletest` is for the implementer) **`plan/design/module-contract.md` (row 4), `internal/core/module`, `moduletest`:** add
   `Deps.Departments departments.Directory` and `Deps.Events events.Directory` (M3;
   unscoped; `Provision` sets them); `moduletest.Conformance` supplies
   `departmentstest.NewFake()` and `eventstest.NewFake()` when nil; `Env.CoreExtensionPoints`
   carries `events.event-setup`; `Env.CoreSignals` the seven names; the rule for module
   authors: foreign keys to `core.department(id)` and `core.event(id)` are allowed
   (`ON DELETE RESTRICT`), the tables are never read directly, and an event-setup
   contribution is idempotent per (event, module). Mark Proposed change 5/13 (whichever
   covers these parts) applied.
4. (applied 2026-10-18 (D37)) **`plan/design/signals.md` (row 10):** mark Proposed change 8 applied for
   departments-events: the seven names and payload keys above; scopes `departments` and
   `events` from `app`.
5. (applied 2026-10-18 (D37)) **`plan/design/db.md` (row 5):** mark Proposed change 10 applied for `departments` and
   `events` (`postgres.Migrations`, `departments.Set`, `events.Set`, schema `core`, set
   names already in the fixed order); note that `events` `0001` inserts the three kinds.
6. (applied 2026-10-18 (D37)) **`plan/design/model.md` (row 2):** mark Proposed change 8 applied: an event is a
   `TimeRange` plus the IANA zone; schedule items are `events`' `ScheduleItem` (a `Starts`
   instant and an optional `Ends`); `Email` is used for the department mailbox.
7. (applied 2026-10-18 (D37), in web-shell.md; `web/src` is for the implementer) **`plan/design/web-shell.md` (row 17) and `web/src`:** decision 3 lists
   `web/src/core/departments/` and `web/src/core/events/`; the nav entries above; the
   smoke run adds the e2e steps; `useFormat` zone rendering is the documented way to show
   an event's times; the access grant screens' department picker reads `GET /api/departments`.
8. (applied 2026-10-18 (D37)) **`plan/design/audit.md` (row 12):** the actions listed in the Summary, all level
   `internal`, `Subject` zero, `Record` kinds `departments.department`, `events.event`,
   `events.kind`, `events.schedule-item`; `events.setup.ran` with `Reason`
   `copy_from:<id>`.
9. (applied 2026-10-18 (D37)) **`plan/design/telemetry.md` (row 15b):** add `bms.events.setup{module,outcome}` to the
   instrument table; spans `departments.*`, `events.*`, `events.setup`.
10. (applied 2026-10-18 (D37)) **`plan/07-abstractions.md`, "departments and events":** Owns becomes "`departments`:
    the standing departments (name, description, shared mailbox), archiving and restoring,
    `Directory` (`NamesOf`, `Active`) which is also authz's `DepartmentSource`, the
    `departments` routes and screens, signals `departments.department-created|archived|
    restored`. `events`: the events (name, kind, dates as instants plus zone, schedule
    items), kinds, closing and reopening, `Directory` (`InfoOf`), the extension point
    `events.event-setup` (`contracts/eventsetup`), the `events` routes and screens,
    signals `events.event-created|changed|closed|reopened`." Doesn't do adds "leadership
    (authz grants), membership and positions (`volunteers`), work around an event
    (modules), personal data (none held)". Two packages, two parts.
11. (applied 2026-10-19, D39, settings.md Proposed change 7) **`plan/design/settings.md` (row 15):** a `regional.timezone` setting (IANA
    name; default empty = the browser's) the new-event form reads through
    `GET /api/settings/public`, falling back to the browser's zone when empty; terminology keys `term_department`, `term_event` and the forms `term_department_plural`, `term_department_title`, `term_department_plural_title` (likewise for `event`).
12. (applied 2026-10-18 (D37)) **`plan/09-core-features.md` §5:** replace "how those are modelled is for
    `departments-events` to settle" with "schedule items: a labelled window or moment,
    with an optional key modules can anchor to"; note kinds are rows; §7 lists the four
    screens; the M3 row of the milestone table gains "add an event's schedule, copy a
    module's set-up into a new event".
13. (applied 2026-10-18 (D37)) **`.claude/skills/go-conventions/SKILL.md` and `add-module/SKILL.md`:**
    `Deps.Departments` (`NamesOf`, `Active`) and `Deps.Events` (`InfoOf`) with their fakes;
    check `Active` before writing a department's record; foreign keys to `core.department`
    and `core.event`; how to contribute to `events.event-setup` (`ContributesTo:
    eventsetup.Point`, a `Provider` in `Contributions()`, idempotent, checks the caller's
    permission); subscribe to `departments.department-archived` and `events.event-closed`
    when the module takes sign-ups. **`plan/README.md`:** follow-up (b) resolved by Open
    question 1 (plan-keeper, after the owner answers).
