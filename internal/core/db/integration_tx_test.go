//go:build integration

package db_test

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

type ctxKey struct{}

// F21
func TestTransact_InvalidModeOrNilFn_ReturnsInvalid(t *testing.T) {
	p, _ := tempDB(t)
	ok := func(context.Context) error { return nil }
	wantKind(t, p.Transact(ctxT(t), db.Mode(0), ok), errs.Invalid)
	wantKind(t, p.Transact(ctxT(t), db.Mode(9), ok), errs.Invalid)
	wantKind(t, p.Transact(ctxT(t), db.Write, nil), errs.Invalid)
}

func TestTransact_CommitRunsHooksInOrderWithCallerCtx(t *testing.T) {
	p, _ := tempDB(t)
	mustExec(t, p, "CREATE TABLE hk (n integer)")
	ctx := context.WithValue(ctxT(t), ctxKey{}, "caller")
	var order []int
	hookCtxOK := true
	err := p.Transact(ctx, db.Write, func(tx context.Context) error {
		if _, err := p.Exec(tx, "INSERT INTO hk VALUES (1)"); err != nil {
			return err //nolint:wrapcheck // test closure
		}
		for i := 1; i <= 3; i++ {
			if err := db.AfterCommit(tx, func(c context.Context) {
				order = append(order, i)
				if c.Value(ctxKey{}) != "caller" {
					hookCtxOK = false
				}
				// The hook runs after commit: the row is visible and the ctx is not the
				// (finished) transaction's, so a statement on it autocommits.
				var n int
				if err := p.QueryRow(c, "SELECT count(*) FROM hk WHERE n = 1").Scan(&n); err != nil || n != 1 {
					t.Errorf("hook %d: count=%d err=%v", i, n, err)
				}
				if _, err := p.Exec(c, "INSERT INTO hk VALUES ($1)", 100+i); err != nil {
					t.Errorf("hook %d autocommit: %v", i, err)
				}
			}); err != nil {
				return err //nolint:wrapcheck // test closure
			}
		}
		if len(order) != 0 {
			t.Errorf("hooks ran before commit")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Transact: %v", err)
	}
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("hook order = %v", order)
	}
	if !hookCtxOK {
		t.Errorf("hooks must get the caller's ctx")
	}
}

func TestTransact_NestedJoinsOuter(t *testing.T) {
	p, _ := tempDB(t)
	mustExec(t, p, "CREATE TABLE ne (n integer)")

	t.Run("inner sees outer's uncommitted row and shares the commit", func(t *testing.T) {
		hooks := 0
		err := p.Transact(ctxT(t), db.Write, func(tx context.Context) error {
			if _, err := p.Exec(tx, "INSERT INTO ne VALUES (1)"); err != nil {
				return err //nolint:wrapcheck // test closure
			}
			if outside := count(t, p, "SELECT count(*) FROM ne"); outside != 0 { //nolint:contextcheck // a fresh ctx is intended: outside the transaction
				t.Errorf("uncommitted row visible outside the transaction")
			}
			return p.Transact(tx, db.Write, func(tx2 context.Context) error { //nolint:wrapcheck // test closure
				var n int
				if err := p.QueryRow(tx2, "SELECT count(*) FROM ne").Scan(&n); err != nil {
					return err //nolint:wrapcheck // test closure
				}
				if n != 1 {
					t.Errorf("inner transaction sees %d rows, want 1", n)
				}
				return db.AfterCommit(tx2, func(context.Context) { hooks++ }) //nolint:wrapcheck // test closure
			})
		})
		if err != nil {
			t.Fatalf("Transact: %v", err)
		}
		if hooks != 1 {
			t.Errorf("hook ran %d times, want 1", hooks)
		}
		if n := count(t, p, "SELECT count(*) FROM ne"); n != 1 {
			t.Errorf("rows = %d, want 1", n)
		}
	})

	t.Run("inner error rolls back both", func(t *testing.T) {
		mustExec(t, p, "TRUNCATE ne")
		hooks := 0
		boom := errs.E("test.inner", errs.Conflict, errors.New("inner failed"))
		err := p.Transact(ctxT(t), db.Write, func(tx context.Context) error {
			if _, err := p.Exec(tx, "INSERT INTO ne VALUES (10)"); err != nil {
				return err //nolint:wrapcheck // test closure
			}
			if err := db.AfterCommit(tx, func(context.Context) { hooks++ }); err != nil {
				return err //nolint:wrapcheck // test closure
			}
			return p.Transact(tx, db.Write, func(tx2 context.Context) error { //nolint:wrapcheck // test closure
				if _, err := p.Exec(tx2, "INSERT INTO ne VALUES (11)"); err != nil {
					return err //nolint:wrapcheck // test closure
				}
				return boom
			})
		})
		if !errors.Is(err, boom) {
			t.Fatalf("want the inner error back, got %v", err)
		}
		wantKind(t, err, errs.Conflict)
		if n := count(t, p, "SELECT count(*) FROM ne"); n != 0 {
			t.Errorf("rows = %d after rollback, want 0", n)
		}
		if hooks != 0 {
			t.Errorf("hooks ran for a rolled-back transaction")
		}
	})
}

// F22
func TestTransact_WriteOnReadOnly_FailsFastWithErrReadOnly(t *testing.T) {
	admin, name := tempDB(t)
	p := readOnlyPool(t, admin, name)
	if !p.ReadOnly() {
		t.Fatalf("ReadOnly() must be true after Open on a read-only database")
	}
	called, hook := false, false
	err := p.Transact(ctxT(t), db.Write, func(tx context.Context) error {
		called = true
		return db.AfterCommit(tx, func(context.Context) { hook = true }) //nolint:wrapcheck // test closure
	})
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Errorf("error does not wrap db.ErrReadOnly")
	}
	if called || hook {
		t.Errorf("fn and hooks must not run")
	}
}

func TestTransact_ReadModeOnReadOnlyDatabase_Works(t *testing.T) {
	admin, name := tempDB(t)
	mustExec(t, admin, "CREATE TABLE ro (n integer)")
	p := readOnlyPool(t, admin, name)
	var n int
	err := p.Transact(ctxT(t), db.Read, func(tx context.Context) error {
		return p.QueryRow(tx, "SELECT count(*) FROM ro").Scan(&n) //nolint:wrapcheck // test closure
	})
	if err != nil {
		t.Fatalf("Read transaction on a read-only database: %v", err)
	}
}

func TestTransact_ReadModeRefusesWrites(t *testing.T) {
	p, _ := tempDB(t)
	mustExec(t, p, "CREATE TABLE rm (n integer)")
	err := p.Transact(ctxT(t), db.Read, func(tx context.Context) error {
		_, e := p.Exec(tx, "INSERT INTO rm VALUES (1)")
		return e //nolint:wrapcheck // test closure
	})
	// A write in a Read transaction is the caller's bug, not a read-only copy.
	wantKind(t, err, errs.Internal)
	if errors.Is(err, db.ErrReadOnly) {
		t.Errorf("must not wrap ErrReadOnly")
	}
	if p.ReadOnly() {
		t.Errorf("must not flip ReadOnly()")
	}
}

// Write nested in Read is a caller bug: Invalid, nothing runs.
func TestTransact_WriteNestedInRead_ReturnsInvalid(t *testing.T) {
	p, _ := tempDB(t)
	f := dbtest.NewFake()
	for name, d := range map[string]db.DB{"pool": p, "fake": f} {
		t.Run(name, func(t *testing.T) {
			ran := false
			err := d.Transact(ctxT(t), db.Read, func(tx context.Context) error {
				return d.Transact(tx, db.Write, func(context.Context) error { ran = true; return nil }) //nolint:wrapcheck // test closure
			})
			wantKind(t, err, errs.Invalid)
			if ran {
				t.Errorf("the nested write fn must not run")
			}
			// Read nested in Write, and Read in Read, join.
			err = d.Transact(ctxT(t), db.Write, func(tx context.Context) error {
				return d.Transact(tx, db.Read, func(context.Context) error { return nil }) //nolint:wrapcheck // test closure
			})
			if err != nil {
				t.Errorf("Read nested in Write: %v", err)
			}
		})
	}
}

// AfterCommit with the ctx of a transaction that has ended without committing is Invalid.
func TestAfterCommit_AfterTransactionRolledBack_ReturnsInvalid(t *testing.T) {
	p, _ := tempDB(t)
	f := dbtest.NewFake()
	for name, d := range map[string]db.DB{"pool": p, "fake": f} {
		t.Run(name, func(t *testing.T) {
			var txCtx context.Context
			boom := errors.New("fail")
			err := d.Transact(ctxT(t), db.Write, func(tx context.Context) error {
				txCtx = tx
				return boom
			})
			if !errors.Is(err, boom) {
				t.Fatalf("Transact: %v", err)
			}
			ran := false
			wantKind(t, db.AfterCommit(txCtx, func(context.Context) { ran = true }), errs.Invalid)
			if ran {
				t.Errorf("hook must not run")
			}
		})
	}
}

// A connection already warm in the pool must be noticed read-only too.
func TestReadOnly_WarmConnections_NoticedWithinOneProbe(t *testing.T) {
	for _, via := range []string{"watcher", "ping"} {
		t.Run(via, func(t *testing.T) {
			admin, name := tempDB(t)
			o := optionsFor(t, name)
			o.MaxConns = 3
			clk := clock.NewFake(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
			p := openWith(t, o, clk)
			warm(t, p, 3)
			if p.ReadOnly() {
				t.Fatalf("writable before the flip")
			}
			setReadOnly(t, admin, name, true)
			if via == "ping" {
				if err := p.Ping(ctxT(t)); err != nil {
					t.Fatalf("Ping: %v", err)
				}
			} else {
				if err := clk.BlockUntil(ctxT(t), 1); err != nil {
					t.Fatalf("watcher ticker: %v", err)
				}
				if err := clk.Advance(15 * time.Second); err != nil {
					t.Fatalf("advance: %v", err)
				}
				waitReadOnly(t, p, true)
			}
			if !p.ReadOnly() {
				t.Errorf("a warm connection must be noticed as read-only")
			}
		})
	}
}

// warm opens n connections of p at once and returns them to the pool.
func warm(t *testing.T, p *db.Pool, n int) {
	t.Helper()
	var held, release sync.WaitGroup
	release.Add(1)
	held.Add(n)
	errc := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			errc <- p.Transact(ctxT(t), db.Read, func(tx context.Context) error {
				var one int
				if err := p.QueryRow(tx, "SELECT 1").Scan(&one); err != nil {
					held.Done()
					return err //nolint:wrapcheck // test closure
				}
				held.Done()
				release.Wait()
				return nil
			})
		}()
	}
	held.Wait()
	release.Done()
	for i := 0; i < n; i++ {
		if err := <-errc; err != nil {
			t.Fatalf("warm: %v", err)
		}
	}
}

// waitReadOnly waits (yielding, not sleeping) until p.ReadOnly() == want or a deadline.
func waitReadOnly(t *testing.T, p *db.Pool, want bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for p.ReadOnly() != want {
		select {
		case <-ctx.Done():
			t.Fatalf("ReadOnly() did not become %v", want)
		default:
			runtime.Gosched()
		}
	}
}

// F17
func TestExec_OnReadOnlyDatabase_ReturnsErrReadOnlyAndFlipsState(t *testing.T) {
	admin, name := tempDB(t)
	mustExec(t, admin, "CREATE TABLE f17 (n integer)")
	o := optionsFor(t, name)
	o.MaxConns = 3
	p := openWith(t, o, nil)
	if p.ReadOnly() {
		t.Fatalf("writable at Open")
	}
	withHeldConn(t, p, func() {
		setReadOnly(t, admin, name, true) // sessions opened from now on are read-only
		_, err := p.Exec(ctxT(t), "INSERT INTO f17 VALUES (1)")
		wantKind(t, err, errs.Unavailable)
		if !errors.Is(err, db.ErrReadOnly) {
			t.Errorf("error does not wrap db.ErrReadOnly")
		}
		noMarker(t, err)
	})
	if !p.ReadOnly() {
		t.Errorf("ReadOnly() must become true after a 25006")
	}
}

func TestReadOnly_ProbeSeesDefaultTransactionReadOnly(t *testing.T) {
	admin, name := tempDB(t)
	if p := openWith(t, optionsFor(t, name), nil); p.ReadOnly() {
		t.Errorf("writable database reported read-only")
	}
	setReadOnly(t, admin, name, true)
	if p := openWith(t, optionsFor(t, name), nil); !p.ReadOnly() {
		t.Errorf("default_transaction_read_only = on must count as read-only")
	}
}

func TestPing_RefreshesReadOnlyState(t *testing.T) {
	admin, name := tempDB(t)
	o := optionsFor(t, name)
	o.MaxConns = 3
	p := openWith(t, o, nil)
	withHeldConn(t, p, func() {
		setReadOnly(t, admin, name, true)
		if err := p.Ping(ctxT(t)); err != nil {
			t.Fatalf("Ping: %v", err)
		}
		if !p.ReadOnly() {
			t.Errorf("Ping must refresh the state to read-only")
		}
		setReadOnly(t, admin, name, false)
		// Occupy the read-only session too, so Ping needs a third, fresh one.
		withHeldConn(t, p, func() {
			if err := p.Ping(ctxT(t)); err != nil {
				t.Fatalf("Ping: %v", err)
			}
			if p.ReadOnly() {
				t.Errorf("Ping must refresh the state back to writable")
			}
		})
	})
}

func TestWatcher_FlipsStateOnTheProbeInterval(t *testing.T) {
	admin, name := tempDB(t)
	o := optionsFor(t, name)
	o.MaxConns = 3
	clk := clock.NewFake(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	c := telemetrytest.New(t)
	p, err := db.Open(ctxT(t), o, clk, c.Logger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer closePool(t, p)
	if err := clk.BlockUntil(ctxT(t), 1); err != nil {
		t.Fatalf("the watcher must be waiting on one ticker: %v", err)
	}
	withHeldConn(t, p, func() {
		setReadOnly(t, admin, name, true)
		if err := clk.Advance(15 * time.Second); err != nil {
			t.Fatalf("advance: %v", err)
		}
		waitReadOnly(t, p, true)
	})
}

// F23
func TestTransact_FnFails_RollsBackAndKeepsKind(t *testing.T) {
	p, _ := tempDB(t)
	mustExec(t, p, "CREATE TABLE f23 (n integer)")
	cause := errs.E("test.fn", errs.NotFound, errors.New("missing"))
	hook := false
	err := p.Transact(ctxT(t), db.Write, func(tx context.Context) error {
		if _, e := p.Exec(tx, "INSERT INTO f23 VALUES (1)"); e != nil {
			return e //nolint:wrapcheck // test closure
		}
		if e := db.AfterCommit(tx, func(context.Context) { hook = true }); e != nil {
			return e //nolint:wrapcheck // test closure
		}
		return cause
	})
	if !errors.Is(err, cause) {
		t.Fatalf("fn's error must be returned, got %v", err)
	}
	wantKind(t, err, errs.NotFound)
	if n := count(t, p, "SELECT count(*) FROM f23"); n != 0 {
		t.Errorf("row survived the rollback")
	}
	if hook {
		t.Errorf("hook ran after a rollback")
	}
}

// F24
func TestTransact_FnPanics_RollsBackAndRepanics(t *testing.T) {
	p, _ := tempDB(t)
	mustExec(t, p, "CREATE TABLE f24 (n integer)")
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = p.Transact(ctxT(t), db.Write, func(tx context.Context) error { //nolint:errcheck // panics
			if _, e := p.Exec(tx, "INSERT INTO f24 VALUES (1)"); e != nil {
				return e //nolint:wrapcheck // test closure
			}
			panic("kaboom")
		})
	}()
	if recovered != "kaboom" {
		t.Fatalf("panic value = %v, want it re-raised unchanged", recovered)
	}
	if n := count(t, p, "SELECT count(*) FROM f24"); n != 0 {
		t.Errorf("row survived the panic")
	}
	// The pool is intact: its connection came back.
	if st := p.Stats(); st.Acquired != 0 {
		t.Errorf("connection leaked after panic: %+v", st)
	}
}

// F25
func TestTransact_CommitFails_SkipsHooks(t *testing.T) {
	p, _ := tempDB(t)
	mustExec(t, p, "CREATE TABLE f25 (n integer, CONSTRAINT f25_n_key UNIQUE (n) DEFERRABLE INITIALLY DEFERRED)")
	hook := false
	err := p.Transact(ctxT(t), db.Write, func(tx context.Context) error {
		if _, e := p.Exec(tx, "INSERT INTO f25 VALUES (1), (1)"); e != nil {
			return e //nolint:wrapcheck // test closure
		}
		return db.AfterCommit(tx, func(context.Context) { hook = true }) //nolint:wrapcheck // test closure
	})
	wantKind(t, err, errs.Conflict)
	var pe *db.Error
	if !errors.As(err, &pe) || pe.Constraint != "f25_n_key" {
		t.Errorf("*db.Error = %+v", pe)
	}
	if hook {
		t.Errorf("hooks must not run when the commit fails")
	}
	if n := count(t, p, "SELECT count(*) FROM f25"); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

// F26
func TestTransact_ContextCanceled_RollsBack(t *testing.T) {
	p, _ := tempDB(t)
	mustExec(t, p, "CREATE TABLE f26 (n integer)")

	t.Run("canceled before commit, fn returns nil", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctxT(t))
		defer cancel()
		err := p.Transact(ctx, db.Write, func(tx context.Context) error {
			if _, e := p.Exec(tx, "INSERT INTO f26 VALUES (1)"); e != nil {
				return e //nolint:wrapcheck // test closure
			}
			cancel()
			return nil
		})
		wantKind(t, err, errs.Canceled)
		if n := count(t, p, "SELECT count(*) FROM f26"); n != 0 {
			t.Errorf("rows = %d, want 0", n)
		}
	})
	t.Run("canceled during a statement", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctxT(t))
		defer cancel()
		err := p.Transact(ctx, db.Write, func(tx context.Context) error {
			if _, e := p.Exec(tx, "INSERT INTO f26 VALUES (2)"); e != nil {
				return e //nolint:wrapcheck // test closure
			}
			cancel()
			_, e := p.Exec(tx, "SELECT pg_sleep(20)")
			return e //nolint:wrapcheck // test closure
		})
		wantKind(t, err, errs.Canceled)
		if n := count(t, p, "SELECT count(*) FROM f26"); n != 0 {
			t.Errorf("rows = %d, want 0", n)
		}
	})
	t.Run("deadline inside", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(ctxT(t), 200*time.Millisecond)
		defer cancel()
		err := p.Transact(ctx, db.Write, func(tx context.Context) error {
			_, e := p.Exec(tx, "SELECT pg_sleep(20)")
			return e //nolint:wrapcheck // test closure
		})
		wantKind(t, err, errs.Timeout)
	})
}

func TestTransact_CtxFromAnotherGoroutine_RaceDetectorClean(t *testing.T) {
	// Documents the rule: fn's ctx stays on fn's goroutine. Many transactions in parallel,
	// each single-goroutine, must be race-clean.
	p, _ := tempDB(t)
	mustExec(t, p, "CREATE TABLE rc (n integer)")
	done := make(chan error, 6)
	for i := 0; i < 6; i++ {
		go func() {
			done <- p.Transact(ctxT(t), db.Write, func(tx context.Context) error {
				_, e := p.Exec(tx, "INSERT INTO rc VALUES (1)")
				return e //nolint:wrapcheck // test closure
			})
		}()
	}
	for i := 0; i < 6; i++ {
		if err := <-done; err != nil {
			t.Errorf("Transact: %v", err)
		}
	}
	if n := count(t, p, "SELECT count(*) FROM rc"); n != 6 {
		t.Errorf("rows = %d, want 6", n)
	}
}

// Decision 10: Write begins with plain BEGIN, so the session's default_transaction_read_only
// applies and the server itself refuses the write (25006).
func TestTransact_WriteOnServerReadOnlySession_RefusedByServerAndNothingWritten(t *testing.T) {
	admin, name := tempDB(t)
	mustExec(t, admin, "CREATE TABLE d10 (n integer)")
	o := optionsFor(t, name)
	o.MaxConns = 3
	p := openWith(t, o, nil)
	withHeldConn(t, p, func() {
		setReadOnly(t, admin, name, true) // sessions opened from now on are read-only
		hook := false
		err := p.Transact(ctxT(t), db.Write, func(tx context.Context) error {
			if _, err := p.Exec(tx, "INSERT INTO d10 VALUES (1)"); err != nil {
				return err //nolint:wrapcheck // test closure
			}
			return db.AfterCommit(tx, func(context.Context) { hook = true }) //nolint:wrapcheck // test closure
		})
		wantKind(t, err, errs.Unavailable)
		if !errors.Is(err, db.ErrReadOnly) {
			t.Errorf("error does not wrap db.ErrReadOnly")
		}
		if hook {
			t.Errorf("hook must not run")
		}
	})
	setReadOnly(t, admin, name, false)
	if n := count(t, admin, "SELECT count(*) FROM d10"); n != 0 {
		t.Errorf("rows written despite refusal: %d", n)
	}
}

func TestTransact_WriteOnFreshPoolOfReadOnlyDatabase_RefusedAndNothingWritten(t *testing.T) {
	admin, name := tempDB(t)
	mustExec(t, admin, "CREATE TABLE d10b (n integer)")
	p := readOnlyPool(t, admin, name)
	err := p.Transact(ctxT(t), db.Write, func(tx context.Context) error {
		_, err := p.Exec(tx, "INSERT INTO d10b VALUES (1)")
		return err //nolint:wrapcheck // test closure
	})
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Errorf("error does not wrap db.ErrReadOnly")
	}
	setReadOnly(t, admin, name, false)
	if n := count(t, admin, "SELECT count(*) FROM d10b"); n != 0 {
		t.Errorf("rows written despite refusal: %d", n)
	}
}

func TestTxMode_InsidePoolTransaction_ReportsMode(t *testing.T) {
	p, _ := tempDB(t)
	if mode, ok := db.TxMode(ctxT(t)); ok || mode != 0 {
		t.Errorf("outside a transaction TxMode = (%v, %v)", mode, ok)
	}
	for _, mode := range []db.Mode{db.Read, db.Write} {
		err := p.Transact(ctxT(t), mode, func(ctx context.Context) error {
			if got, ok := db.TxMode(ctx); !ok || got != mode {
				t.Errorf("TxMode = (%v, %v), want (%v, true)", got, ok, mode)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("Transact(%v): %v", mode, err)
		}
	}
}
