//go:build integration

package jobs_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/model"
)

type noTitles struct{}

func (noTitles) TitlesOf(context.Context, model.PersonID) ([]string, error) { return nil, nil }

// Every run's ctx carries audit's system marker (decision 6, boundaries step 3): an entry a
// handler records with no actor named is the system's, though the audit log's own actor
// function answers nothing. Two jobs, so it is every run and not the first.
func TestRun_HandlerContext_CarriesTheSystemMarker(t *testing.T) {
	dir := t.TempDir()
	fdb := dbtest.NewFake()
	fdb.SetReadOnly(true) // the entry spools, so no audit schema is needed
	c := telemetrytest.New(t)
	log, err := audit.New(audit.Options{
		DB: fdb, Clock: clock.NewFake(epoch), Logger: c.Logger(),
		Tracer: c.Tracer("audit"), Meter: c.Meter("audit"),
		Instance: "test-copy", DataDir: dir, Titles: noTitles{},
		Actor: func(context.Context) (audit.Actor, bool) { return audit.Actor{}, false },
	})
	wantNoErr(t, err)
	t.Cleanup(func() {
		if err := log.Close(context.Background()); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	rec, err := log.Scoped("identity")
	wantNoErr(t, err)

	r := newIRig(t)
	var recordErrs []error
	handler := func(ctx context.Context, _ jobs.Job) error {
		err := rec.Record(ctx, audit.Entry{Action: "identity.login", Outcome: audit.Succeeded})
		recordErrs = append(recordErrs, err)
		return nil
	}
	s := r.register(jobs.Decl{Name: "volunteers.one", Handler: handler})
	s2 := r.register(jobs.Decl{Name: "volunteers.two", Handler: handler})
	enqueue(t, s, jobs.Request{Name: "volunteers.one", Key: "k-1", RunAt: epoch.Add(-1)})
	enqueue(t, s2, jobs.Request{Name: "volunteers.two", Key: "k-2", RunAt: epoch.Add(-1)})
	if ran := r.runOnce(); ran.Ran < 2 {
		t.Fatalf("Ran = %+v, want both jobs", ran)
	}
	for i, err := range recordErrs {
		if err != nil {
			t.Errorf("run %d: Record = %v, want nil", i, err)
		}
	}
	files, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(audit.SpoolDirName), "*"))
	wantNoErr(t, err)
	var all strings.Builder
	for _, f := range files {
		b, err := os.ReadFile(f)
		wantNoErr(t, err)
		all.Write(b)
	}
	// A missing marker would not fail Record: the entry would spool as an anonymous actor.
	if n := strings.Count(all.String(), `"kind":"system"`); n != 2 {
		t.Errorf("%d system entries spooled, want 2:\n%s", n, all.String())
	}
}
