# Demo

- **Status:** Draft (awaiting the owner, 2026-10-28; the owner's direction of 2026-10-28 is the
  brief, to be recorded as the context of D53 once approved)
- **Package:** `internal/core/demo` (service), `internal/core/demo/postgres` (store),
  `internal/core/demo/demotest` (fakes), `internal/contracts/demoseed` (the extension point)
  **UI:** `web/src/shell/` (the banner, in the frame), `web/src/core/demo/` (the captured-mail
  screen)
- **Depends on specs:** config, app, module-contract, db, identity, authz, people,
  departments-events, settings, notify, files, audit, jobs (only to say what it does not use),
  api, web-shell, telemetry (names only; a parallel spec designs client telemetry)
  **Used by:** `app` (the part, the commands, the adapters), the project's public demo
  instance, every module that contributes seed data (volunteers first)

## Summary

**Purpose.** Let strangers use a public copy of bms safely: made-up data in, nothing real out.
A core part (`internal/core/demo`, part 11 after `files`) plus the contract
`contracts/demoseed`. Imports `model`, `lib`, `core/db`, `core/audit`, `core/authz`,
`core/identity` (`Credentials`, `Session`, `WithSession`), `core/people`, `core/departments`,
`core/events`, `core/settings`, `core/notify` (`Channel`, `Envelope`), `core/files` (`Policy`,
`PolicySource`), `core/telemetry`, `contracts/demoseed`. Never `core/module`, `api`, `app`,
`config` or a module; only `app` imports it. With `demo.enabled` false (the default) the part
declares its empty tables and nothing else: production installs never see it.

**Exported (`demoseed`).** `Point = "demo.seed"`; `Provider{Seed(ctx, Request) (Result, error)}`;
`Request{Event, Departments map[string]model.DepartmentID, People map[string]model.PersonID,
Actors}`; `Actors{As(ctx, key) (context.Context, error)}`; `Result{Items}`; the ten `Actor*` keys,
the six `Dept*` keys, `WalkOns = 12`, `WalkOnKey(n)`.

**Exported (`demo`).** `Info{Enabled, Regional, Accounts []AccountInfo, Password}`,
`AccountInfo{Key, Email, Roles}`; `Report{Steps []Step}`, `Step{Name, Items}`; `MailID`,
`Mail{ID, To, Subject, Body, CapturedAt}`, `MailQuery{Limit}`; seams `Accounts`, `Access`,
`Profiles`, `Departments`, `Events`, `Settings`, `Schema`, `Objects` (one to three methods
each, filled by `app`); `Contributor{Module, Provider}`; `Options{Enabled, PublicURL, DB, Clock,
Logger, Tracer, Meter, Audit (scoped "demo"), Authz, Actor, Files files.PolicySource, the seams,
Schemas []string, DataDir, ContributorTimeout}` (+`Validate`); `New(Options) (*Service, error)`
(no I/O); `Link([]Contributor) error`; `Enabled() bool`; `Info() Info` (no I/O); `Seed(ctx, out
io.Writer) (Report, error)`; `Reset(ctx, out) (Report, error)`; `Protected(ctx, model.AccountID)
(bool, error)` (identity's proposed `Guard`); `Channel() notify.Channel`; `Policy(ctx)
(files.Policy, error)` (the capped `files.PolicySource`); `ListMail(ctx, MailQuery) ([]Mail,
error)`; `Routes()`, `OpenAPI()`. Constants `Password = "burn-demo-password"`, `Regional =
"Ember Flats"`, `Domain = "demo.invalid"`, `Zone`, the caps (2 MiB, 256 MiB, 5), `MaxMail` 200,
`MaxMailList` 50, `SeedVersion` 1, `DefaultContributorTimeout` 2 min, `SessionLifetime` 1 h;
`demotest` fakes for the seams and `NewFakeActors()`.

**Key decisions.** 1 one switch, `demo.enabled` (`BMS_DEMO_ENABLED`, default false), plus
`demo.public_url`; demo mode never changes permissions, audit, the privacy filter, validation,
cookies, CSP or the superuser rules. 2 what changes: the banner and account list (`GET
/api/demo`), captured mail instead of SMTP, the upload cap, protected accounts, `X-Robots-Tag:
noindex`, the two commands. 3 the seed acts **as the demo accounts** through the services
(`identity.WithSession`); only two writes are actor-less bootstrap of the kind `bms migrate`
makes: `identity.EnsureAccount` and one `authz.EnsureGrant` (both proposed). 4 modules seed
their own data through the extension point `demo.seed`; the core never touches module data.
5 `bms demo seed` refuses a non-empty or seeded database; `bms demo reset` wipes and reseeds.
6 reset = clear objects and the audit spool, `DROP SCHEMA ... CASCADE` for every set's schema,
`Migrate` as `bms migrate`, seed; the superuser goes with the rest (`bms create-superuser`
recreates one). 7 the automatic reset is an operator timer shipped disabled
(`deploy/systemd/`), run with `serve` stopped; not a job. 8 two guards: the commands refuse
unless `demo.enabled`, and reset refuses without the marker row (`core.demo_state`) the seed
writes first. 9 identity consults a `Guard`: a demo account cannot change or reset its password
or address, be disabled or deleted (`Permission`, field `account`, code `protected`); anything
else a tester breaks, the next reset repairs. 10 demo never sends mail: the capture `Channel`
stores envelopes in `core.demo_mail` (newest `MaxMail`), shown to signed-in people on
`/demo/mail` (`demo.mail.read`, in `participant`, audited). 11 uploads are capped by wrapping
files' `PolicySource`. 12 the data is a constant table (fictional "Ember Flats", `.invalid`
addresses, 555-01xx phones, invented playa names) with dates relative to the clock; IDs differ.
13 one shared password, printed on the sign-in page. 14 `Info` does no I/O. 15 a failed seed
step fails the command and leaves the marker in `seeding`; `reset` is the recovery.

**Errs kinds callers handle.** `Seed`: `Unsupported` (off), `Conflict` (`seeded`, `not_empty`),
`Unavailable` wrapping `db.ErrReadOnly`, `Permission` (a demo actor lacks a permission: the
starter roles or the table are wrong), `Timeout` (contributor), any seam's or contributor's
kind wrapped with the step. `Reset`: `Unsupported`, `Conflict not_demo`, `Unavailable` wrapping
`db.ErrReadOnly`, the migrator's and store kinds. `Protected`: store kinds (identity fails
closed). `Channel.Send`: `Invalid` wrapping `notify.ErrRejected`, `Unavailable`. `ListMail`:
`Unauthenticated`, `Permission`, `Unsupported`, `Invalid` (`limit` `range`), the recorder's and
store kinds. `Policy`: the inner kind. `Info`, `Enabled`, `Channel` cannot fail. `New`/`Link`:
`Invalid`, `Internal`. Exit statuses: off, not empty, seeded, not demo → 2; schema → 3; else 1.

**Owns.** Tables `core.demo_state`, `core.demo_account`, `core.demo_mail`; set `db.Set{Name:
"demo", Schema: "core"}` after `files`; config `demo.enabled`, `demo.public_url`; permission
`demo.mail.read`; extension point `demo.seed`; route set `demo` (`GET /api/demo` public, `GET
/api/demo/mail` signed-in); commands `bms demo seed|reset`; screen `/demo/mail`, the frame's
`DemoBanner`, keys `shell.demo.*`, `core.demo.*`; audit `demo.seed.ran`, `demo.reset.ran`,
`demo.mail.read`; spans `demo.seed`, `demo.seed.step`, `demo.reset`, `demo.mail.capture`;
metrics `bms.demo.seeds{outcome}`, `bms.demo.resets{outcome}`, `bms.demo.mail_captured`. No
settings, signals or jobs.

**Expects of dependencies** (each a numbered Proposed change): identity `EnsureAccount`, `Guard`,
`WithSession`; authz `EnsureGrant`, `Grant`, `ListRoles`; people `UpdateMe`, `Create`, `Conflict
protected`; departments/events/settings `Create`, `AddScheduleItem`, `Set`; notify a swappable
`ChannelFactory`; files `PolicySource`; api `Options.NoIndex`; web-shell the banner, `useDemo()`;
module-contract `demo` reserved, `demo.seed` core point; volunteers a `Provider`; db the set order.

## Purpose

Let strangers use a public copy of bms safely.

## Responsibilities

- The demo switch: say whether this process runs in demo mode and what that changes
  (`Enabled`, `Info`).
- The public banner data: the fictional regional's name, the demo accounts and their shared
  password, for the frame and the sign-in page.
- Seeding: build one complete, obviously fictional regional (people, departments, an upcoming
  event, grants, settings) through the services, as the demo accounts, and ask each enabled
  module to add its own data through `demo.seed`.
- Reset: wipe what strangers entered (database, objects, spool) and seed again, only on a
  database the seed marked as a demo.
- Protecting the shared accounts from being locked for everyone (the `Guard`).
- Keeping mail inside the demo: a capture channel and a screen to read what was captured.
- Capping uploads below the regional-settable limits.

## Not responsible for

- Client telemetry (what testers click, where they stumble): the parallel telemetry spec.
  This spec names only the spans and metrics demo itself emits.
- Scheduling the reset: the operator's timer (`deploy/systemd/`, shipped disabled) and
  `docs/operating.md`. bms runs the reset when asked; it never decides when.
- Rate limiting sign-ins, resets and client errors: identity and telemetry's intake, unchanged
  in demo mode; address-level limits are the reverse proxy's.
- Seeding a module's data: the module, through `demoseed.Provider`. The core seeds only what
  the core owns.
- Deciding who may do what: authz, unchanged. A demo account holds its starter role, nothing
  more.
- Sending mail: notify owns the outbox; demo only replaces the `Channel` it delivers through.
- Deleting personal data a stranger typed in: the reset drops it with everything else;
  between resets, `people`'s export and deletion work as in production.
- The superuser: identity's; not a demo account, not printed, not recreated by the seed.

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| Mode | Say whether this process is a demo. | `Enabled`, `Info`; no I/O |
| Seed | Build the fictional regional through the services as the demo accounts. | `Seed`, `demoseed.Point`, `Actors` |
| Reset | Return a demo database to the seeded state. | `Reset`; the marker guard |
| Guard | Keep a shared demo account usable by everyone. | `Protected`; identity's seam |
| Mail capture | Keep every notification inside the demo. | `Channel`, `ListMail`, `/demo/mail` |
| Upload cap | Bound what a stranger can upload whatever the settings say. | `Policy` over files' `PolicySource` |

## Interface

```go
// Package demoseed is the contract between the core demo seed and the modules that add
// their own demo data. It imports only model.
package demoseed

// Point is the extension point the core defines; a module lists it in
// Manifest.ContributesTo and returns a Provider from Contributions().
const Point = "demo.seed"

// Actor keys. Request.People holds a PersonID for each; Actors.As accepts each.
const (
	ActorParticipant   = "participant"    // base role only; booked on shifts
	ActorNewcomer      = "newcomer"       // base role only; empty profile (the first-run flow)
	ActorLead          = "lead"           // department-admin at Rangers
	ActorCoordinator   = "coordinator"    // volunteer-coordinator
	ActorSteward       = "steward"        // department-steward
	ActorEventAdmin    = "event-admin"    // event-admin
	ActorSettingsAdmin = "settings-admin" // settings-admin
	ActorAccountAdmin  = "account-admin"  // account-admin
	ActorAccessAdmin   = "access-admin"   // access-admin
	ActorAuditReviewer = "audit-reviewer" // audit-reviewer
)

// Department keys. Request.Departments holds a DepartmentID for each.
const (
	DeptRangers   = "rangers"
	DeptGate      = "gate"
	DeptGreeters  = "greeters"
	DeptMedical   = "medical"
	DeptDPW       = "dpw"
	DeptSanctuary = "sanctuary"
)

// WalkOns is how many walk-on people (no account) the core seeds; WalkOnKey(1..WalkOns)
// names them in Request.People.
const WalkOns = 12

// WalkOnKey returns "walkon-NN"; it panics for n outside 1..WalkOns (a programming error).
func WalkOnKey(n int) string

// Request is what a Provider receives: the IDs the core created, by key, and a way to act.
type Request struct {
	Event       model.EventID
	Departments map[string]model.DepartmentID
	People      map[string]model.PersonID
	Actors      Actors
}

// Actors lets a provider run its own use cases as a demo account. As fails with
// errs.Invalid for an unknown key. The returned ctx carries that account's session for
// SessionLifetime; a provider calls As once per actor and reuses the ctx.
type Actors interface {
	As(ctx context.Context, key string) (context.Context, error)
}

// Result is what a provider reports: how many records it created.
type Result struct{ Items int }

// Provider seeds one module's demo data. It runs after the core's steps, under
// ContributorTimeout, against a database that holds no row of the module's yet. It uses
// the module's own services through the actors it is given, so every permission, audit
// and validation rule applies. It must create only data a Validate accepts and may not
// write anything about a real person. A failure fails the whole seed (decision 15).
type Provider interface {
	Seed(ctx context.Context, req Request) (Result, error)
}
```

```go
package demo // internal/core/demo

// Constants. Password and the addresses are public by design: they are printed on the
// sign-in page of a public demo. Nothing here is a secret.
const (
	Password = "burn-demo-password" // one password for every demo account (decision 13)
	Regional = "Ember Flats"        // the fictional regional (decision 12)
	Domain   = "demo.invalid"       // RFC 2606: can never be a real mailbox
	Zone     = "America/Chicago"    // the fictional regional's zone

	MaxUploadBytes = 2 << 20   // files cap in demo mode, whatever files.max_bytes says
	MaxQuotaBytes  = 256 << 20 // site-wide quota cap in demo mode
	MaxPerRecord   = 5         // per-record cap in demo mode
	MaxMail        = 200       // captured mail rows kept (oldest trimmed on insert)
	MaxMailList    = 50        // ListMail page
	SeedVersion    = 1         // bumped when the seed table changes shape

	DefaultContributorTimeout = 2 * time.Minute
	SessionLifetime           = time.Hour // the seed's sessions
	seedTimeout               = 10 * time.Minute
)

// Info is GET /api/demo. When Enabled is false every other field is zero and omitted.
type Info struct {
	Enabled  bool          `json:"enabled"`
	Regional string        `json:"regional,omitempty"`
	Accounts []AccountInfo `json:"accounts,omitempty"`
	Password string        `json:"password,omitempty"`
}

// AccountInfo is one row of the sign-in page's table. Roles are starter role keys, a
// department-scoped one as "<role>@<department key>" (e.g. "department-admin@rangers");
// the UI translates both through its catalogue.
type AccountInfo struct {
	Key   string   `json:"key"`
	Email string   `json:"email"`
	Roles []string `json:"roles"`
}

// Report is what Seed and Reset print: one Step per named step, in order.
type Report struct{ Steps []Step }
type Step struct {
	Name  string // "accounts", "profiles", "grants", "departments", "event", "settings", "walk-ons", "module:<name>", "drop", "migrate", "objects", "spool"
	Items int
}

// MailID identifies a captured message.
type MailID = model.ID[mail]

// Mail is one captured notification. To, Subject and Body are personal (a self-sign-up
// may have typed a real address despite the banner); they live until trimmed or reset.
type Mail struct {
	ID         MailID      `json:"id"`
	To         model.Email `json:"to"`
	Subject    string      `json:"subject"`
	Body       string      `json:"body"`
	CapturedAt time.Time   `json:"captured_at"`
}

// MailQuery: Limit 1..MaxMailList, zero = MaxMailList. Newest first.
type MailQuery struct{ Limit int }

// Seams app fills from the real services. Each is the smallest slice of the service the
// seed uses; tests use demotest fakes.
type Accounts interface {
	// EnsureAccount is identity's proposed writer-only method (Proposed change 3).
	EnsureAccount(ctx context.Context, c identity.Credentials) (identity.AccountResult, error)
}
type Access interface {
	EnsureGrant(ctx context.Context, in authz.GrantInput) (authz.Grant, bool, error) // proposed, bootstrap only
	Grant(ctx context.Context, in authz.GrantInput) (authz.Grant, error)             // as the demo access-admin
	ListRoles(ctx context.Context) ([]authz.Role, error)                              // role keys to IDs
}
type Profiles interface {
	UpdateMe(ctx context.Context, in people.ProfileInput) (people.Person, error) // as each demo account
	Create(ctx context.Context, in people.ProfileInput) (people.Person, error)   // the walk-ons, as account-admin
}
type Departments interface {
	Create(ctx context.Context, in departments.CreateInput) (departments.Department, error) // as steward
}
type Events interface {
	Create(ctx context.Context, in events.CreateInput) (events.Created, error)                                   // as event-admin
	AddScheduleItem(ctx context.Context, e model.EventID, in events.ScheduleInput) (events.ScheduleItem, error) // as event-admin
}
type Settings interface {
	Set(ctx context.Context, ch settings.Changes) (settings.Revision, error) // as settings-admin
}
// Schema is app's migrate step (db.Migrator over every set, then authz.Seed), the same
// code path bms migrate runs.
type Schema interface {
	Migrate(ctx context.Context) error
}
// Objects clears every object of every bucket and the files temp dir; app builds it over
// objects.Store (List + Delete per bucket named by the plan) and cfg.Files.Dir.
type Objects interface {
	Clear(ctx context.Context) (deleted int, err error)
}

// Contributor pairs a module name with its provider (from Set.Contributors(demoseed.Point)).
type Contributor struct {
	Module   string
	Provider demoseed.Provider
}

// Options. Validate fails Invalid naming the field (demo.db, demo.clock, demo.logger,
// demo.tracer, demo.audit, demo.authz, demo.actor, demo.files, demo.accounts, demo.access,
// demo.profiles, demo.departments, demo.events, demo.settings, demo.schema, demo.objects,
// demo.schemas (empty or a name outside the db schema grammar), demo.data_dir,
// demo.contributor_timeout (outside 10s..30m), demo.public_url (not canonical per settings
// decision 15)). PublicURL may be empty.
type Options struct {
	Enabled            bool
	PublicURL          string
	DB                 db.DB
	Clock              clock.Clock
	Logger             *slog.Logger
	Tracer             telemetry.Tracer
	Meter              telemetry.Meter
	Audit              audit.Recorder    // scoped "demo"
	Authz              authz.Authorizer  // ListMail's permission check
	Actor              authz.ActorFunc   // ListMail's actor
	Files              files.PolicySource // the inner (settings-backed) source Policy caps
	Accounts           Accounts
	Access             Access
	Profiles           Profiles
	Departments        Departments
	Events             Events
	Settings           Settings
	Schema             Schema
	Objects            Objects
	Schemas            []string // every migration set's schema, deduplicated ("core", "volunteers", ...)
	DataDir            string   // for the audit spool directory
	ContributorTimeout time.Duration
}

// New validates and builds the service. No I/O; works against db.None().
func New(o Options) (*Service, error)

// Link gives the service the module contributors, once, from app's link step.
// Internal when called twice or given a nil provider; Invalid for an empty module name.
func (s *Service) Link(contributors []Contributor) error

// Enabled reports the switch. Cannot fail. Consumers: app (check, serve's log line).
func (s *Service) Enabled() bool

// Info builds GET /api/demo from constants. No I/O, cannot fail. Consumers: the route,
// the frame's banner and sign-in table, bms check.
func (s *Service) Info() Info

// Seed builds the fictional regional (Behavior, "The seed"). Writes the demo marker first.
// Prints one line per Step to out, then the account table. Consumer: bms demo seed.
func (s *Service) Seed(ctx context.Context, out io.Writer) (Report, error)

// Reset clears objects and the spool, drops every schema in Schemas, runs Schema.Migrate,
// then Seed. Refuses without the marker. Consumer: bms demo reset.
func (s *Service) Reset(ctx context.Context, out io.Writer) (Report, error)

// Protected answers whether an account is a demo account (a row in core.demo_account).
// false, nil for an unknown account. Consumer: identity through its Guard seam.
func (s *Service) Protected(ctx context.Context, a model.AccountID) (bool, error)

// Channel returns the capture channel: Send stores the envelope in core.demo_mail (and
// trims to MaxMail), Ping answers nil. Cannot fail. Consumer: app's ChannelFactory when
// Enabled.
func (s *Service) Channel() notify.Channel

// Policy answers the inner policy with MaxBytes <= MaxUploadBytes, QuotaBytes <=
// MaxQuotaBytes (0 becomes MaxQuotaBytes) and MaxPerRecord <= MaxPerRecord. Consumer:
// app passes the service as files' PolicySource when Enabled.
func (s *Service) Policy(ctx context.Context) (files.Policy, error)

// ListMail lists captured mail, newest first, for a signed-in holder of demo.mail.read;
// records demo.mail.read per returned row. Consumer: GET /api/demo/mail.
func (s *Service) ListMail(ctx context.Context, q MailQuery) ([]Mail, error)

// Routes and OpenAPI: GET / (public) and GET /mail (signed-in), relative to /api/demo.
// Route is the shape audit and identity export; app converts to module.Route.
func (s *Service) Routes() []Route
func (s *Service) OpenAPI() []byte
```

Pre- and post-conditions per method are in Behavior; failure modes in the table.

## Types

Defined here: `Info`, `AccountInfo`, `Report`, `Step`, `MailID`, `Mail`, `MailQuery`, the seams,
`Contributor`, `Options`, `Route`. Uses `model.AccountID`, `PersonID`, `DepartmentID`,
`EventID`, `Email`; `identity.Credentials`, `AccountResult` (proposed), `Session`;
`authz.GrantInput`, `Grant`, `Role`; `people.ProfileInput`, `Person`; `departments.CreateInput`;
`events.CreateInput`, `ScheduleInput`, `Created`; `settings.Changes`; `notify.Channel`,
`Envelope`; `files.Policy`; `audit.Entry`, `Ref`.

## Data

Set `db.Set{Name: "demo", Schema: "core"}`, migrated on every install (the tables are empty
and unread when demo is off; a conditional set would make `Protected` undefined).

| Table | Columns | Sensitivity | Retention |
|---|---|---|---|
| `core.demo_state` | `id smallint PK CHECK (id = 1)`, `seed_version int`, `instance text`, `started_at timestamptz`, `seeded_at timestamptz NULL` (null = seeding or failed) | internal | until reset |
| `core.demo_account` | `account_id uuid PK REFERENCES core.account(id) ON DELETE RESTRICT`, `person_id uuid REFERENCES core.person(id) ON DELETE RESTRICT`, `key text UNIQUE`, `email text`, `position int` | internal (generated addresses, not a person's) | until reset |
| `core.demo_mail` | `id uuid PK`, `to_address text`, `subject text`, `body text`, `message_id text`, `captured_at timestamptz` | `to_address`, `subject`, `body`: personal (a stranger may have typed a real address; a body may name them) | the newest `MaxMail` rows; all gone at reset |

Index `demo_mail_captured_at_idx (captured_at DESC)`. The foreign keys are the backstop behind
the `Guard`: even a direct deletion cannot remove a demo account's rows while the marker table
references them.

## Permissions

| Permission | Allows | Scoped by | Reaches | Default roles |
|---|---|---|---|---|
| `demo.mail.read` | `ListMail`, the `/demo/mail` screen | site | personal | `participant` (the base role: every signed-in tester) |

`GET /api/demo` needs none (public by design). `Seed` and `Reset` are commands: the operator has
the database; audited as `ActorOperator` (`demo.seed.ran`, `demo.reset.ran`) with each step's
own entries recorded by the demo account that performed it. `Protected` and `Policy` are system
reads (no actor). `Channel.Send` runs inside notify's sender loop (system).

## Signals

None published, none subscribed. The seed is a command; nothing reacts to it. (A later
"demo.seeded" signal needs a consumer first.)

## Settings

None owned. The seed **writes** other areas' settings as the demo settings-admin (Behavior):
`regional.name`, `regional.language`, `regional.timezone`, `regional.public_url` (from
`demo.public_url` when set), `notify.smtp_host`, `notify.sender_address`, `notify.sender_name`.
Testers may change them; the reset restores them.

### Config (owned here, declared in `config.md` by Proposed change 1)

| Key | Env | Type | Default | Valid | Error |
|---|---|---|---|---|---|
| `demo.enabled` | `BMS_DEMO_ENABLED` | bool | `false` | `true`/`false` | `Invalid` `BMS_DEMO_ENABLED: malformed` |
| `demo.public_url` | `BMS_DEMO_PUBLIC_URL` | string | empty | an `http`/`https` URL with a host, no query, fragment or userinfo, at most 256 bytes; empty allowed; set while `demo.enabled` is false is an error (an operator who set it meant demo mode) | `Invalid` `BMS_DEMO_PUBLIC_URL: malformed` / `: requires BMS_DEMO_ENABLED` |

The public URL is configuration, not a preserved setting, because a reset must be able to
restore it without an operator and the value describes this deployment, not a regional.

## UI

**The banner (`web/src/shell/DemoBanner`).** Rendered by the frame on every screen, above the
navigation, when `useDemo().enabled` is true (`GET /api/demo` read once at boot with
`/api/settings/public`; a failed read hides the banner and is reported like a failed nav
catalogue load, with the same retry). Not dismissible. `role="note"`, `aria-labelledby` its
heading. Text (keys `shell.demo.banner.title`, `.body`), in English:
"**Public demo.** This is a shared demo of bms. Everything you enter is visible to strangers
and is wiped regularly. Use made-up people only: never enter a real name, e-mail address,
phone number or anything about a real person." Phone-first: wraps, no fixed height; 44 px
link targets. It links to `/demo/mail` ("Captured mail") when signed in.

**The sign-in page.** On `/accounts/sign-in` the frame renders the banner expanded with the
account table (`shell.demo.accounts.title` "Try a demo account", columns Account, Role,
E-mail; the shared password once above the table with a copy button; `shell.demo.password`
"Password for every demo account: {password}"). Role labels are catalogue keys
`core.demo.account.<key>` (10 keys, `en` and `es`); a `@<dept>` suffix renders as "at {dept}"
with `core.demo.department.<key>`. The accounts area itself is unchanged: the tester types or
pastes the address. Self sign-up stays per `accounts.signup` (the sign-up link is identity's).

**Captured mail (`web/src/core/demo/`, route `/demo/mail`, `kind: "core"`, no nav entry).**
A list newest first: to, subject, captured time (`useFormat` relative), the body expanded on
tap (plain text, links clickable so a tester can complete a password reset). Reads
`GET /api/demo/mail`. States: loading; empty (`core.demo.mail.empty` "No mail has been captured
yet. Mail the demo would send appears here instead of being delivered."); forbidden (a `403`
shows `ErrorState`); off (`501` shows `core.demo.mail.off` "This is not a demo instance.");
error with reference and retry. A note above the list: "Anyone signed in to the demo can read
this mail, including password-reset links." Phone-first.

**The password screen (accounts area, Proposed change 10).** A `403` with field `account` code
`protected` shows `core.accounts.field.protected` ("This is a shared demo account. Its
password and e-mail address can't be changed."). The account admin screen shows the same for
disable, and the people deletion screen for `Conflict protected`.

## Behavior

**Mode.** `Enabled` is `Options.Enabled`, fixed for the process (configuration is not
reloaded). `Info` with `Enabled` false is `{enabled: false}`; with true it lists the ten
accounts from the seed table in `position` order, with `Password` and `Regional`. It reads
nothing, so the banner stands even while the database is down. `serve` logs
`demo mode on` (warn) after "bms starting"; `bms check` prints `demo: off` or
`demo: on (seeded <time>|not seeded|seeding)`, reading `core.demo_state` (a failed read prints
`demo: on (state unknown: <kind>)` and is not a check failure).

**What demo mode never changes.** Permissions and starter roles, the authorizer, audit
(every entry is recorded as in production, with the demo account as actor), the privacy filter
and visibility settings, validation, identity's limits and the common-password list, the
cookie flags (the demo instance sits behind TLS like any install), CSP and the other headers,
telemetry scrubbing, retention jobs, the superuser rules. A test (`TestDemo_ChangesOnlyTheListed`)
asserts that with `Enabled` true and no other difference the assembly's permission catalogue,
settings catalogue, signal names and route list differ from the production assembly only by
the `demo` route set.

**The seed** (`Seed`; one span `demo.seed`, a child `demo.seed.step{step}` per step; under
`seedTimeout`; refuses when `Enabled` is false; refuses on a read-only copy).

0. *Marker.* In one `Write` transaction: refuse `Conflict seeded` if `core.demo_state` has a
   row; refuse `Conflict not_empty` if `core.account` has a non-superuser row (the one direct
   read of another core part's table in this package, decision 20); insert the row with
   `seeded_at NULL`. From here a failure leaves the marker, so `reset` can recover.
1. *Accounts.* For each of the ten actor keys, in order: `Accounts.EnsureAccount(ctx,
   Credentials{Email: "<key>@demo.invalid", Password: Password})` (writer-only, operator
   actor, publishes `identity.account-created`), then insert the `core.demo_account` row.
   Addresses: `participant@`, `newcomer@`, `lead@`, `coordinator@`, `steward@`,
   `event-admin@`, `settings-admin@`, `account-admin@`, `access-admin@`,
   `audit-reviewer@` at `demo.invalid`.
2. *Profiles.* As each account (`As(key)`, decision 3): `Profiles.UpdateMe` with the table
   below. The newcomer gets no profile (an empty, incomplete one, to show the first-run
   prompt). `UpdateMe` on a missing row creates it (people decision 2), so the seed does not
   depend on the signal's delivery timing.
3. *Grants.* Bootstrap: `Access.EnsureGrant({Person: access-admin, Role: access-admin})`
   (writer-only; the one actor-less grant). Then as `access-admin`, `Access.Grant` for:
   steward → `department-steward`; event-admin → `event-admin`; settings-admin →
   `settings-admin`; account-admin → `account-admin`; audit-reviewer → `audit-reviewer`;
   coordinator → `volunteer-coordinator`. Role IDs come from `ListRoles` by key; a missing
   starter role (a regional deleted it; impossible on a fresh schema) fails `NotFound`. The
   self-grant rule keeps `access-admin` to its one role.
4. *Departments.* As `steward`: `Departments.Create` for Rangers ("Keep the event safe and
   calm: patrols, perimeters, the radio"), Gate ("Tickets, waivers and wristbands at the
   entrance"), Greeters ("Welcome arrivals and point them to camp"), Medical ("First aid and
   the medical tent"), DPW ("Build and strike the city"), Sanctuary ("Peer support and a
   quiet place"); each with `ContactEmail` `<key>@demo.invalid`. Then as `access-admin`:
   `Grant({Person: lead, Role: department-admin, Department: Rangers})`.
5. *Event.* As `event-admin`: `Events.Create{Name: "Ember Flats <year>", Kind: burn, Zone,
   Dates}` where the start is 10:00 on the first Thursday at least 21 days after the seed
   date in `Zone`, the end 12:00 the following Monday, `<year>` the start's year. Schedule
   items: `gate-open` "Gate hours" (Thu 10:00 to Sun 18:00), `effigy-burn` "Effigy burn"
   (Sat 21:00, a moment), `temple-burn` "Temple burn" (Sun 20:30, a moment), `exodus`
   "Exodus deadline" (Mon 12:00, a moment).
6. *Settings.* As `settings-admin`: `Settings.Set` with `regional.name` = `Regional`,
   `regional.language` = `en`, `regional.timezone` = `Zone`, `regional.public_url` =
   `Options.PublicURL` when set, `notify.smtp_host` = `demo.invalid`,
   `notify.sender_address` = `noreply@demo.invalid`, `notify.sender_name` = "Ember Flats
   (demo)". The SMTP values make `notify.Configured` true so flows that need mail run; the
   capture channel ignores them.
7. *Walk-ons.* As `account-admin` (`people.profiles.manage`): `Profiles.Create` for twelve
   people with a playa name, pronouns and, for six of them, a legal name and a 555-01xx
   phone; no account, no contact e-mail. Keys `walkon-01`..`walkon-12`.
8. *Contributors.* For each `Contributor` in build order: `Provider.Seed(ctx, Request)`
   under `ContributorTimeout`, `Request` holding the event, the six department IDs, the
   twenty-two person IDs and `Actors`. A failure fails the seed with the module's name in
   the error (`fmt.Errorf("module %s: %w", name, err)`), unlike event set-up (decision 15:
   a half-seeded demo misleads testers; the operator reruns `reset`).
9. *Done.* Set `seeded_at`; record `demo.seed.ran` (operator, `Reason: "command"`, `Fields`
   the step names); print the report and the account table (addresses and the password,
   nothing else); count `bms.demo.seeds{outcome}`.

The profile table (all invented; no row may be a real person; the test
`TestSeed_NoRealLookingData` checks every address ends in `@demo.invalid` and every phone is
`+1 555 01xx`):

| Key | Playa name | Pronouns | Legal name | Phone |
|---|---|---|---|---|
| participant | Sprocket | they/them | Alex Example | +1 555 0101 |
| newcomer | (none) | | | |
| lead | Marbles | she/her | Sam Sample | +1 555 0102 |
| coordinator | Compass | he/him | Jordan Placeholder | +1 555 0103 |
| steward | Tumbleweed | they/them | | |
| event-admin | Lantern | she/her | | |
| settings-admin | Dial Tone | he/him | | |
| account-admin | Ledger | they/them | Casey Fictional | +1 555 0104 |
| access-admin | Gatekeeper | she/her | | |
| audit-reviewer | Quill | he/him | | |
| walkon-01..12 | Dusty Gears, Ember, Static, Kite, Pebble, Sundog, Monsoon, Tinsel, Flint, Bramble, Echo, Juniper | rotating they/them, she/her, he/him | odd numbers: "<Playa> Walkon" | odd numbers: +1 555 0111..0121 |

`Actors.As(ctx, key)` builds `identity.Session{ID: random, Account, Person, IssuedAt: now,
ExpiresAt: now + SessionLifetime}` from the account step's result and returns
`identity.WithSession(ctx, s)`. app's actor closures already prefer a session in ctx over the
operator fallback, so authz sees the person and audit records them (app.md, M2 deviations).

**Determinism.** Everything but IDs, timestamps and the event's dates is a constant table;
the dates are a pure function of the seed day in `Zone`. Two seeds on the same day produce
the same names, counts, times and relationships. `SeedVersion` changes when the table does.

**The reset** (`Reset`; span `demo.reset`; refuses when `Enabled` is false; refuses on a
read-only copy; refuses `Conflict not_demo` when `core.demo_state` has no row, checked before
anything is touched). Order, each a `Step`:

1. *objects*: `Objects.Clear` (every bucket, then `<files.dir>/tmp`); derived data first,
   so a failure here leaves the database intact and `reset` can rerun.
2. *spool*: delete every file under `<data.dir>/audit/spool` (a failed import of strangers'
   entries must not resurrect them).
3. *drop*: `DROP SCHEMA IF EXISTS <s> CASCADE` for each of `Schemas`, in one statement, under
   `migrateTimeout`; the migrator's bookkeeping lives in `core` and goes with it.
4. *migrate*: `Schema.Migrate` (every set in order, authz's starter-role seed), as
   `bms migrate` does; `secrets.json` and the session key are untouched, so existing tokens
   now name unknown accounts and every session is over (identity's stateless design).
5. *seed*: `Seed` (the marker check passes: the schema is empty).

Then `demo.reset.ran` is recorded (operator). A failure after step 3 leaves no marker:
the operator runs `bms migrate` if step 4 failed, then `bms demo seed`; the command says so.
The superuser account is gone with the rest; `bms create-superuser` recreates one when the
owner needs break-glass. Captured mail and jobs rows are in the dropped schema.

**Running the reset.** Shipped as `deploy/systemd/bms-demo-reset.service` (oneshot:
`ExecStartPre=systemctl stop bms`, `ExecStart=bms demo reset`, `ExecStartPost=systemctl start
bms`, same environment file as `bms.service`) and `bms-demo-reset.timer` (`OnCalendar=daily
04:00`, **not enabled**; `docs/operating.md` says how to enable it and gives the compose
equivalent). Stopping `serve` first keeps the drop from racing the settings snapshot, the
job leases and in-flight requests. A jobs-hosted reset was not chosen: it would drop its own
table under itself, needs the whole migration step inside a serving process, and cannot stop
the HTTP shell. The owner can run `bms demo reset` by hand at any time.

**Guard.** `Protected` is one indexed read of `core.demo_account`. identity consults its
`Guard` (nil in production) in `ChangePassword`, `SetTemporaryPassword`, `DisableAccount`,
`DeletePerson`, `RequestPasswordReset` (answers nil and sends nothing, as for a superuser
address), `CompletePasswordReset` (no token can exist) and any later e-mail change; the
refusal is `Permission` with field `account` code `protected`. `EnableAccount`, `SignOut`,
`EndOtherSessions` stay allowed (harmless to others). A `Guard` error fails the use case
(fail closed). Not protected, by choice (Q-DEMO4): grants and roles (an `access-admin` tester
can revoke the lead's role; the last-access-admin rule still holds), settings, departments
(archiving), the event (closing), profiles (a `people.profiles.manage` tester can rename
Marbles). All are repaired by the reset.

**Mail capture.** app's `ChannelFactory` returns `demo.Channel()` for any `SMTPConfig` when
`Enabled`; notify's loop runs as in production (leases, retries, `Sent`). `Send` inserts the
envelope (`To`, `Subject`, `Body`, `MessageID`) in its own short `Write` transaction, then
deletes rows beyond the newest `MaxMail`; records `bms.demo.mail_captured`; a malformed
envelope (zero `To`, empty subject) is `Invalid` wrapping `notify.ErrRejected` so notify ends
the row `rejected` instead of retrying; a store failure is `Unavailable` and notify retries.
`Ping` answers nil (the outbox screen's "check connection" succeeds). `ListMail`: actor →
`Authz.Allow(demo.mail.read)` → `Enabled` (`Unsupported` when off) → load → one
`demo.mail.read` entry per row (`Level: personal`, `Record: Ref{Kind: "demo.mail", ID}`,
`Fields: [to, subject, body]`) → return. Never logs an address or a subject.

**Upload cap.** `Policy` calls the inner source and lowers each field to the constant; it
never raises one. The constants are not settings: a demo settings-admin must not be able to
lift them.

**Concurrency and lifecycle.** `*Service` is safe for concurrent use; nothing to close; no
goroutines. `Seed` and `Reset` are meant to run one at a time from a command; a second `Seed`
meets the marker and refuses, a second `Reset` while the first runs may fail on the drop
(`Unavailable`) and is rerun. `New` does no I/O; `bms openapi` and `bms check` build it against
`db.None()`.

**Logging.** `demo mode on` once at start; one info line per seed and reset step with counts;
never an address, a name, a password (even this public one, to keep the scrubber's rules
simple) or a mail subject.

**Layering (decision 20).** The seed's emptiness check reads `core.account` directly
(`SELECT EXISTS (SELECT 1 FROM core.account WHERE NOT superuser)`), because a guard must not
depend on a permission-checked service of the thing it guards and identity offers no system
count. It is the second such read in the project (people's migration `0001` is the first) and
is covered by the integration test, so a column rename breaks the build, not the demo.

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `config.Load` | `BMS_DEMO_ENABLED` not a bool | Invalid | exit 2 | `TestLoad_DemoEnabled_Malformed_ReturnsInvalid` |
| F2 | `config.Load` | `BMS_DEMO_PUBLIC_URL` set while `BMS_DEMO_ENABLED` is false | Invalid | exit 2 | `TestLoad_DemoPublicURL_WithoutDemo_ReturnsInvalid` |
| F3 | `config.Load` | `BMS_DEMO_PUBLIC_URL` not a canonical URL | Invalid | exit 2 | `TestLoad_DemoPublicURL_Malformed_ReturnsInvalid` |
| F4 | `New` | a nil dependency, empty `Schemas`, bad timeout or URL | Invalid (field named) | exit 2 | `TestNew_MissingDependency_ReturnsInvalid` |
| F5 | `Link` | called twice or a nil provider | Internal | fail the build | `TestLink_Twice_ReturnsInternal` |
| F6 | `Seed` | `Enabled` false | Unsupported | exit 2, say `BMS_DEMO_ENABLED` | `TestSeed_DemoOff_ReturnsUnsupported` |
| F7 | `Seed` | marker row exists | Conflict `seeded` | exit 2, suggest `demo reset` | `TestSeed_AlreadySeeded_ReturnsConflict` |
| F8 | `Seed` | a non-superuser account exists | Conflict `not_empty` | exit 2, name the condition | `TestSeed_NonEmptyDatabase_ReturnsConflict` |
| F9 | `Seed` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | exit 1 | `TestSeed_ReadOnly_ReturnsUnavailable` |
| F10 | `Seed` | `EnsureAccount` fails (a demo address is a superuser) | Conflict wrapped with the step and key | exit 1; marker stays `seeding` | `TestSeed_SuperuserAddress_FailsWithStep` |
| F11 | `Seed` | a demo actor lacks a permission for its step | Permission wrapped with step and key | exit 1 (the starter roles or the seed table are wrong) | `TestSeed_ActorLacksPermission_ReturnsPermission` |
| F12 | `Seed` | a starter role is missing | NotFound wrapped | exit 1 | `TestSeed_StarterRoleMissing_ReturnsNotFound` |
| F13 | `Seed` | a contributor returns an error | the error wrapped with `module <name>` | exit 1 | `TestSeed_ContributorFails_FailsSeed` |
| F14 | `Seed` | a contributor exceeds `ContributorTimeout` | Timeout | exit 1 | `TestSeed_ContributorTimeout_ReturnsTimeout` |
| F15 | `Seed` | ctx canceled | Canceled | exit 1 | `TestSeed_Canceled_ReturnsCanceled` |
| F16 | `Seed` | `Audit.Record` fails | the recorder's kind | exit 1 | `TestSeed_AuditFails_FailsSeed` |
| F17 | `Seed` | validation of a seed row fails in a service (a regression in the table) | Invalid wrapped | exit 1 | `TestSeed_TableValidates` (asserts every row passes the services' validators) |
| F18 | `Reset` | `Enabled` false | Unsupported | exit 2 | `TestReset_DemoOff_ReturnsUnsupported` |
| F19 | `Reset` | no marker row | Conflict `not_demo` | exit 2, nothing touched | `TestReset_NoMarker_ReturnsConflict_TouchesNothing` |
| F20 | `Reset` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | exit 1 | `TestReset_ReadOnly_ReturnsUnavailable` |
| F21 | `Reset` | `Objects.Clear` fails | Unavailable | exit 1; database intact; rerun | `TestReset_ObjectsFail_LeavesDatabase` |
| F22 | `Reset` | spool dir unreadable | Unavailable | exit 1; rerun | `TestReset_SpoolUnreadable_ReturnsUnavailable` |
| F23 | `Reset` | drop refused by the server (privilege) | the store's kind (Permission/Unavailable) | exit 1; marker stays | `TestReset_DropFails_KeepsMarker` (integration) |
| F24 | `Reset` | `Schema.Migrate` fails | the migrator's kind (`Conflict` → exit 3) | exit 3/1; say "run `bms migrate`, then `bms demo seed`" | `TestReset_MigrateFails_SaysHowToRecover` |
| F25 | `Protected` | store unavailable | Unavailable | identity fails the change (closed) | `TestProtected_StoreDown_ReturnsUnavailable` |
| F26 | `Protected` | unknown account | — (`false, nil`) | proceed | `TestProtected_Unknown_False` |
| F27 | `Channel.Send` | zero `To` or empty subject | Invalid wrapping `notify.ErrRejected` | notify ends the row `rejected` | `TestCapture_BadEnvelope_Rejected` |
| F28 | `Channel.Send` | store down or read-only | Unavailable (+ `db.ErrReadOnly`) | notify retries | `TestCapture_StoreDown_ReturnsUnavailable` |
| F29 | `Channel.Send` | ctx canceled | Canceled | notify releases | `TestCapture_Canceled` |
| F30 | `ListMail` | no actor | Unauthenticated | 401 | `TestListMail_NoActor_ReturnsUnauthenticated` |
| F31 | `ListMail` | no `demo.mail.read` | Permission | 403 | `TestListMail_NoPermission_ReturnsPermission` |
| F32 | `ListMail` | `Enabled` false | Unsupported | 501, "not a demo" | `TestListMail_DemoOff_ReturnsUnsupported` |
| F33 | `ListMail` | `Limit` outside 1..`MaxMailList` | Invalid `limit` `range` | 400 | `TestListMail_BadLimit_ReturnsInvalid` |
| F34 | `ListMail` | `Audit.Record` fails | the recorder's kind | no audit, no read | `TestListMail_AuditFails_ReturnsNothing` |
| F35 | `ListMail` | store down | Unavailable | 503 | `TestListMail_StoreDown_ReturnsUnavailable` |
| F36 | `Policy` | inner source fails | the inner kind | files answers it | `TestPolicy_InnerFails_Passthrough` |
| F37 | `Policy` | inner values above the caps | — (lowered) | — | `TestPolicy_CapsNeverRaise` |
| F38 | `bms demo <x>` | unknown subcommand or extra arguments | — (usage) | exit 2 | `TestDemoCommand_Usage_Exit2` |
| F39 | `Info`, `Enabled`, `Channel` | — | cannot fail: constants and no I/O | — | `TestInfo_NoIO` (built against `db.None()`) |

## Lock-in check

No external technology is wrapped. Mail capture replaces SMTP with a table; an operator who
prefers an external catcher runs Mailpit (MIT) or MailHog and points `notify.smtp_host` at it
with the capture disabled (a later `demo.mail_capture` switch, not designed now). The reset
timer is a systemd unit; cron or a compose sidecar does the same (`docs/operating.md`).

## Test plan

- **Unit (fakes).** `demotest` fakes for every seam; `dbtest.Fake`, `audittest`,
  `authztest`, `telemetrytest`. The seed's step order and actor per step
  (`TestSeed_StepsRunAsTheRightActor`: every service call carries the expected session's
  person); the table's shape (`TestSeed_TableValidates`, `TestSeed_NoRealLookingData`,
  `TestSeed_Deterministic` runs twice with a fixed clock and compares everything but IDs);
  `Info` from constants; the cap; the capture's trim to `MaxMail`; the marker rule;
  `TestDemo_ChangesOnlyTheListed`.
- **Permission tests.** F11, F30, F31; the guard through identity's tests (Proposed change
  3: `TestChangePassword_Protected_ReturnsPermission` and siblings).
- **Leak tests.** `telemetrytest.New(t)` around the seed, the capture and `ListMail`: no
  address, no subject, no legal name, no password in logs or spans.
- **Integration (`//go:build integration`, PostgreSQL container).** `bms demo seed` on an
  empty schema through `app.Main`; sign in as each of the ten accounts; the lead sees Rangers'
  shifts, the participant their bookings; `ChangePassword` as a demo account is refused;
  `bms demo seed` again → exit 2; `bms demo reset` → objects gone, spool empty, old cookie
  rejected, the same counts as the first seed; `bms demo reset` with `BMS_DEMO_ENABLED`
  unset → exit 2 and nothing changed; a reset on a schema without the marker → exit 2 and
  nothing changed; a notification sent as a tester appears on `/demo/mail` and never reaches
  a listening SMTP fake; an upload over `MaxUploadBytes` fails `too_large` while
  `files.max_bytes` is set higher.
- **Conformance.** Each contributing module's `moduletest.Conformance` gains the optional rule
  `demoseed_provider_valid` (`WithDemoSeed()`): the provider runs against the module's fakes
  with a `demotest.NewFakeActors()` and creates at least one record without a `Permission`
  error (Proposed change 7).
- **Web.** The banner renders on every route and on the sign-in page with the table; axe
  passes at 320 px; the mail screen's five states; `useDemo` failure hides the banner and
  reports.

## Open questions

- **Q-DEMO1. The account set and password.** One account per starter role (nine) plus a
  second participant with an empty profile ("newcomer"), all sharing `burn-demo-password`,
  addresses `<key>@demo.invalid`; the "shift lead" the owner named is a position (Khaki) the
  volunteers seed creates, not a role. *Recommend:* yes. Alternative: fewer, bundled
  accounts (one "admin" with the four site-admin roles), which is closer to a small
  regional but hides permission gaps testers should find.
- **Q-DEMO2. Captured mail is readable by anyone signed in**, including password-reset
  links and the address a self-signed-up stranger typed. *Recommend:* yes, with the note on
  the screen; it is what makes the reset flow testable, and the banner already says strangers
  see everything. Alternative: show only mail addressed to the signed-in account (the reset
  flow, signed out, becomes untestable) or run Mailpit outside bms (the same exposure, less
  control).
- **Q-DEMO3. The automatic reset is an operator timer** (`deploy/systemd/`, shipped disabled,
  runs with `serve` stopped), not a job. *Recommend:* yes. Alternative: a `jobs`-hosted reset
  behind a setting, which would drop its own table under itself and run migrations inside a
  serving process.
- **Q-DEMO4. What "not perfect" covers.** A tester holding `access-admin`, `settings-admin`,
  `department-steward`, `event-admin` or `account-admin` can break the demo for everyone
  until the next reset (revoke the lead's role, archive Rangers, close the event, clear the
  SMTP settings, rename Marbles). Only the lock-out cases (password, address, disable, delete)
  are guarded. *Recommend:* accept, run `bms demo reset` by hand when it happens, and turn the
  timer on once the owner has reviewed a seeded instance.

## Proposed changes elsewhere

1. **`config.md`.** Section `Demo{Enabled bool, PublicURL string}`; keys `demo.enabled`
   (`BMS_DEMO_ENABLED`, bool, default false) and `demo.public_url` (`BMS_DEMO_PUBLIC_URL`,
   default empty, canonical URL rule, `Invalid` when set while `demo.enabled` is false);
   failure modes F1–F3 above; the catalogue and `Keys()`.
2. **`app.md`.** Part 11 `demo` after `files`: `declare` (`demo.Set`, `demo.Permissions`,
   the commands `demo seed` and `demo reset` as one `demo` command with two subcommands,
   `demoseed.Point` in `Env.CoreExtensionPoints`); `build` (`demo.New` over `cfg.Demo`, the
   seams as adapters over the real services, `Schemas` from the core set order plus the
   plan's manifests, `Schema` = the `migrate` step, `Objects` over the `objects.Store` and
   `cfg.Files.Dir`, `audit.Scoped("demo")`); `link` (`Link` over
   `a.set.Contributors(demoseed.Point)`, with `Accepts` checking `demoseed.Provider`); when
   `cfg.Demo.Enabled`: identity's `Guard` = a lazy adapter over `demo.Protected`, notify's
   `ChannelFactory` = `func(SMTPConfig) (Channel, error) { return demo.Channel(), nil }`,
   files' `PolicySource` = the demo service over the settings-backed one, `api.Options.NoIndex`
   = true; route set `demo`; `serve`'s warn line; `bms check`'s `demo:` line; exit statuses
   for the two commands (2 for `Unsupported`/`Conflict seeded|not_empty|not_demo`, 3 for the
   migrator's `Conflict`, else 1); the actor closures' documented preference for a session in
   ctx over the operator (already the behaviour); `TestDemo_ChangesOnlyTheListed`; the
   end-to-end demo test.
3. **`identity.md`.** (a) `EnsureAccount(ctx, Credentials) (AccountResult{Account
   model.AccountID, Person model.PersonID, Created bool}, error)`: writer-only, no actor,
   for commands; creates the account (password validated as `SignUp` does: length, common
   list; no sign-up mode check, no limiter) and publishes `identity.account-created`, or, when
   the address exists and is not a superuser, sets the password, enables the account and
   bumps `session_generation`; `Conflict superuser` for a superuser address; audited
   `identity.account.created` / `identity.password.changed` with `ActorOperator`;
   `Unavailable` wrapping `db.ErrReadOnly`. (b) `Options.Guard Guard` with
   `Guard{Protected(ctx, model.AccountID) (bool, error)}`, nil = nothing protected; consulted
   as in Behavior "Guard"; `Permission` field `account` code `protected`; `RequestPasswordReset`
   answers nil and sends nothing for a protected account; a `Guard` error fails the use case;
   failure modes and tests. (c) `WithSession`'s doc gains `core/demo`'s seed as a permitted
   caller.
4. **`authz.md`.** `EnsureGrant(ctx, GrantInput) (Grant, bool, error)`: writer-only, no
   actor, for commands (the sibling of `Seed`); site-scoped grants only (`Invalid scope` for a
   department); separation-of-duties rules checked (`Conflict`); an existing grant is returned
   with `false`; audited `access.grant.created` with `ActorOperator`; publishes
   `access.grant-created`.
5. **`people.md`.** `Delete`/`FulfilRequest` and `bms delete` map identity's `Permission
   protected` to `Conflict protected` beside `Conflict superuser`; one failure mode and test.
6. **`module-contract.md`.** `demo` joins the reserved core areas (`IsCoreArea`); `demo.seed`
   is a core extension point; `moduletest.Conformance` gains the option `WithDemoSeed()` and
   the rule `demoseed_provider_valid` (Test plan).
7. **`module-volunteers.md`.** `ContributesTo` gains `demoseed.Point`; `Contributions()`
   returns a `demoseed.Provider` (`demo.go`) that, as `lead`: creates the qualifications
   "Ranger training" (Rangers, 365-day validity), "ICS-100" (org-wide, no expiry), "First
   aid" (Medical, 730 days), "Over 18" (org-wide, no expiry); the positions Dirt Ranger
   (Rangers, open, requires Ranger training and Over 18), Khaki (Rangers, members, requires
   Ranger training, ICS-100 and Over 18; the "shift lead" position), Gate Crew (Gate, open),
   Greeter (Greeters, open), Medic (Medical, members, requires First aid); members: the
   participant, the lead and six walk-ons in Rangers, four walk-ons in Medical; records:
   Ranger training and Over 18 verified for the participant, the lead and the Rangers
   walk-ons, ICS-100 verified for the lead, one **pending** self-upload (a generated 1×1 PNG
   certificate) for walk-on 02; shifts for the event, Thursday to Sunday in `Zone`: Dirt
   Ranger 08–14, 14–20, 20–02 with 4 slots, Khaki 07–15 and 15–23 with 1 slot, Gate Crew
   10–14, 14–18, 18–22 with 3 slots, Greeter 10–14 and 14–18 with 3 slots, Medic 08–20 and
   20–08 with 2 slots, plus one standing "Winter inventory" shift (no event) in DPW two
   weeks after the event; sign-ups: as `participant`, two Dirt Ranger shifts (Thu 14–20, Sat
   08–14); as `lead`, `Assign` walk-ons so one Dirt Ranger shift is full, each Gate and
   Greeter shift has one booking, the lead holds Khaki on Friday, one assignment with
   `Waive: true` (walk-on 07 on Dirt Ranger without training); one shift canceled by the
   lead (Sunday 20–02 Dirt Ranger). Items counted. Org-wide qualifications need
   `qualifications.manage` site-wide (`department-steward`), so those two are created as
   `steward`. Permissions seed order and the `Conflict unnamed` rule mean every booked person
   must have a playa name (they do).
8. **`notify.md`.** Note that `app` may supply a `ChannelFactory` that ignores `SMTPConfig`
   (demo mode); `Configured` is unchanged. No interface change.
9. **`files.md`.** Note that `app` may wrap `PolicySource` to lower the limits (demo mode);
   no interface change.
10. **`web-shell.md`.** `useDemo()` in `@/api` over `GET /api/demo` read at boot;
    `DemoBanner` in the frame (every screen; expanded with the account table on
    `/accounts/sign-in`); keys `shell.demo.banner.title|body`, `shell.demo.accounts.title`,
    `shell.demo.password`, `shell.demo.mail_link`; `core.demo.*` keys for the ten account
    labels, the six department labels and the mail screen; `web/src/core/demo/` as a core
    area with `/demo/mail`; the accounts area's `field.protected` message; the people
    deletion screen's `Conflict protected` message.
11. **`api.md`.** `Options.NoIndex bool`: when set, every UI-handler response (D29's set)
    carries `X-Robots-Tag: noindex, nofollow`; a test.
12. **`db.md`.** The fixed core set order gains `demo` after `files`;
    `demo.postgres.Migrations`, `demo.Set`.
13. **`telemetry.md`.** Instrument rows `bms.demo.seeds{outcome}`, `bms.demo.resets{outcome}`,
    `bms.demo.mail_captured`; span names `demo.seed`, `demo.seed.step`, `demo.reset`,
    `demo.mail.capture`.
14. **`07-abstractions.md`.** A `### demo` entry under Core (purpose, owns, doesn't do, spec
    link) and `contracts/demoseed` beside `greeter`, `personpage`, `eventsetup`,
    `membership` in the contracts entry; the Delivery entry for `app` lists the `demo`
    command.
15. **`plan/design/README.md`.** The row for this spec (added with this draft).
16. **`deploy/` and `docs/operating.md`** (targets not written): `deploy/systemd/
    bms-demo-reset.service` and `.timer` (disabled), the compose equivalent, the operator
    text: what demo mode changes, the reset's recovery steps, "the banner is not a
    substitute for keeping real data out", and that backups of a demo hold whatever strangers
    typed.
17. **`.claude/skills/add-module/SKILL.md`** (main session): a module that wants demo data
    adds `demo.go` with a `demoseed.Provider` and `WithDemoSeed()` in its conformance test.
