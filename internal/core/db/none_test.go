package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func wantNoDatabase(t *testing.T, err error) {
	t.Helper()
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrNoDatabase) {
		t.Fatalf("error does not wrap db.ErrNoDatabase")
	}
}

func TestNone_EveryStatementRefusesWithErrNoDatabase(t *testing.T) {
	d := db.None()
	ctx := context.Background()

	t.Run("Exec", func(t *testing.T) {
		_, err := d.Exec(ctx, "SELECT 1")
		wantNoDatabase(t, err)
	})
	t.Run("Query", func(t *testing.T) {
		rows, err := d.Query(ctx, "SELECT 1")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				t.Errorf("a None DB must hold no rows")
			}
			err = rows.Err()
		}
		wantNoDatabase(t, err)
	})
	t.Run("QueryRow", func(t *testing.T) {
		var n int
		wantNoDatabase(t, d.QueryRow(ctx, "SELECT 1").Scan(&n))
	})
	t.Run("Transact", func(t *testing.T) {
		called := false
		err := d.Transact(ctx, db.Write, func(context.Context) error { called = true; return nil })
		wantNoDatabase(t, err)
		if called {
			t.Errorf("fn must not run")
		}
		err = d.Transact(ctx, db.Read, func(context.Context) error { called = true; return nil })
		wantNoDatabase(t, err)
	})
}

func TestNone_ReadOnlyIsFalse(t *testing.T) {
	if db.None().ReadOnly() {
		t.Fatalf("None().ReadOnly() must be false")
	}
}

// F27
func TestAfterCommit_NilFn_ReturnsInvalid(t *testing.T) {
	wantKind(t, db.AfterCommit(context.Background(), nil), errs.Invalid)
}

func TestAfterCommit_OutsideTransaction_RunsImmediately(t *testing.T) {
	ran := 0
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "v")
	err := db.AfterCommit(ctx, func(c context.Context) {
		ran++
		if c.Value(key{}) != "v" {
			t.Errorf("hook must get the caller's ctx unchanged")
		}
	})
	if err != nil {
		t.Fatalf("AfterCommit: %v", err)
	}
	if ran != 1 {
		t.Fatalf("hook ran %d times before AfterCommit returned, want 1", ran)
	}
}
