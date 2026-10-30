//go:build integration

package postgres_test

import (
	"bytes"
	"errors"
	"io/fs"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/people/postgres"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// Amendment P1 (D48), migration 0003. Names are invented.

// upTo2 is the people set as it was at version 2 (before the playa name key).
func upTo2(t *testing.T) db.Set {
	t.Helper()
	m := fstest.MapFS{}
	for _, n := range []string{"0001_person.sql", "0002_requests.sql"} {
		b, err := fs.ReadFile(postgres.Migrations, n)
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		m[n] = &fstest.MapFile{Data: b}
	}
	return db.Set{Name: "people", Schema: "core", Files: m}
}

func setVersion(t *testing.T, pool *db.Pool) int {
	t.Helper()
	var v int
	err := pool.QueryRow(ctxT(t), `SELECT coalesce(max(version), 0) FROM core.schema_migration WHERE set_name = 'people'`).Scan(&v)
	if err != nil {
		t.Fatalf("reading the people version: %v", err)
	}
	return v
}

func insertNamed(t *testing.T, pool *db.Pool, name string) [16]byte {
	t.Helper()
	id := uid()
	mustExec(t, pool, `INSERT INTO core.person (id, playa_name, created_at, updated_at) VALUES ($1, $2, $3, $3)`, id, name, t0)
	return id
}

// Version 3 on an empty database: the column, a partial unique index, and a second Migrate
// does nothing.
func TestMigration0003_EmptyDatabase_AddsColumnAndPartialUniqueIndex(t *testing.T) {
	pool := newPool(t)
	if v := setVersion(t, pool); v != 4 {
		t.Errorf("people version = %d, want 4 (all migrations applied, 0004 included)", v)
	}
	if n := count(t, pool, `SELECT count(*) FROM information_schema.columns WHERE table_schema = 'core' AND table_name = 'person' AND column_name = 'playa_name_key' AND is_nullable = 'NO'`); n != 1 {
		t.Error("core.person.playa_name_key (NOT NULL) is missing")
	}
	var def string
	if err := pool.QueryRow(ctxT(t), `SELECT indexdef FROM pg_indexes WHERE schemaname = 'core' AND indexname = 'person_playa_name_key_idx'`).Scan(&def); err != nil {
		t.Fatalf("person_playa_name_key_idx missing: %v", err)
	}
	up := strings.ToUpper(def)
	if !strings.Contains(up, "UNIQUE") || !strings.Contains(up, "WHERE") {
		t.Errorf("index is not a partial unique index: %s", def)
	}
	if n := count(t, pool, `SELECT count(*) FROM pg_indexes WHERE schemaname = 'core' AND indexname = 'person_playa_name_idx'`); n != 1 {
		t.Error("the search index person_playa_name_idx must stay")
	}
	if again := migrate(t, pool, identity.Set, peopleSet()); len(again) != 0 {
		t.Errorf("a second Migrate applied %d files", len(again))
	}
}

// Migrating a version-2 database with distinct names backfills every key.
func TestMigration0003_DistinctNames_BackfillsKeys(t *testing.T) {
	pool := dbtest.TempDB(t)
	migrate(t, pool, identity.Set, upTo2(t))
	names := []string{"Dusty Roads", "  Zorn   Plume ", "Élan", "", "Ｄash"}
	ids := make([][16]byte, len(names))
	for i, n := range names {
		ids[i] = insertNamed(t, pool, n)
	}
	migrate(t, pool, identity.Set, peopleSet())
	for i, n := range names {
		var key string
		if err := pool.QueryRow(ctxT(t), `SELECT playa_name_key FROM core.person WHERE id = $1`, ids[i]).Scan(&key); err != nil {
			t.Fatal(err)
		}
		if want := people.NameKey(n); key != want {
			t.Errorf("backfilled key for %q = %q, want %q", n, key, want)
		}
	}
}

// F85
func TestMigration0003_Duplicates_FailsLoud(t *testing.T) {
	pool := dbtest.TempDB(t)
	migrate(t, pool, identity.Set, upTo2(t))
	a := insertNamed(t, pool, "Zorn Plume")
	b := insertNamed(t, pool, "  zorn   PLUME")
	insertNamed(t, pool, "Quill Marrow")
	insertNamed(t, pool, "")
	insertNamed(t, pool, "")

	var logs bytes.Buffer
	mig, err := db.NewMigrator(pool, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatalf("migrator: %v", err)
	}
	_, err = mig.Migrate(ctxT(t), []db.Set{identity.Set, peopleSet()})
	if err == nil {
		t.Fatal("migration 0003 succeeded over two rows with one key")
	}
	// The operator sees the error and the migrator's log; the RAISE text may arrive in either.
	msg := err.Error() + "\n" + logs.String()
	for _, id := range [][16]byte{a, b} {
		if s := model.PersonID(id).String(); !strings.Contains(msg, s) {
			t.Errorf("the error does not name person %s: %v", s, err)
		}
	}
	for _, leak := range []string{"Zorn", "zorn", "Plume", "PLUME"} {
		if strings.Contains(msg, leak) {
			t.Errorf("the error carries a name fragment %q", leak)
		}
	}
	if v := setVersion(t, pool); v != 2 {
		t.Errorf("people version = %d, want 2 (nothing applied)", v)
	}
	if n := count(t, pool, `SELECT count(*) FROM information_schema.columns WHERE table_schema = 'core' AND table_name = 'person' AND column_name = 'playa_name_key'`); n != 0 {
		t.Error("the column survived a failed migration")
	}
	if n := count(t, pool, `SELECT count(*) FROM pg_indexes WHERE indexname = 'person_playa_name_key_idx'`); n != 0 {
		t.Error("the index survived a failed migration")
	}
}

// F87 (the real non-UTF-8 database cannot be made here; see TestMigration0003_SQLChecksUTF8).
func TestMigration0003_NonUTF8_Fails(t *testing.T) {
	t.Skip("the throw-away database is created UTF-8; the SQL is asserted by TestMigration0003_SQLChecksUTF8")
}

// The partial index: the same key twice is Conflict on the named index; blanks never clash.
func TestPlayaNameKeyIndex_Table(t *testing.T) {
	pool := newPool(t)
	ins := func(key string) error {
		return exec(t, pool, `INSERT INTO core.person (id, playa_name, playa_name_key, created_at, updated_at) VALUES ($1, $2, $3, $4, $4)`, uid(), key, key, t0)
	}
	if err := ins("dusty"); err != nil {
		t.Fatal(err)
	}
	err := ins("dusty")
	if !errs.Is(err, errs.Conflict) {
		t.Fatalf("same key twice: kind %v (%v), want Conflict", errs.KindOf(err), err)
	}
	var pg *db.Error
	if !errors.As(err, &pg) || pg.Constraint != "person_playa_name_key_idx" {
		t.Errorf("constraint = %+v, want person_playa_name_key_idx", pg)
	}
	for i := 0; i < 3; i++ {
		if err := ins(""); err != nil {
			t.Errorf("empty key #%d: %v", i, err)
		}
	}
	if err := ins("other"); err != nil {
		t.Errorf("a different key: %v", err)
	}
}

// Deleting the row frees the key.
func TestPlayaNameKeyIndex_DeleteFreesKey(t *testing.T) {
	pool := newPool(t)
	id := uid()
	mustExec(t, pool, `INSERT INTO core.person (id, playa_name, playa_name_key, created_at, updated_at) VALUES ($1, 'Dusty', 'dusty', $2, $2)`, id, t0)
	mustExec(t, pool, `DELETE FROM core.person WHERE id = $1`, id)
	if err := exec(t, pool, `INSERT INTO core.person (id, playa_name, playa_name_key, created_at, updated_at) VALUES ($1, 'Dusty', 'dusty', $2, $2)`, uid(), t0); err != nil {
		t.Errorf("the key was not freed: %v", err)
	}
}

// SQL/Go key agreement: each input is backfilled by the real migration 0003 (one database
// each, because equal keys would clash) and compared with NameKey, including names with
// tabs and other Unicode spaces at the edges.
func TestNameKey_MatchesMigrationBackfill(t *testing.T) {
	cases := []string{
		" Dusty ", "DUSTY", "dusty", "Dusty Roads", "Dusty  Roads", "Dusty\tRoads",
		"\u00e9", "e\u0301", "\u00c9", "\uff24usty", "\ufb01re", "  DUSTY   roads\t", "Quill Marrow",
		"\tdusty\t", "\u00a0dusty\u00a0", "dusty\u2003roads", "dusty\u2028roads\u2029",
	}
	for _, in := range cases {
		t.Run(strings.NewReplacer("\t", "_tab_", " ", "_").Replace(in), func(t *testing.T) {
			pool := dbtest.TempDB(t)
			migrate(t, pool, identity.Set, upTo2(t))
			id := insertNamed(t, pool, in)
			migrate(t, pool, identity.Set, peopleSet())
			var got string
			if err := pool.QueryRow(ctxT(t), `SELECT playa_name_key FROM core.person WHERE id = $1`, id).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if want := people.NameKey(in); got != want {
				t.Errorf("%q: backfilled key %q, NameKey %q", in, got, want)
			}
		})
	}
}
