# notify

- **Status:** Approved (2026-10-19, D39)
- **Amendment 2026-10-19 (pending owner approval):** retry, expiry and the text of failed rows (`Message.TTL`, `MaxAge`, `expires_at`, `ExpireStale`, the `SendTo` dedupe index); see "Amendments". Until approved, decision 11 and the Data table stand as written.
- **Amendment 2026-10-19 (pending owner approval):** `Notifier` gains `Configured(ctx) (bool, error)` so a caller can refuse before work whose outcome must not depend on the recipient (closes identity.md's "Needs a decision" on reset-request uniformity); `notifytest.Fake` gains `SetConfigured`. See "Amendment 2026-10-19 (pending owner approval): `Notifier.Configured`" before Open questions; F64 to F66.
- **Package:** `internal/core/notify` (contract, catalogue, outbox, sender, handlers), `internal/core/notify/message` (the record type), `internal/core/notify/postgres` (store, `sql/`, `migrations/`), `internal/core/notify/smtp` (the SMTP channel; the only package importing `net/smtp`), `internal/core/notify/notifytest` (fakes)   **UI:** `web/src/core/notify/`
- **Depends on specs:** `model`, `lib-errs`, `lib-core` (`clock`, `retry`, `validate`), `lib-secret`, `db`, `audit`, `authz` (leaf types, `Authorizer`), `telemetry`, `api` (`httpx`, the route-set shape), `web-shell`, `app` (part 9), `settings` (row 15, approved 2026-10-19, D39: the keys named under Settings, the sealed `Secret` kind), `people` (`LanguageOf`, `ContactEmailOf`: proposed), `identity` (`EmailOf`; the password-reset flow: proposed)   **Used by:** every module (`Deps.Notify`), `identity` (password reset, the superuser alert through `app`'s `Alerter` adapter, later the e-mail change confirmation), `people` (the deletion-request notice), `app` (part 9, the runnable, `bms check`), `web-shell` (the admin screens)

## Summary

**Purpose.** Deliver a notification to a person through a channel: a caller names a template and recipients inside its own transaction, notify renders the text in the recipient's language, queues it durably, and a sender loop on the writing copy delivers it over SMTP with retries. E-mail is the only channel in M4; others arrive behind the same `Channel` seam. Package `internal/core/notify`; imports `model`, `lib`, `core/db`, `core/audit`, `core/authz` (leaf types and the `Authorizer`), `core/telemetry`. Never `core/identity`, `core/people`, `core/settings`, `core/module`, `api`, `app`, `config` or a module: what it needs from those arrives through seams `app` fills.

**Names.** `notify` is the package, the core part, the migration set (schema `core`), the route set (`/api/notify/`), the permission prefix, the audit area, the settings prefix, the screen set and the owner of its one template, `notify.test`.

**Exported (`notify`).**
- `Template` (`<owner>.<ident>`, the `signals.Name` grammar; `Validate`, `Owner`); `Language` (`""` or a BCP 47 tag as people validates it; `Validate`); `MessageID = model.ID[message]`; `Status` (`Queued`, `Sending`, `Sent`, `Failed`; `String`, `ParseStatus`).
- `Message{Template, To []model.PersonID, Key string, Vars map[string]string, Link string}` (+`Validate`): what a caller sends. `Key` is an optional idempotency key per (owner, key, recipient).
- `Notifier{Send(ctx, Message) error}` = `module.Deps.Notify`, scoped to one owner by `(*Service).Scoped(owner)`; `AddressSender{SendTo(ctx, model.Email, Message) error}` (core only: identity's e-mail change confirmation, the alert addresses; one explicit address, `To` empty).
- `Record{ID, Owner, Template, Person model.PersonID, Language, Status, Attempts int, NextAttemptAt, LeasedUntil, CreatedAt, SentAt, FailedAt time.Time, LastError string, Subject string}` (`LogValue` = ID only); `Rendered{Subject, Body string}`; `TemplateInfo{Name Template, Vars []string, Languages []LanguageInfo}`, `LanguageInfo{Language, Overridden bool, UpdatedAt time.Time}`; `TemplateText{Subject, Body string}`; `TemplateView{Name, Language, Default, Current TemplateText, Overridden bool, Vars []string}`; `OutboxView{Configured bool, Counts map[Status]int, Recent []Record}`; `Sent{Sent, Retried, Failed, Skipped int}`.
- Channel seam: `Envelope{From Address, ReplyTo *Address, To model.Email, Subject, Body, MessageID string}`, `Address{Name string, Email model.Email}`; `Channel{Send(ctx, Envelope) error; Ping(ctx) error}`; `ChannelFactory func(SMTPConfig) (Channel, error)` (default `smtp.New`); `ErrRejected` (permanent rejection, `Invalid`).
- Settings seams: `SMTPConfig{Host string, Port int, Security Security, Username string, Password secret.Secret, Sender Address, ReplyTo *Address, AlertAddresses []model.Email}` (+`Validate`, `Configured()`), `Security` (`StartTLS`, `TLS`, `None`); `SMTPSource{SMTP(ctx) (SMTPConfig, error)}`; `Site{Name, URL string, DefaultLanguage Language, Terms map[string]string}`, `SiteSource{Site(ctx, Language) (Site, error)}`; `RetentionSource{Retention(ctx) (time.Duration, error)}`; `ErrNotConfigured` (`Unsupported`).
- Recipient seams: `AddressSource{AddressOf(ctx, model.PersonID) (model.Email, error)}` (`app`: identity's `EmailOf`, then people's `ContactEmailOf`; `NotFound` = no address); `LanguageSource{LanguageOf(ctx, model.PersonID) (Language, error)}` (people; `""` = site default); `NameSource{PlayaNameOf(ctx, model.PersonID) (string, error)}` (people; `""` = unknown).
- `Options{DB, Clock, Logger, Tracer, Meter, Audit audit.Recorder (scoped "notify"), Authz authz.Authorizer, Actor authz.ActorFunc, SMTP SMTPSource, Site SiteSource, Retention RetentionSource, Addresses AddressSource, Languages LanguageSource, Names NameSource, Channel ChannelFactory, PollInterval, Lease, SendTimeout time.Duration, Hostname string}` (+`Validate`); `New(Options) (*Service, error)` (no I/O; works against `db.None()`).
- `*Service` (safe for concurrent use): `Register(owner string, defaults fs.FS) error` (build time, before `Run`); `Scoped(owner) (Notifier, error)`; `AddressSender` (unscoped, core only); `Run(ctx) error` (the sender loop, an `app` runnable); `SendOnce(ctx) (Sent, error)`; `Ping(ctx) error`; `ListTemplates`, `GetTemplate`, `PutTemplate`, `ResetTemplate`, `Preview`, `SendTest`, `Outbox`, `Retry`, `Cancel`; `ExportPerson(ctx, model.PersonID) (json.RawMessage, error)`; `DeletePerson(ctx, model.PersonID) (int, error)`; `Routes() []Route`; `OpenAPI() []byte`; `Close(ctx) error` (idempotent). `Route{Method, Pattern string, Handler http.Handler}` (all signed-in; `app` converts).
- Constants: `MaxRecipients = 100`, `MaxVars = 32`, `MaxVarKeyBytes = 32`, `MaxVarBytes = 1024`, `MaxLinkBytes = 1024`, `MaxKeyBytes = 128`, `MaxTemplateBytes = 16384`, `MaxSubjectBytes = 256`, `MaxAttempts = 8`, `MaxBatch = 50`, `MaxOutboxList = 50`, `DefaultPollInterval = 5s`, `DefaultLease = 2m`, `DefaultSendTimeout = 30s`, `PurgeInterval = 1h`, `MinRetention = 7 days`, `TemplateFileExt = ".tmpl"`, `DefaultLanguage = "en"`.
- Permissions `notify.templates.manage`, `notify.outbox.manage` (site, reach `internal`, starter role `settings-admin`); `Permissions []authz.PermissionDecl`. No signals. Settings (core area `notify`, `Settings []settings.Decl`) `notify.smtp_host|smtp_port|smtp_security|smtp_username|smtp_password`, `notify.sender_address|sender_name`, `notify.reply_to`, `notify.alert_addresses`, `notify.retention` (stored by `settings`; the password lives in settings, sealed: owner, 2026-10-19, D39). Audit actions `notify.template.changed|reset`, `notify.test.sent`, `notify.message.retried|canceled`. `postgres.Migrations fs.FS`; `Set = db.Set{Name: "notify", Schema: "core"}`. Templates: `Templates fs.FS` (`notify.test`).
- `notifytest.NewFake(owner) *Fake` (a `Notifier` that validates like the real one, records `Sent() []Message` after commit, `Fail(err)`); `notifytest.Channel` (an in-memory `Channel` with `Envelopes()`, `Fail(err)`, `Reject()`).
- Pending amendment (2026-10-19, owner approval needed): `Notifier` is `{Send; Configured(ctx) (bool, error)}`: `Configured` is the check `Send` makes first (SMTP host and sender set), callable before any lookup; fails only with the `SMTPSource`'s kind. `notifytest.Fake.SetConfigured(bool)` (default `true`). F64 to F66.

**Key decisions.** 1 `Send` enqueues on the caller's transaction and never talks to SMTP: the row commits or rolls back with the caller's change, so a confirmed sign-up and its e-mail agree. 2 the outbox is notify's own table and sender loop, not `jobs` (unwritten): the row is the notification record itself (status, attempts, export, the admin screen); the loop is one `FOR UPDATE SKIP LOCKED` lease, the shape audit's spool importer already uses; jobs may host the tick and the purge later, nothing else moves. 3 text is rendered at enqueue (the row is self-contained; a bad call fails synchronously); the address is resolved at send (an address change before delivery wins; the row never copies an address a person can change). 4 SMTP over the standard library, `StartTLS` by default, proven on Mailpit; other channels implement `Channel`. 5 "never twice" is an idempotency `Key` per (owner, key, recipient) plus a lease; a crash between the server's acceptance and the row update can resend once after the lease, and the spec says so. 6 retries: `MaxAttempts` with backoff 1m doubling to 1h (`retry.Policy`); a `4xx` reply, a connection or timeout failure retries; a `5xx` reply is final (`rejected`); a recipient without an address is final (`no_address`); an unconfigured SMTP leaves the row queued without counting an attempt (`no_smtp`). 7 on a read-only copy `Send` fails `Unavailable` wrapping `db.ErrReadOnly` (inside a `Write` transaction, like the change; outside, no spool: nothing on a read-only copy produces a notification except the superuser alert, which identity logs anyway) and the loop skips every tick while `ReadOnly()`. 8 the language is the recipient's (`people`), else the site default, else `en`; a tag falls back to its primary subtag. 9 templates are files `<ident>.<lang>.tmpl` (`Subject:` and optional `Vars:` headers, a blank line, the body) shipped by their owner (`Manifest.Templates`, `identity.Templates`, `people.Templates`) and overridden per language by a settings admin in `core.notify_template`; `text/template` with the funcs `term` only, `missingkey=zero`, parsed on save and at `Register`. 10 a caller's `Vars` carry nothing `sensitive` and nothing personal about anyone but the recipient; `Link` is a path the renderer prefixes with `regional.public_url`; notify never logs an address, a subject, a body or a token. 11 subject and body are cleared when a row reaches `Sent` or `Failed`; status rows live until `notify.retention` (0 = forever, D30). 12 `Send` checks no permission: the caller's use case is the authority and the owner scope limits which templates a caller may use; the admin routes check theirs. 13 SMTP and the sender are runtime settings (09 §6: the settings admin sets them; the password is settings' sealed `Secret` kind, owner, 2026-10-19), not configuration; `Send` fails `Unsupported` wrapping `ErrNotConfigured` before looking at any recipient when no host or sender is set. 14 notify subscribes to no signal and decides no recipient (07): people sends its own deletion-request notice through a `Notifier` seam; the superuser alert is an `app` adapter over `authz.HoldersOf` (proposed). 15 no HTML part, no attachments, no per-person preferences, no digests in M4. 16 (owner, 2026-10-19, D39) no automatic sign-in after a password reset: identity sets no cookie from a public route and sends the person to the sign-in page. 17 (owner, 2026-10-19, D39) a reset token lasts 1 hour by default (identity setting `accounts.password_reset_lifetime`, 10 minutes to 24 hours), is single use, and only the newest token for an account is valid. 18 (owner, 2026-10-19, D39) "Send me a test" sends only `notify.test`; `Preview` already shows any template's rendering.

**Errs kinds callers handle.** `Send`/`SendTo`: `Unsupported` wrapping `ErrNotConfigured` (check `errors.Is` first; api answers 501 `notify_off`), `Invalid` (message fields with codes `required`, `malformed`, `too_long {max}`, `too_many {max}`, `control_chars`, `unknown` (template), `scope` (another owner's template), `one_of`), `Unavailable` wrapping `db.ErrReadOnly`, store kinds (`Unavailable`, `Timeout`, `Canceled`), the seams' kinds (`Site`, `Languages`). Admin methods: `Unauthenticated`, `Permission`, `NotFound` (template, language, message), `Invalid` (`subject`/`body`: `required`, `too_long`, `parse` (with the parser's line), `control_chars`; `language` `malformed`), `Conflict` (`status`: retry of a row not `Failed`, cancel of a row not `Queued`/`Failed`), `Unavailable` wrapping `db.ErrReadOnly`. `Register`: `Invalid` (name, missing `en`, parse error, oversize), `Conflict` (owner registered twice), `Internal` (after `Run`). `SendOnce`: `Unavailable` wrapping `db.ErrReadOnly` (nothing leased), store kinds. `Ping`: `Unsupported` wrapping `ErrNotConfigured`, `Unavailable`, `Timeout`. `New`/`Options.Validate`: `Invalid` with fields `notify.db`, `notify.clock`, `notify.logger`, `notify.tracer`, `notify.audit`, `notify.authz`, `notify.actor`, `notify.smtp`, `notify.site`, `notify.addresses`, `notify.languages`, `notify.names`, `notify.channel`, `notify.poll_interval`, `notify.lease`, `notify.send_timeout`, `notify.hostname`. `Channel.Send`: `Invalid` wrapping `ErrRejected`, `Unavailable`, `Timeout`, `Canceled`.

**Owns.** Tables `core.notify_message` (the outbox; `address` and the rendered text are `personal`, cleared on completion; status rows under `notify.retention`), `core.notify_template` (overrides, `internal`). The two permissions, the ten settings keys, the five audit actions, template `notify.test`, route set `notify` (`GET /templates`, `GET|PUT|DELETE /templates/{name}/{lang}`, `POST /templates/{name}/{lang}/preview`, `POST /test`, `GET /outbox`, `POST /outbox/check` (the outbox screen's "check connection": `CheckConnection`, `Ping` behind `notify.outbox.manage`; 204 on success), `POST /outbox/{message}/retry`, `POST /outbox/{message}/cancel`), screens `/notify/templates`, `/notify/templates/:name/:lang`, `/notify/outbox`; keys `core.notify.*`; spans `notify.send`, `notify.tick`, `notify.deliver` (Client), `notify.<use-case>`; metrics `bms.notify.enqueued{owner}`, `bms.notify.delivered{outcome}`, `bms.notify.outbox{status}` (gauge), `bms.notify.purged`.

**Expects of dependencies.** `db`: `Transact`, `TxMode`, `AfterCommit`, `ReadOnly`, `ErrReadOnly`, `Wrap`, `*Error.Constraint`, `Set`, `dbtest`; `notify` added to the fixed set order after `jobs` (Proposed change 2). `audit`: a `Recorder` scoped `notify`. `authz`: `Allow`; `HoldersOf(ctx, Permission) ([]model.PersonID, error)` for `app`'s alert adapter and people (proposed). `settings` (Draft, 2026-10-19, already aligned): the `notify.*` keys under Settings as `settings.Decl`s in core area `notify` (`notify.smtp_password` of kind `Secret`), `Regional` (`regional.name`, `regional.public_url`, `regional.language`), `TermsFor(ctx, lang)` for `term`, a `Reader` scoped `notify`; the `SMTPSource`, `SiteSource` and `RetentionSource` adapters are `app`'s over those. `people`: `PlayaNameOf`, `LanguageOf`, `ContactEmailOf`, `Templates`, a `Notifier` seam for `people.deletion-requested` (proposed). `identity`: `EmailOf`; `Templates` (`identity.password-reset`, `identity.superuser-signed-in`); the password-reset flow (Proposed change 1). `module-contract`: `Deps.Notify notify.Notifier` scoped by name, `Manifest.Templates fs.FS`, `notifytest.NewFake` in `Conformance`. `app`: part 9 (declare, build, link, the runnable, the lazy `Alerter` and `Notifier` adapters, `bms check` lines), `coreSetOrder` gains `notify`. `telemetry`: typed attributes, `telemetrytest`; the metric rows (Proposed change 9). `web-shell`: `web/src/core/notify/` as a core area. `deploy/compose`: Mailpit, `BMS_TEST_SMTP_ADDR`, `BMS_TEST_MAILPIT_URL` (proposed).

**Open questions:** none; the four were answered 2026-10-19 (D39) and are folded into decisions 13, 16, 17 and 18 (SMTP password in settings, sealed; no automatic sign-in after a reset; the reset token lifetime; the test message sends only `notify.test`). **Proposed changes elsewhere:** 12, listed at the end; 11 applied 2026-10-19 (D39), item 12 (`deploy/compose/.env*`, `docs/operating.md`) left to the implementers. **Amendment pending (2026-10-19, sql-author's review):** text lives until `sent` or `expires_at` (`Message.TTL`, `MaxAge` 7 days, `ExpireStale`, `Retry` only while live, a `SendTo` dedupe index); see "Amendments"; F67–F72.

## Purpose

Deliver a notification to a person through a channel.

## Responsibilities

- **The notifier contract.** `Notifier.Send` for modules and core areas (scoped to an owner), `AddressSender.SendTo` for the core's few explicit-address cases.
- **Templates.** Keep each owner's default templates per language, a settings admin's overrides, the rendering (`text/template`, the site's name, URL and terminology, the recipient's playa name, the caller's `Vars` and `Link`), and the language choice.
- **The outbox.** Durable rows written on the caller's transaction, idempotent on `Key`, with status, attempts and a lease; the person's export and deletion of those rows; retention.
- **The sender.** One loop on the writing copy that leases due rows, resolves the address, hands the envelope to the channel, and records the outcome with backoff.
- **The SMTP channel.** RFC 5322 text messages over `net/smtp` with STARTTLS, implicit TLS or plaintext, PLAIN or CRAM-MD5 auth, a deadline per conversation; `Ping` for `bms check` and the admin screen.
- **The admin surface.** Templates (list, edit, reset, preview), a test message to oneself, the outbox (counts, recent failures, retry, cancel), each behind its permission and audited.

## Not responsible for

| Not this | Whose it is |
|---|---|
| Deciding who needs telling, and when | the caller: a module from its own records; `identity` (reset, alert through `app`), `people` (deletion requests) |
| Reading or storing SMTP settings, the sender address, the site name, URL, default language and terminology | `settings` (row 15); notify reads them through `SMTPSource`, `SiteSource`, `RetentionSource` |
| Who holds a permission (recipients of an admin notice) | `authz.HoldersOf` (proposed), called by the sender of the notice |
| The person's address and language | `identity` (`EmailOf`), `people` (`ContactEmailOf`, `LanguageOf`) through the seams |
| The password-reset token, its routes and screens | `identity` (Proposed change 1) |
| Scheduled and deferred work in general; retries of signal deliveries | `jobs` (row 14); notify's loop is its own (decision 2) |
| Receiving e-mail, bounces, unsubscribe lists | nobody in M4 (Later: a bounce address and a `Failed` reason from it) |
| HTML bodies, attachments, SMS and chat channels, per-person preferences, digests | Later, behind `Channel` and a preferences table (09 §6) |
| The browser catalogue of the admin screens' labels | `web-shell` (`core.notify.*`) |
| Logging and tracing policy | `telemetry`; notify emits IDs, tokens and counts only |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `Notifier`, `AddressSender`, `Message` | Let a caller ask for a notification without knowing the channel. | `Send` is scoped; `SendTo` is unscoped and never in `Deps`. |
| Catalogue (`Register`, `Render`, the template store) | Turn a template name, a language and a message into subject and body. | Defaults from `fs.FS`; overrides in `core.notify_template`. |
| Outbox (`message` record, `postgres` store) | Hold every message from enqueue to a final state, once. | `core.notify_message`; the unique `(owner, key, person_id)`. |
| Sender (`Run`, `SendOnce`) | Deliver due rows on the writing copy with retries. | One goroutine; `ReadOnly()` gates each tick. |
| `Channel`, `smtp` | Carry one envelope to one address over SMTP. | Stdlib only; Mailpit proves it. |
| Admin (`Routes`, screens) | Let a settings admin edit wording, test delivery and watch the queue. | Permissions `notify.templates.manage`, `notify.outbox.manage`. |
| `notifytest` | Fakes for modules and for the sender. | `Fake` (a `Notifier`), `Channel`. |

## Interface

```go
package notify

// Template names a message template, "<owner>.<ident>" (the signals.Name grammar:
// owner and ident [a-z][a-z0-9]*(-[a-z0-9]+)*, at most 96 bytes). Owner is a module
// name or a core area. Validate fails Invalid.
type Template string

func (t Template) Validate() error
func (t Template) Owner() string

// Language is "" (the site default) or a BCP 47 tag matching people's grammar
// ^[a-z]{2,3}(-[A-Za-z0-9]{1,8})*$, at most 35 bytes. Validate fails Invalid (malformed).
type Language string

type MessageID = model.ID[message]

type Status int // Queued = 1, Sending, Sent, Failed; zero invalid

// Message is what a caller sends. Vars carry nothing sensitive and nothing personal
// about anyone but the recipient (decision 10). Link is a path under the site URL
// ("/volunteers/shifts/..."), never a full URL.
type Message struct {
	Template Template
	To       []model.PersonID  // 1..MaxRecipients, no duplicates, no zero ID (Send); empty for SendTo
	Key      string            // optional; [A-Za-z0-9._:-]{1,128}; unique per (owner, key, recipient)
	Vars     map[string]string // at most MaxVars; keys [a-z][a-z0-9_]{0,31}; values valid UTF-8, no control characters but \n and \t, at most MaxVarBytes
	Link     string            // "" or a path: starts with "/", not "//", no scheme, no control characters, at most MaxLinkBytes
}

// Validate fails Invalid joining one validate.FieldError per problem: "template"
// (required, malformed), "to" (required, too_many {max}, malformed, duplicate), "key"
// (too_long {max}, malformed), "vars" (too_many {max}), "vars.<key>" (malformed,
// too_long {max}, control_chars), "link" (malformed, too_long {max}, control_chars).
func (m Message) Validate() error

// Notifier is module.Deps.Notify, scoped to one owner. Send validates m, refuses a template
// of another owner (Invalid, field "template", code "scope") or one not registered
// ("unknown"), renders one row per recipient in that recipient's language and inserts the
// rows on the transaction ctx carries (a standalone statement when it carries none). It
// never sends inline and never checks a permission (decision 12). A row whose (owner, key,
// recipient) already exists is skipped silently. Fails Unsupported wrapping
// ErrNotConfigured before looking at any recipient when SMTP or the sender is unset;
// Unavailable wrapping db.ErrReadOnly on a read-only copy; store kinds; the SiteSource's or
// LanguageSource's kind. Consumers: every module, identity (the reset mail), people (the
// deletion-request notice), app's alert adapter.
type Notifier interface {
	Send(ctx context.Context, m Message) error
}

// AddressSender sends one message to one explicit address instead of a person; m.To must
// be empty. The row stores the address (personal) until a final state. The language is the
// site default. Fails as Send, plus Invalid for a malformed address or a non-empty To.
// Consumers: identity (the e-mail change confirmation, M4), app's alert adapter
// (notify.alert_addresses). Never in module.Deps.
type AddressSender interface {
	SendTo(ctx context.Context, to model.Email, m Message) error
}

// ---- Channel ----

type Address struct {
	Name  string      // display name; may be ""
	Email model.Email
}

// Envelope is one rendered message for one address. MessageID is "<id>@<hostname>".
type Envelope struct {
	From      Address
	ReplyTo   *Address
	To        model.Email
	Subject   string
	Body      string // text/plain, UTF-8
	MessageID string
}

// Channel carries one envelope. Send fails Invalid wrapping ErrRejected when the server
// refuses the message or the recipient permanently (5xx), Unavailable when it cannot
// connect, authenticate or gets a transient reply (4xx), Timeout/Canceled from ctx. Ping
// connects, greets, authenticates and quits, with the same kinds. A Channel is cheap to
// construct and does no I/O until Send or Ping.
type Channel interface {
	Send(ctx context.Context, e Envelope) error
	Ping(ctx context.Context) error
}

// ChannelFactory builds the channel for the current SMTP settings; smtp.New by default.
// Fails Invalid when cfg does not validate. Consumers: the sender (per tick), Ping, tests
// (notifytest.Channel).
type ChannelFactory func(cfg SMTPConfig) (Channel, error)

// ErrRejected marks a permanent rejection (Invalid). Check with errors.Is.
var ErrRejected = errors.New("message rejected by the server")

// ErrNotConfigured marks "no SMTP host or sender address is set" (Unsupported).
var ErrNotConfigured = errors.New("e-mail is not configured")

// ---- Settings seams (filled by app over settings) ----

type Security int // StartTLS = 1, TLS, None; zero invalid

type SMTPConfig struct {
	Host           string        // "" means not configured
	Port           int           // 1..65535
	Security       Security
	Username       string        // "" means no auth
	Password       secret.Secret // unset means no auth
	Sender         Address       // required to send
	ReplyTo        *Address
	AlertAddresses []model.Email // at most 10; the maintainers' addresses for the superuser alert
}

// Validate fails Invalid with fields "notify.smtp_host", "notify.smtp_port",
// "notify.smtp_security", "notify.smtp_username", "notify.sender_address",
// "notify.reply_to", "notify.alert_addresses". Configured reports Host != "" and
// Sender.Email set.
func (c SMTPConfig) Validate() error
func (c SMTPConfig) Configured() bool

type SMTPSource interface {
	SMTP(ctx context.Context) (SMTPConfig, error)
}

// Site is what every template may use: the regional's name, the public URL (no trailing
// slash), the default language and the terminology for lang (key -> word).
type Site struct {
	Name            string
	URL             string
	DefaultLanguage Language
	Terms           map[string]string
}

type SiteSource interface {
	Site(ctx context.Context, lang Language) (Site, error)
}

// RetentionSource returns notify.retention: 0 keeps status rows forever.
type RetentionSource interface {
	Retention(ctx context.Context) (time.Duration, error)
}

// ---- Recipient seams ----

// AddressSource answers a person's current address: identity's login address, else
// people's contact address; NotFound when the person has none. Filled by app.
type AddressSource interface {
	AddressOf(ctx context.Context, person model.PersonID) (model.Email, error)
}

// LanguageSource answers a person's saved language, "" when unset or unknown. Filled by
// app over people.LanguageOf.
type LanguageSource interface {
	LanguageOf(ctx context.Context, person model.PersonID) (Language, error)
}

// NameSource answers a person's playa name, "" when unknown (identity's NameSource shape).
// Filled by app over people.PlayaNameOf.
type NameSource interface {
	PlayaNameOf(ctx context.Context, person model.PersonID) (string, error)
}

// ---- The service ----

type Options struct {
	DB           db.DB
	Clock        clock.Clock
	Logger       *slog.Logger
	Tracer       telemetry.Tracer
	Meter        telemetry.Meter // optional
	Audit        audit.Recorder  // scoped "notify"
	Authz        authz.Authorizer
	Actor        authz.ActorFunc
	SMTP         SMTPSource
	Site         SiteSource
	Retention    RetentionSource // optional; nil keeps forever
	Addresses    AddressSource
	Languages    LanguageSource
	Names        NameSource
	Channel      ChannelFactory  // nil means smtp.New
	PollInterval time.Duration   // 1s..5m; DefaultPollInterval
	Lease        time.Duration   // 30s..30m; DefaultLease
	SendTimeout  time.Duration   // 5s..5m; DefaultSendTimeout (one SMTP conversation)
	Hostname     string          // for Message-ID and EHLO; config's instance name by default; [a-z0-9.-]{1,253}
}

// Validate fails Invalid joining one validate.FieldError per problem (fields listed in
// the Summary).
func (o Options) Validate() error

// New validates o, registers notify's own templates (Templates) and the metrics, and
// returns the service. No I/O. Fails Invalid.
func New(o Options) (*Service, error)

// Templates holds notify.test (en, es).
var Templates fs.FS

// Register adds an owner's default templates: files "<ident>.<lang>.tmpl" at the root.
// Every ident needs an "en" file. Fails Invalid (file name, header, parse error, size,
// subject length) naming the file, Conflict when owner was registered before, Internal
// after Run started. Consumers: app (identity's, people's and each module's
// Manifest.Templates at build; bms check validates every template this way).
func (s *Service) Register(owner string, defaults fs.FS) error

// Scoped returns the Notifier for one owner. Fails Invalid for a bad owner. Consumer: app's
// provision (Deps.Notify), the core parts' lazy adapters.
func (s *Service) Scoped(owner string) (Notifier, error)

// SendTo implements AddressSender (unscoped; app wires it to identity and its adapter).
func (s *Service) SendTo(ctx context.Context, to model.Email, m Message) error

// Run is the sender loop: every PollInterval, when !DB.ReadOnly(), SendOnce; every
// PurgeInterval, Purge. Returns when ctx is done (nil) or after Close. Consumer: app (a
// runnable, started after Set.Start, stopped after HTTP drained).
func (s *Service) Run(ctx context.Context) error

// SendOnce leases up to MaxBatch due rows and delivers them, returning counts. Fails
// Unavailable wrapping db.ErrReadOnly (nothing leased), store kinds. Delivery failures are
// recorded on the rows, not returned. Consumers: Run; tests; app's e2e.
func (s *Service) SendOnce(ctx context.Context) (Sent, error)

// Ping builds a channel from the current settings and checks the conversation. Fails
// Unsupported wrapping ErrNotConfigured, the channel's kinds. Consumers: bms check, the
// outbox screen's "check connection".
func (s *Service) Ping(ctx context.Context) error

// Purge deletes Sent and Failed rows older than the retention; 0 deletes nothing. Fails
// Unavailable wrapping db.ErrReadOnly, store kinds. Consumer: Run (later jobs).
func (s *Service) Purge(ctx context.Context) (int64, error)

// Admin use cases; each checks its permission first (notify.templates.manage for the
// template methods and SendTest, notify.outbox.manage for Outbox, Retry, Cancel).
func (s *Service) ListTemplates(ctx context.Context) ([]TemplateInfo, error)
func (s *Service) GetTemplate(ctx context.Context, name Template, lang Language) (TemplateView, error)
func (s *Service) PutTemplate(ctx context.Context, name Template, lang Language, t TemplateText) (TemplateView, error)
func (s *Service) ResetTemplate(ctx context.Context, name Template, lang Language) error
func (s *Service) Preview(ctx context.Context, name Template, lang Language, t TemplateText) (Rendered, error)
func (s *Service) SendTest(ctx context.Context) (MessageID, error)
func (s *Service) Outbox(ctx context.Context) (OutboxView, error)
func (s *Service) Retry(ctx context.Context, id MessageID) error
func (s *Service) Cancel(ctx context.Context, id MessageID) error

// PersonData for people (app registers notify as a people.Source).
func (s *Service) ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
func (s *Service) DeletePerson(ctx context.Context, person model.PersonID) (int, error)

func (s *Service) Routes() []Route
func (s *Service) OpenAPI() []byte

// Close stops Run and waits for an in-flight batch under ctx. Idempotent.
func (s *Service) Close(ctx context.Context) error
```

```go
package smtp // internal/core/notify/smtp

// New builds a notify.Channel for cfg (notify.ChannelFactory). Fails Invalid when cfg
// does not validate or is not configured. Options: SendTimeout, Hostname, Clock.
func New(cfg notify.SMTPConfig, opts Options) (notify.Channel, error)
```

```go
package notifytest

// NewFake returns a Notifier scoped to owner that validates like the real one, records
// messages after the ctx's transaction commits (dbtest.Fake runs AfterCommit hooks), and
// fails every Send with the error given to Fail.
func NewFake(owner string) *Fake
func (f *Fake) Send(ctx context.Context, m notify.Message) error
func (f *Fake) Sent() []notify.Message
func (f *Fake) Fail(err error)

// Channel is an in-memory notify.Channel.
type Channel struct{ ... }
func NewChannel() *Channel
func (c *Channel) Envelopes() []notify.Envelope
func (c *Channel) Fail(err error) // every Send fails with err until cleared
func (c *Channel) Reject()        // every Send fails Invalid wrapping ErrRejected
func (c *Channel) Factory() notify.ChannelFactory
```

### Method table

| Method | Consumer | Preconditions | Postconditions |
|---|---|---|---|
| `Send` | modules, identity, people, app's adapter | valid message; owner's template; SMTP configured | one `Queued` row per recipient on the ctx transaction, rendered; duplicates by `Key` skipped; `bms.notify.enqueued` after commit |
| `SendTo` | identity, app's adapter | explicit address; `To` empty | one row with `address` set, `person_id` NULL |
| `Register` | app (build) | before `Run`; owner once | templates parsed and held in memory |
| `Scoped` | app's provision | valid owner | a `Notifier` bound to the owner |
| `Run`/`SendOnce` | app (runnable), tests | writing copy | due rows delivered or rescheduled; final rows cleared of text |
| `Ping` | `bms check`, the outbox screen | configured | the server accepted EHLO and auth |
| `Purge` | `Run` | retention set | old final rows deleted; `bms.notify.purged` |
| `ListTemplates`..`Preview`, `SendTest` | the templates screens | actor with `notify.templates.manage` | override saved or removed, audited; a test row to the actor |
| `Outbox`, `Retry`, `Cancel` | the outbox screen | actor with `notify.outbox.manage` | counts and recent rows; a `Failed` row requeued; a `Queued`/`Failed` row canceled (deleted) |
| `ExportPerson`, `DeletePerson` | people (through app) | — | the person's rows as JSON; rows deleted (queued ones never sent) |
| `Close` | app (closer) | — | loop stopped |

## Types

Uses `model.PersonID`, `model.Email`, `model.ID`, `secret.Secret`, `clock.Clock`, `retry.Policy`, `audit.Recorder`, `authz.Authorizer`, `authz.ActorFunc`, `authz.Permission`, `authz.PermissionDecl`, `telemetry.Tracer`, `telemetry.Meter`, `db.DB`, `db.Set`. Defines `Template`, `Language`, `MessageID`, `Status`, `Message`, `Envelope`, `Address`, `SMTPConfig`, `Security`, `Site`, `Record`, `Rendered`, `TemplateInfo`, `LanguageInfo`, `TemplateText`, `TemplateView`, `OutboxView`, `Sent`, `Options`, `Route`. The record type `message.Record` lives in `notify/message` (imports `model` and `lib` only) because `postgres/` cannot import the root.

**Template file grammar.** UTF-8, at most `MaxTemplateBytes`. Header lines until the first blank line: `Subject: <template>` (required, one line, renders to at most `MaxSubjectBytes`), `Vars: a, b` (optional; the var names the owner passes, shown on the edit screen and used by `Preview` with each name as its placeholder value). Everything after the blank line is the body template. Data available: `.Site.Name`, `.Site.URL`, `.Recipient.PlayaName` (`""` for an explicit address), `.Link` (absolute: `Site.URL + Message.Link`, `""` when none), `.Vars.<key>`; funcs: `term "<key>"` (the regional's word for a key, the key itself when unset). Options: `missingkey=zero`. Nothing else: no `html/template`, no other funcs, no includes.

**Language resolution.** `LanguageSource.LanguageOf(person)`, else `Site.DefaultLanguage`, else `en`; for each candidate try the exact tag, then its primary subtag; an override in `core.notify_template` beats the shipped default for the same tag; the first hit wins; `en` is guaranteed by `Register`.

## Data

Migration set `notify`, schema `core`, `0001_notify.sql`.

**`core.notify_message`** (the outbox)

| Column | Type | Constraints | Sensitivity | Notes |
|---|---|---|---|---|
| `id` | uuid | PK | — | `MessageID` |
| `owner` | text | NOT NULL, `<= 32` | — | the scope that sent it |
| `template` | text | NOT NULL, `<= 96` | — | full name |
| `key` | text | NULL, `<= 128` | — | idempotency; UNIQUE `(owner, key, person_id)` WHERE key IS NOT NULL |
| `person_id` | uuid | NULL, FK `core.person(id)` ON DELETE RESTRICT | internal | the recipient; NULL for `SendTo` |
| `address` | text | NULL, `<= 254` | personal | explicit address (`SendTo`); NULL when resolved from the person; set NULL on a final state |
| `language` | text | NOT NULL | internal | the tag rendered |
| `subject` | text | NULL, `<= 256` | personal | rendered; NULL on a final state |
| `body` | text | NULL, `<= 65536` | personal (may hold a one-time link) | rendered; NULL on a final state |
| `status` | text | NOT NULL, CHECK in (`queued`,`sending`,`sent`,`failed`) | — | |
| `attempts` | int | NOT NULL DEFAULT 0 | — | |
| `next_attempt_at` | timestamptz | NOT NULL | — | due time |
| `leased_until` | timestamptz | NULL | — | set while `sending` |
| `last_error` | text | NULL, `<= 32` | — | a token: `connect`, `auth`, `timeout`, `smtp_4xx`, `rejected`, `no_address`, `no_smtp`, `canceled` |
| `created_at`, `sent_at`, `failed_at` | timestamptz | `created_at` NOT NULL | — | |

Indexes: `(status, next_attempt_at)` WHERE status IN (`queued`,`sending`); `(person_id)`; `(created_at)` for purge. Why personal columns are needed: the sender needs the rendered text and, for `SendTo`, the address until delivery; both are cleared at the final state (decision 11). Retention: status rows until `notify.retention` (default forever, D30); content never outlives delivery. Export: the person's rows (`id, template, status, language, created_at, sent_at, subject` when still present). Deletion: rows deleted (people's transaction; notify is registered as a `people.Source` so its `DeletePerson` runs before the FK would refuse).

**`core.notify_template`** (overrides)

| Column | Type | Constraints | Sensitivity | Notes |
|---|---|---|---|---|
| `name` | text | PK part, `<= 96` | — | template name |
| `language` | text | PK part, `<= 35` | — | |
| `subject` | text | NOT NULL, `<= 1024` | internal | the subject template |
| `body` | text | NOT NULL, `<= 16384` | internal | the body template |
| `updated_by` | uuid | NULL, FK `core.person(id)` ON DELETE SET NULL | internal | |
| `updated_at` | timestamptz | NOT NULL | — | |

Nothing personal. An override for a name that is not registered in this installation (a disabled module) is kept and shown as "not available", like authz's undeclared permissions.

## Permissions

| Permission | Allows | Scoped by | Default roles |
|---|---|---|---|
| `notify.templates.manage` | list, read, edit, reset and preview templates; send a test message to oneself | site | `settings-admin` |
| `notify.outbox.manage` | see the outbox counts and recent rows (template, person ID, status, attempts, error token: never an address or a body), retry a failed row, cancel a queued or failed row, check the connection | site | `settings-admin` |

Both reach `internal`. `Send`/`SendTo` check no permission (decision 12). `ExportPerson`/`DeletePerson` run under people's permission checks.

## Signals

Published: none. A `notify.message-sent` signal has no consumer in M4; it is added when one appears. Subscribed: none (decision 14): the area that knows who to tell calls `Send`; notify never decides recipients.

## Settings

Owned by notify and declared as `settings.Decl`s in the core area `notify` (`notify.Settings []settings.Decl`, exported like identity's); stored and checked by `settings` (row 15; key grammar `[a-z][a-z0-9]*(_[a-z0-9]+)*` after the area); `app` reads them through `settings.Reader` scoped `notify` and `Regional` and writes the `SMTPSource`, `SiteSource` and `RetentionSource` adapters. Changes take effect at the next tick or `Send`.

| Key | Type | Default | Valid range | Who can change it |
|---|---|---|---|---|
| `notify.smtp_host` | text | `""` (off) | `""` or a host name or IP, `<= 253`; field code `malformed` | settings admin |
| `notify.smtp_port` | int | 587 | 1..65535; `range` | settings admin |
| `notify.smtp_security` | enum | `starttls` | `starttls`, `tls`, `none`; `one_of` (`none` is for a local relay or Mailpit; `bms check` warns) | settings admin |
| `notify.smtp_username` | text | `""` | `<= 255`; `too_long` | settings admin |
| `notify.smtp_password` | secret (settings' sealed `Secret` kind) | unset | `<= 1024` bytes; sealed at rest, never shown back | settings admin |
| `notify.sender_address` | text | `""` | `""` or `model.ParseEmail`; `malformed` | settings admin |
| `notify.sender_name` | text | `""` (= `regional.name`) | `<= 100`, no control characters; `too_long`, `control_chars` | settings admin |
| `notify.reply_to` | text | `""` | `""` or an address; `malformed` | settings admin |
| `notify.alert_addresses` | text | `""` | comma-separated addresses, at most 10; `malformed`, `too_many {max}` | settings admin |
| `notify.retention` | duration | `0` (forever) | `0` or `168h` (`MinRetention`, 7 days)..`876000h` (100 years), written in hours because the `Duration` kind is `time.ParseDuration`; `range` | settings admin |

Expected from settings (not owned here; settings.md, approved 2026-10-19, names them so): `Regional` with `regional.name`, `regional.public_url`, `regional.language` (D28), and `TermsFor(ctx, lang)` (09 §9; the `term` func uses the singular form, D38 (af) adds the plural and title-case forms as `term_plural`, `term_title`, `term_plural_title` when settings ships them). `app` writes `SiteSource` over `Regional` and `TermsFor`, `SMTPSource` and `RetentionSource` over `Scoped("notify")`.

## UI

Core area `web/src/core/notify/`, keys `core.notify.*`, phone-first, visible to holders of either permission (`NavEntry.access`).

- **Templates** (`/notify/templates`; `notify.templates.manage`). A list of every registered template grouped by owner (title `module.<owner>.template.<ident>.title` or `core.<owner>.template.<ident>.title`), with a chip per language: shipped or overridden. A "not available" group lists overrides for unregistered names with a remove action. Empty: "No templates are registered" (only when no module and no core area shipped any; in practice never). Error: `ErrorState` with the reference. Forbidden: not reachable; a direct visit shows the shell's forbidden state.
- **Edit template** (`/notify/templates/:name/:lang`). The subject and body fields prefilled with the current text, the shipped default shown read-only below, the list of `Vars` and the standard fields as copyable chips, a "Preview" button (renders with placeholders, shows subject and body), "Save", "Reset to default" (confirm dialog), and "Send me a test" (sends `notify.test`, shows the message ID and a link to the outbox). Validation errors appear on the field with the parser's line. Read-only copy: fields disabled, banner.
- **Outbox** (`/notify/outbox`; `notify.outbox.manage`). A status card: configured or not (with a link to settings), "Check connection" (`Ping`; shows ok or the error reference), counts by status; then the recent rows (template, recipient's playa name through the people names route or "address" for explicit ones, status, attempts, error token, created), each with "Retry" (failed) or "Cancel" (queued, failed). Empty: "Nothing waiting". Never an address, subject or body.

## Behavior

- **Enqueue.** `Send`: (1) validate `m`; (2) `SMTPSource.SMTP` and `Configured()`, else `Unsupported` wrapping `ErrNotConfigured` (before any recipient is looked at, so the outcome never depends on who the recipient is); (3) the template must be registered for the owner; (4) for each recipient: `LanguageOf`, resolve the language, `SiteSource.Site(lang)`, render subject and body (`Invalid` if the rendered subject exceeds `MaxSubjectBytes` or the body `65536`); (5) insert the rows with `status = queued`, `next_attempt_at = now`, on the ctx's transaction (`db.TxMode`); `ON CONFLICT (owner, key, person_id) DO NOTHING`; (6) `db.AfterCommit`: count `bms.notify.enqueued{owner}`. `Send` inside a `Read` transaction is `Internal` (like audit decision 12). `.Recipient.PlayaName` comes from `NameSource.PlayaNameOf` (app wires `people.PlayaNameOf`; `""` when unknown); a failure of `Names`, `Languages` or `Site` fails `Send` with its kind (F11).
- **Sending.** `Run` ticks on `Clock` every `PollInterval`. A tick: if `DB.ReadOnly()` skip (`outcome=read_only` once per flip, not per tick); else `SendOnce`: (1) `SMTP`; if not configured, mark nothing, log once per hour that `N` messages wait (`last_error` stays, `attempts` untouched), `outcome=no_smtp`; (2) in one short `Write` transaction lease: `UPDATE core.notify_message SET status='sending', leased_until = now + Lease, attempts = attempts + 1 WHERE id IN (SELECT id FROM core.notify_message WHERE (status='queued' AND next_attempt_at <= now) OR (status='sending' AND leased_until < now) ORDER BY next_attempt_at LIMIT MaxBatch FOR UPDATE SKIP LOCKED) RETURNING ...`; (3) outside any transaction, for each row: the address (`address` column, else `AddressSource.AddressOf(person_id)`; `NotFound` → `failed`, `no_address`); build the envelope; `Channel.Send` under `SendTimeout`; (4) record: success → `sent`, `sent_at`, text and address cleared; `Invalid` wrapping `ErrRejected` → `failed`, `rejected`; `Unavailable`/`Timeout` → if `attempts < MaxAttempts` then `queued` with `next_attempt_at = now + policy.Delay(attempts)` (`retry.Policy{Initial: 1m, Max: 1h, Multiplier: 2, MaxAttempts: MaxAttempts}`), else `failed` with the token; `Canceled` → `queued`, `next_attempt_at = now`, attempts not counted (shutdown), stop the batch; an `ErrReadOnly` on any update → stop the batch, leave the lease (it expires). Each row is one `notify.deliver` Client span with `telemetry.ID("message_id")`, `Token("template")`, `Token("outcome")`; the metric `bms.notify.delivered{outcome: ok|retry|failed|rejected|no_address|no_smtp|read_only}`. After each tick the loop stores the counts by status for the `bms.notify.outbox{status}` gauge.
- **Never twice.** The `Key` unique index makes a repeated `Send` (a redelivered signal, a retried job) a no-op; the lease stops two processes on one database from sending the same row; a crash after the server accepted and before the row update resends once when the lease expires (decision 5). `Retry` requeues a `Failed` row with `attempts = 0`.
- **Templates.** `Register` parses every file with `text/template` (`Option("missingkey=zero")`, funcs `term`), checks the headers and sizes, and keeps the parsed defaults in memory. `PutTemplate` parses the override and renders it with placeholder vars; a parse failure is `Invalid` (`subject`/`body`, code `parse`, `Reason` the parser's message with the line) and the override is not saved; a saved override is read on the next render (one row read per render, cached per tick of the sender; no restart). `ResetTemplate` deletes the row. Both record `notify.template.changed`/`.reset` in the `Write` transaction with `Record = Ref{Kind: "notify.template", ID: sha256("notify.template:" + name + ":" + lang)[:16]}`, `Reason` `template:<name>:<lang>`.
- **Test message.** `SendTest` sends `notify.test` to the actor (`To: [actor]`), audited `notify.test.sent`, returns the ID so the screen can link to the outbox; it goes through the normal queue so it tests the whole path.
- **Outbox screen.** `Outbox` returns counts (`SELECT status, count(*)`), `Configured`, and the `MaxOutboxList` most recent rows with `status IN ('queued','sending','failed')`; never `address`, `subject` or `body`. `Retry` and `Cancel` are audited (`notify.message.retried|canceled`, `Record = Ref{Kind: "notify.message", ID}`); `Cancel` deletes the row.
- **Read-only copy.** `Send` fails `Unavailable` wrapping `db.ErrReadOnly` (inside a `Write` transaction the insert returns it like the caller's change; outside, the standalone insert returns it; no spool, decision 7). The loop skips while `ReadOnly()` and stops a batch on `ErrReadOnly` (db.md Proposed change 13). The admin reads work; writes fail read-only.
- **Purge.** Every `PurgeInterval` of `Run`, when `Retention` > 0: `DELETE ... WHERE status IN ('sent','failed') AND coalesce(sent_at, failed_at) < now - retention LIMIT 1000` in a loop until none; counted in `bms.notify.purged`.
- **Export and deletion.** `ExportPerson` returns `{"messages": [...]}` with no `body` and no `address`; `DeletePerson` deletes the person's rows inside people's transaction (the ctx it was given; never a new `Transact`); queued rows are never sent.
- **Privacy.** No log line, span, metric or error message carries an address, a subject, a body, a var or a link: messages are logged by ID, templates by name, failures by token; an SMTP server's reply text (which may echo the address) is mapped to a token and dropped; `telemetrytest` proves it. Vars and links are the caller's responsibility under decision 10, and the `volunteers` spec (and every module spec) names what it passes.
- **Concurrency and lifecycle.** `*Service` is safe for concurrent use; `Run` is one goroutine; `Close` cancels it and waits for the in-flight batch under ctx; `Register` and `Scoped` after `Run` started are `Internal`. app: part 9 `declare` (the set, `Permissions`), `build` (`New`, `Register` for `identity.Templates`, `people.Templates` and `notify.Templates`; `a.deps.Notify` scoped by provision; route set `notify`; the runnable `Run`/`Close`), `link` (`Register` each built module's `Manifest.Templates`; people's `Source{Name: "notify"}`); `bms check` prints `e-mail: configured host:port (security)` or `e-mail: not configured`, the outbox counts, and `smtp: ok` or the `Ping` failure (never failing the check); the lazy adapters for identity's `Alerter` and people's `Notifier` resolve `a.notify` at call time.
- **Telemetry.** Spans `notify.send` (Internal; `Token("template")`, `Int("recipients")`), `notify.tick`, `notify.deliver` (Client), `notify.<use-case>`. Metrics above. Log lines: `notify.send` failures are the caller's to log; the sender logs one warn per final failure (`message_id`, `template`, `error` token) and one info per `no_smtp` hour.

- **Implementation notes (implementation, 2026-10-19).**
  - The sent status constant is `StatusSent` (`Queued`, `Sending`, `StatusSent`, `Failed`), because `Sent` is the name of `SendOnce`'s result struct and Go allows one name per package scope; `Status.String()` and the stored value stay `sent`.
  - A malformed var key reports field `vars` with code `malformed`, never `vars.<key>`, so caller input is not echoed; `vars.<key>` is used only for `too_long` and `control_chars`.
  - `PutTemplate` and `Preview` accept a language for which the template ships no text (the English default is shown as the base); `GetTemplate` returns `NotFound` for a language with neither a shipped text nor an override.

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `Message.Validate` | template empty or malformed | Invalid (`template`: `required`/`malformed`) | fix the call | `TestMessageValidate_Template_Table` |
| F2 | `Message.Validate` | `To` empty, > `MaxRecipients`, zero ID, duplicate | Invalid (`to`: `required`/`too_many`/`malformed`/`duplicate`) | fix the call | `TestMessageValidate_To_Table` |
| F3 | `Message.Validate` | `Key` too long or malformed | Invalid (`key`) | fix the call | `TestMessageValidate_Key_Table` |
| F4 | `Message.Validate` | too many vars; bad key; value too long, bad UTF-8 or control characters | Invalid (`vars`, `vars.<key>`) | fix the call | `TestMessageValidate_Vars_Table` |
| F5 | `Message.Validate` | `Link` with a scheme, `//`, no leading `/`, too long | Invalid (`link`) | fix the call | `TestMessageValidate_Link_Table` |
| F6 | `Send` | SMTP or sender unset | Unsupported wrapping `ErrNotConfigured` | 501 `notify_off`; identity answers uniformly | `TestSend_NotConfigured_Unsupported` |
| F7 | `Send` | template of another owner | Invalid (`template`: `scope`) | fix the call | `TestSend_OtherOwner_InvalidScope` |
| F8 | `Send` | template not registered | Invalid (`template`: `unknown`) | fix the call | `TestSend_UnknownTemplate_Invalid` |
| F9 | `Send` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | 503 `read_only` | `TestSend_ReadOnly_Unavailable` |
| F10 | `Send` | inside a `db.Read` transaction | Internal | programming error | `TestSend_InReadTx_Internal` |
| F11 | `Send` | `LanguageSource`, `SiteSource` or `NameSource` fails | that kind | fail the use case | `TestSend_SeamFails_Propagates` |
| F12 | `Send` | rendered subject > `MaxSubjectBytes` or body > 65536 | Invalid (`template`: `too_long`) | fix the template | `TestSend_RenderedTooLong_Invalid` |
| F13 | `Send` | same (owner, key, recipient) exists | nil; row skipped; nothing counted | nothing | `TestSend_DuplicateKey_Skipped` |
| F14 | `Send` | store fails | Unavailable/Timeout/Canceled | fail the use case | `TestSend_StoreFails_Unavailable` |
| F15 | `Send` | transaction rolls back | no row, no metric | nothing | `TestSend_Rollback_NoRow` |
| F16 | `SendTo` | malformed address or non-empty `To` | Invalid (`to`) | fix the call | `TestSendTo_Invalid_Table` |
| F17 | `Register` | file name not `<ident>.<lang>.tmpl`, bad header, parse error, oversize, no `en` | Invalid naming the file | fix the template; `bms check` exits 2 | `TestRegister_BadTemplates_Table` |
| F18 | `Register` | owner registered twice | Conflict | programming error | `TestRegister_Twice_Conflict` |
| F19 | `Register`/`Scoped` | after `Run` started | Internal | programming error | `TestRegister_AfterRun_Internal` |
| F20 | `SendOnce` | read-only copy | Unavailable wrapping `db.ErrReadOnly`; nothing leased | `Run` waits for the next tick | `TestSendOnce_ReadOnly_NothingLeased` |
| F21 | `SendOnce` | not configured | nil; `Sent{Skipped: n}`; attempts untouched; one info per hour | nothing | `TestSendOnce_NotConfigured_SkipsWithoutAttempt` |
| F22 | `SendOnce` | recipient has no address | row `failed`, `no_address` | admin sees it on the outbox | `TestSendOnce_NoAddress_FailedFinal` |
| F23 | `SendOnce` | channel `Unavailable` (connect, auth, 4xx) | row `queued`, `next_attempt_at` by backoff, `last_error` token | nothing | `TestSendOnce_Transient_RequeuedWithBackoff` |
| F24 | `SendOnce` | channel `Timeout` | as F23, token `timeout` | nothing | `TestSendOnce_Timeout_Requeued` |
| F25 | `SendOnce` | channel `Invalid` wrapping `ErrRejected` | row `failed`, `rejected` | admin retries after fixing | `TestSendOnce_Rejected_FailedFinal` |
| F26 | `SendOnce` | `attempts` reaches `MaxAttempts` on a transient failure | row `failed`, last token | admin retries | `TestSendOnce_MaxAttempts_Failed` |
| F27 | `SendOnce` | ctx canceled mid-batch | rows left `queued` due now, attempt not counted; Canceled | `Run` exits | `TestSendOnce_Canceled_RowsReleased` |
| F28 | `SendOnce` | update after a successful send fails read-only | lease left; row resent once after `Lease` | none (decision 5) | `TestSendOnce_UpdateFailsAfterSend_ResentAfterLease` |
| F29 | `SendOnce` | two senders on one database | each row sent once (`SKIP LOCKED`) | — | `TestSendOnce_TwoWorkers_EachRowOnce` (integration) |
| F30 | `SendOnce` | a `sending` row whose lease expired | re-leased and sent | — | `TestSendOnce_ExpiredLease_Reclaimed` |
| F31 | `Run` | ctx done | returns nil; in-flight batch finishes under `Close`'s ctx | — | `TestRun_Stops_OnCancel` |
| F32 | `Run` | `SendOnce` fails `Unavailable` (database down) | logged once per state change, counted; next tick retries | — | `TestRun_DatabaseDown_LogsOnceAndRetries` |
| F33 | `Ping` | not configured | Unsupported wrapping `ErrNotConfigured` | screen says "not configured" | `TestPing_NotConfigured_Unsupported` |
| F34 | `Ping` | cannot connect or authenticate | Unavailable | screen shows the reference | `TestPing_Unreachable_Unavailable` |
| F35 | `Purge` | retention 0 | nil, 0 | nothing | `TestPurge_Forever_DeletesNothing` |
| F36 | `Purge` | read-only | Unavailable wrapping `db.ErrReadOnly` | `Run` ignores until writable | `TestPurge_ReadOnly_Unavailable` |
| F37 | admin methods | no actor | Unauthenticated | sign in | `TestAdmin_NoActor_Unauthenticated` |
| F38 | template methods, `SendTest` | no `notify.templates.manage` | Permission | forbidden | `TestTemplates_NoPermission_Permission` |
| F39 | `Outbox`, `Retry`, `Cancel` | no `notify.outbox.manage` | Permission | forbidden | `TestOutbox_NoPermission_Permission` |
| F40 | `GetTemplate`/`PutTemplate`/`ResetTemplate` | unknown name, or language malformed | NotFound; Invalid (`language`: `malformed`) | 404 / field error | `TestGetTemplate_Unknown_NotFound`, `TestPutTemplate_BadLanguage_Invalid` |
| F41 | `PutTemplate`/`Preview` | subject or body empty, too long, control characters, parse error | Invalid (`subject`/`body`: `required`/`too_long`/`control_chars`/`parse`) | field error with the line | `TestPutTemplate_Validation_Table` |
| F42 | `PutTemplate`/`ResetTemplate` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | 503 | `TestPutTemplate_ReadOnly_Unavailable` |
| F43 | `PutTemplate`/`ResetTemplate`/`SendTest`/`Retry`/`Cancel` | audit `Record` fails | the audit error; nothing saved | show the reference | `TestPutTemplate_AuditFails_NothingSaved` |
| F44 | `ResetTemplate` | no override exists | nil (idempotent) | nothing | `TestResetTemplate_NoOverride_Nil` |
| F45 | `SendTest` | the actor has no address | nil; the row fails `no_address` later | outbox shows it | `TestSendTest_NoAddress_RowFails` |
| F46 | `SendTest` | not configured | Unsupported wrapping `ErrNotConfigured` | screen says so | `TestSendTest_NotConfigured_Unsupported` |
| F47 | `Retry` | unknown ID | NotFound | 404 | `TestRetry_Unknown_NotFound` |
| F48 | `Retry` | row not `Failed` | Conflict (`status`) | 409 | `TestRetry_NotFailed_Conflict` |
| F49 | `Cancel` | row `Sending` or `Sent` | Conflict (`status`) | 409 | `TestCancel_WrongStatus_Conflict` |
| F50 | `ExportPerson` | zero ID | Invalid | — | `TestExportPerson_ZeroID_Invalid` |
| F51 | `DeletePerson` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | people rolls back | `TestDeletePerson_ReadOnly_Unavailable` |
| F52 | `DeletePerson` | store fails | Unavailable | people rolls back | `TestDeletePerson_StoreFails_Unavailable` |
| F53 | `New`/`Options.Validate` | nil dependency, range, bad hostname | Invalid with the field names | fix the assembly | `TestOptionsValidate_Table` |
| F54 | `SMTPConfig.Validate` | bad host, port, security, address | Invalid with the setting keys | settings refuses the save | `TestSMTPConfigValidate_Table` |
| F55 | `smtp.New` | not configured or invalid | Invalid | — | `TestSMTPNew_Invalid_Table` |
| F56 | `smtp.Send` | connection refused, DNS failure, TLS handshake failure, STARTTLS unsupported when required | Unavailable | sender requeues | `TestSMTPSend_Connect_Unavailable` (integration, closed port) |
| F57 | `smtp.Send` | authentication refused | Unavailable (`auth`) | sender requeues; admin fixes settings | `TestSMTPSend_AuthRefused_Unavailable` (fake server) |
| F58 | `smtp.Send` | server replies 4xx | Unavailable (`smtp_4xx`) | requeued | `TestSMTPSend_4xx_Unavailable` (fake server) |
| F59 | `smtp.Send` | server replies 5xx to MAIL, RCPT or DATA | Invalid wrapping `ErrRejected` | final | `TestSMTPSend_5xx_Rejected` (fake server) |
| F60 | `smtp.Send` | deadline passes mid-conversation | Timeout | requeued | `TestSMTPSend_Deadline_Timeout` (fake server that stalls) |
| F61 | `smtp.Send` | non-ASCII subject or name | encoded (RFC 2047), delivered | — | `TestSMTPSend_NonASCII_Encoded` (Mailpit) |
| F62 | any | a log, span or metric would carry an address, subject, body, var or token | never; `telemetrytest` fails the test | — | `TestNoPersonalDataInTelemetry` |
| F63 | `Routes` | a handler receives a body over the cap, malformed JSON | Invalid (api's `ReadJSON`) | 400 | `TestHandlers_BadBody_Invalid` |
| F67 (amendment 2026-10-19) | `Message.Validate` | `TTL` negative or above `MaxAge` | Invalid (`ttl`: `range`) | fix the call | `TestMessageValidate_TTL_Table` |
| F68 (amendment 2026-10-19) | `Retry` | row `Failed` but past `expires_at` (text cleared) | Conflict (`status`: `expired`) | screen says to trigger the action again | `TestRetry_Expired_Conflict` |
| F69 (amendment 2026-10-19) | `Retry` | row `Failed`, text present | nil; `queued`, `attempts = 0`, text sent on the next tick | — | `TestRetry_Failed_SendsOriginalText` |
| F70 (amendment 2026-10-19) | `ExpireStale` | queued, failed or lease-expired rows past `expires_at` | rows `failed`/`expired`, text and address NULL; a live lease is left alone | — | `TestExpireStale_ClearsStaleRows_KeepsLiveLease` |
| F71 (amendment 2026-10-19) | `SendOnce` | a queued row whose `expires_at` passed before the lease | never leased, so not sent; stays queued with its text until the next `ExpireStale` sweep (at most `PurgeInterval`), which ends it `failed`/`expired` and clears it | — | `TestSendOnce_ExpiredBeforeLease_NotSent` |
| F72 (amendment 2026-10-19) | `SendTo` | same (owner, key, address) while the first row is live | nil; second row skipped; after the first is sent, a repeat inserts again (accepted) | nothing | `TestSendTo_SameKeyAddress_DedupedWhileLive` |

Cannot fail: `Template.Owner`, `Status.String`, `SMTPConfig.Configured`, `Scoped` for a valid owner before `Run` (a map lookup), `Routes`, `OpenAPI` (embedded bytes), `notifytest` accessors.

## Lock-in check

External technology wrapped: SMTP (a protocol, no vendor). The channel uses only `net/smtp`, `crypto/tls`, `net/mail`, `mime` from the standard library (no third-party client; `net/smtp` is frozen but complete for PLAIN, CRAM-MD5 and STARTTLS; implicit TLS is `tls.Dial` plus `smtp.NewClient`). Proven portable on **Mailpit** (MIT) in the compose stack; any relay (Postfix, a hosted provider's SMTP endpoint) works unchanged. The `Channel` seam is what an SMS or chat channel implements later. Templates are `text/template` (stdlib). No hosted API is assumed anywhere.

## Test plan

- **Unit (no infrastructure).** `Message.Validate` tables (F1–F5); `Send` over `dbtest.Fake` with `notifytest`-style seam fakes (a fixed `SMTPConfig`, a `Site`, a language map, a names map): rows and their rendered text, the duplicate skip, the read-only kinds, the `Unsupported` ordering (F6 before any seam call), the `AfterCommit` metric; the renderer: language fallback chain, `term`, `missingkey=zero`, `Link` prefixing, subject cap; `Register` tables over `fstest.MapFS`; the sender over `dbtest.Fake`'s recorded calls and `notifytest.NewChannel()` with a fake clock: backoff schedule (`retry.Policy`), final states, lease reclaim, cancel; `Purge`; the admin use cases with `authztest.NewFake()` and `audittest.NewFake("notify")`; `Options.Validate` and `SMTPConfig.Validate` tables; every test under `telemetrytest.New(t)` asserting `!c.Contains(address)` with invented addresses (F62).
- **SMTP channel.** A fake SMTP server on a loopback listener (a scripted reply table: greeting, EHLO, STARTTLS, AUTH, MAIL, RCPT, DATA, QUIT) proves F56–F60 and the exact message bytes (headers `From`, `To`, `Subject`, `Date`, `Message-ID`, `MIME-Version`, `Content-Type: text/plain; charset=utf-8`, `Content-Transfer-Encoding: quoted-printable`, `Auto-Submitted: auto-generated`, `Reply-To` when set); no `time.Sleep`.
- **Permission tests.** Each admin method with no actor, a wrong permission, the right permission and the superuser (F37–F39); the handlers answer 401/403 through `httpx.Fail`.
- **Integration (`//go:build integration`).** `dbtest.TempDB` for the store: the unique index, `SKIP LOCKED` with two concurrent `SendOnce` (F29), the partial index, the FK to `core.person`, `DeletePerson` inside a people-style transaction, `Purge`. Mailpit through `BMS_TEST_SMTP_ADDR` (security `none`) and its HTTP API at `BMS_TEST_MAILPIT_URL` (`GET /api/v1/search?query=...`, `GET /api/v1/message/{id}`; the test deletes what it created): a message is delivered once with the expected headers and body, non-ASCII is encoded (F61), a rejected recipient (Mailpit accepts everything; the 5xx path is the fake server's).
- **Conformance.** `moduletest.Conformance` gains `notifytest.NewFake(manifest.Name)` for `Deps.Notify` and a rule `templates_register` (every `Manifest.Templates` file parses and has `en`). Every module's own tests assert what it sends with `Fake.Sent()`.
- **End to end (app).** `scripts/e2e.sh` with the compose stack: set the SMTP settings through the API, request a password reset, read the link from Mailpit, complete the reset, sign in (once identity's Proposed change 1 lands).

## Amendment 2026-10-19 (pending owner approval): `Notifier.Configured`

Source: identity.md, "Needs a decision" in its D39 amendment (the reset-request uniformity gap found while applying D39). The design is settled here; identity.md's amendment of the same date applies it.

**Problem.** `Send` answers `Unsupported` when no SMTP host or sender is set (decision 13), but a caller sees that only after it has done the work that must not depend on the recipient. `RequestPasswordReset` looks the account up first, so with e-mail unset a known address would answer `501` and an unknown one `202`: an account-enumeration oracle (`privacy-data`: a response never confirms that an identity exists).

**Decision.** `Notifier` gains one method:

```go
// Notifier is module.Deps.Notify, scoped to one owner (doc as above), plus:
type Notifier interface {
	Send(ctx context.Context, m Message) error
	// Configured reports whether Send would enqueue now: SMTPSource.SMTP succeeded and
	// SMTPConfig.Configured() is true (a host and a sender address are set). It reads the
	// settings snapshot (a map lookup; no SMTP connection) and never looks at a recipient,
	// so its answer cannot depend on who is being written to. Fails with the SMTPSource's
	// kind only. Consumers: identity (before the account lookup in RequestPasswordReset);
	// any use case or screen that must say "e-mail is not set up" before doing work whose
	// outcome must not depend on the recipient. A module is not obliged to call it.
	Configured(ctx context.Context) (bool, error)
}
```

Why on `Notifier` rather than a second seam: it is the check `Send` already makes first (Behavior, "Enqueue", step 2), so exposing it adds no code path, and every scoped `Notifier` (modules, identity, people, the alert adapter) gets the same answer without `app` writing an adapter per consumer. `Scoped(owner)` values and `*Service` implement it (`*Service.Configured` is the same method unscoped; `Outbox` uses it for `OutboxView.Configured`). `AddressSender` is unchanged. `notifytest.Fake` gains `SetConfigured(bool)` (default `true`): when `false`, `Configured` returns `false, nil` and `Send` fails `Unsupported` wrapping `ErrNotConfigured` before recording anything, mirroring the real order.

Options not taken. (a) Always `202` and enqueue: `Send` would have to accept rows while e-mail is off (reversing decision 13), and a queued reset link, a one-time credential that expires in an hour, would sit in the outbox until an admin sets SMTP up, useless by then and a stored secret meanwhile; the person would also be told an e-mail is on its way when none can be. (b) A public `email_enabled` flag on `GET /api/settings/public`: it does not fix the server, which must be uniform whatever the client shows, and it would make `settings` interpret notify's keys (settings "Doesn't do").

Between `Configured` and `Send` a settings admin may clear the host; `Send` then answers `Unsupported` after the caller's lookup, and the caller decides what that means (identity: nil and nothing committed, its F72 as amended). The window is one request long and only a settings admin can open it, so it is not an oracle.

### Failure modes (added)

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F64 | `Configured` | host and sender set / host unset / sender unset / both unset | `true` / `false` / `false` / `false`, nil; no recipient looked at, no SMTP connection | branch before any lookup | `TestConfigured_Table` |
| F65 | `Configured` | `SMTPSource.SMTP` fails | its kind (`Unavailable`, `Timeout`, `Canceled`) | 503/504; identity fails the request before its lookup | `TestConfigured_SourceFails_ReturnsKind` |
| F66 | `notifytest.Fake` | `SetConfigured(false)` | `Configured` is `false, nil`; `Send` fails `Unsupported` wrapping `ErrNotConfigured` and records nothing | tests of the unset path | `TestFake_SetConfigured_SendUnsupported` |

**Test plan (added).** `Configured` is covered in the unit tests over the fixed `SMTPConfig` seam fake; `Conformance` is unchanged (a module need not call it). The identity end-to-end run (Mailpit) adds: with `notify.smtp_host` cleared, `POST /api/accounts/password-reset` answers `501` for a known and an unknown address alike, with identical bodies.

**Elsewhere (when approved).** identity.md: its amendment of the same date (step 3 of `RequestPasswordReset`; F71, F72 replaced; F91). `.claude/skills/go-conventions/SKILL.md` and `add-module/SKILL.md`: one sentence, "call `deps.Notify.Configured` before work whose outcome must not depend on the recipient; otherwise just `Send` and treat `Unsupported` as 'no e-mail'". module-contract.md needs no edit: it names the interface, not its methods. Listed as Proposed change 13 below.

## Amendments

### Amendment 2026-10-19 (pending owner approval): retry, expiry and the text of failed rows

Raised by sql-author during the build: decision 11 and the Data table clear `subject`, `body` and `address` on every final state, `Failed` included, while Behavior lets `Retry` requeue a `Failed` row, so a retried row would have nothing to send; and a row that never reaches a final state (SMTP never configured, a recipient that never gets an address) keeps its text and any `SendTo` address forever, because `Purge` removes final rows only.

**Resolution (one mechanism for both): every row has an expiry, and text lives until `sent` or expiry, whichever comes first.**

1. `Message` gains `TTL time.Duration` (0 means `MaxAge`; `TTL > MaxAge` is `Invalid`, field `ttl`, code `range`); `MaxAge = 7 * 24 * time.Hour` is a constant, not a setting: it is an operational bound, not a regional difference, it exceeds the longest backoff sequence (8 attempts, about 4.3 h) many times over, and it equals `MinRetention`; a setting can be added later without a migration. `core.notify_message` gains `expires_at timestamptz NOT NULL` (= `created_at + TTL`), indexed `(expires_at) WHERE subject IS NOT NULL`.
2. Decision 11 becomes: `subject`, `body` and `address` are cleared when a row reaches `Sent`, and at `expires_at` for any other status; a `Failed` row keeps its text until then so an admin can retry it after fixing the relay. Option (a) of the review, bounded: never "until canceled or purged". Option (c) (re-render on retry) is refused because `Vars` may hold the very token the text holds, so it stores the same secret with more moving parts.
3. `Retry` is allowed only while the text is present (`expires_at > now`): it sets `status = queued`, `attempts = 0`, `next_attempt_at = now`, leaves `expires_at` as it was; past expiry it fails `Conflict` (field `status`, code `expired`) and the screen says "This message has expired; trigger the action again". `Cancel` is unchanged (deletes the row).
4. **Stale rows are ended, not kept.** sql-author's `fail_stale_messages.sql` is adopted as `ExpireStale(ctx) (int64, error)`: `UPDATE core.notify_message SET status = 'failed', last_error = 'expired', failed_at = now, subject = NULL, body = NULL, address = NULL WHERE expires_at <= now AND (subject IS NOT NULL OR address IS NOT NULL) AND NOT (status = 'sending' AND leased_until >= now)`, in batches of 1000; a `Failed` row past expiry is only cleared (its `failed_at` and `last_error` stay). `Run` calls it every `PurgeInterval` whatever `Retention` is, before `Purge`; the lease never takes a row past `expires_at` (`lease_messages` filters `expires_at > now`), so an expired row is not sent (a reset link that is already dead must not go out); it stays queued with its text until the next `ExpireStale` sweep, at most `PurgeInterval` later, which ends it and clears it. A row is expired when `expires_at <= now`; `Lease` and `Retry` treat it as live only while `expires_at > now`. Counted as `bms.notify.delivered{outcome: expired}`. `ExpireStale` fails `Unavailable` wrapping `db.ErrReadOnly` and store kinds, like `Purge`. `Purge` keeps its rule (final rows older than `notify.retention`); expiry does not depend on retention, so the gap is closed even at the default 0.
5. **The reset-token case.** identity passes `TTL: PasswordResetLifetime` (Proposed change 1 gains this line), so a password-reset row holds its link no longer than the token is valid: a failed reset mail expires with its token, and `Retry` on it is refused as `expired` (the person asks again). The superuser alert and the deletion notice use the default. Modules pass a short `TTL` for anything carrying a one-time link, which the add-module checklist should say (Proposed change 4 gains this line).
6. **`SendTo` and `Key` (for the record).** `person_id` is NULL on a `SendTo` row, so the unique index `(owner, key, person_id)` never applies to it: that is intended for the superuser alert, which sends one key to several alert addresses. To stop the same address getting the same keyed message twice while it is live, a second partial unique index is added: `(owner, key, address) WHERE key IS NOT NULL AND address IS NOT NULL`; once the address is cleared (sent or expired) a repeat under the same key would insert again, which is accepted: `SendTo`'s callers (the alert, the e-mail change confirmation) are direct calls, not replayed signal deliveries, and the index covers the live window where a replay could happen.

Failure modes added: F67–F72 below. **Recommendation:** approve as written (text lives until sent or expiry; `MaxAge` 7 days as a constant; stale rows ended as `failed`/`expired`; identity passes the token lifetime as `TTL`; the second partial index for `SendTo`).

Consequences elsewhere once approved (for `spec-editor`): Summary decision 11 and the Data table (`expires_at`, the two partial indexes, the sensitivity note "cleared on `sent` or at `expires_at`"); Interface (`Message.TTL`, `ExpireStale`); Behavior ("Never twice", "Outbox screen", "Purge"); Proposed changes 1 (identity passes `TTL`) and 4 (add-module: a short `TTL` for one-time links); Proposed change 9 (outcome `expired`).

## Open questions

None; answered 2026-10-19 (D39). Record:

1. The SMTP password lives in settings, sealed with settings' generated key (follows settings.md decision 3; decision 13 here).
2. No automatic sign-in after a reset: the person is sent to the sign-in page (decision 16; identity.md decision 20).
3. A reset token lasts 1 hour by default (`accounts.password_reset_lifetime`, 10 minutes to 24 hours), is single use, and only the newest token is valid (decision 17; identity.md decision 21).
4. "Send me a test" sends only `notify.test` (decision 18).

## Proposed changes elsewhere

1. Applied 2026-10-19 (D39), in identity.md, "Amendment 2026-10-19 (D39): the forgotten-password flow", which carries the interface, data, failure modes F69 to F90 and test plan; with the owner's answers: no automatic sign-in, token 1h default, single use, newest only. The superuser alert items below are in app.md. **`plan/design/identity.md` (row 6), the password-reset flow (M4, Q-CORE2; replaces its pending item 9 "the password-reset seam identity adds in M4").**
   - `Options` gains `Notify notify.Notifier` (scoped `identity`; nil means no reset: the routes answer `Unsupported`) and `Policy` gains `PasswordResetLifetime time.Duration` (setting `accounts.password_reset_lifetime`, default 1h, 10m..24h).
   - Table `core.password_reset` (migration `0003`): `id uuid PK`, `account_id uuid NOT NULL FK core.account ON DELETE CASCADE`, `token_hash bytea NOT NULL UNIQUE` (SHA-256 of the token; the token itself is never stored), `expires_at timestamptz NOT NULL`, `used_at timestamptz NULL`, `created_at`; `sensitive-credential`; rows deleted when used, expired rows purged by the next request for that account and by the M4 retention job.
   - `RequestPasswordReset(ctx, model.Email) error`: rate-limited with the existing limiter (both buckets: client address, submitted canonical address; the same limits as sign-in; `Unavailable` wrapping `httpx.ErrRateLimited`); then, if `Notify` is nil or `Send` answers `Unsupported`, `Unsupported` **before** the account lookup (uniform); then looks the account up: unknown, disabled or superuser (D23: command only) → nil with nothing sent (the response never says whether an address exists; a dummy hash is verified so timing matches); otherwise mints a 32-byte random token (base64url), deletes the account's earlier tokens, inserts the hash with `expires_at = now + lifetime`, and sends `identity.password-reset` with `Link: "/accounts/password-reset?token=<token>"` and `Vars{"lifetime": "60 minutes"}` in one `Write` transaction; audits `identity.password.reset-requested` (Subject the account's person; Outcome `Denied` for unknown/disabled/superuser, so an admin can see attempts). Returns nil in every non-error case; the handler answers `202`.
   - `CompletePasswordReset(ctx, token, newPassword string) error`: hashes the token, finds the row; missing, used or expired → `Invalid` (field `token`, code `invalid`; `expired` for an expired one: not an enumeration leak); account disabled → the same `invalid`; validates the password as `ChangePassword` does; in one `Write` transaction sets the hash, clears `password_change_required`, bumps `session_generation` (every session ends), marks `used_at`; audits `identity.password.reset`; no session is issued (decision 16). Handler answers `204`; the shell goes to `/accounts/sign-in` with a "password changed" notice.
   - Routes (public): `POST /api/accounts/password-reset` `{email}`, `POST /api/accounts/password-reset/complete` `{token, password}`. Screens: `/accounts/password-reset` (ask for the address; always "If an account exists for that address, an e-mail is on its way"; a "not configured" state on 501), `/accounts/password-reset?token=…` (new password twice; errors `invalid`/`expired` with a link to ask again). The sign-in screen gains "Forgot your password?". The token travels in the query of a page with no outbound links; the api logs route patterns, never the query.
   - Templates `identity.Templates` (`en`, `es`): `password-reset` (vars `lifetime`), `superuser-signed-in` (vars none; body "A superuser signed in at {{.Site.Name}}. If this was not expected, check the audit log." with `Link: "/audit?superuser=1"`).
   - Failure modes to add: rate limited; not configured; unknown/disabled/superuser silent; token invalid/used/expired; weak password; read-only; audit failure; `Notify.Send` failure other than `Unsupported` fails the request (`Unavailable`) with nothing committed.
   - The superuser alert: identity's `Alerter` stays; `app` implements it (item 7). Identity's F27 unchanged.
   - Also note for M4 (not designed here): `ChangeEmail` with confirmation uses `notify.AddressSender` for the new address and `Notifier` for the old one; e-mail verification at sign-up (decision 15's "revisit") uses the same shape.
2. Applied 2026-10-19 (D39), in db.md. **`plan/design/db.md` (row 5), the fixed set order (Behavior and Proposed change 10):** `identity`, `people`, `authz`, `departments`, `events`, `audit`, `settings`, `jobs`, `notify`, `files`. Mark Proposed change 13 applied for notify (this spec's Behavior: the loop reads `ReadOnly()` before each tick and stops on `ErrReadOnly`). Resolves README follow-up (w)/(j).
3. Applied 2026-10-19 (D39), in app.md, the part 9 row and Proposed change 19. **`plan/design/app.md` (row 18):** part 9 row becomes: migration set `notify`; `build`: `notify.New` over `a.dbc`, `a.clock`, `a.tel`, `audit.Scoped("notify")`, `a.authz`, the actor closure, adapters over settings (`SMTPSource`, `SiteSource`, `RetentionSource`), over identity and people (`AddressSource`: `EmailOf` then `ContactEmailOf`; `LanguageSource`; `NameSource` over `PlayaNameOf`), `Hostname` from `cfg.Instance.Name`; registers `identity.Templates`, `people.Templates`; sets `a.notify`, `a.deps.Notify` (scoped by provision), route set `notify`, the runnable `Run`/`Close` (started after `Set.Start`, stopped after HTTP drained, before `signals.Close`); `link`: registers each built module's `Manifest.Templates`, adds `people.Source{Name: "notify", Exporter: a.notify}`. `coreSetOrder` in `internal/app/core.go` gains `"notify"` after `"jobs"` (the drift in Behavior, "Open spec drift", is closed). Lazy adapters: `lazyAlerter{a}` for identity's `Alerter` (item 7) and `lazyNotifier{a, owner: "people"}` for people's `Notify` seam. `bms check` prints the three e-mail lines (Behavior, "Concurrency and lifecycle"). Part table note "over settings (SMTP) and jobs" becomes "over settings (SMTP); its own runnable (notify decision 2)".
4. Applied 2026-10-19 (D39), in module-contract.md. **`plan/design/module-contract.md` (row 4):** `Manifest` gains `Templates fs.FS` (optional; files `<ident>.<lang>.tmpl`; names are `<name>.<ident>`); `Deps.Notify notify.Notifier` scoped by module name (its pending item 13 applied for notify); `moduletest.Conformance` fills `Deps.Notify` with `notifytest.NewFake(manifest.Name)` and adds the rule `templates_register` (every template parses, every ident has `en`); the leak rule also scans `Fake.Sent()` vars and links for the seed markers. The `hello` module ships one template `hello.greeted` (sent from `Greet`) so the reference covers the path (module-hello.md).
5. Applied 2026-10-19 (D39), in people.md, "Amendment 2026-10-19 (D39)". **`plan/design/people.md` (row 8):** replace its pending item 16 (notify subscribes to `people.deletion-requested`) with: `Options` gains `Notify notify.Notifier` (scoped `people`; nil means no notice) and `Holders` (`HoldersOf(ctx, authz.Permission) ([]model.PersonID, error)`, item 6); `RequestDeletion` sends `people.deletion-requested` (vars none; `Link: "/people/admin/requests"`; body "A deletion request is waiting") to the holders of `people.data.delete` in its `Write` transaction, treating `Unsupported` as "no e-mail" (one info log, the request still succeeds) and any other `Send` error as a failure of the request. Export `people.Templates` (`en`, `es`). Export `LanguageOf(ctx, PersonID) (Language string, error)` and `ContactEmailOf(ctx, PersonID) (model.Email, error)` (system reads for notify: no actor, no audit, `NotFound` when absent; never on a route). Register notify as a `Source` (item 3).
6. Applied 2026-10-19 (D39), in authz.md, `HoldersOf` and Proposed change 18. **`plan/design/authz.md` (row 7):** add `HoldersOf(ctx, p Permission) ([]model.PersonID, error)` on `*Service`: the persons who hold `p` site-wide and effectively now (grants not ended, not denied by a separation-of-duties rule), superusers excluded; a system read with no actor and no permission check, never on a route (consumers: `app`'s alert adapter, people's deletion notice). Its pending item 12 ("you were appointed" through `access.grant-created`) becomes Later with a note that authz would send it itself through a `Notifier` seam when wanted.
7. Applied 2026-10-19 (D39), in app.md ("The superuser alert") and identity.md. **`plan/design/app.md` and `identity.md`, the superuser alert (identity's pending item 9):** `app` implements `identity.Alerter` in `internal/app/alert.go`: `HoldersOf("accounts.manage")` plus `SMTPConfig.AlertAddresses`; sends `identity.superuser-signed-in` through the `identity`-scoped `Notifier` (`Send` to the holders, `SendTo` per alert address, `Key: "superuser-signed-in:" + sessionID`); errors return to identity, which logs and counts (F27). On a read-only copy this fails and the audit entry remains the record.
8. Applied 2026-10-19 (D39), in settings.md. **`plan/design/settings.md` (row 15, Draft 2026-10-19):** its Proposed change 11 already declares the ten `notify.*` keys in a core area `notify` with `notify.smtp_password` of kind `Secret`, `Regional` and `TermsFor`; nothing to add except the `Check` ranges in this spec's Settings table (host grammar, port, security enum, address shapes, at most 10 alert addresses, retention `0` or `MinRetention`..100 years) and that notify needs no `settings.setting-changed` signal (it reads the settings at each tick and `Send`; no pooled connections).
9. Applied 2026-10-19 (D39), in telemetry.md. **`plan/design/telemetry.md` (row 15b), the metric table:** rows `bms.notify.enqueued{owner}` (counter), `bms.notify.delivered{outcome}` (counter), `bms.notify.outbox{status}` (gauge), `bms.notify.purged` (counter); span names `notify.send`, `notify.tick`, `notify.deliver`, `notify.<use-case>`.
10. Applied 2026-10-19 (D39), in 09-core-features.md §6; the milestone table needed no change. **`plan/09-core-features.md` §6 and the milestone table:** "Notifications are queued in the database and sent by `jobs`" becomes "queued in the database on the caller's transaction and sent by notify's own sender loop, only on the writing copy, with retries and an idempotency key (`jobs` may host the tick later)"; "Settings admin: set the SMTP server" stays (decision 13); add "Notices carry a link and the regional's name; no narrative, no other person's details". Specs line: `notify` (and `jobs` for retention).
11. Applied 2026-10-19 (D39), in 07-abstractions.md. **`plan/07-abstractions.md`, "notify":** Owns becomes "the `Notifier` contract (scoped per owner) and `AddressSender`; templates per language with regional overrides; the outbox (`core.notify_message`) written on the caller's transaction; the sender loop on the writing copy with retries; the SMTP channel (stdlib; Mailpit in development); the `notify` routes and screens; permissions `notify.templates.manage`, `notify.outbox.manage`; settings `notify.*`". Doesn't do adds "subscribe to signals or pick recipients; HTML, attachments, SMS (Later behind `Channel`)".
12. Pending: left to the implementers (code and config files are not edited by the spec editor; the exact lines are in the hand-off report). **`deploy/compose/.env.example` and `.env`, `docs/operating.md`:** add `BMS_TEST_MAILPIT_URL=http://localhost:8025` (read by integration tests only); operating docs: how to set the SMTP settings, that Mailpit is for development only, that `none` security is for a local relay, and that queued bodies may hold one-time links until delivered.
13. Pending (amendment 2026-10-19, owner approval needed). **`.claude/skills/go-conventions/SKILL.md` and `add-module/SKILL.md`, the M4 step:** add "ask `deps.Notify.Configured(ctx)` before work whose outcome must not depend on the recipient (a public route that would otherwise answer differently for a known and an unknown address); otherwise call `Send` and treat `Unsupported` as 'no e-mail' (log once, continue), as `hello` and `people` do." **`plan/design/identity.md` (row 6):** carried by its own amendment of the same date. **`plan/07-abstractions.md`, "notify":** Owns gains "`Configured`" after "`Notifier` contract (scoped per owner)".
