//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people/postgres"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/model"
)

var t0 = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func peopleSet() db.Set {
	return db.Set{Name: "people", Schema: "core", Files: postgres.Migrations}
}

func migrate(t *testing.T, pool *db.Pool, sets ...db.Set) []db.Applied {
	t.Helper()
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	applied, err := mig.Migrate(ctxT(t), sets)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return applied
}

func newPool(t *testing.T) *db.Pool {
	t.Helper()
	pool := dbtest.TempDB(t)
	migrate(t, pool, identity.Set, peopleSet())
	return pool
}

func uid() [16]byte { return [16]byte(model.NewID[model.PersonID]()) }

func exec(t *testing.T, pool *db.Pool, sql string, args ...any) error {
	t.Helper()
	_, err := pool.Exec(ctxT(t), sql, args...)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	return nil
}

func mustExec(t *testing.T, pool *db.Pool, sql string, args ...any) {
	t.Helper()
	if err := exec(t, pool, sql, args...); err != nil {
		t.Fatalf("exec %.60s: %v", sql, err)
	}
}

func count(t *testing.T, pool *db.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctxT(t), sql, args...).Scan(&n); err != nil {
		t.Fatalf("query %.60s: %v", sql, err)
	}
	return n
}

func person(t *testing.T, pool *db.Pool) [16]byte {
	t.Helper()
	id := uid()
	mustExec(t, pool, `INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, $2, $2)`, id, t0)
	return id
}

func TestMigrations_ApplyOnAnEmptyDatabase_AndAreIdempotent(t *testing.T) {
	pool := dbtest.TempDB(t)
	applied := migrate(t, pool, identity.Set, peopleSet())
	if len(applied) == 0 {
		t.Fatal("nothing applied")
	}
	if again := migrate(t, pool, identity.Set, peopleSet()); len(again) != 0 {
		t.Errorf("a second Migrate applied %d files", len(again))
	}
	for _, table := range []string{"person", "person_emergency_contact", "person_consent", "data_request"} {
		if n := count(t, pool, `SELECT count(*) FROM information_schema.tables WHERE table_schema = 'core' AND table_name = $1`, table); n != 1 {
			t.Errorf("table core.%s missing", table)
		}
	}
	for _, idx := range []string{"person_playa_name_idx", "data_request_open_idx"} {
		if n := count(t, pool, `SELECT count(*) FROM pg_indexes WHERE schemaname = 'core' AND indexname = $1`, idx); n != 1 {
			t.Errorf("index %s missing", idx)
		}
	}
}

func TestBackfill_OnePersonRowPerExistingAccount(t *testing.T) {
	pool := dbtest.TempDB(t)
	migrate(t, pool, identity.Set) // an M1 installation: accounts, no person rows
	lookup, err := identity.NewLookup(identity.LookupOptions{DB: pool, Tracer: telemetrytest.New(t).Tracer("identity")})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := identity.New(identity.Options{
		Lookup: lookup,
		DB:     pool, Clock: clock.Real(), Logger: telemetrytest.New(t).Logger(), Tracer: telemetrytest.New(t).Tracer("identity"),
		Key: secret.New("people-pg-test-session-key-0123456789abcdef"), Audit: audittest.NewFake("identity"),
		Signals:  mustBus(t),
		Settings: settingstest.NewFake("accounts", identity.Settings),
	})
	if err != nil {
		t.Fatal(err)
	}
	var persons []model.PersonID
	for _, local := range []string{"first.burner", "second.burner", "third.burner"} {
		email, err := model.ParseEmail(local + "@example.org")
		if err != nil {
			t.Fatal(err)
		}
		iss, err := svc.SignUp(httpx.WithScheme(httpx.WithClientAddr(ctxT(t), mustAddr(local)), "https"), identity.Credentials{Email: email, Password: "tangerine-orbit-quiet-77"})
		if err != nil {
			t.Fatalf("SignUp: %v", err)
		}
		persons = append(persons, iss.Session.Person)
	}
	if n := count(t, pool, `SELECT count(*) FROM core.account`); n != 3 {
		t.Fatalf("setup: %d accounts", n)
	}
	migrate(t, pool, identity.Set, peopleSet())
	for _, p := range persons {
		if n := count(t, pool, `SELECT count(*) FROM core.person WHERE id = $1 AND playa_name = '' AND legal_name = '' AND phone = ''`, [16]byte(p)); n != 1 {
			t.Errorf("person %v: %d backfilled rows, want 1 empty row", p, n)
		}
	}
	if n := count(t, pool, `SELECT count(*) FROM core.person`); n != 3 {
		t.Errorf("%d person rows, want exactly one per account", n)
	}
	// The account's creation time is carried over.
	if n := count(t, pool, `SELECT count(*) FROM core.person p JOIN core.account a ON a.person_id = p.id WHERE p.created_at = a.created_at`); n != 3 {
		t.Errorf("%d rows carry the account's creation time, want 3", n)
	}
}

func TestPerson_Constraints_Table(t *testing.T) {
	pool := newPool(t)
	long := func(n int) string { return repeatByte('a', n) }
	tests := []struct {
		name string
		col  string
		val  string
		ok   bool
	}{
		{"playa name 100 bytes", "playa_name", long(100), true},
		{"playa name 101 bytes", "playa_name", long(101), false},
		{"playa name 50 two-byte characters", "playa_name", repeatStr("é", 50), true},
		{"playa name 51 two-byte characters", "playa_name", repeatStr("é", 51), false},
		{"pronouns 40", "pronouns", long(40), true},
		{"pronouns 41", "pronouns", long(41), false},
		{"language 35", "language", long(35), true},
		{"language 36", "language", long(36), false},
		{"legal name 200", "legal_name", long(200), true},
		{"legal name 201", "legal_name", long(201), false},
		{"phone 40", "phone", long(40), true},
		{"phone 41", "phone", long(41), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := exec(t, pool, `INSERT INTO core.person (id, `+tc.col+`, created_at, updated_at) VALUES ($1, $2, $3, $3)`, uid(), tc.val, t0)
			if tc.ok && err != nil {
				t.Fatalf("rejected: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("accepted a value over the limit")
			}
		})
	}
}

func TestPerson_ContactEmail_Constraints(t *testing.T) {
	pool := newPool(t)
	ins := func(v any) error {
		return exec(t, pool, `INSERT INTO core.person (id, contact_email, created_at, updated_at) VALUES ($1, $2, $3, $3)`, uid(), v, t0)
	}
	if err := ins(nil); err != nil {
		t.Errorf("NULL contact_email: %v", err)
	}
	if err := ins("a@b"); err != nil {
		t.Errorf("3-byte address: %v", err)
	}
	if err := ins("ab"); err == nil {
		t.Error("a 2-byte contact_email was accepted")
	}
	if err := ins(""); err == nil {
		t.Error("an empty-string contact_email was accepted (NULL means none)")
	}
	if err := ins(repeatByte('a', 245) + "@example.org"); err == nil {
		t.Error("a contact_email over 254 bytes was accepted")
	}
	// Not unique: two account-less people may share a household address.
	if err := ins("shared@example.org"); err != nil {
		t.Fatal(err)
	}
	if err := ins("shared@example.org"); err != nil {
		t.Errorf("a shared contact address: %v", err)
	}
}

func TestPerson_PrimaryKey_Duplicate_IsConflict(t *testing.T) {
	pool := newPool(t)
	id := person(t, pool)
	err := exec(t, pool, `INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, $2, $2)`, id, t0)
	if !errs.Is(err, errs.Conflict) {
		t.Errorf("duplicate id: kind %v (%v), want Conflict", errs.KindOf(err), err)
	}
}

func TestContact_Constraints_Table(t *testing.T) {
	pool := newPool(t)
	p := person(t, pool)
	ins := func(name, phone, rel string, pos int) error {
		return exec(t, pool, `INSERT INTO core.person_emergency_contact (id, person_id, name, phone, relationship, position, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`, uid(), p, name, phone, rel, pos, t0)
	}
	if err := ins("Orville Tansy", "+1 555 0188", "friend", 1); err != nil {
		t.Fatalf("valid contact: %v", err)
	}
	if err := ins("N", "5", "", 2); err != nil {
		t.Errorf("minimal contact: %v", err)
	}
	for name, err := range map[string]error{
		"empty name":         ins("", "5", "", 3),
		"empty phone":        ins("N", "", "", 4),
		"name 201":           ins(repeatByte('n', 201), "5", "", 5),
		"phone 41":           ins("N", repeatByte('1', 41), "", 6),
		"relationship 61":    ins("N", "5", repeatByte('r', 61), 7),
		"position zero":      ins("N", "5", "", 0),
		"position negative":  ins("N", "5", "", -1),
		"unknown person":     exec(t, pool, `INSERT INTO core.person_emergency_contact (id, person_id, name, phone, position, created_at, updated_at) VALUES ($1, $2, 'N', '5', 1, $3, $3)`, uid(), uid(), t0),
		"duplicate position": ins("Dup", "5", "", 1),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := ins("Dup", "5", "", 1); !errs.Is(err, errs.Conflict) {
		t.Errorf("duplicate position: kind %v, want Conflict", errs.KindOf(err))
	}
	// Positions can be shifted by one statement (the unique constraint is deferrable).
	mustExec(t, pool, `UPDATE core.person_emergency_contact SET position = 3 - position WHERE person_id = $1`, p)
	// Deleting the person removes the contacts (a safety net).
	mustExec(t, pool, `DELETE FROM core.person WHERE id = $1`, p)
	if n := count(t, pool, `SELECT count(*) FROM core.person_emergency_contact WHERE person_id = $1`, p); n != 0 {
		t.Errorf("%d contacts survive their person", n)
	}
}

func TestConsent_Constraints_AndForeignKeys(t *testing.T) {
	pool := newPool(t)
	p, mgr := person(t, pool), person(t, pool)
	ins := func(person [16]byte, purpose string, by any) error {
		return exec(t, pool, `INSERT INTO core.person_consent (person_id, purpose, granted, recorded_by, changed_at) VALUES ($1, $2, true, $3, $4)`, person, purpose, by, t0)
	}
	if err := ins(p, "volunteers.share-contact", mgr); err != nil {
		t.Fatalf("valid consent: %v", err)
	}
	if err := ins(p, "volunteers.share-contact", mgr); !errs.Is(err, errs.Conflict) {
		t.Errorf("one row per (person, purpose): kind %v, want Conflict", errs.KindOf(err))
	}
	if err := ins(p, "x."+repeatByte('a', 63), nil); err == nil {
		t.Error("a purpose over 64 bytes was accepted")
	}
	if err := ins(uid(), "volunteers.other", nil); err == nil {
		t.Error("a consent for an unknown person was accepted")
	}
	// Deleting the manager keeps the consent and clears recorded_by.
	mustExec(t, pool, `DELETE FROM core.person WHERE id = $1`, mgr)
	if n := count(t, pool, `SELECT count(*) FROM core.person_consent WHERE person_id = $1 AND recorded_by IS NULL`, p); n != 1 {
		t.Error("recorded_by was not set NULL, or the consent was removed")
	}
	// Deleting the person removes the consent.
	mustExec(t, pool, `DELETE FROM core.person WHERE id = $1`, p)
	if n := count(t, pool, `SELECT count(*) FROM core.person_consent`); n != 0 {
		t.Errorf("%d consents survive their person", n)
	}
}

func TestDataRequest_Constraints(t *testing.T) {
	pool := newPool(t)
	p, admin := person(t, pool), person(t, pool)
	ins := func(person [16]byte, kind, status, note string) error {
		var decided any
		if status != "open" {
			decided = t0
		}
		return exec(t, pool, `INSERT INTO core.data_request (id, person_id, kind, status, note, created_at, decided_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			uid(), person, kind, status, note, t0, decided)
	}
	if err := ins(p, "delete", "open", "please"); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	for name, err := range map[string]error{
		"export kind":    ins(p, "export", "withdrawn", ""),
		"bad status":     ins(p, "delete", "bogus", ""),
		"note over 500":  ins(p, "delete", "withdrawn", repeatByte('n', 501)),
		"unknown person": ins(uid(), "delete", "open", ""),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := ins(p, "delete", "open", ""); !errs.Is(err, errs.Conflict) {
		t.Errorf("a second open request: kind %v (%v), want Conflict", errs.KindOf(err), err)
	}
	// Decided requests do not count: any number may exist beside the open one.
	for _, st := range []string{"withdrawn", "declined", "withdrawn"} {
		if err := ins(p, "delete", st, ""); err != nil {
			t.Errorf("a %s request beside the open one: %v", st, err)
		}
	}
	// Deleting the decider keeps the request.
	mustExec(t, pool, `UPDATE core.data_request SET decided_by = $2 WHERE person_id = $1 AND status = 'declined'`, p, admin)
	mustExec(t, pool, `DELETE FROM core.person WHERE id = $1`, admin)
	if n := count(t, pool, `SELECT count(*) FROM core.data_request WHERE person_id = $1 AND status = 'declined' AND decided_by IS NULL`, p); n != 1 {
		t.Error("decided_by was not set NULL, or the request was removed")
	}
	mustExec(t, pool, `DELETE FROM core.person WHERE id = $1`, p)
	if n := count(t, pool, `SELECT count(*) FROM core.data_request`); n != 0 {
		t.Errorf("%d requests survive their person", n)
	}
}

func TestPerson_CreatedBy_SetNullOnDelete(t *testing.T) {
	pool := newPool(t)
	mgr := person(t, pool)
	child := uid()
	mustExec(t, pool, `INSERT INTO core.person (id, created_by, created_at, updated_at) VALUES ($1, $2, $3, $3)`, child, mgr, t0)
	mustExec(t, pool, `DELETE FROM core.person WHERE id = $1`, mgr)
	if n := count(t, pool, `SELECT count(*) FROM core.person WHERE id = $1 AND created_by IS NULL`, child); n != 1 {
		t.Error("created_by was not cleared")
	}
}

func TestForeignKeyFromElsewhere_RestrictsDeletion(t *testing.T) {
	pool := newPool(t)
	p := person(t, pool)
	mustExec(t, pool, `CREATE TABLE core.zz_module (person_id uuid NOT NULL REFERENCES core.person(id) ON DELETE RESTRICT)`)
	mustExec(t, pool, `INSERT INTO core.zz_module VALUES ($1)`, p)
	if err := exec(t, pool, `DELETE FROM core.person WHERE id = $1`, p); err == nil {
		t.Error("a person with a module row was deleted")
	}
}

func mustBus(t *testing.T) signals.Bus {
	t.Helper()
	b, err := signalstest.NewFake("identity", []signals.Name{identity.SignalAccountCreated}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
