//go:build integration

package db_test

import (
	"context"
	"errors"
	"net/url"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/model"
)

var (
	_ db.DB = (*db.Pool)(nil)
	_ db.DB = (*dbtest.Fake)(nil)
)

type shift struct{}

type shiftID = model.ID[shift]

func TestOpen_Success_ReportsStateAndBounds(t *testing.T) {
	_, name := tempDB(t)
	o := optionsFor(t, name)
	o.MaxConns = 3
	p := openWith(t, o, nil)
	if p.ReadOnly() {
		t.Errorf("a plain database must be writable")
	}
	if err := p.Ping(ctxT(t)); err != nil {
		t.Errorf("Ping: %v", err)
	}
	if st := p.Stats(); st.Max != 3 {
		t.Errorf("Stats.Max = %d, want 3", st.Max)
	}
	var app string
	if err := p.QueryRow(ctxT(t), "SHOW application_name").Scan(&app); err != nil || app != "bms" {
		t.Errorf("application_name = %q, %v; want bms", app, err)
	}
}

func TestOpen_NeverLogsPassword(t *testing.T) {
	_, name := tempDB(t)
	o := optionsFor(t, name)
	pw, _ := urlFor(t, name).User.Password()
	c := telemetrytest.New(t)
	p, err := db.Open(ctxT(t), o, fakeClock(), c.Logger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer closePool(t, p)
	if pw != "" && c.Contains(pw) {
		t.Errorf("the start line carries the password")
	}
	if c.Contains(o.URL.Value()) {
		t.Errorf("the start line carries the URL")
	}
}

// F7
func TestOpen_BadCredentials_ReturnsInvalidWithoutRetry(t *testing.T) {
	_, name := tempDB(t)
	tests := map[string]func(u *url.URL){
		"wrong password":   func(u *url.URL) { u.User = url.UserPassword(u.User.Username(), marker) },
		"unknown role":     func(u *url.URL) { u.User = url.UserPassword("no_such_role_"+randHex(t), marker) },
		"missing database": func(u *url.URL) { u.Path = "/no_such_db_" + randHex(t) },
	}
	for label, mutate := range tests {
		t.Run(label, func(t *testing.T) {
			u := urlFor(t, name)
			mutate(u)
			o := optionsFor(t, name)
			o.URL = secret.New(u.String())
			o.ConnectTimeout = 30 * time.Second
			start := time.Now()
			p, err := db.Open(ctxT(t), o, clock.Real(), quietLogger())
			if p != nil {
				closePool(t, p)
				t.Fatalf("Open must fail")
			}
			wantKind(t, err, errs.Invalid)
			noMarker(t, err)
			if time.Since(start) > 10*time.Second {
				t.Errorf("Invalid must not be retried until ConnectTimeout")
			}
		})
	}
}

// F10
func TestQuerier_ContextCanceled_ReturnsCanceled(t *testing.T) {
	p, _ := tempDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Exec(ctx, "SELECT 1")
	wantKind(t, err, errs.Canceled)

	rows, err := p.Query(ctx, "SELECT 1")
	if err == nil {
		for rows.Next() {
		}
		err = rows.Err()
		rows.Close()
	}
	wantKind(t, err, errs.Canceled)

	var n int
	wantKind(t, p.QueryRow(ctx, "SELECT 1").Scan(&n), errs.Canceled)

	err = p.Transact(ctx, db.Read, func(context.Context) error { return nil })
	wantKind(t, err, errs.Canceled)
}

// F11
func TestQuerier_ExceedsQueryTimeout_ReturnsTimeout(t *testing.T) {
	_, name := tempDB(t)
	o := optionsFor(t, name)
	o.QueryTimeout = 100 * time.Millisecond
	p := openWith(t, o, nil)
	start := time.Now()

	_, err := p.Exec(ctxT(t), "SELECT pg_sleep(20)")
	wantKind(t, err, errs.Timeout)

	rows, err := p.Query(ctxT(t), "SELECT pg_sleep(20)")
	if err == nil {
		for rows.Next() {
		}
		err = rows.Err()
		rows.Close()
	}
	wantKind(t, err, errs.Timeout)

	var n int
	wantKind(t, p.QueryRow(ctxT(t), "SELECT pg_sleep(20)::text::int").Scan(&n), errs.Timeout)
	if time.Since(start) > 15*time.Second {
		t.Errorf("statements ran far past QueryTimeout")
	}
	// The pool is still usable afterwards.
	if got := count(t, p, "SELECT 1"); got != 1 {
		t.Errorf("pool unusable after timeouts")
	}
}

func TestQuerier_CallerDeadlineShorterThanQueryTimeout_ReturnsTimeout(t *testing.T) {
	p, _ := tempDB(t) // default QueryTimeout is long
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := p.Exec(ctx, "SELECT pg_sleep(20)")
	wantKind(t, err, errs.Timeout)
}

func TestQuerier_DeadlinePassed_ReturnsTimeout(t *testing.T) {
	p, _ := tempDB(t)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err := p.Exec(ctx, "SELECT 1")
	wantKind(t, err, errs.Timeout)
}

func TestQuerier_RoundTrip(t *testing.T) {
	p, _ := tempDB(t)
	ctx := ctxT(t)
	mustExec(t, p, "CREATE TABLE rt (id integer PRIMARY KEY, label text NOT NULL)")
	tag, err := p.Exec(ctx, "INSERT INTO rt (id, label) VALUES ($1, $2), ($3, $4)", 1, "one", 2, "two")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if tag.RowsAffected() != 2 {
		t.Errorf("RowsAffected = %d, want 2", tag.RowsAffected())
	}
	rows, err := p.Query(ctx, "SELECT id, label FROM rt ORDER BY id")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	type row struct {
		ID    int
		Label string
	}
	got, err := pgx.CollectRows(rows, pgx.RowToStructByPos[row])
	if err != nil {
		t.Fatalf("CollectRows: %v", db.Wrap("test", err))
	}
	if len(got) != 2 || got[0] != (row{1, "one"}) || got[1] != (row{2, "two"}) {
		t.Errorf("rows = %+v", got)
	}
	var label string
	if err := p.QueryRow(ctx, "SELECT label FROM rt WHERE id = $1", 2).Scan(&label); err != nil || label != "two" {
		t.Errorf("QueryRow = %q, %v", label, err)
	}
}

// F18
func TestQueryRow_NoRows_ReturnsNotFound(t *testing.T) {
	p, _ := tempDB(t)
	mustExec(t, p, "CREATE TABLE nr (id integer)")
	var id int
	wantKind(t, p.QueryRow(ctxT(t), "SELECT id FROM nr").Scan(&id), errs.NotFound)
}

func TestQuerier_ServerErrors_AreClassifiedAndScrubbed(t *testing.T) {
	p, _ := tempDB(t)
	mustExec(t, p, "CREATE TABLE sv (id integer PRIMARY KEY, email text NOT NULL CONSTRAINT sv_email_key UNIQUE, n integer CHECK (n > 0))")
	mustExec(t, p, "INSERT INTO sv (id, email) VALUES (1, $1)", marker+"@example.org")

	t.Run("unique violation", func(t *testing.T) {
		_, err := p.Exec(ctxT(t), "INSERT INTO sv (id, email) VALUES (2, $1)", marker+"@example.org")
		wantKind(t, err, errs.Conflict)
		noMarker(t, err)
		var pe *db.Error
		if !errors.As(err, &pe) || pe.Code != "23505" || pe.Constraint != "sv_email_key" || pe.Table != "sv" {
			t.Errorf("*db.Error = %+v", pe)
		}
	})
	t.Run("not null", func(t *testing.T) {
		_, err := p.Exec(ctxT(t), "INSERT INTO sv (id, email) VALUES (3, NULL)")
		wantKind(t, err, errs.Invalid)
	})
	t.Run("check", func(t *testing.T) {
		_, err := p.Exec(ctxT(t), "INSERT INTO sv (id, email, n) VALUES (4, 'a', -1)")
		wantKind(t, err, errs.Invalid)
	})
	t.Run("data exception echoes input", func(t *testing.T) {
		var n int
		err := p.QueryRow(ctxT(t), "SELECT $1::text::int", marker).Scan(&n)
		wantKind(t, err, errs.Invalid)
		noMarker(t, err)
	})
	t.Run("foreign key", func(t *testing.T) {
		mustExec(t, p, "CREATE TABLE fk (sv_id integer REFERENCES sv (id))")
		_, err := p.Exec(ctxT(t), "INSERT INTO fk VALUES (99)")
		wantKind(t, err, errs.Invalid)
	})
	t.Run("undefined table", func(t *testing.T) {
		_, err := p.Exec(ctxT(t), "SELECT * FROM no_such_table_"+marker)
		wantKind(t, err, errs.Internal)
		noMarker(t, err)
		var pe *db.Error
		if !errors.As(err, &pe) || pe.Code != "42P01" {
			t.Errorf("*db.Error = %+v", pe)
		}
	})
	t.Run("syntax", func(t *testing.T) {
		_, err := p.Exec(ctxT(t), "SELEKT "+marker)
		wantKind(t, err, errs.Internal)
		noMarker(t, err)
	})
	t.Run("rows scan error is classified", func(t *testing.T) {
		rows, err := p.Query(ctxT(t), "SELECT email FROM sv")
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatalf("no row")
		}
		var n int
		err = rows.Scan(&n) // text into int
		if err == nil {
			t.Fatalf("want a scan error")
		}
		noMarker(t, err)
	})
}

func TestQuery_RowsCloseCancelsDeadline(t *testing.T) {
	p, _ := tempDB(t)
	run := func() {
		for i := 0; i < 50; i++ {
			rows, err := p.Query(ctxT(t), "SELECT generate_series(1, 3)")
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			rows.Close()
		}
	}
	run() // warm the pool so its own goroutines exist
	runtime.GC()
	before := runtime.NumGoroutine()
	run()
	limit := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(limit) {
		runtime.Gosched()
	}
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Errorf("goroutines grew from %d to %d", before, after)
	}
}

// F20
func TestPool_UseAfterClose_ReturnsUnavailable(t *testing.T) {
	_, name := tempDB(t)
	p, err := db.Open(ctxT(t), optionsFor(t, name), clock.Real(), quietLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := p.Close(ctxT(t)); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ctx := ctxT(t)
	_, err = p.Exec(ctx, "SELECT 1")
	wantKind(t, err, errs.Unavailable)
	rows, err := p.Query(ctx, "SELECT 1")
	if err == nil {
		for rows.Next() {
		}
		err = rows.Err()
		rows.Close()
	}
	wantKind(t, err, errs.Unavailable)
	var n int
	wantKind(t, p.QueryRow(ctx, "SELECT 1").Scan(&n), errs.Unavailable)
	wantKind(t, p.Transact(ctx, db.Read, func(context.Context) error { return nil }), errs.Unavailable)
	wantKind(t, p.Ping(ctx), errs.Unavailable)
	if st := p.Stats(); st != (db.Stats{}) {
		t.Errorf("Stats after Close = %+v, want zeros", st)
	}
}

// F30
func TestClose_Twice_IsNil(t *testing.T) {
	_, name := tempDB(t)
	p, err := db.Open(ctxT(t), optionsFor(t, name), clock.Real(), quietLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := p.Close(ctxT(t)); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := p.Close(ctxT(t)); err != nil {
		t.Fatalf("second Close must be nil, got %v", err)
	}
}

// F30
func TestClose_ContextExpired_ReturnsTimeout(t *testing.T) {
	_, name := tempDB(t)
	p, err := db.Open(ctxT(t), optionsFor(t, name), clock.Real(), quietLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	held := make(chan context.Context)
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- p.Transact(context.Background(), db.Read, func(tx context.Context) error {
			held <- tx
			<-release
			return nil
		})
	}()
	tx := <-held

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = p.Close(ctx)
	elapsed := time.Since(start)
	wantKind(t, err, errs.Timeout)
	if elapsed > 5*time.Second {
		t.Errorf("Close returned %v after its deadline", elapsed)
	}

	// The holder's next call is Unavailable.
	var n int
	wantKind(t, p.QueryRow(tx, "SELECT 1").Scan(&n), errs.Unavailable)

	close(release) // releasing afterwards must not panic
	if err := <-holderDone; err == nil {
		t.Log("holder's commit succeeded after Close")
	}
	// The pool finishes closing in the background; a later Close settles.
	if err := p.Close(ctxT(t)); err != nil {
		t.Logf("late Close: %v", err)
	}
}

func TestPing_ContextCanceled_ReturnsCanceled(t *testing.T) {
	_, name := tempDB(t)
	p := openWith(t, optionsFor(t, name), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	wantKind(t, p.Ping(ctx), errs.Canceled)
	dctx, dcancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer dcancel()
	wantKind(t, p.Ping(dctx), errs.Timeout)
}

// F28
func TestPing_Unreachable_ReturnsUnavailable(t *testing.T) {
	_, name := tempDB(t)
	px := newProxy(t)
	o := optionsFor(t, name)
	o.URL = secret.New(px.URL(t, name))
	p := openWith(t, o, nil)
	if err := p.Ping(ctxT(t)); err != nil {
		t.Fatalf("Ping before the outage: %v", err)
	}
	px.Kill()
	err := p.Ping(ctxT(t))
	wantKind(t, err, errs.Unavailable)
	var n int
	wantKind(t, p.QueryRow(ctxT(t), "SELECT 1").Scan(&n), errs.Unavailable)
}

func TestStats_ReflectsAcquiredAndIdle(t *testing.T) {
	_, name := tempDB(t)
	o := optionsFor(t, name)
	o.MaxConns = 5
	p := openWith(t, o, nil)
	err := p.Transact(ctxT(t), db.Read, func(ctx context.Context) error {
		var n int
		if err := p.QueryRow(ctx, "SELECT 1").Scan(&n); err != nil {
			return err //nolint:wrapcheck // test closure
		}
		if st := p.Stats(); st.Acquired != 1 {
			t.Errorf("Acquired = %d inside a transaction, want 1", st.Acquired)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Transact: %v", err)
	}
	st := p.Stats()
	if st.Acquired != 0 || st.Idle < 1 || st.Max != 5 || st.Total < st.Idle {
		t.Errorf("Stats after commit = %+v", st)
	}
}

func TestPool_ConcurrentUse_RaceClean(t *testing.T) {
	_, name := tempDB(t)
	o := optionsFor(t, name)
	o.MaxConns = 4
	p := openWith(t, o, nil)
	mustExec(t, p, "CREATE TABLE cc (id integer PRIMARY KEY)")
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				id := w*100 + i
				err := p.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
					_, e := p.Exec(ctx, "INSERT INTO cc VALUES ($1)", id)
					return e //nolint:wrapcheck // test closure
				})
				if err != nil {
					t.Errorf("Transact: %v", err)
					return
				}
				_ = p.ReadOnly()
				_ = p.Stats()
			}
		}(w)
	}
	wg.Wait()
	if n := count(t, p, "SELECT count(*) FROM cc"); n != 80 {
		t.Errorf("rows = %d, want 80", n)
	}
}

func TestPgx_ScansModelIDAndEmailByUnderlyingType(t *testing.T) {
	p, _ := tempDB(t)
	ctx := ctxT(t)
	mustExec(t, p, "CREATE TABLE sc (id uuid PRIMARY KEY, shift uuid, who uuid, mail text, opt uuid)")
	pid := model.NewID[model.PersonID]()
	sid := model.NewID[shiftID]()
	mail := model.Email("ada@example.org")

	if _, err := p.Exec(ctx, "INSERT INTO sc (id, shift, who, mail) VALUES ($1, $2, $3, $4)", pid, sid, pid, mail); err != nil {
		t.Fatalf("insert by model type: %v", err)
	}
	var gotP model.PersonID
	var gotS shiftID
	var gotE model.Email
	if err := p.QueryRow(ctx, "SELECT id, shift, mail FROM sc WHERE id = $1", pid).Scan(&gotP, &gotS, &gotE); err != nil {
		t.Fatalf("scan by model type: %v", err)
	}
	if gotP != pid || gotS != sid || gotE != mail {
		t.Errorf("round trip mismatch: %v %v %v", gotP, gotS, gotE)
	}

	// The documented fallback: convert to the underlying type.
	pid2 := model.NewID[model.PersonID]()
	mail2 := model.Email("grace@example.org")
	if _, err := p.Exec(ctx, "INSERT INTO sc (id, mail) VALUES ($1, $2)", [16]byte(pid2), string(mail2)); err != nil {
		t.Fatalf("insert by underlying type: %v", err)
	}
	var raw [16]byte
	var s string
	if err := p.QueryRow(ctx, "SELECT id, mail FROM sc WHERE id = $1", [16]byte(pid2)).Scan(&raw, &s); err != nil {
		t.Fatalf("scan by underlying type: %v", err)
	}
	if model.PersonID(raw) != pid2 || model.Email(s) != mail2 {
		t.Errorf("fallback round trip mismatch")
	}

	// A NULL uuid scans into a nil *PersonID; a non-NULL into a non-nil pointer.
	var none *model.PersonID
	if err := p.QueryRow(ctx, "SELECT opt FROM sc WHERE id = $1", pid).Scan(&none); err != nil {
		t.Fatalf("scan NULL: %v", err)
	}
	if none != nil {
		t.Errorf("NULL uuid must scan into a nil *PersonID, got %v", *none)
	}
	var some *model.PersonID
	if err := p.QueryRow(ctx, "SELECT who FROM sc WHERE id = $1", pid).Scan(&some); err != nil {
		t.Fatalf("scan non-NULL into pointer: %v", err)
	}
	if some == nil || *some != pid {
		t.Errorf("non-NULL uuid must scan into a non-nil pointer holding the value, got %v", some)
	}
}

func TestTempDB_Isolated_AndDroppedAfterTest(t *testing.T) {
	admin, adminName := tempDB(t)
	var inner string
	t.Run("sub", func(t *testing.T) {
		p, name := tempDB(t)
		inner = name
		if name == adminName {
			t.Fatalf("two TempDBs share a database")
		}
		mustExec(t, p, "CREATE TABLE only_here (id integer)")
		if n := count(t, admin, "SELECT count(*) FROM pg_tables WHERE tablename = 'only_here'"); n != 0 {
			t.Errorf("tables leak between TempDBs")
		}
		if got := count(t, p, "SELECT count(*) FROM pg_database WHERE datname = $1", name); got != 1 {
			t.Errorf("database not visible")
		}
	})
	if inner == "" {
		t.Fatalf("subtest did not run")
	}
	if n := count(t, admin, "SELECT count(*) FROM pg_database WHERE datname = $1", inner); n != 0 {
		t.Errorf("the temporary database %s was not dropped", inner)
	}
}

// F54
func TestTempDB_SkipsWithoutDSN(t *testing.T) {
	var skipped bool
	t.Run("inner", func(t *testing.T) {
		t.Setenv("BMS_TEST_DB_DSN", "")
		defer func() { skipped = t.Skipped() }()
		dbtest.TempDB(t)
		t.Errorf("TempDB must skip without BMS_TEST_DB_DSN")
	})
	if !skipped {
		t.Fatalf("TempDB did not call t.Skip")
	}
}

// F16: a caller waiting to acquire when Close runs sees Unavailable, not Internal.
func TestPool_CloseWhileWaitingToAcquire_ReturnsUnavailable(t *testing.T) {
	_, name := tempDB(t)
	o := optionsFor(t, name)
	o.MaxConns = 1
	p, err := db.Open(ctxT(t), o, clock.Real(), quietLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	holding := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- p.Transact(context.Background(), db.Read, func(context.Context) error {
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding
	waiter := make(chan error, 1)
	go func() {
		_, err := p.Exec(context.Background(), "SELECT 1")
		waiter <- err
	}()
	limit := time.Now().Add(20 * time.Second)
	for p.Stats().Pending < 1 && time.Now().Before(limit) {
		runtime.Gosched()
	}
	if p.Stats().Pending < 1 {
		t.Fatalf("the second caller never started waiting")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := p.Close(ctx); err == nil {
		t.Log("Close finished without waiting for the holder")
	}
	wantKind(t, <-waiter, errs.Unavailable)
	close(release)
	<-holderDone
}

// A server-side statement_timeout (57014) with the caller's ctx still live is Timeout.
func TestQuerier_ServerStatementTimeout_ReturnsTimeout(t *testing.T) {
	_, name := tempDB(t)
	o := optionsFor(t, name)
	o.MaxConns = 1 // one session, so the SET below applies to every statement
	p := openWith(t, o, nil)
	mustExec(t, p, "SET statement_timeout = '200ms'")
	ctx := ctxT(t)

	_, err := p.Exec(ctx, "SELECT pg_sleep(20)")
	wantKind(t, err, errs.Timeout)
	var n int
	wantKind(t, p.QueryRow(ctx, "SELECT pg_sleep(20)::text::int").Scan(&n), errs.Timeout)
	rows, err := p.Query(ctx, "SELECT pg_sleep(20)")
	if err == nil {
		for rows.Next() {
		}
		err = rows.Err()
		rows.Close()
	}
	wantKind(t, err, errs.Timeout)
	if ctx.Err() != nil {
		t.Fatalf("the caller's ctx must still be live")
	}
}
