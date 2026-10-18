package departmentstest_test

import (
	"context"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/departments/departmentstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

func newDept() model.DepartmentID { return model.NewID[model.DepartmentID]() }

func TestFake_Table(t *testing.T) {
	ctx := context.Background()
	active, archived, unknown := newDept(), newDept(), newDept()
	f := departmentstest.NewFake()
	f.Add(active, "Rangers")
	f.Add(archived, "Gate")
	f.Archive(archived)
	f.Archive(unknown) // ignored

	t.Run("Active", func(t *testing.T) {
		for name, tc := range map[string]struct {
			id   model.DepartmentID
			want bool
		}{"active": {active, true}, "archived": {archived, false}, "unknown": {unknown, false}} {
			got, err := f.Active(ctx, tc.id)
			if err != nil || got != tc.want {
				t.Errorf("%s: Active = %v, %v; want %v", name, got, err, tc.want)
			}
		}
	})
	t.Run("Active zero ID is Invalid", func(t *testing.T) {
		_, err := f.Active(ctx, model.DepartmentID{})
		if !errs.Is(err, errs.Invalid) {
			t.Fatalf("err = %v, want Invalid", err)
		}
		if fe := validate.Fields(err); len(fe) == 0 || fe[0].Field != "id" || fe[0].Code != "required" {
			t.Errorf("fields = %v", fe)
		}
	})
	t.Run("NamesOf includes archived, omits unknown", func(t *testing.T) {
		got, err := f.NamesOf(ctx, []model.DepartmentID{active, archived, unknown})
		if err != nil || len(got) != 2 || got[active] != "Rangers" || got[archived] != "Gate" {
			t.Errorf("NamesOf = %v, %v", got, err)
		}
		if _, ok := got[unknown]; ok {
			t.Error("unknown ID present")
		}
	})
	t.Run("NamesOf nil and empty", func(t *testing.T) {
		for _, ids := range [][]model.DepartmentID{nil, {}} {
			if got, err := f.NamesOf(ctx, ids); err != nil || len(got) != 0 {
				t.Errorf("NamesOf = %v, %v", got, err)
			}
		}
	})
	t.Run("NamesOf too many", func(t *testing.T) {
		ids := make([]model.DepartmentID, departments.MaxNames+1)
		for i := range ids {
			ids[i] = newDept()
		}
		_, err := f.NamesOf(ctx, ids)
		if !errs.Is(err, errs.Invalid) {
			t.Fatalf("err = %v, want Invalid", err)
		}
		if fe := validate.Fields(err); len(fe) == 0 || fe[0].Field != "ids" || fe[0].Code != "too_many" || fe[0].Params["max"] != "200" {
			t.Errorf("fields = %v", fe)
		}
		if _, err := f.NamesOf(ctx, ids[:departments.MaxNames]); err != nil {
			t.Errorf("exactly MaxNames: %v", err)
		}
	})
	t.Run("Add restores and renames", func(t *testing.T) {
		f.Add(archived, "Gate 2")
		ok, err := f.Active(ctx, archived)
		if err != nil || !ok {
			t.Errorf("Add did not restore: %v, %v", ok, err)
		}
		got, err := f.NamesOf(ctx, []model.DepartmentID{archived})
		if err != nil || got[archived] != "Gate 2" {
			t.Errorf("NamesOf = %v, %v", got, err)
		}
	})
	t.Run("no actor needed", func(t *testing.T) {
		if _, err := f.NamesOf(context.Background(), nil); err != nil {
			t.Error(err)
		}
	})
}

func TestFake_ConcurrentUse(t *testing.T) {
	f := departmentstest.NewFake()
	id := newDept()
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.Add(id, "Rangers")
			f.Archive(id)
			if _, err := f.Active(t.Context(), id); err != nil {
				t.Error(err)
			}
			if _, err := f.NamesOf(t.Context(), []model.DepartmentID{id}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
