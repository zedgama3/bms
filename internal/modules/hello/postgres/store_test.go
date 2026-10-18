package postgres_test

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/hello/greeting"
	"github.com/zedgama3/bms/internal/modules/hello/postgres"
)

var carol = model.PersonID{0x44, 0x1a, 0x9c, 0x03, 0x7e, 0xd1, 0x42, 0x58, 0xa0, 0x6f, 0x13, 0xb7, 0x25, 0xcc, 0x90, 0x31}

func row(person model.PersonID, text string) greeting.Greeting {
	return greeting.Greeting{
		ID:        model.NewID[greeting.ID](),
		Person:    person,
		Text:      text,
		CreatedAt: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
	}
}

// F2
func TestNewStore_NilDB_ReturnsInvalid(t *testing.T) {
	s, err := postgres.NewStore(nil)
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("NewStore(nil) err = %v, want Invalid", err)
	}
	if s != nil {
		t.Errorf("NewStore(nil) returned a store")
	}
}

func TestNewStore_Fake_Succeeds(t *testing.T) {
	if _, err := postgres.NewStore(dbtest.NewFake()); err != nil {
		t.Fatalf("NewStore: %v", err)
	}
}

func TestMigrations_AreRootedAtTheFiles(t *testing.T) {
	entries, err := fs.ReadDir(postgres.Migrations, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 2 || entries[0].Name() != "0001_init.sql" || entries[1].Name() != "0002_person_fk.sql" {
		t.Fatalf("entries = %v, want 0001_init.sql and 0002_person_fk.sql", entries)
	}
	v, err := db.Version(postgres.Migrations)
	if err != nil || v != 2 {
		t.Errorf("db.Version = %d, %v; want 2", v, err)
	}
	sql, err := fs.ReadFile(postgres.Migrations, "0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hello.greeting", "greeting_person_created_idx"} {
		if !strings.Contains(string(sql), want) {
			t.Errorf("0001_init.sql does not mention %s", want)
		}
	}
}

// F24 at the store: a read-only copy refuses the writes with the sentinel.
func TestStore_ReadOnly_WritesReturnUnavailable(t *testing.T) {
	d := dbtest.NewFake()
	d.SetReadOnly(true)
	s, err := postgres.NewStore(d)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	err = s.Insert(ctx, row(carol, "hi"))
	if !errs.Is(err, errs.Unavailable) || !errors.Is(err, db.ErrReadOnly) {
		t.Errorf("Insert err = %v, want Unavailable wrapping db.ErrReadOnly", err)
	}
	_, err = s.DeleteByPerson(ctx, carol)
	if !errs.Is(err, errs.Unavailable) || !errors.Is(err, db.ErrReadOnly) {
		t.Errorf("DeleteByPerson err = %v, want Unavailable wrapping db.ErrReadOnly", err)
	}
}

func TestStore_Insert_OnWritableFake_RunsOneStatement(t *testing.T) {
	d := dbtest.NewFake()
	s, err := postgres.NewStore(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Insert(t.Context(), row(carol, "hi")); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if n := len(d.Calls()); n != 1 {
		t.Errorf("Insert ran %d statements, want 1", n)
	}
}
