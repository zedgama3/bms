//go:build integration

package postgres_test

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people/postgres"
)

// upTo3 is the people set as it was at version 3 (before name_prompted_at).
func upTo3(t *testing.T) db.Set {
	t.Helper()
	m := fstest.MapFS{}
	for _, n := range []string{"0001_person.sql", "0002_requests.sql", "0003_playa_name_key.sql"} {
		b, err := fs.ReadFile(postgres.Migrations, n)
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		m[n] = &fstest.MapFile{Data: b}
	}
	return db.Set{Name: "people", Schema: "core", Files: m}
}

// F103 (people P-W1): migration 0004 applied on a version-3 set with data moves it to 4,
// adds a nullable timestamptz column, leaves it NULL for every row and changes nothing else.
func TestMigrations_Apply(t *testing.T) {
	pool := dbtest.TempDB(t)
	migrate(t, pool, identity.Set, upTo3(t))
	if v := setVersion(t, pool); v != 3 {
		t.Fatalf("setup: people version = %d, want 3", v)
	}
	named := insertNamed(t, pool, "Dusty Roads")
	unnamed := insertNamed(t, pool, "")
	if n := count(t, pool, `SELECT count(*) FROM information_schema.columns WHERE table_schema = 'core' AND table_name = 'person' AND column_name = 'name_prompted_at'`); n != 0 {
		t.Fatal("setup: the column exists before 0004")
	}
	migrate(t, pool, identity.Set, peopleSet())
	if v := setVersion(t, pool); v != 4 {
		t.Errorf("people version = %d, want 4", v)
	}
	if n := count(t, pool, `SELECT count(*) FROM information_schema.columns WHERE table_schema = 'core' AND table_name = 'person' AND column_name = 'name_prompted_at' AND data_type = 'timestamp with time zone' AND is_nullable = 'YES' AND column_default IS NULL`); n != 1 {
		t.Error("core.person.name_prompted_at (timestamptz, nullable, no default) is missing")
	}
	if n := count(t, pool, `SELECT count(*) FROM core.person WHERE name_prompted_at IS NOT NULL`); n != 0 {
		t.Errorf("%d rows have name_prompted_at set; every existing row must be NULL", n)
	}
	for _, id := range [][16]byte{named, unnamed} {
		if n := count(t, pool, `SELECT count(*) FROM core.person WHERE id = $1`, id); n != 1 {
			t.Errorf("a row did not survive the migration")
		}
	}
	// A second Migrate does nothing.
	migrate(t, pool, identity.Set, peopleSet())
	if v := setVersion(t, pool); v != 4 {
		t.Errorf("people version after a repeat = %d, want 4", v)
	}
}
