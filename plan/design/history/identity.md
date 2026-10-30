# identity: amendment history

Amendment narratives, reasons, review findings, superseded wording and answered open questions moved verbatim out of `plan/design/identity.md` on 2026-10-30, when the amendments were folded into the spec body (only line breaks at sentence ends were added; no word changed).

## Notes moved from the spec header

- **Amended 2026-10-18 (owner approval of authz/people):** Proposed changes 6 and 7 applied: `EmailOf` and `LinkAccount` added, `DeletePerson` refuses a superuser account, the accounts admin screen links to the profile, the starter role `account-admin` holds `accounts.read` and `accounts.manage` (`StarterRoles`); the sign-up `taken` conflict reaches the browser as `fields` (api D31; F15 and the UI note);
  the per-client-address limiter default is about 100 failures per 15 minutes while the per-account bucket stays at 10 (new `ClientFailureLimit`, decision 12); the two deviations marked pending (ChangePassword sharing the sign-in buckets, IPv6 keyed by /64) are accepted.
- **Amended 2026-10-18 (M1 build, accepted deviations):** implementation deviations recorded in Behavior, "Accepted implementation deviations (M1 build, 2026-10-18)"; `(*Service).CreateSuperuser` added to the Interface; the store interface and the dev-cookie and restricted-session wording corrected to what was built.
- **Amended 2026-10-18 (M2 build, accepted deviations):** `LinkAccount` Conflict codes and the store method `SetPerson` recorded in Behavior, "Accepted implementation deviations (M2 build, 2026-10-18)".
- **Amended 2026-10-19 (D39, owner approval of settings and notify):** the forgotten-password flow (notify Proposed change 1) is specified in full in "Amendment 2026-10-19 (D39): the forgotten-password flow" (before Open questions); `Policy` gains `PasswordResetLifetime`, identity exports `Settings` and `CheckSettings`, and Proposed changes 8 and 9 are applied.
- **Amendment 2026-10-19 (approved, owner, 2026-10-20, D40):** closes the D39 amendment's "Needs a decision" (paragraph removed): `RequestPasswordReset` asks `Notify.Configured` before the account lookup and answers `Unsupported` for every address alike when e-mail is off (notify.md amendment of the same date); F71 and F72 replaced, F91 added. See "Amendment 2026-10-19 (approved, owner, 2026-10-20, D40): reset requests are uniform when e-mail is off".
- **Amended 2026-10-20 (owner, D40; Q-RESET-RATE, Q-RESET-LIFE, Q-WEB-REVIEW):** reset requests have their own limiter buckets (per client address, per submitted address; not sign-in's) and a per-account cooldown between reset e-mails (a request inside it answers the same `202` and sends nothing); the reset e-mail gets the lifetime as `lifetime_minutes` (a number) and each language's template words the unit;
  the password-reset page's document response carries `Referrer-Policy: no-referrer` (api.md step 4, F60); the reset `Message` passes `TTL: PasswordResetLifetime` (notify amendment). The numbers were marked "proposed" until the owner confirmed them as defaults of settings (D41, next bullet).
- **Accepted 2026-10-20 (owner, D40):** the implementation deviations marked "(implementation, 2026-10-19)" in the forgotten-password amendment are accepted as written.
- **Amended 2026-10-20 (approved, owner, 2026-10-20, D43; D41 refines Q-RESET-RATE; ranges as recommended, the cooldown may be `0` = off, no Spanish `es.json` yet):** the reset limits D40 built as constants (`ResetClientLimit`, `ResetAddressLimit`, `PasswordResetCooldown`) become regional settings with the same defaults ("Make it a setting.
  The defaults are good."): `accounts.password_reset_client_limit` (20), `accounts.password_reset_address_limit` (5), `accounts.password_reset_cooldown` (5m; `0` turns it off); `Policy` gains the three fields, `Settings` declares eleven keys, `CheckSettings` gains two cross-field rules; the constants are removed. Decision 22 is no longer "proposed" and its Open question is closed.
  See "Amendment 2026-10-20 (D41): the reset limits are settings" (before Open questions).
- **Amendment 2026-10-30 (owner, D58; demo Q-DEMO6, people P-W1 Proposed change 27):** `SessionInfo` gains `Protected bool` (`protected`, `omitempty`), set from `Options.Guard` when the session is read, so the password screen shows the shared-account notice and disables its form without matching an e-mail address;
  on a `Guard` error `SessionInfo` answers `protected: false` and logs a warning, and the server's refusal stays the backstop (F110); `people` reads `Session.IssuedAt` through `SessionFrom` for its once-per-session prompt, so `IssuedAt` stays the server time at sign-in, fixed for the session's life, and a new session carries a new one (no interface change). See "Amendment 2026-10-30 (D58)" before Open questions.
- **Amendment 2026-10-30 (owner, D57):** the `/accounts/password` page is titled "Account" (it holds "Change password" and "Sign out everywhere else"), matching its nav label; in demo mode a protected account sees the shared-account notice before typing and the form is disabled (demo.md; how the screen knows is demo.md Q-DEMO6).
- **Amendment 2026-10-28 (approved, owner, D53, demo.md Proposed change 3):** `EnsureAccount` (writer-only bootstrap), `Options.Guard` and the `Guard` seam, `WithSession` doc; section "Amendment 2026-10-28" before Open questions; F99 to F109 (D56 review: `LinkAccount` guarded).

## Notes moved from the spec Summary

Summary line "Open questions / Proposed changes elsewhere / Settled":

**Open questions:** none; the nine are settled as decisions 8, 7, 15, 17, 3, 4, 13, 18 and 12 (owner, 2026-10-18); the reset-flow questions as decisions 20 and 21 (owner, 2026-10-19, D39); the reset figures as decision 22 (owner, 2026-10-20, D41); the D41 ranges, the cooldown allowed to be `0` (off) and no Spanish `es.json` yet as decision 22 (owner, 2026-10-20, D43).
**Proposed changes elsewhere:** 21, listed at the end (status there). **Settled 2026-10-20 (D40):** `RequestPasswordReset` asks `Notify.Configured` before the account lookup; e-mail off answers `Unsupported` for every address alike (F71, F72, F91); reset requests use their own limiter buckets and a per-account cooldown (decision 22; F70, F92, F93).
**Settled 2026-10-20 (D41):** the reset limits and the cooldown are settings with D40's figures as defaults (F94 to F98).

**M2 build deviations (2026-10-18).** `LinkAccount` Conflict codes are `superuser` (field `email`) and `has_account` (field `person`); the store gains `SetPerson` (`set_account_person.sql`).

## Superseded wording moved out of the Interface

The `const` block carried this comment after `ClockSkew`:

    	// The password-reset limits and cooldown are Policy fields (D41), not constants: see
    	// PasswordResetClientLimit, PasswordResetAddressLimit and PasswordResetCooldown below.
    	// The constants ResetClientLimit, ResetAddressLimit and PasswordResetCooldown of D40 are
    	// removed, as LoginFailureLimit has no constant either: a default lives in DefaultPolicy.

## Amendment 2026-10-19 (D39): the forgotten-password flow

Source: notify.md Proposed change 1, approved with the owner's answers of 2026-10-19 (D39). It replaces the "later `Reset` seam" this spec once named. Decisions 19 to 21 in the Summary.

### Interface

```go
// Options gains (the Summary line):
//   Notify notify.Notifier // scoped to "identity"; nil means no reset: both routes answer Unsupported

// Policy gains PasswordResetLifetime time.Duration // accounts.password_reset_lifetime
// DefaultPolicy: 1h. Validate: 10m..24h, else Invalid (field "accounts.password_reset_lifetime", code "range").

// Settings declares the accounts.* keys for the settings core area "accounts"
// (module-contract style Decl; the seven existing ones unchanged): eight here (D39),
// eleven since D41; CheckSettings is the area's cross-field Check
// (client_failure_limit >= login_failure_limit; since D41 also the two reset rules). Pure.
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
//     sign-in, and failed sign-ins never block a reset), limits
//     Policy.PasswordResetClientLimit and Policy.PasswordResetAddressLimit per
//     Policy.LoginFailureWindow (D41: read from the policy on this request, as SignIn
//     reads its limits), each request counted once whatever the outcome; at the limit:
//     Unavailable wrapping httpx.ErrRateLimited (D40);
//  3. Notify == nil, or Notify.Configured(ctx) is false: Unsupported, before the account
//     lookup and for every address alike; a Configured error fails with its kind, also
//     before the lookup (F91);
//  4. verify one dummy hash, for every address alike, before the lookup (implementation, 2026-10-19);
//     then the account lookup; unknown, disabled or superuser (D23: command only): an
//     audit entry (Outcome Denied) is recorded, nothing is sent, nil returned;
//  5. otherwise, in one Write transaction (the account row locked first, so two requests
//     cannot both pass the cooldown): read the newest core.password_reset.created_at for
//     the account, used or not; if Policy.PasswordResetCooldown is positive and the row
//     is later than now - Policy.PasswordResetCooldown, record the entry (Outcome Denied,
//     Reason "cooldown"), send nothing, commit nothing else and return nil (the same 202,
//     so nothing is revealed; D40); a cooldown of 0 skips this test (D41); otherwise purge the
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

Store additions (identity's `store` interface, `postgres/`): `ReplaceResetToken(ctx, account model.AccountID, hash []byte, expires time.Time) (id uuid.UUID, err error)` (purges used and expired rows of that account, deletes its other rows, inserts;
one statement set inside the caller's transaction), `ResetByHash(ctx, hash []byte) (ResetRow, error)` (`NotFound` when absent), `CompleteReset(ctx, id uuid.UUID, account model.AccountID, passwordHash string, now time.Time) error` (sets the password and the revocation, marks `used_at`; `NotFound` if the row was already used, so two concurrent redemptions cannot both win).
`ResetState(ctx, account model.AccountID) (account.ResetState, error)` (implementation, 2026-10-20, D40 cooldown: locks the account row and returns `{NewestAt, Disabled, Superuser}`, the newest reset row's `created_at` (zero when none) and the account's flags read under the lock, so an account disabled after the lookup is denied without a send;
a separate call because the cooldown check runs before `ReplaceResetToken` deletes anything).

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

- **Uniform answers.** `RequestPasswordReset` answers `202` for a known, an unknown, a disabled and a superuser address alike; the audit entry (Outcome `Denied` for the last three, so an admin can see attempts) is the only difference.
  Timing is equalised because every request verifies one dummy hash right after the `Configured` probe and before the lookup, for every address alike (on a known address no real hash is verified, so a dummy only on the unknown path would be a timing oracle) (implementation, 2026-10-19).
- **Reset limits and cooldown (D40; settings since D41).** Reset requests are limited in their own buckets (client address `Policy.PasswordResetClientLimit`, submitted address `Policy.PasswordResetAddressLimit`, per `LoginFailureWindow`), so reset traffic cannot lock a person out of signing in and sign-in failures cannot block a reset;
  `ChangePassword` still shares sign-in's buckets (D36(i)), this changes only `RequestPasswordReset`. Within `Policy.PasswordResetCooldown` of the account's newest reset row a request answers the same `202` as any other, sends nothing and records one `Denied` entry with Reason `cooldown`; the cooldown path does the same hash work as every other path (one dummy hash) and sends nothing;
  its database work (the account lock and one read) sits between the unknown-address path and the send path, so it opens no gap beyond the existing known-versus-unknown difference, which the hash cost dominates. Only known, enabled, non-superuser accounts have a cooldown to find.
  A cooldown of `0` means every request that reaches this step sends (the buckets still bound it; the `ResetState` read and lock still happen, so the timing is unchanged). The three are regional settings (`accounts.password_reset_client_limit|address_limit|cooldown`, owner, 2026-10-20, D41) read from the policy at use-case time; see the amendment of 2026-10-20.
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
| F70 | `RequestPasswordReset` | either reset bucket (client address, submitted address) at the limit; sign-in's buckets are not consulted | Unavailable wrapping `httpx.ErrRateLimited` | 429 | `TestRequestPasswordReset_RateLimited_Table` (per client, per address; the limits come from a fake `PolicySource`, not from constants; D41) |
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
| F92 | `RequestPasswordReset` | `Policy.PasswordResetCooldown` is positive and a known address's newest reset row is younger than it | nil, the same `202`; nothing sent, no new token (the earlier link stays valid), one entry Denied (Reason `cooldown`); after the cooldown the next request sends and replaces the token | show the uniform message | `TestRequestPasswordReset_InsideCooldown_SilentNilNothingSent`, `TestRequestPasswordReset_AfterCooldown_Sends` |
| F93 | `RequestPasswordReset`, `SignIn` | reset requests exhaust the reset buckets for an address; sign-in failures exhaust sign-in's buckets for it | the other flow is unaffected: sign-in still works after reset requests are limited, and a reset still works after failed sign-ins | none | `TestResetLimiter_SeparateFromSignIn_Table` |

F94 to F98 (the reset limits as settings) are in the amendment of 2026-10-20 (D41) below.

### Test plan

- **Unit.** The fake store, `notifytest.NewFake("identity")` (`Sent()` after commit, `Fail(err)`), `clock.NewFake`, `audittest.NewFake("identity")`, `telemetrytest.New(t)`: tables for F69 to F90; the token is read from the fake notifier's recorded `Link` and redeemed, never from the store; the stored value is asserted to be 32 bytes of hash and never equal to the token;
  the `lifetime_minutes` var is the bare number ("60") and the stored `TTL` equals the lifetime; the cooldown and both reset buckets with a fake clock; a fake clock moves past `expires_at` (no sleeping); each test puts an invented address, token and password through the service and asserts `!c.Contains(...)` across logs, spans, entries and error strings.
- **Handlers (`httptest`).** `202` for known, unknown and inside-cooldown addresses with identical bodies and headers; `204` on completion with no `Set-Cookie`; `501`, `429`, `503 read_only` statuses; the query string is absent from the log record.
- **Permission tests.** Both routes are public: a signed-in person and a restricted session may use them; neither route returns a session.
- **Integration (`//go:build integration`, `dbtest.TempDB`).** Migration `0002` applies; the unique `token_hash`; the cascade on account deletion; two concurrent `CompleteReset` calls: one wins; `Send` and the token row roll back together.
- **End to end (`app`, with Mailpit).** Set the SMTP settings, request a reset, read the link from Mailpit, complete it, `Authenticate` with the old cookie is `401`, sign in with the new password.
- **Web (`vitest`, `axe-core`).** Both screens' states; the token leaves the address bar; the sign-in link; focus and error summary.

## Amendment 2026-10-19 (approved, owner, 2026-10-20, D40): reset requests are uniform when e-mail is off

Closes "Needs a decision" in the D39 amendment above, together with notify.md's amendment of the same date (`Notifier.Configured`).

**Decision.** Step 3 of `RequestPasswordReset` becomes: `Notify == nil`, or `Notify.Configured(ctx)` returns `false`: `Unsupported`, before the account lookup. The probe is a settings snapshot read (no SMTP connection, no recipient), so its answer cannot depend on the address. Every address therefore gets the same answer whatever the state of e-mail: `501` for all when it is off, `202` for all when it is on.
A `Configured` error fails the request with its kind, still before the lookup, for every address alike (F91). If a settings admin clears the host between the probe and `Send` (a window one request long that only an admin can open), `Send` answers `Unsupported` after the lookup;
identity then rolls back, returns nil (`202`), writes no entry and logs one warn line without the address (F72, replaced): uniformity over honesty for that one request, because the alternative is the oracle. The `/accounts/password-reset` screen is unchanged: its `501` state already reads "Password reset by e-mail isn't set up here. Ask an administrator for a temporary password."

Options not taken: always `202` and enqueue (the queued reset link, a one-time credential, would outlive its hour in the outbox, and the person would be told an e-mail is coming when none can be; notify's `Send` refuses rows while unset by its decision 13); a public `email_enabled` flag read by the screen (does not fix the server, which must be uniform regardless of what the client shows).

Interface change in identity: none. `Options.Notify` stays `notify.Notifier`, which now carries `Configured`; the unit tests' `notifytest.NewFake("identity")` gains `SetConfigured(false)` for the off path.

### Failure modes

F71, F72 and F91 are in the Failure modes table above (folded in 2026-10-20, D40).

**Test plan (added).** The handler test's "identical bodies and headers" case runs twice, with the fake configured and not; `TestPasswordReset_NoSecretsInTelemetry` covers the F72 warn line; the Mailpit end-to-end run clears `notify.smtp_host` and asserts `501` with identical bodies for a known and an unknown address, then restores it.

## Amendment 2026-10-20 (D41): the reset limits are settings

Source: the owner, 2026-10-20 (D41, refining D40 and Q-RESET-RATE): "Make it a setting. The defaults are good." The three figures D40 built as constants in `internal/core/identity/identity.go` (`ResetClientLimit = 20`, `ResetAddressLimit = 5`, `PasswordResetCooldown = 5m`) become `accounts.*` regional settings with those defaults, handled exactly as the sign-in limits already are: `Policy` fields,
`Settings` declarations, `Policy.Validate` and `CheckSettings` as the checks, read at use-case time. Nothing about the reset flow's order, answers or privacy changes. Decision 22 in the Summary carries the result.

**Why these names.** The keys join the existing `accounts.password_reset_lifetime` as one `password_reset_*` family (`password_reset_client_limit`, `password_reset_address_limit`, `password_reset_cooldown`), so the settings admin screen, which lists an area's keys in declaration order, shows the four reset settings together and each key says what it limits,
as `login_failure_limit` and `client_failure_limit` do for sign-in. The `Policy` field names follow the keys (`PasswordResetClientLimit`, `PasswordResetAddressLimit`, `PasswordResetCooldown`). The D40 constants are removed rather than kept as "the defaults": `LoginFailureLimit` has no exported constant either, and a constant that no longer governs anything would be read as if it did.

**Why one window.** The reset buckets keep `accounts.login_failure_window` (decision 22): both pairs of buckets are the same type on the same sweep, the only figure the owner asked to tune is "how many", and a fourth key would be one more thing an admin must understand with no regional having asked for it. If one does, `accounts.password_reset_window` is a one-row addition with the same `1m`..`24h` range.

### Interface

```go
// Policy gains (the full struct is in the Interface section above):
//   PasswordResetClientLimit  int           // accounts.password_reset_client_limit
//   PasswordResetAddressLimit int           // accounts.password_reset_address_limit
//   PasswordResetCooldown     time.Duration // accounts.password_reset_cooldown; 0 = off
//
// DefaultPolicy: PasswordResetClientLimit 20, PasswordResetAddressLimit 5,
// PasswordResetCooldown 5 * time.Minute (D40's figures, unchanged).
//
// Validate adds, each a validate.FieldError (code range, params min and max; durations in
// plain units as the existing Checks print them), all violations reported together:
//   validate.Range("accounts.password_reset_address_limit", p.PasswordResetAddressLimit, 1, 100)
//   validate.Range("accounts.password_reset_client_limit", p.PasswordResetClientLimit,
//                  min(max(5, p.PasswordResetAddressLimit), 1000), 1000)
//   validate.Range("accounts.password_reset_cooldown", p.PasswordResetCooldown,
//                  0, min(time.Hour, p.PasswordResetLifetime))
// (the dependent bounds mirror the existing client_failure_limit rule; the max for the
// cooldown is never below 10m because PasswordResetLifetime's own floor is 10m).
//
// Settings gains three Decls, declared after password_reset_lifetime, in this order:
//   {Key: "password_reset_client_limit",  Kind: settings.Int,      Default: "20", Check: intRange(..., 5, 1000)}
//   {Key: "password_reset_address_limit", Kind: settings.Int,      Default: "5",  Check: intRange(..., 1, 100)}
//   {Key: "password_reset_cooldown",      Kind: settings.Duration, Default: "5m", Check: durationRange(..., 0, time.Hour)}
//
// CheckSettings (the area's cross-field Check over the effective values, bare keys) gains
// two rules beside client_failure_limit >= login_failure_limit, each reported by the bare
// key of the value that must move, all violations joined (validate.Errors):
//   password_reset_client_limit >= password_reset_address_limit
//       ("password_reset_client_limit", code range, min = the address limit, max = 1000)
//   password_reset_cooldown <= password_reset_lifetime
//       ("password_reset_cooldown", code range, min = "0s", max = the lifetime)
// A value that does not parse is still the per-key Check's to report; the rule that needs
// it is skipped. Pure.
```

The use case (`RequestPasswordReset`, step 2 and step 5 of its doc comment above) reads `pol := s.policy(ctx)` once, as it already does for the window and the lifetime, and passes `pol.PasswordResetClientLimit`, `pol.PasswordResetAddressLimit` and `pol.LoginFailureWindow` to `reserveIn` on the reset limiter, and compares the newest reset row against `pol.PasswordResetCooldown` when it is positive.
No signature changes; no new store method; no migration. `app`'s `PolicySource` adapter maps the three keys onto the three fields as it maps the other eight (settings.md Proposed change 4, identity Proposed change 8).

### Settings

The three rows are in the Settings table above (D41). Ranges, and why:

| Key | Default | Range | Reason for the range |
|---|---|---|---|
| `accounts.password_reset_client_limit` | `20` | `5`..`1000`, never below `password_reset_address_limit` | the floor is the smallest number that still lets a shared venue address serve a handful of people in one window; the ceiling matches `client_failure_limit`; the dependent floor has the same reason as sign-in's (many people share one address) |
| `accounts.password_reset_address_limit` | `5` | `1`..`100` | `1` is the strictest a regional can ask (one e-mail per address per window) and still lets the flow work; the ceiling matches `login_failure_limit`; above it the bucket no longer bounds mail to one address |
| `accounts.password_reset_cooldown` | `5m` | `0s`..`1h`, never above `password_reset_lifetime` | `0` turns the cooldown off for a regional that prefers the address bucket alone (the bucket still bounds mail); an hour is already the default link lifetime; a cooldown longer than the lifetime would leave a person whose link expired unable to ask for another, so the lifetime caps it |

Who can change them: the settings admin, like every `accounts.*` key. Nothing personal is stored. The settings admin screen is `settings`' (its `/settings/:area` screen); identity supplies only the declarations and the catalogue text below.

### Behavior

- **A change applies to the next request, no restart.** The limiter holds counts and timestamps, never a limit: each `reserve` takes the limits and the window as arguments (as sign-in's does today), so a changed value is compared on the very next request.
  Counts already in a bucket stay: lowering a limit below an address's current count limits it at once, raising it releases it; the window is also read per request, so shortening it forgets older entries on the next sweep. The cooldown is compared at request time against the newest reset row, so a shortened cooldown lets a waiting person ask again at once and a lengthened one applies to the account's next request.
  `settings` decision 10 (no signal on change, every consumer reads at use-case time) is what makes this free.
- **Invalid policy.** As F62: a `PolicySource` that returns a policy failing `Validate` (a stored value outside its range after an upgrade cannot happen, since `settings` decision 5 yields the default, but a broken adapter can) fails the use case `Invalid`; identity never falls back silently.
- **Cooldown off.** With `PasswordResetCooldown == 0` step 5 skips the age comparison and always replaces the token and sends. The `ResetState` read and the account lock still happen, so the silent and the sending paths keep the same shape and no timing gap opens (D40's reasoning is unchanged).
- **Concurrency, read-only copy, privacy, telemetry:** unchanged. A setting's value never reaches a log, span or metric (settings decision 13); `bms.identity.rate_limited{bucket}` still counts only the bucket name.

### UI

No screen of identity's changes. The settings admin screen (`settings`) shows the three keys with these catalogue entries in `web/src/core/accounts/messages/en.json`, following `core.accounts.setting.<key>.title|description` (settings.md, catalogue tests in `internal/app`: every core area's declared keys must have their titles, so `TestMessageCatalogues_EnglishCoversManifests` fails until they exist):

| Key | en |
|---|---|
| `core.accounts.setting.password_reset_client_limit.title` | Password-reset requests from one device or network |
| `core.accounts.setting.password_reset_client_limit.description` | How many password-reset e-mails may be requested from one device or network address within the sign-in window above. Many people share one address at a venue, so keep it at least as high as the per-address limit below. From 5 to 1000. |
| `core.accounts.setting.password_reset_address_limit.title` | Password-reset requests for one e-mail address |
| `core.accounts.setting.password_reset_address_limit.description` | How many password-reset e-mails may be requested for one e-mail address within the sign-in window above, whether or not an account exists for it. From 1 to 100. |
| `core.accounts.setting.password_reset_cooldown.title` | Pause between password-reset e-mails |
| `core.accounts.setting.password_reset_cooldown.description` | After a reset e-mail is sent, further requests for that account send nothing until this long has passed; the earlier link keeps working. 0 turns the pause off. Up to 1 hour, and never longer than the link lifetime above. |

The existing `core.accounts.setting.login_failure_window.description` is reworded to say what it now also governs: "Failed sign-ins and password-reset requests older than this are forgotten. From 1 minute to 24 hours." (its title stays "How long failed sign-ins are counted"). Spanish, for the `es.json` of the `accounts` catalogue when one exists (today every screen set ships `en.json` only and other languages fall back to `en`, web-shell D8; see Open questions):

| Key | es |
|---|---|
| `core.accounts.setting.password_reset_client_limit.title` | Solicitudes de restablecimiento desde un mismo dispositivo o red |
| `core.accounts.setting.password_reset_client_limit.description` | Cuántos correos de restablecimiento de contraseña pueden solicitarse desde un mismo dispositivo o dirección de red dentro de la ventana de inicio de sesión de arriba. Muchas personas comparten una dirección en el evento, así que manténlo al menos tan alto como el límite por dirección de abajo. De 5 a 1000. |
| `core.accounts.setting.password_reset_address_limit.title` | Solicitudes de restablecimiento para una misma dirección de correo |
| `core.accounts.setting.password_reset_address_limit.description` | Cuántos correos de restablecimiento de contraseña pueden solicitarse para una misma dirección de correo dentro de la ventana de inicio de sesión de arriba, exista o no una cuenta con ella. De 1 a 100. |
| `core.accounts.setting.password_reset_cooldown.title` | Pausa entre correos de restablecimiento |
| `core.accounts.setting.password_reset_cooldown.description` | Tras enviar un correo de restablecimiento, las siguientes solicitudes para esa cuenta no envían nada hasta que pase este tiempo; el enlace anterior sigue funcionando. 0 desactiva la pausa. Hasta 1 hora, y nunca más que la duración del enlace de arriba. |
| `core.accounts.setting.login_failure_window.description` | Los inicios de sesión fallidos y las solicitudes de restablecimiento más antiguos que esto se olvidan. De 1 minuto a 24 horas. |

### Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F94 | `Policy.Validate` | `PasswordResetAddressLimit` outside `1`..`100`; `PasswordResetClientLimit` outside `5`..`1000` or below `PasswordResetAddressLimit`; `PasswordResetCooldown` negative, above `1h` or above `PasswordResetLifetime` | Invalid (field = the full key, code `range`, every violation at once) | settings refuses the change; a broken adapter fails the use case (F62) | `TestPolicy_Validate_ResetLimits_Table` |
| F95 | `CheckSettings` | effective `password_reset_client_limit` below `password_reset_address_limit` | Invalid (`password_reset_client_limit`: `range`, `min` = the address limit) | the settings screen shows the field error on the client limit | `TestCheckSettings_ResetClientNotBelowAddress` |
| F96 | `CheckSettings` | effective `password_reset_cooldown` above `password_reset_lifetime` | Invalid (`password_reset_cooldown`: `range`, `max` = the lifetime) | the settings screen shows the field error on the cooldown | `TestCheckSettings_CooldownNotAboveLifetime` |
| F97 | `RequestPasswordReset` | `PasswordResetCooldown` is `0` and the account's newest reset row is seconds old | nil; a new token replaces the old, one message sent, one entry (never Reason `cooldown`); the `ResetState` read still happens | show the uniform message | `TestRequestPasswordReset_CooldownOff_AlwaysSends` |
| F98 | `RequestPasswordReset` | the fake `PolicySource` lowers a reset limit below an address's current count, or shortens the cooldown below the newest row's age, between two requests | the next request is limited (429) or sends, respectively, with no restart and no limiter reset; raising the limit again releases the address | none | `TestRequestPasswordReset_PolicyChange_AppliesToNextRequest` |

`Settings`' three `Check`s cannot fail in a way of their own: each is `intRange` or `durationRange` over the same bounds as `Validate`, and `TestSettings_Checks_AgreeWithPolicyValidate` proves they agree at both bounds and one past each.

### Test plan

- **Unit (`identity`).** `TestSettings_DeclarationsAreValidAndDefaultToDefaultPolicy` covers eleven keys (`"20"`, `"5"`, `"5m"` equal `DefaultPolicy()`'s fields); `TestSettings_Checks_AgreeWithPolicyValidate` covers the three new keys at `lo-1`, `lo`, `hi`, `hi+1`;
  `TestCheckSettings_ClientLimitNotBelowLoginLimit` gains the two new rules (F95, F96) and a case where two rules fail at once and both field errors are returned; F94 as a table including the dependent bounds (`client 4, address 5` fails on the client key; `cooldown 20m, lifetime 10m` fails on the cooldown key; `cooldown 0` passes);
  `TestRequestPasswordReset_RateLimited_Table` and `TestResetLimiter_SeparateFromSignIn_Table` take their limits from a fake `PolicySource` returning a `Policy` with small values (for example client 3, address 2), so the tests no longer depend on the defaults; F97 and F98 with the fake clock and a fake `PolicySource` whose policy the test swaps between calls.
  Every test still asserts `!c.Contains(address)` across logs, spans, entries and errors (`telemetrytest`).
- **Catalogue (`internal/app`).** The existing core-area catalogue test fails until the six new `en` entries exist; it also checks the reworded `login_failure_window` description is present (any text).
- **Integration, handlers, end to end:** no additions; nothing in the database or on the wire changes. The Mailpit end-to-end run may set `accounts.password_reset_cooldown` to `0` through `PUT /api/settings/values` before requesting two resets in a row and assert two e-mails arrive; recommended, not required.

## Amendment 2026-10-28 (approved, owner, D53): `EnsureAccount` and the `Guard`

Written for the public demo (`demo.md`). Production behaviour is unchanged: `Options.Guard` is nil outside demo mode.

### Interface

```go
// AccountResult is what EnsureAccount reports.
type AccountResult struct {
	Account model.AccountID
	Person  model.PersonID
	Created bool // false: the address existed and its password was reset
}

// EnsureAccount is the writer-only bootstrap for commands (the sibling of CreateSuperuser):
// no actor, no permission check, no sign-up mode check, no limiter. The password is
// validated as SignUp validates it (length, common-password list). It creates the account
// and publishes SignalAccountCreated, or, when the address exists and is not a superuser,
// sets the password, enables the account and bumps session_generation. Audited
// identity.account.created or identity.password.changed with ActorOperator. Fails Invalid
// (password), Conflict (code superuser) for a superuser address, Unavailable wrapping
// db.ErrReadOnly on a read-only copy, store kinds. Consumer: core/demo's seed through app.
func (s *Service) EnsureAccount(ctx context.Context, c Credentials) (AccountResult, error)

// Guard says which accounts are shared and must stay usable by everyone.
type Guard interface {
	Protected(ctx context.Context, a model.AccountID) (bool, error)
}
```

### Behavior

Identity consults `Options.Guard` (when non-nil) in `ChangePassword`, `SetTemporaryPassword`, `DisableAccount`, `DeletePerson`, `LinkAccount` (after the superuser check), `RequestPasswordReset` (answers nil and sends nothing, as for a superuser address), `CompletePasswordReset` (no token can exist) and any later e-mail change. The refusal is `Permission` with field `account` and code `protected`.
`EnableAccount`, `SignOut` and `EndOtherSessions` stay allowed. A `Guard` error fails the use case (closed). `EnsureAccount` itself is not guarded (it is how the seed sets the demo password).

### Failure modes (added)

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F99 | `EnsureAccount` | password fails the sign-up rules | Invalid | fix the seed table | `TestEnsureAccount_WeakPassword_ReturnsInvalid` |
| F100 | `EnsureAccount` | the address is a superuser's | Conflict `superuser` | exit 1 | `TestEnsureAccount_SuperuserAddress_ReturnsConflict` |
| F101 | `EnsureAccount` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | exit 1 | `TestEnsureAccount_ReadOnly_ReturnsUnavailable` |
| F102 | `EnsureAccount` | the address exists | — (`Created` false; password set, account enabled, sessions ended) | proceed | `TestEnsureAccount_Existing_ResetsPassword` |
| F103 | `ChangePassword`, `SetTemporaryPassword` | the account is protected | Permission `account` `protected` | show the shared-account message | `TestChangePassword_Protected_ReturnsPermission` |
| F104 | `DisableAccount` | the account is protected | Permission `account` `protected` | show the message | `TestDisableAccount_Protected_ReturnsPermission` |
| F105 | `DeletePerson` | the person's account is protected | Permission `account` `protected` | people maps it to `Conflict protected` | `TestDeletePerson_Protected_ReturnsPermission` |
| F106 | `RequestPasswordReset` | the account is protected | — (nil, nothing sent) | answer as for any address | `TestRequestPasswordReset_Protected_SendsNothing` |
| F107 | `CompletePasswordReset` | the account is protected | Permission `account` `protected` | show the message | `TestCompletePasswordReset_Protected_ReturnsPermission` |
| F108 | any guarded method | `Guard.Protected` fails | the guard's kind (Unavailable) | fail closed | `TestGuard_Error_FailsTheUseCase` |
| F109 | `LinkAccount` | the account is protected (checked after the superuser check) | Permission `account` `protected` | people maps it to `Conflict protected` | `TestLinkAccount_Protected_ReturnsPermission` |
| F110 | `SessionInfo` | `Guard.Protected` fails (signed in, non-nil Guard) | none: the call succeeds | answers `protected: false` and logs a warning (owner, D58 revision); the server's refusal on password, disable, delete and link stays the fail-closed backstop | `TestSessionInfo_GuardError_AnswersUnprotectedAndLogs` |
| F111 | `SessionInfo` | the session's account is protected / not protected / nil Guard / signed out | — | `protected: true` / absent / absent / absent | `TestSessionInfo_Protected_TrueOnlyForGuardedAccount` |

Nil `Guard`: `TestGuard_Nil_ProtectsNothing`.

## Amendment 2026-10-30 (D58): `SessionInfo.Protected`

From demo.md Q-DEMO6 (answered, owner, 2026-10-30, D58). The password screen must know before the person types that the account is a shared demo account.
The session carries it: `SessionInfo.Protected bool` (wire `protected`, `omitempty`), set by `SessionInfo` from `Options.Guard.Protected(ctx, session.Account)` when the call is made by a signed-in person and the Guard is non-nil; false (absent) for a nil Guard and for a signed-out answer.
The screen shows `core.accounts.field.protected` and disables the form when `protected` is true; the server's `Permission` `account` `protected` stays the backstop for a stale page or a direct call. The e-mail is not added to the session.

**Guard error (owner, 2026-10-30, D58, revised).** If `SessionInfo` failed whenever the Guard errors, a demo database hiccup would make the session answer fail for everyone and break the whole app. On a `Guard` error `SessionInfo` therefore answers `protected: false` and logs a warning (no personal data in the line); the rest of the answer is unaffected.
The server's own refusal (`Permission` `account` `protected`) on password, disable, delete and link stays the fail-closed backstop (F108 still applies to those use cases), so a protected account is never changed; at worst the form shows and the save is refused. Not taken: failing `SessionInfo` with the Guard's kind. F110 and F111 above; `SessionInfo` stays one indexed read heavier in demo mode only.

## Open questions as they stood before the fold-in (moved from the spec)

**Numbers for reset limits: closed (owner, 2026-10-20, D41).** The figures proposed under D40 (20 per client address, 5 per submitted address per `accounts.login_failure_window`, a 5-minute cooldown) are kept as the defaults of three regional settings, `accounts.password_reset_client_limit|address_limit|cooldown` ("Make it a setting.
The defaults are good."); the keys, ranges and tests are in the amendment of 2026-10-20 (D41) above and in decision 22. The ranges were the architect's proposal under D41 and are approved as written (owner, 2026-10-20, D43).

Two choices in that amendment were the owner's rather than the architect's; both are answered as recommended (owner, 2026-10-20, D43), as are the ranges (decision 22):

1. **May the cooldown be turned off (`0`)?** Answered yes (owner, 2026-10-20, D43), range `0s`..`1h`. The per-address bucket still bounds e-mails to any one address, so `0` only removes the "one link per five minutes" courtesy; a regional that prefers the bucket alone, or that is testing its e-mail set-up, should not have to lower it to a token value such as `1s`. If the owner says no, the floor becomes `30s` and F97 is dropped (the text and the `durationRange` floor are the only changes).
2. **Should a Spanish `es.json` start now?** Answered not yet (owner, 2026-10-20, D43; no Spanish accounts catalogue yet). Reason: no screen set ships a second catalogue today and other languages fall back to `en` (web-shell D8);
   the Spanish strings in the amendment are ready for whoever starts the `accounts` catalogue's `es.json`, and starting it with six strings would commit the project to keeping a whole second catalogue current from this change on. If the owner says yes, the file is created with the six entries plus the reworded window description and the catalogue test gains an `es` case.

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
