package authz_test

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/postgres"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

func hasField(err error, field string) bool {
	for _, fe := range validate.Fields(err) {
		if fe.Field == field {
			return true
		}
	}
	return false
}

// F1
func TestNew_NilDependency_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		field string
		mod   func(*authz.Options)
	}{
		{"authz.db", func(o *authz.Options) { o.DB = nil }},
		{"authz.clock", func(o *authz.Options) { o.Clock = clock.Clock(nil) }},
		{"authz.logger", func(o *authz.Options) { o.Logger = (*slog.Logger)(nil) }},
		{"authz.tracer", func(o *authz.Options) { o.Tracer = telemetry.Tracer(nil) }},
		{"authz.audit", func(o *authz.Options) { o.Audit = audit.Recorder(nil) }},
		{"authz.signals", func(o *authz.Options) { o.Signals = signals.Bus(nil) }},
		{"authz.actor", func(o *authz.Options) { o.Actor = nil }},
		{"authz.lookup", func(o *authz.Options) { o.Lookup = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			r := validOptions(t, db.None())
			tc.mod(&r.opts)
			svc, err := authz.New(r.opts)
			wantKind(t, err, errs.Invalid)
			if !hasField(err, tc.field) {
				t.Fatalf("no field error %q in %v", tc.field, validate.Fields(err))
			}
			if svc != nil {
				t.Fatal("New returned a service with an error")
			}
			wantKind(t, r.opts.Validate(), errs.Invalid)
		})
	}
}

func TestNew_AllNilReportsEveryField(t *testing.T) {
	err := authz.Options{}.Validate()
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{"authz.db", "authz.clock", "authz.logger", "authz.tracer", "authz.audit", "authz.signals", "authz.actor", "authz.lookup"} {
		if !hasField(err, f) {
			t.Errorf("missing field error %q", f)
		}
	}
}

func TestNew_OptionalFieldsMayBeNil(t *testing.T) {
	r := validOptions(t, db.None())
	r.opts.Meter = nil
	r.opts.Declared = nil
	svc, err := authz.New(r.opts)
	wantNoErr(t, err)
	if svc == nil {
		t.Fatal("nil service")
	}
}

func TestNew_NoIO_WorksAgainstNoneDB(t *testing.T) {
	r := newUnitRig(t, db.None())
	if r.svc == nil {
		t.Fatal("nil service")
	}
}

// F2
func TestNew_DuplicateDeclaration_ReturnsInvalid(t *testing.T) {
	r := validOptions(t, db.None())
	r.opts.Declared = append(r.opts.Declared, r.opts.Declared[0])
	_, err := authz.New(r.opts)
	wantKind(t, err, errs.Invalid)
	if !hasField(err, "authz.declared") {
		t.Fatalf("no authz.declared field error in %v", validate.Fields(err))
	}
}

func TestNew_InvalidDeclaration_ReportsIndexedFields(t *testing.T) {
	long := strings.Repeat("a", 70)
	tests := []struct {
		name  string
		decl  authz.PermissionDecl
		field string
	}{
		{"name", authz.PermissionDecl{Name: "Bad Name", Scope: authz.ScopeSite, Reaches: model.SensitivityInternal}, "authz.declared[0].name"},
		{"scope", authz.PermissionDecl{Name: "x.y", Scope: 0, Reaches: model.SensitivityInternal}, "authz.declared[0].scope"},
		{"reaches", authz.PermissionDecl{Name: "x.y", Scope: authz.ScopeSite, Reaches: 0}, "authz.declared[0].reaches"},
		{"starter role malformed", authz.PermissionDecl{Name: "x.y", Scope: authz.ScopeSite, Reaches: model.SensitivityInternal, StarterRoles: []string{"Bad_Key"}}, "authz.declared[0].starter_roles[0]"},
		{"starter role too long", authz.PermissionDecl{Name: "x.y", Scope: authz.ScopeSite, Reaches: model.SensitivityInternal, StarterRoles: []string{long}}, "authz.declared[0].starter_roles[0]"},
		{"starter role duplicate", authz.PermissionDecl{Name: "x.y", Scope: authz.ScopeSite, Reaches: model.SensitivityInternal, StarterRoles: []string{"a-b", "a-b"}}, "authz.declared[0].starter_roles[1]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := validOptions(t, db.None())
			r.opts.Declared = []authz.PermissionDecl{tc.decl}
			_, err := authz.New(r.opts)
			wantKind(t, err, errs.Invalid)
			if !hasField(err, tc.field) {
				t.Fatalf("no field error %q in %v", tc.field, validate.Fields(err))
			}
		})
	}
}

func TestPermissionDecl_StarterRoles_Validates(t *testing.T) {
	ok := authz.PermissionDecl{Name: "x.y", Scope: authz.ScopeSite, Reaches: model.SensitivityInternal, StarterRoles: []string{"account-admin", "audit-reviewer"}}
	wantNoErr(t, ok.Validate())
	bad := ok
	bad.StarterRoles = []string{"UPPER"}
	wantKind(t, bad.Validate(), errs.Invalid)
	dup := ok
	dup.StarterRoles = []string{"a", "a"}
	wantKind(t, dup.Validate(), errs.Invalid)
}

func TestConstants_Match_Spec(t *testing.T) {
	if authz.MaxRoleNameBytes != 64 || authz.MaxDescriptionBytes != 512 || authz.MaxNoteBytes != 256 ||
		authz.MaxRolePermissions != 256 || authz.MaxPageSize != 200 || authz.DefaultPageSize != 50 {
		t.Fatal("limit constants differ from the spec")
	}
	keys := map[string]string{
		authz.RoleParticipant: "participant", authz.RoleAccountAdmin: "account-admin",
		authz.RoleAccessAdmin: "access-admin", authz.RoleDepartmentSteward: "department-steward",
		authz.RoleEventAdmin: "event-admin", authz.RoleSettingsAdmin: "settings-admin",
		authz.RoleAuditReviewer: "audit-reviewer", authz.RoleDepartmentAdmin: "department-admin",
	}
	for got, want := range keys {
		if got != want {
			t.Errorf("role key %q, want %q", got, want)
		}
	}
	want := []string{"account-admin", "access-admin", "department-steward", "event-admin", "settings-admin", "audit-reviewer"}
	if len(authz.SiteAdminRoles) != 6 {
		t.Fatalf("SiteAdminRoles = %v, want the six admin keys", authz.SiteAdminRoles)
	}
	have := map[string]bool{}
	for _, k := range authz.SiteAdminRoles {
		have[k] = true
	}
	for _, k := range want {
		if !have[k] {
			t.Errorf("SiteAdminRoles lacks %q", k)
		}
	}
	if have[authz.RoleParticipant] || have[authz.RoleDepartmentAdmin] {
		t.Error("SiteAdminRoles holds the base or department role")
	}
	if authz.Set.Name != "authz" || authz.Set.Schema != "core" {
		t.Errorf("Set = %q/%q, want authz/core", authz.Set.Name, authz.Set.Schema)
	}
}

func TestPermissions_Declared_MatchSpecTable(t *testing.T) {
	want := map[authz.Permission]struct {
		scope authz.Scope
		start []string
	}{
		pRolesRead:   {authz.ScopeSite, []string{authz.RoleAccessAdmin}},
		pRolesManage: {authz.ScopeSite, []string{authz.RoleAccessAdmin}},
		pRulesManage: {authz.ScopeSite, []string{authz.RoleAccessAdmin}},
		pGrantsRead:  {authz.ScopeDepartment, []string{authz.RoleAccessAdmin, authz.RoleDepartmentSteward, authz.RoleDepartmentAdmin}},
		pGrantsMgmt:  {authz.ScopeDepartment, []string{authz.RoleAccessAdmin, authz.RoleDepartmentAdmin}},
		pDeptGrants:  {authz.ScopeSite, []string{authz.RoleDepartmentSteward}},
	}
	if len(authz.Permissions) != len(want) {
		t.Fatalf("%d permissions declared, want %d", len(authz.Permissions), len(want))
	}
	for _, d := range authz.Permissions {
		w, ok := want[d.Name]
		if !ok {
			t.Errorf("unexpected permission %s", d.Name)
			continue
		}
		if d.Scope != w.scope {
			t.Errorf("%s scope = %v, want %v", d.Name, d.Scope, w.scope)
		}
		if d.Reaches != model.SensitivityInternal {
			t.Errorf("%s reaches %v, want internal", d.Name, d.Reaches)
		}
		got := map[string]bool{}
		for _, k := range d.StarterRoles {
			got[k] = true
		}
		for _, k := range w.start {
			if !got[k] {
				t.Errorf("%s lacks starter role %s", d.Name, k)
			}
		}
		if len(got) != len(w.start) {
			t.Errorf("%s starter roles = %v, want %v", d.Name, d.StarterRoles, w.start)
		}
		wantNoErr(t, d.Validate())
	}
}

func TestMigrations_Version_AtLeastOne(t *testing.T) {
	if v, err := db.Version(postgres.Migrations); err != nil || v < 1 {
		t.Errorf("Version = %d, %v", v, err)
	}
}
