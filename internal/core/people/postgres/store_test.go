package postgres_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/people/postgres"
)

func TestMigrations_VersionedAndForwardOnly(t *testing.T) {
	v, err := db.Version(postgres.Migrations)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != 2 {
		t.Errorf("Version = %d, want 2 (0001_person.sql, 0002_requests.sql)", v)
	}
	for _, name := range []string{"0001_person.sql", "0002_requests.sql"} {
		b, err := fs.ReadFile(postgres.Migrations, name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		up := strings.ToUpper(string(b))
		for _, bad := range []string{"DROP TABLE", "DROP SCHEMA", "TRUNCATE", "-- DOWN", "+MIGRATE DOWN"} {
			if strings.Contains(up, bad) {
				t.Errorf("%s contains %q: migrations are forward-only", name, bad)
			}
		}
	}
}

func TestMigrations_SetValidates(t *testing.T) {
	set := db.Set{Name: "people", Schema: "core", Files: postgres.Migrations}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
