//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

var dave = model.PersonID{0x73, 0x0e, 0xa4, 0x18, 0x52, 0xbb, 0x4d, 0x6a, 0x9f, 0x01, 0xe8, 0x3c, 0x6d, 0x20, 0xf5, 0x97}

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func migrated(t *testing.T) (*db.Pool, *postgres.Store) {
	t.Helper()
	pool := dbtest.TempDB(t)
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	sets := []db.Set{{Name: "hello", Schema: "hello", Files: postgres.Migrations}}
	if _, err := mig.Migrate(ctxT(t), sets); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return pool, s
}

func TestMigration0001_AppliesOnEmptyDatabase_StatusReportsVersion1(t *testing.T) {
	pool := dbtest.TempDB(t)
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	sets := []db.Set{{Name: "hello", Schema: "hello", Files: postgres.Migrations}}
	applied, err := mig.Migrate(ctxT(t), sets)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	found := false
	for _, a := range applied {
		if a.Set == "hello" && a.Version == 1 {
			found = true
		}
	}
	if !found {
		t.Errorf("Applied = %+v, want hello version 1", applied)
	}
	st, err := mig.Status(ctxT(t), sets)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	for _, s := range st {
		if s.Set == "hello" && (s.Built != 1 || s.Applied != 1 || s.Err != nil) {
			t.Errorf("Status = %+v, want built 1 applied 1 no error", s)
		}
	}
	again, err := mig.Migrate(ctxT(t), sets)
	if err != nil || len(again) != 0 {
		t.Errorf("second Migrate = %+v, %v; want nothing pending", again, err)
	}
}

func TestStore_RoundTrip(t *testing.T) {
	_, s := migrated(t)
	ctx := ctxT(t)
	base := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	older := row(carol, "first")
	older.CreatedAt = base
	newer := row(carol, "second")
	newer.CreatedAt = base.Add(time.Minute)
	other := row(dave, "other")
	for _, g := range []greeting.Greeting{newer, older, other} { // insert out of order
		if err := s.Insert(ctx, g); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}
	n, err := s.CountByPerson(ctx, carol)
	if err != nil || n != 2 {
		t.Fatalf("CountByPerson = %d, %v; want 2", n, err)
	}
	got, err := s.ListByPerson(ctx, carol)
	if err != nil {
		t.Fatalf("ListByPerson: %v", err)
	}
	if len(got) != 2 || got[0].ID != older.ID || got[1].ID != newer.ID {
		t.Fatalf("ListByPerson = %+v, want oldest first", got)
	}
	if got[0].Person != carol || got[0].Text != "first" || !got[0].CreatedAt.Equal(base) {
		t.Errorf("row did not round-trip: %+v", got[0])
	}
	if err := got[0].Validate(); err != nil {
		t.Errorf("a stored row is invalid: %v", err)
	}
	// unknown person
	if n, err := s.CountByPerson(ctx, model.NewID[model.PersonID]()); err != nil || n != 0 {
		t.Errorf("CountByPerson(unknown) = %d, %v; want 0, nil", n, err)
	}
	list, err := s.ListByPerson(ctx, model.NewID[model.PersonID]())
	if err != nil || list == nil || len(list) != 0 {
		t.Errorf("ListByPerson(unknown) = %v, %v; want empty non-nil", list, err)
	}
	// delete only carol's
	del, err := s.DeleteByPerson(ctx, carol)
	if err != nil || del != 2 {
		t.Fatalf("DeleteByPerson = %d, %v; want 2", del, err)
	}
	del, err = s.DeleteByPerson(ctx, carol)
	if err != nil || del != 0 {
		t.Errorf("second DeleteByPerson = %d, %v; want 0", del, err)
	}
	if n, err := s.CountByPerson(ctx, dave); err != nil || n != 1 {
		t.Errorf("dave's rows = %d, want 1 untouched", n)
	}
}

// F27
func TestInsert_DuplicateID_ReturnsConflict(t *testing.T) {
	_, s := migrated(t)
	g := row(carol, "hi")
	if err := s.Insert(ctxT(t), g); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := s.Insert(ctxT(t), g); !errs.Is(err, errs.Conflict) {
		t.Fatalf("duplicate Insert err = %v, want Conflict", err)
	}
}

// F28
func TestInsert_EmptyGreeting_ReturnsInvalid(t *testing.T) {
	_, s := migrated(t)
	cases := []struct {
		name string
		mut  func(*greeting.Greeting)
	}{
		{"empty text", func(g *greeting.Greeting) { g.Text = "" }},
		{"over-long text", func(g *greeting.Greeting) { g.Text = strings.Repeat("a", 65) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := row(carol, "x")
			tc.mut(&g)
			if err := s.Insert(ctxT(t), g); !errs.Is(err, errs.Invalid) {
				t.Fatalf("Insert err = %v, want Invalid", err)
			}
		})
	}
}

func TestInsert_MaxBytesText_Succeeds(t *testing.T) {
	_, s := migrated(t)
	if err := s.Insert(ctxT(t), row(carol, strings.Repeat("a", 64))); err != nil {
		t.Fatalf("Insert of a 64-byte word: %v", err)
	}
}

// F29
func TestStore_ClosedPool_ReturnsUnavailable(t *testing.T) {
	pool, s := migrated(t)
	if err := pool.Close(ctxT(t)); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ctx := ctxT(t)
	if err := s.Insert(ctx, row(carol, "hi")); !errs.Is(err, errs.Unavailable) {
		t.Errorf("Insert err = %v, want Unavailable", err)
	}
	if _, err := s.CountByPerson(ctx, carol); !errs.Is(err, errs.Unavailable) {
		t.Errorf("CountByPerson err = %v, want Unavailable", err)
	}
	if _, err := s.ListByPerson(ctx, carol); !errs.Is(err, errs.Unavailable) {
		t.Errorf("ListByPerson err = %v, want Unavailable", err)
	}
	if _, err := s.DeleteByPerson(ctx, carol); !errs.Is(err, errs.Unavailable) {
		t.Errorf("DeleteByPerson err = %v, want Unavailable", err)
	}
}

func TestStore_CancelledContext_ReturnsCanceled(t *testing.T) {
	_, s := migrated(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Insert(ctx, row(carol, "hi")); !errs.Is(err, errs.Canceled) {
		t.Errorf("Insert err = %v, want Canceled", err)
	}
}

func TestStore_InsertRolledBackWithItsTransaction(t *testing.T) {
	pool, s := migrated(t)
	boom := errors.New("abort")
	err := pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if err := s.Insert(ctx, row(carol, "hi")); err != nil {
			return fmt.Errorf("insert: %w", err)
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Transact err = %v", err)
	}
	if n, err := s.CountByPerson(ctxT(t), carol); err != nil || n != 0 {
		t.Errorf("CountByPerson after rollback = %d, %v; want 0", n, err)
	}
}
