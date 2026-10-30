package demo_test

// Tests for the D56 review follow-ups of plan/design/demo.md: failure modes F40, F41 and F43.

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5" //nolint:depguard // scriptedDB must implement db.DB

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/demo"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// TestOptions_PublicSchema_ReturnsInvalid covers F40.
func TestOptions_PublicSchema_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name    string
		schemas []string
	}{
		{"only public", []string{"public"}},
		{"public first", []string{"public", "core"}},
		{"public last", []string{"core", "volunteers", "public"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := newRig(t, db.None()).opts
			o.Schemas = tc.schemas
			err := o.Validate()
			wantKind(t, err, errs.Invalid)
			found := false
			for _, f := range validate.Fields(err) {
				found = found || f.Field == "demo.schemas"
			}
			if !found {
				t.Errorf("no field error for demo.schemas in %v", validate.Fields(err))
			}
			svc, nerr := demo.New(o)
			wantKind(t, nerr, errs.Invalid)
			if svc != nil {
				t.Error("New returned a service with its error")
			}
		})
	}
}

// scriptedDB answers the marker read with a seeded marker and the drop-statement query with
// the configured statement and matched count; everything else goes to the fake.
type scriptedDB struct {
	*dbtest.Fake
	matched int64
}

func (s *scriptedDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	switch {
	case strings.Contains(sql, "DROP SCHEMA"):
		return scriptRow{func(dest ...any) error {
			stmt := `DROP SCHEMA IF EXISTS "core" CASCADE`
			return scanInto(dest, &stmt, s.matched)
		}}
	case strings.Contains(sql, "demo_state"):
		return scriptRow{func(dest ...any) error {
			seeded := seedDay
			return scanInto(dest, int32(1), "demo", seedDay, &seeded)
		}}
	}
	return s.Fake.QueryRow(ctx, sql, args...)
}

// scanInto copies vals into dest, failing on a count or type mismatch.
func scanInto(dest []any, vals ...any) error {
	if len(dest) != len(vals) {
		return fmt.Errorf("scan: %d destinations for %d values", len(dest), len(vals))
	}
	for i, v := range vals {
		dv := reflect.ValueOf(dest[i])
		if dv.Kind() != reflect.Pointer || dv.Elem().Type() != reflect.TypeOf(v) {
			return fmt.Errorf("scan: destination %d is %T, want *%T", i, dest[i], v)
		}
		dv.Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

type scriptRow struct{ fn func(dest ...any) error }

func (r scriptRow) Scan(dest ...any) error { return r.fn(dest...) }

func newScripted(matched int64) *scriptedDB {
	return &scriptedDB{Fake: dbtest.NewFake(), matched: matched}
}

// TestReset_DropCountMismatch_ReturnsInternal covers F41: the server matched fewer schemas
// than Options.Schemas names, so nothing is dropped and the migration and seed never run.
func TestReset_DropCountMismatch_ReturnsInternal(t *testing.T) {
	d := newScripted(0)
	r := newRig(t, d)
	rep, err := r.svc.Reset(ctxT(t), &strings.Builder{})
	wantKind(t, err, errs.Internal)
	for _, c := range d.Calls() {
		if strings.HasPrefix(c.SQL, "DROP SCHEMA") {
			t.Errorf("the drop statement ran despite the count mismatch: %q", c.SQL)
		}
	}
	if n := callsOf(r.log, "schema", "Migrate"); n != 0 {
		t.Errorf("Migrate ran %d times after a failed drop", n)
	}
	for _, s := range stepNames(rep) {
		if s == "drop" || s == "migrate" {
			t.Errorf("step %q reported as done", s)
		}
	}
}

// TestReset_MigrateFails_WrapsErrMigrate covers F43.
func TestReset_MigrateFails_WrapsErrMigrate(t *testing.T) {
	for _, kind := range []errs.Kind{errs.Conflict, errs.Unavailable} {
		t.Run(kind.String(), func(t *testing.T) {
			d := newScripted(1)
			r := newRig(t, d)
			r.schema.err = errs.E("db.Migrate", kind, errors.New("migrator failed"))
			_, err := r.svc.Reset(ctxT(t), &strings.Builder{})
			if !errors.Is(err, demo.ErrMigrate) {
				t.Fatalf("error does not wrap demo.ErrMigrate: %v", err)
			}
			wantKind(t, err, kind)
			if n := callsOf(r.log, "accounts", "EnsureAccount"); n != 0 {
				t.Error("the seed ran after the migration failed")
			}
		})
	}
}

// A failure before the migrate step must not carry ErrMigrate.
func TestReset_DropMismatch_NotErrMigrate(t *testing.T) {
	r := newRig(t, newScripted(0))
	_, err := r.svc.Reset(ctxT(t), &strings.Builder{})
	if errors.Is(err, demo.ErrMigrate) {
		t.Errorf("a drop failure wraps ErrMigrate: %v", err)
	}
}

// callsOf counts the calls a seam saw (unit-test twin of the integration helper).
func callsOf(l *callLog, seam, method string) int {
	n := 0
	for _, c := range l.all() {
		if c.Seam == seam && c.Method == method {
			n++
		}
	}
	return n
}
