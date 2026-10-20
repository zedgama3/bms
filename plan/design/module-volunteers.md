# volunteers

- **Status:** Approved (2026-10-20, D43; D42's answers folded in; the D43 visibility amendment is pending)
- **Package:** `internal/modules/volunteers` (leaf `volunteers/vol`, `volunteers/postgres`)   **UI:** `web/src/modules/volunteers/`
- **Depends on specs:** `model`, `lib-errs`, `lib-core` (`clock`, `validate`), `db`, `module-contract` (`Manifest`, `Deps`, `PersonData`, `moduletest`), `module-hello` (the shape to copy), `identity` (`PersonFrom`), `authz` (`Allow`, `AllowIn`, `ExtentOf`, starter roles), `people` (`Deps.People`, `contracts/personpage`), `departments-events` (`Deps.Departments`, `Deps.Events`, `contracts/eventsetup`, the two signals), `audit`, `signals`, `notify` (`Deps.Notify`, `Manifest.Templates`), `settings` (`Deps.Settings`), `privacy-tags` (Draft: `privacy.Group`, `Filter`, `Manifest.Views`/`Groups`), `jobs` (Draft: `Deps.Jobs`), `files` (Draft: `Deps.Files`, `ReadUpload`, `Serve`, the per-route body cap), `api` (`httpx`), `web-shell`, `app` (`modules.go` line)   **Used by:** `app` (registry), `people` (person-page section, `PersonData`), `events` (event set-up provider); later `incidents`, `afterreports` and a trust feature through the signals and the capability sketched under "Later"

## Summary

**Purpose.** Staff a department's shifts with volunteers who hold what the position needs. The first feature module (D26, Phase 1). Five parts in one module: a **catalogue of qualifications** (department-specific or org-wide), **qualification records** that only leads verify (D40, D41), **positions** (roles in a department with qualification requirements and an access rule), **membership** (who belongs to a department), **shifts** (blocks of time in a position, usually for an event) with **sign-ups**. Imports `model`, `lib`, `core/module`, `core/identity` (`PersonFrom`, as the `actorFunc` seam), `core/authz` (leaf types), `core/audit` (`Ref`), `core/signals`, `core/notify`, `core/settings`, `core/privacy` (D43), `core/jobs`, `core/files`, `core/people`, `core/departments`, `core/events`, `contracts/personpage`, `contracts/eventsetup`. Never another module, `api`, `app`, `config`.

**Exported (`volunteers`).** `Manifest` (`Name: "volunteers"`); `New(ctx, deps) (module.Module, error)` (no I/O; nil `DB`, `Authz`, `People`, `Departments`, `Events`, `Audit`, `Signals`, `Settings`, `Notify`, `Jobs`, `Files` fail `Invalid`). Permission constants `PermissionSignUp = "volunteers.shifts.signup"` (site), `PermissionShiftsManage`, `PermissionRostersRead`, `PermissionPositionsManage`, `PermissionMembersManage`, `PermissionQualificationsManage`, `PermissionQualificationsRead`, `PermissionQualificationsVerify` (all `ScopeDepartment`). Setting keys `SettingReminderLead = "reminder_lead"`, `SettingCancelCutoff = "cancel_cutoff"`, `SettingExpiryNoticeDays = "expiry_notice_days"`, `SettingSelfUpload = "self_upload"`, `SettingVisibilityShiftMates = "visibility_shift_mates"`, `SettingVisibilityLegalName = "visibility_legal_name"` (the two `privacy` groups' settings, D43), `SettingRetention = "retention"`. `Manifest.Groups = vol.Groups`, `Manifest.Views` = every `vol` response type (privacy-tags). Signals `SignalSignupBooked = "volunteers.signup-booked"`, `SignalSignupCanceled = "volunteers.signup-canceled"`, `SignalQualificationVerified = "volunteers.qualification-verified"`; payload keys `signup`, `shift`, `person`, `department`, `record`, `qualification`. Jobs `JobShiftReminder = "volunteers.shift-reminder"` (deferred), `JobExpiryScan = "volunteers.expiry-scan"` (daily), `JobPurge = "volunteers.purge"` (daily). Templates `volunteers.shift-reminder`, `volunteers.qualification-expiring`, `volunteers.qualification-verified`, `volunteers.qualification-rejected`, `volunteers.signup-removed`. Section `volunteers.summary` on the person page; an `eventsetup.Provider` that copies a previous event's shift grid within the caller's extent.
**`vol` (leaf):** `PositionID`, `QualificationID`, `RecordID`, `MemberID`, `ShiftID`, `SignupID` (= `model.ID[...]`); `Date{Year, Month, Day}` (civil date, JSON `YYYY-MM-DD`); `Access` (`AccessOpen`, `AccessMembers`); `Qualification`, `Record` (+`Verified()`, `ValidOn(Date)`), `Position` (+`Requirements []QualificationID`, `HideShiftMates bool`), `Member`, `Shift` (+`Canceled()`, `Times() model.TimeRange`), `Signup` (+`WaivedBy`), `SignupStatus` (`Booked`, `Canceled`), `CancelReason` (`self`, `lead`, `shift_canceled`, `department_archived`, `event_closed`); the `*Input` and `*Query` types and the view types `ShiftSummary{Shift, Booked, Mine, Eligibility, Mates *ShiftMates}`, `ShiftMates{Names []string, Unlisted int}` (playa names only), `Eligibility{Eligible bool, Missing []QualificationID, Reason string}`, `RosterEntry{Signup; PlayaName; LegalName *string; Eligible}`, `ScheduleEntry`, `MemberView`; every type has `Validate()`; every field that reaches the wire carries a `json` and a `privacy` tag (D43), and `Groups = []privacy.Group{{Name: "shift_mates", Permission: PermissionSignUp, Default: holders, Options: nobody|booked|holders|signed_in, Extra: booked}, {Name: "legal_name", Permission: "people.contacts.read", Default: holders, Options: nobody|self|holders}}`. Constants `MaxNameBytes = 100`, `MaxDescriptionBytes = 2000`, `MaxLabelBytes = 100`, `MaxLocationBytes = 200`, `MaxSlots = 200`, `MaxShiftDuration = 48h`, `MaxValidityDays = 3650`, `MaxRequirements = 20`, `MaxList = 500`, `MaxIDs = 200`.
**Service interfaces** (all on `*Service`, safe for concurrent use, nothing to close): `Qualifications` (`ListQualifications`, `CreateQualification`, `UpdateQualification`, `ArchiveQualification`), `Records` (`RecordsOf`, `RecordQualification`, `UploadCertificate`, `VerifyRecord`, `RejectRecord`, `RemoveRecord`, `PendingRecords`, `OpenCertificate`), `Positions` (`ListPositions`, `CreatePosition`, `UpdatePosition`, `ArchivePosition`, `SetRequirements`), `Members` (`ListMembers`, `AddMember`, `RemoveMember`, `MyDepartments`, `Leave`), `Shifts` (`ListShifts`, `GetShift`, `CreateShift`, `UpdateShift`, `CancelShift`, `CopyShifts`), `Signups` (`SignUp`, `Assign`, `CancelSignup`, `Roster`, `MySchedule`, `Listed`, `SetListed`); plus `PersonData`, `Sections` (personpage), `SetUp` (eventsetup).

**Key decisions.** 1 a lead is whoever holds the department-scoped permission at that department; the starter roles seed every lead permission into `department-admin` only, never `participant`, which is how D41 ("only department leads verify") is kept as data a regional cannot loosen by accident. 2 a qualification record is verified by a lead or it does not count: a self-upload creates a `pending` record (`verified_by` null) and only `VerifyRecord` makes it count; there is no self-attestation path; self-upload is on by default (`self_upload = true`, D42). 3 a department-specific qualification is verified with `AllowIn(verify, q.Department)`; an org-wide one with `ExtentOf(verify)` not zero (any lead anywhere, D41). 4 a position's requirements are a set of qualifications; a person is eligible for a shift when every requirement has a verified record valid on the shift's start date in the shift's zone; age limits are a qualification ("Over 18", org-wide, indefinite), not a profile field or code. 5 a position's `access` is `open` (anyone eligible) or `members` (department members only); membership is a row a lead adds or a person leaves; join requests are Later. 6 a shift belongs to one position and optionally one event (`event_id` null = standing work such as winter inventory, Q-CORE10) and always carries an IANA zone (copied from the event, required otherwise). 7 bookings are serialised per person then per shift (advisory lock then `FOR UPDATE`), so slots and the no-overlap rule hold under concurrency; a person's booked shifts never overlap (`model.TimeRange.Overlaps`); a lead may `Assign` someone who lacks a requirement only with `Waive: true`, which records `waived_by`, audits `Reason waived` and flags the person on the roster; `SignUp` never waives (D42). 8 the shift reminder is a deferred job keyed by the sign-up ID, so a changed shift time moves it and the handler re-reads state; the expiry notice is a daily scan deduped by a column, not by notify's outbox. 9 the certificate is a `files` upload on the record (`Ref{Kind: "volunteers.qualification-record"}`, subject the person, level `personal`), served from this module's route after its own check; the record itself is `internal` (D42). 10 (D42, reshaped by D43) the full roster (person IDs, eligibility, waivers, links to the person page) is for `rosters.read` holders; volunteers see **who else is on a shift by playa name only** (`ShiftSummary.Mates` on `GetShift`, never a legal name, pronoun, contact or ID), as the `privacy` group `shift_mates` whose audience the regional sets in `visibility_shift_mates` (`nobody`; `booked`: only once the viewer holds a booking on that shift; `holders`: anyone who may volunteer, that is holders of `volunteers.shifts.signup`, the default per D43; `signed_in`: anyone who can see the shift), a position's `HideShiftMates` tightening it, and a person's own choice not to be listed (`SetListed(false)`): a person with no playa name yet, or unlisted, is counted in `Unlisted`, never named; these reads are `internal` and not audited. 17 (D43) "anyone who volunteers" is a holder of `volunteers.shifts.signup`, not a person with a booking: the list has to be visible before the first booking for the owner's reason to work, and a regional that restricts who may volunteer already restricts that permission. 18 (D43) legal names on the roster are the `privacy` group `legal_name` (`RosterEntry.LegalName`, `personal`, audience set in `visibility_legal_name`, default `holders` of `people.contacts.read` covering the shift's department), loaded through `Deps.People.LegalNamesOf` (people Proposed change, privacy-tags 3) only when the policy shows them, and audited twice by design: people's `people.person.read` for its data and this module's `volunteers.roster.read` for the roster; a new starter role `volunteer-coordinator` (authz, privacy-tags Proposed change 2) holds `rosters.read`, `qualifications.read` and people's two read permissions, so a fresh installation has the role the owner named. 19 (D43) the per-person opt-out stays: the owner's scenario has two sides (someone avoiding a person also has reason not to be found by them), the count still tells the viewer someone is withheld, and the lead's roster is unaffected. 11 sign-ups never carry to a new event; shifts do, through `events.event-setup` (within the caller's `shifts.manage` extent) and a lead's `CopyShifts`, idempotent through `copied_from`. 12 archiving a department or closing an event cancels its future shifts and their bookings from a signal handler (system actor, idempotent on state), telling each person once. 13 retention is one key, `volunteers.retention` (0 = forever, D30), enforced by `volunteers.purge` on ended shifts' sign-ups, canceled shifts and long-expired records (with their files); memberships are current state and are never purged. 14 no approval step on sign-up, no check-in or hours credit, no swaps, no anchored shifts, no capability yet: all Later. 15 regional differences are the seven settings (two of them visibility audiences, D43), the catalogue, the positions and the roles. 16 the org-wide catalogue is managed site-wide (`qualifications.manage` seeded into `department-steward`); a department's entries by that department's `department-admin` (D42).

**Errs kinds a caller handles.** Every use case: `Unauthenticated` (no actor), store kinds (`Unavailable`, `Timeout`, `Canceled`), `Unavailable` wrapping `db.ErrReadOnly` on writes. `Permission` (an action on a record the caller may see). `NotFound` (unknown ID, or one outside the caller's extent; never `Permission` for an invisible record). `Invalid` with field codes `required`, `too_long {max}`, `control_chars`, `range`, `malformed`, `one_of`, `too_many {max}`, `unknown`, `self`, `scope`, `hidden`. `Conflict` with codes `taken`, `archived`, `canceled`, `closed`, `full`, `overlap {shift}`, `unqualified {missing}`, `not_member`, `started`, `ended`, `cutoff`, `duplicate`, `status`, `in_use`, `pending_exists`. `Unsupported` (self-upload off; notify unconfigured is swallowed, never returned). `files`' kinds pass through `UploadCertificate` and `OpenCertificate` unchanged.

**Owns.** Schema `volunteers`: tables `qualification`, `qualification_record`, `position`, `position_requirement`, `member`, `shift`, `signup`, `unlisted`. Eight permissions, seven settings (two of them `visibility_*` from the two `privacy` groups), three signals published, two subscribed (`departments.department-archived`, `events.event-closed`), three jobs, five templates, one person-page section, one event-setup contribution. Audit actions `volunteers.<record>.<verb>` listed under Behavior. Routes under `/api/volunteers/` and screens under `/volunteers/` listed under Interface and UI. Catalogue keys `module.volunteers.*`.

**Expects of dependencies.** `jobs` (Draft): `Deps.Jobs jobs.Registrar`, `Register` in `Start`, `Enqueue` joining the transaction with `Key` reschedule semantics, `InJob`; `MaxEvery >= 24h`. `files` (Draft): `Deps.Files files.Store`, `Create` inside the caller's `Write` transaction, `ByRecord`, `Open` auditing a `personal` read, `DeleteByRecord`, `ReadUpload`, `Serve`, `module.Route.MaxBodyBytes`/`Timeout` (files Proposed change 1), `Accept` images and PDF. `authz`: `ExtentOf` for every list, `AllowIn` for one department's record. `people`: `NamesOf`, `people.directory.read` seeded to `department-admin` (leads search people to add members and record qualifications); `LegalNamesOf` (proposed; until applied the roster carries no legal name and the coordinator follows the person-page link). `privacy-tags` (Draft): `Group`, `Settings`, `Check`, `PolicyOf`, `ViewerOf`, `Filter`, `Policy.Shows`, `Manifest.Views`/`Groups` through module-contract. `authz`: the `volunteer-coordinator` starter role (proposed; `Seed` skips the key until the role exists). `departments`/`events`: `Active`, `NamesOf`, `InfoOf` (`Zone`, `Dates`, `Closed`), the two signals, `GET /api/departments` and `GET /api/events` for pickers. `notify`: scoped `Send`, `Unsupported` treated as no e-mail. `settings`: a scoped `Reader`. `audit`: a `Recorder` scoped `volunteers`. `web-shell`: `useFormat` in the shift's zone, `FileField`/`Attachments` (files Proposed change 7).

**Open questions:** Q-VOL1 to Q-VOL5 are closed by D42, Q-VOL6 by D43 (2026-10-20; decisions 17 to 19); none open. **Proposed changes elsewhere:** 9, listed at the end; none applied (8 and 9 restate privacy-tags' 2 and 3).

**D43 amendment (2026-10-20).** The regional sets visibility through `privacy` groups: `visibility_shift_mates` replaces `show_shift_mates`; `visibility_legal_name` and `RosterEntry.LegalName` are new; every `vol` response type is tagged and listed in `Manifest.Views`; F126 to F128 are reworded and F134 to F141 added (`SetListed`'s row is now F142); the `volunteer-coordinator` starter role is named in `StarterRoles`.

## Purpose

Staff a department's shifts with volunteers who hold what the position needs.

## Responsibilities

- The catalogue of qualifications a regional recognises: each department-specific or org-wide, with a name, an optional default validity, archivable.
- Qualification records: which person holds which qualification, acquired on, expires on (or never), verified by which lead and when; the certificate copy a person may upload for a lead to verify; the pending queue; expiry notices.
- Positions within a department, each with its qualification requirements and an access rule.
- Membership: who belongs to a department, added by a lead, left by the person.
- Shifts: a block of time in a position, for an event or for standing work, with slots, a location and notes; creating, changing, canceling and copying them between events.
- Sign-ups: a person booking a slot they are eligible for, a lead assigning someone (with an audited waiver when a requirement is missing), cancellation by either, the roster per shift (with legal names for the audience the regional sets), who else is on a shift by playa name for the audience the regional sets, a person's choice not to be listed, each person's own schedule, the shift reminder.
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
| Terminology for "shift", "position", "qualification" as regional words | `settings` once modules can declare terms (Proposed change 4); until then this module's catalogue keys |

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| `qualifications` | Keep the catalogue of qualifications a regional recognises. | `volunteers.qualification`; org-wide (`department_id` null) or one department's |
| `records` | Hold who holds which qualification, as verified by a lead. | `volunteers.qualification_record`; pending until verified; certificate through `files` |
| `positions` | Define the roles within a department with their requirements. | `volunteers.position`, `position_requirement` |
| `members` | Keep who belongs to a department. | `volunteers.member`; powers `members`-only positions and the person-page section |
| `shifts` | Keep the blocks of time a department needs filled. | `volunteers.shift`; event or standing work; copyable between events |
| `signups` | Book people into shifts within the rules, and show rosters and schedules. | `volunteers.signup`, `volunteers.unlisted`; the reminder job; the shift-mate list |

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
func (r Record) Verified() bool
func (r Record) ValidOn(d Date) bool // Verified() && (ExpiresOn.IsZero() || !ExpiresOn.Before(d))

type RecordInput struct { // a lead records from their own training records (D40 way (a))
	Qualification QualificationID
	Person        model.PersonID
	AcquiredOn    Date
	ExpiresOn     Date // zero = the catalogue default (AcquiredOn + ValidityDays), or indefinite
}
type UploadInput struct { // a person uploads a certificate (D40 way (b))
	Qualification QualificationID
	AcquiredOn    Date // what the certificate says; the lead may correct it
	ExpiresOn     Date
	Upload        *files.Multipart // the file; Accept = images and PDF
}
type VerifyInput struct{ AcquiredOn, ExpiresOn Date } // zero = keep what the person entered
type PendingQuery struct{ Department model.DepartmentID } // zero = every pending record the caller may verify

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
	Event        model.EventID
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
	Within          model.TimeRange    // zero = from now on
	IncludeCanceled bool               // needs shifts.manage in that extent
	Limit           int                // 1..MaxList, default 100
}
// Every vol type that reaches the wire carries json and privacy tags (privacy-tags, D43);
// the tags are shown here only where a field is grouped or above internal. Everything
// else in this module is `privacy:"internal"`, and Person IDs that the caller does not
// need are `json:"-"`.
type ShiftSummary struct {
	Shift
	Booked      int         `json:"booked" privacy:"internal"`
	Mine        SignupID    `json:"mine,omitzero" privacy:"internal"` // the caller's booked sign-up, or zero
	Eligibility Eligibility `json:"eligibility" privacy:"internal"`
	Mates       *ShiftMates `json:"mates,omitempty" privacy:"internal,group=shift_mates"` // who else is on the shift; GetShift only; absent when the policy hides it (see Behavior)
}
// ShiftMates is what a volunteer may see of the others booked on a shift: playa names only.
// Never a legal name, pronouns, a contact detail or a person ID. Cannot be invalid.
type ShiftMates struct {
	Names    []string `json:"names" privacy:"internal"`    // playa names of the other booked people, sorted, duplicates kept
	Unlisted int      `json:"unlisted" privacy:"internal"` // booked people shown by no name: no playa name yet, or chose not to be listed
}

// Groups are this module's privacy groups (privacy-tags): the regional sets each one's
// audience in visibility_<name>. GroupShiftMates guards ShiftSummary.Mates; "anyone who
// volunteers" (D43) is a holder of volunteers.shifts.signup, and "booked" is the extra
// audience the service asserts when the viewer holds a booking on that shift.
// GroupLegalName guards RosterEntry.LegalName: holders are holders of people.contacts.read
// covering the shift's department. Manifest.Settings carries privacy.Settings(Groups).
const ( GroupShiftMates = "shift_mates"; GroupLegalName = "legal_name"; AudienceBooked privacy.Audience = "booked" )
var Groups = []privacy.Group{
	{Name: GroupShiftMates, Permission: PermissionSignUp, Default: privacy.AudienceHolders,
		Options: []privacy.Audience{privacy.AudienceNobody, AudienceBooked, privacy.AudienceHolders, privacy.AudienceSignedIn},
		Extra: []privacy.Audience{AudienceBooked}},
	{Name: GroupLegalName, Permission: "people.contacts.read", Default: privacy.AudienceHolders,
		Options: []privacy.Audience{privacy.AudienceNobody, privacy.AudienceSelf, privacy.AudienceHolders}},
}
type Eligibility struct {
	Eligible bool
	Missing  []QualificationID // requirements without a valid record
	Reason   string            // "", "not_member", "full", "started", "canceled", "closed", "archived", "overlap"
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
	BookedBy   model.PersonID // zero = the person themself
	WaivedBy   model.PersonID // the lead who assigned despite a missing requirement (D42); zero otherwise
	CanceledAt time.Time
	CanceledBy model.PersonID // zero = the system (a signal handler)
	CancelReason CancelReason
	CreatedAt, UpdatedAt time.Time
}
type AssignInput struct{ Person model.PersonID; Waive bool } // Waive: book even if unqualified; recorded and audited (D42)
type RosterEntry struct { // Signup.WaivedBy non-zero flags a waiver
	Signup
	PlayaName string  `json:"playa_name" privacy:"internal"`
	LegalName *string `json:"legal_name,omitempty" privacy:"personal,group=legal_name"` // D43; absent unless the policy shows it and people knows one
	Eligible  bool    `json:"eligible" privacy:"internal"`
}
type Roster struct{ Shift Shift; Entries []RosterEntry }
type ScheduleQuery struct{ Within model.TimeRange; IncludePast bool }
type ScheduleEntry struct{ Signup; Shift Shift; Department, Position string }
```

```go
package volunteers

// Manifest declares the module; New builds the service over deps (no I/O). Fails
// errs.Invalid when a needed dependency is nil (F1); the service never degrades to
// "no permission check" (hello's TestNew_NilAuthz_DeniesEverything rule).
var Manifest = module.Manifest{ /* Name, Migrations, Templates, Permissions, Settings,
	Publishes, Subscribes, ContributesTo: personpage.Point, eventsetup.Point */ }
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
	// extent plus org-wide ones. Audits nothing (records are internal, D42). F12-F13.
	RecordsOf(ctx context.Context, person model.PersonID) ([]vol.Record, error)
	// RecordQualification is way (a): a lead records a qualification as verified now, by
	// the actor. Department-specific: qualifications.verify at that department; org-wide:
	// any verify extent (D41). Never for oneself. F14-F20.
	RecordQualification(ctx context.Context, in vol.RecordInput) (vol.Record, error)
	// UploadCertificate is way (b): the actor creates a pending record for themself with
	// the certificate stored through files (subject = actor, level personal). Refused when
	// self_upload is off (Unsupported) or a pending record for that qualification exists.
	// F21-F27.
	UploadCertificate(ctx context.Context, in vol.UploadInput) (vol.Record, error)
	// VerifyRecord marks a pending record verified by the actor, optionally correcting the
	// dates; permission as RecordQualification. Sends volunteers.qualification-verified
	// to the person. F28-F32.
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
	// the positions each is eligible for today; needs rosters.read at the department. F56-F57.
	ListMembers(ctx context.Context, dept model.DepartmentID) ([]vol.MemberView, error)
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
	// rosters.read extent. Never reveals a shift the actor may not see. F66-F67.
	ListShifts(ctx context.Context, q vol.ShiftQuery) ([]vol.ShiftSummary, error)
	// GetShift: one shift with the same visibility rule (NotFound otherwise), plus Mates
	// (who else is booked, playa names only) when the shift_mates policy shows it to this
	// viewer (privacy.Policy.Shows, then Filter); absent otherwise. Not audited (internal).
	// F68, F126-F134.
	GetShift(ctx context.Context, id vol.ShiftID) (vol.ShiftSummary, error)
	// CreateShift needs shifts.manage at the department (Active); the position must be
	// active and of that department; an event must exist and not be closed; Zone comes
	// from the event or must be a valid IANA name. F69-F76.
	CreateShift(ctx context.Context, dept model.DepartmentID, in vol.ShiftInput) (vol.Shift, error)
	// UpdateShift: same checks; a time change moves every booked person's reminder and
	// refuses when a booked person would then overlap another booking (Conflict overlap)
	// or when Slots would fall below Booked (Conflict full). F77-F80.
	UpdateShift(ctx context.Context, id vol.ShiftID, in vol.ShiftInput) (vol.Shift, error)
	// CancelShift cancels the shift and every booked sign-up (reason shift_canceled),
	// sending volunteers.signup-removed to each person. Idempotent (Conflict canceled on
	// a second call). F81-F83.
	CancelShift(ctx context.Context, id vol.ShiftID) error
	// CopyShifts copies the non-canceled shifts of one department from one event to
	// another, shifting times by the difference of the events' starts; needs
	// shifts.manage at the department; idempotent (a shift already copied from the same
	// source is skipped). Returns the number created. F84-F88.
	CopyShifts(ctx context.Context, dept model.DepartmentID, from, to model.EventID) (int, error)
}

// Signups. Consumers: http.go, the reminder job, the signal handlers, Sections.
type Signups interface {
	// SignUp books the actor into a shift: needs shifts.signup; the shift must be visible,
	// not canceled, not started, of an active department and an open event; the actor
	// must be eligible (requirements, membership), the shift not full, and no other booked
	// shift may overlap. Enqueues the reminder. Publishes volunteers.signup-booked. F89-F99.
	SignUp(ctx context.Context, shift vol.ShiftID) (vol.Signup, error)
	// Assign books another person: needs shifts.manage at the department; allowed until
	// the shift ends (walk-ons); the same rules, except that Waive (D42) records WaivedBy
	// and audits Reason waived instead of refusing unqualified. Only Assign waives;
	// SignUp never does. F100-F104.
	Assign(ctx context.Context, shift vol.ShiftID, in vol.AssignInput) (vol.Signup, error)
	// CancelSignup: the person cancels their own booking before the shift starts and
	// before cancel_cutoff; a shifts.manage holder cancels any (reason lead, the person
	// is told). Publishes volunteers.signup-canceled. F105-F109.
	CancelSignup(ctx context.Context, id vol.SignupID) error
	// Roster: the shift's booked people with person IDs, playa names, waivers and current
	// eligibility; needs rosters.read at the department. The lead's view; volunteers get
	// ShiftSummary.Mates instead (D42). LegalName is filled from People.LegalNamesOf only
	// when the legal_name policy shows it to this viewer, each entry filtered with its own
	// subject, and the read audited volunteers.roster.read (D43). F110-F111, F135-F140.
	Roster(ctx context.Context, shift vol.ShiftID) (vol.Roster, error)
	// MySchedule: the actor's bookings, upcoming by default, with shift, department and
	// position names. Cannot fail beyond store kinds and Unauthenticated.
	MySchedule(ctx context.Context, q vol.ScheduleQuery) ([]vol.ScheduleEntry, error)
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
// module.Module (Start registers two subscriptions and three jobs; never writes; Stop is
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
| `GET /people/{person}/qualifications` | `RecordsOf` | self, or `qualifications.read` extent |
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
| `GET /departments/{department}/members` | `ListMembers` | `rosters.read@D` |
| `POST /departments/{department}/members` | `AddMember` | `members.manage@D` |
| `DELETE /departments/{department}/members/{person}` | `RemoveMember` | `members.manage@D` |
| `GET /me/departments` | `MyDepartments` | self |
| `DELETE /me/departments/{department}` | `Leave` | self |
| `GET /shifts?department=&event=&position=&from=&to=&canceled=&limit=` | `ListShifts` | visibility rule |
| `GET /shifts/{shift}` | `GetShift` | visibility rule |
| `POST /departments/{department}/shifts` | `CreateShift` | `shifts.manage@D` |
| `PUT /shifts/{shift}` | `UpdateShift` | `shifts.manage@D` |
| `POST /shifts/{shift}/cancel` | `CancelShift` | `shifts.manage@D` |
| `POST /departments/{department}/shifts/copy` `{from, to}` | `CopyShifts` | `shifts.manage@D` |
| `POST /shifts/{shift}/signups` | `SignUp` | `shifts.signup` |
| `POST /shifts/{shift}/assign` `{person, waive}` | `Assign` | `shifts.manage@D` |
| `DELETE /signups/{signup}` | `CancelSignup` | self or `shifts.manage@D` |
| `GET /shifts/{shift}/roster` | `Roster` | `rosters.read@D` |
| `GET /me/schedule?from=&to=&past=` | `MySchedule` | self |
| `GET /me/listing` | `Listed` | self |
| `PUT /me/listing` `{listed}` | `SetListed` | self |

Handlers are thin (`httpx.ReadJSON`, `httpx.WriteJSON`, `httpx.Fail`); list responses are wrapped objects (`{"items": [...]}`), as the M2 build settled. `GET` routes change nothing. The OpenAPI fragment names schemas `VolunteersQualification`, `VolunteersRecord`, `VolunteersPosition`, `VolunteersMember`, `VolunteersShift`, `VolunteersShiftSummary`, `VolunteersSignup`, `VolunteersRoster`, `VolunteersScheduleEntry`.

## Types

Defined in `vol` above. Uses `model.PersonID`, `model.DepartmentID`, `model.EventID`, `model.TimeRange`, `files.FileID`, `files.Multipart`, `files.File`, `files.Content`. Every `Validate()` joins `validate.FieldError`s with the codes listed under Summary; names are trimmed and refuse control characters; a zone must load with `time.LoadLocation`.

## Data

Schema `volunteers`; migration `postgres/migrations/0001_init.sql` (version 1). Every table has `created_at`, `updated_at` (`timestamptz not null`). Foreign keys to `core.person(id)`, `core.department(id)` and `core.event(id)` are `on delete restrict` (people calls `DeletePerson` first; departments and events are never deleted).

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

**`shift`:** `id`; `department_id`, `position_id` FK; `event_id uuid null` FK event; `label` (1..100); `starts`, `ends timestamptz` (check `ends > starts and ends - starts <= interval '48 hours'`); `zone text`; `slots int` (1..200); `location` (≤ 200); `notes` (≤ 2000; internal; the form says "no personal information here"); `canceled_at null`; `cancel_reason text null` (check in the five reasons); `copied_from uuid null` FK `shift` set null; `created_by uuid null` FK person. Indexes `(department_id, starts)`, `(event_id, starts)`, `(position_id)`, unique `(copied_from, event_id) where copied_from is not null` (the idempotent copy). Nothing personal; `created_by` is internal; canceled rows purged by `volunteers.retention`.

**`signup`:** `id`; `shift_id` FK; `person_id` FK (internal: shift times with a person, per `privacy-data`'s table); `status text check in ('booked','canceled')`; `booked_by uuid null`, `waived_by uuid null`, `canceled_by uuid null` FK person (internal); `canceled_at null`; `cancel_reason null`. Partial unique `(shift_id, person_id) where status = 'booked'` (`signup_booked_key`); index `(person_id, status)`, `(shift_id, status)`. Kept until `DeletePerson` or `volunteers.retention` after the shift ends (history and, later, credit).

**`unlisted`:** `person_id uuid pk` FK person restrict; `created_at`. One row per person who chose not to appear in shift-mate lists (`SetListed(false)`); no row = listed. The row itself is `internal` and read only by the person (`Listed`) and by the list computation; nobody is told who opted out, only how many booked people are unlisted. Kept until `SetListed(true)` (the row is deleted) or `DeletePerson`; never purged by retention (it is current state, like `member`).

**Operations and their permission:** every write names its permission in Interface; `ListShifts`, `GetShift` and the roster filter with `ExtentOf` (`shifts.manage`, `rosters.read`) and the membership join; `GetShift`'s `Mates` needs no permission beyond seeing the shift: it is gated by the `shift_mates` policy (`visibility_shift_mates`: `holders` of `shifts.signup` by default, `booked` asserted from the actor's own booking) and the position's `hide_shift_mates`; `Roster`'s `LegalName` is gated by the `legal_name` policy (`holders` of `people.contacts.read` covering the department by default) and by people's own check inside `LegalNamesOf`; the system paths used by the jobs, the signal handlers, `ExportPerson` and `DeletePerson` take no actor and are reachable only from those callers (never a route).

**Export** (`ExportPerson`, one JSON document): `memberships` (department ID and name, since), `listed` (`true`/`false`), `signups` (shift label, department, position, times, zone, status, reasons, whether a requirement was waived, who booked or canceled as role words, never names), `qualification_records` (qualification name, dates, verified yes/no and when, certificate file ID: files exports the bytes itself, files decision 13), `verified_for_others` (a count), `shifts_created` (a count). **Deletion** (`DeletePerson`, inside people's transaction, the ctx given): delete the person's sign-ups, memberships, `unlisted` row and records (calling `files.DeleteByRecord` per record), then set `verified_by`, `added_by`, `booked_by`, `waived_by`, `canceled_by`, `created_by` to null where they name the person (`Anonymized`); `Kept` empty. Idempotent.

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

**Subscribed** (handlers registered in `Start`, run as the system, idempotent on state, quick):

| Signal | Handler | On failure |
|---|---|---|
| `departments.department-archived` `{department}` | cancel every non-canceled shift of the department whose `ends` is in the future (`cancel_reason department_archived`) and its booked sign-ups (same reason), sending `volunteers.signup-removed` once per person per shift (notify `Key` `removed:<signup>`); a second delivery finds nothing to do | logged and counted by `signals`; retried by `jobs` for `Unavailable`/`Timeout`; the archive stands and `SignUp` refuses anyway (`Active` false), so a lost delivery leaves stale rows a lead can cancel by hand |
| `events.event-closed` `{event}` | the same for the event's future shifts (`event_closed`) | same; `SignUp` refuses anyway (`Closed`) |

Not subscribed: `events.event-changed` (the shift screen shows "outside the event's dates" from `InfoOf` at render time); `people.person-deleted` (`DeletePerson` already ran); `departments.department-restored` (a lead recreates shifts).

## Settings

| Key | Type | Default | Valid range | Who can change it |
|---|---|---|---|---|
| `volunteers.reminder_lead` | duration | `24h` | `0` (no reminders) or `1h`..`168h` | settings admin |
| `volunteers.cancel_cutoff` | duration | `0` (cancel until the shift starts) | `0`..`168h` | settings admin |
| `volunteers.expiry_notice_days` | int | `30` | `0` (no notice)..`365` | settings admin |
| `volunteers.self_upload` | bool | `true` (D42) | | settings admin |
| `volunteers.visibility_shift_mates` (from `privacy.Settings(Groups)`) | enum | `holders` (D43; see below) | `nobody` (only roster readers see who is booked), `booked` (a person sees the others once they hold a booking on that shift), `holders` (anyone who may volunteer: holders of `shifts.signup`, before booking), `signed_in` (anyone who can see the shift); `one_of` | settings admin |
| `volunteers.visibility_legal_name` (from `privacy.Settings(Groups)`) | enum | `holders` (D43) | `nobody` (the roster never carries legal names), `self` (a person sees their own on a roster, nobody else's), `holders` (holders of `people.contacts.read` covering the shift's department); `one_of` | settings admin |
| `volunteers.retention` | duration | `0` (forever, D30) | `0` or `8760h`..`876000h` (as `audit.retention`) | settings admin |

**Why `holders` is the default for `shift_mates`.** D43: playa names and who is on which shift are visible to anyone who volunteers. "Anyone who volunteers" is a holder of `shifts.signup` (decision 17), which the base role holds, so on a fresh installation every signed-in person sees the list before booking, which is what the owner's reason needs (people who don't get along pick another shift, D42). The cost is bounded: only playa names, only to people who can already see that the shift exists and how full it is, and any person can take themself out of the list. A regional that prefers "see them once you've committed" sets `booked`; one that wants rosters kept with the department sets `nobody`; `signed_in` matters only to a regional that took `shifts.signup` away from the base role and still wants browsers to see the names. A department with a discreet team (peer support, for example, whose volunteers' identities the manuals treat with care) sets `HideShiftMates` on that position instead of asking the regional to turn it off for everyone; the position flag can only hide, never show what the setting hides.

**Why `holders` is the default for `legal_name`.** D43: real names are for the volunteer coordinator and maybe some other roles. `holders` of `people.contacts.read` is exactly that set, and the regional widens or narrows it by editing roles, not this setting; `self` and `nobody` are there for a regional that wants legal names kept to the person page or out of rosters altogether.

Read at use-case or job time through `deps.Settings`, never cached. Catalogue keys `module.volunteers.setting.<key>.title|description`. A `Check` refuses an out-of-range value with `range`.

## UI

All screens phone-first (web-shell decision 21), `useT` with typed keys, `{term_department}`, `{term_event}`, `{term_participant}` for the core terms, times rendered with `useFormat` in the shift's `zone`, `ErrorState`/`ErrorSummary` for errors, no `style` prop. Routes relative to `/volunteers/`; nav: main **Shifts** (`/volunteers/`, signed-in), **My volunteering** (`/volunteers/me`, signed-in); admin **Volunteers** (`/volunteers/admin`, access `[shifts.manage, positions.manage, members.manage, qualifications.manage, qualifications.verify, rosters.read]`).

| Screen | Who | Actions | Empty / error / forbidden |
|---|---|---|---|
| `/volunteers/` Shifts | every signed-in person | filter by event, department, day; each card shows label, department, position, times, free slots, and **Sign up**, **Booked** or why not (`Eligibility`: missing qualifications named, members only, full, overlap) | "No shifts to show yet" with the filters; a 403 on sign-up shows the shell's forbidden state |
| `/volunteers/shifts/:shift` | as visible | sign up, cancel my booking; **Who else is on this shift** (when `mates` is present: the playa names, then "and {n} not listed" when `unlisted` > 0; plain text, no links; the section is absent when `mates` is null, with no hint why); roster (roster readers) with playa names linking to `/people/:person`, a legal name next to the playa name when `legal_name` is present on the entry (no column at all when it is absent on every entry), an eligibility flag and a **requirement waived** flag; **Assign** (playa-name search through `GET /api/people/search`, a **Waive requirements** switch that explains the waiver is recorded), edit, cancel (managers) | NotFound state for an invisible shift; canceled banner; "No one else is booked yet" when `mates` is present and empty |
| `/volunteers/me` My volunteering | self | upcoming and past bookings with cancel; my departments with leave; my qualifications with status (pending, verified, expires on, expired) and **Upload a certificate** (`FileField`, when `self_upload` is on; hidden otherwise); **Show my playa name to others on my shifts** (a switch from `GET /me/listing`, saved with `PUT /me/listing`, with the note that leads always see the roster) | "You have no shifts booked" with a link to Shifts |
| `/volunteers/admin` | holders of any lead permission | the departments the viewer leads (from `session.permissions` `p@D` and `GET /api/departments`), pending certificates count | "You lead no department yet" |
| `/volunteers/departments/:department` | leads | tabs: **Shifts** (by event, fill counts, new, copy from a previous event), **Positions** (list, new, edit with a **Hide who's on shifts from other volunteers** switch, requirements picker, archive), **Members** (list with eligibility per position, add by search, remove), **Qualifications** (the department's catalogue; org-wide ones read-only here), **Pending** (certificates to verify: open the file, correct dates, verify or reject) | each tab's empty state names the action; forbidden tabs are hidden by `session.permissions` and answer the shell's forbidden state if reached |
| `/volunteers/departments/:department/shifts/new`, `/volunteers/shifts/:shift/edit` | `shifts.manage@D` | the form: position, event (or standing work with a zone), label, start and end in the zone, slots, location, notes ("no personal information") | field errors inline (`module.volunteers.field.<code>`) |
| `/volunteers/people/:person` | `qualifications.read`/`.verify` holders | a person's records in the viewer's extent; **Record a qualification** (way (a)); open certificate; remove | NotFound outside the extent |
| `/volunteers/qualifications` | site-wide `qualifications.manage` | the org-wide catalogue: new, edit, archive | |

The person page shows section `volunteers.summary` (title `module.volunteers.section.summary.title`): items **Departments** (names, to the viewer's extent), **Upcoming shifts** (count, `Href` `/volunteers/me` for the person themself).

## Behavior

- **Actor and checks, in order:** actor (`identity.PersonFrom`; none → `Unauthenticated`), input `Validate`, permission, then state. A record outside the caller's extent is `NotFound` (privacy-data); an action the caller may not take on a visible record is `Permission`.
- **Eligibility** (`eligible(person, shift, now)`): department `Active`; shift not canceled; event (if any) not `Closed`; position not archived; if `Access == members`, a `member` row exists; every requirement has a `Record.ValidOn(DateOf(shift.Starts, shift.Zone))`; `Booked < Slots`; no booked sign-up of the person whose shift `Times().Overlaps(shift.Times())`. `ListShifts` computes it for the actor with the actor's records and bookings loaded once.
- **Visibility plumbing** (D43, every use case that returns a `vol` response type): after the permission check and the load, `privacy.ViewerOf(ctx, authz, Groups)` and `privacy.PolicyOf(ctx, settings, Groups)` once, then `privacy.Filter` per record with `Relation{Subject: the entry's person (zero for a shift or position), Department: the record's department, Also: [booked] when the viewer holds a booked sign-up on that shift}`; the audit entries the `Result`s call for are recorded in one `Record` before returning. Types with no grouped field (`Qualification`, `Position`, `Shift`, `Member`, `ScheduleEntry`, ...) still pass through `Filter` (it is cheap and keeps the rule uniform: a later grouped field needs no new call site).
- **Shift-mates** (`GetShift`, D42, D43): after the visibility rule passes, `Mates` is loaded when the position's `hide_shift_mates` is false and `pol.Shows(GroupShiftMates, viewer, rel)` is true (`visibility_shift_mates` read now, never cached; `rel.Also` carries `booked` when the actor holds a `booked` sign-up on this shift); otherwise `Mates` stays nil, `Filter` would zero it anyway, and the response says nothing about why. When loaded: the shift's `booked` sign-ups other than the actor's, joined with `unlisted`; for the listed ones `Deps.People.NamesOf` (one call, at most `MaxSlots` IDs); a person whose playa name is `""` or who is missing from the map (no profile yet, deleted) is counted in `Unlisted` with the opted-out, never shown by any other field (the module has no access to a legal name and asks for none). `Names` is sorted and may repeat (two burners may share a playa name, people decision 3; pronouns are not shown because the owner said names only). `ListShifts` never fills `Mates` (one `NamesOf` per shift would not scale on a list and the card shows the fill count). Canceled shifts show it too (what was booked is not secret from someone who could see the shift). Not audited: everything shown is `internal`, and the per-read audit a `personal` field would get would log every volunteer's every glance at a shift page without protecting anything (the same reasoning as the qualification record, decision 9).
- **Roster legal names** (`Roster`, D43): after `rosters.read@D` passes and the entries are loaded, the service asks `pol.Shows(GroupLegalName, viewer, Relation{Subject: entry.Person, Department: shift.Department})` per entry; when any entry shows, one `Deps.People.LegalNamesOf` call with those persons (people checks `people.contacts.read` for the actor and audits its own `people.person.read` per person; a `Permission` from people, which happens only if the group's permission and people's rule disagree, is answered by leaving every `LegalName` nil, never by failing the roster, and is logged once); a person people omits stays nil. Then `Filter` per entry; the `Result`s with `legal_name` become one `volunteers.roster.read` entry per subject (`Fields [legal_name]`, `Level personal`, `Record` the shift), recorded in one call. Under `nobody`, or when `Deps.People` has no `LegalNamesOf` yet, people is never asked.
- **Listing choice** (`SetListed`): one `Write` transaction: `false` inserts the `unlisted` row (`ON CONFLICT DO NOTHING`), `true` deletes it; audits `volunteers.listing.changed` (`Subject` the actor, `Reason` `listed` or `unlisted`) only when a row changed, so a repeat is a no-op. Takes effect on the next `GetShift`; nothing is recomputed or notified. The choice is one per person, not per shift: someone avoiding another person wants to be absent from every list, and a per-shift switch would make them choose each time on a phone.
- **Booking** (`SignUp`, `Assign`): one `Write` transaction: `pg_advisory_xact_lock(hashtext(person_id))`, then `SELECT ... FROM volunteers.shift WHERE id = $1 FOR UPDATE`, re-check eligibility, insert, audit `volunteers.signup.created` (Fields `[person, shift]`, `Reason` `self`, `lead` or `waived`; `waived` means `Assign` with `Waive: true` booked someone eligibility refused for a missing requirement, D42; `Waive: true` on an eligible person records nothing special and `WaivedBy` stays zero), publish `signup-booked`, and when `reminder_lead > 0` and `Starts - lead > now`, `Jobs.Enqueue{Name: volunteers.shift-reminder, Key: <signup id>, Args: {signup}, RunAt: Starts - lead}`. The partial unique index is the backstop for a duplicate (`Conflict duplicate`).
- **Reminder job** (`volunteers.shift-reminder`, `Timeout 30s`): loads the sign-up; if `Booked`, the shift not canceled and `Starts` in the future, `Notify.Send{Template: volunteers.shift-reminder, To: person, Key: "reminder:<signup>:<starts unix>", Vars: {label, department, position, starts (formatted in the zone, English day and month names in M4), zone, location}, Link: "/volunteers/shifts/<id>"}`; `Unsupported` is logged once and the job succeeds; any other `Send` error is returned (retried by kind). Otherwise a no-op. A time change re-enqueues with the same `Key`, which moves the row (jobs decision 3).
- **Expiry scan** (`volunteers.expiry-scan`, `Every 24h`, `Timeout 5m`): reads `expiry_notice_days` (0 → return); for each verified record with `expires_on` within `[today, today+N]` (today in UTC), `expiry_notified_on` null and no other valid record of the same qualification for the person, sends `volunteers.qualification-expiring` (`Key` `expiring:<record>`, Vars `{qualification, expires_on, days}`) and sets `expiry_notified_on`, each in its own small transaction so a failure loses one notice, not the batch.
- **Purge** (`volunteers.purge`, `Every 24h`, `Timeout 5m`): reads `retention` (0 → return); deletes sign-ups whose shift `ends < now - retention`, canceled shifts with `canceled_at < now - retention` and no sign-ups left, and records with `expires_on < now - retention` (with `files.DeleteByRecord`), in batches of 500; audits `volunteers.purge.ran` with counts in `Reason`.
- **Notifications** (all through `deps.Notify`, inside the transaction, `Unsupported` swallowed with one log line): `qualification-verified` (`Key` `verified:<record>`), `qualification-rejected` (`rejected:<record>`, the name only; no reason text in M4), `signup-removed` (`removed:<signup>`; Vars `{label, starts, zone, reason}` where reason is a token the template words). Vars carry nothing about anyone but the recipient.
- **Audit** (actions `volunteers.*`, in the `Write` transaction): `qualification.created|changed|archived`, `qualification-record.created|verified|rejected|deleted|read` (read: certificate opened by someone other than the person, Level personal, Fields `[certificate]`; recorded outside any transaction before returning), `position.created|changed|archived|requirements-changed`, `member.created|deleted`, `shift.created|changed|canceled|copied`, `signup.created|canceled`, `roster.read` (D43: a legal name returned on a roster entry; `Subject` the entry's person, `Fields [legal_name]`, `Level personal`; recorded outside any transaction before returning), `listing.changed`, `purge.ran`. `Subject` is the person concerned where one is. `Reason` is a token (`self`, `lead`, `waived`, `shift_canceled`, `department_archived`, `event_closed`, `setup`, `listed`, `unlisted`). A `Record` error fails the use case.
- **Event set-up** (`SetUp(ctx, Request{Event, CopyFrom})`): with `CopyFrom` zero returns `Items 0`; otherwise `ExtentOf(shifts.manage)` and, for each department in the extent (every department when `Site`) that has non-canceled shifts in `CopyFrom`, runs `CopyShifts`; idempotent through `copied_from` (a second run finds every source already copied). The caller's permission is the extent; a caller with no extent gets `Items 0` and no error (an event admin who leads nothing copies nothing; leads copy their own later).
- **Concurrency:** `*Service` is safe for concurrent use; the booking locks above; `SetRequirements` and `UpdateShift` take `FOR UPDATE` on their row. **Lifecycle:** `Start` subscribes twice and registers three jobs (no I/O); `Stop` does nothing; no goroutines.
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
| F10 | `ArchiveQualification` | not permitted / unknown | Permission / NotFound | | `TestArchiveQualification_NotLead_ReturnsPermission` |
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
| F26 | `UploadCertificate` | files' `Create` fails (quota, blob down) | Conflict `quota` / Unavailable | show; rolled back | `TestUploadCertificate_FilesFails_RollsBack` |
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
| F49 | `UpdatePosition` / `ArchivePosition` | unknown or outside extent | NotFound | | `TestUpdatePosition_Unknown_ReturnsNotFound` |
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
| F71 | `CreateShift` | position of another department, archived or unknown | Invalid `scope` / Conflict `archived` / Invalid `unknown` | | `TestCreateShift_Position_Table` |
| F72 | `CreateShift` | event unknown | Invalid `unknown` | | `TestCreateShift_UnknownEvent_ReturnsInvalid` |
| F73 | `CreateShift` | event closed | Conflict `closed` | | `TestCreateShift_ClosedEvent_ReturnsConflict` |
| F74 | `CreateShift` | `Ends <= Starts`, longer than 48h, slots out of range, label invalid | Invalid `range`/`too_long`/`required` | | `TestCreateShift_InvalidInput_Table` |
| F75 | `CreateShift` | no event and zone missing or not IANA | Invalid `required`/`malformed` | | `TestCreateShift_StandingWorkBadZone_ReturnsInvalid` |
| F76 | `CreateShift` | `Events.InfoOf` fails | its kind | | `TestCreateShift_EventsFail_ReturnsError` |
| F77 | `UpdateShift` | unknown / outside extent | NotFound | | `TestUpdateShift_Unknown_ReturnsNotFound` |
| F78 | `UpdateShift` | canceled | Conflict `canceled` | | `TestUpdateShift_Canceled_ReturnsConflict` |
| F79 | `UpdateShift` | slots below booked | Conflict `full` | | `TestUpdateShift_SlotsBelowBooked_ReturnsConflict` |
| F80 | `UpdateShift` | new time overlaps a booked person's other shift | Conflict `overlap {shift}` | | `TestUpdateShift_CreatesOverlap_ReturnsConflict` |
| F81 | `CancelShift` | no permission / unknown | Permission / NotFound | | `TestCancelShift_NotLead_ReturnsPermission` |
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
| F100 | `Assign` | no `shifts.manage@D` | Permission | | `TestAssign_NotLead_ReturnsPermission` |
| F101 | `Assign` | shift ended | Conflict `ended` | | `TestAssign_Ended_ReturnsConflict` |
| F102 | `Assign` | unqualified without `Waive` | Conflict `unqualified` | | `TestAssign_UnqualifiedNoWaive_ReturnsConflict` |
| F103 | `Assign` | unqualified with `Waive` | nil, `WaivedBy` set, audited `waived` | | `TestAssign_Waive_RecordsWaiver` |
| F104 | `Assign` | `Jobs.Enqueue` fails | its kind; rolled back | | `TestAssign_EnqueueFails_RollsBack` |
| F105 | `CancelSignup` | another's, no `shifts.manage@D` | NotFound | | `TestCancelSignup_OtherNoPermission_ReturnsNotFound` |
| F106 | `CancelSignup` | own, after the shift started | Conflict `started` | | `TestCancelSignup_Started_ReturnsConflict` |
| F107 | `CancelSignup` | own, inside `cancel_cutoff` | Conflict `cutoff` | contact the lead | `TestCancelSignup_InsideCutoff_ReturnsConflict` |
| F108 | `CancelSignup` | already canceled | Conflict `status` | | `TestCancelSignup_Twice_ReturnsConflict` |
| F109 | `CancelSignup` | lead cancels; notify `Unsupported` | nil | | `TestCancelSignup_ByLead_NotifiesOrLogs` |
| F110 | `Roster` | no `rosters.read@D` | Permission | | `TestRoster_NoPermission_ReturnsPermission` |
| F111 | `Roster` | unknown shift | NotFound | | `TestRoster_Unknown_ReturnsNotFound` |
| F112 | reminder handler | sign-up canceled, shift canceled or started | nil, nothing sent | | `TestReminder_StaleSignup_SendsNothing` |
| F113 | reminder handler | `Send` fails `Unavailable` | Unavailable (jobs retries) | | `TestReminder_NotifyUnavailable_ReturnsUnavailable` |
| F114 | reminder handler | payload lacks `signup` | Invalid (fails, no retry) | | `TestReminder_BadArgs_ReturnsInvalid` |
| F115 | expiry scan | one `Send` fails | continues; returns the first error after the batch | | `TestExpiryScan_OneFailure_ContinuesAndReports` |
| F116 | expiry scan | run twice in a day | second sends nothing (`expiry_notified_on`) | | `TestExpiryScan_Idempotent` |
| F117 | signal handlers | second delivery | no-op | | `TestDepartmentArchived_Idempotent`, `TestEventClosed_Idempotent` |
| F118 | signal handlers | payload lacks the key | Invalid | | `TestDepartmentArchived_BadPayload_ReturnsInvalid` |
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
| F138 | `Roster` | `self`: the viewer is on the roster | their own entry carries `legal_name`, the others do not; no audit (self) | | `TestRoster_LegalNameSelf_OwnEntryOnly` |
| F139 | `Roster` | `LegalNamesOf` fails `Permission` / a store kind | every `legal_name` nil, logged once / that kind | show / show error | `TestRoster_PeoplePermission_HidesQuietly`, `TestRoster_PeopleUnavailable_ReturnsError` |
| F140 | `Roster` | the `roster.read` audit `Record` fails | its kind; no roster returned | show error | `TestRoster_AuditFails_ReturnsError` |
| F141 | `Register` / `New` | a `vol` response type with an untagged wire field, or `Manifest.Settings` lacking a `visibility_*` decl | Invalid from `Manifest.Validate` (`bms check`, every test) | fix the type | `TestManifest_Valid` and `moduletest` `views_tagged` |
| F129 | `GetShift` | a booked person has no playa name, or is missing from `NamesOf` | counted in `Unlisted`; no name of any kind | show the count | `TestGetShift_NoPlayaName_CountsUnlisted_NeverLegalName` |
| F130 | `GetShift` | a booked person is in `unlisted` | counted in `Unlisted` | show the count | `TestGetShift_OptedOut_CountsUnlisted` |
| F131 | `GetShift` | the actor is booked | the actor is not in `Names` or `Unlisted`; canceled sign-ups are absent | | `TestGetShift_Mates_ExcludesActorAndCanceled` |
| F132 | `GetShift` | `People.NamesOf` fails | its kind; nothing returned | show error | `TestGetShift_NamesFail_ReturnsError` |
| F133 | `ListShifts` | any mode | `mates` absent on every summary | | `TestListShifts_NeverFillsMates` |
| F142 | `SetListed` | same value twice | nil; no second audit entry | | `TestSetListed_Idempotent` |

Methods without failure modes beyond the common ones (`MyDepartments`, `MySchedule`, `Listed`, `ListQualifications` of active entries) read only the actor's own rows or `internal` catalogue rows, so no permission, state or input check can fail; `SetListed` takes a bool about the actor, so only the common write failures and F142 apply. `privacy.Filter`'s own failures (`Internal` for an unchecked type) cannot occur at run time because `Manifest.Validate` runs `Check` over every `vol` response type (F141).

## Lock-in check

No external technology is wrapped. Certificate storage, e-mail and scheduling go through `files`, `notify` and `jobs`, each with an OSI-licensed implementation (PostgreSQL chunk store, SMTP/Mailpit, PostgreSQL queue). Nothing here depends on a vendor.

## Test plan

- **Unit** (fakes: `dbtest`, `authztest`, `peopletest`, `departmentstest`, `eventstest`, `audittest`, `signalstest`, `notifytest`, `settingstest`, `jobstest`, `filestest`, fake clock): every row above; eligibility as a table (`TestEligibility_Table`: each rule on and off, expiry on the shift's date in the zone, DST edge); `Date` round trips; the reminder key and time; the `vol` validators.
- **Permission tests:** one per permission per method, including the D41 pair (department lead cannot verify another department's qualification; any lead verifies an org-wide one; a participant can never verify or self-attest: `TestVerify_Participant_ReturnsPermission`, `TestUploadCertificate_NeverCounts`).
- **Privacy:** `telemetrytest` proves no playa name, legal name or file name in logs or spans; `notifytest.Sent()` vars carry only the recipient's data; signals carry IDs only; the shift-mate list is proven to carry playa names only (`TestGetShift_Mates_PlayaNamesOnly`: the JSON of `ShiftSummary` has no person ID, pronoun or legal-name marker from the seed, with `peopletest` seeded with a person whose playa name is empty, F129) and the shift-mate rule as a table over audience × position flag × booked × `shifts.signup` held (`TestShiftMates_Table`); the roster's legal name as a table over audience × `people.contacts.read` extent × self (`TestRosterLegalName_Table`, with `privacytest.AssertHidden`/`AssertPresent` on the handler's body and the `audittest` entries checked for `volunteers.roster.read`); `Check(Manifest.Views, Groups)` passes (`TestViews_Tagged`).
- **Waiver tests** (D42): `TestSignUp_NeverWaives` (a `SignUp` body cannot carry `waive`; an unqualified self sign-up is `Conflict unqualified` whatever the lead holds) alongside F102-F103.
- **Integration** (`//go:build integration`, PostgreSQL container): migrations on empty and previous; the booking race (F99); the partial unique indexes; `files` postgres store round trip for a certificate; the purge.
- **Conformance:** `moduletest.Conformance(t, Manifest, New, provision, WithSeed(seed))` where the seed books the subject on a shift, adds a membership and a verified record with a certificate carrying the markers in its name.
- **UI:** `web-reviewer` at the phone viewport; smoke: sign up, see it in My volunteering, cancel; lead creates a shift, assigns, verifies a certificate.

## Later

Named so the boundary is explicit (each a change to this spec, not a new module): sign-up approval by a lead before it counts (`volunteers.approval_required` per position); swaps; check-in/check-out with hours credit as dated records (D18); walk-on desk mode; anchored shifts (`ScheduleOf`); join requests; volunteer status from recent activity; per-position shift templates; the `qualifications` capability `contracts/qualifications` (`Name = "qualifications"`; `Holds(ctx, people []model.PersonID, key string) (map[model.PersonID]bool, error)` over a `key` token added to the catalogue, so `incidents` can ask "is this person a trained ranger" without this module's IDs); a `departments.department-page` section; pronouns next to shift-mates' playa names if the owner wants them (D42 said names only; it would be one more field in the `shift_mates` group); a `consented:<purpose>` audience for the listing if a regional wants opt-in (privacy-tags, "Later"); `events.schedule-changed`; dropping a disabled account from upcoming shifts (identity's unpublished signal).

## Open questions

**Closed by D42 (owner, 2026-10-20):**

- **Q-VOL1. May a lead assign someone who lacks a requirement?** Yes, leads may override: `Assign` with `Waive: true` records `waived_by`, audits `Reason waived` and the roster flags the person; `SignUp` never waives (decision 7, F102-F103, `TestSignUp_NeverWaives`).
- **Q-VOL2. Sensitivity of a qualification record.** `internal`; the certificate file stays `personal` (decision 9; the Data table).
- **Q-VOL3. Who manages the org-wide catalogue?** Site-wide `qualifications.manage`, seeded into `department-steward`; a department's entries by its `department-admin` (decision 16; the Permissions table).
- **Q-VOL4. Is self-upload on by default?** Yes, `volunteers.self_upload = true` (decision 2; the Settings table).
- **Q-VOL5. May volunteers see who else is on a shift?** Yes, by playa name only, configurable, because some people don't get along and want to pick another shift. This reversed the draft's recommendation; the design is decision 10 (as reshaped by D43: the `shift_mates` privacy group and `visibility_shift_mates`, justified under Settings), `Position.HideShiftMates`, `SetListed`, the `unlisted` table, F126-F134.

**Closed by D43 (owner, 2026-10-20):**

- **Q-VOL6. Is the shift-mate listing opt-out or opt-in?** The owner answered on a different axis: the regional sets visibility, with these defaults: playa names and who is on which shift visible to anyone who volunteers; legal names only to a volunteer coordinator and maybe some other roles; fields tagged so permissions can be enforced and changed later. Folded in as decisions 10 (reshaped), 17, 18 and 19: the `privacy` groups `shift_mates` (default `holders` of `shifts.signup`) and `legal_name` (default `holders` of `people.contacts.read`), the `volunteer-coordinator` starter role, and the per-person opt-out kept (the owner's scenario has two sides; the count shows someone is withheld; the roster is unaffected). The consent alternative is not needed: a regional that wants opt-in sets `visibility_shift_mates` to `nobody` until the Later `consented:<purpose>` audience exists (privacy-tags, "Later").

**Open:** none.

This spec also depends on two Draft specs as written: `jobs` (Q-JOBS1's screen is where a lead checks "did the reminder run"; Q-JOBS4 is not needed, deferred `RunAt` covers reminders; `MaxEvery` must stay ≥ 24h for the daily scans) and `files` (Q-FILES1 is indifferent to this module; Q-FILES5 stripping is right for certificates; Q-FILES6: this module exports file IDs, files exports the bytes). If either changes, Summary "Expects of dependencies" lists what to re-check.

## Proposed changes elsewhere

1. **`people.md`:** define an extension point `people.membership` (`contracts/membership`: `Point = "people.membership"`; `Provider{DepartmentsOf(ctx, people []model.PersonID) (map[model.PersonID][]model.DepartmentID, error)}`) so that `people.directory.read@D` and `people.contacts.read@D` extents filter to D's members (people decision 9's "later membership contract"); then seed `people.contacts.read` into `department-admin`. This module contributes its `member` rows. Until applied, decision 9 stands and leads with `contacts.read` reach everyone.
2. **`module-contract.md` and the `add-module` skill:** `Deps.Jobs jobs.Registrar` and `Deps.Files files.Store` (already proposed by `jobs.md` and `files.md`; this module is their first consumer); `moduletest.Conformance` fills nil ones with `jobstest.NewFake` and `filestest.NewFake`.
3. **`departments-events.md`:** none now; `ScheduleOf` and `departments.department-page` stay Later as that spec says. Note in its "What `volunteers` will need": positions are standing, so event set-up copies shifts only.
4. **`settings.md`:** let a module declare terms (`Area.Terms []string` with defaults in its catalogue) so `shift`, `position`, `qualification` and `lead` become regional words like `department`; until then this module uses its own catalogue keys. Low priority.
5. **`files.md`:** the example record kind `volunteers.qualification` becomes `volunteers.qualification-record`; Proposed change 1 there (`module.Route.MaxBodyBytes`, `Timeout`) is required by the upload and certificate routes here.
6. **`plan/07-abstractions.md`, "Modules":** one line: "`volunteers`: qualifications and their verified records, positions, membership, shifts and sign-ups (spec `design/module-volunteers.md`)."
7. **`plan/design/README.md`:** row 19 to Draft (done with this spec); `plan/05-roadmap.md` Phase 1: "Spec and build the first module" gains "(spec drafted 2026-10-20)". **`burn-domain` skill, "Volunteering and shifts":** add "Volunteers want to see who else is booked on a shift before they book it, by playa name only, because some people don't get along and would rather pick another shift; regionals differ on whether to allow it (owner, 2026-10-20; D42)" and, under "How regionals differ", "whether volunteers see who else is on a shift".
8. (D43; restates privacy-tags Proposed change 3) **`plan/design/people.md`:** `Directory.LegalNamesOf(ctx, []model.PersonID) (map[model.PersonID]string, error)` (permission `people.contacts.read` in any extent, audited per returned person, never on a route; `peopletest.Fake.AddLegalName`); `people.contacts.read` and `people.directory.read` gain `volunteer-coordinator` in `StarterRoles`. Until applied, `RosterEntry.LegalName` is declared, tagged and always absent.
9. (D43; restates privacy-tags Proposed change 2) **`plan/design/authz.md`:** the `volunteer-coordinator` site starter role (`RoleVolunteerCoordinator`, migration `0003`). This module lists the key in `StarterRoles` now; `Seed` skips a key with no role and applies it at the first `bms migrate` after the role exists.
10. (D43) **`plan/05-roadmap.md`, Phase 1:** `privacy-tags` is built before this module's services (privacy-tags Proposed change 7); **`.claude/skills/burn-domain/SKILL.md`, "Who may see what":** "Playa names and who is on which shift are, by default, visible to anyone who volunteers; legal names only to a volunteer coordinator and whichever other roles the regional chooses; the regional sets both (owner, 2026-10-20; D43)" (the main session records it).
