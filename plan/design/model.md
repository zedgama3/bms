# model: shared value types

- **Status:** Approved (2026-10-16)
- **Package:** `internal/model` (Go module `github.com/zedgama3/bms`; foundation, not a bms
  module). **UI:** none.
- **Depends on specs:** `lib-errs.md` (Implemented): `errs.E`, `errs.Is`, kinds `Invalid`
  and `Internal`. `lib-core.md` (Implemented): `validate.Errors`, `validate.FieldError`.
  `lib-secret.md` (Implemented): the `secret.Redacted` and `secret.Unset` markers, for one
  spelling of "redacted".
  **Used by:** every package above the foundation. Named consumers per type and method are
  listed in "Interface". Nothing in `lib` imports `model` (layer rule 1).

## Purpose

Give every layer one vocabulary of identifiers and small values so that records from the
core and from different modules line up without the packages knowing each other.

## Responsibilities

- Define the record identifier (`ID`), how it is made, parsed and printed, and the typed IDs
  for the core's shared keys (D19): `PersonID`, `AccountID`, `DepartmentID`, `EventID`.
- Define the sensitivity level of a field (`Sensitivity`, from the `privacy-data` skill) and
  the one rule that follows from it mechanically: whether a read of that field is audited.
- Define a half-open time range (`TimeRange`) with the comparisons its consumers repeat.
- Define an e-mail address (`Email`) with the one normalisation D21 needs (case-insensitive
  uniqueness) and a guard against logging it.
- Give each type a `Validate() error` that consumers call at boundaries (`go-conventions`).

## Not responsible for

| Concern | Owner |
|---|---|
| The records themselves: people, accounts, departments, events, and their fields | the core: `people`, `identity`, `departments-events`. `model` holds only their IDs. |
| Reference shapes for display (a person's ID plus playa name, a department's ID plus name) | `contracts/<name>` data types (D19), defined by the contract that needs them |
| Permission names, their declaration and scope | `module-contract` and `authz`. A permission is a core concept with behaviour (scope, who declares it), not a plain value; where the type lives is settled when `module-contract.md` is written (see "Proposed changes elsewhere"). |
| Signal names and payload shapes | `signals` |
| Event kinds (burn, decompression, other) and their regional names | `departments-events` and `settings`: a regional can name them, so they are data, not a model enum |
| Money | not defined. Q-PAY is open and no spec needs it; add it to this spec when a consumer appears. |
| Calendar dates and time zones (an event's dates "in its zone") | `departments-events`. Confirmed by the owner (2026-10-16): an event's start and end are each a date and a time, so instants; `departments-events` stores a `TimeRange` plus the IANA zone name and the UI renders in that zone. No civil `Date` type in `model` for now. Schedule items inside an event (gate hours, the temple burn) are also `departments-events`' (Proposed change 8). |
| Reading or writing anything: SQL, JSON over HTTP, files | the storage seam (D8) and `api`. `model` types only implement the standard `encoding.TextMarshaler`/`TextUnmarshaler` so any encoder can carry them. |
| Scanning database columns (`sql.Scanner`, `driver.Valuer`) | the `postgres/` packages, which convert by underlying type (see Behaviour: "At the storage seam"). depguard forbids `database/sql` in `model`. |
| Time: reading it or waiting for it | `lib/clock`. No type here reads the clock; `NewID` needs no time (see decision 1). |
| Which fields are personal or sensitive in a given record | the spec that owns the record, which tags each field with a `Sensitivity` |
| Who may see a field, and auditing the read | `authz` decides; `audit` records; `people` and modules call `Sensitivity.ReadIsAudited` to know whether to. |
| Deliverability of an e-mail address | `notify`, by sending to it. `Email` checks shape only. |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `ID[E]` | Identify one record of kind `E` with a UUID that is unique across every copy of the installation. | Underlying type `[16]byte`. `E` is a marker type, so `PersonID` and `DepartmentID` are distinct types. |
| Typed IDs | Name the core's shared keys: `PersonID`, `AccountID`, `DepartmentID`, `EventID`. | Aliases of `ID[E]` instantiations. Modules declare their own the same way, with no code in `model`. |
| `Sensitivity` | Say how carefully a field about a person must be handled. | Four ordered levels from `privacy-data`. Zero value is invalid, so a forgotten level fails validation instead of defaulting to public. |
| `TimeRange` | Hold a half-open interval `[Start, End)` of instants. | Comparisons are pure; `Validate` names the bad field. |
| `Email` | Hold an e-mail address that compares case-insensitively and never reaches a log by accident. | A string type; `Canonical()` is the uniqueness key (D21). |

## Interface

```go
// Package model is the vocabulary every layer shares: record IDs, sensitivity levels and a
// few small value types. It performs no I/O and depends only on the standard library and
// internal/lib.
package model

// ---- IDs ----

// Entity marks what an ID identifies. It is used only as a type argument: declare an empty
// struct per record kind and alias ID[thatStruct]. Distinct markers make distinct ID types,
// so the compiler rejects a PersonID where a DepartmentID is expected.
type Entity interface{ ~struct{} }

// ID is the identifier of one record: a 128-bit UUID (RFC 9562). The zero value means
// "no record" and is how an optional reference is left empty. The underlying type is
// [16]byte so the storage seam can convert it to and from a PostgreSQL uuid column without
// model knowing any driver.
type ID[E Entity] [16]byte

// The core's shared keys (D19). Modules refer to people, departments and events by these
// and never by name. A person is not an account: PersonID and AccountID are different
// types, and modules use PersonID only.
type (
	PersonID     = ID[person]
	AccountID    = ID[account]
	DepartmentID = ID[department]
	EventID      = ID[event]
)

type (
	person     struct{}
	account    struct{}
	department struct{}
	event      struct{}
)

// NewID returns a fresh version-4 UUID as T, from crypto/rand. It cannot fail: on Go 1.24+
// crypto/rand.Read never returns an error. Safe for concurrent use.
//   id := model.NewID[model.PersonID]()
func NewID[T ~[16]byte]() T

// ParseID parses the canonical 36-character form 8-4-4-4-12 of hexadecimal digits, in
// either case, and returns it as T. It accepts no other form (no braces, no urn: prefix,
// no 32-digit form, no surrounding space). It fails with errs.Invalid, and the error never
// echoes s. The nil UUID parses; ID.Validate rejects it.
//   id, err := model.ParseID[model.EventID](r.PathValue("event"))
func ParseID[T ~[16]byte](s string) (T, error)

// String returns the canonical lower-case 36-character form. Cannot fail.
func (id ID[E]) String() string

// IsZero reports whether id is the zero (nil) UUID, that is, no record. Cannot fail.
func (id ID[E]) IsZero() bool

// Validate fails with errs.Invalid if id is zero. Any other 128-bit value is accepted, so
// IDs imported from another system (Q-IMPORT, Q-TRUST) need not be version 4.
func (id ID[E]) Validate() error

// MarshalText returns String() as bytes and a nil error, so encoding/json emits the
// canonical string. Cannot fail.
func (id ID[E]) MarshalText() ([]byte, error)

// UnmarshalText parses text as ParseID does and stores the result. On failure *id is
// unchanged. It fails with errs.Invalid for bad text and errs.Internal for a nil receiver.
// encoding/json never calls it for null, so a JSON null leaves the field unchanged (zero
// in a fresh struct); use the omitzero tag to leave a zero ID out of a response.
func (id *ID[E]) UnmarshalText(text []byte) error

// ---- Sensitivity ----

// Sensitivity is how carefully a field about a person must be handled (privacy-data).
// Levels are ordered: SensitivityPublic < SensitivityInternal < SensitivityPersonal <
// SensitivitySensitive; compare them with < and >. The zero value is not a level: Validate
// rejects it, so a field whose level was never set fails at the boundary instead of being
// treated as public. Every other method treats an invalid value as SensitivitySensitive.
type Sensitivity uint8

const (
	SensitivityPublic    Sensitivity = iota + 1 // anyone the module shows it to
	SensitivityInternal                         // signed-in people with the module's read permission
	SensitivityPersonal                         // the person, and roles with an explicit permission
	SensitivitySensitive                        // named roles only, scoped narrowly
)

// ParseSensitivity returns the level named by s: "public", "internal", "personal" or
// "sensitive", exactly. It fails with errs.Invalid for anything else; the error lists the
// four names and never echoes s.
func ParseSensitivity(s string) (Sensitivity, error)

// String returns the level's stable name, as ParseSensitivity accepts it, or
// "Sensitivity(<n>)" for a value outside the set. Cannot fail.
func (s Sensitivity) String() string

// Validate fails with errs.Invalid if s is zero or above SensitivitySensitive.
func (s Sensitivity) Validate() error

// ReadIsAudited reports whether a read of a field at this level must be written to the
// audit log: never for public and internal; for personal, unless the reader is the person
// the field is about (self); always for sensitive. An invalid level is audited. Cannot fail.
func (s Sensitivity) ReadIsAudited(self bool) bool

// MarshalText returns String() as bytes. It fails with errs.Invalid for an invalid level,
// so a response that carries an unset level fails loudly rather than emitting a placeholder.
func (s Sensitivity) MarshalText() ([]byte, error)

// UnmarshalText parses text as ParseSensitivity does. On failure *s is unchanged. It fails
// with errs.Invalid for bad text and errs.Internal for a nil receiver.
func (s *Sensitivity) UnmarshalText(text []byte) error

// ---- TimeRange ----

// TimeRange is the half-open interval [Start, End). Both are instants; any zone is fine,
// because every comparison is on the instant. JSON form: {"start": RFC3339, "end": RFC3339}.
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Validate fails with errs.Invalid, joining one validate.FieldError per problem: "start"
// must be set, "end" must be set, and "end" must be after "start".
func (r TimeRange) Validate() error

// Contains reports Start <= t < End. Cannot fail; meaningful once Validate passes.
func (r TimeRange) Contains(t time.Time) bool

// Overlaps reports whether the two half-open ranges share any instant:
// r.Start < o.End && o.Start < r.End. Two ranges that only touch do not overlap.
// Cannot fail; meaningful once both validate.
func (r TimeRange) Overlaps(o TimeRange) bool

// Duration returns End.Sub(Start). Cannot fail; meaningful once Validate passes.
func (r TimeRange) Duration() time.Duration

// ---- Email ----

// Email is an e-mail address as the person gave it. It is personal data (privacy-data):
// hold it only where the owning spec says, and log it never. slog.Any on an Email logs a
// marker (see LogValue); slog.String(k, string(e)) would not, so don't write that.
// Build one with ParseEmail or validate a converted string with Validate; the zero value ""
// means "none" and fails Validate, so an optional address is validated only when set.
type Email string

// ParseEmail trims surrounding space and checks shape: non-empty; at most 254 bytes;
// exactly one "@"; a local part of 1 to 64 bytes; a domain of at least 1 byte that neither
// starts nor ends with "."; and no whitespace, control characters or any of < > , ; "
// anywhere. Deliverability is not checked here (notify proves it by sending). It fails
// with errs.Invalid wrapping a validate.FieldError whose Reason never echoes the input.
func ParseEmail(s string) (Email, error)

// Validate applies ParseEmail's rules to e as it is (no trimming) and fails with errs.Invalid
// if any is broken.
func (e Email) Validate() error

// Canonical returns the address lower-cased, the key under which addresses are unique
// ignoring case (D21). Cannot fail.
func (e Email) Canonical() Email

// LogValue returns secret.Redacted when e is non-empty and secret.Unset when it is empty,
// so slog never prints an address. Cannot fail.
func (e Email) LogValue() slog.Value
```

### Who needs each member

| Member | Consumer(s) | Pre-condition | Post-condition |
|---|---|---|---|
| `ID[E]`, the four typed IDs | every core spec and module that stores or passes a reference: `people`, `identity`, `departments-events`, `authz` (grants scoped to a `DepartmentID`), `audit` (actor and subject `PersonID`, record IDs), `signals` (payloads carry IDs, never personal data), `contracts/*` (batch calls take `[]PersonID`), `api` (path params), modules | — | distinct types per kind; `[16]byte(id)` converts for storage |
| `Entity` | modules declaring their own IDs (`type ShiftID = model.ID[shift]` in `volunteers`), `add-module` skill | marker is an empty struct | the alias is a distinct type with every `ID` method |
| `NewID` | `people`, `identity`, `departments-events`, `audit`, `jobs`, every module service that creates a record | — | version 4, variant RFC 9562, non-zero, unique in practice |
| `ParseID` | `api` handlers (path and query parameters), `app` commands (`export`/`delete` one person by ID), tests | — | success iff canonical form; `T` is the caller's typed ID |
| `ID.String` | `api` (URLs, JSON), `audit` and `telemetry` (log fields hold IDs, not names), `notify` (links in e-mails) | — | 36 lower-case characters |
| `ID.IsZero` | every owner of an optional reference (`volunteers` work with no event, D24; `people` person with no account) | — | `IsZero() == (id == ID[E]{})` |
| `ID.Validate` | services validating requests before store writes; `module-contract` manifest validation | — | nil iff not zero |
| `ID.MarshalText`, `ID.UnmarshalText` | `encoding/json` in `api` and `signals` payloads, `settings` values holding IDs | — | round trip is identity |
| `Sensitivity`, constants, `ParseSensitivity`, `String` | `people` (profile field table), `contracts/people` (a module's person-page section declares its level), `audit` (level of the fields viewed, so D22's settings can distinguish later), `settings` (retention per module and level), `module-contract` (permission declarations; see Proposed changes 3) | — | stable names are API strings |
| `Sensitivity.Validate` | `module-contract` manifest validation; `people` field declarations at start-up | — | nil iff one of the four |
| `Sensitivity.ReadIsAudited` | `people` reads, module reads of personal fields (`incidents`), the `audit` helper | — | the `privacy-data` table, mechanically |
| `Sensitivity.MarshalText`/`UnmarshalText` | `api` (admin screens showing field levels), `settings` | — | names round trip |
| `TimeRange` and `Validate` | `departments-events` (an event's dates; windows inside an event such as gate hours, Proposed change 8), `volunteers` (shift times), `audit` (search by date range), `api` (query validation) | — | FieldErrors `start`, `end` |
| `TimeRange.Contains` | `departments-events` ("current" and "upcoming" events), `volunteers` (is this shift now?) | valid range | half-open semantics |
| `TimeRange.Overlaps` | `volunteers` (a person's shifts must not overlap; a shift must fall in its activity's window), `departments-events` (several events may be open at once, so overlap is allowed there, but the admin screen shows it) | both valid | touching ranges do not overlap |
| `TimeRange.Duration` | `volunteers` (hours credited per completed shift, kept as dated records for D18) | valid range | `End.Sub(Start)` |
| `Email`, `ParseEmail`, `Validate` | `identity` (the login name, D21), `people` (primary e-mail field), `notify` (recipient), `departments-events` (a department's contact address) | — | shape rules above |
| `Email.Canonical` | `identity` and `people` (the unique index and the lookup key, "unique ignoring case") | — | idempotent: `e.Canonical().Canonical() == e.Canonical()` |
| `Email.LogValue` | `log/slog` through every logger in the project | — | marker, never the address |

## Types

Defines `ID[E]`, `Entity`, `PersonID`, `AccountID`, `DepartmentID`, `EventID`,
`Sensitivity`, `TimeRange`, `Email`. Uses `validate.FieldError` and `validate.Errors` from
`lib-core` and the `secret.Redacted`/`secret.Unset` constants from `lib-secret`.

## Data

None. `model` owns no tables and no files. How each type is stored is the owning spec's
decision; the expected PostgreSQL mappings are `uuid` for an ID, `text` for a `Sensitivity`
name, `timestamptz` pairs for a `TimeRange` (or a `tstzrange`; `db.md` and `postgres-sql`
decide) and `text` for an `Email`, with a unique index on `Canonical()` where D21 applies.

## Permissions

None. `model` performs no operation on any record; every consumer names the permission for
its own operations.

## Signals

None published, none subscribed. Signal payloads carry `model` IDs (never personal data,
`privacy-data`); the `signals` spec defines them.

## Settings

None. Nothing here differs between regionals: the four sensitivity levels and the e-mail
shape rules are fixed; what a regional varies (which fields are required, retention per
level, terminology) is a setting of the owning spec.

## UI

None. The web shell and modules mirror the wire forms (a UUID string, the four sensitivity
names, `{start, end}`) in TypeScript; `web-shell.md` decides where those types live.

## Behaviour

### Decisions settled here, with reasons

1. **UUID version 4, generated with the standard library, no third-party library.** A
   random (v4) UUID from `crypto/rand` makes a collision between the on-site and off-site
   copies impossible in practice (122 random bits), reveals nothing about the record (a v7
   ID embeds its creation time to the millisecond, and incident and person IDs appear in
   URLs and logs), needs no time source so `NewID` cannot fail and the clock rule in
   `lib-core` is untouched, and the ~40 lines it takes keep `model` inside depguard's strict
   `foundation-model` allowlist (stdlib and `lib` only). Rejected: v7, whose index-locality
   benefit is negligible at a regional's scale (thousands of rows) and whose timestamp is a
   leak; `github.com/google/uuid`, which would need a depguard exception, reads `time.Now`
   internally for v7, and whose `Parse` accepts five input forms. If the owner prefers a
   library later, only the implementation and the depguard allowlist change; this interface
   does not.
2. **One generic `ID[E]` with marker types, not four copies.** Every ID type in the core and
   in every module gets the same methods, one implementation and one test suite, and a new
   ID type costs two lines (`type shift struct{}`; `type ShiftID = model.ID[shift]`) with no
   code generation. The marker is a type argument only; it has no values and no methods.
   Exported aliases over unexported markers are legal Go and render in `go doc` as
   `PersonID = ID[person]`.
3. **Constructors are generic over `~[16]byte`,** `NewID[model.PersonID]()` and
   `ParseID[model.ShiftID](s)`, so callers name the ID type they want and never the marker,
   which a module keeps unexported.
4. **`ParseID` accepts exactly one form.** The API and the database always emit the canonical
   36-character form; any other input is a client bug, and a lenient parser hides bugs.
5. **`Validate` rejects only the zero ID.** Version and variant are not checked, because
   imported or federated IDs (Q-IMPORT, Q-TRUST) may be another version; PostgreSQL's
   `uuid` stores any 128-bit value.
6. **The zero ID means "none".** Optional references (work with no event, a person with no
   account) are the zero value, nullable in SQL, omitted from JSON with `omitzero`, and
   validated by their owner only when set. No pointers to IDs.
7. **`Sensitivity`'s zero value is invalid, and the levels start at 1 in increasing order of
   restriction.** `privacy-data` says "when in doubt, pick the higher level"; a zero that
   silently meant `public` would be the opposite. Rejecting zero in `Validate` fails early
   (manifest validation at start-up), and the runtime methods treat an invalid value as
   `sensitive`, so neither path can under-protect. Keeping the natural order (1..4) lets
   callers compare with `<` as they would `slog.Level`.
8. **`ReadIsAudited` lives on the type.** The audit rule in `privacy-data` is a pure function
   of the level and "is this the person themself"; defining it once keeps `people`, `audit`
   and every module in agreement.
9. **`Sensitivity.MarshalText` fails for an invalid value** instead of emitting a
   placeholder, so an unset level in a response is a visible bug (error early). `String()`
   still returns `Sensitivity(<n>)`, like `errs.Kind.String`, so logs never panic.
10. **`TimeRange` is half-open and validates with field names** (`start`, `end`) through
    `validate.Errors`, not `timeutil.CheckRange`, so the API's 400 body can name the field;
    the rule is the same (`Start` before `End`, both set). Half-open so adjacent shifts
    (`[08:00, 12:00)` and `[12:00, 16:00)`) do not overlap.
11. **`Email` is in `model`,** because four core specs need the same value with the same
    uniqueness rule: `identity` (login name), `people` (profile), `notify` (recipient),
    `departments-events` (contact address). It is a string type with `Validate`, following
    the same pattern as every other model type (a converted value is validated at the
    boundary), not a sealed struct as `secret.Secret` is: an address must be printable to be
    sent and shown, so the guard is only against logging. `Canonical()` lower-cases the
    whole address, as D21 says "unique ignoring case"; nothing else is normalised. Case-only
    is confirmed by the owner (2026-10-16): no dot or `+tag` stripping, because that is
    provider-specific and would merge addresses that are genuinely different elsewhere.
12. **Error reasons never echo input.** `ParseID`, `ParseSensitivity` and `ParseEmail` build
    their `FieldError.Reason` from the rule ("must be a UUID in the form
    xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"), never from the value, so an e-mail address or a
    mistyped secret cannot leak through an error message or an API 400 body.

### Concurrency and lifecycle

Every value is immutable once built (the `UnmarshalText` methods write to their receiver
under the ordinary Go rule: not concurrently with reads of the same variable). `NewID` is
safe for concurrent use (`crypto/rand` is). No constructor takes dependencies, nothing has
state, nothing is started or closed, nothing reads the clock.

### I/O

None: no files, network or database. The only system interaction is `crypto/rand` reading
the operating system's entropy source, which on Go 1.24+ cannot fail.

### At the storage seam

A `postgres/` package converts an ID with `[16]byte(id)` and back with `model.PersonID(b)`,
an `Email` with `string(e)`, and a `Sensitivity` with `String()`/`ParseSensitivity`. pgx v5
scans and encodes named types whose underlying type is `[16]byte` or `string` through its
`uuid` and `text` codecs, so most stores need no explicit conversion; `db.md`'s integration
test proves that for `model.ID[E]` and `model.Email`, and the explicit conversion is always
available if a pgx version changes. `model` itself implements no `sql.Scanner` or
`driver.Valuer` (depguard: `database/sql` is denied outside `postgres/` and core `db`).

### Imports

Standard library (`crypto/rand`, `encoding/hex`, `errors`, `fmt`, `log/slog`, `strings`,
`time`, `unicode`), `internal/lib/errs`, `internal/lib/validate`, `internal/lib/secret`.
Nothing else: `foundation-model` in `.golangci.yml` enforces it.

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `ParseID` | length is not 36 | Invalid | 400 with field (api) / fix the caller | `TestParseID_WrongLength_ReturnsInvalid` |
| F2 | `ParseID` | hyphens not at positions 8, 13, 18, 23, or a non-hex digit | Invalid | as F1 | `TestParseID_BadCharacterOrHyphen_ReturnsInvalid` |
| F3 | `ParseID` | another UUID form: `{...}`, `urn:uuid:...`, 32 digits, surrounding space | Invalid | as F1 (one canonical form) | `TestParseID_NonCanonicalForms_ReturnInvalid` |
| F4 | `ID.UnmarshalText` | text fails F1–F3 | Invalid; `*id` unchanged | the decoder's caller wraps the decode error as Invalid | `TestIDUnmarshalText_Bad_ReturnsInvalidAndLeavesValue` |
| F5 | `ID.UnmarshalText` | receiver is nil | Internal | fix the call site; `encoding/json` never does this | `TestIDUnmarshalText_NilReceiver_ReturnsInternal` |
| F6 | `ID.Validate` | zero ID | Invalid | treat a required reference as missing | `TestIDValidate_Zero_ReturnsInvalid` |
| F7 | `ParseSensitivity` | not one of the four names (including different case or spaces) | Invalid; reason lists the names, never the input | 400 / reject the manifest | `TestParseSensitivity_Unknown_ReturnsInvalid` |
| F8 | `Sensitivity.Validate` | zero, or above `SensitivitySensitive` | Invalid | refuse the manifest or field declaration at start-up | `TestSensitivityValidate_ZeroAndOutOfRange_ReturnInvalid` |
| F9 | `Sensitivity.MarshalText` | zero or out of range | Invalid | fix the producer (a response carried an unset level) | `TestSensitivityMarshalText_Invalid_ReturnsInvalid` |
| F10 | `Sensitivity.UnmarshalText` | text fails F7 | Invalid; `*s` unchanged | as F4 | `TestSensitivityUnmarshalText_Bad_ReturnsInvalidAndLeavesValue` |
| F11 | `Sensitivity.UnmarshalText` | receiver is nil | Internal | fix the call site | `TestSensitivityUnmarshalText_NilReceiver_ReturnsInternal` |
| F12 | `TimeRange.Validate` | `Start` or `End` is the zero time (each reported, field `start`/`end`) | Invalid | 400 naming the field | `TestTimeRangeValidate_ZeroStartOrEnd_ReturnsInvalidPerField` |
| F13 | `TimeRange.Validate` | both set, `!Start.Before(End)` (field `end`) | Invalid | 400 naming `end` | `TestTimeRangeValidate_EndNotAfterStart_ReturnsInvalid` |
| F14 | `ParseEmail`, `Email.Validate` | empty (after trimming, for `ParseEmail`) | Invalid, field `email` | 400 / "required" | `TestParseEmail_Empty_ReturnsInvalid` |
| F15 | `ParseEmail`, `Email.Validate` | longer than 254 bytes, or local part longer than 64 bytes | Invalid | 400 | `TestParseEmail_TooLong_ReturnsInvalid` |
| F16 | `ParseEmail`, `Email.Validate` | zero or more than one `@`, empty local part, empty domain, domain starting or ending with `.` | Invalid | 400 | `TestParseEmail_BadShape_ReturnsInvalid` |
| F17 | `ParseEmail`, `Email.Validate` | whitespace, a control character, or any of `< > , ; "` inside | Invalid | 400 (also blocks header injection) | `TestParseEmail_ForbiddenCharacters_ReturnsInvalid` |
| F18 | `ParseEmail`, `ParseID`, `ParseSensitivity` | any failure | the `FieldError.Reason` does not contain the input | nothing; this pins decision 12 | `TestParseErrors_NeverEchoInput` |

Members that cannot fail, and why: `NewID` (`crypto/rand.Read` never errors on Go 1.24+,
and `go.mod` is 1.26); `ID.String`, `ID.IsZero`, `ID.MarshalText` (pure formatting of 16
bytes); `Sensitivity.String` (returns a name or `Sensitivity(<n>)`); `ReadIsAudited` (a
four-way table with a safe default); `Contains`, `Overlaps`, `Duration` (pure `time`
comparisons); `Canonical` (`strings.ToLower`); `LogValue` (one of two constants). Each has a
success-path test below.

## Lock-in check

No external technology. UUIDs are RFC 9562; PostgreSQL's `uuid` type and every other
database store them. Nothing here is specific to pgx, PostgreSQL or any encoder beyond the
standard `encoding.TextMarshaler`/`TextUnmarshaler`.

## Test plan

Unit tests only, no infrastructure, `go test -race`, table-driven, black-box package
`model_test` (depguard allows `model` to import itself in tests). Invented data only; no
real addresses.

- IDs: `TestNewID_IsVersion4Variant10AndNonZero` (byte 6 high nibble is 4, byte 8 top two
  bits are `10`); `TestNewID_DistinctAcross10000` (no duplicate in 10 000 IDs, as a sanity
  check, not a proof); `TestNewID_ConcurrentUse` (under `-race`);
  `TestParseID_RoundTripsString` (table of canonical lower, upper and mixed case, and the nil
  UUID); `TestIDString_IsLowercaseCanonical`; `TestIDJSON_RoundTripInStruct` (typed ID
  fields, `null` leaves the field unchanged, `omitzero` omits a zero ID, `omitempty` does
  not); `TestTypedIDs_ConvertToAndFromBytes` (`[16]byte(id)` and back, the storage-seam
  path); `TestIDIsZero_ZeroAndNonZero`; `TestEntityAlias_ModuleStyleDeclaration` (declares
  `type shift struct{}; type ShiftID = model.ID[shift]` in the test and uses every method,
  proving the two-line pattern for modules). That `PersonID` is not assignable to
  `DepartmentID` is a compile-time property of the type system and is documented, not
  tested at run time. Plus F1–F6.
- Sensitivity: `TestSensitivity_NamesRoundTrip` (table pinning each constant to its exact
  name; changing one is a breaking API change); `TestSensitivity_Ordering` (`Public <
  Internal < Personal < Sensitive`); `TestReadIsAudited_Table` (eight rows: four levels ×
  self, plus zero and out-of-range → audited); `TestSensitivityJSON_RoundTrip`; plus F7–F11.
- TimeRange: `TestTimeRange_ContainsHalfOpen` (start in, end out, before and after out);
  `TestTimeRange_OverlapsTable` (disjoint, touching, partial, nested, equal, and in mixed
  zones); `TestTimeRange_Duration`; `TestTimeRangeJSON_RoundTrip`; plus F12–F13.
- Email: `TestParseEmail_AcceptsTable` (plain, trimmed, upper-case, `+` tag, dots, a
  `localhost` domain, a non-ASCII local part); `TestEmailCanonical_LowercasesAndIsIdempotent`;
  `TestEmailLogValue_RedactsUnderTextAndJSONHandlers` (`slog.Any` on an `Email` and on a
  struct holding one yields the marker and never the address; empty yields `[unset]`);
  `TestEmailValidate_MatchesParseEmailWithoutTrimming` (a value with surrounding space fails
  `Validate` but `ParseEmail` of the same text succeeds); plus F14–F18.
- Lint: the package passes `foundation-model` (depguard), `errcheck`, `errorlint`,
  `wrapcheck`, `gosec` (uses `crypto/rand`, not `math/rand`).

## Open questions

None. The two questions in the first draft were answered by the owner on 2026-10-16:

1. **Event dates: instants or calendar dates?** Answered: instants. An event has a start and
   an end, each a date and a time; `departments-events` stores a `TimeRange` plus the IANA
   zone name and the UI renders in that zone; no civil `Date` type in `model` for now. The
   owner added that an event has further dates and times inside it (gate hours, possibly
   several windows; the temple burn, a single moment); those are for `departments-events`
   (Proposed change 8). Recorded in "Not responsible for".
2. **Canonical e-mail beyond case.** Answered: only ignore capitalisation; no dot or `+tag`
   stripping. Recorded in decision 11.

## Proposed changes elsewhere

1. **`plan/07-abstractions.md`, `model` entry:** replace "IDs, person reference, event,
   department reference, time range, money if needed, sensitivity level" with "typed IDs
   (`ID[E]`; `PersonID`, `AccountID`, `DepartmentID`, `EventID`), sensitivity level, time
   range, e-mail address"; drop "money if needed" (Q-PAY is open; added when a consumer
   exists); add "No I/O; generates IDs with the standard library".
2. **`.claude/skills/add-module/SKILL.md`, §2 or §3:** state how a module declares its own
   IDs: `type shift struct{}` and `type ShiftID = model.ID[shift]`; store them in `uuid`
   columns; never use another module's ID types.
3. **`plan/design/module-contract.md` (row 4, when written):** permission declarations carry
   the highest `model.Sensitivity` they reach (09-core-features §4 asks for a "flag for
   whether they reach sensitive data"; a level is strictly more informative for the audit
   reviewer and for separation-of-duties rules). Manifest validation calls `Validate()` on
   every declared level and ID. Decide where the permission-name type lives
   (`core/module` or `core/authz`); `model` does not hold it.
4. **`plan/design/db.md` (row 5):** the integration test proves pgx scans and encodes
   `model.ID[E]` into `uuid` columns and `model.Email` into `text` columns by underlying
   type, and documents the explicit `[16]byte` conversion as the fallback.
5. **`plan/design/identity.md` (row 6) and `people.md` (row 8):** store the address as
   entered and index `Email.Canonical()` uniquely (D21); the login lookup is by canonical
   form.
6. **`plan/design/api.md` (row 16):** path and query IDs are parsed with `model.ParseID`
   (Invalid → 400 with the field, through the existing `validate.FieldError` path); the
   OpenAPI document describes an ID as `string, format: uuid` (canonical form), a
   sensitivity as an enum of the four names, and a time range as `{start, end}`.
7. **`plan/design/lib-core.md`, `timeutil` consumer table:** `CheckRange` is no longer
   listed as used by `model.TimeRange.Validate` (decision 10); module stores remain its
   consumers.
8. **`plan/design/departments-events.md` (when written):** design the schedule items within
   an event that the owner described (2026-10-16): windows such as gate hours, of which an
   event may have several, and single moments such as the temple burn. They are expected to
   use `model.TimeRange` for a window and a single instant (`time.Time`) for a moment, both
   rendered in the event's IANA zone. `model` adds nothing for them.
