# people

- **Status:** Implemented (2026-10-18; Amendment P1 (D48) and D51 implemented 2026-10-23); Approved (2026-10-18; owner approved every recommended answer)
- **Package:** `internal/core/people` (contract and service), `internal/core/people/postgres`
  (store, SQL, migrations), `internal/core/people/peopletest` (the fake `Directory` modules
  test with), `internal/contracts/personpage` (the person-page extension point's shape), `internal/contracts/membership` (the membership extension point's shape, D44).
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
  (`ScreenSet`, `NavEntry.access`), `app.md` (part 5) (Implemented). `settings.md` (`Decl`) and `notify.md` (`Notifier`) (D39); `authz.md` (`HoldersOf`, D39).
- **Used by:** every module (`Deps.People`, the `people.person-page` point, the
  `people.person-deleted` signal), `identity` (`NameSource`), `authz` and `audit` (the
  `search` and `names` routes behind their screens), the web shell (the profile prompt and
  the saved language), `app` (part 5, the `export` and `delete` commands), `notify` (`PlayaNameOf`, `LanguageOf`, `ContactEmailOf`, and its outbox as a `Source`), `settings` (`PolicySource`, `Settings`).

## Summary

**Purpose.** Hold who each participant is: their profile (playa name first), their contact details and emergency contacts, the consents they gave, and the one place that exports or deletes everything bms holds about them.

**Imports.** Package `internal/core/people` imports `model`, `lib`, `core/db`, `core/audit`, `core/authz` (the `Authorizer` and leaf types), `core/identity` (`PersonFrom`, `SessionFrom`, taken as `Options.Actor`), `core/signals`, `core/telemetry`, `contracts/personpage`. Never `core/module`, `api`, `app`, `config` or a module: the modules' `PersonData` and the page contributions reach it through `app`'s adapters (`Link`).

**Names.** `people` is the package, the core part, the migration set, the route set (`/api/people/`), the permission prefix, the audit area, the signal prefix, the settings prefix and the screen set. The extension point's shape lives in `contracts/personpage` so a module never imports two packages called `people`.

**Exported (`people`).**
- `ContactID`, `RequestID` (= `model.ID[...]`); `Purpose` (a consent purpose, `<owner>.<ident>`, +`Validate`, `Owner`); `FieldMode` (`FieldHidden`, `FieldOptional`, `FieldRequired`; zero invalid; `ParseFieldMode`, `String`); `RequestStatus` (`RequestOpen`, `RequestWithdrawn`, `RequestDeclined`).
- `Person{ID, PlayaName, Pronouns, Language, LegalName, Phone, ContactEmail, Email model.Email, HasAccount, Complete bool, CreatedAt, UpdatedAt}` (+`Validate`, `LogValue` = ID only); `EmergencyContact{ID, Person, Name, Phone, Relationship, Position int, CreatedAt, UpdatedAt}`; `Consent{Person,
  Purpose, Granted bool, RecordedBy, ChangedAt}`; `Request{ID, Person, Kind, Status, Note, CreatedAt, DecidedAt, DecidedBy, DecisionNote}`; `Profile{Person, Fields []string, Contacts, Consents, Sections []personpage.Section, Can Capabilities, Policy, FirstPrompt bool}`; `Capabilities{Edit,
  ReadContacts, Export, Delete, Link, ViewRoles, ViewAudit bool}`; `Match{ID, PlayaName, Pronouns}`; `ProfileInput{PlayaName, Pronouns, Language, LegalName, Phone, ContactEmail *string}` (nil = unchanged); `ContactInput{Name, Phone, Relationship}`; `SearchQuery{Q string, Limit int}`;
  `RequestQuery{Status, Person}`; `LinkResult{Account model.AccountID, Orphan model.PersonID, OrphanDeleted bool}`; `ExportFormat = 1`; `DeletionReport{Person, People, Identity, Access int, Modules map[string]ModuleDeletion, Kept []Kept}`, `ModuleDeletion{Deleted, Anonymized int, Kept []Kept}`,
  `Kept{Kind string, Count int}`, `Export{Data json.RawMessage}` (structural twins of `module`'s; `app` converts).
- `Policy{LegalName, Phone, EmergencyContacts FieldMode}` (+`Validate`), `DefaultPolicy()` (optional, optional, optional); `PolicySource{Policy(ctx) (Policy, error)}` (settings, M2 [nil = `DefaultPolicy`]; `app` implements it over a scoped `Reader`, D39); `Settings []settings.Decl` (D39: the three enum keys; `emergency_contacts` lists `hidden|optional` only, so `Policy.Validate`'s rule is the declaration); `Templates fs.FS` (`people.deletion-requested`; `en`, `es`; D39).
- Seams `app` fills: `Accounts{EmailOf(ctx, PersonID) (model.Email, error); LinkAccount(ctx, model.Email, PersonID) (model.AccountID, model.PersonID, error); ExportPerson(ctx, PersonID) (json.RawMessage, error); DeletePerson(ctx, PersonID) (int, error)}` (`*identity.Service`; the two first methods
  are proposed); `Access{ExportPerson; DeletePerson(ctx, PersonID) (int, error)}` (`*authz.Service`); `AuditCounter{CountAbout(ctx, PersonID) (int, error)}` (`*audit.Log`); `Holders{HoldersOf(ctx, authz.Permission) ([]model.PersonID, error)}` (`*authz.Service`, D39); `Notify notify.Notifier` (scoped
  `people`, D39); `Source{Name string; Exporter PersonData}` with `PersonData{ExportPerson(ctx, PersonID) (Export, error); DeletePerson(ctx, PersonID) (ModuleDeletion, error)}`; `SectionProvider{Module string; personpage.Provider}`.
- `Directory` (= `module.Deps.People`): `NamesOf(ctx, []model.PersonID) (map[model.PersonID]string, error)`, `ConsentOf(ctx, model.PersonID, Purpose) (bool, error)`.
- `Options{DB, Clock, Logger, Tracer, Meter, Audit (scoped "people"), Signals (scoped "people"), Authz authz.Authorizer, Actor authz.ActorFunc, Accounts, Access, AuditCount AuditCounter, Policy PolicySource, Notify notify.Notifier, Holders Holders, Session SessionFunc}` (+`Validate`, which refuses a
  nil `Session` (P-W1, D58) as it does the other required fields; `Notify` and `Holders` optional: nil means no notice); `New(Options) (*Service, error)` (no I/O; subscribes to `identity.account-created`; works against `db.None()`); `(*Service).Link(sources []Source, sections []SectionProvider,
  members []MembershipProvider) error` (once, from `app`'s `link`); `MembershipProvider{Module string; Provider membership.Provider}`.
- `SessionFunc func(ctx) (identity.Session, bool)` (= `identity.SessionFrom`; P-W1, D58): `Me` reads the session's `IssuedAt` through it (decision 34). `Me` records the first-sign-in prompt and sets `Profile.FirstPrompt` (decisions 34 to 36); `Get` never does.
- `*Service` (safe for concurrent use; nothing to close): `Directory` + `PlayaNameOf` (= `identity.NameSource`); `Directory.LegalNamesOf(ctx, []PersonID) (map[PersonID]string, error)` (D44: needs `people.contacts.read` in some extent and answers only for the people it reaches (D45) and always for
  the actor's own ID (D46, no audit entry for it), audited per other returned person, never on a route); `LanguageOf(ctx, PersonID) (string, error)` and `ContactEmailOf(ctx, PersonID) (model.Email, error)` (D39: system reads for notify's seams: no actor, no audit, `NotFound` when absent, never on a
  route); `Me`, `UpdateMe`, `Get`, `Create`, `Update`, `AddContact`, `UpdateContact`, `RemoveContact`, `SetConsent`, `Search`, `LinkAccount`, `Export`, `Delete`, `RequestDeletion`, `MyRequests`, `WithdrawRequest`, `ListRequests`, `DeclineRequest`, `FulfilRequest`, `ExportCommand(ctx, io.Writer,
  string) error`, `DeleteCommand(ctx, io.Writer, string) error`, `Routes() []Route`, `OpenAPI() []byte`; `Route{Method, Pattern string, Handler http.Handler}` (all signed-in; `app` converts).
- `NameKey(name string) string`: the one rule for "the same playa name" (NFKC, trim, internal whitespace runs to one space, `strings.ToLower`); `""` for a blank name; pure, cannot fail. Modules compare names with it; the store writes it to `playa_name_key`.
- Constants: `MaxPlayaNameBytes = 100`, `MaxPronounsBytes = 40`, `MaxLegalNameBytes = 200`, `MaxPhoneBytes = 40`, `MaxRelationshipBytes = 60`, `MaxLanguageBytes = 35`, `MaxNoteBytes = 500`, `MaxEmergencyContacts = 10`, `MinSearchBytes = 2`, `MaxSearchBytes = 100`, `MaxSearchResults = 50`, `DefaultSearchResults = 20`, `MaxNames = 200`, `ExportCooldown = 10 * time.Minute`, `MaxTrackedExports = 10000`, `SectionTimeout = 2 * time.Second`.
- Permissions: `people.directory.read` (department-scopable, reaches `internal`), `people.contacts.read` (department-scopable, `personal`), `people.profiles.manage` (site, `personal`), `people.data.export`, `people.data.delete` (site, `sensitive`); `Permissions []authz.PermissionDecl`. Signals
  published `people.person-created {person}`, `people.person-deleted {person}`, `people.deletion-requested {request, person}`; subscribed `identity.account-created`. Extension point `people.person-page` (`personpage.Provider`). Settings `people.legal_name`, `people.phone`,
  `people.emergency_contacts` (enum `hidden|optional|required`; emergency contacts never `required`, Q-CORE7). Audit actions `people.person.read|created|changed|deleted|linked|exported`, `people.search.read`, `people.contact.created|changed|deleted`, `people.consent.changed`,
  `people.request.created|withdrawn|declined|fulfilled`. `postgres.Migrations fs.FS` (files `0001` to `0004`); `Set = db.Set{Name: "people", Schema: "core"}`. Commands `bms export <person-id>`, `bms delete <person-id>`.
- `peopletest.NewFake() *Fake` (a `Directory` with `Fake.Add(id, name)`, `Fake.AddLegalName(id, name)` (D44), `Fake.Consent(id, purpose, granted)`; safe for concurrent use).
- `personpage` (contract): `Item{Label, Value, Href string}`, `Section{Key string, Items []Item, Failed bool}`, `Provider{Sections(ctx, []model.PersonID) (map[model.PersonID][]Section, error)}`; `Point = "people.person-page"`. `membership` (contract, D44): `Point = "people.membership"`; `Provider{DepartmentsOf(ctx, []model.PersonID) (map[model.PersonID][]model.DepartmentID, error)}`.

**Key decisions.**

- **1.** one `core.person` row per `PersonID` minted by identity (sign-up) or by people (`Create`); modules and authz reference it by foreign key, so a dangling person ID cannot be written.
- **2.** the row for a new account is created by the `identity.account-created` handler (idempotent) and, because delivery is at most once, repaired by the person's first `UpdateMe`; no synchronous creation inside `SignUp` (owner, 2026-10-18); `Me` on a missing row answers an empty, incomplete profile without writing (works on a read-only copy).
- **3.** the playa name is the name shown everywhere; it is required, trimmed, unique across the installation and permanent once set (decisions 29 to 32), and empty until the person fills it in; the shell prompts once per session, recorded by the server, and never blocks (decisions 34 to 36).
- **4.** the login e-mail is identity's (identity decision 17): people reads it through `Accounts.EmailOf` for display and holds `contact_email` only for people without an account; search by login address is the accounts screen's.
- **5.** legal name is one field (waivers differ; name order differs by culture; a later migration can add structured names) (owner, 2026-10-18); phone is stored as entered after a loose shape check.
- **6.** `required` field modes bind the person's own saves only: an admin may create or edit a profile with gaps (a lead adding a walk-on may not know the legal name); the profile is then `Complete: false`.
- **7.** no per-field visibility choices in M2: the personal fields are already restricted to the person and permitted roles and pronouns exist to be seen; future sharing choices are consents.
- **8.** consents are rows keyed by purpose, granted and revoked by the person (or recorded by a profile manager for a paper form), read by modules through `Directory.ConsentOf`; the core declares no purpose in M2; the flag is `internal`; the table and `ConsentOf` ship in M2 anyway so a module author needs no core change (owner, 2026-10-18).
- **9.** `people.directory.read` and `people.contacts.read` are department-scopable so a department role may hold them (authz decision 1); while no `people.membership` provider is registered (D44), holding `people.directory.read` in any extent reaches every person, which the spec says out loud;
  `people.contacts.read` is different (owner, 2026-10-21, D45): while no `people.membership` provider is registered, a site extent reaches everyone and a department-scoped extent reaches nobody (fail closed: there is no way to say who belongs to the department), so `department-admin` keeps
  `people.contacts.read` by default and gains reach only when a provider reports members; `people.contacts.read` was seeded into `account-admin` only in M2 and `department-admin` gained it with D44 (owner, 2026-10-18; amended 2026-10-21).
- **10.** a self export is immediate (audited, `ExportCooldown`) because it changes nothing and the data is the person's (a "confirm your password" step is Later hardening) (owner, 2026-10-18); a self deletion is a request a holder of `people.data.delete` fulfils or declines, because it is irreversible and touches safety records.
- **11.** deletion is one `Write` transaction: modules in build order, then files (D44), then authz, then identity, then people's rows; any failure (`last_access_admin`, a superuser account, a module error) rolls everything back; audit rows stay and are reported as `Kept{Kind: "audit.entry"}` (audit decision 10).
- **12.** export runs in no transaction and people records the one `people.person.exported` entry; modules do not audit inside `ExportPerson`.
- **13.** `LinkAccount` takes the account's e-mail (no ID pasting on a phone) and goes through identity; the person row sign-up minted for that account is deleted afterwards in a second transaction only when it is empty and unreferenced, else kept and reported.
- **14.** the person page's module sections come from `people.person-page` through `app`; each provider runs under `SectionTimeout`, a failing provider yields `Section{Failed: true}` and never fails the page.
- **15.** reads of personal fields about someone else record one `people.person.read` with the fields actually returned; self reads are not audited (`ReadIsAudited(self)`); a search that matched personal fields records `people.search.read` per result.
- **16.** `Reason` on audit entries is a token (`request:<id>`, `command`, `admin`), never free text.
- **17.** regional differences are the three settings and role membership; no retention setting yet (nothing measures inactivity; M4 proposes `people.retention.unused` with `jobs`).
- **18.** the person-facing "your data was viewed" is Later (D22, audit's `ViewsOf`); in M2 the person page links holders of `audit.search` to `/audit?subject=<id>`.
- **19.** the person's download, deletion requests, the admin's export and delete and the `bms export` / `bms delete` commands ship in M2, not M4 (09 §11 and the milestone table moved) (owner, 2026-10-18).
- **20.** `people.directory.read` is not in the base role by default (no open directory); a regional that wants one adds it to `participant` (owner, 2026-10-18).
- **21.** (D37) a language-only `PUT /me` (the body sets nothing but `language`) is exempt from the required-fields check, so the shell can save the language before the profile is complete.
- **22.** (D37) the consent audit entry (`people.consent.changed`) records the purpose and the direction (`Fields: [purpose]`, `Reason` `granted:<purpose>` or `withdrawn:<purpose>`, D38 (s)).
- **23.** (D37) a walk-on's `contact_email` is cleared when an account is linked (the account's address replaces it).
- **24.** (D37) reads of deletion-request notes (`MyRequests`, `ListRequests`) by someone other than the requester are audited.
- **25.** (D37) data rights (`Export`, `Delete`, and the commands) for an account holder with no `core.person` row still run the identity and authz steps.
- **26.** (D37) `Delete` publishes, through authz's `DeletePerson`, `access.grant-revoked` for each removed grant.
- **27.** the deletion-request notice is sent by people through its own `Notifier` seam, to the holders of `people.data.delete`, in `RequestDeletion`'s `Write` transaction; notify subscribes to nothing (notify decision 14).
- **28.** `LanguageOf` and `ContactEmailOf` are system reads for notify and never reachable from a route.
- **29.** a playa name is **unique across the installation** (reverses decision 3's "not unique"): the key column `core.person.playa_name_key` is written by the store from `NameKey(playa_name)` on every write of the name and the partial unique index `person_playa_name_key_idx` (`WHERE playa_name_key
  <> ''`) is the one decider, so two people claiming one name at once cannot both win; a `23505` on that index becomes `Conflict` with field `playa_name` code `taken` on `UpdateMe`, `Create` and `Update`, naming nobody (the name is the caller's own input; who holds it is not said, not even to a
  profile manager).
- **30.** a playa name is **self-appointed and permanent**: the person types it (a profile manager entering a walk-on types the name the person gives); once a row holds a non-empty name, `UpdateMe` with a different `NameKey` fails `Invalid` `playa_name` `permanent`, the same name resubmitted is a
  no-op (the form sends the whole profile), a respelling with the same key (capitalisation, spacing) is accepted and audited like any change because the key, not the spelling, is what is permanent and unique; the language-only exemption (decision 21, D37) is unchanged; a holder of
  `people.profiles.manage` may always change any person's set name, their own included, through `Update` (owner, 2026-10-23, Q-PEOPLE-8: "An admin may always make changes - that's why they exist, for when workflows are broken and exceptions need to be made."): audited `people.person.changed` with
  `Fields [playa_name]` and `Reason "rename"`, the same `taken` rule, the old name free at once; this is the stopgap until the retirement and inappropriate-name process exists.
- **31.** `NameKey` is NFKC normalisation (`golang.org/x/text/unicode/norm`, already in the module graph), `strings.TrimSpace`, internal whitespace runs collapsed to one space (`strings.Fields`), then `strings.ToLower`: the owner's purpose is telling people apart on the roster and the radio, so
  names that look alike must share a key; the stored `playa_name` keeps the person's own spelling; the migration backfills the key for existing rows with the equivalent SQL expression (`lower(regexp_replace(btrim(normalize(playa_name, NFKC)), '\s+', ' ', 'g'))`) and refuses to run (RAISE, the
  transaction rolled back, the set's version unchanged) when existing rows already clash, listing the person IDs and never a name, because inventing a new name for someone contradicts "self-appointed" and a silent skip would ship an index that does not hold; the installation resolves the clash on
  the previous version, where renames are still allowed, then migrates.
- **32.** a deleted person's name is **free at once**: holding it would keep a name after "deleted means deleted" and would pre-empt the retirement process the owner wants designed later; the audit trail keeps the person ID, never the name.
- **33.** **Later**, each one sentence so nothing here blocks it: name retirement (a name unused for about five years becomes eligible, with a way to retire a vulgar or otherwise inappropriate name sooner; needs a "last used" signal from modules and a retired-names table), optional approval
  pipelines for a new name (a `pending` name the person cannot yet be shown under), and several names per person (an alias plus a radio handle where only the handle must be unique: a later `handle` column gets its own key column and unique index; whether the playa name stops being unique then is the
  owner's call at that time, and dropping `person_playa_name_key_idx` is one migration). `Search`, `NamesOf`, `PlayaNameOf` and the export are unchanged; no signal is added (a rename can never create a clash, so volunteers' Q-VOL11 case disappears).
- **34.** the first-sign-in prompt is **once per session, recorded by the server**: `core.person.name_prompted_at timestamptz NULL` (migration `0004`) is due when `playa_name = ''` and the column is NULL or before `identity.Session.IssuedAt` (read through the new seam `Options.Session SessionFunc`,
  `app`: `identity.SessionFrom`; `Options.Validate` field `people.session`); a prompt writes `GREATEST(now, IssuedAt)`, so each session is prompted at most once whatever two copies' clocks say, and several devices do not re-prompt each other; a new session after a password change or "sign out
  everywhere else" is prompted once more (owner, 2026-10-30, D58).
- **35.** `Me` records it itself: one conditional `UPDATE ... RETURNING id` (`MarkNamePrompted`) in its own `Write` transaction after the row read and the profile build (a failed build leaves no mark; D58 review), the statement being the one decider under concurrency; `Profile.FirstPrompt` (wire
  `first_prompt`, `omitempty`, `internal`) is true exactly when the row was updated; `Get` never reports it; no POST, no audit, no permission beyond being signed in, `updated_at` untouched; the column is exported with the person and never cleared.
- **36.** the superuser and a named row answer `false` without writing; a read-only copy answers the profile with `false` and no error (`db.ErrReadOnly` from the mark is "cannot record"), so a read-only copy never prompts and never loops; a person with no row yet is answered `true` without writing
  on a writable copy (decision 2's repair comes with their first save) and `false` on a read-only one; any other failure of the mark fails `Me` with its kind. The frame acts on `first_prompt` alone, once per answer, with in-memory state and no browser storage.
- Tests for decisions 34 to 36: F91 to F105 (the first-sign-in prompt); for 29 to 33: F74 to F88 (playa names).

**Errs kinds callers handle.**

- Every use case: `Unauthenticated` (no actor), store kinds (`Unavailable`, `Timeout`, `Canceled`), `Unavailable` wrapping `db.ErrReadOnly` on writes.
- `Get`/`Update`/contacts on another person: `Permission` (no `people.directory.read` anywhere; or no `people.profiles.manage` for a change), `NotFound` (unknown ID).
- Inputs: `Invalid` with field codes `required`, `too_long {max}`, `malformed`, `control_chars`, `hidden`, `too_many {max}`, `one_of`, `self`, `unknown`.
- `Search`: `Invalid` (`q` `too_short {min}`/`too_long`, `limit` `range`), `Permission`.
- `NamesOf`: `Invalid` (`ids` `too_many`).
- `SetConsent`: `Invalid` (purpose), `Permission`.
- `LinkAccount`: `Conflict` (`has_account`, `superuser`), `NotFound` (field `email` code `unknown`; never says more).
- `Export`: `Permission`, `NotFound`, `Unavailable` wrapping `httpx.ErrRateLimited` (self cooldown).
- `Delete`/`FulfilRequest`: `Permission`, `NotFound`, `Invalid` (`person` `self`), `Conflict` (`last_access_admin` from authz, `superuser` from identity, `in_use` from a foreign key), the module's error wrapped with its name.
- Requests: `Conflict duplicate` (an open request exists), `Conflict status` (not open).
- `UpdateMe`, `Create`, `Update`: `Conflict` with field `playa_name` code `taken` when another person holds the same `NameKey` (the unique index decides, so the race is covered; the error names nobody); `UpdateMe`: `Invalid` `playa_name` `permanent` when a name is set and the new one has a different key; `Update` by a profile manager never fails `permanent`: a different key is written and audited with `Reason "rename"`, for any person including the manager.
- `Link`: `Internal` (called twice, nil provider).
- `Me`: `Internal` when a signed-in actor has no session in ctx (a wiring error, F104); the first-sign-in mark failing with a store kind fails `Me` with it, except `db.ErrReadOnly`, which answers `first_prompt` false and no error (F96, F97).
- `New`/`Options.Validate`: `Invalid` with fields `people.db`, `people.clock`, `people.logger`, `people.tracer`, `people.audit`, `people.signals`, `people.authz`, `people.actor`, `people.accounts`, `people.access`, `people.audit_count`, `people.session` (P-W1, F102).

**Owns.**

- Tables `core.person` (columns `playa_name_key`, unique index `person_playa_name_key_idx`, migration `0003_playa_name_key.sql`; column `name_prompted_at`, migration `0004_name_prompted_at.sql`), `core.person_emergency_contact`, `core.person_consent`, `core.data_request`.
- The five permissions; three signals and one subscription; the extension point; the three settings; the audit actions above.
- Route set `people` (listed under Interface); screens `/people/me`, `/people`, `/people/new`, `/people/:person`, `/people/admin/requests`; keys `core.people.*`.
- Spans `people.<use-case>`, `people.section`; metrics `bms.people.exports{outcome}`, `bms.people.deletions{outcome}`, `bms.people.sections{module,outcome}`. Commands `export`, `delete`.

**Expects of dependencies.**

- (D37: `authz.DeletePerson` publishes `access.grant-revoked` per removed grant.)
- `identity`: `PersonFrom`/`SessionFrom` semantics; `EmailOf` and `LinkAccount` (proposed); `DeletePerson` refusing a superuser account with `Permission` (proposed); `NameSource` maps `NotFound` to `""`.
- `authz`: `ExtentOf` for the any-extent checks, `Allow` for site permissions, `Conflict last_access_admin` from `DeletePerson`, migration `0002` adding the foreign key to `core.person`, starter-role seeding from `StarterRoles`.
- `audit`: a `Recorder` scoped `people`; `CountAbout` callable inside a `Write` transaction; the `/audit?subject=` screen filter.
- `signals`: a `Bus` scoped `people` (publishes the three, subscribes `identity.account-created`); `InDelivery`.
- `db`: `Transact` ctx-carried transactions joined by nested calls (so modules' `DeletePerson` run inside people's), `*Error.Constraint`, `ErrReadOnly`.
- `app`: part 5 (declare, build, link, commands, the `NameSource` adapter for identity, the `Accepts` for the point, `Deps.People`).
- `module-contract`: `Deps.People people.Directory`; `peopletest.NewFake()` in `Conformance`. `settings` (M2): the three keys through `PolicySource`.
- `web-shell`: `GET /api/people/me` read once per session by the frame (language, profile prompt); `web/src/core/people/` as a core area.
- `identity` (P-W1, D58): `Session.IssuedAt` stays the server time at sign-in, fixed for the session's life; a new session (password change, "sign out everywhere else") carries a new one.
- `app` (P-W1, D58): `core_people.go` sets `Options.Session = identity.SessionFrom`.

**Open questions:** None. Answered questions are folded into the decisions above; their record is in `plan/design/history/people.md`.

**Proposed changes elsewhere:** 30, listed at the end. All applied or needing no change (18 and 24: `docs/operating.md`, applied 2026-10-30).

**Accepted deviations (M2 build, 2026-10-18).**

- Leaf package `people/rows`.
- Extra exports (`PublishedSignals`, `SubscribedSignals`, `ParseRequestStatus`, `RequestStatus.String`, the Field constants, `personpage.SectionTimeout`).
- `UpdateMe`, `LinkAccount` and `Delete` take the deletion lock.
- Export cooldown kept on Canceled.
- Exit codes of the commands.
- The five owner questions the build raised are answered (decisions 21 to 25, 26). See Behavior, "Accepted implementation deviations (M2 build, 2026-10-18)".

## Purpose

Hold who each participant is, so every other part of bms can refer to a person by ID and show them by playa name.

## Responsibilities

- Keep one profile per person: playa name, pronouns, saved language (`internal`); legal name, phone, a contact address for people without an account (`personal`); whether the person has an account and whether the profile is complete under the regional's settings.
- Keep each playa name unique across the installation and permanent once set, and export the one rule (`NameKey`) that says when two names are the same (decisions 29 to 31).
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
| Which people belong to a department (so a lead sees only their volunteers' contacts) | a module that has members contributes a `membership.Provider` to `people.membership` (D44; `volunteers` is the first); the extents filter on what the providers report. |
| Qualifications, age, certificates, photos, addresses, "about me" | not collected (09 §3; Q-QUAL). |
| Retention jobs, e-mail to the person or the admins about requests | `jobs` and `notify` (M4), through `people.deletion-requested`. |
| Merging two person rows | Later; `LinkAccount` only tidies an empty orphan (decision 13). |
| Retiring a playa name (unused for about five years, or vulgar or otherwise inappropriate), approving a new name before it is shown, a second name such as a radio handle | Later (decision 33); until then a profile manager's `Update` is the stopgap. |
| Invitations, sign-up fields beyond e-mail and password | `identity` (M4). |
| Translating purpose and section titles | the owning module's catalogue (`module.<name>.consent.<ident>.*`, `module.<name>.section.<ident>.*`). |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| Profiles | keep each person's identity-free profile and say what a viewer may see of it | `core.person`; `Me`, `Get`, `Create`, `Update`, `UpdateMe`, `PlayaNameOf`, `NamesOf`, `Search`; `NameKey`, `playa_name_key`, `person_playa_name_key_idx` |
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
	PlayaName  string      // internal; "" until filled in; unique under NameKey across the installation and permanent once set (decisions 29, 30)
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

// NameKey (decision 31) is the form in which two playa names are compared: NFKC
// normalisation, leading and trailing space removed, each run of internal whitespace
// replaced by one space, then lower-cased with strings.ToLower. "" for a blank name. It
// is the one rule for "the same name": the store writes it to core.person.playa_name_key,
// the unique index person_playa_name_key_idx enforces it, and modules that compare playa
// names (volunteers' vol.NameKey delegates here) use it rather than their own copy. Pure:
// no I/O, cannot fail; the result may be longer than MaxPlayaNameBytes (NFKC can expand a
// character), which no constraint limits because the key is never shown or typed. Never
// logged: a key is a name.
func NameKey(name string) string

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
	// FirstPrompt (P-W1, decisions 34 to 36) is set by Me only: true when this call recorded
	// the first-sign-in prompt for this session. Get never sets it. Wire: first_prompt, omitempty.
	FirstPrompt bool
}

// ProfileInput is a partial update: nil leaves a field unchanged; a pointer to "" clears
// it. Validation is per field (trim; UTF-8; no control characters; byte limits; Email
// shape for ContactEmail; language grammar for Language). PlayaName: once the row
// holds a name, UpdateMe accepts the same name again (no-op) or a respelling with the same
// NameKey, and refuses any other (Invalid "permanent"); on every write path a name whose
// NameKey another person holds is Conflict "taken" (decisions 29, 30).
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

// Holders is authz's seam (HoldersOf, D39): the explicit site-wide holders of a permission.
type Holders interface {
	HoldersOf(ctx context.Context, p authz.Permission) ([]model.PersonID, error)
}

// MembershipProvider is one module's contribution to people.membership (D44), in build order.
type MembershipProvider struct {
	Module   string
	Provider membership.Provider
}

// Settings declares people.legal_name, people.phone and people.emergency_contacts (D39).
var Settings []settings.Decl

// Templates holds the deletion-request notice, people.deletion-requested (en, es; D39).
var Templates fs.FS

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
	// LegalNamesOf (D44) returns the legal name of each person who has one. Requires
	// people.contacts.read in any extent (Permission otherwise); Invalid when
	// len(ids) > MaxNames; audits one people.person.read per returned person other than the actor (Fields
	// [legal_name], Level personal) before returning. The actor's own ID is answered whenever
	// they have a legal name, whatever the extent (D46; a self read, decision 15). Consumer: volunteers' roster. Never
	// on a route. peopletest.Fake.AddLegalName seeds it.
	LegalNamesOf(ctx context.Context, ids []model.PersonID) (map[model.PersonID]string, error)
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
	Notify     notify.Notifier // optional (D39); scoped "people"; nil = no deletion-request notice
	Holders    Holders         // optional (D39); nil = no notice
	Session    SessionFunc     // app: identity.SessionFrom; Validate fails Invalid field people.session when nil (decision 34)
}

// SessionFunc reads the request's session from ctx without I/O (identity.SessionFrom).
// ok is false when there is no session. Me reads IssuedAt from it (decision 34).
type SessionFunc func(ctx context.Context) (identity.Session, bool)

// Validate fails errs.Invalid joining a validate.FieldError per nil dependency (fields
// people.db, people.clock, people.logger, people.tracer, people.audit, people.signals,
// people.authz, people.actor, people.accounts, people.access, people.audit_count,
// people.session).
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
func (s *Service) Link(sources []Source, sections []SectionProvider, members []MembershipProvider) error

// Directory and NameSource.
func (s *Service) NamesOf(ctx context.Context, ids []model.PersonID) (map[model.PersonID]string, error)
func (s *Service) ConsentOf(ctx context.Context, person model.PersonID, purpose Purpose) (bool, error)
// PlayaNameOf is identity.NameSource: NotFound when there is no row yet (identity shows "").
func (s *Service) PlayaNameOf(ctx context.Context, person model.PersonID) (string, error)

// Profiles.
// Me answers the actor's own profile. On a writable copy, when the actor is not the
// superuser and the row holds no playa name, Me records the first-sign-in prompt once per
// session and reports it in FirstPrompt (decisions 34 to 36; F91 to F105).
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

// System reads for notify's seams (D39, decision 28): no actor, no audit, never on a route.
// LanguageOf returns the saved language ("" when none); ContactEmailOf returns contact_email
// for a person without an account (NotFound when empty or absent).
func (s *Service) LanguageOf(ctx context.Context, person model.PersonID) (string, error)
func (s *Service) ContactEmailOf(ctx context.Context, person model.PersonID) (model.Email, error)

// LegalNamesOf is Directory's method (D44, D45, D46); see Directory above.
func (s *Service) LegalNamesOf(ctx context.Context, ids []model.PersonID) (map[model.PersonID]string, error)

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

```go
package membership // internal/contracts/membership: imports model only (D44)

// Point is the extension point's name (volunteers' member rows are the first provider).
const Point = "people.membership"

// Provider reports which departments each person belongs to. With providers registered, a
// department-scoped people.directory.read@D or people.contacts.read@D extent reaches only
// the people the providers report as members of D (decision 9).
type Provider interface {
	DepartmentsOf(ctx context.Context, people []model.PersonID) (map[model.PersonID][]model.DepartmentID, error)
}
```

```go
// Store gains (postgres.Store implements it; sql/mark_name_prompted.sql):
//
// MarkNamePrompted records that the person was prompted for a name, if the prompt is due
// (playa_name = '' and name_prompted_at is NULL or before issued). It writes
// GREATEST(now, issued). Returns true when the row was updated. Runs inside the Write
// transaction ctx carries. Fails Unavailable wrapping db.ErrReadOnly on a read-only copy
// (the caller maps it to "not prompted"), and the store's kinds.
MarkNamePrompted(ctx context.Context, person model.PersonID, issued, now time.Time) (bool, error)
```

**Per method: consumer, pre- and post-conditions.** "Self" means the actor's `PersonID` equals `person`. "Any extent" means `ExtentOf(p)` is not zero (site or at least one department), decision 9.

| Method | Consumer | Pre | Post |
|---|---|---|---|
| `NamesOf` | modules (`Deps.People`), `GET /names` for the access and audit screens | ≤ `MaxNames` IDs | map of known IDs → playa name; no audit (internal) |
| `ConsentOf` | modules (registrations, Later) | valid purpose, non-zero person | current flag; false for unknown |
| `PlayaNameOf` | identity (`SessionInfo`) | — | name or `NotFound` |
| `LegalNamesOf` | volunteers' roster | `people.contacts.read` in any extent; ≤ `MaxNames` IDs; legal-name mode not hidden | legal names of the people the extent reaches (decision 9, D45) and always the actor's own (D46); one `people.person.read` per returned person other than the actor; never on a route |
| `LanguageOf`, `ContactEmailOf` | notify's `LanguageSource` and `AddressSource` (`app` writes them) | — (system reads, decision 28) | the language or the account-less address; `NotFound` when absent; no audit |
| `Me` | the profile screen, the shell's frame (language, prompt) | an actor with a session in ctx (else `Internal`, F104) | full profile with every field group, `Can` for self; a missing row answers an empty `Person{ID: actor}` with `Complete: false`, no write; when a first-sign-in prompt is due it is recorded after the profile is built and reported in `FirstPrompt` (decisions 34 to 36, Behavior) |
| `UpdateMe` | the profile screen | an actor; fields valid; `FieldRequired` fields non-empty after the update (not checked when the body sets only `language`, D37); `FieldHidden` fields absent; when the row already holds a playa name, `PlayaName` is nil, the same string, or a respelling with the same `NameKey` (else `Invalid permanent`); the key is not held by another person (else `Conflict taken`, decided by the unique index) | row upserted (decision 2) with `playa_name_key = NameKey(playa_name)` whenever the name is written; `people.person.changed` (or `.created` plus `people.person-created` when the row was missing) in the transaction; an unchanged profile writes and audits nothing |
| `Get` | the person page; admin screens | `people.directory.read` in any extent (self → `Me`) | internal fields; personal fields and contacts when `people.contacts.read` reaches the person (decision 9: site, or a department extent with a provider reporting them); consents when `people.profiles.manage`; sections; `people.person.read` recorded when any personal field was included |
| `Create` | "new person" screen | `people.profiles.manage`; `PlayaName` non-empty (the name the walk-on gives, decision 30); its key not held by another person (else `Conflict taken`); `ContactEmail` optional | row with `HasAccount: false` and its `playa_name_key`; `people.person.created`; `people.person-created` published |
| `Update` | person page edit (admins) | `people.profiles.manage`; person exists; a new name's key not held by another person (`Conflict taken`); a set name may not be cleared (`Invalid required`); changing a set name to a different key is allowed, for any person including the actor (decision 30) | changed fields; `people.person.changed` with `Fields`; a rename carries `Reason "rename"` so the audit screen can list every stopgap rename |
| `Search` | people screen; the access grant form; the audit filter | `people.directory.read` in any extent; `q` within bounds | ≤ limit matches ordered by playa name; matches on legal name, phone or contact address only for people a held `people.contacts.read` reaches (decision 9, D45), each such match recorded `people.search.read` |
| `AddContact`/`UpdateContact`/`RemoveContact` | profile and person page | self or `people.profiles.manage`; `EmergencyContacts != FieldHidden`; `Name` and `Phone` non-empty; count ≤ `MaxEmergencyContacts` | row changed; `people.contact.<verb>` in the transaction |
| `SetConsent` | profile screen (a module's section links to `/people/me#consents`; `Directory` has no setter, so a module can only read consent) | self, or `people.profiles.manage` (a paper form) | row upserted with `RecordedBy`; `people.consent.changed` with `Fields: [purpose]` and `Reason` `granted:<purpose>` or `withdrawn:<purpose>` (D37, D38 (s)). |
| `LinkAccount` | person page "Link account" | `people.profiles.manage`; person has no account | account re-pointed through identity; `people.person.linked` (record = person, fields none); orphan handling per decision 13 |
| `Export` | profile "Download my data"; person page export; admin | self (cooldown) or `people.data.export`; `Link` done | one JSON document (shape under Behavior); `people.person.exported` (level sensitive) recorded before the body is written |
| `Delete` | person page delete; admin | `people.data.delete`; not self; `Link` done | everything gone in one transaction; `people.person.deleted`; `people.person-deleted` published |
| `RequestDeletion` | profile screen | an actor; no open request; note ≤ `MaxNoteBytes` | open request; `people.request.created`; `people.deletion-requested` published |
| `MyRequests` | profile screen | an actor | own requests, newest first (the requester's own read is not audited) |
| `WithdrawRequest` | profile screen | own and open | status withdrawn; `people.request.withdrawn` |
| `ListRequests` | requests screen | `people.data.delete` | requests by status (default open), oldest first, with the person's playa name resolved by the shell through `/names`; records one `people.request.read` (`Level: personal`, `Fields: [note]`, `Subject` the requester) per returned request that has a note and is not the actor's own (D37, D38 (u)). |
| `DeclineRequest` | requests screen | `people.data.delete`; open | status declined, note; `people.request.declined` |
| `FulfilRequest` | requests screen | `people.data.delete`; open; not self | `Delete` of the request's person; `Reason: "request:<id>"` |
| `ExportCommand`/`DeleteCommand` | `app` | writing copy for delete; `Link` done | as `Export`/`Delete` with `ActorOperator`, `Reason: "command"` |
| `Link` | `app` | once | sources and providers frozen |
| `Routes`/`OpenAPI` | `app` → `api` | — | the table below; a fragment that matches |

**Routes** (relative to `/api/people`, all signed-in):

| Method and pattern | Use case | Notes |
|---|---|---|
| `GET /me` | `Me` | the frame reads it once per session; the answer carries `first_prompt` when true (decision 35) |
| `PUT /me` | `UpdateMe` | body `ProfileInput`; answers a `Profile` without `first_prompt` |
| `POST /me/contacts`, `PUT /me/contacts/{contact}`, `DELETE /me/contacts/{contact}` | contacts (self) | |
| `PUT /me/consents/{purpose}` | `SetConsent` (self) | body `{granted}` |
| `GET /me/export` | `Export` (self) | `Content-Disposition: attachment; filename="bms-export-<person>.json"` |
| `GET /me/requests`, `POST /me/requests`, `POST /me/requests/{request}/withdraw` | requests (self) | body `{kind: "delete", note}` |
| `GET /search?q=&limit=` | `Search` | |
| `GET /names?ids=` | `NamesOf` | comma-separated, ≤ 200 |
| `POST /` | `Create` | |
| `GET /{person}`, `PUT /{person}` | `Get`, `Update` | own ID behaves as `/me`, except that a `people.profiles.manage` holder may change their own set playa name here (decision 30, D48) |
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

Validation rules (every text field: trimmed, valid UTF-8, no control characters (`control_chars`), byte limit (`too_long {max}`)): `PlayaName` 1..100 (`required` on `UpdateMe` and `Create`; `required` also on `Update` when the person already has a name; `taken` (`Conflict`) on every write when
another person holds the same `NameKey`; `permanent` on `UpdateMe` when a name is set and the new one has a different key, checked in this order: `required`, the per-field rules, `permanent`, then the index); `Pronouns` 0..40; `Language` `""` or `^[a-z]{2,3}(-[A-Za-z0-9]{1,8})*$` ≤ 35 (`malformed`);
`LegalName` 0..200; `Phone` 0..40 and `^[+0-9][0-9 ().\-]*(x[0-9]{1,6})?$` after trimming (`malformed`; loose on purpose, decision 5); `ContactEmail` `model.ParseEmail` (`malformed`), refused with `Invalid` code `has_account` when the person has an account; contact `Name` 1..200, `Phone` 1..40 (same
shape), `Relationship` 0..60; `note` 0..500. A field whose mode is `FieldHidden` in the body fails `Invalid` code `hidden`.

## Data

Schema `core`, migration set `people` (`db.Set{Name: "people", Schema: "core"}`), files `0001_person.sql`, `0002_requests.sql`, `0003_playa_name_key.sql`, `0004_name_prompted_at.sql` (version 4). Timestamps `timestamptz`. Retention: every row lives until the person is deleted (`Delete`); no time-based retention in M2 (decision 17, D30).

**`core.person`**

| Column | Type | Constraints | Level | Why collected |
|---|---|---|---|---|
| `id` | `uuid` | PK | — | the `PersonID` identity minted or people generated |
| `playa_name` | `text` | NOT NULL DEFAULT '', `octet_length <= 100` | internal | how people know each other; the person's own spelling, kept as entered |
| `playa_name_key` | `text` | NOT NULL DEFAULT ''; written by the store as `NameKey(playa_name)` whenever `playa_name` is written; partial unique index `person_playa_name_key_idx (playa_name_key) WHERE playa_name_key <> ''` | internal (derived from the name; never shown, exported or logged) | the one form in which names are compared, so the index can hold uniqueness (decisions 29, 31) |
| `pronouns` | `text` | NOT NULL DEFAULT '', `<= 40` | internal | address people correctly |
| `language` | `text` | NOT NULL DEFAULT '', `<= 35` | internal | the person's UI language (web-shell Proposed change 13) |
| `legal_name` | `text` | NOT NULL DEFAULT '', `<= 200` | personal | waivers and gate (setting `people.legal_name`) |
| `phone` | `text` | NOT NULL DEFAULT '', `<= 40` | personal | reaching a volunteer (setting `people.phone`) |
| `contact_email` | `text` | NULL, `octet_length between 3 and 254` | personal | reaching a person who has no account (identity decision 17); stored as entered, index on `lower(contact_email)` (not unique) |
| `name_prompted_at` | `timestamptz` | NULL; set only by `MarkNamePrompted`, never by `insert_person.sql`, `upsert_person.sql` or `update_person.sql` | internal (the viewer's own; never shown as a time, never logged) | when the person was last asked for a playa name, so the prompt runs once per session (decisions 34, 35); exported with the person object (`export_person.sql` gains the column; "export everything" has no exception for marks), deleted with the row, moot once `playa_name` is set |
| `created_by` | `uuid` | NULL, FK `core.person(id)` ON DELETE SET NULL | — | which profile manager created an account-less person |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | — | |

Index `person_playa_name_idx ON lower(playa_name) text_pattern_ops` (prefix matches; substring matches scan, acceptable at a regional's scale, revisit with traces). It stays as the search index; uniqueness is the separate `person_playa_name_key_idx` on the stored key, because the rule for "the same
name" lives in Go (`NameKey`) and the column holds exactly what Go computed, so the index can never disagree with the modules that call the same function; an expression index would have made PostgreSQL's `lower()` the rule and Go a copy that drifts on edge cases.

**Migration `0003_playa_name_key.sql`**, one transaction:

1. `RAISE EXCEPTION` unless `current_setting('server_encoding') = 'UTF8'` (`normalize()` needs it; every supported install is UTF-8, and a non-UTF-8 one must learn it here rather than from a wrong key)
2. `ALTER TABLE core.person ADD COLUMN playa_name_key text NOT NULL DEFAULT ''` and its `COMMENT` (`sensitivity: internal`)
3. first a guard: `RAISE EXCEPTION` when the database's `lower()` cannot fold non-ASCII (`lower(E'É') <> E'é'`, e.g. `LC_CTYPE=C`, or, on an ICU-provider database, `lower(E'İ') <> 'i'` or `lower('ΑΣ') <> 'ασ'`) and any existing `playa_name` contains non-ASCII (D51); then `UPDATE core.person SET
   playa_name_key = lower(btrim(regexp_replace(normalize(playa_name, NFKC), '[\s\u0085\u1680\u2028\u2029]+', ' ', 'g'))) WHERE playa_name <> ''` (collapse first, then trim, with an explicit whitespace class so the result does not depend on `LC_CTYPE`; D51; the SQL twin of `NameKey`, proven equal on
   the F82 table by an integration test; from here on the store writes the key and SQL never computes it again)
4. a `DO` block that counts keys held by more than one row and, if any, `RAISE EXCEPTION 'people 0003: % playa name(s) are held by more than one person (person ids: %)'` with the count and the IDs, never a name, so `bms migrate` stops with the set at version 2 and nothing applied (the project is
   pre-release, so this is expected to find nothing; if it does, the installation stays on the previous version, where renames are allowed, until the people concerned have chosen different names; the alternatives, inventing a suffix for one of them or leaving the clash and skipping the index, were
   rejected because the first picks a name for someone and the second ships a rule that does not hold)
5. `CREATE UNIQUE INDEX person_playa_name_key_idx ON core.person (playa_name_key) WHERE playa_name_key <> ''`.

Safe on a live database at a regional's scale (one short exclusive lock on a table of at most thousands of rows); no `CONCURRENTLY` (db decision 11). `has_account` is not a column: it is `EXISTS (SELECT 1 FROM core.account WHERE person_id = $1)`, answered by identity (`EmailOf` not `NotFound`), never by a join from people's SQL into identity's table.

**Migration `0004_name_prompted_at.sql`** (P-W1, version 4): `ALTER TABLE core.person ADD COLUMN name_prompted_at timestamptz NULL` with `COMMENT ... 'sensitivity: internal'`. Safe on a live table (no rewrite: a nullable column with no default).

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

History of grants and revocations is the audit log (`people.consent.changed`, `Fields: [purpose]`, `Reason` `granted:<purpose>` or `withdrawn:<purpose>`; D37, D38 (s)).

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

**Foreign keys from elsewhere** (proposed, not people's migrations): `core.role_grant.person_id → core.person(id)` (authz `0002`, ON DELETE RESTRICT: authz's own `DeletePerson` runs first), `core.role_grant.granted_by → core.person(id)` ON DELETE SET NULL, `hello.greeting.person_id →
core.person(id)` (hello `0002`, RESTRICT). **No foreign key from `core.account`:** identity inserts the account before the person row exists (decision 2), so the constraint cannot hold; the backfill above and the repair path cover the gap, and decision 2 records the owner's choice of the signal plus
repair over a synchronous alternative that would allow one. Module authors: foreign keys to `core.person(id)` are allowed (D10) and `ON DELETE RESTRICT` is the rule, because `people` calls every module's `DeletePerson` before it deletes the row and a cascade would hide a module that forgot
something.

**Personal data never leaves these tables except:** to the person; to holders of the permissions below (audited); in `Export`. Nothing here goes to a log, span, metric, signal, error message or `Reason` (`Person.LogValue`, `Email.LogValue`; decision 16).

## Permissions

| Permission | Allows | Scoped by | Reaches | Starter roles (`StarterRoles`) |
|---|---|---|---|---|
| `people.directory.read` | `Search`; `Get` with internal fields; the person page; the `/people` menu entry | department-scopable; any extent reaches every person until a `people.membership` provider is registered (decision 9; the contract exists from D44, the behaviour changes only when an enabled module contributes a provider) | internal | `account-admin`, `access-admin`, `audit-reviewer`, `department-steward`, `department-admin`, `volunteer-coordinator` (D44) |
| `people.contacts.read` | personal fields and emergency contacts of others in `Get`; personal-field matches in `Search` | department-scopable; a site extent reaches every person; a department-scoped extent reaches nobody while no `people.membership` provider is registered, and only the members the providers report once one is (decision 9, D45; unlike the row above, where any extent reaches everyone until a provider exists) | personal | `account-admin` only in M2 (decision 9); from D44 also `volunteer-coordinator` and `department-admin` |
| `people.profiles.manage` | `Create`, `Update` and contacts for others, `SetConsent` for others, `LinkAccount`; consents shown in `Get` | site | personal | `account-admin` |
| `people.data.export` | `Export` of any person | site | sensitive (the export spans every module) | `account-admin` (09 §11) |
| `people.data.delete` | `Delete`, `ListRequests`, `DeclineRequest`, `FulfilRequest`; the requests screen | site | sensitive | `account-admin` (09 §11) |

Own records need no permission: `Me`, `UpdateMe`, own contacts and consents, own `Export`, own requests, `NamesOf` (internal), all through `identity.PersonFrom`. The base role `participant` holds none of these by default (decision 20). Every list is filtered by `ExtentOf`: with no
`people.membership` provider registered the extent is "nowhere" or "everyone" (a department-scoped grant reaches every person, decision 9); the department filter applies only once an enabled module contributes a provider (D44), and needs no permission change. "Registered" is decided at assembly: a
deployment with `volunteers` disabled has no provider and behaves as decision 9 says.

## Signals

**Published** (inside the transaction that makes the change; delivered after commit):

| Name | When | Payload |
|---|---|---|
| `people.person-created` | a person row is created (`Create`, the `identity.account-created` handler, the `UpdateMe` repair) | `{person: PersonID}` |
| `people.person-deleted` | `Delete` commits (published inside its transaction, after the modules' `DeletePerson`) | `{person: PersonID}` (the row is gone; subscribers clear caches, nothing more) |
| `people.deletion-requested` | `RequestDeletion` | `{request: RequestID, person: PersonID}` (no subscriber in M4: people sends the notice itself, D39; the signal stays for modules that want it) |

**Subscribed:** `identity.account-created {account, person}` → `INSERT INTO core.person (id, ...) VALUES ($person, '', ...) ON CONFLICT (id) DO NOTHING`, publishing `people.person-created` only when a row was inserted; runs as the system (no actor; audit `ActorSystem`, `people.person.created`).
Idempotent on the person ID, so a redelivery (`jobs`, M4) is harmless. **If it fails** (database down, read-only copy): signals logs and counts it; nothing retries in M2; the person's first `UpdateMe` creates the row (decision 2) and `Me`, `PlayaNameOf`, `NamesOf` answer empty until then. A module
writing about that person before then gets `Invalid` from the foreign key; the shell's profile prompt makes that rare (decision 2).

## Settings

| Key | Type | Default | Valid range | Who can change it |
|---|---|---|---|---|
| `people.legal_name` | enum | `optional` | `hidden`, `optional`, `required` | Settings admin |
| `people.phone` | enum | `optional` | `hidden`, `optional`, `required` | Settings admin |
| `people.emergency_contacts` | enum | `optional` | `hidden`, `optional` (never `required`, Q-CORE7; `Policy.Validate` refuses it) | Settings admin |

Declared as `people.Settings` in the settings core area `people` (D39; titles are catalogue keys `core.people.setting.<key>.title|description|option.<opt>`) and read through `PolicySource` on every use case that needs it (no cache; settings has its own snapshot); `app` implements `PolicySource` over
`Scoped("people")` (D39). With a nil source `DefaultPolicy()` applies. Changing a mode to `hidden` hides and stops accepting the field; stored values stay (a later change back shows them again); a change to `required` makes existing profiles `Complete: false` until the person fills it in.

## UI (M2)

`web/src/core/people/` (web-shell decision 3), keys `core.people.*`, every screen phone-first (single column, 44 px targets, native dialogs), works on a read-only copy for reads with every write button disabled behind the shell's read-only banner. Navigation: main "My profile" (`/people/me`, signed-in), main "People" (`/people`, access `people.directory.read`), admin "Deletion requests" (`/people/admin/requests`, access `people.data.delete`).

- **My profile** (`/people/me`).
  - Sections: *About you* (playa name, pronouns, language; legal name and phone when their mode is not `hidden`, marked required when `required`), *Login e-mail* (read-only, "change it from your account" until M4), *Emergency contacts* (cards; add, edit, remove through dialogs; hidden when the mode
    is `hidden`), *Consents* (each granted purpose with its owner's title and a revoke switch; empty: "You haven't been asked for any consent yet"), the modules' *sections* (title from `module.<m>.section.<ident>.title`; a `Failed` section shows "This part couldn't be loaded" with the reference),
    *Your data* ("Download my data" → `GET /me/export`, with the cooldown shown as a toast on 429; "Request deletion" → dialog with an optional note; an open request shows its date and a "Withdraw" button; a declined one shows the decision note).
  - **First sign-in:** the frame sends the person to `/people/me?first=1`, passing the intended page as `next`, when `GET /me` answers `first_prompt: true` and on nothing else (the server decides and records it: once per session, never for the superuser or a person with a name, decisions 34 to 36).
    The frame acts once per answer with in-memory state, reset on sign-out, and uses no browser storage. The screen shows a banner "Tell us your playa name" and focuses the field; the person may navigate away. The banner and the field's hint say, before the first save, that the name is permanent and
    must be one nobody else here uses (`core.people.me.first` loses "you can change it any time"; `core.people.profile.playa_name_hint` becomes "The name other people see. Choose carefully: it can't be changed later, and it must be one no one else here is using.").
  - Once the profile holds a name the field is read-only, shows no "(required)" marker, and has a short explanation under it ("Playa names are permanent." plus "Ask an administrator if yours must change."); the form still sends the whole profile, and the server treats the unchanged name as a no-op.
  - A `409` with field `playa_name` code `taken` shows inline under the field, worded in people's catalogue (`module.people.field.taken`: "Someone here already uses this playa name. Choose another."), and a `400` code `permanent` (only reachable by a stale or hand-made request) shows `module.people.field.permanent` ("Playa names can't be changed once set."); neither says who holds the name.
  - **Empty:** the new-profile state above.
  - **Error:** `ErrorState` with the reference.
  - **Read-only copy:** fields disabled, banner.
- **People** (`/people`). A search box (min 2 characters, hint shown), results as cards (playa name, pronouns) linking to the person page; "New person" button for `people.profiles.manage`. **Empty:** "No one matches". **Forbidden:** route hidden; a direct visit shows the shell's forbidden screen. **Error:** `ErrorState`.
- **New person** (`/people/new`, `people.profiles.manage`). Playa name (required), pronouns, legal name, phone, contact e-mail; on save goes to the person page. The playa-name hint says "Enter the name this person gave. It is permanent and must be one no one else here is using."; a `taken` conflict shows inline, as on My profile.
- **Person page** (`/people/:person`). Internal fields; personal fields and contacts when `Can.ReadContacts` (a note says "Viewing these details is recorded"); edit buttons when `Can.Edit`; in the manager's edit form the playa name is editable behind a confirm dialog ("This changes a permanent playa
  name. The change is recorded."), for any person including the manager's own page, and a `taken` conflict shows inline; "Link account" dialog (e-mail field) when `Can.Link` and the person has no account, with the result ("account linked; the empty profile it came with was removed" or "...was
  kept"); "Roles" link to `/access/grants?person=<id>` when `Can.ViewRoles` (holders of `access.grants.read`, or self); "Audit log" link to `/audit?subject=<id>` when `Can.ViewAudit` (holders of `audit.search`; the "export views" button lives on that screen, audit UI); "Export data" (`Can.Export`)
  and "Delete person" (`Can.Delete`; a confirm dialog that repeats the playa name and lists what will happen; shows the report afterwards). The modules' sections as on My profile. Own ID redirects to `/people/me`. **Not found:** the shell's not-found state. **Forbidden:** as People.
- **Deletion requests** (`/people/admin/requests`, `people.data.delete`). Tabs open / declined / withdrawn; cards with playa name (through `/names`), date, note; "Fulfil" (confirm dialog, then the report) and "Decline" (dialog with a note). **Empty:** "No open requests". **Read-only copy:** decisions disabled.
- The participant's own "who viewed my data" is **Later** (D22, audit `ViewsOf`); this spec adds no screen for it. The admin's "who viewed whose contact details" (M2 milestone) is audit's screen, reached from the person page.

## Behavior

- **Actor and permissions.** Each use case takes the actor from `Options.Actor` (`Unauthenticated` when absent, restricted, or inside `signals.InDelivery`, except `NamesOf`/`ConsentOf`, which serve the system), decides self versus other, then asks authz: `Allow` for site permissions, `ExtentOf(p).IsZero()` for the any-extent ones. The superuser passes authz (D23) and is audited like anyone.
- **Visibility.** `Get` builds `Profile.Fields` from the viewer: internal always; personal when `people.contacts.read` reaches the person (decision 9, D45) or self; consents when self or `people.profiles.manage`. Hidden-mode fields are absent even for the person. Personal fields the viewer may not
  see are zero in the JSON and absent from `Fields`. Personal fields show per person: a site extent shows them for everyone; a department-scoped extent shows them only for members of those departments as the `people.membership` providers report them, and for nobody while none is registered (D45).
- **Audit.** Changes record inside their `Write` transaction (`Record` ref kinds `people.person`, `people.contact`, `people.request`; `Subject` the person; `Fields` the names changed).
  - Reads of personal fields about someone else record one `people.person.read` after loading and before returning, `Level: personal`, `Fields` the personal groups included (`legal_name`, `phone`, `contact_email`, `email`, `emergency_contacts`), outside any `Read` transaction (audit decision 12).
  - `Export` records `people.person.exported` (`Level: sensitive`, `Fields: ["all"]`) before writing the body; a self export too (the export reaches sensitive data).
  - `Search` records `people.search.read` per result that matched a personal field.
  - `ListRequests` records one `people.request.read` per returned request that carries a note and is not the actor's own (`Subject` the requester), after loading and before returning, outside any `Read` transaction (D37, D38 (u)).
  - A `Record` error fails the use case (audit decision 3).
- **Export without a row (D37).** If there is no `core.person` row but `Accounts.EmailOf` finds an account for the ID, `Export` still calls identity, authz and each module and returns a document with `person` absent; `NotFound` only when no account exists either.
- **Transactions.** `UpdateMe`, `Create`, `Update`, contacts, consents, requests: one `Write` transaction each, publishing inside it. `Get`, `Search`, `Me`: plain reads, no transaction. `Export`: no transaction; identity, authz and each module in build order are called in turn; a failing source fails the export (a partial export would be mistaken for complete).
- **Deletion, step by step** (`Delete`, `FulfilRequest`, `DeleteCommand`): take a `pg_advisory_xact_lock` on `hashtext('people.delete:' || person)` so two deletions of one person serialise; load the person (`NotFound`, unless the person has no row but `Accounts.EmailOf` finds an account for the ID: then the module, authz and identity steps below still run, people's own deletes are no-ops and no `people.person-deleted` is published, D37);
  - for each `Source` in build order call `DeletePerson` with the transaction's ctx (nested `Transact` joins, db decision 6), wrapping an error with the module name;
  - `Access.DeletePerson` (`Conflict last_access_admin` ends it); `Accounts.DeletePerson` (`Permission` for a superuser account ends it);
  - delete requests, consents, contacts, the person row (a foreign-key violation from a module that forgot a table becomes `Conflict in_use` naming the constraint);
  - `AuditCount.CountAbout` for `Kept`; record `people.person.deleted` (`Reason` token, `Record` ref the person) and `people.request.fulfilled` when applicable; publish `people.person-deleted`; commit.
  - The report is returned to the caller and never logged (counts only on the span). Idempotency: a second call answers `NotFound`.
- **Linking, step by step.** Transaction 1: load person (`NotFound`), `Conflict has_account` if `EmailOf` succeeds; `Accounts.LinkAccount(email, person)` moves the account's `person_id` (identity records `identity.account.linked`) and returns the account ID and the orphan (the person ID the account pointed at before); clear the person's `contact_email` (the account's address replaces it; the audit entry's `Fields` lists `contact_email`, D37); record `people.person.linked`; commit.
  - Transaction 2 (best effort, logged on failure): delete the orphan row only if its `playa_name`, `legal_name`, `phone`, `pronouns` are empty and it has no contacts, consents or requests, and the delete does not hit a foreign key; on success publish `people.person-deleted` for the orphan and set `OrphanDeleted`.
  - The session of the linked account keeps working: identity re-reads the account row on every request (identity decision 2), so the next request carries the new person ID; the shell refreshes the session on the next load.
- **Sections.** `Get` and `Me` call every `SectionProvider.Sections(ctx, [person])` sequentially under `SectionTimeout` each, in build order, on a ctx derived from the request's (the provider sees the viewer); an error or timeout yields `Section{Key: "<module>.unavailable", Failed: true}`, a warn log line (module name, kind, no IDs beyond the person's) and `bms.people.sections{module, outcome=failed}`. Sections never appear in `Search` or `NamesOf`.
- **Me and the first-sign-in prompt (P-W1, decisions 34 to 36).** `Me`, in order: the actor (F3); the session from ctx, which a signed-in actor must have (else `Internal`, F104); the row (F4); decide whether a prompt is due (writable copy, actor not the superuser, row has no name); build the sections and the profile; and only after the build succeeds, when due, `MarkNamePrompted` in a `Write` transaction of its own (not inside the read: a change never records inside a `Read` transaction).
  - A failed build returns its error and leaves no mark (F105).
  - `db.ErrReadOnly` from the mark is "cannot record": the profile is answered with `FirstPrompt false` and no error (F96); any other store kind fails `Me` (F97).
  - The mark is the conditional `UPDATE core.person SET name_prompted_at = GREATEST($now, $issued) WHERE id = $person AND playa_name = '' AND (name_prompted_at IS NULL OR name_prompted_at < $issued) RETURNING id`; `FirstPrompt` is true exactly when a row was updated. The statement is the one decider under the row lock, so concurrent calls in one session yield exactly one `true` (F99).
  - A person with no row yet is answered `true` without writing on a writable copy and `false` on a read-only one (F98); the repair comes with their first save (decision 2).
  - The superuser and a named row answer `false` without writing (F95). `Get` of one's own ID never reports or records the prompt (F101).
  - The mark is not a profile change: `updated_at` is untouched, nothing is audited, nothing is published; the column is exported with the person and never cleared. Idempotent per session; nothing retries. The span `people.me` carries `telemetry.Bool("first_prompt", v)`; no metric.
  - A forged cross-site `GET /api/people/me` could at most mark the person prompted, which reveals nothing (the cross-origin guard covers unsafe methods only, api decision 6).
- **Deletion-request notice (D39, decisions 27, 28).** `RequestDeletion`, after the request row and `people.request.created` are written, inside the same `Write` transaction: if `Notify` and `Holders` are non-nil, `Holders.HoldersOf(ctx, "people.data.delete")` (site-wide explicit grant holders,
  superusers excluded; the base role is not enumerated, so a permission only the base role carries yields nobody: authz amendment, D40), then `Notify.Send(Message{Template: "people.deletion-requested", To: holders, Key: "deletion-requested:" + requestID, Link: "/people/admin/requests"})` with no
  `Vars`.
  - The body says only "A deletion request is waiting"; it names no person and carries no note.
  - No holders: nothing is sent and one info log line records the count (never a person). `Send` answering `Unsupported` (e-mail not configured) or a nil `Notify` means "no e-mail": one info log line, the request still succeeds.
  - Delivery (implementation, 2026-10-19, accepted D40): the notice sends in batches of `notify.MaxRecipients` under one `Key`. `Notify.Configured` is probed once before the loop, and the whole notice is skipped (one info log line) if it is off. An `Unsupported` partway through the loop rolls the transaction back as `Unavailable`. Any other `Send` or `HoldersOf` error fails the request with its kind and rolls the transaction back (nothing is half-recorded).
- **Linked sources (D39, D44).** `app`'s `link` registers `Source{Name: "notify", Exporter: a.notify}` (notify's messages without bodies or addresses) and `Source{Name: "files", Exporter: adapter}` after the modules, so their `ExportPerson` and `DeletePerson` run inside people's transaction like a
  module's. The export document gains `"files": {...}` (files.md Behavior: `about` with content, `uploaded` metadata only); the deletion report's `Modules["files"]` carries `Anonymized` (the `uploaded_by` references cleared). A module's `DeletePerson` must delete its files whose subject is the
  person; otherwise files' `DeletePerson` answers `Conflict in_use` naming the module and the whole deletion rolls back (F70). The bytes leave the store within the hour (`files.reap`); the operator note for `bms delete` is carried to `docs/operating.md` (not yet written).
- **Legal names (D44, D46).** `LegalNamesOf` returns an empty map with no store call and no audit when the regional's legal-name mode is hidden (F72). Reach follows decision 9 (D45): a site extent answers for every ID; a department-scoped extent answers for nobody else while no `people.membership`
  provider is registered (empty map, no audit, no `Permission`); reach never excludes the actor (D46). The caller's relation check is the further scope. Under decision 7, per-field visibility choices, when people asks for them, are `privacy` groups (privacy-tags Q-PRIV2: later).
- **Membership (D44).** With providers registered, a department-scoped extent reaches only the people the providers report as members of that department; a provider failure fails the call closed with its kind; `Search` over-fetches up to `MaxSearchResults` and filters, so pages can be short (F71).
- **Read-only copy.** `Me`, `Get`, `Search`, `NamesOf`, `ConsentOf`, `PlayaNameOf`, `Export`, `MyRequests`, `ListRequests` work (audit entries spool). Every write fails `Unavailable` wrapping `db.ErrReadOnly` (`503 read_only`); the account-created handler's insert fails the same way and is counted as `read_only` by signals.
- **Cooldown.** Self exports: an in-memory map person → last export time, bounded by `MaxTrackedExports` (oldest evicted), per process; within `ExportCooldown` the call fails `Unavailable` wrapping `httpx.ErrRateLimited` (429, `Retry-After`). Admin exports and the command are not limited.
- **Playa name (decisions 29 to 32).** On every write path that sets `playa_name` (`UpdateMe` including its repair insert, `Create`, `Update`) the service computes `NameKey` and the store writes both columns in the same statement.
  - **Permanence** is checked under the row lock `UpdateMe` already takes (`GetForUpdate`): if `cur.PlayaName != ""` and the input's key differs from the stored `playa_name_key` (the stored key, not a recomputation, so a backfilled row compares against what the index holds), the call fails `Invalid`
    `playa_name` `permanent` before any write; an equal key with a different spelling is written and audited (`Fields [playa_name]`); an identical string is dropped by the existing "nothing changed" path, so the form can send the whole profile and a no-op save neither writes nor audits. `Update` by
    a profile manager does not apply the permanence rule (decision 30): a different key is written and audited with `Reason "rename"`, whoever the person is; clearing a set name is `Invalid required` either way.
  - **Uniqueness** has no pre-check: the write runs and a `23505` whose `*db.Error.Constraint` is `person_playa_name_key_idx` is re-kinded by the store to `Conflict` with `validate.FieldError{Field: "playa_name", Code: "taken"}` (the api mapper emits it as `fields`, as identity's sign-up `taken`,
    D31); any other `23505` stays a plain `Conflict`. Two people claiming the same key at once: both transactions reach the index, the second blocks on the first's uncommitted row and fails `taken` when it commits (or wins if it rolls back), so the index is the only serialisation needed and no
    advisory lock is taken. The transaction rolls back, so no `people.person.created|changed` entry and no `people.person-created` signal survive a clash.
  - **Deletion** removes the row and with it the index entry (decision 32); the orphan cleanup after `LinkAccount` deletes only nameless rows, so it never frees a name.
  - **Never logged:** the key is treated as the name (`Person.LogValue` stays ID-only; `telemetrytest` asserts no key in logs or spans).
  - `peopletest.Fake` gains `SetName` and `RenameByManager` (an accepted deviation, D51; the spec had said the fake is unchanged), which apply the permanence and uniqueness rules in memory; `Fake.Add` still does not enforce uniqueness, so a module test that seeds two people with one key is seeding a state P1 makes impossible and should use distinct names.
- **Concurrency.** The service holds no mutable state beyond the cooldown map (mutex) and the `Link` slices (set once). Profile updates are last-write-wins (a profile is edited by one person at a time in practice; an `If-Match` precondition can be added without a migration). The one cross-person race, two claims of one playa name, is decided by `person_playa_name_key_idx` (above).
- **Lifecycle.** `New` subscribes; `Link` once; nothing to start or stop; no goroutines.
- **Telemetry.** A span per use case (`people.<name>`) with `telemetry.ID("person_id")` and `telemetry.Token` outcomes (`people.me` also `first_prompt`); `people.section` per provider with the module token; metrics above; never a name, address or phone on any of them (`telemetrytest` asserts).

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

`identity` and `access` are the raw messages those services return; `modules` holds each module's `Export.Data` by name (an empty object for a module holding nothing). Audit rows are not included (audit "Export of a person").

**Deletion report** (JSON and the command's lines): `{"person": "...", "people": n, "identity": n, "access": n, "modules": {"<name>": {"deleted": n, "anonymized": n, "kept": [...]}}, "kept": [{"kind": "audit.entry", "count": n}]}`.

**Delivery by milestone.** M2: everything above, including the commands (09 §11 moves "basic" export and deletion here; decision 19). M4: `ChangeEmail` on the profile (identity), notify on `people.deletion-requested`, retries of the account-created handler through `jobs`, the `people.retention.unused` setting and job. Later: `ViewsOf` on the profile (D22), merging two people, a department-scoped extent for contacts (volunteers' membership contract), an export request kind.

### Accepted implementation deviations (M2 build, 2026-10-18)

- **Leaf package.** `people/rows`, aliased as `ContactID` and `RequestID`.
- **Extra exports.** `PublishedSignals`, `SubscribedSignals`, `ParseRequestStatus`, `RequestStatus.String` and the `Field` constants. `personpage.SectionTimeout` is also exported.
- **Profile updates.** `Update` on your own ID delegates to `UpdateMe`, except that a holder of `people.profiles.manage` changing their own set playa name is a manager rename (decision 30, F78). `Create` and `Update` return a profile built like `Get`'s, with a read audit when the viewer holds `people.contacts.read`. The section timeout is a real context deadline.
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
| F55 | `SetConsent` | granted or withdrawn | nil; audit `Reason` is `granted:<purpose>`/`withdrawn:<purpose>`, `Fields` `[purpose]` (D37, D38 (s)). | none | `TestSetConsent_RecordsPurposeAndDirection` |
| F56 | `LinkAccount` | person had a `contact_email` | nil; `contact_email` cleared (D37) | none | `TestLinkAccount_ClearsContactEmail` |
| F57 | `ListRequests` | a returned request has a note | nil; one `people.request.read` per returned request with a note, `Subject` the requester; `Record` error fails the read (D37, D38 (u)) | none | `TestListRequests_WithNote_RecordsRead` |
| F58 | `Export`, `Delete` | no person row, an account exists | nil; identity, authz and module steps run (D37) | none | `TestDelete_AccountWithoutPersonRow_RunsIdentityAndAuthz` |
| F59 | `Delete` | the person held grants | nil; authz publishes `access.grant-revoked` per grant (D37) | none | authz's F66 test `TestDeletePerson_PublishesGrantRevokedPerGrant` (`internal/core/authz/integration_person_test.go`); people's own tests use a fake `Access` |
| F60 | `RequestDeletion` | `Notify` or `Holders` nil, or `Send` answers `Unsupported` | nil; the request is created; one info log | nothing | `TestRequestDeletion_NoEmail_RequestStillCreated` |
| F61 | `RequestDeletion` | `HoldersOf` returns nobody | nil; request created; nothing sent; one info log | nothing | `TestRequestDeletion_NoHolders_NothingSent` |
| F62 | `RequestDeletion` | `HoldersOf` or `Send` fails other than `Unsupported` | its kind; the transaction rolls back; no request row | retry | `TestRequestDeletion_NotifyFails_RollsBack` |
| F63 | `RequestDeletion` | success with holders | the holders are `To`, `Key` is the request ID, `Link` is `/people/admin/requests`, no `Vars` (asserted from `notifytest.Fake.Sent()`) | nothing | `TestRequestDeletion_SendsNoticeToHolders` |
| F64 | `RequestDeletion` | the request is a duplicate | `Conflict`; nothing sent | show the open one | `TestRequestDeletion_Duplicate_NothingSent` |
| F65 | `LanguageOf`, `ContactEmailOf` | zero person; no row or no address | Invalid; NotFound | notify uses the site default / reports `no_address` | `TestLanguageOf_Table`, `TestContactEmailOf_Table` |
| F66 | `Settings` | `emergency_contacts` set to `required` | Invalid (`people.emergency_contacts`, `one_of`) | settings refuses the change | `TestSettings_EmergencyContactsRequired_Invalid` |
| F67 | `LegalNamesOf` | the actor lacks `people.contacts.read` in every extent | Permission | show forbidden | `TestLegalNamesOf_NoContactsRead_ReturnsPermission` |
| F68 | `LegalNamesOf` | more than `MaxNames` IDs | Invalid | batch | `TestLegalNamesOf_TooManyIDs_ReturnsInvalid` |
| F69 | `LegalNamesOf` | a person without a legal name | (omitted from the map, no audit entry for them) | treat as absent | `TestLegalNamesOf_NoLegalName_OmitsAndDoesNotAudit` |
| F70 | `Delete` | a module left a file about the person | Conflict (`in_use`, naming the module) | fix the module's `DeletePerson` | `TestDelete_ModuleLeftFile_ReturnsConflictInUse` |
| F71 | `Get`, `Search` | a department-scoped extent and providers present | (only members of the department are reached); `Get` answers `NotFound` for a person outside a department-scoped directory extent; a provider failure fails the call closed with its kind. `Search` over-fetches up to `MaxSearchResults` and filters, so pages can be short (known limitation; a later contract method `MembersOf` could filter in SQL) | nothing | `TestSearch_DepartmentExtent_ReachesOnlyMembers` |
| F72 | `LegalNamesOf` | the regional's legal-name mode is hidden | (empty map; no store call, no audit) | treat as absent | `TestLegalNamesOf_LegalNameHidden_ReturnsNoneAndAuditsNothing` and `TestLegalNamesOf_LegalNameHidden_ReturnsNoneAndAuditsNothing_Integration` |
| F73 | `Get`, `Search`, `LegalNamesOf` | the actor holds `people.contacts.read` only in department scope (a `department-admin`) and no `people.membership` provider is registered (D45) | `Get`: internal fields only for others, no personal fields, no audit entry; `Search`: no personal-field match (no error, no leak), no `people.search.read` entry; `LegalNamesOf`: empty map for others, the actor's own name still answered (D46), no audit, no `Permission` (the permission is held) | render what came | `TestGet_DepartmentContactsReadNoProvider_OmitsPersonalFields`, `TestSearch_DepartmentContactsReadNoProvider_NoPersonalMatch`, `TestLegalNamesOf_DepartmentContactsReadNoProvider_ReturnsSelfOnly` |
| F74 | `UpdateMe` (first name, or the repair insert), `Create` | another person holds the same `NameKey` (the store sees `23505` on `person_playa_name_key_idx`) | Conflict (`playa_name` `taken`); the transaction rolled back; no audit entry, no signal | show it under the field; say nothing about who | `TestUpdateMe_NameTaken_ReturnsConflict`, `TestCreate_NameTaken_ReturnsConflict`, `TestUpdateMe_NameTaken_NothingAuditedOrPublished` |
| F75 | `UpdateMe` | a name is set and the input's key differs from the stored key | Invalid (`playa_name` `permanent`); nothing written | the field is read-only, so only a stale client sees it | `TestUpdateMe_ChangeSetName_ReturnsInvalidPermanent` |
| F76 | `UpdateMe` | a name is set and the input is the same string | nil; nothing written, nothing audited | none | `TestUpdateMe_SameName_NoOp` |
| F77 | `UpdateMe` | a name is set and the input has the same key but another spelling | nil; `playa_name` written, key unchanged; `people.person.changed` with `Fields [playa_name]` | none | `TestUpdateMe_RespellSameKey_Saves` |
| F78 | `Update` (profile manager, any person including themself) | a name is set and the input's key differs | nil; written; `people.person.changed` with `Fields [playa_name]`, `Reason "rename"`; the `taken` rule applies (F74) | confirm dialog, then the result | `TestUpdate_RenameByManager_SavesAndAuditsReason`, `TestUpdate_RenameOwnNameByManager_SavesAndAuditsReason` |
| F79 | `Update` (profile manager) | a name is set and the input is `""` | Invalid (`playa_name` `required`) | field error | `TestUpdate_ClearSetName_ReturnsInvalidRequired` |
| F80 | `UpdateMe` × 2 (integration) | two people claim the same key at once | exactly one nil and one Conflict (`taken`); no deadlock; one row holds the key | the loser chooses another | `TestUpdateMe_NameRace_OneWins` |
| F81 | the store | a `23505` on an index other than `person_playa_name_key_idx` | Conflict without a field (as before) | 409 | `TestStore_OtherUniqueViolation_PlainConflict` |
| F82 | `NameKey` | `" Dusty "`, `"DUSTY"`, `"dusty"`, `"Dusty Roads"`, `"Dusty  Roads"`, `"Dusty\tRoads"`, `"é"` precomposed and decomposed, a fullwidth `"Ｄusty"`, `""`, `"   "` | the first three share one key; the three two-word forms share one key; the two "é" forms share one key; the fullwidth form equals `"dusty"`; the last two give `""`; never an error | | `TestNameKey_Table` |
| F83 | `UpdateMe` | body sets only `language` while a name is set | nil; language saved (F54 stands) | none | `TestUpdateMe_LanguageOnly_IgnoresPermanence` |
| F84 | account-created handler, then a second account | two nameless rows | nil; both inserted (empty keys are outside the partial index) | none | `TestHandleAccountCreated_TwoNamelessRows_BothInserted` |
| F85 | migration `0003` (integration) | two rows whose backfilled keys are equal | the migration fails with an error naming the count and the person IDs (no name); set version stays 2; no column, no index | the operator resolves the clash on the previous version | `TestMigration0003_Duplicates_FailsLoud` |
| F86 | `Delete`, then `UpdateMe` by someone else | the deleted person's name | nil for the second claim (the name is free, decision 32) | none | `TestDelete_FreesPlayaName` |
| F87 | migration `0003` (integration) | server encoding is not UTF-8 | the migration fails naming the encoding; nothing applied | the operator fixes the database | `TestMigration0003_NonUTF8_Fails` (skipped where the container cannot create such a database; the SQL is asserted to contain the check) |
| F88 | migration `0003` (integration) | `lower()` cannot fold non-ASCII (`LC_CTYPE=C`, or an ICU-provider database where `lower(E'İ') <> 'i'` or `lower('ΑΣ') <> 'ασ'`) and an existing playa name holds non-ASCII (D51) | the migration fails naming the locale; nothing applied | the operator uses a UTF-8 locale such as `C.UTF-8` or `en_US.UTF-8` | `TestMigration0003_SQLChecksUTF8` asserts the guard is in the SQL; the failing case is skipped where the container cannot create such a database |
| F89 | `Delete`, `FulfilRequest`, `DeleteCommand` | identity refuses with `Permission` `protected` (a shared demo account) | Conflict `protected`; nothing deleted | message | `TestDelete_ProtectedAccount_ReturnsConflict_RollsBack` |
| F90 | `LinkAccount` | identity refuses with `Permission` `protected` (a shared demo account) | Conflict `protected`; nothing linked | message | `TestLinkAccount_ProtectedAccount_ReturnsConflict` |
| F91 | `Me` | no name, first read of this session (column NULL or before `IssuedAt`) | nil; `FirstPrompt true`; column = `GREATEST(now, IssuedAt)`; `updated_at` untouched; nothing audited | send the person to `/people/me?first=1` | `TestMe_Unnamed_FirstReadPrompts`, `TestMe_Prompt_NothingAudited` |
| F92 | `Me` | no name, a second read in the same session | nil; `FirstPrompt false`; no write | nothing | `TestMe_SecondReadSameSession_NoPrompt` |
| F93 | `Me` | no name, a later session (`IssuedAt` after the column) | nil; `FirstPrompt true` once more | prompt once | `TestMe_NewSession_PromptsAgain` |
| F94 | `Me` | no name, an older session reads after a newer session was prompted | nil; `FirstPrompt false` | nothing | `TestMe_OlderSession_AfterPrompt_NoPrompt` |
| F95 | `Me` | the row holds a name, or the actor is the superuser | nil; `FirstPrompt false`; no write | nothing | `TestMe_Named_NoPromptNoWrite`, `TestMe_Superuser_NoPromptNoWrite` |
| F96 | `Me` | the mark fails `Unavailable` wrapping `db.ErrReadOnly` (read-only copy) | nil; the profile answered; `FirstPrompt false` | nothing (no loop) | `TestMe_ReadOnly_NoPromptNoError` |
| F97 | `Me` | the mark fails with another store kind | that kind (`Unavailable`, `Timeout`, `Canceled`); no profile | `ErrorState`; the frame never blocks | `TestMe_MarkFails_ReturnsStoreKind` |
| F98 | `Me` | no row yet (decision 2): writable copy / read-only copy | nil; `FirstPrompt true` without writing / `false` | prompt (leads to the repairing save) / nothing | `TestMe_NoRow_PromptsWithoutWriting`, `TestMe_NoRow_ReadOnly_NoPrompt` |
| F99 | `Me` × 2 (integration) | two reads of one session at once | exactly one `true`; one row, one write | prompt once | `TestMe_ConcurrentSameSession_OnePrompt` |
| F100 | `Me` | `now` is before `IssuedAt` (a copy whose clock lags the one that signed the person in) | nil; `FirstPrompt true`; column = `IssuedAt`, so the next read is F92, not F91 | prompt once | `TestMe_ClockBehindIssuedAt_StillOnce` |
| F101 | `Get` (own ID) | no name, prompt due | nil; `FirstPrompt false`; no write | nothing | `TestGet_Self_NeverPrompts` |
| F102 | `New` / `Options.Validate` | `Session` nil | Invalid (field `people.session`) | fix the wiring | `TestOptions_NilSession_ReturnsInvalid` |
| F103 | migration `0004` (integration) | applied on a version-3 set | version 4; the column NULL for every row; `TestMigrations_Apply` moves to 4 | | `TestMigrations_Apply` |
| F104 | `Me` | a signed-in actor with no session in ctx (D58 review) | Internal (wiring error) | fix the wiring | `TestPromptName_NoSessionForAnActor_IsInternal` |
| F105 | `Me` | the profile build fails after a prompt was found due (D58 review) | the build's error; no mark is written, so the next `/me` still prompts | retry | `TestMe_BuildFails_LeavesNoMark` |

`MyRequests`, `ListRequests` (beyond permission), `ConsentOf` (beyond F23), `Routes`, `OpenAPI` have no further modes: fixed reads or pure. `Purpose.Owner`, `FieldMode.String`, `Person.LogValue` cannot fail (pure).

## Lock-in check

No external technology beyond PostgreSQL through `db` (D8). `text_pattern_ops`, partial unique indexes and `pg_advisory_xact_lock` are in every OSI-licensed PostgreSQL ≥ 16 (db decision 20); no `pg_trgm` or other extension is required. No cloud directory, no identity provider. `normalize(text,
NFKC)` and `regexp_replace` are core PostgreSQL (≥ 13, UTF-8 databases) and are used once, in migration `0003`'s backfill; `golang.org/x/text/unicode/norm` (BSD-3, already in `go.mod` indirectly) becomes a direct dependency for `NameKey`.

## Test plan

- **Unit** (`internal/core/people`, no infrastructure): a hand-written fake store; `clock.NewFake`; `audittest.NewFake("people")`; `signalstest.NewFake("people", {person-created, person-deleted, deletion-requested}, {identity.account-created})` with `Deliver` driving the handler;
  `authztest.NewFake()` with `Grant`/`GrantIn`/`Superuser`; fakes for `Accounts`, `Access`, `AuditCounter`, `PolicySource`, `PersonData` sources and `personpage.Provider` (one that fails, one that blocks until its ctx ends, so the timeout path runs without real time); `telemetrytest.New(t)`
  asserting no name, address or phone appears in logs, spans or the recorder's `Reason`/`Fields`. Every F row; the visibility matrix (self / directory.read / contacts.read / manage / superuser × each field group); the policy matrix (hidden / optional / required × own save / admin save).
- **Permission tests:** each use case has `_NoPermission_ReturnsPermission` (or `_NotFound` where the record must stay invisible); `Search` proves no personal-field match leaks without `people.contacts.read`. A `department-admin` holding `people.contacts.read` only in department scope, with no `people.membership` provider, sees no personal fields, no personal-field `Search` matches and no `LegalNamesOf` results for others (F73, D45).
- **Integration** (`//go:build integration`, `dbtest.TempDB`): migrations apply on an empty database; the deletion transaction across a fake module table with a foreign key (F37, F34 rollback leaves every row); the open-request partial index under two concurrent requests; the advisory lock under two concurrent deletions (one `NotFound`); keyset-free search ordering; read-only paths with `SetReadOnly`.
- **Conformance:** `peopletest.Fake` answers the same `NamesOf`/`ConsentOf` cases as the service (`TestFake_MatchesService`); `moduletest.Conformance` supplies it as `Deps.People` (Proposed change 2).
- **app end-to-end** (Proposed change 1): sign up → `GET /api/people/me` is empty and incomplete → `PUT /me` with a playa name → `GET /api/accounts/session` carries it; the account admin creates a person, links a second sign-up's account to them, the orphan is removed; `bms export <id>` prints the
  document with `hello` inside; the admin deletes a person who saved a hello greeting and the report counts it; `bms delete` on the last access admin's person exits 1 with `last_access_admin`; the profile screen and the person page render at the phone viewport.
- **Web** (`web/src/core/people/`): component tests for every screen's loading, empty, error, forbidden and read-only states and the first-sign-in banner; axe checks; the Playwright smoke run adds "fill in the playa name, see it in the header" and "search a person, open their page" at the phone viewport.
- **Playa name.** Unit: `TestNameKey_Table` (F82); `UpdateMe`, `Create`, `Update` with the fake store answering the index violation (F74 to F81, F83, F84); `telemetrytest` asserting no key appears in logs, spans or audit `Fields`/`Reason`. Integration (`dbtest.TempDB`): migration `0003` on an empty
  database, on a database with distinct names, and on one with two rows sharing a key (F85: the error names the IDs and the set stays at version 2); `TestNameKey_MatchesMigrationBackfill` runs the F82 table through the SQL expression and `NameKey` and asserts equality;
  `TestUpdateMe_NameRace_OneWins` (F80: two people, same key, started together; exactly one `Conflict taken`, no deadlock, one row holds the key); `TestDelete_FreesPlayaName` (F86). Web: the field is read-only once set with its explanation; the `taken` error renders inline under the field on My
  profile, New person and the manager's edit form; the first-sign-in banner's new wording; the smoke run adds "a second account tries the first one's playa name and sees the taken message" at the phone viewport.
- **Notice (D39).** The notice tests use `notifytest.NewFake("people")`, a fake `Holders` and `telemetrytest.New(t)` asserting no person ID of a requester or note text appears in the message or any log; the Mailpit end-to-end run files a request and reads the notice.
- **First-sign-in prompt (P-W1, decisions 34 to 36).** Unit: F91 to F98, F100 to F102, F104 and F105 with a fake store and contexts built with `identity.WithSession` and `identity.SessionFrom`, or a closure returning a fixed `IssuedAt`; every `Options` fixture sets `Session`. Integration: F99 and
  F103 (`TestMigrations_Apply` expects version 4). End-to-end: reload after Continue: no second prompt; sign in again: one prompt. Web (`web/src/shell/me.test.tsx` or `people.test.tsx`): "a fresh mount whose `/me` answers `first_prompt: false` never navigates" (the reload case), "`first_prompt:
  true` navigates once with `next`, and not again on the next route change", "a sign-out then a new sign-in acts on the new answer", "nothing is written to `sessionStorage` or `localStorage` except `bms.lang`" (storage spies). The `people.test.tsx` cases that mount with an empty `playa_name` set
  `first_prompt: true` in the fixture; the test "sends a session with no playa name to `/people/me?first=1` once" asserts the flag, not the name.
- **Fixtures (P1).** Every integration seed that inserts several people gives them distinct names (the index refuses a repeat); `dbtest` fixtures that insert `core.person` rows directly also write `playa_name_key` (or insert through the store); the app end-to-end profile flow adds "a second account tries that name and sees the taken message"; web `MyProfile` tests cover the read-only field once set, `taken` inline and the new banner wording.

## Amendments

Amendment narratives (D39, D44, D45, D46, P1 (D48, D51), D53, P-W1 (D57, D58)), their reasons, review findings and superseded wording are in `plan/design/history/people.md`. The current rules are stated in the sections above.

## Open questions

None. Q-PEOPLE-8 to Q-PEOPLE-10 and the earlier questions are answered and folded into decisions 2, 5, 8, 9, 10, 19, 20 to 25, 30, 34 and 36; the record is in `plan/design/history/people.md`.

## Proposed changes elsewhere

Status as of 2026-10-30: items 1 to 17, 19 to 23 and 26 to 30 are applied (17: the redelivery part, 2026-10-20, D43); item 25 needs no change; items 18 and 24 (`docs/operating.md`) are applied 2026-10-30.

1. (applied 2026-10-18, in app.md, row 5 of "How later core services plug in") **`plan/design/app.md` (row 18) and `internal/app`:** part 5 `people` in `core_people.go`: `declare` appends `people.Set` to `coreSets` (set name `people`, already in db's fixed order), `people.Permissions` to
   `corePerms`, the three signal names to `coreSignals`, `module.ExtensionPointDecl{Name: personpage.Point, Accepts: func(v any) bool { _, ok := v.(personpage.Provider); return ok }}` to `corePoints`, and the commands `export` (`usage: <person-id>`) and `delete` (`usage: <person-id>`), both
   requiring the session key like `create-superuser` and running `ExportCommand`/`DeleteCommand` after `Build` and `Link`; `build` calls `people.New` over `a.dbc`, the clock, telemetry scope `people`, `a.deps.audit.Scoped("people")`, `a.disp.Scope("people", {the three},
   {identity.SignalAccountCreated})`, `a.deps.authz`, the authz actor closure, `Accounts: a.identity`, `Access: a.authz`, `AuditCount: a.audit`, `Policy: nil` (settings' part sets a lazy adapter when it lands), and sets `a.deps.people = svc` and route set `people`; identity's `build` passes `Names:
   lazyNames{a}` (empty until people's part sets `a.names`); `link` converts `a.set.PersonData()` to `[]people.Source` and `a.set.Contributors(personpage.Point)` to `[]people.SectionProvider` and calls `Link`. Mark Proposed change 10 applied for people. The e2e test adds the steps in "Test plan".
2. (applied 2026-10-18, in module-contract.md) **`plan/design/module-contract.md` (row 4), `internal/core/module`, `moduletest`:** add `Deps.People people.Directory` (M2; unscoped; `Provision` sets it); `moduletest.Conformance` sets `Deps.People = peopletest.NewFake()` when nil; mark Proposed
   change 8 applied (`Env.CoreExtensionPoints` carries `people.person-page`; `Env.CoreSignals` the three names; `Set.Contributors` consumed by people through `app`); add the rule for module authors: "a foreign key to `core.person(id)` is allowed and uses `ON DELETE RESTRICT`; `people` calls your
   `DeletePerson` first".
3. (applied 2026-10-18, in identity.md, which gains `EmailOf`, `LinkAccount`, the superuser refusal in `DeletePerson` and the profile link) **`plan/design/identity.md` (row 6) and `internal/core/identity`:** add `EmailOf(ctx, model.PersonID) (model.Email, error)` (`NotFound` without an account; no
   permission check, documented as people's seam) and `LinkAccount(ctx, email model.Email, person model.PersonID) (model.AccountID, model.PersonID, error)` returning the account and its previous person (`NotFound` field `email` code `unknown`; `Conflict superuser`; `Conflict has_account` when
   `person` already has an account; audited `identity.account.linked` with `Record` the account, `Subject` the person; no `account-linked` signal); `DeletePerson` fails `Permission` for a superuser account (decision 7's "never changed through the API"); no foreign key from `core.account.person_id`
   to `core.person` (identity creates the account first; people's `0001` backfills a person row per existing account, the one migration-time read of `core.account` by another set); mark Proposed change 7 applied (the subscription, `NameSource`, the prompt, the login address, export and deletion
   through `app`'s adapter, `LinkAccount` as the "link an existing person" method); the accounts admin screen (`/accounts/admin/:account`) shows a "Profile" link to `/people/<person>`.
4. (applied 2026-10-18, in authz.md) **`plan/design/authz.md` (row 7):** mark Proposed change 9 applied: (a) `ExtentOf` any-extent checks, (b) `Access` seam and the `Conflict last_access_admin` rollback, (c) `GET /api/people/search` behind `people.directory.read` and `GET /api/people/names` for any
   signed-in person, (d) the "Roles" link, (e) `StarterRoles` as in the Permissions table (contacts in `account-admin` only, decision 9), (f) authz migration `0002`: `core.role_grant.person_id → core.person(id)` ON DELETE RESTRICT and `granted_by → core.person(id)` ON DELETE SET NULL, (g)
   `NameSource` unchanged; decision 14's "authz does not verify that a person exists" becomes "the foreign key does, from `0002`"; `authz.Permissions` is unchanged.
5. (applied 2026-10-18, in audit.md) **`plan/design/audit.md` (row 12):** mark Proposed change 5 applied: the actions and `Fields` listed here; `people.person.exported` at level `sensitive`; `CountAbout` is consumed in M2 (not M4); the person page links to `/audit?subject=<person>` for
   `audit.search` holders instead of calling `GET /api/audit/people/{person}/views` from people's folder (web isolation; the export button stays on the audit screen); the audit screen's subject filter accepts `?subject=` from the URL.
6. (applied 2026-10-18, in signals.md) **`plan/design/signals.md` (row 10):** mark Proposed change 8 applied for people: `Env.CoreSignals` gains `people.person-created {person}`, `people.person-deleted {person}`, `people.deletion-requested {request, person}`; the core's scope `people` subscribes to `identity.account-created`.
7. (applied 2026-10-18, in web-shell.md) **`plan/design/web-shell.md` (row 17) and `web/src/shell`:** mark Proposed change 13 applied: after a session is established the frame calls `GET /api/people/me` once, applies `language` (mirrored to `localStorage["bms.lang"]`), and sends a session with an
   empty `playa_name` (not the superuser) to `/people/me?first=1` once per session; a language change by a signed-in person is written with `PUT /api/people/me {language}`; decision 3 lists `web/src/core/people/` as a core area; the smoke run adds the two flows in "Test plan"; `@/api` exports a
   `useMe()` hook limited to `core/people` and the frame.
8. (applied 2026-10-18, in module-hello.md) **`plan/design/module-hello.md` (row 4b) and `internal/modules/hello`:** migration `0002` adds `FOREIGN KEY (person_id) REFERENCES core.person(id)` ON DELETE RESTRICT (decision 9's deferral ends); `Manifest.Subscribes` may add `people.person-deleted` to reset the counter (optional; the deferred subscription); the e2e export shows `hello` inside the document.
9. (applied 2026-10-18, in model.md) **`plan/design/model.md` (row 2):** mark Proposed change 5 applied for people: `contact_email` stored as entered, searched by `Canonical()`, not unique (two account-less people may share a household address).
10. (applied 2026-10-18, in db.md) **`plan/design/db.md` (row 5):** mark Proposed change 10 applied for people (`postgres.Migrations`, `people.Set`, schema `core`, set name `people`); note the migration-time backfill from `core.account` (Data, "Backfill") as the one read across sets.
11. (applied 2026-10-18, in 07-abstractions.md) **`plan/07-abstractions.md`, "people":** Owns becomes "profiles (playa name, pronouns, language; legal name, phone, a contact address for people without an account), emergency contacts, consents by purpose, the person row behind every account
    (`identity.account-created`) and account-less people with `LinkAccount`, name lookup and search, the person page with `people.person-page`, export and deletion across identity, authz and every module (`PersonData`) with deletion requests, the `export` and `delete` commands, the `people` routes
    and screens, signals `people.person-created|deleted|deletion-requested`"; Doesn't do adds "the login address (`identity`), who may see a profile (`authz`), the person's own view of who read their data (`audit`, Later), department membership (`volunteers`)". Add the contract
    `contracts/personpage`.
12. (applied 2026-10-18, in 09-core-features.md) **`plan/09-core-features.md`:** §3 "plus the person-page extension point in `contracts/people`" becomes "`contracts/personpage`"; the milestone table's M2 row gains "download your own data, request deletion, export or delete one person (screen or command)", the M4 row drops "export or delete one person by command", and §11's "Later" keeps only retention and the export request kind.
13. (applied 2026-10-18, in telemetry.md) **`plan/design/telemetry.md` (row 15b):** add `bms.people.exports{outcome}`, `bms.people.deletions{outcome}`, `bms.people.sections{module,outcome}` to the instrument table.
14. (applied 2026-10-18, in the go-conventions and add-module skills) **`.claude/skills/go-conventions/SKILL.md` and `add-module/SKILL.md`:** `Deps.People` (`NamesOf`, `ConsentOf`; `peopletest.NewFake()` in tests); how to contribute a person-page section (`ContributesTo: personpage.Point`, a
    `personpage.Provider` in `Contributions()`, catalogue keys `module.<name>.section.<ident>.title`, the provider checks the viewer's permission and audits); foreign keys to `core.person` with `ON DELETE RESTRICT`; `DeletePerson` runs inside people's transaction (use the ctx you were given, never a
    new `Transact(Write)` on another ctx).
15. (applied 2026-10-19, D39) **`plan/design/settings.md` (row 15):** declare `people.legal_name`, `people.phone`, `people.emergency_contacts` with the ranges above and implement `people.PolicySource` (`Policy.Validate` is the check); `app` sets the lazy adapter.
16. (applied 2026-10-19, D39, as notify Proposed change 5: people sends the notice itself instead of notify subscribing; see the amendment) **`plan/design/notify.md` (row 11):** tell holders of `people.data.delete` that a request is waiting (no note, no name in the body; a link to `/people/admin/requests`).
17. (applied 2026-10-20, D43, for the redelivery part, by jobs.md Proposed change 7; the `people.retention.unused` proposal stays with `people`, its key would be `people.retention_unused`, declared when `people` registers that job) **`plan/design/jobs.md` (row 14):** redeliver a failed `identity.account-created` handling to people (retryable kinds); carry the `people.retention.unused` proposal (delete account-less people with no module data after a regional-set period; default forever).
18. (applied 2026-10-30) **`docs/operating.md`:** `bms export <person-id>` prints one JSON document to stdout (handle it as personal data); `bms delete <person-id>` deletes across every module and prints the report, refuses the last access admin and a superuser's person, must run on the writing copy, and is audited as the operator; backups still hold the deleted data (privacy-data "Backups contain everything").
19. **`plan/design/files.md` (row 16), `privacy-tags.md` and `module-volunteers.md`** (applied 2026-10-20, D44): the amendment above. The "docs/operating.md" half of files Proposed change 8 is applied 2026-10-30.

Raised by Amendment P1 (2026-10-22, approved 2026-10-23, D48; another architect is revising `module-volunteers.md` in parallel and takes item 20 up there, so it is listed as a dependency, not applied from here):

20. (**applied 2026-10-23**, volunteers Amendment V1 approved, D49) **`plan/design/module-volunteers.md` and `internal/modules/volunteers`:** with decision 29 the per-event check of its decision 26 (`Conflict name_taken` in `SignUp` and `Assign`, the `(event, NameKey)` advisory lock, the `NamesOf`
    scan of everyone booked at the event, F159, F163, `TestSignUp_NameRace_OneWins`) is redundant: two people can no longer hold one key anywhere, so it can be removed and its decision 26 can say "people keeps playa names unique installation-wide (people decision 29)"; `Conflict unnamed` (a person
    with no name yet cannot be booked) stays, since P1 does not require a name to exist; its Proposed change 12 is satisfied by `people.NameKey`, and `vol.NameKey` delegates to it or goes; its UI copy "Change it on your profile, then try again" is wrong under permanence and should say "Set your
    playa name on your profile first" only for `unnamed`; Q-VOL10 and Q-VOL11 are closed by the owner's answers recorded here (a lead cannot override a name that cannot clash; a rename cannot clash). The `peopletest` seed must give every booked person a distinct name (it already does per V1).
21. (applied 2026-10-23, by the main session) **`.claude/skills/burn-domain/SKILL.md`:** replace "Burn names are unique among the volunteers for one event ... are not required to be unique" with: burn names are unique across the whole installation, self-appointed and permanent once set (owner,
    2026-10-22); a name unused for about five years becomes eligible for retirement, and vulgar or otherwise inappropriate names need a retirement process, both to be designed later; approval pipelines for new names are a later option; a person may have several burn names (an alias and a radio
    handle) of which only the handle must be unique, deferred. Keep the "Handle" row as is.
22. (applied 2026-10-23) **`plan/07-abstractions.md`, "people":** Owns gains "the installation-wide uniqueness and permanence of playa names (`NameKey`, `playa_name_key`)"; Doesn't do gains "retiring or approving names (Later)".
23. (applied 2026-10-23) **`plan/design/db.md`** (low priority): state that a UTF-8 server encoding is required (`normalize()` needs it; people `0003` checks it itself, so nothing breaks until `db` adds the check at `Open`).
24. **`docs/operating.md`** (applied 2026-10-30): `bms migrate` stops at people `0003` when two people share a playa name; the message lists their IDs; resolve on the previous version (one of them renames, or a profile manager renames them), then migrate again; a non-UTF-8 database is refused by the same migration.
25. **`plan/design/identity.md`, `plan/design/web-shell.md`:** no change. `NameSource` is a read and the session's `playa_name` is unaffected; the first-sign-in banner is people's own screen and catalogue key (`core.people.me.first`), the shell's `field.taken` message is overridden by people's `core.people.field.taken` the way `last_access_admin` already is, and `field.permanent` lives in people's catalogue too.

Raised by Amendment P-W1 (2026-10-30, D57; Approved 2026-10-30, D58; items 26 to 30 applied 2026-10-30, D58):

26. (applied 2026-10-30, D58, in web-shell.md) **`plan/design/web-shell.md`** (the 2026-10-18 amended line, "Data", and the `useMe` paragraph): the frame sends a person to `/people/me?first=1` when `GET /api/people/me` answers `first_prompt: true`, and on nothing else (the server decides who is
    prompted and records it; the frame's own `playa_name === ""` and superuser tests go); the frame still acts once per answer with in-memory state and still passes `next`; "Data" gains the sentence "the first-sign-in prompt is recorded by the server (people decision 35), never by the browser"; the
    shell's storage rule is unchanged.
27. (applied 2026-10-30, D58, in identity.md) **`plan/design/identity.md`** (Summary, "Used by"): note that `people` reads `Session.IssuedAt` through `SessionFrom` for the once-per-session prompt (people decision 34), so `IssuedAt` must stay the server time at sign-in, fixed for the session's life, and a new session (password change, "sign out everywhere else") carries a new one; no interface change.
28. (applied 2026-10-30, D58, in api.md, no change needed: it states no "GET never writes" rule) **`plan/design/api.md`**: no change expected. If the spec states that `GET` handlers never write, add the exception "`GET /api/people/me` records the first-sign-in prompt mark, idempotent per session and never personal data (people decision 35)"; the cross-origin guard for unsafe methods (decision 6) is unaffected.
29. (applied 2026-10-30, D58, in 07-abstractions.md) **`plan/07-abstractions.md`, "people":** Owns gains "the once-per-session first-sign-in prompt mark (`name_prompted_at`, D57)".
30. (applied 2026-10-30, D58, in app.md) **`plan/design/app.md`** (row 5 of "How later core services plug in"): `core_people.go` sets `people.Options.Session = identity.SessionFrom`.
