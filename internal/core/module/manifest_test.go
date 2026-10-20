package module_test

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// tripFS fails the test if anything opens it: Manifest.Validate never reads Migrations.
type tripFS struct{ t *testing.T }

func (f tripFS) Open(name string) (fs.File, error) {
	f.t.Errorf("Manifest.Validate read Migrations (Open(%q))", name)
	return nil, fs.ErrNotExist
}

func fullManifest() module.Manifest {
	return module.Manifest{
		Name:        "alpha",
		Migrations:  nil,
		Permissions: []authz.PermissionDecl{perm("alpha.read"), {Name: "alpha.shifts.manage", Scope: authz.ScopeDepartment, Reaches: model.SensitivitySensitive}},
		Settings: []module.SettingDecl{
			{Key: "greeting", Kind: module.SettingText, Default: "hello"},
			{Key: "mode", Kind: module.SettingEnum, Options: []string{"a", "b"}, Default: "a"},
		},
		Publishes:  []signals.Name{"alpha.greeted"},
		Subscribes: []signals.Name{"alpha.greeted", "beta.thing", "people.person-created"},
		Provides:   []module.Capability{"greeter"},
		Requires:   []module.Capability{"credit"},
		Uses:       []module.Capability{"badge"},
		Defines:    []module.ExtensionPointDecl{{Name: "alpha.banner", Accepts: acceptsString}},
		ContributesTo: []module.ExtensionPoint{
			"alpha.banner", "people.person-page",
		},
	}
}

func TestManifestValidate_MinimalAndFull_Pass(t *testing.T) {
	if err := (module.Manifest{Name: "alpha"}).Validate(); err != nil {
		t.Fatalf("name-only manifest: %v", err)
	}
	full := fullManifest()
	full.Migrations = tripFS{t}
	if err := full.Validate(); err != nil {
		t.Fatalf("full manifest: %v", err)
	}
}

func TestManifestValidate_BadOrReservedName_ReturnsInvalid(t *testing.T) {
	bad := []string{"", "a", "Alpha", "al-pha", "al_pha", "1alpha", "al pha", strings.Repeat("a", 33),
		"public", "information", "pg", "pgfoo", "pg_catalog", "core"}
	for _, n := range bad {
		t.Run(fmt.Sprintf("%q", n), func(t *testing.T) {
			wantField(t, (module.Manifest{Name: n}).Validate(), "name")
		})
	}
	for _, n := range []string{"ab", "afterreports", "hello", "a1", strings.Repeat("a", 32), "pxg", "publicity"} {
		if err := (module.Manifest{Name: n}).Validate(); err != nil {
			t.Errorf("name %q: %v, want valid", n, err)
		}
	}
}

func TestReservedNames_Pinned(t *testing.T) {
	reserved := []string{
		// core areas
		"core", "people", "accounts", "access", "identity", "authz", "departments", "events",
		"signals", "notify", "audit", "files", "jobs", "settings", "telemetry", "db", "module", "modules",
		// shell packages
		"api", "app", "config", "web", "admin", "client", "regional",
		// PostgreSQL
		"public", "information", "pg", "pgsomething",
	}
	for _, n := range reserved {
		t.Run(n, func(t *testing.T) {
			wantField(t, (module.Manifest{Name: n}).Validate(), "name")
		})
	}
}

func TestManifestValidate_PermissionNotPrefixed_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name  string
		perms []authz.PermissionDecl
		field string
	}{
		{"other module prefix", []authz.PermissionDecl{perm("alpha.ok"), perm("beta.read")}, "permissions[1].name"},
		{"no prefix at all", []authz.PermissionDecl{perm("read")}, "permissions[0].name"},
		{"prefix is only a prefix of the name", []authz.PermissionDecl{perm("alphabet.read")}, "permissions[0].name"},
		{"malformed", []authz.PermissionDecl{perm("alpha.Read")}, "permissions[0].name"},
		{"name is only the module", []authz.PermissionDecl{perm("alpha")}, "permissions[0].name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := mf("alpha", func(m *module.Manifest) { m.Permissions = tc.perms })
			wantField(t, m.Validate(), tc.field)
		})
	}
}

func TestManifestValidate_PermissionReachesUnset_ReturnsInvalid(t *testing.T) {
	for _, r := range []model.Sensitivity{0, 99} {
		m := mf("alpha", func(m *module.Manifest) {
			m.Permissions = []authz.PermissionDecl{perm("alpha.ok"), {Name: "alpha.x", Scope: authz.ScopeSite, Reaches: r}}
		})
		wantField(t, m.Validate(), "permissions[1].reaches")
	}
}

func TestManifestValidate_PermissionScopeInvalid_ReturnsInvalid(t *testing.T) {
	for _, s := range []authz.Scope{0, 9} {
		m := mf("alpha", func(m *module.Manifest) {
			m.Permissions = []authz.PermissionDecl{perm("alpha.ok"), {Name: "alpha.x", Scope: s, Reaches: model.SensitivityInternal}}
		})
		wantField(t, m.Validate(), "permissions[1].scope")
	}
}

func TestManifestValidate_Duplicates_ReturnInvalid(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*module.Manifest)
		prefix string
	}{
		{"permission", func(m *module.Manifest) { m.Permissions = []authz.PermissionDecl{perm("alpha.a"), perm("alpha.a")} }, "permissions[1]"},
		{"setting key", func(m *module.Manifest) {
			s := module.SettingDecl{Key: "k", Kind: module.SettingBool, Default: "true"}
			m.Settings = []module.SettingDecl{s, s}
		}, "settings[1]"},
		{"publishes", publishes("alpha.a", "alpha.a"), "publishes[1]"},
		{"subscribes", subscribes("beta.a", "beta.a"), "subscribes[1]"},
		{"provides", provides("x", "x"), "provides[1]"},
		{"requires", requires("x", "x"), "requires[1]"},
		{"uses", uses("x", "x"), "uses[1]"},
		{"defines", defines("alpha.p", "alpha.p"), "defines[1]"},
		{"contributesTo", contributes("beta.p", "beta.p"), "contributesTo[1]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantFieldPrefix(t, mf("alpha", tc.mutate).Validate(), tc.prefix)
		})
	}
}

func TestManifestValidate_SettingErrorsCarryIndexedField(t *testing.T) {
	m := mf("alpha", func(m *module.Manifest) {
		m.Settings = []module.SettingDecl{
			{Key: "ok", Kind: module.SettingBool, Default: "true"},
			{Key: "n", Kind: module.SettingInt, Default: "x"},
		}
	})
	wantField(t, m.Validate(), "settings[1].default")
}

func TestManifestValidate_SignalNames_ReturnInvalid(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*module.Manifest)
		field  string
	}{
		{"published wrong publisher", publishes("beta.thing"), "publishes[0]"},
		{"published malformed", publishes("alpha"), "publishes[0]"},
		{"published second bad", publishes("alpha.ok", "alpha.Bad"), "publishes[1]"},
		{"subscribed malformed", subscribes("nodot"), "subscribes[0]"},
		{"subscribed upper", subscribes("beta.ok", "Beta.thing"), "subscribes[1]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { wantField(t, mf("alpha", tc.mutate).Validate(), tc.field) })
	}
}

func TestManifestValidate_CapabilityConflicts_ReturnInvalid(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*module.Manifest)
		prefix string
	}{
		{"malformed provides", provides("Bad"), "provides[0]"},
		{"malformed requires", requires("a.b"), "requires[0]"},
		{"malformed uses", uses(""), "uses[0]"},
		{"requires and uses", func(m *module.Manifest) { requires("x")(m); uses("x")(m) }, "uses[0]"},
		{"provides and requires", func(m *module.Manifest) { provides("x")(m); requires("x")(m) }, "requires[0]"},
		{"provides and uses", func(m *module.Manifest) { provides("x")(m); uses("x")(m) }, "uses[0]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { wantFieldPrefix(t, mf("alpha", tc.mutate).Validate(), tc.prefix) })
	}
}

func TestManifestValidate_ExtensionPoints_ReturnInvalid(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*module.Manifest)
		prefix string
	}{
		{"definer is another module", defines("beta.page"), "defines[0]"},
		{"nil accepts", func(m *module.Manifest) {
			m.Defines = []module.ExtensionPointDecl{{Name: "alpha.page"}}
		}, "defines[0]"},
		{"malformed defined name", defines("alpha"), "defines[0]"},
		{"contributes malformed", contributes("nodot"), "contributesTo[0]"},
		{"second contributes malformed", contributes("beta.ok", "Beta.page"), "contributesTo[1]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { wantFieldPrefix(t, mf("alpha", tc.mutate).Validate(), tc.prefix) })
	}
}

func TestManifestValidate_ReportsAllProblems(t *testing.T) {
	m := module.Manifest{
		Name:        "Bad Name",
		Permissions: []authz.PermissionDecl{{Name: "beta.x"}},
		Settings:    []module.SettingDecl{{Key: "k", Kind: module.SettingInt, Default: "x"}},
		Publishes:   []signals.Name{"beta.x"},
		Subscribes:  []signals.Name{"nodot"},
		Requires:    []module.Capability{"Bad"},
		Defines:     []module.ExtensionPointDecl{{Name: "beta.p"}},
	}
	err := m.Validate()
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{"name", "permissions[0].name", "permissions[0].scope", "permissions[0].reaches",
		"settings[0].default", "publishes[0]", "subscribes[0]", "requires[0]"} {
		if !fieldSet(err)[f] {
			t.Errorf("missing field error %q; have %v", f, fieldSet(err))
		}
	}
	wantFieldPrefix(t, err, "defines[0]")
}
