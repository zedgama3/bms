//go:build integration

package db_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"net/url"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
)

type migRow struct {
	Set      string
	Version  int
	Name     string
	Checksum string
	Duration int
}

func migRows(t testing.TB, q db.Querier) []migRow {
	t.Helper()
	rows, err := q.Query(ctxT(t), "SELECT set_name, version, name, checksum, duration_ms FROM core.schema_migration ORDER BY set_name, version")
	if err != nil {
		t.Fatalf("read schema_migration: %v", err)
	}
	defer rows.Close()
	var out []migRow
	for rows.Next() {
		var r migRow
		if err := rows.Scan(&r.Set, &r.Version, &r.Name, &r.Checksum, &r.Duration); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

func hasTable(t testing.TB, q db.Querier, qualified string) bool {
	t.Helper()
	var ok bool
	if err := q.QueryRow(ctxT(t), "SELECT to_regclass($1) IS NOT NULL", qualified).Scan(&ok); err != nil {
		t.Fatalf("to_regclass: %v", err)
	}
	return ok
}

func sum(t testing.TB, files fs.FS, name string) string {
	t.Helper()
	b, err := fs.ReadFile(files, name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func mset(name string, files map[string]string) db.Set {
	m := fstest.MapFS{}
	for n, body := range files {
		m[n] = &fstest.MapFile{Data: []byte(body)}
	}
	return db.Set{Name: name, Schema: name, Files: m}
}

const createGamma = "CREATE TABLE gamma.t (id integer PRIMARY KEY);\n"

func gamma(extra map[string]string) db.Set {
	files := map[string]string{"0001_create_t.sql": createGamma}
	for k, v := range extra {
		files[k] = v
	}
	return mset("gamma", files)
}

func TestMigrate_EmptyDatabase_AppliesBootstrapThenSetsInOrder(t *testing.T) {
	p, _ := tempDB(t)
	m := migrator(t, p)
	alpha, beta := fixtureSet(t, "alpha"), fixtureSet(t, "beta")

	applied, err := m.Migrate(ctxT(t), []db.Set{alpha, beta})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	type key struct {
		set string
		v   int
	}
	var got []key
	for _, a := range applied {
		got = append(got, key{a.Set, a.Version})
		if a.Name == "" || a.Duration < 0 {
			t.Errorf("incomplete Applied: %+v", a)
		}
	}
	want := []key{{"db", 1}, {"alpha", 1}, {"alpha", 2}, {"beta", 1}}
	if len(got) != len(want) {
		t.Fatalf("applied %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("applied %v, want %v", got, want)
		}
	}
	if applied[1].Name != "create_item" || applied[2].Name != "add_note" {
		t.Errorf("Applied names = %q, %q", applied[1].Name, applied[2].Name)
	}

	for _, schema := range []string{"core", "alpha", "beta"} {
		if n := count(t, p, "SELECT count(*) FROM pg_namespace WHERE nspname = $1", schema); n != 1 {
			t.Errorf("schema %s missing", schema)
		}
	}
	if !hasTable(t, p, "alpha.item") || !hasTable(t, p, "beta.thing") {
		t.Errorf("the set's tables were not created")
	}
	rows := migRows(t, p)
	if len(rows) != 4 {
		t.Fatalf("schema_migration rows = %+v", rows)
	}
	byKey := map[key]migRow{}
	for _, r := range rows {
		byKey[key{r.Set, r.Version}] = r
		if len(r.Checksum) != 64 || r.Duration < 0 {
			t.Errorf("bad row %+v", r)
		}
	}
	if r := byKey[key{"alpha", 1}]; r.Name != "create_item" || r.Checksum != sum(t, alpha.Files, "0001_create_item.sql") {
		t.Errorf("alpha 1 row = %+v", r)
	}
	if r := byKey[key{"alpha", 2}]; r.Name != "add_note" || r.Checksum != sum(t, alpha.Files, "0002_add_note.sql") {
		t.Errorf("alpha 2 row = %+v", r)
	}
	if r := byKey[key{"beta", 1}]; r.Checksum != sum(t, beta.Files, "0001_create_thing.sql") {
		t.Errorf("beta 1 row = %+v", r)
	}
	if r := byKey[key{"db", 1}]; r.Name == "" {
		t.Errorf("bootstrap row = %+v", r)
	}
}

func TestMigrate_Idempotent_SecondRunAppliesNothing(t *testing.T) {
	p, _ := tempDB(t)
	m := migrator(t, p)
	sets := []db.Set{fixtureSet(t, "alpha"), fixtureSet(t, "beta")}
	if _, err := m.Migrate(ctxT(t), sets); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	before := migRows(t, p)
	applied, err := m.Migrate(ctxT(t), sets)
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("second run applied %+v", applied)
	}
	after := migRows(t, p)
	if len(before) != len(after) {
		t.Errorf("rows changed from %d to %d", len(before), len(after))
	}
}

func TestMigrate_NoSets_AppliesOnlyBootstrap(t *testing.T) {
	p, _ := tempDB(t)
	applied, err := migrator(t, p).Migrate(ctxT(t), nil)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(applied) != 1 || applied[0].Set != "db" {
		t.Errorf("applied = %+v", applied)
	}
	if !hasTable(t, p, "core.schema_migration") {
		t.Errorf("bootstrap table missing")
	}
}

func TestMigrate_NewFileOnRerun_AppliesOnlyThePendingOne(t *testing.T) {
	p, _ := tempDB(t)
	m := migrator(t, p)
	if _, err := m.Migrate(ctxT(t), []db.Set{gamma(nil)}); err != nil {
		t.Fatalf("first: %v", err)
	}
	v2 := gamma(map[string]string{"0002_add_col.sql": "ALTER TABLE gamma.t ADD COLUMN c text;\n"})
	applied, err := m.Migrate(ctxT(t), []db.Set{v2})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if len(applied) != 1 || applied[0].Set != "gamma" || applied[0].Version != 2 || applied[0].Name != "add_col" {
		t.Errorf("applied = %+v", applied)
	}
	if n := count(t, p, "SELECT count(*) FROM information_schema.columns WHERE table_schema='gamma' AND table_name='t' AND column_name='c'"); n != 1 {
		t.Errorf("column not added")
	}
}

func TestMigrate_CreatesSchemaIfMissing(t *testing.T) {
	p, _ := tempDB(t)
	if n := count(t, p, "SELECT count(*) FROM pg_namespace WHERE nspname = 'gamma'"); n != 0 {
		t.Fatalf("schema exists before Migrate")
	}
	if _, err := migrator(t, p).Migrate(ctxT(t), []db.Set{gamma(nil)}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if n := count(t, p, "SELECT count(*) FROM pg_namespace WHERE nspname = 'gamma'"); n != 1 {
		t.Errorf("schema gamma not created")
	}
	// A schema that already exists is fine.
	mustExec(t, p, "CREATE SCHEMA delta")
	delta := mset("delta", map[string]string{"0001_t.sql": "CREATE TABLE delta.t (id integer);\n"})
	if _, err := migrator(t, p).Migrate(ctxT(t), []db.Set{delta}); err != nil {
		t.Fatalf("Migrate with existing schema: %v", err)
	}
}

func TestMigrate_DisabledSetNotPassed_IsLeftAlone(t *testing.T) {
	p, _ := tempDB(t)
	m := migrator(t, p)
	alpha, beta := fixtureSet(t, "alpha"), fixtureSet(t, "beta")
	if _, err := m.Migrate(ctxT(t), []db.Set{alpha, beta}); err != nil {
		t.Fatalf("first: %v", err)
	}
	applied, err := m.Migrate(ctxT(t), []db.Set{alpha})
	if err != nil || len(applied) != 0 {
		t.Fatalf("Migrate without beta: %+v, %v", applied, err)
	}
	if !hasTable(t, p, "beta.thing") {
		t.Errorf("beta's table was touched")
	}
	rows := migRows(t, p)
	n := 0
	for _, r := range rows {
		if r.Set == "beta" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("beta rows = %d, want 1", n)
	}
}

func TestStatus_ReportsBuiltAppliedAndProblem(t *testing.T) {
	p, _ := tempDB(t)
	m := migrator(t, p)
	alpha := fixtureSet(t, "alpha")
	find := func(st []db.Status, set string) (db.Status, bool) {
		for _, s := range st {
			if s.Set == set {
				return s, true
			}
		}
		return db.Status{}, false
	}

	st, err := m.Status(ctxT(t), []db.Set{alpha})
	if err != nil {
		t.Fatalf("Status on an empty database: %v", err)
	}
	if hasTable(t, p, "core.schema_migration") {
		t.Errorf("Status must not change the database")
	}
	s, ok := find(st, "alpha")
	if !ok {
		t.Fatalf("no row for alpha in %+v", st)
	}
	if s.Built != 2 || s.Applied != 0 {
		t.Errorf("alpha = %+v, want Built 2 Applied 0", s)
	}
	wantKind(t, s.Err, errs.Conflict)
	if b, ok := find(st, "db"); !ok || b.Built != 1 || b.Applied != 0 {
		t.Errorf("bootstrap row = %+v, %v", b, ok)
	}

	if _, err := m.Migrate(ctxT(t), []db.Set{alpha}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	st, err = m.Status(ctxT(t), []db.Set{alpha})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	for _, s := range st {
		if s.Built != s.Applied || s.Err != nil || s.Built < 1 {
			t.Errorf("after migrate: %+v", s)
		}
	}
}

func TestStatus_InvalidSet_ReturnsInvalid(t *testing.T) {
	p, _ := tempDB(t)
	_, err := migrator(t, p).Status(ctxT(t), []db.Set{{Name: "A", Schema: "x"}})
	wantKind(t, err, errs.Invalid)
}

// F44
func TestStatus_Unreachable_ReturnsUnavailable(t *testing.T) {
	_, name := tempDB(t)
	px := newProxy(t)
	o := optionsFor(t, name)
	o.URL = secret.New(px.URL(t, name))
	p := openWith(t, o, nil)
	m := migrator(t, p)
	px.Kill()
	_, err := m.Status(ctxT(t), []db.Set{fixtureSet(t, "alpha")})
	wantKind(t, err, errs.Unavailable)
}

func TestStatus_ContextCanceled_ReturnsCanceled(t *testing.T) {
	p, _ := tempDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := migrator(t, p).Status(ctx, nil)
	wantKind(t, err, errs.Canceled)
}

func TestCheckSchema_CurrentSet_ReturnsNil(t *testing.T) {
	p, _ := tempDB(t)
	m := migrator(t, p)
	alpha := fixtureSet(t, "alpha")
	if _, err := m.Migrate(ctxT(t), []db.Set{alpha}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := m.CheckSchema(ctxT(t), "alpha", alpha.Files); err != nil {
		t.Fatalf("CheckSchema: %v", err)
	}
}

// F45
func TestCheckSchema_EmptyDatabase_ReturnsConflict(t *testing.T) {
	p, _ := tempDB(t)
	err := migrator(t, p).CheckSchema(ctxT(t), "alpha", fixtureSet(t, "alpha").Files)
	wantKind(t, err, errs.Conflict) // not Internal (42P01), module-contract Proposed change 4
	if hasTable(t, p, "core.schema_migration") {
		t.Errorf("CheckSchema must never write")
	}
}

// F45
func TestCheckSchema_NoRowForModule_ReturnsConflict(t *testing.T) {
	p, _ := tempDB(t)
	m := migrator(t, p)
	if _, err := m.Migrate(ctxT(t), []db.Set{fixtureSet(t, "beta")}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	wantKind(t, m.CheckSchema(ctxT(t), "alpha", fixtureSet(t, "alpha").Files), errs.Conflict)
}

// F46
func TestCheckSchema_RecordedLower_ReturnsConflictSayingMigrate(t *testing.T) {
	p, _ := tempDB(t)
	m := migrator(t, p)
	v1 := gamma(nil)
	if _, err := m.Migrate(ctxT(t), []db.Set{v1}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	v2 := gamma(map[string]string{"0002_more.sql": "ALTER TABLE gamma.t ADD COLUMN c text;\n"})
	err := m.CheckSchema(ctxT(t), "gamma", v2.Files)
	wantKind(t, err, errs.Conflict)
	// The spec fixes the operator's instructions in the message.
	if !strings.Contains(err.Error(), "bms migrate") || !strings.Contains(err.Error(), "gamma") {
		t.Errorf("message must name the module and say to run bms migrate")
	}
}

// F39
func TestCheckSchema_RecordedHigher_ReturnsConflict(t *testing.T) {
	p, _ := tempDB(t)
	m := migrator(t, p)
	v2 := gamma(map[string]string{"0002_more.sql": "ALTER TABLE gamma.t ADD COLUMN c text;\n"})
	if _, err := m.Migrate(ctxT(t), []db.Set{v2}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	err := m.CheckSchema(ctxT(t), "gamma", gamma(nil).Files)
	wantKind(t, err, errs.Conflict)
	if !strings.Contains(strings.ToLower(err.Error()), "upgrade") {
		t.Errorf("message must say to upgrade the binary")
	}
}

// F38
func TestCheckSchema_ChangedAppliedFile_ReturnsConflict(t *testing.T) {
	p, _ := tempDB(t)
	m := migrator(t, p)
	if _, err := m.Migrate(ctxT(t), []db.Set{gamma(nil)}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Run("changed bytes", func(t *testing.T) {
		edited := mset("gamma", map[string]string{"0001_create_t.sql": createGamma + "-- edited\n"})
		wantKind(t, m.CheckSchema(ctxT(t), "gamma", edited.Files), errs.Conflict)
	})
	t.Run("CRLF edit is a change", func(t *testing.T) {
		edited := mset("gamma", map[string]string{"0001_create_t.sql": strings.ReplaceAll(createGamma, "\n", "\r\n")})
		wantKind(t, m.CheckSchema(ctxT(t), "gamma", edited.Files), errs.Conflict)
	})
	t.Run("renamed file", func(t *testing.T) {
		renamed := mset("gamma", map[string]string{"0001_make_t.sql": createGamma})
		wantKind(t, m.CheckSchema(ctxT(t), "gamma", renamed.Files), errs.Conflict)
	})
}

// F48
func TestCheckSchema_MalformedMigrations_ReturnsInvalid(t *testing.T) {
	p, _ := tempDB(t)
	m := migrator(t, p)
	for name, files := range map[string]fs.FS{
		"gap":      mapFS("0001_a.sql", "0003_c.sql"),
		"bad name": mapFS("0001_a.sql", "README"),
		"empty":    mapFS(),
		"nil":      nil,
	} {
		t.Run(name, func(t *testing.T) {
			wantKind(t, m.CheckSchema(ctxT(t), "gamma", files), errs.Invalid)
		})
	}
}

// F50
func TestCheckSchema_BadModuleName_ReturnsInvalid(t *testing.T) {
	p, _ := tempDB(t)
	m := migrator(t, p)
	for _, name := range []string{"", "A", "a", "a-b", "1ab", `a"b`, strings.Repeat("a", 33)} {
		t.Run("name "+name, func(t *testing.T) {
			wantKind(t, m.CheckSchema(ctxT(t), name, mapFS("0001_a.sql")), errs.Invalid)
		})
	}
}

// F49
func TestCheckSchema_Unreachable_ReturnsUnavailable(t *testing.T) {
	_, name := tempDB(t)
	px := newProxy(t)
	o := optionsFor(t, name)
	o.URL = secret.New(px.URL(t, name))
	p := openWith(t, o, nil)
	m := migrator(t, p)
	px.Kill()
	wantKind(t, m.CheckSchema(ctxT(t), "alpha", fixtureSet(t, "alpha").Files), errs.Unavailable)
}

func TestCheckSchema_ContextCanceled_ReturnsCanceled(t *testing.T) {
	p, _ := tempDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	wantKind(t, migrator(t, p).CheckSchema(ctx, "alpha", fixtureSet(t, "alpha").Files), errs.Canceled)
}

// F51
func TestNewMigrator_NilLogger_ReturnsInvalid(t *testing.T) {
	p, _ := tempDB(t)
	m, err := db.NewMigrator(p, nil)
	if m != nil {
		t.Errorf("migrator must be nil on error")
	}
	wantKind(t, err, errs.Invalid)
}

// F41
func TestMigrate_DuplicateSetName_ReturnsInvalid(t *testing.T) {
	p, _ := tempDB(t)
	m := migrator(t, p)
	a1, a2 := fixtureSet(t, "alpha"), fixtureSet(t, "alpha")
	applied, err := m.Migrate(ctxT(t), []db.Set{a1, a2})
	wantKind(t, err, errs.Invalid)
	if len(applied) != 0 || hasTable(t, p, "core.schema_migration") {
		t.Errorf("nothing may be applied when validation fails")
	}
}

// F32, F33-F35 through Migrate: everything is validated before anything is applied.
func TestMigrate_InvalidSet_AppliesNothing(t *testing.T) {
	p, _ := tempDB(t)
	m := migrator(t, p)
	good := fixtureSet(t, "alpha")
	for name, bad := range map[string]db.Set{
		"bad name":     {Name: "A", Schema: "core", Files: mapFS("0001_a.sql")},
		"wrong schema": {Name: "zeta", Schema: "alpha", Files: mapFS("0001_a.sql")},
		"nil files":    {Name: "zeta", Schema: "zeta"},
		"gap in files": {Name: "zeta", Schema: "zeta", Files: mapFS("0001_a.sql", "0003_c.sql")},
		"stray readme": {Name: "zeta", Schema: "zeta", Files: mapFS("0001_a.sql", "README")},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := m.Migrate(ctxT(t), []db.Set{good, bad})
			wantKind(t, err, errs.Invalid)
			if hasTable(t, p, "alpha.item") || hasTable(t, p, "core.schema_migration") {
				t.Errorf("a good set was applied despite a bad one")
			}
		})
	}
}

// F36
func TestMigrate_ReadOnly_RefusesBeforeApplying(t *testing.T) {
	admin, name := tempDB(t)
	p := readOnlyPool(t, admin, name)
	applied, err := migrator(t, p).Migrate(ctxT(t), []db.Set{fixtureSet(t, "alpha")})
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Errorf("error does not wrap db.ErrReadOnly")
	}
	if len(applied) != 0 {
		t.Errorf("applied = %+v", applied)
	}
	if hasTable(t, admin, "core.schema_migration") {
		t.Errorf("a read-only database was changed")
	}
}

// waitAdvisoryWaiter yields until some session of this database waits on an advisory lock.
func waitAdvisoryWaiter(t testing.TB, q db.Querier) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		if count(t, q, "SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock' AND wait_event = 'advisory'") > 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("no session is waiting on the advisory lock")
		default:
			runtime.Gosched()
		}
	}
}

// F37
func TestMigrate_Concurrent_SecondReturnsConflict(t *testing.T) {
	p, _ := tempDB(t)
	slow := mset("slow", map[string]string{
		"0001_wait.sql": "SELECT pg_advisory_xact_lock(7424242); CREATE TABLE slow.t (id integer);\n",
	})
	first := migrator(t, p)
	done := make(chan error, 1)
	// The test holds the advisory lock the first run's file waits for, for as long as the
	// assertions below take.
	hold := make(chan struct{})
	holding := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- p.Transact(context.Background(), db.Write, func(tx context.Context) error {
			if _, err := p.Exec(tx, "SELECT pg_advisory_xact_lock(7424242)"); err != nil {
				return err //nolint:wrapcheck // test closure
			}
			close(holding)
			<-hold
			return nil
		})
	}()
	<-holding
	go func() {
		_, err := first.Migrate(context.Background(), []db.Set{slow})
		done <- err
	}()
	waitAdvisoryWaiter(t, p)

	for label, m := range map[string]*db.Migrator{"another migrator": migrator(t, p), "same migrator": first} {
		applied, err := m.Migrate(ctxT(t), []db.Set{fixtureSet(t, "alpha")})
		if !errs.Is(err, errs.Conflict) {
			t.Errorf("%s: want Conflict, got %v", label, err)
		}
		if len(applied) != 0 {
			t.Errorf("%s: applied %+v", label, applied)
		}
	}
	close(hold)
	if err := <-holderDone; err != nil {
		t.Fatalf("holder: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("the first run must finish: %v", err)
	}
	if hasTable(t, p, "alpha.item") {
		t.Errorf("the refused run applied alpha")
	}
	// The lock is released: a later run works.
	if _, err := migrator(t, p).Migrate(ctxT(t), []db.Set{fixtureSet(t, "alpha")}); err != nil {
		t.Errorf("Migrate after the first finished: %v", err)
	}
}

// F38
func TestMigrate_ChangedAppliedFile_ReturnsConflict(t *testing.T) {
	p, _ := tempDB(t)
	m := migrator(t, p)
	if _, err := m.Migrate(ctxT(t), []db.Set{gamma(nil)}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	other := fixtureSet(t, "beta")
	edited := mset("gamma", map[string]string{
		"0001_create_t.sql": createGamma + "-- edited\n",
		"0002_new.sql":      "ALTER TABLE gamma.t ADD COLUMN c text;\n",
	})
	applied, err := m.Migrate(ctxT(t), []db.Set{other, edited})
	wantKind(t, err, errs.Conflict)
	if len(applied) != 0 {
		t.Errorf("applied = %+v", applied)
	}
	if hasTable(t, p, "beta.thing") {
		t.Errorf("a problem in one set must stop the whole run before anything is applied")
	}
	if n := count(t, p, "SELECT count(*) FROM core.schema_migration WHERE set_name = 'gamma'"); n != 1 {
		t.Errorf("gamma rows = %d, want 1", n)
	}
	renamed := mset("gamma", map[string]string{"0001_other_name.sql": createGamma})
	_, err = m.Migrate(ctxT(t), []db.Set{renamed})
	wantKind(t, err, errs.Conflict)
}

// F39
func TestMigrate_DatabaseNewerThanBinary_ReturnsConflict(t *testing.T) {
	p, _ := tempDB(t)
	m := migrator(t, p)
	v2 := gamma(map[string]string{"0002_more.sql": "ALTER TABLE gamma.t ADD COLUMN c text;\n"})
	if _, err := m.Migrate(ctxT(t), []db.Set{v2}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	applied, err := m.Migrate(ctxT(t), []db.Set{fixtureSet(t, "beta"), gamma(nil)})
	wantKind(t, err, errs.Conflict)
	if len(applied) != 0 || hasTable(t, p, "beta.thing") {
		t.Errorf("nothing may be applied: %+v", applied)
	}
}

// F40
func TestMigrate_FailingFile_RollsBackThatFileOnly(t *testing.T) {
	tests := []struct {
		name string
		bad  string
		kind errs.Kind
	}{
		{"data exception", "CREATE TABLE zeta.leftover (id integer);\nSELECT 1/0;\n", errs.Invalid},
		{"undefined table", "CREATE TABLE zeta.leftover (id integer);\nINSERT INTO zeta.no_such VALUES (1);\n", errs.Internal},
		{"syntax", "CREATE TABLE zeta.leftover (id integer);\nSELEKT;\n", errs.Internal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := tempDB(t)
			m := migrator(t, p)
			set := mset("zeta", map[string]string{
				"0001_good.sql":  "CREATE TABLE zeta.ok (id integer);\n",
				"0002_bad.sql":   tc.bad,
				"0003_after.sql": "CREATE TABLE zeta.after (id integer);\n",
			})
			applied, err := m.Migrate(ctxT(t), []db.Set{set})
			wantKind(t, err, tc.kind)
			// The files applied so far come back with the error.
			sawGood := false
			for _, a := range applied {
				if a.Set == "zeta" && a.Version == 1 {
					sawGood = true
				}
				if a.Set == "zeta" && a.Version > 1 {
					t.Errorf("failed or later file reported as applied: %+v", a)
				}
			}
			if !sawGood {
				t.Errorf("applied = %+v, want zeta 0001 reported with the error", applied)
			}
			if !strings.Contains(err.Error(), "zeta") || !strings.Contains(err.Error(), "0002") {
				t.Errorf("the error must name the set and the file")
			}
			if !hasTable(t, p, "zeta.ok") {
				t.Errorf("the earlier file must stay applied")
			}
			if hasTable(t, p, "zeta.leftover") || hasTable(t, p, "zeta.after") {
				t.Errorf("the failing file was not rolled back, or a later file ran")
			}
			rows := migRows(t, p)
			var zeta []int
			for _, r := range rows {
				if r.Set == "zeta" {
					zeta = append(zeta, r.Version)
				}
			}
			if len(zeta) != 1 || zeta[0] != 1 {
				t.Errorf("recorded versions = %v, want [1]", zeta)
			}
			// Fixing the file lets the next run continue from there.
			fixed := mset("zeta", map[string]string{
				"0001_good.sql":  "CREATE TABLE zeta.ok (id integer);\n",
				"0002_bad.sql":   "CREATE TABLE zeta.fixed (id integer);\n",
				"0003_after.sql": "CREATE TABLE zeta.after (id integer);\n",
			})
			applied, err = m.Migrate(ctxT(t), []db.Set{fixed})
			if err != nil || len(applied) != 2 {
				t.Errorf("rerun: %+v, %v", applied, err)
			}
		})
	}
}

// F42
func TestMigrate_ContextCanceled_RollsBackCurrentFile(t *testing.T) {
	p, _ := tempDB(t)
	m := migrator(t, p)
	slow := func(sql string) db.Set {
		return mset("slow", map[string]string{
			"0001_first.sql": "CREATE TABLE slow.a (id integer);\n",
			"0002_wait.sql":  sql,
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	_, err := m.Migrate(ctx, []db.Set{slow("CREATE TABLE slow.b (id integer);\nSELECT pg_sleep(30);\n")})
	if !errs.Is(err, errs.Timeout) && !errs.Is(err, errs.Canceled) {
		t.Fatalf("want Timeout or Canceled, got %v", err)
	}
	if !hasTable(t, p, "slow.a") || hasTable(t, p, "slow.b") {
		t.Errorf("first file must stay, the interrupted file must be rolled back")
	}
	if n := count(t, p, "SELECT count(*) FROM core.schema_migration WHERE set_name = 'slow'"); n != 1 {
		t.Errorf("recorded rows = %d, want 1", n)
	}
	// The lock was released.
	applied, err := m.Migrate(ctxT(t), []db.Set{slow("CREATE TABLE slow.b (id integer);\n")})
	if err != nil || len(applied) != 1 {
		t.Errorf("rerun: %+v, %v", applied, err)
	}
}

func TestMigrate_ContextAlreadyCanceled_ReturnsCanceled(t *testing.T) {
	p, _ := tempDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := migrator(t, p).Migrate(ctx, []db.Set{fixtureSet(t, "alpha")})
	wantKind(t, err, errs.Canceled)
	if hasTable(t, p, "alpha.item") {
		t.Errorf("nothing may be applied")
	}
}

func TestMigrate_NoQueryTimeoutApplies(t *testing.T) {
	_, name := tempDB(t)
	o := optionsFor(t, name)
	o.QueryTimeout = 100 * time.Millisecond
	p := openWith(t, o, nil)
	slow := mset("slow", map[string]string{"0001_wait.sql": "SELECT pg_sleep(1); CREATE TABLE slow.t (id integer);\n"})
	if _, err := migrator(t, p).Migrate(ctxT(t), []db.Set{slow}); err != nil {
		t.Fatalf("a migration longer than QueryTimeout must succeed: %v", err)
	}
}

// F43
func TestMigrate_BootstrapDenied_ReturnsKind(t *testing.T) {
	admin, name := tempDB(t)
	role := "bms_t_" + randHex(t)
	pw := "pw" + randHex(t)
	mustExec(t, admin, `CREATE ROLE "`+role+`" LOGIN PASSWORD '`+pw+`' NOSUPERUSER NOCREATEDB NOCREATEROLE`)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, `DROP ROLE IF EXISTS "`+role+`"`); err != nil {
			t.Logf("drop role: %v", err)
		}
	})
	mustExec(t, admin, `REVOKE CREATE ON DATABASE "`+name+`" FROM PUBLIC`)

	u := urlFor(t, name)
	u.User = url.UserPassword(role, pw)
	o := optionsFor(t, name)
	o.URL = secret.New(u.String())
	p := openWith(t, o, nil)
	_, err := migrator(t, p).Migrate(ctxT(t), []db.Set{fixtureSet(t, "alpha")})
	wantKind(t, err, errs.Internal)
	var pe *db.Error
	if !errors.As(err, &pe) || pe.Code != "42501" {
		t.Errorf("*db.Error = %+v, want 42501", pe)
	}
	if strings.Contains(err.Error(), pw) {
		t.Errorf("error leaks the password")
	}
}

// F58 (D51): a migration's own RAISE EXCEPTION text reaches the error and the log; every other
// database error stays scrubbed.
func TestMigrate_RaiseException_PassesMessageThrough(t *testing.T) {
	const msg = "refusing to migrate: 3 rows need a human look"
	const detail = "run the cleanup then retry"
	raise := "CREATE TABLE zeta.leftover (id integer);\n" +
		"DO $$ BEGIN RAISE EXCEPTION '" + msg + "' USING DETAIL = '" + detail + "'; END $$;\n"
	newSet := func(bad string) db.Set {
		return mset("zeta", map[string]string{
			"0001_good.sql": "CREATE TABLE zeta.ok (id integer);\n",
			"0002_bad.sql":  bad,
		})
	}
	logged := func(t *testing.T, p *db.Pool) (*db.Migrator, *bytes.Buffer) {
		t.Helper()
		var buf bytes.Buffer
		m, err := db.NewMigrator(p, slog.New(slog.NewTextHandler(&buf, nil)))
		if err != nil {
			t.Fatalf("NewMigrator: %v", err)
		}
		return m, &buf
	}

	t.Run("P0001 message and detail pass through", func(t *testing.T) {
		p, _ := tempDB(t)
		m, buf := logged(t, p)
		_, err := m.Migrate(ctxT(t), []db.Set{newSet(raise)})
		if err == nil {
			t.Fatal("a raising migration succeeded")
		}
		if !errs.Is(err, errs.Internal) {
			t.Errorf("kind = %v, want Internal (P0001 is unclassified by Wrap)", errs.KindOf(err))
		}
		var dbErr *db.Error
		if !errors.As(err, &dbErr) || dbErr.Code != "P0001" {
			t.Errorf("no *db.Error with code P0001 survives withRaised in %v", err)
		}
		for _, want := range []string{msg, detail, "zeta", "0002"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error lacks %q: %v", want, err)
			}
		}
		if !strings.Contains(buf.String(), msg) {
			t.Errorf("the log lacks the raised message:\n%s", buf.String())
		}
		if hasTable(t, p, "zeta.leftover") {
			t.Error("the raising file was not rolled back")
		}
		if !hasTable(t, p, "zeta.ok") {
			t.Error("the earlier file must stay applied")
		}
	})

	t.Run("a non-P0001 failure carries no message text", func(t *testing.T) {
		const marker = "leaky-marker-7"
		bad := "CREATE TABLE zeta.u (v text UNIQUE);\n" +
			"INSERT INTO zeta.u VALUES ('" + marker + "'), ('" + marker + "');\n"
		p, _ := tempDB(t)
		m, buf := logged(t, p)
		_, err := m.Migrate(ctxT(t), []db.Set{newSet(bad)})
		if err == nil {
			t.Fatal("a failing migration succeeded")
		}
		if strings.Contains(err.Error(), marker) || strings.Contains(buf.String(), marker) {
			t.Errorf("a data value leaked:\n%v\n%s", err, buf.String())
		}
		for _, leak := range []string{"duplicate key", "violates"} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("server message text %q reached the error: %v", leak, err)
			}
		}
	})

	t.Run("division by zero stays scrubbed", func(t *testing.T) {
		p, _ := tempDB(t)
		m, _ := logged(t, p)
		_, err := m.Migrate(ctxT(t), []db.Set{newSet("SELECT 1/0;\n")})
		if err == nil || strings.Contains(err.Error(), "division by zero") {
			t.Errorf("err = %v, want a scrubbed error", err)
		}
	})
}
