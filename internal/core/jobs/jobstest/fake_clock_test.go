package jobstest_test

import (
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/jobs/jobstest"
	"github.com/zedgama3/bms/internal/lib/clock"
)

// Q-JOBS12: the fake validates run_at against its own clock.
func TestFake_Enqueue_RunAtBound_UsesTheFakeClock(t *testing.T) {
	// A clock two years in the future: a run_at a year past it is within MaxDelay of the
	// fake clock, so it is valid for the fake though beyond the wall-clock bound.
	past := time.Now().AddDate(2, 0, 0)
	f := jobstest.NewFake("volunteers", jobstest.WithClock(clock.NewFake(past)))
	if err := f.Register(jobs.Decl{Name: "volunteers.remind", Handler: nop}); err != nil {
		t.Fatal(err)
	}
	ctx := ctxT(t)
	if _, err := f.Enqueue(ctx, jobs.Request{Name: "volunteers.remind", RunAt: past.Add(jobs.MaxDelay - time.Hour)}); err != nil {
		t.Errorf("run_at inside MaxDelay of the fake clock was refused: %v", err)
	}
	_, err := f.Enqueue(ctx, jobs.Request{Name: "volunteers.remind", RunAt: past.Add(jobs.MaxDelay + 48*time.Hour)})
	wantField(t, err, "run_at", "")
	// Default (no option) keeps the wall clock.
	g := jobstest.NewFake("volunteers")
	if err := g.Register(jobs.Decl{Name: "volunteers.remind", Handler: nop}); err != nil {
		t.Fatal(err)
	}
	_, err = g.Enqueue(ctx, jobs.Request{Name: "volunteers.remind", RunAt: time.Now().Add(jobs.MaxDelay + 48*time.Hour)})
	wantField(t, err, "run_at", "")
}
