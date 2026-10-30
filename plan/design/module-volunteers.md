# volunteers

- **Status:** Implemented (2026-10-23; V1 D49, V2 D50, D51; V3 and V4 2026-10-30, D57, D58; demo seed D53, D56); Approved (2026-10-20, D43; D42's answers folded in; the D43 visibility amendment approved 2026-10-20, D44)
- **Package:** `internal/modules/volunteers` (leaf `volunteers/vol`, `volunteers/postgres`)   **UI:** `web/src/modules/volunteers/`
- **Depends on specs:** `model`, `lib-errs`, `lib-core` (`clock`, `validate`), `db`, `module-contract` (`Manifest`, `Deps`, `PersonData`, `moduletest`), `module-hello` (the shape to copy), `identity` (`PersonFrom`), `authz` (`Allow`, `AllowIn`, `ExtentOf`, starter roles), `people` (`Deps.People`, `contracts/personpage`), `departments-events` (`Deps.Departments`, `Deps.Events`, `contracts/eventsetup`, the two signals),
  `audit`, `signals`, `notify` (`Deps.Notify`, `Manifest.Templates`), `settings` (`Deps.Settings`), `privacy-tags` (Approved, D44: `privacy.Group`, `Filter`, `Manifest.Views`/`Groups`), `jobs` (Approved, D50: `Deps.Jobs`), `files` (Approved: `Deps.Files`, `ReadUpload`, `Serve`, the per-route body cap), `api` (`httpx`), `web-shell`, `app` (`modules.go` line)   **Used by:** `app` (registry),
  `people` (person-page section, `PersonData`), `events` (event set-up provider); later `incidents`, `afterreports` and a trust feature through the signals and the capability sketched under "Later"

## Summary

**Purpose.** Staff a department's shifts with volunteers who hold what the position needs. The first feature module (D26, Phase 1). Five parts in one module:

- a **catalogue of qualifications** (department-specific or org-wide);
- **qualification records** that only leads verify (D40, D41);
- **positions** (roles in a department with qualification requirements and an access rule);
- **membership** (who belongs to a department);
- **shifts** (blocks of time in a position, usually for an event) with **sign-ups**.

Imports `model`, `lib`, `core/module`, `core/identity` (`PersonFrom`, as the `actorFunc` seam), `core/authz` (leaf types), `core/audit` (`Ref`), `core/signals`, `core/notify`, `core/settings`, `core/privacy` (D43), `core/jobs`, `core/files`, `core/people`, `core/departments`, `core/events`, `contracts/personpage`, `contracts/eventsetup`. Never another module, `api`, `app`, `config`.

**Exported (`volunteers`).**

- `Manifest` (`Name: "volunteers"`); `New(ctx, deps) (module.Module, error)` (no I/O; nil `DB`, `Authz`, `People`, `Departments`, `Events`, `Audit`, `Signals`, `Settings`, `Notify`, `Jobs`, `Files` fail `Invalid`).
- Permissions: `PermissionSignUp = "volunteers.shifts.signup"` (site); all `ScopeDepartment`: `PermissionShiftsManage`, `PermissionRostersRead`, `PermissionPositionsManage`, `PermissionMembersManage`, `PermissionQualificationsManage`, `PermissionQualificationsRead`, `PermissionQualificationsVerify`.
- Setting keys: `SettingReminderLead = "reminder_lead"`, `SettingCancelCutoff = "cancel_cutoff"`, `SettingExpiryNoticeDays = "expiry_notice_days"`, `SettingSelfUpload = "self_upload"`, `SettingRetention = "retention"`, and the two `privacy` groups' settings (D43) `SettingVisibilityShiftMates = "visibility_shift_mates"` and `SettingVisibilityLegalName = "visibility_legal_name"`.
- `Manifest.Groups = vol.Groups`; `Manifest.Views` = every `vol` response type (privacy-tags); `Manifest.Terms` = `shift`, `position`, `qualification` (V3, D57; `lead` is a core term).
- Signals: `SignalSignupBooked = "volunteers.signup-booked"`, `SignalSignupCanceled = "volunteers.signup-canceled"`, `SignalQualificationVerified = "volunteers.qualification-verified"`; payload keys `signup`, `shift`, `person`, `department`, `record`, `qualification`.
- Jobs: `JobShiftReminder = "volunteers.shift-reminder"` (deferred), `JobExpiryScan = "volunteers.expiry-scan"` (daily), `JobPurge = "volunteers.purge"` (daily), `JobCancelShifts = "volunteers.cancel-shifts"` (on demand, from the two signal handlers).
- Templates: `volunteers.shift-reminder`, `volunteers.qualification-expiring`, `volunteers.qualification-verified`, `volunteers.qualification-rejected`, `volunteers.signup-removed`.
- Contributions: section `volunteers.summary` on the person page; an `eventsetup.Provider` that copies a previous event's shift grid within the caller's extent; a `demoseed.Provider` (`demo.go`, D53).

**`vol` (leaf).**

- IDs: `PositionID`, `QualificationID`, `RecordID`, `MemberID`, `ShiftID`, `SignupID` (= `model.ID[...]`); `Date{Year, Month, Day}` (civil date, JSON `YYYY-MM-DD`); `Access` (`AccessOpen`, `AccessMembers`).
- Domain types: `Qualification`, `Record` (+`Verified()`, `ValidOn(Date)`), `Position` (+`Requirements []QualificationID`, `HideShiftMates bool`), `Member`, `Shift` (+`Canceled()`, `Times() model.TimeRange`), `Signup` (+`WaivedBy`), `SignupStatus` (`Booked`, `Canceled`), `CancelReason` (`self`, `lead`, `shift_canceled`, `department_archived`, `event_closed`).
- Inputs and queries: the `*Input` and `*Query` types. `VerifyInput` and `RecordInput` gain `NoExpiry bool`. `ShiftQuery` (gains `EligibleOnly bool`, V4, D58), `ScheduleQuery` and `MemberQuery` embed `Page{Limit int; After Cursor}`. `Cursor` is an opaque string, `Invalid malformed` when it does not parse.
- Views: `ShiftSummary{Shift, DepartmentName, PositionName, Booked, Mine, Eligibility, Mates *ShiftMates}`; `ShiftMates{Names []string, Unlisted int}` (playa names only); `Eligibility{Eligible bool, Missing []QualificationID, Reason string}`; `RosterEntry{Signup; PlayaName; LegalName *string; Eligible}`; `ScheduleEntry`; `MemberView`.
- List wrappers: `RecordList{Items, SelfUpload *bool}`, `ShiftList{Items, Next Cursor}`, `ScheduleList{Items, Next}`, `MemberViewList{Items, Next}` (`items` is `omitzero` over a non-nil slice, so an empty list is `{"items":[]}`).
- Every type has `Validate()`; every field that reaches the wire carries a `json` and a `privacy` tag (D43).
- `Groups = []privacy.Group{{Name: "shift_mates", Permission: PermissionSignUp, Default: holders, Options: nobody|booked|holders|signed_in, Extra: booked}, {Name: "legal_name", Permission: "people.contacts.read", Default: holders, Options: nobody|holders}}` (`self` removed, Q-VOL8).
- Constants: `MaxNameBytes = 100`, `MaxDescriptionBytes = 2000`, `MaxLabelBytes = 100`, `MaxLocationBytes = 200`, `MaxSlots = 200`, `MaxShiftDuration = 48h`, `MaxValidityDays = 3650`, `MaxRequirements = 20`, `MaxList = 500`, `MaxIDs = 200`.

**Service interfaces** (all on `*Service`, safe for concurrent use, nothing to close):

- `Qualifications`: `ListQualifications`, `CreateQualification`, `UpdateQualification`, `ArchiveQualification`.
- `Records`: `RecordsOf`, `RecordQualification`, `UploadCertificate`, `VerifyRecord`, `RejectRecord`, `RemoveRecord`, `PendingRecords`, `OpenCertificate`.
- `Positions`: `ListPositions`, `CreatePosition`, `UpdatePosition`, `ArchivePosition`, `SetRequirements`.
- `Members`: `ListMembers`, `AddMember`, `RemoveMember`, `MyDepartments`, `Leave`.
- `Shifts`: `ListShifts`, `GetShift`, `CreateShift`, `UpdateShift`, `CancelShift`, `CopyShifts`.
- `Signups`: `SignUp`, `Assign`, `CancelSignup`, `Roster`, `MySchedule`, `Listed`, `SetListed`.
- Plus `PersonData`, `Sections` (personpage), `SetUp` (eventsetup).
- Return types: `RecordsOf` returns `RecordList`, `ListShifts` `ShiftList`, `MySchedule` `ScheduleList`, `ListMembers(ctx, MemberQuery)` `MemberViewList`.

**Key decisions** (one line each; the full text is in "Key decisions" below, and other files cite them as "decision N").

1. A lead is whoever holds the department-scoped permission there; lead permissions are seeded into `department-admin` only (D41).
2. A record counts only once a lead verifies it; a self-upload is `pending`; `self_upload` defaults true (D42).
3. A department's qualification is verified with `AllowIn`; an org-wide one by any lead (`ExtentOf` not zero, D41).
4. Eligible = every requirement has a verified record valid on the shift's start date in its zone; age limits are qualifications.
5. A position's `access` is `open` or `members`; membership is a row a lead adds or a person leaves.
6. A shift belongs to one position and optionally one event (null = standing work), and always has an IANA zone.
7. Bookings serialise per person then per shift; none overlap; only `Assign` with `Waive: true` waives (recorded, audited, flagged).
8. The shift reminder is a deferred job keyed by the sign-up ID; the expiry notice is a daily scan deduped by a column.
9. The certificate is a `files` upload on the record, served by this module's route after its own check; the record is `internal`.
10. The roster is for `rosters.read` holders; others see who is on a shift by playa name only, as the regional sets (`visibility_shift_mates`, `HideShiftMates`, `SetListed`).
11. Sign-ups never carry to a new event; shifts do (event set-up, `CopyShifts`, idempotent through `copied_from`).
12. Archiving a department or closing an event cancels its future shifts and bookings from signal handlers.
13. Retention is one key, `volunteers.retention` (0 = forever, D30), enforced by `volunteers.purge`; memberships are never purged.
14. Approval step, check-in, hours, swaps, anchored shifts and a capability are all Later.
15. Regional differences are the seven settings, the catalogue, the positions and the roles.
16. The org-wide catalogue is managed site-wide (`department-steward`), a department's by its `department-admin` (D42); `internal` fields use `omitempty`/`omitzero` (D46).
17. "Anyone who volunteers" is a holder of `volunteers.shifts.signup`, not a person with a booking (D43).
18. Roster legal names are the `legal_name` group (default holders of `people.contacts.read`); the `volunteer-coordinator` role (D43).
19. The per-person opt-out of the shift-mate list stays (D43).
20. A shift ID the caller cannot see is `NotFound` on every method; `Permission` only on a visible one.
21. Withdrawn (Q-VOL7, owner: yes): a lead may waive a requirement for themself, recorded and flagged like any waiver.
22. Long lists page by keyset behind an opaque `Cursor`; a page may be short but `Next` never skips a row.
23. The two signal handlers enqueue `volunteers.cancel-shifts`, which cancels in batches of `MaxList`.
24. `CopyShifts` keeps each shift's wall-clock times in the destination event's zone.
25. A reminder due beyond `jobs.MaxDelay - 24h` is enqueued at the horizon and the handler re-enqueues itself; this rests on jobs decision 23.
26. Playa names are unique and permanent in `people` (P1); this module only requires a playa name to book any shift (`Conflict unnamed`).
27. Legal names stay with `people.contacts.read` holders; the `legal_name` options are `nobody` and `holders` (Q-VOL8).
28. A shift's event is immutable after creation (`Invalid scope` on `event`).

**Errs kinds a caller handles.**

- Every use case: `Unauthenticated` (no actor); store kinds (`Unavailable`, `Timeout`, `Canceled`); `Unavailable` wrapping `db.ErrReadOnly` on writes.
- `Permission` (an action on a record the caller may see); `NotFound` (unknown ID, or one outside the caller's extent; never `Permission` for an invisible record).
- `Invalid` with field codes `required`, `too_long {max}`, `control_chars`, `range`, `malformed`, `one_of`, `too_many {max}`, `unknown`, `self`, `scope`, `hidden`.
- `Conflict` with codes `taken`, `archived`, `canceled`, `closed`, `full`, `overlap {shift}`, `unqualified {missing}`, `not_member`, `started`, `ended`, `cutoff`, `duplicate`, `status`, `in_use`, `pending_exists`, `unnamed` (the person booking has no playa name yet; a name clash cannot reach this module because people refuses it at the profile, P1).
- `Unsupported` (self-upload off; notify unconfigured is swallowed, never returned); `files`' kinds pass through `UploadCertificate` and `OpenCertificate` unchanged.

**Owns.**

- Schema `volunteers`: tables `qualification`, `qualification_record`, `position`, `position_requirement`, `member`, `shift`, `signup` (`booked_as`, `waived`, migration `0002`), `unlisted`.
- Eight permissions; seven settings (two of them `visibility_*` from the two `privacy` groups); three signals published, two subscribed (`departments.department-archived`, `events.event-closed`); four jobs; five templates; one person-page section; one event-setup contribution; one demo seed provider.
- Audit actions `volunteers.<record>.<verb>` (Behavior); routes under `/api/volunteers/` (Interface); screens under `/volunteers/` (UI); catalogue keys `module.volunteers.*`; terms `shift`, `position`, `qualification` (`Manifest.Terms`).

**Expects of dependencies** (the full list is in "Expects of dependencies" below).

- `jobs` (Approved, with J1, D50): `Deps.Jobs`, `Enqueue` in the transaction, `MaxEvery >= 24h`, and decision 23 (the latest request for a `Key` wins).
- `files` (Approved): `Deps.Files`, `Stage`/`Attach`, `Open` auditing a `personal` read, `ReadUpload`, `Serve`, the per-route body cap.
- `people`: `NamesOf`, `LegalNamesOf`, `people.directory.read` for leads, and Amendment P1 (unique, permanent playa names; Approved, D48).
- `privacy-tags` (Approved, D44), `authz` (`ExtentOf`, `AllowIn`, the `volunteer-coordinator` role), `departments-events` (`Active`, `NamesOf`, `InfoOf`, the two signals).
- `notify`, `settings` (incl. `TermDecl` and `TermsFor`), `audit`, `module-contract` (`Manifest.Terms`), `web-shell` (`useFormat`, `FileField`/`Attachments`, `termsOf`).

**Open questions:** none. Q-VOL1 to Q-VOL13 are answered and folded in (see "Open questions" below; the record is in `history/module-volunteers.md`).

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
## Expects of dependencies

- **`jobs`** (Approved, with Amendment J1, D50; `plan/design/history/jobs.md`):
  - `Deps.Jobs jobs.Registrar`, `Register` in `Start`, `Enqueue` joining the transaction, `InJob`; `MaxEvery >= 24h`.
  - Jobs decision 23, "the latest request for a `Key` wins": an `Enqueue` of the sign-up's `Key` from inside the reminder's own run is held and applied when the run ends, and one against a finished row reopens it.
  - Without that rule the reminder never fires for a booking beyond `ReminderHorizon`, and a shift moved within `jobs.DoneTTL` of a sent reminder is not reminded again.
- **`files`** (Approved):
  - `Deps.Files files.Store`; `Stage` outside the caller's transaction and `Attach` inside its `Write` transaction (with a deferred `Discard`); `ByRecord`; `Open` auditing a `personal` read; `DeleteByRecord`; `ReadUpload`; `Serve`.
  - `module.Route.MaxBodyBytes`/`Timeout` (files Proposed change 1); `Accept` images and PDF.
- **`authz`:**
  - `ExtentOf` for every list, `AllowIn` for one department's record.
  - The `volunteer-coordinator` starter role (D44, migration `0003`; `Seed` applies the key at the first `bms migrate` after the role exists).
- **`people`:**
  - `NamesOf`; `people.directory.read` seeded to `department-admin` (leads search people to add members and record qualifications).
  - `LegalNamesOf` (people.md, D44: `people.contacts.read` in any extent, audited per person; `peopletest.Fake.AddLegalName`). Q-VOL8: no change asked of people; this module calls it only for a viewer who holds the permission somewhere (F137, F138).
  - `NamesOf([person])` inside the booking transaction for decision 26 (one ID; a missing entry or `""` means no playa name).
  - **Amendment P1** (Approved, D48): playa names unique across the installation (`people.NameKey`, a unique index, `Conflict taken` on `UpdateMe`/`Create`) and read-only once set. This is what lets this module show shift-mates and rosters by playa name alone with no clash check of its own (Proposed change 14); this module uses none of P1's exported names.
- **`privacy-tags`** (Approved, D44): `Group`, `Settings`, `Check`, `PolicyOf`, `ViewerOf`, `Filter`, `Policy.Shows`, `Manifest.Views`/`Groups` through module-contract.
- **`departments`/`events`:** `Active`, `NamesOf`, `InfoOf` (`Zone`, `Dates`, `Closed`), the two signals, `GET /api/departments` and `GET /api/events` for pickers.
- **`notify`:** scoped `Send`; `Unsupported` treated as no e-mail.
- **`settings`:** a scoped `Reader`; also `TermDecl`, `Area.Terms`, `TermsFor` over declared terms (V3, D57).
- **`audit`:** a `Recorder` scoped `volunteers`.
- **`module-contract`:** `Manifest.Terms` (V3, D57).
- **`web-shell`:** `useFormat` in the shift's zone; `FileField`/`Attachments` (files Proposed change 7); `termsOf` over `module.<name>.term.*` (V3, D57).

## Key decisions

Other files cite these as "decision N", so the numbers stay.

- **1.** a lead is whoever holds the department-scoped permission at that department; the starter roles seed every lead permission into `department-admin` only, never `participant`, which is how D41 ("only department leads verify") is kept as data a regional cannot loosen by accident.
- **2.** a qualification record is verified by a lead or it does not count: a self-upload creates a `pending` record (`verified_by` null) and only `VerifyRecord` makes it count; there is no self-attestation path; self-upload is on by default (`self_upload = true`, D42).
- **3.** a department-specific qualification is verified with `AllowIn(verify, q.Department)`; an org-wide one with `ExtentOf(verify)` not zero (any lead anywhere, D41).
- **4.** a position's requirements are a set of qualifications; a person is eligible for a shift when every requirement has a verified record valid on the shift's start date in the shift's zone; age limits are a qualification ("Over 18", org-wide, indefinite), not a profile field or code.
- **5.** a position's `access` is `open` (anyone eligible) or `members` (department members only); membership is a row a lead adds or a person leaves; join requests are Later.
- **6.** a shift belongs to one position and optionally one event (`event_id` null = standing work such as winter inventory, Q-CORE10) and always carries an IANA zone (copied from the event, required otherwise).
- **7.** bookings are serialised per person then per shift (advisory lock then `FOR UPDATE`), so slots and the no-overlap rule hold under concurrency; a person's booked shifts never overlap (`model.TimeRange.Overlaps`); a lead may `Assign` someone who lacks a requirement only with `Waive: true`, which records `waived_by`, audits `Reason waived` and flags the person on the roster; `SignUp` never waives (D42).
- **8.** the shift reminder is a deferred job keyed by the sign-up ID, so a changed shift time moves it and the handler re-reads state; the expiry notice is a daily scan deduped by a column, not by notify's outbox.
- **9.** the certificate is a `files` upload on the record (`Ref{Kind: "volunteers.qualification-record"}`, subject the person, level `personal`), served from this module's route after its own check; the record itself is `internal` (D42).
- **10.** (D42, reshaped by D43) the full roster (person IDs, eligibility, waivers, links to the person page) is for `rosters.read` holders; volunteers see **who else is on a shift by playa name only** (`ShiftSummary.Mates` on `GetShift`, never a legal name, pronoun, contact or ID), as the `privacy` group `shift_mates` whose audience the regional sets in `visibility_shift_mates` (`nobody`;
  `booked`: only once the viewer holds a booking on that shift; `holders`: anyone who may volunteer, that is holders of `volunteers.shifts.signup`, the default per D43;
  `signed_in`: anyone who can see the shift), a position's `HideShiftMates` tightening it, and a person's own choice not to be listed (`SetListed(false)`): a person with no playa name yet, or unlisted, is counted in `Unlisted`, never named; these reads are `internal` and not audited.
- **11.** sign-ups never carry to a new event; shifts do, through `events.event-setup` (within the caller's `shifts.manage` extent) and a lead's `CopyShifts`, idempotent through `copied_from`.
- **12.** archiving a department or closing an event cancels its future shifts and their bookings from a signal handler (system actor, idempotent on state), telling each person once.
- **13.** retention is one key, `volunteers.retention` (0 = forever, D30), enforced by `volunteers.purge` on ended shifts' sign-ups, canceled shifts and long-expired records (with their files); memberships are current state and are never purged.
- **14.** no approval step on sign-up, no check-in or hours credit, no swaps, no anchored shifts, no capability yet: all Later.
- **15.** regional differences are the seven settings (two of them visibility audiences, D43), the catalogue, the positions and the roles.
- **16.** the org-wide catalogue is managed site-wide (`qualifications.manage` seeded into `department-steward`); a department's entries by that department's `department-admin` (D42). Every `internal` response field carries `omitempty`/`omitzero` and the screens read an absent count or flag as zero (D46).
- **17.** (D43) "anyone who volunteers" is a holder of `volunteers.shifts.signup`, not a person with a booking: the list has to be visible before the first booking for the owner's reason to work, and a regional that restricts who may volunteer already restricts that permission.
- **18.** (D43) legal names on the roster are the `privacy` group `legal_name` (`RosterEntry.LegalName`, `personal`, audience set in `visibility_legal_name`, default `holders` of `people.contacts.read` covering the shift's department), loaded through `Deps.People.LegalNamesOf` (people Proposed change, privacy-tags 3) only when the policy shows them, and audited twice by design: people's `people.person.read` for its data and this module's `volunteers.roster.read` for the roster; a new starter role `volunteer-coordinator` (authz, privacy-tags Proposed change 2) holds `rosters.read`, `qualifications.read` and people's two read permissions, so a fresh installation has the role the owner named.
- **19.** (D43) the per-person opt-out stays: the owner's scenario has two sides (someone avoiding a person also has reason not to be found by them), the count still tells the viewer someone is withheld, and the lead's roster is unaffected.
- **20.** every method that takes a shift ID applies the visibility rule first: an invisible or unknown shift is `NotFound`, a visible one the caller may not act on is `Permission`; qualifications, positions and departments are visible to every signed-in person, so their write methods answer `Permission` and only an unknown ID is `NotFound`.
- **21.** withdrawn (Q-VOL7, owner 2026-10-22: yes): a lead may waive a requirement for themself; `Assign` with `Person == actor` and `Waive` records `waived_by` = the actor, audits `Reason waived` and flags the roster exactly as for anyone else; nothing distinguishes a self-waiver except that the two IDs match.
- **22.** long lists page by keyset `(starts, id)` (members: `(created_at, id)`) behind an opaque `Cursor`; a page may be short (rows dropped after the fetch) but `Next` never skips a row.
- **23.** the two handlers enqueue `volunteers.cancel-shifts` (key `<reason>:<department or event ID>`) and the job cancels in batches of `MaxList` shifts per transaction, so handlers stay quick and no transaction grows with the department.
- **24.** `CopyShifts` keeps each shift's wall-clock start and end in the destination event's zone, shifted by whole days (the civil difference of the events' start dates), so a copy across a DST change keeps "08:00".
- **25.** the reminder horizon: a reminder due beyond `jobs.MaxDelay - 24h` is enqueued at the horizon and the handler re-enqueues itself (same `Key`) while `Starts - lead` is still ahead, so a far booking or a far-moved shift is never reminded early or never; this re-enqueue happens inside the handler's own run, so it rests on jobs decision 23 (a request for a `running` key is held and applied when the run ends), not on decision 3's queued-row move, and the tests prove it against a queue that keeps row state (F112a, F112b, F112c).
- **26.** (owner 2026-10-22, Q-VOL10 and Q-VOL11) **playa names are unique across the whole installation and permanent once set, and `people` enforces it** (Amendment P1: a unique index on a name key, `Conflict taken` on `UpdateMe`/`Create`, the name read-only once set), so two people booked anywhere never share a name and this module keeps no name check, no name lock and no name comparison of its own;
  what it keeps is the precondition that **a person booking any shift, event or standing, has a playa name**: `SignUp` and `Assign` read `Deps.People.NamesOf([person])` inside the booking transaction and answer `Conflict unnamed` when the entry is missing or `""`, with or without `Waive` (a waiver is about qualifications; the name is set in people, by the person or by a lead with `people.profiles.manage`);
  the lock order is person → shift (decision 7).
- **27.** (Q-VOL8: no) legal names stay with `people.contacts.read` holders: the `legal_name` group's options are `nobody` and `holders`; `self` is removed because, without people answering a non-holder, it could only ever show a holder their own name, which `holders` already does; the service asks people only when the viewer holds the permission somewhere.
- **28.** (kept after the name rule moved to people) a shift's event is immutable after creation (`UpdateShift` answers `Invalid scope` on `event`): the zone and the dates every booking was judged against follow the event (decision 6), `events.event-closed` cancels by event (decision 12) and `CopyShifts` dedupes by `copied_from` (decision 11), so a moved shift would carry bookings, a zone and a copy record into an event none of them were checked for; a lead cancels and recreates, or copies.

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

Handlers are thin (`httpx.ReadJSON`, `httpx.WriteJSON`, `httpx.Fail`); list responses are wrapped objects (`{"items": [...]}`), as the M2 build settled. Every wrapper's `items` is `omitzero` and the service returns a non-nil slice, so an empty list encodes `{"items":[]}` and a client never has to treat an absent key as empty (D46 forbids only a placeholder for a hidden field; an empty list is the value).
The paged wrappers add `next`, absent on the last page; a handler parses `after` into `vol.Cursor` without decoding it (the service validates). `GET` routes change nothing. The OpenAPI fragment names schemas `VolunteersQualification`, `VolunteersRecord`, `VolunteersPosition`, `VolunteersMember`, `VolunteersShift`, `VolunteersShiftSummary`, `VolunteersSignup`, `VolunteersRoster`, `VolunteersScheduleEntry`.

## Types

Defined in `vol` above. Uses `model.PersonID`, `model.DepartmentID`, `model.EventID`, `model.TimeRange`, `files.FileID`, `files.Multipart`, `files.File`, `files.Content`. Every `Validate()` joins `validate.FieldError`s with the codes listed under Summary; names are trimmed and refuse control characters; a zone must load with `time.LoadLocation`.

## Data

Schema `volunteers`; migrations `postgres/migrations/0001_init.sql` and `0002_booked_as.sql` (version 2: `signup.booked_as`, backfilled `'lead'` where `booked_by is not null or waived_by is not null`, else `'self'`; and, D51, `signup.waived boolean NOT NULL`, backfilled `true` where `waived_by is not null`, else `false`; the migration has never shipped, so both columns ride in the one file).
Decision 26 adds no column and no index: playa names are never copied into this schema (they are people's data and would go stale on a rename), and the "who is booked at this event" query is served by the existing `shift (event_id, starts)` and `signup (shift_id, status)` indexes. Every table has `created_at`, `updated_at` (`timestamptz not null`).
Foreign keys to `core.person(id)`, `core.department(id)` and `core.event(id)` are `on delete restrict` (people calls `DeletePerson` first; departments and events are never deleted).

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

**`shift`:** `id`; `department_id`, `position_id` FK (a composite FK `(position_id, department_id) → position(id, department_id)` backs decision 6's "the position is of that department"; a violation is translated to `Invalid scope`, F71); `event_id uuid null` FK event; `label` (1..100); `starts`, `ends timestamptz` (check `ends > starts and ends - starts <= interval '48 hours'`); `zone text`; `slots int` (1..200); `location` (≤ 200); `notes` (≤ 2000; internal; the form says "no personal information here"); `canceled_at null`; `cancel_reason text null` (check in the five reasons); `copied_from uuid null` FK `shift` set null; `created_by uuid null` FK person.
Indexes `(department_id, starts)`, `(event_id, starts)`, `(position_id)`, unique `(copied_from, event_id) where copied_from is not null` (the idempotent copy). Nothing personal; `created_by` is internal; canceled rows purged by `volunteers.retention`.

**`signup`:** `id`; `shift_id` FK; `person_id` FK (internal: shift times with a person, per `privacy-data`'s table); `status text check in ('booked','canceled')`; `booked_by uuid null`, `waived_by uuid null`, `canceled_by uuid null` FK person (internal); `booked_as text not null check in ('self','lead')` (who booked, as a role word: survives the lead's deletion, which nulls `booked_by`); `waived boolean not null` (D51: set true when `Assign` waives; survives the waiving lead's deletion, which nulls `waived_by`; not personal data on its own, `internal`, retained with the sign-up); `canceled_at null`; `cancel_reason null`.
Partial unique `(shift_id, person_id) where status = 'booked'` (`signup_booked_key`); index `(person_id, status)`, `(shift_id, status)`. Kept until `DeletePerson` or `volunteers.retention` after the shift ends (history and, later, credit).

**`unlisted`:** `person_id uuid pk` FK person restrict; `created_at`. One row per person who chose not to appear in shift-mate lists (`SetListed(false)`); no row = listed. The row itself is `internal` and read only by the person (`Listed`) and by the list computation; nobody is told who opted out, only how many booked people are unlisted. Kept until `SetListed(true)` (the row is deleted) or `DeletePerson`; never purged by retention (it is current state, like `member`).

**Operations and their permission:** every write names its permission in Interface; `ListShifts`, `GetShift` and the roster filter with `ExtentOf` (`shifts.manage`, `rosters.read`) and the membership join;
`GetShift`'s `Mates` needs no permission beyond seeing the shift: it is gated by the `shift_mates` policy (`visibility_shift_mates`: `holders` of `shifts.signup` by default, `booked` asserted from the actor's own booking) and the position's `hide_shift_mates`;
`Roster`'s `LegalName` is gated by the `legal_name` policy (`holders` of `people.contacts.read` covering the department by default) and by people's own check inside `LegalNamesOf`; the system paths used by the jobs, the signal handlers, `ExportPerson` and `DeletePerson` take no actor and are reachable only from those callers (never a route).

**Export** (`ExportPerson`, one JSON document): `memberships` (department ID and name, since), `listed` (`true`/`false`; absent when the module holds nothing about the person: an unknown person exports `{}`), `signups` (shift label, department, position, times, zone, status, reasons, whether a requirement was waived (the `waived` column, so it survives the waiving lead's deletion), who booked or canceled as role words, never names;
`booked_by` is `booked_as`, and `canceled_by` is derived from `cancel_reason`: `self` → `self`, `lead` → `lead`, the three system reasons → `system`, so a lead's later deletion loses nothing), `qualification_records` (qualification name, dates, verified yes/no and when, certificate file ID: files exports the bytes itself, files decision 13), `verified_for_others` (a count), `shifts_created` (a count).
**Deletion** (`DeletePerson`, inside people's transaction, the ctx given): delete the person's sign-ups, memberships, `unlisted` row and records (calling `files.DeleteByRecord` per record), then set `verified_by`, `added_by`, `booked_by`, `waived_by`, `canceled_by`, `created_by` to null where they name the person (`Anonymized`); `Kept` empty. Idempotent.

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

Own records need no permission: `RecordsOf(self)`, `UploadCertificate`, `RemoveRecord` of one's own pending record, `MyDepartments`, `Leave`, `MySchedule`, `Listed`, `SetListed`, `CancelSignup` of one's own booking, `OpenCertificate` of one's own.
Active catalogue entries and positions and open shifts are `internal` reads every signed-in person may make (they label what a person signs up for), like departments and events (departments-events decision 2).
**Shift-mates** (D42, D43) need no permission of their own: a playa name on a shift is `internal` (people's data table; privacy-data's "playa name, department, shift times" example), the same level as the fill count the shift card already shows; the regional's choice is the `visibility_shift_mates` setting, whose default audience `holders` is "anyone who volunteers" = holders of `shifts.signup`, and a person's choice is `SetListed`.
**Legal names** (D43) are people's data: the `legal_name` group's `holders` are holders of `people.contacts.read` (seeded into `account-admin` today and into `volunteer-coordinator` by people's proposed change), and people checks that permission again inside `LegalNamesOf`; this module declares no permission for them.
The `volunteer-coordinator` starter role (site; authz proposed change) is "the volunteer coordinator and maybe some other roles" of D43 made concrete: `rosters.read` and `qualifications.read` everywhere, `people.directory.read`, `people.contacts.read`; it manages no shifts (leads do).
The base role holds `shifts.signup` by default; a regional that wants invitation-only volunteering removes it or sets positions to `members`.

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

**`volunteers.cancel-shifts`** (on demand, `Timeout 5m`): in batches of `MaxList` shifts, each its own `Write` transaction: cancel the department's (or event's) non-canceled shifts whose `ends` is in the future (`cancel_reason` from the key's reason) and their booked sign-ups (same reason), audit `shift.canceled` and `signup.canceled`, publish `signup-canceled` and send `volunteers.signup-removed` once per sign-up (notify `Key` `removed:<signup>`); repeat until a batch is empty.
Idempotent on state: a retry resumes where it stopped. Args name a department or an event, never a person.

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

**Why `holders` is the default for `shift_mates`.** D43: playa names and who is on which shift are visible to anyone who volunteers. "Anyone who volunteers" is a holder of `shifts.signup` (decision 17), which the base role holds, so on a fresh installation every signed-in person sees the list before booking, which is what the owner's reason needs (people who don't get along pick another shift, D42).
The cost is bounded: only playa names, only to people who can already see that the shift exists and how full it is, and any person can take themself out of the list.
A regional that prefers "see them once you've committed" sets `booked`; one that wants rosters kept with the department sets `nobody`; `signed_in` matters only to a regional that took `shifts.signup` away from the base role and still wants browsers to see the names.
A department with a discreet team (peer support, for example, whose volunteers' identities the manuals treat with care) sets `HideShiftMates` on that position instead of asking the regional to turn it off for everyone; the position flag can only hide, never show what the setting hides.

**Why `holders` is the default for `legal_name`.** D43: real names are for the volunteer coordinator and maybe some other roles. `holders` of `people.contacts.read` is exactly that set, and the regional widens or narrows it by editing roles, not this setting; `nobody` is there for a regional that wants legal names out of rosters altogether.
(Q-VOL8) The owner's answer is that everyone sees playa names and legal names stay with permission holders, so there is no `self` option: people answers `LegalNamesOf` only for a holder, and privacy-tags already shows a holder their own entry under `holders`, so `self` could only have meant "holders, but each sees only their own", which no regional asked for and which would hide from the coordinator the very names the role exists to see.

Read at use-case or job time through `deps.Settings`, never cached. Catalogue keys `module.volunteers.setting.<key>.title|description`. A `Check` refuses an out-of-range value with `range`.

## Terms

(V3, D57; built 2026-10-30.) `shift`, `position` and `qualification` are regional words this module declares in `Manifest.Terms`, and `lead` is a core term (`settings.CoreTerms`): departments, volunteers, incidents and after-burn reports all say it, and a word several parts of bms need belongs to the core (Q-VOL12, owner, 2026-10-30, D57). The mechanism lives in `settings`, `module-contract`, `web-shell` and `app` (Proposed changes 16 to 19, applied); this module only declares and uses.

- **Declaration.** `Manifest.Terms` is `manifestTerms()`, beside `manifestSettings()`, `en` only; `module.go` gains `Terms: manifestTerms()` and nothing else in the Go code reads a term (response types carry tokens and IDs; the shell and notify render the words):

  ```go
  func manifestTerms() []module.TermDecl {
  	return []module.TermDecl{
  		{Key: "shift", Defaults: map[string]module.TermForms{"en": {Singular: "shift", Plural: "shifts", SingularTitle: "Shift", PluralTitle: "Shifts"}}},
  		{Key: "position", Defaults: map[string]module.TermForms{"en": {Singular: "position", Plural: "positions", SingularTitle: "Position", PluralTitle: "Positions"}}},
  		{Key: "qualification", Defaults: map[string]module.TermForms{"en": {Singular: "qualification", Plural: "qualifications", SingularTitle: "Qualification", PluralTitle: "Qualifications"}}},
  	}
  }
  ```

- **One flat namespace.** A term's key is the same string in storage, the admin document, the public document's `term_<key>` arguments and notify templates. A key declared by two enabled areas is a start-up `Invalid` whose fix is promoting the word to `CoreTerms`.
- **Storage.** No migration. A stored row of a term no area declares (a disabled module) is kept, read by nobody, refused by `SetTerms` (`unknown`) and returns when the module is enabled again.
- **Boot fallback is the module's own catalogue.** Every declared term has `module.volunteers.term.<key>`, `.plural`, `.title` and `.plural_title` in `web/src/modules/volunteers/messages/en.json`; the shell's `termsOf` reads them as `term_<key>` arguments, and a `term.<key>` entry (the regional's word from the public document) takes precedence. The `internal/app` catalogue test checks that the four keys exist for every declared term and equal the manifest's `en` defaults.
- **Messages.** Every "shift", "position", "qualification" and "lead" in `messages/en.json` is the `{term_*}` argument in the matching form: `_title` at the start of a sentence or as a heading; `_plural` where the count is not one; where a message uses ICU plural on a count, the `other` branch takes `_plural`. The main nav entry is `{term_shift_plural_title}`. **My volunteering** and **Volunteers** are not terms (`volunteer`, `waiver`, `roster` and `catalogue` are not terms). Permission and setting titles go through `t()` and get the same treatment.
- **Templates** (`qualification-verified`, `qualification-rejected`, `signup-removed`, the reminder) use `{{term "shift"}}`, `{{term "qualification"}}` and so on (the `_plural` and `_title` keys as notify documents them). The text avoids an article before a regional word ("a {{term "lead"}}", "a {{term "department"}}"): write "The {{term "lead"}} of your {{term "department"}}" or restructure the sentence (D58).
- **A module uses only core terms and its own.** The `internal/app` catalogue test scans every `{term_<x>...}` argument in `messages/en.json` and every `{{term "<x>"}}` in `Manifest.Templates` and fails on a key that is neither in `CoreTerms` nor in this module's `Terms`.
- **Terminology screen.** `/settings/terms` groups cards by area: the core's, then one fieldset per module headed by `module.<name>.title`.

Failure modes F167 to F170 are in the table below.

## UI

All screens phone-first (web-shell decision 21), `useT` with typed keys, `{term_department}`, `{term_event}`, `{term_participant}` for the core terms, times rendered with `useFormat` in the shift's `zone`, `ErrorState`/`ErrorSummary` for errors, no `style` prop; an absent `internal` count or flag reads as zero (D46).
Routes relative to `/volunteers/`; nav: main **Shifts** (`/volunteers/`, signed-in), **My volunteering** (`/volunteers/me`, signed-in); admin **Volunteers** (`/volunteers/admin`, access `[shifts.manage, positions.manage, members.manage, qualifications.manage, qualifications.verify, rosters.read]`).

| Screen | Who | Actions | Empty / error / forbidden |
|---|---|---|---|
| `/volunteers/` Shifts | every signed-in person | filter by event, department, day and **Only {term_shift_plural} I can take**; dense rows grouped by day; sign up from the row; **Show more** (see "Screen details") | "No shifts to show yet" with the filters (only when no row is shown and `next` is absent); a 403 on sign-up shows the shell's forbidden state |
| `/volunteers/shifts/:shift` | as visible | a back link; sign up and cancel my booking; who else is on the shift; the roster; Manage (assign, edit, cancel) (see "Screen details") | NotFound state for an invisible shift; canceled banner; "No one else is booked yet" when `mates` is present and empty |
| `/volunteers/me` My volunteering | self | upcoming and past bookings with cancel; my departments with leave; my qualifications and certificate upload; the listing switch (see "Screen details") | "You have no shifts booked" with a link to Shifts |
| `/volunteers/admin` | holders of any lead permission | the departments the viewer may manage or view (from `session.permissions` `p@D` and `GET /api/departments`; `LEAD_PERMISSIONS`), headed by what the person can do (Q-VOL13, D58): a site-wide holder sees "All {term_department_plural}" with the note "You can manage or view every {term_department}.", otherwise "{term_department_plural} you can manage or view"; pending certificates count | "No {term_department} you can manage or view yet." |
| `/volunteers/departments/:department` | leads | tabs **Shifts**, **Positions**, **Members**, **Qualifications**, **Pending** (see "Screen details") | each tab's empty state names the action; forbidden tabs are hidden by `session.permissions` and answer the shell's forbidden state if reached |
| `/volunteers/departments/:department/shifts/new`, `/volunteers/shifts/:shift/edit` | `shifts.manage@D` | the form: position, event (or standing work with a zone), label, start and end in the zone, slots, location, notes ("no personal information") | field errors inline (`module.volunteers.field.<code>`) |
| `/volunteers/people/:person` | `qualifications.read`/`.verify` holders | a person's records in the viewer's extent; **Record a qualification** (way (a); with the same **Never expires** switch); open certificate (through `Attachments`, as on the Pending tab); remove | NotFound outside the extent |
| `/volunteers/qualifications` | site-wide `qualifications.manage` | the org-wide catalogue: new, edit, archive | |

### Screen details

- **`/volunteers/` Shifts** (every signed-in person): filter by event, department, day, and **Only {term_shift_plural} I can take** (`?eligible=1`, server-side `eligible=true`, keeps the shifts the person holds; **Clear filters** removes it;
  D58, V4.3); dense rows grouped by day, one `<li>` per shift, three short lines on a phone and one grid row from 640 px: times only under the dated day heading ("{start} to {end} next day" when the end falls on a later day), the label as the row's link, then department and position (from `department_name` and `position_name` on the summary, no per-department position fetch) and location joined with " · ", the fill count "{booked} of {slots} booked" (V4.4), and the control: **Sign up**, **You're booked** or a short reason (`why_short.*`:
needs qualifications named, members only, full, overlap, already started; the shift page keeps the sentence); the list asks `limit=100` and shows **Show more** while `next` is present, appending the next page, also when a page is empty or short because the day filter or `eligible` dropped its rows
- **`/volunteers/shifts/:shift`** (as visible): a back link above the heading that returns to where the person came from (router state `{back: {to, kind}}` from the list, the lead's Shifts tab or My volunteering, validated by `backFrom`, default `/volunteers`; label "Back to {term_shift_plural}", "Back to My volunteering", or "Back to {department name}" ("Back to the {term_department}" before the shift loads); V4.2, D58); the **Slots** line says "{booked} of {slots} booked"; sign up (decision 26: a `409 unnamed` shows "Set your playa name on your profile first." with a link to `/people/me`), cancel my booking (a confirm `Dialog` "Cancel your booking?" whose dismiss button reads "Keep my booking", `module.volunteers.shift.keep_mine`, D57 records what is built);
  **Who else is on this shift** (when `mates` is present: the playa names, then "and {n} not listed" when `unlisted` > 0; plain text, no links; the section is absent when `mates` is null, with no hint why);
  **Roster** section (`rosters.read@D` holders only) with playa names linking to `/people/:person`, a legal name next to the playa name when `legal_name` is present on the entry (no column at all when it is absent on every entry), an eligibility flag and a **requirement waived** flag;
  **Manage** section (`shifts.manage@D` holders only, independent of the roster section): **Assign** (playa-name search through `GET /api/people/search`, a **Waive requirements** switch that explains the waiver is recorded; Q-VOL7: yes, the switch works for the viewer too; a `409 unnamed` shows "This person has not set a playa name yet."), edit, cancel (a confirm `Dialog` "Cancel this shift?" whose dismiss button reads "Keep this shift", `module.volunteers.shift.keep`, matching the booking dialog's "Keep my booking", D57); a manager without `rosters.read` sees Manage and no roster, a roster reader without `shifts.manage` sees the roster and no Manage
- **`/volunteers/me` My volunteering** (self): upcoming and past bookings (two toggle buttons (`aria-pressed`) in a labelled group, for `past=false`/`past=true`, each paged with **Show more**; an upcoming booking someone else canceled is shown struck through with its reason, "removed by a lead", "shift canceled", "{term_department} archived", "{term_event} closed", until the shift passes, Q-VOL9) with cancel; my departments with leave; my qualifications with status (pending, verified, expires on, expired, never expires) and **Upload a certificate** (`FileField`, shown when `self_upload` on the person's own `GET /people/{person}/qualifications` is true, hidden otherwise; the screen reads the flag from that body, never from `GET /api/settings`);
  **Show my playa name to others on my shifts** (a switch from `GET /me/listing`, saved with `PUT /me/listing`, with the note that leads always see the roster)
- **`/volunteers/departments/:department`** (leads): tabs: **Shifts** (by event, fill counts, new, copy from a previous event; paged with **Show more**), **Positions** (list, new, edit with a **Hide who's on shifts from other volunteers** switch, requirements picker, archive), **Members** (list with eligibility per position, add by search, remove; paged with **Show more**),
  **Qualifications** (the department's catalogue; org-wide ones read-only here), **Pending** (certificates to verify, the org-wide pending ones included and labelled "org-wide" (D41): open the file, correct dates, verify or reject; the verify form has an **Expires on** date and a **Never expires** switch that clears and disables the date, sending `no_expiry: true`; the certificate opens through the shell's `Attachments` row, which fetches through `@/api`: a 404 shows "This certificate is no longer available", a 503 with `Retry-After` the shell's "not available on this copy yet", and success opens the bytes; never a raw link to the route)

### Shift navigation and the compact list

(V4, D57 and D58.) Nothing here changes a table, a permission, a signal, a setting or a job; one query field is added to `ListShifts` (`ShiftQuery.EligibleOnly`, Interface and Behavior "Paging"); everything else is the browser.

- **The back link.** The shift page's back link returns to where the person came from, through router state, produced and read by `web/src/modules/volunteers/back.ts`:

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

- **Why validated.** History state is writable by any script on the origin and survives across deployments: a `to` outside this module would make the back link an open redirect inside the app, and an unknown `kind` would have no label. Nothing personal travels in it (a path, a query of IDs and filter values, a token), which is Proposed change 20's rule for `web-shell`.
- **Producers.** Every link to `/volunteers/shifts/:shift` passes `backTo(kind, location)`:
  - the list (`Shifts`, kind `shifts`; `to` keeps the list's query string, so a filtered list is returned to as filtered);
  - the lead's Shifts tab (`ShiftsTab`, kind `department`; `to` is `/volunteers/departments/${department}?tab=shifts`);
  - My volunteering (`MySchedule`, kind `me`);
  - the shift form: `ShiftNew` navigates to the created shift with kind `department`, and `ShiftEdit` reads the state it was opened with and passes it on, both on its own "Back to the shift" link and on the navigation after saving.
  - A link with no producer (the reminder e-mail, the person-page section's `/volunteers/me` link, a bookmark) has no state and gets the default.
- **Labels.**
  - `shift.back` "Back to {term_shift_plural}" for kind `shifts` and the default; `shift.back_me` "Back to My volunteering" for kind `me`.
  - For kind `department`: `shift.back_department` "Back to {name}" with `name` the loaded shift's `department_name`, and `shift.back_department_generic` "Back to the {term_department}" while the shift is loading or when it failed to load (the link still works: `to` does not depend on the shift).
  - The link is rendered above the heading, is never disabled, and is a plain `Link` (not a button).
- **The shift list** is dense rows grouped by day: each day keeps its `<section>` and `<h2>`; each shift is one `<li>` in the day's `<ul>`, one DOM with two layouts by CSS. Below `sm` (640 px, the kit's) it is three short lines; from `sm` one grid row [times | label | department · position · location | fill | control], rows separated by a border, no card box.
  - Phone lines: 1 the times and the label; 2 department · position · location (when set) · the fill count; 3 the sign-up control (**Sign up**, "You're booked", or a short reason), present only when there is one (a canceled shift says "Canceled" in line 2 and has no line 3).
  - **Times only.** `<time dateTime={starts}>` with `format.time(starts, zone)` to `format.time(ends, zone)`; the day heading keeps `format.date` of the day. When the end falls on a later day than the start (`dayKey(ends, zone) !== dayKey(starts, zone)`) the row says "{start} to {end} next day" (`module.volunteers.when_next_day`; the plain case reuses `module.volunteers.when`, "{start} to {end}").
    `ShiftWhen` (date and times) stays for the shift page, the schedule and the lead's tab; the list uses `ShiftTimes`.
  - **The label** is the row's link (`min-h-11`, the 44 px target), an `<h3>`, so the heading outline is page, day, shift.
  - **The muted line** joins `department_name`, `position_name` and `location` with " · ", skipping what is empty.
  - **The control** is `SignUpControl` with a `short` prop: the button and "You're booked" are unchanged; a reason is rendered from `module.volunteers.why_short.<reason>` instead of the `why.<reason>` sentence (`full` "Full", `not_member` "Members only", `overlap` "Overlaps a booking of yours", `started` "Already started", `closed` "{term_event_title} closed", `archived` "No longer offered", `unknown` "Not available", and a missing qualification "Needs {names}").
    The shift page keeps the sentences. An unknown reason token falls back to `why_short.unknown`.
- **Filters.** Event, department and day stay (event and department server-side, day a window server-side and exact in the browser). **Only {term_shift_plural} I can take** is a `Check` in the filter form, kept in the query string as `?eligible=1` (`replace: true`), cleared by **Clear filters**, counted in the phone's "filters are set, start open" rule, and server-side (`eligible=true`).
  It shows the shifts the person is eligible for **and the ones they already hold** (`eligibility.eligible || mine`), so it never hides a booking of theirs. It is off by default, and a regional cannot change that default (a per-visit choice, not a policy).
- **Paging and empty states.** `limit=100`, **Show more** while `next` is present; the empty state only when no row is shown and `next` is absent; a page whose rows were all dropped (by the day filter in the browser, or by `eligible` on the server) shows **Show more**.
- **One phrasing for the count:** "{booked} of {slots} booked", everywhere a shift's fill is shown (the list row, the shift page's **Slots** line, the lead's Shifts tab), as one key `module.volunteers.fill`. A full shift reads "4 of 4 booked"; the control's reason (`why_short.full`, `why.full`) still says "Full" on the sign-up line.
- **The admin heading is worded by what the person can do** (Q-VOL13, owner, 2026-10-30, D58). `/volunteers/admin` lists the departments the viewer may open, from `ledDepartments(session, LEAD_PERMISSIONS)` (six manage permissions and two read ones, `rosters.read` and `qualifications.read`).
  With `led.site` the heading is `admin.departments_all` "All {term_department_plural}" with the muted note `admin.departments_all_hint` "You can manage or view every {term_department}."; otherwise `admin.departments` "{term_department_plural} you can manage or view"; the empty state `admin.none` "No {term_department} you can manage or view yet."
- **Catalogue keys** (`web/src/modules/volunteers/messages/en.json`): `shift.back`, `shift.back_me`, `shift.back_department`, `shift.back_department_generic`, `when_next_day`, `fill`, `filter.eligible`, `why_short.full|not_member|overlap|started|closed|archived|unknown|missing`, `admin.departments`, `admin.departments_all`, `admin.departments_all_hint`, `admin.none`.
  The keys `free`, `full`, `shift.filled` and `tab.shifts.fill` do not exist (`MessageKey` typing makes a leftover use a build error). The fixed words in these strings are `{term_*}` arguments like every other string.

Job titles (jobs J-W1 decision 24, applied D58): `web/src/modules/volunteers/messages/en.json` holds `module.volunteers.job.shift-reminder.title` "Send {term_shift} reminders", `.expiry-scan.title` "Warn about {term_qualification_plural} about to expire", `.purge.title` "Delete volunteer records past their retention" and `.cancel-shifts.title` "Cancel the {term_shift_plural} of an archived {term_department} or a closed {term_event}" (the title the Background work screen shows for each job `volunteers.<ident>`; checked by `TestMessageCatalogues_EnglishCoversJobs`; no code change in the module).

The person page shows section `volunteers.summary` (title `module.volunteers.section.summary.title`): items **Departments** (names, to the viewer's extent), **Upcoming shifts** (count, `Href` `/volunteers/me` for the person themself).

## Behavior

- **Actor and checks, in order:** actor (`identity.PersonFrom`; none → `Unauthenticated`), input `Validate`, permission, then state. A record outside the caller's extent is `NotFound` (privacy-data); an action the caller may not take on a visible record is `Permission`.
  (decision 20) For a shift, "visible" is `ListShifts`' rule, evaluated for the actor before `AllowIn` in `GetShift`, `UpdateShift`, `CancelShift`, `SignUp`, `Assign` and `Roster`: in the actor's `shifts.manage` or `rosters.read` extent (any state), or not canceled and either open-access or members-only in a department the actor belongs to. Unknown and invisible are the same `NotFound`.
  Qualifications, positions, departments and events are visible to every signed-in person (they label what one signs up for), so their write methods answer `Permission` for a holder-less caller and `NotFound` only for an unknown ID (F7, F10, F45, F49 read that way; an archived position by ID is still visible: archiving hides it from pickers, not from the person who has its link).
- **Paging** (decision 22): `ListShifts`, `MySchedule` and `ListMembers` fetch `Limit + 1` rows in their stable order, return the first `Limit`, and set `Next` to the last returned row's cursor when the extra row exists.
  `ListShifts` drops rows whose department `Departments.NamesOf` no longer returns (archived since the handlers ran) after the fetch; `Next` still comes from the fetched rows, so the next page starts after the last fetched row and nothing is skipped; a short page is not "the end": only an absent `next` is.
  With `EligibleOnly` (V4, D58) the store fetches up to `MaxList + 1` rows in keyset order from `After` instead of `Limit + 1` (one query; eligibility is in Go, decision 4), the service computes `Eligibility` and `Mine` for the actor as above, keeps the rows that pass (eligible, or `Mine` set) and returns the first `Limit` of them; `Next` is the cursor of the last row the call consumed: the last returned row when fetched rows remain beyond it, the last fetched row when fewer than `Limit` passed and a `MaxList + 1`-th row exists, and empty otherwise, so a page is never longer than `Limit`, never skips a row, costs at most one `MaxList`-row scan, and can be empty with `Next` set (the screen shows **Show more**).
  Only `http.go` sets `EligibleOnly`; `SetUp` and the lead's tab never do. A cursor from another list, another order or another person's query simply yields the rows after that key (no secret is in it: a `starts` and an ID the caller could see).
- **Eligibility** (`eligible(person, shift, now)`): department `Active`; shift not canceled; event (if any) not `Closed`; position not archived; if `Access == members`, a `member` row exists; every requirement has a `Record.ValidOn(DateOf(shift.Starts, shift.Zone))`; `Booked < Slots`; no booked sign-up of the person whose shift `Times().Overlaps(shift.Times())`. `ListShifts` computes it for the actor with the actor's records and bookings loaded once.
- **Visibility plumbing** (D43, every use case that returns a `vol` response type): after the permission check and the load, `privacy.ViewerOf(ctx, authz, actor, Groups)` (A1, privacy-tags A1.2, in `plan/design/history/privacy-tags.md`: `actor` is the `model.PersonID` the use case took from `identity.PersonFrom` before its permission check; `viewer.Extents` is keyed by group name, A1.1, which changes nothing here since the service only hands `viewer` on to `Shows` and `Filter`) and `privacy.PolicyOf(ctx, settings, Groups)` once, then `privacy.Filter` per record with `Relation{Subject: the entry's person (zero for a shift or position), Department: the record's department, Also: [booked] when the viewer holds a booked sign-up on that shift}`; the audit entries the `Result`s call for are recorded in one `Record` before returning.
  Types with no grouped field (`Qualification`, `Position`, `Shift`, `Member`, `ScheduleEntry`, ...) still pass through `Filter` (it is cheap and keeps the rule uniform: a later grouped field needs no new call site).
- **Shift-mates** (`GetShift`, D42, D43): after the visibility rule passes, `Mates` is loaded when the position's `hide_shift_mates` is false and `pol.Shows(GroupShiftMates, viewer, rel)` is true (`visibility_shift_mates` read now, never cached; `rel.Also` carries `booked` when the actor holds a `booked` sign-up on this shift); otherwise `Mates` stays nil, `Filter` would zero it anyway, and the response says nothing about why.
  When loaded: the shift's `booked` sign-ups other than the actor's, joined with `unlisted`; for the listed ones `Deps.People.NamesOf` (one call, at most `MaxSlots` IDs); a person whose playa name is `""` or who is missing from the map (no profile yet, deleted) is counted in `Unlisted` with the opted-out, never shown by any other field (the module has no access to a legal name and asks for none).
  `Names` is sorted and never repeats (people P1 makes playa names unique across the installation; pronouns are not shown because the owner said names only). `ListShifts` never fills `Mates` (one `NamesOf` per shift would not scale on a list and the card shows the fill count). Canceled shifts show it too (what was booked is not secret from someone who could see the shift).
  Not audited: everything shown is `internal`, and the per-read audit a `personal` field would get would log every volunteer's every glance at a shift page without protecting anything (the same reasoning as the qualification record, decision 9).
- **Roster legal names** (`Roster`, D43): after `rosters.read@D` passes and the entries are loaded, the service asks `pol.Shows(GroupLegalName, viewer, Relation{Subject: entry.Person, Department: shift.Department})` per entry;
  (Q-VOL8: no) when any entry shows **and** `viewer.Extents[GroupLegalName]` is non-zero (the viewer holds `people.contacts.read` somewhere; privacy-tags shows a subject their own entry under `holders` regardless, but people answers only a holder, so a non-holder on their own roster is never asked for: no call, no `Permission` log line, F138), one `Deps.People.LegalNamesOf` call with those persons (people checks `people.contacts.read` for the actor and audits its own `people.person.read` per person; a `Permission` from people, which happens only if the group's permission and people's rule disagree, is answered by leaving every `LegalName` nil, never by failing the roster, and is logged once); a person people omits stays nil.
  (D45: for a viewer who holds `people.contacts.read` only in department scope, people answers only for people the `people.membership` providers report as members of that department, and for nobody while no provider is registered, so those entries stay nil; a site holder is answered for all. `Shows` decides the audience; people decides reach.) Then `Filter` per entry; the `Result`s with `legal_name` become one `volunteers.roster.read` entry per subject (`Fields [legal_name]`, `Level personal`, `Record` the shift), recorded in one call.
  Under `nobody`, or when `Deps.People` has no `LegalNamesOf` yet, people is never asked.
- **Listing choice** (`SetListed`): one `Write` transaction: `false` inserts the `unlisted` row (`ON CONFLICT DO NOTHING`), `true` deletes it; audits `volunteers.listing.changed` (`Subject` the actor, `Reason` `listed` or `unlisted`) only when a row changed, so a repeat is a no-op. Takes effect on the next `GetShift`; nothing is recomputed or notified.
  The choice is one per person, not per shift: someone avoiding another person wants to be absent from every list, and a per-shift switch would make them choose each time on a phone.
- **Booking** (`SignUp`, `Assign`): one `Write` transaction: `pg_advisory_xact_lock(hashtext(person_id))`, then (decision 26) the playa-name precondition below, then `SELECT ... FROM volunteers.shift WHERE id = $1 FOR UPDATE`, re-check eligibility, insert, audit `volunteers.signup.created` (Fields `[person, shift]`, `Reason` `self`, `lead` or `waived`;
  `waived` means `Assign` with `Waive: true` booked someone eligibility refused for a missing requirement, D42;
  `Waive: true` on an eligible person records nothing special and `WaivedBy` stays zero), publish `signup-booked`, and when `reminder_lead > 0` and `Starts - lead > now`, `Jobs.Enqueue{Name: volunteers.shift-reminder, Key: <signup id>, Args: {signup}, RunAt: min(Starts - lead, now + ReminderHorizon)}` where (decision 25) `ReminderHorizon = jobs.MaxDelay - 24h`: a reminder further ahead than jobs can schedule is parked at the horizon rather than dropped.
  `booked_as` is `self` from `SignUp`, `lead` from `Assign`. The partial unique index is the backstop for a duplicate (`Conflict duplicate`). (Q-VOL7: yes) `Assign` with `Person == actor` and `Waive: true` on an unqualified actor is an ordinary waiver: `waived_by` = the actor, `Reason waived`, the roster flag; the audit entry's `Actor` and `Subject` being the same person is the whole record of it.
- **Playa-name precondition** (decision 26; `SignUp` and `Assign` on every shift, event or standing): with the person lock held, `Deps.People.NamesOf([person])` once; a missing entry or `""` is `Conflict unnamed`, with or without `Waive`, before the shift lock is taken, so a refused booking holds nothing but the person lock.
  The roster, the shift-mate list, the radio and the lead's notices all go by playa name, and a booked person with none would be a blank line on each of them; standing work is included because its roster and shift-mates are the same screens (the earlier draft's standing-shift exemption existed only because the per-event clash check had no event to run in).
  The shell already prompts for a name once per session, and a lead with `people.profiles.manage` can set a walk-on's, so the refusal costs a minute. A `NamesOf` failure returns its kind and the transaction rolls back (F164).
  **Uniqueness is not this module's:** people Amendment P1 (owner, 2026-10-22, Q-VOL10 and Q-VOL11) makes playa names unique across the installation under `people.NameKey` and read-only once set, refusing `Conflict taken` at `UpdateMe`/`Create`; so no two people this module ever books share a name, no rename can create a clash after booking, and this module compares no names, takes no name lock and copies no name into its schema.
  If people later allows a rename, retirement or several names per person (P1: Later), the invariant stays people's (its rule is what makes the roster unambiguous); this module would need a change only if people stopped guaranteeing one unique name per person. `ListShifts` and `GetShift` do not precompute the precondition (the person learns it on booking, with a message that says what to do).
  `UpdateShift` cannot move a shift to another event (decision 28).
- **Reminder job** (`volunteers.shift-reminder`, `Timeout 30s`): loads the sign-up; if canceled, the shift canceled or `Starts` past, a no-op.
  Otherwise reads `reminder_lead` now and computes `due = Starts - lead`: if `due > now`, the shift was moved later (or the booking was parked at the horizon) and the handler re-enqueues itself with the same `Key` at `min(due, now + ReminderHorizon)` and sends nothing; else `Notify.Send{Template: volunteers.shift-reminder, To: person, Key: "reminder:<signup>:<starts unix>", Vars: {label, department, position, starts (formatted in the zone, English day and month names in M4), zone, location}, Link: "/volunteers/shifts/<id>"}`; `Unsupported` is logged once and the job succeeds; any other `Send` error is returned (retried by kind).
  A time change inside the horizon re-enqueues with the same `Key`, which moves the row (jobs decision 3); a change beyond it parks the row at the horizon and the handler carries it forward.
  Both the carry-forward and a move rest on jobs' key rule "the latest request wins" (J1, decision 23): the handler's own `Enqueue` runs while its row is `running` and is held on the row and applied when the run ends (the row is `queued` again at the parked time, not `done`); an `UpdateShift` during the run is held the same way and supersedes the handler's request if it comes later; and an `UpdateShift` after the reminder was sent (row `done`) reopens the row, so the notify `Key` carrying `starts` gets its second chance and a shift moved after a reminder was sent is reminded once more for its new time.
  The handler never relies on the request it made: it re-reads the sign-up, the shift and `reminder_lead` on every run.
- **Expiry scan** (`volunteers.expiry-scan`, `Every 24h`, `Timeout 5m`): reads `expiry_notice_days` (0 → return); for each verified record with `expires_on` within `[today, today+N]` (today in UTC), `expiry_notified_on` null and no other valid record of the same qualification for the person, sends `volunteers.qualification-expiring` (`Key` `expiring:<record>`, Vars `{qualification, expires_on, days}`) and sets `expiry_notified_on`, each in its own small transaction so a failure loses one notice, not the batch.
- **Purge** (`volunteers.purge`, `Every 24h`, `Timeout 5m`): reads `retention` (0 → return); deletes sign-ups whose shift `ends < now - retention`, canceled shifts with `canceled_at < now - retention` and no sign-ups left, and records with `expires_on < now - retention` (with `files.DeleteByRecord`), in batches of 500; audits `volunteers.purge.ran` with counts in `Reason`.
- **Notifications** (all through `deps.Notify`, inside the transaction, `Unsupported` swallowed with one log line): `qualification-verified` (`Key` `verified:<record>`), `qualification-rejected` (`rejected:<record>`, the name only; no reason text in M4), `signup-removed` (`removed:<signup>`; Vars `{label, starts, zone, reason, is_lead, is_self, is_shift_canceled, is_department_archived, is_event_closed}` where `reason` is the `CancelReason` token and exactly one of the five booleans is true: notify's template checker allows only `term` as a function, so a template cannot compare strings; it branches on the flag (`{{if .is_department_archived}}`) and words the department and event with `{{term "department"}}` and `{{term "event"}}`).
  Vars carry nothing about anyone but the recipient.
- **Schedule** (`MySchedule`): upcoming mode lists `status = booked` rows and rows canceled with a reason other than `self` whose shift has not ended, so a person sees "removed by the lead" or "shift canceled" where the booking was until the shift passes (Q-VOL9); past mode lists `booked` rows whose shift ended, newest first. Department names come from one `Departments.NamesOf` per page.
- **Person page** (`Sections`): `Departments.NamesOf` is called in chunks of `MaxIDs` (a viewer whose extent covers more departments than one call takes still gets every name); the upcoming-shift count is one query per request.
- **Audit** (actions `volunteers.*`, in the `Write` transaction): `qualification.created|changed|archived`, `qualification-record.created|verified|rejected|deleted|read` (read: certificate opened by someone other than the person, Level personal, Fields `[certificate]`; recorded outside any transaction before returning), `position.created|changed|archived|requirements-changed`, `member.created|deleted`, `shift.created|changed|canceled|copied`, `signup.created|canceled`, `roster.read` (D43: a legal name returned on a roster entry; `Subject` the entry's person, `Fields [legal_name]`, `Level personal`; recorded outside any transaction before returning), `listing.changed`, `purge.ran`.
  `Subject` is the person concerned where one is. `Reason` is a token (`self`, `lead`, `waived`, `shift_canceled`, `department_archived`, `event_closed`, `setup`, `listed`, `unlisted`). A `Record` error fails the use case.
- **Event set-up** (`SetUp(ctx, Request{Event, CopyFrom})`): with `CopyFrom` zero returns `Items 0`; otherwise `ExtentOf(shifts.manage)` and, for each department in the extent (every department when `Site`) that has non-canceled shifts in `CopyFrom`, runs `CopyShifts`; idempotent through `copied_from` (a second run finds every source already copied).
  The caller's permission is the extent; a caller with no extent gets `Items 0` and no error (an event admin who leads nothing copies nothing; leads copy their own later).
- **Concurrency:** `*Service` is safe for concurrent use; the booking locks above (person → event-name → shift, in that order, always); `SetRequirements` and `UpdateShift` take `FOR UPDATE` on their row. **Lifecycle:** `Start` subscribes twice and registers four jobs (no I/O); `Stop` does nothing; no goroutines.
- **Read-only copy:** every read works (rosters and schedules offline-ish, 09 §12); every write fails `Unavailable` wrapping `db.ErrReadOnly`; the jobs do not run there (jobs decision 4).

## Demo seed provider

(D53; changed by D56, demo.md "Amendment D1".) `ContributesTo` gains `demoseed.Point`; `Contributions()` returns a `demoseed.Provider` (`demo.go`, package `volunteers`). It runs only in demo mode, after the core's steps, against a database with no volunteers rows, and acts through this module's own services as the demo accounts (`req.Actors.As`), so every permission, audit and validation rule applies. It may rely on `Start` having run (`Assign` enqueues the reminder job `Start` registers; the conformance rule starts the module first, D1.6). It creates, in order:

- **Actors.** The `lead` holds `department-admin` at Rangers, Gate, Greeters, Medical and DPW (demo.md D1.1; Q-DEMO5, owner, 2026-10-29: one `lead` account may run five departments, since a small burn has few volunteers), so every `lead` step holds its permission. The `steward` creates the org-wide qualifications and Medical's "First aid" (it holds `qualifications.manage` there).
- **Qualifications.** As `steward`: "ICS-100" (org-wide, no expiry) and "Over 18" (org-wide, no expiry). As `lead`: "Ranger training" (Rangers, 365-day validity). "First aid" (Medical, 730 days) by the `steward`.
- **Positions.** Dirt Ranger (Rangers, open, requires Ranger training and Over 18); Khaki (Rangers, members, requires Ranger training, ICS-100 and Over 18; the "shift lead" position); Gate Crew (Gate, open); Greeter (Greeters, open); Medic (Medical, members, requires First aid); Inventory Crew (DPW, open, no requirements; created by the lead, D1.4).
- **Members** (D1.7). The participant, the lead and walk-ons 01 to 06 in Rangers; walk-ons 08 to 11 in Medical; walk-ons 07 and 12 in neither.
- **Records** (D1.2, D1.3). Ranger training and Over 18 verified by the lead for the participant and walk-ons 01 to 06. **No records for the lead** (`RecordQualification` and `VerifyRecord` refuse `Invalid self`, and nobody else verifies at Rangers). One **pending** self-upload (a generated 1x1 PNG certificate) is the **participant's, for ICS-100**, as the last record step, with `self_upload` at its default `true`.
- **Shifts** for the event, Thursday to Sunday in `Zone`: Dirt Ranger 08-14, 14-20, 20-02 with 4 slots; Khaki 07-15 and 15-23 with 1 slot; Gate Crew 10-14, 14-18, 18-22 with 3 slots; Greeter 10-14 and 14-18 with 3 slots; Medic 08-20 and 20-08 with 2 slots. Plus one standing "Winter inventory" shift (no event) on Inventory Crew, on the day 14 days after the event's end day, 09:00 to 13:00 in the event's zone, 6 slots, location "Warehouse".
- **Event dates.** They come from `Deps.Events.InfoOf`; an event without dates fails `Invalid` (`event`, `required`), F175. The provider carries no default-date rule of its own (D1.5; the conformance rule supplies dates, module-contract).
- **Sign-ups** (D1.3, D1.7), all as `lead` except the participant's own:
  - the participant signs up for Dirt Ranger Thursday 14 to 20 and Saturday 08 to 14;
  - Friday 08 to 14 Dirt Ranger is full with walk-ons 01 to 04;
  - Gate Crew, Thursday to Sunday: walk-on 09 on 10 to 14, 10 on 14 to 18, 11 on 18 to 22;
  - Greeter, Thursday to Sunday: walk-on 12 on 10 to 14, 08 on 14 to 18;
  - the lead holds Khaki Friday 07 to 15 by `Assign` with `Waive: true` on themself (F152: a recorded, flagged self-waiver);
  - walk-on 07 on Dirt Ranger Saturday 14 to 20 with `Waive: true` (no training, not a member; the position is open);
  - Medic shifts stay unbooked (nobody at Medical has First aid verified);
  - Sunday 20 to 02 Dirt Ranger is canceled by the lead with no bookings.

Every booked person has a playa name (the core seed gives each one), which the `Conflict unnamed` rule requires. `Result.Items` counts the records created. Nothing here writes about a real person; the PNG and every name are generated constants. Failure modes F171 to F175 are in the table below.

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
| F167 | `Manifest.Validate` (via `module.Register`, `bms check`) | a term key of this module is in `CoreTerms`, duplicated, malformed, or its `en` defaults lack a form | Invalid (`terms[i].key reserved|duplicate|format`; `terms[i].defaults.en.<form> required`) | fix the manifest; the build fails | `TestManifest_Terms_DeclaresShiftPositionQualification` (pins the three keys and their four `en` forms; the negative cases are settings' `TestTermDecl_Validate_Table`) |
| F168 | `internal/app` catalogue test | a `module.volunteers.term.<key>[.<form>]` entry is missing or differs from the manifest's `en` default | test failure (not a runtime error) | fix `en.json` or the manifest | `TestCatalogue_ModuleTerms_MatchManifestDefaults` (app, generic over every module) |
| F169 | `internal/app` catalogue test | a message in `module.volunteers.*` or a template of this module names a term that is neither core nor this module's | test failure | fix the message | `TestCatalogue_TermsUsed_AreDeclared` (app, generic) |
| F170 | the shell (`Shifts`, `MySchedule`, the admin screens) | the regional overrides `shift` | shows the regional's word in every form, falls back to the module's default when the public document has none | none | `shifts.test.tsx` "renders the regional's word for shift" (applies a public document with `term_shift*` and asserts the nav entry and headings); "falls back to the module's default" (no document) |
| F171 | demo `Seed` | an actor lacks a permission for its step (starter roles or table wrong) | Permission wrapped with the step | the seed fails; fix the table | `TestDemoSeed_ActorLacksPermission_ReturnsPermission` (proves it with the lead's Gate grant removed from the fake) |
| F172 | demo `Seed` | a row fails validation | Invalid wrapped | the seed fails | `TestDemoSeed_TableValidates` |
| F173 | demo `Seed` | a booked person has no playa name | Conflict `unnamed` wrapped | the seed fails | `TestDemoSeed_EveryBookedPersonIsNamed` |
| F174 | demo `Seed` | a normal run | none | `Result.Items` > 0, the counts under "Demo seed provider" (five departments' worth of lead work, one pending record for the participant, no lead records, the Inventory Crew position, two waivers) | `TestDemoSeed_CreatesTheDescribedData`; `moduletest.Conformance` with `WithDemoSeed()` |
| F175 | demo `Seed` | the event has no dates | Invalid (`event`, `required`) | the seed fails; the core always gives dates, so this is a fake set up wrong | `TestDemoSeed_EventWithoutDates_ReturnsInvalid` |
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

Methods without failure modes beyond the common ones (`MyDepartments`, `Listed`, `ListQualifications` of active entries) read only the actor's own rows or `internal` catalogue rows, so no permission, state or input check can fail; `MySchedule` adds only the page and window validation of F149; `SetListed` takes a bool about the actor, so only the common write failures and F142 apply.
`privacy.Filter`'s own failures (`Internal` for an unchecked type) cannot occur at run time because `Manifest.Validate` runs `Check` over every `vol` response type (F141).

No new runtime failure in Go beyond F181: `EligibleOnly` adds no I/O, no permission and no state. This module's Go code reads no term, and a term that is undeclared or unset renders as its default (shell) or as its key (notify), both of which F167 to F169 make impossible for a built module. The browser tests whose expectations changed with the compact list: `shifts.test.tsx` "lists shifts with fill and Sign up" (the phrasing, the row shape), `shiftpage.test.tsx` "shows the shift" (the **Slots** line), `lead.test.tsx` "Shifts tab: fill counts" (the phrasing) and "a site-wide holder sees every department" (the heading).

## Lock-in check

No external technology is wrapped. Certificate storage, e-mail and scheduling go through `files`, `notify` and `jobs`, each with an OSI-licensed implementation (PostgreSQL chunk store, SMTP/Mailpit, PostgreSQL queue). Nothing here depends on a vendor.

## Test plan

- **Unit** (fakes: `dbtest`, `authztest`, `peopletest`, `departmentstest`, `eventstest`, `audittest`, `signalstest`, `notifytest`, `settingstest`, `jobstest`, `filestest`, fake clock): every row above; eligibility as a table (`TestEligibility_Table`: each rule on and off, expiry on the shift's date in the zone, DST edge); `Date` round trips; the reminder key and time; the `vol` validators.
- **Permission tests:** one per permission per method, including the D41 pair (department lead cannot verify another department's qualification; any lead verifies an org-wide one; a participant can never verify or self-attest: `TestVerify_Participant_ReturnsPermission`, `TestUploadCertificate_NeverCounts`).
- **Privacy:** `telemetrytest` proves no playa name, legal name or file name in logs or spans;
  `notifytest.Sent()` vars carry only the recipient's data; signals carry IDs only; the shift-mate list is proven to carry playa names only (`TestGetShift_Mates_PlayaNamesOnly`: the JSON of `ShiftSummary` has no person ID, pronoun or legal-name marker from the seed, with `peopletest` seeded with a person whose playa name is empty, F129) and the shift-mate rule as a table over audience × position flag × booked × `shifts.signup` held (`TestShiftMates_Table`); the roster's legal name as a table over audience × `people.contacts.read` extent × self (`TestRosterLegalName_Table`, with `privacytest.AssertHidden`/`AssertPresent` on the handler's body and the `audittest` entries checked for `volunteers.roster.read`);
  `Check(Manifest.Views, Groups)` passes (`TestViews_Tagged`).
- **Waiver tests** (D42): `TestSignUp_NeverWaives` (a `SignUp` body cannot carry `waive`; an unqualified self sign-up is `Conflict unqualified` whatever the lead holds) alongside F102-F103.
- **Integration** (`//go:build integration`, PostgreSQL container): migrations on empty and previous; the booking race (F99); the far reminder against the real `jobs.Runner` (F112c: `jobs.New` with the test's fake clock and a `dbtest.TempDB`, the module's `Start` registering the handler, `RunOnce` after each clock step; this is the test that catches a fake that drifts from the queue); the partial unique indexes; `files` postgres store round trip for a certificate; the purge.
- **Conformance:** `moduletest.Conformance(t, Manifest, New, provision, WithSeed(seed))` where the seed books the subject on a shift, adds a membership and a verified record with a certificate whose file name is neutral (`certificate.pdf`; a marker in the name would trip the rig's `no_personal_data_in_logs` rule through files' own log lines, and the file name is `personal` by the Data table, so it must never reach a log).
  That the certificate's name never reaches a log or span is proven separately by `TestOpenCertificate_NameNotLogged` with `telemetrytest` and a marker-named upload.
- **Demo seed:** `TestDemoSeed_*` (F171 to F175) and `moduletest.Conformance` with `WithDemoSeed()`.
- **Terms and the compact list (browser and catalogue):** F167 to F170 and F176 to F185 name their vitest files and `it` sentences, and the Go tests for `EligibleOnly` (F178 to F181).
- **UI:** `web-reviewer` at the phone viewport; smoke: sign up, see it in My volunteering, cancel; lead creates a shift, assigns, verifies a certificate.

## Later

Named so the boundary is explicit (each a change to this spec, not a new module): sign-up approval by a lead before it counts (`volunteers.approval_required` per position); swaps; check-in/check-out with hours credit as dated records (D18); walk-on desk mode; anchored shifts (`ScheduleOf`); join requests; volunteer status from recent activity; per-position shift templates; the `qualifications` capability `contracts/qualifications` (`Name = "qualifications"`;
`Holds(ctx, people []model.PersonID, key string) (map[model.PersonID]bool, error)` over a `key` token added to the catalogue, so `incidents` can ask "is this person a trained ranger" without this module's IDs); a `departments.department-page` section; pronouns next to shift-mates' playa names if the owner wants them (D42 said names only; it would be one more field in the `shift_mates` group); a `consented:<purpose>` audience for the listing if a regional wants opt-in (privacy-tags, "Later");
`events.schedule-changed`; dropping a disabled account from upcoming shifts (identity's unpublished signal); if people ever lets a playa name change or retire (P1: Later), whatever this module must do with the roster of a past event then (nothing is expected: the roster shows the name people reports at read time, which stays unique).
## Amendments

Amendments V1 to V4, the 2026-10-28 demo seed provider (D53) with its D1 changes (D56) and the 2026-10-20 visibility amendment (D44) are folded into the text above. Their narratives, the reasons for each choice, what the reviews found and the superseded wording are in `plan/design/history/module-volunteers.md`.

## Open questions

None. Every question is answered and folded into the text above; the questions, the owner's answers and the reasons are in `plan/design/history/module-volunteers.md`.

- Q-VOL1 to Q-VOL5 (closed by D42, owner, 2026-10-20): decisions 2, 3, 7, 10 and 16; the Data, Permissions and Settings tables; F102, F103 and F126 to F134.
- Q-VOL6 (closed by D43, owner, 2026-10-20): decisions 10, 17, 18 and 19.
- Q-VOL7 (owner, 2026-10-22: yes): decision 21 is withdrawn; a self-waiver is an ordinary waiver (F152).
- Q-VOL8 (owner, 2026-10-22: no): decision 27; F138 and F138a.
- Q-VOL9 (owner, 2026-10-22: yes): a canceled booking stays on the schedule until the shift ends (F153).
- Q-VOL10 and Q-VOL11 (owner, 2026-10-22): carried by people Amendment P1 (`plan/design/history/people.md`); decision 26 is reduced to `unnamed` (F161, F164).
- Q-VOL12 (owner, 2026-10-30, D57): the "Terms" section.
- Q-VOL13 (owner, 2026-10-30, D58): UI, "Shift navigation and the compact list".
- V2 rests on jobs Q-JOBS14, answered yes (D50).

This spec also depends on two specs, both now Approved (D43, D44): `jobs` (Q-JOBS1's screen is where a lead checks "did the reminder run"; Q-JOBS4 is not needed, deferred `RunAt` covers reminders; `MaxEvery` must stay ≥ 24h for the daily scans) and `files` (Q-FILES1 is indifferent to this module; Q-FILES5 stripping is right for certificates; Q-FILES6: this module exports file IDs, files exports the bytes).
If either changes, Summary "Expects of dependencies" lists what to re-check.

## Proposed changes elsewhere

1.
   (applied 2026-10-20, D44, in people.md) **`people.md`:** define an extension point `people.membership` (`contracts/membership`: `Point = "people.membership"`; `Provider{DepartmentsOf(ctx, people []model.PersonID) (map[model.PersonID][]model.DepartmentID, error)}`) so that `people.directory.read@D` and `people.contacts.read@D` extents filter to D's members (people decision 9's "later membership contract"); then seed `people.contacts.read` into `department-admin`.
   This module contributes its `member` rows. Until applied, decision 9 stands and leads with `contacts.read` reach everyone.
2. (applied 2026-10-20, D44, in module-contract.md and the skills) **`module-contract.md` and the `add-module` skill:** `Deps.Jobs jobs.Registrar` and `Deps.Files files.Store` (already proposed by `jobs.md` and `files.md`; this module is their first consumer); `moduletest.Conformance` fills nil ones with `jobstest.NewFake` and `filestest.NewFake`.
3. (applied 2026-10-20, D44, in departments-events.md) **`departments-events.md`:** none now; `ScheduleOf` and `departments.department-page` stay Later as that spec says. Note in its "What `volunteers` will need": positions are standing, so event set-up copies shifts only.
4. **`settings.md`:** let a module declare terms (`Area.Terms []string` with defaults in its catalogue) so `shift`, `position`, `qualification` and `lead` become regional words like `department`; until then this module uses its own catalogue keys. Low priority.
   (pending: needs a design choice, 2026-10-20, D44; reported to the owner.) **Superseded 2026-10-28 by Amendment V3 (Approved 2026-10-30, D57):** the design choice D52 asked for is V3.3; the concrete edits are 16 to 19 below.
5. (applied 2026-10-20, D44, in files.md) **`files.md`:** the example record kind `volunteers.qualification` becomes `volunteers.qualification-record`; Proposed change 1 there (`module.Route.MaxBodyBytes`, `Timeout`) is required by the upload and certificate routes here.
6. (applied 2026-10-20, D44, in `plan/07-abstractions.md`) **`plan/07-abstractions.md`, "Modules":** one line: "`volunteers`: qualifications and their verified records, positions, membership, shifts and sign-ups (spec `design/module-volunteers.md`)."
7. (applied; the `burn-domain` additions were left to the main session) **`plan/design/README.md`:** row 19 to Draft (done with this spec); `plan/05-roadmap.md` Phase 1: "Spec and build the first module" gains "(spec drafted 2026-10-20)".
   **`burn-domain` skill, "Volunteering and shifts":** add "Volunteers want to see who else is booked on a shift before they book it, by playa name only, because some people don't get along and would rather pick another shift; regionals differ on whether to allow it (owner, 2026-10-20; D42)" and, under "How regionals differ", "whether volunteers see who else is on a shift".
8.
   (applied 2026-10-20, D44, in people.md) (D43; restates privacy-tags Proposed change 3) **`plan/design/people.md`:** `Directory.LegalNamesOf(ctx, []model.PersonID) (map[model.PersonID]string, error)` (permission `people.contacts.read` in any extent, audited per returned person, never on a route; `peopletest.Fake.AddLegalName`); `people.contacts.read` and `people.directory.read` gain `volunteer-coordinator` in `StarterRoles`.
   Until applied, `RosterEntry.LegalName` is declared, tagged and always absent.
9. (applied 2026-10-20, D44, in authz.md) (D43; restates privacy-tags Proposed change 2) **`plan/design/authz.md`:** the `volunteer-coordinator` site starter role (`RoleVolunteerCoordinator`, migration `0003`). This module lists the key in `StarterRoles` now; `Seed` skips a key with no role and applies it at the first `bms migrate` after the role exists.
10.
    (applied 2026-10-20, D44, in `plan/05-roadmap.md`; the `burn-domain` sentence is the main session's) (D43) **`plan/05-roadmap.md`, Phase 1:** `privacy-tags` is built before this module's services (privacy-tags Proposed change 7); **`.claude/skills/burn-domain/SKILL.md`, "Who may see what":** "Playa names and who is on which shift are, by default, visible to anyone who volunteers; legal names only to a volunteer coordinator and whichever other roles the regional chooses; the regional sets both (owner, 2026-10-20; D43)" (the main session records it).
11. (V1, Q-VOL8; **dropped 2026-10-22**, owner: no) ~~**`plan/design/people.md`, `Directory.LegalNamesOf`:** the actor's own ID answered without `people.contacts.read`.~~ Legal names stay with permission holders; people is unchanged and this module removed the `self` option instead (decision 27).
12. (V1; **dropped 2026-10-22**, Q-VOL10 and Q-VOL11) ~~**`plan/design/people.md`:** export `people.NameKey` for this module's per-event comparison.~~ People Amendment P1 defines `people.NameKey` for its own unique index; this module no longer compares names and uses none of P1's names.
13.
    (V1, Q-VOL7; **applied 2026-10-23**, main session) **`.claude/skills/burn-domain/SKILL.md`:** under "Qualifications and requirements", the sentence "Volunteers can never waive a requirement for themselves" gains "a lead may waive for themself, as a recorded waiver like any other (owner, 2026-10-22)"; under "Who may see what", add "Everyone sees playa names; legal names only for the roles with the permission, never the person's own entry as an exception (owner, 2026-10-22)"; under "Volunteering and shifts", add "A person needs a playa name before they can book a shift: rosters, shift-mate lists and the radio go by it (owner, 2026-10-22, by way of the name-uniqueness rule)".
    The uniqueness, permanence, retirement and multiple-names sentences (Q-VOL10, Q-VOL11) are people P1's to propose, not this spec's.
14.
    (V1, decision 26, Q-VOL10 and Q-VOL11; **satisfied 2026-10-23 by D48**, which approved people Amendment P1, `plan/design/history/people.md`) **Dependency on `plan/design/people.md` Amendment P1:** this module relies on people making playa names unique across the installation (a unique index on `people.NameKey`, `Conflict taken` on `UpdateMe`/`Create`) and read-only once set, which is what lets `ShiftMates.Names` and `RosterEntry.PlayaName` identify a person without a clash check here; nothing in this module imports or calls P1's exported names (`people.NameKey` and the rest are people's).
    If P1 is approved in a form that does not guarantee one unique playa name per person (several names, retirement, a rename path), people's amendment must say which name modules get from `NamesOf` and that it stays unique, and this spec re-checks decision 26 then. Nothing to apply in another file; recorded here so the two amendments are approved together.

15. (V2; **satisfied 2026-10-23 by D50**, which approved jobs Amendment J1, `plan/design/history/jobs.md`) **`plan/design/history/jobs.md` Amendment J1** (drafted 2026-10-23 with this amendment): decision 3 reworded and decision 23 ("the latest request for a key wins"; `next_run_at`/`next_args`, migration `0002`; F43 reworded, F53 to F58; `jobstest.Fake.Queued()` and `RunDue`). This module's code is unchanged by V2; its tests change as V2.3 says.

16.
    (V3, Approved 2026-10-30, D57; replaces 4; applied 2026-10-30, D57, in settings.md) **`plan/design/settings.md`, an amendment "module-declared terms":** `TermDecl{Key string; Defaults map[string]Forms}` with `Validate` (V3.3.1; alias-exported to `module` as `module.TermDecl` and `module.TermForms`); `Area.Terms []TermDecl`; `Area.Validate` adds `terms[i].key duplicate|reserved` (reserved = a `CoreTerms` name);
`Options.Validate`/`New` adds `areas[j].terms[i].key duplicate` across areas (decision: one flat namespace; a word two parts need is promoted to `CoreTerms`); `CoreTerms` gains `lead` (`terms/en.json`: lead, leads, Lead, Leads); `TermsFor` returns every declared term; `Public.Terms` flattens them; `TermDoc` gains `Area string` (`"core"` or the area name);
`Terms` promises no order (the Terminology screen orders: core first, then modules alphabetically by title, then terms by key; D58); F29 `unknown` covers an undeclared module term;
F31's bound is the number of declared terms; the stored row of an undeclared term is kept and ignored (the undeclared-key rule); no migration (the `core.terminology.term` comment is left; refreshing it is a later migration's job, D58); the `/settings/terms` screen groups cards by `Area` under `module.<name>.title`.
    New failure modes there: `New` with a term key declared by two areas → Invalid (`TestNew_DuplicateTermAcrossAreas_ReturnsInvalid`); `TermDecl.Validate`/`Area.Validate` table (`TestTermDecl_Validate_Table`: format, reserved, duplicate, unknown language, `en` missing, a form `required`/`too_long {max}`/`control_chars`);
`TermsFor` and `Public` answer a module term's override, else its `en` default when the language has none (`TestTermsFor_ModuleTerm_DefaultsAndOverride`); `Terms` lists module terms with their `Area` and only declared ones (`TestTerms_ListsDeclaredModuleTermsWithArea`); a stored row of an undeclared term is ignored at load and kept (`TestLoad_UndeclaredTermRow_IgnoredAndKept`);
`SetTerms` on an undeclared module term → Invalid `unknown` (`TestSetTerms_UndeclaredModuleTerm_ReturnsInvalid`).
    The shell's `term.lead*` entries join the decision-7 equality test.
17.
    (V3, Approved D57; applied 2026-10-30, D57, in module-contract.md) **`plan/design/module-contract.md`:** `Manifest.Terms []TermDecl` (the aliases above; `Validate` runs each `TermDecl.Validate` plus the in-manifest duplicate and reserved checks under `terms[i]`); `ModuleSettings.Terms []TermDecl` so `Plan.Settings()` carries them; decision 8's derived keys gain `module.<name>.term.<key>`, `.plural`, `.title`, `.plural_title` for each declared term; the rule "a module's messages and templates use only `CoreTerms` and its own `Terms`" with the `app` test that enforces it (V3.3.6).
    `add-module` skill: one line under Settings/Terminology naming `Manifest.Terms` and the four catalogue keys.
18. (V3, Approved D57; applied 2026-10-30, D57, in web-shell.md) **`plan/design/web-shell.md`:** `termsOf` also reads `module.<name>.term.<key>[.<form>]` entries as `term_<key>[_<form>]` arguments, with a `term.<key>` entry taking precedence (the regional's word over a module's default); a test that a module term renders from the fallback with no public document and from the document once it arrives.
19. (V3, Approved D57; applied 2026-10-30, D57, in app.md) **`plan/design/app.md`:** `settingsAreas` builds an area for a module that declares settings *or* terms (today a module with no settings has no area); the catalogue test gains `TestCatalogue_ModuleTerms_MatchManifestDefaults` and `TestCatalogue_TermsUsed_AreDeclared` (V3.5 F168, F169).
20.
    (V4, Approved; applied 2026-10-30, D58, in web-shell.md "Data") **`plan/design/web-shell.md`, the browser-state table ("What the browser holds"):** one row, "Router state (`history.state`, what a `Link`'s `state` prop carries)": a module screen may pass a small object of module-internal paths and fixed tokens (this module's `{back: {to, kind}}`, V4.2), never a name, an e-mail address, free text or anything about a person; the receiving screen validates it and falls back to a default when it is missing or malformed, because history state survives a reload and can be absent on a deep link; lifetime the browser's history.
    No shell code changes; the row records the rule the first consumer sets so the next module copies it rather than inventing one.
