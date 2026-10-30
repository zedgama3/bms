# Demo

- **Status:** Implemented (2026-10-30; Approved 2026-10-28, D53, with the main session's recommendations; Q-DEMO2: captured mail is shown only to the account it was sent to)
- **Package:** `internal/core/demo` (service), `internal/core/demo/postgres` (store),
  `internal/core/demo/demotest` (fakes), `internal/contracts/demoseed` (the extension point)
  **UI:** `web/src/shell/` (the banner, in the frame), `web/src/core/demo/` (the captured-mail
  screen)
- **Depends on specs:** config, app, module-contract, db, identity, authz, people,
  departments-events, settings, notify, files, audit, jobs (only to say what it does not use),
  api, web-shell, telemetry (names only; a parallel spec designs client telemetry)
  **Used by:** `app` (the part, the commands, the adapters), the project's public demo
  instance, every module that contributes seed data (volunteers first)
- **Amendment 2026-10-30 (owner, D58; Q-DEMO6 answered):** the session answer carries `Protected bool` (`protected`), set from identity's `Guard` (identity.md amendment 2026-10-30 (D58), `SessionInfo.Protected`); the password screen reads it to show the notice and disable the form; on a `Guard` error the session answer says `protected: false` and logs a warning, the server refusal staying the backstop (F46, identity F110).
- **Amendment 2026-10-30 (owner, D57):** the sign-in table shows one short label per account (built from a per-account catalogue key; the lead's departments joined), the password screen shows the protected-account notice up front with the form disabled, and in demo mode the notify outbox status says mail is captured; see the sign-in page, the password screen and Mail capture.
- **Amendment D1 (raised 2026-10-28, Approved 2026-10-29, D56; Q-DEMO5: one lead for five departments): what the volunteers provider build found.** The `lead` holds `department-admin` at the five departments the demo gives volunteer data (Rangers, Gate, Greeters, Medical, DPW), not Rangers only; the pending self-upload is the participant's, not a walk-on's; the lead has no verified records and holds Khaki by a recorded self-waiver; DPW gets the "Inventory Crew" position; the conformance rule `demoseed_provider_valid` starts the module and gives the fake event dates, so a provider never invents dates; the walk-on bookings the table left open are fixed. Section "Amendment D1" before Open questions; the volunteers table changes are in `module-volunteers.md`, "Amendment D1 changes".

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
`AccountInfo{Key, Email, Roles}` (the table's Role column shows one short label built from `Key`, not the roles, D57); `Report{Steps []Step}`, `Step{Name, Items}`; `MailID`,
`Mail{ID, To, Subject, Body, CapturedAt}`, `MailQuery{Limit}`; seams `Accounts`, `Access`,
`Addresses`, `Profiles`, `Departments`, `Events`, `Settings`, `Schema`, `Objects` (one to three methods
each, filled by `app`); `Contributor{Module, Provider}`; `Options{Enabled, PublicURL, DB, Clock,
Logger, Tracer, Meter, Audit (scoped "demo"), Actor, Files files.PolicySource, the seams,
Schemas []string, DataDir, ContributorTimeout}` (+`Validate`); `New(Options) (*Service, error)`
(no I/O); `Link([]Contributor) error`; `Enabled() bool`; `Info() Info` (no I/O); `Seed(ctx, out
io.Writer) (Report, error)`; `Reset(ctx, out) (Report, error)`; `Protected(ctx, model.AccountID)
(bool, error)` (identity's proposed `Guard`); `Channel() notify.Channel`; `Policy(ctx)
(files.Policy, error)` (the capped `files.PolicySource`; the wired policy fails closed with `Unavailable` when demo is enabled but the service is missing); `ListMail(ctx, MailQuery) ([]Mail,
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
(`deploy/systemd/`), run with `serve` stopped; not a job (owner, 2026-10-28). 8 two guards: the commands refuse
unless `demo.enabled`, and reset refuses without the marker row (`core.demo_state`) the seed
writes first. 9 identity consults a `Guard`: a demo account cannot change or reset its password
or address, be disabled, deleted or linked to a person (`Permission`, field `account`, code `protected`); anything
else a tester breaks, the next reset repairs (owner, 2026-10-28); the password screen tells a protected account's holder up front and disables the form, the server refusal stays the backstop (D57), and it knows from `SessionInfo.Protected`, which identity sets from this `Guard` (Q-DEMO6, D58); the sign-in table shows one short label per account (D57). 10 demo never sends mail: the capture `Channel`
stores envelopes in `core.demo_mail` (newest `MaxMail`), shown on `/demo/mail` only to the
signed-in account they were addressed to (login address; no one else's, no admin override, no
permission, not audited: it is the viewer's own mail; owner, 2026-10-28); the sign-up form in
demo mode says to use a made-up address. 11 uploads are capped by wrapping
files' `PolicySource`. 12 the data is a constant table (fictional "Ember Flats", `.invalid`
addresses, 555-01xx phones, invented playa names) with dates relative to the clock; IDs differ.
13 ten accounts, one per starter role plus the empty-profile newcomer, one shared password, printed on the sign-in page (owner, 2026-10-28). 14 `Info` does no I/O. 15 a failed seed
step fails the command and leaves the marker in `seeding`; `reset` is the recovery.

**Errs kinds callers handle.** `Seed`: `Unsupported` (off), `Conflict` (`seeded`, `not_empty`),
`Unavailable` wrapping `db.ErrReadOnly`, `Permission` (a demo actor lacks a permission: the
starter roles or the table are wrong), `Timeout` (contributor), any seam's or contributor's
kind wrapped with the step. `Reset`: `Unsupported`, `Conflict not_demo`, `Unavailable` wrapping
`db.ErrReadOnly`, the migrator's and store kinds. `Protected`: store kinds (identity fails
closed). `Channel.Send`: `Invalid` wrapping `notify.ErrRejected`, `Unavailable`. `ListMail`:
`Unauthenticated`, `Unsupported`, `Invalid` (`limit` `range`), store kinds. `Policy`: the inner kind. `Info`, `Enabled`, `Channel` cannot fail. `New`/`Link`:
`Invalid`, `Internal`. Exit statuses: `Unsupported` (off) and Conflict `seeded`, `not_empty`, `not_demo` → 2; any other Conflict → 1; reset's migrate step uses the migrate command's own mapping (schema → 3), a failed migrate wrapped as `demo.ErrMigrate`; else 1. `bms demo` checks `BMS_DEMO_ENABLED` right after config load, before telemetry or the database (exit 2, naming the key). `Options.Validate` rejects the `public` schema in `Schemas`, and the drop excludes it.

**Owns.** Tables `core.demo_state`, `core.demo_account`, `core.demo_mail`; set `db.Set{Name:
"demo", Schema: "core"}` after `files`; config `demo.enabled`, `demo.public_url`; extension point `demo.seed`; route set `demo` (`GET /api/demo` public, `GET
/api/demo/mail` signed-in); commands `bms demo seed|reset`; screen `/demo/mail`, the frame's
`DemoBanner`, keys `shell.demo.*`, `core.demo.*`; audit `demo.seed.ran`, `demo.reset.ran`;
spans `demo.seed`, `demo.seed.step`, `demo.reset`, `demo.mail.capture`;
metrics `bms.demo.seeds{outcome}`, `bms.demo.resets{outcome}`, `bms.demo.mail_captured`. No
settings, signals or jobs.

**Expects of dependencies** (each a numbered Proposed change): identity `EnsureAccount`, `Guard`,
`WithSession`; authz `EnsureGrant`, `Grant`, `StarterRoleIDs`; people `UpdateMe`, `Create`, `Conflict
protected` (delete and link); departments/events/settings `Create`, `AddScheduleItem`, `Set`; notify a swappable
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
	ActorLead          = "lead"           // department-admin at Rangers, Gate, Greeters, Medical and DPW
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
	StarterRoleIDs(ctx context.Context, keys []string) (map[string]authz.RoleID, error) // authz's role lookup seam (not ListRoles): role keys to IDs; actor-less, read-only
}
// Addresses is identity's EmailOf (existing): the login address of the viewer's account,
// the only address ListMail matches. NotFound (no account) gives an empty list.
type Addresses interface {
	EmailOf(ctx context.Context, person model.PersonID) (model.Email, error)
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
// demo.tracer, demo.audit, demo.actor, demo.files, demo.accounts, demo.access, demo.addresses,
// demo.profiles, demo.departments, demo.events, demo.settings, demo.schema, demo.objects,
// demo.schemas (empty, a name outside the db schema grammar, or `public`), demo.data_dir,
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
	Actor              authz.ActorFunc   // ListMail's viewer (the signed-in person)
	Files              files.PolicySource // the inner (settings-backed) source Policy caps
	Accounts           Accounts
	Access             Access
	Addresses          Addresses
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

// ListMail lists captured mail addressed to the signed-in account's login address, newest
// first; never another address's mail, whoever asks. Needs a session, no permission, and
// is not audited (the viewer's own mail). Consumer: GET /api/demo/mail.
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
`Envelope`; `identity.EmailOf` (as `Addresses`); `files.Policy`; `audit.Entry`, `Ref`.

## Data

Set `db.Set{Name: "demo", Schema: "core"}`, migrated on every install (the tables are empty
and unread when demo is off; a conditional set would make `Protected` undefined).

| Table | Columns | Sensitivity | Retention |
|---|---|---|---|
| `core.demo_state` | `id smallint PK CHECK (id = 1)`, `seed_version int`, `instance text`, `started_at timestamptz`, `seeded_at timestamptz NULL` (null = seeding or failed) | internal | until reset |
| `core.demo_account` | `account_id uuid PK REFERENCES core.account(id) ON DELETE RESTRICT`, `person_id uuid REFERENCES core.person(id) ON DELETE RESTRICT`, `key text UNIQUE`, `email text`, `position int` | internal (generated addresses, not a person's) | until reset |
| `core.demo_mail` | `id uuid PK`, `to_address text`, `subject text`, `body text`, `message_id text`, `captured_at timestamptz` | `to_address`, `subject`, `body`: personal (a stranger may have typed a real address despite the sign-up note; a body may name them) | the newest `MaxMail` rows; all gone at reset |

Index `demo_mail_captured_at_idx (captured_at DESC)` and `demo_mail_to_idx (lower(to_address),
captured_at DESC)` for the recipient filter. The foreign keys are the backstop behind
the `Guard`: even a direct deletion cannot remove a demo account's rows while the marker table
references them.

## Permissions

None owned. `ListMail` and the `/demo/mail` screen need a session and nothing else: the data
is the viewer's own (people's `UpdateMe` is the same shape), so a permission would only add a
role that could widen it; the recipient filter is in the query, not in a grant (owner,
2026-10-28, Q-DEMO2: self only, no admin override). `GET /api/demo` needs none (public by design). `Seed` and `Reset` are commands: the operator has
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
account table (in a `<details>` disclosure: collapsed below the `sm` breakpoint so the sign-in form stays near the top on a phone, open at `sm` and above; `shell.demo.accounts.title` "Try a demo account", columns Account, Role,
E-mail, and a button per row; the shared password stated once above the table, as text, so a tester can sign in again by hand; `shell.demo.password`
"Password for every demo account: {password}"; the button is `shell.demo.sign_in_as` "Sign in as {account}" with the row's account label, so its accessible name is unique per row; (owner, 2026-10-30) it replaces the earlier copy-password button; that button and its messages are gone). (D57) The Role column shows one short label per account, not the list of its roles' long titles, e.g. "Lead: Rangers, Gate, Greeters, Medical, DPW" (the department names in the regional's words), "Participant", "Newcomer (no profile yet)", "Volunteer coordinator".
The label is built from one catalogue key per account, `core.demo.account.<key>` (10 keys, `en` and `es`); for the lead the key's text has a `{departments}` argument, the departments of the lead's `department-admin@<dept>` entries in `AccountInfo.Roles`, each named by `core.demo.department.<key>` and joined with ", " in `Roles` order (the lead holds five, D1.1); the other accounts' keys are plain text. The long role titles are no longer listed in the table. Pressing a row's button fills the sign-in form's e-mail and password fields with that account's address and the shared password and submits the form through its normal path (its errors, its busy state and `returnTo` apply), so the tester is signed in as that account in one click (owner, 2026-10-30).
Seam (as built): the banner calls `requestDemoSignIn(email)` (frame-private `@/api/internal`, ignored while busy; the request is `{id, email}`, never the password; only `core/accounts` may import the `@/api` names, by an eslint ban); the screen fills the password from `useDemo().password`; the Sign in column is hidden when that password is empty; the core sign-in screen reads `useDemoSignInState()` and reports `setDemoSignInBusy()` from `@/api`, fills its real inputs and calls `form.requestSubmit()`, and ignores any request id older than the one present at mount; detail in web-shell.md's demo amendment. Column heading key `shell.demo.col.signin` "Sign in". The accounts area itself is unchanged: the form is identity's and keeps its own fields and
rules; a tester may still type the address. Self sign-up stays per `accounts.signup` (the sign-up link is identity's). On the sign-up form
in demo mode the frame renders a note (`shell.demo.signup_note`, "Use a made-up address, not
your own. Mail the demo sends can be read only by the account it is sent to, but strangers see
everything else you enter.") above the e-mail field.

**Captured mail (`web/src/core/demo/`, route `/demo/mail`, `kind: "core"`, no nav entry).**
A list newest first: to, subject, captured time (`useFormat` relative), the body expanded on
tap (plain text, links clickable so a tester can complete a password reset). Reads
`GET /api/demo/mail`. States: loading; empty (`core.demo.mail.empty` "No mail has been captured
yet. Mail the demo would send appears here instead of being delivered."); signed out (a `401` sends to sign-in as any screen); off (`501` shows `core.demo.mail.off` "This is not a demo instance.");
error with reference and retry. A note above the list (`core.demo.mail.note`): "Only mail sent to your own
account's address appears here." Phone-first.

**The password screen (accounts area, Proposed change 10).** (D57) A protected demo account sees the notice `core.accounts.field.protected` ("This is a shared demo account. Its
password and e-mail address can't be changed.") above the form before typing anything, and the form's fields and button are disabled; nothing waits for a refusal. The server's refusal stays the backstop: a `403` with field `account` code
`protected` (a stale page, a direct call) shows the same text. The screen knows from `SessionInfo.Protected` (`protected` on `GET /api/accounts/session`), which identity sets from its `Guard` when it answers the session (Q-DEMO6, owner, 2026-10-30, D58); no e-mail matching, and `Info` stays free of I/O. The account admin screen shows the same for
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
   coordinator → `volunteer-coordinator`. Role IDs come from `authz.StarterRoleIDs` by key (actor-less, read-only; no superuser session); a missing
   starter role (a regional deleted it; impossible on a fresh schema) fails `NotFound`. The
   self-grant rule keeps `access-admin` to its one role. The `grants` step counts 12: 1 bootstrap, 6 by `access-admin`, 5 for the lead (step 4).
4. *Departments.* As `steward`: `Departments.Create` for Rangers ("Keep the event safe and
   calm: patrols, perimeters, the radio"), Gate ("Tickets, waivers and wristbands at the
   entrance"), Greeters ("Welcome arrivals and point them to camp"), Medical ("First aid and
   the medical tent"), DPW ("Build and strike the city"), Sanctuary ("Peer support and a
   quiet place"); each with `ContactEmail` `<key>@demo.invalid`. Then as `access-admin`:
   `Grant({Person: lead, Role: department-admin, Department: D})` for D in Rangers, Gate,
   Greeters, Medical, DPW, in that order (five rows; `access-admin` holds
   `access.grants.manage` site-wide, so the no-escalation rule does not apply, and the rows
   differ by department, so none is a `duplicate`). Sanctuary stays without a lead on purpose
   (the department whose leads have left, D1.1). The `departments` step reports 6; the five lead grants are not
   counted there but printed as a separate progress line `lead grants 5` after the step.
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
   `migrateTimeout`; `Options.Validate` refuses `public` and the statement excludes it, so a mismatched count is `Internal`; the migrator's bookkeeping lives in `core` and goes with it.
4. *migrate*: `Schema.Migrate` (every set in order, authz's starter-role seed), as
   `bms migrate` does, and exits with the migrate command's own mapping (a failed migrate is wrapped as `demo.ErrMigrate`); `secrets.json` and the session key are untouched, so existing tokens
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
`DeletePerson`, `LinkAccount` (after the superuser check), `RequestPasswordReset` (answers nil and sends nothing, as for a superuser
address), `CompletePasswordReset` (no token can exist) and any later e-mail change; the
refusal is `Permission` with field `account` code `protected`. `EnableAccount`, `SignOut`,
`EndOtherSessions` stay allowed (harmless to others). A `Guard` error fails the use case
(fail closed); that includes the session answer, which carries `Protected` (identity amendment D58, F110): on a Guard error it answers `protected: false` and logs a warning, so the session stays usable and the server's refusal is the backstop. Not protected, by choice (Q-DEMO4): grants and roles (an `access-admin` tester
can revoke the lead's role; the last-access-admin rule still holds), settings, departments
(archiving), the event (closing), profiles (a `people.profiles.manage` tester can rename
Marbles). All are repaired by the reset.

**Mail capture.** app's `ChannelFactory` returns `demo.Channel()` for any `SMTPConfig` when
`Enabled`; notify's loop runs as in production (leases, retries, `Sent`). `Send` inserts the
envelope (`To`, `Subject`, `Body`, `MessageID`) in its own short `Write` transaction, then
deletes rows beyond the newest `MaxMail`; records `bms.demo.mail_captured`; a malformed
envelope (zero `To`, empty subject) is `Invalid` wrapping `notify.ErrRejected` so notify ends
the row `rejected` instead of retrying; a store failure is `Unavailable` and notify retries.
`Ping` answers nil (the outbox screen's "check connection" succeeds). (D57) In demo mode the notify outbox screen's status line says mail is captured for the demo (notify `core.notify.outbox.demo`, "Mail is captured for the demo, not sent"), not "E-mail is set up"; the screen reads `useDemo().enabled`. A message to a person with no address ends `Skipped` there (notify decision 6). `ListMail`: actor
(`Unauthenticated` without one) → `Enabled` (`Unsupported` when off) → `Addresses.EmailOf(actor)`
(`NotFound` → empty list) → load rows whose `to_address` equals that address
(`WHERE lower(to_address) = $1`, the viewer's address lower-cased; there is no other filter and
no parameter or role that widens it) → return. No audit entry: the rows are the viewer's own. Never logs an address or a subject.

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
| F24 | `Reset` | `Schema.Migrate` fails | the migrator's kind (`Conflict` → exit 3) | exit by the migrate command's mapping (3 behind, ahead or busy; else 1, see F43); say "run `bms migrate`, then `bms demo seed`" | `TestReset_MigrateFails_SaysHowToRecover` |
| F25 | `Protected` | store unavailable | Unavailable | identity fails the change (closed) | `TestProtected_StoreDown_ReturnsUnavailable` |
| F26 | `Protected` | unknown account | — (`false, nil`) | proceed | `TestProtected_Unknown_False` |
| F27 | `Channel.Send` | zero `To` or empty subject | Invalid wrapping `notify.ErrRejected` | notify ends the row `rejected` | `TestCapture_BadEnvelope_Rejected` |
| F28 | `Channel.Send` | store down or read-only | Unavailable (+ `db.ErrReadOnly`) | notify retries | `TestCapture_StoreDown_ReturnsUnavailable` |
| F29 | `Channel.Send` | ctx canceled | Canceled | notify releases | `TestCapture_Canceled` |
| F30 | `ListMail` | no actor | Unauthenticated | 401 | `TestListMail_NoActor_ReturnsUnauthenticated` |
| F31 | `ListMail` | mail exists for other addresses (including a demo account's) | — (not listed; no kind) | show only the viewer's own | `TestListMail_OtherAccountsMail_NotListed` |
| F32 | `ListMail` | `Enabled` false | Unsupported | 501, "not a demo" | `TestListMail_DemoOff_ReturnsUnsupported` |
| F33 | `ListMail` | `Limit` outside 1..`MaxMailList` | Invalid `limit` `range` | 400 | `TestListMail_BadLimit_ReturnsInvalid` |
| F34 | `ListMail` | the viewer has no account address (`EmailOf` NotFound) | — (empty list) | show the empty state | `TestListMail_NoAccountAddress_Empty` |
| F35 | `ListMail` | store down | Unavailable | 503 | `TestListMail_StoreDown_ReturnsUnavailable` |
| F36 | `Policy` | inner source fails | the inner kind | files answers it | `TestPolicy_InnerFails_Passthrough` |
| F37 | `Policy` | inner values above the caps | — (lowered) | — | `TestPolicy_CapsNeverRaise` |
| F38 | `bms demo <x>` | unknown subcommand or extra arguments | — (usage) | exit 2 | `TestDemoCommand_Usage_Exit2` |

| F40 | `Options.Validate` | `Schemas` contains `public` | Invalid `demo.schemas` | exit 2 | `TestOptions_PublicSchema_ReturnsInvalid` |
| F41 | `Reset` | the drop statement's schema count differs from `Schemas` | Internal | exit 1; marker stays | `TestReset_DropCountMismatch_ReturnsInternal` |
| F42 | `bms demo <x>` | `BMS_DEMO_ENABLED` off, checked right after config load | — (usage-style) | exit 2, name the key; telemetry and the database untouched | `TestDemoCommand_Disabled_Exit2_NamesKey` |
| F43 | `Reset` | `Schema.Migrate` fails | `demo.ErrMigrate` wrapping the migrator's kind | exit by the migrate command's mapping | `TestReset_MigrateFails_WrapsErrMigrate` |
| F44 | files policy wiring | demo enabled but the demo service is missing | Unavailable (fail closed) | files answers it | `TestDemoPolicy_ServiceMissing_FailsClosed` |
| F45a | password screen (web) | a protected demo account opens it (D57) | notice above the form, fields and button disabled | read the notice | `demo.test.tsx` "password screen shows the protected notice and disables the form" (and "a 403 protected still shows the notice") |
| F46 | session answer (`identity.SessionInfo`) | `Guard.Protected` fails (store down) | none: the call succeeds | answers `protected: false` and logs a warning (owner, D58 revision); the form may show for a protected account and the server's refusal stays the fail-closed backstop | identity `TestSessionInfo_GuardError_AnswersUnprotectedAndLogs` |
| F45 | sign-in page (web) | a one-click "Sign in as" is refused or fails | the form's normal `ApiError` | the form shows its own error and clears its busy state; the tester may press another row or type | `demo.test.tsx` "sign in as failure shows the form's error" |

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
- **Permission tests.** F11, F30; F31 (`TestListMail_OtherAccountsMail_NotListed`: with mail
  captured for two accounts, each sees only its own; a superuser and an `account-admin` see
  only their own; no query parameter widens it); the leak test below covers the rest; the guard through identity's tests (Proposed change
  3: `TestChangePassword_Protected_ReturnsPermission` and siblings).
- **Leak tests.** `telemetrytest.New(t)` around the seed, the capture and `ListMail`: no
  address, no subject, no legal name, no password in logs or spans.
- **Integration (`//go:build integration`, PostgreSQL container).** `bms demo seed` on an
  empty schema through `app.Main`; sign in as each of the ten accounts; the lead sees Rangers'
  shifts, the participant their bookings; `ChangePassword` as a demo account is refused;
  `bms demo seed` again → exit 2; `bms demo reset` → objects gone, spool empty, old cookie
  rejected, the same counts as the first seed; `bms demo reset` with `BMS_DEMO_ENABLED`
  unset → exit 2 and nothing changed; a reset on a schema without the marker → exit 2 and
  nothing changed; a notification sent to a tester's address appears on that account's `/demo/mail`, not on
  another account's, and never reaches a listening SMTP fake; an upload over `MaxUploadBytes` fails `too_large` while
  `files.max_bytes` is set higher.
- **Conformance.** Each contributing module's `moduletest.Conformance` gains the optional rule
  `demoseed_provider_valid` (`WithDemoSeed()`): the provider runs against the module's fakes
  with a `demotest.NewFakeActors()` and creates at least one record without a `Permission`
  error (Proposed change 7, as superseded by `module-volunteers.md` "Amendment D1 changes").
  The rule registers the request's event with dates (as step 5 computes them from the rig's
  clock, zone `UTC`) and calls the module's `Start` before `Provider.Seed` and `Stop` after
  (D1.5, D1.6); a provider that finds an event without dates fails `Invalid`.
- **Web.** The banner renders on every route and on the sign-in page with the table; each
  row's "Sign in as {account}" button has a unique accessible name and, pressed, fills the form
  and submits it (`demo.test.tsx` "sign in as fills the form and signs in", "sign in as keeps
  returnTo", "sign in as failure shows the form's error"); axe
  passes at 320 px; the mail screen's five states; `useDemo` failure hides the banner and
  reports.

## Accepted implementation deviations (demo build, 2026-10-29)

Verified against the code and accepted:

- Only the `en` catalogue exists; there is no `es` yet (the "`en` and `es`" above is the target, not the build).
- `Options.Instance string`: the marker's instance name, `demo` when empty (a valid ident otherwise, else `Invalid`).
- `Service.State(ctx) (seededAt time.Time, found bool, err error)`: the marker for `bms check`; `found` is false when there is no row.
- The command's usage is `seed|reset` (`bms demo seed|reset`).
- Audit and span `Fields` use normalised step names (lower case, runs of non-alphanumerics become `_`): `walk_ons`, `module_volunteers`.
- A contributor that exceeds `ContributorTimeout` returns `Timeout` (not `Internal`), wrapped with the module name.
- The mail capture maps a table refusal (an envelope with no recipient or no subject) to `Invalid` wrapping `notify.ErrRejected`, so notify records a rejected send and does not retry.
- `Reset`'s migrate-failure message names the recovery: "the demo schemas were dropped and not rebuilt: run `bms migrate`, then `bms demo seed`".
- Routes: the core mounts the set at `/` under `/api/demo/`, so the info route is `GET /api/demo/` (public) and the mail route `GET /api/demo/mail`, answering `{"mail":[{"id","to","subject","body","captured_at"}]}`.
- The demo route set is mounted and documented only when `demo.enabled`; `bms openapi` honours `BMS_DEMO_ENABLED`.
- Role IDs for the grants step come from authz's actor-less `StarterRoleIDs(ctx, keys []string) (map[string]RoleID, error)` (see authz.md), the demo's role lookup seam, not `ListRoles`; no superuser session is created.
- D56 review: `identity.LinkAccount` runs the protected-demo-account guard after the superuser check; a protected account gets `Permission` (field `account`, code `protected`) and a guard error fails the link closed; `people.LinkAccount` maps it to `Conflict` `protected`, as for delete.
- D56 review: `bms demo reset` refuses the `public` schema (`Options.Validate`, field `demo.schemas`; the drop excludes it, a mismatched count is `Internal`); `bms demo` checks `BMS_DEMO_ENABLED` right after config load and exits 2 naming the key when off.
- D56 review: exit codes are 2 for `Unsupported` and Conflict `seeded`, `not_empty`, `not_demo` only; any other Conflict exits 1; reset's migrate step uses the migrate command's own mapping (a failed migrate wrapped as `demo.ErrMigrate`).
- D56 review: the demo files policy fails closed (`Unavailable`) when demo is enabled but the demo service is missing.
- D56 review: the seed report's `grants` step counts 12 (1 bootstrap, 6 by access-admin, 5 for the lead); `departments` reports 6; a separate `lead grants 5` progress line follows the departments step.
- api.md: the error mapper keeps field errors with code `protected` on a `Permission` error, so the web shows the protected message.

## Amendment D1 (raised 2026-10-28, Approved 2026-10-29, D56; Q-DEMO5: one lead for five departments): what the volunteers provider build found

Building the volunteers `demoseed.Provider` (Proposed change 7) against the seed as specified
found seven gaps between the core's steps and the volunteers table. Each is settled below;
one needs the owner. Nothing here changes what demo mode does for a tester beyond the data
they find; decisions 1 to 15 stand. Folded into Behavior step 4, the `ActorLead` comment, the lead's label and the Test plan on 2026-10-29
(Q-DEMO5 answered).

**D1.1 The lead's grants (Behavior step 4, `ActorLead`).** Step 4 grants `lead`
`department-admin` at Rangers only, but the volunteers table has the lead create positions,
members, shifts and bookings in Gate, Greeters, Medical and DPW too, so the first Gate step
fails `Permission` (volunteers F171). Three ways out: (a) grant the lead `department-admin`
at every department that gets volunteer data; (b) give the other departments' work to
another of the ten accounts; (c) add a lead account per department. (b) is impossible as the
roles stand: volunteers decision 1 seeds every lead permission into `department-admin` only,
and no other account holds it anywhere. (c) changes D53's ten accounts and the sign-in
page's table for no gain a tester can see. **Decided: (a).** Step 4 becomes: as
`access-admin`, `Grant({Person: lead, Role: department-admin, Department: D})` for D in
Rangers, Gate, Greeters, Medical, DPW, in that order (five rows; `access-admin` holds
`access.grants.manage` site-wide, so the no-escalation rule does not apply, and the rows
differ by department, so none is a `duplicate`). Sanctuary stays without a lead on purpose:
it is the demo's "department whose leads have left", which a `steward` tester repairs with
`access.department-grants.manage`. The `ActorLead` comment reads "department-admin at
Rangers, Gate, Greeters, Medical and DPW". Realism: at a small burn with few volunteers one person
often leads several departments at once (owner, 2026-10-29, Q-DEMO5), and the demo's point is that one sign-in shows every lead screen;
the account list on the sign-in page says "lead of the volunteer departments". The
`steward` still creates the org-wide qualifications and "First aid" (unchanged).

**D1.2 The pending self-upload (Proposed change 7, "records").** The table gave it to
walk-on 02, but walk-ons have no account and `UploadCertificate` records the signed-in person
as the subject; there is no path for a lead to upload on someone's behalf (volunteers
decision 2). **Decided:** the `participant` uploads it, for ICS-100 (org-wide), as the last
record step, with `volunteers.self_upload` at its default `true`. The lead then has one
pending record to verify on `/qualification-records` (org-wide, so any verify extent sees it,
volunteers decision 3), which is a better demo than a walk-on's would have been.

**D1.3 The lead's own records.** `RecordQualification` and `VerifyRecord` refuse the actor's
own records (`Invalid self`), and no other account holds `qualifications.verify` at Rangers,
so "Ranger training, Over 18 and ICS-100 verified for the lead" cannot be seeded. **Decided:**
the lead has no records; the lead holds Khaki on Friday through `Assign` with `Waive: true`
on themself, which volunteers decision 21 (withdrawn by Q-VOL7, owner: yes) allows and F152
tests. The roster then shows a flagged self-waiver, a state the owner asked for and no other
seeded row shows. A second lead account was not added (D1.1 (c)).

**D1.4 The inventory position.** The standing "Winter inventory" shift needs a DPW position
and the table had none. **Decided:** "Inventory Crew" (DPW, open, no requirements), created
by the lead (D1.1 gives `positions.manage@DPW`). The shift: the day 14 days after the event's
end day, 09:00 to 13:00 in the event's zone, 6 slots, location "Warehouse", no bookings.

**D1.5 Event dates in the conformance rule (Test plan, "Conformance"; module-contract).**
`moduletest`'s `demoseed_provider_valid` registers the request's event in `eventstest` with
no dates, so the built provider falls back to the core's own date rule (step 5's "first
Thursday at least 21 days ahead"), copied into the module. A module must not carry a core
part's rule, and the core always gives the event dates (step 5), so the fallback can only
ever serve the fake. **Decided:** the rule registers the event *with* dates, computed as
step 5 does from the rig's clock (Thursday 10:00 to the following Monday 12:00, zone
`UTC`), and a provider that finds an event without dates fails `Invalid` (field `event`,
code `required`); the fallback is removed. Volunteers F175 below.

**D1.6 `Start` before `Seed` in the conformance rule (module-contract).** `bms demo seed`
runs inside an assembly whose modules are started (app.md: the seed is step 8 of a command
that builds and starts every part), so a provider may use anything `Start` set up: the
volunteers `Assign` enqueues `volunteers.shift-reminder`, which `Start` registers, and
`jobstest.Fake.Enqueue` answers `Invalid` (`name`, `unknown`) for an unregistered name. The
rule built the module without starting it. **Decided:** the rule calls the harness's
`start` before `Provider.Seed` (the rig's cleanup stops it), as the command does, so a
provider may rely on what `Start` registered. Proposed change 18 below.

**D1.7 The bookings the table left open.** Recorded as built, so the counts are fixed:
members: `participant`, `lead` and walk-ons 01 to 06 in Rangers; walk-ons 08 to 11 in
Medical; walk-on 07 in neither (the waiver case) and walk-on 12 in neither (an open
position needs no membership). Friday 08 to 14 Dirt Ranger is full with walk-ons 01 to 04.
Walk-on 07 is assigned to Saturday 14 to 20 Dirt Ranger with `Waive: true` (no Ranger
training, not a member; the position is open so membership is not the missing piece). Gate
Crew, Thursday to Sunday: walk-on 09 on 10 to 14, 10 on 14 to 18, 11 on 18 to 22; Greeter,
Thursday to Sunday: walk-on 12 on 10 to 14, 08 on 14 to 18 (one booking per shift, no
overlaps for any person). Medic shifts have no bookings: nobody at Medical has First aid
verified, so a tester as `lead` sees the "needs First aid" eligibility state. The Sunday
20 to 02 Dirt Ranger shift is canceled by the lead with no bookings, so the cancellation
sends no mail.

**What changes where.** Behavior step 4 (five grants), the `ActorLead` comment, the account
list's lead label (web-shell `core.demo.account.lead`), Proposed change 7 (the volunteers
table, superseded by `module-volunteers.md` "Amendment D1 changes"), the Test plan's
conformance bullet (dates and `Start`), `SeedVersion` stays 1 (the demo has not shipped).

**Q-DEMO5 (owner, 2026-10-29, answered).** One `lead` account may run five departments (a
small burn has few volunteers, so one person often leads several); D53's ten accounts and
the sign-in page stand. D1.1 (a) is the decision.

**Proposed change 18** (applied 2026-10-29) (D1.5, D1.6) **`module-contract.md`:** `moduletest`'s rule
`demoseed_provider_valid` (and `WithDemoSeed`'s doc) registers the request's event with
dates (Thursday 10:00 to Monday 12:00 at least 21 days after the rig's clock, zone `UTC`)
and calls the module's `Start` before `Provider.Seed` and `Stop` after, as `bms demo seed`
does; F66's note says a provider may rely on what `Start` registered. **`add-module`
skill:** one clause: "the provider may rely on `Start` having run".

## Open questions

**Answered (owner, 2026-10-30, D58, recommendation accepted).** Q-DEMO6. The session carries it: `SessionInfo.Protected bool` (`protected`), set by identity from its `Guard` when the session is read; the password screen uses it to show the notice and disable the form; the alternative of adding the e-mail to the session was not taken. On a `Guard` error the session answer says `protected: false` and logs a warning, so a Guard hiccup cannot break the whole app; the server refusal stays the backstop (F46; owner changed this choice, 2026-10-30). Open questions: None.

Q-DEMO5 (one lead for five departments) was answered by the owner on
2026-10-29 and is folded into D1.1.

**Answered (D53).** All four were answered by the owner on 2026-10-28 and are folded into the Summary's
decisions: Q-DEMO1 (the ten accounts and the shared password) into 13; Q-DEMO2 (captured
mail) into 10, **changed from the recommendation**: mail is shown only to the account it was
addressed to, so there is no `demo.mail.read` permission and no audit entry; Q-DEMO3 (the
operator timer) into 7; Q-DEMO4 (what "not perfect" covers) into 9. Consequence of Q-DEMO2: a
signed-out visitor cannot read their own password-reset link on `/demo/mail`, so the
forgotten-password flow is testable only from a second session of the same account; the
sign-up form tells testers to use a made-up address.

## Proposed changes elsewhere

1. (applied 2026-10-28, config.md F36 to F39) **`config.md`.** Section `Demo{Enabled bool, PublicURL string}`; keys `demo.enabled`
   (`BMS_DEMO_ENABLED`, bool, default false) and `demo.public_url` (`BMS_DEMO_PUBLIC_URL`,
   default empty, canonical URL rule, `Invalid` when set while `demo.enabled` is false);
   failure modes F1–F3 above; the catalogue and `Keys()`.
2. (applied 2026-10-28, app.md row 11 and amendment) **`app.md`.** Part 11 `demo` after `files`: `declare` (`demo.Set`; no permissions, as mail is own-only (D53);
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
   for the two commands (2 for `Unsupported`/`Conflict seeded|not_empty|not_demo` only, any other Conflict 1; reset's migrate step uses the migrate command's own mapping, 3 for the
   migrator's `Conflict`, else 1); the actor closures' documented preference for a session in
   ctx over the operator (already the behaviour); `TestDemo_ChangesOnlyTheListed`; the
   end-to-end demo test.
3. (applied 2026-10-28, identity.md amendment (F99 to F108)) **`identity.md`.** (a) `EnsureAccount(ctx, Credentials) (AccountResult{Account
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
4. (applied 2026-10-28, authz.md amendment (F72 to F75)) **`authz.md`.** `EnsureGrant(ctx, GrantInput) (Grant, bool, error)`: writer-only, no
   actor, for commands (the sibling of `Seed`); site-scoped grants only (`Invalid scope` for a
   department); separation-of-duties rules checked (`Conflict`); an existing grant is returned
   with `false`; audited `access.grant.created` with `ActorOperator`; publishes
   `access.grant-created`.
5. (applied 2026-10-28, people.md amendment (F89)) **`people.md`.** `Delete`/`FulfilRequest`, `LinkAccount` and `bms delete` map identity's `Permission
   protected` to `Conflict protected` beside `Conflict superuser`; one failure mode and test.
6. (applied 2026-10-28, module-contract.md (F66)) **`module-contract.md`.** `demo` joins the reserved core areas (`IsCoreArea`); `demo.seed`
   is a core extension point; `moduletest.Conformance` gains the option `WithDemoSeed()` and
   the rule `demoseed_provider_valid` (Test plan).
7. (applied 2026-10-28, module-volunteers.md amendment (F171 to F174); the table is superseded by `module-volunteers.md` "Amendment D1 changes", 2026-10-29) **`module-volunteers.md`.** `ContributesTo` gains `demoseed.Point`; `Contributions()`
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
8. (applied 2026-10-28, notify.md) **`notify.md`.** Note that `app` may supply a `ChannelFactory` that ignores `SMTPConfig`
   (demo mode); `Configured` is unchanged. No interface change.
9. (applied 2026-10-28, files.md) **`files.md`.** Note that `app` may wrap `PolicySource` to lower the limits (demo mode);
   no interface change.
10. (applied 2026-10-28, web-shell.md amendment (F40 to F52)) **`web-shell.md`.** `useDemo()` in `@/api` over `GET /api/demo` read at boot;
    `DemoBanner` in the frame (every screen; expanded with the account table on
    `/accounts/sign-in`); keys `shell.demo.banner.title|body`, `shell.demo.accounts.title`,
    `shell.demo.password`, `shell.demo.sign_in_as` (added 2026-10-30), `shell.demo.mail_link`, `shell.demo.signup_note` (Q-DEMO2: use a made-up address), `core.demo.mail.note`; `core.demo.*` keys for the ten account
    labels, the six department labels and the mail screen; `web/src/core/demo/` as a core
    area with `/demo/mail`; the accounts area's `field.protected` message; the people
    deletion screen's `Conflict protected` message.
11. (applied 2026-10-28, api.md amendment (F63, F64)) **`api.md`.** `Options.NoIndex bool`: when set, every UI-handler response (D29's set)
    carries `X-Robots-Tag: noindex, nofollow`; a test.
12. (applied 2026-10-28, db.md) **`db.md`.** The fixed core set order gains `demo` after `files`;
    `demo.postgres.Migrations`, `demo.Set`.
13. (applied 2026-10-28, telemetry.md metrics table and spans) **`telemetry.md`.** Instrument rows `bms.demo.seeds{outcome}`, `bms.demo.resets{outcome}`,
    `bms.demo.mail_captured`; span names `demo.seed`, `demo.seed.step`, `demo.reset`,
    `demo.mail.capture`.
14. (applied 2026-10-28, 07-abstractions.md) **`07-abstractions.md`.** A `### demo` entry under Core (purpose, owns, doesn't do, spec
    link) and `contracts/demoseed` beside `greeter`, `personpage`, `eventsetup`,
    `membership` in the contracts entry; the Delivery entry for `app` lists the `demo`
    command.
15. (applied (added with this draft)) **`plan/design/README.md`.** The row for this spec (added with this draft).
16. (pending: target not written (deploy/ and docs/operating.md)) **`deploy/` and `docs/operating.md`** (targets not written): `deploy/systemd/
    bms-demo-reset.service` and `.timer` (disabled), the compose equivalent, the operator
    text: what demo mode changes, the reset's recovery steps, "the banner is not a
    substitute for keeping real data out", and that backups of a demo hold whatever strangers
    typed.
17. (applied 2026-10-28, add-module skill) **`.claude/skills/add-module/SKILL.md`** (main session): a module that wants demo data
    adds `demo.go` with a `demoseed.Provider` and `WithDemoSeed()` in its conformance test.
