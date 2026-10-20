# authz

- **Status:** Implemented (2026-10-18); Approved (2026-10-18; owner approved every recommended answer)
- **Amended 2026-10-18 (M2 build, accepted deviations):** implementation deviations recorded in Behavior, "Accepted implementation deviations (M2 build, 2026-10-18)": leaf packages `authz/perm` and `authz/rows`, wrapped list responses, fail-closed decisions, grant wire fields, migration 0002, new failure mode F64. Records of what was built, not design changes.
- **Amended 2026-10-19 (D39, owner approval of settings and notify):** `(*Service).HoldersOf` added (notify Proposed change 6; F67 to F69); Proposed changes 11 and 12 are applied (starter role for `settings.*` and `notify.*`; "you were appointed" becomes Later).
- **Amendment 2026-10-19 (pending owner approval):** `HoldersOf` returns explicit grant holders only and never enumerates the base role: confirmed, with the reason; F70 and F71 added. See "Amendment 2026-10-19 (pending owner approval): `HoldersOf` and the base role" before Open questions.
- **Package:** `internal/core/authz` (contract and service), `internal/core/authz/postgres`
  (the store, SQL and migrations), `internal/core/authz/authztest` (the fake modules test
  with). **UI:** `web/src/core/access/` (screen set `access`, keys `core.access.*`).
- **Depends on specs:** `model.md` (Implemented): `ID`, `PersonID`, `DepartmentID`,
  `Sensitivity`. `lib-errs.md`, `lib-core.md` (Implemented): `errs`, `clock`, `validate`.
  `module-contract.md` (Implemented): the leaf types `Permission`, `Scope`,
  `PermissionDecl` already in this package; `Plan.Permissions()`; `Route`/`ModuleRoutes`
  shape; `Deps`. `db.md` (Implemented): `DB`, `Transact`, `AfterCommit`, `Wrap`,
  `ErrReadOnly`, `Set`, `dbtest`. `audit.md` (Implemented): `Recorder`, `Entry`,
  `TitleSource`, `PermissionChecker`. `identity.md` (Implemented): `Session`,
  `SessionFrom`, `PersonFrom` (through an `app` closure, never imported),
  `PermissionChecker`, `PermissionLister`. `signals.md` (Implemented): `Bus`, `Name`,
  `Payload`, `InDelivery`. `telemetry.md` (Implemented): `Tracer`, `Meter`, typed
  attributes. `api.md` (Implemented): `PermissionChecker`, `httpx`, `PermissionModulesRead`.
  `web-shell.md` (Implemented): `ScreenSet`, `NavEntry.access`, `session.permissions`.
  `app.md` (Implemented): part 4, `corePerms`, `a.permissions`, `a.titles`, `lazyPermissions`,
  `lazyTitles`.
- **Used by:** every module (`Deps.Authz`), `api` (shell routes behind `core.modules.read`),
  `audit` (`TitleSource`, `PermissionChecker`), `identity` (`PermissionChecker`,
  `PermissionLister`), `people` (M2, next spec), `departments-events` (M3), `settings`
  (M2), the web shell (`session.permissions`), `app`.

## Summary

**Purpose.** Decide whether a person may perform an action, from the permissions the core and the modules declare, the roles a regional builds from them, the grants it makes (site-wide or for one department, with an optional end date) and its separation-of-duties rules (D23). Package `internal/core/authz`; imports `model`, `lib`, `core/db`, `core/audit`, `core/signals`, `core/telemetry`. Never `core/identity`, `core/module`, `api`, `app`, `config` or a module. The actor comes from an `app` closure over `identity.SessionFrom` (identity decision 6).

**Names.** `authz` is the package, the core part and the migration set (what it is); `access` is the route set (`/api/access/`), the permission prefix, the audit area, the signal prefix and the screen set (what people see).

**Exported (`authz`).**
- Leaf types (unchanged, module-contract): `Permission` (+`Validate`, `Module`), `Scope` (`ScopeSite`, `ScopeDepartment`), `PermissionDecl{Name, Scope, Reaches}`; **new field** `PermissionDecl.StarterRoles []string` (starter role keys the permission is seeded into once; Proposed change 2).
- `Actor{Person model.PersonID, Superuser bool}`; `ActorFunc func(ctx) (Actor, bool)` (no I/O; false for no session or a restricted one; authz also returns false itself inside `signals.InDelivery`).
- `Authorizer` (= `module.Deps.Authz`): `Allow(ctx, Permission) error` (site-wide; also satisfies `api.PermissionChecker`, `audit.PermissionChecker`, `identity.PermissionChecker`); `AllowIn(ctx, Permission, model.DepartmentID) error` (held site-wide or for that department); `ExtentOf(ctx, Permission) (Extent, error)` (the batch primitive: `Extent{Site bool, Departments []model.DepartmentID}`, +`Covers(d)`, `IsZero()`).
- `RoleID`, `GrantID`, `RuleID` (= `model.ID[...]`); `Role{ID, Key, Name, Description, Scope, Base bool, Permissions []RolePermission, CreatedAt, UpdatedAt}`, `RolePermission{Name Permission, Declared bool}`, `RoleInput{Name, Description, Scope, Permissions []Permission}`; `Grant{ID, Person, Role RoleID, Department model.DepartmentID, EndsAt, GrantedBy model.PersonID, CreatedAt}`, `GrantInput{Person, Role, Department, EndsAt}`, `GrantQuery{Person, Role, RoleKey string (D38), Department, Permission, IncludeEnded bool, After Cursor, Limit int}`, `GrantPage{Grants, Next Cursor}`, `Cursor` (opaque `created_at`,`id`); `Rule{ID, Role RoleID, Permission Permission, Reaches model.Sensitivity, Note string, Affected int, CreatedAt}`, `RuleInput{Role, Permission, Reaches, Note}`; `PermissionInfo{PermissionDecl}` (the catalogue row, `Module()` derived); `SeedResult{Added int}`.
- `DepartmentSource{Active(ctx, model.DepartmentID) (bool, error)}` (departments-events, M3; nil means department-scoped grants are `Unsupported`).
- `Options{DB, Clock, Logger, Tracer, Meter, Audit audit.Recorder (scoped "access"), Signals signals.Bus (scoped "access"), Actor ActorFunc, Declared []PermissionDecl, Departments DepartmentSource}` (+`Validate`); `New(Options) (*Service, error)` (no I/O; works against `db.None()`).
- `*Service` (safe for concurrent use; nothing to close): the `Authorizer` methods; `HoldersOf(ctx, Permission) ([]model.PersonID, error)` (D39; a system read: the persons who hold the permission site-wide now, superusers excluded; for `app`'s superuser-alert adapter and people's deletion notice; never on a route); `PermissionsOf(ctx, model.PersonID) ([]string, error)` (= `identity.PermissionLister`; `p` for site, `p@<department-id>` for departments; every declared name for the superuser); `TitlesOf(ctx, model.PersonID) ([]string, error)` (= `audit.TitleSource`; keys or names of the non-base roles held now); `ListPermissions(ctx) ([]PermissionInfo, error)`; `ListRoles(ctx) ([]Role, error)`; `GetRole(ctx, RoleID) (Role, error)`; `CreateRole(ctx, RoleInput) (Role, error)`; `UpdateRole(ctx, RoleID, RoleInput) (Role, error)`; `DeleteRole(ctx, RoleID) error`; `ListGrants(ctx, GrantQuery) (GrantPage, error)`; `Grant(ctx, GrantInput) (Grant, error)`; `Revoke(ctx, GrantID) error`; `ListRules(ctx) ([]Rule, error)`; `CreateRule(ctx, RuleInput) (Rule, error)`; `DeleteRule(ctx, RuleID) error`; `Seed(ctx, []PermissionDecl) (SeedResult, error)` (`bms migrate`, writer only); `ExportPerson(ctx, model.PersonID) (json.RawMessage, error)`; `DeletePerson(ctx, model.PersonID) (int, error)`; `Routes() []Route`; `OpenAPI() []byte`. `Route{Method, Pattern string, Handler http.Handler}` (all signed-in; `app` converts).
- Constants: starter role keys `RoleParticipant = "participant"` (base), `RoleAccountAdmin`, `RoleAccessAdmin`, `RoleDepartmentSteward`, `RoleEventAdmin`, `RoleSettingsAdmin`, `RoleAuditReviewer`, `RoleDepartmentAdmin`; `SiteAdminRoles` (the six admin keys, for `core.modules.read`); `MaxRoleNameBytes = 64`, `MaxDescriptionBytes = 512`, `MaxNoteBytes = 256`, `MaxRolePermissions = 256`, `MaxPageSize = 200`, `DefaultPageSize = 50`.
- Permissions: `access.roles.read`, `access.roles.manage`, `access.rules.manage` (site), `access.grants.read`, `access.grants.manage` (department-scopable), `access.department-grants.manage` (site); all reach `internal`; `Permissions []PermissionDecl`. Signals `access.grant-created`, `access.grant-revoked` (`grant`, `person`, `role`, `department` when scoped). Audit actions `access.role.created|changed|deleted`, `access.grant.created|revoked`, `access.rule.created|deleted`, `access.grants.deleted`. `Set = db.Set{Name: "authz", Schema: "core"}`.
- `authztest.NewFake() *Fake` (an `Authorizer` that denies everything until `Fake.Grant(person, p)` / `GrantIn(person, p, dept)` / `Superuser(person)`; reads the actor through `identity`-free `authztest.WithActor(ctx, Actor)` or an `ActorFunc` the test passes).

**Key decisions.** 1 a role has a scope: `site` roles hold any permission and are granted site-wide; `department` roles hold `ScopeDepartment` permissions only and are granted for one department, so a grant row is consistent for every permission in the role. 2 one **base role** (`participant`) applies to every signed-in person without a grant row; editable, not deletable, not grantable; every other starter role may be deleted and stays deleted (owner, 2026-10-18). 3 the effective set = union of held roles' permissions (with their extents) minus every permission a separation-of-duties rule forbids for a role the person holds; rules are checked when granting (`Conflict`) and again on every decision, so a role edit cannot get around them (09 §4). 4 a rule is "holding role A in any extent forbids permission B (or every declared permission reaching at least level L) in every extent". 5 granting a site role needs `access.grants.manage` site-wide; granting a department role at D needs `access.grants.manage` site-wide, `access.department-grants.manage` site-wide (the steward's "set or reset leadership"), or `access.grants.manage@D`; only an `@D` granter is bound by the **no-escalation rule**: they may grant at D only roles whose every permission they hold at D; site-wide holders of either permission are exempt and bound by the separation-of-duties rules (owner, 2026-10-18). 6 the superuser is allowed before any query (`Allow`/`AllowIn` nil, `ExtentOf{Site: true}`, `PermissionsOf` = every declared name); it is never a role or a grant (D23); a grant to the superuser's own person is not blocked (harmless, the flag already allows everything) (owner, 2026-10-18). 7 no actor, a restricted session, or `signals.InDelivery(ctx)` → `Unauthenticated` (fail closed; a handler runs as the system and uses its spec's system path). 8 `ExtentOf` is the batch answer for lists: a module filters with `WHERE department_id = ANY($1)` or shows all when `Site`; authz never sees records. 9 self-grant and self-revoke are refused (`Invalid`, field `person`, code `self`), superuser excepted (owner, 2026-10-18). 10 the last *effective* access admin (an active site-wide holder of `access.grants.manage` not denied it by a separation-of-duties rule) cannot be revoked, have the permission edited out of their role, be deleted, or be left without it by a new rule (`Conflict`, `last_access_admin`) (D37); the superuser is the break-glass if a term expiry leaves none. 11 revocation deletes the grant row; the audit log is the history; ended (expired) rows stay until `DeletePerson`. 12 **starter roles** are created by migration `0001` (keys, scope, base; no permissions); membership comes from `PermissionDecl.StarterRoles`, applied by `Seed` during `bms migrate`, once per (role key, permission) through `core.role_seed`, so a regional's later edits are never undone and a deleted starter role never returns. 13 a role or rule may name a permission that is not declared in this installation (a disabled module, a later milestone); it is stored, shown as "not available" and has no effect; `Allow` on an undeclared name is a programming error (`Invalid`) (owner, 2026-10-18). 14 authz's own code does not verify that a person exists: the foreign key does, from migration `0002` (`core.role_grant.person_id → core.person(id)` ON DELETE RESTRICT, `granted_by → core.person(id)` ON DELETE SET NULL; people's set runs before authz's; people.md Proposed change 9(f)); authz asks `DepartmentSource.Active` for departments (M3; nil → `Unsupported`). 15 no cache: a decision is at most three statements; the api asks once per request and a service once per use case; revisit with traces. 16 writes that touch the invariants (grant, revoke, role change, role delete, create rule, delete person) take `pg_advisory_xact_lock` on one key, so the duplicate and last-admin checks are serialised. 17 regional differences are data: roles, grants, rules; this spec owns no settings. 18 no event scope and no "own records" scope: terms are end dates, and self-access is each service's `identity.PersonFrom` check.

**Errs kinds callers handle.** `Allow`/`AllowIn`/`ExtentOf`: `Unauthenticated`, `Permission` (not held, or forbidden by a rule), `Invalid` (undeclared permission; zero department), store kinds (`Unavailable`, `Timeout`, `Canceled`). Role, grant and rule methods: `Unauthenticated`; `Permission`; `NotFound` (unknown ID, or outside the caller's extent); `Invalid` with field codes `required`, `malformed`, `too_long {max}`, `duplicate`, `scope`, `mismatch`, `range`, `base`, `immutable`, `self`, `one_of`; `Conflict` with codes `taken` (role name), `duplicate` (grant exists), `separation_of_duties {rule}`, `in_use`, `last_access_admin`; `Unsupported` (department grant without a `DepartmentSource`); `Unavailable` wrapping `db.ErrReadOnly`. `Seed`/`DeletePerson`: `Invalid`, `Unavailable` wrapping `db.ErrReadOnly`, `Conflict` (`last_access_admin`). `New`/`Options.Validate`: `Invalid` with fields `authz.db`, `authz.clock`, `authz.logger`, `authz.tracer`, `authz.audit`, `authz.signals`, `authz.actor`, `authz.declared[i].*`, `authz.declared` (duplicate). No sentinels.

**Owns.** Tables `core.role`, `core.role_permission`, `core.role_grant`, `core.sod_rule`, `core.role_seed` (all `internal`; nothing personal beyond person IDs). The six permissions, two signals, eight audit actions, route set `access` (`GET /permissions`, `GET|POST /roles`, `GET|PUT|DELETE /roles/{role}`, `GET|POST /grants`, `DELETE /grants/{grant}`, `GET|POST /rules`, `DELETE /rules/{rule}`), screens `/access/roles`, `/access/roles/new`, `/access/roles/:role`, `/access/grants`, `/access/grants/new`, `/access/rules`; keys `core.access.*`; spans `authz.allow`, `authz.allow-in`, `authz.extent`, `authz.<use-case>`; metric `bms.authz.decisions{outcome}`. No settings, no config keys.

**Expects of dependencies.** `app`: part 4 (`declare`: `Set`, `Permissions`, the two signal names; `build`: `New` over `a.corePerms` + `a.plan.Permissions()`, the actor closure, `audit.Scoped("access")`, `a.disp.Scope("access", ...)`; sets `a.permissions`, `a.titles`, `a.deps.authz`, route set `access`; a `seed` step in `migrate`; lazy adapters for identity's `Permissions` and `Lister`, built before authz). `module-contract`: `Deps.Authz authz.Authorizer`, `PermissionDecl.StarterRoles`, `moduletest` supplying `authztest.NewFake()`. `identity`: `SessionFrom`/`PersonFrom` semantics (restricted session is not an actor), `PermissionLister` called with the session's own person. `audit`: `Recorder` honours the ctx transaction; `TitlesOf` may be called inside a `Write` transaction. `db`: `Transact`, `AfterCommit`, `Wrap`, `*Error.Constraint`, `ErrReadOnly`. `signals`: a scoped `Bus`; `InDelivery`. `web-shell`: `session.permissions` entries `p` and `p@<dept>`; `NavEntry.access` matching `p` or any `p@`; the M1 "empty list shows everything" placeholder removed. `people` (M2): `GET /api/people/search` and a batch names route for the grant screens; `ExportPerson`/`DeletePerson` called from its orchestration. `departments-events` (M3): `DepartmentSource`, a departments list route for the grant form.

**Open questions:** None (the five were settled 2026-10-18, folded into decisions 2, 5, 6, 9 and 13). **Proposed changes elsewhere:** 18, listed at the end; 17 applied (15 on 2026-10-18, 10 with departments-events, D37; 11, 12 and 18 on 2026-10-19, D39). The last-access-admin check counts separation-of-duties rules and `CreateRule` is refused when it would leave none; `Seed` audits an upgrade that adds a permission to a starter role (D37).

**Accepted deviations (M2 build, 2026-10-18).** `authz/perm` (the leaf types and role-key constants, re-exported here as aliases) and `authz/rows`; list responses are wrapped objects; `UpdateRole` refuses a scope change; decisions fail closed; grants carry `role_key` and `role_name`; F64; migration 0002. See Behavior.

## Purpose

Decide whether a person may perform an action, from the permissions the core and the modules declare and the roles, grants and separation-of-duties rules a regional sets.

## Responsibilities

- Hold the catalogue of declared permissions (the core's and every enabled module's) and show it to access admins with each permission's scope and the sensitivity level it reaches.
- Hold roles (named sets of permissions, site or department scoped), one base role every signed-in person has, and the starter roles a new installation ships with.
- Hold grants: a role to a person, site-wide or for one department, with an optional end date; who granted it; when.
- Hold separation-of-duties rules and enforce them both when granting and on every decision.
- Answer decisions: one permission site-wide (`Allow`), one permission in one department (`AllowIn`), and the extent in which a permission is held (`ExtentOf`), so lists are filtered without a query per row.
- Fill the session's permission list (`PermissionsOf`) so the shell can hide what a person cannot use, and the audit log's actor titles (`TitlesOf`, D22).
- Record every grant, revocation, role change and rule change in the audit log, and publish grant signals.
- Seed starter-role membership from declarations once, at `bms migrate`.
- Export and delete one person's grants for `people`'s per-person export and deletion.
- The `access` API and the M2 screens for roles, grants and rules.

## Not responsible for

- **Who the actor is.** `identity` authenticates and marks the superuser; `app` hands authz a closure over `identity.SessionFrom`.
- **Creating or marking the superuser.** `identity` (D23). authz only honours the flag.
- **"Own records" access** (a person reading their own profile, shift, greeting). Each service checks `identity.PersonFrom(ctx)` against the record itself; authz has no notion of record ownership.
- **Which records are in which department.** Modules attach department IDs to their records and filter with `ExtentOf`; authz never sees records.
- **Whether a person or department exists.** `people` (persons; decision 14) and `departments-events` (`DepartmentSource`).
- **Audit storage and the "who viewed my data" views.** `audit`.
- **Positions, qualifications, shift leads** (Khaki, R12). Those are `volunteers`' data (Q-QUAL); a position is not a role here.
- **Notifying a person they were appointed.** `notify` (M4) may subscribe to `access.grant-created`.
- **Translating role titles.** Starter role titles are catalogue keys the shell translates; a regional's own role names are its data.
- **Terms that renew.** Nothing renews (Q-CORE11); an end date is the whole mechanism.

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| Catalogue | hold the declared permissions and say what each reaches | `Options.Declared`, `ListPermissions`; in memory, frozen at `New` |
| Roles | hold named permission sets with a scope, the base role and the starter roles | `core.role`, `core.role_permission`; `*Role` methods; `Seed` |
| Grants | hold who holds which role where and until when | `core.role_grant`; `Grant`, `Revoke`, `ListGrants` |
| Rules | hold what a role holder may never also hold | `core.sod_rule`; `*Rule` methods |
| Decider | answer `Allow`, `AllowIn`, `ExtentOf`, `PermissionsOf`, `TitlesOf` from the three above | read-only; works on a read-only copy |
| Access API and screens | expose roles, grants and rules to admins over HTTP and in the shell | `Routes`, `OpenAPI`, `web/src/core/access/` |

## Interface

```go
package authz

// Actor is the person a decision is about. app builds it from identity.SessionFrom.
type Actor struct {
	Person    model.PersonID
	Superuser bool // D23: allowed before any query; never a role
}

// ActorFunc reads the actor from ctx without I/O. ok is false when there is no session
// or the session is restricted (identity.PersonFrom's rule). The service additionally
// treats signals.InDelivery(ctx) as "no actor" (signals decision 7).
type ActorFunc func(ctx context.Context) (Actor, bool)

// Extent is where a person holds a permission. Site true means everywhere (Departments
// is then empty). Departments is sorted and unique.
type Extent struct {
	Site        bool
	Departments []model.DepartmentID
}

// Covers reports whether d is inside the extent. Cannot fail.
func (e Extent) Covers(d model.DepartmentID) bool

// IsZero reports that the permission is held nowhere. Cannot fail.
func (e Extent) IsZero() bool

// Authorizer is what every module receives as Deps.Authz and what the api, audit and
// identity seams are satisfied by. Implementations are safe for concurrent use.
type Authorizer interface {
	// Allow fails errs.Permission unless the actor holds p site-wide. Consumers: api
	// (shell routes), audit (Search, Export), identity (admin methods), every module's
	// site-scoped use cases.
	Allow(ctx context.Context, p Permission) error
	// AllowIn fails errs.Permission unless the actor holds p site-wide or for d.
	// Consumers: a module's department-scoped write or single-record read
	// (volunteers.shifts.manage for one department's shift).
	AllowIn(ctx context.Context, p Permission, d model.DepartmentID) error
	// ExtentOf returns where the actor holds p; a zero Extent and nil error when nowhere.
	// Consumers: list, search, count and export use cases, which filter by it.
	ExtentOf(ctx context.Context, p Permission) (Extent, error)
}

// DepartmentSource says whether a department exists and is not archived.
// departments-events implements it (M3); nil refuses department grants (Unsupported).
type DepartmentSource interface {
	Active(ctx context.Context, d model.DepartmentID) (bool, error)
}

// Options builds the service. Audit and Signals are scoped to "access" by app.
type Options struct {
	DB          db.DB
	Clock       clock.Clock
	Logger      *slog.Logger
	Tracer      telemetry.Tracer
	Meter       telemetry.Meter // optional
	Audit       audit.Recorder
	Signals     signals.Bus
	Actor       ActorFunc
	Declared    []PermissionDecl // api.PermissionModulesRead, each core part's, Plan.Permissions()
	Departments DepartmentSource // optional
}

// Validate fails errs.Invalid joining a validate.FieldError per problem (fields authz.db,
// authz.clock, authz.logger, authz.tracer, authz.audit, authz.signals, authz.actor,
// authz.declared[i].name|scope|reaches|starter_roles[j], authz.declared for a duplicate).
func (o Options) Validate() error

// New validates and builds. No I/O, so it works against db.None() (app decision 7).
func New(o Options) (*Service, error)

// Service implements Authorizer, identity.PermissionLister, audit.TitleSource and the
// use cases below. Safe for concurrent use; nothing to close.
type Service struct{ /* unexported */ }

func (s *Service) Allow(ctx context.Context, p Permission) error
func (s *Service) AllowIn(ctx context.Context, p Permission, d model.DepartmentID) error
func (s *Service) ExtentOf(ctx context.Context, p Permission) (Extent, error)

// PermissionsOf is identity.PermissionLister: the names the session's person holds after
// rules, "p" for site and "p@<department-id>" for departments, sorted and unique; every
// declared name for the superuser. person must be the actor's own (Permission otherwise).
func (s *Service) PermissionsOf(ctx context.Context, person model.PersonID) ([]string, error)

// HoldersOf returns the persons who hold p site-wide now (D39): through a grant that has
// not ended, after separation-of-duties rules, in person ID order. Superusers are excluded (they are never a grant, decision 6). The base role
// applies to everyone and is not enumerated, so a permission carried by the base role
// yields only its explicit grant holders. A system read: no actor, no permission check,
// not audited, never on a route; consumers are app's superuser-alert adapter and
// people's deletion notice. Fails Invalid for an undeclared permission and the store
// kinds. A department-scoped permission is held site-wide only by a site grant.
func (s *Service) HoldersOf(ctx context.Context, p Permission) ([]model.PersonID, error)

// TitlesOf is audit.TitleSource: the key (or, when renamed, the name) of each non-base
// role the person holds now, sorted, at most audit.MaxTitles. No actor needed: the
// recorder calls it for whoever is acting, possibly inside a Write transaction.
func (s *Service) TitlesOf(ctx context.Context, person model.PersonID) ([]string, error)

// ListPermissions returns the catalogue. Consumer: the role editor and the rule form.
func (s *Service) ListPermissions(ctx context.Context) ([]PermissionInfo, error)

func (s *Service) ListRoles(ctx context.Context) ([]Role, error)
func (s *Service) GetRole(ctx context.Context, id RoleID) (Role, error)
func (s *Service) CreateRole(ctx context.Context, in RoleInput) (Role, error)
func (s *Service) UpdateRole(ctx context.Context, id RoleID, in RoleInput) (Role, error)
func (s *Service) DeleteRole(ctx context.Context, id RoleID) error

func (s *Service) ListGrants(ctx context.Context, q GrantQuery) (GrantPage, error)
func (s *Service) Grant(ctx context.Context, in GrantInput) (Grant, error)
func (s *Service) Revoke(ctx context.Context, id GrantID) error

func (s *Service) ListRules(ctx context.Context) ([]Rule, error)
func (s *Service) CreateRule(ctx context.Context, in RuleInput) (Rule, error)
func (s *Service) DeleteRule(ctx context.Context, id RuleID) error

// Seed applies each declaration's StarterRoles once (core.role_seed). Consumer: app's
// migrate command, after db.Migrate, on the writing copy. Idempotent.
func (s *Service) Seed(ctx context.Context, declared []PermissionDecl) (SeedResult, error)

// ExportPerson returns the person's grants as JSON. DeletePerson deletes them (ended ones
// too), clears granted_by on grants they made, and returns the number deleted.
// Consumer: people's per-person export and deletion (through app's adapter).
func (s *Service) ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
func (s *Service) DeletePerson(ctx context.Context, person model.PersonID) (int, error)

// Routes are relative to /api/access, all signed-in. app converts them to module.Route.
func (s *Service) Routes() []Route
func (s *Service) OpenAPI() []byte

type Route struct {
	Method  string
	Pattern string
	Handler http.Handler
}
```

**Per method: consumer, pre- and post-conditions.**

| Method | Consumer | Pre | Post |
|---|---|---|---|
| `Allow` | api, audit, identity, modules | `p` declared; an actor in ctx | nil iff superuser, or `p` in the effective site set |
| `AllowIn` | modules | `p` declared (any scope); `d` non-zero | nil iff superuser, `p` held site-wide, or held at `d` |
| `ExtentOf` | modules' lists | `p` declared | `{Site:true}` for the superuser; else union of extents after rules |
| `PermissionsOf` | identity `SessionInfo` | actor's own person | sorted unique names; `[]` never nil |
| `TitlesOf` | audit recorder | none | ≤16 titles, each ≤64 bytes; nil for none |
| `ListPermissions` | role editor, rule form | `access.roles.read` site-wide, **or** `access.grants.read`/`.manage`/`department-grants.manage` in any extent (then `ScopeDepartment` permissions only) | sorted by name |
| `ListRoles` | role list, grant form | same rule as `ListPermissions` (department-extent callers see department roles only) | sorted by name; every role carries its permissions with `Declared` |
| `GetRole` | role editor | as `ListRoles`; a site role for a department-extent caller is `NotFound` | |
| `CreateRole` | role editor | `access.roles.manage` | audited `access.role.created` in the transaction |
| `UpdateRole` | role editor | `access.roles.manage`; `Scope`, `Base`, `Key` unchanged | full replacement of name, description, permissions; `access.role.changed` |
| `DeleteRole` | role editor | `access.roles.manage`; not base; no grants, no rules | `access.role.deleted` |
| `ListGrants` | grants screen, "my roles", people's person page link | `access.grants.read` in some extent, **or** `q.Person` is the actor | results limited to the caller's extent (site roles only for site holders; department grants only inside their departments), newest first, keyset |
| `Grant` | grant form, department admins, stewards | decision 5; not self; role not base; department consistent with the role's scope and `Active`; `EndsAt` zero or future; no duplicate; no rule violated | row written, `access.grant.created` audited (Subject = person) and `access.grant-created` published in the same transaction |
| `Revoke` | grants screen | may grant that same grant (decision 5); not self; not the last access admin | row deleted, `access.grant.revoked`, `access.grant-revoked` |
| `ListRules` | rules screen | `access.roles.read` | sorted by role name; `Affected` = people holding the role and a forbidden permission now |
| `CreateRule` | rules screen | `access.rules.manage`; exactly one of `Permission`/`Reaches` | `access.rule.created`; takes effect at once |
| `DeleteRule` | rules screen | `access.rules.manage` | `access.rule.deleted` |
| `Seed` | app `migrate` | writer | adds missing `(role, permission)` for each declaration's starter keys not in `core.role_seed`; records them; skips keys without a role |
| `ExportPerson` | people | non-zero ID | JSON `{"grants":[...]}`; `[]` when none |
| `DeletePerson` | people | non-zero ID; writer; not the last access admin | audited `access.grants.deleted` (Subject = person); publishes `access.grant-revoked` per removed grant, inside the transaction (D37) |

## Types

```go
type role struct{}; type grant struct{}; type rule struct{}
type RoleID = model.ID[role]
type GrantID = model.ID[grant]
type RuleID = model.ID[rule]

type Role struct {
	ID          RoleID
	Key         string           // starter roles only ("account-admin"); "" otherwise; immutable
	Name        string           // "" while an unrenamed starter role (the shell shows core.access.role.<key>.title)
	Description string
	Scope       Scope            // immutable after creation
	Base        bool             // the one role every signed-in person holds; immutable
	Permissions []RolePermission // sorted by name
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
type RolePermission struct {
	Name     Permission
	Declared bool // false: not available in this installation (decision 13)
}
type RoleInput struct {
	Name        string       // 1..MaxRoleNameBytes after trim, no control characters
	Description string       // ..MaxDescriptionBytes
	Scope       Scope        // create only; ignored by UpdateRole
	Permissions []Permission // unique; ..MaxRolePermissions; ScopeDepartment only in a department role
}

type Grant struct {
	ID         GrantID
	Person     model.PersonID
	Role       RoleID
	Department model.DepartmentID // zero: site-wide
	EndsAt     time.Time          // zero: no end
	GrantedBy  model.PersonID     // zero: operator, seed or a deleted person
	CreatedAt  time.Time
}
type GrantInput struct {
	Person     model.PersonID
	Role       RoleID
	Department model.DepartmentID
	EndsAt     time.Time
}
type GrantQuery struct {
	Person       model.PersonID
	Role         RoleID
	RoleKey      string // a starter role key (D38); ListGrants resolves it to the role's ID after the permission and extent checks; an unknown key gives an empty page, like an unknown ID; setting both Role and RoleKey is Invalid (`role` `one_of`)
	Department   model.DepartmentID
	Permission   Permission // grants of roles holding it ("everyone who holds a permission")
	IncludeEnded bool
	After        Cursor
	Limit        int // 0 = DefaultPageSize; 1..MaxPageSize
}
type GrantPage struct {
	Grants []Grant
	Next   Cursor // zero when no more
}
type Cursor string // opaque: base64url of created_at and id; ParseCursor fails Invalid

type Rule struct {
	ID         RuleID
	Role       RoleID            // holding this role...
	Permission Permission        // ...forbids this permission (exactly one of Permission/Reaches)
	Reaches    model.Sensitivity // ...or every declared permission reaching at least this level
	Note       string            // the regional's reason, ..MaxNoteBytes
	Affected   int               // people holding the role and a forbidden permission now (filled by ListRules)
	CreatedAt  time.Time
}
type RuleInput struct {
	Role       RoleID
	Permission Permission
	Reaches    model.Sensitivity
	Note       string
}

type PermissionInfo struct{ PermissionDecl } // Module() from the name
type SeedResult struct{ Added int }
```

`PermissionDecl` gains `StarterRoles []string` (each a valid ident, unique; Proposed change 2). `Validate` of every input joins one `validate.FieldError` per problem and never echoes a value.

Wire forms (`openapi.json`, schema names `Access*`): `AccessPermission{name, scope, reaches, module, starter_roles}`, `AccessRole{id, key, name, description, scope, base, permissions:[{name, declared}], created_at, updated_at}`, `AccessRoleInput`, `AccessGrant{id, person_id, role_id, department_id?, ends_at?, granted_by?, created_at, ended}`, `AccessGrantInput`, `AccessGrantPage{grants, next?}`, `AccessRule{id, role_id, permission?, reaches?, note, affected, created_at}`, `AccessRuleInput`. IDs as canonical UUID strings; times RFC 3339 UTC.

## Data

Migration set `authz`, schema `core`, `internal/core/authz/postgres/migrations/0001_init.sql`; `0002` adds the foreign keys to `core.person` (decision 14). Every table has `COMMENT ON` lines with the sensitivity level, as identity's does. No column holds a name, address or anything personal: only IDs, role names and permission names. All columns are `internal`; retention is the row's lifetime unless said.

**`core.role`**

| Column | Type | Constraints |
|---|---|---|
| `id` | `uuid` | PK, not the nil UUID |
| `key` | `text` | NULL; UNIQUE; ident grammar (`[a-z][a-z0-9]*(-[a-z0-9]+)*`, ≤64 bytes); set only by migration |
| `name` | `text` | NOT NULL DEFAULT ''; ≤64 bytes; unique on `lower(name)` where `name <> ''` (`role_name_key`) |
| `description` | `text` | NOT NULL DEFAULT ''; ≤512 bytes |
| `scope` | `text` | NOT NULL, CHECK IN ('site','department') |
| `base` | `boolean` | NOT NULL DEFAULT false; unique partial index where `base` (one base role); CHECK (`NOT base OR scope = 'site'`) |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL |

Seeded rows (keys; names empty): `participant` (site, base), `account-admin`, `access-admin`, `department-steward`, `event-admin`, `settings-admin`, `audit-reviewer` (site), `department-admin` (department). No permissions: `Seed` fills them (decision 12).

**`core.role_permission`**: `role_id uuid` FK `core.role ON DELETE CASCADE`, `permission text` (permission grammar, ≤255 bytes); PK `(role_id, permission)`; index on `(permission)` for "who holds this permission".

**`core.role_grant`** (`grant` is reserved in SQL)

| Column | Type | Constraints | Sensitivity | Retention |
|---|---|---|---|---|
| `id` | `uuid` | PK | — | |
| `person_id` | `uuid` | NOT NULL, not nil; no FK (decision 14) | internal (that a person holds a role) | until revoked or `DeletePerson` |
| `role_id` | `uuid` | NOT NULL FK `core.role ON DELETE RESTRICT` | internal | |
| `department_id` | `uuid` | NULL (site-wide when NULL) | internal | |
| `ends_at` | `timestamptz` | NULL | internal | rows past `ends_at` stay (history) until `DeletePerson` |
| `granted_by` | `uuid` | NULL (operator, seed, or the granter was deleted) | internal | set NULL by `DeletePerson` of the granter |
| `created_at` | `timestamptz` | NOT NULL | internal | |

Unique index `role_grant_person_role_department_key` on `(person_id, role_id, coalesce(department_id, '00000000-0000-0000-0000-000000000000'))`. Indexes: `(person_id)` (decisions), `(role_id)` (in-use check, lists), `(department_id)`, `(created_at DESC, id DESC)` (keyset). "Active" everywhere means `ends_at IS NULL OR ends_at > $now`, with `$now` from `clock.Clock` as a parameter.

**`core.sod_rule`**: `id uuid` PK; `role_id uuid` NOT NULL FK `core.role ON DELETE RESTRICT`; `permission text` NULL (grammar); `reaches text` NULL CHECK IN ('public','internal','personal','sensitive'); CHECK exactly one of `permission`/`reaches` is NOT NULL; `note text` NOT NULL DEFAULT '' ≤256 bytes; `created_at`; unique `(role_id, permission)` and `(role_id, reaches)`.

**`core.role_seed`**: `role_key text`, `permission text`, `seeded_at timestamptz` NOT NULL; PK `(role_key, permission)`. Records that a declaration's starter membership was applied, so it is applied once (decision 12).

Export (`ExportPerson`): the person's `core.role_grant` rows with role key and name. Deletion (`DeletePerson`): those rows deleted, `granted_by` nulled elsewhere; audit rows keep the IDs (audit decision 10). Nothing here needs a retention setting: it is `internal` and tiny; an "ended grants" purge is a `jobs` item for Later if a regional asks.

## Permissions

| Permission | Allows | Scope | Reaches | Starter roles |
|---|---|---|---|---|
| `access.roles.read` | list the catalogue, roles and rules | site | internal | `access-admin` |
| `access.roles.manage` | create, change, delete roles | site | internal | `access-admin` |
| `access.rules.manage` | create and delete separation-of-duties rules | site | internal | `access-admin` |
| `access.grants.read` | see who holds what (site-wide, or within one department) | site or department | internal | `access-admin` (site), `department-steward` (site; D38), `department-admin` |
| `access.grants.manage` | grant and revoke roles (site-wide: any role; within a department: that department's roles, bound by decision 5) | site or department | internal | `access-admin` (site), `department-admin` |
| `access.department-grants.manage` | grant and revoke **department** roles in any department (a steward setting or resetting leadership, 09 §5) | site | internal | `department-steward` |

Declared with `StarterRoles` as above. `core.modules.read` (api's) should declare `StarterRoles: authz.SiteAdminRoles` (Proposed change 6). Starter role contents known today, after `Seed` on a fresh M2 installation (the base role holds no `people.*` permission by default, so there is no open directory; a regional that wants one adds `people.directory.read` to it; people.md decision 20):

| Key | Scope | Permissions |
|---|---|---|
| `participant` (base) | site | `hello.greetings.read`, `hello.greetings.write` (owner, module-hello decision 11) |
| `account-admin` | site | `accounts.read`, `accounts.manage`, `core.modules.read`, `people.directory.read`, `people.contacts.read` (only here in M2), `people.profiles.manage`, `people.data.export`, `people.data.delete` |
| `access-admin` | site | the five `access.*` above except `department-grants.manage`, `core.modules.read`, `people.directory.read` |
| `department-steward` | site | `access.grants.read` (D38), `access.department-grants.manage`, `departments.manage`, `departments.details.manage`, `core.modules.read`, `people.directory.read` |
| `event-admin` | site | `events.manage`, `core.modules.read` |
| `settings-admin` | site | `core.modules.read` (settings adds its own) |
| `audit-reviewer` | site | `audit.search`, `audit.export`, `core.modules.read`, `people.directory.read` |
| `department-admin` | department | `access.grants.read`, `access.grants.manage`, `departments.details.manage`, `people.directory.read` (modules add theirs) |

No everyday role holds everything (D23); only the superuser does, by flag.

## Signals

Published (payload keys and types; no personal data):

| Name | When | Payload |
|---|---|---|
| `access.grant-created` | inside the transaction that inserts a grant (delivered after commit) | `grant` GrantID, `person` PersonID, `role` RoleID, `department` DepartmentID (only when scoped) |
| `access.grant-revoked` | inside the transaction that deletes a grant, including each grant `DeletePerson` removes (one signal per removed grant, D37) | same keys |

Not published: role and rule changes (nobody reacts; the audit log records them), `Seed`, expiry (no job runs; a consumer asks `ExtentOf`). Subscribed: none (`DeletePerson` is called directly by `people`, like identity's; it publishes `access.grant-revoked` for each grant it deletes, so a subscriber hears of every removal).

## Settings

None. Every regional difference here (which jobs exist, who holds them, what may never be combined, how long a term lasts) is data: roles, grants with end dates, rules. The one behavioural choice a regional might want switched (decision 9, self-grant) is put to the owner first.

## UI

`web/src/core/access/` (screen set `access`, `kind: "core"`, keys `core.access.*`, `messages/en.json`). Admin-area nav entries: **Roles** (`/access/roles`, `access: ["access.roles.read"]`) and **Grants** (`/access/grants`, `access: ["access.grants.read"]`, which also matches `access.grants.read@<dept>` once the shell applies Proposed change 7). Phone first: lists are cards, forms are one column, dialogs are the shell's `Dialog`. Every failure uses `ErrorState`/`ErrorSummary`; field codes map to inputs by `name`. Playa names come from `people`'s batch names route; until it exists the screens show the person ID (Proposed change 9).

- **Roles** (`/access/roles`): cards (name or translated starter title, scope badge, "everyone" badge for the base role, permission count). "New role" for `access.roles.manage`. A **Rules** tab links to `/access/rules`. *Empty:* "No roles yet." (cannot happen after `migrate`; shown anyway). *Forbidden:* hidden; a direct visit shows the shell's forbidden screen. *Read-only copy:* the list works; the buttons are disabled with the read-only note.
- **Role editor** (`/access/roles/new`, `/access/roles/:role`): name, description, scope (create only; radio "whole site" / "one department"), and the permission picker: the catalogue grouped by module or core area, each row with its scope and a `Reaches` badge (`sensitive` and `personal` stand out), checkboxes; permissions the role holds that are not declared appear in a "Not available in this installation" group with a remove action. In a department role, site-only permissions are shown disabled with a hint. Save; Delete (confirm dialog; explains `in_use`). Errors: `taken`, `scope`, `immutable`, `last_access_admin` (explains that another person must hold access management first). Starter roles show their key and "Rename" sets `name`.
- **Grants** (`/access/grants`): filters person (a search box calling `GET /api/people/search`, or `?person=` from a person page), role (`?role=` is parsed as an ID if it is a UUID, otherwise as a starter role key such as `department-admin`; anything else is 400 on `role`, code `malformed`; D38), department (M3; hidden until `/api/departments` exists), permission ("everyone who holds..."), include ended. Cards: playa name (or ID), role, department, "until <date>" or "ended", granted on/by. Revoke (confirm) where the caller may. "Grant a role" button. *Empty:* "Nobody holds a role matching these filters." A department admin sees only their departments' grants.
- **New grant** (`/access/grants/new`): person (search or prefilled), role (the roles the caller may grant: site roles for site holders; department roles when a department is chosen), department (M3), optional end date (native date input, stored as end of that day in the regional's zone once `settings` exists; UTC midnight until then). Errors: `duplicate`, `separation_of_duties` (shows the rule's note), `self`, `range`, `unsupported` (departments not available yet).
- **Rules** (`/access/rules`): cards "People holding **<role>** may never hold **<permission>**" or "... any permission reaching **<level>** or above", the note, and "<n> people affected now". Create form for `access.rules.manage` (role, then either a permission from the catalogue or a level), delete with confirm. *Empty:* "No separation-of-duties rules. Add one when your regional forbids a combination, for example board members reviewing incident reports."
- **My roles:** not a screen of its own in M2; the person page (`people`) links to `/access/grants?person=<me>`, which `ListGrants` allows for one's own person.

## Behavior

**The decision.** For `Allow(p)` / `AllowIn(p, d)` / `ExtentOf(p)`:
1. `p` must be declared (`Invalid` otherwise: a module asked about a name it did not declare).
2. Actor: `signals.InDelivery(ctx)` → `Unauthenticated`; `Actor(ctx)` not ok → `Unauthenticated`; `Superuser` → allowed (`ExtentOf` → `{Site: true}`), no query, counted `outcome=superuser`.
3. One statement loads the person's active grants joined to `role_permission` (plus the base role's permissions as site), giving `(permission, department | site)` rows; one statement loads the rules whose role the person holds (any extent). Forbidden names (a rule's `Permission`, or every declared name with `Reaches >= rule.Reaches`) are removed from the rows.
4. `Allow`: nil iff a site row for `p` remains. `AllowIn`: nil iff a site row or a row for `d`. `ExtentOf`: `Site` if a site row, else the distinct departments.
A denial is `errs.Permission` with the operation and the permission name in the message (never the person). Decisions are not audited (the caller audits its use case); they are counted (`bms.authz.decisions{outcome}` ∈ `allowed`, `denied`, `superuser`, `unauthenticated`, `error`) and traced (`authz.allow` etc., attributes `permission` as a token, `outcome`). No cache (decision 15).

**Granting.** In one `Write` transaction: take `pg_advisory_xact_lock(hashtext('access.invariants'))`; load the role (not base; scope); check the caller per decision 5 (site holder, steward for department roles, or `@D` holder who passes the no-escalation check: every permission of the role is in the caller's effective set at site or `D`); check department presence against the role's scope and `DepartmentSource.Active`; check `EndsAt`; check self; insert (a unique violation → `Conflict duplicate`); compute the target's effective roles including the new one and fail `Conflict separation_of_duties {rule}` if any rule's role is held and the forbidden set intersects the target's permissions; record `access.grant.created` (Record `access.grant`, Subject the person); publish `access.grant-created`. A read-only copy fails before the lock with `Unavailable` wrapping `db.ErrReadOnly`.

**Revoking.** Same lock; the grant must be inside the caller's granting extent (else `NotFound`, never `Permission`: a department admin must not learn about grants elsewhere); not self; if removing it would leave no effective access admin (no other active site-wide grant of a role holding `access.grants.manage` whose holder is not denied it by a separation-of-duties rule), `Conflict last_access_admin` (D37); delete; audit; publish.

**Roles.** `CreateRole`/`UpdateRole` validate names and permission scopes; `UpdateRole` that removes `access.grants.manage` from a site role fails `last_access_admin` when no other effective access admin remains (D37). `DeleteRole` fails `in_use` while any grant (active or ended) or rule references it. The base role: never deleted, never granted, scope `site`, editable.

**Rules.** Creating a rule that people already violate is allowed; the rule applies at once (their forbidden permissions are denied) and `Affected` shows how many. Order independence: granting A to a holder of B and B to a holder of A are both blocked. `CreateRule` refuses a rule that would leave no effective access admin (every holder of `access.grants.manage` would be denied it): `Conflict last_access_admin`, nothing written (D37).

**Seed.** For each declaration and each key in `StarterRoles`: if `(key, permission)` is in `core.role_seed`, skip; else insert into `role_permission` when a role with that key exists, and record the pair either way. Runs in one transaction under the same lock. `Added` counts inserted permission rows. When an upgrade adds a permission to a starter role (an inserted `(role, permission)` row for a role that exists), `Seed` records an audit entry for it in the same transaction (action `access.role.changed`, Record `access.role`, the change summary in `Reason` as `seed:<permission>`, actor as for any command: `ActorOperator`), so the log shows why a role gained access after an upgrade, including the first run on a fresh install (D37).

**Read-only copy.** Every read (decisions, listers, titles, lists) works. Every write returns `Unavailable` wrapping `db.ErrReadOnly` (the api answers `503 read_only`).

**Audit.** Writes record inside their `Write` transaction through the `access`-scoped `Recorder` (actor from ctx; `Seed` and `DeletePerson` from a command carry whatever actor the ctx has: `ActorOperator` for commands, the person for a request). `TitlesOf` is one statement and may run inside the caller's `Write` transaction; it never opens one.

**Concurrency and lifecycle.** `*Service` is safe for concurrent use, starts no goroutines, holds no ctx, has nothing to close. The catalogue is frozen at `New`. Idempotency: `Grant` is not idempotent (`duplicate`); `Revoke`, `DeleteRole`, `DeleteRule` on a missing ID are `NotFound`; `Seed` and `DeletePerson` are idempotent. Retries: none here; callers see `Unavailable`/`Timeout` and the shell retries GETs only.

**Expired grants** simply stop counting at `ends_at`; nothing is published and nothing is deleted. The grants screen shows them as ended with `include_ended`.

### Accepted implementation deviations (M2 build, 2026-10-18)

- **Leaf packages.** `authz/perm` holds `Permission`, `Scope`, `PermissionDecl` (with `StarterRoles`) and the role-key constants (`RoleParticipant`, ..., `SiteAdminRoles`); `authz` re-exports them as aliases. This breaks an audit<->authz import cycle. `authz/rows` holds the store row types.
- **API shape.** List responses are wrapped objects (`{"roles":[...]}` and so on).
- **Roles.** `UpdateRole` refuses a scope change (`scope` `immutable`). A starter role may be saved with an empty name.
- **Grant and revoke checks.** `Grant` checks separation of duties against the target's whole resulting set, including violations that already existed. `Revoke` answers `Permission` when the caller holds no granting permission anywhere, and `NotFound` when the grant is out of reach.
- **Seed and delete.** `Seed` skips site-only permissions for `department-admin`. The separation-of-duties `Conflict` carries params `{rule, note}`. `DeletePerson` records nothing and publishes nothing when there were no grants. `granted_by` is always the actor's person.
- **Decisions fail closed.** Rows that carry a department for a permission declared `ScopeSite` are dropped at load; the grant-time separation-of-duties check ignores undeclared names; an unreadable rule is `Internal`.
- **Grant wire.** Grants carry `role_key` (nullable) and `role_name`. `ListGrants` reads the role catalogue for the page without gating on `access.roles.read`, so a participant can name their own roles.
- **New failure mode F64** (below): a grant by an actor with no `core.person` row.
- **Migration 0002.** `role_grant_person_id_fkey` is `RESTRICT`; `role_grant_granted_by_fkey` is `ON DELETE SET NULL`, plus a partial index on `granted_by`; a preflight refuses orphan grants with SQLSTATE 23503 carrying schema, table and constraint, and the file rolls back.
- **SQL.** Decision queries ignore grants whose department presence does not match the role's scope; an `INCLUDE` index on `person_id`; a partial department index; no `updated_at` on child tables.
- **UI.** A grant's end date is the end of the chosen day in the browser's zone until `settings` supplies the regional's zone, and is displayed as `ends_at - 1 ms`. Without `access.grants.read`, the grants screen is a self-only "My roles" view.

## Failure modes

Store kinds (`Unavailable`, `Timeout`, `Canceled`) apply to every method that touches the database and are tested once per method group (`Test<Group>_StoreFails_ReturnsStoreKind`); the rows below list the modes specific to each method.

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `New` | nil `DB`, `Clock`, `Logger`, `Tracer`, `Audit`, `Signals` or `Actor` | Invalid | fail start-up, name the field | `TestNew_NilDependency_ReturnsInvalid` |
| F2 | `New` | a declaration invalid, or two with the same name | Invalid (`authz.declared[i]`, `authz.declared`) | fail start-up | `TestNew_DuplicateDeclaration_ReturnsInvalid` |
| F3 | `Allow` | no session in ctx | Unauthenticated | 401 | `TestAllow_NoActor_ReturnsUnauthenticated` |
| F4 | `Allow` | `signals.InDelivery(ctx)` even with a session value in ctx | Unauthenticated | handler uses its system path | `TestAllow_InDelivery_ReturnsUnauthenticated` |
| F5 | `Allow` | permission not declared | Invalid | fix the module | `TestAllow_Undeclared_ReturnsInvalid` |
| F6 | `Allow` | not held | Permission | 403 | `TestAllow_NotHeld_ReturnsPermission` |
| F7 | `Allow` | held only for a department | Permission | 403 | `TestAllow_DepartmentOnly_ReturnsPermission` |
| F8 | `Allow` | held, but forbidden by a rule for a role the person holds | Permission | 403 | `TestAllow_ForbiddenByRule_ReturnsPermission` |
| F9 | `Allow` | the grant has ended | Permission | 403 | `TestAllow_EndedGrant_ReturnsPermission` |
| F10 | `Allow` | superuser | nil, no query | proceed | `TestAllow_Superuser_AllowsWithoutQuery` |
| F11 | `AllowIn` | zero department | Invalid | 400 | `TestAllowIn_ZeroDepartment_ReturnsInvalid` |
| F12 | `AllowIn` | held for another department only | Permission | 403 (or NotFound for a single record, privacy rule) | `TestAllowIn_OtherDepartment_ReturnsPermission` |
| F13 | `ExtentOf` | held nowhere | nil, zero `Extent` | empty list | `TestExtentOf_NotHeld_ReturnsZeroExtent` |
| F14 | `PermissionsOf` | person is not the actor | Permission | identity: fail `SessionInfo` | `TestPermissionsOf_OtherPerson_ReturnsPermission` |
| F15 | `PermissionsOf` | superuser | every declared name | shell shows everything | `TestPermissionsOf_Superuser_ListsAllDeclared` |
| F16 | `TitlesOf` | no roles, or only the base role | nil, nil | audit stores no titles | `TestTitlesOf_BaseOnly_ReturnsNil` |
| F17 | `ListPermissions`/`ListRoles` | neither `access.roles.read` nor a grants permission in any extent | Permission | 403 | `TestListRoles_NoPermission_ReturnsPermission` |
| F18 | `ListRoles` | caller holds grants permissions in a department only | department roles only | screen shows those | `TestListRoles_DepartmentCaller_SeesDepartmentRolesOnly` |
| F19 | `GetRole` | unknown ID, or a site role for a department-extent caller | NotFound | 404 | `TestGetRole_OutsideExtent_ReturnsNotFound` |
| F20 | `CreateRole` | no `access.roles.manage` | Permission | 403 | `TestCreateRole_NoPermission_ReturnsPermission` |
| F21 | `CreateRole` | name empty, too long or with control characters; description too long | Invalid (`name` `required`/`too_long`/`malformed`, `description` `too_long`) | show field errors | `TestCreateRole_BadName_ReturnsInvalid` |
| F22 | `CreateRole` | permission malformed, repeated, or over `MaxRolePermissions` | Invalid (`permissions[i]` `malformed`/`duplicate`, `permissions` `too_long`) | field errors | `TestCreateRole_BadPermissions_ReturnsInvalid` |
| F23 | `CreateRole` | a site-only permission in a department role | Invalid (`permissions[i]` `scope`) | explain | `TestCreateRole_SitePermissionInDepartmentRole_ReturnsInvalid` |
| F24 | `CreateRole` | name already used ignoring case | Conflict (`name` `taken`) | field error | `TestCreateRole_NameTaken_ReturnsConflict` |
| F25 | `CreateRole` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | 503 read_only | `TestCreateRole_ReadOnly_ReturnsReadOnly` |
| F26 | `UpdateRole` | changing scope, key or base | Invalid (`scope`/`base` `immutable`) | explain | `TestUpdateRole_ScopeChange_ReturnsInvalid` |
| F27 | `UpdateRole` | removing `access.grants.manage` would leave no site-wide holder | Conflict (`last_access_admin`) | explain | `TestUpdateRole_RemovesLastAccessAdmin_ReturnsConflict` |
| F28 | `UpdateRole` | unknown ID | NotFound | 404 | `TestUpdateRole_Unknown_ReturnsNotFound` |
| F29 | `DeleteRole` | grants or rules reference it | Conflict (`in_use`) | explain | `TestDeleteRole_InUse_ReturnsConflict` |
| F30 | `DeleteRole` | the base role | Invalid (`role` `base`) | explain | `TestDeleteRole_Base_ReturnsInvalid` |
| F31 | `ListGrants` | no `access.grants.read` anywhere and `Person` is not the actor | Permission | 403 | `TestListGrants_NoPermission_ReturnsPermission` |
| F32 | `ListGrants` | own person without permission | own grants | "my roles" | `TestListGrants_OwnPerson_Allowed` |
| F33 | `ListGrants` | department-extent caller: grants outside their departments | filtered out, never an error | list | `TestListGrants_DepartmentCaller_FiltersToExtent` |
| F34 | `ListGrants` | bad cursor or limit; `?role=` neither a UUID nor a valid key (D38); both `Role` and `RoleKey` set (D38) | Invalid (`after` `malformed`, `limit` `range`, `role` `malformed`, `role` `one_of`) | 400 | `TestListGrants_BadCursor_ReturnsInvalid`, `TestHandlers_ListGrants_BadRole_Returns400`, `TestGrantQuery_RoleAndKey_ReturnsInvalid` |
| F34a | `ListGrants` | `RoleKey` names no role (D38) | nil; empty page, like an unknown ID | none | `TestListGrants_RoleKey_FiltersLikeTheID` (`internal/core/authz/integration_grants_test.go`) |
| F35 | `Grant` | caller may not grant in that extent (no permission; `@D` holder granting a site role; steward granting a site role) | Permission | 403 | `TestGrant_SiteRoleByDepartmentAdmin_ReturnsPermission` |
| F36 | `Grant` | `@D` holder granting a role with a permission they do not hold at D | Permission | explain no-escalation | `TestGrant_Escalation_ReturnsPermission` |
| F37 | `Grant` | own person | Invalid (`person` `self`) | explain | `TestGrant_Self_ReturnsInvalid` |
| F38 | `Grant` | the base role | Invalid (`role` `base`) | explain | `TestGrant_BaseRole_ReturnsInvalid` |
| F39 | `Grant` | unknown role | NotFound | 404 | `TestGrant_UnknownRole_ReturnsNotFound` |
| F40 | `Grant` | department given for a site role / missing for a department role | Invalid (`department` `mismatch`/`required`) | field error | `TestGrant_DepartmentMismatch_ReturnsInvalid` |
| F41 | `Grant` | department role and no `DepartmentSource` (M2) | Unsupported | hide the option | `TestGrant_NoDepartmentSource_ReturnsUnsupported` |
| F42 | `Grant` | `DepartmentSource.Active` false | Invalid (`department` `unknown`) | field error | `TestGrant_InactiveDepartment_ReturnsInvalid` |
| F43 | `Grant` | `EndsAt` not after now | Invalid (`ends_at` `range`) | field error | `TestGrant_EndsInPast_ReturnsInvalid` |
| F44 | `Grant` | same person, role and department already active or ended | Conflict (`duplicate`) | explain | `TestGrant_Duplicate_ReturnsConflict` |
| F45 | `Grant` | a rule forbids the combination | Conflict (`separation_of_duties`, params `rule`) | show the rule | `TestGrant_SeparationOfDuties_ReturnsConflict` |
| F46 | `Grant` | audit `Record` fails | the recorder's kind; nothing written | 5xx | `TestGrant_AuditFails_RollsBack` |
| F47 | `Grant` | `Publish` fails | the bus's kind; rolled back | 5xx | `TestGrant_PublishFails_RollsBack` |
| F48 | `Grant` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | 503 | `TestGrant_ReadOnly_ReturnsReadOnly` |
| F49 | `Revoke` | grant outside the caller's granting extent, or unknown | NotFound | 404 | `TestRevoke_OutsideExtent_ReturnsNotFound` |
| F50 | `Revoke` | own grant | Invalid (`person` `self`) | explain | `TestRevoke_Self_ReturnsInvalid` |
| F51 | `Revoke` | the last effective access admin (D37: counts rules) | Conflict (`last_access_admin`) | explain | `TestRevoke_LastAccessAdmin_ReturnsConflict`, `TestRevoke_LastAdminDeniedByRule_ReturnsConflict` |
| F52 | `CreateRule` | no `access.rules.manage` | Permission | 403 | `TestCreateRule_NoPermission_ReturnsPermission` |
| F53 | `CreateRule` | both or neither of `Permission`/`Reaches`; malformed permission; invalid level; note too long | Invalid (`permission`/`reaches` `one_of`, `malformed`, `note` `too_long`) | field errors | `TestCreateRule_BothOrNeither_ReturnsInvalid` |
| F54 | `CreateRule` | unknown role | NotFound | 404 | `TestCreateRule_UnknownRole_ReturnsNotFound` |
| F55 | `CreateRule` | same rule exists | Conflict (`duplicate`) | explain | `TestCreateRule_Duplicate_ReturnsConflict` |
| F56 | `DeleteRule` | unknown ID | NotFound | 404 | `TestDeleteRule_Unknown_ReturnsNotFound` |
| F57 | `Seed` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | migrate exits 1 | `TestSeed_ReadOnly_ReturnsReadOnly` |
| F58 | `Seed` | a pair already recorded; a key with no role | skipped, nil | idempotent | `TestSeed_Twice_AddsNothing` |
| F59 | `ExportPerson`/`DeletePerson` | zero ID | Invalid | 400 | `TestDeletePerson_ZeroID_ReturnsInvalid` |
| F60 | `DeletePerson` | the person is the last access admin | Conflict (`last_access_admin`) | people fails the deletion with the reason | `TestDeletePerson_LastAccessAdmin_ReturnsConflict` |
| F61 | `DeletePerson` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | 503 / exit 1 | `TestDeletePerson_ReadOnly_ReturnsReadOnly` |
| F65 | `CreateRule` | the rule would leave no effective access admin | Conflict (`last_access_admin`) | explain; nothing written | `TestCreateRule_LeavesNoAccessAdmin_ReturnsConflict` (D37) |
| F66 | `DeletePerson` | the person held grants | nil; one `access.grant-revoked` per removed grant | none | `TestDeletePerson_PublishesGrantRevokedPerGrant` (D37) |
| F67 | `HoldersOf` | `p` not declared | Invalid | fix the caller | `TestHoldersOf_Undeclared_ReturnsInvalid` (D39) |
| F68 | `HoldersOf` | holders with an ended grant, a rule forbidding `p`, a superuser, a department-only grant | only site-wide, effective, non-superuser holders are returned | none | `TestHoldersOf_Table` (D39) |
| F69 | `HoldersOf` | the store fails | Unavailable, Timeout, Canceled | the caller decides (the alert adapter logs; people fails the request) | `TestHoldersOf_StoreFails_ReturnsKind` (D39) |
| F62 | handlers | malformed path ID | Invalid | 400 | `TestHandlers_BadID_Returns400` |
| F63 | handlers | denial on a signed-in route | 403 problem body via `httpx.Fail` | shell forbidden state | `TestHandlers_Permission_Returns403` |
| F64 | `Grant` | the actor has no `core.person` row (FK `role_grant_granted_by_fkey`) | Invalid (`actor` `unknown`) | explain | `TestGrant_ForeignKeyViolation_NamesTheOffendingField` |

`ListRules`, `ListPermissions`, `TitlesOf` and `ExportPerson` have no modes beyond permission (where it applies) and the store kinds: they read fixed data with no input to validate besides the IDs covered above. `Extent.Covers`/`IsZero` and `Cursor.String` cannot fail (pure).

## Lock-in check

No external technology beyond PostgreSQL through `db` (D8); `pg_advisory_xact_lock` and partial unique indexes are PostgreSQL features available in every OSI-licensed release ≥ 16 (db decision 20). No cloud IAM, no policy engine; the decision is three SQL statements that run on the self-hosted database the regional already has.

## Test plan

- **Unit** (`internal/core/authz`, no infrastructure): a hand-written fake store (`storetest` inside the package), `clock.Fake`, `audittest.NewFake("access")`, `signalstest.NewFake("access", {grant-created, grant-revoked}, nil)`, `telemetrytest.New(t)` (asserts no person ID leaks beyond IDs and no denied key); an `ActorFunc` the test controls (`authztest.WithActor`). `Seed` audit: `TestSeed_UpgradeAddsPermission_RecordsAudit` (D37; a second run records nothing). Table tests for the decision algorithm (site, department, ended, base, rule by permission, rule by level, superuser, in-delivery), the no-escalation rule, the last-admin invariant, the department-only views. Every F row above.
- **Permission tests:** each use case has a `_NoPermission_ReturnsPermission` and, where the extent matters, a department-only case; `ListGrants` and `GetRole` answer `NotFound`/filtered, never `Permission`, outside the extent.
- **Integration** (`//go:build integration`, `dbtest.TempDB`): migration applies on an empty database; the eight seeded roles; `Seed` twice adds once; unique index gives `Conflict duplicate` under two concurrent grants; the advisory lock serialises two concurrent revocations of the two last admins (exactly one succeeds); keyset pagination over 250 grants; `SetReadOnly` paths.
- **Conformance:** `authztest.Fake` passes the same decision table as the real service (`TestFake_MatchesService` over shared cases) so modules' tests mean something. `moduletest.Conformance` uses it (Proposed change 2).
- **app end-to-end** (Proposed change 1): `bms migrate` seeds; the superuser grants `access-admin` to a fresh person (`POST /api/access/grants`); that person signs in, `GET /api/accounts/session` lists `access.*`; `/access/roles` renders at the phone viewport; a plain participant's `GET /api/hello/greeting` is `200`; a grant that violates a rule answers `409 separation_of_duties`; a revoke of the last access admin answers `409 last_access_admin`.
- **Web** (`web/src/core/access/`): component tests for each screen's loading, empty, error, forbidden and read-only states; axe checks; the Playwright smoke run adds "grant a role, see the menu change" at the phone viewport.

## Amendment 2026-10-19 (pending owner approval): `HoldersOf` and the base role

Question raised while applying D39: `HoldersOf` (Interface, F67 to F69) returns explicit grant holders only and never enumerates the base role. **Confirmed as drafted.**

Reason. `HoldersOf` exists to find the people appointed to a job: the superuser alert goes to the holders of `accounts.manage`, the deletion notice to the holders of `people.data.delete` (D39). The base role is every signed-in person (decision 2). A permission a regional puts there has no appointed holders, and enumerating `core.person` would e-mail every participant: at best spam, at worst a leak (every participant would learn that a deletion request or a superuser sign-in happened). A permission in the base role *and* in a granted role yields the granted holders, once each. The consumers already handle "nobody": people F61 logs one info line and still creates the request; the alert adapter still sends to `notify.alert_addresses`. The roles screen needs no warning: `HoldersOf` is not on a route, and which permissions are "appointment" permissions is each consumer's business.

Not chosen: an `IncludeBase` flag (no consumer wants it), and reading `core.person` when the base role carries `p` (the spam and leak above, and a table authz does not read: decision 14).

### Failure modes (added)

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F70 | `HoldersOf` | `p` is carried by the base role only (no active grant holds a role with `p`) | `[]model.PersonID{}`, nil; signed-in people are not enumerated | treat as "nobody appointed" (people F61; the alert adapter falls back to the alert addresses) | `TestHoldersOf_BaseRoleOnly_ReturnsEmpty` |
| F71 | `HoldersOf` | `p` in the base role and in a role granted site-wide to A | `[A]`, once | — | `TestHoldersOf_BaseAndGrant_ReturnsGrantHoldersOnce` |

## Open questions

None. The five questions were answered by the owner on 2026-10-18 (all recommendations accepted) and are folded into the Key decisions: Q1 (no-escalation binds department-scoped granters only) in decision 5; Q2 (self-grant and self-revoke refused, superuser exempt) in decision 9; Q3 (every starter role but the base role may be deleted) in decision 2; Q4 (undeclared permissions allowed, shown as "not available") in decision 13; Q5 (grants to the superuser's own person not blocked) in decision 6.

## Proposed changes elsewhere

Status as of 2026-10-19: all 18 items are applied; item 11's end-of-day zone and item 12 are recorded as Later.

1. (applied 2026-10-18, in app.md, row 4 of "How later core services plug in") **`plan/design/app.md` (row 18), "How later core services plug in" row 4 and `internal/app`:** part 4 `authz` in `core_authz.go`: `declare` adds `authz.Set` to `coreSets` (set name `authz`, already in db's fixed order), `authz.Permissions` to `corePerms`, `access.grant-created` and `access.grant-revoked` to `coreSignals`; `build` calls `authz.New` over `a.dbc`, the clock, telemetry scopes `authz`, `a.deps.audit.Scoped("access")`, `a.disp.Scope("access", {the two}, nil)`, the actor closure `func(ctx) (authz.Actor, bool) { p, ok := identity.PersonFrom(ctx); s, _ := identity.SessionFrom(ctx); return authz.Actor{Person: p, Superuser: s.Superuser}, ok }`, `Declared: append(a.corePerms, a.plan.Permissions()...)`, `Departments: nil` (M3 sets a lazy adapter); sets `a.permissions = svc`, `a.titles = svc`, `a.deps.authz = svc` (provision copies it to `Deps.Authz`, unscoped), route set `access` (`authz.Route` → `module.Route`, all signed-in). **identity's build** passes `Permissions: lazyPermissions{a}` (the existing adapter, moved to a shared file) and `Lister: lazyLister{a}` (empty until `a.lister` is set by authz's part), since identity is built before authz. **`migrate`** gains a step after `db.Migrate` on the writer: for each part with a `seed func(ctx, *assembly) error`, run it; authz's calls `Seed(ctx, declared)` and prints `access: <n> starter permissions seeded` when `n > 0`. The e2e test adds the steps in "Test plan". Mark Proposed change 11 (authz part) applied.
2. (applied 2026-10-18, in module-contract.md) **`plan/design/module-contract.md` (row 4) and `internal/core/module`, `internal/core/authz`:** add `Deps.Authz authz.Authorizer` (M2; unscoped; `Provision` sets it); add `PermissionDecl.StarterRoles []string` (each a valid ident, unique; `Validate` reports `starter_roles[j]`); `moduletest.Conformance` sets `Deps.Authz = authztest.NewFake()` when nil (denies everything, so a module that forgets its check fails its tests, not the burner); add the rule "a list, search, count or export use case filters by `ExtentOf`; a single-record use case uses `AllowIn` (department records) or `Allow`"; mark Proposed change 2 applied (`Reaches` shown in the editor, rules may refer to it, `core.modules.read` is api's).
3. (applied 2026-10-18, in module-hello.md) **`plan/design/module-hello.md` (row 4b) and `internal/modules/hello`:** the M2 step of decision 2: `permissionChecker` is `deps.Authz`; `Manifest.Permissions` set `StarterRoles: []string{authz.RoleParticipant}` on both (decision 11); the app e2e asserts `GET /api/hello/greeting` is `200` for a signed-in participant and `POST /api/hello/greetings` drives `deliver:hello.greeted`; migration `0002` (FK to a person row) stays with people.
4. (applied 2026-10-18, in identity.md) **`plan/design/identity.md` (row 6):** mark Proposed change 6 applied: actor through `app`'s closure; `Allow` nil for the superuser; `PermissionChecker` and `PermissionLister` implemented here; `PermissionsOf` returns every declared name for a superuser session and `p@<department-id>` entries; starter role `account-admin` holds `accounts.read`, `accounts.manage` (`identity.Permissions` gains `StarterRoles: []string{authz.RoleAccountAdmin}`); the base role holds nothing of identity's. `SessionInfo.Permissions` doc and `openapi.json` description become "the permission names the person holds: `p` site-wide, `p@<department-id>` for one department".
5. (applied 2026-10-18, in audit.md) **`plan/design/audit.md` (row 12):** mark Proposed change 4 applied: `TitleSource` returns the keys (or renamed names) of the non-base roles held now; `PermissionChecker` is `Allow`; `audit.Permissions` gains `StarterRoles: []string{authz.RoleAuditReviewer}`; authz records through `Scoped("access")` the actions listed here; the session is ignored inside `InDelivery`.
6. (applied 2026-10-18, in api.md) **`plan/design/api.md` (row 16) and `internal/api`:** mark Proposed change 10 applied: `Allow` satisfies `PermissionChecker`; the declaration of `core.modules.read` gains `StarterRoles: authz.SiteAdminRoles` (api already imports `core/authz`); `InDelivery` refused.
7. (applied 2026-10-18, in web-shell.md) **`plan/design/web-shell.md` (row 17) and `web/src/shell/session.ts`:** mark Proposed change 6 applied: `session.permissions` holds `p` and `p@<department-id>`; `NavEntry.access: string[]` matches an entry when the session holds `p` exactly or any `p@...`; **remove the M1 placeholder** that shows every permissioned entry when the list is empty (an empty list now means none); decision 3 lists `web/src/core/access/` as the third core area; the smoke run adds "grant a role and see the menu change" at the phone viewport; `ui` gains a date-only input helper if `Field` lacks one.
8. (applied 2026-10-18, in signals.md) **`plan/design/signals.md` (row 10):** mark Proposed change 7 applied for authz (`Unauthenticated` inside `InDelivery`, decision 7); `Env.CoreSignals` gains `access.grant-created` and `access.grant-revoked` (`grant`, `person`, `role`, `department?`).
9. (applied 2026-10-18, in people.md, which carries (a) to (g): `ExtentOf` any-extent checks, the `Access` seam and the `Conflict last_access_admin` rollback, `GET /api/people/search` behind `people.directory.read` and `GET /api/people/names` for any signed-in person, the "Roles" link, `StarterRoles` per its Permissions table with contacts in `account-admin` only, migration `0002`, `NameSource` unchanged) **`plan/design/people.md` (row 8, unwritten):** (a) take the `Authorizer` from `app` and filter every list and search with `ExtentOf`; (b) call `authz.ExportPerson`/`DeletePerson` in the person export and deletion (deletion in the same transaction; a `Conflict last_access_admin` fails the deletion with that reason); (c) ship `GET /api/people/search?q=&limit=` (id, playa name; `internal`; for any signed-in person, or name the permission) and `GET /api/people/names?ids=` (batch, ≤200) for the access screens and audit's; (d) the person page links to `/access/grants?person=<id>` ("Roles") for holders of `access.grants.read` and the person themselves; (e) declare `StarterRoles` on its permissions (recommended: reading `personal` fields in `account-admin` and `department-admin`); (f) may propose an authz migration `0002` adding `FOREIGN KEY (person_id) REFERENCES core.person(id)` to `core.role_grant` once `core.person` exists (people's set precedes authz's in `coreSetOrder`); (g) `NameSource` for identity, unchanged.
10. (applied 2026-10-18, in departments-events.md, D37) **`plan/design/departments-events.md` (row 9):** implement `authz.DepartmentSource` (`Active` = exists and not archived) and have `app` set it through a lazy adapter (part 6 is built after authz); declare `StarterRoles` on its permissions (`department-steward`, `event-admin`, `department-admin`); ship a departments list route the grant form uses; "set or reset leadership" is granting `department-admin` through `access.department-grants.manage`; archiving a department leaves its grants in place (restore keeps leadership) and its own services refuse writes on an archived department.
11. (applied 2026-10-19, D39, in settings.md: `settings.read` and `settings.manage` carry `StarterRoles: {authz.RoleSettingsAdmin}`; notify's two permissions do the same; the end-of-day zone for grant end dates is Later: authz reads `Regional().Timezone` through a `ZoneSource` seam when it adopts it, and `app` can pass the settings service directly because settings is built before authz's adoption) **`plan/design/settings.md` (row 15):** declare `StarterRoles: {authz.RoleSettingsAdmin}` on its permissions; the end-of-day conversion for grant end dates uses the regional's time zone once it exists.
12. (applied 2026-10-19, D39: becomes Later; notify subscribes to no signal, and authz would send "you were appointed" itself through a `Notifier` seam when wanted) **`plan/design/notify.md` (row 11):** may subscribe to `access.grant-created` for "you were appointed" (no role name in the e-mail body beyond a link).
13. (applied 2026-10-18, in 07-abstractions.md) **`plan/07-abstractions.md`, "authz":** Owns becomes: "the permission catalogue (declared by the core and modules, with scope and `Reaches`); roles (site or department scoped, the base role, the starter roles seeded once from `PermissionDecl.StarterRoles`); grants (site-wide or for one department, optional end date); separation-of-duties rules checked at grant time and on every decision; the `Authorizer` (`Allow`, `AllowIn`, `ExtentOf` as the batch primitive); `PermissionsOf` for the session and `TitlesOf` for audit; the `access` routes and screens; signals `access.grant-created|revoked`". Doesn't do adds: "own-record checks (each service, through `identity.PersonFrom`), whether a person or department exists (`people`, `departments-events`), positions and qualifications (`volunteers`)".
14. (applied 2026-10-18, in 09-core-features.md §4 and §5, with Open question 1 settled: only department-scoped granters are bound) **`plan/09-core-features.md` §4 and §5:** pending Open question 1, "A person can't grant a permission they don't hold, or outside the department they hold it for" becomes "A department admin can't grant a permission they don't hold, nor outside their department; site-wide access admins grant any role within the separation-of-duties rules"; §4's starter table gains the `access.department-grants.manage` note for stewards; §5 "Leadership is a department-scoped grant" names the `department-admin` role.
15. (applied 2026-10-18, in telemetry.md) **`plan/design/telemetry.md` (row 15b):** add `bms.authz.decisions{outcome}` to the instrument table.
16. (applied 2026-10-18, in db.md) **`plan/design/db.md` (row 5):** mark Proposed change 10 applied for authz (`postgres.Migrations`, `authz.Set`, schema `core`, set name `authz`).
17. (applied 2026-10-18, in the go-conventions and add-module skills) **`.claude/skills/go-conventions/SKILL.md` and `add-module/SKILL.md`:** the M2 step: the permission checker seam is `deps.Authz`; "each use case checks its permission via authz first" gains the three shapes (`Allow`, `AllowIn`, `ExtentOf` for lists); a module spec's Permissions table gets a "Starter roles" column mapping to `PermissionDecl.StarterRoles` (keys from `authz.Role*`); module tests use `authztest.NewFake()` and its `Grant`/`GrantIn`/`Superuser`.
18. (applied 2026-10-19, D39, from notify.md Proposed change 6) **`plan/design/notify.md` and `plan/design/people.md`:** add `HoldersOf(ctx, p Permission) ([]model.PersonID, error)` on `*Service` (Interface, F67 to F69): the persons who hold `p` site-wide and effectively now, superusers excluded; a system read with no actor and no permission check, never on a route; consumers `app`'s alert adapter and people's deletion notice.
