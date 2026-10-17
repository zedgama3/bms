//go:build integration

package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/secret"
)

const (
	dsnEnv      = "BMS_TEST_DB_DSN"
	tempTimeout = 30 * time.Second
)

// TempDB creates a database with a random name through BMS_TEST_DB_DSN, opens a db.Pool
// on it with default options and a real clock, and drops it in t.Cleanup. It calls t.Skip
// when BMS_TEST_DB_DSN is unset and t.Fatal when the database cannot be created. The
// role in the DSN needs CREATEDB. Integration build tag only. Cannot return an error:
// failures stop the test. No failure message carries the DSN.
func TempDB(t testing.TB) *db.Pool {
	t.Helper()
	raw := os.Getenv(dsnEnv)
	if raw == "" {
		t.Skip(dsnEnv + " is not set; integration tests need the compose PostgreSQL")
	}
	base, err := url.Parse(raw)
	if err != nil || base.Hostname() == "" {
		t.Fatalf("%s must be a postgres:// URL", dsnEnv)
	}

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("random database name: %v", err)
	}
	name := "bms_test_" + hex.EncodeToString(suffix[:])
	quoted := pgx.Identifier{name}.Sanitize()

	admin := func(sql string) error {
		ctx, cancel := context.WithTimeout(context.Background(), tempTimeout)
		defer cancel()
		conn, err := pgx.Connect(ctx, raw)
		if err != nil {
			return db.Wrap("dbtest.TempDB", err)
		}
		defer func() {
			if cerr := conn.Close(ctx); cerr != nil {
				t.Logf("closing the admin connection: %v", db.Wrap("dbtest.TempDB", cerr))
			}
		}()
		_, err = conn.Exec(ctx, sql)
		return db.Wrap("dbtest.TempDB", err)
	}
	if err := admin("CREATE DATABASE " + quoted); err != nil {
		t.Fatalf("cannot create a temporary database (the role needs CREATEDB): %v", err)
	}
	dropped := false
	drop := func() {
		if dropped {
			return
		}
		dropped = true
		if err := admin("DROP DATABASE IF EXISTS " + quoted + " WITH (FORCE)"); err != nil {
			t.Errorf("cannot drop temporary database %s: %v", name, err)
		}
	}

	u := *base
	u.Path = "/" + name
	opts := db.Options{
		URL:            secret.New(u.String()),
		MaxConns:       8,
		ConnectTimeout: 10 * time.Second,
		QueryTimeout:   30 * time.Second,
	}
	logger := slog.New(slog.DiscardHandler)
	ctx, cancel := context.WithTimeout(context.Background(), tempTimeout)
	defer cancel()
	pool, err := db.Open(ctx, opts, clock.Real(), logger)
	if err != nil {
		drop()
		t.Fatalf("cannot open the temporary database: %v", err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), tempTimeout)
		defer ccancel()
		if err := pool.Close(cctx); err != nil {
			t.Errorf("closing the temporary pool: %v", err)
		}
		drop()
	})
	return pool
}
