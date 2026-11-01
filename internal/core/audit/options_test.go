package audit_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func validOptions(t testing.TB) audit.Options {
	t.Helper()
	return newFx(t).opts
}

func TestNew_Valid_DoesNoIO(t *testing.T) {
	o := validOptions(t)
	o.DataDir = t.TempDir() + "/does/not/exist/yet"
	o.DB = db.None() // app builds audit against db.None() for bms openapi and bms check
	l, err := audit.New(o)
	wantNoErr(t, err)
	if l == nil {
		t.Fatal("nil Log")
	}
	if _, err := audit.New(o); err != nil {
		t.Fatalf("a second New failed")
	}
	// New creates nothing: the spool directory appears with Run or the first spool write.
	if _, err := os.Lstat(o.DataDir); err == nil {
		t.Error("New created the data dir")
	}
}

func TestNew_OptionalDependenciesMayBeNil(t *testing.T) {
	o := validOptions(t)
	o.Meter = nil
	o.Permissions = nil
	o.ImportInterval = 0
	_, err := audit.New(o)
	wantNoErr(t, err)
}

func TestNew_NilDependency_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name  string
		field string
		mod   func(*audit.Options)
	}{
		{"db", "audit.db", func(o *audit.Options) { o.DB = nil }},
		{"clock", "audit.clock", func(o *audit.Options) { o.Clock = nil }},
		{"logger", "audit.logger", func(o *audit.Options) { o.Logger = nil }},
		{"tracer", "audit.tracer", func(o *audit.Options) { o.Tracer = nil }},
		{"actor", "audit.actor", func(o *audit.Options) { o.Actor = nil }},
		{"titles", "audit.titles", func(o *audit.Options) { o.Titles = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := validOptions(t)
			tc.mod(&o)
			l, err := audit.New(o)
			wantKind(t, err, errs.Invalid)
			wantField(t, err, tc.field)
			if l != nil {
				t.Error("New returned a Log with its error")
			}
			wantKind(t, o.Validate(), errs.Invalid)
		})
	}
	t.Run("all nil reports every field", func(t *testing.T) {
		_, err := audit.New(audit.Options{Instance: "x", DataDir: "d"})
		wantKind(t, err, errs.Invalid)
		for _, f := range []string{"audit.db", "audit.clock", "audit.logger", "audit.tracer", "audit.actor", "audit.titles"} {
			wantField(t, err, f)
		}
	})
}

func TestNew_BadInstance_ReturnsInvalid(t *testing.T) {
	bad := []string{"", "Upper", "-lead", "trail-", "has space", "under_score", "dot.ted", strings.Repeat("a", 65)}
	for _, in := range bad {
		t.Run("instance "+in, func(t *testing.T) {
			o := validOptions(t)
			o.Instance = in
			_, err := audit.New(o)
			wantKind(t, err, errs.Invalid)
			wantField(t, err, "instance.name")
		})
	}
	good := []string{"a", "bms", "burn-2026", "0", strings.Repeat("a", 64)}
	for _, in := range good {
		t.Run("valid "+in, func(t *testing.T) {
			o := validOptions(t)
			o.Instance = in
			_, err := audit.New(o)
			wantNoErr(t, err)
		})
	}
}

func TestNew_BadDataDir_ReturnsInvalid(t *testing.T) {
	for name, dir := range map[string]string{"empty": "", "NUL": "/tmp/a\x00b"} {
		t.Run(name, func(t *testing.T) {
			o := validOptions(t)
			o.DataDir = dir
			_, err := audit.New(o)
			wantKind(t, err, errs.Invalid)
			wantField(t, err, "data.dir")
		})
	}
}

func TestNew_BadImportInterval_ReturnsInvalid(t *testing.T) {
	bad := []time.Duration{-time.Second, time.Second - 1, 10*time.Minute + 1, 24 * time.Hour}
	for _, d := range bad {
		t.Run("interval "+d.String(), func(t *testing.T) {
			o := validOptions(t)
			o.ImportInterval = d
			_, err := audit.New(o)
			wantKind(t, err, errs.Invalid)
			wantField(t, err, "audit.import_interval")
		})
	}
	for _, d := range []time.Duration{0, time.Second, 30 * time.Second, 10 * time.Minute} {
		t.Run("valid "+d.String(), func(t *testing.T) {
			o := validOptions(t)
			o.ImportInterval = d
			_, err := audit.New(o)
			wantNoErr(t, err)
		})
	}
}

func TestNew_ReportsEveryProblemAtOnce(t *testing.T) {
	o := validOptions(t)
	o.Instance = ""
	o.DataDir = ""
	o.ImportInterval = time.Hour
	err := o.Validate()
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{"instance.name", "data.dir", "audit.import_interval"} {
		wantField(t, err, f)
	}
}
