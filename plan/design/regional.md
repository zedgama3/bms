# Regional

- **Status:** Implemented (via boundaries.md, 2026-11-01)
- **Package:** `internal/core/regional` (service, `http.go`; the theme code is in `settings.go`)   **UI:** none of its own; the Regional form and the logo field stay in `web/src/core/settings/` (`settings.md`, UI), calling `/api/regional/...`
- **Depends on specs:** `settings` (`Reader`, `Decl`, `Area`; the revision and the terms), `files` (`Store`, `Brand`, `LogoRecordKind`, `ReadUpload`, `Serve`), `audit` (`Recorder`, `Ref`), `authz` (`Authorizer`, `ActorFunc`), `db`, `telemetry`, `lib-errs`, `lib-core`, `model`, `api` (route mounting), `module-contract`
- **Used by:** `notify` (the regional's name, public URL, language: `Profile`), the web shell (`GET /api/regional/public`, `theme.css`, `logo`), `api` (trusted origin, Later), `authz` (the regional's zone, Later), departments-events (the new-event form's default zone, through the public document)
- **Split from:** `settings.md` (2026-11-01, boundaries step 7, decision 4). The F-numbers and test names below are `settings.md`'s, unchanged; B9 and B10 are from `boundaries.md`.

## Summary

**Purpose.** Hold the regional's own public face: its name, contact mailbox, public URL, language, time zone and brand colours, the logo, and the public document, stylesheet and logo the shell boots from.

**Exported names (package `regional`).**
- `Settings []settings.Decl`: the seven `regional.*` keys (moved verbatim from `settings`' built-in area; app passes them as a core area `regional` like `accounts`).
- `Profile{Name, ContactEmail, PublicURL, Language, Timezone, ColorPrimary, ColorPrimaryDark string}` (replaces `settings.Regional`; the build's `Languages` list is no longer in it, it is in the public document).
- `Area` (const `"regional"`, the settings area name) and `Options.Validate() error` (exported; `New` calls it; `Invalid` for a nil dependency).
- `LogoRef audit.Ref` (a `var`, kind `files.LogoRecordKind`, ID `sha256("settings.logo")[:16]`, unchanged so deployed logos keep working) and `MaxLogoBytes int64 = 2 << 20`.
- `Options{Clock, Logger, Tracer, Audit audit.Recorder (scoped "regional"), Authz authz.Authorizer, Actor authz.ActorFunc, DB db.DB (ReadOnly() alone), Settings settings.Reader (scoped "regional"), Terms TermSource (required), Revision RevisionSource, Files files.Store (scoped "settings", required)}`; no `Meter` (A2 point 3); `New(Options) (*Service, error)`, no I/O, `Invalid` for a nil `Files`, `Settings`, `Terms`, `Revision`, `DB` or any dependency (B9).
- `TermSource` (`TermsFor(ctx, lang) (map[string]settings.Forms, error)`) and `RevisionSource` (`Revision(ctx) (settings.Revision, error)`; `Revise(ctx, fn func(ctx) (changed bool, err error)) (settings.Revision, error)`): both satisfied by `*settings.Service`.
- `(*Service).Profile(ctx) (Profile, error)`: reads the seven keys through `Settings`; no permission (system-level, like `Regional` before it).
- `(*Service).Public(ctx)`, `ThemeCSS(ctx)` (the public document and the stylesheet), `SetLogo(ctx, name, body)`, `RemoveLogo(ctx)`, `OpenLogo(ctx)`: as `settings.md` had them; see Interface.
- `(*Service).Routes() []httpx.Route` and `OpenAPI() []byte`: `GET /public`, `GET /theme.css`, `GET /logo` (public), `PUT /logo`, `DELETE /logo` (`settings.manage`) under `/api/regional/`.
- `Public`, `LogoResult` (types moved from `settings`). `Public`'s JSON field set (`name`, `contact_email`, `public_url`, `language`, `timezone`, `languages`, `theme`, `logo`, `terms`) is unchanged, pinned by `TestGetPublic_FieldSetIsPinned`; `Languages` is `settings.Languages`, a build constant.

**Key decisions.**
1. (boundaries decision 4) `regional` owns the regional's profile; `settings` is the typed store, its screens and terminology, and no longer imports `files`.
2. The `regional.*` keys, defaults, ranges, titles and the `regional.public_url` canonical form do not change; every key stays public (settings decision 6).
3. The logo moves with its behavior (settings decisions 16 to 18 and 17's limits: PNG, JPEG or WebP, 2 MiB, no SVG); the permission of `PUT` and `DELETE /logo` stays `settings.manage`.
4. The files owner of the logo stays `"settings"` (boundaries decision 12 of the staging: the bucket and `core.file.owner` of deployed logos keep working); `LogoRef` is unchanged.
5. Paths move from `/api/settings/{public,theme.css,logo}` to `/api/regional/...` (boundaries decisions 11 and 14, owner, 2026-10-31); audit actions become `regional.logo.changed` and `regional.logo.removed` (decision 13; a prefix change only; A2 point 4); old entries keep `settings.logo.changed|removed`.
6. `regional` imports `settings`, `files`, `audit`, `authz`, `db`, `telemetry`, `lib`, `model`; never `notify`, `identity` or `people`. `notify` reads `regional.Profile`; `regional` never reads `notify`.
7. (A2 point 1, owner, 2026-11-01) The logo's two writes run inside `settings.Service.Revise`, which holds the revision row lock and bumps the revision; the `ETag` of `GET /public` and `GET /theme.css` is the revision alone (`"regional-<revision>"`); `Public` reads the logo's file ID from a per-process cache keyed by the revision, refreshed with one `ByRecord` when the revision moved. `regional` depends on `settings`, never the reverse; `settings` does not import `files`.
8. (A2 point 2, owner, 2026-11-01) `Options.Terms` is required: a public document with empty term maps would silently drop a regional's renamed words, and an optional seam that changes a document's content hides a wiring mistake.

**errs kinds a caller must handle.** `Profile`: the Reader's kinds (`Invalid`, `Unavailable`, `Timeout`, `Canceled`). `SetLogo`: `Unauthenticated`, `Permission`, `Unsupported`, `Unavailable` wrapping `db.ErrReadOnly` (checked through `Options.DB` before the body is read), `Invalid` (field `file`: `required`, `too_large`, `type`, `malformed`), `Conflict` (`quota`), the store's and the recorder's kinds. `RemoveLogo`: the same minus the file codes. `OpenLogo`: `NotFound`, `Unavailable` (wrapping `objects.ErrMissing`). Handlers: `Invalid` wrapping `httpx.ErrUnsupportedMediaType`. `Public` and `ThemeCSS`: the revision read's kinds (F9 of `settings.md`) and, when the revision moved, the logo lookup's (`Unavailable`, `Timeout`, `Canceled`; F54).

**Owns.** Settings area `regional` (`name`, `contact_email`, `public_url`, `language`, `timezone`, `color_primary`, `color_primary_dark`; catalogue keys `core.regional.setting.<key>.*`); route set `regional` (above); the file record `LogoRef` (one file, level `public`, files bucket `settings`); audit actions `regional.logo.changed|removed`; OpenAPI operationIds `regionalPublic`, `regionalTheme`, `regionalLogo`, `regionalSetLogo`, `regionalRemoveLogo`, schemas `RegionalPublic`, `RegionalLogoUpload`, `RegionalLogoResult`, tag `regional` (A2 point 6; the five leave settings' fragment); spans `regional.logo.set|remove|open`; the constants above. No permissions of its own (uses `settings.manage`), no signals, no tables (the values are `core.setting` rows, the logo a `core.file` row).

**Expects of dependencies.** `settings`: scoped `Reader`; `Revision` (the `ETag` and the cache key); `Revise` (the revision lock and the bump around the logo writes; it records no audit entry and checks no permission, `regional` has checked its own); `TermsFor`; `core.setting` rows of area `regional` stay as they are; no longer imports `files`. `files`: the `Store` scoped `settings` (`Stage` outside the transaction, `Attach{Record: LogoRef, Level: public, Replace: true}` inside it, `ByRecord` usable inside a `Read` transaction, `Open`, `Serve`, `Brand`, `LogoRecordKind`). `app`: builds `regional` after `settings` and `files`, passes `regional.Settings` to `settings.New` as a core area, mounts the route set, passes `Profile` to `notify` (step 8). `web-shell`: reads the public document, theme and logo from `/api/regional/`. Both former open questions are settled (A2, below).

## Purpose

Hold the regional's own public face: its profile values, brand colours, logo and the public document the shell boots from.

## Responsibilities

- The seven `regional.*` declarations (`Settings`), their checks (name length, contact e-mail, the `regional.public_url` canonical form, language, time zone, colour format and contrast).
- `Profile`: the typed read of those seven keys for `notify`, `api` and `authz` (Later).
- The public document (`GET /public`): the profile, whether the theme is non-empty, the logo's file ID and the terms per language; its `ETag` (`"regional-<revision>"`).
- `theme.css`: the brand colours as a stylesheet.
- The regional's logo: its one file record (`LogoRef`), the three routes that upload, replace, remove and serve it, its cap and accepted kinds, its audit entries and its file ID in the public document. The bytes are files'.

## Not responsible for

- **Storing and validating values, the revision, the snapshot, sealing, the admin document and the screens:** `settings`. The Regional form and the logo field are screens of `web/src/core/settings/`.
- **Terminology** (the four forms per term): `settings` (`TermsFor`); the public document carries them (`Options.Terms`, decision 8).
- **Storing and serving the logo's bytes:** `files` (object store, `files.Brand`, the sniff, the metadata strip, `files.Serve`, the tombstone and the sweep). Sanitising an SVG, should one ever be accepted: a later `files` catalogue entry.
- **The header `<img>`, `upload()` and `ui/FileField`:** `web-shell`.
- **Sending e-mail with the regional's name or URL:** `notify` reads `Profile`.
- **Brand fonts** (web-shell decision 15, Later), a favicon, e-mail templates (`notify`).
- **Personal data:** nothing here is about a person; no `PersonData`, no export, no deletion.

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| Profile | The regional's identity, language, zone and colours. | `Settings`, `Profile` |
| Public document | What the shell reads before anyone signs in. | `Public`, `ThemeCSS`; `GET /public`, `GET /theme.css` |
| Logo | Keep one public image as the regional's logo, replaceable by a settings admin. | `LogoRef`, `SetLogo`, `RemoveLogo`, `OpenLogo`; bytes in `files` |

## Interface

```go
package regional // imports settings, files, audit, authz, db, telemetry, lib, model; never notify, identity, people

// Settings declares the regional.* keys (settings' built-in area, verbatim): name,
// contact_email, public_url, language, timezone, color_primary, color_primary_dark.
var Settings []settings.Decl

// Profile is settings.Regional today, with the two colours added and Languages dropped.
type Profile struct {
    Name, ContactEmail, PublicURL, Language, Timezone, ColorPrimary, ColorPrimaryDark string
}

// LogoRef and MaxLogoBytes move from settings unchanged; the files owner of the logo stays
// "settings" (the bucket and core.file.owner of deployed logos keep working).
var LogoRef audit.Ref
const MaxLogoBytes int64

type Options struct {
    Clock    clock.Clock
    Logger   *slog.Logger
    Tracer   telemetry.Tracer
    Audit    audit.Recorder  // scoped "regional"
    Authz    authz.Authorizer
    Actor    authz.ActorFunc
    DB       db.DB           // ReadOnly() alone: a logo write refuses a read-only copy before a byte of the body is read; Revise owns the transaction
    Settings settings.Reader // scoped "regional"
    Terms    TermSource      // settings (TermsFor); required
    Revision RevisionSource  // settings: Revision and Revise; *settings.Service
    Files    files.Store     // scoped "settings" (the logo); required
}

// TermSource is satisfied by *settings.Service.
type TermSource interface {
    TermsFor(ctx context.Context, lang string) (map[string]settings.Forms, error)
}

// RevisionSource is satisfied by *settings.Service.
type RevisionSource interface {
    Revision(ctx context.Context) (settings.Revision, error)
    Revise(ctx context.Context, fn func(ctx context.Context) (changed bool, err error)) (settings.Revision, error)
}

func New(o Options) (*Service, error)

// Profile reads the seven keys (settings' kinds). Consumer: notify (step 8), api and authz (Later).
func (s *Service) Profile(ctx context.Context) (Profile, error)
func (s *Service) Routes() []httpx.Route
func (s *Service) OpenAPI() []byte
```

`Public`, `ThemeCSS` (the public document and the stylesheet), `SetLogo`, `RemoveLogo` and `OpenLogo` keep `settings.md`'s rules and failure modes (below), with `Options.Files` now required, so the "no `Files`" rows (F40) and `Public.Logo == ""` with no store disappear in favour of `New` failing `Invalid` (B9). The audit actions are `regional.logo.changed|removed`. `Routes` returns `GET /public`, `GET /theme.css`, `GET /logo` (public), `PUT /logo`, `DELETE /logo` under `/api/regional`. The permission of `PUT` and `DELETE /logo` stays `settings.manage`.

Moved from `settings.md`, Interface (text unchanged except paths and action names):

```go
// Public is what GET /public returns.
type Public struct {
	Profile // was Regional; the document's JSON fields and their names must not change (pinned by `TestGetPublic_FieldSetIsPinned`)
	Languages []string // the build's languages, offered to everyone (was Regional.Languages)
	Theme bool                         `json:"theme"` // theme.css is not empty
	// Logo is the logo's file ID ("" when none), from the per-process cache keyed by the
	// revision (A2 point 1; settings decision 16 recorded the snapshot). The shell
	// requests /api/regional/logo?v=<Logo> so a replaced logo is a new URL. Public, like
	// every field here (decision 6).
	Logo string `json:"logo"`
	// Terms per language, flattened for the shell's {term_*} arguments:
	// {"en": {"term_department": "department", "term_department_plural": "departments",
	//         "term_department_title": "Department", "term_department_plural_title": "Departments", ...}}
	Terms map[string]map[string]string `json:"terms"`
}

// ---- The logo ----

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
	Revision settings.Revision `json:"revision"`
	Logo     string   `json:"logo"` // the file ID after the call; "" after a removal
}

// Public builds GET /public. No permission: every field is public by decision 6.
func (s *Service) Public(ctx context.Context) (Public, error)

// ThemeCSS renders the regional's colours as a stylesheet ("" when both are default).
func (s *Service) ThemeCSS(ctx context.Context) (string, error)

// SetLogo needs settings.manage. Outside any transaction it refuses a
// read-only copy (Options.DB), then stages body (named name, at most MaxLogoBytes, a files.Brand kind
// by its bytes) in files' bucket "settings"; then, inside settings.Revise (one Write transaction
// under the revision lock), attaches it to LogoRef replacing any earlier logo and records
// regional.logo.changed; Revise bumps the revision; after commit the next Public sees it.
// A staged object that was not attached is discarded. Consumer: PUT /logo.
func (s *Service) SetLogo(ctx context.Context, name string, body io.Reader) (LogoResult, error)

// RemoveLogo needs settings.manage. In one Write transaction under the revision lock it
// deletes LogoRef's files inside settings.Revise and records regional.logo.removed; Revise bumps
// the revision; with no logo it reports no change and answers the current revision. Consumer: DELETE /logo.
func (s *Service) RemoveLogo(ctx context.Context) (LogoResult, error)

// OpenLogo needs no permission (the logo is public). It lists LogoRef's files (the newest
// when more than one, which the revision lock prevents) and opens it; the caller serves
// with files.Serve and closes the Content. errs.NotFound when there is no logo. Consumer:
// GET /logo.
func (s *Service) OpenLogo(ctx context.Context) (files.File, files.Content, error)
```

Consumers: `Profile`: notify (name, public URL, language), api and authz (Later). `Public`, `ThemeCSS`: the two public handlers. `SetLogo`, `RemoveLogo`, `OpenLogo`: the three logo handlers only. `LogoRef`: this package, `filestest`-based tests, and `bms files verify`'s operator (the record kind it prints). Every use case checks its permission first (`Allow`), then validates, then writes. A write on a read-only copy fails `Unavailable` wrapping `db.ErrReadOnly` and changes nothing.

## Types

`Profile`, `Public`, `LogoResult` (above); uses `model.Email` (`ParseEmail` for `regional.contact_email`), `audit.Ref`, `files.File`, `files.Content`, `httpx.Route`.

## Data

No table of its own. The seven values are `core.setting` rows of area `regional` (settings stores them). The logo is one `core.file` row of kind `settings.logo` in files' bucket `settings`, level `public`, with no `Subject`: nothing here is personal data. `uploaded_by` is files' own column, handled by files' export and deletion (an admin's export lists the logo under `uploaded`, with its name; files decision 27).

## Permissions

| Permission | Allows | Scoped by | Reaches | Starter roles |
|---|---|---|---|---|
| `settings.manage` (declared by `settings`) | `PUT /logo`, `DELETE /logo` | site | internal | `settings-admin` |

`GET /public`, `GET /theme.css` and `GET /logo` are public: everything in them is the site's own appearance (settings decision 6); the logo is attached at level `public`, so `Open` audits nothing and needs no actor (files decision 11). `Profile` is not permission-checked: it reads the regional's own settings as the system, like a module's `Deps.Settings`.

## Signals

None published or subscribed (no signal on change; consumers read at use-case time).

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

**`regional.public_url` canonical form (owner, 2026-10-20, D40).** The submitted text is trimmed, then refused (`url`, with a `reason` param of `scheme`, `host`, `userinfo`, `query`, `fragment` or `chars`) unless: it parses with `net/url`; the scheme is `http` or `https` (any case); the host is non-empty (a port, if present, is 1..65535); there is no userinfo; the text contains no `?` or `#` (so no query, empty query or fragment); and it has no space or control character; it is longer than `MaxURLBytes` is `too_long`.
It is then stored as lowercase scheme, lowercase host (and port), the path as given, **with every trailing `/` removed** (so `https://x.example/` becomes `https://x.example` and a regional behind a sub-path keeps it: `https://x.example/bms`).
The same function is the canonical form `Profile().PublicURL` returns; notify builds links as `PublicURL + Link`, so a link can neither double a slash nor lose its host. A stored value that no longer passes (an upgrade) falls back to the default under decision 5. (Implementation, 2026-10-20:
NUL and DEL are refused first by the generic text check with `control_chars` and no `reason`; tab, newline and space reach this rule as `chars`.
A Unicode format character (category Cf, such as a zero-width space) or a bad percent-escape is `chars`; the canonical authority is taken from the submitted text and lowercased as a whole (percent-escape hex digits included), not from the unescaped host, so canonicalisation is a fixed point; any other parse failure, a port outside 1..65535 or a trailing colon is `host`.
`Profile().PublicURL` canonicalises on read as well as on save, and a stored value the rule refuses reads as unset with the decision-5 warning, logged once per process; not counted (A2 point 9a: `bms.settings.stale_values` no longer counts it). The canonical form is the `Normalize` of this declaration (`settings.Decl.Normalize`, A2 point 5), not a settings-private map.)

Constants that are not settings (pinned by `TestConstants_Pinned` in `settings` and here by `TestLogoRef_Pinned`): `MaxLogoBytes` 2 MiB (a header image is tens of kilobytes; a bound below files' per-file setting, not a choice a regional makes); `MinContrast` 4.5 (WCAG 2.2 AA for text, principle 4) stays in `settings`' constants and is read from there.

## UI

None of its own. The Regional form (name, contact mailbox, public URL, language, time zone, the two colours, the logo section) and the logo field's states are specified in `settings.md`, UI ("The logo field" and the `/settings` row); they call `PUT /api/regional/logo`, `DELETE /api/regional/logo` and read `GET /api/regional/public` (boundaries step 7: paths only). The header `<img src="/api/regional/logo?v=<Public.logo>">` is `web-shell`'s.

## Behavior

Moved from `settings.md`, Behavior (text unchanged except paths, action names and span names). "The revision lock" in the logo text below is settings' mechanism, reached only through `settings.Service.Revise` (A2 point 1); the "snapshot" the logo text once named is replaced by the per-process cache under "Why the logo bumps the revision".

**Public document.** `Public` = `Profile` and `Languages` + whether
`ThemeCSS` is non-empty + `Terms` for every language in `Languages` (defaults with overrides,
flattened to the shell's argument names: `term_<term>`, `term_<term>_plural`,
`term_<term>_title`, `term_<term>_plural_title`). `Terms` come from `Options.Terms` (`TermsFor` per language) and `Languages` from `settings.Languages`. `GET /public` and `GET /theme.css` read the revision first, then the body, so the body is at least as new as the tag; `ETag` is `"regional-<revision>"` (A2 point 9b), and they answer `304` to a matching `If-None-Match`; the api shell's `no-store` on
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

**The logo** (decisions 16 to 18 of `settings.md`). Three use cases over `Options.Files` (required here: a nil `Files` fails `New`, B9; in `settings.md` it was optional and answered `Unsupported`). Both writes run inside `Revise`; `RemoveLogo` and `SetLogo` check `settings.manage` and `DB.ReadOnly()` first, outside it.

*`SetLogo(ctx, name, body)`.* 1 `Allow(settings.manage)` (`Unauthenticated` with no actor,
`Permission` without it). 2 `Options.DB.ReadOnly()`: on a read-only copy, `Unavailable` wrapping
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
5 `Revise(fn)` (settings takes the revision row lock first; it serialises logo writes with each
other and with `Set` across instances, so two admins uploading at once end with one row: the second
`Replace` runs after the first commit and deletes the first file); fn runs `Files.Attach(ctx,
staged, files.Attachment{Record: LogoRef, Level: model.SensitivityPublic, Replace: true})`
(no `Subject`: the logo is about nobody; `Replace` deletes the previous logo's row and
writes its tombstone in this transaction; files records `files.file.created` and
`files.file.deleted` itself); `Audit.Record(regional.logo.changed, Record: LogoRef, Fields:
["logo"], Reason: "file:<new id>")` and reports `true`; `Revise` bumps the revision. A failure anywhere rolls back: no row,
no entry, no bump, and step 4 removes the object. 6 after commit: settings installs the bumped revision, the next `Public` sees it and re-reads the ID;
files' `AfterCommit` deletes the old object. Returns `LogoResult{Revision: the bumped revision, Logo}`.
`SetLogo` always changes something (a new file, a new ID), like a `Secret` re-save (F22a).

*`RemoveLogo(ctx)`.* 1 `Allow(settings.manage)`. 2 `s.db.ReadOnly()` as above. 3
`Revise(fn)`: fn runs `Files.DeleteByRecord(ctx, LogoRef)`; when it returns 0
fn reports `false` (no entry, no bump; the current revision is
returned with `Logo: ""`: idempotent, decision 18); otherwise it runs `Audit.Record(regional.logo.removed,
Record: LogoRef, Fields: ["logo"], Reason: "file:<old id>")` and reports `true`; after
commit the next `Public` has `logo` `""`.

*`OpenLogo(ctx)`.* No permission (public). Outside any transaction: `Files.ByRecord(ctx,
LogoRef)`; none is `NotFound`; the newest is opened with `Files.Open` (public: no actor
needed, no audit; files decisions 10 and 11); the handler calls `files.Serve(w, r, f, c)`
and closes `c`. `ByRecord`, never the cache, so a replica never serves a deleted ID during
its poll interval (decision 16). On a copy whose bytes have not been synced `Open` is
`Unavailable` wrapping `objects.ErrMissing` and the api answers `503` with `Retry-After`;
the header falls back to the name as text (web-shell).

*Handlers.* `PUT /logo`: check `settings.manage` first (D52(e)), then `files.ReadUpload(r, "file")` (`415` when not multipart; `400
file required` with no file part; text parts are ignored), then `SetLogo(ctx, "logo",
mp.Body)` (the stored name is always "logo", D52(e); `settings.manage` is checked before `ReadUpload` reads the body); `200` with `LogoResult`. `Route.MaxBodyBytes = 2*MaxLogoBytes` (the field error
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

*Why the logo bumps the revision (settings decision 16; A2 point 1).* `GET /public`'s `ETag` is the revision alone;
a logo change that did not bump the revision would answer `304` to the shell's refetch after
an upload and the header would keep the old image. Bumping the revision inside `Revise` makes the logo an
ordinary settings write: the writer's after-commit snapshot and a replica's poll both move the revision.
`Public` holds `(revision, logo ID)` in an `atomic.Pointer` and refreshes it with one `ByRecord` when the revision it read differs from the held one: one query per revision change per process, none while nothing changes, so the boot document stays a map lookup. The cache is correct because every logo change bumps the revision. A revision lower than the cached one (a database restore) drops the cache and looks the logo up again; a stored entry never moves the cached revision backwards. *Not taken:* a hidden `regional.logo` setting written through `Set` (settings has no "internal-only" key, and `Set` is one transaction that cannot carry files' `Attach`, so the row and the key could disagree); `ByRecord` on every `/public` with an in-process mutex (two uploads on two instances could leave two rows, F49; the boot document would do I/O on every boot).

**Read-only copy.** Reads and the public routes work. `GET /logo` works when the operator's sync has brought the object (files Q-FILES8), else `503` with `Retry-After` and the header shows the name; `PUT` and `DELETE /logo` fail `read_only` (`Options.DB.ReadOnly()`) before the body is read (decision 18 of `settings.md`), and the screen hides their controls on a read-only copy.

**Concurrency and lifecycle.** `*Service` is safe for concurrent use; no goroutine, nothing to `Close`. Idempotency: `RemoveLogo` with no logo is a no-op; `SetLogo` always changes something. Retries: none here; the shell retries `GET`s, never `PUT`s.

**Telemetry.** Spans `regional.logo.set`, `regional.logo.remove`, `regional.logo.open` with `regional.file_id` (`ID`) once known; never the file name or the declared type (`files.File.LogValue` is the ID). Logs never carry a setting's value or a file name.

## Failure modes

Rows moved from `settings.md` with their numbers and tests unchanged (the numbers F23, F36 to F54 are left unused there; F9 of `settings.md` also covers `Public`'s revision read). F40 changes: `Files` is required, so a nil `Files` is B9. F49 now runs through `Revise`; F54 is restated below. Audit action names in the rows are `regional.logo.changed|removed`.

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F23 | `Set` (`regional.*`) | name empty or too long; contact e-mail malformed; URL not http/https, without a host, with userinfo, query or fragment, or too long (the rule above); zone unknown or `Local`; colour not `#rrggbb`; colour under `MinContrast` | Invalid (`required`, `too_long`, `malformed`, `url {reason}`, `unknown`, `contrast {ratio, min}`) | show the field errors | `TestSet_Regional_Table` |
| F36 | `GET /public`, `/theme.css` | `If-None-Match` equals the current `ETag` | none: `304` | nothing | `TestGetPublic_IfNoneMatch_Returns304` |
| F37 | `PUT /logo` | the body is not `multipart/form-data`, or has no file part | Invalid wrapping `httpx.ErrUnsupportedMediaType` (`415`) / Invalid (`file`, `required`) | fix the client / "choose a file" | `TestPutLogo_NotMultipart_Returns415`, `TestPutLogo_NoFilePart_ReturnsInvalidRequired` |
| F38 | `SetLogo`, `RemoveLogo` | no actor | Unauthenticated | sign in | `TestSetLogo_NoActor_ReturnsUnauthenticated` |
| F39 | `SetLogo`, `RemoveLogo` | no `settings.manage` | Permission | show forbidden | `TestSetLogo_NoPermission_ReturnsPermission`, `TestRemoveLogo_NoPermission_ReturnsPermission` (D44's F38) |
| F40 | (retired) | was `Options.Files` nil: `Unsupported`, `TestLogo_NoFiles_ReturnsUnsupported`; `Files` is required, so this is B9 | | | `TestNew_NilFiles_ReturnsInvalid` |
| F41 | `SetLogo`, `RemoveLogo` | read-only copy | Unavailable wrapping `db.ErrReadOnly`, before any of the body is read (`SetLogo`) and before `Stage` | `503 read_only`; the screen keeps the chosen file | `TestSetLogo_ReadOnly_ReturnsUnavailableBeforeBodyRead` (the body reader records zero reads), `TestRemoveLogo_ReadOnly_ReturnsUnavailable` |
| F42 | `SetLogo` | more than `MaxLogoBytes` bytes in the file part | Invalid (`file`, `too_large {max: 2097152}`); the counting reader stops at the cap, nothing staged | show the limit: "The file is larger than {max}.", `max` formatted as bytes by the UI ("2 MB"; the `Params` stay `{max: 2097152}`, D52(e)) | `TestSetLogo_OverMaxLogoBytes_ReturnsInvalidTooLarge` (asserts the message text; D44's F37; the fake's `Staged()` is empty) |
| F43 | `SetLogo` | the sniffed kind is not in `files.Brand` (SVG, GIF, PDF, HTML named `.png`) | Invalid (`file`, `type`) | "this kind of file isn't accepted" | `TestSetLogo_UnacceptedType_ReturnsInvalidType` (table: an SVG, a GIF, a PDF, an HTML file) |
| F44 | `SetLogo` | the image cannot be parsed for stripping | Invalid (`file`, `malformed`) | "the file is damaged" | `TestSetLogo_Malformed_ReturnsInvalid` |
| F45 | `SetLogo` | the site is at `files.quota_bytes` (files decision 26, at `Stage` or `Attach`) | Conflict (`quota {max}`) | raise the quota or remove files | `TestSetLogo_AtQuota_ReturnsConflictQuota` (nothing attached, no bump, no entry) |
| F46 | `SetLogo` | the object store or the temp dir is unavailable | Unavailable (`503`, `Retry-After`); nothing written | retry later; operator | `TestSetLogo_StoreUnavailable_ReturnsUnavailable` |
| F47 | `SetLogo`, `RemoveLogo` | the audit `Record` fails inside the transaction | the recorder's kind; rollback; the staged object discarded | the use case fails | `TestSetLogo_AuditFails_RollsBackAndDiscards` (`Files()` unchanged, `Staged()` empty after), `TestRemoveLogo_AuditFails_RollsBack` |
| F48 | `SetLogo` | `Attach` fails for a files reason after `Stage` (`Invalid staged expired`, store kinds) | that error; rollback; discarded | the use case fails | `TestSetLogo_AttachFails_RollsBackAndDiscards` |
| F49 | `SetLogo` | two admins upload at once | none: serialised by the revision lock; one row remains (the later upload's), one `regional.logo.changed` each | nothing | `TestSetLogo_Concurrent_OneRowRemains` (integration, now through `Revise`) |
| F50 | `RemoveLogo` | no logo | none: no entry, no bump, `LogoResult{current revision, ""}` | nothing | `TestRemoveLogo_None_IsNoop` |
| F51 | `OpenLogo` | no logo | NotFound (`404 not_found`) | the header shows the name | `TestGetLogo_None_ReturnsNotFound` (D44's F39) |
| F52 | `OpenLogo` | the row exists and the object does not (a copy not yet synced) | Unavailable wrapping `objects.ErrMissing` (`503`, `Retry-After`) | the header falls back to the name; the operator syncs | `TestGetLogo_ObjectMissing_ReturnsUnavailable` |
| F53 | `GET /logo` | `If-None-Match` equals the file's `ETag` | none: `304` (`http.ServeContent`) | nothing | `TestGetLogo_IfNoneMatch_Returns304`; `TestGetLogo_SetsNoCacheControl` pins decision 25 |
| F54 | `Public` | the cache refresh's `ByRecord` fails | the store's kind (Unavailable, Timeout, Canceled) | fail the request; the shell retries | `TestPublic_LogoLookupFails_ReturnsUnavailable` |
| B9 | `New` | nil `Files`, `Settings`, `Terms`, `Revision`, `DB` or any dependency | Invalid | fail the build | `TestNew_NilFiles_ReturnsInvalid`, `TestNew_NilTerms_ReturnsInvalid` |
| B10 | `Profile` | a key fails to read | the Reader's kind | fail the request | `TestProfile_ReaderFails_KeepsTheKind` |

`Public`, `ThemeCSS`, `Profile` and `Routes`/`OpenAPI` cannot fail beyond the Reader's kinds and `Internal` on an unencodable document (a bug). `LogoRef` and `MaxLogoBytes` are pinned by `TestLogoRef_Pinned`.

## Lock-in check

Wraps nothing external. The logo's bytes go through `files.Store`, whose `disk` store is the self-hostable implementation.

## Test plan

- **Moved verbatim from `settings`:** `TestLogo_*`, `TestPublic_*`, `TestTheme_*`, `TestSetLogo_*`, `TestRemoveLogo_*`, `TestGetLogo_*`, `TestPutLogo_*`, `TestSet_Regional_Table` (the `regional.*` checks: URL canonical-form table, zone, colour, contrast with known pairs: `#9a3412` on white passes, `#f97316` fails), `TestLogoRef_Pinned`, `core_settings_logo_test` (integration) and the logo and theme e2e. Their fakes: `filestest.NewFake("settings")`, `authztest`, `audittest`, `dbtest.Fake`, `settingstest.NewFake("regional", regional.Settings)`.
- **New:** `TestNew_NilFiles_ReturnsInvalid`, `TestProfile_ReadsSevenKeys`, `TestProfile_ReaderFails_KeepsTheKind`, `TestRoutes_ArePublicWhereSettingsWere` (the three GETs are public, the two writes need `settings.manage`), `TestNew_NilTerms_ReturnsInvalid`, `TestGetPublic_FieldSetIsPinned`, `TestSetLogo_RecordsRegionalLogoChanged`, `TestRemoveLogo_RecordsRegionalLogoRemoved`, `TestPublic_LogoLookupFails_ReturnsUnavailable`. `TestPublic_ETag_FollowsTheRevisionAndTheLogo`, `TestPublic_ETagAndLogoMoveTogether` and `TestPublic_AfterLoad_DoesNotTouchTheFilesStore` keep their names with the cache semantics (one `ByRecord` per revision; the fake counts calls). The `Revise` tests are settings'.
- **Permission tests:** `PUT` and `DELETE /logo` denied without `settings.manage`, allowed with it, allowed for the superuser.
- **Leak test:** a marker as the uploaded file's name reaches no log, span or audit entry.
- **Catalogue tests (`internal/app`):** `core.regional.setting.<key>.title|description` for every key and `.option.<opt>` for `language`.
- **Web:** `LogoControls.test.tsx` and `publicDoc.test.ts` updated to the new paths; Playwright logo round trip and theme as before.

## Open questions

None. Both former questions were settled by `boundaries.md` amendment A2 (approved by the owner 2026-11-01): 1, the public document's terms and languages: decision 8 and `Options.Terms`; 2, how a logo change reaches the revision: decision 7 and `settings.Revise`.

## Proposed changes elsewhere

None beyond `boundaries.md`'s items 2, 9, 17, 18, 24 and 25 (A2's edits to `settings.md`, `files.md`, `api.md` and `07-abstractions.md` applied 2026-11-01) (the `settings`, `api`, `web-shell`, `07-abstractions.md` and `go-conventions` edits for this split) and item 16 (`notify` reads `regional.Profile`, step 8).
