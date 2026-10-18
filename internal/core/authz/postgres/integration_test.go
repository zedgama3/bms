//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/model"
)

const nilUUID = "00000000-0000-0000-0000-000000000000"

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func migrated(t testing.TB) *db.Pool {
	t.Helper()
	pool := dbtest.TempDB(t)
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mig.Migrate(ctxT(t), []db.Set{identity.Set, people.Set, authz.Set}); err != nil {
		t.Fatalf("migrating authz on an empty database: %v", err)
	}
	return pool
}

// person inserts a core.person row (an empty profile) and returns its ID.
func person(t testing.TB, pool *db.Pool) [16]byte {
	t.Helper()
	id := uid()
	mustExec(t, pool, "INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, now(), now())", id)
	return id
}

func uid() [16]byte { return model.NewID[model.PersonID]() }

func exec(t testing.TB, pool *db.Pool, sql string, args ...any) error {
	t.Helper()
	_, err := pool.Exec(ctxT(t), sql, args...)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	return nil
}

func mustExec(t testing.TB, pool *db.Pool, sql string, args ...any) {
	t.Helper()
	if err := exec(t, pool, sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func wantRejected(t testing.TB, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s was accepted", what)
	}
}

func newRole(t testing.TB, pool *db.Pool, name, scope string) [16]byte {
	t.Helper()
	id := uid()
	mustExec(t, pool, "INSERT INTO core.role (id, name, scope, created_at, updated_at) VALUES ($1,$2,$3,now(),now())", id, name, scope)
	return id
}

func count(t testing.TB, pool *db.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctxT(t), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func TestMigration_AppliesOnEmptyDatabase_AndSeedsEightRoles(t *testing.T) {
	pool := migrated(t)
	if n := count(t, pool, "SELECT count(*) FROM core.role"); n != 8 {
		t.Fatalf("%d roles, want 8", n)
	}
	if n := count(t, pool, "SELECT count(*) FROM core.role WHERE base"); n != 1 {
		t.Fatalf("%d base roles", n)
	}
	if n := count(t, pool, "SELECT count(*) FROM core.role WHERE key = 'participant' AND base AND scope = 'site' AND name = ''"); n != 1 {
		t.Error("participant is not the unnamed site base role")
	}
	if n := count(t, pool, "SELECT count(*) FROM core.role WHERE key = 'department-admin' AND scope = 'department' AND NOT base"); n != 1 {
		t.Error("department-admin is not a department role")
	}
	if n := count(t, pool, "SELECT count(*) FROM core.role_permission"); n != 0 {
		t.Errorf("%d permissions seeded by the migration, want none (Seed fills them)", n)
	}
	if n := count(t, pool, "SELECT count(*) FROM core.role_seed"); n != 0 {
		t.Errorf("%d seed rows from the migration", n)
	}
}

func TestMigration_EveryTableCommentsItsSensitivity(t *testing.T) {
	pool := migrated(t)
	for _, tbl := range []string{"role", "role_permission", "role_grant", "sod_rule", "role_seed"} {
		var c *string
		err := pool.QueryRow(ctxT(t), "SELECT obj_description(('core.'||$1)::regclass, 'pg_class')", tbl).Scan(&c)
		if err != nil {
			t.Fatalf("%s: %v", tbl, err)
		}
		if c == nil || !strings.Contains(strings.ToLower(*c), "internal") {
			t.Errorf("core.%s has no COMMENT naming its sensitivity (internal): %v", tbl, c)
		}
	}
}

func TestMigration_Twice_IsNoOp(t *testing.T) {
	pool := migrated(t)
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mig.Migrate(ctxT(t), []db.Set{identity.Set, people.Set, authz.Set}); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if n := count(t, pool, "SELECT count(*) FROM core.role"); n != 8 {
		t.Fatalf("%d roles after a second migrate", n)
	}
}

func TestRole_Constraints(t *testing.T) {
	pool := migrated(t)
	ins := func(id any, key any, name, desc, scope string, base bool) error {
		return exec(t, pool, "INSERT INTO core.role (id,key,name,description,scope,base,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,now(),now())", id, key, name, desc, scope, base)
	}
	wantRejected(t, ins(uid(), nil, "x", "", "planet", false), "a bad scope")
	wantRejected(t, ins(uid(), nil, "d", "", "department", true), "a department base role")
	wantRejected(t, ins(uid(), nil, "b", "", "site", true), "a second base role")
	wantRejected(t, ins(uid(), "BadKey", "k", "", "site", false), "a malformed key")
	wantRejected(t, ins(uid(), "account-admin", "dupkey", "", "site", false), "a duplicate key")
	wantRejected(t, ins(uid(), nil, strings.Repeat("n", 65), "", "site", false), "a 65-byte name")
	wantRejected(t, ins(uid(), nil, "ok", strings.Repeat("d", 513), "site", false), "a 513-byte description")
	wantRejected(t, ins(nilUUID, nil, "nil", "", "site", false), "the nil UUID")
	wantRejected(t, ins(uid(), nil, "", "", "site", false), "a custom role without a name")
	if err := ins(uid(), nil, "Gate Crew", "", "site", false); err != nil {
		t.Fatal(err)
	}
	wantRejected(t, ins(uid(), nil, "gate crew", "", "site", false), "a name that differs only in case")
	// Unnamed starter roles do not collide with each other.
	if n := count(t, pool, "SELECT count(*) FROM core.role WHERE name = ''"); n != 8 {
		t.Errorf("%d unnamed roles", n)
	}
	if err := ins(uid(), nil, strings.Repeat("n", 64), strings.Repeat("d", 512), "site", false); err != nil {
		t.Errorf("limits are inclusive: %v", err)
	}
}

func TestRolePermission_Constraints_AndCascade(t *testing.T) {
	pool := migrated(t)
	r := newRole(t, pool, "R", "site")
	mustExec(t, pool, "INSERT INTO core.role_permission VALUES ($1,'med.reports.read')", r)
	wantRejected(t, exec(t, pool, "INSERT INTO core.role_permission VALUES ($1,'med.reports.read')", r), "a duplicate permission")
	wantRejected(t, exec(t, pool, "INSERT INTO core.role_permission VALUES ($1,'Bad Name')", r), "a malformed permission")
	wantRejected(t, exec(t, pool, "INSERT INTO core.role_permission VALUES ($1,'single')", r), "a one-segment permission")
	wantRejected(t, exec(t, pool, "INSERT INTO core.role_permission VALUES ($1,'a.b')", uid()), "a permission for an unknown role")
	mustExec(t, pool, "DELETE FROM core.role WHERE id = $1", r)
	if n := count(t, pool, "SELECT count(*) FROM core.role_permission WHERE role_id = $1", r); n != 0 {
		t.Error("deleting a role left its permissions")
	}
}

func TestRoleGrant_UniqueIncludingSiteWide_AndRestrict(t *testing.T) {
	pool := migrated(t)
	r := newRole(t, pool, "R", "site")
	p1, dept := person(t, pool), uid()
	ins := func(p [16]byte, d any) error {
		return exec(t, pool, "INSERT INTO core.role_grant (id, person_id, role_id, department_id, created_at) VALUES ($1,$2,$3,$4,now())", uid(), p, r, d)
	}
	if err := ins(p1, nil); err != nil {
		t.Fatal(err)
	}
	wantRejected(t, ins(p1, nil), "two site-wide grants of one role to one person (NULL department)")
	if err := ins(p1, dept); err != nil {
		t.Fatalf("same role in a department is a different grant: %v", err)
	}
	wantRejected(t, ins(p1, dept), "a duplicate department grant")
	if err := ins(person(t, pool), nil); err != nil {
		t.Fatalf("another person: %v", err)
	}
	wantRejected(t, exec(t, pool, "DELETE FROM core.role WHERE id = $1", r), "deleting a role that has grants (RESTRICT)")
	wantRejected(t, exec(t, pool, "INSERT INTO core.role_grant (id, person_id, role_id, created_at) VALUES ($1,$2,$3,now())", uid(), nilUUID, r), "the nil person")
	wantRejected(t, exec(t, pool, "INSERT INTO core.role_grant (id, person_id, role_id, department_id, created_at) VALUES ($1,$2,$3,$4,now())", uid(), person(t, pool), r, nilUUID), "the nil department")
	wantRejected(t, exec(t, pool, "INSERT INTO core.role_grant (id, person_id, role_id, created_at) VALUES ($1,$2,$3,now())", uid(), person(t, pool), uid()), "a grant of an unknown role")
}

func TestSodRule_Constraints(t *testing.T) {
	pool := migrated(t)
	r := newRole(t, pool, "R", "site")
	ins := func(perm, reaches any, note string) error {
		return exec(t, pool, "INSERT INTO core.sod_rule (id, role_id, permission, reaches, note, created_at) VALUES ($1,$2,$3,$4,$5,now())", uid(), r, perm, reaches, note)
	}
	wantRejected(t, ins(nil, nil, ""), "a rule with neither permission nor level")
	wantRejected(t, ins("a.b", "personal", ""), "a rule with both")
	wantRejected(t, ins(nil, "secret", ""), "a bad level")
	wantRejected(t, ins("Bad Name", nil, ""), "a malformed permission")
	wantRejected(t, ins("a.b", nil, strings.Repeat("n", 257)), "a 257-byte note")
	if err := ins("a.b", nil, strings.Repeat("n", 256)); err != nil {
		t.Fatal(err)
	}
	wantRejected(t, ins("a.b", nil, ""), "a duplicate permission rule")
	if err := ins(nil, "personal", ""); err != nil {
		t.Fatal(err)
	}
	wantRejected(t, ins(nil, "personal", ""), "a duplicate level rule")
	if err := ins(nil, "sensitive", ""); err != nil {
		t.Fatalf("another level: %v", err)
	}
	wantRejected(t, exec(t, pool, "DELETE FROM core.role WHERE id = $1", r), "deleting a role named by a rule (RESTRICT)")
}

func TestRoleSeed_PrimaryKey(t *testing.T) {
	pool := migrated(t)
	mustExec(t, pool, "INSERT INTO core.role_seed (role_key, permission, seeded_at) VALUES ('event-admin','a.b',now())")
	wantRejected(t, exec(t, pool, "INSERT INTO core.role_seed (role_key, permission, seeded_at) VALUES ('event-admin','a.b',now())"), "a repeated seed pair")
	mustExec(t, pool, "INSERT INTO core.role_seed (role_key, permission, seeded_at) VALUES ('event-admin','a.c',now())")
	mustExec(t, pool, "INSERT INTO core.role_seed (role_key, permission, seeded_at) VALUES ('no-such-role','a.b',now())") // not a foreign key
}

func grantRow(t testing.TB, pool *db.Pool, p [16]byte, role [16]byte, grantedBy any) [16]byte {
	t.Helper()
	id := uid()
	mustExec(t, pool, "INSERT INTO core.role_grant (id, person_id, role_id, granted_by, created_at) VALUES ($1,$2,$3,$4,now())", id, p, role, grantedBy)
	return id
}

func TestMigration0002_GrantToUnknownPerson_IsRefused(t *testing.T) {
	pool := migrated(t)
	r := newRole(t, pool, "R", "site")
	wantRejected(t, exec(t, pool, "INSERT INTO core.role_grant (id, person_id, role_id, created_at) VALUES ($1,$2,$3,now())", uid(), uid(), r), "a grant to a person with no core.person row")
	wantRejected(t, exec(t, pool, "INSERT INTO core.role_grant (id, person_id, role_id, granted_by, created_at) VALUES ($1,$2,$3,$4,now())", uid(), person(t, pool), r, uid()), "a grant by a granter with no core.person row")
}

func TestMigration0002_PersonDelete_RestrictedByGrant_SetNullOnGrantedBy(t *testing.T) {
	pool := migrated(t)
	r := newRole(t, pool, "R", "site")
	holder, granter := person(t, pool), person(t, pool)
	g := grantRow(t, pool, holder, r, granter)

	wantRejected(t, exec(t, pool, "DELETE FROM core.person WHERE id = $1", holder), "deleting a person who still holds a grant (RESTRICT)")

	mustExec(t, pool, "DELETE FROM core.person WHERE id = $1", granter) // SET NULL
	var by *[16]byte
	if err := pool.QueryRow(ctxT(t), "SELECT granted_by FROM core.role_grant WHERE id = $1", g).Scan(&by); err != nil {
		t.Fatal(err)
	}
	if by != nil {
		t.Errorf("granted_by = %v after the granter was deleted, want NULL", *by)
	}
	if n := count(t, pool, "SELECT count(*) FROM core.role_grant WHERE id = $1", g); n != 1 {
		t.Error("the grant did not outlive its granter")
	}
	// Once the grant is gone the holder can be deleted.
	mustExec(t, pool, "DELETE FROM core.role_grant WHERE id = $1", g)
	mustExec(t, pool, "DELETE FROM core.person WHERE id = $1", holder)
}

// firstOnly is the authz set with only 0001, to build the "M1 window" state.
func firstOnly(t testing.TB) db.Set {
	t.Helper()
	data, err := fs.ReadFile(authz.Set.Files, "0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	return db.Set{Name: authz.Set.Name, Schema: authz.Set.Schema, Files: fstest.MapFS{"0001_init.sql": &fstest.MapFile{Data: data}}}
}

func TestMigration0002_Preflight_RefusesOrphanGrants_AndRollsBack(t *testing.T) {
	pool := dbtest.TempDB(t)
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mig.Migrate(ctxT(t), []db.Set{identity.Set, people.Set, firstOnly(t)}); err != nil {
		t.Fatalf("migrating up to authz 0001: %v", err)
	}
	r := newRole(t, pool, "R", "site")
	ghost := uid() // a person with no core.person row
	known, ghostGranter := person(t, pool), uid()
	grantRow(t, pool, ghost, r, nil)
	okGrant := grantRow(t, pool, known, r, ghostGranter) // orphan granted_by only

	_, err = mig.Migrate(ctxT(t), []db.Set{identity.Set, people.Set, authz.Set})
	if err == nil {
		t.Fatal("migration 0002 ran over a grant to a person with no core.person row")
	}
	if strings.Contains(err.Error(), fmtID(ghost)) {
		t.Errorf("the failure names a person ID: %v", err)
	}
	if n := count(t, pool, "SELECT count(*) FROM pg_constraint WHERE conname IN ('role_grant_person_id_fkey','role_grant_granted_by_fkey')"); n != 0 {
		t.Errorf("%d constraints left after the failed migration; it must roll back whole", n)
	}
	if n := count(t, pool, "SELECT count(*) FROM core.role_grant WHERE person_id = $1", ghost); n != 1 {
		t.Error("the orphan grant was deleted silently")
	}

	// The operator creates the person; migrate again succeeds, and the orphan granted_by is nulled.
	mustExec(t, pool, "INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, now(), now())", ghost)
	if _, err := mig.Migrate(ctxT(t), []db.Set{identity.Set, people.Set, authz.Set}); err != nil {
		t.Fatalf("migrate after fixing the orphan: %v", err)
	}
	var by *[16]byte
	if err := pool.QueryRow(ctxT(t), "SELECT granted_by FROM core.role_grant WHERE id = $1", okGrant).Scan(&by); err != nil {
		t.Fatal(err)
	}
	if by != nil {
		t.Error("a granted_by with no person row was not set to NULL")
	}
	if n := count(t, pool, "SELECT count(*) FROM pg_constraint WHERE conname IN ('role_grant_person_id_fkey','role_grant_granted_by_fkey')"); n != 2 {
		t.Errorf("%d constraints after 0002, want 2", n)
	}
}

func fmtID(id [16]byte) string { return model.PersonID(id).String() }
