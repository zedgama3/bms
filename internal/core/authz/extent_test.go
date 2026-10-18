package authz_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func TestExtent_CoversAndIsZero(t *testing.T) {
	a, b, c := newDept(), newDept(), newDept()
	tests := []struct {
		name   string
		e      authz.Extent
		covers map[model.DepartmentID]bool
		zero   bool
	}{
		{"zero", authz.Extent{}, map[model.DepartmentID]bool{a: false}, true},
		{"site", authz.Extent{Site: true}, map[model.DepartmentID]bool{a: true, b: true}, false},
		{"departments", authz.Extent{Departments: []model.DepartmentID{a, b}}, map[model.DepartmentID]bool{a: true, b: true, c: false}, false},
		{"nil departments not site", authz.Extent{Departments: nil}, map[model.DepartmentID]bool{c: false}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.e.IsZero(); got != tc.zero {
				t.Errorf("IsZero = %v, want %v", got, tc.zero)
			}
			for d, want := range tc.covers {
				if got := tc.e.Covers(d); got != want {
					t.Errorf("Covers(%v) = %v, want %v", d, got, want)
				}
			}
		})
	}
}

func TestExtent_ZeroDepartment_NotCovered(t *testing.T) {
	e := authz.Extent{Departments: []model.DepartmentID{newDept()}}
	if e.Covers(model.DepartmentID{}) {
		t.Error("a department extent covers the zero department")
	}
}

// F34 (the parsing half)
func TestParseCursor_Garbage_ReturnsInvalid(t *testing.T) {
	for _, s := range []string{"!!!", "not base64 url", "bm90LWEtY3Vyc29y", "a"} {
		t.Run(s, func(t *testing.T) {
			_, err := authz.ParseCursor(s)
			wantKind(t, err, errs.Invalid)
		})
	}
}
