# volunteers

- **Status:** Implemented (2026-10-23; V1 D49, V2 D50, D51); Approved (2026-10-20, D43; D42's answers folded in; the D43 visibility amendment approved 2026-10-20, D44)
- **Amended 2026-10-21 (owner, D46; privacy-tags A2):** every `internal` field of the response types carries `omitempty` (or `omitzero` for a struct or array, so `time.Time` shift bounds and `model.ID`s); the screens treat an absent `internal` count or flag as zero. F126 to F141 unchanged.
- **Amendment V1 (raised 2026-10-21, approved 2026-10-23, D49):** what the build and the reviews found. A shift the caller cannot see is `NotFound` on every method, `Permission` only on a visible one (F77, F81, F100, F110 reworded; F143 to F146); the three long lists (`ListShifts`, `MySchedule`, `ListMembers`) page with an opaque `Cursor` and return `Next` (`Page`, `ShiftList`, `ScheduleList`, `MemberViewList`), `MySchedule` has an upcoming and a past mode (past newest first) and shows a booking someone else canceled until the shift ends (Q-VOL9, owner: yes); a lead may waive a requirement for themself, recorded and flagged like any waiver (Q-VOL7, owner: yes, 2026-10-22; the draft's `Invalid self` rule is withdrawn); legal names stay with `people.contacts.read` holders, so the `legal_name` group loses its `self` option and Proposed change 11 is dropped (Q-VOL8, owner: no); **playa names are unique across the installation and permanent once set, enforced by `people` (Amendment P1; owner 2026-10-22, Q-VOL10 and Q-VOL11 answered)**, so this module keeps no name check of its own and only requires that a person booking any shift has a playa name (decision 26, `Conflict unnamed`; the per-event rule, `name_taken`, `vol.NameKey` and F159, F159a, F159b, F160, F162, F163, F165 are withdrawn); a shift's event is immutable after creation (decision 28); `RecordList.SelfUpload` tells the person whether uploads are on; `ShiftSummary` carries `DepartmentName` and `PositionName`; `VerifyInput`/`RecordInput.NoExpiry` clears an expiry; the reminder re-enqueues itself at the jobs horizon instead of firing early or never; `CopyShifts` keeps wall-clock times in the destination event's zone; the two signal handlers enqueue the on-demand job `volunteers.cancel-shifts`, which cancels in batches; `signup.booked_as` (migration `0002`) keeps who booked after a lead's deletion; list wrappers use `omitzero` so an empty list is `{"items":[]}`; `Verified()` is `verified_at not null`; the UI places Assign in the manage section, opens certificates through `Attachments`, and has a "never expires" switch. The text below is the spec as amended; the record with the choices, the test expectations that change and the owner's questions is "Amendment V1" before Open questions.
- **Amendment V2 (raised 2026-10-23, approved 2026-10-23, D50; rests on jobs Amendment J1):** the reminder's carry-forward past the horizon (decision 25) never happened in the real queue: the handler's `Enqueue` of its own `Key` ran while its row was `running`, which jobs decision 3 made a silent no-op, so a booking more than a year ahead was never reminded; and a shift moved within `DoneTTL` of a sent reminder was not reminded again either. Jobs J1 changes the key rule to "the latest request wins" (a request during the run is held and applied after it; a finished row is reopened), which makes decision 25 and the "reminded once more" sentence true **with no change to this module's code**. What changes here is the proof: F112a asserts the schedule (`jobstest.Fake.Queued()`), F112b drives a far booking across two horizons on a fake clock, F112c does it against the real runner, F151a moves a shift after its reminder was sent. The record is "Amendment V2" before Open questions. **D51 (2026-10-23):** `signup.waived boolean NOT NULL` (migration `0002`) keeps the waiver flag in the export and roster after the waiving lead is deleted (F154a); My volunteering's switch is two toggle buttons in a labelled group, not tabs.
- **Amendment V3 (raised 2026-10-28, Implemented 2026-10-30; Approved 2026-10-30, D57, with the recommended answer to Q-VOL12; the design choice D52 asked for on Proposed change 4):** `shift`, `position` and `qualification` become regional words declared by this module (`Manifest.Terms`, `en` defaults in Go, mirrored by `module.volunteers.term.*` catalogue keys as the shell's boot fallback) and `lead` becomes a core term; one flat term namespace, a key declared by two enabled areas is a start-up `Invalid` whose fix is promoting the word to `CoreTerms`. No service, table, permission or route of this module changes; the record with the options, the mechanism (settings, module-contract, web-shell and app changes: Proposed changes 16 to 19) and Q-VOL12 is "Amendment V3" before Open questions. Q-VOL12 is answered (owner, 2026-10-30, D57: the recommended answer); Proposed changes 16 to 19 are applied. This module's own text below keeps its fixed words until V3 is built.
- **Amendment 2026-10-28 (approved, owner, D53, demo.md Proposed change 7):** `ContributesTo` gains `demoseed.Point`; `Contributions()` returns a `demoseed.Provider` (`demo.go`) that seeds the fictional Ember Flats shifts; section "Amendment 2026-10-28" before Open questions; F171 to F174.
- **Amendment D1 changes (raised 2026-10-28, Approved 2026-10-29, D56; demo.md "Amendment D1"):** the demo table as the build could run it: the lead is `department-admin` at five departments, has no records and holds Khaki by self-waiver; the pending upload is the participant's ICS-100; DPW gains "Inventory Crew"; an event without dates is `Invalid` (F175); the walk-on bookings are fixed. Sub-section under "Amendment 2026-10-28".
- **Amendment V4 (raised 2026-10-30, D57; Approved D58; Implemented 2026-10-30):** the demo walkthrough's four shift-screen findings. The shift page's back link returns to where the person came from (router state `{back: {to, kind}}`, validated, default `/volunteers`) and its label says where; the shift list becomes dense rows grouped by day (times only under the day heading, one line per shift from 640 px, three on a phone) with a new **Only shifts I can take** filter carried by `ShiftQuery.EligibleOnly` (`GET /shifts?eligible=true`, F178 to F181); one fill phrasing, "{booked} of {slots} booked", on the list, the shift page and the lead's tab; the volunteers admin heading is worded by permission ("{term_department_plural} you can manage or view", "All {term_department_plural}" for a site-wide holder). Section "Amendment V4" before Open questions; F176 to F185; Q-VOL13; Proposed change 20. Q-VOL13 is answered (owner, 2026-10-30, D58: the proposed headings); the spec-editor edits of V4.8 are applied (UI table rows, Interface, Behavior "Paging", Summary).
- **Package:** `internal/modules/volunteers` (leaf `volunteers/vol`, `volunteers/postgres`)   **UI:** `web/src/modules/volunteers/`
- **Depends on specs:** `model`, `lib-errs`, `lib-core` (`clock`, `validate`), `db`, `module-contract` (`Manifest`, `Deps`, `PersonData`, `moduletest`), `module-hello` (the shape to copy), `identity` (`PersonFrom`), `authz` (`Allow`, `AllowIn`, `ExtentOf`, starter roles), `people` (`Deps.People`, `contracts/personpage`), `departments-events` (`Deps.Departments`, `Deps.Events`, `contracts/eventsetup`, the two signals), `audit`, `signals`, `notify` (`Deps.Notify`, `Manifest.Templates`), `settings` (`Deps.Settings`), `privacy-tags` (Draft: `privacy.Group`, `Filter`, `Manifest.Views`/`Groups`), `jobs` (Draft: `Deps.Jobs`), `files` (Draft: `Deps.Files`, `ReadUpload`, `Serve`, the per-route body cap), `api` (`httpx`), `web-shell`, `app` (`modules.go` line)   **Used by:** `app` (registry), `people` (person-page section, `PersonData`), `events` (event set-up provider); later `incidents`, `afterreports` and a trust feature through the signals and the capability sketched under "Later"

## Summary

**Purpose.** Staff a department's shifts with volunteers who hold what the position needs. The first feature module (D26, Phase 1). Five parts in one module: a **catalogue of qualifications** (department-specific or org-wide), **qualification records** that only leads verify (D40, D41), **positions** (roles in a department with qualification requirements and an access rule), **membership** (who belongs to a department), **shifts** (blocks of time in a position, usually for an event) with **sign-ups**. Imports `model`, `lib`, `core/module`, `core/identity` (`PersonFrom`, as the `actorFunc` seam), `core/authz` (leaf types), `core/audit` (`Ref`), `core/signals`, `core/notify`, `core/settings`, `core/privacy` (D43), `core/jobs`, `core/files`, `core/people`, `core/departments`, `core/events`, `contracts/personpage`, `contracts/eventsetup`. Never another module, `api`, `app`, `config`.

**Exported (`volunteers`).** `Manifest` (`Name: "volunteers"`); `New(ctx, deps) (module.Module, error)` (no I/O; nil `DB`, `Authz`, `People`, `Departments`, `Events`, `Audit`, `Signals`, `Settings`, `Notify`, `Jobs`, `Files` fail `Invalid`). Permission constants `PermissionSignUp = "volunteers.shifts.signup"` (site), `PermissionShiftsManage`, `PermissionRostersRead`, `PermissionPositionsManage`, `PermissionMembersManage`, `PermissionQualificationsManage`, `PermissionQualificationsRead`, `PermissionQualificationsVerify` (all `ScopeDepartment`). Setting keys `SettingReminderLead = "reminder_lead"`, `SettingCancelCutoff = "cancel_cutoff"`, `SettingExpiryNoticeDays = "expiry_notice_days"`, `SettingSelfUpload = "self_upload"`, `SettingVisibilityShiftMates = "visibility_shift_mates"`, `SettingVisibilityLegalName = "visibility_legal_name"` (the two `privacy` groups' settings, D43), `SettingRetention = "retention"`. `Manifest.Groups = vol.Groups`, `Manifest.Views` = every `vol` response type (privacy-tags). Signals `SignalSignupBooked = "volunteers.signup-booked"`, `SignalSignupCanceled = "volunteers.signup-canceled"`, `SignalQualificationVerified = "volunteers.qualification-verified"`; payload keys `signup`, `shift`, `person`, `department`, `record`, `qualification`. Jobs `JobShiftReminder = "volunteers.shift-reminder"` (deferred), `JobExpiryScan = "volunteers.expiry-scan"` (daily), `JobPurge = "volunteers.purge"` (daily), `JobCancelShifts = "volunteers.cancel-shifts"` (on demand, from the two signal handlers). Templates `volunteers.shift-reminder`, `volunteers.qualification-expiring`, `volunteers.qualification-verified`, `volunteers.qualification-rejected`, `volunteers.signup-removed`. Section `volunteers.summary` on the person page; an `eventsetup.Provider` that copies a previous event's shift grid within the caller's extent.
**`vol` (leaf):** `PositionID`, `QualificationID`, `RecordID`, `MemberID`, `ShiftID`, `SignupID` (= `model.ID[...]`); `Date{Year, Month, Day}` (civil date, JSON `YYYY-MM-DD`); `Access` (`AccessOpen`, `AccessMembers`); `Qualification`, `Record` (+`Verified()`, `ValidOn(Date)`), `Position` (+`Requirements []QualificationID`, `HideShiftMates bool`), `Member`, `Shift` (+`Canceled()`, `Times() model.TimeRange`), `Signup` (+`WaivedBy`), `SignupStatus` (`Booked`, `Canceled`), `CancelReason` (`self`, `lead`, `shift_canceled`, `department_archived`, `event_closed`); the `*Input` and `*Query` types (`VerifyInput` and `RecordInput` gain `NoExpiry bool`; `ShiftQuery` (gains `EligibleOnly bool`, V4, D58), `ScheduleQuery` and the new `MemberQuery` embed `Page{Limit int; After Cursor}`; `Cursor` is an opaque string, `Invalid malformed` when it does not parse) and the view types `ShiftSummary{Shift, DepartmentName, PositionName, Booked, Mine, Eligibility, Mates *ShiftMates}` (the two names), `ShiftMates{Names []string, Unlisted int}` (playa names only), `Eligibility{Eligible bool, Missing []QualificationID, Reason string}`, `RosterEntry{Signup; PlayaName; LegalName *string; Eligible}`, `ScheduleEntry`, `MemberView`, and the list wrappers `RecordList{Items, SelfUpload *bool}`, `ShiftList{Items, Next Cursor}`, `ScheduleList{Items, Next}`, `MemberViewList{Items, Next}` (`items` is `omitzero` over a non-nil slice, so an empty list is `{"items":[]}`); every type has `Validate()`; every field that reaches the wire carries a `json` and a `privacy` tag (D43), and `Groups = []privacy.Group{{Name: "shift_mates", Permission: PermissionSignUp, Default: holders, Options: nobody|booked|holders|signed_in, Extra: booked}, {Name: "legal_name", Permission: "people.contacts.read", Default: holders, Options: nobody|holders}}` (`self` removed, Q-VOL8). Constants `MaxNameBytes = 100`, `MaxDescriptionBytes = 2000`, `MaxLabelBytes = 100`, `MaxLocationBytes = 200`, `MaxSlots = 200`, `MaxShiftDuration = 48h`, `MaxValidityDays = 3650`, `MaxRequirements = 20`, `MaxList = 500`, `MaxIDs = 200`.
**Service interfaces** (all on `*Service`, safe for concurrent use, nothing to close): `Qualifications` (`ListQualifications`, `CreateQualification`, `UpdateQualification`, `ArchiveQualification`), `Records` (`RecordsOf`, `RecordQualification`, `UploadCertificate`, `VerifyRecord`, `RejectRecord`, `RemoveRecord`, `PendingRecords`, `OpenCertificate`), `Positions` (`ListPositions`, `CreatePosition`, `UpdatePosition`, `ArchivePosition`, `SetRequirements`), `Members` (`ListMembers`, `AddMember`, `RemoveMember`, `MyDepartments`, `Leave`), `Shifts` (`ListShifts`, `GetShift`, `CreateShift`, `UpdateShift`, `CancelShift`, `CopyShifts`), `Signups` (`SignUp`, `Assign`, `CancelSignup`, `Roster`, `MySchedule`, `Listed`, `SetListed`); plus `PersonData`, `Sections` (personpage), `SetUp` (eventsetup). `RecordsOf` returns `RecordList`, `ListShifts` `ShiftList`, `MySchedule` `ScheduleList`, `ListMembers(ctx, MemberQuery)` `MemberViewList`.

**Key decisions.** 1 a lead is whoever holds the department-scoped permission at that department; the starter roles seed every lead permission into `department-admin` only, never `participant`, which is how D41 ("only department leads verify") is kept as data a regional cannot loosen by accident. 2 a qualification record is verified by a lead or it does not count: a self-upload creates a `pending` record (`verified_by` null) and only `VerifyRecord` makes it count; there is no self-attestation path; self-upload is on by default (`self_upload = true`, D42). 3 a department-specific qualification is verified with `AllowIn(verify, q.Department)`; an org-wide one with `ExtentOf(verify)` not zero (any lead anywhere, D41). 4 a position's requirements are a set of qualifications; a person is eligible for a shift when every requirement has a verified record valid on the shift's start date in the shift's zone; age limits are a qualification ("Over 18", org-wide, indefinite), not a profile field or code. 5 a position's `access` is `open` (anyone eligible) or `members` (department members only); membership is a row a lead adds or a person leaves; join requests are Later. 6 a shift belongs to one position and optionally one event (`event_id` null = standing work such as winter inventory, Q-CORE10) and always carries an IANA zone (copied from the event, required otherwise). 7 bookings are serialised per person then per shift (advisory lock then `FOR UPDATE`), so slots and the no-overlap rule hold under concurrency; a person's booked shifts never overlap (`model.TimeRange.Overlaps`); a lead may `Assign` someone who lacks a requirement only with `Waive: true`, which records `waived_by`, audits `Reason waived` and flags the person on the roster; `SignUp` never waives (D42). 8 the shift reminder is a deferred job keyed by the sign-up ID, so a changed shift time moves it and the handler re-reads state; the expiry notice is a daily scan deduped by a column, not by notify's outbox. 9 the certificate is a `files` upload on the record (`Ref{Kind: "volunteers.qualification-record"}`, subject the person, level `personal`), served from this module's route after its own check; the record itself is `internal` (D42). 10 (D42, reshaped by D43) the full roster (person IDs, eligibility, waivers, links to the person page) is for `rosters.read` holders; volunteers see **who else is on a shift by playa name only** (`ShiftSummary.Mates` on `GetShift`, never a legal name, pronoun, contact or ID), as the `privacy` group `shift_mates` whose audience the regional sets in `visibility_shift_mates` (`nobody`; `booked`: only once the viewer holds a booking on that shift; `holders`: anyone who may volunteer, that is holders of `volunteers.shifts.signup`, the default per D43; `signed_in`: anyone who can see the shift), a position's `HideShiftMates` tightening it, and a person's own choice not to be listed (`SetListed(false)`): a person with no playa name yet, or unlisted, is counted in `Unlisted`, never named; these reads are `internal` and not audited. 17 (D43) "anyone who volunteers" is a holder of `volunteers.shifts.signup`, not a person with a booking: the list has to be visible before the first booking for the owner's reason to work, and a regional that restricts who may volunteer already restricts that permission. 18 (D43) legal names on the roster are the `privacy` group `legal_name` (`RosterEntry.LegalName`, `personal`, audience set in `visibility_legal_name`, default `holders` of `people.contacts.read` covering the shift's department), loaded through `Deps.People.LegalNamesOf` (people Proposed change, privacy-tags 3) only when the policy shows them, and audited twice by design: people's `people.person.read` for its data and this module's `volunteers.roster.read` for the roster; a new starter role `volunteer-coordinator` (authz, privacy-tags Proposed change 2) holds `rosters.read`, `qualifications.read` and people's two read permissions, so a fresh installation has the role the owner named. 19 (D43) the per-person opt-out stays: the owner's scenario has two sides (someone avoiding a person also has reason not to be found by them), the count still tells the viewer someone is withheld, and the lead's roster is unaffected. 11 sign-ups never carry to a new event; shifts do, through `events.event-setup` (within the caller's `shifts.manage` extent) and a lead's `CopyShifts`, idempotent through `copied_from`. 12 archiving a department or closing an event cancels its future shifts and their bookings from a signal handler (system actor, idempotent on state), telling each person once. 13 retention is one key, `volunteers.retention` (0 = forever, D30), enforced by `volunteers.purge` on ended shifts' sign-ups, canceled shifts and long-expired records (with their files); memberships are current state and are never purged. 14 no approval step on sign-up, no check-in or hours credit, no swaps, no anchored shifts, no capability yet: all Later. 15 regional differences are the seven settings (two of them visibility audiences, D43), the catalogue, the positions and the roles. 16 the org-wide catalogue is managed site-wide (`qualifications.manage` seeded into `department-steward`); a department's entries by that department's `department-admin` (D42). Every `internal` response field carries `omitempty`/`omitzero` and the screens read an absent count or flag as zero (D46). 20 every method that takes a shift ID applies the visibility rule first: an invisible or unknown shift is `NotFound`, a visible one the caller may not act on is `Permission`; qualifications, positions and departments are visible to every signed-in person, so their write methods answer `Permission` and only an unknown ID is `NotFound`. 21 withdrawn (Q-VOL7, owner 2026-10-22: yes): a lead may waive a requirement for themself; `Assign` with `Person == actor` and `Waive` records `waived_by` = the actor, audits `Reason waived` and flags the roster exactly as for anyone else; nothing distinguishes a self-waiver except that the two IDs match. 22 long lists page by keyset `(starts, id)` (members: `(created_at, id)`) behind an opaque `Cursor`; a page may be short (rows dropped after the fetch) but `Next` never skips a row. 23 the two handlers enqueue `volunteers.cancel-shifts` (key `<reason>:<department or event ID>`) and the job cancels in batches of `MaxList` shifts per transaction, so handlers stay quick and no transaction grows with the department. 24 `CopyShifts` keeps each shift's wall-clock start and end in the destination event's zone, shifted by whole days (the civil difference of the events' start dates), so a copy across a DST change keeps "08:00". 25 the reminder horizon: a reminder due beyond `jobs.MaxDelay - 24h` is enqueued at the horizon and the handler re-enqueues itself (same `Key`) while `Starts - lead` is still ahead, so a far booking or a far-moved shift is never reminded early or never; this re-enqueue happens inside the handler's own run, so it rests on jobs decision 23 (a request for a `running` key is held and applied when the run ends), not on decision 3's queued-row move, and the tests prove it against a queue that keeps row state (F112a, F112b, F112c). 26 (owner 2026-10-22, Q-VOL10 and Q-VOL11) **playa names are unique across the whole installation and permanent once set, and `people` enforces it** (Amendment P1: a unique index on a name key, `Conflict taken` on `UpdateMe`/`Create`, the name read-only once set), so two people booked anywhere never share a name and this module keeps no name check, no name lock and no name comparison of its own; what it keeps is the precondition that **a person booking any shift, event or standing, has a playa name**: `SignUp` and `Assign` read `Deps.People.NamesOf([person])` inside the booking transaction and answer `Conflict unnamed` when the entry is missing or `""`, with or without `Waive` (a waiver is about qualifications; the name is set in people, by the person or by a lead with `people.profiles.manage`); the lock order is person → shift (decision 7). 27 (Q-VOL8: no) legal names stay with `people.contacts.read` holders: the `legal_name` group's options are `nobody` and `holders`; `self` is removed because, without people answering a non-holder, it could only ever show a holder their own name, which `holders` already does; the service asks people only when the viewer holds the permission somewhere. 28 (kept after the name rule moved to people) a shift's event is immutable after creation (`UpdateShift` answers `Invalid scope` on `event`): the zone and the dates every booking was judged against follow the event (decision 6), `events.event-closed` cancels by event (decision 12) and `CopyShifts` dedupes by `copied_from` (decision 11), so a moved shift would carry bookings, a zone and a copy record into an event none of them were checked for; a lead cancels and recreates, or copies.

**Errs kinds a caller handles.** Every use case: `Unauthenticated` (no actor), store kinds (`Unavailable`, `Timeout`, `Canceled`), `Unavailable` wrapping `db.ErrReadOnly` on writes. `Permission` (an action on a record the caller may see). `NotFound` (unknown ID, or one outside the caller's extent; never `Permission` for an invisible record). `Invalid` with field codes `required`, `too_long {max}`, `control_chars`, `range`, `malformed`, `one_of`, `too_many {max}`, `unknown`, `self`, `scope`, `hidden`. `Conflict` with codes `taken`, `archived`, `canceled`, `closed`, `full`, `overlap {shift}`, `unqualified {missing}`, `not_member`, `started`, `ended`, `cutoff`, `duplicate`, `status`, `in_use`, `pending_exists`, `unnamed` (the person booking has no playa name yet; a name clash cannot reach this module because people refuses it at the profile, P1). `Unsupported` (self-upload off; notify unconfigured is swallowed, never returned). `files`' kinds pass through `UploadCertificate` and `OpenCertificate` unchanged.

**Owns.** Schema `volunteers`: tables `qualification`, `qualification_record`, `position`, `position_requirement`, `member`, `shift`, `signup` (`booked_as`, `waived`, migration `0002`), `unlisted`. Eight permissions, seven settings (two of them `visibility_*` from the two `privacy` groups), three signals published, two subscribed (`departments.department-archived`, `events.event-closed`), four jobs , five templates, one person-page section, one event-setup contribution. Audit actions `volunteers.<record>.<verb>` listed under Behavior. Routes under `/api/volunteers/` and screens under `/volunteers/` listed under Interface and UI. Catalogue keys `module.volunteers.*`. Terms `shift`, `position`, `qualification` (`Manifest.Terms`, V3, D57; `lead` is a core term).

**Expects of dependencies.** `jobs` (Approved, with Amendment J1, D50): `Deps.Jobs jobs.Registrar`, `Register` in `Start`, `Enqueue` joining the transaction, `InJob`; `MaxEvery >= 24h`; and jobs decision 23, "the latest request for a `Key` wins": an `Enqueue` of the sign-up's `Key` from inside the reminder's own run is held and applied when the run ends, and one against a finished row reopens it; without that rule the reminder never fires for a booking beyond `ReminderHorizon` and a shift moved within `jobs.DoneTTL` of a sent reminder is not reminded again. `files` (Approved): `Deps.Files files.Store`, `Stage` outside the caller's transaction and `Attach` inside its `Write` transaction (with a deferred `Discard`), `ByRecord`, `Open` auditing a `personal` read, `DeleteByRecord`, `ReadUpload`, `Serve`, `module.Route.MaxBodyBytes`/`Timeout` (files Proposed change 1), `Accept` images and PDF. `authz`: `ExtentOf` for every list, `AllowIn` for one department's record. `people`: `NamesOf`, `people.directory.read` seeded to `department-admin` (leads search people to add members and record qualifications); `LegalNamesOf` (people.md, D44: `people.contacts.read` in any extent, audited per person; `peopletest.Fake.AddLegalName`; Q-VOL8: no change asked of people; this module calls it only for a viewer who holds the permission somewhere, F137, F138); `NamesOf([person])` inside the booking transaction for decision 26 (one ID; a missing entry or `""` means no playa name); **Amendment P1** (Draft, in parallel): playa names unique across the installation (`people.NameKey`, a unique index, `Conflict taken` on `UpdateMe`/`Create`) and read-only once set, which is what lets this module show shift-mates and rosters by playa name alone with no clash check of its own (Proposed change 14); this module uses none of P1's exported names. `privacy-tags` (Approved, D44): `Group`, `Settings`, `Check`, `PolicyOf`, `ViewerOf`, `Filter`, `Policy.Shows`, `Manifest.Views`/`Groups` through module-contract. `authz`: the `volunteer-coordinator` starter role (D44, migration `0003`; `Seed` applies the key at the first `bms migrate` after the role exists). `departments`/`events`: `Active`, `NamesOf`, `InfoOf` (`Zone`, `Dates`, `Closed`), the two signals, `GET /api/departments` and `GET /api/events` for pickers. `notify`: scoped `Send`, `Unsupported` treated as no e-mail. `settings`: a scoped `Reader`. `audit`: a `Recorder` scoped `volunteers`. `web-shell`: `useFormat` in the shift's zone, `FileField`/`Attachments` (files Proposed change 7), and (V3, D57) `termsOf` over `module.<name>.term.*`. `settings`: also `TermDecl`, `Area.Terms`, `TermsFor` over declared terms; `module-contract`: `Manifest.Terms`.

**Open questions:** Q-VOL1 to Q-VOL5 are closed by D42, Q-VOL6 by D43 (2026-10-20; decisions 17 to 19); Q-VOL7 (self-waiver: yes), Q-VOL8 (own legal name under `self`: no, everyone sees playa names, legal names stay with holders; and playa names must be unique) and Q-VOL9 (a canceled booking stays on the schedule: yes) answered by the owner 2026-10-22 and folded in (decisions 21 withdrawn, 26, 27, 28). Q-VOL10 and Q-VOL11 (owner, 2026-10-22): no duplicate playa names anywhere, no renaming into a clash, names permanent and self-appointed for now; retirement, approval pipelines and several names per person are Later: all carried by people Amendment P1, so this module's per-event rule is withdrawn (decision 26 reduced to `unnamed`). Q-VOL12 (V3: which words become regional terms; `lead` as a core term) is answered as proposed (owner, 2026-10-30, D57). Q-VOL13 (V4, Draft: the wording of the volunteers admin heading) awaits the owner. **Proposed changes elsewhere:** 13 (`burn-domain` sentences) applied 2026-10-23 and 14 (the dependency on people P1) satisfied by D48; 11 and 12 stay dropped (2026-10-22: Q-VOL8 no; P1 owns `NameKey`); 1 to 10 applied 2026-10-20 (D44) except 4 (settings terms), whose design choice is Amendment V3 and whose edits are 16 to 19 (Draft), and the `burn-domain` skill additions in 7 and 10 (main session).

**V4 amendment (2026-10-30, D58).** Shift navigation and a compact list, all in the browser except one query field: `ShiftQuery.EligibleOnly` (`GET /shifts?eligible=true`; keeps eligible and held shifts, scans up to `MaxList + 1` rows, `Next` from the last row consumed; F178 to F181, Invalid `malformed` on `eligible`); the shift page's back link returns to where the person came from (router state `{back: {to, kind}}`, validated, default `/volunteers`; web-shell "Data" records the rule); the shift list is dense rows grouped by day with times only and the fill count "{booked} of {slots} booked" everywhere (key `module.volunteers.fill`); the admin page heads its list by what the person can do ("All {term_department_plural}" for a site-wide holder, "{term_department_plural} you can manage or view", empty "No {term_department} you can manage or view yet."; Q-VOL13 answered, D58). No table, permission, signal, setting or job changes. Also (D58, jobs J-W1): the catalogue gains `module.volunteers.job.<ident>.title` for the four registered jobs (UI section).

**D43 amendment (2026-10-20).** The regional sets visibility through `privacy` groups: `visibility_shift_mates` replaces `show_shift_mates`; `visibility_legal_name` and `RosterEntry.LegalName` are new; every `vol` response type is tagged and listed in `Manifest.Views`; F126 to F128 are reworded and F134 to F141 added (`SetListed`'s row is now F142); the `volunteer-coordinator` starter role is named in `StarterRoles`.

## Purpose

Staff a department's shifts with volunteers who hold what the position needs.

## Responsibilities

- The catalogue of qualifications a regional recognises: each department-specific or org-wide, with a name, an optional default validity, archivable.
- Qualification records: which person holds which qualification, acquired on, expires on (or never), verified by which lead and when; the certificate copy a person may upload for a lead to verify; the pending queue; expiry notices.
- Positions within a department, each with its qualification requirements and an access rule.
- Membership: who belongs to a department, added by a lead, left by the person.
- Shifts: a block of time in a position, for an event or for standing work, with slots, a location and notes; creating, changing, canceling and copying them between events.
- Sign-ups: a person booking a slot they are eligible for, a lead assigning someone (with an audited waiver when a requirement is missing), cancellation by either, the roster per shift (with legal names for the audience the regional sets), who else is on a shift by playa name for the audience the regional sets, a person's choice not to be listed, each person's own schedule, the shift reminder; requiring a playa name before any booking (decision 26; uniqueness is people's, P1).
- The person-page section, the event set-up contribution, export and deletion of one person's data, the retention purge.

## Not responsible for

| Concern | Owner |
|---|---|
| Who the departments and events are; a department's leadership | `departments-events`; leadership is an authz grant of `department-admin` (09 §5) |
| Deciding permissions | `authz` (`Allow`, `AllowIn`, `ExtentOf`); this spec declares names and scopes |
| Names, pronouns, contact details, emergency contacts | `people` (`NamesOf`, `LegalNamesOf`; the roster links to the person page) |
| Deciding which fields of a response a viewer gets, from the `privacy` tags and the `visibility_*` settings | `privacy-tags` (`Filter`); this module tags its types, declares its two groups and calls `Filter` in every use case that returns a response type |
| Storing, stripping, hashing and serving certificate bytes | `files`; this module checks the permission and calls `Serve` |
| Sending e-mail, the outbox | `notify` |
| Running the reminder and the daily scans when due | `jobs` |
| Recording who read or changed what | `audit`, through the scoped `Recorder` |
| Verifying training by software, a quiz, reading a manual, questionnaires | nobody: leads verify by hand (D40); online steps are Later |
| Approving a sign-up by hand before it counts, swaps between volunteers, walk-on desks | Later (a regional difference the manuals describe; see "Later") |
| Check-in and check-out, hours credit, meal or ticket credit, "active/inactive/retired" status | Later; credit records are the dated records D18 and Q-TRUST need |
| Shifts anchored to a schedule item ("perimeter starts 90 minutes before `temple-burn`") | Later, with `events.Directory.ScheduleOf` (departments-events decision 10) |
| Pairing on the day, shift logs, radio handles, laminates | Later modules or paper |
| Incidents on a shift, after-burn reports | `incidents`, `afterreports` |
| A "which positions does person X qualify for" service for other modules | Later: the `qualifications` capability sketched under "Later" (Q-QUAL, D19); declared when a consumer exists |
| Terminology for "shift", "position", "qualification" as regional words | Declared in `Manifest.Terms` (V3, D57); `lead` is a core term (`settings.CoreTerms`) |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `qualifications` | Keep the catalogue of qualifications a regional recognises. | `volunteers.qualification`; org-wide (`department_id` null) or one department's |
| `records` | Hold who holds which qualification, as verified by a lead. | `volunteers.qualification_record`; pending until verified; certificate through `files` |
| `positions` | Define the roles within a department with their requirements. | `volunteers.position`, `position_requirement` |
| `members` | Keep who belongs to a department. | `volunteers.member`; powers `members`-only positions and the person-page section |
| `shifts` | Keep the blocks of time a department needs filled. | `volunteers.shift`; event or standing work; copyable between events |
| `signups` | Book people into shifts within the rules, and show rosters and schedules. | `volunteers.signup`, `volunteers.unlisted`; the reminder job; the shift-mate list; the playa-name precondition on booking, decision 26 |

The six share one `*Service`, one schema and one manifest because they are one feature a lead runs from one screen; they are separate interfaces so that handlers, the jobs, the providers and the tests take only what they use.

## Interface

```go
package vol // internal/modules/volunteers/vol: record types shared by the service and postgres/

type position struct{}; type qualification struct{}; type record struct{}
type member struct{}; type shift struct{}; type signup struct{}
type PositionID = model.ID[position]
type QualificationID = model.ID[qualification]
type RecordID = model.ID[record]
type MemberID = model.ID[member]
type ShiftID = model.ID[shift]
type SignupID = model.ID[signup]

// Date is a civil date (no zone). JSON "YYYY-MM-DD". Validate fails errs.Invalid
// (code "malformed") for an impossible date; the zero Date means "none".
type Date struct{ Year int; Month time.Month; Day int }
func DateOf(t time.Time, loc *time.Location) Date
func (d Date) IsZero() bool
func (d Date) Before(o Date) bool
func (d Date) AddDays(n int) Date
func (d Date) Validate() error

type Access string
const (
	AccessOpen    Access = "open"    // anyone eligible may sign up
	AccessMembers Access = "members" // department members only
)

type Qualification struct {
	ID          QualificationID
	Department  model.DepartmentID // zero = org-wide
	Name        string             // 1..MaxNameBytes, trimmed, no control characters
	Description string             // 0..MaxDescriptionBytes
	ValidityDays int               // 0 = indefinite by default; else 1..MaxValidityDays, prefills ExpiresOn
	ArchivedAt  time.Time          // zero = active
	CreatedAt, UpdatedAt time.Time
}
type QualificationInput struct {
	Department   model.DepartmentID // zero = org-wide; immutable after creation
	Name, Description string
	ValidityDays int
}
type QualificationQuery struct {
	Department      model.DepartmentID // zero = org-wide ones plus every department the caller may see
	IncludeArchived bool               // needs qualifications.manage in that extent
}

type Record struct {
	ID            RecordID
	Qualification QualificationID
	Person        model.PersonID
	AcquiredOn    Date
	ExpiresOn     Date           // zero = indefinite
	VerifiedBy    model.PersonID // zero = pending
	VerifiedAt    time.Time
	Certificate   files.FileID   // zero = none
	CreatedAt, UpdatedAt time.Time
}
func (r Record) Verified() bool      // !VerifiedAt.IsZero(): VerifiedBy may be zeroed by that lead's deletion, the date stays
func (r Record) ValidOn(d Date) bool // Verified() && (ExpiresOn.IsZero() || !ExpiresOn.Before(d))

type RecordInput struct { // a lead records from their own training records (D40 way (a))
	Qualification QualificationID
	Person        model.PersonID
	AcquiredOn    Date
	ExpiresOn     Date // zero = the catalogue default (AcquiredOn + ValidityDays), or indefinite
	NoExpiry      bool // true = indefinite even when the catalogue has a validity; with ExpiresOn set: Invalid one_of
}
type UploadInput struct { // a person uploads a certificate (D40 way (b))
	Qualification QualificationID
	AcquiredOn    Date // what the certificate says; the lead may correct it
	ExpiresOn     Date
	Upload        *files.Multipart // the file; Accept = images and PDF
}
type VerifyInput struct {
	AcquiredOn, ExpiresOn Date // zero = keep what the person entered
	NoExpiry bool              // true = clear the expiry (the certificate never expires); with ExpiresOn set: Invalid one_of
}
type PendingQuery struct{ Department model.DepartmentID } // zero = every pending record the caller may verify; a department includes the org-wide pending records too (D41: its leads may verify them)
// RecordList is the body of GET /people/{person}/qualifications . SelfUpload is present
// only when the person is the actor: whether UploadCertificate is on (self_upload), so the
// My volunteering screen can show or hide the upload action without settings.read.
type RecordList struct {
	Items      []Record `json:"items,omitzero" privacy:"internal"`
	SelfUpload *bool    `json:"self_upload,omitempty" privacy:"internal"`
}

type Position struct {
	ID           PositionID
	Department   model.DepartmentID
	Name, Description string
	Access       Access
	Requirements []QualificationID // at most MaxRequirements, sorted
	HideShiftMates bool            // true = volunteers never see who else is on this position's shifts, whatever the setting (D42)
	ArchivedAt   time.Time
	CreatedAt, UpdatedAt time.Time
}
type PositionInput struct{ Name, Description string; Access Access; HideShiftMates bool }

type Member struct {
	ID         MemberID
	Department model.DepartmentID
	Person     model.PersonID
	AddedBy    model.PersonID // zero after that person was deleted
	CreatedAt, UpdatedAt time.Time
}
type MemberView struct {
	Member
	PlayaName string
	Eligible  []PositionID // active positions of the department the person is eligible for today
}
// Paging shared by the three long lists. Cursor is opaque to clients: base64url of
// "<sort key RFC3339Nano>|<id>" ((starts, id) for shifts and schedules, (created_at, id) for
// members); a cursor that does not decode, or whose ID is not a valid model.ID, is Invalid
// malformed on "after". The service fetches Limit + 1 rows: when the extra row exists, Next
// is the cursor of the last returned row, else zero (absent). Next is computed from the
// fetched rows before any Go-side filtering, so a page may be shorter than Limit but a
// cursor never skips a row.
type Cursor string
func (c Cursor) IsZero() bool
type Page struct {
	Limit int    // 1..MaxList, default 100
	After Cursor // zero = the first page
}
func (p Page) Validate() error
type MemberQuery struct {
	Department model.DepartmentID
	Page
}
type MemberViewList struct {
	Items []MemberView `json:"items,omitzero" privacy:"internal"`
	Next  Cursor       `json:"next,omitempty" privacy:"internal"`
}

type Shift struct {
	ID         ShiftID
	Department model.DepartmentID
	Position   PositionID
	Event      model.EventID // zero = standing work
	Label      string        // 1..MaxLabelBytes ("Morning dirt")
	Starts, Ends time.Time   // instants; Ends > Starts; at most MaxShiftDuration
	Zone       string        // IANA; the event's when Event is set
	Slots      int           // 1..MaxSlots
	Location   string        // 0..MaxLocationBytes, operational ("Ranger HQ")
	Notes      string        // 0..MaxDescriptionBytes; never personal data (the form says so)
	CanceledAt time.Time
	CancelReason CancelReason
	CopiedFrom ShiftID
	CreatedBy  model.PersonID
	CreatedAt, UpdatedAt time.Time
}
func (s Shift) Canceled() bool
func (s Shift) Times() model.TimeRange
type ShiftInput struct {
	Position     PositionID
	Event        model.EventID // immutable on UpdateShift: must equal the shift's, else Invalid scope (decision 28)
	Label        string
	Starts, Ends time.Time
	Zone         string // ignored when Event is set (the event's zone is used)
	Slots        int
	Location, Notes string
}
type ShiftQuery struct {
	Department      model.DepartmentID // zero = all the caller may see
	Event           model.EventID      // zero = any, including standing work
	Position        PositionID
	Within          model.TimeRange    // zero = from now on; either bound alone is allowed: Start only = from then on, End only = from now until then; Start >= End is Invalid range
	IncludeCanceled bool               // needs shifts.manage in that extent
	// EligibleOnly (V4, D58) keeps only the shifts the actor is eligible for and the shifts the
	// actor holds a booking on; the rest are dropped after the fetch, so a page may be short
	// (decision 22, Behavior "Paging"). Needs no permission beyond seeing the shift. Query
	// parameter `eligible` (a boolean, parsed like `canceled`).
	EligibleOnly bool
	Page                               // replaces Limit; order (starts, id)
}
// ShiftList is the body of GET /shifts .
type ShiftList struct {
	Items []ShiftSummary `json:"items,omitzero" privacy:"internal"`
	Next  Cursor         `json:"next,omitempty" privacy:"internal"`
}
// Every vol type that reaches the wire carries json and privacy tags (privacy-tags, D43);
// the tags are shown here only where a field is grouped or above internal. Everything
// else in this module is `privacy:"internal"`. Every internal field carries the omit
// option its kind needs (D46): `omitempty`, or `omitzero` for a struct or array (a
// `time.Time` shift bound, a `model.ID`), so a hidden field is absent, never a
// placeholder; the screens read an absent internal count or flag as zero. Person IDs that the caller does not
// need are `json:"-"`.
type ShiftSummary struct {
	Shift
	DepartmentName string   `json:"department_name,omitempty" privacy:"internal"` // from Deps.Departments.NamesOf, one batched call per list; "" when the directory does not know it
	PositionName   string   `json:"position_name,omitempty" privacy:"internal"`   // from the position join
	Booked      int         `json:"booked,omitempty" privacy:"internal"`
	Mine        SignupID    `json:"mine,omitzero" privacy:"internal"` // the caller's booked sign-up, or zero
	Eligibility Eligibility `json:"eligibility,omitzero" privacy:"internal"`
	Mates       *ShiftMates `json:"mates,omitempty" privacy:"internal,group=shift_mates"` // who else is on the shift; GetShift only; absent when the policy hides it (see Behavior)
}
// ShiftMates is what a volunteer may see of the others booked on a shift: playa names only.
// Never a legal name, pronouns, a contact detail or a person ID. Cannot be invalid.
type ShiftMates struct {
	Names    []string `json:"names,omitempty" privacy:"internal"` // playa names of the other booked people, sorted; never repeats (people P1: names are unique across the installation)
	Unlisted int      `json:"unlisted,omitempty" privacy:"internal"` // booked people shown by no name: no playa name yet, or chose not to be listed
}

// Groups are this module's privacy groups (privacy-tags): the regional sets each one's
// audience in visibility_<name>. GroupShiftMates guards ShiftSummary.Mates; "anyone who
// volunteers" (D43) is a holder of volunteers.shifts.signup, and "booked" is the extra
// audience the service asserts when the viewer holds a booking on that shift.
// GroupLegalName guards RosterEntry.LegalName: holders are holders of people.contacts.read
// covering the shift's department. (Q-VOL8: no) Its options are nobody and holders only:
// legal names stay with permission holders, so a "self" audience could show nothing that
// "holders" does not (decision 27). Manifest.Settings carries privacy.Settings(Groups).
const ( GroupShiftMates = "shift_mates"; GroupLegalName = "legal_name"; AudienceBooked privacy.Audience = "booked" )
var Groups = []privacy.Group{
	{Name: GroupShiftMates, Permission: PermissionSignUp, Default: privacy.AudienceHolders,
		Options: []privacy.Audience{privacy.AudienceNobody, AudienceBooked, privacy.AudienceHolders, privacy.AudienceSignedIn},
		Extra: []privacy.Audience{AudienceBooked}},
	{Name: GroupLegalName, Permission: "people.contacts.read", Default: privacy.AudienceHolders,
		Options: []privacy.Audience{privacy.AudienceNobody, privacy.AudienceHolders}},
}
type Eligibility struct {
	Eligible bool
	Missing  []QualificationID // requirements without a valid record
	Reason   string            // "", "not_member", "full", "started", "canceled", "closed", "archived", "overlap"
	// When the viewer already holds a booking on the shift, Eligible is false with
	// Reason "" and ShiftSummary.Mine is set: the screen shows "Booked", not a reason.
}

type SignupStatus string
const ( SignupBooked SignupStatus = "booked"; SignupCanceled SignupStatus = "canceled" )
type CancelReason string
const (
	CancelSelf CancelReason = "self"; CancelLead CancelReason = "lead"
	CancelShiftCanceled CancelReason = "shift_canceled"
	CancelDepartmentArchived CancelReason = "department_archived"
	CancelEventClosed CancelReason = "event_closed"
)
type Signup struct {
	ID         SignupID
	Shift      ShiftID
	Person     model.PersonID
	Status     SignupStatus
	BookedBy   model.PersonID // zero = the person themself, or a lead since deleted (see BookedAs)
	BookedAs   CancelReason   // "self" or "lead": who booked, kept after that lead's deletion (the export's role word)
	WaivedBy   model.PersonID // the lead who assigned despite a missing requirement (D42); zero otherwise, and zero after that lead is deleted
	Waived     bool           // a requirement was waived (D51): true from the booking and kept when WaivedBy is nulled
	CanceledAt time.Time
	CanceledBy model.PersonID // zero = the system (a signal handler)
	CancelReason CancelReason
	CreatedAt, UpdatedAt time.Time
}
type AssignInput struct{ Person model.PersonID; Waive bool } // Waive: book even if unqualified; recorded and audited (D42). (Q-VOL7: yes) Person == actor is allowed with or without Waive: a lead books themself like anyone, until the shift ends, and a self-waiver is recorded (WaivedBy = the actor), audited and flagged exactly as any other. Waive never overrides decision 26 (unnamed): the name is set in people, not waived here
type RosterEntry struct { // Signup.Waived (D51) flags a waiver
	Signup
	PlayaName string  `json:"playa_name,omitempty" privacy:"internal"`
	LegalName *string `json:"legal_name,omitempty" privacy:"personal,group=legal_name"` // D43; absent unless the policy shows it and people knows one
	Eligible  bool    `json:"eligible,omitempty" privacy:"internal"`
}
type Roster struct{ Shift Shift; Entries []RosterEntry }
// ScheduleQuery has two modes. Past false (default): bookings whose shift has not
// ended, soonest first, including sign-ups someone other than the person canceled (reason
// lead, shift_canceled, department_archived, event_closed) so a removal is seen even
// without e-mail (Q-VOL9); the person's own cancellations are not listed. Past true: booked
// sign-ups whose shift has ended, newest first (so the latest work is on the first page).
// Within narrows either mode; either bound alone is allowed.
type ScheduleQuery struct {
	Within model.TimeRange
	Past   bool // replaces IncludePast
	Page        // order (starts, id) ascending for upcoming, descending for past
}
type ScheduleEntry struct {
	Signup
	Shift      Shift  `json:"shift,omitzero" privacy:"internal"`
	Department string `json:"department_name,omitempty" privacy:"internal"`
	Position   string `json:"position_name,omitempty" privacy:"internal"`
}
type ScheduleList struct { // the body of GET /me/schedule 
	Items []ScheduleEntry `json:"items,omitzero" privacy:"internal"`
	Next  Cursor          `json:"next,omitempty" privacy:"internal"`
}
```

```go
package volunteers

// Manifest declares the module; New builds the service over deps (no I/O). Fails
// errs.Invalid when a needed dependency is nil (F1); the service never degrades to
// "no permission check" (hello's TestNew_NilAuthz_DeniesEverything rule).
var Manifest = module.Manifest{ /* Name, Migrations, Templates, Permissions, Settings,
	Publishes, Subscribes, ContributesTo: personpage.Point, eventsetup.Point, demoseed.Point */ }
func New(ctx context.Context, deps module.Deps) (module.Module, error)

// Qualifications: the catalogue. Consumers: http.go (catalogue routes), SetRequirements
// (existence), the UI's requirement picker.
type Qualifications interface {
	// ListQualifications lists active (or all) qualifications: org-wide ones for every
	// signed-in person, a department's for everyone (they label positions), archived ones
	// only inside the caller's qualifications.manage extent. Cannot fail beyond store kinds
	// and Unauthenticated: nothing here is personal.
	ListQualifications(ctx context.Context, q vol.QualificationQuery) ([]vol.Qualification, error)
	// CreateQualification needs qualifications.manage site-wide for an org-wide one, or at
	// the department (AllowIn) for a department-specific one; the department must be
	// Active. F2-F6.
	CreateQualification(ctx context.Context, in vol.QualificationInput) (vol.Qualification, error)
	// UpdateQualification changes name, description or validity (never the department). F7-F9.
	UpdateQualification(ctx context.Context, id vol.QualificationID, in vol.QualificationInput) (vol.Qualification, error)
	// ArchiveQualification hides it from pickers; records keep counting until they expire
	// and positions keep requiring it (a lead removes the requirement). F10-F11.
	ArchiveQualification(ctx context.Context, id vol.QualificationID) error
}

// Records: who holds what. Consumers: http.go, the expiry scan, Eligibility, the roster.
type Records interface {
	// RecordsOf lists a person's records: their own without a permission, another's within
	// the caller's qualifications.read extent, filtered to the qualifications of that
	// extent plus org-wide ones. Audits nothing (records are internal, D42). Returns
	// RecordList; SelfUpload is set (the self_upload setting, read now) only when person is
	// the actor. F12-F13, F147.
	RecordsOf(ctx context.Context, person model.PersonID) (vol.RecordList, error)
	// RecordQualification is way (a): a lead records a qualification as verified now, by
	// the actor. Department-specific: qualifications.verify at that department; org-wide:
	// any verify extent (D41). Never for oneself. Audits qualification-record.created
	// and qualification-record.verified (two entries: the record came into being and was
	// vouched for in one act). NoExpiry makes it indefinite whatever the catalogue says. F14-F20, F148.
	RecordQualification(ctx context.Context, in vol.RecordInput) (vol.Record, error)
	// Handler shape (files.md Behavior, D44): the handler calls files.ReadUpload, then
	// Stage (outside the transaction, Accept files.Images and files.Documents); the use
	// case then opens its Write transaction, runs the record checks and calls Attach{Record:
	// Ref{Kind: "volunteers.qualification-record", ID}, Subject: person, Level:
	// model.Personal, Replace: true}, with a deferred Discard of the staged upload.
	// UploadCertificate is way (b): the actor creates a pending record for themself with
	// the certificate stored through files (subject = actor, level personal). Refused when
	// self_upload is off (Unsupported) or a pending record for that qualification exists.
	// F21-F27.
	UploadCertificate(ctx context.Context, in vol.UploadInput) (vol.Record, error)
	// VerifyRecord marks a pending record verified by the actor, optionally correcting the
	// dates; permission as RecordQualification. Sends volunteers.qualification-verified
	// to the person. A zero ExpiresOn keeps what the person entered; NoExpiry clears
	// it (a certificate that never expires); both set is Invalid one_of. F28-F32, F148.
	VerifyRecord(ctx context.Context, id vol.RecordID, in vol.VerifyInput) (vol.Record, error)
	// RejectRecord deletes a pending record and its certificate (nothing to keep: it never
	// counted) and sends volunteers.qualification-rejected. Permission as VerifyRecord. F33-F35.
	RejectRecord(ctx context.Context, id vol.RecordID) error
	// RemoveRecord deletes a record and its certificate: the person removes their own
	// pending one; a lead who may verify removes any (a mistaken entry). F36-F38.
	RemoveRecord(ctx context.Context, id vol.RecordID) error
	// PendingRecords lists records awaiting verification that the actor may verify. F39.
	PendingRecords(ctx context.Context, q vol.PendingQuery) ([]vol.Record, error)
	// OpenCertificate opens the certificate for the person themself or for a lead who may
	// verify (or read) the record; audits volunteers.qualification-record.read (personal,
	// Fields ["certificate"]) when the actor is not the person, then files audits the
	// bytes. The handler passes the result to files.Serve. F40-F43.
	OpenCertificate(ctx context.Context, id vol.RecordID) (files.File, files.Content, error)
}

// Positions. Consumers: http.go, Eligibility, the shift form's position picker.
type Positions interface {
	// ListPositions: active positions of a department for every signed-in person (they
	// label shifts); archived ones inside the caller's positions.manage extent. F44.
	ListPositions(ctx context.Context, dept model.DepartmentID, includeArchived bool) ([]vol.Position, error)
	// CreatePosition needs positions.manage at the department, which must be Active. F45-F48.
	CreatePosition(ctx context.Context, dept model.DepartmentID, in vol.PositionInput) (vol.Position, error)
	UpdatePosition(ctx context.Context, id vol.PositionID, in vol.PositionInput) (vol.Position, error)
	// ArchivePosition refuses while the position has future, non-canceled shifts
	// (Conflict in_use). F49-F51.
	ArchivePosition(ctx context.Context, id vol.PositionID) error
	// SetRequirements replaces the set; each qualification must be active and either
	// org-wide or this department's (F52-F55). Existing bookings are not re-checked
	// (the roster shows Eligible false and the lead decides).
	SetRequirements(ctx context.Context, id vol.PositionID, qualifications []vol.QualificationID) (vol.Position, error)
}

// Members. Consumers: http.go, Eligibility (members-only positions), Sections, people
// (Proposed change 1).
type Members interface {
	// ListMembers lists a department's members with playa names (Deps.People.NamesOf) and
	// the positions each is eligible for today; needs rosters.read at the department. 
	// Paged by (created_at, id); the UI follows Next. F56-F57, F149.
	ListMembers(ctx context.Context, q vol.MemberQuery) (vol.MemberViewList, error)
	// AddMember needs members.manage at the department (Active); the person must exist
	// (foreign key); duplicate is Conflict duplicate. F58-F62.
	AddMember(ctx context.Context, dept model.DepartmentID, person model.PersonID) (vol.Member, error)
	// RemoveMember deletes the row; bookings stay (the lead cancels them if wanted). F63-F64.
	RemoveMember(ctx context.Context, dept model.DepartmentID, person model.PersonID) error
	// MyDepartments lists the actor's memberships. Cannot fail beyond store kinds and
	// Unauthenticated: own records.
	MyDepartments(ctx context.Context) ([]vol.Member, error)
	// Leave removes the actor's own membership; future bookings on members-only
	// positions of that department are canceled (reason self). F65.
	Leave(ctx context.Context, dept model.DepartmentID) error
}

// Shifts. Consumers: http.go, SetUp, the signal handlers (system path), the reminder.
type Shifts interface {
	// ListShifts: shifts the actor may see, with Booked, Mine and Eligibility computed for
	// the actor: open-access positions of active departments for everyone; members-only
	// ones for members; canceled ones and past ones inside the caller's shifts.manage or
	// rosters.read extent. Never reveals a shift the actor may not see. Paged
	// (ShiftList.Next); DepartmentName from one Departments.NamesOf per page, PositionName
	// from the join; shifts of a department NamesOf does not return are dropped from the
	// page (archived since) and the page may be short. F66-F67, F149.
	ListShifts(ctx context.Context, q vol.ShiftQuery) (vol.ShiftList, error)
	// GetShift: one shift with the same visibility rule (NotFound otherwise), plus Mates
	// (who else is booked, playa names only) when the shift_mates policy shows it to this
	// viewer (privacy.Policy.Shows, then Filter); absent otherwise. Not audited (internal).
	// A past shift the actor could otherwise see is returned (the list's "from now on"
	// default is about volume, not secrecy; a bookmarked link keeps working). F68, F126-F134.
	GetShift(ctx context.Context, id vol.ShiftID) (vol.ShiftSummary, error)
	// CreateShift needs shifts.manage at the department (Active); the position must be
	// active and of that department; an event must exist and not be closed; Zone comes
	// from the event or must be a valid IANA name. F69-F76.
	CreateShift(ctx context.Context, dept model.DepartmentID, in vol.ShiftInput) (vol.Shift, error)
	// UpdateShift: same checks; a time change moves every booked person's reminder and
	// refuses when a booked person would then overlap another booking (Conflict overlap)
	// or when Slots would fall below Booked (Conflict full). The visibility rule runs
	// before AllowIn: an invisible shift is NotFound, a visible one without shifts.manage@D
	// is Permission (decision 20). Event is immutable: an input whose Event differs from
	// the shift's is Invalid scope on "event" (decision 28). F77-F80, F143, F166.
	UpdateShift(ctx context.Context, id vol.ShiftID, in vol.ShiftInput) (vol.Shift, error)
	// CancelShift cancels the shift and every booked sign-up (reason shift_canceled),
	// sending volunteers.signup-removed to each person. Idempotent (Conflict canceled on
	// a second call). Visibility first, as UpdateShift. F81-F83, F144.
	CancelShift(ctx context.Context, id vol.ShiftID) error
	// CopyShifts copies the non-canceled shifts of one department from one event to
	// another; needs shifts.manage at the department; idempotent (a shift already copied
	// from the same source is skipped). Returns the number created. Times are copied
	// as wall-clock times in the destination event's zone: each shift's civil start and end
	// (in its own zone) are moved by the whole-day difference between the two events' start
	// dates and re-anchored with time.Date in the destination zone, so "08:00–14:00" stays
	// "08:00–14:00" across a DST change or a zone change; when the result would not satisfy
	// Ends > Starts (a shift inside a skipped hour) Ends is Starts plus the source duration.
	// F84-F88, F150.
	CopyShifts(ctx context.Context, dept model.DepartmentID, from, to model.EventID) (int, error)
}

// Signups. Consumers: http.go, the reminder job, the signal handlers, Sections.
type Signups interface {
	// SignUp books the actor into a shift: needs shifts.signup; the shift must be visible,
	// not canceled, not started, of an active department and an open event; the actor
	// must be eligible (requirements, membership), the shift not full, and no other booked
	// shift may overlap; the actor must have a playa name (decision 26: Conflict
	// unnamed; uniqueness is people's, P1). Enqueues the reminder (at the jobs horizon
	// when the reminder time is further ahead, decision 25). Publishes
	// volunteers.signup-booked. F89-F99, F151, F161, F164.
	SignUp(ctx context.Context, shift vol.ShiftID) (vol.Signup, error)
	// Assign books a person: needs shifts.manage at the department; allowed until the
	// shift ends (walk-ons); the same rules, except that Waive (D42) records WaivedBy and
	// audits Reason waived instead of refusing unqualified. Only Assign waives; SignUp
	// never does. Visibility first (an invisible shift is NotFound); a lead may assign
	// themself, with a waiver if needed (Q-VOL7: yes; recorded like any other). The person
	// must have a playa name (decision 26: Conflict unnamed); Waive does not override it.
	// F100-F104, F145, F152, F161, F164.
	Assign(ctx context.Context, shift vol.ShiftID, in vol.AssignInput) (vol.Signup, error)
	// CancelSignup: the person cancels their own booking before the shift starts and
	// before cancel_cutoff; a shifts.manage holder cancels any (reason lead, the person
	// is told). Publishes volunteers.signup-canceled. F105-F109.
	CancelSignup(ctx context.Context, id vol.SignupID) error
	// Roster: the shift's booked people with person IDs, playa names, waivers and current
	// eligibility; needs rosters.read at the department. The lead's view; volunteers get
	// ShiftSummary.Mates instead (D42). LegalName is filled from People.LegalNamesOf only
	// when the legal_name policy shows it to this viewer, each entry filtered with its own
	// subject, and the read audited volunteers.roster.read (D43). Visibility first: an
	// invisible shift is NotFound, a visible one without rosters.read@D is Permission.
	// F110-F111, F135-F140, F146.
	Roster(ctx context.Context, shift vol.ShiftID) (vol.Roster, error)
	// MySchedule: the actor's bookings with shift, department and position names. Two
	// modes (ScheduleQuery): upcoming (not ended; soonest first; includes bookings someone
	// else canceled, with the reason) and past (ended; booked only; newest first); paged.
	// Fails Invalid on the page or window; otherwise only store kinds and Unauthenticated. F153.
	MySchedule(ctx context.Context, q vol.ScheduleQuery) (vol.ScheduleList, error)
	// Listed reports whether the actor's playa name is shown to other volunteers on their
	// shifts (true unless SetListed(false) was called). Consumer: the My volunteering
	// screen. Own record: cannot fail beyond store kinds and Unauthenticated.
	Listed(ctx context.Context) (bool, error)
	// SetListed records the actor's choice; false hides them from every shift-mate list
	// (counted in Unlisted) from the next read on; it never affects rosters, bookings or
	// what the actor sees. Idempotent; audited volunteers.listing.changed. F142.
	SetListed(ctx context.Context, listed bool) error
}

// *Service implements Qualifications, Records, Positions, Members, Shifts, Signups,
// module.Module (Start registers two subscriptions and four jobs ; never writes; Stop is
// a no-op), module.RouteProvider, module.APIDescriber, module.Contributor
// (personpage.Provider as Sections; eventsetup.Provider as SetUp) and module.PersonData.
```

**Routes** (relative to `/api/volunteers`; all `AccessSignedIn`; IDs with `model.ParseID`):

| Method, pattern | Service call | Permission (checked in the service) |
|---|---|---|
| `GET /qualifications?department=&archived=` | `ListQualifications` | none for active; `qualifications.manage` extent for archived |
| `POST /qualifications` | `CreateQualification` | `qualifications.manage` (site for org-wide, `@D`) |
| `PUT /qualifications/{qualification}` | `UpdateQualification` | same |
| `POST /qualifications/{qualification}/archive` | `ArchiveQualification` | same |
| `GET /people/{person}/qualifications` (body `RecordList`, `self_upload` present for oneself) | `RecordsOf` | self, or `qualifications.read` extent |
| `POST /people/{person}/qualifications` | `RecordQualification` | `qualifications.verify` |
| `POST /me/qualifications` (multipart; `MaxBodyBytes = files.UploadCeiling`, `Timeout = files.UploadTimeout`) | `UploadCertificate` | self; setting `self_upload` |
| `GET /qualification-records?department=` | `PendingRecords` | `qualifications.verify` extent |
| `POST /qualification-records/{record}/verify` | `VerifyRecord` | `qualifications.verify` |
| `POST /qualification-records/{record}/reject` | `RejectRecord` | `qualifications.verify` |
| `DELETE /qualification-records/{record}` | `RemoveRecord` | self (pending) or `qualifications.verify` |
| `GET /qualification-records/{record}/certificate` (`Timeout = files.ServeTimeout`; `files.Serve`) | `OpenCertificate` | self, or `qualifications.read`/`.verify` |
| `GET /departments/{department}/positions?archived=` | `ListPositions` | none for active |
| `POST /departments/{department}/positions` | `CreatePosition` | `positions.manage@D` |
| `PUT /positions/{position}` | `UpdatePosition` | `positions.manage@D` |
| `POST /positions/{position}/archive` | `ArchivePosition` | `positions.manage@D` |
| `PUT /positions/{position}/requirements` | `SetRequirements` | `positions.manage@D` |
| `GET /departments/{department}/members?limit=&after=` (paged, body `MemberViewList`) | `ListMembers` | `rosters.read@D` |
| `POST /departments/{department}/members` | `AddMember` | `members.manage@D` |
| `DELETE /departments/{department}/members/{person}` | `RemoveMember` | `members.manage@D` |
| `GET /me/departments` | `MyDepartments` | self |
| `DELETE /me/departments/{department}` | `Leave` | self |
| `GET /shifts?department=&event=&position=&from=&to=&canceled=&eligible=&limit=&after=` (`after` is `ShiftList.next`; `from` or `to` alone allowed; `eligible` a boolean, `Invalid malformed` on `eligible` otherwise, V4, D58) | `ListShifts` | visibility rule |
| `GET /shifts/{shift}` | `GetShift` | visibility rule |
| `POST /departments/{department}/shifts` | `CreateShift` | `shifts.manage@D` |
| `PUT /shifts/{shift}` | `UpdateShift` | `shifts.manage@D` |
| `POST /shifts/{shift}/cancel` | `CancelShift` | `shifts.manage@D` |
| `POST /departments/{department}/shifts/copy` `{from, to}` | `CopyShifts` | `shifts.manage@D` |
| `POST /shifts/{shift}/signups` | `SignUp` | `shifts.signup` |
| `POST /shifts/{shift}/assign` `{person, waive}` | `Assign` | `shifts.manage@D` |
| `DELETE /signups/{signup}` | `CancelSignup` | self or `shifts.manage@D` |
| `GET /shifts/{shift}/roster` | `Roster` | `rosters.read@D` |
| `GET /me/schedule?from=&to=&past=&limit=&after=` (paged, body `ScheduleList`) | `MySchedule` | self |
| `GET /me/listing` | `Listed` | self |
| `PUT /me/listing` `{listed}` | `SetListed` | self |

Handlers are thin (`httpx.ReadJSON`, `httpx.WriteJSON`, `httpx.Fail`); list responses are wrapped objects (`{"items": [...]}`), as the M2 build settled. Every wrapper's `items` is `omitzero` and the service returns a non-nil slice, so an empty list encodes `{"items":[]}` and a client never has to treat an absent key as empty (D46 forbids only a placeholder for a hidden field; an empty list is the value). The paged wrappers add `next`, absent on the last page; a handler parses `after` into `vol.Cursor` without decoding it (the service validates). `GET` routes change nothing. The OpenAPI fragment names schemas `VolunteersQualification`, `VolunteersRecord`, `VolunteersPosition`, `VolunteersMember`, `VolunteersShift`, `VolunteersShiftSummary`, `VolunteersSignup`, `VolunteersRoster`, `VolunteersScheduleEntry`.

## Types

Defined in `vol` above. Uses `model.PersonID`, `model.DepartmentID`, `model.EventID`, `model.TimeRange`, `files.FileID`, `files.Multipart`, `files.File`, `files.Content`. Every `Validate()` joins `validate.FieldError`s with the codes listed under Summary; names are trimmed and refuse control characters; a zone must load with `time.LoadLocation`.

## Data

Schema `volunteers`; migrations `postgres/migrations/0001_init.sql` and `0002_booked_as.sql` (version 2: `signup.booked_as`, backfilled `'lead'` where `booked_by is not null or waived_by is not null`, else `'self'`; and, D51, `signup.waived boolean NOT NULL`, backfilled `true` where `waived_by is not null`, else `false`; the migration has never shipped, so both columns ride in the one file). Decision 26 adds no column and no index: playa names are never copied into this schema (they are people's data and would go stale on a rename), and the "who is booked at this event" query is served by the existing `shift (event_id, starts)` and `signup (shift_id, status)` indexes. Every table has `created_at`, `updated_at` (`timestamptz not null`). Foreign keys to `core.person(id)`, `core.department(id)` and `core.event(id)` are `on delete restrict` (people calls `DeletePerson` first; departments and events are never deleted).

**`qualification`:** `id uuid pk`; `department_id uuid null` (FK department; null = org-wide); `name text` (1..100 bytes); `description text` (≤ 2000); `validity_days int null` (1..3650); `archived_at timestamptz null`. Unique index on `(coalesce(department_id, '00000000-0000-0000-0000-000000000000'), lower(name))` (`qualification_name_key`). Nothing personal; kept forever.

**`qualification_record`:**

| Column | Type | Sensitivity | Why collected | Retention |
|---|---|---|---|---|
| `id` | `uuid pk` | — | | with the row |
| `qualification_id` | `uuid` FK `qualification` restrict | internal | what is held | |
| `person_id` | `uuid` FK person restrict | internal (D42) | who holds it; eligibility | until `DeletePerson`, or `volunteers.retention` after expiry |
| `acquired_on`, `expires_on` | `date`, `date null` | internal | validity (D40) | |
| `verified_by` | `uuid null` FK person restrict | internal | who vouched (D40); null = pending | nulled by that person's deletion |
| `verified_at` | `timestamptz null` | internal | | |
| `certificate_file_id` | `uuid null` (no FK: files owns the row; `core.file.record_*` points back) | personal (the file: a certificate carries a legal name and an issuer) | a lead verifies the document (D40 way (b)) | deleted with the record (`files.DeleteByRecord`) |
| `expiry_notified_on` | `date null` | internal | the expiry notice is sent once | |

Partial unique index `(qualification_id, person_id) where verified_at is null` (`record_pending_key`); index `(person_id)`, `(expires_on) where verified_at is not null`.

**`position`:** `id`; `department_id` FK; `name` (1..100); `description` (≤ 2000); `access text check in ('open','members')`; `hide_shift_mates boolean not null default false` (D42); `archived_at null`. Unique `(department_id, lower(name))`. **`position_requirement`:** `position_id` FK `position` cascade, `qualification_id` FK restrict, PK both. Nothing personal.

**`member`:** `id`; `department_id` FK; `person_id` FK (internal: who belongs where, shown to leads and on the person page); `added_by uuid null` FK (internal). Unique `(department_id, person_id)`. Kept until removed or `DeletePerson`.

**`shift`:** `id`; `department_id`, `position_id` FK (a composite FK `(position_id, department_id) → position(id, department_id)` backs decision 6's "the position is of that department"; a violation is translated to `Invalid scope`, F71); `event_id uuid null` FK event; `label` (1..100); `starts`, `ends timestamptz` (check `ends > starts and ends - starts <= interval '48 hours'`); `zone text`; `slots int` (1..200); `location` (≤ 200); `notes` (≤ 2000; internal; the form says "no personal information here"); `canceled_at null`; `cancel_reason text null` (check in the five reasons); `copied_from uuid null` FK `shift` set null; `created_by uuid null` FK person. Indexes `(department_id, starts)`, `(event_id, starts)`, `(position_id)`, unique `(copied_from, event_id) where copied_from is not null` (the idempotent copy). Nothing personal; `created_by` is internal; canceled rows purged by `volunteers.retention`.

**`signup`:** `id`; `shift_id` FK; `person_id` FK (internal: shift times with a person, per `privacy-data`'s table); `status text check in ('booked','canceled')`; `booked_by uuid null`, `waived_by uuid null`, `canceled_by uuid null` FK person (internal); `booked_as text not null check in ('self','lead')` (who booked, as a role word: survives the lead's deletion, which nulls `booked_by`); `waived boolean not null` (D51: set true when `Assign` waives; survives the waiving lead's deletion, which nulls `waived_by`; not personal data on its own, `internal`, retained with the sign-up); `canceled_at null`; `cancel_reason null`. Partial unique `(shift_id, person_id) where status = 'booked'` (`signup_booked_key`); index `(person_id, status)`, `(shift_id, status)`. Kept until `DeletePerson` or `volunteers.retention` after the shift ends (history and, later, credit).

**`unlisted`:** `person_id uuid pk` FK person restrict; `created_at`. One row per person who chose not to appear in shift-mate lists (`SetListed(false)`); no row = listed. The row itself is `internal` and read only by the person (`Listed`) and by the list computation; nobody is told who opted out, only how many booked people are unlisted. Kept until `SetListed(true)` (the row is deleted) or `DeletePerson`; never purged by retention (it is current state, like `member`).

**Operations and their permission:** every write names its permission in Interface; `ListShifts`, `GetShift` and the roster filter with `ExtentOf` (`shifts.manage`, `rosters.read`) and the membership join; `GetShift`'s `Mates` needs no permission beyond seeing the shift: it is gated by the `shift_mates` policy (`visibility_shift_mates`: `holders` of `shifts.signup` by default, `booked` asserted from the actor's own booking) and the position's `hide_shift_mates`; `Roster`'s `LegalName` is gated by the `legal_name` policy (`holders` of `people.contacts.read` covering the department by default) and by people's own check inside `LegalNamesOf`; the system paths used by the jobs, the signal handlers, `ExportPerson` and `DeletePerson` take no actor and are reachable only from those callers (never a route).

**Export** (`ExportPerson`, one JSON document): `memberships` (department ID and name, since), `listed` (`true`/`false`; absent when the module holds nothing about the person: an unknown person exports `{}`), `signups` (shift label, department, position, times, zone, status, reasons, whether a requirement was waived (the `waived` column, so it survives the waiving lead's deletion), who booked or canceled as role words, never names; `booked_by` is `booked_as`, and `canceled_by` is derived from `cancel_reason`: `self` → `self`, `lead` → `lead`, the three system reasons → `system`, so a lead's later deletion loses nothing), `qualification_records` (qualification name, dates, verified yes/no and when, certificate file ID: files exports the bytes itself, files decision 13), `verified_for_others` (a count), `shifts_created` (a count). **Deletion** (`DeletePerson`, inside people's transaction, the ctx given): delete the person's sign-ups, memberships, `unlisted` row and records (calling `files.DeleteByRecord` per record), then set `verified_by`, `added_by`, `booked_by`, `waived_by`, `canceled_by`, `created_by` to null where they name the person (`Anonymized`); `Kept` empty. Idempotent.

**Carry-over to a new event:** positions, qualifications, records and members are standing; shifts copy (`SetUp`, `CopyShifts`); sign-ups never. **Module off:** data stays, routes and screens disappear.

## Permissions

| Permission | Allows | Scoped by | Reaches | Starter roles |
|---|---|---|---|---|
| `volunteers.shifts.signup` | `SignUp` (book oneself) | site; own records | internal | `participant` |
| `volunteers.shifts.manage` | create, change, cancel, copy shifts; `Assign` (with `Waive`, D42); cancel anyone's sign-up; see canceled and past shifts; `SetUp` within its extent | department | internal | `department-admin` |
| `volunteers.rosters.read` | `Roster` (the full roster: IDs, waivers, eligibility, person-page links; legal names only for the `legal_name` audience, which is people's permission, not this one), `ListMembers`; the department's bookings in the person-page section | department | internal | `department-admin`, `volunteer-coordinator` (D43; site role proposed in authz) |
| `volunteers.positions.manage` | positions, their requirements and `HideShiftMates` | department | internal | `department-admin` |
| `volunteers.members.manage` | `AddMember`, `RemoveMember` | department | internal | `department-admin` |
| `volunteers.qualifications.manage` | the catalogue: a department's entries at `@D`; org-wide entries site-wide only | department | internal | `department-admin` (at its department), `department-steward` (site-wide, for the org-wide catalogue) (D42) |
| `volunteers.qualifications.read` | another person's records and certificate for the extent's qualifications and org-wide ones | department | personal (certificates) | `department-admin`, `volunteer-coordinator` (D43) |
| `volunteers.qualifications.verify` | `RecordQualification`, `VerifyRecord`, `RejectRecord`, `RemoveRecord`, `PendingRecords`, `OpenCertificate`; a department's qualifications at `@D`, org-wide ones in any extent (D41) | department | personal | `department-admin` **only** (D41: never `participant`) |

Own records need no permission: `RecordsOf(self)`, `UploadCertificate`, `RemoveRecord` of one's own pending record, `MyDepartments`, `Leave`, `MySchedule`, `Listed`, `SetListed`, `CancelSignup` of one's own booking, `OpenCertificate` of one's own. Active catalogue entries and positions and open shifts are `internal` reads every signed-in person may make (they label what a person signs up for), like departments and events (departments-events decision 2). **Shift-mates** (D42, D43) need no permission of their own: a playa name on a shift is `internal` (people's data table; privacy-data's "playa name, department, shift times" example), the same level as the fill count the shift card already shows; the regional's choice is the `visibility_shift_mates` setting, whose default audience `holders` is "anyone who volunteers" = holders of `shifts.signup`, and a person's choice is `SetListed`. **Legal names** (D43) are people's data: the `legal_name` group's `holders` are holders of `people.contacts.read` (seeded into `account-admin` today and into `volunteer-coordinator` by people's proposed change), and people checks that permission again inside `LegalNamesOf`; this module declares no permission for them. The `volunteer-coordinator` starter role (site; authz proposed change) is "the volunteer coordinator and maybe some other roles" of D43 made concrete: `rosters.read` and `qualifications.read` everywhere, `people.directory.read`, `people.contacts.read`; it manages no shifts (leads do). The base role holds `shifts.signup` by default; a regional that wants invitation-only volunteering removes it or sets positions to `members`.

## Signals

**Published** (inside the transaction; delivered after commit):

| Signal | When | Payload |
|---|---|---|
| `volunteers.signup-booked` | `SignUp`, `Assign` | `signup` (SignupID), `shift` (ShiftID), `person` (PersonID), `department` (DepartmentID) |
| `volunteers.signup-canceled` | `CancelSignup`, `CancelShift` (one per sign-up), `Leave`, the two handlers | same keys |
| `volunteers.qualification-verified` | `RecordQualification`, `VerifyRecord` | `record` (RecordID), `qualification` (QualificationID), `person`, `department` (omitted for org-wide) |

No consumer exists today; they are the hooks `afterreports` (shifts worked) and a trust feature (D18) will subscribe to, and they cost one `Publish` each.

**Subscribed** (handlers registered in `Start`, run as the system, idempotent, quick; each handler only enqueues the job below, decision 23):

| Signal | Handler | On failure |
|---|---|---|
| `departments.department-archived` `{department}` | `Jobs.Enqueue{Name: volunteers.cancel-shifts, Key: "department_archived:<department>", Args: {department}}`; a second delivery while the job waits moves the same row (jobs decision 3), one during or after its run makes it run once more (jobs J1, decision 23), and a second run of the job finds nothing to do | `Enqueue` failing is the handler's error: logged and counted by `signals`, retried by `jobs` for `Unavailable`/`Timeout`; the archive stands and `SignUp` refuses anyway (`Active` false), so a lost delivery leaves stale rows a lead can cancel by hand |
| `events.event-closed` `{event}` | the same with `Key "event_closed:<event>"`, `Args {event}` | same; `SignUp` refuses anyway (`Closed`) |

**`volunteers.cancel-shifts`** (on demand, `Timeout 5m`): in batches of `MaxList` shifts, each its own `Write` transaction: cancel the department's (or event's) non-canceled shifts whose `ends` is in the future (`cancel_reason` from the key's reason) and their booked sign-ups (same reason), audit `shift.canceled` and `signup.canceled`, publish `signup-canceled` and send `volunteers.signup-removed` once per sign-up (notify `Key` `removed:<signup>`); repeat until a batch is empty. Idempotent on state: a retry resumes where it stopped. Args name a department or an event, never a person.

Not subscribed: `events.event-changed` (the shift screen shows "outside the event's dates" from `InfoOf` at render time); `people.person-deleted` (`DeletePerson` already ran); `departments.department-restored` (a lead recreates shifts).

## Settings

| Key | Type | Default | Valid range | Who can change it |
|---|---|---|---|---|
| `volunteers.reminder_lead` | duration | `24h` | `0` (no reminders) or `1h`..`168h` | settings admin |
| `volunteers.cancel_cutoff` | duration | `0` (cancel until the shift starts) | `0`..`168h` | settings admin |
| `volunteers.expiry_notice_days` | int | `30` | `0` (no notice)..`365` | settings admin |
| `volunteers.self_upload` | bool | `true` (D42) | | settings admin |
| `volunteers.visibility_shift_mates` (from `privacy.Settings(Groups)`) | enum | `holders` (D43; see below) | `nobody` (only roster readers see who is booked), `booked` (a person sees the others once they hold a booking on that shift), `holders` (anyone who may volunteer: holders of `shifts.signup`, before booking), `signed_in` (anyone who can see the shift); `one_of` | settings admin |
| `volunteers.visibility_legal_name` (from `privacy.Settings(Groups)`) | enum | `holders` (D43) | `nobody` (the roster never carries legal names), `holders` (holders of `people.contacts.read` covering the shift's department); `one_of` (`self` removed, decision 27) | settings admin |
| `volunteers.retention` | duration | `0` (forever, D30) | `0` or `8760h`..`876000h` (as `audit.retention`) | settings admin |

**Why `holders` is the default for `shift_mates`.** D43: playa names and who is on which shift are visible to anyone who volunteers. "Anyone who volunteers" is a holder of `shifts.signup` (decision 17), which the base role holds, so on a fresh installation every signed-in person sees the list before booking, which is what the owner's reason needs (people who don't get along pick another shift, D42). The cost is bounded: only playa names, only to people who can already see that the shift exists and how full it is, and any person can take themself out of the list. A regional that prefers "see them once you've committed" sets `booked`; one that wants rosters kept with the department sets `nobody`; `signed_in` matters only to a regional that took `shifts.signup` away from the base role and still wants browsers to see the names. A department with a discreet team (peer support, for example, whose volunteers' identities the manuals treat with care) sets `HideShiftMates` on that position instead of asking the regional to turn it off for everyone; the position flag can only hide, never show what the setting hides.

**Why `holders` is the default for `legal_name`.** D43: real names are for the volunteer coordinator and maybe some other roles. `holders` of `people.contacts.read` is exactly that set, and the regional widens or narrows it by editing roles, not this setting; `nobody` is there for a regional that wants legal names out of rosters altogether. (Q-VOL8) The owner's answer is that everyone sees playa names and legal names stay with permission holders, so there is no `self` option: people answers `LegalNamesOf` only for a holder, and privacy-tags already shows a holder their own entry under `holders`, so `self` could only have meant "holders, but each sees only their own", which no regional asked for and which would hide from the coordinator the very names the role exists to see.

Read at use-case or job time through `deps.Settings`, never cached. Catalogue keys `module.volunteers.setting.<key>.title|description`. A `Check` refuses an out-of-range value with `range`.

## UI

All screens phone-first (web-shell decision 21), `useT` with typed keys, `{term_department}`, `{term_event}`, `{term_participant}` for the core terms, times rendered with `useFormat` in the shift's `zone`, `ErrorState`/`ErrorSummary` for errors, no `style` prop; an absent `internal` count or flag reads as zero (D46). Routes relative to `/volunteers/`; nav: main **Shifts** (`/volunteers/`, signed-in), **My volunteering** (`/volunteers/me`, signed-in); admin **Volunteers** (`/volunteers/admin`, access `[shifts.manage, positions.manage, members.manage, qualifications.manage, qualifications.verify, rosters.read]`).

| Screen | Who | Actions | Empty / error / forbidden |
|---|---|---|---|
| `/volunteers/` Shifts | every signed-in person | filter by event, department, day, and **Only {term_shift_plural} I can take** (`?eligible=1`, server-side `eligible=true`, keeps the shifts the person holds; **Clear filters** removes it; D58, V4.3); dense rows grouped by day, one `<li>` per shift, three short lines on a phone and one grid row from 640 px: times only under the dated day heading ("{start} to {end} next day" when the end falls on a later day), the label as the row's link, then department and position (from `department_name` and `position_name` on the summary, no per-department position fetch) and location joined with " · ", the fill count "{booked} of {slots} booked" (V4.4), and the control: **Sign up**, **You're booked** or a short reason (`why_short.*`: needs qualifications named, members only, full, overlap, already started; the shift page keeps the sentence); the list asks `limit=100` and shows **Show more** while `next` is present, appending the next page, also when a page is empty or short because the day filter or `eligible` dropped its rows | "No shifts to show yet" with the filters (only when no row is shown and `next` is absent); a 403 on sign-up shows the shell's forbidden state |
| `/volunteers/shifts/:shift` | as visible | a back link above the heading that returns to where the person came from (router state `{back: {to, kind}}` from the list, the lead's Shifts tab or My volunteering, validated by `backFrom`, default `/volunteers`; label "Back to {term_shift_plural}", "Back to My volunteering", or "Back to {department name}" ("Back to the {term_department}" before the shift loads); V4.2, D58); the **Slots** line says "{booked} of {slots} booked"; sign up (decision 26: a `409 unnamed` shows "Set your playa name on your profile first." with a link to `/people/me`), cancel my booking (a confirm `Dialog` "Cancel your booking?" whose dismiss button reads "Keep my booking", `module.volunteers.shift.keep_mine`, D57 records what is built); **Who else is on this shift** (when `mates` is present: the playa names, then "and {n} not listed" when `unlisted` > 0; plain text, no links; the section is absent when `mates` is null, with no hint why); **Roster** section (`rosters.read@D` holders only) with playa names linking to `/people/:person`, a legal name next to the playa name when `legal_name` is present on the entry (no column at all when it is absent on every entry), an eligibility flag and a **requirement waived** flag; **Manage** section (`shifts.manage@D` holders only, independent of the roster section): **Assign** (playa-name search through `GET /api/people/search`, a **Waive requirements** switch that explains the waiver is recorded; Q-VOL7: yes, the switch works for the viewer too; a `409 unnamed` shows "This person has not set a playa name yet."), edit, cancel (a confirm `Dialog` "Cancel this shift?" whose dismiss button reads "Keep this shift", `module.volunteers.shift.keep`, matching the booking dialog's "Keep my booking", D57); a manager without `rosters.read` sees Manage and no roster, a roster reader without `shifts.manage` sees the roster and no Manage | NotFound state for an invisible shift; canceled banner; "No one else is booked yet" when `mates` is present and empty |
| `/volunteers/me` My volunteering | self | upcoming and past bookings (two toggle buttons (`aria-pressed`) in a labelled group, for `past=false`/`past=true`, each paged with **Show more**; an upcoming booking someone else canceled is shown struck through with its reason, "removed by a lead", "shift canceled", "{term_department} archived", "{term_event} closed", until the shift passes, Q-VOL9) with cancel; my departments with leave; my qualifications with status (pending, verified, expires on, expired, never expires) and **Upload a certificate** (`FileField`, shown when `self_upload` on the person's own `GET /people/{person}/qualifications` is true, hidden otherwise; the screen reads the flag from that body, never from `GET /api/settings`); **Show my playa name to others on my shifts** (a switch from `GET /me/listing`, saved with `PUT /me/listing`, with the note that leads always see the roster) | "You have no shifts booked" with a link to Shifts |
| `/volunteers/admin` | holders of any lead permission | the departments the viewer may manage or view (from `session.permissions` `p@D` and `GET /api/departments`; `LEAD_PERMISSIONS`), headed by what the person can do (Q-VOL13, D58): a site-wide holder sees "All {term_department_plural}" with the note "You can manage or view every {term_department}.", otherwise "{term_department_plural} you can manage or view"; pending certificates count | "No {term_department} you can manage or view yet." |
| `/volunteers/departments/:department` | leads | tabs: **Shifts** (by event, fill counts, new, copy from a previous event; paged with **Show more**), **Positions** (list, new, edit with a **Hide who's on shifts from other volunteers** switch, requirements picker, archive), **Members** (list with eligibility per position, add by search, remove; paged with **Show more**), **Qualifications** (the department's catalogue; org-wide ones read-only here), **Pending** (certificates to verify, the org-wide pending ones included and labelled "org-wide" (D41): open the file, correct dates, verify or reject; the verify form has an **Expires on** date and a **Never expires** switch that clears and disables the date, sending `no_expiry: true`; the certificate opens through the shell's `Attachments` row, which fetches through `@/api`: a 404 shows "This certificate is no longer available", a 503 with `Retry-After` the shell's "not available on this copy yet", and success opens the bytes; never a raw link to the route) | each tab's empty state names the action; forbidden tabs are hidden by `session.permissions` and answer the shell's forbidden state if reached |
| `/volunteers/departments/:department/shifts/new`, `/volunteers/shifts/:shift/edit` | `shifts.manage@D` | the form: position, event (or standing work with a zone), label, start and end in the zone, slots, location, notes ("no personal information") | field errors inline (`module.volunteers.field.<code>`) |
| `/volunteers/people/:person` | `qualifications.read`/`.verify` holders | a person's records in the viewer's extent; **Record a qualification** (way (a); with the same **Never expires** switch); open certificate (through `Attachments`, as on the Pending tab); remove | NotFound outside the extent |
| `/volunteers/qualifications` | site-wide `qualifications.manage` | the org-wide catalogue: new, edit, archive | |

Job titles (jobs J-W1 decision 24, applied D58): `web/src/modules/volunteers/messages/en.json` holds `module.volunteers.job.shift-reminder.title` "Send {term_shift} reminders", `.expiry-scan.title` "Warn about {term_qualification_plural} about to expire", `.purge.title` "Delete volunteer records past their retention" and `.cancel-shifts.title` "Cancel the {term_shift_plural} of an archived {term_department} or a closed {term_event}" (the title the Background work screen shows for each job `volunteers.<ident>`; checked by `TestMessageCatalogues_EnglishCoversJobs`; no code change in the module).

The person page shows section `volunteers.summary` (title `module.volunteers.section.summary.title`): items **Departments** (names, to the viewer's extent), **Upcoming shifts** (count, `Href` `/volunteers/me` for the person themself).

## Behavior

- **Actor and checks, in order:** actor (`identity.PersonFrom`; none → `Unauthenticated`), input `Validate`, permission, then state. A record outside the caller's extent is `NotFound` (privacy-data); an action the caller may not take on a visible record is `Permission`. (decision 20) For a shift, "visible" is `ListShifts`' rule, evaluated for the actor before `AllowIn` in `GetShift`, `UpdateShift`, `CancelShift`, `SignUp`, `Assign` and `Roster`: in the actor's `shifts.manage` or `rosters.read` extent (any state), or not canceled and either open-access or members-only in a department the actor belongs to. Unknown and invisible are the same `NotFound`. Qualifications, positions, departments and events are visible to every signed-in person (they label what one signs up for), so their write methods answer `Permission` for a holder-less caller and `NotFound` only for an unknown ID (F7, F10, F45, F49 read that way; an archived position by ID is still visible: archiving hides it from pickers, not from the person who has its link).
- **Paging** (decision 22): `ListShifts`, `MySchedule` and `ListMembers` fetch `Limit + 1` rows in their stable order, return the first `Limit`, and set `Next` to the last returned row's cursor when the extra row exists. `ListShifts` drops rows whose department `Departments.NamesOf` no longer returns (archived since the handlers ran) after the fetch; `Next` still comes from the fetched rows, so the next page starts after the last fetched row and nothing is skipped; a short page is not "the end": only an absent `next` is. With `EligibleOnly` (V4, D58) the store fetches up to `MaxList + 1` rows in keyset order from `After` instead of `Limit + 1` (one query; eligibility is in Go, decision 4), the service computes `Eligibility` and `Mine` for the actor as above, keeps the rows that pass (eligible, or `Mine` set) and returns the first `Limit` of them; `Next` is the cursor of the last row the call consumed: the last returned row when fetched rows remain beyond it, the last fetched row when fewer than `Limit` passed and a `MaxList + 1`-th row exists, and empty otherwise, so a page is never longer than `Limit`, never skips a row, costs at most one `MaxList`-row scan, and can be empty with `Next` set (the screen shows **Show more**). Only `http.go` sets `EligibleOnly`; `SetUp` and the lead's tab never do. A cursor from another list, another order or another person's query simply yields the rows after that key (no secret is in it: a `starts` and an ID the caller could see).
- **Eligibility** (`eligible(person, shift, now)`): department `Active`; shift not canceled; event (if any) not `Closed`; position not archived; if `Access == members`, a `member` row exists; every requirement has a `Record.ValidOn(DateOf(shift.Starts, shift.Zone))`; `Booked < Slots`; no booked sign-up of the person whose shift `Times().Overlaps(shift.Times())`. `ListShifts` computes it for the actor with the actor's records and bookings loaded once.
- **Visibility plumbing** (D43, every use case that returns a `vol` response type): after the permission check and the load, `privacy.ViewerOf(ctx, authz, actor, Groups)` (A1, privacy-tags A1.2: `actor` is the `model.PersonID` the use case took from `identity.PersonFrom` before its permission check; `viewer.Extents` is keyed by group name, A1.1, which changes nothing here since the service only hands `viewer` on to `Shows` and `Filter`) and `privacy.PolicyOf(ctx, settings, Groups)` once, then `privacy.Filter` per record with `Relation{Subject: the entry's person (zero for a shift or position), Department: the record's department, Also: [booked] when the viewer holds a booked sign-up on that shift}`; the audit entries the `Result`s call for are recorded in one `Record` before returning. Types with no grouped field (`Qualification`, `Position`, `Shift`, `Member`, `ScheduleEntry`, ...) still pass through `Filter` (it is cheap and keeps the rule uniform: a later grouped field needs no new call site).
- **Shift-mates** (`GetShift`, D42, D43): after the visibility rule passes, `Mates` is loaded when the position's `hide_shift_mates` is false and `pol.Shows(GroupShiftMates, viewer, rel)` is true (`visibility_shift_mates` read now, never cached; `rel.Also` carries `booked` when the actor holds a `booked` sign-up on this shift); otherwise `Mates` stays nil, `Filter` would zero it anyway, and the response says nothing about why. When loaded: the shift's `booked` sign-ups other than the actor's, joined with `unlisted`; for the listed ones `Deps.People.NamesOf` (one call, at most `MaxSlots` IDs); a person whose playa name is `""` or who is missing from the map (no profile yet, deleted) is counted in `Unlisted` with the opted-out, never shown by any other field (the module has no access to a legal name and asks for none). `Names` is sorted and never repeats (people P1 makes playa names unique across the installation; pronouns are not shown because the owner said names only). `ListShifts` never fills `Mates` (one `NamesOf` per shift would not scale on a list and the card shows the fill count). Canceled shifts show it too (what was booked is not secret from someone who could see the shift). Not audited: everything shown is `internal`, and the per-read audit a `personal` field would get would log every volunteer's every glance at a shift page without protecting anything (the same reasoning as the qualification record, decision 9).
- **Roster legal names** (`Roster`, D43): after `rosters.read@D` passes and the entries are loaded, the service asks `pol.Shows(GroupLegalName, viewer, Relation{Subject: entry.Person, Department: shift.Department})` per entry; (Q-VOL8: no) when any entry shows **and** `viewer.Extents[GroupLegalName]` is non-zero (the viewer holds `people.contacts.read` somewhere; privacy-tags shows a subject their own entry under `holders` regardless, but people answers only a holder, so a non-holder on their own roster is never asked for: no call, no `Permission` log line, F138), one `Deps.People.LegalNamesOf` call with those persons (people checks `people.contacts.read` for the actor and audits its own `people.person.read` per person; a `Permission` from people, which happens only if the group's permission and people's rule disagree, is answered by leaving every `LegalName` nil, never by failing the roster, and is logged once); a person people omits stays nil. (D45: for a viewer who holds `people.contacts.read` only in department scope, people answers only for people the `people.membership` providers report as members of that department, and for nobody while no provider is registered, so those entries stay nil; a site holder is answered for all. `Shows` decides the audience; people decides reach.) Then `Filter` per entry; the `Result`s with `legal_name` become one `volunteers.roster.read` entry per subject (`Fields [legal_name]`, `Level personal`, `Record` the shift), recorded in one call. Under `nobody`, or when `Deps.People` has no `LegalNamesOf` yet, people is never asked.
- **Listing choice** (`SetListed`): one `Write` transaction: `false` inserts the `unlisted` row (`ON CONFLICT DO NOTHING`), `true` deletes it; audits `volunteers.listing.changed` (`Subject` the actor, `Reason` `listed` or `unlisted`) only when a row changed, so a repeat is a no-op. Takes effect on the next `GetShift`; nothing is recomputed or notified. The choice is one per person, not per shift: someone avoiding another person wants to be absent from every list, and a per-shift switch would make them choose each time on a phone.
- **Booking** (`SignUp`, `Assign`): one `Write` transaction: `pg_advisory_xact_lock(hashtext(person_id))`, then (decision 26) the playa-name precondition below, then `SELECT ... FROM volunteers.shift WHERE id = $1 FOR UPDATE`, re-check eligibility, insert, audit `volunteers.signup.created` (Fields `[person, shift]`, `Reason` `self`, `lead` or `waived`; `waived` means `Assign` with `Waive: true` booked someone eligibility refused for a missing requirement, D42; `Waive: true` on an eligible person records nothing special and `WaivedBy` stays zero), publish `signup-booked`, and when `reminder_lead > 0` and `Starts - lead > now`, `Jobs.Enqueue{Name: volunteers.shift-reminder, Key: <signup id>, Args: {signup}, RunAt: min(Starts - lead, now + ReminderHorizon)}` where (decision 25) `ReminderHorizon = jobs.MaxDelay - 24h`: a reminder further ahead than jobs can schedule is parked at the horizon rather than dropped. `booked_as` is `self` from `SignUp`, `lead` from `Assign`. The partial unique index is the backstop for a duplicate (`Conflict duplicate`). (Q-VOL7: yes) `Assign` with `Person == actor` and `Waive: true` on an unqualified actor is an ordinary waiver: `waived_by` = the actor, `Reason waived`, the roster flag; the audit entry's `Actor` and `Subject` being the same person is the whole record of it.
- **Playa-name precondition** (decision 26; `SignUp` and `Assign` on every shift, event or standing): with the person lock held, `Deps.People.NamesOf([person])` once; a missing entry or `""` is `Conflict unnamed`, with or without `Waive`, before the shift lock is taken, so a refused booking holds nothing but the person lock. The roster, the shift-mate list, the radio and the lead's notices all go by playa name, and a booked person with none would be a blank line on each of them; standing work is included because its roster and shift-mates are the same screens (the earlier draft's standing-shift exemption existed only because the per-event clash check had no event to run in). The shell already prompts for a name once per session, and a lead with `people.profiles.manage` can set a walk-on's, so the refusal costs a minute. A `NamesOf` failure returns its kind and the transaction rolls back (F164). **Uniqueness is not this module's:** people Amendment P1 (owner, 2026-10-22, Q-VOL10 and Q-VOL11) makes playa names unique across the installation under `people.NameKey` and read-only once set, refusing `Conflict taken` at `UpdateMe`/`Create`; so no two people this module ever books share a name, no rename can create a clash after booking, and this module compares no names, takes no name lock and copies no name into its schema. If people later allows a rename, retirement or several names per person (P1: Later), the invariant stays people's (its rule is what makes the roster unambiguous); this module would need a change only if people stopped guaranteeing one unique name per person. `ListShifts` and `GetShift` do not precompute the precondition (the person learns it on booking, with a message that says what to do). `UpdateShift` cannot move a shift to another event (decision 28).
- **Reminder job** (`volunteers.shift-reminder`, `Timeout 30s`): loads the sign-up; if canceled, the shift canceled or `Starts` past, a no-op. Otherwise reads `reminder_lead` now and computes `due = Starts - lead`: if `due > now`, the shift was moved later (or the booking was parked at the horizon) and the handler re-enqueues itself with the same `Key` at `min(due, now + ReminderHorizon)` and sends nothing; else `Notify.Send{Template: volunteers.shift-reminder, To: person, Key: "reminder:<signup>:<starts unix>", Vars: {label, department, position, starts (formatted in the zone, English day and month names in M4), zone, location}, Link: "/volunteers/shifts/<id>"}`; `Unsupported` is logged once and the job succeeds; any other `Send` error is returned (retried by kind). A time change inside the horizon re-enqueues with the same `Key`, which moves the row (jobs decision 3); a change beyond it parks the row at the horizon and the handler carries it forward. Both the carry-forward and a move rest on jobs' key rule "the latest request wins" (J1, decision 23): the handler's own `Enqueue` runs while its row is `running` and is held on the row and applied when the run ends (the row is `queued` again at the parked time, not `done`); an `UpdateShift` during the run is held the same way and supersedes the handler's request if it comes later; and an `UpdateShift` after the reminder was sent (row `done`) reopens the row, so the notify `Key` carrying `starts` gets its second chance and a shift moved after a reminder was sent is reminded once more for its new time. The handler never relies on the request it made: it re-reads the sign-up, the shift and `reminder_lead` on every run.
- **Expiry scan** (`volunteers.expiry-scan`, `Every 24h`, `Timeout 5m`): reads `expiry_notice_days` (0 → return); for each verified record with `expires_on` within `[today, today+N]` (today in UTC), `expiry_notified_on` null and no other valid record of the same qualification for the person, sends `volunteers.qualification-expiring` (`Key` `expiring:<record>`, Vars `{qualification, expires_on, days}`) and sets `expiry_notified_on`, each in its own small transaction so a failure loses one notice, not the batch.
- **Purge** (`volunteers.purge`, `Every 24h`, `Timeout 5m`): reads `retention` (0 → return); deletes sign-ups whose shift `ends < now - retention`, canceled shifts with `canceled_at < now - retention` and no sign-ups left, and records with `expires_on < now - retention` (with `files.DeleteByRecord`), in batches of 500; audits `volunteers.purge.ran` with counts in `Reason`.
- **Notifications** (all through `deps.Notify`, inside the transaction, `Unsupported` swallowed with one log line): `qualification-verified` (`Key` `verified:<record>`), `qualification-rejected` (`rejected:<record>`, the name only; no reason text in M4), `signup-removed` (`removed:<signup>`; Vars `{label, starts, zone, reason, is_lead, is_self, is_shift_canceled, is_department_archived, is_event_closed}` where `reason` is the `CancelReason` token and exactly one of the five booleans is true: notify's template checker allows only `term` as a function, so a template cannot compare strings; it branches on the flag (`{{if .is_department_archived}}`) and words the department and event with `{{term "department"}}` and `{{term "event"}}`). Vars carry nothing about anyone but the recipient.
- **Schedule** (`MySchedule`): upcoming mode lists `status = booked` rows and rows canceled with a reason other than `self` whose shift has not ended, so a person sees "removed by the lead" or "shift canceled" where the booking was until the shift passes (Q-VOL9); past mode lists `booked` rows whose shift ended, newest first. Department names come from one `Departments.NamesOf` per page.
- **Person page** (`Sections`): `Departments.NamesOf` is called in chunks of `MaxIDs` (a viewer whose extent covers more departments than one call takes still gets every name); the upcoming-shift count is one query per request.
- **Audit** (actions `volunteers.*`, in the `Write` transaction): `qualification.created|changed|archived`, `qualification-record.created|verified|rejected|deleted|read` (read: certificate opened by someone other than the person, Level personal, Fields `[certificate]`; recorded outside any transaction before returning), `position.created|changed|archived|requirements-changed`, `member.created|deleted`, `shift.created|changed|canceled|copied`, `signup.created|canceled`, `roster.read` (D43: a legal name returned on a roster entry; `Subject` the entry's person, `Fields [legal_name]`, `Level personal`; recorded outside any transaction before returning), `listing.changed`, `purge.ran`. `Subject` is the person concerned where one is. `Reason` is a token (`self`, `lead`, `waived`, `shift_canceled`, `department_archived`, `event_closed`, `setup`, `listed`, `unlisted`). A `Record` error fails the use case.
- **Event set-up** (`SetUp(ctx, Request{Event, CopyFrom})`): with `CopyFrom` zero returns `Items 0`; otherwise `ExtentOf(shifts.manage)` and, for each department in the extent (every department when `Site`) that has non-canceled shifts in `CopyFrom`, runs `CopyShifts`; idempotent through `copied_from` (a second run finds every source already copied). The caller's permission is the extent; a caller with no extent gets `Items 0` and no error (an event admin who leads nothing copies nothing; leads copy their own later).
- **Concurrency:** `*Service` is safe for concurrent use; the booking locks above (person → event-name → shift, in that order, always); `SetRequirements` and `UpdateShift` take `FOR UPDATE` on their row. **Lifecycle:** `Start` subscribes twice and registers four jobs (no I/O); `Stop` does nothing; no goroutines.
- **Read-only copy:** every read works (rosters and schedules offline-ish, 09 §12); every write fails `Unavailable` wrapping `db.ErrReadOnly`; the jobs do not run there (jobs decision 4).

## Failure modes

Store kinds (`Unavailable`, `Timeout`, `Canceled`) apply to every method that reaches the database and are tested once (`TestStoreFailure_PropagatesKind`); `Unavailable` wrapping `db.ErrReadOnly` applies to every write (`TestWrites_ReadOnlyCopy_ReturnUnavailableReadOnly`); an audit or publish failure inside a write rolls it back (`TestWrite_AuditFails_RollsBack`, `TestWrite_PublishFails_RollsBack`); no actor is `Unauthenticated` on every use case (`TestUseCases_NoActor_ReturnUnauthenticated`, table-driven over all).

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `New` | a needed dep is nil | Invalid | fix wiring | `TestNew_NilDependency_ReturnsInvalid` |
| F2 | `CreateQualification` | org-wide without site-wide `qualifications.manage` | Permission | forbidden | `TestCreateQualification_OrgWideWithoutSitePermission_ReturnsPermission` |
| F3 | `CreateQualification` | department-specific without `manage@D` | Permission | forbidden | `TestCreateQualification_NotDepartmentLead_ReturnsPermission` |
| F4 | `CreateQualification` | department archived or unknown | Conflict `archived` / NotFound | show | `TestCreateQualification_ArchivedDepartment_ReturnsConflict` |
| F5 | `CreateQualification` | name empty, too long, control chars; validity out of range | Invalid | field errors | `TestCreateQualification_InvalidInput_Table` |
| F6 | `CreateQualification` | name taken in the same scope | Conflict `taken` | rename | `TestCreateQualification_DuplicateName_ReturnsConflict` |
| F7 | `UpdateQualification` | unknown or outside extent | NotFound | show | `TestUpdateQualification_Unknown_ReturnsNotFound` |
| F8 | `UpdateQualification` | input changes the department | Invalid `scope` | field error | `TestUpdateQualification_ChangesDepartment_ReturnsInvalid` |
| F9 | `UpdateQualification` | archived | Conflict `archived` | show | `TestUpdateQualification_Archived_ReturnsConflict` |
| F10 | `ArchiveQualification` | not permitted (the entry is visible to all, decision 20) / unknown | Permission / NotFound | | `TestArchiveQualification_NotLead_ReturnsPermission` |
| F11 | `ArchiveQualification` | already archived | Conflict `archived` | show | `TestArchiveQualification_Twice_ReturnsConflict` |
| F12 | `RecordsOf` | another person, no `qualifications.read` extent | Permission | forbidden | `TestRecordsOf_OtherPersonNoPermission_ReturnsPermission` |
| F13 | `RecordsOf` | outside the extent: records of other departments' qualifications | (filtered, not an error) | | `TestRecordsOf_FiltersToExtent` |
| F14 | `RecordQualification` | department-specific, no `verify@D` | Permission | forbidden | `TestRecordQualification_NotDepartmentLead_ReturnsPermission` |
| F15 | `RecordQualification` | org-wide, no `verify` anywhere | Permission | forbidden | `TestRecordQualification_OrgWideNoLeadAnywhere_ReturnsPermission` |
| F16 | `RecordQualification` | org-wide, lead of another department | nil (allowed, D41) | | `TestRecordQualification_OrgWideAnyLead_Succeeds` |
| F17 | `RecordQualification` | for oneself | Invalid `self` | field error | `TestRecordQualification_Self_ReturnsInvalid` |
| F18 | `RecordQualification` | qualification unknown or archived | NotFound / Conflict `archived` | | `TestRecordQualification_ArchivedQualification_ReturnsConflict` |
| F19 | `RecordQualification` | expires before acquired; acquired in the future; malformed date | Invalid `range`/`malformed` | field errors | `TestRecordQualification_Dates_Table` |
| F20 | `RecordQualification` | person unknown (FK) | Invalid `unknown` on `person` | field error | `TestRecordQualification_UnknownPerson_ReturnsInvalid` |
| F21 | `UploadCertificate` | `self_upload` false | Unsupported | hide the action | `TestUploadCertificate_SettingOff_ReturnsUnsupported` |
| F22 | `UploadCertificate` | no file / not multipart | Invalid (files' `ReadUpload`) | field error | `TestUploadCertificate_NoFile_ReturnsInvalid` |
| F23 | `UploadCertificate` | sniffed type not image or PDF; too large | Invalid `type`/`too_large` (files) | field error | `TestUploadCertificate_WrongType_ReturnsInvalid` |
| F24 | `UploadCertificate` | pending record exists for that qualification | Conflict `pending_exists` | show | `TestUploadCertificate_PendingExists_ReturnsConflict` |
| F25 | `UploadCertificate` | qualification archived | Conflict `archived` | | `TestUploadCertificate_Archived_ReturnsConflict` |
| F26 | `UploadCertificate` | files' `Stage` or `Attach` fails (quota, store down) | Conflict `quota` / Unavailable | show; rolled back | `TestUploadCertificate_FilesFails_RollsBack` |
| F27 | `UploadCertificate` | read-only copy | Unavailable wrapping `ErrReadOnly` | 503 | covered by the per-package test |
| F28 | `VerifyRecord` | not permitted for that qualification's scope | Permission | forbidden | `TestVerifyRecord_WrongDepartmentLead_ReturnsPermission` |
| F29 | `VerifyRecord` | record outside the extent / unknown | NotFound | | `TestVerifyRecord_Unknown_ReturnsNotFound` |
| F30 | `VerifyRecord` | already verified | Conflict `status` | show | `TestVerifyRecord_AlreadyVerified_ReturnsConflict` |
| F31 | `VerifyRecord` | own record | Invalid `self` | | `TestVerifyRecord_OwnRecord_ReturnsInvalid` |
| F32 | `VerifyRecord` | notify unconfigured | nil (logged once) | | `TestVerifyRecord_NotifyUnsupported_Succeeds` |
| F33 | `RejectRecord` | not permitted | Permission | | `TestRejectRecord_NotLead_ReturnsPermission` |
| F34 | `RejectRecord` | record verified (not pending) | Conflict `status` | use Remove | `TestRejectRecord_Verified_ReturnsConflict` |
| F35 | `RejectRecord` | `files.DeleteByRecord` fails | its kind; rolled back | retry | `TestRejectRecord_FilesFails_RollsBack` |
| F36 | `RemoveRecord` | own record but verified | Permission | ask a lead | `TestRemoveRecord_OwnVerified_ReturnsPermission` |
| F37 | `RemoveRecord` | another's, no verify permission | NotFound | | `TestRemoveRecord_OtherNoPermission_ReturnsNotFound` |
| F38 | `RemoveRecord` | unknown | NotFound | | `TestRemoveRecord_Unknown_ReturnsNotFound` |
| F39 | `PendingRecords` | no verify extent | Permission | forbidden | `TestPendingRecords_NoExtent_ReturnsPermission` |
| F40 | `OpenCertificate` | record has no certificate | NotFound | | `TestOpenCertificate_None_ReturnsNotFound` |
| F41 | `OpenCertificate` | another's, no read or verify permission | NotFound | | `TestOpenCertificate_OtherNoPermission_ReturnsNotFound` |
| F42 | `OpenCertificate` | audit `Record` fails | its kind; nothing opened | | `TestOpenCertificate_AuditFails_ReturnsError` |
| F43 | `OpenCertificate` | files `Open` fails (blob missing) | Unavailable wrapping `files.ErrBlobMissing` | show | `TestOpenCertificate_BlobMissing_ReturnsUnavailable` |
| F44 | `ListPositions` | `includeArchived` without `positions.manage@D` | Permission | | `TestListPositions_ArchivedWithoutPermission_ReturnsPermission` |
| F45 | `CreatePosition` | no `positions.manage@D` | Permission | | `TestCreatePosition_NotLead_ReturnsPermission` |
| F46 | `CreatePosition` | department archived | Conflict `archived` | | `TestCreatePosition_ArchivedDepartment_ReturnsConflict` |
| F47 | `CreatePosition` | invalid name, access not `one_of` | Invalid | | `TestCreatePosition_InvalidInput_Table` |
| F48 | `CreatePosition` | name taken in the department | Conflict `taken` | | `TestCreatePosition_DuplicateName_ReturnsConflict` |
| F49 | `UpdatePosition` / `ArchivePosition` | unknown | NotFound (positions are visible to all, so only an unknown ID; without `positions.manage@D` it is Permission, as F45) | | `TestUpdatePosition_Unknown_ReturnsNotFound`, `TestUpdatePosition_NotLead_ReturnsPermission` |
| F50 | `ArchivePosition` | future non-canceled shifts exist | Conflict `in_use` | cancel them first | `TestArchivePosition_FutureShifts_ReturnsConflict` |
| F51 | `ArchivePosition` | already archived | Conflict `archived` | | `TestArchivePosition_Twice_ReturnsConflict` |
| F52 | `SetRequirements` | no `positions.manage@D` | Permission | | `TestSetRequirements_NotLead_ReturnsPermission` |
| F53 | `SetRequirements` | a qualification of another department | Invalid `scope` | field error | `TestSetRequirements_OtherDepartmentQualification_ReturnsInvalid` |
| F54 | `SetRequirements` | unknown or archived qualification | Invalid `unknown` / Conflict `archived` | | `TestSetRequirements_ArchivedQualification_ReturnsConflict` |
| F55 | `SetRequirements` | more than `MaxRequirements`; duplicates | Invalid `too_many`/`duplicate` | | `TestSetRequirements_TooMany_ReturnsInvalid` |
| F56 | `ListMembers` | no `rosters.read@D` | Permission | | `TestListMembers_NoPermission_ReturnsPermission` |
| F57 | `ListMembers` | `People.NamesOf` fails | its kind | | `TestListMembers_NamesFail_ReturnsError` |
| F58 | `AddMember` | no `members.manage@D` | Permission | | `TestAddMember_NotLead_ReturnsPermission` |
| F59 | `AddMember` | department archived | Conflict `archived` | | `TestAddMember_ArchivedDepartment_ReturnsConflict` |
| F60 | `AddMember` | person unknown (FK) | Invalid `unknown` | | `TestAddMember_UnknownPerson_ReturnsInvalid` |
| F61 | `AddMember` | already a member | Conflict `duplicate` | | `TestAddMember_Twice_ReturnsConflict` |
| F62 | `AddMember` | zero person | Invalid `required` | | `TestAddMember_ZeroPerson_ReturnsInvalid` |
| F63 | `RemoveMember` | not a member | NotFound | | `TestRemoveMember_NotMember_ReturnsNotFound` |
| F64 | `RemoveMember` | no permission | Permission | | `TestRemoveMember_NotLead_ReturnsPermission` |
| F65 | `Leave` | not a member | NotFound | | `TestLeave_NotMember_ReturnsNotFound` |
| F66 | `ListShifts` | `IncludeCanceled` or past without manage/rosters extent | Permission | | `TestListShifts_CanceledWithoutPermission_ReturnsPermission` |
| F67 | `ListShifts` | members-only shifts of departments the actor is not in | (hidden, not an error) | | `TestListShifts_HidesMembersOnlyForNonMembers` |
| F68 | `GetShift` | invisible shift | NotFound | | `TestGetShift_MembersOnlyNonMember_ReturnsNotFound` |
| F69 | `CreateShift` | no `shifts.manage@D` | Permission | | `TestCreateShift_NotDepartmentLead_ReturnsPermission` |
| F70 | `CreateShift` | department archived | Conflict `archived` | | `TestCreateShift_ArchivedDepartment_ReturnsConflict` |
| F71 | `CreateShift` | position of another department (the composite FK violation translated at the store), archived or unknown | Invalid `scope` / Conflict `archived` / Invalid `unknown` | | `TestCreateShift_Position_Table` |
| F72 | `CreateShift` | event unknown | Invalid `unknown` | | `TestCreateShift_UnknownEvent_ReturnsInvalid` |
| F73 | `CreateShift` | event closed | Conflict `closed` | | `TestCreateShift_ClosedEvent_ReturnsConflict` |
| F74 | `CreateShift` | `Ends <= Starts`, longer than 48h, slots out of range, label invalid | Invalid `range`/`too_long`/`required` | | `TestCreateShift_InvalidInput_Table` |
| F75 | `CreateShift` | no event and zone missing or not IANA | Invalid `required`/`malformed` | | `TestCreateShift_StandingWorkBadZone_ReturnsInvalid` |
| F76 | `CreateShift` | `Events.InfoOf` fails | its kind | | `TestCreateShift_EventsFail_ReturnsError` |
| F77 | `UpdateShift` | unknown, or invisible to the actor (the visibility rule, decision 20) | NotFound | | `TestUpdateShift_Unknown_ReturnsNotFound`, `TestUpdateShift_InvisibleShift_ReturnsNotFound` |
| F78 | `UpdateShift` | canceled | Conflict `canceled` | | `TestUpdateShift_Canceled_ReturnsConflict` |
| F79 | `UpdateShift` | slots below booked | Conflict `full` | | `TestUpdateShift_SlotsBelowBooked_ReturnsConflict` |
| F80 | `UpdateShift` | new time overlaps a booked person's other shift | Conflict `overlap {shift}` | | `TestUpdateShift_CreatesOverlap_ReturnsConflict` |
| F81 | `CancelShift` | visible but no `shifts.manage@D` / unknown or invisible | Permission / NotFound | | `TestCancelShift_VisibleNotLead_ReturnsPermission`, `TestCancelShift_InvisibleShift_ReturnsNotFound` |
| F82 | `CancelShift` | already canceled | Conflict `canceled` | | `TestCancelShift_Twice_ReturnsConflict` |
| F83 | `CancelShift` | `Jobs.Enqueue` not involved; notify `Unsupported` | nil | | `TestCancelShift_NotifyUnsupported_Succeeds` |
| F84 | `CopyShifts` | no permission | Permission | | `TestCopyShifts_NotLead_ReturnsPermission` |
| F85 | `CopyShifts` | `from == to`; unknown event | Invalid `self`/`unknown` | | `TestCopyShifts_SameEvent_ReturnsInvalid` |
| F86 | `CopyShifts` | target event closed | Conflict `closed` | | `TestCopyShifts_ClosedTarget_ReturnsConflict` |
| F87 | `CopyShifts` | source position archived | (skipped, counted in the log) | | `TestCopyShifts_SkipsArchivedPositions` |
| F88 | `CopyShifts` | run twice | second returns 0 | | `TestCopyShifts_Idempotent` |
| F89 | `SignUp` | no `shifts.signup` | Permission | | `TestSignUp_NoPermission_ReturnsPermission` |
| F90 | `SignUp` | invisible shift | NotFound | | `TestSignUp_InvisibleShift_ReturnsNotFound` |
| F91 | `SignUp` | canceled shift | Conflict `canceled` | | `TestSignUp_CanceledShift_ReturnsConflict` |
| F92 | `SignUp` | shift started | Conflict `started` | | `TestSignUp_Started_ReturnsConflict` |
| F93 | `SignUp` | department archived / event closed | Conflict `archived`/`closed` | | `TestSignUp_ClosedEvent_ReturnsConflict` |
| F94 | `SignUp` | members-only, not a member | Conflict `not_member` | | `TestSignUp_NotMember_ReturnsConflict` |
| F95 | `SignUp` | missing requirement (none, pending or expired on the shift date) | Conflict `unqualified {missing}` | show the names | `TestSignUp_Unqualified_Table` |
| F96 | `SignUp` | full | Conflict `full` | | `TestSignUp_Full_ReturnsConflict` |
| F97 | `SignUp` | overlaps a booked shift | Conflict `overlap {shift}` | | `TestSignUp_Overlap_ReturnsConflict` |
| F98 | `SignUp` | already booked | Conflict `duplicate` | | `TestSignUp_Twice_ReturnsConflict` |
| F99 | `SignUp` | two concurrent bookings for the last slot | one `Conflict full` | | `TestSignUp_LastSlotRace_OneWins` (integration) |
| F100 | `Assign` | visible shift, no `shifts.manage@D` | Permission | | `TestAssign_VisibleNotLead_ReturnsPermission` |
| F101 | `Assign` | shift ended | Conflict `ended` | | `TestAssign_Ended_ReturnsConflict` |
| F102 | `Assign` | unqualified without `Waive` | Conflict `unqualified` | | `TestAssign_UnqualifiedNoWaive_ReturnsConflict` |
| F103 | `Assign` | unqualified with `Waive` | nil, `WaivedBy` set and `Waived` true, audited `waived` | | `TestAssign_Waive_RecordsWaiver` |
| F104 | `Assign` | `Jobs.Enqueue` fails | its kind; rolled back | | `TestAssign_EnqueueFails_RollsBack` |
| F105 | `CancelSignup` | another's, no `shifts.manage@D` | NotFound | | `TestCancelSignup_OtherNoPermission_ReturnsNotFound` |
| F106 | `CancelSignup` | own, after the shift started | Conflict `started` | | `TestCancelSignup_Started_ReturnsConflict` |
| F107 | `CancelSignup` | own, inside `cancel_cutoff` | Conflict `cutoff` | contact the lead | `TestCancelSignup_InsideCutoff_ReturnsConflict` |
| F108 | `CancelSignup` | already canceled | Conflict `status` | | `TestCancelSignup_Twice_ReturnsConflict` |
| F109 | `CancelSignup` | lead cancels; notify `Unsupported` | nil | | `TestCancelSignup_ByLead_NotifiesOrLogs` |
| F110 | `Roster` | visible shift, no `rosters.read@D` | Permission | | `TestRoster_VisibleNoPermission_ReturnsPermission` |
| F111 | `Roster` | unknown shift, or invisible to the actor | NotFound | | `TestRoster_Unknown_ReturnsNotFound`, `TestRoster_InvisibleShift_ReturnsNotFound` |
| F112 | reminder handler | sign-up canceled, shift canceled or started | nil, nothing sent | | `TestReminder_StaleSignup_SendsNothing` |
| F112a | reminder handler | `Starts - lead` still ahead (shift moved later, or parked at the horizon) | nil; re-enqueued with the same `Key` at `min(due, now + ReminderHorizon)`; nothing sent | | `TestReminder_DueStillAhead_ReenqueuesAndSendsNothing` (V2: after `Fake.Run`, `jobstest.Fake.Queued()` holds exactly one `volunteers.shift-reminder` row with the sign-up's `Key` at that time; `Enqueued()` alone is not the assertion) |
| F112b | reminder handler | a booking due more than `2 × ReminderHorizon` ahead; the fake clock advanced to each parked time and `jobstest.Fake.RunDue` called | nothing sent at the first and second parked runs; each leaves one `Queued()` row with the same `Key` at `min(due, now + ReminderHorizon)`; at `due` exactly one `volunteers.shift-reminder` is sent, with `Key reminder:<signup>:<starts unix>`; a further `RunDue` sends nothing | | `TestReminder_FarBooking_ParksTwiceThenSendsOnce` |
| F112c | reminder handler (integration) | as F112b against the real `jobs.Runner` on a temporary database with the module's handler registered and the shared fake clock, `RunOnce` at each step | after each parked run the sign-up's row is `queued` (not `done`) at the parked time with `attempts = 0`; at `due` one message in the notify outbox; the row is `done` after | | `TestReminder_FarBooking_RealRunner_ParksThenSendsOnce` |
| F151a | `UpdateShift` | the reminder for a booking was already sent (its row `done`) and the shift is moved later | the row is reopened at the new `Starts - lead` (`Queued()` shows it, `Enqueued()` has two requests); when due, a second reminder is sent with a different notify `Key` (the new `starts`) | | `TestUpdateShift_AfterReminderSent_ReschedulesAndRemindsAgain` |
| F151b | `UpdateShift` | the shift is moved while the reminder handler for a booking is running (the handler is blocked on a channel in the test; `UpdateShift` commits; the handler is released and returns nil) | the row is `queued` at the time `UpdateShift` asked for, not `done` and not at the handler's own time | | `TestUpdateShift_DuringReminderRun_RequestIsNotLost` (jobstest: `Fake.Run` in a goroutine, the handler parked on a channel) |
| F113 | reminder handler | `Send` fails `Unavailable` | Unavailable (jobs retries) | | `TestReminder_NotifyUnavailable_ReturnsUnavailable` |
| F114 | reminder handler | payload lacks `signup` | Invalid (fails, no retry) | | `TestReminder_BadArgs_ReturnsInvalid` |
| F115 | expiry scan | one `Send` fails | continues; returns the first error after the batch | | `TestExpiryScan_OneFailure_ContinuesAndReports` |
| F116 | expiry scan | run twice in a day | second sends nothing (`expiry_notified_on`) | | `TestExpiryScan_Idempotent` |
| F117 | signal handlers | second delivery (one `cancel-shifts` row per key; the job run twice finds nothing) | no-op | | `TestDepartmentArchived_EnqueuesCancelJobOnce`, `TestEventClosed_EnqueuesCancelJobOnce`, `TestCancelShiftsJob_Idempotent` |
| F117a | `cancel-shifts` job | more than `MaxList` future shifts; one batch fails | batches, each its own transaction; the error is returned and the retry resumes | | `TestCancelShiftsJob_Batches`, `TestCancelShiftsJob_BatchFails_ResumesOnRetry` |
| F118 | signal handlers / `cancel-shifts` job | payload or args lack the key | Invalid | | `TestDepartmentArchived_BadPayload_ReturnsInvalid`, `TestCancelShiftsJob_BadArgs_ReturnsInvalid` |
| F119 | `SetUp` | caller has no extent | `Result{Items: 0}`, nil | | `TestSetUp_NoExtent_CopiesNothing` |
| F120 | `SetUp` | one department's copy fails | its error (events reports `Failed`); earlier departments' copies stand (each its own transaction) | run set-up again | `TestSetUp_PartialFailure_ReportsError` |
| F121 | `Sections` | viewer may see none of the person's departments | person absent from the map | | `TestSections_NoExtent_OmitsPerson` |
| F122 | `ExportPerson` / `DeletePerson` | zero ID | Invalid | | `TestExportPerson_ZeroID_ReturnsInvalid` |
| F123 | `DeletePerson` | `files.DeleteByRecord` fails | its kind; people rolls back | | `TestDeletePerson_FilesFail_ReturnsError` |
| F124 | `DeletePerson` | run twice | zero counts, nil | | `TestDeletePerson_Idempotent` |
| F125 | `Start` | called twice | Conflict | | `TestStart_Twice_ReturnsConflict` |
| F126 | `GetShift` | `visibility_shift_mates` is `nobody` | `mates` absent from the body, nil error; `NamesOf` never called | hide the section | `TestGetShift_ShiftMatesNobody_OmitsMates` |
| F127 | `GetShift` | `booked`: actor not booked on the shift / booked | absent / present | hide / show | `TestGetShift_ShiftMatesBooked_Table` |
| F128 | `GetShift` | position has `hide_shift_mates` | absent, whatever the setting | hide | `TestGetShift_HiddenPosition_OmitsMates` |
| F134 | `GetShift` | `holders` (default): actor without `shifts.signup` / with it; `signed_in`: actor without it | absent / present; present | | `TestGetShift_ShiftMatesHolders_Table` |
| F135 | `Roster` | `visibility_legal_name` is `nobody` | no `legal_name` key on any entry; `LegalNamesOf` never called; no `roster.read` entry | | `TestRoster_LegalNameNobody_NeverAsksPeople` |
| F136 | `Roster` | `holders`: viewer holds `people.contacts.read` site-wide or covering the department | `legal_name` present where people knows one; one `volunteers.roster.read` per such entry, `Fields [legal_name]`, `Level personal` | | `TestRoster_LegalNameHolder_ShowsAndAudits` |
| F137 | `Roster` | `holders`: viewer holds `rosters.read@D` but no `people.contacts.read` (a plain lead) | no `legal_name` key; `LegalNamesOf` never called; no audit | | `TestRoster_LeadWithoutContactsRead_NoLegalName` |
| F138 | `Roster` | `holders`: the viewer is on the roster and holds no `people.contacts.read` anywhere (a plain lead on their own shift) (Q-VOL8: no) | no `legal_name` on any entry, their own included; `LegalNamesOf` never called (no `Permission` log line); no audit | | `TestRoster_PlainLeadOwnEntry_NoLegalName` |
| F138a | `Manifest.Settings` / `settings` | `visibility_legal_name` set to `self` (decision 27) | Invalid `one_of` from the setting's `Check`; the option is not offered | pick `nobody` or `holders` | `TestSettings_LegalNameSelf_Refused` |
| F139 | `Roster` | `LegalNamesOf` fails `Permission` / a store kind | every `legal_name` nil, logged once / that kind | show / show error | `TestRoster_PeoplePermission_HidesQuietly`, `TestRoster_PeopleUnavailable_ReturnsError` |
| F140 | `Roster` | the `roster.read` audit `Record` fails | its kind; no roster returned | show error | `TestRoster_AuditFails_ReturnsError` |
| F141 | `Register` / `New` | a `vol` response type with an untagged wire field, or `Manifest.Settings` lacking a `visibility_*` decl | Invalid from `Manifest.Validate` (`bms check`, every test) | fix the type | `TestManifest_Valid` and `moduletest` `views_tagged` |
| F129 | `GetShift` | a booked person is missing from `NamesOf` (deleted since booking) or, defensively, has no playa name (decision 26 refuses the booking, and people P1 makes a set name permanent, so only the fake produces this) | counted in `Unlisted`; no name of any kind | show the count | `TestGetShift_NoPlayaName_CountsUnlisted_NeverLegalName` |
| F130 | `GetShift` | a booked person is in `unlisted` | counted in `Unlisted` | show the count | `TestGetShift_OptedOut_CountsUnlisted` |
| F131 | `GetShift` | the actor is booked | the actor is not in `Names` or `Unlisted`; canceled sign-ups are absent | | `TestGetShift_Mates_ExcludesActorAndCanceled` |
| F132 | `GetShift` | `People.NamesOf` fails | its kind; nothing returned | show error | `TestGetShift_NamesFail_ReturnsError` |
| F133 | `ListShifts` | any mode | `mates` absent on every summary | | `TestListShifts_NeverFillsMates` |
| F142 | `SetListed` | same value twice | nil; no second audit entry | | `TestSetListed_Idempotent` |
| F143 | `UpdateShift` | visible shift (open-access, or members-only in the actor's department), no `shifts.manage@D` | Permission | forbidden | `TestUpdateShift_VisibleNotLead_ReturnsPermission` |
| F144 | `CancelShift` | invisible shift: members-only in a department the actor is not in and no extent | NotFound | NotFound state | covered by F81's second test |
| F145 | `Assign` | invisible shift | NotFound | | `TestAssign_InvisibleShift_ReturnsNotFound` |
| F146 | `Roster` | invisible shift | NotFound | | covered by F111's second test |
| F147 | `RecordsOf` | the person is the actor / another person | `SelfUpload` set to the setting / nil (absent) | show or hide the upload | `TestRecordsOf_Self_CarriesSelfUpload`, `TestRecordsOf_Other_OmitsSelfUpload` |
| F148 | `VerifyRecord` / `RecordQualification` | `NoExpiry` with `ExpiresOn` set | Invalid `one_of` on `no_expiry` | field error | `TestVerifyRecord_NoExpiryWithDate_ReturnsInvalid`, `TestRecordQualification_NoExpiryWithDate_ReturnsInvalid` |
| F148a | `VerifyRecord` | `NoExpiry` on a record the person gave an expiry | nil; `ExpiresOn` zero; `ValidOn` any later date | | `TestVerifyRecord_NoExpiry_ClearsExpiry` |
| F149 | `ListShifts` / `MySchedule` / `ListMembers` | `after` does not decode, or its ID is malformed; `limit` out of range | Invalid `malformed` on `after` / `range` on `limit` | field error | `TestListShifts_BadCursor_ReturnsInvalid`, `TestMySchedule_BadCursor_ReturnsInvalid`, `TestListMembers_BadCursor_ReturnsInvalid` |
| F149a | `ListShifts` | more rows than `Limit`; a department archived since the handlers ran | `Next` set from the fetched rows; the archived department's rows dropped, the page short, `Next` still set | follow `Next` | `TestListShifts_Pages_NeverSkips`, `TestListShifts_ArchivedDepartment_ShortPageKeepsNext` |
| F149b | `MySchedule` | past mode | newest first; booked rows only; paged | | `TestMySchedule_Past_NewestFirst` |
| F150 | `CopyShifts` | source and target straddle a DST change, or differ in zone | wall-clock start and end kept in the destination zone; a shift inside a skipped hour keeps its duration | | `TestCopyShifts_AcrossDST_KeepsLocalTimes`, `TestCopyShifts_DifferentZone_KeepsLocalTimes` |
| F151 | `SignUp` / `Assign` / `UpdateShift` | `Starts - lead` beyond `now + ReminderHorizon` | the reminder is enqueued (or moved) to `now + ReminderHorizon` with the sign-up's `Key`; never dropped | | `TestSignUp_FarFuture_ParksReminderAtHorizon`, `TestUpdateShift_MovedBeyondHorizon_ParksReminder` |
| F152 | `Assign` | `Person == actor`, unqualified, with `Waive` (Q-VOL7: yes) | nil; `WaivedBy` = the actor; audited `signup.created` with `Reason waived`, `Actor` = `Subject`; the roster flags the waiver | show the flag | `TestAssign_SelfWaive_RecordsWaiver` |
| F152a | `Assign` | `Person == actor`, eligible, no `Waive` | nil; `BookedAs lead`; `WaivedBy` zero | | `TestAssign_SelfEligible_Succeeds` |
| F153 | `MySchedule` | a booking canceled by a lead or by the system, shift not ended / canceled by the person / shift ended (Q-VOL9) | listed with `cancel_reason` / not listed / not listed in upcoming mode | show struck through with the reason | `TestMySchedule_OtherCanceled_ShownWithReason`, `TestMySchedule_SelfCanceled_Hidden` |
| F154 | `ExportPerson` | the booking lead or canceling lead was deleted | `booked_by` from `booked_as`, `canceled_by` from `cancel_reason`: role words unchanged | | `TestExportPerson_AfterLeadDeleted_KeepsRoles` |
| F154a | `ExportPerson`, `Roster` | the lead who waived a requirement was deleted (`waived_by` nulled) | `waived` stays true in the export and the roster still flags the waiver (D51) | | `TestExportPerson_AfterWaivingLeadDeleted_KeepsWaived`, `TestRoster_AfterWaivingLeadDeleted_KeepsWaiverFlag` |
| F155 | `ExportPerson` | the module holds nothing about the person | `{}`: no `listed` key | | `TestExportPerson_Unknown_IsEmpty` |
| F156 | list handlers | empty result | body `{"items":[]}`, never `{}` | render empty state | `TestListHandlers_Empty_EncodeEmptyItems` (table over the seven list routes) |
| F157 | `Sections` | the viewer's extent covers more than `MaxIDs` departments | names fetched in chunks; every department named | | `TestSections_ManyDepartments_ChunksNamesOf` |
| F158 | `GetShift` | past shift the actor could otherwise see | returned (no `Permission`, no `NotFound`) | render with "ended" | `TestGetShift_PastVisible_Returns` |
| F159, F159a, F159b, F160 | `SignUp` / `Assign` | *withdrawn (2026-10-22):* the per-event `name_taken` rule and its waiver case; people P1 makes names unique across the installation, so the condition cannot arise here | | | (no tests; the numbers stay withdrawn) |
| F161 | `SignUp` / `Assign` | any shift, event or standing; the person has no playa name (`""` or absent from `NamesOf`), with or without `Waive` (decision 26) | Conflict `unnamed`; refused before the shift lock; nothing inserted, no audit, no signal | link to the profile (self) or say the person needs a name (lead) | `TestSignUp_NoPlayaName_ReturnsUnnamed`, `TestAssign_NoPlayaName_ReturnsUnnamed` (table: event shift, standing shift, `Waive` on and off) |
| F161a | `SignUp` / `Assign` | the person has a playa name; another person booked on the same or another shift has any name people allowed | nil: booked; no name comparison is made (people P1 guarantees distinct names) | | `TestSignUp_Named_NoNameComparison` (the `peopletest` fake records one `NamesOf` call with one ID) |
| F162, F163, F165 | | *withdrawn (2026-10-22):* the standing-shift exemption, the name race and `vol.NameKey` went with the per-event rule | | | |
| F164 | `SignUp` / `Assign` | `People.NamesOf` fails during the playa-name precondition | its kind; rolled back, nothing booked | show error | `TestSignUp_NamesFailDuringNameCheck_RollsBack` |
| F166 | `UpdateShift` | input `Event` differs from the shift's (including zero for a shift that has one, or set for standing work) (decision 28) | Invalid `scope` on `event` | field error; cancel and recreate, or copy | `TestUpdateShift_ChangesEvent_ReturnsInvalid` |

Methods without failure modes beyond the common ones (`MyDepartments`, `Listed`, `ListQualifications` of active entries) read only the actor's own rows or `internal` catalogue rows, so no permission, state or input check can fail; `MySchedule` adds only the page and window validation of F149; `SetListed` takes a bool about the actor, so only the common write failures and F142 apply. `privacy.Filter`'s own failures (`Internal` for an unchecked type) cannot occur at run time because `Manifest.Validate` runs `Check` over every `vol` response type (F141).

## Lock-in check

No external technology is wrapped. Certificate storage, e-mail and scheduling go through `files`, `notify` and `jobs`, each with an OSI-licensed implementation (PostgreSQL chunk store, SMTP/Mailpit, PostgreSQL queue). Nothing here depends on a vendor.

## Test plan

- **Unit** (fakes: `dbtest`, `authztest`, `peopletest`, `departmentstest`, `eventstest`, `audittest`, `signalstest`, `notifytest`, `settingstest`, `jobstest`, `filestest`, fake clock): every row above; eligibility as a table (`TestEligibility_Table`: each rule on and off, expiry on the shift's date in the zone, DST edge); `Date` round trips; the reminder key and time; the `vol` validators.
- **Permission tests:** one per permission per method, including the D41 pair (department lead cannot verify another department's qualification; any lead verifies an org-wide one; a participant can never verify or self-attest: `TestVerify_Participant_ReturnsPermission`, `TestUploadCertificate_NeverCounts`).
- **Privacy:** `telemetrytest` proves no playa name, legal name or file name in logs or spans; `notifytest.Sent()` vars carry only the recipient's data; signals carry IDs only; the shift-mate list is proven to carry playa names only (`TestGetShift_Mates_PlayaNamesOnly`: the JSON of `ShiftSummary` has no person ID, pronoun or legal-name marker from the seed, with `peopletest` seeded with a person whose playa name is empty, F129) and the shift-mate rule as a table over audience × position flag × booked × `shifts.signup` held (`TestShiftMates_Table`); the roster's legal name as a table over audience × `people.contacts.read` extent × self (`TestRosterLegalName_Table`, with `privacytest.AssertHidden`/`AssertPresent` on the handler's body and the `audittest` entries checked for `volunteers.roster.read`); `Check(Manifest.Views, Groups)` passes (`TestViews_Tagged`).
- **Waiver tests** (D42): `TestSignUp_NeverWaives` (a `SignUp` body cannot carry `waive`; an unqualified self sign-up is `Conflict unqualified` whatever the lead holds) alongside F102-F103.
- **Integration** (`//go:build integration`, PostgreSQL container): migrations on empty and previous; the booking race (F99); the far reminder against the real `jobs.Runner` (F112c: `jobs.New` with the test's fake clock and a `dbtest.TempDB`, the module's `Start` registering the handler, `RunOnce` after each clock step; this is the test that catches a fake that drifts from the queue); the partial unique indexes; `files` postgres store round trip for a certificate; the purge.
- **Conformance:** `moduletest.Conformance(t, Manifest, New, provision, WithSeed(seed))` where the seed books the subject on a shift, adds a membership and a verified record with a certificate whose file name is neutral (`certificate.pdf`; a marker in the name would trip the rig's `no_personal_data_in_logs` rule through files' own log lines, and the file name is `personal` by the Data table, so it must never reach a log). That the certificate's name never reaches a log or span is proven separately by `TestOpenCertificate_NameNotLogged` with `telemetrytest` and a marker-named upload.
- **UI:** `web-reviewer` at the phone viewport; smoke: sign up, see it in My volunteering, cancel; lead creates a shift, assigns, verifies a certificate.

## Later

Named so the boundary is explicit (each a change to this spec, not a new module): sign-up approval by a lead before it counts (`volunteers.approval_required` per position); swaps; check-in/check-out with hours credit as dated records (D18); walk-on desk mode; anchored shifts (`ScheduleOf`); join requests; volunteer status from recent activity; per-position shift templates; the `qualifications` capability `contracts/qualifications` (`Name = "qualifications"`; `Holds(ctx, people []model.PersonID, key string) (map[model.PersonID]bool, error)` over a `key` token added to the catalogue, so `incidents` can ask "is this person a trained ranger" without this module's IDs); a `departments.department-page` section; pronouns next to shift-mates' playa names if the owner wants them (D42 said names only; it would be one more field in the `shift_mates` group); a `consented:<purpose>` audience for the listing if a regional wants opt-in (privacy-tags, "Later"); `events.schedule-changed`; dropping a disabled account from upcoming shifts (identity's unpublished signal); if people ever lets a playa name change or retire (P1: Later), whatever this module must do with the roster of a past event then (nothing is expected: the roster shows the name people reports at read time, which stays unique).

## Amendment V1 (raised 2026-10-21, approved 2026-10-23, D49): what the build and the reviews found

Raised by `go-reviewer`, `web-reviewer` and the main session against the Approved text while the module was built (`internal/modules/volunteers`, `web/src/modules/volunteers`). Each item names the choice, the reason and the alternative not taken. The inline edits above are now the spec's text (approved 2026-10-23, D49). Three items needed the owner (Q-VOL7 to Q-VOL9); the owner answered on 2026-10-22 and the answers are folded in below (V1.3, V1.5, V1.9 (d) and V1.10); the two follow-up questions V1.10 raised (Q-VOL10, Q-VOL11) were answered the same day and moved the name rule into `people` (Amendment P1), which V1.10 now records; the rest are technical and settled here.

**V1.1 `Permission` or `NotFound` for a shift.** The failure-mode table said `NotFound` for a shift "outside the extent" (F77) and `Permission` for "no permission" (F81, F100, F110), and the build answered `Permission` for any shift the caller lacked `shifts.manage@D` on, including one they could not see, which confirms that the ID exists (privacy-data: a `403` never confirms an ID). *Chosen:* decision 20: every shift method runs the `ListShifts` visibility rule for the actor first; invisible or unknown is `NotFound`, visible-but-not-allowed is `Permission`. A lead of another department sees an open-access shift (everyone does) and gets `Permission` on it; a members-only shift of a department they are not in gets `NotFound`. Qualifications, positions, departments and events are visible to every signed-in person, so `Permission` stays on their write methods and `NotFound` means unknown (F10, F45, F49). *Not taken:* `NotFound` everywhere for a missing permission, which would make a lead's typo on a visible shift look like a dead link.

**V1.2 Paging.** The SQL had a keyset cursor but the API exposed `limit` only; the UI asked for 500 and silently lost the rest; dropping inactive departments after the `LIMIT` could return a short page that looked like the end; `MySchedule` in past mode sorted oldest-first with a cap of 500 (losing the newest); `ListMembers` capped at 500 with no page. *Chosen:* decision 22: one `Page{Limit, After Cursor}` embedded in `ShiftQuery`, `ScheduleQuery` and the new `MemberQuery`; the three return `ShiftList`, `ScheduleList`, `MemberViewList` with `Next`; `Limit + 1` rows decide `Next`, computed before Go-side filtering so a short page never skips; the cursor is opaque (base64url of key and ID) so the sort key can change without an API change; `MySchedule` has an upcoming mode (soonest first) and a past mode (newest first) instead of `IncludePast`. The UI shows **Show more** (phone-first; no page numbers). *Not taken:* offset paging (`OFFSET` drifts while bookings arrive) and filtering inactive departments inside the query (the module may not read `core.department`, and the handlers already cancel an archived department's future shifts, so the Go-side drop is a rare race, not the rule).

**V1.3 A person's own legal name under `visibility_legal_name = self`.** F138 said the viewer's own roster entry carries `legal_name`, but people's `LegalNamesOf` (D44, D46) answers the actor's own ID only when `people.contacts.read` is held somewhere (people F67), so a plain lead on their own roster never saw it. *Chosen (recommended to the owner, Q-VOL8):* Proposed change 11 to `people.md`: `LegalNamesOf` answers the actor's own ID without the permission (a self read, unaudited, like `Me`), and keeps `Permission` only when `ids` names anyone else and the permission is held nowhere. The person can already read their own legal name on `/me`, so nothing new is revealed, and the `self` audience (privacy-tags) means "the subject sees it" without a permission. Until applied, F138 holds only for a viewer who holds the permission somewhere and the interim test pins it. *Not taken:* relaxing F138 for good (then `self` would be a setting that works for coordinators and not for the people it names). **Owner's answer (2026-10-22): no.** "Everyone sees burn names"; legal names stay with the permission holders. Proposed change 11 is dropped, people is not changed, and the `self` option is removed from the `legal_name` group (decision 27): with people answering only holders, `self` could show a holder their own name and nobody else's, which `holders` already does for the subject (privacy-tags' rule) and which would hide from the coordinator exactly what the role exists to see. F138 now pins that a non-holder on their own roster gets no legal name and that people is never asked (no wasted call, no `Permission` log line); F138a pins that the setting refuses `self`. The same answer adds a requirement, recorded as V1.10.

**V1.4 What the screens could not know.** (a) A volunteer could not tell whether `self_upload` is on (`GET /api/settings` needs `settings.read`), so the upload action showed and then failed `Unsupported`. *Chosen:* `RecordList.SelfUpload *bool`, set only on a person's own `GET /people/{person}/qualifications`; the screen that shows the action is the screen that lists the records, so no new route, and a value scoped to the one setting the person may act on. *Not taken:* a `/me` document (nothing else needs one yet) or exposing the settings route. (b) The shift list fetched positions per department to label cards. *Chosen:* `ShiftSummary.DepartmentName` and `PositionName`, as `ScheduleEntry` already had, one `Departments.NamesOf` per page.

**V1.5 Self-waiver (Q-VOL7).** `Assign` with `Person == actor` and `Waive` let a lead book themself without the qualification, which is self-attestation by another door (D41: never self-attested; D42: volunteers never waive for themselves, and a lead is a volunteer on that shift). *Chosen (recommended):* `Invalid self` on `waive` (decision 21, F152); the lead may still assign themself when eligible (F152a), and another lead of the department waives if needed. The UI disables the waive switch when the chosen person is the viewer. *Not taken:* allowing it with an audit entry (an audited breach is still a breach of D41). **Owner's answer (2026-10-22): yes, a lead may waive for themself.** Decision 21 is withdrawn, the `Invalid self` rule, the disabled switch and `TestAssign_SelfWaive_ReturnsInvalid` go; a self-waiver is an ordinary waiver (`waived_by` = the actor, `Reason waived`, flagged on the roster), and F152 now pins that (`TestAssign_SelfWaive_RecordsWaiver`). The audit entry whose actor and subject are the same person is the record a regional reviews; the `burn-domain` sentence "volunteers can never waive a requirement for themselves" needs the qualifier "a lead may, for themself, as a recorded waiver" (Proposed change 13).

**V1.6 The reminder and `jobs.MaxDelay`.** A booking more than 366 days ahead got no reminder, and an `UpdateShift` that moved a shift beyond the horizon left the old queued row, which fired early. *Chosen:* decision 25: `ReminderHorizon = jobs.MaxDelay - 24h`; booking and update enqueue at `min(due, now + horizon)` with the sign-up's `Key`; the handler recomputes `due` from the shift and the current `reminder_lead` and re-enqueues itself (same `Key`) when `due` is still ahead (F112a, F151). A day of slack keeps the request inside `MaxDelay` whatever the clock skew between the enqueueing request and the runner. *Not taken:* dropping far reminders (a year-ahead booking is exactly the one that needs reminding) or a daily scan (one more job for the same effect).

**V1.7 Copying across DST or zones.** `CopyShifts` moved each shift by the instant difference of the events' starts, so a copy across a DST change moved "08:00" to "07:00" or "09:00". *Chosen:* decision 24: whole-day civil delta, wall-clock times re-anchored with `time.Date` in the destination event's zone (F150). Leads think in local times; the rare shift inside a skipped hour keeps its duration. *Not taken:* copying instants (wrong for every spring and autumn burn) or asking the lead (one more step for a thing software can decide).

**V1.8 Choices the reviewers accepted, now in the text.** (a) `Record.Verified()` is `!VerifiedAt.IsZero()`: `VerifiedBy` is nulled by that lead's deletion and the record must keep counting. (b) The composite FK `shift(position_id, department_id) → position(id, department_id)` backs decision 6 and is translated to `Invalid scope` (F71). (c) Export writes `listed` only when the module holds something; an unknown person exports `{}` (F155). (d) `GetShift` returns a past shift the actor could otherwise see (F158): the list's "from now on" is about volume. (e) `Eligibility` is `Eligible false, Reason ""` when the viewer already holds a booking; the screen reads `mine`. (f) `RecordQualification` audits `created` and `verified` (two entries: two facts). (g) List wrappers' `items` are `omitzero` over a non-nil slice so an empty list is `{"items":[]}` (F156); the Approved text's `omitempty` dropped the key and every client had to special-case it. (h) `Sections` chunks `Departments.NamesOf` by `MaxIDs` (F157). (i) The conformance seed's certificate name is neutral; a separate `telemetrytest` test proves a marker-named upload never reaches a log (the Test plan line that put markers in the name contradicted the rig's `no_personal_data_in_logs`). (j) The signal handlers no longer cancel a whole department in one transaction: they enqueue `volunteers.cancel-shifts` (decision 23, F117, F117a), which cancels in batches of `MaxList`; "handlers stay quick" is module-contract's rule and a department can hold hundreds of future shifts with thousands of bookings, each with an audit entry, a signal and a notice. *Not taken:* batching inside the handler (the delivery timeout bounds it, and a retry would re-run from the start). (k) A department-filtered `PendingRecords` includes every org-wide pending record: intended, since D41 lets any lead verify those; the Pending tab labels them. (l) `ShiftQuery.Within` accepts either bound alone, as `ScheduleQuery` did; `Start >= End` is `Invalid range`. (m) `signup.booked_as` (migration `0002`) keeps who booked after a lead's deletion, and the export derives `canceled_by` from `cancel_reason` (F154). (n) The `signup-removed` template's Vars keep `reason` and add one boolean per reason (`is_lead`, `is_self`, `is_shift_canceled`, `is_department_archived`, `is_event_closed`): notify's checker allows only `term`, so the template branches on a flag rather than comparing strings, and names the department and event through `{{term ...}}`.

**V1.9 From the web review.** (a) `VerifyRecord` treated a zero `ExpiresOn` as "keep", so a lead could not clear an expiry the volunteer entered on a certificate that never expires. *Chosen:* `NoExpiry bool` on `VerifyInput` and `RecordInput` (`Invalid one_of` with a date, F148, F148a); the forms get a **Never expires** switch. *Not taken:* `expires_on: null` versus omitted (invisible in a form and in OpenAPI). (b) `Assign` needs `shifts.manage@D` but sat inside the roster section (`rosters.read@D`), so a manager without roster rights could not assign. *Chosen:* a **Manage** section gated by `shifts.manage@D`, independent of the roster section. (c) Opening a certificate followed a raw link, so a 404 or 503 showed problem JSON in a new tab. *Chosen:* the shell's `Attachments` row (files Proposed change 7): fetch through `@/api`, 404 → "no longer available", 503 with `Retry-After` → "not available on this copy yet". (d) `MySchedule` hid canceled bookings, so a booking a lead removed vanished without a word. *Chosen (recommended, Q-VOL9):* upcoming mode lists bookings canceled by someone else, with the reason, until the shift ends (F153); self-cancellations stay hidden (the person did it); past mode lists booked rows only. **Owner's answer (2026-10-22): yes.** Kept as written.

**V1.10 Playa names: unique in people, a precondition here (owner, 2026-10-22, with the Q-VOL8, Q-VOL10 and Q-VOL11 answers).** The Q-VOL8 answer said "Burn names also need to be enforced to be unique when registering to volunteer for that event", and the first draft of this item read it as a per-event rule for this module: a `name_taken` check in `SignUp` and `Assign` comparing names from `NamesOf` under a `vol.NameKey`, serialised by an advisory lock on `(event, NameKey)`, with no lead override (Q-VOL10) and a rename after booking left uncaught (Q-VOL11). The owner's answers to Q-VOL10 and Q-VOL11 (verbatim under Open questions) settle it differently: **the system does not allow duplicate burn names at all**, nobody may rename into a clash, names are permanent and self-appointed for the first deployment, and retirement, approval pipelines and several names per person (an alias plus a radio handle, only the latter unique) come later. That is a rule about the profile, so it belongs to `people`, which is drafting **Amendment P1** in parallel: a name key (`people.NameKey`, trim and case-fold, the same comparison people's index already made), a unique index on it, `Conflict taken` on `UpdateMe` and `Create`, and the playa name read-only once set. *Chosen (decision 26, reduced):* this module drops the per-event rule entirely (no comparison, no name lock, no `vol.NameKey`, no `name_taken`; F159, F159a, F159b, F160, F162, F163, F165 withdrawn, numbers kept) and keeps one precondition: a person booking any shift must have a playa name (`Conflict unnamed`, F161, with or without `Waive`), now for standing work too (F161 table), because the roster, the shift-mate list and the radio go by that name whether or not the shift has an event, and the earlier exemption for standing shifts only existed because the clash check had no event to run in. The lock order is back to person → shift (decision 7). *Why uniqueness lives in people and not here:* a module cannot enforce an invariant over data it does not own (a rename in people would have broken the per-event check silently, which is what Q-VOL11 was about); people's unique index enforces it once for every module and screen, and people's "read-only once set" closes the rename hole without a signal. *Not taken:* (a) keeping a belt-and-braces check here against the day people relaxes the rule: two rules for one fact drift, and if people ever allows several names or retirement it is people's design that must keep "the name shown is unique", not each module's; (b) dropping `unnamed` as well: a booking with no name is a blank line on the roster and the radio list, and the shell prompts for a name once per session, so the refusal costs the person a minute and the lead nothing. *Decision 28 kept:* with the per-event rule gone nothing in the booking path depends on the event being fixed, but the zone and the dates each booking was judged against come from the event (decision 6), `events.event-closed` cancels by event (decision 12) and `CopyShifts` dedupes by `copied_from` (decision 11); a moved shift would carry bookings, a zone and a copy record into an event none of them was checked for; `Invalid scope` on `event` stays (F166) and a lead cancels and recreates, or copies. *UI:* the sign-up and Assign messages for `name_taken` go; `unnamed` keeps its two messages (UI table). *Dependency:* Proposed change 14 records that this module relies on people P1 for the uniqueness of the names it shows and uses none of P1's exported names.

**Test expectations that change** (everything else stands):

| Test | Before | After V1 |
|---|---|---|
| `TestUpdateShift_Unknown_ReturnsNotFound` (F77) | unknown or outside the manage extent | unknown or invisible; new `TestUpdateShift_InvisibleShift_ReturnsNotFound`, `TestUpdateShift_VisibleNotLead_ReturnsPermission` (F143) |
| `TestCancelShift_NotLead_ReturnsPermission` (F81) | any shift without manage | renamed `TestCancelShift_VisibleNotLead_ReturnsPermission` (open-access shift); new `TestCancelShift_InvisibleShift_ReturnsNotFound` (members-only, not a member) |
| `TestAssign_NotLead_ReturnsPermission` (F100) | any shift | renamed `TestAssign_VisibleNotLead_ReturnsPermission`; new `TestAssign_InvisibleShift_ReturnsNotFound` (F145) |
| `TestRoster_NoPermission_ReturnsPermission` (F110) | any shift | renamed `TestRoster_VisibleNoPermission_ReturnsPermission`; new `TestRoster_InvisibleShift_ReturnsNotFound` (F111, F146) |
| `TestUpdatePosition_Unknown_ReturnsNotFound` (F49) | unknown or outside extent | unknown only; new `TestUpdatePosition_NotLead_ReturnsPermission` |
| every `ListShifts`, `MySchedule`, `ListMembers` call | a slice | `ShiftList`/`ScheduleList`/`MemberViewList` with `Items` and `Next`; `ListMembers` takes `MemberQuery`; `ScheduleQuery.Past` replaces `IncludePast`; new F149, F149a, F149b tests |
| every `RecordsOf` call | a slice | `RecordList`; new F147 tests |
| `TestRoster_LegalNameSelf_OwnEntryOnly`, `TestRoster_LegalNameSelf_PlainLead_NoLegalName` (F138) | the `self` audience | both removed (Q-VOL8: no, decision 27); `TestRoster_PlainLeadOwnEntry_NoLegalName` (F138: no call to people), `TestSettings_LegalNameSelf_Refused` (F138a); `TestRosterLegalName_Table` loses its `self` column |
| `TestAssign_Waive_RecordsWaiver` (F103) | any person | unchanged (Q-VOL7: yes); `TestAssign_SelfWaive_RecordsWaiver` (F152: the actor waives for themself, `WaivedBy` = actor), `TestAssign_SelfEligible_Succeeds` (F152a); `TestAssign_SelfWaive_ReturnsInvalid` removed |
| `TestSignUp_*`, `TestAssign_*` success paths | the seed's people have any names | every booked person in the `peopletest` seed has a playa name (any shift, event or standing; a nameless person is `unnamed`, F161); new F161, F161a, F164 tests; no `name_taken`, no `NameKey`, no name-race test (F159, F160, F162, F163, F165 withdrawn, 2026-10-22) |
| `TestUpdateShift_*` | `ShiftInput.Event` free | `Event` must equal the shift's; new `TestUpdateShift_ChangesEvent_ReturnsInvalid` (F166) |
| `TestShiftMates_Table`, `TestGetShift_Mates_*` | duplicates kept in `Names` | the duplicate-name case is removed: fixtures use distinct playa names, as people P1 guarantees; `Names` is asserted sorted, never asserted to repeat |
| `TestReminder_StaleSignup_SendsNothing` (F112) | includes "shift moved later" as stale | moved-later is F112a: `TestReminder_DueStillAhead_ReenqueuesAndSendsNothing` (asserts `jobstest.Fake.Enqueued()` has the same `Key`) |
| `TestDepartmentArchived_Idempotent`, `TestEventClosed_Idempotent` (F117) | the handler cancels; a second `Deliver` is a no-op | the handler enqueues once (`jobstest.Fake.Enqueued()`); `TestCancelShiftsJob_Idempotent` runs the job twice with `Fake.Run`; new F117a tests; F118 adds `TestCancelShiftsJob_BadArgs_ReturnsInvalid` |
| `TestCopyShifts_*` (F84-F88) | instant delta | wall-clock in the destination zone; new F150 tests with a fake clock across a DST change |
| `TestExportPerson_*` | `booked_by`/`canceled_by` role from the ID columns | from `booked_as` and `cancel_reason`; new F154, F155 tests; D51: `waived` from the column (F154a) |
| `TestVerifyRecord_*` (F28-F32) | `VerifyInput{AcquiredOn, ExpiresOn}` | plus `NoExpiry`; new F148, F148a tests |
| list handler tests | empty list → `{}` accepted | `{"items":[]}` (F156, table over the seven list routes) |
| `TestMySchedule_*` | canceled rows absent | F153 tests; past mode newest first |
| conformance seed | certificate name carries markers | neutral name; `TestOpenCertificate_NameNotLogged` |
| `TestStart_Twice_ReturnsConflict` (F125), `TestManifest_Valid` | three jobs | four jobs (`volunteers.cancel-shifts`); migration version 2 |

## Amendment V2 (raised 2026-10-23, approved 2026-10-23, D50): the far reminder never fired

**V2.1 What the build found.** Decision 25 (V1.6) had the reminder handler carry a parked reminder forward by enqueueing its own `Key` from inside its run. In the real queue the row is `running` at that moment, and jobs decision 3's upsert touched only `queued` rows, so the request was a silent no-op: the handler returned nil, the row ended `done`, and a booking further ahead than `ReminderHorizon` (about a year) was never reminded. `jobstest.Fake` recorded the request without a row state, so F112a passed. A move by `UpdateShift` or a new `SignUp` while the row was still `queued` rescheduled correctly. Looking for the same shape elsewhere found a second case the build had not reached: once a reminder is sent its row is `done` for `jobs.DoneTTL` (seven days), and V1's sentence "a shift moved after a reminder was sent is reminded once more for its new time" was false in that window, because that `Enqueue` was a no-op too.

**V2.2 Chosen: fix the queue, not the module (jobs Amendment J1).** The defect is a state of the jobs contract in which a keyed request vanishes without a word, so the fix is there: jobs decision 23 makes "the latest request for a `(name, key)` wins" in every state (a `queued` row moves, a final row is reopened, a request during a run is held on the row and applied when the run ends, whatever its outcome). With that rule the handler as written is correct, `UpdateShift` during the run cannot lose its move (F151b), and the seven-day hole closes (F151a). Nothing in this module's code changes; decision 25, the Behavior paragraph and the Summary now name the rule they rest on, and the "Expects of dependencies" line says what breaks without it. *Not taken:* (a) continuation keys here (`<signup>:<hop>` or `<signup>:<starts>`): the handler could park itself under a new key, but `UpdateShift` has no way to cancel the parked row (modules have no `Cancel`), so every move would leave a live row that fires, re-reads and re-parks under yet another key; correctness under "move while parked" would then rest on the handler's re-read plus a `reminder_sent_for` column on `signup` for the send, the seven-day hole would need a third key component, and `/jobs` would fill with hop keys; (b) a daily `reminder-scan` job replacing the deferred reminder (scan sign-ups whose `Starts - lead` falls in the next day, dedupe by a column): removes the horizon but adds a job, a column and a scan over every future sign-up each day, and leaves the jobs trap for the next module; (c) a jobs sentinel `RunAgainAt(t)` returned by the handler: fixes the carry-forward alone and leaves the seven-day hole and the move-during-run window (jobs J1 "Not taken").

**V2.3 Why the tests missed it, and what changes.** F112a called the handler through `jobstest.Fake.Run` and asserted `Fake.Enqueued()` had the key: it proved the handler *asked*, not that the queue would *hold* the request. J1 gives the fake a row per key with the real states, `Queued()` (what is scheduled now) and `RunDue` (run what is due at a fake-clock time), and the rule for asserting a schedule is: assert `Queued()`, drive it with `RunDue`, and keep one integration test on the real runner (F112c), because a fake that models state can still drift and only the real queue is the truth. **Test expectations that change:** `TestReminder_DueStillAhead_ReenqueuesAndSendsNothing` (F112a) asserts `Queued()`; new `TestReminder_FarBooking_ParksTwiceThenSendsOnce` (F112b), `TestReminder_FarBooking_RealRunner_ParksThenSendsOnce` (F112c, integration), `TestUpdateShift_AfterReminderSent_ReschedulesAndRemindsAgain` (F151a), `TestUpdateShift_DuringReminderRun_RequestIsNotLost` (F151b). F151's two tests are unchanged. The `cancel-shifts` job (key `<reason>:<department or event ID>`) also benefits: a department archived, restored and archived again within seven days now runs the job again instead of silently not (no new test: F117a covers the rerun once J1's fake applies).

**D51 note (2026-10-23, owner, with the web-shell and people follow-ups).** (c) *The waiver flag survives the waiving lead's deletion.* `Assign`'s `waived_by` is nulled when that lead is deleted, so the export's `waived` and the roster's flag, which read `waived_by`, silently went false. *Chosen:* `signup.waived boolean NOT NULL`, set true when `Assign` waives and backfilled true where `waived_by is not null`, carried by migration `0002` with `booked_as` (it has never shipped); the export and the roster read the column, `waived_by` stays the audit link while it lasts. New F154a with `TestExportPerson_AfterWaivingLeadDeleted_KeepsWaived` and `TestRoster_AfterWaivingLeadDeleted_KeepsWaiverFlag`; F103 and F152 also assert `Waived`. (e) *My volunteering's upcoming/past switch* is two toggle buttons (`aria-pressed`) in a labelled group, not tabs: it switches a query, not a set of panels (UI table).

**Owner decision.** Jobs Q-JOBS14 (accept J1) was answered yes (owner, 2026-10-23, D50); options (a) to (c) above stay as the record of what was not taken.

## Amendment V3 (raised 2026-10-28, Approved 2026-10-30, D57): regional words for shift, position, qualification and lead

**V3.1 The problem.** The screens and templates of this module say "shift", "position", "qualification" and "lead" as fixed English words (the catalogue has about 110 "shift", 46 "position", 57 "qualification" and 15 "lead" occurrences), while "department" and "event" are the regional's words through `{term_department}` and `{term_event}`. Regionals name these things differently (`burn-domain`, "How regionals differ": terminology, and position names), and the project rule is that a regional changes a word in a setting, not in code (`add-module` §3). Proposed change 4 asked `settings` for module-declared terms but left two things undecided, which D52 records: the rule when two areas declare the same term, and where a module's defaults live, since both Go (`TermsFor`, notify's `{{term}}`, the Terminology screen's placeholders) and the shell (the boot fallback before `GET /api/settings/public` answers, or while it fails: settings F9) need them. This amendment settles both and names what this module declares. Nothing in it changes a service method, a table, a permission or a route of this module.

**V3.2 Options.**

- **(a) Add the four words to `settings.CoreTerms`.** About twenty lines: `terms/en.json`, the shell's `term.*` entries, and this module's messages. *Not taken:* the core would carry one module's vocabulary, a regional's own module could not add a word without editing the core (principle: features are modules added without changing the core), a disabled `volunteers` would still show "Shift" on the Terminology screen, and every later module repeats the request.
- **(b) Module-declared terms in the manifest, one flat namespace, a shared word promoted to the core.** `Manifest.Terms []TermDecl` with the `en` defaults in Go; the term's identity is its bare key everywhere (`shift`); two enabled areas declaring the same key is a start-up `Invalid`, and the fix is to move the word to `CoreTerms`, as `department` already is. **Chosen**, detail in V3.3.
- **(c) Module-declared terms, namespaced** (`term_volunteers_shift`, stored as `volunteers.shift`). No collision by construction. *Not taken:* every message and template would spell the module's name inside the word, a word two modules need (lead) could never be one term, and a regional that swaps this module for a fork under another name loses its overrides. The one hazard (b) has, a collision, is a build-time error with a one-line fix; (c) pays for it in every message forever.
- **(d) Terms as ordinary `Text` settings in the module's area** (`volunteers.term_shift`, `..._plural`, ...). *Not taken:* twelve rows on the module's settings screen, one language only (a setting has one value, terms have one per language), not public (the shell could not fetch them at boot) and invisible to notify's `term` function, so it would be a second terminology mechanism beside the first.

**V3.3 Chosen: (b), the mechanism.** All of it is a change to `settings`, `module-contract`, `web-shell` and `app` (Proposed changes 16 to 19); this module only declares and uses.

1. **`settings.TermDecl`** (new; alias `module.TermDecl`, and `module.TermForms = settings.Forms`, as `module.SettingDecl` is an alias, settings decision 2):

   ```go
   // TermDecl declares a word a regional may rename. Key is the term's identity
   // everywhere: core.terminology.term, TermsDocument, the shell's {term_<key>} and
   // {term_<key>_plural|_title|_plural_title}, and notify's {{term "<key>"}}.
   type TermDecl struct {
   	Key      string           // [a-z][a-z0-9]*(_[a-z0-9]+)*, at most 32 bytes; not in CoreTerms
   	Defaults map[string]Forms // per language in Languages; "en" required, every form set
   }

   // Validate fails errs.Invalid: key format or reserved; a language not in Languages;
   // "en" missing; a form empty after trimming, over MaxTermBytes, or with control
   // characters. Field names: key, defaults.<lang>, defaults.<lang>.<form>.
   func (d TermDecl) Validate() error
   ```

   `Area` gains `Terms []TermDecl`; `Area.Validate` adds duplicate keys within the area and a key in `CoreTerms` (`terms[i].key duplicate|reserved`). `Options.Validate` (so `New`, which does no I/O and therefore also runs under `bms check`) adds a key declared by two areas: `Invalid areas[j].terms[i].key duplicate`. A language without an entry uses the `en` defaults, the rule the core's `terms/<lang>.json` already has.

2. **One flat namespace.** A term's key is the same string in storage, in the admin document, in the public document's `term_<key>` arguments and in notify templates. A core area may declare terms too, but a word two parts of bms need goes into `CoreTerms` rather than being declared twice: that is where `department` lives, and it is why `lead` goes there now (V3.4). The collision error message names the fix.

3. **Storage: no migration.** `core.terminology.term` is `text`; its comment stays as it is; migrations are forward-only, so refreshing the comment to "a core term or a declared term key" is a later migration's job, not this amendment's (D58). A row whose term is declared by no area in this process (a disabled module) is kept, read by nobody, shown nowhere, refused by `SetTerms` (F29 `unknown`) and returns when the module is enabled again: the rule settings already has for an undeclared setting key. A fork that declares the same key inherits the override, which is what a regional wants from a swapped module.

4. **Reads.** `TermsFor(ctx, lang)` returns every *declared* term (core and module), not "every core term"; `Public.Terms` flattens all of them, so a module term reaches every `t()` call and every template with no further plumbing (notify's `SiteSource` flattens `TermsFor`; the shell's `publicDoc` already turns `term_shift_plural_title` into the catalogue key `term.shift.plural_title`). `TermDoc` gains `Area string` (`"core"` for `CoreTerms`, else the area's name) and `Terms` lists core terms first, then areas in build order, each in declaration order; `planTerms` keeps that order and F31's `too_many` bound becomes the number of declared terms.

5. **The boot fallback is the module's own catalogue.** Every declared term has `module.<name>.term.<key>`, `.plural`, `.title` and `.plural_title` in `web/src/modules/<name>/messages/en.json` (a decision-8 derived key set, like `module.<name>.setting.<key>.title`), and the shell's `termsOf` reads them as `term_<key>` arguments, with a `term.<key>` entry (the regional's word from the public document, or the shell's own core entries) taking precedence over a `module.*.term.<key>` entry (the default). The `internal/app` catalogue test (`catalogue_test.go`) checks that the four keys exist for every declared term and **equal the manifest's `en` defaults**, the same rule settings decision 7 imposes on the shell's `term.*` entries.

6. **A module uses only core terms and its own.** A message or template that names another module's term would render the raw argument when that module is off, which would be a dependency between modules by way of a word. The `internal/app` catalogue test scans every `{term_<x>...}` argument in each module's `messages/en.json` and every `{{term "<x>"}}` in its `Manifest.Templates` and fails on a key that is neither in `CoreTerms` nor in that module's `Terms` (notify's `term` function cannot know this at `Register`, because it is given the templates and not the manifest, and it should not: one test in `app` sees everything).

7. **The Terminology screen** (`/settings/terms`) groups cards by `Area`: the core's under the existing heading, then one `<fieldset>` per module headed by `module.<name>.title`. A card still takes its heading from the default `singular_title`, so no label key per term is needed.

**V3.4 What this module declares and changes.**

- `Manifest.Terms`: `shift` (shift, shifts, Shift, Shifts), `position` (position, positions, Position, Positions), `qualification` (qualification, qualifications, Qualification, Qualifications), `en` only. The shape is `manifestTerms()` beside `manifestSettings()`:

  ```go
  func manifestTerms() []module.TermDecl {
  	return []module.TermDecl{
  		{Key: "shift", Defaults: map[string]module.TermForms{"en": {Singular: "shift", Plural: "shifts", SingularTitle: "Shift", PluralTitle: "Shifts"}}},
  		{Key: "position", Defaults: map[string]module.TermForms{"en": {Singular: "position", Plural: "positions", SingularTitle: "Position", PluralTitle: "Positions"}}},
  		{Key: "qualification", Defaults: map[string]module.TermForms{"en": {Singular: "qualification", Plural: "qualifications", SingularTitle: "Qualification", PluralTitle: "Qualifications"}}},
  	}
  }
  ```

- **`lead` is a core term, not this module's.** Departments (core, `departments-events`) has leads, this module's screens say "lead" fifteen times, and `incidents` and `afterreports` will say it too; under rule V3.3.2 a word several parts need is a `CoreTerms` entry. Proposed change 16 adds it (settings `terms/en.json`: lead, leads, Lead, Leads; the shell's `term.lead*` entries). This module then writes `{term_lead}`.
- **Messages:** every fixed "shift", "position", "qualification" and "lead" in `messages/en.json` becomes the `{term_*}` argument in the matching form (`_title` at the start of a sentence or as a heading; `_plural` where the count is not one; where a message already uses ICU plural on a count, the `other` branch takes `_plural`). The main nav entry **Shifts** becomes `{term_shift_plural_title}`; **My volunteering** and **Volunteers** are unchanged (the owner has not asked for `volunteer` as a term; Q-VOL12). The four derived catalogue keys per term are added (V3.3.5). Permission and setting titles go through `t()` as well and get the same treatment.
- **Templates** (`qualification-verified`, `qualification-rejected`, `signup-removed`, the reminder): fixed words become `{{term "shift"}}`, `{{term "qualification"}}` and so on (the `_plural` and `_title` keys as notify documents them). The template text avoids an article before a regional word: no "a {{term "lead"}}" or "a {{term "department"}}", because the word a regional picks may start with a vowel or a consonant ("a event", "an department"); write "The {{term "lead"}} of your {{term "department"}}" or restructure the sentence (D58).
- **Go code of this module: unchanged.** No service, job or store reads a term: response types carry tokens and IDs, and words are rendered by the shell and by notify. `module.go` gains `Terms: manifestTerms()` only.
- **Spec text once approved** (spec-editor): the "Not responsible for" row "Terminology for shift, position, qualification" becomes "Declared in `Manifest.Terms` (V3); `lead` is a core term"; the Summary's "Owns" line gains "terms `shift`, `position`, `qualification`"; "Expects of dependencies" gains settings (`TermDecl`, `Area.Terms`, `TermsFor` over declared terms), module-contract (`Manifest.Terms`) and web-shell (`termsOf` over `module.<name>.term.*`); Proposed change 4 is marked superseded by 16 to 19.

**V3.5 Failure modes and tests.** New rows for this module; the rows the mechanism adds to `settings` are listed under Proposed change 16 so its spec-editor pass can carry them over.

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F167 | `Manifest.Validate` (via `module.Register`, `bms check`) | a term key of this module is in `CoreTerms`, duplicated, malformed, or its `en` defaults lack a form | Invalid (`terms[i].key reserved|duplicate|format`; `terms[i].defaults.en.<form> required`) | fix the manifest; the build fails | `TestManifest_Terms_DeclaresShiftPositionQualification` (pins the three keys and their four `en` forms; the negative cases are settings' `TestTermDecl_Validate_Table`) |
| F168 | `internal/app` catalogue test | a `module.volunteers.term.<key>[.<form>]` entry is missing or differs from the manifest's `en` default | test failure (not a runtime error) | fix `en.json` or the manifest | `TestCatalogue_ModuleTerms_MatchManifestDefaults` (app, generic over every module) |
| F169 | `internal/app` catalogue test | a message in `module.volunteers.*` or a template of this module names a term that is neither core nor this module's | test failure | fix the message | `TestCatalogue_TermsUsed_AreDeclared` (app, generic) |
| F170 | the shell (`Shifts`, `MySchedule`, the admin screens) | the regional overrides `shift` | shows the regional's word in every form, falls back to the module's default when the public document has none | none | `shifts.test.tsx` "renders the regional's word for shift" (applies a public document with `term_shift*` and asserts the nav entry and headings); "falls back to the module's default" (no document) |

No new runtime failure mode: this module's Go code reads no term, and a term that is undeclared or unset renders as its default (shell) or as its key (notify), both of which the tests above make impossible for a built module.

**V3.6 Owner question (answered, owner, 2026-10-30, D57).** Q-VOL12: `shift`, `position` and `qualification` are this module's terms and `lead` is a core term, as proposed. V3 is built once settings' part (Proposed change 16) is built; until then this module keeps its fixed words.

## Amendment 2026-10-28 (approved, owner, D53): the demo seed provider

*Where the bullets below differ from "Amendment D1 changes" at the end of this amendment (the lead's five departments, the lead's records, the pending upload's owner, Inventory Crew, the bookings), the D1 changes win (Q-DEMO5, owner, 2026-10-29).*

`ContributesTo` gains `demoseed.Point`; `Contributions()` returns a `demoseed.Provider` (`demo.go`, package `volunteers`). It runs only in demo mode, after the core's steps, against a database with no volunteers rows, and acts through this module's own services as the demo accounts (`req.Actors.As`), so every permission, audit and validation rule applies. It creates, in order:

- **Qualifications.** As `steward` (org-wide ones need `qualifications.manage` site-wide): "ICS-100" (org-wide, no expiry) and "Over 18" (org-wide, no expiry). As `lead`: "Ranger training" (Rangers, 365-day validity). "First aid" (Medical, 730 days) is created by whichever actor holds `qualifications.manage` at Medical (the `steward`).
- **Positions.** Dirt Ranger (Rangers, open, requires Ranger training and Over 18); Khaki (Rangers, members, requires Ranger training, ICS-100 and Over 18; the "shift lead" position); Gate Crew (Gate, open); Greeter (Greeters, open); Medic (Medical, members, requires First aid).
- **Members.** The participant, the lead and six walk-ons in Rangers; four walk-ons in Medical.
- **Records.** Ranger training and Over 18 verified for the participant, the lead and the Rangers walk-ons; ICS-100 verified for the lead; one **pending** self-upload (a generated 1x1 PNG certificate) for walk-on 02.
- **Shifts** for the event, Thursday to Sunday in `Zone`: Dirt Ranger 08-14, 14-20, 20-02 with 4 slots; Khaki 07-15 and 15-23 with 1 slot; Gate Crew 10-14, 14-18, 18-22 with 3 slots; Greeter 10-14 and 14-18 with 3 slots; Medic 08-20 and 20-08 with 2 slots; plus one standing "Winter inventory" shift (no event) in DPW two weeks after the event.
- **Sign-ups.** As `participant`: two Dirt Ranger shifts (Thursday 14-20, Saturday 08-14). As `lead`, `Assign` walk-ons so one Dirt Ranger shift is full, each Gate and Greeter shift has one booking, the lead holds Khaki on Friday, one assignment with `Waive: true` (walk-on 07 on Dirt Ranger without training); one shift canceled by the lead (Sunday 20-02 Dirt Ranger).

Every booked person has a playa name (the core seed gives each one), which the `Conflict unnamed` rule requires. `Result.Items` counts the records created. Nothing here writes about a real person; the PNG and every name are generated constants.

### Failure modes (added)

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F171 | demo `Seed` | an actor lacks a permission for its step (starter roles or table wrong) | Permission wrapped with the step | the seed fails; fix the table | `TestDemoSeed_ActorLacksPermission_ReturnsPermission` |
| F172 | demo `Seed` | a row fails validation | Invalid wrapped | the seed fails | `TestDemoSeed_TableValidates` |
| F173 | demo `Seed` | a booked person has no playa name | Conflict `unnamed` wrapped | the seed fails | `TestDemoSeed_EveryBookedPersonIsNamed` |
| F174 | demo `Seed` | a normal run | — | `Result.Items` > 0, the counts above | `TestDemoSeed_CreatesTheDescribedData`; `moduletest.Conformance` with `WithDemoSeed()` |

### Amendment D1 changes (raised 2026-10-28, Approved 2026-10-29, D56; demo.md "Amendment D1")

The build of `demo.go` found the table above could not run as written. demo.md D1.1 to D1.7
settle it; the table changes are these and they supersede the bullets above where they differ.
The owner answered demo.md's Q-DEMO5 on 2026-10-29: one `lead` account may run five
departments (a small burn has few volunteers, so one person leads several departments):

- **Actors.** The `lead` holds `department-admin` at Rangers, Gate, Greeters, Medical and
  DPW (demo.md D1.1), so every `lead` step below holds its permission. The `steward` still
  creates the org-wide "ICS-100" and "Over 18" and Medical's "First aid" (unchanged).
- **Positions** gain "Inventory Crew" (DPW, open, no requirements), created by the lead
  (D1.4).
- **Members** (D1.7): the participant, the lead and walk-ons 01 to 06 in Rangers; walk-ons
  08 to 11 in Medical; walk-ons 07 and 12 in neither.
- **Records** (D1.2, D1.3): Ranger training and Over 18 verified by the lead for the
  participant and walk-ons 01 to 06; **no records for the lead** (`RecordQualification` and
  `VerifyRecord` refuse `Invalid self`, and nobody else verifies at Rangers); the pending
  self-upload (the generated 1x1 PNG) is the **participant's, for ICS-100**, as the last
  record step, with `self_upload` at its default `true`.
- **Shifts**: as above; the standing "Winter inventory" shift is on Inventory Crew, the day
  14 days after the event's end day, 09:00 to 13:00 in the event's zone, 6 slots, location
  "Warehouse". The event's dates come from `Deps.Events.InfoOf`; an event without dates fails
  `Invalid` (`event`, `required`), F175, and the provider carries no default-date rule of its
  own (D1.5; the conformance rule supplies dates, module-contract).
- **Sign-ups** (D1.3, D1.7), all as `lead` except the participant's own: the participant
  signs up for Dirt Ranger Thursday 14 to 20 and Saturday 08 to 14; Friday 08 to 14 Dirt
  Ranger is full with walk-ons 01 to 04; Gate Crew, Thursday to Sunday, walk-on 09 on 10 to
  14, 10 on 14 to 18, 11 on 18 to 22; Greeter, Thursday to Sunday, walk-on 12 on 10 to 14,
  08 on 14 to 18; the lead holds Khaki Friday 07 to 15 by `Assign` with `Waive: true` on
  themself (F152: a recorded, flagged self-waiver); walk-on 07 on Dirt Ranger Saturday 14 to
  20 with `Waive: true` (no training, not a member; the position is open); Medic shifts stay
  unbooked (nobody at Medical has First aid verified); Sunday 20 to 02 Dirt Ranger is
  canceled by the lead with no bookings.
- **Provider lifecycle** (D1.6): the provider may rely on `Start` having run (`Assign`
  enqueues the reminder job `Start` registers); the conformance rule starts the module
  first.

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F175 (D1) | demo `Seed` | the event has no dates | Invalid (`event`, `required`) | the seed fails; the core always gives dates, so this is a fake set up wrong | `TestDemoSeed_EventWithoutDates_ReturnsInvalid` |

Tests that change: `TestDemoSeed_CreatesTheDescribedData`'s counts (five departments' worth
of lead work, one pending record for the participant, no lead records, the Inventory Crew
position, two waivers); `TestDemoSeed_ActorLacksPermission_ReturnsPermission` now proves
F171 with the lead's Gate grant removed from the fake.

## Amendment V4 (raised 2026-10-30, D57): shift navigation and a compact shift list

*Status: Implemented 2026-10-30; Approved D58. Nothing below changes a table, a permission, a signal, a setting or a job. One query field is added to `ListShifts` (V4.3.4); everything else is the browser.*

**V4.1 The problems** (the demo walkthrough, accepted by the owner under D57, "Smaller UX items").

1. **"Back to shifts" loses where the person came from.** The shift page's back link is a fixed `/volunteers`. A lead who opens a shift from their department's Shifts tab (`/volunteers/departments/:department?tab=shifts`) is sent to the public list of about sixty cards instead of back to the tab; someone who came from My volunteering is sent to the list too. The label says "shifts" wherever it goes.
2. **The shift list is very long.** `/volunteers/` renders one full-width card per shift (label, department and position, the full date and time, the location, the free count, the sign-up control: about 190 px each). The demo's 49 shifts make the page about 10,800 px tall on a desktop; each card repeats the date its day heading already shows; and a 600-shift event (ten departments over six days is ordinary) would be 100,000 px with six **Show more** presses.
3. **Two phrasings for one count.** The list says "4 places free of 4" (`module.volunteers.free`), the shift page "0 of 4 booked" (`shift.filled`) and the lead's tab "0 of 4 booked" (`tab.shifts.fill`).
4. **"The departments you lead" is shown to non-leads.** `/volunteers/admin` lists the departments the viewer may open under that heading; a volunteer coordinator or a steward whose permission is site-wide (`led.site`) sees every department under "you lead", which is false.

**V4.2 The back link.**

*Options.* (a) `navigate(-1)`: *not taken*, a deep link, a reload after the history is gone, or a reminder e-mail's link has nothing to go back to, and `-1` can leave the application. (b) A `?from=` query parameter on the shift URL: *not taken*, it puts navigation state in a URL people copy and bookmark, and a copied link would carry a stale return path. (c) **Router state** (`history.state`, the `state` prop of `Link` and `navigate`): invisible in the URL, survives a reload (browsers keep it with the history entry), absent on a deep link, and validated on read with a safe default. **Chosen.**

*The state.* One shape, produced and read by `web/src/modules/volunteers/back.ts`:

```ts
export type BackKind = "shifts" | "department" | "me";
/** What a screen passes as `state` when it links to a shift page. `to` is a path inside this
 *  module (pathname plus query), never a name or free text; `kind` picks the label. */
export interface BackState { back: { to: string; kind: BackKind } }
/** The state for a link from the current location. */
export function backTo(kind: BackKind, location: { pathname: string; search: string }): BackState;
/** The validated back link for a shift page: the state's, else the default
 *  `{to: "/volunteers", kind: "shifts"}`. Malformed means: not an object, `kind` not one of the
 *  three, or `to` not matching `^/volunteers(/[^?#]*)?(\?[^#]*)?$` (a module path with an
 *  optional query; no scheme, no `//`, no fragment). */
export function backFrom(state: unknown): { to: string; kind: BackKind };
```

The validation exists because history state is writable by any script on the origin and survives across deployments: a `to` outside this module would make the back link an open redirect inside the app, and an unknown `kind` would have no label. Nothing personal travels in it (a path, a query of IDs and filter values, a token), which is Proposed change 20's rule for `web-shell`.

*Producers.* Every link to `/volunteers/shifts/:shift` passes `backTo(kind, location)`: the list (`Shifts`, kind `shifts`; `to` keeps the list's query string, so a filtered list is returned to as filtered), the lead's Shifts tab (`ShiftsTab`, kind `department`; `to` is `/volunteers/departments/${department}?tab=shifts`), My volunteering (`MySchedule`, kind `me`), and the shift form: `ShiftNew` navigates to the created shift with kind `department`, and `ShiftEdit` reads the state it was opened with and passes it on, both on its own "Back to the shift" link and on the navigation after saving, so a lead who edits from a department tab still returns to the tab. A link with no producer (the reminder e-mail, the person-page section's `/volunteers/me` link, a bookmark) has no state and gets the default.

*The label says where it goes.* `shift.back` becomes "Back to {term_shift_plural}" (V3's form; "Back to shifts" until V3 is built), used for kind `shifts` and for the default; `shift.back_me` "Back to My volunteering" for kind `me`; for kind `department`, `shift.back_department` "Back to {name}" with `name` the loaded shift's `department_name`, and `shift.back_department_generic` "Back to the {term_department}" while the shift is loading or when it failed to load (the link still works: `to` does not depend on the shift). The link is rendered above the heading as today, is never disabled, and is a plain `Link` (not a button), so the browser's own back and the in-page link behave the same.

**V4.3 The compact list.**

*Options.* (a) **Dense rows grouped by day; one DOM, two layouts by CSS.** Each day keeps its `<section>` and `<h2>`; each shift is one `<li>` in the day's `<ul>`: on a phone three short lines, from the `sm` breakpoint (640 px, the kit's) one grid row. **Chosen**, detail below: it removes the repeated date, the card chrome and the long sentences, keeps the day grouping (the way a volunteer thinks: "what is there on Friday?"), keeps one-tap sign-up on a phone, and changes no component contract. (b) The kit's `Table` per day on a desktop: *not taken*; `Table` stacks every cell under its own label below 640 px, which is longer than the card, so it would need a second phone layout, and a grid row whose cells are self-describing (a time, a link, names, "2 of 4 booked", a button) loses nothing a column header would add. (c) Days side by side (a week view): *not taken*; six columns of a hundred rows is no better than one list, it needs a wide screen, and the day filter already answers "just Friday". (d) `<details>` per day, closed by default: *not taken*; it hides the shifts behind a tap, and the day filter exists for that. (e) Dropping the sign-up control from the list (rows as links only): *not taken*; it would add a tap to every booking on the device most volunteers have.

*The row.* Phone (below `sm`): line 1, the times and the label; line 2, department · position · location (when set) · the fill count; line 3, the sign-up control (**Sign up**, "You're booked", or a short reason), present only when there is one (a canceled shift says "Canceled" in line 2 and has no line 3). From `sm`: the same `<li>` is a grid row [times | label | department · position · location | fill | control], rows separated by a border, no card box. About 110 px per shift on a phone and 48 px on a desktop (the demo's 49 shifts: about 3,200 px on a desktop, from 10,800). Specifically:

- **Times only.** `<time dateTime={starts}>` with `format.time(starts, zone)` to `format.time(ends, zone)` (the zone's short name appears when it differs from the browser's, as `format.time` already does); the day heading keeps `format.date` of the day. When the end falls on a later day than the start (`dayKey(ends, zone) !== dayKey(starts, zone)`: a night shift) the row says "{start} to {end} next day" (`module.volunteers.when_next_day`; the plain case reuses `module.volunteers.when`, "{start} to {end}"), so "8:00 PM to 2:00 AM" is not read as a negative shift. `ShiftWhen` (date and times) stays for the shift page, the schedule and the lead's tab; the list uses a new `ShiftTimes`.
- **The label** is the row's link (`min-h-11`, the 44 px target), an `<h3>` as today, so the heading outline of the page is unchanged (page, day, shift).
- **The muted line** joins `department_name`, `position_name` and `location` with " · ", skipping what is empty.
- **The fill count** is V4.4's one phrasing.
- **The control** is `SignUpControl` with a new `short` prop: the button and "You're booked" are unchanged; a reason is rendered from `module.volunteers.why_short.<reason>` instead of the `why.<reason>` sentence (`full` "Full", `not_member` "Members only", `overlap` "Overlaps a booking of yours", `started` "Already started", `closed` "{term_event_title} closed", `archived` "No longer offered", `unknown` "Not available", and a missing qualification "Needs {names}"). The shift page keeps the sentences; the row's link is one tap away. An unknown reason token falls back to `why_short.unknown` as `useWhyNot` does today.

*Filters.* Event, department and day stay as they are (event and department server-side, day a window server-side and exact in the browser). One is added: **Only {term_shift_plural} I can take**, a `Check` in the filter form, kept in the query string as `?eligible=1` (`replace: true`, like the others), cleared by **Clear filters**, and counted in the phone's "filters are set, start open" rule. It is server-side (V4.3.4): a browser-side filter over a 100-row page would turn "Show more" into a lottery at 600 shifts. It shows the shifts the person is eligible for **and the ones they already hold** (`eligibility.eligible || mine`): "what can I do on Friday" includes what they have on Friday, and the switch never hides a booking of theirs. Off by default: the full picture is what a lead reviewing coverage and a newcomer learning what exists both want first; a regional cannot change the default (it is a per-visit choice, not a policy). Not taken: a day strip of chips (it needs the event's dates and the day field does the job), and a usage action for the switch (nothing in D54 needs it yet).

*Paging and empty states* are unchanged in shape: `limit=100`, **Show more** while `next` is present; the empty state only when no row is shown and `next` is absent; a page whose rows were all dropped (by the day filter in the browser, or by `eligible` on the server) shows **Show more**, as the day filter already does.

*V4.3.4 The service.* `vol.ShiftQuery` gains one field, and `GET /shifts` one query parameter:

```go
type ShiftQuery struct {
	Department      model.DepartmentID
	Event           model.EventID
	Position        PositionID
	Within          model.TimeRange
	IncludeCanceled bool
	// EligibleOnly keeps only the shifts the actor is eligible for (Eligibility.Eligible) and
	// the shifts the actor holds a booking on (Mine not zero); the rest are dropped after the
	// fetch, so a page may be short (decision 22). Needs no permission beyond seeing the shift:
	// it only removes rows. Query parameter `eligible` (a boolean, parsed like `canceled`).
	EligibleOnly bool
	Page
}
```

Behavior (an addition to the "Paging" bullet): with `EligibleOnly`, the store fetches up to `MaxList + 1` rows in keyset order from `After` instead of `Limit + 1` (one query; eligibility is in Go, decision 4, and a 100-row window of ineligible shifts would otherwise return empty pages one after another); the service computes `Eligibility` and `Mine` for the actor as it does today (records, bookings and memberships loaded once), keeps the rows that pass, and returns the first `Limit` of them. `Next` is the cursor of **the last row the call consumed**: the last returned row when fetched rows remain beyond it (kept or not), the last fetched row when fewer than `Limit` passed and a `MaxList + 1`-th row exists, and empty otherwise. So a page is never longer than `Limit`, never skips a row, costs at most one `MaxList`-row scan, and can be empty with `Next` set (the screen shows **Show more**). The archived-department drop runs on the same fetched rows as before. Consumers: `http.go` only (`ListShifts`' other callers, `SetUp` and the lead's tab, never set it).

**V4.4 One phrasing for the count.** "{booked} of {slots} booked", everywhere a shift's fill is shown: the list row, the shift page's **Slots** line and the lead's Shifts tab, as one key `module.volunteers.fill` (the three keys `free`, `full`, `shift.filled` and `tab.shifts.fill` are removed; `MessageKey` typing makes a leftover use a build error). Chosen over "{free} of {slots} free" because it already stands on two of the three screens, a lead reads fill and a volunteer reads "2 of 4 booked" as two places free at the same glance, and a full shift reads "4 of 4 booked" with no second word: the standalone "Full" goes; the control's reason (`why_short.full`, `why.full`) still says it where it matters, on the sign-up line.

**V4.5 The admin heading, worded by what the person can do.** `/volunteers/admin` lists the departments the viewer may open, from `ledDepartments(session, LEAD_PERMISSIONS)`. The heading and empty state no longer say "lead": with `led.site`, the heading is `admin.departments_all` "All {term_department_plural}" with the muted note `admin.departments_all_hint` "You can manage or view every {term_department}."; otherwise `admin.departments` "{term_department_plural} you can manage or view"; the empty state `admin.none` "No {term_department} you can manage or view yet." "Manage or view" is the literal truth of `LEAD_PERMISSIONS` (six manage permissions and two read ones, `rosters.read` and `qualifications.read`): a `department-admin`, a volunteer coordinator and a steward are all described without naming a role the regional may have renamed. The wording is Q-VOL13. Nothing else on the screen changes.

**V4.6 Catalogue keys** (`web/src/modules/volunteers/messages/en.json`). Added: `shift.back_me`, `shift.back_department`, `shift.back_department_generic`, `when_next_day`, `fill`, `filter.eligible`, `why_short.full|not_member|overlap|started|closed|archived|unknown|missing`, `admin.departments_all`, `admin.departments_all_hint`. Reworded: `shift.back`, `admin.departments`, `admin.none`. Removed: `free`, `full`, `shift.filled`, `tab.shifts.fill`. Under V3 the fixed words in the new strings are `{term_*}` arguments like every other string.

**V4.7 Failure modes and tests.** Browser rows name the vitest file and the `it` sentence; Go rows the test function.

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F176 | `backFrom` (the shift page's back link) | no state (a deep link, the reminder's link, a reload that lost it), or a malformed state: not an object, `kind` unknown, `to` outside `/volunteers` (another path, a scheme, `//`, a fragment) | none (a default, not an error) | the link goes to `/volunteers` labelled "Back to shifts" | `shiftpage.test.tsx` "a deep link, or an unusable state, falls back to Back to shifts at /volunteers" (a table: `undefined`, `{}`, `{back: {to: "https://x", kind: "shifts"}}`, `{back: {to: "/people/me", kind: "me"}}`, `{back: {to: "/volunteers//x", kind: "shifts"}}`, `{back: {to: "/volunteers", kind: "lead"}}`); `back.test.ts` "backFrom validates the shape and the path" |
| F177 | the shift page | opened with kind `department` | none | the link returns to the tab; its label names the department once the shift loads and says "Back to the department" before, and when the shift failed to load (404, 503) | `shiftpage.test.tsx` "from a department tab: the back link returns there and names the department once the shift loads", "...names no department when the shift fails to load" |
| F177a | `Shifts`, `ShiftsTab`, `MySchedule`, `ShiftNew`, `ShiftEdit` | a link or navigation to a shift page | none | each passes `backTo` with its kind; the list's `to` keeps its query string; `ShiftEdit` passes on the state it received | `shifts.test.tsx` "opening a shift from a filtered list returns to the same filters"; `lead.test.tsx` "the Shifts tab, a new shift and an edited shift all return to the tab"; `me.test.tsx` "a schedule entry returns to My volunteering" |
| F178 | `ListShifts` | `EligibleOnly` | none | only rows with `Eligibility.Eligible` or `Mine` set are returned; a booking of the actor's whose eligibility is now false (full, overlap with itself) is kept; a canceled shift is dropped unless `Mine` | `TestListShifts_EligibleOnly_KeepsEligibleAndMine` |
| F179 | `ListShifts` | `EligibleOnly` with more than `Limit` passing rows | none | the page holds `Limit` rows; `Next` is the last returned row's cursor; following `Next` yields the rest with no row repeated or skipped | `TestListShifts_EligibleOnly_PagesNeverSkip` |
| F180 | `ListShifts` | `EligibleOnly` with fewer than `Limit` passing rows among `MaxList` fetched and a row beyond | none | the page is short (possibly empty), `Next` is the last fetched row's cursor; `Next` empty when nothing lies beyond | `TestListShifts_EligibleOnly_ScanBoundSetsNext`, `TestListShifts_EligibleOnly_LastPageHasNoNext` |
| F181 | `GET /shifts` | `eligible` is not a boolean | Invalid `malformed` on `eligible` | field error | `TestShiftsRoute_BadEligible_ReturnsInvalid` |
| F182 | `Shifts` | the **Only shifts I can take** switch | none | the query carries `eligible=true` and the URL `?eligible=1`; **Clear filters** removes it; the form starts open on a phone when it is set | `shifts.test.tsx` "Only shifts I can take sends eligible=true, keeps it in the URL and clears with the filters" |
| F183 | `Shifts` | any list | none | rows show times only under a dated day heading, "next day" when the end's day differs, one fill phrasing, no separator for an empty location; the short reason for an ineligible row | `shifts.test.tsx` "rows show times under the day heading, say next day for a night shift, and one booked phrasing", "an ineligible row says the short reason; the shift page keeps the sentence" |
| F184 | `Shifts` | a page with no rows left and `next` present | none | **Show more**, not the empty state | `shifts.test.tsx` "an empty page with next shows Show more, not the empty state" |
| F185 | `Admin` | the viewer's permissions are site-wide; named departments; none | none | "All departments" with the note; "...you can manage or view"; the empty state | `lead.test.tsx` "a site-wide holder sees All departments and the note; a department holder sees the departments they can manage or view" |

No new runtime failure in Go beyond F181: `EligibleOnly` adds no I/O, no permission and no state. Tests that change: `shifts.test.tsx` "lists shifts with fill and Sign up" (the phrasing, the row shape), `shiftpage.test.tsx` "shows the shift" (the **Slots** line), `lead.test.tsx` "Shifts tab: fill counts" (the phrasing) and "a site-wide holder sees every department" (the heading).

**V4.8 Spec text once approved** (spec-editor; applied 2026-10-30, D58, except the F176 to F185 rows in the main table and the `openapi.json` parameter, which stay in this amendment and with the build): the UI table rows for `/volunteers/` (the row shape, the `eligible` filter, times only), `/volunteers/shifts/:shift` (the back link's state and labels) and `/volunteers/admin` (the headings); Interface: `ShiftQuery.EligibleOnly` and the route `GET /shifts?...&eligible=`; Behavior "Paging": the `EligibleOnly` scan rule; the Summary's `vol` line (`ShiftQuery` gains `EligibleOnly`) and its UI sentence ("free slots" becomes "the fill count"); F176 to F185 appended to the table; the module's `openapi.json` gains the `eligible` parameter. Proposed change 20 applies to `web-shell.md`.

**V4.9 Owner question.** Q-VOL13 (answered, owner, 2026-10-30, D58: the proposed words). Everything else is settled above; the owner may still reword any string at approval, since no string here is load-bearing beyond its key.

## Open questions

**Answered (V4, owner, 2026-10-30, D58, the proposed wording):**

- **Q-VOL13. How should the volunteers admin page head its department list for someone whose permission is not "lead"?** The walkthrough found "The departments you lead" shown to a coordinator and a steward whose permissions are site-wide (D57). **Answer (owner, 2026-10-30, D58): as proposed (V4.5):** "All {term_department_plural}" with the note "You can manage or view every {term_department}." for a site-wide holder; "{term_department_plural} you can manage or view" for a person whose permissions name departments; the empty state "No {term_department} you can manage or view yet."

**Answered (V3, owner, 2026-10-30, D57, the recommended answer):**

- **Q-VOL12. Which words should a regional be able to rename in this module, and where does "lead" live?** Proposed: `shift`, `position` and `qualification` are this module's terms; `lead` is a core term (`settings.CoreTerms`), because departments, volunteers, incidents and after-burn reports all say it. Not proposed now: `volunteer` (every regional seems to say it; one line to add later if a regional asks) and `waiver`, `roster`, `catalogue` (jargon of this module's screens rather than words burners rename). If the owner wants different words or more of them, the list in V3.4 changes and nothing else does. **Answer (owner, 2026-10-30, D57): as proposed.**

**Answered.** V2 rests on jobs Q-JOBS14, answered yes (D50).

**Answered by the owner (2026-10-22), folded into V1:**

- **Q-VOL10. May a lead's `Assign` override `name_taken` (book someone whose playa name another volunteer at the event already uses)?** V1 recommended no. **Owner (2026-10-22): "The system does not allow duplicate burn names. A process for retiring names will need to be devised later. General practice is that a name becomes eligable for retirement if not used within 5 years. However, sometimes names are vulgar or inappropriate for other reasons, so a process needs to be made (eventually) to handle those as well."** Read as: duplicates are refused at the profile, installation-wide, not per event, so there is nothing for a lead to override; the retirement process (five years unused, or an inappropriate name) is Later. Carried by people Amendment P1 (the unique index, `Conflict taken`; retirement listed under its Later); this module's `name_taken` and F160 are withdrawn (V1.10).
- **Q-VOL11. When a person who is booked at an event renames themself in people to a name another volunteer at that event already uses, what should happen?** V1 recommended nothing automatic. **Owner (2026-10-22): "The system shouldn't allow someone to rename themselves to a conflicting name. For the sake of initial deployment, burn names are permanent and self-appointed. Future development will create optional approval pipelines. Also, it is possible for someone to have multiple burn names, such as their known alias, then a ranger handle for use on the radio - only the latter needs to be unique. If it helps, we can defer some of this nuance."** Read as: the rename is refused by people (and for the first deployment there is no rename at all: the name is read-only once set), so a clash after booking cannot arise and no signal is needed; approval pipelines and several names per person (alias plus radio handle, only the handle unique) are deferred. Carried by people Amendment P1 (read-only once set; approval and multiple names under its Later); this module drops the rename note and the `people.person-changed` item under Later (V1.10). When several names arrive, which one this module shows on rosters and shift-mate lists is a question for that amendment, not for now.

- **Q-VOL7. May a lead waive a requirement for themself?** V1 recommended no. **Owner: yes.** Decision 21 withdrawn; a self-waiver is recorded, audited and flagged like any other (F152, `TestAssign_SelfWaive_RecordsWaiver`); the UI's disabled switch and `Invalid self` on `waive` are gone (V1.5).
- **Q-VOL8. Under `visibility_legal_name = self`, should a person see their own legal name on a roster without holding `people.contacts.read`?** V1 recommended yes via Proposed change 11. **Owner: no. "Everyone sees burn names. Burn names also need to be enforced to be unique when registering to volunteer for that event."** Proposed change 11 dropped; the `self` option removed from the `legal_name` group (decision 27, F138, F138a; V1.3); and the name-uniqueness sentence first read as a per-event rule for this module (the first V1.10), then, with the Q-VOL10 and Q-VOL11 answers, settled as people's installation-wide rule (P1), leaving this module the `unnamed` precondition (decision 26, V1.10, F161, F164) and decision 28 (F166). "Everyone sees burn names" is read as the answer to the legal-name question: `visibility_shift_mates`, `HideShiftMates` and `SetListed` (D43, decisions 10, 17, 19) stand, since they decide who sees *who is on which shift*, not whether playa names are secret, and a person who opts out of the shift-mate list is still named on the roster.
- **Q-VOL9. When a lead or the system cancels someone's booking, should it stay on their schedule, marked, until the shift passes?** V1 recommended yes. **Owner: yes.** Kept as written (F153, V1.9 (d)).

**Closed by D42 (owner, 2026-10-20):**

- **Q-VOL1. May a lead assign someone who lacks a requirement?** Yes, leads may override: `Assign` with `Waive: true` records `waived_by`, audits `Reason waived` and the roster flags the person; `SignUp` never waives (decision 7, F102-F103, `TestSignUp_NeverWaives`).
- **Q-VOL2. Sensitivity of a qualification record.** `internal`; the certificate file stays `personal` (decision 9; the Data table).
- **Q-VOL3. Who manages the org-wide catalogue?** Site-wide `qualifications.manage`, seeded into `department-steward`; a department's entries by its `department-admin` (decision 16; the Permissions table).
- **Q-VOL4. Is self-upload on by default?** Yes, `volunteers.self_upload = true` (decision 2; the Settings table).
- **Q-VOL5. May volunteers see who else is on a shift?** Yes, by playa name only, configurable, because some people don't get along and want to pick another shift. This reversed the draft's recommendation; the design is decision 10 (as reshaped by D43: the `shift_mates` privacy group and `visibility_shift_mates`, justified under Settings), `Position.HideShiftMates`, `SetListed`, the `unlisted` table, F126-F134.

**Closed by D43 (owner, 2026-10-20):**

- **Q-VOL6. Is the shift-mate listing opt-out or opt-in?** The owner answered on a different axis: the regional sets visibility, with these defaults: playa names and who is on which shift visible to anyone who volunteers; legal names only to a volunteer coordinator and maybe some other roles; fields tagged so permissions can be enforced and changed later. Folded in as decisions 10 (reshaped), 17, 18 and 19: the `privacy` groups `shift_mates` (default `holders` of `shifts.signup`) and `legal_name` (default `holders` of `people.contacts.read`), the `volunteer-coordinator` starter role, and the per-person opt-out kept (the owner's scenario has two sides; the count shows someone is withheld; the roster is unaffected). The consent alternative is not needed: a regional that wants opt-in sets `visibility_shift_mates` to `nobody` until the Later `consented:<purpose>` audience exists (privacy-tags, "Later").

**Open:** none. Q-VOL13 is answered (owner, 2026-10-30, D58, above). Q-VOL12 is answered (owner, 2026-10-30, D57). Q-VOL7 to Q-VOL11 are answered (owner, 2026-10-22); Q-VOL10 and Q-VOL11 are carried by people Amendment P1.

This spec also depends on two specs, both now Approved (D43, D44): `jobs` (Q-JOBS1's screen is where a lead checks "did the reminder run"; Q-JOBS4 is not needed, deferred `RunAt` covers reminders; `MaxEvery` must stay ≥ 24h for the daily scans) and `files` (Q-FILES1 is indifferent to this module; Q-FILES5 stripping is right for certificates; Q-FILES6: this module exports file IDs, files exports the bytes). If either changes, Summary "Expects of dependencies" lists what to re-check.

## Proposed changes elsewhere

1. **`people.md`:** define an extension point `people.membership` (`contracts/membership`: `Point = "people.membership"`; `Provider{DepartmentsOf(ctx, people []model.PersonID) (map[model.PersonID][]model.DepartmentID, error)}`) so that `people.directory.read@D` and `people.contacts.read@D` extents filter to D's members (people decision 9's "later membership contract"); then seed `people.contacts.read` into `department-admin`. This module contributes its `member` rows. Until applied, decision 9 stands and leads with `contacts.read` reach everyone.
2. **`module-contract.md` and the `add-module` skill:** `Deps.Jobs jobs.Registrar` and `Deps.Files files.Store` (already proposed by `jobs.md` and `files.md`; this module is their first consumer); `moduletest.Conformance` fills nil ones with `jobstest.NewFake` and `filestest.NewFake`.
3. **`departments-events.md`:** none now; `ScheduleOf` and `departments.department-page` stay Later as that spec says. Note in its "What `volunteers` will need": positions are standing, so event set-up copies shifts only.
4. **`settings.md`:** let a module declare terms (`Area.Terms []string` with defaults in its catalogue) so `shift`, `position`, `qualification` and `lead` become regional words like `department`; until then this module uses its own catalogue keys. Low priority. **Superseded 2026-10-28 by Amendment V3 (Approved 2026-10-30, D57):** the design choice D52 asked for is V3.3; the concrete edits are 16 to 19 below.
5. **`files.md`:** the example record kind `volunteers.qualification` becomes `volunteers.qualification-record`; Proposed change 1 there (`module.Route.MaxBodyBytes`, `Timeout`) is required by the upload and certificate routes here.
6. **`plan/07-abstractions.md`, "Modules":** one line: "`volunteers`: qualifications and their verified records, positions, membership, shifts and sign-ups (spec `design/module-volunteers.md`)."
7. **`plan/design/README.md`:** row 19 to Draft (done with this spec); `plan/05-roadmap.md` Phase 1: "Spec and build the first module" gains "(spec drafted 2026-10-20)". **`burn-domain` skill, "Volunteering and shifts":** add "Volunteers want to see who else is booked on a shift before they book it, by playa name only, because some people don't get along and would rather pick another shift; regionals differ on whether to allow it (owner, 2026-10-20; D42)" and, under "How regionals differ", "whether volunteers see who else is on a shift".
8. (D43; restates privacy-tags Proposed change 3) **`plan/design/people.md`:** `Directory.LegalNamesOf(ctx, []model.PersonID) (map[model.PersonID]string, error)` (permission `people.contacts.read` in any extent, audited per returned person, never on a route; `peopletest.Fake.AddLegalName`); `people.contacts.read` and `people.directory.read` gain `volunteer-coordinator` in `StarterRoles`. Until applied, `RosterEntry.LegalName` is declared, tagged and always absent.
9. (D43; restates privacy-tags Proposed change 2) **`plan/design/authz.md`:** the `volunteer-coordinator` site starter role (`RoleVolunteerCoordinator`, migration `0003`). This module lists the key in `StarterRoles` now; `Seed` skips a key with no role and applies it at the first `bms migrate` after the role exists.
10. (D43) **`plan/05-roadmap.md`, Phase 1:** `privacy-tags` is built before this module's services (privacy-tags Proposed change 7); **`.claude/skills/burn-domain/SKILL.md`, "Who may see what":** "Playa names and who is on which shift are, by default, visible to anyone who volunteers; legal names only to a volunteer coordinator and whichever other roles the regional chooses; the regional sets both (owner, 2026-10-20; D43)" (the main session records it).
11. (V1, Q-VOL8; **dropped 2026-10-22**, owner: no) ~~**`plan/design/people.md`, `Directory.LegalNamesOf`:** the actor's own ID answered without `people.contacts.read`.~~ Legal names stay with permission holders; people is unchanged and this module removed the `self` option instead (decision 27).
12. (V1; **dropped 2026-10-22**, Q-VOL10 and Q-VOL11) ~~**`plan/design/people.md`:** export `people.NameKey` for this module's per-event comparison.~~ People Amendment P1 defines `people.NameKey` for its own unique index; this module no longer compares names and uses none of P1's names.
13. (V1, Q-VOL7; **applied 2026-10-23**, main session) **`.claude/skills/burn-domain/SKILL.md`:** under "Qualifications and requirements", the sentence "Volunteers can never waive a requirement for themselves" gains "a lead may waive for themself, as a recorded waiver like any other (owner, 2026-10-22)"; under "Who may see what", add "Everyone sees playa names; legal names only for the roles with the permission, never the person's own entry as an exception (owner, 2026-10-22)"; under "Volunteering and shifts", add "A person needs a playa name before they can book a shift: rosters, shift-mate lists and the radio go by it (owner, 2026-10-22, by way of the name-uniqueness rule)". The uniqueness, permanence, retirement and multiple-names sentences (Q-VOL10, Q-VOL11) are people P1's to propose, not this spec's.
14. (V1, decision 26, Q-VOL10 and Q-VOL11; **satisfied 2026-10-23 by D48**, which approved people Amendment P1) **Dependency on `plan/design/people.md` Amendment P1:** this module relies on people making playa names unique across the installation (a unique index on `people.NameKey`, `Conflict taken` on `UpdateMe`/`Create`) and read-only once set, which is what lets `ShiftMates.Names` and `RosterEntry.PlayaName` identify a person without a clash check here; nothing in this module imports or calls P1's exported names (`people.NameKey` and the rest are people's). If P1 is approved in a form that does not guarantee one unique playa name per person (several names, retirement, a rename path), people's amendment must say which name modules get from `NamesOf` and that it stays unique, and this spec re-checks decision 26 then. Nothing to apply in another file; recorded here so the two amendments are approved together.

15. (V2; **satisfied 2026-10-23 by D50**, which approved jobs Amendment J1) **`plan/design/jobs.md` Amendment J1** (drafted 2026-10-23 with this amendment): decision 3 reworded and decision 23 ("the latest request for a key wins"; `next_run_at`/`next_args`, migration `0002`; F43 reworded, F53 to F58; `jobstest.Fake.Queued()` and `RunDue`). This module's code is unchanged by V2; its tests change as V2.3 says.

16. (V3, Approved 2026-10-30, D57; replaces 4; applied 2026-10-30, D57, in settings.md) **`plan/design/settings.md`, an amendment "module-declared terms":** `TermDecl{Key string; Defaults map[string]Forms}` with `Validate` (V3.3.1; alias-exported to `module` as `module.TermDecl` and `module.TermForms`); `Area.Terms []TermDecl`; `Area.Validate` adds `terms[i].key duplicate|reserved` (reserved = a `CoreTerms` name); `Options.Validate`/`New` adds `areas[j].terms[i].key duplicate` across areas (decision: one flat namespace; a word two parts need is promoted to `CoreTerms`); `CoreTerms` gains `lead` (`terms/en.json`: lead, leads, Lead, Leads); `TermsFor` returns every declared term; `Public.Terms` flattens them; `TermDoc` gains `Area string` (`"core"` or the area name); `Terms` promises no order (the Terminology screen orders: core first, then modules alphabetically by title, then terms by key; D58); F29 `unknown` covers an undeclared module term; F31's bound is the number of declared terms; the stored row of an undeclared term is kept and ignored (the undeclared-key rule); no migration (the `core.terminology.term` comment is left; refreshing it is a later migration's job, D58); the `/settings/terms` screen groups cards by `Area` under `module.<name>.title`. New failure modes there: `New` with a term key declared by two areas → Invalid (`TestNew_DuplicateTermAcrossAreas_ReturnsInvalid`); `TermDecl.Validate`/`Area.Validate` table (`TestTermDecl_Validate_Table`: format, reserved, duplicate, unknown language, `en` missing, a form `required`/`too_long {max}`/`control_chars`); `TermsFor` and `Public` answer a module term's override, else its `en` default when the language has none (`TestTermsFor_ModuleTerm_DefaultsAndOverride`); `Terms` lists module terms with their `Area` and only declared ones (`TestTerms_ListsDeclaredModuleTermsWithArea`); a stored row of an undeclared term is ignored at load and kept (`TestLoad_UndeclaredTermRow_IgnoredAndKept`); `SetTerms` on an undeclared module term → Invalid `unknown` (`TestSetTerms_UndeclaredModuleTerm_ReturnsInvalid`). The shell's `term.lead*` entries join the decision-7 equality test.
17. (V3, Approved D57; applied 2026-10-30, D57, in module-contract.md) **`plan/design/module-contract.md`:** `Manifest.Terms []TermDecl` (the aliases above; `Validate` runs each `TermDecl.Validate` plus the in-manifest duplicate and reserved checks under `terms[i]`); `ModuleSettings.Terms []TermDecl` so `Plan.Settings()` carries them; decision 8's derived keys gain `module.<name>.term.<key>`, `.plural`, `.title`, `.plural_title` for each declared term; the rule "a module's messages and templates use only `CoreTerms` and its own `Terms`" with the `app` test that enforces it (V3.3.6). `add-module` skill: one line under Settings/Terminology naming `Manifest.Terms` and the four catalogue keys.
18. (V3, Approved D57; applied 2026-10-30, D57, in web-shell.md) **`plan/design/web-shell.md`:** `termsOf` also reads `module.<name>.term.<key>[.<form>]` entries as `term_<key>[_<form>]` arguments, with a `term.<key>` entry taking precedence (the regional's word over a module's default); a test that a module term renders from the fallback with no public document and from the document once it arrives.
19. (V3, Approved D57; applied 2026-10-30, D57, in app.md) **`plan/design/app.md`:** `settingsAreas` builds an area for a module that declares settings *or* terms (today a module with no settings has no area); the catalogue test gains `TestCatalogue_ModuleTerms_MatchManifestDefaults` and `TestCatalogue_TermsUsed_AreDeclared` (V3.5 F168, F169).
20. (V4, Approved; applied 2026-10-30, D58, in web-shell.md "Data") **`plan/design/web-shell.md`, the browser-state table ("What the browser holds"):** one row, "Router state (`history.state`, what a `Link`'s `state` prop carries)": a module screen may pass a small object of module-internal paths and fixed tokens (this module's `{back: {to, kind}}`, V4.2), never a name, an e-mail address, free text or anything about a person; the receiving screen validates it and falls back to a default when it is missing or malformed, because history state survives a reload and can be absent on a deep link; lifetime the browser's history. No shell code changes; the row records the rule the first consumer sets so the next module copies it rather than inventing one.

## Amendment 2026-10-20 (approved, owner, D44)

The D43 visibility amendment is approved (Status line). Proposed changes 1 to 10 are settled as follows:

1. (applied 2026-10-20, D44, in people.md) the `people.membership` extension point and `Provider`; this module contributes its `member` rows, and `people.contacts.read` is seeded into `department-admin`.
2. (applied 2026-10-20, D44, in module-contract.md and the skills) `Deps.Jobs` and `Deps.Files`, and the `Conformance` fakes.
3. (applied 2026-10-20, D44, in departments-events.md) positions are standing, so event set-up copies shifts only.
4. (pending: needs a design choice) `settings` terms for modules (`Area.Terms`): the term-name collision rule across areas and where a module's defaults come from are not decided in the proposal; this module uses its own catalogue keys until then. Reported to the owner.
5. (applied 2026-10-20, D44, in files.md) the example record kind is `volunteers.qualification-record`; `module.Route.MaxBodyBytes` and `Timeout` exist (api.md decision 32).
6. (applied 2026-10-20, D44, in `plan/07-abstractions.md`) the `volunteers` line under "Modules".
7. (applied) `plan/design/README.md` row 19 and the roadmap (`plan/05-roadmap.md`, Phase 1); the `burn-domain` skill additions wait for the main session.
8. (applied 2026-10-20, D44, in people.md) `Directory.LegalNamesOf`, and `volunteer-coordinator` in `StarterRoles` of `people.contacts.read` and `people.directory.read`.
9. (applied 2026-10-20, D44, in authz.md) the `volunteer-coordinator` role; `Seed` applies the key once the role exists.
10. (applied 2026-10-20, D44, in `plan/05-roadmap.md`) privacy tags are built before this module's services; the `burn-domain` sentence is the main session's.
