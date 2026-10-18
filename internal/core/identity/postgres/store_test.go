package postgres_test

import (
	"io/fs"
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity/postgres"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestNewStore_Nil_Invalid(t *testing.T) {
	s, err := postgres.NewStore(nil)
	if !errs.Is(err, errs.Invalid) || s != nil {
		t.Fatalf("NewStore(nil) = %v, %v", s, err)
	}
}

func TestNewStore_Valid(t *testing.T) {
	s, err := postgres.NewStore(dbtest.NewFake())
	if err != nil || s == nil {
		t.Fatalf("NewStore = %v, %v", s, err)
	}
}

func TestMigrations_VersionedAndForwardOnly(t *testing.T) {
	v, err := db.Version(postgres.Migrations)
	if err != nil || v < 1 {
		t.Fatalf("Version = %d, %v", v, err)
	}
	if _, err := fs.Stat(postgres.Migrations, "0001_init.sql"); err != nil {
		t.Errorf("0001_init.sql: %v", err)
	}
}
