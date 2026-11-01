package jobs_test

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/clock"
)

var _ signals.Deferrer = (*jobs.Deferrer)(nil)

func deferrerOptions(d db.DB) jobs.DeferrerOptions {
	return jobs.DeferrerOptions{DB: d, Clock: clock.NewFake(epoch), Instance: "test-a"}
}

// B27: a nil dependency or a bad instance name fails the build, Invalid.
func TestNewDeferrer_NilDB_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name  string
		mod   func(*jobs.DeferrerOptions)
		field string
	}{
		{"nil db", func(o *jobs.DeferrerOptions) { o.DB = nil }, "jobs.db"},
		{"nil clock", func(o *jobs.DeferrerOptions) { o.Clock = nil }, "jobs.clock"},
		{"malformed instance", func(o *jobs.DeferrerOptions) { o.Instance = "Bad_Name" }, "jobs.instance"},
		{"empty instance", func(o *jobs.DeferrerOptions) { o.Instance = "" }, "jobs.instance"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := deferrerOptions(dbtest.NewFake())
			tc.mod(&o)
			d, err := jobs.NewDeferrer(o)
			wantField(t, err, tc.field, "")
			if d != nil {
				t.Error("a Deferrer came back with the error")
			}
		})
	}
}

func TestNewDeferrer_Valid_DoesNoIO(t *testing.T) {
	f := dbtest.NewFake()
	d, err := jobs.NewDeferrer(deferrerOptions(f))
	wantNoErr(t, err)
	if d == nil {
		t.Fatal("nil Deferrer")
	}
	if n := len(f.Calls()); n != 0 {
		t.Errorf("building touched the database %d times", n)
	}
}

// B28: on a read-only copy Defer fails Unavailable wrapping db.ErrReadOnly and stores nothing.
func TestDefer_ReadOnlyCopy_ReturnsUnavailable(t *testing.T) {
	f := dbtest.NewFake()
	f.SetReadOnly(true)
	d, err := jobs.NewDeferrer(deferrerOptions(f))
	wantNoErr(t, err)
	wantReadOnly(t, d.Defer(ctxT(t), delivery(1)))
	for _, c := range f.Calls() {
		if strings.Contains(strings.ToUpper(c.SQL), "INSERT") {
			t.Errorf("a write reached the read-only database: %q", c.SQL)
		}
	}
}
