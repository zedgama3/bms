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
	if v != 3 {
		t.Errorf("Version = %d, want 3 (0001_person.sql, 0002_requests.sql, 0003_playa_name_key.sql)", v)
	}
	for _, name := range []string{"0001_person.sql", "0002_requests.sql", "0003_playa_name_key.sql"} {
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

// F87: the migration refuses a non-UTF-8 server before computing any key. The SQL is
// asserted here because a throw-away non-UTF-8 database cannot be made on every CI.
func TestMigration0003_SQLChecksUTF8(t *testing.T) {
	b, err := fs.ReadFile(postgres.Migrations, "0003_playa_name_key.sql")
	if err != nil {
		t.Fatalf("0003_playa_name_key.sql: %v", err)
	}
	var code []string
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			code = append(code, line)
		}
	}
	sql := strings.Join(code, "\n") // comments mention normalize() too
	for _, want := range []string{"server_encoding", "UTF8", "RAISE EXCEPTION", "playa_name_key", "person_playa_name_key_idx"} {
		if !strings.Contains(sql, want) {
			t.Errorf("0003 does not mention %q", want)
		}
	}
	if i, j := strings.Index(sql, "server_encoding"), strings.Index(strings.ToLower(sql), "normalize("); i < 0 || j < 0 || i > j {
		t.Error("the encoding check must come before the first normalize() call")
	}
	// F88 (D51): the locale guard, by text, since a database that fails it cannot always be made.
	for _, want := range []string{
		`lower(E'\u00C9')`, `lower(E'\u0130')`, `lower(E'\u0391\u03A3')`, // the three ways a locale differs from Go
		"datlocprovider", "datctype", // the message names the provider and the locale
		"octet_length(playa_name)", // refuses only when an existing name is non-ASCII
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("the locale guard lacks %q", want)
		}
	}
	if g, u := strings.Index(sql, `lower(E'\u0130')`), strings.Index(sql, "UPDATE core.person"); g < 0 || u < 0 || g > u {
		t.Error("the locale guard must come before the backfill UPDATE")
	}
	if !strings.Contains(strings.ToUpper(sql), "WHERE PLAYA_NAME_KEY <> ''") {
		t.Error("the index must be partial (WHERE playa_name_key <> '')")
	}
}
