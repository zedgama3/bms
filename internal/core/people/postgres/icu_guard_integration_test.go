//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5" //nolint:depguard // the ICU database is created over an admin connection, as dbtest.TempDB does

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/secret"
)

// icuPool creates a database with the ICU locale provider ('und') and opens a pool on it. It
// skips when the server cannot create one.
func icuPool(t *testing.T) *db.Pool {
	t.Helper()
	raw := os.Getenv("BMS_TEST_DB_DSN")
	if raw == "" {
		t.Skip("BMS_TEST_DB_DSN not set")
	}
	base, err := url.Parse(raw)
	if err != nil {
		t.Fatal("BMS_TEST_DB_DSN is not a URL")
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	name := "bms_test_icu_" + hex.EncodeToString(b[:])
	quoted := pgx.Identifier{name}.Sanitize()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := func(sql string) error {
		actx, acancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer acancel()
		conn, err := pgx.Connect(actx, raw)
		if err != nil {
			return fmt.Errorf("admin connection: %w", db.Wrap("test", err))
		}
		_, execErr := conn.Exec(actx, sql)
		return errors.Join(execErr, conn.Close(actx))
	}
	if err := admin("CREATE DATABASE " + quoted + " TEMPLATE template0 ENCODING 'UTF8' LOCALE_PROVIDER icu ICU_LOCALE 'en-US' LOCALE 'C.UTF-8'"); err != nil {
		t.Skipf("this server cannot create an ICU database: %v", err)
	}
	t.Cleanup(func() {
		if err := admin("DROP DATABASE IF EXISTS " + quoted + " WITH (FORCE)"); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
	})
	u := *base
	u.Path = "/" + name
	pool, err := db.Open(ctx, db.Options{URL: secret.New(u.String()), MaxConns: 4, ConnectTimeout: 10 * time.Second, QueryTimeout: 30 * time.Second},
		clock.Real(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("open the ICU database: %v", err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		if err := pool.Close(cctx); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return pool
}

func migrateErr(t *testing.T, pool *db.Pool) (string, error) {
	t.Helper()
	var logs bytes.Buffer
	mig, err := db.NewMigrator(pool, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	_, merr := mig.Migrate(ctxT(t), []db.Set{identity.Set, peopleSet()})
	return logs.String(), merr
}

// F88: an ICU-provider database whose lower() cannot fold is refused when an existing name
// holds a non-ASCII character; nothing applies.
func TestMigration0003_ICUDatabase_NonASCIIName_Refused(t *testing.T) {
	pool := icuPool(t)
	var folds bool
	if err := pool.QueryRow(ctxT(t), `SELECT lower(E'İ') = 'i' AND lower(E'ΑΣ') = E'ασ'`).Scan(&folds); err != nil {
		t.Fatal(err)
	}
	if folds {
		t.Skip("this ICU database folds like Go; the guard has nothing to refuse")
	}
	migrate(t, pool, identity.Set, upTo2(t))
	insertNamed(t, pool, "Élan")
	logs, err := migrateErr(t, pool)
	if err == nil {
		t.Fatal("migration 0003 succeeded on an ICU database that folds differently from people.NameKey")
	}
	msg := strings.ToLower(err.Error() + "\n" + logs)
	if !strings.Contains(msg, "locale") {
		t.Errorf("the failure does not name the locale: %v", err)
	}
	if strings.Contains(msg, "élan") {
		t.Error("the failure carries a playa name")
	}
	if v := setVersion(t, pool); v != 2 {
		t.Errorf("people version = %d, want 2 (nothing applied)", v)
	}
}

// The guard only matters when an existing name needs it: ASCII names (and an empty table) pass.
func TestMigration0003_ICUDatabase_OnlyASCIINames_Applies(t *testing.T) {
	pool := icuPool(t)
	migrate(t, pool, identity.Set, upTo2(t))
	insertNamed(t, pool, "Dusty Roads")
	insertNamed(t, pool, "")
	if logs, err := migrateErr(t, pool); err != nil {
		t.Fatalf("migration 0003 refused ASCII-only names on an ICU database: %v\n%s", err, logs)
	}
	if v := setVersion(t, pool); v != 4 {
		t.Errorf("people version = %d, want 4 (all migrations applied, 0004 included)", v)
	}
}
