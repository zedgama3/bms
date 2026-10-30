//go:build integration

package postgres_test

import (
	"context"
	"io/fs"
	"log/slog"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zedgama3/bms/internal/model"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/notify/postgres"
	"github.com/zedgama3/bms/internal/core/people"
)

func ctxT(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func migrateSets(t *testing.T, pool *db.Pool, sets ...db.Set) {
	t.Helper()
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mig.Migrate(ctxT(t), sets); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
}

// notifyUpTo1 is the notify set as it was at version 1 (before the skipped status).
func notifyUpTo1(t *testing.T) db.Set {
	t.Helper()
	b, err := fs.ReadFile(postgres.Migrations, "0001_notify.sql")
	if err != nil {
		t.Fatal(err)
	}
	return db.Set{Name: "notify", Schema: "core", Files: fstest.MapFS{"0001_notify.sql": &fstest.MapFile{Data: b}}}
}

func notifyVersion(t *testing.T, pool *db.Pool) int {
	t.Helper()
	var v int
	if err := pool.QueryRow(ctxT(t), `SELECT coalesce(max(version), 0) FROM core.schema_migration WHERE set_name = 'notify'`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func statusOf(t *testing.T, pool *db.Pool, id [16]byte) (status string, lastErr *string) {
	t.Helper()
	if err := pool.QueryRow(ctxT(t), `SELECT status, last_error FROM core.notify_message WHERE id = $1`, id).Scan(&status, &lastErr); err != nil {
		t.Fatal(err)
	}
	return status, lastErr
}

// Notify 0002 (D57/D58) on a version-1 database that already holds final rows: it applies,
// nothing is backfilled (an old failed/no_address row stays failed), a skipped row can be
// written, and the constraints still hold.
func TestMigration0002_OnVersion1WithRows_AppliesWithoutBackfill(t *testing.T) {
	pool := dbtest.TempDB(t)
	migrateSets(t, pool, identity.Set, people.Set, notifyUpTo1(t))
	if v := notifyVersion(t, pool); v != 1 {
		t.Fatalf("setup: notify version = %d, want 1", v)
	}
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour)
	const ins = `INSERT INTO core.notify_message
		(id, owner, template, language, status, next_attempt_at, created_at, expires_at, sent_at, failed_at, last_error)
		VALUES ($1, 'hello', 'hello.greet', 'en', $2, $3, $3, $4, $5, $6, $7)`
	sent, failed, noAddr := newID(), newID(), newID()
	// 0001 requires: sent_at set exactly for sent, failed_at exactly for failed.
	for _, r := range []struct {
		id      [16]byte
		status  string
		sentAt  *time.Time
		failAt  *time.Time
		lastErr *string
	}{
		{sent, "sent", &later, nil, nil},
		{failed, "failed", nil, &later, ptr("connect")},
		{noAddr, "failed", nil, &later, ptr("no_address")},
	} {
		if _, err := pool.Exec(ctxT(t), ins, r.id, r.status, now, now.Add(24*time.Hour), r.sentAt, r.failAt, r.lastErr); err != nil {
			t.Fatalf("setup insert %s: %v", r.status, err)
		}
	}
	// 0001 refuses a skipped row.
	if _, err := pool.Exec(ctxT(t), ins, newID(), "skipped", now, now.Add(time.Hour), nil, &later, ptr("no_address")); err == nil {
		t.Fatal("setup: version 1 accepted status skipped")
	}

	migrateSets(t, pool, identity.Set, people.Set, db.Set{Name: "notify", Schema: "core", Files: postgres.Migrations})
	if v := notifyVersion(t, pool); v != 2 {
		t.Fatalf("notify version = %d, want 2", v)
	}

	// No backfill: statuses and tokens are as inserted.
	for id, want := range map[[16]byte][2]string{sent: {"sent", ""}, failed: {"failed", "connect"}, noAddr: {"failed", "no_address"}} {
		st, le := statusOf(t, pool, id)
		got := ""
		if le != nil {
			got = *le
		}
		if st != want[0] || got != want[1] {
			t.Errorf("row %s = %s/%q after 0002, want %s/%q", id, st, got, want[0], want[1])
		}
	}

	// A skipped row can be written, holding no personal text, with failed_at as its end.
	if _, err := pool.Exec(ctxT(t), ins, newID(), "skipped", now, now.Add(time.Hour), nil, &later, ptr("no_address")); err != nil {
		t.Fatalf("a skipped row was refused after 0002: %v", err)
	}

	// The constraints hold.
	bad := []struct {
		name string
		sql  string
		args []any
	}{
		{"unknown status", ins, []any{newID(), "bogus", now, now.Add(time.Hour), nil, &later, nil}},
		{"skipped without failed_at", ins, []any{newID(), "skipped", now, now.Add(time.Hour), nil, nil, ptr("no_address")}},
		{"failed without failed_at", ins, []any{newID(), "failed", now, now.Add(time.Hour), nil, nil, nil}},
		{"queued with failed_at", ins, []any{newID(), "queued", now, now.Add(time.Hour), nil, &later, nil}},
		{"skipped holding text", `INSERT INTO core.notify_message
			(id, owner, template, language, status, next_attempt_at, created_at, expires_at, failed_at, address, subject, body)
			VALUES ($1, 'hello', 'hello.greet', 'en', 'skipped', $2, $2, $3, $4, 'x@example.org', 's', 'b')`,
			[]any{newID(), now, now.Add(time.Hour), &later}},
		{"sent holding text", `INSERT INTO core.notify_message
			(id, owner, template, language, status, next_attempt_at, created_at, expires_at, sent_at, subject, body)
			VALUES ($1, 'hello', 'hello.greet', 'en', 'sent', $2, $2, $3, $4, 's', 'b')`,
			[]any{newID(), now, now.Add(time.Hour), &later}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pool.Exec(ctxT(t), tc.sql, tc.args...); err == nil {
				t.Error("the constraint accepted the row")
			}
		})
	}
	// An UPDATE that would break a constraint on an existing row is refused too.
	if _, err := pool.Exec(ctxT(t), `UPDATE core.notify_message SET failed_at = NULL WHERE id = $1`, failed); err == nil {
		t.Error("a failed row lost its failed_at")
	}

	// A repeat does nothing.
	migrateSets(t, pool, identity.Set, people.Set, db.Set{Name: "notify", Schema: "core", Files: postgres.Migrations})
	if v := notifyVersion(t, pool); v != 2 {
		t.Errorf("notify version after a repeat = %d, want 2", v)
	}
}

func ptr[T any](v T) *T { return &v }
func newID() [16]byte   { return [16]byte(model.NewID[model.PersonID]()) }
