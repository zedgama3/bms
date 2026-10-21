# Settings

- **Status:** Implemented (2026-10-19; approved D39; logo deferred to files)
- **Amendment 2026-10-19 (approved, owner, 2026-10-20, D40):** three gaps found while applying D39: `Options.Authz` at part 3 is `app`'s `lazyAuthorizer` (no part reorder); `GET /public` and `/theme.css` keep the `/api/` `no-store` rule with their `ETag`; `api.Options.PublicOrigin` waits, with its shape fixed over `regional.public_url`. See "Amendment 2026-10-19 (approved, owner, 2026-10-20, D40): authz before authz, caching, the public origin" before Open questions.
- **Amendment 2026-10-20 (approved, owner, D44; files Proposed change 6):** the logo, deferred by D39, lands with `files`: `Options.Files files.Store`, three routes under `/api/settings/logo`, `Public.Logo`, audit actions `settings.logo.changed|removed`, a logo field on `/settings`; decision 8 is superseded. See "Amendment 2026-10-20 (D44): the logo" before Open questions.
- **Accepted 2026-10-20 (owner, D40):** the implementation deviations marked "(implementation, 2026-10-19/20)" in this spec are accepted as written; no text changed.
- **Amendment 2026-10-21 (awaiting owner approval): the logo, designed over the built `files` area.** The D44 amendment named the routes; this one makes them implementable now that `files` and `privacy-tags` are Implemented: `Options.Files` (a lazy `files.Store` scoped `settings` from `app`), the fixed record `LogoRef`, three use cases `SetLogo`, `RemoveLogo`, `OpenLogo` behind `GET|PUT|DELETE /api/settings/logo`, the logo's file ID in the snapshot so `Public.Logo` and the `ETag` move together (decision 16), the cap `MaxLogoBytes` 2 MiB enforced by a counting reader before files' site-wide policy, `files.Brand` (PNG, JPEG, WebP; no SVG, Q-LOGO1), audit `settings.logo.changed|removed` with the revision lock serialising logo writes, read-only behaviour, no export or deletion (nothing personal), failure modes F37 to F54, the `/settings` logo field and the header's `<img>` (web-shell's). Inline edits are marked "(logo, 2026-10-21)"; the record is "Amendment 2026-10-21: the logo over files" before Open questions. Four open questions (Q-LOGO1 to Q-LOGO4) with recommendations; Proposed changes 14 to 19.
- **Package:** `internal/core/settings` (store in `internal/core/settings/postgres`, fake in
  `internal/core/settings/settingstest`)   **UI:** `web/src/core/settings/`
- **Depends on specs:** lib-errs, lib-core, lib-secret, model, config, db, audit, authz,
  telemetry, module-contract (`Plan.Settings()`), api (route mounting), web-shell, files
  (the logo's bytes; logo, 2026-10-21)
- **Used by:** every module (`Deps.Settings`), identity and people (`PolicySource`), audit
  (`audit.retention`, `audit.self_view`), notify (sender, regional name, public URL, terms),
  api (trusted origin, Later), authz (the regional's zone, Later), the web shell
  (`GET /api/settings/public`, `/api/settings/theme.css`), departments-events
  (the new-event form's default zone)

## Summary

**Purpose.** Hold what a regional configures while bms runs: typed, validated values with
defaults, declared by the core and the modules, edited by a settings admin, audited, and
effective without a restart. Package `internal/core/settings`; imports `model`, `lib`,
`core/db`, `core/audit`, `core/authz` (Authorizer, leaf types, `ActorFunc`),
`core/telemetry`, `core/files` (the `Store` interface and its types for the logo; files never
imports settings, so no cycle; logo, 2026-10-21). Never `core/module`, `core/identity`, `api`, `app`, `config` or a module.
Configuration (`config.md`: what reaches the database, listens, is a credential of the
process, or differs per process) is not here, and nothing here is read from the environment.

**Exported (leaf types, aliased by `module`).** `Kind` (`Bool`=1, `Int`, `Duration`, `Text`,
`Enum`, `Secret`; zero invalid; `String`), `Decl{Key, Kind, Default, Options, Check func(string) error}`
(+`Validate`), `Area{Name, Core bool, Decls []Decl, Check func(map[string]string) error}` (+`Validate`).
`module.SettingDecl = settings.Decl`, `module.SettingKind = settings.Kind`, `module.SettingBool = settings.Bool`, ... (Proposed change 1).

**Exported (service).**
- `Reader` (= `module.Deps.Settings`, scoped to one area; keys are bare): `Bool`, `Int` (int64),
  `Duration`, `Text` (Text and Enum), `Secret(ctx, key) (secret.Secret, error)`.
- `Regional{Name, ContactEmail, PublicURL, Language, Timezone string; Languages []string}`;
  `Forms{Singular, Plural, SingularTitle, PluralTitle string}`; `Public{Regional;
  Theme bool; Logo string (D44; the logo's file ID from the snapshot, `""` when none; logo, 2026-10-21); Terms map[string]map[string]string}`; `LogoResult{Revision, Logo string}` (logo, 2026-10-21); `Document`, `AreaDoc`, `SettingDoc`,
  `Changes{Values map[string]*string}`, `TermChanges{Terms map[string]FormChanges}`,
  `FormChanges{Singular, Plural, SingularTitle, PluralTitle *string}`, `Revision int64`.
- `Options{DB, Clock, Logger, Tracer, Meter, Audit audit.Recorder (scoped "settings"), Authz
  authz.Authorizer, Actor authz.ActorFunc, Files files.Store (optional, D44; scoped `settings`, lazy from `app`; nil = the logo routes answer `Unsupported`; logo, 2026-10-21), Areas []Area, SealingKey secret.Secret (optional),
  RefreshInterval (default 30 s, 1 s..10 m)}` (+`Validate`); `New(Options) (*Service, error)` (no I/O).
- `*Service` (safe for concurrent use): `Scoped(area) (Reader, error)`; `Regional(ctx)`; `TermsFor(ctx, lang)
  (map[string]Forms, error)`; `Public(ctx)`; `ThemeCSS(ctx) (string, error)`;
  `Document(ctx)`; `Set(ctx, Changes) (Revision, error)`; `Terms(ctx) (TermsDocument, error)`;
  `SetTerms(ctx, lang, TermChanges) (Revision, error)`;
  `Run(ctx) error` (the refresher, an `app` runnable); `Routes() []Route`; `OpenAPI() []byte`. The logo (D44; designed logo, 2026-10-21): `SetLogo(ctx, name string, body io.Reader) (LogoResult, error)`, `RemoveLogo(ctx) (LogoResult, error)`, `OpenLogo(ctx) (files.File, files.Content, error)`; routes `GET /logo` (public; `files.Serve`), `PUT /logo` (multipart field `file`, `settings.manage`), `DELETE /logo` (`settings.manage`); `Route` gains `MaxBodyBytes`, `Timeout` (`app` copies them); audit `settings.logo.changed|removed`.
- Constants: `Languages = []string{"en", "es"}`; `CoreTerms = []string{"event", "department", "participant", "theme_camp"}`;
  `MaxBatch = 200`, `MaxTextBytes = 4096`, `MaxNameBytes = 80`, `MaxURLBytes = 256`, `MaxTermBytes = 60`,
  `MinContrast = 4.5`, `MaxLogoBytes = 2 << 20`, `LogoRef = audit.Ref{Kind: files.LogoRecordKind, ID: sha256("settings.logo")[:16]}` (a `var`; logo, 2026-10-21),
  `DefaultRefreshInterval = 30 * time.Second`, `PermissionRead = "settings.read"`, `PermissionManage = "settings.manage"`.
- `settingstest.NewFake(area string, decls []Decl) *Fake` (a `Reader` answering the defaults; `Set(key, value)`, `SetSecret(key, secret)`; safe for concurrent use).

**Key decisions.** 1 one text form per value, parsed by `Kind`, stored only when it differs from the default (a reset deletes the row). 2 the declaration types move from `module` to `settings` with aliases, as authz's and signals' did, because `settings` cannot import `core/module`. 3 (owner, 2026-10-19: the SMTP password lives in settings, sealed) a sixth kind, `Secret`: sealed at rest with AES-256-GCM under a key derived from `settings.sealing_key` (a generated field of the secrets file, config Proposed change 9), never returned by any route (`secret_set` only), read only by Go consumers. 4 one in-memory snapshot per process, loaded on first read, replaced after each local commit and by a 30 s revision poll (`core.setting_revision`), so a read is a map lookup and a replica follows the writer without LISTEN/NOTIFY (which a standby cannot receive). 5 a stored value that no longer parses after an upgrade yields the default and a warning, never an error. 6 the regional's own values (`regional.*`) are a core area declared here; only they are public. 7 (owner, 2026-10-19: four forms) terminology is data: four forms per term per language, defaults embedded here (`terms/<lang>.json`), overrides in `core.terminology`; the shell's `term.*` catalogue entries stay as the boot fallback and must equal the defaults (test). 8 (superseded 2026-10-20, D44) the logo was deferred to the `files` spec (owner, 2026-10-19); it now lives here, stored and served by `files` (`Options.Files`, `GET|PUT|DELETE /api/settings/logo`, `Public.Logo`, audit `settings.logo.changed|removed`, `MaxLogoBytes` 2 MiB; the amendment before Open questions). 9 (owner, 2026-10-19) colours are one brand colour per scheme, checked for 4.5:1 against the shipped background, served as `/api/settings/theme.css` (light inside `@media not (prefers-color-scheme: dark)`, dark inside `(prefers-color-scheme: dark)`, an unset scheme emits nothing; implementation, 2026-10-19). Duration `range` params are plain whole units (`1h`, `8760h`), not `1h0m0s`. 10 no signal on change: every consumer reads at use-case time from the snapshot; nothing downstream holds a copy. 11 no automatic retention keys per module: the module that runs the job declares its `<name>.retention*` keys when its spec has the job that enforces it (owner, 2026-10-19). 12 `regional` is a reserved area name. 13 values never appear in logs, audit entries, spans or error messages. 14 `settings.read` goes to `settings-admin` only; other admins see what they need on their own screens (owner, 2026-10-19). 15 (owner, 2026-10-20, D40) no early area check: an area `Check` runs only inside the `Set` transaction over stored values; `regional.public_url` is canonicalised (http/https, host required, no query/fragment/userinfo, trailing slashes removed); a `Check` cannot see whether a `Secret` is set and re-saving a `Secret` always counts as a change; the sealing and contrast tests are unit tests. 16 (logo, 2026-10-21, awaiting approval) **a logo change is a settings write**: it runs under the revision row lock, bumps the revision and replaces the snapshot, and the snapshot carries the logo's file ID (read with `ByRecord(LogoRef)` at load), so `Public.Logo`, the `ETag` of `GET /public` and a replica's poll all follow the logo without a second mechanism; `GET /logo` itself reads `ByRecord` (exact, not the snapshot). 17 (logo, 2026-10-21) one logo for both colour schemes, PNG, JPEG or WebP (`files.Brand`), at most `MaxLogoBytes` (2 MiB, a bound, not a setting) enforced by a counting reader in the handler before files' site-wide `files.max_bytes`; no SVG (an XML document that can carry scripts, event handlers and external references; needs a sanitiser files does not have, Q-LOGO1); no pixel-dimension check (the uploader holds `settings.manage`; a bomb would hurt only their own site's browsers). 18 (logo, 2026-10-21) the write refuses a read-only copy after the permission check and before the body is read, so no object is staged for nothing; `RemoveLogo` with no logo is a no-op (no audit, no bump), as `Set` with unchanged values.

**Errs kinds callers handle.** Readers: `Invalid` (undeclared key, kind mismatch), `Unavailable`/`Timeout`/`Canceled` (first load), `Unsupported` (`Secret` with no sealing key), `Internal` (unsealing failed). Use cases: `Unauthenticated`, `Permission`, `Invalid` with field = the setting key and codes `unknown`, `malformed`, `one_of`, `range`, `too_long {max}`, `required`, `control_chars`, `url`, `contrast {ratio, min}`, `rejected` (a `Check`'s reason), `too_many {max}`; `Unsupported` (a secret without a sealing key); `Unavailable` wrapping `db.ErrReadOnly` on every write; `Run`: `Conflict` (started twice). The logo (logo, 2026-10-21): `SetLogo`: `Unauthenticated`, `Permission`, `Unsupported` (no `Files`), `Unavailable` wrapping `db.ErrReadOnly` (before the body is read), `Invalid` with field `file` and codes `required`, `too_large {max}`, `type`, `malformed`, `Conflict quota {max}`, `Unavailable`, `Timeout`, `Canceled`, the recorder's kind; `RemoveLogo`: the same minus the file codes; `OpenLogo`: `Unsupported`, `NotFound` (no logo), `Unavailable` wrapping `objects.ErrMissing` (a copy without the bytes), `Unavailable`; the handler: `Invalid` wrapping `httpx.ErrUnsupportedMediaType` (not multipart). No sentinels.

**Owns.** Tables `core.setting`, `core.setting_revision`, `core.terminology` (nothing personal); permissions `settings.read`, `settings.manage` (site, reach `internal`, starter `settings-admin`); settings area `regional` (`name`, `contact_email`, `public_url`, `language`, `timezone`, `color_primary`, `color_primary_dark`); route set `settings`: `GET /public`, `GET /theme.css`, `GET /logo` (public), `GET /{$}`, `PUT /values`, `GET /terms`, `PUT /terms/{lang}`, `PUT /logo`, `DELETE /logo`; the file record `LogoRef` (kind `settings.logo`, one file, level `public`, in files' bucket `settings`); screens `/settings` (with the logo field), `/settings/terms`, `/settings/:area`; keys `core.settings.*` (`core.settings.logo.*` for the field), `core.regional.setting.*`; audit actions `settings.setting.changed|reset`, `settings.term.changed|reset`, `settings.logo.changed|removed`; spans `settings.<use-case>` (`settings.logo.set|remove|open`), `settings.load`, `settings.refresh`; metrics `bms.settings.refresh{outcome}`, `bms.settings.stale_values`. Config key proposed: `settings.sealing_key` (`BMS_SETTINGS_SEALING_KEY`, generated into `secrets.json` as `settings_sealing_key`). No signals.

**Expects of dependencies.** `app`: part 3 (`declare`: `Set`, `Permissions`; `build`: `New` over the areas (`regional` from this package, `identity.Settings`, `people.Settings`, `auditsettings.Settings`, `Plan.Settings()` as module areas), the actor closure, `audit.Scoped("settings")`, `cfg.Settings.SealingKey`; `a.deps.Settings` scoped per module in `provision`; `PolicySource` adapters for identity and people; `Run` as a runnable; route set `settings`). `module-contract`: the aliases, `Deps.Settings settings.Reader`, `moduletest.Conformance` supplying `settingstest.NewFake`, `regional` reserved. `identity`, `people`, `audit`: export `Settings []settings.Decl` (identity also `CheckSettings`). `authz`: `Allow`, starter-role seeding. `audit`: `Recorder` inside a `Write` transaction. `db`: `Transact`, `AfterCommit`, `Wrap`, `ErrReadOnly`, `Set`. `web-shell`: reads `GET /api/settings/public` at boot, merges `terms` over its catalogue, links `/api/settings/theme.css`, adds the three term forms to its catalogue and the `field.contrast` key; (logo, 2026-10-21) owns `upload()` in `@/api`, `ui/FileField` and the header `<img src="/api/settings/logo?v=<Public.logo>" alt="<regional name>">` with its text fallback (Proposed change 15). `notify`: declares `notify.*` (named under Settings) and reads `Regional`, `TermsFor` and its own `Reader`. `files` (logo, 2026-10-21): a `Store` scoped `settings` (`Stage` outside the transaction, `Attach{Record: LogoRef, Level: public, Replace: true}` inside it with no store I/O, `ByRecord` usable inside a `Read` transaction, `Open` and `Serve` with no `Cache-Control`, decision 25), `files.Brand`, `LogoRecordKind`, a classified error from `Upload.Body` returned unchanged by `Stage` (Proposed change 16). `app` (logo, 2026-10-21): `Options.Files` as a lazy adapter resolved on first use, like `lazyAuthorizer`; `settingsRoutes` copies `MaxBodyBytes` and `Timeout` (Proposed change 14).

**Open questions:** the six of 2026-10-19 were answered (D39) and are folded into decisions 3, 7, 8, 9, 11 and 14; (logo, 2026-10-21) four new ones with recommendations: Q-LOGO1 SVG (no, later with a sanitiser), Q-LOGO2 a dark-scheme logo (not now), Q-LOGO3 a favicon from the logo (no), Q-LOGO4 a pixel-dimension cap (no). **Settled 2026-10-20 (D40):** the amendment of 2026-10-19 is approved (`lazyAuthorizer`; `no-store` with `ETag`; `PublicOrigin` waits); decision 15 below; the implementation deviations marked 2026-10-19 are accepted as written. **Proposed changes elsewhere:** 13, all applied 2026-10-19 (D39), listed at the end; 14 to 19 (logo, 2026-10-21) await the owner.

## Purpose

Hold what a regional configures while bms runs, so that the core and every module read one typed, validated, audited value per setting without a restart.

## Responsibilities

- The declaration seam: the core areas and the modules declare their settings (`Decl`,
  `Area`); this spec validates the declarations once at construction and refuses duplicates.
- Storage of the values a regional sets, the defaults for everything else, and the one
  revision number every write bumps.
- Validation of every change: by kind, by the declaration's `Check`, and by the area's
  cross-field `Check`.
- The regional's own values (`regional.*`): name, contact mailbox, public URL (D18), default
  language (D28), time zone, brand colours.
- Terminology: the words a regional uses for the core's things, per language, in the forms
  the UI and notifications need.
- Reads: scoped `Reader`s for modules and core areas, the typed `Regional` and `TermsFor`
  for notify and the api, and the public document the shell boots from.
- Caching: one snapshot per process, refreshed after a local commit and by a revision poll.
- Audit of every change, permissions on every read of the admin document and every write,
  sealing of secret values at rest.
- (logo, 2026-10-21) The regional's logo: its one file record (`LogoRef`), the three routes
  that upload, replace, remove and serve it, its cap and accepted kinds, its audit entries,
  and its file ID in the public document. The bytes are files'.
- The admin screens.

## Not responsible for

- **Process configuration** (what reaches the database, listens, is a credential of the
  process, or differs per process): `config.md`. Its rule decides which side a value is on;
  this spec adopts it (config.md "Settings").
- **Which modules are enabled**: `config` (`modules.disabled`) and `module-contract`.
- **What each setting means**: the declaring spec (`accounts.*` identity, `people.*` people,
  `audit.*` audit, `notify.*` notify, `<module>.*` the module). This spec stores, checks the
  shape, and hands the value back; it never interprets one, except its own `regional.*`.
- **Enforcing retention**: `jobs` (the retention job reads the declaring area's key).
- **Translations of the UI**: the web catalogues (web-shell decision 8). Terminology here is
  the regional's override of a handful of words, not a translation layer.
- **Browser language detection** (`language.detect_browser`, web-shell Proposed change 7):
  Later, declared here when the shell implements its side.
- **A history of past values**: not kept in M4; the audit log says who changed what and
  when, the default is always shown, and the admin re-enters a value. Later if asked.
- **Department-scoped or per-event settings**: Later (module-contract Proposed change 5
  "site-wide ones only"); a module that needs a per-department choice stores it as its own
  data.
- **Storing and serving the logo's bytes:** `files` (the object store, the catalogue entry `files.Brand`, the sniff, the metadata strip, `files.Serve`, the tombstone and the sweep). The logo's routes, screen field, size cap and audit actions are here (D44, replacing D39's deferral); no other brand image exists. (logo, 2026-10-21) Sanitising an SVG, should one ever be accepted: a later `files` catalogue entry (Q-LOGO1). The header `<img>`, `upload()` and `ui/FileField`: `web-shell` (its "M4 files additions", D44; Proposed change 15 gives them the detail the logo field needs).
- **Brand fonts** (web-shell decision 15, Later), a favicon, e-mail templates (`notify`).
- **Personal data**: nothing here is about a person; no `PersonData`, no export, no deletion.
- **Rotating the sealing key**: a later `rotate-secret` command (config.md decision 7 says the
  same for the session key).

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| Declarations | Give every setting one typed, defaulted, validated declaration. | `Kind`, `Decl`, `Area`; aliased by `module` |
| Values | Store and read the values a regional set, defaults for the rest. | `core.setting`, `Reader`, `Set`, `Document` |
| Snapshot | Make every read a map lookup and every process follow the writer. | load, after-commit replace, revision poll (`Run`) |
| Sealing | Keep a secret value unreadable at rest and never returned. | AES-256-GCM, `Secret` kind, `Reader.Secret` |
| Regional | The regional's identity, language, zone and colours. | area `regional`; `Regional`, `Public`, `ThemeCSS` |
| Terminology | The regional's words for the core's things, per language, in four forms. | `core.terminology`, `terms/<lang>.json`, `TermsFor` |
| Logo (logo, 2026-10-21) | Keep one public image as the regional's logo, replaceable by a settings admin. | `LogoRef`, `SetLogo`, `RemoveLogo`, `OpenLogo`; bytes in `files` |
| Routes and screens | The public document, the admin document, the forms, the logo routes. | route set `settings`, `web/src/core/settings/` |

## Interface

```go
package settings

// ---- Declarations (leaf types; module aliases them) ----

// Kind is the type of a setting's value. The zero value is invalid.
type Kind uint8

const (
	Bool     Kind = iota + 1 // "true" or "false"
	Int                      // a base-10 integer (int64)
	Duration                 // time.ParseDuration form ("72h"); days are written as hours
	Text                     // free text, at most MaxTextBytes
	Enum                     // exactly one of Options
	Secret                   // free text, sealed at rest, never returned; Default must be ""
)

// String returns "bool", "int", "duration", "text", "enum", "secret" or "Kind(<n>)". Cannot fail.
func (k Kind) String() string

// Decl declares one setting of an area. Its title and description are message keys
// (module-contract decision 8): module.<area>.setting.<key>.title for a module,
// core.<area>.setting.<key>.title for a core area; an Enum's options
// module.<area>.setting.<key>.option.<opt>.
type Decl struct {
	Key     string   // one ident: [a-z][a-z0-9]*(_[a-z0-9]+)*, at most 64 bytes
	Kind    Kind     // how Default and every value are parsed
	Default string   // text form; parses as Kind and passes Check; "" for Secret
	Options []string // Enum only: at least two idents, no duplicates
	// Check is an optional extra rule on the text value, pure and safe for concurrent use.
	// It may return validate.FieldErrors (re-fielded under the setting key, codes kept)
	// or any error (code "rejected", reason = err.Error()).
	// A Check cannot see whether a Secret is set: for a Secret it receives the submitted
	// plaintext only, and an area Check sees "" for every Secret key, set or not, so no rule
	// may depend on a Secret already being set (owner, 2026-10-20, D40).
	Check func(value string) error `json:"-"`
}

// Validate fails errs.Invalid with one validate.FieldError per problem on "key", "kind",
// "options", "default" (module-contract F6, unchanged; plus: a Secret with a Default).
func (d Decl) Validate() error

// Area is one owner of settings: a module (Core false) or a core area (Core true).
type Area struct {
	Name  string // a module name or a core area name; "regional" is this package's own
	Core  bool
	Decls []Decl
	// Check is an optional cross-field rule over the area's effective values (every
	// declared key, defaults filled in, bare keys). Pure. Field names it returns are bare
	// keys and are prefixed with "<area>." by Set. Runs inside Set's Write transaction over
	// values read there, never over the snapshot (Behavior, "Writes"). A Secret key reads "".
	Check func(values map[string]string) error `json:"-"`
}

// Validate fails errs.Invalid: name not an ident, a Decl invalid, duplicate keys.
func (a Area) Validate() error

// ---- Reading ----

// Reader reads one area's settings by bare key. Every method fails errs.Invalid for a key
// the area did not declare or a kind that does not match, and with the first load's
// store kinds (Unavailable, Timeout, Canceled) when no snapshot exists yet. After that a
// read is a map lookup: no I/O, no error but the two above and Secret's.
type Reader interface {
	Bool(ctx context.Context, key string) (bool, error)
	Int(ctx context.Context, key string) (int64, error)
	Duration(ctx context.Context, key string) (time.Duration, error)
	// Text returns a Text or an Enum value.
	Text(ctx context.Context, key string) (string, error)
	// Secret unseals a Secret value; secret.Secret{} when unset. Fails errs.Unsupported
	// when the deployment has no sealing key and a value is set, errs.Internal when the
	// stored value cannot be unsealed (the key changed).
	Secret(ctx context.Context, key string) (secret.Secret, error)
}

// Regional is the regional's own values (area "regional"), defaults filled in.
type Regional struct {
	Name         string   // regional.name
	ContactEmail string   // regional.contact_email ("" = none)
	PublicURL    string   // regional.public_url ("" = unset; no trailing slash)
	Language     string   // regional.language, one of Languages
	Timezone     string   // regional.timezone, an IANA name or "" (the browser's)
	Languages    []string // the build's languages (Languages), offered to everyone
}

// Forms are the four forms of one term in one language.
type Forms struct {
	Singular      string `json:"singular"`       // "department"
	Plural        string `json:"plural"`         // "departments"
	SingularTitle string `json:"singular_title"` // "Department"
	PluralTitle   string `json:"plural_title"`   // "Departments"
}

// Public is what GET /public returns.
type Public struct {
	Regional
	Theme bool                         `json:"theme"` // theme.css is not empty
	// Logo is the logo's file ID ("" when none), from the snapshot (decision 16). The shell
	// requests /api/settings/logo?v=<Logo> so a replaced logo is a new URL. Public, like
	// every field here (decision 6). (logo, 2026-10-21)
	Logo string `json:"logo"`
	// Terms per language, flattened for the shell's {term_*} arguments:
	// {"en": {"term_department": "department", "term_department_plural": "departments",
	//         "term_department_title": "Department", "term_department_plural_title": "Departments", ...}}
	Terms map[string]map[string]string `json:"terms"`
}

// ---- Editing ----

// Changes is one batch of value changes: a text form sets, nil resets to the default.
type Changes struct {
	Values map[string]*string `json:"values"` // full keys "<area>.<key>", 1..MaxBatch
}

// Document is what GET / returns to a settings admin.
type Document struct {
	Revision Revision  `json:"revision"`
	Areas    []AreaDoc `json:"areas"` // regional first, then core areas, then modules in build order
}
type AreaDoc struct {
	Name     string       `json:"name"`
	Core     bool         `json:"core"`
	Settings []SettingDoc `json:"settings"`
}
type SettingDoc struct {
	Key       string   `json:"key"`     // full key
	Kind      string   `json:"kind"`    // Kind.String()
	Default   string   `json:"default"` // "" for a secret
	Value     *string  `json:"value"`   // nil = default applies; always nil for a secret
	SecretSet bool     `json:"secret_set"`
	Options   []string `json:"options,omitempty"`
	Stale     bool     `json:"stale"` // a stored value no longer parses (decision 5)
}

// TermsDocument is what GET /terms returns: every language, every term, default and override.
type TermsDocument struct {
	Languages []string                       `json:"languages"`
	Terms     map[string]map[string]TermDoc  `json:"terms"` // lang -> term -> doc
}
type TermDoc struct {
	Default  Forms       `json:"default"`
	Override FormChanges `json:"override"` // nil fields = not overridden
}
type TermChanges struct {
	Terms map[string]FormChanges `json:"terms"` // term -> forms; a nil field is unchanged
}
type FormChanges struct {
	Singular, Plural, SingularTitle, PluralTitle *string // "" is invalid; nil = unchanged
	Reset []string `json:"reset,omitempty"`              // forms to reset: "singular", ...
}

type Revision int64

// ---- The logo (logo, 2026-10-21) ----

// MaxLogoBytes caps the logo's bytes: 2 MiB, a bound, not a setting (a header image far
// below it; a photo above it is the wrong file). Enforced before files' site-wide
// files.max_bytes so the limit a settings admin sees is this one.
const MaxLogoBytes int64 = 2 << 20

// LogoRef is the one file record the logo is attached to: kind files.LogoRecordKind
// ("settings.logo", whose area is "settings", so files' ownership check passes for the
// Store scoped "settings") and a fixed ID, the first 16 bytes of sha256("settings.logo")
// (the fixed-ID pattern of departments-events decision 25). Pinned by TestLogoRef_Pinned.
var LogoRef = audit.Ref{Kind: files.LogoRecordKind, ID: logoID()}

// LogoResult is what PUT /logo and DELETE /logo answer.
type LogoResult struct {
	Revision Revision `json:"revision"`
	Logo     string   `json:"logo"` // the file ID after the call; "" after a removal
}

// ---- Service ----

type Options struct {
	DB              db.DB
	Clock           clock.Clock
	Logger          *slog.Logger
	Tracer          telemetry.Tracer
	Meter           telemetry.Meter
	Audit           audit.Recorder   // scoped "settings"
	Authz           authz.Authorizer
	Actor           authz.ActorFunc
	// Files is a files.Store scoped "settings" for the logo (logo, 2026-10-21). Optional:
	// nil means the three logo routes answer Unsupported and Public.Logo is "". files is
	// core part 10 and settings part 3, so app passes a lazy adapter resolved on first
	// use, as it does for Authz; a method called before files is built is Unavailable.
	Files           files.Store
	Areas           []Area           // every area but "regional", which New adds
	SealingKey      secret.Secret    // optional; without it Secret values cannot be set or read
	RefreshInterval time.Duration    // 0 = DefaultRefreshInterval; 1s..10m
}

// Validate fails errs.Invalid with fields settings.db, settings.clock, settings.logger,
// settings.tracer, settings.audit, settings.authz, settings.actor, settings.refresh_interval,
// settings.areas[i].<field>, settings.areas (duplicate area, an area named "regional").
func (o Options) Validate() error

// New validates and builds the service. No I/O: it succeeds against db.None().
func New(o Options) (*Service, error)

// Scoped returns the Reader for one declared area. Fails errs.Invalid for an unknown area.
func (s *Service) Scoped(area string) (Reader, error)

// Regional returns the regional's own values. Consumers: notify (name, public URL), the
// public and theme handlers, api (trusted origin, Later), authz (zone, Later).
func (s *Service) Regional(ctx context.Context) (Regional, error)

// TermsFor returns every core term in one language, overrides applied over the embedded
// defaults (a language without a defaults file uses "en"). Consumers: notify, GET /public.
// Fails errs.Invalid (lang not in Languages) and the snapshot's kinds.
func (s *Service) TermsFor(ctx context.Context, lang string) (map[string]Forms, error)

// Public builds GET /public. No permission: every field is public by decision 6.
func (s *Service) Public(ctx context.Context) (Public, error)

// ThemeCSS renders the regional's colours as a stylesheet ("" when both are default).
func (s *Service) ThemeCSS(ctx context.Context) (string, error)

// Document needs settings.read.
func (s *Service) Document(ctx context.Context) (Document, error)

// Set needs settings.manage; validates every change, then writes all or nothing, audits
// one entry per key inside the transaction, bumps the revision and replaces the snapshot
// after commit. Returns the new revision.
func (s *Service) Set(ctx context.Context, c Changes) (Revision, error)

// Terms needs settings.read.
func (s *Service) Terms(ctx context.Context) (TermsDocument, error)

// SetTerms needs settings.manage; same transaction rules as Set.
func (s *Service) SetTerms(ctx context.Context, lang string, c TermChanges) (Revision, error)

// SetLogo needs settings.manage (logo, 2026-10-21). Outside any transaction it refuses a
// read-only copy, then stages body (named name, at most MaxLogoBytes, a files.Brand kind
// by its bytes) in files' bucket "settings"; then, in one Write transaction under the
// revision lock, attaches it to LogoRef replacing any earlier logo, records
// settings.logo.changed, bumps the revision; after commit the snapshot carries the new ID.
// A staged object that was not attached is discarded. Consumer: PUT /logo.
func (s *Service) SetLogo(ctx context.Context, name string, body io.Reader) (LogoResult, error)

// RemoveLogo needs settings.manage. In one Write transaction under the revision lock it
// deletes LogoRef's files, records settings.logo.removed and bumps the revision; with no
// logo it changes nothing and answers the current revision. Consumer: DELETE /logo.
func (s *Service) RemoveLogo(ctx context.Context) (LogoResult, error)

// OpenLogo needs no permission (the logo is public). It lists LogoRef's files (the newest
// when more than one, which the revision lock prevents) and opens it; the caller serves
// with files.Serve and closes the Content. errs.NotFound when there is no logo. Consumer:
// GET /logo.
func (s *Service) OpenLogo(ctx context.Context) (files.File, files.Content, error)

// Run is the refresher: every RefreshInterval it reads core.setting_revision and reloads
// the snapshot when it moved. Returns nil when ctx ends; errs.Conflict if already running.
func (s *Service) Run(ctx context.Context) error

func (s *Service) Routes() []Route
func (s *Service) OpenAPI() []byte

// Route is the shape app converts to module.Route (audit decision 14). MaxBodyBytes and
// Timeout (logo, 2026-10-21) are module.Route's (api decision 32): zero = the shell's;
// app copies them. PUT /logo sets MaxBodyBytes = 2*MaxLogoBytes and Timeout =
// files.UploadTimeout; GET /logo sets Timeout = files.ServeTimeout.
type Route struct {
	Method, Pattern string
	Public          bool
	Handler         http.Handler
	MaxBodyBytes    int64
	Timeout         time.Duration
}

// IDOf derives the fixed audit reference for a key (as events does for kinds): the first
// 16 bytes of sha256("settings.setting:" + key). Cannot fail.
func IDOf(key string) [16]byte
```

**Consumers per method.** `Reader.Bool/Int/Duration/Text`: every module for its declared
keys (hello: `Text("greeting")`), app's `PolicySource` adapters for identity (`Enum`, `Duration`,
`Int`) and people (`Enum`), jobs for `audit.retention` (`Duration`); `Bool` has no core consumer
yet and exists because `Bool` is a declared kind a module may use. `Reader.Secret`: notify
(`notify.smtp_password`). `Regional`: notify, the handlers, api and authz Later. `TermsFor`:
notify's templates, `Public`. `Scoped`: app (`provision`, the core adapters). `Document`,
`Set`, `Terms`, `SetTerms`, `Public`, `ThemeCSS`: the
handlers. `Run`: app. `IDOf`: this package's audit entries and tests. (logo, 2026-10-21)
`SetLogo`, `RemoveLogo`, `OpenLogo`: the three logo handlers only; `LogoRef`: this package,
`filestest`-based tests, and `bms files verify`'s operator (the record kind it prints).

**Pre- and post-conditions.** Every use case checks its permission first (`Allow`), then
validates, then writes in one `Transact(Write)`. A write on a read-only copy fails
`Unavailable` wrapping `db.ErrReadOnly` and changes nothing. A successful `Set`/`SetTerms`
is visible to the next read in the same process before the call returns.

## Types

Defined above. Uses `model.Email` (`ParseEmail` for `regional.contact_email`),
`secret.Secret`, `validate.FieldError` and its codes, `authz.Permission`, `authz.PermissionDecl`,
`audit.Entry`, `audit.Ref`.

**Value grammar per kind.** `Bool`: `true`/`false`. `Int`: `strconv.ParseInt(v, 10, 64)`.
`Duration`: `time.ParseDuration` (a day is `24h`; audit's "365d" floor is `8760h`). `Text`,
`Secret`: valid UTF-8, no control characters other than `\n` and `\t`, at most `MaxTextBytes`.
`Enum`: one of `Options`. A value is trimmed of surrounding whitespace before parsing
(`Secret` is not trimmed).

## Data

Migration set `settings`, schema `core` (as the other core parts; db.md "core sets use
schema `core`"). Nothing here is about a person: no sensitivity levels, kept forever.

```sql
-- 0001_init.sql
CREATE TABLE core.setting (
  key        text PRIMARY KEY,            -- "<area>.<key>"
  value      text,                        -- the text form; NULL for a Secret
  sealed     bytea,                       -- nonce || ciphertext for a Secret; NULL otherwise
  updated_at timestamptz NOT NULL,
  CHECK ((value IS NULL) <> (sealed IS NULL))
);
CREATE TABLE core.setting_revision (
  id       smallint PRIMARY KEY CHECK (id = 1),
  revision bigint NOT NULL
);
INSERT INTO core.setting_revision (id, revision) VALUES (1, 1);
CREATE TABLE core.terminology (
  lang       text NOT NULL,               -- one of Languages
  term       text NOT NULL,               -- one of CoreTerms
  form       text NOT NULL,               -- singular | plural | singular_title | plural_title
  value      text NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (lang, term, form)
);
```

- A row in `core.setting` exists only for a key set away from its default; a reset deletes
  it. A row whose key is not declared in this process (a disabled or removed module) is
  kept, read by nobody, shown nowhere, and returns when the module is enabled again.
- Every write transaction ends with `UPDATE core.setting_revision SET revision = revision + 1
  WHERE id = 1 RETURNING revision`: the row lock serialises writers and orders snapshots.
- No `updated_by`: the audit log holds the actor, and a person foreign key would tie
  settings into people's deletion.
- `sealed` holds a 12-byte random nonce followed by the AES-256-GCM ciphertext; the AAD is
  the full key, so a ciphertext moved to another key fails to unseal.

## Permissions

| Permission | Allows | Scoped by | Reaches | Starter roles |
|---|---|---|---|---|
| `settings.read` | `GET /` and `GET /terms`: every declared setting's value (secrets as `secret_set`), the terms | site | internal | `settings-admin` |
| `settings.manage` | `PUT /values`, `PUT /terms/{lang}`, `PUT /logo`, `DELETE /logo` (logo, 2026-10-21) | site | internal | `settings-admin` |

(logo, 2026-10-21) `GET /logo` is public, like `GET /public`: the logo is attached at level
`public` and shown on every page to everyone, so `Open` audits nothing and needs no actor
(files decision 11). The logo record holds nothing about a person: no sensitivity level
beyond `public`, no `Subject`; `UploadedBy` is files' own column, handled by files' export
and deletion (an admin's export lists the logo under `uploaded`, with its name, since it
has no subject and is public: files decision 27).

`settings.manage` does not imply `settings.read`; the starter role holds both, and a regional
that gives a role `manage` alone gets a screen that can save and not show, which the screen
says. `settings.read` goes to `settings-admin` only (owner, 2026-10-19, D39): other admins see what
they need on their own screens, and a module's settings are that module's business. Public reads (`GET /public`, `/theme.css`) need no session: everything in
them is the site's own appearance (decision 6). Module readers (`Deps.Settings`) are not
permission-checked: a module reads its own declared keys as the system, like `Departments`.

## Signals

Published: none. Every consumer reads from the snapshot at use-case time (identity and
people on every policy use, notify per send, modules per use case), so nothing downstream
holds a copy to invalidate, and a signal with no subscriber is dead weight (decision 10).
A later consumer that must react (notify dropping a pooled SMTP connection) asks for
`settings.setting-changed {setting}` with `IDOf(key)` as the reference; the ID is already
defined so that addition is one manifest line.

Subscribed: none.

## Settings

Declared by this package in area `regional` (core; catalogue keys `core.regional.setting.<key>.*`,
shipped in `web/src/core/settings/messages/en.json`). All are public (decision 6).

| Key | Type | Default | Valid range | Who can change it |
|---|---|---|---|---|
| `regional.name` | text | `BMS` | 1..`MaxNameBytes` bytes after trimming, no control characters (`required`, `too_long`, `control_chars`) | settings admin |
| `regional.contact_email` | text | `` | `` or `model.ParseEmail` (`malformed`); the regional's shared mailbox, shown to anyone who opens the site; the form says so | settings admin |
| `regional.public_url` | text | `` | `` or an absolute URL, canonicalised on save by the rule under this table (`url`, `too_long`). The installation's identity (D18) and the base of every link notify sends | settings admin |
| `regional.language` | enum | `en` | `Languages` (`one_of`); the language before a person chooses (D28, web-shell decision 7) | settings admin |
| `regional.timezone` | text | `` | `` (the browser's zone) or an IANA name `time.LoadLocation` accepts, never `Local`, at most 64 bytes (`unknown`, `too_long`); the new-event form's default zone, authz's end-of-day Later | settings admin |
| `regional.color_primary` | text | `` | `` (the shipped palette) or `#rrggbb` (`malformed`); contrast against the shipped light background `#ffffff` at least `MinContrast` (`contrast {ratio, min}`) | settings admin |
| `regional.color_primary_dark` | text | `` | `` or `#rrggbb`; contrast against the shipped dark background `#121212` at least `MinContrast` | settings admin |

Declared elsewhere, stored here (the owning spec gives the ranges; `settings` only checks
shape and calls their `Check`): `accounts.*` (identity, eleven keys, `identity.CheckSettings`
for `client_failure_limit >= login_failure_limit`, `password_reset_client_limit >= password_reset_address_limit` and `password_reset_cooldown <= password_reset_lifetime`), `people.legal_name|phone|emergency_contacts`
(people), `audit.retention|self_view` (audit), `hello.greeting` and every module's
`Manifest.Settings`.

**Keys notify is expected to declare** (coordination with `notify.md`, drafted in parallel;
notify owns their meaning and ranges; its draft writes them with a dotted sub-key such as
`notify.smtp.host`, which the key grammar here does not allow, so the names below are the
underscore form it should adopt): `notify.smtp_host` (text), `notify.smtp_port` (int,
default `587`), `notify.smtp_security` (enum `starttls|tls|none`, default `starttls`),
`notify.smtp_username` (text), `notify.smtp_password` (secret), `notify.sender_address`
(text, the From address), `notify.sender_name` (text, default `` = `regional.name`),
`notify.reply_to` (text), `notify.alert_addresses` (text), `notify.retention` (duration).
**`regional.public_url` canonical form (owner, 2026-10-20, D40).** The submitted text is trimmed, then refused (`url`, with a `reason` param of `scheme`, `host`, `userinfo`, `query`, `fragment` or `chars`) unless: it parses with `net/url`; the scheme is `http` or `https` (any case); the host is non-empty (a port, if present, is 1..65535); there is no userinfo; the text contains no `?` or `#` (so no query, empty query or fragment); and it has no space or control character; it is longer than `MaxURLBytes` is `too_long`. It is then stored as lowercase scheme, lowercase host (and port), the path as given, **with every trailing `/` removed** (so `https://x.example/` becomes `https://x.example` and a regional behind a sub-path keeps it: `https://x.example/bms`). The same function is the canonical form `Regional().PublicURL` returns; notify builds links as `PublicURL + Link`, so a link can neither double a slash nor lose its host. A stored value that no longer passes (an upgrade) falls back to the default under decision 5. (Implementation, 2026-10-20: NUL and DEL are refused first by the generic text check with `control_chars` and no `reason`; tab, newline and space reach this rule as `chars`. A Unicode format character (category Cf, such as a zero-width space) or a bad percent-escape is `chars`; the canonical authority is taken from the submitted text and lowercased as a whole (percent-escape hex digits included), not from the unescaped host, so canonicalisation is a fixed point; any other parse failure, a port outside 1..65535 or a trailing colon is `host`. `Regional().PublicURL` canonicalises on read as well as on save, and a stored value the rule refuses reads as unset with the decision-5 warning.)

What notify's draft calls `site.name`, `site.public_url` and `site.default_language` are
`regional.name`, `regional.public_url` and `regional.language` here, read through
`Regional`; the terminology map is `TermsFor`. Notify's `SMTPSource`, `SiteSource` and
`RetentionSource` seams are adapters `app` writes over `Scoped("notify")` and `Regional`,
as for identity's and people's `PolicySource`.

**Terminology.** Terms are `CoreTerms` (`event`, `department`, `participant`, `theme_camp`),
each with four forms per language in `Languages`. Defaults are embedded in
`internal/core/settings/terms/<lang>.json` (`en` in M4; `es` when its catalogue lands);
a language without a file uses `en`'s. The shell's `term.*` catalogue entries must equal the
`en` defaults (test in `internal/app`, Proposed change 8). A form override is 1..`MaxTermBytes`
bytes, trimmed, no control characters. A module that needs a word of its own declares it
when its spec is written; the mechanism (a `Terms` field on the manifest) is a module-contract
change then, not now.

**Constants that are not settings**, pinned by `TestConstants_Pinned`:

| Constant | Value | Why not configurable |
|---|---|---|
| `RefreshInterval` | 30 s (`Options`, for tests) | a replica's staleness bound; nothing a regional should tune |
| `MaxBatch`, `MaxTextBytes`, `MaxTermBytes`, `MaxNameBytes`, `MaxURLBytes` | 200, 4096, 60, 80, 256 | bounds, not choices |
| `MinContrast` | 4.5 | WCAG 2.2 AA for text (principle 4) |
| `MaxLogoBytes` (logo, 2026-10-21) | 2 MiB | a header image is tens of kilobytes; a bound below files' per-file setting, not a choice a regional makes |
| sealing | AES-256-GCM, HKDF-SHA256 over the sealing key with info `bms settings seal v1` | one algorithm, in the standard library |

## UI

Core screen set `settings` (`web/src/core/settings/`, `kind: "core"`), nav entry
**Settings** in the admin area, `access: ["settings.read"]`. Every screen is phone first
(320 px, 44 px targets), uses `ErrorState`, `ErrorSummary`, `useFieldErrors`, typed keys
`core.settings.*`, no `style` prop.

| Screen | Who sees it | Actions | States |
|---|---|---|---|
| `/settings` (Regional) | `settings.read` | name, contact mailbox, public URL, language (select from `languages`), time zone (select from `Intl.supportedValuesOf("timeZone")`, first entry "browser's zone"), two colour fields with a live swatch and the computed contrast ratio; one **Save** (`PUT /values`); (logo, 2026-10-21) a **Logo** section under the form (its own `<section>`, its own requests, not part of Save: the preview, a `FileField` and **Upload** or **Replace**, **Remove logo** with a confirm `Dialog`; detail under "The logo field" below); links to Terminology and to each area | loading; error; forbidden (403 → `ErrorState` with the permission message; `manage` without `read` shows an explanatory `Banner` and no form); read-only copy (the shell's banner; Save answers `read_only` and the form keeps its edits; the logo controls are hidden, the preview stays) |
| `/settings/terms` | `settings.read` | a language select; per term a card with four fields, the default as placeholder, a **Reset** per field; one **Save** per language (`PUT /terms/{lang}`) | as above; empty never (terms always exist) |
| `/settings/:area` | `settings.read` | the area's settings as a form: a toggle (bool), a number, a duration (`DurationField`: number plus unit, sent as `h`/`m`/`s`), text, select (enum, labels from `.option.<opt>`), secret (`password` input showing "set" or "not set", a Replace and a Clear); a **Reset to default** per setting; one **Save** (`PUT /values` with only the changed keys) | loading; error; forbidden; an unknown area → not-found; a `stale` value shows a warning and the default |

Each field's title and description come from the declaring area's catalogue (decision 8
derivation); a missing key shows the raw setting key and is reported (web-shell decision 2).
After a save the shell refetches `GET /api/settings/public` and re-applies the terms and
the regional name, so the admin sees the change without a reload. Other browsers see it
on their next boot (the document is fetched at boot and not cached across sessions).

The shell changes this spec needs are in Proposed change 8 (terms merge, theme link, the
`field.contrast` key, `useRegionalName` and `configureLanguages` fed from the public document).

**The logo field** (logo, 2026-10-21; `web/src/core/settings/LogoControls.tsx`, used by
`screens/Regional.tsx` under the regional form; catalogue keys `core.settings.logo.*`). Who
sees what: `settings.read` sees the section; the upload and remove controls render only
with `settings.manage` and not on a read-only copy (`useGate`, the shell's read-only store);
`manage` without `read` gets no section (the screen shows no form, as today).

- **Preview.** When `Public.logo` is set (the shell's public document, `usePublic`): an
  `<img src="/api/settings/logo?v=<logo>" alt={t("core.settings.logo.current_alt")}>`
  ("The current logo"), height-bounded by Tailwind classes (`max-h-24 w-auto`), on a
  checkerboard-free plain background in both schemes (the header shows it on the frame's
  own background, so the preview sits on the same token). Image load failure (`onError`,
  a `503` on a copy without the bytes) replaces the image with the text
  `core.settings.logo.unavailable` ("The logo can't be shown right now.").
- **Empty.** No logo: `EmptyState` with `core.settings.logo.none` ("No logo yet. The site
  shows its name, {name}, as text.") where `{name}` is the regional's name.
- **Choose and upload.** `FileField` (`name="file"`, label `core.settings.logo.field`
  "Logo", hint `core.settings.logo.hint` "PNG, JPEG or WebP, up to 2 MB. It is shown in
  the header of every page.", `accept=["image/png","image/jpeg","image/webp"]`,
  `maxBytes=2097152`, no `capture`: a logo is a file, not a photo) and a **Upload** button
  (`core.settings.logo.upload`; `core.settings.logo.replace` "Replace" when a logo exists),
  disabled until a file is chosen. The upload is explicit, not on pick: a mis-pick must
  not replace the live logo for everyone. On press: `upload("/api/settings/logo", {file},
  {method: "PUT"})`; the button is `aria-busy` and disabled while in flight; the `Toast`
  `core.settings.logo.uploaded` ("Logo updated") on success; then the shell's public
  document is refetched (as after Save) so the header and the preview change without a
  reload. `FileField`'s client-side `maxBytes` check shows `field.too_large` at once
  without a request; the server enforces it anyway.
- **Remove.** **Remove logo** (`core.settings.logo.remove`), a `Dialog` asking
  `core.settings.logo.remove_confirm` ("Remove the logo? The site will show its name as
  text instead.") with **Remove** and **Cancel**; `DELETE /api/settings/logo`; `Toast`
  `core.settings.logo.removed` ("Logo removed"); the public document refetched.
- **Errors.** Field errors from `PUT` land on the `FileField` through `useFieldErrors`
  (`file`: `field.too_large {max}`, `field.type`, `field.malformed`, `field.required`;
  `quota` as `field.quota`); `503 read_only` shows `ReadOnlyNote` and keeps the chosen
  file; `501` (no files store) replaces the section's controls with the `Banner`
  `core.settings.logo.unsupported` ("This installation can't store a logo."); `503` with
  `Retry-After` (store unreachable) shows the generic `ErrorSummary` with the reference and
  a retry; `413` (a body past the route cap) is shown as `field.too_large` with
  `max = 2097152` (the client already knows the limit); `403` cannot happen for a person who
  sees the controls, and shows `ErrorSummary` if it does.
- **Accessibility.** The section has `aria-labelledby` its heading
  `core.settings.logo.heading` ("Logo"); the preview's `alt` is the fixed text above (the
  header's `alt` is the regional's name: web-shell); the file input has a visible label
  and a 44 px target; the dialog traps focus and returns it to **Remove logo**; the toast
  is `role="status"`; nothing depends on colour alone.

**The header** (web-shell's frame, D44; Proposed change 15 adds the detail): when
`Public.logo` is set, the home link holds `<img src="/api/settings/logo?v=<logo>"
alt="<regional name>" decoding="async" class="h-8 w-auto max-w-[12rem]">` (a 32 px line on
a phone, `max-w` so a wide logo cannot push the menu button off a 320 px screen); the link's
accessible name is therefore the regional's name, as it is with text. When unset, or when
the image fails to load (`onError`: a copy without the bytes answers `503`), the regional's
name as text, exactly as today. No `loading="lazy"` (above the fold). The `?v=` query
carries the file ID so a replaced logo is a new URL under `no-store` (decision 25 in files;
D45: no browser cache). One logo for both colour schemes (Q-LOGO2); a regional whose logo
needs a dark variant uploads one with a transparent background.

## Behavior

**Construction.** `New` validates `Options`, prepends the `regional` area, validates every
area, refuses duplicate area names and any area named `regional` from outside, and builds
the declaration index (`fullKey -> (Area, Decl)`). No I/O.

**Snapshot.** `snapshot{revision, values map[fullKey]string (text forms as stored),
sealed map[fullKey][]byte, terms map[lang]map[term]map[form]string, logo string (the logo's
file ID or ""; logo, 2026-10-21), loadedAt}` behind an
`atomic.Pointer`. The first read on a process loads it (single-flight, one `Transact(Read)`
with a derived deadline, span `settings.load`; when `Options.Files` is set the same
transaction runs `Files.ByRecord(LogoRef)` and keeps the newest file's ID, logo,
2026-10-21); until it exists every read fails with the
load's error. `Run` polls `SELECT revision FROM core.setting_revision` every
`RefreshInterval` and reloads when it moved (span `settings.refresh`, metric
`bms.settings.refresh{outcome=ok|unchanged|failed}`); a failed poll or reload logs once per
minute and keeps the old snapshot. After a local commit the writer builds the next
snapshot from the old one plus its changes with the revision the transaction returned,
and installs it only if newer than the current (two writers in one process cannot
install out of order); the next poll reconciles anyway.

**Reads.** A `Reader` call: resolve `<area>.<key>` in the index (`Invalid` if absent or of
another kind), take the snapshot (load if none), look the value up, parse it by kind. A
value that fails to parse (a kind or option changed in an upgrade) yields the default, a
warning once per key per process (`setting` attribute only), and counts
`bms.settings.stale_values` (decision 5: an upgrade must not break sign-in). `Secret`
unseals on each call so plaintext is not held; `secret.Secret{}` when unset.

**Writes.** `Set`: `Allow(settings.manage)`; `Validate`: 1..`MaxBatch` keys, each declared
(`unknown`), each non-nil value trimmed (not for `Secret`) and parsed by kind, the `Decl.Check`
run; all problems joined into one `Invalid` with the full key as field. There is **no early
area check** (owner, 2026-10-20, D40, Q-SETTINGS-REVIEW): the snapshot can be up to
`RefreshInterval` (30 s) stale on a copy that has not yet polled, so an area `Check` run
over snapshot values before the transaction could refuse a valid change. Every touched
area's `Check` runs instead inside the `Transact(Write)`, after the revision row lock, over
the values read from `core.setting` there (defaults, the stored values and the batch
merged) and fails `Invalid` with the full key as field, rolling back. A `Secret` change
needs a sealing key (`Unsupported` otherwise). The one `Transact(Write)`: upsert or delete per key, one `audit.Entry` per key
(`settings.setting.changed` or `.reset`, `Record: Ref{Kind: "settings.setting", ID: IDOf(key)}`,
`Fields: [key]`, `Reason: "key:<key>"`, never the value), the revision bump; after commit
the snapshot replace. A value equal to the default is stored as a reset. `SetTerms` is the
same over `core.terminology` (`settings.term.changed|reset`, `Ref{Kind: "settings.term",
ID: IDOf(lang + ":" + term)}`, `Fields: [form...]`, `Reason: "term:<lang>:<term>"`).

**Audit.** Records go through `Options.Audit` inside the write transaction (audit decision
2); a `Record` failure fails the use case (decision 3). Reads of the admin document are not
audited: nothing in it is personal. Secrets are never in an entry.

**Public document.** `Public` = `Regional` + whether
`ThemeCSS` is non-empty + `Terms` for every language in `Languages` (defaults with overrides,
flattened to the shell's argument names: `term_<term>`, `term_<term>_plural`,
`term_<term>_title`, `term_<term>_plural_title`). `GET /public` sets `ETag` from the
revision and answers `304` to a matching `If-None-Match`; the api shell's `no-store` on
`/api/` still applies (api decision 12), so the shell fetches it once per boot.
`GET /theme.css` answers `text/css; charset=utf-8` with

```css
@media not (prefers-color-scheme: dark) { :root { --primary: #...; --primary-fg: #...; --primary-hover: #...; } }
@media (prefers-color-scheme: dark) { :root { --primary: ...; --primary-fg: ...; --primary-hover: ...; } }
```

for the schemes whose colour is set; a scheme with no colour emits no rule, so the shipped
tokens stay for it, and an empty body when neither is set (implementation, 2026-10-19: the
light colour was first written as a bare `:root { ... }`, which in dark mode overrode the
shipped dark tokens and broke contrast, found in the web review, so it is now inside
`not (prefers-color-scheme: dark)`). `--primary-fg` is white
or `#1a1a1a`, whichever contrasts more with the colour, and `--primary-hover` is the colour
darkened (light scheme) or lightened (dark scheme) by 12 % in sRGB; both are derived, so
the regional sets one colour per scheme (decision 9).

**The logo** (logo, 2026-10-21; decisions 16 to 18). Three use cases over `Options.Files`,
every one `Unsupported` when it is nil.

*`SetLogo(ctx, name, body)`.* 1 `Allow(settings.manage)` (`Unauthenticated` with no actor,
`Permission` without it). 2 `s.db.ReadOnly()`: on a read-only copy, `Unavailable` wrapping
`db.ErrReadOnly` now, before a byte of the body is read (decision 18; files asks that a
handler on a read-only copy never reach `Stage`). 3 `Files.Stage(ctx, files.Upload{Name:
name, Accept: files.Brand, Body: limit(body)})` outside any transaction: `limit` is a
counting reader that, on the byte past `MaxLogoBytes`, returns `errs.E(op, errs.Invalid,
validate.FieldError{Field: "file", Code: "too_large", Params: {"max": "2097152"}})`, which
`Stage` returns unchanged (files' `classifyRead` passes a classified `*errs.Error` through;
Proposed change 16 writes that into files' contract); files sniffs the kind (`Detect` on the
first 512 bytes; anything outside `files.Brand`, an SVG, a GIF, a PDF, an HTML file named
`.png`, is `Invalid file type`), strips the image's metadata (`KeepMetadata` false: there is
nothing in a logo's EXIF the site needs, and the rule stays one rule), hashes and puts the
object in bucket `settings`. A `Stage` failure ends the use case with its error; nothing was
written to the database. 4 `defer Files.Discard(ctx, staged)`: a no-op once attached, else
the object goes (a `Discard` error is logged once with the file ID and left to files' sweep).
5 `Transact(Write)`: `LockRevision` first (the row lock serialises logo writes with each
other and with `Set`, so two admins uploading at once end with one row: the second
`Replace` runs after the first commit and deletes the first file); `Files.Attach(ctx,
staged, files.Attachment{Record: LogoRef, Level: model.SensitivityPublic, Replace: true})`
(no `Subject`: the logo is about nobody; `Replace` deletes the previous logo's row and
writes its tombstone in this transaction; files records `files.file.created` and
`files.file.deleted` itself); `Audit.Record(settings.logo.changed, Record: LogoRef, Fields:
["logo"], Reason: "file:<new id>")`; `BumpRevision`. A failure anywhere rolls back: no row,
no entry, no bump, and step 4 removes the object. 6 after commit: the next snapshot is the
old one with `logo` = the new ID and the bumped revision, installed if newer (as `Set`);
files' `AfterCommit` deletes the old object. Returns `LogoResult{Revision, Logo}`.
`SetLogo` always changes something (a new file, a new ID), like a `Secret` re-save (F22a).

*`RemoveLogo(ctx)`.* 1 `Allow(settings.manage)`. 2 `s.db.ReadOnly()` as above. 3
`Transact(Write)`: `LockRevision`; `Files.DeleteByRecord(ctx, LogoRef)`; when it returns 0
the transaction ends with nothing changed (no entry, no bump; the current revision is
returned with `Logo: ""`: idempotent, decision 18); otherwise `Audit.Record(settings.logo.removed,
Record: LogoRef, Fields: ["logo"], Reason: "file:<old id>")` and `BumpRevision`; after
commit the snapshot's `logo` is `""`.

*`OpenLogo(ctx)`.* No permission (public). Outside any transaction: `Files.ByRecord(ctx,
LogoRef)`; none is `NotFound`; the newest is opened with `Files.Open` (public: no actor
needed, no audit; files decisions 10 and 11); the handler calls `files.Serve(w, r, f, c)`
and closes `c`. `ByRecord`, not the snapshot, so a replica never serves a deleted ID during
its poll interval (decision 16). On a copy whose bytes have not been synced `Open` is
`Unavailable` wrapping `objects.ErrMissing` and the api answers `503` with `Retry-After`;
the header falls back to the name as text (web-shell).

*Handlers.* `PUT /logo`: `files.ReadUpload(r, "file")` (`415` when not multipart; `400
file required` with no file part; text parts are ignored), then `SetLogo(ctx, mp.Name,
mp.Body)`; `200` with `LogoResult`. `Route.MaxBodyBytes = 2*MaxLogoBytes` (the field error
from step 3 wins for anything up to twice the cap; a client that ignores it gets the shell's
`413` beyond; files' 1 GiB ceiling, which D44 named, has no use on a 2 MiB route) and
`Route.Timeout = files.UploadTimeout` (a phone on a venue link). `DELETE /logo`: `RemoveLogo`;
`200` with `LogoResult`. `GET /logo`: public; `OpenLogo`, `files.Serve` (sniffed
`Content-Type`, `inline`, `Content-Security-Policy: default-src 'none'`, `ETag` = SHA-256,
no `Cache-Control` of its own: the shell's `no-store` stands, files decision 25, D45; a
matching `If-None-Match` is a `304` through `http.ServeContent`); `Route.Timeout =
files.ServeTimeout`. The `?v=` query the shell adds is ignored by the handler. The OpenAPI
fragment documents the multipart request body, the binary `image/*` response and
`LogoResult` (api decision 33). Handlers never write an error body themselves
(`httpx.Fail`).

*Why the snapshot carries the ID (decision 16).* `GET /public`'s `ETag` is the revision;
a logo change that did not bump the revision would answer `304` to the shell's refetch after
an upload and the header would keep the old image. Bumping the revision makes the logo an
ordinary settings write: the writer's after-commit snapshot and a replica's poll both see
it, and `Public` stays a snapshot read with no I/O (F9 is its only failure). *Not taken:*
`ByRecord` inside `Public` (I/O on the boot document, and a logo change invisible to the
`ETag`); a declared `regional.logo` setting holding the ID (an admin could set it to any
string through `PUT /values`; there is no "internal-only" key).

*Export and deletion.* None: the logo is about nobody (no `Subject`, level `public`); the
spec's "Personal data" line under "Not responsible for" stands. files owns the one person
reference the row has (`uploaded_by`) and anonymises it in its own `DeletePerson`.

**Deviation: duration range errors (implementation, 2026-10-19).** A `range` error on a
`Duration` setting (identity and `auditsettings` checks) gives its `min` and `max` params in
the plainest whole unit (`1h`, `8760h`, `10m`), not Go's `Duration.String()` form
(`1h0m0s`), so the UI can show them as written.

**Sealing.** `seal(key, plaintext)`: HKDF-SHA256(sealing key, salt none, info
`bms settings seal v1`) gives the AES-256 key once at `New`; nonce 12 random bytes; AAD =
the full key. `unseal` reverses it; any failure is `Internal` with no detail beyond the key.
A deployment that never sets a `Secret` value needs no sealing key; `bms migrate`
generates one into `secrets.json` anyway (config Proposed change 9) so the common path
has it.

**Read-only copy.** Reads, the public routes and `Run` work (the standby streams the
writer's rows, and the revision poll sees them). Every write fails `Unavailable` wrapping
`db.ErrReadOnly` before any statement changes anything, and the api answers `503 read_only`.
(logo, 2026-10-21) `GET /logo` works when the operator's sync has brought the object
(files Q-FILES8), else `503` with `Retry-After` and the header shows the name; `PUT` and
`DELETE /logo` fail `read_only` before the body is read (decision 18), and the screen hides
their controls on a read-only copy.

**Concurrency and lifecycle.** `*Service` is safe for concurrent use. `Run` is the one
goroutine, started by `app` as a runnable after `Set.Start`, exiting when its ctx ends;
nothing to `Close`. A second `Run` fails `Conflict`. Idempotency: `Set` with the current
values is a no-op write (no rows change, no audit entry, no revision bump). Retries: none here; the shell retries `GET`s, never `PUT`s (web-shell
decision 11).

**Telemetry.** Spans `settings.<use-case>` with `settings.keys` (`Int`, the batch size) and
`settings.area` (`Token`); never a key's value. (logo, 2026-10-21) Spans `settings.logo.set`,
`settings.logo.remove`, `settings.logo.open` with `settings.file_id` (`ID`) once known; never
the file name or the declared type (files' rule, kept here: `files.File.LogValue` is the ID).
No new metrics: files counts uploads and reads. Logs carry `setting` (the key, a token) and
never a value. The leak test (`telemetrytest`) sets a marker as a text value and a secret
and asserts it appears in no log, span or audit entry.

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `New` | a nil `DB`, `Clock`, `Logger`, `Tracer`, `Audit`, `Authz` or `Actor` | Invalid (`settings.<field>`) | fix the wiring | `TestNew_NilDependency_ReturnsInvalid` |
| F2 | `New` | `RefreshInterval` outside 1 s..10 m | Invalid (`settings.refresh_interval`) | fix the wiring | `TestNew_RefreshIntervalRange_ReturnsInvalid` |
| F3 | `New` | an area invalid: name not an ident, a `Decl` failing `Validate`, duplicate keys | Invalid (`settings.areas[i].*`) | fix the declaring package | `TestNew_InvalidArea_ReturnsInvalid` |
| F4 | `New` | two areas with one name, or an outside area named `regional` | Invalid (`settings.areas`) | fix the wiring | `TestNew_DuplicateArea_ReturnsInvalid` |
| F5 | `Decl.Validate` | a `Secret` with a non-empty `Default` or `Options` | Invalid (`default`) | fix the declaration | `TestDeclValidate_SecretWithDefault_ReturnsInvalid` |
| F6 | `Scoped` | an undeclared area | Invalid | fix the wiring | `TestScoped_UnknownArea_ReturnsInvalid` |
| F7 | `Reader.*` | a key the area did not declare | Invalid | fix the caller | `TestReader_UndeclaredKey_ReturnsInvalid` |
| F8 | `Reader.*` | the method's kind does not match the declaration (`Int` on a text) | Invalid | fix the caller | `TestReader_KindMismatch_ReturnsInvalid` |
| F9 | `Reader.*`, `Regional`, `TermsFor`, `Public` | no snapshot yet and the load fails (database down) | Unavailable (or Timeout, Canceled) | the use case fails; the api answers 503; the shell keeps its defaults and retries | `TestReader_FirstLoadFails_ReturnsUnavailable` |
| F10 | `Reader.*` | the stored value no longer parses as its kind | none: the default is returned, warned once, counted | nothing | `TestReader_StaleValue_ReturnsDefaultAndWarnsOnce` |
| F11 | `Reader.Secret` | a value is set and the service has no sealing key | Unsupported | tell the operator to set `settings.sealing_key` | `TestReaderSecret_NoSealingKey_ReturnsUnsupported` |
| F12 | `Reader.Secret` | unsealing fails (key changed, row tampered) | Internal | the use case fails; the operator restores the key or the admin re-enters the value | `TestReaderSecret_WrongKey_ReturnsInternal` |
| F13 | `TermsFor` | `lang` not in `Languages` | Invalid (`lang`, `one_of`) | show the field error | `TestTermsFor_UnknownLanguage_ReturnsInvalid` |
| F14 | `Document`, `Terms` | no actor | Unauthenticated | sign in | `TestDocument_NoActor_ReturnsUnauthenticated` |
| F15 | `Document`, `Terms` | no `settings.read` | Permission | show forbidden | `TestDocument_NoPermission_ReturnsPermission` |
| F16 | `Set`, `SetTerms` | no `settings.manage` | Permission | show forbidden | `TestSet_NoPermission_ReturnsPermission` |
| F17 | `Set` | empty batch or more than `MaxBatch` keys | Invalid (`values`, `required` / `too_many {max}`) | fix the request | `TestSet_BatchSize_ReturnsInvalid` |
| F18 | `Set` | a key not declared in this process | Invalid (`<key>`, `unknown`) | show the field error | `TestSet_UnknownKey_ReturnsInvalid` |
| F19 | `Set` | a value that does not parse as its kind: bool, int, duration, text too long, control characters, enum not in options | Invalid (`<key>`, `malformed` / `too_long {max}` / `control_chars` / `one_of`) | show the field errors (all collected) | `TestSet_ValueShape_Table` |
| F20 | `Set` | `Decl.Check` rejects the value | Invalid (`<key>`, the check's codes or `rejected` with its reason) | show the field error | `TestSet_CheckRejects_ReturnsInvalid` |
| F21 | `Set` | an area's `Check` rejects the merged values (identity: `client_failure_limit` below `login_failure_limit`) | Invalid (`<area>.<field>`, the check's codes) | show the field error | `TestSet_AreaCheckRejects_ReturnsInvalid` |
| F22a | `Set` | a `Secret` re-saved with the value it already has | counts as a change: written (a fresh nonce makes new ciphertext), revision bumped, `settings.setting.changed` audited; a `Check` and an area `Check` see no "already set" | nothing | `TestSet_SecretResave_AlwaysChanges` |
| F22 | `Set` | a `Secret` value with no sealing key | Unsupported | tell the operator | `TestSet_SecretWithoutSealingKey_ReturnsUnsupported` |
| F23 | `Set` (`regional.*`) | name empty or too long; contact e-mail malformed; URL not http/https, without a host, with userinfo, query or fragment, or too long (the rule above); zone unknown or `Local`; colour not `#rrggbb`; colour under `MinContrast` | Invalid (`required`, `too_long`, `malformed`, `url {reason}`, `unknown`, `contrast {ratio, min}`) | show the field errors | `TestSet_Regional_Table` |
| F23a | `Set` | the snapshot is stale (a copy that has not polled) and the batch is valid against the stored values | accepted: the area `Check` runs inside the transaction over the stored rows, so a stale snapshot cannot refuse it | nothing | `TestSet_StaleSnapshot_AreaCheckUsesStoredRows` |
| F24 | `Set`, `SetTerms` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | the api answers `503 read_only`; the screen keeps its edits | `TestSet_ReadOnly_ReturnsUnavailable` |
| F25 | `Set`, `SetTerms` | the audit `Record` fails | the recorder's error; nothing written | the use case fails | `TestSet_AuditFails_RollsBack` |
| F26 | `Set`, `SetTerms` | `core.setting_revision` has no row | Internal | run `bms migrate`; the operator sees the reference | `TestSet_NoRevisionRow_ReturnsInternal` |
| F27 | `Set`, `SetTerms` | the store fails otherwise | Unavailable, Timeout, Canceled (`db.Wrap`) | the use case fails | `TestSet_StoreFails_ReturnsKind` |
| F28 | `SetTerms` | `lang` not in `Languages` | Invalid (`lang`, `one_of`) | show the field error | `TestSetTerms_UnknownLanguage_ReturnsInvalid` |
| F29 | `SetTerms` | a term not in `CoreTerms`, or a reset naming an unknown form | Invalid (`terms.<term>` / `terms.<term>.reset`, `unknown`) | show the field error | `TestSetTerms_UnknownTerm_ReturnsInvalid` |
| F30 | `SetTerms` | a form empty after trimming, too long, or with control characters | Invalid (`terms.<term>.<form>`, `required` / `too_long {max}` / `control_chars`) | show the field errors | `TestSetTerms_FormShape_Table` |
| F31 | `SetTerms` | no term in the batch, or more than `len(CoreTerms)` | Invalid (`terms`, `required` / `too_many`) | fix the request | `TestSetTerms_Empty_ReturnsInvalid` |
| F32 | `Run` | called while running | Conflict | fix the wiring | `TestRun_Twice_ReturnsConflict` |
| F33 | `Run` | a poll or reload fails | none returned: logged once per minute, counted `failed`, old snapshot kept | nothing | `TestRun_PollFails_KeepsSnapshotAndWarnsOnce` |
| F34 | `Run` | ctx ends | nil | nothing | `TestRun_ContextDone_ReturnsNil` |
| F35 | handlers | malformed JSON, wrong content type | Invalid (`httpx.ReadJSON`'s) | show the error | `TestPutValues_BadBody_Returns400` |
| F36 | `GET /public`, `/theme.css` | `If-None-Match` equals the current `ETag` | none: `304` | nothing | `TestGetPublic_IfNoneMatch_Returns304` |
| F37 | `PUT /logo` | the body is not `multipart/form-data`, or has no file part | Invalid wrapping `httpx.ErrUnsupportedMediaType` (`415`) / Invalid (`file`, `required`) | fix the client / "choose a file" | `TestPutLogo_NotMultipart_Returns415`, `TestPutLogo_NoFilePart_ReturnsInvalidRequired` (logo, 2026-10-21) |
| F38 | `SetLogo`, `RemoveLogo` | no actor | Unauthenticated | sign in | `TestSetLogo_NoActor_ReturnsUnauthenticated` |
| F39 | `SetLogo`, `RemoveLogo` | no `settings.manage` | Permission | show forbidden | `TestSetLogo_NoPermission_ReturnsPermission`, `TestRemoveLogo_NoPermission_ReturnsPermission` (D44's F38) |
| F40 | `SetLogo`, `RemoveLogo`, `OpenLogo` | `Options.Files` is nil | Unsupported (`501`) | the screen says logos aren't available here | `TestLogo_NoFiles_ReturnsUnsupported` (D44's F40) |
| F41 | `SetLogo`, `RemoveLogo` | read-only copy | Unavailable wrapping `db.ErrReadOnly`, before any of the body is read (`SetLogo`) and before `Stage` | `503 read_only`; the screen keeps the chosen file | `TestSetLogo_ReadOnly_ReturnsUnavailableBeforeBodyRead` (the body reader records zero reads), `TestRemoveLogo_ReadOnly_ReturnsUnavailable` |
| F42 | `SetLogo` | more than `MaxLogoBytes` bytes in the file part | Invalid (`file`, `too_large {max: 2097152}`); the counting reader stops at the cap, nothing staged | show the limit | `TestSetLogo_OverMaxLogoBytes_ReturnsInvalidTooLarge` (D44's F37; the fake's `Staged()` is empty) |
| F43 | `SetLogo` | the sniffed kind is not in `files.Brand` (SVG, GIF, PDF, HTML named `.png`) | Invalid (`file`, `type`) | "this kind of file isn't accepted" | `TestSetLogo_UnacceptedType_ReturnsInvalidType` (table: an SVG, a GIF, a PDF, an HTML file) |
| F44 | `SetLogo` | the image cannot be parsed for stripping | Invalid (`file`, `malformed`) | "the file is damaged" | `TestSetLogo_Malformed_ReturnsInvalid` |
| F45 | `SetLogo` | the site is at `files.quota_bytes` (files decision 26, at `Stage` or `Attach`) | Conflict (`quota {max}`) | raise the quota or remove files | `TestSetLogo_AtQuota_ReturnsConflictQuota` (nothing attached, no bump, no entry) |
| F46 | `SetLogo` | the object store or the temp dir is unavailable | Unavailable (`503`, `Retry-After`); nothing written | retry later; operator | `TestSetLogo_StoreUnavailable_ReturnsUnavailable` |
| F47 | `SetLogo`, `RemoveLogo` | the audit `Record` fails inside the transaction | the recorder's kind; rollback; the staged object discarded | the use case fails | `TestSetLogo_AuditFails_RollsBackAndDiscards` (`Files()` unchanged, `Staged()` empty after), `TestRemoveLogo_AuditFails_RollsBack` |
| F48 | `SetLogo` | `Attach` fails for a files reason after `Stage` (`Invalid staged expired`, store kinds) | that error; rollback; discarded | the use case fails | `TestSetLogo_AttachFails_RollsBackAndDiscards` |
| F49 | `SetLogo` | two admins upload at once | none: serialised by the revision lock; one row remains (the later upload's), one `settings.logo.changed` each | nothing | `TestSetLogo_Concurrent_OneRowRemains` (integration) |
| F50 | `RemoveLogo` | no logo | none: no entry, no bump, `LogoResult{current revision, ""}` | nothing | `TestRemoveLogo_None_IsNoop` |
| F51 | `OpenLogo` | no logo | NotFound (`404 not_found`) | the header shows the name | `TestGetLogo_None_ReturnsNotFound` (D44's F39) |
| F52 | `OpenLogo` | the row exists and the object does not (a copy not yet synced) | Unavailable wrapping `objects.ErrMissing` (`503`, `Retry-After`) | the header falls back to the name; the operator syncs | `TestGetLogo_ObjectMissing_ReturnsUnavailable` |
| F53 | `GET /logo` | `If-None-Match` equals the file's `ETag` | none: `304` (`http.ServeContent`) | nothing | `TestGetLogo_IfNoneMatch_Returns304`; `TestGetLogo_SetsNoCacheControl` pins decision 25 |
| F54 | load | `Files.ByRecord(LogoRef)` fails during the snapshot load | the load's error (Unavailable, Timeout, Canceled), as F9 | as F9 | `TestLoad_LogoLookupFails_ReturnsUnavailable` |

`Public`, `ThemeCSS`, `Regional` and `Routes`/`OpenAPI` cannot fail beyond F9 and `Internal`
on an unencodable document (a bug): they read the snapshot and format it. `LogoRef` and
`MaxLogoBytes` are constants pinned by `TestLogoRef_Pinned` and `TestConstants_Pinned`
(logo, 2026-10-21).

## Lock-in check

No external technology beyond PostgreSQL (the project's database) and the Go standard
library (`crypto/aes`, `crypto/cipher`, `crypto/hkdf`, `crypto/sha256`). The storage seam is `postgres/` behind the service's own
store interface (D8), so a fork can swap it. Nothing here calls out of the process.

## Test plan

- **Unit (no infrastructure):** `Decl`/`Area` validation tables; value parsing per kind;
  the sealing (crypto) tests (round trip, wrong key, moved ciphertext) and the contrast tests
  are unit tests with no build tag and no database (owner, 2026-10-20, D40); the
  `regional.*` checks (URL canonical-form table: scheme, host, userinfo, query, fragment,
  trailing slashes, case, sub-path; zone, colour, contrast with known pairs: `#9a3412` on white
  passes, `#f97316` fails); the snapshot (load once, after-commit replace, out-of-order
  install refused, stale value → default and one warning); `Reader` scoping and kind checks
  over a store fake; sealing round trip, wrong key, moved ciphertext (AAD); `ThemeCSS`
  rendering and the derived colours; the public flattening of terms; `Run` with a fake
  clock (no `time.Sleep`): unchanged, moved, failing polls; the handlers over
  `httptest` with `dbtest.Fake` for the kinds a fake can produce (read-only, store failure).
- **Permission tests:** `authztest.NewFake()`: every use case denied without its permission,
  allowed with it, allowed for the superuser; `manage` without `read` can `Set` and not
  `Document`.
- **Integration (`//go:build integration`, `dbtest.TempDB`):** migration applies on an empty
  database; `Set` writes, resets, bumps the revision, audits one entry per key (an
  `audittest.Fake`'s `Recorded()`), is a no-op for unchanged values; undeclared rows survive
  and stay invisible; `SetTerms` and `TermsFor`; a second
  `Service` on the same database sees a change after one poll; writes on a read-only
  database (`dbtest.Fake.SetReadOnly` at unit level, a standby at integration level if the
  harness has one) fail `ErrReadOnly`.
- **Leak test:** `telemetrytest.New(t)`: a marker set as a text value and as a secret
  appears in no log, span, metric or audit entry.
- **Logo (logo, 2026-10-21):** unit tests over `filestest.NewFake("settings")`,
  `authztest`, `audittest` and `dbtest.Fake`: F37 to F48, F50 to F54 as named; the snapshot
  carries the ID after `SetLogo` and `""` after `RemoveLogo`; `GET /public`'s `ETag` moves
  on both; `Public.Logo` is `""` with no `Files`; the counting reader stops reading at
  `MaxLogoBytes + 1` (a reader that counts its reads); the handler over `httptest` with a
  real multipart body (a 1x1 PNG, a JPEG, a WebP header) and `TestGetLogo_SetsNoCacheControl`
  (the header absent); the leak test adds a marker as the uploaded file's name and asserts
  it reaches no log, span or audit entry. Integration (`dbtest.TempDB`, the `disk` store in a
  temp dir): upload, replace (the first object gone after `Reap`), remove (tombstone, then
  gone), a second `Service` sees the new ID after one poll, F49 with two goroutines.
  Catalogue test (`internal/app`): every `core.settings.logo.*` key named under UI exists in
  `en.json`. Playwright (phone and desktop): the smoke run's logo round trip (web-shell
  Proposed change 8): a settings admin uploads a PNG, the header shows it with the regional's
  name as `alt`, replaces it (the `src` changes), removes it (the name is text again);
  `axe` passes on `/settings` with and without a logo.
- **Catalogue tests (`internal/app`):** `core.regional.setting.<key>.title|description` for
  every `regional` key and `.option.<opt>` for `language`; the shell's `term.*` entries equal
  `terms/en.json`; every core area's declared keys have their catalogue titles.
- **End-to-end (Playwright, phone and desktop):** a settings admin renames "department" to
  "team" and the nav and the departments page title change without a reload; a
  participant cannot open `/settings`; a bad colour shows the contrast error.

## Amendment 2026-10-19 (approved, owner, 2026-10-20, D40): authz before authz, caching, the public origin

Three gaps found while applying D39 (app.md Proposed change 18, api.md Proposed change 15). Each is settled with the smallest design; the failure modes live in the spec whose code carries them (app, api), named here.

**1. `Options.Authz` at part 3.** settings is core part 3, authz part 4, and `Options.Authz` is required (F1). Decision: `app` passes a lazy adapter, `lazyAuthorizer{a}` (app.md amendment of the same date), as it already does for identity's and audit's `PermissionChecker`; the part order does not change. Reason: settings consults `Authz` only inside a use case (`Allow` for `settings.read` and `settings.manage`; Behavior, "Pre- and post-conditions"), which runs on a request after every part is built; at build time the value exists only to pass F1. A reorder would renumber parts 3 to 7 in four specs and the pinned order test, and would turn authz's later `ZoneSource` (Proposed change 10) into a lazy adapter in turn. Settings calls nothing but `Allow` (every settings permission is site-scoped); the adapter implements `AllowIn` and `ExtentOf` too so it is a complete `Authorizer`. Nothing in this package changes; `settingstest` and the unit tests are unaffected. Failure mode: app F31 to F33 (`TestLazyAuthorizer_BeforeAuthz_DeniesAndEmptyExtent`).

**2. Caching of `GET /public` and `GET /theme.css`.** Decision: the `/api/` `no-store` rule (api decision 12) stays as it is; neither route is exempted and the handlers do not set `Cache-Control`. Both set `ETag` (from the revision) and answer `304` to a matching `If-None-Match`, as Behavior already says: the web shell keeps the tag in memory and sends it on its refetch after a save (web-shell: the stored `ETag`), and `theme.css` is fetched once per page load (a few hundred bytes). That is the whole M4 cost. Reason: an exemption needs either api knowing settings' paths or a route-level "cacheable" declaration in module-contract, and a handler override would make "no cache ever stores anything personal" depend on every handler author; neither is worth a few hundred bytes per boot. F36 stands; api F58 (`TestPipeline_MountedRouteWithETag_KeepsNoStore`) pins the combination, and app's e2e repeats it on the two routes.

**3. `regional.public_url` as api's public origin.** Decision: `api.Options.PublicOrigin` waits (not M4). Its shape is fixed now so Proposed change 9 closes: `PublicOrigin func(ctx context.Context) (string, error)`, optional, which `app` fills over `Regional(ctx).PublicURL` (a snapshot read per request, so a change needs no restart), returning the URL or `""` when unset; `""` or a nil seam means today's behaviour (the front end preserves `Host`, api Proposed change 14). There is no configuration fallback: `config` owns no public URL (D18, decision 6), so `regional.public_url` is the only source. Why wait: no M4 flow needs a second trusted origin or the `Host` allowlist; the one M4 consumer of `public_url` is notify's link prefix, which reads `Regional` directly. Failure mode: api F59, deferred with the seam.

## Amendment 2026-10-20 (approved, owner, D44): the logo

Applies files.md Proposed change 6; decision 8 is superseded.

- `Options.Files files.Store` (scoped `settings`; lazy from `app`; optional: nil means no logo support and the routes answer `Unsupported`). `logoRef = audit.Ref{Kind: files.LogoRecordKind, ID: sha256("settings.logo")[:16]}`, the fixed-ID pattern of departments-events decision 25.
- Routes: `GET /api/settings/logo` (`AccessPublic`; `ByRecord(logoRef)`, `Open`, `files.Serve`; `404 not_found` when none); `PUT /api/settings/logo` (`settings.manage`; `ReadUpload(r, "file")`, `Stage{Accept: files.Brand}` then, in a `Write` transaction, `Attach{Record: logoRef, Level: model.Public, Replace: true}`, with a deferred `Discard`; `Route.MaxBodyBytes = files.UploadCeiling`, `Route.Timeout = files.UploadTimeout`; a logo over `MaxLogoBytes = 2 MiB` is `Invalid file too_large` before the policy cap); `DELETE /api/settings/logo` (`settings.manage`; `DeleteByRecord`).
- `Public` gains `Logo string` (the file ID, `""` when none) so the shell can version the image URL.
- Audit actions `settings.logo.changed` and `settings.logo.removed`, recorded in the same `Write` transaction as the attach or delete.
- UI: the `/settings` regional area gains a logo field (`ui/FileField`, `accept` PNG, JPEG, WebP, preview, remove).
- Failure modes added: F37 `PUT` logo over `MaxLogoBytes` or of a type outside `files.Brand`: `Invalid` (`file`, `too_large` or `type`), `TestPutLogo_TooLarge_ReturnsInvalid`; F38 `PUT` or `DELETE` without `settings.manage`: `Permission`, `TestPutLogo_NoPermission_ReturnsPermission`; F39 `GET` with no logo: `NotFound`, `TestGetLogo_None_ReturnsNotFound`; F40 `Options.Files` nil: `Unsupported`, `TestLogo_NoFiles_ReturnsUnsupported`.

(The amendment of 2026-10-21 below renumbers these as F42, F43, F39, F51 and F40 and gives them their detail; nothing D44 approved is withdrawn except the `Route.MaxBodyBytes` value, which becomes `2*MaxLogoBytes`.)

## Amendment 2026-10-21 (awaiting owner approval): the logo over files

Raised by the architect after `files` (2026-10-20) and `privacy-tags` (2026-10-21) were built. The D44 amendment above named the routes and the record; it left open how the logo's ID reaches the public document, how the 2 MiB cap meets files' site-wide policy, what the write does on a read-only copy, what the screen and the header show in every state, and which spec owns the upload components. Each point is settled below with the smallest design; the inline edits are marked "(logo, 2026-10-21)" and this section is the record. No existing interface changes; `Options`, `Public`, `Route` gain fields, the service gains three methods and two constants.

**L1. The logo's ID lives in the snapshot, and a logo change is a settings write (decision 16).** *Problem:* `GET /public` answers `304` to the shell's `If-None-Match` whenever the revision has not moved; a logo attached outside the revision would leave the shell's refetch after an upload with the old `Public.Logo`, and a replica would never learn of it. *Chosen:* `SetLogo` and `RemoveLogo` run under `LockRevision`, call `BumpRevision`, and install the next snapshot with the new `logo`; the load reads `Files.ByRecord(LogoRef)` in its `Transact(Read)`; `Public.Logo` is a map lookup. `GET /logo` reads `ByRecord` directly, so a replica in its poll interval never serves a deleted ID. *Why:* one mechanism (the revision) for every public change; `Public` keeps "no I/O, fails only as F9". *Not taken:* `ByRecord` in `Public` (I/O on the boot document and an `ETag` that lies); a declared `regional.logo` key (writable through `PUT /values` with any string).

**L2. `MaxLogoBytes` before files' policy (decision 17).** *Problem:* `Stage` enforces `files.max_bytes` (25 MiB by default, a regional's choice); the logo's 2 MiB is a bound of this spec, and `Upload` has no per-call limit. *Chosen:* the handler wraps the file part in a counting reader that fails with `Invalid file too_large {max: 2097152}` on the byte past the cap; `Stage` returns a classified error from `Body` unchanged (its `classifyRead` already does; Proposed change 16 makes it a contract line). `Route.MaxBodyBytes = 2*MaxLogoBytes` so the field error, not the shell's `413`, is what a person sees for anything near the limit; D44's `files.UploadCeiling` (1 GiB) had no use on this route. *Not taken:* `Upload.MaxBytes` on files' interface (an additive change to an Implemented spec for one consumer; the reader does the same with no change there); `MaxBodyBytes = MaxLogoBytes` (the multipart framing would push a file just under the cap into a `413` with no field).

**L3. Accepted kinds: `files.Brand`, no SVG (decision 17; Q-LOGO1).** PNG, JPEG and WebP by their bytes (files decision 7: the declared type is never trusted; `Detect` on the first 512 bytes), metadata stripped (files decision 9; nothing a logo needs). SVG is refused by files' catalogue (Q-FILES3, D44) and this amendment recommends keeping it so: an SVG is an XML document that may carry `<script>`, event-handler attributes, `<foreignObject>` with HTML, and external `href`s; served inline from the app's origin, a crafted one opened as a top-level document would run in that origin unless every layer holds (`Content-Security-Policy: default-src 'none'` on the response, `<img>` never executing script). Those layers exist, but the sanitiser that would make SVG safe by construction (parse, allow-list elements and attributes, drop scripts, handlers, foreign objects and external references) does not, and the uploader, though an admin, may have been handed the file by someone else. When a regional asks, files adds a sanitised `SVG` kind for `public` files and `Brand` gains it; nothing here changes. No pixel-dimension check (Q-LOGO4): the uploader holds `settings.manage`; a decompression bomb would hurt only that site's own browsers, and files decodes no pixels by design.

**L4. Read-only copies and idempotency (decision 18).** `SetLogo` checks `db.ReadOnly()` after the permission and before reading the body, so a 2 MiB upload is not read and no object staged where `Attach` must fail (files: "a handler on a read-only copy should not reach `Stage` at all"); `RemoveLogo` the same. `RemoveLogo` with no logo is a no-op `200` (no entry, no bump), as `Set` with unchanged values. `GET /logo` works on a copy that has the bytes; `ErrMissing` is `503` with `Retry-After` and the header shows the name.

**L5. Audit and concurrency.** `settings.logo.changed` and `settings.logo.removed` with `Record: LogoRef`, `Fields: ["logo"]`, `Reason: "file:<id>"`, inside the write transaction (audit decision 2); files' own `files.file.created|deleted` entries sit beside them. The revision row lock serialises two concurrent uploads, so `Replace` cannot leave two rows (F49). Reads of the logo are not audited (public, files decision 10).

**L6. Export and deletion: none.** The logo is about nobody: no `Subject`, level `public`. `uploaded_by` is files' column and files' `DeletePerson` anonymises it; an admin's export lists the logo under files' `uploaded` with its name (files decision 27). Settings stays without `PersonData`.

**L7. The screen and the header.** The logo field is specified under UI, "The logo field": an explicit **Upload** (not upload-on-pick: a mis-pick must not replace the live logo for everyone), **Remove logo** behind a confirm dialog, the preview with a text fallback on load failure, the empty state naming the regional, every error state mapped to a field code or a note, and the accessibility rules. The header is web-shell's (D44, "M4 files additions"): `<img>` with the regional's name as `alt` inside the home link, height-bounded and width-capped for a 320 px screen, falling back to the name when unset or on load error; Proposed change 15 gives web-shell that detail. One logo for both colour schemes (Q-LOGO2).

**L8. Who owns the upload components.** `upload()` in `@/api`, `ui/FileField` and `ui/Attachments` are web-shell's (web-shell.md, "M4 files additions (D44)" and its Proposed change 8), not yet built; the logo field is `FileField`'s first consumer and needs no `Attachments`. Their one-sentence description is not enough to implement against, so Proposed change 15 drafts their contract (props, states, the client-side `maxBytes` check, the `fields`-first multipart order, the upload timeout) for web-shell.md; the frontend implementer builds them with the logo field.

**L9. Wiring.** `Options.Files` is a lazy adapter from `app` (files is part 10, settings part 3), as `lazyAuthorizer` is; `settingsRoutes` copies `MaxBodyBytes` and `Timeout` to `module.Route` (Proposed change 14). The OpenAPI fragment gains the three routes and `LogoResult`; `SettingsPublic` gains `logo` (required, string).

## Open questions

Answered 2026-10-19 (D39), record below. Raised 2026-10-21 by the logo amendment, with
recommendations:

- **Q-LOGO1. Accept SVG logos now?** Many regionals' logos are vectors. *Recommendation:*
  no (L3): keep `files.Brand` as PNG, JPEG, WebP; when a regional asks, `files` adds a
  sanitised `SVG` kind for `public` files and `Brand` gains it, with no change here. A
  regional exports its vector logo to PNG or WebP at header size today.
- **Q-LOGO2. A second logo for the dark colour scheme?** The frame switches with
  `prefers-color-scheme`; a dark logo on a dark header may vanish. *Recommendation:* not
  now: one logo, and the hint tells the admin to upload one with a transparent background
  that reads on both; a `logo_dark` variant (a second fixed record, `Public.LogoDark`, a
  `<picture>` with a `media` source) is an additive change when a regional reports the
  problem.
- **Q-LOGO3. Derive the favicon from the logo?** *Recommendation:* no; the favicon stays the
  shipped one ("Not responsible for": a favicon). Resizing is files' Later, and a favicon
  needs several sizes and an ICO or SVG; a regional that wants one forks `index.html`.
- **Q-LOGO4. Cap the logo's pixel dimensions?** A 2 MiB PNG can decode to hundreds of
  megabytes. *Recommendation:* no (L3): the uploader is a settings admin harming only
  their own site; if wanted later, files gains `Upload.MaxPixels` checked from the image
  header (stdlib for PNG and JPEG, a header read for WebP) and this spec passes 4096.

Record of 2026-10-19 (D39):

1. The SMTP password lives in settings, sealed with the generated sealing key (decision 3);
   a settings admin sets up e-mail and sends a test from the screen.
2. Four term forms: singular, plural and a title-case form of each (decision 7).
3. The logo is deferred to the `files` spec and is out of M4 completely (decision 8, "Not
   responsible for").
4. One colour per scheme with a 4.5:1 contrast check (decision 9).
5. The module that runs the retention job declares its retention keys (decision 11).
6. `settings.read` goes to `settings-admin` only (decision 14, "Permissions").

## Proposed changes elsewhere

Status as of 2026-10-20 (D40): all 13 are applied; item 9 is recorded in api.md and settled by the approved amendment of 2026-10-19.

1. Applied 2026-10-19 (D39), in module-contract.md. **`plan/design/module-contract.md` (row 4):** `SettingKind` and `SettingDecl` move to
   `settings` (`settings.Kind`, `settings.Decl`), with aliases `module.SettingKind`,
   `module.SettingDecl` and the constants `module.SettingBool` ... `module.SettingEnum` plus
   the new `module.SettingSecret`, so manifests do not change (as `authz.Permission` and
   `signals.Name`, decision 3). The key grammar becomes `[a-z][a-z0-9]*(_[a-z0-9]+)*`
   (underscores, at most 64 bytes) instead of module's hyphenated ident, because every key
   the approved specs declare (`legal_name`, `session_lifetime`, `self_view`) and every
   `{term_*}` argument (ICU allows no hyphen) already use underscores; hello's `greeting`
   is unaffected. `Plan.Settings()` keeps `[]ModuleSettings`; `app` converts
   to `[]settings.Area{Core: false}`. `Deps.Settings settings.Reader`, scoped per module by
   `provision`; `moduletest.Conformance` supplies `settingstest.NewFake(manifest.Name,
   manifest.Settings)` when nil. `regional` joins the reserved names. Mark Proposed change 5
   applied with the deviation: no automatic retention keys (Open question 5).
2. Applied 2026-10-19 (D39), in config.md. **`plan/design/config.md` (row 3):** catalogue key `settings.sealing_key`
   (`BMS_SETTINGS_SEALING_KEY`, `_FILE`, secrets-file field `settings_sealing_key`,
   `Generated`, when set 32..1024 bytes, no NUL); `Config.Settings{SealingKey secret.Secret}`;
   `bms migrate` generates it like the session key (decision 7); unset is not an error for
   any command. Mark Proposed change 9 applied.
3. Applied 2026-10-19 (D39), in app.md, the part 3 row and Proposed change 18. **`plan/design/app.md` (row 18):** part 3 `settings` as in its table: `declare` adds
   `settings.Set` to `coreSets` and `settings.Permissions` to `corePerms`; `build` calls
   `settings.New` over `identity.Settings`, `people.Settings`, `auditsettings.Settings` (core areas)
   and `a.plan.Settings()` (module areas), `audit.Scoped("settings")`, the authz actor
   closure, `a.cfg.Settings.SealingKey`; sets `a.deps.settings`, the `PolicySource` adapters
   for identity (`settingsPolicy{r}` over `Scoped("accounts")`) and people (over
   `Scoped("people")`), route set `settings` (public routes converted with
   `Access: AccessPublic`); runnable `Run`. `provision` sets `Deps.Settings =
   a.deps.settings.Scoped(m.Name)`.
4. Applied 2026-10-19 (D39), in identity.md. **`plan/design/identity.md` (row 6):** export `Settings []settings.Decl` (the eight
   `accounts.*` keys, the seven of identity.md plus `accounts.password_reset_lifetime` (notify), with their ranges as `Check`s) and `CheckSettings(map[string]string)
   error` (the cross-field rule); `PolicySource` is implemented by `app` over a scoped
   `Reader`. Mark Proposed change 8 applied.
5. Applied 2026-10-19 (D39), in people.md. **`plan/design/people.md` (row 8):** export `Settings []settings.Decl` (the three enum
   keys; `emergency_contacts` without `required` in its options so `Policy.Validate`'s rule
   is the declaration). Mark Proposed change 15 applied.
6. Applied 2026-10-19 (D39), in audit.md. **`plan/design/audit.md` (row 12):** export `Settings []settings.Decl`; `audit.retention`'s
   range is written in hours (`8760h`..`876000h`, `0` = forever) because `Duration` is
   `time.ParseDuration`; `jobs` reads it through `Scoped("audit").Duration("retention")`.
   Mark Proposed change 6 applied.
7. Applied 2026-10-19 (D39), in departments-events.md. **`plan/design/departments-events.md` (row 9):** mark Proposed change 11 applied: the
   new-event form reads `timezone` from `GET /api/settings/public` and falls back to the
   browser's zone when empty; the terms arrive as `term_department`, `term_event` and the
   `_plural`, `_title`, `_plural_title` forms.
8. Applied 2026-10-19 (D39), in web-shell.md, Proposed change 7 (without the logo). **`plan/design/web-shell.md` (row 17):** mark Proposed change 7 applied with these
   details: the frame fetches `GET /api/settings/public` at boot (before the catalogues
   settle; on failure it keeps `DEFAULT_REGIONAL_NAME`, `DEFAULT_LANGUAGE` and the
   catalogue's terms, reports once and retries with the `GET` policy); `useRegionalName`
   and `configureLanguages(languages, language)` read it; `termsOf` merges the document's
   `terms[lang]` over the catalogue's `term.*` entries; the catalogue gains
   `term.<x>.plural`, `term.<x>.title`, `term.<x>.plural_title` for the four core terms (the
   `en` values must equal `terms/en.json`); `<link rel="stylesheet" href="/api/settings/theme.css">`
   in `index.html` after the bundle's CSS (no change to `api.DefaultCSP`: `style-src 'self'`
   covers it); the header shows the regional's name as text (the logo comes with `files`);
   `field.contrast` joins the field-code keys (`"{ratio}:1 is below {min}:1"`); `/settings`
   is listed among the admin nav entries (`settings.read`); `web/src/core/settings/` as a
   core area folder.
9. Applied 2026-10-19 (D39) in api.md as a note on its still-pending Proposed change 15; the two points it raises (a per-request public-origin seam, and where `ETag`/`304` may be set under `no-store`) are settled by the amendment of 2026-10-19 above (approved, owner, 2026-10-20, D40): `no-store` stays with the `ETag`; `PublicOrigin` waits, shape fixed. **`plan/design/api.md` (row 16):** none now. Proposed change 15 (trusted origins and the
   `Host` allowlist from `regional.public_url`) needs a per-request read, since settings
   change without a restart: api gains `Options.PublicOrigin func(ctx) (string, error)`
   (a seam `app` fills over `Regional`) when that change is designed; it stays pending.
   Confirm that a handler may set `Cache-Control` and `ETag` on its own response under the
   `/api/` `no-store` rule, or exempt `/api/settings/theme.css` and `/api/settings/public`
   from it.
10. Applied 2026-10-19 (D39), in authz.md. **`plan/design/authz.md` (row 7):** mark Proposed change 11 applied for the starter role
    (`settings.read`, `settings.manage` → `settings-admin`); the end-of-day zone for grant
    end dates reads `Regional().Timezone` through a `ZoneSource` seam when authz adopts it
    (authz is built after settings, so `app` can pass the service directly).
11. Applied 2026-10-19 (D39), in notify.md (its Settings section declares the keys and ranges) and identity.md. **`plan/design/notify.md` (row 11, drafted in parallel):** declare the `notify.*` keys
    named under Settings as `settings.Decl`s in a core area `notify` (`Core: true`), with
    `notify.smtp_password` of kind `Secret`; read the sender name's fallback and the links'
    base through `Regional`, the words through `TermsFor`. If notify needs to react to a
    change (pooled connections), ask for `settings.setting-changed` (Signals above).
12. Applied 2026-10-19 (D39), in 07-abstractions.md. **`plan/07-abstractions.md`, `settings` entry:** add **Owns:** "the declaration types
    (`Kind`, `Decl`, `Area`), `core.setting` and its revision, terminology (four forms per
    term per language), `regional.*`, the sealed `Secret` kind, scoped `Reader`s,
    `Regional` and `TermsFor`, the public document and `theme.css`, the settings screens";
    **Doesn't do:** "process configuration (`config`), the meaning of other areas' keys (their
    specs), enforcing retention (`jobs`), translations (web-shell), signals on change, storing the logo's bytes (`files`)".
13. Applied 2026-10-19 (D39), in the go-conventions and add-module skills. **`.claude/skills/go-conventions/SKILL.md` and `add-module/SKILL.md`:** the M4 step:
    `deps.Settings` (`settings.Reader`, bare keys, `Text` for text and enum, `Secret` for a
    sealed value) replaces the "declared default" seam; tests use
    `settingstest.NewFake(Manifest.Name, Manifest.Settings)` and `Fake.Set`; a module's
    setting titles are `module.<name>.setting.<key>.title|description|option.<opt>`;
    retention keys are declared by the module whose spec has the job; a module never
    caches a setting (the reader is a map lookup).

Proposed 2026-10-21 by the logo amendment (awaiting the owner; `spec-editor` applies them on approval):

14. **`plan/design/app.md` (row 18):** `app` passes `settings.Options.Files` as a lazy
    adapter `lazyFilesStore{a}` (every `files.Store` method resolves `a.files.Scoped("settings")`
    on first use; before files is built each answers `Unavailable`), the pattern of
    `lazyAuthorizer` (settings amendment 2026-10-19); the files part's `link` step no longer
    needs to hand settings anything (files Proposed change 4 said "a lazy `files.Store`
    scoped `settings`"; this is where it lives). `settingsRoutes` copies `MaxBodyBytes` and
    `Timeout` from `settings.Route` to `module.Route`. Failure mode: `TestLazyFilesStore_BeforeFiles_ReturnsUnavailable`.
    The e2e stack already runs the `disk` store, so the smoke run's logo round trip needs no
    profile.
15. **`plan/design/web-shell.md` (row 17):** give the "M4 files additions (D44)" the detail
    the logo field and the header need. (a) `upload<T>(route, {file: File, fields?:
    Record<string, string>}, opts?: CallOptions & {method?: "PUT" | "POST"})` in `@/api`:
    builds a `FormData` with every field before the file, the file under the name `file`;
    `fetch` with no `Content-Type` of its own (the browser sets the boundary); the same
    `traceparent`, cookie and `ApiError` handling as `api`; never retried (not idempotent);
    a timeout of `UPLOAD_TIMEOUT_MS` = 600 000 (files' `UploadTimeout`) instead of
    `TIMEOUT_MS`; `AbortSignal` from `opts`; a `413` maps to `ApiError` code
    `payload_too_large`. (b) `ui/FileField` props: `{label, name, hint?, error?, accept:
    string[] (MIME types), capture?: "environment" | "user", maxBytes?: number, value: File
    | null, onChange(file: File | null), disabled?}`; renders a visible label, hint and
    error like `Field` (the same `name` = API field path), an `<input type="file">` with
    `accept` joined, the chosen file's name and size (`useFormat`), a 44 px target; a file
    over `maxBytes` is refused client-side with the `field.too_large {max}` message and
    `onChange(null)`; a type outside `accept` (by the browser's `File.type`, a hint) is
    accepted and left to the server's sniff. (c) The header `<img>`: inside the home link,
    `src="/api/settings/logo?v=<Public.logo>"`, `alt` = the regional's name,
    `decoding="async"`, classes `h-8 w-auto max-w-[12rem]`; the name as text when `logo` is
    `""` or on `onError` (a `503` on a copy without the bytes); no `loading="lazy"`. (d)
    `applyPublic` keeps `logo` in the public store (`usePublic().logo`) and the frame
    re-renders on a refetch. (e) The catalogue keys `field.too_large` ("The file is larger
    than {max} bytes"; the UI formats `max` with `useFormat` bytes), `field.type` ("This
    kind of file isn't accepted."), `field.quota` ("The site has no room left for files."),
    `field.too_many`, `field.order` are still absent from `web/src/shell/messages/en.json`
    (only `field.malformed` exists): add them. (f) `Attachments` stays as described; the
    logo does not use it.
16. **`plan/design/files.md` (row 13):** two contract lines, no code change: under
    "Stage", "a `Body` read error that is already a classified `*errs.Error` is returned
    unchanged (settings' `MaxLogoBytes` reader relies on it)"; under "Method contracts",
    `ByRecord` "may be called inside a `Read` transaction (no audit, no store I/O; settings
    reads the logo in its snapshot load)". "Used by" names `settings` as the consumer of
    `LogoRecordKind`, `Brand`, `ReadUpload`, `Serve`.
17. **`plan/07-abstractions.md`, `settings` entry:** "Owns" gains "the regional's logo
    (`LogoRef`, `GET|PUT|DELETE /api/settings/logo`, `MaxLogoBytes`, `settings.logo.*`
    audit actions)"; "Doesn't do" keeps "storing the logo's bytes (`files`)" and adds "SVG
    sanitising (`files`, Later)".
18. **`plan/06-open-questions.md`** (plan-keeper): Q-LOGO1 to Q-LOGO4 with the
    recommendations above; on approval, the answers go to the decision log and
    `settings.md`'s Open questions.
19. **`.claude/skills/go-conventions/SKILL.md`** (one phrase in the M4 files sentence):
    "a core part that caps an upload below `files.max_bytes` wraps `Upload.Body` in a
    counting reader that fails `Invalid file too_large {max}`; `Stage` returns it unchanged
    (settings' logo)". No module needs this today; the sentence stops a second
    implementation from adding a limit to files' interface instead.
