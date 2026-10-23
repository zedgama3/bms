package postgres

import (
	"context"
	"time"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// ---- shifts ----

var (
	sqlInsertShift              = mustSQL("insert_shift.sql")
	sqlInsertShiftCopy          = mustSQL("insert_shift_copy.sql")
	sqlGetShift                 = mustSQL("get_shift.sql")
	sqlGetShiftForUpdate        = mustSQL("get_shift_for_update.sql")
	sqlGetShiftSummary          = mustSQL("get_shift_summary.sql")
	sqlListShifts               = mustSQL("list_shifts.sql")
	sqlUpdateShift              = mustSQL("update_shift.sql")
	sqlCancelShift              = mustSQL("cancel_shift.sql")
	sqlCancelShiftsByDepartment = mustSQL("cancel_shifts_by_department.sql")
	sqlCancelShiftsByEvent      = mustSQL("cancel_shifts_by_event.sql")
	sqlCancelSignupsByShifts    = mustSQL("cancel_signups_by_shifts.sql")
	sqlShiftsToCopy             = mustSQL("list_shifts_for_copy.sql")
	sqlDepartmentsWithShifts    = mustSQL("list_departments_with_shifts_in_event.sql")
)

// shiftDest holds the destinations of the 17 shift columns.
type shiftDest struct {
	id, dept, pos                [16]byte
	event, copied, createdBy     *[16]byte
	label, zone, location, notes string
	starts, ends                 time.Time
	slots                        int32
	canceled                     *time.Time
	reason                       *string
	created, updated             time.Time
}

func (d *shiftDest) ptrs() []any {
	return []any{
		&d.id, &d.dept, &d.pos, &d.event, &d.label, &d.starts, &d.ends, &d.zone, &d.slots, &d.location, &d.notes,
		&d.canceled, &d.reason, &d.copied, &d.createdBy, &d.created, &d.updated,
	}
}

func (d *shiftDest) shift() vol.Shift {
	sh := vol.Shift{
		ID: vol.ShiftID(d.id), Department: model.DepartmentID(d.dept), Position: vol.PositionID(d.pos),
		Event: idOf[model.EventID](d.event), Label: d.label, Starts: d.starts.UTC(), Ends: d.ends.UTC(), Zone: d.zone,
		Slots: int(d.slots), Location: d.location, Notes: d.notes, CanceledAt: timeOf(d.canceled),
		CopiedFrom: idOf[vol.ShiftID](d.copied), CreatedBy: idOf[model.PersonID](d.createdBy),
		CreatedAt: d.created.UTC(), UpdatedAt: d.updated.UTC(),
	}
	if d.reason != nil {
		sh.CancelReason = vol.CancelReason(*d.reason)
	}
	return sh
}

func scanShift(r rowScanner) (vol.Shift, error) {
	var d shiftDest
	if err := r.Scan(d.ptrs()...); err != nil {
		return vol.Shift{}, err //nolint:wrapcheck // translate classifies it
	}
	return d.shift(), nil
}

func scanShiftRow(r rowScanner) (vol.ShiftRow, error) {
	var (
		d       shiftDest
		access  string
		hide    bool
		booked  int64
		mine    *[16]byte
		posName string
	)
	if err := r.Scan(append(d.ptrs(), &access, &hide, &booked, &mine, &posName)...); err != nil {
		return vol.ShiftRow{}, err //nolint:wrapcheck // translate classifies it
	}
	return vol.ShiftRow{
		Shift: d.shift(), PositionAccess: vol.Access(access), PositionName: posName, HideShiftMates: hide, Booked: int(booked),
		Mine: idOf[vol.SignupID](mine),
	}, nil
}

func shiftInsertArgs(sh vol.Shift) []any {
	return []any{
		[16]byte(sh.ID), [16]byte(sh.Department), [16]byte(sh.Position), uuidArg(sh.Event), sh.Label, sh.Starts, sh.Ends,
		sh.Zone, int32(sh.Slots), sh.Location, sh.Notes, uuidArg(sh.CopiedFrom), uuidArg(sh.CreatedBy), sh.CreatedAt, //nolint:gosec // 1..200
	}
}

// InsertShift adds a shift. Fails errs.Invalid (position of another department; unknown
// event; times; slots).
func (s *Store) InsertShift(ctx context.Context, sh vol.Shift) (vol.Shift, error) {
	return queryOne(ctx, s, "volunteers.postgres.InsertShift", sqlInsertShift, scanShift, shiftInsertArgs(sh)...)
}

// InsertShiftCopy adds a copy of a shift; created is false when that source was already
// copied into that event.
func (s *Store) InsertShiftCopy(ctx context.Context, sh vol.Shift) (vol.Shift, bool, error) {
	out, err := queryOne(ctx, s, "volunteers.postgres.InsertShiftCopy", sqlInsertShiftCopy, scanShift, shiftInsertArgs(sh)...)
	if errs.Is(err, errs.NotFound) {
		return vol.Shift{}, false, nil
	}
	if err != nil {
		return vol.Shift{}, false, err
	}
	return out, true, nil
}

// GetShift returns one shift. Fails errs.NotFound.
func (s *Store) GetShift(ctx context.Context, id vol.ShiftID) (vol.Shift, error) {
	return queryOne(ctx, s, "volunteers.postgres.GetShift", sqlGetShift, scanShift, [16]byte(id))
}

// GetShiftForUpdate is GetShift with a row lock. Fails errs.NotFound.
func (s *Store) GetShiftForUpdate(ctx context.Context, id vol.ShiftID) (vol.Shift, error) {
	return queryOne(ctx, s, "volunteers.postgres.GetShiftForUpdate", sqlGetShiftForUpdate, scanShift, [16]byte(id))
}

// GetShiftSummary returns one shift with its booked count, the actor's booking and the
// position facts. Fails errs.NotFound.
func (s *Store) GetShiftSummary(ctx context.Context, id vol.ShiftID, actor model.PersonID) (vol.ShiftRow, error) {
	return queryOne(ctx, s, "volunteers.postgres.GetShiftSummary", sqlGetShiftSummary, scanShiftRow, [16]byte(id), [16]byte(actor))
}

// ListShifts lists up to f.Limit of the shifts the filter allows in (starts, id) order,
// after the keyset cursor (f.AfterStarts, f.AfterID) when f.AfterStarts is set.
func (s *Store) ListShifts(ctx context.Context, f vol.ShiftFilter) ([]vol.ShiftRow, error) {
	var afterStarts, afterID any // nil = the first page
	if !f.AfterStarts.IsZero() {
		afterStarts, afterID = f.AfterStarts, [16]byte(f.AfterID)
	}
	return queryAll(ctx, s, "volunteers.postgres.ListShifts", sqlListShifts, scanShiftRow,
		uuidArg(f.Department), uuidArg(f.Event), uuidArg(f.Position), f.WindowStart, tsArg(f.WindowEnd),
		f.IncludeCanceled, f.PrivilegedAll, uuids(f.PrivilegedDepartments), uuids(f.MemberDepartments),
		[16]byte(f.Actor), afterStarts, afterID, int32(f.Limit)) //nolint:gosec // bounded by vol.MaxList + 1
}

// UpdateShift changes an uncanceled shift. Fails errs.NotFound (gone or canceled).
func (s *Store) UpdateShift(ctx context.Context, sh vol.Shift) (vol.Shift, error) {
	return queryOne(ctx, s, "volunteers.postgres.UpdateShift", sqlUpdateShift, scanShift,
		[16]byte(sh.ID), [16]byte(sh.Position), uuidArg(sh.Event), sh.Label, sh.Starts, sh.Ends, sh.Zone,
		int32(sh.Slots), sh.Location, sh.Notes, sh.UpdatedAt) //nolint:gosec // 1..200
}

// CancelShift cancels an uncanceled shift. Fails errs.NotFound when it is gone or already canceled.
func (s *Store) CancelShift(ctx context.Context, id vol.ShiftID, reason vol.CancelReason, at time.Time) (vol.Shift, error) {
	return queryOne(ctx, s, "volunteers.postgres.CancelShift", sqlCancelShift, scanShift, [16]byte(id), string(reason), at)
}

func scanShiftID(r rowScanner) (vol.ShiftID, error) {
	var id [16]byte
	if err := r.Scan(&id); err != nil {
		return vol.ShiftID{}, err //nolint:wrapcheck // translate classifies it
	}
	return vol.ShiftID(id), nil
}

// CancelShiftsByDepartment cancels one batch of at most limit of the department's shifts that
// have not ended (reason department_archived) and returns their IDs; an empty answer means
// nothing is left.
func (s *Store) CancelShiftsByDepartment(ctx context.Context, dept model.DepartmentID, now time.Time, limit int) ([]vol.ShiftID, error) {
	return queryAll(ctx, s, "volunteers.postgres.CancelShiftsByDepartment", sqlCancelShiftsByDepartment, scanShiftID,
		[16]byte(dept), now, string(vol.CancelDepartmentArchived), int32(limit)) //nolint:gosec // vol.MaxList
}

// CancelShiftsByEvent cancels one batch of at most limit of the event's shifts that have not
// ended (reason event_closed) and returns their IDs.
func (s *Store) CancelShiftsByEvent(ctx context.Context, event model.EventID, now time.Time, limit int) ([]vol.ShiftID, error) {
	return queryAll(ctx, s, "volunteers.postgres.CancelShiftsByEvent", sqlCancelShiftsByEvent, scanShiftID,
		[16]byte(event), now, string(vol.CancelEventClosed), int32(limit)) //nolint:gosec // vol.MaxList
}

// CancelSignupsByShifts cancels the booked sign-ups of the shifts and returns them.
func (s *Store) CancelSignupsByShifts(ctx context.Context, shifts []vol.ShiftID, reason vol.CancelReason, at time.Time) ([]vol.CanceledSignup, error) {
	return queryAll(ctx, s, "volunteers.postgres.CancelSignupsByShifts", sqlCancelSignupsByShifts, scanCanceled,
		uuids(shifts), string(reason), at)
}

// ShiftsToCopy lists one batch (vol.MaxList) of the department's non-canceled shifts in from
// not yet copied into to, in (starts, id) order after the cursor (afterStarts, after); a zero
// afterStarts starts at the beginning.
func (s *Store) ShiftsToCopy(ctx context.Context, dept model.DepartmentID, from, to model.EventID, afterStarts time.Time, after vol.ShiftID) ([]vol.CopyCandidate, error) {
	var cursorStarts, cursorID any // nil = the first batch
	if !afterStarts.IsZero() {
		cursorStarts, cursorID = afterStarts, [16]byte(after)
	}
	return queryAll(ctx, s, "volunteers.postgres.ShiftsToCopy", sqlShiftsToCopy, func(r rowScanner) (vol.CopyCandidate, error) {
		var d shiftDest
		var archived bool
		if err := r.Scan(append(d.ptrs(), &archived)...); err != nil {
			return vol.CopyCandidate{}, err //nolint:wrapcheck // translate classifies it
		}
		return vol.CopyCandidate{Shift: d.shift(), PositionArchived: archived}, nil
	}, [16]byte(dept), [16]byte(from), [16]byte(to), int32(vol.MaxList), cursorStarts, cursorID)
}

// DepartmentsWithShifts lists the departments with non-canceled shifts in the event.
func (s *Store) DepartmentsWithShifts(ctx context.Context, event model.EventID) ([]model.DepartmentID, error) {
	return queryAll(ctx, s, "volunteers.postgres.DepartmentsWithShifts", sqlDepartmentsWithShifts, func(r rowScanner) (model.DepartmentID, error) {
		var id [16]byte
		if err := r.Scan(&id); err != nil {
			return model.DepartmentID{}, err //nolint:wrapcheck // translate classifies it
		}
		return model.DepartmentID(id), nil
	}, [16]byte(event))
}

// ---- sign-ups ----

var (
	sqlLockPersonBookings    = mustSQL("lock_person_bookings.sql")
	sqlLockPersonsBookings   = mustSQL("lock_persons_bookings.sql")
	sqlInsertSignup          = mustSQL("insert_signup.sql")
	sqlFindOverlap           = mustSQL("find_overlapping_booking.sql")
	sqlFindOverlapForChange  = mustSQL("find_overlap_for_shift_change.sql")
	sqlGetSignup             = mustSQL("get_signup.sql")
	sqlGetSignupForUpdate    = mustSQL("get_signup_for_update.sql")
	sqlCancelSignup          = mustSQL("cancel_signup.sql")
	sqlRoster                = mustSQL("list_roster.sql")
	sqlMates                 = mustSQL("list_mates.sql")
	sqlSchedule              = mustSQL("list_schedule.sql")
	sqlSchedulePast          = mustSQL("list_schedule_past.sql")
	sqlBookedWindows         = mustSQL("list_booked_windows.sql")
	sqlGetListed             = mustSQL("get_listed.sql")
	sqlInsertUnlisted        = mustSQL("insert_unlisted.sql")
	sqlDeleteUnlisted        = mustSQL("delete_unlisted.sql")
	sqlUpcomingCounts        = mustSQL("count_upcoming_bookings_by_people.sql")
	sqlPurgeSignups          = mustSQL("purge_signups.sql")
	sqlPurgeCanceledShifts   = mustSQL("purge_canceled_shifts.sql")
	sqlExportRecords         = mustSQL("export_records_by_person.sql")
	sqlExportSignups         = mustSQL("export_signups_by_person.sql")
	sqlCountVerifiedBy       = mustSQL("count_verified_by_person.sql")
	sqlCountShiftsCreatedBy  = mustSQL("count_shifts_created_by_person.sql")
	sqlRecordIDsOf           = mustSQL("list_record_ids_by_person.sql")
	sqlDeleteSignupsByPerson = mustSQL("delete_signups_by_person.sql")
	sqlDeleteMembersByPerson = mustSQL("delete_members_by_person.sql")
	sqlDeleteUnlistedPerson  = mustSQL("delete_unlisted_by_person.sql")
	sqlDeleteRecordsPerson   = mustSQL("delete_records_by_person.sql")
	sqlAnonymisePerson       = mustSQL("anonymise_person.sql")
)

// LockPersonBookings takes the per-person advisory lock for the transaction.
func (s *Store) LockPersonBookings(ctx context.Context, person model.PersonID) error {
	_, err := s.exec(ctx, "volunteers.postgres.LockPersonBookings", sqlLockPersonBookings, [16]byte(person))
	return err
}

// LockPersonsBookings takes the per-person locks of several people in ascending ID order.
func (s *Store) LockPersonsBookings(ctx context.Context, people []model.PersonID) error {
	if len(people) == 0 {
		return nil
	}
	_, err := s.exec(ctx, "volunteers.postgres.LockPersonsBookings", sqlLockPersonsBookings, uuids(people))
	return err
}

func scanSignup(r rowScanner) (vol.Signup, error) {
	var d signupDest
	if err := r.Scan(d.ptrs()...); err != nil {
		return vol.Signup{}, err //nolint:wrapcheck // translate classifies it
	}
	return d.signup(), nil
}

type signupDest struct {
	id, shift, person          [16]byte
	status, bookedAs           string
	waived                     bool
	bookedBy, waivedBy, cancBy *[16]byte
	canceledAt                 *time.Time
	reason                     *string
	created, updated           time.Time
}

func (d *signupDest) ptrs() []any {
	return []any{&d.id, &d.shift, &d.person, &d.status, &d.bookedBy, &d.bookedAs, &d.waived, &d.waivedBy, &d.canceledAt, &d.cancBy, &d.reason, &d.created, &d.updated}
}

func (d *signupDest) signup() vol.Signup {
	su := vol.Signup{
		ID: vol.SignupID(d.id), Shift: vol.ShiftID(d.shift), Person: model.PersonID(d.person), Status: vol.SignupStatus(d.status),
		BookedBy: idOf[model.PersonID](d.bookedBy), BookedAs: vol.BookedAs(d.bookedAs), Waived: d.waived, WaivedBy: idOf[model.PersonID](d.waivedBy),
		CanceledAt: timeOf(d.canceledAt), CanceledBy: idOf[model.PersonID](d.cancBy),
		CreatedAt: d.created.UTC(), UpdatedAt: d.updated.UTC(),
	}
	if d.reason != nil {
		su.CancelReason = vol.CancelReason(*d.reason)
	}
	return su
}

// InsertSignup books a person. Fails errs.Conflict (duplicate), errs.Invalid (unknown person).
func (s *Store) InsertSignup(ctx context.Context, su vol.Signup) (vol.Signup, error) {
	return queryOne(ctx, s, "volunteers.postgres.InsertSignup", sqlInsertSignup, scanSignup,
		[16]byte(su.ID), [16]byte(su.Shift), [16]byte(su.Person), uuidArg(su.BookedBy), uuidArg(su.WaivedBy), su.CreatedAt,
		string(su.BookedAs), su.Waived)
}

// FindOverlap finds the earliest booked, non-canceled shift of person that overlaps times,
// other than exclude.
func (s *Store) FindOverlap(ctx context.Context, person model.PersonID, times model.TimeRange, exclude vol.ShiftID) (vol.Overlap, bool, error) {
	o, err := queryOne(ctx, s, "volunteers.postgres.FindOverlap", sqlFindOverlap, func(r rowScanner) (vol.Overlap, error) {
		var id [16]byte
		var label string
		var starts, ends time.Time
		if err := r.Scan(&id, &label, &starts, &ends); err != nil {
			return vol.Overlap{}, err //nolint:wrapcheck // translate classifies it
		}
		return vol.Overlap{Shift: vol.ShiftID(id), Label: label, Starts: starts.UTC(), Ends: ends.UTC()}, nil
	}, [16]byte(person), times.Start, times.End, [16]byte(exclude))
	if errs.Is(err, errs.NotFound) {
		return vol.Overlap{}, false, nil
	}
	return o, err == nil, err
}

// FindOverlapForShiftChange finds a booked person of the shift who would overlap another of
// their booked shifts with the new times.
func (s *Store) FindOverlapForShiftChange(ctx context.Context, shift vol.ShiftID, times model.TimeRange) (vol.Overlap, bool, error) {
	o, err := queryOne(ctx, s, "volunteers.postgres.FindOverlapForShiftChange", sqlFindOverlapForChange, func(r rowScanner) (vol.Overlap, error) {
		var person, id [16]byte
		if err := r.Scan(&person, &id); err != nil {
			return vol.Overlap{}, err //nolint:wrapcheck // translate classifies it
		}
		return vol.Overlap{Shift: vol.ShiftID(id)}, nil
	}, [16]byte(shift), times.Start, times.End)
	if errs.Is(err, errs.NotFound) {
		return vol.Overlap{}, false, nil
	}
	return o, err == nil, err
}

func scanSignupDetail(r rowScanner) (vol.SignupDetail, error) {
	var (
		d              signupDest
		dept, pos      [16]byte
		posName, label string
		starts, ends   time.Time
		zone, location string
		shiftCanceled  *time.Time
	)
	if err := r.Scan(append(d.ptrs(), &dept, &pos, &posName, &label, &starts, &ends, &zone, &location, &shiftCanceled)...); err != nil {
		return vol.SignupDetail{}, err //nolint:wrapcheck // translate classifies it
	}
	return vol.SignupDetail{
		Signup: d.signup(), Department: model.DepartmentID(dept), Position: vol.PositionID(pos), PositionName: posName,
		Label: label, Starts: starts.UTC(), Ends: ends.UTC(), Zone: zone, Location: location, ShiftCanceledAt: timeOf(shiftCanceled),
	}, nil
}

// GetSignup returns a sign-up with its shift's essentials. Fails errs.NotFound.
func (s *Store) GetSignup(ctx context.Context, id vol.SignupID) (vol.SignupDetail, error) {
	return queryOne(ctx, s, "volunteers.postgres.GetSignup", sqlGetSignup, scanSignupDetail, [16]byte(id))
}

// GetSignupForUpdate is GetSignup with a lock on the sign-up. Fails errs.NotFound.
func (s *Store) GetSignupForUpdate(ctx context.Context, id vol.SignupID) (vol.SignupDetail, error) {
	return queryOne(ctx, s, "volunteers.postgres.GetSignupForUpdate", sqlGetSignupForUpdate, scanSignupDetail, [16]byte(id))
}

// CancelSignup cancels a booked sign-up. Fails errs.NotFound when it is no longer booked.
func (s *Store) CancelSignup(ctx context.Context, id vol.SignupID, by model.PersonID, reason vol.CancelReason, at time.Time) (vol.Signup, error) {
	return queryOne(ctx, s, "volunteers.postgres.CancelSignup", sqlCancelSignup, scanSignup, [16]byte(id), [16]byte(by), string(reason), at)
}

// Roster lists a shift's booked sign-ups, oldest first.
func (s *Store) Roster(ctx context.Context, shift vol.ShiftID) ([]vol.Signup, error) {
	return queryAll(ctx, s, "volunteers.postgres.Roster", sqlRoster, scanSignup, [16]byte(shift))
}

// Mates lists the other booked people on a shift and whether each is listed, in person-ID order.
func (s *Store) Mates(ctx context.Context, shift vol.ShiftID, actor model.PersonID) ([]vol.Mate, error) {
	return queryAll(ctx, s, "volunteers.postgres.Mates", sqlMates, func(r rowScanner) (vol.Mate, error) {
		var p [16]byte
		var listed bool
		if err := r.Scan(&p, &listed); err != nil {
			return vol.Mate{}, err //nolint:wrapcheck // translate classifies it
		}
		return vol.Mate{Person: model.PersonID(p), Listed: listed}, nil
	}, [16]byte(shift), [16]byte(actor))
}

// Schedule lists up to f.Limit of a person's bookings in the mode f.Past selects (upcoming:
// soonest first, canceled-by-others included; past: booked only, newest first), after the
// keyset cursor (f.AfterStarts, f.AfterID, a sign-up ID) when f.AfterStarts is set.
func (s *Store) Schedule(ctx context.Context, f vol.ScheduleFilter) ([]vol.ScheduleRow, error) {
	var afterStarts, afterID any // nil = the first page
	if !f.AfterStarts.IsZero() {
		afterStarts, afterID = f.AfterStarts, [16]byte(f.AfterID)
	}
	q := sqlSchedule
	if f.Past {
		q = sqlSchedulePast
	}
	return queryAll(ctx, s, "volunteers.postgres.Schedule", q, func(r rowScanner) (vol.ScheduleRow, error) {
		var (
			su      signupDest
			sh      shiftDest
			posName string
		)
		dest := append(su.ptrs(), sh.ptrs()...)
		if err := r.Scan(append(dest, &posName)...); err != nil {
			return vol.ScheduleRow{}, err //nolint:wrapcheck // translate classifies it
		}
		return vol.ScheduleRow{Signup: su.signup(), Shift: sh.shift(), PositionName: posName}, nil
	}, [16]byte(f.Person), f.Now, tsArg(f.Within.Start), tsArg(f.Within.End), afterStarts, afterID,
		int32(f.Limit)) //nolint:gosec // bounded by vol.MaxList + 1
}

// BookedWindows lists the time windows of a person's booked, non-canceled shifts that end
// after within.Start and start before within.End (when set).
func (s *Store) BookedWindows(ctx context.Context, person model.PersonID, within model.TimeRange) ([]vol.BookedWindow, error) {
	return queryAll(ctx, s, "volunteers.postgres.BookedWindows", sqlBookedWindows, func(r rowScanner) (vol.BookedWindow, error) {
		var id [16]byte
		var starts, ends time.Time
		if err := r.Scan(&id, &starts, &ends); err != nil {
			return vol.BookedWindow{}, err //nolint:wrapcheck // translate classifies it
		}
		return vol.BookedWindow{Shift: vol.ShiftID(id), Starts: starts.UTC(), Ends: ends.UTC()}, nil
	}, [16]byte(person), within.Start, tsArg(within.End), int32(vol.MaxList))
}

// Listed reports whether a person is listed in shift-mate lists (no unlisted row).
func (s *Store) Listed(ctx context.Context, person model.PersonID) (bool, error) {
	var listed bool
	if err := s.db.QueryRow(ctx, sqlGetListed, [16]byte(person)).Scan(&listed); err != nil {
		return false, translate("volunteers.postgres.Listed", err)
	}
	return listed, nil
}

// SetUnlisted records that a person chose not to be listed; changed is false when they
// already were unlisted.
func (s *Store) SetUnlisted(ctx context.Context, person model.PersonID, at time.Time) (bool, error) {
	n, err := s.exec(ctx, "volunteers.postgres.SetUnlisted", sqlInsertUnlisted, [16]byte(person), at)
	return n > 0, err
}

// SetListedTrue removes a person's unlisted row; changed is false when there was none.
func (s *Store) SetListedTrue(ctx context.Context, person model.PersonID) (bool, error) {
	n, err := s.exec(ctx, "volunteers.postgres.SetListedTrue", sqlDeleteUnlisted, [16]byte(person))
	return n > 0, err
}

// UpcomingCounts counts the upcoming bookings of each person, limited to departments unless all.
func (s *Store) UpcomingCounts(ctx context.Context, people []model.PersonID, now time.Time, all bool, departments []model.DepartmentID) (map[model.PersonID]int, error) {
	type pair struct {
		p model.PersonID
		n int
	}
	rows, err := queryAll(ctx, s, "volunteers.postgres.UpcomingCounts", sqlUpcomingCounts, func(r rowScanner) (pair, error) {
		var p [16]byte
		var n int64
		if err := r.Scan(&p, &n); err != nil {
			return pair{}, err //nolint:wrapcheck // translate classifies it
		}
		return pair{model.PersonID(p), int(n)}, nil
	}, uuids(people), now, all, uuids(departments))
	if err != nil {
		return nil, err
	}
	out := make(map[model.PersonID]int, len(rows))
	for _, r := range rows {
		out[r.p] = r.n
	}
	return out, nil
}

// ---- retention ----

// PurgeSignups deletes one batch of sign-ups whose shift ended before the cutoff.
func (s *Store) PurgeSignups(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	return s.exec(ctx, "volunteers.postgres.PurgeSignups", sqlPurgeSignups, cutoff, int32(limit)) //nolint:gosec // bounded batch
}

// PurgeCanceledShifts deletes one batch of shifts canceled before the cutoff with no sign-up left.
func (s *Store) PurgeCanceledShifts(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	return s.exec(ctx, "volunteers.postgres.PurgeCanceledShifts", sqlPurgeCanceledShifts, cutoff, int32(limit)) //nolint:gosec // bounded batch
}

// ---- one person's data ----

// ExportRecords lists every qualification record of a person.
func (s *Store) ExportRecords(ctx context.Context, person model.PersonID) ([]vol.RecordExport, error) {
	return queryAll(ctx, s, "volunteers.postgres.ExportRecords", sqlExportRecords, func(r rowScanner) (vol.RecordExport, error) {
		var (
			id       [16]byte
			name     string
			acq      time.Time
			exp, at  *time.Time
			verified bool
			cert     *[16]byte
		)
		if err := r.Scan(&id, &name, &acq, &exp, &verified, &at, &cert); err != nil {
			return vol.RecordExport{}, err //nolint:wrapcheck // translate classifies it
		}
		return vol.RecordExport{
			ID: vol.RecordID(id), QualificationName: name, AcquiredOn: vol.DateOf(acq, time.UTC), ExpiresOn: dateOf(exp),
			Verified: verified, VerifiedAt: timeOf(at), Certificate: idOf[files.FileID](cert),
		}, nil
	}, [16]byte(person))
}

// ExportSignups lists every sign-up of a person with role words for who acted.
func (s *Store) ExportSignups(ctx context.Context, person model.PersonID) ([]vol.SignupExport, error) {
	return queryAll(ctx, s, "volunteers.postgres.ExportSignups", sqlExportSignups, func(r rowScanner) (vol.SignupExport, error) {
		var (
			label, posName, zone, status, bookedRole string
			dept                                     [16]byte
			starts, ends, created                    time.Time
			waived                                   bool
			canceledAt                               *time.Time
			canceledRole, reason                     *string
		)
		if err := r.Scan(&label, &dept, &posName, &starts, &ends, &zone, &status, &bookedRole, &waived, &canceledAt, &canceledRole, &reason, &created); err != nil {
			return vol.SignupExport{}, err //nolint:wrapcheck // translate classifies it
		}
		e := vol.SignupExport{
			Label: label, Department: model.DepartmentID(dept), PositionName: posName, Starts: starts.UTC(), Ends: ends.UTC(),
			Zone: zone, Status: vol.SignupStatus(status), BookedByRole: bookedRole, Waived: waived,
			CanceledAt: timeOf(canceledAt), CreatedAt: created.UTC(),
		}
		if canceledRole != nil {
			e.CanceledByRole = *canceledRole
		}
		if reason != nil {
			e.CancelReason = vol.CancelReason(*reason)
		}
		return e, nil
	}, [16]byte(person))
}

// CountVerifiedBy counts the records a person verified.
func (s *Store) CountVerifiedBy(ctx context.Context, person model.PersonID) (int, error) {
	return s.count(ctx, "volunteers.postgres.CountVerifiedBy", sqlCountVerifiedBy, [16]byte(person))
}

// CountShiftsCreatedBy counts the shifts a person created.
func (s *Store) CountShiftsCreatedBy(ctx context.Context, person model.PersonID) (int, error) {
	return s.count(ctx, "volunteers.postgres.CountShiftsCreatedBy", sqlCountShiftsCreatedBy, [16]byte(person))
}

// RecordIDsOf lists a person's records with their certificate file IDs.
func (s *Store) RecordIDsOf(ctx context.Context, person model.PersonID) ([]vol.ExpiredRecord, error) {
	return queryAll(ctx, s, "volunteers.postgres.RecordIDsOf", sqlRecordIDsOf, scanExpired, [16]byte(person))
}

// DeleteSignupsByPerson deletes a person's sign-ups.
func (s *Store) DeleteSignupsByPerson(ctx context.Context, person model.PersonID) (int, error) {
	return s.exec(ctx, "volunteers.postgres.DeleteSignupsByPerson", sqlDeleteSignupsByPerson, [16]byte(person))
}

// DeleteMembersByPerson deletes a person's memberships.
func (s *Store) DeleteMembersByPerson(ctx context.Context, person model.PersonID) (int, error) {
	return s.exec(ctx, "volunteers.postgres.DeleteMembersByPerson", sqlDeleteMembersByPerson, [16]byte(person))
}

// DeleteUnlistedByPerson deletes a person's unlisted row.
func (s *Store) DeleteUnlistedByPerson(ctx context.Context, person model.PersonID) (int, error) {
	return s.exec(ctx, "volunteers.postgres.DeleteUnlistedByPerson", sqlDeleteUnlistedPerson, [16]byte(person))
}

// DeleteRecordsByPerson deletes a person's records.
func (s *Store) DeleteRecordsByPerson(ctx context.Context, person model.PersonID) (int, error) {
	return s.exec(ctx, "volunteers.postgres.DeleteRecordsByPerson", sqlDeleteRecordsPerson, [16]byte(person))
}

// AnonymisePerson nulls every who-did-it column that names a person and counts the rows.
func (s *Store) AnonymisePerson(ctx context.Context, person model.PersonID) (vol.Anonymised, error) {
	return queryOne(ctx, s, "volunteers.postgres.AnonymisePerson", sqlAnonymisePerson, func(r rowScanner) (vol.Anonymised, error) {
		var rec, mem, su, sh int64
		if err := r.Scan(&rec, &mem, &su, &sh); err != nil {
			return vol.Anonymised{}, err //nolint:wrapcheck // translate classifies it
		}
		return vol.Anonymised{Records: int(rec), Members: int(mem), Signups: int(su), Shifts: int(sh)}, nil
	}, [16]byte(person))
}
