//go:build integration

package volunteers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

func jobWith(entries ...signals.Entry) jobs.Job {
	return jobs.Job{Args: signals.NewPayload(entries...)}
}

func (r *rig) run(name string, j jobs.Job) error {
	r.t.Helper()
	return r.jobs.Run(ctxT(r.t), jobs.Name(name), j) //nolint:wrapcheck // the kind must reach the test unchanged
}

func (r *rig) sentTo(template string) []notify.Message {
	var out []notify.Message
	for _, m := range r.mail.Sent() {
		if string(m.Template) == template {
			out = append(out, m)
		}
	}
	return out
}

// atReminderTime moves the clock to the instant the default 24h reminder for a shift is due,
// the earliest the handler (decision 25, F112a) sends anything: before it, it re-enqueues.
func (r *rig) atReminderTime(sh vol.Shift) {
	r.t.Helper()
	r.advance(sh.Starts.Add(-24 * time.Hour).Sub(r.clk.Now()))
}

// drained counts, per rig, the enqueued requests drainCancelJobs has already run.
var drained sync.Map // *rig -> int

// drainCancelJobs runs every volunteers.cancel-shifts request enqueued since the last call,
// as the jobs runner would (handlers only enqueue, decision 23).
func (r *rig) drainCancelJobs() error {
	r.t.Helper()
	reqs := r.jobs.Enqueued()
	start := 0
	if v, ok := drained.Load(r); ok {
		if n, ok := v.(int); ok {
			start = n
		}
	}
	for _, req := range reqs[start:] {
		if string(req.Name) != volunteers.JobCancelShifts {
			continue
		}
		if err := r.run(volunteers.JobCancelShifts, jobs.Job{Name: req.Name, Key: req.Key, Args: req.Args}); err != nil {
			return err
		}
	}
	drained.Store(r, len(reqs))
	return nil
}

// deliverAndRun delivers a signal and then runs the cancel-shifts job it enqueued.
func deliverAndRun(r *rig, name signals.Name, entries ...signals.Entry) error {
	if err := deliver(r, name, entries...); err != nil {
		return err
	}
	return r.drainCancelJobs()
}

// ---- reminder (F112-F114) ----

func TestReminder_SendsToTheBookedPerson(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Dandelion")
	su := r.book(p, s.sh.ID)
	r.atReminderTime(s.sh)
	wantNoErr(t, r.run(volunteers.JobShiftReminder, jobWith(signals.Ref("signup", su.ID))))
	sent := r.sentTo("volunteers.shift-reminder")
	if len(sent) != 1 {
		t.Fatalf("%d reminders sent, want 1", len(sent))
	}
	m := sent[0]
	if len(m.To) != 1 || m.To[0] != p {
		t.Errorf("To = %v, want the booked person", m.To)
	}
	if want := fmt.Sprintf("reminder:%s:%d", su.ID, s.sh.Starts.Unix()); m.Key != want {
		t.Errorf("Key = %q, want %q", m.Key, want)
	}
	if want := "/volunteers/shifts/" + s.sh.ID.String(); m.Link != want {
		t.Errorf("Link = %q, want %q", m.Link, want)
	}
	for _, k := range []string{"label", "department", "position", "starts", "zone", "location"} {
		if m.Vars[k] == "" {
			t.Errorf("Vars lack %q: %v", k, m.Vars)
		}
	}
	if m.Vars["label"] != s.sh.Label || m.Vars["location"] != "Ranger HQ" {
		t.Errorf("Vars = %v", m.Vars)
	}
}

// F112
func TestReminder_StaleSignup_SendsNothing(t *testing.T) {
	t.Run("signup canceled", func(t *testing.T) {
		r := newReal(t)
		s := r.scenario()
		p := r.volunteer("Dandelion")
		su := r.book(p, s.sh.ID)
		wantNoErr(t, r.svc.CancelSignup(r.as(p), su.ID))
		r.atReminderTime(s.sh) // due now, so only the cancellation can stop it
		wantNoErr(t, r.run(volunteers.JobShiftReminder, jobWith(signals.Ref("signup", su.ID))))
		if n := len(r.sentTo("volunteers.shift-reminder")); n != 0 {
			t.Errorf("%d reminders for a canceled sign-up", n)
		}
	})
	t.Run("shift canceled", func(t *testing.T) {
		r := newReal(t)
		s := r.scenario()
		su := r.book(r.volunteer("Dandelion"), s.sh.ID)
		wantNoErr(t, r.svc.CancelShift(r.as(s.lead), s.sh.ID))
		r.mail.Fail(nil)
		r.atReminderTime(s.sh)
		before := len(r.sentTo("volunteers.shift-reminder"))
		wantNoErr(t, r.run(volunteers.JobShiftReminder, jobWith(signals.Ref("signup", su.ID))))
		if n := len(r.sentTo("volunteers.shift-reminder")); n != before {
			t.Error("a reminder was sent for a canceled shift")
		}
	})
	t.Run("shift started", func(t *testing.T) {
		r := newReal(t)
		s := r.scenario()
		su := r.book(r.volunteer("Dandelion"), s.sh.ID)
		r.advance(s.sh.Starts.Add(time.Minute).Sub(now))
		wantNoErr(t, r.run(volunteers.JobShiftReminder, jobWith(signals.Ref("signup", su.ID))))
		if n := len(r.sentTo("volunteers.shift-reminder")); n != 0 {
			t.Errorf("%d reminders after the shift started", n)
		}
	})
	t.Run("unknown signup", func(t *testing.T) {
		r := newReal(t)
		wantNoErr(t, r.run(volunteers.JobShiftReminder, jobWith(signals.Ref("signup", model.NewID[vol.SignupID]()))))
		if n := len(r.mail.Sent()); n != 0 {
			t.Errorf("%d messages for an unknown sign-up", n)
		}
	})
}

// F113
func TestReminder_NotifyUnavailable_ReturnsUnavailable(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	su := r.book(r.volunteer("Dandelion"), s.sh.ID)
	r.atReminderTime(s.sh)
	r.mail.Fail(errs.E("test.notify", errs.Unavailable, errors.New("smtp down")))
	err := r.run(volunteers.JobShiftReminder, jobWith(signals.Ref("signup", su.ID)))
	wantKind(t, err, errs.Unavailable)
}

func TestReminder_NotifyUnsupported_Succeeds(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	su := r.book(r.volunteer("Dandelion"), s.sh.ID)
	r.atReminderTime(s.sh)
	r.mail.SetConfigured(false)
	wantNoErr(t, r.run(volunteers.JobShiftReminder, jobWith(signals.Ref("signup", su.ID))))
}

// F114
func TestReminder_BadArgs_ReturnsInvalid(t *testing.T) {
	r := newReal(t)
	wantKind(t, r.run(volunteers.JobShiftReminder, jobs.Job{}), errs.Invalid)
	wantKind(t, r.run(volunteers.JobShiftReminder, jobWith(signals.Ref("shift", model.NewID[vol.ShiftID]()))), errs.Invalid)
}

// F112a: the shift was moved later, or the reminder was parked at the horizon, so
// Starts - lead is still ahead: nothing is sent, the job puts itself back with the same Key.
func TestReminder_DueStillAhead_ReenqueuesAndSendsNothing(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	su := r.book(r.volunteer("Dandelion"), s.sh.ID)
	// The booking's own reminder is due at Starts - 24h, a fortnight ahead of the clock. The
	// job runs as the queue would run it: with the sign-up's Key, its row running (V2, F112a).
	job := jobs.Job{Name: volunteers.JobShiftReminder, Key: su.ID.String(), Args: signals.NewPayload(signals.Ref("signup", su.ID))}
	wantNoErr(t, r.run(volunteers.JobShiftReminder, job))
	if n := len(r.sentTo("volunteers.shift-reminder")); n != 0 {
		t.Fatalf("%d reminders sent although the reminder is not due", n)
	}
	// The assertion is the schedule the queue would hold, not the requests the fake heard.
	var rows []jobs.Request
	for _, q := range r.jobs.Queued() {
		if string(q.Name) == volunteers.JobShiftReminder {
			rows = append(rows, q)
		}
	}
	if len(rows) != 1 || rows[0].Key != su.ID.String() {
		t.Fatalf("Queued = %+v, want exactly one shift-reminder row with the sign-up's Key", rows)
	}
	if want := s.sh.Starts.Add(-24 * time.Hour); !rows[0].RunAt.Equal(want) {
		t.Errorf("RunAt = %v, want min(due, now + horizon) = %v", rows[0].RunAt, want)
	}
	// Once the clock reaches the due time the same job sends.
	r.atReminderTime(s.sh)
	wantNoErr(t, r.run(volunteers.JobShiftReminder, jobWith(signals.Ref("signup", su.ID))))
	if n := len(r.sentTo("volunteers.shift-reminder")); n != 1 {
		t.Errorf("%d reminders after the due time, want 1", n)
	}
}

// F112a: a shift moved later after the booking is not reminded for the old time.
func TestReminder_ShiftMovedLater_NotRemindedForTheOldTime(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	su := r.book(r.volunteer("Dandelion"), s.sh.ID)
	in := shiftInputOf(s.sh)
	in.Starts, in.Ends = s.sh.Starts.Add(72*time.Hour), s.sh.Ends.Add(72*time.Hour)
	_, err := r.svc.UpdateShift(r.as(s.lead), s.sh.ID, in)
	wantNoErr(t, err)
	r.atReminderTime(s.sh) // the OLD due time: the stale queued row fires now
	wantNoErr(t, r.run(volunteers.JobShiftReminder, jobWith(signals.Ref("signup", su.ID))))
	if n := len(r.sentTo("volunteers.shift-reminder")); n != 0 {
		t.Errorf("%d reminders sent at the old due time of a shift moved 72h later", n)
	}
}

// ---- expiry scan (F115-F116) ----

type expiryWorld struct {
	r                                           *rig
	q                                           vol.Qualification
	soon, today, edge30, edge31, later, expired model.PersonID
	covered                                     model.PersonID
	recSoon                                     vol.Record
}

func newExpiryWorld(t *testing.T, mod ...func(*opts)) expiryWorld {
	t.Helper()
	r := newReal(t, mod...)
	d := r.department("Rangers")
	w := expiryWorld{r: r, q: r.qual(d, "First aid")}
	today := vol.DateOf(now, time.UTC)
	rec := func(p model.PersonID, expires vol.Date) vol.Record {
		got, err := r.svc.RecordQualification(r.as(r.boss), vol.RecordInput{Qualification: w.q.ID, Person: p, AcquiredOn: jan1, ExpiresOn: expires})
		wantNoErr(t, err)
		return got
	}
	w.soon, w.today, w.edge30, w.edge31 = r.person("Dandelion"), r.person("Tumbleweed"), r.person("Quillfeather"), r.person("Driftwood")
	w.later, w.expired, w.covered = r.person("Marigold"), r.person("Thistle"), r.person("Bramble")
	w.recSoon = rec(w.soon, today.AddDays(10))
	rec(w.today, today)
	rec(w.edge30, today.AddDays(30))
	rec(w.edge31, today.AddDays(31))
	rec(w.later, today.AddDays(200))
	rec(w.expired, today.AddDays(-1))
	rec(w.covered, today.AddDays(5))
	rec(w.covered, today.AddDays(400)) // another valid record of the same qualification: no notice
	return w
}

func expiringTo(r *rig) map[model.PersonID]notify.Message {
	out := map[model.PersonID]notify.Message{}
	for _, m := range r.sentTo("volunteers.qualification-expiring") {
		out[m.To[0]] = m
	}
	return out
}

func TestExpiryScan_NotifiesOnceInsideTheWindow(t *testing.T) {
	w := newExpiryWorld(t)
	wantNoErr(t, w.r.run(volunteers.JobExpiryScan, jobs.Job{}))
	got := expiringTo(w.r)
	for name, p := range map[string]model.PersonID{"soon": w.soon, "today": w.today, "edge30": w.edge30} {
		if _, ok := got[p]; !ok {
			t.Errorf("%s was not notified", name)
		}
	}
	for name, p := range map[string]model.PersonID{"edge31": w.edge31, "later": w.later, "expired": w.expired, "covered": w.covered} {
		if _, ok := got[p]; ok {
			t.Errorf("%s was notified but is outside the window or already covered", name)
		}
	}
	m := got[w.soon]
	if m.Key != "expiring:"+w.recSoon.ID.String() {
		t.Errorf("Key = %q, want expiring:<record>", m.Key)
	}
	for _, k := range []string{"qualification", "expires_on", "days"} {
		if m.Vars[k] == "" {
			t.Errorf("Vars lack %q: %v", k, m.Vars)
		}
	}
	if m.Vars["days"] != "10" {
		t.Errorf("days = %q, want 10", m.Vars["days"])
	}
	if _, err := strconv.Atoi(m.Vars["days"]); err != nil {
		t.Errorf("days is not a number: %v", err)
	}
}

// F116
func TestExpiryScan_Idempotent(t *testing.T) {
	w := newExpiryWorld(t)
	wantNoErr(t, w.r.run(volunteers.JobExpiryScan, jobs.Job{}))
	first := len(w.r.sentTo("volunteers.qualification-expiring"))
	if first == 0 {
		t.Fatal("nothing sent on the first run")
	}
	wantNoErr(t, w.r.run(volunteers.JobExpiryScan, jobs.Job{}))
	if n := len(w.r.sentTo("volunteers.qualification-expiring")); n != first {
		t.Errorf("second run sent %d more notices, want none (expiry_notified_on)", n-first)
	}
}

func TestExpiryScan_NoticeDaysZero_SendsNothing(t *testing.T) {
	w := newExpiryWorld(t)
	w.r.set.Set(volunteers.SettingExpiryNoticeDays, "0")
	wantNoErr(t, w.r.run(volunteers.JobExpiryScan, jobs.Job{}))
	if n := len(w.r.sentTo("volunteers.qualification-expiring")); n != 0 {
		t.Errorf("%d notices with expiry_notice_days = 0", n)
	}
}

func TestExpiryScan_NoticeDaysWidensTheWindow(t *testing.T) {
	w := newExpiryWorld(t)
	w.r.set.Set(volunteers.SettingExpiryNoticeDays, "365")
	wantNoErr(t, w.r.run(volunteers.JobExpiryScan, jobs.Job{}))
	got := expiringTo(w.r)
	if _, ok := got[w.later]; !ok {
		t.Error("a record expiring in 200 days is not noticed with a 365-day window")
	}
	if _, ok := got[w.expired]; ok {
		t.Error("an already expired record was noticed")
	}
}

// F115
func TestExpiryScan_OneFailure_ContinuesAndReports(t *testing.T) {
	sel := &selectiveNotify{}
	w := newExpiryWorld(t, func(o *opts) {
		o.wrapNotify = func(n notify.Notifier) notify.Notifier { sel.Notifier = n; return sel }
	})
	sel.fail = map[model.PersonID]error{w.soon: errs.E("test.notify", errs.Unavailable, errors.New("one address bounced"))}
	err := w.r.run(volunteers.JobExpiryScan, jobs.Job{})
	wantKind(t, err, errs.Unavailable)
	got := expiringTo(w.r)
	if _, ok := got[w.today]; !ok {
		t.Error("the batch stopped at the failure: today's record was not noticed")
	}
	if _, ok := got[w.edge30]; !ok {
		t.Error("the batch stopped at the failure: the 30-day record was not noticed")
	}
	if _, ok := got[w.soon]; ok {
		t.Error("the failed notice was recorded as sent")
	}
	// The failed one is retried by the next run, because it was not marked notified.
	sel.mu.Lock()
	sel.fail = nil
	sel.mu.Unlock()
	wantNoErr(t, w.r.run(volunteers.JobExpiryScan, jobs.Job{}))
	if _, ok := expiringTo(w.r)[w.soon]; !ok {
		t.Error("the failed notice was not retried")
	}
	if n := len(w.r.sentTo("volunteers.qualification-expiring")); n != 3 {
		t.Errorf("%d notices in total, want exactly 3 (no duplicates)", n)
	}
}

func TestExpiryScan_NotifyUnsupported_Succeeds(t *testing.T) {
	w := newExpiryWorld(t)
	w.r.mail.SetConfigured(false)
	wantNoErr(t, w.r.run(volunteers.JobExpiryScan, jobs.Job{}))
}

// ---- purge ----

func exportCounts(t *testing.T, r *rig, p model.PersonID) (signups, records, memberships int) {
	t.Helper()
	exp, err := r.mod.ExportPerson(ctxT(t), p)
	wantNoErr(t, err)
	var raw map[string]any
	if err := json.Unmarshal(exp.Data, &raw); err != nil {
		t.Fatalf("export is not a JSON object: %v", err)
	}
	n := func(k string) int {
		if a, ok := raw[k].([]any); ok {
			return len(a)
		}
		return 0
	}
	return n("signups"), n("qualification_records"), n("memberships")
}

func TestPurge_RetentionZero_DeletesNothing(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Dandelion")
	r.book(p, s.sh.ID)
	r.advance(5 * 365 * 24 * time.Hour)
	wantNoErr(t, r.run(volunteers.JobPurge, jobs.Job{}))
	if su, _, _ := exportCounts(t, r, p); su != 1 {
		t.Errorf("%d sign-ups after a purge with retention 0 (forever), want 1", su)
	}
	if n := len(r.audited("volunteers.purge.ran")); n != 0 {
		t.Errorf("purge.ran recorded %d times with retention 0, want it to do nothing", n)
	}
}

func TestPurge_Retention_RemovesOnlyWhatIsOldEnough(t *testing.T) {
	r := newReal(t)
	r.set.Set(volunteers.SettingRetention, "8760h") // one year
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	old := r.shift(d, pos.ID, model.EventID{}, when(0), 4, 3)
	canceledOld := r.shift(d, pos.ID, model.EventID{}, when(10), 4, 3)
	wantNoErr(t, r.svc.CancelShift(r.as(r.boss), canceledOld.ID))
	p, q2 := r.volunteer("Dandelion"), r.volunteer("Tumbleweed")
	r.member(d, p)
	r.book(p, old.ID)
	// A certificate for a record that will have been expired for more than a year, and one for a recent expiry.
	upOld, err := r.svc.UploadCertificate(r.as(p), vol.UploadInput{Qualification: q.ID, AcquiredOn: vol.Date{Year: 2020, Month: 1, Day: 1}, ExpiresOn: vol.Date{Year: 2021, Month: 1, Day: 1}, Upload: upload("old.pdf", pdf())})
	wantNoErr(t, err)
	_, err = r.svc.VerifyRecord(r.as(r.boss), upOld.ID, vol.VerifyInput{})
	wantNoErr(t, err)
	r.verified(q2, q.ID) // indefinite: never purged

	r.advance(time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC).Sub(now))
	fresh := r.shift(d, pos.ID, model.EventID{}, time.Date(2028, 1, 10, 9, 0, 0, 0, time.UTC), 4, 3)
	r.book(q2, fresh.ID)

	wantNoErr(t, r.run(volunteers.JobPurge, jobs.Job{}))
	su, rec, mem := exportCounts(t, r, p)
	if su != 0 {
		t.Errorf("%d old sign-ups survive the purge", su)
	}
	if rec != 0 {
		t.Errorf("%d long-expired records survive the purge", rec)
	}
	if mem != 1 {
		t.Errorf("memberships = %d, want 1 (current state is never purged)", mem)
	}
	if n := len(r.fl.Files()); n != 0 {
		t.Errorf("%d certificate files survive the purge of their record", n)
	}
	su2, rec2, _ := exportCounts(t, r, q2)
	if su2 != 1 || rec2 != 1 {
		t.Errorf("the recent sign-up/indefinite record = %d/%d, want 1/1 (kept)", su2, rec2)
	}
	if n := len(r.audited("volunteers.purge.ran")); n != 1 {
		t.Errorf("purge.ran recorded %d times, want 1", n)
	} else if r.audited("volunteers.purge.ran")[0].Reason == "" {
		t.Error("purge.ran carries no counts in Reason")
	}
	wantNoErr(t, r.run(volunteers.JobPurge, jobs.Job{})) // idempotent
}

// ---- signal handlers (F117-F118) ----

func deliver(r *rig, name signals.Name, entries ...signals.Entry) error {
	return r.sig.Deliver(ctxT(r.t), signals.Signal{ //nolint:wrapcheck // the handler error must reach the test unchanged
		ID: model.NewID[signals.SignalID](), Name: name, At: now, Payload: signals.NewPayload(entries...),
	})
}

type archiveWorld struct {
	r        *rig
	d        model.DepartmentID
	pos      vol.Position
	future   vol.Shift
	past     vol.Shift
	p, q     model.PersonID
	ev       model.EventID
	evShift  vol.Shift
	otherDep scenario
}

func newArchiveWorld(t *testing.T) archiveWorld {
	t.Helper()
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	s0, e0 := burnDates()
	ev := r.event(s0, e0, "UTC", false)
	w := archiveWorld{r: r, d: d, pos: pos, ev: ev, p: r.volunteer("Dandelion"), q: r.volunteer("Tumbleweed")}
	w.past = r.shift(d, pos.ID, model.EventID{}, when(0), 2, 5)
	w.future = r.shift(d, pos.ID, model.EventID{}, when(100), 2, 5)
	w.evShift = r.shift(d, pos.ID, ev, when(50), 2, 5)
	w.otherDep = r.scenario()
	r.book(w.p, w.past.ID)
	r.book(w.p, w.future.ID)
	r.book(w.q, w.future.ID)
	r.book(w.p, w.evShift.ID)
	r.book(r.volunteer("Marigold"), w.otherDep.sh.ID)
	r.advance(when(10).Sub(now)) // `past` has now ended; the others are still ahead
	return w
}

func TestDepartmentArchived_CancelsFutureShiftsAndBookings(t *testing.T) {
	w := newArchiveWorld(t)
	r := w.r
	r.archiveDepartment(w.d)
	wantNoErr(t, deliverAndRun(r, departments.SignalArchived, signals.Ref("department", w.d)))

	boss := r.as(r.boss)
	for _, sh := range []vol.Shift{w.future, w.evShift} {
		got, err := r.svc.GetShift(boss, sh.ID)
		wantNoErr(t, err)
		if !got.Canceled() || got.CancelReason != vol.CancelDepartmentArchived {
			t.Errorf("shift %s: canceled=%v reason=%q, want canceled with department_archived", sh.ID, got.Canceled(), got.CancelReason)
		}
	}
	got, err := r.svc.GetShift(boss, w.past.ID)
	wantNoErr(t, err)
	if got.Canceled() {
		t.Error("a shift that had already ended was canceled")
	}
	if got.Booked != 1 {
		t.Errorf("the ended shift's booking was touched (Booked = %d)", got.Booked)
	}
	other, err := r.svc.GetShift(boss, w.otherDep.sh.ID)
	wantNoErr(t, err)
	if other.Canceled() || other.Booked != 1 {
		t.Error("another department's shift was affected")
	}
	// p had 3 bookings (one ended), q had 1: three future sign-ups canceled, each person told once per shift.
	if n := len(r.published(volunteers.SignalSignupCanceled)); n != 3 {
		t.Errorf("%d signup-canceled signals, want 3", n)
	}
	removed := r.sentTo("volunteers.signup-removed")
	if len(removed) != 3 {
		t.Fatalf("%d signup-removed messages, want 3 (one per person per shift)", len(removed))
	}
	for _, m := range removed {
		if !strings.HasPrefix(m.Key, "removed:") {
			t.Errorf("Key = %q", m.Key)
		}
		if m.Vars["reason"] != "department_archived" {
			t.Errorf("reason var = %q, want department_archived", m.Vars["reason"])
		}
	}
	sched, err := scheduleItems(r.svc.MySchedule(r.as(w.p), vol.ScheduleQuery{Past: true}))
	wantNoErr(t, err)
	if len(sched) != 1 || sched[0].Shift.ID != w.past.ID {
		t.Errorf("p's past bookings = %d, want only the ended one", len(sched))
	}
	// Upcoming mode shows the two removed bookings struck through with their reason (F153, Q-VOL9).
	up, err := scheduleItems(r.svc.MySchedule(r.as(w.p), vol.ScheduleQuery{}))
	wantNoErr(t, err)
	if len(up) != 2 {
		t.Fatalf("p's upcoming schedule = %d entries, want the 2 bookings the archive removed", len(up))
	}
	for _, e := range up {
		if e.Status != vol.SignupCanceled || e.CancelReason != vol.CancelDepartmentArchived {
			t.Errorf("entry %+v, want canceled with department_archived", e.Signup)
		}
	}
}

// F117: a department archive or event close enqueues cancel-shifts under one key per
// department or event; the handler itself changes nothing and a second delivery is a no-op.
func TestDepartmentArchived_EnqueuesCancelJobOnce(t *testing.T) {
	w := newArchiveWorld(t)
	w.r.archiveDepartment(w.d)
	sig := signals.Ref("department", w.d)
	wantNoErr(t, deliver(w.r, departments.SignalArchived, sig))
	wantNoErr(t, deliver(w.r, departments.SignalArchived, sig))
	got := cancelRequests(w.r)
	if len(got) == 0 {
		t.Fatal("the handler enqueued no volunteers.cancel-shifts job")
	}
	want := "department_archived:" + w.d.String()
	for _, req := range got {
		if req.Key != want {
			t.Errorf("Key = %q, want %q (one row per department)", req.Key, want)
		}
		if _, ok := req.Args["department"]; !ok || len(req.Args) != 1 {
			t.Errorf("Args = %v, want the department only (no person)", req.Args)
		}
	}
	// The handler is quick: nothing was cancelled, signalled or sent by it.
	got2, err := w.r.svc.GetShift(w.r.as(w.r.boss), w.future.ID)
	wantNoErr(t, err)
	if got2.Canceled() {
		t.Error("the signal handler cancelled shifts itself instead of enqueueing the job")
	}
	if n := len(w.r.published(volunteers.SignalSignupCanceled)) + len(w.r.mail.Sent()); n != 0 {
		t.Errorf("the handler published/sent %d things itself", n)
	}
}

func TestEventClosed_EnqueuesCancelJobOnce(t *testing.T) {
	w := newArchiveWorld(t)
	w.r.closeEvent(w.ev)
	sig := signals.Ref("event", w.ev)
	wantNoErr(t, deliver(w.r, events.SignalClosed, sig))
	wantNoErr(t, deliver(w.r, events.SignalClosed, sig))
	got := cancelRequests(w.r)
	if len(got) == 0 {
		t.Fatal("the handler enqueued no volunteers.cancel-shifts job")
	}
	want := "event_closed:" + w.ev.String()
	for _, req := range got {
		if req.Key != want {
			t.Errorf("Key = %q, want %q", req.Key, want)
		}
		if _, ok := req.Args["event"]; !ok || len(req.Args) != 1 {
			t.Errorf("Args = %v, want the event only", req.Args)
		}
	}
}

func cancelRequests(r *rig) []jobs.Request {
	var out []jobs.Request
	for _, req := range r.jobs.Enqueued() {
		if string(req.Name) == volunteers.JobCancelShifts {
			out = append(out, req)
		}
	}
	return out
}

// F117: the job run twice (a retry, a second delivery) finds nothing the second time.
func TestCancelShiftsJob_Idempotent(t *testing.T) {
	for name, tc := range map[string]struct {
		sig  func(archiveWorld) (signals.Name, signals.Entry)
		prep func(archiveWorld)
	}{
		"department": {
			sig: func(w archiveWorld) (signals.Name, signals.Entry) {
				return departments.SignalArchived, signals.Ref("department", w.d)
			},
			prep: func(w archiveWorld) { w.r.archiveDepartment(w.d) },
		},
		"event": {
			sig: func(w archiveWorld) (signals.Name, signals.Entry) {
				return events.SignalClosed, signals.Ref("event", w.ev)
			},
			prep: func(w archiveWorld) { w.r.closeEvent(w.ev) },
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newArchiveWorld(t)
			tc.prep(w)
			sigName, entry := tc.sig(w)
			wantNoErr(t, deliverAndRun(w.r, sigName, entry))
			sigsBefore, mailBefore, auditBefore := len(w.r.published(volunteers.SignalSignupCanceled)), len(w.r.mail.Sent()), len(w.r.aud.Recorded())
			if sigsBefore == 0 || mailBefore == 0 {
				t.Fatal("the first run cancelled nothing; the fixture is wrong")
			}
			for _, req := range cancelRequests(w.r) {
				wantNoErr(t, w.r.run(volunteers.JobCancelShifts, jobs.Job{Name: req.Name, Key: req.Key, Args: req.Args}))
			}
			if n := len(w.r.published(volunteers.SignalSignupCanceled)); n != sigsBefore {
				t.Errorf("a second run published %d more signals", n-sigsBefore)
			}
			if n := len(w.r.mail.Sent()); n != mailBefore {
				t.Errorf("a second run sent %d more messages", n-mailBefore)
			}
			if n := len(w.r.aud.Recorded()); n != auditBefore {
				t.Errorf("a second run recorded %d more audit entries", n-auditBefore)
			}
		})
	}
}

// batchWorld has MaxList+5 future shifts in a department, each with one booked sign-up,
// inserted straight into the tables (the point is the batching, not the booking rules).
func newBatchWorld(t *testing.T, mod ...func(*opts)) (*rig, model.DepartmentID, int) {
	t.Helper()
	r := newReal(t, mod...)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	p := r.volunteer("Dandelion")
	n := vol.MaxList + 5
	r.exec(`INSERT INTO volunteers.shift
		(id, department_id, position_id, label, starts, ends, zone, slots, location, notes, created_at, updated_at)
		SELECT gen_random_uuid(), $1::uuid, $2::uuid, 'Dirt ' || g,
		       $3::timestamptz + g * interval '3 hours', $3::timestamptz + g * interval '3 hours' + interval '1 hour',
		       'UTC', 2, '', '', now(), now()
		FROM generate_series(1, $4::int) AS g`, [16]byte(d), [16]byte(pos.ID), when(0), n)
	r.exec(`INSERT INTO volunteers.signup (id, shift_id, person_id, status, booked_as, waived, created_at, updated_at)
		SELECT gen_random_uuid(), s.id, $1::uuid, 'booked', 'self', false, now(), now()
		FROM volunteers.shift s WHERE s.department_id = $2::uuid`, [16]byte(p), [16]byte(d))
	return r, d, n
}

func canceledShifts(t testing.TB, r *rig, d model.DepartmentID) int {
	t.Helper()
	var c int
	if err := r.db.QueryRow(ctxT(t), "SELECT count(*) FROM volunteers.shift WHERE department_id = $1::uuid AND canceled_at IS NOT NULL", [16]byte(d)).Scan(&c); err != nil {
		t.Fatalf("count: %v", err)
	}
	return c
}

// F117a: more than MaxList future shifts are all cancelled, in batches.
func TestCancelShiftsJob_Batches(t *testing.T) {
	r, d, n := newBatchWorld(t)
	r.archiveDepartment(d)
	wantNoErr(t, deliverAndRun(r, departments.SignalArchived, signals.Ref("department", d)))
	if got := canceledShifts(t, r, d); got != n {
		t.Errorf("%d of %d shifts cancelled", got, n)
	}
	if got := len(r.published(volunteers.SignalSignupCanceled)); got != n {
		t.Errorf("%d signup-canceled signals, want one per sign-up (%d)", got, n)
	}
	if got := len(r.sentTo("volunteers.signup-removed")); got != n {
		t.Errorf("%d signup-removed messages, want %d", got, n)
	}
}

// F117a: a failing batch returns its error, the earlier batches stand (each its own
// transaction) and the retry resumes without cancelling or telling anyone twice.
func TestCancelShiftsJob_BatchFails_ResumesOnRetry(t *testing.T) {
	var mu sync.Mutex
	published, failAfter := 0, -1
	r, d, n := newBatchWorld(t, func(o *opts) {
		o.wrapBus = func(b signalsBus) signalsBus {
			return busFn{Bus: b, publish: func(name signalsName) error {
				mu.Lock()
				defer mu.Unlock()
				if string(name) != volunteers.SignalSignupCanceled || failAfter < 0 {
					return nil
				}
				published++
				if published > failAfter {
					return errs.E("test.bus", errs.Unavailable, errors.New("bus down"))
				}
				return nil
			}}
		}
	})
	r.archiveDepartment(d)
	wantNoErr(t, deliver(r, departments.SignalArchived, signals.Ref("department", d)))
	mu.Lock()
	failAfter = vol.MaxList // the first batch publishes MaxList signals and commits; the second fails
	mu.Unlock()
	err := r.drainCancelJobs()
	wantKind(t, err, errs.Unavailable)
	got := canceledShifts(t, r, d)
	if got == 0 || got >= n {
		t.Fatalf("%d of %d shifts cancelled after the failed run, want the committed first batch only", got, n)
	}
	mu.Lock()
	failAfter = -1
	mu.Unlock()
	drained.Store(r, 0) // the runner retries the same row
	wantNoErr(t, r.drainCancelJobs())
	if got := canceledShifts(t, r, d); got != n {
		t.Errorf("%d of %d shifts cancelled after the retry", got, n)
	}
	if got := len(r.published(volunteers.SignalSignupCanceled)); got != n {
		t.Errorf("%d signup-canceled signals in total, want exactly %d (no sign-up cancelled twice)", got, n)
	}
}

func TestEventClosed_CancelsTheEventsFutureShiftsOnly(t *testing.T) {
	w := newArchiveWorld(t)
	w.r.closeEvent(w.ev)
	wantNoErr(t, deliverAndRun(w.r, events.SignalClosed, signals.Ref("event", w.ev)))
	boss := w.r.as(w.r.boss)
	got, err := w.r.svc.GetShift(boss, w.evShift.ID)
	wantNoErr(t, err)
	if !got.Canceled() || got.CancelReason != vol.CancelEventClosed {
		t.Errorf("event shift: canceled=%v reason=%q", got.Canceled(), got.CancelReason)
	}
	standing, err := w.r.svc.GetShift(boss, w.future.ID)
	wantNoErr(t, err)
	if standing.Canceled() || standing.Booked != 2 {
		t.Error("a standing-work shift was canceled by an event closing")
	}
	if n := len(w.r.sentTo("volunteers.signup-removed")); n != 1 {
		t.Errorf("%d messages, want 1 (p's booking on the event shift)", n)
	}
}

func TestSignalHandlers_NotifyUnsupported_StillCancel(t *testing.T) {
	w := newArchiveWorld(t)
	w.r.mail.SetConfigured(false)
	w.r.archiveDepartment(w.d)
	wantNoErr(t, deliverAndRun(w.r, departments.SignalArchived, signals.Ref("department", w.d)))
	got, err := w.r.svc.GetShift(w.r.as(w.r.boss), w.future.ID)
	wantNoErr(t, err)
	if !got.Canceled() {
		t.Error("the shift was not canceled")
	}
}

// F118
func TestDepartmentArchived_BadPayload_ReturnsInvalid(t *testing.T) {
	r := newReal(t)
	wantKind(t, deliver(r, departments.SignalArchived, signals.Ref("event", model.NewID[model.EventID]())), errs.Invalid)
	wantKind(t, deliver(r, events.SignalClosed, signals.Ref("department", model.NewID[model.DepartmentID]())), errs.Invalid)
}

// F118: the job's own args are checked too (it can be enqueued by nobody else, but a bad row must fail, not no-op).
func TestCancelShiftsJob_BadArgs_ReturnsInvalid(t *testing.T) {
	r := newReal(t)
	wantKind(t, r.run(volunteers.JobCancelShifts, jobs.Job{}), errs.Invalid)
	wantKind(t, r.run(volunteers.JobCancelShifts, jobWith(signals.Ref("person", model.NewID[model.PersonID]()))), errs.Invalid)
}

// A handler runs as the system: no actor, so nothing it does may need a session.
func TestSignalHandlers_RunAsTheSystem(t *testing.T) {
	w := newArchiveWorld(t)
	w.r.archiveDepartment(w.d)
	ctx := context.Background() // no session at all
	err := w.r.sig.Deliver(ctx, signals.Signal{
		ID: model.NewID[signals.SignalID](), Name: departments.SignalArchived, At: now,
		Payload: signals.NewPayload(signals.Ref("department", w.d)),
	})
	wantNoErr(t, err)
}

// ---- notifications carry nothing about anyone but the recipient ----

func TestNotifications_VarsCarryOnlyTheRecipientsData(t *testing.T) {
	w := newArchiveWorld(t)
	w.r.archiveDepartment(w.d)
	wantNoErr(t, deliverAndRun(w.r, departments.SignalArchived, signals.Ref("department", w.d)))
	for _, m := range w.r.mail.Sent() {
		if len(m.To) != 1 {
			t.Errorf("a notification has %d recipients", len(m.To))
		}
		for k, v := range m.Vars {
			for _, name := range []string{"Dandelion", "Tumbleweed", "Marigold"} {
				if strings.Contains(v, name) {
					t.Errorf("var %s carries a playa name", k)
				}
			}
		}
	}
}
