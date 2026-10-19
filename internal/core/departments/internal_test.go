package departments

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// D38 (ae): only department_name_key means "name taken"; a Conflict from any other
// constraint stays a generic conflict with no field.
func TestTakenOr_MapsByConstraintName(t *testing.T) {
	conflict := func(constraint string) error {
		return errs.E("test", errs.Conflict, &db.Error{Code: "23505", Constraint: constraint})
	}
	tests := []struct {
		name      string
		err       error
		kind      errs.Kind
		wantField bool
	}{
		{"name index", conflict(conDepartmentName), errs.Conflict, true},
		{"another unique constraint", conflict("department_pkey"), errs.Conflict, false},
		{"conflict without a constraint", errs.E("test", errs.Conflict, driverError{}), errs.Conflict, false},
		{"not a conflict", errs.E("test", errs.Unavailable, &db.Error{Constraint: conDepartmentName}), errs.Unavailable, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := takenOr("departments.test", tc.err)
			if !errs.Is(got, tc.kind) {
				t.Fatalf("kind = %v, want %v (%v)", errs.KindOf(got), tc.kind, got)
			}
			var named bool
			for _, fe := range validate.Fields(got) {
				if fe.Field == fieldName && fe.Code == codeTaken {
					named = true
				}
			}
			if named != tc.wantField {
				t.Errorf("name/taken field = %v, want %v (%v)", named, tc.wantField, got)
			}
		})
	}
}

type driverError struct{}

func (driverError) Error() string { return "a driver error" }
