//go:build integration

package db_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
)

// D52(b): a RAISE from a trigger the migration fires is scrubbed like any server error; only a
// RAISE in the migration's own DO block passes through.
func TestMigrate_TriggerRaise_IsScrubbed(t *testing.T) {
	const marker = "trigger-secret-text-91"
	const detail = "trigger-detail-text-92"
	bad := "CREATE TABLE zeta.t (id integer);\n" +
		"CREATE FUNCTION zeta.refuse() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN " +
		"RAISE EXCEPTION '" + marker + "' USING DETAIL = '" + detail + "'; END $$;\n" +
		"CREATE TRIGGER refuse BEFORE INSERT ON zeta.t FOR EACH ROW EXECUTE FUNCTION zeta.refuse();\n" +
		"INSERT INTO zeta.t VALUES (1);\n"
	set := mset("zeta", map[string]string{
		"0001_good.sql": "CREATE TABLE zeta.ok (id integer);\n",
		"0002_bad.sql":  bad,
	})
	p, _ := tempDB(t)
	var buf bytes.Buffer
	m, err := db.NewMigrator(p, slog.New(slog.NewTextHandler(&buf, nil)))
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	_, err = m.Migrate(ctxT(t), []db.Set{set})
	if err == nil {
		t.Fatal("a migration whose trigger raises succeeded")
	}
	for _, leak := range []string{marker, detail} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("trigger text %q reached the error: %v", leak, err)
		}
		if strings.Contains(buf.String(), leak) {
			t.Errorf("trigger text %q reached the log:\n%s", leak, buf.String())
		}
	}
	if hasTable(t, p, "zeta.t") {
		t.Error("the failing file was not rolled back")
	}
	if !hasTable(t, p, "zeta.ok") {
		t.Error("the earlier file must stay applied")
	}
}

// D52(b), the other side: a DO block that itself raises, inside a larger file, still passes through.
func TestMigrate_OwnDoBlockRaise_AfterOtherStatements_PassesThrough(t *testing.T) {
	const msg = "own-block-message-55"
	const detail = "own-block-detail-56"
	bad := "CREATE TABLE zeta.t (id integer);\n" +
		"INSERT INTO zeta.t VALUES (1);\n" +
		"DO $$ BEGIN IF (SELECT count(*) FROM zeta.t) > 0 THEN RAISE EXCEPTION '" + msg +
		"' USING DETAIL = '" + detail + "'; END IF; END $$;\n"
	set := mset("zeta", map[string]string{"0001_bad.sql": bad})
	p, _ := tempDB(t)
	m, err := db.NewMigrator(p, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	_, err = m.Migrate(ctxT(t), []db.Set{set})
	if err == nil {
		t.Fatal("a raising migration succeeded")
	}
	for _, want := range []string{msg, detail} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
}
