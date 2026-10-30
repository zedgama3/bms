//go:build integration

package jobs_test

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// adminIRig registers a scheduled volunteers.sweep and an on-demand volunteers.remind; the
// counters count their runs, remind first.
func adminIRig(t *testing.T) (*irig, *counter, *counter) {
	t.Helper()
	r := newIRig(t)
	remind, sweep := &counter{}, &counter{}
	r.register(jobs.Decl{Name: "volunteers.sweep", Every: time.Hour, Handler: sweep.handler(nil)})
	r.register(jobs.Decl{Name: "volunteers.remind", Handler: remind.handler(nil)})
	return r, remind, sweep
}

func lastAudit(t *testing.T, r *rig) audit.Entry {
	t.Helper()
	es := r.aud.Recorded()
	if len(es) == 0 {
		t.Fatal("no audit entry recorded")
	}
	return es[len(es)-1]
}

// ---- Status ------------------------------------------------------------------------------

func TestStatus_ListsRegisteredNamesWithScheduleAndLastRun(t *testing.T) {
	r, _, c := adminIRig(t)
	ctx := r.holder(permRead)
	v, err := r.run.Status(ctx)
	wantNoErr(t, err)
	byName := map[jobs.Name]jobs.Registered{}
	for _, g := range v.Registered {
		byName[g.Name] = g
	}
	for _, n := range []jobs.Name{"volunteers.sweep", "volunteers.remind", jobs.NamePurge, jobs.NameRedeliver} {
		if _, ok := byName[n]; !ok {
			t.Fatalf("%s missing from Registered", n)
		}
	}
	sweep := byName["volunteers.sweep"]
	if sweep.Owner != "volunteers" || sweep.Every != time.Hour || sweep.Timeout != jobs.DefaultRunTimeout || sweep.Last != nil || sweep.NextDue.IsZero() {
		t.Errorf("sweep = %+v", sweep)
	}
	if p := byName[jobs.NamePurge]; p.Owner != "jobs" || p.Every != time.Hour || p.Timeout != 10*time.Minute {
		t.Errorf("jobs.purge = %+v", p)
	}
	if g := byName[jobs.NameRedeliver]; g.Every != 0 || !g.NextDue.IsZero() || g.Owner != "signals" {
		t.Errorf("signals.redeliver = %+v", g)
	}
	if byName["volunteers.remind"].NextDue != (time.Time{}) {
		t.Error("an on-demand name has a NextDue")
	}
	if v.ReadOnly {
		t.Error("ReadOnly on a writable copy")
	}

	r.runOnce()
	v, err = r.run.Status(ctx)
	wantNoErr(t, err)
	for _, g := range v.Registered {
		if g.Name == "volunteers.sweep" {
			if g.Last == nil || g.Last.Status != jobs.Done || c.n() != 1 || g.Last.Instance != "test-a" {
				t.Errorf("sweep.Last = %+v", g.Last)
			}
			if !g.NextDue.After(epoch) {
				t.Errorf("NextDue = %v, want after the slot just run", g.NextDue)
			}
		}
	}
	if v.Counts[jobs.Done] < 2 {
		t.Errorf("Counts = %v, want at least the sweep and the purge done", v.Counts)
	}
}

func TestStatus_ReadOnlyCopy_StillWorks(t *testing.T) {
	r, _, _ := adminIRig(t)
	r.flt.set(true, nil)
	v, err := r.run.Status(r.holder(permRead))
	wantNoErr(t, err)
	if !v.ReadOnly {
		t.Error("ReadOnly false on a read-only copy")
	}
}

func TestStatus_StoreUnavailable_ReturnsUnavailable(t *testing.T) {
	r, _, _ := adminIRig(t)
	r.flt.set(false, errs.E("test", errs.Unavailable, errors.New("down")))
	_, err := r.run.Status(r.holder(permRead))
	wantKind(t, err, errs.Unavailable)
}

// F62 for the list query itself: the first Status statements succeed and only list_queue.sql
// fails, so the error must come from the last of the four reads and keep its kind.
func TestStatus_QueueQueryFails_ReturnsStoreKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		kind errs.Kind
	}{
		{"unavailable", errs.E("test", errs.Unavailable, errors.New("down")), errs.Unavailable},
		{"timeout", errs.E("test", errs.Timeout, errors.New("slow")), errs.Timeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _ := adminIRig(t)
			_, err := r.run.Status(r.holder(permRead))
			wantNoErr(t, err) // the statements work before the fault is armed
			var hit int
			r.flt.mu.Lock()
			r.flt.failSQL = func(sql string) error {
				if strings.Contains(sql, "bucket") {
					hit++
					return tc.err
				}
				return nil
			}
			r.flt.mu.Unlock()
			_, err = r.run.Status(r.holder(permRead))
			wantKind(t, err, tc.kind)
			r.flt.mu.Lock()
			r.flt.failSQL = nil
			n := hit
			r.flt.mu.Unlock()
			if n == 0 {
				t.Error("the fault never reached the list_queue statement")
			}
		})
	}
}

// ---- RunNow ------------------------------------------------------------------------------

func TestRunNow_Happy_QueuesAdminRunAndAudits(t *testing.T) {
	r, _, c := adminIRig(t)
	id, err := r.run.RunNow(r.holder(permManage), "volunteers.sweep")
	wantNoErr(t, err)
	x := r.byID(id)
	if x.Name != "volunteers.sweep" || x.Status != "queued" || x.Key == nil || *x.Key != "admin:"+id.String() || !x.RunAt.Equal(epoch) {
		t.Fatalf("row = %+v", x)
	}
	e := lastAudit(t, r.rig)
	if e.Action != "jobs.job.run" || e.Outcome != audit.Succeeded || e.Record != audit.RefOf("jobs.job", id) || e.Reason != "volunteers.sweep" {
		t.Errorf("audit entry = %+v", e)
	}
	if len(r.aud.Recorded()) != 1 {
		t.Errorf("%d audit entries, want 1", len(r.aud.Recorded()))
	}
	r.runOnce()
	found := false
	for _, j := range c.all() {
		if j.ID == id {
			found = true
		}
	}
	if !found {
		t.Error("the admin run never reached the handler")
	}
}

func TestRunNow_AlreadyOpen_ReturnsConflict(t *testing.T) {
	r, _, _ := adminIRig(t)
	ctx := r.holder(permManage)
	_, err := r.run.RunNow(ctx, "volunteers.sweep")
	wantNoErr(t, err)
	_, err = r.run.RunNow(ctx, "volunteers.sweep")
	wantKind(t, err, errs.Conflict)
	if !hasField(err, "", "running") && !strings.Contains(strings.ToLower(err.Error()), "running") {
		t.Errorf("conflict does not carry the code running: %v", describe(err))
	}
	if n := len(r.byName("volunteers.sweep")); n != 1 {
		t.Errorf("%d rows, want 1", n)
	}
	if len(r.aud.Recorded()) != 1 {
		t.Errorf("a refused RunNow was audited (%d entries)", len(r.aud.Recorded()))
	}
}

func TestRunNow_OpenScheduledSlot_ReturnsConflict(t *testing.T) {
	r, _, _ := adminIRig(t)
	r.forceRunning("volunteers.sweep", "2026-10-19T12:00:00Z", epoch.Add(time.Minute))
	_, err := r.run.RunNow(r.holder(permManage), "volunteers.sweep")
	wantKind(t, err, errs.Conflict)
}

func TestRunNow_AfterRunFinished_Succeeds(t *testing.T) {
	r, _, _ := adminIRig(t)
	ctx := r.holder(permManage)
	_, err := r.run.RunNow(ctx, "volunteers.sweep")
	wantNoErr(t, err)
	r.runOnce()
	_, err = r.run.RunNow(ctx, "volunteers.sweep")
	wantNoErr(t, err)
}

func TestRunNow_Concurrent_OnlyOneWins(t *testing.T) {
	r, _, _ := adminIRig(t)
	ctx := r.holder(permManage)
	var wg sync.WaitGroup
	res := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.run.RunNow(ctx, "volunteers.sweep")
			res <- err
		}()
	}
	wg.Wait()
	close(res)
	ok := 0
	for err := range res {
		if err == nil {
			ok++
		} else {
			wantKind(t, err, errs.Conflict)
		}
	}
	if ok != 1 || len(r.byName("volunteers.sweep")) != 1 {
		t.Fatalf("%d succeeded, %d rows; want exactly one of each", ok, len(r.byName("volunteers.sweep")))
	}
}

func TestRunNow_AuditFails_RollsBackTheRow(t *testing.T) {
	r, _, _ := adminIRig(t)
	r.aud.Fail(errs.E("test", errs.Unavailable, errors.New("audit down")))
	_, err := r.run.RunNow(r.holder(permManage), "volunteers.sweep")
	wantKind(t, err, errs.Unavailable)
	if n := len(r.byName("volunteers.sweep")); n != 0 {
		t.Fatalf("%d rows: the action and its audit entry must commit together", n)
	}
}

func TestAdmin_ReadOnly_ReturnsErrReadOnly_ChangesNothing(t *testing.T) {
	r, _, _ := adminIRig(t)
	failed := r.forceFinal("volunteers.remind", "f", "failed", epoch)
	queued := r.forceQueued("volunteers.remind", "q", epoch.Add(time.Hour), "")
	r.flt.set(true, nil)
	ctx := r.holder(permManage)
	_, err := r.run.RunNow(ctx, "volunteers.sweep")
	wantReadOnly(t, err)
	wantReadOnly(t, r.run.Retry(ctx, failed))
	wantReadOnly(t, r.run.Cancel(ctx, queued))
	r.flt.set(false, nil)
	if r.byID(failed).Status != "failed" || r.byID(queued).Status != "queued" || len(r.byName("volunteers.sweep")) != 0 {
		t.Error("a refused admin action changed a row")
	}
	if len(r.aud.Recorded()) != 0 {
		t.Errorf("%d audit entries for refused actions", len(r.aud.Recorded()))
	}
}

// ---- Retry -------------------------------------------------------------------------------

func TestRetry_Failed_RequeuesWithFreshAttemptsAndAudits(t *testing.T) {
	r, c, _ := adminIRig(t)
	id := r.forceFinal("volunteers.remind", "f", "failed", epoch.Add(-time.Hour))
	wantNoErr(t, r.run.Retry(r.holder(permManage), id))
	x := r.byID(id)
	if x.Status != "queued" || x.Attempts != 0 || !x.RunAt.Equal(epoch) || x.LastError != nil || x.Finished != nil || x.Leased != nil {
		t.Fatalf("row = %+v, want queued, attempts 0, run_at now, error and finish cleared", x)
	}
	e := lastAudit(t, r.rig)
	if e.Action != "jobs.job.retried" || e.Record != audit.RefOf("jobs.job", id) || e.Reason != "volunteers.remind" || e.Outcome != audit.Succeeded {
		t.Errorf("audit entry = %+v", e)
	}
	r.runOnce()
	if c.n() != 1 || c.all()[0].Attempt != 1 || r.byID(id).Status != "done" {
		t.Errorf("retried job: calls %d, row %+v; want one run with Attempt 1 ending done", c.n(), r.byID(id))
	}
}

func TestRetry_NoHandlerRow_CanBeRetried(t *testing.T) {
	r, _, _ := adminIRig(t)
	id := r.forceQueued("volunteers.gone", "", epoch.Add(-time.Minute), "")
	r.runOnce()
	if x := r.byID(id); x.Status != "failed" || *x.LastError != "no_handler" {
		t.Fatalf("row = %+v", x)
	}
	wantNoErr(t, r.run.Retry(r.holder(permManage), id))
	if x := r.byID(id); x.Status != "queued" {
		t.Errorf("status = %q after Retry", x.Status)
	}
}

func TestRetry_UnknownID_ReturnsNotFound(t *testing.T) {
	r, _, _ := adminIRig(t)
	wantKind(t, r.run.Retry(r.holder(permManage), model.NewID[jobs.JobID]()), errs.NotFound)
	if len(r.aud.Recorded()) != 0 {
		t.Error("audited a Retry that found nothing")
	}
}

func TestRetry_NotFailed_ReturnsConflict(t *testing.T) {
	r, _, _ := adminIRig(t)
	rows := map[string]jobs.JobID{
		"queued":   r.forceQueued("volunteers.remind", "q", epoch.Add(time.Hour), ""),
		"running":  r.forceRunning("volunteers.remind", "r", epoch.Add(time.Hour)),
		"done":     r.forceFinal("volunteers.remind", "d", "done", epoch),
		"canceled": r.forceFinal("volunteers.remind", "c", "canceled", epoch),
	}
	for status, id := range rows {
		t.Run(status, func(t *testing.T) {
			err := r.run.Retry(r.holder(permManage), id)
			wantKind(t, err, errs.Conflict)
			if !hasField(err, "", "status") && !strings.Contains(strings.ToLower(err.Error()), "status") {
				t.Errorf("conflict does not carry the code status: %v", describe(err))
			}
			if got := r.byID(id).Status; got != status {
				t.Errorf("status changed to %q", got)
			}
		})
	}
	if len(r.aud.Recorded()) != 0 {
		t.Error("audited refused retries")
	}
}

func TestRetry_Concurrent_OnlyOneWins(t *testing.T) {
	r, _, _ := adminIRig(t)
	id := r.forceFinal("volunteers.remind", "f", "failed", epoch)
	ctx := r.holder(permManage)
	var wg sync.WaitGroup
	res := make(chan error, 5)
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res <- r.run.Retry(ctx, id)
		}()
	}
	wg.Wait()
	close(res)
	ok := 0
	for err := range res {
		if err == nil {
			ok++
		} else {
			wantKind(t, err, errs.Conflict)
		}
	}
	if ok != 1 || len(r.aud.Recorded()) != 1 {
		t.Fatalf("%d succeeded and %d audit entries; want 1 and 1", ok, len(r.aud.Recorded()))
	}
}

func TestRetry_AuditFails_RowStaysFailed(t *testing.T) {
	r, _, _ := adminIRig(t)
	id := r.forceFinal("volunteers.remind", "f", "failed", epoch)
	r.aud.Fail(errs.E("test", errs.Unavailable, errors.New("audit down")))
	wantKind(t, r.run.Retry(r.holder(permManage), id), errs.Unavailable)
	if got := r.byID(id).Status; got != "failed" {
		t.Errorf("status = %q: the change and its audit entry must commit together", got)
	}
}

// ---- Cancel ------------------------------------------------------------------------------

func TestCancel_Queued_CancelsAndAudits(t *testing.T) {
	r, c, _ := adminIRig(t)
	id := r.forceQueued("volunteers.remind", "q", epoch.Add(time.Hour), "")
	wantNoErr(t, r.run.Cancel(r.holder(permManage), id))
	x := r.byID(id)
	if x.Status != "canceled" || x.Finished == nil || x.Leased != nil {
		t.Fatalf("row = %+v", x)
	}
	e := lastAudit(t, r.rig)
	if e.Action != "jobs.job.canceled" || e.Record != audit.RefOf("jobs.job", id) || e.Reason != "volunteers.remind" {
		t.Errorf("audit entry = %+v", e)
	}
	r.advance(2 * time.Hour)
	r.runOnce()
	if c.n() != 0 {
		t.Error("a canceled job ran")
	}
}

func TestCancel_UnknownID_ReturnsNotFound(t *testing.T) {
	r, _, _ := adminIRig(t)
	wantKind(t, r.run.Cancel(r.holder(permManage), model.NewID[jobs.JobID]()), errs.NotFound)
}

func TestCancel_NotQueued_ReturnsConflict(t *testing.T) {
	r, _, _ := adminIRig(t)
	rows := map[string]jobs.JobID{
		"running":  r.forceRunning("volunteers.remind", "r", epoch.Add(time.Hour)),
		"done":     r.forceFinal("volunteers.remind", "d", "done", epoch),
		"failed":   r.forceFinal("volunteers.remind", "f", "failed", epoch),
		"canceled": r.forceFinal("volunteers.remind", "c", "canceled", epoch),
	}
	for status, id := range rows {
		t.Run(status, func(t *testing.T) {
			err := r.run.Cancel(r.holder(permManage), id)
			wantKind(t, err, errs.Conflict)
			if got := r.byID(id).Status; got != status {
				t.Errorf("status changed to %q", got)
			}
		})
	}
}

func TestCancel_ThenEnqueueSameKey_Reopens(t *testing.T) {
	r, _, _ := adminIRig(t)
	s := r.scoped("volunteers")
	id := enqueue(t, s, jobs.Request{Name: "volunteers.remind", Key: "k", RunAt: epoch.Add(time.Hour)})
	wantNoErr(t, r.run.Cancel(r.holder(permManage), id))
	again := enqueue(t, s, jobs.Request{Name: "volunteers.remind", Key: "k", RunAt: epoch.Add(2 * time.Hour)})
	x := r.byID(id)
	if again != id || x.Status != "queued" || !x.RunAt.Equal(epoch.Add(2*time.Hour)) || x.Attempts != 0 {
		t.Errorf("Enqueue after Cancel returned %v, row %+v; want the canceled row reopened queued at the new time (J1, F43)", again, x)
	}
}

// ---- routes over a real store ------------------------------------------------------------

func TestRoutes_Status_ReturnsJSONWithoutArgs(t *testing.T) {
	r, _, _ := adminIRig(t)
	person := newPerson()
	r.forceQueued("volunteers.remind", "k", epoch.Add(time.Hour), `{"person": "`+person.String()+`"}`)
	w := serve(r.holder(permRead), t, r.rig, "GET", "/")
	if w.Code != http.StatusOK {
		t.Fatalf("GET / = %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "volunteers.remind") {
		t.Errorf("body lacks the registered names: %s", body)
	}
	if strings.Contains(body, person.String()) || strings.Contains(strings.ToLower(body), `"args"`) {
		t.Errorf("the status response carries job arguments: %s", body)
	}
}

func TestRoutes_Actions_MapKindsToStatuses(t *testing.T) {
	r, _, _ := adminIRig(t)
	mgr := r.holder(permManage, permRead)
	unknown := model.NewID[jobs.JobID]().String()
	failed := r.forceFinal("volunteers.remind", "f", "failed", epoch)
	queued := r.forceQueued("volunteers.remind", "q", epoch.Add(time.Hour), "")
	tests := []struct {
		method, path string
		want         int
	}{
		{"POST", "/queue/" + unknown + "/retry", http.StatusNotFound},
		{"POST", "/queue/" + unknown + "/cancel", http.StatusNotFound},
		{"POST", "/queue/" + queued.String() + "/retry", http.StatusConflict},
		{"POST", "/queue/" + failed.String() + "/cancel", http.StatusConflict},
		{"POST", "/registered/volunteers.never/run", http.StatusNotFound},
		{"POST", "/registered/volunteers.remind/run", http.StatusBadRequest},
		{"POST", "/queue/" + failed.String() + "/retry", http.StatusOK},
		{"POST", "/queue/" + queued.String() + "/cancel", http.StatusOK},
		{"POST", "/registered/volunteers.sweep/run", http.StatusOK},
		{"POST", "/registered/volunteers.sweep/run", http.StatusConflict},
	}
	for _, tc := range tests {
		w := serve(mgr, t, r.rig, tc.method, tc.path)
		got := w.Code
		if tc.want == http.StatusOK && got/100 == 2 {
			got = http.StatusOK
		}
		if got != tc.want {
			t.Errorf("%s %s = %d, want %d (%s)", tc.method, tc.path, w.Code, tc.want, strings.TrimSpace(w.Body.String()))
		}
	}
}
