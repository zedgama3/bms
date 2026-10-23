//go:build integration

package volunteers_test

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"testing"
	"text/template"
	"time"

	"github.com/zedgama3/bms/internal/contracts/eventsetup"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// ---- M1: UpdateShift must not end in overlapping bookings when a sign-up lands between the
// roster read and the shift lock ----

// overlapping reports whether any two of a person's upcoming bookings overlap.
func overlapping(t testing.TB, r *rig, p model.PersonID) bool {
	t.Helper()
	sched, err := scheduleItems(r.svc.MySchedule(r.as(p), vol.ScheduleQuery{}))
	wantNoErr(t, err)
	for i := range sched {
		for j := i + 1; j < len(sched); j++ {
			a, b := sched[i].Shift, sched[j].Shift
			if a.Starts.Before(b.Ends) && b.Starts.Before(a.Ends) {
				return true
			}
		}
	}
	return false
}

// holdPersonLock takes q's per-person booking lock (the one sign-ups and UpdateShift use) in
// a transaction of its own and keeps it until the returned release func is called.
func holdPersonLock(t *testing.T, r *rig, q model.PersonID) (release func()) {
	t.Helper()
	locked, done, stop := make(chan struct{}), make(chan error, 1), make(chan struct{})
	go func() {
		done <- r.db.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
			if _, err := r.db.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('volunteers.signup'), hashtext($1::text))", q.String()); err != nil {
				return err //nolint:wrapcheck // test helper
			}
			close(locked)
			<-stop
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-done:
		t.Fatalf("could not take the person lock: %v", err)
	}
	var once sync.Once
	rel := func() {
		once.Do(func() {
			close(stop)
			if err := <-done; err != nil {
				t.Errorf("lock holder: %v", err)
			}
		})
	}
	t.Cleanup(rel)
	return rel
}

// waitForLockWaiter returns once some session waits for an advisory lock in this database.
func waitForLockWaiter(t *testing.T, r *rig) {
	t.Helper()
	ctx := ctxT(t)
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		var n int
		err := r.db.QueryRow(ctx, `SELECT count(*) FROM pg_locks
			WHERE locktype = 'advisory' AND NOT granted
			  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&n)
		wantNoErr(t, err)
		if n > 0 {
			return
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("UpdateShift never waited for the person lock")
		}
	}
}

// The interleaving is forced: UpdateShift has read the roster (only q) and waits for q's lock
// when p books the shift. p's booking is legal at the shift's old time.
func TestUpdateShift_SignupBetweenRosterReadAndLock_NeverOverlaps(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	lead := r.lead(d)
	a := r.shift(d, pos.ID, model.EventID{}, when(0), 4, 5) // A: 0..4h
	b := r.shift(d, pos.ID, model.EventID{}, when(6), 2, 5) // B: 6..8h
	q, p := r.volunteer("Dandelion"), r.volunteer("Tumbleweed")
	r.book(q, a.ID) // the roster UpdateShift reads first: only q
	r.book(p, b.ID)

	release := holdPersonLock(t, r, q)
	moved := shiftInputOf(a)
	moved.Starts, moved.Ends = when(5), when(9) // 5..9h: overlaps B for anyone on both
	done := make(chan error, 1)
	go func() { done <- errOf(r.svc.UpdateShift(r.as(lead), a.ID, moved)) }()
	waitForLockWaiter(t, r)

	r.book(p, a.ID) // lands between UpdateShift's roster read and its shift lock
	release()

	err := <-done
	if err == nil {
		got, gerr := r.svc.GetShift(r.as(lead), a.ID)
		wantNoErr(t, gerr)
		t.Errorf("UpdateShift succeeded (A now %v..%v) although p is booked on A and B", got.Starts, got.Ends)
	} else if !errs.Is(err, errs.Conflict) || codeOf(err) != "overlap" {
		t.Errorf("UpdateShift = %v, want Conflict overlap", err)
	}
	if overlapping(t, r, p) {
		t.Fatal("p holds overlapping bookings after the race")
	}
	got, gerr := r.svc.GetShift(r.as(lead), a.ID)
	wantNoErr(t, gerr)
	if !got.Starts.Equal(a.Starts) {
		t.Errorf("A moved to %v despite the refusal", got.Starts)
	}
}

func TestUpdateShift_ConcurrentSignups_NeverOverlap(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	lead := r.lead(d)
	for round := range 15 {
		a := r.shift(d, pos.ID, model.EventID{}, when(round*20), 4, 5)
		b := r.shift(d, pos.ID, model.EventID{}, when(round*20+6), 2, 5)
		q, p := r.volunteer("Dandelion"), r.volunteer("Tumbleweed")
		r.book(q, a.ID)
		r.book(p, b.ID)
		moved := shiftInputOf(a)
		moved.Starts, moved.Ends = when(round*20+5), when(round*20+9)
		results := race(4, func(i int) error {
			if i%2 == 0 {
				return errOf(r.svc.UpdateShift(r.as(lead), a.ID, moved))
			}
			return errOf(r.svc.SignUp(r.as(p), a.ID))
		})
		for _, err := range results {
			if err != nil && !errs.Is(err, errs.Conflict) && !errs.Is(err, errs.Unavailable) {
				t.Errorf("round %d: unexpected error %v", round, err)
			}
		}
		if overlapping(t, r, p) {
			t.Fatalf("round %d: p holds overlapping bookings", round)
		}
	}
}

// ---- S1: a roster read without the legal_name extent never asks people for other legal names ----

func TestRoster_BookedPlainLead_NeverAsksForOthersLegalNames(t *testing.T) {
	w := newRosterWorld(t)
	w.r.cpl.resetCounts()
	ro, err := w.r.svc.Roster(w.r.as(w.onRoster), w.shift.ID)
	wantNoErr(t, err)
	if len(ro.Entries) != 4 {
		t.Fatalf("%d entries", len(ro.Entries))
	}
	for _, e := range ro.Entries {
		if e.Person != w.onRoster && e.LegalName != nil {
			t.Errorf("a lead without people.contacts.read sees the legal name of %v", e.Person)
		}
	}
	w.r.cpl.mu.Lock()
	ids := slices.Clone(w.r.cpl.legalIDs)
	w.r.cpl.mu.Unlock()
	for _, id := range ids {
		if id != w.onRoster {
			t.Errorf("LegalNamesOf was asked for %v, who the viewer may not see", id)
		}
	}
	for _, e := range w.r.audited("volunteers.roster.read") {
		if e.Subject != w.onRoster {
			t.Errorf("a roster.read audit entry names %v for a name the viewer cannot see", e.Subject)
		}
	}
}

// ---- S3: UpdateShift refuses an archived department and a closed event, even unchanged ----

func TestUpdateShift_ArchivedDepartment_UnchangedInput_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	r.archiveDepartment(s.dept)
	_, err := r.svc.UpdateShift(r.as(r.boss), s.sh.ID, shiftInputOf(s.sh))
	wantConflict(t, err, "archived")
	changed := shiftInputOf(s.sh)
	changed.Label = "Renamed"
	_, err = r.svc.UpdateShift(r.as(r.boss), s.sh.ID, changed)
	wantConflict(t, err, "archived")
}

func TestUpdateShift_ClosedEvent_UnchangedInput_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	s0, e0 := burnDates()
	ev := r.event(s0, e0, "UTC", false)
	sh := r.shift(d, pos.ID, ev, when(0), 2, 2)
	r.closeEvent(ev)
	_, err := r.svc.UpdateShift(r.as(r.boss), sh.ID, shiftInputOf(sh))
	wantConflict(t, err, "closed")
}

// ---- S4: the signup-removed e-mail words every reason ----

// renderRemoved renders the default English body of volunteers.signup-removed with vars.
func renderRemoved(t testing.TB, vars map[string]string) string {
	t.Helper()
	raw, err := fs.ReadFile(volunteers.Manifest.Templates, "signup-removed.en.tmpl")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	_, body, ok := strings.Cut(string(raw), "\n\n")
	if !ok {
		t.Fatal("template has no header/body split")
	}
	tpl, err := template.New("body").Option("missingkey=zero").Funcs(template.FuncMap{"term": func(s string) string { return s }}).Parse(body)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	var sb strings.Builder
	data := struct {
		Recipient struct{ PlayaName string }
		Vars      map[string]string
		Link      string
	}{Vars: vars, Link: "/me"}
	data.Recipient.PlayaName = "Dandelion"
	if err := tpl.Execute(&sb, data); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	return sb.String()
}

// reasonSentence is what the body says about why, between "was removed." and the schedule link.
func reasonSentence(body string) string {
	_, after, _ := strings.Cut(body, "was removed.")
	before, _, _ := strings.Cut(after, "Your schedule")
	return strings.TrimSpace(before)
}

func TestSignupRemovedTemplate_WordsEachReason(t *testing.T) {
	reasons := []vol.CancelReason{vol.CancelSelf, vol.CancelLead, vol.CancelShiftCanceled, vol.CancelDepartmentArchived, vol.CancelEventClosed}
	base := reasonSentence(renderRemoved(t, map[string]string{"label": "Dirt", "starts": "x", "zone": "UTC"}))
	seen := map[string]vol.CancelReason{}
	for _, reason := range reasons {
		vars := map[string]string{"label": "Dirt", "starts": "x", "zone": "UTC", "reason": string(reason), "is_" + string(reason): "1"}
		sentence := reasonSentence(renderRemoved(t, vars))
		if sentence == "" || sentence == base {
			t.Errorf("reason %q is not worded in the e-mail (reason sentence %q)", reason, sentence)
		}
		if prev, dup := seen[sentence]; dup {
			t.Errorf("reasons %q and %q share the wording %q", prev, reason, sentence)
		}
		seen[sentence] = reason
	}
}

// The Vars the module sends set the flag the template reads, for each reason that sends mail.
func TestSignupRemoved_VarsCarryTheReasonFlag(t *testing.T) {
	t.Run("lead", func(t *testing.T) {
		r := newReal(t)
		s := r.scenario()
		su := r.book(r.volunteer("Dandelion"), s.sh.ID)
		wantNoErr(t, r.svc.CancelSignup(r.as(s.lead), su.ID))
		wantRemovedFlag(t, r, "lead")
	})
	t.Run("shift_canceled", func(t *testing.T) {
		r := newReal(t)
		s := r.scenario()
		r.book(r.volunteer("Dandelion"), s.sh.ID)
		wantNoErr(t, r.svc.CancelShift(r.as(s.lead), s.sh.ID))
		wantRemovedFlag(t, r, "shift_canceled")
	})
	t.Run("department_archived", func(t *testing.T) {
		w := newArchiveWorld(t)
		w.r.archiveDepartment(w.d)
		wantNoErr(t, deliverAndRun(w.r, departments.SignalArchived, signals.Ref("department", w.d)))
		wantRemovedFlag(t, w.r, "department_archived")
	})
	t.Run("event_closed", func(t *testing.T) {
		w := newArchiveWorld(t)
		w.r.closeEvent(w.ev)
		wantNoErr(t, deliverAndRun(w.r, events.SignalClosed, signals.Ref("event", w.ev)))
		wantRemovedFlag(t, w.r, "event_closed")
	})
}

func wantRemovedFlag(t testing.TB, r *rig, reason string) {
	t.Helper()
	msgs := r.sentTo("volunteers.signup-removed")
	if len(msgs) == 0 {
		t.Fatalf("no signup-removed message sent for %s", reason)
	}
	for _, m := range msgs {
		if m.Vars["reason"] != reason || m.Vars["is_"+reason] != "1" {
			t.Errorf("Vars = %v, want reason %s with is_%s=1", m.Vars, reason, reason)
		}
		body := renderRemoved(t, m.Vars)
		if s := reasonSentence(body); s == "" {
			t.Errorf("the e-mail for %s says nothing about why: %q", reason, body)
		}
	}
}

// ---- S5: CopyShifts and SetUp copy every shift, not only the first 500 ----

func TestCopyShifts_MoreThan500_CopiesAll(t *testing.T) {
	for _, n := range []int{501, 1203} {
		t.Run(fmt.Sprintf("%d shifts", n), func(t *testing.T) {
			r := newReal(t)
			d := r.department("Rangers")
			pos := r.position(d, "Dirt crew", vol.AccessOpen)
			start := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
			from := r.event(start, start.Add(30*24*time.Hour), "UTC", false)
			to := r.event(start.AddDate(1, 0, 0), start.AddDate(1, 0, 30), "UTC", false)
			seedShifts(r, d, pos.ID, from, start, n)

			got, err := r.svc.CopyShifts(r.as(r.boss), d, from, to)
			wantNoErr(t, err)
			if got != n {
				t.Fatalf("CopyShifts = %d, want %d", got, n)
			}
			if c := countShifts(t, r, to); c != n {
				t.Errorf("%d shifts in the target event, want %d", c, n)
			}
			again, err := r.svc.CopyShifts(r.as(r.boss), d, from, to)
			wantNoErr(t, err)
			if again != 0 {
				t.Errorf("second CopyShifts = %d, want 0 (idempotent)", again)
			}
			if c := countShifts(t, r, to); c != n {
				t.Errorf("%d shifts after the second copy, want %d", c, n)
			}
		})
	}
}

func TestSetUp_MoreThan500_CopiesAll(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	start := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	from := r.event(start, start.Add(30*24*time.Hour), "UTC", false)
	to := r.event(start.AddDate(1, 0, 0), start.AddDate(1, 0, 30), "UTC", false)
	const n = 501
	seedShifts(r, d, pos.ID, from, start, n)
	res, err := r.svc.SetUp(r.as(r.boss), eventsetup.Request{Event: to, CopyFrom: from})
	wantNoErr(t, err)
	if res.Items != n {
		t.Errorf("SetUp Items = %d, want %d", res.Items, n)
	}
	if c := countShifts(t, r, to); c != n {
		t.Errorf("%d shifts in the target event, want %d", c, n)
	}
}

// seedShifts inserts n one-hour shifts ten minutes apart, straight into the table.
func seedShifts(r *rig, d model.DepartmentID, pos vol.PositionID, ev model.EventID, start time.Time, n int) {
	r.t.Helper()
	r.exec(`INSERT INTO volunteers.shift
		(id, department_id, position_id, event_id, label, starts, ends, zone, slots, location, notes, created_at, updated_at)
		SELECT gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid, 'Dirt ' || g,
		       $4::timestamptz + g * interval '10 minutes', $4::timestamptz + g * interval '10 minutes' + interval '1 hour',
		       'UTC', 2, '', '', now(), now()
		FROM generate_series(1, $5::int) AS g`, [16]byte(d), [16]byte(pos), [16]byte(ev), start, n)
}

func countShifts(t testing.TB, r *rig, ev model.EventID) int {
	t.Helper()
	var c int
	if err := r.db.QueryRow(ctxT(t), "SELECT count(*) FROM volunteers.shift WHERE event_id = $1::uuid", [16]byte(ev)).Scan(&c); err != nil {
		t.Fatalf("count: %v", err)
	}
	return c
}
