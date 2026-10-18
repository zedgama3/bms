package dbtest_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/lib/errs"
)

var _ db.DB = (*dbtest.Fake)(nil)

func wantKind(t *testing.T, err error, kind errs.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("got nil error, want kind %v", kind)
	}
	if !errs.Is(err, kind) {
		t.Fatalf("got kind %v, want %v", errs.KindOf(err), kind)
	}
}

func wantReadOnly(t *testing.T, err error) {
	t.Helper()
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("error does not wrap db.ErrReadOnly")
	}
}

func TestNewFake_IsWritableAndEmpty(t *testing.T) {
	f := dbtest.NewFake()
	if f.ReadOnly() {
		t.Errorf("a new Fake must be writable")
	}
	if n := len(f.Calls()); n != 0 {
		t.Errorf("a new Fake has %d calls", n)
	}
}

func TestFake_RecordsCallsInOrderWithInTx(t *testing.T) {
	f := dbtest.NewFake()
	ctx := context.Background()
	if _, err := f.Exec(ctx, "INSERT 1", "a", 1); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	err := f.Transact(ctx, db.Write, func(tx context.Context) error {
		if _, err := f.Exec(tx, "INSERT 2", "b"); err != nil {
			return err //nolint:wrapcheck // test closure
		}
		var n int
		_ = f.QueryRow(tx, "SELECT 3").Scan(&n) //nolint:errcheck // NotFound is the fake's answer
		return nil
	})
	if err != nil {
		t.Fatalf("Transact: %v", err)
	}
	if _, err := f.Exec(ctx, "INSERT 4"); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	calls := f.Calls()
	if len(calls) < 3 {
		t.Fatalf("got %d calls: %+v", len(calls), calls)
	}
	var sqls []string
	byIdx := map[string]dbtest.Call{}
	for _, c := range calls {
		sqls = append(sqls, c.SQL)
		byIdx[c.SQL] = c
	}
	pos := func(s string) int {
		for i, x := range sqls {
			if x == s {
				return i
			}
		}
		return -1
	}
	if pos("INSERT 1") >= pos("INSERT 2") || pos("INSERT 2") >= pos("INSERT 4") {
		t.Errorf("calls out of order: %v", sqls)
	}
	if c := byIdx["INSERT 1"]; c.InTx || len(c.Args) != 2 || c.Args[0] != "a" || c.Args[1] != 1 {
		t.Errorf("INSERT 1 recorded as %+v", c)
	}
	if c := byIdx["INSERT 2"]; !c.InTx || len(c.Args) != 1 || c.Args[0] != "b" {
		t.Errorf("INSERT 2 recorded as %+v", c)
	}
	if c, ok := byIdx["SELECT 3"]; ok && !c.InTx {
		t.Errorf("SELECT 3 ran in a transaction")
	}
	if c := byIdx["INSERT 4"]; c.InTx {
		t.Errorf("INSERT 4 ran outside a transaction")
	}
}

func TestFake_CallsReturnsACopy(t *testing.T) {
	f := dbtest.NewFake()
	if _, err := f.Exec(context.Background(), "INSERT 1"); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	got := f.Calls()
	got[0].SQL = "tampered"
	if f.Calls()[0].SQL != "INSERT 1" {
		t.Fatalf("Calls must return a copy")
	}
}

func TestFake_QueryReturnsNoRows(t *testing.T) {
	f := dbtest.NewFake()
	rows, err := f.Query(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Errorf("a Fake holds no rows")
	}
	if err := rows.Err(); err != nil {
		t.Errorf("Rows.Err: %v", err)
	}
}

// F53
func TestFake_QueryRow_ReturnsNotFound(t *testing.T) {
	f := dbtest.NewFake()
	var n int
	wantKind(t, f.QueryRow(context.Background(), "SELECT 1").Scan(&n), errs.NotFound)
	// also inside a transaction
	err := f.Transact(context.Background(), db.Read, func(tx context.Context) error {
		return f.QueryRow(tx, "SELECT 1").Scan(&n) //nolint:wrapcheck // test closure
	})
	wantKind(t, err, errs.NotFound)
}

func TestFake_TransactRunsHooksAfterFn(t *testing.T) {
	f := dbtest.NewFake()
	var order []string
	err := f.Transact(context.Background(), db.Write, func(tx context.Context) error {
		for _, n := range []string{"h1", "h2", "h3"} {
			if err := db.AfterCommit(tx, func(context.Context) { order = append(order, n) }); err != nil {
				return err //nolint:wrapcheck // test closure
			}
		}
		order = append(order, "fn")
		return nil
	})
	if err != nil {
		t.Fatalf("Transact: %v", err)
	}
	want := []string{"fn", "h1", "h2", "h3"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestFake_TransactFnError_ReturnedUnchangedAndHooksSkipped(t *testing.T) {
	f := dbtest.NewFake()
	cause := errs.E("test.fn", errs.Conflict, errors.New("dup"))
	hook := false
	err := f.Transact(context.Background(), db.Write, func(tx context.Context) error {
		if e := db.AfterCommit(tx, func(context.Context) { hook = true }); e != nil {
			return e //nolint:wrapcheck // test closure
		}
		return cause
	})
	if !errors.Is(err, cause) {
		t.Fatalf("fn's error must come back unchanged, got %v", err)
	}
	wantKind(t, err, errs.Conflict)
	if hook {
		t.Errorf("hooks must not run when fn fails")
	}
}

func TestFake_NestedTransactJoinsOuter(t *testing.T) {
	f := dbtest.NewFake()
	var order []string
	err := f.Transact(context.Background(), db.Write, func(tx context.Context) error {
		inner := f.Transact(tx, db.Write, func(tx2 context.Context) error {
			if _, err := f.Exec(tx2, "INSERT inner"); err != nil {
				return err //nolint:wrapcheck // test closure
			}
			return db.AfterCommit(tx2, func(context.Context) { order = append(order, "hook") }) //nolint:wrapcheck // test closure
		})
		order = append(order, "after-inner")
		return inner //nolint:wrapcheck // test closure
	})
	if err != nil {
		t.Fatalf("Transact: %v", err)
	}
	if len(order) != 2 || order[0] != "after-inner" || order[1] != "hook" {
		t.Fatalf("hooks must run once, after the outermost fn: %v", order)
	}
	for _, c := range f.Calls() {
		if c.SQL == "INSERT inner" && !c.InTx {
			t.Errorf("inner statement must be InTx")
		}
	}
}

// F21 on the fake
func TestFake_Transact_InvalidModeOrNilFn_ReturnsInvalid(t *testing.T) {
	f := dbtest.NewFake()
	ok := func(context.Context) error { return nil }
	wantKind(t, f.Transact(context.Background(), db.Mode(0), ok), errs.Invalid)
	wantKind(t, f.Transact(context.Background(), db.Mode(9), ok), errs.Invalid)
	wantKind(t, f.Transact(context.Background(), db.Write, nil), errs.Invalid)
}

// F52
func TestFake_ReadOnly_RefusesWrites(t *testing.T) {
	f := dbtest.NewFake()
	f.SetReadOnly(true)
	if !f.ReadOnly() {
		t.Fatalf("ReadOnly() must report what SetReadOnly set")
	}
	ctx := context.Background()

	_, err := f.Exec(ctx, "INSERT 1")
	wantReadOnly(t, err)

	called, hook := false, false
	err = f.Transact(ctx, db.Write, func(tx context.Context) error {
		called = true
		return db.AfterCommit(tx, func(context.Context) { hook = true }) //nolint:wrapcheck // test closure
	})
	wantReadOnly(t, err)
	if called || hook {
		t.Errorf("fn and hooks must not run for a refused Write transaction")
	}

	// Reads still work.
	err = f.Transact(ctx, db.Read, func(context.Context) error { called = true; return nil })
	if err != nil || !called {
		t.Errorf("a Read transaction must run on a read-only fake: %v", err)
	}
	if rows, err := f.Query(ctx, "SELECT 1"); err != nil {
		t.Errorf("Query on read-only: %v", err)
	} else {
		rows.Close()
	}

	// And it can be switched back.
	f.SetReadOnly(false)
	if f.ReadOnly() {
		t.Errorf("ReadOnly() must be false again")
	}
	if _, err := f.Exec(ctx, "INSERT 2"); err != nil {
		t.Errorf("Exec after SetReadOnly(false): %v", err)
	}
}

func TestAfterCommit_OutsideTransaction_RunsAtOnce(t *testing.T) {
	ran := false
	if err := db.AfterCommit(context.Background(), func(context.Context) { ran = true }); err != nil || !ran {
		t.Fatalf("AfterCommit outside a transaction: ran=%v err=%v", ran, err)
	}
}

func TestFake_SafeForConcurrentUse(t *testing.T) {
	f := dbtest.NewFake()
	const workers, each = 16, 25
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			for i := 0; i < each; i++ {
				if _, err := f.Exec(ctx, "INSERT x", i); err != nil {
					t.Errorf("Exec: %v", err)
					return
				}
				err := f.Transact(ctx, db.Write, func(tx context.Context) error {
					_, e := f.Exec(tx, "INSERT y")
					return e //nolint:wrapcheck // test closure
				})
				if err != nil {
					t.Errorf("Transact: %v", err)
					return
				}
				_ = f.Calls()
				_ = f.ReadOnly()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < each; i++ {
			f.SetReadOnly(false)
		}
	}()
	wg.Wait()
	if got, want := len(f.Calls()), workers*each*2; got != want {
		t.Fatalf("recorded %d calls, want %d", got, want)
	}
}

func TestTxMode_InsideFakeTransaction_ReportsMode(t *testing.T) {
	type key struct{}
	f := dbtest.NewFake()
	for _, mode := range []db.Mode{db.Read, db.Write} {
		t.Run(mode.String(), func(t *testing.T) {
			ran := false
			err := f.Transact(context.Background(), mode, func(ctx context.Context) error {
				ran = true
				if got, ok := db.TxMode(ctx); !ok || got != mode {
					t.Errorf("TxMode = (%v, %v), want (%v, true)", got, ok, mode)
				}
				// A nested Read joins and reports the outer's mode.
				return f.Transact(ctx, db.Read, func(inner context.Context) error {
					if got, ok := db.TxMode(inner); !ok || got != mode {
						t.Errorf("nested TxMode = (%v, %v), want (%v, true)", got, ok, mode)
					}
					child := context.WithValue(inner, key{}, "v")
					if got, ok := db.TxMode(child); !ok || got != mode {
						t.Errorf("child TxMode = (%v, %v), want (%v, true)", got, ok, mode)
					}
					return nil
				})
			})
			if err != nil || !ran {
				t.Fatalf("Transact err %v, ran %v", err, ran)
			}
		})
	}
}
