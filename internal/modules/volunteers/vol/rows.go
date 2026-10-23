package vol

import (
	"time"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/model"
)

// The types below are what the service and the postgres store exchange beyond the record
// types themselves: a record with a joined column, a work-list row, a count. None reaches the
// wire, so none carries tags.

// RecordRow is a Record with the department of its qualification (zero = org-wide), for the
// permission check on a record.
type RecordRow struct {
	Record
	QualificationDepartment model.DepartmentID
}

// RecordFilter says which pending records a lead may verify (list_pending_records.sql).
type RecordFilter struct {
	Department  model.DepartmentID // zero = every department the caller may verify
	All         bool               // site-wide verify holder
	Departments []model.DepartmentID
	Actor       model.PersonID // excluded as the record's person (no self-verification)
	Limit       int
}

// QualificationFilter selects catalogue entries in the caller's view.
type QualificationFilter struct {
	Department          model.DepartmentID // zero = no department filter
	IncludeOrgWide      bool               // with Department set: also the org-wide entries
	ArchivedOrgWide     bool               // the caller may see archived org-wide entries
	ArchivedDepartments []model.DepartmentID
	Limit               int
}

// Held says that Person holds a valid record of Qualification on some date.
type Held struct {
	Person        model.PersonID
	Qualification QualificationID
}

// RecordGone is what a deleted record leaves: the audit and the file deletion need it.
type RecordGone struct {
	ID            RecordID
	Person        model.PersonID
	Qualification QualificationID
	Certificate   files.FileID
}

// ExpiringRecord is one row of the daily expiry scan's work list.
type ExpiringRecord struct {
	ID                RecordID
	Person            model.PersonID
	Qualification     QualificationID
	QualificationName string
	ExpiresOn         Date
}

// ExpiredRecord is a record the purge removes (with its certificate file).
type ExpiredRecord struct {
	ID          RecordID
	Certificate files.FileID
}

// ShiftFilter selects shifts for ListShifts (list_shifts.sql documents each field).
type ShiftFilter struct {
	Department            model.DepartmentID
	Event                 model.EventID
	Position              PositionID
	WindowStart           time.Time
	WindowEnd             time.Time // zero = unbounded
	IncludeCanceled       bool
	PrivilegedAll         bool
	PrivilegedDepartments []model.DepartmentID
	MemberDepartments     []model.DepartmentID
	Actor                 model.PersonID
	AfterStarts           time.Time // keyset cursor (starts, id); zero on the first page
	AfterID               ShiftID
	Limit                 int // rows to fetch: the page size plus one
}

// ShiftRow is a shift with the figures a summary carries and the position facts the
// visibility rule needs.
type ShiftRow struct {
	Shift
	PositionAccess Access
	PositionName   string
	HideShiftMates bool
	Booked         int
	Mine           SignupID
}

// ScheduleFilter selects a person's bookings for MySchedule (list_schedule.sql documents it).
type ScheduleFilter struct {
	Person      model.PersonID
	Now         time.Time
	Past        bool            // false: not ended, ascending, canceled-by-others included; true: ended, booked only, descending
	Within      model.TimeRange // either bound may be zero
	AfterStarts time.Time       // keyset cursor (starts, sign-up id); zero on the first page
	AfterID     SignupID
	Limit       int // rows to fetch: the page size plus one
}

// MemberFilter selects a department's members for ListMembers (list_members_by_department.sql).
type MemberFilter struct {
	Department   model.DepartmentID
	AfterCreated time.Time // keyset cursor (created_at, id); zero on the first page
	AfterID      MemberID
	Limit        int // rows to fetch: the page size plus one
}

// CopyCandidate is a shift CopyShifts may copy; PositionArchived means skip it.
type CopyCandidate struct {
	Shift
	PositionArchived bool
}

// SignupDetail is a sign-up with the essentials of its shift and position.
type SignupDetail struct {
	Signup          Signup
	Department      model.DepartmentID
	Position        PositionID
	PositionName    string
	Label           string
	Starts          time.Time
	Ends            time.Time
	Zone            string
	Location        string
	ShiftCanceledAt time.Time
}

// CanceledSignup is a sign-up a cancellation just ended, with what a notice and a signal need.
type CanceledSignup struct {
	Signup     SignupID
	Shift      ShiftID
	Person     model.PersonID
	Department model.DepartmentID
	Label      string
	Starts     time.Time
	Zone       string
}

// BookedWindow is the time window of one of a person's booked shifts.
type BookedWindow struct {
	Shift  ShiftID
	Starts time.Time
	Ends   time.Time
}

// Overlap names a booked shift that overlaps a time range.
type Overlap struct {
	Shift  ShiftID
	Label  string
	Starts time.Time
	Ends   time.Time
}

// Mate is another booked person on a shift and whether they chose to be listed.
type Mate struct {
	Person model.PersonID
	Listed bool
}

// ScheduleRow is one of a person's bookings with its shift and the position's name.
type ScheduleRow struct {
	Signup       Signup
	Shift        Shift
	PositionName string
}

// RecordExport is one qualification record as ExportPerson lists it.
type RecordExport struct {
	ID                RecordID
	QualificationName string
	AcquiredOn        Date
	ExpiresOn         Date
	Verified          bool
	VerifiedAt        time.Time
	Certificate       files.FileID
}

// SignupExport is one sign-up as ExportPerson lists it; who booked or canceled is a role word.
type SignupExport struct {
	Label          string
	Department     model.DepartmentID
	PositionName   string
	Starts         time.Time
	Ends           time.Time
	Zone           string
	Status         SignupStatus
	BookedByRole   string
	Waived         bool
	CanceledAt     time.Time
	CanceledByRole string
	CancelReason   CancelReason
	CreatedAt      time.Time
}

// Anonymised counts the rows whose who-did-it column DeletePerson nulled.
type Anonymised struct {
	Records, Members, Signups, Shifts int
}
