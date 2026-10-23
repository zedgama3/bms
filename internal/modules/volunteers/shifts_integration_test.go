//go:build integration

package volunteers_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/contracts/eventsetup"
	"github.com/zedgama3/bms/internal/contracts/personpage"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

var errPeopleDown = errors.New("people is down")

var (
	burnStart = time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	burnEnd   = time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
)

func burnDates() (time.Time, time.Time) {
	return time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
}

// ---- visibility (F66-F68) ----

// F66
func TestListShifts_CanceledWithoutPermission_ReturnsPermission(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	plain := r.as(r.volunteer("Tumbleweed"))
	_, err := shiftItems(r.svc.ListShifts(plain, vol.ShiftQuery{Department: s.dept, IncludeCanceled: true}))
	wantKind(t, err, errs.Permission)
	_, err = shiftItems(r.svc.ListShifts(plain, vol.ShiftQuery{Department: s.dept, Within: model.TimeRange{Start: now.Add(-48 * time.Hour), End: now.Add(-time.Hour)}}))
	wantKind(t, err, errs.Permission)
	got, err := shiftItems(r.svc.ListShifts(r.as(s.lead), vol.ShiftQuery{Department: s.dept, IncludeCanceled: true}))
	wantNoErr(t, err)
	if len(got) != 1 {
		t.Errorf("the lead sees %d shifts, want 1", len(got))
	}
	// The permission is per extent: a lead of another department does not qualify for d.
	// A lead of another department has an extent, so the call is allowed, but it must
	// show nothing canceled or past of this department.
	other := r.lead(r.department("Greeters"))
	wantNoErr(t, r.svc.CancelShift(r.as(r.boss), s.sh.ID))
	got, err = shiftItems(r.svc.ListShifts(r.as(other), vol.ShiftQuery{Department: s.dept, IncludeCanceled: true}))
	if err != nil {
		wantKind(t, err, errs.Permission)
	} else if len(got) != 0 {
		t.Errorf("a lead of another department sees %d canceled shifts of this one", len(got))
	}
}

// F67
func TestListShifts_HidesMembersOnlyForNonMembers(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	open := r.position(d, "Dirt crew", vol.AccessOpen)
	inner := r.position(d, "Inner circle", vol.AccessMembers)
	shOpen := r.shift(d, open.ID, model.EventID{}, when(0), 2, 2)
	shInner := r.shift(d, inner.ID, model.EventID{}, when(5), 2, 2)
	member, outsider := r.volunteer("Dandelion"), r.volunteer("Tumbleweed")
	r.member(d, member)
	ids := func(p model.PersonID) map[vol.ShiftID]bool {
		got, err := shiftItems(r.svc.ListShifts(r.as(p), vol.ShiftQuery{Department: d}))
		wantNoErr(t, err)
		out := map[vol.ShiftID]bool{}
		for _, s := range got {
			out[s.ID] = true
		}
		return out
	}
	if got := ids(outsider); !got[shOpen.ID] || got[shInner.ID] {
		t.Errorf("outsider sees %v, want the open shift only", got)
	}
	if got := ids(member); !got[shOpen.ID] || !got[shInner.ID] {
		t.Errorf("member sees %v, want both", got)
	}
	if got := ids(r.lead(d)); !got[shInner.ID] {
		t.Error("the department's lead must see its members-only shifts")
	}
}

// F68
func TestGetShift_MembersOnlyNonMember_ReturnsNotFound(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	inner := r.position(d, "Inner circle", vol.AccessMembers)
	sh := r.shift(d, inner.ID, model.EventID{}, when(0), 2, 2)
	_, err := r.svc.GetShift(r.as(r.volunteer("Tumbleweed")), sh.ID)
	wantKind(t, err, errs.NotFound) // never Permission: a 403 would confirm the ID exists
	member := r.volunteer("Dandelion")
	r.member(d, member)
	got, err := r.svc.GetShift(r.as(member), sh.ID)
	wantNoErr(t, err)
	if got.ID != sh.ID {
		t.Errorf("GetShift = %+v", got.Shift)
	}
	_, err = r.svc.GetShift(r.as(member), model.NewID[vol.ShiftID]())
	wantKind(t, err, errs.NotFound)
}

// Spec, ListShifts: "open-access positions of active departments for everyone".
func TestGetShift_ArchivedDepartment_HiddenFromVolunteers(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	r.archiveDepartment(s.dept)
	plain := r.as(r.volunteer("Tumbleweed"))
	_, err := r.svc.GetShift(plain, s.sh.ID)
	wantKind(t, err, errs.NotFound)
	got, err := shiftItems(r.svc.ListShifts(plain, vol.ShiftQuery{Department: s.dept}))
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Errorf("ListShifts shows %d shifts of an archived department to a volunteer", len(got))
	}
}

func TestGetShift_Canceled_HiddenFromVolunteers(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	wantNoErr(t, r.svc.CancelShift(r.as(r.boss), s.sh.ID))
	plain := r.as(r.volunteer("Tumbleweed"))
	_, err := r.svc.GetShift(plain, s.sh.ID)
	wantKind(t, err, errs.NotFound)
	got, err := r.svc.GetShift(r.as(s.lead), s.sh.ID)
	wantNoErr(t, err)
	if !got.Canceled() {
		t.Error("the lead's view of a canceled shift is not marked canceled")
	}
}

func TestListShifts_ComputesBookedMineAndEligibility(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	open := r.position(d, "Dirt crew", vol.AccessOpen)
	radio := r.position(d, "Radio op", vol.AccessOpen, q.ID)
	full := r.shift(d, open.ID, model.EventID{}, when(0), 2, 1)
	needs := r.shift(d, radio.ID, model.EventID{}, when(10), 2, 3)
	overlap := r.shift(d, open.ID, model.EventID{}, when(1), 2, 3) // overlaps `full`
	a, b := r.volunteer("Dandelion"), r.volunteer("Tumbleweed")
	r.book(a, full.ID)
	got, err := shiftItems(r.svc.ListShifts(r.as(b), vol.ShiftQuery{Department: d}))
	wantNoErr(t, err)
	by := map[vol.ShiftID]vol.ShiftSummary{}
	for _, s := range got {
		by[s.ID] = s
		if s.Mates != nil {
			t.Error("ListShifts must never fill Mates")
		}
	}
	if by[full.ID].Booked != 1 || by[full.ID].Eligibility.Eligible || by[full.ID].Eligibility.Reason != "full" {
		t.Errorf("full shift = %+v", by[full.ID].Eligibility)
	}
	e := by[needs.ID].Eligibility
	if e.Eligible || len(e.Missing) != 1 || e.Missing[0] != q.ID {
		t.Errorf("radio shift eligibility = %+v, want missing the radio qualification", e)
	}
	if !by[overlap.ID].Eligibility.Eligible {
		t.Errorf("b has no bookings, so the overlapping shift is open to b: %+v", by[overlap.ID].Eligibility)
	}
	r.book(b, overlap.ID)
	got, err = shiftItems(r.svc.ListShifts(r.as(b), vol.ShiftQuery{Department: d}))
	wantNoErr(t, err)
	for _, s := range got {
		if s.ID == overlap.ID && s.Mine.IsZero() {
			t.Error("Mine is not set on the shift the actor booked")
		}
		if s.ID == full.ID && !s.Mine.IsZero() {
			t.Error("Mine is set on a shift the actor did not book")
		}
	}
	// The actor already holds `overlap`; `full` overlaps it.
	for _, s := range got {
		if s.ID == full.ID && s.Eligibility.Reason != "full" && s.Eligibility.Reason != "overlap" {
			t.Errorf("reason = %q", s.Eligibility.Reason)
		}
	}
}

func TestListShifts_FiltersAndLimit(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	ev := r.event(burnStart, burnEnd, "UTC", false)
	evShift := r.shift(d, pos.ID, ev, when(0), 2, 1)
	r.shift(d, pos.ID, model.EventID{}, when(10), 2, 1)
	r.shift(d, pos.ID, model.EventID{}, when(20), 2, 1)
	plain := r.as(r.volunteer("Tumbleweed"))
	got, err := shiftItems(r.svc.ListShifts(plain, vol.ShiftQuery{Department: d, Event: ev}))
	wantNoErr(t, err)
	if len(got) != 1 || got[0].ID != evShift.ID {
		t.Errorf("event filter returned %d shifts", len(got))
	}
	got, err = shiftItems(r.svc.ListShifts(plain, vol.ShiftQuery{Department: d, Page: vol.Page{Limit: 2}}))
	wantNoErr(t, err)
	if len(got) != 2 {
		t.Errorf("limit 2 returned %d", len(got))
	}
	if _, err := shiftItems(r.svc.ListShifts(plain, vol.ShiftQuery{Page: vol.Page{Limit: vol.MaxList + 1}})); !errs.Is(err, errs.Invalid) {
		t.Errorf("limit above MaxList = %v, want Invalid", err)
	}
	if _, err := shiftItems(r.svc.ListShifts(plain, vol.ShiftQuery{Page: vol.Page{Limit: -1}})); !errs.Is(err, errs.Invalid) {
		t.Errorf("negative limit = %v, want Invalid", err)
	}
}

// ---- create (F69-F76) ----

func TestCreateShift_Lead_CreatesAndAudits(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	lead := r.lead(d)
	got, err := r.svc.CreateShift(r.as(lead), d, vol.ShiftInput{
		Position: pos.ID, Label: "Morning dirt", Starts: when(0), Ends: when(4), Zone: "America/Denver", Slots: 5, Location: "Ranger HQ",
	})
	wantNoErr(t, err)
	if got.Department != d || got.Position != pos.ID || got.Slots != 5 || got.Zone != "America/Denver" || got.CreatedBy != lead {
		t.Errorf("shift = %+v", got)
	}
	if len(r.audited("volunteers.shift.created")) != 1 {
		t.Error("no shift.created audit entry")
	}
}

// F70
func TestCreateShift_ArchivedDepartment_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	r.archiveDepartment(s.dept)
	_, err := r.svc.CreateShift(r.as(r.boss), s.dept, vol.ShiftInput{Position: s.pos.ID, Label: "x", Starts: when(0), Ends: when(2), Zone: "UTC", Slots: 1})
	wantConflict(t, err, "archived")
}

// F71
func TestCreateShift_Position_Table(t *testing.T) {
	r := newReal(t)
	d, other := r.department("Rangers"), r.department("Greeters")
	foreign := r.position(other, "Door crew", vol.AccessOpen)
	retired := r.position(d, "Old crew", vol.AccessOpen)
	wantNoErr(t, r.svc.ArchivePosition(r.as(r.boss), retired.ID))
	tests := []struct {
		name string
		pos  vol.PositionID
		kind errs.Kind
		code string
	}{
		{"another department's", foreign.ID, errs.Invalid, "scope"},
		{"archived", retired.ID, errs.Conflict, "archived"},
		{"unknown", model.NewID[vol.PositionID](), errs.Invalid, "unknown"},
		{"zero", vol.PositionID{}, errs.Invalid, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.svc.CreateShift(r.as(r.boss), d, vol.ShiftInput{Position: tc.pos, Label: "x", Starts: when(0), Ends: when(2), Zone: "UTC", Slots: 1})
			wantKind(t, err, tc.kind)
			if tc.code != "" && codeOf(err) != tc.code {
				t.Errorf("code = %q, want %q", codeOf(err), tc.code)
			}
		})
	}
}

// F72
func TestCreateShift_UnknownEvent_ReturnsInvalid(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	_, err := r.svc.CreateShift(r.as(r.boss), s.dept, vol.ShiftInput{Position: s.pos.ID, Event: model.NewID[model.EventID](), Label: "x", Starts: when(0), Ends: when(2), Slots: 1})
	wantInvalid(t, err, "unknown")
}

// F73
func TestCreateShift_ClosedEvent_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	s0, e0 := burnDates()
	ev := r.event(s0, e0, "UTC", true)
	_, err := r.svc.CreateShift(r.as(r.boss), s.dept, vol.ShiftInput{Position: s.pos.ID, Event: ev, Label: "x", Starts: when(0), Ends: when(2), Slots: 1})
	wantConflict(t, err, "closed")
}

func TestCreateShift_EventSetsTheZone(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	s0, e0 := burnDates()
	ev := r.event(s0, e0, "America/Denver", false)
	got, err := r.svc.CreateShift(r.as(r.boss), s.dept, vol.ShiftInput{Position: s.pos.ID, Event: ev, Label: "x", Starts: when(0), Ends: when(2), Zone: "Asia/Tokyo", Slots: 1})
	wantNoErr(t, err)
	if got.Zone != "America/Denver" || got.Event != ev {
		t.Errorf("zone/event = %q/%v, want the event's zone (the input zone is ignored)", got.Zone, got.Event)
	}
}

// F74
func TestCreateShift_InvalidInput_Table(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	base := vol.ShiftInput{Position: s.pos.ID, Label: "Morning", Starts: when(0), Ends: when(4), Zone: "UTC", Slots: 2}
	tests := []struct {
		name string
		mod  func(*vol.ShiftInput)
		code string
	}{
		{"ends equals starts", func(in *vol.ShiftInput) { in.Ends = in.Starts }, "range"},
		{"ends before starts", func(in *vol.ShiftInput) { in.Ends = in.Starts.Add(-time.Hour) }, "range"},
		{"longer than 48h", func(in *vol.ShiftInput) { in.Ends = in.Starts.Add(vol.MaxShiftDuration + time.Minute) }, "range"},
		{"exactly 48h is fine", func(in *vol.ShiftInput) { in.Ends = in.Starts.Add(vol.MaxShiftDuration) }, ""},
		{"zero slots", func(in *vol.ShiftInput) { in.Slots = 0 }, "range"},
		{"too many slots", func(in *vol.ShiftInput) { in.Slots = vol.MaxSlots + 1 }, "range"},
		{"max slots is fine", func(in *vol.ShiftInput) { in.Slots = vol.MaxSlots }, ""},
		{"empty label", func(in *vol.ShiftInput) { in.Label = "" }, "required"},
		{"label too long", func(in *vol.ShiftInput) { in.Label = strings.Repeat("l", vol.MaxLabelBytes+1) }, "too_long"},
		{"location too long", func(in *vol.ShiftInput) { in.Location = strings.Repeat("l", vol.MaxLocationBytes+1) }, "too_long"},
		{"notes too long", func(in *vol.ShiftInput) { in.Notes = strings.Repeat("n", vol.MaxDescriptionBytes+1) }, "too_long"},
		{"control chars in label", func(in *vol.ShiftInput) { in.Label = "a\x07b" }, "control_chars"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.mod(&in)
			_, err := r.svc.CreateShift(r.as(r.boss), s.dept, in)
			if tc.code == "" {
				wantNoErr(t, err)
				return
			}
			wantInvalid(t, err, tc.code)
		})
	}
}

// F75
func TestCreateShift_StandingWorkBadZone_ReturnsInvalid(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	in := vol.ShiftInput{Position: s.pos.ID, Label: "Winter inventory", Starts: when(0), Ends: when(2), Slots: 1}
	_, err := r.svc.CreateShift(r.as(r.boss), s.dept, in)
	wantInvalid(t, err, "required")
	in.Zone = "Mars/Olympus_Mons"
	_, err = r.svc.CreateShift(r.as(r.boss), s.dept, in)
	wantInvalid(t, err, "malformed")
}

// F76
func TestCreateShift_EventsFail_ReturnsError(t *testing.T) {
	r := newReal(t, func(o *opts) {
		o.wrapEvents = func(d eventsDirectory) eventsDirectory {
			return failEvents{Directory: d, err: errs.E("test.events", errs.Unavailable, errors.New("events down"))}
		}
	})
	s := r.scenario()
	s0, e0 := burnDates()
	ev := r.event(s0, e0, "UTC", false)
	_, err := r.svc.CreateShift(r.as(r.boss), s.dept, vol.ShiftInput{Position: s.pos.ID, Event: ev, Label: "x", Starts: when(0), Ends: when(2), Slots: 1})
	wantKind(t, err, errs.Unavailable)
}

// ---- update (F77-F80) ----

// F77
func TestUpdateShift_Unknown_ReturnsNotFound(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	in := vol.ShiftInput{Position: s.pos.ID, Label: "x", Starts: when(0), Ends: when(2), Zone: "UTC", Slots: 1}
	_, err := r.svc.UpdateShift(r.as(r.boss), model.NewID[vol.ShiftID](), in)
	wantKind(t, err, errs.NotFound)
	_, err = r.svc.UpdateShift(r.as(r.lead(r.department("Greeters"))), s.sh.ID, in)
	wantKind(t, err, errs.Permission)
}

func shiftInputOf(sh vol.Shift) vol.ShiftInput {
	return vol.ShiftInput{Position: sh.Position, Event: sh.Event, Label: sh.Label, Starts: sh.Starts, Ends: sh.Ends, Zone: sh.Zone, Slots: sh.Slots, Location: sh.Location, Notes: sh.Notes}
}

// F78
func TestUpdateShift_Canceled_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	wantNoErr(t, r.svc.CancelShift(r.as(s.lead), s.sh.ID))
	_, err := r.svc.UpdateShift(r.as(s.lead), s.sh.ID, shiftInputOf(s.sh))
	wantConflict(t, err, "canceled")
}

// F79
func TestUpdateShift_SlotsBelowBooked_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	s := r.scenario() // 3 slots
	r.book(r.volunteer("Dandelion"), s.sh.ID)
	r.book(r.volunteer("Tumbleweed"), s.sh.ID)
	in := shiftInputOf(s.sh)
	in.Slots = 1
	_, err := r.svc.UpdateShift(r.as(s.lead), s.sh.ID, in)
	wantConflict(t, err, "full")
	in.Slots = 2
	got, err := r.svc.UpdateShift(r.as(s.lead), s.sh.ID, in)
	wantNoErr(t, err)
	if got.Slots != 2 {
		t.Errorf("slots = %d", got.Slots)
	}
	if len(r.audited("volunteers.shift.changed")) != 1 {
		t.Error("no shift.changed audit entry")
	}
}

// F80
func TestUpdateShift_CreatesOverlap_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	a := r.shift(d, pos.ID, model.EventID{}, when(0), 2, 2)
	b := r.shift(d, pos.ID, model.EventID{}, when(3), 2, 2)
	p := r.volunteer("Dandelion")
	r.book(p, a.ID)
	r.book(p, b.ID)
	in := shiftInputOf(b)
	in.Starts, in.Ends = when(1), when(4) // now overlaps a for p
	_, err := r.svc.UpdateShift(r.as(r.boss), b.ID, in)
	wantConflict(t, err, "overlap")
	var fe *validate.FieldError
	if errors.As(err, &fe) && fe.Params["shift"] == "" {
		t.Errorf("overlap carries no {shift} param: %+v", fe.Params)
	}
}

func TestUpdateShift_TimeChange_MovesEveryReminder(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p, q := r.volunteer("Dandelion"), r.volunteer("Tumbleweed")
	a, b := r.book(p, s.sh.ID), r.book(q, s.sh.ID)
	in := shiftInputOf(s.sh)
	in.Starts, in.Ends = when(24), when(28)
	_, err := r.svc.UpdateShift(r.as(s.lead), s.sh.ID, in)
	wantNoErr(t, err)
	byKey := map[string]jobs.Request{}
	for _, req := range r.jobs.Enqueued() {
		if string(req.Name) == volunteers.JobShiftReminder {
			byKey[req.Key] = req // the later request for a key replaces the earlier (Key reschedule semantics)
		}
	}
	want := when(24).Add(-24 * time.Hour) // reminder_lead defaults to 24h
	for _, su := range []vol.Signup{a, b} {
		req, ok := byKey[su.ID.String()]
		if !ok || !req.RunAt.Equal(want) {
			t.Errorf("reminder for %s = %+v, want RunAt %v", su.ID, req, want)
		}
	}
}

// ---- cancel (F81-F83) ----

// F81: the shift is open-access, so visible to everyone: lacking shifts.manage@D is Permission.
func TestCancelShift_VisibleNotLead_ReturnsPermission(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	wantKind(t, r.svc.CancelShift(r.as(r.lead(r.department("Greeters"))), s.sh.ID), errs.Permission)
	wantKind(t, r.svc.CancelShift(r.as(r.volunteer("Tumbleweed")), s.sh.ID), errs.Permission)
	wantKind(t, r.svc.CancelShift(r.as(r.boss), model.NewID[vol.ShiftID]()), errs.NotFound)
}

func TestCancelShift_CancelsBookingsNotifiesAndPublishes(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p, q := r.volunteer("Dandelion"), r.volunteer("Tumbleweed")
	su := r.book(p, s.sh.ID)
	r.book(q, s.sh.ID)
	wantNoErr(t, r.svc.CancelShift(r.as(s.lead), s.sh.ID))
	if n := len(r.published(volunteers.SignalSignupCanceled)); n != 2 {
		t.Errorf("%d signup-canceled signals, want one per sign-up (2)", n)
	}
	sent := r.mail.Sent()
	if len(sent) != 2 {
		t.Fatalf("%d notifications, want 2", len(sent))
	}
	for _, m := range sent {
		if string(m.Template) != "volunteers.signup-removed" || len(m.To) != 1 {
			t.Errorf("message = %+v", m)
		}
	}
	if sent[0].Key != "removed:"+su.ID.String() && sent[1].Key != "removed:"+su.ID.String() {
		t.Error("no notification is keyed removed:<signup>")
	}
	// Q-VOL9 (F153): the booking stays on the schedule, marked, until the shift ends.
	mine, err := scheduleItems(r.svc.MySchedule(r.as(p), vol.ScheduleQuery{}))
	wantNoErr(t, err)
	if len(mine) != 1 || mine[0].Status != vol.SignupCanceled || mine[0].CancelReason != vol.CancelShiftCanceled {
		t.Errorf("schedule = %+v, want the one booking canceled with reason shift_canceled", mine)
	}
	if len(r.audited("volunteers.shift.canceled")) != 1 {
		t.Error("no shift.canceled audit entry")
	}
}

// F82
func TestCancelShift_Twice_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	wantNoErr(t, r.svc.CancelShift(r.as(s.lead), s.sh.ID))
	wantConflict(t, r.svc.CancelShift(r.as(s.lead), s.sh.ID), "canceled")
}

// F83
func TestCancelShift_NotifyUnsupported_Succeeds(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	r.book(r.volunteer("Dandelion"), s.sh.ID)
	r.mail.SetConfigured(false)
	wantNoErr(t, r.svc.CancelShift(r.as(s.lead), s.sh.ID))
}

// ---- copy and set-up (F84-F88, F119) ----

type copyWorld struct {
	r         *rig
	d         model.DepartmentID
	pos       vol.Position
	from, to  model.EventID
	fromStart time.Time
}

func newCopyWorld(t *testing.T) copyWorld {
	t.Helper()
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	from := r.event(time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), "UTC", false)
	to := r.event(time.Date(2027, 11, 4, 0, 0, 0, 0, time.UTC), time.Date(2027, 11, 8, 0, 0, 0, 0, time.UTC), "UTC", false)
	return copyWorld{r: r, d: d, pos: pos, from: from, to: to, fromStart: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)}
}

// F84
func TestCopyShifts_NotLead_ReturnsPermission(t *testing.T) {
	w := newCopyWorld(t)
	_, err := w.r.svc.CopyShifts(w.r.as(w.r.lead(w.r.department("Greeters"))), w.d, w.from, w.to)
	wantKind(t, err, errs.Permission)
}

// F85
func TestCopyShifts_SameEvent_ReturnsInvalid(t *testing.T) {
	w := newCopyWorld(t)
	_, err := w.r.svc.CopyShifts(w.r.as(w.r.boss), w.d, w.from, w.from)
	wantInvalid(t, err, "self")
	_, err = w.r.svc.CopyShifts(w.r.as(w.r.boss), w.d, w.from, model.NewID[model.EventID]())
	wantInvalid(t, err, "unknown")
}

// F86
func TestCopyShifts_ClosedTarget_ReturnsConflict(t *testing.T) {
	w := newCopyWorld(t)
	w.r.closeEvent(w.to)
	_, err := w.r.svc.CopyShifts(w.r.as(w.r.boss), w.d, w.from, w.to)
	wantConflict(t, err, "closed")
}

func TestCopyShifts_ShiftsTimesByTheEventsStartDifference(t *testing.T) {
	w := newCopyWorld(t)
	src := w.r.shift(w.d, w.pos.ID, w.from, w.fromStart.Add(10*time.Hour), 4, 3)
	canceled := w.r.shift(w.d, w.pos.ID, w.from, w.fromStart.Add(30*time.Hour), 4, 3)
	wantNoErr(t, w.r.svc.CancelShift(w.r.as(w.r.boss), canceled.ID))
	p := w.r.volunteer("Dandelion")
	w.r.book(p, src.ID)

	lead := w.r.lead(w.d)
	n, err := w.r.svc.CopyShifts(w.r.as(lead), w.d, w.from, w.to)
	wantNoErr(t, err)
	if n != 1 {
		t.Fatalf("copied %d shifts, want 1 (canceled ones are not copied)", n)
	}
	got, err := shiftItems(w.r.svc.ListShifts(w.r.as(lead), vol.ShiftQuery{Department: w.d, Event: w.to}))
	wantNoErr(t, err)
	if len(got) != 1 {
		t.Fatalf("%d shifts in the target event", len(got))
	}
	c := got[0]
	delta := time.Date(2027, 11, 4, 0, 0, 0, 0, time.UTC).Sub(w.fromStart)
	if !c.Starts.Equal(src.Starts.Add(delta)) || !c.Ends.Equal(src.Ends.Add(delta)) {
		t.Errorf("copied times %v-%v, want the source shifted by %v", c.Starts, c.Ends, delta)
	}
	if c.CopiedFrom != src.ID || c.Event != w.to || c.Slots != src.Slots || c.Label != src.Label || c.Position != src.Position {
		t.Errorf("copy = %+v", c.Shift)
	}
	if c.Booked != 0 {
		t.Errorf("sign-ups were carried to the new event (Booked = %d)", c.Booked)
	}
	if len(w.r.audited("volunteers.shift.copied")) == 0 {
		t.Error("no shift.copied audit entry")
	}
}

// F87
func TestCopyShifts_SkipsArchivedPositions(t *testing.T) {
	w := newCopyWorld(t)
	retired := w.r.position(w.d, "Old crew", vol.AccessOpen)
	w.r.shift(w.d, w.pos.ID, w.from, w.fromStart.Add(10*time.Hour), 2, 1)
	old := w.r.shift(w.d, retired.ID, w.from, w.fromStart.Add(20*time.Hour), 2, 1)
	wantNoErr(t, w.r.svc.CancelShift(w.r.as(w.r.boss), old.ID)) // archiving needs no future non-canceled shifts
	wantNoErr(t, w.r.svc.ArchivePosition(w.r.as(w.r.boss), retired.ID))
	live := w.r.shift(w.d, w.pos.ID, w.from, w.fromStart.Add(40*time.Hour), 2, 1)
	_ = live
	n, err := w.r.svc.CopyShifts(w.r.as(w.r.boss), w.d, w.from, w.to)
	wantNoErr(t, err)
	if n != 2 {
		t.Errorf("copied %d, want the 2 shifts of the live position", n)
	}
}

// F88
func TestCopyShifts_Idempotent(t *testing.T) {
	w := newCopyWorld(t)
	w.r.shift(w.d, w.pos.ID, w.from, w.fromStart.Add(10*time.Hour), 2, 1)
	w.r.shift(w.d, w.pos.ID, w.from, w.fromStart.Add(20*time.Hour), 2, 1)
	n, err := w.r.svc.CopyShifts(w.r.as(w.r.boss), w.d, w.from, w.to)
	wantNoErr(t, err)
	if n != 2 {
		t.Fatalf("first copy = %d, want 2", n)
	}
	n, err = w.r.svc.CopyShifts(w.r.as(w.r.boss), w.d, w.from, w.to)
	wantNoErr(t, err)
	if n != 0 {
		t.Errorf("second copy = %d, want 0", n)
	}
	w.r.shift(w.d, w.pos.ID, w.from, w.fromStart.Add(30*time.Hour), 2, 1)
	n, err = w.r.svc.CopyShifts(w.r.as(w.r.boss), w.d, w.from, w.to)
	wantNoErr(t, err)
	if n != 1 {
		t.Errorf("third copy after one new source shift = %d, want 1", n)
	}
}

// F119
func TestSetUp_NoExtent_CopiesNothing(t *testing.T) {
	w := newCopyWorld(t)
	w.r.shift(w.d, w.pos.ID, w.from, w.fromStart.Add(10*time.Hour), 2, 1)
	res, err := w.r.svc.SetUp(w.r.as(w.r.volunteer("Tumbleweed")), eventsetup.Request{Event: w.to, CopyFrom: w.from})
	wantNoErr(t, err)
	if res.Items != 0 {
		t.Errorf("Items = %d, want 0 for a caller who leads nothing", res.Items)
	}
}

func TestSetUp_CopyFromZero_ReturnsZeroItems(t *testing.T) {
	w := newCopyWorld(t)
	res, err := w.r.svc.SetUp(w.r.as(w.r.boss), eventsetup.Request{Event: w.to})
	wantNoErr(t, err)
	if res.Items != 0 {
		t.Errorf("Items = %d", res.Items)
	}
}

func TestSetUp_CopiesOnlyWithinTheCallersExtent_AndIsIdempotent(t *testing.T) {
	w := newCopyWorld(t)
	other := w.r.department("Greeters")
	otherPos := w.r.position(other, "Door crew", vol.AccessOpen)
	w.r.shift(w.d, w.pos.ID, w.from, w.fromStart.Add(10*time.Hour), 2, 1)
	w.r.shift(other, otherPos.ID, w.from, w.fromStart.Add(12*time.Hour), 2, 1)
	lead := w.r.lead(w.d)
	res, err := w.r.svc.SetUp(w.r.as(lead), eventsetup.Request{Event: w.to, CopyFrom: w.from})
	wantNoErr(t, err)
	if res.Items != 1 {
		t.Errorf("a lead of one department copied %d shifts, want 1", res.Items)
	}
	res, err = w.r.svc.SetUp(w.r.as(lead), eventsetup.Request{Event: w.to, CopyFrom: w.from})
	wantNoErr(t, err)
	if res.Items != 0 {
		t.Errorf("a second SetUp copied %d, want 0", res.Items)
	}
	res, err = w.r.svc.SetUp(w.r.as(w.r.boss), eventsetup.Request{Event: w.to, CopyFrom: w.from})
	wantNoErr(t, err)
	if res.Items != 1 {
		t.Errorf("the site-wide caller copied %d more, want the other department's 1", res.Items)
	}
}

// ---- person-page section (F121) ----

// F121
func TestSections_NoExtent_OmitsPerson(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Dandelion")
	r.member(s.dept, p)
	r.book(p, s.sh.ID)

	// A stranger with no extent sees nothing of p.
	got, err := r.svc.Sections(r.as(r.volunteer("Nosy Parker")), []model.PersonID{p})
	wantNoErr(t, err)
	if secs, ok := got[p]; ok {
		t.Errorf("a viewer with no extent was given sections %+v for the person", secs)
	}
	// The person sees their own; a lead of the department sees p too.
	for name, viewer := range map[string]model.PersonID{"self": p, "lead": s.lead} {
		got, err := r.svc.Sections(r.as(viewer), []model.PersonID{p})
		wantNoErr(t, err)
		secs := got[p]
		if len(secs) != 1 || secs[0].Key != "volunteers.summary" || secs[0].Failed || len(secs[0].Items) == 0 {
			t.Errorf("%s: sections = %+v, want one volunteers.summary with items", name, secs)
		}
	}
	// A lead of another department sees nothing.
	got, err = r.svc.Sections(r.as(r.lead(r.department("Greeters"))), []model.PersonID{p})
	wantNoErr(t, err)
	if _, ok := got[p]; ok {
		t.Error("a lead of another department sees the person's volunteering")
	}
	var _ personpage.Provider = r.svc
	var _ eventsetup.Provider = r.svc
}
