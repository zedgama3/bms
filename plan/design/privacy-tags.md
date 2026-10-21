# privacy-tags

- **Status:** Approved (2026-10-20, D44)
- **Amendment A1 (approved 2026-10-21, D45):** `Viewer.Extents` is keyed by group name, not by permission; `ViewerOf(ctx, az, person model.PersonID, groups)` takes the actor because `authz.Authorizer` cannot tell it who the actor is and `privacy` may not import `identity` (a zero `person` is `Unauthenticated`); every grouped field needs `omitempty`/`omitzero` whatever its kind (a zeroed pointer without it serializes `null`, which is a marker); `Check` also rejects an auditable field whose JSON name does not fit `audit.Entry.Fields`; the conformance rules `views_tagged` and `visibility_settings_declared` run right after `manifest_valid`; the `privacy.audience.*` keys live in the shell's catalogues under `web/`; `hello` tags and lists all four of its wire types. Folded into the text below; "Amendment A1" before Open questions is kept as the record, with the test expectations that changed.
- **Amendment A2 (approved 2026-10-21, D46):** `encoding/json`'s `omitempty` never omits a struct (`time.Time` included) and omits an array only when its length is zero, so a hidden grouped struct came back as a zero placeholder; `Check` now requires `omitzero` on Struct and Array kinds and `omitempty` or `omitzero` on the rest, and requires the option on **every non-public field**, grouped or not (an ungrouped `internal` field zeroed for an anonymous reader was a placeholder too). `GroupLevels(views)` joins the interface (what `Manifest.Validate`'s `Reaches` check needs). `Check` rejects a view that marshals itself (`marshaler`), a field of interface kind (`dynamic`) and a non-public named unexported embedded field (`unsettable`); the plan follows `encoding/json`'s field rules exactly (unexported embedded structs and pointers to structs are walked). `Filter` fails closed (`Internal`) on any plan problem, `not_omittable` included, and on a hidden field it cannot set; it writes through an embedded pointer, so responses never share pointees. `PolicyOf` with a nil reader is `Internal`. Folded into the text below; "Amendment A2" before Open questions is kept as the record, with the test expectations that change.
- **Package:** `internal/core/privacy` (+ `privacytest`)   **UI:** none
- **Depends on specs:** `model` (`Sensitivity`, `PersonID`, `DepartmentID`), `lib-errs`, `lib-core` (`validate`), `authz` (`Authorizer.ExtentOf`, `Extent`, `Permission`, `PermissionDecl.Reaches`), `settings` (`Reader`, `Decl`, `Enum`), `audit` (the `Entry.Fields`/`Level` shape a filter result feeds), `module-contract` (`Manifest`, `moduletest`)   **Used by:** every module with response types (`volunteers` first, `hello` as the template), `module` (`Manifest.Validate`, `moduletest`), later `people`, `identity`, `notify` (Proposed changes)

## Summary

**Purpose.** Decide, field by field, which of a record's fields a viewer gets back, from a `privacy` struct tag on each response field and the regional's choice of audience per named field group. Package `internal/core/privacy`; imports `model`, `lib`, `core/authz` (the `Authorizer` and leaf types), `core/settings` (the `Reader` and leaf types), `core/audit` (the constants `MaxFields`, `MaxFieldBytes` only). Never `core/identity`, `core/module`, `core/people`, `api`, `app`, `config` or a module. No tables, routes, signals or screens. `core/module` imports it (`Manifest.Views`, `Manifest.Groups`).

**The tag.** `privacy:"<level>[,group=<name>]"` on every exported field of a response type that reaches the wire (`json:"-"` fields are exempt). `<level>` is a `model.Sensitivity` name. A `personal` or `sensitive` field must name a group; a `public` or `internal` field may. Every non-public field (grouped or not; `internal` is hidden from an anonymous reader) carries the omit option its kind needs, so a hidden field is absent, never `null`, `""`, `0` or a zero object: `omitzero` for a Struct or Array kind (`time.Time`, `model.ID` and every other struct- or array-kind marshaler included: `omitempty` never omits a struct and omits an array only when its length is zero), `omitempty` or `omitzero` for every other kind (a zeroed pointer, slice or map without one serializes as `"x": null`, which is a marker). An ungrouped `public` field needs none: it is never hidden (a grouped `public` field can be hidden, so it carries the option). A view marshals through its fields, not itself (no `json.Marshaler`/`TextMarshaler` on the view, directly or promoted from an embedded `time.Time`), and no wire field is of interface kind. An auditable field (`personal` or `sensitive`) has a JSON name that fits `audit.Entry.Fields`: `[a-z][a-z0-9_]*`, at most `audit.MaxFieldBytes`.

**Exported (`privacy`).**
- `Audience` (ident; built-in `AudienceNobody = "nobody"`, `AudienceSelf = "self"`, `AudienceHolders = "holders"`, `AudienceSignedIn = "signed_in"`; `Validate`).
- `Group{Name string, Permission authz.Permission, Default Audience, Options []Audience, Extra []Audience}` (+`Validate`): a named set of fields the regional configures as one setting `visibility_<name>` (kind `Enum`, options = `Options`, default `Default`). `Permission` is who counts as `holders` (held site-wide, or at the record's department when department-scoped). `Extra` names module-defined audiences (for example `booked`) the service asserts per record.
- `Settings(groups []Group) ([]settings.Decl, error)`: the `visibility_<name>` declarations a manifest or core area appends to its settings.
- `Check(views []any, groups []Group) error`: the conformance check over response types (every exported wire field tagged, levels valid, groups known, personal/sensitive grouped, non-public fields omittable for their kind, auditable names within audit's grammar and length, no self-marshaling view, no interface-kind field, no non-public named unexported embedded field, nested structs walked as `encoding/json` walks them).
- `GroupLevels(views []any) map[string]model.Sensitivity`: the highest level among the fields each group names, across the views and the nested types they reach; cannot fail (what it cannot parse, `Check` reports). Consumer: `Manifest.Validate`'s `Reaches` cross-check (module-contract F64).
- `Policy` (`map[string]Audience`, one entry per group); `PolicyOf(ctx, settings.Reader, []Group) (Policy, error)` reads the current audiences (never cached; settings decision 10).
- `Viewer{Person model.PersonID, Extents map[string]authz.Extent}` (keyed by **group name**: where the viewer holds that group's `Permission`); `ViewerOf(ctx, authz.Authorizer, person model.PersonID, []Group) (Viewer, error)` (the service passes the actor it already has; a zero `person` is `Unauthenticated` without calling authz): one `ExtentOf` per distinct group permission, its extent copied to every group naming it; a `Permission` error means "not held" (no entry). The zero `Viewer` is anonymous; a public use case passes `privacy.Viewer{}` to `Filter` and never calls `ViewerOf`.
- `Relation{Subject model.PersonID, Department model.DepartmentID, Also []Audience}`: what one record is to the viewer.
- `Result{Fields []string, Level model.Sensitivity}`: the JSON names of the returned fields that must be audited (level `personal` about someone else, or `sensitive`), and the highest level among them; `Result.Audited()` reports whether an entry is due.
- `Filter(v any, pol Policy, viewer Viewer, rel Relation) (Result, error)`: zeroes, in place, every field of `*v` the viewer may not see; shallow (top-level and embedded fields, exported or not, through an embedded pointer when non-nil); the service calls it once per record that has its own subject. Fails closed (`Internal`, `*v` untouched) on any problem `Check` would report for the type.
- `(Policy).Shows(group string, viewer Viewer, rel Relation) bool`: the predicate `Filter` applies, for a service to ask before loading an expensive field.
- `privacytest.AssertHidden(t, body []byte, jsonNames ...string)`, `privacytest.AssertPresent(t, body, names...)`.

**The rule** (`Shows`). The viewer's audiences for a record are `{signed_in}` when `viewer.Person` is set, plus `self` when it equals `rel.Subject`, plus `holders` when `Extents[group.Name]` is `Site` or covers `rel.Department`, plus `rel.Also`. A grouped field is returned when the policy's audience for its group is in that set, or when the viewer is the subject and the audience is not `nobody`; `nobody` never. An ungrouped field: `public` always, `internal` for a signed-in viewer. No superuser special case: the superuser holds every permission, so is a `holders` everywhere, and is not `self` anywhere.

**Key decisions.** 1 Enforcement is in the service, not in `httpx.WriteJSON` (decision 2 under Behavior): the service knows the subject, the department and the per-record audiences; the encoder knows none, and the audit entry must be recorded before the response is written. 2 The tag is the single declaration (level and group); who may see a group is data (`visibility_<group>` settings) so a regional changes it without code, and the audience vocabulary is extensible per module (`Extra`) so "booked on this shift" is expressible without a core change. 3 A hidden field is absent, never a placeholder, and the response says nothing about why (privacy-data: a `403` or a marker would confirm what exists). 4 Audit stays the caller's: `Filter` reports what must be audited; the service records `<module>.<record>.read` after loading and before returning (go-conventions). 5 Action permissions stay authz's: tags never decide whether a use case runs or a record is visible; they shape the fields of a record the service has already decided to return. 6 `Reaches` stays declared on permissions (authz uses it for separation-of-duties rules); `Manifest.Validate` cross-checks it against the groups a module's own permission guards. 7 Types are parsed once and cached (`sync.Map` by `reflect.Type`); `Filter` is a loop over a parsed plan, no allocation per field. 8 First version: no nested recursion in `Filter`, no OpenAPI generation, no browser-side counterpart, no per-person consent integration (Later). 9 Absence is enforced at build time for every non-public field, not at run time: `Check` demands the omit option the kind needs, so `Filter` never has to choose between a placeholder and a failure; the cost, an `internal` scalar's zero is absent on the wire, is unambiguous (a signed-in reader is never denied an `internal` field, so absent means zero) and is what proto3's JSON mapping does.

**Errs kinds callers handle.** `Check`, `Group.Validate`, `Settings`: `Invalid` (field paths `views[i].<Field>`, `groups[j].<field>`; codes `required`, `malformed`, `one_of`, `unknown`, `duplicate`, `not_omittable`, `ungrouped`, `not_struct`, `too_many`, `too_long`, and `marshaler`, `dynamic`, `unsettable`). `Filter`: `Internal` (not a non-nil pointer to a struct; any problem `Check` would report for the type, `not_omittable` included; a group missing from `pol`; a hidden field that cannot be set): fail closed, `*v` untouched, nothing returned. `GroupLevels`: cannot fail. `ViewerOf`: `Unauthenticated` (zero `person`), `Invalid` (an undeclared permission), store kinds; never `Permission`. `PolicyOf`: `Internal` for a nil reader, else the reader's kinds (`Invalid`, `Unavailable`, `Timeout`, `Canceled`). `Shows`: cannot fail (unknown group is `false`).

**Owns.** The tag grammar; the setting key pattern `visibility_<group>` in every area that declares groups (titles `module.<name>.setting.visibility_<group>.title|description|option.<audience>`, core areas `core.<area>.setting.…`); `Manifest.Views []any` and `Manifest.Groups []Group` (module-contract Proposed change 1); the `moduletest` rules `views_tagged` and `visibility_settings_declared` (run right after `manifest_valid`); the catalogue keys for the built-in audiences `privacy.audience.<name>`, which are frontend text in the shell's catalogues `web/src/shell/messages/<lang>.json` (web-shell Proposed change 5); no Go package holds them.

**Expects of dependencies.** `authz`: `ExtentOf` answering `Permission` when not held, `Extent.Covers`, `PermissionDecl.Reaches`. `settings`: a scoped `Reader.Text` for `Enum`, decision 5 (an unparseable stored value yields the default). `module`: `Manifest.Validate` calls `Check` and the `Reaches` and settings cross-checks; `moduletest.Conformance` runs `views_tagged` and `visibility_settings_declared` second and third, and its own fixture modules (routes, no response type) pass `WithoutViews()`. `audit`: `Entry.Fields` names match `[a-z][a-z0-9_]*`, at most `MaxFieldBytes` each and `MaxFields` per entry; `Check` enforces all three at build time. `people` (Proposed change 3): `Directory.LegalNamesOf` for the first `personal` group a module shows.

**Open questions:** none: Q-PRIV1 to Q-PRIV4 were settled with the recommendations (owner, 2026-10-20, D44): the four built-in audiences plus module extras; `people` adopts the tags later; OpenAPI marking Later; the `volunteer-coordinator` starter role is added. **Proposed changes elsewhere:** 9, listed at the end; 1 to 8 applied 2026-10-20 (D44); 9 (`06-open-questions.md`) is the plan-keeper's. **Amendment A1** (approved 2026-10-21, D45) is the section before Open questions; it is folded into the text, and its matching edits to `module-contract.md`, `module-hello.md` and `module-volunteers.md` are in those specs. **Amendment A2** (approved 2026-10-21, D46) follows A1 and is folded into the text: the omit rule per kind on every non-public field, `GroupLevels`, three new `Check` rejections, `Filter` failing closed on every plan problem, `PolicyOf` nil reader; its edits to `module-hello.md` were made alongside and the rest are Proposed changes 10 to 13 (applied, D46).

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
// non-public field (grouped or not) whose json tag lacks the omit option its kind
// needs: omitzero for a Struct or Array kind, omitempty or omitzero for every other
// kind (code not_omittable; the reason names the option wanted); an auditable
// (personal or sensitive) field whose JSON name is longer than audit.MaxFieldBytes
// (too_long) or does not match [a-z][a-z0-9_]* (malformed); more auditable fields than
// audit.MaxFields (too_many); a view that is not a struct (not_struct); a view whose
// type implements json.Marshaler or encoding.TextMarshaler itself, directly or by
// promotion from an embedded field, so its tags are not its wire shape (marshaler);
// a wire field of interface kind, directly or under pointers, slices, arrays or maps,
// whose shape is decided at run time (dynamic); an embedded unexported struct or
// pointer to one with a json name (a named field Filter cannot set) at a non-public
// level (unsettable). Fields are collected exactly as encoding/json collects them
//: an embedded struct or pointer to struct, exported or not, is promoted when its
// json tag names nothing and is a named field when it does; an unexported embedded
// field of any other kind and an unexported non-embedded field are not on the wire.
// Named and embedded struct fields, slice, array and map elements and pointers are
// walked, so a nested response type is checked through its container; a nested type
// implementing json.Marshaler or encoding.TextMarshaler is a leaf (tagged as a whole,
// omitzero when its kind is Struct or Array). Consumers: Manifest.Validate; moduletest
// views_tagged.
func Check(views []any, groups []Group) error

// GroupLevels returns, for each group a tag names anywhere in views (nested types
// included, walked as Check walks them), the highest level among that group's fields.
// A view or field Check would reject contributes nothing (a broken tag has no level);
// it cannot fail. Consumer: Manifest.Validate, which holds a group's holders permission
// to a Reaches at least that level (module-contract F64).
func GroupLevels(views []any) map[string]model.Sensitivity

// Policy is the regional's current audience per group name. Consumer: a service, once
// per use case.
type Policy map[string]Audience

// PolicyOf reads visibility_<name> for each group through the area's Reader. Never cached:
// settings are a map lookup and a change applies at once. Fails Internal for a nil r
// (a wiring bug), else with the reader's kinds.
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
// non-nil pointer to a struct that Check accepts (errs.Internal otherwise, with *v
// untouched: any problem Check would report for the type, not_omittable included, a
// group missing from pol, or a hidden field that cannot be set, means the module
// skipped Check, and nothing is returned). It writes through a non-nil embedded
// pointer, so a pointee shared between records is changed for all of them.
// Consumer: every service use case that returns a response type.
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
4. **What `Filter` zeroes.** A hidden field is set to its zero value (`nil` for a pointer, slice or map; the zero struct or scalar otherwise), which the field's omit option then drops from the body. `encoding/json`'s `omitempty` drops `false`, `0`, `""`, a nil pointer, interface, slice or map and an empty slice, map or string; it **never** drops a struct (`time.Time` included) and drops an array only when its length is zero, so a zeroed struct would come back as `{"street":""}` or `"0001-01-01T00:00:00Z"`, a placeholder decision 3 forbids. `omitzero` drops any zero value (`IsZero()` when the type has it). So `Check` requires `omitzero` on a Struct or Array kind and either option on every other kind, and requires it on every non-public field: an ungrouped `internal` field is hidden from an anonymous reader (F27, F40) and without the option would ship as `"note": ""`. A slice or map whose empty-but-visible value must still appear uses `omitzero`, which drops only nil; an `internal` scalar whose zero is absent on the wire is read as zero by the screen (a signed-in reader is never denied an `internal` field, so absence is unambiguous; decision 9). It never allocates, copies or reads the hidden value, and never logs. It walks top-level fields and the fields of embedded structs, exported or not (they are promoted on the wire), through an embedded pointer when it is non-nil (a nil one has nothing on the wire); it writes through that pointer, so a response must own its values: never embed a pointer to a cached or shared struct, or `Filter` for one viewer changes it for the next. It does not enter named struct fields, slices or maps: a container such as `Roster{Entries []RosterEntry}` is filtered after each entry, with the container's own relation (usually no subject).
5. **What `Result` reports.** For each returned grouped field: level `sensitive` always; level `personal` when the viewer is not the subject (`model.Sensitivity.ReadIsAudited(self)`); `internal` and `public` never. `Fields` are the JSON names in field order (deduplicated), bounded by `audit.MaxFields` (a type with more auditable fields than that fails `Check`); `Level` is the highest. A caller whose `Result.Audited()` is false records nothing for that record.
6. **Parsing and caching.** The plan for a `reflect.Type` is built once and kept in a `sync.Map`; `Check` and `Filter` share it. A plan records, per wire field: index path, JSON name, level, group, kind, omittable for its kind, settable. Fields are collected exactly as `encoding/json`'s `typeFields` collects them: an embedded field whose type (after one pointer) is a struct is walked and promoted when its `json` tag names nothing, exported or not; with a `json` name it is a named wire field (and, unexported, one `Filter` cannot set: `Check` rejects it unless `public`); an unexported embedded field of any other kind, an unexported non-embedded field and a `json:"-"` field are not on the wire; an exported embedded non-struct or interface field is a named field under its type name. A problem recorded in the plan, of any code, makes `Filter` fail closed. `Check` fails on a type whose JSON name is duplicated through embedding only when both are grouped (the ambiguity is `encoding/json`'s to resolve; the tag check does not try). A view type that is itself a leaf (implements a marshaler, directly or through a promoted method) has no wire fields to tag, so `Check` rejects it (`marshaler`); an interface-kind field has no static shape, so `Check` rejects it (`dynamic`): a module that needs a varying shape uses one tagged struct per shape, or `json.RawMessage` tagged as a whole.
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
| F6 | `Check` | a non-public field (grouped, or ungrouped `internal`) whose `json` tag lacks the option its kind needs: a scalar, pointer, slice, map or interface without `omitempty` or `omitzero`; a struct (`time.Time` and `model.ID` included) or array without `omitzero` (`omitempty` alone fails there, since it never omits a struct and omits an array only at length zero) | Invalid (`not_omittable`; the reason names the option wanted) | add the option | `TestCheck_NotOmittable_ReturnsInvalid` (was `TestCheck_GroupedNotOmittable_ReturnsInvalid`; table: scalar, pointer, slice, map, interface pass with either option; struct, `time.Time`, `model.ID`, array pass only with `omitzero`; an ungrouped `internal` field needs the option; an ungrouped `public` field never does; a grouped one does) |
| F6a | `Filter` + `json.Marshal` | a hidden grouped struct, `time.Time`, `model.ID` or array field, tagged `omitzero`; a hidden ungrouped `internal` scalar for the anonymous viewer, tagged `omitempty` | the encoded body lacks the key (no `{}`, no zero time, no `""`) | | `TestFilter_HiddenFieldEncodesAbsent` (table over the kinds; `privacytest.AssertHidden` on the encoded body) |
| F6b | `Check` | a view whose type implements `json.Marshaler` or `encoding.TextMarshaler`, directly or by promotion (it embeds `time.Time`) | Invalid (`views[i]` `marshaler`) | tag the fields of a plain struct instead | `TestCheck_SelfMarshalingView_ReturnsInvalid` (two cases: a direct method, a promoted one) |
| F6c | `Check` | a wire field of interface kind (`Data any`, `[]any`, `map[string]any`, `*any`) | Invalid (`views[i].<Field>` `dynamic`) | one tagged struct per shape, or `json.RawMessage` tagged as a whole | `TestCheck_InterfaceField_ReturnsInvalid` |
| F6d | `Check` | an embedded unexported struct, or pointer to one, with a `json` name, at a non-public or grouped level | Invalid (`views[i].<field>` `unsettable`) | export the type or drop the name | `TestCheck_NamedUnexportedEmbedded_ReturnsInvalid` (and the same field tagged ungrouped `public` passes) |
| F6e | `Check` | an embedded unexported struct, or pointer to one, without a `json` name, holding an untagged exported field | Invalid naming the promoted field (`views[i].inner.Name` `required`): it is walked as `encoding/json` promotes it | tag it | `TestCheck_UnexportedEmbeddedWalked` |
| F7 | `Check` | a view is not a struct or pointer to one | Invalid (`not_struct`) | | `TestCheck_NotStruct_ReturnsInvalid` |
| F8 | `Check` | a nested named struct, slice element or map value has an untagged field | Invalid naming the path (`views[i].Entries.LegalName`) | | `TestCheck_WalksNestedTypes` |
| F9 | `Check` | a **nested** type implementing `json.Marshaler`/`TextMarshaler` (`time.Time`, `model.ID`) | treated as a leaf; nil (at the top level it is F6b) | | `TestCheck_MarshalerIsLeaf` |
| F9b | `Check` | a **nested** type whose `json.Marshaler`/`TextMarshaler` has only a pointer receiver (clarification under D46): `encoding/json` calls it only when the value is addressable, so in a slice element, map value or non-addressable field it walks the struct's fields instead | Invalid (`views[i].<path>` `marshaler`) | give the type a value receiver, or tag it as a whole through a leaf type | `TestCheck_PointerReceiverMarshaler_ReturnsInvalid` |
| F9c | `Check` | a type that embeds two marshalers at the same depth (the promoted method is ambiguous, so the type has none and `encoding/json` walks its fields) (clarification under D46) | Invalid (`views[i].<path>` `marshaler`) | embed one, or name the field | `TestCheck_AmbiguousEmbeddedMarshalers_ReturnsInvalid` |
| F9a | `GroupLevels` | one group named at `internal` in one view and `sensitive` in a nested type of another; a group no view names; a view that is not a struct among valid ones | `sensitive` for the first; absent for the second; the non-struct skipped, the rest reported | `Manifest.Validate` compares `Reaches` | `TestGroupLevels_HighestAcrossViewsAndNested`, `TestGroupLevels_SkipsNonStruct` |
| F10 | `Check` | more auditable fields than `audit.MaxFields` | Invalid (`too_many {max}`) | split the type | `TestCheck_TooManyAuditable_ReturnsInvalid` |
| F10a | `Check` | an auditable (`personal` or `sensitive`) field whose JSON name is longer than `audit.MaxFieldBytes` | Invalid (`views[i].<Field>` `too_long {max}`) | shorten the JSON name | `TestCheck_AuditableNameTooLong_ReturnsInvalid` |
| F10b | `Check` | an auditable field whose JSON name does not match `[a-z][a-z0-9_]*` (`legalName`, `legal-name`) | Invalid (`views[i].<Field>` `malformed`) | rename it (snake case) | `TestCheck_AuditableNameNotAuditGrammar_ReturnsInvalid` |
| F11 | `Group.Validate` | `holders` offered without a `Permission`; malformed permission | Invalid (`permission` `required`/`malformed`) | | `TestGroupValidate_HoldersWithoutPermission_ReturnsInvalid` |
| F12 | `Group.Validate` | `Default` not in `Options`; fewer than two options; duplicate; unknown audience (neither built-in nor `Extra`) | Invalid (`default` `one_of`; `options` `too_few`/`duplicate`/`unknown`) | | `TestGroupValidate_Options_Table` |
| F13 | `Group.Validate` | `Extra` names a built-in audience | Invalid (`extra` `duplicate`) | rename | `TestGroupValidate_ExtraCollidesBuiltin_ReturnsInvalid` |
| F14 | `Settings` | two groups share a name | Invalid (`groups[j].name` `duplicate`) | | `TestSettings_DuplicateGroup_ReturnsInvalid` |
| F15 | `Settings` | valid groups | one `Enum` decl per group, key `visibility_<name>`, passing `settings.Decl.Validate` | | `TestSettings_ProducesEnumDecls` |
| F16 | `Filter` | `v` is nil, not a pointer, or not to a struct | Internal | fix the call | `TestFilter_NotPointerToStruct_ReturnsInternal` |
| F17 | `Filter` | the type fails `Check` for any reason (untagged field, bad level, malformed tag, ungrouped, `not_omittable`, duplicate, too many or malformed auditable names, `marshaler` (F6b, F9b, F9c), `dynamic`, `unsettable`) | Internal; `*v` untouched; a nested type failing `Check` for any reason fails `Filter` closed; nothing returned by the caller | fail the use case | `TestFilter_UncheckedType_ReturnsInternal` (table over every code, each asserting `*v` unchanged) |
| F17a | `Filter` | a field to hide cannot be set (a named unexported embedded field that `Check` would have rejected) | Internal before any field is changed | fail the use case | `TestFilter_UnsettableHiddenField_ReturnsInternal` |
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
| F28 | `Filter` | embedded struct with grouped fields, exported or unexported | its fields decided like top-level ones | | `TestFilter_EmbeddedWalked` (an unexported embedded case too) |
| F28a | `Filter` | two records embed the same `*Shared` pointer; one is filtered for a viewer who may not see a field | the field is zero in both (written through the pointer) | build each response from its own values; never embed a cached or shared pointee | `TestFilter_EmbeddedPointer_WritesThrough` (documents the behaviour) |
| F29 | `Filter` | a named struct field or slice holding grouped fields | untouched (the caller filters elements first) | | `TestFilter_NestedNotEntered` |
| F30 | `Filter` | hidden value | never read, logged or copied (`telemetrytest` capture clean; the zeroed memory has no marker) | | `TestFilter_HiddenValueNeverLogged` |
| F31 | `ViewerOf` | `person` is zero, with or without groups | Unauthenticated; no authz call | 401 | `TestViewerOf_NoActor_ReturnsUnauthenticated` (two cases: groups and none; the `authztest` fake records no call) |
| F32 | `ViewerOf` | `ExtentOf` answers `Permission` for a group permission | not an error; every group naming that permission is absent from `Extents` | | `TestViewerOf_NotHeld_IsAbsent` |
| F33 | `ViewerOf` | `ExtentOf` answers `Invalid` (undeclared) or a store kind | that kind | fail the use case | `TestViewerOf_AuthzError_Propagates` |
| F34 | `ViewerOf` | two groups share a permission | one `ExtentOf` call; both group names carry the same extent | | `TestViewerOf_DistinctPermissionsOnce` |
| F39 | `ViewerOf` | a person and no groups | `Viewer{Person: person}`, nil `Extents`, no authz call | | `TestViewerOf_NoGroups_NoAuthzCall` |
| F40 | `Filter` | the zero `Viewer` against a type with `public`, `internal` and grouped fields under every policy | `public` kept; everything else zeroed; empty `Result` | serve public routes with it | `TestFilter_ZeroViewer_PublicOnly_AllPolicies` |
| F35 | `PolicyOf` | reader fails | its kind | fail the use case | `TestPolicyOf_ReaderError_Propagates` |
| F35a | `PolicyOf` | `r` is nil | Internal | fix the wiring | `TestPolicyOf_NilReader_ReturnsInternal` |
| F36 | `PolicyOf` | a stored value outside `Options` | the group's default (settings decision 5) | | `TestPolicyOf_StaleValue_UsesDefault` (through `settingstest`) |
| F37 | `Shows` | unknown group | false | | `TestShows_UnknownGroup_False` |
| F38 | `privacytest.AssertHidden` | name present at any depth | fails `t` | | `TestAssertHidden_FindsNested` |

`Audience.Validate`, `Result.Audited`, `Policy.Shows` and `GroupLevels` cannot fail: they compute on values already in memory. `Filter` has no I/O.

## Lock-in check

No external technology is wrapped: reflection and `encoding/json` from the standard library. The tags are plain Go struct tags; a fork that drops this package keeps compiling.

## Test plan

- **Unit:** every row above, table-driven over policy × viewer × relation; the plan cache (`TestPlan_ParsedOnce`, `-race`); `Check` on the `hello` and `volunteers` view types as fixtures of a well-tagged module; a fixture type per `Check` failure. Every fixture that passes `Check` carries the omit option on each non-public field; `TestFilter_HiddenFieldEncodesAbsent` encodes with `encoding/json` after `Filter`, because the placeholder is the encoder's doing and only the body proves absence.
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

## Amendment A2 (raised 2026-10-21, approved 2026-10-21, D46): the encoder's omit rules, and what the review found

Raised by `go-reviewer` against the A1 implementation. Each item names the choice, the reason and the alternative not taken. The inline edits above are folded in; `module-hello.md` was edited alongside (its A2 bullet) and the other specs by Proposed changes 10 to 13 (applied).

**A2.1 `omitempty` never omits a struct.** `encoding/json` omits, under `omitempty`, `false`, `0`, `""`, a nil pointer, interface, slice or map and an empty slice, map or string; a struct is never omitted and an array only when its length is zero. A hidden grouped `time.Time`, `model.ID` (`[16]byte`), nested struct or array therefore encoded as a placeholder (`"when":"0001-01-01T00:00:00Z"`, `"home":{"street":""}`, `"arr":[0,0]`), which decision 3 forbids, while F6 said a struct "passes with omitempty or omitzero". *Chosen:* `Check` requires `omitzero` on a field of Struct or Array kind (marshalers included: the kind, not the method, decides what the encoder omits) and `omitempty` or `omitzero` on every other kind (F6, F6a). *Why:* `omitzero` drops any zero value and asks `IsZero()` when the type has one (`time.Time`, `model.ID`), so after `SetZero` the field is always absent; `omitempty` is kept where it already works so existing tags need not change. *Not taken:* requiring `omitzero` everywhere (a churn on every pointer, slice and map tag for no gain in safety); having `Filter` set a struct to a nil pointer (it cannot: the field's type is the struct).

**A2.2 An ungrouped `internal` field is hidden too, so it needs the option.** `Filter` zeroes ungrouped `internal` fields for the anonymous viewer (F27, F40), but `Check` required the option on grouped fields only, so a public route's body read `{"greeting":"hi","note":""}`: a placeholder, against decision 3 and against hello F38. *Chosen:* `Check` requires the option, per the kind rule, on every non-public field (decision 9). *Why:* it keeps decision 3 one rule, enforced at build time by `Check` rather than at run time by a viewer-dependent `Filter` failure; `privacytest.AssertHidden` keeps one meaning for every level; and the cost, an `internal` scalar's zero is absent on the wire, is unambiguous for the only readers who get `internal` fields (signed in, never denied them), so a screen reads absent as zero, which is what proto3's JSON mapping has done for years. *Not taken:* relaxing F38 and decision 3 for `internal` (two rules, and `hello`, the template, would teach the weaker one); `Filter` answering `Internal` when it must hide a non-omittable `internal` field (fail closed, but at run time on a public route that no build-time check can guard, since `Check` does not know which types serve anonymous readers).

**A2.3 `GroupLevels`.** `Manifest.Validate`'s F64 check (a group's holders permission `Reaches` at least the group's highest field level) needs the levels per group, and the parsed plans are unexported. *Chosen:* `GroupLevels(views []any) map[string]model.Sensitivity`, the highest level per group across the views and the nested types they reach; it cannot fail (what it cannot parse, `Check` reports). *Not taken:* `Check` returning the levels (its signature is approved and `error` is its whole answer); exporting the plan.

**A2.4 `Filter` fails closed on every plan problem.** The implementation split problems into "structural" (`Filter` fails) and the rest (`Check` only), and `not_omittable` was in the rest, so a type that skipped `Check` could ship a placeholder. *Chosen:* `Filter` answers `Internal` for any problem `Check` would report on the type, and for a hidden field it cannot set, before changing anything (F17, F17a). *Why:* a type reaching `Filter` with any problem means the module skipped `Check`; there is no problem it is safe to filter through. The plan follows `encoding/json`'s `typeFields` exactly (Behavior 6): unexported embedded structs and pointers to structs are walked (promoted when untagged, a named field when tagged), which the A1 implementation got wrong for the pointer case (it skipped it; the encoder does not). `PolicyOf` with a nil reader is `Internal` (F35a), as every constructor-less entry point treats a nil dependency.

**A2.5 Three more `Check` rejections** (the review's C1, C2, and the named-unexported case from A2.4). (a) `marshaler` (F6b): a view that implements `json.Marshaler` or `TextMarshaler`, directly or by a method promoted from an embedded `time.Time`, encodes as whatever the method returns; its tags are not its wire shape, so passing `Check` proved nothing. (b) `dynamic` (F6c): an interface-kind field has no static shape; neither `Check` nor `Filter` can see inside, so a module uses one tagged struct per shape or `json.RawMessage` tagged as a whole (a leaf). (c) `unsettable` (F6d): a named unexported embedded field is on the wire but `reflect` cannot set it; `public` is allowed (never zeroed), any other level is rejected at build time so F17a stays a backstop. *Not taken:* walking interface values at run time in `Filter` (a different design: per-value plans, allocation per call, and `Check` still blind).

**A2.6 Writing through an embedded pointer** (the review's C4). `Filter` zeroes fields through a non-nil embedded pointer; a pointee shared between two responses, or with a cache, is changed for both. *Chosen:* documented (Behavior 4, F28a): a response owns its values; a service never embeds a pointer to a shared struct. *Not taken:* copying the pointee before zeroing (an allocation per record, against decision 7, and it would silently hide the sharing bug).

**A2.7 `hello` F38 could not be tested.** `PublicGreeting` builds its types directly with every field `public` and the `Filter` calls hard-coded; removing them failed nothing, and a "test-only fixture through the same call" had no seam. *Chosen:* an unexported generic helper in `hello`, `publicView[T any](v *T) error` (one `Filter` with the zero `Viewer`, zero `Policy` and zero `Relation`), called by `PublicGreeting` for the container and each `Banner`; the test calls it with a test-only type holding an `internal` `omitempty` field and proves the key is absent from the encoded body (hello F38, reworded). That the use case calls the helper stays the reviewer's check, as Behavior 2 already says for every `Filter` call. *Not taken:* a `publicFilter` function field on the service (a seam that exists only to be replaced in a test, which the template would then teach every module to add).

**A2.8 `people.LegalNamesOf` and the actor's own ID.** With `people.contacts.read` held in department scope and no `people.membership` provider, F73 answers an empty map "for others", and the implementation drops the actor's own ID with them. The choice is clear from people's own rules (own records need no permission; `Get` on self is `Me`; self reads are not audited, decision 15): reach never excludes the actor. Proposed change 11.

**A2.9 Clarifications (no new decision, D46).** (a) Only an *ungrouped* `public` field is exempt from the omit option: A1.4 required it on every grouped field and A2 on every non-public field, so a grouped `public` field carries it (the Summary, F6 and F6d now say so). (b) Nested marshalers (F9b, F9c): a nested type whose marshaler has only a pointer receiver is rejected (`marshaler`), because `encoding/json` calls a pointer-receiver method only on an addressable value and otherwise walks the fields; two embedded marshalers at the same depth are rejected (`marshaler`), because the ambiguous promoted method leaves the type with none. A nested type failing `Check` for any reason makes `Filter` fail closed (F17).

**Test expectations that change** (everything else stands):

| Test | Before | After A2 |
|---|---|---|
| `TestCheck_GroupedNotOmittable_ReturnsInvalid` (F6) | "struct omitempty" was not a row; "ungrouped pointer plain" and "ungrouped scalar plain" passed | renamed `TestCheck_NotOmittable_ReturnsInvalid`; rows "struct omitempty", "time.Time omitempty", "model.ID omitempty", "array omitempty" fail `not_omittable`; "struct omitzero", "array omitzero" pass; "interface omitempty" passes; "ungrouped internal plain" (scalar, pointer, struct) fails; "ungrouped public plain" passes |
| `TestCheck_WellTaggedType_Passes`, `TestCheck_MarshalerIsLeaf` (F9) and every fixture that passes `Check`, in `privacy`, `module` (`viewOK`), `moduletest` and `hello` | `internal` fields untagged for omission; a grouped `time.Time` with `omitempty` | every non-public field carries the option its kind needs (`omitzero` on `time.Time`, `model.ID`, nested structs) |
| `TestFilter_UncheckedType_ReturnsInternal` (F17) | untagged field, unknown group | a table over every `Check` code, `not_omittable` included; each case leaves `*v` unchanged |
| `TestFilter_Anonymous_PublicOnly` (F27), `TestFilter_ZeroViewer_PublicOnly_AllPolicies` (F40) | fields zeroed | unchanged, and the new F6a encodes the result and asserts the keys are absent |
| `TestFilter_EmbeddedWalked` (F28) | exported embedded struct | plus an unexported embedded struct and a non-nil unexported embedded pointer |
| New: `TestFilter_HiddenFieldEncodesAbsent` (F6a), `TestCheck_SelfMarshalingView_ReturnsInvalid` (F6b), `TestCheck_InterfaceField_ReturnsInvalid` (F6c), `TestCheck_NamedUnexportedEmbedded_ReturnsInvalid` (F6d), `TestCheck_UnexportedEmbeddedWalked` (F6e), `TestGroupLevels_HighestAcrossViewsAndNested` and `TestGroupLevels_SkipsNonStruct` (F9a), `TestFilter_UnsettableHiddenField_ReturnsInternal` (F17a), `TestFilter_EmbeddedPointer_WritesThrough` (F28a), `TestPolicyOf_NilReader_ReturnsInternal` (F35a) | — | as the rows say |
| `hello`: `TestPublicGreeting_ZeroViewer_PublicFieldsOnly` (F38) | asserts keys of other types absent from the public body | keeps that; the fixture half moves to `TestPublicView_HidesNonPublicField`, which calls `publicView` with a test-only type (`Greeting string public`, `Note string internal omitempty`) and asserts `note` absent from the encoded body |
| `hello`: `TestViews_Tagged`, `TestViews_LevelsAsSpecified`, `TestGreeting_SignedIn_KeepsInternalFieldsOnly` | `mine`, `seen`, `id`, `greeting`, `created_at` tagged without the option | the tags carry `omitempty` (`omitzero` on `id`, `created_at`); a signed-in reader with `mine` 0 gets a body without `mine`, and the screen shows the `=0` wording |
| `hello` web: `hello.test.tsx` "signed-in counts with `mine` 0" | the body has `"mine": 0` | the body lacks `mine`; the screen renders the `=0` wording; the TS type has `mine?: number`, `seen?: number` |
| `module`: `TestManifestValidate_HoldersPermissionReachesTooLow_ReturnsInvalid` (F64) | levels computed inside `module` or through a fixture | through `privacy.GroupLevels`; a case where the group's highest level sits in a nested type |

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
10. (applied 2026-10-21, D46) **`plan/design/module-contract.md` (row 4):** `Manifest.Validate`'s F64 check takes its levels from `privacy.GroupLevels(Views)`; F62's `privacy.Check` is widened again (A2: the option per kind on every non-public field; `marshaler`, `dynamic`, `unsettable`); the `module` fixtures (`viewOK` and the rest) carry the option on every non-public field. The `add-module` skill's layout line gains "every non-public field of a response type is `omitempty`, or `omitzero` when it is a struct or array".
11. (applied 2026-10-21, D46) **`plan/design/people.md` (row 8), `LegalNamesOf` and F73:** reach never excludes the actor: with `people.contacts.read` held in any extent, the actor's own ID is answered whenever they have a legal name and the mode is not hidden (no audit entry: a self read, decision 15), whatever the extent and whether a `people.membership` provider is registered; F73's `LegalNamesOf` column reads "empty map for others; the actor's own name is still answered". F67 (no `people.contacts.read` anywhere: `Permission`) is unchanged. Test `TestLegalNamesOf_DepartmentContactsReadNoProvider_ReturnsNone` gains the self case (`...ReturnsSelfOnly`).
12. (applied 2026-10-21, D46) **`plan/design/module-volunteers.md` (row 19):** every `internal` field of its response types carries `omitempty` (or `omitzero` for a struct or array, so `time.Time` shift bounds and `model.ID`s); the screens treat an absent `internal` count or flag as zero; F126 to F141 unchanged.
13. (applied 2026-10-21, D46) **`plan/design/web-shell.md` (row 17) and `.claude/skills/go-conventions/SKILL.md`:** the UI rule gains "an absent `internal` scalar is its zero (a count shows 0, a flag is off); only a grouped field's absence means hidden"; a module's TypeScript response types mark every non-public field optional. go-conventions' module layout line gains the omit rule from change 10.
