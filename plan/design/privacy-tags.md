# privacy-tags

- **Status:** Approved (2026-10-20, D44)
- **Amendment A1 (approved 2026-10-21, D45):** `Viewer.Extents` is keyed by group name, not by permission; `ViewerOf(ctx, az, person model.PersonID, groups)` takes the actor because `authz.Authorizer` cannot tell it who the actor is and `privacy` may not import `identity` (a zero `person` is `Unauthenticated`); every grouped field needs `omitempty`/`omitzero` whatever its kind (a zeroed pointer without it serializes `null`, which is a marker); `Check` also rejects an auditable field whose JSON name does not fit `audit.Entry.Fields`; the conformance rules `views_tagged` and `visibility_settings_declared` run right after `manifest_valid`; the `privacy.audience.*` keys live in the shell's catalogues under `web/`; `hello` tags and lists all four of its wire types. Folded into the text below; "Amendment A1" before Open questions is kept as the record, with the test expectations that changed.
- **Package:** `internal/core/privacy` (+ `privacytest`)   **UI:** none
- **Depends on specs:** `model` (`Sensitivity`, `PersonID`, `DepartmentID`), `lib-errs`, `lib-core` (`validate`), `authz` (`Authorizer.ExtentOf`, `Extent`, `Permission`, `PermissionDecl.Reaches`), `settings` (`Reader`, `Decl`, `Enum`), `audit` (the `Entry.Fields`/`Level` shape a filter result feeds), `module-contract` (`Manifest`, `moduletest`)   **Used by:** every module with response types (`volunteers` first, `hello` as the template), `module` (`Manifest.Validate`, `moduletest`), later `people`, `identity`, `notify` (Proposed changes)

## Summary

**Purpose.** Decide, field by field, which of a record's fields a viewer gets back, from a `privacy` struct tag on each response field and the regional's choice of audience per named field group. Package `internal/core/privacy`; imports `model`, `lib`, `core/authz` (the `Authorizer` and leaf types), `core/settings` (the `Reader` and leaf types), `core/audit` (the constants `MaxFields`, `MaxFieldBytes` only). Never `core/identity`, `core/module`, `core/people`, `api`, `app`, `config` or a module. No tables, routes, signals or screens. `core/module` imports it (`Manifest.Views`, `Manifest.Groups`).

**The tag.** `privacy:"<level>[,group=<name>]"` on every exported field of a response type that reaches the wire (`json:"-"` fields are exempt). `<level>` is a `model.Sensitivity` name. A `personal` or `sensitive` field must name a group; a `public` or `internal` field may. A grouped field's `json` tag must carry `omitempty` or `omitzero`, whatever the field's kind (a zeroed pointer, slice or map without one serializes as `"x": null`, which is a marker), so a hidden field is absent, never `null` or empty. An auditable field (`personal` or `sensitive`) has a JSON name that fits `audit.Entry.Fields`: `[a-z][a-z0-9_]*`, at most `audit.MaxFieldBytes`.

**Exported (`privacy`).**
- `Audience` (ident; built-in `AudienceNobody = "nobody"`, `AudienceSelf = "self"`, `AudienceHolders = "holders"`, `AudienceSignedIn = "signed_in"`; `Validate`).
- `Group{Name string, Permission authz.Permission, Default Audience, Options []Audience, Extra []Audience}` (+`Validate`): a named set of fields the regional configures as one setting `visibility_<name>` (kind `Enum`, options = `Options`, default `Default`). `Permission` is who counts as `holders` (held site-wide, or at the record's department when department-scoped). `Extra` names module-defined audiences (for example `booked`) the service asserts per record.
- `Settings(groups []Group) ([]settings.Decl, error)`: the `visibility_<name>` declarations a manifest or core area appends to its settings.
- `Check(views []any, groups []Group) error`: the conformance check over response types (every exported wire field tagged, levels valid, groups known, personal/sensitive grouped, grouped fields `omitempty`/`omitzero`, auditable names within audit's grammar and length, nested structs walked).
- `Policy` (`map[string]Audience`, one entry per group); `PolicyOf(ctx, settings.Reader, []Group) (Policy, error)` reads the current audiences (never cached; settings decision 10).
- `Viewer{Person model.PersonID, Extents map[string]authz.Extent}` (keyed by **group name**: where the viewer holds that group's `Permission`); `ViewerOf(ctx, authz.Authorizer, person model.PersonID, []Group) (Viewer, error)` (the service passes the actor it already has; a zero `person` is `Unauthenticated` without calling authz): one `ExtentOf` per distinct group permission, its extent copied to every group naming it; a `Permission` error means "not held" (no entry). The zero `Viewer` is anonymous; a public use case passes `privacy.Viewer{}` to `Filter` and never calls `ViewerOf`.
- `Relation{Subject model.PersonID, Department model.DepartmentID, Also []Audience}`: what one record is to the viewer.
- `Result{Fields []string, Level model.Sensitivity}`: the JSON names of the returned fields that must be audited (level `personal` about someone else, or `sensitive`), and the highest level among them; `Result.Audited()` reports whether an entry is due.
- `Filter(v any, pol Policy, viewer Viewer, rel Relation) (Result, error)`: zeroes, in place, every field of `*v` the viewer may not see; shallow (top-level and embedded fields); the service calls it once per record that has its own subject.
- `(Policy).Shows(group string, viewer Viewer, rel Relation) bool`: the predicate `Filter` applies, for a service to ask before loading an expensive field.
- `privacytest.AssertHidden(t, body []byte, jsonNames ...string)`, `privacytest.AssertPresent(t, body, names...)`.

**The rule** (`Shows`). The viewer's audiences for a record are `{signed_in}` when `viewer.Person` is set, plus `self` when it equals `rel.Subject`, plus `holders` when `Extents[group.Name]` is `Site` or covers `rel.Department`, plus `rel.Also`. A grouped field is returned when the policy's audience for its group is in that set, or when the viewer is the subject and the audience is not `nobody`; `nobody` never. An ungrouped field: `public` always, `internal` for a signed-in viewer. No superuser special case: the superuser holds every permission, so is a `holders` everywhere, and is not `self` anywhere.

**Key decisions.** 1 Enforcement is in the service, not in `httpx.WriteJSON` (decision 2 under Behavior): the service knows the subject, the department and the per-record audiences; the encoder knows none, and the audit entry must be recorded before the response is written. 2 The tag is the single declaration (level and group); who may see a group is data (`visibility_<group>` settings) so a regional changes it without code, and the audience vocabulary is extensible per module (`Extra`) so "booked on this shift" is expressible without a core change. 3 A hidden field is absent, never a placeholder, and the response says nothing about why (privacy-data: a `403` or a marker would confirm what exists). 4 Audit stays the caller's: `Filter` reports what must be audited; the service records `<module>.<record>.read` after loading and before returning (go-conventions). 5 Action permissions stay authz's: tags never decide whether a use case runs or a record is visible; they shape the fields of a record the service has already decided to return. 6 `Reaches` stays declared on permissions (authz uses it for separation-of-duties rules); `Manifest.Validate` cross-checks it against the groups a module's own permission guards. 7 Types are parsed once and cached (`sync.Map` by `reflect.Type`); `Filter` is a loop over a parsed plan, no allocation per field. 8 First version: no nested recursion in `Filter`, no OpenAPI generation, no browser-side counterpart, no per-person consent integration (Later).

**Errs kinds callers handle.** `Check`, `Group.Validate`, `Settings`: `Invalid` (field paths `views[i].<Field>`, `groups[j].<field>`; codes `required`, `malformed`, `one_of`, `unknown`, `duplicate`, `not_omittable`, `ungrouped`, `not_struct`, `too_many`, `too_long`). `Filter`: `Internal` (not a pointer to a struct; an untagged field or unknown group reached at run time; a group missing from `pol`): fail closed, nothing returned. `ViewerOf`: `Unauthenticated` (zero `person`), `Invalid` (an undeclared permission), store kinds; never `Permission`. `PolicyOf`: the reader's kinds (`Invalid`, `Unavailable`, `Timeout`, `Canceled`). `Shows`: cannot fail (unknown group is `false`).

**Owns.** The tag grammar; the setting key pattern `visibility_<group>` in every area that declares groups (titles `module.<name>.setting.visibility_<group>.title|description|option.<audience>`, core areas `core.<area>.setting.…`); `Manifest.Views []any` and `Manifest.Groups []Group` (module-contract Proposed change 1); the `moduletest` rules `views_tagged` and `visibility_settings_declared` (run right after `manifest_valid`); the catalogue keys for the built-in audiences `privacy.audience.<name>`, which are frontend text in the shell's catalogues `web/src/shell/messages/<lang>.json` (web-shell Proposed change 5); no Go package holds them.

**Expects of dependencies.** `authz`: `ExtentOf` answering `Permission` when not held, `Extent.Covers`, `PermissionDecl.Reaches`. `settings`: a scoped `Reader.Text` for `Enum`, decision 5 (an unparseable stored value yields the default). `module`: `Manifest.Validate` calls `Check` and the `Reaches` and settings cross-checks; `moduletest.Conformance` runs `views_tagged` and `visibility_settings_declared` second and third, and its own fixture modules (routes, no response type) pass `WithoutViews()`. `audit`: `Entry.Fields` names match `[a-z][a-z0-9_]*`, at most `MaxFieldBytes` each and `MaxFields` per entry; `Check` enforces all three at build time. `people` (Proposed change 3): `Directory.LegalNamesOf` for the first `personal` group a module shows.

**Open questions:** none: Q-PRIV1 to Q-PRIV4 were settled with the recommendations (owner, 2026-10-20, D44): the four built-in audiences plus module extras; `people` adopts the tags later; OpenAPI marking Later; the `volunteer-coordinator` starter role is added. **Proposed changes elsewhere:** 9, listed at the end; 1 to 8 applied 2026-10-20 (D44); 9 (`06-open-questions.md`) is the plan-keeper's. **Amendment A1** (approved 2026-10-21, D45) is the section before Open questions; it is folded into the text, and its matching edits to `module-contract.md`, `module-hello.md` and `module-volunteers.md` are in those specs.

## Purpose

Decide, field by field, which of a record's fields a viewer gets back.

## Responsibilities

- The `privacy` struct tag: its grammar, parsing and caching per type.
- Named field groups and the `visibility_<group>` setting each one becomes, with the audiences a regional may choose.
- The viewer's audiences for a record (self, holders of the group's permission within the record's department, signed-in, module-asserted) and the filter that applies them.
- Reporting which returned fields the caller must audit.
- The conformance check that every exported wire field of a module's response types carries a tag, and the test helpers that assert a field is absent or present in a body.

## Not responsible for

| Concern | Owner |
|---|---|
| Whether the viewer may run the use case at all, or see the record at all (`Allow`, `AllowIn`, `ExtentOf`; `NotFound` for an invisible record) | `authz` and each service, before `Filter` is called |
| Recording the audited read | the calling service, through its scoped `audit.Recorder`, with the `Result` |
| Storing or editing the `visibility_<group>` values; the settings screen | `settings` |
| Who the viewer is | `identity` (`PersonFrom`); the service passes the actor to `ViewerOf`, or the zero `Viewer` for an anonymous reader |
| Column-level sensitivity in the Data tables, retention, export and deletion | each spec's Data section; `people` orchestrates export and deletion |
| A person's own sharing choices (opt-in, opt-out) | the module's own data (for example `volunteers.unlisted`) or a `people` consent; a service turns them into `Relation.Also` or into not loading the field |
| Keeping personal data out of logs, spans and signals | `telemetry`, `signals`, go-conventions |
| What the browser shows for an absent field | each module's screens (render nothing; never "hidden") |
| The OpenAPI description of optional fields | each module's fragment (Later: generated marks, Q-PRIV3) |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `tags` | Parse and cache the `privacy` tag of every exported field of a type. | `parsePlan(reflect.Type) (*plan, error)`; unexported; used by `Check` and `Filter` |
| `groups` | Declare the configurable field groups and derive their settings. | `Group`, `Audience`, `Settings`, `PolicyOf` |
| `filter` | Apply the policy to one record for one viewer and report what to audit. | `Viewer`, `ViewerOf`, `Relation`, `Result`, `Filter`, `Policy.Shows` |
| `privacytest` | Assert on a response body in tests. | `AssertHidden`, `AssertPresent` |

## Interface

```go
package privacy // internal/core/privacy

// Audience names who may see a group of fields. Built-in audiences are below; a module may
// add its own through Group.Extra and assert them per record through Relation.Also.
type Audience string

const (
	AudienceNobody   Audience = "nobody"    // never returned; the feature is off
	AudienceSelf     Audience = "self"      // the person the record is about
	AudienceHolders  Audience = "holders"   // the subject, and holders of Group.Permission in the record's extent
	AudienceSignedIn Audience = "signed_in" // the subject, and any signed-in viewer who may see the record
)

// Validate fails errs.Invalid (code malformed) unless a is an ident of at most 32 bytes.
func (a Audience) Validate() error

// Group is a named set of fields a regional configures as one setting, visibility_<Name>.
type Group struct {
	Name       string           // ident; the setting is visibility_<Name>; unique within an area
	Permission authz.Permission // who counts as holders; required when Options includes holders
	Default    Audience         // must be in Options
	Options    []Audience       // what the regional may choose, in display order; at least two; built-in or Extra
	Extra      []Audience       // module-defined audiences; must not collide with the built-in names
}

// Validate fails errs.Invalid with field codes: name required/malformed; permission
// required (holders offered) or malformed; default one_of (not in Options); options
// too_few/duplicate/unknown; extra duplicate/malformed.
func (g Group) Validate() error

// Settings returns one settings.Decl per group: Key "visibility_<name>", Kind Enum,
// Options = the group's Options as strings, Default = the group's Default. A manifest
// appends them to Manifest.Settings; a core area to its Settings slice. Consumer: every
// adopter's manifest; Manifest.Validate checks the result is present. Fails errs.Invalid
// when a group fails Validate or two groups share a name.
func Settings(groups []Group) ([]settings.Decl, error)

// Check parses every view (a struct value or pointer; the module's response types) and
// fails errs.Invalid listing every problem: an exported field without a privacy tag whose
// json tag is not "-"; a level that is not public|internal|personal|sensitive; a group
// name not in groups; a personal or sensitive field with no group (code ungrouped); a
// grouped field whose json tag lacks omitempty/omitzero, whatever its kind (code
// not_omittable); an auditable (personal or sensitive) field whose JSON name is
// longer than audit.MaxFieldBytes (too_long) or does not match [a-z][a-z0-9_]*
// (malformed); more auditable fields than audit.MaxFields (too_many); a view that
// is not a struct (not_struct). Named and embedded struct
// fields, slice and map elements and pointers are walked, so a nested response type is
// checked through its container; a type implementing json.Marshaler or
// encoding.TextMarshaler is a leaf. Consumers: Manifest.Validate; moduletest views_tagged.
func Check(views []any, groups []Group) error

// Policy is the regional's current audience per group name. Consumer: a service, once
// per use case.
type Policy map[string]Audience

// PolicyOf reads visibility_<name> for each group through the area's Reader. Never cached:
// settings are a map lookup and a change applies at once. Fails with the reader's kinds.
func PolicyOf(ctx context.Context, r settings.Reader, groups []Group) (Policy, error)

// Shows reports whether a field in group would be returned to viewer for rel. False for an
// unknown group. Cannot fail. Consumer: a service deciding whether to load a field
// (volunteers' shift-mates list needs a NamesOf call it should not make for nothing).
func (p Policy) Shows(group string, viewer Viewer, rel Relation) bool

// Viewer is who is looking: the actor and, per group name, where the actor holds that
// group's Permission (keyed by Group.Name, so Filter and Shows, which know only the
// group a field names, need no permission lookup). A group with no entry is not held
// anywhere. The zero Viewer is anonymous (public fields only); a public use case passes
// it to Filter directly and never calls ViewerOf. Consumer: a service, once per use case.
type Viewer struct {
	Person  model.PersonID
	Extents map[string]authz.Extent // by Group.Name
}

// ViewerOf builds the viewer for person, the actor the service already holds from
// identity.PersonFrom(ctx) (an Authorizer answers for the actor in ctx but cannot
// say who that is, and this package may not import identity; person must be that actor,
// which ViewerOf cannot check). It calls ExtentOf once per distinct Group.Permission and
// stores the extent under every group naming it; a Permission error means not held and
// leaves those groups absent. Fails Unauthenticated when person is zero (before any
// authz call, so it holds with no groups too), Invalid (a permission not declared in
// this installation), store kinds. Never Permission.
func ViewerOf(ctx context.Context, az authz.Authorizer, person model.PersonID, groups []Group) (Viewer, error)

// Relation is what one record is to the viewer.
type Relation struct {
	Subject    model.PersonID     // the person the record is about; zero = nobody's
	Department model.DepartmentID // the department the record belongs to; zero = none
	Also       []Audience         // Extra audiences the viewer is in for this record
}

// Result says what the caller must audit for this record: the JSON names of the returned
// fields that are personal about someone other than the viewer, or sensitive, and the
// highest level among them.
type Result struct {
	Fields []string
	Level  model.Sensitivity
}

// Audited reports whether an audit entry is due (len(Fields) > 0).
func (r Result) Audited() bool

// Filter zeroes in place every field of *v the viewer may not see and returns what to
// audit. Shallow: it decides the top-level and embedded fields of *v; a nested record
// with its own subject is filtered by its own call before its container. v must be a
// non-nil pointer to a struct that Check accepts (errs.Internal otherwise: an untagged
// field or an unknown group reached at run time means the module skipped Check, and
// nothing is returned). Consumer: every service use case that returns a response type.
func Filter(v any, pol Policy, viewer Viewer, rel Relation) (Result, error)
```

```go
package privacytest // internal/core/privacy/privacytest

// AssertHidden fails t if any of names is a key anywhere in the JSON body (objects are
// walked; arrays too). AssertPresent fails t if any of names is absent at the top level.
func AssertHidden(t testing.TB, body []byte, names ...string)
func AssertPresent(t testing.TB, body []byte, names ...string)
```

**How a module uses it** (the shape `volunteers` follows; `hello` shows the minimum):

```go
// module.go
var Manifest = module.Manifest{
	Name:   "volunteers",
	Groups: vol.Groups,                                  // []privacy.Group
	Views:  []any{vol.ShiftSummary{}, vol.Roster{}, vol.RosterEntry{}, /* every response type */},
	Settings: append(ownSettings, mustSettings(vol.Groups)...), // privacy.Settings; Validate checks it
}

// service.go, inside Roster (actor is the model.PersonID the use case took from identity.PersonFrom first)
viewer, err := privacy.ViewerOf(ctx, s.authz, actor, vol.Groups)
pol, err := privacy.PolicyOf(ctx, s.settings, vol.Groups)
var entries []audit.Entry
for i := range roster.Entries {
	res, err := privacy.Filter(&roster.Entries[i], pol, viewer,
		privacy.Relation{Subject: roster.Entries[i].Person, Department: shift.Department})
	if res.Audited() {
		entries = append(entries, audit.Entry{Action: "volunteers.roster.read", Outcome: audit.Succeeded,
			Subject: roster.Entries[i].Person, Record: audit.RefOf("volunteers.shift", shift.ID),
			Fields: res.Fields, Level: res.Level})
	}
}
if len(entries) > 0 { if err := s.audit.Record(ctx, entries...); err != nil { return vol.Roster{}, err } }
```

## Types

Defined above. Uses `model.Sensitivity` (levels and `ReadIsAudited(self)` for the audit rule), `model.PersonID`, `model.DepartmentID`, `authz.Permission`, `authz.Extent`, `settings.Decl`, `settings.Reader`. Every value type has `Validate()`; `Policy`, `Viewer`, `Relation` and `Result` are plain values that cannot be invalid on their own (`Filter` validates what it needs and answers `Internal`).

## Data

None: no tables. The regional's choices are `settings` rows (`core.setting`, key `<area>.visibility_<group>`, `internal`, kept as settings keeps them). Nothing personal passes through this package except the values it zeroes in the caller's memory; it never copies, logs or returns them.

## Permissions

None declared. `holders` is defined by the permission each `Group` names, which its owner declares with a `Reaches` at least the level of the group's fields (checked for the module's own permissions by `Manifest.Validate`, module-contract Proposed change 1). Changing a `visibility_<group>` value needs `settings.manage`, as every setting does.

## Signals

None published, none subscribed: a policy change is a setting read at use-case time (settings decision 10).

## Settings

Owned pattern, declared by each adopter through `Settings(groups)`:

| Key | Type | Default | Valid range | Who can change it |
|---|---|---|---|---|
| `<area>.visibility_<group>` | enum | the group's `Default` | the group's `Options` (`one_of`) | settings admin |

Catalogue keys: `module.<name>.setting.visibility_<group>.title|description|option.<audience>` (core areas: `core.<area>.setting.…`). The shell holds `privacy.audience.nobody|self|holders|signed_in` as the fallback option titles so a module only words its `Extra` audiences (web-shell Proposed change 5). They are frontend text: entries in `web/src/shell/messages/en.json` and `es.json`, checked by the shell's catalogue test; the Go `privacy` package carries no human-readable text, like a manifest.

## UI

None of its own. Rules for every screen that shows a tagged type: an absent field is not rendered (no row, no "hidden", no placeholder); a field that is present but empty is rendered as empty. The settings screen shows `visibility_*` settings like any enum, grouped under the module.

## Behavior

1. **Order inside a use case.** Actor → input `Validate` → permission (`Allow`/`AllowIn`/`ExtentOf`) → load → `ViewerOf` and `PolicyOf` once → `Filter` per record (each with its own `Relation`) → audit entries from the `Result`s, recorded in one `Record` call (outside any `Read` transaction; a failure fails the use case, audit decision 3) → return. `Shows` may be asked before load to skip work (a `NamesOf` call, a join) the policy would discard.
2. **Why the service, not the encoder.** A central filter in `httpx.WriteJSON` or an api middleware would have to learn, per response, whose data each nested record is, which department it belongs to and which module-defined audiences apply, and would have to audit after the handler returned; a failed audit could then no longer fail the use case before anything was loaded or counted. The service already holds the subject, the department, the extent it queried with and the transaction; one call there is explicit, testable with fakes and reviewable (the reviewer checks that every use case returning a response type calls `Filter`, as it checks for the permission call). The safety nets are mechanical: `Check` at `Register` and in `moduletest` (no untagged field ships), `Filter`'s `Internal` on an unchecked type (an unfiltered path cannot leak by accident if it calls `Filter`; one that never calls it is the review's job), and the per-module privacy tests in the Test plan.
3. **The audiences of a viewer for a record** are a set, not a ladder (see Summary, "The rule"): `signed_in` ⊂ every signed-in viewer; `self` when `viewer.Person == rel.Subject` (zero never equals zero: an anonymous viewer is nobody's self); `holders` when `viewer.Extents[g.Name]` is `Site` or `Covers(rel.Department)` (a department-scoped permission never covers a zero department; a site permission is held or not); `rel.Also` as given, restricted to the group's `Extra` (an `Also` naming a built-in audience is ignored, so a module cannot promote a viewer to `holders` by mistake). A grouped field is returned when `pol[g.Name]` is in the set, or when the viewer is the subject and `pol[g.Name] != nobody`. An ungrouped `internal` field needs `viewer.Person` set; `public` needs nothing.
4. **What `Filter` zeroes.** A hidden field is set to its zero value (`nil` for a pointer, slice or map; the zero struct or scalar otherwise), which the field's `omitempty`/`omitzero` then drops from the body (`encoding/json` writes a nil pointer, slice or map as `null` unless the tag says to omit it, and `null` tells the reader the field exists; so `Check` requires the option on every grouped field, and a slice or map whose empty-but-visible value must still appear uses `omitzero`, which drops only nil). It never allocates, copies or reads the hidden value, and never logs. It walks top-level fields and the fields of embedded structs (they are promoted on the wire); it does not enter named struct fields, slices or maps: a container such as `Roster{Entries []RosterEntry}` is filtered after each entry, with the container's own relation (usually no subject).
5. **What `Result` reports.** For each returned grouped field: level `sensitive` always; level `personal` when the viewer is not the subject (`model.Sensitivity.ReadIsAudited(self)`); `internal` and `public` never. `Fields` are the JSON names in field order (deduplicated), bounded by `audit.MaxFields` (a type with more auditable fields than that fails `Check`); `Level` is the highest. A caller whose `Result.Audited()` is false records nothing for that record.
6. **Parsing and caching.** The plan for a `reflect.Type` is built once and kept in a `sync.Map`; `Check` and `Filter` share it. A plan records, per wire field: index path, JSON name, level, group, omittable. Embedded fields are flattened as `encoding/json` promotes them. `Check` fails on a type whose JSON name is duplicated through embedding only when both are grouped (the ambiguity is `encoding/json`'s to resolve; the tag check does not try).
7. **Settings read.** `PolicyOf` calls `Reader.Text` per group; a stored value outside the group's current `Options` (an upgrade removed one) comes back as the default from settings (settings decision 5) and `PolicyOf` logs nothing (it has no logger; the settings service counts `stale_values`).
8. **Concurrency and lifecycle.** Every function is safe for concurrent use; there is nothing to construct, start, stop or close. No goroutines, no I/O beyond `ExtentOf` and the settings map lookup.
9. **Read-only copy.** Everything works (reads only); the caller's audit entries spool as audit decision 4 says.
10. **Who the viewer is**. The service resolves the actor first (`identity.PersonFrom(ctx)`; it already does, before its permission check) and passes that `model.PersonID` to `ViewerOf`, which passes the same `ctx` to `ExtentOf` so authz answers for the same actor. A use case that serves anonymous readers (a public route) does not call `ViewerOf`: it passes `privacy.Viewer{}` to `Filter`, which then keeps `public` fields only, so a non-public field added to a public response type by mistake is dropped rather than shipped. A use case with an optional actor builds the viewer either way: `ViewerOf` when `PersonFrom` reports one, the zero `Viewer` otherwise. `ViewerOf` with a person and no groups makes no authz call and returns `Viewer{Person: person}`.

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `Check` | an exported field with a `json` name has no `privacy` tag | Invalid (`views[i].Field`, `required`) | tag it | `TestCheck_UntaggedField_ReturnsInvalid` |
| F2 | `Check` | a `json:"-"` field without a tag | nil (exempt) | | `TestCheck_JSONDashField_Exempt` |
| F3 | `Check` | level not one of the four names | Invalid (`one_of`) | fix | `TestCheck_BadLevel_ReturnsInvalid` |
| F4 | `Check` | `group=` names an undeclared group | Invalid (`unknown`) | declare it | `TestCheck_UnknownGroup_ReturnsInvalid` |
| F5 | `Check` | `personal` or `sensitive` field with no group | Invalid (`ungrouped`) | add a group | `TestCheck_PersonalWithoutGroup_ReturnsInvalid` |
| F6 | `Check` | a grouped field whose `json` tag lacks `omitempty`/`omitzero`: a scalar, a struct, **and a pointer, slice or map too** | Invalid (`not_omittable`) | add the option | `TestCheck_GroupedNotOmittable_ReturnsInvalid` (table: scalar, struct, pointer, slice, map; each passes with `omitempty` or `omitzero`) |
| F7 | `Check` | a view is not a struct or pointer to one | Invalid (`not_struct`) | | `TestCheck_NotStruct_ReturnsInvalid` |
| F8 | `Check` | a nested named struct, slice element or map value has an untagged field | Invalid naming the path (`views[i].Entries.LegalName`) | | `TestCheck_WalksNestedTypes` |
| F9 | `Check` | a type implementing `json.Marshaler`/`TextMarshaler` (`time.Time`, `model.ID`) | treated as a leaf; nil | | `TestCheck_MarshalerIsLeaf` |
| F10 | `Check` | more auditable fields than `audit.MaxFields` | Invalid (`too_many {max}`) | split the type | `TestCheck_TooManyAuditable_ReturnsInvalid` |
| F10a | `Check` | an auditable (`personal` or `sensitive`) field whose JSON name is longer than `audit.MaxFieldBytes` | Invalid (`views[i].<Field>` `too_long {max}`) | shorten the JSON name | `TestCheck_AuditableNameTooLong_ReturnsInvalid` |
| F10b | `Check` | an auditable field whose JSON name does not match `[a-z][a-z0-9_]*` (`legalName`, `legal-name`) | Invalid (`views[i].<Field>` `malformed`) | rename it (snake case) | `TestCheck_AuditableNameNotAuditGrammar_ReturnsInvalid` |
| F11 | `Group.Validate` | `holders` offered without a `Permission`; malformed permission | Invalid (`permission` `required`/`malformed`) | | `TestGroupValidate_HoldersWithoutPermission_ReturnsInvalid` |
| F12 | `Group.Validate` | `Default` not in `Options`; fewer than two options; duplicate; unknown audience (neither built-in nor `Extra`) | Invalid (`default` `one_of`; `options` `too_few`/`duplicate`/`unknown`) | | `TestGroupValidate_Options_Table` |
| F13 | `Group.Validate` | `Extra` names a built-in audience | Invalid (`extra` `duplicate`) | rename | `TestGroupValidate_ExtraCollidesBuiltin_ReturnsInvalid` |
| F14 | `Settings` | two groups share a name | Invalid (`groups[j].name` `duplicate`) | | `TestSettings_DuplicateGroup_ReturnsInvalid` |
| F15 | `Settings` | valid groups | one `Enum` decl per group, key `visibility_<name>`, passing `settings.Decl.Validate` | | `TestSettings_ProducesEnumDecls` |
| F16 | `Filter` | `v` is nil, not a pointer, or not to a struct | Internal | fix the call | `TestFilter_NotPointerToStruct_ReturnsInternal` |
| F17 | `Filter` | the type fails `Check` (untagged field, unknown group) | Internal; `*v` untouched; nothing returned by the caller | fail the use case | `TestFilter_UncheckedType_ReturnsInternal` |
| F18 | `Filter` | `pol` lacks a group the type uses | Internal | fail the use case | `TestFilter_PolicyMissingGroup_ReturnsInternal` |
| F19 | `Filter` | policy `nobody` | field zeroed for everyone, the subject included | | `TestFilter_Nobody_HidesFromSubject` |
| F20 | `Filter` | policy `self`, viewer is the subject / is not | kept / zeroed | | `TestFilter_Self_Table` |
| F21 | `Filter` | policy `holders`: permission held site-wide; held at the record's department; held at another department; not held; department-scoped with a zero `Relation.Department` | kept; kept; zeroed; zeroed; zeroed | | `TestFilter_Holders_Table` |
| F22 | `Filter` | policy `signed_in`: signed-in viewer / anonymous | kept / zeroed | | `TestFilter_SignedIn_Table` |
| F23 | `Filter` | policy is an `Extra` audience: present in `Also` / absent / `Also` names a built-in | kept / zeroed / ignored | | `TestFilter_Extra_Table` |
| F24 | `Filter` | the subject views a `personal` grouped field under any policy but `nobody` | kept; `Result.Fields` empty | | `TestFilter_SubjectSeesOwn_NotAudited` |
| F25 | `Filter` | a holder views a `personal` field about someone else | kept; `Result.Fields` has its JSON name, `Level personal` | audit | `TestFilter_OtherPersonal_Audited` |
| F26 | `Filter` | a `sensitive` field returned to the subject | `Result.Fields` has it, `Level sensitive` | audit | `TestFilter_SensitiveSelf_Audited` |
| F27 | `Filter` | ungrouped `internal` field, anonymous viewer | zeroed | | `TestFilter_Anonymous_PublicOnly` |
| F28 | `Filter` | embedded struct with grouped fields | its fields decided like top-level ones | | `TestFilter_EmbeddedWalked` |
| F29 | `Filter` | a named struct field or slice holding grouped fields | untouched (the caller filters elements first) | | `TestFilter_NestedNotEntered` |
| F30 | `Filter` | hidden value | never read, logged or copied (`telemetrytest` capture clean; the zeroed memory has no marker) | | `TestFilter_HiddenValueNeverLogged` |
| F31 | `ViewerOf` | `person` is zero, with or without groups | Unauthenticated; no authz call | 401 | `TestViewerOf_NoActor_ReturnsUnauthenticated` (two cases: groups and none; the `authztest` fake records no call) |
| F32 | `ViewerOf` | `ExtentOf` answers `Permission` for a group permission | not an error; every group naming that permission is absent from `Extents` | | `TestViewerOf_NotHeld_IsAbsent` |
| F33 | `ViewerOf` | `ExtentOf` answers `Invalid` (undeclared) or a store kind | that kind | fail the use case | `TestViewerOf_AuthzError_Propagates` |
| F34 | `ViewerOf` | two groups share a permission | one `ExtentOf` call; both group names carry the same extent | | `TestViewerOf_DistinctPermissionsOnce` |
| F39 | `ViewerOf` | a person and no groups | `Viewer{Person: person}`, nil `Extents`, no authz call | | `TestViewerOf_NoGroups_NoAuthzCall` |
| F40 | `Filter` | the zero `Viewer` against a type with `public`, `internal` and grouped fields under every policy | `public` kept; everything else zeroed; empty `Result` | serve public routes with it | `TestFilter_ZeroViewer_PublicOnly_AllPolicies` |
| F35 | `PolicyOf` | reader fails | its kind | fail the use case | `TestPolicyOf_ReaderError_Propagates` |
| F36 | `PolicyOf` | a stored value outside `Options` | the group's default (settings decision 5) | | `TestPolicyOf_StaleValue_UsesDefault` (through `settingstest`) |
| F37 | `Shows` | unknown group | false | | `TestShows_UnknownGroup_False` |
| F38 | `privacytest.AssertHidden` | name present at any depth | fails `t` | | `TestAssertHidden_FindsNested` |

`Audience.Validate`, `Result.Audited` and `Policy.Shows` cannot fail: they compute on values already in memory. `Filter` has no I/O.

## Lock-in check

No external technology is wrapped: reflection and `encoding/json` from the standard library. The tags are plain Go struct tags; a fork that drops this package keeps compiling.

## Test plan

- **Unit:** every row above, table-driven over policy × viewer × relation; the plan cache (`TestPlan_ParsedOnce`, `-race`); `Check` on the `hello` and `volunteers` view types as fixtures of a well-tagged module; a fixture type per `Check` failure.
- **Privacy:** `TestFilter_HiddenValueNeverLogged` with `telemetrytest.New(t)` and a marker value; a fuzz test that `Filter` never panics on arbitrary tagged structs (`FuzzFilter`).
- **Conformance (module-contract Proposed change 1):** `moduletest` rule `views_tagged` runs `Check(manifest.Views, manifest.Groups)` and fails when `Views` is empty while the module has routes (a module with routes but no response types says so with `WithoutViews()`); rule `visibility_settings_declared` checks `Settings(Groups)` ⊆ `Manifest.Settings`. Both run right after `manifest_valid` (second and third): a broken manifest is reported before anything is built, and the rules need no fixture. `moduletest`'s own fixture modules in `conformance_test.go` have routes (`http.NotFoundHandler`) and no response type, so their `Conformance` calls pass `WithoutViews()`; one fixture (`untaggedView`) lists an untagged type for F65.
- **Per adopter (in each module's own tests, listed in its spec):** for each response type with a grouped field, a test that a viewer in no audience gets a body without the field (`privacytest.AssertHidden`) and a viewer in the audience gets it (`AssertPresent`) plus the audit entry; the volunteers rows are F126 to F128 and F134 to F141 there.
- **Integration:** none of its own (no I/O); covered by the adopters' integration tests.

## Later

Named so the boundary is explicit: a recursive `Filter` with a subject resolver, if containers with per-element subjects become common; OpenAPI marks (`x-bms-privacy: {level, group}`) generated from the tags and checked by `api` against the fragment (Q-PRIV3); a browser-side `privacy` map so screens can show "set by your regional" help text for absent fields; `people` consents as a built-in audience (`consented:<purpose>`), once a module needs an opt-in group; a per-person audience ("the people I lead") when a membership contract exists (volunteers Proposed change 1); `bms check` listing every group and its current audience for an operator.

## Amendment A1 (raised 2026-10-20, approved 2026-10-21, D45): two interface contradictions and six smaller points

Raised by the implementer against the Approved text. Each item names the choice, the reason and the alternative not taken. The inline edits above are folded into the text; `module-contract.md`, `module-hello.md` and `module-volunteers.md` were edited alongside so the three stay consistent with this one.

**A1.1 `Filter` and `Shows` could not find the viewer's extent.** `Policy` is keyed by group name, the tag carries only `group=<name>`, and `Viewer.Extents` was keyed by permission; nothing in `Filter`'s or `Shows`' arguments mapped a group to its permission. *Chosen:* `Viewer.Extents map[string]authz.Extent`, keyed by `Group.Name`; `ViewerOf` still calls `ExtentOf` once per distinct permission (F34) and writes the extent under every group naming it. *Why:* the viewer's extents are consumed per group, so keying them per group puts the join where the data is produced (once per use case) instead of where it is consumed (once per field per record); `Filter`'s and `Shows`' signatures stay as approved, the `Policy` literal tests write stays a plain map, and a hand-built `Viewer` in a test reads as "holder of group X here". *Not taken:* (B) passing `groups` to `Filter` and `Shows` widens every call site and repeats a lookup per field; (C) a `Policy` struct carrying the permission per group makes `Policy` opaque (no literal in tests) and leaves `Viewer` saying something (`Extents` by permission) that no consumer asks.

**A1.2 `ViewerOf` could not learn `Viewer.Person`.** `authz.Authorizer` answers for the actor in `ctx` but exposes only `Allow`, `AllowIn` and `ExtentOf`; `privacy` may not import `identity`; and with no groups there was no `ExtentOf` call to produce F31's `Unauthenticated`. *Chosen:* `ViewerOf(ctx, az, person model.PersonID, groups)`; a zero `person` is `Unauthenticated` before any authz call. *Why:* every use case already resolves the actor before its permission check (Behavior 1), so passing it costs nothing and keeps `privacy` free of `identity`; failing on a zero person keeps F31 and makes the anonymous path explicit: a public use case passes `privacy.Viewer{}` to `Filter` (Behavior 10, F40). *Not taken:* an `actorFunc` seam (a second way to say the same thing; the service has the ID in hand); returning the zero `Viewer` for a zero person (a service that forgot its actor check would silently serve public fields instead of failing).

**A1.3 `hello` has four wire types, not one.** Only `Greet` returns `greeting.Greeting`; `Greeting` returns `hello.Greeting{Greeting, Mine, Seen}`, and `PublicGreeting{Greeting, Banners}` and `Banner{Module, Key}` are wire types too. *Chosen:* all four tagged and listed in `Views`: `PublicGreeting` and `Banner` fields `public` (the route has no actor), `hello.Greeting` and `greeting.Greeting` fields `internal`; `Filter` in all three use cases: `Greeting` and `Greet` with `ViewerOf`, `PublicGreeting` with the zero `Viewer` over the container and over each `Banner`. *Why:* `Check` guarantees that a type is tagged, not that a public route's type is all `public`; only `Filter` with the zero `Viewer` enforces "public fields only" for an anonymous reader, and the template should show that shape as well as the signed-in one. The per-`Banner` call shows the container-then-elements shape (Behavior 4) without a second module. *Not taken:* `Filter` only in `Greeting` and `Greet` (the implementer's proposal): it leaves the reviewer's rule "every use case that returns a response type calls `Filter`" with an exception in the template itself.

**A1.4 `null` is a marker.** A zeroed `nil` pointer, slice or map without `omitempty`/`omitzero` encodes as `"x": null`, which tells the reader the field exists (decision 3 forbids a marker). *Chosen:* `Check` requires `omitempty` or `omitzero` on every grouped field, whatever its kind (F6). `omitzero` is the choice for a slice or map whose empty-but-visible value must still appear (`[]`), since it drops only the nil value; `omitempty` is fine for a pointer. *Not taken:* having `Filter` detect the kind and rely on the encoder: the encoder's behavior is what produces the marker.

**A1.5 Conformance rule order.** `views_tagged` and `visibility_settings_declared` run right after `manifest_valid`, before `templates_register`: both read the manifest only, and a tagging problem should be named before the rig builds the module. *Why not last:* the rules after `manifest_valid` assume a well-formed manifest; a `Views` mistake would otherwise surface as a confusing failure in a later rule.

**A1.6 `moduletest`'s own fixtures.** The fixture modules in `conformance_test.go` have routes (`http.NotFoundHandler`) and no response type; after A1.5 their `Conformance` calls fail `views_tagged` unless they pass `WithoutViews()`. They do; one new fixture (`untaggedView`: `Views` listing a struct with an untagged exported field) proves F65.

**A1.7 Where the `privacy.audience.*` keys live.** They are frontend catalogue entries in `web/src/shell/messages/<lang>.json` (the shell's catalogues, checked by the shell's catalogue test), not anything in Go: the `privacy` package, like a manifest, carries no human-readable text.

**A1.8 `Check` and audit's field grammar.** `Result.Fields` go straight into `audit.Entry.Fields`, which `audit.Entry.Validate` rejects when a name is longer than `audit.MaxFieldBytes` (64) or outside `[a-z][a-z0-9_]*`; that would fail a use case at run time, on the first audited read. *Chosen:* `Check` rejects an auditable field whose JSON name breaks either rule (F10a `too_long`, F10b `malformed`); `privacy` imports `core/audit` for the two constants (core contracts may import each other acyclically; `audit` does not import `privacy`). The grammar is restated in `privacy` rather than exported from `audit`, which would be a change to an Implemented spec for one regexp; `TestCheck_AuditableNameNotAuditGrammar_ReturnsInvalid` also feeds the rejected names to `audit.Entry.Validate` so the two cannot drift apart unnoticed.

**Test expectations that change** (for the test-author writing from the Approved text; everything else stands):

| Test | Before | After A1 |
|---|---|---|
| Any helper building a `Viewer` (`holderSite`, `holderIn` in `helpers_test.go`) | `Extents[authz.Permission]` | `Extents[string]` keyed by group name; the helpers take group names |
| `TestFilter_Holders_Table` (F21), `TestFilter_Extra_Table` (F23), any `Shows` test | the held permission is looked up through the group | the viewer's entry is under the group name; two groups sharing a permission each need their own entry |
| `TestViewerOf_NoActor_ReturnsUnauthenticated` (F31) | an empty ctx | `person` zero, two cases (with and without groups); the `authztest` fake records no `ExtentOf` call |
| `TestViewerOf_NotHeld_IsAbsent` (F32) | the permission is absent from `Extents` | every group naming that permission is absent |
| `TestViewerOf_DistinctPermissionsOnce` (F34) | one call | one call, and both group names carry the same extent |
| every `ViewerOf` call | `ViewerOf(ctx, az, groups)` | `ViewerOf(ctx, az, person, groups)` with `ctx` from `authztest.WithActor(ctx, authz.Actor{Person: person})` |
| `TestCheck_GroupedNotOmittable_ReturnsInvalid` (F6) | a pointer, slice or map without `omitempty` passes | it fails `not_omittable`; the table adds pointer, slice and map rows, each passing with `omitempty` or `omitzero` |
| `TestCheck_WellTaggedType_Passes` and every fixture type with a grouped pointer, slice or map field | no `omitempty` needed | the fixture's grouped fields carry `omitempty` or `omitzero` |
| New: `TestCheck_AuditableNameTooLong_ReturnsInvalid` (F10a), `TestCheck_AuditableNameNotAuditGrammar_ReturnsInvalid` (F10b), `TestViewerOf_NoGroups_NoAuthzCall` (F39), `TestFilter_ZeroViewer_PublicOnly_AllPolicies` (F40) | — | as the rows say |
| `moduletest` conformance tests (`conformance_test.go`) | fixtures without `WithoutViews()` | every fixture passes `WithoutViews()` except `untaggedView`; the rule order has `views_tagged`, `visibility_settings_declared` second and third |
| `hello`: `TestManifest_UntaggedViewField_ReturnsInvalid` (F37 there), `TestViews_Tagged` | `Views = [greeting.Greeting{}]` | `Views` lists `PublicGreeting{}`, `Banner{}`, `Greeting{}` (hello's), `greeting.Greeting{}`; new `TestPublicGreeting_ZeroViewer_PublicFieldsOnly` (hello F38) |
| `volunteers` | `ViewerOf(ctx, authz, Groups)` | `ViewerOf(ctx, authz, actor, Groups)`; F126 to F141 unchanged otherwise |

## Open questions

- **Q-PRIV1. Is the built-in audience vocabulary right: `nobody`, `self`, `holders`, `signed_in`, plus module-defined extras?** Alternatives: a numeric ladder (simpler settings screen, but `booked` and `holders` are not ordered), or role names in the setting (what the owner's "volunteer coordinator and maybe some other roles" sounds like, but roles are a regional's data and a starter role may be deleted; `holders` of a permission is what a role is made of and survives renames). *Recommendation:* the four plus extras; a regional that wants "these roles" builds a role holding the group's permission. **Settled: the built-in vocabulary `nobody`, `self`, `holders`, `signed_in` plus module-defined extras; a regional that wants "these roles" builds a role holding the group's permission (owner, 2026-10-20, D44).**
- **Q-PRIV2. Does `people` adopt the tags now or later?** Its hand-rolled version (`Profile.Fields`, `personalFields`, `auditPersonalRead`) behaves exactly like groups `legal_name`, `phone`, `email`, `emergency_contacts` with `Permission people.contacts.read`, default `holders`. Adopting now adds four `people.visibility_*` settings (so a regional could, for example, let every signed-in person see phone numbers, which no one has asked for) and a rewrite of a tested package. *Recommendation:* later, when a regional asks for a people-level visibility choice; the volunteers module is the first adopter, hello the template (Proposed change 4). **Settled: later; `people` keeps its hand-rolled version until a regional asks for a people-level visibility choice; the volunteers module is the first adopter and `hello` the template (Proposed change 4) (owner, 2026-10-20, D44).**
- **Q-PRIV3. Should the OpenAPI fragments mark tagged fields?** A reviewer can check today that grouped fields are not `required` in a fragment. *Recommendation:* Later; generate marks from the tags when `api` grows a fragment generator, not by hand now. **Settled: Later; marks are generated from the tags when `api` grows a fragment generator (owner, 2026-10-20, D44).**
- **Q-PRIV4. The `volunteer-coordinator` starter role (D43, "real names visible only to the volunteer coordinator"): a site starter role seeded with `people.directory.read`, `people.contacts.read`, `volunteers.rosters.read` and `volunteers.qualifications.read`, created by an authz migration.** Alternatives: no new role (a regional builds one; but then a fresh installation has no role that sees legal names except `account-admin`, which is not what the owner described), or seeding `people.contacts.read` into `department-admin` (people decision 9 says to wait for a membership-limited extent). *Recommendation:* add the role (authz Proposed change 2), with those four permissions and nothing that manages shifts; `people.contacts.read` is a people decision, so it is proposed there (Proposed change 3) and not assumed. **Settled: the role is added (authz Proposed change 2, applied: `people.directory.read`, `people.contacts.read`, `volunteers.rosters.read`, `volunteers.qualifications.read`, nothing that manages shifts); `people.contacts.read` is proposed to `people` (Proposed change 3, applied) (owner, 2026-10-20, D44).**

## Proposed changes elsewhere

1. (applied 2026-10-20, D44) **`plan/design/module-contract.md` (row 4) and `internal/core/module`:** `Manifest.Views []any` and `Manifest.Groups []privacy.Group`; `Manifest.Validate` runs `privacy.Check(Views, Groups)` (problems under `views[i]`, `groups[j]`), requires `privacy.Settings(Groups)` to be present in `Settings` with equal options and default (`settings[k]` `missing`/`mismatch`), and checks that for each group offering `holders` whose `Permission` this module declares, that permission's `Reaches` is at least the highest level among the group's fields (`permissions[i].reaches` `too_low`); `moduletest.Conformance` gains rules `views_tagged` and `visibility_settings_declared` and the option `WithoutViews()`; the `add-module` skill's layout gains "`Views` and `Groups` in the manifest; `privacy.Filter` in every use case that returns a response type; `visibility_<group>` catalogue keys in `en.json`".
2. (applied 2026-10-20, D44) **`plan/design/authz.md` (row 7):** a ninth starter role `volunteer-coordinator` (`RoleVolunteerCoordinator`, site scope), created by migration `0003`; `Seed`'s "a key with no role is skipped" is confirmed to retry at the next `bms migrate` so a module may list the key before the role exists; `SiteAdminRoles` unchanged (the coordinator is not an admin and does not get `core.modules.read`). Depends on Q-PRIV4.
3. (applied 2026-10-20, D44) **`plan/design/people.md` (row 8):** (a) `Directory` gains `LegalNamesOf(ctx, []model.PersonID) (map[model.PersonID]string, error)`: requires `people.contacts.read` in any extent for the actor (decision 9; `Permission` otherwise), at most `MaxNames` IDs, omits people with no legal name, audits one `people.person.read` per returned person with `Fields [legal_name]`, `Level personal` (decision 15), never on a route; `peopletest.Fake` gains `AddLegalName(id, name)`; (b) `people.contacts.read` and `people.directory.read` list `volunteer-coordinator` in `StarterRoles` (with Proposed change 2); (c) a note under decision 7 that per-field visibility choices, when they come, are `privacy` groups (Q-PRIV2).
4. (applied 2026-10-20, D44) **`plan/design/module-hello.md` (row 4b):** the reference module tags `greeting.Greeting` (`id`, `greeting`, `created_at` all `internal`; `Person` is `json:"-"`), declares `Views: []any{greeting.Greeting{}}`, no groups, and calls `privacy.Filter` in `List` and `Create` so module authors copy the shape.
5. (applied 2026-10-20, D44) **`plan/design/web-shell.md` (row 17):** catalogue keys `privacy.audience.nobody|self|holders|signed_in` (title and one-line description each) as the fallback option titles for every `visibility_*` setting; the rule "an absent field renders nothing".
6. (applied 2026-10-20, D44) **`plan/07-abstractions.md`, "Core":** a new entry after `settings`: "**privacy: who sees which field** — Purpose: decide, field by field, which of a record's fields a viewer gets back. Owns: the `privacy` struct tag, field groups and their `visibility_<group>` settings, the viewer's audiences, `Filter` and what it reports for audit, the `views_tagged` conformance rule. Doesn't do: action permissions and record visibility (`authz`, each service), recording the audit entry (the caller), column sensitivity, retention, export and deletion (each spec, `people`). Spec: `design/privacy-tags.md`." The layer diagram's core row gains `privacy`.
7. (applied 2026-10-20, D44) **`plan/05-roadmap.md`, Phase 1:** "privacy tags (`privacy-tags.md`) built before the volunteers module's services, in the same milestone" and the identity, notify and people adoptions listed as Later items.
8. (applied 2026-10-20, D44) **`.claude/skills/privacy-data/SKILL.md`:** under Rules, after "Access goes through the authorizer": "**Fields carry a `privacy` tag** (`privacy-tags.md`): every exported field of a response type names its level and, for `personal`, `sensitive` or configurable fields, a group; the regional sets each group's audience in `visibility_<group>`; the service calls `privacy.Filter` and audits what it reports." The go-conventions skill's module layout line gains the same sentence.
9. (not mine: plan-keeper) **`plan/06-open-questions.md`:** add Q-PRIV1 to Q-PRIV4 as open, pointing here.
