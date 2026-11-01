package auditlog

import (
	"context"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/jobs/jobstest"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// F40: purge refuses a zero or future cutoff before it touches the database or records
// anything.
func TestPurge_FutureOrZeroBefore_ReturnsInvalid_NothingDeleted(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		before time.Time
	}{
		{"zero", time.Time{}},
		{"future", now.Add(time.Nanosecond)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := dbtest.NewFake()
			rec := audittest.NewFake("audit")
			tel := telemetrytest.New(t)
			svc, err := New(Options{
				DB:       fake,
				Clock:    clock.NewFake(now),
				Logger:   tel.Logger(),
				Tracer:   tel.Tracer("audit"),
				Authz:    authztest.NewFake(),
				Actor:    func(context.Context) (authz.Actor, bool) { return authz.Actor{}, false },
				Settings: settingstest.NewFake("audit", Settings),
				Jobs:     jobstest.NewFake("audit"),
				Audit:    rec,
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			n, err := svc.purge(t.Context(), tc.before)
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("kind = %v, want Invalid (err %v)", errs.KindOf(err), err)
			}
			if n != 0 {
				t.Errorf("deleted = %d, want 0", n)
			}
			if got := len(fake.Calls()); got != 0 {
				t.Errorf("ran %d statements, want none", got)
			}
			if got := rec.Recorded(); len(got) != 0 {
				t.Errorf("recorded %d entries, want none", len(got))
			}
		})
	}
}
