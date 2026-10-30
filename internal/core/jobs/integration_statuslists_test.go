//go:build integration

package jobs_test

// Amendment J-W1, decision 25: StatusView's four lists (F59 to F62). Every row is invented.

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/jobs"
)

func idsOf(rs []jobs.Record) []jobs.JobID {
	out := make([]jobs.JobID, len(rs))
	for i, r := range rs {
		out[i] = r.ID
	}
	return out
}

func sameIDs(t *testing.T, list string, got []jobs.Record, want ...jobs.JobID) {
	t.Helper()
	if !slices.Equal(idsOf(got), want) {
		t.Errorf("%s = %v, want %v", list, idsOf(got), want)
	}
}

// markRetrying sets last_error on a queued row, as a retryable failure or a reclaim does.
func (r *irig) markRetrying(id jobs.JobID, token string) {
	r.t.Helper()
	r.exec("UPDATE core.job SET last_error = $1 WHERE id = $2", token, [16]byte(id))
}

// F59, TestStatus_ListQueue_OneSnapshot: rows in every status land in the right list, in
// the list's own order, and done and canceled rows are in none.
func TestStatus_SplitsQueueByState(t *testing.T) {
	r, _, _ := adminIRig(t)
	at := func(m int) time.Time { return epoch.Add(time.Duration(m) * time.Minute) }

	// failed: newest finished_at first
	f1 := r.forceFinal("volunteers.remind", "f1", "failed", at(10))
	f2 := r.forceFinal("volunteers.remind", "f2", "failed", at(30))
	f3 := r.forceFinal("volunteers.remind", "f3", "failed", at(20))
	// running: earliest started_at first (started = leasedUntil - 5 min)
	r1 := r.forceRunning("volunteers.remind", "r1", at(500))
	r2 := r.forceRunning("volunteers.remind", "r2", at(100))
	// waiting: soonest run_at first
	w1 := r.forceQueued("volunteers.remind", "w1", at(900), "")
	w2 := r.forceQueued("volunteers.remind", "w2", at(700), "")
	// retrying: queued with last_error, soonest run_at first
	t1 := r.forceQueued("volunteers.remind", "t1", at(800), "")
	t2 := r.forceQueued("volunteers.remind", "t2", at(600), "")
	r.markRetrying(t1, "unavailable")
	r.markRetrying(t2, "timeout")
	// in no list
	r.forceFinal("volunteers.remind", "d1", "done", at(40))
	r.forceFinal("volunteers.remind", "c1", "canceled", at(41))

	v, err := r.run.Status(r.holder(permRead))
	wantNoErr(t, err)
	sameIDs(t, "Failed", v.Failed, f2, f3, f1)
	sameIDs(t, "Running", v.Running, r2, r1)
	sameIDs(t, "Retrying", v.Retrying, t2, t1)
	sameIDs(t, "Waiting", v.Waiting, w2, w1)
	if v.Counts[jobs.Queued] != 4 || v.Counts[jobs.Failed] != 3 || v.Counts[jobs.Running] != 2 ||
		v.Counts[jobs.Done] != 1 || v.Counts[jobs.Canceled] != 1 {
		t.Errorf("Counts = %v", v.Counts)
	}
	for _, rec := range v.Failed {
		if rec.Status != jobs.Failed {
			t.Errorf("Failed holds a %v row", rec.Status)
		}
	}
	for _, rec := range v.Waiting {
		if rec.Status != jobs.Queued || rec.LastError != "" {
			t.Errorf("Waiting holds %+v", rec)
		}
	}
	for _, rec := range v.Retrying {
		if rec.Status != jobs.Queued || rec.LastError == "" {
			t.Errorf("Retrying holds %+v", rec)
		}
	}
}

// F60: each list is capped on its own, so a long healthy queue cannot push a failure off.
func TestStatus_EachListCappedSeparately(t *testing.T) {
	r, _, _ := adminIRig(t)
	at := func(i int) time.Time { return epoch.Add(time.Duration(i) * time.Second) }
	const extra = 5
	var firstWaiting []jobs.JobID
	for i := range jobs.MaxRecent + extra {
		id := r.forceQueued("volunteers.remind", fmt.Sprintf("w%03d", i), at(10000+i), "")
		if i < jobs.MaxRecent {
			firstWaiting = append(firstWaiting, id)
		}
	}
	var newestFailed []jobs.JobID
	for i := range jobs.MaxRecent + extra {
		id := r.forceFinal("volunteers.remind", fmt.Sprintf("f%03d", i), "failed", at(i))
		newestFailed = append([]jobs.JobID{id}, newestFailed...)
	}
	var retrying []jobs.JobID
	for i := range jobs.MaxRecent + extra {
		id := r.forceQueued("volunteers.remind", fmt.Sprintf("t%03d", i), at(20000+i), "")
		r.markRetrying(id, "unavailable")
		if i < jobs.MaxRecent {
			retrying = append(retrying, id)
		}
	}
	// A healthy long queue plus few failures: the single failure is still listed.
	v, err := r.run.Status(r.holder(permRead))
	wantNoErr(t, err)
	if len(v.Waiting) != jobs.MaxRecent || len(v.Failed) != jobs.MaxRecent || len(v.Retrying) != jobs.MaxRecent {
		t.Fatalf("list sizes waiting/failed/retrying = %d/%d/%d, want %d each",
			len(v.Waiting), len(v.Failed), len(v.Retrying), jobs.MaxRecent)
	}
	sameIDs(t, "Waiting", v.Waiting, firstWaiting...)
	sameIDs(t, "Failed", v.Failed, newestFailed[:jobs.MaxRecent]...)
	sameIDs(t, "Retrying", v.Retrying, retrying...)
	// Counts are not shortened.
	if v.Counts[jobs.Queued] != 2*(jobs.MaxRecent+extra) || v.Counts[jobs.Failed] != jobs.MaxRecent+extra {
		t.Errorf("Counts = %v, want the totals", v.Counts)
	}
}

// F60, the point of decision 25: a failure survives a queue longer than the cap.
func TestStatus_FailureSurvivesLongHealthyQueue(t *testing.T) {
	r, _, _ := adminIRig(t)
	for i := range jobs.MaxRecent * 2 {
		r.forceQueued("volunteers.remind", fmt.Sprintf("w%03d", i), epoch.Add(time.Duration(i)*time.Second), "")
	}
	failed := r.forceFinal("volunteers.remind", "only", "failed", epoch.Add(-24*time.Hour))
	v, err := r.run.Status(r.holder(permRead))
	wantNoErr(t, err)
	sameIDs(t, "Failed", v.Failed, failed)
}

// F61.
func TestStatus_RequeuedRowIsRetrying(t *testing.T) {
	r, _, _ := adminIRig(t)
	id := r.forceQueued("volunteers.remind", "k", epoch.Add(time.Hour), "")
	r.markRetrying(id, "unavailable")
	v, err := r.run.Status(r.holder(permRead))
	wantNoErr(t, err)
	sameIDs(t, "Retrying", v.Retrying, id)
	sameIDs(t, "Waiting", v.Waiting)
	if len(v.Retrying) == 1 && v.Retrying[0].LastError != "unavailable" {
		t.Errorf("LastError = %q", v.Retrying[0].LastError)
	}
}

// F61: Retry clears last_error, so a retried row is Waiting.
func TestStatus_AdminRetriedRowIsWaiting(t *testing.T) {
	r, _, _ := adminIRig(t)
	id := r.forceFinal("volunteers.remind", "k", "failed", epoch)
	v, err := r.run.Status(r.holder(permRead))
	wantNoErr(t, err)
	sameIDs(t, "Failed", v.Failed, id)
	wantNoErr(t, r.run.Retry(r.holder(permManage), id))
	v, err = r.run.Status(r.holder(permRead))
	wantNoErr(t, err)
	sameIDs(t, "Failed", v.Failed)
	sameIDs(t, "Retrying", v.Retrying)
	sameIDs(t, "Waiting", v.Waiting, id)
}

// Empty store: four empty lists, never an error.
func TestStatus_EmptyQueue_FourEmptyLists(t *testing.T) {
	r, _, _ := adminIRig(t)
	v, err := r.run.Status(r.holder(permRead))
	wantNoErr(t, err)
	if len(v.Failed)+len(v.Running)+len(v.Retrying)+len(v.Waiting) != 0 {
		t.Errorf("lists = %d/%d/%d/%d on an empty queue", len(v.Failed), len(v.Running), len(v.Retrying), len(v.Waiting))
	}
}

// Decision 25: StatusView.Instance is this process's Options.Instance.
func TestStatus_Instance_IsThisServer(t *testing.T) {
	r, _, _ := adminIRig(t)
	v, err := r.run.Status(r.holder(permRead))
	wantNoErr(t, err)
	if v.Instance != "test-a" {
		t.Errorf("Instance = %q, want the runner's Options.Instance", v.Instance)
	}
}
