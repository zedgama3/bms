package authz

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/authz/rows"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// StarterRoleIDs is the demo seed's actor-less lookup: no actor in ctx, no permission, no
// audit entry.
func TestStarterRoleIDs_Table(t *testing.T) {
	access := model.NewID[RoleID]()
	steward := model.NewID[RoleID]()
	made := model.NewID[RoleID]() // a regional's own role: no key
	st := &fakeStore{roles: []rows.Role{
		{ID: access, Key: RoleAccessAdmin},
		{ID: steward, Key: RoleDepartmentSteward},
		{ID: made, Key: ""},
	}}
	tests := []struct {
		name     string
		keys     []string
		wantKind errs.Kind
		wantErr  bool
		want     map[string]RoleID
	}{
		{name: "one key", keys: []string{RoleAccessAdmin}, want: map[string]RoleID{RoleAccessAdmin: access}},
		{name: "two keys", keys: []string{RoleDepartmentSteward, RoleAccessAdmin}, want: map[string]RoleID{RoleAccessAdmin: access, RoleDepartmentSteward: steward}},
		{name: "repeated key", keys: []string{RoleAccessAdmin, RoleAccessAdmin}, want: map[string]RoleID{RoleAccessAdmin: access}},
		{name: "unknown key", keys: []string{RoleAccessAdmin, "no-such-role"}, wantErr: true, wantKind: errs.NotFound},
		{name: "a key that is a role without one", keys: []string{"made"}, wantErr: true, wantKind: errs.NotFound},
		{name: "no keys", keys: nil, wantErr: true, wantKind: errs.Invalid},
		{name: "empty key", keys: []string{""}, wantErr: true, wantKind: errs.Invalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestService(t, st)
			got, err := s.StarterRoleIDs(t.Context(), tc.keys) // no actor in the context
			if tc.wantErr {
				if !errs.Is(err, tc.wantKind) {
					t.Fatalf("err = %v (kind %v), want kind %v", err, errs.KindOf(err), tc.wantKind)
				}
				if got != nil {
					t.Errorf("got %v with an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("StarterRoleIDs: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for k, id := range tc.want {
				if got[k] != id {
					t.Errorf("%s = %v, want %v", k, got[k], id)
				}
			}
		})
	}
}
