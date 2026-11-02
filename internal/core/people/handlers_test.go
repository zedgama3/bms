package people_test

import (
	"encoding/json"
	"go/build"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// ---- permissions ------------------------------------------------------------------------

// The data permissions are declared by datarights now (boundaries decision 12).
func TestPermissions_DataRightsPermissionsAreNotPeoples(t *testing.T) {
	for _, d := range people.Permissions {
		if d.Name == permExport || d.Name == permDelete {
			t.Errorf("people still declares %s, which datarights declares", d.Name)
		}
	}
}

func TestPermissions_MatchTheSpecTable(t *testing.T) {
	type want struct {
		scope   authz.Scope
		reaches model.Sensitivity
		roles   []string
	}
	table := map[authz.Permission]want{
		permDirectory: {authz.ScopeDepartment, model.SensitivityInternal, []string{
			authz.RoleAccountAdmin, authz.RoleAccessAdmin, authz.RoleAuditReviewer, authz.RoleDepartmentSteward, authz.RoleDepartmentAdmin, authz.RoleVolunteerCoordinator, // D44
		}},
		// D44 (people.md, Permissions table and amendment part 3): account-admin, the
		// volunteer-coordinator and the department-admin.
		permContacts: {authz.ScopeDepartment, model.SensitivityPersonal, []string{authz.RoleAccountAdmin, authz.RoleVolunteerCoordinator, authz.RoleDepartmentAdmin}},
		permManage:   {authz.ScopeSite, model.SensitivityPersonal, []string{authz.RoleAccountAdmin}},
	}
	if len(people.Permissions) != len(table) {
		t.Fatalf("%d declarations, want %d", len(people.Permissions), len(table))
	}
	for _, d := range people.Permissions {
		w, ok := table[d.Name]
		if !ok {
			t.Errorf("unexpected permission %q", d.Name)
			continue
		}
		if d.Scope != w.scope {
			t.Errorf("%s: scope %v, want %v", d.Name, d.Scope, w.scope)
		}
		if d.Reaches != w.reaches {
			t.Errorf("%s: reaches %v, want %v", d.Name, d.Reaches, w.reaches)
		}
		got := slices.Clone(d.StarterRoles)
		slices.Sort(got)
		wantRoles := slices.Clone(w.roles)
		slices.Sort(wantRoles)
		if !slices.Equal(got, wantRoles) {
			t.Errorf("%s: starter roles %v, want %v", d.Name, got, wantRoles)
		}
		if err := d.Validate(); err != nil {
			t.Errorf("%s: %v", d.Name, err)
		}
	}
}

// Decision 9 and 20, D36, D44: the base role holds none of them, and people.contacts.read
// is not seeded into the other administrators (it is held by account-admin, the
// volunteer-coordinator and the department-admin).
func TestPermissions_BaseRoleHoldsNone_ContactsNotForOtherAdmins(t *testing.T) {
	for _, d := range people.Permissions {
		if slices.Contains(d.StarterRoles, authz.RoleParticipant) {
			t.Errorf("%s is seeded into the base role", d.Name)
		}
		if d.Name == permContacts {
			for _, role := range []string{
				authz.RoleAccessAdmin, authz.RoleDepartmentSteward, authz.RoleEventAdmin, authz.RoleSettingsAdmin,
				authz.RoleAuditReviewer, authz.RoleParticipant,
			} {
				if slices.Contains(d.StarterRoles, role) {
					t.Errorf("people.contacts.read is seeded into %s; it is not an everyday administrator permission", role)
				}
			}
		}
	}
}

// D36: a signed-in person with no grants (the base role holds none) is refused everywhere
// that reads other people.
func TestBaseRole_SignedInWithNoGrants_IsDeniedTheDirectory(t *testing.T) {
	u := newUnit(t)
	ctx := asPerson(ctxT(t), newPerson())
	_, err := u.svc.Search(ctx, people.SearchQuery{Q: "ember"})
	wantKind(t, err, errs.Permission)
	_, err = u.svc.Get(ctx, newPerson())
	wantKind(t, err, errs.Permission)
}

// ---- layering ---------------------------------------------------------------------------

func TestImports_NeverModuleApiAppConfig(t *testing.T) {
	forbidden := []string{
		"github.com/zedgama3/bms/internal/core/module",
		"github.com/zedgama3/bms/internal/api",
		"github.com/zedgama3/bms/internal/app",
		"github.com/zedgama3/bms/internal/config",
		"github.com/zedgama3/bms/internal/modules",
		"github.com/zedgama3/bms/web",
	}
	for _, dir := range []string{".", "./peopletest", "./postgres"} {
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

// ---- routes -----------------------------------------------------------------------------

var specRoutes = []string{
	"GET /me", "PUT /me",
	"POST /me/contacts", "PUT /me/contacts/{contact}", "DELETE /me/contacts/{contact}",
	"PUT /me/consents/{purpose}",
	"GET /search", "GET /names",
	"POST /",
	"GET /{person}", "PUT /{person}",
	"POST /{person}/contacts", "PUT /{person}/contacts/{contact}", "DELETE /{person}/contacts/{contact}",
	"PUT /{person}/consents/{purpose}",
	"POST /{person}/link-account",
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

func TestOpenAPI_IsJSONAndDescribesEveryRoute(t *testing.T) {
	u := newUnit(t)
	raw := u.svc.OpenAPI()
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("OpenAPI is not JSON: %v", err)
	}
	for _, rt := range specRoutes {
		method, pattern, _ := strings.Cut(rt, " ")
		ops, ok := doc.Paths[pattern]
		if !ok {
			t.Errorf("no path %q in the fragment", pattern)
			continue
		}
		if _, ok := ops[strings.ToLower(method)]; !ok {
			t.Errorf("%s %s is not described", method, pattern)
		}
	}
	for path := range doc.Paths {
		found := false
		for _, rt := range specRoutes {
			if strings.HasSuffix(rt, " "+path) {
				found = true
			}
		}
		if !found {
			t.Errorf("the fragment describes %s, which has no route", path)
		}
	}
}

// F53
func TestHandlers_Errors_MapToProblem(t *testing.T) {
	me := newPerson()
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
		{"me without a session", "GET", "/me", "", false, nil, errs.Unauthenticated, 401},
		{"update me without a session", "PUT", "/me", `{"playa_name":"x"}`, false, nil, errs.Unauthenticated, 401},
		{"names are served to the system but the id list is checked", "GET", "/names?ids=not-a-uuid", "", true, nil, errs.Invalid, 400},
		{"bad person id", "GET", "/not-a-uuid", "", true, nil, errs.Invalid, 400},
		{"bad contact id", "DELETE", "/me/contacts/nope", "", true, nil, errs.Invalid, 400},
		{"body is not json", "PUT", "/me", `{nope`, true, nil, errs.Invalid, 400},
		{"body is an array", "PUT", "/me", `[]`, true, nil, errs.Invalid, 400},
		{"search too short", "GET", "/search?q=a", "", true, []authz.Permission{permDirectory}, errs.Invalid, 400},
		{"search limit not a number", "GET", "/search?q=ember&limit=abc", "", true, []authz.Permission{permDirectory}, errs.Invalid, 400},
		{"search without permission", "GET", "/search?q=ember", "", true, nil, errs.Permission, 403},
		{"other person without permission", "GET", "/" + newPerson().String(), "", true, nil, errs.Permission, 403},
		{"consent with a bad purpose", "PUT", "/me/consents/nodot", `{"granted":true}`, true, nil, errs.Invalid, 400},
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

func TestHandlers_NamesTooMany_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	ids := make([]string, people.MaxNames+1)
	for i := range ids {
		ids[i] = newPerson().String()
	}
	_, f := u.do(asPerson(ctxT(t), newPerson()), "GET", "/names?ids="+strings.Join(ids, ","), "")
	if f.count() != 1 {
		t.Fatalf("%d failures", f.count())
	}
	wantField(t, f.last(), "ids", "too_many")
}

func TestHandlers_Names_ServedToAnySignedInPersonWithoutPermission(t *testing.T) {
	u := newUnit(t)
	w, f := u.do(asPerson(ctxT(t), newPerson()), "GET", "/names?ids="+newPerson().String(), "")
	if f.count() != 0 || w.Code != http.StatusOK {
		t.Fatalf("status %d, failures %d (%v)", w.Code, f.count(), f.last())
	}
	if !json.Valid(w.Body.Bytes()) {
		t.Errorf("body is not JSON: %s", w.Body.String())
	}
}

func TestHandlers_Me_NoRow_IsOKJSON(t *testing.T) {
	u := newUnit(t)
	me := newPerson()
	w, f := u.do(asPerson(ctxT(t), me), "GET", "/me", "")
	if f.count() != 0 || w.Code != http.StatusOK {
		t.Fatalf("status %d, failures %d (%v)", w.Code, f.count(), f.last())
	}
	if !json.Valid(w.Body.Bytes()) || !strings.Contains(w.Body.String(), me.String()) {
		t.Errorf("body = %s", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestHandlers_GETRoutes_ChangeNothing(t *testing.T) {
	u := newUnit(t)
	me := newPerson()
	u.az.Grant(me, permDirectory)
	for _, path := range []string{"/me", "/search?q=ember", "/names?ids=" + newPerson().String()} {
		u.fake.SetReadOnly(true)
		w, f := u.do(asPerson(ctxT(t), me), "GET", path, "")
		if f.count() != 0 || w.Code != http.StatusOK {
			t.Errorf("GET %s on a read-only copy: status %d, failure %v", path, w.Code, f.last())
		}
	}
	for _, c := range u.fake.Calls() {
		if strings.HasPrefix(strings.TrimSpace(strings.ToUpper(c.SQL)), "INSERT") ||
			strings.HasPrefix(strings.TrimSpace(strings.ToUpper(c.SQL)), "UPDATE") ||
			strings.HasPrefix(strings.TrimSpace(strings.ToUpper(c.SQL)), "DELETE") {
			t.Errorf("a GET ran a write: %.60s", c.SQL)
		}
	}
}

func TestHandlers_WriteOnReadOnlyCopy_Maps503(t *testing.T) {
	u := newUnit(t)
	u.fake.SetReadOnly(true)
	w, f := u.do(asPerson(ctxT(t), newPerson()), "PUT", "/me", `{"playa_name":"Dusty Ember"}`)
	if f.count() != 1 {
		t.Fatalf("%d failures, status %d", f.count(), w.Code)
	}
	wantReadOnly(t, f.last())
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}
