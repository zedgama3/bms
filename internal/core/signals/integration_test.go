//go:build integration

package signals_test

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
)

func realFixture(t *testing.T) (*db.Pool, *telemetrytest.Capture, signals.Bus, *signals.Dispatcher) {
	t.Helper()
	pool := dbtest.TempDB(t)
	c := telemetrytest.New(t)
	d, err := signals.New(baseOptions(c, clock.Real()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a, err := d.Scope("alpha", []signals.Name{nameX}, nil)
	if err != nil {
		t.Fatalf("Scope: %v", err)
	}
	return pool, c, a, d
}

func mustExec(t *testing.T, p *db.Pool, sql string, args ...any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if _, err := p.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func subscriber(t *testing.T, d *signals.Dispatcher, h signals.Handler) {
	t.Helper()
	b, err := d.Scope("beta", nil, []signals.Name{nameX})
	if err != nil {
		t.Fatalf("Scope: %v", err)
	}
	subscribe(t, b, nameX, h)
}

func TestPublish_AfterCommit_HandlerSeesTheRow(t *testing.T) {
	pool, _, a, d := realFixture(t)
	mustExec(t, pool, "CREATE TABLE sig_rows (id int PRIMARY KEY)")
	var seen atomic.Int64
	subscriber(t, d, func(ctx context.Context, _ signals.Signal) error {
		var n int64
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM sig_rows").Scan(&n); err != nil {
			return err //nolint:wrapcheck // pool errors are classified by db
		}
		seen.Store(n)
		return nil
	})
	err := pool.Transact(context.Background(), db.Write, func(ctx context.Context) error {
		if _, err := pool.Exec(ctx, "INSERT INTO sig_rows VALUES (1)"); err != nil {
			return err //nolint:wrapcheck // test
		}
		return a.Publish(ctx, nameX, person())
	})
	noErr(t, err)
	if seen.Load() != 1 {
		t.Errorf("handler saw %d rows, want 1 (delivery must follow the commit)", seen.Load())
	}
}

func TestPublish_CommitFails_DeliversNothing(t *testing.T) {
	pool, c, a, d := realFixture(t)
	mustExec(t, pool, "CREATE TABLE sig_parent (id int PRIMARY KEY)")
	mustExec(t, pool, "CREATE TABLE sig_child (pid int REFERENCES sig_parent(id) DEFERRABLE INITIALLY DEFERRED)")
	var called atomic.Bool
	subscriber(t, d, func(context.Context, signals.Signal) error { called.Store(true); return nil })
	err := pool.Transact(context.Background(), db.Write, func(ctx context.Context) error {
		if _, err := pool.Exec(ctx, "INSERT INTO sig_child VALUES (42)"); err != nil {
			return err //nolint:wrapcheck // test
		}
		return a.Publish(ctx, nameX, person())
	})
	if err == nil {
		t.Fatal("commit should have failed")
	}
	if called.Load() || len(delivered(c)) != 0 {
		t.Error("delivered although COMMIT failed")
	}
	if n := len(measures(c, "bms.signals.published")); n != 0 {
		t.Errorf("published counted although COMMIT failed: %d", n)
	}
}

func TestPublish_RollbackDeliversNothing_Real(t *testing.T) {
	pool, c, a, d := realFixture(t)
	var called atomic.Bool
	subscriber(t, d, func(context.Context, signals.Signal) error { called.Store(true); return nil })
	err := pool.Transact(context.Background(), db.Write, func(ctx context.Context) error {
		if err := a.Publish(ctx, nameX, person()); err != nil {
			return err //nolint:wrapcheck // test
		}
		return errBoom
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	if called.Load() || len(delivered(c)) != 0 {
		t.Error("delivered after rollback")
	}
	if n := len(measures(c, "bms.signals.published")); n != 0 {
		t.Errorf("published counted after rollback: %d", n)
	}
}

func TestDeliver_HandlerWriteOnReadOnlyCopy_OutcomeReadOnly(t *testing.T) {
	pool, c, a, d := realFixture(t)
	mustExec(t, pool, "CREATE TABLE sig_ro (id int)")

	var name string
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("current_database: %v", err)
	}
	mustExec(t, pool, "ALTER DATABASE \""+name+"\" SET default_transaction_read_only = on")

	base, err := url.Parse(os.Getenv("BMS_TEST_DB_DSN"))
	if err != nil || base.Hostname() == "" {
		t.Fatal("BMS_TEST_DB_DSN must be a postgres:// URL")
	}
	u := *base
	u.Path = "/" + name
	ro, err := db.Open(ctx, db.Options{
		URL:            secret.New(u.String()),
		MaxConns:       2,
		ConnectTimeout: 10 * time.Second,
		QueryTimeout:   10 * time.Second,
	}, clock.Real(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("open read-only pool: %v", err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), testTimeout)
		defer ccancel()
		if err := ro.Close(cctx); err != nil {
			t.Errorf("close: %v", err)
		}
	})

	var hErr error
	subscriber(t, d, func(hctx context.Context, _ signals.Signal) error {
		_, hErr = ro.Exec(hctx, "INSERT INTO sig_ro VALUES (1)")
		return hErr //nolint:wrapcheck // pool errors are classified by db
	})
	noErr(t, a.Publish(context.Background(), nameX, person())) // outside a transaction: at once
	wantKind(t, hErr, errs.Unavailable)
	if outcomes(c)["beta"] != "read_only" {
		t.Errorf("outcomes = %v", outcomes(c))
	}
}
