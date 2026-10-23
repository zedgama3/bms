//go:build integration

package volunteers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// horizon is ReminderHorizon (decision 25): jobs.MaxDelay - 24h.
const horizon = jobs.MaxDelay - 24*time.Hour

func (r *rig) reminderRows() []jobs.Request {
	var out []jobs.Request
	for _, q := range r.jobs.Queued() {
		if string(q.Name) == volunteers.JobShiftReminder {
			out = append(out, q)
		}
	}
	return out
}

func (r *rig) runDue() {
	r.t.Helper()
	if _, err := r.jobs.RunDue(ctxT(r.t), r.clk.Now()); err != nil {
		r.t.Fatalf("RunDue: %v", err)
	}
}

// farScenario is a department, open position and a shift starting days days after the rig's clock.
func (r *rig) farScenario(days int) scenario {
	r.t.Helper()
	d := r.department("Rangers " + model.NewID[model.DepartmentID]().String()[:6])
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	start := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC).AddDate(0, 0, days)
	return scenario{dept: d, lead: r.lead(d), pos: pos, sh: r.shift(d, pos.ID, model.EventID{}, start, 4, 3)}
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// F112b
func TestReminder_FarBooking_ParksTwiceThenSendsOnce(t *testing.T) {
	r := newReal(t)
	s := r.farScenario(800) // more than 2 x horizon (about 682 days) ahead
	su := r.book(r.volunteer("Dandelion"), s.sh.ID)
	due := s.sh.Starts.Add(-24 * time.Hour)
	for step := 1; step <= 2; step++ {
		rows := r.reminderRows()
		if len(rows) != 1 || rows[0].Key != su.ID.String() {
			t.Fatalf("step %d: Queued = %+v, want one row with the sign-up's Key", step, rows)
		}
		if want := minTime(due, r.clk.Now().Add(horizon)); !rows[0].RunAt.Equal(want) {
			t.Fatalf("step %d: parked at %v, want %v", step, rows[0].RunAt, want)
		}
		if !rows[0].RunAt.Before(due) {
			t.Fatalf("step %d: parked at or after due (%v)", step, due)
		}
		r.advance(rows[0].RunAt.Sub(r.clk.Now()))
		r.runDue()
		if n := len(r.sentTo("volunteers.shift-reminder")); n != 0 {
			t.Fatalf("step %d: %d reminders sent at a parked run", step, n)
		}
	}
	rows := r.reminderRows()
	if len(rows) != 1 || !rows[0].RunAt.Equal(due) {
		t.Fatalf("after two parks Queued = %+v, want one row at due %v", rows, due)
	}
	r.advance(due.Sub(r.clk.Now()))
	r.runDue()
	sent := r.sentTo("volunteers.shift-reminder")
	if len(sent) != 1 {
		t.Fatalf("%d reminders at due, want 1", len(sent))
	}
	if want := fmt.Sprintf("reminder:%s:%d", su.ID, s.sh.Starts.Unix()); sent[0].Key != want {
		t.Errorf("Key = %q, want %q", sent[0].Key, want)
	}
	if len(r.reminderRows()) != 0 {
		t.Errorf("Queued after the send = %+v, want none", r.reminderRows())
	}
	r.advance(time.Hour)
	r.runDue()
	if n := len(r.sentTo("volunteers.shift-reminder")); n != 1 {
		t.Errorf("a further RunDue sent another reminder (%d)", n)
	}
}

// F151a
func TestUpdateShift_AfterReminderSent_ReschedulesAndRemindsAgain(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	su := r.book(r.volunteer("Dandelion"), s.sh.ID)
	r.atReminderTime(s.sh)
	r.runDue()
	first := r.sentTo("volunteers.shift-reminder")
	if len(first) != 1 || len(r.reminderRows()) != 0 {
		t.Fatalf("setup: %d sent, Queued %+v; want the reminder sent and its row finished", len(first), r.reminderRows())
	}
	in := shiftInputOf(s.sh)
	in.Starts, in.Ends = s.sh.Starts.Add(72*time.Hour), s.sh.Ends.Add(72*time.Hour)
	_, err := r.svc.UpdateShift(r.as(s.lead), s.sh.ID, in)
	wantNoErr(t, err)
	newDue := in.Starts.Add(-24 * time.Hour)
	rows := r.reminderRows()
	if len(rows) != 1 || !rows[0].RunAt.Equal(newDue) || rows[0].Key != su.ID.String() {
		t.Fatalf("Queued = %+v, want the row reopened at %v", rows, newDue)
	}
	n := 0
	for _, req := range r.jobs.Enqueued() {
		if string(req.Name) == volunteers.JobShiftReminder && req.Key == su.ID.String() {
			n++
		}
	}
	if n != 2 {
		t.Errorf("Enqueued has %d requests for the key, want 2", n)
	}
	r.advance(newDue.Sub(r.clk.Now()))
	r.runDue()
	all := r.sentTo("volunteers.shift-reminder")
	if len(all) != 2 || all[0].Key == all[1].Key {
		t.Fatalf("sent = %d with keys %v; want a second reminder under a different Key", len(all), all)
	}
}

// parkNotify blocks a Send until released, so a test can act while the handler runs.
type parkNotify struct {
	notify.Notifier
	entered chan struct{}
	release chan struct{}
}

func (p *parkNotify) Send(ctx context.Context, m notify.Message) error {
	if m.Template == volunteers.TemplateShiftReminder {
		p.entered <- struct{}{}
		select {
		case <-p.release:
		case <-ctx.Done():
			return fmt.Errorf("parked send: %w", ctx.Err())
		}
	}
	return p.Notifier.Send(ctx, m) //nolint:wrapcheck // test wrapper
}

// F151b
func TestUpdateShift_DuringReminderRun_RequestIsNotLost(t *testing.T) {
	pn := &parkNotify{entered: make(chan struct{}, 1), release: make(chan struct{})}
	r := newReal(t, func(o *opts) {
		o.wrapNotify = func(n notify.Notifier) notify.Notifier { pn.Notifier = n; return pn }
	})
	s := r.scenario()
	r.book(r.volunteer("Dandelion"), s.sh.ID)
	r.atReminderTime(s.sh)
	done := make(chan error, 1)
	go func() {
		_, err := r.jobs.RunDue(ctxT(t), r.clk.Now())
		done <- err
	}()
	select {
	case <-pn.entered:
	case <-time.After(30 * time.Second):
		t.Fatal("the reminder handler did not start")
	}
	in := shiftInputOf(s.sh)
	in.Starts, in.Ends = s.sh.Starts.Add(96*time.Hour), s.sh.Ends.Add(96*time.Hour)
	_, err := r.svc.UpdateShift(r.as(s.lead), s.sh.ID, in)
	wantNoErr(t, err)
	close(pn.release)
	select {
	case err := <-done:
		wantNoErr(t, err)
	case <-time.After(30 * time.Second):
		t.Fatal("RunDue did not return")
	}
	want := in.Starts.Add(-24 * time.Hour)
	rows := r.reminderRows()
	if len(rows) != 1 || !rows[0].RunAt.Equal(want) {
		t.Fatalf("Queued = %+v, want one row at the time UpdateShift asked for (%v), not done", rows, want)
	}
}

type noRedeliver struct{}

func (noRedeliver) Redeliver(context.Context, signals.Delivery) error { return nil }

// F112c: the same far booking against the real runner.
func TestReminder_FarBooking_RealRunner_ParksThenSendsOnce(t *testing.T) {
	pool := realDB(t)
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	wantNoErr(t, err)
	_, err = mig.Migrate(ctxT(t), []db.Set{jobs.Set})
	wantNoErr(t, err)

	tel := telemetrytest.New(t)
	clk := clock.NewFake(now) // the runner and the module read one clock
	az := authztest.NewFake()
	run, err := jobs.New(jobs.Options{
		DB: pool, Clock: clk, Logger: tel.Logger(), Tracer: tel.Tracer("jobs"), Meter: tel.Meter("jobs"),
		Audit: audittest.NewFake("jobs"), Authz: az, Actor: actorFromSession, Redeliverer: noRedeliver{},
		Instance: "test-a", Workers: 1,
	})
	wantNoErr(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := run.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	reg, err := run.Scoped(volunteers.Manifest.Name)
	wantNoErr(t, err)
	r2 := newRig(t, opts{db: pool, real: true, clk: clk, jobsReg: reg})

	s := r2.farScenario(800)
	su := r2.book(r2.volunteer("Dandelion"), s.sh.ID)
	due := s.sh.Starts.Add(-24 * time.Hour)
	rowOf := func() (status string, attempts int, runAt time.Time) {
		t.Helper()
		err := pool.QueryRow(ctxT(t), `SELECT status, attempts, run_at FROM core.job WHERE name = $1 AND key = $2`,
			volunteers.JobShiftReminder, su.ID.String()).Scan(&status, &attempts, &runAt)
		wantNoErr(t, err)
		return status, attempts, runAt
	}
	once := func() {
		t.Helper()
		_, err := run.RunOnce(ctxT(t))
		wantNoErr(t, err)
	}
	for step := 1; step <= 2; step++ {
		status, attempts, at := rowOf()
		if status != "queued" || attempts != 0 {
			t.Fatalf("step %d: row = %s/%d, want queued/0", step, status, attempts)
		}
		if want := minTime(due, r2.clk.Now().Add(horizon)); !at.Equal(want) {
			t.Fatalf("step %d: parked at %v, want %v", step, at, want)
		}
		r2.advance(at.Sub(r2.clk.Now()))
		once()
		if n := len(r2.sentTo("volunteers.shift-reminder")); n != 0 {
			t.Fatalf("step %d: %d reminders sent while parked", step, n)
		}
		if st, att, _ := rowOf(); st != "queued" || att != 0 {
			t.Fatalf("step %d: after the parked run the row is %s/%d, want queued/0 (not done)", step, st, att)
		}
	}
	_, _, at := rowOf()
	if !at.Equal(due) {
		t.Fatalf("after two parks the row is at %v, want due %v", at, due)
	}
	r2.advance(due.Sub(r2.clk.Now()))
	once()
	if n := len(r2.sentTo("volunteers.shift-reminder")); n != 1 {
		t.Fatalf("%d reminders at due, want 1", n)
	}
	if st, _, _ := rowOf(); st != "done" {
		t.Errorf("row after the send = %s, want done", st)
	}
}

// F154a
func TestExportPerson_AfterWaivingLeadDeleted_KeepsWaived(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	pos := r.position(d, "Radio op", vol.AccessOpen, q.ID)
	sh := r.shift(d, pos.ID, model.EventID{}, when(0), 4, 2)
	lead := r.lead(d)
	p := r.person("Dandelion")
	su, err := r.svc.Assign(r.as(lead), sh.ID, vol.AssignInput{Person: p, Waive: true})
	wantNoErr(t, err)
	if !su.Waived || su.WaivedBy != lead {
		t.Fatalf("setup: signup = %+v, want Waived and WaivedBy the lead", su)
	}
	exported := func() bool {
		t.Helper()
		exp, err := r.mod.ExportPerson(ctxT(t), p)
		wantNoErr(t, err)
		var doc struct {
			Signups []struct {
				Waived bool `json:"requirement_waived"`
			} `json:"signups"`
		}
		wantNoErr(t, json.Unmarshal(exp.Data, &doc))
		if len(doc.Signups) != 1 {
			t.Fatalf("export has %d signups, want 1", len(doc.Signups))
		}
		return doc.Signups[0].Waived
	}
	if !exported() {
		t.Fatal("export does not flag the waiver before the lead is deleted")
	}
	_, err = r.mod.DeletePerson(ctxT(t), lead)
	wantNoErr(t, err)
	if !exported() {
		t.Error("export lost the waiver flag when the waiving lead was deleted")
	}
}

// F154a
func TestRoster_AfterWaivingLeadDeleted_KeepsWaiverFlag(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	pos := r.position(d, "Radio op", vol.AccessOpen, q.ID)
	sh := r.shift(d, pos.ID, model.EventID{}, when(0), 4, 2)
	lead, other := r.lead(d), r.lead(d)
	p := r.person("Dandelion")
	_, err := r.svc.Assign(r.as(lead), sh.ID, vol.AssignInput{Person: p, Waive: true})
	wantNoErr(t, err)
	_, err = r.mod.DeletePerson(ctxT(t), lead)
	wantNoErr(t, err)
	ro, err := r.svc.Roster(r.as(other), sh.ID)
	wantNoErr(t, err)
	if len(ro.Entries) != 1 {
		t.Fatalf("roster = %+v, want one entry", ro.Entries)
	}
	e := ro.Entries[0]
	if !e.WaivedBy.IsZero() {
		t.Error("waived_by still names the deleted lead")
	}
	if !e.Waived {
		t.Errorf("roster entry %+v lost the waiver flag", e)
	}
}

// F152/F103 also pin the column: a waiver on an eligible person records nothing.
func TestAssign_WaiveOnEligible_LeavesWaivedFalse(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	su, err := r.svc.Assign(r.as(s.lead), s.sh.ID, vol.AssignInput{Person: r.person("Dandelion"), Waive: true})
	wantNoErr(t, err)
	if su.Waived || !su.WaivedBy.IsZero() {
		t.Errorf("signup = %+v, want no waiver for an eligible person", su)
	}
}
