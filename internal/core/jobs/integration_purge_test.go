//go:build integration

package jobs_test

import (
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/jobs"
)

func TestPurge_DeletesOnlyFinalRowsOlderThanDoneTTL(t *testing.T) {
	r := newIRig(t)
	old := epoch.Add(-jobs.DoneTTL - time.Hour)
	recent := epoch.Add(-jobs.DoneTTL + time.Hour)

	oldDone := r.forceFinal("volunteers.old", "od", "done", old)
	oldFailed := r.forceFinal("volunteers.old", "of", "failed", old)
	oldCanceled := r.forceFinal("volunteers.old", "oc", "canceled", old)
	newDone := r.forceFinal("volunteers.old", "nd", "done", recent)
	newFailed := r.forceFinal("volunteers.old", "nf", "failed", epoch.Add(-time.Minute))
	// Open rows are never purged, however old they are.
	queued := r.forceQueued("volunteers.old", "q", epoch.Add(24*time.Hour), "")
	r.exec("UPDATE core.job SET created_at = $1, updated_at = $1 WHERE id = $2", old, [16]byte(queued))
	running := r.forceRunning("volunteers.old", "r", epoch.Add(time.Hour))

	ran := r.runOnce() // jobs.purge is hourly: its slot is due at the first tick
	if ran.Ran < 1 {
		t.Fatalf("Ran = %+v, jobs.purge should have run", ran)
	}
	if p := r.one(jobs.NamePurge); p.Status != "done" {
		t.Fatalf("jobs.purge row = %+v", p)
	}
	gone := []jobs.JobID{oldDone, oldFailed, oldCanceled}
	kept := []jobs.JobID{newDone, newFailed, queued, running}
	for _, id := range gone {
		if n := r.count("id = $1", [16]byte(id)); n != 0 {
			t.Errorf("old final row %v survived the purge", id)
		}
	}
	for _, id := range kept {
		if n := r.count("id = $1", [16]byte(id)); n != 1 {
			t.Errorf("row %v was purged but should have been kept", id)
		}
	}
}

func TestPurge_MoreThanOneBatch_DeletesAll(t *testing.T) {
	r := newIRig(t)
	old := epoch.Add(-jobs.DoneTTL - 2*time.Hour)
	total := 2*jobs.MaxBatch + 500
	r.exec(`INSERT INTO core.job (id, name, key, args, status, attempts, run_at, started_at, finished_at, created_at, updated_at)
		SELECT gen_random_uuid(), 'volunteers.old', 'k' || g, '{}'::jsonb, 'done', 1, $1, $1, $1, $1, $1
		FROM generate_series(1, $2::int) g`, old, total)
	keep := r.forceFinal("volunteers.old", "keep", "done", epoch.Add(-time.Hour))
	if n := r.count("name = 'volunteers.old'"); n != total+1 {
		t.Fatalf("setup inserted %d rows", n)
	}
	r.runOnce()
	if n := r.count("name = 'volunteers.old'"); n != 1 {
		t.Fatalf("%d old rows left after the purge, want only the recent one", n)
	}
	if r.count("id = $1", [16]byte(keep)) != 1 {
		t.Error("the recent row was purged")
	}
}

func TestPurge_ReadOnly_DeletesNothing(t *testing.T) {
	r := newIRig(t)
	r.forceFinal("volunteers.old", "od", "done", epoch.Add(-jobs.DoneTTL-time.Hour))
	r.flt.set(true, nil)
	_, err := r.run.RunOnce(ctxT(t))
	wantReadOnly(t, err)
	r.flt.set(false, nil)
	if n := r.count(""); n != 1 {
		t.Fatalf("%d rows, want the old row untouched", n)
	}
}

func TestPurge_PurgedKey_StartsAFreshRow(t *testing.T) {
	// A finished row is reopened by its key until the purge removes it (J1, F43); after the
	// purge the key starts a new row.
	r := newIRig(t)
	s := r.register(jobs.Decl{Name: "volunteers.remind", Handler: nop})
	id := r.forceFinal("volunteers.remind", "k", "done", epoch.Add(-time.Hour))
	got, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind", Key: "k", RunAt: epoch.Add(time.Hour)})
	wantNoErr(t, err)
	if got != id || r.byID(id).Status != "queued" {
		t.Fatalf("Enqueue returned %v (status %s), want the finished row %v reopened", got, r.byID(id).Status, id)
	}
	old := r.forceFinal("volunteers.remind", "k2", "done", epoch.Add(-time.Hour))
	r.advance(jobs.DoneTTL + 2*time.Hour)
	r.runOnce() // purge removes the finished row
	fresh, err := s.Enqueue(ctxT(t), jobs.Request{Name: "volunteers.remind", Key: "k2"})
	wantNoErr(t, err)
	if fresh == old {
		t.Fatal("the purged key still reached the old row")
	}
}
