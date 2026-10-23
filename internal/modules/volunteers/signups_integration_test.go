//go:build integration

package volunteers_test

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// lead plus the right to volunteer: sees every shift of the department and may sign up.
// A lead who books needs a playa name like anyone (decision 26).
func (r *rig) volunteeringLead(d model.DepartmentID) model.PersonID {
	p := r.lead(d)
	r.az.Grant(p, pSignUp)
	r.ppl.Add(p, r.names.unique("Lead Booker"))
	return p
}

func TestSignUp_Books_PublishesAuditsAndSchedulesReminder(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Dandelion")
	su, err := r.svc.SignUp(r.as(p), s.sh.ID)
	wantNoErr(t, err)
	if su.Person != p || su.Shift != s.sh.ID || su.Status != vol.SignupBooked || !su.BookedBy.IsZero() || !su.WaivedBy.IsZero() {
		t.Errorf("signup = %+v", su)
	}
	sigs := r.published(volunteers.SignalSignupBooked)
	if len(sigs) != 1 {
		t.Fatalf("%d signup-booked signals, want 1", len(sigs))
	}
	for _, k := range []string{"signup", "shift", "person", "department"} {
		if _, ok := sigs[0].Payload[k]; !ok {
			t.Errorf("payload lacks %q", k)
		}
	}
	entries := r.audited("volunteers.signup.created")
	if len(entries) != 1 || entries[0].Reason != "self" || entries[0].Subject != p {
		t.Errorf("audit = %+v, want one entry with reason self about the person", entries)
	}
	var reminders []jobs.Request
	for _, req := range r.jobs.Enqueued() {
		if string(req.Name) == volunteers.JobShiftReminder {
			reminders = append(reminders, req)
		}
	}
	if len(reminders) != 1 {
		t.Fatalf("%d reminders enqueued, want 1", len(reminders))
	}
	if reminders[0].Key != su.ID.String() {
		t.Errorf("reminder key = %q, want the sign-up ID (never the person)", reminders[0].Key)
	}
	if !reminders[0].RunAt.Equal(s.sh.Starts.Add(-24 * time.Hour)) {
		t.Errorf("reminder RunAt = %v, want start - reminder_lead (24h)", reminders[0].RunAt)
	}
}

func TestSignUp_ReminderLeadZero_EnqueuesNothing(t *testing.T) {
	r := newReal(t)
	r.set.Set(volunteers.SettingReminderLead, "0")
	s := r.scenario()
	r.book(r.volunteer("Dandelion"), s.sh.ID)
	if n := len(r.jobs.Enqueued()); n != 0 {
		t.Errorf("%d jobs enqueued with reminders off", n)
	}
}

func TestSignUp_ReminderWouldBeInThePast_EnqueuesNothing(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	r.advance(s.sh.Starts.Add(-2 * time.Hour).Sub(now)) // two hours before the start; the 24h lead has passed
	r.book(r.volunteer("Dandelion"), s.sh.ID)
	if n := len(r.jobs.Enqueued()); n != 0 {
		t.Errorf("%d reminders enqueued although start - lead is in the past", n)
	}
}

// SignUp never waives (D42): the method takes only a shift.
func TestSignUp_NeverWaives(t *testing.T) {
	m, ok := reflect.TypeFor[*volunteers.Service]().MethodByName("SignUp")
	if !ok {
		t.Fatal("no SignUp method")
	}
	if m.Type.NumIn() != 3 { // receiver, ctx, shift
		t.Errorf("SignUp takes %d arguments besides the receiver, want ctx and the shift only; a sign-up cannot carry waive", m.Type.NumIn()-1)
	}
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	pos := r.position(d, "Radio op", vol.AccessOpen, q.ID)
	sh := r.shift(d, pos.ID, model.EventID{}, when(0), 4, 2)
	// A person who holds every lead permission (including shifts.manage, which allows Assign
	// with Waive) is still refused when signing themself up unqualified.
	lead := r.volunteeringLead(d)
	_, err := r.svc.SignUp(r.as(lead), sh.ID)
	wantConflict(t, err, "unqualified")
	ro, err := r.svc.Roster(r.as(lead), sh.ID)
	wantNoErr(t, err)
	if len(ro.Entries) != 0 {
		t.Error("an unqualified self sign-up was booked")
	}
}

// F90
func TestSignUp_InvisibleShift_ReturnsNotFound(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	inner := r.position(d, "Inner circle", vol.AccessMembers)
	sh := r.shift(d, inner.ID, model.EventID{}, when(0), 2, 2)
	_, err := r.svc.SignUp(r.as(r.volunteer("Tumbleweed")), sh.ID)
	wantKind(t, err, errs.NotFound)
	_, err = r.svc.SignUp(r.as(r.volunteer("Tumbleweed")), model.NewID[vol.ShiftID]())
	wantKind(t, err, errs.NotFound)
}

// F91: a canceled shift is visible only to its department's managers, who may also volunteer.
func TestSignUp_CanceledShift_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	wantNoErr(t, r.svc.CancelShift(r.as(s.lead), s.sh.ID))
	_, err := r.svc.SignUp(r.as(r.volunteeringLead(s.dept)), s.sh.ID)
	wantConflict(t, err, "canceled")
	_, err = r.svc.SignUp(r.as(r.volunteer("Tumbleweed")), s.sh.ID)
	wantKind(t, err, errs.NotFound) // decision 20: a canceled shift does not exist for a plain volunteer
}

// F92
func TestSignUp_Started_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	lead := r.volunteeringLead(s.dept)
	r.advance(s.sh.Starts.Add(time.Minute).Sub(now))
	_, err := r.svc.SignUp(r.as(lead), s.sh.ID)
	wantConflict(t, err, "started")
}

// F93
func TestSignUp_ClosedEvent_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	s0, e0 := burnDates()
	ev := r.event(s0, e0, "UTC", false)
	sh := r.shift(d, pos.ID, ev, when(0), 2, 2)
	r.closeEvent(ev)
	_, err := r.svc.SignUp(r.as(r.volunteer("Tumbleweed")), sh.ID)
	wantConflict(t, err, "closed")
}

func TestSignUp_ArchivedDepartment_IsRefused(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	r.archiveDepartment(s.dept)
	lead := r.volunteeringLead(s.dept)
	_, err := r.svc.SignUp(r.as(lead), s.sh.ID)
	wantConflict(t, err, "archived")
	// To a plain volunteer the archived department's shift does not exist.
	_, err = r.svc.SignUp(r.as(r.volunteer("Tumbleweed")), s.sh.ID)
	if !errs.Is(err, errs.NotFound) && !errs.Is(err, errs.Conflict) {
		t.Errorf("got %v, want NotFound or Conflict archived", err)
	}
}

// F94: a lead sees the department's members-only shifts without being a member.
func TestSignUp_NotMember_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	inner := r.position(d, "Inner circle", vol.AccessMembers)
	sh := r.shift(d, inner.ID, model.EventID{}, when(0), 2, 2)
	lead := r.volunteeringLead(d)
	_, err := r.svc.SignUp(r.as(lead), sh.ID)
	wantConflict(t, err, "not_member")
	r.member(d, lead)
	_, err = r.svc.SignUp(r.as(lead), sh.ID)
	wantNoErr(t, err)
}

// F95
func TestSignUp_Unqualified_Table(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	lead := r.lead(d)
	s0, e0 := burnDates()
	denver := r.event(s0, e0, "America/Denver", false)
	pos := r.position(d, "Radio op", vol.AccessOpen, q.ID)
	// 03:00 UTC on 8 November is 20:00 on 7 November in Denver (MST, UTC-7 after the DST switch).
	startsUTC := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	sh, err := r.svc.CreateShift(r.as(lead), d, vol.ShiftInput{Position: pos.ID, Event: denver, Label: "Late shift", Starts: startsUTC, Ends: startsUTC.Add(3 * time.Hour), Slots: 20})
	wantNoErr(t, err)

	record := func(p model.PersonID, expires vol.Date) {
		t.Helper()
		_, err := r.svc.RecordQualification(r.as(lead), vol.RecordInput{Qualification: q.ID, Person: p, AcquiredOn: jan1, ExpiresOn: expires})
		wantNoErr(t, err)
	}
	tests := []struct {
		name  string
		setup func(p model.PersonID)
		ok    bool
	}{
		{"no record", func(model.PersonID) {}, false},
		{"pending record", func(p model.PersonID) {
			_, err := r.svc.UploadCertificate(r.as(p), vol.UploadInput{Qualification: q.ID, AcquiredOn: jan1, Upload: upload("c.pdf", pdf())})
			wantNoErr(t, err)
		}, false},
		{"expired the day before the shift date", func(p model.PersonID) { record(p, vol.Date{Year: 2026, Month: 11, Day: 6}) }, false},
		{"expires on the shift date in the shift's zone", func(p model.PersonID) { record(p, vol.Date{Year: 2026, Month: 11, Day: 7}) }, true},
		{"indefinite", func(p model.PersonID) { record(p, vol.Date{}) }, true},
		{"expires well after", func(p model.PersonID) { record(p, vol.Date{Year: 2027, Month: 6, Day: 1}) }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := r.volunteer("Volunteer " + tc.name)
			tc.setup(p)
			_, err := r.svc.SignUp(r.as(p), sh.ID)
			if tc.ok {
				wantNoErr(t, err)
				return
			}
			wantConflict(t, err, "unqualified")
			list, lerr := shiftItems(r.svc.ListShifts(r.as(p), vol.ShiftQuery{Department: d}))
			wantNoErr(t, lerr)
			for _, s := range list {
				if s.ID == sh.ID && (s.Eligibility.Eligible || len(s.Eligibility.Missing) != 1 || s.Eligibility.Missing[0] != q.ID) {
					t.Errorf("eligibility = %+v, want not eligible, missing the radio qualification", s.Eligibility)
				}
			}
		})
	}
}

// F96
func TestSignUp_Full_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	sh := r.shift(d, pos.ID, model.EventID{}, when(0), 2, 2)
	r.book(r.volunteer("Dandelion"), sh.ID)
	r.book(r.volunteer("Tumbleweed"), sh.ID)
	_, err := r.svc.SignUp(r.as(r.volunteer("Quillfeather")), sh.ID)
	wantConflict(t, err, "full")
}

func TestSignUp_CanceledSignupFreesTheSlot(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	sh := r.shift(d, pos.ID, model.EventID{}, when(0), 2, 1)
	a, b := r.volunteer("Dandelion"), r.volunteer("Tumbleweed")
	su := r.book(a, sh.ID)
	wantNoErr(t, r.svc.CancelSignup(r.as(a), su.ID))
	r.book(b, sh.ID)
	// Re-booking after canceling is allowed (the unique index covers booked rows only).
	wantNoErr(t, r.svc.CancelSignup(r.as(b), r.bookingOf(b, sh.ID)))
	r.book(a, sh.ID)
}

func (r *rig) bookingOf(p model.PersonID, sh vol.ShiftID) vol.SignupID {
	r.t.Helper()
	sched, err := scheduleItems(r.svc.MySchedule(r.as(p), vol.ScheduleQuery{}))
	wantNoErr(r.t, err)
	for _, e := range sched {
		if e.Shift.ID == sh {
			return e.ID
		}
	}
	r.t.Fatalf("no booking of %v on %v", p, sh)
	return vol.SignupID{}
}

// F97
func TestSignUp_Overlap_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	a := r.shift(d, pos.ID, model.EventID{}, when(0), 4, 3)
	b := r.shift(d, pos.ID, model.EventID{}, when(2), 4, 3)
	adjacent := r.shift(d, pos.ID, model.EventID{}, when(4), 2, 3) // starts exactly when a ends
	p := r.volunteer("Dandelion")
	r.book(p, a.ID)
	_, err := r.svc.SignUp(r.as(p), b.ID)
	wantConflict(t, err, "overlap")
	var fe *validate.FieldError
	if !errors.As(err, &fe) || fe.Params["shift"] == "" {
		t.Errorf("overlap carries no {shift} param naming the clashing shift: %+v", fe)
	}
	if _, err := r.svc.SignUp(r.as(p), adjacent.ID); err != nil {
		t.Errorf("back-to-back shifts must not overlap: %v", err)
	}
}

// F98
func TestSignUp_Twice_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Dandelion")
	r.book(p, s.sh.ID)
	_, err := r.svc.SignUp(r.as(p), s.sh.ID)
	wantConflict(t, err, "duplicate")
}

// F99 and the serialisation rule (decision 7): bookings are serialised per person then per shift.
func TestSignUp_LastSlotRace_OneWins(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	sh := r.shift(d, pos.ID, model.EventID{}, when(0), 2, 1)
	var people []model.PersonID
	for range 8 {
		people = append(people, r.volunteer("Racer"))
	}
	results := race(len(people), func(i int) error {
		return errOf(r.svc.SignUp(r.as(people[i]), sh.ID))
	})
	wins, fulls := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case errs.Is(err, errs.Conflict) && codeOf(err) == "full":
			fulls++
		default:
			t.Errorf("unexpected result: %v", err)
		}
	}
	if wins != 1 || fulls != len(people)-1 {
		t.Fatalf("wins = %d, full = %d; want exactly one winner for the last slot", wins, fulls)
	}
	ro, err := r.svc.Roster(r.as(r.lead(d)), sh.ID)
	wantNoErr(t, err)
	if len(ro.Entries) != 1 {
		t.Errorf("roster has %d entries, want 1", len(ro.Entries))
	}
}

func TestSignUp_CapacityHoldsUnderConcurrency(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	const slots, contenders = 3, 20
	sh := r.shift(d, pos.ID, model.EventID{}, when(0), 2, slots)
	people := make([]model.PersonID, contenders)
	for i := range people {
		people[i] = r.volunteer("Racer")
	}
	results := race(contenders, func(i int) error {
		return errOf(r.svc.SignUp(r.as(people[i]), sh.ID))
	})
	wins := 0
	for _, err := range results {
		if err == nil {
			wins++
		} else if !errs.Is(err, errs.Conflict) || codeOf(err) != "full" {
			t.Errorf("unexpected result: %v", err)
		}
	}
	if wins != slots {
		t.Fatalf("%d bookings for %d slots", wins, slots)
	}
	got, err := r.svc.GetShift(r.as(people[0]), sh.ID)
	wantNoErr(t, err)
	if got.Booked != slots {
		t.Errorf("Booked = %d, want %d", got.Booked, slots)
	}
}

func TestSignUp_SamePersonConcurrently_BooksOnce(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Dandelion")
	results := race(8, func(int) error {
		return errOf(r.svc.SignUp(r.as(p), s.sh.ID))
	})
	wins := 0
	for _, err := range results {
		if err == nil {
			wins++
		} else if !errs.Is(err, errs.Conflict) || codeOf(err) != "duplicate" {
			t.Errorf("unexpected result: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d bookings of one person into one shift, want 1", wins)
	}
}

func TestSignUp_OverlappingShiftsConcurrently_OnlyOneBooks(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	a := r.shift(d, pos.ID, model.EventID{}, when(0), 4, 5)
	b := r.shift(d, pos.ID, model.EventID{}, when(2), 4, 5)
	p := r.volunteer("Dandelion")
	shifts := []vol.ShiftID{a.ID, b.ID}
	results := race(10, func(i int) error {
		return errOf(r.svc.SignUp(r.as(p), shifts[i%2]))
	})
	wins := 0
	for _, err := range results {
		if err == nil {
			wins++
		} else if !errs.Is(err, errs.Conflict) {
			t.Errorf("unexpected result: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d bookings of overlapping shifts won, want exactly 1 (per-person serialisation)", wins)
	}
	sched, err := scheduleItems(r.svc.MySchedule(r.as(p), vol.ScheduleQuery{}))
	wantNoErr(t, err)
	if len(sched) != 1 {
		t.Errorf("schedule holds %d overlapping bookings", len(sched))
	}
}

func TestAssignAndSignUp_RaceForTheLastSlot_OneWins(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	sh := r.shift(d, pos.ID, model.EventID{}, when(0), 2, 1)
	lead := r.lead(d)
	a, b := r.volunteer("Dandelion"), r.volunteer("Tumbleweed")
	results := race(2, func(i int) error {
		if i == 0 {
			return errOf(r.svc.SignUp(r.as(a), sh.ID))
		}
		return errOf(r.svc.Assign(r.as(lead), sh.ID, vol.AssignInput{Person: b}))
	})
	wins := 0
	for _, err := range results {
		if err == nil {
			wins++
		} else if !errs.Is(err, errs.Conflict) || codeOf(err) != "full" {
			t.Errorf("unexpected result: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d winners for one slot", wins)
	}
}

// race runs fn(0..n-1) at once, released together, and returns the n results.
func race(n int, fn func(i int) error) []error {
	out := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			out[i] = fn(i)
		}()
	}
	close(start)
	wg.Wait()
	return out
}

// ---- Assign (F100-F104) ----

// F100: the shift is open-access, so visible to everyone: without shifts.manage@D it is Permission.
func TestAssign_VisibleNotLead_ReturnsPermission(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.person("Dandelion")
	_, err := r.svc.Assign(r.as(r.lead(r.department("Greeters"))), s.sh.ID, vol.AssignInput{Person: p})
	wantKind(t, err, errs.Permission)
	_, err = r.svc.Assign(r.as(r.volunteer("Tumbleweed")), s.sh.ID, vol.AssignInput{Person: p})
	wantKind(t, err, errs.Permission)
	_, err = r.svc.Assign(r.as(r.lead(s.dept)), model.NewID[vol.ShiftID](), vol.AssignInput{Person: p})
	wantKind(t, err, errs.NotFound)
}

// F145 (decision 20): a members-only shift of a department the caller is not in, with no
// extent, does not exist for them: NotFound, never Permission (a 403 would confirm the ID).
func TestAssign_InvisibleShift_ReturnsNotFound(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	inner := r.position(d, "Inner circle", vol.AccessMembers)
	sh := r.shift(d, inner.ID, model.EventID{}, when(0), 2, 2)
	p := r.person("Dandelion")
	for name, who := range map[string]model.PersonID{
		"plain volunteer":     r.volunteer("Tumbleweed"),
		"lead of another dep": r.lead(r.department("Greeters")),
	} {
		_, err := r.svc.Assign(r.as(who), sh.ID, vol.AssignInput{Person: p})
		if !errs.Is(err, errs.NotFound) {
			t.Errorf("%s: Assign on an invisible shift = %v (kind %v), want NotFound", name, err, errs.KindOf(err))
		}
	}
}

func TestAssign_BooksAnotherPerson(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.person("Dandelion") // Assign needs no shifts.signup of the assignee
	su, err := r.svc.Assign(r.as(s.lead), s.sh.ID, vol.AssignInput{Person: p})
	wantNoErr(t, err)
	if su.Person != p || su.BookedBy != s.lead || su.Waived || !su.WaivedBy.IsZero() {
		t.Errorf("signup = %+v, want booked for the person by the lead, no waiver", su)
	}
	entries := r.audited("volunteers.signup.created")
	if len(entries) != 1 || entries[0].Reason != "lead" || entries[0].Subject != p {
		t.Errorf("audit = %+v, want reason lead about the assignee", entries)
	}
	if len(r.published(volunteers.SignalSignupBooked)) != 1 {
		t.Error("no signup-booked signal")
	}
	// Waive on an eligible person records nothing special.
	q := r.person("Tumbleweed")
	su, err = r.svc.Assign(r.as(s.lead), s.sh.ID, vol.AssignInput{Person: q, Waive: true})
	wantNoErr(t, err)
	if !su.WaivedBy.IsZero() {
		t.Errorf("WaivedBy = %v for an eligible person, want zero", su.WaivedBy)
	}
	for _, e := range r.audited("volunteers.signup.created") {
		if e.Reason == "waived" {
			t.Error("a waiver was audited for an eligible person")
		}
	}
}

func TestAssign_RulesStillApply(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	a := r.shift(d, pos.ID, model.EventID{}, when(0), 4, 1)
	b := r.shift(d, pos.ID, model.EventID{}, when(2), 4, 3)
	lead := r.lead(d)
	p, q := r.person("Dandelion"), r.person("Tumbleweed")
	_, err := r.svc.Assign(r.as(lead), a.ID, vol.AssignInput{Person: p})
	wantNoErr(t, err)
	_, err = r.svc.Assign(r.as(lead), a.ID, vol.AssignInput{Person: p})
	wantConflict(t, err, "duplicate")
	_, err = r.svc.Assign(r.as(lead), a.ID, vol.AssignInput{Person: q, Waive: true})
	wantConflict(t, err, "full") // Waive covers a missing requirement, nothing else
	_, err = r.svc.Assign(r.as(lead), b.ID, vol.AssignInput{Person: p, Waive: true})
	wantConflict(t, err, "overlap")
	// A person people does not know has no playa name: decision 26 answers unnamed before the insert.
	_, err = r.svc.Assign(r.as(lead), b.ID, vol.AssignInput{Person: model.NewID[model.PersonID]()})
	wantConflict(t, err, "unnamed")
	_, err = r.svc.Assign(r.as(lead), b.ID, vol.AssignInput{})
	wantInvalid(t, err, "required")
}

// F101
func TestAssign_Ended_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	r.advance(s.sh.Ends.Add(time.Minute).Sub(now))
	_, err := r.svc.Assign(r.as(s.lead), s.sh.ID, vol.AssignInput{Person: r.person("Dandelion")})
	wantConflict(t, err, "ended")
}

func TestAssign_AfterStartBeforeEnd_WalkOnsAllowed(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	r.advance(s.sh.Starts.Add(time.Hour).Sub(now))
	_, err := r.svc.Assign(r.as(s.lead), s.sh.ID, vol.AssignInput{Person: r.person("Dandelion")})
	wantNoErr(t, err)
}

// F102
func TestAssign_UnqualifiedNoWaive_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	pos := r.position(d, "Radio op", vol.AccessOpen, q.ID)
	sh := r.shift(d, pos.ID, model.EventID{}, when(0), 4, 2)
	_, err := r.svc.Assign(r.as(r.lead(d)), sh.ID, vol.AssignInput{Person: r.person("Dandelion")})
	wantConflict(t, err, "unqualified")
}

// F103 (D42)
func TestAssign_Waive_RecordsWaiver(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	pos := r.position(d, "Radio op", vol.AccessOpen, q.ID)
	sh := r.shift(d, pos.ID, model.EventID{}, when(0), 4, 2)
	lead := r.lead(d)
	p := r.person("Dandelion")
	su, err := r.svc.Assign(r.as(lead), sh.ID, vol.AssignInput{Person: p, Waive: true})
	wantNoErr(t, err)
	if !su.Waived || su.WaivedBy != lead || su.BookedBy != lead {
		t.Errorf("signup = %+v, want Waived, WaivedBy and BookedBy the lead", su)
	}
	entries := r.audited("volunteers.signup.created")
	if len(entries) != 1 || entries[0].Reason != "waived" || entries[0].Subject != p {
		t.Errorf("audit = %+v, want one entry with reason waived", entries)
	}
	ro, err := r.svc.Roster(r.as(lead), sh.ID)
	wantNoErr(t, err)
	if len(ro.Entries) != 1 || !ro.Entries[0].Waived || ro.Entries[0].WaivedBy != lead || ro.Entries[0].Eligible {
		t.Errorf("roster = %+v, want the person flagged waived and not eligible", ro.Entries)
	}
	// Waiving needs shifts.manage: a participant cannot.
	_, err = r.svc.Assign(r.as(r.volunteer("Tumbleweed")), sh.ID, vol.AssignInput{Person: r.person("Quillfeather"), Waive: true})
	wantKind(t, err, errs.Permission)
}

// F104
func TestAssign_EnqueueFails_RollsBack(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.person("Dandelion")
	r.jobs.Fail(errs.E("test.jobs", errs.Unavailable, errors.New("jobs down")))
	_, err := r.svc.Assign(r.as(s.lead), s.sh.ID, vol.AssignInput{Person: p})
	wantKind(t, err, errs.Unavailable)
	ro, err := r.svc.Roster(r.as(s.lead), s.sh.ID)
	wantNoErr(t, err)
	if len(ro.Entries) != 0 {
		t.Errorf("%d bookings survive a failed Enqueue, want none", len(ro.Entries))
	}
	if len(r.published(volunteers.SignalSignupBooked)) != 0 {
		t.Error("a signup-booked signal was published for a rolled-back booking")
	}
}

func TestSignUp_EnqueueFails_RollsBack(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Dandelion")
	r.jobs.Fail(errs.E("test.jobs", errs.Unavailable, errors.New("jobs down")))
	_, err := r.svc.SignUp(r.as(p), s.sh.ID)
	wantKind(t, err, errs.Unavailable)
	sched, err := scheduleItems(r.svc.MySchedule(r.as(p), vol.ScheduleQuery{}))
	wantNoErr(t, err)
	if len(sched) != 0 {
		t.Error("the booking survived")
	}
}

func TestWrite_AuditFails_RollsBack(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Dandelion")
	r.aud.Fail(errs.E("test.audit", errs.Unavailable, errors.New("audit down")))
	_, err := r.svc.SignUp(r.as(p), s.sh.ID)
	wantKind(t, err, errs.Unavailable)
	_, err = r.svc.CreatePosition(r.as(r.boss), s.dept, vol.PositionInput{Name: "Another", Access: vol.AccessOpen})
	wantKind(t, err, errs.Unavailable)
	r.aud.Fail(nil)
	sched, err := scheduleItems(r.svc.MySchedule(r.as(p), vol.ScheduleQuery{}))
	wantNoErr(t, err)
	if len(sched) != 0 {
		t.Error("a booking survived a failed audit Record")
	}
	got, err := r.svc.ListPositions(r.as(r.boss), s.dept, false)
	wantNoErr(t, err)
	if len(got) != 1 {
		t.Errorf("%d positions after a rolled-back create, want only the fixture's", len(got))
	}
}

func TestWrite_PublishFails_RollsBack(t *testing.T) {
	failing := true
	r := newReal(t, func(o *opts) {
		o.wrapBus = func(b signalsBus) signalsBus {
			return busFn{Bus: b, publish: func(signalsName) error {
				if failing {
					return errs.E("test.bus", errs.Internal, errors.New("bus refused"))
				}
				return nil
			}}
		}
	})
	failing = false
	s := r.scenario()
	p := r.volunteer("Dandelion")
	failing = true
	_, err := r.svc.SignUp(r.as(p), s.sh.ID)
	wantKind(t, err, errs.Internal)
	failing = false
	sched, err := scheduleItems(r.svc.MySchedule(r.as(p), vol.ScheduleQuery{}))
	wantNoErr(t, err)
	if len(sched) != 0 {
		t.Error("a booking survived a failed Publish")
	}
}

// ---- CancelSignup (F105-F109) ----

// F105
func TestCancelSignup_OtherNoPermission_ReturnsNotFound(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	su := r.book(r.volunteer("Dandelion"), s.sh.ID)
	wantKind(t, r.svc.CancelSignup(r.as(r.volunteer("Tumbleweed")), su.ID), errs.NotFound)
	wantKind(t, r.svc.CancelSignup(r.as(r.lead(r.department("Greeters"))), su.ID), errs.NotFound)
	wantKind(t, r.svc.CancelSignup(r.as(r.boss), model.NewID[vol.SignupID]()), errs.NotFound)
}

func TestCancelSignup_Own_FreesSlotPublishesAndAudits(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Dandelion")
	su := r.book(p, s.sh.ID)
	wantNoErr(t, r.svc.CancelSignup(r.as(p), su.ID))
	sigs := r.published(volunteers.SignalSignupCanceled)
	if len(sigs) != 1 {
		t.Fatalf("%d signup-canceled signals, want 1", len(sigs))
	}
	entries := r.audited("volunteers.signup.canceled")
	if len(entries) != 1 || entries[0].Reason != "self" {
		t.Errorf("audit = %+v, want one entry with reason self", entries)
	}
	if n := len(r.mail.Sent()); n != 0 {
		t.Errorf("%d notifications for a self-cancel, want none", n)
	}
	got, err := r.svc.GetShift(r.as(p), s.sh.ID)
	wantNoErr(t, err)
	if got.Booked != 0 || !got.Mine.IsZero() {
		t.Errorf("Booked/Mine = %d/%v after canceling", got.Booked, got.Mine)
	}
}

// F106
func TestCancelSignup_Started_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Dandelion")
	su := r.book(p, s.sh.ID)
	r.advance(s.sh.Starts.Add(time.Minute).Sub(now))
	wantConflict(t, r.svc.CancelSignup(r.as(p), su.ID), "started")
}

// F107
func TestCancelSignup_InsideCutoff_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	r.set.Set(volunteers.SettingCancelCutoff, "48h")
	s := r.scenario()
	p := r.volunteer("Dandelion")
	su := r.book(p, s.sh.ID)
	// 40 hours before the start: inside the 48h cutoff.
	r.advance(s.sh.Starts.Add(-40 * time.Hour).Sub(now))
	wantConflict(t, r.svc.CancelSignup(r.as(p), su.ID), "cutoff")
	// A lead may still cancel inside the cutoff.
	wantNoErr(t, r.svc.CancelSignup(r.as(s.lead), su.ID))
}

func TestCancelSignup_OutsideCutoff_Succeeds(t *testing.T) {
	r := newReal(t)
	r.set.Set(volunteers.SettingCancelCutoff, "48h")
	s := r.scenario()
	p := r.volunteer("Dandelion")
	su := r.book(p, s.sh.ID)
	r.advance(s.sh.Starts.Add(-72 * time.Hour).Sub(now))
	wantNoErr(t, r.svc.CancelSignup(r.as(p), su.ID))
}

// F108
func TestCancelSignup_Twice_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Dandelion")
	su := r.book(p, s.sh.ID)
	wantNoErr(t, r.svc.CancelSignup(r.as(p), su.ID))
	wantConflict(t, r.svc.CancelSignup(r.as(p), su.ID), "status")
}

// F109
func TestCancelSignup_ByLead_NotifiesOrLogs(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p, q := r.volunteer("Dandelion"), r.volunteer("Tumbleweed")
	a, b := r.book(p, s.sh.ID), r.book(q, s.sh.ID)
	wantNoErr(t, r.svc.CancelSignup(r.as(s.lead), a.ID))
	sent := r.mail.Sent()
	if len(sent) != 1 || string(sent[0].Template) != "volunteers.signup-removed" || sent[0].To[0] != p {
		t.Fatalf("sent = %+v, want one signup-removed message to the person", sent)
	}
	if entries := r.audited("volunteers.signup.canceled"); len(entries) != 1 || entries[0].Reason != "lead" {
		t.Errorf("audit = %+v, want reason lead", entries)
	}
	r.mail.SetConfigured(false)
	wantNoErr(t, r.svc.CancelSignup(r.as(s.lead), b.ID))
}

// ---- Roster and schedule (F110-F111) ----

// F110: an open-access shift is visible to everyone, so lacking rosters.read@D is Permission.
func TestRoster_VisibleNoPermission_ReturnsPermission(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	_, err := r.svc.Roster(r.as(r.volunteer("Tumbleweed")), s.sh.ID)
	wantKind(t, err, errs.Permission)
	_, err = r.svc.Roster(r.as(r.lead(r.department("Greeters"))), s.sh.ID)
	wantKind(t, err, errs.Permission)
}

// F111
func TestRoster_Unknown_ReturnsNotFound(t *testing.T) {
	r := newReal(t)
	_, err := r.svc.Roster(r.as(r.boss), model.NewID[vol.ShiftID]())
	wantKind(t, err, errs.NotFound)
}

// F111, F146 (decision 20): an invisible shift is NotFound, not Permission.
func TestRoster_InvisibleShift_ReturnsNotFound(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	inner := r.position(d, "Inner circle", vol.AccessMembers)
	sh := r.shift(d, inner.ID, model.EventID{}, when(0), 2, 2)
	for name, who := range map[string]model.PersonID{
		"plain volunteer":     r.volunteer("Tumbleweed"),
		"lead of another dep": r.lead(r.department("Greeters")),
	} {
		_, err := r.svc.Roster(r.as(who), sh.ID)
		if !errs.Is(err, errs.NotFound) {
			t.Errorf("%s: Roster of an invisible shift = %v (kind %v), want NotFound", name, err, errs.KindOf(err))
		}
	}
	// The same shift for the department's own lead is visible and readable.
	_, err := r.svc.Roster(r.as(r.lead(d)), sh.ID)
	wantNoErr(t, err)
}

func TestRoster_ShowsBookedPeopleWithPlayaNamesAndEligibility(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	sh := r.shift(d, pos.ID, model.EventID{}, when(0), 2, 5)
	a, b, c := r.volunteer("Dandelion"), r.volunteer("Tumbleweed"), r.volunteer("Quillfeather")
	r.book(a, sh.ID)
	r.book(b, sh.ID)
	cs := r.book(c, sh.ID)
	wantNoErr(t, r.svc.CancelSignup(r.as(c), cs.ID))
	// Requirements change after booking: nobody holds the qualification, so all booked are not eligible.
	_, err := r.svc.SetRequirements(r.as(r.boss), pos.ID, []vol.QualificationID{q.ID})
	wantNoErr(t, err)
	r.verified(a, q.ID)
	coord := r.person("Coordinator Test")
	r.az.GrantIn(coord, pRosters, d)
	ro, err := r.svc.Roster(r.as(coord), sh.ID)
	wantNoErr(t, err)
	if ro.Shift.ID != sh.ID || len(ro.Entries) != 2 {
		t.Fatalf("roster = %d entries for shift %v, want the 2 booked (canceled excluded)", len(ro.Entries), ro.Shift.ID)
	}
	for _, e := range ro.Entries {
		switch e.Person {
		case a:
			if e.PlayaName != "Dandelion" || !e.Eligible {
				t.Errorf("a = %+v", e)
			}
		case b:
			if e.PlayaName != "Tumbleweed" || e.Eligible {
				t.Errorf("b = %+v", e)
			}
		default:
			t.Errorf("unexpected person %v on the roster", e.Person)
		}
	}
	if n := len(r.audited("volunteers.roster.read")); n != 0 {
		t.Errorf("a roster without legal names wrote %d roster.read entries", n)
	}
}

func TestMySchedule_UpcomingByDefault_PastOnRequest(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	early := r.shift(d, pos.ID, model.EventID{}, when(0), 2, 2)
	late := r.shift(d, pos.ID, model.EventID{}, when(48), 2, 2)
	p := r.volunteer("Dandelion")
	r.book(p, early.ID)
	r.book(p, late.ID)
	r.advance(when(10).Sub(now)) // early is now in the past
	got, err := scheduleItems(r.svc.MySchedule(r.as(p), vol.ScheduleQuery{}))
	wantNoErr(t, err)
	if len(got) != 1 || got[0].Shift.ID != late.ID {
		t.Fatalf("upcoming = %d entries, want only the later shift", len(got))
	}
	if got[0].Department != "Rangers" || got[0].Position != "Dirt crew" {
		t.Errorf("names = %q/%q, want the department and position names", got[0].Department, got[0].Position)
	}
	past, err := scheduleItems(r.svc.MySchedule(r.as(p), vol.ScheduleQuery{Past: true}))
	wantNoErr(t, err)
	if len(past) != 1 || past[0].Shift.ID != early.ID {
		t.Errorf("past mode = %d entries, want only the ended shift", len(past))
	}
	other, err := scheduleItems(r.svc.MySchedule(r.as(r.volunteer("Tumbleweed")), vol.ScheduleQuery{Past: true}))
	wantNoErr(t, err)
	if len(other) != 0 {
		t.Error("MySchedule leaked another person's bookings")
	}
}
