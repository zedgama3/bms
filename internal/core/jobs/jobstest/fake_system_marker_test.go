package jobstest_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/jobs/jobstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/model"
)

// Fake.Run hands the handler a ctx with audit's system marker, as the real runner does
// (decision 6, boundaries step 3): an entry that names no actor is the system's even though
// the audit log's own actor function answers nothing.
func TestFakeRun_HandlerContext_CarriesTheSystemMarker(t *testing.T) {
	dir := t.TempDir()
	fdb := dbtest.NewFake()
	fdb.SetReadOnly(true) // the entry spools, so no audit schema is needed
	c := telemetrytest.New(t)
	log, err := audit.New(audit.Options{
		DB:       fdb,
		Clock:    clock.NewFake(time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)),
		Logger:   c.Logger(),
		Tracer:   c.Tracer("audit"),
		Meter:    c.Meter("audit"),
		Instance: "test-copy",
		DataDir:  dir,
		Titles:   noTitles{},
		Actor:    func(context.Context) (audit.Actor, bool) { return audit.Actor{}, false },
	})
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() {
		if err := log.Close(context.Background()); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	rec, err := log.Scoped("identity")
	if err != nil {
		t.Fatalf("Scoped: %v", err)
	}

	const name jobs.Name = "volunteers.mark"
	f := jobstest.NewFake("volunteers")
	var recordErr error
	err = f.Register(jobs.Decl{Name: name, Handler: func(ctx context.Context, _ jobs.Job) error {
		recordErr = rec.Record(ctx, audit.Entry{Action: "identity.login", Outcome: audit.Succeeded})
		return nil
	}})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := f.Run(ctxT(t), name, jobs.Job{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if recordErr != nil {
		t.Fatalf("Record = %v, want nil", recordErr)
	}
	files, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(audit.SpoolDirName), "*"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	var all strings.Builder
	for _, p := range files {
		b, err := os.ReadFile(p) //nolint:gosec // a file in this test's own temp spool
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		all.Write(b)
	}
	if n := strings.Count(all.String(), `"kind":"system"`); n != 1 {
		t.Errorf("%d system entries spooled, want 1:\n%s", n, all.String())
	}
}

// noTitles is a TitleSource that knows nobody (audit.Options.Titles is required).
type noTitles struct{}

func (noTitles) TitlesOf(context.Context, model.PersonID) ([]string, error) { return nil, nil }
