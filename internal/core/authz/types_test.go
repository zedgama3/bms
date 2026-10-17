package authz_test

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

func TestPermissionValidate_Table(t *testing.T) {
	long := "a." + strings.Repeat("b", 64)
	tooLong := "a." + strings.Repeat("b", 65)
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"two segments", "volunteers.read", true},
		{"three segments", "volunteers.shifts.manage", true},
		{"kebab and digits", "afterreports.report-2.read", true},
		{"max ident", long, true},
		{"empty", "", false},
		{"one segment", "volunteers", false},
		{"upper case", "Volunteers.read", false},
		{"upper case later", "volunteers.Read", false},
		{"underscore", "volunteers.read_all", false},
		{"empty segment", "volunteers..read", false},
		{"leading dot", ".volunteers.read", false},
		{"trailing dot", "volunteers.read.", false},
		{"leading hyphen", "volunteers.-read", false},
		{"trailing hyphen", "volunteers.read-", false},
		{"double hyphen", "volunteers.re--ad", false},
		{"digit first", "1volunteers.read", false},
		{"too long ident", tooLong, false},
		{"space", "volunteers. read", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := authz.Permission(tc.in).Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate(%q) = %v, want nil", tc.in, err)
			}
			if !tc.ok && !errs.Is(err, errs.Invalid) {
				t.Fatalf("Validate(%q) = %v, want Invalid", tc.in, err)
			}
		})
	}
}

func TestPermissionModule_ReturnsFirstSegment(t *testing.T) {
	tests := map[string]string{
		"volunteers.shifts.manage": "volunteers",
		"core.modules.read":        "core",
		"a.b":                      "a",
	}
	for in, want := range tests {
		if got := authz.Permission(in).Module(); got != want {
			t.Errorf("Module(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScopeString_Table(t *testing.T) {
	tests := []struct {
		s    authz.Scope
		want string
	}{
		{authz.ScopeSite, "site"},
		{authz.ScopeDepartment, "department"},
		{authz.Scope(0), "Scope(0)"},
		{authz.Scope(9), "Scope(9)"},
	}
	for _, tc := range tests {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("Scope(%d).String() = %q, want %q", uint8(tc.s), got, tc.want)
		}
	}
	if authz.ScopeSite == 0 || authz.ScopeDepartment == 0 {
		t.Error("the zero Scope must be invalid")
	}
}

func TestPermissionDeclValidate_Table(t *testing.T) {
	good := authz.PermissionDecl{Name: "alpha.read", Scope: authz.ScopeSite, Reaches: model.SensitivityInternal}
	tests := []struct {
		name   string
		mutate func(*authz.PermissionDecl)
		fields int // minimum number of FieldErrors; 0 means valid
	}{
		{"valid site", func(*authz.PermissionDecl) {}, 0},
		{"valid department sensitive", func(d *authz.PermissionDecl) {
			d.Scope = authz.ScopeDepartment
			d.Reaches = model.SensitivitySensitive
		}, 0},
		{"bad name", func(d *authz.PermissionDecl) { d.Name = "alpha" }, 1},
		{"empty name", func(d *authz.PermissionDecl) { d.Name = "" }, 1},
		{"zero scope", func(d *authz.PermissionDecl) { d.Scope = 0 }, 1},
		{"scope out of range", func(d *authz.PermissionDecl) { d.Scope = 77 }, 1},
		{"reaches unset", func(d *authz.PermissionDecl) { d.Reaches = 0 }, 1},
		{"reaches out of range", func(d *authz.PermissionDecl) { d.Reaches = 99 }, 1},
		{"everything wrong", func(d *authz.PermissionDecl) { *d = authz.PermissionDecl{Name: "X"} }, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := good
			tc.mutate(&d)
			err := d.Validate()
			if tc.fields == 0 {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("Validate() = %v, want Invalid", err)
			}
			if got := len(validate.Fields(err)); got < tc.fields {
				t.Fatalf("Validate() reported %d field errors, want at least %d (every problem at once)", got, tc.fields)
			}
		})
	}
}
