# web-shell: the browser application

- **Status:** Implemented (2026-10-17); Approved (2026-10-17)
- **Amended 2026-10-28 (D52(d), (e), (f)):** (d) `ui/Attachments` keeps an empty `role="status"` live region mounted at all times and fills it when a file is ready, so "file is ready" is announced (a region inserted with its text is not); its Open button and the file link have distinct accessible names; tests assert that object URLs are revoked (on refetch and on unmount); the My volunteering announcement does not repeat after a cancel. (e) `field.too_large` reads "The file is larger than {max}." with `max` formatted as bytes by `useFormat`, e.g. "2 MB"; the duplication note becomes a decision: build `ui/PersonSearch` (a `search(q, signal)` prop) and shared `useAction`, `holds` and `useNames`, and replace the copies in `core/access` and `core/audit`. (f) Vitest config caps `maxWorkers` and raises `testTimeout`, so tests do not time out under load.
- **Amended 2026-10-17 (owner):** decisions 24 to 26 added (real error code sent in `code`; licence allowlist gains MIT-0 and Python-2.0 with a dev-only caniuse-lite exception; `web.FS()` hides dot-paths) and decision 27 (accepted implementation deviations); decision 6 amended so sign-in's own 401 does not redirect.
- **Amended 2026-10-18 (owner approval of audit/identity):** from M1 `/sign-in` redirects to `/accounts/sign-in` (keeping the query) and the sign-in screen, `shell/identity.ts` and the `shell.signin.*` keys move to `web/src/core/accounts/` (`core.accounts.*`); the session body and store gain `accountId`, `passwordChangeRequired`, `expiresAt`, `signupOpen`; a restricted session is sent to `/accounts/password?returnTo=` from every route outside `/accounts/`; the header shows `shell.account.anonymous` ("Account") when `playa_name` is empty; `web/src/core/audit/` is a core area folder with the M2 audit screen (identity.md Proposed change 2, audit.md Proposed change 14).
- **Amended 2026-10-18 (D37; departments-events Proposed change 7):** decision 3 and the Summary list `web/src/core/departments/` and `web/src/core/events/` as core areas from M3, with their nav entries; `useFormat` zone rendering is the documented way to show an event's times; the smoke run adds creating a department and an event and copying a module's set-up at the phone viewport (implementation work for the frontend implementer); the grant screens' department picker reads `GET /api/departments`.
- **Amended 2026-10-18 (owner approval of authz/people):** `session.permissions` holds `p` (site-wide) and `p@<department-id>`; `NavEntry.access: string[]` matches an entry when the session holds `p` exactly or any `p@...`; the M1 placeholder that showed every permissioned entry when the list was empty is removed (an empty list now means none); `web/src/core/access/` and `web/src/core/people/` are core areas (decision 3); after a session is established the frame calls `GET /api/people/me` once, applies `language` (mirrored to `localStorage["bms.lang"]`), sends a session with an empty `playa_name` (not the superuser) to `/people/me?first=1` once per session, and writes a signed-in person's language change with `PUT /api/people/me {language}`; `@/api` exports a `useMe()` hook limited to `core/people` and the frame; `@/ui` gains a date-only input helper; the smoke run adds "grant a role and see the menu change" and the two people flows, all at the phone viewport (authz.md Proposed change 7, people.md Proposed change 7).
- **Amended 2026-10-18 (M1 build, accepted deviations):** implementation deviations recorded in Behavior, "Accepted implementation deviations (M1 build, 2026-10-18)"; the Interface gains the `@/api` additions and `ErrorState`'s `message` prop; the restricted-session gate holds the person on `/accounts/password` only.
- **Amended 2026-10-18 (M2 build, accepted deviations):** implementation deviations recorded in Behavior, "Accepted implementation deviations (M2 build, 2026-10-18)": RouteAnnouncer, `@/api` additions, UI kit additions, known duplication; `web/src/core/audit/` built.
- **Amended 2026-10-20 (D44, owner approval of privacy-tags):** catalogue keys `privacy.audience.nobody|self|holders|signed_in` (title and one-line description each) are the fallback option titles for every `visibility_*` setting; a screen renders nothing for a field the response omits (an absent field is never a placeholder). Proposed change 9.
- **Amended 2026-10-21 (D46, owner approval of privacy-tags A2):** the UI rule gains "an absent `internal` scalar is its zero (a count shows 0, a flag is off); only a grouped field's absence means hidden"; a module's TypeScript response types mark every non-public field optional.
- **Amended 2026-10-20 (D44, owner approval of files):** Proposed change 7 (files.md) applied: `@/api` gains `upload(route, {file, fields}, opts)`; the UI kit gains `FileField` and `Attachments`; the header renders the logo from `Public.logo`; catalogue keys `field.too_large|type|quota|too_many|order|malformed`; the e2e smoke covers the logo round trip. Proposed change 8.
- **Amended 2026-10-21 (D47, owner approval of the settings logo amendment; settings.md Proposed change 15):** the "M4 files additions" gain their contract: `upload()` (`UPLOAD_TIMEOUT_MS`, never retried, `413` maps to `payload_too_large`), `FileField`'s props and client-side `maxBytes` check, the header `<img>`, `applyPublic` keeping `logo`, and the catalogue keys `field.too_large|type|quota|too_many|order`; `Attachments` unchanged. Audience label wording accepted (owner, 2026-10-21, D47): each `privacy.audience.*` description says "the person this information is about"; the `holders` title is "People whose role allows it".
- **Amended 2026-10-23 (D51, owner; build deviations, accepted):** `@/api` gains `download(route, opts)` (returns a `Blob` through the same trace, cookie and `ApiError` handling); `ui/Attachments` gets its callback contract (M4 files additions (g)); a paged list shows an error when a background refresh fails. files.md Proposed change 7 (Attachments) was already marked applied.
- **Amended 2026-10-19 (D39, owner approval of settings and notify):** Proposed change 7 applied (settings.md Proposed change 8): the frame fetches `GET /api/settings/public` at boot, `termsOf` merges the document's terms over the catalogue's `term.*` entries (now with plural and title forms), `/api/settings/theme.css` is linked, `field.contrast` joins the field codes, `/settings` joins the admin nav, and `web/src/core/settings/` and `web/src/core/notify/` are core area folders. The logo is not part of M4 (it comes with the `files` spec): the header shows the regional's name as text.
- **Package:** `web/` (pnpm workspace; `web/src/...`), plus the Go package `web`
  (`web/embed.go`, Go module `github.com/zedgama3/bms/web`) that embeds the built files for
  `app`. **UI:** this spec is the UI frame; module and core-area screens plug into it.
- **Depends on specs:** `api.md` (Approved): the problem body (`code`, `ref`, `fields[]`
  with `field`, `code`, `params`, `reason`), the codes table, `X-Read-Only`, `X-Trace-Id`,
  `GET /api/modules`, `GET /api/admin/modules`, `POST /api/client-errors`,
  `GET /api/openapi.json`, cookies and cross-origin protection (decisions 6, 7, 24), the
  CSP (`DefaultCSP`, decision 12), `Options.UI`, `Options.Version`. `telemetry.md`
  (Approved): `intake.Batch`, `Report`, `APICall`, the kinds, the limits, `Ref`
  (`XXXX-XXXX`), `WithParentFrom` (the `traceparent` form), the token grammar.
  `module-contract.md` (Approved): the module name grammar and reserved names, decision 8
  (message keys derived from names), decision 9 (the web registry is the source of truth
  for screens), the admin graph. `config.md` (Approved): `modules.disabled` (why a module
  may be compiled in and not enabled), `http.allow_plaintext` (development on a phone).
  `model.md` (Approved): the wire forms mirrored in TypeScript (UUID string, the four
  sensitivity names, `{start, end}`; event times rendered in the event's zone).
  `lib-errs.md`, `lib-core.md` (Implemented): the kind names that are `code`s; the
  `FieldError` codes `required`, `range`, `positive`, `one_of`, `url`, `host_port`,
  `malformed`. `db.md` (Approved): what read-only means. `signals.md` (Approved): the
  admin screen shows `publishes` and `subscribes`.
  **Used by:** `app.md` (row 18: `web.FS()`, `web.Version()`, `Options.UI`, the `bms
  openapi` command this spec asks for), `module-hello.md` (row 4b: the reference
  `web/src/modules/hello/`), every module spec with screens (`module-volunteers.md`, ...),
  `identity.md`, `authz.md`, `settings.md`, `people.md` (the endpoints the frame reads,
  listed under "Proposed changes elsewhere"), `frontend-implementer` and `web-reviewer`
  (the rules they work by), `docs/operating.md` (browsers, building the UI).

## Summary

**Purpose:** host every module's screens in one browser application (`web/`, React/TypeScript, pnpm), plus the Go package `web` (`web/embed.go`) that embeds the built files for `app`. This spec is the UI frame; module and core-area screens plug into it.

**Exported to screens (TypeScript; a module may import only `@/registry/types`, `@/api`, `@/i18n`, `@/ui`):**
- `ScreenSet{name, kind: "module"|"core", routes: RouteObject[] (relative paths, lazy elements, handle {title, access?}), nav: NavEntry[], messages: {lang: () => Promise<Messages>}}` (`en` required); `NavEntry{label: MessageKey, to, area: "main"|"admin", access?: "public"|"signed-in"|string[]}`. A set is exported as `ui` from `web/src/modules/<name>/index.ts` or `web/src/core/<area>/index.ts` and listed in the matching `index.ts` (D31).
- `api` typed client generated from `/api/openapi.json` (`openapi-typescript` + `openapi-fetch`, both outputs committed); `CallOptions{signal, params}`; `ApiError{status, code, ref, fields, method, route, module, traceId, retryAfter, reported}`; `ProblemCode` (mirrors api.md codes), `ClientCode` (`network`, `malformed_response`); `Problem`, `Field`, `Method`; wire types `ID`, `Sensitivity`, `TimeRange`; `configureClient({onUnauthenticated, onReadOnly, onFailure})` (frame only).
- i18n: `useT()`, `MessageKey` (generated typed keys), `isMessageKey(s)` (runtime guard for keys another module contributes), `useFormat()` (date/time with an event zone, number, relative, list), `useLanguage()`.
- Shared helpers (D52): `@/api` also exports `useAction(mapError?)` (returns `{run, busy, error, clear}`), `holdsSite`, `holdsAt`, `holdsAny`, `useNames(ids, fallback?)` and `searchPeople(q, signal)`; `@/ui` exports `PersonSearch` and `PersonMatch`.
- Error display: `messageFor`, `ErrorState` (optional `message` prop), `ErrorSummary`, `useFieldErrors` (returns `FieldMessage{text, reason?}`), `showsRef`.
- Reporter: `installReporter(...)`, `Reporter{apiFailure, screenCrash, flush, reportable}`.
- UI kit: `Button`, `Field`, `Dialog` (native `<dialog>`), `Banner`, `EmptyState`, `Loading`, `Table`, `Toast`, `PersonSearch`.
- Go `web`: `FS() (fs.FS, error)` (`errs.NotFound` when `dist/index.html` is missing; dot-prefixed paths hidden, D26), `Version() (string, error)` (`NotFound`, or `Invalid` unless a telemetry token of at most 64 bytes). Imports stdlib and `lib/errs`; only `app` imports it.

**Key decisions:**
- D1 modules mount under `/<name>/` in the browser; reserved names `api, assets, admin, account, sign-in, healthz, readyz, modules, core`.
- D2 an untranslated field code shows the server's English `reason` inside the translated `field.unknown` wrapper in `<span lang="en">` and is reported (owner 2026-10-17); a top-level unknown code shows `error.unknown`.
- D3 core areas register through the same `ScreenSet` (`kind: "core"`, always enabled); from M1 `web/src/core/accounts/` (sign-in, sign-up, password, account admin; identity.md) and from M2 `web/src/core/audit/` (the audit log; audit.md), `web/src/core/access/` (roles, grants; authz.md) and `web/src/core/people/` (profile, people, deletion requests; people.md), and from M3 `web/src/core/departments/` (departments; departments-events.md) and `web/src/core/events/` (events, kinds, schedule; departments-events.md). Nav entries: main area Departments (`/departments`, signed-in) and Events (`/events`, signed-in); admin area Event kinds (`/events/kinds`, `events.manage`). An event's times are shown with `useFormat` in the event's IANA zone, the documented way to render them; the access grant screens' department picker reads `GET /api/departments` (D37).
- D4 module isolation enforced by ESLint and `scripts/check-web-module-imports.sh` (under `make imports`); a literal `"/api/<other>/"` is refused.
- D5 client generated from the server's document, committed; `make openapi-check` regenerates via `bms openapi`; `fetch` only in `@/api` and the reporter.
- D6 every call: fresh `traceparent` (flags `01`), same-origin credentials, 35 s timeout, one `ApiError`, three hooks; `401` (except the session endpoint and sign-in's own) signs out and goes to `/sign-in?returnTo=`.
- D7 language = regional default then the person's explicit choice (`localStorage["bms.lang"]`, the only storage key); terminology via `{term_*}` arguments; FormatJS/ICU.
- D8 typed message keys, `en` reference, missing keys in other languages fall back to `en`; Go test `TestMessageCatalogues_EnglishCoversManifests` in `internal/app`.
- D9 the reference is shown for every error except `invalid`, `unauthenticated`, `read_only`, `network`.
- D10 lives inside `api.DefaultCSP`: no inline script or style, no `style` prop, Tailwind classes only, regional colours via same-origin `GET /theme.css`.
- D11 TanStack Query, memory only, `staleTime` 30 s, `gcTime` 5 min, cleared on sign-out and `401`; GETs retried twice, mutations never.
- D12 no component library (Tailwind v4, Lucide); D13 reporter installed before React and never throws.
- D14 only `network`, `malformed_response`, `internal`, `cross_origin`, `misdirected`, `method_not_allowed`, `payload_too_large`, `unsupported_media_type` (and untranslated codes) are reported as `api`.
- D15 no third-party request, system fonts; D16 admin modules screen is tables, not a graph.
- D17 Baseline widely-available browsers only, no "update your browser" page; D18 dark mode from `prefers-color-scheme` in M0; D19 logical CSS properties.
- D20 bundle version = binary version (`BMS_VERSION`, `dist/version.txt`, `__BMS_VERSION__`).
- D21 phone first is a hard requirement (owner 2026-10-17; 320 px, 44x44 px targets, touch only); D22 end-to-end Playwright smoke run in M0 at phone and desktop viewports.; D23 `isMessageKey(s)` exported from `@/i18n` (module-hello's request; additive).
- D24 the reporter sends the error body's real code in `api.code` (any telemetry token), never folded into `message` (owner 2026-10-17).
- D25 licence allowlist adds MIT-0 and Python-2.0; caniuse-lite (CC-BY-4.0, dev-only) is an explicit exception (owner 2026-10-17).
- D26 `web.FS()` hides dot-prefixed paths (owner 2026-10-17).
- M1 additions to `@/api`: `refreshSession`, `setSessionEnded` (ESLint limits both to `core/accounts`), `safeReturnTo`, `useReadOnly`, `useSessionCheckFailed`.
- D27 accepted deviations: react-router 8.4; the client resolves with the body; `useSession` and `Session` are exported from `@/api`, every other client/i18n/ui setter is frame-private (`internal.ts`) and lint-banned in `modules/**` and `core/**`; nav catalogues load at boot (report plus retry banner on failure); `ui` may `import type` from `@/api`; sets may not declare route loaders or actions; `axe-core` helper instead of `vitest-axe`; logical-property violations are lint errors; the isolation script is `scripts/check-web-module-imports.sh`.

**Errors/codes callers handle (client side, no Go errs except `web.*`):** `ApiError.code` is the problem body code or `network` (offline, DNS, reset, 35 s timeout) or `malformed_response` (non-2xx without problem body, or unparseable 2xx). `RegistryError` (F19-F21) for a bad name, absolute route path, untitled leaf or missing `en`. `web.FS`: `errs.NotFound` (app warns, `UI: nil`). `web.Version`: `NotFound`/`Invalid` (app warns).

**Owns (routes, keys, constants):** screens `/`, `/sign-in` (from M1 a redirect to `/accounts/sign-in`, keeping the query), `/admin/modules`, not-found, crashed, cannot-reach-server; keys `error.<code>`, `field.<code>`, `field.unknown`, `module.<name>.field.<code>`, `term.*`; constants (not settings): 35 s timeout, `UPLOAD_TIMEOUT_MS` 600 000 for `upload()` (D47), reporter 10 s timer / 100 pending / count cap 1000 / backoff 10 s to 5 min, 300 ms loading delay, bundle budget (entry 200 KB gzip JS, 30 KB CSS, lazy chunks 100 KB), `localStorage["bms.lang"]`. No Go settings or permissions; reads `core.modules.read` (api) and `session.permissions` only to hide UI. Defaults used until `settings` exists: regional name `BMS`, language `en`.

**Expects of dependencies:** api (problem body with `fields[].code/params/reason`, `X-Read-Only`, `X-Trace-Id`, `GET /api/modules` and `/api/admin/modules` shapes, `POST /api/client-errors`, CSP, `Options.UI`); telemetry (intake wire form and limits, `Ref`, `traceparent` form, token grammar for `screen` and `api.route`); module-contract (name grammar, message-key derivation, admin graph); identity (`GET /api/accounts/session`, `POST /api/accounts/sign-in|sign-out`; M0 answers 404 and the shell is signed out); authz (`permissions` in the session response); settings (`GET /api/settings/public`, `/theme.css`); lib-core (field codes `required, range, positive, one_of, url, host_port, malformed`).

**Pending proposed changes:** items 5 (identity endpoints) and 15 (`docs/operating.md`) wait on targets not yet written; item 7 applied 2026-10-19 (D39) except `language.detect_browser` and the brand font (Later); items 6 and 13 applied 2026-10-18; the rest are applied.

**M4 privacy additions (D44).** Catalogue keys `privacy.audience.nobody|self|holders|signed_in` (title and description) in the shell's `en` and `es` catalogues; (D47) each description says "the person this information is about" and the `holders` title is "People whose role allows it"; the rule "an absent field renders nothing". (D46) An absent `internal` scalar is its zero (a count shows 0, a flag is off); only a grouped field's absence means hidden. A module's TypeScript response types mark every non-public field optional.

**M4 files additions (D44; detail D47).** (a) `upload<T>(route, {file: File, fields?: Record<string, string>}, opts?: CallOptions & {method?: "PUT" | "POST"})` in `@/api`: builds a `FormData` with every field before the file, the file under the name `file`; `fetch` (inside `@/api`) with no `Content-Type` of its own (the browser sets the boundary); the same `traceparent`, cookie and `ApiError` handling as `api`; never retried (not idempotent); a timeout of `UPLOAD_TIMEOUT_MS` = 600 000 (files' `UploadTimeout`) instead of `TIMEOUT_MS`; `AbortSignal` from `opts`; a `413` maps to `ApiError` code `payload_too_large`. (b) `ui/FileField` props: `{label, name, hint?, error?, accept: string[] (MIME types), capture?: "environment" | "user", maxBytes?: number, value: File | null, onChange(file: File | null), disabled?}`; renders a visible label, hint and error like `Field` (the same `name` = API field path), an `<input type="file">` with `accept` joined, the chosen file's name and size (`useFormat`), a 44 px target; a file over `maxBytes` is refused client-side with the `field.too_large {max}` message and `onChange(null)`; a type outside `accept` (by the browser's `File.type`, a hint) is accepted and left to the server's sniff. (c) The header `<img>`: inside the home link, `src="/api/settings/logo?v=<Public.logo>"`, `alt` = the regional's name, `decoding="async"`, classes `max-h-8 w-auto max-w-[12rem]` and the link `min-w-11` (review correction, D47: a fixed height distorted a logo wider than 6:1; `max-h-8` keeps the aspect ratio and a short logo renders smaller; the home link has `min-w-11` so a square logo still gives a 44x44 target); the name as text when `logo` is `""` or on `onError` (a `503` on a copy without the bytes); no `loading="lazy"`. (d) `applyPublic` keeps `logo` in the public store (`usePublic().logo`) and the frame re-renders on a refetch. (e) The catalogue keys `field.too_large` ("The file is larger than {max}."; the UI formats `max` with `useFormat` bytes, e.g. "2 MB", D52(e)), `field.type` ("This kind of file isn't accepted."), `field.quota` ("The site has no room left for files."), `field.too_many`, `field.order` join `field.malformed` in `web/src/shell/messages/en.json`. (f) `Attachments` (File rows with download and a remove button the screen enables; a `503` with `Retry-After` on a download shows "not available on this copy yet") is detailed in (g); the logo does not use it. (g) **Added D51 (owner, 2026-10-23).** `download(route: string, opts?: CallOptions): Promise<Blob>` in `@/api`: a `GET` through the same `traceparent`, cookie and `ApiError` handling as `api` (a non-2xx answer becomes an `ApiError` with `status`, `code`, `ref` and `retryAfter`); the body is returned as a `Blob`; `AbortSignal` from `opts`. `ui/Attachments` takes `rows: AttachmentRow[]` with `AttachmentRow = {id: string, label: string, fetch(signal: AbortSignal): Promise<Blob>, onRemove?: () => void}`; the screen supplies `fetch` (normally a call to `download`) and the component owns the rest. After a successful `fetch` the row shows a tap-to-open link (an object URL, `target="_blank"`, `rel="noopener"`), moves focus to it and announces a "file is ready" status through an empty `role="status"` region that stays mounted (D52(d)); the Open button's name is "Open <label>" (the label is visually hidden text) and the file link's is "<label>: open in a new tab" (`shell.attachments.open_link`). A `404` shows the screen's own "gone" text, passed as `goneMessage?: string` (default `shell.attachments.gone`, "This file is no longer available."); a `503` with `Retry-After` shows "not available on this copy yet"; any other failure, including an unexpected non-`ApiError` throw, shows a message with a Retry button. Object URLs are revoked on a refetch and on unmount (a test asserts it, D52(d)). `onRemove`, when given, shows the remove button. (h) **Paged lists (D51).** A paged list whose background refresh fails (a refetch after the first page loaded) shows an error (`ErrorState` with the reference and a retry) instead of keeping stale rows silently.

**M2 build deviations (2026-10-18).** `RouteAnnouncer` moves focus only on a pathname change; `@/api` gains `useMe` and `useSetMe` and the client accepts multi-line NDJSON; the UI kit gains `ui/DateField`, `ui/Check` and a ref-accepting `Button`; `PersonPicker` is duplicated (candidate refactor noted); the audit screen is built in `web/src/core/audit/`.

**Amended 2026-10-28 (D52).** `Attachments` keeps a mounted empty `role="status"` region and gives its Open button and file link distinct names, with a test that object URLs are revoked; `field.too_large` reads "The file is larger than {max}."; `ui/PersonSearch`, `useAction`, `holds`, `useNames` replace the copies; Vitest sets `maxWorkers` "50%" and `testTimeout` and `hookTimeout` 15000 (`web/vite.config.ts`); the server's `too_large` field errors are shown with `max` formatted as bytes by `useFieldErrors`; the Attachments link is named "<label>: open in a new tab" and the Open button "Open <label>". Modules may import the new `@/api` and `@/ui` members.

## Purpose

Host every module's screens in one browser application.

## Responsibilities

- **The frame:** layout, landmarks, navigation built from the enabled modules and the
  person's sign-in state, the sign-in and home screens, the "this copy is read-only" and
  superuser banners, the language menu, the not-found and crashed-screen states.
- **The API client:** one typed way to call `/api/...`, generated from `/api/openapi.json`
  at build time, which sends `traceparent`, turns every problem body into one error type,
  watches `X-Read-Only`, and sends a person to sign-in on `401`.
- **The screen registry (D31):** a build-time list that imports one folder per module and
  one per core area, mounts each under `/<name>/`, and shows a module only when
  `GET /api/modules` says it is enabled.
- **Translation (D28):** every string a person sees comes from a message catalogue by key,
  in the regional's default language or the person's choice, with the regional's
  terminology substituted; the catalogues for modules follow module-contract decision 8.
- **Showing errors to people:** `code` and `fields[].code` translated, the reference shown
  where a maintainer can use it; the server's English `reason` appears only as the
  last-resort fallback for a field code with no translation, inside a translated wrapper
  (decision 2; owner, 2026-10-17).
- **Error reporting (D20):** catch crashes, unhandled rejections, crashed screens and the
  API failures that are the shell's or the deployment's, and send them in batches to
  `POST /api/client-errors` within telemetry's limits.
- **Accessibility and mobile:** WCAG 2.2 AA; **phones are a hard requirement** (owner,
  2026-10-17): many participants have only a phone, so every screen and every flow is
  designed for a small phone screen first and must be completable there, with touch
  targets, usable on a slow connection, as rules the lint, the tests (including an
  end-to-end run at a phone viewport) and the review enforce (decision 21).
- **Keeping nothing personal in the browser** beyond the page's memory.
- **Tooling:** the pinned, OSI-licensed toolchain, the checks `make check` runs for `web/`,
  and the Go embed package `app` serves.

## Not responsible for

| Concern | Owner |
|---|---|
| Any module's screens, their fields and flows | the module's spec (`module-<name>.md`, "UI"), built in `web/src/modules/<name>/` by the rules here |
| The core areas' screens (accounts, profile, people, departments, events, roles, settings, audit; `web/src/core/accounts/` from M1, `web/src/core/audit/` from M2) | each area's spec (`identity.md`, `people.md`, ...), built in `web/src/core/<area>/` with the same registry shape (decision 3) |
| Sessions, the cookie, sign-in and sign-out routes, password rules | `identity.md`. The shell renders the sign-in screen and calls its routes (Proposed change 5) |
| Deciding what a person may do | the server, on every call (09 ground rules). The shell hides what `session.permissions` says the person lacks (09 §4) and never relies on it |
| Serving the files, the CSP and other headers, caching, `index.html` for app paths | `api.md` decision 12. This spec builds what it serves and keeps to its CSP (decision 10) |
| The error body, the codes, the status table | `api.md`; mirrored here in TypeScript, never redefined |
| Validating, limiting, scrubbing and logging client reports | `telemetry.md` (`intake`). The shell sends what `intake` accepts and nothing it would scrub |
| The regional's name, logo, colours, default language, available languages, terminology | `settings.md` (row 15). Until it exists the shell ships defaults (decision 7, Proposed change 7) |
| A person's saved language | `people.md` (row 8), as a profile field read from `GET /api/people/me` (Proposed change 13, applied). Before M2: the browser's one stored key (decision 7) |
| Offline use, a service worker, cached data on the device | Q-OFFLINE (D27). Nothing here caches API data on disk (decision 11) |
| Configurable forms | D32: forms are code, fixed per module |
| Charts and reporting screens | a later extension point (D19) and the `dataviz` guidance when one exists |
| Native apps | out of scope (02-scope) |
| Build and release packaging of the binary with the UI inside it | `app.md` and `docs/operating.md` (Proposed changes 4 and 15) |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| Frame (`web/src/shell/`) | Put the person, the navigation, the banners and the current screen on one page. | Layout, `SkipLink`, `Nav`, `ReadOnlyBanner`, `SuperuserBanner`, `LanguageMenu`, `RouteAnnouncer`, `ErrorBoundary`; the shell's own screens: home, sign-in, admin modules, not found, crashed, cannot-reach-server. |
| API client (`web/src/api/`) | Call the server with types from its own document and one error shape. | `schema.d.ts` (generated), `client.ts`, `errors.ts` (`ApiError`, codes), `traceparent.ts`, `query.ts` (TanStack Query defaults). |
| Registry (`web/src/registry/`, `web/src/modules/index.ts`, `web/src/core/index.ts`) | Turn the compiled-in screen sets into routes and navigation for the enabled ones. | `ScreenSet`, `NavEntry`, `validateRegistry`, `enabledSets`. |
| Translation (`web/src/i18n/`) | Give every string a key, a language and the regional's terms. | FormatJS (ICU MessageFormat), `useT`, `useFormat`, `keys.d.ts` (generated), `lang.ts` (the one storage key), `terms.ts`. |
| Error display (`web/src/ui/errors/`) | Show a failed call to a person in their language, with the reference when it helps. | `ErrorState` (screens), `ErrorSummary` (forms), `useFieldErrors`, `messageFor(error)`. |
| Reporter (`web/src/shell/report.ts`) | Collect what broke in the browser and deliver it to intake within its limits. | Installed first in `main.tsx`; the only code besides the client that talks to the network. |
| Session and read-only stores (`web/src/shell/session.ts`, `readonly.ts`) | Hold who is signed in and whether this copy is read-only, for the frame and the client. | Plain stores read with `useSyncExternalStore`; memory only. |
| UI kit (`web/src/ui/`) | The few accessible building blocks every screen uses. | `Button`, `Field` (label, hint, error), `Dialog` (native `<dialog>`), `Banner`, `EmptyState`, `Loading`, `Table`, `Toast`; from M4 `FileField` and `Attachments` (D44). No third-party component library (decision 12). |
| Embed (`web/embed.go`) | Hand `app` the built files and their version. | `FS()`, `Version()`; stdlib and `lib/errs` only. |
| Tooling (`web/package.json`, `web/scripts/`, Makefile targets) | Build, check and generate, with pinned OSI-licensed dependencies only. | `pnpm check`; `make web-check`, `web-build`, `openapi-check`; the license allowlist; the bundle budget. |

## Interface

The shell's public surface is what a module's `web/src/modules/<name>/` may import:
`@/registry/types`, `@/api`, `@/i18n`, `@/ui`. Everything under `@/shell` is private to the
frame (decision 4). TypeScript signatures below; the one Go package is at the end.

```ts
// web/src/registry/types.ts — what a module or core area exports from its index.ts.

import type { RouteObject } from "react-router";
import type { MessageKey } from "@/i18n";

/** One module's or core area's screens. Exported as `ui` from web/src/modules/<name>/index.ts
 *  (or web/src/core/<area>/index.ts) and listed in the matching index.ts (D31). */
export interface ScreenSet {
  /** The module's name exactly as in its Go manifest ([a-z][a-z0-9]{1,31}), or the core
   *  area's name. Routes mount under `/<name>/`; a shell-reserved name is refused (F19). */
  readonly name: string;
  /** Core areas are always enabled; modules only when GET /api/modules lists them. */
  readonly kind: "module" | "core";
  /** Routes relative to `/<name>`. `path` is relative (no leading `/`, F20). Elements are
   *  lazy (`React.lazy`) so a module's code loads when first visited. A route's `handle`
   *  carries `{ title: MessageKey; access?: "public" | "signed-in" }` (default signed-in). */
  readonly routes: readonly RouteObject[];
  /** Entries for the frame's navigation. */
  readonly nav: readonly NavEntry[];
  /** Message catalogues by language tag, loaded on demand. `en` is required (F21). */
  readonly messages: Readonly<Record<string, () => Promise<Messages>>>;
}

export interface NavEntry {
  /** The label's message key (typed; a missing key is a compile error). */
  readonly label: MessageKey;
  /** Path relative to `/<name>`. */
  readonly to: string;
  /** `main`: the primary navigation. `admin`: the "Administration" group. */
  readonly area: "main" | "admin";
  /** Who sees it before the server decides: `public` (always), `signed-in` (default), or
   *  the permissions of which the person needs any one (from session.permissions): an
   *  entry matches when the session holds `p` exactly or any `p@<department-id>`; an empty
   *  list means none, so the entry is hidden (M2; the M1 placeholder that showed every
   *  permissioned entry is gone). */
  readonly access?: "public" | "signed-in" | readonly string[];
}

export type Messages = Readonly<Record<string, string>>;
```

```ts
// web/src/api/index.ts — the client every screen uses. No `fetch` anywhere else (decision 5).

import type { paths } from "./schema"; // generated from /api/openapi.json by openapi-typescript

/** Typed by the OpenAPI paths: api.GET("/api/hello/greeting"), api.POST(path, { body }).
 *  Resolves with the parsed body (undefined for 204); rejects with ApiError, never with
 *  anything else (F1–F11). Consumers: every screen, through TanStack Query hooks. */
export const api: Client<paths>;

/** Options accepted on every call. */
export interface CallOptions {
  /** The caller's cancellation; the client adds its own 35 s timeout (decision 6). */
  signal?: AbortSignal;
  /** Path and query parameters, typed from the document. */
  params?: { path?: Record<string, string>; query?: Record<string, string | number | boolean> };
}

/** Every failed call. `code` is the problem body's code, or one of the two client codes. */
export class ApiError extends Error {
  readonly status: number;            // HTTP status; 0 when no response arrived
  readonly code: ProblemCode | ClientCode;
  readonly ref?: string;              // telemetry.Ref "XXXX-XXXX", when the body had one
  readonly fields: readonly Field[];  // for code "invalid"; else empty
  readonly method: Method;
  readonly route: string;             // the route pattern called, e.g. "/api/people/{id}" (never the URL)
  readonly module: string;            // "hello" from "/api/hello/..."; "core" for the shell's routes
  readonly traceId: string;           // the traceparent trace ID the call carried
  readonly retryAfter?: number;       // seconds, from Retry-After
  /** Set once the reporter has queued this error, so a rethrow is not reported twice. */
  reported: boolean;
}

/** api.md "Codes", mirrored exactly; a test pins the list against web/openapi.json. */
export type ProblemCode =
  | "invalid" | "unauthenticated" | "permission" | "cross_origin" | "misdirected"
  | "not_found" | "method_not_allowed" | "conflict" | "payload_too_large"
  | "unsupported_media_type" | "rate_limited" | "internal" | "unsupported"
  | "unavailable" | "read_only" | "shutting_down" | "timeout";
/** Codes only the client produces. */
export type ClientCode =
  | "network"             // fetch rejected: offline, DNS, reset, or the client's 35 s timeout
  | "malformed_response"; // a non-2xx without a problem body, or a 2xx whose JSON does not parse

/** httpx.Problem and httpx.Field, mirrored. */
export interface Problem { status: number; code: string; ref?: string; fields?: Field[] }
export interface Field { field: string; code: string; params?: Record<string, string>; reason: string }
export type Method = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";

/** Wire forms from model.md, mirrored here (model.md "UI"). */
export type ID = string; // canonical UUID
export type Sensitivity = "public" | "internal" | "personal" | "sensitive";
export interface TimeRange { start: string; end: string } // RFC 3339 instants

/** Hooks the frame installs once (decision 6). Consumers: the frame; tests install fakes. */
export function configureClient(hooks: {
  onUnauthenticated(returnTo: string): void;   // the frame clears the session and navigates to sign-in
  onReadOnly(readOnly: boolean): void;          // from X-Read-Only on every response
  onFailure(error: ApiError): void;             // the reporter decides whether it is reportable
}): void;
```

```ts
// web/src/i18n/index.ts

/** Every key in web/src/shell/messages/en.json and every set's messages/en.json, as a
 *  union type generated by `pnpm generate:messages` (decision 8). */
export type MessageKey = keyof GeneratedKeys;

/** Runtime guard over the loaded catalogues: true when s is a key of the shell's or any
 *  registered set's `en` catalogue. For keys that arrive as data, such as extension-point
 *  contributions that carry another module's keys (`hello.banner`, later
 *  `people.person-page`), so a screen translates them without a cast (decision 23).
 *  Pure; never throws. Consumers: screens that render contributed keys. */
export function isMessageKey(s: string): s is MessageKey;

/** Returns the message for key in the active language, with values interpolated and the
 *  regional's terms available as {term_event}, {term_department}, ... (decision 7).
 *  Falls back to `en`, then to the key itself (F23, F24). Never throws. Consumers: every
 *  component; `messageFor` in error display. */
export function useT(): (key: MessageKey, values?: Record<string, string | number | Date | boolean>) => string;

/** Locale-aware formatting through Intl, in the active language. `date` and `time` take an
 *  IANA zone (an event's) and show the zone when it differs from the browser's. */
export function useFormat(): {
  date(instant: string | Date, zone?: string): string;
  time(instant: string | Date, zone?: string): string;
  dateTime(instant: string | Date, zone?: string): string;
  number(n: number): string;
  relative(instant: string | Date): string;
  list(items: string[]): string;
};

/** The active language and the ones offered (the shell's catalogue languages, restricted
 *  later by settings). `set` persists the choice (decision 7) and re-renders. */
export function useLanguage(): { current: string; offered: readonly string[]; set(tag: string): void };
```

```ts
// web/src/ui/errors/index.ts

/** The translated sentence for an error, by code: `error.<code>`, or `error.unknown` for a
 *  code the catalogue lacks (a problem body carries no `reason` at the top level, so there
 *  is nothing else to show; the code is reported, decision 2). Consumers: ErrorState,
 *  ErrorSummary, toasts. */
export function messageFor(t: ReturnType<typeof useT>, error: ApiError): string;

/** A screen's error state: the sentence, the reference line and copy button when
 *  `showsRef(error)` (decision 9), a retry button when `onRetry` is given; an optional `message` prop (M1 build). */
export function ErrorState(props: { error: ApiError; onRetry?: () => void; message?: string }): JSX.Element;

/** A form's error summary: `role="alert"`, receives focus when it appears, lists each field
 *  error as a link to its input (by the input's `name` equal to `fields[].field`), then the
 *  non-field message; shows the reference when `showsRef(error)`. */
export function ErrorSummary(props: { error: ApiError | null; formRef: React.RefObject<HTMLFormElement> }): JSX.Element | null;

/** Per-input messages: `fields[].code` translated through `module.<module>.field.<code>`,
 *  then `field.<code>`, with `params` as values. For a code with no translation (or an
 *  empty code) the message is the translated wrapper `field.unknown` with the server's
 *  English `reason` as its `{reason}` argument, rendered in a `<span lang="en">`; the code
 *  is reported once (decision 2). `reason` is rendered in no other case. Each entry is
 *  therefore a `FieldMessage`, not a bare string. */
export function useFieldErrors(error: ApiError | null): Readonly<Record<string, FieldMessage>>;

/** One input's message: `text` is the translated sentence; `reason` is set only in the
 *  `field.unknown` case and is rendered by `Field` as `<span lang="en">`. */
export interface FieldMessage { readonly text: string; readonly reason?: string }

/** True for every code except `invalid`, `unauthenticated`, `read_only` and the client
 *  code `network` (decision 9). */
export function showsRef(error: ApiError): boolean;
```

```ts
// web/src/shell/report.ts — the reporter (D20, telemetry.md Proposed change 4).

/** Installs the window listeners and the flush timer. Called once, first thing in
 *  main.tsx, before React renders (decision 13). Idempotent. */
export function installReporter(opts: { appVersion: string; now?: () => Date; send?: Sender }): Reporter;

export interface Reporter {
  /** From the client's onFailure hook: queues an `api` report when reportable(error). */
  apiFailure(error: ApiError): void;
  /** From an ErrorBoundary: queues a `screen` report with the matched route pattern. */
  screenCrash(error: unknown, screen: string): void;
  /** Sends what is pending now (pagehide, tests). Never throws. */
  flush(reason: "timer" | "pagehide" | "manual"): void;
  /** Which failures are the shell's or the deployment's business (decision 14). */
  reportable(error: ApiError): boolean;
}
```

```go
// Package web embeds the built web shell for app (api.Options.UI). It imports the
// standard library and lib/errs only; only app imports it (layer rules, Proposed change 8).
package web

import "io/fs"

// FS returns the built shell rooted at dist/ (index.html at the root, hashed files under
// assets/), for api.Options.UI. It fails with errs.NotFound when dist/ holds no index.html
// (a checkout where `pnpm build` has not run): app then starts without a UI, logs one
// warning, and non-API paths answer 404 (api decision 12). Cannot fail otherwise.
// Consumer: app (`serve`).
func FS() (fs.FS, error)

// Version returns the bundle's version, written by the build to dist/version.txt (the
// same string Vite injects as __BMS_VERSION__ and the shell sends as app_version). It
// fails with errs.NotFound when the file is missing and errs.Invalid when its content is
// not a telemetry token of at most 64 bytes (F31). Consumer: app, which warns once at
// start when it differs from the binary's version (Proposed change 4).
func Version() (string, error)
```

### Who needs each member

| Member | Consumer(s) | Pre-condition | Post-condition |
|---|---|---|---|
| `ScreenSet`, `NavEntry`, `Messages` | every module and core area (`index.ts`); the registry; `module-hello` | — | one shape for screens, mirrored on the Go manifest |
| `api`, `CallOptions` | every screen (through TanStack Query), the frame (`/api/modules`, session, admin modules) | the generated schema matches the server (Proposed change 4) | typed calls; `ApiError` on failure |
| `ApiError`, `ProblemCode`, `ClientCode`, `Problem`, `Field` | error display; the reporter; tests | — | one error shape |
| `ID`, `Sensitivity`, `TimeRange` | module and core screens | — | model.md's wire forms in one place |
| `configureClient` | the frame (once); tests | — | the three hooks installed |
| `useT`, `MessageKey`, `useFormat`, `useLanguage` | every component; the frame's language menu | a catalogue loaded | translated text; formatted values |
| `isMessageKey` | screens that render keys contributed by another module (`module-hello`'s banner) | — | narrows a string to `MessageKey` |
| `messageFor`, `ErrorState`, `ErrorSummary`, `useFieldErrors`, `showsRef` | every screen with a query or a form | — | errors shown the same way everywhere |
| `useAction(mapError?)` | every screen with a mutation (`core/*`, `modules/volunteers` through its `withRangeCodes` wrapper) | `mapError` is a stable function | `run(fn)` sets busy, clears the last error, keeps an `ApiError`, resolves true on success; returns `{run, busy, error, clear}`; nothing is cached |
| `holdsSite`, `holdsAt`, `holdsAny` | screens deciding what to show from the session's grants | a loaded `Session` | a boolean; the server still decides |
| `useNames(ids, fallback?)`, `searchPeople(q, signal)` | screens showing or picking people (`core/access`, `core/audit`, `modules/volunteers`) | signed in | names from `GET /api/people/names` (at most 200 ids; `fallback` or the ID until they arrive); query keys `["names", …]` and `["people-search", …]` are shared |
| `PersonSearch`, `PersonMatch` | the grant, audit and volunteer screens | the screen passes `search(q, signal)` (normally `searchPeople`); the kit makes no request of its own | a chosen person's ID and name; the typed term stays in the component; messages `shell.person_search.*` |
| `installReporter`, `Reporter` | `main.tsx`; the client's `onFailure`; `ErrorBoundary`; tests | — | reports reach intake within its limits |
| `web.FS`, `web.Version` | `app` | `pnpm build` ran | the files `api` serves; the version to compare |

## Types

Defined here: `ScreenSet`, `NavEntry`, `Messages`, `ApiError`, `ProblemCode`, `ClientCode`,
`Problem`, `Field`, `Method`, `ID`, `Sensitivity`, `TimeRange`, `MessageKey`, `FieldMessage`,
`Reporter`, plus the internal `Session` and `ReadOnlyState`:

```ts
// web/src/shell/session.ts (private to the frame; screens read it through useSession())
export type Session =
  | { signedIn: false; signupOpen: boolean }
  | { signedIn: true; personId: ID; accountId: ID; playaName: string; superuser: boolean; passwordChangeRequired: boolean; expiresAt: Date; signupOpen: boolean; permissions: readonly string[] };
// Filled from GET /api/accounts/session (identity.md, Proposed change 5; permissions from
// authz.md, Proposed change 6). signupOpen is also sent when signed out (identity.md Proposed change 2). In M0 the route does not exist and the shell is signed out (F28).

export type ReadOnlyState = { readOnly: boolean; since?: Date };

// M1 additions exported from `@/api` (web/src/api/index.ts):
export function refreshSession(): Promise<void>;   // re-reads GET /api/accounts/session; ESLint: core/accounts only
export function setSessionEnded(ended: boolean): void; // set when a 401 ended the session, so sign-in can say so once; ESLint: core/accounts only
export function safeReturnTo(raw: string | null): string; // a same-origin path, else "/"
export function useReadOnly(): ReadOnlyState;
export function useSessionCheckFailed(): boolean;  // the boot session check failed (not "signed out")
```

Wire types a module needs come from `schema.d.ts` (`components["schemas"]["VolunteersShift"]`),
never hand-written: a hand-written type drifts from the server; the generated one fails
`openapi-check` when it drifts (decision 5).

## Data

No tables. What the browser holds, where, at what sensitivity (privacy-data), and for how
long. The rule: **personal data lives in the page's memory only and leaves with it.**

| What | Where | Level | Why | Lifetime |
|---|---|---|---|---|
| `Session` (person ID, playa name, superuser flag, permission names) | memory (the session store) | internal | the frame's header and navigation; route gating before the server's own check | until sign-out, a `401`, or the tab closes |
| Query cache: whatever the current screens fetched (up to `sensitive`, e.g. an incident) | memory (TanStack Query, no persister) | the record's own level | screens render from it; back navigation does not refetch | `gcTime` 5 minutes after the last screen using it unmounts; cleared entirely on sign-out and on `401` (decision 11) |
| Pending error reports (`message`, `stack`, route patterns, `app_version`, counts) | memory (the reporter's queue, at most 100) | internal; patterns and messages, never values | D20 | until sent (≤ 10 s) or the page is hidden |
| Language tag | `localStorage["bms.lang"]`: the one key the shell writes (decision 7) | not personal (a BCP 47 tag) | a person's choice must survive sign-out and reload | until changed |
| The session cookie | the browser's cookie jar, `HttpOnly`: unreadable by this code | — | `identity.md` | identity's |
| URLs (history) | the browser | — | module screens put IDs in paths and filters in query strings, never names, e-mail addresses or free text (privacy-data: "never in URLs"); a module spec that needs a text search uses `POST` or keeps the query in memory | the browser's |

Never written by the shell: `sessionStorage`, `indexedDB`, the Cache API, a service worker,
cookies, `document.title` with personal data (titles are screen names: "Shift details",
never the person's name), `console` output in production builds, any third-party request
(no analytics, no CDN, no web fonts; decision 15). Form inputs holding `personal` or
`sensitive` text carry `autocomplete="off"` (sign-in's username and password keep the
browser's password-manager autocomplete, which is the person's own choice). No draft of a
form is kept across navigation unless a module spec says so and where.

## Permissions

The shell decides nothing; the server checks every call (09 ground rules). What it reads
from the session to show or hide:

| Operation | Permission | Scoped by | Default roles | Notes |
|---|---|---|---|---|
| `GET /api/modules` at boot | none (public, api decision 27) | — | anyone | names and enablement only |
| `GET /api/accounts/session` at boot and after sign-in/out | none (public; `{signed_in:false}` is a `200`) | — | anyone | Proposed change 5 |
| `GET /api/admin/modules` (admin "modules" screen) | `core.modules.read` (api) | site | every site-admin starter role | the entry shows when `session.permissions` has it (M2+; `p` exactly or any `p@...`); the screen's forbidden state handles a `403` |
| `POST /api/client-errors` | none (public, telemetry decision 9) | — | anyone | the reporter, within the limits |
| Showing a `NavEntry` or mounting a route | `access` on the entry or route: `public`, `signed-in`, or any of the named permissions | — | — | a hidden entry is a convenience; the module's service still decides |
| Any module call | the module's own | — | — | the shell passes the cookie and nothing else |

Superuser (D23): the shell shows the banner when `session.superuser` is true; it grants
nothing and hides nothing for it.

## Signals

None. The shell is a client; it learns of changes by calling again.

## Settings

None of its own in M0. Everything a regional would do differently is data the shell reads,
with the defaults below until `settings.md` provides the endpoint (Proposed change 7):

| Value | Source when `settings` exists | Default now | Used for |
|---|---|---|---|
| Regional name | `GET /api/settings/public` | `BMS` | the header, `document.title` suffix, the home greeting |
| Logo | `Public.logo` (the file ID) gives `/api/settings/logo?v=<logo>`, a same-origin URL | none (the name as text) | the header; the regional's name is its `alt` |
| Colours | `GET /theme.css` (same-origin stylesheet, decision 10) | the shipped palette | the whole frame; settings validates contrast (Proposed change 7) |
| Default language | `GET /api/settings/public` | `en` | the language before a person chooses (decision 7) |
| Offered languages | same | every language the shell's catalogue has | the language menu |
| Terminology (`event`, `department`, `theme camp`, `participant`, ...) per language | same | the catalogue's `term.*` entries | `{term_*}` arguments in every message (decision 7) |

Constants inside the shell, pinned by tests, with the reason each is not a setting:

| Constant | Value | Why not configurable |
|---|---|---|
| client call timeout | 35 s | `http.request_timeout` defaults to 30 s; the client gives up just after the server would (decision 6) |
| query `staleTime` / `gcTime` | 30 s / 5 min | freshness is per screen; the cap bounds what memory holds (decision 11) |
| GET retries | 2, at 1 s then 3 s, or `Retry-After` when longer | enough for a flap, not enough to hide an outage |
| reporter flush interval; pending cap; dedup counter cap | 10 s; 100; 1000 | telemetry Proposed change 4 and `intake.MaxCount` |
| reporter backoff after a network failure | 10 s doubling to 5 min | telemetry: never flood |
| loading indicator delay | 300 ms | a spinner that flashes is worse than none |
| bundle budget | shell entry ≤ 200 KB gzip JS and ≤ 30 KB CSS; each lazy chunk ≤ 100 KB | "usable on a slow connection" (principle 4) |
| minimum touch target | 44 × 44 CSS px | the project's rule (web-reviewer), above WCAG 2.2's 24 px |
| smallest supported width | 320 CSS px | the project's rule |

## UI

The shell's own screens. Every screen has a title (`document.title = "<title> · <regional
name>"`), an `h1`, and handles loading, empty, error, forbidden and success where each
applies. Wording is the `en` catalogue's; it is given here so the translator and the
reviewer see the same text.

| Route | Who | Content | States |
|---|---|---|---|
| frame (every page) | everyone | skip link ("Skip to content") → `<header>` (logo or name, `<nav aria-label="Main">` from enabled sets' `main` entries, the language menu, the person's playa name with a "Sign out" button or a "Sign in" link) → banners → `<main id="content" tabIndex={-1}>` → `<footer>` (regional name, the bundle version as `<small>`). On a phone the navigation is a disclosure button ("Menu", `aria-expanded`) that opens a full-height panel; on wide screens a horizontal bar. | read-only banner (`role="status"`): "This copy is read-only. You can look things up, but nothing can be saved right now."; superuser banner (`role="status"`): "You are signed in as the break-glass superuser. Every action is recorded."; session-check banner (F29) |
| `/` home | everyone | signed out: "Welcome to {regional}" with a sign-in button and the public nav entries as cards; signed in: "Hi, {playa_name}" and a card per `main` entry the person may see | empty (no enabled sets with entries): "Nothing is turned on yet. An administrator can enable modules." |
| `/sign-in` | signed out | the form `identity.md` defines (e-mail address, password), `autocomplete` on both, errors through `ErrorSummary`; `returnTo` (a same-origin path, validated: begins with `/`, not `//`) followed after success | M0: "Sign-in isn't available in this build yet." (F28); `401` → "That e-mail address or password isn't right." (never which); `429` → `error.rate_limited`; read-only: sign-in still works (09 ground rules) |
| `/admin/modules` | signed in with `core.modules.read` | version; a table per module: title (`module.<name>.title`), enabled, schema (built, applied, "up to date" or "needs `bms migrate`"), permissions, settings keys, publishes, subscribes, provides, requires, uses, defines, contributes to; warnings as a list (the server's English `message`, operator text by module-contract decision 8, marked `lang="en"`); edges as a table "from → to (kind, via)". No drawn graph in M0: a table is accessible and enough (decision 16) | loading; forbidden (`403`): "You don't have access to this screen." with ref; error with ref and retry; empty: "No modules are compiled into this build." |
| `*` not found | everyone | "There's nothing at this address." with a link home; also what a disabled module's path shows (decision 3) | — |
| crashed screen (an `errorElement`) | everyone | "Something went wrong on this screen." with "Try again" (re-renders the route) and "Go home"; if the cause is an `ApiError` with a ref, the reference line | — |
| cannot reach the server (boot, F27) | everyone | "Can't reach the server. Check your connection and try again." with retry; or, for a server error, "Something went wrong." with the reference line and retry | — |
| reference line (telemetry.md "UI") | — | "If you ask for help, quote the reference **1A2B-3C4D**." with a "Copy reference" button (`navigator.clipboard`; falls back to a selectable `<code>`) | — |

Error messages by code (`error.<code>` in `web/src/shell/messages/en.json`):

| code | English |
|---|---|
| `invalid` | "Some of the information isn't right. Check the fields below." |
| `unauthenticated` | "Your session ended. Sign in again." (shown as a toast on the sign-in page after the redirect) |
| `permission` | "You don't have permission to do that." |
| `not_found` | "We couldn't find that. It may have been removed, or you may not have access to it." |
| `conflict` | "That change clashes with another one. Reload and try again." |
| `rate_limited` | "Too many attempts. Wait {seconds, plural, one {# second} other {# seconds}} and try again." |
| `read_only` | "This copy is read-only right now, so that can't be saved." |
| `unavailable`, `timeout`, `shutting_down` | "The server isn't available right now. Try again in a moment." |
| `unsupported` | "That feature isn't turned on here." |
| `internal`, `payload_too_large`, `unsupported_media_type`, `method_not_allowed`, `cross_origin`, `misdirected`, `malformed_response`, unknown | "Something went wrong." |
| `network` | "Can't reach the server. Check your connection and try again." |

Field messages (`field.<code>`): `required` "Required."; `range` "Must be between {min} and
{max}."; `positive` "Must be more than zero."; `one_of` "Must be one of: {options}."; `url`
"Must be a web address."; `host_port` "Must be a host and port, like host:5432.";
`malformed` "This isn't in the expected form."; `unknown` "This value isn't valid:
<en>{reason}</en>" (the wrapper for a code with no translation; `<en>` renders
`<span lang="en">` around the server's English `reason`, decision 2). A module adds its own
under `module.<name>.field.<code>` (for example `module.volunteers.field.overlap`).

## Behavior

### Decisions settled here, with reasons

1. **Modules mount under `/<name>/` in the browser, as under `/api/<name>/` on the
   server.** Two modules can then never claim the same path, a disabled module's whole
   subtree disappears at once, the reporter's `screen` pattern names the module, and a
   person reading a URL knows which module they are in. The shell keeps `/`, `/sign-in`,
   `/admin/...`, `/account/...` and the asset paths (`/assets/`, `/api/`, `/healthz`,
   `/readyz`) as reserved names (F19). A module's admin screens also live under its own
   prefix (`/volunteers/positions`), grouped in the "Administration" navigation by
   `NavEntry.area`; `/admin/` holds only the shell's screens.
2. **A field code with no translation shows the server's English `reason` inside a
   translated wrapper, and the code is reported** (owner, 2026-10-17; keeps api.md
   decision 25). The draft proposed never rendering `reason`; the owner reversed it: a
   true English sentence is more use to a volunteer at a gate than a generic "this value
   isn't valid", and English as the last resort inside a wrapper in the person's language
   is accepted under D28 (the clarification is proposed to plan-keeper in this spec's
   report). So the lookup is `module.<module>.field.<code>` → `field.<code>` →
   `field.unknown` with `{reason}` as its argument, the reason rendered in a `<span
   lang="en">` so a screen reader switches voice; `reason` is rendered in no other case
   (a known code's translation wins even when it is less specific). The shell still
   reports the untranslated code as an `api` report ("untranslated field code `<code>`",
   decision 14) so it gets a translation. A top-level `code` the catalogue lacks has no
   `reason` on the wire (`httpx.Problem` carries none), so it shows `error.unknown`
   ("Something went wrong.") and is reported the same way. In a development build the
   client also logs the whole problem to the console.
3. **Core areas register through the same `ScreenSet` as modules, in `web/src/core/`,
   always enabled (from M3 also `core/departments` and `core/events`).** The alternative, writing the accounts, people and settings screens
   into the frame, would make the frame the largest package and give core specs no place of
   their own. With one shape, `identity.md`'s sign-in screen is `core/accounts`, people's
   screens are `core/people`, and the frame stays the thing that hosts them. `kind: "core"`
   skips the `GET /api/modules` check because a core area is not a module (api decision 15
   lists modules only).
4. **Module isolation is enforced by lint and by a script, not by convention.** D9 in the
   browser: `web/src/modules/<a>/**` may import `@/api`, `@/i18n`, `@/ui`,
   `@/registry/types` and itself; never `@/modules/<b>`, `@/core/*` or `@/shell/*`. The
   same for `web/src/core/<a>/**`. The frame imports `@/modules/index.ts` and
   `@/core/index.ts` (the lists) and no folder directly. A string literal `"/api/<b>/"`
   inside `modules/<a>` is refused too: calling another module's API is importing it by
   another name; what a module needs from another goes through a core contract or an
   extension point whose screens the owning module renders (D19). ESLint
   (`no-restricted-imports` with per-folder overrides) catches it in the editor;
   `scripts/check-web-module-imports.sh` (grep, no Node needed) runs under `make imports`
   so the Go and web rules fail the same target.
5. **The client is generated from the server's document and committed.** `openapi-typescript`
   (MIT) turns `web/openapi.json` into `schema.d.ts` (types only, no runtime); `openapi-fetch`
   (MIT, ~6 KB, no dependencies) gives typed `GET`/`POST` over `fetch`, and the shell's
   `client.ts` wraps it with middleware for `traceparent`, timeouts, `X-Read-Only`, problem
   parsing and the hooks. Both files are committed: a reviewer sees an API change as a
   diff, the web build needs no Go toolchain, and `make openapi-check` (regenerate from
   `bms openapi`, Proposed change 4, and `git diff --exit-code`) proves they match the
   binary. A hand-written typed client was rejected (the path-typing is the hard part and
   is exactly what the library does); a heavier generator with runtime validation was
   rejected (the server validates; the browser would validate twice and ship a schema
   runtime). `fetch` is allowed only in `web/src/api/**` and the reporter (lint).
6. **Every call: a fresh `traceparent`, same-origin credentials, a 35 s timeout, one
   `ApiError` shape, three hooks.** `traceparent` is `00-<32 hex>-<16 hex>-01` from
   `crypto.getRandomValues`, kept on the `ApiError` as `traceId` so a report's `trace_id`
   leads to the server's spans (telemetry decision 6; the server ignores the flags).
   `credentials: "same-origin"` because the cookie is `SameSite=Lax` and the API has one
   origin (api decision 6); no `Authorization` header exists. The client's own
   `AbortController` fires at 35 s and is combined with the caller's signal by hand (no
   `AbortSignal.any`, decision 17). The response's `X-Read-Only` goes to `onReadOnly` on
   every response, present or not, so the banner appears on the next call and clears on
   the first response after the copy is writable again (api decision 16). A `401` on any
   route but the session endpoint and sign-in's own (a wrong password is the form's error,
   not a lost session; amended 2026-10-17) calls `onUnauthenticated(returnTo)`: the frame clears the
   session and the query cache and navigates to `/sign-in?returnTo=<path>`; the error is
   still thrown so the screen's state settles. Every failure calls `onFailure`, and the
   reporter decides (decision 14). No automatic retry lives in the client: TanStack Query
   retries `GET`s (decision 11) and never mutations.
7. **Language: the regional's default, then the person's explicit choice; the browser's
   language list is not consulted** (owner, 2026-10-17: regional default for now; browser
   detection is a later feature behind a `language.detect_browser` setting, Proposed
   change 7). D28 makes the default language a per-regional setting,
   which a per-browser guess would silently override; a regional that serves a Spanish-
   speaking community chooses Spanish for everyone, and anyone else switches once. The
   choice is kept in `localStorage["bms.lang"]` (a language tag is not personal data and
   must survive sign-out and the sign-in page), mirrored to the profile when `people`
   exists (Proposed change 13). `<html lang>` follows it. Offered languages are those the
   shell's catalogue has (`en` in M0; `es` next, D28). **Terminology** (what a regional
   calls an event or a department) reaches every message as arguments `{term_event}`,
   `{term_department}`, ... merged into the values of every `t()` call, from the catalogue's
   `term.*` entries until `settings` supplies them per language (Proposed change 7).
   FormatJS (`react-intl`, BSD-3) with ICU MessageFormat was chosen over a lighter library
   because ICU is what translators and translation tools already speak (plurals, selects,
   dates through `Intl`), and the 30 KB it costs fits the budget; Lingui was the runner-up.
8. **Message keys are typed; `en` is the reference; a module's manifest-derived keys are
   checked by a Go test.** `pnpm generate:messages` reads every `messages/en.json` and
   writes `web/src/i18n/keys.d.ts`, so `t("shell.nope")` fails `tsc` and no screen can
   ship with a missing English string. Other languages may be incomplete: a key missing in
   `es` falls back to `en` (F23) and `pnpm check` prints the missing keys per language as a
   warning, not a failure, so a translation can land in parts. The keys module-contract
   decision 8 derives from a manifest (`module.<name>.title`, `.permission.<rest>.title`,
   ...) are the one set the TypeScript side cannot know; `internal/app` imports every
   module, so a Go test there (`TestMessageCatalogues_EnglishCoversManifests`) walks every
   registered manifest and checks `web/src/modules/<name>/messages/en.json` (Proposed
   changes 3 and 4). The catalogue files are flat JSON objects (`{"key": "ICU message"}`),
   one per language per set: the format every translation tool imports.
9. **The reference is shown for every error but `invalid`, `unauthenticated`, `read_only`
   and `network`.** telemetry.md "UI" says every 5xx and every 4xx but validation. Three
   refinements: `read_only` is an expected state with its own banner and message, and
   "quote this reference" would send a volunteer to ask for help about a copy that is
   behaving correctly; `unauthenticated` ends in the sign-in page, where a reference
   helps nobody; `network` has no reference because no server answered. Everything else,
   including `not_found` and `permission`, shows it: those are where a person asks "why
   can't I see this?" and a maintainer wants the trace. Proposed change 2 records it.
10. **The shell lives inside `api.DefaultCSP` and asks for no relaxation.** `default-src
    'self'` with no `'unsafe-inline'` means no inline `<script>` (Vite's production output
    has none), no `<style>` elements and **no `style=` attributes** (a CSP without
    `'unsafe-inline'` in `style-src` blocks those too). So: Tailwind classes only, the
    `style` prop and `dangerouslySetInnerHTML` are lint errors, and a regional's colours
    arrive as a same-origin stylesheet `GET /theme.css` that `settings` serves (Proposed
    change 7) rather than as inline custom properties. If a later screen needs a computed
    inline style (a chart), its spec proposes `style-src-attr 'unsafe-inline'` to api.md
    then, with the reason. `img-src 'self' data:` covers the logo (same origin) and
    inline SVG icons. COOP and CORP `same-origin` mean no cross-origin embeds or popups,
    which the shell has no use for.
11. **TanStack Query holds every server read, in memory only, cleared on sign-out.** Every
    screen has loading, empty, error and success states; one library gives them one shape
    (`isPending`, `error`, `data`), retries `GET`s on `network`, `unavailable`, `timeout`
    and `shutting_down` (twice, honouring `Retry-After`), refetches on window focus (a
    phone that comes back from a pocket shows fresh shifts) and never persists. No
    `persistQueryClient`, no `IndexedDB`: a cached incident on a shared device is a leak
    (privacy-data: "never in the browser's storage"). `queryClient.clear()` runs on
    sign-out and on `401`. Offline caching, when Q-OFFLINE is designed, is a separate spec
    with its own privacy answer.
12. **No component library; a small UI kit over native elements and Tailwind.** Accessible
    primitives are few here (button, field, dialog, banner, table, toast), native
    `<dialog>` gives focus trapping and return for free, and a library brings its own
    styling system, bundle weight and upgrade churn. Tailwind v4 (MIT) because utility
    classes keep styles in the component and need no `<style>` tag (decision 10). Icons
    from Lucide (ISC), tree-shaken, always `aria-hidden` with a text label beside or a
    visually hidden one.
13. **The reporter is installed before React and never throws.** A crash during boot is the
    kind a maintainer never hears about. `installReporter` runs first in `main.tsx`, adds
    `error` and `unhandledrejection` listeners, and every path inside it is wrapped so a
    bug in the reporter cannot take the page down or recurse. An `ApiError` the client
    already reported carries `reported = true`, so the same failure thrown up to a boundary
    or a rejection handler is not counted twice.
14. **Only failures that are the shell's or the deployment's are reported as `api`.** The
    server already logs every error response with its reference (api decision 4), so a
    browser report of a `404` or a `403 permission` would duplicate a log line and spend
    the intake budget (30 per address per minute). Reported: `network` (no server log
    exists), `malformed_response` (a proxy page or a contract mismatch), `internal` (the
    maintainer wants the screen, version and browser), and the deployment or shell bugs
    `cross_origin`, `misdirected`, `method_not_allowed`, `payload_too_large`,
    `unsupported_media_type`. Not reported: `invalid`, `unauthenticated`, `permission`,
    `not_found`, `conflict`, `rate_limited`, `unsupported`, `unavailable`, `timeout`,
    `read_only`, `shutting_down`. Plus the one special case of decision 2 (an untranslated
    `code` or `fields[].code`, reported as `api` with the code in the message, once per
    page per code). `/api/client-errors`'s own failures are never reported (telemetry
    Proposed change 4).
15. **No third-party request, ever.** No analytics, no CDN, no web fonts (a system font
    stack; owner, 2026-10-17: system fonts for the MVP, a self-hosted brand font is a
    later `settings` feature, Proposed change 7), no map tiles, no error-tracking SaaS:
    D20 routes browser errors through bms,
    privacy-data forbids analytics, and principle 6 forbids a dependency a regional cannot
    self-host. `connect-src 'self'` in the CSP makes this a runtime guarantee, and
    `TestBundle_NoExternalURLs` greps the built assets for `https://` origins other than
    documentation links.
16. **The admin "modules" screen is tables, not a drawn graph.** A diagram needs an
    accessible equivalent anyway, and the edges list is that equivalent; M0 ships the
    tables and a module author reads provides/uses as rows. A drawing can come later as an
    enhancement that keeps the tables.
17. **Browser support is Baseline "widely available" at the time of the build, and nothing
    newer.** Phones at an event are not new; Vite's `baseline-widely-available` target
    (features shipped in every major engine for at least 30 months) is a rule a reviewer
    can check with one lookup and avoids both a legacy-browser plugin (inline scripts,
    CSP) and APIs only one browser has. `AbortSignal.any`, for example, is replaced by ten
    lines. `docs/operating.md` states it as "browsers from the last two and a half years"
    (Proposed change 15). Confirmed by the owner (2026-10-17): only modern browsers. There
    is **no "update your browser" page**: detecting a failed module script needs an inline
    or second script and a second code path for the sake of browsers the project does not
    support; `index.html` carries only a `<noscript>` line ("This application needs
    JavaScript.") in English, the one string outside the catalogue because no catalogue
    can load without a script.
18. **Dark mode follows `prefers-color-scheme` from M0** (owner, 2026-10-17). Screens are
    read at night on the playa at full brightness; a dark palette is one Tailwind variant
    and a second set of tokens, cheap now and expensive to retrofit. No toggle in M0 (the
    OS setting is the toggle); both palettes meet AA contrast.
19. **Logical properties, not left/right.** Spanish is left-to-right, but a right-to-left
    language later would otherwise mean touching every margin. Tailwind's `ms-`/`me-`/
    `ps-`/`pe-`/`start`/`end` utilities are used; `ml-`/`mr-`/`pl-`/`pr-`/`left-`/`right-`
    are a lint error (a custom ESLint rule over class strings), so the habit forms now.
20. **The bundle version is the binary's version.** `make web-build` runs `pnpm build` with
    `BMS_VERSION` set to the same string `-ldflags -X` gives the binary (`git describe`,
    or `dev`); Vite injects it as `__BMS_VERSION__`, the reporter sends it as `app_version`,
    the footer shows it, the build writes `dist/version.txt`, and `app` warns once when
    `web.Version()` and the binary's version differ (a stale `dist/` in a developer's tree).
21. **A phone is the primary device, not a smaller desktop** (owner, 2026-10-17: a hard
    requirement; many people only have phones). Every screen is laid out for a narrow
    touch screen first and widened from there, never the reverse; every flow (sign in,
    find a shift, file a report, read a person's emergency contact) must be completable
    on a small phone with touch alone. The concrete rules are under "Accessibility and
    mobile rules"; the Playwright smoke run executes every scenario at a phone viewport as
    well as a desktop one (test plan), so a layout that only works wide cannot pass M0;
    and `frontend-implementer` builds and `web-reviewer` checks at the phone width first
    (Proposed changes 11 and 14).
22. **An end-to-end smoke run is part of M0** (owner, 2026-10-17). M0's "done when" lists
    behaviours (the frame, the sign-in page, the read-only banner) that only a browser
    against the real binary can prove; the run is the scenarios in the test plan and
    nothing more, so it stays fast enough to run on every change.
23. **`isMessageKey` is exported from `@/i18n`** (requested by `module-hello`; settled
    2026-10-17 as a small additive export). Extension-point contributions carry another
    module's message keys, which are dynamic by nature; without a runtime guard a screen
    cannot translate them without a cast. The guard checks the loaded `en` catalogues, and
    a key it rejects is shown as the key itself (F23's fallback), never thrown on.
24. **The reporter sends the error body's real code in `api.code`** (owner, 2026-10-17;
    telemetry decision 21). Intake accepts any token there now, so the shell no longer
    folds the code into `message` to get past a validator that knew only `errs.Kind`
    names. A code that is not a token is sent as "".
25. **Licence allowlist: MIT-0 and Python-2.0 are added; caniuse-lite is a dev-only
    exception** (owner, 2026-10-17). MIT-0 and Python-2.0 are OSI-approved. caniuse-lite
    (CC-BY-4.0) is data read by the build tooling (browserslist), never shipped in
    `dist/`; `check-licenses.mjs` lists it by name as the single exception, and a test
    pins that nothing outside the allowlist or that name passes.
26. **`web.FS()` hides dot-prefixed paths** (owner, 2026-10-17), so `dist/.vite/manifest.json`
    and `.gitkeep` are never served; `FS()` otherwise returns `fs.Sub(dist)`.
27. **Accepted implementation deviations** (reviewed, 2026-10-17). The tooling table says
    react-router 8.4 (data mode), not 7. The `api` client resolves with the parsed body.
    `useSession` and `Session` are exported from `@/api`; every other client, i18n and ui
    setter (`configureClient`, language and read-only setters) lives in a frame-private
    `internal.ts` that ESLint bans in `modules/**` and `core/**`. Navigation catalogues
    load at boot; a failure is reported and shows a banner with retry. `@/ui` may
    `import type` from `@/api`. A `ScreenSet` may not declare route loaders or actions
    (the registry refuses them; F38). The accessibility helper wraps `axe-core` directly
    instead of `vitest-axe`. Logical-property violations are lint errors, not warnings
    (decision 19). The isolation script is `scripts/check-web-module-imports.sh`.

### Boot, step by step (`main.tsx`, `app.tsx`)

1. `installReporter({ appVersion: __BMS_VERSION__ })` (decision 13). Read the language
   (`lang.ts`: the stored tag if offered, else `en` until settings exists; F25, F26). Set
   `<html lang>`.
2. `validateRegistry([...coreSets, ...moduleSets])` (F19–F21). In a production build a
   failure renders the crashed-screen state with the message; it cannot reach production
   because `registry.test.ts` imports the real lists.
3. `configureClient({ onUnauthenticated, onReadOnly, onFailure: reporter.apiFailure })`.
4. Render `<App/>`: the `QueryClientProvider`, the `IntlProvider` (shell catalogue for the
   language, loaded; sets' catalogues load when their routes mount), the stores' providers,
   the router.
5. `App` issues, in parallel, `GET /api/modules` and `GET /api/accounts/session` (and
   later `GET /api/settings/public`). Until both settle: the frame's chrome with a
   `Loading` region (text after 300 ms, `aria-live="polite"`), no navigation yet. If
   `/api/modules` fails: the cannot-reach-server state with retry (F27). If the session
   call fails: F28/F29. The navigation catalogues of the enabled sets load at boot too;
   if one fails to load, the failure is reported and a banner with retry shows (F39).
6. `enabledSets = sets.filter(s => s.kind === "core" || enabled.has(s.name))`. Build the
   router: the shell's routes, then for each enabled set a parent route `/<name>` with the
   set's children, each wrapped in an `ErrorBoundary` (the set's `errorElement`) and a
   gate that redirects to `/sign-in?returnTo=` when `handle.access` is not `public` and
   the session is signed out; then `*` not found.
7. On every route change: `document.title`, focus to `<h1>` (or `#content`), the
   `RouteAnnouncer` live region reads the title, the reporter's current `screen` pattern is
   updated (`:id` → `{id}`).
8. After sign-in or sign-out: refetch `/api/modules` and the session; `queryClient.clear()`
   on sign-out; navigate to `returnTo` or `/`.

### A call, step by step (`client.ts`)

1. Build the URL from the typed path and `params` (path values URL-encoded; query from
   `params.query` only, never from free text the caller formats). Derive `route` (the
   pattern) and `module` (the second path segment, or `core`).
2. Headers: `traceparent`, `Accept: application/json, application/problem+json`,
   `Content-Type: application/json` when there is a body. Serialize the body with
   `JSON.stringify`. `credentials: "same-origin"`.
3. Start the 35 s timer; abort on it or the caller's signal.
4. `fetch`. A rejection → `ApiError{status: 0, code: "network"}` (F1, F2); the caller's own
   abort rethrows a `DOMException` `AbortError` unchanged (not an error, not reported).
5. `onReadOnly(response.headers.get("X-Read-Only") === "true")`.
6. `2xx`: `204` → `undefined`; else parse JSON; a parse failure → `malformed_response` (F5).
7. Otherwise: if `Content-Type` is `application/problem+json` parse `Problem` → `ApiError`
   with `status`, `code`, `ref`, `fields`, `retryAfter` from `Retry-After` (F3); else
   `malformed_response` with the status (F4). `401` and `route !== "/api/accounts/session"`
   → `onUnauthenticated(location.pathname + location.search)` (F6). `onFailure(error)`.
   In a development build, `console.debug` the whole problem (decision 2). Throw.

### Showing an error, step by step

- **A screen's query fails:** render `<ErrorState error onRetry>` inside the screen's layout
  (the frame stays; the person can navigate away). `messageFor` picks `error.<code>`;
  `showsRef` adds the reference line (decision 9). `not_found` on a record route is this
  same state with the `not_found` sentence.
- **A form's mutation fails:** `ErrorSummary` renders above the form with `role="alert"`
  and `tabIndex={-1}`, receives focus, and lists one link per field error (`href="#<input
  id>"`), then the non-field message. Each input whose `name` equals `fields[].field` gets
  `aria-invalid="true"` and `aria-describedby` pointing at its message from
  `useFieldErrors` (lookup `module.<module>.field.<code>` → `field.<code>` →
  `field.unknown` with `{reason}` in a `<span lang="en">`, `params` as values; an empty
  `code` is treated as unknown; the unknown code is reported once, decision 2). A field
  with no matching input appears in the summary only. `read_only` shows `error.read_only` in
  the summary and the banner is already up. `unauthenticated` is handled by the redirect
  before the summary renders.
- **A crash:** the route's `ErrorBoundary` renders the crashed-screen state and calls
  `reporter.screenCrash(error, screen)`.
- **Toasts** (`role="status"`, auto-dismiss after 8 s, never the only place an error is
  shown): "Your session ended" after a `401` redirect; "Saved" after a mutation when the
  screen navigates away.

### The reporter, step by step (`report.ts`)

1. **Sources.** `window` `error` → kind `crash` (`event.message`, `event.error?.stack`);
   `unhandledrejection` → kind `rejection` (skip an `ApiError` with `reported`; else
   `String(reason)` and `reason.stack`); `screenCrash` → kind `screen`; `apiFailure` →
   kind `api` when `reportable(error)` (decision 14), then `error.reported = true`.
2. **Shape.** `message` and `stack` with every `?query` stripped and cut at 512 and 4096
   bytes (the server cuts again); `screen` = the current matched route pattern with
   `:param` rewritten `{param}` (or omitted before the router mounted); `occurred_at` =
   now (ISO); `app_version`; for `api`: `api: { method, route, status, code }` (`code` is the error body's real code, a token
   per telemetry decision 21, or ""; never put in `message`, decision 24),
   `error_ref`, `trace_id`. Never the URL, a query string, a body, a form value or a header.
   A test pins that every `screen` and `route` the shell can produce matches telemetry's
   token grammar (F17).
3. **Dedup and queue.** Key `(kind, message, screen)`; a repeat increments `count` (cap
   1000). At most 100 distinct pending; the oldest is dropped beyond that (F16). The first
   queued report starts a 10 s timer; there is no timer while nothing is pending.
4. **Flush.** At most 20 reports per batch, oldest first. `timer`/`manual`:
   `fetch("/api/client-errors", { method: "POST", keepalive: true, body })` through
   `fetch` directly (not the client: the client's `onFailure` would loop); `pagehide` and
   `visibilitychange` → `hidden`: `navigator.sendBeacon(url, new Blob([body],
   { type: "application/json" }))`, falling back to the keepalive `fetch` when it returns
   `false`. `202` → remove the sent reports. `429` → keep them, pause for `Retry-After`
   (default 60 s) (F13). `400` → drop that batch (a shell bug; `console.warn` in development)
   (F14). Network failure → keep, back off 10 s doubling to 5 min (F15). Nothing about the
   intake call is ever reported (decision 14) and every exception inside is caught (F18).

### The registry, step by step (`registry.ts`)

1. Names: each `name` matches `[a-z][a-z0-9]{1,31}`, is unique across core and module
   sets, and is not reserved (`api`, `assets`, `admin`, `account`, `sign-in`, `healthz`,
   `readyz`, `modules`, `core`) (F19).
2. Routes: every `path` is relative and contains no `..` segment; `handle.title` is present
   on every leaf (F20); no route declares a `loader` or `action` (F38, decision 27).
3. Catalogues: `messages.en` exists (F21); other languages are whatever the set ships.
4. Navigation: `to` is relative; `access` well-formed. (A missing `label` key is a type
   error, F22.)
5. The checked list is frozen and handed to `App`.

Adding a module's UI (add-module §2): create `web/src/modules/<name>/index.ts` exporting
`ui: ScreenSet`, `messages/en.json`, `screens/`; add one line to
`web/src/modules/index.ts`. The `hello` module is the reference (Proposed change 10).

### Accessibility and mobile rules (WCAG 2.2 AA; phones are a hard requirement)

These are requirements on every screen built in `web/`, checked by `eslint-plugin-jsx-a11y`,
by `axe-core` in component tests and in the Playwright smoke run (which executes at a phone
viewport and a desktop one), and by `web-reviewer`:

- **Phone first** (decision 21; owner, 2026-10-17): a screen is designed at 320–390 CSS px
  wide and then widened; a flow is complete on a phone with touch alone (no hover, no
  keyboard shortcut, no drag, no right-click as the only way); the primary action of a
  screen is visible in the first viewport or fixed within thumb reach at the bottom, never
  only at the end of a long page; navigation is a disclosure "Menu" on a phone; tables
  collapse to stacked rows; dialogs are full-screen below 640 px; inputs use the right
  `type`, `inputmode` and `autocomplete` so a phone shows the right keyboard; nothing
  depends on a precise pointer (no tiny icons as the only control, no hover tooltips as the
  only explanation). A component test renders every shell screen at 320 px wide and fails
  on horizontal overflow.

- Landmarks: one `<header>`, one `<nav aria-label>` per navigation, one `<main>`, one
  `<footer>`; a skip link first in the DOM; one `<h1>` per screen; headings in order.
- Every input has a `<label>` (the `Field` component makes anything else hard); hints and
  errors are tied by `aria-describedby`; required fields say so in text, not colour; error
  text names the problem (never "invalid").
- Keyboard: everything reachable in DOM order; focus visible (a 2 px outline with 3:1
  contrast against both palettes; WCAG 2.4.13); focus never obscured by the sticky header
  (`scroll-padding-top`; 2.4.11); dialogs are `<dialog>` with `showModal()` (trap, `Esc`,
  return focus); menus are disclosure buttons, not hover.
- Pointer: targets at least 44 × 44 CSS px; no drag-only interaction (2.5.7); no
  hover-only content.
- Motion and colour: `prefers-reduced-motion` disables transitions; colour is never the
  only signal (icon or text beside it); text contrast 4.5:1 and UI contrast 3:1 in both
  palettes, checked by a test over the token values.
- Layout: works at 320 px wide with no horizontal scroll (1.4.10); text resizes to 200%;
  tables collapse to stacked rows on narrow screens with headers repeated as labels.
- Announcements: route changes and form outcomes reach a live region; loading states are
  `aria-busy` on the region and text after 300 ms.
- Authentication (3.3.8): the sign-in form allows paste and password managers; no
  cognitive test.
- Language: `<html lang>` follows the active language; a fragment in another language
  (the admin warnings, the `reason` inside `field.unknown`) carries `lang`.
- Slow connections: module code and catalogues load lazily; the bundle budget is enforced
  by `scripts/check-bundle-size.mjs` against `dist/.vite/manifest.json`; images are
  avoided or sized; no blocking web fonts.

### Build and tooling

| Concern | Choice (license) | Pinned how |
|---|---|---|
| Runtime | Node.js LTS (MIT); pnpm (MIT) | `web/.nvmrc`, `packageManager` in `package.json`; `pnpm-lock.yaml` committed; `save-exact=true` in `.npmrc` |
| Build | Vite (MIT); `@vitejs/plugin-react` (MIT); `@tailwindcss/vite` (MIT) | exact versions |
| UI | React 19 (MIT); react-router 8.4 in data mode (MIT); TanStack Query (MIT); FormatJS `react-intl` (BSD-3); Lucide (ISC) | exact versions |
| API | `openapi-typescript` (MIT, dev); `openapi-fetch` (MIT) | exact versions |
| Lint and format | ESLint (MIT) with `typescript-eslint` (MIT), `eslint-plugin-react-hooks` (MIT), `eslint-plugin-jsx-a11y` (MIT); Prettier (MIT) | exact versions |
| Tests | Vitest (MIT), jsdom (MIT), Testing Library (MIT), `axe-core` through a small project helper (MPL-2.0, OSI-approved), Playwright and `@axe-core/playwright` (Apache-2.0, MPL-2.0) | exact versions |
| OpenAPI lint | Spectral CLI (Apache-2.0) with the `spectral:oas` ruleset | exact version |
| Licenses | `web/scripts/check-licenses.mjs` over `pnpm licenses list --json`: every package's license must be in the allowlist (MIT, ISC, BSD-2/3, Apache-2.0, MPL-2.0, 0BSD, Unlicense, CC0-1.0, BlueOak-1.0.0, MIT-0, Python-2.0), plus caniuse-lite (CC-BY-4.0, dev-only) by name (decision 25); anything else fails `pnpm check` | the allowlist is in the script |

`pnpm check` runs, in order: `generate:messages` (and fails if `keys.d.ts` changed and is
not committed), `lint`, `typecheck` (`tsc --noEmit`, strict), `test` (Vitest with
coverage), `openapi:lint` (Spectral over `web/openapi.json`), `licenses`, `build`,
`bundle-size`. The Makefile gains `web-check` (`pnpm -C web check`), `web-build`
(`BMS_VERSION=... pnpm -C web build`, then `dist/version.txt`), `openapi-check` (`go run
./cmd/bms openapi > web/openapi.json && pnpm -C web generate:api && git diff --exit-code
web/openapi.json web/src/api/schema.d.ts`), `test-e2e` (Playwright against `bms serve` on
the compose database; needs `make up`), and `check` adds `web-check` and `openapi-check`
(Proposed change 4). Development: `pnpm dev` runs Vite on `:5173` with `/api`, `/healthz`
and `/readyz` proxied to `:8080` (the browser sees one origin, so cookies and cross-origin
protection behave as in production; `localhost` is a secure context, so the `__Host-`
cookie works; a phone on the LAN uses `http.allow_plaintext` and the development cookie,
api decision 24). Vite's dev server has its own relaxed CSP; the production build is what
`api` serves and tests.

Output: `dist/index.html`, `dist/assets/<name>-<hash>.{js,css,svg}`, `dist/version.txt`,
`dist/.vite/manifest.json`; `base: "/"`; no inline scripts; no `public/` files besides
`favicon.svg` and `robots.txt` (`Disallow: /`: a regional's site is not for indexing). The
Go package `web` embeds `all:dist` and `FS()` returns `fs.Sub(dist)` with dot-prefixed paths hidden (decision 26); `web/dist/.gitkeep` is
committed so the embed compiles in a checkout without a build (Proposed change 9).

### Concurrency and lifecycle

Single-threaded; no web worker, no service worker (decision 11). The stores are plain
objects with `subscribe` read through `useSyncExternalStore`. The reporter owns one timer
at a time and the `pagehide`/`visibilitychange` listeners; it is installed once and never
uninstalled. TanStack Query's garbage collection bounds memory. Sign-out and `401` reset
every store and the cache; a full reload is not needed but `location.replace` is used after
sign-out so no screen keeps a reference to old data. Two tabs are two independent pages;
the cookie is shared by the browser and a `401` in one tab does not inform the other until
its next call.

### Imports and boundaries

`web/src/shell/**` → anything under `@/` plus `@/modules/index.ts` and `@/core/index.ts`
(never a set's folder). `web/src/modules/<a>/**` and `web/src/core/<a>/**` → `@/api`,
`@/i18n`, `@/ui`, `@/registry/types`, themselves. `web/src/api/**` → `@/i18n` (nothing
else of ours). `web/src/ui/**` may `import type` from `@/api`. The frame-private
setters (`internal.ts` in `api`, `i18n`, `ui`) are imported by `shell/**` only (decision 27). `web/src/i18n/**`, `web/src/ui/**`, `web/src/registry/**` → `@/i18n`, `@/ui`
(acyclic). `fetch`, `XMLHttpRequest`, `sendBeacon`, `WebSocket`, `EventSource`: `@/api/**`
and `shell/report.ts` only. `localStorage`: `i18n/lang.ts` only; `sessionStorage`,
`indexedDB`, `caches`, `document.cookie`: nowhere. The Go package `web`: standard library
and `internal/lib/errs`; imported by `internal/app` only (depguard, Proposed change 8).

### Accepted implementation deviations (M1 build, 2026-10-18)

- The restricted-session gate (a session with `password_change_required`) holds the person on `/accounts/password` for every route except that one. The identity spec said "outside /accounts/", but the server answers 401 on `/accounts/admin`; identity.md's UI paragraph is corrected too.
- `@/api` gains `refreshSession`, `setSessionEnded` (ESLint limits both to `core/accounts`), `safeReturnTo`, `useReadOnly` and `useSessionCheckFailed`; see the Interface.
- `ErrorState` gains an optional `message` prop.

**M2 build (2026-10-18):**

- **Shell.** `RouteAnnouncer` moves focus only on a pathname change.
- **`@/api`.** `useMe` and `useSetMe`, limited to `core/people` by ESLint. The client accepts multi-line NDJSON with `parseAs: "text"`.
- **UI kit additions.** `ui/DateField` (`endOfDayInstant`, `lastDayOf`, `todayDate`); `ui/Check` (44 px); `Button` accepts a ref.
- **Duplication.** Done (D52(e)): `ui/PersonSearch` (a `search(q, signal)` prop) replaced the `PersonPicker` copies in `core/access`, `core/audit` and `modules/volunteers`, and `useAction`, `holdsSite`/`holdsAt`/`holdsAny` and `useNames` live in `@/api` (`web/src/api/action.ts`, `permissions.ts`, `index.ts`). Kept on purpose: the volunteers `useAction` wrapper (`withRangeCodes`, which maps that module's own range codes) and Leadership's own names query (`["departments", "names", …]`).
- `web/src/core/audit/` (the audit screen) is built in M2.

## Failure modes

"Kind" is the `ApiError.code` a screen sees, the shell's own error class, or (for the Go
package) the `errs` kind. Tests are Vitest files and names unless marked Go or e2e.

| # | Method | Condition | Kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `api.*` | `fetch` rejects: offline, DNS, reset | `ApiError{status 0, code network}`; reported | `ErrorState` with retry; queries retry twice | `client.test.ts` "network failure becomes ApiError network and is reported" |
| F2 | `api.*` | no response within 35 s | `network` (message `timeout`); reported | same | "client timeout aborts at 35 s with fake timers" |
| F3 | `api.*` | non-2xx with `application/problem+json` | `ApiError` from the body: `status`, `code`, `ref`, `fields`, `retryAfter` | show per code | "problem body maps every field", "codes table is pinned against web/openapi.json" |
| F4 | `api.*` | non-2xx without a problem body (HTML from a proxy, empty) | `malformed_response` with the status; reported | generic error with ref if any | "non-problem error body is malformed_response and reported" |
| F5 | `api.*` | 2xx whose JSON does not parse | `malformed_response`; reported | generic error | "unparsable success body" |
| F6 | `api.*` | `401` on any route but the session endpoint and sign-in's own | `unauthenticated`; `onUnauthenticated(returnTo)` called; not reported | the frame redirects; the screen settles | "401 clears session and navigates to sign-in with returnTo", "401 on the session endpoint does not redirect" |
| F7 | `api.*` | `403 cross_origin`, `403 misdirected`, `405`, `413`, `415` | that code; reported | generic error with ref | "deployment and shell bugs are reported" (table) |
| F8 | `api.*` | `429` | `rate_limited`, `retryAfter`; not reported | `error.rate_limited` with the seconds | "429 carries retryAfter" |
| F9 | `api.*` | `503 read_only` | `read_only`; `onReadOnly(true)` already from the header; not reported | `error.read_only` in the summary; banner up | "read_only sets the store and is not reported" |
| F10 | `api.*` | `500 internal` | `internal`; reported with `error_ref` and `trace_id` | `ErrorState` with ref | "internal is reported with ref and trace id" |
| F11 | `useFieldErrors` | `fields[].code` unknown or empty | the translated wrapper `field.unknown` with the English `reason` inside a `<span lang="en">`; one `api` report "untranslated field code <code>" per page per code | add the translation | `errors.test.tsx` "unknown field code shows the wrapper with reason marked lang=en and reports once"; "known field code never renders reason" |
| F11b | `messageFor` | `code` not in the catalogue (a server newer than the shell) | `error.unknown`; one `api` report "untranslated error code <code>"; reported even when the code itself is not reportable (decision 14) | add the translation | `errors.test.tsx` "unknown error code shows error.unknown and reports once" |
| F12 | `api.*` | any response's `X-Read-Only` | not a failure: `onReadOnly(true/false)` per response | banner follows | "X-Read-Only toggles the store both ways" |
| F13 | reporter `flush` | intake answers `429` | reports kept; paused for `Retry-After` (60 s default) | — | `report.test.ts` "429 pauses for Retry-After and keeps pending" |
| F14 | reporter `flush` | intake answers `400` | that batch dropped; dev `console.warn` | fix the shell | "400 drops the batch" |
| F15 | reporter `flush` | intake unreachable | kept; backoff 10 s doubling to 5 min; never itself reported | — | "network failure backs off and never recurses" |
| F16 | reporter | more than 100 distinct pending | oldest dropped | — | "pending cap drops the oldest" |
| F17 | reporter | a `screen` or `api.route` that is not a token | cannot happen: patterns come from the router and the client, `:p` rewritten `{p}`; pinned | — | "every producible pattern matches the token grammar" |
| F18 | reporter | an exception inside any handler | swallowed; the page continues | — | "a throwing sender never propagates" |
| F19 | `validateRegistry` | a duplicate, invalid or reserved `name` | `RegistryError` naming the set; the crashed-screen state in production, a failing test in CI | fix the set | `registry.test.ts` "duplicate, invalid and reserved names are refused", "the real lists validate" |
| F20 | `validateRegistry` | an absolute route `path`, a `..` segment, or a leaf without `handle.title` | `RegistryError` | fix the set | "absolute or untitled routes are refused" |
| F21 | `validateRegistry` | a set without `messages.en` | `RegistryError` | add `en.json` | "a set without en is refused" |
| F22 | `NavEntry.label`, `t()` | a key not in any `en.json` | a `tsc` error (generated key types); cannot reach runtime | fix the key | `generate-messages.test.ts` "keys.d.ts lists every en key and nothing else" |
| F23 | `t()` | key missing in the active language | the `en` text; one dev warning per key | translate it | `i18n.test.ts` "missing es key falls back to en" |
| F24 | `t()` | key missing in `en` too (a catalogue edited by hand without regenerating) | the key literal, never English prose from elsewhere | regenerate | "missing en key returns the key" |
| F25 | `lang.ts` | `localStorage` throws (private mode, quota) | ignored; the choice lives in memory for the page | — | `lang.test.ts` "storage errors are ignored" |
| F26 | `lang.ts` | stored tag not offered | ignored; the default language | — | "unknown stored tag is ignored" |
| F27 | boot | `GET /api/modules` fails | the cannot-reach-server state (`network`) or the error state with ref; retry refetches | retry | `app.test.tsx` "modules failure shows retry state" |
| F28 | boot | `GET /api/accounts/session` answers `404 not_found` (M0: no identity) | signed out; sign-in page says sign-in is unavailable; not reported | — | "session 404 means signed out (M0)" |
| F29 | boot | the session call fails otherwise (`network`, 5xx) | signed out for the frame, a banner "Couldn't check whether you're signed in." with retry; not a crash | retry | "session failure shows banner and keeps the frame" |
| F30 | `web.FS` (Go) | `dist/index.html` missing | `errs.NotFound` | app: warn, `UI: nil` | Go `TestFS_WithoutBuild_ReturnsNotFound` (a `testing/fstest` dir), `TestFS_WithBuild_ServesIndexAndAssets` |
| F31 | `web.Version` (Go) | `version.txt` missing; not a token or > 64 bytes | `errs.NotFound`; `errs.Invalid` | app: warn | Go `TestVersion_Table` |
| F32 | route change | focus and announcement | not a failure mode: `h1` focused, title set, live region updated | — | `frame.test.tsx` "route change moves focus and announces" |
| F33 | `ErrorSummary` | a mutation fails with fields | summary focused, `role=alert`, links per field, `aria-invalid` and `aria-describedby` on inputs; unknown field paths in the summary only | — | `errors.test.tsx` "summary focuses and wires inputs" |
| F34 | `/sign-in` | `returnTo` not a same-origin path (`//evil`, `https://...`) | ignored; `/` after sign-in | — | `signin.test.tsx` "open redirect refused" |
| F35 | gate | a signed-in route visited while signed out | redirect to `/sign-in?returnTo=` without a call | sign in | "gate redirects before calling" |
| F36 | build | a chunk over budget; a dependency with a license outside the allowlist; an external origin in `dist/` | `pnpm check` fails | fix it | `check-bundle-size` and `check-licenses` have fixture tests; `TestBundle_NoExternalURLs` |
| F38 | `validateRegistry` | a set declares a route `loader` or `action` | `RegistryError` | fetch in the screen through TanStack Query | `registry.test.ts` "loaders and actions are refused" |
| F39 | boot | a navigation catalogue of an enabled set fails to load | reported as `api`/`network` or `malformed_response`; banner with retry; the frame stays | retry | `app.test.tsx` "catalogue failure shows retry banner" |
| F37 | `make imports` | `modules/<a>` imports `modules/<b>`, `core/*` or `shell/*`, or names `"/api/<b>/"` | the script fails naming the file | use a contract or extension point | `scripts/check-web-module-imports.sh` fixture run in CI; ESLint rule tests |

Members that cannot fail, and why: `messageFor`, `showsRef` (table lookups with a default);
`useT` (never throws: fallback chain); `Reporter.flush` (every path caught); `configureClient`
(stores functions); `web.FS` when `index.html` exists (an embedded FS). Each has a
success-path test.

## Lock-in check

No hosted service and nothing proprietary: every dependency is OSI-licensed (the allowlist
is enforced in `pnpm check`), self-hosted from the binary, and replaceable at a seam:

| Wrapped | Seam | Alternative that proves it |
|---|---|---|
| `openapi-fetch` + `openapi-typescript` | `web/src/api/client.ts`; screens see `api` and `ApiError` only | a hand-written `fetch` wrapper over the same `schema.d.ts`, or any OpenAPI 3.1 generator |
| FormatJS / ICU | `useT`, `useFormat`; catalogues are flat JSON | Lingui or i18next read the same JSON after a one-time syntax conversion |
| TanStack Query | screens' hooks only | React's own `use` with a tiny cache; SWR |
| react-router | `ScreenSet.routes` is `RouteObject[]` | TanStack Router with an adapter in the registry |
| Tailwind | class strings | plain CSS modules (the tokens are CSS variables) |
| OpenTelemetry on the server | the shell emits W3C `traceparent` only, no RUM SDK | any backend that reads W3C trace context |
| Browser APIs | standard only (`fetch`, `sendBeacon`, `<dialog>`, `Intl`, `crypto.getRandomValues`) | every current engine |

## Test plan

**Unit and component** (Vitest, jsdom, Testing Library, `vi.useFakeTimers()` for every
timing, a fake `fetch` installed per test, the `axe-core` helper on every rendered state; no network,
no real timers, no `localStorage` unless the test is about it; invented people and markers
`ZQ9MARKER7` / `zq9@marker.example` asserted absent from every report body and URL):

- Client: F1–F10, F12; `traceparent` format and freshness per call; `credentials`; the
  caller's abort is not an `ApiError`; `module` and `route` derivation (`/api/hello/greeting`
  → `hello`; `/api/modules` → `core`); `params.path` encoding; never a query string built
  from a free string.
- Error display: F11, F11b, F33; `messageFor` for every code in the table; `showsRef` table
  (decision 9); field lookup order `module.<m>.field.<code>` → `field.<code>` → `field.unknown`;
  `params` interpolation (`range` with `min`/`max`); the reference line and copy button;
  `reason` absent from the DOM for every known code and present only inside
  `<span lang="en">` for an unknown one (decision 2).
- Reporter: F13–F18; dedup key and `count`; batch ≤ 20; 10 s timer only while pending;
  `pagehide` uses `sendBeacon` with an `application/json` Blob and falls back; reportable
  table (decision 14); an `ApiError` with `reported` is skipped by the rejection handler;
  query strings stripped from stacks; `occurred_at` within a second of the fake clock;
  `app_version` present; the intake call never passes through the client.
- Registry: F19–F21; the real `coreSets` and `moduleSets` validate; a disabled module's
  routes are not mounted and its nav is absent (`/hello` → not found when `hello` is
  disabled); core sets ignore enablement; `access` filtering of nav by session.
- i18n: F23–F26; `{term_event}` substitution; plural and date formatting in `en` and a
  second catalogue; `<html lang>` follows the language; `keys.d.ts` generation (F22).
- Frame: F27–F29, F32, F34, F35; the read-only banner appears after a response with the
  header and clears after one without; the superuser banner; the home screen's three
  states; `document.title`; the admin modules screen's five states from a fixture matching
  api.md's `AdminModulesResponse` (lower-case keys), including a `warnings` fragment with
  `lang="en"`; the mobile menu's `aria-expanded` and focus; sign-out clears the query cache.
- Accessibility and phone layout: every shell screen and UI-kit component rendered in each
  state has no `axe` violations; every shell screen rendered at 320 px wide has no
  horizontal overflow and its primary action is in the first viewport (decision 21); token
  contrast test (4.5:1 text, 3:1 UI, both palettes); logical property lint fixture.
- Tooling: `check-bundle-size`, `check-licenses`, `check-web-module-imports.sh` and the
  message-key generator each have fixtures (pass and fail).

**Go** (`web` package, no infrastructure): F30, F31; `TestFS_ServesHashedAssetsAndIndex`
over a `fstest.MapFS` and over the real `dist/` when present (skipped otherwise).

**End-to-end smoke** (Playwright, `make test-e2e`, behind `make up`, against `bms serve`
with the `hello` module and the production build; decisions 21 and 22). Two Playwright
projects run **every** scenario: `phone` (a Playwright device preset for a current
mid-range Android phone: about 390 × 850 CSS px, `hasTouch`, `isMobile`, driven by taps)
and `desktop` (1280 × 800, mouse and keyboard); `@axe-core/playwright` runs on each shell
screen in both, and the `phone` project also asserts no horizontal scroll on any screen.
Scenarios: the frame renders with skip link and landmarks (on the phone: the "Menu"
disclosure opens, traps nothing, and closes on navigation); the `hello`
entry appears and its screen calls `GET /api/hello/public-greeting`; with
`BMS_MODULES_DISABLED=hello` the entry is gone and `/hello` is not found; with the database
set `default_transaction_read_only = on` the banner appears after the first call and the
server's `X-Read-Only` is `true`; a forced `GET /api/hello/greeting` without a session lands
on `/sign-in?returnTo=/hello` (M0: F28 state); the CSP header is present on `/` and no
console CSP violation is logged during the run. From M2 the run adds "grant a role and see the menu change" and the two people flows (a new account is sent to `/people/me?first=1`, fills in a playa name and sees it in the header; a language change survives a reload), at the phone viewport.

**Lint:** ESLint (boundaries, `jsx-a11y`, `react-hooks`, `no-console` except the reporter,
no `style` prop, no `dangerouslySetInnerHTML`, no `fetch` outside `@/api` and the reporter,
no storage APIs outside `lang.ts`, logical properties, no `min-width` utility above
`320px` on a layout container and no `hover:`-only disclosure of content), `tsc --strict`,
Prettier, Spectral over `web/openapi.json`.

**Conformance:** `module-hello`'s `web/src/modules/hello/` is the reference set and must
pass `registry.test.ts`, the boundary script and the Go catalogue test; every later module
UI is checked by the same three.

## Open questions

None. The draft's six questions were answered by the owner on 2026-10-17 and are recorded
under "Decisions settled here": the server's `reason` as the last-resort fallback inside a
translated wrapper (decision 2; reversed from the draft's proposal), the regional's default
language with browser detection as a later setting (decision 7), dark mode from M0
(decision 18), modern browsers only with no update notice (decision 17), system fonts for
the MVP (decision 15), the end-to-end smoke run in M0 (decision 22), and phones as a hard
requirement (decision 21).

## Proposed changes elsewhere

1. **`plan/design/api.md` (row 16), Proposed change 12; `plan/design/lib-core.md` (row 1b)**
   (applied 2026-10-17: api.md's Proposed change 12 marked applied; lib-core.md needed no
   change). The draft asked both to say the shell never renders `reason`; the owner
   reversed that (decision 2), so api.md decision 25 and lib-core's "the web shell then
   shows `Reason`" stand as written, with the refinement that the shell shows `reason`
   inside the translated `field.unknown` wrapper and reports the code. Proposed change
   12's items are met: codes mirrored (`ProblemCode`), `error.<code>` keys, `ref` per
   decision 9 here, `401` → sign-in, the banner from `X-Read-Only` both ways,
   `GET /api/modules` and `GET /api/admin/modules` read as specified, the client generated
   at build time and the document linted with Spectral, hashed assets under `assets/`, no
   inline scripts, no plugins, **no CSP change requested** (decision 10), `traceparent` on
   every call, `403 misdirected` treated like `cross_origin`, `version` from the admin
   route. One note for api.md: `GET /api/modules` and `GET /api/accounts/session` are
   called at boot in parallel, so neither may depend on the other.
2. **`plan/design/telemetry.md` (row 15b), "UI" and Proposed change 4** (applied 2026-10-17): the reference is
   shown for every code except `invalid`, `unauthenticated`, `read_only` and the client's
   `network` (decision 9); Proposed change 4 is answered with these refinements: which API
   failures are reported (decision 14: `network`, `malformed_response`, `internal`,
   `cross_origin`, `misdirected`, `method_not_allowed`, `payload_too_large`,
   `unsupported_media_type`, plus an untranslated field code), a pending cap of 100 with
   oldest-first drop, `keepalive` `fetch` as the `sendBeacon` fallback, the 10 s timer only
   while something is pending, backoff 10 s to 5 min on a network failure, `screen` derived
   from the router's pattern with `:p` → `{p}`. Nothing is written to browser storage by
   the reporter (the one storage key is the language, decision 7).
3. **`plan/design/module-contract.md` (row 4), Proposed change 7 and "UI"** (applied 2026-10-17, minimal: Proposed change 7 marked applied with the summary below; decision 9 gained the core-areas sentence): answered: each
   set ships `web/src/modules/<name>/messages/<lang>.json` (flat JSON, ICU messages) with
   `en` required; the manifest-derived keys are checked by a Go test in `internal/app`
   (`TestMessageCatalogues_EnglishCoversManifests`), which imports every module and reads the
   files relative to the repository root (skipped when `web/` is absent); screens mount
   only when `GET /api/modules` lists the module as enabled; the admin "modules" screen
   renders the graph as tables with the warnings (decision 16). Add to decision 9: the web
   registry lists core areas too (`kind: "core"`), always enabled.
4. **`plan/design/app.md` (row 18)** (applied 2026-10-17, by app.md's "`openapi`, step by step", decision 16 and "The Makefile"; refinements: `info.version` is fixed to `dev` so the committed document is stable across commits; `TestMessageCatalogues_EnglishCoversManifests` requires `messages/en.json` for every registered module, screens or not, and is skipped only when `web/src` is absent; `bin` (not `build`) depends on `web-build`; `check-go` is the Go-only subset; `scripts/e2e.sh` owns the server's start and stop under `test-e2e`): (a) a `bms openapi` command that prints the assembled
   document (`api.New` with the compiled-in modules' route sets, a stub `Database` and
   `SchemaStatus`, `UI: nil`; no network or database I/O) for `make openapi-check`;
   (b) `serve` calls `web.FS()`, passes it as `api.Options.UI`, and on `errs.NotFound`
   passes `nil` and logs one warning "serving without a UI"; (c) `serve` compares
   `web.Version()` with the binary's version and warns once when they differ; (d) `app`'s
   tests host `TestMessageCatalogues_EnglishCoversManifests` (item 3); (e) the Makefile
   targets `web-check`, `web-build`, `openapi-check`, `test-e2e`, with `check` extended
   by `web-check` and `openapi-check`, and `build` depending on `web-build` for a release
   binary; (f) `app` is the only importer of the `web` package.
5. **`plan/design/identity.md` (row 6)** (applied 2026-10-18, by identity.md Proposed change 2, which extends the session body with `account_id`, `password_change_required`, `expires_at`, `signup_open`, redirects `/sign-in` to `/accounts/sign-in`, moves the screen and keys to `core/accounts` and adds the restricted-session gate (it holds the person on `/accounts/password` for every route except that one, because the server answers 401 on `/accounts/admin`), the neutral "Account" label, the field codes `too_short {min}`, `too_long {max}`, `common`, `taken`, `incorrect`, `self` and the sign-up, sign-in and password change smoke run; the audit core folder `web/src/core/audit/` is audit.md Proposed change 14): the frame needs `GET /api/accounts/session`
   (`AccessPublic`, `200` always: `{"signed_in": false}` or `{"signed_in": true,
   "person_id", "playa_name", "superuser", "permissions": []}`; `no-store`), `POST
   /api/accounts/sign-in` (`{email, password}` → `204` + cookie; `401 unauthenticated`
   without saying which was wrong; `429 rate_limited`), `POST /api/accounts/sign-out`
   (`204`; works on a read-only copy). The sign-in screen lives in `web/src/core/accounts/`
   as a `ScreenSet` named `accounts`; `/sign-in` is a shell alias to it. The shell never
   reads the cookie. `401` handling is decision 6 here.
6. **`plan/design/authz.md` (row 7)** (applied 2026-10-18, by authz.md Proposed change 7: `permissions` holds `p` and `p@<department-id>`, `access` matches `p` exactly or any `p@...`, the M1 placeholder is removed, `web/src/core/access/` is a core area, the smoke run adds "grant a role and see the menu change", `ui` gains a date-only input helper): fill `permissions` in the session response (09 §4:
   "once per page load") with the names the person holds site-wide plus, for
   department-scoped grants, `"<permission>@<department-id>"`, so a `NavEntry.access`
   can name a permission and a department screen can test its own; the server still
   decides every call.
7. **`plan/design/settings.md` (row 15)** (applied 2026-10-19, D39, by settings.md Proposed change 8; `language.detect_browser` and the brand font (decision 15) stay Later, declared when the shell implements its side; the logo comes with the `files` spec and is not part of M4): `GET /api/settings/public` (`AccessPublic`,
   `no-cache` in intent; `no-store` still applies under `/api/`, api decision 12, so it is fetched once per boot, with an `ETag` and `304`): regional name, default language, offered languages, time zone, whether a theme exists,
   terminology per language as `{"term_event": "...", ...}`; `GET /api/settings/theme.css` (same
   origin, `text/css`, the regional's colours as CSS custom properties), validated for
   AA contrast against the shipped palette when saved. The details: the frame fetches the public document at boot (before the catalogues settle; on failure it keeps `DEFAULT_REGIONAL_NAME`, `DEFAULT_LANGUAGE` and the catalogue's terms, reports once and retries with the `GET` policy); `useRegionalName` and `configureLanguages(languages, language)` read it; `termsOf` merges the document's `terms[lang]` over the catalogue's `term.*` entries; the catalogue gains `term.<x>.plural`, `term.<x>.title`, `term.<x>.plural_title` for the four core terms (`event`, `department`, `participant`, `theme_camp`; the `en` values must equal settings' `terms/en.json`, tested in `internal/app`); `<link rel="stylesheet" href="/api/settings/theme.css">` in `index.html` after the bundle's CSS (no change to `api.DefaultCSP`: `style-src 'self'` covers it); the header shows the regional's name as text; `field.contrast` joins the field-code keys (`"{ratio}:1 is below {min}:1"`); `/settings` is listed among the admin nav entries (`settings.read`); `web/src/core/settings/` and `web/src/core/notify/` (notify.md; visible to holders of either notify permission) are core area folders.
8. **`plan/07-abstractions.md`, `web shell` entry and layer rules** (applied 2026-10-17, with "never the server's `reason`" corrected to the owner's answer and the phone requirement added): replace the entry with:
   "**Purpose:** host every module's screens in one browser application. **Owns:** the
   frame (navigation from the enabled modules, sign-in state, the read-only and superuser
   banners, the language menu), the typed API client generated from `/api/openapi.json`
   (`traceparent`, one `ApiError`, `X-Read-Only`, `401` → sign-in), the build-time screen
   registry (one folder per module under `web/src/modules/`, one per core area under
   `web/src/core/`, mounted under `/<name>/`, modules shown only when enabled; D31),
   translation (FormatJS/ICU catalogues per set, keys typed, terminology as arguments;
   D28), error display (codes and field codes translated, the reference shown, the
   server's English `reason` only as the last-resort fallback inside a translated
   wrapper), the reporter (`POST /api/client-errors`; D20), the accessibility rules and
   the phone-first rules (a phone is the primary device; a hard requirement), the pinned
   OSI-only toolchain, and the Go package `web` that embeds
   `dist/` for `app`. **Doesn't do:** any module's or core area's screens (their specs),
   authorization (the server), serving and the CSP (`api`), settings and branding values
   (`settings`), offline caching (Q-OFFLINE). **Spec:** `design/web-shell.md`." Add a
   layer line: "`web` (`web/embed.go`): the built shell; imports stdlib and `lib/errs`;
   imported by `app` only." Add to the rules: "In `web/src/`, a module's or core area's
   folder never imports another's, `core/*` or `shell/*`; enforced by ESLint and
   `scripts/check-web-module-imports.sh` under `make imports`."
9. **`.gitignore`** (applied 2026-10-17): `web/dist/` becomes `web/dist/*` plus `!web/dist/.gitkeep`, so the Go
   embed compiles in a fresh checkout (F30 then reports no build).
10. **`plan/design/module-hello.md` (row 4b)** (applied 2026-10-17, by module-hello.md's Proposed change 3; the set is as that spec's "UI" describes, and `isMessageKey` is added to the interface, decision 23): ship `web/src/modules/hello/` (`index.ts`
    exporting `ui: ScreenSet` named `hello`, `messages/en.json` with `module.hello.title`,
    `.description`, the permission and signal titles module-contract decision 8 implies,
    and `screens/Greeting.tsx` calling `GET /api/hello/public-greeting` and, signed in,
    `GET /api/hello/greeting`, with every state), as the reference for module authors; it
    is the set the e2e smoke test exercises.
11. **`.claude/agents/frontend-implementer.md` and `web-reviewer.md`** (applied 2026-10-17): stack line adds
    TanStack Query, FormatJS (`react-intl`), `openapi-fetch`, Testing Library, `axe-core`,
    Playwright; rules add: screens under `web/src/modules/<name>/` or `web/src/core/<area>/`
    exporting `ui: ScreenSet`, routes relative to `/<name>/`; strings through `useT` with
    typed keys and `{term_*}` for terminology; no `style` prop (CSP); `ErrorState` and
    `ErrorSummary` for every failure; `name` attributes equal to the API's field paths;
    `pnpm check` is the verification command; **phones are a hard requirement**: build at
    the phone width first, every flow completable with touch on a small phone, the e2e run
    at the phone viewport must pass. `web-reviewer` adds: the server's `reason` in the DOM
    only inside the `field.unknown` wrapper with `lang="en"`; no `fetch` outside `@/api`;
    logical properties; review at the phone width first and treat a flow that needs a wide
    screen or a mouse as **must fix**.
12. **`.claude/skills/add-module/SKILL.md` §2 and §3** (applied 2026-10-17): the `web/src/modules/<name>/` block
    becomes `index.ts` (exports `ui: ScreenSet`; one import line in
    `web/src/modules/index.ts`), `messages/en.json` (the keys of module-contract decision 8
    plus the module's own, `module.<name>.*`), `screens/`, `components/`; §3 adds "a
    module's screens call only `/api/<name>/` and the core's routes; another module's data
    comes through a contract the owning module renders".
13. **`plan/design/people.md` (row 8)** (applied 2026-10-18, by people.md Proposed change 7: the language is read from `GET /api/people/me`, called once by the frame after the session is established, not from the session response; a signed-in change is written with `PUT /api/people/me {language}`; an empty `playa_name` sends the person to `/people/me?first=1` once per session; `useMe()` in `@/api`): a profile field `language` (`internal`, optional, a
    BCP 47 tag from the offered list) that the shell reads from `GET /api/people/me` and
    writes when a signed-in person changes the language; the browser key mirrors it.
14. **`plan/09-core-features.md`, "Ground rules", §1 "Done when" and §7** (applied 2026-10-17): "WCAG 2.1 AA" → "WCAG 2.2 AA" (principle
    4 already says 2.2); "Mobile first" becomes a hard requirement in the owner's words
    (many people only have phones; every screen and flow usable on a small phone screen
    with touch; phone-first layouts and touch targets; the end-to-end smoke run at a phone
    viewport). §1 "Done when" gains: the shell's screens pass the smoke run at a phone
    viewport. §7 "Admin screens": the admin "modules" screen is the shell's; the others
    are each core area's `ScreenSet`.
15. **`docs/operating.md` (when written) and `README.md`** (pending: target not written): supported browsers (decision 17);
    building the UI (`make web-build`, Node LTS and pnpm); that the binary serves the UI
    and no Node is needed at runtime; that a build without `dist/` serves the API only;
    that the UI is built for phones first.
16. **`.golangci.yml`** (applied 2026-10-17): a depguard rule for `web/**` (standard library and `internal/lib/errs`
    only) and `web` added to the deny list of every rule but `app`'s.
17. **`.claude/skills/go-conventions/SKILL.md`, "Layers"** (applied 2026-10-17): add the `web` line from item 8.
18. **`plan/03-decisions.md`, D28** (for plan-keeper; applied 2026-10-17): add one bullet:
    "Clarified (owner, 2026-10-17): when no translation exists for a server field-error
    code, the shell shows the server's English `reason` inside a translated wrapper
    message and reports the untranslated code; English is accepted as the last-resort
    fallback only inside such a wrapper (web-shell.md decision 2)."
8. **`plan/design/files.md` (row 17)** (applied 2026-10-20, D44, by files.md Proposed change 7): `upload`, `FileField`, `Attachments`, the header logo, the catalogue keys `field.too_large`, `field.type`, `field.quota`, `field.too_many`, `field.order`, `field.malformed` (add to the `en` and `es` catalogues if absent), and the e2e logo round trip in the smoke test; ESLint keeps `fetch` inside `@/api`.
9. **`plan/design/privacy-tags.md` (row 17)** (applied 2026-10-20, D44, by privacy-tags Proposed change 5): the `privacy.audience.*` catalogue keys and the rule "an absent field renders nothing".
