package departments_test

import (
	"context"
	"encoding/json"
	"go/build"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// unit is a rig over a dbtest.Fake: it proves what happens before the store is reached.
type unit struct {
	*rig
	fake *dbtest.Fake
}

func newUnit(t *testing.T) *unit {
	t.Helper()
	f := dbtest.NewFake()
	return &unit{rig: newRig(t, f), fake: f}
}

func (u *unit) noStatements(t *testing.T, what string) {
	t.Helper()
	if n := len(u.fake.Calls()); n != 0 {
		t.Errorf("%d statements for %s", n, what)
	}
	if len(u.rec.Recorded()) != 0 {
		t.Errorf("audit entries %v for %s", actionsOf(u.rec.Recorded()), what)
	}
	if len(u.bus.Published()) != 0 {
		t.Errorf("signals published for %s", what)
	}
}

// ---- New and Options (F1) --------------------------------------------------------------

func TestNew_MissingDependency_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		field string
		mod   func(*departments.Options)
	}{
		{"departments.db", func(o *departments.Options) { o.DB = nil }},
		{"departments.clock", func(o *departments.Options) { o.Clock = nil }},
		{"departments.logger", func(o *departments.Options) { o.Logger = nil }},
		{"departments.tracer", func(o *departments.Options) { o.Tracer = nil }},
		{"departments.meter", func(o *departments.Options) { o.Meter = nil }},
		{"departments.audit", func(o *departments.Options) { o.Audit = nil }},
		{"departments.signals", func(o *departments.Options) { o.Signals = nil }},
		{"departments.authz", func(o *departments.Options) { o.Authz = nil }},
		{"departments.actor", func(o *departments.Options) { o.Actor = nil }},
		{"departments.lookup", func(o *departments.Options) { o.Lookup = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			o, _ := validOptions(t, dbtest.NewFake())
			tc.mod(&o)
			wantField(t, o.Validate(), tc.field, "")
			svc, err := departments.New(o)
			wantField(t, err, tc.field, "")
			if svc != nil {
				t.Error("New returned a service with its error")
			}
		})
	}
}

func TestOptions_Validate_Table(t *testing.T) {
	o, _ := validOptions(t, dbtest.NewFake())
	wantNoErr(t, o.Validate())

	// Every missing field is reported at once.
	all := departments.Options{}
	err := all.Validate()
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{"db", "clock", "logger", "tracer", "meter", "audit", "signals", "authz", "actor", "lookup"} {
		if !hasField(err, "departments."+f, "") {
			t.Errorf("zero Options does not report departments.%s", f)
		}
	}
}

func TestNew_AgainstNoDatabase_Succeeds(t *testing.T) {
	o, _ := validOptions(t, db.None())
	if _, err := departments.New(o); err != nil {
		t.Fatalf("New against db.None(): %v", err)
	}
}

// ---- no actor (F2) ---------------------------------------------------------------------

type useCase struct {
	name string
	call func(ctx context.Context, s *departments.Service) error
}

func allUseCases() []useCase {
	id := newDept()
	return []useCase{
		{"List", func(ctx context.Context, s *departments.Service) error {
			_, err := s.List(ctx, departments.ListQuery{})
			return keep(err)
		}},
		{"Get", func(ctx context.Context, s *departments.Service) error { _, err := s.Get(ctx, id); return keep(err) }},
		{"Create", func(ctx context.Context, s *departments.Service) error {
			_, err := s.Create(ctx, departments.CreateInput{Name: "Rangers"})
			return keep(err)
		}},
		{"Update", func(ctx context.Context, s *departments.Service) error {
			_, err := s.Update(ctx, id, departments.UpdateInput{Description: str("x")})
			return keep(err)
		}},
		{"Archive", func(ctx context.Context, s *departments.Service) error {
			_, err := s.Archive(ctx, id)
			return keep(err)
		}},
		{"Restore", func(ctx context.Context, s *departments.Service) error {
			_, err := s.Restore(ctx, id)
			return keep(err)
		}},
	}
}

// F2 and Behavior: no session, a restricted session and a signal delivery are no actor.
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
				wantKind(t, uc.call(mk(t), u.svc), errs.Unauthenticated)
				u.noStatements(t, "an unauthenticated call")
			})
		}
	}
}

func TestList_NoActor_ReturnsUnauthenticated(t *testing.T) {
	u := newUnit(t)
	_, err := u.svc.List(ctxT(t), departments.ListQuery{})
	wantKind(t, err, errs.Unauthenticated)
}

// NamesOf and Active need no actor: they run from signal handlers and jobs.
func TestNamesOfAndActive_ServeTheSystem(t *testing.T) {
	u := newUnit(t)
	for name, ctx := range map[string]context.Context{"no session": ctxT(t), "signal delivery": inDelivery(t)} {
		t.Run(name, func(t *testing.T) {
			got, err := u.svc.NamesOf(ctx, []model.DepartmentID{newDept()})
			wantNoErr(t, err)
			if len(got) != 0 {
				t.Errorf("NamesOf of an unknown department = %v", got)
			}
			ok, err := u.svc.Active(ctx, newDept())
			wantNoErr(t, err)
			if ok {
				t.Error("Active of an unknown department = true")
			}
		})
	}
}

// ---- permissions on the way in ---------------------------------------------------------

// Decision 2: a signed-in person with no grant lists active departments.
func TestList_ActiveWithoutAnyPermission_Allowed(t *testing.T) {
	u := newUnit(t)
	got, err := u.svc.List(asPerson(ctxT(t), newPerson()), departments.ListQuery{})
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Errorf("List over an empty store = %v", got)
	}
}

// F3
func TestList_IncludeArchivedWithoutManage_ReturnsPermission(t *testing.T) {
	tests := []struct {
		name  string
		grant func(r *rig, p model.PersonID)
	}{
		{"nothing", func(*rig, model.PersonID) {}},
		{"details at a department", func(r *rig, p model.PersonID) {
			r.az.GrantIn(p, departments.PermissionDetailsManage, newDept())
		}},
		{"details site-wide", func(r *rig, p model.PersonID) { r.az.Grant(p, departments.PermissionDetailsManage) }},
		{"manage at a department only", func(r *rig, p model.PersonID) {
			r.az.GrantIn(p, departments.PermissionManage, newDept())
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			me := newPerson()
			tc.grant(u.rig, me)
			_, err := u.svc.List(asPerson(ctxT(t), me), departments.ListQuery{IncludeArchived: true})
			wantKind(t, err, errs.Permission)
			u.noStatements(t, "a refused list")
		})
	}
}

func TestList_IncludeArchivedAsHolderOrSuperuser_Allowed(t *testing.T) {
	u := newUnit(t)
	me := newPerson()
	u.az.Grant(me, departments.PermissionManage)
	_, err := u.svc.List(asPerson(ctxT(t), me), departments.ListQuery{IncludeArchived: true})
	wantNoErr(t, err)
	_, err = u.svc.List(asSuper(ctxT(t), newPerson()), departments.ListQuery{IncludeArchived: true})
	wantNoErr(t, err)
}

// F4: a zero ID is never a department.
func TestGet_ZeroID_ReturnsNotFound(t *testing.T) {
	u := newUnit(t)
	_, err := u.svc.Get(asPerson(ctxT(t), newPerson()), model.DepartmentID{})
	wantKind(t, err, errs.NotFound)
}

// F7 and the permission table for departments.manage.
func TestCreate_WithoutManage_ReturnsPermission(t *testing.T) {
	tests := []struct {
		name  string
		grant func(r *rig, p model.PersonID)
	}{
		{"no grants", func(*rig, model.PersonID) {}},
		{"details.manage site-wide", func(r *rig, p model.PersonID) { r.az.Grant(p, departments.PermissionDetailsManage) }},
		{"details.manage at a department", func(r *rig, p model.PersonID) {
			r.az.GrantIn(p, departments.PermissionDetailsManage, newDept())
		}},
		{"manage at a department is not site-wide", func(r *rig, p model.PersonID) {
			r.az.GrantIn(p, departments.PermissionManage, newDept())
		}},
		{"a permission of another area", func(r *rig, p model.PersonID) { r.az.Grant(p, "events.manage") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			me := newPerson()
			tc.grant(u.rig, me)
			_, err := u.svc.Create(asPerson(ctxT(t), me), departments.CreateInput{Name: "Rangers"})
			wantKind(t, err, errs.Permission)
			u.noStatements(t, "a refused create")
		})
	}
}

// F8
func TestCreate_BadName_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name string
		in   string
		code string
	}{
		{"empty", "", "required"},
		{"101 bytes", repeat("a", departments.MaxNameBytes+1), "too_long"},
		{"51 two-byte runes are 102 bytes", repeat("é", 51), "too_long"},
		{"control character", "Ran\x07gers", "control_chars"},
		{"NUL", "Ran\x00gers", "control_chars"},
		{"newline", "Ran\ngers", "control_chars"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			me := newPerson()
			u.az.Grant(me, departments.PermissionManage)
			_, err := u.svc.Create(asPerson(ctxT(t), me), departments.CreateInput{Name: tc.in})
			wantField(t, err, "name", tc.code)
			if tc.code == "too_long" {
				if got := paramOf(err, "name", "too_long", "max"); got != "100" {
					t.Errorf("max param = %q, want 100", got)
				}
			}
			u.noStatements(t, "an invalid create")
		})
	}
}

// F9
func TestCreate_LongDescription_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	me := newPerson()
	u.az.Grant(me, departments.PermissionManage)
	_, err := u.svc.Create(asPerson(ctxT(t), me), departments.CreateInput{Name: "Rangers", Description: repeat("d", departments.MaxDescriptionBytes+1)})
	wantField(t, err, "description", "too_long")
	if got := paramOf(err, "description", "too_long", "max"); got != "2000" {
		t.Errorf("max param = %q, want 2000", got)
	}
	u.noStatements(t, "an invalid create")
}

// F10
func TestCreate_BadContactEmail_ReturnsInvalid(t *testing.T) {
	for _, bad := range []string{"not-an-email", "two@@example.org", "no-domain@", "@example.org", "spa ce@example.org"} {
		t.Run(bad, func(t *testing.T) {
			u := newUnit(t)
			me := newPerson()
			u.az.Grant(me, departments.PermissionManage)
			_, err := u.svc.Create(asPerson(ctxT(t), me), departments.CreateInput{Name: "Rangers", ContactEmail: bad})
			wantField(t, err, "contact_email", "malformed")
			u.noStatements(t, "an invalid create")
		})
	}
}

// Every problem is reported at once.
func TestCreate_AllFieldsBad_ReportsEachField(t *testing.T) {
	u := newUnit(t)
	me := newPerson()
	u.az.Grant(me, departments.PermissionManage)
	_, err := u.svc.Create(asPerson(ctxT(t), me), departments.CreateInput{
		Name: "", Description: repeat("d", departments.MaxDescriptionBytes+1), ContactEmail: "nope",
	})
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{"name", "description", "contact_email"} {
		if !hasField(err, f, "") {
			t.Errorf("no field error for %s in %v", f, err)
		}
	}
}

// Validation is "before any statement" and before the permission check: a person without
// the permission who sends bad input still sees Invalid only if validation runs first.
// Behavior orders actor, validate, permission.
func TestCreate_InvalidInputBeforePermission(t *testing.T) {
	u := newUnit(t)
	_, err := u.svc.Create(asPerson(ctxT(t), newPerson()), departments.CreateInput{Name: ""})
	wantKind(t, err, errs.Invalid)
}

// F12
func TestUpdate_NoFields_ReturnsInvalid(t *testing.T) {
	for name, grant := range map[string]bool{"holder": true, "non-holder": false} {
		t.Run(name, func(t *testing.T) {
			u := newUnit(t)
			me := newPerson()
			if grant {
				u.az.Grant(me, departments.PermissionManage)
				u.az.Grant(me, departments.PermissionDetailsManage)
			}
			_, err := u.svc.Update(asPerson(ctxT(t), me), newDept(), departments.UpdateInput{})
			wantField(t, err, "body", "required")
			u.noStatements(t, "an empty update")
		})
	}
}

// F13, F14 and decision 6: Update decides by the fields set, and both checks run when
// both kinds of field are set.
func TestUpdate_PermissionByField_ReturnsPermission(t *testing.T) {
	d := newDept()
	other := newDept()
	tests := []struct {
		name  string
		in    departments.UpdateInput
		grant func(r *rig, p model.PersonID)
	}{
		{"rename with no grants", departments.UpdateInput{Name: str("Rangers 2")}, func(*rig, model.PersonID) {}},
		{"rename with details.manage only", departments.UpdateInput{Name: str("Rangers 2")}, func(r *rig, p model.PersonID) {
			r.az.Grant(p, departments.PermissionDetailsManage)
		}},
		{"rename with details.manage at the department", departments.UpdateInput{Name: str("Rangers 2")}, func(r *rig, p model.PersonID) {
			r.az.GrantIn(p, departments.PermissionDetailsManage, d)
		}},
		{"rename with manage at a department only", departments.UpdateInput{Name: str("Rangers 2")}, func(r *rig, p model.PersonID) {
			r.az.GrantIn(p, departments.PermissionManage, d)
		}},
		{"details with no grants", departments.UpdateInput{Description: str("x")}, func(*rig, model.PersonID) {}},
		{"details with details.manage at another department", departments.UpdateInput{Description: str("x")}, func(r *rig, p model.PersonID) {
			r.az.GrantIn(p, departments.PermissionDetailsManage, other)
		}},
		{"contact with manage only", departments.UpdateInput{ContactEmail: str("rangers@example.org")}, func(r *rig, p model.PersonID) {
			r.az.Grant(p, departments.PermissionManage)
		}},
		{"name and details with details.manage only", departments.UpdateInput{Name: str("Rangers 2"), Description: str("x")}, func(r *rig, p model.PersonID) {
			r.az.GrantIn(p, departments.PermissionDetailsManage, d)
		}},
		{"name and details with manage only", departments.UpdateInput{Name: str("Rangers 2"), Description: str("x")}, func(r *rig, p model.PersonID) {
			r.az.Grant(p, departments.PermissionManage)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			me := newPerson()
			tc.grant(u.rig, me)
			_, err := u.svc.Update(asPerson(ctxT(t), me), d, tc.in)
			wantKind(t, err, errs.Permission)
			u.noStatements(t, "a refused update")
		})
	}
}

// F18
func TestUpdate_BadValues_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name  string
		in    departments.UpdateInput
		field string
		code  string
	}{
		{"empty name", departments.UpdateInput{Name: str("")}, "name", "required"},
		{"long name", departments.UpdateInput{Name: str(repeat("a", 101))}, "name", "too_long"},
		{"control name", departments.UpdateInput{Name: str("a\x07b")}, "name", "control_chars"},
		{"long description", departments.UpdateInput{Description: str(repeat("d", 2001))}, "description", "too_long"},
		{"bad email", departments.UpdateInput{ContactEmail: str("nope")}, "contact_email", "malformed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			me := newPerson()
			u.az.Grant(me, departments.PermissionManage)
			u.az.Grant(me, departments.PermissionDetailsManage)
			_, err := u.svc.Update(asPerson(ctxT(t), me), newDept(), tc.in)
			wantField(t, err, tc.field, tc.code)
			u.noStatements(t, "an invalid update")
		})
	}
}

// F19, F24
func TestArchiveRestore_WithoutManage_ReturnsPermission(t *testing.T) {
	d := newDept()
	for _, name := range []string{"Archive", "Restore"} {
		for gname, grant := range map[string]func(*rig, model.PersonID){
			"no grants":                func(*rig, model.PersonID) {},
			"details.manage at D":      func(r *rig, p model.PersonID) { r.az.GrantIn(p, departments.PermissionDetailsManage, d) },
			"details.manage site-wide": func(r *rig, p model.PersonID) { r.az.Grant(p, departments.PermissionDetailsManage) },
			"manage at D only":         func(r *rig, p model.PersonID) { r.az.GrantIn(p, departments.PermissionManage, d) },
		} {
			t.Run(name+"/"+gname, func(t *testing.T) {
				u := newUnit(t)
				me := newPerson()
				grant(u.rig, me)
				var err error
				if name == "Archive" {
					_, err = u.svc.Archive(asPerson(ctxT(t), me), d)
				} else {
					_, err = u.svc.Restore(asPerson(ctxT(t), me), d)
				}
				wantKind(t, err, errs.Permission)
				u.noStatements(t, "a refused "+name)
			})
		}
	}
}

// F26
func TestNamesOf_TooMany_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	ids := make([]model.DepartmentID, departments.MaxNames+1)
	for i := range ids {
		ids[i] = newDept()
	}
	_, err := u.svc.NamesOf(ctxT(t), ids)
	wantField(t, err, "ids", "too_many")
	if got := paramOf(err, "ids", "too_many", "max"); got != "200" {
		t.Errorf("max param = %q, want 200", got)
	}
	if n := len(u.fake.Calls()); n != 0 {
		t.Errorf("%d statements for an over-long list", n)
	}
	// The boundary is accepted.
	if _, err := u.svc.NamesOf(ctxT(t), ids[:departments.MaxNames]); err != nil {
		t.Errorf("exactly MaxNames IDs: %v", err)
	}
}

func TestNamesOf_EmptyAndNil_ReturnEmpty(t *testing.T) {
	u := newUnit(t)
	for name, ids := range map[string][]model.DepartmentID{"nil": nil, "empty": {}} {
		got, err := u.svc.NamesOf(ctxT(t), ids)
		if err != nil || len(got) != 0 {
			t.Errorf("%s: got %v, %v", name, got, err)
		}
	}
}

// F29
func TestActive_ZeroID_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	_, err := u.svc.Active(ctxT(t), model.DepartmentID{})
	wantField(t, err, "id", "required")
	if n := len(u.fake.Calls()); n != 0 {
		t.Errorf("%d statements for a zero id", n)
	}
}

// F30 (unknown half, over the empty fake store)
func TestActive_Unknown_False(t *testing.T) {
	u := newUnit(t)
	ok, err := u.svc.Active(ctxT(t), newDept())
	wantNoErr(t, err)
	if ok {
		t.Error("Active(unknown) = true")
	}
}

// A cancelled context stops a read before it reaches the store's answer.
func TestUseCases_CancelledContext_ReturnsCanceled(t *testing.T) {
	me := newPerson()
	for _, uc := range allUseCases() {
		t.Run(uc.name, func(t *testing.T) {
			r := newRig(t, failDB{err: errs.E("test.failDB", errs.Canceled, context.Canceled)})
			r.az.Grant(me, departments.PermissionManage)
			r.az.Grant(me, departments.PermissionDetailsManage)
			ctx, cancel := context.WithCancel(asPerson(ctxT(t), me))
			cancel()
			err := uc.call(ctx, r.svc)
			if err == nil {
				t.Fatal("a cancelled context succeeded")
			}
			if !errs.Is(err, errs.Canceled) {
				t.Errorf("kind = %v, want Canceled", errs.KindOf(err))
			}
		})
	}
}

// ---- read-only copy and store failure ----------------------------------------------------

func TestWrites_ReadOnlyCopy_ReturnUnavailableReadOnly(t *testing.T) {
	me := newPerson()
	d := newDept()
	writes := []useCase{
		{"Create", func(ctx context.Context, s *departments.Service) error {
			_, err := s.Create(ctx, departments.CreateInput{Name: "Rangers"})
			return keep(err)
		}},
		{"Update rename", func(ctx context.Context, s *departments.Service) error {
			_, err := s.Update(ctx, d, departments.UpdateInput{Name: str("Rangers 2")})
			return keep(err)
		}},
		{"Update details", func(ctx context.Context, s *departments.Service) error {
			_, err := s.Update(ctx, d, departments.UpdateInput{Description: str("x")})
			return keep(err)
		}},
		{"Archive", func(ctx context.Context, s *departments.Service) error { _, err := s.Archive(ctx, d); return keep(err) }},
		{"Restore", func(ctx context.Context, s *departments.Service) error { _, err := s.Restore(ctx, d); return keep(err) }},
	}
	for _, uc := range writes {
		t.Run(uc.name, func(t *testing.T) {
			u := newUnit(t)
			u.fake.SetReadOnly(true)
			u.az.Grant(me, departments.PermissionManage)
			u.az.Grant(me, departments.PermissionDetailsManage)
			wantReadOnly(t, uc.call(asPerson(ctxT(t), me), u.svc))
			if len(u.rec.Recorded()) != 0 {
				t.Errorf("entries %v on a read-only copy", actionsOf(u.rec.Recorded()))
			}
			if len(u.bus.Published()) != 0 {
				t.Error("a signal was published on a read-only copy")
			}
		})
	}
}

func TestReads_ReadOnlyCopy_Work(t *testing.T) {
	u := newUnit(t)
	u.fake.SetReadOnly(true)
	ctx := asPerson(ctxT(t), newPerson())
	if _, err := u.svc.List(ctx, departments.ListQuery{}); err != nil {
		t.Errorf("List on a read-only copy: %v", err)
	}
	if _, err := u.svc.NamesOf(ctx, []model.DepartmentID{newDept()}); err != nil {
		t.Errorf("NamesOf on a read-only copy: %v", err)
	}
	if _, err := u.svc.Active(ctx, newDept()); err != nil {
		t.Errorf("Active on a read-only copy: %v", err)
	}
}

func TestStoreFailure_PropagatesKind(t *testing.T) {
	me := newPerson()
	d := newDept()
	calls := append(allUseCases(),
		useCase{"NamesOf", func(ctx context.Context, s *departments.Service) error {
			_, err := s.NamesOf(ctx, []model.DepartmentID{d})
			return keep(err)
		}},
		useCase{"Active", func(ctx context.Context, s *departments.Service) error {
			_, err := s.Active(ctx, d)
			return keep(err)
		}},
		useCase{"Update rename", func(ctx context.Context, s *departments.Service) error {
			_, err := s.Update(ctx, d, departments.UpdateInput{Name: str("Rangers 2")})
			return keep(err)
		}},
	)
	for _, kind := range []errs.Kind{errs.Unavailable, errs.Timeout, errs.Canceled} {
		for _, uc := range calls {
			t.Run(kind.String()+"/"+uc.name, func(t *testing.T) {
				r := newRig(t, failDB{err: errs.E("test.failDB", kind, context.DeadlineExceeded)})
				r.az.Grant(me, departments.PermissionManage)
				r.az.Grant(me, departments.PermissionDetailsManage)
				wantKind(t, uc.call(asPerson(ctxT(t), me), r.svc), kind)
				if len(r.rec.Recorded()) != 0 || len(r.bus.Published()) != 0 {
					t.Error("an audit entry or a signal for a failed write")
				}
			})
		}
	}
}

// ---- permissions, constants, signals ----------------------------------------------------

func TestPermissions_MatchTheSpecTable(t *testing.T) {
	type want struct {
		scope authz.Scope
		roles []string
	}
	table := map[authz.Permission]want{
		"departments.manage":         {authz.ScopeSite, []string{authz.RoleDepartmentSteward}},
		"departments.details.manage": {authz.ScopeDepartment, []string{authz.RoleDepartmentAdmin, authz.RoleDepartmentSteward}},
	}
	if departments.PermissionManage != "departments.manage" || departments.PermissionDetailsManage != "departments.details.manage" {
		t.Errorf("permission constants = %q, %q", departments.PermissionManage, departments.PermissionDetailsManage)
	}
	if len(departments.Permissions) != len(table) {
		t.Fatalf("%d declarations, want %d", len(departments.Permissions), len(table))
	}
	for _, d := range departments.Permissions {
		w, ok := table[d.Name]
		if !ok {
			t.Errorf("unexpected permission %q", d.Name)
			continue
		}
		if d.Scope != w.scope {
			t.Errorf("%s: scope %v, want %v", d.Name, d.Scope, w.scope)
		}
		if d.Reaches != model.SensitivityInternal {
			t.Errorf("%s: reaches %v, want internal", d.Name, d.Reaches)
		}
		got, wantRoles := slices.Clone(d.StarterRoles), slices.Clone(w.roles)
		slices.Sort(got)
		slices.Sort(wantRoles)
		if !slices.Equal(got, wantRoles) {
			t.Errorf("%s: starter roles %v, want %v", d.Name, got, wantRoles)
		}
		if slices.Contains(d.StarterRoles, authz.RoleParticipant) {
			t.Errorf("%s is seeded into the base role", d.Name)
		}
		if err := d.Validate(); err != nil {
			t.Errorf("%s: %v", d.Name, err)
		}
	}
}

func TestConstantsAndSignals_MatchTheSpec(t *testing.T) {
	if departments.MaxNameBytes != 100 || departments.MaxDescriptionBytes != 2000 || departments.MaxNames != 200 {
		t.Errorf("limits = %d, %d, %d", departments.MaxNameBytes, departments.MaxDescriptionBytes, departments.MaxNames)
	}
	want := map[string]string{
		string(departments.SignalCreated):  "departments.department-created",
		string(departments.SignalArchived): "departments.department-archived",
		string(departments.SignalRestored): "departments.department-restored",
		departments.PayloadDepartment:      "department",
	}
	for got, w := range want {
		if got != w {
			t.Errorf("constant = %q, want %q", got, w)
		}
	}
	for _, n := range []string{"departments.department-created", "departments.department-archived", "departments.department-restored"} {
		found := false
		for _, p := range departments.PublishedSignals {
			found = found || string(p) == n
		}
		if !found {
			t.Errorf("PublishedSignals lacks %s", n)
		}
	}
	if len(departments.PublishedSignals) != 3 {
		t.Errorf("PublishedSignals = %v, want exactly the three", departments.PublishedSignals)
	}
	if departments.Set.Name != "departments" || departments.Set.Schema != "core" {
		t.Errorf("Set = %q in %q", departments.Set.Name, departments.Set.Schema)
	}
}

// ---- Validate --------------------------------------------------------------------------

func TestValidate_Table(t *testing.T) {
	ok := departments.Department{ID: newDept(), Name: "Rangers", Description: "Keep people safe", ContactEmail: "rangers@example.org", CreatedAt: epoch, UpdatedAt: epoch}
	tests := []struct {
		name  string
		mod   func(*departments.Department)
		field string
		code  string
	}{
		{"valid", func(*departments.Department) {}, "", ""},
		{"no contact is fine", func(d *departments.Department) { d.ContactEmail = "" }, "", ""},
		{"name at the limit", func(d *departments.Department) { d.Name = repeat("a", 100) }, "", ""},
		{"description at the limit", func(d *departments.Department) { d.Description = repeat("d", 2000) }, "", ""},
		{"empty name", func(d *departments.Department) { d.Name = "" }, "name", "required"},
		{"long name", func(d *departments.Department) { d.Name = repeat("a", 101) }, "name", "too_long"},
		{"control name", func(d *departments.Department) { d.Name = "a\x07" }, "name", "control_chars"},
		{"long description", func(d *departments.Department) { d.Description = repeat("d", 2001) }, "description", "too_long"},
		{"bad email", func(d *departments.Department) { d.ContactEmail = "nope" }, "contact_email", "malformed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := ok
			tc.mod(&d)
			err := d.Validate()
			if tc.field == "" {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, tc.field, tc.code)
		})
	}
}

// ---- layering --------------------------------------------------------------------------

func TestImports_NeverModuleApiAppConfigIdentityEvents(t *testing.T) {
	forbidden := []string{
		"github.com/zedgama3/bms/internal/core/module",
		"github.com/zedgama3/bms/internal/core/identity",
		"github.com/zedgama3/bms/internal/core/events",
		"github.com/zedgama3/bms/internal/api",
		"github.com/zedgama3/bms/internal/app",
		"github.com/zedgama3/bms/internal/config",
		"github.com/zedgama3/bms/internal/modules",
		"github.com/zedgama3/bms/web",
	}
	for _, dir := range []string{".", "./postgres"} {
		pkg, err := build.ImportDir(dir, 0)
		if err != nil {
			t.Fatalf("ImportDir(%s): %v", dir, err)
		}
		for _, imp := range pkg.Imports {
			for _, bad := range forbidden {
				if imp == bad || strings.HasPrefix(imp, bad+"/") {
					t.Errorf("%s imports %s", dir, imp)
				}
			}
		}
	}
}

// ---- routes, OpenAPI, handlers ----------------------------------------------------------

var specRoutes = []string{
	"GET /", "POST /",
	"GET /{department}", "PUT /{department}",
	"POST /{department}/archive", "POST /{department}/restore",
}

func TestRoutes_MatchTheSpecTable(t *testing.T) {
	u := newUnit(t)
	var got []string
	for _, rt := range u.svc.Routes() {
		if rt.Handler == nil {
			t.Errorf("%s %s: nil handler", rt.Method, rt.Pattern)
		}
		got = append(got, rt.Method+" "+rt.Pattern)
	}
	slices.Sort(got)
	want := slices.Clone(specRoutes)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("routes differ\n got: %v\nwant: %v", got, want)
	}
}

// F33
func TestOpenAPI_NamesExactlyTheRoutes(t *testing.T) {
	u := newUnit(t)
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(u.svc.OpenAPI(), &doc); err != nil {
		t.Fatalf("OpenAPI is not JSON: %v", err)
	}
	described := map[string]bool{}
	for path, ops := range doc.Paths {
		for method := range ops {
			described[strings.ToUpper(method)+" "+path] = true
		}
	}
	for _, rt := range specRoutes {
		if !described[rt] {
			t.Errorf("%s is not described", rt)
		}
		delete(described, rt)
	}
	for extra := range described {
		t.Errorf("the fragment describes %s, which has no route", extra)
	}
}

// F32 and the status mapping of the other failures.
func TestHandlers_BadPathID_Returns400(t *testing.T) {
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/not-a-uuid", ""},
		{"PUT", "/not-a-uuid", `{"description":"x"}`},
		{"POST", "/not-a-uuid/archive", ""},
		{"POST", "/not-a-uuid/restore", ""},
		{"GET", "/12345", ""},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			u := newUnit(t)
			me := newPerson()
			u.az.Grant(me, departments.PermissionManage)
			u.az.Grant(me, departments.PermissionDetailsManage)
			w, f := u.do(asPerson(ctxT(t), me), tc.method, tc.path, tc.body)
			if f.count() != 1 {
				t.Fatalf("%d failures (status %d), want 1", f.count(), w.Code)
			}
			wantField(t, f.last(), "department", "malformed")
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", w.Code)
			}
			if n := len(u.fake.Calls()); n != 0 {
				t.Errorf("%d statements for a malformed id", n)
			}
		})
	}
}

func TestHandlers_Errors_MapToStatus(t *testing.T) {
	me := newPerson()
	d := newDept().String()
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		signed bool
		grant  []authz.Permission
		kind   errs.Kind
		status int
	}{
		{"list without a session", "GET", "/", "", false, nil, errs.Unauthenticated, 401},
		{"get without a session", "GET", "/" + d, "", false, nil, errs.Unauthenticated, 401},
		{"create without a session", "POST", "/", `{"name":"Rangers"}`, false, nil, errs.Unauthenticated, 401},
		{"create without permission", "POST", "/", `{"name":"Rangers"}`, true, nil, errs.Permission, 403},
		{"create with a bad name", "POST", "/", `{"name":""}`, true, []authz.Permission{departments.PermissionManage}, errs.Invalid, 400},
		{"create with a body that is not json", "POST", "/", `{nope`, true, []authz.Permission{departments.PermissionManage}, errs.Invalid, 400},
		{"create with an array body", "POST", "/", `[]`, true, []authz.Permission{departments.PermissionManage}, errs.Invalid, 400},
		{"update with no field", "PUT", "/" + d, `{}`, true, []authz.Permission{departments.PermissionManage}, errs.Invalid, 400},
		{"update details without permission", "PUT", "/" + d, `{"description":"x"}`, true, nil, errs.Permission, 403},
		{"rename without permission", "PUT", "/" + d, `{"name":"x"}`, true, []authz.Permission{departments.PermissionDetailsManage}, errs.Permission, 403},
		{"archive without permission", "POST", "/" + d + "/archive", "", true, nil, errs.Permission, 403},
		{"restore without permission", "POST", "/" + d + "/restore", "", true, nil, errs.Permission, 403},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			ctx := ctxT(t)
			if tc.signed {
				ctx = asPerson(ctx, me)
			}
			for _, p := range tc.grant {
				u.az.Grant(me, p)
			}
			w, f := u.do(ctx, tc.method, tc.path, tc.body)
			if f.count() != 1 {
				t.Fatalf("%d failures handed to httpx.Fail (status %d), want 1", f.count(), w.Code)
			}
			wantKind(t, f.last(), tc.kind)
			if w.Code != tc.status {
				t.Errorf("status = %d, want %d", w.Code, tc.status)
			}
		})
	}
}

func TestHandlers_WriteOnReadOnlyCopy_Maps503(t *testing.T) {
	u := newUnit(t)
	u.fake.SetReadOnly(true)
	me := newPerson()
	u.az.Grant(me, departments.PermissionManage)
	w, f := u.do(asPerson(ctxT(t), me), "POST", "/", `{"name":"Rangers"}`)
	if f.count() != 1 {
		t.Fatalf("%d failures, status %d", f.count(), w.Code)
	}
	wantReadOnly(t, f.last())
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestHandlers_List_IsJSON(t *testing.T) {
	u := newUnit(t)
	w, f := u.do(asPerson(ctxT(t), newPerson()), "GET", "/", "")
	if f.count() != 0 || w.Code != http.StatusOK {
		t.Fatalf("status %d, failures %d (%v)", w.Code, f.count(), f.last())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	if !json.Valid(w.Body.Bytes()) {
		t.Errorf("body is not JSON: %s", w.Body.String())
	}
}

// TestWire_Shapes pins the JSON the web screens read.
func TestWire_Shapes(t *testing.T) {
	keys := func(t *testing.T, v any) map[string]bool {
		t.Helper()
		raw, err := json.Marshal(v)
		wantNoErr(t, err)
		var m map[string]json.RawMessage
		wantNoErr(t, json.Unmarshal(raw, &m))
		out := map[string]bool{}
		for k := range m {
			out[k] = true
		}
		return out
	}
	equal := func(t *testing.T, got map[string]bool, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("keys = %v, want %v", got, want)
		}
		for _, k := range want {
			if !got[k] {
				t.Errorf("missing key %q in %v", k, got)
			}
		}
	}
	active := departments.Department{ID: newDept(), Name: "Rangers", CreatedAt: epoch, UpdatedAt: epoch}
	equal(t, keys(t, active), "id", "name", "description", "archived", "created_at", "updated_at")
	full := active
	full.ContactEmail = "rangers@example.org"
	full.Archived = true
	full.ArchivedAt = epoch.Add(time.Hour)
	equal(t, keys(t, full), "id", "name", "description", "contact_email", "archived", "archived_at", "created_at", "updated_at")

	var in departments.UpdateInput
	wantNoErr(t, json.Unmarshal([]byte(`{"description":"x"}`), &in))
	if in.Name != nil || in.ContactEmail != nil || in.Description == nil || *in.Description != "x" {
		t.Errorf("UpdateInput decoded as %+v", in)
	}
	var cr departments.CreateInput
	wantNoErr(t, json.Unmarshal([]byte(`{"name":"A","description":"B","contact_email":"c@example.org"}`), &cr))
	if cr.Name != "A" || cr.Description != "B" || cr.ContactEmail != "c@example.org" {
		t.Errorf("CreateInput decoded as %+v", cr)
	}
}
