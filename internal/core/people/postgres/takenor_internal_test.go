package postgres

import (
	"errors"
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// F81: takenOr re-kinds only a 23505 on person_playa_name_key_idx.
func TestTakenOr_Table(t *testing.T) {
	plain := errs.E("x", errs.Unavailable, errors.New("down"))
	for _, tc := range []struct {
		name      string
		in        error
		wantKind  errs.Kind
		wantTaken bool
	}{
		{"playa name key", &db.Error{Code: "23505", Constraint: playaNameKeyIdx}, errs.Conflict, true},
		{"other unique index", &db.Error{Code: "23505", Constraint: "person_pkey"}, errs.Internal, false},
		{"other unique, no constraint", &db.Error{Code: "23505"}, errs.Internal, false},
		{"playa index, other code", &db.Error{Code: "23514", Constraint: playaNameKeyIdx}, errs.Internal, false},
		{"not a db error", plain, errs.Unavailable, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := takenOr("op", tc.in)
			if got == nil {
				t.Fatal("nil error")
			}
			if tc.wantTaken {
				var fe *validate.FieldError
				if !errors.As(got, &fe) || fe.Field != "playa_name" || fe.Code != "taken" || !errs.Is(got, errs.Conflict) {
					t.Errorf("got %v, want Conflict playa_name/taken", got)
				}
				return
			}
			if !errors.Is(got, tc.in) {
				t.Errorf("the error was changed: %v", got)
			}
			var fe *validate.FieldError
			if errors.As(got, &fe) {
				t.Errorf("unexpected field error %s/%s", fe.Field, fe.Code)
			}
		})
	}
}
