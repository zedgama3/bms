//go:build integration

package postgres_test

import (
	"errors"
	"testing"

	"github.com/zedgama3/bms/internal/core/people/postgres"
	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// F81: a unique violation on an index other than person_playa_name_key_idx stays a plain
// Conflict and never becomes the playa_name "taken" field error. The person writes have no
// other reachable unique index (ON CONFLICT (id) absorbs a repeated id), so the real path is
// the second open data request, which hits data_request_open_idx through the same store.
func TestStore_OtherUniqueViolation_PlainConflict(t *testing.T) {
	pool := newPool(t)
	st, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	p := model.PersonID(person(t, pool))
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

	// A repeated person id is absorbed (ON CONFLICT (id) DO NOTHING): no error, not inserted,
	// and in particular not the playa_name taken conflict.
	row := rows.Person{ID: p, PlayaName: "Dandelion", PlayaNameKey: "dandelion"}
	_, inserted, err := st.InsertPerson(ctxT(t), row, t0)
	if err != nil || inserted {
		t.Errorf("repeated id: inserted=%v err=%v, want false, nil", inserted, err)
	}
}
