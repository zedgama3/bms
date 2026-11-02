//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/datarights/postgres"
	"github.com/zedgama3/bms/internal/core/datarights/rows"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

var t0 = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// F81 (moved from people's store test): a unique violation on the open-request index stays a
// plain Conflict and never becomes a "taken" field error. The second open data request hits
// data_request_open_idx through datarights' store.
func TestStore_OtherUniqueViolation_PlainConflict(t *testing.T) {
	pool := dbtest.TempDB(t)
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mig.Migrate(ctxT(t), []db.Set{identity.Set, people.Set}); err != nil {
		t.Fatal(err)
	}
	st, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	p := model.NewID[model.PersonID]()
	if _, err := pool.Exec(ctxT(t), `INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, $2, $2)`, [16]byte(p), t0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertRequest(ctxT(t), model.NewID[rows.RequestID](), p, "first", t0); err != nil {
		t.Fatalf("first open request: %v", err)
	}
	_, err = st.InsertRequest(ctxT(t), model.NewID[rows.RequestID](), p, "second", t0)
	if !errs.Is(err, errs.Conflict) {
		t.Fatalf("second open request: kind %v (%v), want Conflict", errs.KindOf(err), err)
	}
	var fe *validate.FieldError
	if errors.As(err, &fe) && (fe.Field == "playa_name" || fe.Code == "taken") {
		t.Errorf("an unrelated unique violation was reported as %s/%s", fe.Field, fe.Code)
	}
}
