# people

- **Status:** Implemented (2026-10-18); Approved (2026-10-18; owner approved every recommended answer)
- **Amended 2026-10-18 (M2 build, accepted deviations):** implementation deviations recorded in Behavior, "Accepted implementation deviations (M2 build, 2026-10-18)"; five owner questions raised by the build were answered 2026-10-18 (D37) and are folded into decisions 21 to 25 (plus 26 for the grant-revoked signal). Records of what was built, not design changes.
- **Package:** `internal/core/people` (contract and service), `internal/core/people/postgres`
  (store, SQL, migrations), `internal/core/people/peopletest` (the fake `Directory` modules
  test with), `internal/contracts/personpage` (the person-page extension point's shape).
  **UI:** `web/src/core/people/` (screen set `people`, keys `core.people.*`).
- **Depends on specs:** `model.md` (Implemented): `ID`, `PersonID`, `AccountID`, `Email`,
  `Sensitivity.ReadIsAudited`. `lib-errs.md`, `lib-core.md` (Implemented): `errs`, `clock`,
  `validate`. `db.md` (Implemented): `DB`, `Transact`, `AfterCommit`, `Wrap`, `ErrReadOnly`,
  `Set`, `dbtest`. `signals.md` (Implemented): `Bus`, `Name`, `Payload`, `Signal`,
  `InDelivery`. `audit.md` (Implemented): `Recorder`, `Entry`, `Ref`, `CountAbout`.
  `identity.md` (Implemented): `PersonFrom`, `SessionFrom`, `NameSource`,
  `SignalAccountCreated`, `ExportPerson`, `DeletePerson`; the two methods proposed below
  (`EmailOf`, `LinkAccount`). `authz.md` (Approved): `Authorizer` (`Allow`, `AllowIn`,
  `ExtentOf`), `Actor`/`ActorFunc`, `PermissionDecl.StarterRoles`, the starter role keys,
  `ExportPerson`, `DeletePerson`, Proposed change 9. `module-contract.md` (Implemented):
  `PersonData`, `Export`, `Deletion`, `Kept`, `Set.PersonData()`, `Set.Contributors`,
  `Env.CoreSignals`, `Env.CoreExtensionPoints` (all reached through `app`; this package
  never imports `core/module`). `telemetry.md`, `api.md` (`httpx`), `web-shell.md`
  (`ScreenSet`, `NavEntry.access`), `app.md` (part 5) (Implemented).
- **Used by:** every module (`Deps.People`, the `people.person-page` point, the
  `people.person-deleted` signal), `identity` (`NameSource`), `authz` and `audit` (the
  `search` and `names` routes behind their screens), the web shell (the profile prompt and
  the saved language), `app` (part 5, the `export` and `delete` commands).

## Summary

**Purpose.** Hold who each participant is: their profile (playa name first), their contact details and emergency contacts, the consents they gave, and the one place that exports or deletes everything bms holds about them. Package `internal/core/people`; imports `model`, `lib`, `core/db`, `core/audit`, `core/authz` (the `Authorizer` and leaf types), `core/identity` (`PersonFrom`, `SessionFrom`, taken as `Options.Actor`), `core/signals`, `core/telemetry`, `contracts/personpage`. Never `core/module`, `api`, `app`, `config` or a module: the modules' `PersonData` and the page contributions reach it through `app`'s adapters (`Link`).

**Names.** `people` is the package, the core part, the migration set, the route set (`/api/people/`), the permission prefix, the audit area, the signal prefix, the settings prefix and the screen set. The extension point's shape lives in `contracts/personpage` so a module never imports two packages called `people`.

**Exported (`people`).**
- `ContactID`, `RequestID` (= `model.ID[...]`); `Purpose` (a consent purpose, `<owner>.<ident>`, +`Validate`, `Owner`); `FieldMode` (`FieldHidden`, `FieldOptional`, `FieldRequired`; zero invalid; `ParseFieldMode`, `String`); `RequestStatus` (`RequestOpen`, `RequestWithdrawn`, `RequestDeclined`).
- `Person{ID, PlayaName, Pronouns, Language, LegalName, Phone, ContactEmail, Email model.Email, HasAccount, Complete bool, CreatedAt, UpdatedAt}` (+`Validate`, `LogValue` = ID only); `EmergencyContact{ID, Person, Name, Phone, Relationship, Position int, CreatedAt, UpdatedAt}`; `Consent{Person, Purpose, Granted bool, RecordedBy, ChangedAt}`; `Request{ID, Person, Kind, Status, Note, CreatedAt, DecidedAt, DecidedBy, DecisionNote}`; `Profile{Person, Fields []string, Contacts, Consents, Sections []personpage.Section, Can Capabilities, Policy}`; `Capabilities{Edit, ReadContacts, Export, Delete, Link, ViewRoles, ViewAudit bool}`; `Match{ID, PlayaName, Pronouns}`; `ProfileInput{PlayaName, Pronouns, Language, LegalName, Phone, ContactEmail *string}` (nil = unchanged); `ContactInput{Name, Phone, Relationship}`; `SearchQuery{Q string, Limit int}`; `RequestQuery{Status, Person}`; `LinkResult{Account model.AccountID, Orphan model.PersonID, OrphanDeleted bool}`; `ExportFormat = 1`; `DeletionReport{Person, People, Identity, Access int, Modules map[string]ModuleDeletion, Kept []Kept}`, `ModuleDeletion{Deleted, Anonymized int, Kept []Kept}`, `Kept{Kind string, Count int}`, `Export{Data json.RawMessage}` (structural twins of `module`'s; `app` converts).
- `Policy{LegalName, Phone, EmergencyContacts FieldMode}` (+`Validate`), `DefaultPolicy()` (optional, optional, optional); `PolicySource{Policy(ctx) (Policy, error)}` (settings, M2 [nil = `DefaultPolicy`]).
- Seams `app` fills: `Accounts{EmailOf(ctx, PersonID) (model.Email, error); LinkAccount(ctx, model.Email, PersonID) (model.AccountID, model.PersonID, error); ExportPerson(ctx, PersonID) (json.RawMessage, error); DeletePerson(ctx, PersonID) (int, error)}` (`*identity.Service`; the two first methods are proposed); `Access{ExportPerson; DeletePerson(ctx, PersonID) (int, error)}` (`*authz.Service`); `AuditCounter{CountAbout(ctx, PersonID) (int, error)}` (`*audit.Log`); `Source{Name string; Exporter PersonData}` with `PersonData{ExportPerson(ctx, PersonID) (Export, error); DeletePerson(ctx, PersonID) (ModuleDeletion, error)}`; `SectionProvider{Module string; personpage.Provider}`.
- `Directory` (= `module.Deps.People`): `NamesOf(ctx, []model.PersonID) (map[model.PersonID]string, error)`, `ConsentOf(ctx, model.PersonID, Purpose) (bool, error)`.
- `Options{DB, Clock, Logger, Tracer, Meter, Audit (scoped "people"), Signals (scoped "people"), Authz authz.Authorizer, Actor authz.ActorFunc, Accounts, Access, AuditCount AuditCounter, Policy PolicySource}` (+`Validate`); `New(Options) (*Service, error)` (no I/O; subscribes to `identity.account-created`; works against `db.None()`); `(*Service).Link(sources []Source, sections []SectionProvider) error` (once, from `app`'s `link`).
- `*Service` (safe for concurrent use; nothing to close): `Directory` + `PlayaNameOf` (= `identity.NameSource`); `Me`, `UpdateMe`, `Get`, `Create`, `Update`, `AddContact`, `UpdateContact`, `RemoveContact`, `SetConsent`, `Search`, `LinkAccount`, `Export`, `Delete`, `RequestDeletion`, `MyRequests`, `WithdrawRequest`, `ListRequests`, `DeclineRequest`, `FulfilRequest`, `ExportCommand(ctx, io.Writer, string) error`, `DeleteCommand(ctx, io.Writer, string) error`, `Routes() []Route`, `OpenAPI() []byte`; `Route{Method, Pattern string, Handler http.Handler}` (all signed-in; `app` converts).
- Constants: `MaxPlayaNameBytes = 100`, `MaxPronounsBytes = 40`, `MaxLegalNameBytes = 200`, `MaxPhoneBytes = 40`, `MaxRelationshipBytes = 60`, `MaxLanguageBytes = 35`, `MaxNoteBytes = 500`, `MaxEmergencyContacts = 10`, `MinSearchBytes = 2`, `MaxSearchBytes = 100`, `MaxSearchResults = 50`, `DefaultSearchResults = 20`, `MaxNames = 200`, `ExportCooldown = 10 * time.Minute`, `MaxTrackedExports = 10000`, `SectionTimeout = 2 * time.Second`.
- Permissions: `people.directory.read` (department-scopable, reaches `internal`), `people.contacts.read` (department-scopable, `personal`), `people.profiles.manage` (site, `personal`), `people.data.export`, `people.data.delete` (site, `sensitive`); `Permissions []authz.PermissionDecl`. Signals published `people.person-created {person}`, `people.person-deleted {person}`, `people.deletion-requested {request, person}`; subscribed `identity.account-created`. Extension point `people.person-page` (`personpage.Provider`). Settings `people.legal_name`, `people.phone`, `people.emergency_contacts` (enum `hidden|optional|required`; emergency contacts never `required`, Q-CORE7). Audit actions `people.person.read|created|changed|deleted|linked|exported`, `people.search.read`, `people.contact.created|changed|deleted`, `people.consent.changed`, `people.request.created|withdrawn|declined|fulfilled`. `postgres.Migrations fs.FS`; `Set = db.Set{Name: "people", Schema: "core"}`. Commands `bms export <person-id>`, `bms delete <person-id>`.
- `peopletest.NewFake() *Fake` (a `Directory` with `Fake.Add(id, name)`, `Fake.Consent(id, purpose, granted)`; safe for concurrent use).
- `personpage` (contract): `Item{Label, Value, Href string}`, `Section{Key string, Items []Item, Failed bool}`, `Provider{Sections(ctx, []model.PersonID) (map[model.PersonID][]Section, error)}`; `Point = "people.person-page"`.

**Key decisions.** 1 one `core.person` row per `PersonID` minted by identity (sign-up) or by people (`Create`); modules and authz reference it by foreign key, so a dangling person ID cannot be written. 2 the row for a new account is created by the `identity.account-created` handler (idempotent) and, because delivery is at most once, repaired by the person's first `UpdateMe`; no synchronous creation inside `SignUp` (owner, 2026-10-18); `Me` on a missing row answers an empty, incomplete profile without writing (works on a read-only copy). 3 the playa name is the name shown everywhere; it is required, trimmed, not unique (two burners may share one; the UI shows pronouns and lets a lead tell them apart) and empty until the person fills it in; the shell prompts once per session, it never blocks. 4 the login e-mail is identity's (identity decision 17): people reads it through `Accounts.EmailOf` for display and holds `contact_email` only for people without an account; search by login address is the accounts screen's. 5 legal name is one field (waivers differ; name order differs by culture; a later migration can add structured names) (owner, 2026-10-18); phone is stored as entered after a loose shape check. 6 `required` field modes bind the person's own saves only: an admin may create or edit a profile with gaps (a lead adding a walk-on may not know the legal name); the profile is then `Complete: false`. 7 no per-field visibility choices in M2: the personal fields are already restricted to the person and permitted roles and pronouns exist to be seen; future sharing choices are consents. 8 consents are rows keyed by purpose, granted and revoked by the person (or recorded by a profile manager for a paper form), read by modules through `Directory.ConsentOf`; the core declares no purpose in M2; the flag is `internal`; the table and `ConsentOf` ship in M2 anyway so a module author needs no core change (owner, 2026-10-18). 9 `people.directory.read` and `people.contacts.read` are department-scopable so a department role may hold them (authz decision 1); until a membership contract exists (volunteers), holding either in any extent reaches every person, which the spec says out loud; `people.contacts.read` is seeded into `account-admin` only in M2 and `department-admin` gains it when the extent can be limited to the department's volunteers (owner, 2026-10-18). 10 a self export is immediate (audited, `ExportCooldown`) because it changes nothing and the data is the person's (a "confirm your password" step is Later hardening) (owner, 2026-10-18); a self deletion is a request a holder of `people.data.delete` fulfils or declines, because it is irreversible and touches safety records. 11 deletion is one `Write` transaction: modules in build order, then authz, then identity, then people's rows; any failure (`last_access_admin`, a superuser account, a module error) rolls everything back; audit rows stay and are reported as `Kept{Kind: "audit.entry"}` (audit decision 10). 12 export runs in no transaction and people records the one `people.person.exported` entry; modules do not audit inside `ExportPerson`. 13 `LinkAccount` takes the account's e-mail (no ID pasting on a phone) and goes through identity; the person row sign-up minted for that account is deleted afterwards in a second transaction only when it is empty and unreferenced, else kept and reported. 14 the person page's module sections come from `people.person-page` through `app`; each provider runs under `SectionTimeout`, a failing provider yields `Section{Failed: true}` and never fails the page. 15 reads of personal fields about someone else record one `people.person.read` with the fields actually returned; self reads are not audited (`ReadIsAudited(self)`); a search that matched personal fields records `people.search.read` per result. 16 `Reason` on audit entries is a token (`request:<id>`, `command`, `admin`), never free text. 17 regional differences are the three settings and role membership; no retention setting yet (nothing measures inactivity; M4 proposes `people.retention.unused` with `jobs`). 18 the person-facing "your data was viewed" is Later (D22, audit's `ViewsOf`); in M2 the person page links holders of `audit.search` to `/audit?subject=<id>`. 19 the person's download, deletion requests, the admin's export and delete and the `bms export` / `bms delete` commands ship in M2, not M4 (09 §11 and the milestone table moved) (owner, 2026-10-18). 20 `people.directory.read` is not in the base role by default (no open directory); a regional that wants one adds it to `participant` (owner, 2026-10-18). 21 (D37) a language-only `PUT /me` (the body sets nothing but `language`) is exempt from the required-fields check, so the shell can save the language before the profile is complete. 22 (D37) the consent audit entry (`people.consent.changed`) records the purpose and the direction (`granted` or `withdrawn`). 23 (D37) a walk-on's `contact_email` is cleared when an account is linked (the account's address replaces it). 24 (D37) reads of deletion-request notes (`MyRequests`, `ListRequests`) by someone other than the requester are audited. 25 (D37) data rights (`Export`, `Delete`, and the commands) for an account holder with no `core.person` row still run the identity and authz steps. 26 (D37) `Delete` publishes, through authz's `DeletePerson`, `access.grant-revoked` for each removed grant.

**Errs kinds callers handle.** Every use case: `Unauthenticated` (no actor), store kinds (`Unavailable`, `Timeout`, `Canceled`), `Unavailable` wrapping `db.ErrReadOnly` on writes. `Get`/`Update`/contacts on another person: `Permission` (no `people.directory.read` anywhere; or no `people.profiles.manage` for a change), `NotFound` (unknown ID). Inputs: `Invalid` with field codes `required`, `too_long {max}`, `malformed`, `control_chars`, `hidden`, `too_many {max}`, `one_of`, `self`, `unknown`. `Search`: `Invalid` (`q` `too_short {min}`/`too_long`, `limit` `range`), `Permission`. `NamesOf`: `Invalid` (`ids` `too_many`). `SetConsent`: `Invalid` (purpose), `Permission`. `LinkAccount`: `Conflict` (`has_account`, `superuser`), `NotFound` (field `email` code `unknown`; never says more). `Export`: `Permission`, `NotFound`, `Unavailable` wrapping `httpx.ErrRateLimited` (self cooldown). `Delete`/`FulfilRequest`: `Permission`, `NotFound`, `Invalid` (`person` `self`), `Conflict` (`last_access_admin` from authz, `superuser` from identity, `in_use` from a foreign key), the module's error wrapped with its name. Requests: `Conflict duplicate` (an open request exists), `Conflict status` (not open). `Link`: `Internal` (called twice, nil provider). `New`/`Options.Validate`: `Invalid` with fields `people.db`, `people.clock`, `people.logger`, `people.tracer`, `people.audit`, `people.signals`, `people.authz`, `people.actor`, `people.accounts`, `people.access`, `people.audit_count`.

**Owns.** Tables `core.person`, `core.person_emergency_contact`, `core.person_consent`, `core.data_request`; the five permissions; three signals and one subscription; the extension point; the three settings; the audit actions above; route set `people` (listed under Interface); screens `/people/me`, `/people`, `/people/new`, `/people/:person`, `/people/admin/requests`; keys `core.people.*`; spans `people.<use-case>`, `people.section`; metrics `bms.people.exports{outcome}`, `bms.people.deletions{outcome}`, `bms.people.sections{module,outcome}`. Commands `export`, `delete`.

**Expects of dependencies.** (D37: `authz.DeletePerson` publishes `access.grant-revoked` per removed grant.) `identity`: `PersonFrom`/`SessionFrom` semantics; `EmailOf` and `LinkAccount` (proposed); `DeletePerson` refusing a superuser account with `Permission` (proposed); `NameSource` maps `NotFound` to `""`. `authz`: `ExtentOf` for the any-extent checks, `Allow` for site permissions, `Conflict last_access_admin` from `DeletePerson`, migration `0002` adding the foreign key to `core.person`, starter-role seeding from `StarterRoles`. `audit`: a `Recorder` scoped `people`; `CountAbout` callable inside a `Write` transaction; the `/audit?subject=` screen filter. `signals`: a `Bus` scoped `people` (publishes the three, subscribes `identity.account-created`); `InDelivery`. `db`: `Transact` ctx-carried transactions joined by nested calls (so modules' `DeletePerson` run inside people's), `*Error.Constraint`, `ErrReadOnly`. `app`: part 5 (declare, build, link, commands, the `NameSource` adapter for identity, the `Accepts` for the point, `Deps.People`). `module-contract`: `Deps.People people.Directory`; `peopletest.NewFake()` in `Conformance`. `settings` (M2): the three keys through `PolicySource`. `web-shell`: `GET /api/people/me` read once per session by the frame (language, profile prompt); `web/src/core/people/` as a core area.

**Open questions:** None (the seven were settled 2026-10-18, folded into decisions 2, 5, 8, 9, 10, 19 and 20; the five raised by the M2 build were answered 2026-10-18, D37, decisions 21 to 25). **Proposed changes elsewhere:** 18, listed at the end; 14 applied 2026-10-18, 4 pending (settings, notify, jobs, `docs/operating.md`: targets not written).

**Accepted deviations (M2 build, 2026-10-18).** Leaf package `people/rows`; extra exports (`PublishedSignals`, `SubscribedSignals`, `ParseRequestStatus`, `RequestStatus.String`, the Field constants, `personpage.SectionTimeout`); `UpdateMe`, `LinkAccount` and `Delete` take the deletion lock; export cooldown kept on Canceled; exit codes of the commands. Five owner questions are open. See Behavior and Open questions.

## Purpose

Hold who each participant is, so every other part of bms can refer to a person by ID and show them by playa name.

## Responsibilities

- Keep one profile per person: playa name, pronouns, saved language (`internal`); legal name, phone, a contact address for people without an account (`personal`); whether the person has an account and whether the profile is complete under the regional's settings.
- Keep each person's emergency contacts (any number up to `MaxEmergencyContacts`, never required; Q-CORE7).
- Record consents by purpose and answer "did this person consent to X" for modules.
- Create the person row when identity creates an account, and let a profile manager create a person without an account and link an account to them later.
- Answer name lookups (batch) and a search by playa name (and, for permitted roles, by legal name, phone and contact address) without revealing anyone the searcher may not see.
- Assemble the person page and "my page": the profile plus the sections every enabled module contributes through `people.person-page`.
- Export everything bms holds about one person, and delete it, across identity, authz and every module, in one audited operation; take and decide deletion requests; the `export` and `delete` commands.
- Audit every read of personal fields by someone other than the person and every change; publish `people.person-created`, `people.person-deleted`, `people.deletion-requested`.
- Fill identity's `NameSource` so the session carries the playa name.

## Not responsible for

| Not this | Whose it is |
|---|---|
| The login address, passwords, sessions, `ChangeEmail` (M4), who is the superuser | `identity` (D21; identity decision 17). People shows the address through `Accounts.EmailOf`. |
| Deciding who may read or change a profile | `authz`; people names its permissions and calls `Allow`/`ExtentOf`. |
| Storing audit entries; the person's own "my data was viewed" view (D22) | `audit` (`ViewsOf`, Later). People records entries and links to the audit screen. |
| Module data about a person (shifts, registrations, incidents) and its export or deletion | each module's `PersonData`; people orchestrates and reports. |
| Which people belong to a department (so a lead sees only their volunteers' contacts) | `volunteers` (a later membership contract); until then decision 9 applies. |
| Qualifications, age, certificates, photos, addresses, "about me" | not collected (09 §3; Q-QUAL). |
| Retention jobs, e-mail to the person or the admins about requests | `jobs` and `notify` (M4), through `people.deletion-requested`. |
| Merging two person rows | Later; `LinkAccount` only tidies an empty orphan (decision 13). |
| Invitations, sign-up fields beyond e-mail and password | `identity` (M4). |
| Translating purpose and section titles | the owning module's catalogue (`module.<name>.consent.<ident>.*`, `module.<name>.section.<ident>.*`). |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| Profiles | keep each person's identity-free profile and say what a viewer may see of it | `core.person`; `Me`, `Get`, `Create`, `Update`, `UpdateMe`, `PlayaNameOf`, `NamesOf`, `Search` |
| Contacts | keep each person's emergency contacts | `core.person_emergency_contact`; `AddContact`, `UpdateContact`, `RemoveContact` |
| Consents | record what a person agreed to, by purpose | `core.person_consent`; `SetConsent`, `ConsentOf` |
| Linking | create the row behind a new account and join an existing person to an account | the `identity.account-created` handler; `LinkAccount` |
| Person page | assemble the profile with every module's sections | `personpage.Provider`, `Link`, `SectionTimeout` |
| Data rights | export and delete one person across the system; take deletion requests | `core.data_request`; `Export`, `Delete`, `Request*`, the commands |
| People API and screens | expose the above over `/api/people/` and in `web/src/core/people/` | `Routes`, `OpenAPI`, the five screens |

## Interface

```go
package people

// Purpose names what a person consented to: "<owner>.<ident>", owner a module name or a
// core area, ident per the module-contract grammar; at most 64 bytes.
type Purpose string

func (p Purpose) Validate() error // errs.Invalid
func (p Purpose) Owner() string   // the part before the first dot; "" if invalid

// FieldMode is a regional's choice for an optional profile field.
type FieldMode uint8

const (
	FieldHidden   FieldMode = iota + 1 // not shown, not stored on save (Invalid "hidden")
	FieldOptional                      // shown, may stay empty
	FieldRequired                      // the person's own save must fill it
)

func ParseFieldMode(s string) (FieldMode, error) // "hidden" | "optional" | "required"
func (m FieldMode) String() string
func (m FieldMode) Validate() error

// Policy is what a regional set (settings people.*). Emergency contacts may be hidden or
// optional, never required (Q-CORE7): Validate fails with field "people.emergency_contacts"
// code "one_of" for FieldRequired.
type Policy struct {
	LegalName         FieldMode
	Phone             FieldMode
	EmergencyContacts FieldMode
}

func DefaultPolicy() Policy // optional, optional, optional
func (p Policy) Validate() error

// PolicySource is settings' seam (M2). nil means DefaultPolicy.
type PolicySource interface {
	Policy(ctx context.Context) (Policy, error)
}

type contact struct{}
type request struct{}

type ContactID = model.ID[contact]
type RequestID = model.ID[request]

// Person is one profile. Personal fields (LegalName, Phone, ContactEmail, Email) are zero
// when the viewer may not see them; Profile.Fields says which were included. LogValue
// emits the ID only.
type Person struct {
	ID         model.PersonID
	PlayaName  string      // internal; "" until filled in
	Pronouns   string      // internal
	Language   string      // internal; BCP 47 tag or "" (the regional's default)
	LegalName  string      // personal
	Phone      string      // personal
	ContactEmail model.Email // personal; only for a person without an account
	Email      model.Email // personal; the login address (identity), zero without an account
	HasAccount bool
	Complete   bool // PlayaName set and every FieldRequired field set
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (p Person) Validate() error
func (p Person) LogValue() slog.Value

// EmergencyContact is one entry on a person's list. Every text field is personal.
type EmergencyContact struct {
	ID           ContactID
	Person       model.PersonID
	Name         string
	Phone        string
	Relationship string
	Position     int // 1-based display order
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Consent is the current state of one purpose for one person. The flag is internal.
type Consent struct {
	Person     model.PersonID
	Purpose    Purpose
	Granted    bool
	RecordedBy model.PersonID // the person, or the profile manager who recorded a paper form
	ChangedAt  time.Time
}

type RequestStatus uint8

const (
	RequestOpen RequestStatus = iota + 1
	RequestWithdrawn
	RequestDeclined
)

// Request is a person's deletion request. Kind is "delete" in M2 (the column exists so an
// export request can be added without a migration). Note and DecisionNote are personal.
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

// Capabilities tells the shell which actions the viewer may take on this profile. The
// server checks again on every call.
type Capabilities struct {
	Edit, ReadContacts, Export, Delete, Link, ViewRoles, ViewAudit bool
}

// Profile is what Me and Get return: the person as the viewer may see them, the field
// names included, their contacts (only when ReadContacts or self), consents (self or
// profiles.manage), the modules' sections, what the viewer may do, and the field policy
// so the form knows what to show and require.
type Profile struct {
	Person   Person
	Fields   []string // "playa_name", "pronouns", "language", "legal_name", "phone", "contact_email", "email", "emergency_contacts", "consents"
	Contacts []EmergencyContact
	Consents []Consent
	Sections []personpage.Section
	Can      Capabilities
	Policy   Policy
}

// ProfileInput is a partial update: nil leaves a field unchanged; a pointer to "" clears
// it. Validation is per field (trim; UTF-8; no control characters; byte limits; Email
// shape for ContactEmail; language grammar for Language).
type ProfileInput struct {
	PlayaName    *string
	Pronouns     *string
	Language     *string
	LegalName    *string
	Phone        *string
	ContactEmail *string
}

type ContactInput struct {
	Name         string
	Phone        string
	Relationship string
}

type SearchQuery struct {
	Q     string // MinSearchBytes..MaxSearchBytes after trimming
	Limit int    // 0 = DefaultSearchResults; at most MaxSearchResults
}

// Match is a search result: internal fields only.
type Match struct {
	ID        model.PersonID
	PlayaName string
	Pronouns  string
}

type RequestQuery struct {
	Status RequestStatus  // zero = open
	Person model.PersonID // zero = any
}

type LinkResult struct {
	Account       model.AccountID
	Orphan        model.PersonID // the row sign-up minted for the account; zero if none
	OrphanDeleted bool
}

// Export, ModuleDeletion and Kept are structural twins of module.Export, module.Deletion
// and module.Kept (this package never imports core/module; app converts).
type Export struct{ Data json.RawMessage }
type Kept struct {
	Kind  string
	Count int
}
type ModuleDeletion struct {
	Deleted, Anonymized int
	Kept                []Kept
}

// DeletionReport is what Delete, FulfilRequest and the delete command return.
type DeletionReport struct {
	Person   model.PersonID
	People   int                       // rows this package deleted
	Identity int                       // account rows
	Access   int                       // grant rows
	Modules  map[string]ModuleDeletion // by module name
	Kept     []Kept                    // always includes {"audit.entry", n} when n > 0
}

// PersonData is what app adapts each module.ModulePersonData to.
type PersonData interface {
	ExportPerson(ctx context.Context, person model.PersonID) (Export, error)
	DeletePerson(ctx context.Context, person model.PersonID) (ModuleDeletion, error)
}

// Source is one module's PersonData with its name, in build order.
type Source struct {
	Name     string
	Exporter PersonData
}

// SectionProvider is one module's contribution to people.person-page.
type SectionProvider struct {
	Module   string
	Provider personpage.Provider
}

// Accounts is identity's seam. EmailOf fails NotFound when the person has no account.
// LinkAccount re-points the account with that address to person and returns the account
// ID and the person ID it pointed at before (the orphan sign-up minted); it fails NotFound
// (field "email", code "unknown") when no account has the address, Conflict "superuser"
// for a superuser account, Conflict "has_account" when person already has one. Neither
// checks a permission: people has authorized and audits the use case.
type Accounts interface {
	EmailOf(ctx context.Context, person model.PersonID) (model.Email, error)
	LinkAccount(ctx context.Context, email model.Email, person model.PersonID) (model.AccountID, model.PersonID, error)
	ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
	DeletePerson(ctx context.Context, person model.PersonID) (int, error)
}

// Access is authz's seam (its ExportPerson/DeletePerson).
type Access interface {
	ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
	DeletePerson(ctx context.Context, person model.PersonID) (int, error)
}

// AuditCounter is audit.Log's CountAbout, for the deletion report.
type AuditCounter interface {
	CountAbout(ctx context.Context, person model.PersonID) (int, error)
}

// Directory is what every module receives as Deps.People.
type Directory interface {
	// NamesOf returns the playa name of each known person (missing or deleted IDs are
	// omitted; an unfilled name is ""). Internal level: any actor, including a signal
	// handler or a job running as the system. Consumers: modules that show people
	// (volunteers' rosters, notify's templates); in M2 the HTTP form serves the access
	// and audit screens. Fails Invalid when len(ids) > MaxNames.
	NamesOf(ctx context.Context, ids []model.PersonID) (map[model.PersonID]string, error)
	// ConsentOf reports whether person currently consents to purpose (false when unknown
	// or deleted). Internal level, no permission: the module is deciding whether it may
	// process; the module audits the read it then performs. Consumers: registrations
	// ("share my contact details with the camp"), any module that gates a field on
	// consent. Fails Invalid for a bad purpose or a zero person.
	ConsentOf(ctx context.Context, person model.PersonID, purpose Purpose) (bool, error)
}

type Options struct {
	DB         db.DB
	Clock      clock.Clock
	Logger     *slog.Logger
	Tracer     telemetry.Tracer
	Meter      telemetry.Meter // optional
	Audit      audit.Recorder  // scoped "people"
	Signals    signals.Bus     // scoped "people": publishes the three, subscribes identity.account-created
	Authz      authz.Authorizer
	Actor      authz.ActorFunc // app: identity.PersonFrom + SessionFrom.Superuser
	Accounts   Accounts
	Access     Access
	AuditCount AuditCounter
	Policy     PolicySource // optional; nil = DefaultPolicy
}

// Validate fails errs.Invalid joining a validate.FieldError per nil dependency (fields
// people.db, people.clock, people.logger, people.tracer, people.audit, people.signals,
// people.authz, people.actor, people.accounts, people.access, people.audit_count).
func (o Options) Validate() error

// New validates, builds and subscribes the identity.account-created handler on
// Options.Signals (no I/O, so it works against db.None(); app decision 7).
func New(o Options) (*Service, error)

// Service implements Directory, identity.NameSource and the use cases below. Safe for
// concurrent use; nothing to close.
type Service struct{ /* unexported */ }

// Link hands the service the modules' PersonData and page providers, once, from app's
// link step (after module Build, before api.New). Fails Internal on a second call or a
// nil provider. Before Link, Export and Delete fail Internal and pages carry no sections.
func (s *Service) Link(sources []Source, sections []SectionProvider) error

// Directory and NameSource.
func (s *Service) NamesOf(ctx context.Context, ids []model.PersonID) (map[model.PersonID]string, error)
func (s *Service) ConsentOf(ctx context.Context, person model.PersonID, purpose Purpose) (bool, error)
// PlayaNameOf is identity.NameSource: NotFound when there is no row yet (identity shows "").
func (s *Service) PlayaNameOf(ctx context.Context, person model.PersonID) (string, error)

// Profiles.
func (s *Service) Me(ctx context.Context) (Profile, error)
func (s *Service) UpdateMe(ctx context.Context, in ProfileInput) (Profile, error)
func (s *Service) Get(ctx context.Context, person model.PersonID) (Profile, error)
func (s *Service) Create(ctx context.Context, in ProfileInput) (Profile, error)
func (s *Service) Update(ctx context.Context, person model.PersonID, in ProfileInput) (Profile, error)
func (s *Service) Search(ctx context.Context, q SearchQuery) ([]Match, error)

// Contacts (person is the actor's own ID, or any with people.profiles.manage).
func (s *Service) AddContact(ctx context.Context, person model.PersonID, in ContactInput) (EmergencyContact, error)
func (s *Service) UpdateContact(ctx context.Context, person model.PersonID, id ContactID, in ContactInput) (EmergencyContact, error)
func (s *Service) RemoveContact(ctx context.Context, person model.PersonID, id ContactID) error

// Consents.
func (s *Service) SetConsent(ctx context.Context, person model.PersonID, purpose Purpose, granted bool) (Consent, error)

// Linking.
func (s *Service) LinkAccount(ctx context.Context, person model.PersonID, email model.Email) (LinkResult, error)

// Data rights.
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

// Routes are relative to /api/people, all signed-in; app converts to module.Route.
func (s *Service) Routes() []Route
func (s *Service) OpenAPI() []byte

type Route struct {
	Method  string
	Pattern string
	Handler http.Handler
}
```

```go
package personpage // internal/contracts/personpage: imports model only

// Point is the extension point's name. app declares it in Env.CoreExtensionPoints with
// Accepts = func(v any) bool { _, ok := v.(Provider); return ok }.
const Point = "people.person-page"

// Item is one line in a section. Label is a message key in the contributing module's
// catalogue; Value is data already authorized for the viewer; Href is a shell route or "".
type Item struct {
	Label string
	Value string
	Href  string
}

// Section is one module's block on a person page. Key is "<module>.<ident>" and names
// the title key module.<module>.section.<ident>.title. Failed is set by people when the
// provider failed or timed out; Items is then empty.
type Section struct {
	Key    string
	Items  []Item
	Failed bool
}

// Provider is what a module contributes. It receives the viewer through
// identity.PersonFrom(ctx) and must return only what that viewer may see of each person,
// auditing its own personal and sensitive reads (D19: the provider checks permissions for
// the person asking). Batch: one call per page, len(people) >= 1. A person with nothing to
// show is simply absent from the map. Fails with the module's own kinds; people turns any
// error into Section{Failed: true}.
type Provider interface {
	Sections(ctx context.Context, people []model.PersonID) (map[model.PersonID][]Section, error)
}
```

**Per method: consumer, pre- and post-conditions.** "Self" means the actor's `PersonID` equals `person`. "Any extent" means `ExtentOf(p)` is not zero (site or at least one department), decision 9.

| Method | Consumer | Pre | Post |
|---|---|---|---|
| `NamesOf` | modules (`Deps.People`), `GET /names` for the access and audit screens | ≤ `MaxNames` IDs | map of known IDs → playa name; no audit (internal) |
| `ConsentOf` | modules (registrations, Later) | valid purpose, non-zero person | current flag; false for unknown |
| `PlayaNameOf` | identity (`SessionInfo`) | — | name or `NotFound` |
| `Me` | the profile screen, the shell's frame (language, prompt) | an actor | full profile with every field group, `Can` for self; a missing row answers an empty `Person{ID: actor}` with `Complete: false`, no write |
| `UpdateMe` | the profile screen | an actor; fields valid; `FieldRequired` fields non-empty after the update (not checked when the body sets only `language`, D37); `FieldHidden` fields absent | row upserted (decision 2); `people.person.changed` (or `.created` plus `people.person-created` when the row was missing) in the transaction |
| `Get` | the person page; admin screens | `people.directory.read` in any extent (self → `Me`) | internal fields; personal fields and contacts when `people.contacts.read` in any extent; consents when `people.profiles.manage`; sections; `people.person.read` recorded when any personal field was included |
| `Create` | "new person" screen | `people.profiles.manage`; `PlayaName` non-empty; `ContactEmail` optional | row with `HasAccount: false`; `people.person.created`; `people.person-created` published |
| `Update` | person page edit (admins) | `people.profiles.manage`; person exists | changed fields; `people.person.changed` with `Fields` |
| `Search` | people screen; the access grant form; the audit filter | `people.directory.read` in any extent; `q` within bounds | ≤ limit matches ordered by playa name; matches on legal name, phone or contact address only for holders of `people.contacts.read`, each such match recorded `people.search.read` |
| `AddContact`/`UpdateContact`/`RemoveContact` | profile and person page | self or `people.profiles.manage`; `EmergencyContacts != FieldHidden`; `Name` and `Phone` non-empty; count ≤ `MaxEmergencyContacts` | row changed; `people.contact.<verb>` in the transaction |
| `SetConsent` | profile screen (a module's section links to `/people/me#consents`; `Directory` has no setter, so a module can only read consent) | self, or `people.profiles.manage` (a paper form) | row upserted with `RecordedBy`; `people.consent.changed` with `Fields: [purpose]` and `Reason` `granted` or `withdrawn` (D37) |
| `LinkAccount` | person page "Link account" | `people.profiles.manage`; person has no account | account re-pointed through identity; `people.person.linked` (record = person, fields none); orphan handling per decision 13 |
| `Export` | profile "Download my data"; person page export; admin | self (cooldown) or `people.data.export`; `Link` done | one JSON document (shape under Behavior); `people.person.exported` (level sensitive) recorded before the body is written |
| `Delete` | person page delete; admin | `people.data.delete`; not self; `Link` done | everything gone in one transaction; `people.person.deleted`; `people.person-deleted` published |
| `RequestDeletion` | profile screen | an actor; no open request; note ≤ `MaxNoteBytes` | open request; `people.request.created`; `people.deletion-requested` published |
| `MyRequests` | profile screen | an actor | own requests, newest first (the requester's own read is not audited) |
| `WithdrawRequest` | profile screen | own and open | status withdrawn; `people.request.withdrawn` |
| `ListRequests` | requests screen | `people.data.delete` | requests by status (default open), oldest first, with the person's playa name resolved by the shell through `/names`; records one `people.request.read` (`Level: personal`, `Fields: [note]`) when any returned request has a note (D37) |
| `DeclineRequest` | requests screen | `people.data.delete`; open | status declined, note; `people.request.declined` |
| `FulfilRequest` | requests screen | `people.data.delete`; open; not self | `Delete` of the request's person; `Reason: "request:<id>"` |
| `ExportCommand`/`DeleteCommand` | `app` | writing copy for delete; `Link` done | as `Export`/`Delete` with `ActorOperator`, `Reason: "command"` |
| `Link` | `app` | once | sources and providers frozen |
| `Routes`/`OpenAPI` | `app` → `api` | — | the table below; a fragment that matches |

**Routes** (relative to `/api/people`, all signed-in):

| Method and pattern | Use case | Notes |
|---|---|---|
| `GET /me` | `Me` | the frame reads it once per session |
| `PUT /me` | `UpdateMe` | body `ProfileInput` |
| `POST /me/contacts`, `PUT /me/contacts/{contact}`, `DELETE /me/contacts/{contact}` | contacts (self) | |
| `PUT /me/consents/{purpose}` | `SetConsent` (self) | body `{granted}` |
| `GET /me/export` | `Export` (self) | `Content-Disposition: attachment; filename="bms-export-<person>.json"` |
| `GET /me/requests`, `POST /me/requests`, `POST /me/requests/{request}/withdraw` | requests (self) | body `{kind: "delete", note}` |
| `GET /search?q=&limit=` | `Search` | |
| `GET /names?ids=` | `NamesOf` | comma-separated, ≤ 200 |
| `POST /` | `Create` | |
| `GET /{person}`, `PUT /{person}` | `Get`, `Update` | own ID behaves as `/me` |
| `POST /{person}/contacts`, `PUT /{person}/contacts/{contact}`, `DELETE /{person}/contacts/{contact}` | contacts (manage) | |
| `PUT /{person}/consents/{purpose}` | `SetConsent` (manage) | |
| `POST /{person}/link-account` | `LinkAccount` | body `{email}` |
| `GET /{person}/export` | `Export` | attachment as above |
| `DELETE /{person}` | `Delete` | returns the report |
| `GET /requests?status=&person=` | `ListRequests` | |
| `POST /requests/{request}/decline` | `DeclineRequest` | body `{note}` |
| `POST /requests/{request}/fulfil` | `FulfilRequest` | returns the report |

Handlers are thin (`httpx.ReadJSON`, the service, `httpx.WriteJSON`, `httpx.Fail`); path IDs through `model.ParseID`; `GET` routes change nothing (`Me` on a missing row does not write).

## Types

Defined above; uses `model.PersonID`, `model.AccountID`, `model.Email`, `model.Sensitivity`, `authz.Permission`, `authz.PermissionDecl`, `authz.ActorFunc`, `audit.Entry`, `signals.Name`, `signals.Payload`.

Validation rules (every text field: trimmed, valid UTF-8, no control characters (`control_chars`), byte limit (`too_long {max}`)): `PlayaName` 1..100 (`required` on `UpdateMe` and `Create`); `Pronouns` 0..40; `Language` `""` or `^[a-z]{2,3}(-[A-Za-z0-9]{1,8})*$` ≤ 35 (`malformed`); `LegalName` 0..200; `Phone` 0..40 and `^[+0-9][0-9 ().\-]*(x[0-9]{1,6})?$` after trimming (`malformed`; loose on purpose, decision 5); `ContactEmail` `model.ParseEmail` (`malformed`), refused with `Invalid` code `has_account` when the person has an account; contact `Name` 1..200, `Phone` 1..40 (same shape), `Relationship` 0..60; `note` 0..500. A field whose mode is `FieldHidden` in the body fails `Invalid` code `hidden`.

## Data

Schema `core`, migration set `people` (`db.Set{Name: "people", Schema: "core"}`), files `0001_person.sql`, `0002_requests.sql`. Timestamps `timestamptz`. Retention: every row lives until the person is deleted (`Delete`); no time-based retention in M2 (decision 17, D30).

**`core.person`**

| Column | Type | Constraints | Level | Why collected |
|---|---|---|---|---|
| `id` | `uuid` | PK | — | the `PersonID` identity minted or people generated |
| `playa_name` | `text` | NOT NULL DEFAULT '', `octet_length <= 100` | internal | how people know each other |
| `pronouns` | `text` | NOT NULL DEFAULT '', `<= 40` | internal | address people correctly |
| `language` | `text` | NOT NULL DEFAULT '', `<= 35` | internal | the person's UI language (web-shell Proposed change 13) |
| `legal_name` | `text` | NOT NULL DEFAULT '', `<= 200` | personal | waivers and gate (setting `people.legal_name`) |
| `phone` | `text` | NOT NULL DEFAULT '', `<= 40` | personal | reaching a volunteer (setting `people.phone`) |
| `contact_email` | `text` | NULL, `octet_length between 3 and 254` | personal | reaching a person who has no account (identity decision 17); stored as entered, index on `lower(contact_email)` (not unique) |
| `created_by` | `uuid` | NULL, FK `core.person(id)` ON DELETE SET NULL | — | which profile manager created an account-less person |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | — | |

Index `person_playa_name_idx ON lower(playa_name) text_pattern_ops` (prefix matches; substring matches scan, acceptable at a regional's scale, revisit with traces). `has_account` is not a column: it is `EXISTS (SELECT 1 FROM core.account WHERE person_id = $1)`, answered by identity (`EmailOf` not `NotFound`), never by a join from people's SQL into identity's table.

**`core.person_emergency_contact`**

| Column | Type | Constraints | Level |
|---|---|---|---|
| `id` | `uuid` | PK | — |
| `person_id` | `uuid` | NOT NULL, FK `core.person(id)` ON DELETE CASCADE | — |
| `name` | `text` | NOT NULL, `1..200` | personal |
| `phone` | `text` | NOT NULL, `1..40` | personal |
| `relationship` | `text` | NOT NULL DEFAULT '', `<= 60` | personal |
| `position` | `integer` | NOT NULL, UNIQUE `(person_id, position)` deferrable | — |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | — |

The cascade is a safety net only; `Delete` removes contacts explicitly so the count is reported.

**`core.person_consent`**

| Column | Type | Constraints | Level |
|---|---|---|---|
| `person_id` | `uuid` | FK `core.person(id)` ON DELETE CASCADE | — |
| `purpose` | `text` | `<= 64`, grammar checked in Go | internal |
| `granted` | `boolean` | NOT NULL | internal |
| `recorded_by` | `uuid` | NULL, FK `core.person(id)` ON DELETE SET NULL | — |
| `changed_at` | `timestamptz` | NOT NULL | — |
| | | PK `(person_id, purpose)` | |

History of grants and revocations is the audit log (`people.consent.changed`, `Fields: [purpose]`, `Reason` `granted` or `withdrawn`; D37).

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

Partial unique index `data_request_open_idx ON (person_id) WHERE status = 'open'` (one open request per person; a race answers `Conflict duplicate` through `*db.Error.Constraint`). A fulfilled request disappears with its person; the trail is `people.request.fulfilled` and `people.person.deleted`.

**Backfill.** `0001_person.sql` ends with `INSERT INTO core.person (id, created_at, updated_at) SELECT person_id, created_at, now() FROM core.account ON CONFLICT (id) DO NOTHING`, so an M1 installation gets a row for every existing account (identity's set runs before people's in db's fixed order). It is the one place people's SQL touches identity's table, in a migration only, never in a query.

**Foreign keys from elsewhere** (proposed, not people's migrations): `core.role_grant.person_id → core.person(id)` (authz `0002`, ON DELETE RESTRICT: authz's own `DeletePerson` runs first), `core.role_grant.granted_by → core.person(id)` ON DELETE SET NULL, `hello.greeting.person_id → core.person(id)` (hello `0002`, RESTRICT). **No foreign key from `core.account`:** identity inserts the account before the person row exists (decision 2), so the constraint cannot hold; the backfill above and the repair path cover the gap, and decision 2 records the owner's choice of the signal plus repair over a synchronous alternative that would allow one. Module authors: foreign keys to `core.person(id)` are allowed (D10) and `ON DELETE RESTRICT` is the rule, because `people` calls every module's `DeletePerson` before it deletes the row and a cascade would hide a module that forgot something.

**Personal data never leaves these tables except:** to the person; to holders of the permissions below (audited); in `Export`. Nothing here goes to a log, span, metric, signal, error message or `Reason` (`Person.LogValue`, `Email.LogValue`; decision 16).

## Permissions

| Permission | Allows | Scoped by | Reaches | Starter roles (`StarterRoles`) |
|---|---|---|---|---|
| `people.directory.read` | `Search`; `Get` with internal fields; the person page; the `/people` menu entry | department-scopable; any extent reaches every person until a membership contract exists (decision 9) | internal | `account-admin`, `access-admin`, `audit-reviewer`, `department-steward`, `department-admin` |
| `people.contacts.read` | personal fields and emergency contacts of others in `Get`; personal-field matches in `Search` | department-scopable; any extent (decision 9) | personal | `account-admin` only in M2 (decision 9) |
| `people.profiles.manage` | `Create`, `Update` and contacts for others, `SetConsent` for others, `LinkAccount`; consents shown in `Get` | site | personal | `account-admin` |
| `people.data.export` | `Export` of any person | site | sensitive (the export spans every module) | `account-admin` (09 §11) |
| `people.data.delete` | `Delete`, `ListRequests`, `DeclineRequest`, `FulfilRequest`; the requests screen | site | sensitive | `account-admin` (09 §11) |

Own records need no permission: `Me`, `UpdateMe`, own contacts and consents, own `Export`, own requests, `NamesOf` (internal), all through `identity.PersonFrom`. The base role `participant` holds none of these by default (decision 20). Every list is filtered by `ExtentOf`: today the extent is "nowhere" or "everyone"; the department filter arrives with the membership contract and needs no permission change.

## Signals

**Published** (inside the transaction that makes the change; delivered after commit):

| Name | When | Payload |
|---|---|---|
| `people.person-created` | a person row is created (`Create`, the `identity.account-created` handler, the `UpdateMe` repair) | `{person: PersonID}` |
| `people.person-deleted` | `Delete` commits (published inside its transaction, after the modules' `DeletePerson`) | `{person: PersonID}` (the row is gone; subscribers clear caches, nothing more) |
| `people.deletion-requested` | `RequestDeletion` | `{request: RequestID, person: PersonID}` (notify, M4: tell `people.data.delete` holders; the body says only "a deletion request is waiting") |

**Subscribed:** `identity.account-created {account, person}` → `INSERT INTO core.person (id, ...) VALUES ($person, '', ...) ON CONFLICT (id) DO NOTHING`, publishing `people.person-created` only when a row was inserted; runs as the system (no actor; audit `ActorSystem`, `people.person.created`). Idempotent on the person ID, so a redelivery (`jobs`, M4) is harmless. **If it fails** (database down, read-only copy): signals logs and counts it; nothing retries in M2; the person's first `UpdateMe` creates the row (decision 2) and `Me`, `PlayaNameOf`, `NamesOf` answer empty until then. A module writing about that person before then gets `Invalid` from the foreign key; the shell's profile prompt makes that rare (decision 2).

## Settings

| Key | Type | Default | Valid range | Who can change it |
|---|---|---|---|---|
| `people.legal_name` | enum | `optional` | `hidden`, `optional`, `required` | Settings admin |
| `people.phone` | enum | `optional` | `hidden`, `optional`, `required` | Settings admin |
| `people.emergency_contacts` | enum | `optional` | `hidden`, `optional` (never `required`, Q-CORE7; `Policy.Validate` refuses it) | Settings admin |

Read through `PolicySource` on every use case that needs it (no cache; settings has its own). Until `settings` exists, `app` passes nil and `DefaultPolicy()` applies. Changing a mode to `hidden` hides and stops accepting the field; stored values stay (a later change back shows them again); a change to `required` makes existing profiles `Complete: false` until the person fills it in.

## UI (M2)

`web/src/core/people/` (web-shell decision 3), keys `core.people.*`, every screen phone-first (single column, 44 px targets, native dialogs), works on a read-only copy for reads with every write button disabled behind the shell's read-only banner. Navigation: main "My profile" (`/people/me`, signed-in), main "People" (`/people`, access `people.directory.read`), admin "Deletion requests" (`/people/admin/requests`, access `people.data.delete`).

- **My profile** (`/people/me`). Sections: *About you* (playa name, pronouns, language; legal name and phone when their mode is not `hidden`, marked required when `required`), *Login e-mail* (read-only, "change it from your account" until M4), *Emergency contacts* (cards; add, edit, remove through dialogs; hidden when the mode is `hidden`), *Consents* (each granted purpose with its owner's title and a revoke switch; empty: "You haven't been asked for any consent yet"), the modules' *sections* (title from `module.<m>.section.<ident>.title`; a `Failed` section shows "This part couldn't be loaded" with the reference), *Your data* ("Download my data" → `GET /me/export`, with the cooldown shown as a toast on 429; "Request deletion" → dialog with an optional note; an open request shows its date and a "Withdraw" button; a declined one shows the decision note). **First sign-in:** the frame sends a session whose `playa_name` is empty to `/people/me?first=1` once per session (not the superuser); the screen shows a banner "Tell us your playa name" and focuses the field; the person may navigate away. **Empty:** the new-profile state above. **Error:** `ErrorState` with the reference. **Read-only copy:** fields disabled, banner.
- **People** (`/people`). A search box (min 2 characters, hint shown), results as cards (playa name, pronouns) linking to the person page; "New person" button for `people.profiles.manage`. **Empty:** "No one matches". **Forbidden:** route hidden; a direct visit shows the shell's forbidden screen. **Error:** `ErrorState`.
- **New person** (`/people/new`, `people.profiles.manage`). Playa name (required), pronouns, legal name, phone, contact e-mail; on save goes to the person page.
- **Person page** (`/people/:person`). Internal fields; personal fields and contacts when `Can.ReadContacts` (a note says "Viewing these details is recorded"); edit buttons when `Can.Edit`; "Link account" dialog (e-mail field) when `Can.Link` and the person has no account, with the result ("account linked; the empty profile it came with was removed" or "...was kept"); "Roles" link to `/access/grants?person=<id>` when `Can.ViewRoles` (holders of `access.grants.read`, or self); "Audit log" link to `/audit?subject=<id>` when `Can.ViewAudit` (holders of `audit.search`; the "export views" button lives on that screen, audit UI); "Export data" (`Can.Export`) and "Delete person" (`Can.Delete`; a confirm dialog that repeats the playa name and lists what will happen; shows the report afterwards). The modules' sections as on My profile. Own ID redirects to `/people/me`. **Not found:** the shell's not-found state. **Forbidden:** as People.
- **Deletion requests** (`/people/admin/requests`, `people.data.delete`). Tabs open / declined / withdrawn; cards with playa name (through `/names`), date, note; "Fulfil" (confirm dialog, then the report) and "Decline" (dialog with a note). **Empty:** "No open requests". **Read-only copy:** decisions disabled.
- The participant's own "who viewed my data" is **Later** (D22, audit `ViewsOf`); this spec adds no screen for it. The admin's "who viewed whose contact details" (M2 milestone) is audit's screen, reached from the person page.

## Behavior

- **Actor and permissions.** Each use case takes the actor from `Options.Actor` (`Unauthenticated` when absent, restricted, or inside `signals.InDelivery`, except `NamesOf`/`ConsentOf`, which serve the system), decides self versus other, then asks authz: `Allow` for site permissions, `ExtentOf(p).IsZero()` for the any-extent ones. The superuser passes authz (D23) and is audited like anyone.
- **Visibility.** `Get` builds `Profile.Fields` from the viewer: internal always; personal when `people.contacts.read` (any extent) or self; consents when self or `people.profiles.manage`. Hidden-mode fields are absent even for the person. Personal fields the viewer may not see are zero in the JSON and absent from `Fields`.
- **Audit.** Changes record inside their `Write` transaction (`Record` ref kinds `people.person`, `people.contact`, `people.request`; `Subject` the person; `Fields` the names changed). Reads of personal fields about someone else record one `people.person.read` after loading and before returning, `Level: personal`, `Fields` the personal groups included (`legal_name`, `phone`, `contact_email`, `email`, `emergency_contacts`), outside any `Read` transaction (audit decision 12). `Export` records `people.person.exported` (`Level: sensitive`, `Fields: ["all"]`) before writing the body; a self export too (the export reaches sensitive data). `Search` records `people.search.read` per result that matched a personal field. `ListRequests` records `people.request.read` when a returned request carries a note, after loading and before returning, outside any `Read` transaction (D37). A `Record` error fails the use case (audit decision 3).
- **Export without a row (D37).** If there is no `core.person` row but `Accounts.EmailOf` finds an account for the ID, `Export` still calls identity, authz and each module and returns a document with `person` absent; `NotFound` only when no account exists either.
- **Transactions.** `UpdateMe`, `Create`, `Update`, contacts, consents, requests: one `Write` transaction each, publishing inside it. `Get`, `Search`, `Me`: plain reads, no transaction. `Export`: no transaction; identity, authz and each module in build order are called in turn; a failing source fails the export (a partial export would be mistaken for complete).
- **Deletion, step by step** (`Delete`, `FulfilRequest`, `DeleteCommand`): take a `pg_advisory_xact_lock` on `hashtext('people.delete:' || person)` so two deletions of one person serialise; load the person (`NotFound`, unless the person has no row but `Accounts.EmailOf` finds an account for the ID: then the module, authz and identity steps below still run, people's own deletes are no-ops and no `people.person-deleted` is published, D37); for each `Source` in build order call `DeletePerson` with the transaction's ctx (nested `Transact` joins, db decision 6), wrapping an error with the module name; `Access.DeletePerson` (`Conflict last_access_admin` ends it); `Accounts.DeletePerson` (`Permission` for a superuser account ends it); delete requests, consents, contacts, the person row (a foreign-key violation from a module that forgot a table becomes `Conflict in_use` naming the constraint); `AuditCount.CountAbout` for `Kept`; record `people.person.deleted` (`Reason` token, `Record` ref the person) and `people.request.fulfilled` when applicable; publish `people.person-deleted`; commit. The report is returned to the caller and never logged (counts only on the span). Idempotency: a second call answers `NotFound`.
- **Linking, step by step.** Transaction 1: load person (`NotFound`), `Conflict has_account` if `EmailOf` succeeds; `Accounts.LinkAccount(email, person)` moves the account's `person_id` (identity records `identity.account.linked`) and returns the account ID and the orphan (the person ID the account pointed at before); clear the person's `contact_email` (the account's address replaces it; the audit entry's `Fields` lists `contact_email`, D37); record `people.person.linked`; commit. Transaction 2 (best effort, logged on failure): delete the orphan row only if its `playa_name`, `legal_name`, `phone`, `pronouns` are empty and it has no contacts, consents or requests, and the delete does not hit a foreign key; on success publish `people.person-deleted` for the orphan and set `OrphanDeleted`. The session of the linked account keeps working: identity re-reads the account row on every request (identity decision 2), so the next request carries the new person ID; the shell refreshes the session on the next load.
- **Sections.** `Get` and `Me` call every `SectionProvider.Sections(ctx, [person])` sequentially under `SectionTimeout` each, in build order, on a ctx derived from the request's (the provider sees the viewer); an error or timeout yields `Section{Key: "<module>.unavailable", Failed: true}`, a warn log line (module name, kind, no IDs beyond the person's) and `bms.people.sections{module, outcome=failed}`. Sections never appear in `Search` or `NamesOf`.
- **Read-only copy.** `Me`, `Get`, `Search`, `NamesOf`, `ConsentOf`, `PlayaNameOf`, `Export`, `MyRequests`, `ListRequests` work (audit entries spool). Every write fails `Unavailable` wrapping `db.ErrReadOnly` (`503 read_only`); the account-created handler's insert fails the same way and is counted as `read_only` by signals.
- **Cooldown.** Self exports: an in-memory map person → last export time, bounded by `MaxTrackedExports` (oldest evicted), per process; within `ExportCooldown` the call fails `Unavailable` wrapping `httpx.ErrRateLimited` (429, `Retry-After`). Admin exports and the command are not limited.
- **Concurrency.** The service holds no mutable state beyond the cooldown map (mutex) and the `Link` slices (set once). Profile updates are last-write-wins (a profile is edited by one person at a time in practice; an `If-Match` precondition can be added without a migration).
- **Lifecycle.** `New` subscribes; `Link` once; nothing to start or stop; no goroutines.
- **Telemetry.** A span per use case (`people.<name>`) with `telemetry.ID("person_id")` and `telemetry.Token` outcomes; `people.section` per provider with the module token; metrics above; never a name, address or phone on any of them (`telemetrytest` asserts).

**Export document** (`ExportFormat` 1), one JSON object:

```json
{
  "format": 1,
  "exported_at": "...",
  "person": { "id": "...", "playa_name": "...", "pronouns": "...", "language": "...", "legal_name": "...", "phone": "...", "contact_email": "...", "created_at": "...", "updated_at": "..." },
  "emergency_contacts": [ { "name": "...", "phone": "...", "relationship": "...", "position": 1 } ],
  "consents": [ { "purpose": "...", "granted": true, "changed_at": "..." } ],
  "requests": [ { "kind": "delete", "status": "declined", "note": "...", "created_at": "...", "decision_note": "..." } ],
  "identity": { "account": { } },
  "access": { },
  "modules": { "<name>": { } }
}
```

`identity` and `access` are the raw messages those services return; `modules` holds each module's `Export.Data` by name (an empty object for a module holding nothing). Audit rows are not included (audit "Export of a person").

**Deletion report** (JSON and the command's lines): `{"person": "...", "people": n, "identity": n, "access": n, "modules": {"<name>": {"deleted": n, "anonymized": n, "kept": [...]}}, "kept": [{"kind": "audit.entry", "count": n}]}`.

**Delivery by milestone.** M2: everything above, including the commands (09 §11 moves "basic" export and deletion here; decision 19). M4: `ChangeEmail` on the profile (identity), notify on `people.deletion-requested`, retries of the account-created handler through `jobs`, the `people.retention.unused` setting and job. Later: `ViewsOf` on the profile (D22), merging two people, a department-scoped extent for contacts (volunteers' membership contract), an export request kind.

### Accepted implementation deviations (M2 build, 2026-10-18)

- **Leaf package.** `people/rows`, aliased as `ContactID` and `RequestID`.
- **Extra exports.** `PublishedSignals`, `SubscribedSignals`, `ParseRequestStatus`, `RequestStatus.String` and the `Field` constants. `personpage.SectionTimeout` is also exported.
- **Profile updates.** `Update` on your own ID delegates to `UpdateMe`. `Create` and `Update` return a profile built like `Get`'s, with a read audit when the viewer holds `people.contacts.read`. The section timeout is a real context deadline.
- **Search.** Search by contact address is an exact, case-insensitive match. The store passes no paging cursors (first page, limit 200).
- **Concurrency.** `UpdateMe`'s repair takes the deletion lock and re-checks the account before validating, so a deleted person always gets `Unauthenticated`. `LinkAccount` takes the deletion lock first. `Delete` loads the person with `GetForUpdate`.
- **Orphans on link.** The cleanup runs outside the transaction and records `people.person.deleted` with the system actor and reason `link`.
- **Export.** The cooldown is kept on `Canceled` and once the audit entry is written.
- **Commands.** The `DeleteCommand` report has one line per part. `export` and `delete` exit 2 only for a malformed person ID, and 1 otherwise.
- **SQL.** The `(lower(playa_name) COLLATE "C", id)` index; the partial contact-email index; the position key is `DEFERRABLE INITIALLY IMMEDIATE`; `delete_orphan_person` is stricter; `data_request_decided_chk`.

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `New` | a nil dependency | Invalid (`people.<dep>`) | app exits 2 | `TestNew_NilDependency_ReturnsInvalid` |
| F2 | `Link` | called twice, or a nil provider or exporter | Internal | app exits 1 | `TestLink_Twice_ReturnsInternal` |
| F3 | every use case | no actor, restricted session, or `InDelivery` (except `NamesOf`, `ConsentOf`) | Unauthenticated | 401 | `TestUseCases_NoActor_ReturnsUnauthenticated` |
| F4 | `Me` | no row yet | nil (empty, incomplete profile) | show the first-sign-in state | `TestMe_NoRow_ReturnsEmptyProfileWithoutWrite` |
| F5 | `UpdateMe`, `Create` | playa name empty after trim | Invalid (`playa_name` `required`) | field error | `TestUpdateMe_EmptyPlayaName_ReturnsInvalid` |
| F6 | profile inputs | too long, control characters, bad e-mail, bad language, bad phone shape | Invalid (`too_long {max}`, `control_chars`, `malformed`) | field errors | `TestProfileInput_Validation_Table` |
| F7 | `UpdateMe` | a `FieldRequired` field empty after the update | Invalid (`<field>` `required`) | field error | `TestUpdateMe_RequiredLegalName_ReturnsInvalid` |
| F8 | `UpdateMe`, `Update`, `Create` | a `FieldHidden` field present in the body | Invalid (`<field>` `hidden`) | field error | `TestUpdateMe_HiddenPhone_ReturnsInvalid` |
| F9 | `Update`, `Create` with `ContactEmail` | person has an account | Invalid (`contact_email` `has_account`) | field error | `TestUpdate_ContactEmailWithAccount_ReturnsInvalid` |
| F10 | `Get`, `Search` | `people.directory.read` held nowhere | Permission | forbidden state | `TestGet_NoDirectoryRead_ReturnsPermission` |
| F11 | `Get`, `Update`, contacts, `LinkAccount`, `Export`, `Delete` | unknown person | NotFound | not-found state | `TestGet_UnknownPerson_ReturnsNotFound` |
| F12 | `Get` | viewer without `people.contacts.read` | nil; personal fields and contacts omitted; no audit entry | render what came | `TestGet_NoContactsRead_OmitsPersonalFieldsAndDoesNotAudit` |
| F13 | `Get` | viewer with `people.contacts.read`, `Record` fails | the recorder's kind (Unavailable) | error state | `TestGet_AuditFails_FailsRead` |
| F14 | `Create`, `Update`, others' contacts and consents, `LinkAccount` | no `people.profiles.manage` | Permission | hidden buttons; 403 | `TestUpdate_NoManage_ReturnsPermission` |
| F15 | `Search` | `q` shorter than `MinSearchBytes` or longer than max; `limit` out of range | Invalid (`q` `too_short {min}`/`too_long {max}`, `limit` `range`) | hint | `TestSearch_ShortQuery_ReturnsInvalid` |
| F16 | `Search` | personal-field match without `people.contacts.read` | not matched (no error, no leak) | — | `TestSearch_LegalNameWithoutContactsRead_NoMatch` |
| F17 | `NamesOf` | more than `MaxNames` IDs | Invalid (`ids` `too_many {max}`) | 400 | `TestNamesOf_TooMany_ReturnsInvalid` |
| F18 | `NamesOf` | unknown or deleted IDs | nil; those IDs omitted | show a placeholder | `TestNamesOf_UnknownIDs_Omitted` |
| F19 | `AddContact` | count already `MaxEmergencyContacts` | Invalid (`contacts` `too_many {max}`) | disable add | `TestAddContact_AtMax_ReturnsInvalid` |
| F20 | `AddContact`, `UpdateContact` | name or phone empty | Invalid (`required`) | field errors | `TestAddContact_EmptyName_ReturnsInvalid` |
| F21 | contacts | mode `hidden` | Invalid (`contacts` `hidden`) | section hidden | `TestAddContact_Hidden_ReturnsInvalid` |
| F22 | `UpdateContact`, `RemoveContact` | contact not the person's | NotFound | not-found | `TestUpdateContact_OtherPersons_ReturnsNotFound` |
| F23 | `SetConsent`, `ConsentOf` | bad purpose | Invalid (`purpose` `malformed`) | 400 | `TestSetConsent_BadPurpose_ReturnsInvalid` |
| F24 | `LinkAccount` | no account with that address | NotFound (`email` `unknown`) | field error, nothing else said | `TestLinkAccount_UnknownEmail_ReturnsNotFound` |
| F25 | `LinkAccount` | person already has an account | Conflict (`has_account`) | message | `TestLinkAccount_HasAccount_ReturnsConflict` |
| F26 | `LinkAccount` | the address is a superuser account | Conflict (`superuser`) | message | `TestLinkAccount_Superuser_ReturnsConflict` |
| F27 | `LinkAccount` | orphan not empty or referenced | nil; `OrphanDeleted: false` | tell the admin | `TestLinkAccount_OrphanWithData_Kept` |
| F28 | `Export` (self) | within `ExportCooldown` | Unavailable wrapping `httpx.ErrRateLimited` | 429 toast | `TestExport_SelfWithinCooldown_ReturnsRateLimited` |
| F29 | `Export` (other) | no `people.data.export` | Permission | hidden button | `TestExport_NoPermission_ReturnsPermission` |
| F30 | `Export` | a source fails | the source's kind, wrapped with its name | error state; nothing written | `TestExport_ModuleFails_FailsWhole` |
| F31 | `Export`, `Delete` | before `Link` | Internal | 500 | `TestExport_BeforeLink_ReturnsInternal` |
| F32 | `Delete`, `FulfilRequest` | no `people.data.delete` | Permission | hidden | `TestDelete_NoPermission_ReturnsPermission` |
| F33 | `Delete`, `FulfilRequest` | the person is the actor | Invalid (`person` `self`) | message | `TestDelete_Self_ReturnsInvalid` |
| F34 | `Delete` | authz refuses (last access admin) | Conflict (`last_access_admin`) | message; nothing deleted | `TestDelete_LastAccessAdmin_RollsBack` |
| F35 | `Delete` | identity refuses (superuser account) | Permission | message; nothing deleted | `TestDelete_SuperuserPerson_RollsBack` |
| F36 | `Delete` | a module's `DeletePerson` fails | the module's kind, wrapped with its name | message; nothing deleted | `TestDelete_ModuleFails_RollsBack` |
| F37 | `Delete` | a foreign key still references the person | Conflict (`in_use` + constraint) | message naming the constraint | `TestDelete_ForeignKeyLeft_ReturnsConflict` (integration) |
| F38 | `Delete` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | 503 read_only | `TestDelete_ReadOnly_ReturnsUnavailable` |
| F39 | `Delete` | `Record` fails | the recorder's kind; transaction rolled back | error | `TestDelete_AuditFails_RollsBack` |
| F40 | `RequestDeletion` | an open request exists | Conflict (`duplicate`) | show the open one | `TestRequestDeletion_Open_ReturnsConflict` |
| F41 | `RequestDeletion`, `DeclineRequest` | note too long | Invalid (`note` `too_long {max}`) | field error | `TestRequestDeletion_LongNote_ReturnsInvalid` |
| F42 | `WithdrawRequest` | not own, or not open | NotFound / Conflict (`status`) | refresh | `TestWithdrawRequest_NotOpen_ReturnsConflict` |
| F43 | `DeclineRequest`, `FulfilRequest` | not open | Conflict (`status`) | refresh | `TestDeclineRequest_NotOpen_ReturnsConflict` |
| F44 | `ListRequests`, `DeclineRequest` | no `people.data.delete` | Permission | hidden | `TestListRequests_NoPermission_ReturnsPermission` |
| F45 | account-created handler | row exists | nil; nothing published | — | `TestHandleAccountCreated_Twice_InsertsOnce` |
| F46 | account-created handler | read-only copy | Unavailable wrapping `db.ErrReadOnly` (signals counts `read_only`) | — | `TestHandleAccountCreated_ReadOnly_ReturnsUnavailable` |
| F47 | account-created handler | payload without `person` | Invalid | signals logs it | `TestHandleAccountCreated_BadPayload_ReturnsInvalid` |
| F48 | `Get`/`Me` sections | a provider fails or exceeds `SectionTimeout` | nil; `Section{Failed: true}` | show the failed card | `TestGet_ProviderFails_SectionMarkedFailed` |
| F49 | `PlayaNameOf` | no row | NotFound | identity shows "" | `TestPlayaNameOf_NoRow_ReturnsNotFound` |
| F50 | `ExportCommand`, `DeleteCommand` | malformed ID | Invalid | app exits 2 | `TestDeleteCommand_BadID_ReturnsInvalid` |
| F51 | `Policy.Validate` | `EmergencyContacts == FieldRequired` | Invalid (`people.emergency_contacts` `one_of`) | settings refuses | `TestPolicy_RequiredContacts_ReturnsInvalid` |
| F52 | every store method | database down, deadline | Unavailable, Timeout, Canceled (`db.Wrap`) | 503/504 | `TestStore_Unavailable_Propagates` |
| F53 | handlers | any error | `httpx.Fail` problem body | shell states | `TestHandlers_Errors_MapToProblem` |
| F54 | `UpdateMe` | body sets only `language` and a `FieldRequired` field is empty | nil; language saved (D37) | none | `TestUpdateMe_LanguageOnly_SkipsRequiredCheck` |
| F55 | `SetConsent` | granted or withdrawn | nil; audit `Reason` is `granted`/`withdrawn`, `Fields` `[purpose]` (D37) | none | `TestSetConsent_RecordsPurposeAndDirection` |
| F56 | `LinkAccount` | person had a `contact_email` | nil; `contact_email` cleared (D37) | none | `TestLinkAccount_ClearsContactEmail` |
| F57 | `ListRequests` | a returned request has a note | nil; `people.request.read` recorded; `Record` error fails the read (D37) | none | `TestListRequests_WithNote_RecordsRead` |
| F58 | `Export`, `Delete` | no person row, an account exists | nil; identity, authz and module steps run (D37) | none | `TestDelete_AccountWithoutPersonRow_RunsIdentityAndAuthz` |
| F59 | `Delete` | the person held grants | nil; authz publishes `access.grant-revoked` per grant (D37) | none | `TestDelete_PublishesGrantRevoked` |

`MyRequests`, `ListRequests` (beyond permission), `ConsentOf` (beyond F23), `Routes`, `OpenAPI` have no further modes: fixed reads or pure. `Purpose.Owner`, `FieldMode.String`, `Person.LogValue` cannot fail (pure).

## Lock-in check

No external technology beyond PostgreSQL through `db` (D8). `text_pattern_ops`, partial unique indexes and `pg_advisory_xact_lock` are in every OSI-licensed PostgreSQL ≥ 16 (db decision 20); no `pg_trgm` or other extension is required. No cloud directory, no identity provider.

## Test plan

- **Unit** (`internal/core/people`, no infrastructure): a hand-written fake store; `clock.NewFake`; `audittest.NewFake("people")`; `signalstest.NewFake("people", {person-created, person-deleted, deletion-requested}, {identity.account-created})` with `Deliver` driving the handler; `authztest.NewFake()` with `Grant`/`GrantIn`/`Superuser`; fakes for `Accounts`, `Access`, `AuditCounter`, `PolicySource`, `PersonData` sources and `personpage.Provider` (one that fails, one that blocks until its ctx ends, so the timeout path runs without real time); `telemetrytest.New(t)` asserting no name, address or phone appears in logs, spans or the recorder's `Reason`/`Fields`. Every F row; the visibility matrix (self / directory.read / contacts.read / manage / superuser × each field group); the policy matrix (hidden / optional / required × own save / admin save).
- **Permission tests:** each use case has `_NoPermission_ReturnsPermission` (or `_NotFound` where the record must stay invisible); `Search` proves no personal-field match leaks without `people.contacts.read`.
- **Integration** (`//go:build integration`, `dbtest.TempDB`): migrations apply on an empty database; the deletion transaction across a fake module table with a foreign key (F37, F34 rollback leaves every row); the open-request partial index under two concurrent requests; the advisory lock under two concurrent deletions (one `NotFound`); keyset-free search ordering; read-only paths with `SetReadOnly`.
- **Conformance:** `peopletest.Fake` answers the same `NamesOf`/`ConsentOf` cases as the service (`TestFake_MatchesService`); `moduletest.Conformance` supplies it as `Deps.People` (Proposed change 2).
- **app end-to-end** (Proposed change 1): sign up → `GET /api/people/me` is empty and incomplete → `PUT /me` with a playa name → `GET /api/accounts/session` carries it; the account admin creates a person, links a second sign-up's account to them, the orphan is removed; `bms export <id>` prints the document with `hello` inside; the admin deletes a person who saved a hello greeting and the report counts it; `bms delete` on the last access admin's person exits 1 with `last_access_admin`; the profile screen and the person page render at the phone viewport.
- **Web** (`web/src/core/people/`): component tests for every screen's loading, empty, error, forbidden and read-only states and the first-sign-in banner; axe checks; the Playwright smoke run adds "fill in the playa name, see it in the header" and "search a person, open their page" at the phone viewport.

## Open questions

None. The seven questions were answered by the owner on 2026-10-18 (all recommendations accepted) and are folded into the Key decisions: Q1 (export, deletion and the two commands in M2) in decision 19; Q2 (`people.contacts.read` to `account-admin` only) in decision 9; Q3 (signal plus repair) in decision 2; Q4 (no `people.directory.read` in the base role) in decision 20; Q5 (immediate, audited self export with a 10-minute cooldown) in decision 10; Q6 (keep consents and `ConsentOf`) in decision 8; Q7 (one legal-name field) in decision 5.

**Raised by the M2 build, answered 2026-10-18 (D37), each as its recommendation.** (1) A language-only `PUT /me` is exempt from the required-fields check (decision 21). (2) The consent audit records purpose and direction (decision 22). (3) `contact_email` is cleared on link (decision 23). (4) Reads of deletion-request notes are audited (decision 24). (5) Data rights for an account holder with no `core.person` row still run the identity and authz steps (decision 25).

## Proposed changes elsewhere

Status as of 2026-10-18: items 1 to 14 are applied; items 15 (settings), 16 (notify), 17 (jobs) and 18 (`docs/operating.md`) wait for their targets.

1. (applied 2026-10-18, in app.md, row 5 of "How later core services plug in") **`plan/design/app.md` (row 18) and `internal/app`:** part 5 `people` in `core_people.go`: `declare` appends `people.Set` to `coreSets` (set name `people`, already in db's fixed order), `people.Permissions` to `corePerms`, the three signal names to `coreSignals`, `module.ExtensionPointDecl{Name: personpage.Point, Accepts: func(v any) bool { _, ok := v.(personpage.Provider); return ok }}` to `corePoints`, and the commands `export` (`usage: <person-id>`) and `delete` (`usage: <person-id>`), both requiring the session key like `create-superuser` and running `ExportCommand`/`DeleteCommand` after `Build` and `Link`; `build` calls `people.New` over `a.dbc`, the clock, telemetry scope `people`, `a.deps.audit.Scoped("people")`, `a.disp.Scope("people", {the three}, {identity.SignalAccountCreated})`, `a.deps.authz`, the authz actor closure, `Accounts: a.identity`, `Access: a.authz`, `AuditCount: a.audit`, `Policy: nil` (settings' part sets a lazy adapter when it lands), and sets `a.deps.people = svc` and route set `people`; identity's `build` passes `Names: lazyNames{a}` (empty until people's part sets `a.names`); `link` converts `a.set.PersonData()` to `[]people.Source` and `a.set.Contributors(personpage.Point)` to `[]people.SectionProvider` and calls `Link`. Mark Proposed change 10 applied for people. The e2e test adds the steps in "Test plan".
2. (applied 2026-10-18, in module-contract.md) **`plan/design/module-contract.md` (row 4), `internal/core/module`, `moduletest`:** add `Deps.People people.Directory` (M2; unscoped; `Provision` sets it); `moduletest.Conformance` sets `Deps.People = peopletest.NewFake()` when nil; mark Proposed change 8 applied (`Env.CoreExtensionPoints` carries `people.person-page`; `Env.CoreSignals` the three names; `Set.Contributors` consumed by people through `app`); add the rule for module authors: "a foreign key to `core.person(id)` is allowed and uses `ON DELETE RESTRICT`; `people` calls your `DeletePerson` first".
3. (applied 2026-10-18, in identity.md, which gains `EmailOf`, `LinkAccount`, the superuser refusal in `DeletePerson` and the profile link) **`plan/design/identity.md` (row 6) and `internal/core/identity`:** add `EmailOf(ctx, model.PersonID) (model.Email, error)` (`NotFound` without an account; no permission check, documented as people's seam) and `LinkAccount(ctx, email model.Email, person model.PersonID) (model.AccountID, model.PersonID, error)` returning the account and its previous person (`NotFound` field `email` code `unknown`; `Conflict superuser`; `Conflict has_account` when `person` already has an account; audited `identity.account.linked` with `Record` the account, `Subject` the person; no `account-linked` signal); `DeletePerson` fails `Permission` for a superuser account (decision 7's "never changed through the API"); no foreign key from `core.account.person_id` to `core.person` (identity creates the account first; people's `0001` backfills a person row per existing account, the one migration-time read of `core.account` by another set); mark Proposed change 7 applied (the subscription, `NameSource`, the prompt, the login address, export and deletion through `app`'s adapter, `LinkAccount` as the "link an existing person" method); the accounts admin screen (`/accounts/admin/:account`) shows a "Profile" link to `/people/<person>`.
4. (applied 2026-10-18, in authz.md) **`plan/design/authz.md` (row 7):** mark Proposed change 9 applied: (a) `ExtentOf` any-extent checks, (b) `Access` seam and the `Conflict last_access_admin` rollback, (c) `GET /api/people/search` behind `people.directory.read` and `GET /api/people/names` for any signed-in person, (d) the "Roles" link, (e) `StarterRoles` as in the Permissions table (contacts in `account-admin` only, decision 9), (f) authz migration `0002`: `core.role_grant.person_id → core.person(id)` ON DELETE RESTRICT and `granted_by → core.person(id)` ON DELETE SET NULL, (g) `NameSource` unchanged; decision 14's "authz does not verify that a person exists" becomes "the foreign key does, from `0002`"; `authz.Permissions` is unchanged.
5. (applied 2026-10-18, in audit.md) **`plan/design/audit.md` (row 12):** mark Proposed change 5 applied: the actions and `Fields` listed here; `people.person.exported` at level `sensitive`; `CountAbout` is consumed in M2 (not M4); the person page links to `/audit?subject=<person>` for `audit.search` holders instead of calling `GET /api/audit/people/{person}/views` from people's folder (web isolation; the export button stays on the audit screen); the audit screen's subject filter accepts `?subject=` from the URL.
6. (applied 2026-10-18, in signals.md) **`plan/design/signals.md` (row 10):** mark Proposed change 8 applied for people: `Env.CoreSignals` gains `people.person-created {person}`, `people.person-deleted {person}`, `people.deletion-requested {request, person}`; the core's scope `people` subscribes to `identity.account-created`.
7. (applied 2026-10-18, in web-shell.md) **`plan/design/web-shell.md` (row 17) and `web/src/shell`:** mark Proposed change 13 applied: after a session is established the frame calls `GET /api/people/me` once, applies `language` (mirrored to `localStorage["bms.lang"]`), and sends a session with an empty `playa_name` (not the superuser) to `/people/me?first=1` once per session; a language change by a signed-in person is written with `PUT /api/people/me {language}`; decision 3 lists `web/src/core/people/` as a core area; the smoke run adds the two flows in "Test plan"; `@/api` exports a `useMe()` hook limited to `core/people` and the frame.
8. (applied 2026-10-18, in module-hello.md) **`plan/design/module-hello.md` (row 4b) and `internal/modules/hello`:** migration `0002` adds `FOREIGN KEY (person_id) REFERENCES core.person(id)` ON DELETE RESTRICT (decision 9's deferral ends); `Manifest.Subscribes` may add `people.person-deleted` to reset the counter (optional; the deferred subscription); the e2e export shows `hello` inside the document.
9. (applied 2026-10-18, in model.md) **`plan/design/model.md` (row 2):** mark Proposed change 5 applied for people: `contact_email` stored as entered, searched by `Canonical()`, not unique (two account-less people may share a household address).
10. (applied 2026-10-18, in db.md) **`plan/design/db.md` (row 5):** mark Proposed change 10 applied for people (`postgres.Migrations`, `people.Set`, schema `core`, set name `people`); note the migration-time backfill from `core.account` (Data, "Backfill") as the one read across sets.
11. (applied 2026-10-18, in 07-abstractions.md) **`plan/07-abstractions.md`, "people":** Owns becomes "profiles (playa name, pronouns, language; legal name, phone, a contact address for people without an account), emergency contacts, consents by purpose, the person row behind every account (`identity.account-created`) and account-less people with `LinkAccount`, name lookup and search, the person page with `people.person-page`, export and deletion across identity, authz and every module (`PersonData`) with deletion requests, the `export` and `delete` commands, the `people` routes and screens, signals `people.person-created|deleted|deletion-requested`"; Doesn't do adds "the login address (`identity`), who may see a profile (`authz`), the person's own view of who read their data (`audit`, Later), department membership (`volunteers`)". Add the contract `contracts/personpage`.
12. (applied 2026-10-18, in 09-core-features.md) **`plan/09-core-features.md`:** §3 "plus the person-page extension point in `contracts/people`" becomes "`contracts/personpage`"; the milestone table's M2 row gains "download your own data, request deletion, export or delete one person (screen or command)", the M4 row drops "export or delete one person by command", and §11's "Later" keeps only retention and the export request kind.
13. (applied 2026-10-18, in telemetry.md) **`plan/design/telemetry.md` (row 15b):** add `bms.people.exports{outcome}`, `bms.people.deletions{outcome}`, `bms.people.sections{module,outcome}` to the instrument table.
14. (applied 2026-10-18, in the go-conventions and add-module skills) **`.claude/skills/go-conventions/SKILL.md` and `add-module/SKILL.md`:** `Deps.People` (`NamesOf`, `ConsentOf`; `peopletest.NewFake()` in tests); how to contribute a person-page section (`ContributesTo: personpage.Point`, a `personpage.Provider` in `Contributions()`, catalogue keys `module.<name>.section.<ident>.title`, the provider checks the viewer's permission and audits); foreign keys to `core.person` with `ON DELETE RESTRICT`; `DeletePerson` runs inside people's transaction (use the ctx you were given, never a new `Transact(Write)` on another ctx).
15. (pending: target not written) **`plan/design/settings.md` (row 15, unwritten):** declare `people.legal_name`, `people.phone`, `people.emergency_contacts` with the ranges above and implement `people.PolicySource` (`Policy.Validate` is the check); `app` sets the lazy adapter.
16. (pending: target not written) **`plan/design/notify.md` (row 11, unwritten):** subscribe to `people.deletion-requested` and tell holders of `people.data.delete` that a request is waiting (no note, no name in the body; a link to `/people/admin/requests`).
17. (pending: target not written) **`plan/design/jobs.md` (row 14, unwritten):** redeliver a failed `identity.account-created` handling to people (retryable kinds); carry the `people.retention.unused` proposal (delete account-less people with no module data after a regional-set period; default forever).
18. (pending: target not written) **`docs/operating.md` (unwritten):** `bms export <person-id>` prints one JSON document to stdout (handle it as personal data); `bms delete <person-id>` deletes across every module and prints the report, refuses the last access admin and a superuser's person, must run on the writing copy, and is audited as the operator; backups still hold the deleted data (privacy-data "Backups contain everything").
