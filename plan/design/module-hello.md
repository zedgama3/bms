# module-hello

- **Status:** Implemented (2026-10-17); Approved (2026-10-17)
- **Amended 2026-10-17 (owner):** `SettingDecl.Check` replaces `Validate`; Conformance is called with a factory over the in-memory fake store and `moduletest.WithSeed(seed)` (module-contract Proposed change 15). Reviewer wording corrections: `Start` validates banner keys before subscribing; `hello.greeting` gains `updated_at`, a byte-length check and named constraints; `DeletePerson` runs in `db.Transact(Write)`; `handleGreeted` waits for the counter's acknowledgement; `Stop` is nil when already stopped; module tests use a bare `ServeMux` only and the api-shell and OpenAPI-versus-routes tests live in `internal/app`; handlers `httpx.Fail` on a non-Canceled write failure; `Greet` trims the source word.
- **Package:** `internal/modules/hello` (with `internal/modules/hello/greeting`,
  `internal/modules/hello/postgres`) and `internal/contracts/greeter`
  **UI:** `web/src/modules/hello`
- **Depends on specs:** `module-contract`, `db`, `signals`, `telemetry`, `api` (`lib/httpx`),
  `web-shell`, `app`, `model`, `lib-errs`, `lib-core`
  **Used by:** `app` (compiled in through `internal/app/modules.go` and
  `web/src/modules/index.ts`; its end-to-end test), `web-shell` (Playwright smoke), every
  later module author (copy-paste template), `moduletest.Conformance` (the reference it is
  calibrated on)

## Summary

**Purpose.** Show, in working code, how a module plugs into every part of the core. `hello`
is the smallest module that touches the whole contract: a manifest, one schema and
migration, three routes with an OpenAPI fragment, two permissions, one setting, one signal
published and subscribed, one capability, one extension point, `PersonData`, spans, one
goroutine, one phone-first screen, and `moduletest.Conformance` (with `WithSeed`). It is not useful to a
burner and never will be; a regional turns it off with `BMS_MODULES_DISABLED=hello`.

**Exported (`hello`):** `Manifest` (`Name: "hello"`); `New(ctx, deps) (module.Module, error)`;
`PermissionGreetingsRead = "hello.greetings.read"`, `PermissionGreetingsWrite =
"hello.greetings.write"` (both `ScopeSite`, `Reaches` internal); `SettingGreeting =
"greeting"` (text, default `DefaultGreeting = "hello"`, 1..`MaxGreetingBytes` 64 after
trim, no control characters); `SignalGreeted = "hello.greeted"`, `PayloadPerson = "person"`;
`PointBanner = "hello.banner"`. Routes relative to `/api/hello`: `GET /public-greeting`
(public), `GET /greeting` (signed in, `greetings.read`), `POST /greetings` (signed in,
`greetings.write`, no body, 201). Fragment `openapi.json` embedded; schemas
`HelloPublicGreeting`, `HelloBanner`, `HelloGreeting`, `HelloGreeted`.
**`greeting`:** `ID = model.ID[marker]`, `Greeting{ID, Person model.PersonID, Text,
CreatedAt}` with `Validate()`. **`postgres`:** `Migrations fs.FS` (`0001_init.sql` creates
`hello.greeting`), `NewStore(db.DB) (*Store, error)`.
**`contracts/greeter`:** `const Name = "greeter"`; `Greeter{Greeting(ctx) (string, error)}`;
`Banner{Key string}` (a message key `module.<contributor>.banner.<ident>`).

**Key decisions.** 1 two permissions, not one: a read use case and a write use case each
check their own, which is the pattern every module copies. 2 three seams the core lacks in
M0 (`actorFunc`, `permissionChecker`, `greetingSource`) are wired to "no actor", "deny" and
"the declared default" in M0 and to identity (M1), authz (M2) and settings (M2) later, so
the service fails closed and no code path changes shape. 3 the `hello.greeted` subscriber
counts deliveries in memory on one goroutine (no write, so it works on a read-only copy);
idempotent on `Signal.ID` through a 256-entry ring. 4 banner contributions are validated in
`Start` (frozen values, no I/O) and read per request with `Links.Contributors`. 5 record
types live in the leaf package `hello/greeting` because `postgres/` cannot import the
module root (the root imports `postgres/` to build the store). 6 one `SpanInternal` span
per use case, named `hello.<use-case>`, integer attributes only. 7 `POST /greetings` takes
no body: `hello` has no input to read, so `httpx.ReadJSON` and `model.ParseID` are shown
by `api.md`, not here. 8 no retention setting in M0 (settings and jobs do not exist);
rows are kept until `DeletePerson`. 9 no FK to a person row until the core has one (M2,
migration `0002`). 10 the `greeter` capability has no consumer in M0; it exists so
`Plan.Build` resolves a real provider and authors see the shape.

**Errs kinds a caller handles.** `New`: `Invalid` (nil dependency). `Start`: `Invalid`
(a banner key not under its contributor's prefix; checked before subscribing, so a failed
`Start` leaves no handler), `Conflict` (started twice; a second handler for the name), bus kinds. `Stop`: `Canceled`/`Timeout` (counter goroutine did not
exit in time; nil when already stopped, even with a done ctx). `Greeting`/`Greet`: `Unauthenticated` (no actor), `Permission` (denied;
always in M0), `Unavailable` wrapping `db.ErrReadOnly` (`Greet` on a read-only copy), db
kinds. `PublicGreeting`: `Internal` (`Links` misuse). Handler: `Invalid` (payload without
`person`), `Canceled`/`Timeout`. `ExportPerson`/`DeletePerson`: `Invalid` (zero ID),
`Unavailable` wrapping `db.ErrReadOnly` (delete), db kinds. No sentinels of its own.

**Owns.** Schema `hello`, table `hello.greeting` (`person_id`, `greeting`, `created_at`, `updated_at`:
all `internal`, kept until deleted). Permissions above. Setting `hello.greeting`. Signal
`hello.greeted {person}`, published inside the `Transact` that inserts the row; subscribed
by itself. Capability `greeter` (provided). Extension point `hello.banner` (defined,
contributed to with `module.hello.banner.welcome`). Spans `hello.public-greeting`,
`hello.greeting`, `hello.greet`, `hello.export-person`, `hello.delete-person`. Message keys
`module.hello.*` (decision 8 set plus `nav.greeting`, `banner.welcome`, `screen.*`). Screen
`/hello/` (index), nav entry in `main`, public.

**Expects of dependencies.** module-contract (`Manifest`, `Deps` M0 fields, `Links`,
`Route`, `PersonData`, `moduletest.Conformance`); db (`Transact`, `Wrap`, `ErrReadOnly`,
`dbtest.Fake`, `TempDB`, migration `Set` rules); signals (`Bus`, `Ref`/`NewPayload`/`Get`,
`signalstest.Fake.Deliver`, delivery after commit under `deliver:hello.greeted`); telemetry
(`Tracer.Start`, `Int`, `telemetrytest.New`); api (`httpx.WriteJSON`/`Fail`, mounting,
fragment check, 401 before the handler on signed-in routes in M0); web-shell (`ScreenSet`,
`api` client, `useSession`, `useT`, `ErrorState`, `EmptyState`, `Button`, `Toast`); app
(`modules.go` line, e2e test, catalogue test); model (`ID`, `PersonID`, `NewID`).

**Settled by the owner (2026-10-17).** 11 once authz exists, the base role every signed-in
person gets holds both `hello.greetings.read` and `hello.greetings.write`. 12 `hello` stays
compiled in and enabled by default through Phase 1; regionals set `BMS_MODULES_DISABLED=hello`;
revisit when `volunteers` is Implemented.

**Open questions:** None. **Proposed changes elsewhere:** all applied 2026-10-17 (item 9 is
README row 4b).

## Purpose

Show, in working code, how a module plugs into every part of the core.

## Responsibilities

- Declare itself to the core: name, migrations, permissions, setting, signals, capability,
  extension point, routes, fragment.
- Save a greeting for the signed-in person inside one transaction that also publishes
  `hello.greeted`.
- Answer its own signal with a handler that changes nothing in the database.
- Serve a public greeting, a signed-in greeting with counts, and the write that saves one.
- Export and delete one person's greetings.
- Ship one screen that works on a phone, with every state the shell defines.
- Pass `moduletest.Conformance` and be the module the core's tests are calibrated on.
- Be short enough to read in one sitting and copy for a real module.

## Not responsible for

| Concern | Owner |
|---|---|
| Who the actor is (session, cookie) | `identity.md` (M1); until then `hello` has no actor and its signed-in use cases fail closed |
| Whether the actor holds a permission | `authz.md` (M2); until then `hello` denies |
| Storing or editing the `greeting` setting | `settings.md` (M2); until then the declared default is the value |
| The request pipeline, the problem body, the 401 on signed-in routes, the `read_only` mapping | `api.md` |
| Delivering `hello.greeted`, its spans and metrics, retrying a failed handler | `signals.md` (and `jobs.md` from M4) |
| Auditing export and deletion, assembling the per-person export | `people.md` and `audit.md` |
| Retention of greetings on a schedule | `settings.md` (the retention keys) and `jobs.md` (the job); not in M0 |
| The frame, navigation, the typed API client, catalogues of other languages | `web-shell.md` |
| Being useful at a burn | nothing: `hello` is a proof, and a regional turns it off |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| manifest (`module.go`) | Declare `hello` to the core | `var Manifest`, `New`, `init`; no English text |
| records (`greeting/`) | Name the one record type both the service and the store use | leaf: imports `model`, `lib` only |
| service (`service.go`) | Run the five use cases, each behind its permission, each under its span | declares `store`, `actorFunc`, `permissionChecker`, `greetingSource` |
| store (`postgres/`) | Keep `hello.greeting` in PostgreSQL | the only package with SQL and pgx; `Migrations` |
| http (`http.go`, `openapi.json`) | Wire the use cases to three routes | thin; `httpx.WriteJSON`, `httpx.Fail` |
| signals (`signals.go`) | Publish `hello.greeted`; count deliveries on one goroutine | the `counter` |
| privacy (`privacy.go`) | Export and delete one person's greetings | implements `module.PersonData` |
| greeter contract (`internal/contracts/greeter`) | Name the capability and the banner shape | plain types, no implementation |
| screens (`web/src/modules/hello`) | Show the greeting on a phone | one screen, `messages/en.json` |

## Interface

### Package `greeter` (`internal/contracts/greeter`)

```go
// Package greeter is the contract hello provides and the shape of its banner extension
// point. It holds no implementation (07-abstractions, "contracts").
package greeter

// Name is the capability's name; it converts to module.Capability.
const Name = "greeter"

// Greeter returns the regional's greeting text. It is public text, so there is no actor to
// check and nothing to batch; a real capability takes batches and checks permissions for
// the actor in ctx (module-contract decision 15).
type Greeter interface {
	// Greeting fails with Canceled/Timeout when ctx is done, and with the settings
	// store's kind once settings exist (M2); in M0 it cannot fail otherwise.
	Greeting(ctx context.Context) (string, error)
}

// Banner is one contribution to the extension point "hello.banner": a line the hello
// screen shows for the contributing module. Key is a full message key owned by the
// contributor, "module.<contributor>.banner.<ident>"; hello checks the prefix at Start.
type Banner struct {
	Key string
}
```

Consumers: `hello` (provides `Greeter`, defines the point with `Accepts` on `Banner`,
contributes one `Banner`); `moduletest.Conformance` (`implements_declared`); nobody else in
M0 (decision 10).

### Package `greeting` (`internal/modules/hello/greeting`)

```go
// Package greeting holds the record type shared by hello's service and its postgres store.
package greeting

type marker struct{}

// ID identifies one saved greeting.
type ID = model.ID[marker]

// Greeting is one saved greeting: who greeted, with which word, when.
type Greeting struct {
	ID        ID             `json:"id"`
	Person    model.PersonID `json:"-"`          // never on the wire; the caller already knows who they are
	Text      string         `json:"greeting"`
	CreatedAt time.Time      `json:"created_at"`
}

// Validate fails with errs.Invalid (joined validate.FieldErrors on "id", "person",
// "greeting", "created_at") for a zero ID, a zero person, an empty or over-long Text
// (MaxBytes = 64) or a zero CreatedAt.
func (g Greeting) Validate() error
```

Consumers: `hello` (service, http, privacy), `hello/postgres` (store).

### Package `hello`

```go
package hello

const (
	// Permissions (module-contract: "<module>.<ident>(.<ident>)*"). Both ScopeSite,
	// Reaches model.SensitivityInternal.
	PermissionGreetingsRead  authz.Permission = "hello.greetings.read"
	PermissionGreetingsWrite authz.Permission = "hello.greetings.write"

	// SettingGreeting is the one setting: the word used in every greeting.
	SettingGreeting  = "greeting"
	DefaultGreeting  = "hello"
	MaxGreetingBytes = 64

	// SignalGreeted is published inside the transaction that saves a greeting.
	SignalGreeted signals.Name = "hello.greeted"
	// PayloadPerson is the one payload key: the person who greeted.
	PayloadPerson = "person"

	// PointBanner is the extension point hello defines and contributes to.
	PointBanner module.ExtensionPoint = "hello.banner"

	// seenRing is how many Signal.IDs the counter remembers to stay idempotent.
	seenRing = 256
)

// Manifest declares hello to the core. It carries no text (decision 8).
var Manifest = module.Manifest{
	Name:       "hello",
	Migrations: postgres.Migrations,
	Permissions: []authz.PermissionDecl{
		{Name: PermissionGreetingsRead, Scope: authz.ScopeSite, Reaches: model.SensitivityInternal},
		{Name: PermissionGreetingsWrite, Scope: authz.ScopeSite, Reaches: model.SensitivityInternal},
	},
	Settings:      []module.SettingDecl{{Key: SettingGreeting, Kind: module.SettingText, Default: DefaultGreeting, Check: validateGreeting}},
	Publishes:     []signals.Name{SignalGreeted},
	Subscribes:    []signals.Name{SignalGreeted},
	Provides:      []module.Capability{greeter.Name},
	Defines:       []module.ExtensionPointDecl{{Name: PointBanner, Accepts: acceptsBanner}},
	ContributesTo: []module.ExtensionPoint{PointBanner},
}

func init() { module.Register(Manifest, New) }

// New builds the module from deps. It reads nothing from the database (app.md decision 7)
// and starts nothing. Fails with errs.Invalid when Logger, Tracer, Clock, DB, Signals or
// Links is nil, or when postgres.NewStore fails.
func New(ctx context.Context, deps module.Deps) (module.Module, error)
```

`New` wires the three M0 seams (Behavior, "Seams") and returns a `*Module` that implements
`module.Module`, `module.RouteProvider`, `module.APIDescriber`, `module.CapabilityProvider`
and `module.Contributor`:

```go
// Start validates every banner contribution (frozen values, no I/O), then subscribes the
// counter's handler to SignalGreeted and starts the counter goroutine. Validating first
// means a failed Start leaves no handler, so a retried Start does not hit Conflict. It writes nothing
// (module-contract decision 14). Fails with errs.Conflict if called twice, errs.Invalid
// when a contribution's Key is not "module.<its module>.banner.<ident>" (naming the
// module), or the bus's kind from Subscribe.
func (m *Module) Start(ctx context.Context) error

// Stop asks the counter goroutine to exit and waits for it, or for ctx. Nil when Start
// was never called or Stop already ran (even when ctx is done). Otherwise it checks ctx
// before waiting and fails with errs.Canceled or errs.Timeout from ctx.
func (m *Module) Stop(ctx context.Context) error

// Routes are relative to /api/hello (module-contract decision 13).
func (m *Module) Routes() []module.Route
// OpenAPI returns the embedded openapi.json (api.md decision 11).
func (m *Module) OpenAPI() []byte
// Capabilities provides greeter.Name as a greeter.Greeter.
func (m *Module) Capabilities() map[module.Capability]any
// Contributions contributes greeter.Banner{Key: "module.hello.banner.welcome"} to PointBanner.
func (m *Module) Contributions() map[module.ExtensionPoint]any

// ExportPerson and DeletePerson implement module.PersonData (Privacy below).
func (m *Module) ExportPerson(ctx context.Context, person model.PersonID) (module.Export, error)
func (m *Module) DeletePerson(ctx context.Context, person model.PersonID) (module.Deletion, error)
```

The service (unexported; the handlers and `PersonData` call it):

```go
// store is what the service needs from storage. postgres.Store implements it. No SQL or
// driver types here (D8).
type store interface {
	Insert(ctx context.Context, g greeting.Greeting) error
	CountByPerson(ctx context.Context, person model.PersonID) (int, error)
	ListByPerson(ctx context.Context, person model.PersonID) ([]greeting.Greeting, error)
	DeleteByPerson(ctx context.Context, person model.PersonID) (int, error)
}

// actorFunc reads the signed-in person from ctx. M0: always (zero, false). M1: identity's
// accessor.
type actorFunc func(ctx context.Context) (model.PersonID, bool)

// permissionChecker decides one permission for the actor in ctx; the same shape as
// api.PermissionChecker and authz's checker (M2). M0: denies everything with
// errs.Permission.
type permissionChecker interface {
	Allow(ctx context.Context, p authz.Permission) error
}

// greetingSource returns the current greeting word. M0: DefaultGreeting. M2:
// Deps.Settings for SettingGreeting.
type greetingSource func(ctx context.Context) (string, error)

type service struct {
	store    store
	actor    actorFunc
	allow    permissionChecker
	greeting greetingSource
	links    *module.Links
	bus      signals.Bus
	db       db.DB
	tracer   telemetry.Tracer
	logger   *slog.Logger
	clock    clock.Clock
	counter  *counter
}

// PublicGreeting is GET /public-greeting: the greeting word and the banners every
// contributor registered. No actor, no permission: nothing about a person is read.
// Span "hello.public-greeting" with Int("banners"). Fails with the greetingSource's kind
// or errs.Internal when Links refuses PointBanner (a bug: it is declared).
func (s *service) PublicGreeting(ctx context.Context) (PublicGreeting, error)

// Greeting is GET /greeting: the greeting word, how many times the actor has greeted
// (own rows only) and how many hello.greeted deliveries this process has seen. Needs an
// actor (errs.Unauthenticated) and PermissionGreetingsRead (errs.Permission). Span
// "hello.greeting" with Int("mine"), Int("seen"). Store failures keep db's kind.
func (s *service) Greeting(ctx context.Context) (Greeting, error)

// Greet is POST /greetings: in one db.Transact(Write), insert a greeting for the actor
// and publish SignalGreeted {person: actor}; delivery happens after commit. Needs an actor
// (errs.Unauthenticated) and PermissionGreetingsWrite (errs.Permission). Span
// "hello.greet". On a read-only copy fails errs.Unavailable wrapping db.ErrReadOnly; a
// refused Publish rolls the insert back and its kind is returned.
func (s *service) Greet(ctx context.Context) (greeting.Greeting, error)

// handleGreeted is the subscriber for SignalGreeted. It reads PayloadPerson with
// signals.Get (errs.Invalid if absent), hands {Signal.ID} to the counter and returns.
// It touches no database, so it runs on a read-only copy. A Signal.ID already seen is
// counted once (nil). It waits for the counter goroutine's acknowledgement, so Seen() is
// deterministic once it returns. Fails errs.Canceled/Timeout if ctx ends (or stop closes)
// before the counter takes or acknowledges it.
func (s *service) handleGreeted(ctx context.Context, sig signals.Signal) error
```

Who needs what:

| Member | Consumer | Pre-condition | Post-condition |
|---|---|---|---|
| `Manifest`, `New`, `init` | `module.Register`, `app` (`modules.go`), `Conformance` | — | registered under `hello` |
| `Start`, `Stop` | `module.Set` for `app`; `Conformance` (`start_read_only`, `subscribes_every_declared_name`, `stop_honors_context`, `stop_without_start_is_nil`) | built | handler subscribed, counter running; stopped and waited |
| `Routes`, `OpenAPI` | `api` through `Set.Routes`; `Conformance` (`routes_under_prefix`, `implements_declared`) | — | three routes, fragment agrees (api F10–F17) |
| `Capabilities` | `Plan.Build` (provider resolution); `Conformance` | — | `greeter.Greeter` under `greeter.Name` |
| `Contributions` | `Plan.Build` (`Accepts`); hello's own `PublicGreeting` via `Links.Contributors` | — | one `greeter.Banner` |
| `PublicGreeting` | `GET /public-greeting`; `Greeter.Greeting` (the word only) | — | word and banners |
| `Greeting` | `GET /greeting` | actor, permission | counts |
| `Greet` | `POST /greetings` | actor, permission, writable copy | row committed, signal delivered after commit |
| `handleGreeted` | `signals` (through `Start`'s `Subscribe`); tests via `signalstest.Fake.Deliver` | payload has `person` | `counter.Seen()` grew by one, once per `Signal.ID` |
| `ExportPerson`, `DeletePerson` | `people` through `Set.PersonData` (M4 command, later self-service); `Conformance` | non-zero ID | JSON object; rows gone |
| `store.*` | the service; `postgres` integration test | — | rows as named |

### Package `postgres` (`internal/modules/hello/postgres`)

```go
// Migrations is the module's migration set, rooted at the files (db.md: NNNN_name.sql at
// the set root): 0001_init.sql creates schema hello, table hello.greeting and its index.
// It is fs.Sub(embedded, "migrations") resolved in init; that Sub failing on an embedded
// directory is impossible, so init panics (go-conventions). Handed to the Manifest;
// applied by `bms migrate` (D17).
var Migrations fs.FS

//go:embed migrations/*.sql
var embedded embed.FS

// NewStore returns the store over d. Fails with errs.Invalid when d is nil.
func NewStore(d db.DB) (*Store, error)

// Insert adds one greeting. Runs inside the caller's transaction when there is one.
// Fails errs.Conflict on a duplicate ID, errs.Invalid on a check or not-null violation,
// errs.Unavailable wrapping db.ErrReadOnly on a read-only copy, Timeout/Canceled, and
// every other kind db.Wrap assigns.
func (s *Store) Insert(ctx context.Context, g greeting.Greeting) error
// CountByPerson counts the person's greetings (0 for an unknown person; cannot be
// NotFound). Fails with db's kinds.
func (s *Store) CountByPerson(ctx context.Context, person model.PersonID) (int, error)
// ListByPerson returns the person's greetings oldest first (empty for an unknown person).
// Fails with db's kinds.
func (s *Store) ListByPerson(ctx context.Context, person model.PersonID) ([]greeting.Greeting, error)
// DeleteByPerson removes the person's greetings and returns how many. Fails
// errs.Unavailable wrapping db.ErrReadOnly on a read-only copy, and with db's kinds.
func (s *Store) DeleteByPerson(ctx context.Context, person model.PersonID) (int, error)
```

Every error leaves through `db.Wrap(op, err)`; the SQL lives in `postgres/sql/*.sql`
embedded with `go:embed` (postgres-sql skill).

### HTTP (`http.go`, `openapi.json`)

| Route | Access | Permission | 2xx | Body |
|---|---|---|---|---|
| `GET /public-greeting` | public | none | 200 `HelloPublicGreeting{greeting, banners: [HelloBanner{module, key}]}` | — |
| `GET /greeting` | signed in | `hello.greetings.read` | 200 `HelloGreeting{greeting, mine, seen}` | — |
| `POST /greetings` | signed in | `hello.greetings.write` | 201 `HelloGreeted{id, greeting, created_at}` | none (decision 7) |

Handlers call the service and `httpx.WriteJSON`; on error `httpx.Fail(w, r, err)` and
return (api decision 2). When `WriteJSON` itself fails for a reason other than `Canceled`,
the handler calls `httpx.Fail` too. No handler reads the body, parses a path ID, starts a goroutine or
changes state on `GET`. Every error status is api's mapping of the service's kind: 401
`unauthenticated`, 403 `permission`, 503 `read_only` (sentinel), 503 `unavailable`, 504
`timeout`, 500 `internal`. The fragment: paths `/public-greeting`, `/greeting`, `/greetings`;
`components.schemas` `HelloPublicGreeting`, `HelloBanner`, `HelloGreeting`, `HelloGreeted`
(`id` is `$ref: ID`, `created_at` is `date-time`); tag `hello`; `$ref`s point only at its own
schemas and the base ones (api F16). The web client's types come from this document.

## Types

```go
// PublicGreeting is GET /public-greeting's body.
type PublicGreeting struct {
	Greeting string   `json:"greeting"`
	Banners  []Banner `json:"banners"` // never null: an empty array when nobody contributes
}

// Banner is one contribution, as the screen needs it.
type Banner struct {
	Module string `json:"module"` // Contribution.Module
	Key    string `json:"key"`    // greeter.Banner.Key
}

// Greeting is GET /greeting's body.
type Greeting struct {
	Greeting string `json:"greeting"`
	Mine     int    `json:"mine"` // the actor's own rows
	Seen     int    `json:"seen"` // hello.greeted deliveries this process counted since Start
}
```

Uses `model.PersonID`, `greeting.Greeting`, `greeting.ID`, `module.Export`,
`module.Deletion`, `signals.Signal`.

## Data

Schema `hello`, created by `db`'s migrator; migration `postgres/migrations/0001_init.sql`
(version 1):

| Column | Type | Sensitivity | Why collected | Retention |
|---|---|---|---|---|
| `id` | `uuid` primary key (constraint `greeting_pkey`) | — | identify the row | with the row |
| `person_id` | `uuid` not null | `internal` (links a row to a person; shown only as the person's own count) | export, deletion and the signal payload need the owner | kept until `DeletePerson` (shipped default keep forever, Q-RETAIN); a retention setting arrives with `settings` and `jobs`, named `hello.retention.internal` by the core's convention |
| `greeting` | `text` not null, check `octet_length(greeting) BETWEEN 1 AND 64` (bytes, matching `greeting.MaxBytes`), constraint `greeting_text_length_chk` | `internal` (a copy of the setting's word at the time) | shows which word was current | with the row |
| `created_at` | `timestamptz` not null | `internal` | ordering and export | with the row |
| `updated_at` | `timestamptz` not null default now() | `internal` | every table has it (postgres-sql) | with the row |

Index `greeting_person_created_idx (person_id, created_at)`. No foreign key: the core has no
person table in M0; migration `0002` adds `references core.person(id) on delete cascade`
when `people.md` ships its table (M2), with `DeletePerson` staying the explicit path
(people runs it before deleting the person). No `audit` reads: nothing here is `personal`
or `sensitive`. Nothing is tied to an event: there is no carry-over to a new event and no
history to keep. Turning the module off leaves the table in place.

Operations and their permission: `Insert` behind `hello.greetings.write` (own row only:
`person_id` is always the actor); `CountByPerson` behind `hello.greetings.read` (own rows
only: the query is filtered by the actor); `ListByPerson` and `DeleteByPerson` run as the
system for `people` behind `people.data.export`/`people.data.delete`, which `people`
checks and audits; the read-only public route reads no row.

## Permissions

| Permission | Allows | Scoped by | Reaches | Default roles |
|---|---|---|---|---|
| `hello.greetings.read` | `GET /greeting`: see one's own greeting count and the process counter | site; the data is own records only | internal | none shipped; proposed: every signed-in person's base role once `authz` defines roles (Open question 1). Until M2 the shell shows the entry to every signed-in person and the screen shows its forbidden state |
| `hello.greetings.write` | `POST /greetings`: save a greeting as oneself | site; writes only the actor's row | internal | as above |

Two permissions, not one (decision 1): module-contract's hello paragraph predates the
write route `app.md` asked for; a reference that gates a write with a read permission would
be copied.

## Signals

**Published:** `hello.greeted`, payload `{person: model.PersonID}` built with
`signals.NewPayload(signals.Ref(PayloadPerson, actor))`, published by `Greet` inside the
`db.Transact(Write)` that inserts the row, so a rolled-back insert publishes nothing and
delivery runs after commit (signals decision 1). The payload carries an ID only: no word,
no time (signals decision 4).

**Subscribed:** `hello.greeted` by `handleGreeted`, subscribed once in `Start`. It runs
as the system (signals decision 7), touches no database (works on a read-only copy), and
is idempotent on `Signal.ID` (the counter remembers the last `seenRing` IDs and ignores a
repeat). If it fails, `signals` logs and counts the delivery (`outcome=failed`) and the
publisher is unaffected (signals decision 3); nothing is lost except one increment of a
process-local counter. Nothing else subscribes to `hello.greeted` in M0; a module that
does and is disabled is a graph `Warning`, not hello's concern.

**Later:** when the core publishes a signal (`people.person-deleted`, M2), `hello`
subscribes to it too and the handler logs the delivery at debug with the ID only, so the
reference shows a core subscription; added by that spec's Proposed changes, not now.

## Settings

| Key | Type | Default | Valid range | Who can change it |
|---|---|---|---|---|
| `hello.greeting` | text | `hello` | 1..64 bytes after trimming, no control characters (`validateGreeting`, `errs.Invalid` with field `hello.greeting`) | settings admins (`settings.md`, M2) |

The setting is declared in M0 so `Plan.Settings()` has one to carry and `Manifest.Validate`
checks a `Validate` func; its value is read through `greetingSource`, which in M0 returns
`DefaultGreeting`. There is no `Deps.Settings` before M2 (module-contract "Deps fields by
spec"), so this is the only honest wiring. Terminology: none; the one word is the setting.

## UI

`web/src/modules/hello/`: `index.ts` exporting `ui: ScreenSet` (`name: "hello"`, `kind:
"module"`, one route `""` with `handle: {title: "module.hello.title", access: "public"}`
and a lazy `screens/Greeting.tsx`; `nav: [{label: "module.hello.nav.greeting", to: "",
area: "main", access: "public"}]`; `messages: {en: () => import("./messages/en.json")}`).
It imports only `@/api`, `@/i18n`, `@/ui`, `@/registry/types`, and calls only
`/api/hello/...` (web-shell decision 4).

**Greeting screen** (`/hello/`), one column at 320 px, every control at least 44 × 44 px,
no `style` prop, logical properties (web-shell decision 21):

| Part | Who | Calls | States |
|---|---|---|---|
| Greeting heading `module.hello.screen.greeting` (`"{greeting}!"`) and the banner list (one line per banner, text from the banner's key) | everyone | `GET /api/hello/public-greeting` | loading (`Loading` after 300 ms); error (`ErrorState` with retry; `network` has no reference); banners empty: the list is omitted, nothing says "no banners" |
| Counts `module.hello.screen.mine` (`"You have greeted {mine, plural, =0 {never} one {once} other {# times}}"`) and `module.hello.screen.seen` | signed in (`useSession().signedIn`) | `GET /api/hello/greeting` | loading; forbidden (`403`: `ErrorState` with `error.permission`; the public part stays); error; `mine = 0` shows the same line (its own empty wording), no separate `EmptyState` |
| `Button` `module.hello.screen.greet` | signed in | `POST /api/hello/greetings` | pending (disabled, no double submit); success (`Toast` `module.hello.screen.greeted`, both queries refetched); `read_only` (`ErrorSummary` under the button; the shell's banner already explains); `403` (`ErrorSummary`); other errors (`ErrorSummary` with reference) |
| Signed out | everyone in M0 | — | only the public part renders; no sign-in prompt of its own (the frame has one) |

Contributed banner text: the key is dynamic by nature (it names another module's
catalogue), so the screen narrows it with the shell's runtime guard (`isMessageKey`,
Proposed change 3) and drops a banner whose key is not in the loaded catalogues (no
untranslated string is ever shown). Forbidden never confirms anything: the counts are the
actor's own.

`messages/en.json` keys: `module.hello.title` ("Hello"), `.description`,
`.permission.greetings.read.title`/`.description`,
`.permission.greetings.write.title`/`.description`, `.setting.greeting.title`/`.description`,
`.signal.greeted.title`, `.nav.greeting`, `.banner.welcome`, `.screen.greeting`,
`.screen.mine`, `.screen.seen`, `.screen.greet`, `.screen.greeted`. No `kept.*` (nothing is
kept). Other languages are optional and fall back to `en`.

## Behavior

**Seams (decision 2).** `New` wires `actor`, `allow` and `greeting` to M0 implementations:
`actor` returns `(zero, false)`, `allow` returns `errs.E("hello.allow", errs.Permission,
errDenied)` for every permission, `greeting` returns `DefaultGreeting`. When `Deps` gains
`Authz` (M2) and `Settings` (M2), and `identity` exports its accessor (M1), `New` wires
them in and nothing else changes; tests inject fakes for all three. The order inside a
signed-in use case is fixed: actor, then permission, then the span, then the work.
In M0 a real request never reaches `Greeting` or `Greet`: `api` answers 401 first.

**Ordering and transactions.** `Greet` opens one `db.Transact(ctx, db.Write, ...)` and, in
it, `Insert` then `Publish`; `Publish` failing rolls the insert back (signals decision
13). The ID comes from `model.NewID`, the time from `Deps.Clock`. `Insert` never retries
(db never retries; a `Conflict` on the fresh ID is a bug).

**Counter and goroutine (decision 3).** `Start` launches one goroutine that owns `seen int`
and the ring of recent `Signal.ID`s, fed by a buffered channel (64). `handleGreeted` sends
and then waits for the goroutine's acknowledgement, each bounded by the goroutine's stop
channel and `ctx.Done()`, so a handler never blocks past its 5 s deadline and `Seen()` is
deterministic after it returns. `Seen()` reads under a mutex the goroutine updates. `Stop` returns nil when already stopped
(even with a done ctx); otherwise it checks `ctx`, closes the stop channel and waits on a
`sync.WaitGroup` or `ctx`. A mutex alone would count; the
goroutine is there so the reference shows the lifecycle of background work (module-contract
asks for one). The counter resets at start; it is a process-local number, not data.

**Concurrency.** `*Module` and the service are safe for concurrent use after `Start`;
`Start`/`Stop` are serialised by the `Set`. `Links` is frozen and safe. The store holds a
`db.DB` and no state.

**Idempotency.** `Greet` is not idempotent (each call saves a row; the client never retries
mutations, web-shell decision 11). `DeletePerson` runs inside `db.Transact(Write)`, so a read-only copy refuses it even when
the store is an in-memory fake; it is idempotent (0 on the second call). `Greet` trims the
source word the same way the setting check does.
`handleGreeted` is idempotent on `Signal.ID`.

**Lifecycle.** `New` (no I/O) → `Start` (validate banners, subscribe, launch counter) →
requests and deliveries → `Stop` (wait for the counter). `signals.Close` runs before
`Set.Stop` (app decision 10), so no delivery reaches a stopped module. Deadlines come from
the caller (`api`'s request deadline, `signals`' 5 s, `people`'s export deadline); every
statement gets `db`'s `QueryTimeout` cap.

**Read-only copy.** `Start` succeeds; `PublicGreeting`, `Greeting`, `ExportPerson` work;
`Greet` and `DeletePerson` fail `Unavailable` wrapping `db.ErrReadOnly` (api maps to 503
`read_only`); the handler works.

**Disabled.** With `BMS_MODULES_DISABLED=hello`: no routes, no screen (the shell hides the
set), no migrations applied, table left alone; `greeter` has no provider, which is fine
because nothing requires it.

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `New` | a nil `Logger`, `Tracer`, `Clock`, `DB`, `Signals` or `Links` | Invalid | fix `Provision` | `TestNew_NilDependency_ReturnsInvalid` |
| F2 | `New` | `postgres.NewStore` fails (nil DB) | Invalid | fix `Provision` | `TestNewStore_NilDB_ReturnsInvalid` |
| F3 | `Start` | called a second time | Conflict | bug in the caller | `TestStart_Twice_ReturnsConflict` |
| F4 | `Start` | `Subscribe` refused (undeclared name, second handler, bus closed) | the bus's kind (Invalid, Conflict, Internal) | fail start-up | `TestStart_SubscribeFails_ReturnsBusKind` |
| F5 | `Start` | a `hello.banner` contribution's `Key` is not `module.<contributor>.banner.<ident>` | Invalid naming the module | fix the contributor | `TestStart_BannerKeyWrongPrefix_ReturnsInvalid` |
| F6 | `Stop` | the counter goroutine has not exited when ctx ends | Canceled / Timeout | log, continue shutdown | `TestStop_CtxDone_ReturnsTimeout` |
| F7 | `Stop` | never started, or already stopped | nil (cannot fail) | — | `TestStop_WithoutStart_IsNil` (also Conformance) |
| F8 | `PublicGreeting` | `greetingSource` fails (M2+: settings unreachable) | the source's kind | 503 | `TestPublicGreeting_SourceFails_ReturnsKind` |
| F9 | `PublicGreeting` | `Links.Contributors(PointBanner)` fails | Internal | 500; it is a bug | `TestPublicGreeting_LinksRefuses_ReturnsInternal` |
| F10 | `Greeting` | no actor in ctx | Unauthenticated | 401 | `TestGreeting_NoActor_ReturnsUnauthenticated` |
| F11 | `Greeting` | permission denied (always in M0) | Permission | 403 | `TestGreeting_Denied_ReturnsPermission`, `TestGreeting_M0Wiring_AlwaysDenies` |
| F12 | `Greeting` | `CountByPerson` fails | db's kind | 503/504/500 | `TestGreeting_StoreFails_ReturnsKind` |
| F13 | `Greet` | no actor | Unauthenticated | 401 | `TestGreet_NoActor_ReturnsUnauthenticated` |
| F14 | `Greet` | permission denied | Permission | 403 | `TestGreet_Denied_ReturnsPermission` |
| F15 | `Greet` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | 503 `read_only`; nothing published | `TestGreet_ReadOnly_ReturnsUnavailableAndPublishesNothing` |
| F16 | `Greet` | `Publish` refused | Publish's kind; insert rolled back | 400/503 | `TestGreet_PublishFails_RollsBackAndReturnsKind` |
| F17 | `Greet` | `Insert` fails | db's kind; nothing published | 503/500 | `TestGreet_InsertFails_ReturnsKindAndPublishesNothing` |
| F18 | `handleGreeted` | payload has no `person` | Invalid (from `signals.Get`) | signals logs `failed` | `TestHandleGreeted_NoPerson_ReturnsInvalid` |
| F19 | `handleGreeted` | ctx done before the counter accepts | Canceled / Timeout | signals logs | `TestHandleGreeted_CtxDone_ReturnsCanceled` |
| F20 | `handleGreeted` | same `Signal.ID` delivered again | nil; counted once | — | `TestHandleGreeted_Duplicate_CountsOnce` |
| F21 | `ExportPerson` | zero ID | Invalid | bug in `people` | `TestExportPerson_ZeroID_ReturnsInvalid` (also Conformance) |
| F22 | `ExportPerson` | `ListByPerson` fails | db's kind | `people` reports the module | `TestExportPerson_StoreFails_ReturnsKind` |
| F23 | `DeletePerson` | zero ID | Invalid | bug in `people` | `TestDeletePerson_ZeroID_ReturnsInvalid` |
| F24 | `DeletePerson` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | `people` refuses the command | `TestDeletePerson_ReadOnly_ReturnsUnavailable` (also Conformance) |
| F25 | `DeletePerson` | `DeleteByPerson` fails | db's kind | `people` reports | `TestDeletePerson_StoreFails_ReturnsKind` |
| F26 | `Greeter.Greeting` | `greetingSource` fails | the source's kind | caller hides or 503 | `TestGreeter_SourceFails_ReturnsKind` |
| F27 | `Store.Insert` | duplicate `id` | Conflict | 500; it is a bug (fresh IDs) | `TestInsert_DuplicateID_ReturnsConflict` (integration) |
| F28 | `Store.Insert` | check or not-null violation | Invalid | 400 | `TestInsert_EmptyGreeting_ReturnsInvalid` (integration) |
| F29 | `Store.*` | connection lost, statement deadline | Unavailable / Timeout | 503/504 | `TestStore_ClosedPool_ReturnsUnavailable` (integration) |
| F30 | handlers | the service returns an error | `httpx.Fail` writes api's status (fallback table in unit tests) | — | `TestHandlers_Fail_WritesStatusByKind` (401, 403, 503) |
| F31 | `Manifest.Validate`, `Route.Validate`, fragment check | a declaration drifts | Invalid / Conflict at start-up | fix the module | `Conformance/manifest_valid`, `TestOpenAPI_MatchesRoutes` |

Methods that cannot fail: `Routes`, `OpenAPI`, `Capabilities`, `Contributions` return
constants; `counter.Seen` reads an int under a mutex.

## Lock-in check

No external technology beyond PostgreSQL, reached only through `db` (PostgreSQL 16, OSI
licensed). The module imports no third-party package; `postgres/` imports pgx through
`db`'s rules. The screen uses the shell's stack only.

## Test plan

**Unit** (no infrastructure; `hello_test.go` black-box, plus `postgres` fakes): a fake
`store` (in-memory map; failure injection by method), fake `actorFunc`, an allow-all and a
deny-all `permissionChecker`, a fake `greetingSource`; `dbtest.NewFake()` (and
`SetReadOnly(true)`) as `Deps.DB`; `signalstest.NewFake(Manifest.Name, Manifest.Publishes,
Manifest.Subscribes)` as `Deps.Signals` (`Published()` after `Greet`, `Deliver` for the
handler); `telemetrytest.New(t)` for logger and tracer (asserting the span names and that
no marker text appears); `clock.NewFake`; a `module.Links` built by a fresh registry with
only `hello` enabled. Table tests for F1–F26, F30, F31; `TestGreet_PublishesInsideTransaction`
(`Fake.Published()` holds `{person}` once, `Call.InTx` true for the insert);
`TestGreet_SpanUnderParent`; `TestCapabilities_Greeter_ReturnsGreeting`;
`TestContributions_Banner_HasOwnKey`; `TestExportPerson_ReturnsGreetingsJSON` (object with
`greetings`, no `person_id`); `TestMessages_EnglishCoversKeys` is `app`'s catalogue test.

**Handlers:** `httptest` over `Routes()` mounted at `/api/hello` on a bare `ServeMux` with
no `Failer`, so `httpx.Fail`'s fallback writes the status table: 200 and body shapes, 201,
and F30. The module's own tests use only a bare `ServeMux`; tests that drive the real api
shell and check the OpenAPI fragment against routes (`TestOpenAPI_MatchesRoutes`) live in
`internal/app`, and no module test imports `internal/api`.

**Conformance:** `moduletest.Conformance(t, Manifest, factory, provision,
moduletest.WithSeed(seed))`, where `factory` builds the module over the in-memory fake
store (the one the unit tests use; the production `New` cannot be seeded because
`dbtest.Fake` holds no rows), `provision` builds the fakes above, and `seed` saves one
greeting for `subject.Person` (the greeting row has no name, e-mail or phone field, so the
other markers are unused here). `hello` requires and uses no capability, so no
`WithCapability`. Every named subtest passes.

**Integration** (`//go:build integration`, `dbtest.TempDB`, compose stack): migration
`0001` applies on an empty database and `Status` reports version 1; `Insert`, `CountByPerson`,
`ListByPerson`, `DeleteByPerson` round trip with invented IDs; F27–F29; and
`TestGreet_EndToEnd_DeliversInSameTrace`: real `db.Pool`, real `signals.Dispatcher` scoped to
`hello`, `telemetrytest` tracer, a fake actor and allow-all checker; `Greet` commits one row,
`deliver:hello.greeted` is a descendant of the `hello.greet` span's trace, and `Seen()` is 1.
(The request-driven form of this assertion waits for a session, M1: Proposed change 6.)

**Web** (`web/`): `registry.test.ts` accepts the set; `scripts/check-web-module-imports.sh`
passes; `Greeting.test.tsx` (Testing Library, mocked client) covers loading, loaded with
and without banners, `403`, `network`, signed-in counts with `mine` 0 and 3, greet success
and `read_only`; `axe` on the screen; Playwright scenarios are web-shell's, run at the
phone and desktop viewports by `make test-e2e`.

## Decisions settled here

Decisions 1 to 10 are listed in the Summary. The owner settled two more on 2026-10-17, each
with the proposed answer:

11. **Default roles for `hello.greetings.read` and `hello.greetings.write`** (owner,
    2026-10-17). Once `authz` (M2) exists, the base role every signed-in person gets holds
    both, so a fresh installation can greet without an admin touching roles.
12. **`hello` stays compiled in at the first release** (owner, 2026-10-17). It is enabled by
    default through Phase 1 so `make test-e2e` and the admin modules screen always have a
    module to show; the operator docs tell a regional to set `BMS_MODULES_DISABLED=hello`;
    revisit when `volunteers` is Implemented.

## Open questions

None. Both questions in the draft are answered above (decisions 11 and 12).

## Proposed changes elsewhere

1. (applied 2026-10-17) **`plan/design/module-contract.md` (row 4), "The `hello` reference module":** replace
   the paragraph's list with this spec's: two permissions (`hello.greetings.read`,
   `hello.greetings.write`), the write route `POST /greetings`, `greeter.Greeter{Greeting(ctx)
   (string, error)}` and `greeter.Banner{Key}` as the point's shape, one span per use case in
   the service (not only in the `GET /greeting` handler), the counter goroutine, and the core
   subscription deferred until `people.person-deleted` exists. Mark Proposed change 12 done.
2. (applied 2026-10-17) **`plan/design/api.md` (row 16), Proposed change 16:** mark applied with this wording:
   `hello`'s unit tests assert `httpx.Fail`'s fallback statuses for the kinds `hello`
   returns (401, 403, 503); `hello` has no `NotFound` path, so the `404 not_found` body is
   asserted by `app`'s e2e test on `GET /api/hello/nope`, and the 401 body on
   `GET /api/hello/greeting`.
3. (applied 2026-10-17) **`plan/design/web-shell.md` (row 17):** mark Proposed change 10 applied (the set is as
   described in "UI"). New request: export from `@/i18n` a runtime guard `isMessageKey(s:
   string): s is MessageKey` over the loaded catalogues, because extension-point
   contributions (`hello.banner` now, `people.person-page` later) carry another module's
   keys, which are dynamic by nature; without it a screen cannot translate them without a
   cast.
4. (applied 2026-10-17) **`plan/design/signals.md` (row 10), Proposed change 11:** mark applied; the handler
   counts on the counter goroutine, idempotent on `Signal.ID` with a 256-entry ring.
5. (applied 2026-10-17) **`plan/design/telemetry.md` (row 15b), Proposed change 1 (tail):** "The `hello` module
   opens one span in its handler" becomes "one span per use case in its service, under the
   request span".
6. (applied 2026-10-17) **`plan/design/app.md` (row 18), e2e test and Proposed change 7:** the write route is
   `POST /api/hello/greetings`; in M0 it answers 401 before the handler, so the
   request-driven `deliver:hello.greeted` assertion moves to M1 (first session), while
   `hello`'s own integration test `TestGreet_EndToEnd_DeliversInSameTrace` proves the same
   trace relationship in M0. Add `GET /api/hello/nope` → `404 not_found` body to
   `TestServe_EndToEnd_StartsServesStops`.
7. (applied 2026-10-17) **`plan/07-abstractions.md`, "Modules" and "contracts":** list `hello` as the reference
   module (M0, this spec), and name `contracts/greeter` as the first, example contract.
8. (applied 2026-10-17) **`.claude/skills/add-module/SKILL.md` and `go-conventions`, "Module package layout":**
   add the leaf package for record types (`<name>/<record>/`: the module root imports
   `postgres/` to build the store, so `postgres/` cannot import the root; record types shared
   by both live in a leaf that imports only `model` and `lib`), and note the M0 seams (actor,
   permission checker, setting source) and their deny/default wiring until identity, authz
   and settings exist. Point both at `hello` as the template.
9. **`plan/design/README.md`:** row 4b to Draft (done with this draft).
