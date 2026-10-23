// Package vol holds the record, input and response types the volunteers module's service
// and its postgres store share (module-volunteers.md, "Interface"). It imports only model,
// lib and the core contracts it names types from (files, authz, privacy).
//
// Every type that reaches the wire carries json and privacy tags (privacy-tags.md, D43):
// everything here is internal unless a field says otherwise, each internal field carries the
// omit option its kind needs (D46: omitempty, or omitzero for a struct or array such as a
// time.Time or a model.ID), so a hidden field is absent, never a placeholder, and a screen
// reads an absent count or flag as zero.
package vol

import (
	"time"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// The limits (module-volunteers.md, Summary). Bytes, not characters.
const (
	MaxNameBytes        = 100
	MaxDescriptionBytes = 2000
	MaxLabelBytes       = 100
	MaxLocationBytes    = 200
	MaxSlots            = 200
	MaxShiftDuration    = 48 * time.Hour
	MaxValidityDays     = 3650
	MaxRequirements     = 20
	MaxList             = 500
	MaxIDs              = 200
)

// The permissions the module declares (all department-scoped except PermissionSignUp).
const (
	PermissionSignUp               = "volunteers.shifts.signup"
	PermissionShiftsManage         = "volunteers.shifts.manage"
	PermissionRostersRead          = "volunteers.rosters.read"
	PermissionPositionsManage      = "volunteers.positions.manage"
	PermissionMembersManage        = "volunteers.members.manage"
	PermissionQualificationsManage = "volunteers.qualifications.manage"
	PermissionQualificationsRead   = "volunteers.qualifications.read"
	PermissionQualificationsVerify = "volunteers.qualifications.verify"
	// PermissionContactsRead is people's permission, the holders of the legal_name group.
	PermissionContactsRead = "people.contacts.read"
)

type (
	position      struct{}
	qualification struct{}
	record        struct{}
	member        struct{}
	shift         struct{}
	signup        struct{}
)

type (
	// PositionID identifies a position.
	PositionID = model.ID[position]
	// QualificationID identifies a catalogue entry.
	QualificationID = model.ID[qualification]
	// RecordID identifies a qualification record.
	RecordID = model.ID[record]
	// MemberID identifies a membership.
	MemberID = model.ID[member]
	// ShiftID identifies a shift.
	ShiftID = model.ID[shift]
	// SignupID identifies a sign-up.
	SignupID = model.ID[signup]
)

// Access says who may sign up for a position's shifts.
type Access string

// The access rules.
const (
	AccessOpen    Access = "open"    // anyone eligible may sign up
	AccessMembers Access = "members" // department members only
)

// Valid reports whether a is one of the two access rules. Cannot fail.
func (a Access) Valid() bool { return a == AccessOpen || a == AccessMembers }

// ---- qualifications ----

// Qualification is one entry of the catalogue.
type Qualification struct {
	ID           QualificationID    `json:"id,omitzero" privacy:"internal"`
	Department   model.DepartmentID `json:"department,omitzero" privacy:"internal"` // zero = org-wide
	Name         string             `json:"name,omitempty" privacy:"internal"`
	Description  string             `json:"description,omitempty" privacy:"internal"`
	ValidityDays int                `json:"validity_days,omitempty" privacy:"internal"` // 0 = indefinite by default
	ArchivedAt   time.Time          `json:"archived_at,omitzero" privacy:"internal"`    // zero = active
	CreatedAt    time.Time          `json:"created_at,omitzero" privacy:"internal"`
	UpdatedAt    time.Time          `json:"updated_at,omitzero" privacy:"internal"`
}

// Archived reports whether the qualification is hidden from pickers.
func (q Qualification) Archived() bool { return !q.ArchivedAt.IsZero() }

// Validate fails errs.Invalid joining one validate.FieldError per problem, on the fields
// "id", "name", "description" and "validity_days" (codes required, too_long {max},
// control_chars, malformed, range).
func (q Qualification) Validate() error {
	acc := validate.Errors{Op: "vol.Qualification.Validate"}
	if q.ID.IsZero() {
		acc.Addf("id", validate.CodeRequired, "must be set")
	}
	checkText(&acc, "name", q.Name, 1, MaxNameBytes, false)
	checkText(&acc, "description", q.Description, 0, MaxDescriptionBytes, true)
	acc.Add(validity(q.ValidityDays))
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

func validity(days int) error {
	if days == 0 {
		return nil
	}
	return validate.Range("validity_days", days, 1, MaxValidityDays) //nolint:wrapcheck // validate.Range carries errs.Invalid
}

// QualificationInput creates or changes a qualification.
type QualificationInput struct {
	Department   model.DepartmentID `json:"department"` // zero = org-wide; immutable after creation
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	ValidityDays int                `json:"validity_days"` // 0 = indefinite
}

// Validate fails errs.Invalid on "name", "description" and "validity_days" as
// Qualification.Validate does.
func (in QualificationInput) Validate() error {
	acc := validate.Errors{Op: "vol.QualificationInput.Validate"}
	checkText(&acc, "name", in.Name, 1, MaxNameBytes, false)
	checkText(&acc, "description", in.Description, 0, MaxDescriptionBytes, true)
	acc.Add(validity(in.ValidityDays))
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// QualificationQuery filters ListQualifications.
type QualificationQuery struct {
	Department      model.DepartmentID // zero = org-wide ones plus every department's the caller may see
	IncludeArchived bool               // needs qualifications.manage in that extent
}

// QualificationList is the body of the list route.
type QualificationList struct {
	Items []Qualification `json:"items,omitzero" privacy:"internal"`
}

// ---- records ----

// Record says that a person holds a qualification, as a lead verified it.
type Record struct {
	ID            RecordID        `json:"id,omitzero" privacy:"internal"`
	Qualification QualificationID `json:"qualification,omitzero" privacy:"internal"`
	Person        model.PersonID  `json:"person,omitzero" privacy:"internal"`
	AcquiredOn    Date            `json:"acquired_on,omitzero" privacy:"internal"`
	ExpiresOn     Date            `json:"expires_on,omitzero" privacy:"internal"`  // zero = indefinite
	VerifiedBy    model.PersonID  `json:"verified_by,omitzero" privacy:"internal"` // zero = pending
	VerifiedAt    time.Time       `json:"verified_at,omitzero" privacy:"internal"`
	Certificate   files.FileID    `json:"certificate,omitzero" privacy:"internal"` // zero = none; the file itself is personal and served by its own route
	CreatedAt     time.Time       `json:"created_at,omitzero" privacy:"internal"`
	UpdatedAt     time.Time       `json:"updated_at,omitzero" privacy:"internal"`
}

// Verified reports whether a lead has verified the record (a pending one does not count).
func (r Record) Verified() bool { return !r.VerifiedAt.IsZero() }

// ValidOn reports whether the record counts on d: verified, and not expired before d.
func (r Record) ValidOn(d Date) bool {
	return r.Verified() && (r.ExpiresOn.IsZero() || !r.ExpiresOn.Before(d))
}

// Validate fails errs.Invalid on "id", "qualification", "person", "acquired_on" and
// "expires_on" (required, malformed, range).
func (r Record) Validate() error {
	acc := validate.Errors{Op: "vol.Record.Validate"}
	if r.ID.IsZero() {
		acc.Addf("id", validate.CodeRequired, "must be set")
	}
	if r.Qualification.IsZero() {
		acc.Addf("qualification", validate.CodeRequired, "must be set")
	}
	if r.Person.IsZero() {
		acc.Addf("person", validate.CodeRequired, "must be set")
	}
	checkDates(&acc, r.AcquiredOn, r.ExpiresOn)
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// checkDates records a missing or impossible AcquiredOn and an ExpiresOn before it.
func checkDates(acc *validate.Errors, acquired, expires Date) {
	if acquired.IsZero() {
		acc.Addf("acquired_on", validate.CodeRequired, "must be set")
	}
	checkDate(acc, "acquired_on", acquired)
	checkDate(acc, "expires_on", expires)
	if !acquired.IsZero() && !expires.IsZero() && expires.Before(acquired) {
		acc.Addf("expires_on", validate.CodeRange, "must not be before acquired_on")
	}
}

// RecordInput is way (a): a lead records a qualification from their own training records.
type RecordInput struct {
	Qualification QualificationID `json:"qualification"`
	Person        model.PersonID  `json:"person"`
	AcquiredOn    Date            `json:"acquired_on"`
	ExpiresOn     Date            `json:"expires_on"` // zero = the catalogue default (AcquiredOn + ValidityDays), or indefinite
	NoExpiry      bool            `json:"no_expiry"`  // true = indefinite even when the catalogue has a validity; with ExpiresOn set: Invalid one_of
}

// Validate fails errs.Invalid on "qualification", "person" (required), "acquired_on" and
// "expires_on" (required, malformed, range) and "no_expiry" (one_of: set together with an
// expires_on).
func (in RecordInput) Validate() error {
	acc := validate.Errors{Op: "vol.RecordInput.Validate"}
	if in.Qualification.IsZero() {
		acc.Addf("qualification", validate.CodeRequired, "must be set")
	}
	if in.Person.IsZero() {
		acc.Addf("person", validate.CodeRequired, "must be set")
	}
	checkDates(&acc, in.AcquiredOn, in.ExpiresOn)
	checkNoExpiry(&acc, in.NoExpiry, in.ExpiresOn)
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// checkNoExpiry records no_expiry set together with an expires_on (one_of).
func checkNoExpiry(acc *validate.Errors, noExpiry bool, expires Date) {
	if noExpiry && !expires.IsZero() {
		acc.Addf("no_expiry", validate.CodeOneOf, "give an expiry date or never expires, not both")
	}
}

// UploadInput is way (b): a person uploads a certificate for a lead to verify. The handler
// builds it from the multipart form: the text fields before the file are qualification,
// acquired_on and expires_on.
type UploadInput struct {
	Qualification QualificationID
	AcquiredOn    Date // what the certificate says; the lead may correct it
	ExpiresOn     Date
	Upload        *files.Multipart // the file; Accept = images and PDF
}

// Validate fails errs.Invalid on "qualification", "acquired_on", "expires_on" and "file"
// (required when Upload is nil).
func (in UploadInput) Validate() error {
	acc := validate.Errors{Op: "vol.UploadInput.Validate"}
	if in.Qualification.IsZero() {
		acc.Addf("qualification", validate.CodeRequired, "must be set")
	}
	checkDates(&acc, in.AcquiredOn, in.ExpiresOn)
	if in.Upload == nil {
		acc.Addf("file", validate.CodeRequired, "choose a file")
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// VerifyInput lets the lead correct the dates the person entered; a zero date keeps it.
type VerifyInput struct {
	AcquiredOn Date `json:"acquired_on"`
	ExpiresOn  Date `json:"expires_on"`
	NoExpiry   bool `json:"no_expiry"` // true = clear the expiry (the certificate never expires); with ExpiresOn set: Invalid one_of
}

// Validate fails errs.Invalid on "acquired_on" and "expires_on" (malformed) and "no_expiry"
// (one_of: set together with an expires_on).
func (in VerifyInput) Validate() error {
	acc := validate.Errors{Op: "vol.VerifyInput.Validate"}
	checkDate(&acc, "acquired_on", in.AcquiredOn)
	checkDate(&acc, "expires_on", in.ExpiresOn)
	checkNoExpiry(&acc, in.NoExpiry, in.ExpiresOn)
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// PendingQuery filters PendingRecords.
type PendingQuery struct {
	Department model.DepartmentID // zero = every pending record the caller may verify
}

// RecordList is the body of the record list routes. SelfUpload is present only on a person's
// own list: whether UploadCertificate is on (the self_upload setting), so the My volunteering
// screen can show or hide the upload action without settings.read.
type RecordList struct {
	Items      []Record `json:"items,omitzero" privacy:"internal"`
	SelfUpload *bool    `json:"self_upload,omitempty" privacy:"internal"`
}

// ---- positions ----

// Position is a role within a department, with the qualifications it needs.
type Position struct {
	ID             PositionID         `json:"id,omitzero" privacy:"internal"`
	Department     model.DepartmentID `json:"department,omitzero" privacy:"internal"`
	Name           string             `json:"name,omitempty" privacy:"internal"`
	Description    string             `json:"description,omitempty" privacy:"internal"`
	Access         Access             `json:"access,omitempty" privacy:"internal"`
	Requirements   []QualificationID  `json:"requirements,omitempty" privacy:"internal"`     // at most MaxRequirements, sorted
	HideShiftMates bool               `json:"hide_shift_mates,omitempty" privacy:"internal"` // true = volunteers never see who else is on this position's shifts (D42)
	ArchivedAt     time.Time          `json:"archived_at,omitzero" privacy:"internal"`
	CreatedAt      time.Time          `json:"created_at,omitzero" privacy:"internal"`
	UpdatedAt      time.Time          `json:"updated_at,omitzero" privacy:"internal"`
}

// Archived reports whether the position is hidden from pickers.
func (p Position) Archived() bool { return !p.ArchivedAt.IsZero() }

// Validate fails errs.Invalid on "id", "department", "name", "description", "access" and
// "requirements".
func (p Position) Validate() error {
	acc := validate.Errors{Op: "vol.Position.Validate"}
	if p.ID.IsZero() {
		acc.Addf("id", validate.CodeRequired, "must be set")
	}
	if p.Department.IsZero() {
		acc.Addf("department", validate.CodeRequired, "must be set")
	}
	checkText(&acc, "name", p.Name, 1, MaxNameBytes, false)
	checkText(&acc, "description", p.Description, 0, MaxDescriptionBytes, true)
	if !p.Access.Valid() {
		acc.Addf("access", validate.CodeOneOf, "must be one of: open, members")
	}
	if len(p.Requirements) > MaxRequirements {
		acc.Add(FieldProblem("", "requirements", CodeTooMany, "too many requirements", maxParam(MaxRequirements)))
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// PositionInput creates or changes a position.
type PositionInput struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Access         Access `json:"access"`
	HideShiftMates bool   `json:"hide_shift_mates"`
}

// Validate fails errs.Invalid on "name", "description" and "access" (one_of).
func (in PositionInput) Validate() error {
	acc := validate.Errors{Op: "vol.PositionInput.Validate"}
	checkText(&acc, "name", in.Name, 1, MaxNameBytes, false)
	checkText(&acc, "description", in.Description, 0, MaxDescriptionBytes, true)
	if !in.Access.Valid() {
		acc.Addf("access", validate.CodeOneOf, "must be one of: open, members")
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// RequirementsInput is the body of PUT /positions/{position}/requirements.
type RequirementsInput struct {
	Qualifications []QualificationID `json:"qualifications"`
}

// PositionList is the body of the position list route.
type PositionList struct {
	Items []Position `json:"items,omitzero" privacy:"internal"`
}

// ---- members ----

// Member says that a person belongs to a department.
type Member struct {
	ID         MemberID           `json:"id,omitzero" privacy:"internal"`
	Department model.DepartmentID `json:"department,omitzero" privacy:"internal"`
	Person     model.PersonID     `json:"person,omitzero" privacy:"internal"`
	AddedBy    model.PersonID     `json:"-"` // zero after that person was deleted; the screens do not need it
	CreatedAt  time.Time          `json:"created_at,omitzero" privacy:"internal"`
	UpdatedAt  time.Time          `json:"updated_at,omitzero" privacy:"internal"`
}

// Validate fails errs.Invalid on "id", "department" and "person" (required).
func (m Member) Validate() error {
	acc := validate.Errors{Op: "vol.Member.Validate"}
	if m.ID.IsZero() {
		acc.Addf("id", validate.CodeRequired, "must be set")
	}
	if m.Department.IsZero() {
		acc.Addf("department", validate.CodeRequired, "must be set")
	}
	if m.Person.IsZero() {
		acc.Addf("person", validate.CodeRequired, "must be set")
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// MemberView is a member with the playa name and the positions they are eligible for today.
type MemberView struct {
	Member
	PlayaName string       `json:"playa_name,omitempty" privacy:"internal"`
	Eligible  []PositionID `json:"eligible,omitempty" privacy:"internal"` // active positions of the department the person is eligible for today
}

// MemberInput is the body of POST /departments/{department}/members.
type MemberInput struct {
	Person model.PersonID `json:"person"`
}

// MemberList is the body of GET /me/departments.
type MemberList struct {
	Items []Member `json:"items,omitzero" privacy:"internal"`
}

// MemberQuery pages ListMembers (order created_at, id).
type MemberQuery struct {
	Department model.DepartmentID
	Page
}

// Validate fails errs.Invalid on "department" (required), "limit" (range) and "after" (malformed).
func (q MemberQuery) Validate() error {
	acc := validate.Errors{Op: "vol.MemberQuery.Validate"}
	if q.Department.IsZero() {
		acc.Addf("department", validate.CodeRequired, "must be set")
	}
	acc.Add(q.Page.Validate())
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// MemberViewList is the body of GET /departments/{department}/members. Next is the cursor of
// the next page, absent on the last.
type MemberViewList struct {
	Items []MemberView `json:"items,omitzero" privacy:"internal"`
	Next  Cursor       `json:"next,omitempty" privacy:"internal"`
}

// ---- shifts ----

// BookedAs says who booked a sign-up: the person themself or a lead.
type BookedAs string

// The BookedAs values.
const (
	BookedSelf BookedAs = "self"
	BookedLead BookedAs = "lead"
)

// CancelReason says why a sign-up or a shift was canceled.
type CancelReason string

// The cancel reasons.
const (
	CancelSelf               CancelReason = "self"
	CancelLead               CancelReason = "lead"
	CancelShiftCanceled      CancelReason = "shift_canceled"
	CancelDepartmentArchived CancelReason = "department_archived"
	CancelEventClosed        CancelReason = "event_closed"
)

// Shift is a block of time in a position, for an event or for standing work.
type Shift struct {
	ID           ShiftID            `json:"id,omitzero" privacy:"internal"`
	Department   model.DepartmentID `json:"department,omitzero" privacy:"internal"`
	Position     PositionID         `json:"position,omitzero" privacy:"internal"`
	Event        model.EventID      `json:"event,omitzero" privacy:"internal"` // zero = standing work
	Label        string             `json:"label,omitempty" privacy:"internal"`
	Starts       time.Time          `json:"starts,omitzero" privacy:"internal"`
	Ends         time.Time          `json:"ends,omitzero" privacy:"internal"`
	Zone         string             `json:"zone,omitempty" privacy:"internal"` // IANA; the event's when Event is set
	Slots        int                `json:"slots,omitempty" privacy:"internal"`
	Location     string             `json:"location,omitempty" privacy:"internal"`
	Notes        string             `json:"notes,omitempty" privacy:"internal"`
	CanceledAt   time.Time          `json:"canceled_at,omitzero" privacy:"internal"`
	CancelReason CancelReason       `json:"cancel_reason,omitempty" privacy:"internal"`
	CopiedFrom   ShiftID            `json:"-"`
	CreatedBy    model.PersonID     `json:"-"`
	CreatedAt    time.Time          `json:"created_at,omitzero" privacy:"internal"`
	UpdatedAt    time.Time          `json:"updated_at,omitzero" privacy:"internal"`
}

// Canceled reports whether the shift was canceled.
func (s Shift) Canceled() bool { return !s.CanceledAt.IsZero() }

// Times returns the shift's time range.
func (s Shift) Times() model.TimeRange { return model.TimeRange{Start: s.Starts, End: s.Ends} }

// Loc returns the shift's zone as a *time.Location, UTC when the zone does not load
// (a stored zone always loads; this is a safe fallback for a corrupt row).
func (s Shift) Loc() *time.Location {
	loc, err := time.LoadLocation(s.Zone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Validate fails errs.Invalid on "id", "department", "position", "label", "starts", "ends",
// "zone", "slots", "location" and "notes".
func (s Shift) Validate() error {
	acc := validate.Errors{Op: "vol.Shift.Validate"}
	if s.ID.IsZero() {
		acc.Addf("id", validate.CodeRequired, "must be set")
	}
	if s.Department.IsZero() {
		acc.Addf("department", validate.CodeRequired, "must be set")
	}
	if s.Position.IsZero() {
		acc.Addf("position", validate.CodeRequired, "must be set")
	}
	checkShiftFields(&acc, s.Label, s.Starts, s.Ends, s.Slots, s.Location, s.Notes)
	if !ValidZone(s.Zone) {
		acc.Addf("zone", validate.CodeMalformed, "must be an IANA time zone name")
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

func checkShiftFields(acc *validate.Errors, label string, starts, ends time.Time, slots int, location, notes string) {
	checkText(acc, "label", label, 1, MaxLabelBytes, false)
	checkText(acc, "location", location, 0, MaxLocationBytes, false)
	checkText(acc, "notes", notes, 0, MaxDescriptionBytes, true)
	acc.Add(validate.Range("slots", slots, 1, MaxSlots))
	switch {
	case starts.IsZero():
		acc.Addf("starts", validate.CodeRequired, "must be set")
	case ends.IsZero():
		acc.Addf("ends", validate.CodeRequired, "must be set")
	case !starts.Before(ends):
		acc.Addf("ends", validate.CodeRange, "must be after starts")
	case ends.Sub(starts) > MaxShiftDuration:
		acc.Add(FieldProblem("", "ends", validate.CodeRange, "a shift is at most 48 hours", map[string]string{"max": MaxShiftDuration.String()}))
	}
}

// ShiftInput creates or changes a shift.
type ShiftInput struct {
	Position PositionID    `json:"position"`
	Event    model.EventID `json:"event"`
	Label    string        `json:"label"`
	Starts   time.Time     `json:"starts"`
	Ends     time.Time     `json:"ends"`
	Zone     string        `json:"zone"` // ignored when Event is set (the event's zone is used)
	Slots    int           `json:"slots"`
	Location string        `json:"location"`
	Notes    string        `json:"notes"`
}

// Validate fails errs.Invalid on "position" (required), "label", "starts", "ends", "slots",
// "location", "notes" and, for standing work, "zone" (required, malformed).
func (in ShiftInput) Validate() error {
	acc := validate.Errors{Op: "vol.ShiftInput.Validate"}
	if in.Position.IsZero() {
		acc.Addf("position", validate.CodeRequired, "must be set")
	}
	checkShiftFields(&acc, in.Label, in.Starts, in.Ends, in.Slots, in.Location, in.Notes)
	if in.Event.IsZero() {
		switch {
		case in.Zone == "":
			acc.Addf("zone", validate.CodeRequired, "is required for standing work")
		case !ValidZone(in.Zone):
			acc.Addf("zone", validate.CodeMalformed, "must be an IANA time zone name")
		}
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// ShiftQuery filters ListShifts (order starts, id).
type ShiftQuery struct {
	Department      model.DepartmentID // zero = all the caller may see
	Event           model.EventID      // zero = any, including standing work
	Position        PositionID
	Within          model.TimeRange // zero = from now on; either bound alone is allowed (Start only = from then on, End only = from now until then)
	IncludeCanceled bool            // needs shifts.manage or rosters.read in that extent
	Page
}

// Validate fails errs.Invalid on "limit" (range), "after" (malformed) and "within" (range, when
// both bounds are set and the start is not before the end).
func (q ShiftQuery) Validate() error {
	acc := validate.Errors{Op: "vol.ShiftQuery.Validate"}
	acc.Add(q.Page.Validate())
	checkWithin(&acc, q.Within)
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// checkWithin records a window whose start is not before its end (either bound alone is fine).
func checkWithin(acc *validate.Errors, w model.TimeRange) {
	if !w.Start.IsZero() && !w.End.IsZero() && !w.Start.Before(w.End) {
		acc.Addf("within", validate.CodeRange, "needs a start before its end")
	}
}

// Eligibility says whether the viewer may sign up for a shift now.
type Eligibility struct {
	Eligible bool              `json:"eligible,omitempty" privacy:"internal"`
	Missing  []QualificationID `json:"missing,omitempty" privacy:"internal"` // requirements without a valid record
	Reason   string            `json:"reason,omitempty" privacy:"internal"`  // "", or one of the Reason* constants
}

// The reasons an Eligibility can carry.
const (
	ReasonNotMember = "not_member"
	ReasonFull      = "full"
	ReasonStarted   = "started"
	ReasonCanceled  = "canceled"
	ReasonClosed    = "closed"
	ReasonArchived  = "archived"
	ReasonOverlap   = "overlap"
)

// ShiftMates is what a volunteer may see of the others booked on a shift: playa names
// only, never a legal name, pronouns, a contact detail or a person ID.
type ShiftMates struct {
	Names    []string `json:"names,omitempty" privacy:"internal"`    // playa names of the other booked people, sorted (people keeps them unique)
	Unlisted int      `json:"unlisted,omitempty" privacy:"internal"` // booked people shown by no name: no playa name yet, or chose not to be listed
}

// ShiftSummary is a shift with what the viewer needs to decide.
type ShiftSummary struct {
	Shift
	DepartmentName string      `json:"department_name,omitempty" privacy:"internal"` // from Departments.NamesOf, one batched call per list; "" when the directory does not know it
	PositionName   string      `json:"position_name,omitempty" privacy:"internal"`   // from the position join
	Booked         int         `json:"booked,omitempty" privacy:"internal"`
	Mine           SignupID    `json:"mine,omitzero" privacy:"internal"` // the caller's booked sign-up, or zero
	Eligibility    Eligibility `json:"eligibility,omitzero" privacy:"internal"`
	Mates          *ShiftMates `json:"mates,omitempty" privacy:"internal,group=shift_mates"` // GetShift only; absent when the policy hides it
}

// ShiftList is the body of the shift list route. Next is the cursor of the next page, absent
// on the last.
type ShiftList struct {
	Items []ShiftSummary `json:"items,omitzero" privacy:"internal"`
	Next  Cursor         `json:"next,omitempty" privacy:"internal"`
}

// CopyInput is the body of POST /departments/{department}/shifts/copy.
type CopyInput struct {
	From model.EventID `json:"from"`
	To   model.EventID `json:"to"`
}

// CopyResult is the body of a CopyShifts answer.
type CopyResult struct {
	Created int `json:"created,omitempty" privacy:"internal"`
}

// ---- sign-ups ----

// SignupStatus is where a sign-up is.
type SignupStatus string

// The sign-up statuses.
const (
	SignupBooked   SignupStatus = "booked"
	SignupCanceled SignupStatus = "canceled"
)

// Signup books a person into a shift.
type Signup struct {
	ID           SignupID       `json:"id,omitzero" privacy:"internal"`
	Shift        ShiftID        `json:"shift,omitzero" privacy:"internal"`
	Person       model.PersonID `json:"person,omitzero" privacy:"internal"`
	Status       SignupStatus   `json:"status,omitempty" privacy:"internal"`
	BookedBy     model.PersonID `json:"-"`                                     // zero = the person themself
	BookedAs     BookedAs       `json:"-"`                                     // BookedSelf or BookedLead: who booked, kept after that lead's deletion (the export's role word)
	Waived       bool           `json:"waived,omitempty" privacy:"internal"`   // a requirement was waived at booking (D42); kept after the waiving lead is deleted (D51); the roster flag and the export read this
	WaivedBy     model.PersonID `json:"waived_by,omitzero" privacy:"internal"` // the lead who assigned despite a missing requirement (D42); nulled when that lead is deleted
	CanceledAt   time.Time      `json:"canceled_at,omitzero" privacy:"internal"`
	CanceledBy   model.PersonID `json:"-"` // zero = the system
	CancelReason CancelReason   `json:"cancel_reason,omitempty" privacy:"internal"`
	CreatedAt    time.Time      `json:"created_at,omitzero" privacy:"internal"`
	UpdatedAt    time.Time      `json:"updated_at,omitzero" privacy:"internal"`
}

// Booked reports whether the sign-up still holds a slot.
func (s Signup) Booked() bool { return s.Status == SignupBooked }

// AssignInput is the body of POST /shifts/{shift}/assign.
type AssignInput struct {
	Person model.PersonID `json:"person"`
	Waive  bool           `json:"waive"` // book even if unqualified; recorded and audited (D42)
}

// Validate fails errs.Invalid on "person" (required).
func (in AssignInput) Validate() error {
	acc := validate.Errors{Op: "vol.AssignInput.Validate"}
	if in.Person.IsZero() {
		acc.Addf("person", validate.CodeRequired, "must be set")
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// RosterEntry is one booked person on the lead's roster; Signup.Waived flags a
// waiver.
type RosterEntry struct {
	Signup
	PlayaName string  `json:"playa_name,omitempty" privacy:"internal"`
	LegalName *string `json:"legal_name,omitempty" privacy:"personal,group=legal_name"` // D43; absent unless the policy shows it and people knows one
	Eligible  bool    `json:"eligible,omitempty" privacy:"internal"`
}

// Roster is a shift with the people booked on it.
type Roster struct {
	Shift   Shift         `json:"shift,omitzero" privacy:"internal"`
	Entries []RosterEntry `json:"entries,omitempty" privacy:"internal"`
}

// ScheduleQuery filters MySchedule. Past false (default): bookings whose shift has not ended,
// soonest first, including sign-ups someone other than the person canceled (reason lead,
// shift_canceled, department_archived, event_closed) so a removal is seen without e-mail;
// the person's own cancellations are not listed. Past true: booked sign-ups whose shift has
// ended, newest first. Within narrows either mode; either bound alone is allowed.
type ScheduleQuery struct {
	Within model.TimeRange
	Past   bool
	Page   // order (starts, id) ascending for upcoming, descending for past
}

// Validate fails errs.Invalid on "limit" (range), "after" (malformed) and "within" (range).
func (q ScheduleQuery) Validate() error {
	acc := validate.Errors{Op: "vol.ScheduleQuery.Validate"}
	acc.Add(q.Page.Validate())
	checkWithin(&acc, q.Within)
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// ScheduleEntry is one of the actor's bookings with its shift, department and position
// names. The top-level id is the sign-up's; the shift is the nested object.
type ScheduleEntry struct {
	Signup
	Shift      Shift  `json:"shift,omitzero" privacy:"internal"`
	Department string `json:"department_name,omitempty" privacy:"internal"`
	Position   string `json:"position_name,omitempty" privacy:"internal"`
}

// ScheduleList is the body of GET /me/schedule. Next is the cursor of the next page, absent
// on the last.
type ScheduleList struct {
	Items []ScheduleEntry `json:"items,omitzero" privacy:"internal"`
	Next  Cursor          `json:"next,omitempty" privacy:"internal"`
}

// Listing is the body of GET /me/listing: whether the actor's playa name is shown to other
// volunteers on their shifts. An absent flag reads as false.
type Listing struct {
	Listed bool `json:"listed,omitempty" privacy:"internal"`
}

// ListingInput is the body of PUT /me/listing.
type ListingInput struct {
	Listed bool `json:"listed"`
}

// ---- privacy groups ----

// The privacy groups (privacy-tags) and the extra audience of the first.
const (
	GroupShiftMates                  = "shift_mates"
	GroupLegalName                   = "legal_name"
	AudienceBooked  privacy.Audience = "booked"
)

// Groups are this module's privacy groups: the regional sets each one's audience in
// visibility_<name>. GroupShiftMates guards ShiftSummary.Mates ("anyone who volunteers",
// D43, is a holder of volunteers.shifts.signup; "booked" is asserted by the service when the
// viewer holds a booking on that shift). GroupLegalName guards RosterEntry.LegalName: holders
// are holders of people.contacts.read covering the shift's department; its options are nobody
// and holders only (Q-VOL8: legal names stay with permission holders).
var Groups = []privacy.Group{
	{
		Name: GroupShiftMates, Permission: PermissionSignUp, Default: privacy.AudienceHolders,
		Options: []privacy.Audience{privacy.AudienceNobody, AudienceBooked, privacy.AudienceHolders, privacy.AudienceSignedIn},
		Extra:   []privacy.Audience{AudienceBooked},
	},
	{
		Name: GroupLegalName, Permission: PermissionContactsRead, Default: privacy.AudienceHolders,
		Options: []privacy.Audience{privacy.AudienceNobody, privacy.AudienceHolders}, // no self: legal names stay with holders (Q-VOL8)
	},
}

// Views lists every response type, for Manifest.Views (privacy-tags A1.3).
func Views() []any {
	return []any{
		Qualification{}, QualificationList{}, Record{}, RecordList{}, Position{}, PositionList{},
		Member{}, MemberList{}, MemberView{}, MemberViewList{}, Shift{}, ShiftSummary{}, ShiftList{},
		RosterEntry{}, ScheduleEntry{},
		CopyResult{}, Signup{}, Roster{}, ScheduleList{}, Listing{},
	}
}
