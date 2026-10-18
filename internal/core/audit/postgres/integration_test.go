//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func migrated(t *testing.T) *db.Pool {
	t.Helper()
	pool := dbtest.TempDB(t)
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	if _, err := mig.Migrate(ctxT(t), []db.Set{audit.Set}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return pool
}

const insertSQL = `INSERT INTO core.audit_entry
	(id, at, instance, action, outcome, actor_kind, actor_person_id, actor_superuser, subject_person_id, record_kind, record_id, fields, level, reason, trace_id, actor_titles)
	VALUES ($1::uuid, $2, $3, $4, $5, $6, $7::uuid, $8, $9::uuid, $10, $11::uuid, $12::text[], $13, $14, $15, $16::text[])`

// row is one INSERT's arguments; valid() is a row every CHECK accepts.
type row struct {
	id, instance, action, outcome, kind string
	actor                               any
	superuser                           bool
	subject                             any
	recKind                             any
	recID                               any
	fields                              []string
	level                               any
	reason                              any
	trace                               any
	titles                              []string
}

func valid() row {
	return row{instance: "test-copy", action: "identity.login", outcome: "succeeded", kind: "anonymous", fields: []string{}, titles: []string{}}
}

func (r row) insert(ctx context.Context, p *db.Pool) error {
	id := r.id
	if id == "" {
		id = model.NewID[audit.RecordID]().String()
	}
	_, err := p.Exec(ctx, insertSQL, id, time.Now(), r.instance, r.action, r.outcome, r.kind, r.actor, r.superuser,
		r.subject, r.recKind, r.recID, r.fields, r.level, r.reason, r.trace, r.titles)
	return err //nolint:wrapcheck // Pool.Exec already classified it
}

func count(t *testing.T, p *db.Pool) int {
	t.Helper()
	var n int
	if err := p.QueryRow(ctxT(t), "SELECT count(*) FROM core.audit_entry").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestMigration_AppliesAndIsVersioned(t *testing.T) {
	v, err := db.Version(audit.Migrations)
	if err != nil || v < 1 {
		t.Fatalf("Version = %d, %v", v, err)
	}
	if audit.Set.Name != "audit" || audit.Set.Schema != "core" {
		t.Errorf("Set = %+v, want name audit, schema core", audit.Set)
	}
	p := migrated(t)
	if err := valid().insert(ctxT(t), p); err != nil {
		t.Fatalf("a valid row was refused (kind %v)", errs.KindOf(err))
	}
	// Migrating again is a no-op.
	mig, err := db.NewMigrator(p, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mig.Migrate(ctxT(t), []db.Set{audit.Set}); err != nil {
		t.Fatalf("second Migrate failed (kind %v)", errs.KindOf(err))
	}
}

func TestChecks_RefuseIncoherentRows_AsInvalid(t *testing.T) {
	p := migrated(t)
	alice := model.PersonID{1}.String()
	rec := model.AccountID{2}.String()
	nilUUID := "00000000-0000-0000-0000-000000000000"
	tests := []struct {
		name string
		mod  func(*row)
	}{
		{"person actor without person", func(r *row) { r.kind = "person" }},
		{"anonymous actor with person", func(r *row) { r.actor = alice }},
		{"superuser without person actor", func(r *row) { r.superuser = true }},
		{"unknown actor kind", func(r *row) { r.kind = "wizard" }},
		{"unknown outcome", func(r *row) { r.outcome = "maybe" }},
		{"bad action grammar", func(r *row) { r.action = "Identity.Login" }},
		{"action without ident", func(r *row) { r.action = "identity" }},
		{"bad instance", func(r *row) { r.instance = "Bad Instance" }},
		{"bad trace id", func(r *row) { r.trace = "xyz" }},
		{"fields without level", func(r *row) { r.fields = []string{"email"} }},
		{"level without fields", func(r *row) { r.level = "personal" }},
		{"unknown level", func(r *row) { r.fields = []string{"email"}; r.level = "secret" }},
		{"bad field name", func(r *row) { r.fields = []string{"Legal Name"}; r.level = "personal" }},
		{"record kind without id", func(r *row) { r.recKind = "identity.account" }},
		{"record id without kind", func(r *row) { r.recID = rec }},
		{"nil uuid subject", func(r *row) { r.subject = nilUUID }},
		{"nil uuid record", func(r *row) { r.recKind = "identity.account"; r.recID = nilUUID }},
		{"reason over 256 bytes", func(r *row) { r.reason = string(make([]byte, 0)) + repeat("a", 257) }},
		{"too many titles", func(r *row) {
			r.kind, r.actor = "person", alice
			for range 17 {
				r.titles = append(r.titles, "lead")
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := valid()
			tc.mod(&r)
			err := r.insert(ctxT(t), p)
			if err == nil || !errs.Is(err, errs.Invalid) {
				t.Fatalf("kind %v (err nil: %v), want Invalid", errs.KindOf(err), err == nil)
			}
		})
	}
	if n := count(t, p); n != 0 {
		t.Errorf("%d incoherent rows were stored", n)
	}
	t.Run("coherent rows pass", func(t *testing.T) {
		for _, mod := range []func(*row){
			func(r *row) { r.kind, r.actor = "person", alice },
			func(r *row) { r.kind, r.actor, r.superuser = "person", alice, true },
			func(r *row) { r.kind = "system" },
			func(r *row) { r.recKind, r.recID = "identity.account", rec },
			func(r *row) { r.fields, r.level = []string{"email", "last_login_at"}, "personal" },
			func(r *row) { r.reason = repeat("a", 256) },
			func(r *row) { r.trace = repeat("a", 32) },
		} {
			r := valid()
			mod(&r)
			if err := r.insert(ctxT(t), p); err != nil {
				t.Errorf("a coherent row was refused (kind %v)", errs.KindOf(err))
			}
		}
	})
}

func repeat(s string, n int) string {
	out := make([]byte, 0, n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}

func TestInsert_DuplicateID_IsConflict(t *testing.T) {
	p := migrated(t)
	r := valid()
	r.id = model.NewID[audit.RecordID]().String()
	if err := r.insert(ctxT(t), p); err != nil {
		t.Fatal(err)
	}
	if err := r.insert(ctxT(t), p); !errs.Is(err, errs.Conflict) {
		t.Errorf("kind %v, want Conflict", errs.KindOf(err))
	}
}

func TestGuard_UpdateDeleteTruncate_Refused(t *testing.T) {
	p := migrated(t)
	r := valid()
	if err := r.insert(ctxT(t), p); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"UPDATE core.audit_entry SET action = 'identity.tampered'",
		"UPDATE core.audit_entry SET reason = NULL",
		"DELETE FROM core.audit_entry",
		"DELETE FROM core.audit_entry WHERE true",
		"TRUNCATE core.audit_entry",
		"TRUNCATE TABLE core.audit_entry",
	} {
		t.Run(stmt, func(t *testing.T) {
			_, err := p.Exec(ctxT(t), stmt)
			if err == nil {
				t.Fatal("statement succeeded")
			}
			if !errs.Is(err, errs.Internal) {
				t.Errorf("kind %v, want Internal (a refused write is a bug)", errs.KindOf(err))
			}
		})
	}
	if n := count(t, p); n != 1 {
		t.Fatalf("rows = %d, want the 1 inserted", n)
	}
	var action string
	if err := p.QueryRow(ctxT(t), "SELECT action FROM core.audit_entry").Scan(&action); err != nil || action != "identity.login" {
		t.Errorf("row changed: %q %v", action, err)
	}
}

func TestAuditPurge_IsTheDeletionPath(t *testing.T) {
	p := migrated(t)
	if _, err := p.Exec(ctxT(t), `INSERT INTO core.audit_entry (id, at, instance, action, outcome, actor_kind)
		VALUES ($1::uuid, $2, 'test-copy', 'identity.login', 'succeeded', 'anonymous')`,
		model.NewID[audit.RecordID]().String(), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctxT(t), "DELETE FROM core.audit_entry"); err == nil {
		t.Fatal("a plain DELETE succeeded")
	}
	var deleted int64
	err := p.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		return p.QueryRow(ctx, "SELECT core.audit_purge($1)", time.Now().Add(-time.Minute)).Scan(&deleted)
	})
	if err != nil {
		t.Fatalf("core.audit_purge failed (kind %v)", errs.KindOf(err))
	}
	if deleted != 1 {
		t.Errorf("audit_purge deleted %d, want 1", deleted)
	}
	if n := count(t, p); n != 0 {
		t.Errorf("rows after purge = %d", n)
	}
}

func TestGuard_FlagDoesNotOutliveThePurge(t *testing.T) {
	p := migrated(t)
	for range 2 {
		if err := valid().insert(ctxT(t), p); err != nil {
			t.Fatal(err)
		}
	}
	err := p.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		var n int64
		if err := p.QueryRow(ctx, "SELECT core.audit_purge($1)", time.Now().Add(-time.Hour)).Scan(&n); err != nil {
			return err //nolint:wrapcheck // test
		}
		// Same transaction, after the purge: a plain DELETE is refused again.
		_, err := p.Exec(ctx, "DELETE FROM core.audit_entry")
		return err //nolint:wrapcheck // test
	})
	if err == nil {
		t.Fatal("a plain DELETE after core.audit_purge in the same transaction succeeded")
	}
	if n := count(t, p); n != 2 {
		t.Errorf("rows = %d, want 2 (the transaction rolled back)", n)
	}
}

func TestAuditPurge_DeletesOnlyOlderThanBefore_AndCounts(t *testing.T) {
	p := migrated(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ins := func(at time.Time) {
		t.Helper()
		_, err := p.Exec(ctxT(t), `INSERT INTO core.audit_entry (id, at, instance, action, outcome, actor_kind)
			VALUES ($1::uuid, $2, 'test-copy', 'identity.login', 'succeeded', 'anonymous')`,
			model.NewID[audit.RecordID]().String(), at)
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	ins(base)
	ins(base.Add(time.Hour))
	ins(base.Add(2 * time.Hour)) // exactly "before": kept (at < before)
	ins(base.Add(3 * time.Hour))
	var n int64
	err := p.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		return p.QueryRow(ctx, "SELECT core.audit_purge($1)", base.Add(2*time.Hour)).Scan(&n)
	})
	if err != nil || n != 2 {
		t.Fatalf("purged %d, err %v; want 2", n, err)
	}
	if left := count(t, p); left != 2 {
		t.Errorf("rows left = %d, want 2", left)
	}
}

func TestIndexes_KeysetAndFilterColumnsAreIndexed(t *testing.T) {
	p := migrated(t)
	rows, err := p.Query(ctxT(t), `SELECT indexdef FROM pg_indexes WHERE schemaname = 'core' AND tablename = 'audit_entry'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var defs []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		defs = append(defs, d)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, col := range []string{"subject_person_id", "actor_person_id", "record_kind", "actor_superuser"} {
		found := false
		for _, d := range defs {
			found = found || contains(d, col)
		}
		if !found {
			t.Errorf("no index mentions %s", col)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestAuditPurge_FutureBefore_ReturnsInvalid(t *testing.T) {
	p := migrated(t)
	if err := valid().insert(ctxT(t), p); err != nil {
		t.Fatal(err)
	}
	err := p.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		var n int64
		return p.QueryRow(ctx, "SELECT core.audit_purge($1)", time.Now().Add(time.Hour)).Scan(&n)
	})
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("kind %v, want Invalid", errs.KindOf(err))
	}
	var pe *db.Error
	if !errors.As(err, &pe) || pe.Code != "22023" {
		t.Errorf("error is not SQLSTATE 22023: %v", err)
	}
	if n := count(t, p); n != 1 {
		t.Errorf("rows = %d, want 1: a refused purge deletes nothing", n)
	}
}
