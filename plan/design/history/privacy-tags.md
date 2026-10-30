# privacy-tags: amendment history

Amendment narratives, reasons, review findings and superseded wording moved verbatim out of `plan/design/privacy-tags.md` on 2026-10-30, when the amendments were folded into the spec body (only line breaks at sentence ends were added; no word changed).

## Notes moved from the spec header

- **Amendment A1 (approved 2026-10-21, D45):** `Viewer.Extents` is keyed by group name, not by permission; `ViewerOf(ctx, az, person model.PersonID, groups)` takes the actor because `authz.Authorizer` cannot tell it who the actor is and `privacy` may not import `identity` (a zero `person` is `Unauthenticated`); every grouped field needs `omitempty`/`omitzero` whatever its kind (a zeroed pointer without it serializes `null`, which is a marker);
  `Check` also rejects an auditable field whose JSON name does not fit `audit.Entry.Fields`; the conformance rules `views_tagged` and `visibility_settings_declared` run right after `manifest_valid`; the `privacy.audience.*` keys live in the shell's catalogues under `web/`; `hello` tags and lists all four of its wire types.
  Folded into the text below; "Amendment A1" before Open questions is kept as the record, with the test expectations that changed.
- **Amendment A2 (approved 2026-10-21, D46):** `encoding/json`'s `omitempty` never omits a struct (`time.Time` included) and omits an array only when its length is zero, so a hidden grouped struct came back as a zero placeholder;
  `Check` now requires `omitzero` on Struct and Array kinds and `omitempty` or `omitzero` on the rest, and requires the option on **every non-public field**, grouped or not (an ungrouped `internal` field zeroed for an anonymous reader was a placeholder too). `GroupLevels(views)` joins the interface (what `Manifest.Validate`'s `Reaches` check needs).
  `Check` rejects a view that marshals itself (`marshaler`), a field of interface kind (`dynamic`) and a non-public named unexported embedded field (`unsettable`); the plan follows `encoding/json`'s field rules exactly (unexported embedded structs and pointers to structs are walked).
  `Filter` fails closed (`Internal`) on any plan problem, `not_omittable` included, and on a hidden field it cannot set; it writes through an embedded pointer, so responses never share pointees. `PolicyOf` with a nil reader is `Internal`. Folded into the text below; "Amendment A2" before Open questions is kept as the record, with the test expectations that change.

## Amendment A1 (raised 2026-10-20, approved 2026-10-21, D45): two interface contradictions and six smaller points

Raised by the implementer against the Approved text. Each item names the choice, the reason and the alternative not taken. The inline edits above are folded into the text; `module-contract.md`, `module-hello.md` and `module-volunteers.md` were edited alongside so the three stay consistent with this one.

**A1.1 `Filter` and `Shows` could not find the viewer's extent.** `Policy` is keyed by group name, the tag carries only `group=<name>`, and `Viewer.Extents` was keyed by permission; nothing in `Filter`'s or `Shows`' arguments mapped a group to its permission. *Chosen:* `Viewer.Extents map[string]authz.Extent`, keyed by `Group.Name`;
`ViewerOf` still calls `ExtentOf` once per distinct permission (F34) and writes the extent under every group naming it. *Why:* the viewer's extents are consumed per group, so keying them per group puts the join where the data is produced (once per use case) instead of where it is consumed (once per field per record);
`Filter`'s and `Shows`' signatures stay as approved, the `Policy` literal tests write stays a plain map, and a hand-built `Viewer` in a test reads as "holder of group X here". *Not taken:* (B) passing `groups` to `Filter` and `Shows` widens every call site and repeats a lookup per field;
(C) a `Policy` struct carrying the permission per group makes `Policy` opaque (no literal in tests) and leaves `Viewer` saying something (`Extents` by permission) that no consumer asks.

**A1.2 `ViewerOf` could not learn `Viewer.Person`.** `authz.Authorizer` answers for the actor in `ctx` but exposes only `Allow`, `AllowIn` and `ExtentOf`; `privacy` may not import `identity`; and with no groups there was no `ExtentOf` call to produce F31's `Unauthenticated`. *Chosen:* `ViewerOf(ctx, az, person model.PersonID, groups)`; a zero `person` is `Unauthenticated` before any authz call.
*Why:* every use case already resolves the actor before its permission check (Behavior 1), so passing it costs nothing and keeps `privacy` free of `identity`; failing on a zero person keeps F31 and makes the anonymous path explicit: a public use case passes `privacy.Viewer{}` to `Filter` (Behavior 10, F40).
*Not taken:* an `actorFunc` seam (a second way to say the same thing; the service has the ID in hand); returning the zero `Viewer` for a zero person (a service that forgot its actor check would silently serve public fields instead of failing).

**A1.3 `hello` has four wire types, not one.** Only `Greet` returns `greeting.Greeting`; `Greeting` returns `hello.Greeting{Greeting, Mine, Seen}`, and `PublicGreeting{Greeting, Banners}` and `Banner{Module, Key}` are wire types too. *Chosen:* all four tagged and listed in `Views`: `PublicGreeting` and `Banner` fields `public` (the route has no actor), `hello.Greeting` and `greeting.Greeting` fields `internal`; `Filter` in all three use cases:
`Greeting` and `Greet` with `ViewerOf`, `PublicGreeting` with the zero `Viewer` over the container and over each `Banner`. *Why:* `Check` guarantees that a type is tagged, not that a public route's type is all `public`; only `Filter` with the zero `Viewer` enforces "public fields only" for an anonymous reader, and the template should show that shape as well as the signed-in one.
The per-`Banner` call shows the container-then-elements shape (Behavior 4) without a second module. *Not taken:* `Filter` only in `Greeting` and `Greet` (the implementer's proposal): it leaves the reviewer's rule "every use case that returns a response type calls `Filter`" with an exception in the template itself.

**A1.4 `null` is a marker.** A zeroed `nil` pointer, slice or map without `omitempty`/`omitzero` encodes as `"x": null`, which tells the reader the field exists (decision 3 forbids a marker). *Chosen:* `Check` requires `omitempty` or `omitzero` on every grouped field, whatever its kind (F6). `omitzero` is the choice for a slice or map whose empty-but-visible value must still appear (`[]`), since it drops only the nil value;
`omitempty` is fine for a pointer. *Not taken:* having `Filter` detect the kind and rely on the encoder: the encoder's behavior is what produces the marker.

**A1.5 Conformance rule order.** `views_tagged` and `visibility_settings_declared` run right after `manifest_valid`, before `templates_register`: both read the manifest only, and a tagging problem should be named before the rig builds the module. *Why not last:* the rules after `manifest_valid` assume a well-formed manifest; a `Views` mistake would otherwise surface as a confusing failure in a later rule.

**A1.6 `moduletest`'s own fixtures.** The fixture modules in `conformance_test.go` have routes (`http.NotFoundHandler`) and no response type; after A1.5 their `Conformance` calls fail `views_tagged` unless they pass `WithoutViews()`. They do; one new fixture (`untaggedView`: `Views` listing a struct with an untagged exported field) proves F65.

**A1.7 Where the `privacy.audience.*` keys live.** They are frontend catalogue entries in `web/src/shell/messages/<lang>.json` (the shell's catalogues, checked by the shell's catalogue test), not anything in Go: the `privacy` package, like a manifest, carries no human-readable text.

**A1.8 `Check` and audit's field grammar.** `Result.Fields` go straight into `audit.Entry.Fields`, which `audit.Entry.Validate` rejects when a name is longer than `audit.MaxFieldBytes` (64) or outside `[a-z][a-z0-9_]*`; that would fail a use case at run time, on the first audited read. *Chosen:* `Check` rejects an auditable field whose JSON name breaks either rule (F10a `too_long`, F10b `malformed`);
`privacy` imports `core/audit` for the two constants (core contracts may import each other acyclically; `audit` does not import `privacy`). The grammar is restated in `privacy` rather than exported from `audit`, which would be a change to an Implemented spec for one regexp; `TestCheck_AuditableNameNotAuditGrammar_ReturnsInvalid` also feeds the rejected names to `audit.Entry.Validate` so the two cannot drift apart unnoticed.

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

**A2.1 `omitempty` never omits a struct.** `encoding/json` omits, under `omitempty`, `false`, `0`, `""`, a nil pointer, interface, slice or map and an empty slice, map or string; a struct is never omitted and an array only when its length is zero.
A hidden grouped `time.Time`, `model.ID` (`[16]byte`), nested struct or array therefore encoded as a placeholder (`"when":"0001-01-01T00:00:00Z"`, `"home":{"street":""}`, `"arr":[0,0]`), which decision 3 forbids, while F6 said a struct "passes with omitempty or omitzero".
*Chosen:* `Check` requires `omitzero` on a field of Struct or Array kind (marshalers included: the kind, not the method, decides what the encoder omits) and `omitempty` or `omitzero` on every other kind (F6, F6a). *Why:* `omitzero` drops any zero value and asks `IsZero()` when the type has one (`time.Time`, `model.ID`), so after `SetZero` the field is always absent; `omitempty` is kept where it already works so existing tags need not change.
*Not taken:* requiring `omitzero` everywhere (a churn on every pointer, slice and map tag for no gain in safety); having `Filter` set a struct to a nil pointer (it cannot: the field's type is the struct).

**A2.2 An ungrouped `internal` field is hidden too, so it needs the option.** `Filter` zeroes ungrouped `internal` fields for the anonymous viewer (F27, F40), but `Check` required the option on grouped fields only, so a public route's body read `{"greeting":"hi","note":""}`: a placeholder, against decision 3 and against hello F38. *Chosen:* `Check` requires the option, per the kind rule, on every non-public field (decision 9).
*Why:* it keeps decision 3 one rule, enforced at build time by `Check` rather than at run time by a viewer-dependent `Filter` failure; `privacytest.AssertHidden` keeps one meaning for every level; and the cost, an `internal` scalar's zero is absent on the wire, is unambiguous for the only readers who get `internal` fields (signed in, never denied them), so a screen reads absent as zero, which is what proto3's JSON mapping has done for years.
*Not taken:* relaxing F38 and decision 3 for `internal` (two rules, and `hello`, the template, would teach the weaker one); `Filter` answering `Internal` when it must hide a non-omittable `internal` field (fail closed, but at run time on a public route that no build-time check can guard, since `Check` does not know which types serve anonymous readers).

**A2.3 `GroupLevels`.** `Manifest.Validate`'s F64 check (a group's holders permission `Reaches` at least the group's highest field level) needs the levels per group, and the parsed plans are unexported. *Chosen:* `GroupLevels(views []any) map[string]model.Sensitivity`, the highest level per group across the views and the nested types they reach; it cannot fail (what it cannot parse, `Check` reports).
*Not taken:* `Check` returning the levels (its signature is approved and `error` is its whole answer); exporting the plan.

**A2.4 `Filter` fails closed on every plan problem.** The implementation split problems into "structural" (`Filter` fails) and the rest (`Check` only), and `not_omittable` was in the rest, so a type that skipped `Check` could ship a placeholder. *Chosen:* `Filter` answers `Internal` for any problem `Check` would report on the type, and for a hidden field it cannot set, before changing anything (F17, F17a).
*Why:* a type reaching `Filter` with any problem means the module skipped `Check`; there is no problem it is safe to filter through. The plan follows `encoding/json`'s `typeFields` exactly (Behavior 6): unexported embedded structs and pointers to structs are walked (promoted when untagged, a named field when tagged), which the A1 implementation got wrong for the pointer case (it skipped it; the encoder does not).
`PolicyOf` with a nil reader is `Internal` (F35a), as every constructor-less entry point treats a nil dependency.

**A2.5 Three more `Check` rejections** (the review's C1, C2, and the named-unexported case from A2.4). (a) `marshaler` (F6b): a view that implements `json.Marshaler` or `TextMarshaler`, directly or by a method promoted from an embedded `time.Time`, encodes as whatever the method returns; its tags are not its wire shape, so passing `Check` proved nothing.
(b) `dynamic` (F6c): an interface-kind field has no static shape; neither `Check` nor `Filter` can see inside, so a module uses one tagged struct per shape or `json.RawMessage` tagged as a whole (a leaf). (c) `unsettable` (F6d): a named unexported embedded field is on the wire but `reflect` cannot set it; `public` is allowed (never zeroed), any other level is rejected at build time so F17a stays a backstop.
*Not taken:* walking interface values at run time in `Filter` (a different design: per-value plans, allocation per call, and `Check` still blind).

**A2.6 Writing through an embedded pointer** (the review's C4). `Filter` zeroes fields through a non-nil embedded pointer; a pointee shared between two responses, or with a cache, is changed for both. *Chosen:* documented (Behavior 4, F28a): a response owns its values; a service never embeds a pointer to a shared struct.
*Not taken:* copying the pointee before zeroing (an allocation per record, against decision 7, and it would silently hide the sharing bug).

**A2.7 `hello` F38 could not be tested.** `PublicGreeting` builds its types directly with every field `public` and the `Filter` calls hard-coded; removing them failed nothing, and a "test-only fixture through the same call" had no seam.
*Chosen:* an unexported generic helper in `hello`, `publicView[T any](v *T) error` (one `Filter` with the zero `Viewer`, zero `Policy` and zero `Relation`), called by `PublicGreeting` for the container and each `Banner`; the test calls it with a test-only type holding an `internal` `omitempty` field and proves the key is absent from the encoded body (hello F38, reworded).
That the use case calls the helper stays the reviewer's check, as Behavior 2 already says for every `Filter` call. *Not taken:* a `publicFilter` function field on the service (a seam that exists only to be replaced in a test, which the template would then teach every module to add).

**A2.8 `people.LegalNamesOf` and the actor's own ID.** With `people.contacts.read` held in department scope and no `people.membership` provider, F73 answers an empty map "for others", and the implementation drops the actor's own ID with them. The choice is clear from people's own rules (own records need no permission; `Get` on self is `Me`; self reads are not audited, decision 15): reach never excludes the actor. Proposed change 11.

**A2.9 Clarifications (no new decision, D46).** (a) Only an *ungrouped* `public` field is exempt from the omit option: A1.4 required it on every grouped field and A2 on every non-public field, so a grouped `public` field carries it (the Summary, F6 and F6d now say so).
(b) Nested marshalers (F9b, F9c): a nested type whose marshaler has only a pointer receiver is rejected (`marshaler`), because `encoding/json` calls a pointer-receiver method only on an addressable value and otherwise walks the fields; two embedded marshalers at the same depth are rejected (`marshaler`), because the ambiguous promoted method leaves the type with none. A nested type failing `Check` for any reason makes `Filter` fail closed (F17).

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
