# people: amendment history

Amendment narratives, reasons, review findings and superseded wording moved verbatim out of `plan/design/people.md` on 2026-10-30, when the amendments were folded into the spec body (only line breaks were added; no word changed).

## Notes moved from the spec header

- **Amended 2026-10-18 (M2 build, accepted deviations):** implementation deviations recorded in Behavior, "Accepted implementation deviations (M2 build, 2026-10-18)"; five owner questions raised by the build were answered 2026-10-18 (D37) and are folded into decisions 21 to 25 (plus 26 for the grant-revoked signal). Records of what was built, not design changes.
- **Amended 2026-10-19 (D39, owner approval of settings and notify):** people exports `Settings []settings.Decl`, sends the deletion-request notice itself through a `Notifier` seam (replacing the planned subscription by notify), exports `Templates`, `LanguageOf` and `ContactEmailOf`, and registers notify as a `Source`; see "Amendment 2026-10-19 (D39): settings and notify" before Open questions. Proposed changes 15 and 16 are applied.
- **Amendment 2026-10-20 (approved, owner, D44):** a `Source` named `files` is linked after the modules and a module's `DeletePerson` must delete its files about the person (files.md Proposed change 8); `Directory.LegalNamesOf` and the `volunteer-coordinator` starter role (privacy-tags Proposed change 3, volunteers Proposed change 8); the extension point `people.membership` (volunteers Proposed change 1). See "Amendment 2026-10-20 (D44)" before Open questions.
- **Amended 2026-10-21 (owner, D45):** decision 9 and the `people.contacts.read` row: a department-scoped extent of `people.contacts.read` reaches nobody while no `people.membership` provider is registered; a site extent reaches everyone; `people.directory.read` is unchanged. New failure mode F73.
- **Amended 2026-10-21 (owner, D46):** `LegalNamesOf` reach never excludes the actor: with `people.contacts.read` held in any extent, the actor's own ID is answered whenever they have a legal name and the mode is not hidden (no audit entry: a self read), whether or not a `people.membership` provider is registered. F73's `LegalNamesOf` column and its test change.
- **Accepted 2026-10-20 (owner, D40):** the implementation deviations marked "(implementation, 2026-10-19)" in this spec are accepted as written; no text changed.
- **Amendment P1 (2026-10-22, approved 2026-10-23, D48):** the owner's answers to volunteers' Q-VOL10 and Q-VOL11 (2026-10-22): **a playa name is unique across the whole installation** (decision 3's "not unique" is reversed by decision 29), **self-appointed** and **permanent once set**
  (decision 30). One exported rule, `people.NameKey`, says when two names are the same (NFKC, trim, one space between words, lower case; decision 31); the store writes it to the new column `core.person.playa_name_key` and the partial unique index `person_playa_name_key_idx` (migration `0003`)
  decides every clash, including the race between two people claiming one name; the violation is translated to `Conflict` with field `playa_name` code `taken`, which names nobody. `UpdateMe` refuses a different name once one is set (`Invalid` `playa_name` `permanent`); the same name again is a no-op
  and a respelling with the same key is accepted; the language-only exemption (D37) stands. A holder of `people.profiles.manage` may always change any person's set name, their own included, through `Update`, audited `people.person.changed` with `Fields [playa_name]` and `Reason "rename"`
  (Q-PEOPLE-8, answered yes by the owner 2026-10-23: "An admin may always make changes - that's why they exist, for when workflows are broken and exceptions need to be made."). A deleted person's name is free at once (decision 32). Name retirement, approval pipelines and several names per person (an
  alias plus a unique radio handle) are Later (decision 33). The profile screen makes the field read-only once set and says so before the first save. The record with the choices, the test expectations that changed and the owner's answer is "Amendment P1" before Open questions.
- **Amended 2026-10-23 (D51, owner; build deviations, accepted):** migration `0003`'s SQL key expression is the collapse-then-trim form with an explicit whitespace class (`lower(btrim(regexp_replace(normalize(playa_name, NFKC), '[\s\u0085\u1680\u2028\u2029]+', ' ', 'g')))`, as in the file) instead
  of the earlier `btrim`-then-collapse expression; its locale guard also refuses ICU-provider databases whose `lower()` cannot fold (F88); F85's "names the IDs" holds through db's `P0001` pass-through (db decision 4, D51); `peopletest.Fake` gains `SetName` and `RenameByManager` (an accepted
  deviation: the spec said the fake is unchanged); the error message keys are `module.people.field.taken` and `module.people.field.permanent`, not `core.people.field.*`; the locked playa name shows no "(required)".
- **Amendment P-W1 (raised 2026-10-30, D57; Approved D58; Implemented 2026-10-30):** the first-sign-in prompt is shown once per session and the server records it, with no browser storage: one column `core.person.name_prompted_at` (migration `0004`) compared with the session's `IssuedAt`
  (decision 34); `Me` records it in one conditional `UPDATE` and answers `first_prompt: true` exactly when it did (decision 35); never for the superuser or a named row; a read-only copy answers `false` and no error, so there is no loop (decision 36); `Options.Session` (`identity.SessionFrom`); the
  frame acts on `first_prompt` alone. See "Amendment P-W1" before Open questions; F91 to F103; Proposed changes 26 to 30 (applied 2026-10-30, D58); Q-PEOPLE-9 and Q-PEOPLE-10 answered (D58): a new session after a password change or "sign out everywhere else" prompts once more, and the prompt never
  stops.
- **Amendment 2026-10-28 (approved, owner, D53, demo.md Proposed change 5):** `Delete`, `FulfilRequest`, `LinkAccount` and `bms delete` map identity's `Permission` `protected` to `Conflict` `protected`; F89, F90.

## Summary paragraphs replaced by lists in the spec

The five paragraphs below stood in the Summary; the spec now states the same decisions as list items 27 to 36 and the lists around them.

**Open questions:** None (Q-PEOPLE-8, may a profile manager change a set playa name, was answered yes by the owner on 2026-10-23, broader than recommended, and is folded into decision 30). Before P1: the seven were settled 2026-10-18, folded into decisions 2, 5, 8, 9, 10, 19 and 20; the five raised
by the M2 build were answered 2026-10-18, D37, decisions 21 to 25). **Proposed changes elsewhere:** 18, listed at the end; 16 applied (14 on 2026-10-18, 15 and 16 on 2026-10-19, D39), 17 applied 2026-10-20 (D43), 18 pending (`docs/operating.md`: target not written).

**D39 amendment.** 27 the deletion-request notice is sent by people through its own `Notifier` seam, to the holders of `people.data.delete`, in `RequestDeletion`'s `Write` transaction; notify subscribes to nothing (notify decision 14). 28 `LanguageOf` and `ContactEmailOf` are system reads for notify and never reachable from a route.

**P1 amendment (2026-10-22, approved 2026-10-23, D48; owner's answers to volunteers Q-VOL10 and Q-VOL11 and, for the manager's rename, Q-PEOPLE-8).** 29 a playa name is **unique across the installation** (reverses decision 3's "not unique"): the key column `core.person.playa_name_key` is written by
the store from `NameKey(playa_name)` on every write of the name and the partial unique index `person_playa_name_key_idx` (`WHERE playa_name_key <> ''`) is the one decider, so two people claiming one name at once cannot both win; a `23505` on that index becomes `Conflict` with field `playa_name` code
`taken` on `UpdateMe`, `Create` and `Update`, naming nobody (the name is the caller's own input; who holds it is not said, not even to a profile manager). 30 a playa name is **self-appointed and permanent**: the person types it (a profile manager entering a walk-on types the name the person gives);
once a row holds a non-empty name, `UpdateMe` with a different `NameKey` fails `Invalid` `playa_name` `permanent`, the same name resubmitted is a no-op (the form sends the whole profile), a respelling with the same key (capitalisation, spacing) is accepted and audited like any change because the
key, not the spelling, is what is permanent and unique; the language-only exemption (decision 21, D37) is unchanged; a holder of `people.profiles.manage` may always change any person's set name, their own included, through `Update` (owner, 2026-10-23, Q-PEOPLE-8: "An admin may always make changes -
that's why they exist, for when workflows are broken and exceptions need to be made."): audited `people.person.changed` with `Fields [playa_name]` and `Reason "rename"`, the same `taken` rule, the old name free at once; this is the stopgap until the retirement and inappropriate-name process exists.
31 `NameKey` is NFKC normalisation (`golang.org/x/text/unicode/norm`, already in the module graph), `strings.TrimSpace`, internal whitespace runs collapsed to one space (`strings.Fields`), then `strings.ToLower`: the owner's purpose is telling people apart on the roster and the radio, so names that
look alike must share a key; the stored `playa_name` keeps the person's own spelling; the migration backfills the key for existing rows with the equivalent SQL expression (`lower(regexp_replace(btrim(normalize(playa_name, NFKC)), '\s+', ' ', 'g'))`) and refuses to run (RAISE, the transaction rolled
back, the set's version unchanged) when existing rows already clash, listing the person IDs and never a name, because inventing a new name for someone contradicts "self-appointed" and a silent skip would ship an index that does not hold; the installation resolves the clash on the previous version,
where renames are still allowed, then migrates. 32 a deleted person's name is **free at once**: holding it would keep a name after "deleted means deleted" and would pre-empt the retirement process the owner wants designed later; the audit trail keeps the person ID, never the name. 33 **Later**, each
one sentence so nothing here blocks it: name retirement (a name unused for about five years becomes eligible, with a way to retire a vulgar or otherwise inappropriate name sooner; needs a "last used" signal from modules and a retired-names table), optional approval pipelines for a new name (a
`pending` name the person cannot yet be shown under), and several names per person (an alias plus a radio handle where only the handle must be unique: a later `handle` column gets its own key column and unique index; whether the playa name stops being unique then is the owner's call at that time,
and dropping `person_playa_name_key_idx` is one migration). `Search`, `NamesOf`, `PlayaNameOf` and the export are unchanged; no signal is added (a rename can never create a clash, so volunteers' Q-VOL11 case disappears).

**P-W1 amendment (raised 2026-10-30, D57; Approved D58; Implemented 2026-10-30).** 34 the first-sign-in prompt is **once per session, recorded by the server**: `core.person.name_prompted_at timestamptz NULL` (migration `0004`) is due when `playa_name = ''` and the column is NULL or before
`identity.Session.IssuedAt` (read through the new seam `Options.Session SessionFunc`, `app`: `identity.SessionFrom`; `Options.Validate` field `people.session`); a prompt writes `GREATEST(now, IssuedAt)`, so each session is prompted at most once whatever two copies' clocks say, and several devices do
not re-prompt each other; a new session after a password change or "sign out everywhere else" is prompted once more (owner, 2026-10-30, D58). 35 `Me` records it itself: one conditional `UPDATE ... RETURNING id` (`MarkNamePrompted`) in its own `Write` transaction after the row read and the profile
build (a failed build leaves no mark; D58 review), the statement being the one decider under concurrency; `Profile.FirstPrompt` (wire `first_prompt`, `omitempty`, `internal`) is true exactly when the row was updated; `Get` never reports it; no POST, no audit, no permission beyond being signed in,
`updated_at` untouched; the column is exported with the person and never cleared. 36 the superuser and a named row answer `false` without writing; a read-only copy answers the profile with `false` and no error (`db.ErrReadOnly` from the mark is "cannot record"), so a read-only copy never prompts and
never loops; a person with no row yet is answered `true` without writing on a writable copy (decision 2's repair comes with their first save) and `false` on a read-only one; any other failure of the mark fails `Me` with its kind. The frame acts on `first_prompt` alone, once per answer, with
in-memory state and no browser storage. F91 to F103. **Open:** none (Q-PEOPLE-9 and Q-PEOPLE-10 answered, owner, 2026-10-30, D58: the repeat prompt after a new session stays; no stop for a person who keeps declining). **Proposed changes:** 26 to 30, applied 2026-10-30 (D58).

**Accepted deviations (M2 build, 2026-10-18).** Leaf package `people/rows`; extra exports (`PublishedSignals`, `SubscribedSignals`, `ParseRequestStatus`, `RequestStatus.String`, the Field constants, `personpage.SectionTimeout`); `UpdateMe`, `LinkAccount` and `Delete` take the deletion lock; export cooldown kept on Canceled; exit codes of the commands. Five owner questions are open. See Behavior and Open questions.

## Superseded wording replaced in the body

Each quotation is the text the body carried before it was rewritten to the current rule.

> 3 the playa name is the name shown everywhere; it is required, trimmed, not unique (two burners may share one; the UI shows pronouns and lets a lead tell them apart) and empty until the person fills it in; the shell prompts once per session, it never blocks (the "not unique" part is reversed by decision 29; the rest stands).

> until a membership contract exists (volunteers), holding `people.directory.read` in any extent reaches every person, which the spec says out loud;

> | `Me` | the profile screen, the shell's frame (language, prompt) | an actor | full profile with every field group, `Can` for self; a missing row answers an empty `Person{ID: actor}` with `Complete: false`, no write |

> from D44 also `volunteer-coordinator` and `department-admin` (see the amendment) |

> the department filter applies only once an enabled module contributes a provider (the 2026-10-20 amendment, part 3), and needs no permission change.

> **First sign-in:** the frame sends a session whose `playa_name` is empty to `/people/me?first=1` once per session (not the superuser); the screen shows a banner

> Status as of 2026-10-20: items 1 to 17 are applied (17, the redelivery part, 2026-10-20, D43); item 18 (`docs/operating.md`) waits for its target.

## Amendment 2026-10-19 (D39): settings and notify

Source: settings.md Proposed change 5 and notify.md Proposed change 5, approved 2026-10-19.

- **Settings.** `people.Settings []settings.Decl` declares `people.legal_name`, `people.phone` (`hidden|optional|required`, default `optional`) and `people.emergency_contacts` (`hidden|optional`; `required` is not an option, so the declaration is the rule and `Policy.Validate` agrees). Titles are catalogue keys `core.people.setting.<key>.title|description|option.<opt>`. `app` implements `PolicySource` over `Scoped("people")`.
- **Deletion-request notice.** `RequestDeletion` (after the request row and `people.request.created` are written, inside the same `Write` transaction): if `Notify` and `Holders` are non-nil, `Holders.HoldersOf(ctx, "people.data.delete")` (site-wide explicit grant holders, superusers excluded; the
  base role is not enumerated, so a permission only the base role carries yields nobody: authz amendment, approved 2026-10-20, D40), then `Notify.Send(Message{Template: "people.deletion-requested", To: holders, Key: "deletion-requested:" + requestID, Link: "/people/admin/requests"})` with no `Vars`.
  The body says only "A deletion request is waiting"; it names no person and carries no note (decision 27). No holders: nothing is sent and one info log line records the count (never a person). `Send` answering `Unsupported` (e-mail not configured) or a nil `Notify` means "no e-mail": one info log
  line, the request still succeeds. Delivery (implementation, 2026-10-19): the notice sends in batches of `notify.MaxRecipients` under one `Key`. `Notify.Configured` is probed once before the loop, and the whole notice is skipped (one info log line) if it is off. An `Unsupported` partway through the
  loop rolls the transaction back as `Unavailable`. Any other `Send` or `HoldersOf` error fails the request with its kind and rolls the transaction back (nothing is half-recorded).
- **System reads for notify.** `LanguageOf(ctx, PersonID) (string, error)` returns the person's saved language ("" when none); `ContactEmailOf(ctx, PersonID) (model.Email, error)` returns `contact_email` for a person without an account (`NotFound` when empty or absent). Neither checks an actor or audits; neither is on a route; `app` writes notify's `LanguageSource` and `AddressSource` over them (and over identity's `EmailOf`).
- **Export and deletion of notify's rows.** `app`'s `link` registers `Source{Name: "notify", Exporter: a.notify}` so `ExportPerson` and `DeletePerson` of notify (messages without bodies or addresses) run inside people's transaction like a module's.
- **Templates.** `people.Templates` (`deletion-requested.en.tmpl`, `.es.tmpl`; owner `people`; registered by `app`).

### Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F60 | `RequestDeletion` | `Notify` or `Holders` nil, or `Send` answers `Unsupported` | nil; the request is created; one info log | nothing | `TestRequestDeletion_NoEmail_RequestStillCreated` |
| F61 | `RequestDeletion` | `HoldersOf` returns nobody | nil; request created; nothing sent; one info log | nothing | `TestRequestDeletion_NoHolders_NothingSent` |
| F62 | `RequestDeletion` | `HoldersOf` or `Send` fails other than `Unsupported` | its kind; the transaction rolls back; no request row | retry | `TestRequestDeletion_NotifyFails_RollsBack` |
| F63 | `RequestDeletion` | success with holders | the holders are `To`, `Key` is the request ID, `Link` is `/people/admin/requests`, no `Vars` (asserted from `notifytest.Fake.Sent()`) | nothing | `TestRequestDeletion_SendsNoticeToHolders` |
| F64 | `RequestDeletion` | the request is a duplicate | `Conflict`; nothing sent | show the open one | `TestRequestDeletion_Duplicate_NothingSent` |
| F65 | `LanguageOf`, `ContactEmailOf` | zero person; no row or no address | Invalid; NotFound | notify uses the site default / reports `no_address` | `TestLanguageOf_Table`, `TestContactEmailOf_Table` |
| F66 | `Settings` | `emergency_contacts` set to `required` | Invalid (`people.emergency_contacts`, `one_of`) | settings refuses the change | `TestSettings_EmergencyContactsRequired_Invalid` |

Test plan additions: the notice tests use `notifytest.NewFake("people")`, a fake `Holders` and `telemetrytest.New(t)` asserting no person ID of a requester or note text appears in the message or any log; the Mailpit end-to-end run files a request and reads the notice.

## Amendment 2026-10-20 (approved, owner, D44)

**1. A `Source` named `files`** (files.md Proposed change 8). `app` links `people.Source{Name: "files", Exporter: adapter}` after the modules. The export document gains `"files": {...}` (the shape is files.md's, Behavior: `about` with content, `uploaded` metadata only); the deletion report's
`Modules["files"]` carries `Anonymized` (the `uploaded_by` references cleared). Decision 11's order reads "modules in build order, then files, then authz, then identity, then people's rows". A module's `DeletePerson` must delete its files whose subject is the person; otherwise files' `DeletePerson`
answers `Conflict in_use` naming the module and people's whole deletion rolls back. The bytes leave the store within the hour (`files.reap`); the operator note for `bms delete` is carried to `docs/operating.md` (not yet written).

**2. `Directory.LegalNamesOf(ctx, ids []model.PersonID) (map[model.PersonID]string, error)`** (privacy-tags Proposed change 3; volunteers Proposed change 8). Consumer: volunteers' roster. Requires `people.contacts.read` in any extent for the actor (`Permission` otherwise); at most `MaxNames` IDs
(`Invalid` above); omits people with no legal name; records one `people.person.read` per returned person with `Fields [legal_name]`, `Level personal` (decision 15), after loading and before returning; never on a route; `peopletest.Fake` gains `AddLegalName(id, name)`. `people.contacts.read` and
`people.directory.read` list `volunteer-coordinator` in `StarterRoles` (authz's role, same date). Under decision 7: per-field visibility choices, when people asks for them, are `privacy` groups (privacy-tags Q-PRIV2: later). Reach follows decision 9 (D45): a site extent answers for every ID; a
department-scoped extent answers for nobody else while no `people.membership` provider is registered (empty map, no audit, no `Permission`); reach never excludes the actor: their own ID is answered whenever they have a legal name and the mode is not hidden, whatever the extent and the provider, with
no audit entry (a self read, decision 15; D46); the caller's relation check is the further scope. When the regional's legal-name mode is hidden it returns an empty map with no store call and no audit (F72).

**3. The `people.membership` extension point** (volunteers Proposed change 1; the "later membership contract" of decision 9). `internal/contracts/membership`: `Point = "people.membership"`, `Provider{DepartmentsOf(ctx, people []model.PersonID) (map[model.PersonID][]model.DepartmentID, error)}`. A
module with members contributes a provider (`volunteers` contributes its `member` rows). With providers present, a department-scoped `people.directory.read@D` and `people.contacts.read@D` extent reaches only the people the providers report as members of D; `people.contacts.read` is seeded into
`department-admin`. Decision 9 is amended to that extent; where no provider is registered, `people.directory.read`'s "any extent reaches every person" stands unchanged until one is, but a department-scoped `people.contacts.read` reaches nobody (owner, 2026-10-21, D45).

### Failure modes (added)

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F67 | `LegalNamesOf` | the actor lacks `people.contacts.read` in every extent | Permission | show forbidden | `TestLegalNamesOf_NoContactsRead_ReturnsPermission` |
| F68 | `LegalNamesOf` | more than `MaxNames` IDs | Invalid | batch | `TestLegalNamesOf_TooManyIDs_ReturnsInvalid` |
| F69 | `LegalNamesOf` | a person without a legal name | (omitted from the map, no audit entry for them) | treat as absent | `TestLegalNamesOf_NoLegalName_OmitsAndDoesNotAudit` |
| F70 | `Delete` | a module left a file about the person | Conflict (`in_use`, naming the module) | fix the module's `DeletePerson` | `TestDelete_ModuleLeftFile_ReturnsConflictInUse` |
| F71 | `Get`, `Search` | a department-scoped extent and providers present | (only members of the department are reached); `Get` answers `NotFound` for a person outside a department-scoped directory extent; a provider failure fails the call closed with its kind. `Search` over-fetches up to `MaxSearchResults` and filters, so pages can be short (known limitation; a later contract method `MembersOf` could filter in SQL) | nothing | `TestSearch_DepartmentExtent_ReachesOnlyMembers` |
| F72 | `LegalNamesOf` | the regional's legal-name mode is hidden | (empty map; no store call, no audit) | treat as absent | `TestLegalNamesOf_LegalNameHidden_ReturnsNoneAndAuditsNothing` and `TestLegalNamesOf_LegalNameHidden_ReturnsNoneAndAuditsNothing_Integration` |
| F73 | `Get`, `Search`, `LegalNamesOf` | the actor holds `people.contacts.read` only in department scope (a `department-admin`) and no `people.membership` provider is registered (D45) | `Get`: internal fields only for others, no personal fields, no audit entry; `Search`: no personal-field match (no error, no leak), no `people.search.read` entry; `LegalNamesOf`: empty map for others, the actor's own name still answered (D46), no audit, no `Permission` (the permission is held) | render what came | `TestGet_DepartmentContactsReadNoProvider_OmitsPersonalFields`, `TestSearch_DepartmentContactsReadNoProvider_NoPersonalMatch`, `TestLegalNamesOf_DepartmentContactsReadNoProvider_ReturnsSelfOnly` |

## Amendment P1 (raised 2026-10-22, approved 2026-10-23, D48): one playa name, unique and permanent

Source: the owner's answers of 2026-10-22 to volunteers' Q-VOL10 and Q-VOL11, verbatim: "Q-VOL10: The system does not allow duplicate burn names. A process for retiring names will need to be devised later. General practice is that a name becomes eligable for retirement if not used within 5 years.
However, sometimes names are vulgar or inappropriate for other reasons, so a process needs to be made (eventually) to handle those as well. Q-VOL11: The system shouldn't allow someone to rename themselves to a conflicting name. For the sake of initial deployment, burn names are permanent and
self-appointed. Future development will create optional approval pipelines. Also, it is possible for someone to have multiple burn names, such as their known alias, then a ranger handle for use on the radio - only the latter needs to be unique. If it helps, we can defer some of this nuance." "Burn
name" is the playa name (`burn-domain`). Read as: for the initial deployment each person has one playa name, unique across the installation, chosen by the person and permanent once set; retirement, approval pipelines and several names are deferred. The inline text above is the spec's text. The owner
approved the amendment on 2026-10-23 (D48) and answered Q-PEOPLE-8 (P1.6); P1.5 and P1.7 were confirmed as written, and one name per person stands for now, handles Later (P1.8). Each item names the choice, the reason and the alternative not taken.

**P1.1 Where the rule lives (decision 29).** Volunteers' V1 enforced uniqueness per event, in its booking path, by reading names live and taking an advisory lock, because people decision 3 said names were not unique. The owner's answer makes the rule global, and a global rule belongs where the name
is written. *Chosen:* a key column on `core.person` written by the store from `NameKey` and a partial unique index on it; the index is the decider on every path, including the race. *Not taken:* (a) keeping the check in each module (every module that shows names would re-implement it, and none could
stop a clash made on the profile screen); (b) a pre-check `SELECT` before the write (it would still need the index for the race, so it only adds a query); (c) an expression index `lower(playa_name)` with no column (then PostgreSQL's `lower()` is the rule and Go's `NameKey`, which modules call, is a
copy that can drift; with the column, the index holds exactly what Go computed).

**P1.2 What "the same name" means (decision 31).** The owner's purpose is telling people apart on the roster and over the radio, so two names that look or sound alike must collide. *Chosen:* NFKC, trim, one space between words, `strings.ToLower`. NFKC folds precomposed and decomposed accents,
fullwidth letters and ligatures into one form; whitespace collapsing stops "Dusty  Roads" from passing as a second "Dusty Roads"; lower-casing is what the search index already did. The stored name keeps the person's own spelling. *Not taken:* (a) trim and lower only, as volunteers' V1 did ("Dusty
Roads" and "Dusty  Roads", or "é" in two encodings, would be two names on the roster that nobody can tell apart); (b) stripping diacritics or confusable-character folding ("Dusty" and "Dusty" with a Cyrillic "у"): a judgment call about lookalikes that belongs to the inappropriate-name process the
owner wants later, not to a key; (c) Unicode case folding (`ß` → `ss`) instead of `ToLower`: it has no SQL twin for the backfill, and the gain is a handful of letters. The one new dependency, `golang.org/x/text/unicode/norm`, is already in the module graph.

**P1.3 Existing duplicates at migration time.** The index cannot be created while two rows share a key. *Chosen:* migration `0003` backfills the key, then refuses to run (an exception, rolled back, version unchanged) if any key is held by more than one row, listing the person IDs and never a name;
the installation resolves the clash on the previous version, where renames are still allowed, and migrates again. The project is pre-release, so this is expected to find nothing, and when it does the operator must know. *Not taken:* (a) renaming one of the two with a suffix (the system would be
appointing a name, which the owner said the person does); (b) creating the index only when no clash exists and skipping it otherwise (a silent skip ships a rule that does not hold, and `module.Build`'s checksum check would never notice); (c) making the backfilled keys of clashing rows empty so the
index applies to everyone else (the same silent exception, hidden in data).

**P1.4 The error on a clash.** *Chosen:* `Conflict`, field `playa_name`, code `taken`, on every write path, as identity's sign-up answers a used address (identity decision 15, D31: the api mapper emits the field on a `409`). `Conflict` rather than `Invalid` because the input is well-formed and the
refusal depends on another row. The error carries nothing about the holder: the name is the caller's own input, so "taken" reveals only that someone in the installation uses it, which the uniqueness rule itself makes public; which person is not said, not even to a profile manager (who can search for
it, audited, if they must). *Not taken:* `Invalid taken` (would put a state-dependent refusal in the input-validation kind, and the shell already shows `fields` on a `409`).

**P1.5 Permanence (decision 30).** *Chosen:* `UpdateMe` refuses a different key once a name is set (`Invalid permanent`), accepts the identical string as a no-op (the form sends the whole profile) and accepts a respelling with the same key (audited like any change), because the key is what is unique
and permanent and the spelling is the person's. The language-only exemption (D37) is untouched: a language-only body sets no name. *Not taken:* refusing any change to the string (a person who typed their name in lower case on a phone could never fix it, for no gain in uniqueness).

**P1.6 A profile manager's change of a set name (Q-PEOPLE-8, answered by the owner 2026-10-23).** The owner said the process for retiring inappropriate names comes later. Until it exists, an installation with a vulgar name on a roster would have no remedy but deleting the person. *Recommended was*
that a holder of `people.profiles.manage` may change another person's set name; the owner went further: "An admin may always make changes - that's why they exist, for when workflows are broken and exceptions need to be made." *Chosen:* a holder of `people.profiles.manage` may change any person's set
name through `Update`, their own included (the "never for oneself" limit is dropped), audited `people.person.changed` with `Fields [playa_name]` and `Reason "rename"`, subject to the same `taken` rule; the old name is free at once. `UpdateMe` stays strict, so a person without the permission cannot
rename themself; `Update` on the actor's own ID still delegates to `UpdateMe`, except that a manager's own rename is a manager rename. The manager's edit form makes the name editable behind a confirm dialog. The audit entry is the record the regional reviews, and the permission is already the one
that creates and edits profiles for others. *Not taken:* no rename anywhere until the process is designed (the only remedy for an inappropriate name would be deleting the person, with their shifts and history); and the recommended "never for oneself" limit.

**P1.7 Deletion frees the name (decision 32).** *Chosen:* free at once; the row and its index entry go together. Holding it would keep a deleted person's name in a table after "deleted means deleted", and would decide a part of the retirement design (what is held, for how long, released by whom) the owner wants to make later. *Not taken:* a `retired_name` table written on delete (that is the Later design, and it can be added by a migration that does not touch this one).

**P1.8 Later (decision 33), one sentence each.** Retirement: a name unused for about five years becomes eligible, and a process retires vulgar or otherwise inappropriate names sooner; it needs a "last used" signal from modules (an event the person was booked at) and a retired-names table, neither of
which P1 creates. Approval pipelines: a regional may want a new name held `pending` until a role approves it; P1 writes the name at once, and a later `pending` state is a column, not a change to the key. Several names: an alias that need not be unique plus a radio handle that must (`burn-domain`,
"Handle"); a later `handle` column gets its own key column and unique index, and whether the playa name then stops being unique, or the handle simply defaults to it, is the owner's call then; `person_playa_name_key_idx` can be dropped in one migration, and nothing in P1 assumes it stays.

**P1.9 What does not change.** `Search`, `NamesOf`, `PlayaNameOf`, `LegalNamesOf`, the session's `playa_name` (identity's `NameSource` is a read), the export document (the key is derived and is not exported), the signals (no rename signal: under P1 a rename cannot create a clash, so volunteers'
Q-VOL11 case cannot arise), `Create` still requiring a name (decision 6's "gaps" never included the playa name; the account-created handler still creates a nameless row, whose empty key the partial index ignores, so any number of new accounts may wait to choose a name), and the shell (the
first-sign-in banner is people's own screen and key).

### Failure modes (added)

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F74 | `UpdateMe` (first name, or the repair insert), `Create` | another person holds the same `NameKey` (the store sees `23505` on `person_playa_name_key_idx`) | Conflict (`playa_name` `taken`); the transaction rolled back; no audit entry, no signal | show it under the field; say nothing about who | `TestUpdateMe_NameTaken_ReturnsConflict`, `TestCreate_NameTaken_ReturnsConflict`, `TestUpdateMe_NameTaken_NothingAuditedOrPublished` |
| F75 | `UpdateMe` | a name is set and the input's key differs from the stored key | Invalid (`playa_name` `permanent`); nothing written | the field is read-only, so only a stale client sees it | `TestUpdateMe_ChangeSetName_ReturnsInvalidPermanent` |
| F76 | `UpdateMe` | a name is set and the input is the same string | nil; nothing written, nothing audited | none | `TestUpdateMe_SameName_NoOp` |
| F77 | `UpdateMe` | a name is set and the input has the same key but another spelling | nil; `playa_name` written, key unchanged; `people.person.changed` with `Fields [playa_name]` | none | `TestUpdateMe_RespellSameKey_Saves` |
| F78 | `Update` (profile manager, any person including themself) | a name is set and the input's key differs | nil; written; `people.person.changed` with `Fields [playa_name]`, `Reason "rename"`; the `taken` rule applies (F74) | confirm dialog, then the result | `TestUpdate_RenameByManager_SavesAndAuditsReason`, `TestUpdate_RenameOwnNameByManager_SavesAndAuditsReason` |
| F79 | `Update` (profile manager) | a name is set and the input is `""` | Invalid (`playa_name` `required`) | field error | `TestUpdate_ClearSetName_ReturnsInvalidRequired` |
| F80 | `UpdateMe` × 2 (integration) | two people claim the same key at once | exactly one nil and one Conflict (`taken`); no deadlock; one row holds the key | the loser chooses another | `TestUpdateMe_NameRace_OneWins` |
| F81 | the store | a `23505` on an index other than `person_playa_name_key_idx` | Conflict without a field (as before) | 409 | `TestStore_OtherUniqueViolation_PlainConflict` |
| F82 | `NameKey` | `" Dusty "`, `"DUSTY"`, `"dusty"`, `"Dusty Roads"`, `"Dusty  Roads"`, `"Dusty\tRoads"`, `"é"` precomposed and decomposed, a fullwidth `"Ｄusty"`, `""`, `"   "` | the first three share one key; the three two-word forms share one key; the two "é" forms share one key; the fullwidth form equals `"dusty"`; the last two give `""`; never an error | | `TestNameKey_Table` |
| F83 | `UpdateMe` | body sets only `language` while a name is set | nil; language saved (F54 stands) | none | `TestUpdateMe_LanguageOnly_IgnoresPermanence` |
| F84 | account-created handler, then a second account | two nameless rows | nil; both inserted (empty keys are outside the partial index) | none | `TestHandleAccountCreated_TwoNamelessRows_BothInserted` |
| F85 | migration `0003` (integration) | two rows whose backfilled keys are equal | the migration fails with an error naming the count and the person IDs (no name); set version stays 2; no column, no index | the operator resolves the clash on the previous version | `TestMigration0003_Duplicates_FailsLoud` |
| F86 | `Delete`, then `UpdateMe` by someone else | the deleted person's name | nil for the second claim (the name is free, decision 32) | none | `TestDelete_FreesPlayaName` |
| F87 | migration `0003` (integration) | server encoding is not UTF-8 | the migration fails naming the encoding; nothing applied | the operator fixes the database | `TestMigration0003_NonUTF8_Fails` (skipped where the container cannot create such a database; the SQL is asserted to contain the check) |
| F88 | migration `0003` (integration) | `lower()` cannot fold non-ASCII (`LC_CTYPE=C`, or an ICU-provider database where `lower(E'İ') <> 'i'` or `lower('ΑΣ') <> 'ασ'`) and an existing playa name holds non-ASCII (D51) | the migration fails naming the locale; nothing applied | the operator uses a UTF-8 locale such as `C.UTF-8` or `en_US.UTF-8` | `TestMigration0003_SQLChecksUTF8` asserts the guard is in the SQL; the failing case is skipped where the container cannot create such a database |

F5 (`required`) is unchanged and runs before F75. The earlier sentence "the UI shows pronouns and lets a lead tell them apart" in decision 3 is no longer the mechanism for equal names, which cannot occur.

**Test expectations that change** (everything else stands):

| Test | Before | After |
|---|---|---|
| `TestUpdateMe_*` success paths that renamed a person | any new name accepted | a second, different name is F75; fixtures set the name once, or use the same key (F77) |
| `TestUpdate_*` (manager) that renamed a person | accepted silently | F78 (`Reason "rename"` asserted; also for the manager's own name) |
| `TestProfileInput_Validation_Table` (F6) | shape rules only | unchanged, but the integration seeds give every person a distinct name |
| every integration seed that inserted several people | names free | distinct names (the index refuses a repeat); `dbtest` fixtures that insert `core.person` rows directly must also write `playa_name_key` (or insert through the store) |
| `TestMigrations_Apply` (integration) | version 2 | version 3; the F85 and F87 cases added |
| the app end-to-end profile flow | "fill in the playa name, see it in the header" | plus "a second account tries that name and sees the taken message" |
| web `MyProfile` tests | the field always editable | read-only once set, with its explanation; `taken` inline; the new banner wording |

## Amendment 2026-10-28 (approved, owner, D53): shared demo accounts cannot be deleted

In demo mode identity's `Guard` refuses `DeletePerson` for a demo account with `Permission` (field `account`, code `protected`; identity.md F105). `Delete`, `FulfilRequest` and `DeleteCommand` map that to `Conflict` with code `protected`, beside `Conflict superuser`; `LinkAccount` does the same when
identity's `LinkAccount` refuses a protected account (identity.md F109; the guard error fails the link closed); nothing is deleted (the transaction rolls back, as for F35). The people deletion screen shows the shared-account message for it (web-shell Proposed change 10).

### Failure modes (added)

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F89 | `Delete`, `FulfilRequest`, `DeleteCommand` | identity refuses with `Permission` `protected` (a shared demo account) | Conflict `protected`; nothing deleted | message | `TestDelete_ProtectedAccount_ReturnsConflict_RollsBack` |
| F90 | `LinkAccount` | identity refuses with `Permission` `protected` (a shared demo account) | Conflict `protected`; nothing linked | message | `TestLinkAccount_ProtectedAccount_ReturnsConflict` |

## Amendment P-W1 (raised 2026-10-30, D57): first-sign-in prompt once per session, recorded by the server

- **Status:** Implemented 2026-10-30; Approved D58 (the rest of the spec stays Implemented).

**Source.** The owner decided (D57) that the first-sign-in prompt for a person with no playa name is shown **once per session**, and that **the server records that the person was prompted**, with no browser storage (web-shell bans `sessionStorage`, "Data"; `localStorage["bms.lang"]` is the shell's
one key). Today the frame keeps an in-memory flag in `web/src/shell/me.ts`, so every full page load (a reload, a typed URL, a deep link) prompts again. The frame already passes the intended page as router state (`next`) and `/people/me?first=1` offers "Continue to where you were going"; both stay.

**P-W1.1 Where the mark lives (decision 34).** Identity has no row per session: the token is stateless and a read-only copy cannot write one (identity decisions 2 and 3). "Once per session" therefore cannot be a flag on a session row. *Chosen:* one nullable column on the person's own row,
`core.person.name_prompted_at timestamptz`, compared with the session's `IssuedAt` (`identity.Session.IssuedAt`, server time at sign-in, fixed for the session's life): the prompt is **due** when `playa_name = ''` and `name_prompted_at IS NULL OR name_prompted_at < session.IssuedAt`. Recording the
prompt sets the column to `GREATEST(now, session.IssuedAt)`, so after one prompt the column is never older than the session that saw it, whatever the clocks of two copies say. Each session is prompted at most once: a session issued before the column's value has been prompted (or a later session of
the same person has, which also satisfies the owner's rule); a session issued after it is prompted once and then moves the column past its own `IssuedAt`. *Not taken:* (a) storing the last prompted session's ID (two devices signed in at once would prompt each other again on every reload, because
each prompt overwrites the other's ID); (b) a column on `core.account` (the prompt is about the profile, which is people's, and identity's table stays identity's); (c) a per-session table (identity decision 3 left that for Later, and it would not work on a read-only copy). After a password change or
"sign out everywhere else" identity issues a new session (a new `IssuedAt`), so the prompt shows once more; that is the rule taken literally (accepted, owner, 2026-10-30, D58).

**P-W1.2 How it is set (decision 35).** *Chosen:* `Me` records it itself, in the same call that first answers "unnamed" for this session: `Me` decides whether a prompt is due, builds the profile, and only after the build succeeds records the mark in its own short `Write` transaction (D58 review: a
failed build leaves no mark, so the next `/me` still prompts), with one conditional statement, `UPDATE core.person SET name_prompted_at = GREATEST($now, $issued) WHERE id = $person AND playa_name = '' AND (name_prompted_at IS NULL OR name_prompted_at < $issued) RETURNING id`, and answers
`first_prompt: true` exactly when a row was updated. The statement is the one decider, so two tabs loading at once in one session get one `true` and one `false`, and repeating the call is harmless (it is idempotent per session), which is what matters for a read that writes. The column is not a
profile change: `updated_at` is untouched and nothing is audited (the viewer is the person; the mark is `internal`). *Not taken:* (a) a separate `POST /api/people/me/prompted` (an extra route, an extra round trip, and the browser deciding when to call it; the read-only case then needs browser logic
to avoid a loop, which is what D57 removes); (b) a field on `GET /api/accounts/session` (identity would be recording a fact about the profile). The cross-origin guard covers unsafe methods only (api decision 6); a forged cross-site `GET /api/people/me` could at most mark the person prompted, which
reveals nothing and changes nothing they can see.

**P-W1.3 Who is never prompted, and the missing row (decision 36).** The superuser answers `first_prompt: false` with no write (the break-glass account has no profile to complete). A row that already holds a name answers `false` with no write. A person **with no row yet** (the
`identity.account-created` handler failed; decision 2) has nothing to mark: `Me` answers `true` without writing when the copy is writable, so the prompt leads them to the save that repairs the row (`UpdateMe`'s upsert), and a reload before that save prompts again; that costs one tap in a case
decision 2 already calls rare, and inserting the row from `Me` would turn a read into the creation path with its audit entry and signal. `Get` of one's own ID behaves as `Me` for the profile but never reports or records the prompt (`first_prompt` absent): the frame reads `/me` only.

**P-W1.4 A read-only copy.** `Transact(Write)` on a read-only copy fails before any statement with `Unavailable` wrapping `db.ErrReadOnly` (db F22). `Me` treats that one error as "cannot record" and answers the profile with `first_prompt: false` and a nil error: the person cannot save a name on that
copy either (the screen's fields are disabled behind the shell's read-only banner), so a prompt there is useless and, unrecorded, would repeat on every load. A missing row on a read-only copy answers `false` for the same reason (`db.DB.ReadOnly()`). When the copy becomes writable again the next
`/me` prompts once. Any other failure of the mark (`Unavailable`, `Timeout`, `Canceled`) fails `Me` with that kind: the read already needed the same database, and swallowing a write failure would hide an outage; the frame tolerates a failed `/me` (it never blocks).

**P-W1.5 The seam.** `Me` needs the session's `IssuedAt`, which `authz.Actor` does not carry. `Options` gains `Session SessionFunc` with `type SessionFunc func(ctx context.Context) (identity.Session, bool)`; `app` wires `identity.SessionFrom`; `Options.Validate` adds the field `people.session` to
its nil checks; tests build contexts with `identity.WithSession` and pass `identity.SessionFrom`, or a closure returning a fixed `IssuedAt`. `core/identity` is already in people's import list (Summary). `authz.Actor` is unchanged. *Not taken:* adding `IssuedAt` to `authz.Actor` (a change to authz
for one reader).

**P-W1.6 The wire and the frame.** `Profile` gains `FirstPrompt bool`; the wire form `GET /api/people/me` gains `first_prompt` (boolean, `omitempty`, so it is absent when false; level `internal`, no group: it is only ever the viewer's own), and `openapi.json` and the generated `web/src/api` types
follow. The frame (`web/src/shell/me.ts`) acts on `first_prompt === true` and on nothing else: it drops its own `playa_name === ""` and `superuser` tests (the server decides) and sends the person to `/people/me?first=1` with `next` as today, unless they are already there. A cached answer with
`first_prompt: true` must not fire again on every route change, so the frame keeps its in-memory "acted on this answer" marker (JavaScript state, reset on sign-out; not storage, which web-shell "Data" forbids). A reload fetches `/me` afresh and the server answers `false`, which is the whole point.
`PUT /api/people/me` answers a `Profile` without `first_prompt`. The profile screen (`?first=1`, the banner, the focus, "Continue to where you were going") is unchanged.

**P-W1.7 What does not change.** Permissions (none added: `Me` needs a signed-in actor and touches the actor's own row), audit (nothing recorded: a self read and an `internal` mark), signals, settings (no regional would want the prompt more or less than once per session, so there is no setting),
`NamesOf`, `PlayaNameOf`, `Search`, `UpdateMe` (a saved name makes the column moot; it is never cleared), identity (reads `Session.IssuedAt` only), demo mode (the shared `newcomer` account's visitors each have their own session, so each is prompted once; the seed and reset leave the column NULL;
demo.md needs no change).

### Interface (added)

```go
// SessionFunc reads the request's session from ctx without I/O (identity.SessionFrom).
// ok is false when there is no session. Me reads IssuedAt from it (decision 34).
type SessionFunc func(ctx context.Context) (identity.Session, bool)

// Options gains:
//   Session SessionFunc // app: identity.SessionFrom; Validate fails Invalid field people.session when nil

// Profile gains:
//   FirstPrompt bool // Me only: true when this call recorded the first-sign-in prompt for
//                    // this session (decision 35); Get never sets it. Wire: first_prompt, omitempty.

// store gains (postgres.Store implements it; sql/mark_name_prompted.sql):
//
// MarkNamePrompted records that the person was prompted for a name, if the prompt is due
// (playa_name = '' and name_prompted_at is NULL or before issued). It writes
// GREATEST(now, issued). Returns true when the row was updated. Runs inside the Write
// transaction ctx carries. Fails Unavailable wrapping db.ErrReadOnly on a read-only copy
// (the caller maps it to "not prompted"), and the store's kinds.
MarkNamePrompted(ctx context.Context, person model.PersonID, issued, now time.Time) (bool, error)
```

`Me`'s doc comment gains: "On a writable copy, when the actor is not the superuser and the row holds no playa name, `Me` records the first-sign-in prompt once per session and reports it in `FirstPrompt` (F91 to F99)."

### Data (added)

Migration `0004_name_prompted_at.sql` (version 4), one statement plus its comment: `ALTER TABLE core.person ADD COLUMN name_prompted_at timestamptz NULL` with `COMMENT ... 'sensitivity: internal'`. Safe on a live table (no rewrite: a nullable column with no default).

| Column | Type | Constraints | Level | Why collected |
|---|---|---|---|---|
| `name_prompted_at` | `timestamptz` | NULL | internal (the viewer's own; never shown as a time, never logged) | when the person was last asked for a playa name, so the prompt runs once per session (decisions 34, 35); exported with the person object (`export_person.sql` gains the column; "export everything" has no exception for marks), deleted with the row, moot once `playa_name` is set |

`insert_person.sql`, `upsert_person.sql` and `update_person.sql` never set or clear the column.

### Behavior (added)

`Me`, in order (D58 review): actor (F3); the session from ctx, which a signed-in actor must have (else `Internal`, a wiring error, F104); the row (F4); decide whether a prompt is due (writable copy, actor not the superuser, row has no name); build the sections and the profile as today; and only
after the build succeeds, when due, `MarkNamePrompted` in a `Write` transaction of its own (not inside the read, go-conventions "a change never records inside a `Read` transaction"), `db.ErrReadOnly` mapped to `false`, with `FirstPrompt` set from the result. A failed build returns its error and
leaves no mark (F105). Concurrency: the `UPDATE`'s `WHERE` decides under the row lock, so concurrent calls in one session yield exactly one `true`. Idempotent per session. Nothing retries. The span `people.me` gets `telemetry.Bool("first_prompt", v)`; no metric.

### Failure modes (added)

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F91 | `Me` | no name, first read of this session (column NULL or before `IssuedAt`) | nil; `FirstPrompt true`; column = `GREATEST(now, IssuedAt)`; `updated_at` untouched; nothing audited | send the person to `/people/me?first=1` | `TestMe_Unnamed_FirstReadPrompts`, `TestMe_Prompt_NothingAudited` |
| F92 | `Me` | no name, a second read in the same session | nil; `FirstPrompt false`; no write | nothing | `TestMe_SecondReadSameSession_NoPrompt` |
| F93 | `Me` | no name, a later session (`IssuedAt` after the column) | nil; `FirstPrompt true` once more | prompt once | `TestMe_NewSession_PromptsAgain` |
| F94 | `Me` | no name, an older session reads after a newer session was prompted | nil; `FirstPrompt false` | nothing | `TestMe_OlderSession_AfterPrompt_NoPrompt` |
| F95 | `Me` | the row holds a name, or the actor is the superuser | nil; `FirstPrompt false`; no write | nothing | `TestMe_Named_NoPromptNoWrite`, `TestMe_Superuser_NoPromptNoWrite` |
| F96 | `Me` | the mark fails `Unavailable` wrapping `db.ErrReadOnly` (read-only copy) | nil; the profile answered; `FirstPrompt false` | nothing (no loop) | `TestMe_ReadOnly_NoPromptNoError` |
| F97 | `Me` | the mark fails with another store kind | that kind (`Unavailable`, `Timeout`, `Canceled`); no profile | `ErrorState`; the frame never blocks | `TestMe_MarkFails_ReturnsStoreKind` |
| F98 | `Me` | no row yet (decision 2): writable copy / read-only copy | nil; `FirstPrompt true` without writing / `false` | prompt (leads to the repairing save) / nothing | `TestMe_NoRow_PromptsWithoutWriting`, `TestMe_NoRow_ReadOnly_NoPrompt` |
| F99 | `Me` × 2 (integration) | two reads of one session at once | exactly one `true`; one row, one write | prompt once | `TestMe_ConcurrentSameSession_OnePrompt` |
| F100 | `Me` | `now` is before `IssuedAt` (a copy whose clock lags the one that signed the person in) | nil; `FirstPrompt true`; column = `IssuedAt`, so the next read is F92, not F91 | prompt once | `TestMe_ClockBehindIssuedAt_StillOnce` |
| F101 | `Get` (own ID) | no name, prompt due | nil; `FirstPrompt false`; no write | nothing | `TestGet_Self_NeverPrompts` |
| F102 | `New` / `Options.Validate` | `Session` nil | Invalid (field `people.session`) | fix the wiring | `TestOptions_NilSession_ReturnsInvalid` |
| F103 | migration `0004` (integration) | applied on a version-3 set | version 4; the column NULL for every row; `TestMigrations_Apply` moves to 4 | | `TestMigrations_Apply` |
| F104 | `Me` | a signed-in actor with no session in ctx (D58 review) | Internal (wiring error) | fix the wiring | `TestPromptName_NoSessionForAnActor_IsInternal` |
| F105 | `Me` | the profile build fails after a prompt was found due (D58 review) | the build's error; no mark is written, so the next `/me` still prompts | retry | `TestMe_BuildFails_LeavesNoMark` |

Web (`web/src/shell/me.test.tsx` or `people.test.tsx`): "a fresh mount whose `/me` answers `first_prompt: false` never navigates" (the reload case), "`first_prompt: true` navigates once with `next`, and not again on the next route change", "a sign-out then a new sign-in acts on the new answer",
"nothing is written to `sessionStorage` or `localStorage` except `bms.lang`" (storage spies). The `people.test.tsx` cases that mount with an empty `playa_name` now set `first_prompt: true` in the fixture; the test "sends a session with no playa name to `/people/me?first=1` once" keeps its name and
asserts the flag, not the name.

**Test expectations that change** (everything else stands): every `Options` fixture gains `Session`; `TestMigrations_Apply` expects version 4; the app end-to-end profile flow adds "reload after Continue: no second prompt; sign in again: one prompt".

## Open questions: answered items moved from the spec

**Answered 2026-10-30 (owner, D58, both recommendations accepted).** Q-PEOPLE-9: a new session after a password change or "sign out everywhere else" prompts a person with no chosen name once more; accepted, it is "once per session" taken literally and costs one tap (decision 34, P-W1.1). Q-PEOPLE-10: the prompt never stops for a person who keeps declining; once per session stays, with no "don't ask again" and no setting (decision 34, P-W1.7). Open questions: None.

**Answered 2026-10-23 (owner, D48).** Q-PEOPLE-8, may a holder of `people.profiles.manage` change a set playa name as the stopgap until the retirement process exists: yes, broader than recommended: any person's name including their own, audited with `Reason "rename"`, same `taken` rule, the old name free at once (decision 30, P1.6, F78). Numbering continues from Q7; the five questions the M2 build raised were unnumbered.

**Before P1 (2026-10-22):** None. The seven questions were answered by the owner on 2026-10-18 (all recommendations accepted) and are folded into the Key decisions: Q1 (export, deletion and the two commands in M2) in decision 19; Q2 (`people.contacts.read` to `account-admin` only) in decision 9; Q3
(signal plus repair) in decision 2; Q4 (no `people.directory.read` in the base role) in decision 20; Q5 (immediate, audited self export with a 10-minute cooldown) in decision 10; Q6 (keep consents and `ConsentOf`) in decision 8; Q7 (one legal-name field) in decision 5.

**Raised by the M2 build, answered 2026-10-18 (D37), each as its recommendation.** (1) A language-only `PUT /me` is exempt from the required-fields check (decision 21). (2) The consent audit records purpose and direction (decision 22). (3) `contact_email` is cleared on link (decision 23). (4) Reads of deletion-request notes are audited (decision 24). (5) Data rights for an account holder with no `core.person` row still run the identity and authz steps (decision 25).
