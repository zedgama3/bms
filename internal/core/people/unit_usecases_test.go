package people_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"        //nolint:depguard // failDB must implement db.DB, whose Query returns pgx.Rows
	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // failDB must implement db.DB, whose Exec returns pgconn.CommandTag

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/signals"
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
	call func(ctx context.Context, s *people.Service) error
}

func allUseCases() []useCase {
	other := newPerson()
	cid := model.NewID[people.ContactID]()
	rid := model.NewID[people.RequestID]()
	in := people.ProfileInput{PlayaName: str("Dusty Ember")}
	ci := people.ContactInput{Name: "Orville Tansy", Phone: "+1 555 0188"}
	email := model.Email("link.target@example.org")
	return []useCase{
		{"Me", func(ctx context.Context, s *people.Service) error { _, err := s.Me(ctx); return keep(err) }},
		{"UpdateMe", func(ctx context.Context, s *people.Service) error { _, err := s.UpdateMe(ctx, in); return keep(err) }},
		{"Get", func(ctx context.Context, s *people.Service) error { _, err := s.Get(ctx, other); return keep(err) }},
		{"Create", func(ctx context.Context, s *people.Service) error { _, err := s.Create(ctx, in); return keep(err) }},
		{"Update", func(ctx context.Context, s *people.Service) error {
			_, err := s.Update(ctx, other, in)
			return keep(err)
		}},
		{"Search", func(ctx context.Context, s *people.Service) error {
			_, err := s.Search(ctx, people.SearchQuery{Q: "ember"})
			return keep(err)
		}},
		{"AddContact", func(ctx context.Context, s *people.Service) error {
			_, err := s.AddContact(ctx, other, ci)
			return keep(err)
		}},
		{"UpdateContact", func(ctx context.Context, s *people.Service) error {
			_, err := s.UpdateContact(ctx, other, cid, ci)
			return keep(err)
		}},
		{"RemoveContact", func(ctx context.Context, s *people.Service) error { return s.RemoveContact(ctx, other, cid) }},
		{"SetConsent", func(ctx context.Context, s *people.Service) error {
			_, err := s.SetConsent(ctx, other, "volunteers.share-contact", true)
			return keep(err)
		}},
		{"LinkAccount", func(ctx context.Context, s *people.Service) error {
			_, err := s.LinkAccount(ctx, other, email)
			return keep(err)
		}},
		{"Export", func(ctx context.Context, s *people.Service) error { _, err := s.Export(ctx, other); return keep(err) }},
		{"Delete", func(ctx context.Context, s *people.Service) error { _, err := s.Delete(ctx, other); return keep(err) }},
		{"RequestDeletion", func(ctx context.Context, s *people.Service) error {
			_, err := s.RequestDeletion(ctx, "")
			return keep(err)
		}},
		{"MyRequests", func(ctx context.Context, s *people.Service) error { _, err := s.MyRequests(ctx); return keep(err) }},
		{"WithdrawRequest", func(ctx context.Context, s *people.Service) error { return s.WithdrawRequest(ctx, rid) }},
		{"ListRequests", func(ctx context.Context, s *people.Service) error {
			_, err := s.ListRequests(ctx, people.RequestQuery{})
			return keep(err)
		}},
		{"DeclineRequest", func(ctx context.Context, s *people.Service) error {
			_, err := s.DeclineRequest(ctx, rid, "")
			return keep(err)
		}},
		{"FulfilRequest", func(ctx context.Context, s *people.Service) error {
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
				// Even a session holding every permission is no actor when it is
				// restricted or inside a delivery.
				err := uc.call(mk(t), u.svc)
				wantKind(t, err, errs.Unauthenticated)
				if n := len(u.fake.Calls()); n != 0 {
					t.Errorf("%d statements before refusing an unauthenticated call", n)
				}
				if len(u.rec.Recorded()) != 0 {
					t.Error("an audit entry for an unauthenticated call")
				}
			})
		}
	}
}

// F3: the exceptions.
func TestNamesOfAndConsentOf_ServeTheSystem(t *testing.T) {
	u := newUnit(t)
	for name, ctx := range map[string]context.Context{"no session": ctxT(t), "signal delivery": inDelivery(t)} {
		t.Run(name, func(t *testing.T) {
			got, err := u.svc.NamesOf(ctx, []model.PersonID{newPerson()})
			wantNoErr(t, err)
			if len(got) != 0 {
				t.Errorf("NamesOf of an unknown person = %v", got)
			}
			ok, err := u.svc.ConsentOf(ctx, newPerson(), "volunteers.share-contact")
			wantNoErr(t, err)
			if ok {
				t.Error("ConsentOf an unknown person = true")
			}
		})
	}
}

// F10, F14, F29, F32, F44: with no grants every protected call is refused, for an unknown
// ID as for a known one, so a 403 never confirms that an ID exists.
func TestUseCases_NoGrants_ReturnsPermission(t *testing.T) {
	protected := map[string]bool{
		"Get": true, "Create": true, "Update": true, "Search": true, "AddContact": true, "UpdateContact": true,
		"RemoveContact": true, "SetConsent": true, "LinkAccount": true, "Export": true, "Delete": true,
		"ListRequests": true, "DeclineRequest": true, "FulfilRequest": true,
	}
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
		{"delete cannot update", func(r *rig, p model.PersonID) { r.az.Grant(p, permDelete) }, "Update"},
		{"delete cannot link", func(r *rig, p model.PersonID) { r.az.Grant(p, permDelete) }, "LinkAccount"},
		{"delete cannot search", func(r *rig, p model.PersonID) { r.az.Grant(p, permDelete) }, "Search"},
		{"manage cannot get", func(r *rig, p model.PersonID) { r.az.Grant(p, permManage) }, "Get"},
		{"manage cannot search", func(r *rig, p model.PersonID) { r.az.Grant(p, permManage) }, "Search"},
		{"manage cannot export", func(r *rig, p model.PersonID) { r.az.Grant(p, permManage) }, "Export"},
		{"manage cannot delete", func(r *rig, p model.PersonID) { r.az.Grant(p, permManage) }, "Delete"},
		{"contacts alone cannot get", func(r *rig, p model.PersonID) { r.az.Grant(p, permContacts) }, "Get"},
		{"contacts alone cannot search", func(r *rig, p model.PersonID) { r.az.Grant(p, permContacts) }, "Search"},
		{"directory cannot update", func(r *rig, p model.PersonID) { r.az.Grant(p, permDirectory) }, "Update"},
		{"directory cannot create", func(r *rig, p model.PersonID) { r.az.Grant(p, permDirectory) }, "Create"},
		{"directory cannot add a contact for another", func(r *rig, p model.PersonID) { r.az.Grant(p, permDirectory) }, "AddContact"},
		{"directory cannot set another's consent", func(r *rig, p model.PersonID) { r.az.Grant(p, permDirectory) }, "SetConsent"},
		{"contacts cannot update", func(r *rig, p model.PersonID) { r.az.Grant(p, permContacts) }, "Update"},
		// people.profiles.manage, export and delete are site permissions: a department
		// grant does not count.
		{"department manage is not enough", func(r *rig, p model.PersonID) { r.az.GrantIn(p, permManage, model.NewID[model.DepartmentID]()) }, "Update"},
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

// Decision 9: a department-scoped directory grant reaches every person for now.
func TestSearch_DepartmentExtent_Allowed(t *testing.T) {
	u := newUnit(t)
	me := newPerson()
	u.az.GrantIn(me, permDirectory, model.NewID[model.DepartmentID]())
	_, err := u.svc.Search(asPerson(ctxT(t), me), people.SearchQuery{Q: "ember"})
	wantNoErr(t, err)
}

func TestSearch_Superuser_Allowed(t *testing.T) {
	u := newUnit(t)
	_, err := u.svc.Search(asSuper(ctxT(t), newPerson()), people.SearchQuery{Q: "ember"})
	wantNoErr(t, err)
}

// F15
func TestSearch_BadQuery_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name  string
		q     people.SearchQuery
		field string
		code  string
	}{
		{"empty", people.SearchQuery{Q: ""}, "q", "too_short"},
		{"one byte", people.SearchQuery{Q: "a"}, "q", "too_short"},
		{"one byte padded with spaces", people.SearchQuery{Q: "   a   "}, "q", "too_short"},
		{"only spaces", people.SearchQuery{Q: "      "}, "q", "too_short"},
		{"101 bytes", people.SearchQuery{Q: repeat("a", 101)}, "q", "too_long"},
		{"limit negative", people.SearchQuery{Q: "ember", Limit: -1}, "limit", "range"},
		{"limit over max", people.SearchQuery{Q: "ember", Limit: people.MaxSearchResults + 1}, "limit", "range"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			me := newPerson()
			u.az.Grant(me, permDirectory)
			_, err := u.svc.Search(asPerson(ctxT(t), me), tc.q)
			wantField(t, err, tc.field, tc.code)
			if n := len(u.fake.Calls()); n != 0 {
				t.Errorf("%d statements for an invalid query", n)
			}
		})
	}
}

func TestSearch_BoundaryQueries_Accepted(t *testing.T) {
	for name, q := range map[string]people.SearchQuery{
		"two bytes":        {Q: "ab"},
		"100 bytes":        {Q: repeat("a", 100)},
		"limit max":        {Q: "ember", Limit: people.MaxSearchResults},
		"limit one":        {Q: "ember", Limit: 1},
		"padded two bytes": {Q: "  ab  "},
	} {
		t.Run(name, func(t *testing.T) {
			u := newUnit(t)
			me := newPerson()
			u.az.Grant(me, permDirectory)
			if _, err := u.svc.Search(asPerson(ctxT(t), me), q); err != nil {
				t.Fatalf("Search: %v", err)
			}
		})
	}
}

func TestSearch_TooShortErrorCarriesTheMinimum(t *testing.T) {
	u := newUnit(t)
	me := newPerson()
	u.az.Grant(me, permDirectory)
	_, err := u.svc.Search(asPerson(ctxT(t), me), people.SearchQuery{Q: "a"})
	if got := paramOf(err, "q", "too_short", "min"); got != "2" {
		t.Errorf("min param = %q, want 2", got)
	}
	_, err = u.svc.Search(asPerson(ctxT(t), me), people.SearchQuery{Q: repeat("a", 101)})
	if got := paramOf(err, "q", "too_long", "max"); got != "100" {
		t.Errorf("max param = %q, want 100", got)
	}
}

// F17
func TestNamesOf_TooMany_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	ids := make([]model.PersonID, people.MaxNames+1)
	for i := range ids {
		ids[i] = newPerson()
	}
	_, err := u.svc.NamesOf(ctxT(t), ids)
	wantField(t, err, "ids", "too_many")
	if got := paramOf(err, "ids", "too_many", "max"); got != "200" {
		t.Errorf("max param = %q, want 200", got)
	}
	if n := len(u.fake.Calls()); n != 0 {
		t.Errorf("%d statements for an over-long list", n)
	}
	if _, err := u.svc.NamesOf(ctxT(t), ids[:people.MaxNames]); err != nil {
		t.Errorf("exactly MaxNames IDs: %v", err)
	}
}

func TestNamesOf_EmptyAndNil_ReturnEmpty(t *testing.T) {
	u := newUnit(t)
	for name, ids := range map[string][]model.PersonID{"nil": nil, "empty": {}} {
		got, err := u.svc.NamesOf(ctxT(t), ids)
		if err != nil || len(got) != 0 {
			t.Errorf("%s: got %v, %v", name, got, err)
		}
	}
}

// F23
func TestSetConsent_BadPurpose_ReturnsInvalid(t *testing.T) {
	for _, bad := range []people.Purpose{"", "nodot", "Upper.case", "a.b c", "x." + people.Purpose(repeat("a", 70))} {
		t.Run(string(bad), func(t *testing.T) {
			u := newUnit(t)
			me := newPerson()
			_, err := u.svc.SetConsent(asPerson(ctxT(t), me), me, bad, true)
			wantField(t, err, "purpose", "malformed")
			if n := len(u.fake.Calls()); n != 0 {
				t.Errorf("%d statements for a bad purpose", n)
			}
		})
	}
}

func TestConsentOf_BadInput_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	_, err := u.svc.ConsentOf(ctxT(t), newPerson(), "nodot")
	wantField(t, err, "purpose", "malformed")
	_, err = u.svc.ConsentOf(ctxT(t), model.PersonID{}, "volunteers.share-contact")
	wantKind(t, err, errs.Invalid)
	if n := len(u.fake.Calls()); n != 0 {
		t.Errorf("%d statements for bad input", n)
	}
}

// F49
func TestPlayaNameOf_NoRow_ReturnsNotFound(t *testing.T) {
	u := newUnit(t)
	_, err := u.svc.PlayaNameOf(ctxT(t), newPerson())
	wantKind(t, err, errs.NotFound)
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

// F38 and the writes of the read-only family.
func TestWrites_ReadOnly_ReturnUnavailable(t *testing.T) {
	me := newPerson()
	writes := []struct {
		name  string
		grant []authz.Permission
		call  func(ctx context.Context, s *people.Service) error
	}{
		{"UpdateMe", nil, func(ctx context.Context, s *people.Service) error {
			_, err := s.UpdateMe(ctx, people.ProfileInput{PlayaName: str("Dusty Ember")})
			return keep(err)
		}},
		{"Create", []authz.Permission{permManage}, func(ctx context.Context, s *people.Service) error {
			_, err := s.Create(ctx, people.ProfileInput{PlayaName: str("Dusty Ember")})
			return keep(err)
		}},
		{"SetConsent", nil, func(ctx context.Context, s *people.Service) error {
			_, err := s.SetConsent(ctx, me, "volunteers.share-contact", true)
			return keep(err)
		}},
		{"RequestDeletion", nil, func(ctx context.Context, s *people.Service) error {
			_, err := s.RequestDeletion(ctx, "")
			return keep(err)
		}},
		{"Delete", []authz.Permission{permDelete}, func(ctx context.Context, s *people.Service) error {
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
		})
	}
}

// F46
func TestHandleAccountCreated_ReadOnly_ReturnsUnavailable(t *testing.T) {
	u := newUnit(t)
	u.fake.SetReadOnly(true)
	err := u.bus.Deliver(ctxT(t), accountCreatedSignal(model.NewID[model.AccountID](), newPerson()))
	wantReadOnly(t, err)
}

// F47
func TestHandleAccountCreated_BadPayload_ReturnsInvalid(t *testing.T) {
	tests := map[string]signals.Payload{
		"empty":        signals.NewPayload(),
		"account only": signals.NewPayload(signals.Ref(identity.PayloadAccount, model.NewID[model.AccountID]())),
		"other key":    signals.NewPayload(signals.Ref("thing", newPerson())),
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			u := newUnit(t)
			sig := accountCreatedSignal(model.NewID[model.AccountID](), newPerson())
			sig.Payload = payload
			wantKind(t, u.bus.Deliver(ctxT(t), sig), errs.Invalid)
			if n := len(u.fake.Calls()); n != 0 {
				t.Errorf("%d statements for a bad payload", n)
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
	calls := []useCase{}
	for _, uc := range allUseCases() {
		switch uc.name {
		case "Me", "UpdateMe", "MyRequests", "RequestDeletion":
			calls = append(calls, uc)
		}
	}
	calls = append(calls,
		useCase{"NamesOf", func(ctx context.Context, s *people.Service) error {
			_, err := s.NamesOf(ctx, []model.PersonID{newPerson()})
			return keep(err)
		}},
		useCase{"ConsentOf", func(ctx context.Context, s *people.Service) error {
			_, err := s.ConsentOf(ctx, newPerson(), "volunteers.share-contact")
			return keep(err)
		}},
		useCase{"PlayaNameOf", func(ctx context.Context, s *people.Service) error {
			_, err := s.PlayaNameOf(ctx, newPerson())
			return keep(err)
		}},
		useCase{"Search", func(ctx context.Context, s *people.Service) error {
			_, err := s.Search(ctx, people.SearchQuery{Q: "ember"})
			return keep(err)
		}},
		useCase{"Get", func(ctx context.Context, s *people.Service) error {
			_, err := s.Get(ctx, newPerson())
			return keep(err)
		}},
		useCase{"ListRequests", func(ctx context.Context, s *people.Service) error {
			_, err := s.ListRequests(ctx, people.RequestQuery{})
			return keep(err)
		}},
	)
	for _, kind := range kinds {
		for _, uc := range calls {
			t.Run(kind.String()+"/"+uc.name, func(t *testing.T) {
				r := newRig(t, failDB{err: errs.E("test.failDB", kind, errors.New("down"))})
				r.link()
				r.az.Grant(me, permDirectory)
				r.az.Grant(me, permDelete)
				err := uc.call(asPerson(ctxT(t), me), r.svc)
				wantKind(t, err, kind)
			})
		}
	}
}
