# config: process configuration

- **Status:** Draft (2026-10-16)
- **Package:** `internal/config` (Go module `github.com/zedgama3/bms`; foundation-level, not
  a bms module). **UI:** none.
- **Depends on specs:** `lib-errs.md` (Implemented): `errs.E`, `errs.Is`, kinds `Invalid`,
  `Internal`, `Canceled`, `Timeout`. `lib-core.md` (Implemented): `validate.Errors`,
  `validate.FieldError`, `validate.Fields`, `validate.NonEmpty`, `validate.Range`,
  `validate.OneOf`, `validate.URL`, `validate.ListenAddr`. `lib-secret.md` (Implemented):
  `secret.Secret`, `secret.New`, `Value`, `IsSet`. `module-contract.md` (Approved): decision
  7 and Proposed change 10 (`modules.disabled`, opt-out enablement; `Env.Enabled`).
  **Used by:** `cmd/bms` (calls `Load` once, prints problems) and `app` (hands each section
  to the core package it is for, derives `module.Env.Enabled`). Nothing else imports
  `config` (layer rules 2 and 3 in `07-abstractions.md`): the core and the modules receive
  typed options from `app`.

## Purpose

Turn what the operator sets in the environment into one typed, validated value the process
starts from.

## Responsibilities

- Define the **catalogue of keys**: every configuration value the process accepts, with its
  dotted name (`db.url`), its environment variable (`BMS_DB_URL`), its type, its default and
  whether it is required or secret. One table drives loading, validation, `bms check` and
  the operator documentation.
- **Load** the values from the environment (`os.Environ()` form), including a secret's
  `*_FILE` variant, and apply defaults.
- **Parse** each value by its type with one strict grammar per type (integer, duration,
  byte count, enum, list, CIDR list, listen address, URL).
- **Validate** the whole value and report every problem at once, each naming the
  environment variable to fix, never echoing a value (09 §1: "Configuration errors are all
  reported at once, with the setting's name").
- Hold every credential in `secret.Secret`, so the loaded value can be logged or marshalled
  without leaking (lib-secret Proposed change 1).
- Refuse unknown `BMS_*` variables, so a typo in a name is an error and not a silent default.

## Not responsible for

| Concern | Owner |
|---|---|
| What a regional configures at runtime: default language (D28), retention periods (D30), terminology, branding, the public URL (09 §9, D18), SMTP server and sender (09 §6), sign-up open or invitation only (D21), which profile fields are required, what a person sees of views of their data (D22) | `settings` (`settings.md`). See "Settings" below for the rule that draws the line. |
| Which modules are compiled in, and whether a disabled name is registered | `module` holds the registry; `app` checks every name in `modules.disabled` against `module.Default().Manifests()` and derives `Env.Enabled` (Behavior: "`modules.disabled`"). `config` cannot import `core/module` (layer rule). |
| Opening the database, the pool, read-only detection, the connect backoff schedule | `db` (`db.md`). `db` detects a read-only copy itself (`pg_is_in_recovery()`); there is no configuration flag for it (decision 6). The retry schedule for the initial connect is `db`'s constant inside `db.connect_timeout`. |
| Serving HTTP, deriving the client address behind a proxy, TLS | `api` (`api.md`) uses `HTTP.Addr`, `RequestTimeout`, `MaxBodyBytes`, `TrustedProxies`. TLS termination is a front end for now (D15: Cloudflare Tunnel, nginx, Caddy examples in `deploy/`); see Open question 2. |
| Signing and verifying sessions | `identity` (`identity.md`) uses `Session.Key`. |
| Logger, tracer and meter set-up; the `OTEL_*` variables | `telemetry` (`telemetry.md`) builds the logger from `Log` and lets the OpenTelemetry SDK read `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME` and the rest itself (D20). `config` neither reads nor validates `OTEL_*`. Log retention (D20: 30 days) is the collector's configuration, not bms's. |
| Which copy an audit entry came from, and the read-only audit spool | `audit` (`audit.md`) records `Instance.Name` and writes its spool under `Data.Dir`. |
| Command-line parsing, which command runs, printing problems to the terminal | `cmd/bms` and `app` (`app.md`). Configuration has no command-line flags (decision 2). |
| A configuration file format (TOML, YAML, JSON) or `.env` parsing | nobody (decision 1). An operator who wants a file uses systemd's `EnvironmentFile=`, compose's `env_file`, or the shell. |
| Reloading configuration while running | nobody. Configuration is read once at start; changing it means a restart. Runtime changes are settings (09 §9: "take effect without a restart"). |
| pgx's `PGHOST`, `PGPASSWORD`, ... fallbacks | `db` decides whether to honour them when it parses `DB.URL`; `config` neither reads nor clears them. |
| Trimming, size caps and the `_FILE` convention for secrets held elsewhere | this spec covers only its own keys; `settings` decides how it stores a secret a regional enters (lib-secret "Not responsible for"). |
| Later keys: OIDC issuer, client ID and client secret; the trusted-proxy-header provider (D15); S3-compatible access keys (`files`); TLS certificate and key; a sealing key if `settings` stores secrets | the spec that needs them adds a section here through "Proposed changes elsewhere"; the catalogue grows, the mechanism does not change. |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `Key`, `Keys` | Name every value the process accepts, once, with its variable, default and kind. | The table `Load` walks; exported for `bms check` and the docs test. |
| `Load` | Read the environment into a `Config`, reporting every problem at once. | `os.Environ()` in; `_FILE` for secrets; unknown `BMS_*` refused. |
| Grammars | Parse one text form per type, strictly. | Integer, duration, byte count, enum, list, CIDR list, listen address, URL. Unexported; pinned by tests. |
| `Config` and sections | Carry the typed values to `app`, immutably. | `Instance`, `DB`, `HTTP`, `Session`, `Log`, `Data`, `Modules`. |
| `Validate` | Check ranges and shapes, naming the variable and never the value. | Runs inside `Load`; callable on a hand-built `Config` in tests and `app`. |

## Interface

```go
// Package config loads the process configuration from the environment and validates it,
// reporting every problem at once. It depends on the standard library and internal/lib
// only. cmd/bms and app are its only importers; the core and the modules receive typed
// options from app.
package config

import (
	"context"
	"log/slog"
	"net/netip"
	"time"

	"github.com/zedgama3/bms/internal/lib/secret"
)

// Prefix is the start of every environment variable this package reads. Any variable
// whose name starts with it (ignoring case) must be a known key, or Load fails (F11);
// variables starting with TestPrefix are left alone.
const (
	Prefix     = "BMS_"
	TestPrefix = "BMS_TEST_" // read by integration tests, never by Load
)

// MaxSecretFileBytes caps a *_FILE read. A larger file fails Load (F9).
const MaxSecretFileBytes = 64 << 10

// Key describes one configuration value. Keys() is the single source of truth: Load
// reads exactly these variables, Validate reports problems under Env, bms check and the
// operator documentation list them. Env is derived from Name: Prefix plus Name upper-cased
// with "." replaced by "_" (db.max_conns -> BMS_DB_MAX_CONNS).
type Key struct {
	Name     string // dotted: "db.url"
	Env      string // "BMS_DB_URL"
	Default  string // text form of the default; "" when Required
	Required bool   // Load fails when neither Env nor Env+"_FILE" is set (F1)
	Secret   bool   // held in secret.Secret; Env+"_FILE" accepted; never echoed
	Doc      string // one English line for operators, e.g. "PostgreSQL connection URL"
}

// Keys returns the catalogue in the order of the sections below, as a copy. Cannot fail.
// Consumers: Load (internally), cmd/bms (`bms check` and usage text), the docs test.
func Keys() []Key

// Config is the whole process configuration. It is a value: immutable once Load returns,
// safe to copy, share, marshal and log (every credential is a secret.Secret). The zero
// value is not usable; start from Default() or Load.
type Config struct {
	Instance Instance
	DB       DB
	HTTP     HTTP
	Session  Session
	Log      Log
	Data     Data
	Modules  Modules
}

// Instance identifies this process among the copies of one installation (D27: on-site and
// off-site copies share a database but are different processes).
type Instance struct {
	// Name is recorded by audit as "which copy" (09 §8) and by telemetry as a resource
	// attribute. Grammar: "[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?" (1..64). Default "main".
	Name string
}

// DB is what db needs to open the pool. app copies it into db's own options type.
type DB struct {
	// URL is the PostgreSQL connection URL: postgres:// or postgresql:// with a host. It is
	// a secret because it carries the password. Required. BMS_DB_URL or BMS_DB_URL_FILE.
	URL secret.Secret
	// MaxConns is the pool's upper bound. Default 10; 1..1000.
	MaxConns int
	// ConnectTimeout bounds the initial connect at start-up, retries included. Default 30s;
	// 1s..10m.
	ConnectTimeout time.Duration
	// QueryTimeout is the deadline a caller derives for one statement when it has no
	// tighter one. Default 10s; 100ms..10m.
	QueryTimeout time.Duration
}

// HTTP is what the api shell needs to listen.
type HTTP struct {
	// Addr is the listen address, "host:port" or ":port". Default ":8080".
	Addr string
	// RequestTimeout is the deadline of one request. Default 30s; 1s..10m.
	RequestTimeout time.Duration
	// ShutdownTimeout is how long in-flight requests get to finish at stop (09 §1).
	// Default 15s; 1s..5m.
	ShutdownTimeout time.Duration
	// MaxBodyBytes caps a request body (JSON API; file uploads are files' later concern).
	// Default 1048576 (1 MiB); 1024..1073741824.
	MaxBodyBytes int64
	// TrustedProxies lists the networks whose X-Forwarded-For and X-Forwarded-Proto are
	// believed (D15: an optional front end). Empty, the default, trusts none. At most 64
	// prefixes; a bare address means that one address.
	TrustedProxies []netip.Prefix
}

// Session is what identity needs to sign session tokens without a database write (09 §2).
type Session struct {
	// Key is the signing key material; identity derives purpose-specific keys from it.
	// Required; 32..1024 bytes; no NUL. The same value on every copy of one installation.
	// BMS_SESSION_KEY or BMS_SESSION_KEY_FILE.
	Key secret.Secret
}

// LogFormat is how telemetry writes log lines to stdout.
type LogFormat string

const (
	LogJSON LogFormat = "json" // default; D20
	LogText LogFormat = "text" // for a developer's terminal
)

// Log is what telemetry needs to build the logger.
type Log struct {
	// Level is the minimum level written: "debug", "info", "warn" or "error". Default info.
	Level slog.Level
	// Format is "json" or "text". Default json.
	Format LogFormat
}

// Data is where this process keeps local files (the audit spool on a read-only copy,
// 09 §8; later, files' local-disk storage).
type Data struct {
	// Dir is a directory path, absolute or relative to the working directory. Default
	// "data". Its existence and permissions are checked by the consumer at Start, not here.
	Dir string
}

// Modules is the opt-out list of module-contract decision 7.
type Modules struct {
	// Disabled names compiled-in modules not to run. Default empty: every compiled-in
	// module runs. Items are unique and non-empty; app checks each is registered.
	Disabled []string
}

// Default returns a Config with every default applied and every required value unset.
// Default().Validate() therefore fails (the required keys); tests set them. Cannot fail.
// Consumers: Load (its starting point), tests in app and config.
func Default() Config

// Load reads environ (entries "NAME=value", as os.Environ returns them), applies defaults,
// parses every known key, reads *_FILE variants for secrets, validates, and returns the
// Config or one error that joins every problem found (F1–F21). Each problem is a
// validate.FieldError whose Field is the environment variable to fix and whose Reason
// never contains a value. ctx bounds the file reads. Safe for concurrent use.
// Consumers: cmd/bms (once per process); app's tests with a hand-made environ.
func Load(ctx context.Context, environ []string) (Config, error)

// Validate checks every section's ranges and shapes (F14–F21) and fails with errs.Invalid
// joining one validate.FieldError per problem, Field being the variable's Env name. It
// does no I/O: it does not open Data.Dir, resolve HTTP.Addr or connect to DB.URL.
// Consumers: Load; app's tests building a Config by hand.
func (c Config) Validate() error
```

### Who needs each member

| Member | Consumer(s) | Pre-condition | Post-condition |
|---|---|---|---|
| `Prefix`, `TestPrefix`, `MaxSecretFileBytes` | `Load`; tests; `deploy/` docs | — | one spelling |
| `Key`, `Keys` | `Load`; `cmd/bms` (`bms check` lists every key with its default and whether it is set; usage text); `TestKeys_DocsInSync` | — | one row per field of `Config`, `Env` derived from `Name` |
| `Config` | `app` (holds it for the process lifetime; copies sections into each core package's options) | `Load` returned nil error | immutable value |
| `Instance.Name` | `audit` (which copy, 09 §8), `telemetry` (resource attribute `service.instance.id`), `app` (start-up log) | — | grammar above |
| `DB` | `db` through `app` (`db.Options{URL, MaxConns, ConnectTimeout, QueryTimeout}`) | — | URL is a `postgres`/`postgresql` URL with a host |
| `HTTP` | `api` through `app` (listen, request deadline, body cap, client address); `app` (shutdown deadline) | — | ranges above |
| `Session.Key` | `identity` through `app` | — | 32..1024 bytes |
| `Log` | `telemetry` through `app` (logger) | — | one of the four levels; one of two formats |
| `Data.Dir` | `audit` (spool, M1) and `files` (Later) through `app` | — | non-empty path |
| `Modules.Disabled` | `app` (derives `module.Env.Enabled`) | — | unique, non-empty names |
| `Default` | `Load`; tests | — | every default from `Keys()` applied |
| `Load` | `cmd/bms` (every command: `serve`, `migrate`, `check`, `create-superuser`, `export`, `delete`) | `environ` entries are `NAME=value` | a valid `Config`, or every problem at once |
| `Validate` | `Load`; `app` tests | — | nil iff every rule holds |

## Types

Defines `Key`, `Config`, `Instance`, `DB`, `HTTP`, `Session`, `LogFormat`, `Log`, `Data`,
`Modules`. Uses `secret.Secret` (lib-secret), `netip.Prefix`, `slog.Level`, `time.Duration`,
`validate.FieldError` and `validate.Errors` (lib-core). Uses nothing from `model` (no key
holds an ID, a sensitivity or an address) and nothing from `core/`.

### The catalogue

| Key | Variable | Type | Default | Valid | Consumer |
|---|---|---|---|---|---|
| `instance.name` | `BMS_INSTANCE_NAME` | string | `main` | 1..64, `[a-z0-9]([a-z0-9-]*[a-z0-9])?` | audit, telemetry |
| `db.url` | `BMS_DB_URL` (+`_FILE`) | secret URL | required | absolute URL, scheme `postgres` or `postgresql`, non-empty host, ≤ 4096 bytes, no NUL | db |
| `db.max_conns` | `BMS_DB_MAX_CONNS` | int | `10` | 1..1000 | db |
| `db.connect_timeout` | `BMS_DB_CONNECT_TIMEOUT` | duration | `30s` | 1s..10m | db |
| `db.query_timeout` | `BMS_DB_QUERY_TIMEOUT` | duration | `10s` | 100ms..10m | db |
| `http.addr` | `BMS_HTTP_ADDR` | listen address | `:8080` | `validate.ListenAddr` | api |
| `http.request_timeout` | `BMS_HTTP_REQUEST_TIMEOUT` | duration | `30s` | 1s..10m | api |
| `http.shutdown_timeout` | `BMS_HTTP_SHUTDOWN_TIMEOUT` | duration | `15s` | 1s..5m | app |
| `http.max_body_bytes` | `BMS_HTTP_MAX_BODY_BYTES` | int64 (bytes) | `1048576` | 1024..1073741824 | api |
| `http.trusted_proxies` | `BMS_HTTP_TRUSTED_PROXIES` | CIDR list | empty | each a prefix or an address; ≤ 64 | api, identity (rate limit by address) |
| `session.key` | `BMS_SESSION_KEY` (+`_FILE`) | secret | required | 32..1024 bytes, no NUL | identity |
| `log.level` | `BMS_LOG_LEVEL` | enum | `info` | `debug`, `info`, `warn`, `error` | telemetry |
| `log.format` | `BMS_LOG_FORMAT` | enum | `json` | `json`, `text` | telemetry |
| `data.dir` | `BMS_DATA_DIR` | path | `data` | non-empty after trimming; no NUL | audit, files |
| `modules.disabled` | `BMS_MODULES_DISABLED` | list | empty | items non-empty after trimming, unique; ≤ 64; registered (checked by app) | app |

Variables the process reads that are **not** in the catalogue and are not `BMS_*`:
`OTEL_*` (the OpenTelemetry SDK, `telemetry.md`, D20) and, if `db` chooses to honour them,
pgx's `PG*`. `BMS_TEST_*` are read only by integration tests.

## Data

None. `config` owns no tables and no files; it reads the files that `*_FILE` variables name
and keeps nothing on disk. Nothing here is personal data. `DB.URL` and `Session.Key` are
credentials: held in `secret.Secret` from the moment they are read, never echoed in a
`FieldError.Reason`, never logged (a test pins that `slog.Any` and `encoding/json` on a
`Config` show the markers only).

## Permissions

None. `Load` runs at process start with no actor; nothing here is reachable through HTTP.
`bms check` is run by the operator on the host; the operating system's permissions on the
environment and the `*_FILE` files are the access control.

## Signals

None published, none subscribed.

## Settings

None of its own. This spec draws the line between process configuration and runtime
settings, which later specs follow:

**A value is configuration (here) when any of these holds:** it is needed to reach the
database or to start listening (`db.*`, `http.addr`); it is a credential or a security
boundary that a web admin must not be able to change (`db.url`, `session.key`,
`http.trusted_proxies`); it differs per process rather than per regional (`instance.name`,
`data.dir`, `log.*`, the on-site copy versus the off-site one, D27); or it fixes what the
process may touch before `bms migrate` runs (`modules.disabled`, module-contract decision
7). **Everything else is a setting** (`settings.md`): typed, defaulted, edited by a settings
admin, audited, effective without a restart. Examples, each settled elsewhere: default
language (D28), retention per module and level (D30), terminology, branding, the public URL
(09 §9; also the installation's identity, D18, shared by every copy and therefore not a
process value), SMTP (09 §6), sign-up mode (D21), required profile fields (09 §3), what a
person sees of views of their data (D22), session lifetime and login rate limits
(`identity.md`; regional policy, so settings with defaults).

Regional differences never appear here as code paths: every row of the catalogue has a
default a regional can leave alone, and nothing in it names a regional, a department or a
vendor.

## UI

None. `bms check` (a command, `app.md`) prints the result of `Load`: on failure, one line
per `validate.FieldError` as `<Env>: <Reason>`; on success, every key with its effective
value, secrets shown as `[redacted]` or `[unset]`.

## Behavior

### Decisions settled here, with reasons

1. **The environment is the only source: no configuration file, no flags, no `.env`
   parsing.** Every deployment tool an operator will use (systemd `EnvironmentFile=`,
   compose `env_file`, container platforms, a shell) already turns a file into an
   environment, and one source means one precedence rule and nothing to document about
   which wins. A TOML or YAML parser would be the first third-party dependency in the
   foundation for no gain. Development runs `set -a; . deploy/compose/.env` or exports the
   variables; the Makefile may do that for `make run` later.
2. **No command-line flags for configuration values.** Commands (`serve`, `migrate`,
   `check`, ...) and their arguments (a person ID for `export`) are `cmd/bms`'s; a value
   that can come from two places is a value that is set in the wrong one. Settled with
   decision 1.
3. **One `Config` for every command, fully validated every time.** `bms migrate` does not
   need `session.key`, but validating everything regardless of command means `bms check`
   finds every problem, and an operator never discovers a missing key by the next command
   they run. The cost is setting two required variables before the first migration, which
   the install docs do in one step.
4. **`FieldError.Field` is the environment variable name, not the dotted key.** The
   operator fixes a variable; `validate.Fields(err)` printed one per line is then directly
   actionable (`BMS_DB_MAX_CONNS: must be between 1 and 1000, got 0`). The dotted name is
   the key's identity in the plan and in `Key.Name`; `Key.Env` is derived from it
   mechanically, so neither can drift.
5. **Secrets are `secret.Secret` from the moment they are read and support `*_FILE`.**
   Container platforms mount secrets as files; an environment variable is visible to every
   process the same user runs. Both `BMS_X` and `BMS_X_FILE` set is an error, not a
   precedence: silently preferring one hides a stale value. Only secret keys accept
   `_FILE`; there is no reason to read a port from a file, and fewer rules are fewer tests.
6. **No read-only flag; `db` detects it.** A flag can lie in both directions: "read-only"
   on a primary merely blocks writes, "writable" on a replica changes nothing because the
   database refuses the writes anyway. `pg_is_in_recovery()` is the truth, `db` already
   owns the check (module-contract Proposed change 4), and `/readyz` reports what `db`
   found. If the switchover design (Q-OFFLINE) later needs "refuse writes before the
   database is demoted", that is a new key (`db.read_only`, default `false`) added then,
   with the mechanism unchanged.
7. **The session key is configuration, not a row the installer generates.** Sessions are
   verified without a database write (09 §2), every copy of one installation must hold the
   same key, and privacy-data warns that backups contain everything: a key kept in the
   database travels with every dump, and a leaked dump would then mint sessions for any
   admin until someone rotates it. A key the operator sets stays out of the data. The price
   is one `openssl rand -base64 48` in the install docs and copying the value to the
   on-site copy, which switchover has to handle anyway. See Open question 1.
8. **Unknown `BMS_*` variables fail loading.** `BMS_DB_MAXCONNS=5` would otherwise leave
   the default in place without a word. The check ignores case on the prefix so
   `bms_db_url` is caught too, and exempts `BMS_TEST_*`, which the integration tests own
   (`deploy/compose/.env.example`).
9. **An empty value means unset.** `BMS_MODULES_DISABLED=` is "disable nothing" and
   `BMS_SESSION_KEY=` is "not set" (required, so an error), which is what compose and
   systemd produce for an empty assignment. Non-secret values are trimmed of surrounding
   whitespace; secrets from the environment are taken verbatim; a `*_FILE` has its trailing
   CR and LF bytes removed (editors and `docker secret` add one) and nothing else.
10. **Grammars are strict.** `BMS_DB_MAX_CONNS=10.0`, `BMS_HTTP_REQUEST_TIMEOUT=30`
    (no unit), `BMS_LOG_LEVEL=Info` and `BMS_MODULES_DISABLED=a,,b` are errors, each with a
    reason that states the rule. Leniency would mean documenting which lenient forms work,
    and the error message is the documentation.
11. **`modules.disabled` is validated for shape here and for existence in `app`.** Checking
    that a name is registered needs `core/module`, which `config` may not import (layer
    rule). Duplicating the module-name grammar here would be a second copy of a rule;
    "registered" implies "well-formed", so `config` checks only non-empty and unique, and
    `app` reports an unknown name as `errs.Invalid` under `BMS_MODULES_DISABLED`, listing
    the compiled-in names, before `Resolve` (see "`modules.disabled`" below).
12. **Sections are `config`'s own types; `app` copies them into each core package's
    options.** Layer rule 2 forbids `db` or `api` importing `config`, so `db.Options` and
    `config.DB` are two structs with the same fields and a few lines in `app` between them.
    That keeps `config` a leaf and lets `db` be built in a test with no environment at all.
13. **`Load` takes `[]string`, not a lookup function.** `os.Environ()` is the natural input,
    it lets `Load` enumerate for the unknown-variable check (decision 8), and a test writes
    `[]string{"BMS_DB_URL=postgres://..."}` with nothing to fake.
14. **The byte count is a plain integer.** `1048576` rather than `1MiB`: a unit grammar
    (`k`/`M`/`Mi`, case rules) is more code and more ways to be wrong than one documented
    default. Revisit if a second byte-count key appears.
15. **`log.format` offers `text` as well as D20's JSON.** D20's "JSON on stdout, always"
    is about what a deployment emits; the default is JSON and nothing in a deployment
    needs to change it. `text` exists for a developer reading a terminal. See Open
    question 3 for confirmation.

### `Load`, step by step

1. If `ctx` is done, fail (F13).
2. Split each entry of `environ` at its first `=`; an entry without `=` is a caller bug
   (F12). Later entries for the same name win, as the operating system would resolve them.
3. For every name whose upper-cased form starts with `Prefix` and not `TestPrefix`: if it is
   not the `Env` of a key, or `Env+"_FILE"` of a secret key, record it (F11).
4. Start from `Default()`. For each key in `Keys()`:
   - Non-secret: take the value, trim it; empty means unset and the default stays.
   - Secret: if both `Env` and `Env+"_FILE"` are non-empty, record F7 and skip the key. If
     `Env+"_FILE"` is set: check `ctx` (F13), open the file, read at most
     `MaxSecretFileBytes+1` bytes through `io.LimitReader`, fail if the file could not be
     opened or read (F8) or exceeded the cap (F9), close it (its `Close` error is joined),
     trim trailing `\r` and `\n`. If `Env` is set, take it verbatim. Empty means unset.
   - Parse the text by the key's type (F2–F6, F15); a parse failure records the problem and
     leaves the default in place, and marks the key as failed.
5. Call `Validate()` on the result; drop any `FieldError` whose `Field` is a key marked
   failed in step 4 (one problem per key, F19), keep the rest, including "required" for a
   secret that is unset (F1).
6. If anything was recorded, return the zero `Config` and one error:
   `validate.Errors{Op: "config.Load"}` holding every problem, in catalogue order, so
   `errs.Is(err, errs.Invalid)` holds and `validate.Fields(err)` lists each. Otherwise return
   the `Config` and nil.

Step 4's file read is the package's only I/O. It is bounded by the cap, by `ctx`, and by
the file being a regular file the operator named; `Load` does not follow a path into a
directory listing or a network. No log output: `config` has no logger; `cmd/bms` prints what
`Load` returns.

### `Validate`, per section

| Section | Rules (each a `FieldError` under the variable) |
|---|---|
| `Instance` | `Name` matches the grammar (F17). |
| `DB` | `URL` set (F1); `validate.URL(env, url, "postgres", "postgresql")` (F15); ≤ 4096 bytes and no NUL (F15). `MaxConns`, `ConnectTimeout`, `QueryTimeout` in range (F14). |
| `HTTP` | `validate.ListenAddr` on `Addr` (F18); the three durations and `MaxBodyBytes` in range (F14); `len(TrustedProxies) <= 64` (F20); each prefix valid (`Prefix.IsValid()`, F6 when built by hand). |
| `Session` | `Key` set (F1); 32..1024 bytes and no NUL (F16). |
| `Log` | `Level` one of the four (F4; a hand-built `slog.Level(3)` is rejected); `Format` one of two (F4). |
| `Data` | `Dir` non-empty after trimming and no NUL (F21). |
| `Modules` | each item non-empty after trimming, no duplicates, ≤ 64 items (F5). |

Reasons never echo string input, following `lib-core`'s rule: "must be a postgres:// or
postgresql:// URL with a host", "must be at least 32 bytes". Numbers and durations are
echoed ("must be between 1s and 10m, got 0s").

### `modules.disabled`: how enablement is derived (for `app.md`)

`config` delivers `Modules.Disabled` as given. `app`, which may import both `config` and
`core/module`:

1. Takes `names` = the `Name` of every manifest in `module.Default().Manifests()`.
2. For each entry in `Disabled` not in `names`: records
   `validate.FieldError{Field: "BMS_MODULES_DISABLED", Reason: "unknown module <name>; compiled-in modules are: <names>"}`
   (module names are not values an operator typed in secret, so they are echoed). Any such
   entry fails start-up with `errs.Invalid`, all of them listed, before `Resolve`.
3. Sets `module.Env.Enabled` = `names` minus `Disabled`, in registry order.

Resolve's own check that every enabled name is registered (module-contract F15) then holds
by construction. `bms check` runs the same derivation and reports the same error.

### Concurrency and lifecycle

`Load` has no shared state and may be called from any goroutine; in practice `cmd/bms`
calls it once. `Config` is a value: copying it copies `secret.Secret` pointers to immutable
strings and shares the `TrustedProxies` and `Disabled` slices, which nobody mutates after
`Load` (`app` treats them as read-only; a test reads them concurrently under `-race`). There
is nothing to start or close, no clock, no logger, no goroutine.

### Imports

Standard library (`context`, `errors`, `fmt`, `io`, `log/slog`, `net/netip`, `os`,
`regexp`, `strconv`, `strings`, `time`), `internal/lib/errs`, `internal/lib/validate`,
`internal/lib/secret`. Nothing else, enforced by a new depguard rule `config` (Proposed
change 1). `cmd/bms` and `internal/app` are the only packages allowed to import
`internal/config`; `core`, `modules` and `api` are denied it.

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `Load`, `Validate` | a required key (`db.url`, `session.key`) is unset: neither `Env` nor `Env_FILE`, or both empty | Invalid, `Field` = `Env`, reason "required" | set it; `bms check` prints the line | `TestLoad_RequiredMissing_ReturnsInvalidNamingVariable` |
| F2 | `Load` | an int key is not a base-10 integer by `strconv.ParseInt` rules, base 10 (`10.0`, `ten`, `1_000`, `0x10` all fail; a leading `+` or `-` parses, and a negative value is then rejected by its range, F14) | Invalid | fix the value | `TestLoad_BadInt_ReturnsInvalid` |
| F3 | `Load` | a duration key fails `time.ParseDuration` or has no unit (`30`) | Invalid | fix the value | `TestLoad_BadDuration_ReturnsInvalid` |
| F4 | `Load`, `Validate` | an enum key is not exactly one of its options (`Info`, `INFO`, `verbose`) | Invalid; reason lists the options, never the value | fix the value | `TestLoad_BadEnum_ReturnsInvalidListingOptions` |
| F5 | `Load`, `Validate` | a list has an empty item (`a,,b`, `a,`), a duplicate, or more than 64 items | Invalid | fix the value | `TestLoad_BadList_ReturnsInvalid` |
| F6 | `Load`, `Validate` | a trusted-proxy entry is neither a prefix nor an address, or a hand-built `netip.Prefix` is invalid | Invalid | fix the value | `TestLoad_BadCIDR_ReturnsInvalid` |
| F7 | `Load` | both `BMS_X` and `BMS_X_FILE` are non-empty for a secret key | Invalid, `Field` = `BMS_X_FILE`, reason names both variables | remove one | `TestLoad_SecretAndFileBothSet_ReturnsInvalid` |
| F8 | `Load` | `BMS_X_FILE` cannot be opened or read (missing, a directory, permission denied) | Invalid wrapping the OS error (the path is the operator's, not a secret; the content is never read into the message) | fix the path or permissions | `TestLoad_SecretFileUnreadable_ReturnsInvalid` |
| F9 | `Load` | `BMS_X_FILE` is longer than `MaxSecretFileBytes` | Invalid, reason gives the cap | point at the right file | `TestLoad_SecretFileTooLarge_ReturnsInvalid` |
| F10 | `Load` | `BMS_X_FILE` is empty after trimming trailing CR/LF | treated as unset: F1 if required | set it | `TestLoad_SecretFileEmpty_IsUnset` |
| F11 | `Load` | a variable whose upper-cased name starts with `BMS_` (not `BMS_TEST_`) is not a known key or `_FILE` of a secret key | Invalid, one `FieldError` per unknown name, reason "unknown configuration variable" | fix the name | `TestLoad_UnknownVariable_ReturnsInvalid` |
| F12 | `Load` | an `environ` entry has no `=` | Internal | fix the caller (`os.Environ` never does this) | `TestLoad_MalformedEnviron_ReturnsInternal` |
| F13 | `Load` | `ctx` done before or during a file read | Canceled / Timeout | stop | `TestLoad_ContextCanceled_ReturnsCanceled` |
| F14 | `Validate` | a number or duration out of range: `db.max_conns`, `db.connect_timeout`, `db.query_timeout`, `http.request_timeout`, `http.shutdown_timeout`, `http.max_body_bytes` | Invalid, reason gives the bounds and the value | fix the value | `TestValidate_RangesTable` |
| F15 | `Validate` | `db.url` is not an absolute `postgres`/`postgresql` URL with a host, exceeds 4096 bytes, or contains NUL | Invalid; reason never contains the URL | fix the value | `TestValidate_DBURL_ReturnsInvalidWithoutEcho` |
| F16 | `Validate` | `session.key` shorter than 32 or longer than 1024 bytes, or contains NUL | Invalid; reason never contains the key | generate a proper key | `TestValidate_SessionKey_ReturnsInvalidWithoutEcho` |
| F17 | `Validate` | `instance.name` fails the grammar (empty, upper case, `_`, leading or trailing `-`, 65 bytes) | Invalid | fix the value | `TestValidate_InstanceName_ReturnsInvalid` |
| F18 | `Validate` | `http.addr` fails `validate.ListenAddr` | Invalid | fix the value | `TestValidate_HTTPAddr_ReturnsInvalid` |
| F19 | `Load` | several of F1–F18 at once | one Invalid joining all, each key reported once, catalogue order | fix them all in one go | `TestLoad_ManyProblems_ReportsEachOnce` |
| F20 | `Validate` | more than 64 trusted proxies | Invalid | aggregate the prefixes | `TestValidate_TooManyTrustedProxies_ReturnsInvalid` |
| F21 | `Validate` | `data.dir` empty after trimming, or contains NUL | Invalid | set a path | `TestValidate_DataDir_ReturnsInvalid` |
| F22 | `app` (obligation, not this package) | a `modules.disabled` entry is not a registered module | Invalid under `BMS_MODULES_DISABLED`, listing the compiled-in names | fix the name | `app.md`: `TestEnabledModules_UnknownDisabledName_ReturnsInvalid` |

Members that cannot fail, and why: `Keys` (returns a copy of a package-level table that a
test proves is complete and well-formed); `Default` (applies the same table's defaults,
which `TestDefault_WithRequiredSet_Validates` proves valid). Each has a success-path test.

## Lock-in check

No external technology. The environment, `*_FILE` secrets and `os.Environ` are POSIX and
Windows alike; `netip`, `slog` and `time.ParseDuration` are standard library. Nothing in the
catalogue names a vendor: `db.url` is any PostgreSQL (D8), the observability variables are
OpenTelemetry's (D20), and the front end behind `http.trusted_proxies` can be any proxy or
tunnel.

## Test plan

Unit tests only, no infrastructure, `go test -race`, table-driven, black-box package
`config_test`. Files for `*_FILE` cases are written to `t.TempDir()`. Every credential in a
test is an invented marker that cannot occur by accident (`ZQ9MARKER7` inside
`postgres://bms:ZQ9MARKER7@db.example:5432/bms`; a 48-byte session key of repeated marker
text); every leak check asserts `!strings.Contains(out, marker)`. No real host names,
passwords or people.

- Catalogue: `TestKeys_CoversEveryConfigField` (reflection over `Config`'s sections: one
  `Key` per leaf field and no extra keys); `TestKeys_EnvDerivedFromName`;
  `TestKeys_DefaultsParseAsTheirType` (every non-required `Default` round-trips through
  `Load` with an empty environment); `TestKeys_DocsInSync` (the table in this spec's
  "Catalogue" and in `docs/operating.md`, once it exists, match `Keys()`; until the doc
  exists the test covers the spec table).
- Defaults: `TestDefault_WithRequiredSet_Validates`; `TestDefault_Validate_FailsOnlyOnRequired`
  (exactly two `FieldError`s: `BMS_DB_URL`, `BMS_SESSION_KEY`).
- Load, success paths: `TestLoad_MinimalEnviron_AppliesDefaults` (only the two required
  keys set); `TestLoad_EveryKeySet_RoundTrips` (each key set to a non-default value and read
  back); `TestLoad_EmptyValueMeansUnset`; `TestLoad_TrimsNonSecretsOnly` (`" :8080 "`
  accepted; a secret with surrounding spaces keeps them); `TestLoad_SecretFromFile_TrimsTrailingNewlinesOnly`
  (`"key\r\n"`, `"key\n\n"`, `" key \n"` → `"key"`, `"key"`, `" key "`);
  `TestLoad_LaterDuplicateEntryWins`; `TestLoad_IgnoresTestPrefixAndOtherVariables`
  (`BMS_TEST_DB_DSN`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `PATH`, `PGPASSWORD` present → no
  error); `TestLoad_TrustedProxies_PrefixesAndBareAddresses` (`10.0.0.0/8, 192.0.2.1,
  2001:db8::/32` → three prefixes, the bare one `/32`); `TestLoad_ModulesDisabled_List`
  (`"volunteers, incidents"` → two trimmed names); `TestLoad_LogLevelAndFormat_Table`.
- Leak checks: `TestConfig_SlogAnyAndJSONNeverLeakSecrets` (`slog.Any("config", cfg)` under
  the text and JSON handlers, `json.Marshal(cfg)`, `fmt.Sprintf("%+v", cfg)`: output contains
  `[redacted]` and never the markers); `TestLoad_ErrorsNeverEchoSecrets` (an invalid URL and
  a short key built from the marker: `err.Error()` and every `FieldError.Reason` lack it).
- Concurrency: `TestConfig_ConcurrentReads` (under `-race`, several goroutines read every
  field of one loaded `Config`).
- Failure modes: F1–F21, one test each as named. F19 sets a bad int, a bad enum, an unknown
  variable, both forms of a secret, and omits the other required key, and asserts the exact
  set of `Field`s with no repeats and in catalogue order (unknown variables last).
- Lint: passes the new depguard rule `config`, `errcheck`, `errorlint`, `wrapcheck`,
  `gosec` (file read bounded by `io.LimitReader`; `G304` acknowledged with a comment: the
  path is the operator's), `sloglint` (no logging in the package).

Integration (behind the `integration` tag) is not needed: nothing here touches a database
or the network. `app.md`'s start-up test covers `Load` → `db` → `Resolve` end to end.

## Open questions

1. **Session key: set by the operator (this spec) or generated by `bms migrate` and stored
   in the database?** Proposed: by the operator, as decision 7 argues (out of backups,
   shared across copies by the operator who already copies the configuration). If the owner
   prefers zero-step installs, the alternative is a `core.keys` row created by `bms migrate`
   and a `bms rotate-session-key` command; `Session` would then leave this spec and
   `identity.md` would own it. Either way `config`'s mechanism is unchanged.
2. **Must the binary terminate TLS itself before the pilot?** D15 says no reverse proxy is
   required, and `deploy/` ships front-end examples (Caddy and Cloudflare Tunnel give HTTPS
   with no certificate work). Proposed: a front end is acceptable for the MVP; when `api.md`
   adds TLS serving (static certificate first, ACME later), it adds `http.tls.cert_file` and
   `http.tls.key_file` here (both or neither), with the files opened by `api` at Start.
3. **Is a `text` log format acceptable alongside D20's "JSON on stdout, always"?** Proposed:
   yes, default `json`, `text` only for a developer's terminal and never in the deploy
   examples. If the owner wants "always" to be literal, `log.format` is dropped and the
   catalogue loses one row.

## Proposed changes elsewhere

1. **`.golangci.yml`:** add a depguard rule `config` (files `**/internal/config/**`,
   `list-mode: strict`, allow `$gostd`, `internal/lib`, `internal/config`), and deny
   `internal/config` in the `api` rule (the `core` and `modules` rules already deny it).
   `cmd/bms` and `internal/app` stay the only importers.
2. **`plan/07-abstractions.md`, `config` entry:** replace with: "**Purpose:** turn what the
   operator sets in the environment into one typed, validated value the process starts
   from. **Owns:** the catalogue of keys (`Keys()`: dotted name, `BMS_*` variable, type,
   default, required, secret), loading from the environment with `*_FILE` for secrets,
   strict grammars, `Validate` reporting every problem at once by variable name, refusing
   unknown `BMS_*` variables. **Doesn't do:** runtime settings (`settings`), `OTEL_*`
   (`telemetry`), read-only detection (`db`), checking that a disabled module is registered
   (`app`), a configuration file or flags." Add to the layer rules: "`config` imports only
   `lib`; only `cmd/bms` and `app` import it."
3. **`plan/design/app.md` (row 18):** `cmd/bms` calls `config.Load(ctx, os.Environ())` once
   for every command and prints `validate.Fields(err)` one per line as `<Env>: <Reason>` on
   failure (exit status 2); derive `module.Env.Enabled` as in Behavior
   "`modules.disabled`" (F22, with its test); copy `config.DB` into `db`'s options,
   `config.HTTP` into `api`'s, `config.Session.Key` into `identity`'s, `config.Log` and
   `config.Instance.Name` into `telemetry`'s, `config.Data.Dir` into `audit`'s (and later
   `files`'); use `HTTP.ShutdownTimeout` for the stop deadline; log the effective non-secret
   configuration once at start (`slog.Any("config", cfg)` is safe); `bms check` prints every
   key with its effective value, secrets as markers. The install docs give
   `openssl rand -base64 48` for `BMS_SESSION_KEY` (or `bms` gains a `gen-key` command if
   the owner wants one).
4. **`plan/design/db.md` (row 5):** define `db.Options{URL secret.Secret; MaxConns int;
   ConnectTimeout, QueryTimeout time.Duration}` with its own `Validate` (the same bounds as
   the catalogue); read `URL.Value()` once when opening the pool; detect read-only itself
   (`pg_is_in_recovery()`, re-checked on a schedule and on the write error) and expose it for
   `/readyz`; there is no configuration flag (decision 6; coordinates with module-contract
   Proposed change 4); hold the connect retry schedule as a constant inside
   `ConnectTimeout`; decide whether pgx's `PG*` fallbacks are honoured and say so in the
   operator docs.
5. **`plan/design/api.md` (row 16):** take `Addr`, `RequestTimeout`, `MaxBodyBytes` and
   `TrustedProxies` as its options; derive the client address and scheme from
   `X-Forwarded-For`/`X-Forwarded-Proto` only when the peer is in `TrustedProxies`, and from
   the connection otherwise; TLS per Open question 2.
6. **`plan/design/identity.md` (row 6):** take `Session.Key` as its signing material and
   derive purpose-specific keys from it (HKDF), so one configured value serves session
   tokens and, later, password-reset links; rate-limit failed logins by the client address
   `api` derives; when OIDC and the trusted-proxy-header provider are designed, their
   issuer, client ID, client secret and header name become catalogue keys under
   `identity.*` (secrets with `_FILE`), proposed back to this spec.
7. **`plan/design/telemetry.md` (row 15b):** build the logger from `Log.Level` and
   `Log.Format`; set `service.instance.id` (or an equivalent attribute) from
   `Instance.Name`; read `OTEL_*` through the SDK, not through `config`; state that log
   retention is the collector's setting.
8. **`plan/design/audit.md` (row 12):** record `Instance.Name` as the "which copy" of every
   entry (09 §8); keep the read-only spool under `Data.Dir`, creating the directory at
   `Start` and failing with `errs.Invalid` under `BMS_DATA_DIR` if it cannot.
9. **`plan/design/settings.md` (row 15):** adopt the configuration-versus-settings rule in
   "Settings" above; hold the public URL (also the installation's identity, D18), default
   language, retention, terminology, branding, SMTP, sign-up mode, required profile fields
   and the D22 visibility level; if it stores a secret (the SMTP password), decide the
   sealing and, if a key is needed, propose `settings.sealing_key` here.
10. **`plan/design/lib-core.md`, consumer tables:** `config` uses `validate` (`URL`,
    `ListenAddr`, `Range`, `OneOf`, `NonEmpty`, `Errors`, `Fields`) but not `retry.Policy`
    (the connect schedule is `db`'s constant); amend the `retry` consumer row.
11. **`deploy/compose/.env.example` and the operator docs (`docs/operating.md`, when
    written):** list every catalogue row with its default; the `BMS_TEST_*` variables stay
    and are documented as test-only; the deploy examples set `BMS_DATA_DIR=/var/lib/bms`
    and never `BMS_LOG_FORMAT=text`.
12. **`plan/09-core-features.md` §1:** "Configuration errors are all reported at once, with
    the setting's name" may read "with the environment variable's name" to match decision 4.
