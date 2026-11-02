package datarights_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"        //nolint:depguard // failDB must implement db.DB, whose Query returns pgx.Rows
	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // failDB must implement db.DB, whose Exec returns pgconn.CommandTag

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/datarights"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// unit is a linked rig over a dbtest.Fake.
type unit struct {
	*rig
	fake *dbtest.Fake
}

func newUnit(t *testing.T) *unit {
	t.Helper()
	f := dbtest.NewFake()
	r := newRig(t, f)
	r.link()
	return &unit{rig: r, fake: f}
}

type useCase struct {
	name string
	call func(ctx context.Context, s *datarights.Service) error
}

func allUseCases() []useCase {
	other := newPerson()
	rid := model.NewID[datarights.RequestID]()
	return []useCase{
		{"Export", func(ctx context.Context, s *datarights.Service) error {
			_, err := s.Export(ctx, other)
			return keep(err)
		}},
		{"Delete", func(ctx context.Context, s *datarights.Service) error {
			_, err := s.Delete(ctx, other)
			return keep(err)
		}},
		{"RequestDeletion", func(ctx context.Context, s *datarights.Service) error {
			_, err := s.RequestDeletion(ctx, "")
			return keep(err)
		}},
		{"MyRequests", func(ctx context.Context, s *datarights.Service) error { _, err := s.MyRequests(ctx); return keep(err) }},
		{"WithdrawRequest", func(ctx context.Context, s *datarights.Service) error { return s.WithdrawRequest(ctx, rid) }},
		{"ListRequests", func(ctx context.Context, s *datarights.Service) error {
			_, err := s.ListRequests(ctx, datarights.RequestQuery{})
			return keep(err)
		}},
		{"DeclineRequest", func(ctx context.Context, s *datarights.Service) error {
			_, err := s.DeclineRequest(ctx, rid, "")
			return keep(err)
		}},
		{"FulfilRequest", func(ctx context.Context, s *datarights.Service) error {
			_, err := s.FulfilRequest(ctx, rid)
			return keep(err)
		}},
	}
}

// F3
func TestUseCases_NoActor_ReturnsUnauthenticated(t *testing.T) {
	contexts := map[string]func(testing.TB) context.Context{
		"no session":         func(t testing.TB) context.Context { return ctxT(t) },
		"restricted session": func(t testing.TB) context.Context { return asRestricted(ctxT(t), newPerson()) },
		"signal delivery":    func(t testing.TB) context.Context { return inDelivery(t) },
	}
	for cname, mk := range contexts {
		for _, uc := range allUseCases() {
			t.Run(cname+"/"+uc.name, func(t *testing.T) {
				u := newUnit(t)
				err := uc.call(mk(t), u.svc)
				wantKind(t, err, errs.Unauthenticated)
				if n := len(u.fake.Calls()); n != 0 {
					t.Errorf("%d statements before refusing an unauthenticated call", n)
				}
				if len(u.rec.Recorded()) != 0 {
					t.Error("an audit entry for an unauthenticated call")
				}
				if got := u.log.all(); len(got) != 0 {
					t.Errorf("sources called before refusing: %v", got)
				}
			})
		}
	}
}

// F29, F32, F44: with no grants every protected call is refused, for an unknown ID as for a
// known one, so a 403 never confirms that an ID exists.
func TestUseCases_NoGrants_ReturnsPermission(t *testing.T) {
	protected := map[string]bool{"Export": true, "Delete": true, "ListRequests": true, "DeclineRequest": true, "FulfilRequest": true}
	for _, uc := range allUseCases() {
		if !protected[uc.name] {
			continue
		}
		t.Run(uc.name+"_NoPermission_ReturnsPermission", func(t *testing.T) {
			u := newUnit(t)
			err := uc.call(asPerson(ctxT(t), newPerson()), u.svc)
			wantKind(t, err, errs.Permission)
			for _, c := range u.fake.Calls() {
				t.Errorf("statement before the permission check: %.40s", c.SQL)
			}
			if len(u.rec.Recorded()) != 0 {
				t.Errorf("entries %v for a refused call", actionsOf(u.rec.Recorded()))
			}
			if got := u.log.all(); len(got) != 0 {
				t.Errorf("sources called before the permission check: %v", got)
			}
		})
	}
}

// Each permission opens only its own doors.
func TestUseCases_WrongPermission_ReturnsPermission(t *testing.T) {
	tests := []struct {
		name  string
		grant func(r *rig, p model.PersonID)
		use   string
	}{
		{"export cannot delete", func(r *rig, p model.PersonID) { r.az.Grant(p, permExport) }, "Delete"},
		{"export cannot list requests", func(r *rig, p model.PersonID) { r.az.Grant(p, permExport) }, "ListRequests"},
		{"export cannot fulfil", func(r *rig, p model.PersonID) { r.az.Grant(p, permExport) }, "FulfilRequest"},
		{"export cannot decline", func(r *rig, p model.PersonID) { r.az.Grant(p, permExport) }, "DeclineRequest"},
		{"delete cannot export", func(r *rig, p model.PersonID) { r.az.Grant(p, permDelete) }, "Export"},
		{"manage cannot export", func(r *rig, p model.PersonID) { r.az.Grant(p, permManage) }, "Export"},
		{"manage cannot delete", func(r *rig, p model.PersonID) { r.az.Grant(p, permManage) }, "Delete"},
		{"directory cannot export", func(r *rig, p model.PersonID) { r.az.Grant(p, permDirectory) }, "Export"},
		{"contacts cannot delete", func(r *rig, p model.PersonID) { r.az.Grant(p, permContacts) }, "Delete"},
		// The data permissions are site permissions: a department grant does not count.
		{"department export is not enough", func(r *rig, p model.PersonID) { r.az.GrantIn(p, permExport, model.NewID[model.DepartmentID]()) }, "Export"},
		{"department delete is not enough", func(r *rig, p model.PersonID) { r.az.GrantIn(p, permDelete, model.NewID[model.DepartmentID]()) }, "Delete"},
		{"department delete cannot list requests", func(r *rig, p model.PersonID) { r.az.GrantIn(p, permDelete, model.NewID[model.DepartmentID]()) }, "ListRequests"},
	}
	byName := map[string]useCase{}
	for _, uc := range allUseCases() {
		byName[uc.name] = uc
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			me := newPerson()
			tc.grant(u.rig, me)
			wantKind(t, byName[tc.use].call(asPerson(ctxT(t), me), u.svc), errs.Permission)
		})
	}
}

// F50
func TestCommands_BadID_ReturnsInvalid(t *testing.T) {
	for _, bad := range []string{"", "not-a-uuid", "12345", "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz"} {
		t.Run("delete/"+bad, func(t *testing.T) {
			u := newUnit(t)
			wantKind(t, u.svc.DeleteCommand(ctxT(t), io.Discard, bad), errs.Invalid)
		})
		t.Run("export/"+bad, func(t *testing.T) {
			u := newUnit(t)
			wantKind(t, u.svc.ExportCommand(ctxT(t), io.Discard, bad), errs.Invalid)
		})
	}
}

// F31: before Link every export and deletion fails Internal and calls nothing.
func TestCommands_BeforeLink_ReturnInternal_NoSourceCalled(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	id := newPerson().String()
	wantKind(t, r.svc.DeleteCommand(ctxT(t), io.Discard, id), errs.Internal)
	wantKind(t, r.svc.ExportCommand(ctxT(t), io.Discard, id), errs.Internal)
	if got := r.log.all(); len(got) != 0 {
		t.Errorf("sources called by an unlinked service: %v", got)
	}
}

// F38 and the writes of the read-only family.
func TestWrites_ReadOnly_ReturnUnavailable(t *testing.T) {
	me := newPerson()
	writes := []struct {
		name  string
		grant []authz.Permission
		call  func(ctx context.Context, s *datarights.Service) error
	}{
		{"RequestDeletion", nil, func(ctx context.Context, s *datarights.Service) error {
			_, err := s.RequestDeletion(ctx, "")
			return keep(err)
		}},
		{"Delete", []authz.Permission{permDelete}, func(ctx context.Context, s *datarights.Service) error {
			_, err := s.Delete(ctx, newPerson())
			return keep(err)
		}},
	}
	for _, tc := range writes {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			u.fake.SetReadOnly(true)
			for _, p := range tc.grant {
				u.az.Grant(me, p)
			}
			wantReadOnly(t, tc.call(asPerson(ctxT(t), me), u.svc))
			if len(u.rec.Recorded()) != 0 {
				t.Errorf("entries %v on a read-only copy", actionsOf(u.rec.Recorded()))
			}
			if len(u.bus.Published()) != 0 {
				t.Error("a signal was published on a read-only copy")
			}
			if got := u.log.all(); len(got) != 0 {
				t.Errorf("sources called on a read-only copy: %v", got)
			}
		})
	}
}

// ---- F52: a store that fails -----------------------------------------------------------

type failDB struct{ err error }

func (f failDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, f.err
}
func (f failDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, f.err }
func (f failDB) QueryRow(context.Context, string, ...any) pgx.Row        { return failRow(f) }
func (f failDB) Transact(context.Context, db.Mode, func(context.Context) error) error {
	return f.err
}
func (failDB) ReadOnly() bool { return false }

type failRow struct{ err error }

func (r failRow) Scan(...any) error { return r.err }

// F52
func TestStore_Unavailable_Propagates(t *testing.T) {
	kinds := []errs.Kind{errs.Unavailable, errs.Timeout, errs.Canceled}
	me := newPerson()
	for _, kind := range kinds {
		for _, uc := range allUseCases() {
			if uc.name == "Export" { // reads the person through the PeopleSource first; the spec fixes no order
				continue
			}
			t.Run(kind.String()+"/"+uc.name, func(t *testing.T) {
				r := newRig(t, failDB{err: errs.E("test.failDB", kind, errors.New("down"))})
				r.link()
				r.az.Grant(me, permExport)
				r.az.Grant(me, permDelete)
				err := uc.call(asPerson(ctxT(t), me), r.svc)
				wantKind(t, err, kind)
			})
		}
	}
}
