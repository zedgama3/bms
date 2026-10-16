# lib/secret: a string that cannot be printed

- **Status:** Implemented (ported from Sojourn, D11, 2026-10-16). Review items C1–C5 from the original
  are applied. The redacted wire form is an open question for `api.md` (proposed: a readOnly enum in OpenAPI).
- **Package:** `internal/lib/secret` (module `github.com/zedgama3/bms`)
- **Depends on specs:** `lib-errs.md`: `errs.E` and the kinds `Invalid`, `Internal`. Nothing
  else: this is a foundation package and imports only the standard library and
  `internal/lib/errs`.
  **Used by:** `config` (every secret key), `identity` (local-account passwords and API
  tokens in transit before hashing), `notify` (the SMTP password, read once by the e-mail
  sender), `db` (the connection string or password, read once when opening the pool), `files`
  (S3-compatible access keys), `app` (passes config secrets to constructors), and every test
  that builds one of those values by hand.

## Purpose

Hold a string so that no formatting, logging or encoding path can print it.

## Responsibilities

- Define the `Secret` value, whose zero value means "unset", and the two ways in and out of
  it: `New` and `UnmarshalText` to put a string in; `Value` to read it back; `IsSet` to ask
  whether there is one.
- Redact on every output path Go offers: `fmt` (`Format`, `String`, `GoString`), `log/slog`
  (`LogValue`) and the text encoders (`MarshalText`, which `encoding/json`, `encoding/xml`
  and `text/template` consult).
- Make the redaction hold even when a formatter bypasses the methods above and falls back to
  reflection (the bad-verb and `%p` paths in `fmt`): the string is held behind a pointer, so
  those paths print an address, never the bytes.
- Define the two marker strings, `Redacted` and `Unset`, once, so tests, the API and the UI
  reference one spelling.
- Refuse to decode its own markers, so a Secret that was marshalled for display cannot come
  back as a live password whose value is the word "[redacted]".

## Not responsible for

| Concern | Owner |
|---|---|
| Encrypting, sealing or opening a secret | not needed in bms today; a future owner (for example a settings store holding secrets) decides |
| Persisting secrets | the owning package, which stores a hash or a sealed form; a `Secret` is never written to a database or a file as itself (its text form is a marker) |
| Reading secrets from the environment or files, trimming trailing newlines, size caps | `config` (expected: environment values and `*_FILE` files, with their own size caps; settled in `config.md`) |
| Validating the content: length, UTF-8, NUL bytes, "required" | the type that holds the field: `config.Validate` or the owning module's request validation |
| Bounding the length of a decoded value | the reader of the containing document (the file is already capped); see decision 5 |
| Comparing two secrets, in constant time or otherwise | no consumer exists; see decision 6. A future caller uses `crypto/subtle.ConstantTimeCompare` on `Value()` at the call site |
| Zeroing the memory after use | nobody. Go strings are immutable and garbage-collected; this type does not promise erasure and no spec may claim it does |
| Defending against code that deliberately reads the unexported field through `reflect` or `unsafe` | out of scope. The type prevents accidental output; it is not a boundary against code in the same process |
| Choosing what to log and at which level | the package that logs, per `go-conventions` |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `Secret` | Hold one string behind a pointer so that even reflective output shows an address. | `struct{ p *string }`, a plain pointer to a string. Zero value is unset. |
| Redaction | Yield a marker on every output path. | `Format`, `String`, `GoString`, `LogValue`, `MarshalText`. All constant-time, all pure. |
| Text input | Fill a `Secret` from text so a struct holding one can be decoded by `encoding/json`. | `UnmarshalText`. The only method with a pointer receiver and the only one that can fail. |
| Markers | Name the two fixed strings. | `Redacted`, `Unset`. Exported so there is one spelling. |

## Interface

```go
// Package secret holds a string that must never be printed. Every formatting, logging and
// encoding path yields a marker instead of the value; only Value returns it. The package
// depends on the standard library and internal/lib/errs.
package secret

const (
	// Redacted is the text a set Secret prints, logs and marshals as.
	Redacted = "[redacted]"
	// Unset is the text an unset Secret prints, logs and marshals as.
	Unset = "[unset]"
)

// Secret holds a value that must never be printed. The zero value (nil pointer) is "unset".
// The value is held behind a pointer so that fmt's reflective fallbacks (a bad verb on a
// struct, %p) see a pointer, never the plaintext bytes. Every formatting path (fmt verbs,
// encoding/json, slog, templates) yields a marker.
//
// A Secret holds a pointer, so == compares identity: two New("x") are not equal, copies of
// one are. Compare Value() instead; never use == or reflect.DeepEqual on a Secret.
type Secret struct{ p *string }

// New wraps v; an empty v yields the zero (unset) Secret. Cannot fail.
// Consumers: config (binding each secret key), tests that
// build a Config by hand.
func New(v string) Secret

// Value returns the secret, or "" when unset. This is the only way to read it. Cannot fail.
// Consumers: identity (hashing or comparing a password or token), notify's SMTP sender (the
// SMTP password, read once), db (building the connection string), app (passing config
// secrets to constructors) and config.Validate (length and charset checks).
func (s Secret) Value() string

// IsSet reports whether the secret is non-empty. Cannot fail.
// Consumers: config.Validate ("required when ..."), app.
func (s Secret) IsSet() bool

// String returns Redacted when set and Unset when not. Cannot fail.
// Consumers: fmt (via Format), text/template, any caller that wants the marker.
func (s Secret) String() string

// GoString is String, so %#v on a Secret held in an interface cannot leak the value.
// Cannot fail. Consumer: fmt.
func (s Secret) GoString() string

// Format implements fmt.Formatter and is the path fmt takes for every verb except %T, %p and %w,
// which fmt handles itself before consulting a Formatter (%w takes the reflective path and
// prints the pointer's address, never the value; review C1, 2026-10-16). For every other verb (including
// %+v, %#v, %d, %x, %c and unknown verbs) Format writes the marker, formatted with
// fmt.FormatString(f, verb) where verb is 'q' for %q and 's' otherwise, so width, precision
// and flags apply and %q quotes. It never writes the value and never panics. A failed write
// to f is dropped: it has nowhere to be reported, and the value was never written, which is
// the only property that matters here. Consumer: fmt, and through it log/slog's text
// handler, errors built with fmt.Errorf, and test output.
func (s Secret) Format(f fmt.State, verb rune)

// LogValue returns the marker as a slog string value, so a slog handler never sees the
// Secret itself. Cannot fail. Consumers: log/slog, via slog.Any on a Secret or a *Secret.
func (s Secret) LogValue() slog.Value

// MarshalText returns the marker as bytes and a nil error, so encoding/json, encoding/xml
// and any other TextMarshaler consumer emit the marker. It never returns the value. This
// makes a Secret display-only in encoded form: see UnmarshalText for the consequence.
// Consumers: encoding/json (API responses and debug dumps that contain a Secret), slog's
// JSON handler (through LogValue first, MarshalText as the fallback).
func (s Secret) MarshalText() ([]byte, error)

// UnmarshalText sets *s to text, verbatim: no trimming, no normalisation. Empty text yields
// the zero (unset) Secret, exactly like New(""). It fails with errs.Invalid when text equals
// Redacted or Unset, because that input is almost certainly a marshalled Secret being fed
// back in, and accepting it would turn a display artefact into a live password; and with
// errs.Internal when s is nil. It never panics. The owner of the containing struct
// validates the result (length, UTF-8, NUL); this method does not.
// Consumer: config and settings loaders decoding JSON or text values with encoding/json.
func (s *Secret) UnmarshalText(text []byte) error
```

### Who needs each member

| Member | Consumer | Pre-condition | Post-condition |
|---|---|---|---|
| `Redacted`, `Unset` | tests in `config`, `identity`, `notify` (assert the exact marker); `api` (documents the wire form of a redacted field) | — | compile-time constants |
| `New` | `config` bind, tests | — | `New("").IsSet() == false` and `New("") == Secret{}`; otherwise `Value() == v` |
| `Value` | `notify` SMTP sender, `db`, `identity`, `app`, `Validate` methods | — | the exact string given to `New` or `UnmarshalText`, or `""` |
| `IsSet` | `config.Validate`, `app` | — | `IsSet() == (Value() != "")` |
| `String`, `GoString` | `fmt`, `text/template` | — | `Redacted` or `Unset`; never the value |
| `Format` | `fmt` (and so slog text handler, `fmt.Errorf`, test output) | any verb, any flags | writes the marker once, formatted; never the value; never panics |
| `LogValue` | `log/slog` | — | `slog.StringValue(marker)` |
| `MarshalText` | `encoding/json`, `encoding/xml`, slog JSON fallback | — | `[]byte(marker), nil` |
| `UnmarshalText` | config and settings loaders through `encoding/json` | `s != nil` | `Value() == string(text)` on success; `*s` unchanged on failure |

## Types

Defines `Secret` only. Uses nothing from `model` (layer rule 1: `lib/*` depends on nothing in
the project except `lib`).

## Behavior

### Decisions settled here, with reasons

1. **The shape is `struct{ p *string }`, a plain pointer to a string**.
   A `struct{ v string }` shape leaks under `%d`: `fmt` calls
   `Formatter` for every verb except `%T` and `%p`, but when it hits a bad verb it sets an
   "erroring" flag that bypasses method lookup and prints the struct reflectively, field by
   field. With a `string` field that path prints the password; with a `*string` field it
   prints an address (`%!p(secret.Secret={0xc000012345})`). The same reflective path is what
   a `Secret` in an *unexported* field of some other struct takes under any verb, because
   `fmt` cannot call methods on a value it cannot `Interface()`; it prints
   `{0xc000012345}` there too. A pointer field is therefore not a style choice but the second
   line of defence, and it stays.
2. **`Format` handles every verb, and `%q` quotes.** The marker is formatted with
   `fmt.FormatString(f, 's')`, or `'q'` when the caller asked for `%q`, so width, precision
   and flags survive: `%-12v|` gives `[redacted]  |`, `%.3s` gives `[re`, `%q` gives
   `"[redacted]"`. `%+v` and `%#v` print the bare marker (the `+` and `#` flags mean nothing to
   `%s`). This spec settles `%q`: it quotes, because a caller who asked for quotes is building a line for a parser and the
   quotes are part of its grammar.
3. **`MarshalText` redacts.** `encoding/json` is how API responses and debug dumps are
   produced, and a `Secret` sits inside `config.Config` and any module or core
   settings struct. Any of those must be safe to marshal without the author remembering to
   strip a field. The alternative, a `MarshalJSON` that returns an error, would make every
   struct that holds a Secret unmarshalable and push the redaction work onto each caller.
   Consequence, stated plainly: **the encoded form of a Secret is for display only. A Secret
   never survives a JSON round trip**, and decision 4 makes that failure loud rather than
   silent. Persistence goes through the owning package's hashed or sealed form, never the `Secret`.
4. **`UnmarshalText` is verbatim, and refuses the two markers.** Verbatim because a
   JSON file the operator writes can hold a password that may legitimately start or end with a
   space; trimming is the file reader's business if it wants it (config is expected to trim trailing
   newlines for `_FILE` values; a JSON decode does not). Refusing `Redacted` and `Unset` with
   `errs.Invalid` because the only realistic way that text arrives is a marshalled Secret fed
   back in (a config dump saved and reloaded, a password copied from an API response). If
   it were accepted, a login or an SMTP connection would be attempted with the password
   "[redacted]", and repeated failed logins can lock accounts. A real password
   that is literally one of the two markers is refused too; that cost is accepted and this
   doc comment is the notice. A nil receiver returns `errs.Internal` rather than panicking:
   `encoding/json` never passes one, so it only arises from a direct call, which is a bug, and
   library code returns errors (`go-conventions`).
   `encoding/json` specifics, each pinned by a test:
   - A JSON string calls `UnmarshalText` with the decoded bytes (escapes already resolved;
     invalid UTF-8 already replaced with U+FFFD by `encoding/json`, as it does for any
     string). `""` yields the unset Secret.
   - JSON `null` on a `Secret` field leaves the field unchanged, because `encoding/json`
     never calls a `TextUnmarshaler` for `null`. A fresh struct therefore stays unset. On a
     `*Secret` field `null` sets the pointer to nil.
   - A JSON number, bool, array or object on a `Secret` field is a `*json.UnmarshalTypeError`
     raised by `encoding/json` before `UnmarshalText` runs. It carries no `errs` kind; the
     owner (the loader) wraps the whole decode error as `errs.Invalid`.
   - The `errs.Invalid` error from `UnmarshalText` reaches the caller of `json.Unmarshal`
     unwrapped, so `errs.Is(err, errs.Invalid)` holds at the loader's boundary.
   - Map values (`map[string]Secret`) decode correctly: `encoding/json` decodes into an
     addressable temporary and stores it.
   - The `omitzero` struct tag omits an unset Secret (it is the zero struct); `omitempty`
     does not, because `omitempty` never applies to struct values. Specs that want an unset
     secret absent from a response use `omitzero`.
5. **No length cap in `UnmarshalText`.** Every document that can contain a Secret is already
   bounded by its reader (for example a size-capped `*_FILE` read in `config`), and
   the value's own bound differs by owner (a connection string may be long; a password has
   its own cap). A cap here would be a second constant to keep in sync with one of
   them and wrong for the other. The owner validates after decoding, as it does for every
   other field.
6. **No `Equal`, constant-time or otherwise.** No consumer compares two Secrets: local-account
   passwords are checked against stored hashes (`identity`), and nothing diffs config. A method nobody needs is a method
   nobody tests. When a consumer appears, it writes
   `subtle.ConstantTimeCompare([]byte(a.Value()), []byte(b.Value()))` at its call site, which
   is one line and keeps the policy (is timing even a concern there?) with the caller. The
   alternative of making the type incomparable (adding a `_ [0]func()` field so `==` is a
   compile error) was considered and rejected: an incomparable Secret
   makes every struct that holds one incomparable too (`config.Config`),
   which breaks ordinary `got != want` table tests on those types. The rule "compare
   `Value()`, never `==`" is stated in the type's doc comment and in `go-conventions`
   ("Logging and observability").
7. **`New("")` is the zero value.** An empty secret and an unset one are the same thing: both
   print `Unset`, both have `IsSet() == false`, both are `== Secret{}`. This keeps "the zero
   value is unset" true without a separate empty state, and it is what `config.Validate`
   relies on when a key is absent. `UnmarshalText` with empty text follows the same rule.
   `IsSet` still checks both `p != nil` and `*p != ""` so the property holds even
   for a Secret built by reflection in a test.
8. **The markers are exported constants.** Tests in `config`, `identity` and `notify` assert
   the exact text; the API will document it as the wire form of a redacted field; the UI will
   display it. One spelling in one place, instead of a literal in each.
9. **The constructor is `New`, not `NewSecret`.** `secret.NewSecret` stutters; `secret.New` is
   idiomatic for a single-type package.

### Concurrency

A `Secret` is immutable once built by `New`: copies share a pointer to a Go string, which is
itself immutable, so any number of goroutines may call `Value`, `IsSet` and the output
methods on the same or copied values concurrently. `UnmarshalText` writes to its receiver and
has the same rule as any Go assignment: not concurrently with reads of that same variable.
`encoding/json` decodes into a value it owns until `Unmarshal` returns, so this never arises
in practice.

### Lifecycle and configuration

None. No state beyond the value, no `New…` that can fail, no `Close`, no settings.

### Imports

`fmt`, `log/slog`, `errors` (to build the cause passed to `errs.E`), and
`github.com/zedgama3/bms/internal/lib/errs`. The `foundation-lib` depguard rule already
allows `lib` to import `lib`.

## Failure modes

| # | Member | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `UnmarshalText` | `string(text) == Redacted` | Invalid (op `secret.Secret.UnmarshalText`) | the loader: report the entry as invalid, without echoing the text; do not round-trip Secrets | `TestUnmarshalText_RedactedMarker_ReturnsInvalid` |
| F2 | `UnmarshalText` | `string(text) == Unset` | Invalid | as F1 | `TestUnmarshalText_UnsetMarker_ReturnsInvalid` |
| F3 | `UnmarshalText` | receiver is nil | Internal | fix the call site; `encoding/json` never does this | `TestUnmarshalText_NilReceiver_ReturnsInternal` |
| F4 | `UnmarshalText` | F1 or F2 reached through `json.Unmarshal` | Invalid, unwrapped: `errs.Is(err, errs.Invalid)` is true on the error `json.Unmarshal` returns; the target field is unchanged | the loader: wrap as Invalid, exit at start-up | `TestSecret_JSONRoundTrip_FailsInvalid` (set and unset cases) |
| F5 | `json.Unmarshal` into a `Secret` | the JSON value is not a string or `null` | `*json.UnmarshalTypeError` from `encoding/json` (unkinded; `errs.KindOf` says Internal); `UnmarshalText` is not called | owner wraps as Invalid | `TestSecret_JSONNonString_TypeErrorNotCalled` |
| F6 | `Format` | `f.Write` returns an error | no error path exists (`fmt.Formatter` has none); nothing further is written; no panic | nothing; the value was never written | `TestFormat_WriterFails_NoPanicNoLeak` (a fake `fmt.State` whose `Write` fails) |
| F7 | `gob.Encode` of a value containing a `Secret` | always | `encoding/gob` rejects the type ("has no exported fields"); the bytes written before the error contain no value | do not gob a Secret; this is the intended outcome | `TestSecret_Gob_EncodeFailsWithoutLeak` |

Members that cannot fail, and why: `New` (any string is a valid Secret; it allocates one
string header); `Value` and `IsSet` (a nil check and a dereference of a pointer the type
itself set); `String` and `GoString` (return one of two constants); `LogValue`
(`slog.StringValue` of a constant); `MarshalText` (returns a constant's bytes and a literal
`nil`). Each has a success-path test in the plan below.

## Lock-in check

No external technology. Standard library plus `lib/errs`, satisfying layer rule 1 (`plan/07-abstractions.md`).
Nothing here is specific to any encoder: `encoding.TextMarshaler`/`TextUnmarshaler` are the
standard interfaces every Go encoder honours.

## Test plan

Unit tests only, no infrastructure, `go test -race`. External test package `secret_test` in
`internal/lib/secret/secret_test.go`, plus one internal test file (`package secret`) for the
one assertion that needs the unexported field. Table-driven. Every test that checks for a
leak uses a marker value that cannot occur by accident (`"ZQ9MARKER7"`: upper case, no
punctuation, ten bytes) and asserts `!strings.Contains(out, marker)`.

### Behaviour (success paths)

- `TestNew_ValueAndIsSet`: table over `""`, `"x"`, a 64 KiB string, a string with leading and
  trailing spaces, a string containing `"\x00"` and invalid UTF-8: `Value()` returns it
  byte-for-byte; `IsSet()` is `len(v) > 0`; `New("") == Secret{}`.
- `TestSecret_ZeroValue_IsUnset`: `Secret{}` and `New("")` both: `IsSet()` false, `Value()`
  `""`, every verb prints `Unset`, `String()` is `Unset`.
- `TestSecret_RedactionConstants_AndInterfaces`: `String`, `GoString`, `MarshalText` and
  `LogValue().String()` all equal `Redacted` for a set Secret and `Unset` for an unset one;
  `MarshalText` returns a nil error; compile-time `var _ fmt.Formatter`, `fmt.Stringer`,
  `fmt.GoStringer`, `encoding.TextMarshaler`, `slog.LogValuer` on `Secret` and
  `encoding.TextUnmarshaler` on `*Secret`.
- `TestFormat_HonoursWidthPrecisionAndFlags`: exact expectations, set and unset:
  `%v`→`[redacted]`, `%-12v|`→`[redacted]  |`, `%12s`→`  [redacted]`, `%q`→`"[redacted]"`,
  `%20q`, `%.3s`→`[re`, `%d`, `%x`, `%X`, `%08d`, `% x`, `%#x`, `%c`, `%U`, `%t`, `%e`, `%o`,
  `%b`, `%#v`, `%+v`→`[redacted]`, `%z`→`[unset]` for the unset case. No output contains the
  value.
- `TestSecret_FormattingNeverLeaks`: the cross product of subjects × output paths, each
  checked for the marker. Subjects: `s`, `&s`, `embedsSecret{Secret: s}` and its pointer,
  `holdsSecret{S: s, P: &s, u: s}` (an exported field, a pointer field, and an **unexported**
  field) and its pointer, a struct that holds `holdsSecret` in an unexported field (depth 2),
  `[]Secret{s}`, `[1]Secret{s}`, `map[string]Secret{"k": s}`, `any(s)`, `reflect.ValueOf(s)`.
  Paths: every verb in the list above plus `%T` and `%p`; `fmt.Sprint`, `fmt.Sprintln`,
  `fmt.Errorf("x: %v", s).Error()`, `fmt.Errorf("x: %w", fmt.Errorf("%v", s))`;
  `json.Marshal` of each subject that `encoding/json` accepts; `json.MarshalIndent`;
  `encoding/xml` `Marshal` of `holdsSecret`; `text/template` `{{.}}`, `{{.S}}`, `{{printf
  "%v" .S}}`, `{{.S.Value}}` is **not** rendered (the test only confirms the first three are
  redacted); `slog` text and JSON handlers with `slog.Any` on `s`, `&s`, `holdsSecret`,
  `&holdsSecret`, plus `slog.String("k", s.String())`. The test also asserts the positive:
  `%s`, `%v`, `%+v`, `%#v`, `%d`, `%q` on `s` contain `redacted`.
- `TestFormat_UnexportedField_ShowsAddressNotBytes`: `fmt.Sprintf("%v", holdsSecret{u: s})`
  and `%#v` of the same contain `0x` and not the marker (pins decision 1: the reflective path
  sees a pointer). `fmt.Sprintf("%p", s)` starts with `%!p(secret.Secret=` and contains no
  marker.
- `TestLogValue_PointerAndValue_Resolve`: `slog.Any("k", s)` and `slog.Any("k", &s)` both
  render the marker under the JSON and text handlers.
- `TestUnmarshalText_Verbatim`: table over `"pw"`, `"  pw  "`, `"a\tb"`, `"ünïcödé"`, a 64 KiB
  string, `"[redacted"` (marker prefix, accepted), `"[unset]x"` (accepted): `Value()` equals
  the input exactly, `IsSet()` true; `""` yields `Secret{}`.
- `TestSecret_JSONDecode_StringAndMap`: `json.Unmarshal` of `{"S":"  pw  ","P":"x"}` into
  `holdsSecret` gives `S.Value() == "  pw  "` and `P.Value() == "x"`; `map[string]Secret`
  decodes `{"k":"v1"}` to `Value() == "v1"`.
- `TestSecret_JSONDecode_NullLeavesUnchanged`: a `holdsSecret{S: New("keep")}` decoded from
  `{"S":null}` still has `S.Value() == "keep"`; a fresh struct stays unset; a `*Secret` field
  becomes nil.
- `TestSecret_JSONDecode_EmptyStringIsUnset`: `{"S":""}` gives `IsSet()` false.
- `TestSecret_JSONEncode_OmitZeroVsOmitEmpty`: a struct with two unset Secret fields, one
  tagged `omitzero` (key `z`) and one tagged `omitempty` (key `e`), marshals to a document
  that has `"e":"[unset]"` and no `z` key.
- `ExampleNew`, `ExampleSecret_Format`: runnable examples showing `fmt.Println(s)` →
  `[redacted]` and `s.Value()` for the one legitimate read.

### Failure modes

F1 through F7, one test each, named in the table. `TestFormat_WriterFails_NoPanicNoLeak` uses
a hand-written `fmt.State` fake whose `Write` returns an error and records what it was asked
to write; it asserts no panic and that the recorded bytes never include the value.
`TestSecret_Gob_EncodeFailsWithoutLeak` encodes `s` and `holdsSecret` into a buffer, asserts
`Encode` returns a non-nil error and the buffer does not contain the marker.

### Lint

The package passes `errcheck` (the `Fprintf` result in `Format` is checked and deliberately
dropped with a comment), `errorlint`, `wrapcheck` (the `errs.E` cause is built
with `errors.New`), `gosec`, and the `foundation-lib` depguard rule.

## Open questions

1. **Wire form of a redacted field in API responses.** This spec makes `"[redacted]"` and
   `"[unset]"` the JSON text of a Secret, and the frontend will see them wherever a response
   struct carries a Secret. Proposed answer: `api.md` adopts that as the documented form (the
   OpenAPI schema marks such fields `readOnly` with an enum of the two markers) rather than
   stripping the fields, so the UI can show "a password is set" without a second field. To be
   confirmed when `api.md` is written; nothing here changes either way.

## Proposed changes elsewhere

1. **`plan/design/config.md` (row 3):** use `secret.Secret` for every secret key, with
   `secret.New` for binding; add `internal/lib/secret` to config's import list.
2. **`plan/design/notify.md` (row 11), `identity.md` (row 6), `db.md` (row 5), `files.md`
   (row 13):** hold passwords, tokens and access keys as `secret.Secret` and read them once
   with `Value()`.
3. **Done (2026-10-16).** `.claude/skills/go-conventions/SKILL.md`, "Logging and observability": extend the
   bullet: "Hold them in `secret.Secret`, read them once with `Value()`, and compare with
   `Value()`, never with `==`."
