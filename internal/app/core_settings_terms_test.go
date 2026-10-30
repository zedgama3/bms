package app

import (
	"context"
	"testing"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/settings"
)

func termDecl(key string) module.TermDecl {
	return module.TermDecl{Key: key, Defaults: map[string]module.TermForms{
		"en": {Singular: key, Plural: key + "s", SingularTitle: "T" + key, PluralTitle: "T" + key + "s"},
	}}
}

func areaNamed(areas []settings.Area, name string) (settings.Area, bool) {
	for _, a := range areas {
		if a.Name == name {
			return a, true
		}
	}
	return settings.Area{}, false
}

// F44 (D57): a module that declares terms but no settings still gets an area.
func TestSettingsAreas_TermsOnlyModule_HasArea(t *testing.T) {
	reg := module.NewRegistry()
	add := func(m module.Manifest) {
		t.Helper()
		err := reg.Add(m, func(context.Context, module.Deps) (module.Module, error) { return nil, nil }) //nolint:nilnil // never built here
		if err != nil {
			t.Fatalf("add %s: %v", m.Name, err)
		}
	}
	add(module.Manifest{Name: "zqterms", Terms: []module.TermDecl{termDecl("widget"), termDecl("gadget")}})
	add(module.Manifest{Name: "zqboth",
		Settings: []module.SettingDecl{{Key: "greeting", Kind: module.SettingText, Default: "hi"}},
		Terms:    []module.TermDecl{termDecl("gizmo")}})
	add(module.Manifest{Name: "zqsettings",
		Settings: []module.SettingDecl{{Key: "greeting", Kind: module.SettingText, Default: "hi"}}})
	add(module.Manifest{Name: "zqnothing"})
	add(module.Manifest{Name: "zqoff", Terms: []module.TermDecl{termDecl("ghost")}})
	plan, err := reg.Resolve(module.Env{Enabled: []string{"zqterms", "zqboth", "zqsettings", "zqnothing"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	areas := settingsAreas(&assembly{plan: plan})

	a, ok := areaNamed(areas, "zqterms")
	if !ok {
		t.Fatal("a terms-only module has no settings area")
	}
	if a.Core || len(a.Decls) != 0 || len(a.Terms) != 2 || a.Terms[0].Key != "widget" || a.Terms[1].Key != "gadget" {
		t.Errorf("zqterms area = %+v", a)
	}
	if b, ok := areaNamed(areas, "zqboth"); !ok || len(b.Decls) != 1 || len(b.Terms) != 1 {
		t.Errorf("zqboth area = %+v (present %v), want one setting and one term", b, ok)
	}
	if s, ok := areaNamed(areas, "zqsettings"); !ok || len(s.Decls) != 1 || len(s.Terms) != 0 {
		t.Errorf("zqsettings area = %+v (present %v)", s, ok)
	}
	if _, ok := areaNamed(areas, "zqnothing"); ok {
		t.Error("a module with neither settings nor terms got an area")
	}
	if _, ok := areaNamed(areas, "zqoff"); ok {
		t.Error("a disabled module got an area")
	}
	for _, a := range areas {
		if err := a.Validate(); err != nil {
			t.Errorf("area %s: %v", a.Name, err)
		}
	}
}

// The flat term namespace holds across every registered module and the core's own words.
func TestManifests_RealModules_HaveNoTermCollision(t *testing.T) {
	seen := map[string]string{}
	for _, m := range module.Default().Manifests() {
		for _, d := range m.Terms {
			if prev, dup := seen[d.Key]; dup {
				t.Errorf("term %q declared by %s and %s: promote it to settings.CoreTerms", d.Key, prev, m.Name)
			}
			seen[d.Key] = m.Name
		}
	}
	for _, core := range settings.CoreTerms {
		if by, dup := seen[core]; dup {
			t.Errorf("core term %q also declared by %s", core, by)
		}
	}
}
