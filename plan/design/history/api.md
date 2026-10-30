# api: amendment history

Amendment narratives, reasons, review findings and superseded wording moved verbatim out of `plan/design/api.md` on 2026-10-30, when the amendments were folded into the spec body (only line breaks at sentence ends were added; no word changed).

## Notes moved from the spec header

- **Amended 2026-10-17 (owner):** decisions 19 and 24 reworded, decisions 28 to 30 added (mapper span error carries only op and kind below 500; cookie guard refuses `bms_session_dev` unless `AllowPlaintext`; port 0 in tests, CSP on every UI-handler response, status 499 for a gone client; accepted implementation deviations).
- **Amended 2026-10-18 (owner approval of audit/identity):** `httpx.ErrRateLimited` is added to `lib/httpx` (identity and intake both use it): the mapper answers `429 rate_limited` with `Retry-After: 60` for anything wrapping it, and `intake.ErrRateLimited` is, or wraps, it; `Authenticator` is implemented by identity as in Proposed change 9 (identity.md Proposed change 1).
- **Amended 2026-10-18 (owner approval of authz/people):** the mapper emits `fields` (from `validate.FieldError`) for a `409 conflict` as it does for `400 invalid`, so a duplicate can name its field (sign-up's `taken` names `email`; identity.md F15);
  `authz.Allow` satisfies `PermissionChecker` and refuses the actor inside `signals.InDelivery`; the declaration of `core.modules.read` gains `StarterRoles: authz.SiteAdminRoles` (authz.md Proposed change 6, Proposed change 10 applied).
- **Amended 2026-10-18 (M2 build, accepted deviations):** `api.Permissions` and the exported `core.modules.read` declaration (`StarterRoles: authz.SiteAdminRoles`) are in the exported surface (app.md, "Accepted implementation deviations (M2 build, 2026-10-18)"). A record of what was built, not a design change.
- **Amendment 2026-10-19 (approved, owner, 2026-10-20, D40):** Proposed change 15 settled for M4: decision 12's `no-store` stays on every mounted `/api/` route, a handler never sets `Cache-Control`, and `ETag`/`304` are allowed with it (the settings public routes); `Options.PublicOrigin` waits, its shape fixed over `regional.public_url`. F58, F59. See "Amendment 2026-10-19 (approved, owner, 2026-10-20, D40):
  Proposed change 15 settled for M4" before Open questions.
- **Amendment 2026-10-28 (approved, owner, D53 and D54; demo.md Proposed change 11, telemetry.md Proposed change 19):** `Options.NoIndex` (`X-Robots-Tag: noindex, nofollow` on every UI-handler response); `POST /api/client-events` (`AccessPublic`) over `Options.Usage *intake.Usage`; `bms.api.errors{http.request.method, http.route, code}`; `RouteTokens`; section "Amendment 2026-10-28" before Open questions; F63 to F72.

## Amendment 2026-10-19 (approved, owner, 2026-10-20, D40): Proposed change 15 settled for M4

Two questions settings.md's Proposed change 9 left with this spec, answered with the smallest design (settings.md's amendment of the same date carries the same decisions from its side).

**1. `Cache-Control` on `/api/settings/public` and `/api/settings/theme.css`.** Decision 12 stands unchanged: every `/api/` response carries `no-store` except the shell's own `/api/openapi.json` and `/api/modules`; no mounted route is exempted, and **a handler never sets `Cache-Control`**: the shell sets it before the handler runs, a handler that overwrites it is a review finding, and F58 pins the two routes that were tempted.
`ETag` and `304` are allowed with `no-store`: a validator lets a client that keeps the tag itself (the web shell, in memory) revalidate, and tells a cache nothing it may store. The cost is one small fetch of each per boot. A route-level "cacheable" declaration (`module.Route.Cache`) is not added until a route needs it; `files`, which will serve large bodies, is the candidate and may propose it.

**2. `Options.PublicOrigin`.** Waits; not M4. Its shape is fixed now so no spec waits on it:

```go
// PublicOrigin returns the regional's public URL (settings regional.public_url) or ""
// when unset. Optional; nil or "" is today's behaviour (the front end preserves Host,
// Proposed change 14). Read per request: a change needs no restart. app fills it over
// settings.Regional. The shell derives the origin (scheme://host[:port]) from it for
// decision 6 (one more trusted origin) and for the Host allowlist (the origin's host plus
// loopback and the bind address; a request whose Host is not listed is 404 with no body).
PublicOrigin func(ctx context.Context) (string, error)
```

A seam error is treated as `""` for that request and logged at warn once per minute (the settings snapshot cannot fail after its first load, so this is a start-up window). There is no configuration fallback: `config` owns no public URL (D18), so `regional.public_url` is the only source. Why wait: nothing in M4 needs it (the front end preserves `Host`; notify reads `Regional` itself for its links).

### Failure modes (added)

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F58 | pipeline | a mounted route answers with `ETag` (the settings public routes) | none: the response carries `Cache-Control: no-store` and the `ETag`; a matching `If-None-Match` gets `304`, also with `no-store`; a handler's own `Cache-Control` never reaches the client on `/api/` | nothing | `TestPipeline_MountedRouteWithETag_KeepsNoStore` (a fake route set in api's tests; `app`'s e2e repeats it on `GET /api/settings/public` and `/theme.css` once settings is built) |
| F59 | pipeline (lands with `PublicOrigin`) | the seam fails | none: treated as unset for the request; one warn per minute, no URL in it | fix settings | `TestPipeline_PublicOriginFails_TreatedAsUnset` |
| F60 (D40) | UI handler | a document request for `/accounts/password-reset` (with or without `?token=`) | none: the `text/html` response carries `Referrer-Policy: no-referrer`; any other document path keeps `strict-origin-when-cross-origin` | nothing | `TestUIHandler_PasswordResetPage_NoReferrer` (and `..._OtherPath_KeepsDefault`) |
| F61 (D44) | `New` | a route's `MaxBodyBytes` above 1 GiB or `Timeout` above 10 minutes (or negative) | Invalid (`api.routes[i].max_body_bytes` / `.timeout`) | fix the route | `TestNew_RouteCapAboveCeiling_ReturnsInvalid` |
| F62 (D44) | pipeline | a route with `Timeout` or `MaxBodyBytes` above the shell's | the route's own limits apply; a body over the route's cap is `413 payload_too_large` | nothing | `TestPipeline_RouteCapRaised_AppliesRouteLimits` |

Proposed change 15 now reads: deferred, shape settled above; it is applied when a regional needs a front end that rewrites `Host`, or with `files`, whichever comes first.

## Amendment 2026-10-28 (approved, owner, D53 and D54): `NoIndex`, `client-events`, `bms.api.errors`, `RouteTokens`

**`Options.NoIndex` (D53).** When set, every UI-handler response (D29's set) carries `X-Robots-Tag: noindex, nofollow`; `/api/` JSON does not. `app` sets it in demo mode only.

**`POST /api/client-events` (D54).** Mounted as `AccessPublic` beside `/api/client-errors` (decision 20's shell-route list grows by one).
The handler decodes under the 64 KiB `http.MaxBytesReader` into `intake.EventBatch` (unknown fields ignored), calls `Usage.Accept` with `EventSubmission{From: the derived client address, UserAgent, Batch}` and **never the session's person** (the handler does not read the session for this route; its request log, when any, carries no `person_id`).
It answers 202 with no body, 400 with field errors, 413, 415, 429 with `Retry-After: 60` and code `rate_limited` when `errors.Is(err, intake.ErrRateLimited)` (checked before the kind), and 501 `unsupported` when `Usage` is nil or the kind is `Unsupported` (usage counting off). The span is the normal request span.

**`bms.api.errors` (D54).** The error mapper records `bms.api.errors{http.request.method, http.route, code}` (counter, `{response}`) for every error response it writes, `code` being the problem code it chose (a kind name or a shell code); `/healthz` and `/readyz` are excluded as for the histogram.

**`RouteTokens` (D54).** `func RouteTokens(routes []module.ModuleRoutes) []string` returns the route tokens the tracing middleware names spans with (`{$}` as `/`, the shell's own routes included), for `app` to hand to `intake` (telemetry.md T1.5). It cannot fail and is pure.

**OpenAPI.** The shell routes' fragment documents `/api/client-events` with the `Event` schema and the response codes above.

### Failure modes (added)

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F63 | UI handler | `NoIndex` true | — | every response carries `X-Robots-Tag: noindex, nofollow` | `TestUI_NoIndex_SetsRobotsHeader` |
| F64 | UI handler | `NoIndex` false | — | no such header; `/api/` never has it | `TestUI_NoIndexOff_NoHeader` |
| F65 | `POST /api/client-events` | `Usage.Accept` fails `Invalid` | Invalid | 400 with `events[i].<field>` errors | `TestClientEvents_Invalid_Returns400` |
| F66 | `POST /api/client-events` | body over 64 KiB; wrong content type | Invalid | 413; 415 | `TestClientEvents_TooLarge_413`, `TestClientEvents_WrongContentType_415` |
| F67 | `POST /api/client-events` | `errors.Is(err, intake.ErrRateLimited)` | Unavailable | 429, `Retry-After: 60`, `rate_limited` | `TestClientEvents_RateLimited_429` |
| F68 | `POST /api/client-events` | usage off, or `Options.Usage` nil | Unsupported | 501 `unsupported` | `TestClientEvents_Off_501`, `TestClientEvents_NilUsage_501` |
| F69 | `POST /api/client-events` | a signed-in request | — | the person is never read or logged | `TestClientEvents_NeverReadsSession` |
| F70 | `POST /api/client-events` | accepted | — | 202, no body | `TestClientEvents_Accepted_202` |
| F71 | error mapper | any error response | — | `bms.api.errors{method, route, code}` counted; `/healthz` and `/readyz` excluded | `TestMapper_CountsErrorsByCode` |
| F72 | `RouteTokens` | routes mounted | — | tokens equal the span names | `TestRouteTokens_MatchSpanNames` |

