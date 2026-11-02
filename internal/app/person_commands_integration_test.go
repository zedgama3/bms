//go:build integration

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/model"
)

// pcFixture is a migrated database and the environment for `bms` commands against it.
func pcFixture(t *testing.T) (env []string, pool *db.Pool, dbName string) {
	t.Helper()
	u, pool, dbName := itPool(t)
	env, _ = itEnv(t, u.String())
	if r := itMain(t.Context(), env, "migrate"); r.code != ExitOK {
		t.Fatalf("migrate: %d\n%s\n%s", r.code, r.out, r.errOut)
	}
	return env, pool, dbName
}

// pcPerson inserts a person row with an empty profile (no account) and returns the ID.
func pcPerson(t *testing.T, pool *db.Pool) string {
	t.Helper()
	id := model.NewID[model.PersonID]().String()
	if _, err := pool.Exec(t.Context(), `INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, now(), now())`, id); err != nil {
		t.Fatalf("insert a person: %v", err)
	}
	return id
}

// pcGrant gives the person the starter role with key.
func pcGrant(t *testing.T, pool *db.Pool, person, roleKey string) {
	t.Helper()
	_, err := pool.Exec(t.Context(), `INSERT INTO core.role_grant (id, person_id, role_id, created_at)
		SELECT $1, $2, id, now() FROM core.role WHERE key = $3`, model.NewID[model.PersonID]().String(), person, roleKey)
	if err != nil {
		t.Fatalf("grant %s: %v", roleKey, err)
	}
}

func pcFreeze(t *testing.T, pool *db.Pool, name string) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), `ALTER DATABASE "`+name+`" SET default_transaction_read_only = on`); err != nil {
		t.Fatalf("freeze the database: %v", err)
	}
}

func pcCount(t *testing.T, pool *db.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func envWithout(env []string, name string) []string {
	var out []string
	for _, e := range env {
		if !strings.HasPrefix(e, name+"=") {
			out = append(out, e)
		}
	}
	return out
}

func TestDelete_Integration_ReadOnlyCopy_Exit1(t *testing.T) {
	env, pool, name := pcFixture(t)
	person := pcPerson(t, pool)
	pcFreeze(t, pool, name)
	r := itMain(t.Context(), env, "delete", person)
	if r.code != ExitFailure || !strings.Contains(r.errOut, "read-only") {
		t.Fatalf("delete on a read-only copy = %d, stderr %q; want ExitFailure and a read-only message", r.code, r.errOut)
	}
	if n := pcCount(t, pool, `SELECT count(*) FROM core.person WHERE id = $1`, person); n != 1 {
		t.Error("the person was deleted on a read-only copy")
	}
}

func TestExport_Integration_ReadOnlyCopy_WorksAndSpoolsTheAuditEntry(t *testing.T) {
	env, pool, name := pcFixture(t)
	person := pcPerson(t, pool)
	pcFreeze(t, pool, name)
	r := itMain(t.Context(), env, "export", person)
	if r.code != ExitOK {
		t.Fatalf("export on a read-only copy = %d\n%s\n%s", r.code, r.out, r.errOut)
	}
	if got := jsonField(t, r.out, "person.id"); got != person {
		t.Errorf("export person.id = %q, want %q", got, person)
	}
	dir := filepath.Join(env2(env, "BMS_DATA_DIR"), filepath.FromSlash(audit.SpoolDirName))
	files, err := os.ReadDir(dir)
	if err != nil || len(files) == 0 {
		t.Fatalf("spool %s: %v, %d files; want the export's audit entry spooled", dir, err, len(files))
	}
}

func TestExport_Integration_UnknownPerson_Exit1_MalformedID_Exit2(t *testing.T) {
	env, _, _ := pcFixture(t)
	unknown := model.NewID[model.PersonID]().String()
	if r := itMain(t.Context(), env, "export", unknown); r.code != ExitFailure || r.out != "" {
		t.Errorf("export of an unknown person = %d, stdout %q; want ExitFailure and no document", r.code, r.out)
	}
	if r := itMain(t.Context(), env, "export", "not-a-uuid"); r.code != ExitUsage {
		t.Errorf("export with a malformed id = %d, want ExitUsage", r.code)
	}
	if r := itMain(t.Context(), env, "delete", unknown); r.code != ExitFailure {
		t.Errorf("delete of an unknown person = %d, want ExitFailure", r.code)
	}
	if r := itMain(t.Context(), env, "delete", "not-a-uuid"); r.code != ExitUsage {
		t.Errorf("delete with a malformed id = %d, want ExitUsage", r.code)
	}
}

func TestExportAndDelete_Integration_NoSessionKey_Exit2(t *testing.T) {
	env, pool, _ := pcFixture(t)
	person := pcPerson(t, pool)
	env = envWithout(env, "BMS_SESSION_KEY")
	for _, cmd := range []string{"export", "delete"} {
		if r := itMain(t.Context(), env, cmd, person); r.code != ExitUsage {
			t.Errorf("%s without a session key = %d, want ExitUsage\n%s", cmd, r.code, r.errOut)
		}
	}
	if n := pcCount(t, pool, `SELECT count(*) FROM core.person WHERE id = $1`, person); n != 1 {
		t.Error("the person was deleted without a session key")
	}
}

func TestDelete_Integration_LastAccessAdmin_Exit1(t *testing.T) {
	env, pool, _ := pcFixture(t)
	admin := pcPerson(t, pool)
	pcGrant(t, pool, admin, "access-admin")
	r := itMain(t.Context(), env, "delete", admin)
	if r.code != ExitFailure {
		t.Fatalf("delete of the last access admin = %d, want ExitFailure\n%s\n%s", r.code, r.out, r.errOut)
	}
	if n := pcCount(t, pool, `SELECT count(*) FROM core.person WHERE id = $1`, admin); n != 1 {
		t.Error("the last access admin's person row was deleted")
	}
	if n := pcCount(t, pool, `SELECT count(*) FROM core.role_grant WHERE person_id = $1`, admin); n != 1 {
		t.Error("the last access admin's grant was deleted")
	}
	// With a second admin the first can be deleted.
	other := pcPerson(t, pool)
	pcGrant(t, pool, other, "access-admin")
	if r := itMain(t.Context(), env, "delete", admin); r.code != ExitOK {
		t.Fatalf("delete with another admin present = %d\n%s\n%s", r.code, r.out, r.errOut)
	}
}

func TestDelete_Integration_PersonWithGrant_AuditedAsOperator(t *testing.T) {
	env, pool, _ := pcFixture(t)
	person := pcPerson(t, pool)
	pcGrant(t, pool, person, "event-admin")
	r := itMain(t.Context(), env, "delete", person)
	if r.code != ExitOK {
		t.Fatalf("delete = %d\n%s\n%s", r.code, r.out, r.errOut)
	}
	if n := pcCount(t, pool, `SELECT count(*) FROM core.role_grant WHERE person_id = $1`, person); n != 0 {
		t.Error("the person's grant remains")
	}
	rows, err := pool.Query(t.Context(), `SELECT action, actor_kind FROM core.audit_entry WHERE action IN ('access.grants.deleted','datarights.delete')`)
	if err != nil {
		t.Fatalf("read the audit table: %v", err)
	}
	defer rows.Close()
	kinds := map[string]string{}
	for rows.Next() {
		var action, kind string
		if err := rows.Scan(&action, &kind); err != nil {
			t.Fatal(err)
		}
		kinds[action] = kind
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"access.grants.deleted", "datarights.delete"} {
		if kinds[action] != "operator" {
			t.Errorf("%s was recorded with actor_kind %q, want operator (all: %v)", action, kinds[action], kinds)
		}
	}
}

// hello 0002's preflight: a greeting for a person with no core.person row stops migrate.
func TestMigrate_Integration_OrphanGreeting_Exit1_NamesTheTable(t *testing.T) {
	env, pool, _ := pcFixture(t)
	ctx := t.Context()
	// Rebuild the state before hello 0002: drop its constraint and its migration record.
	for _, q := range []string{
		`ALTER TABLE hello.greeting DROP CONSTRAINT greeting_person_id_fkey`,
		`DELETE FROM core.schema_migration WHERE set_name = 'hello' AND version = 2`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	orphan := model.NewID[model.PersonID]().String()
	if _, err := pool.Exec(ctx, `INSERT INTO hello.greeting (id, person_id, greeting, created_at) VALUES ($1, $2, 'howdy', now())`,
		model.NewID[model.PersonID]().String(), orphan); err != nil {
		t.Fatalf("insert an orphan greeting: %v", err)
	}
	r := itMain(ctx, env, "migrate")
	if r.code != ExitFailure {
		t.Fatalf("migrate over an orphan greeting = %d, want ExitFailure\n%s\n%s", r.code, r.out, r.errOut)
	}
	text := strings.ToLower(r.out + r.errOut)
	if !strings.Contains(text, "greeting") {
		t.Errorf("the output does not name the table or constraint:\n%s\n%s", r.out, r.errOut)
	}
	if strings.Contains(text, orphan) {
		t.Error("the output names the person ID")
	}
	if n := pcCount(t, pool, `SELECT count(*) FROM pg_constraint WHERE conname = 'greeting_person_id_fkey'`); n != 0 {
		t.Error("the failed migration left its constraint behind")
	}
	// Creating the person lets migrate finish.
	if _, err := pool.Exec(ctx, `INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, now(), now())`, orphan); err != nil {
		t.Fatal(err)
	}
	if r := itMain(ctx, env, "migrate"); r.code != ExitOK {
		t.Fatalf("migrate after creating the person = %d\n%s\n%s", r.code, r.out, r.errOut)
	}
}
