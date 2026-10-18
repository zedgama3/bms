package authz

import (
	"context"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz/rows"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// insertFailStore answers Grant's reads and fails the insert with a foreign key violation
// on the named constraint, the way db.Wrap reports 23503.
type insertFailStore struct {
	fakeStore
	role       rows.Role
	constraint string
}

func (f *insertFailStore) GetRole(context.Context, rows.RoleID) (rows.Role, error) {
	return f.role, nil
}

func (f *insertFailStore) InsertGrant(context.Context, rows.Grant) error {
	return errs.E("test.InsertGrant", errs.Invalid, &db.Error{Code: "23503", Constraint: f.constraint})
}

func TestGrant_ForeignKeyViolation_NamesTheOffendingField(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		wantField  string
	}{
		{"unknown person", "role_grant_person_id_fkey", "person"},
		{"granting actor has no person row", "role_grant_granted_by_fkey", "actor"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &insertFailStore{
				role:       rows.Role{ID: model.NewID[RoleID](), Key: RoleAccessAdmin, Scope: rows.ScopeSite},
				constraint: tc.constraint,
			}
			s := newTestService(t, st)
			ctx := context.WithValue(context.Background(), ctxActor{}, Actor{Person: model.NewID[model.PersonID](), Superuser: true})
			_, err := s.Grant(ctx, GrantInput{Person: model.NewID[model.PersonID](), Role: st.role.ID})
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("Grant = %v, want Invalid", err)
			}
			fields := validate.Fields(err)
			if len(fields) != 1 || fields[0].Field != tc.wantField || fields[0].Code != codeUnknown {
				t.Errorf("fields = %+v, want one %q field with code %q", fields, tc.wantField, codeUnknown)
			}
		})
	}
}
