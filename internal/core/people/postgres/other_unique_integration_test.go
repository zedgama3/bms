//go:build integration

package postgres_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/people/postgres"
	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/model"
)

// F81: a unique violation on an index other than person_playa_name_key_idx stays a plain
// Conflict and never becomes the playa_name "taken" field error. The person writes have no
// other reachable unique index (ON CONFLICT (id) absorbs a repeated id), so the real path is
// the second open data request, which datarights tests now (same name, its own store).
func TestStore_OtherUniqueViolation_PlainConflict(t *testing.T) {
	pool := newPool(t)
	st, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	p := model.PersonID(person(t, pool))
	// A repeated person id is absorbed (ON CONFLICT (id) DO NOTHING): no error, not inserted,
	// and in particular not the playa_name taken conflict.
	row := rows.Person{ID: p, PlayaName: "Dandelion", PlayaNameKey: "dandelion"}
	_, inserted, err := st.InsertPerson(ctxT(t), row, t0)
	if err != nil || inserted {
		t.Errorf("repeated id: inserted=%v err=%v, want false, nil", inserted, err)
	}
}
