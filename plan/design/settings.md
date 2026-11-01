# Settings

- **Status:** Implemented (2026-10-19; approved D39; logo deferred to files)
- **Package:** `internal/core/settings` (store in `internal/core/settings/postgres`, fake in
  `internal/core/settings/settingstest`)   **UI:** `web/src/core/settings/`
- **Depends on specs:** lib-errs, lib-core, lib-secret, model, config, db, audit, authz,
  telemetry, module-contract (`Plan.Settings()`), api (route mounting), web-shell
- **Amended 2026-11-01 (boundaries step 7, `boundaries.md` decision 4; items 15 and 17):** the regional's profile moved out. The `regional.*` settings area, `Regional`, `Public`, `ThemeCSS`, the logo (`LogoRef`, `MaxLogoBytes`, `SetLogo`, `RemoveLogo`, `OpenLogo`, `LogoResult`), `Options.Files` and the routes `GET /public`, `GET /theme.css`, `GET|PUT|DELETE /logo` are now `internal/core/regional` (spec `regional.md`, paths under `/api/regional/`; audit actions `regional.logo.changed|removed`, old entries keep `settings.logo.changed|removed`). `settings` no longer imports `files`, no longer adds the `regional` area (app passes `regional.Settings` as a core area) and serves four routes: `GET /`, `PUT /values`, `GET /terms`, `PUT /terms/{lang}`. Keys, defaults, ranges, permissions, tables and the revision are unchanged. The moved failure rows are F23 and F36 to F54 (numbers left unused here); the moved decisions are 6 (the public half), 8, 9 (the stylesheet), 16, 17, 18. See `regional.md`.
- **Used by:** every module (`Deps.Settings`), identity and people (`PolicySource`), audit
  (`audit.retention`, `audit.self_view`), notify (sender, regional name, public URL, terms),
  api (trusted origin, Later), authz (the regional's zone, Later), the web shell
  (the Regional form and the terms; the public document is `regional`'s), departments-events
  (the new-event form's default zone)

## Summary

**Purpose.** Hold what a regional configures while bms runs: typed, validated values with
defaults, declared by the core and the modules, edited by a settings admin, audited, and
effective without a restart. Package `internal/core/settings`; imports `model`, `lib`,
`core/db`, `core/audit`, `core/authz` (Authorizer, leaf types, `ActorFunc`),
`core/telemetry`, not `core/files` (files imports settings for its own area, since step 7). Never `core/module`, `core/identity`, `api`, `app`, `config` or a module.
Configuration (`config.md`: what reaches the database, listens, is a credential of the
process, or differs per process) is not here, and nothing here is read from the environment.

**Exported (leaf types, aliased by `module`).** `Kind` (`Bool`=1, `Int`, `Duration`, `Text`,
`Enum`, `Secret`; zero invalid; `String`), `Decl{Key, Kind, Default, Options, Check func(string) error, Normalize func(string) string}`
(+`Validate`), `Area{Name, Core bool, Decls []Decl, Check func(map[string]string) error}` (+`Validate`).
`module.SettingDecl = settings.Decl`, `module.SettingKind = settings.Kind`, `module.SettingBool = settings.Bool`, ... (Proposed change 1).

**Exported (service).**
- `Reader` (= `module.Deps.Settings`, scoped to one area; keys are bare): `Bool`, `Int` (int64),
  `Duration`, `Text` (Text and Enum), `Secret(ctx, key) (secret.Secret, error)`.
- `Forms{Singular, Plural, SingularTitle, PluralTitle string}`; `Document`, `AreaDoc`, `SettingDoc`,
  `TermDecl{Key string; Defaults map[string]Forms}` (+`Validate`; D57; a key may not end in `_plural` or `_title`, `reserved`, D58), `Changes{Values map[string]*string}`, `TermChanges{Terms map[string]FormChanges}`,
  `FormChanges{Singular, Plural, SingularTitle, PluralTitle *string}`, `Revision int64`.
- `Options{DB, Clock, Logger, Tracer, Meter, Audit audit.Recorder (scoped "settings"), Authz
  authz.Authorizer, Actor authz.ActorFunc, Areas []Area (every area; `regional.Settings` is passed by `app` like `accounts`), SealingKey secret.Secret (optional),
  RefreshInterval (default 30 s, 1 s..10 m)}` (+`Validate`); `New(Options) (*Service, error)` (no I/O).
- `*Service` (safe for concurrent use): `Scoped(area) (Reader, error)`; `TermsFor(ctx, lang)
  (map[string]Forms, error)` (every declared term, core and module, D57);
  `Document(ctx)`; `Set(ctx, Changes) (Revision, error)`; `Terms(ctx) (TermsDocument, error)`;
  `SetTerms(ctx, lang, TermChanges) (Revision, error)`; `Revision(ctx) (Revision, error)` (the current snapshot's revision; consumer: `regional`'s `ETag` and cache key); `Revise(ctx, fn func(ctx) (changed bool, err error)) (Revision, error)` (the revision lock as a seam for `regional`'s logo writes; boundaries A2 point 1);
  `Run(ctx) error` (the refresher, an `app` runnable); `Routes() []httpx.Route` (`GET /`, `PUT /values`, `GET /terms`, `PUT /terms/{lang}`; `Route` was `settings.Route` with `MaxBodyBytes`, `Timeout`, replaced by `httpx.Route`, boundaries step 2); `OpenAPI() []byte`. The logo, the public document and the stylesheet are `regional`'s (`regional.md`).
- Constants: `Languages = []string{"en", "es"}`; `CoreTerms = []string{"event", "department", "participant", "theme_camp", "lead"}` (`lead` added D57);
  `MaxBatch = 200`, `MaxTextBytes = 4096`, `MaxNameBytes = 80`, `MaxURLBytes = 256`, `MaxTermBytes = 60`,
  `MinContrast = 4.5`,
  `DefaultRefreshInterval = 30 * time.Second`, `PermissionRead = "settings.read"`, `PermissionManage = "settings.manage"`.
- `settingstest.NewFake(area string, decls []Decl) *Fake` (a `Reader` answering the defaults; `Set(key, value)`, `SetSecret(key, secret)`; safe for concurrent use).

**Key decisions.**
1. one text form per value, parsed by `Kind`, stored only when it differs from the default (a reset deletes the row).
2. the declaration types move from `module` to `settings` with aliases, as authz's and signals' did, because `settings` cannot import `core/module`.
3. (owner, 2026-10-19: the SMTP password lives in settings, sealed) a sixth kind, `Secret`: sealed at rest with AES-256-GCM under a key derived from `settings.sealing_key` (a generated field of the secrets file, config Proposed change 9), never returned by any route (`secret_set` only), read only by Go consumers.
4. one in-memory snapshot per process, loaded on first read, replaced after each local commit and by a 30 s revision poll (`core.setting_revision`), so a read is a map lookup and a replica follows the writer without LISTEN/NOTIFY (which a standby cannot receive).
5. a stored value that no longer parses after an upgrade yields the default and a warning, never an error.
6. the regional's own values (`regional.*`) are a core area, declared by `regional` (step 7; before, here) and passed in by `app`; only they are public.
7. (owner, 2026-10-19: four forms) terminology is data: four forms per term per language, defaults embedded here (`terms/<lang>.json`), overrides in `core.terminology`; the shell's `term.*` catalogue entries stay as the boot fallback and must equal the defaults (test).
8. (superseded 2026-10-20, D44; moved 2026-11-01) the logo lives in `regional` (`regional.md`, decision 3): stored and served by `files`, `GET|PUT|DELETE /api/regional/logo`, audit `regional.logo.changed|removed`, `MaxLogoBytes` 2 MiB.
9. (owner, 2026-10-19) colours are one brand colour per scheme, checked for 4.5:1 against the shipped background, served by `regional` as `/api/regional/theme.css` (light inside `@media not (prefers-color-scheme: dark)`, dark inside `(prefers-color-scheme: dark)`, an unset scheme emits nothing; implementation, 2026-10-19). Duration `range` params are plain whole units (`1h`, `8760h`), not `1h0m0s`.
10. no signal on change: every consumer reads at use-case time from the snapshot; nothing downstream holds a copy.
11. no automatic retention keys per module: the module that runs the job declares its `<name>.retention*` keys when its spec has the job that enforces it (owner, 2026-10-19).
12. `regional` is a reserved area name (only `regional.Settings` may declare it).
13. values never appear in logs, audit entries, spans or error messages.
14. `settings.read` goes to `settings-admin` only; other admins see what they need on their own screens (owner, 2026-10-19).
15. (owner, 2026-10-20, D40) no early area check: an area `Check` runs only inside the `Set` transaction over stored values; `regional.public_url` is canonicalised (http/https, host required, no query/fragment/userinfo, trailing slashes removed); a `Check` cannot see whether a `Secret` is set and re-saving a `Secret` always counts as a change; the sealing and contrast tests are unit tests.
16. (moved 2026-11-01 to `regional.md`, decision 3 and decision 7) the logo rules; the snapshot no longer carries the logo's file ID because settings no longer imports `files`: the logo bumps the revision through `Revise`, and `regional` caches the ID by revision (boundaries A2 point 1).
17. (moved 2026-11-01 to `regional.md`, decision 3) the logo's kinds, size bound and the refused extras (no SVG, no pixel check, no dark-scheme logo, no favicon; owner, 2026-10-21, D47).
18. (moved 2026-11-01 to `regional.md`, decision 3) the logo write refuses a read-only copy before the body is read; `RemoveLogo` with no logo is a no-op.
19. (owner, 2026-10-30, D57) a module or core area may declare terms (`Area.Terms []TermDecl`), one flat namespace: a key declared by two areas, or equal to a `CoreTerms` name, is `Invalid` at `New`, whose fix is promoting the word to `CoreTerms` (as `lead` was); a stored row of an undeclared term is kept and ignored. (D58) A module term key may not end in `_plural` or `_title` (`reserved`:
  `term_x_plural` would be both a form of `x` and the term `x_plural`), and the terms document promises no order: the Terminology screen orders terms core first, then modules alphabetically by title, then terms by key.

**Errs kinds callers handle.** Readers: `Invalid` (undeclared key, kind mismatch), `Unavailable`/`Timeout`/`Canceled` (first load), `Unsupported` (`Secret` with no sealing key), `Internal` (unsealing failed). Use cases:
`Unauthenticated`, `Permission`, `Invalid` with field = the setting key and codes `unknown`, `malformed`, `one_of`, `range`, `too_long {max}`, `required`, `control_chars`, `url`, `contrast {ratio, min}`, `rejected` (a `Check`'s reason), `too_many {max}`; `Unsupported` (a secret without a sealing key); `Unavailable` wrapping `db.ErrReadOnly` on every write; `Run`: `Conflict` (started twice).
The logo's errors are `regional`'s (`regional.md`). No sentinels.

**Owns.** Tables `core.setting`, `core.setting_revision`, `core.terminology` (nothing personal); permissions `settings.read`, `settings.manage` (site, reach `internal`, starter `settings-admin`); no area of its own to declare (the `regional` area is `regional`'s); route set `settings`:
`GET /{$}`, `PUT /values`, `GET /terms`, `PUT /terms/{lang}`; screens `/settings` (with the Regional form and the logo field, which call `regional`'s routes), `/settings/terms`, `/settings/:area`; keys `core.settings.*` (`core.settings.logo.*` for the field; `core.regional.setting.*` belong to `regional`'s declarations but ship in this folder's catalogue); audit actions `settings.setting.changed|reset`, `settings.term.changed|reset`; spans `settings.<use-case>`, `settings.load`, `settings.refresh`; metrics `bms.settings.refresh{outcome}`, `bms.settings.stale_values`.
Config key proposed: `settings.sealing_key` (`BMS_SETTINGS_SEALING_KEY`, generated into `secrets.json` as `settings_sealing_key`). No signals.

**Expects of dependencies.** `app`: part 3 (`declare`: `Set`, `Permissions`; `build`: `New` over the areas (`regional.Settings`, `identity.Settings`, `people.Settings`, `auditsettings.Settings`, `Plan.Settings()` as module areas), the actor closure, `audit.Scoped("settings")`, `cfg.Settings.SealingKey`; `a.deps.Settings` scoped per module in `provision`;
`PolicySource` adapters for identity and people; `Run` as a runnable; route set `settings`). `module-contract`: the aliases, `Deps.Settings settings.Reader`, `moduletest.Conformance` supplying `settingstest.NewFake`, `regional` reserved. `identity`, `people`, `audit`: export `Settings []settings.Decl` (identity also `CheckSettings`). `authz`: `Allow`, starter-role seeding. `audit`:
`Recorder` inside a `Write` transaction. `db`: `Transact`, `AfterCommit`, `Wrap`, `ErrReadOnly`, `Set`.
`web-shell`: reads `GET /api/regional/public` at boot, merges `terms` over its catalogue, links `/api/regional/theme.css`, adds the three term forms to its catalogue and the `field.contrast` key; owns `upload()` in `@/api`, `ui/FileField` and the header `<img src="/api/regional/logo?v=<Public.logo>" alt="<regional name>">` with its text fallback (Proposed change 15; paths moved by boundaries step 7).
`notify`: declares `notify.*` (named under Settings) and reads `Regional`, `TermsFor` and its own `Reader`. `regional` (step 7): `regional.Settings` is passed as a core area; `GET /public` carries `TermsFor` (`regional.md`, decision 8: `Options.Terms`, required). `files`: nothing; `settings` no longer imports it.

**Open questions:** the six of 2026-10-19 were answered (D39) and are folded into decisions 3, 7, 8, 9, 11 and 14; the four logo questions were answered (owner, 2026-10-21, D47) and are folded into decision 17: no SVG now (a sanitised kind later in `files`), no dark-scheme logo, no favicon from the logo, no pixel-dimension cap.
**Settled 2026-10-20 (D40):** the amendment of 2026-10-19 is approved (`lazyAuthorizer`; `no-store` with `ETag`; `PublicOrigin` waits); decision 15 below; the implementation deviations marked 2026-10-19 are accepted as written. **Proposed changes elsewhere:** 13, all applied 2026-10-19 (D39), listed at the end; 14 to 19 applied 2026-10-21 (D47).

## Purpose

Hold what a regional configures while bms runs, so that the core and every module read one typed, validated, audited value per setting without a restart.

## Responsibilities

- The declaration seam: the core areas and the modules declare their settings (`Decl`,
  `Area`); this spec validates the declarations once at construction and refuses duplicates.
- Storage of the values a regional sets, the defaults for everything else, and the one
  revision number every write bumps.
- Validation of every change: by kind, by the declaration's `Check`, and by the area's
  cross-field `Check`.
- Terminology: the words a regional uses for the core's things, per language, in the forms
  the UI and notifications need.
- Reads: scoped `Reader`s for modules and core areas, `TermsFor`
  for notify and the public document (which `regional` builds).
- Caching: one snapshot per process, refreshed after a local commit and by a revision poll.
- Audit of every change, permissions on every read of the admin document and every write,
  sealing of secret values at rest.
- The admin screens.

## Not responsible for

- **Process configuration** (what reaches the database, listens, is a credential of the
  process, or differs per process): `config.md`. Its rule decides which side a value is on;
  this spec adopts it (config.md "Settings").
- **Which modules are enabled**: `config` (`modules.disabled`) and `module-contract`.
- **What each setting means**: the declaring spec (`accounts.*` identity, `people.*` people,
  `audit.*` audit, `notify.*` notify, `<module>.*` the module). This spec stores, checks the
  shape, and hands the value back; it never interprets one (the `regional.*` checks are `regional`'s).
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
- **The regional's profile, the public document, `theme.css` and the logo** (the `regional.*` area and its checks, `Profile`, the logo's routes, cap, kinds and audit entries): `regional` (`regional.md`, step 7). The Regional form and the logo field are still screens here; they call `regional`'s routes. Storing the logo's bytes: `files`. The header `<img>`, `upload()` and `ui/FileField`: `web-shell`.
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
| Terminology | The regional's words for the core's things, per language, in four forms. | `core.terminology`, `terms/<lang>.json`, `TermsFor` |
| Routes and screens | The admin document, the forms, the terms routes. | route set `settings`, `web/src/core/settings/` |

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
	// Normalize is an optional rewrite of a trimmed value to its canonical form, run before
	// Check and before the value is stored (boundaries A2 point 5). Rules: pure; a fixed point
	// (Normalize(Normalize(v)) == Normalize(v)), so a value it cannot improve is returned
	// unchanged and Check reports why it is refused; run only for non-Secret kinds. A Secret
	// Decl with a Normalize fails Validate (Invalid, field "normalize", code "unsupported"):
	// a secret is never trimmed or rewritten. The canonical form is the declaring area's rule,
	// as Check is (regional.public_url declares it).
	Normalize func(value string) string `json:"-"`
}

// Validate fails errs.Invalid with one validate.FieldError per problem on "key", "kind",
// "options", "default", "normalize" (module-contract F6, unchanged; plus: a Secret with a
// Default or a Normalize).
func (d Decl) Validate() error

// Area is one owner of settings: a module (Core false) or a core area (Core true).
type Area struct {
	Name  string // a module name or a core area name; "regional" is declared by the `regional` package
	Core  bool
	Decls []Decl
	// Terms are the words this area lets a regional rename (D57). Keys are one flat
	// namespace across areas and CoreTerms.
	Terms []TermDecl
	// Check is an optional cross-field rule over the area's effective values (every
	// declared key, defaults filled in, bare keys). Pure. Field names it returns are bare
	// keys and are prefixed with "<area>." by Set. Runs inside Set's Write transaction over
	// values read there, never over the snapshot (Behavior, "Writes"). A Secret key reads "".
	Check func(values map[string]string) error `json:"-"`
}

// Validate fails errs.Invalid: name not an ident, a Decl invalid, duplicate keys,
// terms[i].key duplicate within the area or reserved (a CoreTerms name) (D57).
func (a Area) Validate() error

// TermDecl declares a word a regional may rename (D57). Key is the term's identity
// everywhere: core.terminology.term, TermsDocument, the shell's {term_<key>[_plural|_title|
// _plural_title]} and notify's {{term "<key>"}}.
type TermDecl struct {
	Key      string           // [a-z][a-z0-9]*(_[a-z0-9]+)*, at most 32 bytes; not in CoreTerms; not ending in _plural or _title (D58)
	Defaults map[string]Forms // per language in Languages; "en" required, every form set
}

// Validate fails errs.Invalid: key format (code format) or reserved (a CoreTerms name, or a
// key ending in _plural or _title, which would collide with a form's argument name; code
// reserved); a language not in Languages (defaults.<lang>, code unknown); "en" missing
// (defaults.en, code required); a form empty after trimming (required), over MaxTermBytes
// (too_long {max}), with control characters (control_chars) or not valid UTF-8 (malformed).
// Fields: key, defaults.<lang>, defaults.<lang>.<form>.
func (d TermDecl) Validate() error

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

// Regional (the typed regional values) moved to regional.Profile (regional.md, step 7).

// Forms are the four forms of one term in one language.
type Forms struct {
	Singular      string `json:"singular"`       // "department"
	Plural        string `json:"plural"`         // "departments"
	SingularTitle string `json:"singular_title"` // "Department"
	PluralTitle   string `json:"plural_title"`   // "Departments"
}

// Public (the public document) moved to regional.md, step 7; it carries TermsFor.

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
	Area     string      `json:"area"` // "core" for CoreTerms, else the declaring area's name (D57)
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

// ---- The logo ---- moved to regional.md (MaxLogoBytes, LogoRef, LogoResult), step 7.

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
	Areas           []Area           // every area, including regional.Settings as a core area (passed by app)
	SealingKey      secret.Secret    // optional; without it Secret values cannot be set or read
	RefreshInterval time.Duration    // 0 = DefaultRefreshInterval; 1s..10m
}

// Validate fails errs.Invalid with fields settings.db, settings.clock, settings.logger,
// settings.tracer, settings.audit, settings.authz, settings.actor, settings.refresh_interval,
// settings.areas[i].<field>, settings.areas (duplicate area).
func (o Options) Validate() error

// New validates and builds the service. No I/O: it succeeds against db.None().
func New(o Options) (*Service, error)

// Scoped returns the Reader for one declared area. Fails errs.Invalid for an unknown area.
func (s *Service) Scoped(area string) (Reader, error)

// TermsFor returns every declared term (core and module, D57) in one language, overrides applied over the embedded
// defaults (a language without a defaults file uses "en"). Consumers: notify, regional's public document.
// Fails errs.Invalid (lang not in Languages) and the snapshot's kinds.
func (s *Service) TermsFor(ctx context.Context, lang string) (map[string]Forms, error)

// Document needs settings.read.
func (s *Service) Document(ctx context.Context) (Document, error)

// Set needs settings.manage; validates every change, then writes all or nothing, audits
// one entry per key inside the transaction, bumps the revision and replaces the snapshot
// after commit. Returns the new revision.
func (s *Service) Set(ctx context.Context, c Changes) (Revision, error)

// Revision is the current snapshot's revision. No permission. Consumer: regional (the ETag
// of GET /public and GET /theme.css, and the key of its logo-ID cache).
func (s *Service) Revision(ctx context.Context) (Revision, error)

// Revise runs fn inside one Write transaction after taking the revision row lock, which
// serialises it with Set, SetTerms and every other Revise across instances. When fn
// reports changed, Revise bumps the revision and, after commit, installs the snapshot it
// built under the lock (as Set does); when fn reports no change, it commits nothing new
// and returns the current revision. fn's error rolls everything back and is returned with
// its kind. Revise records no audit entry (fn records under its own area) and checks no
// permission: it is a method of *settings.Service, which only app holds (modules see a
// Reader), and the caller has checked its own permission first. Call it outside any
// transaction. Consumer: regional (SetLogo, RemoveLogo). Failure modes: boundaries B34 to
// B38 (fn fails, read-only copy, no change, no revision row F26, concurrent callers).
func (s *Service) Revise(ctx context.Context, fn func(ctx context.Context) (changed bool, err error)) (Revision, error)

// Terms needs settings.read. The map carries no order and none is promised: the Terminology
// screen orders what it shows (core first, then modules alphabetically by title, then terms
// by key; D58).
func (s *Service) Terms(ctx context.Context) (TermsDocument, error)

// SetTerms needs settings.manage; same transaction rules as Set.
func (s *Service) SetTerms(ctx context.Context, lang string, c TermChanges) (Revision, error)

// Run is the refresher: every RefreshInterval it reads core.setting_revision and reloads
// the snapshot when it moved. Returns nil when ctx ends; errs.Conflict if already running.
func (s *Service) Run(ctx context.Context) error

func (s *Service) Routes() []httpx.Route // GET /, PUT /values, GET /terms, PUT /terms/{lang}
func (s *Service) OpenAPI() []byte

// IDOf derives the fixed audit reference for a key (as events does for kinds): the first
// 16 bytes of sha256("settings.setting:" + key). Cannot fail.
func IDOf(key string) [16]byte
```

**Consumers per method.** `Reader.Bool/Int/Duration/Text`: every module for its declared
keys (hello: `Text("greeting")`), app's `PolicySource` adapters for identity (`Enum`, `Duration`,
`Int`) and people (`Enum`), jobs for `audit.retention` (`Duration`); `Bool` has no core consumer
yet and exists because `Bool` is a declared kind a module may use. `Reader.Secret`: notify
(`notify.smtp_password`). `TermsFor`:
notify's templates, regional's public document. `Scoped`: app (`provision`, the core adapters). `Document`,
`Set`, `Terms`, `SetTerms`: the
handlers. `Run`: app. `IDOf`: this package's audit entries and tests.


**Pre- and post-conditions.** Every use case checks its permission first (`Allow`), then
validates, then writes in one `Transact(Write)`. A write on a read-only copy fails
`Unavailable` wrapping `db.ErrReadOnly` and changes nothing. A successful `Set`/`SetTerms`
is visible to the next read in the same process before the call returns.

## Types

Defined above. Uses `model.Email` (still used by `regional`'s checks; see `regional.md`),
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
  term       text NOT NULL,               -- a core term or a declared term key (D57)
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
| `settings.manage` | `PUT /values`, `PUT /terms/{lang}` (and, in `regional`, `PUT /logo`, `DELETE /logo`) | site | internal | `settings-admin` |

The logo's permissions and its public reads (`GET /public`, `/theme.css`, `/logo`) are `regional`'s; `settings.manage` is still the permission that guards its writes.

`settings.manage` does not imply `settings.read`; the starter role holds both, and a regional
that gives a role `manage` alone gets a screen that can save and not show, which the screen
says. `settings.read` goes to `settings-admin` only (owner, 2026-10-19, D39): other admins see what
they need on their own screens, and a module's settings are that module's business. Module readers (`Deps.Settings`) are not
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

Moved 2026-11-01 (boundaries step 7) to `regional.md`: the area `regional` (seven keys, `core.regional.setting.<key>.*`; keys, defaults, ranges and who can change them unchanged). `settings` still stores their values, checks their shape and calls their `Check`.

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
**`regional.public_url` canonical form (owner, 2026-10-20, D40).** Moved 2026-11-01 (boundaries step 7) to `regional.md` (the rule is `regional`'s `Check`); the decision-5 warning for a stored value that no longer parses still applies.

What notify's draft calls `site.name`, `site.public_url` and `site.default_language` are
`regional.name`, `regional.public_url` and `regional.language`, read through
`regional.Profile`; the terminology map is `TermsFor`. Notify's `SMTPSource`, `SiteSource` and
`RetentionSource` seams are adapters `app` writes over `Scoped("notify")` and `regional.Profile`,
as for identity's and people's `PolicySource`.

**Terminology.** Terms are `CoreTerms` (`event`, `department`, `participant`, `theme_camp`, `lead`; `lead` added D57) and the terms areas declare (`Area.Terms`, D57),
each with four forms per language in `Languages`. Defaults are embedded in
`internal/core/settings/terms/<lang>.json` (`en` in M4; `es` when its catalogue lands);
a language without a file uses `en`'s. The shell's `term.*` catalogue entries must equal the
`en` defaults (test in `internal/app`, Proposed change 8). A form override is 1..`MaxTermBytes`
bytes, trimmed, no control characters. A module that needs a word of its own declares it in `Manifest.Terms` (module-contract, D57), with `en` defaults in Go; its fallback catalogue keys are `module.<name>.term.<key>[.plural|.title|.plural_title]`.

**Constants that are not settings**, pinned by `TestConstants_Pinned`:

| Constant | Value | Why not configurable |
|---|---|---|
| `RefreshInterval` | 30 s (`Options`, for tests) | a replica's staleness bound; nothing a regional should tune |
| `MaxBatch`, `MaxTextBytes`, `MaxTermBytes`, `MaxNameBytes`, `MaxURLBytes` | 200, 4096, 60, 80, 256 | bounds, not choices |
| `MinContrast` | 4.5 | WCAG 2.2 AA for text (principle 4); `regional`'s colour checks read it |
| sealing | AES-256-GCM, HKDF-SHA256 over the sealing key with info `bms settings seal v1` | one algorithm, in the standard library |

## UI

Core screen set `settings` (`web/src/core/settings/`, `kind: "core"`), nav entry
**Settings** in the admin area, `access: ["settings.read"]`. Every screen is phone first
(320 px, 44 px targets), uses `ErrorState`, `ErrorSummary`, `useFieldErrors`, typed keys
`core.settings.*`, no `style` prop.

| Screen | Who sees it | Actions | States |
|---|---|---|---|
| `/settings` (Regional) | `settings.read` | name, contact mailbox, public URL, language (select from `languages`), time zone (select from `Intl.supportedValuesOf("timeZone")`, first entry "browser's zone"), two colour fields with a live swatch and the computed contrast ratio; one **Save** (`PUT /values`); a **Logo** section under the form (its own `<section>`, its own requests, not part of Save: the preview, a `FileField` and **Upload** or **Replace**, **Remove logo** with a confirm `Dialog`; detail under "The logo field" below); links to Terminology and to each area | loading; error; forbidden (403 → `ErrorState` with the permission message; `manage` without `read` shows an explanatory `Banner` and no form); read-only copy (the shell's banner; Save answers `read_only` and the form keeps its edits; the logo controls are hidden, the preview stays) |
| `/settings/terms` | `settings.read` | a language select; per term a card with four fields, the default as placeholder, a **Reset** per field; cards grouped by `TermDoc.Area`: the core's first, then one `<fieldset>` per module headed by `module.<name>.title`, modules alphabetically by that title and terms by key within a group (D57, D58; the document itself carries no order); one **Save** per language (`PUT /terms/{lang}`) | as above; empty never (terms always exist) |
| `/settings/:area` | `settings.read` | the area's settings as a form: a toggle (bool), a number, a duration (`DurationField`: number plus unit, sent as `h`/`m`/`s`), text, select (enum, labels from `.option.<opt>`), secret (`password` input showing "set" or "not set", a Replace and a Clear); a **Reset to default** per setting; one **Save** (`PUT /values` with only the changed keys) | loading; error; forbidden; an unknown area → not-found; a `stale` value shows a warning and the default |

Each field's title and description come from the declaring area's catalogue (decision 8
derivation); a missing key shows the raw setting key and is reported (web-shell decision 2).
After a save the shell refetches `GET /api/regional/public` and re-applies the terms and
the regional name, so the admin sees the change without a reload. Other browsers see it
on their next boot (the document is fetched at boot and not cached across sessions).

The shell changes this spec needs are in Proposed change 8 (terms merge, theme link, the
`field.contrast` key, `useRegionalName` and `configureLanguages` fed from the public document).

**The logo field** (`web/src/core/settings/LogoControls.tsx`, used by
`screens/Regional.tsx` under the regional form; catalogue keys `core.settings.logo.*`). Who
sees what: `settings.read` sees the section; the upload and remove controls render only
with `settings.manage` and not on a read-only copy (`useGate`, the shell's read-only store);
`manage` without `read` gets no section (the screen shows no form, as today).

- **Preview.** When `Public.logo` is set (the shell's public document, `usePublic`): an
  `<img src="/api/regional/logo?v=<logo>" alt={t("core.settings.logo.current_alt")}>`
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
  not replace the live logo for everyone. On press: `upload("/api/regional/logo", {file},
  {method: "PUT"})`; the button is `aria-busy` and disabled while in flight; the `Toast`
  `core.settings.logo.uploaded` ("Logo updated") on success; then the shell's public
  document is refetched (as after Save) so the header and the preview change without a
  reload. `FileField`'s client-side `maxBytes` check shows `field.too_large` at once
  without a request; the server enforces it anyway.
- **Remove.** **Remove logo** (`core.settings.logo.remove`), a `Dialog` asking
  `core.settings.logo.remove_confirm` ("Remove the logo? The site will show its name as
  text instead.") with **Remove** and **Cancel**; `DELETE /api/regional/logo`; `Toast`
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
`Public.logo` is set, the home link holds `<img src="/api/regional/logo?v=<logo>"
alt="<regional name>" decoding="async" class="max-h-8 w-auto max-w-[12rem]">` (at most a 32 px line on
a phone; the home link has `min-w-11` so a square logo still gives a 44x44 target; review correction, D47: a fixed `h-8` distorted a logo wider than 6:1, `max-h-8` keeps the aspect ratio and a short logo renders smaller; `max-w` so a wide logo cannot push the menu button off a 320 px screen); the link's
accessible name is therefore the regional's name, as it is with text. When unset, or when
the image fails to load (`onError`: a copy without the bytes answers `503`), the regional's
name as text, exactly as today. No `loading="lazy"` (above the fold). The `?v=` query
carries the file ID so a replaced logo is a new URL under `no-store` (decision 25 in files;
D45: no browser cache). One logo for both colour schemes (decision 17); a regional whose logo
needs a dark variant uploads one with a transparent background.

## Behavior

**Construction.** `New` validates `Options`, validates every
area, refuses duplicate area names (a second `regional` area is a duplicate; the reserved name is `module`'s check, `api.md` item 24; the area itself is passed in by `app` from `regional.Settings`), and builds
the declaration index (`fullKey -> (Area, Decl)`). No I/O.

**Snapshot.** `snapshot{revision, values map[fullKey]string (text forms as stored),
sealed map[fullKey][]byte, terms map[lang]map[term]map[form]string, loadedAt}` behind an
`atomic.Pointer`. The first read on a process loads it (single-flight, one `Transact(Read)`
with a derived deadline, span `settings.load`); until it exists every read fails with the
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
(`unknown`), each non-nil value trimmed (not for `Secret`), normalised (`Decl.Normalize`, when set, non-`Secret` only), parsed by kind, the `Decl.Check`
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

**Public document and `theme.css`.** Moved 2026-11-01 (boundaries step 7) to `regional.md` (`GET /public`, `GET /theme.css`, the `ETag` and the derived colours).

**The logo** (decisions 16 to 18). Moved 2026-11-01 (boundaries step 7) to `regional.md`: `SetLogo`, `RemoveLogo`, `OpenLogo`, their handlers, the rationale for the snapshot's logo ID and the export/deletion note. How the logo write takes part in the revision lock without `settings` importing `files` is `regional.md`, Open question 2.

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
 The logo routes' behavior on a read-only copy is `regional`'s.

**Concurrency and lifecycle.** `*Service` is safe for concurrent use. `Run` is the one
goroutine, started by `app` as a runnable after `Set.Start`, exiting when its ctx ends;
nothing to `Close`. A second `Run` fails `Conflict`. Idempotency: `Set` with the current
values is a no-op write (no rows change, no audit entry, no revision bump). Retries: none here; the shell retries `GET`s, never `PUT`s (web-shell
decision 11).

**Telemetry.** Spans `settings.<use-case>` with `settings.keys` (`Int`, the batch size) and
`settings.area` (`Token`); never a key's value. The logo's spans are `regional`'s. Logs carry `setting` (the key, a token) and
never a value. The leak test (`telemetrytest`) sets a marker as a text value and a secret
and asserts it appears in no log, span or audit entry.

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `New` | a nil `DB`, `Clock`, `Logger`, `Tracer`, `Audit`, `Authz` or `Actor` | Invalid (`settings.<field>`) | fix the wiring | `TestNew_NilDependency_ReturnsInvalid` |
| F2 | `New` | `RefreshInterval` outside 1 s..10 m | Invalid (`settings.refresh_interval`) | fix the wiring | `TestNew_RefreshIntervalRange_ReturnsInvalid` |
| F3 | `New` | an area invalid: name not an ident, a `Decl` failing `Validate`, duplicate keys | Invalid (`settings.areas[i].*`) | fix the declaring package | `TestNew_InvalidArea_ReturnsInvalid` |
| F4 | `New` | two areas with one name, or a non-core area named `regional` | Invalid (`settings.areas`) | fix the wiring | `TestNew_DuplicateArea_ReturnsInvalid` |
| F5 | `Decl.Validate` | a `Secret` with a non-empty `Default` or `Options` | Invalid (`default`) | fix the declaration | `TestDeclValidate_SecretWithDefault_ReturnsInvalid` |
| F6 | `Scoped` | an undeclared area | Invalid | fix the wiring | `TestScoped_UnknownArea_ReturnsInvalid` |
| F7 | `Reader.*` | a key the area did not declare | Invalid | fix the caller | `TestReader_UndeclaredKey_ReturnsInvalid` |
| F8 | `Reader.*` | the method's kind does not match the declaration (`Int` on a text) | Invalid | fix the caller | `TestReader_KindMismatch_ReturnsInvalid` |
| F9 | `Reader.*`, `TermsFor` | no snapshot yet and the load fails (database down) | Unavailable (or Timeout, Canceled) | the use case fails; the api answers 503; the shell keeps its defaults and retries | `TestReader_FirstLoadFails_ReturnsUnavailable` |
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
| F23a | `Set` | the snapshot is stale (a copy that has not polled) and the batch is valid against the stored values | accepted: the area `Check` runs inside the transaction over the stored rows, so a stale snapshot cannot refuse it | nothing | `TestSet_StaleSnapshot_AreaCheckUsesStoredRows` |
| F24 | `Set`, `SetTerms` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | the api answers `503 read_only`; the screen keeps its edits | `TestSet_ReadOnly_ReturnsUnavailable` |
| F25 | `Set`, `SetTerms` | the audit `Record` fails | the recorder's error; nothing written | the use case fails | `TestSet_AuditFails_RollsBack` |
| F26 | `Set`, `SetTerms` | `core.setting_revision` has no row | Internal | run `bms migrate`; the operator sees the reference | `TestSet_NoRevisionRow_ReturnsInternal` |
| F27 | `Set`, `SetTerms` | the store fails otherwise | Unavailable, Timeout, Canceled (`db.Wrap`) | the use case fails | `TestSet_StoreFails_ReturnsKind` |
| F28 | `SetTerms` | `lang` not in `Languages` | Invalid (`lang`, `one_of`) | show the field error | `TestSetTerms_UnknownLanguage_ReturnsInvalid` |
| F29 | `SetTerms` | a term not declared (not in `CoreTerms` or any area's `Terms`, D57), or a reset naming an unknown form | Invalid (`terms.<term>` / `terms.<term>.reset`, `unknown`) | show the field error | `TestSetTerms_UnknownTerm_ReturnsInvalid` |
| F29a | `New` (via `Options.Validate`) | a term key declared by two areas (D57) | Invalid (`areas[j].terms[i].key`, `duplicate`; the message names promotion to `CoreTerms`) | fix the manifests | `TestNew_DuplicateTermAcrossAreas_ReturnsInvalid` |
| F29b | `TermDecl.Validate`, `Area.Validate` | key malformed, reserved (a `CoreTerms` name, or ending in `_plural` or `_title`, D58), or duplicate in the area; unknown language; `en` missing; a form empty, too long, with control characters or not valid UTF-8 (D57) | Invalid (`terms[i].key` `format`/`reserved`/`duplicate`; `defaults.<lang>` `unknown`; `defaults.en` `required`; `defaults.<lang>.<form>` `required`/`too_long {max}`/`control_chars`/`malformed`; the codes as built, accepted D58). Deviation (accepted, owner, D58): a `too_long` raised at start-up through `Addf` loses its `max` param (the error is still `too_long` on the same field) | fix the declaration | `TestTermDecl_Validate_Table` |
| F29c | `TermsFor`, `Public` | a module term with no override, or no entry in the language (D57) | — (the override, else the `en` default) | none | `TestTermsFor_ModuleTerm_DefaultsAndOverride` |
| F29d | `Terms`, load | `Terms` lists only declared terms, with `Area`; a stored row of an undeclared term is ignored at load and kept (D57) | — | none | `TestTerms_ListsDeclaredModuleTermsWithArea`, `TestLoad_UndeclaredTermRow_IgnoredAndKept` |
| F30 | `SetTerms` | a form empty after trimming, too long, or with control characters | Invalid (`terms.<term>.<form>`, `required` / `too_long {max}` / `control_chars`) | show the field errors | `TestSetTerms_FormShape_Table` |
| F31 | `SetTerms` | no term in the batch, or more than the number of declared terms (D57) | Invalid (`terms`, `required` / `too_many`) | fix the request | `TestSetTerms_Empty_ReturnsInvalid` |
| F32 | `Run` | called while running | Conflict | fix the wiring | `TestRun_Twice_ReturnsConflict` |
| F33 | `Run` | a poll or reload fails | none returned: logged once per minute, counted `failed`, old snapshot kept | nothing | `TestRun_PollFails_KeepsSnapshotAndWarnsOnce` |
| F34 | `Run` | ctx ends | nil | nothing | `TestRun_ContextDone_ReturnsNil` |
| F35 | handlers | malformed JSON, wrong content type | Invalid (`httpx.ReadJSON`'s) | show the error | `TestPutValues_BadBody_Returns400` |

Moved to `regional.md` (step 7), with numbers and tests unchanged: F23 (`regional.*` checks), F36 to F54 (public document, theme and the whole logo). Those numbers are left unused here. `Routes`/`OpenAPI` cannot fail beyond F9 and `Internal` on an unencodable document (a bug). `TestConstants_Pinned` stays here.

## Lock-in check

No external technology beyond PostgreSQL (the project's database) and the Go standard
library (`crypto/aes`, `crypto/cipher`, `crypto/hkdf`, `crypto/sha256`). The storage seam is `postgres/` behind the service's own
store interface (D8), so a fork can swap it. Nothing here calls out of the process.

## Test plan

- **Unit (no infrastructure):** `Decl`/`Area` validation tables; value parsing per kind;
  the sealing (crypto) tests (round trip, wrong key, moved ciphertext) and the contrast tests
  are unit tests with no build tag and no database (owner, 2026-10-20, D40); the snapshot (load once, after-commit replace, out-of-order
  install refused, stale value → default and one warning); `Reader` scoping and kind checks
  over a store fake; sealing round trip, wrong key, moved ciphertext (AAD); the flattening of terms; `Run` with a fake
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
- **Logo, theme, public document and the `regional.*` checks:** moved to `regional.md`'s test plan with their names (`TestLogo_*`, `TestPublic_*`, `TestTheme_*`, `TestSet_Regional_Table`, `core_settings_logo_test`). `TestSet_Regional_Table` keeps the canonical-form rows, in `regional`.
- **Normalize and Revise (boundaries A2):** `TestSet_NormalizeRunsBeforeCheck`, `TestDeclValidate_SecretWithNormalize_ReturnsInvalid`, `TestRevise_FnFails_RollsBackAndKeepsRevision`, `TestRevise_ReadOnlyCopy_ReturnsUnavailable`, `TestRevise_Unchanged_ReturnsCurrentRevision`, `TestRevise_NoRevisionRow_ReturnsInternal`, `TestRevise_Changed_BumpsAndInstallsSnapshot`, `TestRevise_Concurrent_Serialised` (integration). Playwright still runs the logo round trip and `axe` on `/settings` with and without a logo against `/api/regional/`.
- **Module terms (D57):** the F29a to F29d tests, `TestSetTerms_UndeclaredModuleTerm_ReturnsInvalid`; the shell's `term.lead*` entries join the `terms/en.json` equality test.
- **Catalogue tests (`internal/app`):** the shell's `term.*` entries equal
  `terms/en.json`; every core area's declared keys have their catalogue titles.
- **End-to-end (Playwright, phone and desktop):** a settings admin renames "department" to
  "team" and the nav and the departments page title change without a reload; a
  participant cannot open `/settings`; a bad colour shows the contrast error.

## Amendments

Amendments 2026-10-19 (D40), 2026-10-20 (D44), 2026-10-21 (D47), 2026-10-28 (D52(e)) and 2026-10-30 (D57, D58) are folded into the text above. Their narratives, the reasons for each choice and the superseded wording are in `plan/design/history/settings.md`.

Amendment 2026-11-01 (boundaries step 7, `boundaries.md` decision 4, items 15 and 17; A2: `Revise`, `Revision`, `Decl.Normalize`, audit actions `regional.logo.changed|removed`): the regional's profile, public document, theme and logo moved to `regional.md`; `Options.Files` is removed; routes are `GET /`, `PUT /values`, `GET /terms`, `PUT /terms/{lang}`; the `regional` area is passed in as a core area, not added by `New`. Done in place: this spec's Summary and body say so where they changed; the moved text, rows F23 and F36 to F54 and the logo tests are in `regional.md`. The step is not yet implemented, so the narrative moves to `history/settings.md` when it is.

## Open questions

None. Answered 2026-10-19 (D39), record below, and 2026-10-21 (D47), the four logo questions:

- **Q-LOGO1. Accept SVG logos now?** Answered (owner, 2026-10-21, D47): no (L3, decision 17).
  `files.Brand` stays PNG, JPEG, WebP; when a regional asks, `files` adds a sanitised `SVG`
  kind for `public` files and `Brand` gains it, with no change here. A regional exports its
  vector logo to PNG or WebP at header size today.
- **Q-LOGO2. A second logo for the dark colour scheme?** Answered (owner, 2026-10-21, D47):
  not now (decision 17). One logo, and the hint tells the admin to upload one with a
  transparent background that reads on both; a `logo_dark` variant (a second fixed record,
  `Public.LogoDark`, a `<picture>` with a `media` source) is an additive change when a
  regional reports the problem.
- **Q-LOGO3. Derive the favicon from the logo?** Answered (owner, 2026-10-21, D47): no
  (decision 17). The favicon stays the shipped one ("Not responsible for": a favicon);
  resizing is files' Later; a regional that wants one forks `index.html`.
- **Q-LOGO4. Cap the logo's pixel dimensions?** Answered (owner, 2026-10-21, D47): no (L3,
  decision 17). The uploader is a settings admin harming only their own site; if wanted
  later, files gains `Upload.MaxPixels` checked from the image header and this spec passes
  4096.

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
9. Applied 2026-10-19 (D39) in api.md as a note on its still-pending Proposed change 15; the two points it raises (a per-request public-origin seam, and where `ETag`/`304` may be set under `no-store`) are settled by the amendment of 2026-10-19 (`history/settings.md`; approved, owner, 2026-10-20, D40): `no-store` stays with the `ETag`; `PublicOrigin` waits, shape fixed. **`plan/design/api.md` (row 16):** none now. Proposed change 15 (trusted origins and the
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

Proposed 2026-10-21 by the logo amendment; all six applied 2026-10-21 (D47):

14. Applied 2026-10-21 (D47), in app.md. **`plan/design/app.md` (row 18):** `app` passes `settings.Options.Files` as a lazy
    adapter `lazyFilesStore{a}` (every `files.Store` method resolves `a.files.Scoped("settings")`
    on first use; before files is built each answers `Unavailable`), the pattern of
    `lazyAuthorizer` (settings amendment 2026-10-19); the files part's `link` step no longer
    needs to hand settings anything (files Proposed change 4 said "a lazy `files.Store`
    scoped `settings`"; this is where it lives). `settingsRoutes` copies `MaxBodyBytes` and
    `Timeout` from `settings.Route` to `module.Route`. Failure mode: `TestLazyFilesStore_BeforeFiles_ReturnsUnavailable`.
    The e2e stack already runs the `disk` store, so the smoke run's logo round trip needs no
    profile.
15. Applied 2026-10-21 (D47), in web-shell.md. **`plan/design/web-shell.md` (row 17):** give the "M4 files additions (D44)" the detail
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
    `decoding="async"`, classes `max-h-8 w-auto max-w-[12rem]` and the link `min-w-11` (review correction, D47: a fixed height distorted a logo wider than 6:1; `max-h-8` keeps the aspect ratio and a short logo renders smaller; the home link has `min-w-11` so a square logo still gives a 44x44 target); the name as text when `logo` is
    `""` or on `onError` (a `503` on a copy without the bytes); no `loading="lazy"`. (d)
    `applyPublic` keeps `logo` in the public store (`usePublic().logo`) and the frame
    re-renders on a refetch. (e) The catalogue keys `field.too_large` ("The file is larger
    than {max}." (D52(e)); the UI formats `max` with `useFormat` bytes, so "2 MB"), `field.type` ("This
    kind of file isn't accepted."), `field.quota` ("The site has no room left for files."),
    `field.too_many`, `field.order` are still absent from `web/src/shell/messages/en.json`
    (only `field.malformed` exists): add them. (f) `Attachments` stays as described; the
    logo does not use it.
16. Applied 2026-10-21 (D47), in files.md. **`plan/design/files.md` (row 13):** two contract lines, no code change: under
    "Stage", "a `Body` read error that is already a classified `*errs.Error` is returned
    unchanged (settings' `MaxLogoBytes` reader relies on it)"; under "Method contracts",
    `ByRecord` "may be called inside a `Read` transaction (no audit, no store I/O; settings
    reads the logo in its snapshot load)". "Used by" names `settings` as the consumer of
    `LogoRecordKind`, `Brand`, `ReadUpload`, `Serve`.
17. Applied 2026-10-21 (D47), in 07-abstractions.md. **`plan/07-abstractions.md`, `settings` entry:** "Owns" gains "the regional's logo
    (`LogoRef`, `GET|PUT|DELETE /api/settings/logo`, `MaxLogoBytes`, `settings.logo.*`
    audit actions)"; "Doesn't do" keeps "storing the logo's bytes (`files`)" and adds "SVG
    sanitising (`files`, Later)".
18. Applied 2026-10-21 (D47), in 06-open-questions.md (recorded in parallel by plan-keeper). **`plan/06-open-questions.md`** (plan-keeper): Q-LOGO1 to Q-LOGO4, recorded as answered
    (owner, 2026-10-21) with the answers above; the decision log holds D47.
19. Applied 2026-10-21 (D47), in go-conventions skill. **`.claude/skills/go-conventions/SKILL.md`** (one phrase in the M4 files sentence):
    "a core part that caps an upload below `files.max_bytes` wraps `Upload.Body` in a
    counting reader that fails `Invalid file too_large {max}`; `Stage` returns it unchanged
    (settings' logo)". No module needs this today; the sentence stops a second
    implementation from adding a limit to files' interface instead.
