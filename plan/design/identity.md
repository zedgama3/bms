# Identity

- **Status:** Implemented (2026-10-18); Approved (2026-10-18; owner approved every recommended answer)
- **Amended 2026-10-18 (owner approval of authz/people):** Proposed changes 6 and 7 applied: `EmailOf` and `LinkAccount` added, `DeletePerson` refuses a superuser account, the accounts admin screen links to the profile, the starter role `account-admin` holds `accounts.read` and `accounts.manage` (`StarterRoles`); the sign-up `taken` conflict reaches the browser as `fields` (api D31; F15 and the UI note); the per-client-address limiter default is about 100 failures per 15 minutes while the per-account bucket stays at 10 (new `ClientFailureLimit`, decision 12); the two deviations marked pending (ChangePassword sharing the sign-in buckets, IPv6 keyed by /64) are accepted.
- **Amended 2026-10-18 (M1 build, accepted deviations):** implementation deviations recorded in Behavior, "Accepted implementation deviations (M1 build, 2026-10-18)"; `(*Service).CreateSuperuser` added to the Interface; the store interface and the dev-cookie and restricted-session wording corrected to what was built.
- **Amended 2026-10-18 (M2 build, accepted deviations):** `LinkAccount` Conflict codes and the store method `SetPerson` recorded in Behavior, "Accepted implementation deviations (M2 build, 2026-10-18)".
- **Amended 2026-10-19 (D39, owner approval of settings and notify):** the forgotten-password flow (notify Proposed change 1) is specified in full in "Amendment 2026-10-19 (D39): the forgotten-password flow" (before Open questions); `Policy` gains `PasswordResetLifetime`, identity exports `Settings` and `CheckSettings`, and Proposed changes 8 and 9 are applied.
- **Amendment 2026-10-19 (approved, owner, 2026-10-20, D40):** closes the D39 amendment's "Needs a decision" (paragraph removed): `RequestPasswordReset` asks `Notify.Configured` before the account lookup and answers `Unsupported` for every address alike when e-mail is off (notify.md amendment of the same date); F71 and F72 replaced, F91 added. See "Amendment 2026-10-19 (approved, owner, 2026-10-20, D40): reset requests are uniform when e-mail is off".
- **Amended 2026-10-20 (owner, D40; Q-RESET-RATE, Q-RESET-LIFE, Q-WEB-REVIEW):** reset requests have their own limiter buckets (per client address, per submitted address; not sign-in's) and a per-account cooldown between reset e-mails (a request inside it answers the same `202` and sends nothing); the reset e-mail gets the lifetime as `lifetime_minutes` (a number) and each language's template words the unit; the password-reset page's document response carries `Referrer-Policy: no-referrer` (api.md step 4, F60); the reset `Message` passes `TTL: PasswordResetLifetime` (notify amendment). The numbers marked "proposed" below await the owner's confirmation.
- **Accepted 2026-10-20 (owner, D40):** the implementation deviations marked "(implementation, 2026-10-19)" in the forgotten-password amendment are accepted as written.
- **Package:** `internal/core/identity` (service, `Authenticator`, tokens, handlers), `internal/core/identity/account` (the record type), `internal/core/identity/postgres` (store, `sql/`, `migrations/`)   **UI:** `web/src/core/accounts/`
- **Depends on specs:** `model`, `lib-errs`, `lib-core`, `lib-secret`, `config` (through `app`), `db`, `signals`, `settings` (`Decl`, D39), `notify` (`Notifier`, D39), `telemetry`, `api` (the `Authenticator` seam, `httpx`, the cookie rules), `audit` (Approved; the `Recorder`), `web-shell`, `app`   **Used by:** `api` (`Authenticator`), `app` (part 2, after audit; the `create-superuser` command, the audit actor closure), `audit` (`ActorFunc`, through `app`), every module and core service that needs the actor (`PersonFrom`), `web-shell` (the session endpoint, the sign-in flow), later `authz` (the actor and the superuser flag), `people` (`identity.account-created`, export and deletion), `settings` (`PolicySource`), `notify` (`Alerter`)

## Summary

**Purpose.** Say who is making a request: local accounts keyed by e-mail address (D21), Argon2id passwords, signed session tokens in a cookie that are checked without a database write, the per-request `Authenticator` the api shell calls, and the break-glass superuser created only by command (D23). Package `internal/core/identity`; imports `model`, `lib` (`errs`, `clock`, `validate`, `secret`, `httpx`), `core/db`, `core/audit`, `core/signals`, `core/telemetry`, `core/authz` (leaf types only). Never `core/module`, `api`, `app`, `config` or a module. `authz` and `audit` never import identity: they receive the actor through a closure `app` writes over `SessionFrom` (decision 6).

**Names.** `identity` is the package, the core part, the migration set and the audit area (what it is); `accounts` is the route set (`/api/accounts/`), the permission prefix, the settings prefix and the screen set (what people see).

**Exported (`identity`).**
- `Session{ID SessionID, Account model.AccountID, Person model.PersonID, Superuser, PasswordChangeRequired bool, IssuedAt, ExpiresAt time.Time}` (+`Validate`); `SessionFrom(ctx) (Session, bool)`; `PersonFrom(ctx) (model.PersonID, bool)` (false with no session, a restricted session or inside `signals.InDelivery`); `WithSession(ctx, Session) context.Context` (tests and `app`'s end-to-end test only). The session value is request-scoped: never stored, never copied into a job or a handler.
- `Account{ID, Person, Email model.Email, Superuser, PasswordChangeRequired bool, DisabledAt, LastLoginAt, PasswordChangedAt, CreatedAt, UpdatedAt time.Time}` (+`Validate`, `LogValue` emitting IDs only); `AccountQuery{Email model.Email, After Cursor, Limit int}`, `Cursor`, `AccountPage{Accounts, Next}`; `Credentials{Email model.Email, Password string}`; `PasswordChange{Current, New string}`; `SessionInfo{SignedIn, PersonID, AccountID, PlayaName, Superuser, PasswordChangeRequired bool, ExpiresAt, SignupOpen bool, Permissions []string}` (the wire shape of `GET /session`); `Issued{Session, token}` (returned by the use cases that start a session; the handlers turn it into the cookie); `SuperuserResult{Account model.AccountID, Person model.PersonID, Created bool, Password secret.Secret}`.
- `Policy{SignupMode, SessionLifetime, SuperuserSessionLifetime, PasswordMinLength, LoginFailureLimit, ClientFailureLimit, LoginFailureWindow, PasswordResetLifetime}` (+`Validate`), `DefaultPolicy()`, `SignupOpen|SignupInvitation`; `Settings []settings.Decl` (the eight `accounts.*` keys with their ranges as `Check`s) and `CheckSettings(map[string]string) error` (the cross-field rule `client_failure_limit >= login_failure_limit`); `PolicySource` is implemented by `app` over a scoped settings `Reader` (D39).
- Seams (interfaces identity declares, filled later by `app`; nil means the M1 behaviour in brackets): `PolicySource{Policy(ctx) (Policy, error)}` (settings, M2 [`DefaultPolicy`]); `PermissionChecker{Allow(ctx, authz.Permission) error}` (authz, M2 [deny; the superuser is allowed before the seam is asked]); `PermissionLister{PermissionsOf(ctx, model.PersonID) ([]string, error)}` (authz, M2 [empty]); `NameSource{PlayaNameOf(ctx, model.PersonID) (string, error)}` (people, M2 [""]); `Alerter{SuperuserSignedIn(ctx, model.PersonID) error}` (notify, M4 [one warn log line]); `Notify notify.Notifier` (scoped `identity`, D39 [nil: the reset routes answer `Unsupported`]).
- `Options{DB, Clock, Logger, Tracer, Meter, Key secret.Secret, AllowPlaintext bool, Audit audit.Recorder, Signals signals.Bus, Policy, Permissions, PermissionLister, Names, Alerter, Notify notify.Notifier}` (+`Validate`); `New(Options) (*Service, error)` (no I/O; works against `db.None()`).
- `*Service` (safe for concurrent use, nothing to close): `Authenticate(ctx, *http.Request) (context.Context, model.PersonID, error)` (= `api.Authenticator`); `SessionInfo(ctx) (SessionInfo, error)`; `SignUp(ctx, Credentials) (Issued, error)`; `SignIn(ctx, Credentials) (Issued, error)`; `SignOut(ctx) error`; `ChangePassword(ctx, PasswordChange) (Issued, error)`; `EndOtherSessions(ctx) (Issued, error)`; `ListAccounts(ctx, AccountQuery) (AccountPage, error)`; `GetAccount(ctx, model.AccountID) (Account, error)`; `DisableAccount(ctx, model.AccountID) error`; `EnableAccount(ctx, model.AccountID) error`; `SetTemporaryPassword(ctx, model.AccountID) (secret.Secret, error)`; `RequestPasswordReset(ctx, model.Email) error`; `CompletePasswordReset(ctx, token, newPassword string) error` (D39); `EnsureSuperuser(ctx, model.Email) (SuperuserResult, error)`; `CreateSuperuser(ctx, out io.Writer, email string) error` (app's command; prints the spec's lines, never the address); `ExportPerson(ctx, model.PersonID) (json.RawMessage, error)`; `DeletePerson(ctx, model.PersonID) (int, error)` (`Permission` for a superuser account); `EmailOf(ctx, model.PersonID) (model.Email, error)`; `LinkAccount(ctx, model.Email, model.PersonID) (model.AccountID, model.PersonID, error)`; `Routes() []Route`; `OpenAPI() []byte`. `Route{Method, Pattern string, Public bool, Handler http.Handler}` (`app` converts to `module.Route`, as for audit).
- Constants: `CookieName = "__Host-bms_session"`, `DevCookieName = "bms_session_dev"`, `TokenVersion = 1`, `MaxPasswordBytes = 256`, `PasswordMinLengthFloor = 12`, `GeneratedPasswordLength = 24`, `HashConcurrency = 4`, `MaxTrackedKeys = 10000`, `MaxPageSize = 100`, `DefaultPageSize = 50`, `ClockSkew = 5 * time.Minute`, `PasswordResetTokenBytes = 32` (D39).
- `PermissionAccountsRead = "accounts.read"`, `PermissionAccountsManage = "accounts.manage"` (both `ScopeSite`, reach `personal`); `Permissions []authz.PermissionDecl`. `SignalAccountCreated = "identity.account-created"` (payload `account`, `person`). Audit actions `identity.login`, `identity.login-failed`, `identity.logout`, `identity.account.created`, `.read`, `.disabled`, `.enabled`, `.deleted`, `identity.account.linked`, `identity.password.changed`, `identity.password.temporary-set`, `identity.password.reset-requested`, `identity.password.reset`, `identity.sessions.ended`, `identity.superuser.created`, `identity.superuser.reset`, `identity.superuser.login`. Settings `accounts.signup`, `accounts.session_lifetime`, `accounts.superuser_session_lifetime`, `accounts.password_min_length`, `accounts.login_failure_limit`, `accounts.client_failure_limit`, `accounts.login_failure_window`, `accounts.password_reset_lifetime`. Templates `Templates fs.FS` (`identity.password-reset`, `identity.superuser-signed-in`; `en`, `es`). `postgres.Migrations fs.FS`; `Set = db.Set{Name: "identity", Schema: "core"}`. Command `bms create-superuser <email>`.

**Key decisions.** 1 one account table, no provider table yet: providers (OIDC, trusted header) are a later column and a later spec, not a code path now. 2 the token is stateless (HMAC-SHA256 over version, account, session ID, generation, issued, expires; key from HKDF over `Session.Key`); every authenticated request reads one account row (state, never a write), so sessions work on a read-only copy, and revocation is the per-account `session_generation` (09 §2). 3 no sliding renewal: fixed lifetime (30 days; superuser 1 hour), a new token on every password change and "sign out everywhere else"; listing a person's other sessions is Later, best effort (it needs a row per session, which a read-only copy cannot write) (owner, 2026-10-18). 4 the login error never says whether the address exists: unknown address and disabled account both answer `Unauthenticated`, a dummy hash is verified when nothing matched, and the limiter keys on the submitted address, not on whether an account exists; a disabled account with the right password gets the same 401 as a wrong one, and the `Denied` audit entry lets an admin see it (owner, 2026-10-18). 5 Argon2id in PHC string form (m=19456 KiB, t=2, p=1), at most `HashConcurrency` hashes at once; parameters live in the hash so they can be raised with rehash-on-login. 6 `authz` and `audit` get the actor from `app` closures over `SessionFrom`, never by importing identity (no import cycle through `core/authz`'s leaf types). 7 the superuser is an account with `superuser = true`: created or reset by `bms create-superuser <email>` with a generated password printed once, never promoted from a participant account (`Conflict`); several superuser accounts are allowed, each created by its own command and listed on the accounts screen (owner, 2026-10-18); never changed through the API (`Permission`), short sessions, `identity.superuser.login` plus an alert, allowed before any permission seam is asked. 8 identity mints the `PersonID` at sign-up and publishes `identity.account-created {account, person}`; the profile is `people`'s (M2); M1 sign-up asks for e-mail and password only; the header shows a neutral label until the person fills in their profile in M2 (owner, 2026-10-18). 9 a restricted session (`password_change_required`) is signed in to identity's own routes only: `Authenticate` returns the zero person, so every other signed-in route answers 401 and the shell sends the person to the password screen. 10 sign-out clears the cookie before it records `identity.logout`, so a failed audit write still leaves the browser signed out while the response is the audit error; this is an explicit exception to audit decision 3 ("no audit, no action"), recorded in both specs (owner, 2026-10-18). 11 disable, enable and `create-superuser` are idempotent. 12 rate limits are in memory per process, two buckets (client address; submitted canonical address), bounded by `MaxTrackedKeys`, answering `Unavailable` wrapping `httpx.ErrRateLimited` (shared sentinel, Proposed change 1); by default about 100 failures per fifteen minutes per client address (`ClientFailureLimit`: many people share one address at a venue) and ten per fifteen minutes per submitted address (`LoginFailureLimit`), in memory per process, so an on-site and an off-site copy count separately (owner, 2026-10-18; the client-address default raised the same day). 13 the common-password list is embedded (SecLists top 100 000, MIT), checked exactly; a regional cannot change it in M1 (owner, 2026-10-18). 14 regional policy (sign-up mode, lifetimes, minimum length, limits) is the `Policy` read through `PolicySource`, defaults in M1. 15 sign-up answers `409` with field `email` code `taken` (no e-mail verification exists yet; rate-limited per address; revisit with e-mail verification in M4) (owner, 2026-10-18). 16 the e-mail is stored as entered and unique on `lower(email)` (model.md decision 11). 17 identity owns the login address (one row, one uniqueness rule); `people` shows and edits it through an identity method (`ChangeEmail`, M4, with confirmation) and holds a contact address only for people without an account (owner, 2026-10-18). 18 temporary passwords ship in M1 (one method, one button): without e-mail reset until M4 they are the only way to help a locked-out participant (owner, 2026-10-18). 19 (D39) forgotten password is an e-mailed single-use link: `RequestPasswordReset` answers the same whatever the address, `CompletePasswordReset` redeems the token and ends every session; the token is 32 random bytes and only its SHA-256 is stored. 20 no automatic sign-in after a reset: no cookie is set from a public route, and the person is sent to the sign-in page (owner, 2026-10-19). 21 a reset token lasts 1 hour by default (`accounts.password_reset_lifetime`, 10 minutes to 24 hours), is single use, and only the newest token for an account is valid (owner, 2026-10-19). 22 (owner, 2026-10-20, D40) reset requests are limited apart from sign-in: own per-client-address and per-submitted-address buckets (proposed `ResetClientLimit` 20, `ResetAddressLimit` 5 per `LoginFailureWindow`) and a per-account cooldown (proposed `PasswordResetCooldown` 5 minutes) inside which a request answers the same `202` and sends nothing; `ChangePassword` still shares sign-in's buckets (D36(i)); the e-mail receives `lifetime_minutes`, and the template words the unit.

**Errs kinds callers handle.** `Authenticate`: `(ctx, zero, nil)` for no session; `Unauthenticated` for a malformed, expired, revoked, disabled or unknown session; store kinds (`Unavailable`, `Timeout`, `Canceled`) otherwise; never a nil ctx. `SignIn`: `Invalid` (body), `Unavailable` wrapping `httpx.ErrRateLimited` (429), `Unauthenticated` (wrong, unknown or disabled), `Unavailable` (store or audit), `Timeout`/`Canceled`. `SignUp`: `Unsupported` (invitation mode), `Invalid` (field codes `required`, `malformed`, `too_short`, `too_long`, `common`), `Conflict` (field `email`, code `taken`, emitted as `fields` by the api mapper), `Unavailable` wrapping `db.ErrReadOnly`, limiter 429. `ChangePassword`/`EndOtherSessions`/`SignOut`: `Unauthenticated`, `Invalid` (`current_password` code `incorrect`), `Unavailable` wrapping `db.ErrReadOnly` (not for sign-out). Admin methods: `Unauthenticated`, `Permission` (no permission; the superuser as target), `NotFound` (unknown ID), `Invalid` (query, cursor, own account: field `account` code `self`), `Unavailable` wrapping `db.ErrReadOnly`. `EnsureSuperuser`: `Invalid` (address), `Conflict` (address belongs to a participant account), `Unavailable` wrapping `db.ErrReadOnly`. `ExportPerson`/`DeletePerson`: `Invalid` (zero ID), `Permission` (`DeletePerson` of a superuser account), store kinds. `EmailOf`: `NotFound`. `LinkAccount`: `NotFound` (`email` `unknown`), `Conflict` (`superuser`, `has_account`). `RequestPasswordReset`: `Invalid` (`email`: `required`, `malformed`), `Unavailable` wrapping `httpx.ErrRateLimited`, `Unsupported` (no `Notify`), `Unavailable` (store, audit, `Notify.Send` other than `Unsupported`), `Unavailable` wrapping `db.ErrReadOnly`; nil for an unknown, disabled or superuser address. `CompletePasswordReset`: `Invalid` (`token`: `invalid`, `expired`; `new_password`: `too_short`, `too_long`, `common`), `Unavailable` wrapping `db.ErrReadOnly`, store and audit kinds. `New`/`Options.Validate`: `Invalid` with fields `identity.db`, `identity.clock`, `identity.logger`, `identity.tracer`, `identity.audit`, `identity.signals`, `session.key`. No sentinels of its own.

**Owns.** Tables `core.account` (`email` personal, `password_hash` sensitive-credential, `last_login_at` and `disabled_at` personal; rows live until the person is deleted) and `core.password_reset` (`token_hash` sensitive-credential; D39). Permissions `accounts.read`, `accounts.manage`. Signal `identity.account-created`. Settings `accounts.*` (seven keys above; defaults until `settings`). Route set `accounts`: `GET /session`, `POST /sign-up`, `POST /sign-in`, `POST /sign-out`, `POST /password` (public; the principal is checked inside), `POST /sessions/end-others`, `GET /` (list), `GET /{account}`, `POST /{account}/disable`, `POST /{account}/enable`, `POST /{account}/temporary-password`, and (D39, public) `POST /password-reset`, `POST /password-reset/complete`. Screens `/accounts/password-reset` (with and without `?token=`), `/accounts/sign-in`, `/accounts/sign-up`, `/accounts/password`, `/accounts/admin`, `/accounts/admin/:account`; message keys `core.accounts.*`. Spans `identity.<use-case>`; metrics `bms.identity.logins{outcome}`, `bms.identity.sessions_rejected{reason}`, `bms.identity.rate_limited{bucket}`. Command `create-superuser`. The cookie: `Secure; HttpOnly; SameSite=Lax; Path=/`, `Max-Age` = lifetime, `__Host-` name; `bms_session_dev` without `Secure` only under api decision 24; never both on one response.

**Expects of dependencies.** `api`: calls `Authenticate` on every routed request, 401 on signed-in routes when the person is zero, `httpx.ClientAddrFrom`/`SchemeFrom`, the cookie guard, `httpx.ErrRateLimited` mapped to 429 (Proposed change 1). `audit`: a `Recorder` scoped to `identity` at build time (audit built before identity, audit.md Proposed change 1), `Record` outside any `Read` transaction, the spool on a read-only copy, an explicit `Actor` honoured. `db`: `Transact`, `AfterCommit`, `Wrap`, `ErrReadOnly`, `*Error.Constraint`, `Set`, `dbtest`. `signals`: a `Bus` scoped to `identity`, `InDelivery`. `config` through `app`: `Session.Key`, `HTTP.AllowPlaintext`. `model`: `AccountID`, `PersonID`, `Email` (`ParseEmail`, `Canonical`, `LogValue`). `app`: part 2 as in app.md's table, the command, the actor closures for audit (M1) and authz (M2), the `accounts` route set, the end-to-end test. `web-shell`: the session store, the `401` rule, `/sign-in` as a redirect to `/accounts/sign-in` (Proposed change 2).

**Open questions:** one small one (D40): the numbers of decision 22 are proposed, not chosen by the owner (see "Open questions"); otherwise none; the nine are settled as decisions 8, 7, 15, 17, 3, 4, 13, 18 and 12 (owner, 2026-10-18); the reset-flow questions as decisions 20 and 21 (owner, 2026-10-19, D39). **Proposed changes elsewhere:** 18, listed at the end (status there). **Settled 2026-10-20 (D40):** `RequestPasswordReset` asks `Notify.Configured` before the account lookup; e-mail off answers `Unsupported` for every address alike (F71, F72, F91); reset requests use their own limiter buckets and a per-account cooldown (decision 22; F70, F92, F93).

**M2 build deviations (2026-10-18).** `LinkAccount` Conflict codes are `superuser` (field `email`) and `has_account` (field `person`); the store gains `SetPerson` (`set_account_person.sql`).

## Purpose

Say who is making a request.

## Responsibilities

- Local accounts: one per person, keyed by the person's primary e-mail address (D21), with sign-up, disable and enable, a temporary password an account admin sets, and the fields an account admin may see (address, created, last sign-in, state).
- Passwords: the policy (minimum length, the common-password list, no composition rules), Argon2id hashing and verification, change by the person.
- Sessions: the signed token, the cookie, `Authenticate` for the api shell, the principal in the request context (`SessionFrom`, `PersonFrom`), "sign out" and "sign out everywhere else" through the per-account generation, short sessions for the superuser.
- Failed-login limits per client address and per submitted address.
- The break-glass superuser (D23): `bms create-superuser <email>`, the flag the audit log and `authz` read, the alert on every sign-in.
- Audit entries for every login, failed login, logout, account change, password change and admin read of an account.
- The signal `identity.account-created`, so `people` (M2) can create the person's profile row.
- The forgotten-password flow (D39): the single-use reset token, `RequestPasswordReset`, `CompletePasswordReset` and their two screens.
- Export and deletion of what identity holds about a person (the account row), for `people`'s per-person export and deletion.
- The screens: sign in, sign up, change password (with "sign out everywhere else"), and the account admin's list and detail screens.

## Not responsible for

| Concern | Who |
|---|---|
| Who the person is beyond the login: playa name, legal name, contacts, consent | `people` (M2). Identity mints the `PersonID` at sign-up and tells `people` through `identity.account-created`; the session's `playa_name` comes from `people` through `NameSource` |
| What the person may do; roles, grants, separation of duties; the starter role "Account admin" | `authz` (M2). Identity checks its own two permissions through `PermissionChecker` and allows the superuser first; `authz`'s own `Allow` does the same with the flag `app` hands it |
| Delivering anything: the reset e-mail, confirming a changed address, the superuser alert | `notify` (M4) through `Notify` (the reset: decision 19) and `Alerter`; identity decides when to send and what the link carries, notify delivers |
| Changing the login address | identity, from M4, once the new address can be confirmed; not in M1 |
| Invitations when `accounts.signup` is `invitation` (who invites, the token, the screen) | a later identity amendment; M1 only refuses open sign-up in that mode |
| Listing a person's sessions on a screen | Later (decision 3); M1 ships "sign out everywhere else" |
| OIDC and the trusted-proxy-header provider (D15), passkeys, magic links, two-factor | later identity specs behind the same `Service`; the table leaves room (decision 1) |
| Sealing, generating, backing up and rotating `Session.Key`; `bms rotate-secret` | `config` (decision 7) and `app` (`secrets.Ensure`); rotation is a later command designed when needed |
| CSRF, the cookie guard, request deadlines, the error body, the route span | `api` |
| Settings storage and the admin screen for `accounts.*` | `settings` (M4); `app` implements `PolicySource` over a scoped `Reader` |
| Retention of the audit trail of logins | `audit` |
| The read-only banner, the superuser banner, the `401` redirect | `web-shell` |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| Accounts | Keep one account per person with its state. | `SignUp`, `ListAccounts`, `GetAccount`, `DisableAccount`, `EnableAccount`, `SetTemporaryPassword`, `ExportPerson`, `DeletePerson`; the table |
| Passwords | Decide whether a password is acceptable, hash it and verify it. | `password.go`: policy check, Argon2id, the embedded list, the hash semaphore, the dummy hash |
| Sessions | Turn a valid login into a token and a token into a principal, without writing. | `token.go` (sign, verify), `cookie.go` (read, write, clear), `Authenticate`, `SignIn`, `SignOut`, `ChangePassword`, `EndOtherSessions`, `SessionFrom`, `PersonFrom` |
| Login limiter | Slow down guessing by address and by submitted address. | `limiter.go`: two bounded maps for sign-in, and (D40) a second pair of the same type for password-reset requests, a fake-clock-friendly sweep on access |
| Superuser | Create or reset the break-glass account by command and mark its sessions. | `EnsureSuperuser`, the `create-superuser` command (in `app`), the short lifetime, the alert |
| Accounts screens | Let a person sign in, sign up and change their password, and an account admin manage accounts, on a phone. | `web/src/core/accounts/` |

## Interface

```go
package identity // internal/core/identity
//
// Imports: model; lib/errs, lib/clock, lib/validate, lib/secret, lib/httpx; core/db;
// core/audit; core/signals; core/telemetry; core/authz (Permission, PermissionDecl: leaf
// types). Never core/module, api, app, config or a module. Neither authz nor audit may
// import identity (decision 6).

const (
	CookieName             = "__Host-bms_session" // api decision 7
	DevCookieName          = "bms_session_dev"    // api decision 24
	TokenVersion           = 1
	MaxPasswordBytes       = 256 // a longer password is Invalid (too_long); Argon2 has no 72-byte limit
	PasswordMinLengthFloor = 12  // Policy.PasswordMinLength may not go below this (09 §2)
	GeneratedPasswordLength = 24 // create-superuser and temporary passwords; alphabet "abcdefghjkmnpqrstuvwxyz23456789"
	HashConcurrency        = 4   // Argon2id hashes in flight per process; more wait on the ctx
	MaxTrackedKeys         = 10000 // per limiter bucket; the oldest key is evicted
	MaxPageSize            = 100
	DefaultPageSize        = 50
	ClockSkew              = 5 * time.Minute // a token issued further in the future is malformed
	// Password-reset requests (D40; the numbers are PROPOSED, owner to confirm):
	ResetClientLimit      = 20              // requests per client address per Policy.LoginFailureWindow (own bucket)
	ResetAddressLimit     = 5               // requests per canonical submitted address per window (own bucket)
	PasswordResetCooldown = 5 * time.Minute // minimum gap between reset e-mails to one account
)

// ---- The principal ----

type session struct{}
// SessionID identifies one issued token; it is in the token and in the audit entries
// identity.login, identity.logout and identity.sessions.ended (Record kind "identity.session").
type SessionID = model.ID[session]

// Session is the principal a request carries once Authenticate accepted its token. It is
// request-scoped: it lives in the request ctx only, is never stored, and is never the actor
// inside a signal delivery or a job (signals decision 7).
type Session struct {
	ID                     SessionID
	Account                model.AccountID
	Person                 model.PersonID
	Superuser              bool // D23: the audit log marks it; authz allows everything
	PasswordChangeRequired bool // a temporary password: only identity's own routes see a person (decision 9)
	IssuedAt, ExpiresAt    time.Time
}

// Validate fails errs.Invalid: zero ID, Account or Person; ExpiresAt not after IssuedAt.
func (s Session) Validate() error

// SessionFrom returns the session in ctx. Consumers: identity's handlers; app's closures
// for audit.ActorFunc (M1) and authz's actor option (M2). Cannot fail; ok is false without
// a session. Returns the session even when PasswordChangeRequired is set.
func SessionFrom(ctx context.Context) (Session, bool)

// PersonFrom is what every service and module reads the actor with (hello's actorFunc,
// M1). ok is false when there is no session, when the session is restricted
// (PasswordChangeRequired) or when signals.InDelivery(ctx) is true: a handler runs as the
// system, never as the person whose request published the signal. Cannot fail.
func PersonFrom(ctx context.Context) (model.PersonID, bool)

// WithSession puts s in ctx. Consumers: tests of any service that reads PersonFrom, and
// app's end-to-end test. Production code never calls it: Authenticate is the only writer.
// Cannot fail (an invalid s is a test bug; PersonFrom still returns it).
func WithSession(ctx context.Context, s Session) context.Context

// ---- Values ----

// Account is what an account admin (accounts.read) or the person sees. Never the hash.
type Account struct {
	ID                     model.AccountID
	Person                 model.PersonID
	Email                  model.Email // personal; as entered
	Superuser              bool
	PasswordChangeRequired bool
	DisabledAt             time.Time // zero when enabled; personal
	LastLoginAt            time.Time // zero when never; personal; updated only on a writing copy
	PasswordChangedAt      time.Time
	CreatedAt, UpdatedAt   time.Time
}

// Validate fails errs.Invalid (fields "id", "person", "email", "created_at"...).
func (a Account) Validate() error
// LogValue renders IDs and booleans only, never Email (model.md decision 13).
func (a Account) LogValue() slog.Value

// Credentials is the body of POST /sign-in and POST /sign-up.
type Credentials struct {
	Email    model.Email `json:"email"`
	Password string      `json:"password"`
}

// PasswordChange is the body of POST /password.
type PasswordChange struct {
	Current string `json:"current_password"`
	New     string `json:"new_password"`
}

// AccountQuery selects accounts for the admin list. Email set: exactly that address
// (canonical match; zero or one result; After and Limit ignored). Otherwise newest first,
// keyset-paged on (created_at, id).
type AccountQuery struct {
	Email model.Email
	After Cursor
	Limit int // 0 = DefaultPageSize; at most MaxPageSize
}
func (q AccountQuery) Validate() error // errs.Invalid: "email" (set but invalid), "after", "limit"

// Cursor is an opaque, URL-safe continuation token.
type Cursor string

type AccountPage struct {
	Accounts []Account
	Next     Cursor // "" on the last page
}

// SessionInfo is the body of GET /session (web-shell Proposed change 5, extended).
type SessionInfo struct {
	SignedIn               bool           `json:"signed_in"`
	PersonID               model.PersonID `json:"person_id,omitzero"`
	AccountID              model.AccountID `json:"account_id,omitzero"`
	PlayaName              string         `json:"playa_name,omitempty"` // NameSource; "" in M1
	Superuser              bool           `json:"superuser"`
	PasswordChangeRequired bool           `json:"password_change_required"`
	ExpiresAt              time.Time      `json:"expires_at,omitzero"`
	SignupOpen             bool           `json:"signup_open"` // Policy.SignupMode == SignupOpen; also when signed out
	Permissions            []string       `json:"permissions"` // PermissionLister; [] in M1, never null
}

// Issued is a session the service started. The token is unexported: only the handlers in
// this package turn it into Set-Cookie, and nothing logs it.
type Issued struct {
	Session Session
	token   string
}

// SuperuserResult is what create-superuser prints from.
type SuperuserResult struct {
	Account  model.AccountID
	Person   model.PersonID
	Created  bool          // false: an existing superuser account was reset
	Password secret.Secret // shown once by the command; never logged
}

// ---- Policy (regional settings, M2) ----

type SignupMode string

const (
	SignupOpen       SignupMode = "open"
	SignupInvitation SignupMode = "invitation"
)

// Policy is what a regional may change. settings (M2) maps the accounts.* keys onto it.
type Policy struct {
	SignupMode               SignupMode    // accounts.signup
	SessionLifetime          time.Duration // accounts.session_lifetime
	SuperuserSessionLifetime time.Duration // accounts.superuser_session_lifetime
	PasswordMinLength        int           // accounts.password_min_length (characters, counted as runes)
	LoginFailureLimit        int           // accounts.login_failure_limit (per submitted address; default 10)
	ClientFailureLimit       int           // accounts.client_failure_limit (per client address; default 100)
	LoginFailureWindow       time.Duration // accounts.login_failure_window
}

// DefaultPolicy returns the shipped defaults (Settings table). Cannot fail.
func DefaultPolicy() Policy
// Validate fails errs.Invalid with a validate.FieldError per key out of its range.
func (p Policy) Validate() error

// PolicySource returns the current policy. settings implements it (M2); nil means
// DefaultPolicy. A source that fails or returns an invalid Policy fails the use case with
// its kind (F62): identity never falls back silently to the defaults once a source exists.
type PolicySource interface {
	Policy(ctx context.Context) (Policy, error)
}

// PermissionChecker decides one site-wide permission for the actor in ctx (the shape of
// api.PermissionChecker). authz implements it (M2); nil denies with errs.Permission. Identity
// allows the superuser before asking it (decision 7).
type PermissionChecker interface {
	Allow(ctx context.Context, p authz.Permission) error
}

// PermissionLister fills SessionInfo.Permissions (09 §4: once per page load). authz
// implements it (M2); nil means an empty list.
type PermissionLister interface {
	PermissionsOf(ctx context.Context, person model.PersonID) ([]string, error)
}

// NameSource fills SessionInfo.PlayaName. people implements it (M2); nil means "".
// A NotFound from it means "" (a person row not yet created); any other kind fails
// SessionInfo (F8).
type NameSource interface {
	PlayaNameOf(ctx context.Context, person model.PersonID) (string, error)
}

// Alerter tells the maintainers and the site admins that the superuser signed in (D23).
// notify implements it (M4); nil means one warn log line with the person ID. An Alerter
// failure is logged and counted, never fails the sign-in (F27): the audit entry is the
// record of the event.
type Alerter interface {
	SuperuserSignedIn(ctx context.Context, person model.PersonID) error
}

// ---- The service ----

type Options struct {
	DB             db.DB
	Clock          clock.Clock
	Logger         *slog.Logger
	Tracer         telemetry.Tracer
	Meter          telemetry.Meter // optional
	Key            secret.Secret   // config Session.Key; 32..1024 bytes (config validated it); required
	AllowPlaintext bool            // config HTTP.AllowPlaintext: may write DevCookieName (api decision 24)
	Audit          audit.Recorder  // scoped to "identity"; required
	Signals        signals.Bus     // scoped to "identity", publishes SignalAccountCreated; required
	Policy         PolicySource    // optional
	Permissions    PermissionChecker
	Lister         PermissionLister
	Names          NameSource
	Alerter        Alerter
}

// Validate fails errs.Invalid joining a validate.FieldError per problem: "identity.db",
// "identity.clock", "identity.logger", "identity.tracer", "identity.audit",
// "identity.signals" (nil) and "session.key" (unset).
func (o Options) Validate() error

// New validates o, derives the signing key (HKDF-SHA256, info "bms identity session v1"),
// loads the embedded common-password list and prepares the dummy hash. No I/O: it works
// against db.None() (app decision 7) and on a read-only copy. Fails errs.Invalid (F1).
func New(o Options) (*Service, error)

// Service is identity. Safe for concurrent use; nothing to close (no goroutines).
type Service struct{ /* unexported */ }

// Authenticate implements api.Authenticator. It reads CookieName, or DevCookieName when
// AllowPlaintext is set and httpx.SchemeFrom is "http", verifies the token, reads the
// account's state (one statement, no transaction, no write) and returns the ctx with the
// Session, the person, and nil. No cookie: (ctx, zero, nil). Restricted session: the
// Session is in ctx but the person is zero (decision 9). Fails errs.Unauthenticated for a
// malformed, tampered, expired, revoked (generation), disabled or deleted session (F2-F6);
// the store's kind otherwise (F7). Never returns a nil ctx. Consumer: api.
func (s *Service) Authenticate(ctx context.Context, r *http.Request) (context.Context, model.PersonID, error)

// SessionInfo is GET /session: SignedIn false (with SignupOpen) when ctx has no session,
// else the session with PlayaName (Names) and Permissions (Lister). Works on a read-only
// copy; nothing is audited (the person reads their own state). Fails with Names'/Lister's/
// Policy's kind (F8). Consumer: the web shell at boot and after sign-in/out.
func (s *Service) SessionInfo(ctx context.Context) (SessionInfo, error)

// SignUp creates an account and signs it in. Steps: policy (Unsupported when invitation,
// F9); validate (F10-F13); limiter by address (F14); Transact Write: insert (Conflict with
// field email/taken, F15), publish identity.account-created {account, person}, record
// {identity.account.created, Succeeded, Actor: the new person, Subject: person, Record:
// account}; then issue a session (generation 0). Fails Unavailable wrapping db.ErrReadOnly
// on a read-only copy (F16), store kinds, audit kinds. Consumer: POST /sign-up.
func (s *Service) SignUp(ctx context.Context, c Credentials) (Issued, error)

// SignIn verifies c and issues a session. Steps: validate (F17); limiter by address and by
// canonical address (F18); load by canonical address; verify the password (the dummy hash
// when none matched, F19); a disabled account answers like a wrong password (F20, Outcome
// Denied in the audit entry); on success: TouchLogin (last_login_at; db.ErrReadOnly is
// ignored, F21), record {identity.login | identity.superuser.login, Succeeded, Actor: the
// person explicitly, Subject: person, Record: session}, alert when superuser, issue a
// token (superuser: Policy.SuperuserSessionLifetime). A failure records
// {identity.login-failed, Failed|Denied, ActorAnonymous, Record: the account when one
// matched, zero otherwise} and counts in both buckets. Never the address anywhere.
// Fails Unauthenticated (F19, F20), Unavailable wrapping httpx.ErrRateLimited (F18),
// store kinds (F22), audit kinds (F23). Consumer: POST /sign-in.
func (s *Service) SignIn(ctx context.Context, c Credentials) (Issued, error)

// SignOut records {identity.logout, Succeeded, Actor: the session's person, Record:
// session} for the session in ctx (restricted included). No session: nil, nothing
// recorded. The handler clears the cookie before calling it (decision 10). Works on a
// read-only copy (the entry spools). Fails with the audit kind (F24). Consumer: POST /sign-out.
func (s *Service) SignOut(ctx context.Context) error

// ChangePassword needs a Session (restricted allowed; Unauthenticated otherwise, F25),
// verifies Current (Invalid, field current_password, code incorrect, F26), checks New
// (F11-F13), then in one Write transaction stores the hash, clears
// password_change_required, bumps session_generation, records {identity.password.changed,
// Succeeded, Subject: person, Record: account}, and issues a new session with the new
// generation (every other session ends). Fails Unavailable wrapping db.ErrReadOnly (F28).
// Consumer: POST /password.
func (s *Service) ChangePassword(ctx context.Context, c PasswordChange) (Issued, error)

// EndOtherSessions needs a full Session (PersonFrom; Unauthenticated otherwise, F29),
// bumps session_generation, records {identity.sessions.ended, Succeeded, Record: session},
// and issues a new session. Fails Unavailable wrapping db.ErrReadOnly (F30). Consumer:
// POST /sessions/end-others.
func (s *Service) EndOtherSessions(ctx context.Context) (Issued, error)

// ListAccounts needs accounts.read (F31, F32). Works on a read-only copy. Records
// {identity.account.read, Succeeded, Subject: the account's person, Record: account,
// Fields: ["email", "last_login_at", "disabled_at"], Level: personal} for every account
// returned except the actor's own, in one batch, after loading and before returning (F35).
// Fails Invalid (F33), store kinds (F34). Consumer: GET /.
func (s *Service) ListAccounts(ctx context.Context, q AccountQuery) (AccountPage, error)

// GetAccount is ListAccounts for one ID (F36 NotFound; the read is audited unless it is
// the actor's own account). Consumer: GET /{account}; the admin detail screen.
func (s *Service) GetAccount(ctx context.Context, id model.AccountID) (Account, error)

// DisableAccount needs accounts.manage (F37, F38). Refuses the superuser account
// (Permission, F39) and the actor's own account (Invalid, field account, code self, F40).
// In one Write transaction: disabled_at = now, session_generation + 1, record
// {identity.account.disabled, Succeeded, Subject: person, Record: account}. Already
// disabled: nil, nothing recorded (decision 11). Fails NotFound (F41), Unavailable
// wrapping db.ErrReadOnly (F42). Consumer: POST /{account}/disable.
func (s *Service) DisableAccount(ctx context.Context, id model.AccountID) error

// EnableAccount is the inverse (identity.account.enabled; F37-F42 apply). Consumer: POST /{account}/enable.
func (s *Service) EnableAccount(ctx context.Context, id model.AccountID) error

// SetTemporaryPassword needs accounts.manage; refuses the superuser account and the
// actor's own account (F37-F41). Generates a GeneratedPasswordLength password, stores its
// hash, sets password_change_required, bumps session_generation, records
// {identity.password.temporary-set, Succeeded, Subject: person, Record: account}, and
// returns the password once. Fails Unavailable wrapping db.ErrReadOnly (F42), Internal
// (crypto/rand, F43). Consumer: POST /{account}/temporary-password.
func (s *Service) SetTemporaryPassword(ctx context.Context, id model.AccountID) (secret.Secret, error)

// EnsureSuperuser is the create-superuser command's use case (ActorOperator, D23). Validates
// email (F44). In one Write transaction: no account at that canonical address: insert
// {superuser: true, person: NewID}, publish identity.account-created, record
// {identity.superuser.created, Succeeded, ActorOperator, Record: account}; a superuser
// account exists: new password, disabled_at cleared, session_generation + 1, record
// {identity.superuser.reset}; a participant account exists: Conflict (F45; decision 7).
// Returns the generated password. Fails Unavailable wrapping db.ErrReadOnly (F46),
// Internal (rand, F43). Consumer: app's create-superuser command.
func (s *Service) EnsureSuperuser(ctx context.Context, email model.Email) (SuperuserResult, error)

// CreateSuperuser is what app's create-superuser command calls: it parses email, runs
// EnsureSuperuser and writes the spec's output lines to out. It never prints the address.
func (s *Service) CreateSuperuser(ctx context.Context, out io.Writer, email string) error

// ExportPerson returns {"account": {...}} (Account without the hash) or {"account": null}
// when the person has none. people (M2/M4) calls it through app. Fails Invalid for a zero
// ID (F47), store kinds. Not audited here: people records people.person.exported.
func (s *Service) ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)

// DeletePerson deletes the person's account row (0 or 1) and records
// {identity.account.deleted, Succeeded, Subject: person, Record: account} in the caller's
// Write transaction; its sessions fail at the next request (NotFound → Unauthenticated).
// Idempotent. Fails Invalid (F47), Permission for a superuser account (F68; a superuser
// is never changed through the API, decision 7), Unavailable wrapping db.ErrReadOnly
// (F48). Consumer: people's deletion (M2) through app.
func (s *Service) DeletePerson(ctx context.Context, person model.PersonID) (int, error)

// EmailOf returns the login address of the person's account, for people's display. It
// checks no permission (people's seam; people checks its own). NotFound when the person
// has no account (F64), Invalid for a zero ID, store kinds. Not audited here.
func (s *Service) EmailOf(ctx context.Context, person model.PersonID) (model.Email, error)

// LinkAccount moves the account with this e-mail address to the given person and returns
// the account and its previous person (people deletes the empty previous row). NotFound
// (field email, code unknown; never says more, F65), Conflict superuser (F66), Conflict
// has_account when person already has an account (F67), Invalid for a zero ID, Unavailable
// wrapping db.ErrReadOnly. Runs in the caller's Write transaction when there is one;
// records {identity.account.linked, Succeeded, Subject: person, Record: account}. Publishes
// no signal. Consumer: people's "link an existing person" (M2) through app.
func (s *Service) LinkAccount(ctx context.Context, email model.Email, person model.PersonID) (model.AccountID, model.PersonID, error)

// Route is one HTTP route of the accounts set; app converts it to module.Route (identity
// cannot import core/module), Public → module.AccessPublic.
type Route struct {
	Method  string
	Pattern string // relative to /api/accounts
	Public  bool
	Handler http.Handler
}

// Routes returns the eleven routes (Summary, "Owns"). OpenAPI returns the embedded
// fragment (schemas AccountsSession, AccountsCredentials, AccountsPasswordChange,
// AccountsAccount, AccountsAccountPage, AccountsTemporaryPassword). Cannot fail.
func (s *Service) Routes() []Route
func (s *Service) OpenAPI() []byte

// ---- Declarations app registers ----

const (
	PermissionAccountsRead   authz.Permission = "accounts.read"   // list and view accounts, including the address (personal)
	PermissionAccountsManage authz.Permission = "accounts.manage" // disable, enable, temporary password

	SignalAccountCreated signals.Name = "identity.account-created" // payload keys below
	PayloadAccount = "account" // model.AccountID
	PayloadPerson  = "person"  // model.PersonID
)

var Permissions = []authz.PermissionDecl{
	{Name: PermissionAccountsRead, Scope: authz.ScopeSite, Reaches: model.SensitivityPersonal},
	{Name: PermissionAccountsManage, Scope: authz.ScopeSite, Reaches: model.SensitivityPersonal},
}

// Set is the migration set app adds to coreSets (db.md Proposed change 10).
var Set = db.Set{Name: "identity", Schema: "core", Files: postgres.Migrations}
```

```go
package account // internal/core/identity/account (imports model, lib only)

// Record is the row of core.account as the store and the service share it. The hash is a
// secret.Secret so no log, span or error can print it.
type Record struct {
	ID                     model.AccountID
	Person                 model.PersonID
	Email                  model.Email
	PasswordHash           secret.Secret // PHC string "$argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>"
	Superuser              bool
	PasswordChangeRequired bool
	Generation             int32 // session_generation
	DisabledAt             time.Time
	LastLoginAt            time.Time
	PasswordChangedAt      time.Time
	CreatedAt, UpdatedAt   time.Time
}
func (r Record) Validate() error

// State is the per-request read Authenticate makes.
type State struct {
	Person                 model.PersonID
	Superuser              bool
	PasswordChangeRequired bool
	Generation             int32
	Disabled               bool
}
```

```go
// store is the storage the service declares (D8); postgres.Store implements it. Every
// error is already classified by db.Wrap. A unique violation on account_email_canonical_key
// is Conflict with *db.Error.Constraint set. The per-purpose writes are single statements,
// so a concurrent change cannot be lost to a read-modify-write.
type store interface {
	Insert(ctx context.Context, r account.Record) error
	ByID(ctx context.Context, id model.AccountID) (account.Record, error)       // with the hash: to verify a password
	ByIDNoHash(ctx context.Context, id model.AccountID) (account.Record, error) // without it: the admin reads
	ByCanonicalEmail(ctx context.Context, e model.Email) (account.Record, error)
	ByPerson(ctx context.Context, p model.PersonID) (account.Record, error)
	StateByID(ctx context.Context, id model.AccountID) (account.State, error)
	List(ctx context.Context, after account.Key, limit int) ([]account.Record, error)
	ListByEmail(ctx context.Context, e model.Email) ([]account.Record, error)
	ExportByPerson(ctx context.Context, p model.PersonID) (account.Record, error)
	DeleteByPerson(ctx context.Context, p model.PersonID) (model.AccountID, int, error)
	SetPassword(ctx context.Context, id model.AccountID, hash secret.Secret, at time.Time) (int32, error)
	SetTemporaryPassword(ctx context.Context, id model.AccountID, hash secret.Secret, at time.Time) (int32, error)
	BumpGeneration(ctx context.Context, id model.AccountID, at time.Time) (int32, error)
	Disable(ctx context.Context, id model.AccountID, at time.Time) (bool, error)
	Enable(ctx context.Context, id model.AccountID, at time.Time) (bool, error)
	TouchLogin(ctx context.Context, id model.AccountID, at time.Time) error
	Rehash(ctx context.Context, id model.AccountID, newHash, oldHash secret.Secret) error
	Superuser(ctx context.Context, id model.AccountID, person model.PersonID, e model.Email, hash secret.Secret, at time.Time) (account.Superuser, error)
}
```

```go
package postgres // internal/core/identity/postgres
var Migrations fs.FS // 0001_init.sql
func NewStore(d db.DB) (*Store, error) // Invalid for nil
```

### The token

Bytes, before encoding: `version(1) | account_id(16) | session_id(16) | generation(4, big-endian uint32) | issued_at(8, unix seconds, big-endian int64) | expires_at(8) | mac(32)`; 85 bytes; the cookie value is the unpadded base64url form (114 characters). `mac = HMAC-SHA256(k, bytes[0:53])` with `k = HKDF-SHA256(ikm = Session.Key bytes, salt = nil, info = "bms identity session v1", length 32)`. Verification: exact length, version `1`, constant-time MAC compare, `issued_at <= now + ClockSkew`, `expires_at > now`; then the account's `State`: not disabled, `Generation == generation`. Nothing in the token is secret except the MAC; nothing in it is personal (IDs and times). Changing `Session.Key` invalidates every session (config decision 7).

### The cookie

`Set-Cookie: __Host-bms_session=<token>; Path=/; Secure; HttpOnly; SameSite=Lax; Max-Age=<lifetime seconds>`. Under api decision 24 (AllowPlaintext and `httpx.SchemeFrom` is `http`) the handler writes `bms_session_dev` with the same attributes but `Secure`, and never both. Clearing uses the same name and attributes with `Max-Age=0`. Reading prefers `__Host-bms_session`; `bms_session_dev` is read only when `AllowPlaintext` is set and `httpx.SchemeFrom` is `http`. The handlers call `httpx.Fail` for every error and write nothing else themselves; `Issued.token` never appears in a log, span, error or body.

## Types

Defined here: `Session`, `SessionID`, `Account`, `AccountQuery`, `Cursor`, `AccountPage`, `Credentials`, `PasswordChange`, `SessionInfo`, `Issued`, `SuperuserResult`, `Policy`, `SignupMode`, `Route`, `account.Record`, `account.State`, `account.Key`. Uses `model.AccountID`, `model.PersonID`, `model.Email`, `secret.Secret`, `audit.Entry`/`Actor`/`Ref`, `signals.Name`/`Payload`, `authz.Permission`/`PermissionDecl`.

Field codes identity adds to the shell's vocabulary: `too_short` (params `min`), `too_long` (params `max`), `common`, `taken`, `incorrect`, `self`; plus the existing `required`, `malformed`, `range`.

## Data

Migration set `identity`, schema `core`, `internal/core/identity/postgres/migrations/0001_init.sql`.

**`core.account`** (one row per account; a person has at most one)

| Column | Type | Constraints | Sensitivity | Why | Retention |
|---|---|---|---|---|---|
| `id` | `uuid` | PK | — | | row lifetime |
| `person_id` | `uuid` | NOT NULL, UNIQUE | — | modules refer to the person, never the account (09 ground rules) | row lifetime; stays in audit rows after deletion (audit decision 10) |
| `email` | `text` | NOT NULL, CHECK (octet_length between 3 and 254) | personal | the login name (D21); shown to the person and `accounts.read` | until the person is deleted (`DeletePerson`); no separate retention: an account that exists must be reachable |
| `email_canonical` | `text` | GENERATED ALWAYS AS (lower(email)) STORED, UNIQUE (`account_email_canonical_key`) | personal | uniqueness ignoring case (model.md decision 11) | as `email` |
| `password_hash` | `text` | NOT NULL | sensitive (a credential: read only to verify; never exported, logged or shown) | | replaced on every change; gone with the row |
| `superuser` | `boolean` | NOT NULL DEFAULT false | internal | D23 | row lifetime |
| `password_change_required` | `boolean` | NOT NULL DEFAULT false | internal | temporary passwords | until the next change |
| `session_generation` | `integer` | NOT NULL DEFAULT 0, CHECK (>= 0) | internal | revocation without session rows (09 §2) | row lifetime |
| `disabled_at` | `timestamptz` | NULL | personal (an admin-only fact about the person) | disable and re-enable | row lifetime |
| `last_login_at` | `timestamptz` | NULL | personal | "see when an account last logged in" (09 §2) | row lifetime |
| `password_changed_at` | `timestamptz` | NOT NULL | internal | rehash and policy checks later | row lifetime |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | internal | | row lifetime |

Indexes: `account_email_canonical_key` (unique), `account_person_id_key` (unique), `account_created_at_id_idx` on `(created_at DESC, id DESC)` for the list. `COMMENT ON COLUMN` carries each sensitivity level. No foreign key to a person row until `people` exists (M2 decides whether `people.person` references `core.account.person_id` or the reverse; identity proposes that `people` references nothing here and matches by ID).

Personal data never leaves the table except: `email`, `disabled_at`, `last_login_at` to the person themselves and to `accounts.read` (audited, F35); `ExportPerson`. Nothing here goes to a log, span, metric, signal or error message (`Account.LogValue`, `Email.LogValue`, `secret.Secret`).

Failed-login counters live in memory only (two bounded maps keyed by `netip.Addr` and by canonical address, each a ring of failure times within the window); they are personal data in flight (api.md "client address") and are never logged or persisted.

## Permissions

| Permission | Allows | Scoped by | Default roles (authz, M2) |
|---|---|---|---|
| `accounts.read` | list accounts; view one: address, state, last sign-in, created | site | Account admin |
| `accounts.manage` | disable, enable, set a temporary password | site | Account admin |

Operations and the permission each needs:

| Operation | Permission | Notes |
|---|---|---|
| `Authenticate`, `SessionInfo`, `SignIn`, `SignUp`, `SignOut` | none | public routes; a person acts on their own session |
| `ChangePassword`, `EndOtherSessions` | none (the session itself) | restricted sessions may change their password only |
| `ListAccounts`, `GetAccount` | `accounts.read` | the superuser passes without a grant (D23); every returned record but the actor's own is an audited personal read |
| `DisableAccount`, `EnableAccount`, `SetTemporaryPassword` | `accounts.manage` | never on the superuser account, never on oneself |
| `EnsureSuperuser` | none: the operator on the host (`ActorOperator`) | no route |
| `ExportPerson`, `DeletePerson`, `EmailOf`, `LinkAccount` | `people`'s (`people.data.export`/`.delete`, `people.profiles.manage`, M2), checked by `people` before it calls identity | no route here |

In M1 `Permissions` is nil: `accounts.*` is denied to everyone but the superuser, so the milestone's "disable an account" is done by the superuser, and the admin screens show their forbidden state to anyone else (as `hello` and `audit` answer 403 until authz). A single record the caller may not see answers `Permission` here, not `NotFound`: the action (`accounts.read`) is refused before any ID is looked up, so no `403` confirms that an ID exists.

## Signals

**Published:** `identity.account-created` `{account: AccountID, person: PersonID}`, inside the `Write` transaction of `SignUp` and of `EnsureSuperuser` (when it creates). Consumers: `people` (M2) creates the person's profile row, idempotent on the person ID. In M1 nobody subscribes; the name goes in `Env.CoreSignals`. No signal carries the address or anything personal (signals decision 4).

Not published, on purpose: disable, enable, delete. No consumer is named yet; when one is (for example `volunteers` dropping a disabled person from upcoming shifts), the name is added with it.

**Subscribed:** none.

## Settings

Declared here, read through `PolicySource` (settings, M2); until then `DefaultPolicy()`.

| Key | Type | Default | Valid range | Who can change it |
|---|---|---|---|---|
| `accounts.signup` | enum | `open` | `open`, `invitation` (Q-CORE1, D21) | settings admin |
| `accounts.session_lifetime` | duration | `720h` (30 days) | `1h`..`8760h` | settings admin |
| `accounts.superuser_session_lifetime` | duration | `1h` | `10m`..`24h` | settings admin |
| `accounts.password_min_length` | int | `12` | `12`..`128` (never below `PasswordMinLengthFloor`) | settings admin |
| `accounts.login_failure_limit` | int | `10` | `3`..`100` (failures per submitted address per window) | settings admin |
| `accounts.client_failure_limit` | int | `100` | `10`..`1000` (failures per client address per window; never below `login_failure_limit`) | settings admin |
| `accounts.login_failure_window` | duration | `15m` | `1m`..`24h` | settings admin |
| `accounts.password_reset_lifetime` | duration | `1h` | `10m`..`24h` (D39); declared, with `Policy.PasswordResetLifetime`, together with the forgotten-password flow when notify is built, not in the first settings round (implementation, 2026-10-19) | settings admin |

`Policy.Validate` is the validation error for each (`Invalid`, field = the key). A changed lifetime applies to tokens issued after the change; existing tokens keep their `expires_at`.

Configuration consumed through `app`: `session.key` (`Options.Key`), `http.allow_plaintext` (`Options.AllowPlaintext`). No new catalogue keys in M1.

## UI

Screen set `accounts` (`kind: "core"`, `web/src/core/accounts/`, catalogue `messages/en.json`, keys `core.accounts.*`). Every screen is phone-first (web-shell decision 21), uses `Field`, `Button`, `ErrorSummary`, `useFieldErrors`, the generated client (`shell/identity.ts` is deleted once the routes exist, as its comment says), and `autocomplete` as noted so password managers work (WCAG 3.3.8).

| Route | Who | What is on it | Empty / error / forbidden |
|---|---|---|---|
| `/accounts/sign-in` (public; the shell's `/sign-in` redirects here keeping `?returnTo=`) | signed out | the form the shell has today: e-mail (`autocomplete=username`, `inputMode=email`), password (`current-password`, a show/hide toggle), "Sign in"; a "Create an account" link when `signup_open`; already signed in: `Navigate` to `returnTo` | `401` → "That e-mail address or password isn't right." (never which); `429` → `error.rate_limited` with the retry time; `503 read_only` cannot happen here (sign-in works read-only); other errors through `ErrorSummary` with the reference |
| `/accounts/sign-up` (public) | signed out, when `signup_open` | e-mail, password (`new-password`, the minimum length and "not a common password" stated under the field, a show/hide toggle; no confirm field), "Create account"; success: session refreshed, `Navigate` to `returnTo` or `/` | field errors by `fields[].code` (`too_short {min}`, `too_long {max}`, `common`, `taken` on the e-mail input from the `409`'s `fields`, `malformed`); `501` (invitation mode) → "This regional creates accounts by invitation."; `503 read_only` → the shell's read-only wording ("nothing can be saved right now"); `429` as above |
| `/accounts/password` (signed in, restricted sessions included; nav `main` "Account") | the person | "Change password": current, new (`new-password`), "Change"; after success a toast "Password changed" and, when the session was restricted, `Navigate` to `returnTo` or `/`; a second section "Sign out everywhere else" with one button and a confirmation `Dialog`; a `Banner` "You must choose a new password before you continue." when `password_change_required` | `incorrect` on the current field; `503 read_only` → read-only wording; `401` → the shell's redirect |
| `/accounts/admin` (nav `admin` "Accounts", `access: ["accounts.read"]`) | account admins, the superuser | a search field "E-mail address" (exact match) and, below it, the newest accounts as a list (phone: one card per account; wide: `Table`): address, state (Active, Disabled, Superuser), last sign-in, created; "Load more" keyset paging; tap → detail | empty: "No accounts yet."; `403` → "You don't have permission to see accounts." (`ErrorState`, forbidden); search with no match: "No account has that address." |
| `/accounts/admin/:account` | as above | the account's fields; "Disable" / "Enable" (confirmation `Dialog`; disabled for the superuser account and for oneself, with the reason as text); a "Profile" link to `/people/<person>` (people, M2); "Set a temporary password" (confirmation, then a `Dialog` showing the password once in a `<code>` with a copy button and "The person must change it at their next sign-in."); a toast per action | `404` → `ErrorState` not found; `403` as above; `503 read_only` → actions disabled with the banner's wording |

The frame (web-shell) shows the superuser banner from `superuser`, holds the person on `/accounts/password` for every route except that one while `password_change_required` is true (the server answers 401 on `/accounts/admin`, so identity's other routes are not usable either), and shows a neutral label ("Account") where it would show the playa name when `playa_name` is empty (Proposed change 2). Nothing identity renders includes the token, and no screen stores anything in the browser.

## Behavior

- **Ordering.** A use case that changes a row runs one `Transact(ctx, db.Write, ...)`: the row change, the signal (`Publish` inside), the audit entry (`Record` inside), in that order; commit publishes. Reads that are audited (`ListAccounts`, `GetAccount`) load without a transaction, then `Record`, then return (audit decision 2, 12). `SignIn` runs no transaction: load, verify, `TouchLogin` (one statement), `Record`, alert, issue.
- **Issue after commit.** `Issued` is built only after the transaction committed, so a token is never handed out for a row that rolled back.
- **Concurrency.** `*Service` is safe for concurrent use: the limiter holds a mutex; the hash semaphore is a buffered channel of `HashConcurrency`; a hash waits on `ctx` (so a slow login fails `Timeout`/`Canceled`, never piles up). Two concurrent `DisableAccount` calls both succeed (one changes, one sees it done). Two concurrent `SignUp` with the same address: one `Conflict`.
- **Idempotency.** Disable, enable and `EnsureSuperuser` are idempotent; `SetTemporaryPassword` is not (each call is a new password and a new entry); `SignIn` is not (each call is a login).
- **Retries.** Identity retries nothing. The shell never retries POSTs (web-shell decision 11). The limiter is the only backoff.
- **Read-only copy.** `Authenticate`, `SessionInfo`, `SignIn`, `SignOut`, `ListAccounts`, `GetAccount`, `ExportPerson` work (audit entries spool); `SignIn` skips `last_login_at`. `SignUp`, `ChangePassword`, `EndOtherSessions`, the admin changes, `EnsureSuperuser` and `DeletePerson` fail `Unavailable` wrapping `db.ErrReadOnly` (api maps to `503 read_only`).
- **Lifecycle.** `New` does no I/O; there is no `Start`, no goroutine and no `Close`. The limiter sweeps expired entries on each access, so a fake clock drives it in tests. `app` builds identity in part 2, after audit (audit.md Proposed change 1), sets `a.authenticator`, adds the `accounts` route set, the migration set, the command, `Permissions` to `corePerms` and `SignalAccountCreated` to `coreSignals`.
- **The command.** `bms create-superuser <email>`: loads configuration like `serve` (needs `Session.Key`, app F6), builds the assembly through `check`'s path (so a schema behind exits 3 and a read-only copy exits 1 with "this copy is read-only; run it against the writing copy"), calls `EnsureSuperuser`, prints to stdout exactly: `Superuser account created.` or `Superuser account reset; its sessions have ended.`, then `Password (shown once, not stored anywhere else): <password>`, then `Sign in at /sign-in and keep this password in your password manager.` It never prints the address (the operator typed it). Exit 2 for a missing or invalid argument or a participant address (`Invalid`, `Conflict` with a line telling the operator to use another address), 1 otherwise.
- **Superuser sign-in.** The token's lifetime is `SuperuserSessionLifetime`; the audit action is `identity.superuser.login`; `Alerter.SuperuserSignedIn` is called (nil: `logger.WarnContext(ctx, "superuser signed in", slog.String("person_id", ...))`); the metric counts `outcome=superuser`.
- **Password verification timing.** When no account matches, the service verifies the submitted password against a fixed dummy hash with the same parameters so the response time does not reveal the difference; the limiter still counts the attempt under both keys.
- **Rehash on login.** When a stored hash's parameters are weaker than the current constants, `SignIn` re-hashes and `Rehash`es (compare-and-swap) on a writing copy (ignored on read-only), so parameters can be raised in a later release without a migration.
- **Telemetry.** One `SpanInternal` span per use case, `identity.<use-case>`, attributes `telemetry.Token("outcome", ...)` and `telemetry.ID("account_id", ...)` only; `person_id` is on the request span already. Metrics: `bms.identity.logins{outcome: ok|superuser|bad_credentials|disabled|rate_limited|restricted}`, `bms.identity.sessions_rejected{reason: malformed|expired|revoked|disabled|unknown}`, `bms.identity.rate_limited{bucket: address|email|reset_address|reset_email}`. Never an address, a token or a password.

### Accepted implementation deviations (M1 build, 2026-10-18)

- `(*Service).CreateSuperuser(ctx, out io.Writer, email string) error` is exported for app's command; it prints the spec's lines, never the address.
- The store's `Update(r)` is replaced by per-purpose atomic methods: `SetPassword`, `SetTemporaryPassword`, `BumpGeneration`, `Disable`, `Enable`, `Rehash`, `Superuser`, `ListByEmail`, `ExportByPerson`, `ByIDNoHash`. `DeleteByPerson` returns the account id too.
- Admin reads (`GetAccount`, the manage target) never load the password hash.
- The common-password list file is `xato-net-10-million-passwords-100000.txt` (the same SecLists top 100 000, renamed upstream).
- HKDF uses the standard library `crypto/hkdf`, not `golang.org/x/crypto/hkdf`.
- An extra metric, `bms.identity.alert_failures`.
- Success responses: sign-up 201; sign-in, sign-out, password, end-others, disable and enable 204; temporary-password 200 `{password}` with no-store. The list path is `/api/accounts/`.
- `LogValue` and `String` on `Credentials`, `PasswordChange` and `Issued` (nothing secret reaches a log).
- An empty password gives field code `required`.
- The dev cookie is read only when `AllowPlaintext` is set **and** the scheme is `http`. This resolves the spec's two readings to the stricter one; both places in the spec now say so.
- The limiter reserves atomically: check-and-count under one lock, released on success or on errors that say nothing about the password, kept on a wrong guess. The reservation time is taken under the lock.
- `Authenticate` returns a ctx derived from the caller's ctx.
- The dummy hash is built in `New`. `maxArgonMemory` is 256 MiB. `Record.Validate` runs before `Insert`.
- `ChangePassword`'s current-password check reserves in the same two limiter buckets as sign-in, so wrong guesses there also block sign-in for that window (accepted, owner, 2026-10-18).
- IPv6 client addresses are keyed by /64 (accepted, owner, 2026-10-18).
- UI: the restricted-session gate holds the person on `/accounts/password` for every route except that one. The spec said "outside /accounts/", but the server answers 401 on `/accounts/admin`. The UI paragraph here and web-shell.md are corrected.
- UI: a 409 on sign-up was shown as `taken` on the email field whatever the body contained; now the api mapper emits `fields` for a 409 (api D31) and the sign-up screen maps `fields[].field` and `fields[].code` (`email`/`taken`) like any other field error (owner, 2026-10-18).

**M2 build (2026-10-18):**

- `LinkAccount` Conflict codes are `superuser` (field `email`) and `has_account` (field `person`).
- The store gains `SetPerson` (`set_account_person.sql`).

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `New` | a nil dependency, `Key` unset, an unreadable embedded list | Invalid (`identity.*`, `session.key`); Internal for the list | fix wiring (exit 2 in app) | `TestNew_Table` |
| F2 | `Authenticate` | cookie absent | nil, zero person | serve anonymously | `TestAuthenticate_NoCookie_Anonymous` |
| F3 | `Authenticate` | token malformed: bad base64, wrong length, wrong version, MAC mismatch, issued in the future beyond skew | Unauthenticated | 401 on signed-in routes | `TestAuthenticate_Malformed_Table` (one case per byte region tampered) |
| F4 | `Authenticate` | `expires_at <= now` | Unauthenticated | sign in again | `TestAuthenticate_Expired_Unauthenticated` |
| F5 | `Authenticate` | account's `session_generation` differs; account disabled | Unauthenticated | sign in again | `TestAuthenticate_Revoked_Unauthenticated`, `TestAuthenticate_Disabled_Unauthenticated` |
| F6 | `Authenticate` | account row gone (deleted person) | Unauthenticated | sign in (fails) | `TestAuthenticate_UnknownAccount_Unauthenticated` |
| F7 | `Authenticate` | `StateByID` fails Unavailable/Timeout/Canceled | that kind | api F33 | `TestAuthenticate_StoreUnavailable_PassesKind` |
| F7b | `Authenticate` | `bms_session_dev` present, `AllowPlaintext` false or the scheme not `http` | ignored: treated as no cookie | — | `TestAuthenticate_DevCookieIgnoredWithoutAllowPlaintext` |
| F8 | `SessionInfo` | `Names`, `Lister` or `Policy` fails (not NotFound from Names) | its kind | shell F29 banner | `TestSessionInfo_SeamFailure_PassesKind`, `TestSessionInfo_NameNotFound_EmptyName` |
| F9 | `SignUp` | `Policy.SignupMode` is `invitation` | Unsupported | 501, explain | `TestSignUp_InvitationMode_Unsupported` |
| F10 | `SignUp`, `SignIn` | e-mail empty or fails `ParseEmail` | Invalid (`email`: `required`/`malformed`) | field error | `TestSignUp_BadEmail_Invalid` |
| F11 | `SignUp`, `ChangePassword` | password shorter than `PasswordMinLength` runes | Invalid (`password`/`new_password`: `too_short`, `min`) | field error | `TestPassword_TooShort_Invalid` |
| F12 | same | longer than `MaxPasswordBytes` | Invalid (`too_long`, `max`) | field error | `TestPassword_TooLong_Invalid` |
| F13 | same | in the common list (exact match) | Invalid (`common`) | field error | `TestPassword_Common_Invalid` |
| F14 | `SignUp` | the address bucket is at the limit | Unavailable wrapping `httpx.ErrRateLimited` | 429 | `TestSignUp_RateLimitedByAddress_429` |
| F15 | `SignUp` | canonical address exists | Conflict carrying a `FieldError` (`email`: `taken`); the api answers 409 with `fields` (api D31) | field error on the e-mail input, by `fields[].code` | `TestSignUp_Taken_Conflict` (asserts the `FieldError`; fake store `*db.Error{Constraint: "account_email_canonical_key"}`; integration proves the index) |
| F16 | `SignUp` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | 503 read_only | `TestSignUp_ReadOnly_Unavailable` |
| F17 | `SignIn` | empty password, bad address | Invalid | field error | `TestSignIn_BadBody_Invalid` |
| F18 | `SignIn` | address or e-mail bucket at the limit (checked before verification) | Unavailable wrapping `httpx.ErrRateLimited` | 429, `Retry-After` | `TestSignIn_RateLimited_Table` (per address; per e-mail from two addresses; the limit resets after the window on a fake clock; eviction at `MaxTrackedKeys`) |
| F19 | `SignIn` | unknown address or wrong password | Unauthenticated; entry `identity.login-failed` Failed | "address or password isn't right" | `TestSignIn_Unknown_UnauthenticatedSameAsWrong` (same kind, same timing class: the dummy hash is verified; asserts an entry with zero Record), `TestSignIn_WrongPassword_RecordsFailed` |
| F20 | `SignIn` | correct password, account disabled | Unauthenticated; entry Denied with the account | same message | `TestSignIn_Disabled_UnauthenticatedDenied` |
| F21 | `SignIn` | `TouchLogin` fails `db.ErrReadOnly` | ignored, login succeeds | — | `TestSignIn_ReadOnly_SucceedsWithoutTouch` |
| F22 | `SignIn` | a store call fails otherwise | its kind | 503/504 | `TestSignIn_StoreUnavailable_PassesKind` |
| F23 | `SignIn` | `Record` fails | its kind; no token issued | retry later | `TestSignIn_AuditFails_NoSession` |
| F24 | `SignOut` | `Record` fails | its kind; the handler already cleared the cookie | the shell resets anyway | `TestSignOut_AuditFails_CookieStillCleared` (handler test) |
| F25 | `ChangePassword` | no session | Unauthenticated | sign in | `TestChangePassword_NoSession_Unauthenticated` |
| F26 | `ChangePassword` | current password wrong | Invalid (`current_password`: `incorrect`); no generation change | field error | `TestChangePassword_WrongCurrent_Invalid` |
| F27 | `SignIn` (superuser) | `Alerter` fails | logged, counted; login succeeds | — | `TestSignIn_Superuser_AlerterFailureDoesNotFail` |
| F28 | `ChangePassword` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | 503 | `TestChangePassword_ReadOnly_Unavailable` |
| F29 | `EndOtherSessions` | no session, or a restricted session | Unauthenticated | sign in / change password first | `TestEndOtherSessions_Restricted_Unauthenticated` |
| F30 | `EndOtherSessions` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | 503 | `TestEndOtherSessions_ReadOnly_Unavailable` |
| F31 | `ListAccounts`, `GetAccount` | no full session | Unauthenticated | sign in | `TestListAccounts_NoSession_Unauthenticated` |
| F32 | same | `Permissions` nil or denies; not the superuser | Permission | forbidden state | `TestListAccounts_Denied_Permission`, `TestListAccounts_Superuser_Allowed` |
| F33 | `ListAccounts` | bad cursor, limit out of range, invalid `Email` | Invalid | 400 | `TestListAccounts_BadQuery_Invalid` |
| F34 | same | store fails | its kind | 503 | `TestListAccounts_StoreUnavailable_PassesKind` |
| F35 | same | `Record` of the read entries fails | its kind; nothing returned | retry | `TestListAccounts_AuditFails_ReturnsNothing`, `TestListAccounts_RecordsOneEntryPerOtherAccount` (own account excluded; Fields and Level personal) |
| F36 | `GetAccount` | unknown ID (after the permission check) | NotFound | 404 | `TestGetAccount_Unknown_NotFound` |
| F37 | admin changes | no full session | Unauthenticated | sign in | `TestDisableAccount_NoSession_Unauthenticated` |
| F38 | admin changes | denied | Permission | forbidden | `TestDisableAccount_Denied_Permission` |
| F39 | admin changes | target is the superuser account | Permission | explain: by command only | `TestDisableAccount_Superuser_Permission`, `TestSetTemporaryPassword_Superuser_Permission` |
| F40 | admin changes | target is the actor's own account | Invalid (`account`: `self`) | explain | `TestDisableAccount_Self_Invalid` |
| F41 | admin changes | unknown ID | NotFound | 404 | `TestDisableAccount_Unknown_NotFound` |
| F42 | admin changes | read-only copy | Unavailable wrapping `db.ErrReadOnly` | 503 | `TestDisableAccount_ReadOnly_Unavailable` |
| F43 | `SetTemporaryPassword`, `EnsureSuperuser` | `crypto/rand` fails | Internal | 500 / exit 1 | `TestGeneratePassword_RandFails_Internal` |
| F44 | `EnsureSuperuser` | invalid address | Invalid | exit 2 | `TestEnsureSuperuser_BadEmail_Invalid` |
| F45 | `EnsureSuperuser` | a non-superuser account has the address | Conflict | exit 2, use another address | `TestEnsureSuperuser_ParticipantAddress_Conflict` |
| F46 | `EnsureSuperuser` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | exit 1 | `TestEnsureSuperuser_ReadOnly_Unavailable` |
| F47 | `ExportPerson`, `DeletePerson` | zero person | Invalid | 400 | `TestExportPerson_ZeroID_Invalid` |
| F48 | `DeletePerson` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | 503 | `TestDeletePerson_ReadOnly_Unavailable` |
| F49 | `DisableAccount` | already disabled | nil, no entry | — | `TestDisableAccount_Idempotent` |
| F50 | `EnsureSuperuser` | superuser account exists | reset: new password, re-enabled, generation bumped, `identity.superuser.reset` | — | `TestEnsureSuperuser_Existing_ResetsAndEndsSessions` |
| F51 | handlers | body not JSON / unknown shape / too large | `httpx.ReadJSON`'s kinds | 400/413/415 | `TestHandlers_BadBody_Table` |
| F52 | handlers | path `{account}` not a canonical UUID | Invalid (`account`: `malformed`) | 400 | `TestHandlers_BadAccountID_400` |
| F53 | handlers | a response would carry both cookies, or a cookie without an attribute | never happens: one writer, `TestCookie_Table` asserts attributes; the api cookie guard would answer 500 | — | `TestCookie_Table` (production, dev under both conditions, clear) |
| F54 | `PersonFrom` | inside `signals.InDelivery` | ok false | handler runs as system | `TestPersonFrom_InDelivery_False` |
| F55 | `PersonFrom` | restricted session | ok false | 401 outside identity | `TestPersonFrom_Restricted_False` |
| F56 | `Policy.Validate` | a value outside its range | Invalid (field = key) | settings rejects the change | `TestPolicy_Validate_Table` |
| F57 | `SignUp` | `Publish` fails (undeclared name is a wiring bug) | its kind; transaction rolls back | 500/503 | `TestSignUp_PublishFails_RollsBack` |
| F58 | `SignUp` | `Record` fails inside the transaction | its kind; rolled back; no account | retry | `TestSignUp_AuditFails_RollsBack` |
| F59 | `SignIn` | restricted account (temporary password) signs in | succeeds; `Issued.Session.PasswordChangeRequired` true; `Authenticate` then yields the zero person | shell redirects | `TestSignIn_TemporaryPassword_RestrictedSession` |
| F60 | `hash` | more than `HashConcurrency` in flight and ctx done while waiting | Timeout/Canceled | 504 | `TestHash_WaitsOnSemaphore_CtxDone` |
| F61 | `Authenticate` | the request has an `Authorization` header | ignored (no bearer scheme in M1) | — | `TestAuthenticate_IgnoresAuthorizationHeader` |
| F62 | any | `PolicySource` returns an error or an invalid `Policy` | its kind / Invalid | 503/500 | `TestPolicy_SourceFailure_FailsUseCase` |
| F63 | store (integration) | unique violations, generated column, keyset order, `TouchLogin` on read-only | as `db.Wrap` classifies | — | `TestStore_Integration_Table` (`dbtest.TempDB`) |
| F64 | `EmailOf` | the person has no account | NotFound | people shows no login address | `TestEmailOf_NoAccount_NotFound` |
| F65 | `LinkAccount` | no account has the address | NotFound (`email`: `unknown`) | field error, never more | `TestLinkAccount_UnknownEmail_NotFound` |
| F66 | `LinkAccount` | the account is a superuser's | Conflict (`superuser`) | refuse | `TestLinkAccount_Superuser_Conflict` |
| F67 | `LinkAccount` | the target person already has an account | Conflict (`has_account`) | refuse | `TestLinkAccount_PersonHasAccount_Conflict` |
| F68 | `DeletePerson` | the person's account is a superuser's | Permission | people reports it, deletion rolls back | `TestDeletePerson_Superuser_Permission` |

## Lock-in check

No external service. Libraries: `golang.org/x/crypto/argon2` (BSD-3-Clause), the standard library for HKDF (`crypto/hkdf`), HMAC, base64 and cookies, and PostgreSQL (D8). The embedded password list is SecLists' `xato-net-10-million-passwords-100000.txt` (MIT; the same SecLists top 100 000, renamed upstream), kept under `internal/core/identity/passwords/` with its licence text. Later providers (OIDC) must be implementable against Dex (D13 compose profile `oidc`) or Keycloak, both self-hostable; the trusted-header provider needs no external software.

## Test plan

- **Unit** (`identity`): a hand-written fake `store`, `clock.NewFake`, `audittest.NewFake("identity")`, `signalstest.NewFake("identity", []signals.Name{SignalAccountCreated}, nil)`, `telemetrytest.New(t)`; a fake `PolicySource`, `PermissionChecker` (allow/deny), `PermissionLister`, `NameSource`, `Alerter`. Table tests per failure mode above. Token tests tamper each region and check constant-time behaviour by construction (`hmac.Equal`). Limiter tests advance the fake clock. Every test puts an invented address and password through the service and asserts `!c.Contains(address)` and `!c.Contains(password)` across logs, spans, the fake recorder's entries (`Reason`, `Fields`) and error strings; the capture fails on any violation at cleanup.
- **Handlers** (`httptest`): each route with `httpx.Fail`'s fallback statuses, the cookie attributes (F53), `no-store` on `/session` and `/temporary-password`, `bms_session_dev` under both of decision 24's conditions and refused otherwise, sign-out clears the cookie even when the recorder fails (F24).
- **Permission tests:** F31, F32, F37-F40, F55; the superuser passes with a nil checker; a denying checker is honoured even for `accounts.read` on one's own account (the person is not special on the admin routes).
- **Integration** (`//go:build integration`, `dbtest.TempDB`): `db.Version(postgres.Migrations)`; F63; `Authenticate` makes exactly one statement (the `Querier` tracer) and no write; `SignUp` → `SignIn` → `DisableAccount` → `Authenticate` 401 on a real pool; the read-only path with `SetReadOnly` on the fake and `default_transaction_read_only` on the container.
- **End to end** (in `app`, extending the M0 run): `bms migrate` → `bms create-superuser <invented>` prints the password once and never the address → `POST /api/accounts/sign-up` → `GET /api/accounts/session` is signed in → `GET /api/hello/greeting` is now `403` (actor present, permission denied; app.md item 1083) → `POST /api/accounts/sign-out` → `401`; the superuser signs in, sees `superuser: true`, disables the participant, whose next request is `401`; the audit table holds the entries with the superuser marked; the e2e Playwright run signs up, signs in and changes a password at the phone viewport.
- **Web** (`vitest`, `axe-core`): each screen's states in the UI table; "open redirect refused" moves here with the sign-in screen; the restricted-session redirect; the generated client replaces `shell/identity.ts`.

## Amendment 2026-10-19 (D39): the forgotten-password flow

Source: notify.md Proposed change 1, approved with the owner's answers of 2026-10-19 (D39). It replaces the "later `Reset` seam" this spec once named. Decisions 19 to 21 in the Summary.

### Interface

```go
// Options gains (the Summary line):
//   Notify notify.Notifier // scoped to "identity"; nil means no reset: both routes answer Unsupported

// Policy gains PasswordResetLifetime time.Duration // accounts.password_reset_lifetime
// DefaultPolicy: 1h. Validate: 10m..24h, else Invalid (field "accounts.password_reset_lifetime", code "range").

// Settings declares the eight accounts.* keys for the settings core area "accounts"
// (module-contract style Decl; the seven existing ones unchanged); CheckSettings is the
// area's cross-field Check (client_failure_limit >= login_failure_limit). Pure.
var Settings []settings.Decl
func CheckSettings(values map[string]string) error

// Templates holds password-reset.{en,es}.tmpl and superuser-signed-in.{en,es}.tmpl
// (owner "identity"; app registers it with notify, notify.md Behavior).
var Templates fs.FS

// RequestPasswordReset asks for a reset link for the account with this address.
// Consumers: POST /api/accounts/password-reset. No session, no permission. Order:
//  1. email.Validate (Invalid: email required / malformed);
//  2. the reset limiter's two buckets (client address, canonical submitted address),
//     separate from sign-in's failure buckets (a flood of reset requests never blocks
//     sign-in, and failed sign-ins never block a reset), limits ResetClientLimit and
//     ResetAddressLimit per Policy.LoginFailureWindow, each request counted once whatever
//     the outcome; at the limit: Unavailable wrapping httpx.ErrRateLimited (D40);
//  3. Notify == nil, or Notify.Configured(ctx) is false: Unsupported, before the account
//     lookup and for every address alike; a Configured error fails with its kind, also
//     before the lookup (F91);
//  4. verify one dummy hash, for every address alike, before the lookup (implementation, 2026-10-19);
//     then the account lookup; unknown, disabled or superuser (D23: command only): an
//     audit entry (Outcome Denied) is recorded, nothing is sent, nil returned;
//  5. otherwise, in one Write transaction (the account row locked first, so two requests
//     cannot both pass the cooldown): read the newest core.password_reset.created_at for
//     the account, used or not; if it is later than now - PasswordResetCooldown, record
//     the entry (Outcome Denied, Reason "cooldown"), send nothing, commit nothing else and
//     return nil (the same 202, so nothing is revealed; D40); otherwise purge the
//     account's expired and used tokens,
//     delete its earlier tokens (only the newest is valid), mint PasswordResetTokenBytes
//     random bytes (base64url, no padding), insert sha256(token) with
//     expires_at = now + Policy.PasswordResetLifetime, Notify.Send(identity.password-reset,
//     To: the account's person, Key: "password-reset:" + reset row id,
//     TTL: Policy.PasswordResetLifetime (notify amendment: the queued text expires with
//     the token), Link: "/accounts/password-reset?token=<token>",
//     Vars{"lifetime_minutes": "<N>"}) (D40: a bare whole number of minutes, no unit),
//     Record identity.password.reset-requested (Subject: the account's person).
//     If Send answers Invalid or NotFound (for example a missing person row), the request
//     answers silently (nil, nothing committed) with one warn line (implementation, 2026-10-19).
//     The lifetime is formatted as whole minutes ("60"); each language's template words
//     the unit ("{{.Vars.lifetime_minutes}} minutes", "... minutos"), so Spanish and
//     English plurals and word order are the template's business, not the code's.
// Returns nil in every non-error case; the handler answers 202 with no body. The body
// of the request is {email} (implementation, 2026-10-19).
func (s *Service) RequestPasswordReset(ctx context.Context, email model.Email) error

// CompletePasswordReset redeems a token and sets a new password. Consumers:
// POST /api/accounts/password-reset/complete. No session. Hashes the token and finds the
// row; missing, used or disabled account: Invalid (token: invalid); expired: Invalid
// (token: expired). The new password is checked as ChangePassword checks it (min length,
// MaxPasswordBytes, common list). The body is {token, new_password} (implementation, 2026-10-19). Order (implementation, 2026-10-19):
// a non-locking lookup and the validity checks; hash the new password outside any
// transaction; then one Write transaction that locks the account first, marks the token
// used (the `used_at IS NULL` guard decides a race), then sets password_hash and
// password_changed_at, clears password_change_required, bumps session_generation (every
// session ends) and Records identity.password.reset. Locking the account first avoids a
// lock-order deadlock with RequestPasswordReset. No session is issued and no
// cookie is set (decision 20). The handler answers 204; the shell goes to the sign-in page
// with a "password changed" notice.
func (s *Service) CompletePasswordReset(ctx context.Context, token, newPassword string) error
```

Store additions (identity's `store` interface, `postgres/`): `ReplaceResetToken(ctx, account model.AccountID, hash []byte, expires time.Time) (id uuid.UUID, err error)` (purges used and expired rows of that account, deletes its other rows, inserts; one statement set inside the caller's transaction), `ResetByHash(ctx, hash []byte) (ResetRow, error)` (`NotFound` when absent), `CompleteReset(ctx, id uuid.UUID, account model.AccountID, passwordHash string, now time.Time) error` (sets the password and the revocation, marks `used_at`; `NotFound` if the row was already used, so two concurrent redemptions cannot both win). `ResetState(ctx, account model.AccountID) (account.ResetState, error)` (implementation, 2026-10-20, D40 cooldown: locks the account row and returns `{NewestAt, Disabled, Superuser}`, the newest reset row's `created_at` (zero when none) and the account's flags read under the lock, so an account disabled after the lookup is denied without a send; a separate call because the cooldown check runs before `ReplaceResetToken` deletes anything).

### Data

Migration `0002_password_reset.sql` in `internal/core/identity/postgres/migrations/`.

**`core.password_reset`**

| Column | Type | Constraints | Sensitivity | Why | Retention |
|---|---|---|---|---|---|
| `id` | `uuid` | PK | internal | the idempotency key of the e-mail | row lifetime |
| `account_id` | `uuid` | NOT NULL, FK `core.account(id)` ON DELETE CASCADE | internal | whose reset it is; goes with the account on `DeletePerson` | row lifetime |
| `token_hash` | `bytea` | NOT NULL, UNIQUE | sensitive (a credential: SHA-256 of the token; the token itself is never stored, logged or exported) | redeem by hash | row lifetime |
| `expires_at` | `timestamptz` | NOT NULL | internal | `now + accounts.password_reset_lifetime` | row lifetime |
| `used_at` | `timestamptz` | NULL | internal | a replay answers `invalid` | row lifetime |
| `created_at` | `timestamptz` | NOT NULL | internal | | row lifetime |

Index: `password_reset_account_id_idx` on `(account_id)`. Used and expired rows are removed by the next request for that account (`ReplaceResetToken`) and by the M4 retention job; a deleted person's rows go with the account (cascade). `ExportPerson` does not include the table (a credential, nothing about the person).

### Settings

`accounts.password_reset_lifetime` (duration, default `1h`, range `10m`..`24h`, settings admin); see the Settings table. A changed lifetime applies to tokens minted after the change.

### UI

Added to the `accounts` screen set. The sign-in screen gains a "Forgot your password?" link.

| Route | Who | What is on it | States |
|---|---|---|---|
| `/accounts/password-reset` (public) | signed out | one e-mail field, "Send the link"; on any `202` always "If an account exists for that address, an e-mail is on its way." | `501` (not configured) -> "Password reset by e-mail isn't set up here. Ask an administrator for a temporary password."; `429` -> `error.rate_limited` with the retry time; `503 read_only` -> the shell's read-only wording; field error for `malformed` |
| `/accounts/password-reset?token=...` (public) | signed out | new password and the same again (`new-password`, the minimum length and "not a common password" under the field), "Change password"; on `204` `Navigate` to `/accounts/sign-in` with a "Password changed. Sign in with your new password." notice | `invalid` / `expired` -> an `ErrorState` with a link to ask again; password field errors (`too_short {min}`, `too_long {max}`, `common`); `503 read_only` as above |

The token travels in the query of a page with no outbound links, and the document response for `/accounts/password-reset` carries `Referrer-Policy: no-referrer` (api.md step 4, F60; owner, 2026-10-20, D40), so the URL is never sent as a referrer even if a link is added later; the shell removes it from the address bar after reading it (`history.replaceState`) and sends it only in the body of the complete request; the api logs route patterns, never the query. The keys are `core.accounts.reset.*`.

### Behavior

- **Uniform answers.** `RequestPasswordReset` answers `202` for a known, an unknown, a disabled and a superuser address alike; the audit entry (Outcome `Denied` for the last three, so an admin can see attempts) is the only difference. Timing is equalised because every request verifies one dummy hash right after the `Configured` probe and before the lookup, for every address alike (on a known address no real hash is verified, so a dummy only on the unknown path would be a timing oracle) (implementation, 2026-10-19).
- **Reset limits and cooldown (D40; numbers proposed).** Reset requests are limited in their own buckets (client address `ResetClientLimit`, submitted address `ResetAddressLimit`, per `LoginFailureWindow`), so reset traffic cannot lock a person out of signing in and sign-in failures cannot block a reset; `ChangePassword` still shares sign-in's buckets (D36(i)), this changes only `RequestPasswordReset`. Within `PasswordResetCooldown` of the account's newest reset row a request answers the same `202` as any other, sends nothing and records one `Denied` entry with Reason `cooldown`; the cooldown path does the same hash work as every other path (one dummy hash) and sends nothing; its database work (the account lock and one read) sits between the unknown-address path and the send path, so it opens no gap beyond the existing known-versus-unknown difference, which the hash cost dominates. Only known, enabled, non-superuser accounts have a cooldown to find. These are constants, not settings; they can become `accounts.reset_*` keys if a regional needs them tunable (owner to say).
- **Newest token only.** A new request deletes the account's earlier tokens in the same transaction that inserts the new one, so an older e-mail's link answers `invalid`.
- **Single use.** `CompleteReset` marks `used_at` and fails `NotFound` on a used row; two concurrent redemptions: one succeeds, the other answers `invalid`.
- **After a reset.** `session_generation` is bumped, so every session of the account ends, including a restricted one; `password_change_required` is cleared. No session is issued.
- **Transactionality.** `Send` enqueues on the request's `Write` transaction (notify decision 1): the token row, the outbox row and the audit entry commit or roll back together. A `Send` failure other than `Unsupported` fails the request `Unavailable` with nothing committed; no token exists without its e-mail.
- **Read-only copy.** Both methods fail `Unavailable` wrapping `db.ErrReadOnly` (the routes answer `503 read_only`); the limiter and the dummy-hash path run first so the response time does not tell.
- **Privacy.** No log line, span attribute, metric or error carries the token, the address or the link (`telemetrytest` proves it). The rendered body, which holds the link, is cleared by notify when the row reaches `Sent` or at its `expires_at` (the reset row's `TTL` is the token lifetime), so a failed reset mail keeps its link no longer than the token is valid.
- **Telemetry.** Spans `identity.request-password-reset` and `identity.complete-password-reset`; no metric carries the outcome of a reset request, because it must not distinguish the cases.
- **Also for M4 (not designed here).** `ChangeEmail` with confirmation uses `notify.AddressSender` for the new address and `Notifier` for the old one; e-mail verification at sign-up (decision 15's "revisit") uses the same shape.

### Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F69 | `RequestPasswordReset` | e-mail empty or fails `ParseEmail` | Invalid (`email`: `required` / `malformed`) | field error | `TestRequestPasswordReset_BadEmail_Invalid` |
| F70 | `RequestPasswordReset` | either reset bucket (client address, submitted address) at the limit; sign-in's buckets are not consulted | Unavailable wrapping `httpx.ErrRateLimited` | 429 | `TestRequestPasswordReset_RateLimited_Table` (per client, per address; proposed limits) |
| F71 | `RequestPasswordReset` | `Notify` nil, or `Notify.Configured` is `false` | Unsupported, before the lookup, for a known, an unknown, a disabled and a superuser address alike; the store records no lookup | 501, the screen says it is not set up | `TestRequestPasswordReset_NotConfigured_UnsupportedForAnyAddress` (table over the four addresses; identical error kinds, no store call) |
| F72 | `RequestPasswordReset` | `Configured` was `true` and `Send` answers Unsupported (the settings changed in between) | nil; nothing committed (no token, no message, no entry); one warn log without the address | show the uniform message | `TestRequestPasswordReset_UnconfiguredAfterProbe_SilentNil` |
| F73 | `RequestPasswordReset` | unknown, disabled or superuser address | nil; nothing sent; entry Denied; the dummy hash was verified before the lookup, as for every address | show the uniform message | `TestRequestPasswordReset_UnknownDisabledSuperuser_SilentNil` |
| F74 | `RequestPasswordReset` | a known address | nil; one token row (hash only), one `identity.password-reset` message with the link, `lifetime_minutes` and `TTL` = the lifetime, one entry | show the uniform message | `TestRequestPasswordReset_Known_StoresHashAndSends` |
| F75 | `RequestPasswordReset` | a second request for the account | the earlier token is deleted; only the newest redeems | show the uniform message | `TestRequestPasswordReset_Twice_OnlyNewestValid` |
| F76 | `RequestPasswordReset` | `Notify.Send` fails other than Unsupported | its kind (Unavailable, Timeout, Canceled); no token row | retry later | `TestRequestPasswordReset_SendFails_NothingCommitted` |
| F77 | `RequestPasswordReset` | `Record` fails | its kind; rolled back; no token, no message | retry later | `TestRequestPasswordReset_AuditFails_RollsBack` |
| F78 | `RequestPasswordReset` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | 503 | `TestRequestPasswordReset_ReadOnly_Unavailable` |
| F79 | `CompletePasswordReset` | token missing, unknown or already used | Invalid (`token`: `invalid`) | "ask again" | `TestCompletePasswordReset_UnknownOrUsed_Invalid` |
| F80 | `CompletePasswordReset` | token expired (`expires_at <= now`) | Invalid (`token`: `expired`) | "ask again" | `TestCompletePasswordReset_Expired_Invalid` |
| F81 | `CompletePasswordReset` | the account is disabled | Invalid (`token`: `invalid`) | "ask again" | `TestCompletePasswordReset_Disabled_Invalid` |
| F82 | `CompletePasswordReset` | password too short, too long or common | Invalid (`new_password`: `too_short` / `too_long` / `common`); the token stays valid | field error | `TestCompletePasswordReset_WeakPassword_Invalid` |
| F83 | `CompletePasswordReset` | success | nil; new hash, `password_change_required` cleared, `session_generation` bumped (`Authenticate` of an old token fails), `used_at` set, one entry, no `Issued` | go to sign-in | `TestCompletePasswordReset_Success_EndsSessions` |
| F84 | `CompletePasswordReset` | the same token redeemed twice, or concurrently | the second answers Invalid (`token`: `invalid`) | "ask again" | `TestCompletePasswordReset_Replay_Invalid` |
| F85 | `CompletePasswordReset` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | 503 | `TestCompletePasswordReset_ReadOnly_Unavailable` |
| F86 | `CompletePasswordReset` | `Record` fails | its kind; rolled back; the token stays valid | retry | `TestCompletePasswordReset_AuditFails_RollsBack` |
| F87 | `Policy.Validate` | `PasswordResetLifetime` outside 10m..24h | Invalid (`accounts.password_reset_lifetime`: `range`) | settings refuses the change | `TestPolicy_Validate_ResetLifetime_Table` |
| F88 | `Templates` (at `notify.Register`) | `password-reset` or `superuser-signed-in` lacks `en`, or does not parse | Invalid naming the file (notify F17) | fix the template | `TestTemplates_Register_Parses` |
| F89 | handlers | body not JSON, unknown shape, over the cap | `httpx.ReadJSON`'s kinds | 400/413/415 | `TestResetHandlers_BadBody_Table` |
| F90 | any | a log, span, metric or error would carry the token, the address or the link | never; `telemetrytest` fails the test | - | `TestPasswordReset_NoSecretsInTelemetry` |
| F91 | `RequestPasswordReset` | `Notify.Configured` fails | its kind (`Unavailable`, `Timeout`, `Canceled`), before the lookup, for every address alike | 503/504 | `TestRequestPasswordReset_ProbeFails_ReturnsKindBeforeLookup` |
| F92 | `RequestPasswordReset` | a known address whose newest reset row is younger than `PasswordResetCooldown` | nil, the same `202`; nothing sent, no new token (the earlier link stays valid), one entry Denied (Reason `cooldown`); after the cooldown the next request sends and replaces the token | show the uniform message | `TestRequestPasswordReset_InsideCooldown_SilentNilNothingSent`, `TestRequestPasswordReset_AfterCooldown_Sends` |
| F93 | `RequestPasswordReset`, `SignIn` | reset requests exhaust the reset buckets for an address; sign-in failures exhaust sign-in's buckets for it | the other flow is unaffected: sign-in still works after reset requests are limited, and a reset still works after failed sign-ins | none | `TestResetLimiter_SeparateFromSignIn_Table` |

### Test plan

- **Unit.** The fake store, `notifytest.NewFake("identity")` (`Sent()` after commit, `Fail(err)`), `clock.NewFake`, `audittest.NewFake("identity")`, `telemetrytest.New(t)`: tables for F69 to F90; the token is read from the fake notifier's recorded `Link` and redeemed, never from the store; the stored value is asserted to be 32 bytes of hash and never equal to the token; the `lifetime_minutes` var is the bare number ("60") and the stored `TTL` equals the lifetime; the cooldown and both reset buckets with a fake clock; a fake clock moves past `expires_at` (no sleeping); each test puts an invented address, token and password through the service and asserts `!c.Contains(...)` across logs, spans, entries and error strings.
- **Handlers (`httptest`).** `202` for known, unknown and inside-cooldown addresses with identical bodies and headers; `204` on completion with no `Set-Cookie`; `501`, `429`, `503 read_only` statuses; the query string is absent from the log record.
- **Permission tests.** Both routes are public: a signed-in person and a restricted session may use them; neither route returns a session.
- **Integration (`//go:build integration`, `dbtest.TempDB`).** Migration `0002` applies; the unique `token_hash`; the cascade on account deletion; two concurrent `CompleteReset` calls: one wins; `Send` and the token row roll back together.
- **End to end (`app`, with Mailpit).** Set the SMTP settings, request a reset, read the link from Mailpit, complete it, `Authenticate` with the old cookie is `401`, sign in with the new password.
- **Web (`vitest`, `axe-core`).** Both screens' states; the token leaves the address bar; the sign-in link; focus and error summary.

## Amendment 2026-10-19 (approved, owner, 2026-10-20, D40): reset requests are uniform when e-mail is off

Closes "Needs a decision" in the D39 amendment above, together with notify.md's amendment of the same date (`Notifier.Configured`).

**Decision.** Step 3 of `RequestPasswordReset` becomes: `Notify == nil`, or `Notify.Configured(ctx)` returns `false`: `Unsupported`, before the account lookup. The probe is a settings snapshot read (no SMTP connection, no recipient), so its answer cannot depend on the address. Every address therefore gets the same answer whatever the state of e-mail: `501` for all when it is off, `202` for all when it is on. A `Configured` error fails the request with its kind, still before the lookup, for every address alike (F91). If a settings admin clears the host between the probe and `Send` (a window one request long that only an admin can open), `Send` answers `Unsupported` after the lookup; identity then rolls back, returns nil (`202`), writes no entry and logs one warn line without the address (F72, replaced): uniformity over honesty for that one request, because the alternative is the oracle. The `/accounts/password-reset` screen is unchanged: its `501` state already reads "Password reset by e-mail isn't set up here. Ask an administrator for a temporary password."

Options not taken: always `202` and enqueue (the queued reset link, a one-time credential, would outlive its hour in the outbox, and the person would be told an e-mail is coming when none can be; notify's `Send` refuses rows while unset by its decision 13); a public `email_enabled` flag read by the screen (does not fix the server, which must be uniform regardless of what the client shows).

Interface change in identity: none. `Options.Notify` stays `notify.Notifier`, which now carries `Configured`; the unit tests' `notifytest.NewFake("identity")` gains `SetConfigured(false)` for the off path.

### Failure modes

F71, F72 and F91 are in the Failure modes table above (folded in 2026-10-20, D40).

**Test plan (added).** The handler test's "identical bodies and headers" case runs twice, with the fake configured and not; `TestPasswordReset_NoSecretsInTelemetry` covers the F72 warn line; the Mailpit end-to-end run clears `notify.smtp_host` and asserts `501` with identical bodies for a known and an unknown address, then restores it.

## Open questions

**Numbers for reset limits (proposed, D40; owner to confirm or change).** The owner decided the shape (own buckets, per-IP and per-address; a per-account cooldown with a uniform `202`; recommended 5 minutes) but not the figures. Proposed: `ResetClientLimit = 20` and `ResetAddressLimit = 5` requests per `accounts.login_failure_window` (15 minutes by default), `PasswordResetCooldown = 5 minutes`, all constants rather than settings. Reasoning: with the 5-minute cooldown an account gets at most three reset e-mails per 15 minutes anyway, so the per-address bucket of 5 mostly bounds guessing of unknown addresses; 20 per client address covers a shared network (a camp, a venue) without letting one host sweep addresses. If the owner wants them tunable, add `accounts.reset_*` keys (the cooldown `1m`..`1h`).

Otherwise none. The nine questions are settled as decisions (owner, 2026-10-18), each marked where it sits in the Summary:

1. **No playa name at sign-up in M1 (decision 8).** The profile is `people`'s (M2); M1 sign-up asks for an e-mail address and a password only, the header shows a neutral label until the person fills in their profile in M2, and `people` prompts "finish your profile" on first sign-in after it ships.
2. **Several superuser accounts (decision 7).** Allowed, each created by `create-superuser` with its own address, none promotable from a participant account; the admin accounts list shows them.
3. **Sign-up reveals an existing address (decision 15).** `409 taken`, rate-limited per address, until e-mail verification in M4.
4. **Who owns the login address (decision 17).** Identity.
5. **"See my other sessions" (decision 3).** Later, best effort; M1 ships "sign out everywhere else".
6. **A disabled account with the right password (decision 4).** The same 401 as a wrong password; the `Denied` audit entry shows the admin.
7. **The common-password list (decision 13).** SecLists top 100 000 (MIT), embedded, checked exactly.
8. **Temporary passwords in M1 (decision 18).** Included.
9. **Rate limit keyed on the submitted address (decision 12).** Yes: about 100 failures per fifteen minutes per client address and ten per fifteen minutes per submitted address, in memory per process (the client-address default raised, owner, 2026-10-18).

**Exception to audit decision 3 (owner, 2026-10-18).** Sign-out clears the cookie before it records `identity.logout` (decision 10), so a person is never kept signed in by an audit failure; the response is still the error. Recorded in audit.md decision 3 as well. Every other action of identity follows "no audit, no action".

## Proposed changes elsewhere

Status as of 2026-10-19: items 1 to 17 are applied; item 18 waits for `docs/operating.md` (not written) and is carried by this list until then.

1. (applied 2026-10-18) **`plan/design/api.md` (row 16) and `internal/lib/httpx`:** add `httpx.ErrRateLimited`; the mapper maps anything wrapping it to `429 rate_limited` with `Retry-After: 60` (identity and intake both use it); `telemetry.md`'s `intake.ErrRateLimited` becomes `= httpx.ErrRateLimited` (or wraps it). Mark api Proposed change 9 applied: `Authenticator` implemented here with no write, the principal accessors are `SessionFrom`/`PersonFrom`, cookies as decisions 7 and 24, the `accounts` set with public sign-in, sign-up, session, sign-out and password routes; `httpx.TrustedPeerFrom` is not requested yet.
2. (applied 2026-10-18) **`plan/design/web-shell.md` (row 17):** `/sign-in` becomes a redirect to `/accounts/sign-in` that keeps the query; `shell/screens/SignIn.tsx`, `shell/identity.ts` and the `shell.signin.*` keys move to `web/src/core/accounts/` (`core.accounts.*`); `web/src/core/index.ts` lists `accounts`; the session store gains `accountId`, `passwordChangeRequired`, `expiresAt`, `signupOpen`; the route gate sends a restricted session to `/accounts/password?returnTo=` for every route outside `/accounts/`; the header shows `shell.account.anonymous` ("Account") when `playa_name` is empty; `AccountControls` calls sign-out through the generated client; field codes `too_short {min}`, `too_long {max}`, `common`, `taken`, `incorrect`, `self` in the shell catalogue (`field.*`); mark Proposed change 5 applied with the extended session body; the e2e smoke run adds sign-up, sign-in and password change at the phone viewport.
3. (applied 2026-10-18) **`plan/design/app.md` (row 18):** part 2 `identity` in `core_identity.go`: `declare` appends `identity.Set` to `coreSets`, `identity.Permissions` to `corePerms`, `SignalAccountCreated` to `coreSignals`, the command `create-superuser` (`usage: <email>`); `build` calls `identity.New` over `cfg.Session.Key`, `cfg.HTTP.AllowPlaintext`, `a.dbc`, the clock, telemetry, `audit.Scoped("identity")` (audit is built first, audit.md Proposed change 1: the table's order becomes audit 1, identity 2), `a.disp.Scope("identity", {SignalAccountCreated}, nil)`, every seam nil, and sets `a.authenticator` and the `accounts` route set (`Route` → `module.Route`); the audit `ActorFunc` closure is `func(ctx) (audit.Actor, bool) { s, ok := identity.SessionFrom(ctx); ... {Kind: ActorPerson, Person: s.Person, Superuser: s.Superuser}, ok }` (a restricted session is still the person for audit); the command prints as in Behavior; the end-to-end test as in the test plan; mark Proposed change 9 applied; F6 lists `create-superuser` (unchanged).
4. (applied 2026-10-18) **`plan/design/module-hello.md` (row 4b) and `internal/modules/hello`:** M1 step of decision 2: `actor: identity.PersonFrom`; the e2e assertion `GET /api/hello/greeting` with a session is `403` and `deliver:hello.greeted` is asserted request-driven once the owner's base role exists (M2).
5. (applied 2026-10-18) **`plan/design/module-contract.md` (row 4), `.claude/skills/go-conventions/SKILL.md`, `add-module/SKILL.md`:** "the actor is `identity.PersonFrom(ctx)` (false inside a signal delivery and for a restricted session); a module imports `core/identity` for it and takes it as a seam (`actorFunc`) so tests use `identity.WithSession`"; no `Deps` field is added.
6. (applied 2026-10-18, by authz.md Proposed change 4: `PermissionsOf` returns every declared name for a superuser session and `p@<department-id>` entries; `identity.Permissions` carries `StarterRoles: []string{authz.RoleAccountAdmin}`) **`plan/design/authz.md` (row 7):** receive the actor through an option `app` fills from `identity.SessionFrom` (never import identity); `Allow` returns nil for `Superuser` (D23; the audit log marks it; no grant check); implement `identity.PermissionChecker` and `identity.PermissionLister`; the starter role "Account admin" holds `accounts.read` and `accounts.manage`; every signed-in person's base role holds nothing of identity's (the person's own routes need no permission).
7. (applied 2026-10-18, by people.md Proposed change 3: the subscription, `NameSource`, the profile prompt, the login address, export and deletion through `app`'s adapter, `LinkAccount` as the "link an existing person" method) **`plan/design/people.md` (row 8):** subscribe to `identity.account-created {account, person}` and create the person row idempotently (and lazily on first profile access, since delivery is at most once); implement `identity.NameSource`; prompt for the playa name on first sign-in; the login address is identity's (decision 17); call `identity.ExportPerson`/`DeletePerson` in the person export and deletion (through `app`'s adapter, in the same transaction for deletion); "link an existing person to an account" is an identity method proposed with people's spec.
8. (applied 2026-10-19, D39) **`plan/design/settings.md` (row 15):** declare the eight (the seven above plus `accounts.password_reset_lifetime`) `accounts.*` keys with the ranges above and implement `identity.PolicySource` (`Policy.Validate` is the check).
9. (applied 2026-10-19, D39: the superuser alert is `app`'s adapter, notify Proposed change 7; the reset flow is the amendment below, notify Proposed change 1) **`plan/design/notify.md` (row 11):** implement `identity.Alerter` (superuser sign-in to maintainers and site admins; D23) and the password-reset seam identity adds in M4.
10. (applied 2026-10-18) **`plan/design/audit.md` (row 12):** item 3's action list gains `identity.account.read` (Fields `email`, `last_login_at`, `disabled_at`; personal), `identity.account.deleted`, `identity.password.temporary-set`, `identity.sessions.ended`, `identity.superuser.reset`; `Record` kinds used: `identity.account`, `identity.session`; the superuser's login records `identity.superuser.login` instead of `identity.login`.
11. (applied 2026-10-18) **`plan/design/config.md` (row 3):** mark Proposed change 6 applied in part: HKDF derivation with info `bms identity session v1`; failed logins limited by `httpx.ClientAddrFrom`; rotation stays a later command; no new keys.
12. (applied 2026-10-18) **`plan/design/signals.md` (row 10):** mark Proposed change 7 applied for identity (`PersonFrom` is false in delivery; the session value is request-scoped); `Env.CoreSignals` gains `identity.account-created` (payload `account`, `person`).
13. (applied 2026-10-18) **`plan/design/model.md` (row 2):** mark Proposed change 5 applied for identity (address stored as entered, unique on `lower(email)`, lookup by canonical form).
14. (applied 2026-10-18) **`plan/design/db.md` (row 5):** mark Proposed change 10 applied for identity (`postgres.Migrations`, `identity.Set`, schema `core`).
15. (applied 2026-10-18) **`plan/design/telemetry.md` (row 15b):** add `bms.identity.logins{outcome}`, `bms.identity.sessions_rejected{reason}`, `bms.identity.rate_limited{bucket}` to the instrument table.
16. (applied 2026-10-18) **`plan/07-abstractions.md`, "identity":** Owns becomes "local accounts keyed by e-mail (D21), the password policy and Argon2id, the signed session token and cookie checked without a write, the `Authenticator` and the principal accessors (`SessionFrom`, `PersonFrom`), per-account revocation, failed-login limits, the superuser and `bms create-superuser` (D23), the `accounts` routes and screens, `identity.account-created`; providers (OIDC, trusted header) Later behind the same service"; Doesn't do adds "the profile and the playa name (`people`), invitations and password reset until their milestones, e-mail (`notify`)". Also the plan's statement that authz "doesn't do the superuser" becomes "authz allows the superuser by the flag identity supplies; identity creates and marks it".
17. (applied 2026-10-18) **`plan/09-core-features.md` §2:** the M1 line reads "create the superuser by command, sign up, sign in and out, change a password, sign out everywhere else, disable and re-enable an account, set a temporary password"; "see and end their other sessions" becomes "end their other sessions (M1); see them: Later"; the login name's ownership per decision 17 (applied).
18. (pending: target not written) **`docs/operating.md` (unwritten):** `bms create-superuser <email>` (password shown once; re-running resets it and ends its sessions; a participant address is refused); losing `secrets.json` ends every session but keeps passwords; the superuser is for emergencies and every sign-in is logged and audited; the limiter is per process.
